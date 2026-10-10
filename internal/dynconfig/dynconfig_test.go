package dynconfig

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func TestJournal_NilIsNoop(t *testing.T) {
	var j *Journal

	if got := j.Path(); got != "" {
		t.Errorf("nil Journal Path() = %q, want empty", got)
	}
	if err := j.Append(Event{Op: OpPeerAdd}); err != nil {
		t.Errorf("nil Journal Append() error = %v", err)
	}
	events, err := j.Load()
	if err != nil {
		t.Errorf("nil Journal Load() error = %v", err)
	}
	if events != nil {
		t.Errorf("nil Journal Load() = %v, want nil", events)
	}
	if err := j.Compact(nil); err != nil {
		t.Errorf("nil Journal Compact() error = %v", err)
	}
}

func TestNewJournal_EmptyDataDir(t *testing.T) {
	if j := NewJournal(""); j != nil {
		t.Errorf("NewJournal(\"\") = %v, want nil", j)
	}
}

func TestJournal_AppendLoadRoundTrip(t *testing.T) {
	j := NewJournal(t.TempDir())

	in := []Event{
		{Op: OpDisplayNameSet, Name: "gateway"},
		{Op: OpRouteAdd, Network: "10.0.0.0/8", Metric: 2, Comment: "internal corp"},
		{Op: OpRouteAdd, Network: "192.168.1.0/24", SourceIP: "192.168.1.50"},
		{Op: OpRouteCommentSet, Network: "10.0.0.0/8", Comment: "renamed corp"},
		{Op: OpRouteSourceIPSet, Network: "192.168.1.0/24", SourceIP: "192.168.1.60"},
		{Op: OpRouteInterfaceSet, Network: "192.168.1.0/24", Interface: "eth0"},
		{Op: OpForwardAdd, Key: "web", Address: "127.0.0.1:9090", MaxConnections: 50},
		{Op: OpForwardEndpointAdd, Key: "web", Target: "127.0.0.1:3000"},
		{Op: OpForwardEndpointRemove, Key: "web"},
		{Op: OpPeerAdd, Address: "peer.example:4433", Transport: "quic", ExpectedID: "abc123"},
		{Op: OpPeerAdd, Address: "relay.example:3000", Transport: "h2", ExpectedID: "def456", TCPKeepAlive: "20s"},
		{Op: OpPeerRemove, Address: "peer.example:4433"},
		{Op: OpListenerAdd, Transport: "ws", Address: "127.0.0.1:3002", Path: "/ws/", PlainText: true, AdvertiseAddress: "edge.example.com:4433", AdvertisePath: "/pub/", AdvertiseTransport: "ws"},
		{Op: OpListenerRemove, Address: "127.0.0.1:3002"},
	}
	for _, e := range in {
		if err := j.Append(e); err != nil {
			t.Fatalf("Append(%v) error = %v", e, err)
		}
	}

	out, err := j.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(out) != len(in) {
		t.Fatalf("Load() len = %d, want %d", len(out), len(in))
	}
	for i, want := range in {
		got := out[i]
		// Timestamps are caller-populated; ignore for equality.
		got.Timestamp, want.Timestamp = 0, 0
		if got != want {
			t.Errorf("event[%d] = %+v, want %+v", i, got, want)
		}
	}
}

func TestJournal_LoadMissingFile(t *testing.T) {
	j := NewJournal(t.TempDir())

	events, err := j.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if events != nil {
		t.Errorf("Load() = %v, want nil on missing file", events)
	}
}

func TestJournal_LoadMalformedLine(t *testing.T) {
	dir := t.TempDir()
	j := NewJournal(dir)

	if err := j.Append(Event{Op: OpPeerAdd, Address: "a:1"}); err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	// Append a malformed line directly so we don't need to reach into the struct.
	f, err := os.OpenFile(j.Path(), os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatalf("open journal: %v", err)
	}
	if _, err := f.WriteString("not json\n"); err != nil {
		t.Fatalf("write malformed: %v", err)
	}
	f.Close()

	if _, err := j.Load(); err == nil {
		t.Error("Load() error = nil, want parse error")
	} else if !strings.Contains(err.Error(), "line 2") {
		t.Errorf("Load() error = %v, want to mention line 2", err)
	}
}

func TestJournal_LoadSkipsBlankLines(t *testing.T) {
	dir := t.TempDir()
	j := NewJournal(dir)

	if err := j.Append(Event{Op: OpPeerAdd, Address: "a:1"}); err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	f, err := os.OpenFile(j.Path(), os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := f.WriteString("\n\n"); err != nil {
		t.Fatalf("write blanks: %v", err)
	}
	f.Close()

	events, err := j.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(events) != 1 {
		t.Errorf("Load() len = %d, want 1 (blank lines skipped)", len(events))
	}
}

func TestJournal_CompactRewrites(t *testing.T) {
	dir := t.TempDir()
	j := NewJournal(dir)

	// Add a lot of churn.
	for i := 0; i < 20; i++ {
		_ = j.Append(Event{Op: OpPeerAdd, Address: "a:1"})
		_ = j.Append(Event{Op: OpPeerRemove, Address: "a:1"})
	}
	_ = j.Append(Event{Op: OpPeerAdd, Address: "a:1"})

	// Compact to the "live" state: one add.
	compacted := []Event{{Op: OpPeerAdd, Address: "a:1"}}
	if err := j.Compact(compacted); err != nil {
		t.Fatalf("Compact() error = %v", err)
	}

	out, err := j.Load()
	if err != nil {
		t.Fatalf("Load() after Compact() error = %v", err)
	}
	if len(out) != 1 || out[0].Op != OpPeerAdd || out[0].Address != "a:1" {
		t.Errorf("Load() after Compact() = %+v, want single peer_add a:1", out)
	}
}

func TestJournal_CompactEmptyRemovesFile(t *testing.T) {
	dir := t.TempDir()
	j := NewJournal(dir)

	_ = j.Append(Event{Op: OpPeerAdd, Address: "a:1"})
	if err := j.Compact(nil); err != nil {
		t.Fatalf("Compact(nil) error = %v", err)
	}
	if _, err := os.Stat(j.Path()); !os.IsNotExist(err) {
		t.Errorf("journal file still exists after empty Compact: err=%v", err)
	}
	// And Compact on an already-missing file is fine.
	if err := j.Compact(nil); err != nil {
		t.Errorf("Compact(nil) on missing file error = %v", err)
	}
}

func TestJournal_ConcurrentAppendsSerialize(t *testing.T) {
	dir := t.TempDir()
	j := NewJournal(dir)

	const workers = 8
	const perWorker = 50

	var wg sync.WaitGroup
	wg.Add(workers)
	for w := 0; w < workers; w++ {
		w := w
		go func() {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				if err := j.Append(Event{Op: OpPeerAdd, Address: "w", Metric: uint16(w)}); err != nil {
					t.Errorf("worker %d: Append error = %v", w, err)
					return
				}
			}
		}()
	}
	wg.Wait()

	events, err := j.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got := len(events); got != workers*perWorker {
		t.Errorf("Load() len = %d, want %d (no torn lines)", got, workers*perWorker)
	}
}

func TestJournal_AppendFailsOnUnwritableDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod semantics differ on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("running as root bypasses mode bits")
	}

	parent := t.TempDir()
	dataDir := filepath.Join(parent, "ro")
	if err := os.MkdirAll(dataDir, 0500); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Restore perms so t.TempDir cleanup works.
	t.Cleanup(func() { _ = os.Chmod(dataDir, 0700) })

	j := NewJournal(dataDir)
	if err := j.Append(Event{Op: OpPeerAdd, Address: "a:1"}); err == nil {
		t.Error("Append() on read-only dir: error = nil, want EACCES or similar")
	}
}
