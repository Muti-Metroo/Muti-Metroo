package icmp

import (
	"time"
)

// Config holds configuration for the ICMP echo handler.
type Config struct {
	// Enabled controls whether ICMP echo is active.
	// When false, ICMP_OPEN requests are rejected with ErrICMPDisabled.
	Enabled bool

	// MaxSessions limits concurrent ICMP sessions.
	// 0 means unlimited.
	MaxSessions int

	// IdleTimeout is how long a session can be idle before cleanup.
	// 0 means no timeout.
	IdleTimeout time.Duration

	// EchoTimeout is the timeout for each individual ICMP echo.
	// Default is 5 seconds.
	EchoTimeout time.Duration

	// MaxConcurrentReplies limits concurrent reply-waiting goroutines.
	// 0 means unlimited (default).
	MaxConcurrentReplies int
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		Enabled:     true,
		MaxSessions: 100,
		IdleTimeout: 60 * time.Second,
		EchoTimeout: 5 * time.Second,
	}
}
