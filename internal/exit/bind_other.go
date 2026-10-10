//go:build !linux

package exit

import (
	"fmt"
	"runtime"
	"syscall"
)

// bindToDeviceControl on non-Linux platforms returns a callback that fails
// every dial with a clear error. The dial path only reaches it when
// canBindToDevice reports true, which it never does off Linux, so this stub is
// defense in depth -- it ensures a misconfigured cross-platform deployment
// fails loudly instead of silently ignoring the interface binding.
func bindToDeviceControl(iface string) func(network, address string, c syscall.RawConn) error {
	return func(network, address string, c syscall.RawConn) error {
		return fmt.Errorf("interface binding %q is only supported on linux (current GOOS=%s)", iface, runtime.GOOS)
	}
}

// canBindToDevice reports whether this process can pin an outbound socket to a
// network interface via SO_BINDTODEVICE. That syscall is Linux-only, so the
// answer off Linux is always false and the dial path uses portable bind-to-IP
// (net.Dialer.LocalAddr) instead.
func canBindToDevice() bool {
	return false
}
