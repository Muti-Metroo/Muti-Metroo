package socks5

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"nhooyr.io/websocket"
)

func TestNewWebSocketListener_RequiresTLSOrPlaintext(t *testing.T) {
	handler := NewHandler(nil, nil)

	// Should fail without TLS or plaintext
	_, err := NewWebSocketListener(WebSocketConfig{
		Address: "127.0.0.1:0",
	}, handler)
	if err == nil {
		t.Error("expected error without TLS or plaintext mode")
	}

	// Should succeed with plaintext
	_, err = NewWebSocketListener(WebSocketConfig{
		Address:   "127.0.0.1:0",
		PlainText: true,
	}, handler)
	if err != nil {
		t.Errorf("unexpected error with plaintext: %v", err)
	}
}

func TestNewWebSocketListener_DefaultPath(t *testing.T) {
	handler := NewHandler(nil, nil)
	l, err := NewWebSocketListener(WebSocketConfig{
		Address:   "127.0.0.1:0",
		PlainText: true,
	}, handler)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if l.cfg.Path != "/socks5" {
		t.Errorf("default path = %s, want /socks5", l.cfg.Path)
	}
}

func TestWebSocketListener_StartStop(t *testing.T) {
	handler := NewHandler(nil, nil)
	l, err := NewWebSocketListener(WebSocketConfig{
		Address:   "127.0.0.1:0",
		PlainText: true,
	}, handler)
	if err != nil {
		t.Fatalf("create listener: %v", err)
	}

	if err := l.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}

	if !l.IsRunning() {
		t.Error("listener should be running")
	}

	// Starting again should fail
	if err := l.Start(); err == nil {
		t.Error("expected error starting already running listener")
	}

	if err := l.Stop(); err != nil {
		t.Errorf("stop: %v", err)
	}

	if l.IsRunning() {
		t.Error("listener should not be running after stop")
	}
}

func TestWebSocketListener_SplashPage(t *testing.T) {
	handler := NewHandler(nil, nil)
	l, err := NewWebSocketListener(WebSocketConfig{
		Address:   "127.0.0.1:0",
		PlainText: true,
	}, handler)
	if err != nil {
		t.Fatalf("create listener: %v", err)
	}

	if err := l.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer l.Stop()

	// Request splash page
	resp, err := http.Get("http://" + l.Address() + "/")
	if err != nil {
		t.Fatalf("get splash page: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	contentType := resp.Header.Get("Content-Type")
	if !strings.Contains(contentType, "text/html") {
		t.Errorf("content-type = %s, want text/html", contentType)
	}

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Muti Metroo") {
		t.Error("splash page should contain 'Muti Metroo'")
	}
}

func TestWebSocketListener_404ForUnknownPaths(t *testing.T) {
	handler := NewHandler(nil, nil)
	l, err := NewWebSocketListener(WebSocketConfig{
		Address:   "127.0.0.1:0",
		PlainText: true,
	}, handler)
	if err != nil {
		t.Fatalf("create listener: %v", err)
	}

	if err := l.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer l.Stop()

	resp, err := http.Get("http://" + l.Address() + "/unknown")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestWebSocketListener_WebSocketUpgrade(t *testing.T) {
	handler := NewHandler(nil, nil)
	l, err := NewWebSocketListener(WebSocketConfig{
		Address:   "127.0.0.1:0",
		Path:      "/socks5",
		PlainText: true,
	}, handler)
	if err != nil {
		t.Fatalf("create listener: %v", err)
	}

	if err := l.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer l.Stop()

	// Connect via WebSocket
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	wsURL := "ws://" + l.Address() + "/socks5"
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		Subprotocols: []string{"socks5"},
	})
	if err != nil {
		t.Fatalf("WebSocket dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	// websocket.Dial returns after the 101 Switching Protocols response is
	// read, but the server handler increments the tracker *after* Accept
	// returns. Poll briefly to close the inherent race between Dial returning
	// and the handler registering the connection.
	deadline := time.Now().Add(2 * time.Second)
	for l.ConnectionCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if count := l.ConnectionCount(); count != 1 {
		t.Errorf("connection count = %d, want 1", count)
	}
}

func TestWsConn_ReadWrite(t *testing.T) {
	// Create a test WebSocket server
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "")

		// Echo loop
		for {
			msgType, data, err := conn.Read(context.Background())
			if err != nil {
				return
			}
			if err := conn.Write(context.Background(), msgType, data); err != nil {
				return
			}
		}
	}))
	defer srv.Close()

	// Connect to the server
	ctx := context.Background()
	wsURL := "ws" + srv.URL[4:] // http -> ws
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	// Wrap the connection
	wc := newWsConn(conn)
	defer wc.Close()

	// Test write and read
	testData := []byte("hello websocket")
	n, err := wc.Write(testData)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if n != len(testData) {
		t.Errorf("wrote %d bytes, want %d", n, len(testData))
	}

	buf := make([]byte, len(testData))
	_, err = io.ReadFull(wc, buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	if string(buf) != string(testData) {
		t.Errorf("got %q, want %q", buf, testData)
	}
}

func TestWsConn_SetDeadline(t *testing.T) {
	// Create a test WebSocket server
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "")
		// Just wait
		<-r.Context().Done()
	}))
	defer srv.Close()

	// Connect to the server
	ctx := context.Background()
	wsURL := "ws" + srv.URL[4:]
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	wc := newWsConn(conn)
	defer wc.Close()

	// Test SetDeadline
	if err := wc.SetDeadline(time.Now().Add(time.Second)); err != nil {
		t.Errorf("SetDeadline: %v", err)
	}

	// Test SetReadDeadline
	if err := wc.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Errorf("SetReadDeadline: %v", err)
	}

	// Test SetWriteDeadline
	if err := wc.SetWriteDeadline(time.Now().Add(time.Second)); err != nil {
		t.Errorf("SetWriteDeadline: %v", err)
	}

	// Test clearing deadline
	if err := wc.SetDeadline(time.Time{}); err != nil {
		t.Errorf("SetDeadline(zero): %v", err)
	}
}

func TestWsConn_Addresses(t *testing.T) {
	// Create a test WebSocket server
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		conn.Close(websocket.StatusNormalClosure, "")
	}))
	defer srv.Close()

	ctx := context.Background()
	wsURL := "ws" + srv.URL[4:]
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	wc := newWsConn(conn)
	defer wc.Close()

	// LocalAddr and RemoteAddr should return nil
	if wc.LocalAddr() != nil {
		t.Error("LocalAddr should return nil")
	}
	if wc.RemoteAddr() != nil {
		t.Error("RemoteAddr should return nil")
	}
}

func TestServer_StartWebSocket(t *testing.T) {
	cfg := DefaultServerConfig()
	cfg.Address = "127.0.0.1:0"
	srv := NewServer(cfg)

	// Start TCP server first
	if err := srv.Start(); err != nil {
		t.Fatalf("start TCP: %v", err)
	}
	defer srv.Stop()

	// Start WebSocket listener
	wsCfg := WebSocketConfig{
		Address:   "127.0.0.1:0",
		PlainText: true,
	}
	if err := srv.StartWebSocket(wsCfg); err != nil {
		t.Fatalf("start WebSocket: %v", err)
	}

	// Check WebSocket address
	if addr := srv.WebSocketAddress(); addr == "" {
		t.Error("WebSocket address should not be empty")
	}

	// Starting again should fail
	if err := srv.StartWebSocket(wsCfg); err == nil {
		t.Error("expected error starting WebSocket again")
	}

	// Stop should stop both
	if err := srv.Stop(); err != nil {
		t.Errorf("stop: %v", err)
	}

	if addr := srv.WebSocketAddress(); addr != "" {
		t.Error("WebSocket address should be empty after stop")
	}
}

func TestServer_StopWebSocket(t *testing.T) {
	cfg := DefaultServerConfig()
	cfg.Address = "127.0.0.1:0"
	srv := NewServer(cfg)

	// StopWebSocket on non-running server should not error
	if err := srv.StopWebSocket(); err != nil {
		t.Errorf("StopWebSocket on nil: %v", err)
	}

	// Start and stop
	if err := srv.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer srv.Stop()

	wsCfg := WebSocketConfig{
		Address:   "127.0.0.1:0",
		PlainText: true,
	}
	if err := srv.StartWebSocket(wsCfg); err != nil {
		t.Fatalf("start WebSocket: %v", err)
	}

	if err := srv.StopWebSocket(); err != nil {
		t.Errorf("stop WebSocket: %v", err)
	}

	// Connection count should be 0
	if count := srv.WebSocketConnectionCount(); count != 0 {
		t.Errorf("connection count = %d, want 0", count)
	}
}

// Integration test for SOCKS5 over WebSocket
func TestWebSocketSOCKS5Integration(t *testing.T) {
	cfg := DefaultServerConfig()
	cfg.Address = "127.0.0.1:0"
	srv := NewServer(cfg)

	if err := srv.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer srv.Stop()

	wsCfg := WebSocketConfig{
		Address:   "127.0.0.1:0",
		Path:      "/socks5",
		PlainText: true,
	}
	if err := srv.StartWebSocket(wsCfg); err != nil {
		t.Fatalf("start WebSocket: %v", err)
	}

	// Connect via WebSocket
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	wsURL := "ws://" + srv.WebSocketAddress() + "/socks5"
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		Subprotocols: []string{"socks5"},
	})
	if err != nil {
		t.Fatalf("WebSocket dial: %v", err)
	}

	// Wrap as net.Conn
	wc := newWsConn(conn)
	defer wc.Close()

	// Send SOCKS5 greeting
	greeting := []byte{0x05, 0x01, 0x00} // Version 5, 1 method, no auth
	if _, err := wc.Write(greeting); err != nil {
		t.Fatalf("write greeting: %v", err)
	}

	// Read method selection response
	response := make([]byte, 2)
	if _, err := io.ReadFull(wc, response); err != nil {
		t.Fatalf("read response: %v", err)
	}

	if response[0] != 0x05 {
		t.Errorf("response version = %d, want 5", response[0])
	}
	if response[1] != 0x00 {
		t.Errorf("response method = %d, want 0 (no auth)", response[1])
	}
}

// Verify net.Conn interface
var _ net.Conn = (*wsConn)(nil)

// TestWebSocketSOCKS5Connect exercises a full CONNECT over the WebSocket
// listener, including the dial monitor path. Regression test: the peekConn
// wrapper used to hide wsConn's NoDeadlineMonitor opt-out, so dialWithMonitor
// polled the WebSocket with read deadlines and killed it before the reply.
func TestWebSocketSOCKS5Connect(t *testing.T) {
	echo, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("echo listen: %v", err)
	}
	defer echo.Close()
	go func() {
		for {
			c, err := echo.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_, _ = io.Copy(c, c)
			}(c)
		}
	}()

	cfg := DefaultServerConfig()
	cfg.Address = "127.0.0.1:0"
	srv := NewServer(cfg)

	if err := srv.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer srv.Stop()

	wsCfg := WebSocketConfig{
		Address:   "127.0.0.1:0",
		Path:      "/socks5",
		PlainText: true,
	}
	if err := srv.StartWebSocket(wsCfg); err != nil {
		t.Fatalf("start WebSocket: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	wsURL := "ws://" + srv.WebSocketAddress() + "/socks5"
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		Subprotocols: []string{"socks5"},
	})
	if err != nil {
		t.Fatalf("WebSocket dial: %v", err)
	}

	wc := newWsConn(conn)
	defer wc.Close()

	// SOCKS5 greeting (no auth)
	if _, err := wc.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		t.Fatalf("write greeting: %v", err)
	}
	methodResp := make([]byte, 2)
	if _, err := io.ReadFull(wc, methodResp); err != nil {
		t.Fatalf("read method response: %v", err)
	}

	// CONNECT to the echo server (IPv4)
	echoAddr := echo.Addr().(*net.TCPAddr)
	req := []byte{0x05, 0x01, 0x00, 0x01}
	req = append(req, echoAddr.IP.To4()...)
	req = append(req, byte(echoAddr.Port>>8), byte(echoAddr.Port))
	if _, err := wc.Write(req); err != nil {
		t.Fatalf("write CONNECT: %v", err)
	}

	reply := make([]byte, 10)
	if _, err := io.ReadFull(wc, reply); err != nil {
		t.Fatalf("read CONNECT reply: %v", err)
	}
	if reply[1] != 0x00 {
		t.Fatalf("CONNECT reply status = %d, want 0 (succeeded)", reply[1])
	}

	// Data relay round-trip
	msg := []byte("ping-through-ws")
	if _, err := wc.Write(msg); err != nil {
		t.Fatalf("write payload: %v", err)
	}
	buf := make([]byte, len(msg))
	if _, err := io.ReadFull(wc, buf); err != nil {
		t.Fatalf("read echo: %v", err)
	}
	if !bytes.Equal(buf, msg) {
		t.Errorf("echo = %q, want %q", buf, msg)
	}
}

// Test connTracker double remove safety
func TestConnTracker_DoubleRemove(t *testing.T) {
	tracker := newConnTracker[net.Conn]()

	// Create a mock connection
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	// Add and verify count
	tracker.add(client)
	if count := tracker.count(); count != 1 {
		t.Errorf("count after add = %d, want 1", count)
	}

	// Remove once
	tracker.remove(client)
	if count := tracker.count(); count != 0 {
		t.Errorf("count after first remove = %d, want 0", count)
	}

	// Remove again - should not go negative
	tracker.remove(client)
	if count := tracker.count(); count != 0 {
		t.Errorf("count after second remove = %d, want 0 (not negative)", count)
	}
}

// Test connTracker closeAll resets state
func TestConnTracker_CloseAllResetsState(t *testing.T) {
	tracker := newConnTracker[net.Conn]()

	// Create mock connections
	server1, client1 := net.Pipe()
	server2, client2 := net.Pipe()
	defer server1.Close()
	defer server2.Close()

	tracker.add(client1)
	tracker.add(client2)

	if count := tracker.count(); count != 2 {
		t.Errorf("count after adds = %d, want 2", count)
	}

	// Close all - should reset count to 0
	tracker.closeAll()

	if count := tracker.count(); count != 0 {
		t.Errorf("count after closeAll = %d, want 0", count)
	}

	// Remove after closeAll should not go negative
	tracker.remove(client1)
	if count := tracker.count(); count != 0 {
		t.Errorf("count after remove post-closeAll = %d, want 0", count)
	}
}

// Test WebSocket subprotocol validation
func TestWebSocketListener_SubprotocolValidation(t *testing.T) {
	handler := NewHandler(nil, nil)
	l, err := NewWebSocketListener(WebSocketConfig{
		Address:   "127.0.0.1:0",
		Path:      "/socks5",
		PlainText: true,
	}, handler)
	if err != nil {
		t.Fatalf("create listener: %v", err)
	}

	if err := l.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer l.Stop()

	// Connect without specifying subprotocol - should be rejected
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	wsURL := "ws://" + l.Address() + "/socks5"
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		// No subprotocol specified
	})
	if err != nil {
		// Connection might fail at dial or after
		return
	}

	// If we got a connection, it should be closed by server due to missing subprotocol
	// Try to read - should get an error
	_, _, readErr := conn.Read(ctx)
	if readErr == nil {
		t.Error("expected connection to be closed due to missing subprotocol")
	}
	conn.Close(websocket.StatusNormalClosure, "")
}

// Test OnError callback
func TestWebSocketListener_OnErrorCallback(t *testing.T) {
	handler := NewHandler(nil, nil)

	l, err := NewWebSocketListener(WebSocketConfig{
		Address:   "127.0.0.1:0",
		PlainText: true,
		OnError: func(err error) {
			// Callback registered for serve errors
		},
	}, handler)
	if err != nil {
		t.Fatalf("create listener: %v", err)
	}

	if err := l.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer l.Stop()

	// Verify the callback is set correctly
	if l.cfg.OnError == nil {
		t.Error("OnError callback should be set")
	}
}

// Test WebSocket Basic Auth - no credentials when required
func TestWebSocketListener_BasicAuth_NoCredentials(t *testing.T) {
	handler := NewHandler(nil, nil)

	// Create listener with authentication required
	creds := StaticCredentials{"testuser": "testpass"}
	l, err := NewWebSocketListener(WebSocketConfig{
		Address:     "127.0.0.1:0",
		Path:        "/socks5",
		PlainText:   true,
		Credentials: creds,
	}, handler)
	if err != nil {
		t.Fatalf("create listener: %v", err)
	}

	if err := l.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer l.Stop()

	// Try to connect without credentials
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	wsURL := "ws://" + l.Address() + "/socks5"
	_, resp, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		Subprotocols: []string{"socks5"},
	})

	// Should get 401 Unauthorized
	if err == nil {
		t.Error("expected error when connecting without credentials")
	}
	if resp != nil && resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status code = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
}

// Test WebSocket Basic Auth - wrong credentials
func TestWebSocketListener_BasicAuth_WrongCredentials(t *testing.T) {
	handler := NewHandler(nil, nil)

	creds := StaticCredentials{"testuser": "testpass"}
	l, err := NewWebSocketListener(WebSocketConfig{
		Address:     "127.0.0.1:0",
		Path:        "/socks5",
		PlainText:   true,
		Credentials: creds,
	}, handler)
	if err != nil {
		t.Fatalf("create listener: %v", err)
	}

	if err := l.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer l.Stop()

	// Try to connect with wrong credentials
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	wsURL := "ws://" + l.Address() + "/socks5"
	_, resp, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		Subprotocols: []string{"socks5"},
		HTTPHeader: http.Header{
			"Authorization": []string{"Basic " + base64Encode("testuser:wrongpass")},
		},
	})

	// Should get 401 Unauthorized
	if err == nil {
		t.Error("expected error when connecting with wrong credentials")
	}
	if resp != nil && resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status code = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
}

// Test WebSocket Basic Auth - correct credentials
func TestWebSocketListener_BasicAuth_CorrectCredentials(t *testing.T) {
	handler := NewHandler(nil, nil)

	creds := StaticCredentials{"testuser": "testpass"}
	l, err := NewWebSocketListener(WebSocketConfig{
		Address:     "127.0.0.1:0",
		Path:        "/socks5",
		PlainText:   true,
		Credentials: creds,
	}, handler)
	if err != nil {
		t.Fatalf("create listener: %v", err)
	}

	if err := l.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer l.Stop()

	// Connect with correct credentials
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	wsURL := "ws://" + l.Address() + "/socks5"
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		Subprotocols: []string{"socks5"},
		HTTPHeader: http.Header{
			"Authorization": []string{"Basic " + base64Encode("testuser:testpass")},
		},
	})
	if err != nil {
		t.Fatalf("dial with correct credentials: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	// Verify connection succeeded by checking subprotocol
	if conn.Subprotocol() != "socks5" {
		t.Errorf("subprotocol = %q, want %q", conn.Subprotocol(), "socks5")
	}
}

// Test WebSocket Basic Auth with hashed credentials
func TestWebSocketListener_BasicAuth_HashedCredentials(t *testing.T) {
	handler := NewHandler(nil, nil)

	// Create hashed credentials
	hash := MustHashPassword("securepass")
	creds := HashedCredentials{"secureuser": hash}

	l, err := NewWebSocketListener(WebSocketConfig{
		Address:     "127.0.0.1:0",
		Path:        "/socks5",
		PlainText:   true,
		Credentials: creds,
	}, handler)
	if err != nil {
		t.Fatalf("create listener: %v", err)
	}

	if err := l.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer l.Stop()

	// Connect with correct credentials
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	wsURL := "ws://" + l.Address() + "/socks5"
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		Subprotocols: []string{"socks5"},
		HTTPHeader: http.Header{
			"Authorization": []string{"Basic " + base64Encode("secureuser:securepass")},
		},
	})
	if err != nil {
		t.Fatalf("dial with correct credentials: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	// Verify connection succeeded
	if conn.Subprotocol() != "socks5" {
		t.Errorf("subprotocol = %q, want %q", conn.Subprotocol(), "socks5")
	}
}

// base64Encode encodes a string to base64 for Basic Auth header
func base64Encode(s string) string {
	return base64.StdEncoding.EncodeToString([]byte(s))
}
