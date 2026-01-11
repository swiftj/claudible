// Package pushbullet provides a Pushbullet notification provider.
// It sends push notifications to devices via the Pushbullet API.
package pushbullet

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
	"unicode"

	"github.com/swiftj/claudible/internal/config"
	"github.com/swiftj/claudible/internal/notification"
)

const (
	// providerName is the unique identifier for this provider.
	providerName = "pushbullet"

	// apiEndpoint is the Pushbullet API endpoint for sending pushes.
	apiEndpoint = "https://api.pushbullet.com/v2/pushes"

	// defaultTimeout is the default HTTP request timeout.
	defaultTimeout = 30 * time.Second

	// maxRequestLength is the maximum length for the request field in the body.
	maxRequestLength = 100
)

// pushRequest represents the JSON payload for a Pushbullet push.
type pushRequest struct {
	Type       string `json:"type"`
	Title      string `json:"title"`
	Body       string `json:"body"`
	DeviceIden string `json:"device_iden,omitempty"`
}

// pushResponse represents the JSON response from Pushbullet API.
type pushResponse struct {
	Iden  string `json:"iden"`
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// Provider implements the notification.Provider interface for Pushbullet.
type Provider struct {
	enabled    bool
	apiKey     string
	deviceIden string
	httpClient *http.Client
}

// Option is a functional option for configuring the Provider.
type Option func(*Provider)

// WithClient sets a custom HTTP client for the provider.
func WithClient(client *http.Client) Option {
	return func(p *Provider) {
		if client != nil {
			p.httpClient = client
		}
	}
}

// WithTimeout sets the timeout for HTTP requests.
func WithTimeout(timeout time.Duration) Option {
	return func(p *Provider) {
		if timeout > 0 {
			p.httpClient.Timeout = timeout
		}
	}
}

// New creates a new Pushbullet provider with the given configuration.
func New(enabled bool, apiKey string, deviceIden string, opts ...Option) *Provider {
	p := &Provider{
		enabled:    enabled,
		apiKey:     apiKey,
		deviceIden: deviceIden,
		httpClient: &http.Client{
			Timeout: defaultTimeout,
		},
	}

	// Apply options
	for _, opt := range opts {
		opt(p)
	}

	return p
}

// NewFromConfig creates a new Pushbullet provider from configuration.
func NewFromConfig(cfg *config.PushbulletConfig, opts ...Option) *Provider {
	if cfg == nil {
		return New(false, "", "", opts...)
	}
	return New(cfg.Enabled, cfg.APIKey, cfg.DeviceIden, opts...)
}

// Name returns the provider name.
func (p *Provider) Name() string {
	return providerName
}

// Enabled returns whether the provider is enabled.
func (p *Provider) Enabled() bool {
	return p.enabled
}

// Send sends a notification message via the Pushbullet API.
func (p *Provider) Send(ctx context.Context, msg notification.NotificationMessage) error {
	if !p.enabled {
		return nil
	}

	if p.apiKey == "" {
		return fmt.Errorf("pushbullet provider: API key is not configured")
	}

	title := formatTitle(msg)
	body := formatBody(msg)

	// Create the push request payload
	pushReq := pushRequest{
		Type:       "note",
		Title:      title,
		Body:       body,
		DeviceIden: p.deviceIden,
	}

	payload, err := json.Marshal(pushReq)
	if err != nil {
		return fmt.Errorf("pushbullet provider: failed to marshal request: %w", err)
	}

	// Create the HTTP request
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiEndpoint, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("pushbullet provider: failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Access-Token", p.apiKey)
	req.Header.Set("Content-Type", "application/json")

	// Execute the request
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("pushbullet provider: request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return fmt.Errorf("pushbullet provider: failed to read response: %w", err)
	}

	// Check for non-2xx status codes
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("pushbullet provider: received status %d: %s", resp.StatusCode, string(respBody))
	}

	// Parse response to verify success
	var pushResp pushResponse
	if err := json.Unmarshal(respBody, &pushResp); err != nil {
		return fmt.Errorf("pushbullet provider: failed to parse response: %w", err)
	}

	// Check for API-level errors
	if pushResp.Error.Code != "" {
		return fmt.Errorf("pushbullet provider: API error %s: %s", pushResp.Error.Code, pushResp.Error.Message)
	}

	// Verify we got an identifier back (indicates success)
	if pushResp.Iden == "" {
		return fmt.Errorf("pushbullet provider: no push identifier returned")
	}

	return nil
}

// formatTitle creates the notification title from the message.
func formatTitle(msg notification.NotificationMessage) string {
	stateStr := titleCase(string(msg.State))
	return fmt.Sprintf("Claude Code: %s", stateStr)
}

// titleCase converts the first character to uppercase.
func titleCase(s string) string {
	if s == "" {
		return s
	}
	runes := []rune(s)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}

// formatBody creates the notification body from the message.
func formatBody(msg notification.NotificationMessage) string {
	body := msg.Summary

	if msg.Cwd != "" {
		body += "\nProject: " + msg.Cwd
	}

	if msg.Request != "" {
		request := msg.Request
		if len(request) > maxRequestLength {
			request = request[:maxRequestLength-3] + "..."
		}
		body += "\nRequest: " + request
	}

	return body
}

// Compile-time check that Provider implements notification.Provider.
var _ notification.Provider = (*Provider)(nil)
