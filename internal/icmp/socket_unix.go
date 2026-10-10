//go:build !windows

package icmp

import (
	"fmt"

	"golang.org/x/net/icmp"
)

// NewSocketV4 creates a new unprivileged IPv4 ICMP socket.
// Uses "udp4" network which allows unprivileged ICMP on Linux when
// net.ipv4.ping_group_range sysctl is properly configured, and is available
// by default on macOS/BSD.
func NewSocketV4() (*Socket, error) {
	conn, err := icmp.ListenPacket("udp4", "0.0.0.0")
	if err != nil {
		return nil, fmt.Errorf("create ICMP socket: %w", err)
	}
	return &Socket{conn: conn, isIPv6: false}, nil
}

// NewSocketV6 creates a new unprivileged IPv6 ICMP socket.
// Uses "udp6" network which allows unprivileged ICMPv6 on Linux when
// net.ipv4.ping_group_range sysctl is properly configured, and is available
// by default on macOS/BSD.
func NewSocketV6() (*Socket, error) {
	conn, err := icmp.ListenPacket("udp6", "::")
	if err != nil {
		return nil, fmt.Errorf("create ICMPv6 socket: %w", err)
	}
	return &Socket{conn: conn, isIPv6: true}, nil
}
