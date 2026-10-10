package udp

import (
	"time"
)

// Config holds configuration for the UDP relay handler.
type Config struct {
	// Enabled controls whether UDP relay is active.
	// When false, UDP_OPEN requests are rejected with ErrUDPDisabled.
	Enabled bool

	// MaxAssociations limits concurrent UDP associations.
	// 0 means unlimited.
	MaxAssociations int

	// IdleTimeout is how long an association can be idle before cleanup.
	// 0 means no timeout.
	IdleTimeout time.Duration

	// MaxDatagramSize is the maximum UDP payload size.
	// Default is 1472 (typical MTU - IP/UDP headers).
	MaxDatagramSize int
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		Enabled:         false,
		MaxAssociations: 1000,
		IdleTimeout:     5 * time.Minute,
		MaxDatagramSize: 1472,
	}
}
