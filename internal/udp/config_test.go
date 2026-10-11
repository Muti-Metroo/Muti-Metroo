package udp

import (
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Enabled {
		t.Error("Enabled should be false by default")
	}
	if cfg.MaxAssociations != 1000 {
		t.Errorf("MaxAssociations = %d, want 1000", cfg.MaxAssociations)
	}
	if cfg.IdleTimeout != 5*time.Minute {
		t.Errorf("IdleTimeout = %v, want 5m", cfg.IdleTimeout)
	}
	if cfg.MaxDatagramSize != 1472 {
		t.Errorf("MaxDatagramSize = %d, want 1472", cfg.MaxDatagramSize)
	}
}
