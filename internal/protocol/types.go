// Package protocol defines the wire protocol for Muti Metroo mesh communication.
package protocol

import "fmt"

// Frame type constants
const (
	// Stream frames
	FrameStreamOpen    uint8 = 0x01 // Request to open stream
	FrameStreamOpenAck uint8 = 0x02 // Stream opened successfully
	FrameStreamOpenErr uint8 = 0x03 // Stream open failed
	FrameStreamData    uint8 = 0x04 // Payload data
	FrameStreamClose   uint8 = 0x05 // Graceful close
	FrameStreamReset   uint8 = 0x06 // Abort stream
	FrameStreamWindow  uint8 = 0x07 // Grant the sender more data frames (flow control)

	// Routing frames
	FrameRouteAdvertise    uint8 = 0x10 // Announce CIDR routes
	FrameRouteWithdraw     uint8 = 0x11 // Remove CIDR routes
	FrameNodeInfoAdvertise uint8 = 0x12 // Announce node metadata

	// Control frames
	FramePeerHello    uint8 = 0x20 // Initial handshake
	FramePeerHelloAck uint8 = 0x21 // Handshake response
	FrameKeepalive    uint8 = 0x22 // Liveness probe
	FrameKeepaliveAck uint8 = 0x23 // Liveness response

	// Mesh control frames (for remote status queries)
	FrameControlRequest  uint8 = 0x24 // Request status from remote agent
	FrameControlResponse uint8 = 0x25 // Response with status data

	// UDP frames (for SOCKS5 UDP ASSOCIATE)
	FrameUDPOpen     uint8 = 0x30 // Request UDP association
	FrameUDPOpenAck  uint8 = 0x31 // Association established
	FrameUDPOpenErr  uint8 = 0x32 // Association failed
	FrameUDPDatagram uint8 = 0x33 // UDP datagram payload
	FrameUDPClose    uint8 = 0x34 // Close association

	// ICMP frames (for ICMP echo/ping through mesh)
	FrameICMPOpen    uint8 = 0x40 // Request ICMP echo session
	FrameICMPOpenAck uint8 = 0x41 // Session established
	FrameICMPOpenErr uint8 = 0x42 // Session failed
	FrameICMPEcho    uint8 = 0x43 // Echo request/reply data
	FrameICMPClose   uint8 = 0x44 // Close session

	// Sleep/Wake control frames (for mesh hibernation)
	FrameSleepCommand uint8 = 0x50 // Sleep command (flooded to mesh)
	FrameWakeCommand  uint8 = 0x51 // Wake command (flooded to mesh)
	FrameQueuedState  uint8 = 0x52 // Queued state for reconnecting agents
)

// Control request types
const (
	// 0x01 reserved (previously used for metrics)
	ControlTypeStatus uint8 = 0x02 // Request agent status
	ControlTypePeers  uint8 = 0x03 // Request peer list
	ControlTypeRoutes uint8 = 0x04 // Request route table
	ControlTypeRPC    uint8 = 0x05 // Remote procedure call (shell command)
	// 0x06 and 0x07 reserved (previously used for legacy file transfer)
	ControlTypeRouteManage           uint8 = 0x08 // Dynamic route management (add/remove/list)
	ControlTypeForwardManage         uint8 = 0x09 // Dynamic forward listener management (add/remove/list)
	ControlTypeFileBrowse            uint8 = 0x0A // File browsing (directory listing, stat, roots)
	ControlTypeDisplayNameManage     uint8 = 0x0B // Dynamic display name management
	ControlTypePeerManage            uint8 = 0x0C // Dynamic peer management (add/remove/list)
	ControlTypeProbe                 uint8 = 0x0D // Connectivity probe (dial + handshake) executed on remote agent
	ControlTypeForwardEndpointManage uint8 = 0x0E // Dynamic forward endpoint management (add/remove/list)
	ControlTypeRouteTrace            uint8 = 0x0F // Route selection trace (read-only diagnostic)
	ControlTypeStop                  uint8 = 0x10 // Graceful agent shutdown (signed; receiver exits)
	ControlTypeListenerManage        uint8 = 0x11 // Dynamic transport listener management (add/remove/list)
	ControlTypeInterfaces            uint8 = 0x12 // Request local interface addresses (read-only diagnostic)
)

// Frame flags
const (
	FlagFinWrite uint8 = 0x01 // Sender done writing
	// Flag bit 0x02 (historically "FIN_READ", sender done reading) is reserved
	// on the wire but never set or acted on; TCP has no "done receiving"
	// signal to propagate. Receivers ignore it.

	// FlagFlowControl on STREAM_OPEN asks for per-stream flow control; on
	// STREAM_OPEN_ACK it confirms it. A hop only keeps the flag on a forwarded
	// STREAM_OPEN when the next hop advertised CapabilityFlowControl, so an
	// acknowledged flag means every hop relays STREAM_WINDOW frames.
	FlagFlowControl uint8 = 0x04
)

// Per-stream flow control. When it is on, each STREAM_DATA frame with a
// non-empty payload uses one unit of the receiver's window. The sender stops
// at zero and resumes when the receiver returns units in STREAM_WINDOW frames
// as it consumes data, so the receiver never buffers more than
// StreamWindowFrames frames for a stream.
const (
	// CapabilityFlowControl is advertised in PEER_HELLO by agents that
	// support per-stream flow control and relay STREAM_WINDOW frames.
	CapabilityFlowControl = "flow-control"

	// StreamWindowFrames is the initial window in each direction, in data
	// frames (at most StreamWindowFrames * MaxPayloadSize bytes).
	StreamWindowFrames = 128

	// StreamWindowUpdateThreshold is how many consumed frames a receiver
	// collects before it sends a STREAM_WINDOW update.
	StreamWindowUpdateThreshold = 32
)

// Address type constants
const (
	AddrTypeIPv4   uint8 = 0x01 // 4 bytes
	AddrTypeIPv6   uint8 = 0x04 // 16 bytes
	AddrTypeDomain uint8 = 0x03 // 1-byte length + string
)

// Address family constants (for routes)
const (
	AddrFamilyIPv4    uint8 = 0x01
	AddrFamilyIPv6    uint8 = 0x02
	AddrFamilyDomain  uint8 = 0x03 // Domain pattern route
	AddrFamilyForward uint8 = 0x04 // Port forward routing key
	AddrFamilyAgent   uint8 = 0x05 // Agent presence route
)

// Error codes for STREAM_OPEN_ERR and STREAM_RESET
const (
	ErrNoRoute            uint16 = 1
	ErrConnectionRefused  uint16 = 2
	ErrConnectionTimeout  uint16 = 3
	ErrTTLExceeded        uint16 = 4
	ErrHostUnreachable    uint16 = 5
	ErrNetworkUnreachable uint16 = 6
	ErrDNSError           uint16 = 7
	ErrExitDisabled       uint16 = 8
	ErrResourceLimit      uint16 = 9
	ErrConnectionLimit    uint16 = 10
	ErrNotAllowed         uint16 = 11
	ErrFileTransferDenied uint16 = 12
	ErrAuthRequired       uint16 = 13
	ErrPathNotAllowed     uint16 = 14
	ErrFileTooLarge       uint16 = 15
	ErrFileNotFound       uint16 = 16
	ErrWriteFailed        uint16 = 17
	ErrGeneralFailure     uint16 = 18
	ErrResumeFailed       uint16 = 19 // File changed since partial transfer, resume not possible
	ErrShellDisabled      uint16 = 20 // Shell feature is disabled
	ErrShellAuthFailed    uint16 = 21 // Shell authentication failed
	ErrPTYFailed          uint16 = 22 // PTY allocation failed
	ErrCommandNotAllowed  uint16 = 23 // Command not in whitelist
	ErrUDPDisabled        uint16 = 30 // UDP relay is disabled
	ErrUDPPortNotAllowed  uint16 = 31 // UDP port not in whitelist
	ErrForwardNotFound    uint16 = 40 // Port forward routing key not configured
	ErrICMPDisabled       uint16 = 50 // ICMP echo is disabled
	ErrICMPDestNotAllowed uint16 = 51 // ICMP destination not in allowed CIDRs
	ErrICMPSessionLimit   uint16 = 52 // Maximum ICMP sessions reached
)

// Protocol constants
const (
	// ProtocolVersion is the current protocol version
	ProtocolVersion uint16 = 1

	// HeaderSize is the size of a frame header in bytes
	HeaderSize = 14

	// MaxPayloadSize is the maximum frame payload size (16 KB)
	MaxPayloadSize = 16384

	// MaxFrameSize is the maximum total frame size
	MaxFrameSize = HeaderSize + MaxPayloadSize

	// ControlStreamID is reserved for control messages
	ControlStreamID uint64 = 0
)

// File transfer stream addresses (used with AddrTypeDomain)
const (
	// FileTransferUpload is the domain address for file upload streams
	FileTransferUpload = "file:upload"

	// FileTransferDownload is the domain address for file download streams
	FileTransferDownload = "file:download"
)

// Shell stream addresses (used with AddrTypeDomain)
const (
	// ShellStream is the domain address for streaming command execution (no PTY)
	ShellStream = "shell:stream"

	// ShellInteractive is the domain address for interactive PTY sessions
	ShellInteractive = "shell:tty"
)

// UDP stream addresses (used with AddrTypeDomain)
const (
	// UDPAssociation is the domain address for UDP ASSOCIATE streams
	UDPAssociation = "udp:assoc"
)

// Forward stream addresses (used with AddrTypeDomain)
const (
	// ForwardStreamPrefix is the prefix for port forward stream addresses.
	// Format: "forward:<routing-key>"
	ForwardStreamPrefix = "forward:"
)

// ICMP stream addresses (used with AddrTypeDomain)
const (
	// ICMPEchoSession is the domain address for ICMP echo sessions
	ICMPEchoSession = "icmp:echo"
)

// ICMP close reasons
const (
	ICMPCloseNormal  uint8 = 0 // Normal close
	ICMPCloseTimeout uint8 = 1 // Idle timeout
	ICMPCloseError   uint8 = 2 // Error occurred
)

// FrameTypeName returns a human-readable name for a frame type.
func FrameTypeName(t uint8) string {
	switch t {
	case FrameStreamOpen:
		return "STREAM_OPEN"
	case FrameStreamOpenAck:
		return "STREAM_OPEN_ACK"
	case FrameStreamOpenErr:
		return "STREAM_OPEN_ERR"
	case FrameStreamData:
		return "STREAM_DATA"
	case FrameStreamClose:
		return "STREAM_CLOSE"
	case FrameStreamWindow:
		return "STREAM_WINDOW"
	case FrameStreamReset:
		return "STREAM_RESET"
	case FrameRouteAdvertise:
		return "ROUTE_ADVERTISE"
	case FrameRouteWithdraw:
		return "ROUTE_WITHDRAW"
	case FrameNodeInfoAdvertise:
		return "NODE_INFO_ADVERTISE"
	case FramePeerHello:
		return "PEER_HELLO"
	case FramePeerHelloAck:
		return "PEER_HELLO_ACK"
	case FrameKeepalive:
		return "KEEPALIVE"
	case FrameKeepaliveAck:
		return "KEEPALIVE_ACK"
	case FrameControlRequest:
		return "CONTROL_REQUEST"
	case FrameControlResponse:
		return "CONTROL_RESPONSE"
	case FrameUDPOpen:
		return "UDP_OPEN"
	case FrameUDPOpenAck:
		return "UDP_OPEN_ACK"
	case FrameUDPOpenErr:
		return "UDP_OPEN_ERR"
	case FrameUDPDatagram:
		return "UDP_DATAGRAM"
	case FrameUDPClose:
		return "UDP_CLOSE"
	case FrameICMPOpen:
		return "ICMP_OPEN"
	case FrameICMPOpenAck:
		return "ICMP_OPEN_ACK"
	case FrameICMPOpenErr:
		return "ICMP_OPEN_ERR"
	case FrameICMPEcho:
		return "ICMP_ECHO"
	case FrameICMPClose:
		return "ICMP_CLOSE"
	case FrameSleepCommand:
		return "SLEEP_COMMAND"
	case FrameWakeCommand:
		return "WAKE_COMMAND"
	case FrameQueuedState:
		return "QUEUED_STATE"
	default:
		return "UNKNOWN"
	}
}

// ErrorCodeName returns a human-readable name for an error code.
func ErrorCodeName(code uint16) string {
	switch code {
	case ErrNoRoute:
		return "NO_ROUTE"
	case ErrConnectionRefused:
		return "CONNECTION_REFUSED"
	case ErrConnectionTimeout:
		return "CONNECTION_TIMEOUT"
	case ErrTTLExceeded:
		return "TTL_EXCEEDED"
	case ErrHostUnreachable:
		return "HOST_UNREACHABLE"
	case ErrNetworkUnreachable:
		return "NETWORK_UNREACHABLE"
	case ErrDNSError:
		return "DNS_ERROR"
	case ErrExitDisabled:
		return "EXIT_DISABLED"
	case ErrResourceLimit:
		return "RESOURCE_LIMIT"
	case ErrConnectionLimit:
		return "CONNECTION_LIMIT"
	case ErrNotAllowed:
		return "NOT_ALLOWED"
	case ErrFileTransferDenied:
		return "FILE_TRANSFER_DENIED"
	case ErrAuthRequired:
		return "AUTH_REQUIRED"
	case ErrPathNotAllowed:
		return "PATH_NOT_ALLOWED"
	case ErrFileTooLarge:
		return "FILE_TOO_LARGE"
	case ErrFileNotFound:
		return "FILE_NOT_FOUND"
	case ErrWriteFailed:
		return "WRITE_FAILED"
	case ErrGeneralFailure:
		return "GENERAL_FAILURE"
	case ErrResumeFailed:
		return "RESUME_FAILED"
	case ErrShellDisabled:
		return "SHELL_DISABLED"
	case ErrShellAuthFailed:
		return "SHELL_AUTH_FAILED"
	case ErrPTYFailed:
		return "PTY_FAILED"
	case ErrCommandNotAllowed:
		return "COMMAND_NOT_ALLOWED"
	case ErrUDPDisabled:
		return "UDP_DISABLED"
	case ErrUDPPortNotAllowed:
		return "UDP_PORT_NOT_ALLOWED"
	case ErrForwardNotFound:
		return "FORWARD_NOT_FOUND"
	case ErrICMPDisabled:
		return "ICMP_DISABLED"
	case ErrICMPDestNotAllowed:
		return "ICMP_DEST_NOT_ALLOWED"
	case ErrICMPSessionLimit:
		return "ICMP_SESSION_LIMIT"
	default:
		return "UNKNOWN"
	}
}

// IsStreamFrame returns true if the frame type is a stream-related frame.
func IsStreamFrame(t uint8) bool {
	return t >= FrameStreamOpen && t <= FrameStreamReset
}

// IsRoutingFrame returns true if the frame type is a routing-related frame.
func IsRoutingFrame(t uint8) bool {
	return t == FrameRouteAdvertise || t == FrameRouteWithdraw || t == FrameNodeInfoAdvertise
}

// IsControlFrame returns true if the frame type is a control frame.
func IsControlFrame(t uint8) bool {
	return t >= FramePeerHello && t <= FrameControlResponse
}

// IsUDPFrame returns true if the frame type is a UDP-related frame.
func IsUDPFrame(t uint8) bool {
	return t >= FrameUDPOpen && t <= FrameUDPClose
}

// IsICMPFrame returns true if the frame type is an ICMP-related frame.
func IsICMPFrame(t uint8) bool {
	return t >= FrameICMPOpen && t <= FrameICMPClose
}

// IsSleepFrame returns true if the frame type is a sleep/wake-related frame.
func IsSleepFrame(t uint8) bool {
	return t >= FrameSleepCommand && t <= FrameQueuedState
}

// DialError wraps a STREAM_OPEN_ERR from the mesh so that callers (such as
// the SOCKS5 server) can map the mesh-level ErrorCode onto a protocol-specific
// reply code via errors.As. Message is the human-readable text from the remote.
type DialError struct {
	ErrorCode uint16
	Message   string
}

func (e *DialError) Error() string {
	name := ErrorCodeName(e.ErrorCode)
	if e.Message != "" {
		return fmt.Sprintf("mesh dial failed: %s (code=%d): %s", name, e.ErrorCode, e.Message)
	}
	return fmt.Sprintf("mesh dial failed: %s (code=%d)", name, e.ErrorCode)
}
