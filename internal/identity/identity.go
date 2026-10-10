// Package identity provides agent identity management.
package identity

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	// IDSize is the size of an AgentID in bytes (128 bits)
	IDSize = 16

	// idFileName is the name of the file storing the agent ID
	idFileName = "agent_id"
)

var (
	// ErrInvalidIDLength is returned when the ID length is incorrect
	ErrInvalidIDLength = errors.New("invalid agent ID length: expected 16 bytes")

	// ErrInvalidHexString is returned when the hex string is malformed
	ErrInvalidHexString = errors.New("invalid hex string for agent ID")

	// ZeroID represents an uninitialized agent ID
	ZeroID = AgentID{}
)

// AgentID represents a unique 128-bit identifier for an agent.
// It is generated randomly using crypto/rand and persisted to disk.
type AgentID [IDSize]byte

// NewAgentID generates a new random AgentID using crypto/rand.
func NewAgentID() (AgentID, error) {
	var id AgentID
	if _, err := io.ReadFull(rand.Reader, id[:]); err != nil {
		return ZeroID, fmt.Errorf("failed to generate agent ID: %w", err)
	}
	return id, nil
}

// ParseAgentID parses an AgentID from a hex string.
func ParseAgentID(s string) (AgentID, error) {
	// Remove any whitespace and 0x prefix
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "0x")
	s = strings.TrimPrefix(s, "0X")

	if len(s) != IDSize*2 {
		return ZeroID, fmt.Errorf("%w: got %d hex chars, expected %d", ErrInvalidHexString, len(s), IDSize*2)
	}

	bytes, err := hex.DecodeString(s)
	if err != nil {
		return ZeroID, fmt.Errorf("%w: %v", ErrInvalidHexString, err)
	}

	var id AgentID
	copy(id[:], bytes)
	return id, nil
}

// FromBytes creates an AgentID from a byte slice.
func FromBytes(b []byte) (AgentID, error) {
	if len(b) != IDSize {
		return ZeroID, fmt.Errorf("%w: got %d bytes", ErrInvalidIDLength, len(b))
	}
	var id AgentID
	copy(id[:], b)
	return id, nil
}

// String returns the full hex representation of the AgentID.
func (id AgentID) String() string {
	return hex.EncodeToString(id[:])
}

// ShortString returns a shortened hex representation (first 8 chars).
func (id AgentID) ShortString() string {
	return hex.EncodeToString(id[:4])
}

// Bytes returns the AgentID as a byte slice.
func (id AgentID) Bytes() []byte {
	return id[:]
}

// IsZero returns true if the AgentID is uninitialized (all zeros).
func (id AgentID) IsZero() bool {
	return id == ZeroID
}

// Equal returns true if two AgentIDs are identical.
func (id AgentID) Equal(other AgentID) bool {
	return id == other
}

// Compare returns -1, 0, or 1 comparing id and other byte-wise. Useful when a
// deterministic but otherwise arbitrary total order over AgentIDs is needed.
func (id AgentID) Compare(other AgentID) int {
	return bytes.Compare(id[:], other[:])
}

// MarshalText implements encoding.TextMarshaler.
func (id AgentID) MarshalText() ([]byte, error) {
	return []byte(id.String()), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
func (id *AgentID) UnmarshalText(text []byte) error {
	parsed, err := ParseAgentID(string(text))
	if err != nil {
		return err
	}
	*id = parsed
	return nil
}

// Store persists the AgentID to the specified data directory.
func (id AgentID) Store(dataDir string) error {
	if id.IsZero() {
		return errors.New("cannot store zero agent ID")
	}

	// Ensure directory exists
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return fmt.Errorf("failed to create data directory: %w", err)
	}

	filePath := filepath.Join(dataDir, idFileName)

	// Write atomically by writing to temp file first
	tempPath := filePath + ".tmp"
	if err := os.WriteFile(tempPath, []byte(id.String()+"\n"), 0600); err != nil {
		return fmt.Errorf("failed to write agent ID: %w", err)
	}

	if err := os.Rename(tempPath, filePath); err != nil {
		os.Remove(tempPath) // Clean up temp file
		return fmt.Errorf("failed to persist agent ID: %w", err)
	}

	return nil
}

// Load reads an AgentID from the specified data directory.
func Load(dataDir string) (AgentID, error) {
	filePath := filepath.Join(dataDir, idFileName)

	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return ZeroID, fmt.Errorf("agent ID not found at %s", filePath)
		}
		return ZeroID, fmt.Errorf("failed to read agent ID: %w", err)
	}

	return ParseAgentID(strings.TrimSpace(string(data)))
}

// LoadOrCreate loads an existing AgentID from the data directory,
// or creates and persists a new one if none exists.
func LoadOrCreate(dataDir string) (AgentID, bool, error) {
	id, err := Load(dataDir)
	if err == nil {
		return id, false, nil // Loaded existing ID
	}

	// Check if it's a "not found" error
	if !strings.Contains(err.Error(), "not found") {
		return ZeroID, false, err // Some other error
	}

	// Generate new ID
	id, err = NewAgentID()
	if err != nil {
		return ZeroID, false, err
	}

	// Persist it
	if err := id.Store(dataDir); err != nil {
		return ZeroID, false, err
	}

	return id, true, nil // Created new ID
}

// Exists checks if an AgentID file exists in the data directory.
func Exists(dataDir string) bool {
	filePath := filepath.Join(dataDir, idFileName)
	_, err := os.Stat(filePath)
	return err == nil
}
