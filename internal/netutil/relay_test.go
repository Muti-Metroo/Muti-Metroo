package netutil

import (
	"errors"
	"io"
	"net"
	"syscall"
	"testing"
	"time"
)

// tcpPair returns the two ends of a loopback TCP connection.
func tcpPair(t *testing.T) (net.Conn, net.Conn) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		c, _ := ln.Accept()
		accepted <- c
	}()
	dialed, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	server := <-accepted
	if server == nil {
		t.Fatal("accept failed")
	}
	t.Cleanup(func() { dialed.Close(); server.Close() })
	return dialed, server
}

// wrapped hides the TCP connection behind a wrapper, like the SOCKS5
// version-sniffing peekConn.
type wrapped struct{ net.Conn }

func (w wrapped) NetConn() net.Conn { return w.Conn }

// failingConn returns err from Read, like a reset mesh stream.
type failingConn struct {
	net.Conn
	err error
}

func (f failingConn) Read([]byte) (int, error) { return 0, f.err }

// A clean end from the target half-closes the client, which then sees EOF
// while its own direction stays open.
func TestRelay_HalfClosePropagates(t *testing.T) {
	clientApp, clientSide := tcpPair(t)
	targetSide, targetApp := tcpPair(t)

	done := make(chan error, 1)
	go func() { done <- Relay(clientSide, targetSide) }()

	targetApp.Write([]byte("response"))
	targetApp.(*net.TCPConn).CloseWrite()

	clientApp.SetReadDeadline(time.Now().Add(5 * time.Second))
	got, err := io.ReadAll(clientApp)
	if err != nil {
		t.Fatalf("client read: %v", err)
	}
	if string(got) != "response" {
		t.Fatalf("client got %q", got)
	}

	// The client's direction still works after the target's FIN.
	clientApp.Write([]byte("late"))
	clientApp.(*net.TCPConn).CloseWrite()
	targetApp.SetReadDeadline(time.Now().Add(5 * time.Second))
	got, err = io.ReadAll(targetApp)
	if err != nil || string(got) != "late" {
		t.Fatalf("target read = %q, %v", got, err)
	}
	if err := <-done; err != nil {
		t.Fatalf("Relay = %v", err)
	}
}

// A failed target read resets the client connection, even through a
// wrapper, instead of ending it with a clean FIN.
func TestRelay_ErrorResetsClient(t *testing.T) {
	clientApp, clientSide := tcpPair(t)
	targetSide, _ := tcpPair(t)
	streamErr := errors.New("stream reset")

	done := make(chan error, 1)
	go func() {
		done <- Relay(wrapped{clientSide}, failingConn{Conn: targetSide, err: streamErr})
	}()

	clientApp.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, err := io.ReadAll(clientApp)
	if !errors.Is(err, syscall.ECONNRESET) {
		t.Fatalf("client read error = %v, want connection reset", err)
	}
	if err := <-done; err == nil {
		t.Fatal("Relay returned nil after a failed direction")
	}
}
