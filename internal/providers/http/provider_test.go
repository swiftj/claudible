package http

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/johnswift/claudible/internal/config"
	"github.com/johnswift/claudible/internal/notification"
	"github.com/johnswift/claudible/internal/state"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name     string
		enabled  bool
		endpoint string
		headers  map[string]string
	}{
		{
			name:     "basic provider",
			enabled:  true,
			endpoint: "https://example.com/webhook",
			headers:  map[string]string{"X-Custom": "value"},
		},
		{
			name:     "disabled provider",
			enabled:  false,
			endpoint: "",
			headers:  nil,
		},
		{
			name:     "nil headers",
			enabled:  true,
			endpoint: "https://example.com/webhook",
			headers:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := New(tt.enabled, tt.endpoint, tt.headers)

			if p.Enabled() != tt.enabled {
				t.Errorf("Enabled() = %v, want %v", p.Enabled(), tt.enabled)
			}
			if p.endpoint != tt.endpoint {
				t.Errorf("endpoint = %v, want %v", p.endpoint, tt.endpoint)
			}
			if p.headers == nil {
				t.Error("headers should not be nil")
			}
			if p.httpClient == nil {
				t.Error("httpClient should not be nil")
			}
		})
	}
}

func TestNewFromConfig(t *testing.T) {
	tests := []struct {
		name string
		cfg  *config.HTTPConfig
		want struct {
			enabled  bool
			endpoint string
		}
	}{
		{
			name: "valid config",
			cfg: &config.HTTPConfig{
				Enabled:  true,
				Endpoint: "https://example.com/webhook",
				Headers:  map[string]string{"Authorization": "Bearer token"},
			},
			want: struct {
				enabled  bool
				endpoint string
			}{true, "https://example.com/webhook"},
		},
		{
			name: "nil config",
			cfg:  nil,
			want: struct {
				enabled  bool
				endpoint string
			}{false, ""},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewFromConfig(tt.cfg)

			if p.Enabled() != tt.want.enabled {
				t.Errorf("Enabled() = %v, want %v", p.Enabled(), tt.want.enabled)
			}
			if p.endpoint != tt.want.endpoint {
				t.Errorf("endpoint = %v, want %v", p.endpoint, tt.want.endpoint)
			}
		})
	}
}

func TestProviderName(t *testing.T) {
	p := New(true, "https://example.com", nil)
	if got := p.Name(); got != "http" {
		t.Errorf("Name() = %v, want http", got)
	}
}

func TestWithClient(t *testing.T) {
	customClient := &http.Client{Timeout: 60 * time.Second}
	p := New(true, "https://example.com", nil, WithClient(customClient))

	if p.httpClient != customClient {
		t.Error("WithClient option did not set the custom client")
	}
}

func TestWithClientNil(t *testing.T) {
	p := New(true, "https://example.com", nil, WithClient(nil))

	if p.httpClient == nil {
		t.Error("WithClient(nil) should not set httpClient to nil")
	}
}

func TestWithTimeout(t *testing.T) {
	timeout := 10 * time.Second
	p := New(true, "https://example.com", nil, WithTimeout(timeout))

	if p.httpClient.Timeout != timeout {
		t.Errorf("httpClient.Timeout = %v, want %v", p.httpClient.Timeout, timeout)
	}
}

func TestWithTimeoutZero(t *testing.T) {
	p := New(true, "https://example.com", nil, WithTimeout(0))

	if p.httpClient.Timeout != DefaultTimeout {
		t.Errorf("httpClient.Timeout = %v, want %v (default)", p.httpClient.Timeout, DefaultTimeout)
	}
}

func TestSendSuccess(t *testing.T) {
	var receivedBody []byte
	var receivedHeaders http.Header

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header.Clone()
		body, _ := io.ReadAll(r.Body)
		receivedBody = body
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	headers := map[string]string{
		"X-Custom-Header": "custom-value",
		"Authorization":   "Bearer token123",
	}
	p := New(true, server.URL, headers)

	msg := notification.NotificationMessage{
		Title:     "Test Title",
		State:     state.StateComplete,
		Summary:   "Test summary",
		Request:   "Test request",
		Cwd:       "/test/path",
		SessionID: "session-123",
		Timestamp: time.Now(),
	}

	err := p.Send(context.Background(), msg)
	if err != nil {
		t.Errorf("Send() error = %v, want nil", err)
	}

	// Verify the body was sent correctly
	var receivedMsg notification.NotificationMessage
	if err := json.Unmarshal(receivedBody, &receivedMsg); err != nil {
		t.Fatalf("Failed to unmarshal received body: %v", err)
	}
	if receivedMsg.Title != msg.Title {
		t.Errorf("received Title = %v, want %v", receivedMsg.Title, msg.Title)
	}
	if receivedMsg.State != msg.State {
		t.Errorf("received State = %v, want %v", receivedMsg.State, msg.State)
	}

	// Verify headers
	if receivedHeaders.Get("Content-Type") != ContentTypeJSON {
		t.Errorf("Content-Type = %v, want %v", receivedHeaders.Get("Content-Type"), ContentTypeJSON)
	}
	if receivedHeaders.Get("X-Custom-Header") != "custom-value" {
		t.Errorf("X-Custom-Header = %v, want custom-value", receivedHeaders.Get("X-Custom-Header"))
	}
	if receivedHeaders.Get("Authorization") != "Bearer token123" {
		t.Errorf("Authorization = %v, want Bearer token123", receivedHeaders.Get("Authorization"))
	}
}

func TestSendCustomContentType(t *testing.T) {
	var receivedContentType string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedContentType = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	headers := map[string]string{
		"Content-Type": "application/json; charset=utf-8",
	}
	p := New(true, server.URL, headers)

	msg := notification.NotificationMessage{
		Title: "Test",
		State: state.StateComplete,
	}

	err := p.Send(context.Background(), msg)
	if err != nil {
		t.Errorf("Send() error = %v, want nil", err)
	}

	if receivedContentType != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %v, want application/json; charset=utf-8", receivedContentType)
	}
}

func TestSendDisabled(t *testing.T) {
	p := New(false, "https://example.com", nil)

	msg := notification.NotificationMessage{
		Title: "Test",
		State: state.StateComplete,
	}

	err := p.Send(context.Background(), msg)
	if err != nil {
		t.Errorf("Send() on disabled provider should return nil, got %v", err)
	}
}

func TestSendEmptyEndpoint(t *testing.T) {
	p := New(true, "", nil)

	msg := notification.NotificationMessage{
		Title: "Test",
		State: state.StateComplete,
	}

	err := p.Send(context.Background(), msg)
	if err == nil {
		t.Error("Send() with empty endpoint should return error")
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
			body:       "invalid request",
		},
		{
			name:       "401 Unauthorized",
			statusCode: http.StatusUnauthorized,
			body:       "unauthorized",
		},
		{
			name:       "500 Internal Server Error",
			statusCode: http.StatusInternalServerError,
			body:       "server error",
		},
		{
			name:       "503 Service Unavailable",
			statusCode: http.StatusServiceUnavailable,
			body:       "service unavailable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.body))
			}))
			defer server.Close()

			p := New(true, server.URL, nil)

			msg := notification.NotificationMessage{
				Title: "Test",
				State: state.StateComplete,
			}

			err := p.Send(context.Background(), msg)
			if err == nil {
				t.Errorf("Send() should return error for status %d", tt.statusCode)
			}
		})
	}
}

func TestSendContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	p := New(true, server.URL, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	msg := notification.NotificationMessage{
		Title: "Test",
		State: state.StateComplete,
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

	p := New(true, server.URL, nil, WithTimeout(100*time.Millisecond))

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	msg := notification.NotificationMessage{
		Title: "Test",
		State: state.StateComplete,
	}

	err := p.Send(ctx, msg)
	if err == nil {
		t.Error("Send() with timed out context should return error")
	}
}

func TestSendConnectionError(t *testing.T) {
	// Use a non-existent endpoint
	p := New(true, "http://localhost:59999/webhook", nil, WithTimeout(100*time.Millisecond))

	msg := notification.NotificationMessage{
		Title: "Test",
		State: state.StateComplete,
	}

	err := p.Send(context.Background(), msg)
	if err == nil {
		t.Error("Send() to non-existent endpoint should return error")
	}
}

func TestProviderImplementsInterface(t *testing.T) {
	var _ notification.Provider = (*Provider)(nil)
}

func TestSend2xxStatusCodes(t *testing.T) {
	successCodes := []int{200, 201, 202, 204}

	for _, code := range successCodes {
		t.Run(http.StatusText(code), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(code)
			}))
			defer server.Close()

			p := New(true, server.URL, nil)

			msg := notification.NotificationMessage{
				Title: "Test",
				State: state.StateComplete,
			}

			err := p.Send(context.Background(), msg)
			if err != nil {
				t.Errorf("Send() with status %d should not return error, got %v", code, err)
			}
		})
	}
}

func TestSendRequestMethod(t *testing.T) {
	var receivedMethod string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedMethod = r.Method
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	p := New(true, server.URL, nil)

	msg := notification.NotificationMessage{
		Title: "Test",
		State: state.StateComplete,
	}

	err := p.Send(context.Background(), msg)
	if err != nil {
		t.Errorf("Send() error = %v, want nil", err)
	}

	if receivedMethod != http.MethodPost {
		t.Errorf("request method = %v, want POST", receivedMethod)
	}
}
