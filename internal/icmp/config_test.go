package icmp

import (
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if !cfg.Enabled {
		t.Error("Enabled should be true by default")
	}
	if cfg.MaxSessions != 100 {
		t.Errorf("MaxSessions = %d, want 100", cfg.MaxSessions)
	}
	if cfg.IdleTimeout.Seconds() != 60 {
		t.Errorf("IdleTimeout = %v, want 60s", cfg.IdleTimeout)
	}
	if cfg.EchoTimeout.Seconds() != 5 {
		t.Errorf("EchoTimeout = %v, want 5s", cfg.EchoTimeout)
	}
}
