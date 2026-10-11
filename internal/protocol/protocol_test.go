package protocol

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Muti-Metroo/Muti-Metroo/internal/identity"
)

func TestFrameTypeName(t *testing.T) {
	tests := []struct {
		frameType uint8
		want      string
	}{
		{FrameStreamOpen, "STREAM_OPEN"},
		{FrameStreamOpenAck, "STREAM_OPEN_ACK"},
		{FrameStreamOpenErr, "STREAM_OPEN_ERR"},
		{FrameStreamData, "STREAM_DATA"},
		{FrameStreamClose, "STREAM_CLOSE"},
		{FrameStreamReset, "STREAM_RESET"},
		{FrameRouteAdvertise, "ROUTE_ADVERTISE"},
		{FrameRouteWithdraw, "ROUTE_WITHDRAW"},
		{FramePeerHello, "PEER_HELLO"},
		{FramePeerHelloAck, "PEER_HELLO_ACK"},
		{FrameKeepalive, "KEEPALIVE"},
		{FrameKeepaliveAck, "KEEPALIVE_ACK"},
		{0xFF, "UNKNOWN"},
	}

	for _, tt := range tests {
		if got := FrameTypeName(tt.frameType); got != tt.want {
			t.Errorf("FrameTypeName(%d) = %s, want %s", tt.frameType, got, tt.want)
		}
	}
}

func TestErrorCodeName(t *testing.T) {
	tests := []struct {
		code uint16
		want string
	}{
		{ErrNoRoute, "NO_ROUTE"},
		{ErrConnectionRefused, "CONNECTION_REFUSED"},
		{ErrConnectionTimeout, "CONNECTION_TIMEOUT"},
		{ErrTTLExceeded, "TTL_EXCEEDED"},
		{ErrHostUnreachable, "HOST_UNREACHABLE"},
		{ErrNetworkUnreachable, "NETWORK_UNREACHABLE"},
		{ErrDNSError, "DNS_ERROR"},
		{ErrExitDisabled, "EXIT_DISABLED"},
		{ErrResourceLimit, "RESOURCE_LIMIT"},
		{999, "UNKNOWN"},
	}

	for _, tt := range tests {
		if got := ErrorCodeName(tt.code); got != tt.want {
			t.Errorf("ErrorCodeName(%d) = %s, want %s", tt.code, got, tt.want)
		}
	}
}

func TestIsStreamFrame(t *testing.T) {
	streamFrames := []uint8{FrameStreamOpen, FrameStreamOpenAck, FrameStreamOpenErr, FrameStreamData, FrameStreamClose, FrameStreamReset}
	nonStreamFrames := []uint8{FrameRouteAdvertise, FrameRouteWithdraw, FramePeerHello, FrameKeepalive}

	for _, ft := range streamFrames {
		if !IsStreamFrame(ft) {
			t.Errorf("IsStreamFrame(%s) = false, want true", FrameTypeName(ft))
		}
	}
	for _, ft := range nonStreamFrames {
		if IsStreamFrame(ft) {
			t.Errorf("IsStreamFrame(%s) = true, want false", FrameTypeName(ft))
		}
	}
}

func TestIsRoutingFrame(t *testing.T) {
	if !IsRoutingFrame(FrameRouteAdvertise) {
		t.Error("IsRoutingFrame(ROUTE_ADVERTISE) = false")
	}
	if !IsRoutingFrame(FrameRouteWithdraw) {
		t.Error("IsRoutingFrame(ROUTE_WITHDRAW) = false")
	}
	if IsRoutingFrame(FrameStreamOpen) {
		t.Error("IsRoutingFrame(STREAM_OPEN) = true")
	}
}

func TestIsControlFrame(t *testing.T) {
	controlFrames := []uint8{FramePeerHello, FramePeerHelloAck, FrameKeepalive, FrameKeepaliveAck}
	for _, ft := range controlFrames {
		if !IsControlFrame(ft) {
			t.Errorf("IsControlFrame(%s) = false, want true", FrameTypeName(ft))
		}
	}
	if IsControlFrame(FrameStreamOpen) {
		t.Error("IsControlFrame(STREAM_OPEN) = true")
	}
}

func TestFrame_EncodeDecode(t *testing.T) {
	tests := []struct {
		name  string
		frame Frame
	}{
		{
			name: "empty payload",
			frame: Frame{
				Type:     FrameStreamOpen,
				Flags:    0,
				StreamID: 42,
				Payload:  []byte{},
			},
		},
		{
			name: "with payload",
			frame: Frame{
				Type:     FrameStreamData,
				Flags:    FlagFinWrite,
				StreamID: 12345678,
				Payload:  []byte("Hello, World!"),
			},
		},
		{
			name: "max stream ID",
			frame: Frame{
				Type:     FrameKeepalive,
				Flags:    0,
				StreamID: ^uint64(0),
				Payload:  []byte{0x01, 0x02, 0x03},
			},
		},
		{
			name: "control stream",
			frame: Frame{
				Type:     FramePeerHello,
				Flags:    0,
				StreamID: ControlStreamID,
				Payload:  make([]byte, 100),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Encode
			data, err := tt.frame.Encode()
			if err != nil {
				t.Fatalf("Encode() error = %v", err)
			}

			// Verify header size
			if len(data) != HeaderSize+len(tt.frame.Payload) {
				t.Errorf("Encoded length = %d, want %d", len(data), HeaderSize+len(tt.frame.Payload))
			}

			// Decode
			decoded, err := Decode(data)
			if err != nil {
				t.Fatalf("Decode() error = %v", err)
			}

			// Verify
			if decoded.Type != tt.frame.Type {
				t.Errorf("Type = %d, want %d", decoded.Type, tt.frame.Type)
			}
			if decoded.Flags != tt.frame.Flags {
				t.Errorf("Flags = %d, want %d", decoded.Flags, tt.frame.Flags)
			}
			if decoded.StreamID != tt.frame.StreamID {
				t.Errorf("StreamID = %d, want %d", decoded.StreamID, tt.frame.StreamID)
			}
			if !bytes.Equal(decoded.Payload, tt.frame.Payload) {
				t.Errorf("Payload mismatch")
			}
		})
	}
}

func TestFrame_Encode_TooLarge(t *testing.T) {
	f := Frame{
		Type:    FrameStreamData,
		Payload: make([]byte, MaxPayloadSize+1),
	}

	_, err := f.Encode()
	if err != ErrFrameTooLarge {
		t.Errorf("Encode() error = %v, want ErrFrameTooLarge", err)
	}
}

func TestDecode_HeaderTooShort(t *testing.T) {
	_, err := Decode(make([]byte, HeaderSize-1))
	if err == nil {
		t.Error("Decode() should fail with short header")
	}
}

func TestDecode_PayloadTruncated(t *testing.T) {
	// Create valid header but truncated payload
	header := make([]byte, HeaderSize)
	header[0] = FrameStreamData
	// Set length to 100
	header[2] = 0
	header[3] = 0
	header[4] = 0
	header[5] = 100

	// Only provide 50 bytes of payload
	data := append(header, make([]byte, 50)...)

	_, err := Decode(data)
	if err == nil {
		t.Error("Decode() should fail with truncated payload")
	}
}

func TestFrame_String(t *testing.T) {
	f := Frame{
		Type:     FrameStreamOpen,
		Flags:    FlagFinWrite,
		StreamID: 42,
		Payload:  []byte("test"),
	}

	s := f.String()
	if s == "" {
		t.Error("String() returned empty string")
	}
	if !bytes.Contains([]byte(s), []byte("STREAM_OPEN")) {
		t.Error("String() should contain frame type name")
	}
}

func TestPeerHello_EncodeDecode(t *testing.T) {
	agentID, _ := identity.NewAgentID()

	original := &PeerHello{
		Version:      ProtocolVersion,
		AgentID:      agentID,
		Timestamp:    1703001234,
		Capabilities: []string{"exit", "socks5"},
	}

	data := original.Encode()
	decoded, err := DecodePeerHello(data)
	if err != nil {
		t.Fatalf("DecodePeerHello() error = %v", err)
	}

	if decoded.Version != original.Version {
		t.Errorf("Version = %d, want %d", decoded.Version, original.Version)
	}
	if !decoded.AgentID.Equal(original.AgentID) {
		t.Errorf("AgentID mismatch")
	}
	if decoded.Timestamp != original.Timestamp {
		t.Errorf("Timestamp = %d, want %d", decoded.Timestamp, original.Timestamp)
	}
	if len(decoded.Capabilities) != len(original.Capabilities) {
		t.Fatalf("Capabilities length = %d, want %d", len(decoded.Capabilities), len(original.Capabilities))
	}
	for i, cap := range decoded.Capabilities {
		if cap != original.Capabilities[i] {
			t.Errorf("Capabilities[%d] = %s, want %s", i, cap, original.Capabilities[i])
		}
	}
}

func TestPeerHello_EmptyCapabilities(t *testing.T) {
	agentID, _ := identity.NewAgentID()

	original := &PeerHello{
		Version:      1,
		AgentID:      agentID,
		Timestamp:    12345,
		Capabilities: []string{},
	}

	data := original.Encode()
	decoded, err := DecodePeerHello(data)
	if err != nil {
		t.Fatalf("DecodePeerHello() error = %v", err)
	}

	if len(decoded.Capabilities) != 0 {
		t.Errorf("Capabilities length = %d, want 0", len(decoded.Capabilities))
	}
}

func TestStreamOpen_EncodeDecode_IPv4(t *testing.T) {
	path1, _ := identity.NewAgentID()
	path2, _ := identity.NewAgentID()

	original := &StreamOpen{
		RequestID:     12345678,
		AddressType:   AddrTypeIPv4,
		Address:       []byte{10, 0, 0, 1},
		Port:          8080,
		TTL:           15,
		RemainingPath: []identity.AgentID{path1, path2},
	}

	data := original.Encode()
	decoded, err := DecodeStreamOpen(data)
	if err != nil {
		t.Fatalf("DecodeStreamOpen() error = %v", err)
	}

	if decoded.RequestID != original.RequestID {
		t.Errorf("RequestID = %d, want %d", decoded.RequestID, original.RequestID)
	}
	if decoded.AddressType != AddrTypeIPv4 {
		t.Errorf("AddressType = %d, want %d", decoded.AddressType, AddrTypeIPv4)
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
	if len(decoded.RemainingPath) != 2 {
		t.Fatalf("RemainingPath length = %d, want 2", len(decoded.RemainingPath))
	}
}

func TestStreamOpen_EncodeDecode_Domain(t *testing.T) {
	domain := "example.com"
	address := make([]byte, 1+len(domain))
	address[0] = byte(len(domain))
	copy(address[1:], domain)

	original := &StreamOpen{
		RequestID:     1,
		AddressType:   AddrTypeDomain,
		Address:       address,
		Port:          443,
		TTL:           10,
		RemainingPath: []identity.AgentID{},
	}

	data := original.Encode()
	decoded, err := DecodeStreamOpen(data)
	if err != nil {
		t.Fatalf("DecodeStreamOpen() error = %v", err)
	}

	if decoded.GetDestinationDomain() != domain {
		t.Errorf("Domain = %s, want %s", decoded.GetDestinationDomain(), domain)
	}
}

func TestStreamOpen_GetDestinationIP(t *testing.T) {
	s := &StreamOpen{
		AddressType: AddrTypeIPv4,
		Address:     []byte{192, 168, 1, 100},
	}

	ip := s.GetDestinationIP()
	if ip == nil {
		t.Fatal("GetDestinationIP() returned nil")
	}
	if ip.String() != "192.168.1.100" {
		t.Errorf("IP = %s, want 192.168.1.100", ip.String())
	}
}

func TestStreamOpenAck_EncodeDecode(t *testing.T) {
	original := &StreamOpenAck{
		RequestID:     12345,
		BoundAddrType: AddrTypeIPv4,
		BoundAddr:     []byte{10, 0, 0, 1},
		BoundPort:     22,
	}

	data := original.Encode()
	decoded, err := DecodeStreamOpenAck(data)
	if err != nil {
		t.Fatalf("DecodeStreamOpenAck() error = %v", err)
	}

	if decoded.RequestID != original.RequestID {
		t.Errorf("RequestID = %d, want %d", decoded.RequestID, original.RequestID)
	}
	if decoded.BoundPort != original.BoundPort {
		t.Errorf("BoundPort = %d, want %d", decoded.BoundPort, original.BoundPort)
	}
}

func TestStreamOpenErr_EncodeDecode(t *testing.T) {
	original := &StreamOpenErr{
		RequestID: 12345,
		ErrorCode: ErrConnectionRefused,
		Message:   "Connection refused by target",
	}

	data := original.Encode()
	decoded, err := DecodeStreamOpenErr(data)
	if err != nil {
		t.Fatalf("DecodeStreamOpenErr() error = %v", err)
	}

	if decoded.RequestID != original.RequestID {
		t.Errorf("RequestID = %d, want %d", decoded.RequestID, original.RequestID)
	}
	if decoded.ErrorCode != original.ErrorCode {
		t.Errorf("ErrorCode = %d, want %d", decoded.ErrorCode, original.ErrorCode)
	}
	if decoded.Message != original.Message {
		t.Errorf("Message = %s, want %s", decoded.Message, original.Message)
	}
}

func TestStreamReset_EncodeDecode(t *testing.T) {
	original := &StreamReset{ErrorCode: ErrResourceLimit}

	data := original.Encode()
	decoded, err := DecodeStreamReset(data)
	if err != nil {
		t.Fatalf("DecodeStreamReset() error = %v", err)
	}

	if decoded.ErrorCode != original.ErrorCode {
		t.Errorf("ErrorCode = %d, want %d", decoded.ErrorCode, original.ErrorCode)
	}
}

func TestKeepalive_EncodeDecode(t *testing.T) {
	original := &Keepalive{Timestamp: 1703001234567}

	data := original.Encode()
	decoded, err := DecodeKeepalive(data)
	if err != nil {
		t.Fatalf("DecodeKeepalive() error = %v", err)
	}

	if decoded.Timestamp != original.Timestamp {
		t.Errorf("Timestamp = %d, want %d", decoded.Timestamp, original.Timestamp)
	}
}

func TestRouteAdvertise_EncodeDecode(t *testing.T) {
	origin, _ := identity.NewAgentID()
	path1, _ := identity.NewAgentID()
	seen1, _ := identity.NewAgentID()

	original := &RouteAdvertise{
		OriginAgent: origin,
		Sequence:    42,
		Routes: []Route{
			{
				AddressFamily: AddrFamilyIPv4,
				PrefixLength:  8,
				Prefix:        []byte{10, 0, 0, 0},
				Metric:        1,
			},
			{
				AddressFamily: AddrFamilyIPv4,
				PrefixLength:  16,
				Prefix:        []byte{192, 168, 0, 0},
				Metric:        2,
			},
		},
		Path:   []identity.AgentID{path1},
		SeenBy: []identity.AgentID{origin, seen1},
	}

	data := original.Encode()
	decoded, err := DecodeRouteAdvertise(data)
	if err != nil {
		t.Fatalf("DecodeRouteAdvertise() error = %v", err)
	}

	if !decoded.OriginAgent.Equal(original.OriginAgent) {
		t.Error("OriginAgent mismatch")
	}
	if decoded.Sequence != original.Sequence {
		t.Errorf("Sequence = %d, want %d", decoded.Sequence, original.Sequence)
	}
	if len(decoded.Routes) != len(original.Routes) {
		t.Fatalf("Routes length = %d, want %d", len(decoded.Routes), len(original.Routes))
	}
	for i, route := range decoded.Routes {
		if route.AddressFamily != original.Routes[i].AddressFamily {
			t.Errorf("Route[%d].AddressFamily = %d, want %d", i, route.AddressFamily, original.Routes[i].AddressFamily)
		}
		if route.PrefixLength != original.Routes[i].PrefixLength {
			t.Errorf("Route[%d].PrefixLength = %d, want %d", i, route.PrefixLength, original.Routes[i].PrefixLength)
		}
		if route.Metric != original.Routes[i].Metric {
			t.Errorf("Route[%d].Metric = %d, want %d", i, route.Metric, original.Routes[i].Metric)
		}
	}
	if len(decoded.Path) != 1 {
		t.Errorf("Path length = %d, want 1", len(decoded.Path))
	}
	if len(decoded.SeenBy) != 2 {
		t.Errorf("SeenBy length = %d, want 2", len(decoded.SeenBy))
	}
}

func TestRouteWithdraw_EncodeDecode(t *testing.T) {
	origin, _ := identity.NewAgentID()

	original := &RouteWithdraw{
		OriginAgent: origin,
		Sequence:    100,
		Routes: []Route{
			{
				AddressFamily: AddrFamilyIPv4,
				PrefixLength:  24,
				Prefix:        []byte{10, 5, 3, 0},
				Metric:        0,
			},
		},
		SeenBy: []identity.AgentID{origin},
	}

	data := original.Encode()
	decoded, err := DecodeRouteWithdraw(data)
	if err != nil {
		t.Fatalf("DecodeRouteWithdraw() error = %v", err)
	}

	if decoded.Sequence != original.Sequence {
		t.Errorf("Sequence = %d, want %d", decoded.Sequence, original.Sequence)
	}
	if len(decoded.Routes) != 1 {
		t.Fatalf("Routes length = %d, want 1", len(decoded.Routes))
	}
}

// TestRouteAdvertise_UnknownAddressFamily verifies that a route carrying an
// unrecognized address family is rejected loudly, at the route's own offset,
// instead of being parsed as a guessed 16-byte prefix that would mis-parse
// the rest of the frame.
func TestRouteAdvertise_UnknownAddressFamily(t *testing.T) {
	origin, _ := identity.NewAgentID()

	original := &RouteAdvertise{
		OriginAgent: origin,
		Sequence:    1,
		Routes: []Route{
			{
				AddressFamily: 0x7F, // unrecognized family
				PrefixLength:  8,
				Prefix:        []byte{10, 0, 0, 0},
				Metric:        1,
			},
		},
		Path:   []identity.AgentID{origin},
		SeenBy: []identity.AgentID{origin},
	}

	_, err := DecodeRouteAdvertise(original.Encode())
	if err == nil {
		t.Fatal("DecodeRouteAdvertise() should fail on unknown address family")
	}
	if !errors.Is(err, ErrInvalidFrame) {
		t.Errorf("error = %v, want ErrInvalidFrame", err)
	}
	if !strings.Contains(err.Error(), "unknown address family") {
		t.Errorf("error = %v, want mention of unknown address family", err)
	}
}

// TestRouteWithdraw_UnknownAddressFamily verifies the same loud failure for
// ROUTE_WITHDRAW.
func TestRouteWithdraw_UnknownAddressFamily(t *testing.T) {
	origin, _ := identity.NewAgentID()

	original := &RouteWithdraw{
		OriginAgent: origin,
		Sequence:    1,
		Routes: []Route{
			{
				AddressFamily: 0x7F, // unrecognized family
				PrefixLength:  24,
				Prefix:        []byte{10, 5, 3, 0},
				Metric:        0,
			},
		},
		SeenBy: []identity.AgentID{origin},
	}

	_, err := DecodeRouteWithdraw(original.Encode())
	if err == nil {
		t.Fatal("DecodeRouteWithdraw() should fail on unknown address family")
	}
	if !errors.Is(err, ErrInvalidFrame) {
		t.Errorf("error = %v, want ErrInvalidFrame", err)
	}
	if !strings.Contains(err.Error(), "unknown address family") {
		t.Errorf("error = %v, want mention of unknown address family", err)
	}
}

func TestFrameReaderWriter(t *testing.T) {
	// Create a buffer to simulate network connection
	buf := new(bytes.Buffer)

	writer := NewFrameWriter(buf)
	reader := NewFrameReader(buf)

	// Write multiple frames
	frames := []*Frame{
		{Type: FrameStreamOpen, StreamID: 1, Payload: []byte("open")},
		{Type: FrameStreamData, StreamID: 1, Payload: []byte("data payload here")},
		{Type: FrameStreamClose, Flags: FlagFinWrite, StreamID: 1, Payload: []byte{}},
	}

	for _, f := range frames {
		if err := writer.Write(f); err != nil {
			t.Fatalf("Write() error = %v", err)
		}
	}

	// Read frames back
	for i, expected := range frames {
		got, err := reader.Read()
		if err != nil {
			t.Fatalf("Read() frame %d error = %v", i, err)
		}

		if got.Type != expected.Type {
			t.Errorf("Frame %d Type = %d, want %d", i, got.Type, expected.Type)
		}
		if got.Flags != expected.Flags {
			t.Errorf("Frame %d Flags = %d, want %d", i, got.Flags, expected.Flags)
		}
		if got.StreamID != expected.StreamID {
			t.Errorf("Frame %d StreamID = %d, want %d", i, got.StreamID, expected.StreamID)
		}
		if !bytes.Equal(got.Payload, expected.Payload) {
			t.Errorf("Frame %d Payload mismatch", i)
		}
	}
}

func TestFrameReader_EOF(t *testing.T) {
	buf := new(bytes.Buffer)
	reader := NewFrameReader(buf)

	_, err := reader.Read()
	if err != io.EOF {
		t.Errorf("Read() on empty buffer error = %v, want io.EOF", err)
	}
}

func TestFrameWriter_WriteFrame(t *testing.T) {
	buf := new(bytes.Buffer)
	writer := NewFrameWriter(buf)

	err := writer.WriteFrame(FrameKeepalive, 0, ControlStreamID, []byte{1, 2, 3, 4, 5, 6, 7, 8})
	if err != nil {
		t.Fatalf("WriteFrame() error = %v", err)
	}

	reader := NewFrameReader(buf)
	f, err := reader.Read()
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}

	if f.Type != FrameKeepalive {
		t.Errorf("Type = %d, want %d", f.Type, FrameKeepalive)
	}
	if f.StreamID != ControlStreamID {
		t.Errorf("StreamID = %d, want %d", f.StreamID, ControlStreamID)
	}
}

// shortWriter deliberately writes at most max bytes per call to exercise
// writeFull's short-write loop.
type shortWriter struct {
	buf bytes.Buffer
	max int
}

func (w *shortWriter) Write(p []byte) (int, error) {
	if len(p) > w.max {
		p = p[:w.max]
	}
	return w.buf.Write(p)
}

func TestFrameWriter_WriteMatchesEncode(t *testing.T) {
	frames := []*Frame{
		{Type: FrameStreamOpen, StreamID: 1, Payload: []byte("open")},
		{Type: FrameStreamData, StreamID: 42, Payload: make([]byte, 1024)},
		{Type: FrameKeepalive, StreamID: ControlStreamID, Payload: nil},
	}

	for i, f := range frames {
		want, err := f.Encode()
		if err != nil {
			t.Fatalf("frame %d Encode() error = %v", i, err)
		}

		// The two-write path must produce bytes identical to Encode, even
		// through a writer that returns short writes.
		w := &shortWriter{max: 5}
		if err := NewFrameWriter(w).Write(f); err != nil {
			t.Fatalf("frame %d Write() error = %v", i, err)
		}
		if got := w.buf.Bytes(); !bytes.Equal(got, want) {
			t.Errorf("frame %d written bytes differ from Encode()", i)
		}
	}

	// Oversized payloads must be rejected before any bytes hit the writer.
	w := &shortWriter{max: 5}
	big := &Frame{Type: FrameStreamData, StreamID: 1, Payload: make([]byte, MaxPayloadSize+1)}
	if err := NewFrameWriter(w).Write(big); err != ErrFrameTooLarge {
		t.Errorf("oversized Write() error = %v, want ErrFrameTooLarge", err)
	}
	if w.buf.Len() != 0 {
		t.Errorf("oversized Write() wrote %d bytes, want 0", w.buf.Len())
	}
}

func TestDecodeStreamOpen_InvalidAddressType(t *testing.T) {
	data := make([]byte, 20)
	data[8] = 0xFF // Invalid address type

	_, err := DecodeStreamOpen(data)
	if err == nil {
		t.Error("DecodeStreamOpen() should fail with invalid address type")
	}
}

func TestDecodePeerHello_TooShort(t *testing.T) {
	_, err := DecodePeerHello(make([]byte, 10))
	if err == nil {
		t.Error("DecodePeerHello() should fail with short data")
	}
}

func TestDecodeKeepalive_TooShort(t *testing.T) {
	_, err := DecodeKeepalive(make([]byte, 4))
	if err == nil {
		t.Error("DecodeKeepalive() should fail with short data")
	}
}

func TestDecodeStreamReset_TooShort(t *testing.T) {
	_, err := DecodeStreamReset(make([]byte, 1))
	if err == nil {
		t.Error("DecodeStreamReset() should fail with short data")
	}
}

func TestConstants(t *testing.T) {
	if HeaderSize != 14 {
		t.Errorf("HeaderSize = %d, want 14", HeaderSize)
	}
	if MaxPayloadSize != 16384 {
		t.Errorf("MaxPayloadSize = %d, want 16384", MaxPayloadSize)
	}
	if MaxFrameSize != HeaderSize+MaxPayloadSize {
		t.Errorf("MaxFrameSize = %d, want %d", MaxFrameSize, HeaderSize+MaxPayloadSize)
	}
	if ControlStreamID != 0 {
		t.Errorf("ControlStreamID = %d, want 0", ControlStreamID)
	}
	if ProtocolVersion != 1 {
		t.Errorf("ProtocolVersion = %d, want 1", ProtocolVersion)
	}
}

func BenchmarkFrame_Encode(b *testing.B) {
	f := &Frame{
		Type:     FrameStreamData,
		Flags:    0,
		StreamID: 12345,
		Payload:  make([]byte, 1024),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = f.Encode()
	}
}

func BenchmarkFrame_Decode(b *testing.B) {
	f := &Frame{
		Type:     FrameStreamData,
		Flags:    0,
		StreamID: 12345,
		Payload:  make([]byte, 1024),
	}
	data, _ := f.Encode()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = Decode(data)
	}
}

func BenchmarkFrameWriter_Write(b *testing.B) {
	f := &Frame{
		Type:     FrameStreamData,
		Flags:    0,
		StreamID: 12345,
		Payload:  make([]byte, 1024),
	}
	fw := NewFrameWriter(io.Discard)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = fw.Write(f)
	}
}

func TestNodeInfoAdvertise_EncodeDecode(t *testing.T) {
	origin, _ := identity.NewAgentID()
	seen1, _ := identity.NewAgentID()

	original := &NodeInfoAdvertise{
		OriginAgent: origin,
		Sequence:    42,
		Info: NodeInfo{
			DisplayName: "test-agent",
			Hostname:    "test-host.local",
			OS:          "linux",
			Arch:        "amd64",
			Version:     "1.0.0",
			StartTime:   1703001234,
			IPAddresses: []string{"192.168.1.100", "10.0.0.5"},
		},
		SeenBy: []identity.AgentID{origin, seen1},
	}

	data := original.Encode()
	decoded, err := DecodeNodeInfoAdvertise(data)
	if err != nil {
		t.Fatalf("DecodeNodeInfoAdvertise() error = %v", err)
	}

	if !decoded.OriginAgent.Equal(original.OriginAgent) {
		t.Error("OriginAgent mismatch")
	}
	if decoded.Sequence != original.Sequence {
		t.Errorf("Sequence = %d, want %d", decoded.Sequence, original.Sequence)
	}
	if decoded.Info.DisplayName != original.Info.DisplayName {
		t.Errorf("DisplayName = %s, want %s", decoded.Info.DisplayName, original.Info.DisplayName)
	}
	if decoded.Info.Hostname != original.Info.Hostname {
		t.Errorf("Hostname = %s, want %s", decoded.Info.Hostname, original.Info.Hostname)
	}
	if decoded.Info.OS != original.Info.OS {
		t.Errorf("OS = %s, want %s", decoded.Info.OS, original.Info.OS)
	}
	if decoded.Info.Arch != original.Info.Arch {
		t.Errorf("Arch = %s, want %s", decoded.Info.Arch, original.Info.Arch)
	}
	if decoded.Info.Version != original.Info.Version {
		t.Errorf("Version = %s, want %s", decoded.Info.Version, original.Info.Version)
	}
	if decoded.Info.StartTime != original.Info.StartTime {
		t.Errorf("StartTime = %d, want %d", decoded.Info.StartTime, original.Info.StartTime)
	}
	if len(decoded.Info.IPAddresses) != len(original.Info.IPAddresses) {
		t.Fatalf("IPAddresses length = %d, want %d", len(decoded.Info.IPAddresses), len(original.Info.IPAddresses))
	}
	for i, ip := range decoded.Info.IPAddresses {
		if ip != original.Info.IPAddresses[i] {
			t.Errorf("IPAddresses[%d] = %s, want %s", i, ip, original.Info.IPAddresses[i])
		}
	}
	if len(decoded.SeenBy) != len(original.SeenBy) {
		t.Errorf("SeenBy length = %d, want %d", len(decoded.SeenBy), len(original.SeenBy))
	}
}

func TestNodeInfoAdvertise_WithPeers(t *testing.T) {
	origin, _ := identity.NewAgentID()
	peer1, _ := identity.NewAgentID()
	peer2, _ := identity.NewAgentID()

	original := &NodeInfoAdvertise{
		OriginAgent: origin,
		Sequence:    100,
		Info: NodeInfo{
			DisplayName: "agent-with-peers",
			Hostname:    "host1",
			OS:          "darwin",
			Arch:        "arm64",
			Version:     "2.0.0",
			StartTime:   1703001234,
			IPAddresses: []string{"10.0.0.1"},
			Peers: []PeerConnectionInfo{
				{
					PeerID:    peer1,
					Transport: "quic",
					RTTMs:     5,
					IsDialer:  true,
				},
				{
					PeerID:    peer2,
					Transport: "ws",
					RTTMs:     25,
					IsDialer:  false,
				},
			},
			Shells: []string{"bash", "sh", "zsh"},
		},
		SeenBy: []identity.AgentID{origin},
	}

	data := original.Encode()
	decoded, err := DecodeNodeInfoAdvertise(data)
	if err != nil {
		t.Fatalf("DecodeNodeInfoAdvertise() error = %v", err)
	}

	// Verify peers were decoded correctly
	if len(decoded.Info.Peers) != 2 {
		t.Fatalf("Peers length = %d, want 2", len(decoded.Info.Peers))
	}

	// Check first peer
	if !bytes.Equal(decoded.Info.Peers[0].PeerID[:], peer1[:]) {
		t.Error("Peer[0].PeerID mismatch")
	}
	if decoded.Info.Peers[0].Transport != "quic" {
		t.Errorf("Peer[0].Transport = %s, want quic", decoded.Info.Peers[0].Transport)
	}
	if decoded.Info.Peers[0].RTTMs != 5 {
		t.Errorf("Peer[0].RTTMs = %d, want 5", decoded.Info.Peers[0].RTTMs)
	}
	if !decoded.Info.Peers[0].IsDialer {
		t.Error("Peer[0].IsDialer = false, want true")
	}

	// Check second peer
	if !bytes.Equal(decoded.Info.Peers[1].PeerID[:], peer2[:]) {
		t.Error("Peer[1].PeerID mismatch")
	}
	if decoded.Info.Peers[1].Transport != "ws" {
		t.Errorf("Peer[1].Transport = %s, want ws", decoded.Info.Peers[1].Transport)
	}
	if decoded.Info.Peers[1].RTTMs != 25 {
		t.Errorf("Peer[1].RTTMs = %d, want 25", decoded.Info.Peers[1].RTTMs)
	}
	if decoded.Info.Peers[1].IsDialer {
		t.Error("Peer[1].IsDialer = true, want false")
	}

	// Check shells
	if len(decoded.Info.Shells) != 3 {
		t.Fatalf("Shells length = %d, want 3", len(decoded.Info.Shells))
	}
	expectedShells := []string{"bash", "sh", "zsh"}
	for i, sh := range decoded.Info.Shells {
		if sh != expectedShells[i] {
			t.Errorf("Shells[%d] = %s, want %s", i, sh, expectedShells[i])
		}
	}
}

func TestNodeInfoAdvertise_BackwardCompatibility(t *testing.T) {
	// Simulate old-format NodeInfo (without peers) by encoding without peers
	// then decoding - should work and have empty peers slice
	origin, _ := identity.NewAgentID()

	original := &NodeInfoAdvertise{
		OriginAgent: origin,
		Sequence:    1,
		Info: NodeInfo{
			DisplayName: "old-agent",
			Hostname:    "oldhost",
			OS:          "linux",
			Arch:        "amd64",
			Version:     "0.9.0",
			StartTime:   1700000000,
			IPAddresses: []string{},
			Peers:       nil, // No peers (old format)
		},
		SeenBy: []identity.AgentID{},
	}

	data := original.Encode()
	decoded, err := DecodeNodeInfoAdvertise(data)
	if err != nil {
		t.Fatalf("DecodeNodeInfoAdvertise() error = %v", err)
	}

	// Should decode successfully with empty or nil peers
	if len(decoded.Info.Peers) != 0 {
		t.Errorf("Peers length = %d, want 0", len(decoded.Info.Peers))
	}
}

func TestNodeInfoAdvertise_MaxPeers(t *testing.T) {
	origin, _ := identity.NewAgentID()

	// Create more than MaxPeersInNodeInfo peers
	peers := make([]PeerConnectionInfo, MaxPeersInNodeInfo+10)
	for i := range peers {
		peerID, _ := identity.NewAgentID()
		peers[i] = PeerConnectionInfo{
			PeerID:    peerID,
			Transport: "quic",
			RTTMs:     int64(i),
			IsDialer:  i%2 == 0,
		}
	}

	original := &NodeInfoAdvertise{
		OriginAgent: origin,
		Sequence:    1,
		Info: NodeInfo{
			DisplayName: "many-peers",
			Hostname:    "host",
			OS:          "linux",
			Arch:        "amd64",
			Version:     "1.0.0",
			StartTime:   1703001234,
			IPAddresses: []string{},
			Peers:       peers,
		},
		SeenBy: []identity.AgentID{},
	}

	data := original.Encode()
	decoded, err := DecodeNodeInfoAdvertise(data)
	if err != nil {
		t.Fatalf("DecodeNodeInfoAdvertise() error = %v", err)
	}

	// Should be limited to MaxPeersInNodeInfo
	if len(decoded.Info.Peers) != MaxPeersInNodeInfo {
		t.Errorf("Peers length = %d, want %d", len(decoded.Info.Peers), MaxPeersInNodeInfo)
	}
}

func TestDecodeNodeInfoAdvertise_TooShort(t *testing.T) {
	_, err := DecodeNodeInfoAdvertise(make([]byte, 20))
	if err == nil {
		t.Error("DecodeNodeInfoAdvertise() should fail with short data")
	}
}

// ============================================================================
// Ephemeral Public Key Tests (E2E Encryption)
// ============================================================================

func TestStreamOpen_WithEphemeralKey(t *testing.T) {
	path1, _ := identity.NewAgentID()

	// Generate a mock ephemeral public key (32 bytes)
	var ephemeralKey [EphemeralKeySize]byte
	for i := range ephemeralKey {
		ephemeralKey[i] = byte(i + 1)
	}

	original := &StreamOpen{
		RequestID:       12345678,
		AddressType:     AddrTypeIPv4,
		Address:         []byte{10, 0, 0, 1},
		Port:            8080,
		TTL:             15,
		RemainingPath:   []identity.AgentID{path1},
		EphemeralPubKey: ephemeralKey,
	}

	data := original.Encode()
	decoded, err := DecodeStreamOpen(data)
	if err != nil {
		t.Fatalf("DecodeStreamOpen() error = %v", err)
	}

	// Verify ephemeral key was encoded/decoded correctly
	if decoded.EphemeralPubKey != original.EphemeralPubKey {
		t.Errorf("EphemeralPubKey mismatch: got %v, want %v", decoded.EphemeralPubKey, original.EphemeralPubKey)
	}

	// Verify the key is not zero
	var zeroKey [EphemeralKeySize]byte
	if decoded.EphemeralPubKey == zeroKey {
		t.Error("Decoded EphemeralPubKey should not be zero")
	}
}

func TestStreamOpen_IPv6WithEphemeralKey(t *testing.T) {
	// IPv6 address: ::1
	ipv6Addr := []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1}

	var ephemeralKey [EphemeralKeySize]byte
	for i := range ephemeralKey {
		ephemeralKey[i] = byte(255 - i)
	}

	original := &StreamOpen{
		RequestID:       999,
		AddressType:     AddrTypeIPv6,
		Address:         ipv6Addr,
		Port:            443,
		TTL:             10,
		RemainingPath:   []identity.AgentID{},
		EphemeralPubKey: ephemeralKey,
	}

	data := original.Encode()
	decoded, err := DecodeStreamOpen(data)
	if err != nil {
		t.Fatalf("DecodeStreamOpen() error = %v", err)
	}

	if decoded.AddressType != AddrTypeIPv6 {
		t.Errorf("AddressType = %d, want %d", decoded.AddressType, AddrTypeIPv6)
	}
	if !bytes.Equal(decoded.Address, ipv6Addr) {
		t.Errorf("Address mismatch for IPv6")
	}
	if decoded.EphemeralPubKey != original.EphemeralPubKey {
		t.Errorf("EphemeralPubKey mismatch")
	}
}

func TestStreamOpenAck_WithEphemeralKey(t *testing.T) {
	var ephemeralKey [EphemeralKeySize]byte
	for i := range ephemeralKey {
		ephemeralKey[i] = byte(i * 2)
	}

	original := &StreamOpenAck{
		RequestID:       12345,
		BoundAddrType:   AddrTypeIPv4,
		BoundAddr:       []byte{10, 0, 0, 1},
		BoundPort:       22,
		EphemeralPubKey: ephemeralKey,
	}

	data := original.Encode()
	decoded, err := DecodeStreamOpenAck(data)
	if err != nil {
		t.Fatalf("DecodeStreamOpenAck() error = %v", err)
	}

	if decoded.EphemeralPubKey != original.EphemeralPubKey {
		t.Errorf("EphemeralPubKey mismatch")
	}
	if decoded.RequestID != original.RequestID {
		t.Errorf("RequestID = %d, want %d", decoded.RequestID, original.RequestID)
	}
}

func TestStreamOpenAck_IPv6WithEphemeralKey(t *testing.T) {
	// Test with IPv6 bound address
	ipv6Addr := []byte{0x20, 0x01, 0x0d, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1}

	var ephemeralKey [EphemeralKeySize]byte
	for i := range ephemeralKey {
		ephemeralKey[i] = byte(i + 100)
	}

	original := &StreamOpenAck{
		RequestID:       54321,
		BoundAddrType:   AddrTypeIPv6,
		BoundAddr:       ipv6Addr,
		BoundPort:       8443,
		EphemeralPubKey: ephemeralKey,
	}

	data := original.Encode()
	decoded, err := DecodeStreamOpenAck(data)
	if err != nil {
		t.Fatalf("DecodeStreamOpenAck() error = %v", err)
	}

	if decoded.BoundAddrType != AddrTypeIPv6 {
		t.Errorf("BoundAddrType = %d, want %d", decoded.BoundAddrType, AddrTypeIPv6)
	}
	if !bytes.Equal(decoded.BoundAddr, ipv6Addr) {
		t.Errorf("BoundAddr mismatch")
	}
	if decoded.EphemeralPubKey != original.EphemeralPubKey {
		t.Errorf("EphemeralPubKey mismatch")
	}
}

func TestDecodeStreamOpen_MissingEphemeralKey(t *testing.T) {
	// Create a truncated StreamOpen without ephemeral key
	// This simulates a malformed frame
	data := make([]byte, 13) // Minimum without key
	data[8] = AddrTypeIPv4   // Address type

	_, err := DecodeStreamOpen(data)
	if err == nil {
		t.Error("DecodeStreamOpen() should fail when ephemeral key is missing")
	}
}

func TestDecodeStreamOpenAck_MissingEphemeralKey(t *testing.T) {
	// Create truncated StreamOpenAck without ephemeral key
	data := make([]byte, 11) // 8 + 1 + 2 (missing 32 byte key)

	_, err := DecodeStreamOpenAck(data)
	if err == nil {
		t.Error("DecodeStreamOpenAck() should fail when ephemeral key is missing")
	}
}

// ============================================================================
// Control Request/Response Tests
// ============================================================================

func TestControlRequest_EncodeDecode(t *testing.T) {
	target, _ := identity.NewAgentID()
	path1, _ := identity.NewAgentID()
	path2, _ := identity.NewAgentID()

	original := &ControlRequest{
		RequestID:   12345,
		ControlType: ControlTypeStatus,
		TargetAgent: target,
		Path:        []identity.AgentID{path1, path2},
		Data:        []byte("request payload data"),
	}

	data := original.Encode()
	decoded, err := DecodeControlRequest(data)
	if err != nil {
		t.Fatalf("DecodeControlRequest() error = %v", err)
	}

	if decoded.RequestID != original.RequestID {
		t.Errorf("RequestID = %d, want %d", decoded.RequestID, original.RequestID)
	}
	if decoded.ControlType != original.ControlType {
		t.Errorf("ControlType = %d, want %d", decoded.ControlType, original.ControlType)
	}
	if !decoded.TargetAgent.Equal(original.TargetAgent) {
		t.Error("TargetAgent mismatch")
	}
	if len(decoded.Path) != len(original.Path) {
		t.Errorf("Path length = %d, want %d", len(decoded.Path), len(original.Path))
	}
	if !bytes.Equal(decoded.Data, original.Data) {
		t.Errorf("Data mismatch")
	}
}

func TestControlRequest_EmptyPathAndData(t *testing.T) {
	target, _ := identity.NewAgentID()

	original := &ControlRequest{
		RequestID:   1,
		ControlType: ControlTypeStatus,
		TargetAgent: target,
		Path:        []identity.AgentID{},
		Data:        []byte{},
	}

	data := original.Encode()
	decoded, err := DecodeControlRequest(data)
	if err != nil {
		t.Fatalf("DecodeControlRequest() error = %v", err)
	}

	if len(decoded.Path) != 0 {
		t.Errorf("Path should be empty")
	}
	if len(decoded.Data) != 0 {
		t.Errorf("Data should be empty")
	}
}

func TestControlRequest_LargeData(t *testing.T) {
	target, _ := identity.NewAgentID()

	largeData := make([]byte, 10000)
	for i := range largeData {
		largeData[i] = byte(i % 256)
	}

	original := &ControlRequest{
		RequestID:   999,
		ControlType: ControlTypeRPC,
		TargetAgent: target,
		Path:        []identity.AgentID{},
		Data:        largeData,
	}

	data := original.Encode()
	decoded, err := DecodeControlRequest(data)
	if err != nil {
		t.Fatalf("DecodeControlRequest() error = %v", err)
	}

	if !bytes.Equal(decoded.Data, original.Data) {
		t.Errorf("Large data mismatch")
	}
}

func TestDecodeControlRequest_TooShort(t *testing.T) {
	_, err := DecodeControlRequest(make([]byte, 20))
	if err == nil {
		t.Error("DecodeControlRequest() should fail with short data")
	}
}

func TestControlResponse_EncodeDecode(t *testing.T) {
	original := &ControlResponse{
		RequestID:   12345,
		ControlType: ControlTypeStatus,
		Success:     true,
		Data:        []byte("status data here"),
	}

	data := original.Encode()
	decoded, err := DecodeControlResponse(data)
	if err != nil {
		t.Fatalf("DecodeControlResponse() error = %v", err)
	}

	if decoded.RequestID != original.RequestID {
		t.Errorf("RequestID = %d, want %d", decoded.RequestID, original.RequestID)
	}
	if decoded.ControlType != original.ControlType {
		t.Errorf("ControlType = %d, want %d", decoded.ControlType, original.ControlType)
	}
	if decoded.Success != original.Success {
		t.Errorf("Success = %v, want %v", decoded.Success, original.Success)
	}
	if !bytes.Equal(decoded.Data, original.Data) {
		t.Errorf("Data mismatch")
	}
}

func TestControlResponse_Failure(t *testing.T) {
	original := &ControlResponse{
		RequestID:   999,
		ControlType: ControlTypeStatus,
		Success:     false,
		Data:        []byte("error: agent not found"),
	}

	data := original.Encode()
	decoded, err := DecodeControlResponse(data)
	if err != nil {
		t.Fatalf("DecodeControlResponse() error = %v", err)
	}

	if decoded.Success != false {
		t.Error("Success should be false")
	}
}

func TestControlResponse_EmptyData(t *testing.T) {
	original := &ControlResponse{
		RequestID:   1,
		ControlType: ControlTypeStatus,
		Success:     true,
		Data:        []byte{},
	}

	data := original.Encode()
	decoded, err := DecodeControlResponse(data)
	if err != nil {
		t.Fatalf("DecodeControlResponse() error = %v", err)
	}

	if len(decoded.Data) != 0 {
		t.Errorf("Data should be empty, got %d bytes", len(decoded.Data))
	}
}

func TestDecodeControlResponse_TooShort(t *testing.T) {
	_, err := DecodeControlResponse(make([]byte, 10))
	if err == nil {
		t.Error("DecodeControlResponse() should fail with short data")
	}
}

func TestControlResponse_HasMoreFlag(t *testing.T) {
	// HasMore must survive a round-trip independently of Success.
	cases := []struct {
		name    string
		success bool
		hasMore bool
	}{
		{"success final", true, false},
		{"success chunk", true, true},
		{"failure", false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			original := &ControlResponse{
				RequestID:   42,
				ControlType: ControlTypeStatus,
				Success:     tc.success,
				HasMore:     tc.hasMore,
				Data:        []byte("chunk"),
			}
			decoded, err := DecodeControlResponse(original.Encode())
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if decoded.Success != tc.success {
				t.Errorf("Success = %v, want %v", decoded.Success, tc.success)
			}
			if decoded.HasMore != tc.hasMore {
				t.Errorf("HasMore = %v, want %v", decoded.HasMore, tc.hasMore)
			}
		})
	}
}

func TestControlResponse_DataTruncation(t *testing.T) {
	// Test that very large data is truncated during encoding
	largeData := make([]byte, MaxPayloadSize+1000)
	for i := range largeData {
		largeData[i] = byte(i % 256)
	}

	original := &ControlResponse{
		RequestID:   1,
		ControlType: ControlTypeStatus,
		Success:     true,
		Data:        largeData,
	}

	data := original.Encode()
	decoded, err := DecodeControlResponse(data)
	if err != nil {
		t.Fatalf("DecodeControlResponse() error = %v", err)
	}

	// Data should be truncated
	if len(decoded.Data) >= len(original.Data) {
		t.Errorf("Data should be truncated, got %d bytes", len(decoded.Data))
	}
}

// ============================================================================
// RouteAdvertise with DisplayName Tests
// ============================================================================

func TestRouteAdvertise_WithDisplayName(t *testing.T) {
	origin, _ := identity.NewAgentID()
	path1, _ := identity.NewAgentID()

	original := &RouteAdvertise{
		OriginAgent:       origin,
		OriginDisplayName: "exit-node-1",
		Sequence:          42,
		Routes: []Route{
			{
				AddressFamily: AddrFamilyIPv4,
				PrefixLength:  24,
				Prefix:        []byte{10, 0, 0, 0},
				Metric:        1,
			},
		},
		Path:   []identity.AgentID{path1},
		SeenBy: []identity.AgentID{origin},
	}

	data := original.Encode()
	decoded, err := DecodeRouteAdvertise(data)
	if err != nil {
		t.Fatalf("DecodeRouteAdvertise() error = %v", err)
	}

	if decoded.OriginDisplayName != original.OriginDisplayName {
		t.Errorf("OriginDisplayName = %s, want %s", decoded.OriginDisplayName, original.OriginDisplayName)
	}
}

func TestPeerHello_WithDisplayName(t *testing.T) {
	agentID, _ := identity.NewAgentID()

	original := &PeerHello{
		Version:      ProtocolVersion,
		AgentID:      agentID,
		Timestamp:    1703001234,
		DisplayName:  "transit-node-west",
		Capabilities: []string{"exit", "socks5"},
	}

	data := original.Encode()
	decoded, err := DecodePeerHello(data)
	if err != nil {
		t.Fatalf("DecodePeerHello() error = %v", err)
	}

	if decoded.DisplayName != original.DisplayName {
		t.Errorf("DisplayName = %s, want %s", decoded.DisplayName, original.DisplayName)
	}
}

func TestEncryptedData_EncodeDecode(t *testing.T) {
	tests := []struct {
		name      string
		encrypted bool
		data      []byte
	}{
		{"empty_plaintext", false, []byte{}},
		{"short_plaintext", false, []byte("hello")},
		{"long_plaintext", false, bytes.Repeat([]byte("A"), 1000)},
		{"encrypted_blob", true, bytes.Repeat([]byte{0xFF}, 60)}, // Simulates sealed box overhead
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			original := &EncryptedData{
				Encrypted: tc.encrypted,
				Data:      tc.data,
			}

			encoded := EncodeEncryptedData(original)

			// Verify format: flag(1) + len(2) + data
			if len(encoded) != 3+len(tc.data) {
				t.Errorf("encoded length = %d, want %d", len(encoded), 3+len(tc.data))
			}

			decoded, consumed, err := DecodeEncryptedData(encoded)
			if err != nil {
				t.Fatalf("DecodeEncryptedData() error = %v", err)
			}

			if consumed != len(encoded) {
				t.Errorf("consumed = %d, want %d", consumed, len(encoded))
			}

			if decoded.Encrypted != tc.encrypted {
				t.Errorf("Encrypted = %v, want %v", decoded.Encrypted, tc.encrypted)
			}

			if !bytes.Equal(decoded.Data, tc.data) {
				t.Errorf("Data mismatch")
			}
		})
	}
}

func TestEncodeNodeInfo_DecodeNodeInfo(t *testing.T) {
	original := &NodeInfo{
		DisplayName: "test-agent",
		Hostname:    "server-01.local",
		OS:          "linux",
		Arch:        "amd64",
		Version:     "1.0.0",
		StartTime:   1703001234,
		IPAddresses: []string{"192.168.1.100", "10.0.0.5"},
		Peers: []PeerConnectionInfo{
			{Transport: "quic", RTTMs: 15, IsDialer: true},
			{Transport: "h2", RTTMs: 25, IsDialer: false},
		},
		UDPEnabled:          true,
		Shells:              []string{"bash", "sh"},
		FileTransferEnabled: true,
		ShellEnabled:        true,
		IcmpEnabled:         true,
		AdvertisedListeners: []AdvertisedListener{
			{Transport: "h2", Address: "kreata.ee:3000", Path: "/mesh"},
			{Transport: "quic", Address: "1.2.3.4:4433"},
		},
		PID: 12345,
	}
	copy(original.PublicKey[:], bytes.Repeat([]byte{0xAB}, EphemeralKeySize))

	encoded := EncodeNodeInfo(original)
	decoded, err := DecodeNodeInfo(encoded)
	if err != nil {
		t.Fatalf("DecodeNodeInfo() error = %v", err)
	}

	if decoded.DisplayName != original.DisplayName {
		t.Errorf("DisplayName = %s, want %s", decoded.DisplayName, original.DisplayName)
	}
	if decoded.Hostname != original.Hostname {
		t.Errorf("Hostname = %s, want %s", decoded.Hostname, original.Hostname)
	}
	if decoded.OS != original.OS {
		t.Errorf("OS = %s, want %s", decoded.OS, original.OS)
	}
	if decoded.Arch != original.Arch {
		t.Errorf("Arch = %s, want %s", decoded.Arch, original.Arch)
	}
	if decoded.Version != original.Version {
		t.Errorf("Version = %s, want %s", decoded.Version, original.Version)
	}
	if decoded.StartTime != original.StartTime {
		t.Errorf("StartTime = %d, want %d", decoded.StartTime, original.StartTime)
	}
	if len(decoded.IPAddresses) != len(original.IPAddresses) {
		t.Errorf("IPAddresses count = %d, want %d", len(decoded.IPAddresses), len(original.IPAddresses))
	}
	if len(decoded.Peers) != len(original.Peers) {
		t.Errorf("Peers count = %d, want %d", len(decoded.Peers), len(original.Peers))
	}
	if decoded.PublicKey != original.PublicKey {
		t.Errorf("PublicKey mismatch")
	}
	if decoded.UDPEnabled != original.UDPEnabled {
		t.Errorf("UDPEnabled = %v, want %v", decoded.UDPEnabled, original.UDPEnabled)
	}
	if len(decoded.Shells) != len(original.Shells) {
		t.Fatalf("Shells count = %d, want %d", len(decoded.Shells), len(original.Shells))
	}
	for i, sh := range decoded.Shells {
		if sh != original.Shells[i] {
			t.Errorf("Shells[%d] = %s, want %s", i, sh, original.Shells[i])
		}
	}
	if decoded.FileTransferEnabled != original.FileTransferEnabled {
		t.Errorf("FileTransferEnabled = %v, want %v", decoded.FileTransferEnabled, original.FileTransferEnabled)
	}
	if decoded.ShellEnabled != original.ShellEnabled {
		t.Errorf("ShellEnabled = %v, want %v", decoded.ShellEnabled, original.ShellEnabled)
	}
	if decoded.IcmpEnabled != original.IcmpEnabled {
		t.Errorf("IcmpEnabled = %v, want %v", decoded.IcmpEnabled, original.IcmpEnabled)
	}
	if len(decoded.AdvertisedListeners) != len(original.AdvertisedListeners) {
		t.Fatalf("AdvertisedListeners count = %d, want %d", len(decoded.AdvertisedListeners), len(original.AdvertisedListeners))
	}
	for i, al := range decoded.AdvertisedListeners {
		if al != original.AdvertisedListeners[i] {
			t.Errorf("AdvertisedListeners[%d] = %+v, want %+v", i, al, original.AdvertisedListeners[i])
		}
	}
	if decoded.PID != original.PID {
		t.Errorf("PID = %d, want %d", decoded.PID, original.PID)
	}
}

// TestEncodeNodeInfo_PIDZeroRoundTrips verifies that an unset PID (zero
// value) survives a round-trip without becoming non-zero.
func TestEncodeNodeInfo_PIDZeroRoundTrips(t *testing.T) {
	original := &NodeInfo{
		DisplayName: "no-pid",
		Hostname:    "host",
		OS:          "linux",
		Arch:        "amd64",
		Version:     "1.0.0",
		StartTime:   1,
	}
	encoded := EncodeNodeInfo(original)
	decoded, err := DecodeNodeInfo(encoded)
	if err != nil {
		t.Fatalf("DecodeNodeInfo() error = %v", err)
	}
	if decoded.PID != 0 {
		t.Errorf("PID = %d, want 0", decoded.PID)
	}
}

// TestDecodeNodeInfo_BackwardCompat_NoPID verifies that a NodeInfo encoded
// by an older agent (no PID trailing bytes) still decodes cleanly. We
// simulate this by truncating the encoded payload before the 8-byte PID
// section.
func TestDecodeNodeInfo_BackwardCompat_NoPID(t *testing.T) {
	original := &NodeInfo{
		DisplayName: "old-agent",
		Hostname:    "host",
		OS:          "linux",
		Arch:        "amd64",
		Version:     "1.0.0",
		StartTime:   1,
		IcmpEnabled: true,
		PID:         9999,
	}
	encoded := EncodeNodeInfo(original)
	// Strip the trailing 8-byte PID section.
	truncated := encoded[:len(encoded)-8]
	decoded, err := DecodeNodeInfo(truncated)
	if err != nil {
		t.Fatalf("DecodeNodeInfo(truncated) error = %v", err)
	}
	if !decoded.IcmpEnabled {
		t.Errorf("IcmpEnabled = false, want true (must still decode last known field)")
	}
	if decoded.PID != 0 {
		t.Errorf("PID = %d, want 0 (must default to zero when bytes absent)", decoded.PID)
	}
}

func TestEncodeNodeInfo_AdvertisedListenersOmittedWhenEmpty(t *testing.T) {
	original := &NodeInfo{
		DisplayName: "no-advertise",
		Hostname:    "host",
		OS:          "linux",
		Arch:        "amd64",
		Version:     "1.0.0",
		StartTime:   1,
	}
	encoded := EncodeNodeInfo(original)
	decoded, err := DecodeNodeInfo(encoded)
	if err != nil {
		t.Fatalf("DecodeNodeInfo() error = %v", err)
	}
	if len(decoded.AdvertisedListeners) != 0 {
		t.Errorf("AdvertisedListeners = %+v, want empty", decoded.AdvertisedListeners)
	}
}

// TestDecodeNodeInfo_BackwardCompat_NoAdvertisedListeners verifies that a
// NodeInfo encoded by an older agent (no AdvertisedListeners trailing bytes)
// still decodes cleanly. We simulate this by truncating the encoded payload
// just past IcmpEnabled.
func TestDecodeNodeInfo_BackwardCompat_NoAdvertisedListeners(t *testing.T) {
	original := &NodeInfo{
		DisplayName:         "old-agent",
		Hostname:            "host",
		OS:                  "linux",
		Arch:                "amd64",
		Version:             "1.0.0",
		StartTime:           1,
		IcmpEnabled:         true,
		AdvertisedListeners: nil,
	}
	encoded := EncodeNodeInfo(original)
	// Strip everything after IcmpEnabled: AdvertisedListeners count (1 byte,
	// = 0 here) plus PID (8 bytes). The simulated older agent never emitted
	// either section.
	truncated := encoded[:len(encoded)-9]
	decoded, err := DecodeNodeInfo(truncated)
	if err != nil {
		t.Fatalf("DecodeNodeInfo(truncated) error = %v", err)
	}
	if !decoded.IcmpEnabled {
		t.Errorf("IcmpEnabled = false, want true (must still decode last known field)")
	}
	if len(decoded.AdvertisedListeners) != 0 {
		t.Errorf("AdvertisedListeners = %+v, want empty/nil", decoded.AdvertisedListeners)
	}
}

func TestEncodePath_DecodePath(t *testing.T) {
	tests := []struct {
		name    string
		pathLen int
	}{
		{"empty", 0},
		{"one", 1},
		{"few", 3},
		{"many", 10},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Generate path
			path := make([]identity.AgentID, tc.pathLen)
			for i := 0; i < tc.pathLen; i++ {
				id, _ := identity.NewAgentID()
				path[i] = id
			}

			encoded := EncodePath(path)
			decoded, err := DecodePath(encoded)
			if err != nil {
				t.Fatalf("DecodePath() error = %v", err)
			}

			if len(decoded) != len(path) {
				t.Errorf("path length = %d, want %d", len(decoded), len(path))
			}

			for i := range path {
				if decoded[i] != path[i] {
					t.Errorf("path[%d] mismatch", i)
				}
			}
		})
	}
}

func TestNodeInfoAdvertise_WithEncryption(t *testing.T) {
	agentID, _ := identity.NewAgentID()

	// Create NodeInfoAdvertise with encrypted data
	original := &NodeInfoAdvertise{
		OriginAgent: agentID,
		Sequence:    12345,
		EncInfo: &EncryptedData{
			Encrypted: true,
			Data:      bytes.Repeat([]byte{0xDE, 0xAD}, 100), // Simulates encrypted blob
		},
		SeenBy: []identity.AgentID{agentID},
	}

	encoded := original.Encode()
	decoded, err := DecodeNodeInfoAdvertise(encoded)
	if err != nil {
		t.Fatalf("DecodeNodeInfoAdvertise() error = %v", err)
	}

	if decoded.OriginAgent != original.OriginAgent {
		t.Errorf("OriginAgent mismatch")
	}
	if decoded.Sequence != original.Sequence {
		t.Errorf("Sequence = %d, want %d", decoded.Sequence, original.Sequence)
	}
	if decoded.EncInfo == nil {
		t.Fatal("EncInfo is nil")
	}
	if !decoded.EncInfo.Encrypted {
		t.Error("EncInfo.Encrypted = false, want true")
	}
	if !bytes.Equal(decoded.EncInfo.Data, original.EncInfo.Data) {
		t.Error("EncInfo.Data mismatch")
	}
	if len(decoded.SeenBy) != len(original.SeenBy) {
		t.Errorf("SeenBy length = %d, want %d", len(decoded.SeenBy), len(original.SeenBy))
	}
}

func TestRouteAdvertise_WithEncryptedPath(t *testing.T) {
	agentID, _ := identity.NewAgentID()

	// Create RouteAdvertise with encrypted path
	original := &RouteAdvertise{
		OriginAgent:       agentID,
		OriginDisplayName: "exit-node",
		Sequence:          54321,
		Routes: []Route{
			{AddressFamily: AddrFamilyIPv4, PrefixLength: 24, Prefix: []byte{10, 0, 0, 0}, Metric: 5},
		},
		EncPath: &EncryptedData{
			Encrypted: true,
			Data:      bytes.Repeat([]byte{0xBE, 0xEF}, 50), // Simulates encrypted path
		},
		SeenBy: []identity.AgentID{agentID},
	}

	encoded := original.Encode()
	decoded, err := DecodeRouteAdvertise(encoded)
	if err != nil {
		t.Fatalf("DecodeRouteAdvertise() error = %v", err)
	}

	if decoded.OriginAgent != original.OriginAgent {
		t.Errorf("OriginAgent mismatch")
	}
	if decoded.OriginDisplayName != original.OriginDisplayName {
		t.Errorf("OriginDisplayName = %s, want %s", decoded.OriginDisplayName, original.OriginDisplayName)
	}
	if decoded.Sequence != original.Sequence {
		t.Errorf("Sequence = %d, want %d", decoded.Sequence, original.Sequence)
	}
	if len(decoded.Routes) != len(original.Routes) {
		t.Errorf("Routes count = %d, want %d", len(decoded.Routes), len(original.Routes))
	}
	if decoded.EncPath == nil {
		t.Fatal("EncPath is nil")
	}
	if !decoded.EncPath.Encrypted {
		t.Error("EncPath.Encrypted = false, want true")
	}
	if !bytes.Equal(decoded.EncPath.Data, original.EncPath.Data) {
		t.Error("EncPath.Data mismatch")
	}
	// When encrypted, Path should be nil (caller must decrypt)
	if decoded.Path != nil {
		t.Error("Path should be nil when encrypted")
	}
}

// ============================================================================
// SleepCommand Tests
// ============================================================================

func TestSleepCommand_EncodeDecode(t *testing.T) {
	origin, _ := identity.NewAgentID()
	seen1, _ := identity.NewAgentID()
	seen2, _ := identity.NewAgentID()

	// Create a signature (just random bytes for testing wire format)
	var sig [SignatureSize]byte
	for i := range sig {
		sig[i] = byte(i + 1)
	}

	original := &SleepCommand{
		OriginAgent: origin,
		CommandID:   12345678,
		Timestamp:   1703001234,
		Signature:   sig,
		SeenBy:      []identity.AgentID{seen1, seen2},
	}

	data := original.Encode()
	decoded, err := DecodeSleepCommand(data)
	if err != nil {
		t.Fatalf("DecodeSleepCommand() error = %v", err)
	}

	if !decoded.OriginAgent.Equal(original.OriginAgent) {
		t.Error("OriginAgent mismatch")
	}
	if decoded.CommandID != original.CommandID {
		t.Errorf("CommandID = %d, want %d", decoded.CommandID, original.CommandID)
	}
	if decoded.Timestamp != original.Timestamp {
		t.Errorf("Timestamp = %d, want %d", decoded.Timestamp, original.Timestamp)
	}
	if decoded.Signature != original.Signature {
		t.Error("Signature mismatch")
	}
	if len(decoded.SeenBy) != len(original.SeenBy) {
		t.Errorf("SeenBy length = %d, want %d", len(decoded.SeenBy), len(original.SeenBy))
	}
	for i, id := range decoded.SeenBy {
		if !id.Equal(original.SeenBy[i]) {
			t.Errorf("SeenBy[%d] mismatch", i)
		}
	}
}

func TestSleepCommand_WithZeroSignature(t *testing.T) {
	origin, _ := identity.NewAgentID()

	// Zero signature (unsigned command)
	original := &SleepCommand{
		OriginAgent: origin,
		CommandID:   999,
		Timestamp:   1700000000,
		Signature:   [SignatureSize]byte{}, // All zeros
		SeenBy:      []identity.AgentID{origin},
	}

	data := original.Encode()
	decoded, err := DecodeSleepCommand(data)
	if err != nil {
		t.Fatalf("DecodeSleepCommand() error = %v", err)
	}

	if !decoded.IsZeroSignature() {
		t.Error("IsZeroSignature() = false, want true")
	}
}

func TestSleepCommand_EmptySeenBy(t *testing.T) {
	origin, _ := identity.NewAgentID()

	original := &SleepCommand{
		OriginAgent: origin,
		CommandID:   1,
		Timestamp:   1700000000,
		Signature:   [SignatureSize]byte{},
		SeenBy:      []identity.AgentID{},
	}

	data := original.Encode()
	decoded, err := DecodeSleepCommand(data)
	if err != nil {
		t.Fatalf("DecodeSleepCommand() error = %v", err)
	}

	if len(decoded.SeenBy) != 0 {
		t.Errorf("SeenBy should be empty, got %d", len(decoded.SeenBy))
	}
}

func TestSleepCommand_SignableBytes(t *testing.T) {
	origin, _ := identity.NewAgentID()
	seen1, _ := identity.NewAgentID()

	cmd := &SleepCommand{
		OriginAgent: origin,
		CommandID:   12345,
		Timestamp:   9999999,
		Signature:   [SignatureSize]byte{0xFF, 0xFF}, // Non-zero signature
		SeenBy:      []identity.AgentID{seen1},
	}

	signable := cmd.SignableBytes()

	// Should be exactly 32 bytes: 16 (AgentID) + 8 (CommandID) + 8 (Timestamp)
	if len(signable) != 32 {
		t.Errorf("SignableBytes length = %d, want 32", len(signable))
	}

	// Verify the signable bytes don't include signature or SeenBy
	// by checking that the same command with different signature/seenBy
	// produces the same signable bytes
	cmd2 := &SleepCommand{
		OriginAgent: origin,
		CommandID:   12345,
		Timestamp:   9999999,
		Signature:   [SignatureSize]byte{0xAA, 0xBB}, // Different signature
		SeenBy:      []identity.AgentID{},            // Different SeenBy
	}

	signable2 := cmd2.SignableBytes()
	if !bytes.Equal(signable, signable2) {
		t.Error("SignableBytes should be identical regardless of Signature/SeenBy")
	}
}

func TestSleepCommand_IsZeroSignature(t *testing.T) {
	cmd := &SleepCommand{}

	// Zero signature
	if !cmd.IsZeroSignature() {
		t.Error("IsZeroSignature() = false for zero signature")
	}

	// Non-zero signature
	cmd.Signature[0] = 1
	if cmd.IsZeroSignature() {
		t.Error("IsZeroSignature() = true for non-zero signature")
	}

	// Last byte non-zero
	cmd.Signature[0] = 0
	cmd.Signature[SignatureSize-1] = 0xFF
	if cmd.IsZeroSignature() {
		t.Error("IsZeroSignature() = true when last byte is non-zero")
	}
}

func TestDecodeSleepCommand_TooShort(t *testing.T) {
	// Minimum size is 16 + 8 + 8 + 64 + 1 = 97 bytes
	_, err := DecodeSleepCommand(make([]byte, 96))
	if err == nil {
		t.Error("DecodeSleepCommand() should fail with short data")
	}
}

// ============================================================================
// WakeCommand Tests
// ============================================================================

func TestWakeCommand_EncodeDecode(t *testing.T) {
	origin, _ := identity.NewAgentID()
	seen1, _ := identity.NewAgentID()
	seen2, _ := identity.NewAgentID()

	// Create a signature
	var sig [SignatureSize]byte
	for i := range sig {
		sig[i] = byte(255 - i)
	}

	original := &WakeCommand{
		OriginAgent: origin,
		CommandID:   87654321,
		Timestamp:   1703002345,
		Signature:   sig,
		SeenBy:      []identity.AgentID{seen1, seen2},
	}

	data := original.Encode()
	decoded, err := DecodeWakeCommand(data)
	if err != nil {
		t.Fatalf("DecodeWakeCommand() error = %v", err)
	}

	if !decoded.OriginAgent.Equal(original.OriginAgent) {
		t.Error("OriginAgent mismatch")
	}
	if decoded.CommandID != original.CommandID {
		t.Errorf("CommandID = %d, want %d", decoded.CommandID, original.CommandID)
	}
	if decoded.Timestamp != original.Timestamp {
		t.Errorf("Timestamp = %d, want %d", decoded.Timestamp, original.Timestamp)
	}
	if decoded.Signature != original.Signature {
		t.Error("Signature mismatch")
	}
	if len(decoded.SeenBy) != len(original.SeenBy) {
		t.Errorf("SeenBy length = %d, want %d", len(decoded.SeenBy), len(original.SeenBy))
	}
}

func TestWakeCommand_WithZeroSignature(t *testing.T) {
	origin, _ := identity.NewAgentID()

	original := &WakeCommand{
		OriginAgent: origin,
		CommandID:   888,
		Timestamp:   1700000000,
		Signature:   [SignatureSize]byte{}, // All zeros
		SeenBy:      []identity.AgentID{origin},
	}

	data := original.Encode()
	decoded, err := DecodeWakeCommand(data)
	if err != nil {
		t.Fatalf("DecodeWakeCommand() error = %v", err)
	}

	if !decoded.IsZeroSignature() {
		t.Error("IsZeroSignature() = false, want true")
	}
}

func TestWakeCommand_EmptySeenBy(t *testing.T) {
	origin, _ := identity.NewAgentID()

	original := &WakeCommand{
		OriginAgent: origin,
		CommandID:   1,
		Timestamp:   1700000000,
		Signature:   [SignatureSize]byte{},
		SeenBy:      []identity.AgentID{},
	}

	data := original.Encode()
	decoded, err := DecodeWakeCommand(data)
	if err != nil {
		t.Fatalf("DecodeWakeCommand() error = %v", err)
	}

	if len(decoded.SeenBy) != 0 {
		t.Errorf("SeenBy should be empty, got %d", len(decoded.SeenBy))
	}
}

func TestWakeCommand_SignableBytes(t *testing.T) {
	origin, _ := identity.NewAgentID()
	seen1, _ := identity.NewAgentID()

	cmd := &WakeCommand{
		OriginAgent: origin,
		CommandID:   54321,
		Timestamp:   8888888,
		Signature:   [SignatureSize]byte{0xDE, 0xAD},
		SeenBy:      []identity.AgentID{seen1},
	}

	signable := cmd.SignableBytes()

	// Should be exactly 32 bytes: 16 (AgentID) + 8 (CommandID) + 8 (Timestamp)
	if len(signable) != 32 {
		t.Errorf("SignableBytes length = %d, want 32", len(signable))
	}

	// Verify consistency - same fields produce same signable bytes
	cmd2 := &WakeCommand{
		OriginAgent: origin,
		CommandID:   54321,
		Timestamp:   8888888,
		Signature:   [SignatureSize]byte{0xBE, 0xEF}, // Different signature
		SeenBy:      []identity.AgentID{},            // Different SeenBy
	}

	signable2 := cmd2.SignableBytes()
	if !bytes.Equal(signable, signable2) {
		t.Error("SignableBytes should be identical regardless of Signature/SeenBy")
	}
}

func TestWakeCommand_IsZeroSignature(t *testing.T) {
	cmd := &WakeCommand{}

	// Zero signature
	if !cmd.IsZeroSignature() {
		t.Error("IsZeroSignature() = false for zero signature")
	}

	// Non-zero signature
	cmd.Signature[0] = 1
	if cmd.IsZeroSignature() {
		t.Error("IsZeroSignature() = true for non-zero signature")
	}

	// Middle byte non-zero
	cmd.Signature[0] = 0
	cmd.Signature[32] = 0x42
	if cmd.IsZeroSignature() {
		t.Error("IsZeroSignature() = true when middle byte is non-zero")
	}
}

func TestDecodeWakeCommand_TooShort(t *testing.T) {
	// Minimum size is 16 + 8 + 8 + 64 + 1 = 97 bytes
	_, err := DecodeWakeCommand(make([]byte, 96))
	if err == nil {
		t.Error("DecodeWakeCommand() should fail with short data")
	}
}

func TestEncodeDecodeAgentPrefix(t *testing.T) {
	agentID, _ := identity.NewAgentID()

	encoded := EncodeAgentPrefix(agentID)
	if len(encoded) != identity.IDSize {
		t.Fatalf("EncodeAgentPrefix() length = %d, want %d", len(encoded), identity.IDSize)
	}

	decoded := DecodeAgentPrefix(encoded)
	if decoded != agentID {
		t.Errorf("DecodeAgentPrefix() = %s, want %s", decoded.String(), agentID.String())
	}
}

func TestDecodeAgentPrefix_TooShort(t *testing.T) {
	decoded := DecodeAgentPrefix(make([]byte, 8))
	if decoded != (identity.AgentID{}) {
		t.Error("DecodeAgentPrefix() should return ZeroID for short input")
	}
}

func TestRouteAdvertise_WithAgentPresenceRoute(t *testing.T) {
	origin, _ := identity.NewAgentID()
	agentTarget, _ := identity.NewAgentID()
	path1, _ := identity.NewAgentID()

	original := &RouteAdvertise{
		OriginAgent: origin,
		Sequence:    99,
		Routes: []Route{
			{
				AddressFamily: AddrFamilyIPv4,
				PrefixLength:  24,
				Prefix:        []byte{10, 0, 1, 0},
				Metric:        1,
			},
			{
				AddressFamily: AddrFamilyAgent,
				PrefixLength:  0,
				Prefix:        EncodeAgentPrefix(agentTarget),
				Metric:        0,
			},
		},
		Path:   []identity.AgentID{path1},
		SeenBy: []identity.AgentID{origin},
	}

	data := original.Encode()
	decoded, err := DecodeRouteAdvertise(data)
	if err != nil {
		t.Fatalf("DecodeRouteAdvertise() error = %v", err)
	}

	if len(decoded.Routes) != 2 {
		t.Fatalf("Routes length = %d, want 2", len(decoded.Routes))
	}

	// Verify CIDR route
	if decoded.Routes[0].AddressFamily != AddrFamilyIPv4 {
		t.Errorf("Route[0].AddressFamily = %d, want %d", decoded.Routes[0].AddressFamily, AddrFamilyIPv4)
	}
	if decoded.Routes[0].Metric != 1 {
		t.Errorf("Route[0].Metric = %d, want 1", decoded.Routes[0].Metric)
	}

	// Verify agent presence route
	if decoded.Routes[1].AddressFamily != AddrFamilyAgent {
		t.Errorf("Route[1].AddressFamily = %d, want %d", decoded.Routes[1].AddressFamily, AddrFamilyAgent)
	}
	if decoded.Routes[1].Metric != 0 {
		t.Errorf("Route[1].Metric = %d, want 0", decoded.Routes[1].Metric)
	}

	decodedAgent := DecodeAgentPrefix(decoded.Routes[1].Prefix)
	if decodedAgent != agentTarget {
		t.Errorf("Decoded agent ID = %s, want %s", decodedAgent.String(), agentTarget.String())
	}
}

// TestRouteAdvertise_CommentsRoundTrip covers the new optional Comments
// trailing block: parallel slice survives encode/decode, nil decodes back
// to nil, and a mismatched count is silently dropped (defensive).
func TestRouteAdvertise_CommentsRoundTrip(t *testing.T) {
	origin, _ := identity.NewAgentID()
	makeAdv := func(comments []string) *RouteAdvertise {
		return &RouteAdvertise{
			OriginAgent: origin,
			Sequence:    7,
			Routes: []Route{
				{AddressFamily: AddrFamilyIPv4, PrefixLength: 8, Prefix: []byte{10, 0, 0, 0}, Metric: 1},
				{AddressFamily: AddrFamilyIPv4, PrefixLength: 16, Prefix: []byte{192, 168, 0, 0}, Metric: 2},
			},
			Path:     []identity.AgentID{origin},
			SeenBy:   []identity.AgentID{origin},
			Comments: comments,
		}
	}

	t.Run("with_comments", func(t *testing.T) {
		adv := makeAdv([]string{"office LAN", "internal"})
		decoded, err := DecodeRouteAdvertise(adv.Encode())
		if err != nil {
			t.Fatalf("DecodeRouteAdvertise: %v", err)
		}
		if got, want := decoded.Comments, adv.Comments; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
			t.Errorf("Comments = %v, want %v", got, want)
		}
	})

	t.Run("nil_comments", func(t *testing.T) {
		decoded, err := DecodeRouteAdvertise(makeAdv(nil).Encode())
		if err != nil {
			t.Fatalf("DecodeRouteAdvertise: %v", err)
		}
		if decoded.Comments != nil {
			t.Errorf("Comments = %v, want nil", decoded.Comments)
		}
	})

	t.Run("empty_string_comments_preserved", func(t *testing.T) {
		// Encoder emits the trailing block whenever Comments is non-nil,
		// so a slice of empty strings round-trips as a slice of empty
		// strings, not nil. This matters when one of N routes has a
		// comment and the rest do not.
		adv := makeAdv([]string{"", "set"})
		decoded, err := DecodeRouteAdvertise(adv.Encode())
		if err != nil {
			t.Fatalf("DecodeRouteAdvertise: %v", err)
		}
		if len(decoded.Comments) != 2 || decoded.Comments[0] != "" || decoded.Comments[1] != "set" {
			t.Errorf("Comments = %v, want [\"\", \"set\"]", decoded.Comments)
		}
	})
}

// TestRouteAdvertise_OldReceiverIgnoresComments simulates an older agent
// by truncating the encoded message back to the pre-Comments wire shape,
// which is the same as encoding with Comments=nil. The decoded message
// must still parse cleanly with no error.
func TestRouteAdvertise_OldReceiverIgnoresComments(t *testing.T) {
	origin, _ := identity.NewAgentID()
	withComments := &RouteAdvertise{
		OriginAgent: origin,
		Sequence:    7,
		Routes: []Route{
			{AddressFamily: AddrFamilyIPv4, PrefixLength: 8, Prefix: []byte{10, 0, 0, 0}, Metric: 1},
		},
		Path:     []identity.AgentID{origin},
		SeenBy:   []identity.AgentID{origin},
		Comments: []string{"set"},
	}
	withoutComments := *withComments
	withoutComments.Comments = nil

	encoded := withComments.Encode()
	encodedNoComments := withoutComments.Encode()
	if len(encoded) <= len(encodedNoComments) {
		t.Fatalf("expected new encoding to be longer than old (got %d vs %d)", len(encoded), len(encodedNoComments))
	}
	// The first len(encodedNoComments) bytes must be byte-equal: the old
	// shape is a strict prefix of the new shape, which is what makes
	// older agents able to ignore the trailing block.
	for i := range encodedNoComments {
		if encoded[i] != encodedNoComments[i] {
			t.Fatalf("byte %d differs: new=%02x old=%02x - Comments block is not a pure trailing addition", i, encoded[i], encodedNoComments[i])
		}
	}
	// Truncate to the old length and decode: simulates an older receiver
	// that doesn't know about the Comments block. Must still succeed.
	truncated := encoded[:len(encodedNoComments)]
	decoded, err := DecodeRouteAdvertise(truncated)
	if err != nil {
		t.Fatalf("DecodeRouteAdvertise(truncated) error = %v", err)
	}
	if len(decoded.Routes) != 1 || decoded.Comments != nil {
		t.Errorf("decoded = %+v, want 1 route and nil Comments", decoded)
	}
}

// TestRouteAdvertise_MismatchedCommentCount confirms a malformed sender
// that emits len(Comments) != len(Routes) is silently dropped on
// receive (Comments nil); the route advertisement itself is unaffected.
func TestRouteAdvertise_MismatchedCommentCount(t *testing.T) {
	origin, _ := identity.NewAgentID()
	adv := &RouteAdvertise{
		OriginAgent: origin,
		Sequence:    7,
		Routes: []Route{
			{AddressFamily: AddrFamilyIPv4, PrefixLength: 8, Prefix: []byte{10, 0, 0, 0}, Metric: 1},
			{AddressFamily: AddrFamilyIPv4, PrefixLength: 16, Prefix: []byte{192, 168, 0, 0}, Metric: 2},
		},
		Path:     []identity.AgentID{origin},
		SeenBy:   []identity.AgentID{origin},
		Comments: []string{"only one"}, // len 1, but 2 routes
	}
	decoded, err := DecodeRouteAdvertise(adv.Encode())
	if err != nil {
		t.Fatalf("DecodeRouteAdvertise: %v", err)
	}
	if decoded.Comments != nil {
		t.Errorf("Comments = %v, want nil (mismatched count must be dropped)", decoded.Comments)
	}
	if len(decoded.Routes) != 2 {
		t.Errorf("Routes corrupted: len=%d, want 2", len(decoded.Routes))
	}
}

// A string longer than the 1-byte length prefix can describe must not produce
// a frame the peer cannot parse. Display name is the first string in
// PEER_HELLO, so a wrapped length there desynchronises the whole handshake and
// the connection is dropped before it is ever established.
func TestPeerHello_OverlongDisplayNameStaysDecodable(t *testing.T) {
	agentID := identity.AgentID{1, 2, 3}
	for _, size := range []int{MaxStringLen, MaxStringLen + 1, 300, 1000} {
		hello := &PeerHello{
			Version:     ProtocolVersion,
			AgentID:     agentID,
			Timestamp:   1234567890,
			DisplayName: strings.Repeat("a", size),
			// A capability after the name only decodes correctly if the name
			// left the reader at the right offset.
			Capabilities: []string{"exit"},
		}

		decoded, err := DecodePeerHello(hello.Encode())
		if err != nil {
			t.Fatalf("size %d: DecodePeerHello: %v", size, err)
		}
		want := size
		if want > MaxStringLen {
			want = MaxStringLen
		}
		if len(decoded.DisplayName) != want {
			t.Errorf("size %d: DisplayName length = %d, want %d", size, len(decoded.DisplayName), want)
		}
		if decoded.AgentID != agentID {
			t.Errorf("size %d: AgentID corrupted", size)
		}
		if len(decoded.Capabilities) != 1 || decoded.Capabilities[0] != "exit" {
			t.Errorf("size %d: Capabilities = %v, want [exit]", size, decoded.Capabilities)
		}
	}
}

// Truncation must not split a multi-byte rune, or the clamped name is no
// longer valid UTF-8.
func TestWriteString_ClampsOnRuneBoundary(t *testing.T) {
	// 3 bytes per rune, so 255 bytes lands mid-rune at 85 runes exactly...
	// use a 2-byte rune to force a boundary that does not divide evenly.
	// \u00e4 is 2 bytes in UTF-8, so 255 bytes lands mid-rune. Written as an
	// escape to keep the source ASCII-only.
	name := strings.Repeat("\u00e4", 200) // 400 bytes
	hello := &PeerHello{Version: ProtocolVersion, DisplayName: name}

	decoded, err := DecodePeerHello(hello.Encode())
	if err != nil {
		t.Fatalf("DecodePeerHello: %v", err)
	}
	if !utf8.ValidString(decoded.DisplayName) {
		t.Errorf("clamped display name is not valid UTF-8: %q", decoded.DisplayName)
	}
	if len(decoded.DisplayName) > MaxStringLen {
		t.Errorf("clamped to %d bytes, want <= %d", len(decoded.DisplayName), MaxStringLen)
	}
}

// The route count rides in a single byte. An advertisement built with 256 or
// more routes would wrap that count and become undecodable, which drops the
// peer connection rather than degrading gracefully -- so the encoder must stay
// decodable for any count the caller hands it.
func TestRouteAdvertise_LargeRouteCountStaysDecodable(t *testing.T) {
	origin := identity.AgentID{9}
	for _, n := range []int{254, 255} {
		adv := &RouteAdvertise{
			OriginAgent: origin,
			Sequence:    1,
			Path:        []identity.AgentID{origin},
			SeenBy:      []identity.AgentID{origin},
		}
		for i := 0; i < n; i++ {
			adv.Routes = append(adv.Routes, Route{
				AddressFamily: AddrFamilyIPv4,
				PrefixLength:  24,
				Prefix:        []byte{10, byte(i / 256), byte(i % 256), 0},
				Metric:        1,
			})
		}

		decoded, err := DecodeRouteAdvertise(adv.Encode())
		if err != nil {
			t.Fatalf("n=%d: DecodeRouteAdvertise: %v", n, err)
		}
		if len(decoded.Routes) != n {
			t.Errorf("n=%d: decoded %d routes", n, len(decoded.Routes))
		}
	}
}

// QueuedState advances past an embedded SleepCommand by a hardcoded size. If
// that size disagrees with SleepCommand.Encode, everything after it -- the
// WakeCmd flag and payload -- decodes from the wrong offset.
func TestQueuedState_SleepCommandOffset(t *testing.T) {
	origin := identity.AgentID{4}
	for _, seenBy := range [][]identity.AgentID{nil, {origin}, {origin, {5}, {6}}} {
		sleepCmd := &SleepCommand{
			OriginAgent: origin,
			CommandID:   77,
			Timestamp:   1234,
			SeenBy:      seenBy,
		}
		for i := range sleepCmd.Signature {
			sleepCmd.Signature[i] = byte(i)
		}
		wakeCmd := &WakeCommand{
			OriginAgent: origin,
			CommandID:   88,
			Timestamp:   5678,
		}

		q := &QueuedState{SleepCmd: sleepCmd, WakeCmd: wakeCmd}
		decoded, err := DecodeQueuedState(q.Encode())
		if err != nil {
			t.Fatalf("seenBy=%d: DecodeQueuedState: %v", len(seenBy), err)
		}
		if decoded.SleepCmd == nil {
			t.Fatalf("seenBy=%d: SleepCmd missing", len(seenBy))
		}
		if decoded.SleepCmd.CommandID != 77 {
			t.Errorf("seenBy=%d: SleepCmd.CommandID = %d, want 77", len(seenBy), decoded.SleepCmd.CommandID)
		}
		// The real assertion: WakeCmd only survives if the reader advanced
		// past the sleep command by exactly the right number of bytes.
		if decoded.WakeCmd == nil {
			t.Fatalf("seenBy=%d: WakeCmd lost -- sleep command offset is wrong", len(seenBy))
		}
		if decoded.WakeCmd.CommandID != 88 {
			t.Errorf("seenBy=%d: WakeCmd.CommandID = %d, want 88", len(seenBy), decoded.WakeCmd.CommandID)
		}
	}
}

// A corrupt embedded SleepCommand must fail the whole decode. The sleep blob
// is not length-prefixed, so swallowing the error would leave the reader
// offset stale and the WakeCmd flag would be read from inside the blob.
func TestQueuedState_CorruptSleepCommandFails(t *testing.T) {
	origin := identity.AgentID{4}
	q := &QueuedState{
		SleepCmd: &SleepCommand{OriginAgent: origin, CommandID: 77, Timestamp: 1234},
		WakeCmd:  &WakeCommand{OriginAgent: origin, CommandID: 88, Timestamp: 5678},
	}

	data := q.Encode()

	// Corrupt the sleep command's SeenBy count so DecodeSleepCommand fails:
	// 6 bytes of section counts, 1 byte sleep-present flag, then 96 bytes of
	// OriginAgent + CommandID + Timestamp + Signature before the count byte.
	const seenByCountOffset = 6 + 1 + 16 + 8 + 8 + SignatureSize
	data[seenByCountOffset] = 0xFF

	decoded, err := DecodeQueuedState(data)
	if err == nil {
		t.Fatalf("DecodeQueuedState() should fail on a corrupt sleep command, got %+v", decoded)
	}
	if !errors.Is(err, ErrInvalidFrame) {
		t.Errorf("error = %v, want ErrInvalidFrame", err)
	}
}
