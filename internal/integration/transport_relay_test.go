// Package integration provides integration tests for Muti Metroo.
package integration

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/postalsys/muti-metroo/internal/transport"
)

// TestH2Transport_BidirectionalRelay tests bidirectional data relay through H2 transport.
func TestH2Transport_BidirectionalRelay(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Create TLS certificates
	certPEM, keyPEM, err := transport.GenerateSelfSignedCert("test-h2", 24*time.Hour)
	if err != nil {
		t.Fatalf("Failed to generate cert: %v", err)
	}

	tmpDir, err := os.MkdirTemp("", "h2-relay-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	certFile := tmpDir + "/cert.pem"
	keyFile := tmpDir + "/key.pem"
	os.WriteFile(certFile, certPEM, 0600)
	os.WriteFile(keyFile, keyPEM, 0600)

	tlsCert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		t.Fatalf("Failed to load TLS cert: %v", err)
	}

	h2Transport := transport.NewH2Transport()
	defer h2Transport.Close()

	// Start listener
	listener, err := h2Transport.Listen("127.0.0.1:0", transport.ListenOptions{
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{tlsCert},
		},
		Path: "/mesh",
	})
	if err != nil {
		t.Fatalf("Failed to listen: %v", err)
	}
	defer listener.Close()

	addr := listener.Addr().String()
	t.Logf("H2 listener at %s", addr)

	// Accept connections in goroutine
	var serverConn transport.PeerConn
	serverReady := make(chan struct{})
	serverDone := make(chan struct{})

	go func() {
		defer close(serverDone)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		conn, err := listener.Accept(ctx)
		if err != nil {
			t.Errorf("Accept failed: %v", err)
			return
		}
		serverConn = conn

		stream, err := conn.AcceptStream(ctx)
		if err != nil {
			t.Errorf("AcceptStream failed: %v", err)
			return
		}
		close(serverReady)

		// Echo data back
		buf := make([]byte, 4096)
		for {
			n, err := stream.Read(buf)
			if err != nil {
				if err != io.EOF {
					t.Logf("Server read error: %v", err)
				}
				return
			}
			if n > 0 {
				_, err := stream.Write(buf[:n])
				if err != nil {
					t.Logf("Server write error: %v", err)
					return
				}
			}
		}
	}()

	// Dial connection
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	clientConn, err := h2Transport.Dial(ctx, addr, transport.DialOptions{
		TLSConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
		Timeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatalf("Dial failed: %v", err)
	}
	defer clientConn.Close()

	clientStream, err := clientConn.OpenStream(ctx)
	if err != nil {
		t.Fatalf("OpenStream failed: %v", err)
	}

	// Wait for server to be ready
	select {
	case <-serverReady:
	case <-time.After(5 * time.Second):
		t.Fatal("Server not ready in time")
	}

	t.Log("H2 connection established, testing bidirectional relay...")

	// Test multiple round trips
	for i := 0; i < 10; i++ {
		testData := []byte(fmt.Sprintf("H2 relay test message %d with some padding data to test larger frames", i))

		// Write data
		n, err := clientStream.Write(testData)
		if err != nil {
			t.Fatalf("Write %d failed: %v", i, err)
		}
		if n != len(testData) {
			t.Fatalf("Write %d: expected %d bytes, wrote %d", i, len(testData), n)
		}

		// Read response
		response := make([]byte, len(testData))
		_, err = io.ReadFull(clientStream, response)
		if err != nil {
			t.Fatalf("Read %d failed: %v", i, err)
		}

		if !bytes.Equal(testData, response) {
			t.Fatalf("Round trip %d: mismatch, got %q, want %q", i, response, testData)
		}
	}

	t.Log("H2 bidirectional relay test passed!")

	// Cleanup
	clientStream.Close()
	clientConn.Close()
	if serverConn != nil {
		serverConn.Close()
	}
	<-serverDone
}

// TestWSTransport_BidirectionalRelay tests bidirectional data relay through WebSocket transport.
func TestWSTransport_BidirectionalRelay(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Create TLS certificates
	certPEM, keyPEM, err := transport.GenerateSelfSignedCert("test-ws", 24*time.Hour)
	if err != nil {
		t.Fatalf("Failed to generate cert: %v", err)
	}

	tmpDir, err := os.MkdirTemp("", "ws-relay-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	certFile := tmpDir + "/cert.pem"
	keyFile := tmpDir + "/key.pem"
	os.WriteFile(certFile, certPEM, 0600)
	os.WriteFile(keyFile, keyPEM, 0600)

	tlsCert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		t.Fatalf("Failed to load TLS cert: %v", err)
	}

	wsTransport := transport.NewWebSocketTransport()
	defer wsTransport.Close()

	// Start listener
	listener, err := wsTransport.Listen("127.0.0.1:0", transport.ListenOptions{
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{tlsCert},
		},
		Path: "/mesh",
	})
	if err != nil {
		t.Fatalf("Failed to listen: %v", err)
	}
	defer listener.Close()

	addr := listener.Addr().String()
	t.Logf("WebSocket listener at %s", addr)

	// Accept connections in goroutine
	var serverConn transport.PeerConn
	serverReady := make(chan struct{})
	serverDone := make(chan struct{})

	go func() {
		defer close(serverDone)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		conn, err := listener.Accept(ctx)
		if err != nil {
			t.Errorf("Accept failed: %v", err)
			return
		}
		serverConn = conn

		stream, err := conn.AcceptStream(ctx)
		if err != nil {
			t.Errorf("AcceptStream failed: %v", err)
			return
		}
		close(serverReady)

		// Echo data back
		buf := make([]byte, 4096)
		for {
			n, err := stream.Read(buf)
			if err != nil {
				if err != io.EOF {
					t.Logf("Server read error: %v", err)
				}
				return
			}
			if n > 0 {
				_, err := stream.Write(buf[:n])
				if err != nil {
					t.Logf("Server write error: %v", err)
					return
				}
			}
		}
	}()

	// Dial connection
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	clientConn, err := wsTransport.Dial(ctx, addr, transport.DialOptions{
		TLSConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
		Timeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatalf("Dial failed: %v", err)
	}
	defer clientConn.Close()

	clientStream, err := clientConn.OpenStream(ctx)
	if err != nil {
		t.Fatalf("OpenStream failed: %v", err)
	}

	// Wait for server to be ready
	select {
	case <-serverReady:
	case <-time.After(5 * time.Second):
		t.Fatal("Server not ready in time")
	}

	t.Log("WebSocket connection established, testing bidirectional relay...")

	// Test multiple round trips
	for i := 0; i < 10; i++ {
		testData := []byte(fmt.Sprintf("WebSocket relay test message %d with some padding data to test larger frames", i))

		// Write data
		n, err := clientStream.Write(testData)
		if err != nil {
			t.Fatalf("Write %d failed: %v", i, err)
		}
		if n != len(testData) {
			t.Fatalf("Write %d: expected %d bytes, wrote %d", i, len(testData), n)
		}

		// Read response
		response := make([]byte, len(testData))
		_, err = io.ReadFull(clientStream, response)
		if err != nil {
			t.Fatalf("Read %d failed: %v", i, err)
		}

		if !bytes.Equal(testData, response) {
			t.Fatalf("Round trip %d: mismatch, got %q, want %q", i, response, testData)
		}
	}

	t.Log("WebSocket bidirectional relay test passed!")

	// Cleanup
	clientStream.Close()
	clientConn.Close()
	if serverConn != nil {
		serverConn.Close()
	}
	<-serverDone
}

// TestH2Transport_ConcurrentReadWrite tests concurrent read/write on H2 transport.
// Note: H2 streams don't support SetReadDeadline, so we use a different approach
// than QUIC - we read until stream closes rather than polling with timeouts.
func TestH2Transport_ConcurrentReadWrite(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Create TLS certificates
	certPEM, keyPEM, err := transport.GenerateSelfSignedCert("test-h2-concurrent", 24*time.Hour)
	if err != nil {
		t.Fatalf("Failed to generate cert: %v", err)
	}

	tmpDir, err := os.MkdirTemp("", "h2-concurrent-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	certFile := tmpDir + "/cert.pem"
	keyFile := tmpDir + "/key.pem"
	os.WriteFile(certFile, certPEM, 0600)
	os.WriteFile(keyFile, keyPEM, 0600)

	tlsCert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		t.Fatalf("Failed to load TLS cert: %v", err)
	}

	h2Transport := transport.NewH2Transport()
	defer h2Transport.Close()

	listener, err := h2Transport.Listen("127.0.0.1:0", transport.ListenOptions{
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{tlsCert},
		},
		Path: "/mesh",
	})
	if err != nil {
		t.Fatalf("Failed to listen: %v", err)
	}
	defer listener.Close()

	addr := listener.Addr().String()
	t.Logf("H2 listener at %s", addr)

	// Server: read and write concurrently
	serverReady := make(chan struct{})
	serverWriteDone := make(chan struct{})
	serverDone := make(chan error, 1)
	var serverReceived atomic.Int64

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		conn, err := listener.Accept(ctx)
		if err != nil {
			serverDone <- fmt.Errorf("accept failed: %w", err)
			return
		}
		defer conn.Close()

		stream, err := conn.AcceptStream(ctx)
		if err != nil {
			serverDone <- fmt.Errorf("accept stream failed: %w", err)
			return
		}
		close(serverReady)

		var wg sync.WaitGroup

		// Writer goroutine - sends its own data
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer close(serverWriteDone)
			for i := 0; i < 50; i++ { // Reduced iterations for faster test
				data := []byte(fmt.Sprintf("SERVER:%d", i))
				_, err := stream.Write(data)
				if err != nil {
					t.Logf("Server write error: %v", err)
					return
				}
				time.Sleep(5 * time.Millisecond)
			}
		}()

		// Reader goroutine - reads until error (stream closed)
		wg.Add(1)
		go func() {
			defer wg.Done()
			buf := make([]byte, 4096)
			for {
				n, err := stream.Read(buf)
				if err != nil {
					if err != io.EOF {
						t.Logf("Server read error: %v", err)
					}
					return
				}
				if n > 0 {
					serverReceived.Add(int64(n))
				}
			}
		}()

		wg.Wait()
		serverDone <- nil
	}()

	// Client
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	clientConn, err := h2Transport.Dial(ctx, addr, transport.DialOptions{
		TLSConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
		Timeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatalf("Dial failed: %v", err)
	}
	defer clientConn.Close()

	clientStream, err := clientConn.OpenStream(ctx)
	if err != nil {
		t.Fatalf("OpenStream failed: %v", err)
	}

	// Wait for server to be ready
	select {
	case <-serverReady:
	case <-time.After(5 * time.Second):
		t.Fatal("Server not ready in time")
	}

	t.Log("Testing concurrent read/write on H2...")

	var clientReceived atomic.Int64
	clientWriteDone := make(chan struct{})

	// Client writer
	go func() {
		defer close(clientWriteDone)
		for i := 0; i < 50; i++ { // Reduced iterations for faster test
			data := []byte(fmt.Sprintf("CLIENT:%d", i))
			_, err := clientStream.Write(data)
			if err != nil {
				t.Logf("Client write error: %v", err)
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()

	// Client reader - reads until error (stream closed)
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		buf := make([]byte, 4096)
		for {
			n, err := clientStream.Read(buf)
			if err != nil {
				return
			}
			if n > 0 {
				clientReceived.Add(int64(n))
			}
		}
	}()

	// Wait for both sides to finish writing
	<-clientWriteDone
	<-serverWriteDone

	// Give a small window for final data to arrive
	time.Sleep(100 * time.Millisecond)

	// Close client stream to signal end and unblock readers
	clientStream.Close()
	clientConn.Close()

	// Wait for reader to finish
	select {
	case <-readerDone:
	case <-time.After(2 * time.Second):
		// Reader should exit once stream is closed
	}

	// Wait for server
	select {
	case err := <-serverDone:
		if err != nil {
			t.Errorf("Server error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Error("Server didn't finish in time")
	}

	t.Logf("Client received: %d bytes, Server received: %d bytes", clientReceived.Load(), serverReceived.Load())

	if clientReceived.Load() == 0 {
		t.Error("Client received no data")
	}
	if serverReceived.Load() == 0 {
		t.Error("Server received no data")
	}

	t.Log("H2 concurrent read/write test passed!")
}

// TestWSTransport_ConcurrentReadWrite tests concurrent read/write on WebSocket transport.
// Note: WebSocket streams don't support SetReadDeadline, so we use a different approach
// than QUIC - we read until stream closes rather than polling with timeouts.
func TestWSTransport_ConcurrentReadWrite(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Create TLS certificates
	certPEM, keyPEM, err := transport.GenerateSelfSignedCert("test-ws-concurrent", 24*time.Hour)
	if err != nil {
		t.Fatalf("Failed to generate cert: %v", err)
	}

	tmpDir, err := os.MkdirTemp("", "ws-concurrent-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	certFile := tmpDir + "/cert.pem"
	keyFile := tmpDir + "/key.pem"
	os.WriteFile(certFile, certPEM, 0600)
	os.WriteFile(keyFile, keyPEM, 0600)

	tlsCert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		t.Fatalf("Failed to load TLS cert: %v", err)
	}

	wsTransport := transport.NewWebSocketTransport()
	defer wsTransport.Close()

	listener, err := wsTransport.Listen("127.0.0.1:0", transport.ListenOptions{
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{tlsCert},
		},
		Path: "/mesh",
	})
	if err != nil {
		t.Fatalf("Failed to listen: %v", err)
	}
	defer listener.Close()

	addr := listener.Addr().String()
	t.Logf("WebSocket listener at %s", addr)

	// Server
	serverReady := make(chan struct{})
	serverWriteDone := make(chan struct{})
	serverDone := make(chan error, 1)
	var serverReceived atomic.Int64

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		conn, err := listener.Accept(ctx)
		if err != nil {
			serverDone <- fmt.Errorf("accept failed: %w", err)
			return
		}
		defer conn.Close()

		stream, err := conn.AcceptStream(ctx)
		if err != nil {
			serverDone <- fmt.Errorf("accept stream failed: %w", err)
			return
		}
		close(serverReady)

		var wg sync.WaitGroup

		// Writer
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer close(serverWriteDone)
			for i := 0; i < 50; i++ { // Reduced iterations for faster test
				data := []byte(fmt.Sprintf("SERVER:%d", i))
				_, err := stream.Write(data)
				if err != nil {
					t.Logf("Server write error: %v", err)
					return
				}
				time.Sleep(5 * time.Millisecond)
			}
		}()

		// Reader - reads until error (stream closed)
		wg.Add(1)
		go func() {
			defer wg.Done()
			buf := make([]byte, 4096)
			for {
				n, err := stream.Read(buf)
				if err != nil {
					if err != io.EOF {
						t.Logf("Server read error: %v", err)
					}
					return
				}
				if n > 0 {
					serverReceived.Add(int64(n))
				}
			}
		}()

		wg.Wait()
		serverDone <- nil
	}()

	// Client
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	clientConn, err := wsTransport.Dial(ctx, addr, transport.DialOptions{
		TLSConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
		Timeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatalf("Dial failed: %v", err)
	}
	defer clientConn.Close()

	clientStream, err := clientConn.OpenStream(ctx)
	if err != nil {
		t.Fatalf("OpenStream failed: %v", err)
	}

	select {
	case <-serverReady:
	case <-time.After(5 * time.Second):
		t.Fatal("Server not ready in time")
	}

	t.Log("Testing concurrent read/write on WebSocket...")

	var clientReceived atomic.Int64
	clientWriteDone := make(chan struct{})

	// Client writer
	go func() {
		defer close(clientWriteDone)
		for i := 0; i < 50; i++ { // Reduced iterations for faster test
			data := []byte(fmt.Sprintf("CLIENT:%d", i))
			_, err := clientStream.Write(data)
			if err != nil {
				t.Logf("Client write error: %v", err)
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()

	// Client reader - reads until error (stream closed)
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		buf := make([]byte, 4096)
		for {
			n, err := clientStream.Read(buf)
			if err != nil {
				return
			}
			if n > 0 {
				clientReceived.Add(int64(n))
			}
		}
	}()

	// Wait for both sides to finish writing
	<-clientWriteDone
	<-serverWriteDone

	// Give a small window for final data to arrive
	time.Sleep(100 * time.Millisecond)

	// Close client stream to signal end and unblock readers
	clientStream.Close()
	clientConn.Close()

	// Wait for reader to finish
	select {
	case <-readerDone:
	case <-time.After(2 * time.Second):
		// Reader should exit once stream is closed
	}

	select {
	case err := <-serverDone:
		if err != nil {
			t.Errorf("Server error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Error("Server didn't finish in time")
	}

	t.Logf("Client received: %d bytes, Server received: %d bytes", clientReceived.Load(), serverReceived.Load())

	if clientReceived.Load() == 0 {
		t.Error("Client received no data")
	}
	if serverReceived.Load() == 0 {
		t.Error("Server received no data")
	}

	t.Log("WebSocket concurrent read/write test passed!")
}

// TestH2Transport_LargeDataTransfer tests large data transfer through H2 transport.
func TestH2Transport_LargeDataTransfer(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	certPEM, keyPEM, err := transport.GenerateSelfSignedCert("test-h2-large", 24*time.Hour)
	if err != nil {
		t.Fatalf("Failed to generate cert: %v", err)
	}

	tmpDir, err := os.MkdirTemp("", "h2-large-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	certFile := tmpDir + "/cert.pem"
	keyFile := tmpDir + "/key.pem"
	os.WriteFile(certFile, certPEM, 0600)
	os.WriteFile(keyFile, keyPEM, 0600)

	tlsCert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		t.Fatalf("Failed to load TLS cert: %v", err)
	}

	h2Transport := transport.NewH2Transport()
	defer h2Transport.Close()

	listener, err := h2Transport.Listen("127.0.0.1:0", transport.ListenOptions{
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{tlsCert},
		},
		Path: "/mesh",
	})
	if err != nil {
		t.Fatalf("Failed to listen: %v", err)
	}
	defer listener.Close()

	addr := listener.Addr().String()

	// Server echoes back
	serverReady := make(chan struct{})
	serverDone := make(chan struct{})

	go func() {
		defer close(serverDone)
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		conn, err := listener.Accept(ctx)
		if err != nil {
			t.Errorf("Accept failed: %v", err)
			return
		}
		defer conn.Close()

		stream, err := conn.AcceptStream(ctx)
		if err != nil {
			t.Errorf("AcceptStream failed: %v", err)
			return
		}
		close(serverReady)

		io.Copy(stream, stream)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	clientConn, err := h2Transport.Dial(ctx, addr, transport.DialOptions{
		TLSConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
		Timeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatalf("Dial failed: %v", err)
	}
	defer clientConn.Close()

	clientStream, err := clientConn.OpenStream(ctx)
	if err != nil {
		t.Fatalf("OpenStream failed: %v", err)
	}
	defer clientStream.Close()

	select {
	case <-serverReady:
	case <-time.After(5 * time.Second):
		t.Fatal("Server not ready")
	}

	// Generate 1MB of test data
	testData := make([]byte, 1024*1024)
	for i := range testData {
		testData[i] = byte(i % 256)
	}

	t.Log("Testing 1MB data transfer through H2...")

	// Write in chunks
	var written int
	chunkSize := 16 * 1024 // 16KB chunks
	for written < len(testData) {
		end := written + chunkSize
		if end > len(testData) {
			end = len(testData)
		}
		n, err := clientStream.Write(testData[written:end])
		if err != nil {
			t.Fatalf("Write failed at offset %d: %v", written, err)
		}
		written += n
	}

	// Read response
	response := make([]byte, len(testData))
	var read int
	for read < len(response) {
		n, err := clientStream.Read(response[read:])
		if err != nil {
			t.Fatalf("Read failed at offset %d: %v", read, err)
		}
		read += n
	}

	if !bytes.Equal(testData, response) {
		t.Error("Data mismatch in large transfer")
	} else {
		t.Log("1MB H2 data transfer verified successfully!")
	}

	clientStream.Close()
	clientConn.Close()
	<-serverDone
}
