// Package desktop provides native desktop notifications for macOS and Linux.
package desktop

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"unicode"

	"github.com/johnswift/claudible/internal/config"
	"github.com/johnswift/claudible/internal/notification"
)

const (
	// providerName is the unique identifier for this provider.
	providerName = "desktop"

	// maxSummaryLength is the maximum length for notification body text.
	maxSummaryLength = 200
)

// CommandExecutor interface allows for testable command execution.
type CommandExecutor interface {
	Execute(ctx context.Context, name string, args ...string) error
}

// DefaultExecutor implements CommandExecutor using os/exec.
type DefaultExecutor struct{}

// Execute runs a command with the given context, name, and arguments.
func (e *DefaultExecutor) Execute(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	return cmd.Run()
}

// Provider implements the notification.Provider interface for desktop notifications.
type Provider struct {
	enabled  bool
	executor CommandExecutor
}

// Option is a functional option for configuring the Provider.
type Option func(*Provider)

// WithExecutor sets a custom CommandExecutor for the provider.
// This is primarily used for testing.
func WithExecutor(executor CommandExecutor) Option {
	return func(p *Provider) {
		p.executor = executor
	}
}

// New creates a new desktop notification Provider.
func New(enabled bool, opts ...Option) *Provider {
	p := &Provider{
		enabled:  enabled,
		executor: &DefaultExecutor{},
	}

	for _, opt := range opts {
		opt(p)
	}

	return p
}

// NewFromConfig creates a Provider from configuration.
func NewFromConfig(cfg *config.DesktopConfig, opts ...Option) *Provider {
	return New(cfg.Enabled, opts...)
}

// Name returns the unique identifier for this provider.
func (p *Provider) Name() string {
	return providerName
}

// Enabled returns whether this provider is currently enabled.
func (p *Provider) Enabled() bool {
	return p.enabled
}

// Send sends a desktop notification.
func (p *Provider) Send(ctx context.Context, msg notification.NotificationMessage) error {
	if !p.enabled {
		return nil
	}

	title := formatTitle(msg)
	body := formatBody(msg)

	switch runtime.GOOS {
	case "darwin":
		return p.sendMacOS(ctx, title, body)
	case "linux":
		return p.sendLinux(ctx, title, body)
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

// sendMacOS sends a notification using osascript on macOS.
func (p *Provider) sendMacOS(ctx context.Context, title, body string) error {
	// Escape double quotes in title and body for AppleScript
	escapedTitle := escapeAppleScript(title)
	escapedBody := escapeAppleScript(body)

	script := fmt.Sprintf(`display notification "%s" with title "%s"`, escapedBody, escapedTitle)
	return p.executor.Execute(ctx, "osascript", "-e", script)
}

// sendLinux sends a notification using notify-send on Linux.
func (p *Provider) sendLinux(ctx context.Context, title, body string) error {
	return p.executor.Execute(ctx, "notify-send", title, body)
}

// formatTitle creates the notification title from the message.
func formatTitle(msg notification.NotificationMessage) string {
	stateStr := titleCase(string(msg.State))
	return fmt.Sprintf("Claude Code: %s", stateStr)
}

// titleCase converts the first character to uppercase.
func titleCase(s string) string {
	if s == "" {
		return s
	}
	runes := []rune(s)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}

// formatBody creates the notification body from the message, truncating if necessary.
func formatBody(msg notification.NotificationMessage) string {
	summary := msg.Summary
	if len(summary) > maxSummaryLength {
		summary = summary[:maxSummaryLength-3] + "..."
	}
	return summary
}

// escapeAppleScript escapes special characters for AppleScript strings.
func escapeAppleScript(s string) string {
	// Escape backslashes first, then double quotes
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	return s
}

// Compile-time check that Provider implements notification.Provider.
var _ notification.Provider = (*Provider)(nil)
