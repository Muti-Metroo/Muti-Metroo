// Package peer manages peer connections and handshakes for Muti Metroo.
package peer

import (
	"math"
	"sync"
	"time"
)

// ReconnectConfig contains configuration for reconnection behavior.
type ReconnectConfig struct {
	InitialDelay time.Duration
	MaxDelay     time.Duration
	Multiplier   float64
	MaxAttempts  int // 0 means unlimited
	Jitter       float64
}

// DefaultReconnectConfig returns sensible defaults for reconnection.
func DefaultReconnectConfig() ReconnectConfig {
	return ReconnectConfig{
		InitialDelay: 1 * time.Second,
		MaxDelay:     60 * time.Second,
		Multiplier:   2.0,
		MaxAttempts:  0,   // Unlimited
		Jitter:       0.2, // 20% jitter makes timing patterns less distinguishable
	}
}

// reconnectState tracks the state of reconnection attempts for a peer.
type reconnectState struct {
	attempts    int
	nextDelay   time.Duration
	lastAttempt time.Time
	timer       *time.Timer
}

// Reconnector handles automatic reconnection with exponential backoff.
type Reconnector struct {
	cfg      ReconnectConfig
	callback func(addr string) error

	mu     sync.Mutex
	states map[string]*reconnectState
	closed bool
	paused bool
}

// NewReconnector creates a new reconnector.
func NewReconnector(cfg ReconnectConfig, callback func(addr string) error) *Reconnector {
	return &Reconnector{
		cfg:      cfg,
		callback: callback,
		states:   make(map[string]*reconnectState),
	}
}

// Schedule schedules a reconnection attempt for the given address.
func (r *Reconnector) Schedule(addr string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed || r.paused {
		return
	}

	state, exists := r.states[addr]
	if !exists {
		state = &reconnectState{
			nextDelay: r.cfg.InitialDelay,
		}
		r.states[addr] = state
	}

	// Cancel any existing timer
	if state.timer != nil {
		state.timer.Stop()
	}

	// Check max attempts
	if r.cfg.MaxAttempts > 0 && state.attempts >= r.cfg.MaxAttempts {
		delete(r.states, addr)
		return
	}

	// Calculate delay with jitter
	delay := r.addJitter(state.nextDelay)

	// Schedule reconnect
	state.timer = time.AfterFunc(delay, func() {
		r.attemptReconnect(addr)
	})
}

// attemptReconnect attempts to reconnect to the given address.
func (r *Reconnector) attemptReconnect(addr string) {
	r.mu.Lock()
	state, exists := r.states[addr]
	if !exists || r.closed {
		r.mu.Unlock()
		return
	}
	firedTimer := state.timer // pin the timer that invoked us

	state.attempts++
	state.lastAttempt = time.Now()

	// Calculate next delay with exponential backoff
	nextDelay := time.Duration(float64(state.nextDelay) * r.cfg.Multiplier)
	if nextDelay > r.cfg.MaxDelay {
		nextDelay = r.cfg.MaxDelay
	}
	state.nextDelay = nextDelay
	r.mu.Unlock()

	// Attempt reconnection
	err := r.callback(addr)

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return
	}

	// State may have been cleared (Cancel/Reset) or replaced while the
	// callback was running; in that case there is nothing to do here.
	if current, ok := r.states[addr]; !ok || current != state {
		return
	}

	// A concurrent Schedule -- typically handleDisconnect of a peer that
	// briefly connected during the callback and died immediately -- has
	// already installed a new timer. Leave the state alone so that timer
	// gets to fire; re-arming or deleting here would strand it.
	if state.timer != firedTimer {
		return
	}

	if err != nil {
		// Reschedule if still within limits
		if r.cfg.MaxAttempts == 0 || state.attempts < r.cfg.MaxAttempts {
			delay := r.addJitter(state.nextDelay)
			state.timer = time.AfterFunc(delay, func() {
				r.attemptReconnect(addr)
			})
		} else {
			// Max attempts reached, clean up
			delete(r.states, addr)
		}
	} else {
		// Success with no concurrent Schedule: nothing pending, drop state.
		delete(r.states, addr)
	}
}

// addJitter adds random jitter to a duration.
func (r *Reconnector) addJitter(d time.Duration) time.Duration {
	if r.cfg.Jitter <= 0 {
		return d
	}

	// Use time-based pseudo-random for simplicity
	// In production, you might want crypto/rand
	jitterRange := float64(d) * r.cfg.Jitter
	jitter := (float64(time.Now().UnixNano()%1000)/1000.0 - 0.5) * 2 * jitterRange

	result := time.Duration(float64(d) + jitter)
	if result < 0 {
		result = d
	}
	return result
}

// Cancel cancels any pending reconnection for the given address.
func (r *Reconnector) Cancel(addr string) {
	r.clearState(addr)
}

// Reset resets the reconnection state for an address.
// This is an alias for Cancel.
func (r *Reconnector) Reset(addr string) {
	r.clearState(addr)
}

// clearState removes the reconnection state for an address, stopping any pending timer.
func (r *Reconnector) clearState(addr string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if state, exists := r.states[addr]; exists {
		if state.timer != nil {
			state.timer.Stop()
		}
		delete(r.states, addr)
	}
}

// ResetAll clears all reconnection state, stopping any pending timers.
// This resets backoff delays to initial values for fresh reconnection attempts.
func (r *Reconnector) ResetAll() {
	r.mu.Lock()
	defer r.mu.Unlock()

	for addr, state := range r.states {
		if state.timer != nil {
			state.timer.Stop()
		}
		delete(r.states, addr)
	}
}

// GetAttempts returns the number of reconnection attempts for an address.
func (r *Reconnector) GetAttempts(addr string) int {
	r.mu.Lock()
	defer r.mu.Unlock()

	if state, exists := r.states[addr]; exists {
		return state.attempts
	}
	return 0
}

// IsPending returns true if a reconnection is pending for the address.
func (r *Reconnector) IsPending(addr string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	_, exists := r.states[addr]
	return exists
}

// Stop stops all reconnection attempts.
func (r *Reconnector) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.closed = true

	for addr, state := range r.states {
		if state.timer != nil {
			state.timer.Stop()
		}
		delete(r.states, addr)
	}
}

// Pause temporarily stops all reconnection attempts without clearing state.
// Pending timers are stopped but state is preserved for Resume().
func (r *Reconnector) Pause() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.paused || r.closed {
		return
	}

	r.paused = true

	// Stop all pending timers
	for _, state := range r.states {
		if state.timer != nil {
			state.timer.Stop()
			state.timer = nil
		}
	}
}

// Resume resumes reconnection attempts after Pause.
// Does not automatically reschedule - call Schedule() for specific addresses.
func (r *Reconnector) Resume() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.paused = false
}

// IsPaused returns true if the reconnector is paused.
func (r *Reconnector) IsPaused() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.paused
}

// BackoffCalculator calculates backoff delays.
type BackoffCalculator struct {
	cfg ReconnectConfig
}

// NewBackoffCalculator creates a new backoff calculator.
func NewBackoffCalculator(cfg ReconnectConfig) *BackoffCalculator {
	return &BackoffCalculator{cfg: cfg}
}

// CalculateDelay calculates the delay for the given attempt number (0-indexed).
func (b *BackoffCalculator) CalculateDelay(attempt int) time.Duration {
	if attempt <= 0 {
		return b.cfg.InitialDelay
	}

	delay := float64(b.cfg.InitialDelay) * math.Pow(b.cfg.Multiplier, float64(attempt))
	if delay > float64(b.cfg.MaxDelay) {
		delay = float64(b.cfg.MaxDelay)
	}

	return time.Duration(delay)
}
