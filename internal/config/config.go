// Package config provides YAML configuration loading for Claudible.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// Config represents the root configuration structure for Claudible.
type Config struct {
	Notifications Notifications   `yaml:"notifications"`
	Behavior      Behavior        `yaml:"behavior"`
	Evaluator     EvaluatorConfig `yaml:"evaluator"`
}

// Notifications holds notification provider configuration.
type Notifications struct {
	DefaultProvider string    `yaml:"default_provider"`
	Providers       Providers `yaml:"providers"`
}

// Providers contains all available notification provider configurations.
type Providers struct {
	IMessage   IMessageConfig   `yaml:"imessage"`
	SMS        SMSConfig        `yaml:"sms"`
	HTTP       HTTPConfig       `yaml:"http"`
	Desktop    DesktopConfig    `yaml:"desktop"`
	Pushover   PushoverConfig   `yaml:"pushover"`
	Pushbullet PushbulletConfig `yaml:"pushbullet"`
}

// IMessageConfig holds iMessage notification settings.
type IMessageConfig struct {
	Enabled bool   `yaml:"enabled"`
	Target  string `yaml:"target"`
}

// SMSConfig holds SMS notification settings.
type SMSConfig struct {
	Enabled    bool   `yaml:"enabled"`
	Provider   string `yaml:"provider"` // twilio, vonage, plivo
	From       string `yaml:"from"`
	To         string `yaml:"to"`
	// Twilio
	AccountSID string `yaml:"account_sid"`
	AuthToken  string `yaml:"auth_token"`
	// Vonage
	APIKey    string `yaml:"api_key"`
	APISecret string `yaml:"api_secret"`
	// Plivo
	AuthID string `yaml:"auth_id"`
	// auth_token is reused for Plivo
}

// HTTPConfig holds HTTP webhook notification settings.
type HTTPConfig struct {
	Enabled  bool              `yaml:"enabled"`
	Endpoint string            `yaml:"endpoint"`
	Headers  map[string]string `yaml:"headers"`
}

// DesktopConfig holds desktop notification settings.
type DesktopConfig struct {
	Enabled bool `yaml:"enabled"`
}

// PushoverConfig holds Pushover notification settings.
type PushoverConfig struct {
	Enabled  bool   `yaml:"enabled"`
	AppToken string `yaml:"app_token"`
	UserKey  string `yaml:"user_key"`
	Priority int    `yaml:"priority"` // -2 to 2, default 0
}

// PushbulletConfig holds Pushbullet notification settings.
type PushbulletConfig struct {
	Enabled    bool   `yaml:"enabled"`
	APIKey     string `yaml:"api_key"`
	DeviceIden string `yaml:"device_iden"` // optional, target specific device
}

// EvaluatorConfig holds LLM evaluator settings for contextual analysis.
type EvaluatorConfig struct {
	Enabled   bool   `yaml:"enabled"`
	Provider  string `yaml:"provider"`   // anthropic, openai
	APIKey    string `yaml:"api_key"`
	Model     string `yaml:"model"`      // claude-3-haiku-20240307, gpt-4o-mini
	MaxTokens int    `yaml:"max_tokens"`
}

// Behavior holds notification behavior settings.
type Behavior struct {
	NotifyOn         []string      `yaml:"notify_on"`
	MaxMessageLength int           `yaml:"max_message_length"`
	IncludeRepoPath  bool          `yaml:"include_repo_path"`
	DedupeWindow     time.Duration `yaml:"dedupe_window"` // Time window for deduplication (default 30s)
}

// DefaultConfigPath returns the default configuration file path.
func DefaultConfigPath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get user home directory: %w", err)
	}
	return filepath.Join(homeDir, ".config", "claudible", "config.yaml"), nil
}

// Default returns a Config with sensible default values.
// iMessage is enabled by default as the primary notification method.
func Default() *Config {
	return &Config{
		Notifications: Notifications{
			DefaultProvider: "imessage",
			Providers: Providers{
				IMessage: IMessageConfig{
					Enabled: true,
					Target:  "",
				},
				SMS: SMSConfig{
					Enabled:    false,
					Provider:   "twilio",
					From:       "",
					To:         "",
					AccountSID: "",
					AuthToken:  "",
					APIKey:     "",
					APISecret:  "",
					AuthID:     "",
				},
				HTTP: HTTPConfig{
					Enabled:  false,
					Endpoint: "",
					Headers:  make(map[string]string),
				},
				Desktop: DesktopConfig{
					Enabled: false,
				},
				Pushover: PushoverConfig{
					Enabled:  false,
					AppToken: "",
					UserKey:  "",
					Priority: 0,
				},
				Pushbullet: PushbulletConfig{
					Enabled:    false,
					APIKey:     "",
					DeviceIden: "",
				},
			},
		},
		Behavior: Behavior{
			NotifyOn:         []string{"complete", "waiting"},
			MaxMessageLength: 900,
			IncludeRepoPath:  true,
			DedupeWindow:     30 * time.Second,
		},
		Evaluator: EvaluatorConfig{
			Enabled:   false,
			Provider:  "",
			APIKey:    "",
			Model:     "",
			MaxTokens: 1000,
		},
	}
}

// Load reads configuration from the default path (~/.config/claudible/config.yaml).
// If the file does not exist, it returns default configuration.
func Load() (*Config, error) {
	configPath, err := DefaultConfigPath()
	if err != nil {
		return nil, err
	}
	return LoadFromPath(configPath)
}

// LoadFromPath reads configuration from the specified file path.
// If the file does not exist, it returns default configuration.
func LoadFromPath(path string) (*Config, error) {
	cfg := Default()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// Return default config if file doesn't exist
			return cfg, nil
		}
		return nil, fmt.Errorf("failed to read config file %s: %w", path, err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file %s: %w", path, err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return cfg, nil
}

// Validate checks the configuration for validity and returns an error if invalid.
func (c *Config) Validate() error {
	var errs []error

	// Validate default provider exists and is enabled
	if c.Notifications.DefaultProvider != "" {
		if !c.isProviderValid(c.Notifications.DefaultProvider) {
			errs = append(errs, fmt.Errorf("unknown default_provider: %s", c.Notifications.DefaultProvider))
		}
	}

	// Validate iMessage config
	if c.Notifications.Providers.IMessage.Enabled {
		if c.Notifications.Providers.IMessage.Target == "" {
			errs = append(errs, errors.New("imessage.target is required when imessage is enabled"))
		}
	}

	// Validate SMS config
	if c.Notifications.Providers.SMS.Enabled {
		sms := c.Notifications.Providers.SMS
		if sms.Provider == "" {
			errs = append(errs, errors.New("sms.provider is required when sms is enabled"))
		}
		if sms.From == "" {
			errs = append(errs, errors.New("sms.from is required when sms is enabled"))
		}
		if sms.To == "" {
			errs = append(errs, errors.New("sms.to is required when sms is enabled"))
		}
		switch sms.Provider {
		case "twilio":
			if sms.AccountSID == "" {
				errs = append(errs, errors.New("sms.account_sid is required for twilio provider"))
			}
			if sms.AuthToken == "" {
				errs = append(errs, errors.New("sms.auth_token is required for twilio provider"))
			}
		case "vonage":
			if sms.APIKey == "" {
				errs = append(errs, errors.New("sms.api_key is required for vonage provider"))
			}
			if sms.APISecret == "" {
				errs = append(errs, errors.New("sms.api_secret is required for vonage provider"))
			}
		case "plivo":
			if sms.AuthID == "" {
				errs = append(errs, errors.New("sms.auth_id is required for plivo provider"))
			}
			if sms.AuthToken == "" {
				errs = append(errs, errors.New("sms.auth_token is required for plivo provider"))
			}
		}
	}

	// Validate HTTP config
	if c.Notifications.Providers.HTTP.Enabled {
		if c.Notifications.Providers.HTTP.Endpoint == "" {
			errs = append(errs, errors.New("http.endpoint is required when http is enabled"))
		}
	}

	// Validate Pushover config
	if c.Notifications.Providers.Pushover.Enabled {
		pushover := c.Notifications.Providers.Pushover
		if pushover.AppToken == "" {
			errs = append(errs, errors.New("pushover.app_token is required when pushover is enabled"))
		}
		if pushover.UserKey == "" {
			errs = append(errs, errors.New("pushover.user_key is required when pushover is enabled"))
		}
		if pushover.Priority < -2 || pushover.Priority > 2 {
			errs = append(errs, errors.New("pushover.priority must be between -2 and 2"))
		}
	}

	// Validate Pushbullet config
	if c.Notifications.Providers.Pushbullet.Enabled {
		if c.Notifications.Providers.Pushbullet.APIKey == "" {
			errs = append(errs, errors.New("pushbullet.api_key is required when pushbullet is enabled"))
		}
	}

	// Validate Evaluator config
	if c.Evaluator.Enabled {
		if c.Evaluator.Provider == "" {
			errs = append(errs, errors.New("evaluator.provider is required when evaluator is enabled"))
		}
		if c.Evaluator.APIKey == "" {
			errs = append(errs, errors.New("evaluator.api_key is required when evaluator is enabled"))
		}
		if c.Evaluator.Model == "" {
			errs = append(errs, errors.New("evaluator.model is required when evaluator is enabled"))
		}
		if c.Evaluator.Provider != "" && c.Evaluator.Provider != "anthropic" && c.Evaluator.Provider != "openai" {
			errs = append(errs, fmt.Errorf("evaluator.provider must be 'anthropic' or 'openai', got: %s", c.Evaluator.Provider))
		}
	}

	// Validate behavior
	if c.Behavior.MaxMessageLength <= 0 {
		errs = append(errs, errors.New("behavior.max_message_length must be positive"))
	}

	validNotifyStates := map[string]bool{
		"complete": true,
		"waiting":  true,
		"stopped":  true,
		"idle":     true,
	}
	for _, state := range c.Behavior.NotifyOn {
		if !validNotifyStates[state] {
			errs = append(errs, fmt.Errorf("invalid notify_on state: %s", state))
		}
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	return nil
}

// isProviderValid checks if the given provider name is a known provider.
func (c *Config) isProviderValid(provider string) bool {
	validProviders := map[string]bool{
		"imessage":   true,
		"sms":        true,
		"http":       true,
		"desktop":    true,
		"pushover":   true,
		"pushbullet": true,
	}
	return validProviders[provider]
}

// IsProviderEnabled returns true if the specified provider is enabled.
func (c *Config) IsProviderEnabled(provider string) bool {
	switch provider {
	case "imessage":
		return c.Notifications.Providers.IMessage.Enabled
	case "sms":
		return c.Notifications.Providers.SMS.Enabled
	case "http":
		return c.Notifications.Providers.HTTP.Enabled
	case "desktop":
		return c.Notifications.Providers.Desktop.Enabled
	case "pushover":
		return c.Notifications.Providers.Pushover.Enabled
	case "pushbullet":
		return c.Notifications.Providers.Pushbullet.Enabled
	default:
		return false
	}
}

// GetEnabledProviders returns a list of all enabled provider names.
func (c *Config) GetEnabledProviders() []string {
	var providers []string
	if c.Notifications.Providers.IMessage.Enabled {
		providers = append(providers, "imessage")
	}
	if c.Notifications.Providers.SMS.Enabled {
		providers = append(providers, "sms")
	}
	if c.Notifications.Providers.HTTP.Enabled {
		providers = append(providers, "http")
	}
	if c.Notifications.Providers.Desktop.Enabled {
		providers = append(providers, "desktop")
	}
	if c.Notifications.Providers.Pushover.Enabled {
		providers = append(providers, "pushover")
	}
	if c.Notifications.Providers.Pushbullet.Enabled {
		providers = append(providers, "pushbullet")
	}
	return providers
}

// ShouldNotifyOn returns true if notifications should be sent for the given state.
func (c *Config) ShouldNotifyOn(state string) bool {
	for _, s := range c.Behavior.NotifyOn {
		if s == state {
			return true
		}
	}
	return false
}
