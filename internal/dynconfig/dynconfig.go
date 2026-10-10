// Package dynconfig persists dynamic runtime configuration changes (peers,
// transport listeners, forward listeners, routes, display name) to an append-only JSONL journal
// under the agent's data directory. On agent startup, the journal is replayed
// through the same Manage* methods that CLI/HTTP callers use, restoring the
// dynamic state while preserving the config-vs-dynamic distinction.
//
// The journal file is optional: when the agent's data_dir is empty, or when
// writes fail for any reason (permissions, read-only fs, disk full), the
// runtime mutation still succeeds and the agent falls back to the original
// ephemeral behavior.
package dynconfig

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

// JournalFileName is the name of the dynamic config journal inside data_dir.
const JournalFileName = "dynamic-config.log"

// Op constants name the mutation kinds recorded in the journal.
const (
	OpPeerAdd               = "peer_add"
	OpPeerRemove            = "peer_remove"
	OpForwardAdd            = "forward_add"
	OpForwardRemove         = "forward_remove"
	OpForwardEndpointAdd    = "forward_endpoint_add"
	OpForwardEndpointRemove = "forward_endpoint_remove"
	OpListenerAdd           = "listener_add"
	OpListenerRemove        = "listener_remove"
	OpRouteAdd              = "route_add"
	OpRouteRemove           = "route_remove"
	OpRouteCommentSet       = "route_comment_set"
	OpRouteSourceIPSet      = "route_source_ip_set"
	OpRouteInterfaceSet     = "route_interface_set"
	OpDisplayNameSet        = "display_name_set"
	OpHostAdd               = "host_add"
	OpHostRemove            = "host_remove"
	OpSuffixAdd             = "suffix_add"
	OpSuffixRemove          = "suffix_remove"
)

// Event is a single mutation record. Fields are a flat union; only the subset
// relevant to the Op is populated, and omitempty keeps serialized lines compact.
type Event struct {
	Op        string `json:"op"`
	Timestamp int64  `json:"ts,omitempty"`

	Address        string `json:"address,omitempty"`
	Transport      string `json:"transport,omitempty"`
	ExpectedID     string `json:"expected_id,omitempty"`
	TCPKeepAlive   string `json:"tcp_keepalive,omitempty"` // peer add: SO_KEEPALIVE override (duration string)
	Network        string `json:"network,omitempty"`
	Metric         uint16 `json:"metric,omitempty"`
	Key            string `json:"key,omitempty"`
	Target         string `json:"target,omitempty"`
	MaxConnections int    `json:"max_connections,omitempty"`
	Name           string `json:"name,omitempty"`
	IPv4           string `json:"ipv4,omitempty"`
	IPv6           string `json:"ipv6,omitempty"`
	Comment        string `json:"comment,omitempty"`
	Suffix         string `json:"suffix,omitempty"`
	SourceIP       string `json:"source_ip,omitempty"`
	Interface      string `json:"interface,omitempty"`

	// Transport listener fields (OpListenerAdd/OpListenerRemove). Transport and
	// Address above are reused; the rest are listener-specific.
	Path               string `json:"path,omitempty"`
	PlainText          bool   `json:"plaintext,omitempty"`
	AdvertiseAddress   string `json:"advertise_address,omitempty"`
	AdvertisePath      string `json:"advertise_path,omitempty"`
	AdvertiseTransport string `json:"advertise_transport,omitempty"`
}

// Journal is an append-only JSONL journal backed by a single file inside
// data_dir. A nil *Journal is a valid no-op value (Append returns nil, Load
// returns nil events, Compact returns nil); callers do not need nil checks.
type Journal struct {
	path string
	mu   sync.Mutex
}

// NewJournal returns a Journal rooted at <dataDir>/dynamic-config.log.
// When dataDir is empty, NewJournal returns nil so persistence is disabled --
// matching the documented "ephemeral when no data_dir" behavior.
func NewJournal(dataDir string) *Journal {
	if dataDir == "" {
		return nil
	}
	return &Journal{path: filepath.Join(dataDir, JournalFileName)}
}

// Path returns the absolute journal file path, or "" for a nil Journal.
func (j *Journal) Path() string {
	if j == nil {
		return ""
	}
	return j.path
}

// Append writes a single event to the end of the journal file. The write
// opens the file with O_APPEND|O_CREATE|O_WRONLY (0600), writes one line,
// fsyncs, and closes. Concurrent callers are serialized via an internal
// mutex so JSONL lines never interleave.
//
// A nil Journal returns nil; errors (e.g. ENOENT data_dir, EACCES, ENOSPC)
// are surfaced to the caller, which logs a warning and continues -- the
// in-memory mutation still succeeds.
func (j *Journal) Append(e Event) error {
	if j == nil {
		return nil
	}

	line, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	line = append(line, '\n')

	j.mu.Lock()
	defer j.mu.Unlock()

	f, err := os.OpenFile(j.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if _, err := f.Write(line); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// Load reads every event from the journal in order. A missing file is not an
// error and returns (nil, nil). A malformed line returns an error citing the
// line number so operators can fix or remove the file by hand. Empty lines
// are skipped.
//
// A nil Journal returns (nil, nil).
func (j *Journal) Load() ([]Event, error) {
	if j == nil {
		return nil, nil
	}

	j.mu.Lock()
	defer j.mu.Unlock()

	f, err := os.Open(j.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var events []Event
	scanner := bufio.NewScanner(f)
	// Events are small (< 1 KB) but allow generous headroom in case future
	// fields expand -- avoid the default 64 KB limit surprising us later.
	scanner.Buffer(make([]byte, 0, 4096), 1<<20)

	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var e Event
		if err := json.Unmarshal(line, &e); err != nil {
			return nil, fmt.Errorf("dynconfig: parse line %d: %w", lineNo, err)
		}
		events = append(events, e)
	}
	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return events, nil
}

// Compact atomically rewrites the journal so it contains exactly the given
// events (in order). Used after replay to collapse add/remove churn into the
// minimal set of "currently live" records.
//
// Write goes to <path>.tmp first, then os.Rename swaps it into place. On
// failure the tmp file is removed. A nil Journal returns nil.
//
// Compact with an empty events slice removes the journal file entirely.
func (j *Journal) Compact(events []Event) error {
	if j == nil {
		return nil
	}

	j.mu.Lock()
	defer j.mu.Unlock()

	if len(events) == 0 {
		if err := os.Remove(j.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}

	tempPath := j.path + ".tmp"
	f, err := os.OpenFile(tempPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}

	w := bufio.NewWriter(f)
	enc := json.NewEncoder(w)
	for _, e := range events {
		if err := enc.Encode(e); err != nil {
			_ = f.Close()
			_ = os.Remove(tempPath)
			return err
		}
	}
	if err := w.Flush(); err != nil {
		_ = f.Close()
		_ = os.Remove(tempPath)
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tempPath)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tempPath)
		return err
	}

	if err := os.Rename(tempPath, j.path); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	return nil
}
