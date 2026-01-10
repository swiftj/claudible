// Package http provides an HTTP webhook notification provider.
// It sends notifications via HTTP POST to webhooks like Pushcut, Pushover, IFTTT,
// or any custom endpoint that accepts JSON payloads.
package http

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/johnswift/claudible/internal/config"
	"github.com/johnswift/claudible/internal/notification"
)

const (
	// DefaultTimeout is the default HTTP request timeout.
	DefaultTimeout = 30 * time.Second

	// ContentTypeJSON is the default content type for JSON payloads.
	ContentTypeJSON = "application/json"
)

// Provider implements the notification.Provider interface for HTTP webhooks.
type Provider struct {
	enabled    bool
	endpoint   string
	headers    map[string]string
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

// New creates a new HTTP webhook provider with the given configuration.
func New(enabled bool, endpoint string, headers map[string]string, opts ...Option) *Provider {
	p := &Provider{
		enabled:  enabled,
		endpoint: endpoint,
		headers:  headers,
		httpClient: &http.Client{
			Timeout: DefaultTimeout,
		},
	}

	// Ensure headers map is initialized
	if p.headers == nil {
		p.headers = make(map[string]string)
	}

	// Apply options
	for _, opt := range opts {
		opt(p)
	}

	return p
}

// NewFromConfig creates a new HTTP webhook provider from configuration.
func NewFromConfig(cfg *config.HTTPConfig, opts ...Option) *Provider {
	if cfg == nil {
		return New(false, "", nil, opts...)
	}
	return New(cfg.Enabled, cfg.Endpoint, cfg.Headers, opts...)
}

// Name returns the provider name.
func (p *Provider) Name() string {
	return "http"
}

// Enabled returns whether the provider is enabled.
func (p *Provider) Enabled() bool {
	return p.enabled
}

// Send sends a notification message via HTTP POST to the configured endpoint.
func (p *Provider) Send(ctx context.Context, msg notification.NotificationMessage) error {
	if !p.enabled {
		return nil
	}

	if p.endpoint == "" {
		return fmt.Errorf("http provider: endpoint is not configured")
	}

	// Marshal the message to JSON
	payload, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("http provider: failed to marshal message: %w", err)
	}

	// Create the request
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("http provider: failed to create request: %w", err)
	}

	// Set default Content-Type if not provided in headers
	hasContentType := false
	for key := range p.headers {
		if http.CanonicalHeaderKey(key) == "Content-Type" {
			hasContentType = true
			break
		}
	}
	if !hasContentType {
		req.Header.Set("Content-Type", ContentTypeJSON)
	}

	// Set configured headers
	for key, value := range p.headers {
		req.Header.Set(key, value)
	}

	// Execute the request
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http provider: request failed: %w", err)
	}
	defer resp.Body.Close()

	// Check for non-2xx status codes
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("http provider: received status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// Ensure Provider implements notification.Provider interface.
var _ notification.Provider = (*Provider)(nil)
