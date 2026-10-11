package chaos

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ============================================================================
// Connection State Chaos Tests
// ============================================================================

// MockConnection represents a simplified connection for chaos testing.
type MockConnection struct {
	id            string
	state         atomic.Int32
	mu            sync.Mutex
	streams       map[uint64]*MockStream
	alive         atomic.Bool
	closeMu       sync.Mutex // Protects closeOnce and closed
	closeOnce     sync.Once
	closed        chan struct{}
	onStateChange func(string, ConnectionTestState)
	faultInjector *FaultInjector
}

// ConnectionTestState represents connection states for testing.
type ConnectionTestState int32

const (
	TestStateDisconnected ConnectionTestState = iota
	TestStateConnecting
	TestStateHandshaking
	TestStateConnected
	TestStateReconnecting
)

func (s ConnectionTestState) String() string {
	switch s {
	case TestStateDisconnected:
		return "DISCONNECTED"
	case TestStateConnecting:
		return "CONNECTING"
	case TestStateHandshaking:
		return "HANDSHAKING"
	case TestStateConnected:
		return "CONNECTED"
	case TestStateReconnecting:
		return "RECONNECTING"
	default:
		return "UNKNOWN"
	}
}

// MockStream represents a simplified stream for chaos testing.
type MockStream struct {
	id        uint64
	state     atomic.Int32
	conn      *MockConnection
	data      chan []byte
	closed    chan struct{}
	closeOnce sync.Once
}

// StreamTestState represents stream states for testing.
type StreamTestState int32

const (
	TestStreamOpening StreamTestState = iota
	TestStreamOpen
	TestStreamHalfClosedLocal
	TestStreamHalfClosedRemote
	TestStreamClosed
)

// NewMockConnection creates a new mock connection for chaos testing.
func NewMockConnection(id string, injector *FaultInjector) *MockConnection {
	c := &MockConnection{
		id:            id,
		streams:       make(map[uint64]*MockStream),
		closed:        make(chan struct{}),
		faultInjector: injector,
	}
	c.alive.Store(true)
	c.state.Store(int32(TestStateDisconnected))
	return c
}

// ID returns the connection ID.
func (c *MockConnection) ID() string {
	return c.id
}

// Kill closes the connection abruptly.
func (c *MockConnection) Kill() error {
	c.closeMu.Lock()
	defer c.closeMu.Unlock()

	c.closeOnce.Do(func() {
		c.alive.Store(false)
		c.SetState(TestStateDisconnected)
		close(c.closed)

		// Close all streams
		c.mu.Lock()
		for _, s := range c.streams {
			s.Close()
		}
		c.streams = make(map[uint64]*MockStream)
		c.mu.Unlock()
	})
	return nil
}

// IsAlive returns whether the connection is alive.
func (c *MockConnection) IsAlive() bool {
	return c.alive.Load()
}

// Restart restarts the connection.
func (c *MockConnection) Restart() error {
	c.closeMu.Lock()
	defer c.closeMu.Unlock()

	c.alive.Store(true)
	c.closed = make(chan struct{})
	c.closeOnce = sync.Once{}
	c.SetState(TestStateConnecting)
	return nil
}

// State returns the current connection state.
func (c *MockConnection) State() ConnectionTestState {
	return ConnectionTestState(c.state.Load())
}

// SetState sets the connection state.
func (c *MockConnection) SetState(state ConnectionTestState) {
	old := c.State()
	c.state.Store(int32(state))
	if c.onStateChange != nil && old != state {
		c.onStateChange(c.id, state)
	}
}

// Connect simulates connecting to the peer.
func (c *MockConnection) Connect(ctx context.Context) error {
	if !c.alive.Load() {
		return errors.New("connection closed")
	}

	c.SetState(TestStateConnecting)

	// Inject fault during connect
	if c.faultInjector != nil {
		if delay := c.faultInjector.MaybeDelay(); delay > 0 {
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		if c.faultInjector.MaybeDisconnect() {
			c.Kill()
			return errors.New("injected disconnect during connect")
		}
		if c.faultInjector.MaybeError() {
			return errors.New("injected error during connect")
		}
	}

	c.SetState(TestStateConnected)
	return nil
}

// OpenStream opens a new stream on the connection.
func (c *MockConnection) OpenStream(ctx context.Context, streamID uint64) (*MockStream, error) {
	if c.State() != TestStateConnected {
		return nil, errors.New("connection not connected")
	}

	// Inject fault during stream open
	if c.faultInjector != nil {
		if c.faultInjector.MaybeDisconnect() {
			c.Kill()
			return nil, errors.New("connection killed during stream open")
		}
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if _, exists := c.streams[streamID]; exists {
		return nil, errors.New("stream already exists")
	}

	stream := &MockStream{
		id:     streamID,
		conn:   c,
		data:   make(chan []byte, 64),
		closed: make(chan struct{}),
	}
	stream.state.Store(int32(TestStreamOpen))
	c.streams[streamID] = stream

	return stream, nil
}

// GetStream returns a stream by ID.
func (c *MockConnection) GetStream(streamID uint64) *MockStream {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.streams[streamID]
}

// CloseStream closes a stream.
func (c *MockConnection) CloseStream(streamID uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if stream, ok := c.streams[streamID]; ok {
		stream.Close()
		delete(c.streams, streamID)
	}
}

// StreamCount returns the number of active streams.
func (c *MockConnection) StreamCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.streams)
}

// Close closes the stream.
func (s *MockStream) Close() {
	s.closeOnce.Do(func() {
		close(s.closed)
		s.state.Store(int32(TestStreamClosed))
	})
}

// State returns the stream state.
func (s *MockStream) State() StreamTestState {
	return StreamTestState(s.state.Load())
}

// ============================================================================
// Chaos Test Scenarios
// ============================================================================

// TestChaos_ConnectionStateTransitions tests state transitions under chaos.
func TestChaos_ConnectionStateTransitions(t *testing.T) {
	injector := NewFaultInjector(
		FaultConfig{Type: FaultDisconnect, Probability: 0.2},
		FaultConfig{Type: FaultDelay, Probability: 0.3, MinDelay: 10 * time.Millisecond, MaxDelay: 50 * time.Millisecond},
	)

	stateTransitions := make([]struct {
		connID string
		state  ConnectionTestState
	}, 0)
	var mu sync.Mutex

	conn := NewMockConnection("test-conn-1", injector)
	conn.onStateChange = func(id string, state ConnectionTestState) {
		mu.Lock()
		stateTransitions = append(stateTransitions, struct {
			connID string
			state  ConnectionTestState
		}{id, state})
		mu.Unlock()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Attempt multiple connect cycles
	successCount := 0
	failCount := 0

	for i := 0; i < 20; i++ {
		if !conn.IsAlive() {
			conn.Restart()
		}

		err := conn.Connect(ctx)
		if err != nil {
			failCount++
		} else {
			successCount++

			// Try to open some streams
			for j := 0; j < 5; j++ {
				stream, err := conn.OpenStream(ctx, uint64(i*5+j))
				if err != nil {
					break
				}
				stream.Close()
			}
		}

		// Random delay between attempts
		time.Sleep(10 * time.Millisecond)
	}

	t.Logf("Connect attempts: success=%d, fail=%d", successCount, failCount)
	t.Logf("State transitions recorded: %d", len(stateTransitions))

	// Verify we had some failures due to chaos
	if failCount == 0 {
		t.Log("No failures occurred (chaos may not have triggered)")
	}

	// Verify state machine integrity
	for i, trans := range stateTransitions {
		if trans.state < 0 || trans.state > TestStateReconnecting {
			t.Errorf("Invalid state at transition %d: %v", i, trans.state)
		}
	}
}

// TestChaos_StreamStateMachine tests stream state machine under chaos.
func TestChaos_StreamStateMachine(t *testing.T) {
	injector := NewFaultInjector(
		FaultConfig{Type: FaultDisconnect, Probability: 0.1},
		FaultConfig{Type: FaultError, Probability: 0.1},
	)

	conn := NewMockConnection("stream-test-conn", injector)
	ctx := context.Background()

	// Connect first
	conn.SetState(TestStateConnected)
	conn.alive.Store(true)

	var streamErrors int32
	var streamSuccesses int32

	// Open many streams concurrently
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(streamID uint64) {
			defer wg.Done()

			stream, err := conn.OpenStream(ctx, streamID)
			if err != nil {
				atomic.AddInt32(&streamErrors, 1)
				return
			}

			// Simulate some work
			time.Sleep(5 * time.Millisecond)

			// Verify state is valid
			state := stream.State()
			if state != TestStreamOpen && state != TestStreamClosed {
				t.Errorf("Stream %d in unexpected state: %v", streamID, state)
			}

			stream.Close()
			atomic.AddInt32(&streamSuccesses, 1)
		}(uint64(i))
	}

	wg.Wait()

	t.Logf("Streams: success=%d, errors=%d", streamSuccesses, streamErrors)
}

// TestChaos_ConcurrentConnectionKill tests killing connections concurrently.
func TestChaos_ConcurrentConnectionKill(t *testing.T) {
	injector := NewFaultInjector(
		FaultConfig{Type: FaultDisconnect, Probability: 0.3},
	)

	connections := make([]*MockConnection, 10)
	for i := range connections {
		connections[i] = NewMockConnection(fmt.Sprintf("conn-%d", i), injector)
		connections[i].alive.Store(true)
		connections[i].SetState(TestStateConnected)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Spawn goroutines that open streams
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()

			for j := 0; j < 10; j++ {
				select {
				case <-ctx.Done():
					return
				default:
				}

				conn := connections[worker%len(connections)]
				if !conn.IsAlive() {
					continue
				}

				stream, err := conn.OpenStream(ctx, uint64(worker*10+j))
				if err == nil {
					time.Sleep(2 * time.Millisecond)
					stream.Close()
				}
			}
		}(i)
	}

	// Meanwhile, chaos monkey kills connections
	monkey := NewChaosMonkey(20*time.Millisecond, injector)
	for _, conn := range connections {
		monkey.AddTarget(conn)
	}

	monkey.Start(ctx)
	wg.Wait()
	monkey.Stop()

	// Count alive connections
	alive := 0
	for _, conn := range connections {
		if conn.IsAlive() {
			alive++
		}
	}

	t.Logf("Connections alive after chaos: %d/%d", alive, len(connections))

	// Get chaos stats
	stats := injector.GetStats()
	t.Logf("Chaos stats: disconnects=%d", stats[FaultDisconnect])
}

// TestChaos_StateRecovery tests that state recovers properly after failures.
func TestChaos_StateRecovery(t *testing.T) {
	injector := NewFaultInjector(
		FaultConfig{Type: FaultDisconnect, Probability: 0.5},
	)

	conn := NewMockConnection("recovery-test", injector)
	ctx := context.Background()

	recoveryAttempts := 0
	successfulRecoveries := 0

	for i := 0; i < 20; i++ {
		// Try to connect
		err := conn.Connect(ctx)
		if err != nil {
			// Connection failed, try recovery
			recoveryAttempts++

			// Simulate reconnection backoff
			time.Sleep(10 * time.Millisecond)

			// Restart connection
			conn.Restart()

			// Retry
			err = conn.Connect(ctx)
			if err == nil {
				successfulRecoveries++
			}
		}

		if conn.State() == TestStateConnected {
			// Open a stream to verify connection is usable
			stream, err := conn.OpenStream(ctx, uint64(i))
			if err == nil {
				stream.Close()
			}
		}

		// Kill for next iteration
		conn.Kill()
		conn.Restart()
	}

	t.Logf("Recovery attempts: %d, successful: %d", recoveryAttempts, successfulRecoveries)

	// Final state should be valid
	state := conn.State()
	if state < TestStateDisconnected || state > TestStateReconnecting {
		t.Errorf("Invalid final state: %v", state)
	}
}

// TestChaos_StreamCleanupOnDisconnect tests that streams are cleaned up on disconnect.
func TestChaos_StreamCleanupOnDisconnect(t *testing.T) {
	conn := NewMockConnection("cleanup-test", nil)
	conn.alive.Store(true)
	conn.SetState(TestStateConnected)

	ctx := context.Background()

	// Open several streams
	streams := make([]*MockStream, 10)
	for i := range streams {
		stream, err := conn.OpenStream(ctx, uint64(i))
		if err != nil {
			t.Fatalf("Failed to open stream %d: %v", i, err)
		}
		streams[i] = stream
	}

	// Verify streams are open
	if conn.StreamCount() != 10 {
		t.Errorf("Expected 10 streams, got %d", conn.StreamCount())
	}

	// Kill the connection
	conn.Kill()

	// Verify all streams are closed
	if conn.StreamCount() != 0 {
		t.Errorf("Expected 0 streams after kill, got %d", conn.StreamCount())
	}

	// Verify each stream is in closed state
	for i, stream := range streams {
		if stream.State() != TestStreamClosed {
			t.Errorf("Stream %d should be closed, got state %v", i, stream.State())
		}
	}
}

// TestChaos_RapidStateChanges tests handling of rapid state changes.
func TestChaos_RapidStateChanges(t *testing.T) {
	conn := NewMockConnection("rapid-state-test", nil)

	stateChanges := make([]ConnectionTestState, 0, 1000)
	var mu sync.Mutex

	conn.onStateChange = func(id string, state ConnectionTestState) {
		mu.Lock()
		stateChanges = append(stateChanges, state)
		mu.Unlock()
	}

	// Rapidly change states from multiple goroutines
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			states := []ConnectionTestState{
				TestStateDisconnected,
				TestStateConnecting,
				TestStateHandshaking,
				TestStateConnected,
				TestStateReconnecting,
			}
			for j := 0; j < 100; j++ {
				conn.SetState(states[(worker+j)%len(states)])
			}
		}(i)
	}

	wg.Wait()

	mu.Lock()
	changeCount := len(stateChanges)
	mu.Unlock()

	t.Logf("Total state changes recorded: %d", changeCount)

	// Verify final state is valid
	finalState := conn.State()
	if finalState < TestStateDisconnected || finalState > TestStateReconnecting {
		t.Errorf("Invalid final state: %v", finalState)
	}
}

// TestChaos_DelayedOperations tests operations with injected delays.
func TestChaos_DelayedOperations(t *testing.T) {
	injector := NewFaultInjector(
		FaultConfig{
			Type:        FaultDelay,
			Probability: 1.0, // Always delay
			MinDelay:    50 * time.Millisecond,
			MaxDelay:    100 * time.Millisecond,
		},
	)

	conn := NewMockConnection("delay-test", injector)
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := conn.Connect(ctx)
	elapsed := time.Since(start)

	// Should have been delayed
	if elapsed < 50*time.Millisecond && err == nil {
		t.Errorf("Operation completed too fast: %v", elapsed)
	}

	t.Logf("Connect took %v (expected ~50-100ms delay)", elapsed)
}

// TestChaos_ScenarioNetworkPartition simulates a network partition scenario.
func TestChaos_ScenarioNetworkPartition(t *testing.T) {
	runner := NewScenarioRunner()

	// Partition injector - high disconnect probability
	partitionInjector := NewFaultInjector(
		FaultConfig{Type: FaultDisconnect, Probability: 0.8},
	)

	connections := make([]*MockConnection, 5)
	for i := range connections {
		connections[i] = NewMockConnection(fmt.Sprintf("partition-conn-%d", i), partitionInjector)
	}

	runner.AddScenario(Scenario{
		Name:        "network-partition",
		Description: "Simulates network partition with high disconnect rate",
		Duration:    200 * time.Millisecond,
		Setup: func() error {
			for _, conn := range connections {
				conn.alive.Store(true)
				conn.SetState(TestStateConnected)
			}
			return nil
		},
		Check: func() error {
			// After partition, some connections should be dead
			deadCount := 0
			for _, conn := range connections {
				if !conn.IsAlive() {
					deadCount++
				}
			}
			t.Logf("Partition result: %d/%d connections dead", deadCount, len(connections))
			return nil
		},
		Teardown: func() error {
			for _, conn := range connections {
				conn.Kill()
			}
			return nil
		},
	})

	// Run during chaos
	ctx := context.Background()
	monkey := NewChaosMonkey(10*time.Millisecond, partitionInjector)
	for _, conn := range connections {
		monkey.AddTarget(conn)
	}
	monkey.Start(ctx)
	defer monkey.Stop()

	results := runner.Run(ctx)
	if len(results) != 1 {
		t.Errorf("Expected 1 result, got %d", len(results))
	}

	result := results[0]
	t.Logf("Scenario %q: success=%v, duration=%v", result.Scenario, result.Success, result.Duration)
}

// TestChaos_PanicRecovery tests that panic faults are recovered.
func TestChaos_PanicRecovery(t *testing.T) {
	injector := NewFaultInjector(
		FaultConfig{Type: FaultPanic, Probability: 1.0},
	)

	recovered := false
	func() {
		defer func() {
			if r := recover(); r != nil {
				recovered = true
				t.Logf("Recovered from panic: %v", r)
			}
		}()
		injector.MaybePanic()
	}()

	if !recovered {
		t.Error("Should have panicked and recovered")
	}
}
