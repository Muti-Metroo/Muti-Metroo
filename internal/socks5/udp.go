package socks5

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
)

// MaxStreamDatagramFrame is the largest UDP STREAM frame body (RFC 1928 UDP
// header plus payload), bounded by the 2-byte length prefix.
const MaxStreamDatagramFrame = 0xFFFF

var (
	// ErrFragmentedDatagram is returned when a fragmented UDP datagram is received.
	// Fragmentation is not supported.
	ErrFragmentedDatagram = errors.New("fragmented datagrams not supported")

	// ErrUDPDisabled is returned when UDP relay is disabled.
	ErrUDPDisabled = errors.New("UDP relay is disabled")

	// ErrICMPDisabled is returned when ICMP echo is disabled.
	ErrICMPDisabled = errors.New("ICMP echo is disabled")
)

// UDPAssociationHandler is the interface for handling UDP associations.
// Implemented by the agent to relay datagrams through the mesh.
type UDPAssociationHandler interface {
	// CreateUDPAssociation creates a new UDP association for a SOCKS5 client.
	// Returns the stream ID for the association, or an error if disabled.
	CreateUDPAssociation(ctx context.Context, clientAddr *net.UDPAddr) (streamID uint64, err error)

	// SetSOCKS5UDPAssociation links a SOCKS5 UDP association to an ingress stream.
	// This allows responses to be forwarded back to the SOCKS5 client.
	SetSOCKS5UDPAssociation(streamID uint64, assoc *UDPAssociation)

	// RelayUDPDatagram relays a UDP datagram through the mesh.
	// The datagram is forwarded to the exit node for delivery.
	RelayUDPDatagram(streamID uint64, destAddr net.Addr, destPort uint16, addrType byte, rawAddr []byte, data []byte) error

	// CloseUDPAssociation closes a UDP association.
	CloseUDPAssociation(streamID uint64)

	// IsUDPEnabled returns whether UDP relay is enabled.
	IsUDPEnabled() bool
}

// UDPAssociation represents an active SOCKS5 UDP association.
// Created when a client sends UDP ASSOCIATE.
type UDPAssociation struct {
	// TCP control connection (lifetime tied to association)
	TCPConn net.Conn

	// Local UDP relay socket (nil in stream mode)
	UDPConn *net.UDPConn

	// Stream mode (UDP STREAM): datagrams are length-prefixed frames on
	// TCPConn instead of packets on UDPConn. streamWriteMu serializes
	// frame writes.
	stream        bool
	streamWriteMu sync.Mutex

	// Expected client address (from UDP ASSOCIATE request)
	ExpectedClientAddr *net.UDPAddr

	// Actual client address (first datagram received)
	ActualClientAddr *net.UDPAddr

	// Stream ID for mesh routing
	StreamID uint64

	// Handler for relaying through mesh
	Handler UDPAssociationHandler

	// Cancellation
	ctx    context.Context
	cancel context.CancelFunc

	// State
	closed atomic.Bool
	mu     sync.RWMutex
}

// NewUDPAssociation creates a new UDP association.
// bindIP specifies the IP to bind the UDP relay socket to (nil defaults to 0.0.0.0).
func NewUDPAssociation(tcpConn net.Conn, handler UDPAssociationHandler, bindIP net.IP) (*UDPAssociation, error) {
	ctx, cancel := context.WithCancel(context.Background())

	// Create UDP relay socket
	// Use "udp4" to force IPv4 - on macOS "udp" creates a dual-stack IPv6 socket
	// which reports [::] as the local address and causes issues with SOCKS5 clients
	// Bind to the same IP as the SOCKS5 TCP listener for security
	udpBindIP := net.IPv4zero
	if bindIP != nil && !bindIP.IsUnspecified() {
		udpBindIP = bindIP
	}
	udpConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: udpBindIP, Port: 0})
	if err != nil {
		cancel()
		return nil, fmt.Errorf("create UDP socket: %w", err)
	}

	return &UDPAssociation{
		TCPConn: tcpConn,
		UDPConn: udpConn,
		Handler: handler,
		ctx:     ctx,
		cancel:  cancel,
	}, nil
}

// NewStreamUDPAssociation creates a UDP association whose datagrams are
// framed on conn (UDP STREAM) instead of a separate UDP relay socket.
func NewStreamUDPAssociation(conn net.Conn, handler UDPAssociationHandler) *UDPAssociation {
	ctx, cancel := context.WithCancel(context.Background())
	return &UDPAssociation{
		TCPConn: conn,
		Handler: handler,
		stream:  true,
		ctx:     ctx,
		cancel:  cancel,
	}
}

// LocalAddr returns the local address of the UDP relay socket.
func (a *UDPAssociation) LocalAddr() *net.UDPAddr {
	return a.UDPConn.LocalAddr().(*net.UDPAddr)
}

// SetStreamID sets the stream ID for mesh routing.
func (a *UDPAssociation) SetStreamID(id uint64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.StreamID = id
}

// GetStreamID returns the stream ID.
func (a *UDPAssociation) GetStreamID() uint64 {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.StreamID
}

// SetExpectedClientAddr sets the expected client address from UDP ASSOCIATE.
func (a *UDPAssociation) SetExpectedClientAddr(addr *net.UDPAddr) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.ExpectedClientAddr = addr
}

// Close terminates the association and releases resources.
func (a *UDPAssociation) Close() error {
	if a.closed.Swap(true) {
		return nil // Already closed
	}

	a.cancel()

	// Close UDP socket
	if a.UDPConn != nil {
		a.UDPConn.Close()
	}

	// In stream mode the client connection carries the datagrams, so closing
	// it is what tells the client the association is gone.
	if a.stream && a.TCPConn != nil {
		a.TCPConn.Close()
	}

	// Notify handler
	a.mu.RLock()
	streamID := a.StreamID
	a.mu.RUnlock()

	if a.Handler != nil && streamID != 0 {
		a.Handler.CloseUDPAssociation(streamID)
	}

	return nil
}

// IsClosed returns true if the association is closed.
func (a *UDPAssociation) IsClosed() bool {
	return a.closed.Load()
}

// Context returns the association's context.
func (a *UDPAssociation) Context() context.Context {
	return a.ctx
}

// ReadLoop reads datagrams from the SOCKS5 client and relays them through the mesh.
// This should be run in a goroutine.
func (a *UDPAssociation) ReadLoop() {
	buf := make([]byte, 65535) // Max UDP datagram size

	for {
		select {
		case <-a.ctx.Done():
			return
		default:
		}

		n, clientAddr, err := a.UDPConn.ReadFromUDP(buf)
		if err != nil {
			if a.IsClosed() {
				return
			}
			continue
		}

		// Update actual client address on first datagram
		a.mu.Lock()
		if a.ActualClientAddr == nil {
			a.ActualClientAddr = clientAddr
		}
		a.mu.Unlock()

		// Verify client address if expected address was specified
		a.mu.RLock()
		expected := a.ExpectedClientAddr
		a.mu.RUnlock()

		if expected != nil && expected.IP != nil && !expected.IP.IsUnspecified() {
			if !clientAddr.IP.Equal(expected.IP) {
				// Ignore datagrams from unexpected addresses
				continue
			}
		}

		// Parse SOCKS5 UDP header
		header, payload, err := ParseUDPHeader(buf[:n])
		if err != nil {
			// Invalid header, ignore
			continue
		}

		// Relay through mesh
		a.mu.RLock()
		streamID := a.StreamID
		handler := a.Handler
		a.mu.RUnlock()

		if handler != nil && streamID != 0 {
			// Build destination address (domain names have nil IP, resolved at exit)
			destAddr := &net.UDPAddr{IP: header.Address, Port: int(header.Port)}
			handler.RelayUDPDatagram(streamID, destAddr, header.Port, header.AddrType, header.RawAddr, payload)
		}
	}
}

// StreamReadLoop reads framed datagrams from the client connection of a
// stream-mode association and relays them through the mesh. It returns when
// the connection fails or a frame is malformed at the framing level.
func (a *UDPAssociation) StreamReadLoop() {
	var lenBuf [2]byte
	buf := make([]byte, MaxStreamDatagramFrame)

	for {
		if _, err := io.ReadFull(a.TCPConn, lenBuf[:]); err != nil {
			return
		}
		n := int(binary.BigEndian.Uint16(lenBuf[:]))
		if _, err := io.ReadFull(a.TCPConn, buf[:n]); err != nil {
			return
		}

		header, payload, err := ParseUDPHeader(buf[:n])
		if err != nil {
			// Same as the socket path: drop the datagram, keep the association.
			continue
		}

		a.mu.RLock()
		streamID := a.StreamID
		handler := a.Handler
		a.mu.RUnlock()

		if handler != nil && streamID != 0 {
			destAddr := &net.UDPAddr{IP: header.Address, Port: int(header.Port)}
			handler.RelayUDPDatagram(streamID, destAddr, header.Port, header.AddrType, header.RawAddr, payload)
		}
	}
}

// WriteToClient sends a datagram back to the SOCKS5 client.
// The data should be the raw UDP payload (will be wrapped with SOCKS5 header).
func (a *UDPAssociation) WriteToClient(addrType byte, addr []byte, port uint16, data []byte) error {
	if a.IsClosed() {
		return errors.New("association closed")
	}

	// Build SOCKS5 UDP header
	header := BuildUDPHeader(addrType, addr, port)

	if a.stream {
		size := len(header) + len(data)
		if size > MaxStreamDatagramFrame {
			return fmt.Errorf("datagram too large for stream frame: %d bytes", size)
		}
		frame := make([]byte, 2+size)
		binary.BigEndian.PutUint16(frame, uint16(size))
		copy(frame[2:], header)
		copy(frame[2+len(header):], data)

		a.streamWriteMu.Lock()
		defer a.streamWriteMu.Unlock()
		_, err := a.TCPConn.Write(frame)
		return err
	}

	a.mu.RLock()
	clientAddr := a.ActualClientAddr
	a.mu.RUnlock()

	if clientAddr == nil {
		return errors.New("no client address")
	}

	// Combine header and data
	packet := make([]byte, len(header)+len(data))
	copy(packet, header)
	copy(packet[len(header):], data)

	_, err := a.UDPConn.WriteToUDP(packet, clientAddr)
	return err
}

// UDPHeader represents the SOCKS5 UDP request header.
// RFC 1928 Section 7.
type UDPHeader struct {
	Frag     byte   // Fragment number (0 = no fragmentation)
	AddrType byte   // Address type
	Address  net.IP // Destination IP (nil for domain)
	Domain   string // Destination domain (empty for IP)
	Port     uint16 // Destination port
	RawAddr  []byte // Raw address bytes for forwarding
}

// ParseUDPHeader parses a SOCKS5 UDP header from a datagram.
// Returns the header and the payload data.
//
// UDP Request Header:
// +----+------+------+----------+----------+----------+
// |RSV | FRAG | ATYP | DST.ADDR | DST.PORT |   DATA   |
// +----+------+------+----------+----------+----------+
// | 2  |  1   |  1   | Variable |    2     | Variable |
// +----+------+------+----------+----------+----------+
func ParseUDPHeader(data []byte) (*UDPHeader, []byte, error) {
	if len(data) < 10 { // Minimum: 2 (RSV) + 1 (FRAG) + 1 (ATYP) + 4 (IPv4) + 2 (PORT)
		return nil, nil, errors.New("datagram too short")
	}

	// Check fragmentation - we don't support it
	frag := data[2]
	if frag != 0 {
		return nil, nil, ErrFragmentedDatagram
	}

	header := &UDPHeader{
		Frag:     frag,
		AddrType: data[3],
	}

	offset := 4 // Start after RSV + FRAG + ATYP

	switch header.AddrType {
	case AddrTypeIPv4:
		if len(data) < offset+4+2 {
			return nil, nil, errors.New("datagram too short for IPv4")
		}
		header.Address = net.IP(data[offset : offset+4])
		header.RawAddr = data[offset : offset+4]
		offset += 4

	case AddrTypeDomain:
		if len(data) < offset+1 {
			return nil, nil, errors.New("datagram too short for domain length")
		}
		domainLen := int(data[offset])
		offset++
		if len(data) < offset+domainLen+2 {
			return nil, nil, errors.New("datagram too short for domain")
		}
		header.Domain = string(data[offset : offset+domainLen])
		header.RawAddr = data[offset-1 : offset+domainLen] // Include length byte
		offset += domainLen

	case AddrTypeIPv6:
		if len(data) < offset+16+2 {
			return nil, nil, errors.New("datagram too short for IPv6")
		}
		header.Address = net.IP(data[offset : offset+16])
		header.RawAddr = data[offset : offset+16]
		offset += 16

	default:
		return nil, nil, fmt.Errorf("unsupported address type: %d", header.AddrType)
	}

	// Read port
	if len(data) < offset+2 {
		return nil, nil, errors.New("datagram too short for port")
	}
	header.Port = binary.BigEndian.Uint16(data[offset:])
	offset += 2

	// Return payload
	return header, data[offset:], nil
}

// BuildUDPHeader creates a SOCKS5 UDP header.
func BuildUDPHeader(addrType byte, addr []byte, port uint16) []byte {
	// RSV(2) + FRAG(1) + ATYP(1) + ADDR(var) + PORT(2)
	headerLen := 4 + len(addr) + 2
	header := make([]byte, headerLen)

	// RSV (2 bytes, must be 0)
	header[0] = 0
	header[1] = 0

	// FRAG (1 byte, 0 = no fragmentation)
	header[2] = 0

	// ATYP (1 byte)
	header[3] = addrType

	// Address
	copy(header[4:], addr)

	// Port
	binary.BigEndian.PutUint16(header[4+len(addr):], port)

	return header
}
