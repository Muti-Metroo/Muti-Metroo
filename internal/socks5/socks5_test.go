package socks5

import (
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

// ============================================================================
// Authentication Tests
// ============================================================================

func TestNoAuthAuthenticator_Authenticate(t *testing.T) {
	auth := &NoAuthAuthenticator{}

	user, err := auth.Authenticate(nil, nil)
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if user != "" {
		t.Errorf("Authenticate() user = %q, want empty", user)
	}
}

func TestNoAuthAuthenticator_GetMethod(t *testing.T) {
	auth := &NoAuthAuthenticator{}
	if auth.GetMethod() != AuthMethodNoAuth {
		t.Errorf("GetMethod() = %d, want %d", auth.GetMethod(), AuthMethodNoAuth)
	}
}

func TestStaticCredentials_Valid(t *testing.T) {
	creds := StaticCredentials{
		"user1": "pass1",
		"user2": "pass2",
	}

	tests := []struct {
		username string
		password string
		want     bool
	}{
		{"user1", "pass1", true},
		{"user2", "pass2", true},
		{"user1", "wrong", false},
		{"unknown", "pass1", false},
		{"", "", false},
	}

	for _, tt := range tests {
		got := creds.Valid(tt.username, tt.password)
		if got != tt.want {
			t.Errorf("Valid(%q, %q) = %v, want %v", tt.username, tt.password, got, tt.want)
		}
	}
}

func TestHashedCredentials_Valid(t *testing.T) {
	// Create bcrypt hashes for testing
	hash1 := MustHashPassword("pass1")
	hash2 := MustHashPassword("pass2")

	creds := HashedCredentials{
		"user1": hash1,
		"user2": hash2,
	}

	tests := []struct {
		username string
		password string
		want     bool
	}{
		{"user1", "pass1", true},
		{"user2", "pass2", true},
		{"user1", "wrong", false},
		{"user2", "pass1", false}, // Wrong user/pass combo
		{"unknown", "pass1", false},
		{"", "", false},
	}

	for _, tt := range tests {
		got := creds.Valid(tt.username, tt.password)
		if got != tt.want {
			t.Errorf("HashedCredentials.Valid(%q, %q) = %v, want %v", tt.username, tt.password, got, tt.want)
		}
	}
}

func TestHashPassword(t *testing.T) {
	password := "testpassword123"

	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}

	if hash == "" {
		t.Fatal("HashPassword() returned empty hash")
	}

	// Verify it's a bcrypt hash (starts with $2)
	if hash[0] != '$' || hash[1] != '2' {
		t.Errorf("HashPassword() returned invalid bcrypt hash prefix: %s", hash[:4])
	}

	// Create credentials and verify
	creds := HashedCredentials{"testuser": hash}
	if !creds.Valid("testuser", password) {
		t.Error("HashedCredentials.Valid() returned false for correct password")
	}
	if creds.Valid("testuser", "wrongpassword") {
		t.Error("HashedCredentials.Valid() returned true for wrong password")
	}
}

func TestMustHashPassword(t *testing.T) {
	hash := MustHashPassword("testpass")
	if hash == "" {
		t.Fatal("MustHashPassword() returned empty hash")
	}
}

func TestUserPassAuthenticator_GetMethod(t *testing.T) {
	auth := NewUserPassAuthenticator(StaticCredentials{})
	if auth.GetMethod() != AuthMethodUserPass {
		t.Errorf("GetMethod() = %d, want %d", auth.GetMethod(), AuthMethodUserPass)
	}
}

func TestUserPassAuthenticator_Authenticate_Success(t *testing.T) {
	creds := StaticCredentials{"testuser": "testpass"}
	auth := NewUserPassAuthenticator(creds)

	// Build auth request: VER=0x01, ULEN, USERNAME, PLEN, PASSWORD
	request := []byte{
		0x01, // version
		0x08, // username length
		't', 'e', 's', 't', 'u', 's', 'e', 'r',
		0x08, // password length
		't', 'e', 's', 't', 'p', 'a', 's', 's',
	}

	reader := bytes.NewReader(request)
	writer := &bytes.Buffer{}

	user, err := auth.Authenticate(reader, writer)
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if user != "testuser" {
		t.Errorf("Authenticate() user = %q, want %q", user, "testuser")
	}

	// Check response
	if writer.Len() != 2 {
		t.Fatalf("Response length = %d, want 2", writer.Len())
	}
	response := writer.Bytes()
	if response[0] != 0x01 || response[1] != AuthStatusSuccess {
		t.Errorf("Response = %v, want [0x01, 0x00]", response)
	}
}

func TestUserPassAuthenticator_Authenticate_Failure(t *testing.T) {
	creds := StaticCredentials{"testuser": "testpass"}
	auth := NewUserPassAuthenticator(creds)

	// Wrong password
	request := []byte{
		0x01,
		0x08,
		't', 'e', 's', 't', 'u', 's', 'e', 'r',
		0x05,
		'w', 'r', 'o', 'n', 'g',
	}

	reader := bytes.NewReader(request)
	writer := &bytes.Buffer{}

	_, err := auth.Authenticate(reader, writer)
	if err == nil {
		t.Error("Authenticate() should fail with wrong password")
	}

	// Check failure response
	response := writer.Bytes()
	if len(response) < 2 || response[1] != AuthStatusFailure {
		t.Errorf("Response should indicate failure, got %v", response)
	}
}

func TestUserPassAuthenticator_Authenticate_InvalidVersion(t *testing.T) {
	auth := NewUserPassAuthenticator(StaticCredentials{})

	// Invalid version
	request := []byte{0x02, 0x04, 't', 'e', 's', 't'}
	reader := bytes.NewReader(request)
	writer := &bytes.Buffer{}

	_, err := auth.Authenticate(reader, writer)
	if err == nil {
		t.Error("Authenticate() should fail with invalid version")
	}
}

func TestCreateAuthenticators(t *testing.T) {
	hash := MustHashPassword("hashedpass")

	tests := []struct {
		name      string
		cfg       AuthConfig
		wantLen   int
		hasNoAuth bool
	}{
		{
			name:      "no auth only",
			cfg:       AuthConfig{Enabled: false, Required: false},
			wantLen:   1,
			hasNoAuth: true,
		},
		{
			name: "user/pass only (plaintext)",
			cfg: AuthConfig{
				Enabled:  true,
				Required: true,
				Users:    map[string]string{"user": "pass"},
			},
			wantLen:   1,
			hasNoAuth: false,
		},
		{
			name: "user/pass only (hashed)",
			cfg: AuthConfig{
				Enabled:     true,
				Required:    true,
				HashedUsers: map[string]string{"user": hash},
			},
			wantLen:   1,
			hasNoAuth: false,
		},
		{
			name: "hashed takes precedence over plaintext",
			cfg: AuthConfig{
				Enabled:     true,
				Required:    true,
				Users:       map[string]string{"user": "plainpass"},
				HashedUsers: map[string]string{"user": hash},
			},
			wantLen:   1,
			hasNoAuth: false,
		},
		{
			name: "both methods with hashed",
			cfg: AuthConfig{
				Enabled:     true,
				Required:    false,
				HashedUsers: map[string]string{"user": hash},
			},
			wantLen:   2,
			hasNoAuth: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			auths := CreateAuthenticators(tt.cfg)
			if len(auths) != tt.wantLen {
				t.Errorf("CreateAuthenticators() len = %d, want %d", len(auths), tt.wantLen)
			}

			foundNoAuth := false
			for _, a := range auths {
				if a.GetMethod() == AuthMethodNoAuth {
					foundNoAuth = true
				}
			}
			if foundNoAuth != tt.hasNoAuth {
				t.Errorf("Has NoAuth = %v, want %v", foundNoAuth, tt.hasNoAuth)
			}
		})
	}
}

// ============================================================================
// Handler Tests
// ============================================================================

func TestNewHandler(t *testing.T) {
	h := NewHandler(nil, nil)
	if h == nil {
		t.Fatal("NewHandler() returned nil")
	}
}

func TestRequest_AddrTypes(t *testing.T) {
	tests := []struct {
		name     string
		addrType byte
		addrData []byte
		port     uint16
		wantAddr string
	}{
		{
			name:     "IPv4",
			addrType: AddrTypeIPv4,
			addrData: []byte{127, 0, 0, 1},
			port:     8080,
			wantAddr: "127.0.0.1",
		},
		{
			name:     "IPv6",
			addrType: AddrTypeIPv6,
			addrData: []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1},
			port:     8080,
			wantAddr: "::1",
		},
		{
			name:     "Domain",
			addrType: AddrTypeDomain,
			addrData: []byte{0x09, 'l', 'o', 'c', 'a', 'l', 'h', 'o', 's', 't'},
			port:     80,
			wantAddr: "localhost",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Build request
			buf := &bytes.Buffer{}
			buf.WriteByte(SOCKS5Version)
			buf.WriteByte(CmdConnect)
			buf.WriteByte(0x00) // RSV
			buf.WriteByte(tt.addrType)
			buf.Write(tt.addrData)
			binary.Write(buf, binary.BigEndian, tt.port)

			h := NewHandler(nil, nil)
			req, err := h.readRequest(newMockConn(buf, nil))
			if err != nil {
				t.Fatalf("readRequest() error = %v", err)
			}

			if req.DestAddr != tt.wantAddr {
				t.Errorf("DestAddr = %q, want %q", req.DestAddr, tt.wantAddr)
			}
			if req.DestPort != tt.port {
				t.Errorf("DestPort = %d, want %d", req.DestPort, tt.port)
			}
		})
	}
}

func TestHandler_UnsupportedAddressType(t *testing.T) {
	buf := &bytes.Buffer{}
	buf.WriteByte(SOCKS5Version)
	buf.WriteByte(CmdConnect)
	buf.WriteByte(0x00)
	buf.WriteByte(0xFF) // Invalid address type
	buf.Write([]byte{127, 0, 0, 1})
	binary.Write(buf, binary.BigEndian, uint16(8080))

	writer := &bytes.Buffer{}
	h := NewHandler(nil, nil)
	_, err := h.readRequest(newMockConn(buf, writer))
	if err == nil {
		t.Error("readRequest() should fail for unsupported address type")
	}

	// Check reply was sent
	if writer.Len() > 0 {
		reply := writer.Bytes()
		if reply[1] != ReplyAddrNotSupported {
			t.Errorf("Reply = %d, want %d", reply[1], ReplyAddrNotSupported)
		}
	}
}

// ============================================================================
// Server Tests
// ============================================================================

func TestDefaultServerConfig(t *testing.T) {
	cfg := DefaultServerConfig()

	if cfg.Address != "127.0.0.1:1080" {
		t.Errorf("Address = %q, want %q", cfg.Address, "127.0.0.1:1080")
	}
	if cfg.MaxConnections != 1000 {
		t.Errorf("MaxConnections = %d, want 1000", cfg.MaxConnections)
	}
	if cfg.ConnectTimeout != 30*time.Second {
		t.Errorf("ConnectTimeout = %v, want 30s", cfg.ConnectTimeout)
	}
}

func TestNewServer(t *testing.T) {
	cfg := DefaultServerConfig()
	s := NewServer(cfg)

	if s == nil {
		t.Fatal("NewServer() returned nil")
	}
	if s.IsRunning() {
		t.Error("New server should not be running")
	}
}

func TestServer_StartStop(t *testing.T) {
	cfg := DefaultServerConfig()
	cfg.Address = "127.0.0.1:0" // Random port
	s := NewServer(cfg)

	err := s.Start()
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if !s.IsRunning() {
		t.Error("Server should be running after Start()")
	}

	addr := s.Address()
	if addr == nil {
		t.Error("Address() should return address after Start()")
	}

	// Double start should fail
	err = s.Start()
	if err == nil {
		t.Error("Double Start() should fail")
	}

	err = s.Stop()
	if err != nil {
		t.Errorf("Stop() error = %v", err)
	}

	if s.IsRunning() {
		t.Error("Server should not be running after Stop()")
	}

	// Double stop should be safe
	err = s.Stop()
	if err != nil {
		t.Errorf("Double Stop() error = %v", err)
	}
}

func TestServer_ConnectionCount(t *testing.T) {
	cfg := DefaultServerConfig()
	cfg.Address = "127.0.0.1:0"
	s := NewServer(cfg)

	err := s.Start()
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer s.Stop()

	if s.ConnectionCount() != 0 {
		t.Errorf("ConnectionCount() = %d, want 0", s.ConnectionCount())
	}
}

func TestServer_BasicConnect(t *testing.T) {
	// Start an echo server
	echoListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Echo server listen error: %v", err)
	}
	defer echoListener.Close()

	echoAddr := echoListener.Addr().String()
	go func() {
		for {
			conn, err := echoListener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				io.Copy(c, c)
			}(conn)
		}
	}()

	// Start SOCKS5 server
	cfg := DefaultServerConfig()
	cfg.Address = "127.0.0.1:0"
	s := NewServer(cfg)
	err = s.Start()
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer s.Stop()

	// Connect to SOCKS5 server
	conn, err := net.Dial("tcp", s.Address().String())
	if err != nil {
		t.Fatalf("Dial SOCKS5 error: %v", err)
	}
	defer conn.Close()

	// Send greeting (no auth)
	conn.Write([]byte{SOCKS5Version, 1, AuthMethodNoAuth})

	// Read method selection
	methodResp := make([]byte, 2)
	io.ReadFull(conn, methodResp)
	if methodResp[1] != AuthMethodNoAuth {
		t.Errorf("Method = %d, want %d", methodResp[1], AuthMethodNoAuth)
	}

	// Parse echo address
	echoHost, echoPortStr, _ := net.SplitHostPort(echoAddr)
	echoIP := net.ParseIP(echoHost)
	var echoPort uint16
	var n int
	n, _ = net.LookupPort("tcp", echoPortStr)
	echoPort = uint16(n)

	// Send CONNECT request
	connectReq := &bytes.Buffer{}
	connectReq.WriteByte(SOCKS5Version)
	connectReq.WriteByte(CmdConnect)
	connectReq.WriteByte(0x00)
	connectReq.WriteByte(AddrTypeIPv4)
	connectReq.Write(echoIP.To4())
	binary.Write(connectReq, binary.BigEndian, echoPort)
	conn.Write(connectReq.Bytes())

	// Read reply
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	reply := make([]byte, 10)
	_, err = io.ReadFull(conn, reply)
	if err != nil {
		t.Fatalf("Read reply error: %v", err)
	}

	if reply[1] != ReplySucceeded {
		t.Errorf("Reply = %d, want %d", reply[1], ReplySucceeded)
	}

	// Test echo
	testData := []byte("Hello, SOCKS5!")
	conn.Write(testData)

	response := make([]byte, len(testData))
	_, err = io.ReadFull(conn, response)
	if err != nil {
		t.Fatalf("Read echo error: %v", err)
	}

	if !bytes.Equal(response, testData) {
		t.Errorf("Echo response = %q, want %q", response, testData)
	}
}

func TestServer_MaxConnections(t *testing.T) {
	cfg := DefaultServerConfig()
	cfg.Address = "127.0.0.1:0"
	cfg.MaxConnections = 2
	s := NewServer(cfg)

	err := s.Start()
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer s.Stop()

	// Create connections
	var conns []net.Conn
	var mu sync.Mutex

	for i := 0; i < 5; i++ {
		conn, err := net.Dial("tcp", s.Address().String())
		if err != nil {
			continue
		}
		mu.Lock()
		conns = append(conns, conn)
		mu.Unlock()
	}

	defer func() {
		mu.Lock()
		for _, c := range conns {
			c.Close()
		}
		mu.Unlock()
	}()

	// Give server time to process
	time.Sleep(100 * time.Millisecond)

	// Should have limited connections
	if s.ConnectionCount() > int64(cfg.MaxConnections) {
		t.Errorf("ConnectionCount() = %d, exceeded max %d", s.ConnectionCount(), cfg.MaxConnections)
	}
}

func TestServerConfig_WithMethods(t *testing.T) {
	cfg := DefaultServerConfig()

	cfg = cfg.WithMaxConnections(500)
	if cfg.MaxConnections != 500 {
		t.Errorf("MaxConnections = %d, want 500", cfg.MaxConnections)
	}

	dialer := &DirectDialer{}
	cfg = cfg.WithDialer(dialer)
	if cfg.Dialer != dialer {
		t.Error("Dialer not set correctly")
	}

	auths := []Authenticator{&NoAuthAuthenticator{}}
	cfg = cfg.WithAuthenticators(auths...)
	if len(cfg.Authenticators) != 1 {
		t.Errorf("Authenticators len = %d, want 1", len(cfg.Authenticators))
	}
}

// ============================================================================
// Helper Types
// ============================================================================

// mockConn implements net.Conn for testing.
type mockConn struct {
	reader io.Reader
	writer io.Writer
}

func newMockConn(reader io.Reader, writer io.Writer) *mockConn {
	if writer == nil {
		writer = &bytes.Buffer{}
	}
	return &mockConn{reader: reader, writer: writer}
}

func (m *mockConn) Read(b []byte) (n int, err error) {
	return m.reader.Read(b)
}

func (m *mockConn) Write(b []byte) (n int, err error) {
	return m.writer.Write(b)
}

func (m *mockConn) Close() error                       { return nil }
func (m *mockConn) LocalAddr() net.Addr                { return &net.TCPAddr{} }
func (m *mockConn) RemoteAddr() net.Addr               { return &net.TCPAddr{} }
func (m *mockConn) SetDeadline(t time.Time) error      { return nil }
func (m *mockConn) SetReadDeadline(t time.Time) error  { return nil }
func (m *mockConn) SetWriteDeadline(t time.Time) error { return nil }
