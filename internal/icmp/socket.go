package icmp

import (
	"fmt"
	"net"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

// IANA protocol numbers for ICMP
const (
	ICMPv4ProtocolNumber = 1
	ICMPv6ProtocolNumber = 58
)

// Socket wraps an ICMP packet connection with IP version information.
type Socket struct {
	conn   *icmp.PacketConn
	isIPv6 bool
}

// NewSocket creates an ICMP socket for the given IP address.
// For IPv4 addresses, creates an IPv4 ICMP socket.
// For IPv6 addresses, creates an IPv6 ICMP socket.
//
// On Windows this returns an error immediately because unprivileged ICMP
// sockets are not available on that platform (see socket_windows.go).
func NewSocket(destIP net.IP) (*Socket, error) {
	isIPv6 := destIP.To4() == nil

	if isIPv6 {
		return NewSocketV6()
	}
	return NewSocketV4()
}

// Close closes the socket.
func (s *Socket) Close() error {
	return s.conn.Close()
}

// SendEchoRequest sends an ICMP echo request to the destination.
func (s *Socket) SendEchoRequest(destIP net.IP, id, seq uint16, payload []byte) error {
	var msgType icmp.Type
	var destAddr net.Addr

	if s.isIPv6 {
		msgType = ipv6.ICMPTypeEchoRequest
		destAddr = &net.UDPAddr{IP: destIP.To16()}
	} else {
		msgType = ipv4.ICMPTypeEcho
		ip4 := destIP.To4()
		if ip4 == nil {
			return fmt.Errorf("IPv4 socket cannot send to IPv6 address %s", destIP)
		}
		destAddr = &net.UDPAddr{IP: ip4}
	}

	msg := icmp.Message{
		Type: msgType,
		Code: 0,
		Body: &icmp.Echo{
			ID:   int(id),
			Seq:  int(seq),
			Data: payload,
		},
	}

	msgBytes, err := msg.Marshal(nil)
	if err != nil {
		return fmt.Errorf("marshal ICMP message: %w", err)
	}

	_, err = s.conn.WriteTo(msgBytes, destAddr)
	if err != nil {
		return fmt.Errorf("send ICMP: %w", err)
	}

	return nil
}

// EchoReply contains the parsed ICMP echo reply data.
type EchoReply struct {
	ID      uint16
	Seq     uint16
	Payload []byte
	SrcIP   net.IP
}

// ReadEchoReply reads an ICMP echo reply from the socket.
// Returns the reply data or an error if timeout is reached.
func (s *Socket) ReadEchoReply(timeout time.Duration) (*EchoReply, error) {
	if err := s.conn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return nil, fmt.Errorf("set read deadline: %w", err)
	}

	buf := make([]byte, 1500)
	n, peer, err := s.conn.ReadFrom(buf)
	if err != nil {
		return nil, err // Timeout or other error
	}

	// Parse ICMP message based on IP version
	var protoNum int
	var expectedType icmp.Type

	if s.isIPv6 {
		protoNum = ICMPv6ProtocolNumber
		expectedType = ipv6.ICMPTypeEchoReply
	} else {
		protoNum = ICMPv4ProtocolNumber
		expectedType = ipv4.ICMPTypeEchoReply
	}

	msg, err := icmp.ParseMessage(protoNum, buf[:n])
	if err != nil {
		return nil, fmt.Errorf("parse ICMP: %w", err)
	}

	// Check for echo reply
	if msg.Type != expectedType {
		return nil, fmt.Errorf("unexpected ICMP type: %v (expected %v)", msg.Type, expectedType)
	}

	echo, ok := msg.Body.(*icmp.Echo)
	if !ok {
		return nil, fmt.Errorf("invalid echo body")
	}

	// Extract source IP
	var srcIP net.IP
	switch addr := peer.(type) {
	case *net.UDPAddr:
		srcIP = addr.IP
	case *net.IPAddr:
		srcIP = addr.IP
	default:
		srcIP = nil
	}

	return &EchoReply{
		ID:      uint16(echo.ID),
		Seq:     uint16(echo.Seq),
		Payload: echo.Data,
		SrcIP:   srcIP,
	}, nil
}

// ReadEchoReplyFiltered reads an echo reply with timeout.
// Note: For unprivileged ICMP sockets, we accept any reply since
// each session has its own socket and the kernel may assign different IDs.
func (s *Socket) ReadEchoReplyFiltered(expectedID uint16, timeout time.Duration) (*EchoReply, error) {
	return s.ReadEchoReply(timeout)
}

// IsIPv6 returns true if this socket is for IPv6.
func (s *Socket) IsIPv6() bool {
	return s.isIPv6
}
