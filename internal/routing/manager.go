// Package routing implements CIDR-based routing for Muti Metroo mesh network.
package routing

import (
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/postalsys/muti-metroo/internal/crypto"
	"github.com/postalsys/muti-metroo/internal/identity"
	"github.com/postalsys/muti-metroo/internal/protocol"
)

// RouteChange represents a route update.
type RouteChange struct {
	Type    RouteChangeType
	Route   *Route
	OldPath []identity.AgentID // For updates
}

// RouteChangeType indicates the type of route change.
type RouteChangeType int

const (
	RouteAdded RouteChangeType = iota
	RouteRemoved
	RouteUpdated
)

// LocalRoute represents a locally-announced route.
type LocalRoute struct {
	Network *net.IPNet
	Metric  uint16
	Comment string

	// configComment is the comment baseline at construction time.
	// SetRouteComment leaves it untouched, so a runtime override on
	// a config-seeded route is detectable as Comment != configComment.
	configComment string
}

// NodeInfoEntry stores node info with metadata.
type NodeInfoEntry struct {
	Encrypted  bool                    // True if EncInfo contains encrypted data
	EncInfo    *protocol.EncryptedData // Raw encrypted/plaintext data for forwarding
	Info       *protocol.NodeInfo      // Decrypted NodeInfo (nil if encrypted and can't decrypt)
	Sequence   uint64
	LastUpdate time.Time
}

// Manager handles route management including local routes and propagation.
type Manager struct {
	mu sync.RWMutex

	localID       identity.AgentID
	table         *Table
	domainTable   *DomainTable  // Domain-based routing table
	forwardTable  *ForwardTable // Port forward routing table
	agentTable    *AgentTable   // Agent presence routing table
	localRoutes   map[string]*LocalRoute
	dynamicRoutes map[string]*LocalRoute              // Routes added via API (subset of localRoutes)
	localDomains  map[string]*LocalDomainRoute        // Local domain routes
	localForwards map[string]*LocalForwardRoute       // Local port forward routes
	displayNames  map[identity.AgentID]string         // Agent ID -> Display Name mapping
	nodeInfos     map[identity.AgentID]*NodeInfoEntry // Agent ID -> Node Info mapping
	sequence      uint64
	sealedBox     *crypto.SealedBox // For decrypting NodeInfo (nil if not configured)

	// Subscribers for route changes
	subscribers []chan<- RouteChange
	subMu       sync.RWMutex
}

// NewManager creates a new route manager.
func NewManager(localID identity.AgentID) *Manager {
	return &Manager{
		localID:       localID,
		table:         NewTable(localID),
		domainTable:   NewDomainTable(localID),
		forwardTable:  NewForwardTable(localID),
		agentTable:    NewAgentTable(localID),
		localRoutes:   make(map[string]*LocalRoute),
		dynamicRoutes: make(map[string]*LocalRoute),
		localDomains:  make(map[string]*LocalDomainRoute),
		localForwards: make(map[string]*LocalForwardRoute),
		displayNames:  make(map[identity.AgentID]string),
		nodeInfos:     make(map[identity.AgentID]*NodeInfoEntry),
		sequence:      uint64(time.Now().UnixMilli()),
	}
}

// SetSealedBox sets the SealedBox for decrypting NodeInfo.
// This should be called during initialization before any NodeInfo is received.
func (m *Manager) SetSealedBox(sealedBox *crypto.SealedBox) {
	m.mu.Lock()
	m.sealedBox = sealedBox
	m.mu.Unlock()
}

// SetDisplayName stores a display name for an agent.
func (m *Manager) SetDisplayName(agentID identity.AgentID, displayName string) {
	if displayName == "" {
		return
	}
	m.mu.Lock()
	m.displayNames[agentID] = displayName
	m.mu.Unlock()
}

// GetDisplayName returns the display name for an agent, or empty string if not known.
func (m *Manager) GetDisplayName(agentID identity.AgentID) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.displayNames[agentID]
}

// GetAllDisplayNames returns a copy of all known agent display names.
func (m *Manager) GetAllDisplayNames() map[identity.AgentID]string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make(map[identity.AgentID]string, len(m.displayNames))
	for k, v := range m.displayNames {
		result[k] = v
	}
	return result
}

// SetNodeInfo stores or updates node info for an agent (plaintext mode).
// Only updates if the sequence is newer than the existing entry.
// This is used for local node info that doesn't need encryption.
func (m *Manager) SetNodeInfo(agentID identity.AgentID, info *protocol.NodeInfo, sequence uint64) bool {
	if info == nil {
		return false
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if we already have a newer entry
	existing, exists := m.nodeInfos[agentID]
	if exists && existing.Sequence >= sequence {
		return false // Already have newer or equal info
	}

	// For local NodeInfo, store as plaintext EncryptedData for consistency
	encInfo := &protocol.EncryptedData{
		Encrypted: false,
		Data:      protocol.EncodeNodeInfo(info),
	}

	m.nodeInfos[agentID] = &NodeInfoEntry{
		Encrypted:  false,
		EncInfo:    encInfo,
		Info:       info,
		Sequence:   sequence,
		LastUpdate: time.Now(),
	}

	// Also update display name for consistency
	if info.DisplayName != "" {
		m.displayNames[agentID] = info.DisplayName
	}

	return true
}

// SetNodeInfoEncrypted stores or updates node info for an agent (encrypted mode).
// Only updates if the sequence is newer than the existing entry.
// Attempts to decrypt if SealedBox is available.
func (m *Manager) SetNodeInfoEncrypted(agentID identity.AgentID, encInfo *protocol.EncryptedData, sequence uint64) bool {
	if encInfo == nil {
		return false
	}

	// Never store node info about ourselves from external sources.
	// Our own node info is set via SetNodeInfo from AnnounceLocalNodeInfo.
	// Storing stale self-info from peers would overwrite our current state.
	if agentID == m.localID {
		return false
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if we already have a newer entry
	existing, exists := m.nodeInfos[agentID]
	if exists && existing.Sequence >= sequence {
		return false // Already have newer or equal info
	}

	entry := &NodeInfoEntry{
		Encrypted:  encInfo.Encrypted,
		EncInfo:    encInfo,
		Info:       nil, // Will be set if we can decrypt
		Sequence:   sequence,
		LastUpdate: time.Now(),
	}

	// Attempt to decrypt
	if encInfo.Encrypted {
		// Encrypted - try to decrypt if we have the private key
		if m.sealedBox != nil && m.sealedBox.CanDecrypt() {
			decrypted, err := m.sealedBox.Open(encInfo.Data)
			if err == nil {
				info, err := protocol.DecodeNodeInfo(decrypted)
				if err == nil {
					entry.Info = info
					// Update display name if we could decrypt
					if info.DisplayName != "" {
						m.displayNames[agentID] = info.DisplayName
					}
				}
			}
		}
	} else {
		// Plaintext - decode directly
		info, err := protocol.DecodeNodeInfo(encInfo.Data)
		if err == nil {
			entry.Info = info
			// Update display name
			if info.DisplayName != "" {
				m.displayNames[agentID] = info.DisplayName
			}
		}
	}

	m.nodeInfos[agentID] = entry
	return true
}

// GetNodeInfo returns node info for an agent.
func (m *Manager) GetNodeInfo(agentID identity.AgentID) *protocol.NodeInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if entry, ok := m.nodeInfos[agentID]; ok {
		return entry.Info
	}
	return nil
}

// GetAllNodeInfo returns a copy of all known decrypted node info.
// Only returns entries where Info is non-nil (decrypted successfully).
func (m *Manager) GetAllNodeInfo() map[identity.AgentID]*protocol.NodeInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make(map[identity.AgentID]*protocol.NodeInfo, len(m.nodeInfos))
	for k, v := range m.nodeInfos {
		if v.Info != nil {
			result[k] = v.Info
		}
	}
	return result
}

// GetAllNodeInfoEntries returns a copy of all known node info entries.
// This includes encrypted entries where Info may be nil.
func (m *Manager) GetAllNodeInfoEntries() map[identity.AgentID]*NodeInfoEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make(map[identity.AgentID]*NodeInfoEntry, len(m.nodeInfos))
	for k, v := range m.nodeInfos {
		result[k] = v
	}
	return result
}

// GetNodeInfoEntry returns the full node info entry with metadata.
func (m *Manager) GetNodeInfoEntry(agentID identity.AgentID) *NodeInfoEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.nodeInfos[agentID]
}

// GetNodeInfoSequence returns the current sequence for a node's info, or 0 if unknown.
func (m *Manager) GetNodeInfoSequence(agentID identity.AgentID) uint64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if entry, ok := m.nodeInfos[agentID]; ok {
		return entry.Sequence
	}
	return 0
}

// Table returns the underlying routing table.
func (m *Manager) Table() *Table {
	return m.table
}

// AddLocalRoute adds a locally-originated route. The comment is local
// metadata for operators (config-seeded or set via the management API);
// it is not advertised to peers.
func (m *Manager) AddLocalRoute(network *net.IPNet, metric uint16, comment string) bool {
	if network == nil {
		return false
	}

	key := network.String()

	m.mu.Lock()
	m.sequence++
	seq := m.sequence

	m.localRoutes[key] = &LocalRoute{
		Network:       network,
		Metric:        metric,
		Comment:       comment,
		configComment: comment,
	}
	m.mu.Unlock()

	// Add to table with ourselves as origin
	// Note: Path is empty for local routes to avoid loop detection on our own ID
	route := &Route{
		Network:     network,
		NextHop:     m.localID, // Local route
		OriginAgent: m.localID,
		Metric:      metric,
		Path:        nil, // Empty path for local routes
		Sequence:    seq,
		Comment:     comment,
	}

	added := m.table.AddRoute(route)
	if added {
		m.notifyChange(RouteChange{
			Type:  RouteAdded,
			Route: route.Clone(),
		})
	}

	return added
}

// RemoveLocalRoute removes a locally-originated route.
func (m *Manager) RemoveLocalRoute(network *net.IPNet) bool {
	if network == nil {
		return false
	}

	key := network.String()

	m.mu.Lock()
	_, exists := m.localRoutes[key]
	if !exists {
		m.mu.Unlock()
		return false
	}
	delete(m.localRoutes, key)
	m.mu.Unlock()

	removed := m.table.RemoveRoute(network, m.localID)
	if removed {
		m.notifyChange(RouteChange{
			Type: RouteRemoved,
			Route: &Route{
				Network:     network,
				OriginAgent: m.localID,
			},
		})
	}

	return removed
}

// GetLocalRoutes returns all locally-originated routes.
func (m *Manager) GetLocalRoutes() []*LocalRoute {
	m.mu.RLock()
	defer m.mu.RUnlock()

	routes := make([]*LocalRoute, 0, len(m.localRoutes))
	for _, r := range m.localRoutes {
		routes = append(routes, &LocalRoute{
			Network: r.Network,
			Metric:  r.Metric,
			Comment: r.Comment,
		})
	}
	return routes
}

// AddDynamicRoute adds a route via the API. Returns an error if the route
// already exists as a config route. If the route already exists as a dynamic
// route, it is updated with the new metric and comment.
func (m *Manager) AddDynamicRoute(network *net.IPNet, metric uint16, comment string) error {
	if network == nil {
		return fmt.Errorf("network is nil")
	}

	key := network.String()

	m.mu.Lock()
	// Check if this route exists as a config-only route
	if _, inLocal := m.localRoutes[key]; inLocal {
		if _, inDynamic := m.dynamicRoutes[key]; !inDynamic {
			m.mu.Unlock()
			return fmt.Errorf("route %s exists as a config route", key)
		}
	}

	m.sequence++
	seq := m.sequence
	lr := &LocalRoute{Network: network, Metric: metric, Comment: comment, configComment: comment}
	m.localRoutes[key] = lr
	m.dynamicRoutes[key] = lr
	m.mu.Unlock()

	route := &Route{
		Network:     network,
		NextHop:     m.localID,
		OriginAgent: m.localID,
		Metric:      metric,
		Path:        nil,
		Sequence:    seq,
		Comment:     comment,
	}

	added := m.table.AddRoute(route)
	if added {
		m.notifyChange(RouteChange{
			Type:  RouteAdded,
			Route: route.Clone(),
		})
	}

	return nil
}

// RemoveDynamicRoute removes a route added via the API. Returns an error if the
// route is a config route or does not exist.
func (m *Manager) RemoveDynamicRoute(network *net.IPNet) error {
	if network == nil {
		return fmt.Errorf("network is nil")
	}

	key := network.String()

	m.mu.Lock()
	if _, inDynamic := m.dynamicRoutes[key]; !inDynamic {
		_, isConfigRoute := m.localRoutes[key]
		m.mu.Unlock()
		if isConfigRoute {
			return fmt.Errorf("route %s is a config route and cannot be removed dynamically", key)
		}
		return fmt.Errorf("route %s not found", key)
	}
	delete(m.dynamicRoutes, key)
	delete(m.localRoutes, key)
	m.mu.Unlock()

	removed := m.table.RemoveRoute(network, m.localID)
	if removed {
		m.notifyChange(RouteChange{
			Type: RouteRemoved,
			Route: &Route{
				Network:     network,
				OriginAgent: m.localID,
			},
		})
	}

	return nil
}

// GetDynamicRoutes returns all routes added via the API.
func (m *Manager) GetDynamicRoutes() []*LocalRoute {
	m.mu.RLock()
	defer m.mu.RUnlock()

	routes := make([]*LocalRoute, 0, len(m.dynamicRoutes))
	for _, r := range m.dynamicRoutes {
		routes = append(routes, &LocalRoute{
			Network: r.Network,
			Metric:  r.Metric,
			Comment: r.Comment,
		})
	}
	return routes
}

// GetCommentOverrides returns network-string -> current-comment for
// config-seeded routes whose comment was overridden at runtime via
// SetRouteComment. Dynamic routes are excluded because their full state
// (including comment) is already captured by GetDynamicRoutes. Used by
// journal compaction to preserve operator overrides on YAML-defined routes.
func (m *Manager) GetCommentOverrides() map[string]string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make(map[string]string)
	for k, r := range m.localRoutes {
		if _, dyn := m.dynamicRoutes[k]; dyn {
			continue
		}
		if r.Comment != r.configComment {
			out[k] = r.Comment
		}
	}
	return out
}

// SetRouteComment updates the operator comment on a locally-originated route.
// Works on both config-seeded and dynamically-added routes; the underlying
// localRoutes map covers both. The table-level route is updated in place too
// so dashboards and the next periodic re-advertisement see the new value.
// Returns an error if no local route covers the given network.
func (m *Manager) SetRouteComment(network *net.IPNet, comment string) error {
	if network == nil {
		return fmt.Errorf("network is nil")
	}

	key := network.String()

	m.mu.Lock()
	lr, ok := m.localRoutes[key]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("route %s not found", key)
	}
	lr.Comment = comment
	m.mu.Unlock()

	m.table.SetLocalComment(network, m.localID, comment)
	return nil
}

// IsDynamicRoute returns true if the route was added via the API.
func (m *Manager) IsDynamicRoute(network *net.IPNet) bool {
	if network == nil {
		return false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.dynamicRoutes[network.String()]
	return ok
}

// HasDynamicRoutes returns true if any routes were added via the API.
func (m *Manager) HasDynamicRoutes() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.dynamicRoutes) > 0
}

// ProcessRouteAdvertise processes an incoming ROUTE_ADVERTISE.
// Returns the list of routes that were added/updated.
func (m *Manager) ProcessRouteAdvertise(
	fromPeer identity.AgentID,
	originAgent identity.AgentID,
	sequence uint64,
	routes []RouteEntry,
	path []identity.AgentID,
	encPath *protocol.EncryptedData,
) []*Route {
	var accepted []*Route

	// Path already contains the sender prepended by the flooder, use it directly
	// (the first element of path should be fromPeer, set by floodAdvertisement)
	for _, entry := range routes {
		route := &Route{
			Network:     entry.Network,
			NextHop:     fromPeer,
			OriginAgent: originAgent,
			Metric:      entry.Metric + 1, // Increment metric
			Path:        path,
			EncPath:     encPath,
			Sequence:    sequence,
			Comment:     entry.Comment,
		}

		if m.table.AddRoute(route) {
			accepted = append(accepted, route.Clone())
			m.notifyChange(RouteChange{
				Type:  RouteAdded,
				Route: route.Clone(),
			})
		}
	}

	return accepted
}

// ProcessRouteWithdraw processes an incoming ROUTE_WITHDRAW.
// Routes whose installed sequence is newer than the withdraw's sequence are
// kept: a withdraw traveling a slow path must not delete a fresher
// re-advertisement from the same origin.
// Returns true if any routes were removed.
func (m *Manager) ProcessRouteWithdraw(
	originAgent identity.AgentID,
	sequence uint64,
	routes []RouteEntry,
) bool {
	removed := false

	for _, entry := range routes {
		if m.table.RemoveRouteWithSeq(entry.Network, originAgent, sequence) {
			removed = true
			m.notifyChange(RouteChange{
				Type: RouteRemoved,
				Route: &Route{
					Network:     entry.Network,
					OriginAgent: originAgent,
				},
			})
		}
	}

	return removed
}

// HandlePeerDisconnect removes all routes learned from a disconnected peer.
// Returns the removed routes so the caller can propagate withdrawals.
func (m *Manager) HandlePeerDisconnect(peerID identity.AgentID) []*Route {
	return m.table.RemoveRoutesFromPeer(peerID)
}

// Lookup finds the best route for an IP address.
func (m *Manager) Lookup(ip net.IP) *Route {
	return m.table.Lookup(ip)
}

// LookupNextHop returns just the next-hop peer ID for an IP.
func (m *Manager) LookupNextHop(ip net.IP) (identity.AgentID, bool) {
	route := m.table.Lookup(ip)
	if route == nil {
		return identity.AgentID{}, false
	}
	return route.NextHop, true
}

// Subscribe registers a channel to receive route changes.
func (m *Manager) Subscribe(ch chan<- RouteChange) {
	m.subMu.Lock()
	defer m.subMu.Unlock()
	m.subscribers = append(m.subscribers, ch)
}

// Unsubscribe removes a channel from route change notifications.
func (m *Manager) Unsubscribe(ch chan<- RouteChange) {
	m.subMu.Lock()
	defer m.subMu.Unlock()

	for i, sub := range m.subscribers {
		if sub == ch {
			m.subscribers = append(m.subscribers[:i], m.subscribers[i+1:]...)
			return
		}
	}
}

// notifyChange sends a route change to all subscribers.
func (m *Manager) notifyChange(change RouteChange) {
	m.subMu.RLock()
	defer m.subMu.RUnlock()

	for _, ch := range m.subscribers {
		select {
		case ch <- change:
		default:
			// Channel full, skip
		}
	}
}

// GetCurrentSequence returns the current sequence number.
func (m *Manager) GetCurrentSequence() uint64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sequence
}

// IncrementSequence increments and returns the new sequence number.
func (m *Manager) IncrementSequence() uint64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sequence++
	return m.sequence
}

// RouteEntry is a simplified route for advertisements.
type RouteEntry struct {
	Network *net.IPNet
	Metric  uint16
	Comment string // Operator metadata propagated alongside the route.
}

// GetRoutesToAdvertise returns routes that should be advertised to peers.
// This includes local routes and routes learned from other peers.
func (m *Manager) GetRoutesToAdvertise(excludePeer identity.AgentID) []RouteEntry {
	allRoutes := m.table.GetAllRoutes()
	var entries []RouteEntry

	seen := make(map[string]bool)
	for _, route := range allRoutes {
		// Don't advertise routes learned from the peer we're advertising to
		if route.NextHop == excludePeer {
			continue
		}

		key := route.Network.String()
		if seen[key] {
			continue
		}
		seen[key] = true

		entries = append(entries, RouteEntry{
			Network: route.Network,
			Metric:  route.Metric,
			Comment: route.Comment,
		})
	}

	return entries
}

// GetFullRoutesForAdvertise returns full route information for advertising to a peer.
// Routes are filtered to exclude those learned from the peer we're advertising to.
func (m *Manager) GetFullRoutesForAdvertise(excludePeer identity.AgentID) []*Route {
	allRoutes := m.table.GetAllRoutes()
	var result []*Route

	seen := make(map[string]bool)
	for _, route := range allRoutes {
		// Don't advertise routes learned from the peer we're advertising to
		if route.NextHop == excludePeer {
			continue
		}

		// Use a key of network+origin to allow multiple origins for same network
		key := route.Network.String() + ":" + route.OriginAgent.String()
		if seen[key] {
			continue
		}
		seen[key] = true

		result = append(result, route)
	}

	return result
}

// Size returns the number of unique prefixes in the routing table.
func (m *Manager) Size() int {
	return m.table.Size()
}

// TotalRoutes returns the total number of route entries.
func (m *Manager) TotalRoutes() int {
	return m.table.TotalRoutes()
}

// CleanupStaleNodeInfo removes node info entries that haven't been updated
// within the specified maxAge duration. Returns the number of entries removed.
func (m *Manager) CleanupStaleNodeInfo(maxAge time.Duration) int {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	removed := 0

	for agentID, entry := range m.nodeInfos {
		// Don't remove our own node info
		if agentID == m.localID {
			continue
		}

		if now.Sub(entry.LastUpdate) > maxAge {
			delete(m.nodeInfos, agentID)
			delete(m.displayNames, agentID)
			removed++
		}
	}

	return removed
}

// CleanupStaleRoutes removes routes that haven't been updated within maxAge.
// Local routes are never removed. Returns the number of routes removed.
func (m *Manager) CleanupStaleRoutes(maxAge time.Duration) int {
	return m.table.CleanupStaleRoutes(maxAge)
}

// DomainTable returns the underlying domain routing table.
func (m *Manager) DomainTable() *DomainTable {
	return m.domainTable
}

// AddLocalDomainRoute adds a locally-originated domain route.
func (m *Manager) AddLocalDomainRoute(pattern string, metric uint16) bool {
	if pattern == "" {
		return false
	}

	// Validate and parse the pattern
	if err := ValidateDomainPattern(pattern); err != nil {
		return false
	}

	isWildcard, baseDomain := ParseDomainPattern(pattern)
	key := pattern

	m.mu.Lock()
	m.sequence++
	seq := m.sequence

	m.localDomains[key] = &LocalDomainRoute{
		Pattern:    pattern,
		IsWildcard: isWildcard,
		BaseDomain: baseDomain,
		Metric:     metric,
	}
	m.mu.Unlock()

	// Add to domain table with ourselves as origin
	route := &DomainRoute{
		Pattern:     pattern,
		IsWildcard:  isWildcard,
		BaseDomain:  baseDomain,
		NextHop:     m.localID, // Local route
		OriginAgent: m.localID,
		Metric:      metric,
		Path:        nil, // Empty path for local routes
		Sequence:    seq,
	}

	return m.domainTable.AddRoute(route)
}

// RemoveLocalDomainRoute removes a locally-originated domain route.
func (m *Manager) RemoveLocalDomainRoute(pattern string) bool {
	if pattern == "" {
		return false
	}

	key := pattern

	m.mu.Lock()
	_, exists := m.localDomains[key]
	if !exists {
		m.mu.Unlock()
		return false
	}
	delete(m.localDomains, key)
	m.mu.Unlock()

	return m.domainTable.RemoveRoute(pattern, m.localID)
}

// GetLocalDomainRoutes returns all locally-originated domain routes.
func (m *Manager) GetLocalDomainRoutes() []*LocalDomainRoute {
	m.mu.RLock()
	defer m.mu.RUnlock()

	routes := make([]*LocalDomainRoute, 0, len(m.localDomains))
	for _, r := range m.localDomains {
		routes = append(routes, &LocalDomainRoute{
			Pattern:    r.Pattern,
			IsWildcard: r.IsWildcard,
			BaseDomain: r.BaseDomain,
			Metric:     r.Metric,
		})
	}
	return routes
}

// LookupDomain finds the best domain route for a domain name.
func (m *Manager) LookupDomain(domain string) *DomainRoute {
	return m.domainTable.Lookup(domain)
}

// DomainRouteEntry is a simplified domain route for advertisements.
type DomainRouteEntry struct {
	Pattern    string
	IsWildcard bool
	Metric     uint16
}

// ProcessDomainRouteAdvertise processes incoming domain route advertisements.
// Returns the list of routes that were added/updated.
func (m *Manager) ProcessDomainRouteAdvertise(
	fromPeer identity.AgentID,
	originAgent identity.AgentID,
	sequence uint64,
	routes []DomainRouteEntry,
	path []identity.AgentID,
	encPath *protocol.EncryptedData,
) []*DomainRoute {
	var accepted []*DomainRoute

	for _, entry := range routes {
		isWildcard, baseDomain := ParseDomainPattern(entry.Pattern)
		route := &DomainRoute{
			Pattern:     entry.Pattern,
			IsWildcard:  isWildcard,
			BaseDomain:  baseDomain,
			NextHop:     fromPeer,
			OriginAgent: originAgent,
			Metric:      entry.Metric + 1, // Increment metric
			Path:        path,
			EncPath:     encPath,
			Sequence:    sequence,
		}

		if m.domainTable.AddRoute(route) {
			accepted = append(accepted, route.Clone())
		}
	}

	return accepted
}

// ProcessDomainRouteWithdraw removes domain routes from the given origin.
// Routes whose installed sequence is newer than the withdraw's sequence are
// kept. Returns true if any routes were removed.
func (m *Manager) ProcessDomainRouteWithdraw(
	originAgent identity.AgentID,
	sequence uint64,
	entries []DomainRouteEntry,
) bool {
	removed := false
	for _, e := range entries {
		if m.domainTable.RemoveRouteWithSeq(e.Pattern, originAgent, sequence) {
			removed = true
		}
	}
	return removed
}

// HandlePeerDisconnectDomain removes all domain routes learned from a disconnected peer.
// Returns the removed routes so the caller can propagate withdrawals.
func (m *Manager) HandlePeerDisconnectDomain(peerID identity.AgentID) []*DomainRoute {
	return m.domainTable.RemoveRoutesFromPeer(peerID)
}

// CleanupStaleDomainRoutes removes domain routes that haven't been updated within maxAge.
// Local routes are never removed. Returns the number of routes removed.
func (m *Manager) CleanupStaleDomainRoutes(maxAge time.Duration) int {
	return m.domainTable.CleanupStaleRoutes(maxAge)
}

// DomainSize returns the number of unique domain patterns in the routing table.
func (m *Manager) DomainSize() int {
	return m.domainTable.Size()
}

// TotalDomainRoutes returns the total number of domain route entries.
func (m *Manager) TotalDomainRoutes() int {
	return m.domainTable.TotalRoutes()
}

// ForwardTable returns the underlying port forward routing table.
func (m *Manager) ForwardTable() *ForwardTable {
	return m.forwardTable
}

// AddLocalForwardRoute adds a locally-originated port forward route.
func (m *Manager) AddLocalForwardRoute(key, target string, metric uint16) bool {
	if key == "" || target == "" {
		return false
	}

	m.mu.Lock()
	m.sequence++
	seq := m.sequence

	m.localForwards[key] = &LocalForwardRoute{
		Key:    key,
		Target: target,
		Metric: metric,
	}
	m.mu.Unlock()

	// Add to forward table with ourselves as origin
	route := &ForwardRoute{
		Key:         key,
		Target:      target,
		NextHop:     m.localID, // Local route
		OriginAgent: m.localID,
		Metric:      metric,
		Path:        nil, // Empty path for local routes
		Sequence:    seq,
	}

	return m.forwardTable.AddRoute(route)
}

// RemoveLocalForwardRoute removes a locally-originated port forward route.
func (m *Manager) RemoveLocalForwardRoute(key string) bool {
	if key == "" {
		return false
	}

	m.mu.Lock()
	_, exists := m.localForwards[key]
	if !exists {
		m.mu.Unlock()
		return false
	}
	delete(m.localForwards, key)
	m.mu.Unlock()

	return m.forwardTable.RemoveRoute(key, m.localID)
}

// GetLocalForwardRoutes returns all locally-originated port forward routes.
func (m *Manager) GetLocalForwardRoutes() []*LocalForwardRoute {
	m.mu.RLock()
	defer m.mu.RUnlock()

	routes := make([]*LocalForwardRoute, 0, len(m.localForwards))
	for _, r := range m.localForwards {
		routes = append(routes, &LocalForwardRoute{
			Key:    r.Key,
			Target: r.Target,
			Metric: r.Metric,
		})
	}
	return routes
}

// LookupForward finds the best port forward route for a routing key.
func (m *Manager) LookupForward(key string) *ForwardRoute {
	return m.forwardTable.Lookup(key)
}

// ForwardRouteEntry is a simplified port forward route for advertisements.
type ForwardRouteEntry struct {
	Key    string
	Target string // Target address (host:port)
	Metric uint16
}

// ProcessForwardRouteAdvertise processes incoming port forward route advertisements.
// Returns the list of routes that were added/updated.
func (m *Manager) ProcessForwardRouteAdvertise(
	fromPeer identity.AgentID,
	originAgent identity.AgentID,
	sequence uint64,
	routes []ForwardRouteEntry,
	path []identity.AgentID,
	encPath *protocol.EncryptedData,
) []*ForwardRoute {
	var accepted []*ForwardRoute

	for _, entry := range routes {
		route := &ForwardRoute{
			Key:         entry.Key,
			Target:      entry.Target,
			NextHop:     fromPeer,
			OriginAgent: originAgent,
			Metric:      entry.Metric + 1, // Increment metric
			Path:        path,
			EncPath:     encPath,
			Sequence:    sequence,
		}

		if m.forwardTable.AddRoute(route) {
			accepted = append(accepted, route.Clone())
		}
	}

	return accepted
}

// ProcessForwardRouteWithdraw removes port forward routes from the given origin.
// Routes whose installed sequence is newer than the withdraw's sequence are
// kept. Returns true if any routes were removed.
func (m *Manager) ProcessForwardRouteWithdraw(
	originAgent identity.AgentID,
	sequence uint64,
	entries []ForwardRouteEntry,
) bool {
	removed := false
	for _, e := range entries {
		if m.forwardTable.RemoveRouteWithSeq(e.Key, originAgent, sequence) {
			removed = true
		}
	}
	return removed
}

// HandlePeerDisconnectForward removes all port forward routes learned from a disconnected peer.
// Returns the removed routes so the caller can propagate withdrawals.
func (m *Manager) HandlePeerDisconnectForward(peerID identity.AgentID) []*ForwardRoute {
	return m.forwardTable.RemoveRoutesFromPeer(peerID)
}

// CleanupStaleForwardRoutes removes port forward routes that haven't been updated within maxAge.
// Local routes are never removed. Returns the number of routes removed.
func (m *Manager) CleanupStaleForwardRoutes(maxAge time.Duration) int {
	return m.forwardTable.CleanupStaleRoutes(maxAge)
}

// ForwardSize returns the number of unique port forward keys in the routing table.
func (m *Manager) ForwardSize() int {
	return m.forwardTable.Size()
}

// TotalForwardRoutes returns the total number of port forward route entries.
func (m *Manager) TotalForwardRoutes() int {
	return m.forwardTable.TotalRoutes()
}

// AgentTable returns the underlying agent presence routing table.
func (m *Manager) AgentTable() *AgentTable {
	return m.agentTable
}

// LookupAgent finds the best agent presence route for a target agent.
func (m *Manager) LookupAgent(agentID identity.AgentID) *AgentRoute {
	return m.agentTable.Lookup(agentID)
}

// ProcessAgentRouteAdvertise processes an incoming agent presence route advertisement.
// Returns true if the route was added/updated.
func (m *Manager) ProcessAgentRouteAdvertise(
	fromPeer identity.AgentID,
	originAgent identity.AgentID,
	sequence uint64,
	agentID identity.AgentID,
	path []identity.AgentID,
	encPath *protocol.EncryptedData,
	metric uint16,
) bool {
	route := &AgentRoute{
		AgentID:     agentID,
		NextHop:     fromPeer,
		OriginAgent: originAgent,
		Metric:      metric,
		Path:        path,
		EncPath:     encPath,
		Sequence:    sequence,
	}
	return m.agentTable.AddRoute(route)
}

// ProcessAgentRouteWithdraw processes an agent presence route withdrawal.
// The route is kept when its installed sequence is newer than the withdraw's
// sequence. Returns true if a route was removed.
func (m *Manager) ProcessAgentRouteWithdraw(
	originAgent identity.AgentID,
	sequence uint64,
	agentID identity.AgentID,
) bool {
	return m.agentTable.RemoveRouteWithSeq(agentID, originAgent, sequence)
}

// HandlePeerDisconnectAgent removes all agent routes learned from a disconnected peer.
// Returns the removed routes so the caller can propagate withdrawals.
func (m *Manager) HandlePeerDisconnectAgent(peerID identity.AgentID) []*AgentRoute {
	return m.agentTable.RemoveRoutesFromPeer(peerID)
}

// CleanupStaleAgentRoutes removes agent routes that haven't been updated within maxAge.
// Local routes are never removed. Returns the number of routes removed.
func (m *Manager) CleanupStaleAgentRoutes(maxAge time.Duration) int {
	return m.agentTable.CleanupStaleRoutes(maxAge)
}

// ForwardListenerAgent represents an agent with a forward listener.
type ForwardListenerAgent struct {
	AgentID identity.AgentID
	Address string // Listen address
}

// GetForwardListenerAgents returns all agents that have a listener for the given key.
// This uses NodeInfo data propagated through the mesh.
func (m *Manager) GetForwardListenerAgents(key string) []ForwardListenerAgent {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []ForwardListenerAgent
	for agentID, entry := range m.nodeInfos {
		if entry.Info == nil {
			continue
		}
		for _, listener := range entry.Info.ForwardListeners {
			if listener.Key == key {
				result = append(result, ForwardListenerAgent{
					AgentID: agentID,
					Address: listener.Address,
				})
				break // Only one listener per key per agent
			}
		}
	}
	return result
}

// GetAllForwardListeners returns all known forward listeners from all agents.
// Returns a map of key -> list of agents with that listener.
func (m *Manager) GetAllForwardListeners() map[string][]ForwardListenerAgent {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string][]ForwardListenerAgent)
	for agentID, entry := range m.nodeInfos {
		if entry.Info == nil {
			continue
		}
		for _, listener := range entry.Info.ForwardListeners {
			result[listener.Key] = append(result[listener.Key], ForwardListenerAgent{
				AgentID: agentID,
				Address: listener.Address,
			})
		}
	}
	return result
}

// GetHopsBetweenAgents computes the shortest path (hop count) between two agents.
// Uses BFS on the peer connection graph from NodeInfo.
// Returns -1 if no path exists or either agent is unknown.
func (m *Manager) GetHopsBetweenAgents(from, to identity.AgentID) int {
	if from == to {
		return 0
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	// Build adjacency list from peer connections
	adj := make(map[identity.AgentID][]identity.AgentID)
	for agentID, entry := range m.nodeInfos {
		if entry.Info == nil {
			continue
		}
		for _, peer := range entry.Info.Peers {
			peerID := identity.AgentID(peer.PeerID)
			adj[agentID] = append(adj[agentID], peerID)
			// Add reverse edge (connections are bidirectional)
			adj[peerID] = append(adj[peerID], agentID)
		}
	}

	// Also add local agent's connections
	if entry, ok := m.nodeInfos[m.localID]; ok && entry.Info != nil {
		for _, peer := range entry.Info.Peers {
			peerID := identity.AgentID(peer.PeerID)
			adj[m.localID] = append(adj[m.localID], peerID)
			adj[peerID] = append(adj[peerID], m.localID)
		}
	}

	// BFS from 'from' to 'to'
	visited := make(map[identity.AgentID]bool)
	queue := []struct {
		id   identity.AgentID
		hops int
	}{{from, 0}}
	visited[from] = true

	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]

		for _, neighbor := range adj[curr.id] {
			if neighbor == to {
				return curr.hops + 1
			}
			if !visited[neighbor] {
				visited[neighbor] = true
				queue = append(queue, struct {
					id   identity.AgentID
					hops int
				}{neighbor, curr.hops + 1})
			}
		}
	}

	return -1 // No path found
}
