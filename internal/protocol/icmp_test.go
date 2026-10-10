package protocol

import (
	"bytes"
	"net"
	"testing"

	"github.com/postalsys/muti-metroo/internal/identity"
)

// ============================================================================
// ICMPOpen Tests
// ============================================================================

func TestICMPOpen_EncodeDecode_IPv4(t *testing.T) {
	path1, _ := identity.NewAgentID()
	path2, _ := identity.NewAgentID()

	var ephKey [EphemeralKeySize]byte
	for i := range ephKey {
		ephKey[i] = byte(i)
	}

	original := &ICMPOpen{
		RequestID:       12345678,
		DestIP:          []byte{8, 8, 8, 8},
		TTL:             10,
		RemainingPath:   []identity.AgentID{path1, path2},
		EphemeralPubKey: ephKey,
	}

	data := original.Encode()

	decoded, err := DecodeICMPOpen(data)
	if err != nil {
		t.Fatalf("DecodeICMPOpen() error = %v", err)
	}

	if decoded.RequestID != original.RequestID {
		t.Errorf("RequestID = %d, want %d", decoded.RequestID, original.RequestID)
	}
	if !bytes.Equal(decoded.DestIP, original.DestIP) {
		t.Errorf("DestIP = %v, want %v", decoded.DestIP, original.DestIP)
	}
	if decoded.TTL != original.TTL {
		t.Errorf("TTL = %d, want %d", decoded.TTL, original.TTL)
	}
	if len(decoded.RemainingPath) != len(original.RemainingPath) {
		t.Errorf("RemainingPath length = %d, want %d", len(decoded.RemainingPath), len(original.RemainingPath))
	}
	for i := range decoded.RemainingPath {
		if decoded.RemainingPath[i] != original.RemainingPath[i] {
			t.Errorf("RemainingPath[%d] = %v, want %v", i, decoded.RemainingPath[i], original.RemainingPath[i])
		}
	}
	if decoded.EphemeralPubKey != original.EphemeralPubKey {
		t.Errorf("EphemeralPubKey mismatch")
	}
}

func TestICMPOpen_EncodeDecode_EmptyPath(t *testing.T) {
	var ephKey [EphemeralKeySize]byte

	original := &ICMPOpen{
		RequestID:       1,
		DestIP:          []byte{127, 0, 0, 1},
		TTL:             1,
		RemainingPath:   []identity.AgentID{}, // Exit node
		EphemeralPubKey: ephKey,
	}

	data := original.Encode()

	decoded, err := DecodeICMPOpen(data)
	if err != nil {
		t.Fatalf("DecodeICMPOpen() error = %v", err)
	}

	if len(decoded.RemainingPath) != 0 {
		t.Errorf("RemainingPath length = %d, want 0", len(decoded.RemainingPath))
	}
}

func TestICMPOpen_GetDestinationIP(t *testing.T) {
	original := &ICMPOpen{
		RequestID: 1,
		DestIP:    []byte{192, 168, 1, 1},
		TTL:       5,
	}

	ip := original.GetDestinationIP()
	if ip.String() != "192.168.1.1" {
		t.Errorf("GetDestinationIP() = %v, want 192.168.1.1", ip)
	}
}

func TestDecodeICMPOpen_TooShort(t *testing.T) {
	_, err := DecodeICMPOpen([]byte{1, 2, 3})
	if err == nil {
		t.Error("DecodeICMPOpen() should fail for short buffer")
	}
}

// ============================================================================
// ICMPOpenAck Tests
// ============================================================================

func TestICMPOpenAck_EncodeDecode(t *testing.T) {
	var ephKey [EphemeralKeySize]byte
	for i := range ephKey {
		ephKey[i] = byte(0xFF - i)
	}

	original := &ICMPOpenAck{
		RequestID:       12345678,
		EphemeralPubKey: ephKey,
	}

	data := original.Encode()

	decoded, err := DecodeICMPOpenAck(data)
	if err != nil {
		t.Fatalf("DecodeICMPOpenAck() error = %v", err)
	}

	if decoded.RequestID != original.RequestID {
		t.Errorf("RequestID = %d, want %d", decoded.RequestID, original.RequestID)
	}
	if decoded.EphemeralPubKey != original.EphemeralPubKey {
		t.Errorf("EphemeralPubKey mismatch")
	}
}

func TestDecodeICMPOpenAck_TooShort(t *testing.T) {
	_, err := DecodeICMPOpenAck([]byte{1, 2, 3})
	if err == nil {
		t.Error("DecodeICMPOpenAck() should fail for short buffer")
	}
}

// ============================================================================
// ICMPOpenErr Tests
// ============================================================================

func TestICMPOpenErr_EncodeDecode(t *testing.T) {
	original := &ICMPOpenErr{
		RequestID: 12345678,
		ErrorCode: ErrICMPDisabled,
		Message:   "ICMP echo is disabled",
	}

	data := original.Encode()

	decoded, err := DecodeICMPOpenErr(data)
	if err != nil {
		t.Fatalf("DecodeICMPOpenErr() error = %v", err)
	}

	if decoded.RequestID != original.RequestID {
		t.Errorf("RequestID = %d, want %d", decoded.RequestID, original.RequestID)
	}
	if decoded.ErrorCode != original.ErrorCode {
		t.Errorf("ErrorCode = %d, want %d", decoded.ErrorCode, original.ErrorCode)
	}
	if decoded.Message != original.Message {
		t.Errorf("Message = %q, want %q", decoded.Message, original.Message)
	}
}

func TestICMPOpenErr_EncodeDecode_EmptyMessage(t *testing.T) {
	original := &ICMPOpenErr{
		RequestID: 1,
		ErrorCode: ErrICMPDestNotAllowed,
		Message:   "",
	}

	data := original.Encode()

	decoded, err := DecodeICMPOpenErr(data)
	if err != nil {
		t.Fatalf("DecodeICMPOpenErr() error = %v", err)
	}

	if decoded.Message != "" {
		t.Errorf("Message = %q, want empty", decoded.Message)
	}
}

func TestICMPOpenErr_EncodeDecode_LongMessage(t *testing.T) {
	// Create message longer than 255 bytes
	longMsg := make([]byte, 300)
	for i := range longMsg {
		longMsg[i] = 'x'
	}

	original := &ICMPOpenErr{
		RequestID: 1,
		ErrorCode: ErrGeneralFailure,
		Message:   string(longMsg),
	}

	data := original.Encode()

	decoded, err := DecodeICMPOpenErr(data)
	if err != nil {
		t.Fatalf("DecodeICMPOpenErr() error = %v", err)
	}

	// Message should be truncated to 255 bytes
	if len(decoded.Message) != 255 {
		t.Errorf("Message length = %d, want 255", len(decoded.Message))
	}
}

func TestDecodeICMPOpenErr_TooShort(t *testing.T) {
	_, err := DecodeICMPOpenErr([]byte{1, 2, 3})
	if err == nil {
		t.Error("DecodeICMPOpenErr() should fail for short buffer")
	}
}

// ============================================================================
// ICMPEcho Tests
// ============================================================================

func TestICMPEcho_EncodeDecode_Request(t *testing.T) {
	original := &ICMPEcho{
		Identifier: 12345,
		Sequence:   1,
		IsReply:    false,
		Data:       []byte("ping payload data"),
	}

	data := original.Encode()

	decoded, err := DecodeICMPEcho(data)
	if err != nil {
		t.Fatalf("DecodeICMPEcho() error = %v", err)
	}

	if decoded.Identifier != original.Identifier {
		t.Errorf("Identifier = %d, want %d", decoded.Identifier, original.Identifier)
	}
	if decoded.Sequence != original.Sequence {
		t.Errorf("Sequence = %d, want %d", decoded.Sequence, original.Sequence)
	}
	if decoded.IsReply != original.IsReply {
		t.Errorf("IsReply = %v, want %v", decoded.IsReply, original.IsReply)
	}
	if !bytes.Equal(decoded.Data, original.Data) {
		t.Errorf("Data = %v, want %v", decoded.Data, original.Data)
	}
}

func TestICMPEcho_EncodeDecode_Reply(t *testing.T) {
	original := &ICMPEcho{
		Identifier: 12345,
		Sequence:   5,
		IsReply:    true,
		Data:       []byte("pong reply data"),
	}

	data := original.Encode()

	decoded, err := DecodeICMPEcho(data)
	if err != nil {
		t.Fatalf("DecodeICMPEcho() error = %v", err)
	}

	if !decoded.IsReply {
		t.Error("IsReply should be true for reply")
	}
	if decoded.Sequence != 5 {
		t.Errorf("Sequence = %d, want 5", decoded.Sequence)
	}
}

func TestICMPEcho_EncodeDecode_ReplyWithSrcIPv4(t *testing.T) {
	srcIP := net.IPv4(192, 168, 1, 1).To4()
	original := &ICMPEcho{
		Identifier: 12345,
		Sequence:   5,
		IsReply:    true,
		SrcIP:      srcIP,
		Data:       []byte("pong reply data"),
	}

	data := original.Encode()

	decoded, err := DecodeICMPEcho(data)
	if err != nil {
		t.Fatalf("DecodeICMPEcho() error = %v", err)
	}

	if !decoded.IsReply {
		t.Error("IsReply should be true for reply")
	}
	if !bytes.Equal(decoded.SrcIP, srcIP) {
		t.Errorf("SrcIP = %v, want %v", decoded.SrcIP, srcIP)
	}
	if !bytes.Equal(decoded.Data, original.Data) {
		t.Errorf("Data = %v, want %v", decoded.Data, original.Data)
	}
}

func TestICMPEcho_EncodeDecode_ReplyWithSrcIPv6(t *testing.T) {
	srcIP := net.ParseIP("2001:db8::1")
	original := &ICMPEcho{
		Identifier: 9999,
		Sequence:   3,
		IsReply:    true,
		SrcIP:      srcIP,
		Data:       []byte("ipv6 reply"),
	}

	data := original.Encode()

	decoded, err := DecodeICMPEcho(data)
	if err != nil {
		t.Fatalf("DecodeICMPEcho() error = %v", err)
	}

	if !bytes.Equal(decoded.SrcIP, srcIP) {
		t.Errorf("SrcIP = %v, want %v", decoded.SrcIP, srcIP)
	}
}

func TestICMPEcho_EncodeDecode_RequestNoSrcIP(t *testing.T) {
	original := &ICMPEcho{
		Identifier: 100,
		Sequence:   1,
		IsReply:    false,
		Data:       []byte("ping"),
	}

	data := original.Encode()

	decoded, err := DecodeICMPEcho(data)
	if err != nil {
		t.Fatalf("DecodeICMPEcho() error = %v", err)
	}

	if len(decoded.SrcIP) != 0 {
		t.Errorf("SrcIP should be empty for request, got %v", decoded.SrcIP)
	}
}

func TestICMPEcho_EncodeDecode_EmptyData(t *testing.T) {
	original := &ICMPEcho{
		Identifier: 1,
		Sequence:   1,
		IsReply:    false,
		Data:       []byte{},
	}

	data := original.Encode()

	decoded, err := DecodeICMPEcho(data)
	if err != nil {
		t.Fatalf("DecodeICMPEcho() error = %v", err)
	}

	if len(decoded.Data) != 0 {
		t.Errorf("Data length = %d, want 0", len(decoded.Data))
	}
}

func TestICMPEcho_EncodeDecode_MaxSize(t *testing.T) {
	// Test with maximum echo payload size
	payload := make([]byte, MaxICMPEchoDataSize)
	for i := range payload {
		payload[i] = byte(i % 256)
	}

	original := &ICMPEcho{
		Identifier: 65535,
		Sequence:   65535,
		IsReply:    true,
		Data:       payload,
	}

	data := original.Encode()

	decoded, err := DecodeICMPEcho(data)
	if err != nil {
		t.Fatalf("DecodeICMPEcho() error = %v", err)
	}

	if len(decoded.Data) != MaxICMPEchoDataSize {
		t.Errorf("Data length = %d, want %d", len(decoded.Data), MaxICMPEchoDataSize)
	}
	if !bytes.Equal(decoded.Data, original.Data) {
		t.Error("Data content mismatch")
	}
}

func TestDecodeICMPEcho_TooShort(t *testing.T) {
	_, err := DecodeICMPEcho([]byte{1, 2, 3})
	if err == nil {
		t.Error("DecodeICMPEcho() should fail for short buffer")
	}
}

// ============================================================================
// ICMPClose Tests
// ============================================================================

func TestICMPClose_EncodeDecode(t *testing.T) {
	tests := []struct {
		name   string
		reason uint8
	}{
		{"normal", ICMPCloseNormal},
		{"timeout", ICMPCloseTimeout},
		{"error", ICMPCloseError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			original := &ICMPClose{Reason: tt.reason}

			data := original.Encode()

			decoded, err := DecodeICMPClose(data)
			if err != nil {
				t.Fatalf("DecodeICMPClose() error = %v", err)
			}

			if decoded.Reason != original.Reason {
				t.Errorf("Reason = %d, want %d", decoded.Reason, original.Reason)
			}
		})
	}
}

func TestDecodeICMPClose_TooShort(t *testing.T) {
	_, err := DecodeICMPClose([]byte{})
	if err == nil {
		t.Error("DecodeICMPClose() should fail for empty buffer")
	}
}

// ============================================================================
// Helper Function Tests
// ============================================================================

func TestIsICMPFrame(t *testing.T) {
	tests := []struct {
		frameType uint8
		want      bool
	}{
		{FrameICMPOpen, true},
		{FrameICMPOpenAck, true},
		{FrameICMPOpenErr, true},
		{FrameICMPEcho, true},
		{FrameICMPClose, true},
		{FrameStreamOpen, false},
		{FrameStreamData, false},
		{FrameKeepalive, false},
		{FrameControlRequest, false},
		{FrameUDPOpen, false},
		{FrameUDPDatagram, false},
		{0x00, false},
		{0xFF, false},
	}

	for _, tt := range tests {
		got := IsICMPFrame(tt.frameType)
		if got != tt.want {
			t.Errorf("IsICMPFrame(0x%02x) = %v, want %v", tt.frameType, got, tt.want)
		}
	}
}

func TestFrameTypeName_ICMP(t *testing.T) {
	tests := []struct {
		frameType uint8
		want      string
	}{
		{FrameICMPOpen, "ICMP_OPEN"},
		{FrameICMPOpenAck, "ICMP_OPEN_ACK"},
		{FrameICMPOpenErr, "ICMP_OPEN_ERR"},
		{FrameICMPEcho, "ICMP_ECHO"},
		{FrameICMPClose, "ICMP_CLOSE"},
	}

	for _, tt := range tests {
		got := FrameTypeName(tt.frameType)
		if got != tt.want {
			t.Errorf("FrameTypeName(0x%02x) = %q, want %q", tt.frameType, got, tt.want)
		}
	}
}

func TestErrorCodeName_ICMP(t *testing.T) {
	tests := []struct {
		code uint16
		want string
	}{
		{ErrICMPDisabled, "ICMP_DISABLED"},
		{ErrICMPDestNotAllowed, "ICMP_DEST_NOT_ALLOWED"},
		{ErrICMPSessionLimit, "ICMP_SESSION_LIMIT"},
	}

	for _, tt := range tests {
		got := ErrorCodeName(tt.code)
		if got != tt.want {
			t.Errorf("ErrorCodeName(%d) = %q, want %q", tt.code, got, tt.want)
		}
	}
}

// ============================================================================
// Benchmark Tests
// ============================================================================

func BenchmarkICMPOpen_Encode(b *testing.B) {
	path1, _ := identity.NewAgentID()
	var ephKey [EphemeralKeySize]byte

	i := &ICMPOpen{
		RequestID:       12345678,
		DestIP:          []byte{8, 8, 8, 8},
		TTL:             10,
		RemainingPath:   []identity.AgentID{path1},
		EphemeralPubKey: ephKey,
	}

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		_ = i.Encode()
	}
}

func BenchmarkICMPOpen_Decode(b *testing.B) {
	path1, _ := identity.NewAgentID()
	var ephKey [EphemeralKeySize]byte

	i := &ICMPOpen{
		RequestID:       12345678,
		DestIP:          []byte{8, 8, 8, 8},
		TTL:             10,
		RemainingPath:   []identity.AgentID{path1},
		EphemeralPubKey: ephKey,
	}
	data := i.Encode()

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		_, _ = DecodeICMPOpen(data)
	}
}

func BenchmarkICMPEcho_Encode(b *testing.B) {
	i := &ICMPEcho{
		Identifier: 12345,
		Sequence:   1,
		IsReply:    false,
		Data:       make([]byte, 64), // Typical ping payload size
	}

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		_ = i.Encode()
	}
}

func BenchmarkICMPEcho_Decode(b *testing.B) {
	i := &ICMPEcho{
		Identifier: 12345,
		Sequence:   1,
		IsReply:    false,
		Data:       make([]byte, 64),
	}
	data := i.Encode()

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		_, _ = DecodeICMPEcho(data)
	}
}
