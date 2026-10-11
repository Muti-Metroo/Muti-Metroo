// Package stream implements stream management for Muti Metroo.
package stream

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/postalsys/muti-metroo/internal/crypto"
	"github.com/postalsys/muti-metroo/internal/identity"
	"github.com/postalsys/muti-metroo/internal/protocol"
)

// StreamState represents the state of a stream.
type StreamState int32

const (
	StateOpening StreamState = iota
	StateOpen
	StateHalfClosedLocal  // We sent FIN_WRITE
	StateHalfClosedRemote // Received FIN_WRITE from peer
	StateClosed
)

// String returns a human-readable state name.
func (s StreamState) String() string {
	switch s {
	case StateOpening:
		return "OPENING"
	case StateOpen:
		return "OPEN"
	case StateHalfClosedLocal:
		return "HALF_CLOSED_LOCAL"
	case StateHalfClosedRemote:
		return "HALF_CLOSED_REMOTE"
	case StateClosed:
		return "CLOSED"
	default:
		return "UNKNOWN"
	}
}

// Stream represents a virtual stream over a peer connection.
type Stream struct {
	ID        uint64
	LocalID   identity.AgentID
	RemoteID  identity.AgentID
	RequestID uint64

	state       atomic.Int32
	mu          sync.Mutex
	readBuffer  chan []byte
	writeBuffer chan []byte
	closeOnce   sync.Once
	closed      chan struct{}

	// Half-close tracking
	localFinWrite  bool
	remoteFinWrite bool
	remoteFinCh    chan struct{} // Signals remote half-close to readers

	// For request/response tracking
	DestAddr  string
	DestPort  uint16
	CreatedAt time.Time
	BytesSent atomic.Uint64
	BytesRecv atomic.Uint64

	// Callbacks
	onData  func(*Stream, []byte)
	onClose func(*Stream, error)
	onReset func(*Stream, uint16)

	// End-to-end encryption session key
	sessionKey *crypto.SessionKey
}

// NewStream creates a new stream.
func NewStream(id uint64, localID, remoteID identity.AgentID, requestID uint64) *Stream {
	s := &Stream{
		ID:          id,
		LocalID:     localID,
		RemoteID:    remoteID,
		RequestID:   requestID,
		readBuffer:  make(chan []byte, 64),
		writeBuffer: make(chan []byte, 64),
		closed:      make(chan struct{}),
		remoteFinCh: make(chan struct{}),
		CreatedAt:   time.Now(),
	}
	s.state.Store(int32(StateOpening))
	return s
}

// State returns the current stream state.
func (s *Stream) State() StreamState {
	return StreamState(s.state.Load())
}

// SetState sets the stream state.
func (s *Stream) SetState(state StreamState) {
	s.state.Store(int32(state))
}

// Open marks the stream as open.
func (s *Stream) Open() {
	s.SetState(StateOpen)
}

// IsOpen returns true if the stream is open for reading and writing.
func (s *Stream) IsOpen() bool {
	state := s.State()
	return state == StateOpen || state == StateHalfClosedLocal || state == StateHalfClosedRemote
}

// CanWrite returns true if we can write to this stream.
func (s *Stream) CanWrite() bool {
	state := s.State()
	return state == StateOpen || state == StateHalfClosedRemote
}

// CanRead returns true if we can read from this stream.
func (s *Stream) CanRead() bool {
	state := s.State()
	return state == StateOpen || state == StateHalfClosedLocal
}

// CloseWrite performs a half-close (stops writing but can still read).
func (s *Stream) CloseWrite() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.localFinWrite {
		return
	}
	s.localFinWrite = true

	state := s.State()
	if state == StateOpen {
		s.SetState(StateHalfClosedLocal)
	} else if state == StateHalfClosedRemote {
		// Both sides closed
		s.SetState(StateClosed)
	}
}

// HandleRemoteFinWrite handles FIN_WRITE from the remote side.
// This signals to readers that no more data will arrive.
func (s *Stream) HandleRemoteFinWrite() {
	s.mu.Lock()
	if s.remoteFinWrite {
		s.mu.Unlock()
		return
	}
	s.remoteFinWrite = true
	s.mu.Unlock()

	// Signal readers that remote is done writing - this causes Read() to return EOF
	close(s.remoteFinCh)

	// Update state
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.State()
	if state == StateOpen {
		s.SetState(StateHalfClosedRemote)
	} else if state == StateHalfClosedLocal {
		// Both sides closed
		s.SetState(StateClosed)
	}
}

// PushData adds data to the read buffer.
func (s *Stream) PushData(data []byte) error {
	select {
	case <-s.closed:
		return io.EOF
	default:
	}

	select {
	case s.readBuffer <- data:
		s.BytesRecv.Add(uint64(len(data)))
		return nil
	case <-s.closed:
		return io.EOF
	}
}

// Read reads data from the stream.
// Prioritizes reading buffered data before returning EOF on close or remote half-close.
func (s *Stream) Read(ctx context.Context) ([]byte, error) {
	// First, try to read any buffered data without blocking
	select {
	case data := <-s.readBuffer:
		return data, nil
	default:
		// No data immediately available, continue to blocking select
	}

	// Wait for data, close, remote half-close, or context cancellation
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.closed:
		// Stream closed - drain any remaining buffered data first
		select {
		case data := <-s.readBuffer:
			return data, nil
		default:
			return nil, io.EOF
		}
	case <-s.remoteFinCh:
		// Remote half-closed - drain buffered data then return EOF
		select {
		case data := <-s.readBuffer:
			return data, nil
		default:
			return nil, io.EOF
		}
	case data := <-s.readBuffer:
		return data, nil
	}
}

// ReadWithTimeout reads with a timeout.
func (s *Stream) ReadWithTimeout(timeout time.Duration) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return s.Read(ctx)
}

// Close closes the stream.
func (s *Stream) Close() error {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.SetState(StateClosed)
		s.mu.Unlock()
		close(s.closed)
		// Do not drain readBuffer: Read() consumes remaining data before
		// returning io.EOF. Draining here would race with concurrent readers.
	})
	return nil
}

// IsClosed returns true if the stream is closed.
func (s *Stream) IsClosed() bool {
	select {
	case <-s.closed:
		return true
	default:
		return false
	}
}

// ReadBuffer returns the read buffer channel for direct access.
// Use this sparingly - prefer using Read() instead.
func (s *Stream) ReadBuffer() <-chan []byte {
	return s.readBuffer
}

// Done returns a channel that's closed when the stream closes.
func (s *Stream) Done() <-chan struct{} {
	return s.closed
}

// SetCallbacks sets the stream callbacks.
func (s *Stream) SetCallbacks(onData func(*Stream, []byte), onClose func(*Stream, error), onReset func(*Stream, uint16)) {
	s.onData = onData
	s.onClose = onClose
	s.onReset = onReset
}

// SetSessionKey sets the encryption session key for this stream.
func (s *Stream) SetSessionKey(key *crypto.SessionKey) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessionKey = key
}

// GetSessionKey returns the encryption session key for this stream.
func (s *Stream) GetSessionKey() *crypto.SessionKey {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessionKey
}

// String returns a debug representation.
func (s *Stream) String() string {
	return fmt.Sprintf("Stream{id=%d, state=%s, dest=%s:%d}",
		s.ID, s.State(), s.DestAddr, s.DestPort)
}

// ============================================================================
// Stream Manager
// ============================================================================

// ManagerConfig contains configuration for the stream manager.
type ManagerConfig struct {
	MaxStreamsPerPeer int
	MaxStreamsTotal   int
	BufferSize        int
	IdleTimeout       time.Duration
}

// DefaultManagerConfig returns sensible defaults.
func DefaultManagerConfig() ManagerConfig {
	return ManagerConfig{
		MaxStreamsPerPeer: 1000,
		MaxStreamsTotal:   10000,
		BufferSize:        32768,
		IdleTimeout:       5 * time.Minute,
	}
}

// PendingRequest represents an awaiting STREAM_OPEN_ACK.
type PendingRequest struct {
	Stream    *Stream
	Timer     *time.Timer
	Result    chan<- *StreamOpenResult
	CreatedAt time.Time

	// Ephemeral keys for E2E encryption key exchange
	EphemeralPrivate [crypto.KeySize]byte
	EphemeralPublic  [crypto.KeySize]byte
}

// StreamOpenResult contains the result of opening a stream.
type StreamOpenResult struct {
	Stream    *Stream
	Error     error
	ErrorCode uint16
	BoundIP   net.IP
	BoundPort uint16

	// RemoteEphemeral is the exit node's ephemeral public key for E2E encryption
	RemoteEphemeral [crypto.KeySize]byte
}

// streamKey identifies a stream by its peer as well as its ID. Stream IDs are
// allocated per peer connection, so an agent with two peers hands out ID 1 on
// each: keying on the ID alone would let a stream opened via one next hop
// displace a stream opened via another, and let either peer's frames land on
// the other's stream.
type streamKey struct {
	peer     identity.AgentID
	streamID uint64
}

// Manager manages streams for a peer connection.
type Manager struct {
	cfg     ManagerConfig
	localID identity.AgentID

	mu              sync.RWMutex
	streams         map[streamKey]*Stream
	pendingRequests map[uint64]*PendingRequest
	nextRequestID   atomic.Uint64

	// Callbacks
	onStreamOpen  func(*Stream)
	onStreamClose func(*Stream, error)
	onStreamData  func(*Stream, []byte)
}

// NewManager creates a new stream manager.
func NewManager(cfg ManagerConfig, localID identity.AgentID) *Manager {
	return &Manager{
		cfg:             cfg,
		localID:         localID,
		streams:         make(map[streamKey]*Stream),
		pendingRequests: make(map[uint64]*PendingRequest),
	}
}

// SetCallbacks sets the manager's callbacks.
func (m *Manager) SetCallbacks(onOpen func(*Stream), onClose func(*Stream, error), onData func(*Stream, []byte)) {
	m.onStreamOpen = onOpen
	m.onStreamClose = onClose
	m.onStreamData = onData
}

// OpenStreamResult contains the result channel and request ID for opening a stream.
type OpenStreamPending struct {
	ResultCh  <-chan *StreamOpenResult
	RequestID uint64
}

// OpenStream initiates opening a stream and returns the pending info.
func (m *Manager) OpenStream(streamID uint64, remoteID identity.AgentID, destAddr string, destPort uint16, timeout time.Duration) *OpenStreamPending {
	resultCh := make(chan *StreamOpenResult, 1)

	requestID := m.nextRequestID.Add(1)
	stream := NewStream(streamID, m.localID, remoteID, requestID)
	stream.DestAddr = destAddr
	stream.DestPort = destPort

	m.mu.Lock()
	// Check limits
	if len(m.streams) >= m.cfg.MaxStreamsTotal {
		m.mu.Unlock()
		resultCh <- &StreamOpenResult{Error: fmt.Errorf("max streams limit reached")}
		return &OpenStreamPending{ResultCh: resultCh, RequestID: requestID}
	}

	timer := time.AfterFunc(timeout, func() {
		m.handleRequestTimeout(requestID)
	})

	m.pendingRequests[requestID] = &PendingRequest{
		Stream:    stream,
		Timer:     timer,
		Result:    resultCh,
		CreatedAt: time.Now(),
	}
	m.mu.Unlock()

	return &OpenStreamPending{ResultCh: resultCh, RequestID: requestID}
}

// handleRequestTimeout handles a timed-out stream open request.
func (m *Manager) handleRequestTimeout(requestID uint64) {
	m.mu.Lock()
	pending, ok := m.pendingRequests[requestID]
	if ok {
		delete(m.pendingRequests, requestID)
	}
	m.mu.Unlock()

	if ok {
		crypto.ZeroKey(&pending.EphemeralPrivate)
		if pending.Result != nil {
			pending.Result <- &StreamOpenResult{
				Error:     fmt.Errorf("stream open timeout"),
				ErrorCode: protocol.ErrConnectionTimeout,
			}
		}
	}
}

// HandleStreamOpenAck processes a STREAM_OPEN_ACK frame. peer is the peer the
// frame arrived from; it must be the peer the request was sent to, or any peer
// could complete a request this agent addressed to a different one. Request IDs
// are a sequential counter, so they are as guessable as stream IDs.
func (m *Manager) HandleStreamOpenAck(peer identity.AgentID, requestID uint64, boundAddr net.IP, boundPort uint16, remoteEphemeral [crypto.KeySize]byte) (*Stream, error) {
	m.mu.Lock()
	pending, ok := m.pendingRequests[requestID]
	if !ok {
		m.mu.Unlock()
		return nil, fmt.Errorf("no pending request with ID %d", requestID)
	}
	if pending.Stream.RemoteID != peer {
		m.mu.Unlock()
		return nil, fmt.Errorf("request %d belongs to peer %s, not %s",
			requestID, pending.Stream.RemoteID.ShortString(), peer.ShortString())
	}
	delete(m.pendingRequests, requestID)
	pending.Timer.Stop()
	crypto.ZeroKey(&pending.EphemeralPrivate)

	stream := pending.Stream
	stream.Open()
	m.streams[streamKey{peer: stream.RemoteID, streamID: stream.ID}] = stream
	m.mu.Unlock()

	// Send success result with ephemeral key for key exchange
	pending.Result <- &StreamOpenResult{
		Stream:          stream,
		BoundIP:         boundAddr,
		BoundPort:       boundPort,
		RemoteEphemeral: remoteEphemeral,
	}

	// Notify callback
	if m.onStreamOpen != nil {
		m.onStreamOpen(stream)
	}

	return stream, nil
}

// HandleStreamOpenErr processes a STREAM_OPEN_ERR frame. peer must match the
// peer the request was sent to, for the same reason as HandleStreamOpenAck:
// otherwise any peer could fail a request addressed to another.
func (m *Manager) HandleStreamOpenErr(peer identity.AgentID, requestID uint64, errorCode uint16, message string) error {
	m.mu.Lock()
	pending, ok := m.pendingRequests[requestID]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("no pending request with ID %d", requestID)
	}
	if pending.Stream.RemoteID != peer {
		m.mu.Unlock()
		return fmt.Errorf("request %d belongs to peer %s, not %s",
			requestID, pending.Stream.RemoteID.ShortString(), peer.ShortString())
	}
	delete(m.pendingRequests, requestID)
	pending.Timer.Stop()
	crypto.ZeroKey(&pending.EphemeralPrivate)
	m.mu.Unlock()

	// Return *protocol.DialError so callers (e.g. SOCKS5) can recover the
	// mesh ErrorCode via errors.As.
	pending.Result <- &StreamOpenResult{
		Error:     &protocol.DialError{ErrorCode: errorCode, Message: message},
		ErrorCode: errorCode,
	}

	return nil
}

// CancelPendingRequest cancels a pending stream open request.
// This is called when the client disconnects before the stream is established.
// Returns true if the request was found and cancelled.
func (m *Manager) CancelPendingRequest(requestID uint64) bool {
	m.mu.Lock()
	pending, ok := m.pendingRequests[requestID]
	if !ok {
		m.mu.Unlock()
		return false
	}
	delete(m.pendingRequests, requestID)
	pending.Timer.Stop()
	m.mu.Unlock()

	crypto.ZeroKey(&pending.EphemeralPrivate)

	// Send cancellation result (non-blocking in case channel is already closed)
	select {
	case pending.Result <- &StreamOpenResult{
		Error:     fmt.Errorf("stream open cancelled"),
		ErrorCode: protocol.ErrConnectionTimeout,
	}:
	default:
	}

	return true
}

// AcceptStream accepts an incoming stream.
func (m *Manager) AcceptStream(streamID uint64, requestID uint64, remoteID identity.AgentID, destAddr string, destPort uint16) (*Stream, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.streams) >= m.cfg.MaxStreamsTotal {
		return nil, fmt.Errorf("max streams limit reached")
	}

	stream := NewStream(streamID, m.localID, remoteID, requestID)
	stream.DestAddr = destAddr
	stream.DestPort = destPort
	stream.Open()

	m.streams[streamKey{peer: remoteID, streamID: streamID}] = stream

	if m.onStreamOpen != nil {
		m.onStreamOpen(stream)
	}

	return stream, nil
}

// GetStream returns a stream by peer and ID.
func (m *Manager) GetStream(peer identity.AgentID, streamID uint64) *Stream {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.streams[streamKey{peer: peer, streamID: streamID}]
}

// RemoveStream removes a stream.
func (m *Manager) RemoveStream(peer identity.AgentID, streamID uint64) {
	key := streamKey{peer: peer, streamID: streamID}
	m.mu.Lock()
	stream, ok := m.streams[key]
	if ok {
		delete(m.streams, key)
	}
	m.mu.Unlock()

	if ok {
		stream.Close()
		if m.onStreamClose != nil {
			m.onStreamClose(stream, nil)
		}
	}
}

// HandleStreamData processes incoming stream data.
func (m *Manager) HandleStreamData(peer identity.AgentID, streamID uint64, flags uint8, data []byte) error {
	m.mu.RLock()
	stream := m.streams[streamKey{peer: peer, streamID: streamID}]
	m.mu.RUnlock()

	if stream == nil {
		return fmt.Errorf("unknown stream %d", streamID)
	}

	// Handle FIN flags
	if flags&protocol.FlagFinWrite != 0 {
		stream.HandleRemoteFinWrite()
	}

	if len(data) > 0 {
		if err := stream.PushData(data); err != nil {
			return err
		}

		if m.onStreamData != nil {
			m.onStreamData(stream, data)
		}
	}

	return nil
}

// HandleStreamClose processes a STREAM_CLOSE frame.
func (m *Manager) HandleStreamClose(peer identity.AgentID, streamID uint64) {
	m.RemoveStream(peer, streamID)
}

// HandleStreamReset processes a STREAM_RESET frame.
func (m *Manager) HandleStreamReset(peer identity.AgentID, streamID uint64, errorCode uint16) {
	key := streamKey{peer: peer, streamID: streamID}
	m.mu.Lock()
	stream, ok := m.streams[key]
	if ok {
		delete(m.streams, key)
	}
	m.mu.Unlock()

	if ok {
		stream.Close()
		if stream.onReset != nil {
			stream.onReset(stream, errorCode)
		}
		if m.onStreamClose != nil {
			m.onStreamClose(stream, fmt.Errorf("stream reset: code=%d", errorCode))
		}
	}
}

// CloseStreamsForPeer closes every stream opened through the given peer and
// fails its pending stream-open requests. Called when the peer connection
// dies: the streams' next hop is gone, so without this their readers (e.g.
// ingress SOCKS5 clients) would block until their own timeout, and the
// peer's stream IDs restart at 1 on reconnect and collide with the stale
// entries. No STREAM_CLOSE is sent -- the peer is gone anyway.
func (m *Manager) CloseStreamsForPeer(peer identity.AgentID) int {
	m.mu.Lock()
	var closing []*Stream
	for key, stream := range m.streams {
		if key.peer != peer {
			continue
		}
		delete(m.streams, key)
		closing = append(closing, stream)
	}
	var pending []*PendingRequest
	for requestID, preq := range m.pendingRequests {
		if preq.Stream.RemoteID != peer {
			continue
		}
		delete(m.pendingRequests, requestID)
		preq.Timer.Stop()
		pending = append(pending, preq)
	}
	m.mu.Unlock()

	// Close outside the lock: Close fires no callbacks but readers wake up,
	// and onStreamClose may call back into the manager.
	for _, stream := range closing {
		stream.Close()
		if m.onStreamClose != nil {
			m.onStreamClose(stream, fmt.Errorf("peer %s disconnected", peer.ShortString()))
		}
	}
	for _, preq := range pending {
		crypto.ZeroKey(&preq.EphemeralPrivate)
		if preq.Result != nil {
			preq.Result <- &StreamOpenResult{
				Error:     fmt.Errorf("peer %s disconnected", peer.ShortString()),
				ErrorCode: protocol.ErrHostUnreachable,
			}
		}
	}
	return len(closing)
}

// StreamCount returns the number of active streams.
func (m *Manager) StreamCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.streams)
}

// PendingCount returns the number of pending requests.
func (m *Manager) PendingCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.pendingRequests)
}

// GetAllStreams returns all active streams.
func (m *Manager) GetAllStreams() []*Stream {
	m.mu.RLock()
	defer m.mu.RUnlock()

	streams := make([]*Stream, 0, len(m.streams))
	for _, s := range m.streams {
		streams = append(streams, s)
	}
	return streams
}

// Close closes all streams.
func (m *Manager) Close() {
	m.mu.Lock()
	// Cancel all pending requests
	for _, pending := range m.pendingRequests {
		pending.Timer.Stop()
		crypto.ZeroKey(&pending.EphemeralPrivate)
		pending.Result <- &StreamOpenResult{Error: fmt.Errorf("manager closed")}
	}
	m.pendingRequests = make(map[uint64]*PendingRequest)

	// Close all streams
	for key, stream := range m.streams {
		stream.Close()
		delete(m.streams, key)
	}
	m.mu.Unlock()
}

// NextRequestID returns the next available request ID.
func (m *Manager) NextRequestID() uint64 {
	return m.nextRequestID.Add(1)
}

// GetPendingEphemeralKeys returns the ephemeral keys for a pending request.
// Returns the private key, public key, and a boolean indicating if the request was found.
func (m *Manager) GetPendingEphemeralKeys(requestID uint64) ([crypto.KeySize]byte, [crypto.KeySize]byte, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	pending, ok := m.pendingRequests[requestID]
	if !ok {
		var zeroKey [crypto.KeySize]byte
		return zeroKey, zeroKey, false
	}

	return pending.EphemeralPrivate, pending.EphemeralPublic, true
}

// SetPendingEphemeralKeys sets the ephemeral keys for a pending request.
func (m *Manager) SetPendingEphemeralKeys(requestID uint64, privateKey, publicKey [crypto.KeySize]byte) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	pending, ok := m.pendingRequests[requestID]
	if !ok {
		return false
	}

	pending.EphemeralPrivate = privateKey
	pending.EphemeralPublic = publicKey
	return true
}
