//go:build darwin

package logging

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
)

// newSystemLogger creates a macOS-specific logger.
// Since we can't use CGO (project requirement: single static binary, no CGO),
// we use a hybrid approach:
// 1. Write to stderr with structured format (visible in Console.app via process name)
// 2. Also write important messages via the `logger` command for syslog compatibility
//
// To view logs in Console.app:
//   - Filter by Process: "claudible"
//   - Or use: log stream --predicate 'process == "claudible"'
func newSystemLogger(level slog.Level) (Logger, error) {
	// Create a multi-writer that writes to both stderr and syslog
	writer := &darwinWriter{
		stderr: os.Stderr,
	}

	handler := slog.NewTextHandler(writer, &slog.HandlerOptions{
		Level: level,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			// Add subsystem prefix to make filtering easier in Console.app
			if a.Key == slog.MessageKey {
				a.Value = slog.StringValue(fmt.Sprintf("[%s] %s", Subsystem, a.Value.String()))
			}
			return a
		},
	})

	return &logger{slog.New(handler)}, nil
}

// darwinWriter writes to stderr and optionally to syslog via the logger command.
type darwinWriter struct {
	stderr io.Writer
	mu     sync.Mutex
}

func (w *darwinWriter) Write(p []byte) (n int, error error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Always write to stderr (this shows up in Console.app when filtering by process)
	n, err := w.stderr.Write(p)
	if err != nil {
		return n, err
	}

	// For error-level messages, also write to syslog via the logger command
	// This ensures critical errors are captured in the system log
	msg := string(p)
	if strings.Contains(msg, "level=ERROR") || strings.Contains(msg, "level=WARN") {
		// Fire and forget - don't block on syslog write
		go w.writeToSyslog(msg)
	}

	return n, nil
}

// writeToSyslog writes a message to syslog using the macOS logger command.
// This is a fallback mechanism for important messages.
func (w *darwinWriter) writeToSyslog(msg string) {
	// Clean the message - remove timestamp and level since syslog adds its own
	msg = strings.TrimSpace(msg)

	// Use the logger command to write to syslog
	// -p user.err sets the facility and priority
	// -t sets the tag (identifier)
	cmd := exec.Command("logger", "-p", "user.err", "-t", Subsystem, msg)
	_ = cmd.Run() // Ignore errors - this is best-effort
}
