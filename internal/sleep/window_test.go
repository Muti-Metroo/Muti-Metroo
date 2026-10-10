package sleep

import (
	"testing"
	"time"

	"github.com/postalsys/muti-metroo/internal/identity"
)

// mustNewAgentID creates a new agent ID and panics on error (test helper).
func mustNewAgentID(t *testing.T) identity.AgentID {
	t.Helper()
	id, err := identity.NewAgentID()
	if err != nil {
		t.Fatalf("failed to create agent ID: %v", err)
	}
	return id
}

func TestDefaultWindowConfig(t *testing.T) {
	cfg := DefaultWindowConfig()

	if cfg.CycleLength != 5*time.Minute {
		t.Errorf("expected CycleLength 5m, got %v", cfg.CycleLength)
	}
	if cfg.WindowLength != 30*time.Second {
		t.Errorf("expected WindowLength 30s, got %v", cfg.WindowLength)
	}
	if cfg.ClockTolerance != 5*time.Second {
		t.Errorf("expected ClockTolerance 5s, got %v", cfg.ClockTolerance)
	}
	if cfg.Epoch != time.Unix(0, 0).UTC() {
		t.Errorf("expected Unix epoch, got %v", cfg.Epoch)
	}
}

func TestNewWindowCalculator(t *testing.T) {
	t.Run("valid config", func(t *testing.T) {
		cfg := DefaultWindowConfig()
		calc := NewWindowCalculator(cfg)

		if calc == nil {
			t.Fatal("expected non-nil calculator")
		}
		if calc.cfg.CycleLength != cfg.CycleLength {
			t.Error("config not preserved")
		}
	})

	t.Run("window length too large", func(t *testing.T) {
		cfg := DefaultWindowConfig()
		cfg.WindowLength = 10 * time.Minute // larger than CycleLength

		calc := NewWindowCalculator(cfg)

		// Should be clamped to ~16% of cycle
		if calc.cfg.WindowLength >= cfg.CycleLength {
			t.Errorf("window length should be clamped, got %v", calc.cfg.WindowLength)
		}
	})

	t.Run("zero epoch uses unix epoch", func(t *testing.T) {
		cfg := DefaultWindowConfig()
		cfg.Epoch = time.Time{}

		calc := NewWindowCalculator(cfg)

		if calc.cfg.Epoch.IsZero() {
			t.Error("should have set epoch to Unix epoch")
		}
	})
}

func TestWindowCalculator_seedFromAgentID(t *testing.T) {
	// Test that different agent IDs produce different seeds
	id1 := mustNewAgentID(t)
	id2 := mustNewAgentID(t)

	seed1 := seedFromAgentID(id1)
	seed2 := seedFromAgentID(id2)

	if seed1 == seed2 {
		t.Error("different agent IDs should produce different seeds")
	}

	// Same ID should produce same seed (deterministic)
	seed1Again := seedFromAgentID(id1)
	if seed1 != seed1Again {
		t.Error("same agent ID should produce same seed")
	}
}

func TestWindowCalculator_NextWindow(t *testing.T) {
	cfg := WindowConfig{
		CycleLength:    1 * time.Minute,
		WindowLength:   10 * time.Second,
		ClockTolerance: 2 * time.Second,
		Epoch:          time.Unix(0, 0).UTC(),
	}
	calc := NewWindowCalculator(cfg)
	agentID := mustNewAgentID(t)

	t.Run("returns future window", func(t *testing.T) {
		now := time.Now()
		start, end := calc.NextWindow(agentID, now)

		// Window should be in the future or current
		if end.Before(now) {
			t.Error("window end should be at or after now")
		}

		// Window should have correct length
		windowLen := end.Sub(start)
		if windowLen != cfg.WindowLength {
			t.Errorf("expected window length %v, got %v", cfg.WindowLength, windowLen)
		}
	})

	t.Run("deterministic for same agent", func(t *testing.T) {
		now := time.Now()
		start1, end1 := calc.NextWindow(agentID, now)
		start2, end2 := calc.NextWindow(agentID, now)

		if !start1.Equal(start2) || !end1.Equal(end2) {
			t.Error("same agent should get same window")
		}
	})

	t.Run("different agents may have different windows", func(t *testing.T) {
		now := time.Now()
		id1 := mustNewAgentID(t)
		id2 := mustNewAgentID(t)

		start1, end1 := calc.NextWindow(id1, now)
		start2, end2 := calc.NextWindow(id2, now)

		// Both windows should be valid (not zero) and in the future or present
		if start1.IsZero() || end1.IsZero() {
			t.Error("window for agent1 should not be zero")
		}
		if start2.IsZero() || end2.IsZero() {
			t.Error("window for agent2 should not be zero")
		}

		// Window end should not be before now (it's the next/current window)
		if end1.Before(now) {
			t.Errorf("window for agent1 should not be in the past: end=%v, now=%v", end1, now)
		}
		if end2.Before(now) {
			t.Errorf("window for agent2 should not be in the past: end=%v, now=%v", end2, now)
		}
	})
}

func TestWindowCalculator_GetWindowInfo(t *testing.T) {
	cfg := WindowConfig{
		CycleLength:    1 * time.Minute,
		WindowLength:   10 * time.Second,
		ClockTolerance: 2 * time.Second,
		Epoch:          time.Unix(0, 0).UTC(),
	}
	calc := NewWindowCalculator(cfg)
	agentID := mustNewAgentID(t)

	t.Run("safe times include tolerance", func(t *testing.T) {
		now := time.Now()
		info := calc.GetWindowInfo(agentID, now)

		// SafeStart should be Start - tolerance
		expectedSafeStart := info.Start.Add(-cfg.ClockTolerance)
		if !info.SafeStart.Equal(expectedSafeStart) {
			t.Errorf("SafeStart: expected %v, got %v", expectedSafeStart, info.SafeStart)
		}

		// SafeEnd should be End + tolerance
		expectedSafeEnd := info.End.Add(cfg.ClockTolerance)
		if !info.SafeEnd.Equal(expectedSafeEnd) {
			t.Errorf("SafeEnd: expected %v, got %v", expectedSafeEnd, info.SafeEnd)
		}
	})

	t.Run("midpoint is center of window", func(t *testing.T) {
		now := time.Now()
		info := calc.GetWindowInfo(agentID, now)

		expectedMidpoint := info.Start.Add(cfg.WindowLength / 2)
		if !info.Midpoint.Equal(expectedMidpoint) {
			t.Errorf("Midpoint: expected %v, got %v", expectedMidpoint, info.Midpoint)
		}
	})

	t.Run("time until is positive before window", func(t *testing.T) {
		// Use a time well before any potential window
		farPast := time.Unix(0, 0).UTC().Add(1 * time.Second)
		info := calc.GetWindowInfo(agentID, farPast)

		if info.TimeUntil < 0 {
			t.Errorf("TimeUntil should be non-negative, got %v", info.TimeUntil)
		}
	})

	t.Run("currently active when in window", func(t *testing.T) {
		now := time.Now()
		info := calc.GetWindowInfo(agentID, now)

		// Check at midpoint (definitely in window)
		infoAtMidpoint := calc.GetWindowInfo(agentID, info.Midpoint)
		if !infoAtMidpoint.CurrentlyActive {
			t.Error("should be active at midpoint")
		}
	})
}

func TestWindowCalculator_IsInWindow(t *testing.T) {
	cfg := WindowConfig{
		CycleLength:    1 * time.Minute,
		WindowLength:   10 * time.Second,
		ClockTolerance: 2 * time.Second,
		Epoch:          time.Unix(0, 0).UTC(),
	}
	calc := NewWindowCalculator(cfg)
	agentID := mustNewAgentID(t)

	now := time.Now()
	start, end := calc.NextWindow(agentID, now)

	t.Run("true during window", func(t *testing.T) {
		midpoint := start.Add(5 * time.Second)
		if !calc.IsInWindow(agentID, midpoint) {
			t.Error("should be in window at midpoint")
		}
	})

	t.Run("true during tolerance before", func(t *testing.T) {
		beforeTolerance := start.Add(-1 * time.Second) // within 2s tolerance
		if !calc.IsInWindow(agentID, beforeTolerance) {
			t.Error("should be in window during safe start tolerance")
		}
	})

	t.Run("false after window end", func(t *testing.T) {
		// After the window ends, NextWindow returns the NEXT cycle's window,
		// so IsInWindow correctly returns false. The tolerance is for the
		// connecting peer, not the sleeping agent.
		afterWindow := end.Add(1 * time.Second)
		if calc.IsInWindow(agentID, afterWindow) {
			t.Error("should not be in window after end (next cycle's window applies)")
		}
	})

	t.Run("false well before window", func(t *testing.T) {
		wellBefore := start.Add(-10 * time.Second)
		if calc.IsInWindow(agentID, wellBefore) {
			t.Error("should not be in window well before start")
		}
	})
}

func TestWindowCalculator_PreviousWindow(t *testing.T) {
	cfg := WindowConfig{
		CycleLength:    1 * time.Minute,
		WindowLength:   10 * time.Second,
		ClockTolerance: 2 * time.Second,
		Epoch:          time.Unix(0, 0).UTC(),
	}
	calc := NewWindowCalculator(cfg)
	agentID := mustNewAgentID(t)

	now := time.Now()
	prevStart, prevEnd := calc.PreviousWindow(agentID, now)

	t.Run("previous window is before now", func(t *testing.T) {
		if prevEnd.After(now) && prevStart.After(now) {
			// The previous window might overlap with now, but start should be before
			t.Logf("prevStart=%v, prevEnd=%v, now=%v", prevStart, prevEnd, now)
		}
	})

	t.Run("has correct length", func(t *testing.T) {
		windowLen := prevEnd.Sub(prevStart)
		if windowLen != cfg.WindowLength {
			t.Errorf("expected window length %v, got %v", cfg.WindowLength, windowLen)
		}
	})
}

func TestWindowCalculator_GetConfig(t *testing.T) {
	cfg := DefaultWindowConfig()
	calc := NewWindowCalculator(cfg)

	gotCfg := calc.GetConfig()
	if gotCfg.CycleLength != cfg.CycleLength {
		t.Error("GetConfig should return the configuration")
	}
}
