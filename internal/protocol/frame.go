package protocol

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"unicode/utf8"

	"github.com/Muti-Metroo/Muti-Metroo/internal/identity"
)

var (
	// ErrFrameTooLarge is returned when a frame exceeds the maximum size
	ErrFrameTooLarge = errors.New("frame payload exceeds maximum size")

	// ErrInvalidFrame is returned when a frame is malformed
	ErrInvalidFrame = errors.New("invalid frame")
)

// ============================================================================
// Binary encoding/decoding helpers
// ============================================================================

// bufferWriter provides efficient buffer writing with automatic offset tracking.
type bufferWriter struct {
	buf    []byte
	offset int
}

func newBufferWriter(size int) *bufferWriter {
	return &bufferWriter{buf: make([]byte, size)}
}

func (w *bufferWriter) writeUint8(v uint8) {
	w.buf[w.offset] = v
	w.offset++
}

func (w *bufferWriter) writeUint16(v uint16) {
	binary.BigEndian.PutUint16(w.buf[w.offset:], v)
	w.offset += 2
}

func (w *bufferWriter) writeUint32(v uint32) {
	binary.BigEndian.PutUint32(w.buf[w.offset:], v)
	w.offset += 4
}

func (w *bufferWriter) writeUint64(v uint64) {
	binary.BigEndian.PutUint64(w.buf[w.offset:], v)
	w.offset += 8
}

func (w *bufferWriter) writeBytes(data []byte) {
	copy(w.buf[w.offset:], data)
	w.offset += len(data)
}

func (w *bufferWriter) writeBool(v bool) {
	if v {
		w.buf[w.offset] = 1
	} else {
		w.buf[w.offset] = 0
	}
	w.offset++
}

// MaxCountLen is the largest element count a 1-byte count prefix can describe.
const MaxCountLen = 255

// MaxRoutesPerMessage caps how many routes ride in one ROUTE_ADVERTISE or
// ROUTE_WITHDRAW. Both wire formats carry the route count in a single byte, so
// a message holding 256 or more routes encodes a wrapped count that the peer
// cannot decode. A payload decode error is log-and-drop, so the sender would
// keep re-sending the same undecodable message forever. Origins with more
// routes than this must split across several messages.
const MaxRoutesPerMessage = 255

// MaxStringLen is the longest string the 1-byte length prefix can describe.
// Callers should validate operator input against this; writeString clamps as
// a last resort so an over-long value cannot produce an undecodable frame.
const MaxStringLen = 255

// writeString writes a length-prefixed string (1-byte length + string data).
//
// Strings longer than MaxStringLen are truncated. Writing the untruncated
// length through a uint8 would wrap it, leaving a length prefix that disagrees
// with the bytes that follow; the peer then misparses the rest of the frame
// and drops the connection, which for a display name in PEER_HELLO means the
// handshake never completes. Callers size their buffer from the full length,
// and bytes() slices to the offset actually written, so clamping here leaves
// no stray padding behind.
func (w *bufferWriter) writeString(s string) {
	s = clampString(s)
	w.buf[w.offset] = uint8(len(s))
	w.offset++
	copy(w.buf[w.offset:], s)
	w.offset += len(s)
}

// clampString truncates s to MaxStringLen bytes without splitting a UTF-8
// rune, so a clamped display name stays valid text rather than ending in a
// partial multi-byte sequence.
func clampString(s string) string {
	if len(s) <= MaxStringLen {
		return s
	}
	end := MaxStringLen
	for end > 0 && !utf8.RuneStart(s[end]) {
		end--
	}
	return s[:end]
}

// writeAgentIDs writes a list of AgentIDs with a 1-byte count prefix.
//
// The list is clamped to MaxCountLen for the same reason writeString clamps:
// a wrapped count byte describes fewer entries than follow, the peer misparses
// the rest of the frame, and a decode error drops the connection permanently.
// Callers are expected to stay well under this; the clamp is a backstop.
func (w *bufferWriter) writeAgentIDs(ids []identity.AgentID) {
	if len(ids) > MaxCountLen {
		ids = ids[:MaxCountLen]
	}
	w.buf[w.offset] = uint8(len(ids))
	w.offset++
	for _, id := range ids {
		copy(w.buf[w.offset:], id[:])
		w.offset += 16
	}
}

func (w *bufferWriter) bytes() []byte {
	return w.buf[:w.offset]
}

// bufferReader provides efficient buffer reading with bounds checking.
type bufferReader struct {
	buf    []byte
	offset int
	err    error
	ctx    string // context for error messages
}

func newBufferReader(buf []byte, ctx string) *bufferReader {
	return &bufferReader{buf: buf, ctx: ctx}
}

func (r *bufferReader) remaining() int {
	return len(r.buf) - r.offset
}

func (r *bufferReader) setError(msg string) {
	if r.err == nil {
		r.err = fmt.Errorf("%w: %s %s", ErrInvalidFrame, r.ctx, msg)
	}
}

func (r *bufferReader) readUint8() uint8 {
	if r.err != nil || r.offset >= len(r.buf) {
		r.setError("truncated")
		return 0
	}
	v := r.buf[r.offset]
	r.offset++
	return v
}

func (r *bufferReader) readUint16() uint16 {
	if r.err != nil || r.offset+2 > len(r.buf) {
		r.setError("truncated")
		return 0
	}
	v := binary.BigEndian.Uint16(r.buf[r.offset:])
	r.offset += 2
	return v
}

func (r *bufferReader) readUint32() uint32 {
	if r.err != nil || r.offset+4 > len(r.buf) {
		r.setError("truncated")
		return 0
	}
	v := binary.BigEndian.Uint32(r.buf[r.offset:])
	r.offset += 4
	return v
}

func (r *bufferReader) readUint64() uint64 {
	if r.err != nil || r.offset+8 > len(r.buf) {
		r.setError("truncated")
		return 0
	}
	v := binary.BigEndian.Uint64(r.buf[r.offset:])
	r.offset += 8
	return v
}

func (r *bufferReader) readBytes(n int) []byte {
	if r.err != nil || r.offset+n > len(r.buf) {
		r.setError("truncated")
		return nil
	}
	data := make([]byte, n)
	copy(data, r.buf[r.offset:r.offset+n])
	r.offset += n
	return data
}

func (r *bufferReader) readBool() bool {
	return r.readUint8() != 0
}

// readString reads a length-prefixed string (1-byte length + string data).
func (r *bufferReader) readString() string {
	length := int(r.readUint8())
	if r.err != nil {
		return ""
	}
	if r.offset+length > len(r.buf) {
		r.setError("string truncated")
		return ""
	}
	s := string(r.buf[r.offset : r.offset+length])
	r.offset += length
	return s
}

// readAgentID reads a single 16-byte AgentID.
func (r *bufferReader) readAgentID() identity.AgentID {
	var id identity.AgentID
	if r.err != nil || r.offset+16 > len(r.buf) {
		r.setError("agentID truncated")
		return id
	}
	copy(id[:], r.buf[r.offset:r.offset+16])
	r.offset += 16
	return id
}

// readAgentIDs reads a list of AgentIDs with a 1-byte count prefix.
func (r *bufferReader) readAgentIDs() []identity.AgentID {
	count := int(r.readUint8())
	if r.err != nil {
		return nil
	}
	ids := make([]identity.AgentID, count)
	for i := 0; i < count; i++ {
		ids[i] = r.readAgentID()
		if r.err != nil {
			return nil
		}
	}
	return ids
}

// readEphemeralKey reads a 32-byte ephemeral public key.
func (r *bufferReader) readEphemeralKey() [EphemeralKeySize]byte {
	var key [EphemeralKeySize]byte
	if r.err != nil || r.offset+EphemeralKeySize > len(r.buf) {
		r.setError("ephemeral key missing")
		return key
	}
	copy(key[:], r.buf[r.offset:r.offset+EphemeralKeySize])
	r.offset += EphemeralKeySize
	return key
}

// addressLength returns the byte length for a given address type.
// For domain addresses, domainLenByte should be the first byte of the address data.
func addressLength(addrType uint8, domainLenByte byte) (int, error) {
	switch addrType {
	case AddrTypeIPv4:
		return 4, nil
	case AddrTypeIPv6:
		return 16, nil
	case AddrTypeDomain:
		return 1 + int(domainLenByte), nil
	default:
		return 0, fmt.Errorf("%w: unknown address type %d", ErrInvalidFrame, addrType)
	}
}

// prefixLength returns the byte length of a route prefix for the fixed-size
// address families. Domain and forward routes are variable-length and sized
// by their callers from the actual prefix bytes. Unknown families return an
// error: guessing a size would make the decoder consume the wrong number of
// bytes and silently mis-parse the rest of the frame.
func prefixLength(family uint8) (int, error) {
	switch family {
	case AddrFamilyIPv4:
		return 4, nil
	case AddrFamilyIPv6:
		return 16, nil
	case AddrFamilyAgent:
		return 16, nil // AgentID is 16 bytes
	default:
		return 0, fmt.Errorf("unknown address family %d", family)
	}
}

// Frame represents a wire protocol frame.
// Header format (14 bytes):
//
//	Type     [1 byte]  - Frame type
//	Flags    [1 byte]  - Frame flags
//	Length   [4 bytes] - Payload length (big-endian)
//	StreamID [8 bytes] - Stream identifier (big-endian)
type Frame struct {
	Type     uint8
	Flags    uint8
	StreamID uint64
	Payload  []byte
}

// Encode serializes the frame to bytes.
func (f *Frame) Encode() ([]byte, error) {
	if len(f.Payload) > MaxPayloadSize {
		return nil, ErrFrameTooLarge
	}

	buf := make([]byte, HeaderSize+len(f.Payload))

	// Header
	f.encodeHeader(buf[:HeaderSize])

	// Payload
	copy(buf[HeaderSize:], f.Payload)

	return buf, nil
}

// encodeHeader writes the 14-byte frame header into buf.
// Callers must ensure the payload length was validated against MaxPayloadSize.
func (f *Frame) encodeHeader(buf []byte) {
	buf[0] = f.Type
	buf[1] = f.Flags
	binary.BigEndian.PutUint32(buf[2:6], uint32(len(f.Payload)))
	binary.BigEndian.PutUint64(buf[6:14], f.StreamID)
}

// DecodeHeader decodes a frame header from bytes.
func DecodeHeader(buf []byte) (frameType uint8, flags uint8, length uint32, streamID uint64, err error) {
	if len(buf) < HeaderSize {
		return 0, 0, 0, 0, fmt.Errorf("%w: header too short", ErrInvalidFrame)
	}

	frameType = buf[0]
	flags = buf[1]
	length = binary.BigEndian.Uint32(buf[2:6])
	streamID = binary.BigEndian.Uint64(buf[6:14])

	if length > MaxPayloadSize {
		return 0, 0, 0, 0, ErrFrameTooLarge
	}

	return
}

// Decode deserializes a frame from bytes.
func Decode(buf []byte) (*Frame, error) {
	frameType, flags, length, streamID, err := DecodeHeader(buf)
	if err != nil {
		return nil, err
	}

	if len(buf) < HeaderSize+int(length) {
		return nil, fmt.Errorf("%w: buffer too short for payload", ErrInvalidFrame)
	}

	payload := make([]byte, length)
	copy(payload, buf[HeaderSize:HeaderSize+length])

	return &Frame{
		Type:     frameType,
		Flags:    flags,
		StreamID: streamID,
		Payload:  payload,
	}, nil
}

// String returns a debug representation of the frame.
func (f *Frame) String() string {
	return fmt.Sprintf("Frame{Type=%s, Flags=0x%02x, StreamID=%d, PayloadLen=%d}",
		FrameTypeName(f.Type), f.Flags, f.StreamID, len(f.Payload))
}

// ============================================================================
// Payload structures
// ============================================================================

// PeerHello is the payload for PEER_HELLO and PEER_HELLO_ACK frames.
type PeerHello struct {
	Version uint16
	AgentID identity.AgentID
	// Timestamp is reserved/unused: the receiver echoes it back in
	// PEER_HELLO_ACK but neither side reads it (RTT is measured with
	// time.Since around the handshake and via keepalives). Kept on the wire
	// for compatibility.
	Timestamp uint64
	// Capabilities is reserved/unused: it is exchanged and stored but no
	// production code path consumes it (HasCapability has no callers).
	// Kept on the wire for compatibility and future feature negotiation.
	Capabilities []string
	DisplayName  string // Added for topology visualization
}

// Encode serializes PeerHello to bytes.
func (p *PeerHello) Encode() []byte {
	// Calculate size: version(2) + agentID(16) + timestamp(8) + displayName(1+len) + caps(1 + sum(1+len))
	size := 2 + 16 + 8 + 1 + len(p.DisplayName) + 1
	for _, cap := range p.Capabilities {
		size += 1 + len(cap)
	}

	w := newBufferWriter(size)
	w.writeUint16(p.Version)
	w.writeBytes(p.AgentID[:])
	w.writeUint64(p.Timestamp)
	w.writeString(p.DisplayName)
	w.writeUint8(uint8(len(p.Capabilities)))
	for _, cap := range p.Capabilities {
		w.writeString(cap)
	}

	return w.bytes()
}

// DecodePeerHello deserializes PeerHello from bytes.
func DecodePeerHello(buf []byte) (*PeerHello, error) {
	if len(buf) < 28 { // 2 + 16 + 8 + 1 + 1 (min: empty displayName + capLen)
		return nil, fmt.Errorf("%w: PeerHello too short", ErrInvalidFrame)
	}

	r := newBufferReader(buf, "PeerHello")
	p := &PeerHello{
		Version:     r.readUint16(),
		AgentID:     r.readAgentID(),
		Timestamp:   r.readUint64(),
		DisplayName: r.readString(),
	}

	capLen := int(r.readUint8())
	p.Capabilities = make([]string, 0, capLen)
	for i := 0; i < capLen && r.err == nil; i++ {
		p.Capabilities = append(p.Capabilities, r.readString())
	}

	if r.err != nil {
		return nil, r.err
	}
	return p, nil
}

// EphemeralKeySize is the size of X25519 ephemeral public keys.
const EphemeralKeySize = 32

// openWire is the shared wire layout of StreamOpen and UDPOpen:
//
//	requestID(8) + addrType(1) + address(var) + port(2) + ttl(1) +
//	pathLen(1) + path + ephemeralPubKey(32)
//
// ICMPOpen deliberately differs (no port, length-prefixed IP) and does not
// use this layout. The two public Open types encode through these helpers so
// their serializers cannot drift apart; the wire format is unchanged.
type openWire struct {
	RequestID       uint64
	AddressType     uint8
	Address         []byte
	Port            uint16
	TTL             uint8
	RemainingPath   []identity.AgentID
	EphemeralPubKey [EphemeralKeySize]byte
}

func (o *openWire) encode() []byte {
	w := newBufferWriter(8 + 1 + len(o.Address) + 2 + 1 + 1 + len(o.RemainingPath)*16 + EphemeralKeySize)
	w.writeUint64(o.RequestID)
	w.writeUint8(o.AddressType)
	w.writeBytes(o.Address)
	w.writeUint16(o.Port)
	w.writeUint8(o.TTL)
	w.writeAgentIDs(o.RemainingPath)
	w.writeBytes(o.EphemeralPubKey[:])

	return w.bytes()
}

func decodeOpenWire(buf []byte, name string) (*openWire, error) {
	if len(buf) < 13+EphemeralKeySize { // 8 + 1 + 2 + 1 + 1 + 32 (minimum with key)
		return nil, fmt.Errorf("%w: %s too short", ErrInvalidFrame, name)
	}

	r := newBufferReader(buf, name)
	o := &openWire{
		RequestID:   r.readUint64(),
		AddressType: r.readUint8(),
	}

	// Determine address length based on type
	var addrLen int
	if o.AddressType == AddrTypeDomain {
		if r.offset >= len(buf) {
			return nil, fmt.Errorf("%w: %s domain length missing", ErrInvalidFrame, name)
		}
		addrLen = 1 + int(buf[r.offset])
	} else {
		var err error
		addrLen, err = addressLength(o.AddressType, 0)
		if err != nil {
			return nil, err
		}
	}

	o.Address = r.readBytes(addrLen)
	o.Port = r.readUint16()
	o.TTL = r.readUint8()
	o.RemainingPath = r.readAgentIDs()
	o.EphemeralPubKey = r.readEphemeralKey()

	if r.err != nil {
		return nil, r.err
	}
	return o, nil
}

// ackWire is the shared wire layout of StreamOpenAck and UDPOpenAck:
//
//	requestID(8) + boundAddrType(1) + boundAddr(var) + boundPort(2) +
//	ephemeralPubKey(32)
//
// ICMPOpenAck is smaller (no bound address) and does not use this layout.
type ackWire struct {
	RequestID       uint64
	BoundAddrType   uint8
	BoundAddr       []byte
	BoundPort       uint16
	EphemeralPubKey [EphemeralKeySize]byte
}

func (a *ackWire) encode() []byte {
	w := newBufferWriter(8 + 1 + len(a.BoundAddr) + 2 + EphemeralKeySize)
	w.writeUint64(a.RequestID)
	w.writeUint8(a.BoundAddrType)
	w.writeBytes(a.BoundAddr)
	w.writeUint16(a.BoundPort)
	w.writeBytes(a.EphemeralPubKey[:])

	return w.bytes()
}

func decodeAckWire(buf []byte, name string) (*ackWire, error) {
	if len(buf) < 12+EphemeralKeySize { // 8 + 1 + 1 + 2 + 32 minimum (empty addr + key)
		return nil, fmt.Errorf("%w: %s too short", ErrInvalidFrame, name)
	}

	r := newBufferReader(buf, name)
	a := &ackWire{
		RequestID:     r.readUint64(),
		BoundAddrType: r.readUint8(),
	}

	// Determine address length (only IPv4/IPv6 for bound addresses)
	var addrLen int
	switch a.BoundAddrType {
	case AddrTypeIPv4:
		addrLen = 4
	case AddrTypeIPv6:
		addrLen = 16
	default:
		addrLen = 0
	}

	a.BoundAddr = r.readBytes(addrLen)
	a.BoundPort = r.readUint16()
	a.EphemeralPubKey = r.readEphemeralKey()

	if r.err != nil {
		return nil, r.err
	}
	return a, nil
}

// errWire is the shared wire layout of StreamOpenErr, UDPOpenErr, and
// ICMPOpenErr:
//
//	requestID(8) + errorCode(2) + messageLen(1) + message
type errWire struct {
	RequestID uint64
	ErrorCode uint16
	Message   string
}

func (e *errWire) encode() []byte {
	msg := clampString(e.Message)

	w := newBufferWriter(8 + 2 + 1 + len(msg))
	w.writeUint64(e.RequestID)
	w.writeUint16(e.ErrorCode)
	w.writeString(msg)

	return w.bytes()
}

func decodeErrWire(buf []byte, name string) (*errWire, error) {
	if len(buf) < 11 { // 8 + 2 + 1
		return nil, fmt.Errorf("%w: %s too short", ErrInvalidFrame, name)
	}

	r := newBufferReader(buf, name)
	e := &errWire{
		RequestID: r.readUint64(),
		ErrorCode: r.readUint16(),
		Message:   r.readString(),
	}

	if r.err != nil {
		return nil, r.err
	}
	return e, nil
}

// StreamOpen is the payload for STREAM_OPEN frames.
type StreamOpen struct {
	RequestID       uint64
	AddressType     uint8
	Address         []byte // IPv4 (4), IPv6 (16), or domain (1+N)
	Port            uint16
	TTL             uint8
	RemainingPath   []identity.AgentID
	EphemeralPubKey [EphemeralKeySize]byte // Initiator's ephemeral public key for E2E encryption
}

// Encode serializes StreamOpen to bytes.
func (s *StreamOpen) Encode() []byte {
	return (&openWire{
		RequestID:       s.RequestID,
		AddressType:     s.AddressType,
		Address:         s.Address,
		Port:            s.Port,
		TTL:             s.TTL,
		RemainingPath:   s.RemainingPath,
		EphemeralPubKey: s.EphemeralPubKey,
	}).encode()
}

// DecodeStreamOpen deserializes StreamOpen from bytes.
func DecodeStreamOpen(buf []byte) (*StreamOpen, error) {
	o, err := decodeOpenWire(buf, "StreamOpen")
	if err != nil {
		return nil, err
	}
	return &StreamOpen{
		RequestID:       o.RequestID,
		AddressType:     o.AddressType,
		Address:         o.Address,
		Port:            o.Port,
		TTL:             o.TTL,
		RemainingPath:   o.RemainingPath,
		EphemeralPubKey: o.EphemeralPubKey,
	}, nil
}

// GetDestinationIP returns the destination IP for IPv4/IPv6 addresses.
func (s *StreamOpen) GetDestinationIP() net.IP {
	switch s.AddressType {
	case AddrTypeIPv4:
		return net.IP(s.Address)
	case AddrTypeIPv6:
		return net.IP(s.Address)
	default:
		return nil
	}
}

// GetDestinationDomain returns the domain name for domain type addresses.
func (s *StreamOpen) GetDestinationDomain() string {
	if s.AddressType == AddrTypeDomain && len(s.Address) > 1 {
		return string(s.Address[1:])
	}
	return ""
}

// StreamOpenAck is the payload for STREAM_OPEN_ACK frames.
type StreamOpenAck struct {
	RequestID       uint64
	BoundAddrType   uint8
	BoundAddr       []byte
	BoundPort       uint16
	EphemeralPubKey [EphemeralKeySize]byte // Responder's ephemeral public key for E2E encryption
}

// Encode serializes StreamOpenAck to bytes.
func (s *StreamOpenAck) Encode() []byte {
	return (&ackWire{
		RequestID:       s.RequestID,
		BoundAddrType:   s.BoundAddrType,
		BoundAddr:       s.BoundAddr,
		BoundPort:       s.BoundPort,
		EphemeralPubKey: s.EphemeralPubKey,
	}).encode()
}

// DecodeStreamOpenAck deserializes StreamOpenAck from bytes.
func DecodeStreamOpenAck(buf []byte) (*StreamOpenAck, error) {
	a, err := decodeAckWire(buf, "StreamOpenAck")
	if err != nil {
		return nil, err
	}
	return &StreamOpenAck{
		RequestID:       a.RequestID,
		BoundAddrType:   a.BoundAddrType,
		BoundAddr:       a.BoundAddr,
		BoundPort:       a.BoundPort,
		EphemeralPubKey: a.EphemeralPubKey,
	}, nil
}

// StreamOpenErr is the payload for STREAM_OPEN_ERR frames.
type StreamOpenErr struct {
	RequestID uint64
	ErrorCode uint16
	Message   string
}

// Encode serializes StreamOpenErr to bytes.
func (s *StreamOpenErr) Encode() []byte {
	return (&errWire{RequestID: s.RequestID, ErrorCode: s.ErrorCode, Message: s.Message}).encode()
}

// DecodeStreamOpenErr deserializes StreamOpenErr from bytes.
func DecodeStreamOpenErr(buf []byte) (*StreamOpenErr, error) {
	e, err := decodeErrWire(buf, "StreamOpenErr")
	if err != nil {
		return nil, err
	}
	return &StreamOpenErr{RequestID: e.RequestID, ErrorCode: e.ErrorCode, Message: e.Message}, nil
}

// StreamReset is the payload for STREAM_RESET frames.
type StreamReset struct {
	ErrorCode uint16
}

// Encode serializes StreamReset to bytes.
func (s *StreamReset) Encode() []byte {
	w := newBufferWriter(2)
	w.writeUint16(s.ErrorCode)
	return w.bytes()
}

// DecodeStreamReset deserializes StreamReset from bytes.
func DecodeStreamReset(buf []byte) (*StreamReset, error) {
	if len(buf) < 2 {
		return nil, fmt.Errorf("%w: StreamReset too short", ErrInvalidFrame)
	}
	r := newBufferReader(buf, "StreamReset")
	return &StreamReset{ErrorCode: r.readUint16()}, nil
}

// StreamWindow is the payload of a STREAM_WINDOW frame: the number of data
// frames the receiver of the frame may send in addition to its current window.
type StreamWindow struct {
	Increment uint32
}

// Encode serializes StreamWindow to bytes.
func (s *StreamWindow) Encode() []byte {
	w := newBufferWriter(4)
	w.writeUint32(s.Increment)
	return w.bytes()
}

// DecodeStreamWindow deserializes StreamWindow from bytes.
func DecodeStreamWindow(buf []byte) (*StreamWindow, error) {
	if len(buf) < 4 {
		return nil, fmt.Errorf("%w: StreamWindow too short", ErrInvalidFrame)
	}
	r := newBufferReader(buf, "StreamWindow")
	return &StreamWindow{Increment: r.readUint32()}, nil
}

// Keepalive is the payload for KEEPALIVE and KEEPALIVE_ACK frames.
type Keepalive struct {
	Timestamp uint64
}

// Encode serializes Keepalive to bytes.
func (k *Keepalive) Encode() []byte {
	w := newBufferWriter(8)
	w.writeUint64(k.Timestamp)
	return w.bytes()
}

// DecodeKeepalive deserializes Keepalive from bytes.
func DecodeKeepalive(buf []byte) (*Keepalive, error) {
	if len(buf) < 8 {
		return nil, fmt.Errorf("%w: Keepalive too short", ErrInvalidFrame)
	}
	r := newBufferReader(buf, "Keepalive")
	return &Keepalive{Timestamp: r.readUint64()}, nil
}

// Route represents a single route in ROUTE_ADVERTISE/WITHDRAW.
type Route struct {
	AddressFamily uint8
	PrefixLength  uint8
	Prefix        []byte // 4 or 16 bytes for IP, length-prefixed string for domain
	Metric        uint16
}

// routesEncodedSize returns the wire size of a route list:
// family(1) + prefixLen(1) + prefix(var) + metric(2) per route.
// Variable-length (domain, forward) and unknown families are sized from the
// actual prefix bytes so the buffer is always exact; the decoder rejects
// unknown families explicitly.
func routesEncodedSize(routes []Route) int {
	size := 0
	for _, route := range routes {
		pLen, err := prefixLength(route.AddressFamily)
		if err != nil || route.AddressFamily == AddrFamilyDomain || route.AddressFamily == AddrFamilyForward {
			pLen = len(route.Prefix)
		}
		size += 2 + pLen + 2
	}
	return size
}

// encodeRoutes writes the route list shared by ROUTE_ADVERTISE and
// ROUTE_WITHDRAW.
func encodeRoutes(w *bufferWriter, routes []Route) {
	for _, route := range routes {
		w.writeUint8(route.AddressFamily)
		w.writeUint8(route.PrefixLength)
		w.writeBytes(route.Prefix)
		w.writeUint16(route.Metric)
	}
}

// decodeRoutes reads count routes in the layout written by encodeRoutes.
// Errors are recorded on rd (rd.err), matching the bufferReader idiom.
func decodeRoutes(rd *bufferReader, buf []byte, count int) []Route {
	routes := make([]Route, count)
	for i := 0; i < count && rd.err == nil; i++ {
		route := &routes[i]
		route.AddressFamily = rd.readUint8()
		route.PrefixLength = rd.readUint8()

		// Determine prefix length based on address family
		var pLen int
		switch route.AddressFamily {
		case AddrFamilyDomain:
			// Domain routes use length-prefixed strings: [1 byte len][domain]
			if rd.offset >= len(buf) {
				rd.setError("prefix length missing")
				break
			}
			pLen = 1 + int(buf[rd.offset])
		case AddrFamilyForward:
			// Forward routes: [1 byte key len][key][1 byte target len][target]
			if rd.offset >= len(buf) {
				rd.setError("forward key length missing")
				break
			}
			keyLen := int(buf[rd.offset])
			// Calculate total: keyLen byte + key + targetLen byte + target
			targetLenOffset := rd.offset + 1 + keyLen
			if targetLenOffset >= len(buf) {
				rd.setError("forward target length missing")
				break
			}
			targetLen := int(buf[targetLenOffset])
			pLen = 1 + keyLen + 1 + targetLen
		case AddrFamilyAgent:
			// Agent presence routes: 16-byte AgentID prefix
			pLen = 16
		default:
			// IPv4/IPv6 routes have fixed prefix lengths; an unrecognized
			// family fails loudly here instead of guessing a size and
			// mis-parsing the rest of the frame.
			if pl, err := prefixLength(route.AddressFamily); err != nil {
				rd.setError(err.Error())
			} else {
				pLen = pl
			}
		}

		route.Prefix = rd.readBytes(pLen)
		route.Metric = rd.readUint16()
	}
	return routes
}

// Encode serializes Route to bytes.
func (r *Route) Encode() []byte {
	w := newBufferWriter(2 + len(r.Prefix) + 2)
	w.writeUint8(r.AddressFamily)
	w.writeUint8(r.PrefixLength)
	w.writeBytes(r.Prefix)
	w.writeUint16(r.Metric)
	return w.bytes()
}

// EncodeDomainPrefix encodes a domain pattern for route advertisement.
// Returns length-prefixed domain string (1 byte length + domain).
func EncodeDomainPrefix(pattern string) []byte {
	buf := make([]byte, 1+len(pattern))
	buf[0] = uint8(len(pattern))
	copy(buf[1:], pattern)
	return buf
}

// DecodeDomainPrefix decodes a domain pattern from route advertisement.
// Input is length-prefixed domain string (1 byte length + domain).
func DecodeDomainPrefix(prefix []byte) string {
	if len(prefix) < 1 {
		return ""
	}
	domainLen := int(prefix[0])
	if len(prefix) < 1+domainLen {
		return ""
	}
	return string(prefix[1 : 1+domainLen])
}

// EncodeForwardKey encodes a port forward routing key for route advertisement.
// Returns length-prefixed key string (1 byte length + key).
func EncodeForwardKey(key string) []byte {
	buf := make([]byte, 1+len(key))
	buf[0] = uint8(len(key))
	copy(buf[1:], key)
	return buf
}

// EncodeForwardKeyWithTarget encodes a port forward routing key and target for route advertisement.
// Format: [1 byte key length][key][1 byte target length][target]
func EncodeForwardKeyWithTarget(key, target string) []byte {
	buf := make([]byte, 2+len(key)+len(target))
	buf[0] = uint8(len(key))
	copy(buf[1:], key)
	buf[1+len(key)] = uint8(len(target))
	copy(buf[2+len(key):], target)
	return buf
}

// DecodeForwardKey decodes a port forward routing key from route advertisement.
// Input is length-prefixed key string (1 byte length + key).
func DecodeForwardKey(prefix []byte) string {
	if len(prefix) < 1 {
		return ""
	}
	keyLen := int(prefix[0])
	if len(prefix) < 1+keyLen {
		return ""
	}
	return string(prefix[1 : 1+keyLen])
}

// DecodeForwardKeyAndTarget decodes a port forward routing key and target from route advertisement.
// Format: [1 byte key length][key][1 byte target length][target]
// Returns key and target strings.
func DecodeForwardKeyAndTarget(prefix []byte) (key, target string) {
	if len(prefix) < 1 {
		return "", ""
	}
	keyLen := int(prefix[0])
	if len(prefix) < 1+keyLen {
		return "", ""
	}
	key = string(prefix[1 : 1+keyLen])

	// Check if target is present
	targetOffset := 1 + keyLen
	if len(prefix) < targetOffset+1 {
		return key, ""
	}
	targetLen := int(prefix[targetOffset])
	if len(prefix) < targetOffset+1+targetLen {
		return key, ""
	}
	target = string(prefix[targetOffset+1 : targetOffset+1+targetLen])
	return key, target
}

// EncodeAgentPrefix encodes an agent ID for agent presence route advertisement.
// Returns the raw 16-byte agent ID.
func EncodeAgentPrefix(agentID identity.AgentID) []byte {
	buf := make([]byte, identity.IDSize)
	copy(buf, agentID[:])
	return buf
}

// DecodeAgentPrefix decodes an agent ID from agent presence route advertisement.
// Input is a 16-byte agent ID.
func DecodeAgentPrefix(prefix []byte) identity.AgentID {
	if len(prefix) < identity.IDSize {
		return identity.ZeroID
	}
	var id identity.AgentID
	copy(id[:], prefix[:identity.IDSize])
	return id
}

// RouteAdvertise is the payload for ROUTE_ADVERTISE frames.
type RouteAdvertise struct {
	OriginAgent       identity.AgentID
	OriginDisplayName string // Display name of the origin agent for topology visualization
	Sequence          uint64
	Routes            []Route
	Path              []identity.AgentID // Route path (plaintext; EncPath carries it on the wire)
	// EncPath wraps the path for the wire. The name is historical: path
	// encryption was removed (transit agents need the plaintext path), so
	// current agents always send Encrypted=false. The Encrypted=true branch
	// remains decodable for wire compatibility only.
	EncPath *EncryptedData
	SeenBy  []identity.AgentID
	// Comments is a parallel slice to Routes carrying the operator
	// comment for each prefix (empty string if unset). It is encoded as
	// an optional trailing field; older agents stop parsing after SeenBy
	// and silently ignore it. nil means "no comments" and is encoded as
	// a zero count.
	Comments []string
}

// Encode serializes RouteAdvertise to bytes.
// Format:
//
//	origin(16) + displayNameLen(1) + displayName + seq(8) + routeCount(1) + routes +
//	EncryptedData(flag+len+path) + seenByLen(1) + seenBy +
//	commentCount(1) + comments  (optional trailing field)
func (r *RouteAdvertise) Encode() []byte {
	// Prepare path data (encrypted or plaintext)
	encPath := r.EncPath
	if encPath == nil {
		encPath = &EncryptedData{Encrypted: false, Data: EncodePath(r.Path)}
	}
	encPathBytes := EncodeEncryptedData(encPath)

	// Calculate size
	size := 16 + 1 + len(r.OriginDisplayName) + 8 + 1
	size += routesEncodedSize(r.Routes)
	size += len(encPathBytes)
	size += 1 + len(r.SeenBy)*16
	// Optional trailing Comments block; omitted when empty so the
	// encoded message stays byte-compatible with pre-Comments senders.
	if len(r.Comments) > 0 {
		size += 1
		for _, c := range r.Comments {
			size += 1 + len(c)
		}
	}

	w := newBufferWriter(size)
	w.writeBytes(r.OriginAgent[:])
	w.writeString(r.OriginDisplayName)
	w.writeUint64(r.Sequence)
	w.writeUint8(uint8(len(r.Routes)))

	encodeRoutes(w, r.Routes)

	w.writeBytes(encPathBytes)
	w.writeAgentIDs(r.SeenBy)
	if len(r.Comments) > 0 {
		w.writeUint8(uint8(len(r.Comments)))
		for _, c := range r.Comments {
			w.writeString(c)
		}
	}

	return w.bytes()
}

// DecodeRouteAdvertise deserializes RouteAdvertise from bytes.
// Supports new format with encrypted path:
//
//	origin(16) + displayNameLen(1) + displayName + seq(8) + routeCount(1) + routes +
//	EncryptedData(flag+len+path) + seenByLen(1) + seenBy
func DecodeRouteAdvertise(buf []byte) (*RouteAdvertise, error) {
	if len(buf) < 28 { // Minimum size
		return nil, fmt.Errorf("%w: RouteAdvertise too short", ErrInvalidFrame)
	}

	rd := newBufferReader(buf, "RouteAdvertise")
	ra := &RouteAdvertise{
		OriginAgent:       rd.readAgentID(),
		OriginDisplayName: rd.readString(),
		Sequence:          rd.readUint64(),
	}
	if rd.err != nil {
		return nil, rd.err
	}

	routeCount := int(rd.readUint8())
	ra.Routes = decodeRoutes(rd, buf, routeCount)
	if rd.err != nil {
		return nil, rd.err
	}

	// Encrypted path data (uses offset directly due to consumed bytes return)
	encPath, consumed, err := DecodeEncryptedData(buf[rd.offset:])
	if err != nil {
		return nil, fmt.Errorf("decode path: %w", err)
	}
	rd.offset += consumed
	ra.EncPath = encPath

	// If not encrypted, decode path immediately
	if !encPath.Encrypted {
		path, err := DecodePath(encPath.Data)
		if err != nil {
			return nil, fmt.Errorf("decode path: %w", err)
		}
		ra.Path = path
	}

	ra.SeenBy = rd.readAgentIDs()
	if rd.err != nil {
		return nil, rd.err
	}

	// Optional trailing Comments block. Absent from older senders; a
	// mismatched count or read error is silently dropped because the
	// route advertisement above is already complete.
	if rd.remaining() > 0 {
		commentCount := int(rd.readUint8())
		if rd.err == nil && commentCount == len(ra.Routes) {
			comments := make([]string, commentCount)
			for i := 0; i < commentCount && rd.err == nil; i++ {
				comments[i] = rd.readString()
			}
			if rd.err == nil {
				ra.Comments = comments
			}
		}
		rd.err = nil
	}

	return ra, nil
}

// RouteWithdraw is the payload for ROUTE_WITHDRAW frames.
type RouteWithdraw struct {
	OriginAgent identity.AgentID
	Sequence    uint64
	Routes      []Route
	SeenBy      []identity.AgentID
}

// Encode serializes RouteWithdraw to bytes.
func (r *RouteWithdraw) Encode() []byte {
	// Calculate size: origin + seq + routeCount + routes + seenBy
	size := 16 + 8 + 1
	size += routesEncodedSize(r.Routes)
	size += 1 + len(r.SeenBy)*16

	w := newBufferWriter(size)
	w.writeBytes(r.OriginAgent[:])
	w.writeUint64(r.Sequence)
	w.writeUint8(uint8(len(r.Routes)))

	encodeRoutes(w, r.Routes)

	w.writeAgentIDs(r.SeenBy)
	return w.bytes()
}

// DecodeRouteWithdraw deserializes RouteWithdraw from bytes.
func DecodeRouteWithdraw(buf []byte) (*RouteWithdraw, error) {
	if len(buf) < 26 { // 16 + 8 + 1 + 1
		return nil, fmt.Errorf("%w: RouteWithdraw too short", ErrInvalidFrame)
	}

	rd := newBufferReader(buf, "RouteWithdraw")
	rw := &RouteWithdraw{
		OriginAgent: rd.readAgentID(),
		Sequence:    rd.readUint64(),
	}

	routeCount := int(rd.readUint8())
	rw.Routes = decodeRoutes(rd, buf, routeCount)
	if rd.err != nil {
		return nil, rd.err
	}

	rw.SeenBy = rd.readAgentIDs()
	if rd.err != nil {
		return nil, rd.err
	}

	return rw, nil
}

// ============================================================================
// Encrypted data wrappers for management key encryption
// ============================================================================

// EncryptedData wraps data that may be encrypted with the management key.
// Used for NodeInfo and route paths to protect mesh topology from compromised agents.
type EncryptedData struct {
	Encrypted bool   // True if Data contains encrypted blob
	Data      []byte // Plaintext bytes OR encrypted blob (ephemeral_pub + nonce + ciphertext + tag)
}

// EncodeEncryptedData encodes an EncryptedData wrapper to bytes.
// Format: Encrypted(1) + DataLen(2) + Data
func EncodeEncryptedData(e *EncryptedData) []byte {
	w := newBufferWriter(1 + 2 + len(e.Data))
	w.writeBool(e.Encrypted)
	w.writeUint16(uint16(len(e.Data)))
	w.writeBytes(e.Data)
	return w.bytes()
}

// DecodeEncryptedData decodes an EncryptedData wrapper from bytes.
// Returns the wrapper and the number of bytes consumed.
func DecodeEncryptedData(buf []byte) (*EncryptedData, int, error) {
	if len(buf) < 3 {
		return nil, 0, fmt.Errorf("%w: EncryptedData too short", ErrInvalidFrame)
	}

	r := newBufferReader(buf, "EncryptedData")
	e := &EncryptedData{
		Encrypted: r.readBool(),
	}
	dataLen := int(r.readUint16())
	e.Data = r.readBytes(dataLen)

	if r.err != nil {
		return nil, 0, r.err
	}
	return e, 3 + dataLen, nil
}

// ============================================================================
// Node info frames
// ============================================================================

// PeerConnectionInfo describes a peer connection for an agent.
// Used in NodeInfo to advertise connected peers to the mesh.
type PeerConnectionInfo struct {
	PeerID    [16]byte // Remote peer AgentID
	Transport string   // Transport type: "quic", "h2", "ws"
	RTTMs     int64    // Round-trip time in milliseconds (0 if unknown)
	IsDialer  bool     // True if this agent initiated the connection
}

// MaxPeersInNodeInfo is the maximum number of peers to include in NodeInfo.
const MaxPeersInNodeInfo = 50

// MaxForwardListenersInNodeInfo is the maximum number of forward listeners to include in NodeInfo.
const MaxForwardListenersInNodeInfo = 20

// MaxShellsInNodeInfo is the maximum number of shells to include in NodeInfo.
const MaxShellsInNodeInfo = 10

// MaxAdvertisedListenersInNodeInfo is the maximum number of advertised
// listeners to include in NodeInfo.
const MaxAdvertisedListenersInNodeInfo = 8

// ForwardListenerInfo contains port forward listener information for NodeInfo.
// Used to advertise which agents have listeners for specific forward routing keys.
type ForwardListenerInfo struct {
	Key     string // Routing key (e.g., "web-server")
	Address string // Listen address (e.g., ":8080", "0.0.0.0:443")
}

// AdvertisedListener describes a listener that an agent advertises to the
// mesh as a public dial target. JSON tags are used by the topology API;
// the binary NodeInfo encoder ignores them.
type AdvertisedListener struct {
	Transport string `json:"transport"`      // "quic" | "h2" | "ws"
	Address   string `json:"address"`        // host:port (no path)
	Path      string `json:"path,omitempty"` // h2/ws only
}

// NodeInfo contains metadata about an agent in the mesh.
type NodeInfo struct {
	DisplayName         string                 // Human-readable name (from config)
	Hostname            string                 // System hostname
	OS                  string                 // Operating system (runtime.GOOS)
	Arch                string                 // Architecture (runtime.GOARCH)
	Version             string                 // Agent version
	StartTime           int64                  // Agent start time (Unix timestamp)
	IPAddresses         []string               // Local IP addresses (non-loopback)
	Peers               []PeerConnectionInfo   // Connected peers (max 50)
	PublicKey           [EphemeralKeySize]byte // Agent's static X25519 public key for E2E encryption
	UDPEnabled          bool                   // UDP relay enabled (for exit agents)
	ForwardListeners    []ForwardListenerInfo  // Port forward listeners (for ingress agents)
	Shells              []string               // Available shells (e.g., ["bash", "sh", "zsh"])
	FileTransferEnabled bool                   // File transfer enabled (for exit agents)
	ShellEnabled        bool                   // Shell access enabled (for exit agents)
	IcmpEnabled         bool                   // ICMP echo (ping) handler is running
	AdvertisedListeners []AdvertisedListener   // Public dial targets advertised by this agent
	PID                 int                    // OS process ID of the agent (os.Getpid() at collection time)
}

// EncodeNodeInfo encodes just the NodeInfo portion to bytes.
// This is used for encryption - the returned bytes can be encrypted with the management key.
func EncodeNodeInfo(info *NodeInfo) []byte {
	// Limit peers to max
	peers := info.Peers
	if len(peers) > MaxPeersInNodeInfo {
		peers = peers[:MaxPeersInNodeInfo]
	}

	// Limit forward listeners to max
	forwardListeners := info.ForwardListeners
	if len(forwardListeners) > MaxForwardListenersInNodeInfo {
		forwardListeners = forwardListeners[:MaxForwardListenersInNodeInfo]
	}

	// Limit shells to max
	shells := info.Shells
	if len(shells) > MaxShellsInNodeInfo {
		shells = shells[:MaxShellsInNodeInfo]
	}

	// Limit advertised listeners to max
	advertisedListeners := info.AdvertisedListeners
	if len(advertisedListeners) > MaxAdvertisedListenersInNodeInfo {
		advertisedListeners = advertisedListeners[:MaxAdvertisedListenersInNodeInfo]
	}

	// Calculate size
	size := 1 + len(info.DisplayName)
	size += 1 + len(info.Hostname)
	size += 1 + len(info.OS)
	size += 1 + len(info.Arch)
	size += 1 + len(info.Version)
	size += 8 // StartTime
	size += 1 // IPCount
	for _, ip := range info.IPAddresses {
		size += 1 + len(ip)
	}
	size += 1 // PeerCount
	for _, peer := range peers {
		size += 16 + 1 + len(peer.Transport) + 8 + 1
	}
	size += EphemeralKeySize + 1 // PublicKey + UDPEnabled
	size += 1                    // ForwardListenerCount
	for _, fl := range forwardListeners {
		size += 1 + len(fl.Key) + 1 + len(fl.Address)
	}
	size += 1 // ShellCount
	for _, sh := range shells {
		size += 1 + len(sh)
	}
	size += 1 // FileTransferEnabled
	size += 1 // ShellEnabled
	size += 1 // IcmpEnabled
	size += 1 // AdvertisedListenerCount
	for _, al := range advertisedListeners {
		size += 1 + len(al.Transport) + 1 + len(al.Address) + 1 + len(al.Path)
	}
	size += 8 // PID

	w := newBufferWriter(size)
	w.writeString(info.DisplayName)
	w.writeString(info.Hostname)
	w.writeString(info.OS)
	w.writeString(info.Arch)
	w.writeString(info.Version)
	w.writeUint64(uint64(info.StartTime))

	// IPAddresses
	w.writeUint8(uint8(len(info.IPAddresses)))
	for _, ip := range info.IPAddresses {
		w.writeString(ip)
	}

	// Peers
	w.writeUint8(uint8(len(peers)))
	for _, peer := range peers {
		w.writeBytes(peer.PeerID[:])
		w.writeString(peer.Transport)
		w.writeUint64(uint64(peer.RTTMs))
		w.writeBool(peer.IsDialer)
	}

	w.writeBytes(info.PublicKey[:])
	w.writeBool(info.UDPEnabled)

	// ForwardListeners
	w.writeUint8(uint8(len(forwardListeners)))
	for _, fl := range forwardListeners {
		w.writeString(fl.Key)
		w.writeString(fl.Address)
	}

	// Shells
	w.writeUint8(uint8(len(shells)))
	for _, sh := range shells {
		w.writeString(sh)
	}

	// FileTransferEnabled
	w.writeBool(info.FileTransferEnabled)

	// ShellEnabled
	w.writeBool(info.ShellEnabled)

	// IcmpEnabled
	w.writeBool(info.IcmpEnabled)

	// AdvertisedListeners
	w.writeUint8(uint8(len(advertisedListeners)))
	for _, al := range advertisedListeners {
		w.writeString(al.Transport)
		w.writeString(al.Address)
		w.writeString(al.Path)
	}

	// PID
	w.writeUint64(uint64(info.PID))

	return w.bytes()
}

// DecodeNodeInfo decodes just the NodeInfo portion from bytes.
// This is the inverse of EncodeNodeInfo, used after decryption.
func DecodeNodeInfo(buf []byte) (*NodeInfo, error) {
	if len(buf) < 5+EphemeralKeySize {
		return nil, fmt.Errorf("%w: NodeInfo too short", ErrInvalidFrame)
	}

	r := newBufferReader(buf, "NodeInfo")
	info := &NodeInfo{
		DisplayName: r.readString(),
		Hostname:    r.readString(),
		OS:          r.readString(),
		Arch:        r.readString(),
		Version:     r.readString(),
		StartTime:   int64(r.readUint64()),
	}
	if r.err != nil {
		return nil, r.err
	}

	// IPAddresses
	ipCount := int(r.readUint8())
	info.IPAddresses = make([]string, ipCount)
	for i := 0; i < ipCount && r.err == nil; i++ {
		info.IPAddresses[i] = r.readString()
	}
	if r.err != nil {
		return nil, r.err
	}

	// Peers (with graceful truncation for incomplete data)
	peerCount := int(r.readUint8())
	if peerCount > MaxPeersInNodeInfo {
		peerCount = MaxPeersInNodeInfo
	}
	info.Peers = make([]PeerConnectionInfo, 0, peerCount)
	for i := 0; i < peerCount; i++ {
		if r.remaining() < 16 {
			break
		}
		var peer PeerConnectionInfo
		copy(peer.PeerID[:], r.readBytes(16))
		peer.Transport = r.readString()
		if r.remaining() < 9 { // 8 (RTTMs) + 1 (IsDialer)
			break
		}
		peer.RTTMs = int64(r.readUint64())
		peer.IsDialer = r.readBool()
		info.Peers = append(info.Peers, peer)
	}

	// PublicKey
	info.PublicKey = r.readEphemeralKey()
	if r.err != nil {
		return nil, r.err
	}

	// UDP info (optional - for backward compatibility with older agents)
	if r.remaining() > 0 {
		info.UDPEnabled = r.readBool()
	}

	// ForwardListeners (optional - for backward compatibility with older agents)
	if r.remaining() > 0 {
		listenerCount := int(r.readUint8())
		if listenerCount > MaxForwardListenersInNodeInfo {
			listenerCount = MaxForwardListenersInNodeInfo
		}
		info.ForwardListeners = make([]ForwardListenerInfo, 0, listenerCount)
		for i := 0; i < listenerCount && r.remaining() > 0; i++ {
			key := r.readString()
			if r.remaining() < 1 {
				break
			}
			address := r.readString()
			if r.err != nil {
				break
			}
			info.ForwardListeners = append(info.ForwardListeners, ForwardListenerInfo{
				Key:     key,
				Address: address,
			})
		}
	}

	// Shells (optional - for backward compatibility with older agents)
	if r.remaining() > 0 {
		shellCount := int(r.readUint8())
		if shellCount > MaxShellsInNodeInfo {
			shellCount = MaxShellsInNodeInfo
		}
		info.Shells = make([]string, 0, shellCount)
		for i := 0; i < shellCount && r.remaining() > 0; i++ {
			sh := r.readString()
			if r.err != nil {
				break
			}
			info.Shells = append(info.Shells, sh)
		}
	}

	// FileTransferEnabled (optional - for backward compatibility with older agents)
	if r.remaining() > 0 {
		info.FileTransferEnabled = r.readBool()
	}

	// ShellEnabled (optional - for backward compatibility with older agents)
	if r.remaining() > 0 {
		info.ShellEnabled = r.readBool()
	}

	// IcmpEnabled (optional - for backward compatibility with older agents)
	if r.remaining() > 0 {
		info.IcmpEnabled = r.readBool()
	}

	// AdvertisedListeners (optional - for backward compatibility with older agents)
	if r.remaining() > 0 {
		listenerCount := int(r.readUint8())
		if listenerCount > MaxAdvertisedListenersInNodeInfo {
			listenerCount = MaxAdvertisedListenersInNodeInfo
		}
		info.AdvertisedListeners = make([]AdvertisedListener, 0, listenerCount)
		for i := 0; i < listenerCount && r.remaining() > 0; i++ {
			transport := r.readString()
			if r.remaining() < 1 {
				break
			}
			address := r.readString()
			if r.remaining() < 1 {
				break
			}
			path := r.readString()
			if r.err != nil {
				break
			}
			info.AdvertisedListeners = append(info.AdvertisedListeners, AdvertisedListener{
				Transport: transport,
				Address:   address,
				Path:      path,
			})
		}
	}

	// PID (optional - for backward compatibility with older agents)
	if r.remaining() >= 8 {
		info.PID = int(r.readUint64())
	}

	return info, nil
}

// EncodePath encodes a path (slice of AgentIDs) to bytes.
// This is used for encryption - the returned bytes can be encrypted with the management key.
func EncodePath(path []identity.AgentID) []byte {
	w := newBufferWriter(1 + len(path)*16)
	w.writeAgentIDs(path)
	return w.bytes()
}

// DecodePath decodes a path from bytes.
// This is the inverse of EncodePath, used after decryption.
func DecodePath(buf []byte) ([]identity.AgentID, error) {
	if len(buf) < 1 {
		return nil, fmt.Errorf("%w: Path too short", ErrInvalidFrame)
	}
	r := newBufferReader(buf, "Path")
	ids := r.readAgentIDs()
	if r.err != nil {
		return nil, r.err
	}
	return ids, nil
}

// NodeInfoAdvertise is the payload for NODE_INFO_ADVERTISE frames.
// Used to announce node metadata to all agents in the mesh.
type NodeInfoAdvertise struct {
	OriginAgent identity.AgentID   // Agent advertising its info
	Sequence    uint64             // Monotonically increasing sequence
	Info        NodeInfo           // Node metadata (may be decrypted from EncryptedInfo)
	EncInfo     *EncryptedData     // Encrypted NodeInfo data (nil if not using encryption)
	SeenBy      []identity.AgentID // Loop prevention (agents that have seen this)
}

// Encode serializes NodeInfoAdvertise to bytes.
// New format (v2) with encryption support:
//
//	OriginAgent(16) + Sequence(8) + EncryptedData(flag+len+data) + SeenByLen(1) + SeenBy(N*16)
//
// Where EncryptedData contains NodeInfo (plaintext or encrypted blob).
func (n *NodeInfoAdvertise) Encode() []byte {
	// Prepare NodeInfo data (encrypted or plaintext)
	encData := n.EncInfo
	if encData == nil {
		encData = &EncryptedData{Encrypted: false, Data: EncodeNodeInfo(&n.Info)}
	}
	encDataBytes := EncodeEncryptedData(encData)

	w := newBufferWriter(16 + 8 + len(encDataBytes) + 1 + len(n.SeenBy)*16)
	w.writeBytes(n.OriginAgent[:])
	w.writeUint64(n.Sequence)
	w.writeBytes(encDataBytes)
	w.writeAgentIDs(n.SeenBy)

	return w.bytes()
}

// DecodeNodeInfoAdvertise deserializes NodeInfoAdvertise from bytes.
// Supports new format (v2) with encryption:
//
//	OriginAgent(16) + Sequence(8) + EncryptedData(flag+len+data) + SeenByLen(1) + SeenBy(N*16)
func DecodeNodeInfoAdvertise(buf []byte) (*NodeInfoAdvertise, error) {
	if len(buf) < 28 { // Minimum: 16 + 8 + 3 (encData min) + 1 (seenByLen)
		return nil, fmt.Errorf("%w: NodeInfoAdvertise too short", ErrInvalidFrame)
	}

	r := newBufferReader(buf, "NodeInfoAdvertise")
	n := &NodeInfoAdvertise{
		OriginAgent: r.readAgentID(),
		Sequence:    r.readUint64(),
	}

	// EncryptedData (uses offset directly due to consumed bytes return)
	encData, consumed, err := DecodeEncryptedData(buf[r.offset:])
	if err != nil {
		return nil, err
	}
	r.offset += consumed
	n.EncInfo = encData

	// If not encrypted, decode NodeInfo immediately
	if !encData.Encrypted {
		info, err := DecodeNodeInfo(encData.Data)
		if err != nil {
			return nil, fmt.Errorf("decode NodeInfo: %w", err)
		}
		n.Info = *info
	}

	n.SeenBy = r.readAgentIDs()
	if r.err != nil {
		return nil, r.err
	}

	return n, nil
}

// ============================================================================
// Control frames (for remote status queries)
// ============================================================================

// ControlRequest is the payload for CONTROL_REQUEST frames.
// Used to request status or other information from remote agents.
type ControlRequest struct {
	RequestID   uint64             // Unique request ID for correlation
	ControlType uint8              // Type of control request (ControlTypeStatus, etc.)
	TargetAgent identity.AgentID   // Target agent to forward request to (zero = this agent)
	Path        []identity.AgentID // Remaining path to target
	Data        []byte             // Optional request data (e.g., RPC request payload)
}

// Encode serializes ControlRequest to bytes.
func (c *ControlRequest) Encode() []byte {
	// Format: RequestID(8) + ControlType(1) + TargetAgent(16) + PathLen(1) + Path(N*16) + DataLen(4) + Data
	w := newBufferWriter(8 + 1 + 16 + 1 + len(c.Path)*16 + 4 + len(c.Data))
	w.writeUint64(c.RequestID)
	w.writeUint8(c.ControlType)
	w.writeBytes(c.TargetAgent[:])
	w.writeAgentIDs(c.Path)
	w.writeUint32(uint32(len(c.Data)))
	w.writeBytes(c.Data)

	return w.bytes()
}

// DecodeControlRequest deserializes ControlRequest from bytes.
func DecodeControlRequest(buf []byte) (*ControlRequest, error) {
	if len(buf) < 30 { // 8 + 1 + 16 + 1 + 4 (minimum with empty path and data)
		return nil, fmt.Errorf("%w: ControlRequest too short", ErrInvalidFrame)
	}

	r := newBufferReader(buf, "ControlRequest")
	c := &ControlRequest{
		RequestID:   r.readUint64(),
		ControlType: r.readUint8(),
		TargetAgent: r.readAgentID(),
		Path:        r.readAgentIDs(),
	}

	dataLen := int(r.readUint32())
	if dataLen > 0 {
		c.Data = r.readBytes(dataLen)
	}

	if r.err != nil {
		return nil, r.err
	}
	return c, nil
}

// ControlResponse is the payload for CONTROL_RESPONSE frames.
// Contains the response data from a control request.
type ControlResponse struct {
	RequestID   uint64 // Matches the request ID
	ControlType uint8  // Type of control response
	Success     bool   // Whether the request succeeded
	HasMore     bool   // More chunks follow with the same RequestID; receivers accumulate until HasMore=false
	Data        []byte // Response data (Prometheus text, JSON status, etc.)
}

// ControlResponseHeaderSize is the fixed overhead of an encoded ControlResponse:
// RequestID(8) + ControlType(1) + Flags(1) + DataLen(2).
const ControlResponseHeaderSize = 12

// ControlResponseMaxChunkSize is the largest Data payload that fits in one
// CONTROL_RESPONSE frame. Callers sending larger responses must split and
// set HasMore on all but the final chunk.
const ControlResponseMaxChunkSize = MaxPayloadSize - ControlResponseHeaderSize

// Encode serializes ControlResponse to bytes. Data must not exceed
// ControlResponseMaxChunkSize; anything beyond that is dropped as a safety
// net (the caller is expected to chunk via HasMore).
func (c *ControlResponse) Encode() []byte {
	data := c.Data
	if len(data) > ControlResponseMaxChunkSize {
		data = data[:ControlResponseMaxChunkSize]
	}

	// Flags byte: bit 0 = Success, bit 1 = HasMore. Legacy receivers
	// interpret this byte as a plain Success bool (non-zero = true); since
	// we never chunk failed responses, HasMore=true implies Success=true,
	// so the byte stays non-zero for legacy compatibility on the success
	// path (they just see only the first chunk).
	var flags uint8
	if c.Success {
		flags |= 0x01
	}
	if c.HasMore {
		flags |= 0x02
	}

	w := newBufferWriter(ControlResponseHeaderSize + len(data))
	w.writeUint64(c.RequestID)
	w.writeUint8(c.ControlType)
	w.writeUint8(flags)
	w.writeUint16(uint16(len(data)))
	w.writeBytes(data)

	return w.bytes()
}

// DecodeControlResponse deserializes ControlResponse from bytes.
func DecodeControlResponse(buf []byte) (*ControlResponse, error) {
	if len(buf) < ControlResponseHeaderSize {
		return nil, fmt.Errorf("%w: ControlResponse too short", ErrInvalidFrame)
	}

	r := newBufferReader(buf, "ControlResponse")
	c := &ControlResponse{
		RequestID:   r.readUint64(),
		ControlType: r.readUint8(),
	}

	flags := r.readUint8()
	c.Success = flags&0x01 != 0
	c.HasMore = flags&0x02 != 0

	dataLen := int(r.readUint16())
	c.Data = r.readBytes(dataLen)

	if r.err != nil {
		return nil, r.err
	}
	return c, nil
}

// ============================================================================
// UDP frames (for SOCKS5 UDP ASSOCIATE)
// ============================================================================

// UDPOpen is the payload for UDP_OPEN frames.
// Requests a UDP association through the mesh network.
type UDPOpen struct {
	RequestID       uint64                 // Stable across hops for correlation
	AddressType     uint8                  // Bind address type (typically 0x01 for IPv4)
	Address         []byte                 // Bind address (usually 0.0.0.0)
	Port            uint16                 // Bind port (usually 0)
	TTL             uint8                  // Hop limit
	RemainingPath   []identity.AgentID     // Route to exit agent
	EphemeralPubKey [EphemeralKeySize]byte // Initiator's ephemeral public key for E2E encryption
}

// Encode serializes UDPOpen to bytes.
func (u *UDPOpen) Encode() []byte {
	return (&openWire{
		RequestID:       u.RequestID,
		AddressType:     u.AddressType,
		Address:         u.Address,
		Port:            u.Port,
		TTL:             u.TTL,
		RemainingPath:   u.RemainingPath,
		EphemeralPubKey: u.EphemeralPubKey,
	}).encode()
}

// DecodeUDPOpen deserializes UDPOpen from bytes.
func DecodeUDPOpen(buf []byte) (*UDPOpen, error) {
	o, err := decodeOpenWire(buf, "UDPOpen")
	if err != nil {
		return nil, err
	}
	return &UDPOpen{
		RequestID:       o.RequestID,
		AddressType:     o.AddressType,
		Address:         o.Address,
		Port:            o.Port,
		TTL:             o.TTL,
		RemainingPath:   o.RemainingPath,
		EphemeralPubKey: o.EphemeralPubKey,
	}, nil
}

// UDPOpenAck is the payload for UDP_OPEN_ACK frames.
// Confirms the UDP association with relay address information.
type UDPOpenAck struct {
	RequestID       uint64                 // Correlation ID
	BoundAddrType   uint8                  // Relay address type
	BoundAddr       []byte                 // Relay bind address
	BoundPort       uint16                 // Relay bind port
	EphemeralPubKey [EphemeralKeySize]byte // Responder's ephemeral public key for E2E encryption
}

// Encode serializes UDPOpenAck to bytes.
func (u *UDPOpenAck) Encode() []byte {
	return (&ackWire{
		RequestID:       u.RequestID,
		BoundAddrType:   u.BoundAddrType,
		BoundAddr:       u.BoundAddr,
		BoundPort:       u.BoundPort,
		EphemeralPubKey: u.EphemeralPubKey,
	}).encode()
}

// DecodeUDPOpenAck deserializes UDPOpenAck from bytes.
func DecodeUDPOpenAck(buf []byte) (*UDPOpenAck, error) {
	a, err := decodeAckWire(buf, "UDPOpenAck")
	if err != nil {
		return nil, err
	}
	return &UDPOpenAck{
		RequestID:       a.RequestID,
		BoundAddrType:   a.BoundAddrType,
		BoundAddr:       a.BoundAddr,
		BoundPort:       a.BoundPort,
		EphemeralPubKey: a.EphemeralPubKey,
	}, nil
}

// UDPOpenErr is the payload for UDP_OPEN_ERR frames.
// Indicates failure to establish the UDP association.
type UDPOpenErr struct {
	RequestID uint64 // Correlation ID
	ErrorCode uint16 // Error code (ErrUDPDisabled, etc.)
	Message   string // Human-readable error message
}

// Encode serializes UDPOpenErr to bytes.
func (u *UDPOpenErr) Encode() []byte {
	return (&errWire{RequestID: u.RequestID, ErrorCode: u.ErrorCode, Message: u.Message}).encode()
}

// DecodeUDPOpenErr deserializes UDPOpenErr from bytes.
func DecodeUDPOpenErr(buf []byte) (*UDPOpenErr, error) {
	e, err := decodeErrWire(buf, "UDPOpenErr")
	if err != nil {
		return nil, err
	}
	return &UDPOpenErr{RequestID: e.RequestID, ErrorCode: e.ErrorCode, Message: e.Message}, nil
}

// UDPDatagram is the payload for UDP_DATAGRAM frames.
// Carries a single UDP datagram through the mesh.
type UDPDatagram struct {
	AddressType uint8  // Destination address type
	Address     []byte // Destination IP or domain
	Port        uint16 // Destination port
	Data        []byte // UDP payload (encrypted with E2E session key)
}

// MaxUDPDatagramSize is the maximum UDP payload size (typical MTU - IP/UDP headers).
const MaxUDPDatagramSize = 1472

// Encode serializes UDPDatagram to bytes.
func (u *UDPDatagram) Encode() []byte {
	// Format: AddrType(1) + Address(var) + Port(2) + DataLen(2) + Data
	w := newBufferWriter(1 + len(u.Address) + 2 + 2 + len(u.Data))
	w.writeUint8(u.AddressType)
	w.writeBytes(u.Address)
	w.writeUint16(u.Port)
	w.writeUint16(uint16(len(u.Data)))
	w.writeBytes(u.Data)

	return w.bytes()
}

// DecodeUDPDatagram deserializes UDPDatagram from bytes.
func DecodeUDPDatagram(buf []byte) (*UDPDatagram, error) {
	if len(buf) < 6 { // 1 + 1 + 2 + 2 minimum (IPv4 with empty data)
		return nil, fmt.Errorf("%w: UDPDatagram too short", ErrInvalidFrame)
	}

	r := newBufferReader(buf, "UDPDatagram")
	u := &UDPDatagram{
		AddressType: r.readUint8(),
	}

	// Determine address length based on type
	var addrLen int
	if u.AddressType == AddrTypeDomain {
		if r.offset >= len(buf) {
			return nil, fmt.Errorf("%w: UDPDatagram domain length missing", ErrInvalidFrame)
		}
		addrLen = 1 + int(buf[r.offset])
	} else {
		var err error
		addrLen, err = addressLength(u.AddressType, 0)
		if err != nil {
			return nil, err
		}
	}

	u.Address = r.readBytes(addrLen)
	u.Port = r.readUint16()
	dataLen := int(r.readUint16())
	u.Data = r.readBytes(dataLen)

	if r.err != nil {
		return nil, r.err
	}
	return u, nil
}

// UDPClose is the payload for UDP_CLOSE frames.
// Terminates a UDP association.
type UDPClose struct {
	Reason uint8 // Close reason code
}

// UDP close reason codes
const (
	UDPCloseNormal     uint8 = 0 // Normal termination
	UDPCloseTimeout    uint8 = 1 // Idle timeout
	UDPCloseError      uint8 = 2 // Error occurred
	UDPCloseTCPClosed  uint8 = 3 // TCP control connection closed
	UDPCloseAdminClose uint8 = 4 // Administrative close
)

// Encode serializes UDPClose to bytes.
func (u *UDPClose) Encode() []byte {
	return encodeCloseReason(u.Reason)
}

// DecodeUDPClose deserializes UDPClose from bytes.
func DecodeUDPClose(buf []byte) (*UDPClose, error) {
	reason, err := decodeCloseReason(buf, "UDPClose")
	if err != nil {
		return nil, err
	}
	return &UDPClose{Reason: reason}, nil
}

// ============================================================================
// ICMP frames (for ICMP echo/ping through mesh)
// ============================================================================

// ICMPOpen is the payload for ICMP_OPEN frames.
// Requests an ICMP echo session through the mesh network.
type ICMPOpen struct {
	RequestID       uint64                 // Stable across hops for correlation
	DestIP          []byte                 // Destination IP (4 bytes for IPv4)
	TTL             uint8                  // Hop limit
	RemainingPath   []identity.AgentID     // Route to exit agent
	EphemeralPubKey [EphemeralKeySize]byte // Initiator's ephemeral public key for E2E encryption
}

// Encode serializes ICMPOpen to bytes.
func (i *ICMPOpen) Encode() []byte {
	// Format: RequestID(8) + DestIPLen(1) + DestIP + TTL(1) + PathLen(1) + Path + EphemeralPubKey(32)
	w := newBufferWriter(8 + 1 + len(i.DestIP) + 1 + 1 + len(i.RemainingPath)*16 + EphemeralKeySize)
	w.writeUint64(i.RequestID)
	w.writeUint8(uint8(len(i.DestIP)))
	w.writeBytes(i.DestIP)
	w.writeUint8(i.TTL)
	w.writeAgentIDs(i.RemainingPath)
	w.writeBytes(i.EphemeralPubKey[:])

	return w.bytes()
}

// DecodeICMPOpen deserializes ICMPOpen from bytes.
func DecodeICMPOpen(buf []byte) (*ICMPOpen, error) {
	if len(buf) < 11+EphemeralKeySize { // 8 + 1 + 1 + 1 + 32 minimum (empty IP, no path)
		return nil, fmt.Errorf("%w: ICMPOpen too short", ErrInvalidFrame)
	}

	r := newBufferReader(buf, "ICMPOpen")
	i := &ICMPOpen{
		RequestID: r.readUint64(),
	}

	destIPLen := int(r.readUint8())
	i.DestIP = r.readBytes(destIPLen)
	i.TTL = r.readUint8()
	i.RemainingPath = r.readAgentIDs()
	i.EphemeralPubKey = r.readEphemeralKey()

	if r.err != nil {
		return nil, r.err
	}
	return i, nil
}

// GetDestinationIP returns the destination IP as net.IP.
func (i *ICMPOpen) GetDestinationIP() net.IP {
	return net.IP(i.DestIP)
}

// ICMPOpenAck is the payload for ICMP_OPEN_ACK frames.
// Confirms the ICMP echo session is established.
type ICMPOpenAck struct {
	RequestID       uint64                 // Correlation ID
	EphemeralPubKey [EphemeralKeySize]byte // Responder's ephemeral public key for E2E encryption
}

// Encode serializes ICMPOpenAck to bytes.
func (i *ICMPOpenAck) Encode() []byte {
	w := newBufferWriter(8 + EphemeralKeySize)
	w.writeUint64(i.RequestID)
	w.writeBytes(i.EphemeralPubKey[:])

	return w.bytes()
}

// DecodeICMPOpenAck deserializes ICMPOpenAck from bytes.
func DecodeICMPOpenAck(buf []byte) (*ICMPOpenAck, error) {
	if len(buf) < 8+EphemeralKeySize {
		return nil, fmt.Errorf("%w: ICMPOpenAck too short", ErrInvalidFrame)
	}

	r := newBufferReader(buf, "ICMPOpenAck")
	i := &ICMPOpenAck{
		RequestID:       r.readUint64(),
		EphemeralPubKey: r.readEphemeralKey(),
	}

	if r.err != nil {
		return nil, r.err
	}
	return i, nil
}

// ICMPOpenErr is the payload for ICMP_OPEN_ERR frames.
// Indicates failure to establish the ICMP echo session.
type ICMPOpenErr struct {
	RequestID uint64 // Correlation ID
	ErrorCode uint16 // Error code (ErrICMPDisabled, etc.)
	Message   string // Human-readable error message
}

// Encode serializes ICMPOpenErr to bytes.
func (i *ICMPOpenErr) Encode() []byte {
	return (&errWire{RequestID: i.RequestID, ErrorCode: i.ErrorCode, Message: i.Message}).encode()
}

// DecodeICMPOpenErr deserializes ICMPOpenErr from bytes.
func DecodeICMPOpenErr(buf []byte) (*ICMPOpenErr, error) {
	e, err := decodeErrWire(buf, "ICMPOpenErr")
	if err != nil {
		return nil, err
	}
	return &ICMPOpenErr{RequestID: e.RequestID, ErrorCode: e.ErrorCode, Message: e.Message}, nil
}

// ICMPEcho is the payload for ICMP_ECHO frames.
// Carries ICMP echo request/reply data through the mesh.
type ICMPEcho struct {
	Identifier uint16 // Original ICMP identifier
	Sequence   uint16 // ICMP sequence number
	IsReply    bool   // false = echo request, true = echo reply
	SrcIP      []byte // Source IP of echo reply (4=IPv4, 16=IPv6, 0=request)
	Data       []byte // Echo payload (encrypted with E2E session key)
}

// MaxICMPEchoDataSize is the maximum ICMP echo payload size.
const MaxICMPEchoDataSize = 1472

// Encode serializes ICMPEcho to bytes.
func (i *ICMPEcho) Encode() []byte {
	// Format: Identifier(2) + Sequence(2) + IsReply(1) + SrcIPLen(1) + SrcIP(0/4/16) + DataLen(2) + Data
	w := newBufferWriter(2 + 2 + 1 + 1 + len(i.SrcIP) + 2 + len(i.Data))
	w.writeUint16(i.Identifier)
	w.writeUint16(i.Sequence)
	w.writeBool(i.IsReply)
	w.writeUint8(uint8(len(i.SrcIP)))
	w.writeBytes(i.SrcIP)
	w.writeUint16(uint16(len(i.Data)))
	w.writeBytes(i.Data)

	return w.bytes()
}

// DecodeICMPEcho deserializes ICMPEcho from bytes.
func DecodeICMPEcho(buf []byte) (*ICMPEcho, error) {
	if len(buf) < 8 { // 2 + 2 + 1 + 1 + 2 minimum (no SrcIP, empty data)
		return nil, fmt.Errorf("%w: ICMPEcho too short", ErrInvalidFrame)
	}

	r := newBufferReader(buf, "ICMPEcho")
	i := &ICMPEcho{
		Identifier: r.readUint16(),
		Sequence:   r.readUint16(),
		IsReply:    r.readBool(),
	}

	srcIPLen := int(r.readUint8())
	if srcIPLen > 0 {
		i.SrcIP = r.readBytes(srcIPLen)
	}

	dataLen := int(r.readUint16())
	i.Data = r.readBytes(dataLen)

	if r.err != nil {
		return nil, r.err
	}
	return i, nil
}

// ICMPClose is the payload for ICMP_CLOSE frames.
// Terminates an ICMP echo session.
type ICMPClose struct {
	Reason uint8 // Close reason code (ICMPCloseNormal, ICMPCloseTimeout, ICMPCloseError)
}

// Encode serializes ICMPClose to bytes.
func (i *ICMPClose) Encode() []byte {
	return encodeCloseReason(i.Reason)
}

// DecodeICMPClose deserializes ICMPClose from bytes.
func DecodeICMPClose(buf []byte) (*ICMPClose, error) {
	reason, err := decodeCloseReason(buf, "ICMPClose")
	if err != nil {
		return nil, err
	}
	return &ICMPClose{Reason: reason}, nil
}

// encodeCloseReason serializes the single-reason-byte close payloads
// (UDPClose, ICMPClose). The reason enums differ per protocol but the wire
// layout is one byte.
func encodeCloseReason(reason uint8) []byte {
	return []byte{reason}
}

// decodeCloseReason deserializes a single-reason-byte close payload.
func decodeCloseReason(buf []byte, name string) (uint8, error) {
	if len(buf) < 1 {
		return 0, fmt.Errorf("%w: %s too short", ErrInvalidFrame, name)
	}
	return buf[0], nil
}

// ============================================================================
// Sleep/Wake control frames
// ============================================================================

// SignatureSize is the size of Ed25519 signatures in bytes.
const SignatureSize = 64

// sleepWakeWire is the shared wire layout of SleepCommand and WakeCommand:
//
//	OriginAgent [16 bytes] \
//	CommandID   [8 bytes]   } Signed data (SignableBytes)
//	Timestamp   [8 bytes]  /
//	Signature   [64 bytes]   Ed25519 signature (or zeros if unsigned)
//	SeenBy      [variable]   NOT signed (changes during propagation)
//
// Both commands encode through these helpers so the two serializers cannot
// drift apart; the wire format is unchanged.
type sleepWakeWire struct {
	OriginAgent identity.AgentID
	CommandID   uint64
	Timestamp   uint64
	Signature   [SignatureSize]byte
	SeenBy      []identity.AgentID
}

// signableBytes returns the bytes that are signed (OriginAgent + CommandID +
// Timestamp). SeenBy is excluded because it changes during propagation.
func (c *sleepWakeWire) signableBytes() []byte {
	w := newBufferWriter(16 + 8 + 8)
	w.writeBytes(c.OriginAgent[:])
	w.writeUint64(c.CommandID)
	w.writeUint64(c.Timestamp)
	return w.bytes()
}

// isZeroSignature returns true if the signature is all zeros (unsigned command).
func (c *sleepWakeWire) isZeroSignature() bool {
	for _, b := range c.Signature {
		if b != 0 {
			return false
		}
	}
	return true
}

// encodedLen is the exact wire size of this command. Encode sizes its buffer
// from it and DecodeQueuedState advances past an embedded command by it, so
// the two cannot drift apart.
func (c *sleepWakeWire) encodedLen() int {
	return 16 + 8 + 8 + SignatureSize + 1 + len(c.SeenBy)*16
}

func (c *sleepWakeWire) encode() []byte {
	w := newBufferWriter(c.encodedLen())
	w.writeBytes(c.OriginAgent[:])
	w.writeUint64(c.CommandID)
	w.writeUint64(c.Timestamp)
	w.writeBytes(c.Signature[:])
	w.writeAgentIDs(c.SeenBy)

	return w.bytes()
}

func decodeSleepWakeWire(buf []byte, name string) (*sleepWakeWire, error) {
	if len(buf) < 16+8+8+SignatureSize+1 { // Minimum with signature
		return nil, fmt.Errorf("%w: %s too short", ErrInvalidFrame, name)
	}

	r := newBufferReader(buf, name)
	c := &sleepWakeWire{
		OriginAgent: r.readAgentID(),
		CommandID:   r.readUint64(),
		Timestamp:   r.readUint64(),
	}

	// Read signature
	sigBytes := r.readBytes(SignatureSize)
	if r.err != nil {
		return nil, r.err
	}
	copy(c.Signature[:], sigBytes)

	c.SeenBy = r.readAgentIDs()

	if r.err != nil {
		return nil, r.err
	}
	return c, nil
}

// SleepCommand is the payload for SLEEP_COMMAND frames.
// This command floods through the mesh to instruct agents to hibernate.
// Wire format: see sleepWakeWire.
type SleepCommand struct {
	OriginAgent identity.AgentID    // Agent that initiated the sleep command
	CommandID   uint64              // Unique command ID for deduplication
	Timestamp   uint64              // Unix timestamp when command was issued
	Signature   [SignatureSize]byte // Ed25519 signature (zeros = unsigned)
	SeenBy      []identity.AgentID  // Loop prevention (agents that have seen this)
}

func (s *SleepCommand) wire() *sleepWakeWire {
	return &sleepWakeWire{
		OriginAgent: s.OriginAgent,
		CommandID:   s.CommandID,
		Timestamp:   s.Timestamp,
		Signature:   s.Signature,
		SeenBy:      s.SeenBy,
	}
}

// SignableBytes returns the bytes that are signed (OriginAgent + CommandID + Timestamp).
// SeenBy is excluded because it changes during propagation.
func (s *SleepCommand) SignableBytes() []byte {
	return s.wire().signableBytes()
}

// IsZeroSignature returns true if the signature is all zeros (unsigned command).
func (s *SleepCommand) IsZeroSignature() bool {
	return s.wire().isZeroSignature()
}

// encodedLen is the exact wire size of this command (see sleepWakeWire).
func (s *SleepCommand) encodedLen() int {
	return s.wire().encodedLen()
}

// Encode serializes SleepCommand to bytes.
func (s *SleepCommand) Encode() []byte {
	return s.wire().encode()
}

// DecodeSleepCommand deserializes SleepCommand from bytes.
func DecodeSleepCommand(buf []byte) (*SleepCommand, error) {
	c, err := decodeSleepWakeWire(buf, "SleepCommand")
	if err != nil {
		return nil, err
	}
	return &SleepCommand{
		OriginAgent: c.OriginAgent,
		CommandID:   c.CommandID,
		Timestamp:   c.Timestamp,
		Signature:   c.Signature,
		SeenBy:      c.SeenBy,
	}, nil
}

// WakeCommand is the payload for WAKE_COMMAND frames.
// This command floods through the mesh to instruct agents to wake from sleep.
// Wire format: see sleepWakeWire.
type WakeCommand struct {
	OriginAgent identity.AgentID    // Agent that initiated the wake command
	CommandID   uint64              // Unique command ID for deduplication
	Timestamp   uint64              // Unix timestamp when command was issued
	Signature   [SignatureSize]byte // Ed25519 signature (zeros = unsigned)
	SeenBy      []identity.AgentID  // Loop prevention (agents that have seen this)
}

func (w *WakeCommand) wire() *sleepWakeWire {
	return &sleepWakeWire{
		OriginAgent: w.OriginAgent,
		CommandID:   w.CommandID,
		Timestamp:   w.Timestamp,
		Signature:   w.Signature,
		SeenBy:      w.SeenBy,
	}
}

// SignableBytes returns the bytes that are signed (OriginAgent + CommandID + Timestamp).
// SeenBy is excluded because it changes during propagation.
func (w *WakeCommand) SignableBytes() []byte {
	return w.wire().signableBytes()
}

// IsZeroSignature returns true if the signature is all zeros (unsigned command).
func (w *WakeCommand) IsZeroSignature() bool {
	return w.wire().isZeroSignature()
}

// Encode serializes WakeCommand to bytes.
func (w *WakeCommand) Encode() []byte {
	return w.wire().encode()
}

// DecodeWakeCommand deserializes WakeCommand from bytes.
func DecodeWakeCommand(buf []byte) (*WakeCommand, error) {
	c, err := decodeSleepWakeWire(buf, "WakeCommand")
	if err != nil {
		return nil, err
	}
	return &WakeCommand{
		OriginAgent: c.OriginAgent,
		CommandID:   c.CommandID,
		Timestamp:   c.Timestamp,
		Signature:   c.Signature,
		SeenBy:      c.SeenBy,
	}, nil
}

// SleepState represents the sleep state for queued state messages.
type SleepState uint8

const (
	SleepStateAwake    SleepState = 0 // Normal operation
	SleepStateSleeping SleepState = 1 // Hibernated
	SleepStatePolling  SleepState = 2 // Temporarily reconnected during sleep
)

// QueuedState is the payload for QUEUED_STATE frames.
// Sent to agents when they reconnect after sleeping, containing queued state updates.
type QueuedState struct {
	Routes    []RouteAdvertise    // Queued route advertisements
	Withdraws []RouteWithdraw     // Queued route withdrawals
	NodeInfos []NodeInfoAdvertise // Queued node info updates
	SleepCmd  *SleepCommand       // Last sleep command (if still sleeping)
	WakeCmd   *WakeCommand        // Last wake command (if woken)
}

// Encode serializes QueuedState to bytes.
func (q *QueuedState) Encode() []byte {
	// Calculate size
	// Routes: count(2) + each route's encoded bytes
	// Withdraws: count(2) + each withdraw's encoded bytes
	// NodeInfos: count(2) + each nodeinfo's encoded bytes
	// SleepCmd: present(1) + encoded bytes
	// WakeCmd: present(1) + encoded bytes

	// Pre-encode all items to get their sizes
	routeBytes := make([][]byte, len(q.Routes))
	totalRouteSize := 0
	for i, r := range q.Routes {
		routeBytes[i] = r.Encode()
		totalRouteSize += 2 + len(routeBytes[i]) // 2 bytes for length prefix
	}

	withdrawBytes := make([][]byte, len(q.Withdraws))
	totalWithdrawSize := 0
	for i, w := range q.Withdraws {
		withdrawBytes[i] = w.Encode()
		totalWithdrawSize += 2 + len(withdrawBytes[i])
	}

	nodeInfoBytes := make([][]byte, len(q.NodeInfos))
	totalNodeInfoSize := 0
	for i, n := range q.NodeInfos {
		nodeInfoBytes[i] = n.Encode()
		totalNodeInfoSize += 2 + len(nodeInfoBytes[i])
	}

	var sleepCmdBytes []byte
	if q.SleepCmd != nil {
		sleepCmdBytes = q.SleepCmd.Encode()
	}

	var wakeCmdBytes []byte
	if q.WakeCmd != nil {
		wakeCmdBytes = q.WakeCmd.Encode()
	}

	size := 2 + totalRouteSize + 2 + totalWithdrawSize + 2 + totalNodeInfoSize
	size += 1 + len(sleepCmdBytes) // present flag + data
	size += 1 + len(wakeCmdBytes)  // present flag + data

	w := newBufferWriter(size)

	// Routes
	w.writeUint16(uint16(len(q.Routes)))
	for _, rb := range routeBytes {
		w.writeUint16(uint16(len(rb)))
		w.writeBytes(rb)
	}

	// Withdraws
	w.writeUint16(uint16(len(q.Withdraws)))
	for _, wb := range withdrawBytes {
		w.writeUint16(uint16(len(wb)))
		w.writeBytes(wb)
	}

	// NodeInfos
	w.writeUint16(uint16(len(q.NodeInfos)))
	for _, nb := range nodeInfoBytes {
		w.writeUint16(uint16(len(nb)))
		w.writeBytes(nb)
	}

	// SleepCmd
	if q.SleepCmd != nil {
		w.writeBool(true)
		w.writeBytes(sleepCmdBytes)
	} else {
		w.writeBool(false)
	}

	// WakeCmd
	if q.WakeCmd != nil {
		w.writeBool(true)
		w.writeBytes(wakeCmdBytes)
	} else {
		w.writeBool(false)
	}

	return w.bytes()
}

// DecodeQueuedState deserializes QueuedState from bytes.
func DecodeQueuedState(buf []byte) (*QueuedState, error) {
	if len(buf) < 8 { // 2+2+2+1+1 minimum (empty arrays, no commands)
		return nil, fmt.Errorf("%w: QueuedState too short", ErrInvalidFrame)
	}

	r := newBufferReader(buf, "QueuedState")
	q := &QueuedState{}

	// Routes
	routeCount := int(r.readUint16())
	q.Routes = make([]RouteAdvertise, 0, routeCount)
	for i := 0; i < routeCount && r.err == nil; i++ {
		length := int(r.readUint16())
		data := r.readBytes(length)
		if r.err != nil {
			break
		}
		adv, err := DecodeRouteAdvertise(data)
		if err != nil {
			continue // Skip malformed entries
		}
		q.Routes = append(q.Routes, *adv)
	}

	// Withdraws
	withdrawCount := int(r.readUint16())
	q.Withdraws = make([]RouteWithdraw, 0, withdrawCount)
	for i := 0; i < withdrawCount && r.err == nil; i++ {
		length := int(r.readUint16())
		data := r.readBytes(length)
		if r.err != nil {
			break
		}
		w, err := DecodeRouteWithdraw(data)
		if err != nil {
			continue
		}
		q.Withdraws = append(q.Withdraws, *w)
	}

	// NodeInfos
	nodeInfoCount := int(r.readUint16())
	q.NodeInfos = make([]NodeInfoAdvertise, 0, nodeInfoCount)
	for i := 0; i < nodeInfoCount && r.err == nil; i++ {
		length := int(r.readUint16())
		data := r.readBytes(length)
		if r.err != nil {
			break
		}
		n, err := DecodeNodeInfoAdvertise(data)
		if err != nil {
			continue
		}
		q.NodeInfos = append(q.NodeInfos, *n)
	}

	// SleepCmd
	if r.readBool() {
		// Read remaining bytes for sleep command
		sleepData := r.buf[r.offset:]
		sleepCmd, err := DecodeSleepCommand(sleepData)
		if err != nil {
			// Fail the decode: the sleep blob is not length-prefixed, so a
			// corrupt command leaves the offset stale and the WakeCmd flag
			// would be read from inside the sleep blob.
			r.setError(fmt.Sprintf("sleep command decode: %v", err))
		} else {
			q.SleepCmd = sleepCmd
			r.offset += sleepCmd.encodedLen()
		}
	}

	// WakeCmd
	if r.readBool() && r.err == nil {
		wakeData := r.buf[r.offset:]
		wakeCmd, err := DecodeWakeCommand(wakeData)
		if err == nil {
			q.WakeCmd = wakeCmd
		}
	}

	if r.err != nil {
		return nil, r.err
	}
	return q, nil
}

// ============================================================================
// Frame Reader/Writer
// ============================================================================

// FrameReader reads frames from an io.Reader.
type FrameReader struct {
	r      io.Reader
	header [HeaderSize]byte
}

// NewFrameReader creates a new FrameReader.
func NewFrameReader(r io.Reader) *FrameReader {
	return &FrameReader{r: r}
}

// Read reads the next frame.
//
// The payload buffer is freshly allocated per frame and deliberately NOT
// drawn from a sync.Pool: ownership of the slice passes to asynchronous
// consumers with no release point. Frames are dispatched through channels
// (peer Connection.frameCh/fastLaneCh) to handlers, and stream data payloads
// are retained by stream read buffers until the application drains them
// (e.g. agent.handleStreamData -> stream.Manager.HandleStreamData). Recycling
// the buffer at any point visible to FrameReader would corrupt data still in
// flight. A pool would need an explicit ownership/release protocol across
// peer, agent, and stream layers -- not worth the risk for one allocation
// per frame.
func (fr *FrameReader) Read() (*Frame, error) {
	// Read header
	if _, err := io.ReadFull(fr.r, fr.header[:]); err != nil {
		return nil, err
	}

	frameType, flags, length, streamID, err := DecodeHeader(fr.header[:])
	if err != nil {
		return nil, err
	}

	// Read payload
	payload := make([]byte, length)
	if length > 0 {
		if _, err := io.ReadFull(fr.r, payload); err != nil {
			return nil, err
		}
	}

	return &Frame{
		Type:     frameType,
		Flags:    flags,
		StreamID: streamID,
		Payload:  payload,
	}, nil
}

// FrameWriter writes frames to an io.Writer.
type FrameWriter struct {
	w io.Writer
}

// NewFrameWriter creates a new FrameWriter.
func NewFrameWriter(w io.Writer) *FrameWriter {
	return &FrameWriter{w: w}
}

// Write writes a frame.
//
// The header and payload are written as two separate writes instead of
// copying both into one coalesced buffer (Encode): the payload slice can be
// handed straight to the socket, skipping a 14+len allocation and a full
// payload copy per frame per hop. All transports (QUIC stream, H2 pipe,
// WebSocket-as-bytestream) are ordered byte streams, so splitting a frame
// across two writes is transparent to the reader. Callers serialize writes
// (peer.Connection.WriteFrame holds writeMu), so the two writes cannot
// interleave with another frame.
func (fw *FrameWriter) Write(f *Frame) error {
	if len(f.Payload) > MaxPayloadSize {
		return ErrFrameTooLarge
	}

	var header [HeaderSize]byte
	f.encodeHeader(header[:])
	if err := writeFull(fw.w, header[:]); err != nil {
		return err
	}
	if len(f.Payload) == 0 {
		return nil
	}
	return writeFull(fw.w, f.Payload)
}

// writeFull writes all of p, looping over short writes. io.Writer permits a
// short write without an error; the transports used here write fully or fail,
// but the loop keeps this correct for any writer.
func writeFull(w io.Writer, p []byte) error {
	for len(p) > 0 {
		n, err := w.Write(p)
		if err != nil {
			return err
		}
		if n <= 0 {
			return io.ErrShortWrite
		}
		p = p[n:]
	}
	return nil
}

// WriteFrame is a convenience method to write a frame with the given parameters.
func (fw *FrameWriter) WriteFrame(frameType uint8, flags uint8, streamID uint64, payload []byte) error {
	return fw.Write(&Frame{
		Type:     frameType,
		Flags:    flags,
		StreamID: streamID,
		Payload:  payload,
	})
}
