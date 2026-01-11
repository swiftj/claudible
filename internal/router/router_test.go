package router

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/swiftj/claudible/internal/config"
	"github.com/swiftj/claudible/internal/hook"
	"github.com/swiftj/claudible/internal/notification"
	"github.com/swiftj/claudible/internal/state"
	"github.com/swiftj/claudible/internal/transcript"
)

// mockProvider is a test double for notification.Provider.
type mockProvider struct {
	name      string
	enabled   bool
	sendErr   error
	sendCalls []notification.NotificationMessage
}

func newMockProvider(name string, enabled bool) *mockProvider {
	return &mockProvider{
		name:      name,
		enabled:   enabled,
		sendCalls: make([]notification.NotificationMessage, 0),
	}
}

func (m *mockProvider) Name() string {
	return m.name
}

func (m *mockProvider) Enabled() bool {
	return m.enabled
}

func (m *mockProvider) Send(ctx context.Context, msg notification.NotificationMessage) error {
	m.sendCalls = append(m.sendCalls, msg)
	return m.sendErr
}

func (m *mockProvider) withError(err error) *mockProvider {
	m.sendErr = err
	return m
}

// Helper to create a temporary transcript file for testing.
func createTempTranscript(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "transcript.jsonl")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to create temp transcript: %v", err)
	}
	return path
}

func TestNewRouter(t *testing.T) {
	t.Run("with nil config uses defaults", func(t *testing.T) {
		r := NewRouter(nil)

		if r.config == nil {
			t.Error("expected config to be set")
		}
		if r.classifier == nil {
			t.Error("expected classifier to be set")
		}
		if r.registry == nil {
			t.Error("expected registry to be set")
		}
	})

	t.Run("with custom config", func(t *testing.T) {
		cfg := config.Default()
		cfg.Behavior.NotifyOn = []string{"complete"}

		r := NewRouterForTesting(cfg)

		if r.config != cfg {
			t.Error("expected custom config to be used")
		}
	})

	t.Run("registers all providers", func(t *testing.T) {
		cfg := config.Default()
		r := NewRouterForTesting(cfg)

		// Should have 6 providers registered (imessage, http, sms, desktop, pushover, pushbullet)
		if r.registry.Count() != 6 {
			t.Errorf("expected 6 providers, got %d", r.registry.Count())
		}
	})
}

func TestRouter_Process(t *testing.T) {
	t.Run("returns error for nil input", func(t *testing.T) {
		cfg := config.Default()
		r := NewRouterForTesting(cfg)

		err := r.Process(context.Background(), nil)
		if err == nil {
			t.Error("expected error for nil input")
		}
		if err.Error() != "router: nil hook input" {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("returns error for missing transcript", func(t *testing.T) {
		cfg := config.Default()
		r := NewRouterForTesting(cfg)

		input := &hook.HookInput{
			SessionID:      "test-session",
			TranscriptPath: "/nonexistent/path/transcript.jsonl",
			HookEventName:  "Stop",
		}

		err := r.Process(context.Background(), input)
		if err == nil {
			t.Error("expected error for missing transcript")
		}
	})

	t.Run("skips notification when state not in notify_on", func(t *testing.T) {
		cfg := config.Default()
		cfg.Behavior.NotifyOn = []string{"complete"} // Only notify on complete

		r := NewRouterForTesting(cfg)

		// Replace registry with mock providers
		r.registry = notification.NewProviderRegistry()
		mock := newMockProvider("mock", true)
		r.registry.Register(mock)

		// Create transcript that will result in "waiting" state
		transcriptContent := `{"role":"user","content":"test"}
{"role":"assistant","content":"I need more info"}`
		transcriptPath := createTempTranscript(t, transcriptContent)

		input := &hook.HookInput{
			SessionID:        "test-session",
			TranscriptPath:   transcriptPath,
			HookEventName:    "Notification",
			NotificationType: "idle_prompt", // This produces "waiting" state
		}

		err := r.Process(context.Background(), input)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		// Should not have called Send since "waiting" is not in notify_on
		if len(mock.sendCalls) != 0 {
			t.Errorf("expected 0 send calls, got %d", len(mock.sendCalls))
		}
	})

	t.Run("sends notification to enabled providers", func(t *testing.T) {
		cfg := config.Default()
		cfg.Behavior.NotifyOn = []string{"complete"}

		r := NewRouterForTesting(cfg)

		// Replace registry with mock providers
		r.registry = notification.NewProviderRegistry()
		enabledMock := newMockProvider("enabled", true)
		disabledMock := newMockProvider("disabled", false)
		r.registry.Register(enabledMock)
		r.registry.Register(disabledMock)

		// Create transcript that will result in "complete" state
		transcriptContent := `{"role":"user","content":"build a feature"}
{"role":"assistant","content":"Task completed successfully"}`
		transcriptPath := createTempTranscript(t, transcriptContent)

		input := &hook.HookInput{
			SessionID:      "test-session",
			Cwd:            "/test/project",
			TranscriptPath: transcriptPath,
			HookEventName:  "Stop",
		}

		err := r.Process(context.Background(), input)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		// Enabled provider should have received the message
		if len(enabledMock.sendCalls) != 1 {
			t.Errorf("expected 1 send call to enabled provider, got %d", len(enabledMock.sendCalls))
		}

		// Disabled provider should not have received any messages
		if len(disabledMock.sendCalls) != 0 {
			t.Errorf("expected 0 send calls to disabled provider, got %d", len(disabledMock.sendCalls))
		}

		// Verify message content
		if len(enabledMock.sendCalls) > 0 {
			msg := enabledMock.sendCalls[0]
			if msg.State != state.StateComplete {
				t.Errorf("expected state complete, got %s", msg.State)
			}
			if msg.SessionID != "test-session" {
				t.Errorf("expected session ID test-session, got %s", msg.SessionID)
			}
			if msg.Cwd != "/test/project" {
				t.Errorf("expected cwd /test/project, got %s", msg.Cwd)
			}
			if msg.Request != "build a feature" {
				t.Errorf("expected request 'build a feature', got %s", msg.Request)
			}
		}
	})

	t.Run("handles provider errors gracefully", func(t *testing.T) {
		cfg := config.Default()
		cfg.Behavior.NotifyOn = []string{"complete"}

		r := NewRouterForTesting(cfg)

		// Replace registry with mock providers
		r.registry = notification.NewProviderRegistry()
		failingMock := newMockProvider("failing", true).withError(errors.New("send failed"))
		succeedingMock := newMockProvider("succeeding", true)
		r.registry.Register(failingMock)
		r.registry.Register(succeedingMock)

		transcriptContent := `{"role":"user","content":"test"}
{"role":"assistant","content":"Done"}`
		transcriptPath := createTempTranscript(t, transcriptContent)

		input := &hook.HookInput{
			SessionID:        "test-session",
			TranscriptPath:   transcriptPath,
			HookEventName: "Stop",
		}

		// Should not return error since at least one provider succeeded
		err := r.Process(context.Background(), input)
		if err != nil {
			t.Errorf("expected no error when some providers succeed, got: %v", err)
		}

		// Both providers should have been called
		if len(failingMock.sendCalls) != 1 {
			t.Errorf("expected 1 send call to failing provider, got %d", len(failingMock.sendCalls))
		}
		if len(succeedingMock.sendCalls) != 1 {
			t.Errorf("expected 1 send call to succeeding provider, got %d", len(succeedingMock.sendCalls))
		}
	})

	t.Run("returns error when all providers fail", func(t *testing.T) {
		cfg := config.Default()
		cfg.Behavior.NotifyOn = []string{"complete"}

		r := NewRouterForTesting(cfg)

		// Replace registry with only failing providers
		r.registry = notification.NewProviderRegistry()
		failing1 := newMockProvider("failing1", true).withError(errors.New("error 1"))
		failing2 := newMockProvider("failing2", true).withError(errors.New("error 2"))
		r.registry.Register(failing1)
		r.registry.Register(failing2)

		transcriptContent := `{"role":"user","content":"test"}
{"role":"assistant","content":"Done"}`
		transcriptPath := createTempTranscript(t, transcriptContent)

		input := &hook.HookInput{
			SessionID:        "test-session",
			TranscriptPath:   transcriptPath,
			HookEventName: "Stop",
		}

		err := r.Process(context.Background(), input)
		if err == nil {
			t.Error("expected error when all providers fail")
		}
	})

	t.Run("respects context cancellation", func(t *testing.T) {
		cfg := config.Default()
		r := NewRouterForTesting(cfg)

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		input := &hook.HookInput{
			SessionID:        "test-session",
			TranscriptPath:   "/some/path",
			HookEventName: "Stop",
		}

		err := r.Process(ctx, input)
		if !errors.Is(err, context.Canceled) {
			t.Errorf("expected context.Canceled error, got: %v", err)
		}
	})
}

func TestRouter_buildMessage(t *testing.T) {
	cfg := config.Default()
	r := NewRouterForTesting(cfg)

	t.Run("builds message with all fields", func(t *testing.T) {
		transcriptContent := `{"role":"user","content":"please fix the bug"}
{"role":"assistant","content":"I've fixed the bug in auth.go"}`
		transcriptPath := createTempTranscript(t, transcriptContent)

		trans, err := transcript.Parse(transcriptPath)
		if err != nil {
			t.Fatalf("failed to parse transcript: %v", err)
		}

		input := &hook.HookInput{
			SessionID:        "session-123",
			Cwd:              "/home/user/project",
			TranscriptPath:   transcriptPath,
			HookEventName: "Stop",
		}

		msg := r.buildMessage(input, trans, state.StateComplete)

		if msg.Title != "Claude Code - Complete" {
			t.Errorf("expected title 'Claude Code - Complete', got %s", msg.Title)
		}
		if msg.State != state.StateComplete {
			t.Errorf("expected state complete, got %s", msg.State)
		}
		if msg.SessionID != "session-123" {
			t.Errorf("expected session ID 'session-123', got %s", msg.SessionID)
		}
		if msg.Cwd != "/home/user/project" {
			t.Errorf("expected cwd '/home/user/project', got %s", msg.Cwd)
		}
		if msg.Request != "please fix the bug" {
			t.Errorf("expected request 'please fix the bug', got %s", msg.Request)
		}
		if msg.Timestamp.IsZero() {
			t.Error("expected timestamp to be set")
		}
	})

	t.Run("handles nil input gracefully", func(t *testing.T) {
		msg := r.buildMessage(nil, nil, state.StateIdle)

		if msg.Title != "Claude Code - Idle" {
			t.Errorf("expected title 'Claude Code - Idle', got %s", msg.Title)
		}
		if msg.SessionID != "" {
			t.Errorf("expected empty session ID, got %s", msg.SessionID)
		}
		if msg.Cwd != "" {
			t.Errorf("expected empty cwd, got %s", msg.Cwd)
		}
		if msg.Request != "" {
			t.Errorf("expected empty request, got %s", msg.Request)
		}
	})

	t.Run("generates correct titles for each state", func(t *testing.T) {
		testCases := []struct {
			state         state.State
			expectedTitle string
		}{
			{state.StateComplete, "Claude Code - Complete"},
			{state.StateWaiting, "Claude Code - Waiting"},
			{state.StateStopped, "Claude Code - Stopped"},
			{state.StateIdle, "Claude Code - Idle"},
		}

		for _, tc := range testCases {
			msg := r.buildMessage(nil, nil, tc.state)
			if msg.Title != tc.expectedTitle {
				t.Errorf("for state %s: expected title %q, got %q", tc.state, tc.expectedTitle, msg.Title)
			}
		}
	})
}

func TestTruncateSummary(t *testing.T) {
	t.Run("returns string unchanged if under limit", func(t *testing.T) {
		input := "short string"
		result := truncateSummary(input, 100)
		if result != input {
			t.Errorf("expected %q, got %q", input, result)
		}
	})

	t.Run("truncates long string at word boundary (no ellipsis)", func(t *testing.T) {
		input := "This is a very long string that should be truncated at some point"
		result := truncateSummary(input, 30)
		if len(result) > 30 {
			t.Errorf("result too long: %d chars, got %q", len(result), result)
		}
		// Smart truncation should NOT use ellipsis
		if strings.HasSuffix(result, "...") {
			t.Errorf("should not use ellipsis, got %q", result)
		}
	})

	t.Run("truncates at sentence boundary when period is in second half", func(t *testing.T) {
		// Period is at position 40, limit is 50, so period is at 80% (> 50% threshold)
		input := "This is a longer first sentence that ends here. Then more text follows."
		result := truncateSummary(input, 50)
		// Should truncate at the period since it's past the midpoint
		if len(result) > 50 {
			t.Errorf("result too long: %d chars, got %q", len(result), result)
		}
		if !strings.HasSuffix(result, ".") {
			t.Errorf("expected truncation at sentence boundary (ending with period), got %q", result)
		}
	})

	t.Run("trims whitespace", func(t *testing.T) {
		input := "  spaced   content  "
		result := truncateSummary(input, 100)
		if result != "spaced content" {
			t.Errorf("expected %q, got %q", "spaced content", result)
		}
	})

	t.Run("uses default when maxLen is zero", func(t *testing.T) {
		input := "test"
		result := truncateSummary(input, 0)
		if result != input {
			t.Errorf("expected %q, got %q", input, result)
		}
	})
}

func TestRouter_handleProviderErrors(t *testing.T) {
	cfg := config.Default()

	t.Run("returns nil for no errors", func(t *testing.T) {
		r := NewRouterForTesting(cfg)
		r.registry = notification.NewProviderRegistry()
		mock := newMockProvider("test", true)
		r.registry.Register(mock)

		err := r.handleProviderErrors(map[string]error{})
		if err != nil {
			t.Errorf("expected nil, got %v", err)
		}
	})

	t.Run("returns nil when some providers succeed", func(t *testing.T) {
		r := NewRouterForTesting(cfg)
		r.registry = notification.NewProviderRegistry()
		r.registry.Register(newMockProvider("p1", true))
		r.registry.Register(newMockProvider("p2", true))

		// Only one provider failed
		errs := map[string]error{
			"p1": errors.New("failed"),
		}

		err := r.handleProviderErrors(errs)
		if err != nil {
			t.Errorf("expected nil when some succeed, got %v", err)
		}
	})

	t.Run("returns error when all providers fail", func(t *testing.T) {
		r := NewRouterForTesting(cfg)
		r.registry = notification.NewProviderRegistry()
		r.registry.Register(newMockProvider("p1", true))
		r.registry.Register(newMockProvider("p2", true))

		// All providers failed
		errs := map[string]error{
			"p1": errors.New("error 1"),
			"p2": errors.New("error 2"),
		}

		err := r.handleProviderErrors(errs)
		if err == nil {
			t.Error("expected error when all providers fail")
		}
	})
}

func TestRouter_Accessors(t *testing.T) {
	cfg := config.Default()
	r := NewRouterForTesting(cfg)

	t.Run("Registry returns registry", func(t *testing.T) {
		if r.Registry() == nil {
			t.Error("expected registry to be non-nil")
		}
		if r.Registry() != r.registry {
			t.Error("expected Registry() to return internal registry")
		}
	})

	t.Run("Config returns config", func(t *testing.T) {
		if r.Config() != cfg {
			t.Error("expected Config() to return the configuration")
		}
	})

	t.Run("Classifier returns classifier", func(t *testing.T) {
		if r.Classifier() == nil {
			t.Error("expected classifier to be non-nil")
		}
	})
}

// Integration test with real transcript parsing.
func TestRouter_Integration(t *testing.T) {
	t.Run("full flow with real transcript parsing", func(t *testing.T) {
		cfg := config.Default()
		cfg.Behavior.NotifyOn = []string{"complete", "waiting"}

		r := NewRouterForTesting(cfg)

		// Replace with mock provider
		r.registry = notification.NewProviderRegistry()
		mock := newMockProvider("mock", true)
		r.registry.Register(mock)

		// Create a real transcript
		transcriptContent := `{"role":"user","content":"Implement user authentication"}
{"role":"assistant","content":"I'll implement user authentication for you."}
{"role":"user","content":"Great, please continue"}
{"role":"assistant","content":"I've completed the implementation. All tests pass and the authentication system is working correctly."}`
		transcriptPath := createTempTranscript(t, transcriptContent)

		input := &hook.HookInput{
			SessionID:        "integration-test",
			Cwd:              "/test/auth-project",
			TranscriptPath:   transcriptPath,
			HookEventName: "Stop",
		}

		err := r.Process(context.Background(), input)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		// Verify the notification was sent
		if len(mock.sendCalls) != 1 {
			t.Fatalf("expected 1 send call, got %d", len(mock.sendCalls))
		}

		msg := mock.sendCalls[0]
		if msg.State != state.StateComplete {
			t.Errorf("expected complete state, got %s", msg.State)
		}
		if msg.Request != "Great, please continue" {
			t.Errorf("expected last user prompt in request, got %s", msg.Request)
		}
		if !strings.Contains(msg.Summary, "completed") {
			t.Errorf("expected summary to contain completion info, got %s", msg.Summary)
		}
	})
}

// Benchmark for router processing.
func BenchmarkRouter_Process(b *testing.B) {
	cfg := config.Default()
	cfg.Behavior.NotifyOn = []string{"complete"}

	r := NewRouterForTesting(cfg)

	// Use a no-op mock provider
	r.registry = notification.NewProviderRegistry()
	r.registry.Register(newMockProvider("bench", true))

	// Create transcript file once
	transcriptContent := `{"role":"user","content":"test request"}
{"role":"assistant","content":"test response completed"}`
	dir := b.TempDir()
	transcriptPath := filepath.Join(dir, "transcript.jsonl")
	if err := os.WriteFile(transcriptPath, []byte(transcriptContent), 0644); err != nil {
		b.Fatal(err)
	}

	input := &hook.HookInput{
		SessionID:        "bench-session",
		Cwd:              "/bench",
		TranscriptPath:   transcriptPath,
		HookEventName: "Stop",
	}

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = r.Process(ctx, input)
	}
}

// Test timeout behavior.
func TestRouter_Timeout(t *testing.T) {
	cfg := config.Default()
	cfg.Behavior.NotifyOn = []string{"complete"}

	r := NewRouterForTesting(cfg)

	// Create a slow provider
	r.registry = notification.NewProviderRegistry()
	slowProvider := &slowMockProvider{
		name:    "slow",
		enabled: true,
		delay:   100 * time.Millisecond,
	}
	r.registry.Register(slowProvider)

	transcriptContent := `{"role":"user","content":"test"}
{"role":"assistant","content":"Done"}`
	transcriptPath := createTempTranscript(t, transcriptContent)

	input := &hook.HookInput{
		SessionID:        "timeout-test",
		TranscriptPath:   transcriptPath,
		HookEventName: "Stop",
	}

	// Use a very short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	err := r.Process(ctx, input)
	// The router should check context before parsing, so it might succeed or fail
	// depending on timing - we just want to ensure no panic
	_ = err
}

type slowMockProvider struct {
	name    string
	enabled bool
	delay   time.Duration
}

func (p *slowMockProvider) Name() string {
	return p.name
}

func (p *slowMockProvider) Enabled() bool {
	return p.enabled
}

func (p *slowMockProvider) Send(ctx context.Context, msg notification.NotificationMessage) error {
	select {
	case <-time.After(p.delay):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Tests for evaluator integration
func TestRouter_classifyState(t *testing.T) {
	t.Run("uses heuristics when evaluator disabled", func(t *testing.T) {
		cfg := config.Default()
		cfg.Evaluator.Enabled = false
		r := NewRouterForTesting(cfg)

		// Create transcript
		transcriptContent := `{"role":"user","content":"fix the bug"}
{"role":"assistant","content":"I've completed the fix."}`
		transcriptPath := createTempTranscript(t, transcriptContent)
		trans, _ := transcript.Parse(transcriptPath)

		input := &hook.HookInput{
			HookEventName: "Stop",
		}

		classifiedState, summary := r.classifyState(context.Background(), input, trans)

		// Should use heuristic classification (complete due to "completed" in response)
		if classifiedState != state.StateComplete {
			t.Errorf("expected StateComplete, got %v", classifiedState)
		}
		// Summary should be empty (no LLM summary)
		if summary != "" {
			t.Errorf("expected empty summary when evaluator disabled, got %q", summary)
		}
	})

	t.Run("uses heuristics when evaluator has no API key", func(t *testing.T) {
		cfg := config.Default()
		cfg.Evaluator.Enabled = true
		cfg.Evaluator.Provider = "anthropic"
		cfg.Evaluator.APIKey = "" // No API key
		r := NewRouterForTesting(cfg)

		transcriptContent := `{"role":"user","content":"help me"}
{"role":"assistant","content":"Here's the answer"}`
		transcriptPath := createTempTranscript(t, transcriptContent)
		trans, _ := transcript.Parse(transcriptPath)

		input := &hook.HookInput{
			HookEventName:    "Notification",
			NotificationType: "idle_prompt",
		}

		classifiedState, summary := r.classifyState(context.Background(), input, trans)

		// Should use heuristic classification (waiting due to idle_prompt)
		if classifiedState != state.StateWaiting {
			t.Errorf("expected StateWaiting, got %v", classifiedState)
		}
		if summary != "" {
			t.Errorf("expected empty summary, got %q", summary)
		}
	})

	t.Run("uses heuristics when transcript is nil", func(t *testing.T) {
		cfg := config.Default()
		cfg.Evaluator.Enabled = true
		cfg.Evaluator.Provider = "anthropic"
		cfg.Evaluator.APIKey = "test-key"
		r := NewRouterForTesting(cfg)

		input := &hook.HookInput{
			HookEventName: "Stop",
		}

		classifiedState, summary := r.classifyState(context.Background(), input, nil)

		// Should use heuristic classification
		if classifiedState != state.StateIdle {
			t.Errorf("expected StateIdle for nil transcript, got %v", classifiedState)
		}
		if summary != "" {
			t.Errorf("expected empty summary, got %q", summary)
		}
	})
}

func TestRouter_mapEvaluatorState(t *testing.T) {
	cfg := config.Default()
	r := NewRouterForTesting(cfg)

	tests := []struct {
		evalState string
		expected  state.State
	}{
		{"complete", state.StateComplete},
		{"waiting", state.StateWaiting},
		{"needs_review", state.StateComplete}, // Maps to complete
		{"unknown", state.StateIdle},
		{"", state.StateIdle},
	}

	for _, tt := range tests {
		t.Run(tt.evalState, func(t *testing.T) {
			result := r.mapEvaluatorState(tt.evalState)
			if result != tt.expected {
				t.Errorf("mapEvaluatorState(%q) = %v, want %v", tt.evalState, result, tt.expected)
			}
		})
	}
}

func TestRouter_buildMessageWithSummary(t *testing.T) {
	cfg := config.Default()
	r := NewRouterForTesting(cfg)

	t.Run("uses LLM summary when provided", func(t *testing.T) {
		transcriptContent := `{"role":"user","content":"test"}
{"role":"assistant","content":"long response here"}`
		transcriptPath := createTempTranscript(t, transcriptContent)
		trans, _ := transcript.Parse(transcriptPath)

		input := &hook.HookInput{
			SessionID: "test",
			Cwd:       "/project",
		}

		llmSummary := "AI-generated summary of the task"
		msg := r.buildMessageWithSummary(input, trans, state.StateComplete, llmSummary)

		if msg.Summary != llmSummary {
			t.Errorf("expected LLM summary %q, got %q", llmSummary, msg.Summary)
		}
	})

	t.Run("falls back to generated summary when LLM summary empty", func(t *testing.T) {
		transcriptContent := `{"role":"user","content":"test"}
{"role":"assistant","content":"the response content"}`
		transcriptPath := createTempTranscript(t, transcriptContent)
		trans, _ := transcript.Parse(transcriptPath)

		input := &hook.HookInput{
			SessionID: "test",
			Cwd:       "/project",
		}

		msg := r.buildMessageWithSummary(input, trans, state.StateComplete, "")

		// Should contain the assistant response
		if !strings.Contains(msg.Summary, "response content") {
			t.Errorf("expected summary to contain transcript content, got %q", msg.Summary)
		}
	})
}

func TestRouter_Evaluator(t *testing.T) {
	t.Run("evaluator is created from config", func(t *testing.T) {
		cfg := config.Default()
		cfg.Evaluator.Enabled = true
		cfg.Evaluator.Provider = "anthropic"
		cfg.Evaluator.APIKey = "test-key"
		cfg.Evaluator.Model = "claude-3-haiku-20240307"

		r := NewRouterForTesting(cfg)

		if r.evaluator == nil {
			t.Error("expected evaluator to be created")
		}
		if !r.evaluator.Enabled() {
			t.Error("expected evaluator to be enabled")
		}
	})

	t.Run("evaluator is disabled when config disabled", func(t *testing.T) {
		cfg := config.Default()
		cfg.Evaluator.Enabled = false

		r := NewRouterForTesting(cfg)

		if r.evaluator == nil {
			t.Error("expected evaluator to be created (even if disabled)")
		}
		if r.evaluator.Enabled() {
			t.Error("expected evaluator to be disabled")
		}
	})
}
