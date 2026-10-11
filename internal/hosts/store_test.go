package hosts

import (
	"errors"
	"sort"
	"strings"
	"testing"
)

func TestNewSuffixValidation(t *testing.T) {
	tests := []struct {
		name     string
		suffixes []string
		wantErr  bool
	}{
		{"default empty", nil, false},
		{"empty slice", []string{}, false},
		{"valid mesh", []string{"mesh"}, false},
		{"valid multiple", []string{"mesh", "lan", "alt"}, false},
		{"trims whitespace", []string{"  mesh  "}, false},
		{"lowercases", []string{"Mesh"}, false},
		{"dedupes", []string{"mesh", "MESH", "  mesh"}, false},
		{"multi-level suffix", []string{"muti-metroo.alt"}, false},
		{"deep multi-level suffix", []string{"lab.test.mesh"}, false},
		{"empty element rejected", []string{""}, true},
		{"leading dot rejected", []string{".mesh"}, true},
		{"trailing dot rejected", []string{"mesh."}, true},
		{"empty inner label rejected", []string{"my..mesh"}, true},
		{"bad inner label rejected", []string{"my.-bad.mesh"}, true},
		{"contains slash rejected", []string{"me/sh"}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, err := New(tc.suffixes, nil)
			if (err != nil) != tc.wantErr {
				t.Fatalf("New(%v): err=%v, wantErr=%v", tc.suffixes, err, tc.wantErr)
			}
			if err == nil && len(s.Suffixes()) == 0 {
				t.Fatalf("expected non-empty suffix set")
			}
		})
	}
}

func TestNewDefaultsToMesh(t *testing.T) {
	s, err := New(nil, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	got := s.Suffixes()
	if len(got) != 1 || got[0] != "mesh" {
		t.Fatalf("expected default [mesh], got %v", got)
	}
	if cfg := s.ConfigSuffixes(); len(cfg) != 1 || cfg[0] != "mesh" {
		t.Fatalf("expected ConfigSuffixes [mesh], got %v", cfg)
	}
}

func TestNewSeedValidation(t *testing.T) {
	tests := []struct {
		name    string
		seed    []Host
		wantErr error
	}{
		{
			name:    "valid single",
			seed:    []Host{{Name: "gw.mesh", IPv4: "10.0.0.1"}},
			wantErr: nil,
		},
		{
			name:    "missing suffix",
			seed:    []Host{{Name: "gw.example", IPv4: "10.0.0.1"}},
			wantErr: ErrInvalid,
		},
		{
			name:    "no IP",
			seed:    []Host{{Name: "gw.mesh"}},
			wantErr: ErrInvalid,
		},
		{
			name:    "bad ipv4",
			seed:    []Host{{Name: "gw.mesh", IPv4: "not-an-ip"}},
			wantErr: ErrInvalid,
		},
		{
			name:    "ipv6 in ipv4 field",
			seed:    []Host{{Name: "gw.mesh", IPv4: "fd00::1"}},
			wantErr: ErrInvalid,
		},
		{
			name:    "ipv4 in ipv6 field",
			seed:    []Host{{Name: "gw.mesh", IPv6: "10.0.0.1"}},
			wantErr: ErrInvalid,
		},
		{
			name: "duplicate name",
			seed: []Host{
				{Name: "gw.mesh", IPv4: "10.0.0.1"},
				{Name: "gw.mesh", IPv4: "10.0.0.2"},
			},
			wantErr: ErrExists,
		},
		{
			name:    "name too long",
			seed:    []Host{{Name: strings.Repeat("a", 250) + ".mesh", IPv4: "10.0.0.1"}},
			wantErr: ErrInvalid,
		},
		{
			name:    "empty prefix",
			seed:    []Host{{Name: ".mesh", IPv4: "10.0.0.1"}},
			wantErr: ErrInvalid,
		},
		{
			name:    "label with leading hyphen",
			seed:    []Host{{Name: "-bad.mesh", IPv4: "10.0.0.1"}},
			wantErr: ErrInvalid,
		},
		{
			name:    "uppercase normalized",
			seed:    []Host{{Name: "GW.Mesh", IPv4: "10.0.0.1"}},
			wantErr: nil,
		},
		{
			name:    "dual stack",
			seed:    []Host{{Name: "gw.mesh", IPv4: "10.0.0.1", IPv6: "fd00::1"}},
			wantErr: nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New([]string{"mesh"}, tc.seed)
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err=%v, want errors.Is(_, %v)", err, tc.wantErr)
			}
		})
	}
}

func TestStoreAddRemoveList(t *testing.T) {
	s, err := New([]string{"mesh"}, []Host{{Name: "seed.mesh", IPv4: "10.0.0.1", Comment: "from config"}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	added, err := s.Add(Host{Name: "Dyn.MESH", IPv4: "10.0.0.2"})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if added.Name != "dyn.mesh" {
		t.Errorf("expected normalized name dyn.mesh, got %q", added.Name)
	}

	if _, err := s.Add(Host{Name: "dyn.mesh", IPv4: "10.0.0.3"}); !errors.Is(err, ErrExists) {
		t.Errorf("expected ErrExists on dynamic dup, got %v", err)
	}
	if _, err := s.Add(Host{Name: "seed.mesh", IPv4: "10.0.0.3"}); !errors.Is(err, ErrExists) {
		t.Errorf("expected ErrExists when re-adding config name, got %v", err)
	}

	list := s.List()
	if len(list) != 2 || list[0].Name != "dyn.mesh" || list[1].Name != "seed.mesh" {
		t.Errorf("List() returned wrong order/contents: %+v", list)
	}

	dyn := s.Dynamic()
	if len(dyn) != 1 || dyn[0].Name != "dyn.mesh" {
		t.Errorf("Dynamic() returned %+v, expected one entry", dyn)
	}

	if _, err := s.Remove("missing.mesh"); !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound for missing, got %v", err)
	}
	if _, err := s.Remove("seed.mesh"); !errors.Is(err, ErrProtected) {
		t.Errorf("expected ErrProtected for config entry, got %v", err)
	}
	removed, err := s.Remove("DYN.mesh")
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if removed.Name != "dyn.mesh" || removed.IPv4 != "10.0.0.2" {
		t.Errorf("Remove returned unexpected entry: %+v", removed)
	}
	if got := s.List(); len(got) != 1 {
		t.Errorf("after remove, List() len=%d want 1", len(got))
	}
}

func TestStoreLookup(t *testing.T) {
	s, err := New([]string{"mesh"}, []Host{
		{Name: "cfg.mesh", IPv4: "10.0.0.1", Comment: "seeded"},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := s.Add(Host{Name: "dyn.mesh", IPv4: "10.0.0.2", IPv6: "fd00::2"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	tests := []struct {
		name    string
		query   string
		wantOK  bool
		wantIP4 string
	}{
		{"hit dynamic", "dyn.mesh", true, "10.0.0.2"},
		{"hit config", "cfg.mesh", true, "10.0.0.1"},
		{"case insensitive", "Dyn.Mesh", true, "10.0.0.2"},
		{"whitespace trimmed", "  cfg.mesh  ", true, "10.0.0.1"},
		{"miss", "nope.mesh", false, ""},
		{"empty input", "", false, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h, ok := s.Lookup(tc.query)
			if ok != tc.wantOK {
				t.Fatalf("Lookup(%q) ok=%v want %v", tc.query, ok, tc.wantOK)
			}
			if !tc.wantOK {
				if h != (Host{}) {
					t.Errorf("Lookup miss returned non-zero Host: %+v", h)
				}
				return
			}
			if h.IPv4 != tc.wantIP4 {
				t.Errorf("Lookup(%q).IPv4 = %q, want %q", tc.query, h.IPv4, tc.wantIP4)
			}
		})
	}
}

func TestStoreCustomSuffix(t *testing.T) {
	s, err := New([]string{"lan"}, []Host{{Name: "host.lan", IPv4: "10.0.0.1"}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := s.Add(Host{Name: "x.mesh", IPv4: "10.0.0.2"}); !errors.Is(err, ErrInvalid) {
		t.Errorf("expected ErrInvalid for wrong suffix, got %v", err)
	}
	if _, err := s.Add(Host{Name: "x.lan", IPv4: "10.0.0.2"}); err != nil {
		t.Errorf("Add with matching suffix: %v", err)
	}
}

func TestStoreBareSuffixHost(t *testing.T) {
	// A multi-label TLD itself can be registered as a host so muti-metroo /
	// mutiauk can resolve a bare suffix to a single IP
	// (e.g. "muti-metroo.alt -> 1.2.3.4"). Single-label bare names are rejected
	// separately -- see TestStoreRejectSingleLabelBareName.
	s, err := New([]string{"mesh", "muti-metroo.alt"}, []Host{
		{Name: "muti-metroo.alt", IPv4: "1.2.3.4"},
	})
	if err != nil {
		t.Fatalf("New with bare-suffix seed: %v", err)
	}

	// Bare seed appears in lookups.
	h, ok := s.Lookup("muti-metroo.alt")
	if !ok || h.IPv4 != "1.2.3.4" {
		t.Errorf("Lookup(muti-metroo.alt) = (%+v, %v), want (1.2.3.4, true)", h, ok)
	}

	// A bare-suffix host blocks RemoveSuffix.
	if err := s.RemoveSuffix("muti-metroo.alt"); !errors.Is(err, ErrSuffixInUse) {
		t.Errorf("RemoveSuffix with bare-suffix dependent: err=%v, want ErrSuffixInUse", err)
	}
	if !strings.Contains(s.RemoveSuffix("muti-metroo.alt").Error(), "muti-metroo.alt") {
		t.Error("error should list muti-metroo.alt as the dependent host")
	}

	// Names ending with -mesh still need a separator: -mesh is not a
	// suffix match for "mesh".
	if _, err := s.Add(Host{Name: "fakemesh", IPv4: "10.0.0.51"}); !errors.Is(err, ErrInvalid) {
		t.Errorf("Add(fakemesh) without separator: err=%v, want ErrInvalid", err)
	}
}

func TestStoreRejectSingleLabelBareName(t *testing.T) {
	// Bare single-label names (name == suffix, no dot) cannot resolve
	// end-to-end through systemd-resolved by default, so they are
	// rejected at registration time. Multi-label bare names are still
	// allowed.
	s, err := New([]string{"mesh", "muti-metroo.alt"}, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Reject single-label bare seed at construction time.
	if _, err := New([]string{"mesh"}, []Host{{Name: "mesh", IPv4: "1.2.3.4"}}); !errors.Is(err, ErrInvalid) {
		t.Errorf("New with single-label bare seed: err=%v, want ErrInvalid", err)
	}

	// Reject single-label bare add via the public API.
	if _, err := s.Add(Host{Name: "mesh", IPv4: "1.2.3.4"}); !errors.Is(err, ErrInvalid) {
		t.Errorf("Add(mesh): err=%v, want ErrInvalid", err)
	} else if !strings.Contains(err.Error(), "single-label") {
		t.Errorf("Add(mesh) error message = %q, want substring \"single-label\"", err.Error())
	}

	// Multi-label bare add still works.
	if _, err := s.Add(Host{Name: "muti-metroo.alt", IPv4: "10.0.0.50"}); err != nil {
		t.Errorf("Add(muti-metroo.alt): %v, want success", err)
	}

	// Sub-label still works under either suffix.
	if _, err := s.Add(Host{Name: "host.mesh", IPv4: "10.0.0.51"}); err != nil {
		t.Errorf("Add(host.mesh): %v, want success", err)
	}
}

func TestStoreMultiSuffix(t *testing.T) {
	s, err := New([]string{"mesh", "lan"}, []Host{
		{Name: "gw.mesh", IPv4: "10.0.0.1"},
		{Name: "gw.lan", IPv4: "10.0.0.2"},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	got := s.Suffixes()
	if len(got) != 2 || got[0] != "lan" || got[1] != "mesh" {
		t.Fatalf("Suffixes() = %v, want [lan mesh]", got)
	}
	if _, err := s.Add(Host{Name: "host.alt", IPv4: "10.0.0.3"}); !errors.Is(err, ErrInvalid) {
		t.Errorf("expected ErrInvalid for unconfigured suffix, got %v", err)
	}
}

func TestAddSuffix(t *testing.T) {
	s, err := New([]string{"mesh"}, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	got, err := s.AddSuffix("Alt")
	if err != nil {
		t.Fatalf("AddSuffix: %v", err)
	}
	if got != "alt" {
		t.Errorf("AddSuffix returned %q, want %q", got, "alt")
	}
	if !contains(s.Suffixes(), "alt") {
		t.Errorf("Suffixes %v missing alt", s.Suffixes())
	}

	if _, err := s.AddSuffix("alt"); !errors.Is(err, ErrExists) {
		t.Errorf("expected ErrExists on duplicate, got %v", err)
	}
	if _, err := s.AddSuffix("mesh"); !errors.Is(err, ErrExists) {
		t.Errorf("expected ErrExists when adding seeded suffix, got %v", err)
	}
	if _, err := s.AddSuffix(""); !errors.Is(err, ErrInvalid) {
		t.Errorf("expected ErrInvalid for empty, got %v", err)
	}
	if _, err := s.AddSuffix("bad..label"); !errors.Is(err, ErrInvalid) {
		t.Errorf("expected ErrInvalid for empty inner label, got %v", err)
	}
	if _, err := s.AddSuffix(".lan"); !errors.Is(err, ErrInvalid) {
		t.Errorf("expected ErrInvalid for leading-dot, got %v", err)
	}
	if _, err := s.AddSuffix("lan."); !errors.Is(err, ErrInvalid) {
		t.Errorf("expected ErrInvalid for trailing-dot, got %v", err)
	}

	// Newly-added suffix is honored by the validator.
	if _, err := s.Add(Host{Name: "host.alt", IPv4: "10.0.0.5"}); err != nil {
		t.Errorf("Add(host.alt): %v", err)
	}

	// Multi-level suffixes are accepted and used by the validator.
	if _, err := s.AddSuffix("muti-metroo.alt"); err != nil {
		t.Errorf("AddSuffix(muti-metroo.alt): %v", err)
	}
	if _, err := s.Add(Host{Name: "edge.muti-metroo.alt", IPv4: "10.0.0.6"}); err != nil {
		t.Errorf("Add(edge.muti-metroo.alt): %v", err)
	}
}

func TestRemoveSuffix(t *testing.T) {
	s, err := New([]string{"mesh", "lan"}, []Host{{Name: "gw.mesh", IPv4: "10.0.0.1"}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Remove a config-seeded suffix that has no hosts -- allowed.
	if err := s.RemoveSuffix("lan"); err != nil {
		t.Fatalf("RemoveSuffix(lan): %v", err)
	}
	if contains(s.Suffixes(), "lan") {
		t.Errorf("Suffixes %v still contains lan", s.Suffixes())
	}
	// ConfigSuffixes baseline is unchanged so the snapshot diff still
	// emits a suffix_remove for lan.
	cfg := s.ConfigSuffixes()
	sort.Strings(cfg)
	if len(cfg) != 2 || cfg[0] != "lan" || cfg[1] != "mesh" {
		t.Errorf("ConfigSuffixes = %v, want [lan mesh]", cfg)
	}

	// Re-adding now lands as a non-seeded entry.
	if _, err := s.AddSuffix("lan"); err != nil {
		t.Fatalf("AddSuffix(lan): %v", err)
	}

	// Removing a suffix with a config-seeded host attached fails.
	err = s.RemoveSuffix("mesh")
	if !errors.Is(err, ErrSuffixInUse) {
		t.Errorf("expected ErrSuffixInUse, got %v", err)
	}
	if !strings.Contains(err.Error(), "gw.mesh") {
		t.Errorf("error %q should mention dependent host gw.mesh", err)
	}

	// Removing a non-existent suffix returns ErrNotFound.
	if err := s.RemoveSuffix("missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}

	// Add a dynamic host on lan, then attempt to drop lan -- also blocked.
	if _, err := s.Add(Host{Name: "host.lan", IPv4: "10.0.0.6"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := s.RemoveSuffix("lan"); !errors.Is(err, ErrSuffixInUse) {
		t.Errorf("expected ErrSuffixInUse for dynamic host, got %v", err)
	}
}

func contains(set []string, want string) bool {
	for _, v := range set {
		if v == want {
			return true
		}
	}
	return false
}

func TestSetOnChangeFiresOnMutations(t *testing.T) {
	s, err := New([]string{"mesh", "lan"}, []Host{{Name: "seed.mesh", IPv4: "10.0.0.1"}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	var count int
	s.SetOnChange(func() { count++ })

	// Successful Add fires once.
	if _, err := s.Add(Host{Name: "host.mesh", IPv4: "10.0.0.2"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if count != 1 {
		t.Fatalf("Add: want 1 callback, got %d", count)
	}

	// Failed Add (duplicate) does not fire.
	if _, err := s.Add(Host{Name: "host.mesh", IPv4: "10.0.0.3"}); !errors.Is(err, ErrExists) {
		t.Fatalf("Add duplicate: want ErrExists, got %v", err)
	}
	if count != 1 {
		t.Fatalf("failed Add: want 1 callback, got %d", count)
	}

	// Successful Remove fires.
	if _, err := s.Remove("host.mesh"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if count != 2 {
		t.Fatalf("Remove: want 2 callbacks, got %d", count)
	}

	// Failed Remove (protected config host) does not fire.
	if _, err := s.Remove("seed.mesh"); !errors.Is(err, ErrProtected) {
		t.Fatalf("Remove protected: want ErrProtected, got %v", err)
	}
	if count != 2 {
		t.Fatalf("failed Remove: want 2 callbacks, got %d", count)
	}

	// Successful AddSuffix fires.
	if _, err := s.AddSuffix("alt"); err != nil {
		t.Fatalf("AddSuffix: %v", err)
	}
	if count != 3 {
		t.Fatalf("AddSuffix: want 3 callbacks, got %d", count)
	}

	// Failed AddSuffix (duplicate) does not fire.
	if _, err := s.AddSuffix("alt"); !errors.Is(err, ErrExists) {
		t.Fatalf("AddSuffix duplicate: want ErrExists, got %v", err)
	}
	if count != 3 {
		t.Fatalf("failed AddSuffix: want 3 callbacks, got %d", count)
	}

	// Successful RemoveSuffix fires.
	if err := s.RemoveSuffix("alt"); err != nil {
		t.Fatalf("RemoveSuffix: %v", err)
	}
	if count != 4 {
		t.Fatalf("RemoveSuffix: want 4 callbacks, got %d", count)
	}

	// Failed RemoveSuffix (in use) does not fire.
	if err := s.RemoveSuffix("mesh"); !errors.Is(err, ErrSuffixInUse) {
		t.Fatalf("RemoveSuffix in-use: want ErrSuffixInUse, got %v", err)
	}
	if count != 4 {
		t.Fatalf("failed RemoveSuffix: want 4 callbacks, got %d", count)
	}
}
