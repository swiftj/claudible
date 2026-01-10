package evaluator

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestNewEvaluator_NilConfig verifies that NewEvaluator handles nil config gracefully.
func TestNewEvaluator_NilConfig(t *testing.T) {
	e := NewEvaluator(nil)

	if e == nil {
		t.Fatal("expected non-nil evaluator")
	}
	if e.config == nil {
		t.Fatal("expected non-nil config")
	}
	if e.config.MaxTokens != DefaultMaxTokens {
		t.Errorf("expected MaxTokens=%d, got %d", DefaultMaxTokens, e.config.MaxTokens)
	}
	if e.httpClient == nil {
		t.Fatal("expected non-nil httpClient")
	}
	if e.httpClient.Timeout != DefaultTimeout {
		t.Errorf("expected timeout=%v, got %v", DefaultTimeout, e.httpClient.Timeout)
	}
}

// TestNewEvaluator_ValidConfig verifies that NewEvaluator correctly initializes with valid config.
func TestNewEvaluator_ValidConfig(t *testing.T) {
	cfg := &Config{
		Enabled:  true,
		Provider: "anthropic",
		APIKey:   "test-api-key",
		Model:    "claude-3-haiku-20240307",
	}

	e := NewEvaluator(cfg)

	if e == nil {
		t.Fatal("expected non-nil evaluator")
	}
	if e.config.MaxTokens != DefaultMaxTokens {
		t.Errorf("expected default MaxTokens=%d, got %d", DefaultMaxTokens, e.config.MaxTokens)
	}
	if e.config.Provider != "anthropic" {
		t.Errorf("expected provider=anthropic, got %s", e.config.Provider)
	}
}

// TestNewEvaluator_CustomMaxTokens verifies that custom MaxTokens is preserved.
func TestNewEvaluator_CustomMaxTokens(t *testing.T) {
	cfg := &Config{
		MaxTokens: 512,
	}

	e := NewEvaluator(cfg)

	if e.config.MaxTokens != 512 {
		t.Errorf("expected MaxTokens=512, got %d", e.config.MaxTokens)
	}
}

// TestEnabled_DisabledConfig verifies Enabled returns false when config.Enabled is false.
func TestEnabled_DisabledConfig(t *testing.T) {
	cfg := &Config{
		Enabled:  false,
		Provider: "anthropic",
		APIKey:   "test-key",
	}

	e := NewEvaluator(cfg)

	if e.Enabled() {
		t.Error("expected Enabled() to return false when config.Enabled is false")
	}
}

// TestEnabled_MissingAPIKey verifies Enabled returns false when API key is missing.
func TestEnabled_MissingAPIKey(t *testing.T) {
	cfg := &Config{
		Enabled:  true,
		Provider: "anthropic",
		APIKey:   "",
	}

	e := NewEvaluator(cfg)

	if e.Enabled() {
		t.Error("expected Enabled() to return false when APIKey is empty")
	}
}

// TestEnabled_ProperlyConfigured verifies Enabled returns true when properly configured.
func TestEnabled_ProperlyConfigured(t *testing.T) {
	cfg := &Config{
		Enabled:  true,
		Provider: "anthropic",
		APIKey:   "test-api-key",
		Model:    "claude-3-haiku-20240307",
	}

	e := NewEvaluator(cfg)

	if !e.Enabled() {
		t.Error("expected Enabled() to return true when properly configured")
	}
}

// TestEvaluate_DisabledReturnsNilNil verifies Evaluate returns nil, nil when disabled.
func TestEvaluate_DisabledReturnsNilNil(t *testing.T) {
	cfg := &Config{
		Enabled: false,
	}

	e := NewEvaluator(cfg)
	result, err := e.Evaluate(context.Background(), "test request", "test response")

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if result != nil {
		t.Errorf("expected nil result, got %+v", result)
	}
}

// TestEvaluate_AnthropicProvider tests successful Anthropic API integration.
func TestEvaluate_AnthropicProvider(t *testing.T) {
	// Create a mock Anthropic server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request headers
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected Content-Type=application/json, got %s", r.Header.Get("Content-Type"))
		}
		if r.Header.Get("x-api-key") != "test-anthropic-key" {
			t.Errorf("expected x-api-key=test-anthropic-key, got %s", r.Header.Get("x-api-key"))
		}
		if r.Header.Get("anthropic-version") != "2023-06-01" {
			t.Errorf("expected anthropic-version=2023-06-01, got %s", r.Header.Get("anthropic-version"))
		}

		// Verify request body
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("failed to read request body: %v", err)
		}

		var reqBody anthropicRequest
		if err := json.Unmarshal(body, &reqBody); err != nil {
			t.Fatalf("failed to unmarshal request body: %v", err)
		}

		if reqBody.Model != "claude-3-haiku-20240307" {
			t.Errorf("expected model=claude-3-haiku-20240307, got %s", reqBody.Model)
		}
		if len(reqBody.Messages) != 1 {
			t.Errorf("expected 1 message, got %d", len(reqBody.Messages))
		}
		if reqBody.Messages[0].Role != "user" {
			t.Errorf("expected role=user, got %s", reqBody.Messages[0].Role)
		}

		// Return mock response
		resp := anthropicResponse{
			Content: []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}{
				{Type: "text", Text: `{"state": "complete", "summary": "Task completed successfully", "confidence": 0.95}`},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// Create evaluator with mock server
	cfg := &Config{
		Enabled:  true,
		Provider: "anthropic",
		APIKey:   "test-anthropic-key",
		Model:    "claude-3-haiku-20240307",
	}

	e := NewEvaluator(cfg)
	// Override the API URL by using a custom HTTP client that redirects requests
	e.httpClient = &http.Client{
		Transport: &mockTransport{
			url:      server.URL,
			delegate: http.DefaultTransport,
		},
	}

	result, err := e.Evaluate(context.Background(), "Fix the bug", "I've fixed the bug in auth.go")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.State != StateComplete {
		t.Errorf("expected state=complete, got %s", result.State)
	}
	if result.Summary != "Task completed successfully" {
		t.Errorf("expected summary='Task completed successfully', got %s", result.Summary)
	}
	if result.Confidence != 0.95 {
		t.Errorf("expected confidence=0.95, got %f", result.Confidence)
	}
}

// TestEvaluate_OpenAIProvider tests successful OpenAI API integration.
func TestEvaluate_OpenAIProvider(t *testing.T) {
	// Create a mock OpenAI server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request headers
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected Content-Type=application/json, got %s", r.Header.Get("Content-Type"))
		}
		if r.Header.Get("Authorization") != "Bearer test-openai-key" {
			t.Errorf("expected Authorization=Bearer test-openai-key, got %s", r.Header.Get("Authorization"))
		}

		// Verify request body
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("failed to read request body: %v", err)
		}

		var reqBody openaiRequest
		if err := json.Unmarshal(body, &reqBody); err != nil {
			t.Fatalf("failed to unmarshal request body: %v", err)
		}

		if reqBody.Model != "gpt-4o-mini" {
			t.Errorf("expected model=gpt-4o-mini, got %s", reqBody.Model)
		}
		if len(reqBody.Messages) != 1 {
			t.Errorf("expected 1 message, got %d", len(reqBody.Messages))
		}

		// Return mock response
		resp := openaiResponse{
			Choices: []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			}{
				{Message: struct {
					Content string `json:"content"`
				}{Content: `{"state": "waiting", "summary": "Asking for clarification", "confidence": 0.88}`}},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// Create evaluator with mock server
	cfg := &Config{
		Enabled:  true,
		Provider: "openai",
		APIKey:   "test-openai-key",
		Model:    "gpt-4o-mini",
	}

	e := NewEvaluator(cfg)
	e.httpClient = &http.Client{
		Transport: &mockTransport{
			url:      server.URL,
			delegate: http.DefaultTransport,
		},
	}

	result, err := e.Evaluate(context.Background(), "What should I do?", "Could you please clarify what you mean?")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.State != StateWaiting {
		t.Errorf("expected state=waiting, got %s", result.State)
	}
	if result.Summary != "Asking for clarification" {
		t.Errorf("expected summary='Asking for clarification', got %s", result.Summary)
	}
	if result.Confidence != 0.88 {
		t.Errorf("expected confidence=0.88, got %f", result.Confidence)
	}
}

// TestEvaluate_UnsupportedProvider tests error handling for unsupported providers.
func TestEvaluate_UnsupportedProvider(t *testing.T) {
	cfg := &Config{
		Enabled:  true,
		Provider: "unknown-provider",
		APIKey:   "test-key",
	}

	e := NewEvaluator(cfg)
	result, err := e.Evaluate(context.Background(), "test", "test")

	if err == nil {
		t.Error("expected error for unsupported provider")
	}
	if result != nil {
		t.Error("expected nil result for unsupported provider")
	}
	if !strings.Contains(err.Error(), "unsupported provider") {
		t.Errorf("expected error to contain 'unsupported provider', got %v", err)
	}
}

// TestParseResponse_ValidJSON tests parsing of valid JSON responses.
func TestParseResponse_ValidJSON(t *testing.T) {
	tests := []struct {
		name       string
		response   string
		wantState  string
		wantConf   float64
	}{
		{
			name:       "complete state",
			response:   `{"state": "complete", "summary": "Done", "confidence": 0.9}`,
			wantState:  StateComplete,
			wantConf:   0.9,
		},
		{
			name:       "waiting state",
			response:   `{"state": "waiting", "summary": "Needs input", "confidence": 0.75}`,
			wantState:  StateWaiting,
			wantConf:   0.75,
		},
		{
			name:       "needs_review state",
			response:   `{"state": "needs_review", "summary": "Review needed", "confidence": 0.85}`,
			wantState:  StateNeedsReview,
			wantConf:   0.85,
		},
		{
			name:       "JSON with surrounding text",
			response:   `Here is my analysis: {"state": "complete", "summary": "Done", "confidence": 0.95} Hope this helps!`,
			wantState:  StateComplete,
			wantConf:   0.95,
		},
		{
			name:       "JSON with leading whitespace",
			response:   `   {"state": "complete", "summary": "Done", "confidence": 0.8}`,
			wantState:  StateComplete,
			wantConf:   0.8,
		},
	}

	e := NewEvaluator(nil)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := e.parseResponse(tt.response)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.State != tt.wantState {
				t.Errorf("expected state=%s, got %s", tt.wantState, result.State)
			}
			if result.Confidence != tt.wantConf {
				t.Errorf("expected confidence=%f, got %f", tt.wantConf, result.Confidence)
			}
		})
	}
}

// TestParseResponse_InvalidState tests error handling for invalid states.
func TestParseResponse_InvalidState(t *testing.T) {
	tests := []struct {
		name     string
		response string
	}{
		{
			name:     "invalid state value",
			response: `{"state": "invalid", "summary": "Test", "confidence": 0.5}`,
		},
		{
			name:     "empty state",
			response: `{"state": "", "summary": "Test", "confidence": 0.5}`,
		},
		{
			name:     "numeric state",
			response: `{"state": 123, "summary": "Test", "confidence": 0.5}`,
		},
	}

	e := NewEvaluator(nil)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := e.parseResponse(tt.response)
			if err == nil {
				t.Error("expected error for invalid state")
			}
			if result != nil {
				t.Error("expected nil result for invalid state")
			}
		})
	}
}

// TestParseResponse_ConfidenceClamping tests that confidence is clamped to [0, 1].
func TestParseResponse_ConfidenceClamping(t *testing.T) {
	tests := []struct {
		name         string
		response     string
		wantConfLow  float64
		wantConfHigh float64
	}{
		{
			name:         "negative confidence clamped to 0",
			response:     `{"state": "complete", "summary": "Done", "confidence": -0.5}`,
			wantConfLow:  0,
			wantConfHigh: 0,
		},
		{
			name:         "confidence above 1 clamped to 1",
			response:     `{"state": "complete", "summary": "Done", "confidence": 1.5}`,
			wantConfLow:  1,
			wantConfHigh: 1,
		},
		{
			name:         "zero confidence stays at 0",
			response:     `{"state": "complete", "summary": "Done", "confidence": 0}`,
			wantConfLow:  0,
			wantConfHigh: 0,
		},
		{
			name:         "confidence of 1 stays at 1",
			response:     `{"state": "complete", "summary": "Done", "confidence": 1}`,
			wantConfLow:  1,
			wantConfHigh: 1,
		},
		{
			name:         "valid confidence unchanged",
			response:     `{"state": "complete", "summary": "Done", "confidence": 0.75}`,
			wantConfLow:  0.75,
			wantConfHigh: 0.75,
		},
	}

	e := NewEvaluator(nil)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := e.parseResponse(tt.response)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.Confidence < tt.wantConfLow || result.Confidence > tt.wantConfHigh {
				t.Errorf("expected confidence in [%f, %f], got %f", tt.wantConfLow, tt.wantConfHigh, result.Confidence)
			}
		})
	}
}

// TestParseResponse_NoJSON tests error handling when no JSON is found.
func TestParseResponse_NoJSON(t *testing.T) {
	tests := []struct {
		name     string
		response string
	}{
		{
			name:     "plain text",
			response: "This is just plain text without JSON",
		},
		{
			name:     "only opening brace",
			response: "Some text {",
		},
		{
			name:     "only closing brace",
			response: "Some text }",
		},
		{
			name:     "empty string",
			response: "",
		},
	}

	e := NewEvaluator(nil)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := e.parseResponse(tt.response)
			if err == nil {
				t.Error("expected error when no JSON found")
			}
			if result != nil {
				t.Error("expected nil result when no JSON found")
			}
		})
	}
}

// TestWithHTTPClient tests the WithHTTPClient option.
func TestWithHTTPClient(t *testing.T) {
	customClient := &http.Client{
		Timeout: 60 * time.Second,
	}

	e := NewEvaluator(nil, WithHTTPClient(customClient))

	if e.httpClient != customClient {
		t.Error("expected custom HTTP client to be set")
	}
}

// TestWithHTTPClient_NilClient tests that nil client is ignored.
func TestWithHTTPClient_NilClient(t *testing.T) {
	e := NewEvaluator(nil, WithHTTPClient(nil))

	if e.httpClient == nil {
		t.Error("expected default HTTP client when nil is passed")
	}
	if e.httpClient.Timeout != DefaultTimeout {
		t.Error("expected default timeout when nil client is passed")
	}
}

// TestWithTimeout tests the WithTimeout option.
func TestWithTimeout(t *testing.T) {
	customTimeout := 45 * time.Second

	e := NewEvaluator(nil, WithTimeout(customTimeout))

	if e.httpClient.Timeout != customTimeout {
		t.Errorf("expected timeout=%v, got %v", customTimeout, e.httpClient.Timeout)
	}
}

// TestWithTimeout_ZeroOrNegative tests that zero/negative timeout is ignored.
func TestWithTimeout_ZeroOrNegative(t *testing.T) {
	tests := []struct {
		name    string
		timeout time.Duration
	}{
		{name: "zero timeout", timeout: 0},
		{name: "negative timeout", timeout: -5 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := NewEvaluator(nil, WithTimeout(tt.timeout))

			if e.httpClient.Timeout != DefaultTimeout {
				t.Errorf("expected default timeout=%v, got %v", DefaultTimeout, e.httpClient.Timeout)
			}
		})
	}
}

// TestWithHTTPClientAndTimeout tests combining both options.
func TestWithHTTPClientAndTimeout(t *testing.T) {
	customClient := &http.Client{
		Timeout: 10 * time.Second,
	}
	newTimeout := 120 * time.Second

	// WithTimeout should modify the custom client's timeout
	e := NewEvaluator(nil, WithHTTPClient(customClient), WithTimeout(newTimeout))

	if e.httpClient != customClient {
		t.Error("expected custom HTTP client to be set")
	}
	if e.httpClient.Timeout != newTimeout {
		t.Errorf("expected timeout=%v, got %v", newTimeout, e.httpClient.Timeout)
	}
}

// TestEvaluate_AnthropicAPIError tests error handling for Anthropic API errors.
func TestEvaluate_AnthropicAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]string{
				"type":    "invalid_request_error",
				"message": "Invalid API key",
			},
		})
	}))
	defer server.Close()

	cfg := &Config{
		Enabled:  true,
		Provider: "anthropic",
		APIKey:   "invalid-key",
		Model:    "claude-3-haiku-20240307",
	}

	e := NewEvaluator(cfg)
	e.httpClient = &http.Client{
		Transport: &mockTransport{
			url:      server.URL,
			delegate: http.DefaultTransport,
		},
	}

	result, err := e.Evaluate(context.Background(), "test", "test")

	if err == nil {
		t.Error("expected error for API error response")
	}
	if result != nil {
		t.Error("expected nil result for API error response")
	}
}

// TestEvaluate_OpenAIAPIError tests error handling for OpenAI API errors.
func TestEvaluate_OpenAIAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]string{
				"type":    "authentication_error",
				"message": "Invalid API key",
			},
		})
	}))
	defer server.Close()

	cfg := &Config{
		Enabled:  true,
		Provider: "openai",
		APIKey:   "invalid-key",
		Model:    "gpt-4o-mini",
	}

	e := NewEvaluator(cfg)
	e.httpClient = &http.Client{
		Transport: &mockTransport{
			url:      server.URL,
			delegate: http.DefaultTransport,
		},
	}

	result, err := e.Evaluate(context.Background(), "test", "test")

	if err == nil {
		t.Error("expected error for API error response")
	}
	if result != nil {
		t.Error("expected nil result for API error response")
	}
}

// TestEvaluate_EmptyAnthropicResponse tests handling of empty Anthropic response.
func TestEvaluate_EmptyAnthropicResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := anthropicResponse{
			Content: []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}{},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cfg := &Config{
		Enabled:  true,
		Provider: "anthropic",
		APIKey:   "test-key",
		Model:    "claude-3-haiku-20240307",
	}

	e := NewEvaluator(cfg)
	e.httpClient = &http.Client{
		Transport: &mockTransport{
			url:      server.URL,
			delegate: http.DefaultTransport,
		},
	}

	result, err := e.Evaluate(context.Background(), "test", "test")

	if err == nil {
		t.Error("expected error for empty content response")
	}
	if !strings.Contains(err.Error(), "empty content") {
		t.Errorf("expected error to mention 'empty content', got %v", err)
	}
	if result != nil {
		t.Error("expected nil result")
	}
}

// TestEvaluate_EmptyOpenAIResponse tests handling of empty OpenAI response.
func TestEvaluate_EmptyOpenAIResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := openaiResponse{
			Choices: []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			}{},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cfg := &Config{
		Enabled:  true,
		Provider: "openai",
		APIKey:   "test-key",
		Model:    "gpt-4o-mini",
	}

	e := NewEvaluator(cfg)
	e.httpClient = &http.Client{
		Transport: &mockTransport{
			url:      server.URL,
			delegate: http.DefaultTransport,
		},
	}

	result, err := e.Evaluate(context.Background(), "test", "test")

	if err == nil {
		t.Error("expected error for no choices response")
	}
	if !strings.Contains(err.Error(), "no choices") {
		t.Errorf("expected error to mention 'no choices', got %v", err)
	}
	if result != nil {
		t.Error("expected nil result")
	}
}

// TestEvaluate_ProviderCaseInsensitive tests that provider names are case-insensitive.
func TestEvaluate_ProviderCaseInsensitive(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := anthropicResponse{
			Content: []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}{
				{Type: "text", Text: `{"state": "complete", "summary": "Done", "confidence": 0.9}`},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	providers := []string{"ANTHROPIC", "Anthropic", "AnThRoPiC"}

	for _, provider := range providers {
		t.Run(provider, func(t *testing.T) {
			cfg := &Config{
				Enabled:  true,
				Provider: provider,
				APIKey:   "test-key",
				Model:    "claude-3-haiku-20240307",
			}

			e := NewEvaluator(cfg)
			e.httpClient = &http.Client{
				Transport: &mockTransport{
					url:      server.URL,
					delegate: http.DefaultTransport,
				},
			}

			result, err := e.Evaluate(context.Background(), "test", "test")

			if err != nil {
				t.Fatalf("unexpected error for provider %s: %v", provider, err)
			}
			if result == nil {
				t.Fatalf("expected non-nil result for provider %s", provider)
			}
		})
	}
}

// mockTransport is a custom RoundTripper that redirects requests to a mock server.
type mockTransport struct {
	url      string
	delegate http.RoundTripper
}

func (t *mockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Replace the URL with mock server URL
	req.URL.Scheme = "http"
	req.URL.Host = strings.TrimPrefix(t.url, "http://")
	return t.delegate.RoundTrip(req)
}
