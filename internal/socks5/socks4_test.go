package socks5

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"strconv"
	"testing"
	"time"
)

// buildSOCKS4Request constructs a raw SOCKS4 request.
// If domain is non-empty the IP is set to 0.0.0.1 (SOCKS4a sentinel).
func buildSOCKS4Request(cmd byte, ip net.IP, port uint16, userid, domain string) []byte {
	buf := &bytes.Buffer{}
	buf.WriteByte(SOCKS4Version)
	buf.WriteByte(cmd)
	binary.Write(buf, binary.BigEndian, port)
	if domain != "" {
		buf.Write([]byte{0, 0, 0, 1})
	} else {
		buf.Write(ip.To4())
	}
	buf.WriteString(userid)
	buf.WriteByte(0)
	if domain != "" {
		buf.WriteString(domain)
		buf.WriteByte(0)
	}
	return buf.Bytes()
}

func parseSOCKS4Bytes(data []byte) (*Request, error) {
	return parseSOCKS4Request(bufio.NewReader(bytes.NewReader(data)))
}

func TestParseSOCKS4Request_IPv4(t *testing.T) {
	ip := net.ParseIP("127.0.0.1")
	data := buildSOCKS4Request(socks4CmdConnect, ip, 8080, "user", "")

	req, err := parseSOCKS4Bytes(data)
	if err != nil {
		t.Fatalf("parseSOCKS4Request() error = %v", err)
	}

	if req.DestAddr != "127.0.0.1" {
		t.Errorf("DestAddr = %q, want %q", req.DestAddr, "127.0.0.1")
	}
	if req.DestPort != 8080 {
		t.Errorf("DestPort = %d, want 8080", req.DestPort)
	}
	if req.AddrType != AddrTypeIPv4 {
		t.Errorf("AddrType = %d, want IPv4", req.AddrType)
	}
	if req.Command != socks4CmdConnect {
		t.Errorf("Command = %d, want CONNECT", req.Command)
	}
}

func TestParseSOCKS4Request_SOCKS4a(t *testing.T) {
	data := buildSOCKS4Request(socks4CmdConnect, nil, 443, "", "example.com")

	req, err := parseSOCKS4Bytes(data)
	if err != nil {
		t.Fatalf("parseSOCKS4Request() error = %v", err)
	}

	if req.DestAddr != "example.com" {
		t.Errorf("DestAddr = %q, want %q", req.DestAddr, "example.com")
	}
	if req.DestPort != 443 {
		t.Errorf("DestPort = %d, want 443", req.DestPort)
	}
	if req.AddrType != AddrTypeDomain {
		t.Errorf("AddrType = %d, want Domain", req.AddrType)
	}
}

func TestParseSOCKS4Request_EmptyDomain(t *testing.T) {
	buf := &bytes.Buffer{}
	buf.WriteByte(SOCKS4Version)
	buf.WriteByte(socks4CmdConnect)
	binary.Write(buf, binary.BigEndian, uint16(80))
	buf.Write([]byte{0, 0, 0, 1})
	buf.WriteByte(0)
	buf.WriteByte(0)

	_, err := parseSOCKS4Bytes(buf.Bytes())
	if err == nil {
		t.Error("parseSOCKS4Request() should fail for empty SOCKS4a domain")
	}
}

func TestSendSOCKS4Reply_Granted(t *testing.T) {
	writer := &bytes.Buffer{}
	ip := net.ParseIP("1.2.3.4")
	err := sendSOCKS4Reply(newMockConn(nil, writer), socks4Granted, ip, 8080)
	if err != nil {
		t.Fatalf("sendSOCKS4Reply() error = %v", err)
	}

	b := writer.Bytes()
	if len(b) != 8 {
		t.Fatalf("reply length = %d, want 8", len(b))
	}
	if b[0] != 0x00 {
		t.Errorf("b[0] = %d, want 0x00", b[0])
	}
	if b[1] != socks4Granted {
		t.Errorf("b[1] = %d, want 0x5A", b[1])
	}
	port := binary.BigEndian.Uint16(b[2:4])
	if port != 8080 {
		t.Errorf("port = %d, want 8080", port)
	}
	if !bytes.Equal(b[4:8], ip.To4()) {
		t.Errorf("IP = %v, want %v", b[4:8], ip.To4())
	}
}

// startEchoServer starts a TCP echo server on a random port and returns the
// resolved address. The listener is closed via t.Cleanup.
func startEchoServer(t *testing.T) (net.IP, uint16) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("echo listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				io.Copy(c, c)
			}(c)
		}
	}()

	host, portStr, _ := net.SplitHostPort(ln.Addr().String())
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("parse echo port: %v", err)
	}
	return net.ParseIP(host), uint16(port)
}

func startSOCKSServer(t *testing.T, cfg *ServerConfig) *Server {
	t.Helper()
	c := DefaultServerConfig()
	if cfg != nil {
		c = *cfg
	}
	if c.Address == "" {
		c.Address = "127.0.0.1:0"
	}
	s := NewServer(c)
	if err := s.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() { s.Stop() })
	return s
}

func TestServer_SOCKS4_Connect(t *testing.T) {
	echoIP, echoPort := startEchoServer(t)
	s := startSOCKSServer(t, nil)

	conn, err := net.Dial("tcp", s.Address().String())
	if err != nil {
		t.Fatalf("Dial error: %v", err)
	}
	defer conn.Close()

	if _, err := conn.Write(buildSOCKS4Request(socks4CmdConnect, echoIP, echoPort, "", "")); err != nil {
		t.Fatalf("write request: %v", err)
	}

	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	reply := make([]byte, 8)
	if _, err := io.ReadFull(conn, reply); err != nil {
		t.Fatalf("read reply: %v", err)
	}
	if reply[1] != socks4Granted {
		t.Fatalf("reply = 0x%02X, want 0x%02X (Granted)", reply[1], socks4Granted)
	}

	msg := []byte("Hello, SOCKS4!")
	if _, err := conn.Write(msg); err != nil {
		t.Fatalf("write echo: %v", err)
	}
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	got := make([]byte, len(msg))
	if _, err := io.ReadFull(conn, got); err != nil {
		t.Fatalf("read echo: %v", err)
	}
	if !bytes.Equal(got, msg) {
		t.Errorf("echo = %q, want %q", got, msg)
	}
}

func TestServer_SOCKS4a_Connect(t *testing.T) {
	_, echoPort := startEchoServer(t)
	s := startSOCKSServer(t, nil)

	conn, err := net.Dial("tcp", s.Address().String())
	if err != nil {
		t.Fatalf("Dial error: %v", err)
	}
	defer conn.Close()

	if _, err := conn.Write(buildSOCKS4Request(socks4CmdConnect, nil, echoPort, "", "localhost")); err != nil {
		t.Fatalf("write request: %v", err)
	}

	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	reply := make([]byte, 8)
	if _, err := io.ReadFull(conn, reply); err != nil {
		t.Fatalf("read reply: %v", err)
	}
	if reply[1] != socks4Granted {
		t.Fatalf("reply = 0x%02X, want 0x%02X (Granted)", reply[1], socks4Granted)
	}

	msg := []byte("Hello, SOCKS4a!")
	if _, err := conn.Write(msg); err != nil {
		t.Fatalf("write echo: %v", err)
	}
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	got := make([]byte, len(msg))
	if _, err := io.ReadFull(conn, got); err != nil {
		t.Fatalf("read echo: %v", err)
	}
	if !bytes.Equal(got, msg) {
		t.Errorf("echo = %q, want %q", got, msg)
	}
}

func TestServer_SOCKS4_UnsupportedCommand(t *testing.T) {
	s := startSOCKSServer(t, nil)

	conn, err := net.Dial("tcp", s.Address().String())
	if err != nil {
		t.Fatalf("Dial error: %v", err)
	}
	defer conn.Close()

	// BIND (0x02) is not supported.
	if _, err := conn.Write(buildSOCKS4Request(0x02, net.ParseIP("127.0.0.1"), 80, "", "")); err != nil {
		t.Fatalf("write request: %v", err)
	}

	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	reply := make([]byte, 8)
	if _, err := io.ReadFull(conn, reply); err != nil {
		t.Fatalf("read reply: %v", err)
	}
	if reply[1] != socks4Rejected {
		t.Errorf("reply = 0x%02X, want 0x%02X (Rejected)", reply[1], socks4Rejected)
	}
}

func TestReadNullTerminatedBytes_TooLong(t *testing.T) {
	data := make([]byte, maxSOCKS4FieldLen+1)
	for i := range data {
		data[i] = 'A'
	}
	_, err := readNullTerminatedBytes(bufio.NewReader(bytes.NewReader(data)))
	if err == nil {
		t.Error("readNullTerminatedBytes() should fail when field exceeds max length")
	}
}

func TestServer_SOCKS4_RejectedWhenAuthRequired(t *testing.T) {
	cfg := DefaultServerConfig()
	cfg.Authenticators = []Authenticator{
		NewUserPassAuthenticator(StaticCredentials{"user": "pass"}),
	}
	s := startSOCKSServer(t, &cfg)

	conn, err := net.Dial("tcp", s.Address().String())
	if err != nil {
		t.Fatalf("Dial error: %v", err)
	}
	defer conn.Close()

	if _, err := conn.Write(buildSOCKS4Request(socks4CmdConnect, net.ParseIP("127.0.0.1"), 80, "", "")); err != nil {
		t.Fatalf("write request: %v", err)
	}

	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	reply := make([]byte, 8)
	if _, err := io.ReadFull(conn, reply); err != nil {
		t.Fatalf("read reply: %v", err)
	}
	if reply[1] != socks4Rejected {
		t.Errorf("reply = 0x%02X, want 0x%02X (Rejected)", reply[1], socks4Rejected)
	}
}

func TestServer_AutoDetect_SOCKS4andSOCKS5(t *testing.T) {
	echoIP, echoPort := startEchoServer(t)
	s := startSOCKSServer(t, nil)

	t.Run("SOCKS4", func(t *testing.T) {
		conn, err := net.Dial("tcp", s.Address().String())
		if err != nil {
			t.Fatalf("Dial: %v", err)
		}
		defer conn.Close()
		if _, err := conn.Write(buildSOCKS4Request(socks4CmdConnect, echoIP, echoPort, "", "")); err != nil {
			t.Fatalf("write: %v", err)
		}
		conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		reply := make([]byte, 8)
		if _, err := io.ReadFull(conn, reply); err != nil {
			t.Fatalf("read reply: %v", err)
		}
		if reply[1] != socks4Granted {
			t.Errorf("SOCKS4 reply = 0x%02X, want Granted", reply[1])
		}
	})

	t.Run("SOCKS5", func(t *testing.T) {
		conn, err := net.Dial("tcp", s.Address().String())
		if err != nil {
			t.Fatalf("Dial: %v", err)
		}
		defer conn.Close()
		if _, err := conn.Write([]byte{SOCKS5Version, 1, AuthMethodNoAuth}); err != nil {
			t.Fatalf("write greeting: %v", err)
		}
		conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		methodResp := make([]byte, 2)
		if _, err := io.ReadFull(conn, methodResp); err != nil {
			t.Fatalf("read method: %v", err)
		}
		if methodResp[1] != AuthMethodNoAuth {
			t.Errorf("SOCKS5 method = %d, want NoAuth", methodResp[1])
		}

		req := &bytes.Buffer{}
		req.WriteByte(SOCKS5Version)
		req.WriteByte(CmdConnect)
		req.WriteByte(0x00)
		req.WriteByte(AddrTypeIPv4)
		req.Write(echoIP.To4())
		binary.Write(req, binary.BigEndian, echoPort)
		if _, err := conn.Write(req.Bytes()); err != nil {
			t.Fatalf("write request: %v", err)
		}

		conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		reply := make([]byte, 10)
		if _, err := io.ReadFull(conn, reply); err != nil {
			t.Fatalf("read reply: %v", err)
		}
		if reply[1] != ReplySucceeded {
			t.Errorf("SOCKS5 reply = %d, want Succeeded", reply[1])
		}
	})
}
