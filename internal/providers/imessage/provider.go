//go:build darwin

// Package imessage provides an iMessage notification provider for macOS.
// It sends notifications via AppleScript using the Messages.app.
package imessage

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/swiftj/claudible/internal/config"
	"github.com/swiftj/claudible/internal/evaluator"
	"github.com/swiftj/claudible/internal/logging"
	"github.com/swiftj/claudible/internal/notification"
)

const (
	// ProviderName is the unique identifier for this provider.
	ProviderName = "imessage"

	// DefaultMaxMessageLength is the default maximum message length for iMessage.
	// iMessage supports up to 20,000 characters, but we keep it reasonable.
	DefaultMaxMessageLength = 900
)

// Provider implements the notification.Provider interface for iMessage on macOS.
type Provider struct {
	enabled          bool
	target           string
	maxMessageLength int
}

// New creates a new iMessage provider with the specified settings.
func New(enabled bool, target string) *Provider {
	return &Provider{
		enabled:          enabled,
		target:           target,
		maxMessageLength: DefaultMaxMessageLength,
	}
}

// NewFromConfig creates a new iMessage provider from configuration.
func NewFromConfig(cfg *config.IMessageConfig) *Provider {
	return &Provider{
		enabled:          cfg.Enabled,
		target:           cfg.Target,
		maxMessageLength: DefaultMaxMessageLength,
	}
}

// Name returns the unique identifier for this provider.
func (p *Provider) Name() string {
	return ProviderName
}

// Enabled returns whether this provider is currently enabled and configured.
func (p *Provider) Enabled() bool {
	return p.enabled && p.target != ""
}

// SetMaxMessageLength sets the maximum message length for truncation.
func (p *Provider) SetMaxMessageLength(length int) {
	if length > 0 {
		p.maxMessageLength = length
	}
}

// Send sends a notification message via iMessage.
// It formats the message and executes AppleScript to send it through Messages.app.
func (p *Provider) Send(ctx context.Context, msg notification.NotificationMessage) error {
	if !p.Enabled() {
		return fmt.Errorf("imessage provider is not enabled or target is not configured")
	}

	// Check for context cancellation before proceeding
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Format the message
	formattedMsg := p.formatMessage(msg)

	// Build and execute the AppleScript
	script := p.buildAppleScript(formattedMsg)

	// Create the command with context
	cmd := exec.CommandContext(ctx, "osascript", "-e", script)

	// Execute the command
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Check if it was a context cancellation
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// Provide descriptive error messages for common failures
		outputStr := strings.TrimSpace(string(output))
		if strings.Contains(outputStr, "Application isn't running") ||
			strings.Contains(outputStr, "not running") {
			logging.Error("iMessage send failed",
				"reason", "Messages.app not running",
				"target", p.target)
			return fmt.Errorf("messages.app is not running: please open Messages.app to send iMessage notifications")
		}
		if strings.Contains(outputStr, "Can't get buddy") ||
			strings.Contains(outputStr, "Invalid index") {
			logging.Error("iMessage send failed",
				"reason", "invalid target",
				"target", p.target,
				"output", outputStr)
			return fmt.Errorf("invalid iMessage target %q: ensure the phone number or email is correct and registered with iMessage", p.target)
		}
		if strings.Contains(outputStr, "iMessage") && strings.Contains(outputStr, "service") {
			logging.Error("iMessage send failed",
				"reason", "service unavailable",
				"output", outputStr)
			return fmt.Errorf("iMessage service not available: ensure you are signed into iMessage in Messages.app")
		}

		logging.Error("iMessage send failed",
			"error", err,
			"output", outputStr)
		return fmt.Errorf("failed to send iMessage: %w (output: %s)", err, outputStr)
	}

	logging.Debug("iMessage sent", "target", p.target)
	return nil
}

// formatMessage creates the notification message text from the NotificationMessage.
// Format: "[State] - Summary\nProject: folder_name\nRequest: truncated_prompt"
// Optimized for mobile display with concise output.
func (p *Provider) formatMessage(msg notification.NotificationMessage) string {
	var builder strings.Builder

	// Sanitize summary as safety net (strip any markdown that slipped through)
	sanitizedSummary := evaluator.SanitizeSummary(msg.Summary)

	// State and summary line
	stateStr := strings.ToUpper(string(msg.State))
	builder.WriteString(fmt.Sprintf("[%s] - %s", stateStr, sanitizedSummary))

	// Project line (just folder name, not full path)
	if msg.Cwd != "" {
		projectName := extractProjectName(msg.Cwd)
		builder.WriteString(fmt.Sprintf("\nProject: %s", projectName))
	}

	// Request line (last user prompt) - only if meaningful
	if msg.Request != "" && msg.Request != "[Continued session]" {
		builder.WriteString(fmt.Sprintf("\nRequest: %s", msg.Request))
	}

	result := builder.String()

	// Final truncation using smart truncation (no ellipsis)
	if len(result) > p.maxMessageLength {
		result = smartTruncate(result, p.maxMessageLength)
	}

	return result
}

// smartTruncate truncates text to maxLen by finding the previous sentence boundary
// (period or line break) rather than using ellipsis mid-sentence.
func smartTruncate(s string, maxLen int) string {
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

// extractProjectName gets just the folder name from a full path.
func extractProjectName(path string) string {
	// Get the last component of the path
	if path == "" {
		return ""
	}

	// Handle trailing slashes
	path = strings.TrimSuffix(path, "/")
	path = strings.TrimSuffix(path, "\\")

	// Find the last separator
	if idx := strings.LastIndexAny(path, "/\\"); idx >= 0 {
		return path[idx+1:]
	}

	return path
}

// buildAppleScript creates the AppleScript command to send an iMessage.
func (p *Provider) buildAppleScript(message string) string {
	// Escape special characters for AppleScript string
	escapedMessage := escapeAppleScriptString(message)
	escapedTarget := escapeAppleScriptString(p.target)

	return fmt.Sprintf(`tell application "Messages"
	set targetService to 1st service whose service type = iMessage
	set targetBuddy to buddy "%s" of targetService
	send "%s" to targetBuddy
end tell`, escapedTarget, escapedMessage)
}

// escapeAppleScriptString escapes special characters for use in AppleScript strings.
func escapeAppleScriptString(s string) string {
	// Escape backslashes first, then quotes
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	// Replace newlines with AppleScript line break
	s = strings.ReplaceAll(s, "\n", "\" & return & \"")
	return s
}

// Ensure Provider implements notification.Provider at compile time.
var _ notification.Provider = (*Provider)(nil)
