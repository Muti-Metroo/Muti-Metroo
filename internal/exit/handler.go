package exit

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/postalsys/muti-metroo/internal/crypto"
	"github.com/postalsys/muti-metroo/internal/identity"
	"github.com/postalsys/muti-metroo/internal/logging"
	"github.com/postalsys/muti-metroo/internal/protocol"
	"github.com/postalsys/muti-metroo/internal/recovery"
	"github.com/postalsys/muti-metroo/internal/sysinfo"
)

// DomainPattern represents an allowed domain pattern.
type DomainPattern struct {
	Pattern    string // Original pattern (e.g., "*.example.com" or "api.test.local")
	IsWildcard bool
	BaseDomain string // For wildcards: domain without "*." prefix
}

// AllowedRoute is one entry in the exit handler's allowed-routes table.
//
// Source, when non-nil, binds outbound dials matching this CIDR to a specific
// local address (net.Dialer.LocalAddr). It must be an address local to this
// host and of the same family as the CIDR.
//
// Interface, when non-empty, pins the egress network device for the dial.
// computeBindingPlan decides how: on Linux + root via SO_BINDTODEVICE (which
// honors any Source, including an off-interface loopback address); otherwise
// portably by binding an address on the interface as the dial source
// (net.Dialer.LocalAddr), which works unprivileged on every platform when
// Source is unset or already assigned to the interface. An off-interface
// Source without root is rejected at the dial site.
type AllowedRoute struct {
	Network   *net.IPNet
	Source    net.IP
	Interface string
}

// HandlerConfig contains exit handler configuration.
type HandlerConfig struct {
	// AllowedRoutes defines which destinations are allowed (CIDR format)
	AllowedRoutes []AllowedRoute

	// AllowedDomains defines which domain patterns are allowed
	AllowedDomains []DomainPattern

	// ConnectTimeout for outbound connections
	ConnectTimeout time.Duration

	// IdleTimeout for idle connections
	IdleTimeout time.Duration

	// MaxConnections limits concurrent connections
	MaxConnections int

	// DNS configuration
	DNS DNSConfig

	// Logger for logging
	Logger *slog.Logger
}

// DefaultHandlerConfig returns sensible defaults.
func DefaultHandlerConfig() HandlerConfig {
	return HandlerConfig{
		AllowedRoutes:  nil, // nil means deny all (security: deny by default)
		ConnectTimeout: 30 * time.Second,
		IdleTimeout:    5 * time.Minute,
		MaxConnections: 1000,
		DNS:            DefaultDNSConfig(),
	}
}

// StreamWriter is an interface for writing to virtual streams.
type StreamWriter interface {
	// WriteStreamData writes data to a stream.
	WriteStreamData(peerID identity.AgentID, streamID uint64, data []byte, flags uint8) error

	// WriteStreamOpenAck sends a successful open acknowledgment with ephemeral public key for E2E encryption.
	WriteStreamOpenAck(peerID identity.AgentID, streamID uint64, requestID uint64, boundIP net.IP, boundPort uint16, ephemeralPubKey [crypto.KeySize]byte) error

	// WriteStreamOpenErr sends a failed open acknowledgment.
	WriteStreamOpenErr(peerID identity.AgentID, streamID uint64, requestID uint64, errorCode uint16, message string) error

	// WriteStreamClose sends a close frame.
	WriteStreamClose(peerID identity.AgentID, streamID uint64) error
}

// ActiveConnection represents an active exit connection.
type ActiveConnection struct {
	StreamID   uint64
	RemoteID   identity.AgentID
	DestAddr   string
	DestPort   uint16
	Conn       net.Conn
	StartedAt  time.Time
	closed     atomic.Bool
	closeOnce  sync.Once
	sessionKey *crypto.SessionKey // E2E encryption session key
}

// Close closes the connection.
func (ac *ActiveConnection) Close() error {
	var err error
	ac.closeOnce.Do(func() {
		ac.closed.Store(true)
		if ac.Conn != nil {
			err = ac.Conn.Close()
		}
	})
	return err
}

// IsClosed returns true if the connection is closed.
func (ac *ActiveConnection) IsClosed() bool {
	return ac.closed.Load()
}

// streamKey identifies a stream by the peer that opened it as well as its ID.
// Stream IDs are allocated per peer connection -- every dialer starts at 1 and
// steps by 2 -- so the ID alone is not unique across peers. Two agents peering
// directly with this exit both open stream 1, and keying on the ID alone would
// let the second displace the first: the first peer's TCP connection would be
// orphaned with its read loop still running, connCount would drift upward, and
// either peer could close or reset the other's stream by quoting a small,
// entirely guessable ID.
type streamKey struct {
	peer     identity.AgentID
	streamID uint64
}

// Handler handles exit node connections.
type Handler struct {
	cfg      HandlerConfig
	localID  identity.AgentID
	resolver *Resolver
	writer   StreamWriter
	logger   *slog.Logger

	mu          sync.RWMutex
	connections map[streamKey]*ActiveConnection
	connCount   atomic.Int64

	// boundRoutes is the number of AllowedRoutes that carry any local
	// binding -- a source IP, an egress interface, or both. lookupBinding
	// consults this first so a clean default-binding deployment pays one
	// atomic load per stream open, not an RLock + slice scan.
	boundRoutes atomic.Int32

	routesMu sync.RWMutex // Guards cfg.AllowedRoutes for dynamic modification

	// Binding-plan dependencies, injectable for tests. They default to the
	// platform probe and the sysinfo interface resolvers in NewHandler.
	canBindDevice func() bool
	ifaceHasAddr  func(name string, ip net.IP) (has bool, resolved bool)
	pickIfaceSrc  func(name string, wantV4 bool) net.IP

	running  atomic.Bool
	stopOnce sync.Once
	stopCh   chan struct{}
	wg       sync.WaitGroup
}

// NewHandler creates a new exit handler.
func NewHandler(cfg HandlerConfig, localID identity.AgentID, writer StreamWriter) *Handler {
	logger := cfg.Logger
	if logger == nil {
		logger = logging.NopLogger()
	}

	h := &Handler{
		cfg:           cfg,
		localID:       localID,
		resolver:      NewResolver(cfg.DNS),
		writer:        writer,
		logger:        logger,
		connections:   make(map[streamKey]*ActiveConnection),
		stopCh:        make(chan struct{}),
		canBindDevice: canBindToDevice,
		ifaceHasAddr:  sysinfo.InterfaceHasAddress,
		pickIfaceSrc:  sysinfo.PickInterfaceSource,
	}
	for _, r := range cfg.AllowedRoutes {
		if r.Source != nil || r.Interface != "" {
			h.boundRoutes.Add(1)
		}
	}
	return h
}

// Start starts the exit handler.
func (h *Handler) Start() {
	h.running.Store(true)
}

// Stop stops the exit handler.
func (h *Handler) Stop() {
	h.stopOnce.Do(func() {
		h.running.Store(false)
		close(h.stopCh)

		// Close all connections
		h.mu.Lock()
		for _, conn := range h.connections {
			conn.Close()
		}
		h.connections = make(map[streamKey]*ActiveConnection)
		h.mu.Unlock()

		h.wg.Wait()
	})
}

// IsRunning returns true if the handler is running.
func (h *Handler) IsRunning() bool {
	return h.running.Load()
}

// HandleStreamOpen processes a STREAM_OPEN request.
// The TCP dial is performed asynchronously to avoid blocking the frame processing loop.
func (h *Handler) HandleStreamOpen(ctx context.Context, streamID uint64, requestID uint64, remoteID identity.AgentID, destAddr string, destPort uint16, remoteEphemeralPub [crypto.KeySize]byte) error {
	if !h.running.Load() {
		return fmt.Errorf("handler not running")
	}

	// Check connection limit
	if h.cfg.MaxConnections > 0 && h.connCount.Load() >= int64(h.cfg.MaxConnections) {
		h.sendOpenErr(remoteID, streamID, requestID, protocol.ErrConnectionLimit, "connection limit exceeded")
		return fmt.Errorf("connection limit exceeded")
	}

	// Check if this is a domain (not IP) that matches a domain pattern
	domainAllowed := false
	if net.ParseIP(destAddr) == nil {
		// destAddr is a domain name, check against domain patterns
		domainAllowed = h.isDomainAllowed(destAddr)
	}

	// Perform the rest asynchronously to avoid blocking the frame processing loop.
	// TCP dials to filtered ports can take 20+ seconds to timeout.
	h.wg.Add(1)
	go func() {
		defer h.wg.Done()
		h.handleStreamOpenAsync(ctx, streamID, requestID, remoteID, destAddr, destPort, remoteEphemeralPub, domainAllowed)
	}()

	return nil
}

// handleStreamOpenAsync performs the actual stream open work asynchronously.
func (h *Handler) handleStreamOpenAsync(ctx context.Context, streamID uint64, requestID uint64, remoteID identity.AgentID, destAddr string, destPort uint16, remoteEphemeralPub [crypto.KeySize]byte, domainAllowed bool) {
	h.logger.Debug("exit stream_open received",
		"stream_id", streamID,
		"remote_id", remoteID.ShortString(),
		"dest", destAddr,
		"dest_port", destPort,
	)

	// Resolve address
	ip, err := h.resolver.Resolve(ctx, destAddr)
	if err != nil {
		h.logger.Info("exit dns resolve failed",
			"stream_id", streamID,
			"dest", destAddr,
			"error", err,
		)
		h.sendOpenErr(remoteID, streamID, requestID, protocol.ErrHostUnreachable, err.Error())
		return
	}

	// Check if destination is allowed (domain patterns OR CIDR routes)
	if !domainAllowed && !h.isAllowed(ip) {
		h.logger.Info("exit destination rejected",
			"stream_id", streamID,
			"dest", destAddr,
			"dest_ip", ip,
			"reason", "not in allowed routes or domain patterns",
		)
		h.sendOpenErr(remoteID, streamID, requestID, protocol.ErrNotAllowed, "destination not allowed")
		return
	}

	// Generate ephemeral keypair for E2E encryption key exchange
	ephPriv, ephPub, err := crypto.GenerateEphemeralKeypair()
	if err != nil {
		h.sendOpenErr(remoteID, streamID, requestID, protocol.ErrGeneralFailure, "key generation failed")
		return
	}

	// Compute shared secret from ECDH
	sharedSecret, err := crypto.ComputeECDH(ephPriv, remoteEphemeralPub)
	if err != nil {
		crypto.ZeroKey(&ephPriv)
		h.sendOpenErr(remoteID, streamID, requestID, protocol.ErrGeneralFailure, "key exchange failed")
		return
	}

	// Zero out ephemeral private key after computing shared secret
	crypto.ZeroKey(&ephPriv)

	// Derive session key - we are the responder (exit node)
	// Use requestID (not streamID) because streamID changes at each relay hop
	sessionKey := crypto.DeriveSessionKey(sharedSecret, requestID, remoteEphemeralPub, ephPub, false)
	crypto.ZeroKey(&sharedSecret)

	addr := fmt.Sprintf("%s:%d", ip.String(), destPort)
	dialer := &net.Dialer{Timeout: h.cfg.ConnectTimeout}
	bindSource, bindIface, err := h.applyBinding(dialer, ip)
	if err != nil {
		h.logger.Info("exit dial rejected: binding not satisfiable",
			"stream_id", streamID,
			"addr", addr,
			"error", err,
		)
		h.sendOpenErr(remoteID, streamID, requestID, protocol.ErrGeneralFailure, err.Error())
		return
	}

	h.logger.Debug("exit dial start",
		"stream_id", streamID,
		"addr", addr,
		"source_ip", bindSource,
		"interface", bindIface,
	)

	dialStart := time.Now()
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		h.logger.Info("exit dial failed",
			"stream_id", streamID,
			"addr", addr,
			"source_ip", bindSource,
			"interface", bindIface,
			"duration", time.Since(dialStart),
			"error", err,
		)
		errorCode := h.mapDialError(err)
		h.sendOpenErr(remoteID, streamID, requestID, errorCode, err.Error())
		return
	}
	h.logger.Debug("exit dial succeeded",
		"stream_id", streamID,
		"addr", addr,
		"source_ip", bindSource,
		"interface", bindIface,
		"duration", time.Since(dialStart),
		"local_addr", conn.LocalAddr().String(),
	)

	// Get local address for ACK
	localAddr := conn.LocalAddr().(*net.TCPAddr)

	// Track connection with session key
	ac := &ActiveConnection{
		StreamID:   streamID,
		RemoteID:   remoteID,
		DestAddr:   destAddr,
		DestPort:   destPort,
		Conn:       conn,
		StartedAt:  time.Now(),
		sessionKey: sessionKey,
	}

	h.mu.Lock()
	h.connections[streamKey{peer: remoteID, streamID: streamID}] = ac
	h.connCount.Add(1)
	h.mu.Unlock()

	// Send ACK with our ephemeral public key
	if err := h.writer.WriteStreamOpenAck(remoteID, streamID, requestID, localAddr.IP, uint16(localAddr.Port), ephPub); err != nil {
		ac.Close()
		h.removeConnection(streamKey{peer: remoteID, streamID: streamID})
		return
	}

	// Start reading from destination
	h.wg.Add(1)
	go func() {
		defer h.wg.Done()
		h.readLoop(ac)
	}()
}

// HandleStreamData processes incoming stream data.
func (h *Handler) HandleStreamData(peerID identity.AgentID, streamID uint64, data []byte, flags uint8) error {
	h.mu.RLock()
	ac := h.connections[streamKey{peer: peerID, streamID: streamID}]
	h.mu.RUnlock()

	if ac == nil {
		return fmt.Errorf("unknown stream %d", streamID)
	}

	if ac.IsClosed() {
		return fmt.Errorf("stream %d closed", streamID)
	}

	// Decrypt data before writing to destination
	if len(data) > 0 {
		if ac.sessionKey == nil {
			h.closeConnection(peerID, streamID, fmt.Errorf("no session key"))
			return fmt.Errorf("no session key for stream %d", streamID)
		}

		plaintext, err := ac.sessionKey.DecryptStrict(data)
		if err != nil {
			h.closeConnection(peerID, streamID, err)
			return fmt.Errorf("decrypt: %w", err)
		}

		if _, err := ac.Conn.Write(plaintext); err != nil {
			h.closeConnection(peerID, streamID, err)
			return err
		}
	}

	// Handle FIN flag
	if flags&protocol.FlagFinWrite != 0 {
		// Client is done sending, close write side of destination
		if tcpConn, ok := ac.Conn.(*net.TCPConn); ok {
			tcpConn.CloseWrite()
		}
	}

	return nil
}

// HandleStreamClose processes a stream close request.
func (h *Handler) HandleStreamClose(peerID identity.AgentID, streamID uint64) {
	h.closeConnection(peerID, streamID, nil)
}

// HandleStreamReset processes a stream reset request.
func (h *Handler) HandleStreamReset(peerID identity.AgentID, streamID uint64, errorCode uint16) {
	h.closeConnection(peerID, streamID, fmt.Errorf("reset with code %d", errorCode))
}

// readLoop reads data from the destination and forwards to the stream.
func (h *Handler) readLoop(ac *ActiveConnection) {
	defer h.closeConnection(ac.RemoteID, ac.StreamID, nil)
	defer recovery.RecoverWithLog(h.logger, "exit.readLoop")

	// Account for encryption overhead when reading
	// Each encrypted message must fit in a single frame to be decryptable
	maxPlaintext := protocol.MaxPayloadSize - crypto.EncryptionOverhead
	buf := make([]byte, maxPlaintext)
	for {
		select {
		case <-h.stopCh:
			return
		default:
		}

		// Set read deadline for idle timeout
		if h.cfg.IdleTimeout > 0 {
			ac.Conn.SetReadDeadline(time.Now().Add(h.cfg.IdleTimeout))
		}

		n, err := ac.Conn.Read(buf)
		if n > 0 {
			// Encrypt data before forwarding
			if ac.sessionKey == nil {
				h.logger.Error("no session key in readLoop",
					logging.KeyStreamID, ac.StreamID)
				return
			}

			ciphertext, encErr := ac.sessionKey.Encrypt(buf[:n])
			if encErr != nil {
				h.logger.Error("encrypt failed in readLoop",
					logging.KeyStreamID, ac.StreamID,
					logging.KeyError, encErr)
				return
			}

			// Forward encrypted data to stream
			if writeErr := h.writer.WriteStreamData(ac.RemoteID, ac.StreamID, ciphertext, 0); writeErr != nil {
				return
			}
		}

		if err != nil {
			if err == io.EOF {
				// Send FIN_WRITE (no data to encrypt)
				h.writer.WriteStreamData(ac.RemoteID, ac.StreamID, nil, protocol.FlagFinWrite)
			}
			return
		}
	}
}

// closeConnection closes a connection and cleans up.
func (h *Handler) closeConnection(peerID identity.AgentID, streamID uint64, err error) {
	ac := h.removeConnection(streamKey{peer: peerID, streamID: streamID})
	if ac == nil {
		return
	}

	ac.Close()

	// Notify stream is closed
	if h.writer != nil {
		h.writer.WriteStreamClose(peerID, streamID)
	}
}

// removeConnection removes a connection from tracking.
func (h *Handler) removeConnection(key streamKey) *ActiveConnection {
	h.mu.Lock()
	defer h.mu.Unlock()

	ac, ok := h.connections[key]
	if !ok {
		return nil
	}

	delete(h.connections, key)
	h.connCount.Add(-1)
	return ac
}

// isAllowed checks if an IP is allowed by the configured routes.
// Security: Returns false (deny) when no routes are configured.
func (h *Handler) isAllowed(ip net.IP) bool {
	h.routesMu.RLock()
	defer h.routesMu.RUnlock()

	if len(h.cfg.AllowedRoutes) == 0 {
		return false // Deny by default when no routes configured
	}

	for _, route := range h.cfg.AllowedRoutes {
		if route.Network.Contains(ip) {
			return true
		}
	}

	return false
}

// applyBinding configures dialer's LocalAddr / Control for the per-route
// source IP / egress interface binding that applies to destIP, returning the
// bound source and interface (empty strings when none apply) for logging. It
// returns an error when a configured binding is not satisfiable on this host
// -- an off-interface source without Linux+root, or an interface with no usable
// address. This is the single binding implementation shared by the exit stream
// path (handleStreamOpenAsync) and the local-exit dial path (DialerFor).
func (h *Handler) applyBinding(dialer *net.Dialer, destIP net.IP) (bindSource, bindIface string, err error) {
	src, iface := h.lookupBinding(destIP)
	if src == nil && iface == "" {
		return "", "", nil
	}
	plan, err := computeBindingPlan(src, iface, destIP.To4() != nil, h.canBindDevice(), h.ifaceHasAddr, h.pickIfaceSrc)
	if err != nil {
		return "", "", err
	}
	if plan.localAddr != nil {
		dialer.LocalAddr = &net.TCPAddr{IP: plan.localAddr}
		bindSource = plan.localAddr.String()
	}
	if plan.useDevice {
		dialer.Control = bindToDeviceControl(iface)
		bindIface = iface
	}
	return bindSource, bindIface, nil
}

// DialerFor returns a net.Dialer configured with the source IP / egress
// interface binding that applies to destIP, for callers that dial outside the
// stream handler -- notably the agent's local-exit SOCKS path, where ingress
// and exit are the same agent and no STREAM_OPEN frame is involved. timeout
// sets Dialer.Timeout. The returned source/interface strings are for logging.
// Returns an error if a configured binding is not satisfiable on this host.
func (h *Handler) DialerFor(destIP net.IP, timeout time.Duration) (*net.Dialer, string, string, error) {
	dialer := &net.Dialer{Timeout: timeout}
	src, iface, err := h.applyBinding(dialer, destIP)
	if err != nil {
		return nil, "", "", err
	}
	return dialer, src, iface, nil
}

// lookupBinding returns the source IP and egress interface configured for
// the most-specific (longest-prefix) allowed route containing ip. Returns
// (nil, "") when no matching route has any binding. Defends against family
// mismatches even though config validation already rejects them, because a
// ::/0 route legitimately matches v4-mapped addresses (the source still must
// match family). The interface binding has no family requirement.
func (h *Handler) lookupBinding(ip net.IP) (net.IP, string) {
	if h.boundRoutes.Load() == 0 {
		return nil, ""
	}

	h.routesMu.RLock()
	defer h.routesMu.RUnlock()

	ipIsV4 := ip.To4() != nil
	var bestSrc net.IP
	var bestIface string
	bestOnes := -1
	for _, route := range h.cfg.AllowedRoutes {
		if route.Source == nil && route.Interface == "" {
			continue
		}
		if !route.Network.Contains(ip) {
			continue
		}
		// Source must match address family of the destination IP; an
		// interface binding is family-agnostic so it still applies even
		// if a sibling Source would be filtered out.
		ones, _ := route.Network.Mask.Size()
		if ones <= bestOnes {
			continue
		}
		bestOnes = ones
		bestIface = route.Interface
		if route.Source != nil && (route.Source.To4() != nil) == ipIsV4 {
			bestSrc = route.Source
		} else {
			bestSrc = nil
		}
	}
	return bestSrc, bestIface
}

// routeIsBound is the invariant boundRoutes counts: a route contributes when
// it carries either a source IP or an egress interface.
func routeIsBound(r AllowedRoute) bool {
	return r.Source != nil || r.Interface != ""
}

// AddAllowedRoute adds a CIDR route to the allowed routes list. source may be
// nil and iface may be empty; when set, they bind outbound dials matching this
// CIDR to a local IP and/or pin the egress NIC.
func (h *Handler) AddAllowedRoute(network *net.IPNet, source net.IP, iface string) {
	h.routesMu.Lock()
	defer h.routesMu.Unlock()
	entry := AllowedRoute{Network: network, Source: source, Interface: iface}
	h.cfg.AllowedRoutes = append(h.cfg.AllowedRoutes, entry)
	if routeIsBound(entry) {
		h.boundRoutes.Add(1)
	}
}

// SetAllowedRouteSource updates the source IP on an existing allowed route.
// Pass source=nil to clear. Returns an error if the network is not present.
// Maintains the boundRoutes counter so lookupBinding's fast path stays accurate.
func (h *Handler) SetAllowedRouteSource(network *net.IPNet, source net.IP) error {
	return h.mutateRoute(network, func(r *AllowedRoute) { r.Source = source })
}

// SetAllowedRouteInterface updates the egress interface on an existing allowed
// route. Pass iface="" to clear. Returns an error if the network is not present.
// Maintains the boundRoutes counter so lookupBinding's fast path stays accurate.
func (h *Handler) SetAllowedRouteInterface(network *net.IPNet, iface string) error {
	return h.mutateRoute(network, func(r *AllowedRoute) { r.Interface = iface })
}

// mutateRoute is the shared "find route by network and apply f, fixing the
// boundRoutes counter" helper used by the set_source_ip / set_interface
// management actions.
func (h *Handler) mutateRoute(network *net.IPNet, f func(*AllowedRoute)) error {
	h.routesMu.Lock()
	defer h.routesMu.Unlock()

	target := network.String()
	for i, route := range h.cfg.AllowedRoutes {
		if route.Network.String() != target {
			continue
		}
		wasBound := routeIsBound(route)
		f(&h.cfg.AllowedRoutes[i])
		nowBound := routeIsBound(h.cfg.AllowedRoutes[i])
		switch {
		case !wasBound && nowBound:
			h.boundRoutes.Add(1)
		case wasBound && !nowBound:
			h.boundRoutes.Add(-1)
		}
		return nil
	}
	return fmt.Errorf("route %s not found", target)
}

// RemoveAllowedRoute removes a CIDR route from the allowed routes list.
// Returns true if the route was found and removed.
func (h *Handler) RemoveAllowedRoute(network *net.IPNet) bool {
	h.routesMu.Lock()
	defer h.routesMu.Unlock()

	target := network.String()
	for i, route := range h.cfg.AllowedRoutes {
		if route.Network.String() == target {
			if routeIsBound(route) {
				h.boundRoutes.Add(-1)
			}
			h.cfg.AllowedRoutes = append(h.cfg.AllowedRoutes[:i], h.cfg.AllowedRoutes[i+1:]...)
			return true
		}
	}
	return false
}

// AllowedRouteCount returns the number of allowed routes.
func (h *Handler) AllowedRouteCount() int {
	h.routesMu.RLock()
	defer h.routesMu.RUnlock()
	return len(h.cfg.AllowedRoutes)
}

// RouteBinding is the snapshot view of an exit route's local-only binding
// fields (source IP and/or egress interface). Used by list-route output and
// the dashboard payload.
type RouteBinding struct {
	Source    net.IP
	Interface string
}

// BindingsByCIDR returns a snapshot map of CIDR string -> binding for every
// allowed route that has any binding configured. The atomic-counter fast path
// keeps default deployments (no per-route source IPs or interfaces) free of
// lock + alloc per call.
func (h *Handler) BindingsByCIDR() map[string]RouteBinding {
	if h.boundRoutes.Load() == 0 {
		return nil
	}
	h.routesMu.RLock()
	defer h.routesMu.RUnlock()
	if len(h.cfg.AllowedRoutes) == 0 {
		return nil
	}
	out := make(map[string]RouteBinding, len(h.cfg.AllowedRoutes))
	for _, r := range h.cfg.AllowedRoutes {
		if !routeIsBound(r) {
			continue
		}
		out[r.Network.String()] = RouteBinding{Source: r.Source, Interface: r.Interface}
	}
	return out
}

// isDomainAllowed checks if a domain matches any allowed domain pattern.
func (h *Handler) isDomainAllowed(domain string) bool {
	if len(h.cfg.AllowedDomains) == 0 {
		return false
	}

	domain = strings.ToLower(domain)

	for _, dp := range h.cfg.AllowedDomains {
		if dp.IsWildcard {
			// Wildcard pattern: *.example.com matches foo.example.com (single level only)
			// Check if domain ends with .baseDomain and has exactly one more level
			suffix := "." + strings.ToLower(dp.BaseDomain)
			if strings.HasSuffix(domain, suffix) {
				// Count dots before the suffix - should be zero for single-level wildcard
				prefix := domain[:len(domain)-len(suffix)]
				if !strings.Contains(prefix, ".") && len(prefix) > 0 {
					return true
				}
			}
		} else {
			// Exact match
			if domain == strings.ToLower(dp.Pattern) {
				return true
			}
		}
	}

	return false
}

// mapDialError maps dial errors to protocol error codes.
func (h *Handler) mapDialError(err error) uint16 {
	if netErr, ok := err.(*net.OpError); ok {
		if netErr.Timeout() {
			return protocol.ErrConnectionTimeout
		}
	}

	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return protocol.ErrHostUnreachable
	}

	return protocol.ErrConnectionRefused
}

// sendOpenErr sends an open error response.
func (h *Handler) sendOpenErr(peerID identity.AgentID, streamID, requestID uint64, errorCode uint16, message string) {
	if h.writer != nil {
		h.writer.WriteStreamOpenErr(peerID, streamID, requestID, errorCode, message)
	}
}

// CloseConnectionsForPeer closes every connection opened by peerID and returns
// how many were closed. Called when the peer disconnects: without it these
// connections linger until their idle timeout, holding real TCP sockets open
// for a peer that can no longer receive their data, and the peer's stream IDs
// restart at 1 on reconnect and collide with the stale entries.
func (h *Handler) CloseConnectionsForPeer(peerID identity.AgentID) int {
	h.mu.Lock()
	var closing []*ActiveConnection
	for key, ac := range h.connections {
		if key.peer != peerID {
			continue
		}
		delete(h.connections, key)
		h.connCount.Add(-1)
		closing = append(closing, ac)
	}
	h.mu.Unlock()

	// Close outside the lock: Close touches the network and the read loop
	// wakes up and re-enters removeConnection, which takes h.mu.
	for _, ac := range closing {
		ac.Close()
	}
	return len(closing)
}

// ConnectionCount returns the number of active connections.
func (h *Handler) ConnectionCount() int64 {
	return h.connCount.Load()
}

// GetConnection returns an active connection by peer and stream ID.
func (h *Handler) GetConnection(peerID identity.AgentID, streamID uint64) *ActiveConnection {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.connections[streamKey{peer: peerID, streamID: streamID}]
}

// SetWriter sets the stream writer.
func (h *Handler) SetWriter(writer StreamWriter) {
	h.writer = writer
}

// ParseAllowedRoutes parses a list of CIDR strings into AllowedRoute entries
// with no source binding. Use AllowedRoute{Network, Source} directly when a
// per-route source IP is needed.
func ParseAllowedRoutes(routes []string) ([]AllowedRoute, error) {
	var result []AllowedRoute
	for _, r := range routes {
		_, ipNet, err := net.ParseCIDR(r)
		if err != nil {
			return nil, fmt.Errorf("invalid CIDR %q: %w", r, err)
		}
		result = append(result, AllowedRoute{Network: ipNet})
	}
	return result, nil
}
