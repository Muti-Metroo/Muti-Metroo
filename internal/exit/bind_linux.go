//go:build linux

package exit

import (
	"fmt"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

// canBindToDevice reports whether this process can pin an outbound socket to a
// network interface via SO_BINDTODEVICE. On Linux that syscall is privileged,
// so the answer is "only when running as root". When it returns false the dial
// path falls back to portable bind-to-IP (net.Dialer.LocalAddr), which works
// for an unset source or a source already assigned to the interface but cannot
// honor an off-interface source (the loopback-over-NIC case).
func canBindToDevice() bool {
	return os.Geteuid() == 0
}

// bindToDeviceControl returns a net.Dialer.Control callback that pins the
// outbound socket to the given network interface via SO_BINDTODEVICE and
// enables IP_FREEBIND / IPV6_FREEBIND so bind() can succeed against a
// source IP that isn't assigned to that interface (or isn't assigned to any
// interface at all). The common case this unlocks: a BGP-announced or
// anycast source IP that lives on lo while egress packets must leave via a
// physical NIC like ens192. Without IP_FREEBIND, bind(lo-ip) fails with
// EADDRNOTAVAIL once SO_BINDTODEVICE has pinned the socket to ens192 -- the
// IP isn't on ens192, so the kernel rejects the bind.
//
// SO_BINDTODEVICE requires CAP_NET_RAW (or root); failures propagate as
// typed net.OpErrors that the existing mapDialError logic classifies.
// FREEBIND failures are best-effort and ignored: on a kernel that doesn't
// support the option, or the wrong address family, bind() will still
// succeed for a normally-assigned source IP.
func bindToDeviceControl(iface string) func(network, address string, c syscall.RawConn) error {
	return func(network, address string, c syscall.RawConn) error {
		var sockErr error
		if err := c.Control(func(fd uintptr) {
			if sockErr = unix.SetsockoptString(int(fd), unix.SOL_SOCKET, unix.SO_BINDTODEVICE, iface); sockErr != nil {
				return
			}
			// IP_FREEBIND for v4 sockets, IPV6_FREEBIND for v6. "tcp"
			// (unresolved family) attempts both. Errors are intentionally
			// dropped: a kernel without these options still lets bind()
			// succeed for a locally-assigned source IP.
			switch network {
			case "tcp4":
				_ = unix.SetsockoptInt(int(fd), unix.IPPROTO_IP, unix.IP_FREEBIND, 1)
			case "tcp6":
				_ = unix.SetsockoptInt(int(fd), unix.IPPROTO_IPV6, unix.IPV6_FREEBIND, 1)
			default:
				_ = unix.SetsockoptInt(int(fd), unix.IPPROTO_IP, unix.IP_FREEBIND, 1)
				_ = unix.SetsockoptInt(int(fd), unix.IPPROTO_IPV6, unix.IPV6_FREEBIND, 1)
			}
		}); err != nil {
			return err
		}
		if sockErr != nil {
			return fmt.Errorf("SO_BINDTODEVICE %q: %w", iface, sockErr)
		}
		return nil
	}
}
