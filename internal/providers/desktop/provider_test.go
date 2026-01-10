package desktop

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/johnswift/claudible/internal/config"
	"github.com/johnswift/claudible/internal/notification"
	"github.com/johnswift/claudible/internal/state"
)

// MockExecutor is a mock implementation of CommandExecutor for testing.
type MockExecutor struct {
	ExecutedCommands []ExecutedCommand
	ReturnError      error
}

// ExecutedCommand records a command that was executed.
type ExecutedCommand struct {
	Name string
	Args []string
}

// Execute records the command and returns the configured error.
func (m *MockExecutor) Execute(_ context.Context, name string, args ...string) error {
	m.ExecutedCommands = append(m.ExecutedCommands, ExecutedCommand{
		Name: name,
		Args: args,
	})
	return m.ReturnError
}

func TestNew(t *testing.T) {
	tests := []struct {
		name    string
		enabled bool
	}{
		{"enabled provider", true},
		{"disabled provider", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := New(tt.enabled)
			if p.Enabled() != tt.enabled {
				t.Errorf("Expected enabled=%v, got %v", tt.enabled, p.Enabled())
			}
			if p.Name() != "desktop" {
				t.Errorf("Expected name='desktop', got %q", p.Name())
			}
		})
	}
}

func TestNewFromConfig(t *testing.T) {
	cfg := &config.DesktopConfig{Enabled: true}
	p := NewFromConfig(cfg)

	if !p.Enabled() {
		t.Error("Expected provider to be enabled")
	}
	if p.Name() != "desktop" {
		t.Errorf("Expected name='desktop', got %q", p.Name())
	}
}

func TestWithExecutor(t *testing.T) {
	mock := &MockExecutor{}
	p := New(true, WithExecutor(mock))

	// The executor should be our mock
	if p.executor != mock {
		t.Error("Expected custom executor to be set")
	}
}

func TestProvider_Name(t *testing.T) {
	p := New(true)
	if p.Name() != "desktop" {
		t.Errorf("Expected 'desktop', got %q", p.Name())
	}
}

func TestProvider_Enabled(t *testing.T) {
	tests := []struct {
		name    string
		enabled bool
		want    bool
	}{
		{"enabled", true, true},
		{"disabled", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := New(tt.enabled)
			if got := p.Enabled(); got != tt.want {
				t.Errorf("Enabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestProvider_Send_Disabled(t *testing.T) {
	mock := &MockExecutor{}
	p := New(false, WithExecutor(mock))

	msg := notification.NotificationMessage{
		State:   state.StateComplete,
		Summary: "Task completed",
	}

	err := p.Send(context.Background(), msg)
	if err != nil {
		t.Errorf("Expected no error for disabled provider, got: %v", err)
	}

	if len(mock.ExecutedCommands) > 0 {
		t.Error("Expected no commands to be executed when provider is disabled")
	}
}

func TestProvider_Send_ExecutorError(t *testing.T) {
	expectedErr := errors.New("command failed")
	mock := &MockExecutor{ReturnError: expectedErr}
	p := New(true, WithExecutor(mock))

	msg := notification.NotificationMessage{
		State:   state.StateComplete,
		Summary: "Task completed",
	}

	err := p.Send(context.Background(), msg)
	if err == nil {
		t.Error("Expected error from executor")
	}
	if !errors.Is(err, expectedErr) {
		t.Errorf("Expected error %v, got %v", expectedErr, err)
	}
}

func TestFormatTitle(t *testing.T) {
	tests := []struct {
		name     string
		state    state.State
		expected string
	}{
		{"complete state", state.StateComplete, "Claude Code: Complete"},
		{"waiting state", state.StateWaiting, "Claude Code: Waiting"},
		{"stopped state", state.StateStopped, "Claude Code: Stopped"},
		{"idle state", state.StateIdle, "Claude Code: Idle"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := notification.NotificationMessage{State: tt.state}
			got := formatTitle(msg)
			if got != tt.expected {
				t.Errorf("formatTitle() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestFormatBody(t *testing.T) {
	tests := []struct {
		name     string
		summary  string
		expected string
	}{
		{
			name:     "short summary",
			summary:  "Task completed successfully",
			expected: "Task completed successfully",
		},
		{
			name:     "empty summary",
			summary:  "",
			expected: "",
		},
		{
			name:     "exactly max length",
			summary:  strings.Repeat("a", maxSummaryLength),
			expected: strings.Repeat("a", maxSummaryLength),
		},
		{
			name:     "over max length - truncated with ellipsis",
			summary:  strings.Repeat("a", maxSummaryLength+50),
			expected: strings.Repeat("a", maxSummaryLength-3) + "...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := notification.NotificationMessage{Summary: tt.summary}
			got := formatBody(msg)
			if got != tt.expected {
				t.Errorf("formatBody() length = %d, want %d", len(got), len(tt.expected))
			}
		})
	}
}

func TestEscapeAppleScript(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "no special characters",
			input:    "Hello World",
			expected: "Hello World",
		},
		{
			name:     "double quotes",
			input:    `Say "Hello"`,
			expected: `Say \"Hello\"`,
		},
		{
			name:     "backslashes",
			input:    `Path\to\file`,
			expected: `Path\\to\\file`,
		},
		{
			name:     "mixed special characters",
			input:    `He said "Hello\" to all`,
			expected: `He said \"Hello\\\" to all`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := escapeAppleScript(tt.input)
			if got != tt.expected {
				t.Errorf("escapeAppleScript(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestProvider_ImplementsInterface(t *testing.T) {
	var _ notification.Provider = (*Provider)(nil)
}

func TestDefaultExecutor_Execute(t *testing.T) {
	// Test with a simple command that should succeed on most systems
	executor := &DefaultExecutor{}
	err := executor.Execute(context.Background(), "echo", "test")
	if err != nil {
		t.Errorf("Expected echo command to succeed, got: %v", err)
	}
}

func TestDefaultExecutor_Execute_InvalidCommand(t *testing.T) {
	executor := &DefaultExecutor{}
	err := executor.Execute(context.Background(), "nonexistent_command_12345")
	if err == nil {
		t.Error("Expected error for nonexistent command")
	}
}

func TestDefaultExecutor_Execute_ContextCancellation(t *testing.T) {
	executor := &DefaultExecutor{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// Use sleep to ensure context cancellation is detected
	err := executor.Execute(ctx, "sleep", "10")
	if err == nil {
		t.Error("Expected error for cancelled context")
	}
}

func TestTitleCase(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"lowercase word", "complete", "Complete"},
		{"already capitalized", "Complete", "Complete"},
		{"empty string", "", ""},
		{"single char", "a", "A"},
		{"unicode", "idle", "Idle"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := titleCase(tt.input)
			if got != tt.expected {
				t.Errorf("titleCase(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestProvider_sendMacOS(t *testing.T) {
	mock := &MockExecutor{}
	p := New(true, WithExecutor(mock))

	err := p.sendMacOS(context.Background(), "Test Title", "Test Body")
	if err != nil {
		t.Errorf("sendMacOS() error = %v", err)
	}

	if len(mock.ExecutedCommands) != 1 {
		t.Fatalf("Expected 1 command, got %d", len(mock.ExecutedCommands))
	}

	cmd := mock.ExecutedCommands[0]
	if cmd.Name != "osascript" {
		t.Errorf("Expected command 'osascript', got %q", cmd.Name)
	}

	if len(cmd.Args) != 2 || cmd.Args[0] != "-e" {
		t.Errorf("Expected args [-e, script], got %v", cmd.Args)
	}

	expectedScript := `display notification "Test Body" with title "Test Title"`
	if cmd.Args[1] != expectedScript {
		t.Errorf("Expected script %q, got %q", expectedScript, cmd.Args[1])
	}
}

func TestProvider_sendMacOS_WithSpecialChars(t *testing.T) {
	mock := &MockExecutor{}
	p := New(true, WithExecutor(mock))

	err := p.sendMacOS(context.Background(), `Title with "quotes"`, `Body with "quotes" and \backslash`)
	if err != nil {
		t.Errorf("sendMacOS() error = %v", err)
	}

	if len(mock.ExecutedCommands) != 1 {
		t.Fatalf("Expected 1 command, got %d", len(mock.ExecutedCommands))
	}

	cmd := mock.ExecutedCommands[0]
	expectedScript := `display notification "Body with \"quotes\" and \\backslash" with title "Title with \"quotes\""`
	if cmd.Args[1] != expectedScript {
		t.Errorf("Expected script %q, got %q", expectedScript, cmd.Args[1])
	}
}

func TestProvider_sendLinux(t *testing.T) {
	mock := &MockExecutor{}
	p := New(true, WithExecutor(mock))

	err := p.sendLinux(context.Background(), "Test Title", "Test Body")
	if err != nil {
		t.Errorf("sendLinux() error = %v", err)
	}

	if len(mock.ExecutedCommands) != 1 {
		t.Fatalf("Expected 1 command, got %d", len(mock.ExecutedCommands))
	}

	cmd := mock.ExecutedCommands[0]
	if cmd.Name != "notify-send" {
		t.Errorf("Expected command 'notify-send', got %q", cmd.Name)
	}

	if len(cmd.Args) != 2 {
		t.Errorf("Expected 2 args, got %d", len(cmd.Args))
	}

	if cmd.Args[0] != "Test Title" {
		t.Errorf("Expected first arg 'Test Title', got %q", cmd.Args[0])
	}

	if cmd.Args[1] != "Test Body" {
		t.Errorf("Expected second arg 'Test Body', got %q", cmd.Args[1])
	}
}

func TestProvider_Send_FullMessage(t *testing.T) {
	mock := &MockExecutor{}
	p := New(true, WithExecutor(mock))

	msg := notification.NotificationMessage{
		Title:     "Claude Code - Complete",
		State:     state.StateComplete,
		Summary:   "Successfully implemented the authentication module",
		Request:   "Add authentication to the API",
		Cwd:       "/home/user/project",
		SessionID: "abc123",
	}

	err := p.Send(context.Background(), msg)
	if err != nil {
		t.Errorf("Send() error = %v", err)
	}

	if len(mock.ExecutedCommands) != 1 {
		t.Fatalf("Expected 1 command, got %d", len(mock.ExecutedCommands))
	}

	// The command was executed successfully
	cmd := mock.ExecutedCommands[0]
	// On darwin, it should be osascript; on linux, notify-send
	// Since we can't control runtime.GOOS, we just verify a command was run
	if cmd.Name != "osascript" && cmd.Name != "notify-send" {
		t.Errorf("Expected osascript or notify-send, got %q", cmd.Name)
	}
}

func TestNewFromConfig_Disabled(t *testing.T) {
	cfg := &config.DesktopConfig{Enabled: false}
	p := NewFromConfig(cfg)

	if p.Enabled() {
		t.Error("Expected provider to be disabled")
	}
}

func TestNewFromConfig_WithExecutor(t *testing.T) {
	cfg := &config.DesktopConfig{Enabled: true}
	mock := &MockExecutor{}
	p := NewFromConfig(cfg, WithExecutor(mock))

	if !p.Enabled() {
		t.Error("Expected provider to be enabled")
	}
	if p.executor != mock {
		t.Error("Expected custom executor to be set")
	}
}
