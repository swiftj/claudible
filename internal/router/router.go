// Package router provides notification routing for Claude Code hooks.
// It orchestrates the flow from hook input to notification providers.
package router

import (
	"context"
	"fmt"
	"strings"

	"github.com/swiftj/claudible/internal/config"
	"github.com/swiftj/claudible/internal/dedup"
	"github.com/swiftj/claudible/internal/evaluator"
	"github.com/swiftj/claudible/internal/hook"
	"github.com/swiftj/claudible/internal/logging"
	"github.com/swiftj/claudible/internal/notification"
	"github.com/swiftj/claudible/internal/providers/desktop"
	"github.com/swiftj/claudible/internal/providers/http"
	"github.com/swiftj/claudible/internal/providers/imessage"
	"github.com/swiftj/claudible/internal/providers/pushbullet"
	"github.com/swiftj/claudible/internal/providers/pushover"
	"github.com/swiftj/claudible/internal/providers/sms"
	"github.com/swiftj/claudible/internal/state"
	"github.com/swiftj/claudible/internal/transcript"
)

// Router orchestrates the notification flow: receives hook input,
// parses transcript, classifies state, and routes to enabled providers.
type Router struct {
	config       *config.Config
	classifier   *state.Classifier
	registry     *notification.ProviderRegistry
	evaluator    *evaluator.Evaluator
	deduplicator *dedup.Deduplicator
}

// NewRouter creates a new Router with providers initialized from configuration.
func NewRouter(cfg *config.Config) *Router {
	return newRouter(cfg, true)
}

// NewRouterForTesting creates a Router without persistent dedup state.
// This prevents cross-test contamination in unit tests.
func NewRouterForTesting(cfg *config.Config) *Router {
	return newRouter(cfg, false)
}

// newRouter is the internal constructor with dedup persistence control.
func newRouter(cfg *config.Config, persistDedup bool) *Router {
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
		config:       cfg,
		classifier:   state.NewClassifier(),
		registry:     notification.NewProviderRegistry(),
		evaluator:    evaluator.NewEvaluator(evalConfig),
		deduplicator: dedup.NewWithPersistence(cfg.Behavior.DedupeWindow, persistDedup),
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
		logging.Debug("skipping notification", "state", classifiedState, "reason", "not in notify_on list")
		return nil
	}

	// Build the notification message (use LLM summary if available)
	msg := r.buildMessageWithSummary(input, t, classifiedState, summary)

	// Check for duplicate notifications
	if r.deduplicator != nil && !r.deduplicator.ShouldSend(msg) {
		logging.Debug("suppressing duplicate", "session", input.SessionID)
		return nil
	}

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
			// Get enabled provider names for formatting hints
			enabledProviders := r.config.GetEnabledProviders()
			result, err := r.evaluator.Evaluate(ctx, userRequest, assistantResponse, enabledProviders...)
			if err != nil {
				logging.Warn("evaluator failed, using heuristics", "error", err)
			} else if result != nil {
				// Map evaluator state to internal state
				evalState := r.mapEvaluatorState(result.State)
				logging.Debug("LLM classification", "state", evalState, "confidence", result.Confidence)
				return evalState, result.Summary
			}
		}
	}

	// Fall back to heuristic classification
	classifiedState := r.classifier.Classify(input, t)
	logging.Debug("heuristic classification", "state", classifiedState)
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

	// Get the last user prompt as the request (truncated for mobile)
	request := ""
	if t != nil {
		request = truncateRequest(t.LastUserPrompt())
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
// Maximum summary length is 330 chars - fits well on mobile while allowing complete thoughts.
const maxSummaryLength = 330

func (r *Router) generateSummary(t *transcript.Transcript, s state.State) string {
	// First, try to get a meaningful summary from the last assistant response
	if t != nil {
		lastResponse := t.LastAssistantResponse()
		if lastResponse != "" {
			// Truncate to mobile-friendly length (max 150 chars)
			summary := truncateSummary(lastResponse, maxSummaryLength)
			return summary
		}
	}

	// Fall back to state description
	return s.Description()
}

// truncateSummary sanitizes and truncates a string to the specified length.
// It strips markdown formatting and uses smart truncation (sentence boundaries) instead of ellipsis.
func truncateSummary(s string, maxLen int) string {
	if maxLen <= 0 {
		maxLen = 330 // Default maximum
	}

	// First, sanitize any markdown formatting (this also handles whitespace cleanup)
	s = evaluator.SanitizeSummary(s)

	// If already within limit after sanitization, return as-is
	if len(s) <= maxLen {
		return s
	}

	// Smart truncate to sentence boundary (no ellipsis)
	return smartTruncateText(s, maxLen)
}

// smartTruncateText truncates text to maxLen by finding the previous sentence boundary
// (period or line break) rather than using ellipsis mid-sentence.
func smartTruncateText(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}

	truncated := s[:maxLen]

	// Find the last sentence boundary (period followed by space, or newline)
	lastPeriod := strings.LastIndex(truncated, ". ")
	lastNewline := strings.LastIndex(truncated, "\n")

	// Use whichever boundary is later (closer to the end)
	boundary := lastPeriod
	if lastNewline > boundary {
		boundary = lastNewline
	}

	// If we found a good boundary in the second half, use it
	if boundary > maxLen/2 {
		if truncated[boundary] == '.' {
			return truncated[:boundary+1] // Include the period
		}
		return strings.TrimSpace(truncated[:boundary])
	}

	// Look for standalone period at end of word
	lastPeriodOnly := strings.LastIndex(truncated, ".")
	if lastPeriodOnly > maxLen/2 {
		return truncated[:lastPeriodOnly+1]
	}

	// Last resort: truncate at word boundary without ellipsis
	lastSpace := strings.LastIndex(truncated, " ")
	if lastSpace > maxLen/2 {
		return strings.TrimSpace(truncated[:lastSpace])
	}

	// Absolute fallback: just trim to length
	return strings.TrimSpace(truncated)
}

// truncateRequest extracts a concise request from the user prompt.
// For very long prompts (like context summaries), it extracts just the first line.
// Maximum length is 100 characters for mobile display.
const maxRequestLength = 100

func truncateRequest(s string) string {
	if s == "" {
		return ""
	}

	// Clean and get the first line
	s = strings.TrimSpace(s)

	// Find first newline
	if idx := strings.Index(s, "\n"); idx > 0 {
		s = s[:idx]
	}

	// Strip any leading "This session is being continued..." boilerplate
	// These are context restoration messages, not actual requests
	if strings.HasPrefix(s, "This session is being continued") {
		// Try to find the actual request after "Request:" or similar marker
		// If not found, just use a generic description
		s = "[Continued session]"
	}

	// Sanitize any markdown
	s = evaluator.SanitizeSummary(s)
	s = strings.TrimSpace(s)

	// Final length truncation using smart truncation (no ellipsis)
	if len(s) > maxRequestLength {
		s = smartTruncateText(s, maxRequestLength)
	}

	return s
}

// handleProviderErrors aggregates errors from multiple providers.
// Individual provider failures are logged but don't stop other providers.
func (r *Router) handleProviderErrors(errors map[string]error) error {
	if len(errors) == 0 {
		return nil
	}

	// Log each error
	for provider, err := range errors {
		logging.Error("provider failed", "provider", provider, "error", err)
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
