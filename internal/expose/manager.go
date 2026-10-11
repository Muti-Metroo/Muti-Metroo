package expose

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"sync"

	"github.com/Muti-Metroo/Muti-Metroo/internal/forward"
	"github.com/Muti-Metroo/Muti-Metroo/internal/logging"
)

// AgentInterface is the slice of agent functionality the Manager needs to
// register and unregister ephemeral forward keys.
type AgentInterface interface {
	// RegisterEphemeralForwardKey records the key as owned by the given
	// session id and installs the supplied backend. Must perform a collision
	// check against config endpoints, dynamic endpoints, and other live
	// sessions; return an error on conflict.
	RegisterEphemeralForwardKey(key, owner string, b forward.Backend) error

	// UnregisterEphemeralForwardKey removes the key only if owner matches
	// the recorded owner. Idempotent.
	UnregisterEphemeralForwardKey(key, owner string)

	// TriggerRouteAdvertise asks the routing layer to push an updated route
	// advertisement now rather than waiting for the next periodic tick.
	TriggerRouteAdvertise()
}

// ManagerConfig configures the expose Manager.
type ManagerConfig struct {
	// ReadLimit caps the size of any single inbound expose frame. Defaults to
	// 1 MiB. Must be larger than the largest expected DATA payload (operators
	// can send up to this size; outbound DATA is chunked at MaxDataPayload).
	ReadLimit int64

	// Session controls per-session timeouts.
	Session SessionConfig

	// Logger for manager-level events. Sessions get a derived logger.
	Logger *slog.Logger
}

// defaultReadLimit caps any single inbound expose frame at 1 MiB. Must stay
// well above MaxDataPayload so operators with large in-flight payloads are not
// artificially throttled.
const defaultReadLimit = 1 << 20

func (c ManagerConfig) withDefaults() ManagerConfig {
	if c.ReadLimit <= 0 {
		c.ReadLimit = defaultReadLimit
	}
	if c.Logger == nil {
		c.Logger = logging.NopLogger()
	}
	return c
}

// Manager owns the live set of expose Sessions for one Muti Metroo agent. Wire it
// up alongside the forward handler at agent start, then plumb its
// HandleRawConn method into the SOCKS5 server's CmdExpose handler.
type Manager struct {
	agent AgentInterface
	cfg   ManagerConfig

	mu       sync.Mutex
	sessions map[string]*Session
}

// NewManager constructs a Manager. The provided AgentInterface is used to
// install and remove ephemeral forward keys.
func NewManager(agent AgentInterface, cfg ManagerConfig) *Manager {
	cfg = cfg.withDefaults()
	return &Manager{
		agent:    agent,
		cfg:      cfg,
		sessions: make(map[string]*Session),
	}
}

// HandleRawConn runs a Session over an already-authenticated raw stream (the
// SOCKS5 CmdExpose connection). It blocks until the stream terminates. The
// caller is responsible for authentication; this method assumes the SOCKS5
// handshake has already authorized the connection.
func (m *Manager) HandleRawConn(conn net.Conn, remoteAddr string) {
	cfg := m.cfg.Session
	if cfg.Logger == nil {
		cfg.Logger = m.cfg.Logger
	}
	reg := &sessionRegistrar{m: m}
	sess := newSession(context.Background(), newRawConn(conn, m.cfg.ReadLimit), remoteAddr, reg, cfg)
	reg.sess = sess

	m.addSession(sess)
	defer m.removeSession(sess)

	_ = sess.Run()
}

// Shutdown closes all live sessions. Used when the agent is stopping.
func (m *Manager) Shutdown() {
	m.mu.Lock()
	sessions := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		sessions = append(sessions, s)
	}
	m.mu.Unlock()
	for _, s := range sessions {
		_ = s.conn.Close()
	}
}

func (m *Manager) addSession(s *Session) {
	m.mu.Lock()
	m.sessions[s.id] = s
	m.mu.Unlock()
}

func (m *Manager) removeSession(s *Session) {
	m.mu.Lock()
	delete(m.sessions, s.id)
	m.mu.Unlock()
}

// sessionRegistrar implements keyRegistrar by delegating to the agent and
// the bound session.
type sessionRegistrar struct {
	m    *Manager
	sess *Session
}

func (r *sessionRegistrar) register(key string, sess *Session) error {
	if r.sess == nil {
		return errors.New("expose: registrar not bound to session")
	}
	return r.m.agent.RegisterEphemeralForwardKey(key, sess.id, NewBackend(sess, key))
}

func (r *sessionRegistrar) unregister(key string, sess *Session) {
	r.m.agent.UnregisterEphemeralForwardKey(key, sess.id)
}

func (r *sessionRegistrar) triggerAdvertise() {
	r.m.agent.TriggerRouteAdvertise()
}
