package exit

import (
	"net"
	"strings"
	"testing"
)

func TestComputeBindingPlan(t *testing.T) {
	srcOnIface := net.ParseIP("192.168.1.50")
	srcOffIface := net.ParseIP("10.9.9.9")
	picked := net.ParseIP("192.168.1.1")

	// hasAddr factory: reports the given (has, resolved) regardless of args.
	hasAddrFn := func(has, resolved bool) func(string, net.IP) (bool, bool) {
		return func(string, net.IP) (bool, bool) { return has, resolved }
	}
	pickFn := func(ret net.IP) func(string, bool) net.IP {
		return func(string, bool) net.IP { return ret }
	}

	tests := []struct {
		name       string
		src        net.IP
		iface      string
		destIsV4   bool
		canDevice  bool
		hasAddr    func(string, net.IP) (bool, bool)
		pickSource func(string, bool) net.IP
		wantLocal  net.IP
		wantDevice bool
		wantErr    string
	}{
		{
			name:      "no interface, no source",
			iface:     "",
			wantLocal: nil,
		},
		{
			name:      "no interface, with source",
			src:       srcOnIface,
			iface:     "",
			wantLocal: srcOnIface,
		},
		{
			name:       "device-capable, no source: device binding only",
			iface:      "eth0",
			canDevice:  true,
			wantLocal:  nil,
			wantDevice: true,
		},
		{
			name:       "device-capable, with source: source + device binding",
			src:        srcOffIface,
			iface:      "eth0",
			canDevice:  true,
			wantLocal:  srcOffIface,
			wantDevice: true,
		},
		{
			name:      "portable, source on interface: bind source",
			src:       srcOnIface,
			iface:     "eth0",
			hasAddr:   hasAddrFn(true, true),
			wantLocal: srcOnIface,
		},
		{
			name:      "portable, interface unresolvable: defer by binding source",
			src:       srcOnIface,
			iface:     "eth0",
			hasAddr:   hasAddrFn(false, false),
			wantLocal: srcOnIface,
		},
		{
			name:    "portable, off-interface source: rejected",
			src:     srcOffIface,
			iface:   "eth0",
			hasAddr: hasAddrFn(false, true),
			wantErr: "off-interface source requires Linux + root",
		},
		{
			name:       "portable, no source: pick interface address",
			iface:      "eth0",
			destIsV4:   true,
			pickSource: pickFn(picked),
			wantLocal:  picked,
		},
		{
			name:       "portable, no source, no usable address: rejected",
			iface:      "eth0",
			destIsV4:   true,
			pickSource: pickFn(nil),
			wantErr:    "no usable IPv4 address",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan, err := computeBindingPlan(tt.src, tt.iface, tt.destIsV4, tt.canDevice, tt.hasAddr, tt.pickSource)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want substring %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if !plan.localAddr.Equal(tt.wantLocal) {
				t.Errorf("localAddr = %v, want %v", plan.localAddr, tt.wantLocal)
			}
			if plan.useDevice != tt.wantDevice {
				t.Errorf("useDevice = %v, want %v", plan.useDevice, tt.wantDevice)
			}
		})
	}
}
