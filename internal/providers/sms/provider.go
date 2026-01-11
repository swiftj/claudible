// Package sms provides SMS notification support via Twilio and other providers.
package sms

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/swiftj/claudible/internal/config"
	"github.com/swiftj/claudible/internal/notification"
)

// maxSMSLength is the maximum length for a single SMS segment.
const maxSMSLength = 160

// SMSBackend defines the interface for pluggable SMS providers.
type SMSBackend interface {
	// Send sends an SMS message to the specified recipient.
	Send(ctx context.Context, to, from, body string) error
	// Name returns the name of this SMS backend.
	Name() string
}

// Provider implements the notification.Provider interface for SMS.
type Provider struct {
	enabled bool
	to      string
	from    string
	backend SMSBackend
}

// New creates a new SMS Provider with the given configuration.
func New(enabled bool, to, from string, backend SMSBackend) *Provider {
	return &Provider{
		enabled: enabled,
		to:      to,
		from:    from,
		backend: backend,
	}
}

// NewFromConfig creates an SMS Provider from configuration.
// It selects the appropriate backend based on cfg.Provider.
func NewFromConfig(cfg *config.SMSConfig) *Provider {
	if cfg == nil {
		return &Provider{enabled: false}
	}

	var backend SMSBackend
	switch strings.ToLower(cfg.Provider) {
	case "twilio":
		backend = NewTwilioBackend(cfg.AccountSID, cfg.AuthToken)
	case "vonage", "nexmo":
		backend = NewVonageBackend(cfg.APIKey, cfg.APISecret)
	case "plivo":
		backend = NewPlivoBackend(cfg.AuthID, cfg.AuthToken)
	default:
		// Default to Twilio if provider not recognized
		backend = NewTwilioBackend(cfg.AccountSID, cfg.AuthToken)
	}

	return New(cfg.Enabled, cfg.To, cfg.From, backend)
}

// Name returns the provider name.
func (p *Provider) Name() string {
	return "sms"
}

// Enabled returns whether the provider is enabled.
func (p *Provider) Enabled() bool {
	return p.enabled && p.backend != nil
}

// Send sends a notification message via SMS.
func (p *Provider) Send(ctx context.Context, msg notification.NotificationMessage) error {
	if !p.Enabled() {
		return fmt.Errorf("sms provider is not enabled")
	}

	body := formatMessage(msg)
	return p.backend.Send(ctx, p.to, p.from, body)
}

// formatMessage formats the notification message for SMS.
// It creates a concise message and truncates if needed.
func formatMessage(msg notification.NotificationMessage) string {
	// Format: "[State] Summary"
	body := fmt.Sprintf("[%s] %s", msg.State, msg.Summary)

	// Truncate if too long
	if len(body) > maxSMSLength {
		body = body[:maxSMSLength-3] + "..."
	}

	return body
}

// TwilioBackend implements SMSBackend using the Twilio REST API.
type TwilioBackend struct {
	accountSID string
	authToken  string
	httpClient *http.Client
	baseURL    string
}

// NewTwilioBackend creates a new Twilio SMS backend.
func NewTwilioBackend(accountSID, authToken string) *TwilioBackend {
	return &TwilioBackend{
		accountSID: accountSID,
		authToken:  authToken,
		httpClient: http.DefaultClient,
		baseURL:    "https://api.twilio.com/2010-04-01",
	}
}

// Name returns the backend name.
func (t *TwilioBackend) Name() string {
	return "twilio"
}

// Send sends an SMS via the Twilio REST API.
func (t *TwilioBackend) Send(ctx context.Context, to, from, body string) error {
	endpoint := fmt.Sprintf("%s/Accounts/%s/Messages.json", t.baseURL, t.accountSID)

	// Prepare form data
	data := url.Values{}
	data.Set("To", to)
	data.Set("From", from)
	data.Set("Body", body)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.SetBasicAuth(t.accountSID, t.authToken)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send SMS: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("twilio API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	return nil
}
