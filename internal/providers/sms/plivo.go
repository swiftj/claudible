// Package sms provides SMS notification support via various providers.
package sms

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// PlivoBackend implements SMSBackend using the Plivo REST API.
type PlivoBackend struct {
	authID     string
	authToken  string
	httpClient *http.Client
	baseURL    string
}

// plivoRequest represents the JSON payload for Plivo SMS API.
type plivoRequest struct {
	Src  string `json:"src"`
	Dst  string `json:"dst"`
	Text string `json:"text"`
}

// plivoErrorResponse represents an error response from Plivo API.
type plivoErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

// NewPlivoBackend creates a new Plivo SMS backend.
func NewPlivoBackend(authID, authToken string) *PlivoBackend {
	return &PlivoBackend{
		authID:     authID,
		authToken:  authToken,
		httpClient: http.DefaultClient,
		baseURL:    "https://api.plivo.com/v1",
	}
}

// Name returns the backend name.
func (p *PlivoBackend) Name() string {
	return "plivo"
}

// Send sends an SMS via the Plivo REST API.
func (p *PlivoBackend) Send(ctx context.Context, to, from, body string) error {
	endpoint := fmt.Sprintf("%s/Account/%s/Message/", p.baseURL, p.authID)

	// Prepare JSON payload
	payload := plivoRequest{
		Src:  from,
		Dst:  to,
		Text: body,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal request payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.SetBasicAuth(p.authID, p.authToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send SMS via Plivo: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)

		// Try to parse error response for better error messages
		var errResp plivoErrorResponse
		if json.Unmarshal(respBody, &errResp) == nil && errResp.Error != "" {
			return fmt.Errorf("plivo API error (status %d): %s", resp.StatusCode, errResp.Error)
		}
		if errResp.Message != "" {
			return fmt.Errorf("plivo API error (status %d): %s", resp.StatusCode, errResp.Message)
		}

		return fmt.Errorf("plivo API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	return nil
}
