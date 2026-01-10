// Package pushover provides a Pushover push notification provider.
// It sends notifications via the Pushover API (https://pushover.net/).
package pushover

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/johnswift/claudible/internal/config"
	"github.com/johnswift/claudible/internal/notification"
)

const (
	// DefaultTimeout is the default HTTP request timeout.
	DefaultTimeout = 30 * time.Second

	// PushoverAPIEndpoint is the Pushover API endpoint for sending messages.
	PushoverAPIEndpoint = "https://api.pushover.net/1/messages.json"
)

// PushoverResponse represents the JSON response from the Pushover API.
type PushoverResponse struct {
	Status  int      `json:"status"`
	Request string   `json:"request"`
	Errors  []string `json:"errors,omitempty"`
}

// Provider implements the notification.Provider interface for Pushover.
type Provider struct {
	enabled    bool
	appToken   string
	userKey    string
	priority   int
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

// New creates a new Pushover provider with the given configuration.
func New(enabled bool, appToken, userKey string, priority int, opts ...Option) *Provider {
	// Clamp priority to valid range (-2 to 2)
	if priority < -2 {
		priority = -2
	}
	if priority > 2 {
		priority = 2
	}

	p := &Provider{
		enabled:  enabled,
		appToken: appToken,
		userKey:  userKey,
		priority: priority,
		httpClient: &http.Client{
			Timeout: DefaultTimeout,
		},
	}

	// Apply options
	for _, opt := range opts {
		opt(p)
	}

	return p
}

// NewFromConfig creates a new Pushover provider from configuration.
func NewFromConfig(cfg *config.PushoverConfig, opts ...Option) *Provider {
	if cfg == nil {
		return New(false, "", "", 0, opts...)
	}
	return New(cfg.Enabled, cfg.AppToken, cfg.UserKey, cfg.Priority, opts...)
}

// Name returns the provider name.
func (p *Provider) Name() string {
	return "pushover"
}

// Enabled returns whether the provider is enabled.
func (p *Provider) Enabled() bool {
	return p.enabled
}

// Send sends a notification message via the Pushover API.
func (p *Provider) Send(ctx context.Context, msg notification.NotificationMessage) error {
	if !p.enabled {
		return nil
	}

	if p.appToken == "" {
		return fmt.Errorf("pushover provider: app_token is not configured")
	}

	if p.userKey == "" {
		return fmt.Errorf("pushover provider: user_key is not configured")
	}

	// Format the message
	title := fmt.Sprintf("Claude Code: %s", msg.State)
	message := msg.Summary
	if msg.Cwd != "" {
		message = fmt.Sprintf("%s\nProject: %s", msg.Summary, msg.Cwd)
	}

	// Prepare form data
	data := url.Values{}
	data.Set("token", p.appToken)
	data.Set("user", p.userKey)
	data.Set("title", title)
	data.Set("message", message)
	data.Set("priority", fmt.Sprintf("%d", p.priority))

	// Create the request
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, PushoverAPIEndpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return fmt.Errorf("pushover provider: failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	// Execute the request
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("pushover provider: request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return fmt.Errorf("pushover provider: failed to read response: %w", err)
	}

	// Parse JSON response
	var pushoverResp PushoverResponse
	if err := json.Unmarshal(body, &pushoverResp); err != nil {
		return fmt.Errorf("pushover provider: failed to parse response: %w", err)
	}

	// Check for success (status=1)
	if pushoverResp.Status != 1 {
		errMsg := "unknown error"
		if len(pushoverResp.Errors) > 0 {
			errMsg = strings.Join(pushoverResp.Errors, "; ")
		}
		return fmt.Errorf("pushover provider: API error: %s", errMsg)
	}

	return nil
}

// Ensure Provider implements notification.Provider interface.
var _ notification.Provider = (*Provider)(nil)
