// Package evaluator provides LLM-based smart state classification for Claude Code interactions.
// It analyzes user requests and assistant responses to determine completion state more
// intelligently than heuristic-based classification.
package evaluator

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	// DefaultMaxTokens is the default maximum tokens for LLM responses.
	DefaultMaxTokens = 1024

	// DefaultTimeout is the default HTTP request timeout.
	DefaultTimeout = 30 * time.Second

	// AnthropicAPIURL is the Anthropic Messages API endpoint.
	AnthropicAPIURL = "https://api.anthropic.com/v1/messages"

	// OpenAIAPIURL is the OpenAI Chat Completions API endpoint.
	OpenAIAPIURL = "https://api.openai.com/v1/chat/completions"

	// StateComplete indicates the assistant finished the requested work.
	StateComplete = "complete"

	// StateWaiting indicates the assistant is asking a question or needs user input.
	StateWaiting = "waiting"

	// StateNeedsReview indicates work is done but may need human verification.
	StateNeedsReview = "needs_review"
)

// evaluationPromptTemplate is the prompt used to analyze Claude Code interactions.
// The %s placeholders are: user_request, assistant_response, provider_names
const evaluationPromptTemplate = `You are a notification summarizer for mobile devices (%s).

RULES:
1. Summary: PLAIN TEXT only, 1-2 sentences, ~200 chars max
2. FORBIDDEN: Markdown (## ** ` + "`" + `), tables (|), bullets (- *), code blocks, line breaks
3. Allowed: Plain words, spaces, emojis (✅ ❌ ⏳ 🔧)
4. Style: Conversational text message, not documentation

GOOD: "✅ Added JWT auth and updated all tests. Ready to deploy."
BAD: "## Summary\n- Added auth" (has markdown/bullets)

User Request: %s

Assistant Response: %s

Respond with ONLY JSON:
{"state":"complete|waiting|needs_review","summary":"1-2 sentence summary","confidence":0.0-1.0}

States: complete=finished, waiting=needs input, needs_review=done but verify`

// EvaluationResult contains the LLM's assessment of the interaction state.
type EvaluationResult struct {
	State      string  `json:"state"`      // complete, waiting, needs_review
	Summary    string  `json:"summary"`    // One-line human readable summary
	Confidence float64 `json:"confidence"` // 0.0 to 1.0
}

// Config holds the configuration for the LLM evaluator.
type Config struct {
	Enabled   bool   `yaml:"enabled"`
	Provider  string `yaml:"provider"` // anthropic, openai
	APIKey    string `yaml:"api_key"`
	Model     string `yaml:"model"`      // claude-3-haiku-20240307, gpt-4o-mini
	MaxTokens int    `yaml:"max_tokens"` // default 1024
}

// Evaluator uses an LLM to analyze Claude Code interactions and determine completion state.
type Evaluator struct {
	config     *Config
	httpClient *http.Client
}

// Option is a functional option for configuring an Evaluator.
type Option func(*Evaluator)

// WithHTTPClient sets a custom HTTP client for the evaluator.
func WithHTTPClient(client *http.Client) Option {
	return func(e *Evaluator) {
		if client != nil {
			e.httpClient = client
		}
	}
}

// WithTimeout sets the timeout for HTTP requests.
func WithTimeout(timeout time.Duration) Option {
	return func(e *Evaluator) {
		if timeout > 0 {
			e.httpClient.Timeout = timeout
		}
	}
}

// NewEvaluator creates a new Evaluator with the given configuration.
func NewEvaluator(cfg *Config, opts ...Option) *Evaluator {
	if cfg == nil {
		cfg = &Config{}
	}

	// Set defaults
	if cfg.MaxTokens <= 0 {
		cfg.MaxTokens = DefaultMaxTokens
	}

	e := &Evaluator{
		config: cfg,
		httpClient: &http.Client{
			Timeout: DefaultTimeout,
		},
	}

	// Apply options
	for _, opt := range opts {
		opt(e)
	}

	return e
}

// Enabled returns whether the evaluator is enabled.
func (e *Evaluator) Enabled() bool {
	return e.config != nil && e.config.Enabled && e.config.APIKey != ""
}

// Evaluate analyzes the user request and assistant response to determine completion state.
// The providers parameter indicates which notification providers are enabled (for formatting hints).
// Returns nil if evaluation fails (caller should fall back to heuristics).
func (e *Evaluator) Evaluate(ctx context.Context, userRequest, assistantResponse string, providers ...string) (*EvaluationResult, error) {
	if !e.Enabled() {
		return nil, nil
	}

	// Default provider hint if none specified
	providerHint := "mobile notifications (iMessage, SMS)"
	if len(providers) > 0 {
		providerHint = strings.Join(providers, ", ")
	}

	// Truncate assistant response to avoid token limits (keep first 2000 chars)
	truncatedResponse := assistantResponse
	if len(truncatedResponse) > 2000 {
		truncatedResponse = truncatedResponse[:2000] + "..."
	}

	// Template order: providerHint, userRequest, truncatedResponse
	prompt := fmt.Sprintf(evaluationPromptTemplate, providerHint, userRequest, truncatedResponse)

	var response string
	var err error

	switch strings.ToLower(e.config.Provider) {
	case "anthropic":
		response, err = e.callAnthropic(ctx, prompt)
	case "openai":
		response, err = e.callOpenAI(ctx, prompt)
	default:
		return nil, fmt.Errorf("unsupported provider: %s", e.config.Provider)
	}

	if err != nil {
		return nil, err
	}

	return e.parseResponse(response)
}

// anthropicRequest represents the request body for the Anthropic Messages API.
type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	Messages  []anthropicMessage `json:"messages"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// anthropicResponse represents the response from the Anthropic Messages API.
type anthropicResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// callAnthropic sends a request to the Anthropic Messages API.
func (e *Evaluator) callAnthropic(ctx context.Context, prompt string) (string, error) {
	reqBody := anthropicRequest{
		Model:     e.config.Model,
		MaxTokens: e.config.MaxTokens,
		Messages: []anthropicMessage{
			{Role: "user", Content: prompt},
		},
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal anthropic request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, AnthropicAPIURL, bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("failed to create anthropic request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", e.config.APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("anthropic request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return "", fmt.Errorf("failed to read anthropic response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("anthropic API returned status %d: %s", resp.StatusCode, string(body))
	}

	var apiResp anthropicResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return "", fmt.Errorf("failed to parse anthropic response: %w", err)
	}

	if apiResp.Error != nil {
		return "", fmt.Errorf("anthropic API error: %s: %s", apiResp.Error.Type, apiResp.Error.Message)
	}

	if len(apiResp.Content) == 0 {
		return "", fmt.Errorf("anthropic returned empty content")
	}

	return apiResp.Content[0].Text, nil
}

// openaiRequest represents the request body for the OpenAI Chat Completions API.
type openaiRequest struct {
	Model     string          `json:"model"`
	MaxTokens int             `json:"max_tokens"`
	Messages  []openaiMessage `json:"messages"`
}

type openaiMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// openaiResponse represents the response from the OpenAI Chat Completions API.
type openaiResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

// callOpenAI sends a request to the OpenAI Chat Completions API.
func (e *Evaluator) callOpenAI(ctx context.Context, prompt string) (string, error) {
	reqBody := openaiRequest{
		Model:     e.config.Model,
		MaxTokens: e.config.MaxTokens,
		Messages: []openaiMessage{
			{Role: "user", Content: prompt},
		},
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal openai request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, OpenAIAPIURL, bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("failed to create openai request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.config.APIKey)

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("openai request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return "", fmt.Errorf("failed to read openai response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("openai API returned status %d: %s", resp.StatusCode, string(body))
	}

	var apiResp openaiResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return "", fmt.Errorf("failed to parse openai response: %w", err)
	}

	if apiResp.Error != nil {
		return "", fmt.Errorf("openai API error: %s: %s", apiResp.Error.Type, apiResp.Error.Message)
	}

	if len(apiResp.Choices) == 0 {
		return "", fmt.Errorf("openai returned no choices")
	}

	return apiResp.Choices[0].Message.Content, nil
}

// parseResponse extracts the EvaluationResult from the LLM response.
func (e *Evaluator) parseResponse(response string) (*EvaluationResult, error) {
	// Trim whitespace and try to extract JSON
	response = strings.TrimSpace(response)

	// Try to find JSON object in the response
	start := strings.Index(response, "{")
	end := strings.LastIndex(response, "}")

	if start == -1 || end == -1 || end < start {
		return nil, fmt.Errorf("no JSON object found in response: %s", response)
	}

	jsonStr := response[start : end+1]

	var result EvaluationResult
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		return nil, fmt.Errorf("failed to parse evaluation result: %w", err)
	}

	// Validate state
	validStates := map[string]bool{
		StateComplete:    true,
		StateWaiting:     true,
		StateNeedsReview: true,
	}
	if !validStates[result.State] {
		return nil, fmt.Errorf("invalid state in response: %s", result.State)
	}

	// Clamp confidence to valid range
	if result.Confidence < 0 {
		result.Confidence = 0
	}
	if result.Confidence > 1 {
		result.Confidence = 1
	}

	// Sanitize summary - strip any Markdown that slipped through
	result.Summary = SanitizeSummary(result.Summary)

	return &result, nil
}

// SanitizeSummary removes any Markdown or special formatting from the summary.
// This is a safety net in case the LLM ignores formatting instructions.
// Exported for use by other packages (router fallback, providers).
func SanitizeSummary(s string) string {
	result := s

	// Strip Markdown emphasis patterns (bold/italic) while keeping the text
	// Order matters: process longer patterns first
	result = stripEmphasis(result, "***") // Bold+italic
	result = stripEmphasis(result, "___") // Bold+italic alt
	result = stripEmphasis(result, "**")  // Bold
	result = stripEmphasis(result, "__")  // Bold alt (when used for emphasis)
	result = stripEmphasis(result, "*")   // Italic
	result = stripEmphasis(result, "_")   // Italic alt

	// Remove other common Markdown patterns
	replacements := []struct {
		old string
		new string
	}{
		{"##", ""},    // Headers
		{"~~", ""},    // Strikethrough markers (text already handled by stripEmphasis logic)
		{"```", ""},   // Code blocks
		{"`", ""},     // Inline code
		{"---", ""},   // Horizontal rule
		{"* ", ""},    // Bullet points (note: space after to avoid hitting emphasis)
		{"- ", ""},    // Bullet points alt
		{"• ", ""},    // Unicode bullet
		{"\n", " "},   // Newlines to spaces
		{"\r", ""},    // Carriage returns
		{"\t", " "},   // Tabs to spaces
	}

	for _, r := range replacements {
		result = strings.ReplaceAll(result, r.old, r.new)
	}

	// Handle strikethrough ~~text~~ pattern
	result = stripEmphasis(result, "~~")

	// Remove table-like patterns (pipes with content)
	if strings.Contains(result, "|") {
		parts := strings.Split(result, "|")
		var cleanParts []string
		for _, part := range parts {
			trimmed := strings.TrimSpace(part)
			if trimmed != "" && !strings.HasPrefix(trimmed, "-") && !strings.HasPrefix(trimmed, "=") {
				cleanParts = append(cleanParts, trimmed)
			}
		}
		if len(cleanParts) > 0 {
			result = strings.Join(cleanParts, " - ")
		}
	}

	// Collapse multiple spaces
	for strings.Contains(result, "  ") {
		result = strings.ReplaceAll(result, "  ", " ")
	}

	// Trim and enforce length limit (330 chars allows complete thoughts on mobile)
	result = strings.TrimSpace(result)
	if len(result) > 330 {
		result = smartTruncate(result, 330)
	}

	return result
}

// smartTruncate truncates text to maxLen by finding the previous sentence boundary
// (period or line break) rather than using ellipsis mid-sentence.
func smartTruncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}

	// Look for the last period or line break within the limit
	truncated := s[:maxLen]

	// Find the last sentence boundary (period followed by space, or line break)
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
		return strings.TrimSpace(truncated[:boundary]) // Exclude newline, trim
	}

	// No good boundary found - look for standalone period at end of word
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

// stripEmphasis removes Markdown emphasis markers (like ** or _) while keeping the text.
// For example: "**bold**" -> "bold", "_italic_" -> "italic"
// Importantly, this respects word boundaries - underscores in the middle of words like
// "MCP_Cortex.md" are NOT treated as emphasis markers.
func stripEmphasis(s, marker string) string {
	for {
		start := strings.Index(s, marker)
		if start == -1 {
			break
		}
		end := strings.Index(s[start+len(marker):], marker)
		if end == -1 {
			// Unmatched marker - just leave it alone
			break
		}
		// Found matching pair - check if they form valid emphasis boundaries
		end = start + len(marker) + end
		if !isValidOpeningMarker(s, start, len(marker)) || !isValidClosingMarker(s, end, len(marker)) {
			// Not valid emphasis markers (in middle of word), skip
			// Search for next occurrence after this one
			remaining := s[start+len(marker):]
			nextStart := strings.Index(remaining, marker)
			if nextStart == -1 {
				break
			}
			// Continue searching from after the current marker
			prefix := s[:start+len(marker)]
			suffix := stripEmphasis(remaining, marker)
			return prefix + suffix
		}
		// Valid emphasis - remove both markers, keep content
		content := s[start+len(marker) : end]
		s = s[:start] + content + s[end+len(marker):]
	}
	return s
}

// isValidOpeningMarker checks if a marker at position pos can be a valid opening emphasis marker.
// An opening marker must be:
// - At start of string OR preceded by whitespace/punctuation (not alphanumeric)
// - Followed by non-whitespace (the content to emphasize)
func isValidOpeningMarker(s string, pos int, markerLen int) bool {
	// Check character before the marker (must not be alphanumeric)
	if pos > 0 {
		before := s[pos-1]
		if isAlphanumeric(before) {
			return false
		}
	}

	// Check character after the marker (must exist and not be whitespace)
	afterPos := pos + markerLen
	if afterPos >= len(s) {
		return false // Nothing after marker
	}
	return !isWhitespace(s[afterPos])
}

// isValidClosingMarker checks if a marker at position pos can be a valid closing emphasis marker.
// A closing marker must be:
// - Preceded by non-whitespace (the content to emphasize)
// - At end of string OR followed by whitespace/punctuation (not alphanumeric)
func isValidClosingMarker(s string, pos int, markerLen int) bool {
	// Check character before the marker (must not be whitespace)
	if pos > 0 {
		before := s[pos-1]
		if isWhitespace(before) {
			return false
		}
	} else {
		return false // Nothing before marker
	}

	// Check character after the marker (must not be alphanumeric)
	afterPos := pos + markerLen
	if afterPos < len(s) {
		after := s[afterPos]
		if isAlphanumeric(after) {
			return false
		}
	}

	return true
}

// isAlphanumeric returns true if the byte is a letter or digit.
func isAlphanumeric(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

// isWhitespace returns true if the byte is a whitespace character.
func isWhitespace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}
