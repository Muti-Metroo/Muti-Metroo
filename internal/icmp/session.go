package icmp

import (
	"context"
	"net"
	"sync"
	"time"

	"github.com/postalsys/muti-metroo/internal/crypto"
	"github.com/postalsys/muti-metroo/internal/identity"
)

// SessionState represents the state of an ICMP session.
type SessionState int

const (
	// StateOpening means the session is being established.
	StateOpening SessionState = iota
	// StateOpen means the session is active and can relay echo packets.
	StateOpen
	// StateClosed means the session has been terminated.
	StateClosed
)

// String returns a human-readable name for the state.
func (s SessionState) String() string {
	switch s {
	case StateOpening:
		return "OPENING"
	case StateOpen:
		return "OPEN"
	case StateClosed:
		return "CLOSED"
	default:
		return "UNKNOWN"
	}
}

// Session represents an active ICMP echo tunnel through the mesh.
type Session struct {
	mu sync.RWMutex

	// Identifiers
	StreamID  uint64           // Changes per hop (local to this agent)
	RequestID uint64           // Stable across hops for correlation
	PeerID    identity.AgentID // Direct peer that sent the ICMP_OPEN

	// Target
	DestIP net.IP // Destination IP for ICMP echo

	// State
	State        SessionState
	CreatedAt    time.Time
	LastActivity time.Time

	// ICMP socket (only on exit node)
	Socket *Socket

	// Encryption
	SessionKey *crypto.SessionKey

	// Cleanup
	ctx    context.Context
	cancel context.CancelFunc
	closed bool
}

// NewSession creates a new ICMP session.
func NewSession(streamID, requestID uint64, peerID identity.AgentID, destIP net.IP) *Session {
	ctx, cancel := context.WithCancel(context.Background())
	now := time.Now()

	return &Session{
		StreamID:     streamID,
		RequestID:    requestID,
		PeerID:       peerID,
		DestIP:       destIP,
		State:        StateOpening,
		CreatedAt:    now,
		LastActivity: now,
		ctx:          ctx,
		cancel:       cancel,
	}
}

// SetOpen transitions the session to the open state.
func (s *Session) SetOpen() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.State == StateOpening {
		s.State = StateOpen
		s.LastActivity = time.Now()
	}
}

// UpdateActivity updates the last activity timestamp.
func (s *Session) UpdateActivity() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.LastActivity = time.Now()
}

// IsExpired checks if the session has been idle longer than the timeout.
func (s *Session) IsExpired(timeout time.Duration) bool {
	if timeout == 0 {
		return false
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	return time.Since(s.LastActivity) > timeout
}

// IsClosed returns true if the session has been closed.
func (s *Session) IsClosed() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.closed
}

// GetState returns the current state.
func (s *Session) GetState() SessionState {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.State
}

// Context returns the session's context.
func (s *Session) Context() context.Context {
	return s.ctx
}

// Close terminates the session and releases resources.
func (s *Session) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}

	s.closed = true
	s.State = StateClosed
	s.cancel()

	// Close ICMP socket if present
	if s.Socket != nil {
		if err := s.Socket.Close(); err != nil {
			return err
		}
		s.Socket = nil
	}

	// Zero and clear session key
	if s.SessionKey != nil {
		s.SessionKey.Zero()
		s.SessionKey = nil
	}

	return nil
}

// SetSocket sets the ICMP socket for this session (exit node).
func (s *Session) SetSocket(sock *Socket) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Socket = sock
}

// GetSocket returns the ICMP socket.
func (s *Session) GetSocket() *Socket {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.Socket
}

// SetSessionKey sets the E2E encryption session key.
func (s *Session) SetSessionKey(key *crypto.SessionKey) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.SessionKey = key
}

// GetSessionKey returns the E2E encryption session key.
func (s *Session) GetSessionKey() *crypto.SessionKey {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.SessionKey
}

// Encrypt encrypts data using the session key.
// Returns the original data if no session key is set.
//
// The RLock is held for the duration of the crypto operation so that a
// concurrent Close (which zeros the key bytes) cannot race a use of the
// key. This matters when frame dispatch is parallel: an ICMP_ECHO and an
// ICMP_CLOSE for the same stream can otherwise hit Encrypt/Decrypt and
// Close at the same time.
func (s *Session) Encrypt(plaintext []byte) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.SessionKey == nil {
		return plaintext, nil
	}

	return s.SessionKey.Encrypt(plaintext)
}

// Decrypt decrypts data using the session key.
// Returns the original data if no session key is set.
//
// See Encrypt for the lock-holding rationale.
func (s *Session) Decrypt(ciphertext []byte) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.SessionKey == nil {
		return ciphertext, nil
	}

	return s.SessionKey.Decrypt(ciphertext)
}
