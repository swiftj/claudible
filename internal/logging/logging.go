// Package logging provides unified system logging for claudible.
// It integrates with the native logging infrastructure on each platform:
// - macOS: Writes to stderr with structured format visible in Console.app
// - Linux: Uses syslog for integration with journalctl/syslog
// - Windows: Stub that writes to stderr (full implementation pending)
//
// All logging uses Go's log/slog for structured, leveled logging.
package logging

import (
	"context"
	"io"
	"log/slog"
	"os"
	"sync"
)

// Subsystem is the identifier for claudible in system logs.
// On macOS, this can be used with: log stream --predicate 'process == "claudible"'
const Subsystem = "claudible"

// Level represents the logging level.
type Level = slog.Level

// Log levels.
const (
	LevelDebug = slog.LevelDebug
	LevelInfo  = slog.LevelInfo
	LevelWarn  = slog.LevelWarn
	LevelError = slog.LevelError
)

// Logger is the interface for structured logging.
type Logger interface {
	Debug(msg string, args ...any)
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
	With(args ...any) Logger
	Enabled(level Level) bool
}

// logger wraps slog.Logger to implement our Logger interface.
type logger struct {
	*slog.Logger
}

func (l *logger) Debug(msg string, args ...any) {
	l.Logger.Debug(msg, args...)
}

func (l *logger) Info(msg string, args ...any) {
	l.Logger.Info(msg, args...)
}

func (l *logger) Warn(msg string, args ...any) {
	l.Logger.Warn(msg, args...)
}

func (l *logger) Error(msg string, args ...any) {
	l.Logger.Error(msg, args...)
}

func (l *logger) With(args ...any) Logger {
	return &logger{l.Logger.With(args...)}
}

func (l *logger) Enabled(level Level) bool {
	return l.Logger.Enabled(context.Background(), level)
}

// Global logger instance.
var (
	globalLogger Logger
	globalMu     sync.RWMutex
	initialized  bool
)

// Init initializes the global logger with the specified level.
// It automatically selects the appropriate backend for the current platform.
// This should be called once at application startup.
func Init(level Level) error {
	globalMu.Lock()
	defer globalMu.Unlock()

	l, err := newSystemLogger(level)
	if err != nil {
		return err
	}

	globalLogger = l
	initialized = true
	return nil
}

// InitWithWriter initializes the global logger with a custom writer.
// Useful for testing or custom logging destinations.
func InitWithWriter(w io.Writer, level Level) {
	globalMu.Lock()
	defer globalMu.Unlock()

	handler := slog.NewTextHandler(w, &slog.HandlerOptions{
		Level: level,
	})
	globalLogger = &logger{slog.New(handler)}
	initialized = true
}

// Get returns the global logger.
// If Init has not been called, it returns a default stderr logger.
func Get() Logger {
	globalMu.RLock()
	defer globalMu.RUnlock()

	if !initialized || globalLogger == nil {
		// Return a default stderr logger if not initialized
		handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
			Level: LevelInfo,
		})
		return &logger{slog.New(handler)}
	}
	return globalLogger
}

// Close releases any resources held by the logger.
// Should be called during application shutdown.
func Close() error {
	globalMu.Lock()
	defer globalMu.Unlock()

	if closer, ok := globalLogger.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

// Convenience functions that use the global logger.

// Debug logs a debug message.
func Debug(msg string, args ...any) {
	Get().Debug(msg, args...)
}

// Info logs an info message.
func Info(msg string, args ...any) {
	Get().Info(msg, args...)
}

// Warn logs a warning message.
func Warn(msg string, args ...any) {
	Get().Warn(msg, args...)
}

// Error logs an error message.
func Error(msg string, args ...any) {
	Get().Error(msg, args...)
}

// With returns a new logger with the given attributes.
func With(args ...any) Logger {
	return Get().With(args...)
}
