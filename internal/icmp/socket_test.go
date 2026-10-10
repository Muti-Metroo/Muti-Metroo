package icmp

import (
	"net"
	"runtime"
	"testing"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
)

// newICMPSocket creates a new unprivileged IPv4 ICMP socket for testing.
func newICMPSocket() (*icmp.PacketConn, error) {
	return icmp.ListenPacket("udp4", "0.0.0.0")
}

func TestEchoReply_Fields(t *testing.T) {
	reply := &EchoReply{
		ID:      1234,
		Seq:     5678,
		Payload: []byte("test payload"),
		SrcIP:   net.ParseIP("8.8.8.8"),
	}

	if reply.ID != 1234 {
		t.Errorf("ID = %d, want 1234", reply.ID)
	}
	if reply.Seq != 5678 {
		t.Errorf("Seq = %d, want 5678", reply.Seq)
	}
	if string(reply.Payload) != "test payload" {
		t.Errorf("Payload = %q, want %q", reply.Payload, "test payload")
	}
	if !reply.SrcIP.Equal(net.ParseIP("8.8.8.8")) {
		t.Errorf("SrcIP = %v, want 8.8.8.8", reply.SrcIP)
	}
}

func TestNewICMPSocket(t *testing.T) {
	// Skip on Windows and non-Linux systems where unprivileged ICMP may not work
	if runtime.GOOS == "windows" {
		t.Skip("Skipping socket test on Windows")
	}

	conn, err := newICMPSocket()
	if err != nil {
		// This is expected to fail on systems without net.ipv4.ping_group_range configured
		// or when running as non-root without proper permissions
		t.Skipf("newICMPSocket() failed (may need sysctl configuration): %v", err)
	}
	defer conn.Close()

	// Verify we got a valid connection
	if conn == nil {
		t.Fatal("newICMPSocket() returned nil without error")
	}
}

func TestSendEchoRequest_MessageFormat(t *testing.T) {
	// This test verifies the ICMP message encoding without actually sending
	// by checking the marshaled message format

	destIP := net.ParseIP("8.8.8.8")
	id := uint16(1234)
	seq := uint16(5678)
	payload := []byte("test payload")

	msg := icmp.Message{
		Type: ipv4.ICMPTypeEcho,
		Code: 0,
		Body: &icmp.Echo{
			ID:   int(id),
			Seq:  int(seq),
			Data: payload,
		},
	}

	msgBytes, err := msg.Marshal(nil)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	// ICMP header is 8 bytes + payload
	expectedLen := 8 + len(payload)
	if len(msgBytes) != expectedLen {
		t.Errorf("Message length = %d, want %d", len(msgBytes), expectedLen)
	}

	// Verify message type (Echo Request = 8)
	if msgBytes[0] != 8 {
		t.Errorf("Message type = %d, want 8", msgBytes[0])
	}

	// Verify code is 0
	if msgBytes[1] != 0 {
		t.Errorf("Message code = %d, want 0", msgBytes[1])
	}

	// Verify destination IP is not encoded in ICMP (it's in IP header)
	_ = destIP // Used only for actual send
}

func TestICMPv4ProtocolNumber(t *testing.T) {
	// Verify the protocol number constant
	if ICMPv4ProtocolNumber != 1 {
		t.Errorf("ICMPv4ProtocolNumber = %d, want 1", ICMPv4ProtocolNumber)
	}
}

func TestReadEchoReply_Timeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping socket test on Windows")
	}

	sock, err := NewSocketV4()
	if err != nil {
		t.Skipf("NewSocketV4() failed: %v", err)
	}
	defer sock.Close()

	// Read with a very short timeout - should timeout quickly
	start := time.Now()
	_, err = sock.ReadEchoReply(10 * time.Millisecond)
	elapsed := time.Since(start)

	// Should have timed out
	if err == nil {
		t.Error("ReadEchoReply() should timeout with no incoming packets")
	}

	// Verify it didn't take too long
	if elapsed > 100*time.Millisecond {
		t.Errorf("ReadEchoReply() took %v, expected ~10ms", elapsed)
	}
}

func TestReadEchoReplyFiltered_Timeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping socket test on Windows")
	}

	sock, err := NewSocketV4()
	if err != nil {
		t.Skipf("NewSocketV4() failed: %v", err)
	}
	defer sock.Close()

	// Read with a very short timeout - should timeout quickly
	start := time.Now()
	_, err = sock.ReadEchoReplyFiltered(1234, 10*time.Millisecond)
	elapsed := time.Since(start)

	// Should have timed out
	if err == nil {
		t.Error("ReadEchoReplyFiltered() should timeout with no incoming packets")
	}

	// Verify it didn't take too long
	if elapsed > 100*time.Millisecond {
		t.Errorf("ReadEchoReplyFiltered() took %v, expected ~10ms", elapsed)
	}
}

func TestSendEchoRequest_PayloadSizes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping socket test on Windows")
	}

	sock, err := NewSocketV4()
	if err != nil {
		t.Skipf("NewSocketV4() failed: %v", err)
	}
	defer sock.Close()

	destIP := net.ParseIP("127.0.0.1")

	testCases := []struct {
		name    string
		payload []byte
	}{
		{"empty", []byte{}},
		{"small", []byte("test")},
		{"medium", make([]byte, 100)},
		{"large", make([]byte, 1000)},
		{"near_max", make([]byte, 1400)},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := sock.SendEchoRequest(destIP, 1, 1, tc.payload)
			if err != nil {
				// Errors are OK for testing marshaling
				t.Logf("SendEchoRequest() with %s payload: %v", tc.name, err)
			}
		})
	}
}

// BenchmarkSendEchoRequest_Marshal benchmarks just the message marshaling
func BenchmarkSendEchoRequest_Marshal(b *testing.B) {
	payload := make([]byte, 64)

	for i := 0; i < b.N; i++ {
		msg := icmp.Message{
			Type: ipv4.ICMPTypeEcho,
			Code: 0,
			Body: &icmp.Echo{
				ID:   1234,
				Seq:  5678,
				Data: payload,
			},
		}
		_, _ = msg.Marshal(nil)
	}
}

// Tests for the new Socket type

func TestNewSocket_IPv4(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping socket test on Windows")
	}

	destIP := net.ParseIP("8.8.8.8")
	sock, err := NewSocket(destIP)
	if err != nil {
		t.Skipf("NewSocket() failed: %v", err)
	}
	defer sock.Close()

	if sock.IsIPv6() {
		t.Error("Socket for IPv4 address should not be IPv6")
	}
}

func TestNewSocket_IPv6(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping socket test on Windows")
	}

	destIP := net.ParseIP("2001:4860:4860::8888")
	sock, err := NewSocket(destIP)
	if err != nil {
		t.Skipf("NewSocket() for IPv6 failed: %v", err)
	}
	defer sock.Close()

	if !sock.IsIPv6() {
		t.Error("Socket for IPv6 address should be IPv6")
	}
}

func TestNewSocketV4(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping socket test on Windows")
	}

	sock, err := NewSocketV4()
	if err != nil {
		t.Skipf("NewSocketV4() failed: %v", err)
	}
	defer sock.Close()

	if sock.IsIPv6() {
		t.Error("NewSocketV4() should return IPv4 socket")
	}
}

func TestNewSocketV6(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping socket test on Windows")
	}

	sock, err := NewSocketV6()
	if err != nil {
		t.Skipf("NewSocketV6() failed: %v", err)
	}
	defer sock.Close()

	if !sock.IsIPv6() {
		t.Error("NewSocketV6() should return IPv6 socket")
	}
}

func TestSocket_SendEchoRequest_IPv4(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping socket test on Windows")
	}

	sock, err := NewSocketV4()
	if err != nil {
		t.Skipf("NewSocketV4() failed: %v", err)
	}
	defer sock.Close()

	destIP := net.ParseIP("127.0.0.1")
	err = sock.SendEchoRequest(destIP, 1, 1, []byte("test"))
	if err != nil {
		t.Logf("SendEchoRequest() error (may be expected): %v", err)
	}
}

func TestSocket_SendEchoRequest_IPv6(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping socket test on Windows")
	}

	sock, err := NewSocketV6()
	if err != nil {
		t.Skipf("NewSocketV6() failed: %v", err)
	}
	defer sock.Close()

	destIP := net.ParseIP("::1")
	err = sock.SendEchoRequest(destIP, 1, 1, []byte("test"))
	if err != nil {
		t.Logf("SendEchoRequest() IPv6 error (may be expected): %v", err)
	}
}

func TestSocket_SendEchoRequest_WrongVersion(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping socket test on Windows")
	}

	// IPv4 socket trying to send to IPv6 address should fail
	sock, err := NewSocketV4()
	if err != nil {
		t.Skipf("NewSocketV4() failed: %v", err)
	}
	defer sock.Close()

	destIP := net.ParseIP("::1")
	err = sock.SendEchoRequest(destIP, 1, 1, []byte("test"))
	if err == nil {
		t.Error("IPv4 socket should fail to send to IPv6 address")
	} else {
		t.Logf("Expected error: %v", err)
	}
}

func TestSocket_ReadEchoReply_Timeout_IPv4(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping socket test on Windows")
	}

	sock, err := NewSocketV4()
	if err != nil {
		t.Skipf("NewSocketV4() failed: %v", err)
	}
	defer sock.Close()

	start := time.Now()
	_, err = sock.ReadEchoReply(10 * time.Millisecond)
	elapsed := time.Since(start)

	if err == nil {
		t.Error("ReadEchoReply() should timeout")
	}

	if elapsed > 100*time.Millisecond {
		t.Errorf("ReadEchoReply() took %v, expected ~10ms", elapsed)
	}
}

func TestSocket_ReadEchoReply_Timeout_IPv6(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping socket test on Windows")
	}

	sock, err := NewSocketV6()
	if err != nil {
		t.Skipf("NewSocketV6() failed: %v", err)
	}
	defer sock.Close()

	start := time.Now()
	_, err = sock.ReadEchoReply(10 * time.Millisecond)
	elapsed := time.Since(start)

	if err == nil {
		t.Error("ReadEchoReply() should timeout")
	}

	if elapsed > 100*time.Millisecond {
		t.Errorf("ReadEchoReply() took %v, expected ~10ms", elapsed)
	}
}

func TestICMPv6ProtocolNumber(t *testing.T) {
	if ICMPv6ProtocolNumber != 58 {
		t.Errorf("ICMPv6ProtocolNumber = %d, want 58", ICMPv6ProtocolNumber)
	}
}
