package socks5

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"time"
)

const (
	SOCKS4Version    = 0x04
	socks4CmdConnect = 0x01
	socks4Granted    = 0x5A
	socks4Rejected   = 0x5B
)

// maxSOCKS4FieldLen caps the userid and domain fields to prevent DoS via
// unbounded reads from clients that never send a NUL terminator.
const maxSOCKS4FieldLen = 255

func (h *Handler) handleSOCKS4(conn *peekConn) error {
	if !h.noAuthAllowed() {
		sendSOCKS4Reply(conn, socks4Rejected, nil, 0)
		return fmt.Errorf("SOCKS4 rejected: server requires authentication")
	}

	req, err := parseSOCKS4Request(conn.br)
	if err != nil {
		return fmt.Errorf("SOCKS4 parse: %w", err)
	}

	if req.Command != socks4CmdConnect {
		sendSOCKS4Reply(conn, socks4Rejected, nil, 0)
		return fmt.Errorf("SOCKS4 unsupported command: %d", req.Command)
	}

	targetAddr := net.JoinHostPort(req.DestAddr, strconv.Itoa(int(req.DestPort)))

	target, err := h.dialWithMonitor(conn, targetAddr)
	if err != nil {
		if !errors.Is(err, errClientDisconnected) {
			sendSOCKS4Reply(conn, socks4Rejected, nil, 0)
		}
		return fmt.Errorf("SOCKS4 dial %s: %w", targetAddr, err)
	}
	defer target.Close()

	localAddr := target.LocalAddr().(*net.TCPAddr)
	sendSOCKS4Reply(conn, socks4Granted, localAddr.IP, uint16(localAddr.Port))

	conn.SetDeadline(time.Time{})
	target.SetDeadline(time.Time{})

	return relay(conn, target)
}

// parseSOCKS4Request reads and parses a SOCKS4/4a request.
// Wire format: VER(1) CMD(1) DSTPORT(2) DSTIP(4) USERID\0 [DOMAIN\0 for SOCKS4a]
func parseSOCKS4Request(br *bufio.Reader) (*Request, error) {
	buf := make([]byte, 8)
	if _, err := io.ReadFull(br, buf); err != nil {
		return nil, err
	}

	cmd := buf[1]
	port := binary.BigEndian.Uint16(buf[2:4])
	rawIP := buf[4:8]

	if _, err := readNullTerminatedBytes(br); err != nil {
		return nil, fmt.Errorf("SOCKS4 read userid: %w", err)
	}

	req := &Request{
		Version:  SOCKS4Version,
		Command:  cmd,
		DestPort: port,
	}

	// SOCKS4a sentinel: IP 0.0.0.x with x != 0 means a hostname follows the userid.
	if rawIP[0] == 0 && rawIP[1] == 0 && rawIP[2] == 0 && rawIP[3] != 0 {
		domain, err := readNullTerminatedBytes(br)
		if err != nil {
			return nil, fmt.Errorf("SOCKS4a read domain: %w", err)
		}
		if len(domain) == 0 {
			return nil, fmt.Errorf("SOCKS4a empty domain name")
		}
		req.DestAddr = string(domain)
		req.AddrType = AddrTypeDomain
	} else {
		ip := make(net.IP, 4)
		copy(ip, rawIP)
		req.DestIP = ip
		req.DestAddr = ip.String()
		req.AddrType = AddrTypeIPv4
	}

	return req, nil
}

// readNullTerminatedBytes reads up to maxSOCKS4FieldLen bytes followed by a NUL.
// The cap protects against a client that never sends a terminator.
func readNullTerminatedBytes(br *bufio.Reader) ([]byte, error) {
	line, err := br.ReadSlice(0)
	if err != nil {
		return nil, err
	}
	if len(line)-1 > maxSOCKS4FieldLen {
		return nil, fmt.Errorf("field exceeds maximum length of %d bytes", maxSOCKS4FieldLen)
	}
	out := make([]byte, len(line)-1)
	copy(out, line[:len(line)-1])
	return out, nil
}

// sendSOCKS4Reply writes a SOCKS4 reply to conn.
// Format: [0x00][REP][PORT:2be][IP:4]. The leading byte is 0x00, not the
// version 0x04 - this is per the SOCKS4 spec ("VN" reply field).
func sendSOCKS4Reply(conn net.Conn, rep byte, ip net.IP, port uint16) error {
	buf := make([]byte, 8)
	buf[0] = 0x00
	buf[1] = rep
	binary.BigEndian.PutUint16(buf[2:4], port)
	if ip4 := ip.To4(); ip4 != nil {
		copy(buf[4:8], ip4)
	}
	_, err := conn.Write(buf)
	return err
}
