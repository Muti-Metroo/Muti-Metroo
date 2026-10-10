package agent

import (
	"net"
	"testing"

	"github.com/postalsys/muti-metroo/internal/config"
	"github.com/postalsys/muti-metroo/internal/health"
)

// freeTCPAddr returns a currently-free 127.0.0.1:port address. The momentary
// listener is closed before returning; ws transport listeners bind TCP, so this
// gives dynamic-listener tests a concrete address without hard-coding ports.
func freeTCPAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("freeTCPAddr: %v", err)
	}
	defer l.Close()
	return l.Addr().String()
}

func listenerByAddr(res *health.ListenerManageResult, addr string) (health.ListenerManageResultEntry, bool) {
	for _, l := range res.Listeners {
		if l.Address == addr {
			return l, true
		}
	}
	return health.ListenerManageResultEntry{}, false
}

// TestAgent_ManageListener exercises the dynamic transport listener API:
// config listeners are listed but write-protected; dynamic add/list/remove and
// duplicate-address replace round-trip; bad input is rejected.
func TestAgent_ManageListener(t *testing.T) {
	dir := t.TempDir()
	cfgAddr := freeTCPAddr(t)
	dynAddr := freeTCPAddr(t)

	cfg := config.Default()
	cfg.Agent.DataDir = dir
	cfg.Listeners = []config.ListenerConfig{
		{Transport: "ws", Address: cfgAddr, Path: "/ws/", PlainText: true},
	}
	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := a.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer a.Stop()

	// Config listener appears in the list, flagged static.
	res, err := a.ManageListener(&health.ListenerManageRequest{Action: "list"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if entry, ok := listenerByAddr(res, cfgAddr); !ok {
		t.Errorf("config listener %q missing from list: %+v", cfgAddr, res.Listeners)
	} else if entry.Dynamic {
		t.Errorf("config listener flagged Dynamic=true")
	}

	// Config listeners are write-protected.
	if _, err := a.ManageListener(&health.ListenerManageRequest{Action: "add", Transport: "ws", Address: cfgAddr, Path: "/ws/", PlainText: true}); err == nil {
		t.Error("adding over config address: expected error")
	}
	if _, err := a.ManageListener(&health.ListenerManageRequest{Action: "remove", Address: cfgAddr}); err == nil {
		t.Error("removing config address: expected error")
	}

	// Validation.
	if _, err := a.ManageListener(&health.ListenerManageRequest{Action: "add", Transport: "bogus", Address: dynAddr}); err == nil {
		t.Error("add bad transport: expected error")
	}
	if _, err := a.ManageListener(&health.ListenerManageRequest{Action: "add", Transport: "ws", Address: ""}); err == nil {
		t.Error("add empty address: expected error")
	}
	if _, err := a.ManageListener(&health.ListenerManageRequest{Action: "remove", Address: dynAddr}); err == nil {
		t.Error("remove unknown address: expected error")
	}

	// Dynamic add with an advertise target.
	if _, err := a.ManageListener(&health.ListenerManageRequest{
		Action:           "add",
		Transport:        "ws",
		Address:          dynAddr,
		Path:             "/ws/",
		PlainText:        true,
		AdvertiseAddress: "edge.example.com:4433",
	}); err != nil {
		t.Fatalf("add dynamic listener: %v", err)
	}

	res, err = a.ManageListener(&health.ListenerManageRequest{Action: "list"})
	if err != nil {
		t.Fatalf("list after add: %v", err)
	}
	entry, ok := listenerByAddr(res, dynAddr)
	if !ok {
		t.Fatalf("dynamic listener %q missing after add: %+v", dynAddr, res.Listeners)
	}
	if !entry.Dynamic {
		t.Errorf("dynamic listener Dynamic=false, want true")
	}
	if !entry.PlainText {
		t.Errorf("dynamic listener PlainText=false, want true")
	}
	if entry.AdvertiseAddress != "edge.example.com:4433" {
		t.Errorf("dynamic listener AdvertiseAddress=%q, want edge.example.com:4433", entry.AdvertiseAddress)
	}

	// Advertised listeners reflect the dynamic add.
	var advertised bool
	for _, l := range a.getAdvertisedListeners() {
		if l.Address == "edge.example.com:4433" {
			advertised = true
		}
	}
	if !advertised {
		t.Errorf("dynamic listener advertise target not reflected in getAdvertisedListeners")
	}

	// Re-adding the same address replaces in place (still a single entry).
	if _, err := a.ManageListener(&health.ListenerManageRequest{Action: "add", Transport: "ws", Address: dynAddr, Path: "/ws2/", PlainText: true}); err != nil {
		t.Fatalf("replace dynamic listener: %v", err)
	}
	res, _ = a.ManageListener(&health.ListenerManageRequest{Action: "list"})
	count := 0
	for _, l := range res.Listeners {
		if l.Address == dynAddr {
			count++
			if l.Path != "/ws2/" {
				t.Errorf("replaced listener Path=%q, want /ws2/", l.Path)
			}
		}
	}
	if count != 1 {
		t.Errorf("after replace, %d entries for %q, want 1", count, dynAddr)
	}

	// Remove deletes the dynamic listener.
	if _, err := a.ManageListener(&health.ListenerManageRequest{Action: "remove", Address: dynAddr}); err != nil {
		t.Fatalf("remove dynamic listener: %v", err)
	}
	res, _ = a.ManageListener(&health.ListenerManageRequest{Action: "list"})
	if _, ok := listenerByAddr(res, dynAddr); ok {
		t.Errorf("dynamic listener still present after remove")
	}
}

// TestAgent_ManageListener_ReplayAcrossRestart proves a dynamic transport
// listener is journalled and restored (still flagged Dynamic) after a restart.
func TestAgent_ManageListener_ReplayAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	dynAddr := freeTCPAddr(t)

	a := newTestAgent(t, dir)
	if _, err := a.ManageListener(&health.ListenerManageRequest{
		Action:    "add",
		Transport: "ws",
		Address:   dynAddr,
		Path:      "/ws/",
		PlainText: true,
	}); err != nil {
		t.Fatalf("add dynamic listener: %v", err)
	}

	events, err := a.journal.Load()
	if err != nil {
		t.Fatalf("journal Load: %v", err)
	}
	var sawListenerAdd bool
	for _, e := range events {
		if e.Address == dynAddr && e.Transport == "ws" {
			sawListenerAdd = true
		}
	}
	if !sawListenerAdd {
		t.Errorf("listener_add event missing from journal: %+v", events)
	}

	if err := a.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	b := newTestAgent(t, dir)
	defer b.Stop()
	if err := b.ReplayDynamicConfig(); err != nil {
		t.Fatalf("ReplayDynamicConfig: %v", err)
	}

	res, err := b.ManageListener(&health.ListenerManageRequest{Action: "list"})
	if err != nil {
		t.Fatalf("list after replay: %v", err)
	}
	entry, ok := listenerByAddr(res, dynAddr)
	if !ok {
		t.Fatalf("dynamic listener %q missing after replay: %+v", dynAddr, res.Listeners)
	}
	if !entry.Dynamic {
		t.Errorf("replayed listener Dynamic=false, want true")
	}
	if entry.Transport != "ws" {
		t.Errorf("replayed listener Transport=%q, want ws", entry.Transport)
	}
}
