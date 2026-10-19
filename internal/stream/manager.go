// Package stream implements stream management for Muti Metroo.
package stream

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Muti-Metroo/Muti-Metroo/internal/crypto"
	"github.com/Muti-Metroo/Muti-Metroo/internal/identity"
	"github.com/Muti-Metroo/Muti-Metroo/internal/protocol"
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

	state      atomic.Int32
	mu         sync.Mutex
	readBuffer chan []byte
	closeOnce  sync.Once
	closed     chan struct{}

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

	// End-to-end encryption session key
	sessionKey *crypto.SessionKey

	// Flow control (nil when the path does not support it): sendWindow gates
	// this side's data frames, credit batches the frames this side has
	// consumed, and onCredit sends them back to the peer as STREAM_WINDOW.
	sendWindow *Window
	credit     *Credit
	onCredit   func(n uint32)

	// resetErr is set when the stream was reset rather than closed, so
	// readers fail instead of seeing a clean end of stream.
	resetErr error
}

// ResetError reports that a stream was aborted with STREAM_RESET, by the peer
// or locally, instead of closed cleanly. Data after the reset is lost, so
// readers must not treat it as an end of stream.
type ResetError struct {
	Code uint16
}

func (e *ResetError) Error() string {
	return fmt.Sprintf("stream reset (code %d)", e.Code)
}

// NewStream creates a new stream.
func NewStream(id uint64, localID, remoteID identity.AgentID, requestID uint64) *Stream {
	s := &Stream{
		ID:        id,
		LocalID:   localID,
		RemoteID:  remoteID,
		RequestID: requestID,
		// One slot per frame of the flow-control window: a sender that honors
		// the window can never overflow it.
		readBuffer:  make(chan []byte, protocol.StreamWindowFrames),
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

// ErrReadBufferFull is returned by PushData when the stream's read buffer is
// full. The data frame is dropped: keeping it would corrupt the byte stream,
// so the caller is expected to reset the stream. The buffer is drained by a
// single consumer goroutine, and PushData runs on the peer connection's
// sequential frame drainer -- a blocking send here would let one stalled
// consumer jam every stream and control frame on that connection.
var ErrReadBufferFull = errors.New("stream read buffer full")

// PushData adds data to the read buffer. It never blocks: a full buffer drops
// the frame and returns ErrReadBufferFull.
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
	default:
		return ErrReadBufferFull
	}
}

// Read reads data from the stream.
// Prioritizes reading buffered data before returning EOF on close or remote
// half-close. A reset stream returns its *ResetError instead.
func (s *Stream) Read(ctx context.Context) ([]byte, error) {
	// First, try to read any buffered data without blocking
	select {
	case data := <-s.readBuffer:
		return s.took(data), nil
	default:
		// No data immediately available, continue to blocking select
	}

	// Wait for data, close, remote half-close, or context cancellation
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.closed:
		if err := s.resetError(); err != nil {
			return nil, err
		}
		// Stream closed - drain any remaining buffered data first
		select {
		case data := <-s.readBuffer:
			return s.took(data), nil
		default:
			return nil, io.EOF
		}
	case <-s.remoteFinCh:
		// Remote half-closed - drain buffered data then return EOF
		select {
		case data := <-s.readBuffer:
			return s.took(data), nil
		default:
			return nil, io.EOF
		}
	case data := <-s.readBuffer:
		return s.took(data), nil
	}
}

// TryRead returns a buffered frame without waiting, and false when none is
// buffered.
func (s *Stream) TryRead() ([]byte, bool) {
	select {
	case data := <-s.readBuffer:
		return s.took(data), true
	default:
		return nil, false
	}
}

// took records that a frame left the read buffer and returns it. With flow
// control on, consumed frames are returned to the sender in batches.
func (s *Stream) took(data []byte) []byte {
	s.mu.Lock()
	credit, onCredit := s.credit, s.onCredit
	s.mu.Unlock()
	if credit != nil {
		if n := credit.Consumed(1); n > 0 {
			onCredit(n)
		}
	}
	return data
}

// EnableFlowControl turns on flow control for the stream: data frames this
// side sends need window (AcquireSend), and onCredit is called with the
// number of frames to grant back to the peer as this side consumes data.
// Call it before the stream is handed to its reader and writer.
func (s *Stream) EnableFlowControl(onCredit func(n uint32)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sendWindow = NewWindow(protocol.StreamWindowFrames)
	s.credit = &Credit{}
	s.onCredit = onCredit
}

// AcquireSend waits for window to send one data frame. It returns at once
// when flow control is off, and ErrWindowClosed if the stream ends first.
func (s *Stream) AcquireSend() error {
	s.mu.Lock()
	w := s.sendWindow
	s.mu.Unlock()
	if w == nil {
		return nil
	}
	return w.Acquire(s.closed)
}

// GrantSend adds n frames to the send window (a STREAM_WINDOW from the peer).
func (s *Stream) GrantSend(n int) {
	s.mu.Lock()
	w := s.sendWindow
	s.mu.Unlock()
	if w != nil {
		w.Grant(n)
	}
}

// Reset aborts the stream: readers get err instead of buffered data or EOF.
func (s *Stream) Reset(err error) {
	s.mu.Lock()
	if s.resetErr == nil {
		s.resetErr = err
	}
	s.mu.Unlock()
	s.Close()
}

func (s *Stream) resetError() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.resetErr
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
		w := s.sendWindow
		s.mu.Unlock()
		close(s.closed)
		if w != nil {
			w.Close()
		}
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

// Done returns a channel that's closed when the stream closes.
func (s *Stream) Done() <-chan struct{} {
	return s.closed
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
	// MaxStreamsTotal caps the number of concurrent streams (established plus
	// pending opens). This is the only enforced stream limit.
	MaxStreamsTotal int
}

// DefaultManagerConfig returns sensible defaults.
func DefaultManagerConfig() ManagerConfig {
	return ManagerConfig{
		MaxStreamsTotal: 10000,
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

	// windowSender delivers a STREAM_WINDOW grant for a flow-controlled
	// stream to its peer. Set once by the owner before streams are opened.
	windowSender func(peer identity.AgentID, streamID uint64, n uint32)
}

// SetWindowSender sets the function that sends STREAM_WINDOW grants for
// flow-controlled streams. Call it before opening streams.
func (m *Manager) SetWindowSender(f func(peer identity.AgentID, streamID uint64, n uint32)) {
	m.windowSender = f
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
	// Check limits. Pending opens count too: a stream only enters m.streams
	// when its ACK arrives, so a burst of concurrent opens would otherwise
	// exceed the limit while waiting in pendingRequests.
	if len(m.streams)+len(m.pendingRequests) >= m.cfg.MaxStreamsTotal {
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
// flowControl reports that the ACK carried FlagFlowControl: every hop
// supports flow control, so the stream uses it in both directions.
func (m *Manager) HandleStreamOpenAck(peer identity.AgentID, requestID uint64, boundAddr net.IP, boundPort uint16, remoteEphemeral [crypto.KeySize]byte, flowControl bool) (*Stream, error) {
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
	if flowControl && m.windowSender != nil {
		send, remote, id := m.windowSender, stream.RemoteID, stream.ID
		stream.EnableFlowControl(func(n uint32) { send(remote, id, n) })
	}
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
	}

	return nil
}

// HandleStreamClose processes a STREAM_CLOSE frame.
func (m *Manager) HandleStreamClose(peer identity.AgentID, streamID uint64) {
	m.RemoveStream(peer, streamID)
}

// HandleStreamReset processes a STREAM_RESET frame.
func (m *Manager) HandleStreamReset(peer identity.AgentID, streamID uint64, errorCode uint16) {
	m.ResetStream(peer, streamID, &ResetError{Code: errorCode})
}

// ResetStream removes a stream and fails its readers with err.
func (m *Manager) ResetStream(peer identity.AgentID, streamID uint64, err error) {
	key := streamKey{peer: peer, streamID: streamID}
	m.mu.Lock()
	stream, ok := m.streams[key]
	if ok {
		delete(m.streams, key)
	}
	m.mu.Unlock()

	if ok {
		stream.Reset(err)
	}
}

// HandleStreamWindow processes a STREAM_WINDOW frame for a stream this
// manager owns, and reports whether the stream was found.
func (m *Manager) HandleStreamWindow(peer identity.AgentID, streamID uint64, increment uint32) bool {
	stream := m.GetStream(peer, streamID)
	if stream == nil {
		return false
	}
	stream.GrantSend(int(increment))
	return true
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

	// Close outside the lock: Close fires no callbacks but readers wake up.
	for _, stream := range closing {
		stream.Close()
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
