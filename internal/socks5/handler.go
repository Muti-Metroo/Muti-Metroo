package socks5

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/postalsys/muti-metroo/internal/protocol"
)

// SOCKS5 protocol constants per RFC 1928.
const (
	SOCKS5Version = 0x05
)

// udpControlReadDeadline bounds each blocking read on a UDP ASSOCIATE control
// connection so a half-open peer is eventually noticed. Expiry is expected and
// the read simply repeats; only a real error ends the association.
const udpControlReadDeadline = 5 * time.Minute

// Command types.
const (
	CmdConnect      = 0x01
	CmdBind         = 0x02
	CmdUDPAssociate = 0x03
	CmdICMPEcho     = 0x04 // Custom command for ICMP echo
	CmdRouteWatch   = 0x05 // Custom command: stream dashboard snapshots on change
	CmdSnapshot     = 0x06 // Custom command: one-shot dashboard snapshot
	CmdExpose       = 0x07 // Custom command: run an expose session over the raw stream
)

// Address types.
const (
	AddrTypeIPv4   = 0x01
	AddrTypeDomain = 0x03
	AddrTypeIPv6   = 0x04
)

// Reply codes.
const (
	ReplySucceeded          = 0x00
	ReplyServerFailure      = 0x01
	ReplyNotAllowed         = 0x02
	ReplyNetworkUnreachable = 0x03
	ReplyHostUnreachable    = 0x04
	ReplyConnectionRefused  = 0x05
	ReplyTTLExpired         = 0x06
	ReplyCmdNotSupported    = 0x07
	ReplyAddrNotSupported   = 0x08
)

// halfCloser is implemented by connections that support half-close (TCP, mesh connections).
// This allows signaling that one direction is done while keeping the other open.
type halfCloser interface {
	CloseWrite() error
}

// peekConn wraps a net.Conn and routes reads through a bufio.Reader so that a
// single byte can be peeked (ReadByte + UnreadByte) to detect the SOCKS version
// without consuming it from the underlying connection.
type peekConn struct {
	net.Conn
	br *bufio.Reader
}

func (c *peekConn) Read(b []byte) (int, error) { return c.br.Read(b) }

// NoDeadlineMonitor forwards the wrapped connection's opt-out of
// deadline-based disconnect monitoring (see noDeadlineMonitor). Without this
// the wrapper hides the interface from dialWithMonitor, which then polls a
// WebSocket connection with read deadlines and kills it mid-dial.
func (c *peekConn) NoDeadlineMonitor() bool {
	if ndm, ok := c.Conn.(noDeadlineMonitor); ok {
		return ndm.NoDeadlineMonitor()
	}
	return false
}

// errClientDisconnected is returned by dialWithMonitor when the client closes
// the connection before the outbound dial completes.
var errClientDisconnected = errors.New("client disconnected during dial")

// Request represents a SOCKS5 request.
type Request struct {
	Version  byte
	Command  byte
	AddrType byte
	DestAddr string
	DestPort uint16
	DestIP   net.IP
	RawDest  []byte // Raw destination bytes for forwarding
}

// Handler processes SOCKS5 connections.
type Handler struct {
	authenticators []Authenticator
	dialer         Dialer

	// UDP support
	udpHandler      UDPAssociationHandler
	udpBindIP       net.IP // IP to bind UDP relay sockets (inherited from SOCKS5 listener)
	udpAssocMu      sync.Mutex
	udpAssociations map[uint64]*UDPAssociation

	// ICMP support
	icmpHandler      ICMPHandler
	icmpAssocMu      sync.Mutex
	icmpAssociations map[uint64]*ICMPAssociation

	// Route-watch support (dashboard snapshot push over CmdRouteWatch / CmdSnapshot)
	routeWatch RouteWatchProvider

	// Expose support (expose session run over the raw CmdExpose stream)
	expose ExposeOverSocks
}

// Dialer interface for making outbound connections.
type Dialer interface {
	Dial(network, address string) (net.Conn, error)
	// DialContext dials with context support for cancellation.
	// Implementations should cancel the dial when ctx is done.
	DialContext(ctx context.Context, network, address string) (net.Conn, error)
}

// DirectDialer connects directly to destinations.
type DirectDialer struct{}

// Dial makes a direct TCP connection.
func (d *DirectDialer) Dial(network, address string) (net.Conn, error) {
	return net.Dial(network, address)
}

// DialContext makes a direct TCP connection with context support.
func (d *DirectDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	var dialer net.Dialer
	return dialer.DialContext(ctx, network, address)
}

// NewHandler creates a new SOCKS5 handler.
func NewHandler(auths []Authenticator, dialer Dialer) *Handler {
	if dialer == nil {
		dialer = &DirectDialer{}
	}
	if len(auths) == 0 {
		auths = []Authenticator{&NoAuthAuthenticator{}}
	}
	return &Handler{
		authenticators:   auths,
		dialer:           dialer,
		udpAssociations:  make(map[uint64]*UDPAssociation),
		icmpAssociations: make(map[uint64]*ICMPAssociation),
	}
}

// SetUDPHandler sets the UDP association handler.
// This must be called before handling UDP ASSOCIATE requests.
func (h *Handler) SetUDPHandler(handler UDPAssociationHandler) {
	h.udpHandler = handler
}

// SetUDPBindIP sets the IP address for UDP relay sockets.
// This should match the SOCKS5 TCP listener's bind address.
func (h *Handler) SetUDPBindIP(ip net.IP) {
	h.udpBindIP = ip
}

// SetICMPHandler sets the ICMP echo handler.
// This must be called before handling ICMP ECHO requests.
func (h *Handler) SetICMPHandler(handler ICMPHandler) {
	h.icmpHandler = handler
}

// SetRouteWatchHandler sets the route-watch provider that backs the
// CmdRouteWatch (0x05) and CmdSnapshot (0x06) commands. When nil, those
// commands reply ReplyCmdNotSupported so older/unconfigured agents are
// detectable by the client.
func (h *Handler) SetRouteWatchHandler(provider RouteWatchProvider) {
	h.routeWatch = provider
}

// SetExposeHandler sets the handler that backs the CmdExpose (0x07) command.
// When nil, the command replies ReplyCmdNotSupported so older/unconfigured
// agents are detectable by the client.
func (h *Handler) SetExposeHandler(handler ExposeOverSocks) {
	h.expose = handler
}

// Handle processes a connection, auto-detecting SOCKS4/4a or SOCKS5 by the
// first byte (0x04 vs 0x05).
func (h *Handler) Handle(conn net.Conn) error {
	br := bufio.NewReader(conn)
	ver, err := br.ReadByte()
	if err != nil {
		return err
	}
	br.UnreadByte()
	pc := &peekConn{Conn: conn, br: br}

	if ver == SOCKS4Version {
		return h.handleSOCKS4(pc)
	}

	_, err = h.authenticate(pc)
	if err != nil {
		return fmt.Errorf("authentication: %w", err)
	}

	req, err := h.readRequest(pc)
	if err != nil {
		return fmt.Errorf("read request: %w", err)
	}

	switch req.Command {
	case CmdConnect:
		return h.handleConnect(pc, req)
	case CmdUDPAssociate:
		return h.handleUDPAssociate(pc, req)
	case CmdICMPEcho:
		return h.handleICMPEcho(pc, req)
	case CmdRouteWatch:
		return h.handleRouteWatch(pc, req)
	case CmdSnapshot:
		return h.handleSnapshot(pc, req)
	case CmdExpose:
		return h.handleExpose(pc, req)
	default:
		h.sendReply(pc, ReplyCmdNotSupported, nil, 0)
		return fmt.Errorf("unsupported command: %d", req.Command)
	}
}

// noDeadlineMonitor is an optional interface that connections can implement
// to indicate they don't support deadline-based polling for disconnect detection.
// WebSocket connections implement this because their underlying library closes
// the connection when read contexts are canceled, which breaks the polling pattern.
type noDeadlineMonitor interface {
	NoDeadlineMonitor() bool
}

// handleConnect handles CONNECT commands.
func (h *Handler) handleConnect(conn net.Conn, req *Request) error {
	targetAddr := net.JoinHostPort(req.DestAddr, strconv.Itoa(int(req.DestPort)))

	target, err := h.dialWithMonitor(conn, targetAddr)
	if err != nil {
		if !errors.Is(err, errClientDisconnected) {
			h.sendReplyForError(conn, err)
		}
		return err
	}
	defer target.Close()

	localAddr := target.LocalAddr().(*net.TCPAddr)
	h.sendReply(conn, ReplySucceeded, localAddr.IP, uint16(localAddr.Port))

	// Clear the handshake-phase deadline (ServerConfig.IdleTimeout) from both
	// sides: it must not apply to the established tunnel, or long-lived idle
	// connections (e.g. SSH) would be cut mid-session.
	conn.SetDeadline(time.Time{})
	target.SetDeadline(time.Time{})

	return relay(conn, target)
}

// dialWithMonitor dials targetAddr while monitoring conn for early client disconnect.
// If the client closes the connection before the dial completes the context is
// cancelled and errClientDisconnected is returned so callers know not to write
// an error reply to a gone client.
func (h *Handler) dialWithMonitor(conn net.Conn, targetAddr string) (net.Conn, error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Check if connection supports deadline-based monitoring.
	// WebSocket connections don't support this because their library closes
	// the connection when read contexts are canceled, which breaks the polling pattern.
	useMonitor := true
	if ndm, ok := conn.(noDeadlineMonitor); ok && ndm.NoDeadlineMonitor() {
		useMonitor = false
	}

	dialDone := make(chan struct{})
	monitorExited := make(chan struct{})

	if useMonitor {
		// After the proxy handshake the client should not send data until we reply,
		// so any read returning means the client closed the connection.
		go func() {
			defer close(monitorExited)
			buf := make([]byte, 1)
			for {
				select {
				case <-dialDone:
					return
				default:
				}
				conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
				_, err := conn.Read(buf)
				select {
				case <-dialDone:
					return
				default:
				}
				if err != nil {
					if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
						continue
					}
					cancel()
					return
				}
				cancel()
				return
			}
		}()
	} else {
		close(monitorExited)
	}

	target, err := h.dialer.DialContext(ctx, "tcp", targetAddr)
	close(dialDone)

	if useMonitor {
		// Force interrupt any ongoing Read by setting a deadline in the past.
		// This prevents the race where monitor reads data meant for the relay.
		conn.SetReadDeadline(time.Now().Add(-time.Second))
	}
	<-monitorExited
	conn.SetReadDeadline(time.Time{})

	if err != nil {
		if ctx.Err() == context.Canceled {
			return nil, fmt.Errorf("%w to %s", errClientDisconnected, targetAddr)
		}
		return nil, err
	}
	return target, nil
}

// handleUDPAssociate handles UDP ASSOCIATE commands (RFC 1928 Section 4).
// Creates a UDP relay socket and manages the association lifetime.
func (h *Handler) handleUDPAssociate(conn net.Conn, req *Request) error {
	// Check if UDP is enabled
	if h.udpHandler == nil || !h.udpHandler.IsUDPEnabled() {
		h.sendReply(conn, ReplyCmdNotSupported, nil, 0)
		return ErrUDPDisabled
	}

	// Parse expected client address from request
	// The client MAY specify the address/port it will use, or 0.0.0.0:0
	var expectedClient *net.UDPAddr
	if req.DestIP != nil && !req.DestIP.IsUnspecified() {
		expectedClient = &net.UDPAddr{
			IP:   req.DestIP,
			Port: int(req.DestPort),
		}
	}

	// Create UDP association
	// Use the configured bind IP (inherited from SOCKS5 TCP listener)
	assoc, err := NewUDPAssociation(conn, h.udpHandler, h.udpBindIP)
	if err != nil {
		h.sendReply(conn, ReplyServerFailure, nil, 0)
		return fmt.Errorf("create UDP association: %w", err)
	}

	// Set expected client address
	if expectedClient != nil {
		assoc.SetExpectedClientAddr(expectedClient)
	}

	// Create association in mesh (get stream ID)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	streamID, err := h.udpHandler.CreateUDPAssociation(ctx, expectedClient)
	if err != nil {
		assoc.Close()
		h.sendReply(conn, ReplyServerFailure, nil, 0)
		return fmt.Errorf("create mesh association: %w", err)
	}
	assoc.SetStreamID(streamID)

	// Link the SOCKS5 association to the ingress stream for responses
	h.udpHandler.SetSOCKS5UDPAssociation(streamID, assoc)

	// Track the association
	h.udpAssocMu.Lock()
	h.udpAssociations[streamID] = assoc
	h.udpAssocMu.Unlock()

	// Send success reply with relay address
	// Use the TCP connection's local IP (the IP the client connected to)
	// rather than 0.0.0.0 which the client can't send to
	relayAddr := assoc.LocalAddr()
	var replyIP net.IP
	if tcpLocal, ok := conn.LocalAddr().(*net.TCPAddr); ok && !tcpLocal.IP.IsUnspecified() {
		replyIP = tcpLocal.IP
	} else {
		// Fallback to 127.0.0.1 if we can't determine the IP
		replyIP = net.IPv4(127, 0, 0, 1)
	}
	h.sendReply(conn, ReplySucceeded, replyIP, uint16(relayAddr.Port))

	// Start reading from UDP socket
	go assoc.ReadLoop()

	// Wait for TCP control connection to close
	// Per RFC 1928: "A UDP association terminates when the TCP connection
	// that the UDP ASSOCIATE request arrived terminates."
	// Use periodic read deadlines to detect half-open connections.
	//
	// A deadline expiry is the normal case, not a reason to tear down: RFC 1928
	// clients send nothing on this connection for the life of the association,
	// so treating a timeout like a close would kill every association after one
	// deadline regardless of UDP activity. Only a real read error (EOF, reset)
	// ends the association.
	buf := make([]byte, 1)
	for {
		conn.SetReadDeadline(time.Now().Add(udpControlReadDeadline))
		if _, err := conn.Read(buf); err != nil && !os.IsTimeout(err) {
			break
		}
	}

	// Clean up
	h.udpAssocMu.Lock()
	delete(h.udpAssociations, streamID)
	h.udpAssocMu.Unlock()

	assoc.Close()
	return nil
}

// handleICMPEcho handles ICMP ECHO commands (custom command 0x04).
// Creates an ICMP session through the mesh for ping operations.
func (h *Handler) handleICMPEcho(conn net.Conn, req *Request) error {
	// Check if ICMP is enabled
	if h.icmpHandler == nil || !h.icmpHandler.IsICMPEnabled() {
		h.sendReply(conn, ReplyCmdNotSupported, nil, 0)
		return ErrICMPDisabled
	}

	// The destination must be an IP address for ICMP
	if req.DestIP == nil || req.DestIP.IsUnspecified() {
		h.sendReply(conn, ReplyAddrNotSupported, nil, 0)
		return fmt.Errorf("ICMP requires IP address destination")
	}

	// Create ICMP association
	assoc, err := NewICMPAssociation(conn, h.icmpHandler, req.DestIP)
	if err != nil {
		h.sendReply(conn, ReplyServerFailure, nil, 0)
		return fmt.Errorf("create ICMP association: %w", err)
	}

	// Create association in mesh (get stream ID and perform key exchange)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	streamID, err := h.icmpHandler.CreateICMPSession(ctx, req.DestIP)
	if err != nil {
		assoc.Close()
		h.sendReply(conn, ReplyServerFailure, nil, 0)
		return fmt.Errorf("create mesh ICMP session: %w", err)
	}
	assoc.SetStreamID(streamID)

	// Link the SOCKS5 association to the ingress stream for responses
	h.icmpHandler.SetSOCKS5ICMPAssociation(streamID, assoc)

	// Track the association
	h.icmpAssocMu.Lock()
	h.icmpAssociations[streamID] = assoc
	h.icmpAssocMu.Unlock()

	// Send success reply
	h.sendReply(conn, ReplySucceeded, nil, 0)

	// Clear deadlines
	conn.SetDeadline(time.Time{})

	// Start ICMP echo relay loop
	// Reads echo requests from client, forwards through mesh
	err = assoc.RelayLoop()

	// Clean up
	h.icmpAssocMu.Lock()
	delete(h.icmpAssociations, streamID)
	h.icmpAssocMu.Unlock()

	assoc.Close()
	return err
}

// GetICMPAssociation returns an ICMP association by stream ID.
func (h *Handler) GetICMPAssociation(streamID uint64) *ICMPAssociation {
	h.icmpAssocMu.Lock()
	defer h.icmpAssocMu.Unlock()
	return h.icmpAssociations[streamID]
}

// GetUDPAssociation returns a UDP association by stream ID.
func (h *Handler) GetUDPAssociation(streamID uint64) *UDPAssociation {
	h.udpAssocMu.Lock()
	defer h.udpAssocMu.Unlock()
	return h.udpAssociations[streamID]
}

// noAuthAllowed reports whether the server permits unauthenticated connections,
// i.e. whether any of its authenticators accepts the no-auth method (0x00).
// SOCKS4 has no auth mechanism so it is only allowed when no-auth is permitted.
func (h *Handler) noAuthAllowed() bool {
	for _, a := range h.authenticators {
		if _, ok := a.(*NoAuthAuthenticator); ok {
			return true
		}
	}
	return false
}

// authenticate performs the authentication handshake.
func (h *Handler) authenticate(conn net.Conn) (string, error) {
	// Read the greeting
	// +----+----------+----------+
	// |VER | NMETHODS | METHODS  |
	// +----+----------+----------+
	// | 1  |    1     | 1 to 255 |
	// +----+----------+----------+

	header := make([]byte, 2)
	if _, err := io.ReadFull(conn, header); err != nil {
		return "", err
	}

	if header[0] != SOCKS5Version {
		return "", fmt.Errorf("unsupported SOCKS version: %d", header[0])
	}

	numMethods := int(header[1])
	methods := make([]byte, numMethods)
	if _, err := io.ReadFull(conn, methods); err != nil {
		return "", err
	}

	// Select authentication method
	var selectedAuth Authenticator
	for _, auth := range h.authenticators {
		for _, m := range methods {
			if m == auth.GetMethod() {
				selectedAuth = auth
				break
			}
		}
		if selectedAuth != nil {
			break
		}
	}

	if selectedAuth == nil {
		// No acceptable method
		conn.Write([]byte{SOCKS5Version, AuthMethodNoAcceptable})
		return "", errors.New("no acceptable authentication method")
	}

	// Send method selection
	// +----+--------+
	// |VER | METHOD |
	// +----+--------+
	// | 1  |   1    |
	// +----+--------+
	if _, err := conn.Write([]byte{SOCKS5Version, selectedAuth.GetMethod()}); err != nil {
		return "", err
	}

	// Perform authentication
	return selectedAuth.Authenticate(conn, conn)
}

// readRequest reads the SOCKS5 request.
func (h *Handler) readRequest(conn net.Conn) (*Request, error) {
	// +----+-----+-------+------+----------+----------+
	// |VER | CMD |  RSV  | ATYP | DST.ADDR | DST.PORT |
	// +----+-----+-------+------+----------+----------+
	// | 1  |  1  | X'00' |  1   | Variable |    2     |
	// +----+-----+-------+------+----------+----------+

	header := make([]byte, 4)
	if _, err := io.ReadFull(conn, header); err != nil {
		return nil, err
	}

	if header[0] != SOCKS5Version {
		return nil, fmt.Errorf("unsupported SOCKS version: %d", header[0])
	}

	req := &Request{
		Version:  header[0],
		Command:  header[1],
		AddrType: header[3],
	}

	// Read destination address based on type
	switch req.AddrType {
	case AddrTypeIPv4:
		addr := make([]byte, 4)
		if _, err := io.ReadFull(conn, addr); err != nil {
			return nil, err
		}
		req.DestIP = net.IP(addr)
		req.DestAddr = req.DestIP.String()
		req.RawDest = addr

	case AddrTypeDomain:
		// Read domain length
		lenBuf := make([]byte, 1)
		if _, err := io.ReadFull(conn, lenBuf); err != nil {
			return nil, err
		}
		domainLen := int(lenBuf[0])
		if domainLen == 0 {
			h.sendReply(conn, ReplyServerFailure, nil, 0)
			return nil, fmt.Errorf("invalid zero-length domain name")
		}
		domain := make([]byte, domainLen)
		if _, err := io.ReadFull(conn, domain); err != nil {
			return nil, err
		}
		req.DestAddr = string(domain)
		req.RawDest = append(lenBuf, domain...)

	case AddrTypeIPv6:
		addr := make([]byte, 16)
		if _, err := io.ReadFull(conn, addr); err != nil {
			return nil, err
		}
		req.DestIP = net.IP(addr)
		req.DestAddr = req.DestIP.String()
		req.RawDest = addr

	default:
		h.sendReply(conn, ReplyAddrNotSupported, nil, 0)
		return nil, fmt.Errorf("unsupported address type: %d", req.AddrType)
	}

	// Read port
	portBuf := make([]byte, 2)
	if _, err := io.ReadFull(conn, portBuf); err != nil {
		return nil, err
	}
	req.DestPort = binary.BigEndian.Uint16(portBuf)

	return req, nil
}

// sendReply sends a SOCKS5 reply.
func (h *Handler) sendReply(conn net.Conn, reply byte, bindIP net.IP, bindPort uint16) error {
	// +----+-----+-------+------+----------+----------+
	// |VER | REP |  RSV  | ATYP | BND.ADDR | BND.PORT |
	// +----+-----+-------+------+----------+----------+
	// | 1  |  1  | X'00' |  1   | Variable |    2     |
	// +----+-----+-------+------+----------+----------+

	// Determine address type and normalized IP
	var addrType byte
	var addrBytes []byte

	if ipv4 := bindIP.To4(); ipv4 != nil {
		addrType = AddrTypeIPv4
		addrBytes = ipv4
	} else if bindIP != nil {
		addrType = AddrTypeIPv6
		addrBytes = bindIP
	} else {
		addrType = AddrTypeIPv4
		addrBytes = make([]byte, 4) // 0.0.0.0
	}

	// Build response: VER(1) + REP(1) + RSV(1) + ATYP(1) + ADDR(4 or 16) + PORT(2)
	buf := make([]byte, 4+len(addrBytes)+2)
	buf[0] = SOCKS5Version
	buf[1] = reply
	buf[2] = 0x00 // RSV
	buf[3] = addrType
	copy(buf[4:], addrBytes)
	binary.BigEndian.PutUint16(buf[4+len(addrBytes):], bindPort)

	_, err := conn.Write(buf)
	return err
}

// sendReplyForError maps network errors to SOCKS5 reply codes.
func (h *Handler) sendReplyForError(conn net.Conn, err error) {
	reply := mapErrorToReply(err)
	h.sendReply(conn, reply, nil, 0)
}

// mapErrorToReply converts a dial error into the appropriate SOCKS5 reply
// code, preferring mesh-level information from *protocol.DialError over
// Go-level network-error types.
//
// Mapping a bare dial timeout to 0x06 (TTL expired) follows curl's convention
// since SOCKS5 defines no dedicated connection-timeout reply.
func mapErrorToReply(err error) byte {
	var dialErr *protocol.DialError
	if errors.As(err, &dialErr) {
		return meshErrorToSOCKS5(dialErr.ErrorCode)
	}

	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return ReplyHostUnreachable
	}

	if netErr, ok := err.(*net.OpError); ok {
		if netErr.Timeout() {
			return ReplyTTLExpired
		}
		if netErr.Op == "dial" {
			return ReplyHostUnreachable
		}
	}

	return ReplyServerFailure
}

// meshErrorToSOCKS5 maps a mesh error code onto a SOCKS5 reply per RFC 1928.
// Codes with no direct analogue collapse to 0x01 general failure.
func meshErrorToSOCKS5(code uint16) byte {
	switch code {
	case protocol.ErrNoRoute,
		protocol.ErrHostUnreachable,
		protocol.ErrDNSError:
		return ReplyHostUnreachable
	case protocol.ErrNetworkUnreachable:
		return ReplyNetworkUnreachable
	case protocol.ErrConnectionRefused:
		return ReplyConnectionRefused
	case protocol.ErrConnectionTimeout,
		protocol.ErrTTLExceeded:
		return ReplyTTLExpired
	case protocol.ErrNotAllowed,
		protocol.ErrPathNotAllowed:
		return ReplyNotAllowed
	default:
		return ReplyServerFailure
	}
}

// relay copies data bidirectionally between two connections.
// Supports half-close on any connection type that implements halfCloser interface.
func relay(client, target net.Conn) error {
	errCh := make(chan error, 2)

	go func() {
		_, err := io.Copy(target, client)
		// Signal target that we're done writing (half-close)
		if hc, ok := target.(halfCloser); ok {
			hc.CloseWrite()
		}
		errCh <- err
	}()

	go func() {
		_, err := io.Copy(client, target)
		// Signal client that we're done writing (half-close)
		if hc, ok := client.(halfCloser); ok {
			hc.CloseWrite()
		}
		errCh <- err
	}()

	// Wait for both directions to complete
	err1 := <-errCh
	err2 := <-errCh

	if err1 != nil {
		return err1
	}
	return err2
}
