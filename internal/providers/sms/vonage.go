// Package sms provides SMS notification support via various providers.
package sms

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// VonageBackend implements SMSBackend using the Vonage (Nexmo) REST API.
type VonageBackend struct {
	apiKey     string
	apiSecret  string
	httpClient *http.Client
	baseURL    string
}

// vonageResponse represents the JSON response from Vonage SMS API.
type vonageResponse struct {
	MessageCount string           `json:"message-count"`
	Messages     []vonageMessage  `json:"messages"`
}

// vonageMessage represents a single message in the Vonage response.
type vonageMessage struct {
	Status    string `json:"status"`
	ErrorText string `json:"error-text,omitempty"`
	MessageID string `json:"message-id,omitempty"`
}

// NewVonageBackend creates a new Vonage SMS backend.
func NewVonageBackend(apiKey, apiSecret string) *VonageBackend {
	return &VonageBackend{
		apiKey:     apiKey,
		apiSecret:  apiSecret,
		httpClient: http.DefaultClient,
		baseURL:    "https://rest.nexmo.com",
	}
}

// Name returns the backend name.
func (v *VonageBackend) Name() string {
	return "vonage"
}

// Send sends an SMS via the Vonage REST API.
func (v *VonageBackend) Send(ctx context.Context, to, from, body string) error {
	endpoint := fmt.Sprintf("%s/sms/json", v.baseURL)

	// Prepare form data
	data := url.Values{}
	data.Set("api_key", v.apiKey)
	data.Set("api_secret", v.apiSecret)
	data.Set("from", from)
	data.Set("to", to)
	data.Set("text", body)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return fmt.Errorf("vonage: failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := v.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("vonage: failed to send SMS: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("vonage: failed to read response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("vonage: API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	// Parse the JSON response
	var vonageResp vonageResponse
	if err := json.Unmarshal(respBody, &vonageResp); err != nil {
		return fmt.Errorf("vonage: failed to parse response: %w", err)
	}

	// Check if we have at least one message in the response
	if len(vonageResp.Messages) == 0 {
		return fmt.Errorf("vonage: no messages in response")
	}

	// Status "0" indicates success
	msg := vonageResp.Messages[0]
	if msg.Status != "0" {
		errorText := msg.ErrorText
		if errorText == "" {
			errorText = "unknown error"
		}
		return fmt.Errorf("vonage: message delivery failed (status %s): %s", msg.Status, errorText)
	}

	return nil
}
