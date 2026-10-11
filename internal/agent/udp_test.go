package agent

import (
	"errors"
	"testing"

	"github.com/Muti-Metroo/Muti-Metroo/internal/logging"
	"github.com/Muti-Metroo/Muti-Metroo/internal/protocol"
)

// Oversize datagrams must be rejected at the ingress against
// protocol.MaxUDPDatagramSize, not silently dropped at the exit where the
// sender cannot diagnose the loss.
func TestRelayUDPDatagram_RejectsOversize(t *testing.T) {
	a := &Agent{logger: logging.NopLogger()}

	oversize := make([]byte, protocol.MaxUDPDatagramSize+1)
	err := a.RelayUDPDatagram(1, nil, 53, protocol.AddrTypeIPv4, []byte{10, 0, 0, 1}, oversize)
	if !errors.Is(err, ErrUDPDatagramTooLarge) {
		t.Errorf("RelayUDPDatagram(oversize) = %v, want ErrUDPDatagramTooLarge", err)
	}
	if a.udpOversizeDrops.Load() != 1 {
		t.Errorf("udpOversizeDrops = %d, want 1", a.udpOversizeDrops.Load())
	}

	// A datagram at the limit passes the size gate and fails later (unknown
	// stream), proving the gate is size-specific.
	ok := make([]byte, protocol.MaxUDPDatagramSize)
	err = a.RelayUDPDatagram(1, nil, 53, protocol.AddrTypeIPv4, []byte{10, 0, 0, 1}, ok)
	if !errors.Is(err, ErrUDPStreamNotFound) {
		t.Errorf("RelayUDPDatagram(max size) = %v, want ErrUDPStreamNotFound", err)
	}
}
