// Package router provides notification routing for Claude Code hooks.
// It orchestrates the flow from hook input to notification providers.
package router

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/johnswift/claudible/internal/config"
	"github.com/johnswift/claudible/internal/evaluator"
	"github.com/johnswift/claudible/internal/hook"
	"github.com/johnswift/claudible/internal/notification"
	"github.com/johnswift/claudible/internal/providers/desktop"
	"github.com/johnswift/claudible/internal/providers/http"
	"github.com/johnswift/claudible/internal/providers/imessage"
	"github.com/johnswift/claudible/internal/providers/pushbullet"
	"github.com/johnswift/claudible/internal/providers/pushover"
	"github.com/johnswift/claudible/internal/providers/sms"
	"github.com/johnswift/claudible/internal/state"
	"github.com/johnswift/claudible/internal/transcript"
)

// Router orchestrates the notification flow: receives hook input,
// parses transcript, classifies state, and routes to enabled providers.
type Router struct {
	config     *config.Config
	classifier *state.Classifier
	registry   *notification.ProviderRegistry
	evaluator  *evaluator.Evaluator
}

// NewRouter creates a new Router with providers initialized from configuration.
func NewRouter(cfg *config.Config) *Router {
	if cfg == nil {
		cfg = config.Default()
	}

	// Convert config.EvaluatorConfig to evaluator.Config
	evalConfig := &evaluator.Config{
		Enabled:   cfg.Evaluator.Enabled,
		Provider:  cfg.Evaluator.Provider,
		APIKey:    cfg.Evaluator.APIKey,
		Model:     cfg.Evaluator.Model,
		MaxTokens: cfg.Evaluator.MaxTokens,
	}

	r := &Router{
		config:     cfg,
		classifier: state.NewClassifier(),
		registry:   notification.NewProviderRegistry(),
		evaluator:  evaluator.NewEvaluator(evalConfig),
	}

	// Initialize and register providers based on config
	r.initializeProviders()

	return r
}

// initializeProviders creates and registers all configured providers.
func (r *Router) initializeProviders() {
	// Register iMessage provider
	imsgProvider := imessage.NewFromConfig(&r.config.Notifications.Providers.IMessage)
	if r.config.Behavior.MaxMessageLength > 0 {
		imsgProvider.SetMaxMessageLength(r.config.Behavior.MaxMessageLength)
	}
	r.registry.Register(imsgProvider)

	// Register HTTP provider
	httpProvider := http.NewFromConfig(&r.config.Notifications.Providers.HTTP)
	r.registry.Register(httpProvider)

	// Register SMS provider
	smsProvider := sms.NewFromConfig(&r.config.Notifications.Providers.SMS)
	r.registry.Register(smsProvider)

	// Register Desktop provider
	desktopProvider := desktop.NewFromConfig(&r.config.Notifications.Providers.Desktop)
	r.registry.Register(desktopProvider)

	// Register Pushover provider
	pushoverProvider := pushover.NewFromConfig(&r.config.Notifications.Providers.Pushover)
	r.registry.Register(pushoverProvider)

	// Register Pushbullet provider
	pushbulletProvider := pushbullet.NewFromConfig(&r.config.Notifications.Providers.Pushbullet)
	r.registry.Register(pushbulletProvider)
}

// Process is the main entry point for handling hook events.
// It parses the transcript, classifies the state, and routes notifications
// to all enabled providers.
func (r *Router) Process(ctx context.Context, input *hook.HookInput) error {
	// Check for context cancellation
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Validate input
	if input == nil {
		return fmt.Errorf("router: nil hook input")
	}

	// Parse transcript from input path
	t, err := transcript.Parse(input.TranscriptPath)
	if err != nil {
		return fmt.Errorf("router: failed to parse transcript: %w", err)
	}

	// Classify state - try LLM evaluator first, fall back to heuristics
	classifiedState, summary := r.classifyState(ctx, input, t)

	// Check if we should notify on this state
	if !r.config.ShouldNotifyOn(classifiedState.String()) {
		log.Printf("router: skipping notification for state %q (not in notify_on list)", classifiedState)
		return nil
	}

	// Build the notification message (use LLM summary if available)
	msg := r.buildMessageWithSummary(input, t, classifiedState, summary)

	// Send to all enabled providers
	errors := r.registry.SendAll(ctx, msg)

	// Log errors but aggregate them
	return r.handleProviderErrors(errors)
}

// classifyState determines the agent state using LLM evaluator if available,
// falling back to heuristic classification otherwise.
// Returns the classified state and an optional LLM-generated summary.
func (r *Router) classifyState(ctx context.Context, input *hook.HookInput, t *transcript.Transcript) (state.State, string) {
	// Try LLM evaluator first if enabled
	if r.evaluator != nil && r.evaluator.Enabled() && t != nil {
		userRequest := t.LastUserPrompt()
		assistantResponse := t.LastAssistantResponse()

		if userRequest != "" && assistantResponse != "" {
			result, err := r.evaluator.Evaluate(ctx, userRequest, assistantResponse)
			if err != nil {
				log.Printf("router: evaluator failed, falling back to heuristics: %v", err)
			} else if result != nil {
				// Map evaluator state to internal state
				evalState := r.mapEvaluatorState(result.State)
				log.Printf("router: LLM classified state as %q (confidence: %.2f)", evalState, result.Confidence)
				return evalState, result.Summary
			}
		}
	}

	// Fall back to heuristic classification
	classifiedState := r.classifier.Classify(input, t)
	log.Printf("router: heuristic classified state as %q", classifiedState)
	return classifiedState, ""
}

// mapEvaluatorState converts evaluator state strings to internal State type.
func (r *Router) mapEvaluatorState(evalState string) state.State {
	switch evalState {
	case evaluator.StateComplete:
		return state.StateComplete
	case evaluator.StateWaiting:
		return state.StateWaiting
	case evaluator.StateNeedsReview:
		// Map needs_review to complete (work is done, just needs verification)
		return state.StateComplete
	default:
		return state.StateIdle
	}
}

// buildMessage creates a NotificationMessage from the input, transcript, and state.
func (r *Router) buildMessage(input *hook.HookInput, t *transcript.Transcript, s state.State) notification.NotificationMessage {
	return r.buildMessageWithSummary(input, t, s, "")
}

// buildMessageWithSummary creates a NotificationMessage, preferring the provided summary
// (e.g., from LLM evaluator) over the default generated summary.
func (r *Router) buildMessageWithSummary(input *hook.HookInput, t *transcript.Transcript, s state.State, llmSummary string) notification.NotificationMessage {
	// Generate title based on state
	title := r.generateTitle(s)

	// Use LLM summary if provided, otherwise generate from transcript
	summary := llmSummary
	if summary == "" {
		summary = r.generateSummary(t, s)
	}

	// Get the last user prompt as the request
	request := ""
	if t != nil {
		request = t.LastUserPrompt()
	}

	// Get cwd from input
	cwd := ""
	if input != nil {
		cwd = input.Cwd
	}

	// Get session ID from input
	sessionID := ""
	if input != nil {
		sessionID = input.SessionID
	}

	return notification.NewNotificationMessage(title, s, summary, request, cwd, sessionID)
}

// generateTitle creates a notification title based on the state.
func (r *Router) generateTitle(s state.State) string {
	switch s {
	case state.StateComplete:
		return "Claude Code - Complete"
	case state.StateWaiting:
		return "Claude Code - Waiting"
	case state.StateStopped:
		return "Claude Code - Stopped"
	case state.StateIdle:
		return "Claude Code - Idle"
	default:
		return "Claude Code"
	}
}

// generateSummary creates a summary from the transcript and state.
func (r *Router) generateSummary(t *transcript.Transcript, s state.State) string {
	// First, try to get a meaningful summary from the last assistant response
	if t != nil {
		lastResponse := t.LastAssistantResponse()
		if lastResponse != "" {
			// Truncate if too long
			summary := truncateSummary(lastResponse, r.config.Behavior.MaxMessageLength/2)
			return summary
		}
	}

	// Fall back to state description
	return s.Description()
}

// truncateSummary truncates a string to the specified length, adding ellipsis if needed.
func truncateSummary(s string, maxLen int) string {
	if maxLen <= 0 {
		maxLen = 200 // Default maximum
	}

	// Clean up the string - remove excessive whitespace
	s = strings.TrimSpace(s)
	s = strings.Join(strings.Fields(s), " ")

	if len(s) <= maxLen {
		return s
	}

	// Try to truncate at a word boundary
	truncated := s[:maxLen]
	lastSpace := strings.LastIndex(truncated, " ")
	if lastSpace > maxLen/2 {
		truncated = truncated[:lastSpace]
	}

	return truncated + "..."
}

// handleProviderErrors aggregates errors from multiple providers.
// Individual provider failures are logged but don't stop other providers.
func (r *Router) handleProviderErrors(errors map[string]error) error {
	if len(errors) == 0 {
		return nil
	}

	// Log each error
	for provider, err := range errors {
		log.Printf("router: provider %q failed: %v", provider, err)
	}

	// If all providers failed, return an aggregated error
	enabledCount := r.registry.EnabledCount()
	if len(errors) >= enabledCount && enabledCount > 0 {
		var errMsgs []string
		for provider, err := range errors {
			errMsgs = append(errMsgs, fmt.Sprintf("%s: %v", provider, err))
		}
		return fmt.Errorf("all providers failed: %s", strings.Join(errMsgs, "; "))
	}

	// Some providers succeeded, so we don't return an error
	return nil
}

// Registry returns the provider registry for testing or custom provider registration.
func (r *Router) Registry() *notification.ProviderRegistry {
	return r.registry
}

// Config returns the current configuration.
func (r *Router) Config() *config.Config {
	return r.config
}

// Classifier returns the state classifier.
func (r *Router) Classifier() *state.Classifier {
	return r.classifier
}
