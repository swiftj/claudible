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

	// NotificationTypePermissionPrompt indicates the agent is waiting for permission.
	NotificationTypePermissionPrompt NotificationType = "permission_prompt"

	// NotificationTypeStop indicates the agent has stopped.
	NotificationTypeStop NotificationType = "stop"
)

// HookEventName represents the type of hook event from Claude Code.
type HookEventName string

const (
	// HookEventNameStop indicates a Stop hook event.
	HookEventNameStop HookEventName = "Stop"

	// HookEventNameNotification indicates a Notification hook event.
	HookEventNameNotification HookEventName = "Notification"
)

// String returns the string representation of the notification type.
func (n NotificationType) String() string {
	return string(n)
}

// IsValid checks if the notification type is a recognized value.
func (n NotificationType) IsValid() bool {
	switch n {
	case NotificationTypeIdlePrompt, NotificationTypePermissionPrompt, NotificationTypeStop:
		return true
	default:
		return false
	}
}

// HookInput represents the JSON payload from Claude Code hooks via stdin.
// Claude Code sends this data when hook events are triggered.
//
// Claude Code sends different JSON schemas for different hook events:
//   - Stop hooks: have "hook_event_name" but no "notification_type"
//   - Notification hooks: have both "hook_event_name" and "notification_type"
type HookInput struct {
	// SessionID is the unique identifier for the Claude Code session.
	SessionID string `json:"session_id"`

	// Cwd is the current working directory of the Claude Code session.
	Cwd string `json:"cwd"`

	// TranscriptPath is the path to the session transcript file.
	TranscriptPath string `json:"transcript_path"`

	// HookEventName indicates the type of hook event (Stop, Notification, etc.).
	// This field is always present in Claude Code hook input.
	HookEventName string `json:"hook_event_name"`

	// NotificationType indicates the type of notification (idle_prompt, permission_prompt).
	// This field is only present for Notification hooks, not for Stop hooks.
	NotificationType string `json:"notification_type,omitempty"`

	// StopHookActive indicates if Claude Code is already continuing from a stop hook.
	// This is only present for Stop hooks.
	StopHookActive bool `json:"stop_hook_active,omitempty"`

	// PermissionMode indicates the current permission mode of Claude Code.
	PermissionMode string `json:"permission_mode,omitempty"`
}

// GetHookEventName returns the hook event name as a typed value.
func (h *HookInput) GetHookEventName() HookEventName {
	return HookEventName(h.HookEventName)
}

// GetNotificationType returns the notification type as a typed value.
// For Stop hooks (which don't have notification_type), it returns NotificationTypeStop.
// For Notification hooks, it returns the actual notification_type value.
func (h *HookInput) GetNotificationType() NotificationType {
	// Stop hooks don't have notification_type, derive it from hook_event_name
	if h.GetHookEventName() == HookEventNameStop {
		return NotificationTypeStop
	}
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

	// hook_event_name is always required and tells us which hook type this is
	if h.HookEventName == "" {
		return fmt.Errorf("hook_event_name is required")
	}

	// For Stop hooks, notification_type is not present (we derive it as "stop")
	// For Notification hooks, notification_type must be present and valid
	switch h.GetHookEventName() {
	case HookEventNameStop:
		// Stop hooks don't have notification_type, that's expected
		return nil
	case HookEventNameNotification:
		if h.NotificationType == "" {
			return fmt.Errorf("notification_type is required for Notification hooks")
		}
		if !h.GetNotificationType().IsValid() {
			return fmt.Errorf("invalid notification_type: %q", h.NotificationType)
		}
		return nil
	default:
		// Unknown hook event, allow it but log for debugging
		// This makes the system forward-compatible with new hook types
		return nil
	}
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
