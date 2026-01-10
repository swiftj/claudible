package sms

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/johnswift/claudible/internal/config"
	"github.com/johnswift/claudible/internal/notification"
	"github.com/johnswift/claudible/internal/state"
)

// MockBackend is a mock SMS backend for testing.
type MockBackend struct {
	name      string
	sendFunc  func(ctx context.Context, to, from, body string) error
	sendCalls []SendCall
}

// SendCall records a call to Send.
type SendCall struct {
	To   string
	From string
	Body string
}

func NewMockBackend(name string) *MockBackend {
	return &MockBackend{
		name:      name,
		sendCalls: make([]SendCall, 0),
	}
}

func (m *MockBackend) Name() string {
	return m.name
}

func (m *MockBackend) Send(ctx context.Context, to, from, body string) error {
	m.sendCalls = append(m.sendCalls, SendCall{To: to, From: from, Body: body})
	if m.sendFunc != nil {
		return m.sendFunc(ctx, to, from, body)
	}
	return nil
}

func TestProvider_Name(t *testing.T) {
	p := New(true, "+1234567890", "+0987654321", NewMockBackend("mock"))
	if p.Name() != "sms" {
		t.Errorf("expected name 'sms', got '%s'", p.Name())
	}
}

func TestProvider_Enabled(t *testing.T) {
	tests := []struct {
		name     string
		enabled  bool
		backend  SMSBackend
		expected bool
	}{
		{
			name:     "enabled with backend",
			enabled:  true,
			backend:  NewMockBackend("mock"),
			expected: true,
		},
		{
			name:     "disabled with backend",
			enabled:  false,
			backend:  NewMockBackend("mock"),
			expected: false,
		},
		{
			name:     "enabled without backend",
			enabled:  true,
			backend:  nil,
			expected: false,
		},
		{
			name:     "disabled without backend",
			enabled:  false,
			backend:  nil,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := New(tt.enabled, "+1234567890", "+0987654321", tt.backend)
			if p.Enabled() != tt.expected {
				t.Errorf("expected Enabled()=%v, got %v", tt.expected, p.Enabled())
			}
		})
	}
}

func TestProvider_Send(t *testing.T) {
	mock := NewMockBackend("mock")
	p := New(true, "+1234567890", "+0987654321", mock)

	msg := notification.NotificationMessage{
		Title:     "Claude Code - Complete",
		State:     state.StateComplete,
		Summary:   "Task completed successfully",
		Request:   "Build the project",
		Cwd:       "/home/user/project",
		SessionID: "abc123",
		Timestamp: time.Now(),
	}

	err := p.Send(context.Background(), msg)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(mock.sendCalls) != 1 {
		t.Fatalf("expected 1 send call, got %d", len(mock.sendCalls))
	}

	call := mock.sendCalls[0]
	if call.To != "+1234567890" {
		t.Errorf("expected to '+1234567890', got '%s'", call.To)
	}
	if call.From != "+0987654321" {
		t.Errorf("expected from '+0987654321', got '%s'", call.From)
	}
	if call.Body != "[complete] Task completed successfully" {
		t.Errorf("unexpected body: '%s'", call.Body)
	}
}

func TestProvider_Send_Disabled(t *testing.T) {
	mock := NewMockBackend("mock")
	p := New(false, "+1234567890", "+0987654321", mock)

	msg := notification.NotificationMessage{
		State:   state.StateComplete,
		Summary: "Test",
	}

	err := p.Send(context.Background(), msg)
	if err == nil {
		t.Fatal("expected error for disabled provider")
	}
}

func TestProvider_Send_BackendError(t *testing.T) {
	mock := NewMockBackend("mock")
	mock.sendFunc = func(ctx context.Context, to, from, body string) error {
		return errors.New("backend error")
	}

	p := New(true, "+1234567890", "+0987654321", mock)

	msg := notification.NotificationMessage{
		State:   state.StateComplete,
		Summary: "Test",
	}

	err := p.Send(context.Background(), msg)
	if err == nil {
		t.Fatal("expected error from backend")
	}
	if err.Error() != "backend error" {
		t.Errorf("expected 'backend error', got '%s'", err.Error())
	}
}

func TestFormatMessage(t *testing.T) {
	tests := []struct {
		name     string
		msg      notification.NotificationMessage
		expected string
	}{
		{
			name: "short message",
			msg: notification.NotificationMessage{
				State:   state.StateComplete,
				Summary: "Task done",
			},
			expected: "[complete] Task done",
		},
		{
			name: "waiting state",
			msg: notification.NotificationMessage{
				State:   state.StateWaiting,
				Summary: "Waiting for input",
			},
			expected: "[waiting] Waiting for input",
		},
		{
			name: "long message truncated",
			msg: notification.NotificationMessage{
				State:   state.StateComplete,
				Summary: "This is a very long message that exceeds the maximum SMS length of 160 characters and should be truncated with ellipsis at the end to fit within the limit properly",
			},
			expected: "[complete] This is a very long message that exceeds the maximum SMS length of 160 characters and should be truncated with ellipsis at the end to fit within t...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatMessage(tt.msg)
			if result != tt.expected {
				t.Errorf("expected '%s', got '%s'", tt.expected, result)
			}
			if len(result) > maxSMSLength {
				t.Errorf("result exceeds max length: %d > %d", len(result), maxSMSLength)
			}
		})
	}
}

func TestNewFromConfig(t *testing.T) {
	tests := []struct {
		name            string
		cfg             *config.SMSConfig
		expectedEnabled bool
		expectedBackend string
	}{
		{
			name:            "nil config",
			cfg:             nil,
			expectedEnabled: false,
		},
		{
			name: "twilio provider",
			cfg: &config.SMSConfig{
				Enabled:    true,
				Provider:   "twilio",
				From:       "+1234567890",
				To:         "+0987654321",
				AccountSID: "ACxxx",
				AuthToken:  "token",
			},
			expectedEnabled: true,
			expectedBackend: "twilio",
		},
		{
			name: "disabled provider",
			cfg: &config.SMSConfig{
				Enabled:    false,
				Provider:   "twilio",
				From:       "+1234567890",
				To:         "+0987654321",
				AccountSID: "ACxxx",
				AuthToken:  "token",
			},
			expectedEnabled: false,
			expectedBackend: "twilio",
		},
		{
			name: "unknown provider defaults to twilio",
			cfg: &config.SMSConfig{
				Enabled:    true,
				Provider:   "unknown",
				From:       "+1234567890",
				To:         "+0987654321",
				AccountSID: "ACxxx",
				AuthToken:  "token",
			},
			expectedEnabled: true,
			expectedBackend: "twilio",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewFromConfig(tt.cfg)
			if p.Enabled() != tt.expectedEnabled {
				t.Errorf("expected Enabled()=%v, got %v", tt.expectedEnabled, p.Enabled())
			}
			if tt.expectedBackend != "" && p.backend != nil {
				if p.backend.Name() != tt.expectedBackend {
					t.Errorf("expected backend '%s', got '%s'", tt.expectedBackend, p.backend.Name())
				}
			}
		})
	}
}

func TestTwilioBackend_Name(t *testing.T) {
	b := NewTwilioBackend("ACxxx", "token")
	if b.Name() != "twilio" {
		t.Errorf("expected name 'twilio', got '%s'", b.Name())
	}
}

// =============================================================================
// Vonage Backend Tests
// =============================================================================

func TestVonageBackend_Name(t *testing.T) {
	b := NewVonageBackend("api_key", "api_secret")
	if b.Name() != "vonage" {
		t.Errorf("expected name 'vonage', got '%s'", b.Name())
	}
}

func TestVonageBackend_Creation(t *testing.T) {
	b := NewVonageBackend("test_api_key", "test_api_secret")

	if b.apiKey != "test_api_key" {
		t.Errorf("expected apiKey 'test_api_key', got '%s'", b.apiKey)
	}
	if b.apiSecret != "test_api_secret" {
		t.Errorf("expected apiSecret 'test_api_secret', got '%s'", b.apiSecret)
	}
	if b.baseURL != "https://rest.nexmo.com" {
		t.Errorf("expected baseURL 'https://rest.nexmo.com', got '%s'", b.baseURL)
	}
	if b.httpClient == nil {
		t.Error("expected httpClient to be set")
	}
}

func TestVonageBackend_Send_Success(t *testing.T) {
	// Create a mock server that returns a successful response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request method and content type
		if r.Method != http.MethodPost {
			t.Errorf("expected POST method, got %s", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
			t.Errorf("expected Content-Type 'application/x-www-form-urlencoded', got '%s'", r.Header.Get("Content-Type"))
		}

		// Parse form data
		if err := r.ParseForm(); err != nil {
			t.Fatalf("failed to parse form: %v", err)
		}

		// Verify form values
		if r.Form.Get("api_key") != "test_api_key" {
			t.Errorf("expected api_key 'test_api_key', got '%s'", r.Form.Get("api_key"))
		}
		if r.Form.Get("api_secret") != "test_api_secret" {
			t.Errorf("expected api_secret 'test_api_secret', got '%s'", r.Form.Get("api_secret"))
		}
		if r.Form.Get("from") != "+1234567890" {
			t.Errorf("expected from '+1234567890', got '%s'", r.Form.Get("from"))
		}
		if r.Form.Get("to") != "+0987654321" {
			t.Errorf("expected to '+0987654321', got '%s'", r.Form.Get("to"))
		}
		if r.Form.Get("text") != "Test message" {
			t.Errorf("expected text 'Test message', got '%s'", r.Form.Get("text"))
		}

		// Return successful response
		response := vonageResponse{
			MessageCount: "1",
			Messages: []vonageMessage{
				{
					Status:    "0",
					MessageID: "msg123",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	// Create backend with test server URL
	b := NewVonageBackend("test_api_key", "test_api_secret")
	b.baseURL = server.URL

	err := b.Send(context.Background(), "+0987654321", "+1234567890", "Test message")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestVonageBackend_Send_ErrorStatus(t *testing.T) {
	tests := []struct {
		name          string
		status        string
		errorText     string
		expectedError string
	}{
		{
			name:          "status 1 with error text",
			status:        "1",
			errorText:     "Throttled",
			expectedError: "vonage: message delivery failed (status 1): Throttled",
		},
		{
			name:          "status 2 without error text",
			status:        "2",
			errorText:     "",
			expectedError: "vonage: message delivery failed (status 2): unknown error",
		},
		{
			name:          "status 4 with error",
			status:        "4",
			errorText:     "Invalid credentials",
			expectedError: "vonage: message delivery failed (status 4): Invalid credentials",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				response := vonageResponse{
					MessageCount: "1",
					Messages: []vonageMessage{
						{
							Status:    tt.status,
							ErrorText: tt.errorText,
						},
					},
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(response)
			}))
			defer server.Close()

			b := NewVonageBackend("api_key", "api_secret")
			b.baseURL = server.URL

			err := b.Send(context.Background(), "+1234567890", "+0987654321", "Test")
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if err.Error() != tt.expectedError {
				t.Errorf("expected error '%s', got '%s'", tt.expectedError, err.Error())
			}
		})
	}
}

func TestVonageBackend_Send_EmptyMessages(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := vonageResponse{
			MessageCount: "0",
			Messages:     []vonageMessage{},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	b := NewVonageBackend("api_key", "api_secret")
	b.baseURL = server.URL

	err := b.Send(context.Background(), "+1234567890", "+0987654321", "Test")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "no messages in response") {
		t.Errorf("expected 'no messages in response' error, got '%s'", err.Error())
	}
}

func TestVonageBackend_Send_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Internal Server Error"))
	}))
	defer server.Close()

	b := NewVonageBackend("api_key", "api_secret")
	b.baseURL = server.URL

	err := b.Send(context.Background(), "+1234567890", "+0987654321", "Test")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "API error (status 500)") {
		t.Errorf("expected 'API error (status 500)' error, got '%s'", err.Error())
	}
}

func TestVonageBackend_Send_NetworkError(t *testing.T) {
	// Create backend with invalid URL to simulate network error
	b := NewVonageBackend("api_key", "api_secret")
	b.baseURL = "http://localhost:99999" // Invalid port

	err := b.Send(context.Background(), "+1234567890", "+0987654321", "Test")
	if err == nil {
		t.Fatal("expected network error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to send SMS") {
		t.Errorf("expected 'failed to send SMS' error, got '%s'", err.Error())
	}
}

func TestVonageBackend_Send_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("invalid json"))
	}))
	defer server.Close()

	b := NewVonageBackend("api_key", "api_secret")
	b.baseURL = server.URL

	err := b.Send(context.Background(), "+1234567890", "+0987654321", "Test")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to parse response") {
		t.Errorf("expected 'failed to parse response' error, got '%s'", err.Error())
	}
}

func TestVonageBackend_Send_ContextCanceled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond) // Delay response
		response := vonageResponse{
			MessageCount: "1",
			Messages:     []vonageMessage{{Status: "0"}},
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	b := NewVonageBackend("api_key", "api_secret")
	b.baseURL = server.URL

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err := b.Send(ctx, "+1234567890", "+0987654321", "Test")
	if err == nil {
		t.Fatal("expected context canceled error, got nil")
	}
}

// =============================================================================
// Plivo Backend Tests
// =============================================================================

func TestPlivoBackend_Name(t *testing.T) {
	b := NewPlivoBackend("auth_id", "auth_token")
	if b.Name() != "plivo" {
		t.Errorf("expected name 'plivo', got '%s'", b.Name())
	}
}

func TestPlivoBackend_Creation(t *testing.T) {
	b := NewPlivoBackend("test_auth_id", "test_auth_token")

	if b.authID != "test_auth_id" {
		t.Errorf("expected authID 'test_auth_id', got '%s'", b.authID)
	}
	if b.authToken != "test_auth_token" {
		t.Errorf("expected authToken 'test_auth_token', got '%s'", b.authToken)
	}
	if b.baseURL != "https://api.plivo.com/v1" {
		t.Errorf("expected baseURL 'https://api.plivo.com/v1', got '%s'", b.baseURL)
	}
	if b.httpClient == nil {
		t.Error("expected httpClient to be set")
	}
}

func TestPlivoBackend_Send_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request method and content type
		if r.Method != http.MethodPost {
			t.Errorf("expected POST method, got %s", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected Content-Type 'application/json', got '%s'", r.Header.Get("Content-Type"))
		}

		// Verify Basic Auth
		username, password, ok := r.BasicAuth()
		if !ok {
			t.Error("expected Basic Auth to be set")
		}
		if username != "test_auth_id" {
			t.Errorf("expected username 'test_auth_id', got '%s'", username)
		}
		if password != "test_auth_token" {
			t.Errorf("expected password 'test_auth_token', got '%s'", password)
		}

		// Verify URL path contains auth_id
		if !strings.Contains(r.URL.Path, "/Account/test_auth_id/Message/") {
			t.Errorf("expected URL to contain '/Account/test_auth_id/Message/', got '%s'", r.URL.Path)
		}

		// Parse and verify JSON body
		var payload plivoRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}

		if payload.Src != "+1234567890" {
			t.Errorf("expected src '+1234567890', got '%s'", payload.Src)
		}
		if payload.Dst != "+0987654321" {
			t.Errorf("expected dst '+0987654321', got '%s'", payload.Dst)
		}
		if payload.Text != "Test message" {
			t.Errorf("expected text 'Test message', got '%s'", payload.Text)
		}

		// Return successful response (202 Accepted)
		w.WriteHeader(http.StatusAccepted)
		w.Write([]byte(`{"message":"message(s) queued","message_uuid":["uuid123"]}`))
	}))
	defer server.Close()

	b := NewPlivoBackend("test_auth_id", "test_auth_token")
	b.baseURL = server.URL

	err := b.Send(context.Background(), "+0987654321", "+1234567890", "Test message")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestPlivoBackend_Send_ErrorResponse(t *testing.T) {
	tests := []struct {
		name          string
		statusCode    int
		responseBody  string
		expectedError string
	}{
		{
			name:          "400 with error field",
			statusCode:    http.StatusBadRequest,
			responseBody:  `{"error":"Invalid destination number"}`,
			expectedError: "plivo API error (status 400): Invalid destination number",
		},
		{
			name:          "401 with message field",
			statusCode:    http.StatusUnauthorized,
			responseBody:  `{"message":"Authentication Failed"}`,
			expectedError: "plivo API error (status 401): Authentication Failed",
		},
		{
			name:          "500 with plain text",
			statusCode:    http.StatusInternalServerError,
			responseBody:  "Internal Server Error",
			expectedError: "plivo API error (status 500): Internal Server Error",
		},
		{
			name:          "403 forbidden",
			statusCode:    http.StatusForbidden,
			responseBody:  `{"error":"Account disabled"}`,
			expectedError: "plivo API error (status 403): Account disabled",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.responseBody))
			}))
			defer server.Close()

			b := NewPlivoBackend("auth_id", "auth_token")
			b.baseURL = server.URL

			err := b.Send(context.Background(), "+1234567890", "+0987654321", "Test")
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if err.Error() != tt.expectedError {
				t.Errorf("expected error '%s', got '%s'", tt.expectedError, err.Error())
			}
		})
	}
}

func TestPlivoBackend_Send_NetworkError(t *testing.T) {
	// Create backend with invalid URL to simulate network error
	b := NewPlivoBackend("auth_id", "auth_token")
	b.baseURL = "http://localhost:99999" // Invalid port

	err := b.Send(context.Background(), "+1234567890", "+0987654321", "Test")
	if err == nil {
		t.Fatal("expected network error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to send SMS via Plivo") {
		t.Errorf("expected 'failed to send SMS via Plivo' error, got '%s'", err.Error())
	}
}

func TestPlivoBackend_Send_ContextCanceled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond) // Delay response
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	b := NewPlivoBackend("auth_id", "auth_token")
	b.baseURL = server.URL

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err := b.Send(ctx, "+1234567890", "+0987654321", "Test")
	if err == nil {
		t.Fatal("expected context canceled error, got nil")
	}
}

func TestPlivoBackend_Send_SuccessWithDifferentStatusCodes(t *testing.T) {
	// Test that any 2xx status code is treated as success
	successCodes := []int{200, 201, 202}

	for _, code := range successCodes {
		t.Run(http.StatusText(code), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(code)
				w.Write([]byte(`{"message":"success"}`))
			}))
			defer server.Close()

			b := NewPlivoBackend("auth_id", "auth_token")
			b.baseURL = server.URL

			err := b.Send(context.Background(), "+1234567890", "+0987654321", "Test")
			if err != nil {
				t.Fatalf("expected no error for status %d, got %v", code, err)
			}
		})
	}
}

// =============================================================================
// Integration Tests for NewFromConfig with Vonage and Plivo
// =============================================================================

func TestNewFromConfig_VonageProvider(t *testing.T) {
	cfg := &config.SMSConfig{
		Enabled:   true,
		Provider:  "vonage",
		From:      "+1234567890",
		To:        "+0987654321",
		APIKey:    "vonage_api_key",
		APISecret: "vonage_api_secret",
	}

	p := NewFromConfig(cfg)
	if !p.Enabled() {
		t.Error("expected provider to be enabled")
	}
	if p.backend == nil {
		t.Fatal("expected backend to be set")
	}
	if p.backend.Name() != "vonage" {
		t.Errorf("expected backend name 'vonage', got '%s'", p.backend.Name())
	}
}

func TestNewFromConfig_PlivoProvider(t *testing.T) {
	cfg := &config.SMSConfig{
		Enabled:   true,
		Provider:  "plivo",
		From:      "+1234567890",
		To:        "+0987654321",
		AuthID:    "plivo_auth_id",
		AuthToken: "plivo_auth_token",
	}

	p := NewFromConfig(cfg)
	if !p.Enabled() {
		t.Error("expected provider to be enabled")
	}
	if p.backend == nil {
		t.Fatal("expected backend to be set")
	}
	if p.backend.Name() != "plivo" {
		t.Errorf("expected backend name 'plivo', got '%s'", p.backend.Name())
	}
}
