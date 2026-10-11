// Package shell implements streaming and interactive shell functionality for Muti Metroo.
package shell

import (
	"encoding/binary"
	"encoding/json"
	"fmt"

	"github.com/Muti-Metroo/Muti-Metroo/internal/crypto"
)

// Message type constants for shell stream protocol.
// These are the first byte of each STREAM_DATA payload.
const (
	MsgMeta       uint8 = 0x01 // JSON metadata (command, args, tty settings)
	MsgAck        uint8 = 0x02 // JSON acknowledgment
	MsgStdin      uint8 = 0x03 // Raw stdin bytes
	MsgStdout     uint8 = 0x04 // Raw stdout bytes
	MsgStderr     uint8 = 0x05 // Raw stderr bytes
	MsgResize     uint8 = 0x06 // 4 bytes: rows (uint16 BE), cols (uint16 BE)
	MsgSignal     uint8 = 0x07 // 1 byte: signal number
	MsgExit       uint8 = 0x08 // 4 bytes: exit code (int32 BE)
	MsgError      uint8 = 0x09 // JSON error message
	MsgStdinClose uint8 = 0x0a // No payload. Client signals EOF on stdin so EOF-sensitive commands (cat, wc, sort, ...) can exit cleanly.
)

// ShellError.Code values. Separate namespace from protocol.Err* (which are
// STREAM_OPEN_ERR frame codes) -- these ride in the JSON MSG_ERROR body.
const (
	// ErrCodeAuthRequired signals a missing management signature (or a target
	// agent that has no public key configured to verify against). Kept as a
	// stable wire code so clients can distinguish auth failures from other
	// errors.
	ErrCodeAuthRequired = 21
	// ErrCodeInvalidCredentials signals a signature that did not verify
	// (bad bytes, stale timestamp, wrong target, or wrong op binding).
	ErrCodeInvalidCredentials = 22
)

// MsgTypeName returns a human-readable name for a message type.
func MsgTypeName(t uint8) string {
	switch t {
	case MsgMeta:
		return "META"
	case MsgAck:
		return "ACK"
	case MsgStdin:
		return "STDIN"
	case MsgStdout:
		return "STDOUT"
	case MsgStderr:
		return "STDERR"
	case MsgResize:
		return "RESIZE"
	case MsgSignal:
		return "SIGNAL"
	case MsgExit:
		return "EXIT"
	case MsgError:
		return "ERROR"
	case MsgStdinClose:
		return "STDIN_CLOSE"
	default:
		return "UNKNOWN"
	}
}

// EncodeStdinClose returns the 1-byte message signalling that no more
// stdin will arrive from the client.
func EncodeStdinClose() []byte {
	return EncodeMessage(MsgStdinClose, nil)
}

// ShellMeta is sent as the first message after STREAM_OPEN_ACK.
type ShellMeta struct {
	Command string               `json:"command"`            // Command to execute
	Args    []string             `json:"args,omitempty"`     // Command arguments
	Env     map[string]string    `json:"env,omitempty"`      // Environment variables to set
	WorkDir string               `json:"work_dir,omitempty"` // Working directory
	TTY     *TTYSettings         `json:"tty,omitempty"`      // Non-nil = allocate PTY
	Timeout int                  `json:"timeout,omitempty"`  // Session timeout in seconds (0 = no timeout)
	Auth    *crypto.AuthEnvelope `json:"auth,omitempty"`     // Management-key signature (required on the wire)
}

// TTYSettings contains terminal configuration for interactive sessions.
type TTYSettings struct {
	Rows uint16 `json:"rows"`           // Terminal rows
	Cols uint16 `json:"cols"`           // Terminal columns
	Term string `json:"term,omitempty"` // TERM environment variable (default: xterm-256color)
}

// ShellAck is sent in response to ShellMeta.
type ShellAck struct {
	Success bool   `json:"success"`         // Whether the shell session was started
	Error   string `json:"error,omitempty"` // Error message if success is false
}

// ShellError is sent when an error occurs during the session.
type ShellError struct {
	Message string `json:"message"`
	Code    int    `json:"code,omitempty"` // Optional error code
}

// EncodeMessage encodes a message with its type prefix.
func EncodeMessage(msgType uint8, payload []byte) []byte {
	result := make([]byte, 1+len(payload))
	result[0] = msgType
	copy(result[1:], payload)
	return result
}

// DecodeMessage decodes a message, returning the type and payload.
func DecodeMessage(data []byte) (msgType uint8, payload []byte, err error) {
	if len(data) < 1 {
		return 0, nil, fmt.Errorf("message too short")
	}
	return data[0], data[1:], nil
}

// EncodeMeta encodes a ShellMeta message.
func EncodeMeta(meta *ShellMeta) ([]byte, error) {
	payload, err := json.Marshal(meta)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal meta: %w", err)
	}
	return EncodeMessage(MsgMeta, payload), nil
}

// DecodeMeta decodes a ShellMeta message from payload (without type prefix).
func DecodeMeta(payload []byte) (*ShellMeta, error) {
	var meta ShellMeta
	if err := json.Unmarshal(payload, &meta); err != nil {
		return nil, fmt.Errorf("failed to unmarshal meta: %w", err)
	}
	return &meta, nil
}

// EncodeAck encodes a ShellAck message.
func EncodeAck(ack *ShellAck) ([]byte, error) {
	payload, err := json.Marshal(ack)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal ack: %w", err)
	}
	return EncodeMessage(MsgAck, payload), nil
}

// DecodeAck decodes a ShellAck message from payload (without type prefix).
func DecodeAck(payload []byte) (*ShellAck, error) {
	var ack ShellAck
	if err := json.Unmarshal(payload, &ack); err != nil {
		return nil, fmt.Errorf("failed to unmarshal ack: %w", err)
	}
	return &ack, nil
}

// EncodeStdin encodes stdin data.
func EncodeStdin(data []byte) []byte {
	return EncodeMessage(MsgStdin, data)
}

// EncodeStdout encodes stdout data.
func EncodeStdout(data []byte) []byte {
	return EncodeMessage(MsgStdout, data)
}

// EncodeStderr encodes stderr data.
func EncodeStderr(data []byte) []byte {
	return EncodeMessage(MsgStderr, data)
}

// EncodeResize encodes a terminal resize message.
func EncodeResize(rows, cols uint16) []byte {
	payload := make([]byte, 4)
	binary.BigEndian.PutUint16(payload[0:2], rows)
	binary.BigEndian.PutUint16(payload[2:4], cols)
	return EncodeMessage(MsgResize, payload)
}

// DecodeResize decodes a terminal resize message from payload (without type prefix).
func DecodeResize(payload []byte) (rows, cols uint16, err error) {
	if len(payload) < 4 {
		return 0, 0, fmt.Errorf("resize payload too short: %d bytes", len(payload))
	}
	rows = binary.BigEndian.Uint16(payload[0:2])
	cols = binary.BigEndian.Uint16(payload[2:4])
	return rows, cols, nil
}

// EncodeSignal encodes a signal message.
func EncodeSignal(signum uint8) []byte {
	return EncodeMessage(MsgSignal, []byte{signum})
}

// DecodeSignal decodes a signal message from payload (without type prefix).
func DecodeSignal(payload []byte) (signum uint8, err error) {
	if len(payload) < 1 {
		return 0, fmt.Errorf("signal payload too short")
	}
	return payload[0], nil
}

// EncodeExit encodes an exit code message.
func EncodeExit(exitCode int32) []byte {
	payload := make([]byte, 4)
	binary.BigEndian.PutUint32(payload, uint32(exitCode))
	return EncodeMessage(MsgExit, payload)
}

// DecodeExit decodes an exit code message from payload (without type prefix).
func DecodeExit(payload []byte) (exitCode int32, err error) {
	if len(payload) < 4 {
		return 0, fmt.Errorf("exit payload too short: %d bytes", len(payload))
	}
	return int32(binary.BigEndian.Uint32(payload)), nil
}

// EncodeError encodes an error message.
func EncodeError(shellErr *ShellError) ([]byte, error) {
	payload, err := json.Marshal(shellErr)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal error: %w", err)
	}
	return EncodeMessage(MsgError, payload), nil
}

// DecodeError decodes an error message from payload (without type prefix).
func DecodeError(payload []byte) (*ShellError, error) {
	var shellErr ShellError
	if err := json.Unmarshal(payload, &shellErr); err != nil {
		return nil, fmt.Errorf("failed to unmarshal error: %w", err)
	}
	return &shellErr, nil
}
