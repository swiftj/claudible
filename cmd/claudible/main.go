// Package main provides the CLI entry point for Claudible.
// This executable is designed to be called by Claude Code hooks.
// It reads JSON from stdin and processes notifications based on agent state.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/swiftj/claudible/internal/config"
	"github.com/swiftj/claudible/internal/hook"
	"github.com/swiftj/claudible/internal/router"
	"github.com/swiftj/claudible/internal/setup"
	"github.com/swiftj/claudible/internal/state"
	"github.com/swiftj/claudible/internal/transcript"
)

const (
	// Version is the current version of Claudible.
	Version = "0.1.0"

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

	// Set up logger to write to stderr (stdout reserved for future use)
	logger := log.New(os.Stderr, "claudible: ", log.LstdFlags)

	// Set up signal handling for graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), DefaultTimeout)
	defer cancel()

	// Handle SIGINT and SIGTERM for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigChan
		if f.verbose {
			logger.Printf("received signal: %v, shutting down", sig)
		}
		cancel()
	}()

	// Run the main logic
	exitCode := run(ctx, f, logger)
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
func run(ctx context.Context, f flags, logger *log.Logger) int {
	// Load configuration
	cfg, err := loadConfig(f.configPath)
	if err != nil {
		logger.Printf("error loading config: %v", err)
		return 1
	}

	if f.verbose {
		logger.Printf("loaded configuration from: %s", configSource(f.configPath))
	}

	// Parse hook input from stdin
	input, err := hook.ParseHookInputFromStdin()
	if err != nil {
		logger.Printf("error parsing hook input: %v", err)
		return 1
	}

	// Validate hook input
	if err := input.Validate(); err != nil {
		logger.Printf("invalid hook input: %v", err)
		return 1
	}

	if f.verbose {
		logger.Printf("received hook: session=%s, type=%s, cwd=%s",
			input.SessionID, input.NotificationType, input.Cwd)
	}

	// Parse transcript for dry-run classification
	t, err := transcript.Parse(input.TranscriptPath)
	if err != nil {
		if f.verbose {
			logger.Printf("warning: failed to parse transcript: %v", err)
		}
		// Continue with nil transcript - classifier handles this
		t = nil
	}

	// Classify state for dry-run and verbose logging
	classifier := state.NewClassifier()
	agentState := classifier.Classify(input, t)

	if f.verbose {
		logger.Printf("classified state: %s (%s)", agentState, agentState.Description())
	}

	// Check if we should notify for this state
	if !cfg.ShouldNotifyOn(agentState.String()) {
		if f.verbose {
			logger.Printf("notifications disabled for state: %s", agentState)
		}
		return 0
	}

	// Dry run mode - stop before sending notifications
	if f.dryRun {
		logger.Printf("dry-run: would send notification for state=%s", agentState)
		return 0
	}

	// Create router and process the notification
	r := router.NewRouter(cfg)

	if r.Registry().EnabledCount() == 0 {
		if f.verbose {
			logger.Printf("no notification providers enabled")
		}
		return 0
	}

	if f.verbose {
		logger.Printf("sending notification to %d provider(s)", r.Registry().EnabledCount())
	}

	// Process the notification through the router
	err = r.Process(ctx, input)
	if err != nil {
		// Log errors but exit 0 - notifications are best-effort
		logger.Printf("notification error: %v", err)
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
