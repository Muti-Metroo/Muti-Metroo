package stream

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/postalsys/muti-metroo/internal/crypto"
	"github.com/postalsys/muti-metroo/internal/identity"
	"github.com/postalsys/muti-metroo/internal/protocol"
)

// ============================================================================
// StreamState Tests
// ============================================================================

func TestStreamState_String(t *testing.T) {
	tests := []struct {
		state StreamState
		want  string
	}{
		{StateOpening, "OPENING"},
		{StateOpen, "OPEN"},
		{StateHalfClosedLocal, "HALF_CLOSED_LOCAL"},
		{StateHalfClosedRemote, "HALF_CLOSED_REMOTE"},
		{StateClosed, "CLOSED"},
		{StreamState(99), "UNKNOWN"},
	}

	for _, tt := range tests {
		if got := tt.state.String(); got != tt.want {
			t.Errorf("StreamState(%d).String() = %q, want %q", tt.state, got, tt.want)
		}
	}
}

// ============================================================================
// Stream Tests
// ============================================================================

func TestNewStream(t *testing.T) {
	localID, _ := identity.NewAgentID()
	remoteID, _ := identity.NewAgentID()

	s := NewStream(1, localID, remoteID, 100)

	if s == nil {
		t.Fatal("NewStream returned nil")
	}
	if s.ID != 1 {
		t.Errorf("ID = %d, want 1", s.ID)
	}
	if s.RequestID != 100 {
		t.Errorf("RequestID = %d, want 100", s.RequestID)
	}
	if s.State() != StateOpening {
		t.Errorf("Initial state = %v, want StateOpening", s.State())
	}
}

func TestStream_Open(t *testing.T) {
	localID, _ := identity.NewAgentID()
	remoteID, _ := identity.NewAgentID()

	s := NewStream(1, localID, remoteID, 100)
	s.Open()

	if s.State() != StateOpen {
		t.Errorf("State after Open = %v, want StateOpen", s.State())
	}
	if !s.IsOpen() {
		t.Error("IsOpen should return true")
	}
	if !s.CanWrite() {
		t.Error("CanWrite should return true")
	}
	if !s.CanRead() {
		t.Error("CanRead should return true")
	}
}

func TestStream_CloseWrite(t *testing.T) {
	localID, _ := identity.NewAgentID()
	remoteID, _ := identity.NewAgentID()

	s := NewStream(1, localID, remoteID, 100)
	s.Open()
	s.CloseWrite()

	if s.State() != StateHalfClosedLocal {
		t.Errorf("State = %v, want StateHalfClosedLocal", s.State())
	}
	if !s.CanRead() {
		t.Error("Should still be able to read")
	}
	if s.CanWrite() {
		t.Error("Should not be able to write")
	}
}

func TestStream_HandleRemoteFinWrite(t *testing.T) {
	localID, _ := identity.NewAgentID()
	remoteID, _ := identity.NewAgentID()

	s := NewStream(1, localID, remoteID, 100)
	s.Open()
	s.HandleRemoteFinWrite()

	if s.State() != StateHalfClosedRemote {
		t.Errorf("State = %v, want StateHalfClosedRemote", s.State())
	}
	if !s.CanWrite() {
		t.Error("Should still be able to write")
	}
	if s.CanRead() {
		t.Error("Should not be able to read")
	}
}

func TestStream_BothSidesClose(t *testing.T) {
	localID, _ := identity.NewAgentID()
	remoteID, _ := identity.NewAgentID()

	s := NewStream(1, localID, remoteID, 100)
	s.Open()

	// Local closes write first
	s.CloseWrite()
	if s.State() != StateHalfClosedLocal {
		t.Errorf("State = %v, want StateHalfClosedLocal", s.State())
	}

	// Then remote closes
	s.HandleRemoteFinWrite()
	if s.State() != StateClosed {
		t.Errorf("State = %v, want StateClosed", s.State())
	}
}

func TestStream_PushAndRead(t *testing.T) {
	localID, _ := identity.NewAgentID()
	remoteID, _ := identity.NewAgentID()

	s := NewStream(1, localID, remoteID, 100)
	s.Open()

	testData := []byte("hello world")
	if err := s.PushData(testData); err != nil {
		t.Fatalf("PushData failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	data, err := s.Read(ctx)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	if string(data) != string(testData) {
		t.Errorf("Read data = %q, want %q", data, testData)
	}

	if s.BytesRecv.Load() != uint64(len(testData)) {
		t.Errorf("BytesRecv = %d, want %d", s.BytesRecv.Load(), len(testData))
	}
}

func TestStream_ReadTimeout(t *testing.T) {
	localID, _ := identity.NewAgentID()
	remoteID, _ := identity.NewAgentID()

	s := NewStream(1, localID, remoteID, 100)
	s.Open()

	_, err := s.ReadWithTimeout(50 * time.Millisecond)
	if err == nil {
		t.Error("Expected timeout error")
	}
}

func TestStream_ReadAfterClose(t *testing.T) {
	localID, _ := identity.NewAgentID()
	remoteID, _ := identity.NewAgentID()

	s := NewStream(1, localID, remoteID, 100)
	s.Open()
	s.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := s.Read(ctx)
	if err == nil {
		t.Error("Expected error reading closed stream")
	}
}

func TestStream_PushAfterClose(t *testing.T) {
	localID, _ := identity.NewAgentID()
	remoteID, _ := identity.NewAgentID()

	s := NewStream(1, localID, remoteID, 100)
	s.Open()
	s.Close()

	err := s.PushData([]byte("test"))
	if err == nil {
		t.Error("Expected error pushing to closed stream")
	}
}

func TestStream_MultipleClose(t *testing.T) {
	localID, _ := identity.NewAgentID()
	remoteID, _ := identity.NewAgentID()

	s := NewStream(1, localID, remoteID, 100)

	// Multiple closes should not panic
	for i := 0; i < 5; i++ {
		s.Close()
	}

	if !s.IsClosed() {
		t.Error("Stream should be closed")
	}
}

func TestStream_Done(t *testing.T) {
	localID, _ := identity.NewAgentID()
	remoteID, _ := identity.NewAgentID()

	s := NewStream(1, localID, remoteID, 100)

	// Should not be closed initially
	select {
	case <-s.Done():
		t.Error("Done channel should not be closed initially")
	default:
	}

	s.Close()

	// Should be closed now
	select {
	case <-s.Done():
	default:
		t.Error("Done channel should be closed after Close()")
	}
}

func TestStream_String(t *testing.T) {
	localID, _ := identity.NewAgentID()
	remoteID, _ := identity.NewAgentID()

	s := NewStream(1, localID, remoteID, 100)
	s.DestAddr = "10.0.0.1"
	s.DestPort = 80
	s.Open()

	str := s.String()
	if str == "" {
		t.Error("String() should not be empty")
	}
}

// ============================================================================
// Manager Config Tests
// ============================================================================

func TestDefaultManagerConfig(t *testing.T) {
	cfg := DefaultManagerConfig()

	if cfg.MaxStreamsPerPeer != 1000 {
		t.Errorf("MaxStreamsPerPeer = %d, want 1000", cfg.MaxStreamsPerPeer)
	}
	if cfg.MaxStreamsTotal != 10000 {
		t.Errorf("MaxStreamsTotal = %d, want 10000", cfg.MaxStreamsTotal)
	}
	if cfg.BufferSize != 32768 {
		t.Errorf("BufferSize = %d, want 32768", cfg.BufferSize)
	}
}

// ============================================================================
// Manager Tests
// ============================================================================

func TestNewManager(t *testing.T) {
	localID, _ := identity.NewAgentID()
	cfg := DefaultManagerConfig()

	m := NewManager(cfg, localID)

	if m == nil {
		t.Fatal("NewManager returned nil")
	}
	if m.StreamCount() != 0 {
		t.Errorf("Initial StreamCount = %d, want 0", m.StreamCount())
	}
}

func TestManager_AcceptStream(t *testing.T) {
	localID, _ := identity.NewAgentID()
	remoteID, _ := identity.NewAgentID()
	cfg := DefaultManagerConfig()

	m := NewManager(cfg, localID)

	stream, err := m.AcceptStream(1, 100, remoteID, "10.0.0.1", 80)
	if err != nil {
		t.Fatalf("AcceptStream failed: %v", err)
	}

	if stream.ID != 1 {
		t.Errorf("Stream ID = %d, want 1", stream.ID)
	}
	if stream.State() != StateOpen {
		t.Errorf("Stream state = %v, want StateOpen", stream.State())
	}
	if m.StreamCount() != 1 {
		t.Errorf("StreamCount = %d, want 1", m.StreamCount())
	}
}

func TestManager_AcceptStream_MaxLimit(t *testing.T) {
	localID, _ := identity.NewAgentID()
	remoteID, _ := identity.NewAgentID()
	cfg := DefaultManagerConfig()
	cfg.MaxStreamsTotal = 2

	m := NewManager(cfg, localID)

	// Add 2 streams
	m.AcceptStream(1, 100, remoteID, "10.0.0.1", 80)
	m.AcceptStream(2, 101, remoteID, "10.0.0.2", 80)

	// 3rd should fail
	_, err := m.AcceptStream(3, 102, remoteID, "10.0.0.3", 80)
	if err == nil {
		t.Error("Expected error when max streams reached")
	}
}

func TestManager_GetStream(t *testing.T) {
	localID, _ := identity.NewAgentID()
	remoteID, _ := identity.NewAgentID()
	cfg := DefaultManagerConfig()

	m := NewManager(cfg, localID)
	m.AcceptStream(1, 100, remoteID, "10.0.0.1", 80)

	stream := m.GetStream(remoteID, 1)
	if stream == nil {
		t.Error("GetStream should return the stream")
	}

	unknown := m.GetStream(remoteID, 999)
	if unknown != nil {
		t.Error("GetStream should return nil for unknown ID")
	}
}

func TestManager_RemoveStream(t *testing.T) {
	localID, _ := identity.NewAgentID()
	remoteID, _ := identity.NewAgentID()
	cfg := DefaultManagerConfig()

	m := NewManager(cfg, localID)
	m.AcceptStream(1, 100, remoteID, "10.0.0.1", 80)

	m.RemoveStream(remoteID, 1)

	if m.StreamCount() != 0 {
		t.Errorf("StreamCount after remove = %d, want 0", m.StreamCount())
	}

	stream := m.GetStream(remoteID, 1)
	if stream != nil {
		t.Error("Stream should be removed")
	}
}

// TestManager_PushDataFullBuffer verifies that PushData never blocks the
// caller: a stream whose readBuffer is full drops the frame and reports
// ErrReadBufferFull, so the peer connection's ordered drainFrames worker
// cannot be jammed by one stalled consumer. After RemoveStream, PushData
// reports EOF.
func TestManager_PushDataFullBuffer(t *testing.T) {
	localID, _ := identity.NewAgentID()
	remoteID, _ := identity.NewAgentID()
	cfg := DefaultManagerConfig()

	m := NewManager(cfg, localID)
	m.AcceptStream(1, 100, remoteID, "10.0.0.1", 80)
	s := m.GetStream(remoteID, 1)
	if s == nil {
		t.Fatal("stream not found after AcceptStream")
	}

	// Fill the readBuffer to capacity.
	for i := 0; i < cap(s.readBuffer); i++ {
		if err := s.PushData([]byte{byte(i)}); err != nil {
			t.Fatalf("fill PushData[%d]: %v", i, err)
		}
	}

	// The next push must return immediately with ErrReadBufferFull, not block.
	done := make(chan error, 1)
	go func() {
		done <- s.PushData([]byte("overflow"))
	}()
	select {
	case err := <-done:
		if !errors.Is(err, ErrReadBufferFull) {
			t.Errorf("PushData on full buffer = %v, want ErrReadBufferFull", err)
		}
	case <-time.After(time.Second):
		t.Fatal("PushData blocked on a full buffer")
	}

	// Buffered data is still intact and readable.
	if data, err := s.ReadWithTimeout(time.Second); err != nil || len(data) != 1 {
		t.Errorf("Read after overflow = %v, %v; want buffered byte", data, err)
	}

	m.RemoveStream(remoteID, 1)

	if err := s.PushData([]byte("after-close")); err == nil {
		t.Error("PushData after RemoveStream returned nil; want non-nil (EOF)")
	}
}

func TestManager_HandleStreamData(t *testing.T) {
	localID, _ := identity.NewAgentID()
	remoteID, _ := identity.NewAgentID()
	cfg := DefaultManagerConfig()

	m := NewManager(cfg, localID)
	m.AcceptStream(1, 100, remoteID, "10.0.0.1", 80)

	testData := []byte("hello world")
	err := m.HandleStreamData(remoteID, 1, 0, testData)
	if err != nil {
		t.Fatalf("HandleStreamData failed: %v", err)
	}

	stream := m.GetStream(remoteID, 1)
	if stream.BytesRecv.Load() != uint64(len(testData)) {
		t.Errorf("BytesRecv = %d, want %d", stream.BytesRecv.Load(), len(testData))
	}
}

func TestManager_HandleStreamData_WithFinWrite(t *testing.T) {
	localID, _ := identity.NewAgentID()
	remoteID, _ := identity.NewAgentID()
	cfg := DefaultManagerConfig()

	m := NewManager(cfg, localID)
	m.AcceptStream(1, 100, remoteID, "10.0.0.1", 80)

	err := m.HandleStreamData(remoteID, 1, protocol.FlagFinWrite, []byte{})
	if err != nil {
		t.Fatalf("HandleStreamData failed: %v", err)
	}

	stream := m.GetStream(remoteID, 1)
	if stream.State() != StateHalfClosedRemote {
		t.Errorf("State = %v, want StateHalfClosedRemote", stream.State())
	}
}

func TestManager_HandleStreamData_UnknownStream(t *testing.T) {
	localID, _ := identity.NewAgentID()
	remoteID, _ := identity.NewAgentID()
	cfg := DefaultManagerConfig()

	m := NewManager(cfg, localID)

	err := m.HandleStreamData(remoteID, 999, 0, []byte("test"))
	if err == nil {
		t.Error("Expected error for unknown stream")
	}
}

func TestManager_HandleStreamClose(t *testing.T) {
	localID, _ := identity.NewAgentID()
	remoteID, _ := identity.NewAgentID()
	cfg := DefaultManagerConfig()

	m := NewManager(cfg, localID)
	m.AcceptStream(1, 100, remoteID, "10.0.0.1", 80)

	m.HandleStreamClose(remoteID, 1)

	if m.StreamCount() != 0 {
		t.Errorf("StreamCount = %d, want 0", m.StreamCount())
	}
}

func TestManager_HandleStreamReset(t *testing.T) {
	localID, _ := identity.NewAgentID()
	remoteID, _ := identity.NewAgentID()
	cfg := DefaultManagerConfig()

	resetReceived := false
	m := NewManager(cfg, localID)
	m.SetCallbacks(nil, func(s *Stream, err error) {
		resetReceived = true
	}, nil)

	m.AcceptStream(1, 100, remoteID, "10.0.0.1", 80)

	m.HandleStreamReset(remoteID, 1, protocol.ErrConnectionRefused)

	if !resetReceived {
		t.Error("Reset callback should be called")
	}
	if m.StreamCount() != 0 {
		t.Errorf("StreamCount = %d, want 0", m.StreamCount())
	}
}

func TestManager_OpenStream_Timeout(t *testing.T) {
	localID, _ := identity.NewAgentID()
	remoteID, _ := identity.NewAgentID()
	cfg := DefaultManagerConfig()

	m := NewManager(cfg, localID)

	pending := m.OpenStream(1, remoteID, "10.0.0.1", 80, 50*time.Millisecond)

	result := <-pending.ResultCh
	if result.Error == nil {
		t.Error("Expected timeout error")
	}
}

func TestManager_OpenStream_Success(t *testing.T) {
	localID, _ := identity.NewAgentID()
	remoteID, _ := identity.NewAgentID()
	cfg := DefaultManagerConfig()

	m := NewManager(cfg, localID)

	pending := m.OpenStream(1, remoteID, "10.0.0.1", 80, 1*time.Second)

	// Simulate receiving ACK
	go func() {
		time.Sleep(10 * time.Millisecond)
		var remoteEphemeral [crypto.KeySize]byte
		m.HandleStreamOpenAck(remoteID, pending.RequestID, nil, 0, remoteEphemeral)
	}()

	result := <-pending.ResultCh
	if result.Error != nil {
		t.Errorf("OpenStream failed: %v", result.Error)
	}
	if result.Stream == nil {
		t.Error("Stream should not be nil")
	}
}

func TestManager_OpenStream_Error(t *testing.T) {
	localID, _ := identity.NewAgentID()
	remoteID, _ := identity.NewAgentID()
	cfg := DefaultManagerConfig()

	m := NewManager(cfg, localID)

	pending := m.OpenStream(1, remoteID, "10.0.0.1", 80, 1*time.Second)

	// Simulate receiving error
	go func() {
		time.Sleep(10 * time.Millisecond)
		m.HandleStreamOpenErr(remoteID, pending.RequestID, protocol.ErrNoRoute, "no route")
	}()

	result := <-pending.ResultCh
	if result.Error == nil {
		t.Error("Expected error")
	}
	if result.ErrorCode != protocol.ErrNoRoute {
		t.Errorf("ErrorCode = %d, want ErrNoRoute", result.ErrorCode)
	}
}

func TestManager_GetAllStreams(t *testing.T) {
	localID, _ := identity.NewAgentID()
	remoteID, _ := identity.NewAgentID()
	cfg := DefaultManagerConfig()

	m := NewManager(cfg, localID)
	m.AcceptStream(1, 100, remoteID, "10.0.0.1", 80)
	m.AcceptStream(2, 101, remoteID, "10.0.0.2", 80)

	streams := m.GetAllStreams()
	if len(streams) != 2 {
		t.Errorf("GetAllStreams returned %d streams, want 2", len(streams))
	}
}

func TestManager_Close(t *testing.T) {
	localID, _ := identity.NewAgentID()
	remoteID, _ := identity.NewAgentID()
	cfg := DefaultManagerConfig()

	m := NewManager(cfg, localID)
	m.AcceptStream(1, 100, remoteID, "10.0.0.1", 80)
	m.AcceptStream(2, 101, remoteID, "10.0.0.2", 80)

	// Also add a pending request
	pending := m.OpenStream(3, remoteID, "10.0.0.3", 80, 10*time.Second)

	m.Close()

	if m.StreamCount() != 0 {
		t.Errorf("StreamCount after close = %d, want 0", m.StreamCount())
	}

	// Pending request should be cancelled
	result := <-pending.ResultCh
	if result.Error == nil {
		t.Error("Pending request should be cancelled")
	}
}

func TestManager_Callbacks(t *testing.T) {
	localID, _ := identity.NewAgentID()
	remoteID, _ := identity.NewAgentID()
	cfg := DefaultManagerConfig()

	openCalled := false
	closeCalled := false
	dataCalled := false

	m := NewManager(cfg, localID)
	m.SetCallbacks(
		func(s *Stream) { openCalled = true },
		func(s *Stream, err error) { closeCalled = true },
		func(s *Stream, data []byte) { dataCalled = true },
	)

	// Open
	m.AcceptStream(1, 100, remoteID, "10.0.0.1", 80)
	if !openCalled {
		t.Error("Open callback should be called")
	}

	// Data
	m.HandleStreamData(remoteID, 1, 0, []byte("test"))
	if !dataCalled {
		t.Error("Data callback should be called")
	}

	// Close
	m.HandleStreamClose(remoteID, 1)
	if !closeCalled {
		t.Error("Close callback should be called")
	}
}

// ============================================================================
// E2E Encryption Tests
// ============================================================================

func TestStream_SetSessionKey(t *testing.T) {
	localID, _ := identity.NewAgentID()
	remoteID, _ := identity.NewAgentID()

	s := NewStream(1, localID, remoteID, 100)

	// Initially no session key
	if s.GetSessionKey() != nil {
		t.Error("SessionKey should be nil initially")
	}

	// Create session key
	privA, pubA, _ := crypto.GenerateEphemeralKeypair()
	_, pubB, _ := crypto.GenerateEphemeralKeypair()
	secret, _ := crypto.ComputeECDH(privA, pubB)
	sk := crypto.DeriveSessionKey(secret, 1, pubA, pubB, true)

	s.SetSessionKey(sk)

	if s.GetSessionKey() == nil {
		t.Error("SessionKey should be set")
	}
	if s.GetSessionKey() != sk {
		t.Error("SessionKey should match")
	}
}

func TestStream_EncryptDecryptWithSessionKey(t *testing.T) {
	localID, _ := identity.NewAgentID()
	remoteID, _ := identity.NewAgentID()

	// Create two streams with matching session keys (simulating ingress and exit)
	streamIngress := NewStream(1, localID, remoteID, 100)
	streamExit := NewStream(1, remoteID, localID, 100)

	// Generate keypairs and derive session keys
	privIngress, pubIngress, _ := crypto.GenerateEphemeralKeypair()
	privExit, pubExit, _ := crypto.GenerateEphemeralKeypair()

	secretIngress, _ := crypto.ComputeECDH(privIngress, pubExit)
	secretExit, _ := crypto.ComputeECDH(privExit, pubIngress)

	skIngress := crypto.DeriveSessionKey(secretIngress, 1, pubIngress, pubExit, true) // initiator
	skExit := crypto.DeriveSessionKey(secretExit, 1, pubIngress, pubExit, false)      // responder

	streamIngress.SetSessionKey(skIngress)
	streamExit.SetSessionKey(skExit)

	// Test encryption from ingress and decryption at exit
	plaintext := []byte("Hello from ingress!")
	ciphertext, err := streamIngress.GetSessionKey().Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	decrypted, err := streamExit.GetSessionKey().Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}

	if string(decrypted) != string(plaintext) {
		t.Errorf("Decrypted = %q, want %q", decrypted, plaintext)
	}
}

func TestStream_BidirectionalEncryption(t *testing.T) {
	localID, _ := identity.NewAgentID()
	remoteID, _ := identity.NewAgentID()

	// Setup ingress and exit with matching session keys
	privIngress, pubIngress, _ := crypto.GenerateEphemeralKeypair()
	privExit, pubExit, _ := crypto.GenerateEphemeralKeypair()

	secretIngress, _ := crypto.ComputeECDH(privIngress, pubExit)
	secretExit, _ := crypto.ComputeECDH(privExit, pubIngress)

	skIngress := crypto.DeriveSessionKey(secretIngress, 42, pubIngress, pubExit, true)
	skExit := crypto.DeriveSessionKey(secretExit, 42, pubIngress, pubExit, false)

	streamIngress := NewStream(42, localID, remoteID, 100)
	streamExit := NewStream(42, remoteID, localID, 100)
	streamIngress.SetSessionKey(skIngress)
	streamExit.SetSessionKey(skExit)

	// Test ingress -> exit
	msgToExit := []byte("request data")
	encToExit, _ := streamIngress.GetSessionKey().Encrypt(msgToExit)
	decFromIngress, err := streamExit.GetSessionKey().Decrypt(encToExit)
	if err != nil {
		t.Fatalf("Exit decrypt failed: %v", err)
	}
	if string(decFromIngress) != string(msgToExit) {
		t.Errorf("ingress->exit mismatch")
	}

	// Test exit -> ingress
	msgToIngress := []byte("response data")
	encToIngress, _ := streamExit.GetSessionKey().Encrypt(msgToIngress)
	decFromExit, err := streamIngress.GetSessionKey().Decrypt(encToIngress)
	if err != nil {
		t.Fatalf("Ingress decrypt failed: %v", err)
	}
	if string(decFromExit) != string(msgToIngress) {
		t.Errorf("exit->ingress mismatch")
	}
}

// ============================================================================
// Concurrent Stream Tests
// ============================================================================

func TestManager_ConcurrentStreamAccess(t *testing.T) {
	localID, _ := identity.NewAgentID()
	remoteID, _ := identity.NewAgentID()
	cfg := DefaultManagerConfig()

	m := NewManager(cfg, localID)

	const numGoroutines = 10
	const streamsPerGoroutine = 10

	var wg sync.WaitGroup
	errCh := make(chan error, numGoroutines*streamsPerGoroutine)

	// Concurrent stream creation
	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			for i := 0; i < streamsPerGoroutine; i++ {
				streamID := uint64(goroutineID*1000 + i)
				_, err := m.AcceptStream(streamID, uint64(i), remoteID, "10.0.0.1", uint16(8000+i))
				if err != nil {
					errCh <- fmt.Errorf("AcceptStream %d: %w", streamID, err)
				}
			}
		}(g)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("Concurrent error: %v", err)
	}

	// Verify expected stream count
	expectedCount := numGoroutines * streamsPerGoroutine
	if m.StreamCount() != expectedCount {
		t.Errorf("StreamCount = %d, want %d", m.StreamCount(), expectedCount)
	}

	// Concurrent data handling
	errCh = make(chan error, numGoroutines*streamsPerGoroutine)
	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			for i := 0; i < streamsPerGoroutine; i++ {
				streamID := uint64(goroutineID*1000 + i)
				data := []byte(fmt.Sprintf("data-%d-%d", goroutineID, i))
				err := m.HandleStreamData(remoteID, streamID, 0, data)
				if err != nil {
					errCh <- fmt.Errorf("HandleStreamData %d: %w", streamID, err)
				}
			}
		}(g)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("Concurrent data error: %v", err)
	}

	// Concurrent stream close
	errCh = make(chan error, numGoroutines*streamsPerGoroutine)
	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			for i := 0; i < streamsPerGoroutine; i++ {
				streamID := uint64(goroutineID*1000 + i)
				m.HandleStreamClose(remoteID, streamID)
			}
		}(g)
	}

	wg.Wait()

	if m.StreamCount() != 0 {
		t.Errorf("StreamCount after close = %d, want 0", m.StreamCount())
	}
}

func TestStream_ConcurrentPushAndRead(t *testing.T) {
	localID, _ := identity.NewAgentID()
	remoteID, _ := identity.NewAgentID()

	s := NewStream(1, localID, remoteID, 100)
	s.Open()

	const numMessages = 100
	var wg sync.WaitGroup
	errCh := make(chan error, numMessages*2)

	// Producer. PushData drops with ErrReadBufferFull when the consumer falls
	// behind (it never blocks), so retry those drops; any other error fails.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < numMessages; i++ {
			data := []byte(fmt.Sprintf("message-%d", i))
			for {
				err := s.PushData(data)
				if err == nil {
					break
				}
				if errors.Is(err, ErrReadBufferFull) {
					time.Sleep(time.Millisecond)
					continue
				}
				errCh <- err
				return
			}
		}
	}()

	// Consumer
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < numMessages; i++ {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			_, err := s.Read(ctx)
			cancel()
			if err != nil {
				errCh <- err
				return
			}
		}
	}()

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("Concurrent push/read error: %v", err)
	}
}

func TestStream_ReadDrainsBufferBeforeEOF(t *testing.T) {
	localID, _ := identity.NewAgentID()
	remoteID, _ := identity.NewAgentID()

	s := NewStream(1, localID, remoteID, 100)
	s.Open()

	// Push 10 items then close
	const itemCount = 10
	for i := 0; i < itemCount; i++ {
		data := []byte(fmt.Sprintf("item-%d", i))
		if err := s.PushData(data); err != nil {
			t.Fatalf("PushData(%d) failed: %v", i, err)
		}
	}
	s.Close()

	// Read should return all 10 items in order before io.EOF
	for i := 0; i < itemCount; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		data, err := s.Read(ctx)
		cancel()
		if err != nil {
			t.Fatalf("Read(%d): unexpected error: %v", i, err)
		}
		expected := fmt.Sprintf("item-%d", i)
		if string(data) != expected {
			t.Errorf("Read(%d) = %q, want %q", i, data, expected)
		}
	}

	// Next read should return io.EOF
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := s.Read(ctx)
	if err != io.EOF {
		t.Errorf("Read after drain: got %v, want io.EOF", err)
	}
}

// Request IDs are a sequential counter, so they are as guessable as stream
// IDs. A STREAM_OPEN_ACK or _ERR must only be honoured from the peer the
// request was actually sent to, or any peer can complete or fail a request
// this agent addressed to another.
func TestManager_OpenAckRejectsWrongPeer(t *testing.T) {
	localID, _ := identity.NewAgentID()
	intendedPeer, _ := identity.NewAgentID()
	impostor, _ := identity.NewAgentID()

	m := NewManager(DefaultManagerConfig(), localID)
	pending := m.OpenStream(1, intendedPeer, "10.0.0.1", 80, time.Second)

	var remoteEphemeral [crypto.KeySize]byte
	if _, err := m.HandleStreamOpenAck(impostor, pending.RequestID, nil, 0, remoteEphemeral); err == nil {
		t.Error("an impostor peer completed a request addressed to another peer")
	}

	// The request must survive the rejection and still be completable by its
	// intended peer -- otherwise the check is a denial of service instead.
	if _, err := m.HandleStreamOpenAck(intendedPeer, pending.RequestID, nil, 0, remoteEphemeral); err != nil {
		t.Fatalf("intended peer could not complete its own request: %v", err)
	}
	if s := m.GetStream(intendedPeer, 1); s == nil {
		t.Error("stream was not registered after a valid ack")
	}
}

func TestManager_OpenErrRejectsWrongPeer(t *testing.T) {
	localID, _ := identity.NewAgentID()
	intendedPeer, _ := identity.NewAgentID()
	impostor, _ := identity.NewAgentID()

	m := NewManager(DefaultManagerConfig(), localID)
	pending := m.OpenStream(1, intendedPeer, "10.0.0.1", 80, time.Second)

	if err := m.HandleStreamOpenErr(impostor, pending.RequestID, protocol.ErrNoRoute, "nope"); err == nil {
		t.Error("an impostor peer failed a request addressed to another peer")
	}
	if err := m.HandleStreamOpenErr(intendedPeer, pending.RequestID, protocol.ErrNoRoute, "no route"); err != nil {
		t.Fatalf("intended peer could not fail its own request: %v", err)
	}
}

// When a peer dies, every stream opened through it must be closed so local
// readers (e.g. ingress SOCKS5 clients) get an error instead of hanging, and
// pending opens to that peer must fail immediately rather than wait out the
// open timeout.
func TestManager_CloseStreamsForPeer(t *testing.T) {
	localID, _ := identity.NewAgentID()
	deadPeer, _ := identity.NewAgentID()
	otherPeer, _ := identity.NewAgentID()
	cfg := DefaultManagerConfig()

	m := NewManager(cfg, localID)
	m.AcceptStream(1, 100, deadPeer, "10.0.0.1", 80)
	m.AcceptStream(2, 101, deadPeer, "10.0.0.2", 80)
	m.AcceptStream(1, 102, otherPeer, "10.0.0.3", 80)

	pending := m.OpenStream(3, deadPeer, "10.0.0.4", 80, time.Minute)

	n := m.CloseStreamsForPeer(deadPeer)
	if n != 2 {
		t.Errorf("CloseStreamsForPeer closed %d streams, want 2", n)
	}

	// Streams toward the dead peer are gone and closed.
	for _, id := range []uint64{1, 2} {
		if s := m.GetStream(deadPeer, id); s != nil {
			t.Errorf("stream %d still tracked after peer teardown", id)
		}
	}

	// The other peer's stream survives.
	if s := m.GetStream(otherPeer, 1); s == nil || s.IsClosed() {
		t.Error("unrelated peer's stream was closed")
	}

	// The pending open to the dead peer fails immediately.
	select {
	case res := <-pending.ResultCh:
		if res.Error == nil {
			t.Error("pending open to dead peer returned nil error")
		}
	case <-time.After(time.Second):
		t.Error("pending open to dead peer was not failed")
	}
	if m.PendingCount() != 0 {
		t.Errorf("PendingCount = %d, want 0", m.PendingCount())
	}
}

// A reader blocked in Read must wake with EOF when the stream is torn down by
// CloseStreamsForPeer.
func TestManager_CloseStreamsForPeer_UnblocksReader(t *testing.T) {
	localID, _ := identity.NewAgentID()
	deadPeer, _ := identity.NewAgentID()
	cfg := DefaultManagerConfig()

	m := NewManager(cfg, localID)
	m.AcceptStream(1, 100, deadPeer, "10.0.0.1", 80)
	s := m.GetStream(deadPeer, 1)

	readErr := make(chan error, 1)
	go func() {
		_, err := s.Read(context.Background())
		readErr <- err
	}()

	m.CloseStreamsForPeer(deadPeer)

	select {
	case err := <-readErr:
		if err == nil {
			t.Error("Read returned nil error after peer teardown")
		}
	case <-time.After(time.Second):
		t.Error("Read still blocked after peer teardown")
	}
}
