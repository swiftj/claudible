// Package notification provides types and interfaces for the notification system.
package notification

import (
	"context"
	"time"

	"github.com/swiftj/claudible/internal/state"
)

// NotificationMessage is the unified message format for all providers.
// It contains all the information needed to send a notification about
// the current state of a Claude Code session.
type NotificationMessage struct {
	// Title is the notification title (e.g., "Claude Code - Complete").
	Title string `json:"title"`

	// State is the classified state of the agent (complete, waiting, stopped, idle).
	State state.State `json:"state"`

	// Summary is a brief description of what happened or the current status.
	Summary string `json:"summary"`

	// Request is the last user prompt/request that was processed.
	Request string `json:"request"`

	// Cwd is the current working directory of the Claude Code session.
	Cwd string `json:"cwd"`

	// SessionID is the unique identifier for the Claude Code session.
	SessionID string `json:"session_id"`

	// Timestamp is when the notification was generated.
	Timestamp time.Time `json:"timestamp"`
}

// NewNotificationMessage creates a new NotificationMessage with the current timestamp.
func NewNotificationMessage(title string, s state.State, summary, request, cwd, sessionID string) NotificationMessage {
	return NotificationMessage{
		Title:     title,
		State:     s,
		Summary:   summary,
		Request:   request,
		Cwd:       cwd,
		SessionID: sessionID,
		Timestamp: time.Now(),
	}
}

// Provider interface that all notification providers must implement.
// Each provider (e.g., macOS, Pushover, ntfy) implements this interface
// to handle sending notifications through their specific channel.
type Provider interface {
	// Name returns the unique identifier for this provider.
	Name() string

	// Enabled returns whether this provider is currently enabled and configured.
	Enabled() bool

	// Send sends a notification message through this provider.
	// Returns an error if the notification could not be sent.
	Send(ctx context.Context, msg NotificationMessage) error
}

// ProviderRegistry manages multiple notification providers.
type ProviderRegistry struct {
	providers []Provider
}

// NewProviderRegistry creates a new empty provider registry.
func NewProviderRegistry() *ProviderRegistry {
	return &ProviderRegistry{
		providers: make([]Provider, 0),
	}
}

// Register adds a provider to the registry.
func (r *ProviderRegistry) Register(p Provider) {
	r.providers = append(r.providers, p)
}

// GetEnabled returns all enabled providers.
func (r *ProviderRegistry) GetEnabled() []Provider {
	enabled := make([]Provider, 0)
	for _, p := range r.providers {
		if p.Enabled() {
			enabled = append(enabled, p)
		}
	}
	return enabled
}

// GetByName returns a provider by name, or nil if not found.
func (r *ProviderRegistry) GetByName(name string) Provider {
	for _, p := range r.providers {
		if p.Name() == name {
			return p
		}
	}
	return nil
}

// SendAll sends a notification to all enabled providers.
// Returns a map of provider names to any errors that occurred.
func (r *ProviderRegistry) SendAll(ctx context.Context, msg NotificationMessage) map[string]error {
	errors := make(map[string]error)
	for _, p := range r.GetEnabled() {
		if err := p.Send(ctx, msg); err != nil {
			errors[p.Name()] = err
		}
	}
	return errors
}

// Count returns the total number of registered providers.
func (r *ProviderRegistry) Count() int {
	return len(r.providers)
}

// EnabledCount returns the number of enabled providers.
func (r *ProviderRegistry) EnabledCount() int {
	return len(r.GetEnabled())
}
