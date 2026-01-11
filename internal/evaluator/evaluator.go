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
	DefaultMaxTokens = 1000

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
// The %s placeholders are: provider_names, user_request, assistant_response
const evaluationPromptTemplate = `Analyze this Claude Code interaction and determine if the task is complete.

User Request: %s

Assistant Response: %s

IMPORTANT FORMATTING RULES:
- These summaries will be sent to: %s
- Use PLAIN TEXT only - NO Markdown formatting (no ##, **, ---, |tables|, etc.)
- Keep it concise: 1-2 sentences maximum
- Emojis are OK and encouraged for status indicators (✅ ❌ ⏳ etc.)
- No bullet points, numbered lists, or special formatting

Respond with JSON only:
{"state": "complete|waiting|needs_review", "summary": "plain text summary with optional emojis", "confidence": 0.0-1.0}

- complete: The assistant finished the requested work
- waiting: The assistant is asking a question or needs user input
- needs_review: Work done but may need human verification`

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
	MaxTokens int    `yaml:"max_tokens"` // default 256
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

	prompt := fmt.Sprintf(evaluationPromptTemplate, userRequest, assistantResponse, providerHint)

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

	return &result, nil
}
