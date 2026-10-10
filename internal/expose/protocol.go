// Package expose implements the ephemeral forward endpoint server served over
// the SOCKS5 CmdExpose stream.
//
// Operators run the lightweight Mutiauk binary on firewalled laptops and
// hold a long-lived authenticated connection to a Muti Metroo agent over the SOCKS5
// endpoint. They can claim one or more forward routing keys for the lifetime
// of that connection; per-stream traffic is tunneled in both directions over
// it. Registrations are ephemeral: they exist only in memory and disappear
// when the connection drops.
//
// The wire protocol is documented in the package-level constants.
package expose

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// Frame types. Each message carries a single frame:
//
//	[u8 type][u64 stream_id BE][payload...]
//
// Control frames use stream_id=0. JSON payloads where noted.
const (
	FrameRegister      uint8 = 0x01 // E->M, JSON RegisterPayload
	FrameRegisterAck   uint8 = 0x02 // M->E, JSON RegisterAckPayload
	FrameUnregister    uint8 = 0x03 // E->M, JSON UnregisterPayload
	FrameUnregisterAck uint8 = 0x04 // M->E, JSON UnregisterAckPayload
	FrameOpen          uint8 = 0x10 // M->E, JSON OpenPayload (M assigns stream id)
	FrameOpenAck       uint8 = 0x11 // E->M, JSON OpenAckPayload (same stream id)
	FrameData          uint8 = 0x20 // both, raw bytes
	FrameCloseWrite    uint8 = 0x21 // both, empty payload
	FrameClose         uint8 = 0x22 // both, empty payload
	FramePing          uint8 = 0xF0 // both, u64 nonce
	FramePong          uint8 = 0xF1 // both, u64 nonce
)

// FrameHeaderSize is the on-wire length of the fixed frame header (1 byte
// type + 8 byte stream id).
const FrameHeaderSize = 9

// MaxDataPayload is the largest DATA frame the server will emit. Inbound DATA
// frames may be larger; the read limit is enforced by the transport (see
// ManagerConfig.ReadLimit).
const MaxDataPayload = 16 * 1024

// LabelPrefix is the prefix used for the synthetic target label advertised
// for ephemeral expose endpoints (e.g. "expose:my-key"). Other
// agents see this in route advertisements; the exit agent itself ignores
// the target and looks up the backend by key.
const LabelPrefix = "expose:"

// Label returns the route-advertisement label for a given key.
func Label(key string) string { return LabelPrefix + key }

// ErrShortFrame is returned when a message is shorter than the frame header.
var ErrShortFrame = errors.New("expose: frame shorter than header")

// ErrUnknownFrameType is returned by DecodeFrame for unrecognised type bytes.
var ErrUnknownFrameType = errors.New("expose: unknown frame type")

// EncodeFrame returns a single-allocation frame header + payload.
func EncodeFrame(frameType uint8, streamID uint64, payload []byte) []byte {
	out := make([]byte, FrameHeaderSize+len(payload))
	out[0] = frameType
	binary.BigEndian.PutUint64(out[1:9], streamID)
	copy(out[FrameHeaderSize:], payload)
	return out
}

// DecodeFrame parses a message into its frame parts. The returned payload
// aliases buf; callers that need to retain it past the next call must copy.
func DecodeFrame(buf []byte) (frameType uint8, streamID uint64, payload []byte, err error) {
	if len(buf) < FrameHeaderSize {
		return 0, 0, nil, ErrShortFrame
	}
	frameType = buf[0]
	streamID = binary.BigEndian.Uint64(buf[1:9])
	payload = buf[FrameHeaderSize:]
	if !knownFrameType(frameType) {
		return frameType, streamID, payload, fmt.Errorf("%w: 0x%02x", ErrUnknownFrameType, frameType)
	}
	return frameType, streamID, payload, nil
}

func knownFrameType(t uint8) bool {
	switch t {
	case FrameRegister, FrameRegisterAck, FrameUnregister, FrameUnregisterAck,
		FrameOpen, FrameOpenAck, FrameData, FrameCloseWrite, FrameClose,
		FramePing, FramePong:
		return true
	}
	return false
}

// EncodeNonceFrame builds a control frame whose payload is a single u64
// (PING/PONG nonce).
func EncodeNonceFrame(frameType uint8, nonce uint64) []byte {
	out := make([]byte, FrameHeaderSize+8)
	out[0] = frameType
	// stream id is implicitly zero
	binary.BigEndian.PutUint64(out[FrameHeaderSize:], nonce)
	return out
}

// DecodeNonce parses a PING/PONG payload into its u64 nonce.
func DecodeNonce(payload []byte) (uint64, error) {
	if len(payload) != 8 {
		return 0, fmt.Errorf("expose: ping/pong payload must be 8 bytes, got %d", len(payload))
	}
	return binary.BigEndian.Uint64(payload), nil
}

// RegisterPayload is the JSON body of a REGISTER frame.
type RegisterPayload struct {
	Forwards []RegisterEntry `json:"forwards"`
}

// RegisterEntry describes a single key the operator wants to claim.
type RegisterEntry struct {
	Key     string `json:"key"`
	Comment string `json:"comment,omitempty"`
}

// RegisterAckPayload is the JSON body of a REGISTER_ACK frame.
type RegisterAckPayload struct {
	Results []KeyResult `json:"results"`
}

// UnregisterPayload is the JSON body of an UNREGISTER frame.
type UnregisterPayload struct {
	Keys []string `json:"keys"`
}

// UnregisterAckPayload is the JSON body of an UNREGISTER_ACK frame.
type UnregisterAckPayload struct {
	Results []KeyResult `json:"results"`
}

// KeyResult is a per-key success/error entry shared by REGISTER_ACK and
// UNREGISTER_ACK.
type KeyResult struct {
	Key   string `json:"key"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// OpenPayload is the JSON body of an OPEN frame sent by Muti Metroo when a forward
// stream arrives for a registered key.
type OpenPayload struct {
	Key        string `json:"key"`
	RemoteAddr string `json:"remote_addr,omitempty"`
}

// OpenAckPayload is the JSON body of an OPEN_ACK frame returned by Muti Metroo
// Entry. OK=false with a non-empty Error indicates the operator side could
// not accept the stream.
type OpenAckPayload struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}
