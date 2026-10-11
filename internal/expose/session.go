package expose

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Muti-Metroo/Muti-Metroo/internal/logging"
	"github.com/Muti-Metroo/Muti-Metroo/internal/recovery"
)

// Session holds one live expose connection from a Mutiauk instance. One
// Session is created per accepted SOCKS5 CmdExpose connection and lives until
// the stream terminates.
type Session struct {
	id         string // short identifier for logs/cleanup-ownership checks
	conn       messageConn
	logger     *slog.Logger
	remoteAddr string

	// dependencies
	register keyRegistrar
	cancelFn context.CancelFunc
	rootCtx  context.Context

	writeMu sync.Mutex // serialises writes to conn

	mu         sync.Mutex
	keys       map[string]struct{}
	streams    map[uint64]*muxStream
	pending    map[uint64]chan openResult
	nextID     uint64
	closed     bool
	lastRecvNS atomic.Int64

	// heartbeat / open timeouts
	openTimeout       time.Duration
	idleHeartbeat     time.Duration
	heartbeatDeadline time.Duration
}

// keyRegistrar abstracts the agent-level registration / unregistration the
// manager hooks into. The session itself never touches the agent directly.
type keyRegistrar interface {
	register(key string, sess *Session) error
	unregister(key string, sess *Session)
	triggerAdvertise()
}

type openResult struct {
	ok  bool
	err string
}

// SessionConfig configures session timeouts and dependencies. Zero-valued
// fields use sensible defaults.
type SessionConfig struct {
	OpenTimeout       time.Duration // default 30s
	IdleHeartbeat     time.Duration // default 60s
	HeartbeatDeadline time.Duration // default 30s
	Logger            *slog.Logger
}

func (c SessionConfig) withDefaults() SessionConfig {
	if c.OpenTimeout <= 0 {
		c.OpenTimeout = 30 * time.Second
	}
	if c.IdleHeartbeat <= 0 {
		c.IdleHeartbeat = 60 * time.Second
	}
	if c.HeartbeatDeadline <= 0 {
		c.HeartbeatDeadline = 30 * time.Second
	}
	if c.Logger == nil {
		c.Logger = logging.NopLogger()
	}
	return c
}

func newSession(parent context.Context, conn messageConn, remoteAddr string, reg keyRegistrar, cfg SessionConfig) *Session {
	cfg = cfg.withDefaults()
	ctx, cancel := context.WithCancel(parent)
	idBytes := make([]byte, 6)
	_, _ = rand.Read(idBytes)
	id := fmt.Sprintf("%x", idBytes)
	s := &Session{
		id:                id,
		conn:              conn,
		logger:            cfg.Logger.With(logging.KeyComponent, "expose", "session", id),
		remoteAddr:        remoteAddr,
		register:          reg,
		cancelFn:          cancel,
		rootCtx:           ctx,
		keys:              make(map[string]struct{}),
		streams:           make(map[uint64]*muxStream),
		pending:           make(map[uint64]chan openResult),
		openTimeout:       cfg.OpenTimeout,
		idleHeartbeat:     cfg.IdleHeartbeat,
		heartbeatDeadline: cfg.HeartbeatDeadline,
	}
	s.lastRecvNS.Store(time.Now().UnixNano())
	return s
}

// Run is the session main loop. It blocks until the stream terminates,
// then performs cleanup. Returns the close error (or nil on graceful close).
func (s *Session) Run() error {
	s.logger.Info("expose session started", "remote_addr", s.remoteAddr)

	heartbeatDone := make(chan struct{})
	go func() {
		defer close(heartbeatDone)
		defer recovery.RecoverWithLog(s.logger, "expose.heartbeat")
		s.heartbeatLoop()
	}()

	err := s.readLoop()

	// Tear down. Cancel context so any blocked open waiters unblock.
	s.cancelFn()
	<-heartbeatDone
	s.cleanup()

	if err != nil && !errors.Is(err, context.Canceled) {
		s.logger.Info("expose session ended", "err", err)
	} else {
		s.logger.Info("expose session ended")
	}
	return err
}

func (s *Session) readLoop() error {
	for {
		payload, err := s.conn.ReadMessage()
		if err != nil {
			return err
		}
		s.lastRecvNS.Store(time.Now().UnixNano())
		if err := s.dispatch(payload); err != nil {
			s.logger.Warn("frame dispatch failed", logging.KeyError, err)
		}
	}
}

func (s *Session) dispatch(buf []byte) error {
	frameType, streamID, body, err := DecodeFrame(buf)
	if err != nil {
		// Treat unknown types as a soft error so a future protocol bump from
		// Mutiauk doesn't tear down the session.
		if errors.Is(err, ErrUnknownFrameType) {
			return nil
		}
		return err
	}

	switch frameType {
	case FrameRegister:
		return s.handleRegister(body)
	case FrameUnregister:
		return s.handleUnregister(body)
	case FrameOpenAck:
		return s.handleOpenAck(streamID, body)
	case FrameData:
		return s.handleData(streamID, body)
	case FrameCloseWrite:
		return s.handleCloseWrite(streamID)
	case FrameClose:
		return s.handleClose(streamID)
	case FramePing:
		return s.handlePing(body)
	case FramePong:
		return nil // lastRecv already updated
	default:
		return nil
	}
}

func (s *Session) handleRegister(body []byte) error {
	var req RegisterPayload
	if err := json.Unmarshal(body, &req); err != nil {
		return s.sendJSONFrame(FrameRegisterAck, 0, RegisterAckPayload{Results: []KeyResult{{Key: "", OK: false, Error: "invalid JSON: " + err.Error()}}})
	}

	results := make([]KeyResult, 0, len(req.Forwards))
	registered := 0
	for _, e := range req.Forwards {
		if e.Key == "" {
			results = append(results, KeyResult{OK: false, Error: "key is required"})
			continue
		}
		if err := s.register.register(e.Key, s); err != nil {
			results = append(results, KeyResult{Key: e.Key, OK: false, Error: err.Error()})
			continue
		}
		s.mu.Lock()
		s.keys[e.Key] = struct{}{}
		s.mu.Unlock()
		results = append(results, KeyResult{Key: e.Key, OK: true})
		registered++
	}
	if registered > 0 {
		s.register.triggerAdvertise()
	}
	return s.sendJSONFrame(FrameRegisterAck, 0, RegisterAckPayload{Results: results})
}

func (s *Session) handleUnregister(body []byte) error {
	var req UnregisterPayload
	if err := json.Unmarshal(body, &req); err != nil {
		return s.sendJSONFrame(FrameUnregisterAck, 0, UnregisterAckPayload{Results: []KeyResult{{Key: "", OK: false, Error: "invalid JSON: " + err.Error()}}})
	}

	results := make([]KeyResult, 0, len(req.Keys))
	removed := 0
	for _, k := range req.Keys {
		s.mu.Lock()
		_, owned := s.keys[k]
		if owned {
			delete(s.keys, k)
		}
		s.mu.Unlock()
		if !owned {
			results = append(results, KeyResult{Key: k, OK: false, Error: "key not held by this session"})
			continue
		}
		s.register.unregister(k, s)
		results = append(results, KeyResult{Key: k, OK: true})
		removed++
	}
	if removed > 0 {
		s.register.triggerAdvertise()
	}
	return s.sendJSONFrame(FrameUnregisterAck, 0, UnregisterAckPayload{Results: results})
}

func (s *Session) handleOpenAck(streamID uint64, body []byte) error {
	var ack OpenAckPayload
	if err := json.Unmarshal(body, &ack); err != nil {
		ack = OpenAckPayload{OK: false, Error: "invalid JSON: " + err.Error()}
	}
	s.mu.Lock()
	ch, ok := s.pending[streamID]
	if ok {
		delete(s.pending, streamID)
	}
	s.mu.Unlock()
	if !ok {
		return nil
	}
	select {
	case ch <- openResult{ok: ack.OK, err: ack.Error}:
	default:
	}
	return nil
}

func (s *Session) handleData(streamID uint64, payload []byte) error {
	stream := s.getStream(streamID)
	if stream == nil {
		// Stream is gone (already closed). Tell the operator.
		_ = s.sendControl(FrameClose, streamID)
		return nil
	}
	stream.deliver(payload)
	return nil
}

func (s *Session) handleCloseWrite(streamID uint64) error {
	if stream := s.getStream(streamID); stream != nil {
		stream.closeReadSide()
	}
	return nil
}

func (s *Session) handleClose(streamID uint64) error {
	if stream := s.getStream(streamID); stream != nil {
		_ = stream.tearDown(false)
	}
	return nil
}

func (s *Session) handlePing(body []byte) error {
	nonce, err := DecodeNonce(body)
	if err != nil {
		return err
	}
	return s.sendRaw(EncodeNonceFrame(FramePong, nonce))
}

func (s *Session) heartbeatLoop() {
	// Tick four times per idle window so the worst-case detection latency is
	// idleHeartbeat * 1.25 (one tick to notice idle, then heartbeatDeadline
	// to give up).
	ticker := time.NewTicker(s.idleHeartbeat / 4)
	defer ticker.Stop()

	var pingSentAt time.Time // zero when no ping is outstanding

	for {
		select {
		case <-s.rootCtx.Done():
			return
		case now := <-ticker.C:
			lastRecv := time.Unix(0, s.lastRecvNS.Load())
			if pingSentAt.IsZero() && now.Sub(lastRecv) >= s.idleHeartbeat {
				if err := s.sendRaw(EncodeNonceFrame(FramePing, randomU64())); err != nil {
					s.logger.Debug("heartbeat ping failed", logging.KeyError, err)
					_ = s.conn.Close()
					return
				}
				pingSentAt = now
			}
			if !pingSentAt.IsZero() {
				if lastRecv.After(pingSentAt) {
					pingSentAt = time.Time{}
				} else if now.Sub(pingSentAt) > s.heartbeatDeadline {
					s.logger.Info("heartbeat timeout; closing session")
					_ = s.conn.Close()
					return
				}
			}
		}
	}
}

// openStream is called by wsBackend.Open to allocate a new stream id, send
// an OPEN frame, and wait for OPEN_ACK from the operator.
func (s *Session) openStream(ctx context.Context, key, remoteAddr string) (io.ReadWriteCloser, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, errors.New("expose: session closed")
	}
	s.nextID++
	id := s.nextID
	ch := make(chan openResult, 1)
	s.pending[id] = ch
	s.mu.Unlock()

	payload, err := json.Marshal(OpenPayload{Key: key, RemoteAddr: remoteAddr})
	if err != nil {
		s.cancelPending(id)
		return nil, err
	}
	if err := s.sendRaw(EncodeFrame(FrameOpen, id, payload)); err != nil {
		s.cancelPending(id)
		return nil, err
	}

	timer := time.NewTimer(s.openTimeout)
	defer timer.Stop()

	select {
	case <-s.rootCtx.Done():
		s.cancelPending(id)
		return nil, errors.New("expose: session closed")
	case <-ctx.Done():
		s.cancelPending(id)
		return nil, ctx.Err()
	case <-timer.C:
		s.cancelPending(id)
		return nil, errors.New("expose: OPEN_ACK timeout")
	case res := <-ch:
		if !res.ok {
			msg := res.err
			if msg == "" {
				msg = "operator rejected stream"
			}
			return nil, errors.New(msg)
		}
		stream := newMuxStream(s, id)
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			return nil, errors.New("expose: session closed")
		}
		s.streams[id] = stream
		s.mu.Unlock()
		return stream, nil
	}
}

func (s *Session) cancelPending(id uint64) {
	s.mu.Lock()
	delete(s.pending, id)
	s.mu.Unlock()
}

func (s *Session) getStream(id uint64) *muxStream {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.streams[id]
}

func (s *Session) removeStream(id uint64) {
	s.mu.Lock()
	delete(s.streams, id)
	s.mu.Unlock()
}

func (s *Session) sendControl(frameType uint8, streamID uint64) error {
	return s.sendRaw(EncodeFrame(frameType, streamID, nil))
}

func (s *Session) sendData(streamID uint64, payload []byte) error {
	return s.sendRaw(EncodeFrame(FrameData, streamID, payload))
}

func (s *Session) sendJSONFrame(frameType uint8, streamID uint64, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return s.sendRaw(EncodeFrame(frameType, streamID, body))
}

func (s *Session) sendRaw(buf []byte) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.conn.WriteMessage(buf)
}

func (s *Session) cleanup() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	streams := make([]*muxStream, 0, len(s.streams))
	for _, st := range s.streams {
		streams = append(streams, st)
	}
	s.streams = make(map[uint64]*muxStream)
	keys := make([]string, 0, len(s.keys))
	for k := range s.keys {
		keys = append(keys, k)
	}
	s.keys = make(map[string]struct{})
	for id, ch := range s.pending {
		close(ch)
		delete(s.pending, id)
	}
	s.mu.Unlock()

	for _, st := range streams {
		_ = st.tearDown(false)
	}

	if len(keys) > 0 {
		for _, k := range keys {
			s.register.unregister(k, s)
		}
		s.register.triggerAdvertise()
	}
}

// muxStream is one virtual stream multiplexed over the expose connection. It
// implements io.ReadWriteCloser and the optional halfCloseWriter capability
// recognised by forward.Handler.
type muxStream struct {
	sess *Session
	id   uint64

	rxMu     sync.Mutex
	rxBuf    []byte
	rxQueue  [][]byte
	rxNotify chan struct{}
	rxClosed bool // remote sent CLOSE_WRITE or CLOSE
	closed   atomic.Bool

	closeOnce      sync.Once
	closeWriteOnce sync.Once
}

func newMuxStream(sess *Session, id uint64) *muxStream {
	return &muxStream{
		sess:     sess,
		id:       id,
		rxNotify: make(chan struct{}, 1),
	}
}

func (s *muxStream) Read(p []byte) (int, error) {
	for {
		s.rxMu.Lock()
		if len(s.rxBuf) > 0 {
			n := copy(p, s.rxBuf)
			s.rxBuf = s.rxBuf[n:]
			if len(s.rxBuf) == 0 {
				s.rxBuf = nil
			}
			s.rxMu.Unlock()
			return n, nil
		}
		if len(s.rxQueue) > 0 {
			s.rxBuf = s.rxQueue[0]
			s.rxQueue = s.rxQueue[1:]
			s.rxMu.Unlock()
			continue
		}
		if s.closed.Load() {
			s.rxMu.Unlock()
			return 0, net.ErrClosed
		}
		if s.rxClosed {
			s.rxMu.Unlock()
			return 0, io.EOF
		}
		s.rxMu.Unlock()

		select {
		case <-s.sess.rootCtx.Done():
			return 0, io.EOF
		case <-s.rxNotify:
		}
	}
}

func (s *muxStream) Write(p []byte) (int, error) {
	if s.closed.Load() {
		return 0, net.ErrClosed
	}
	written := 0
	for len(p) > 0 {
		chunk := p
		if len(chunk) > MaxDataPayload {
			chunk = chunk[:MaxDataPayload]
		}
		if err := s.sess.sendData(s.id, chunk); err != nil {
			if written > 0 {
				return written, err
			}
			return 0, err
		}
		written += len(chunk)
		p = p[len(chunk):]
	}
	return written, nil
}

// CloseWrite signals to the operator that no more bytes will arrive on this
// stream. Called by forward.Handler when it sees a FlagFinWrite from the
// upstream peer.
// The frame is sent outside closeWriteOnce for the same reason as tearDown:
// a blocking write held inside the Once stalls every other caller behind it.
func (s *muxStream) CloseWrite() error {
	var closeWriteRan bool
	s.closeWriteOnce.Do(func() {
		closeWriteRan = true
	})
	if closeWriteRan {
		return s.sess.sendControl(FrameCloseWrite, s.id)
	}
	return nil
}

// Close fully tears down the stream from the local (mesh) side, sending a
// CLOSE frame downstream and removing the stream from the session.
func (s *muxStream) Close() error {
	return s.tearDown(true)
}

// tearDown marks the stream closed, removes it from the session, and unblocks
// any pending Read. When sendClose is true, it also emits a CLOSE frame
// downstream; callers responding to a peer-initiated close or session
// shutdown set sendClose=false to skip the redundant frame.
//
// The CLOSE frame is sent outside closeOnce. sendControl blocks until the
// transport accepts the write, and the readLoop calls tearDown too when the
// peer closes first: sending inside the Once lets a local Close() park on the
// write while holding the Once, which blocks the readLoop that would otherwise
// drain the connection and let that very write complete.
func (s *muxStream) tearDown(sendClose bool) error {
	var teardownRan bool
	s.closeOnce.Do(func() {
		teardownRan = true
		s.closed.Store(true)
		s.sess.removeStream(s.id)
		s.notifyReader()
	})
	if teardownRan && sendClose {
		return s.sess.sendControl(FrameClose, s.id)
	}
	return nil
}

func (s *muxStream) closeReadSide() {
	s.rxMu.Lock()
	s.rxClosed = true
	s.rxMu.Unlock()
	s.notifyReader()
}

func (s *muxStream) deliver(payload []byte) {
	if len(payload) == 0 {
		return
	}
	cp := make([]byte, len(payload))
	copy(cp, payload)
	s.rxMu.Lock()
	s.rxQueue = append(s.rxQueue, cp)
	s.rxMu.Unlock()
	s.notifyReader()
}

func (s *muxStream) notifyReader() {
	select {
	case s.rxNotify <- struct{}{}:
	default:
	}
}

func randomU64() uint64 {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return binary.BigEndian.Uint64(b[:])
}
