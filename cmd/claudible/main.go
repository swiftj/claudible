// Package main provides the CLI entry point for Claudible.
// This executable is designed to be called by Claude Code hooks.
// It reads JSON from stdin and processes notifications based on agent state.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/swiftj/claudible/internal/config"
	"github.com/swiftj/claudible/internal/hook"
	"github.com/swiftj/claudible/internal/logging"
	"github.com/swiftj/claudible/internal/router"
	"github.com/swiftj/claudible/internal/setup"
	"github.com/swiftj/claudible/internal/state"
	"github.com/swiftj/claudible/internal/transcript"
)

const (
	// Version is the current version of Claudible.
	Version = "0.5.2"

	// DefaultTimeout is the default context timeout for processing.
	DefaultTimeout = 30 * time.Second
)

// flags holds the command-line flags.
type flags struct {
	configPath string
	dryRun     bool
	version    bool
	verbose    bool
}

func main() {
	// Check for subcommands first
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "setup":
			runSetup()
			return
		case "help", "-h", "--help":
			printUsage()
			os.Exit(0)
		}
	}

	// Parse command-line flags
	f := parseFlags()

	// Handle --version flag
	if f.version {
		fmt.Printf("claudible version %s\n", Version)
		os.Exit(0)
	}

	// Initialize system logging
	// Verbose mode: debug level; Normal mode: info level (errors always logged)
	logLevel := logging.LevelInfo
	if f.verbose {
		logLevel = logging.LevelDebug
	}
	if err := logging.Init(logLevel); err != nil {
		// Fallback to stderr if system logging fails
		fmt.Fprintf(os.Stderr, "warning: failed to initialize system logging: %v\n", err)
	}
	defer func() { _ = logging.Close() }()

	// Set up signal handling for graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), DefaultTimeout)
	defer cancel()

	// Handle SIGINT and SIGTERM for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigChan
		logging.Debug("received signal, shutting down", "signal", sig)
		cancel()
	}()

	// Run the main logic
	exitCode := run(ctx, f)
	os.Exit(exitCode)
}

// parseFlags parses command-line flags and returns a flags struct.
func parseFlags() flags {
	f := flags{}

	flag.StringVar(&f.configPath, "config", "", "custom config file path")
	flag.BoolVar(&f.dryRun, "dry-run", false, "parse and classify but don't send notifications")
	flag.BoolVar(&f.version, "version", false, "print version and exit")
	flag.BoolVar(&f.verbose, "verbose", false, "enable verbose logging")

	flag.Parse()

	return f
}

// run contains the main application logic and returns an exit code.
func run(ctx context.Context, f flags) int {
	// Load configuration
	cfg, err := loadConfig(f.configPath)
	if err != nil {
		logging.Error("failed to load config", "error", err)
		return 1
	}

	logging.Debug("loaded configuration", "source", configSource(f.configPath))

	// Parse hook input from stdin
	input, err := hook.ParseHookInputFromStdin()
	if err != nil {
		logging.Error("failed to parse hook input", "error", err)
		return 1
	}

	// Validate hook input
	if err := input.Validate(); err != nil {
		logging.Error("invalid hook input", "error", err)
		return 1
	}

	logging.Debug("received hook",
		"session", input.SessionID,
		"type", input.NotificationType,
		"cwd", input.Cwd)

	// Parse transcript for dry-run classification
	t, err := transcript.Parse(input.TranscriptPath)
	if err != nil {
		logging.Warn("failed to parse transcript", "error", err, "path", input.TranscriptPath)
		// Continue with nil transcript - classifier handles this
		t = nil
	}

	// Classify state for dry-run and verbose logging
	classifier := state.NewClassifier()
	agentState := classifier.Classify(input, t)

	logging.Debug("classified state",
		"state", agentState.String(),
		"description", agentState.Description())

	// Check if we should notify for this state
	if !cfg.ShouldNotifyOn(agentState.String()) {
		logging.Debug("notifications disabled for state", "state", agentState)
		return 0
	}

	// Dry run mode - stop before sending notifications
	if f.dryRun {
		logging.Info("dry-run mode", "state", agentState)
		return 0
	}

	// Create router and process the notification
	r := router.NewRouter(cfg)

	if r.Registry().EnabledCount() == 0 {
		logging.Debug("no notification providers enabled")
		return 0
	}

	logging.Debug("sending notification", "providers", r.Registry().EnabledCount())

	// Process the notification through the router
	err = r.Process(ctx, input)
	if err != nil {
		// Log errors but exit 0 - notifications are best-effort
		logging.Error("notification failed", "error", err)
	} else {
		logging.Info("notification sent",
			"session", input.SessionID,
			"state", agentState.String())
	}

	return 0
}

// loadConfig loads configuration from the specified path or default location.
func loadConfig(configPath string) (*config.Config, error) {
	if configPath != "" {
		return config.LoadFromPath(configPath)
	}
	return config.Load()
}

// configSource returns a description of where config was loaded from.
func configSource(configPath string) string {
	if configPath != "" {
		return configPath
	}
	defaultPath, err := config.DefaultConfigPath()
	if err != nil {
		return "default"
	}
	return defaultPath
}

// runSetup handles the 'setup' subcommand.
func runSetup() {
	// Parse setup-specific flags
	setupFlags := flag.NewFlagSet("setup", flag.ExitOnError)
	phone := setupFlags.String("phone", "", "phone number for iMessage (non-interactive mode)")
	verbose := setupFlags.Bool("verbose", false, "enable verbose output")

	// Skip "setup" in args
	if err := setupFlags.Parse(os.Args[2:]); err != nil {
		fmt.Fprintf(os.Stderr, "claudible setup: %v\n", err)
		os.Exit(1)
	}

	wizard := setup.NewWizard(*verbose)

	var err error
	if *phone != "" {
		// Non-interactive mode
		err = wizard.RunNonInteractive(*phone)
	} else {
		// Interactive mode
		err = wizard.Run()
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "Setup failed: %v\n", err)
		os.Exit(1)
	}
}

// printUsage prints the usage message.
func printUsage() {
	fmt.Printf(`Claudible - Claude Code Notification System v%s

Usage:
  claudible [flags]           Process hook input from stdin (normal mode)
  claudible setup [flags]     Interactive setup wizard

Flags:
  --config PATH    Custom config file path
  --dry-run        Parse and classify but don't send notifications
  --verbose        Enable verbose logging
  --version        Print version and exit

Setup Flags:
  --phone NUMBER   Phone number for iMessage (skip interactive prompt)
  --verbose        Enable verbose output

Examples:
  # Interactive setup (recommended for first-time users)
  claudible setup

  # Non-interactive setup with phone number
  claudible setup --phone "+15551234567"

  # Test hook processing without sending notifications
  echo '{"session_id":"test"}' | claudible --dry-run --verbose

For more information, visit: https://github.com/swiftj/claudible
`, Version)
}
