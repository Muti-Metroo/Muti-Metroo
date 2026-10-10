// Package chaos provides chaos testing utilities for fault injection.
package chaos

import (
	"context"
	"math/rand"
	"sync"
	"time"
)

// FaultType represents the type of fault to inject.
type FaultType int

const (
	// FaultDisconnect causes a connection to be dropped.
	FaultDisconnect FaultType = iota
	// FaultDelay adds latency to operations.
	FaultDelay
	// FaultPanic causes a panic in a goroutine.
	FaultPanic
	// FaultError causes an operation to return an error.
	FaultError
)

// FaultConfig configures fault injection behavior.
type FaultConfig struct {
	// Probability is the chance of fault injection (0.0 to 1.0).
	Probability float64

	// Type is the type of fault to inject.
	Type FaultType

	// MinDelay is the minimum delay to add for FaultDelay.
	MinDelay time.Duration

	// MaxDelay is the maximum delay to add for FaultDelay.
	MaxDelay time.Duration
}

// FaultInjector injects faults into a system.
type FaultInjector struct {
	configs   []FaultConfig
	enabled   bool
	mu        sync.RWMutex
	rng       *rand.Rand
	faultHits map[FaultType]int64
}

// NewFaultInjector creates a new fault injector.
func NewFaultInjector(configs ...FaultConfig) *FaultInjector {
	return &FaultInjector{
		configs:   configs,
		enabled:   true,
		rng:       rand.New(rand.NewSource(time.Now().UnixNano())),
		faultHits: make(map[FaultType]int64),
	}
}

// Enable enables fault injection.
func (f *FaultInjector) Enable() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.enabled = true
}

// Disable disables fault injection.
func (f *FaultInjector) Disable() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.enabled = false
}

// IsEnabled returns whether fault injection is enabled.
func (f *FaultInjector) IsEnabled() bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.enabled
}

// MaybeInject checks if a fault should be injected and returns the fault type.
// Returns -1 if no fault should be injected.
func (f *FaultInjector) MaybeInject() FaultType {
	f.mu.RLock()
	if !f.enabled {
		f.mu.RUnlock()
		return -1
	}
	configs := f.configs
	f.mu.RUnlock()

	for _, config := range configs {
		if f.shouldInject(config.Probability) {
			f.mu.Lock()
			f.faultHits[config.Type]++
			f.mu.Unlock()
			return config.Type
		}
	}

	return -1
}

// MaybeDisconnect returns true if a disconnect fault should be injected.
func (f *FaultInjector) MaybeDisconnect() bool {
	return f.MaybeInject() == FaultDisconnect
}

// MaybeDelay returns a delay duration if a delay fault should be injected.
func (f *FaultInjector) MaybeDelay() time.Duration {
	f.mu.RLock()
	if !f.enabled {
		f.mu.RUnlock()
		return 0
	}
	configs := f.configs
	f.mu.RUnlock()

	for _, config := range configs {
		if config.Type == FaultDelay && f.shouldInject(config.Probability) {
			f.mu.Lock()
			f.faultHits[FaultDelay]++
			f.mu.Unlock()
			return f.randomDelay(config.MinDelay, config.MaxDelay)
		}
	}

	return 0
}

// MaybePanic panics if a panic fault should be injected.
func (f *FaultInjector) MaybePanic() {
	if f.MaybeInject() == FaultPanic {
		panic("chaos: injected panic")
	}
}

// MaybeError returns true if an error fault should be injected.
func (f *FaultInjector) MaybeError() bool {
	return f.MaybeInject() == FaultError
}

// GetStats returns the fault injection statistics.
func (f *FaultInjector) GetStats() map[FaultType]int64 {
	f.mu.RLock()
	defer f.mu.RUnlock()
	stats := make(map[FaultType]int64)
	for k, v := range f.faultHits {
		stats[k] = v
	}
	return stats
}

// Reset resets the fault injection statistics.
func (f *FaultInjector) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.faultHits = make(map[FaultType]int64)
}

func (f *FaultInjector) shouldInject(probability float64) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.rng.Float64() < probability
}

func (f *FaultInjector) randomDelay(min, max time.Duration) time.Duration {
	if max <= min {
		return min
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	delta := max - min
	return min + time.Duration(f.rng.Int63n(int64(delta)))
}

// ChaosMonkey orchestrates chaos testing scenarios.
type ChaosMonkey struct {
	targets   []Target
	mu        sync.RWMutex
	running   bool
	stopCh    chan struct{}
	wg        sync.WaitGroup
	interval  time.Duration
	injector  *FaultInjector
	eventChan chan Event
}

// Target represents something that can be subjected to chaos.
type Target interface {
	// ID returns a unique identifier for the target.
	ID() string
	// Kill forcefully terminates the target.
	Kill() error
	// IsAlive returns whether the target is still alive.
	IsAlive() bool
	// Restart restarts the target after being killed.
	Restart() error
}

// Event represents a chaos event.
type Event struct {
	Time     time.Time
	TargetID string
	Action   string
	Success  bool
	Error    error
}

// NewChaosMonkey creates a new chaos monkey.
func NewChaosMonkey(interval time.Duration, injector *FaultInjector) *ChaosMonkey {
	return &ChaosMonkey{
		interval:  interval,
		injector:  injector,
		eventChan: make(chan Event, 100),
	}
}

// AddTarget adds a target to the chaos monkey.
func (c *ChaosMonkey) AddTarget(target Target) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.targets = append(c.targets, target)
}

// RemoveTarget removes a target from the chaos monkey.
func (c *ChaosMonkey) RemoveTarget(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, t := range c.targets {
		if t.ID() == id {
			c.targets = append(c.targets[:i], c.targets[i+1:]...)
			return
		}
	}
}

// Start starts the chaos monkey.
func (c *ChaosMonkey) Start(ctx context.Context) {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return
	}
	c.running = true
	c.stopCh = make(chan struct{})
	c.mu.Unlock()

	c.wg.Add(1)
	go c.run(ctx)
}

// Stop stops the chaos monkey.
func (c *ChaosMonkey) Stop() {
	c.mu.Lock()
	if !c.running {
		c.mu.Unlock()
		return
	}
	close(c.stopCh)
	c.running = false
	c.mu.Unlock()

	c.wg.Wait()
}

// Events returns a channel that receives chaos events.
func (c *ChaosMonkey) Events() <-chan Event {
	return c.eventChan
}

func (c *ChaosMonkey) run(ctx context.Context) {
	defer c.wg.Done()

	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.stopCh:
			return
		case <-ticker.C:
			c.maybeInjectChaos()
		}
	}
}

func (c *ChaosMonkey) maybeInjectChaos() {
	c.mu.RLock()
	targets := make([]Target, len(c.targets))
	copy(targets, c.targets)
	c.mu.RUnlock()

	if len(targets) == 0 {
		return
	}

	// Check if we should inject a fault
	if c.injector != nil && !c.injector.IsEnabled() {
		return
	}

	// Pick a random target
	idx := rand.Intn(len(targets))
	target := targets[idx]

	if !target.IsAlive() {
		// Try to restart dead targets
		event := Event{
			Time:     time.Now(),
			TargetID: target.ID(),
			Action:   "restart",
		}
		err := target.Restart()
		event.Success = err == nil
		event.Error = err
		c.sendEvent(event)
		return
	}

	// Maybe kill the target
	if c.injector.MaybeDisconnect() {
		event := Event{
			Time:     time.Now(),
			TargetID: target.ID(),
			Action:   "kill",
		}
		err := target.Kill()
		event.Success = err == nil
		event.Error = err
		c.sendEvent(event)
	}
}

func (c *ChaosMonkey) sendEvent(event Event) {
	select {
	case c.eventChan <- event:
	default:
		// Drop event if channel is full
	}
}

// Scenario represents a chaos testing scenario.
type Scenario struct {
	Name        string
	Description string
	Duration    time.Duration
	Setup       func() error
	Teardown    func() error
	Check       func() error
}

// ScenarioRunner runs chaos testing scenarios.
type ScenarioRunner struct {
	scenarios []Scenario
	results   []ScenarioResult
	mu        sync.Mutex
}

// ScenarioResult contains the result of running a scenario.
type ScenarioResult struct {
	Scenario   string
	Success    bool
	Duration   time.Duration
	Error      error
	SetupError error
	CheckError error
}

// NewScenarioRunner creates a new scenario runner.
func NewScenarioRunner() *ScenarioRunner {
	return &ScenarioRunner{}
}

// AddScenario adds a scenario to the runner.
func (r *ScenarioRunner) AddScenario(scenario Scenario) {
	r.scenarios = append(r.scenarios, scenario)
}

// Run executes all scenarios.
func (r *ScenarioRunner) Run(ctx context.Context) []ScenarioResult {
	for _, scenario := range r.scenarios {
		result := r.runScenario(ctx, scenario)
		r.mu.Lock()
		r.results = append(r.results, result)
		r.mu.Unlock()
	}
	return r.results
}

func (r *ScenarioRunner) runScenario(ctx context.Context, scenario Scenario) ScenarioResult {
	result := ScenarioResult{
		Scenario: scenario.Name,
	}

	startTime := time.Now()

	// Setup
	if scenario.Setup != nil {
		if err := scenario.Setup(); err != nil {
			result.SetupError = err
			result.Duration = time.Since(startTime)
			return result
		}
	}

	// Teardown at the end
	if scenario.Teardown != nil {
		defer scenario.Teardown()
	}

	// Run for duration
	select {
	case <-ctx.Done():
		result.Error = ctx.Err()
	case <-time.After(scenario.Duration):
	}

	// Check
	if scenario.Check != nil {
		if err := scenario.Check(); err != nil {
			result.CheckError = err
		}
	}

	result.Duration = time.Since(startTime)
	result.Success = result.SetupError == nil && result.Error == nil && result.CheckError == nil

	return result
}
