package exit

import (
	"fmt"
	"net"
)

// bindingPlan is the resolved decision for how a single outbound dial should
// bind its local end. localAddr, when non-nil, is set as net.Dialer.LocalAddr.
// useDevice, when true, pins the egress NIC via net.Dialer.Control =
// bindToDeviceControl(iface) (Linux + root only).
type bindingPlan struct {
	localAddr net.IP
	useDevice bool
}

// computeBindingPlan decides how to bind an outbound dial given the route's
// source IP (src, may be nil) and egress interface (iface, may be empty), the
// destination address family (destIsV4), and the runtime's binding capability.
//
// The deps are injected so the dial path is unit-testable without touching the
// host's real interfaces:
//   - canDevice: whether SO_BINDTODEVICE is usable (Linux + root).
//   - hasAddr: reports whether an IP is assigned to an interface; its second
//     result is false when the interface cannot be enumerated.
//   - pickSource: chooses a family-matched source address on an interface.
//
// Behavior tiers:
//   - No interface: bind the source IP if set (unchanged legacy behavior).
//   - Interface + canDevice (Linux + root): true device binding via
//     SO_BINDTODEVICE, which handles both a matching and an off-interface
//     (loopback) source.
//   - Interface, portable (non-root / non-Linux): bind to an address on the
//     interface. With an explicit source we require it to be assigned to the
//     interface (or the interface to be currently unresolvable, in which case
//     we defer to the OS at dial time); an off-interface source is rejected
//     because only the privileged path can honor it. With no source we pick a
//     usable address off the interface.
func computeBindingPlan(
	src net.IP,
	iface string,
	destIsV4 bool,
	canDevice bool,
	hasAddr func(name string, ip net.IP) (has bool, resolved bool),
	pickSource func(name string, wantV4 bool) net.IP,
) (bindingPlan, error) {
	if iface == "" {
		return bindingPlan{localAddr: src}, nil
	}

	if canDevice {
		return bindingPlan{localAddr: src, useDevice: true}, nil
	}

	if src != nil {
		has, resolved := hasAddr(iface, src)
		if has || !resolved {
			return bindingPlan{localAddr: src}, nil
		}
		return bindingPlan{}, fmt.Errorf("source IP %s is not assigned to interface %q; an off-interface source requires Linux + root (SO_BINDTODEVICE)", src, iface)
	}

	addr := pickSource(iface, destIsV4)
	if addr == nil {
		fam := "IPv6"
		if destIsV4 {
			fam = "IPv4"
		}
		return bindingPlan{}, fmt.Errorf("interface %q has no usable %s address to bind as source", iface, fam)
	}
	return bindingPlan{localAddr: addr}, nil
}
