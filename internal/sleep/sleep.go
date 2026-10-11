// Package sleep implements mesh hibernation mode for Muti Metroo agents.
// Sleep mode allows agents to close all peer connections and hibernate,
// periodically polling for queued messages with randomized timing to avoid
// traffic pattern detection.
package sleep

import (
	"encoding/json"
	"errors"
	"log/slog"
	"math/rand"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Muti-Metroo/Muti-Metroo/internal/config"
	"github.com/Muti-Metroo/Muti-Metroo/internal/identity"
	"github.com/Muti-Metroo/Muti-Metroo/internal/logging"
	"github.com/Muti-Metroo/Muti-Metroo/internal/protocol"
	"github.com/Muti-Metroo/Muti-Metroo/internal/recovery"
)

// State represents the current sleep state of an agent.
type State uint8

const (
	// StateAwake is normal operation with all connections active.
	StateAwake State = iota
	// StateSleeping is hibernated with connections closed and poll timer running.
	StateSleeping
	// StatePolling is briefly reconnected to receive queued messages.
	StatePolling
)

// String returns a human-readable name for the state.
func (s State) String() string {
	switch s {
	case StateAwake:
		return "AWAKE"
	case StateSleeping:
		return "SLEEPING"
	case StatePolling:
		return "POLLING"
	default:
		return "UNKNOWN"
	}
}

var (
	// ErrNotEnabled is returned when sleep mode operations are attempted but not enabled.
	ErrNotEnabled = errors.New("sleep mode not enabled")
	// ErrAlreadySleeping is returned when trying to sleep while already sleeping.
	ErrAlreadySleeping = errors.New("already sleeping")
	// ErrNotSleeping is returned when trying to wake while not sleeping.
	ErrNotSleeping = errors.New("not sleeping")
	// ErrCallbackNotSet is returned when a required callback is not configured.
	ErrCallbackNotSet = errors.New("required callback not set")
)

// Callbacks defines the functions called during state transitions.
type Callbacks struct {
	// OnSleep is called when entering sleep mode.
	// Should close all peer connections and stop SOCKS5/listeners.
	OnSleep func() error

	// OnWake is called when exiting sleep mode.
	// Should restart all connections and services.
	OnWake func() error

	// OnPoll is called during polling to briefly reconnect.
	// Should reconnect to peers, receive queued state, then disconnect.
	OnPoll func() error

	// OnPollEnd is called when poll duration ends.
	// Should disconnect peers and return to sleeping.
	OnPollEnd func() error
}

// PersistedState is the sleep state saved to disk.
type PersistedState struct {
	State          State     `json:"state"`
	SleepStartTime time.Time `json:"sleep_start_time,omitempty"`
	LastPollTime   time.Time `json:"last_poll_time,omitempty"`
	CommandSeq     uint64    `json:"command_seq"`
}

// Manager handles sleep mode state transitions and scheduling.
type Manager struct {
	cfg     config.SleepConfig
	dataDir string
	logger  *slog.Logger

	// State
	state     atomic.Value // State
	stateMu   sync.RWMutex
	stateFile string

	// Timing
	sleepStartTime time.Time
	lastPollTime   time.Time
	nextPollTime   time.Time
	pollTimer      *time.Timer

	// Deterministic windows
	localID    identity.AgentID
	windowCalc *WindowCalculator

	// Command tracking for deduplication
	commandSeq atomic.Uint64
	seenMu     sync.RWMutex
	seenCmds   map[sleepCmdKey]time.Time

	// Message queue for sleeping peers
	queue *StateQueue

	// Callbacks to agent
	callbacks Callbacks

	// Lifecycle
	stopCh   chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
}

type sleepCmdKey struct {
	OriginAgent identity.AgentID
	CommandID   uint64
}

// NewManager creates a new sleep manager.
func NewManager(cfg config.SleepConfig, dataDir string, logger *slog.Logger) *Manager {
	if logger == nil {
		logger = logging.NopLogger()
	}

	m := &Manager{
		cfg:       cfg,
		dataDir:   dataDir,
		logger:    logger.With("component", "sleep"),
		stateFile: filepath.Join(dataDir, "sleep_state.json"),
		seenCmds:  make(map[sleepCmdKey]time.Time),
		queue:     NewStateQueue(cfg.MaxQueuedMessages),
		stopCh:    make(chan struct{}),
	}

	// Initialize deterministic window calculator if enabled
	if cfg.DeterministicWindows.Enabled {
		windowCfg := DefaultWindowConfig()

		// Use poll interval as cycle length (agents listen once per poll cycle)
		windowCfg.CycleLength = cfg.PollInterval

		if cfg.DeterministicWindows.WindowLength > 0 {
			windowCfg.WindowLength = cfg.DeterministicWindows.WindowLength
		}
		if cfg.DeterministicWindows.ClockTolerance > 0 {
			windowCfg.ClockTolerance = cfg.DeterministicWindows.ClockTolerance
		}
		if cfg.DeterministicWindows.Epoch != "" {
			if epoch, err := time.Parse(time.RFC3339, cfg.DeterministicWindows.Epoch); err == nil {
				windowCfg.Epoch = epoch
			}
		}

		m.windowCalc = NewWindowCalculator(windowCfg)
		m.logger.Info("deterministic windows enabled",
			"cycle_length", windowCfg.CycleLength,
			"window_length", windowCfg.WindowLength,
			"clock_tolerance", windowCfg.ClockTolerance)
	}

	// Initialize state
	m.state.Store(StateAwake)

	return m
}

// SetCallbacks configures the callbacks for state transitions.
func (m *Manager) SetCallbacks(cb Callbacks) {
	m.callbacks = cb
}

// Start begins the sleep manager.
// If PersistState is enabled and state was saved, it resumes from that state.
func (m *Manager) Start() error {
	if !m.cfg.Enabled {
		return nil
	}

	// Load persisted state if available
	if m.cfg.PersistState {
		if err := m.LoadState(); err != nil {
			m.logger.Debug("no persisted sleep state", logging.KeyError, err)
		}
	}

	// Start cleanup goroutine for seen commands cache
	m.wg.Add(1)
	go m.cleanupLoop()

	// If we loaded a sleeping state, resume sleep mode
	currentState := m.GetState()
	if currentState == StateSleeping || currentState == StatePolling {
		m.logger.Info("resuming sleep mode from persisted state")
		m.schedulePoll()
	} else if m.cfg.AutoSleepOnStart {
		m.logger.Info("auto-sleeping on start")
		// Delay the auto-sleep to allow agent to fully initialize
		m.wg.Add(1)
		go func() {
			defer m.wg.Done()
			select {
			case <-time.After(5 * time.Second):
			case <-m.stopCh:
				return
			}
			if err := m.Sleep(); err != nil && err != ErrAlreadySleeping {
				m.logger.Error("auto-sleep failed", logging.KeyError, err)
			}
		}()
	}

	return nil
}

// Stop gracefully stops the sleep manager. Safe to call more than once;
// closing stopCh unconditionally would panic on a second call.
func (m *Manager) Stop() {
	m.stopOnce.Do(func() {
		close(m.stopCh)

		m.stateMu.Lock()
		if m.pollTimer != nil {
			m.pollTimer.Stop()
		}
		m.stateMu.Unlock()

		m.wg.Wait()

		// Persist final state
		if m.cfg.PersistState {
			if err := m.persistState(); err != nil {
				m.logger.Debug("failed to persist sleep state", logging.KeyError, err)
			}
		}
	})
}

// Sleep transitions the agent into sleep mode.
func (m *Manager) Sleep() error {
	if !m.cfg.Enabled {
		return ErrNotEnabled
	}

	m.stateMu.Lock()
	defer m.stateMu.Unlock()

	currentState := m.state.Load().(State)
	if currentState == StateSleeping || currentState == StatePolling {
		return ErrAlreadySleeping
	}

	// Call sleep callback under the lock to prevent concurrent
	// Sleep/Wake transitions. Safe because callbacks (agent.enterSleep)
	// do not call back into stateMu-protected methods.
	if m.callbacks.OnSleep != nil {
		if err := m.callbacks.OnSleep(); err != nil {
			return err
		}
	}

	// Update state
	m.state.Store(StateSleeping)
	m.sleepStartTime = time.Now()
	m.lastPollTime = time.Time{}

	// Schedule first poll
	m.schedulePollLocked()

	// Persist state
	if m.cfg.PersistState {
		if err := m.persistState(); err != nil {
			m.logger.Debug("failed to persist sleep state", logging.KeyError, err)
		}
	}

	m.logger.Info("entered sleep mode",
		"next_poll", m.nextPollTime.Format(time.RFC3339))

	return nil
}

// Wake transitions the agent out of sleep mode.
func (m *Manager) Wake() error {
	if !m.cfg.Enabled {
		return ErrNotEnabled
	}

	m.stateMu.Lock()
	defer m.stateMu.Unlock()

	currentState := m.state.Load().(State)
	if currentState == StateAwake {
		return ErrNotSleeping
	}

	// Stop poll timer
	if m.pollTimer != nil {
		m.pollTimer.Stop()
		m.pollTimer = nil
	}

	// Call wake callback under the lock to prevent concurrent
	// Sleep/Wake transitions. Safe because callbacks (agent.exitSleep)
	// do not call back into stateMu-protected methods.
	if m.callbacks.OnWake != nil {
		if err := m.callbacks.OnWake(); err != nil {
			return err
		}
	}

	// Update state
	m.state.Store(StateAwake)
	sleepDuration := time.Since(m.sleepStartTime)
	m.sleepStartTime = time.Time{}
	m.nextPollTime = time.Time{}

	// Clear queue
	m.queue.Clear()

	// Persist state
	if m.cfg.PersistState {
		if err := m.persistState(); err != nil {
			m.logger.Debug("failed to persist sleep state", logging.KeyError, err)
		}
	}

	m.logger.Info("exited sleep mode",
		"sleep_duration", sleepDuration.String())

	return nil
}

// Poll briefly reconnects to receive queued messages.
// This is called automatically by the poll timer.
func (m *Manager) Poll() error {
	if !m.cfg.Enabled {
		return ErrNotEnabled
	}

	m.stateMu.Lock()
	currentState := m.state.Load().(State)
	if currentState != StateSleeping {
		m.stateMu.Unlock()
		return nil // Silently skip if not sleeping
	}

	// Transition to polling
	m.state.Store(StatePolling)
	m.lastPollTime = time.Now()
	m.stateMu.Unlock()

	m.logger.Debug("starting poll")

	// Call poll callback
	if m.callbacks.OnPoll != nil {
		if err := m.callbacks.OnPoll(); err != nil {
			m.logger.Error("poll callback failed", logging.KeyError, err)
		}
	}

	// Wait for poll duration
	select {
	case <-time.After(m.cfg.PollDuration):
	case <-m.stopCh:
		return nil
	}

	m.stateMu.Lock()
	defer m.stateMu.Unlock()

	// Check if we were woken during poll
	if m.state.Load().(State) == StateAwake {
		return nil
	}

	// Call poll end callback
	if m.callbacks.OnPollEnd != nil {
		if err := m.callbacks.OnPollEnd(); err != nil {
			m.logger.Error("poll end callback failed", logging.KeyError, err)
		}
	}

	// Return to sleeping
	m.state.Store(StateSleeping)

	// Schedule next poll
	m.schedulePollLocked()

	// Persist state
	if m.cfg.PersistState {
		if err := m.persistState(); err != nil {
			m.logger.Debug("failed to persist sleep state", logging.KeyError, err)
		}
	}

	m.logger.Debug("poll complete",
		"next_poll", m.nextPollTime.Format(time.RFC3339))

	return nil
}

// GetState returns the current sleep state.
func (m *Manager) GetState() State {
	return m.state.Load().(State)
}

// GetStatus returns detailed status information.
func (m *Manager) GetStatus() StatusInfo {
	m.stateMu.RLock()
	defer m.stateMu.RUnlock()

	status := StatusInfo{
		State:                m.state.Load().(State),
		Enabled:              m.cfg.Enabled,
		SleepStartTime:       m.sleepStartTime,
		LastPollTime:         m.lastPollTime,
		NextPollTime:         m.nextPollTime,
		QueuedPeers:          m.queue.PeerCount(),
		DeterministicWindows: m.windowCalc != nil,
	}

	// Include window info if deterministic windows enabled
	if m.windowCalc != nil && m.localID != (identity.AgentID{}) {
		info := m.windowCalc.GetWindowInfo(m.localID, time.Now())
		status.NextWindow = &info
	}

	return status
}

// StatusInfo contains detailed sleep status.
type StatusInfo struct {
	State                State       `json:"state"`
	Enabled              bool        `json:"enabled"`
	SleepStartTime       time.Time   `json:"sleep_start_time,omitempty"`
	LastPollTime         time.Time   `json:"last_poll_time,omitempty"`
	NextPollTime         time.Time   `json:"next_poll_time,omitempty"`
	QueuedPeers          int         `json:"queued_peers"`
	DeterministicWindows bool        `json:"deterministic_windows"`
	NextWindow           *WindowInfo `json:"next_window,omitempty"`
}

// NextCommandID returns the next command ID for sleep/wake commands.
func (m *Manager) NextCommandID() uint64 {
	return m.commandSeq.Add(1)
}

// isNewCommand checks if a command has been seen and marks it as seen if new.
// Returns true if the command is new.
func (m *Manager) isNewCommand(originAgent identity.AgentID, commandID uint64) bool {
	key := sleepCmdKey{
		OriginAgent: originAgent,
		CommandID:   commandID,
	}

	m.seenMu.Lock()
	defer m.seenMu.Unlock()

	if _, seen := m.seenCmds[key]; seen {
		return false
	}
	m.seenCmds[key] = time.Now()
	return true
}

// HandleSleepCommand processes an incoming sleep command.
// Returns true if the command is new and should be acted upon.
func (m *Manager) HandleSleepCommand(cmd *protocol.SleepCommand) bool {
	if !m.cfg.Enabled {
		return false
	}

	if !m.isNewCommand(cmd.OriginAgent, cmd.CommandID) {
		return false
	}

	m.logger.Info("received sleep command",
		"origin", cmd.OriginAgent.ShortString(),
		"command_id", cmd.CommandID)

	return true
}

// HandleWakeCommand processes an incoming wake command.
// Returns true if the command is new and should be acted upon.
func (m *Manager) HandleWakeCommand(cmd *protocol.WakeCommand) bool {
	if !m.cfg.Enabled {
		return false
	}

	if !m.isNewCommand(cmd.OriginAgent, cmd.CommandID) {
		return false
	}

	m.logger.Info("received wake command",
		"origin", cmd.OriginAgent.ShortString(),
		"command_id", cmd.CommandID)

	return true
}

// QueueForPeer queues a route advertisement for a sleeping peer.
func (m *Manager) QueueRouteForPeer(peerID identity.AgentID, adv *protocol.RouteAdvertise) {
	m.queue.AddRoute(peerID, adv)
}

// QueueWithdrawForPeer queues a route withdrawal for a sleeping peer.
func (m *Manager) QueueWithdrawForPeer(peerID identity.AgentID, withdraw *protocol.RouteWithdraw) {
	m.queue.AddWithdraw(peerID, withdraw)
}

// QueueNodeInfoForPeer queues a node info advertisement for a sleeping peer.
func (m *Manager) QueueNodeInfoForPeer(peerID identity.AgentID, info *protocol.NodeInfoAdvertise) {
	m.queue.AddNodeInfo(peerID, info)
}

// GetQueuedState retrieves and clears queued state for a reconnecting peer.
func (m *Manager) GetQueuedState(peerID identity.AgentID) *protocol.QueuedState {
	return m.queue.GetAndClear(peerID)
}

// schedulePoll schedules the next poll with jittered timing.
func (m *Manager) schedulePoll() {
	m.stateMu.Lock()
	defer m.stateMu.Unlock()
	m.schedulePollLocked()
}

// schedulePollLocked schedules the next poll (must hold stateMu).
func (m *Manager) schedulePollLocked() {
	var interval time.Duration

	// Use deterministic windows if enabled and we have a local ID
	if m.windowCalc != nil && m.localID != (identity.AgentID{}) {
		now := time.Now()
		windowInfo := m.windowCalc.GetWindowInfo(m.localID, now)

		// If we're currently in a window, schedule for the next one
		if windowInfo.CurrentlyActive {
			// Get next cycle's window
			nextCycleStart := windowInfo.End.Add(m.cfg.PollInterval - m.windowCalc.cfg.WindowLength)
			interval = nextCycleStart.Sub(now)
			if interval < 0 {
				interval = m.cfg.PollInterval
			}
		} else {
			// Schedule for the upcoming window
			interval = windowInfo.TimeUntil
			if interval <= 0 {
				// Fallback to jittered interval
				interval = m.jitteredInterval(m.cfg.PollInterval, m.cfg.PollIntervalJitter)
			}
		}

		m.logger.Debug("deterministic window scheduled",
			"next_window_start", windowInfo.Start.Format(time.RFC3339),
			"interval", interval)
	} else {
		// Fallback to jittered random interval
		interval = m.jitteredInterval(m.cfg.PollInterval, m.cfg.PollIntervalJitter)
	}

	m.nextPollTime = time.Now().Add(interval)

	if m.pollTimer != nil {
		m.pollTimer.Stop()
	}

	m.pollTimer = time.AfterFunc(interval, func() {
		defer recovery.RecoverWithLog(m.logger, "sleep.pollTimer")
		select {
		case <-m.stopCh:
			return
		default:
		}
		if err := m.Poll(); err != nil {
			m.logger.Error("poll failed", logging.KeyError, err)
		}
	})
}

// jitteredInterval calculates an interval with random jitter.
func (m *Manager) jitteredInterval(base time.Duration, jitter float64) time.Duration {
	if jitter <= 0 {
		return base
	}
	// Random value in range [-jitter, +jitter]
	factor := 1.0 + (rand.Float64()*2-1)*jitter
	return time.Duration(float64(base) * factor)
}

// cleanupLoop periodically cleans up expired seen commands.
func (m *Manager) cleanupLoop() {
	defer m.wg.Done()
	defer recovery.RecoverWithLog(m.logger, "sleep.cleanupLoop")

	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopCh:
			return
		case <-ticker.C:
			m.cleanupSeenCommands()
		}
	}
}

// cleanupSeenCommands removes old entries from the seen commands cache.
func (m *Manager) cleanupSeenCommands() {
	m.seenMu.Lock()
	defer m.seenMu.Unlock()

	cutoff := time.Now().Add(-30 * time.Minute)
	for key, seenAt := range m.seenCmds {
		if seenAt.Before(cutoff) {
			delete(m.seenCmds, key)
		}
	}
}

// persistState saves the current state to disk.
func (m *Manager) persistState() error {
	state := PersistedState{
		State:          m.state.Load().(State),
		SleepStartTime: m.sleepStartTime,
		LastPollTime:   m.lastPollTime,
		CommandSeq:     m.commandSeq.Load(),
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(m.stateFile, data, 0600)
}

// LoadState loads persisted state from disk.
func (m *Manager) LoadState() error {
	data, err := os.ReadFile(m.stateFile)
	if err != nil {
		return err
	}

	var state PersistedState
	if err := json.Unmarshal(data, &state); err != nil {
		return err
	}

	m.state.Store(state.State)
	m.sleepStartTime = state.SleepStartTime
	m.lastPollTime = state.LastPollTime
	m.commandSeq.Store(state.CommandSeq)

	return nil
}

// IsSleeping returns true if the agent is currently sleeping or polling.
func (m *Manager) IsSleeping() bool {
	state := m.GetState()
	return state == StateSleeping || state == StatePolling
}

// SetLocalID sets the local agent ID for deterministic window calculations.
// Must be called before entering sleep mode if deterministic windows are enabled.
func (m *Manager) SetLocalID(id identity.AgentID) {
	m.stateMu.Lock()
	defer m.stateMu.Unlock()
	m.localID = id
}

// GetNextWindowInfo returns the next listening window for the given agent ID.
// Allows peers to calculate when a sleeping agent will be available.
// Returns nil if deterministic windows are not enabled.
func (m *Manager) GetNextWindowInfo(agentID identity.AgentID) *WindowInfo {
	if m.windowCalc == nil {
		return nil
	}
	info := m.windowCalc.GetWindowInfo(agentID, time.Now())
	return &info
}

// GetLocalWindowInfo returns the next listening window for the local agent.
// Returns nil if deterministic windows are not enabled or local ID is not set.
func (m *Manager) GetLocalWindowInfo() *WindowInfo {
	if m.windowCalc == nil {
		return nil
	}

	m.stateMu.RLock()
	localID := m.localID
	m.stateMu.RUnlock()

	if localID == (identity.AgentID{}) {
		return nil
	}

	info := m.windowCalc.GetWindowInfo(localID, time.Now())
	return &info
}

// HasDeterministicWindows returns true if deterministic windows are enabled.
func (m *Manager) HasDeterministicWindows() bool {
	return m.windowCalc != nil
}
