package socks5

import (
	"context"
	"net"
	"testing"
)

func TestParseUDPHeader_IPv4(t *testing.T) {
	// Build a valid SOCKS5 UDP header for IPv4
	// RSV(2) + FRAG(1) + ATYP(1) + IPv4(4) + PORT(2) + DATA
	data := []byte{
		0x00, 0x00, // RSV
		0x00,       // FRAG (no fragmentation)
		0x01,       // ATYP (IPv4)
		8, 8, 8, 8, // IPv4 address
		0x00, 0x35, // Port 53 (DNS)
		'h', 'e', 'l', 'l', 'o', // Payload
	}

	header, payload, err := ParseUDPHeader(data)
	if err != nil {
		t.Fatalf("ParseUDPHeader error: %v", err)
	}

	if header.Frag != 0 {
		t.Errorf("Frag = %d, want 0", header.Frag)
	}
	if header.AddrType != AddrTypeIPv4 {
		t.Errorf("AddrType = %d, want %d", header.AddrType, AddrTypeIPv4)
	}
	if !header.Address.Equal(net.IPv4(8, 8, 8, 8)) {
		t.Errorf("Address = %v, want 8.8.8.8", header.Address)
	}
	if header.Port != 53 {
		t.Errorf("Port = %d, want 53", header.Port)
	}
	if string(payload) != "hello" {
		t.Errorf("Payload = %q, want %q", payload, "hello")
	}
}

func TestParseUDPHeader_IPv6(t *testing.T) {
	// RSV(2) + FRAG(1) + ATYP(1) + IPv6(16) + PORT(2) + DATA
	data := []byte{
		0x00, 0x00, // RSV
		0x00, // FRAG
		0x04, // ATYP (IPv6)
		// IPv6 address (2001:4860:4860::8888)
		0x20, 0x01, 0x48, 0x60, 0x48, 0x60, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x88, 0x88,
		0x01, 0xBB, // Port 443
		'd', 'a', 't', 'a',
	}

	header, payload, err := ParseUDPHeader(data)
	if err != nil {
		t.Fatalf("ParseUDPHeader error: %v", err)
	}

	if header.AddrType != AddrTypeIPv6 {
		t.Errorf("AddrType = %d, want %d", header.AddrType, AddrTypeIPv6)
	}
	if header.Port != 443 {
		t.Errorf("Port = %d, want 443", header.Port)
	}
	if string(payload) != "data" {
		t.Errorf("Payload = %q, want %q", payload, "data")
	}
}

func TestParseUDPHeader_Domain(t *testing.T) {
	// RSV(2) + FRAG(1) + ATYP(1) + LEN(1) + DOMAIN + PORT(2) + DATA
	domain := "example.com"
	data := []byte{
		0x00, 0x00, // RSV
		0x00,              // FRAG
		0x03,              // ATYP (Domain)
		byte(len(domain)), // Domain length
	}
	data = append(data, []byte(domain)...)
	data = append(data, 0x00, 0x50) // Port 80
	data = append(data, []byte("test")...)

	header, payload, err := ParseUDPHeader(data)
	if err != nil {
		t.Fatalf("ParseUDPHeader error: %v", err)
	}

	if header.AddrType != AddrTypeDomain {
		t.Errorf("AddrType = %d, want %d", header.AddrType, AddrTypeDomain)
	}
	if header.Domain != domain {
		t.Errorf("Domain = %q, want %q", header.Domain, domain)
	}
	if header.Port != 80 {
		t.Errorf("Port = %d, want 80", header.Port)
	}
	if string(payload) != "test" {
		t.Errorf("Payload = %q, want %q", payload, "test")
	}
}

func TestParseUDPHeader_TooShort(t *testing.T) {
	data := []byte{0x00, 0x00, 0x00} // Only 3 bytes

	_, _, err := ParseUDPHeader(data)
	if err == nil {
		t.Error("Expected error for short data")
	}
}

func TestParseUDPHeader_Fragmented(t *testing.T) {
	data := []byte{
		0x00, 0x00, // RSV
		0x01,       // FRAG > 0 (fragmented)
		0x01,       // ATYP
		8, 8, 8, 8, // IPv4
		0x00, 0x35, // Port
	}

	_, _, err := ParseUDPHeader(data)
	if err != ErrFragmentedDatagram {
		t.Errorf("Error = %v, want ErrFragmentedDatagram", err)
	}
}

func TestBuildUDPHeader_IPv4(t *testing.T) {
	addr := net.IPv4(1, 2, 3, 4).To4()
	header := BuildUDPHeader(AddrTypeIPv4, addr, 1234)

	// Verify: RSV(2) + FRAG(1) + ATYP(1) + ADDR(4) + PORT(2) = 10 bytes
	if len(header) != 10 {
		t.Fatalf("Header length = %d, want 10", len(header))
	}

	// RSV should be 0
	if header[0] != 0 || header[1] != 0 {
		t.Errorf("RSV = [%d, %d], want [0, 0]", header[0], header[1])
	}

	// FRAG should be 0
	if header[2] != 0 {
		t.Errorf("FRAG = %d, want 0", header[2])
	}

	// ATYP
	if header[3] != AddrTypeIPv4 {
		t.Errorf("ATYP = %d, want %d", header[3], AddrTypeIPv4)
	}

	// Address
	if header[4] != 1 || header[5] != 2 || header[6] != 3 || header[7] != 4 {
		t.Errorf("Address = %v, want [1,2,3,4]", header[4:8])
	}

	// Port (big-endian)
	port := uint16(header[8])<<8 | uint16(header[9])
	if port != 1234 {
		t.Errorf("Port = %d, want 1234", port)
	}
}

func TestBuildUDPHeader_Domain(t *testing.T) {
	domain := "test.com"
	domainBytes := append([]byte{byte(len(domain))}, []byte(domain)...)
	header := BuildUDPHeader(AddrTypeDomain, domainBytes, 8080)

	// RSV(2) + FRAG(1) + ATYP(1) + LEN(1) + DOMAIN(8) + PORT(2) = 15 bytes
	expectedLen := 4 + len(domainBytes) + 2
	if len(header) != expectedLen {
		t.Fatalf("Header length = %d, want %d", len(header), expectedLen)
	}

	// ATYP
	if header[3] != AddrTypeDomain {
		t.Errorf("ATYP = %d, want %d", header[3], AddrTypeDomain)
	}
}

func TestParseUDPHeader_RoundTrip(t *testing.T) {
	// Build header, parse it back
	addr := net.IPv4(192, 168, 1, 1).To4()
	original := BuildUDPHeader(AddrTypeIPv4, addr, 5000)
	original = append(original, []byte("payload")...)

	header, payload, err := ParseUDPHeader(original)
	if err != nil {
		t.Fatalf("ParseUDPHeader error: %v", err)
	}

	if !header.Address.Equal(net.IPv4(192, 168, 1, 1)) {
		t.Errorf("Address mismatch: %v", header.Address)
	}
	if header.Port != 5000 {
		t.Errorf("Port = %d, want 5000", header.Port)
	}
	if string(payload) != "payload" {
		t.Errorf("Payload = %q, want %q", payload, "payload")
	}
}

// Mock UDP handler for testing
type mockUDPHandler struct {
	enabled      bool
	createError  error
	nextStreamID uint64
}

func (m *mockUDPHandler) CreateUDPAssociation(ctx context.Context, clientAddr *net.UDPAddr) (uint64, error) {
	if m.createError != nil {
		return 0, m.createError
	}
	m.nextStreamID++
	return m.nextStreamID, nil
}

func (m *mockUDPHandler) RelayUDPDatagram(streamID uint64, destAddr net.Addr, destPort uint16, addrType byte, rawAddr []byte, data []byte) error {
	return nil
}

func (m *mockUDPHandler) CloseUDPAssociation(streamID uint64) {
}

func (m *mockUDPHandler) IsUDPEnabled() bool {
	return m.enabled
}

func (m *mockUDPHandler) SetSOCKS5UDPAssociation(streamID uint64, assoc *UDPAssociation) {
	// No-op for tests
}

func TestUDPAssociation_NewAndClose(t *testing.T) {
	// Create a mock TCP connection
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	handler := &mockUDPHandler{enabled: true}
	assoc, err := NewUDPAssociation(server, handler, nil)
	if err != nil {
		t.Fatalf("NewUDPAssociation error: %v", err)
	}

	if assoc.IsClosed() {
		t.Error("New association should not be closed")
	}

	// Get local addr
	addr := assoc.LocalAddr()
	if addr == nil {
		t.Error("LocalAddr should not be nil")
	}

	// Set stream ID
	assoc.SetStreamID(123)
	if assoc.GetStreamID() != 123 {
		t.Errorf("StreamID = %d, want 123", assoc.GetStreamID())
	}

	// Close
	err = assoc.Close()
	if err != nil {
		t.Errorf("Close error: %v", err)
	}

	if !assoc.IsClosed() {
		t.Error("Association should be closed")
	}

	// Double close should be safe
	err = assoc.Close()
	if err != nil {
		t.Errorf("Double close error: %v", err)
	}
}

func TestUDPAssociation_Context(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	handler := &mockUDPHandler{enabled: true}
	assoc, err := NewUDPAssociation(server, handler, nil)
	if err != nil {
		t.Fatalf("NewUDPAssociation error: %v", err)
	}

	ctx := assoc.Context()
	select {
	case <-ctx.Done():
		t.Error("Context should not be done")
	default:
	}

	assoc.Close()

	select {
	case <-ctx.Done():
		// Expected
	default:
		t.Error("Context should be done after close")
	}
}

func TestHandler_UDPAssociate_Disabled(t *testing.T) {
	h := NewHandler(nil, nil)
	// Don't set UDP handler - should reject UDP ASSOCIATE

	server, client := net.Pipe()
	defer server.Close()

	// Send SOCKS5 handshake and UDP ASSOCIATE request in background
	go func() {
		// Greeting: version + 1 method (no auth)
		client.Write([]byte{0x05, 0x01, 0x00})

		// Read method selection response
		resp := make([]byte, 2)
		client.Read(resp)

		// Send UDP ASSOCIATE request
		// VER(1) + CMD(1) + RSV(1) + ATYP(1) + DST.ADDR(4) + DST.PORT(2)
		client.Write([]byte{
			0x05, 0x03, 0x00, // VER, CMD=UDP_ASSOCIATE, RSV
			0x01,       // ATYP (IPv4)
			0, 0, 0, 0, // 0.0.0.0
			0x00, 0x00, // Port 0
		})

		// Read response
		resp = make([]byte, 10)
		client.Read(resp)
		client.Close()
	}()

	err := h.Handle(server)
	if err != ErrUDPDisabled {
		t.Errorf("Error = %v, want ErrUDPDisabled", err)
	}
}
