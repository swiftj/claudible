package setup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestIsValidPhone(t *testing.T) {
	tests := []struct {
		name  string
		phone string
		want  bool
	}{
		{"valid US with plus", "+15551234567", true},
		{"valid US without plus", "15551234567", true},
		{"valid US 10 digits", "5551234567", true},
		{"valid with dashes", "555-123-4567", true},
		{"valid with parens", "(555) 123-4567", true},
		{"valid international", "+44123456789", true},
		{"too short", "12345", false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isValidPhone(tt.phone)
			if got != tt.want {
				t.Errorf("isValidPhone(%q) = %v, want %v", tt.phone, got, tt.want)
			}
		})
	}
}

func TestNormalizePhone(t *testing.T) {
	tests := []struct {
		name  string
		phone string
		want  string
	}{
		{"already normalized", "+15551234567", "+15551234567"},
		{"US without plus", "15551234567", "+15551234567"},
		{"US 10 digits", "5551234567", "+15551234567"},
		{"with dashes", "555-123-4567", "+15551234567"},
		{"with parens", "(555) 123-4567", "+15551234567"},
		{"international with plus", "+44123456789", "+44123456789"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizePhone(tt.phone)
			if got != tt.want {
				t.Errorf("normalizePhone(%q) = %q, want %q", tt.phone, got, tt.want)
			}
		})
	}
}

func TestCreateClaudibleConfig(t *testing.T) {
	// Create temp directory
	tempDir := t.TempDir()

	// Override home directory for test
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tempDir)
	defer os.Setenv("HOME", origHome)

	wizard := NewWizard(false)
	configPath, err := wizard.createClaudibleConfig("+15551234567")
	if err != nil {
		t.Fatalf("createClaudibleConfig() error = %v", err)
	}

	// Verify config file exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Fatalf("config file not created at %s", configPath)
	}

	// Read and parse config
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read config: %v", err)
	}

	var config ClaudibleConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		t.Fatalf("failed to parse config: %v", err)
	}

	// Verify config values
	if config.Notifications.DefaultProvider != "imessage" {
		t.Errorf("default_provider = %q, want %q", config.Notifications.DefaultProvider, "imessage")
	}
	if !config.Notifications.Providers.IMessage.Enabled {
		t.Error("imessage.enabled = false, want true")
	}
	if config.Notifications.Providers.IMessage.Target != "+15551234567" {
		t.Errorf("imessage.target = %q, want %q", config.Notifications.Providers.IMessage.Target, "+15551234567")
	}
}

func TestAddClaudeCodeHook(t *testing.T) {
	// Create temp directory
	tempDir := t.TempDir()

	// Override home directory for test
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tempDir)
	defer os.Setenv("HOME", origHome)

	wizard := NewWizard(false)
	binaryPath := "/usr/local/bin/claudible"

	err := wizard.addClaudeCodeHook(binaryPath)
	if err != nil {
		t.Fatalf("addClaudeCodeHook() error = %v", err)
	}

	// Verify settings file exists
	settingsPath := filepath.Join(tempDir, ".claude", "settings.json")
	if _, err := os.Stat(settingsPath); os.IsNotExist(err) {
		t.Fatalf("settings file not created at %s", settingsPath)
	}

	// Read and parse settings
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("failed to read settings: %v", err)
	}

	var settings map[string]json.RawMessage
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf("failed to parse settings: %v", err)
	}

	// Verify hooks exist
	hooksRaw, ok := settings["hooks"]
	if !ok {
		t.Fatal("hooks not found in settings")
	}

	var hooks map[string][]HookMatcher
	if err := json.Unmarshal(hooksRaw, &hooks); err != nil {
		t.Fatalf("failed to parse hooks: %v", err)
	}

	// Verify Notification hooks exist
	notificationHooks, ok := hooks["Notification"]
	if !ok {
		t.Fatal("Notification hook not found")
	}

	// Verify we have both idle_prompt and permission_prompt matchers
	matchers := make(map[string]bool)
	for _, matcher := range notificationHooks {
		matchers[matcher.Matcher] = true
		// Verify each has claudible command
		found := false
		for _, hook := range matcher.Hooks {
			if hook.Command == binaryPath {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Notification matcher %q missing claudible command", matcher.Matcher)
		}
	}

	if !matchers["idle_prompt"] {
		t.Error("Notification hook missing idle_prompt matcher")
	}
	if !matchers["permission_prompt"] {
		t.Error("Notification hook missing permission_prompt matcher")
	}

	// Verify Stop hook exists
	stopHooks, ok := hooks["Stop"]
	if !ok {
		t.Fatal("Stop hook not found")
	}

	if len(stopHooks) == 0 {
		t.Fatal("Stop has no hook matchers")
	}

	// Verify Stop hook has claudible command
	found := false
	for _, matcher := range stopHooks {
		for _, hook := range matcher.Hooks {
			if hook.Command == binaryPath {
				found = true
				break
			}
		}
	}
	if !found {
		t.Errorf("hook command %q not found in Stop hooks", binaryPath)
	}
}

func TestAddClaudeCodeHook_ExistingSettings(t *testing.T) {
	// Create temp directory
	tempDir := t.TempDir()

	// Override home directory for test
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tempDir)
	defer os.Setenv("HOME", origHome)

	// Create .claude directory
	claudeDir := filepath.Join(tempDir, ".claude")
	if err := os.MkdirAll(claudeDir, 0755); err != nil {
		t.Fatalf("failed to create .claude dir: %v", err)
	}

	// Create existing settings with other fields
	existingSettings := map[string]any{
		"env": map[string]string{
			"FOO": "bar",
		},
		"permissions": map[string]any{
			"allow": []string{"Bash(ls:*)"},
		},
	}

	data, _ := json.MarshalIndent(existingSettings, "", "  ")
	settingsPath := filepath.Join(claudeDir, "settings.json")
	if err := os.WriteFile(settingsPath, data, 0644); err != nil {
		t.Fatalf("failed to write existing settings: %v", err)
	}

	wizard := NewWizard(false)
	binaryPath := "/usr/local/bin/claudible"

	err := wizard.addClaudeCodeHook(binaryPath)
	if err != nil {
		t.Fatalf("addClaudeCodeHook() error = %v", err)
	}

	// Read updated settings
	data, err = os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("failed to read settings: %v", err)
	}

	var settings map[string]json.RawMessage
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf("failed to parse settings: %v", err)
	}

	// Verify original fields preserved
	if _, ok := settings["env"]; !ok {
		t.Error("env field was removed from settings")
	}
	if _, ok := settings["permissions"]; !ok {
		t.Error("permissions field was removed from settings")
	}

	// Verify hooks added with correct structure
	hooksRaw, ok := settings["hooks"]
	if !ok {
		t.Fatal("hooks field not added to settings")
	}

	var hooks map[string][]HookMatcher
	if err := json.Unmarshal(hooksRaw, &hooks); err != nil {
		t.Fatalf("failed to parse hooks: %v", err)
	}

	// Verify both Notification and Stop hooks exist
	if _, ok := hooks["Notification"]; !ok {
		t.Error("Notification hooks not added")
	}
	if _, ok := hooks["Stop"]; !ok {
		t.Error("Stop hooks not added")
	}
}

func TestWizardNonInteractive(t *testing.T) {
	// Create temp directory
	tempDir := t.TempDir()

	// Override home directory for test
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tempDir)
	defer os.Setenv("HOME", origHome)

	wizard := NewWizard(false)

	// Test with valid phone
	err := wizard.RunNonInteractive("+15551234567")
	if err != nil {
		t.Fatalf("RunNonInteractive() error = %v", err)
	}

	// Verify config created
	configPath := filepath.Join(tempDir, ".config", "claudible", "config.yaml")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Error("config file not created")
	}

	// Verify hooks created
	settingsPath := filepath.Join(tempDir, ".claude", "settings.json")
	if _, err := os.Stat(settingsPath); os.IsNotExist(err) {
		t.Error("settings file not created")
	}
}

func TestWizardNonInteractive_InvalidPhone(t *testing.T) {
	wizard := NewWizard(false)

	// Test with invalid phone
	err := wizard.RunNonInteractive("123")
	if err == nil {
		t.Error("RunNonInteractive() should error on invalid phone")
	}
}
