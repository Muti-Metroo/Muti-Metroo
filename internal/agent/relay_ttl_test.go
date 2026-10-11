package agent

import (
	"testing"

	"github.com/Muti-Metroo/Muti-Metroo/internal/identity"
	"github.com/Muti-Metroo/Muti-Metroo/internal/protocol"
)

// findFrameByType returns the first captured frame of the given type,
// regardless of stream ID (forwarded opens get a freshly allocated ID).
func findFrameByType(frames []*protocol.Frame, frameType uint8) *protocol.Frame {
	for _, f := range frames {
		if f.Type == frameType {
			return f
		}
	}
	return nil
}

// TestAgent_StreamOpenTTL_DecrementedPerHop verifies that a transit hop
// forwards STREAM_OPEN with TTL decremented by one and the next hop popped
// from the remaining path.
func TestAgent_StreamOpenTTL_DecrementedPerHop(t *testing.T) {
	a := newRelayTestAgent(t)

	upID, _ := identity.NewAgentID()
	downID, _ := identity.NewAgentID()
	registerRelayTestPeer(t, a, upID)
	downStream := registerRelayTestPeer(t, a, downID)

	open := &protocol.StreamOpen{
		RequestID:     42,
		AddressType:   protocol.AddrTypeIPv4,
		Address:       []byte{10, 0, 0, 1},
		Port:          443,
		TTL:           3,
		RemainingPath: []identity.AgentID{downID},
	}
	a.processFrame(upID, &protocol.Frame{
		Type:     protocol.FrameStreamOpen,
		StreamID: 11,
		Payload:  open.Encode(),
	})

	fwd := findFrameByType(downStream.framesWritten(t), protocol.FrameStreamOpen)
	if fwd == nil {
		t.Fatal("downstream peer should receive the forwarded STREAM_OPEN")
	}
	fwdOpen, err := protocol.DecodeStreamOpen(fwd.Payload)
	if err != nil {
		t.Fatalf("DecodeStreamOpen() error = %v", err)
	}
	if fwdOpen.TTL != 2 {
		t.Errorf("forwarded TTL = %d, want 2 (decremented from 3)", fwdOpen.TTL)
	}
	if len(fwdOpen.RemainingPath) != 0 {
		t.Errorf("forwarded RemainingPath length = %d, want 0", len(fwdOpen.RemainingPath))
	}
	if fwdOpen.RequestID != open.RequestID {
		t.Errorf("forwarded RequestID = %d, want %d", fwdOpen.RequestID, open.RequestID)
	}
}

// TestAgent_StreamOpenTTL_ZeroRejected verifies that a transit hop rejects a
// STREAM_OPEN whose TTL has reached zero with ErrTTLExceeded instead of
// forwarding it (loop protection).
func TestAgent_StreamOpenTTL_ZeroRejected(t *testing.T) {
	a := newRelayTestAgent(t)

	upID, _ := identity.NewAgentID()
	downID, _ := identity.NewAgentID()
	upStream := registerRelayTestPeer(t, a, upID)
	downStream := registerRelayTestPeer(t, a, downID)

	open := &protocol.StreamOpen{
		RequestID:     42,
		AddressType:   protocol.AddrTypeIPv4,
		Address:       []byte{10, 0, 0, 1},
		Port:          443,
		TTL:           0,
		RemainingPath: []identity.AgentID{downID},
	}
	a.processFrame(upID, &protocol.Frame{
		Type:     protocol.FrameStreamOpen,
		StreamID: 11,
		Payload:  open.Encode(),
	})

	errFrame := findFrame(upStream.framesWritten(t), protocol.FrameStreamOpenErr, 11)
	if errFrame == nil {
		t.Fatal("upstream peer should receive STREAM_OPEN_ERR for an exhausted TTL")
	}
	openErr, err := protocol.DecodeStreamOpenErr(errFrame.Payload)
	if err != nil {
		t.Fatalf("DecodeStreamOpenErr() error = %v", err)
	}
	if openErr.ErrorCode != protocol.ErrTTLExceeded {
		t.Errorf("error code = %d, want %d (ErrTTLExceeded)", openErr.ErrorCode, protocol.ErrTTLExceeded)
	}
	if f := findFrameByType(downStream.framesWritten(t), protocol.FrameStreamOpen); f != nil {
		t.Error("downstream peer should not receive a STREAM_OPEN with exhausted TTL")
	}
	if e, _ := a.tcpRelay.LookupBoth(upID, 11); e != nil {
		t.Error("no relay entry should be created for a rejected open")
	}
}

// TestAgent_UDPOpenTTL_DecrementedPerHop verifies the UDP transit hop
// decrements TTL when relaying UDP_OPEN.
func TestAgent_UDPOpenTTL_DecrementedPerHop(t *testing.T) {
	a := newRelayTestAgent(t)

	upID, _ := identity.NewAgentID()
	downID, _ := identity.NewAgentID()
	registerRelayTestPeer(t, a, upID)
	downStream := registerRelayTestPeer(t, a, downID)

	open := &protocol.UDPOpen{
		RequestID:     42,
		AddressType:   protocol.AddrTypeIPv4,
		Address:       []byte{0, 0, 0, 0},
		Port:          0,
		TTL:           3,
		RemainingPath: []identity.AgentID{downID},
	}
	a.processFrame(upID, &protocol.Frame{
		Type:     protocol.FrameUDPOpen,
		StreamID: 33,
		Payload:  open.Encode(),
	})

	fwd := findFrameByType(downStream.framesWritten(t), protocol.FrameUDPOpen)
	if fwd == nil {
		t.Fatal("downstream peer should receive the forwarded UDP_OPEN")
	}
	fwdOpen, err := protocol.DecodeUDPOpen(fwd.Payload)
	if err != nil {
		t.Fatalf("DecodeUDPOpen() error = %v", err)
	}
	if fwdOpen.TTL != 2 {
		t.Errorf("forwarded TTL = %d, want 2 (decremented from 3)", fwdOpen.TTL)
	}
}

// TestAgent_UDPOpenTTL_ZeroRejected verifies that a transit hop rejects a
// UDP_OPEN whose TTL has reached zero with ErrTTLExceeded.
func TestAgent_UDPOpenTTL_ZeroRejected(t *testing.T) {
	a := newRelayTestAgent(t)

	upID, _ := identity.NewAgentID()
	downID, _ := identity.NewAgentID()
	upStream := registerRelayTestPeer(t, a, upID)
	downStream := registerRelayTestPeer(t, a, downID)

	open := &protocol.UDPOpen{
		RequestID:     42,
		AddressType:   protocol.AddrTypeIPv4,
		Address:       []byte{0, 0, 0, 0},
		TTL:           0,
		RemainingPath: []identity.AgentID{downID},
	}
	a.processFrame(upID, &protocol.Frame{
		Type:     protocol.FrameUDPOpen,
		StreamID: 33,
		Payload:  open.Encode(),
	})

	errFrame := findFrame(upStream.framesWritten(t), protocol.FrameUDPOpenErr, 33)
	if errFrame == nil {
		t.Fatal("upstream peer should receive UDP_OPEN_ERR for an exhausted TTL")
	}
	openErr, err := protocol.DecodeUDPOpenErr(errFrame.Payload)
	if err != nil {
		t.Fatalf("DecodeUDPOpenErr() error = %v", err)
	}
	if openErr.ErrorCode != protocol.ErrTTLExceeded {
		t.Errorf("error code = %d, want %d (ErrTTLExceeded)", openErr.ErrorCode, protocol.ErrTTLExceeded)
	}
	if f := findFrameByType(downStream.framesWritten(t), protocol.FrameUDPOpen); f != nil {
		t.Error("downstream peer should not receive a UDP_OPEN with exhausted TTL")
	}
}

// TestAgent_ICMPOpenTTL_DecrementedPerHop verifies the ICMP transit hop
// decrements TTL when relaying ICMP_OPEN.
func TestAgent_ICMPOpenTTL_DecrementedPerHop(t *testing.T) {
	a := newRelayTestAgent(t)

	upID, _ := identity.NewAgentID()
	downID, _ := identity.NewAgentID()
	registerRelayTestPeer(t, a, upID)
	downStream := registerRelayTestPeer(t, a, downID)

	open := &protocol.ICMPOpen{
		RequestID:     42,
		DestIP:        []byte{10, 0, 0, 1},
		TTL:           3,
		RemainingPath: []identity.AgentID{downID},
	}
	a.processFrame(upID, &protocol.Frame{
		Type:     protocol.FrameICMPOpen,
		StreamID: 55,
		Payload:  open.Encode(),
	})

	fwd := findFrameByType(downStream.framesWritten(t), protocol.FrameICMPOpen)
	if fwd == nil {
		t.Fatal("downstream peer should receive the forwarded ICMP_OPEN")
	}
	fwdOpen, err := protocol.DecodeICMPOpen(fwd.Payload)
	if err != nil {
		t.Fatalf("DecodeICMPOpen() error = %v", err)
	}
	if fwdOpen.TTL != 2 {
		t.Errorf("forwarded TTL = %d, want 2 (decremented from 3)", fwdOpen.TTL)
	}
}

// TestAgent_ICMPOpenTTL_ZeroRejected verifies that a transit hop rejects an
// ICMP_OPEN whose TTL has reached zero with ErrTTLExceeded.
func TestAgent_ICMPOpenTTL_ZeroRejected(t *testing.T) {
	a := newRelayTestAgent(t)

	upID, _ := identity.NewAgentID()
	downID, _ := identity.NewAgentID()
	upStream := registerRelayTestPeer(t, a, upID)
	downStream := registerRelayTestPeer(t, a, downID)

	open := &protocol.ICMPOpen{
		RequestID:     42,
		DestIP:        []byte{10, 0, 0, 1},
		TTL:           0,
		RemainingPath: []identity.AgentID{downID},
	}
	a.processFrame(upID, &protocol.Frame{
		Type:     protocol.FrameICMPOpen,
		StreamID: 55,
		Payload:  open.Encode(),
	})

	errFrame := findFrame(upStream.framesWritten(t), protocol.FrameICMPOpenErr, 55)
	if errFrame == nil {
		t.Fatal("upstream peer should receive ICMP_OPEN_ERR for an exhausted TTL")
	}
	openErr, err := protocol.DecodeICMPOpenErr(errFrame.Payload)
	if err != nil {
		t.Fatalf("DecodeICMPOpenErr() error = %v", err)
	}
	if openErr.ErrorCode != protocol.ErrTTLExceeded {
		t.Errorf("error code = %d, want %d (ErrTTLExceeded)", openErr.ErrorCode, protocol.ErrTTLExceeded)
	}
	if f := findFrameByType(downStream.framesWritten(t), protocol.FrameICMPOpen); f != nil {
		t.Error("downstream peer should not receive an ICMP_OPEN with exhausted TTL")
	}
}
