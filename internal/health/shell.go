package health

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"time"

	"nhooyr.io/websocket"

	"github.com/postalsys/muti-metroo/internal/crypto"
	"github.com/postalsys/muti-metroo/internal/identity"
	"github.com/postalsys/muti-metroo/internal/protocol"
	"github.com/postalsys/muti-metroo/internal/shell"
)

// ShellProvider provides shell session functionality.
type ShellProvider interface {
	// OpenShellStream opens a shell stream to a remote agent.
	// Returns a ShellSession that can be used to communicate with the remote agent.
	OpenShellStream(ctx context.Context, targetID identity.AgentID, meta *shell.ShellMeta, interactive bool) (*ShellSession, error)
}

// ShellSession represents an active shell session with a remote agent.
type ShellSession struct {
	StreamID uint64
	TargetID identity.AgentID

	// Channels for bidirectional communication
	Send    chan []byte // Send data to remote
	Receive chan []byte // Receive data from remote

	// Done channel closes when session ends
	Done     chan struct{}
	ExitCode int32
	Error    error

	// Cleanup function
	Close func()
}

// handleShellWebSocket handles WebSocket connections for shell sessions.
// GET /agents/{agent-id}/shell?mode=stream|tty
func (s *Server) handleShellWebSocket(w http.ResponseWriter, r *http.Request, targetID identity.AgentID) {
	if s.shellProvider == nil {
		http.Error(w, "shell not available", http.StatusServiceUnavailable)
		return
	}

	// Disable write deadline for long-lived WebSocket connections.
	// The default HTTP WriteTimeout (10s) causes the write side to close
	// after inactivity, breaking bidirectional shell communication.
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Time{}) // Zero time = no deadline

	// Parse mode from query params
	mode := r.URL.Query().Get("mode")
	interactive := mode != "stream" // Default to interactive (TTY)

	// Accept WebSocket connection
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		Subprotocols: []string{"muti-metroo-shell"},
	})
	if err != nil {
		http.Error(w, "failed to accept websocket: "+err.Error(), http.StatusBadRequest)
		return
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	ctx := r.Context()

	// Read initial metadata from client
	_, metaData, err := conn.Read(ctx)
	if err != nil {
		conn.Close(websocket.StatusProtocolError, "failed to read metadata")
		return
	}

	// Decode message type and payload (client sends MsgMeta prefix)
	msgType, payload, err := shell.DecodeMessage(metaData)
	if err != nil {
		conn.Close(websocket.StatusProtocolError, "failed to decode message: "+err.Error())
		return
	}
	if msgType != shell.MsgMeta {
		conn.Close(websocket.StatusProtocolError, "expected META message")
		return
	}

	// Parse metadata from payload
	meta, err := shell.DecodeMeta(payload)
	if err != nil {
		conn.Close(websocket.StatusProtocolError, "invalid metadata: "+err.Error())
		return
	}

	// Open shell stream to target agent
	session, err := s.shellProvider.OpenShellStream(ctx, targetID, meta, interactive)
	if err != nil {
		// Send error response
		errResp := shell.ShellError{Message: err.Error()}
		errData, _ := json.Marshal(errResp)
		conn.Write(ctx, websocket.MessageBinary, shell.EncodeMessage(shell.MsgError, errData))
		conn.Close(websocket.StatusInternalError, "failed to open shell")
		return
	}
	defer session.Close()

	// Wait for the remote agent's ACK or ERROR response (first message from remote)
	// This ensures we don't tell the client "success" before remote auth is verified
	select {
	case <-ctx.Done():
		conn.Close(websocket.StatusGoingAway, "context cancelled")
		return
	case <-session.Done:
		// Session ended before we got ACK - send error
		errResp := shell.ShellError{Message: "shell session ended unexpectedly"}
		errData, _ := json.Marshal(errResp)
		conn.Write(ctx, websocket.MessageBinary, shell.EncodeMessage(shell.MsgError, errData))
		conn.Close(websocket.StatusInternalError, "session ended")
		return
	case firstMsg, ok := <-session.Receive:
		if !ok {
			errResp := shell.ShellError{Message: "shell session closed"}
			errData, _ := json.Marshal(errResp)
			conn.Write(ctx, websocket.MessageBinary, shell.EncodeMessage(shell.MsgError, errData))
			conn.Close(websocket.StatusInternalError, "session closed")
			return
		}
		// Forward the remote agent's response (ACK or ERROR) to WebSocket client
		if err := conn.Write(ctx, websocket.MessageBinary, firstMsg); err != nil {
			return
		}
		// Check if it was an error - if so, we're done
		if len(firstMsg) > 0 {
			msgType, _, _ := shell.DecodeMessage(firstMsg)
			if msgType == shell.MsgError {
				conn.Close(websocket.StatusNormalClosure, "")
				return
			}
		}
	}

	// Create context that cancels when either side closes
	sessionCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup

	// Goroutine: WebSocket -> Remote Agent (stdin)
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer cancel()
		for {
			select {
			case <-sessionCtx.Done():
				return
			default:
			}

			_, data, err := conn.Read(sessionCtx)
			if err != nil {
				return
			}

			select {
			case session.Send <- data:
			case <-sessionCtx.Done():
				return
			case <-session.Done:
				return
			}
		}
	}()

	// Goroutine: Remote Agent -> WebSocket (stdout/stderr)
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer cancel()
		for {
			select {
			case <-sessionCtx.Done():
				return
			case data, ok := <-session.Receive:
				if !ok {
					return
				}
				if err := conn.Write(sessionCtx, websocket.MessageBinary, data); err != nil {
					return
				}
			case <-session.Done:
				// Drain remaining data before exiting.
				for {
					select {
					case data, ok := <-session.Receive:
						if !ok {
							return
						}
						conn.Write(sessionCtx, websocket.MessageBinary, data)
					default:
						return
					}
				}
			}
		}
	}()

	// Wait for session to complete
	select {
	case <-session.Done:
	case <-sessionCtx.Done():
	}

	// Wait for goroutines with timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
	}
}

// SetShellProvider sets the shell session provider.
func (s *Server) SetShellProvider(provider ShellProvider) {
	s.shellProvider = provider
}

// PeerSender is an interface for sending frames to peers.
type PeerSender interface {
	SendToPeer(peerID identity.AgentID, frame *protocol.Frame) error
}

// ShellStreamAdapter adapts the mesh shell stream to the ShellSession interface.
// This is implemented by the agent.
type ShellStreamAdapter struct {
	streamID       uint64
	targetID       identity.AgentID
	send           chan []byte
	receive        chan []byte
	done           chan struct{}
	exitCode       int32
	err            error
	closeFunc      func()
	nextHop        identity.AgentID
	peerSender     PeerSender
	sessionKey     *crypto.SessionKey // E2E encryption session key
	closedByRemote bool               // set when the remote initiated the close so Close() skips the echo
	mu             sync.Mutex
}

// NewShellStreamAdapter creates a new shell stream adapter.
func NewShellStreamAdapter(streamID uint64, targetID identity.AgentID, closeFunc func()) *ShellStreamAdapter {
	return &ShellStreamAdapter{
		streamID:  streamID,
		targetID:  targetID,
		send:      make(chan []byte, 64),
		receive:   make(chan []byte, 64),
		done:      make(chan struct{}),
		closeFunc: closeFunc,
	}
}

// ToSession converts the adapter to a ShellSession.
func (a *ShellStreamAdapter) ToSession() *ShellSession {
	return &ShellSession{
		StreamID: a.streamID,
		TargetID: a.targetID,
		Send:     a.send,
		Receive:  a.receive,
		Done:     a.done,
		Close: func() {
			a.Close()
		},
	}
}

// PushReceive pushes data to the receive channel (called by stream handler).
func (a *ShellStreamAdapter) PushReceive(data []byte) {
	select {
	case a.receive <- data:
	case <-a.done:
	case <-time.After(100 * time.Millisecond):
		// Buffer full after brief wait - drop data
	}
}

// PopSend pops data from the send channel (called by stream handler).
func (a *ShellStreamAdapter) PopSend() ([]byte, bool) {
	select {
	case data := <-a.send:
		return data, true
	case <-a.done:
		return nil, false
	}
}

// SetExitCode sets the exit code when the remote process exits.
func (a *ShellStreamAdapter) SetExitCode(code int32) {
	a.mu.Lock()
	a.exitCode = code
	a.mu.Unlock()
}

// SetError sets an error.
func (a *ShellStreamAdapter) SetError(err error) {
	a.mu.Lock()
	a.err = err
	a.mu.Unlock()
}

// Close closes the adapter. When the local side initiated the close (CLI exit,
// WebSocket disconnect, error) it emits a STREAM_CLOSE to the remote peer so
// the shell handler there tears down its PTY pump. When reacting to an
// incoming STREAM_CLOSE (or a peer disconnect), call CloseFromRemote instead
// to avoid echoing a STREAM_CLOSE back.
func (a *ShellStreamAdapter) Close() {
	a.mu.Lock()
	defer a.mu.Unlock()
	select {
	case <-a.done:
		return
	default:
		close(a.done)
		if !a.closedByRemote && a.peerSender != nil && a.nextHop != (identity.AgentID{}) {
			frame := &protocol.Frame{
				Type:     protocol.FrameStreamClose,
				StreamID: a.streamID,
			}
			_ = a.peerSender.SendToPeer(a.nextHop, frame)
		}
		if a.closeFunc != nil {
			a.closeFunc()
		}
	}
}

// CloseFromRemote closes the adapter without echoing a STREAM_CLOSE frame
// back. Use when reacting to an incoming STREAM_CLOSE from the remote, or
// when the next-hop peer has disconnected and the frame could not be
// delivered anyway.
func (a *ShellStreamAdapter) CloseFromRemote() {
	a.mu.Lock()
	a.closedByRemote = true
	a.mu.Unlock()
	a.Close()
}

// NextHop returns the next-hop peer ID (zero value before SetNextHop).
func (a *ShellStreamAdapter) NextHop() identity.AgentID {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.nextHop
}

// Read implements io.Reader for the receive channel.
func (a *ShellStreamAdapter) Read(p []byte) (n int, err error) {
	select {
	case data, ok := <-a.receive:
		if !ok {
			return 0, io.EOF
		}
		n = copy(p, data)
		return n, nil
	case <-a.done:
		return 0, io.EOF
	}
}

// Write implements io.Writer for the send channel.
func (a *ShellStreamAdapter) Write(p []byte) (n int, err error) {
	data := make([]byte, len(p))
	copy(data, p)
	select {
	case a.send <- data:
		return len(p), nil
	case <-a.done:
		return 0, io.ErrClosedPipe
	}
}

// SetNextHop sets the next hop peer and sender for outgoing data.
func (a *ShellStreamAdapter) SetNextHop(nextHop identity.AgentID, sender PeerSender) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.nextHop = nextHop
	a.peerSender = sender
}

// GetStreamID returns the stream ID.
func (a *ShellStreamAdapter) GetStreamID() uint64 {
	return a.streamID
}

// SetSessionKey sets the E2E encryption session key.
func (a *ShellStreamAdapter) SetSessionKey(key *crypto.SessionKey) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.sessionKey = key
}

// GetSessionKey returns the E2E encryption session key.
func (a *ShellStreamAdapter) GetSessionKey() *crypto.SessionKey {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.sessionKey
}
