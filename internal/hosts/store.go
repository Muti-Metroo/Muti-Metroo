// Package hosts provides a registry of mesh-local hostname -> IP mappings
// (e.g. "target-11.mesh -> 10.8.0.11"). The registry is served to local
// Mutiauk clients via the agent's /api/dashboard JSON; it is not
// advertised to the rest of the mesh.
//
// The store keeps two parallel sets of hosts: config-seeded entries from
// YAML (immutable via the runtime API) and dynamic entries added via
// /hosts/manage (journalled and replayed across restarts). It also keeps
// two parallel sets of TLD suffixes: a config baseline and the currently
// active set, mutated at runtime by AddSuffix / RemoveSuffix and likewise
// journalled.
package hosts

import (
	"errors"
	"fmt"
	"net"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// Host is a single entry. At least one of IPv4 / IPv6 must be set.
type Host struct {
	Name    string `json:"name" yaml:"name"`
	IPv4    string `json:"ipv4,omitempty" yaml:"ipv4,omitempty"`
	IPv6    string `json:"ipv6,omitempty" yaml:"ipv6,omitempty"`
	Comment string `json:"comment,omitempty" yaml:"comment,omitempty"`
}

// DefaultSuffix is the suffix used when the config does not list any.
const DefaultSuffix = "mesh"

// MaxNameLength is the RFC 1035 cap on total domain name length.
const MaxNameLength = 253

// Sentinel errors so HTTP handlers can map to status codes.
var (
	ErrInvalid     = errors.New("invalid host entry")
	ErrExists      = errors.New("host already exists")
	ErrNotFound    = errors.New("host not found")
	ErrProtected   = errors.New("config-seeded host, cannot remove dynamically")
	ErrSuffixInUse = errors.New("suffix has registered hosts")
)

// labelRe matches a single RFC 1035 label after lowercasing: letters,
// digits, internal hyphens, no leading/trailing hyphen, 1-63 chars.
var labelRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// Store holds config-seeded and dynamic hosts behind an RWMutex.
type Store struct {
	mu             sync.RWMutex
	configSuffixes map[string]struct{} // immutable baseline; used to compute diffs at compaction
	activeSuffixes map[string]struct{} // current effective set; mutated by AddSuffix/RemoveSuffix
	config         map[string]Host     // immutable, seeded once
	dynamic        map[string]Host     // mutable via Add/Remove

	onChangeMu sync.RWMutex
	onChange   func() // fired after a successful runtime mutation; may be nil
}

// SetOnChange registers a callback invoked (outside the store lock) after any
// successful runtime mutation: Add, Remove, AddSuffix, or RemoveSuffix. It
// lets the route-watch provider re-snapshot when mesh-host or suffix entries
// change, since those edits do not flow through the routing change bus. Pass
// nil to clear. Safe for concurrent use.
func (s *Store) SetOnChange(fn func()) {
	s.onChangeMu.Lock()
	s.onChange = fn
	s.onChangeMu.Unlock()
}

// notify fires the registered change callback, if any. Callers must invoke it
// after releasing the store mutex so the callback cannot re-enter and
// deadlock.
func (s *Store) notify() {
	s.onChangeMu.RLock()
	fn := s.onChange
	s.onChangeMu.RUnlock()
	if fn != nil {
		fn()
	}
}

// New creates a Store with the given suffixes and seed entries. The seed
// is validated and lowercased; any invalid entry returns ErrInvalid
// wrapped with details. An empty or nil suffixes slice falls back to
// [DefaultSuffix]. Suffixes may be a single DNS label (mesh) or a
// multi-level domain (muti-metroo.alt, lab.test.mesh) -- each dot-separated
// piece must be a valid RFC 1035 label and there must be no leading or
// trailing dot.
func New(suffixes []string, seed []Host) (*Store, error) {
	normalized := make(map[string]struct{}, len(suffixes))
	for i, raw := range suffixes {
		s, err := normalizeSuffix(raw)
		if err != nil {
			return nil, fmt.Errorf("%w: mesh_hosts_suffixes[%d]: %s", ErrInvalid, i, err.Error())
		}
		normalized[s] = struct{}{}
	}
	if len(normalized) == 0 {
		normalized[DefaultSuffix] = struct{}{}
	}

	configSuffixes := make(map[string]struct{}, len(normalized))
	activeSuffixes := make(map[string]struct{}, len(normalized))
	for s := range normalized {
		configSuffixes[s] = struct{}{}
		activeSuffixes[s] = struct{}{}
	}

	st := &Store{
		configSuffixes: configSuffixes,
		activeSuffixes: activeSuffixes,
		config:         make(map[string]Host, len(seed)),
		dynamic:        make(map[string]Host),
	}

	for i, h := range seed {
		entry, err := st.validate(h)
		if err != nil {
			return nil, fmt.Errorf("mesh_hosts[%d]: %w", i, err)
		}
		if _, dup := st.config[entry.Name]; dup {
			return nil, fmt.Errorf("mesh_hosts[%d]: %w: %s", i, ErrExists, entry.Name)
		}
		st.config[entry.Name] = entry
	}

	return st, nil
}

// Suffixes returns the currently active suffix set, sorted.
func (s *Store) Suffixes() []string {
	s.mu.RLock()
	out := make([]string, 0, len(s.activeSuffixes))
	for k := range s.activeSuffixes {
		out = append(out, k)
	}
	s.mu.RUnlock()
	sort.Strings(out)
	return out
}

// ConfigSuffixes returns the immutable set seeded from config, sorted.
// Used by the dynconfig snapshot to compute the symmetric difference
// against the active set so journal compaction stays minimal.
func (s *Store) ConfigSuffixes() []string {
	out := make([]string, 0, len(s.configSuffixes))
	for k := range s.configSuffixes {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// AddSuffix activates a new TLD. Returns the canonical (lowercased,
// trimmed) form. ErrInvalid for malformed labels, ErrExists if the
// suffix is already active. Multi-level suffixes (muti-metroo.alt) are
// accepted; every dot-separated label must satisfy RFC 1035.
func (s *Store) AddSuffix(suffix string) (string, error) {
	v, err := normalizeSuffix(suffix)
	if err != nil {
		return "", fmt.Errorf("%w: %s", ErrInvalid, err.Error())
	}

	s.mu.Lock()
	if _, ok := s.activeSuffixes[v]; ok {
		s.mu.Unlock()
		return "", fmt.Errorf("%w: %s", ErrExists, v)
	}
	s.activeSuffixes[v] = struct{}{}
	s.mu.Unlock()

	s.notify()
	return v, nil
}

// RemoveSuffix deactivates a TLD. ErrNotFound when the suffix is not
// currently active. ErrSuffixInUse (with the dependent hostnames as
// part of the wrapped message) when any registered host -- config or
// dynamic -- still ends with .<suffix>; the operator must remove those
// hosts first.
func (s *Store) RemoveSuffix(suffix string) error {
	v := strings.ToLower(strings.TrimSpace(suffix))
	if v == "" {
		return fmt.Errorf("%w: suffix is required", ErrInvalid)
	}

	s.mu.Lock()

	if _, ok := s.activeSuffixes[v]; !ok {
		s.mu.Unlock()
		return fmt.Errorf("%w: %s", ErrNotFound, v)
	}

	tail := "." + v
	var dependents []string
	for name := range s.config {
		if name == v || strings.HasSuffix(name, tail) {
			dependents = append(dependents, name)
		}
	}
	for name := range s.dynamic {
		if name == v || strings.HasSuffix(name, tail) {
			dependents = append(dependents, name)
		}
	}
	if len(dependents) > 0 {
		s.mu.Unlock()
		sort.Strings(dependents)
		return fmt.Errorf("%w: %s used by %s", ErrSuffixInUse, v, strings.Join(dependents, ", "))
	}

	delete(s.activeSuffixes, v)
	s.mu.Unlock()

	s.notify()
	return nil
}

// Add registers a runtime host. Returns ErrExists if the name is already
// in either the config or dynamic set, ErrInvalid for validation
// failures.
func (s *Store) Add(h Host) (Host, error) {
	s.mu.Lock()

	normalized, err := s.validateLocked(h)
	if err != nil {
		s.mu.Unlock()
		return Host{}, err
	}

	if _, ok := s.config[normalized.Name]; ok {
		s.mu.Unlock()
		return Host{}, fmt.Errorf("%w: %s", ErrExists, normalized.Name)
	}
	if _, ok := s.dynamic[normalized.Name]; ok {
		s.mu.Unlock()
		return Host{}, fmt.Errorf("%w: %s", ErrExists, normalized.Name)
	}

	s.dynamic[normalized.Name] = normalized
	s.mu.Unlock()

	s.notify()
	return normalized, nil
}

// Remove deletes a runtime host. Returns ErrProtected when the name
// matches a config-seeded entry, ErrNotFound otherwise.
func (s *Store) Remove(name string) (Host, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return Host{}, fmt.Errorf("%w: name is required", ErrInvalid)
	}

	s.mu.Lock()

	if h, ok := s.dynamic[name]; ok {
		delete(s.dynamic, name)
		s.mu.Unlock()
		s.notify()
		return h, nil
	}
	if _, ok := s.config[name]; ok {
		s.mu.Unlock()
		return Host{}, fmt.Errorf("%w: %s", ErrProtected, name)
	}
	s.mu.Unlock()
	return Host{}, fmt.Errorf("%w: %s", ErrNotFound, name)
}

// Lookup returns the entry whose name (case-insensitive, surrounding
// whitespace stripped) matches name. Dynamic entries shadow config
// entries with the same name, matching the precedence used by
// Add/Remove. Returns ok=false (with a zero-value Host) when no entry
// matches or name is empty.
func (s *Store) Lookup(name string) (Host, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return Host{}, false
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	if h, ok := s.dynamic[name]; ok {
		return h, true
	}
	if h, ok := s.config[name]; ok {
		return h, true
	}
	return Host{}, false
}

// List returns all entries (config + dynamic) sorted by name.
func (s *Store) List() []Host {
	s.mu.RLock()
	out := make([]Host, 0, len(s.config)+len(s.dynamic))
	for _, h := range s.config {
		out = append(out, h)
	}
	for _, h := range s.dynamic {
		out = append(out, h)
	}
	s.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Dynamic returns just the runtime-added entries sorted by name. Used by
// the dynconfig snapshot.
func (s *Store) Dynamic() []Host {
	s.mu.RLock()
	out := make([]Host, 0, len(s.dynamic))
	for _, h := range s.dynamic {
		out = append(out, h)
	}
	s.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// validate normalizes h against the currently active suffix set. Used
// for seed validation in New(); takes the read lock.
func (s *Store) validate(h Host) (Host, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.validateLocked(h)
}

// validateLocked is the shared validation body. The caller must hold
// either the read or write lock.
func (s *Store) validateLocked(h Host) (Host, error) {
	name := strings.ToLower(strings.TrimSpace(h.Name))
	if name == "" {
		return Host{}, fmt.Errorf("%w: name is required", ErrInvalid)
	}
	if len(name) > MaxNameLength {
		return Host{}, fmt.Errorf("%w: name %q exceeds %d chars", ErrInvalid, name, MaxNameLength)
	}

	matched := ""
	for suffix := range s.activeSuffixes {
		if name == suffix || strings.HasSuffix(name, "."+suffix) {
			if len(suffix) > len(matched) {
				matched = suffix
			}
		}
	}
	if matched == "" {
		return Host{}, fmt.Errorf("%w: name %q must equal or end with one of %s", ErrInvalid, name, formatSuffixList(s.activeSuffixes))
	}
	// A bare-suffix entry (name == matched, e.g. "mesh.alt") has no prefix
	// to validate -- the suffix has already been validated as a chain of
	// RFC 1035 labels in normalizeSuffix. Reject single-label bare entries
	// though: systemd-resolved does not forward single-label queries by
	// default, so the registration would not resolve end-to-end on a
	// typical host.
	if name == matched {
		if !strings.Contains(name, ".") {
			return Host{}, fmt.Errorf("%w: name %q is single-label; register a multi-label name (e.g. host.%s)", ErrInvalid, name, name)
		}
	} else {
		prefix := strings.TrimSuffix(name, "."+matched)
		for _, label := range strings.Split(prefix, ".") {
			if !labelRe.MatchString(label) {
				return Host{}, fmt.Errorf("%w: label %q in %q is not a valid DNS label", ErrInvalid, label, name)
			}
		}
	}

	ipv4 := strings.TrimSpace(h.IPv4)
	ipv6 := strings.TrimSpace(h.IPv6)
	if ipv4 == "" && ipv6 == "" {
		return Host{}, fmt.Errorf("%w: at least one of ipv4 or ipv6 is required", ErrInvalid)
	}
	if ipv4 != "" {
		ip := net.ParseIP(ipv4)
		if ip == nil || ip.To4() == nil {
			return Host{}, fmt.Errorf("%w: ipv4 %q is not a valid IPv4 address", ErrInvalid, ipv4)
		}
		ipv4 = ip.To4().String()
	}
	if ipv6 != "" {
		ip := net.ParseIP(ipv6)
		if ip == nil || ip.To4() != nil {
			return Host{}, fmt.Errorf("%w: ipv6 %q is not a valid IPv6 address", ErrInvalid, ipv6)
		}
		ipv6 = ip.String()
	}

	return Host{
		Name:    name,
		IPv4:    ipv4,
		IPv6:    ipv6,
		Comment: strings.TrimSpace(h.Comment),
	}, nil
}

// normalizeSuffix trims, lowercases, and validates a TLD suffix. Single-
// label suffixes like "mesh" and multi-level suffixes like "muti-metroo.alt"
// are both accepted; the suffix must not have a leading or trailing dot,
// and each dot-separated piece must be a valid RFC 1035 label. Returns
// the canonical form (lowercased, trimmed).
func normalizeSuffix(raw string) (string, error) {
	v := strings.ToLower(strings.TrimSpace(raw))
	if v == "" {
		return "", fmt.Errorf("suffix is required")
	}
	if strings.HasPrefix(v, ".") {
		return "", fmt.Errorf("suffix %q must not start with a dot", v)
	}
	if strings.HasSuffix(v, ".") {
		return "", fmt.Errorf("suffix %q must not end with a dot", v)
	}
	for _, label := range strings.Split(v, ".") {
		if !labelRe.MatchString(label) {
			return "", fmt.Errorf("suffix %q has invalid DNS label %q", v, label)
		}
	}
	return v, nil
}

func formatSuffixList(set map[string]struct{}) string {
	if len(set) == 0 {
		return "(none configured)"
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, "."+k)
	}
	sort.Strings(out)
	return strings.Join(out, ", ")
}
