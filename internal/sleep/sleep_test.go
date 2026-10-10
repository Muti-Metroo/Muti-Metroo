package sleep

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/postalsys/muti-metroo/internal/config"
	"github.com/postalsys/muti-metroo/internal/identity"
	"github.com/postalsys/muti-metroo/internal/logging"
	"github.com/postalsys/muti-metroo/internal/protocol"
)

func TestState_String(t *testing.T) {
	tests := []struct {
		state State
		want  string
	}{
		{StateAwake, "AWAKE"},
		{StateSleeping, "SLEEPING"},
		{StatePolling, "POLLING"},
		{State(99), "UNKNOWN"},
	}

	for _, tt := range tests {
		got := tt.state.String()
		if got != tt.want {
			t.Errorf("State(%d).String() = %q, want %q", tt.state, got, tt.want)
		}
	}
}

func TestNewManager(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := config.SleepConfig{
		Enabled:            true,
		PollInterval:       5 * time.Minute,
		PollIntervalJitter: 0.3,
		PollDuration:       30 * time.Second,
		PersistState:       false,
		MaxQueuedMessages:  1000,
	}
	logger := logging.NewLogger("error", "text")

	mgr := NewManager(cfg, tmpDir, logger)
	if mgr == nil {
		t.Fatal("NewManager returned nil")
	}

	if mgr.GetState() != StateAwake {
		t.Errorf("Initial state = %v, want AWAKE", mgr.GetState())
	}

	mgr.Stop()
}

func TestManager_SleepWakeCycle(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := config.SleepConfig{
		Enabled:            true,
		PollInterval:       100 * time.Millisecond,
		PollIntervalJitter: 0.1,
		PollDuration:       50 * time.Millisecond,
		PersistState:       false,
		MaxQueuedMessages:  100,
	}
	logger := logging.NewLogger("error", "text")

	mgr := NewManager(cfg, tmpDir, logger)
	defer mgr.Stop()

	var sleepCalled, wakeCalled bool
	mgr.SetCallbacks(Callbacks{
		OnSleep: func() error {
			sleepCalled = true
			return nil
		},
		OnWake: func() error {
			wakeCalled = true
			return nil
		},
	})

	// Test sleep
	if err := mgr.Sleep(); err != nil {
		t.Fatalf("Sleep() error = %v", err)
	}
	if !sleepCalled {
		t.Error("OnSleep callback not called")
	}
	if mgr.GetState() != StateSleeping {
		t.Errorf("State after Sleep() = %v, want SLEEPING", mgr.GetState())
	}

	// Test wake
	if err := mgr.Wake(); err != nil {
		t.Fatalf("Wake() error = %v", err)
	}
	if !wakeCalled {
		t.Error("OnWake callback not called")
	}
	if mgr.GetState() != StateAwake {
		t.Errorf("State after Wake() = %v, want AWAKE", mgr.GetState())
	}
}

func TestManager_SleepErrors(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := config.SleepConfig{
		Enabled:      true,
		PollInterval: 1 * time.Minute,
		PollDuration: 30 * time.Second,
	}
	logger := logging.NewLogger("error", "text")

	mgr := NewManager(cfg, tmpDir, logger)
	defer mgr.Stop()

	mgr.SetCallbacks(Callbacks{
		OnSleep: func() error { return nil },
		OnWake:  func() error { return nil },
	})

	// First sleep should succeed
	if err := mgr.Sleep(); err != nil {
		t.Fatalf("First Sleep() error = %v", err)
	}

	// Second sleep should fail (already sleeping)
	if err := mgr.Sleep(); err != ErrAlreadySleeping {
		t.Errorf("Second Sleep() error = %v, want ErrAlreadySleeping", err)
	}
}

func TestManager_WakeErrors(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := config.SleepConfig{
		Enabled:      true,
		PollInterval: 1 * time.Minute,
		PollDuration: 30 * time.Second,
	}
	logger := logging.NewLogger("error", "text")

	mgr := NewManager(cfg, tmpDir, logger)
	defer mgr.Stop()

	mgr.SetCallbacks(Callbacks{
		OnSleep: func() error { return nil },
		OnWake:  func() error { return nil },
	})

	// Wake while awake should fail
	if err := mgr.Wake(); err != ErrNotSleeping {
		t.Errorf("Wake() while awake error = %v, want ErrNotSleeping", err)
	}
}

func TestManager_HandleSleepCommand(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := config.SleepConfig{
		Enabled:      true,
		PollInterval: 1 * time.Minute,
		PollDuration: 30 * time.Second,
	}
	logger := logging.NewLogger("error", "text")

	mgr := NewManager(cfg, tmpDir, logger)
	defer mgr.Stop()

	originID, err := identity.NewAgentID()
	if err != nil {
		t.Fatalf("NewAgentID() error = %v", err)
	}
	cmd := &protocol.SleepCommand{
		OriginAgent: originID,
		CommandID:   1,
		Timestamp:   uint64(time.Now().Unix()),
		SeenBy:      []identity.AgentID{originID},
	}

	// First time should return true
	if !mgr.HandleSleepCommand(cmd) {
		t.Error("First HandleSleepCommand() = false, want true")
	}

	// Same command again should return false (deduplicated)
	if mgr.HandleSleepCommand(cmd) {
		t.Error("Second HandleSleepCommand() = true, want false (deduplicated)")
	}

	// Different command ID should return true
	cmd2 := &protocol.SleepCommand{
		OriginAgent: originID,
		CommandID:   2,
		Timestamp:   uint64(time.Now().Unix()),
		SeenBy:      []identity.AgentID{originID},
	}
	if !mgr.HandleSleepCommand(cmd2) {
		t.Error("HandleSleepCommand with different ID = false, want true")
	}
}

func TestManager_HandleWakeCommand(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := config.SleepConfig{
		Enabled:      true,
		PollInterval: 1 * time.Minute,
		PollDuration: 30 * time.Second,
	}
	logger := logging.NewLogger("error", "text")

	mgr := NewManager(cfg, tmpDir, logger)
	defer mgr.Stop()

	originID, err := identity.NewAgentID()
	if err != nil {
		t.Fatalf("NewAgentID() error = %v", err)
	}
	cmd := &protocol.WakeCommand{
		OriginAgent: originID,
		CommandID:   1,
		Timestamp:   uint64(time.Now().Unix()),
		SeenBy:      []identity.AgentID{originID},
	}

	// First time should return true
	if !mgr.HandleWakeCommand(cmd) {
		t.Error("First HandleWakeCommand() = false, want true")
	}

	// Same command again should return false (deduplicated)
	if mgr.HandleWakeCommand(cmd) {
		t.Error("Second HandleWakeCommand() = true, want false (deduplicated)")
	}
}

func TestManager_GetStatus(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := config.SleepConfig{
		Enabled:            true,
		PollInterval:       5 * time.Minute,
		PollIntervalJitter: 0.3,
		PollDuration:       30 * time.Second,
	}
	logger := logging.NewLogger("error", "text")

	mgr := NewManager(cfg, tmpDir, logger)
	defer mgr.Stop()

	status := mgr.GetStatus()
	if status.State != StateAwake {
		t.Errorf("Status.State = %v, want AWAKE", status.State)
	}
	if !status.Enabled {
		t.Error("Status.Enabled = false, want true")
	}
}

func TestManager_Persistence(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := config.SleepConfig{
		Enabled:           true,
		PollInterval:      5 * time.Minute,
		PollDuration:      30 * time.Second,
		PersistState:      true,
		MaxQueuedMessages: 100,
	}
	logger := logging.NewLogger("error", "text")

	// Create first manager and put it to sleep
	mgr1 := NewManager(cfg, tmpDir, logger)
	mgr1.SetCallbacks(Callbacks{
		OnSleep: func() error { return nil },
		OnWake:  func() error { return nil },
	})

	if err := mgr1.Sleep(); err != nil {
		t.Fatalf("Sleep() error = %v", err)
	}
	mgr1.Stop()

	// Verify state file exists
	stateFile := filepath.Join(tmpDir, "sleep_state.json")
	if _, err := os.Stat(stateFile); os.IsNotExist(err) {
		t.Error("State file not created")
	}

	// Create second manager and verify it loads the sleeping state
	mgr2 := NewManager(cfg, tmpDir, logger)
	if err := mgr2.LoadState(); err != nil {
		t.Fatalf("LoadState() error = %v", err)
	}
	if mgr2.GetState() != StateSleeping {
		t.Errorf("Loaded state = %v, want SLEEPING", mgr2.GetState())
	}
	mgr2.Stop()
}

func TestManager_NotEnabled(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := config.SleepConfig{
		Enabled: false,
	}
	logger := logging.NewLogger("error", "text")

	mgr := NewManager(cfg, tmpDir, logger)
	defer mgr.Stop()

	originID, err := identity.NewAgentID()
	if err != nil {
		t.Fatalf("NewAgentID() error = %v", err)
	}

	// Operations should fail when not enabled
	cmd := &protocol.SleepCommand{
		OriginAgent: originID,
		CommandID:   1,
		Timestamp:   uint64(time.Now().Unix()),
	}
	if mgr.HandleSleepCommand(cmd) {
		t.Error("HandleSleepCommand when disabled = true, want false")
	}
}

func TestJitteredInterval(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := config.SleepConfig{
		Enabled:            true,
		PollInterval:       5 * time.Minute,
		PollIntervalJitter: 0.3,
		PollDuration:       30 * time.Second,
	}
	logger := logging.NewLogger("error", "text")
	mgr := NewManager(cfg, tmpDir, logger)
	defer mgr.Stop()

	baseInterval := 5 * time.Minute
	jitter := 0.3 // 30%

	// Run multiple times to test randomness
	for i := 0; i < 100; i++ {
		result := mgr.jitteredInterval(baseInterval, jitter)
		minExpected := time.Duration(float64(baseInterval) * (1 - jitter))
		maxExpected := time.Duration(float64(baseInterval) * (1 + jitter))

		if result < minExpected || result > maxExpected {
			t.Errorf("Jittered interval %v out of range [%v, %v]", result, minExpected, maxExpected)
		}
	}
}

func TestIsSleeping(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := config.SleepConfig{
		Enabled:      true,
		PollInterval: 1 * time.Minute,
		PollDuration: 30 * time.Second,
	}
	logger := logging.NewLogger("error", "text")

	mgr := NewManager(cfg, tmpDir, logger)
	defer mgr.Stop()

	mgr.SetCallbacks(Callbacks{
		OnSleep: func() error { return nil },
		OnWake:  func() error { return nil },
	})

	if mgr.IsSleeping() {
		t.Error("IsSleeping() = true when awake, want false")
	}

	if err := mgr.Sleep(); err != nil {
		t.Fatalf("Sleep() error = %v", err)
	}

	if !mgr.IsSleeping() {
		t.Error("IsSleeping() = false when sleeping, want true")
	}
}

// ============================================================================
// Concurrency Race Condition Tests
// ============================================================================

// TestManager_ConcurrentSleepWake verifies that concurrent Sleep and Wake
// calls do not cause panics, data races, or callback overlap.
func TestManager_ConcurrentSleepWake(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := config.SleepConfig{
		Enabled:            true,
		PollInterval:       1 * time.Hour, // Long interval to avoid poll interference
		PollIntervalJitter: 0,
		PollDuration:       1 * time.Millisecond,
		PersistState:       false,
		MaxQueuedMessages:  100,
	}
	logger := logging.NewLogger("error", "text")

	mgr := NewManager(cfg, tmpDir, logger)
	defer mgr.Stop()

	// Track concurrent callback execution
	var callbackActive atomic.Bool
	var overlapDetected atomic.Bool

	checkOverlap := func() {
		if !callbackActive.CompareAndSwap(false, true) {
			overlapDetected.Store(true)
			return
		}
		time.Sleep(100 * time.Microsecond) // Brief hold to widen race window
		callbackActive.Store(false)
	}

	mgr.SetCallbacks(Callbacks{
		OnSleep: func() error {
			checkOverlap()
			return nil
		},
		OnWake: func() error {
			checkOverlap()
			return nil
		},
	})

	var wg sync.WaitGroup
	const goroutines = 20
	const iterations = 50

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				if id%2 == 0 {
					mgr.Sleep()
				} else {
					mgr.Wake()
				}
			}
		}(i)
	}

	wg.Wait()

	if overlapDetected.Load() {
		t.Error("detected concurrent callback execution (Sleep/Wake overlap)")
	}

	// Final state must be valid
	state := mgr.GetState()
	if state != StateAwake && state != StateSleeping {
		t.Errorf("final state = %v, want Awake or Sleeping", state)
	}
}

// TestManager_ConcurrentWakeWake verifies that when multiple goroutines
// call Wake() concurrently, OnWake is called exactly once.
func TestManager_ConcurrentWakeWake(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := config.SleepConfig{
		Enabled:            true,
		PollInterval:       1 * time.Hour,
		PollIntervalJitter: 0,
		PollDuration:       1 * time.Millisecond,
		PersistState:       false,
		MaxQueuedMessages:  100,
	}
	logger := logging.NewLogger("error", "text")

	mgr := NewManager(cfg, tmpDir, logger)
	defer mgr.Stop()

	var wakeCount atomic.Int64

	mgr.SetCallbacks(Callbacks{
		OnSleep: func() error { return nil },
		OnWake: func() error {
			wakeCount.Add(1)
			return nil
		},
	})

	// Put to sleep first
	if err := mgr.Sleep(); err != nil {
		t.Fatalf("Sleep() error = %v", err)
	}

	// Concurrent wake attempts
	var wg sync.WaitGroup
	const goroutines = 10

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			mgr.Wake()
		}()
	}

	wg.Wait()

	if count := wakeCount.Load(); count != 1 {
		t.Errorf("OnWake called %d times, want exactly 1", count)
	}

	if mgr.GetState() != StateAwake {
		t.Errorf("final state = %v, want Awake", mgr.GetState())
	}
}

// TestManager_ConcurrentSleepSleep verifies that when multiple goroutines
// call Sleep() concurrently, OnSleep is called exactly once.
func TestManager_ConcurrentSleepSleep(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := config.SleepConfig{
		Enabled:            true,
		PollInterval:       1 * time.Hour,
		PollIntervalJitter: 0,
		PollDuration:       1 * time.Millisecond,
		PersistState:       false,
		MaxQueuedMessages:  100,
	}
	logger := logging.NewLogger("error", "text")

	mgr := NewManager(cfg, tmpDir, logger)
	defer mgr.Stop()

	var sleepCount atomic.Int64
	var nilErrors atomic.Int64

	mgr.SetCallbacks(Callbacks{
		OnSleep: func() error {
			sleepCount.Add(1)
			return nil
		},
		OnWake: func() error { return nil },
	})

	// Concurrent sleep attempts from awake state
	var wg sync.WaitGroup
	const goroutines = 10

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := mgr.Sleep(); err == nil {
				nilErrors.Add(1)
			}
		}()
	}

	wg.Wait()

	if count := sleepCount.Load(); count != 1 {
		t.Errorf("OnSleep called %d times, want exactly 1", count)
	}

	if count := nilErrors.Load(); count != 1 {
		t.Errorf("%d goroutines returned nil error, want exactly 1", count)
	}

	if mgr.GetState() != StateSleeping {
		t.Errorf("final state = %v, want Sleeping", mgr.GetState())
	}
}
