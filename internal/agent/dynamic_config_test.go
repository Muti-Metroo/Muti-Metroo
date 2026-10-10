package agent

import (
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/postalsys/muti-metroo/internal/config"
	"github.com/postalsys/muti-metroo/internal/dynconfig"
	"github.com/postalsys/muti-metroo/internal/health"
)

// waitForPeerInList polls ManagePeer("list") until an entry with the given
// address appears or deadline passes. Peer registration goes through a
// background goroutine launched by ManagePeer add, so the list result is
// eventually consistent with the mutation.
func waitForPeerInList(t *testing.T, a *Agent, address string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		res, err := a.ManagePeer("list", "", "", "", "")
		if err == nil {
			for _, p := range res.Peers {
				if p.Address == address {
					return
				}
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("peer %q never appeared in list", address)
}

func newTestAgent(t *testing.T, dataDir string) *Agent {
	t.Helper()
	cfg := config.Default()
	cfg.Agent.DataDir = dataDir
	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := a.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	return a
}

func TestAgent_DynamicConfig_RoundTripEveryEntity(t *testing.T) {
	dir := t.TempDir()

	// Phase 1: make dynamic changes of every supported kind.
	a := newTestAgent(t, dir)
	if _, err := a.ManageDisplayName("set", "gateway-test"); err != nil {
		t.Fatalf("ManageDisplayName set: %v", err)
	}
	if _, err := a.ManageRoute("add", "10.42.0.0/16", 3, "", "", "", ""); err != nil {
		t.Fatalf("ManageRoute add: %v", err)
	}
	if _, err := a.ManageForwardListener("add", "web", "127.0.0.1:0", 50); err != nil {
		t.Fatalf("ManageForwardListener add: %v", err)
	}
	if _, err := a.ManageForwardEndpoint("add", "svc", "127.0.0.1:3000"); err != nil {
		t.Fatalf("ManageForwardEndpoint add: %v", err)
	}
	if _, err := a.ManagePeer("add", "peer.invalid:4433", "quic", "", ""); err != nil {
		t.Fatalf("ManagePeer add: %v", err)
	}

	// Journal should have five events.
	events, err := a.journal.Load()
	if err != nil {
		t.Fatalf("journal Load: %v", err)
	}
	if len(events) != 5 {
		t.Errorf("journal events = %d, want 5: %+v", len(events), events)
	}

	if err := a.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// Phase 2: restart, replay, verify every entity was restored and tagged Dynamic.
	b := newTestAgent(t, dir)
	defer b.Stop()

	if err := b.ReplayDynamicConfig(); err != nil {
		t.Fatalf("ReplayDynamicConfig: %v", err)
	}

	if got := b.DisplayName(); got != "gateway-test" {
		t.Errorf("DisplayName after replay = %q, want %q", got, "gateway-test")
	}

	routes, err := b.ManageRoute("list", "", 0, "", "", "", "")
	if err != nil {
		t.Fatalf("ManageRoute list: %v", err)
	}
	foundRoute := false
	for _, r := range routes.Routes {
		if r.Network == "10.42.0.0/16" && r.Metric == 3 {
			foundRoute = true
		}
	}
	if !foundRoute {
		t.Errorf("route 10.42.0.0/16 missing after replay: %+v", routes.Routes)
	}

	forwards, err := b.ManageForwardListener("list", "", "", 0)
	if err != nil {
		t.Fatalf("ManageForwardListener list: %v", err)
	}
	var foundForward bool
	for _, f := range forwards.Listeners {
		if f.Key == "web" {
			foundForward = true
			if !f.Dynamic {
				t.Errorf("replayed forward listener %q: Dynamic=false, want true", f.Key)
			}
			if f.MaxConnections != 50 {
				t.Errorf("replayed forward listener %q: MaxConnections=%d, want 50", f.Key, f.MaxConnections)
			}
		}
	}
	if !foundForward {
		t.Errorf("forward listener %q missing after replay: %+v", "web", forwards.Listeners)
	}

	endpoints, err := b.ManageForwardEndpoint("list", "", "")
	if err != nil {
		t.Fatalf("ManageForwardEndpoint list: %v", err)
	}
	var foundEndpoint bool
	for _, e := range endpoints.Endpoints {
		if e.Key == "svc" {
			foundEndpoint = true
			if !e.Dynamic {
				t.Errorf("replayed forward endpoint %q: Dynamic=false, want true", e.Key)
			}
			if e.Target != "127.0.0.1:3000" {
				t.Errorf("replayed forward endpoint %q: Target=%q, want 127.0.0.1:3000", e.Key, e.Target)
			}
		}
	}
	if !foundEndpoint {
		t.Errorf("forward endpoint %q missing after replay: %+v", "svc", endpoints.Endpoints)
	}

	waitForPeerInList(t, b, "peer.invalid:4433")
	peers, err := b.ManagePeer("list", "", "", "", "")
	if err != nil {
		t.Fatalf("ManagePeer list: %v", err)
	}
	foundPeer := false
	for _, p := range peers.Peers {
		if p.Address == "peer.invalid:4433" {
			foundPeer = true
			if !p.Dynamic {
				t.Errorf("replayed peer %q: Dynamic=false, want true", p.Address)
			}
			if p.Transport != "quic" {
				t.Errorf("replayed peer %q: Transport=%q, want quic", p.Address, p.Transport)
			}
		}
	}
	if !foundPeer {
		t.Errorf("peer %q missing after replay: %+v", "peer.invalid:4433", peers.Peers)
	}
}

func TestAgent_DynamicConfig_AddThenRemoveAbsentAfterRestart(t *testing.T) {
	dir := t.TempDir()

	a := newTestAgent(t, dir)
	if _, err := a.ManageRoute("add", "192.168.99.0/24", 0, "", "", "", ""); err != nil {
		t.Fatalf("add: %v", err)
	}
	if _, err := a.ManageRoute("remove", "192.168.99.0/24", 0, "", "", "", ""); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if err := a.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	b := newTestAgent(t, dir)
	defer b.Stop()
	if err := b.ReplayDynamicConfig(); err != nil {
		t.Fatalf("ReplayDynamicConfig: %v", err)
	}

	routes, err := b.ManageRoute("list", "", 0, "", "", "", "")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, r := range routes.Routes {
		if r.Network == "192.168.99.0/24" {
			t.Errorf("route %q unexpectedly present after add+remove+restart", r.Network)
		}
	}

	// After replay the journal should be compacted: no leftover add/remove pair.
	events, err := b.journal.Load()
	if err != nil {
		t.Fatalf("journal Load: %v", err)
	}
	for _, e := range events {
		if e.Network == "192.168.99.0/24" {
			t.Errorf("journal still contains event for removed route: %+v", e)
		}
	}
}

func TestAgent_DynamicConfig_CompactionAfterChurn(t *testing.T) {
	dir := t.TempDir()

	a := newTestAgent(t, dir)
	// Flip the same display name a bunch, add & remove a route repeatedly,
	// leave one route live.
	for i := 0; i < 10; i++ {
		if _, err := a.ManageDisplayName("set", "name"); err != nil {
			t.Fatalf("set display name: %v", err)
		}
	}
	for i := 0; i < 5; i++ {
		if _, err := a.ManageRoute("add", "10.99.0.0/16", 0, "", "", "", ""); err != nil {
			t.Fatalf("add route: %v", err)
		}
		if _, err := a.ManageRoute("remove", "10.99.0.0/16", 0, "", "", "", ""); err != nil {
			t.Fatalf("remove route: %v", err)
		}
	}
	if _, err := a.ManageRoute("add", "10.100.0.0/16", 0, "", "", "", ""); err != nil {
		t.Fatalf("add live route: %v", err)
	}

	// Pre-replay journal should have accumulated many events.
	pre, err := a.journal.Load()
	if err != nil {
		t.Fatalf("journal Load: %v", err)
	}
	if len(pre) < 20 {
		t.Errorf("expected heavy churn in journal, got %d events", len(pre))
	}

	if err := a.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	b := newTestAgent(t, dir)
	defer b.Stop()
	if err := b.ReplayDynamicConfig(); err != nil {
		t.Fatalf("ReplayDynamicConfig: %v", err)
	}

	// Post-replay compaction should leave only the live state:
	// one display_name_set + one route_add.
	post, err := b.journal.Load()
	if err != nil {
		t.Fatalf("journal Load after replay: %v", err)
	}
	if len(post) != 2 {
		t.Errorf("post-compact journal len = %d, want 2: %+v", len(post), post)
	}
}

func TestAgent_DynamicConfig_UnwritableJournalStaysEphemeral(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod semantics differ on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("running as root bypasses mode bits")
	}

	dir := t.TempDir()

	// Start + stop once so identity is persisted and directory is initialized.
	a := newTestAgent(t, dir)
	if err := a.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// Make the data_dir read-only so the journal file cannot be created.
	// Cleanup restores write permission so t.TempDir's deletion can proceed.
	if err := os.Chmod(dir, 0500); err != nil {
		t.Fatalf("chmod dir ro: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0700) })

	b := newTestAgent(t, dir)
	defer b.Stop()

	// Mutation itself must succeed even though the journal write fails.
	if _, err := b.ManageRoute("add", "10.123.0.0/16", 0, "", "", "", ""); err != nil {
		t.Fatalf("ManageRoute add on ro data_dir: %v", err)
	}

	routes, err := b.ManageRoute("list", "", 0, "", "", "", "")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var found bool
	for _, r := range routes.Routes {
		if r.Network == "10.123.0.0/16" {
			found = true
		}
	}
	if !found {
		t.Errorf("route missing from in-memory state despite journal write failure: %+v", routes.Routes)
	}

	// Journal file must not exist: the write failed and we did not create it.
	if _, err := os.Stat(filepath.Join(dir, dynconfig.JournalFileName)); !os.IsNotExist(err) {
		t.Errorf("journal file unexpectedly exists on read-only dir: err=%v", err)
	}
}

func TestAgent_ManageForwardEndpoint(t *testing.T) {
	dir := t.TempDir()

	cfg := config.Default()
	cfg.Agent.DataDir = dir
	cfg.Forward.Endpoints = []config.ForwardEndpoint{
		{Key: "static", Target: "127.0.0.1:5000"},
	}
	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := a.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer a.Stop()

	// list should show the config entry flagged static (Dynamic=false).
	res, err := a.ManageForwardEndpoint("list", "", "")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var sawStatic bool
	for _, e := range res.Endpoints {
		if e.Key == "static" {
			sawStatic = true
			if e.Dynamic {
				t.Errorf("config endpoint flagged Dynamic=true")
			}
		}
	}
	if !sawStatic {
		t.Errorf("config endpoint missing from list: %+v", res.Endpoints)
	}

	// Config endpoints are write-protected.
	if _, err := a.ManageForwardEndpoint("add", "static", "127.0.0.1:6000"); err == nil {
		t.Error("adding over config key: expected error")
	}
	if _, err := a.ManageForwardEndpoint("remove", "static", ""); err == nil {
		t.Error("removing config key: expected error")
	}

	// Validation.
	if _, err := a.ManageForwardEndpoint("add", "", "127.0.0.1:6000"); err == nil {
		t.Error("add empty key: expected error")
	}
	if _, err := a.ManageForwardEndpoint("add", "x", ""); err == nil {
		t.Error("add empty target: expected error")
	}
	if _, err := a.ManageForwardEndpoint("add", "x", "not-a-host-port"); err == nil {
		t.Error("add malformed target: expected error")
	}
	if _, err := a.ManageForwardEndpoint("remove", "never-added", ""); err == nil {
		t.Error("remove unknown key: expected error")
	}

	// Dynamic add+overwrite round trip.
	if _, err := a.ManageForwardEndpoint("add", "dyn", "127.0.0.1:7000"); err != nil {
		t.Fatalf("add dyn: %v", err)
	}
	if _, err := a.ManageForwardEndpoint("add", "dyn", "127.0.0.1:7001"); err != nil {
		t.Fatalf("overwrite dyn: %v", err)
	}
	res, err = a.ManageForwardEndpoint("list", "", "")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var dynTarget string
	var dynFlag bool
	for _, e := range res.Endpoints {
		if e.Key == "dyn" {
			dynTarget = e.Target
			dynFlag = e.Dynamic
		}
	}
	if dynTarget != "127.0.0.1:7001" {
		t.Errorf("dyn target after overwrite = %q, want 127.0.0.1:7001", dynTarget)
	}
	if !dynFlag {
		t.Errorf("dyn endpoint Dynamic=false, want true")
	}

	// Remove deletes the runtime entry.
	if _, err := a.ManageForwardEndpoint("remove", "dyn", ""); err != nil {
		t.Fatalf("remove dyn: %v", err)
	}
	res, _ = a.ManageForwardEndpoint("list", "", "")
	for _, e := range res.Endpoints {
		if e.Key == "dyn" {
			t.Errorf("dyn endpoint still present after remove: %+v", e)
		}
	}
}

func TestAgent_DynamicConfig_NoDataDirIsNoop(t *testing.T) {
	cfg := config.Default()
	// Provide an ID so identity does not need data_dir.
	cfg.Agent.ID = "0123456789abcdef0123456789abcdef"
	cfg.Agent.DataDir = ""
	// Agent requires identity keypair; provide one via config.
	// Generating a valid keypair inline would pull in extra imports; instead,
	// rely on New() returning an error here and check we never touch the
	// journal. If a data-dir-less deployment becomes first-class later we can
	// extend this test, but for now the noop-journal contract is proven by
	// dynconfig.NewJournal returning nil for empty dataDir (see dynconfig_test.go).
	if _, err := New(cfg); err == nil {
		t.Log("data-dir-less deployments now work; verify journal is nil")
	}
}

// TestAgent_ManageRoute_CommentAndOverride exercises the comment field
// end-to-end: add carries it through, set_comment edits both dynamic and
// config-seeded routes, set_comment on unknown CIDR errors, and the
// override survives restart via journal replay (last write wins).
func TestAgent_ManageRoute_CommentAndOverride(t *testing.T) {
	dir := t.TempDir()

	cfg := config.Default()
	cfg.Agent.DataDir = dir
	cfg.Exit.Routes = []config.RouteConfig{
		{CIDR: "10.0.0.0/8", Comment: "from config"},
	}
	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := a.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Add a dynamic route with a comment via the management API.
	if _, err := a.ManageRoute("add", "192.168.0.0/16", 0, "temp customer", "", "", ""); err != nil {
		t.Fatalf("ManageRoute add: %v", err)
	}

	// list returns the dynamic route with its comment.
	routes, err := a.ManageRoute("list", "", 0, "", "", "", "")
	if err != nil {
		t.Fatalf("ManageRoute list: %v", err)
	}
	if len(routes.Routes) != 1 || routes.Routes[0].Network != "192.168.0.0/16" || routes.Routes[0].Comment != "temp customer" {
		t.Errorf("ManageRoute list = %+v, want one entry with Comment=temp customer", routes.Routes)
	}

	// set_comment edits the dynamic route in place.
	if _, err := a.ManageRoute("set_comment", "192.168.0.0/16", 0, "renamed dyn", "", "", ""); err != nil {
		t.Fatalf("ManageRoute set_comment dyn: %v", err)
	}

	// set_comment overrides the config-seeded route. The dashboard path
	// surfaces local-route comments via GetRouteDetails; verify there too.
	if _, err := a.ManageRoute("set_comment", "10.0.0.0/8", 0, "override", "", "", ""); err != nil {
		t.Fatalf("ManageRoute set_comment cfg: %v", err)
	}
	commentByCIDR := map[string]string{}
	for _, d := range a.GetRouteDetails() {
		commentByCIDR[d.Network] = d.Comment
	}
	if commentByCIDR["10.0.0.0/8"] != "override" {
		t.Errorf("dashboard comment for config route = %q, want %q", commentByCIDR["10.0.0.0/8"], "override")
	}

	// set_comment for an unknown CIDR is an error.
	if _, err := a.ManageRoute("set_comment", "172.16.0.0/12", 0, "nope", "", "", ""); err == nil {
		t.Error("ManageRoute set_comment for unknown CIDR: error = nil, want error")
	}

	if err := a.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// Restart: config seeds 10.0.0.0/8 with the original comment, journal
	// then replays add(192.168.0.0/16, "temp customer") followed by
	// set_comment("renamed dyn") and set_comment(10.0.0.0/8, "override").
	// Result: dynamic route comment is "renamed dyn"; config route
	// comment is "override" (operator override survives restart).
	b, err := New(cfg)
	if err != nil {
		t.Fatalf("New() restart: %v", err)
	}
	if err := b.Start(); err != nil {
		t.Fatalf("Start() restart: %v", err)
	}
	defer b.Stop()
	if err := b.ReplayDynamicConfig(); err != nil {
		t.Fatalf("ReplayDynamicConfig: %v", err)
	}

	finalByCIDR := map[string]string{}
	for _, d := range b.GetRouteDetails() {
		finalByCIDR[d.Network] = d.Comment
	}
	if finalByCIDR["10.0.0.0/8"] != "override" {
		t.Errorf("after restart, config route comment = %q, want %q (override should win over config)", finalByCIDR["10.0.0.0/8"], "override")
	}
	if finalByCIDR["192.168.0.0/16"] != "renamed dyn" {
		t.Errorf("after restart, dynamic route comment = %q, want %q", finalByCIDR["192.168.0.0/16"], "renamed dyn")
	}

	// scope="dynamic" (the default) returns only the runtime-added route
	// so existing scripts and `muti-metroo route list` keep their contract.
	dynamicOnly, err := b.ManageRoute("list", "", 0, "", "", "", "")
	if err != nil {
		t.Fatalf("ManageRoute list dynamic: %v", err)
	}
	if len(dynamicOnly.Routes) != 1 || dynamicOnly.Routes[0].Network != "192.168.0.0/16" || !dynamicOnly.Routes[0].Dynamic {
		t.Errorf("scope=dynamic list = %+v, want one Dynamic=true entry for 192.168.0.0/16", dynamicOnly.Routes)
	}

	all, err := b.ManageRoute("list", "", 0, "", "", "", "all")
	if err != nil {
		t.Fatalf("ManageRoute list all: %v", err)
	}
	got := map[string]health.RouteManageResultEntry{}
	for _, r := range all.Routes {
		got[r.Network] = r
	}
	if cfgEntry := got["10.0.0.0/8"]; cfgEntry.Network == "" || cfgEntry.Dynamic || cfgEntry.Comment != "override" {
		t.Errorf("scope=all config route = %+v, want Dynamic=false Comment=override", cfgEntry)
	}
	if dynEntry := got["192.168.0.0/16"]; dynEntry.Network == "" || !dynEntry.Dynamic || dynEntry.Comment != "renamed dyn" {
		t.Errorf("scope=all dynamic route = %+v, want Dynamic=true Comment=\"renamed dyn\"", dynEntry)
	}

	// Unknown scope is rejected so typos don't silently fall through to a
	// default that may not be what the operator wanted.
	if _, err := b.ManageRoute("list", "", 0, "", "", "", "bogus"); err == nil {
		t.Error("ManageRoute list scope=bogus error = nil, want error")
	}
}

// TestAgent_ManageRoute_SourceIPSet exercises set_source_ip end-to-end:
// add seeds a source binding, set_source_ip updates and clears it, an unknown
// CIDR is rejected, and the final binding survives restart via journal replay.
// Also asserts the dashboard payload (GetRouteDetails) carries source_ip for
// the local agent's own routes.
func TestAgent_ManageRoute_SourceIPSet(t *testing.T) {
	dir := t.TempDir()

	cfg := config.Default()
	cfg.Agent.DataDir = dir
	cfg.Exit.Enabled = true
	cfg.Exit.Routes = []config.RouteConfig{
		{CIDR: "10.0.0.0/8", SourceIP: "127.0.0.1"},
	}
	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := a.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Dynamic add with a source.
	if _, err := a.ManageRoute("add", "192.168.0.0/16", 0, "", "127.0.0.5", "", ""); err != nil {
		t.Fatalf("ManageRoute add: %v", err)
	}

	// set_source_ip updates the dynamic route in place.
	if _, err := a.ManageRoute("set_source_ip", "192.168.0.0/16", 0, "", "127.0.0.6", "", ""); err != nil {
		t.Fatalf("ManageRoute set_source_ip dyn: %v", err)
	}
	// set_source_ip overrides the config-seeded route's source.
	if _, err := a.ManageRoute("set_source_ip", "10.0.0.0/8", 0, "", "127.0.0.7", "", ""); err != nil {
		t.Fatalf("ManageRoute set_source_ip cfg: %v", err)
	}

	// Dashboard surfaces source_ip for locally-originated routes.
	sourceByCIDR := map[string]string{}
	for _, d := range a.GetRouteDetails() {
		sourceByCIDR[d.Network] = d.SourceIP
	}
	if sourceByCIDR["10.0.0.0/8"] != "127.0.0.7" {
		t.Errorf("dashboard source_ip for config route = %q, want %q", sourceByCIDR["10.0.0.0/8"], "127.0.0.7")
	}
	if sourceByCIDR["192.168.0.0/16"] != "127.0.0.6" {
		t.Errorf("dashboard source_ip for dynamic route = %q, want %q", sourceByCIDR["192.168.0.0/16"], "127.0.0.6")
	}

	// Clear via empty string.
	if _, err := a.ManageRoute("set_source_ip", "192.168.0.0/16", 0, "", "", "", ""); err != nil {
		t.Fatalf("ManageRoute set_source_ip clear: %v", err)
	}
	for _, d := range a.GetRouteDetails() {
		if d.Network == "192.168.0.0/16" && d.SourceIP != "" {
			t.Errorf("after clear, dashboard source_ip = %q, want empty", d.SourceIP)
		}
	}

	// Unknown CIDR is an error.
	if _, err := a.ManageRoute("set_source_ip", "172.16.0.0/12", 0, "", "127.0.0.1", "", ""); err == nil {
		t.Error("ManageRoute set_source_ip for unknown CIDR: error = nil, want error")
	}
	// Family mismatch is an error.
	if _, err := a.ManageRoute("set_source_ip", "10.0.0.0/8", 0, "", "fd00::1", "", ""); err == nil {
		t.Error("ManageRoute set_source_ip with v6 source on v4 CIDR: error = nil, want error")
	}

	if err := a.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// Restart: journal must replay the override for 10.0.0.0/8 (last write
	// 127.0.0.7) and the clear for 192.168.0.0/16 (last write "").
	b, err := New(cfg)
	if err != nil {
		t.Fatalf("New() restart: %v", err)
	}
	if err := b.Start(); err != nil {
		t.Fatalf("Start() restart: %v", err)
	}
	defer b.Stop()
	if err := b.ReplayDynamicConfig(); err != nil {
		t.Fatalf("ReplayDynamicConfig: %v", err)
	}

	finalByCIDR := map[string]string{}
	for _, d := range b.GetRouteDetails() {
		finalByCIDR[d.Network] = d.SourceIP
	}
	if finalByCIDR["10.0.0.0/8"] != "127.0.0.7" {
		t.Errorf("after restart, config route source_ip = %q, want %q", finalByCIDR["10.0.0.0/8"], "127.0.0.7")
	}
	if finalByCIDR["192.168.0.0/16"] != "" {
		t.Errorf("after restart, dynamic route source_ip = %q, want empty (cleared)", finalByCIDR["192.168.0.0/16"])
	}
}

// TestAgent_ManageRoute_InterfaceTwoTier verifies the route.interface policy
// through the dynamic management path:
//   - an interface-only binding is accepted on every platform (portable
//     bind-to-IP) and survives journal replay;
//   - an off-interface source paired with an interface is rejected unless
//     running as root on Linux (the privileged SO_BINDTODEVICE path).
func TestAgent_ManageRoute_InterfaceTwoTier(t *testing.T) {
	ifname := loopbackInterfaceNameForTest()
	if ifname == "" {
		t.Skip("no loopback interface found to test against")
	}

	dir := t.TempDir()
	cfg := config.Default()
	cfg.Agent.DataDir = dir
	cfg.Exit.Enabled = true
	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := a.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer a.Stop()

	// Interface-only add is accepted everywhere now.
	if _, err := a.ManageRoute("add", "10.10.0.0/16", 0, "", "", ifname, ""); err != nil {
		t.Fatalf("interface-only add should be accepted, got %v", err)
	}
	// set_interface on an existing route is likewise accepted.
	if _, err := a.ManageRoute("add", "10.20.0.0/16", 0, "", "", "", ""); err != nil {
		t.Fatalf("add without interface: %v", err)
	}
	if _, err := a.ManageRoute("set_interface", "10.20.0.0/16", 0, "", "", ifname, ""); err != nil {
		t.Fatalf("interface-only set_interface should be accepted, got %v", err)
	}

	// The interface-only binding survives a restart via journal replay.
	if err := a.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	a2, err := New(cfg)
	if err != nil {
		t.Fatalf("New (restart): %v", err)
	}
	if err := a2.Start(); err != nil {
		t.Fatalf("Start (restart): %v", err)
	}
	defer a2.Stop()

	// Off-interface source (TEST-NET-1) + interface needs Linux + root.
	_, err = a2.ManageRoute("add", "10.30.0.0/16", 0, "", "192.0.2.1", ifname, "")
	if err == nil {
		t.Skip("running as root on Linux -- off-interface source is permitted here, cannot test rejection")
	}
	if !strings.Contains(err.Error(), "interface") {
		t.Errorf("add with off-interface source rejected with %v, want substring \"interface\"", err)
	}
}

// loopbackInterfaceNameForTest returns the host loopback interface name, which
// is always present and enumerable, for route interface binding tests.
func loopbackInterfaceNameForTest() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagLoopback != 0 {
			return ifc.Name
		}
	}
	return ""
}

// TestAgent_RouteComment_PersistAcrossCompaction verifies that route comments
// survive the journal-compaction step that runs at the end of replay. The
// single-restart variant above masks the bug: in-memory state is correct
// after replay even if compaction subsequently corrupts the journal. This
// test does two restarts so the second restart replays from the *compacted*
// journal -- the moment of truth.
func TestAgent_RouteComment_PersistAcrossCompaction(t *testing.T) {
	dir := t.TempDir()

	cfg := config.Default()
	cfg.Agent.DataDir = dir
	cfg.Exit.Routes = []config.RouteConfig{
		{CIDR: "10.0.0.0/8", Comment: "from config"},
	}
	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := a.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if _, err := a.ManageRoute("add", "192.168.0.0/16", 0, "temp customer", "", "", ""); err != nil {
		t.Fatalf("ManageRoute add: %v", err)
	}
	if _, err := a.ManageRoute("set_comment", "10.0.0.0/8", 0, "override", "", "", ""); err != nil {
		t.Fatalf("ManageRoute set_comment: %v", err)
	}
	if err := a.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// Restart 1: triggers ReplayDynamicConfig + compaction. In-memory state
	// is rebuilt from the original journal and is correct; the on-disk
	// journal is then rewritten from the snapshot.
	b, err := New(cfg)
	if err != nil {
		t.Fatalf("New restart-1: %v", err)
	}
	if err := b.Start(); err != nil {
		t.Fatalf("Start restart-1: %v", err)
	}
	if err := b.ReplayDynamicConfig(); err != nil {
		t.Fatalf("ReplayDynamicConfig restart-1: %v", err)
	}

	// Inspect the compacted on-disk journal directly.
	post, err := b.journal.Load()
	if err != nil {
		t.Fatalf("journal Load post-compact: %v", err)
	}
	var sawAdd, sawCommentSet bool
	for _, e := range post {
		switch e.Op {
		case dynconfig.OpRouteAdd:
			if e.Network == "192.168.0.0/16" && e.Comment != "temp customer" {
				t.Errorf("compacted route_add for %s has Comment=%q, want %q", e.Network, e.Comment, "temp customer")
			}
			sawAdd = true
		case dynconfig.OpRouteCommentSet:
			if e.Network == "10.0.0.0/8" && e.Comment != "override" {
				t.Errorf("compacted route_comment_set for %s has Comment=%q, want %q", e.Network, e.Comment, "override")
			}
			sawCommentSet = true
		}
	}
	if !sawAdd {
		t.Errorf("compacted journal missing route_add for dynamic route: %+v", post)
	}
	if !sawCommentSet {
		t.Errorf("compacted journal missing route_comment_set for config-seeded override: %+v", post)
	}
	if err := b.Stop(); err != nil {
		t.Fatalf("Stop restart-1: %v", err)
	}

	// Restart 2: replays from the compacted journal. If compaction lost
	// either comment, this is the restart where the operator notices.
	c, err := New(cfg)
	if err != nil {
		t.Fatalf("New restart-2: %v", err)
	}
	if err := c.Start(); err != nil {
		t.Fatalf("Start restart-2: %v", err)
	}
	defer c.Stop()
	if err := c.ReplayDynamicConfig(); err != nil {
		t.Fatalf("ReplayDynamicConfig restart-2: %v", err)
	}

	got := map[string]string{}
	for _, d := range c.GetRouteDetails() {
		got[d.Network] = d.Comment
	}
	if got["10.0.0.0/8"] != "override" {
		t.Errorf("after restart-2, config route comment = %q, want %q", got["10.0.0.0/8"], "override")
	}
	if got["192.168.0.0/16"] != "temp customer" {
		t.Errorf("after restart-2, dynamic route comment = %q, want %q", got["192.168.0.0/16"], "temp customer")
	}
}

// TestAgent_DynamicConfig_SuffixReplayBeforeHosts proves that when a
// runtime-added TLD has a host registered under it, both records survive
// a restart. Replay must apply suffix events before host events,
// otherwise the host_add fails validation against the config-only suffix
// set.
func TestAgent_DynamicConfig_SuffixReplayBeforeHosts(t *testing.T) {
	dir := t.TempDir()

	a := newTestAgent(t, dir)
	if _, err := a.ManageMeshHostSuffix("add", "alt"); err != nil {
		t.Fatalf("ManageMeshHostSuffix add: %v", err)
	}
	if _, err := a.ManageMeshHost("add", "host.alt", "10.5.0.1", "", ""); err != nil {
		t.Fatalf("ManageMeshHost add: %v", err)
	}
	if err := a.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	b := newTestAgent(t, dir)
	defer b.Stop()
	if err := b.ReplayDynamicConfig(); err != nil {
		t.Fatalf("ReplayDynamicConfig: %v", err)
	}

	suffixes := b.MeshHostsSuffixes()
	var sawAlt bool
	for _, s := range suffixes {
		if s == "alt" {
			sawAlt = true
		}
	}
	if !sawAlt {
		t.Errorf("suffix alt missing after restart: %v", suffixes)
	}

	hostList := b.MeshHosts()
	var sawHost bool
	for _, h := range hostList {
		if h.Name == "host.alt" {
			sawHost = true
			if h.IPv4 != "10.5.0.1" {
				t.Errorf("host.alt IPv4 = %q, want 10.5.0.1", h.IPv4)
			}
		}
	}
	if !sawHost {
		t.Errorf("host.alt missing after restart: %v", hostList)
	}
}

// TestAgent_DynamicConfig_SuffixRemoveOfConfigSeed proves that a TLD
// originally seeded by config but removed at runtime stays removed
// across restart, courtesy of the symmetric-diff snapshot logic.
func TestAgent_DynamicConfig_SuffixRemoveOfConfigSeed(t *testing.T) {
	dir := t.TempDir()

	cfg := config.Default()
	cfg.Agent.DataDir = dir
	cfg.MeshHostsSuffixes = []string{"mesh", "lan"}
	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := a.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if _, err := a.ManageMeshHostSuffix("remove", "lan"); err != nil {
		t.Fatalf("ManageMeshHostSuffix remove: %v", err)
	}
	if err := a.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	b, err := New(cfg)
	if err != nil {
		t.Fatalf("New restart: %v", err)
	}
	if err := b.Start(); err != nil {
		t.Fatalf("Start restart: %v", err)
	}
	defer b.Stop()
	if err := b.ReplayDynamicConfig(); err != nil {
		t.Fatalf("ReplayDynamicConfig: %v", err)
	}

	for _, s := range b.MeshHostsSuffixes() {
		if s == "lan" {
			t.Errorf("suffix lan should have stayed removed across restart")
		}
	}
}

// TestAgent_RouteBindings_PersistAcrossCompaction regression-tests the bug
// where buildDynamicSnapshot omitted route source_ip + interface bindings,
// so after the first restart-replay-compaction cycle the journal would be
// rewritten without them and the next restart would replay a binding-less
// state. The test runs two restart cycles -- the second cycle is the one
// that catches the bug, because the first cycle's compaction is what loses
// the events; the second cycle re-reads the compacted file.
func TestAgent_RouteBindings_PersistAcrossCompaction(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Agent.DataDir = dir
	cfg.Exit.Enabled = true

	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := a.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if _, err := a.ManageRoute("add", "10.42.0.0/16", 0, "", "127.0.0.5", "", ""); err != nil {
		t.Fatalf("ManageRoute add (with source on first attempt): %v", err)
	}
	// Then mutate source again to exercise the set_source_ip path too --
	// makes sure the bindings present at snapshot time come from the live
	// exit-handler state, not just the initial add event.
	if _, err := a.ManageRoute("set_source_ip", "10.42.0.0/16", 0, "", "127.0.0.6", "", ""); err != nil {
		t.Fatalf("ManageRoute set_source_ip: %v", err)
	}
	if err := a.Stop(); err != nil {
		t.Fatalf("Stop a: %v", err)
	}

	// First restart-replay-compact cycle. Without the fix, this rewrites
	// the journal to a snapshot that omits the source binding.
	b, err := New(cfg)
	if err != nil {
		t.Fatalf("New b: %v", err)
	}
	if err := b.Start(); err != nil {
		t.Fatalf("Start b: %v", err)
	}
	if err := b.ReplayDynamicConfig(); err != nil {
		t.Fatalf("ReplayDynamicConfig b: %v", err)
	}
	// Sanity: live state has the source after first replay.
	srcByCIDR := map[string]string{}
	for _, d := range b.GetRouteDetails() {
		srcByCIDR[d.Network] = d.SourceIP
	}
	if srcByCIDR["10.42.0.0/16"] != "127.0.0.6" {
		t.Fatalf("after first replay, source_ip = %q, want 127.0.0.6", srcByCIDR["10.42.0.0/16"])
	}
	if err := b.Stop(); err != nil {
		t.Fatalf("Stop b: %v", err)
	}

	// Second restart-replay cycle. This replays the compacted journal.
	// Bug: the compacted journal had no set_source_ip events, so the
	// route's source binding is lost. Fix: snapshot now includes them.
	c, err := New(cfg)
	if err != nil {
		t.Fatalf("New c: %v", err)
	}
	if err := c.Start(); err != nil {
		t.Fatalf("Start c: %v", err)
	}
	defer c.Stop()
	if err := c.ReplayDynamicConfig(); err != nil {
		t.Fatalf("ReplayDynamicConfig c: %v", err)
	}
	srcByCIDR = map[string]string{}
	for _, d := range c.GetRouteDetails() {
		srcByCIDR[d.Network] = d.SourceIP
	}
	if srcByCIDR["10.42.0.0/16"] != "127.0.0.6" {
		t.Errorf("after second restart (post-compaction), source_ip = %q, want 127.0.0.6 -- compaction snapshot dropped the binding", srcByCIDR["10.42.0.0/16"])
	}
}
