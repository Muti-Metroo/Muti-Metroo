// Package logging provides structured logging for Muti Metroo.
package logging

import (
	"io"
	"log/slog"
	"os"
	"strings"
)

// NewLogger creates a new structured logger with the specified level and format.
// Supported levels: debug, info, warn, error
// Supported formats: text, json
func NewLogger(level, format string) *slog.Logger {
	return NewLoggerWithWriter(level, format, os.Stderr)
}

// NewLoggerWithWriter creates a new structured logger with a custom writer.
func NewLoggerWithWriter(level, format string, w io.Writer) *slog.Logger {
	lvl := parseLevel(level)

	opts := &slog.HandlerOptions{
		Level: lvl,
	}

	var handler slog.Handler
	switch strings.ToLower(format) {
	case "json":
		handler = slog.NewJSONHandler(w, opts)
	default:
		handler = slog.NewTextHandler(w, opts)
	}

	return slog.New(handler)
}

// parseLevel converts a string log level to slog.Level.
func parseLevel(level string) slog.Level {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// NopLogger returns a logger that discards all output.
func NopLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// Common attribute keys for consistent logging.
const (
	KeyPeerID     = "peer_id"
	KeyStreamID   = "stream_id"
	KeyRequestID  = "request_id"
	KeyAddress    = "address"
	KeyTransport  = "transport"
	KeyRoute      = "route"
	KeyHops       = "hops"
	KeyError      = "error"
	KeyComponent  = "component"
	KeyAgentID    = "agent_id"
	KeyRemoteAddr = "remote_addr"
	KeyLocalAddr  = "local_addr"
	KeyDuration   = "duration"
	KeyCount      = "count"
)
