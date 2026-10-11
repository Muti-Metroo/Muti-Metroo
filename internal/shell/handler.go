package shell

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/postalsys/muti-metroo/internal/crypto"
	"github.com/postalsys/muti-metroo/internal/identity"
	"github.com/postalsys/muti-metroo/internal/logging"
	"github.com/postalsys/muti-metroo/internal/protocol"
)

// AuthVerifier checks a shell request's management signature. Returning
// nil means the request is authorized; any error rejects it. Implementations
// typically wrap crypto.VerifyManagement with the agent's configured public
// key and self ID.
type AuthVerifier func(env *crypto.AuthEnvelope) error

// DataWriter is the interface for sending data to a stream.
type DataWriter interface {
	WriteStreamData(peerID identity.AgentID, streamID uint64, data []byte, flags uint8) error
	WriteStreamClose(peerID identity.AgentID, streamID uint64) error
}

// ShellStream tracks an active shell stream.
type ShellStream struct {
	StreamID      uint64
	PeerID        identity.AgentID
	RequestID     uint64
	IsInteractive bool                // true for TTY mode
	Meta          *ShellMeta          // Metadata after first frame
	MetaReceived  bool                // true after metadata frame received
	Session       *Session            // Command session (normal mode)
	PTYSession    PTYSessionInterface // PTY session (interactive mode)
	// closed is atomic because it is set while holding h.mu (alongside the
	// map delete) but read while holding only ss.mu. Guarding one field with
	// two different locks is a data race; an atomic sidesteps the question
	// without forcing the two locks into a fixed order.
	closed     atomic.Bool
	Released   bool
	StartTime  time.Time
	sessionKey *crypto.SessionKey // E2E encryption session key
	// pumpsDone is signalled by pumpStdout/pumpStderr when their underlying
	// readers have hit EOF (i.e. the child closed its stdout/stderr fds).
	// waitForExit waits on this before sending the EXIT message so the EXIT
	// frame is never written to the stream before the final stdout/stderr
	// chunks: clients (especially the streaming-mode client) terminate
	// their read loop on EXIT, so any data still in flight after EXIT is
	// dropped.
	pumpsDone sync.WaitGroup
	// writeMu serializes Encrypt+Write pairs in writeEncrypted. Without it,
	// concurrent pumps (pumpStdout / pumpStderr / waitForExit's sendExit)
	// can interleave: goroutine A grabs nonce N, goroutine B grabs nonce
	// N+1, then B writes its frame to the wire first. The receiver sees
	// nonce N+1 first, advances its expected counter past N, and rejects
	// nonce N as "nonce too old". This was reported in real shell sessions
	// before too -- see commit 75a0ec4 which fixed an analogous race in
	// frame dispatch but not in the per-stream send path.
	writeMu sync.Mutex
	mu      sync.Mutex
}

// streamKey identifies a shell stream by its peer as well as its ID. Stream
// IDs are allocated per peer connection, so the ID alone is not a unique
// identity -- see "Stream Identification" in Architecture.md.
type streamKey struct {
	peer     identity.AgentID
	streamID uint64
}

// Handler handles shell stream operations.
type Handler struct {
	executor *Executor
	writer   DataWriter
	logger   *slog.Logger
	verify   AuthVerifier
	streams  map[streamKey]*ShellStream
	mu       sync.RWMutex
}

// NewHandler creates a new shell handler. verify MUST be non-nil for the
// handler to accept any stream -- when the agent has no signing public key
// configured, pass a verifier that always rejects.
func NewHandler(executor *Executor, writer DataWriter, verify AuthVerifier, logger *slog.Logger) *Handler {
	return &Handler{
		executor: executor,
		writer:   writer,
		logger:   logger,
		verify:   verify,
		streams:  make(map[streamKey]*ShellStream),
	}
}

// HandleStreamOpen handles a new shell stream open request.
// Returns error code and local ephemeral public key for E2E encryption.
func (h *Handler) HandleStreamOpen(peerID identity.AgentID, streamID uint64, requestID uint64, interactive bool, remoteEphemeralPub [crypto.KeySize]byte) (uint16, [crypto.KeySize]byte) {
	h.logger.Debug("shell stream open",
		logging.KeyPeerID, peerID.ShortString(),
		logging.KeyStreamID, streamID,
		logging.KeyRequestID, requestID,
		"interactive", interactive)

	var zeroKey [crypto.KeySize]byte

	// Check if shell is enabled
	if h.executor == nil || !h.executor.config.Enabled {
		return protocol.ErrShellDisabled, zeroKey
	}

	// Check for zero key (strict encryption mode - reject unencrypted connections)
	if remoteEphemeralPub == zeroKey {
		h.logger.Warn("rejecting shell stream with zero ephemeral key (encryption required)",
			logging.KeyPeerID, peerID.ShortString(),
			logging.KeyStreamID, streamID)
		return protocol.ErrGeneralFailure, zeroKey
	}

	// Generate ephemeral keypair for E2E encryption
	ephPriv, ephPub, err := crypto.GenerateEphemeralKeypair()
	if err != nil {
		h.logger.Error("failed to generate ephemeral keypair",
			logging.KeyError, err)
		return protocol.ErrGeneralFailure, zeroKey
	}

	// Compute shared secret from ECDH
	sharedSecret, err := crypto.ComputeECDH(ephPriv, remoteEphemeralPub)
	if err != nil {
		crypto.ZeroKey(&ephPriv)
		h.logger.Error("failed to compute ECDH shared secret",
			logging.KeyError, err)
		return protocol.ErrGeneralFailure, zeroKey
	}

	// Zero out ephemeral private key after computing shared secret
	crypto.ZeroKey(&ephPriv)

	// Derive session key - we are the responder (shell target)
	// Use requestID (not streamID) because streamID changes at each relay hop
	sessionKey := crypto.DeriveSessionKey(sharedSecret, requestID, remoteEphemeralPub, ephPub, false)
	crypto.ZeroKey(&sharedSecret)

	// Create shell stream entry with session key
	ss := &ShellStream{
		StreamID:      streamID,
		PeerID:        peerID,
		RequestID:     requestID,
		IsInteractive: interactive,
		MetaReceived:  false,
		StartTime:     time.Now(),
		sessionKey:    sessionKey,
	}

	h.mu.Lock()
	h.streams[streamKey{peer: peerID, streamID: streamID}] = ss
	h.mu.Unlock()

	return 0, ephPub // Success with our ephemeral public key
}

// HandleStreamData processes data for a shell stream.
func (h *Handler) HandleStreamData(peerID identity.AgentID, streamID uint64, data []byte, flags uint8) {
	h.mu.RLock()
	ss := h.streams[streamKey{peer: peerID, streamID: streamID}]
	h.mu.RUnlock()

	if ss == nil {
		return
	}

	if ss.closed.Load() {
		return
	}

	ss.mu.Lock()
	sessionKey := ss.sessionKey
	ss.mu.Unlock()

	// Decrypt data using E2E session key
	if sessionKey == nil {
		h.logger.Error("no session key for shell stream",
			logging.KeyStreamID, streamID)
		return
	}

	plaintext, err := sessionKey.DecryptStrict(data)
	if err != nil {
		h.logger.Error("failed to decrypt shell data",
			logging.KeyStreamID, streamID,
			logging.KeyError, err)
		return
	}

	// First data frame contains metadata
	if !ss.MetaReceived {
		h.handleMetadata(ss, plaintext)
		return
	}

	// Subsequent frames are messages
	h.handleMessage(ss, plaintext, flags)
}

// HandleStreamClose handles stream close.
func (h *Handler) HandleStreamClose(peerID identity.AgentID, streamID uint64) {
	h.mu.Lock()
	ss := h.streams[streamKey{peer: peerID, streamID: streamID}]
	if ss != nil {
		ss.closed.Store(true)
		delete(h.streams, streamKey{peer: peerID, streamID: streamID})
	}
	h.mu.Unlock()

	if ss == nil {
		return
	}

	h.logger.Debug("shell stream closed",
		logging.KeyStreamID, streamID,
		"duration", time.Since(ss.StartTime))

	h.releaseSession(ss)
}

// handleMetadata processes the first data frame containing metadata.
func (h *Handler) handleMetadata(ss *ShellStream, data []byte) {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	// Helper to send error and close stream.
	// Does not call releaseSession because no session is active at these
	// error points (or release was already done manually), and ss.mu is
	// already held by this function.
	fail := func(msg string, code int) {
		h.sendError(ss, msg, code)

		h.mu.Lock()
		if !ss.closed.Swap(true) {
			delete(h.streams, streamKey{peer: ss.PeerID, streamID: ss.StreamID})
		}
		h.mu.Unlock()

		h.writer.WriteStreamClose(ss.PeerID, ss.StreamID)
	}

	failSession := func(prefix string, err error) {
		fail(prefix+": "+err.Error(), 0)
	}

	msgType, payload, err := DecodeMessage(data)
	if err != nil {
		fail("invalid message format", 0)
		return
	}

	if msgType != MsgMeta {
		fail("expected META message", 0)
		return
	}

	meta, err := DecodeMeta(payload)
	if err != nil {
		fail("invalid metadata: "+err.Error(), 0)
		return
	}

	ss.Meta = meta
	ss.MetaReceived = true

	if err := h.verify(meta.Auth); err != nil {
		switch {
		case errors.Is(err, crypto.ErrAuthMissing):
			fail("management signature required", ErrCodeAuthRequired)
		default:
			fail("management signature rejected: "+err.Error(), ErrCodeInvalidCredentials)
		}
		return
	}

	ctx := context.Background()

	if ss.IsInteractive && meta.TTY != nil {
		ptySession, err := h.executor.NewPTYSession(ctx, meta)
		if err != nil {
			failSession("failed to start PTY session", err)
			return
		}

		ss.PTYSession = ptySession
		h.sendAck(ss, true, "")
		go h.pumpPTYOutput(ss)
		return
	}

	// Streaming session
	session, err := h.executor.NewSession(ctx, meta)
	if err != nil {
		failSession("failed to start session", err)
		return
	}

	// Arm drain signaling before Start so the executor's cmd.Wait goroutine
	// blocks until pumpStdout/pumpStderr have finished reading the pipes
	// (otherwise cmd.Wait closes the pipes mid-read and drops data).
	session.RequireDrain()

	if err := session.Start(); err != nil {
		h.executor.ReleaseSession()
		fail("failed to start command: "+err.Error(), 0)
		return
	}

	ss.Session = session
	h.sendAck(ss, true, "")
	ss.pumpsDone.Add(2)
	go h.pumpStdout(ss)
	go h.pumpStderr(ss)
	go h.waitForExit(ss)
}

// handleMessage processes subsequent messages after metadata.
func (h *Handler) handleMessage(ss *ShellStream, data []byte, flags uint8) {
	if len(data) == 0 {
		return
	}

	msgType, payload, err := DecodeMessage(data)
	if err != nil {
		h.logger.Debug("invalid shell message", logging.KeyStreamID, ss.StreamID)
		return
	}

	switch msgType {
	case MsgStdin:
		h.handleStdin(ss, payload)
	case MsgStdinClose:
		h.handleStdinClose(ss)
	case MsgResize:
		h.handleResize(ss, payload)
	case MsgSignal:
		h.handleSignal(ss, payload)
	default:
		h.logger.Debug("unexpected shell message type",
			logging.KeyStreamID, ss.StreamID,
			"type", MsgTypeName(msgType))
	}
}

// handleStdin writes stdin data to the session.
func (h *Handler) handleStdin(ss *ShellStream, data []byte) {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	if ss.PTYSession != nil {
		ss.PTYSession.Write(data)
	} else if ss.Session != nil {
		ss.Session.Stdin().Write(data)
	}
}

// handleStdinClose signals EOF on the child's stdin so commands waiting
// on a full input stream (cat, wc, sort, ...) can exit. PTY sessions
// deliver EOF via their own mechanism (master close / Ctrl-D) so only
// the non-PTY streaming path is handled here.
func (h *Handler) handleStdinClose(ss *ShellStream) {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	if ss.Session != nil {
		_ = ss.Session.Stdin().Close()
	}
}

// handleResize handles terminal resize.
func (h *Handler) handleResize(ss *ShellStream, data []byte) {
	rows, cols, err := DecodeResize(data)
	if err != nil {
		h.logger.Debug("invalid resize message", logging.KeyStreamID, ss.StreamID)
		return
	}

	ss.mu.Lock()
	defer ss.mu.Unlock()

	if ss.PTYSession != nil {
		ss.PTYSession.Resize(rows, cols)
	}
}

// handleSignal handles signal delivery.
func (h *Handler) handleSignal(ss *ShellStream, data []byte) {
	signum, err := DecodeSignal(data)
	if err != nil {
		h.logger.Debug("invalid signal message", logging.KeyStreamID, ss.StreamID)
		return
	}

	ss.mu.Lock()
	defer ss.mu.Unlock()

	if ss.PTYSession != nil {
		ss.PTYSession.Signal(syscall.Signal(signum))
	} else if ss.Session != nil {
		ss.Session.Signal(syscall.Signal(signum))
	}
}

// writeEncrypted encrypts data and sends it via the stream.
//
// Holds ss.writeMu so the (Encrypt -> WriteStreamData) pair is atomic per
// stream. Without serialization, two goroutines can grab consecutive nonces
// and then race on which calls WriteStreamData first, putting nonces on the
// wire out of order and tripping the receiver's nonce-monotonicity check.
func (h *Handler) writeEncrypted(ss *ShellStream, data []byte, flags uint8) error {
	if ss.sessionKey == nil {
		return nil // Should not happen, but handle gracefully
	}

	ss.writeMu.Lock()
	defer ss.writeMu.Unlock()

	ciphertext, err := ss.sessionKey.Encrypt(data)
	if err != nil {
		h.logger.Error("failed to encrypt shell data",
			logging.KeyStreamID, ss.StreamID,
			logging.KeyError, err)
		return err
	}

	return h.writer.WriteStreamData(ss.PeerID, ss.StreamID, ciphertext, flags)
}

// pumpOutput reads from a reader and sends encoded messages to the client.
// The encoder function determines the message type (stdout or stderr).
func (h *Handler) pumpOutput(ss *ShellStream, getReader func() io.Reader, encode func([]byte) []byte) {
	buf := make([]byte, 16*1024) // 16KB buffer
	for {
		ss.mu.Lock()
		session := ss.Session
		ss.mu.Unlock()

		if session == nil {
			return
		}

		reader := getReader()
		if reader == nil {
			return
		}

		n, err := reader.Read(buf)
		if n > 0 {
			h.writeEncrypted(ss, encode(buf[:n]), 0)
		}
		if err != nil {
			return
		}
	}
}

// pumpStdout reads stdout and sends it to the client.
func (h *Handler) pumpStdout(ss *ShellStream) {
	defer ss.pumpsDone.Done()
	h.pumpOutput(ss, func() io.Reader {
		if ss.Session != nil {
			return ss.Session.Stdout()
		}
		return nil
	}, EncodeStdout)
}

// pumpStderr reads stderr and sends it to the client.
func (h *Handler) pumpStderr(ss *ShellStream) {
	defer ss.pumpsDone.Done()
	h.pumpOutput(ss, func() io.Reader {
		if ss.Session != nil {
			return ss.Session.Stderr()
		}
		return nil
	}, EncodeStderr)
}

// pumpPTYOutput reads PTY output and sends it to the client.
func (h *Handler) pumpPTYOutput(ss *ShellStream) {
	buf := make([]byte, 16*1024) // 16KB buffer
	for {
		ss.mu.Lock()
		ptySession := ss.PTYSession
		ss.mu.Unlock()

		if ptySession == nil {
			return
		}

		n, err := ptySession.Read(buf)
		if n > 0 {
			// PTY output goes to stdout
			msg := EncodeStdout(buf[:n])
			h.writeEncrypted(ss, msg, 0)
		}
		if err != nil {
			if err != io.EOF {
				h.logger.Debug("PTY read error",
					logging.KeyStreamID, ss.StreamID,
					logging.KeyError, err)
			}
			break
		}
	}

	// Wait for process to exit
	ss.mu.Lock()
	ptySession := ss.PTYSession
	ss.mu.Unlock()

	if ptySession != nil {
		exitCode := ptySession.Wait()
		h.sendExit(ss, exitCode)
	}

	h.closeStream(ss)
}

// waitOrTimeout blocks on ch up to timeout, logging a warning at "what" if
// the timeout fires before ch closes.
func (h *Handler) waitOrTimeout(ch <-chan struct{}, timeout time.Duration, streamID uint64, what string) {
	select {
	case <-ch:
	case <-time.After(timeout):
		h.logger.Warn(what, logging.KeyStreamID, streamID)
	}
}

// waitForExit waits for the session to exit and sends the exit code.
//
// Pumps must drain BEFORE cmd.Wait runs: cmd.Wait closes the stdout/stderr
// pipes and any unread buffered data is dropped (per os/exec docs). The
// normal path is "child closes its end of the pipes -> pumps see EOF". A
// child that forks a grandchild inheriting the pipe fds breaks that path,
// so we also race the wait against session.Context().Done() and use
// ForceCloseOutput as the escape hatch.
func (h *Handler) waitForExit(ss *ShellStream) {
	ss.mu.Lock()
	session := ss.Session
	ss.mu.Unlock()

	if session == nil {
		return
	}

	pumpsDoneCh := make(chan struct{})
	go func() {
		ss.pumpsDone.Wait()
		close(pumpsDoneCh)
	}()

	select {
	case <-pumpsDoneCh:
	case <-session.Context().Done():
		session.ForceCloseOutput()
		h.waitOrTimeout(pumpsDoneCh, 2*time.Second, ss.StreamID,
			"waitForExit: pumps did not exit after force close")
	}

	// Reap the process. cmd.Wait on a killed process returns essentially
	// immediately, so 500ms is generous; the backstop only exists to
	// guarantee waitForExit always returns.
	session.SignalDrainDone()
	h.waitOrTimeout(session.Done(), 500*time.Millisecond, ss.StreamID,
		"waitForExit: cmd.Wait did not complete after drain")

	h.sendExit(ss, session.ExitCode())
	h.closeStream(ss)
}

// sendAck sends an ACK message.
func (h *Handler) sendAck(ss *ShellStream, success bool, errMsg string) {
	ack := &ShellAck{
		Success: success,
		Error:   errMsg,
	}
	data, err := EncodeAck(ack)
	if err != nil {
		h.logger.Error("failed to encode ack", logging.KeyError, err)
		return
	}
	h.writeEncrypted(ss, data, 0)
}

// sendError sends an error message with an optional ShellError.Code.
func (h *Handler) sendError(ss *ShellStream, errMsg string, code int) {
	shellErr := &ShellError{
		Message: errMsg,
		Code:    code,
	}
	data, err := EncodeError(shellErr)
	if err != nil {
		h.logger.Error("failed to encode error", logging.KeyError, err)
		return
	}
	h.writeEncrypted(ss, data, 0)
}

// sendExit sends an exit code message.
func (h *Handler) sendExit(ss *ShellStream, exitCode int32) {
	data := EncodeExit(exitCode)
	h.writeEncrypted(ss, data, 0)
}

// releaseSession closes the session and releases the session slot.
// Must be called without ss.mu held.
func (h *Handler) releaseSession(ss *ShellStream) {
	ss.mu.Lock()
	if !ss.Released {
		ss.Released = true
		if ss.Session != nil {
			ss.Session.Close()
			h.executor.ReleaseSession()
		}
		if ss.PTYSession != nil {
			ss.PTYSession.Close()
			h.executor.ReleaseSession()
		}
	}
	ss.mu.Unlock()
}

// closeStream closes the stream and releases the session slot.
func (h *Handler) closeStream(ss *ShellStream) {
	h.mu.Lock()
	if !ss.closed.Swap(true) {
		delete(h.streams, streamKey{peer: ss.PeerID, streamID: ss.StreamID})
	}
	h.mu.Unlock()

	h.releaseSession(ss)
	h.writer.WriteStreamClose(ss.PeerID, ss.StreamID)
}

// ActiveStreams returns the number of active shell streams.
func (h *Handler) ActiveStreams() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.streams)
}

// CloseStreamsForPeer closes every shell stream opened by peerID and returns
// how many were closed. Called when the peer disconnects: a shell stream owns
// a live process or PTY, and leaving it running for a peer that can no longer
// read its output wastes the process and lets the peer's stream IDs, which
// restart at 1 on reconnect, collide with the stale entries.
func (h *Handler) CloseStreamsForPeer(peerID identity.AgentID) int {
	h.mu.Lock()
	var closing []*ShellStream
	for key, ss := range h.streams {
		if key.peer == peerID {
			closing = append(closing, ss)
		}
	}
	h.mu.Unlock()

	for _, ss := range closing {
		h.HandleStreamClose(ss.PeerID, ss.StreamID)
	}
	return len(closing)
}

// Close closes all active streams.
func (h *Handler) Close() {
	h.mu.Lock()
	streams := make([]*ShellStream, 0, len(h.streams))
	for _, ss := range h.streams {
		streams = append(streams, ss)
	}
	h.mu.Unlock()

	for _, ss := range streams {
		h.HandleStreamClose(ss.PeerID, ss.StreamID)
	}
}
