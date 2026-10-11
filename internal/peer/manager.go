// Package peer manages peer connections and handshakes for Muti Metroo.
package peer

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"sync"
	"time"

	"github.com/Muti-Metroo/Muti-Metroo/internal/identity"
	"github.com/Muti-Metroo/Muti-Metroo/internal/logging"
	"github.com/Muti-Metroo/Muti-Metroo/internal/protocol"
	"github.com/Muti-Metroo/Muti-Metroo/internal/recovery"
	"github.com/Muti-Metroo/Muti-Metroo/internal/transport"
)

// PeerInfo contains information about a configured peer.
type PeerInfo struct {
	Address      string
	ExpectedID   identity.AgentID
	Capabilities []string
	Persistent   bool // If true, auto-reconnect on disconnect
	DialOptions  *transport.DialOptions
	Transport    transport.Transport // Transport to use for this peer (nil = use manager default)
}

// ManagerConfig contains configuration for the peer manager.
type ManagerConfig struct {
	LocalID           identity.AgentID
	DisplayName       string
	Capabilities      []string
	Transport         transport.Transport
	DialOptions       transport.DialOptions
	HandshakeTimeout  time.Duration
	KeepaliveInterval time.Duration
	KeepaliveTimeout  time.Duration
	KeepaliveJitter   float64       // Jitter fraction (0.0-1.0) to randomize keepalive timing
	WriteTimeout      time.Duration // Per-frame write deadline on peer connections; 0 disables
	ReconnectConfig   ReconnectConfig
	Logger            *slog.Logger
	OnPeerConnected   func(*Connection)
	OnPeerDisconnect  func(*Connection, error)
	OnFrame           func(*Connection, *protocol.Frame)
}

// DefaultManagerConfig returns a config with sensible defaults.
func DefaultManagerConfig(localID identity.AgentID, tr transport.Transport) ManagerConfig {
	return ManagerConfig{
		LocalID:           localID,
		Capabilities:      []string{},
		Transport:         tr,
		HandshakeTimeout:  10 * time.Second,
		KeepaliveInterval: 30 * time.Second,
		KeepaliveTimeout:  10 * time.Second,
		KeepaliveJitter:   0.2, // 20% jitter makes timing patterns less distinguishable
		WriteTimeout:      30 * time.Second,
		ReconnectConfig:   DefaultReconnectConfig(),
	}
}

// Manager manages connections to multiple peers.
type Manager struct {
	cfg        ManagerConfig
	handshaker *Handshaker
	logger     *slog.Logger

	mu          sync.RWMutex
	peers       map[identity.AgentID]*Connection
	peerInfos   map[string]*PeerInfo // Address -> PeerInfo
	reconnector *Reconnector

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewManager creates a new peer manager.
func NewManager(cfg ManagerConfig) *Manager {
	ctx, cancel := context.WithCancel(context.Background())

	logger := cfg.Logger
	if logger == nil {
		logger = logging.NopLogger()
	}

	m := &Manager{
		cfg:        cfg,
		handshaker: NewHandshaker(cfg.LocalID, cfg.DisplayName, cfg.Capabilities, cfg.HandshakeTimeout),
		logger:     logger,
		peers:      make(map[identity.AgentID]*Connection),
		peerInfos:  make(map[string]*PeerInfo),
		ctx:        ctx,
		cancel:     cancel,
	}

	// Create reconnector with callback to this manager
	m.reconnector = NewReconnector(cfg.ReconnectConfig, m.handleReconnect)

	return m
}

// AddPeer adds a peer configuration to the manager.
func (m *Manager) AddPeer(info PeerInfo) {
	m.mu.Lock()
	m.peerInfos[info.Address] = &info
	m.mu.Unlock()
}

// RemovePeer removes a peer configuration.
func (m *Manager) RemovePeer(addr string) {
	m.mu.Lock()
	delete(m.peerInfos, addr)
	m.mu.Unlock()
}

// RemovePeerAndDisconnect removes the PeerInfo for addr, cancels any pending
// reconnect attempt, and closes the active connection that was dialed from addr
// (if any). Returns the remote AgentID of the closed connection (zero if none)
// and whether an active connection was closed.
func (m *Manager) RemovePeerAndDisconnect(addr string) (identity.AgentID, bool) {
	m.mu.Lock()
	delete(m.peerInfos, addr)

	var victim *Connection
	var victimID identity.AgentID
	for id, conn := range m.peers {
		if conn.ConfigAddr() == addr {
			victim = conn
			victimID = id
			delete(m.peers, id)
			break
		}
	}
	m.mu.Unlock()

	// Cancel pending reconnect attempts for this address regardless of whether
	// a live connection existed (we might be mid-backoff).
	m.reconnector.Cancel(addr)

	if victim == nil {
		return identity.AgentID{}, false
	}
	_ = victim.Close()
	return victimID, true
}

// ConnectedByAddr returns a map from each live peer connection's config dial
// address to its remote AgentID. Accepted (incoming) connections have an empty
// ConfigAddr and are skipped. Useful for callers that need to join peer info
// against connection state in a single lock acquisition.
func (m *Manager) ConnectedByAddr() map[string]identity.AgentID {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make(map[string]identity.AgentID, len(m.peers))
	for id, conn := range m.peers {
		addr := conn.ConfigAddr()
		if addr == "" {
			continue
		}
		result[addr] = id
	}
	return result
}

// AcceptedPeerInfo describes a single inbound (accepted) peer connection.
type AcceptedPeerInfo struct {
	RemoteID   identity.AgentID
	RemoteAddr string // remote socket address (ephemeral source port)
	Transport  transport.TransportType
}

// AcceptedConnections returns live connections that were accepted (inbound),
// i.e. peers that dialed this agent. These have no ConfigAddr so they are not
// visible through GetPeerInfos / ConnectedByAddr.
func (m *Manager) AcceptedConnections() []AcceptedPeerInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]AcceptedPeerInfo, 0)
	for id, conn := range m.peers {
		if conn.ConfigAddr() != "" {
			continue // outbound; covered by GetPeerInfos/ConnectedByAddr
		}
		out = append(out, AcceptedPeerInfo{
			RemoteID:   id,
			RemoteAddr: conn.RemoteAddr(),
			Transport:  conn.TransportType(),
		})
	}
	return out
}

// Connect initiates a connection to a peer at the given address.
func (m *Manager) Connect(ctx context.Context, addr string) (*Connection, error) {
	return m.connectWithTransport(ctx, m.cfg.Transport, addr)
}

// ConnectWithTransport connects to a peer using the specified transport.
// This allows using different transports for different peers (e.g., WebSocket for proxy traversal).
func (m *Manager) ConnectWithTransport(ctx context.Context, tr transport.Transport, addr string) (*Connection, error) {
	return m.connectWithTransport(ctx, tr, addr)
}

// connectWithTransport is the internal implementation for connecting to peers.
func (m *Manager) connectWithTransport(ctx context.Context, tr transport.Transport, addr string) (*Connection, error) {
	m.mu.RLock()
	info := m.peerInfos[addr]
	m.mu.RUnlock()

	connCfg, dialOpts := m.buildConnectionConfig(info)

	conn, err := m.handshaker.DialAndHandshake(ctx, tr, addr, connCfg, dialOpts)
	if err != nil {
		if info != nil && info.Persistent {
			m.reconnector.Schedule(addr)
		}
		return nil, err
	}

	conn.SetConfigAddr(addr)
	m.registerConnection(conn)
	return conn, nil
}

// buildConnectionConfig creates a ConnectionConfig and DialOptions from peer info.
func (m *Manager) buildConnectionConfig(info *PeerInfo) (ConnectionConfig, transport.DialOptions) {
	var expectedID identity.AgentID
	if info != nil {
		expectedID = info.ExpectedID
	}

	connCfg := ConnectionConfig{
		LocalID:          m.cfg.LocalID,
		ExpectedPeerID:   expectedID,
		Capabilities:     m.cfg.Capabilities,
		HandshakeTimeout: m.cfg.HandshakeTimeout,
		WriteTimeout:     m.cfg.WriteTimeout,
		OnFrame:          m.cfg.OnFrame,
		OnDisconnect:     m.handleDisconnect,
	}

	dialOpts := m.cfg.DialOptions
	if info != nil && info.DialOptions != nil {
		dialOpts = *info.DialOptions
	}

	return connCfg, dialOpts
}

// Accept accepts an incoming connection and performs handshake.
func (m *Manager) Accept(ctx context.Context, peerConn transport.PeerConn) (*Connection, error) {
	connCfg, _ := m.buildConnectionConfig(nil)

	conn, err := m.handshaker.AcceptHandshake(ctx, peerConn, connCfg)
	if err != nil {
		return nil, err
	}

	m.registerConnection(conn)
	return conn, nil
}

// registerConnection adds a connection to the manager.
func (m *Manager) registerConnection(conn *Connection) {
	m.mu.Lock()
	// Reject new registrations after the manager has been canceled (Close
	// runs cancel() then waits on wg). Calling wg.Add concurrently with
	// wg.Wait when the counter goes 0 -> positive races and risks panicking;
	// also there is no point starting goroutines we are about to drain.
	select {
	case <-m.ctx.Done():
		m.mu.Unlock()
		conn.Close()
		return
	default:
	}

	var evicted *Connection
	if existing, ok := m.peers[conn.RemoteID]; ok {
		if m.isStale(existing) {
			// Existing connection is past the keepalive deadline. Evict it so
			// a brand-new reconnect doesn't wait up to
			// KeepaliveInterval+Timeout (~6.5m by default) for keepaliveLoop
			// to catch up.
			evicted = existing
		} else if preferNewConnection(m.cfg.LocalID, conn.RemoteID, existing, conn) {
			// The deterministic tie-break (or a same-direction duplicate)
			// picks the new connection. A fresh completed handshake always
			// replaces a live-looking connection of the same direction, so a
			// zombie that has not tripped the staleness check yet cannot
			// block reconnects from the far side.
			evicted = existing
		} else {
			// The tie-break keeps the existing connection: both sides of a
			// simultaneous dial converge on the socket dialed by the lower
			// AgentID. Close the loser without registering it.
			m.mu.Unlock()
			m.logger.Debug("closing duplicate connection (tie-break keeps existing)",
				"peer", conn.RemoteID.ShortString(),
				"new_dialer", conn.IsDialer(),
				"existing_dialer", existing.IsDialer())
			conn.Close()
			return
		}
		delete(m.peers, conn.RemoteID)
	}
	m.peers[conn.RemoteID] = conn
	// Add to the WaitGroup under m.mu so Close (which also takes m.mu before
	// calling wg.Wait below) cannot race the Add with the Wait.
	m.wg.Add(2)
	m.mu.Unlock()

	// Close the evicted connection after dropping the lock. Its readLoop will
	// fail and call handleDisconnect, which will be a no-op (peers map no
	// longer holds it) and will not fire OnPeerDisconnect (gated on
	// wasActive). That stops the agent-level cleanup from wiping out the
	// routes the new connection is about to install, and prevents a second
	// full disconnect cleanup for the same peer.
	if evicted != nil {
		m.logger.Debug("closing replaced connection (new connection wins)",
			"peer", conn.RemoteID.ShortString(),
			"new_dialer", conn.IsDialer(),
			"existing_dialer", evicted.IsDialer())
		evicted.Close()
	}

	go m.readLoop(conn)
	go m.keepaliveLoop(conn)

	// Notify callback
	if m.cfg.OnPeerConnected != nil {
		m.cfg.OnPeerConnected(conn)
	}
}

// preferNewConnection decides which of two live connections to the same peer
// survives registration. Both sides apply the same rule, so after a
// simultaneous dial they converge on the same socket without communication:
//
//   - Opposite directions (one outbound, one inbound on this side): the
//     surviving connection is the one dialed by the peer with the lower
//     AgentID. Equivalently, the lower AgentID keeps its OUTBOUND connection
//     and closes inbound duplicates; the higher AgentID keeps inbound.
//   - Same direction (a redial or re-accept racing teardown of the old
//     connection): the fresh connection wins, so a reconnect from the far
//     side is never rejected in favor of a zombie that has not tripped the
//     staleness check yet.
func preferNewConnection(localID, remoteID identity.AgentID, existing, newConn *Connection) bool {
	if existing.IsDialer() != newConn.IsDialer() {
		lowerKeepsOutbound := bytes.Compare(localID[:], remoteID[:]) < 0
		return newConn.IsDialer() == lowerKeepsOutbound
	}
	return true
}

// isStale reports whether conn has received no inbound traffic for longer
// than the keepalive deadline. Past this point keepaliveLoop would close conn
// the next time it ticks; registerConnection just gets there sooner. The
// check reads the inbound-only clock: outbound writes (including our own
// keepalives) must not make a blackholed peer look alive.
func (m *Manager) isStale(conn *Connection) bool {
	threshold := m.cfg.KeepaliveInterval + m.cfg.KeepaliveTimeout
	if threshold <= 0 {
		return false
	}
	return time.Since(conn.LastInboundActivity()) > threshold
}

// handleDisconnect is called when a connection is closed.
func (m *Manager) handleDisconnect(conn *Connection, err error) {
	m.mu.Lock()
	// Remove from peers map only if this is still the active connection. A
	// stale connection that registerConnection evicted will reach this path
	// later (its readLoop fails) and must NOT trigger peer-disconnect cleanup
	// -- a new connection is already serving the same RemoteID.
	var wasActive bool
	if existing, ok := m.peers[conn.RemoteID]; ok && existing == conn {
		delete(m.peers, conn.RemoteID)
		wasActive = true
	}

	// Find the peer info using the config address (original dial address).
	// This is necessary because RemoteAddr() returns the resolved IP,
	// but peerInfos is keyed by the config address (which may be a hostname).
	var peerInfo *PeerInfo
	configAddr := conn.ConfigAddr()
	if configAddr != "" {
		peerInfo = m.peerInfos[configAddr]
	}
	m.mu.Unlock()

	if !wasActive {
		return
	}

	// Notify callback
	if m.cfg.OnPeerDisconnect != nil {
		m.cfg.OnPeerDisconnect(conn, err)
	}

	// Schedule reconnect if persistent, using the config address
	if peerInfo != nil && peerInfo.Persistent && configAddr != "" {
		m.reconnector.Schedule(configAddr)
	}
}

// handleReconnect attempts to reconnect to a peer.
func (m *Manager) handleReconnect(addr string) error {
	ctx, cancel := context.WithTimeout(m.ctx, m.cfg.HandshakeTimeout+m.cfg.DialOptions.Timeout)
	defer cancel()

	// Check if peer has a specific transport configured
	m.mu.RLock()
	info := m.peerInfos[addr]
	m.mu.RUnlock()

	// Use peer-specific transport if available, otherwise use default Connect
	if info != nil && info.Transport != nil {
		_, err := m.ConnectWithTransport(ctx, info.Transport, addr)
		return err
	}

	_, err := m.Connect(ctx, addr)
	return err
}

// readLoop reads frames from a connection.
func (m *Manager) readLoop(conn *Connection) {
	defer m.wg.Done()
	defer recovery.RecoverWithLog(m.logger, "peer.readLoop")

	// Wait for handshake to complete and reader to be initialized
	select {
	case <-conn.Ready():
		// Handshake complete, reader is now safe to use
	case <-conn.Done():
		return
	case <-m.ctx.Done():
		return
	}

	for {
		select {
		case <-conn.Done():
			return
		case <-m.ctx.Done():
			return
		default:
		}

		frame, err := conn.reader.Read()
		if err != nil {
			conn.Close()
			m.handleDisconnect(conn, err)
			return
		}

		conn.updateActivity()
		conn.updateInboundActivity()

		// Handle control frames internally
		switch frame.Type {
		case protocol.FrameKeepalive:
			ka, err := protocol.DecodeKeepalive(frame.Payload)
			if err == nil {
				go conn.SendKeepaliveAck(ka.Timestamp)
			}
		case protocol.FrameKeepaliveAck:
			ka, err := protocol.DecodeKeepalive(frame.Payload)
			if err == nil {
				conn.UpdateRTT(ka.Timestamp)
				conn.clearPendingPing(ka.Timestamp)
			}
		default:
			// Stream-oriented frames go to the sequential processor to
			// preserve per-connection ordering (e.g., STREAM_CLOSE must
			// not pass STREAM_DATA on the same stream). Unordered frame
			// types (UDP_DATAGRAM, ICMP_ECHO) take a parallel fast lane
			// to avoid head-of-line blocking the sequential path; UDP and
			// ICMP are unordered by definition and per-frame handlers
			// have no cross-frame state.
			if frame.Type == protocol.FrameUDPDatagram || frame.Type == protocol.FrameICMPEcho {
				// Drop-tail: UDP/ICMP have loss semantics, so a burst
				// overflows the fast lane by dropping datagrams -- never
				// by blocking readLoop, which would stall every stream
				// and control frame on this connection.
				select {
				case conn.fastLaneCh <- frame:
				case <-conn.Done():
					return
				default:
					if drops, shouldLog := conn.noteFastLaneDrop(); shouldLog {
						m.logger.Warn("fast lane full, dropping datagram frame",
							"peer_id", conn.RemoteID.ShortString(),
							"frame_type", protocol.FrameTypeName(frame.Type),
							"total_drops", drops)
					}
				}
				continue
			}
			select {
			case conn.frameCh <- frame:
			case <-conn.Done():
				return
			}
		}
	}
}

// jitteredKeepaliveInterval returns the keepalive interval with random jitter applied.
// The jitter is calculated as: interval * (1 + random(-jitter, +jitter))
// For example, with 30s interval and 0.2 jitter, returns 24s-36s.
func (m *Manager) jitteredKeepaliveInterval() time.Duration {
	if m.cfg.KeepaliveJitter <= 0 {
		return m.cfg.KeepaliveInterval
	}

	// Use math/rand for jitter calculation (crypto/rand not needed for timing jitter)
	// rand.Float64() returns [0.0, 1.0), we convert to [-1.0, 1.0)
	jitterRange := float64(m.cfg.KeepaliveInterval) * m.cfg.KeepaliveJitter
	jitter := (rand.Float64()*2 - 1) * jitterRange

	result := time.Duration(float64(m.cfg.KeepaliveInterval) + jitter)
	if result < time.Second {
		// Ensure minimum 1 second interval
		result = time.Second
	}
	return result
}

// keepaliveLoop sends periodic keepalives with jittered timing and enforces
// the two dead-peer detectors:
//
//  1. Inbound silence: if nothing has been received for longer than
//     KeepaliveInterval+KeepaliveTimeout (checked before each send), the
//     connection is closed. This reads the inbound-only activity clock, so
//     our own keepalives cannot refresh it.
//  2. Unanswered ping: each keepalive must be answered by a KEEPALIVE_ACK
//     within KeepaliveTimeout, otherwise the connection is closed. This
//     catches a blackholed peer even when stray inbound traffic (or an
//     asymmetric link failure) keeps the inbound clock fresh.
func (m *Manager) keepaliveLoop(conn *Connection) {
	defer m.wg.Done()
	defer recovery.RecoverWithLog(m.logger, "peer.keepaliveLoop")

	// Use timer with jittered intervals instead of fixed ticker
	// This makes timing patterns less distinguishable in network traffic
	timer := time.NewTimer(m.jitteredKeepaliveInterval())
	defer timer.Stop()

	// ackTimer fires KeepaliveTimeout after each keepalive send. ackC is nil
	// (blocking forever) while no ping is outstanding.
	ackTimer := time.NewTimer(0)
	if !ackTimer.Stop() {
		select {
		case <-ackTimer.C:
		default:
		}
	}
	defer ackTimer.Stop()
	var ackC <-chan time.Time

	for {
		select {
		case <-conn.Done():
			return
		case <-m.ctx.Done():
			return
		case <-ackC:
			ackC = nil
			if conn.PendingPing() != 0 {
				conn.Close()
				m.handleDisconnect(conn, fmt.Errorf("keepalive ack timeout"))
				return
			}
		case <-timer.C:
			// Check if connection is still active
			if conn.State() != StateConnected {
				return
			}

			// Check for timeout
			if m.isStale(conn) {
				conn.Close()
				m.handleDisconnect(conn, fmt.Errorf("keepalive timeout"))
				return
			}

			// Send keepalive
			if err := conn.SendKeepalive(); err != nil {
				conn.Close()
				m.handleDisconnect(conn, err)
				return
			}

			// Arm the ACK deadline for the ping just sent
			if m.cfg.KeepaliveTimeout > 0 {
				ackTimer.Reset(m.cfg.KeepaliveTimeout)
				ackC = ackTimer.C
			}

			// Reset timer with new jittered interval
			timer.Reset(m.jitteredKeepaliveInterval())
		}
	}
}

// GetPeer returns a connection by peer ID.
func (m *Manager) GetPeer(id identity.AgentID) *Connection {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.peers[id]
}

// GetAllPeers returns all connected peers.
func (m *Manager) GetAllPeers() []*Connection {
	m.mu.RLock()
	defer m.mu.RUnlock()

	peers := make([]*Connection, 0, len(m.peers))
	for _, conn := range m.peers {
		peers = append(peers, conn)
	}
	return peers
}

// PeerCount returns the number of connected peers.
func (m *Manager) PeerCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.peers)
}

// Broadcast sends a frame to all connected peers.
func (m *Manager) Broadcast(frame *protocol.Frame) error {
	m.mu.RLock()
	peers := make([]*Connection, 0, len(m.peers))
	for _, conn := range m.peers {
		peers = append(peers, conn)
	}
	m.mu.RUnlock()

	var lastErr error
	for _, conn := range peers {
		if err := conn.WriteFrame(frame); err != nil {
			lastErr = err
		}
	}
	return lastErr
}

// Disconnect closes a connection to a specific peer.
func (m *Manager) Disconnect(id identity.AgentID) error {
	m.mu.Lock()
	conn, ok := m.peers[id]
	if ok {
		delete(m.peers, id)
	}
	m.mu.Unlock()

	if !ok {
		return fmt.Errorf("peer not found: %s", id.String())
	}

	return conn.Close()
}

// Close shuts down the manager and all connections.
func (m *Manager) Close() error {
	m.cancel()

	// Stop reconnector
	m.reconnector.Stop()

	// Take m.mu around the connection close + the wg.Wait, paired with the
	// m.mu held in registerConnection during wg.Add. This guarantees that
	// any concurrent registerConnection either finishes its Add(2) before we
	// Wait, or sees ctx.Done() and bails out without touching the WaitGroup.
	m.mu.Lock()
	for _, conn := range m.peers {
		conn.Close()
	}
	m.peers = make(map[identity.AgentID]*Connection)
	m.mu.Unlock()

	// Wait for goroutines
	m.wg.Wait()

	return nil
}

// SendToPeer sends a frame to a specific peer.
// Implements flood.PeerSender interface.
func (m *Manager) SendToPeer(peerID identity.AgentID, frame *protocol.Frame) error {
	m.mu.RLock()
	conn := m.peers[peerID]
	m.mu.RUnlock()

	if conn == nil {
		return fmt.Errorf("peer not found: %s", peerID.ShortString())
	}

	return conn.WriteFrame(frame)
}

// GetPeerIDs returns all connected peer IDs.
// Implements flood.PeerSender interface.
func (m *Manager) GetPeerIDs() []identity.AgentID {
	m.mu.RLock()
	defer m.mu.RUnlock()

	ids := make([]identity.AgentID, 0, len(m.peers))
	for id := range m.peers {
		ids = append(ids, id)
	}
	return ids
}

// SetFrameCallback sets the callback for incoming frames.
func (m *Manager) SetFrameCallback(callback func(identity.AgentID, *protocol.Frame)) {
	// Wrap the callback to match the internal interface
	m.cfg.OnFrame = func(conn *Connection, frame *protocol.Frame) {
		if callback != nil {
			callback(conn.RemoteID, frame)
		}
	}
}

// DisconnectAll closes all peer connections without removing peer configurations.
// This is used for sleep mode - connections can be re-established later.
// Unlike Close(), this does not stop the manager or reconnector.
func (m *Manager) DisconnectAll() error {
	m.mu.Lock()
	conns := make([]*Connection, 0, len(m.peers))
	for _, conn := range m.peers {
		conns = append(conns, conn)
	}
	m.peers = make(map[identity.AgentID]*Connection)
	m.mu.Unlock()

	// Stop reconnector temporarily to prevent immediate reconnection
	m.reconnector.Pause()

	// Close all connections
	var lastErr error
	for _, conn := range conns {
		if err := conn.Close(); err != nil {
			lastErr = err
		}
	}

	m.logger.Info("disconnected all peers", "count", len(conns))
	return lastErr
}

// ReconnectAll re-establishes connections to all configured persistent peers.
// This is used to resume from sleep mode.
func (m *Manager) ReconnectAll(ctx context.Context) error {
	// Reset backoff delays and resume reconnector for fresh start after sleep
	m.reconnector.ResetAll()
	m.reconnector.Resume()

	m.mu.RLock()
	infos := make([]*PeerInfo, 0)
	addrs := make([]string, 0)
	for addr, info := range m.peerInfos {
		if info.Persistent {
			infos = append(infos, info)
			addrs = append(addrs, addr)
		}
	}
	m.mu.RUnlock()

	m.logger.Info("reconnecting to peers", "count", len(addrs))

	var lastErr error
	for i, addr := range addrs {
		info := infos[i]

		// Skip if already connected to this peer
		if info.ExpectedID != (identity.AgentID{}) {
			m.mu.RLock()
			_, connected := m.peers[info.ExpectedID]
			m.mu.RUnlock()
			if connected {
				continue
			}
		}

		// Use peer-specific transport if available
		var tr transport.Transport
		if info.Transport != nil {
			tr = info.Transport
		} else {
			tr = m.cfg.Transport
		}

		connCtx, cancel := context.WithTimeout(ctx, m.cfg.HandshakeTimeout+m.cfg.DialOptions.Timeout)
		_, err := m.connectWithTransport(connCtx, tr, addr)
		cancel()

		if err != nil {
			m.logger.Debug("failed to reconnect to peer",
				"addr", addr,
				logging.KeyError, err)
			lastErr = err
			// Schedule for reconnection
			m.reconnector.Schedule(addr)
		}
	}

	return lastErr
}

// GetPeerInfos returns a copy of all configured peer infos.
// This is useful for resuming connections after sleep.
func (m *Manager) GetPeerInfos() map[string]*PeerInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	infos := make(map[string]*PeerInfo, len(m.peerInfos))
	for addr, info := range m.peerInfos {
		infoCopy := *info
		infos[addr] = &infoCopy
	}
	return infos
}

// IsPaused returns true if the reconnector is paused.
func (m *Manager) IsPaused() bool {
	return m.reconnector.IsPaused()
}
