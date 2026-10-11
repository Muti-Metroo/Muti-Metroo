package chaos

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestFaultInjector_Basic(t *testing.T) {
	injector := NewFaultInjector(FaultConfig{
		Type:        FaultDisconnect,
		Probability: 1.0, // Always inject
	})

	// Should inject
	if !injector.MaybeDisconnect() {
		t.Error("expected disconnect fault to be injected")
	}

	// Check stats
	stats := injector.GetStats()
	if stats[FaultDisconnect] < 1 {
		t.Error("expected at least 1 disconnect fault hit")
	}
}

func TestFaultInjector_Disabled(t *testing.T) {
	injector := NewFaultInjector(FaultConfig{
		Type:        FaultDisconnect,
		Probability: 1.0,
	})

	injector.Disable()

	if injector.MaybeDisconnect() {
		t.Error("expected no fault when disabled")
	}
}

func TestFaultInjector_Probability(t *testing.T) {
	// 0% probability - should never inject
	injector := NewFaultInjector(FaultConfig{
		Type:        FaultDisconnect,
		Probability: 0.0,
	})

	for i := 0; i < 100; i++ {
		if injector.MaybeDisconnect() {
			t.Error("expected no fault with 0% probability")
		}
	}
}

func TestFaultInjector_Delay(t *testing.T) {
	injector := NewFaultInjector(FaultConfig{
		Type:        FaultDelay,
		Probability: 1.0,
		MinDelay:    10 * time.Millisecond,
		MaxDelay:    20 * time.Millisecond,
	})

	delay := injector.MaybeDelay()
	if delay < 10*time.Millisecond || delay > 20*time.Millisecond {
		t.Errorf("delay %v outside expected range [10ms, 20ms]", delay)
	}
}

func TestFaultInjector_Panic(t *testing.T) {
	injector := NewFaultInjector(FaultConfig{
		Type:        FaultPanic,
		Probability: 1.0,
	})

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic to be triggered")
		}
	}()

	injector.MaybePanic()
}

func TestFaultInjector_Error(t *testing.T) {
	injector := NewFaultInjector(FaultConfig{
		Type:        FaultError,
		Probability: 1.0,
	})

	if !injector.MaybeError() {
		t.Error("expected error fault to be injected")
	}
}

func TestFaultInjector_Reset(t *testing.T) {
	injector := NewFaultInjector(FaultConfig{
		Type:        FaultDisconnect,
		Probability: 1.0,
	})

	injector.MaybeDisconnect()
	injector.Reset()

	stats := injector.GetStats()
	if stats[FaultDisconnect] != 0 {
		t.Errorf("expected 0 hits after reset, got %d", stats[FaultDisconnect])
	}
}

// mockTarget implements Target for testing.
type mockTarget struct {
	id      string
	alive   bool
	mu      sync.Mutex
	killErr error
}

func (m *mockTarget) ID() string {
	return m.id
}

func (m *mockTarget) Kill() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.killErr != nil {
		return m.killErr
	}
	m.alive = false
	return nil
}

func (m *mockTarget) IsAlive() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.alive
}

func (m *mockTarget) Restart() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.alive = true
	return nil
}

func TestChaosMonkey_StartStop(t *testing.T) {
	injector := NewFaultInjector(FaultConfig{
		Type:        FaultDisconnect,
		Probability: 0.0, // Never inject for this test
	})

	monkey := NewChaosMonkey(10*time.Millisecond, injector)
	target := &mockTarget{id: "test-1", alive: true}
	monkey.AddTarget(target)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	monkey.Start(ctx)
	time.Sleep(50 * time.Millisecond)
	monkey.Stop()
}

func TestChaosMonkey_KillsTarget(t *testing.T) {
	injector := NewFaultInjector(FaultConfig{
		Type:        FaultDisconnect,
		Probability: 1.0, // Always inject
	})

	monkey := NewChaosMonkey(10*time.Millisecond, injector)
	target := &mockTarget{id: "test-1", alive: true}
	monkey.AddTarget(target)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	monkey.Start(ctx)

	// Wait for chaos to happen
	time.Sleep(50 * time.Millisecond)

	monkey.Stop()

	// Check that target was killed at some point
	events := make([]Event, 0)
	for {
		select {
		case e := <-monkey.Events():
			events = append(events, e)
		default:
			goto done
		}
	}
done:

	foundKill := false
	for _, e := range events {
		if e.Action == "kill" {
			foundKill = true
			break
		}
	}

	if !foundKill {
		t.Error("expected at least one kill event")
	}
}

func TestChaosMonkey_RestartsTarget(t *testing.T) {
	injector := NewFaultInjector(FaultConfig{
		Type:        FaultDisconnect,
		Probability: 0.5,
	})

	monkey := NewChaosMonkey(10*time.Millisecond, injector)
	target := &mockTarget{id: "test-1", alive: false} // Start dead
	monkey.AddTarget(target)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	monkey.Start(ctx)
	time.Sleep(50 * time.Millisecond)
	monkey.Stop()

	// Check that target was restarted
	events := make([]Event, 0)
	for {
		select {
		case e := <-monkey.Events():
			events = append(events, e)
		default:
			goto done
		}
	}
done:

	foundRestart := false
	for _, e := range events {
		if e.Action == "restart" {
			foundRestart = true
			break
		}
	}

	if !foundRestart {
		t.Error("expected at least one restart event")
	}
}

func TestChaosMonkey_RemoveTarget(t *testing.T) {
	injector := NewFaultInjector()
	monkey := NewChaosMonkey(10*time.Millisecond, injector)

	target1 := &mockTarget{id: "test-1", alive: true}
	target2 := &mockTarget{id: "test-2", alive: true}

	monkey.AddTarget(target1)
	monkey.AddTarget(target2)
	monkey.RemoveTarget("test-1")

	monkey.mu.RLock()
	count := len(monkey.targets)
	monkey.mu.RUnlock()

	if count != 1 {
		t.Errorf("expected 1 target after removal, got %d", count)
	}
}

func TestScenarioRunner_Basic(t *testing.T) {
	runner := NewScenarioRunner()

	setupCalled := int32(0)
	teardownCalled := int32(0)
	checkCalled := int32(0)

	runner.AddScenario(Scenario{
		Name:        "test-scenario",
		Description: "A test scenario",
		Duration:    50 * time.Millisecond,
		Setup: func() error {
			atomic.AddInt32(&setupCalled, 1)
			return nil
		},
		Teardown: func() error {
			atomic.AddInt32(&teardownCalled, 1)
			return nil
		},
		Check: func() error {
			atomic.AddInt32(&checkCalled, 1)
			return nil
		},
	})

	ctx := context.Background()
	results := runner.Run(ctx)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	result := results[0]
	if !result.Success {
		t.Errorf("expected success, got failure: setup=%v, error=%v, check=%v",
			result.SetupError, result.Error, result.CheckError)
	}

	if atomic.LoadInt32(&setupCalled) != 1 {
		t.Error("expected setup to be called once")
	}
	if atomic.LoadInt32(&teardownCalled) != 1 {
		t.Error("expected teardown to be called once")
	}
	if atomic.LoadInt32(&checkCalled) != 1 {
		t.Error("expected check to be called once")
	}
}

func TestScenarioRunner_SetupError(t *testing.T) {
	runner := NewScenarioRunner()

	runner.AddScenario(Scenario{
		Name:     "failing-setup",
		Duration: 50 * time.Millisecond,
		Setup: func() error {
			return errors.New("setup failed")
		},
		Check: func() error {
			return nil
		},
	})

	ctx := context.Background()
	results := runner.Run(ctx)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	result := results[0]
	if result.Success {
		t.Error("expected failure due to setup error")
	}
	if result.SetupError == nil {
		t.Error("expected setup error to be set")
	}
}

func TestScenarioRunner_CheckError(t *testing.T) {
	runner := NewScenarioRunner()

	runner.AddScenario(Scenario{
		Name:     "failing-check",
		Duration: 10 * time.Millisecond,
		Check: func() error {
			return errors.New("check failed")
		},
	})

	ctx := context.Background()
	results := runner.Run(ctx)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	result := results[0]
	if result.Success {
		t.Error("expected failure due to check error")
	}
	if result.CheckError == nil {
		t.Error("expected check error to be set")
	}
}

func TestScenarioRunner_ContextCancellation(t *testing.T) {
	runner := NewScenarioRunner()

	runner.AddScenario(Scenario{
		Name:     "long-running",
		Duration: 10 * time.Second, // Long duration
	})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	results := runner.Run(ctx)
	elapsed := time.Since(start)

	if elapsed > 1*time.Second {
		t.Errorf("expected fast cancellation, took %v", elapsed)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	result := results[0]
	if result.Success {
		t.Error("expected failure due to context cancellation")
	}
}

func TestScenarioRunner_MultipleScenarios(t *testing.T) {
	runner := NewScenarioRunner()

	for i := 0; i < 3; i++ {
		runner.AddScenario(Scenario{
			Name:     "scenario",
			Duration: 10 * time.Millisecond,
		})
	}

	ctx := context.Background()
	results := runner.Run(ctx)

	if len(results) != 3 {
		t.Errorf("expected 3 results, got %d", len(results))
	}

	for _, r := range results {
		if !r.Success {
			t.Errorf("expected all scenarios to succeed, %s failed", r.Scenario)
		}
	}
}
