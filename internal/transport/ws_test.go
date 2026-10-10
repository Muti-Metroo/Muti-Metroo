package transport

import (
	"context"
	"crypto/tls"
	"sync"
	"testing"
	"time"
)

func TestWebSocketTransport_Type(t *testing.T) {
	transport := NewWebSocketTransport()
	defer transport.Close()

	if transport.Type() != TransportWebSocket {
		t.Errorf("Type() = %s, want %s", transport.Type(), TransportWebSocket)
	}
}

func TestWebSocketTransport_ListenDialClose(t *testing.T) {
	// Generate certificate for both server and client
	certPEM, keyPEM, err := GenerateSelfSignedCert("localhost", 24*time.Hour)
	if err != nil {
		t.Fatalf("GenerateSelfSignedCert() error = %v", err)
	}

	serverTLS, err := TLSConfigFromBytes(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("TLSConfigFromBytes() error = %v", err)
	}

	clientTLS := &tls.Config{
		InsecureSkipVerify: true,
	}

	// Create transport and listener
	transport := NewWebSocketTransport()
	defer transport.Close()

	listener, err := transport.Listen("127.0.0.1:0", ListenOptions{
		TLSConfig: serverTLS,
		Path:      "/mesh",
	})
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	defer listener.Close()

	addr := listener.Addr().String()

	// Accept in goroutine
	var serverConn PeerConn
	var acceptErr error
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		serverConn, acceptErr = listener.Accept(ctx)
	}()

	// Dial
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	wsURL := "wss://" + addr + "/mesh"
	clientConn, err := transport.Dial(ctx, wsURL, DialOptions{
		TLSConfig: clientTLS,
		Timeout:   5 * time.Second,
	})
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer clientConn.Close()

	wg.Wait()

	if acceptErr != nil {
		t.Fatalf("Accept() error = %v", acceptErr)
	}
	defer serverConn.Close()

	// Verify connection properties
	if !clientConn.IsDialer() {
		t.Error("Client IsDialer() = false")
	}
	if serverConn.IsDialer() {
		t.Error("Server IsDialer() = true")
	}
}

func TestWebSocketTransport_StreamBidirectional(t *testing.T) {
	// Generate certificate
	certPEM, keyPEM, err := GenerateSelfSignedCert("localhost", 24*time.Hour)
	if err != nil {
		t.Fatalf("GenerateSelfSignedCert() error = %v", err)
	}

	serverTLS, _ := TLSConfigFromBytes(certPEM, keyPEM)
	clientTLS := &tls.Config{
		InsecureSkipVerify: true,
	}

	transport := NewWebSocketTransport()
	defer transport.Close()

	listener, err := transport.Listen("127.0.0.1:0", ListenOptions{
		TLSConfig: serverTLS,
		Path:      "/mesh",
	})
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	defer listener.Close()

	addr := listener.Addr().String()

	serverResult := make(chan error, 1)
	clientConnected := make(chan struct{})

	// Server goroutine - accepts connection, then accepts stream and echoes
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		conn, err := listener.Accept(ctx)
		if err != nil {
			serverResult <- err
			return
		}
		defer conn.Close()

		// Signal that connection is established
		close(clientConnected)

		// Accept stream
		stream, err := conn.AcceptStream(ctx)
		if err != nil {
			serverResult <- err
			return
		}

		// Read data and echo back
		buf := make([]byte, 1024)
		n, err := stream.Read(buf)
		if err != nil {
			serverResult <- err
			return
		}

		_, err = stream.Write(buf[:n])
		if err != nil {
			serverResult <- err
			return
		}

		serverResult <- nil
	}()

	// Client side
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	wsURL := "wss://" + addr + "/mesh"
	clientConn, err := transport.Dial(ctx, wsURL, DialOptions{
		TLSConfig: clientTLS,
		Timeout:   5 * time.Second,
	})
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer clientConn.Close()

	// Wait for server to accept
	select {
	case <-clientConnected:
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for server connection")
	}

	// Open stream
	stream, err := clientConn.OpenStream(ctx)
	if err != nil {
		t.Fatalf("OpenStream() error = %v", err)
	}

	// Write test data
	testData := []byte("Hello, WebSocket!")
	_, err = stream.Write(testData)
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	// Read response
	buf := make([]byte, 1024)
	n, err := stream.Read(buf)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}

	if string(buf[:n]) != string(testData) {
		t.Errorf("Received %q, want %q", string(buf[:n]), string(testData))
	}

	// Check server result
	select {
	case err := <-serverResult:
		if err != nil {
			t.Errorf("Server error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Error("Timeout waiting for server result")
	}
}

func TestWebSocketTransport_MultipleMessages(t *testing.T) {
	// Generate certificate
	certPEM, keyPEM, err := GenerateSelfSignedCert("localhost", 24*time.Hour)
	if err != nil {
		t.Fatalf("GenerateSelfSignedCert() error = %v", err)
	}

	serverTLS, _ := TLSConfigFromBytes(certPEM, keyPEM)
	clientTLS := &tls.Config{
		InsecureSkipVerify: true,
	}

	transport := NewWebSocketTransport()
	defer transport.Close()

	listener, err := transport.Listen("127.0.0.1:0", ListenOptions{
		TLSConfig: serverTLS,
		Path:      "/mesh",
	})
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	defer listener.Close()

	addr := listener.Addr().String()

	done := make(chan struct{})
	messageCount := 10

	// Server goroutine
	go func() {
		defer close(done)

		ctx := context.Background()
		conn, err := listener.Accept(ctx)
		if err != nil {
			t.Errorf("Accept() error = %v", err)
			return
		}
		defer conn.Close()

		stream, err := conn.AcceptStream(ctx)
		if err != nil {
			t.Errorf("AcceptStream() error = %v", err)
			return
		}

		// Echo all messages
		for i := 0; i < messageCount; i++ {
			buf := make([]byte, 1024)
			n, err := stream.Read(buf)
			if err != nil {
				t.Errorf("Read() error = %v", err)
				return
			}

			_, err = stream.Write(buf[:n])
			if err != nil {
				t.Errorf("Write() error = %v", err)
				return
			}
		}
	}()

	// Client side
	ctx := context.Background()
	wsURL := "wss://" + addr + "/mesh"
	clientConn, err := transport.Dial(ctx, wsURL, DialOptions{
		TLSConfig: clientTLS,
	})
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer clientConn.Close()

	stream, err := clientConn.OpenStream(ctx)
	if err != nil {
		t.Fatalf("OpenStream() error = %v", err)
	}

	// Send and receive multiple messages
	for i := 0; i < messageCount; i++ {
		testData := []byte("Message " + string(rune('A'+i)))

		_, err = stream.Write(testData)
		if err != nil {
			t.Fatalf("Write() error = %v", err)
		}

		buf := make([]byte, 1024)
		n, err := stream.Read(buf)
		if err != nil {
			t.Fatalf("Read() error = %v", err)
		}

		if string(buf[:n]) != string(testData) {
			t.Errorf("Message %d: received %q, want %q", i, string(buf[:n]), string(testData))
		}
	}

	<-done
}

func TestWebSocketTransport_DialClosed(t *testing.T) {
	transport := NewWebSocketTransport()
	transport.Close()

	ctx := context.Background()
	_, err := transport.Dial(ctx, "wss://localhost:443/mesh", DialOptions{})
	if err == nil {
		t.Error("Dial() should fail on closed transport")
	}
}

func TestWebSocketTransport_ListenClosed(t *testing.T) {
	transport := NewWebSocketTransport()
	transport.Close()

	_, err := transport.Listen("127.0.0.1:0", ListenOptions{
		TLSConfig: &tls.Config{},
	})
	if err == nil {
		t.Error("Listen() should fail on closed transport")
	}
}

func TestWebSocketTransport_ListenRequiresTLS(t *testing.T) {
	transport := NewWebSocketTransport()
	defer transport.Close()

	_, err := transport.Listen("127.0.0.1:0", ListenOptions{})
	if err == nil {
		t.Error("Listen() should require TLS config")
	}
}

func TestParseWebSocketURL(t *testing.T) {
	tests := []struct {
		addr     string
		expected string
	}{
		{"wss://localhost:443/mesh", "wss://localhost:443/mesh"},
		{"ws://localhost:8080/mesh", "ws://localhost:8080/mesh"},
		{"localhost:443", "wss://localhost:443/mesh"},
		// Note: bare host:port always uses wss:// for security (TLS required)
		{"localhost:8080", "wss://localhost:8080/mesh"},
	}

	for _, tt := range tests {
		t.Run(tt.addr, func(t *testing.T) {
			result := parseWebSocketURL(tt.addr)

			if result != tt.expected {
				t.Errorf("parseWebSocketURL() = %s, want %s", result, tt.expected)
			}
		})
	}
}

func TestWebSocketStream_StreamID(t *testing.T) {
	stream := &WebSocketStream{id: 42}
	if stream.StreamID() != 42 {
		t.Errorf("StreamID() = %d, want 42", stream.StreamID())
	}
}

func TestWebSocketTransport_PlainText_Listen(t *testing.T) {
	// Create plaintext WebSocket listener (no TLS)
	transport := NewWebSocketTransport()
	defer transport.Close()

	listener, err := transport.Listen("127.0.0.1:0", ListenOptions{
		Path:      "/mesh",
		PlainText: true, // No TLS required
	})
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	defer listener.Close()

	addr := listener.Addr().String()

	// Accept in goroutine
	var serverConn PeerConn
	var acceptErr error
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		serverConn, acceptErr = listener.Accept(ctx)
	}()

	// Dial using plain ws:// URL
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	wsURL := "ws://" + addr + "/mesh"
	clientConn, err := transport.Dial(ctx, wsURL, DialOptions{
		Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer clientConn.Close()

	wg.Wait()

	if acceptErr != nil {
		t.Fatalf("Accept() error = %v", acceptErr)
	}
	defer serverConn.Close()

	// Verify connection properties
	if !clientConn.IsDialer() {
		t.Error("Client IsDialer() = false")
	}
	if serverConn.IsDialer() {
		t.Error("Server IsDialer() = true")
	}
	if clientConn.TransportType() != TransportWebSocket {
		t.Errorf("TransportType() = %s, want %s", clientConn.TransportType(), TransportWebSocket)
	}
}

func TestWebSocketTransport_PlainText_StreamBidirectional(t *testing.T) {
	// Create plaintext WebSocket transport
	transport := NewWebSocketTransport()
	defer transport.Close()

	listener, err := transport.Listen("127.0.0.1:0", ListenOptions{
		Path:      "/mesh",
		PlainText: true,
	})
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	defer listener.Close()

	addr := listener.Addr().String()

	serverResult := make(chan error, 1)
	clientConnected := make(chan struct{})

	// Server goroutine - accepts connection, then accepts stream and echoes
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		conn, err := listener.Accept(ctx)
		if err != nil {
			serverResult <- err
			return
		}
		defer conn.Close()

		// Signal that connection is established
		close(clientConnected)

		// Accept stream
		stream, err := conn.AcceptStream(ctx)
		if err != nil {
			serverResult <- err
			return
		}

		// Read data and echo back
		buf := make([]byte, 1024)
		n, err := stream.Read(buf)
		if err != nil {
			serverResult <- err
			return
		}

		_, err = stream.Write(buf[:n])
		if err != nil {
			serverResult <- err
			return
		}

		serverResult <- nil
	}()

	// Client side - dial using plain ws://
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	wsURL := "ws://" + addr + "/mesh"
	clientConn, err := transport.Dial(ctx, wsURL, DialOptions{
		Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer clientConn.Close()

	// Wait for server to accept
	select {
	case <-clientConnected:
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for server connection")
	}

	// Open stream
	stream, err := clientConn.OpenStream(ctx)
	if err != nil {
		t.Fatalf("OpenStream() error = %v", err)
	}

	// Write test data
	testData := []byte("Hello, Plain WebSocket!")
	_, err = stream.Write(testData)
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	// Read response
	buf := make([]byte, 1024)
	n, err := stream.Read(buf)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}

	if string(buf[:n]) != string(testData) {
		t.Errorf("Received %q, want %q", string(buf[:n]), string(testData))
	}

	// Check server result
	select {
	case err := <-serverResult:
		if err != nil {
			t.Errorf("Server error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Error("Timeout waiting for server result")
	}
}

func TestWebSocketTransport_PlainText_RequiresWSTransport(t *testing.T) {
	// Verify that plaintext mode requires explicit PlainText flag
	transport := NewWebSocketTransport()
	defer transport.Close()

	// Without PlainText, nil TLS config should fail
	_, err := transport.Listen("127.0.0.1:0", ListenOptions{
		Path: "/mesh",
		// PlainText: false (default)
		// TLSConfig: nil
	})
	if err == nil {
		t.Error("Listen() should fail without TLS config or PlainText flag")
	}

	// With PlainText flag, nil TLS config should succeed
	listener, err := transport.Listen("127.0.0.1:0", ListenOptions{
		Path:      "/mesh",
		PlainText: true,
	})
	if err != nil {
		t.Fatalf("Listen() with PlainText should succeed: %v", err)
	}
	listener.Close()
}
