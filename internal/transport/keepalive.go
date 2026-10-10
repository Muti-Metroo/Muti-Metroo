package transport

import (
	"net"
	"time"
)

// dialerKeepAlive maps a configured SO_KEEPALIVE period onto net.Dialer.KeepAlive
// semantics. net.Dialer treats a negative value as "disabled" and 0 as "OS
// default", so a configured 0 (disabled) is normalized to a negative sentinel
// and a positive period is passed through unchanged.
func dialerKeepAlive(period time.Duration) time.Duration {
	if period <= 0 {
		return -1
	}
	return period
}

// keepaliveDialer returns a *net.Dialer configured with the given SO_KEEPALIVE
// period. TCP-based transports use this so the keepalive normalization lives in
// one place and a future transport inherits it by calling this helper.
func keepaliveDialer(period time.Duration) *net.Dialer {
	return &net.Dialer{KeepAlive: dialerKeepAlive(period)}
}

// setTCPKeepAlive applies SO_KEEPALIVE to the TCP connection underlying conn.
// A positive period enables keepalive with that interval; 0 or negative disables
// it. Connections that do not wrap a *net.TCPConn are left untouched.
func setTCPKeepAlive(conn net.Conn, period time.Duration) error {
	tcp := tcpConnOf(conn)
	if tcp == nil {
		return nil
	}
	if period <= 0 {
		return tcp.SetKeepAlive(false)
	}
	if err := tcp.SetKeepAlive(true); err != nil {
		return err
	}
	return tcp.SetKeepAlivePeriod(period)
}

// keepAliveListener wraps a net.Listener so every accepted connection gets
// SO_KEEPALIVE applied before it is handed to the server.
type keepAliveListener struct {
	net.Listener
	period time.Duration
}

func (l keepAliveListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	_ = setTCPKeepAlive(conn, l.period)
	return conn, nil
}

// wrapKeepAliveListener returns ln wrapped so accepted TCP connections get the
// given SO_KEEPALIVE period. When period <= 0, ln is returned unchanged.
func wrapKeepAliveListener(ln net.Listener, period time.Duration) net.Listener {
	if period <= 0 {
		return ln
	}
	return keepAliveListener{Listener: ln, period: period}
}

// tcpConnOf unwraps conn to its underlying *net.TCPConn, peeling any layer that
// exposes the connection it wraps via a NetConn() accessor (e.g. *tls.Conn,
// utls.UConn). Returns nil if no TCP connection is found.
func tcpConnOf(conn net.Conn) *net.TCPConn {
	switch c := conn.(type) {
	case *net.TCPConn:
		return c
	case interface{ NetConn() net.Conn }:
		inner := c.NetConn()
		if inner == nil || inner == conn {
			return nil
		}
		return tcpConnOf(inner)
	}
	return nil
}
