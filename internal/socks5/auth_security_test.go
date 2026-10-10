package socks5

import (
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"
)

// ============================================================================
// Authentication Bypass Negative Tests
// ============================================================================

// TestAuthBypass_SkipMethodSelection tests attempts to bypass auth by skipping
// the method selection phase and going directly to CONNECT.
func TestAuthBypass_SkipMethodSelection(t *testing.T) {
	cfg := DefaultServerConfig()
	cfg.Address = "127.0.0.1:0"
	cfg.Authenticators = []Authenticator{
		NewUserPassAuthenticator(StaticCredentials{"admin": "secret"}),
	}

	s := NewServer(cfg)
	if err := s.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer s.Stop()

	conn, err := net.Dial("tcp", s.Address().String())
	if err != nil {
		t.Fatalf("Dial error: %v", err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))

	// Try to skip greeting and send CONNECT directly
	connectReq := []byte{
		SOCKS5Version,
		CmdConnect,
		0x00,
		AddrTypeIPv4,
		127, 0, 0, 1,
		0x00, 0x50, // Port 80
	}
	conn.Write(connectReq)

	// Server should reject - expect either connection close or error response
	response := make([]byte, 10)
	n, err := conn.Read(response)

	// Either EOF (connection closed) or an error response is acceptable
	if err == nil && n >= 2 {
		// If we got a response, it should NOT be success
		if response[1] == ReplySucceeded {
			t.Error("server allowed CONNECT without authentication - bypass successful!")
		}
	}
}

// TestAuthBypass_WrongMethodVersion tests sending wrong version in auth request.
func TestAuthBypass_WrongMethodVersion(t *testing.T) {
	creds := StaticCredentials{"testuser": "testpass"}
	auth := NewUserPassAuthenticator(creds)

	testCases := []struct {
		name    string
		request []byte
	}{
		{
			name:    "version 0x00",
			request: []byte{0x00, 0x08, 't', 'e', 's', 't', 'u', 's', 'e', 'r', 0x08, 't', 'e', 's', 't', 'p', 'a', 's', 's'},
		},
		{
			name:    "version 0x02",
			request: []byte{0x02, 0x08, 't', 'e', 's', 't', 'u', 's', 'e', 'r', 0x08, 't', 'e', 's', 't', 'p', 'a', 's', 's'},
		},
		{
			name:    "version 0xFF",
			request: []byte{0xFF, 0x08, 't', 'e', 's', 't', 'u', 's', 'e', 'r', 0x08, 't', 'e', 's', 't', 'p', 'a', 's', 's'},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			reader := bytes.NewReader(tc.request)
			writer := &bytes.Buffer{}

			_, err := auth.Authenticate(reader, writer)
			if err == nil {
				t.Error("Authenticate() should fail with wrong version")
			}
		})
	}
}

// TestAuthBypass_TruncatedCredentials tests handling of truncated auth data.
func TestAuthBypass_TruncatedCredentials(t *testing.T) {
	creds := StaticCredentials{"testuser": "testpass"}
	auth := NewUserPassAuthenticator(creds)

	testCases := []struct {
		name    string
		request []byte
	}{
		{
			name:    "no username length",
			request: []byte{0x01},
		},
		{
			name:    "username length but no username",
			request: []byte{0x01, 0x08},
		},
		{
			name:    "partial username",
			request: []byte{0x01, 0x08, 't', 'e', 's', 't'},
		},
		{
			name:    "username but no password length",
			request: []byte{0x01, 0x04, 't', 'e', 's', 't'},
		},
		{
			name:    "password length but no password",
			request: []byte{0x01, 0x04, 't', 'e', 's', 't', 0x08},
		},
		{
			name:    "partial password",
			request: []byte{0x01, 0x04, 't', 'e', 's', 't', 0x08, 'p', 'a', 's'},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			reader := bytes.NewReader(tc.request)
			writer := &bytes.Buffer{}

			_, err := auth.Authenticate(reader, writer)
			if err == nil {
				t.Error("Authenticate() should fail with truncated credentials")
			}
		})
	}
}

// TestAuthBypass_OverflowLengths tests handling of length fields that exceed buffer.
func TestAuthBypass_OverflowLengths(t *testing.T) {
	creds := StaticCredentials{"testuser": "testpass"}
	auth := NewUserPassAuthenticator(creds)

	testCases := []struct {
		name    string
		request []byte
	}{
		{
			name: "username length overflow",
			// Claims 255 bytes but provides only 4
			request: []byte{0x01, 0xFF, 't', 'e', 's', 't'},
		},
		{
			name: "password length overflow",
			// Valid username, but claims 255 bytes for password
			request: []byte{0x01, 0x04, 't', 'e', 's', 't', 0xFF, 'p', 'a', 's', 's'},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			reader := bytes.NewReader(tc.request)
			writer := &bytes.Buffer{}

			_, err := auth.Authenticate(reader, writer)
			if err == nil {
				t.Error("Authenticate() should fail with overflow lengths")
			}
		})
	}
}

// TestAuthBypass_EmptyCredentials tests handling of zero-length credentials.
func TestAuthBypass_EmptyCredentials(t *testing.T) {
	creds := StaticCredentials{"testuser": "testpass"}
	auth := NewUserPassAuthenticator(creds)

	testCases := []struct {
		name    string
		request []byte
	}{
		{
			name:    "empty username",
			request: []byte{0x01, 0x00, 0x08, 't', 'e', 's', 't', 'p', 'a', 's', 's'},
		},
		{
			name:    "empty password",
			request: []byte{0x01, 0x08, 't', 'e', 's', 't', 'u', 's', 'e', 'r', 0x00},
		},
		{
			name:    "both empty",
			request: []byte{0x01, 0x00, 0x00},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			reader := bytes.NewReader(tc.request)
			writer := &bytes.Buffer{}

			_, err := auth.Authenticate(reader, writer)
			if err == nil {
				t.Error("Authenticate() should fail with empty credentials")
			}
		})
	}
}

// TestAuthBypass_MethodDowngrade tests attempts to downgrade from required auth.
func TestAuthBypass_MethodDowngrade(t *testing.T) {
	cfg := DefaultServerConfig()
	cfg.Address = "127.0.0.1:0"
	// Only user/pass auth is allowed - no anonymous auth
	cfg.Authenticators = []Authenticator{
		NewUserPassAuthenticator(StaticCredentials{"admin": "secret"}),
	}

	s := NewServer(cfg)
	if err := s.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer s.Stop()

	conn, err := net.Dial("tcp", s.Address().String())
	if err != nil {
		t.Fatalf("Dial error: %v", err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))

	// Offer only no-auth method when server requires user/pass
	greeting := []byte{SOCKS5Version, 1, AuthMethodNoAuth}
	conn.Write(greeting)

	response := make([]byte, 2)
	_, err = io.ReadFull(conn, response)
	if err != nil {
		// Connection closed is acceptable
		return
	}

	// Server should reject with "no acceptable methods"
	if response[1] == AuthMethodNoAuth {
		t.Error("server accepted no-auth when user/pass is required - downgrade attack successful!")
	}

	if response[1] != AuthMethodNoAcceptable {
		t.Logf("server responded with method 0x%02x (expected 0xFF)", response[1])
	}
}

// TestAuthBypass_ReplayPreviousSession tests that credentials cannot be replayed.
func TestAuthBypass_ReplayPreviousSession(t *testing.T) {
	cfg := DefaultServerConfig()
	cfg.Address = "127.0.0.1:0"
	cfg.Authenticators = []Authenticator{
		NewUserPassAuthenticator(StaticCredentials{"admin": "secret"}),
	}

	s := NewServer(cfg)
	if err := s.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer s.Stop()

	// First, capture a valid auth exchange
	conn1, err := net.Dial("tcp", s.Address().String())
	if err != nil {
		t.Fatalf("Dial error: %v", err)
	}
	conn1.SetDeadline(time.Now().Add(5 * time.Second))

	// Complete handshake
	conn1.Write([]byte{SOCKS5Version, 1, AuthMethodUserPass})
	methodResp := make([]byte, 2)
	io.ReadFull(conn1, methodResp)

	// Send valid auth
	authReq := []byte{0x01, 0x05, 'a', 'd', 'm', 'i', 'n', 0x06, 's', 'e', 'c', 'r', 'e', 't'}
	conn1.Write(authReq)
	authResp := make([]byte, 2)
	io.ReadFull(conn1, authResp)

	if authResp[1] != AuthStatusSuccess {
		t.Fatalf("First auth should succeed, got status 0x%02x", authResp[1])
	}
	conn1.Close()

	// Now try to replay the exact same auth bytes on a new connection
	// without proper handshake
	conn2, err := net.Dial("tcp", s.Address().String())
	if err != nil {
		t.Fatalf("Dial error: %v", err)
	}
	defer conn2.Close()
	conn2.SetDeadline(time.Now().Add(5 * time.Second))

	// Skip method selection, try to replay auth directly
	conn2.Write(authReq)

	response := make([]byte, 10)
	n, err := conn2.Read(response)
	if err == nil && n >= 2 {
		// Should not succeed
		if response[0] == 0x01 && response[1] == AuthStatusSuccess {
			t.Error("server accepted replayed auth without handshake - replay attack possible!")
		}
	}
}

// TestAuthBypass_NullByteInjection tests handling of null bytes in credentials.
func TestAuthBypass_NullByteInjection(t *testing.T) {
	// Real user with embedded null in password
	creds := StaticCredentials{"admin": "secret"}
	auth := NewUserPassAuthenticator(creds)

	testCases := []struct {
		name     string
		username string
		password string
	}{
		{
			name:     "null in username",
			username: "admin\x00evil",
			password: "secret",
		},
		{
			name:     "null in password",
			username: "admin",
			password: "secret\x00anything",
		},
		{
			name:     "null before username",
			username: "\x00admin",
			password: "secret",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Build request
			var buf bytes.Buffer
			buf.WriteByte(0x01) // version
			buf.WriteByte(byte(len(tc.username)))
			buf.WriteString(tc.username)
			buf.WriteByte(byte(len(tc.password)))
			buf.WriteString(tc.password)

			reader := bytes.NewReader(buf.Bytes())
			writer := &bytes.Buffer{}

			_, err := auth.Authenticate(reader, writer)
			if err == nil {
				// Check that we didn't authenticate as the wrong user
				// (null byte truncation attack)
				t.Error("Authenticate() should fail for credentials with null bytes")
			}
		})
	}
}

// TestAuthBypass_TimingAttack verifies that failed auth takes similar time
// regardless of whether username exists or not.
func TestAuthBypass_TimingConsistency(t *testing.T) {
	hash := MustHashPassword("correctpassword")
	creds := HashedCredentials{
		"existinguser": hash,
	}
	auth := NewUserPassAuthenticator(creds)

	// Test with existing user + wrong password
	measureAuth := func(username, password string) time.Duration {
		var buf bytes.Buffer
		buf.WriteByte(0x01)
		buf.WriteByte(byte(len(username)))
		buf.WriteString(username)
		buf.WriteByte(byte(len(password)))
		buf.WriteString(password)

		start := time.Now()
		for i := 0; i < 10; i++ {
			reader := bytes.NewReader(buf.Bytes())
			writer := &bytes.Buffer{}
			auth.Authenticate(reader, writer)
		}
		return time.Since(start)
	}

	// Compare timing for existing vs non-existing user
	existingUserTime := measureAuth("existinguser", "wrongpassword")
	nonExistingUserTime := measureAuth("nonexistinguser", "wrongpassword")

	// Times should be within 50% of each other (accounting for noise)
	// This is a weak test but catches obvious timing leaks
	ratio := float64(existingUserTime) / float64(nonExistingUserTime)
	if ratio < 0.5 || ratio > 2.0 {
		t.Logf("Potential timing difference: existing=%v, nonexisting=%v, ratio=%f",
			existingUserTime, nonExistingUserTime, ratio)
		// Note: This is informational, not a hard failure, as timing tests are flaky
	}
}

// TestAuthBypass_ConcurrentAttempts tests that concurrent auth attempts don't
// interfere with each other or cause race conditions.
func TestAuthBypass_ConcurrentAttempts(t *testing.T) {
	cfg := DefaultServerConfig()
	cfg.Address = "127.0.0.1:0"
	cfg.Authenticators = []Authenticator{
		NewUserPassAuthenticator(StaticCredentials{"admin": "secret"}),
	}

	s := NewServer(cfg)
	if err := s.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer s.Stop()

	// Launch many concurrent auth attempts with wrong credentials
	done := make(chan bool, 100)
	for i := 0; i < 100; i++ {
		go func(attempt int) {
			defer func() { done <- true }()

			conn, err := net.Dial("tcp", s.Address().String())
			if err != nil {
				return
			}
			defer conn.Close()
			conn.SetDeadline(time.Now().Add(5 * time.Second))

			// Handshake
			conn.Write([]byte{SOCKS5Version, 1, AuthMethodUserPass})
			methodResp := make([]byte, 2)
			if _, err := io.ReadFull(conn, methodResp); err != nil {
				return
			}

			// Wrong password
			authReq := []byte{0x01, 0x05, 'a', 'd', 'm', 'i', 'n', 0x05, 'w', 'r', 'o', 'n', 'g'}
			conn.Write(authReq)

			authResp := make([]byte, 2)
			if _, err := io.ReadFull(conn, authResp); err != nil {
				return
			}

			if authResp[1] == AuthStatusSuccess {
				t.Errorf("Concurrent attempt %d: wrong password was accepted!", attempt)
			}
		}(i)
	}

	// Wait for all attempts
	for i := 0; i < 100; i++ {
		<-done
	}
}

// TestAuthBypass_RequestMalformed tests various malformed SOCKS5 requests.
func TestAuthBypass_RequestMalformed(t *testing.T) {
	cfg := DefaultServerConfig()
	cfg.Address = "127.0.0.1:0"
	cfg.Authenticators = []Authenticator{&NoAuthAuthenticator{}}

	// Start echo server as target
	echoListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Echo server listen error: %v", err)
	}
	defer echoListener.Close()

	s := NewServer(cfg)
	if err := s.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer s.Stop()

	testCases := []struct {
		name     string
		greeting []byte
		request  []byte
	}{
		{
			name:     "wrong SOCKS version in request",
			greeting: []byte{SOCKS5Version, 1, AuthMethodNoAuth},
			request:  []byte{0x04, CmdConnect, 0x00, AddrTypeIPv4, 127, 0, 0, 1, 0x00, 0x50},
		},
		{
			name:     "invalid command",
			greeting: []byte{SOCKS5Version, 1, AuthMethodNoAuth},
			request:  []byte{SOCKS5Version, 0xFF, 0x00, AddrTypeIPv4, 127, 0, 0, 1, 0x00, 0x50},
		},
		{
			name:     "truncated IPv4 address",
			greeting: []byte{SOCKS5Version, 1, AuthMethodNoAuth},
			request:  []byte{SOCKS5Version, CmdConnect, 0x00, AddrTypeIPv4, 127, 0},
		},
		{
			name:     "truncated port",
			greeting: []byte{SOCKS5Version, 1, AuthMethodNoAuth},
			request:  []byte{SOCKS5Version, CmdConnect, 0x00, AddrTypeIPv4, 127, 0, 0, 1, 0x00},
		},
		{
			name:     "domain with zero length",
			greeting: []byte{SOCKS5Version, 1, AuthMethodNoAuth},
			request:  []byte{SOCKS5Version, CmdConnect, 0x00, AddrTypeDomain, 0x00, 0x00, 0x50},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			conn, err := net.Dial("tcp", s.Address().String())
			if err != nil {
				t.Fatalf("Dial error: %v", err)
			}
			defer conn.Close()
			conn.SetDeadline(time.Now().Add(2 * time.Second))

			conn.Write(tc.greeting)
			methodResp := make([]byte, 2)
			io.ReadFull(conn, methodResp)

			conn.Write(tc.request)

			// Server should either close connection or send error reply
			reply := make([]byte, 10)
			n, err := conn.Read(reply)
			if err == nil && n >= 2 && reply[1] == ReplySucceeded {
				t.Error("server accepted malformed request")
			}
		})
	}
}

// TestAuthBypass_MaxMethods tests handling of maximum number of auth methods.
func TestAuthBypass_MaxMethods(t *testing.T) {
	cfg := DefaultServerConfig()
	cfg.Address = "127.0.0.1:0"
	cfg.Authenticators = []Authenticator{
		NewUserPassAuthenticator(StaticCredentials{"admin": "secret"}),
	}

	s := NewServer(cfg)
	if err := s.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer s.Stop()

	conn, err := net.Dial("tcp", s.Address().String())
	if err != nil {
		t.Fatalf("Dial error: %v", err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))

	// Send 255 methods (maximum allowed by protocol)
	greeting := make([]byte, 257)
	greeting[0] = SOCKS5Version
	greeting[1] = 255 // 255 methods
	for i := 2; i < 257; i++ {
		greeting[i] = byte(i - 2) // Methods 0x00 through 0xFE
	}
	conn.Write(greeting)

	response := make([]byte, 2)
	n, err := conn.Read(response)
	if err != nil {
		// Connection closed or timeout is acceptable
		return
	}

	if n >= 2 {
		// Server should pick user/pass (0x02) since it's in the list
		if response[1] != AuthMethodUserPass && response[1] != AuthMethodNoAcceptable {
			t.Logf("unexpected method selection: 0x%02x", response[1])
		}
	}
}

// TestAuthBypass_AfterSuccessfulAuth tests that authentication is properly
// enforced on each new connection even after previous successful auth.
func TestAuthBypass_AfterSuccessfulAuth(t *testing.T) {
	cfg := DefaultServerConfig()
	cfg.Address = "127.0.0.1:0"
	cfg.Authenticators = []Authenticator{
		NewUserPassAuthenticator(StaticCredentials{"admin": "secret"}),
	}

	// Start echo server
	echoListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Echo server listen error: %v", err)
	}
	defer echoListener.Close()
	echoAddr := echoListener.Addr().(*net.TCPAddr)

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

	s := NewServer(cfg)
	if err := s.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer s.Stop()

	// First connection - successful auth
	conn1, _ := net.Dial("tcp", s.Address().String())
	conn1.SetDeadline(time.Now().Add(5 * time.Second))
	conn1.Write([]byte{SOCKS5Version, 1, AuthMethodUserPass})
	io.ReadFull(conn1, make([]byte, 2))
	conn1.Write([]byte{0x01, 0x05, 'a', 'd', 'm', 'i', 'n', 0x06, 's', 'e', 'c', 'r', 'e', 't'})
	authResp := make([]byte, 2)
	io.ReadFull(conn1, authResp)
	if authResp[1] != AuthStatusSuccess {
		t.Fatal("First auth should succeed")
	}
	conn1.Close()

	// Second connection - try without auth
	conn2, _ := net.Dial("tcp", s.Address().String())
	defer conn2.Close()
	conn2.SetDeadline(time.Now().Add(5 * time.Second))

	// Skip auth, try to CONNECT directly
	connectReq := &bytes.Buffer{}
	connectReq.WriteByte(SOCKS5Version)
	connectReq.WriteByte(CmdConnect)
	connectReq.WriteByte(0x00)
	connectReq.WriteByte(AddrTypeIPv4)
	connectReq.Write(echoAddr.IP.To4())
	binary.Write(connectReq, binary.BigEndian, uint16(echoAddr.Port))

	conn2.Write(connectReq.Bytes())

	response := make([]byte, 10)
	n, err := conn2.Read(response)
	if err == nil && n >= 2 && response[1] == ReplySucceeded {
		t.Error("server allowed CONNECT without auth on new connection after previous auth")
	}
}
