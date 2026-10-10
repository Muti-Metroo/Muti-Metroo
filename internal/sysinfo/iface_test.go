package sysinfo

import (
	"net"
	"testing"
)

func loopbackName(t *testing.T) string {
	t.Helper()
	ifaces, err := net.Interfaces()
	if err != nil {
		t.Skipf("net.Interfaces: %v", err)
	}
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagLoopback != 0 {
			return ifc.Name
		}
	}
	t.Skip("no loopback interface found")
	return ""
}

func TestInterfaceHasAddress(t *testing.T) {
	lo := loopbackName(t)

	if has, resolved := InterfaceHasAddress(lo, net.ParseIP("127.0.0.1")); !resolved || !has {
		t.Errorf("InterfaceHasAddress(%q, 127.0.0.1) = (has=%v, resolved=%v), want (true, true)", lo, has, resolved)
	}

	// TEST-NET-1, never assigned to loopback: resolved but not present.
	if has, resolved := InterfaceHasAddress(lo, net.ParseIP("192.0.2.1")); !resolved || has {
		t.Errorf("InterfaceHasAddress(%q, 192.0.2.1) = (has=%v, resolved=%v), want (false, true)", lo, has, resolved)
	}

	// A non-existent interface cannot be enumerated: resolved=false.
	if has, resolved := InterfaceHasAddress("muti-metroo-no-such-iface-xyz", net.ParseIP("127.0.0.1")); resolved || has {
		t.Errorf("InterfaceHasAddress(absent) = (has=%v, resolved=%v), want (false, false)", has, resolved)
	}
}

func TestPickInterfaceSource(t *testing.T) {
	lo := loopbackName(t)

	// Loopback always has an IPv4 address; PickInterfaceSource falls back to
	// it when no global address is present.
	ip := PickInterfaceSource(lo, true)
	if ip == nil {
		t.Fatalf("PickInterfaceSource(%q, v4) = nil, want a loopback v4 address", lo)
	}
	if ip.To4() == nil {
		t.Errorf("PickInterfaceSource(%q, v4) = %v, want an IPv4 address", lo, ip)
	}

	// An interface with no matching-family address yields nil.
	if got := PickInterfaceSource("muti-metroo-no-such-iface-xyz", true); got != nil {
		t.Errorf("PickInterfaceSource(absent) = %v, want nil", got)
	}
}
