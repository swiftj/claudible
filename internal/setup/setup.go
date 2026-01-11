// Package setup provides the interactive setup wizard for Claudible.
// It handles creating configuration files and integrating with Claude Code hooks.
package setup

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"gopkg.in/yaml.v3"
)

// Wizard handles the interactive setup process for Claudible.
type Wizard struct {
	reader  *bufio.Reader
	verbose bool
}

// NewWizard creates a new setup wizard.
func NewWizard(verbose bool) *Wizard {
	return &Wizard{
		reader:  bufio.NewReader(os.Stdin),
		verbose: verbose,
	}
}

// Run executes the setup wizard interactively.
func (w *Wizard) Run() error {
	fmt.Println("🔔 Claudible Setup Wizard")
	fmt.Println("========================")
	fmt.Println()

	// Check platform
	if runtime.GOOS != "darwin" {
		fmt.Println("⚠️  Warning: iMessage notifications only work on macOS.")
		fmt.Println("   You can still set up Claudible, but you may need to configure")
		fmt.Println("   a different provider (SMS, Pushover, etc.) manually.")
		fmt.Println()
	}

	// Get phone number
	phone, err := w.promptPhoneNumber()
	if err != nil {
		return err
	}

	// Get claudible binary path
	binaryPath, err := w.findBinaryPath()
	if err != nil {
		return fmt.Errorf("could not determine claudible binary path: %w", err)
	}

	// Create claudible config
	configPath, err := w.createClaudibleConfig(phone)
	if err != nil {
		return fmt.Errorf("failed to create claudible config: %w", err)
	}

	// Add Claude Code hook
	if err := w.addClaudeCodeHook(binaryPath); err != nil {
		return fmt.Errorf("failed to add Claude Code hook: %w", err)
	}

	// Print success message
	w.printSuccess(configPath, binaryPath)

	return nil
}

// RunNonInteractive runs setup with provided values (for scripting).
func (w *Wizard) RunNonInteractive(phone string) error {
	// Validate phone number
	if !isValidPhone(phone) {
		return fmt.Errorf("invalid phone number format: %s", phone)
	}

	// Normalize phone number
	phone = normalizePhone(phone)

	// Get claudible binary path
	binaryPath, err := w.findBinaryPath()
	if err != nil {
		return fmt.Errorf("could not determine claudible binary path: %w", err)
	}

	// Create claudible config
	configPath, err := w.createClaudibleConfig(phone)
	if err != nil {
		return fmt.Errorf("failed to create claudible config: %w", err)
	}

	// Add Claude Code hook
	if err := w.addClaudeCodeHook(binaryPath); err != nil {
		return fmt.Errorf("failed to add Claude Code hook: %w", err)
	}

	// Print success message
	w.printSuccess(configPath, binaryPath)

	return nil
}

// promptPhoneNumber asks for and validates the phone number.
func (w *Wizard) promptPhoneNumber() (string, error) {
	fmt.Println("📱 Enter your phone number for iMessage notifications.")
	fmt.Println("   Format: +1XXXXXXXXXX (include country code)")
	fmt.Println("   You can also use an Apple ID email address.")
	fmt.Println()

	for {
		fmt.Print("Phone number or email: ")
		input, err := w.reader.ReadString('\n')
		if err != nil {
			return "", fmt.Errorf("failed to read input: %w", err)
		}

		input = strings.TrimSpace(input)
		if input == "" {
			fmt.Println("❌ Phone number cannot be empty. Please try again.")
			continue
		}

		// Check if it's an email
		if strings.Contains(input, "@") {
			return input, nil
		}

		// Validate and normalize phone number
		if !isValidPhone(input) {
			fmt.Println("❌ Invalid phone number format. Please use +1XXXXXXXXXX format.")
			continue
		}

		return normalizePhone(input), nil
	}
}

// isValidPhone validates a phone number format.
func isValidPhone(phone string) bool {
	// Accept various formats, we'll normalize them
	// +1XXXXXXXXXX, 1XXXXXXXXXX, XXXXXXXXXX, (XXX) XXX-XXXX, etc.
	cleaned := regexp.MustCompile(`[^\d+]`).ReplaceAllString(phone, "")

	// Must have at least 10 digits
	digits := regexp.MustCompile(`\d`).FindAllString(cleaned, -1)
	return len(digits) >= 10
}

// normalizePhone converts a phone number to +1XXXXXXXXXX format.
func normalizePhone(phone string) string {
	// Remove all non-digit characters except leading +
	cleaned := regexp.MustCompile(`[^\d+]`).ReplaceAllString(phone, "")

	// If it starts with +, keep it
	if strings.HasPrefix(cleaned, "+") {
		return cleaned
	}

	// If it starts with 1 and has 11 digits, add +
	if strings.HasPrefix(cleaned, "1") && len(cleaned) == 11 {
		return "+" + cleaned
	}

	// Otherwise assume US number, add +1
	return "+1" + cleaned
}

// findBinaryPath returns the path to the claudible binary.
func (w *Wizard) findBinaryPath() (string, error) {
	// First, check if we're running from the binary itself
	execPath, err := os.Executable()
	if err == nil {
		// Resolve symlinks
		execPath, err = filepath.EvalSymlinks(execPath)
		if err == nil {
			return execPath, nil
		}
	}

	// Check common installation locations
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	possiblePaths := []string{
		filepath.Join(homeDir, "go", "bin", "claudible"),
		"/usr/local/bin/claudible",
		"/opt/homebrew/bin/claudible",
	}

	for _, path := range possiblePaths {
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}

	// Return the expected GOPATH/bin location even if not found yet
	return filepath.Join(homeDir, "go", "bin", "claudible"), nil
}

// ClaudibleConfig represents the claudible configuration file structure.
type ClaudibleConfig struct {
	Notifications NotificationsConfig `yaml:"notifications"`
	Behavior      BehaviorConfig      `yaml:"behavior"`
}

// NotificationsConfig holds notification settings.
type NotificationsConfig struct {
	DefaultProvider string          `yaml:"default_provider"`
	Providers       ProvidersConfig `yaml:"providers"`
}

// ProvidersConfig holds provider-specific settings.
type ProvidersConfig struct {
	IMessage IMessageConfig `yaml:"imessage"`
}

// IMessageConfig holds iMessage settings.
type IMessageConfig struct {
	Enabled bool   `yaml:"enabled"`
	Target  string `yaml:"target"`
}

// BehaviorConfig holds behavior settings.
type BehaviorConfig struct {
	NotifyOn         []string `yaml:"notify_on"`
	MaxMessageLength int      `yaml:"max_message_length"`
	IncludeRepoPath  bool     `yaml:"include_repo_path"`
}

// createClaudibleConfig creates the claudible configuration file.
func (w *Wizard) createClaudibleConfig(target string) (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	configDir := filepath.Join(homeDir, ".config", "claudible")
	configPath := filepath.Join(configDir, "config.yaml")

	// Create config directory
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create config directory: %w", err)
	}

	// Check if config already exists
	if _, err := os.Stat(configPath); err == nil {
		fmt.Printf("⚠️  Config file already exists at %s\n", configPath)
		fmt.Print("   Overwrite? [y/N]: ")
		response, _ := w.reader.ReadString('\n')
		response = strings.TrimSpace(strings.ToLower(response))
		if response != "y" && response != "yes" {
			fmt.Println("   Keeping existing config.")
			return configPath, nil
		}
	}

	// Create config
	config := ClaudibleConfig{
		Notifications: NotificationsConfig{
			DefaultProvider: "imessage",
			Providers: ProvidersConfig{
				IMessage: IMessageConfig{
					Enabled: true,
					Target:  target,
				},
			},
		},
		Behavior: BehaviorConfig{
			NotifyOn:         []string{"complete", "waiting"},
			MaxMessageLength: 900,
			IncludeRepoPath:  true,
		},
	}

	// Marshal to YAML
	data, err := yaml.Marshal(&config)
	if err != nil {
		return "", fmt.Errorf("failed to marshal config: %w", err)
	}

	// Add header comment
	header := `# Claudible Configuration
# Created by claudible setup
# Edit this file to customize notification behavior

`
	data = append([]byte(header), data...)

	// Write config file
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return "", fmt.Errorf("failed to write config file: %w", err)
	}

	if w.verbose {
		fmt.Printf("✅ Created config at %s\n", configPath)
	}

	return configPath, nil
}

// ClaudeSettings represents the Claude Code settings.json structure.
type ClaudeSettings struct {
	Hooks map[string][]HookMatcher `json:"hooks,omitempty"`
	// Other fields are preserved as raw JSON
	Other map[string]json.RawMessage `json:"-"`
}

// HookMatcher represents a hook matcher configuration.
type HookMatcher struct {
	Matcher string `json:"matcher"`
	Hooks   []Hook `json:"hooks"`
}

// Hook represents an individual hook.
type Hook struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

// addClaudeCodeHook adds Notification and Stop hooks to Claude Code settings.
// This configures claudible to receive notifications when:
// - Claude is idle waiting for user input (idle_prompt)
// - Claude is blocked waiting for permission (permission_prompt)
// - Claude stops for any reason (completion, interruption, error)
func (w *Wizard) addClaudeCodeHook(binaryPath string) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	settingsPath := filepath.Join(homeDir, ".claude", "settings.json")

	// Ensure .claude directory exists
	claudeDir := filepath.Join(homeDir, ".claude")
	if err := os.MkdirAll(claudeDir, 0755); err != nil {
		return fmt.Errorf("failed to create .claude directory: %w", err)
	}

	// Read existing settings or create new
	var rawSettings map[string]json.RawMessage
	if data, err := os.ReadFile(settingsPath); err == nil {
		if err := json.Unmarshal(data, &rawSettings); err != nil {
			return fmt.Errorf("failed to parse existing settings.json: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("failed to read settings.json: %w", err)
	} else {
		rawSettings = make(map[string]json.RawMessage)
	}

	// Parse existing hooks if any
	var hooks map[string][]HookMatcher
	if rawHooks, ok := rawSettings["hooks"]; ok {
		if err := json.Unmarshal(rawHooks, &hooks); err != nil {
			return fmt.Errorf("failed to parse existing hooks: %w", err)
		}
	} else {
		hooks = make(map[string][]HookMatcher)
	}

	// Create the claudible hook command
	claudibleHook := Hook{
		Type:    "command",
		Command: binaryPath,
	}

	// Configure Notification hooks (idle_prompt and permission_prompt)
	hooks["Notification"] = w.updateOrAddHooks(hooks["Notification"], []HookMatcher{
		{
			Matcher: "idle_prompt",
			Hooks:   []Hook{claudibleHook},
		},
		{
			Matcher: "permission_prompt",
			Hooks:   []Hook{claudibleHook},
		},
	}, binaryPath)

	// Configure Stop hooks (fires on completion, interruption, or error)
	hooks["Stop"] = w.updateOrAddHooks(hooks["Stop"], []HookMatcher{
		{
			Matcher: "", // Empty matcher catches all stop events
			Hooks:   []Hook{claudibleHook},
		},
	}, binaryPath)

	// Marshal hooks back
	hooksData, err := json.Marshal(hooks)
	if err != nil {
		return fmt.Errorf("failed to marshal hooks: %w", err)
	}
	rawSettings["hooks"] = hooksData

	// Write settings back with pretty formatting
	output, err := json.MarshalIndent(rawSettings, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal settings: %w", err)
	}

	if err := os.WriteFile(settingsPath, output, 0644); err != nil {
		return fmt.Errorf("failed to write settings.json: %w", err)
	}

	if w.verbose {
		fmt.Printf("✅ Added hooks to %s\n", settingsPath)
	}

	return nil
}

// updateOrAddHooks updates existing claudible hooks or adds new ones.
// It preserves non-claudible hooks and updates/adds claudible hooks by matcher.
func (w *Wizard) updateOrAddHooks(existing []HookMatcher, desired []HookMatcher, _ string) []HookMatcher {
	// Build a map of desired matchers for quick lookup
	desiredByMatcher := make(map[string]HookMatcher)
	for _, d := range desired {
		desiredByMatcher[d.Matcher] = d
	}

	// Track which desired matchers we've handled
	handled := make(map[string]bool)

	// Update existing hooks
	result := make([]HookMatcher, 0, len(existing)+len(desired))
	for _, matcher := range existing {
		// Check if this matcher has a claudible hook
		hasClaudible := false
		for _, hook := range matcher.Hooks {
			if strings.Contains(hook.Command, "claudible") {
				hasClaudible = true
				break
			}
		}

		if hasClaudible {
			// If we have a desired hook for this matcher, replace the claudible hook
			if desiredHook, ok := desiredByMatcher[matcher.Matcher]; ok {
				// Update claudible hooks, preserve others
				newHooks := make([]Hook, 0, len(matcher.Hooks))
				for _, hook := range matcher.Hooks {
					if !strings.Contains(hook.Command, "claudible") {
						newHooks = append(newHooks, hook)
					}
				}
				newHooks = append(newHooks, desiredHook.Hooks...)
				matcher.Hooks = newHooks
				handled[matcher.Matcher] = true
			}
		}
		result = append(result, matcher)
	}

	// Add any desired hooks that weren't already present
	for _, d := range desired {
		if !handled[d.Matcher] {
			// Check if there's an existing matcher without claudible
			found := false
			for i, matcher := range result {
				if matcher.Matcher == d.Matcher {
					// Add claudible hook to existing matcher
					result[i].Hooks = append(result[i].Hooks, d.Hooks...)
					found = true
					break
				}
			}
			if !found {
				result = append(result, d)
			}
		}
	}

	return result
}

// printSuccess prints the success message with next steps.
func (w *Wizard) printSuccess(configPath, binaryPath string) {
	fmt.Println()
	fmt.Println("✅ Claudible setup complete!")
	fmt.Println()
	fmt.Println("📋 What was configured:")
	fmt.Printf("   • Config file: %s\n", configPath)
	fmt.Printf("   • Binary path: %s\n", binaryPath)
	fmt.Println("   • Claude Code hooks:")
	fmt.Println("     - Notification (idle_prompt) → claudible")
	fmt.Println("     - Notification (permission_prompt) → claudible")
	fmt.Println("     - Stop → claudible")
	fmt.Println()
	fmt.Println("🎉 You're all set! Claude Code will now send you iMessage")
	fmt.Println("   notifications when:")
	fmt.Println("   • Claude finishes and is waiting for your input")
	fmt.Println("   • Claude needs permission to proceed")
	fmt.Println("   • Claude stops for any reason (completion/error)")
	fmt.Println()
	fmt.Println("💡 Tips:")
	fmt.Println("   • Edit the config to customize notification behavior")
	fmt.Println("   • Run 'claudible --dry-run' to test without sending")
	fmt.Println("   • Use '/hooks' in Claude Code to view registered hooks")
}
