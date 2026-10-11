package agent

import (
	"sort"
	"sync"
)

// dynRegistry tracks the config-vs-dynamic origin of a set of keyed runtime
// resources such as peers (keyed by dial address) and transport listeners
// (keyed by listen address), together with a value T describing each entry.
//
// Entries seeded from config.yaml (via SetConfig) are immutable at runtime: the
// dynamic management APIs refuse to replace or remove them. Entries added at
// runtime (via PutDynamic) may be replaced or removed freely. The two sets never
// share a key -- callers reject a dynamic add when the key is already config.
//
// The value T is the durable description of the entry, not its live object:
//   - peers store struct{} (membership only; live connections live in peer.Manager)
//   - listeners store config.ListenerConfig (the spec needed to (re)start the
//     listener; live *listenerHandle objects live in Agent.liveListeners and are
//     rebuilt across sleep/wake)
//
// All methods are safe for concurrent use. Callers needing check-then-act
// atomicity (reject-if-config, replace-if-dynamic) serialize those steps
// themselves; the registry only guarantees each individual call is consistent.
type dynRegistry[T any] struct {
	mu      sync.RWMutex
	config  map[string]T
	dynamic map[string]T
}

// newDynRegistry returns an empty registry ready for use.
func newDynRegistry[T any]() *dynRegistry[T] {
	return &dynRegistry[T]{
		config:  make(map[string]T),
		dynamic: make(map[string]T),
	}
}

// SetConfig records key as config-sourced (read-only at runtime) with value val.
func (r *dynRegistry[T]) SetConfig(key string, val T) {
	r.mu.Lock()
	r.config[key] = val
	r.mu.Unlock()
}

// IsConfig reports whether key was seeded from config.yaml.
func (r *dynRegistry[T]) IsConfig(key string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.config[key]
	return ok
}

// PutDynamic stores (or replaces) the value for a runtime-added key.
func (r *dynRegistry[T]) PutDynamic(key string, val T) {
	r.mu.Lock()
	r.dynamic[key] = val
	r.mu.Unlock()
}

// GetDynamic returns the stored value for a dynamic key.
func (r *dynRegistry[T]) GetDynamic(key string) (T, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	val, ok := r.dynamic[key]
	return val, ok
}

// IsDynamic reports whether key was added at runtime.
func (r *dynRegistry[T]) IsDynamic(key string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.dynamic[key]
	return ok
}

// DeleteDynamic removes a dynamic entry, returning its prior value (if any).
func (r *dynRegistry[T]) DeleteDynamic(key string) (T, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	val, ok := r.dynamic[key]
	if ok {
		delete(r.dynamic, key)
	}
	return val, ok
}

// All returns a merged copy of every entry (config + dynamic). The two sets
// never share a key, so no entry is overwritten.
func (r *dynRegistry[T]) All() map[string]T {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[string]T, len(r.config)+len(r.dynamic))
	for k, v := range r.config {
		out[k] = v
	}
	for k, v := range r.dynamic {
		out[k] = v
	}
	return out
}

// DynamicSnapshot returns a shallow copy of only the dynamic entries.
func (r *dynRegistry[T]) DynamicSnapshot() map[string]T {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[string]T, len(r.dynamic))
	for k, v := range r.dynamic {
		out[k] = v
	}
	return out
}

// DynamicKeys returns the dynamic keys in sorted order.
func (r *dynRegistry[T]) DynamicKeys() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	keys := make([]string, 0, len(r.dynamic))
	for k := range r.dynamic {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
