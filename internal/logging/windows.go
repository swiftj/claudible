//go:build windows

package logging

import (
	"log/slog"
	"os"
)

// newSystemLogger creates a Windows-specific logger.
// TODO: Implement Windows Event Log integration.
// For now, this falls back to stderr logging.
//
// Future implementation could use:
//   - golang.org/x/sys/windows/svc/eventlog
//   - Windows Event Viewer filtering by source "claudible"
func newSystemLogger(level slog.Level) (Logger, error) {
	// Fall back to stderr logging on Windows
	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: level,
	})
	return &logger{slog.New(handler)}, nil
}
