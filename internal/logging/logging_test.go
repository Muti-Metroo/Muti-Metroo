package logging

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestNewLogger_TextFormat(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLoggerWithWriter("info", "text", &buf)

	logger.Info("test message", "key", "value")

	output := buf.String()
	if !strings.Contains(output, "test message") {
		t.Errorf("expected output to contain 'test message', got: %s", output)
	}
	if !strings.Contains(output, "key=value") {
		t.Errorf("expected output to contain 'key=value', got: %s", output)
	}
}

func TestNewLogger_JSONFormat(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLoggerWithWriter("info", "json", &buf)

	logger.Info("test message", "key", "value")

	output := buf.String()
	if !strings.Contains(output, `"msg":"test message"`) {
		t.Errorf("expected JSON output with msg field, got: %s", output)
	}
	if !strings.Contains(output, `"key":"value"`) {
		t.Errorf("expected JSON output with key field, got: %s", output)
	}
}

func TestNewLogger_LevelFiltering(t *testing.T) {
	tests := []struct {
		name         string
		configLevel  string
		logLevel     slog.Level
		shouldAppear bool
	}{
		{"debug at debug level", "debug", slog.LevelDebug, true},
		{"info at debug level", "debug", slog.LevelInfo, true},
		{"debug at info level", "info", slog.LevelDebug, false},
		{"info at info level", "info", slog.LevelInfo, true},
		{"warn at info level", "info", slog.LevelWarn, true},
		{"info at warn level", "warn", slog.LevelInfo, false},
		{"warn at warn level", "warn", slog.LevelWarn, true},
		{"error at warn level", "warn", slog.LevelError, true},
		{"warn at error level", "error", slog.LevelWarn, false},
		{"error at error level", "error", slog.LevelError, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			logger := NewLoggerWithWriter(tc.configLevel, "text", &buf)

			logger.Log(nil, tc.logLevel, "test message")

			hasOutput := buf.Len() > 0
			if hasOutput != tc.shouldAppear {
				t.Errorf("level %s at config %s: expected shouldAppear=%v, got output=%v",
					tc.logLevel, tc.configLevel, tc.shouldAppear, hasOutput)
			}
		})
	}
}

func TestParseLevel(t *testing.T) {
	tests := []struct {
		input    string
		expected slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"DEBUG", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"INFO", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"WARN", slog.LevelWarn},
		{"warning", slog.LevelWarn},
		{"error", slog.LevelError},
		{"ERROR", slog.LevelError},
		{"unknown", slog.LevelInfo}, // Default
		{"", slog.LevelInfo},        // Default
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			result := parseLevel(tc.input)
			if result != tc.expected {
				t.Errorf("parseLevel(%q) = %v, want %v", tc.input, result, tc.expected)
			}
		})
	}
}

func TestNopLogger(t *testing.T) {
	logger := NopLogger()
	if logger == nil {
		t.Fatal("NopLogger returned nil")
	}

	// Should not panic
	logger.Info("this should be discarded")
	logger.Error("this too")
}

func TestNewLogger_DefaultsToStderr(t *testing.T) {
	// Just verify it doesn't panic
	logger := NewLogger("info", "text")
	if logger == nil {
		t.Fatal("NewLogger returned nil")
	}
}

func TestLoggerWithAttributes(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLoggerWithWriter("info", "text", &buf)

	logger.Info("peer connected",
		KeyPeerID, "abc123",
		KeyAddress, "192.168.1.1:4433",
		KeyTransport, "quic",
	)

	output := buf.String()
	if !strings.Contains(output, "peer_id=abc123") {
		t.Errorf("expected peer_id attribute, got: %s", output)
	}
	if !strings.Contains(output, "address=192.168.1.1:4433") {
		t.Errorf("expected address attribute, got: %s", output)
	}
	if !strings.Contains(output, "transport=quic") {
		t.Errorf("expected transport attribute, got: %s", output)
	}
}
