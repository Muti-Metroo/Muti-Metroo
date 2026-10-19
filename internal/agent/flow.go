package agent

import (
	"github.com/Muti-Metroo/Muti-Metroo/internal/identity"
	"github.com/Muti-Metroo/Muti-Metroo/internal/logging"
	"github.com/Muti-Metroo/Muti-Metroo/internal/peer"
	"github.com/Muti-Metroo/Muti-Metroo/internal/protocol"
	"github.com/Muti-Metroo/Muti-Metroo/internal/stream"
)

// Per-stream flow control (see protocol.FlagFlowControl).
//
// The initiator side of a stream keeps its window and credit in the
// stream.Stream (stream.Manager.HandleStreamOpenAck turns them on). The
// target side -- exit, forward endpoint, file transfer -- has no
// stream.Stream, so its state lives in a.flowTargets, created when a
// STREAM_OPEN with FlagFlowControl arrives and released when the stream
// ends. Transit agents keep no state: they relay STREAM_WINDOW frames like
// STREAM_DATA.

// targetFlow is the flow-control state of a stream this agent is the target
// of.
type targetFlow struct {
	window *stream.Window // frames this agent may still send
	credit stream.Credit  // frames this agent consumed, not yet granted back
}

// streamOpenFlags returns the flags for a STREAM_OPEN sent to nextHop:
// FlagFlowControl when nextHop advertised support for it.
func (a *Agent) streamOpenFlags(nextHop identity.AgentID) uint8 {
	if a.flowControlWith(a.peerMgr.GetPeer(nextHop)) {
		return protocol.FlagFlowControl
	}
	return 0
}

// flowControlWith reports whether streams through conn can use flow
// control: both this agent and the peer support it.
func (a *Agent) flowControlWith(conn *peer.Connection) bool {
	return !a.cfg.Limits.DisableFlowControl && conn != nil &&
		conn.HasCapability(protocol.CapabilityFlowControl)
}

// registerTargetFlow turns on flow control for a stream this agent is the
// target of. WriteStreamOpenAck then confirms it to the initiator.
func (a *Agent) registerTargetFlow(peer identity.AgentID, streamID uint64) {
	a.flowMu.Lock()
	defer a.flowMu.Unlock()
	a.flowTargets[streamKey{peer: peer, streamID: streamID}] = &targetFlow{
		window: stream.NewWindow(protocol.StreamWindowFrames),
	}
}

func (a *Agent) targetFlowFor(peer identity.AgentID, streamID uint64) *targetFlow {
	a.flowMu.Lock()
	defer a.flowMu.Unlock()
	return a.flowTargets[streamKey{peer: peer, streamID: streamID}]
}

// releaseTargetFlow drops a target stream's flow-control state and wakes any
// sender waiting for window.
func (a *Agent) releaseTargetFlow(peer identity.AgentID, streamID uint64) {
	key := streamKey{peer: peer, streamID: streamID}
	a.flowMu.Lock()
	tf := a.flowTargets[key]
	delete(a.flowTargets, key)
	a.flowMu.Unlock()
	if tf != nil {
		tf.window.Close()
	}
}

// releaseTargetFlowsForPeer drops the state of every target stream from peer,
// for when the peer connection goes away.
func (a *Agent) releaseTargetFlowsForPeer(peer identity.AgentID) {
	a.flowMu.Lock()
	var closing []*targetFlow
	for key, tf := range a.flowTargets {
		if key.peer == peer {
			delete(a.flowTargets, key)
			closing = append(closing, tf)
		}
	}
	a.flowMu.Unlock()
	for _, tf := range closing {
		tf.window.Close()
	}
}

// acquireSend waits for window to send one data frame on a stream, from
// either side. Streams without flow control return at once.
//
// Never call it (or WriteStreamData) from a peer's frame drainer: the
// STREAM_WINDOW that would unblock it arrives through that same drainer.
// Drainer-side code that must send on a flow-controlled stream releases its
// flow-control state first (see closeFileTransferStream).
func (a *Agent) acquireSend(peer identity.AgentID, streamID uint64) error {
	if s := a.streamMgr.GetStream(peer, streamID); s != nil {
		return s.AcquireSend()
	}
	if tf := a.targetFlowFor(peer, streamID); tf != nil {
		return tf.window.Acquire(nil)
	}
	return nil
}

// StreamConsumed records that this agent, as the target of a stream, has
// consumed one data frame from peer (written it to the destination, or
// processed it), and returns window to the peer when enough has collected.
// Exit and forward handlers call it from their writer goroutines.
func (a *Agent) StreamConsumed(peer identity.AgentID, streamID uint64) {
	tf := a.targetFlowFor(peer, streamID)
	if tf == nil {
		return
	}
	if n := tf.credit.Consumed(1); n > 0 {
		a.sendStreamWindow(peer, streamID, n)
	}
}

// sendStreamWindow grants peer n more data frames on a stream.
func (a *Agent) sendStreamWindow(peer identity.AgentID, streamID uint64, n uint32) {
	frame := &protocol.Frame{
		Type:     protocol.FrameStreamWindow,
		StreamID: streamID,
		Payload:  (&protocol.StreamWindow{Increment: n}).Encode(),
	}
	if err := a.peerMgr.SendToPeer(peer, frame); err != nil {
		a.logger.Debug("failed to send stream window",
			logging.KeyPeerID, peer.ShortString(),
			logging.KeyStreamID, streamID,
			logging.KeyError, err)
	}
}

// handleStreamWindow processes a STREAM_WINDOW frame: relayed along the
// stream's path like STREAM_DATA, or applied to this agent's send window when
// it is an endpoint of the stream.
func (a *Agent) handleStreamWindow(peerID identity.AgentID, frame *protocol.Frame) {
	upRelay, downRelay := a.tcpRelay.LookupBoth(peerID, frame.StreamID)
	if upRelay != nil || downRelay != nil {
		var dstPeer identity.AgentID
		var dstID uint64
		if upRelay != nil {
			dstPeer, dstID = upRelay.DownstreamPeer, upRelay.DownstreamID
		} else {
			dstPeer, dstID = downRelay.UpstreamPeer, downRelay.UpstreamID
		}
		fwd := &protocol.Frame{
			Type:     protocol.FrameStreamWindow,
			StreamID: dstID,
			Payload:  frame.Payload,
		}
		if err := a.peerMgr.SendToPeer(dstPeer, fwd); err != nil {
			a.logger.Debug("failed to relay stream window",
				logging.KeyPeerID, dstPeer.ShortString(),
				logging.KeyStreamID, dstID,
				logging.KeyError, err)
		}
		return
	}

	win, err := protocol.DecodeStreamWindow(frame.Payload)
	if err != nil {
		return
	}
	if a.streamMgr.HandleStreamWindow(peerID, frame.StreamID, win.Increment) {
		return
	}
	if tf := a.targetFlowFor(peerID, frame.StreamID); tf != nil {
		tf.window.Grant(int(win.Increment))
	}
}
