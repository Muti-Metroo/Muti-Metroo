package agent

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/postalsys/muti-metroo/internal/config"
	"github.com/postalsys/muti-metroo/internal/identity"
	"github.com/postalsys/muti-metroo/internal/protocol"
	"github.com/postalsys/muti-metroo/internal/transport"
)

// relayTestStream is a scripted control stream for relay tests: Read first
// serves the preloaded PEER_HELLO frame bytes (so the manager's Accept
// handshake completes), then blocks until Close. Write captures outbound
// frames, or fails when failWrites is set.
type relayTestStream struct {
	hello *bytes.Reader

	mu         sync.Mutex
	written    bytes.Buffer
	failWrites atomic.Bool

	closed    chan struct{}
	closeOnce sync.Once
}

func newRelayTestStream(remoteID identity.AgentID) *relayTestStream {
	hello := &protocol.PeerHello{
		Version:   protocol.ProtocolVersion,
		AgentID:   remoteID,
		Timestamp: uint64(time.Now().UnixNano()),
	}
	var buf bytes.Buffer
	if err := protocol.NewFrameWriter(&buf).Write(&protocol.Frame{
		Type:     protocol.FramePeerHello,
		StreamID: protocol.ControlStreamID,
		Payload:  hello.Encode(),
	}); err != nil {
		panic(err)
	}
	return &relayTestStream{
		hello:  bytes.NewReader(buf.Bytes()),
		closed: make(chan struct{}),
	}
}

func (s *relayTestStream) StreamID() uint64 { return 0 }

func (s *relayTestStream) Read(p []byte) (int, error) {
	if s.hello.Len() > 0 {
		return s.hello.Read(p)
	}
	<-s.closed
	return 0, io.EOF
}

func (s *relayTestStream) Write(p []byte) (int, error) {
	if s.failWrites.Load() {
		return 0, errors.New("write failed")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.written.Write(p)
}

func (s *relayTestStream) CloseWrite() error { return nil }

func (s *relayTestStream) Close() error {
	s.closeOnce.Do(func() { close(s.closed) })
	return nil
}

func (s *relayTestStream) SetDeadline(t time.Time) error      { return nil }
func (s *relayTestStream) SetReadDeadline(t time.Time) error  { return nil }
func (s *relayTestStream) SetWriteDeadline(t time.Time) error { return nil }

// framesWritten parses all complete frames captured by Write.
func (s *relayTestStream) framesWritten(t *testing.T) []*protocol.Frame {
	t.Helper()
	s.mu.Lock()
	data := append([]byte(nil), s.written.Bytes()...)
	s.mu.Unlock()

	var frames []*protocol.Frame
	reader := protocol.NewFrameReader(bytes.NewReader(data))
	for {
		frame, err := reader.Read()
		if err != nil {
			break
		}
		frames = append(frames, frame)
	}
	return frames
}

// relayTestPeerConn adapts relayTestStream to transport.PeerConn.
type relayTestPeerConn struct {
	stream *relayTestStream
}

func (c *relayTestPeerConn) OpenStream(ctx context.Context) (transport.Stream, error) {
	return c.stream, nil
}

func (c *relayTestPeerConn) AcceptStream(ctx context.Context) (transport.Stream, error) {
	return c.stream, nil
}

func (c *relayTestPeerConn) Close() error                           { return c.stream.Close() }
func (c *relayTestPeerConn) LocalAddr() net.Addr                    { return nil }
func (c *relayTestPeerConn) RemoteAddr() net.Addr                   { return nil }
func (c *relayTestPeerConn) IsDialer() bool                         { return false }
func (c *relayTestPeerConn) TransportType() transport.TransportType { return transport.TransportQUIC }

// registerRelayTestPeer handshakes a scripted peer connection through the
// agent's peer manager and returns its stream for write capture/control.
func registerRelayTestPeer(t *testing.T, a *Agent, remoteID identity.AgentID) *relayTestStream {
	t.Helper()
	stream := newRelayTestStream(remoteID)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := a.peerMgr.Accept(ctx, &relayTestPeerConn{stream: stream}); err != nil {
		t.Fatalf("Accept() error = %v", err)
	}
	t.Cleanup(func() { stream.Close() })
	return stream
}

func newRelayTestAgent(t *testing.T) *Agent {
	t.Helper()
	cfg := config.Default()
	cfg.Agent.DataDir = t.TempDir()
	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return a
}

// findFrame returns the first captured frame of the given type and stream ID.
func findFrame(frames []*protocol.Frame, frameType uint8, streamID uint64) *protocol.Frame {
	for _, f := range frames {
		if f.Type == frameType && f.StreamID == streamID {
			return f
		}
	}
	return nil
}

// TestAgent_RelayDataForwardFailure_ResetsBothWays verifies that a transit
// relay whose downstream write fails pops the relay entry and sends
// STREAM_RESET to both ends instead of silently dropping the frame (and every
// later frame of the stream).
func TestAgent_RelayDataForwardFailure_ResetsBothWays(t *testing.T) {
	a := newRelayTestAgent(t)

	upID, _ := identity.NewAgentID()
	downID, _ := identity.NewAgentID()
	upStream := registerRelayTestPeer(t, a, upID)
	downStream := registerRelayTestPeer(t, a, downID)
	downStream.failWrites.Store(true)

	a.tcpRelay.Insert(&relayEntry{
		UpstreamPeer: upID, UpstreamID: 11,
		DownstreamPeer: downID, DownstreamID: 22,
	})

	a.processFrame(upID, &protocol.Frame{
		Type:     protocol.FrameStreamData,
		StreamID: 11,
		Payload:  []byte("payload"),
	})

	if up, down := a.tcpRelay.LookupBoth(upID, 11); up != nil || down != nil {
		t.Fatal("relay entry should be popped after a forward failure")
	}

	reset := findFrame(upStream.framesWritten(t), protocol.FrameStreamReset, 11)
	if reset == nil {
		t.Fatal("upstream peer should receive STREAM_RESET for the failed relay")
	}
	payload, err := protocol.DecodeStreamReset(reset.Payload)
	if err != nil {
		t.Fatalf("DecodeStreamReset() error = %v", err)
	}
	if payload.ErrorCode != protocol.ErrWriteFailed {
		t.Errorf("reset error code = %d, want %d (ErrWriteFailed)", payload.ErrorCode, protocol.ErrWriteFailed)
	}
}

// TestAgent_RelayDataForwardFailure_DownstreamDirection covers the reverse
// direction: data from the downstream side whose upstream write fails.
func TestAgent_RelayDataForwardFailure_DownstreamDirection(t *testing.T) {
	a := newRelayTestAgent(t)

	upID, _ := identity.NewAgentID()
	downID, _ := identity.NewAgentID()
	upStream := registerRelayTestPeer(t, a, upID)
	upStream.failWrites.Store(true)
	downStream := registerRelayTestPeer(t, a, downID)

	a.tcpRelay.Insert(&relayEntry{
		UpstreamPeer: upID, UpstreamID: 11,
		DownstreamPeer: downID, DownstreamID: 22,
	})

	a.processFrame(downID, &protocol.Frame{
		Type:     protocol.FrameStreamData,
		StreamID: 22,
		Payload:  []byte("payload"),
	})

	if up, down := a.tcpRelay.LookupBoth(downID, 22); up != nil || down != nil {
		t.Fatal("relay entry should be popped after a forward failure")
	}

	reset := findFrame(downStream.framesWritten(t), protocol.FrameStreamReset, 22)
	if reset == nil {
		t.Fatal("downstream peer should receive STREAM_RESET for the failed relay")
	}
}

// TestAgent_UDPRelayForwardFailure_ClosesBothWays verifies the UDP relay path:
// a failed datagram forward pops the entry and closes the association on both
// sides with UDP_CLOSE.
func TestAgent_UDPRelayForwardFailure_ClosesBothWays(t *testing.T) {
	a := newRelayTestAgent(t)

	upID, _ := identity.NewAgentID()
	downID, _ := identity.NewAgentID()
	upStream := registerRelayTestPeer(t, a, upID)
	downStream := registerRelayTestPeer(t, a, downID)
	downStream.failWrites.Store(true)

	a.udpRelay.Insert(&relayEntry{
		UpstreamPeer: upID, UpstreamID: 33,
		DownstreamPeer: downID, DownstreamID: 44,
	})

	a.processFrame(upID, &protocol.Frame{
		Type:     protocol.FrameUDPDatagram,
		StreamID: 33,
		Payload:  []byte("datagram"),
	})

	if up, down := a.udpRelay.LookupBoth(upID, 33); up != nil || down != nil {
		t.Fatal("udp relay entry should be popped after a forward failure")
	}

	if findFrame(upStream.framesWritten(t), protocol.FrameUDPClose, 33) == nil {
		t.Fatal("upstream peer should receive UDP_CLOSE for the failed relay")
	}
}

// TestAgent_ICMPRelayForwardFailure_ClosesBothWays verifies the ICMP relay
// path: a failed echo forward pops the entry and closes the session on both
// sides with ICMP_CLOSE.
func TestAgent_ICMPRelayForwardFailure_ClosesBothWays(t *testing.T) {
	a := newRelayTestAgent(t)

	upID, _ := identity.NewAgentID()
	downID, _ := identity.NewAgentID()
	upStream := registerRelayTestPeer(t, a, upID)
	downStream := registerRelayTestPeer(t, a, downID)
	downStream.failWrites.Store(true)

	a.icmpRelay.Insert(&relayEntry{
		UpstreamPeer: upID, UpstreamID: 55,
		DownstreamPeer: downID, DownstreamID: 66,
	})

	a.processFrame(upID, &protocol.Frame{
		Type:     protocol.FrameICMPEcho,
		StreamID: 55,
		Payload:  []byte("echo"),
	})

	if up, down := a.icmpRelay.LookupBoth(upID, 55); up != nil || down != nil {
		t.Fatal("icmp relay entry should be popped after a forward failure")
	}

	if findFrame(upStream.framesWritten(t), protocol.FrameICMPClose, 55) == nil {
		t.Fatal("upstream peer should receive ICMP_CLOSE for the failed relay")
	}
}

// TestAgent_RelayForwardFailure_HealthyRelayUnaffected verifies the happy
// path still forwards without resets.
func TestAgent_RelayForwardFailure_HealthyRelayUnaffected(t *testing.T) {
	a := newRelayTestAgent(t)

	upID, _ := identity.NewAgentID()
	downID, _ := identity.NewAgentID()
	upStream := registerRelayTestPeer(t, a, upID)
	downStream := registerRelayTestPeer(t, a, downID)

	a.tcpRelay.Insert(&relayEntry{
		UpstreamPeer: upID, UpstreamID: 11,
		DownstreamPeer: downID, DownstreamID: 22,
	})

	a.processFrame(upID, &protocol.Frame{
		Type:     protocol.FrameStreamData,
		StreamID: 11,
		Payload:  []byte("payload"),
	})

	if up, _ := a.tcpRelay.LookupBoth(upID, 11); up == nil {
		t.Fatal("relay entry should survive a successful forward")
	}
	data := findFrame(downStream.framesWritten(t), protocol.FrameStreamData, 22)
	if data == nil || !bytes.Equal(data.Payload, []byte("payload")) {
		t.Fatal("downstream peer should receive the forwarded STREAM_DATA")
	}
	if findFrame(upStream.framesWritten(t), protocol.FrameStreamReset, 11) != nil {
		t.Error("no STREAM_RESET expected on the happy path")
	}
}
