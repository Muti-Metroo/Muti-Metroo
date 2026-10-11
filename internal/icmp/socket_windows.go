//go:build windows

package icmp

import "errors"

// ErrNotSupported is returned by NewSocketV4/NewSocketV6 on Windows because
// Windows does not provide unprivileged ICMP sockets. Callers should surface
// this to users as "ICMP is not supported on Windows".
var ErrNotSupported = errors.New("ICMP: unprivileged ICMP sockets are not supported on Windows")

// NewSocketV4 always returns ErrNotSupported on Windows.
func NewSocketV4() (*Socket, error) {
	return nil, ErrNotSupported
}

// NewSocketV6 always returns ErrNotSupported on Windows.
func NewSocketV6() (*Socket, error) {
	return nil, ErrNotSupported
}
