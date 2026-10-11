package forward

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// mockDialer is a mock implementation of TunnelDialer for testing.
type mockDialer struct {
	dialFunc func(ctx context.Context, key string) (net.Conn, error)
	calls    atomic.Int64
}

func (m *mockDialer) DialForward(ctx context.Context, key string) (net.Conn, error) {
	m.calls.Add(1)
	if m.dialFunc != nil {
		return m.dialFunc(ctx, key)
	}
	return nil, fmt.Errorf("dial not implemented")
}

// mockConn is a mock net.Conn for testing.
type mockConn struct {
	readData   []byte
	readIdx    int
	writeData  []byte
	writeMu    sync.Mutex
	closed     atomic.Bool
	readErr    error
	writeErr   error
	localAddr  net.Addr
	remoteAddr net.Addr
}

func newMockConn() *mockConn {
	return &mockConn{
		localAddr:  &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 12345},
		remoteAddr: &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 54321},
	}
}

func (m *mockConn) Read(b []byte) (n int, err error) {
	if m.closed.Load() {
		return 0, io.EOF
	}
	if m.readErr != nil {
		return 0, m.readErr
	}
	if m.readIdx >= len(m.readData) {
		// Block until closed
		for !m.closed.Load() {
			time.Sleep(10 * time.Millisecond)
		}
		return 0, io.EOF
	}
	n = copy(b, m.readData[m.readIdx:])
	m.readIdx += n
	return n, nil
}

func (m *mockConn) Write(b []byte) (n int, err error) {
	if m.closed.Load() {
		return 0, fmt.Errorf("connection closed")
	}
	if m.writeErr != nil {
		return 0, m.writeErr
	}
	m.writeMu.Lock()
	m.writeData = append(m.writeData, b...)
	m.writeMu.Unlock()
	return len(b), nil
}

func (m *mockConn) Close() error {
	m.closed.Store(true)
	return nil
}

func (m *mockConn) LocalAddr() net.Addr                { return m.localAddr }
func (m *mockConn) RemoteAddr() net.Addr               { return m.remoteAddr }
func (m *mockConn) SetDeadline(t time.Time) error      { return nil }
func (m *mockConn) SetReadDeadline(t time.Time) error  { return nil }
func (m *mockConn) SetWriteDeadline(t time.Time) error { return nil }

func (m *mockConn) GetWriteData() []byte {
	m.writeMu.Lock()
	defer m.writeMu.Unlock()
	return m.writeData
}

func TestNewListener(t *testing.T) {
	dialer := &mockDialer{}
	cfg := ListenerConfig{
		Key:            "test-key",
		Address:        "127.0.0.1:0",
		MaxConnections: 100,
	}

	listener := NewListener(cfg, dialer)

	if listener == nil {
		t.Fatal("NewListener returned nil")
	}

	if listener.Key() != "test-key" {
		t.Errorf("expected key 'test-key', got '%s'", listener.Key())
	}

	if listener.ConnectionCount() != 0 {
		t.Errorf("expected 0 connections, got %d", listener.ConnectionCount())
	}

	if listener.Address() != nil {
		t.Error("expected nil address before start")
	}
}

func TestListener_StartStop(t *testing.T) {
	dialer := &mockDialer{}
	cfg := ListenerConfig{
		Key:     "test-key",
		Address: "127.0.0.1:0",
	}

	listener := NewListener(cfg, dialer)

	// Start listener
	err := listener.Start()
	if err != nil {
		t.Fatalf("failed to start listener: %v", err)
	}

	// Verify address is assigned
	addr := listener.Address()
	if addr == nil {
		t.Fatal("expected non-nil address after start")
	}

	// Try to start again (should fail)
	err = listener.Start()
	if err == nil {
		t.Error("expected error when starting already running listener")
	}

	// Stop listener
	err = listener.Stop()
	if err != nil {
		t.Errorf("failed to stop listener: %v", err)
	}
}

func TestListener_ConnectionLimit(t *testing.T) {
	// Use a channel to control when dialer returns
	dialBlocked := make(chan struct{})
	dialer := &mockDialer{
		dialFunc: func(ctx context.Context, key string) (net.Conn, error) {
			// Block until context is done or test signals
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-dialBlocked:
				return nil, fmt.Errorf("dial blocked for test")
			}
		},
	}

	cfg := ListenerConfig{
		Key:            "test-key",
		Address:        "127.0.0.1:0",
		MaxConnections: 2,
	}

	listener := NewListener(cfg, dialer)

	err := listener.Start()
	if err != nil {
		t.Fatalf("failed to start listener: %v", err)
	}

	addr := listener.Address().String()

	// Create connections up to the limit
	conns := make([]net.Conn, 0, 3)
	for i := 0; i < 2; i++ {
		conn, err := net.Dial("tcp", addr)
		if err != nil {
			t.Fatalf("failed to connect: %v", err)
		}
		conns = append(conns, conn)
	}

	// Wait for connections to be tracked
	time.Sleep(100 * time.Millisecond)

	// Verify connection count
	if listener.ConnectionCount() != 2 {
		t.Errorf("expected 2 connections, got %d", listener.ConnectionCount())
	}

	// Third connection should be rejected (closed immediately at accept)
	conn3, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	conns = append(conns, conn3)

	// Wait a bit and verify it was rejected
	time.Sleep(100 * time.Millisecond)

	// The connection count should still be 2 (third was rejected)
	if listener.ConnectionCount() > 2 {
		t.Errorf("expected at most 2 connections, got %d", listener.ConnectionCount())
	}

	// Cleanup - close all connections first
	for _, conn := range conns {
		conn.Close()
	}

	// Then stop the listener (which will cancel pending dials)
	listener.Stop()
}

func TestListener_DialFailure(t *testing.T) {
	dialer := &mockDialer{
		dialFunc: func(ctx context.Context, key string) (net.Conn, error) {
			return nil, fmt.Errorf("dial failed: no route")
		},
	}

	cfg := ListenerConfig{
		Key:     "test-key",
		Address: "127.0.0.1:0",
	}

	listener := NewListener(cfg, dialer)

	err := listener.Start()
	if err != nil {
		t.Fatalf("failed to start listener: %v", err)
	}
	defer listener.Stop()

	addr := listener.Address().String()

	// Connect - should succeed initially
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	// Wait for dial attempt
	time.Sleep(100 * time.Millisecond)

	// Connection should be closed due to dial failure
	buf := make([]byte, 1)
	_, err = conn.Read(buf)
	if err == nil {
		t.Error("expected read error after dial failure")
	}
}

func TestListener_DataRelay(t *testing.T) {
	// Create a target server
	targetListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create target listener: %v", err)
	}
	defer targetListener.Close()

	targetAddr := targetListener.Addr().String()

	// Accept connections on target and echo data
	go func() {
		for {
			conn, err := targetListener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				io.Copy(c, c) // Echo
			}(conn)
		}
	}()

	// Create dialer that connects to target
	dialer := &mockDialer{
		dialFunc: func(ctx context.Context, key string) (net.Conn, error) {
			return net.Dial("tcp", targetAddr)
		},
	}

	cfg := ListenerConfig{
		Key:     "test-key",
		Address: "127.0.0.1:0",
	}

	listener := NewListener(cfg, dialer)

	err = listener.Start()
	if err != nil {
		t.Fatalf("failed to start listener: %v", err)
	}
	defer listener.Stop()

	addr := listener.Address().String()

	// Connect through the listener
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	// Send data
	testData := []byte("hello tunnel")
	_, err = conn.Write(testData)
	if err != nil {
		t.Fatalf("failed to write: %v", err)
	}

	// Read echoed data
	conn.SetReadDeadline(time.Now().Add(time.Second))
	buf := make([]byte, len(testData))
	n, err := io.ReadFull(conn, buf)
	if err != nil {
		t.Fatalf("failed to read: %v", err)
	}

	if string(buf[:n]) != string(testData) {
		t.Errorf("expected '%s', got '%s'", testData, buf[:n])
	}
}

func TestListener_Key(t *testing.T) {
	tests := []struct {
		name string
		key  string
	}{
		{"simple", "mykey"},
		{"with-dash", "my-key"},
		{"with-underscore", "my_key"},
		{"with-numbers", "key123"},
		{"empty", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := ListenerConfig{
				Key:     tt.key,
				Address: "127.0.0.1:0",
			}
			listener := NewListener(cfg, &mockDialer{})
			if listener.Key() != tt.key {
				t.Errorf("expected key '%s', got '%s'", tt.key, listener.Key())
			}
		})
	}
}

func TestRelay(t *testing.T) {
	// Create two connected mock connections
	client := newMockConn()
	target := newMockConn()

	// Set up data
	client.readData = []byte("client to target")
	target.readData = []byte("target to client")

	// Close connections after a delay to end relay
	go func() {
		time.Sleep(50 * time.Millisecond)
		client.Close()
		target.Close()
	}()

	// Run relay
	relay(client, target)

	// Verify data was copied
	clientWritten := client.GetWriteData()
	targetWritten := target.GetWriteData()

	if string(targetWritten) != "client to target" {
		t.Errorf("expected target to receive 'client to target', got '%s'", targetWritten)
	}

	if string(clientWritten) != "target to client" {
		t.Errorf("expected client to receive 'target to client', got '%s'", clientWritten)
	}
}

func TestListener_StopClosesConnections(t *testing.T) {
	// Create a blocking dialer
	dialCh := make(chan struct{})
	dialer := &mockDialer{
		dialFunc: func(ctx context.Context, key string) (net.Conn, error) {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-dialCh:
				return newMockConn(), nil
			}
		},
	}

	cfg := ListenerConfig{
		Key:     "test-key",
		Address: "127.0.0.1:0",
	}

	listener := NewListener(cfg, dialer)

	err := listener.Start()
	if err != nil {
		t.Fatalf("failed to start listener: %v", err)
	}

	addr := listener.Address().String()

	// Connect
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	// Wait a bit for the connection to be handled
	time.Sleep(50 * time.Millisecond)

	// Stop the listener - should cancel pending dials
	stopDone := make(chan struct{})
	go func() {
		listener.Stop()
		close(stopDone)
	}()

	// Should stop quickly
	select {
	case <-stopDone:
		// Good
	case <-time.After(2 * time.Second):
		t.Error("Stop took too long")
	}
}
