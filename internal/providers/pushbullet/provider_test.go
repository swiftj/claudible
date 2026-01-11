package pushbullet

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/swiftj/claudible/internal/config"
	"github.com/swiftj/claudible/internal/notification"
	"github.com/swiftj/claudible/internal/state"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name       string
		enabled    bool
		apiKey     string
		deviceIden string
	}{
		{
			name:       "basic enabled provider",
			enabled:    true,
			apiKey:     "test-api-key",
			deviceIden: "device-123",
		},
		{
			name:       "disabled provider",
			enabled:    false,
			apiKey:     "",
			deviceIden: "",
		},
		{
			name:       "enabled without device",
			enabled:    true,
			apiKey:     "test-api-key",
			deviceIden: "",
		},
		{
			name:       "enabled without api key",
			enabled:    true,
			apiKey:     "",
			deviceIden: "device-123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := New(tt.enabled, tt.apiKey, tt.deviceIden)

			if p.Enabled() != tt.enabled {
				t.Errorf("Enabled() = %v, want %v", p.Enabled(), tt.enabled)
			}
			if p.apiKey != tt.apiKey {
				t.Errorf("apiKey = %v, want %v", p.apiKey, tt.apiKey)
			}
			if p.deviceIden != tt.deviceIden {
				t.Errorf("deviceIden = %v, want %v", p.deviceIden, tt.deviceIden)
			}
			if p.httpClient == nil {
				t.Error("httpClient should not be nil")
			}
			if p.httpClient.Timeout != defaultTimeout {
				t.Errorf("httpClient.Timeout = %v, want %v", p.httpClient.Timeout, defaultTimeout)
			}
		})
	}
}

func TestNewFromConfig(t *testing.T) {
	tests := []struct {
		name string
		cfg  *config.PushbulletConfig
		want struct {
			enabled    bool
			apiKey     string
			deviceIden string
		}
	}{
		{
			name: "valid config with all fields",
			cfg: &config.PushbulletConfig{
				Enabled:    true,
				APIKey:     "my-api-key",
				DeviceIden: "device-abc",
			},
			want: struct {
				enabled    bool
				apiKey     string
				deviceIden string
			}{true, "my-api-key", "device-abc"},
		},
		{
			name: "valid config without device",
			cfg: &config.PushbulletConfig{
				Enabled:    true,
				APIKey:     "my-api-key",
				DeviceIden: "",
			},
			want: struct {
				enabled    bool
				apiKey     string
				deviceIden string
			}{true, "my-api-key", ""},
		},
		{
			name: "nil config",
			cfg:  nil,
			want: struct {
				enabled    bool
				apiKey     string
				deviceIden string
			}{false, "", ""},
		},
		{
			name: "disabled config",
			cfg: &config.PushbulletConfig{
				Enabled:    false,
				APIKey:     "unused-key",
				DeviceIden: "unused-device",
			},
			want: struct {
				enabled    bool
				apiKey     string
				deviceIden string
			}{false, "unused-key", "unused-device"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewFromConfig(tt.cfg)

			if p.Enabled() != tt.want.enabled {
				t.Errorf("Enabled() = %v, want %v", p.Enabled(), tt.want.enabled)
			}
			if p.apiKey != tt.want.apiKey {
				t.Errorf("apiKey = %v, want %v", p.apiKey, tt.want.apiKey)
			}
			if p.deviceIden != tt.want.deviceIden {
				t.Errorf("deviceIden = %v, want %v", p.deviceIden, tt.want.deviceIden)
			}
		})
	}
}

func TestProviderName(t *testing.T) {
	p := New(true, "test-key", "")
	if got := p.Name(); got != "pushbullet" {
		t.Errorf("Name() = %v, want pushbullet", got)
	}
}

func TestEnabled(t *testing.T) {
	tests := []struct {
		name    string
		enabled bool
	}{
		{"enabled provider", true},
		{"disabled provider", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := New(tt.enabled, "test-key", "")
			if got := p.Enabled(); got != tt.enabled {
				t.Errorf("Enabled() = %v, want %v", got, tt.enabled)
			}
		})
	}
}

func TestWithClient(t *testing.T) {
	customClient := &http.Client{Timeout: 60 * time.Second}
	p := New(true, "test-key", "", WithClient(customClient))

	if p.httpClient != customClient {
		t.Error("WithClient option did not set the custom client")
	}
}

func TestWithClientNil(t *testing.T) {
	p := New(true, "test-key", "", WithClient(nil))

	if p.httpClient == nil {
		t.Error("WithClient(nil) should not set httpClient to nil")
	}
	if p.httpClient.Timeout != defaultTimeout {
		t.Errorf("httpClient.Timeout = %v, want %v (default)", p.httpClient.Timeout, defaultTimeout)
	}
}

func TestWithTimeout(t *testing.T) {
	timeout := 10 * time.Second
	p := New(true, "test-key", "", WithTimeout(timeout))

	if p.httpClient.Timeout != timeout {
		t.Errorf("httpClient.Timeout = %v, want %v", p.httpClient.Timeout, timeout)
	}
}

func TestWithTimeoutZero(t *testing.T) {
	p := New(true, "test-key", "", WithTimeout(0))

	if p.httpClient.Timeout != defaultTimeout {
		t.Errorf("httpClient.Timeout = %v, want %v (default)", p.httpClient.Timeout, defaultTimeout)
	}
}

func TestWithTimeoutNegative(t *testing.T) {
	p := New(true, "test-key", "", WithTimeout(-5*time.Second))

	if p.httpClient.Timeout != defaultTimeout {
		t.Errorf("httpClient.Timeout = %v, want %v (default)", p.httpClient.Timeout, defaultTimeout)
	}
}

func TestSendDisabled(t *testing.T) {
	p := New(false, "test-key", "")

	msg := notification.NotificationMessage{
		Title:   "Test Title",
		State:   state.StateComplete,
		Summary: "Test summary",
	}

	err := p.Send(context.Background(), msg)
	if err != nil {
		t.Errorf("Send() on disabled provider should return nil, got %v", err)
	}
}

func TestSendMissingAPIKey(t *testing.T) {
	p := New(true, "", "")

	msg := notification.NotificationMessage{
		Title:   "Test Title",
		State:   state.StateComplete,
		Summary: "Test summary",
	}

	err := p.Send(context.Background(), msg)
	if err == nil {
		t.Error("Send() with missing API key should return error")
	}
	if !strings.Contains(err.Error(), "API key is not configured") {
		t.Errorf("Error message should mention API key, got: %v", err)
	}
}

func TestSendSuccess(t *testing.T) {
	var receivedBody []byte
	var receivedHeaders http.Header
	var receivedMethod string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedMethod = r.Method
		receivedHeaders = r.Header.Clone()
		body, _ := io.ReadAll(r.Body)
		receivedBody = body

		// Return a successful response with an identifier
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(pushResponse{Iden: "push-12345"})
	}))
	defer server.Close()

	// Create provider with custom client that points to our test server
	customClient := &http.Client{
		Transport: &testTransport{serverURL: server.URL},
	}
	p := New(true, "test-api-key", "", WithClient(customClient))

	msg := notification.NotificationMessage{
		Title:     "Test Title",
		State:     state.StateComplete,
		Summary:   "Task completed successfully",
		Request:   "run tests",
		Cwd:       "/home/user/project",
		SessionID: "session-123",
		Timestamp: time.Now(),
	}

	err := p.Send(context.Background(), msg)
	if err != nil {
		t.Errorf("Send() error = %v, want nil", err)
	}

	// Verify request method
	if receivedMethod != http.MethodPost {
		t.Errorf("request method = %v, want POST", receivedMethod)
	}

	// Verify headers
	if receivedHeaders.Get("Access-Token") != "test-api-key" {
		t.Errorf("Access-Token = %v, want test-api-key", receivedHeaders.Get("Access-Token"))
	}
	if receivedHeaders.Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type = %v, want application/json", receivedHeaders.Get("Content-Type"))
	}

	// Verify request body
	var req pushRequest
	if err := json.Unmarshal(receivedBody, &req); err != nil {
		t.Fatalf("Failed to unmarshal request body: %v", err)
	}
	if req.Type != "note" {
		t.Errorf("request type = %v, want note", req.Type)
	}
	if !strings.Contains(req.Title, "Complete") {
		t.Errorf("request title should contain 'Complete', got: %v", req.Title)
	}
	if !strings.Contains(req.Body, "Task completed successfully") {
		t.Errorf("request body should contain summary, got: %v", req.Body)
	}
}

func TestSendWithDeviceIden(t *testing.T) {
	var receivedBody []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		receivedBody = body

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(pushResponse{Iden: "push-12345"})
	}))
	defer server.Close()

	customClient := &http.Client{
		Transport: &testTransport{serverURL: server.URL},
	}
	p := New(true, "test-api-key", "my-device-123", WithClient(customClient))

	msg := notification.NotificationMessage{
		Title:   "Test",
		State:   state.StateComplete,
		Summary: "Test summary",
	}

	err := p.Send(context.Background(), msg)
	if err != nil {
		t.Errorf("Send() error = %v, want nil", err)
	}

	// Verify device_iden is included in request
	var req pushRequest
	if err := json.Unmarshal(receivedBody, &req); err != nil {
		t.Fatalf("Failed to unmarshal request body: %v", err)
	}
	if req.DeviceIden != "my-device-123" {
		t.Errorf("request device_iden = %v, want my-device-123", req.DeviceIden)
	}
}

func TestSendWithoutDeviceIden(t *testing.T) {
	var receivedBody []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		receivedBody = body

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(pushResponse{Iden: "push-12345"})
	}))
	defer server.Close()

	customClient := &http.Client{
		Transport: &testTransport{serverURL: server.URL},
	}
	p := New(true, "test-api-key", "", WithClient(customClient))

	msg := notification.NotificationMessage{
		Title:   "Test",
		State:   state.StateComplete,
		Summary: "Test summary",
	}

	err := p.Send(context.Background(), msg)
	if err != nil {
		t.Errorf("Send() error = %v, want nil", err)
	}

	// Verify device_iden is empty/omitted (should be empty string which omits from JSON)
	var req pushRequest
	if err := json.Unmarshal(receivedBody, &req); err != nil {
		t.Fatalf("Failed to unmarshal request body: %v", err)
	}
	if req.DeviceIden != "" {
		t.Errorf("request device_iden should be empty, got: %v", req.DeviceIden)
	}
}

func TestSendAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK) // Pushbullet can return 200 with error in body
		json.NewEncoder(w).Encode(pushResponse{
			Error: struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			}{
				Code:    "invalid_access_token",
				Message: "Access token is invalid",
			},
		})
	}))
	defer server.Close()

	customClient := &http.Client{
		Transport: &testTransport{serverURL: server.URL},
	}
	p := New(true, "invalid-key", "", WithClient(customClient))

	msg := notification.NotificationMessage{
		Title:   "Test",
		State:   state.StateComplete,
		Summary: "Test summary",
	}

	err := p.Send(context.Background(), msg)
	if err == nil {
		t.Error("Send() with API error should return error")
	}
	if !strings.Contains(err.Error(), "invalid_access_token") {
		t.Errorf("Error should contain error code, got: %v", err)
	}
}

func TestSendHTTPError(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
	}{
		{
			name:       "400 Bad Request",
			statusCode: http.StatusBadRequest,
			body:       `{"error": {"code": "bad_request", "message": "Invalid request"}}`,
		},
		{
			name:       "401 Unauthorized",
			statusCode: http.StatusUnauthorized,
			body:       `{"error": {"code": "unauthorized", "message": "Invalid access token"}}`,
		},
		{
			name:       "403 Forbidden",
			statusCode: http.StatusForbidden,
			body:       `{"error": {"code": "forbidden", "message": "Access denied"}}`,
		},
		{
			name:       "429 Too Many Requests",
			statusCode: http.StatusTooManyRequests,
			body:       `{"error": {"code": "rate_limited", "message": "Rate limit exceeded"}}`,
		},
		{
			name:       "500 Internal Server Error",
			statusCode: http.StatusInternalServerError,
			body:       `{"error": {"code": "server_error", "message": "Internal error"}}`,
		},
		{
			name:       "503 Service Unavailable",
			statusCode: http.StatusServiceUnavailable,
			body:       `{"error": {"code": "service_unavailable", "message": "Service temporarily unavailable"}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.body))
			}))
			defer server.Close()

			customClient := &http.Client{
				Transport: &testTransport{serverURL: server.URL},
			}
			p := New(true, "test-key", "", WithClient(customClient))

			msg := notification.NotificationMessage{
				Title:   "Test",
				State:   state.StateComplete,
				Summary: "Test summary",
			}

			err := p.Send(context.Background(), msg)
			if err == nil {
				t.Errorf("Send() should return error for status %d", tt.statusCode)
			}
		})
	}
}

func TestSendNoIdentifierReturned(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// Return empty iden - indicates something went wrong
		json.NewEncoder(w).Encode(pushResponse{Iden: ""})
	}))
	defer server.Close()

	customClient := &http.Client{
		Transport: &testTransport{serverURL: server.URL},
	}
	p := New(true, "test-key", "", WithClient(customClient))

	msg := notification.NotificationMessage{
		Title:   "Test",
		State:   state.StateComplete,
		Summary: "Test summary",
	}

	err := p.Send(context.Background(), msg)
	if err == nil {
		t.Error("Send() with no push identifier should return error")
	}
	if !strings.Contains(err.Error(), "no push identifier returned") {
		t.Errorf("Error should mention missing identifier, got: %v", err)
	}
}

func TestSendContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	customClient := &http.Client{
		Transport: &testTransport{serverURL: server.URL},
	}
	p := New(true, "test-key", "", WithClient(customClient))

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	msg := notification.NotificationMessage{
		Title:   "Test",
		State:   state.StateComplete,
		Summary: "Test summary",
	}

	err := p.Send(ctx, msg)
	if err == nil {
		t.Error("Send() with cancelled context should return error")
	}
}

func TestSendContextTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	customClient := &http.Client{
		Transport: &testTransport{serverURL: server.URL},
		Timeout:   100 * time.Millisecond,
	}
	p := New(true, "test-key", "", WithClient(customClient))

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	msg := notification.NotificationMessage{
		Title:   "Test",
		State:   state.StateComplete,
		Summary: "Test summary",
	}

	err := p.Send(ctx, msg)
	if err == nil {
		t.Error("Send() with timed out context should return error")
	}
}

func TestSendConnectionError(t *testing.T) {
	// Use a transport that points to a non-existent server
	customClient := &http.Client{
		Transport: &testTransport{serverURL: "http://localhost:59999"},
		Timeout:   100 * time.Millisecond,
	}
	p := New(true, "test-key", "", WithClient(customClient))

	msg := notification.NotificationMessage{
		Title:   "Test",
		State:   state.StateComplete,
		Summary: "Test summary",
	}

	err := p.Send(context.Background(), msg)
	if err == nil {
		t.Error("Send() to non-existent endpoint should return error")
	}
}

func TestSendInvalidJSONResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("not valid json"))
	}))
	defer server.Close()

	customClient := &http.Client{
		Transport: &testTransport{serverURL: server.URL},
	}
	p := New(true, "test-key", "", WithClient(customClient))

	msg := notification.NotificationMessage{
		Title:   "Test",
		State:   state.StateComplete,
		Summary: "Test summary",
	}

	err := p.Send(context.Background(), msg)
	if err == nil {
		t.Error("Send() with invalid JSON response should return error")
	}
	if !strings.Contains(err.Error(), "failed to parse response") {
		t.Errorf("Error should mention parse failure, got: %v", err)
	}
}

func TestSend2xxStatusCodes(t *testing.T) {
	successCodes := []int{200, 201, 202}

	for _, code := range successCodes {
		t.Run(http.StatusText(code), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(code)
				json.NewEncoder(w).Encode(pushResponse{Iden: "push-success"})
			}))
			defer server.Close()

			customClient := &http.Client{
				Transport: &testTransport{serverURL: server.URL},
			}
			p := New(true, "test-key", "", WithClient(customClient))

			msg := notification.NotificationMessage{
				Title:   "Test",
				State:   state.StateComplete,
				Summary: "Test summary",
			}

			err := p.Send(context.Background(), msg)
			if err != nil {
				t.Errorf("Send() with status %d should not return error, got %v", code, err)
			}
		})
	}
}

func TestFormatTitle(t *testing.T) {
	tests := []struct {
		name     string
		state    state.State
		expected string
	}{
		{
			name:     "complete state",
			state:    state.StateComplete,
			expected: "Claude Code: Complete",
		},
		{
			name:     "waiting state",
			state:    state.StateWaiting,
			expected: "Claude Code: Waiting",
		},
		{
			name:     "stopped state",
			state:    state.StateStopped,
			expected: "Claude Code: Stopped",
		},
		{
			name:     "idle state",
			state:    state.StateIdle,
			expected: "Claude Code: Idle",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := notification.NotificationMessage{
				State: tt.state,
			}
			title := formatTitle(msg)
			if title != tt.expected {
				t.Errorf("formatTitle() = %v, want %v", title, tt.expected)
			}
		})
	}
}

func TestFormatBody(t *testing.T) {
	tests := []struct {
		name     string
		msg      notification.NotificationMessage
		contains []string
	}{
		{
			name: "full message",
			msg: notification.NotificationMessage{
				Summary: "Task completed",
				Cwd:     "/home/user/project",
				Request: "run tests",
			},
			contains: []string{"Task completed", "Project: /home/user/project", "Request: run tests"},
		},
		{
			name: "summary only",
			msg: notification.NotificationMessage{
				Summary: "Task completed",
			},
			contains: []string{"Task completed"},
		},
		{
			name: "long request truncated",
			msg: notification.NotificationMessage{
				Summary: "Task completed",
				Request: strings.Repeat("x", 150),
			},
			contains: []string{"Task completed", "Request:", "..."},
		},
		{
			name: "empty summary",
			msg: notification.NotificationMessage{
				Cwd:     "/home/user/project",
				Request: "test",
			},
			contains: []string{"Project: /home/user/project", "Request: test"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := formatBody(tt.msg)
			for _, substr := range tt.contains {
				if !strings.Contains(body, substr) {
					t.Errorf("formatBody() = %v, should contain %v", body, substr)
				}
			}
		})
	}
}

func TestTitleCase(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"complete", "Complete"},
		{"waiting", "Waiting"},
		{"ALREADY_UPPER", "ALREADY_UPPER"},
		{"", ""},
		{"a", "A"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := titleCase(tt.input)
			if result != tt.expected {
				t.Errorf("titleCase(%v) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestProviderImplementsInterface(t *testing.T) {
	var _ notification.Provider = (*Provider)(nil)
}

func TestNewFromConfigWithOptions(t *testing.T) {
	cfg := &config.PushbulletConfig{
		Enabled:    true,
		APIKey:     "test-key",
		DeviceIden: "device-123",
	}

	customClient := &http.Client{Timeout: 60 * time.Second}
	p := NewFromConfig(cfg, WithClient(customClient), WithTimeout(45*time.Second))

	if p.httpClient != customClient {
		t.Error("WithClient option was not applied with NewFromConfig")
	}
	if p.httpClient.Timeout != 45*time.Second {
		t.Errorf("WithTimeout option was not applied, got %v", p.httpClient.Timeout)
	}
}

func TestNewWithMultipleOptions(t *testing.T) {
	customClient := &http.Client{Timeout: 60 * time.Second}
	p := New(true, "key", "device",
		WithClient(customClient),
		WithTimeout(15*time.Second),
	)

	if p.httpClient != customClient {
		t.Error("WithClient option was not applied")
	}
	if p.httpClient.Timeout != 15*time.Second {
		t.Errorf("WithTimeout option was not applied correctly, got %v", p.httpClient.Timeout)
	}
}

// testTransport is a custom HTTP transport that redirects all requests to a test server.
type testTransport struct {
	serverURL string
}

func (t *testTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Redirect the request to our test server
	req.URL.Scheme = "http"
	req.URL.Host = strings.TrimPrefix(t.serverURL, "http://")
	return http.DefaultTransport.RoundTrip(req)
}
