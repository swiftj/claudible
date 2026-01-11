//go:build !darwin && !linux && !windows

package logging

import (
	"log/slog"
	"os"
)

// newSystemLogger creates a fallback logger for unsupported platforms.
// Uses stderr logging as a safe default.
func newSystemLogger(level slog.Level) (Logger, error) {
	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: level,
	})
	return &logger{slog.New(handler)}, nil
}
