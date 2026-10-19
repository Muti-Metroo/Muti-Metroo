// Package netutil holds connection helpers shared by the ingress paths
// (SOCKS5 and port-forward listeners).
package netutil

import (
	"io"
	"net"
)

// halfCloser is implemented by connections that support half-close (TCP,
// mesh connections).
type halfCloser interface {
	CloseWrite() error
}

// wrapper is implemented by connection wrappers that expose the connection
// they wrap, like tls.Conn.
type wrapper interface {
	NetConn() net.Conn
}

// Relay copies data in both directions between client and target until both
// directions are done, and returns the first error. A clean end of one
// direction half-closes the other side when it supports that; a failed one
// (for example a reset mesh stream) aborts both connections, so the client
// sees an error instead of a truncated stream that looks complete.
func Relay(client, target net.Conn) error {
	errCh := make(chan error, 2)

	pipe := func(dst, src net.Conn) {
		_, err := io.Copy(dst, src)
		if err != nil {
			Abort(dst)
			src.Close()
		} else if hc, ok := dst.(halfCloser); ok {
			hc.CloseWrite()
		}
		errCh <- err
	}
	go pipe(target, client)
	go pipe(client, target)

	err1 := <-errCh
	err2 := <-errCh
	if err1 != nil {
		return err1
	}
	return err2
}

// Abort closes conn so the peer sees a failure: a TCP connection, also one
// behind a wrapper, is reset (RST) rather than closed with FIN.
func Abort(conn net.Conn) {
	inner := conn
	for {
		w, ok := inner.(wrapper)
		if !ok {
			break
		}
		inner = w.NetConn()
	}
	if tc, ok := inner.(*net.TCPConn); ok {
		tc.SetLinger(0)
	}
	conn.Close()
}
