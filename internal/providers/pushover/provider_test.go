package pushover

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/johnswift/claudible/internal/config"
	"github.com/johnswift/claudible/internal/notification"
	"github.com/johnswift/claudible/internal/state"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name         string
		enabled      bool
		appToken     string
		userKey      string
		priority     int
		wantEnabled  bool
		wantPriority int
	}{
		{
			name:         "basic provider",
			enabled:      true,
			appToken:     "test-app-token",
			userKey:      "test-user-key",
			priority:     0,
			wantEnabled:  true,
			wantPriority: 0,
		},
		{
			name:         "disabled provider",
			enabled:      false,
			appToken:     "",
			userKey:      "",
			priority:     0,
			wantEnabled:  false,
			wantPriority: 0,
		},
		{
			name:         "high priority",
			enabled:      true,
			appToken:     "token",
			userKey:      "key",
			priority:     2,
			wantEnabled:  true,
			wantPriority: 2,
		},
		{
			name:         "low priority",
			enabled:      true,
			appToken:     "token",
			userKey:      "key",
			priority:     -2,
			wantEnabled:  true,
			wantPriority: -2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := New(tt.enabled, tt.appToken, tt.userKey, tt.priority)

			if p.Enabled() != tt.wantEnabled {
				t.Errorf("Enabled() = %v, want %v", p.Enabled(), tt.wantEnabled)
			}
			if p.appToken != tt.appToken {
				t.Errorf("appToken = %v, want %v", p.appToken, tt.appToken)
			}
			if p.userKey != tt.userKey {
				t.Errorf("userKey = %v, want %v", p.userKey, tt.userKey)
			}
			if p.priority != tt.wantPriority {
				t.Errorf("priority = %v, want %v", p.priority, tt.wantPriority)
			}
			if p.httpClient == nil {
				t.Error("httpClient should not be nil")
			}
		})
	}
}

func TestNewPriorityClamping(t *testing.T) {
	tests := []struct {
		name         string
		priority     int
		wantPriority int
	}{
		{
			name:         "priority too low",
			priority:     -5,
			wantPriority: -2,
		},
		{
			name:         "priority too high",
			priority:     10,
			wantPriority: 2,
		},
		{
			name:         "priority at min boundary",
			priority:     -2,
			wantPriority: -2,
		},
		{
			name:         "priority at max boundary",
			priority:     2,
			wantPriority: 2,
		},
		{
			name:         "priority within range",
			priority:     1,
			wantPriority: 1,
		},
		{
			name:         "negative priority within range",
			priority:     -1,
			wantPriority: -1,
		},
		{
			name:         "zero priority",
			priority:     0,
			wantPriority: 0,
		},
		{
			name:         "extreme low priority",
			priority:     -100,
			wantPriority: -2,
		},
		{
			name:         "extreme high priority",
			priority:     100,
			wantPriority: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := New(true, "token", "key", tt.priority)

			if p.priority != tt.wantPriority {
				t.Errorf("priority = %v, want %v (input was %v)", p.priority, tt.wantPriority, tt.priority)
			}
		})
	}
}

func TestNewFromConfig(t *testing.T) {
	tests := []struct {
		name string
		cfg  *config.PushoverConfig
		want struct {
			enabled  bool
			appToken string
			userKey  string
			priority int
		}
	}{
		{
			name: "valid config",
			cfg: &config.PushoverConfig{
				Enabled:  true,
				AppToken: "app-token-123",
				UserKey:  "user-key-456",
				Priority: 1,
			},
			want: struct {
				enabled  bool
				appToken string
				userKey  string
				priority int
			}{true, "app-token-123", "user-key-456", 1},
		},
		{
			name: "nil config",
			cfg:  nil,
			want: struct {
				enabled  bool
				appToken string
				userKey  string
				priority int
			}{false, "", "", 0},
		},
		{
			name: "disabled config",
			cfg: &config.PushoverConfig{
				Enabled:  false,
				AppToken: "token",
				UserKey:  "key",
				Priority: 0,
			},
			want: struct {
				enabled  bool
				appToken string
				userKey  string
				priority int
			}{false, "token", "key", 0},
		},
		{
			name: "config with priority clamping needed",
			cfg: &config.PushoverConfig{
				Enabled:  true,
				AppToken: "token",
				UserKey:  "key",
				Priority: 5, // Should be clamped to 2
			},
			want: struct {
				enabled  bool
				appToken string
				userKey  string
				priority int
			}{true, "token", "key", 2},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewFromConfig(tt.cfg)

			if p.Enabled() != tt.want.enabled {
				t.Errorf("Enabled() = %v, want %v", p.Enabled(), tt.want.enabled)
			}
			if p.appToken != tt.want.appToken {
				t.Errorf("appToken = %v, want %v", p.appToken, tt.want.appToken)
			}
			if p.userKey != tt.want.userKey {
				t.Errorf("userKey = %v, want %v", p.userKey, tt.want.userKey)
			}
			if p.priority != tt.want.priority {
				t.Errorf("priority = %v, want %v", p.priority, tt.want.priority)
			}
		})
	}
}

func TestProviderName(t *testing.T) {
	p := New(true, "token", "key", 0)
	if got := p.Name(); got != "pushover" {
		t.Errorf("Name() = %v, want pushover", got)
	}
}

func TestProviderEnabled(t *testing.T) {
	tests := []struct {
		name    string
		enabled bool
		want    bool
	}{
		{
			name:    "enabled provider",
			enabled: true,
			want:    true,
		},
		{
			name:    "disabled provider",
			enabled: false,
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := New(tt.enabled, "token", "key", 0)
			if got := p.Enabled(); got != tt.want {
				t.Errorf("Enabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWithClient(t *testing.T) {
	customClient := &http.Client{Timeout: 60 * time.Second}
	p := New(true, "token", "key", 0, WithClient(customClient))

	if p.httpClient != customClient {
		t.Error("WithClient option did not set the custom client")
	}
}

func TestWithClientNil(t *testing.T) {
	p := New(true, "token", "key", 0, WithClient(nil))

	if p.httpClient == nil {
		t.Error("WithClient(nil) should not set httpClient to nil")
	}
}

func TestWithTimeout(t *testing.T) {
	timeout := 10 * time.Second
	p := New(true, "token", "key", 0, WithTimeout(timeout))

	if p.httpClient.Timeout != timeout {
		t.Errorf("httpClient.Timeout = %v, want %v", p.httpClient.Timeout, timeout)
	}
}

func TestWithTimeoutZero(t *testing.T) {
	p := New(true, "token", "key", 0, WithTimeout(0))

	if p.httpClient.Timeout != DefaultTimeout {
		t.Errorf("httpClient.Timeout = %v, want %v (default)", p.httpClient.Timeout, DefaultTimeout)
	}
}

func TestWithTimeoutNegative(t *testing.T) {
	p := New(true, "token", "key", 0, WithTimeout(-5*time.Second))

	if p.httpClient.Timeout != DefaultTimeout {
		t.Errorf("httpClient.Timeout = %v, want %v (default)", p.httpClient.Timeout, DefaultTimeout)
	}
}

func TestSendDisabled(t *testing.T) {
	p := New(false, "token", "key", 0)

	msg := notification.NotificationMessage{
		Title:   "Test",
		State:   state.StateComplete,
		Summary: "Test summary",
	}

	err := p.Send(context.Background(), msg)
	if err != nil {
		t.Errorf("Send() on disabled provider should return nil, got %v", err)
	}
}

func TestSendMissingAppToken(t *testing.T) {
	p := New(true, "", "user-key", 0)

	msg := notification.NotificationMessage{
		Title:   "Test",
		State:   state.StateComplete,
		Summary: "Test summary",
	}

	err := p.Send(context.Background(), msg)
	if err == nil {
		t.Error("Send() with missing app_token should return error")
	}
	if !strings.Contains(err.Error(), "app_token is not configured") {
		t.Errorf("error should mention app_token, got: %v", err)
	}
}

func TestSendMissingUserKey(t *testing.T) {
	p := New(true, "app-token", "", 0)

	msg := notification.NotificationMessage{
		Title:   "Test",
		State:   state.StateComplete,
		Summary: "Test summary",
	}

	err := p.Send(context.Background(), msg)
	if err == nil {
		t.Error("Send() with missing user_key should return error")
	}
	if !strings.Contains(err.Error(), "user_key is not configured") {
		t.Errorf("error should mention user_key, got: %v", err)
	}
}

func TestSendSuccess(t *testing.T) {
	var receivedBody string
	var receivedContentType string
	var receivedMethod string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedMethod = r.Method
		receivedContentType = r.Header.Get("Content-Type")
		body, _ := io.ReadAll(r.Body)
		receivedBody = string(body)

		resp := PushoverResponse{
			Status:  1,
			Request: "test-request-id",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// Create provider with custom client that uses the test server
	customClient := &http.Client{
		Transport: &testTransport{server.URL},
	}
	p := New(true, "test-app-token", "test-user-key", 1, WithClient(customClient))

	msg := notification.NotificationMessage{
		Title:     "Test Title",
		State:     state.StateComplete,
		Summary:   "Test summary message",
		Request:   "Test request",
		Cwd:       "/test/project",
		SessionID: "session-123",
		Timestamp: time.Now(),
	}

	err := p.Send(context.Background(), msg)
	if err != nil {
		t.Errorf("Send() error = %v, want nil", err)
	}

	// Verify method
	if receivedMethod != http.MethodPost {
		t.Errorf("request method = %v, want POST", receivedMethod)
	}

	// Verify content type
	if receivedContentType != "application/x-www-form-urlencoded" {
		t.Errorf("Content-Type = %v, want application/x-www-form-urlencoded", receivedContentType)
	}

	// Parse and verify form data
	values, err := url.ParseQuery(receivedBody)
	if err != nil {
		t.Fatalf("Failed to parse form data: %v", err)
	}

	if values.Get("token") != "test-app-token" {
		t.Errorf("token = %v, want test-app-token", values.Get("token"))
	}
	if values.Get("user") != "test-user-key" {
		t.Errorf("user = %v, want test-user-key", values.Get("user"))
	}
	if values.Get("priority") != "1" {
		t.Errorf("priority = %v, want 1", values.Get("priority"))
	}
	if !strings.Contains(values.Get("title"), "Claude Code:") {
		t.Errorf("title should contain 'Claude Code:', got %v", values.Get("title"))
	}
	if !strings.Contains(values.Get("title"), string(state.StateComplete)) {
		t.Errorf("title should contain state, got %v", values.Get("title"))
	}
	if !strings.Contains(values.Get("message"), "Test summary message") {
		t.Errorf("message should contain summary, got %v", values.Get("message"))
	}
	if !strings.Contains(values.Get("message"), "/test/project") {
		t.Errorf("message should contain project path, got %v", values.Get("message"))
	}
}

func TestSendSuccessWithoutCwd(t *testing.T) {
	var receivedBody string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		receivedBody = string(body)

		resp := PushoverResponse{
			Status:  1,
			Request: "test-request-id",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	customClient := &http.Client{
		Transport: &testTransport{server.URL},
	}
	p := New(true, "token", "key", 0, WithClient(customClient))

	msg := notification.NotificationMessage{
		Title:   "Test",
		State:   state.StateComplete,
		Summary: "Test summary",
		Cwd:     "", // Empty Cwd
	}

	err := p.Send(context.Background(), msg)
	if err != nil {
		t.Errorf("Send() error = %v, want nil", err)
	}

	// Verify message does not contain "Project:" when Cwd is empty
	values, _ := url.ParseQuery(receivedBody)
	if strings.Contains(values.Get("message"), "Project:") {
		t.Errorf("message should not contain 'Project:' when Cwd is empty, got %v", values.Get("message"))
	}
}

func TestSendAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := PushoverResponse{
			Status:  0,
			Request: "test-request-id",
			Errors:  []string{"invalid token", "user not found"},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	customClient := &http.Client{
		Transport: &testTransport{server.URL},
	}
	p := New(true, "bad-token", "bad-key", 0, WithClient(customClient))

	msg := notification.NotificationMessage{
		Title:   "Test",
		State:   state.StateComplete,
		Summary: "Test summary",
	}

	err := p.Send(context.Background(), msg)
	if err == nil {
		t.Error("Send() should return error for API error response")
	}
	if !strings.Contains(err.Error(), "API error") {
		t.Errorf("error should mention API error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "invalid token") {
		t.Errorf("error should contain API error message, got: %v", err)
	}
}

func TestSendAPIErrorNoMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := PushoverResponse{
			Status:  0,
			Request: "test-request-id",
			Errors:  []string{}, // Empty errors array
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	customClient := &http.Client{
		Transport: &testTransport{server.URL},
	}
	p := New(true, "token", "key", 0, WithClient(customClient))

	msg := notification.NotificationMessage{
		Title:   "Test",
		State:   state.StateComplete,
		Summary: "Test summary",
	}

	err := p.Send(context.Background(), msg)
	if err == nil {
		t.Error("Send() should return error for API error response")
	}
	if !strings.Contains(err.Error(), "unknown error") {
		t.Errorf("error should mention unknown error when no errors provided, got: %v", err)
	}
}

func TestSendInvalidJSONResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("not valid json"))
	}))
	defer server.Close()

	customClient := &http.Client{
		Transport: &testTransport{server.URL},
	}
	p := New(true, "token", "key", 0, WithClient(customClient))

	msg := notification.NotificationMessage{
		Title:   "Test",
		State:   state.StateComplete,
		Summary: "Test summary",
	}

	err := p.Send(context.Background(), msg)
	if err == nil {
		t.Error("Send() should return error for invalid JSON response")
	}
	if !strings.Contains(err.Error(), "failed to parse response") {
		t.Errorf("error should mention parse failure, got: %v", err)
	}
}

func TestSendConnectionError(t *testing.T) {
	// Use a non-existent endpoint
	p := New(true, "token", "key", 0, WithTimeout(100*time.Millisecond))

	// Replace the default endpoint with a non-existent one
	customClient := &http.Client{
		Timeout: 100 * time.Millisecond,
		Transport: &testTransport{"http://localhost:59999"},
	}
	p.httpClient = customClient

	msg := notification.NotificationMessage{
		Title:   "Test",
		State:   state.StateComplete,
		Summary: "Test summary",
	}

	err := p.Send(context.Background(), msg)
	if err == nil {
		t.Error("Send() to non-existent endpoint should return error")
	}
	if !strings.Contains(err.Error(), "request failed") {
		t.Errorf("error should mention request failure, got: %v", err)
	}
}

func TestSendContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
		resp := PushoverResponse{Status: 1}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	customClient := &http.Client{
		Transport: &testTransport{server.URL},
	}
	p := New(true, "token", "key", 0, WithClient(customClient))

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
		resp := PushoverResponse{Status: 1}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	customClient := &http.Client{
		Transport: &testTransport{server.URL},
		Timeout:   100 * time.Millisecond,
	}
	p := New(true, "token", "key", 0, WithClient(customClient))

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

func TestProviderImplementsInterface(t *testing.T) {
	var _ notification.Provider = (*Provider)(nil)
}

func TestSendDifferentStates(t *testing.T) {
	states := []state.State{
		state.StateComplete,
		state.StateWaiting,
		state.StateStopped,
		state.StateIdle,
	}

	for _, s := range states {
		t.Run(string(s), func(t *testing.T) {
			var receivedBody string

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				receivedBody = string(body)

				resp := PushoverResponse{Status: 1, Request: "test"}
				json.NewEncoder(w).Encode(resp)
			}))
			defer server.Close()

			customClient := &http.Client{
				Transport: &testTransport{server.URL},
			}
			p := New(true, "token", "key", 0, WithClient(customClient))

			msg := notification.NotificationMessage{
				Title:   "Test",
				State:   s,
				Summary: "Test summary",
			}

			err := p.Send(context.Background(), msg)
			if err != nil {
				t.Errorf("Send() error = %v, want nil", err)
			}

			values, _ := url.ParseQuery(receivedBody)
			expectedTitle := "Claude Code: " + string(s)
			if values.Get("title") != expectedTitle {
				t.Errorf("title = %v, want %v", values.Get("title"), expectedTitle)
			}
		})
	}
}

func TestSendAllPriorities(t *testing.T) {
	priorities := []int{-2, -1, 0, 1, 2}

	for _, priority := range priorities {
		t.Run(string(rune('0'+priority+2)), func(t *testing.T) {
			var receivedBody string

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				receivedBody = string(body)

				resp := PushoverResponse{Status: 1, Request: "test"}
				json.NewEncoder(w).Encode(resp)
			}))
			defer server.Close()

			customClient := &http.Client{
				Transport: &testTransport{server.URL},
			}
			p := New(true, "token", "key", priority, WithClient(customClient))

			msg := notification.NotificationMessage{
				Title:   "Test",
				State:   state.StateComplete,
				Summary: "Test summary",
			}

			err := p.Send(context.Background(), msg)
			if err != nil {
				t.Errorf("Send() error = %v, want nil", err)
			}

			values, _ := url.ParseQuery(receivedBody)
			expectedPriority := string(rune('0' + priority))
			if priority < 0 {
				expectedPriority = "-" + string(rune('0'-priority))
			}
			if values.Get("priority") != expectedPriority {
				t.Errorf("priority = %v, want %v", values.Get("priority"), expectedPriority)
			}
		})
	}
}

func TestMultipleOptions(t *testing.T) {
	customClient := &http.Client{Timeout: 60 * time.Second}
	timeout := 10 * time.Second

	p := New(true, "token", "key", 0, WithClient(customClient), WithTimeout(timeout))

	if p.httpClient != customClient {
		t.Error("WithClient option did not set the custom client")
	}
	if p.httpClient.Timeout != timeout {
		t.Errorf("httpClient.Timeout = %v, want %v", p.httpClient.Timeout, timeout)
	}
}

func TestNewFromConfigWithOptions(t *testing.T) {
	cfg := &config.PushoverConfig{
		Enabled:  true,
		AppToken: "token",
		UserKey:  "key",
		Priority: 1,
	}

	customClient := &http.Client{Timeout: 45 * time.Second}
	p := NewFromConfig(cfg, WithClient(customClient))

	if p.httpClient != customClient {
		t.Error("WithClient option did not set the custom client when using NewFromConfig")
	}
}

// testTransport is a custom http.RoundTripper that redirects requests to a test server.
type testTransport struct {
	testServerURL string
}

func (t *testTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Redirect the request to the test server
	testURL, _ := url.Parse(t.testServerURL)
	req.URL.Scheme = testURL.Scheme
	req.URL.Host = testURL.Host
	return http.DefaultTransport.RoundTrip(req)
}
