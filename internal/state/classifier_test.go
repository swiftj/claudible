package state

import (
	"strings"
	"testing"

	"github.com/swiftj/claudible/internal/hook"
	"github.com/swiftj/claudible/internal/transcript"
)

// Helper to create a transcript from entries.
func makeTranscript(entries ...transcript.Entry) *transcript.Transcript {
	return &transcript.Transcript{Entries: entries}
}

// Helper to create a hook input.
// For "stop" notification type, creates a Stop hook event.
// For other types (idle_prompt, permission_prompt), creates a Notification hook event.
func makeHookInput(notificationType string) *hook.HookInput {
	hookEventName := "Notification"
	if notificationType == "stop" {
		hookEventName = "Stop"
		// Stop hooks don't have notification_type in the actual JSON from Claude Code
		notificationType = ""
	}
	return &hook.HookInput{
		SessionID:        "test-session",
		Cwd:              "/tmp",
		TranscriptPath:   "/tmp/transcript.jsonl",
		HookEventName:    hookEventName,
		NotificationType: notificationType,
	}
}

func TestNewClassifier(t *testing.T) {
	t.Run("default error patterns", func(t *testing.T) {
		c := NewClassifier()
		patterns := c.ErrorPatterns()

		if len(patterns) != len(defaultErrorPatterns) {
			t.Errorf("expected %d patterns, got %d", len(defaultErrorPatterns), len(patterns))
		}

		for i, p := range defaultErrorPatterns {
			if patterns[i] != p {
				t.Errorf("pattern %d: expected %q, got %q", i, p, patterns[i])
			}
		}
	})

	t.Run("custom error patterns", func(t *testing.T) {
		customPatterns := []string{"crash", "panic", "fatal"}
		c := NewClassifier(WithErrorPatterns(customPatterns))
		patterns := c.ErrorPatterns()

		if len(patterns) != len(customPatterns) {
			t.Errorf("expected %d patterns, got %d", len(customPatterns), len(patterns))
		}

		for i, p := range customPatterns {
			if patterns[i] != p {
				t.Errorf("pattern %d: expected %q, got %q", i, p, patterns[i])
			}
		}
	})

	t.Run("additional error patterns", func(t *testing.T) {
		additionalPatterns := []string{"crash", "panic"}
		c := NewClassifier(WithAdditionalErrorPatterns(additionalPatterns))
		patterns := c.ErrorPatterns()

		expectedLen := len(defaultErrorPatterns) + len(additionalPatterns)
		if len(patterns) != expectedLen {
			t.Errorf("expected %d patterns, got %d", expectedLen, len(patterns))
		}

		// Check that additional patterns are at the end
		for i, p := range additionalPatterns {
			idx := len(defaultErrorPatterns) + i
			if patterns[idx] != p {
				t.Errorf("additional pattern %d: expected %q, got %q", i, p, patterns[idx])
			}
		}
	})
}

func TestClassifier_Classify_IdlePrompt(t *testing.T) {
	c := NewClassifier()
	input := makeHookInput("idle_prompt")

	tests := []struct {
		name       string
		transcript *transcript.Transcript
		want       State
	}{
		{
			name:       "nil transcript",
			transcript: nil,
			want:       StateWaiting,
		},
		{
			name:       "empty transcript",
			transcript: makeTranscript(),
			want:       StateWaiting,
		},
		{
			name: "transcript with content",
			transcript: makeTranscript(
				transcript.Entry{Role: "user", Content: "hello"},
				transcript.Entry{Role: "assistant", Content: "hi there"},
			),
			want: StateWaiting,
		},
		{
			name: "transcript with completion indicators",
			transcript: makeTranscript(
				transcript.Entry{Role: "assistant", Content: "Task is done!"},
			),
			want: StateWaiting,
		},
		{
			name: "transcript with error patterns",
			transcript: makeTranscript(
				transcript.Entry{Role: "assistant", Content: "An error occurred"},
			),
			want: StateWaiting,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := c.Classify(input, tt.transcript)
			if got != tt.want {
				t.Errorf("Classify() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestClassifier_Classify_Stop_Complete(t *testing.T) {
	c := NewClassifier()
	input := makeHookInput("stop")

	completionMessages := []string{
		"The task is done.",
		"Work is complete!",
		"I have completed the implementation.",
		"Everything is finished now.",
		"All tests pass successfully.",
		"The feature has been implemented.",
		"Ready for review!",
		"Task complete - all changes have been made.",
	}

	for _, msg := range completionMessages {
		t.Run(msg, func(t *testing.T) {
			tr := makeTranscript(
				transcript.Entry{Role: "assistant", Content: msg},
			)
			got := c.Classify(input, tr)
			if got != StateComplete {
				t.Errorf("Classify() with message %q = %v, want StateComplete", msg, got)
			}
		})
	}
}

func TestClassifier_Classify_Stop_Stopped(t *testing.T) {
	c := NewClassifier()
	input := makeHookInput("stop")

	errorMessages := []string{
		"An error occurred while processing.",
		"The operation failed.",
		"Exception thrown: NullPointerException",
		"Cannot connect to the database.",
		"Unable to find the file.",
		"Unexpected token in JSON.",
	}

	for _, msg := range errorMessages {
		t.Run(msg, func(t *testing.T) {
			tr := makeTranscript(
				transcript.Entry{Role: "assistant", Content: msg},
			)
			got := c.Classify(input, tr)
			if got != StateStopped {
				t.Errorf("Classify() with message %q = %v, want StateStopped", msg, got)
			}
		})
	}
}

func TestClassifier_Classify_Stop_Idle(t *testing.T) {
	c := NewClassifier()
	input := makeHookInput("stop")

	idleMessages := []string{
		"Here is the code you requested.",
		"I've made some changes to the file.",
		"The function looks like this.",
		"Let me explain how this works.",
	}

	for _, msg := range idleMessages {
		t.Run(msg, func(t *testing.T) {
			tr := makeTranscript(
				transcript.Entry{Role: "assistant", Content: msg},
			)
			got := c.Classify(input, tr)
			if got != StateIdle {
				t.Errorf("Classify() with message %q = %v, want StateIdle", msg, got)
			}
		})
	}
}

func TestClassifier_Classify_Stop_NilTranscript(t *testing.T) {
	c := NewClassifier()
	input := makeHookInput("stop")

	got := c.Classify(input, nil)
	if got != StateIdle {
		t.Errorf("Classify() with nil transcript = %v, want StateIdle", got)
	}
}

func TestClassifier_Classify_Stop_EmptyTranscript(t *testing.T) {
	c := NewClassifier()
	input := makeHookInput("stop")

	got := c.Classify(input, makeTranscript())
	if got != StateIdle {
		t.Errorf("Classify() with empty transcript = %v, want StateIdle", got)
	}
}

func TestClassifier_Classify_NilInput(t *testing.T) {
	c := NewClassifier()
	tr := makeTranscript(
		transcript.Entry{Role: "assistant", Content: "Task is done!"},
	)

	got := c.Classify(nil, tr)
	if got != StateIdle {
		t.Errorf("Classify() with nil input = %v, want StateIdle", got)
	}
}

func TestClassifier_Classify_UnknownNotificationType(t *testing.T) {
	c := NewClassifier()
	input := makeHookInput("unknown_type")
	tr := makeTranscript(
		transcript.Entry{Role: "assistant", Content: "Task is done!"},
	)

	got := c.Classify(input, tr)
	if got != StateIdle {
		t.Errorf("Classify() with unknown type = %v, want StateIdle", got)
	}
}

func TestClassifier_Classify_CompleteTakesPrecedenceOverError(t *testing.T) {
	// When a message contains both completion and error indicators,
	// completion should take precedence.
	c := NewClassifier()
	input := makeHookInput("stop")
	tr := makeTranscript(
		transcript.Entry{
			Role:    "assistant",
			Content: "Fixed the error. Task is done!",
		},
	)

	got := c.Classify(input, tr)
	if got != StateComplete {
		t.Errorf("Classify() with both completion and error = %v, want StateComplete", got)
	}
}

func TestClassifier_Classify_CaseInsensitivePatterns(t *testing.T) {
	c := NewClassifier()
	input := makeHookInput("stop")

	tests := []struct {
		content string
		want    State
	}{
		{"TASK IS DONE", StateComplete},
		{"task is done", StateComplete},
		{"Task Is Done", StateComplete},
		{"AN ERROR OCCURRED", StateStopped},
		{"An Error Occurred", StateStopped},
		{"FAILED TO PROCESS", StateStopped},
		{"Failed To Process", StateStopped},
	}

	for _, tt := range tests {
		t.Run(tt.content, func(t *testing.T) {
			tr := makeTranscript(
				transcript.Entry{Role: "assistant", Content: tt.content},
			)
			got := c.Classify(input, tr)
			if got != tt.want {
				t.Errorf("Classify() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestClassifier_Classify_LastAssistantResponseUsed(t *testing.T) {
	c := NewClassifier()
	input := makeHookInput("stop")

	// The last assistant response should be used, not earlier ones
	tr := makeTranscript(
		transcript.Entry{Role: "assistant", Content: "An error occurred"},
		transcript.Entry{Role: "user", Content: "Please fix it"},
		transcript.Entry{Role: "assistant", Content: "Task is done!"},
	)

	got := c.Classify(input, tr)
	if got != StateComplete {
		t.Errorf("Classify() should use last assistant response, got %v, want StateComplete", got)
	}
}

func TestClassifier_CustomPatterns(t *testing.T) {
	customPatterns := []string{"kaboom", "exploded"}
	c := NewClassifier(WithErrorPatterns(customPatterns))
	input := makeHookInput("stop")

	tests := []struct {
		content string
		want    State
	}{
		{"Something went kaboom!", StateStopped},
		{"The server exploded.", StateStopped},
		{"An error occurred", StateIdle}, // Not in custom patterns
		{"Just normal text", StateIdle},
	}

	for _, tt := range tests {
		t.Run(tt.content, func(t *testing.T) {
			tr := makeTranscript(
				transcript.Entry{Role: "assistant", Content: tt.content},
			)
			got := c.Classify(input, tr)
			if got != tt.want {
				t.Errorf("Classify() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestClassifier_ErrorPatterns_ReturnsCopy(t *testing.T) {
	c := NewClassifier()
	patterns := c.ErrorPatterns()

	// Modify the returned slice
	patterns[0] = "modified"

	// Original should be unchanged
	original := c.ErrorPatterns()
	if original[0] == "modified" {
		t.Error("ErrorPatterns() should return a copy, not the original slice")
	}
}

func TestDefaultErrorPatterns(t *testing.T) {
	expectedPatterns := []string{
		"error",
		"failed",
		"exception",
		"cannot",
		"unable to",
		"unexpected",
	}

	if len(defaultErrorPatterns) != len(expectedPatterns) {
		t.Errorf("defaultErrorPatterns length = %d, want %d", len(defaultErrorPatterns), len(expectedPatterns))
	}

	for i, p := range expectedPatterns {
		if defaultErrorPatterns[i] != p {
			t.Errorf("defaultErrorPatterns[%d] = %q, want %q", i, defaultErrorPatterns[i], p)
		}
	}
}

func TestClassifier_Classify_AllStates(t *testing.T) {
	// Ensure we can reach all defined states
	c := NewClassifier()

	tests := []struct {
		name       string
		input      *hook.HookInput
		transcript *transcript.Transcript
		want       State
	}{
		{
			name:  "StateWaiting via idle_prompt",
			input: makeHookInput("idle_prompt"),
			transcript: makeTranscript(
				transcript.Entry{Role: "assistant", Content: "Hello"},
			),
			want: StateWaiting,
		},
		{
			name:  "StateComplete via stop with completion",
			input: makeHookInput("stop"),
			transcript: makeTranscript(
				transcript.Entry{Role: "assistant", Content: "Task is done!"},
			),
			want: StateComplete,
		},
		{
			name:  "StateStopped via stop with error",
			input: makeHookInput("stop"),
			transcript: makeTranscript(
				transcript.Entry{Role: "assistant", Content: "An error occurred"},
			),
			want: StateStopped,
		},
		{
			name:  "StateIdle via stop with neutral message",
			input: makeHookInput("stop"),
			transcript: makeTranscript(
				transcript.Entry{Role: "assistant", Content: "Here is the code"},
			),
			want: StateIdle,
		},
		{
			name:       "StateIdle via nil input",
			input:      nil,
			transcript: makeTranscript(),
			want:       StateIdle,
		},
		{
			name:       "StateIdle via unknown notification type",
			input:      makeHookInput("some_other_type"),
			transcript: makeTranscript(),
			want:       StateIdle,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := c.Classify(tt.input, tt.transcript)
			if got != tt.want {
				t.Errorf("Classify() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestClassifier_Classify_ErrorPatternSubstring(t *testing.T) {
	c := NewClassifier()
	input := makeHookInput("stop")

	// Test that error patterns match as substrings
	messages := []string{
		"Something errorful happened",         // contains "error"
		"The request has failed miserably",    // contains "failed"
		"An exception was thrown",             // contains "exception"
		"I cannot do that",                    // contains "cannot"
		"I am unable to help with that",       // contains "unable to"
		"There was an unexpected result",      // contains "unexpected"
		"errors everywhere",                   // contains "error"
		"it failed!",                          // contains "failed"
		"NullPointerException at line 42",     // contains "exception"
		"cannot find module",                  // contains "cannot"
		"unable to connect",                   // contains "unable to"
		"unexpected token",                    // contains "unexpected"
	}

	for _, msg := range messages {
		t.Run(msg, func(t *testing.T) {
			tr := makeTranscript(
				transcript.Entry{Role: "assistant", Content: msg},
			)
			got := c.Classify(input, tr)
			if got != StateStopped {
				// Check which pattern should have matched
				msgLower := strings.ToLower(msg)
				var matchedPattern string
				for _, p := range defaultErrorPatterns {
					if strings.Contains(msgLower, p) {
						matchedPattern = p
						break
					}
				}
				t.Errorf("Classify() = %v, want StateStopped (should match pattern %q)", got, matchedPattern)
			}
		})
	}
}
