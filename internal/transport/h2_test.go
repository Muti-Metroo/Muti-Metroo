package transport

import (
	"context"
	"crypto/tls"
	"sync"
	"testing"
	"time"
)

func TestH2Transport_Type(t *testing.T) {
	transport := NewH2Transport()
	defer transport.Close()

	if transport.Type() != TransportHTTP2 {
		t.Errorf("Type() = %s, want %s", transport.Type(), TransportHTTP2)
	}
}

func TestH2Transport_ListenDialClose(t *testing.T) {
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
		NextProtos:         []string{"h2"},
	}

	// Create transport and listener
	transport := NewH2Transport()
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

	h2URL := "https://" + addr + "/mesh"
	clientConn, err := transport.Dial(ctx, h2URL, DialOptions{
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

func TestH2Transport_StreamBidirectional(t *testing.T) {
	// Generate certificate
	certPEM, keyPEM, err := GenerateSelfSignedCert("localhost", 24*time.Hour)
	if err != nil {
		t.Fatalf("GenerateSelfSignedCert() error = %v", err)
	}

	serverTLS, _ := TLSConfigFromBytes(certPEM, keyPEM)
	clientTLS := &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"h2"},
	}

	transport := NewH2Transport()
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

	// Server goroutine
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		conn, err := listener.Accept(ctx)
		if err != nil {
			serverResult <- err
			return
		}
		defer conn.Close()

		close(clientConnected)

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

	h2URL := "https://" + addr + "/mesh"
	clientConn, err := transport.Dial(ctx, h2URL, DialOptions{
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
	testData := []byte("Hello, HTTP/2!")
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

func TestH2Transport_DialClosed(t *testing.T) {
	transport := NewH2Transport()
	transport.Close()

	ctx := context.Background()
	_, err := transport.Dial(ctx, "https://localhost:443/mesh", DialOptions{})
	if err == nil {
		t.Error("Dial() should fail on closed transport")
	}
}

func TestH2Transport_ListenClosed(t *testing.T) {
	transport := NewH2Transport()
	transport.Close()

	_, err := transport.Listen("127.0.0.1:0", ListenOptions{
		TLSConfig: &tls.Config{},
	})
	if err == nil {
		t.Error("Listen() should fail on closed transport")
	}
}

func TestH2Transport_ListenRequiresTLS(t *testing.T) {
	transport := NewH2Transport()
	defer transport.Close()

	_, err := transport.Listen("127.0.0.1:0", ListenOptions{})
	if err == nil {
		t.Error("Listen() should require TLS config")
	}
}

func TestParseH2Address(t *testing.T) {
	tests := []struct {
		addr         string
		expectedBase string
		expectedPath string
	}{
		{"https://localhost:443/mesh", "https://localhost:443", "/mesh"},
		{"https://localhost:8443/custom", "https://localhost:8443", "/custom"},
		{"localhost:443", "https://localhost:443", "/mesh"},
		{"192.168.1.1:8443", "https://192.168.1.1:8443", "/mesh"},
	}

	for _, tt := range tests {
		t.Run(tt.addr, func(t *testing.T) {
			baseURL, path := parseH2Address(tt.addr)

			if baseURL != tt.expectedBase {
				t.Errorf("baseURL = %s, want %s", baseURL, tt.expectedBase)
			}
			if path != tt.expectedPath {
				t.Errorf("path = %s, want %s", path, tt.expectedPath)
			}
		})
	}
}

func TestH2Stream_StreamID(t *testing.T) {
	stream := &H2Stream{id: 42}
	if stream.StreamID() != 42 {
		t.Errorf("StreamID() = %d, want 42", stream.StreamID())
	}
}
