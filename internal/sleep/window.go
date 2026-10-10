// Package sleep implements mesh hibernation mode for Muti Metroo agents.
package sleep

import (
	"encoding/binary"
	"time"

	"github.com/postalsys/muti-metroo/internal/identity"
)

// WindowConfig configures deterministic listening window calculation.
type WindowConfig struct {
	// CycleLength is the base cycle duration. Agents listen once per cycle.
	// Default: 5 minutes.
	CycleLength time.Duration

	// WindowLength is the maximum duration of the listening window.
	// Must be less than CycleLength.
	// Default: 30 seconds.
	WindowLength time.Duration

	// ClockTolerance accounts for clock drift between agents.
	// Connection attempts should start ClockTolerance before the predicted window.
	// Default: 5 seconds.
	ClockTolerance time.Duration

	// Epoch is the reference point for window calculations.
	// All agents should use the same epoch.
	// Default: Unix epoch (1970-01-01 00:00:00 UTC).
	Epoch time.Time
}

// DefaultWindowConfig returns a WindowConfig with sensible defaults.
func DefaultWindowConfig() WindowConfig {
	return WindowConfig{
		CycleLength:    5 * time.Minute,
		WindowLength:   30 * time.Second,
		ClockTolerance: 5 * time.Second,
		Epoch:          time.Unix(0, 0).UTC(), // Unix epoch
	}
}

// WindowInfo contains information about a listening window.
type WindowInfo struct {
	// Start is the beginning of the window (without tolerance).
	Start time.Time

	// End is the end of the window (without tolerance).
	End time.Time

	// SafeStart is Start minus ClockTolerance (for early connection attempts).
	SafeStart time.Time

	// SafeEnd is End plus ClockTolerance (for late connection attempts).
	SafeEnd time.Time

	// Midpoint is the ideal time to attempt connection.
	Midpoint time.Time

	// TimeUntil is the duration until SafeStart (negative if already in window).
	TimeUntil time.Duration

	// CurrentlyActive is true if the current time is within SafeStart to SafeEnd.
	CurrentlyActive bool
}

// WindowCalculator computes deterministic listening windows based on agent ID.
type WindowCalculator struct {
	cfg WindowConfig
}

// NewWindowCalculator creates a new WindowCalculator with the given config.
func NewWindowCalculator(cfg WindowConfig) *WindowCalculator {
	// Ensure WindowLength < CycleLength
	if cfg.WindowLength >= cfg.CycleLength {
		cfg.WindowLength = cfg.CycleLength / 6 // ~16% of cycle
	}

	// Ensure Epoch is set
	if cfg.Epoch.IsZero() {
		cfg.Epoch = time.Unix(0, 0).UTC()
	}

	return &WindowCalculator{cfg: cfg}
}

// seedFromAgentID derives a deterministic seed from an AgentID.
// Uses XOR of the two 64-bit halves of the 16-byte ID.
func seedFromAgentID(agentID identity.AgentID) uint64 {
	hi := binary.BigEndian.Uint64(agentID[:8])
	lo := binary.BigEndian.Uint64(agentID[8:])
	return hi ^ lo
}

// windowOffset calculates the offset within a cycle for a given agent.
// The offset is deterministic based on the agent's ID.
func (w *WindowCalculator) windowOffset(agentID identity.AgentID) time.Duration {
	seed := seedFromAgentID(agentID)

	// Maximum offset ensures window fits within cycle
	maxOffset := w.cfg.CycleLength - w.cfg.WindowLength
	if maxOffset <= 0 {
		return 0
	}

	// Deterministic offset based on agent ID
	offsetNs := int64(seed % uint64(maxOffset.Nanoseconds()))
	return time.Duration(offsetNs)
}

// cycleStart returns the start time of the cycle containing the given time.
func (w *WindowCalculator) cycleStart(t time.Time) time.Time {
	elapsed := t.Sub(w.cfg.Epoch)
	cycleNum := elapsed / w.cfg.CycleLength
	return w.cfg.Epoch.Add(cycleNum * w.cfg.CycleLength)
}

// NextWindow calculates the next listening window start and end times for an agent.
// If 'now' is during a window, returns that window.
// Otherwise, returns the next future window.
func (w *WindowCalculator) NextWindow(agentID identity.AgentID, now time.Time) (start, end time.Time) {
	offset := w.windowOffset(agentID)

	// Get current cycle's window
	cycleStart := w.cycleStart(now)
	windowStart := cycleStart.Add(offset)
	windowEnd := windowStart.Add(w.cfg.WindowLength)

	// If we're past this cycle's window, use next cycle
	if now.After(windowEnd) {
		cycleStart = cycleStart.Add(w.cfg.CycleLength)
		windowStart = cycleStart.Add(offset)
		windowEnd = windowStart.Add(w.cfg.WindowLength)
	}

	return windowStart, windowEnd
}

// GetWindowInfo returns detailed information about the next listening window.
func (w *WindowCalculator) GetWindowInfo(agentID identity.AgentID, now time.Time) WindowInfo {
	start, end := w.NextWindow(agentID, now)

	safeStart := start.Add(-w.cfg.ClockTolerance)
	safeEnd := end.Add(w.cfg.ClockTolerance)
	midpoint := start.Add(w.cfg.WindowLength / 2)

	var timeUntil time.Duration
	if now.Before(safeStart) {
		timeUntil = safeStart.Sub(now)
	} else {
		timeUntil = 0 // Already in or past safe start
	}

	currentlyActive := !now.Before(safeStart) && now.Before(safeEnd)

	return WindowInfo{
		Start:           start,
		End:             end,
		SafeStart:       safeStart,
		SafeEnd:         safeEnd,
		Midpoint:        midpoint,
		TimeUntil:       timeUntil,
		CurrentlyActive: currentlyActive,
	}
}

// TimeUntilWindow returns the duration until the next listening window's safe start.
// Returns 0 if currently within the safe window.
func (w *WindowCalculator) TimeUntilWindow(agentID identity.AgentID, now time.Time) time.Duration {
	info := w.GetWindowInfo(agentID, now)
	return info.TimeUntil
}

// IsInWindow returns true if the given time falls within the agent's listening window
// (including clock tolerance).
func (w *WindowCalculator) IsInWindow(agentID identity.AgentID, t time.Time) bool {
	info := w.GetWindowInfo(agentID, t)
	return info.CurrentlyActive
}

// PreviousWindow returns the previous listening window for an agent.
// Useful for debugging and testing.
func (w *WindowCalculator) PreviousWindow(agentID identity.AgentID, now time.Time) (start, end time.Time) {
	offset := w.windowOffset(agentID)

	// Get current cycle's window
	cycleStart := w.cycleStart(now)
	windowStart := cycleStart.Add(offset)

	// If we haven't reached this cycle's window, use previous cycle
	if now.Before(windowStart) {
		cycleStart = cycleStart.Add(-w.cfg.CycleLength)
		windowStart = cycleStart.Add(offset)
	}

	windowEnd := windowStart.Add(w.cfg.WindowLength)
	return windowStart, windowEnd
}

// GetConfig returns the calculator's configuration (for testing/debugging).
func (w *WindowCalculator) GetConfig() WindowConfig {
	return w.cfg
}
