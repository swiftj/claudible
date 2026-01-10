package transcript

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		wantLen     int
		wantErr     bool
		errContains string
	}{
		{
			name: "valid transcript",
			content: `{"role": "user", "content": "Hello"}
{"role": "assistant", "content": "Hi there!"}
{"role": "user", "content": "How are you?"}`,
			wantLen: 3,
			wantErr: false,
		},
		{
			name:    "empty file",
			content: "",
			wantLen: 0,
			wantErr: false,
		},
		{
			name: "with timestamps",
			content: `{"role": "user", "content": "Test", "timestamp": "2024-01-15T10:30:00Z"}
{"role": "assistant", "content": "Response"}`,
			wantLen: 2,
			wantErr: false,
		},
		{
			name: "skip malformed lines",
			content: `{"role": "user", "content": "Valid"}
not valid json
{"role": "assistant", "content": "Also valid"}`,
			wantLen: 2,
			wantErr: false,
		},
		{
			name: "skip entries without role",
			content: `{"role": "user", "content": "Valid"}
{"content": "No role field"}
{"role": "assistant", "content": "Also valid"}`,
			wantLen: 2,
			wantErr: false,
		},
		{
			name: "empty lines ignored",
			content: `{"role": "user", "content": "First"}

{"role": "assistant", "content": "Second"}

`,
			wantLen: 2,
			wantErr: false,
		},
		{
			name: "whitespace-only lines ignored",
			content: `{"role": "user", "content": "First"}

{"role": "assistant", "content": "Second"}`,
			wantLen: 2,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temp file
			tmpDir := t.TempDir()
			path := filepath.Join(tmpDir, "transcript.jsonl")
			if err := os.WriteFile(path, []byte(tt.content), 0644); err != nil {
				t.Fatalf("failed to create temp file: %v", err)
			}

			transcript, err := Parse(path)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				} else if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("error %q should contain %q", err.Error(), tt.errContains)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if transcript.Len() != tt.wantLen {
				t.Errorf("got %d entries, want %d", transcript.Len(), tt.wantLen)
			}
		})
	}
}

func TestParseFileNotFound(t *testing.T) {
	_, err := Parse("/nonexistent/path/transcript.jsonl")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention 'not found', got: %v", err)
	}
}

func TestParseReader(t *testing.T) {
	content := `{"role": "user", "content": "Hello from reader"}
{"role": "assistant", "content": "Response from reader"}`

	transcript, err := ParseReader(strings.NewReader(content))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if transcript.Len() != 2 {
		t.Errorf("got %d entries, want 2", transcript.Len())
	}
}

func TestLastUserPrompt(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name: "multiple user messages",
			content: `{"role": "user", "content": "First"}
{"role": "assistant", "content": "Response"}
{"role": "user", "content": "Second"}
{"role": "assistant", "content": "Another response"}`,
			want: "Second",
		},
		{
			name:    "no user messages",
			content: `{"role": "assistant", "content": "Only assistant"}`,
			want:    "",
		},
		{
			name:    "empty transcript",
			content: "",
			want:    "",
		},
		{
			name:    "user message last",
			content: `{"role": "user", "content": "Last one"}`,
			want:    "Last one",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transcript, err := ParseReader(strings.NewReader(tt.content))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			got := transcript.LastUserPrompt()
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLastAssistantResponse(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name: "multiple assistant messages",
			content: `{"role": "user", "content": "Question"}
{"role": "assistant", "content": "First response"}
{"role": "user", "content": "Another question"}
{"role": "assistant", "content": "Final response"}`,
			want: "Final response",
		},
		{
			name:    "no assistant messages",
			content: `{"role": "user", "content": "Only user"}`,
			want:    "",
		},
		{
			name:    "empty transcript",
			content: "",
			want:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transcript, err := ParseReader(strings.NewReader(tt.content))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			got := transcript.LastAssistantResponse()
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestHasCompletionIndicators(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{
			name:    "contains done",
			content: `{"role": "assistant", "content": "The task is done."}`,
			want:    true,
		},
		{
			name:    "contains complete",
			content: `{"role": "assistant", "content": "Implementation is complete."}`,
			want:    true,
		},
		{
			name:    "contains finished",
			content: `{"role": "assistant", "content": "I have finished the work."}`,
			want:    true,
		},
		{
			name:    "contains tests pass",
			content: `{"role": "assistant", "content": "All tests pass now."}`,
			want:    true,
		},
		{
			name:    "contains successfully",
			content: `{"role": "assistant", "content": "Successfully implemented the feature."}`,
			want:    true,
		},
		{
			name:    "case insensitive",
			content: `{"role": "assistant", "content": "DONE! All work COMPLETED."}`,
			want:    true,
		},
		{
			name:    "no indicators",
			content: `{"role": "assistant", "content": "Working on it..."}`,
			want:    false,
		},
		{
			name:    "empty transcript",
			content: "",
			want:    false,
		},
		{
			name:    "only user messages",
			content: `{"role": "user", "content": "Is it done?"}`,
			want:    false,
		},
		{
			name: "indicator in old message not last",
			content: `{"role": "assistant", "content": "That task is done."}
{"role": "user", "content": "Now do this other thing."}
{"role": "assistant", "content": "Working on the new task..."}`,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transcript, err := ParseReader(strings.NewReader(tt.content))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			got := transcript.HasCompletionIndicators()
			if got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsEmpty(t *testing.T) {
	empty, _ := ParseReader(strings.NewReader(""))
	if !empty.IsEmpty() {
		t.Error("expected empty transcript to return true for IsEmpty()")
	}

	notEmpty, _ := ParseReader(strings.NewReader(`{"role": "user", "content": "Hi"}`))
	if notEmpty.IsEmpty() {
		t.Error("expected non-empty transcript to return false for IsEmpty()")
	}

	var nilTranscript *Transcript
	if !nilTranscript.IsEmpty() {
		t.Error("expected nil transcript to return true for IsEmpty()")
	}
}

func TestNilTranscript(t *testing.T) {
	var nilTranscript *Transcript

	if nilTranscript.LastUserPrompt() != "" {
		t.Error("nil transcript LastUserPrompt should return empty string")
	}

	if nilTranscript.LastAssistantResponse() != "" {
		t.Error("nil transcript LastAssistantResponse should return empty string")
	}

	if nilTranscript.HasCompletionIndicators() {
		t.Error("nil transcript HasCompletionIndicators should return false")
	}

	if nilTranscript.Len() != 0 {
		t.Error("nil transcript Len should return 0")
	}

	if nilTranscript.UserEntries() != nil {
		t.Error("nil transcript UserEntries should return nil")
	}

	if nilTranscript.AssistantEntries() != nil {
		t.Error("nil transcript AssistantEntries should return nil")
	}
}

func TestUserEntries(t *testing.T) {
	content := `{"role": "user", "content": "First user"}
{"role": "assistant", "content": "First response"}
{"role": "user", "content": "Second user"}
{"role": "assistant", "content": "Second response"}
{"role": "user", "content": "Third user"}`

	transcript, err := ParseReader(strings.NewReader(content))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	users := transcript.UserEntries()
	if len(users) != 3 {
		t.Errorf("got %d user entries, want 3", len(users))
	}

	expectedContents := []string{"First user", "Second user", "Third user"}
	for i, entry := range users {
		if entry.Content != expectedContents[i] {
			t.Errorf("user entry %d: got %q, want %q", i, entry.Content, expectedContents[i])
		}
		if entry.Role != "user" {
			t.Errorf("user entry %d: got role %q, want 'user'", i, entry.Role)
		}
	}
}

func TestAssistantEntries(t *testing.T) {
	content := `{"role": "user", "content": "First user"}
{"role": "assistant", "content": "First response"}
{"role": "user", "content": "Second user"}
{"role": "assistant", "content": "Second response"}`

	transcript, err := ParseReader(strings.NewReader(content))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assistants := transcript.AssistantEntries()
	if len(assistants) != 2 {
		t.Errorf("got %d assistant entries, want 2", len(assistants))
	}

	expectedContents := []string{"First response", "Second response"}
	for i, entry := range assistants {
		if entry.Content != expectedContents[i] {
			t.Errorf("assistant entry %d: got %q, want %q", i, entry.Content, expectedContents[i])
		}
		if entry.Role != "assistant" {
			t.Errorf("assistant entry %d: got role %q, want 'assistant'", i, entry.Role)
		}
	}
}

func TestTimestampParsing(t *testing.T) {
	content := `{"role": "user", "content": "Test", "timestamp": "2024-01-15T10:30:00Z"}`

	transcript, err := ParseReader(strings.NewReader(content))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if transcript.Len() != 1 {
		t.Fatalf("got %d entries, want 1", transcript.Len())
	}

	entry := transcript.Entries[0]
	expectedTime := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	if !entry.Timestamp.Equal(expectedTime) {
		t.Errorf("got timestamp %v, want %v", entry.Timestamp, expectedTime)
	}
}
