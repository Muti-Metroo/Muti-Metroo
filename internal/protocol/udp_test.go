package protocol

import (
	"bytes"
	"testing"

	"github.com/postalsys/muti-metroo/internal/identity"
)

// ============================================================================
// UDPOpen Tests
// ============================================================================

func TestUDPOpen_EncodeDecode_IPv4(t *testing.T) {
	path1, _ := identity.NewAgentID()
	path2, _ := identity.NewAgentID()

	var ephKey [EphemeralKeySize]byte
	for i := range ephKey {
		ephKey[i] = byte(i)
	}

	original := &UDPOpen{
		RequestID:       12345678,
		AddressType:     AddrTypeIPv4,
		Address:         []byte{0, 0, 0, 0}, // 0.0.0.0
		Port:            0,
		TTL:             10,
		RemainingPath:   []identity.AgentID{path1, path2},
		EphemeralPubKey: ephKey,
	}

	data := original.Encode()

	decoded, err := DecodeUDPOpen(data)
	if err != nil {
		t.Fatalf("DecodeUDPOpen() error = %v", err)
	}

	if decoded.RequestID != original.RequestID {
		t.Errorf("RequestID = %d, want %d", decoded.RequestID, original.RequestID)
	}
	if decoded.AddressType != original.AddressType {
		t.Errorf("AddressType = %d, want %d", decoded.AddressType, original.AddressType)
	}
	if !bytes.Equal(decoded.Address, original.Address) {
		t.Errorf("Address = %v, want %v", decoded.Address, original.Address)
	}
	if decoded.Port != original.Port {
		t.Errorf("Port = %d, want %d", decoded.Port, original.Port)
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

func TestUDPOpen_EncodeDecode_IPv6(t *testing.T) {
	var ephKey [EphemeralKeySize]byte

	original := &UDPOpen{
		RequestID:       99999,
		AddressType:     AddrTypeIPv6,
		Address:         make([]byte, 16), // ::
		Port:            5353,
		TTL:             5,
		RemainingPath:   []identity.AgentID{},
		EphemeralPubKey: ephKey,
	}

	data := original.Encode()

	decoded, err := DecodeUDPOpen(data)
	if err != nil {
		t.Fatalf("DecodeUDPOpen() error = %v", err)
	}

	if decoded.AddressType != AddrTypeIPv6 {
		t.Errorf("AddressType = %d, want %d", decoded.AddressType, AddrTypeIPv6)
	}
	if len(decoded.Address) != 16 {
		t.Errorf("Address length = %d, want 16", len(decoded.Address))
	}
}

func TestUDPOpen_EncodeDecode_EmptyPath(t *testing.T) {
	var ephKey [EphemeralKeySize]byte

	original := &UDPOpen{
		RequestID:       1,
		AddressType:     AddrTypeIPv4,
		Address:         []byte{127, 0, 0, 1},
		Port:            53,
		TTL:             1,
		RemainingPath:   []identity.AgentID{}, // Exit node
		EphemeralPubKey: ephKey,
	}

	data := original.Encode()

	decoded, err := DecodeUDPOpen(data)
	if err != nil {
		t.Fatalf("DecodeUDPOpen() error = %v", err)
	}

	if len(decoded.RemainingPath) != 0 {
		t.Errorf("RemainingPath length = %d, want 0", len(decoded.RemainingPath))
	}
}

func TestDecodeUDPOpen_TooShort(t *testing.T) {
	_, err := DecodeUDPOpen([]byte{1, 2, 3})
	if err == nil {
		t.Error("DecodeUDPOpen() should fail for short buffer")
	}
}

// ============================================================================
// UDPOpenAck Tests
// ============================================================================

func TestUDPOpenAck_EncodeDecode_IPv4(t *testing.T) {
	var ephKey [EphemeralKeySize]byte
	for i := range ephKey {
		ephKey[i] = byte(0xFF - i)
	}

	original := &UDPOpenAck{
		RequestID:       12345678,
		BoundAddrType:   AddrTypeIPv4,
		BoundAddr:       []byte{192, 168, 1, 100},
		BoundPort:       45678,
		EphemeralPubKey: ephKey,
	}

	data := original.Encode()

	decoded, err := DecodeUDPOpenAck(data)
	if err != nil {
		t.Fatalf("DecodeUDPOpenAck() error = %v", err)
	}

	if decoded.RequestID != original.RequestID {
		t.Errorf("RequestID = %d, want %d", decoded.RequestID, original.RequestID)
	}
	if decoded.BoundAddrType != original.BoundAddrType {
		t.Errorf("BoundAddrType = %d, want %d", decoded.BoundAddrType, original.BoundAddrType)
	}
	if !bytes.Equal(decoded.BoundAddr, original.BoundAddr) {
		t.Errorf("BoundAddr = %v, want %v", decoded.BoundAddr, original.BoundAddr)
	}
	if decoded.BoundPort != original.BoundPort {
		t.Errorf("BoundPort = %d, want %d", decoded.BoundPort, original.BoundPort)
	}
	if decoded.EphemeralPubKey != original.EphemeralPubKey {
		t.Errorf("EphemeralPubKey mismatch")
	}
}

func TestUDPOpenAck_EncodeDecode_IPv6(t *testing.T) {
	var ephKey [EphemeralKeySize]byte

	original := &UDPOpenAck{
		RequestID:       1,
		BoundAddrType:   AddrTypeIPv6,
		BoundAddr:       make([]byte, 16),
		BoundPort:       8080,
		EphemeralPubKey: ephKey,
	}

	data := original.Encode()

	decoded, err := DecodeUDPOpenAck(data)
	if err != nil {
		t.Fatalf("DecodeUDPOpenAck() error = %v", err)
	}

	if decoded.BoundAddrType != AddrTypeIPv6 {
		t.Errorf("BoundAddrType = %d, want %d", decoded.BoundAddrType, AddrTypeIPv6)
	}
	if len(decoded.BoundAddr) != 16 {
		t.Errorf("BoundAddr length = %d, want 16", len(decoded.BoundAddr))
	}
}

func TestDecodeUDPOpenAck_TooShort(t *testing.T) {
	_, err := DecodeUDPOpenAck([]byte{1, 2, 3})
	if err == nil {
		t.Error("DecodeUDPOpenAck() should fail for short buffer")
	}
}

// ============================================================================
// UDPOpenErr Tests
// ============================================================================

func TestUDPOpenErr_EncodeDecode(t *testing.T) {
	original := &UDPOpenErr{
		RequestID: 12345678,
		ErrorCode: ErrUDPDisabled,
		Message:   "UDP relay is disabled",
	}

	data := original.Encode()

	decoded, err := DecodeUDPOpenErr(data)
	if err != nil {
		t.Fatalf("DecodeUDPOpenErr() error = %v", err)
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

func TestUDPOpenErr_EncodeDecode_EmptyMessage(t *testing.T) {
	original := &UDPOpenErr{
		RequestID: 1,
		ErrorCode: ErrUDPPortNotAllowed,
		Message:   "",
	}

	data := original.Encode()

	decoded, err := DecodeUDPOpenErr(data)
	if err != nil {
		t.Fatalf("DecodeUDPOpenErr() error = %v", err)
	}

	if decoded.Message != "" {
		t.Errorf("Message = %q, want empty", decoded.Message)
	}
}

func TestUDPOpenErr_EncodeDecode_LongMessage(t *testing.T) {
	// Create message longer than 255 bytes
	longMsg := make([]byte, 300)
	for i := range longMsg {
		longMsg[i] = 'x'
	}

	original := &UDPOpenErr{
		RequestID: 1,
		ErrorCode: ErrGeneralFailure,
		Message:   string(longMsg),
	}

	data := original.Encode()

	decoded, err := DecodeUDPOpenErr(data)
	if err != nil {
		t.Fatalf("DecodeUDPOpenErr() error = %v", err)
	}

	// Message should be truncated to 255 bytes
	if len(decoded.Message) != 255 {
		t.Errorf("Message length = %d, want 255", len(decoded.Message))
	}
}

func TestDecodeUDPOpenErr_TooShort(t *testing.T) {
	_, err := DecodeUDPOpenErr([]byte{1, 2, 3})
	if err == nil {
		t.Error("DecodeUDPOpenErr() should fail for short buffer")
	}
}

// ============================================================================
// UDPDatagram Tests
// ============================================================================

func TestUDPDatagram_EncodeDecode_IPv4(t *testing.T) {
	original := &UDPDatagram{
		AddressType: AddrTypeIPv4,
		Address:     []byte{8, 8, 8, 8},
		Port:        53,
		Data:        []byte("DNS query payload"),
	}

	data := original.Encode()

	decoded, err := DecodeUDPDatagram(data)
	if err != nil {
		t.Fatalf("DecodeUDPDatagram() error = %v", err)
	}

	if decoded.AddressType != original.AddressType {
		t.Errorf("AddressType = %d, want %d", decoded.AddressType, original.AddressType)
	}
	if !bytes.Equal(decoded.Address, original.Address) {
		t.Errorf("Address = %v, want %v", decoded.Address, original.Address)
	}
	if decoded.Port != original.Port {
		t.Errorf("Port = %d, want %d", decoded.Port, original.Port)
	}
	if !bytes.Equal(decoded.Data, original.Data) {
		t.Errorf("Data = %v, want %v", decoded.Data, original.Data)
	}
}

func TestUDPDatagram_EncodeDecode_IPv6(t *testing.T) {
	addr := make([]byte, 16)
	addr[15] = 1 // ::1

	original := &UDPDatagram{
		AddressType: AddrTypeIPv6,
		Address:     addr,
		Port:        123,
		Data:        []byte("NTP request"),
	}

	data := original.Encode()

	decoded, err := DecodeUDPDatagram(data)
	if err != nil {
		t.Fatalf("DecodeUDPDatagram() error = %v", err)
	}

	if decoded.AddressType != AddrTypeIPv6 {
		t.Errorf("AddressType = %d, want %d", decoded.AddressType, AddrTypeIPv6)
	}
	if len(decoded.Address) != 16 {
		t.Errorf("Address length = %d, want 16", len(decoded.Address))
	}
}

func TestUDPDatagram_EncodeDecode_EmptyData(t *testing.T) {
	original := &UDPDatagram{
		AddressType: AddrTypeIPv4,
		Address:     []byte{1, 2, 3, 4},
		Port:        12345,
		Data:        []byte{},
	}

	data := original.Encode()

	decoded, err := DecodeUDPDatagram(data)
	if err != nil {
		t.Fatalf("DecodeUDPDatagram() error = %v", err)
	}

	if len(decoded.Data) != 0 {
		t.Errorf("Data length = %d, want 0", len(decoded.Data))
	}
}

func TestUDPDatagram_EncodeDecode_MaxSize(t *testing.T) {
	// Test with maximum datagram size
	payload := make([]byte, MaxUDPDatagramSize)
	for i := range payload {
		payload[i] = byte(i % 256)
	}

	original := &UDPDatagram{
		AddressType: AddrTypeIPv4,
		Address:     []byte{10, 0, 0, 1},
		Port:        8080,
		Data:        payload,
	}

	data := original.Encode()

	decoded, err := DecodeUDPDatagram(data)
	if err != nil {
		t.Fatalf("DecodeUDPDatagram() error = %v", err)
	}

	if len(decoded.Data) != MaxUDPDatagramSize {
		t.Errorf("Data length = %d, want %d", len(decoded.Data), MaxUDPDatagramSize)
	}
	if !bytes.Equal(decoded.Data, original.Data) {
		t.Error("Data content mismatch")
	}
}

func TestDecodeUDPDatagram_TooShort(t *testing.T) {
	_, err := DecodeUDPDatagram([]byte{1, 2, 3})
	if err == nil {
		t.Error("DecodeUDPDatagram() should fail for short buffer")
	}
}

func TestDecodeUDPDatagram_InvalidAddressType(t *testing.T) {
	data := []byte{0xFF, 1, 2, 3, 4, 0, 0, 0, 0} // Invalid address type 0xFF
	_, err := DecodeUDPDatagram(data)
	if err == nil {
		t.Error("DecodeUDPDatagram() should fail for invalid address type")
	}
}

// ============================================================================
// UDPClose Tests
// ============================================================================

func TestUDPClose_EncodeDecode(t *testing.T) {
	tests := []struct {
		name   string
		reason uint8
	}{
		{"normal", UDPCloseNormal},
		{"timeout", UDPCloseTimeout},
		{"error", UDPCloseError},
		{"tcp_closed", UDPCloseTCPClosed},
		{"admin_close", UDPCloseAdminClose},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			original := &UDPClose{Reason: tt.reason}

			data := original.Encode()

			decoded, err := DecodeUDPClose(data)
			if err != nil {
				t.Fatalf("DecodeUDPClose() error = %v", err)
			}

			if decoded.Reason != original.Reason {
				t.Errorf("Reason = %d, want %d", decoded.Reason, original.Reason)
			}
		})
	}
}

func TestDecodeUDPClose_TooShort(t *testing.T) {
	_, err := DecodeUDPClose([]byte{})
	if err == nil {
		t.Error("DecodeUDPClose() should fail for empty buffer")
	}
}

// ============================================================================
// Helper Function Tests
// ============================================================================

func TestIsUDPFrame(t *testing.T) {
	tests := []struct {
		frameType uint8
		want      bool
	}{
		{FrameUDPOpen, true},
		{FrameUDPOpenAck, true},
		{FrameUDPOpenErr, true},
		{FrameUDPDatagram, true},
		{FrameUDPClose, true},
		{FrameStreamOpen, false},
		{FrameStreamData, false},
		{FrameKeepalive, false},
		{FrameControlRequest, false},
		{0x00, false},
		{0xFF, false},
	}

	for _, tt := range tests {
		got := IsUDPFrame(tt.frameType)
		if got != tt.want {
			t.Errorf("IsUDPFrame(0x%02x) = %v, want %v", tt.frameType, got, tt.want)
		}
	}
}

func TestFrameTypeName_UDP(t *testing.T) {
	tests := []struct {
		frameType uint8
		want      string
	}{
		{FrameUDPOpen, "UDP_OPEN"},
		{FrameUDPOpenAck, "UDP_OPEN_ACK"},
		{FrameUDPOpenErr, "UDP_OPEN_ERR"},
		{FrameUDPDatagram, "UDP_DATAGRAM"},
		{FrameUDPClose, "UDP_CLOSE"},
	}

	for _, tt := range tests {
		got := FrameTypeName(tt.frameType)
		if got != tt.want {
			t.Errorf("FrameTypeName(0x%02x) = %q, want %q", tt.frameType, got, tt.want)
		}
	}
}

func TestErrorCodeName_UDP(t *testing.T) {
	tests := []struct {
		code uint16
		want string
	}{
		{ErrUDPDisabled, "UDP_DISABLED"},
		{ErrUDPPortNotAllowed, "UDP_PORT_NOT_ALLOWED"},
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

func BenchmarkUDPOpen_Encode(b *testing.B) {
	path1, _ := identity.NewAgentID()
	var ephKey [EphemeralKeySize]byte

	u := &UDPOpen{
		RequestID:       12345678,
		AddressType:     AddrTypeIPv4,
		Address:         []byte{0, 0, 0, 0},
		Port:            0,
		TTL:             10,
		RemainingPath:   []identity.AgentID{path1},
		EphemeralPubKey: ephKey,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = u.Encode()
	}
}

func BenchmarkUDPOpen_Decode(b *testing.B) {
	path1, _ := identity.NewAgentID()
	var ephKey [EphemeralKeySize]byte

	u := &UDPOpen{
		RequestID:       12345678,
		AddressType:     AddrTypeIPv4,
		Address:         []byte{0, 0, 0, 0},
		Port:            0,
		TTL:             10,
		RemainingPath:   []identity.AgentID{path1},
		EphemeralPubKey: ephKey,
	}
	data := u.Encode()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = DecodeUDPOpen(data)
	}
}

func BenchmarkUDPDatagram_Encode(b *testing.B) {
	u := &UDPDatagram{
		AddressType: AddrTypeIPv4,
		Address:     []byte{8, 8, 8, 8},
		Port:        53,
		Data:        make([]byte, 512), // Typical DNS query size
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = u.Encode()
	}
}

func BenchmarkUDPDatagram_Decode(b *testing.B) {
	u := &UDPDatagram{
		AddressType: AddrTypeIPv4,
		Address:     []byte{8, 8, 8, 8},
		Port:        53,
		Data:        make([]byte, 512),
	}
	data := u.Encode()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = DecodeUDPDatagram(data)
	}
}
