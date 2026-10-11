package transport

import (
	"crypto/tls"
	"net"
	"testing"
	"time"
)

func TestDialerKeepAlive(t *testing.T) {
	tests := []struct {
		name string
		in   time.Duration
		want time.Duration
	}{
		{"negative disables", -5 * time.Second, -1},
		{"zero disables", 0, -1},
		{"positive passes through", 30 * time.Second, 30 * time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := dialerKeepAlive(tt.in); got != tt.want {
				t.Errorf("dialerKeepAlive(%v) = %v, want %v", tt.in, got, tt.want)
			}
			// keepaliveDialer must carry the normalized period.
			if d := keepaliveDialer(tt.in); d == nil || d.KeepAlive != tt.want {
				t.Errorf("keepaliveDialer(%v).KeepAlive = %v, want %v", tt.in, d.KeepAlive, tt.want)
			}
		})
	}
}

// acceptOne returns a server-side connection from a fresh loopback listener,
// with the dialed client connection kept open until cleanup.
func acceptOne(t *testing.T) net.Conn {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	clientCh := make(chan net.Conn, 1)
	go func() {
		c, _ := net.Dial("tcp", ln.Addr().String())
		clientCh <- c
	}()

	srv, err := ln.Accept()
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	t.Cleanup(func() { srv.Close() })

	client := <-clientCh
	if client == nil {
		t.Fatal("dial returned nil")
	}
	t.Cleanup(func() { client.Close() })

	return srv
}

func TestSetTCPKeepAlive(t *testing.T) {
	conn := acceptOne(t)

	// Enable, idempotent re-enable, then disable -- none should error.
	if err := setTCPKeepAlive(conn, 15*time.Second); err != nil {
		t.Errorf("setTCPKeepAlive enable: %v", err)
	}
	if err := setTCPKeepAlive(conn, 15*time.Second); err != nil {
		t.Errorf("setTCPKeepAlive idempotent: %v", err)
	}
	if err := setTCPKeepAlive(conn, 0); err != nil {
		t.Errorf("setTCPKeepAlive disable: %v", err)
	}

	// Non-TCP connections are a no-op (no panic, no error).
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	if err := setTCPKeepAlive(a, 15*time.Second); err != nil {
		t.Errorf("setTCPKeepAlive(non-tcp) = %v, want nil", err)
	}
}

func TestTCPConnOf(t *testing.T) {
	conn := acceptOne(t)

	// Direct *net.TCPConn unwraps to itself.
	if tcpConnOf(conn) == nil {
		t.Error("tcpConnOf(*net.TCPConn) = nil, want non-nil")
	}

	// A *tls.Conn wrapper unwraps to the underlying TCP conn (no handshake needed).
	tlsConn := tls.Client(conn, &tls.Config{InsecureSkipVerify: true})
	if tcpConnOf(tlsConn) == nil {
		t.Error("tcpConnOf(*tls.Conn) = nil, want non-nil")
	}

	// A non-TCP connection yields nil.
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	if tcpConnOf(a) != nil {
		t.Error("tcpConnOf(net.Pipe) != nil, want nil")
	}
}

func TestWrapKeepAliveListener(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	// period <= 0 returns the listener unchanged.
	if got := wrapKeepAliveListener(ln, 0); got != ln {
		t.Error("wrapKeepAliveListener(0) should return the original listener")
	}

	// period > 0 wraps the listener and still accepts connections.
	wrapped := wrapKeepAliveListener(ln, 10*time.Second)
	if _, ok := wrapped.(keepAliveListener); !ok {
		t.Fatalf("wrapKeepAliveListener(>0) = %T, want keepAliveListener", wrapped)
	}

	go func() {
		c, _ := net.Dial("tcp", ln.Addr().String())
		if c != nil {
			c.Close()
		}
	}()

	conn, err := wrapped.Accept()
	if err != nil {
		t.Fatalf("wrapped Accept: %v", err)
	}
	conn.Close()
}
