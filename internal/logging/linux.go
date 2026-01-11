//go:build linux

package logging

import (
	"context"
	"log/slog"
	"log/syslog"
	"os"
)

// newSystemLogger creates a Linux-specific logger using syslog.
// Logs can be viewed with:
//   - journalctl -t claudible
//   - grep claudible /var/log/syslog
func newSystemLogger(level slog.Level) (Logger, error) {
	// Connect to the local syslog daemon
	writer, err := syslog.New(
		syslog.LOG_INFO|syslog.LOG_DAEMON,
		Subsystem,
	)
	if err != nil {
		// Fall back to stderr if syslog is unavailable
		return newFallbackLogger(level), nil
	}

	// Create a syslog-aware handler
	handler := &syslogHandler{
		writer: writer,
		level:  level,
	}

	l := &syslogLogger{
		writer: writer,
	}
	l.logger = &logger{slog.New(handler)}
	return l, nil
}

// syslogLogger wraps the logger to properly close the syslog connection.
type syslogLogger struct {
	*logger
	writer *syslog.Writer
}

func (l *syslogLogger) Close() error {
	if l.writer != nil {
		return l.writer.Close()
	}
	return nil
}

// syslogHandler implements slog.Handler for syslog output.
type syslogHandler struct {
	writer *syslog.Writer
	level  slog.Level
	attrs  []slog.Attr
	group  string
}

func (h *syslogHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level
}

func (h *syslogHandler) Handle(_ context.Context, r slog.Record) error {
	// Build the message with attributes
	msg := r.Message
	r.Attrs(func(a slog.Attr) bool {
		msg += " " + a.Key + "=" + a.Value.String()
		return true
	})
	for _, a := range h.attrs {
		msg += " " + a.Key + "=" + a.Value.String()
	}

	// Write to appropriate syslog level
	switch {
	case r.Level >= slog.LevelError:
		return h.writer.Err(msg)
	case r.Level >= slog.LevelWarn:
		return h.writer.Warning(msg)
	case r.Level >= slog.LevelInfo:
		return h.writer.Info(msg)
	default:
		return h.writer.Debug(msg)
	}
}

func (h *syslogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	newAttrs := make([]slog.Attr, len(h.attrs)+len(attrs))
	copy(newAttrs, h.attrs)
	copy(newAttrs[len(h.attrs):], attrs)
	return &syslogHandler{
		writer: h.writer,
		level:  h.level,
		attrs:  newAttrs,
		group:  h.group,
	}
}

func (h *syslogHandler) WithGroup(name string) slog.Handler {
	return &syslogHandler{
		writer: h.writer,
		level:  h.level,
		attrs:  h.attrs,
		group:  name,
	}
}

// newFallbackLogger creates a stderr logger when syslog is unavailable.
func newFallbackLogger(level slog.Level) Logger {
	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: level,
	})
	return &logger{slog.New(handler)}
}
