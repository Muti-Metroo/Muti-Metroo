// Package forward implements TCP port forwarding through the mesh network.
// This enables ngrok/localtunnel-style reverse tunneling where local services
// can be exposed through the mesh network using named routing keys.
package forward

import (
	"context"
	"net"

	"github.com/postalsys/muti-metroo/internal/crypto"
	"github.com/postalsys/muti-metroo/internal/identity"
)

// ForwardDialer is an interface for dialing port forward connections through the mesh.
type ForwardDialer interface {
	// DialForward opens a connection to the exit agent with the matching routing key.
	DialForward(ctx context.Context, key string) (net.Conn, error)
}

// StreamWriter is an interface for writing to virtual streams.
type StreamWriter interface {
	// WriteStreamData writes data to a stream.
	WriteStreamData(peerID identity.AgentID, streamID uint64, data []byte, flags uint8) error

	// WriteStreamOpenAck sends a successful open acknowledgment with ephemeral public key for E2E encryption.
	WriteStreamOpenAck(peerID identity.AgentID, streamID uint64, requestID uint64, boundIP net.IP, boundPort uint16, ephemeralPubKey [crypto.KeySize]byte) error

	// WriteStreamOpenErr sends a failed open acknowledgment.
	WriteStreamOpenErr(peerID identity.AgentID, streamID uint64, requestID uint64, errorCode uint16, message string) error

	// WriteStreamClose sends a close frame.
	WriteStreamClose(peerID identity.AgentID, streamID uint64) error

	// WriteStreamReset sends a reset frame aborting the stream.
	WriteStreamReset(peerID identity.AgentID, streamID uint64, errorCode uint16) error
}

// writeQueueSize bounds each connection's pending backend writes. The queue
// exists so a slow backend never stalls the peer connection's frame drainer;
// when it fills, only the affected stream is reset.
const writeQueueSize = 64

// writeRequest is one unit of work for a connection's writer goroutine. The
// FIN marker travels through the queue so CloseWrite cannot overtake queued
// data.
type writeRequest struct {
	data []byte // plaintext payload; empty for a pure FIN
	fin  bool   // half-close the backend after data is written
}

// Endpoint represents a tunnel exit point configuration.
type Endpoint struct {
	Key    string // Routing key
	Target string // Fixed target host:port
}
