// Package transcript provides parsing for Claude Code JSONL transcript files.
package transcript

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// Entry represents a single message in a Claude Code transcript.
type Entry struct {
	Role      string    `json:"role"`                // "user" or "assistant"
	Content   string    `json:"content"`             // Message content
	Timestamp time.Time `json:"timestamp,omitempty"` // Optional timestamp
}

// Transcript represents a parsed Claude Code conversation.
type Transcript struct {
	Entries []Entry
}

// completionIndicators are keywords that suggest work is complete.
var completionIndicators = []string{
	"done",
	"complete",
	"completed",
	"finished",
	"tests pass",
	"all tests pass",
	"successfully",
	"implemented",
	"ready for review",
	"task complete",
	"work is done",
}

// Parse reads and parses a JSONL transcript file.
// Each line in the file should be a valid JSON object representing an Entry.
// Malformed lines are skipped with a warning logged to stderr.
func Parse(path string) (*Transcript, error) {
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("transcript file not found: %s", path)
		}
		return nil, fmt.Errorf("failed to open transcript: %w", err)
	}
	defer file.Close()

	return ParseReader(file)
}

// ParseReader reads and parses a JSONL transcript from an io.Reader.
// This allows parsing from any source, not just files.
func ParseReader(r io.Reader) (*Transcript, error) {
	transcript := &Transcript{
		Entries: make([]Entry, 0),
	}

	scanner := bufio.NewScanner(r)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines
		if line == "" {
			continue
		}

		var entry Entry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			fmt.Fprintf(os.Stderr, "warning: skipping malformed JSON at line %d: %v\n", lineNum, err)
			continue
		}

		// Skip entries without a role
		if entry.Role == "" {
			fmt.Fprintf(os.Stderr, "warning: skipping entry at line %d: missing role field\n", lineNum)
			continue
		}

		transcript.Entries = append(transcript.Entries, entry)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading transcript: %w", err)
	}

	return transcript, nil
}

// LastUserPrompt returns the content of the last user message in the transcript.
// Returns an empty string if there are no user messages.
func (t *Transcript) LastUserPrompt() string {
	if t == nil {
		return ""
	}

	for i := len(t.Entries) - 1; i >= 0; i-- {
		if t.Entries[i].Role == "user" {
			return t.Entries[i].Content
		}
	}

	return ""
}

// LastAssistantResponse returns the content of the last assistant message in the transcript.
// Returns an empty string if there are no assistant messages.
func (t *Transcript) LastAssistantResponse() string {
	if t == nil {
		return ""
	}

	for i := len(t.Entries) - 1; i >= 0; i-- {
		if t.Entries[i].Role == "assistant" {
			return t.Entries[i].Content
		}
	}

	return ""
}

// HasCompletionIndicators checks if the last assistant response contains
// keywords that suggest the work is complete.
func (t *Transcript) HasCompletionIndicators() bool {
	response := t.LastAssistantResponse()
	if response == "" {
		return false
	}

	responseLower := strings.ToLower(response)
	for _, indicator := range completionIndicators {
		if strings.Contains(responseLower, indicator) {
			return true
		}
	}

	return false
}

// IsEmpty returns true if the transcript has no entries.
func (t *Transcript) IsEmpty() bool {
	return t == nil || len(t.Entries) == 0
}

// Len returns the number of entries in the transcript.
func (t *Transcript) Len() int {
	if t == nil {
		return 0
	}
	return len(t.Entries)
}

// UserEntries returns all user messages from the transcript.
func (t *Transcript) UserEntries() []Entry {
	if t == nil {
		return nil
	}

	var entries []Entry
	for _, e := range t.Entries {
		if e.Role == "user" {
			entries = append(entries, e)
		}
	}
	return entries
}

// AssistantEntries returns all assistant messages from the transcript.
func (t *Transcript) AssistantEntries() []Entry {
	if t == nil {
		return nil
	}

	var entries []Entry
	for _, e := range t.Entries {
		if e.Role == "assistant" {
			entries = append(entries, e)
		}
	}
	return entries
}
