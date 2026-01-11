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
// Claude Code uses a nested format where the actual role/content are inside a "message" field.
type Entry struct {
	// Claude Code's actual format fields
	Type      string       `json:"type"`              // "user", "assistant", "summary", "system", etc.
	Message   *MessageBody `json:"message,omitempty"` // Nested message content
	Timestamp time.Time    `json:"timestamp,omitempty"`

	// Legacy/simple format fields (for backwards compatibility)
	Role    string `json:"role"`    // Direct role field
	Content string `json:"content"` // Direct content field
}

// MessageBody represents the nested message structure in Claude Code transcripts.
type MessageBody struct {
	Role    string      `json:"role"`    // "user" or "assistant"
	Content interface{} `json:"content"` // Can be string or []ContentBlock
}

// ContentBlock represents a single content block in Claude Code's array format.
type ContentBlock struct {
	Type string `json:"type"` // "text", "tool_use", etc.
	Text string `json:"text"` // The actual text content
}

// GetRole returns the role from either the nested message or legacy format.
func (e *Entry) GetRole() string {
	// Prefer the top-level Type field (Claude Code's actual format)
	if e.Type == "user" || e.Type == "assistant" {
		return e.Type
	}
	// Check nested message
	if e.Message != nil && e.Message.Role != "" {
		return e.Message.Role
	}
	// Fall back to legacy format
	return e.Role
}

// GetContent extracts the text content from either format.
func (e *Entry) GetContent() string {
	// Try nested message format first (Claude Code's actual format)
	if e.Message != nil {
		return extractContent(e.Message.Content)
	}
	// Fall back to legacy direct content field
	return e.Content
}

// extractContent handles both string and array content formats.
func extractContent(content interface{}) string {
	if content == nil {
		return ""
	}

	// Handle direct string content
	if str, ok := content.(string); ok {
		return str
	}

	// Handle array of content blocks
	if arr, ok := content.([]interface{}); ok {
		var texts []string
		for _, item := range arr {
			if block, ok := item.(map[string]interface{}); ok {
				if blockType, _ := block["type"].(string); blockType == "text" {
					if text, ok := block["text"].(string); ok {
						texts = append(texts, text)
					}
				}
			}
		}
		return strings.Join(texts, "\n")
	}

	return ""
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
	defer func() { _ = file.Close() }()

	return ParseReader(file)
}

// ParseReader reads and parses a JSONL transcript from an io.Reader.
// This allows parsing from any source, not just files.
func ParseReader(r io.Reader) (*Transcript, error) {
	transcript := &Transcript{
		Entries: make([]Entry, 0),
	}

	scanner := bufio.NewScanner(r)
	// Increase buffer size to handle large transcript entries (tool results, code blocks, etc.)
	// Default is 64KB, increase to 10MB to handle Claude Code's large tool outputs
	const maxScanTokenSize = 10 * 1024 * 1024 // 10MB
	buf := make([]byte, maxScanTokenSize)
	scanner.Buffer(buf, maxScanTokenSize)
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

		// Skip entries that aren't user or assistant messages
		// Claude Code uses "type" field for message type, not "role"
		role := entry.GetRole()
		if role != "user" && role != "assistant" {
			// Not a conversation entry (could be "summary", "system", "file-history-snapshot", etc.)
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
		if t.Entries[i].GetRole() == "user" {
			return t.Entries[i].GetContent()
		}
	}

	return ""
}

// LastAssistantResponse returns the content of the last assistant message in the transcript
// that contains actual text content. Tool-only entries (tool_use without text) are skipped.
// Returns an empty string if there are no assistant messages with text content.
func (t *Transcript) LastAssistantResponse() string {
	if t == nil {
		return ""
	}

	for i := len(t.Entries) - 1; i >= 0; i-- {
		if t.Entries[i].GetRole() == "assistant" {
			content := t.Entries[i].GetContent()
			// Skip entries with no text content (e.g., tool_use only)
			if content != "" {
				return content
			}
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
		if e.GetRole() == "user" {
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
		if e.GetRole() == "assistant" {
			entries = append(entries, e)
		}
	}
	return entries
}
