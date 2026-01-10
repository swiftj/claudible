// Package hook provides types and utilities for handling Claude Code hook events.
package hook

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// NotificationType represents the type of hook notification.
type NotificationType string

const (
	// NotificationTypeIdlePrompt indicates the agent is idle and prompting for input.
	NotificationTypeIdlePrompt NotificationType = "idle_prompt"

	// NotificationTypeStop indicates the agent has stopped.
	NotificationTypeStop NotificationType = "stop"
)

// String returns the string representation of the notification type.
func (n NotificationType) String() string {
	return string(n)
}

// IsValid checks if the notification type is a recognized value.
func (n NotificationType) IsValid() bool {
	switch n {
	case NotificationTypeIdlePrompt, NotificationTypeStop:
		return true
	default:
		return false
	}
}

// HookInput represents the JSON payload from Claude Code hooks via stdin.
// Claude Code sends this data when hook events are triggered.
type HookInput struct {
	// SessionID is the unique identifier for the Claude Code session.
	SessionID string `json:"session_id"`

	// Cwd is the current working directory of the Claude Code session.
	Cwd string `json:"cwd"`

	// TranscriptPath is the path to the session transcript file.
	TranscriptPath string `json:"transcript_path"`

	// NotificationType indicates the type of hook event (idle_prompt, stop).
	NotificationType string `json:"notification_type"`
}

// GetNotificationType returns the notification type as a typed value.
func (h *HookInput) GetNotificationType() NotificationType {
	return NotificationType(h.NotificationType)
}

// Validate checks if the HookInput has all required fields.
func (h *HookInput) Validate() error {
	if h.SessionID == "" {
		return fmt.Errorf("session_id is required")
	}
	if h.TranscriptPath == "" {
		return fmt.Errorf("transcript_path is required")
	}
	if h.NotificationType == "" {
		return fmt.Errorf("notification_type is required")
	}
	if !h.GetNotificationType().IsValid() {
		return fmt.Errorf("invalid notification_type: %q", h.NotificationType)
	}
	return nil
}

// ParseHookInput reads and parses the hook input JSON from the given reader.
func ParseHookInput(r io.Reader) (*HookInput, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("failed to read input: %w", err)
	}

	if len(data) == 0 {
		return nil, fmt.Errorf("empty input")
	}

	var input HookInput
	if err := json.Unmarshal(data, &input); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	return &input, nil
}

// ParseHookInputFromStdin reads and parses the hook input JSON from stdin.
func ParseHookInputFromStdin() (*HookInput, error) {
	return ParseHookInput(os.Stdin)
}

// ParseHookInputFromBytes parses the hook input JSON from a byte slice.
func ParseHookInputFromBytes(data []byte) (*HookInput, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty input")
	}

	var input HookInput
	if err := json.Unmarshal(data, &input); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	return &input, nil
}
