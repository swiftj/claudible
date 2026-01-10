package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefault(t *testing.T) {
	cfg := Default()

	if cfg.Notifications.DefaultProvider != "imessage" {
		t.Errorf("expected default_provider to be 'imessage', got %s", cfg.Notifications.DefaultProvider)
	}

	if !cfg.Notifications.Providers.IMessage.Enabled {
		t.Error("expected imessage to be enabled by default")
	}

	if cfg.Notifications.Providers.SMS.Enabled {
		t.Error("expected sms to be disabled by default")
	}

	if cfg.Notifications.Providers.HTTP.Enabled {
		t.Error("expected http to be disabled by default")
	}

	if cfg.Notifications.Providers.Desktop.Enabled {
		t.Error("expected desktop to be disabled by default")
	}

	if cfg.Behavior.MaxMessageLength != 900 {
		t.Errorf("expected max_message_length to be 900, got %d", cfg.Behavior.MaxMessageLength)
	}

	if !cfg.Behavior.IncludeRepoPath {
		t.Error("expected include_repo_path to be true by default")
	}
}

func TestLoadFromPath_NonExistent(t *testing.T) {
	cfg, err := LoadFromPath("/nonexistent/path/config.yaml")
	if err != nil {
		t.Fatalf("expected no error for non-existent file, got %v", err)
	}

	// Should return defaults
	if cfg.Notifications.DefaultProvider != "imessage" {
		t.Errorf("expected default config, got default_provider=%s", cfg.Notifications.DefaultProvider)
	}
}

func TestLoadFromPath_ValidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	yamlContent := `
notifications:
  default_provider: imessage
  providers:
    imessage:
      enabled: true
      target: "+1234567890"
    sms:
      enabled: false
    http:
      enabled: true
      endpoint: "https://example.com/webhook"
      headers:
        Content-Type: application/json
        Authorization: Bearer token123
    desktop:
      enabled: false
behavior:
  notify_on:
    - complete
    - waiting
  max_message_length: 500
  include_repo_path: false
`
	if err := os.WriteFile(configPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	cfg, err := LoadFromPath(configPath)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	if cfg.Notifications.Providers.IMessage.Target != "+1234567890" {
		t.Errorf("expected target '+1234567890', got %s", cfg.Notifications.Providers.IMessage.Target)
	}

	if !cfg.Notifications.Providers.HTTP.Enabled {
		t.Error("expected http to be enabled")
	}

	if cfg.Notifications.Providers.HTTP.Endpoint != "https://example.com/webhook" {
		t.Errorf("expected endpoint 'https://example.com/webhook', got %s", cfg.Notifications.Providers.HTTP.Endpoint)
	}

	if cfg.Notifications.Providers.HTTP.Headers["Authorization"] != "Bearer token123" {
		t.Errorf("expected Authorization header, got %v", cfg.Notifications.Providers.HTTP.Headers)
	}

	if cfg.Behavior.MaxMessageLength != 500 {
		t.Errorf("expected max_message_length 500, got %d", cfg.Behavior.MaxMessageLength)
	}

	if cfg.Behavior.IncludeRepoPath {
		t.Error("expected include_repo_path to be false")
	}
}

func TestLoadFromPath_InvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	invalidYAML := `
notifications:
  default_provider: [invalid yaml
`
	if err := os.WriteFile(configPath, []byte(invalidYAML), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	_, err := LoadFromPath(configPath)
	if err == nil {
		t.Error("expected error for invalid YAML")
	}
}

func TestValidate_ValidConfig(t *testing.T) {
	cfg := Default()
	cfg.Notifications.Providers.IMessage.Target = "+1234567890"

	if err := cfg.Validate(); err != nil {
		t.Errorf("expected no validation error, got %v", err)
	}
}

func TestValidate_MissingIMessageTarget(t *testing.T) {
	cfg := Default()
	// imessage enabled but no target

	err := cfg.Validate()
	if err == nil {
		t.Error("expected validation error for missing imessage target")
	}
}

func TestValidate_SMSMissingFields(t *testing.T) {
	cfg := Default()
	cfg.Notifications.Providers.IMessage.Enabled = false
	cfg.Notifications.Providers.SMS.Enabled = true
	cfg.Notifications.Providers.SMS.Provider = "twilio"
	// Missing from, to, account_sid, auth_token

	err := cfg.Validate()
	if err == nil {
		t.Error("expected validation error for missing SMS fields")
	}
}

func TestValidate_HTTPMissingEndpoint(t *testing.T) {
	cfg := Default()
	cfg.Notifications.Providers.IMessage.Enabled = false
	cfg.Notifications.Providers.HTTP.Enabled = true
	// Missing endpoint

	err := cfg.Validate()
	if err == nil {
		t.Error("expected validation error for missing HTTP endpoint")
	}
}

func TestValidate_InvalidNotifyOnState(t *testing.T) {
	cfg := Default()
	cfg.Notifications.Providers.IMessage.Target = "+1234567890"
	cfg.Behavior.NotifyOn = []string{"complete", "invalid_state"}

	err := cfg.Validate()
	if err == nil {
		t.Error("expected validation error for invalid notify_on state")
	}
}

func TestValidate_InvalidMaxMessageLength(t *testing.T) {
	cfg := Default()
	cfg.Notifications.Providers.IMessage.Target = "+1234567890"
	cfg.Behavior.MaxMessageLength = 0

	err := cfg.Validate()
	if err == nil {
		t.Error("expected validation error for zero max_message_length")
	}
}

func TestValidate_UnknownDefaultProvider(t *testing.T) {
	cfg := Default()
	cfg.Notifications.Providers.IMessage.Enabled = false
	cfg.Notifications.DefaultProvider = "unknown_provider"

	err := cfg.Validate()
	if err == nil {
		t.Error("expected validation error for unknown default_provider")
	}
}

func TestIsProviderEnabled(t *testing.T) {
	cfg := Default()

	if !cfg.IsProviderEnabled("imessage") {
		t.Error("expected imessage to be enabled")
	}

	if cfg.IsProviderEnabled("sms") {
		t.Error("expected sms to be disabled")
	}

	if cfg.IsProviderEnabled("unknown") {
		t.Error("expected unknown provider to return false")
	}
}

func TestGetEnabledProviders(t *testing.T) {
	cfg := Default()
	providers := cfg.GetEnabledProviders()

	if len(providers) != 1 {
		t.Errorf("expected 1 enabled provider, got %d", len(providers))
	}

	if providers[0] != "imessage" {
		t.Errorf("expected 'imessage', got %s", providers[0])
	}

	// Enable more providers
	cfg.Notifications.Providers.HTTP.Enabled = true
	cfg.Notifications.Providers.Desktop.Enabled = true
	providers = cfg.GetEnabledProviders()

	if len(providers) != 3 {
		t.Errorf("expected 3 enabled providers, got %d", len(providers))
	}
}

func TestShouldNotifyOn(t *testing.T) {
	cfg := Default()

	if !cfg.ShouldNotifyOn("complete") {
		t.Error("expected to notify on 'complete'")
	}

	if !cfg.ShouldNotifyOn("waiting") {
		t.Error("expected to notify on 'waiting'")
	}

	if cfg.ShouldNotifyOn("stopped") {
		t.Error("expected not to notify on 'stopped' by default")
	}

	if cfg.ShouldNotifyOn("unknown") {
		t.Error("expected not to notify on unknown state")
	}
}
