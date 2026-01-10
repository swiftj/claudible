// Package state provides state classification for Claude Code agent sessions.
package state

import (
	"strings"

	"github.com/johnswift/claudible/internal/hook"
	"github.com/johnswift/claudible/internal/transcript"
)

// Default error patterns that indicate a stopped/failed state.
var defaultErrorPatterns = []string{
	"error",
	"failed",
	"exception",
	"cannot",
	"unable to",
	"unexpected",
}

// Classifier determines the semantic state of a Claude Code agent
// based on transcript analysis and hook event type.
type Classifier struct {
	errorPatterns []string
}

// Option is a functional option for configuring a Classifier.
type Option func(*Classifier)

// WithErrorPatterns sets custom error patterns for the classifier.
// These patterns are matched case-insensitively against the last assistant response.
func WithErrorPatterns(patterns []string) Option {
	return func(c *Classifier) {
		c.errorPatterns = patterns
	}
}

// WithAdditionalErrorPatterns adds additional error patterns to the default set.
func WithAdditionalErrorPatterns(patterns []string) Option {
	return func(c *Classifier) {
		c.errorPatterns = append(c.errorPatterns, patterns...)
	}
}

// NewClassifier creates a new Classifier with the given options.
// If no error patterns are specified, default patterns are used.
func NewClassifier(opts ...Option) *Classifier {
	c := &Classifier{
		errorPatterns: make([]string, len(defaultErrorPatterns)),
	}
	copy(c.errorPatterns, defaultErrorPatterns)

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// Classify determines the state of the agent based on the hook input
// and transcript content.
//
// Classification logic:
//   - If notification_type is "idle_prompt" → StateWaiting
//   - If notification_type is "stop":
//   - Check transcript.HasCompletionIndicators() → StateComplete
//   - Check for error patterns in last response → StateStopped
//   - Otherwise → StateIdle
func (c *Classifier) Classify(input *hook.HookInput, t *transcript.Transcript) State {
	if input == nil {
		return StateIdle
	}

	notificationType := input.GetNotificationType()

	switch notificationType {
	case hook.NotificationTypeIdlePrompt:
		return StateWaiting

	case hook.NotificationTypeStop:
		return c.classifyStopState(t)

	default:
		return StateIdle
	}
}

// classifyStopState determines the specific state when a stop notification is received.
func (c *Classifier) classifyStopState(t *transcript.Transcript) State {
	// Check for completion indicators first
	if t != nil && t.HasCompletionIndicators() {
		return StateComplete
	}

	// Check for error patterns in the last response
	if c.hasErrorPatterns(t) {
		return StateStopped
	}

	// Default to idle
	return StateIdle
}

// hasErrorPatterns checks if the last assistant response contains any error patterns.
func (c *Classifier) hasErrorPatterns(t *transcript.Transcript) bool {
	if t == nil {
		return false
	}

	response := t.LastAssistantResponse()
	if response == "" {
		return false
	}

	responseLower := strings.ToLower(response)
	for _, pattern := range c.errorPatterns {
		if strings.Contains(responseLower, strings.ToLower(pattern)) {
			return true
		}
	}

	return false
}

// ErrorPatterns returns a copy of the current error patterns.
func (c *Classifier) ErrorPatterns() []string {
	patterns := make([]string, len(c.errorPatterns))
	copy(patterns, c.errorPatterns)
	return patterns
}
