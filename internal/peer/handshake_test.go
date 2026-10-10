package peer

import (
	"bytes"
	"context"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/postalsys/muti-metroo/internal/identity"
	"github.com/postalsys/muti-metroo/internal/protocol"
)

// ============================================================================
// Handshake Failure Scenario Tests
// ============================================================================

func TestDialerHandshake_WrongFrameType(t *testing.T) {
	localID, _ := identity.NewAgentID()
	remoteID, _ := identity.NewAgentID()

	h := NewHandshaker(localID, "", []string{"test"}, 1*time.Second)

	// Create a pipe to simulate connection
	clientReader, serverWriter := io.Pipe()
	serverReader, clientWriter := io.Pipe()

	cfg := DefaultConnectionConfig(localID)
	cfg.ExpectedPeerID = remoteID

	mockConn := &mockPeerConn{isDialer: true}
	conn := NewConnection(mockConn, cfg)
	defer conn.Close()

	// Create mock stream with piped data
	mockCtrlStream := &pipedMockStream{
		reader: clientReader,
		writer: clientWriter,
	}
	conn.controlStream = mockCtrlStream

	// Start dialer handshake in goroutine
	errCh := make(chan error, 1)
	go func() {
		reader := protocol.NewFrameReader(mockCtrlStream)
		writer := protocol.NewFrameWriter(mockCtrlStream)
		_, err := h.dialerHandshake(context.Background(), conn, reader, writer, remoteID)
		errCh <- err
	}()

	// Server side: read the PEER_HELLO
	serverFrameReader := protocol.NewFrameReader(serverReader)
	serverFrameWriter := protocol.NewFrameWriter(serverWriter)

	frame, err := serverFrameReader.Read()
	if err != nil {
		t.Fatalf("Failed to read PEER_HELLO: %v", err)
	}
	if frame.Type != protocol.FramePeerHello {
		t.Fatalf("Expected PEER_HELLO, got %d", frame.Type)
	}

	// Send wrong frame type (STREAM_DATA instead of PEER_HELLO_ACK)
	wrongFrame := &protocol.Frame{
		Type:     protocol.FrameStreamData,
		StreamID: protocol.ControlStreamID,
		Payload:  []byte("garbage"),
	}
	if err := serverFrameWriter.Write(wrongFrame); err != nil {
		t.Fatalf("Failed to write wrong frame: %v", err)
	}

	// Check for expected error
	select {
	case err := <-errCh:
		if err == nil {
			t.Error("Expected error for wrong frame type, got nil")
		}
		// Error should mention wrong frame type
		if err != nil && !bytes.Contains([]byte(err.Error()), []byte("expected PEER_HELLO_ACK")) {
			t.Errorf("Expected 'expected PEER_HELLO_ACK' error, got: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Handshake did not complete in time")
	}

	serverWriter.Close()
	clientWriter.Close()
}

func TestListenerHandshake_WrongFrameType(t *testing.T) {
	localID, _ := identity.NewAgentID()

	h := NewHandshaker(localID, "", []string{"test"}, 1*time.Second)

	// Create a pipe to simulate connection
	_, serverWriter := io.Pipe()
	serverReader, clientWriter := io.Pipe()

	cfg := DefaultConnectionConfig(localID)

	mockConn := &mockPeerConn{isDialer: false}
	conn := NewConnection(mockConn, cfg)
	defer conn.Close()

	// Create mock stream with piped data
	mockCtrlStream := &pipedMockStream{
		reader: serverReader,
		writer: serverWriter,
	}
	conn.controlStream = mockCtrlStream

	// Start listener handshake in goroutine
	errCh := make(chan error, 1)
	go func() {
		reader := protocol.NewFrameReader(mockCtrlStream)
		writer := protocol.NewFrameWriter(mockCtrlStream)
		_, err := h.listenerHandshake(context.Background(), conn, reader, writer, identity.AgentID{})
		errCh <- err
	}()

	// Client side: send wrong frame type
	clientFrameWriter := protocol.NewFrameWriter(clientWriter)

	wrongFrame := &protocol.Frame{
		Type:     protocol.FrameKeepalive, // Wrong type
		StreamID: protocol.ControlStreamID,
		Payload:  []byte{},
	}
	if err := clientFrameWriter.Write(wrongFrame); err != nil {
		t.Fatalf("Failed to write wrong frame: %v", err)
	}

	// Check for expected error
	select {
	case err := <-errCh:
		if err == nil {
			t.Error("Expected error for wrong frame type, got nil")
		}
		// Error should mention wrong frame type
		if err != nil && !bytes.Contains([]byte(err.Error()), []byte("expected PEER_HELLO")) {
			t.Errorf("Expected 'expected PEER_HELLO' error, got: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Handshake did not complete in time")
	}

	serverWriter.Close()
	clientWriter.Close()
}

func TestListenerHandshake_VersionMismatch(t *testing.T) {
	localID, _ := identity.NewAgentID()
	remoteID, _ := identity.NewAgentID()

	h := NewHandshaker(localID, "", []string{"test"}, 1*time.Second)

	// Create a pipe to simulate connection
	clientReader, serverWriter := io.Pipe()
	serverReader, clientWriter := io.Pipe()

	cfg := DefaultConnectionConfig(localID)

	mockConn := &mockPeerConn{isDialer: false}
	conn := NewConnection(mockConn, cfg)
	defer conn.Close()

	// Create mock stream with piped data
	mockCtrlStream := &pipedMockStream{
		reader: serverReader,
		writer: serverWriter,
	}
	conn.controlStream = mockCtrlStream

	// Start listener handshake in goroutine
	errCh := make(chan error, 1)
	go func() {
		reader := protocol.NewFrameReader(mockCtrlStream)
		writer := protocol.NewFrameWriter(mockCtrlStream)
		_, err := h.listenerHandshake(context.Background(), conn, reader, writer, identity.AgentID{})
		errCh <- err
	}()

	// Client side: send PEER_HELLO with wrong version
	clientFrameWriter := protocol.NewFrameWriter(clientWriter)

	hello := &protocol.PeerHello{
		Version:      0xFF, // Wrong version
		AgentID:      remoteID,
		Timestamp:    uint64(time.Now().UnixNano()),
		Capabilities: []string{},
	}

	frame := &protocol.Frame{
		Type:     protocol.FramePeerHello,
		StreamID: protocol.ControlStreamID,
		Payload:  hello.Encode(),
	}
	if err := clientFrameWriter.Write(frame); err != nil {
		t.Fatalf("Failed to write frame: %v", err)
	}

	// Check for expected error
	select {
	case err := <-errCh:
		if err == nil {
			t.Error("Expected error for version mismatch, got nil")
		}
		// Error should mention version mismatch
		if err != nil && !bytes.Contains([]byte(err.Error()), []byte("protocol version mismatch")) {
			t.Errorf("Expected 'protocol version mismatch' error, got: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Handshake did not complete in time")
	}

	serverWriter.Close()
	clientWriter.Close()
	clientReader.Close()
}

func TestListenerHandshake_PeerIDMismatch(t *testing.T) {
	localID, _ := identity.NewAgentID()
	remoteID, _ := identity.NewAgentID()
	expectedID, _ := identity.NewAgentID() // Different from remoteID

	h := NewHandshaker(localID, "", []string{"test"}, 1*time.Second)

	// Create a pipe to simulate connection
	clientReader, serverWriter := io.Pipe()
	serverReader, clientWriter := io.Pipe()

	cfg := DefaultConnectionConfig(localID)
	cfg.ExpectedPeerID = expectedID

	mockConn := &mockPeerConn{isDialer: false}
	conn := NewConnection(mockConn, cfg)
	defer conn.Close()

	// Create mock stream with piped data
	mockCtrlStream := &pipedMockStream{
		reader: serverReader,
		writer: serverWriter,
	}
	conn.controlStream = mockCtrlStream

	// Start listener handshake in goroutine
	errCh := make(chan error, 1)
	go func() {
		reader := protocol.NewFrameReader(mockCtrlStream)
		writer := protocol.NewFrameWriter(mockCtrlStream)
		_, err := h.listenerHandshake(context.Background(), conn, reader, writer, expectedID)
		errCh <- err
	}()

	// Client side: send PEER_HELLO with wrong peer ID
	clientFrameWriter := protocol.NewFrameWriter(clientWriter)

	hello := &protocol.PeerHello{
		Version:      protocol.ProtocolVersion,
		AgentID:      remoteID, // Different from expectedID
		Timestamp:    uint64(time.Now().UnixNano()),
		Capabilities: []string{},
	}

	frame := &protocol.Frame{
		Type:     protocol.FramePeerHello,
		StreamID: protocol.ControlStreamID,
		Payload:  hello.Encode(),
	}
	if err := clientFrameWriter.Write(frame); err != nil {
		t.Fatalf("Failed to write frame: %v", err)
	}

	// Check for expected error
	select {
	case err := <-errCh:
		if err == nil {
			t.Error("Expected error for peer ID mismatch, got nil")
		}
		// Error should mention peer ID mismatch
		if err != nil && !bytes.Contains([]byte(err.Error()), []byte("peer ID mismatch")) {
			t.Errorf("Expected 'peer ID mismatch' error, got: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Handshake did not complete in time")
	}

	serverWriter.Close()
	clientWriter.Close()
	clientReader.Close()
}

func TestDialerHandshake_PeerIDMismatch(t *testing.T) {
	localID, _ := identity.NewAgentID()
	remoteID, _ := identity.NewAgentID()
	expectedID, _ := identity.NewAgentID() // Different from remoteID

	h := NewHandshaker(localID, "", []string{"test"}, 1*time.Second)

	// Create a pipe to simulate connection
	clientReader, serverWriter := io.Pipe()
	serverReader, clientWriter := io.Pipe()

	cfg := DefaultConnectionConfig(localID)
	cfg.ExpectedPeerID = expectedID

	mockConn := &mockPeerConn{isDialer: true}
	conn := NewConnection(mockConn, cfg)
	defer conn.Close()

	// Create mock stream with piped data
	mockCtrlStream := &pipedMockStream{
		reader: clientReader,
		writer: clientWriter,
	}
	conn.controlStream = mockCtrlStream

	// Start dialer handshake in goroutine
	errCh := make(chan error, 1)
	go func() {
		reader := protocol.NewFrameReader(mockCtrlStream)
		writer := protocol.NewFrameWriter(mockCtrlStream)
		_, err := h.dialerHandshake(context.Background(), conn, reader, writer, expectedID)
		errCh <- err
	}()

	// Server side: read the PEER_HELLO and respond with wrong ID
	serverFrameReader := protocol.NewFrameReader(serverReader)
	serverFrameWriter := protocol.NewFrameWriter(serverWriter)

	_, err := serverFrameReader.Read()
	if err != nil {
		t.Fatalf("Failed to read PEER_HELLO: %v", err)
	}

	// Send PEER_HELLO_ACK with wrong peer ID
	ack := &protocol.PeerHello{
		Version:      protocol.ProtocolVersion,
		AgentID:      remoteID, // Different from expectedID
		Timestamp:    uint64(time.Now().UnixNano()),
		Capabilities: []string{},
	}

	ackFrame := &protocol.Frame{
		Type:     protocol.FramePeerHelloAck,
		StreamID: protocol.ControlStreamID,
		Payload:  ack.Encode(),
	}
	if err := serverFrameWriter.Write(ackFrame); err != nil {
		t.Fatalf("Failed to write ack: %v", err)
	}

	// Check for expected error
	select {
	case err := <-errCh:
		if err == nil {
			t.Error("Expected error for peer ID mismatch, got nil")
		}
		// Error should mention peer ID mismatch
		if err != nil && !bytes.Contains([]byte(err.Error()), []byte("peer ID mismatch")) {
			t.Errorf("Expected 'peer ID mismatch' error, got: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Handshake did not complete in time")
	}

	serverWriter.Close()
	clientWriter.Close()
}

func TestListenerHandshake_MalformedPayload(t *testing.T) {
	localID, _ := identity.NewAgentID()

	h := NewHandshaker(localID, "", []string{"test"}, 1*time.Second)

	// Create a pipe to simulate connection
	_, serverWriter := io.Pipe()
	serverReader, clientWriter := io.Pipe()

	cfg := DefaultConnectionConfig(localID)

	mockConn := &mockPeerConn{isDialer: false}
	conn := NewConnection(mockConn, cfg)
	defer conn.Close()

	// Create mock stream with piped data
	mockCtrlStream := &pipedMockStream{
		reader: serverReader,
		writer: serverWriter,
	}
	conn.controlStream = mockCtrlStream

	// Start listener handshake in goroutine
	errCh := make(chan error, 1)
	go func() {
		reader := protocol.NewFrameReader(mockCtrlStream)
		writer := protocol.NewFrameWriter(mockCtrlStream)
		_, err := h.listenerHandshake(context.Background(), conn, reader, writer, identity.AgentID{})
		errCh <- err
	}()

	// Client side: send PEER_HELLO with malformed payload
	clientFrameWriter := protocol.NewFrameWriter(clientWriter)

	frame := &protocol.Frame{
		Type:     protocol.FramePeerHello,
		StreamID: protocol.ControlStreamID,
		Payload:  []byte{0x01, 0x02, 0x03}, // Too short, malformed
	}
	if err := clientFrameWriter.Write(frame); err != nil {
		t.Fatalf("Failed to write frame: %v", err)
	}

	// Check for expected error
	select {
	case err := <-errCh:
		if err == nil {
			t.Error("Expected error for malformed payload, got nil")
		}
		// Error should mention decode failure
		if err != nil && !bytes.Contains([]byte(err.Error()), []byte("failed to decode PEER_HELLO")) {
			t.Errorf("Expected 'failed to decode PEER_HELLO' error, got: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Handshake did not complete in time")
	}

	serverWriter.Close()
	clientWriter.Close()
}

func TestDialerHandshake_MalformedAck(t *testing.T) {
	localID, _ := identity.NewAgentID()
	remoteID, _ := identity.NewAgentID()

	h := NewHandshaker(localID, "", []string{"test"}, 1*time.Second)

	// Create a pipe to simulate connection
	clientReader, serverWriter := io.Pipe()
	serverReader, clientWriter := io.Pipe()

	cfg := DefaultConnectionConfig(localID)

	mockConn := &mockPeerConn{isDialer: true}
	conn := NewConnection(mockConn, cfg)
	defer conn.Close()

	// Create mock stream with piped data
	mockCtrlStream := &pipedMockStream{
		reader: clientReader,
		writer: clientWriter,
	}
	conn.controlStream = mockCtrlStream

	// Start dialer handshake in goroutine
	errCh := make(chan error, 1)
	go func() {
		reader := protocol.NewFrameReader(mockCtrlStream)
		writer := protocol.NewFrameWriter(mockCtrlStream)
		_, err := h.dialerHandshake(context.Background(), conn, reader, writer, remoteID)
		errCh <- err
	}()

	// Server side: read the PEER_HELLO
	serverFrameReader := protocol.NewFrameReader(serverReader)
	serverFrameWriter := protocol.NewFrameWriter(serverWriter)

	_, err := serverFrameReader.Read()
	if err != nil {
		t.Fatalf("Failed to read PEER_HELLO: %v", err)
	}

	// Send malformed PEER_HELLO_ACK
	ackFrame := &protocol.Frame{
		Type:     protocol.FramePeerHelloAck,
		StreamID: protocol.ControlStreamID,
		Payload:  []byte{0xFF, 0xFE}, // Malformed
	}
	if err := serverFrameWriter.Write(ackFrame); err != nil {
		t.Fatalf("Failed to write ack: %v", err)
	}

	// Check for expected error
	select {
	case err := <-errCh:
		if err == nil {
			t.Error("Expected error for malformed ack, got nil")
		}
		// Error should mention decode failure
		if err != nil && !bytes.Contains([]byte(err.Error()), []byte("failed to decode PEER_HELLO_ACK")) {
			t.Errorf("Expected 'failed to decode PEER_HELLO_ACK' error, got: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Handshake did not complete in time")
	}

	serverWriter.Close()
	clientWriter.Close()
}

func TestHandshake_Success(t *testing.T) {
	localID, _ := identity.NewAgentID()
	remoteID, _ := identity.NewAgentID()

	dialerHandshaker := NewHandshaker(localID, "", []string{"cap1", "cap2"}, 5*time.Second)
	listenerHandshaker := NewHandshaker(remoteID, "", []string{"cap3"}, 5*time.Second)

	// Create pipes for bidirectional communication
	dialerReader, listenerWriter := io.Pipe()
	listenerReader, dialerWriter := io.Pipe()

	// Dialer side
	dialerCfg := DefaultConnectionConfig(localID)
	dialerMockConn := &mockPeerConn{isDialer: true}
	dialerConn := NewConnection(dialerMockConn, dialerCfg)
	defer dialerConn.Close()

	dialerStream := &pipedMockStream{reader: dialerReader, writer: dialerWriter}
	dialerConn.controlStream = dialerStream

	// Listener side
	listenerCfg := DefaultConnectionConfig(remoteID)
	listenerMockConn := &mockPeerConn{isDialer: false}
	listenerConn := NewConnection(listenerMockConn, listenerCfg)
	defer listenerConn.Close()

	listenerStream := &pipedMockStream{reader: listenerReader, writer: listenerWriter}
	listenerConn.controlStream = listenerStream

	// Results channels
	dialerResultCh := make(chan *HandshakeResult, 1)
	dialerErrCh := make(chan error, 1)
	listenerResultCh := make(chan *HandshakeResult, 1)
	listenerErrCh := make(chan error, 1)

	// Start both handshakes
	go func() {
		reader := protocol.NewFrameReader(dialerStream)
		writer := protocol.NewFrameWriter(dialerStream)
		result, err := dialerHandshaker.dialerHandshake(context.Background(), dialerConn, reader, writer, identity.AgentID{})
		dialerResultCh <- result
		dialerErrCh <- err
	}()

	go func() {
		reader := protocol.NewFrameReader(listenerStream)
		writer := protocol.NewFrameWriter(listenerStream)
		result, err := listenerHandshaker.listenerHandshake(context.Background(), listenerConn, reader, writer, identity.AgentID{})
		listenerResultCh <- result
		listenerErrCh <- err
	}()

	// Wait for both to complete
	timeout := time.After(5 * time.Second)

	var dialerResult, listenerResult *HandshakeResult
	var dialerErr, listenerErr error

	for i := 0; i < 2; i++ {
		select {
		case dialerResult = <-dialerResultCh:
		case dialerErr = <-dialerErrCh:
		case listenerResult = <-listenerResultCh:
		case listenerErr = <-listenerErrCh:
		case <-timeout:
			t.Fatal("Handshake timed out")
		}
	}

	// Both should complete without error
	if dialerErr != nil {
		t.Errorf("Dialer handshake failed: %v", dialerErr)
	}
	if listenerErr != nil {
		t.Errorf("Listener handshake failed: %v", listenerErr)
	}

	// Verify results
	if dialerResult != nil {
		if dialerResult.RemoteID != remoteID {
			t.Errorf("Dialer got wrong remote ID: expected %s, got %s",
				remoteID.String(), dialerResult.RemoteID.String())
		}
		if len(dialerResult.Capabilities) != 1 || dialerResult.Capabilities[0] != "cap3" {
			t.Errorf("Dialer got wrong capabilities: %v", dialerResult.Capabilities)
		}
	}

	if listenerResult != nil {
		if listenerResult.RemoteID != localID {
			t.Errorf("Listener got wrong remote ID: expected %s, got %s",
				localID.String(), listenerResult.RemoteID.String())
		}
		if len(listenerResult.Capabilities) != 2 {
			t.Errorf("Listener got wrong capabilities count: %d", len(listenerResult.Capabilities))
		}
	}

	listenerWriter.Close()
	dialerWriter.Close()
}

func TestHandshakeResult_Fields(t *testing.T) {
	remoteID, _ := identity.NewAgentID()

	result := &HandshakeResult{
		RemoteID:     remoteID,
		Capabilities: []string{"exit", "relay"},
		RTT:          100 * time.Millisecond,
	}

	if result.RemoteID != remoteID {
		t.Error("RemoteID not set correctly")
	}
	if len(result.Capabilities) != 2 {
		t.Errorf("Capabilities count = %d, want 2", len(result.Capabilities))
	}
	if result.RTT != 100*time.Millisecond {
		t.Errorf("RTT = %v, want 100ms", result.RTT)
	}
}

// ============================================================================
// Piped Mock Stream for Testing
// ============================================================================

// pipedMockStream wraps io.Pipe for testing handshakes
type pipedMockStream struct {
	reader io.Reader
	writer io.Writer
	mu     sync.Mutex
	closed bool
}

func (s *pipedMockStream) StreamID() uint64 {
	return 0
}

func (s *pipedMockStream) Read(p []byte) (int, error) {
	return s.reader.Read(p)
}

func (s *pipedMockStream) Write(p []byte) (int, error) {
	return s.writer.Write(p)
}

func (s *pipedMockStream) CloseWrite() error {
	if closer, ok := s.writer.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

func (s *pipedMockStream) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	if closer, ok := s.reader.(io.Closer); ok {
		closer.Close()
	}
	if closer, ok := s.writer.(io.Closer); ok {
		closer.Close()
	}
	return nil
}

func (s *pipedMockStream) SetDeadline(t time.Time) error {
	return nil
}

func (s *pipedMockStream) SetReadDeadline(t time.Time) error {
	return nil
}

func (s *pipedMockStream) SetWriteDeadline(t time.Time) error {
	return nil
}
