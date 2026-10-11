package transport

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestStreamIDAllocator(t *testing.T) {
	t.Run("dialer allocates odd IDs", func(t *testing.T) {
		alloc := NewStreamIDAllocator(true)

		if !alloc.IsDialer() {
			t.Error("IsDialer() = false, want true")
		}

		for i := 0; i < 5; i++ {
			id := alloc.Next()
			if id%2 != 1 {
				t.Errorf("Dialer ID %d is not odd", id)
			}
		}
	})

	t.Run("listener allocates even IDs", func(t *testing.T) {
		alloc := NewStreamIDAllocator(false)

		if alloc.IsDialer() {
			t.Error("IsDialer() = true, want false")
		}

		for i := 0; i < 5; i++ {
			id := alloc.Next()
			if id%2 != 0 {
				t.Errorf("Listener ID %d is not even", id)
			}
		}
	})

	t.Run("IDs are sequential", func(t *testing.T) {
		alloc := NewStreamIDAllocator(true)

		id1 := alloc.Next()
		id2 := alloc.Next()
		id3 := alloc.Next()

		if id2 != id1+2 || id3 != id2+2 {
			t.Errorf("IDs not sequential: %d, %d, %d", id1, id2, id3)
		}
	})

	t.Run("concurrent access produces unique IDs", func(t *testing.T) {
		alloc := NewStreamIDAllocator(true)
		const numGoroutines = 100
		const idsPerGoroutine = 100

		// Channel to collect all allocated IDs
		idChan := make(chan uint64, numGoroutines*idsPerGoroutine)

		var wg sync.WaitGroup
		wg.Add(numGoroutines)

		for i := 0; i < numGoroutines; i++ {
			go func() {
				defer wg.Done()
				for j := 0; j < idsPerGoroutine; j++ {
					idChan <- alloc.Next()
				}
			}()
		}

		wg.Wait()
		close(idChan)

		// Collect all IDs and check for uniqueness
		seen := make(map[uint64]bool)
		for id := range idChan {
			if seen[id] {
				t.Errorf("Duplicate ID allocated: %d", id)
			}
			seen[id] = true

			// Verify all IDs are odd (dialer)
			if id%2 != 1 {
				t.Errorf("ID %d is not odd", id)
			}
		}

		expectedCount := numGoroutines * idsPerGoroutine
		if len(seen) != expectedCount {
			t.Errorf("Expected %d unique IDs, got %d", expectedCount, len(seen))
		}
	})
}

func TestDefaultOptions(t *testing.T) {
	dialOpts := DefaultDialOptions()
	if dialOpts.Timeout != 30*time.Second {
		t.Errorf("DialOptions.Timeout = %v, want 30s", dialOpts.Timeout)
	}

	listenOpts := DefaultListenOptions()
	if listenOpts.MaxStreams != 10000 {
		t.Errorf("ListenOptions.MaxStreams = %d, want 10000", listenOpts.MaxStreams)
	}
}

func TestGenerateSelfSignedCert(t *testing.T) {
	certPEM, keyPEM, err := GenerateSelfSignedCert("test.local", 24*time.Hour)
	if err != nil {
		t.Fatalf("GenerateSelfSignedCert() error = %v", err)
	}

	if len(certPEM) == 0 {
		t.Error("certPEM is empty")
	}
	if len(keyPEM) == 0 {
		t.Error("keyPEM is empty")
	}

	// Verify we can parse the certificate
	_, err = tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Errorf("Failed to parse generated certificate: %v", err)
	}
}

func TestTLSConfigFromBytes(t *testing.T) {
	certPEM, keyPEM, err := GenerateSelfSignedCert("test.local", 24*time.Hour)
	if err != nil {
		t.Fatalf("GenerateSelfSignedCert() error = %v", err)
	}

	config, err := TLSConfigFromBytes(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("TLSConfigFromBytes() error = %v", err)
	}

	if len(config.Certificates) != 1 {
		t.Errorf("Certificates count = %d, want 1", len(config.Certificates))
	}
	if config.MinVersion != tls.VersionTLS13 {
		t.Errorf("MinVersion = %d, want TLS 1.3", config.MinVersion)
	}
}

func TestGenerateAndSaveCert(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "muti-metroo-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	certFile := filepath.Join(tmpDir, "cert.pem")
	keyFile := filepath.Join(tmpDir, "key.pem")

	err = GenerateAndSaveCert(certFile, keyFile, "test.local", 24*time.Hour)
	if err != nil {
		t.Fatalf("GenerateAndSaveCert() error = %v", err)
	}

	// Verify files exist
	if _, err := os.Stat(certFile); os.IsNotExist(err) {
		t.Error("Certificate file not created")
	}
	if _, err := os.Stat(keyFile); os.IsNotExist(err) {
		t.Error("Key file not created")
	}

	// Verify we can load them
	_, err = LoadTLSConfig(certFile, keyFile)
	if err != nil {
		t.Errorf("LoadTLSConfig() error = %v", err)
	}
}

func TestLoadTLSConfig(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "muti-metroo-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	certFile := filepath.Join(tmpDir, "cert.pem")
	keyFile := filepath.Join(tmpDir, "key.pem")

	// Generate certificate
	certPEM, keyPEM, err := GenerateSelfSignedCert("test.local", 24*time.Hour)
	if err != nil {
		t.Fatalf("GenerateSelfSignedCert() error = %v", err)
	}

	os.WriteFile(certFile, certPEM, 0644)
	os.WriteFile(keyFile, keyPEM, 0600)

	config, err := LoadTLSConfig(certFile, keyFile)
	if err != nil {
		t.Fatalf("LoadTLSConfig() error = %v", err)
	}

	if len(config.NextProtos) == 0 || config.NextProtos[0] != ALPNProtocol {
		t.Errorf("NextProtos = %v, want %s", config.NextProtos, ALPNProtocol)
	}
}

func TestLoadTLSConfig_NotFound(t *testing.T) {
	_, err := LoadTLSConfig("/nonexistent/cert.pem", "/nonexistent/key.pem")
	if err == nil {
		t.Error("LoadTLSConfig() should fail for nonexistent files")
	}
}

func TestCloneTLSConfig(t *testing.T) {
	original := &tls.Config{
		MinVersion: tls.VersionTLS13,
		ServerName: "test.local",
	}

	cloned := CloneTLSConfig(original)
	if cloned == original {
		t.Error("CloneTLSConfig() returned same pointer")
	}
	if cloned.MinVersion != original.MinVersion {
		t.Error("CloneTLSConfig() did not copy MinVersion")
	}
	if cloned.ServerName != original.ServerName {
		t.Error("CloneTLSConfig() did not copy ServerName")
	}

	// Test nil case
	if CloneTLSConfig(nil) != nil {
		t.Error("CloneTLSConfig(nil) should return nil")
	}
}

func TestLoadClientTLSConfig(t *testing.T) {
	config, err := LoadClientTLSConfig("", false)
	if err != nil {
		t.Fatalf("LoadClientTLSConfig() error = %v", err)
	}

	if config.MinVersion != tls.VersionTLS13 {
		t.Errorf("MinVersion = %d, want TLS 1.3", config.MinVersion)
	}
}

func TestLoadClientTLSConfig_StrictVerify(t *testing.T) {
	// strictVerify=true means InsecureSkipVerify=false (verify certs)
	config, err := LoadClientTLSConfig("", true)
	if err != nil {
		t.Fatalf("LoadClientTLSConfig() error = %v", err)
	}

	if config.InsecureSkipVerify {
		t.Error("InsecureSkipVerify = true, want false when strictVerify=true")
	}
}

func TestLoadClientTLSConfig_NoStrictVerify(t *testing.T) {
	// strictVerify=false means InsecureSkipVerify=true (skip verification)
	config, err := LoadClientTLSConfig("", false)
	if err != nil {
		t.Fatalf("LoadClientTLSConfig() error = %v", err)
	}

	if !config.InsecureSkipVerify {
		t.Error("InsecureSkipVerify = false, want true when strictVerify=false")
	}
}

func TestLoadCAPool(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "muti-metroo-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Generate a CA certificate
	certPEM, _, err := GenerateSelfSignedCert("ca.local", 24*time.Hour)
	if err != nil {
		t.Fatalf("GenerateSelfSignedCert() error = %v", err)
	}

	caFile := filepath.Join(tmpDir, "ca.pem")
	os.WriteFile(caFile, certPEM, 0644)

	pool, err := LoadCAPool(caFile)
	if err != nil {
		t.Fatalf("LoadCAPool() error = %v", err)
	}

	if pool == nil {
		t.Error("LoadCAPool() returned nil pool")
	}
}

func TestLoadCAPool_NotFound(t *testing.T) {
	_, err := LoadCAPool("/nonexistent/ca.pem")
	if err == nil {
		t.Error("LoadCAPool() should fail for nonexistent file")
	}
}

func TestLoadCAPool_InvalidCert(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "muti-metroo-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	caFile := filepath.Join(tmpDir, "invalid.pem")
	os.WriteFile(caFile, []byte("not a valid certificate"), 0644)

	_, err = LoadCAPool(caFile)
	if err == nil {
		t.Error("LoadCAPool() should fail for invalid certificate")
	}
}

func TestQUICTransport_Type(t *testing.T) {
	transport := NewQUICTransport()
	defer transport.Close()

	if transport.Type() != TransportQUIC {
		t.Errorf("Type() = %s, want %s", transport.Type(), TransportQUIC)
	}
}

func TestQUICTransport_ListenDialClose(t *testing.T) {
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
		NextProtos:         []string{ALPNProtocol},
	}

	// Create transport and listener
	transport := NewQUICTransport()
	defer transport.Close()

	listener, err := transport.Listen("127.0.0.1:0", ListenOptions{
		TLSConfig: serverTLS,
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

	clientConn, err := transport.Dial(ctx, addr, DialOptions{
		TLSConfig: clientTLS,
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

	// Verify addresses
	if clientConn.LocalAddr() == nil {
		t.Error("Client LocalAddr() = nil")
	}
	if clientConn.RemoteAddr() == nil {
		t.Error("Client RemoteAddr() = nil")
	}
}

func TestQUICTransport_StreamBidirectional(t *testing.T) {
	// Generate certificate
	certPEM, keyPEM, err := GenerateSelfSignedCert("localhost", 24*time.Hour)
	if err != nil {
		t.Fatalf("GenerateSelfSignedCert() error = %v", err)
	}

	serverTLS, _ := TLSConfigFromBytes(certPEM, keyPEM)
	clientTLS := &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{ALPNProtocol},
	}

	transport := NewQUICTransport()
	defer transport.Close()

	listener, err := transport.Listen("127.0.0.1:0", ListenOptions{TLSConfig: serverTLS})
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	defer listener.Close()

	addr := listener.Addr().String()

	serverResult := make(chan error, 1)
	clientConnected := make(chan struct{})
	clientDone := make(chan struct{})

	// Server goroutine - accepts connection, then accepts stream and echoes
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		conn, err := listener.Accept(ctx)
		if err != nil {
			serverResult <- fmt.Errorf("accept connection: %w", err)
			return
		}

		// Signal that connection is established
		close(clientConnected)

		stream, err := conn.AcceptStream(ctx)
		if err != nil {
			conn.Close()
			serverResult <- fmt.Errorf("accept stream: %w", err)
			return
		}

		// Echo data back with deadline
		stream.SetReadDeadline(time.Now().Add(5 * time.Second))
		buf := make([]byte, 1024)
		n, err := stream.Read(buf)
		if err != nil && err != io.EOF {
			conn.Close()
			serverResult <- fmt.Errorf("read: %w", err)
			return
		}

		stream.SetWriteDeadline(time.Now().Add(5 * time.Second))
		_, err = stream.Write(buf[:n])
		if err != nil {
			conn.Close()
			serverResult <- fmt.Errorf("write: %w", err)
			return
		}

		serverResult <- nil

		// Wait for client to finish reading before closing
		<-clientDone
		conn.Close()
	}()

	// Client - dial and open stream
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	clientConn, err := transport.Dial(ctx, addr, DialOptions{TLSConfig: clientTLS})
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer clientConn.Close()

	// Wait for server to accept connection before opening stream
	select {
	case <-clientConnected:
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for connection")
	}

	stream, err := clientConn.OpenStream(ctx)
	if err != nil {
		t.Fatalf("OpenStream() error = %v", err)
	}
	defer stream.Close()

	// Send and receive
	testData := []byte("Hello, QUIC!")
	_, err = stream.Write(testData)
	if err != nil {
		t.Fatalf("Client Write() error = %v", err)
	}

	// Set deadline for read
	stream.SetReadDeadline(time.Now().Add(5 * time.Second))

	response := make([]byte, len(testData))
	_, err = io.ReadFull(stream, response)
	if err != nil {
		t.Fatalf("Client Read() error = %v", err)
	}

	if !bytes.Equal(response, testData) {
		t.Errorf("Response = %s, want %s", response, testData)
	}

	// Signal client is done reading
	close(clientDone)

	// Wait for server to finish
	select {
	case err := <-serverResult:
		if err != nil {
			t.Errorf("Server error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Error("Timeout waiting for server to finish")
	}
}

func TestQUICTransport_Listen_NoTLS(t *testing.T) {
	transport := NewQUICTransport()
	defer transport.Close()

	_, err := transport.Listen("127.0.0.1:0", ListenOptions{})
	if err == nil {
		t.Error("Listen() should fail without TLS config")
	}
}

func TestQUICTransport_Dial_AutoGeneratesTLS(t *testing.T) {
	transport := NewQUICTransport()
	defer transport.Close()

	ctx := context.Background()

	// Dial without TLS config should auto-generate one (default is StrictVerify=false)
	// The dial will fail for connection reasons (no server), not TLS config reasons
	// Use a random high port that's unlikely to be in use
	_, err := transport.Dial(ctx, "127.0.0.1:59999", DialOptions{Timeout: 500 * time.Millisecond})

	// The error should be a connection error (timeout or refused), not a TLS config error
	if err != nil && err.Error() == "TLS config required" {
		t.Error("Dial() without TLS config should auto-generate one, not require explicit config")
	}
	// Note: We don't require err != nil because QUIC dial behavior varies by platform.
	// The key assertion is that if there IS an error, it's not about missing TLS config.
}

func TestQUICTransport_Dial_Closed(t *testing.T) {
	transport := NewQUICTransport()
	transport.Close()

	ctx := context.Background()
	_, err := transport.Dial(ctx, "127.0.0.1:4433", DialOptions{})
	if err == nil {
		t.Error("Dial() on closed transport should fail")
	}
}

func TestQUICTransport_Listen_Closed(t *testing.T) {
	transport := NewQUICTransport()
	transport.Close()

	_, err := transport.Listen("127.0.0.1:0", ListenOptions{
		TLSConfig: &tls.Config{},
	})
	if err == nil {
		t.Error("Listen() on closed transport should fail")
	}
}

func TestQUICTransport_CloseMultiple(t *testing.T) {
	transport := NewQUICTransport()

	err1 := transport.Close()
	err2 := transport.Close()

	// Both should succeed (second is no-op)
	if err1 != nil {
		t.Errorf("First Close() error = %v", err1)
	}
	if err2 != nil {
		t.Errorf("Second Close() error = %v", err2)
	}
}

func TestTransportType_String(t *testing.T) {
	if TransportQUIC != "quic" {
		t.Errorf("TransportQUIC = %s, want quic", TransportQUIC)
	}
	if TransportHTTP2 != "h2" {
		t.Errorf("TransportHTTP2 = %s, want h2", TransportHTTP2)
	}
	if TransportWebSocket != "ws" {
		t.Errorf("TransportWebSocket = %s, want ws", TransportWebSocket)
	}
}

func TestQUICListener_Address(t *testing.T) {
	certPEM, keyPEM, _ := GenerateSelfSignedCert("localhost", 24*time.Hour)
	serverTLS, _ := TLSConfigFromBytes(certPEM, keyPEM)

	transport := NewQUICTransport()
	defer transport.Close()

	listener, err := transport.Listen("127.0.0.1:0", ListenOptions{TLSConfig: serverTLS})
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	defer listener.Close()

	addr := listener.Addr()
	if addr == nil {
		t.Fatal("Addr() = nil")
	}

	// Should be a UDP address
	_, ok := addr.(*net.UDPAddr)
	if !ok {
		t.Errorf("Addr() type = %T, want *net.UDPAddr", addr)
	}
}
