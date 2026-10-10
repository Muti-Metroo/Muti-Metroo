// Package flood implements route flooding for Muti Metroo mesh network.
package flood

import (
	"fmt"
	"log/slog"
	"net"
	"sort"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/postalsys/muti-metroo/internal/crypto"
	"github.com/postalsys/muti-metroo/internal/identity"
	"github.com/postalsys/muti-metroo/internal/logging"
	"github.com/postalsys/muti-metroo/internal/protocol"
	"github.com/postalsys/muti-metroo/internal/recovery"
	"github.com/postalsys/muti-metroo/internal/routing"
)

// AdvertisementKey uniquely identifies a route advertisement for dedup.
type AdvertisementKey struct {
	OriginAgent identity.AgentID
	Sequence    uint64
}

// SeenAdvertisement tracks a seen advertisement for loop prevention.
type SeenAdvertisement struct {
	Key      AdvertisementKey
	SeenAt   time.Time
	SeenFrom identity.AgentID
}

// NodeInfoKey uniquely identifies a node info advertisement for dedup.
type NodeInfoKey struct {
	OriginAgent identity.AgentID
	Sequence    uint64
}

// SeenNodeInfo tracks a seen node info advertisement.
type SeenNodeInfo struct {
	Key      NodeInfoKey
	SeenAt   time.Time
	SeenFrom identity.AgentID
}

// SleepCommandKey uniquely identifies a sleep/wake command for dedup.
type SleepCommandKey struct {
	OriginAgent identity.AgentID
	CommandID   uint64
}

// SeenSleepCommand tracks a seen sleep/wake command.
type SeenSleepCommand struct {
	Key      SleepCommandKey
	SeenAt   time.Time
	SeenFrom identity.AgentID
}

// FloodConfig contains configuration for the flood protocol.
type FloodConfig struct {
	// SeenCacheTTL is how long to remember seen advertisements
	SeenCacheTTL time.Duration

	// FloodInterval is the minimum time between flooding the same route
	FloodInterval time.Duration

	// MaxSeenCacheSize limits the seen cache size
	MaxSeenCacheSize int

	// LocalDisplayName is the display name to include in route advertisements
	LocalDisplayName string

	// Logger for logging
	Logger *slog.Logger

	// SealedBox for management key encryption (nil if not configured)
	// When set, NodeInfo and route paths are encrypted before flooding
	SealedBox *crypto.SealedBox

	// SigningPublicKey is the Ed25519 public key for verifying signed commands.
	// When set (non-nil), sleep/wake commands must be signed with the corresponding
	// private key. When nil, all commands are accepted (backward compatible).
	SigningPublicKey *[32]byte

	// TimestampWindow is the maximum age of a command timestamp to accept.
	// Commands with timestamps outside +/- this window are rejected.
	// Default is 5 minutes.
	TimestampWindow time.Duration

	// AdvertiseRateLimit is the steady-state rate (advertisements per second)
	// accepted from a single peer for ROUTE_ADVERTISE / NODE_INFO_ADVERTISE.
	// A misbehaving peer exceeding this rate has frames dropped at ingest.
	// Default is 10 per second; zero or negative disables the limiter.
	AdvertiseRateLimit float64

	// AdvertiseRateBurst is the per-peer burst size for the advertise rate
	// limiter. Default is 50.
	AdvertiseRateBurst int
}

// DefaultFloodConfig returns sensible defaults.
func DefaultFloodConfig() FloodConfig {
	return FloodConfig{
		SeenCacheTTL:       5 * time.Minute,
		FloodInterval:      1 * time.Second,
		MaxSeenCacheSize:   10000,
		TimestampWindow:    5 * time.Minute,
		AdvertiseRateLimit: 10,
		AdvertiseRateBurst: 50,
	}
}

// PeerSender is an interface for sending frames to peers.
type PeerSender interface {
	// SendToPeer sends a frame to a specific peer.
	SendToPeer(peerID identity.AgentID, frame *protocol.Frame) error

	// GetPeerIDs returns all connected peer IDs.
	GetPeerIDs() []identity.AgentID
}

// Flooder handles route flooding to mesh peers.
type Flooder struct {
	cfg              FloodConfig
	localID          identity.AgentID
	displayNameMu    sync.RWMutex
	localDisplayName string
	routeMgr         *routing.Manager
	sender           PeerSender
	logger           *slog.Logger
	sealedBox        *crypto.SealedBox // Management key encryption (nil if not configured)
	signingPubKey    *[32]byte         // Ed25519 public key for command verification (nil = no verification)
	timestampWindow  time.Duration     // Validity window for command timestamps

	mu        sync.RWMutex
	seenCache map[AdvertisementKey]*SeenAdvertisement

	// Node info seen cache (separate from route advertisements)
	nodeInfoMu        sync.RWMutex
	nodeInfoSeenCache map[NodeInfoKey]*SeenNodeInfo
	nodeInfoSeq       uint64

	// Sleep command seen cache
	sleepCmdMu        sync.RWMutex
	sleepCmdSeenCache map[SleepCommandKey]*SeenSleepCommand

	// Pending wake command to forward to new peers
	pendingWakeMu  sync.RWMutex
	pendingWakeCmd *protocol.WakeCommand
	pendingWakeAt  time.Time

	// Per-peer rate limiters for ROUTE_ADVERTISE / NODE_INFO_ADVERTISE ingress.
	// advLimiters is nil when rate limiting is disabled.
	advLimiterMu sync.Mutex
	advLimiters  map[identity.AgentID]*rate.Limiter

	wg       sync.WaitGroup
	stopOnce sync.Once
	stopCh   chan struct{}
}

// NewFlooder creates a new route flooder.
func NewFlooder(cfg FloodConfig, localID identity.AgentID, routeMgr *routing.Manager, sender PeerSender) *Flooder {
	logger := cfg.Logger
	if logger == nil {
		logger = logging.NopLogger()
	}

	// Use default timestamp window if not set
	timestampWindow := cfg.TimestampWindow
	if timestampWindow == 0 {
		timestampWindow = 5 * time.Minute
	}

	// A non-positive AdvertiseRateLimit leaves advLimiters nil, disabling the
	// limiter entirely.
	var advLimiters map[identity.AgentID]*rate.Limiter
	if cfg.AdvertiseRateLimit > 0 {
		advLimiters = make(map[identity.AgentID]*rate.Limiter)
	}

	f := &Flooder{
		cfg:               cfg,
		localID:           localID,
		localDisplayName:  cfg.LocalDisplayName,
		routeMgr:          routeMgr,
		sender:            sender,
		logger:            logger,
		sealedBox:         cfg.SealedBox,
		signingPubKey:     cfg.SigningPublicKey,
		timestampWindow:   timestampWindow,
		seenCache:         make(map[AdvertisementKey]*SeenAdvertisement),
		nodeInfoSeenCache: make(map[NodeInfoKey]*SeenNodeInfo),
		nodeInfoSeq:       uint64(time.Now().UnixMilli()),
		sleepCmdSeenCache: make(map[SleepCommandKey]*SeenSleepCommand),
		advLimiters:       advLimiters,
		stopCh:            make(chan struct{}),
	}

	// Start cache cleanup goroutine
	f.wg.Add(1)
	go f.cleanupLoop()

	return f
}

// allowAdvertise returns true if the peer's token bucket admits this
// advertisement. Returns true unconditionally when rate limiting is disabled.
func (f *Flooder) allowAdvertise(peerID identity.AgentID) bool {
	if f.advLimiters == nil {
		return true
	}
	f.advLimiterMu.Lock()
	lim, ok := f.advLimiters[peerID]
	if !ok {
		burst := f.cfg.AdvertiseRateBurst
		if burst <= 0 {
			burst = 50
		}
		lim = rate.NewLimiter(rate.Limit(f.cfg.AdvertiseRateLimit), burst)
		f.advLimiters[peerID] = lim
	}
	f.advLimiterMu.Unlock()
	return lim.Allow()
}

// RemoveAdvertiseLimiter releases the per-peer limiter on peer disconnect so
// the map does not grow unbounded. No-op when the limiter is disabled.
func (f *Flooder) RemoveAdvertiseLimiter(peerID identity.AgentID) {
	if f.advLimiters == nil {
		return
	}
	f.advLimiterMu.Lock()
	delete(f.advLimiters, peerID)
	f.advLimiterMu.Unlock()
}

// HandleRouteAdvertise processes an incoming ROUTE_ADVERTISE frame.
// Returns true if the advertisement was new and should be processed.
// comments, when non-nil, must be parallel to routes (one entry per
// protocol.Route). It carries operator metadata for CIDR routes; older
// senders omit the slice entirely.
func (f *Flooder) HandleRouteAdvertise(
	fromPeer identity.AgentID,
	originAgent identity.AgentID,
	originDisplayName string,
	sequence uint64,
	routes []protocol.Route,
	comments []string,
	encPath *protocol.EncryptedData,
	seenBy []identity.AgentID,
) bool {
	if !f.allowAdvertise(fromPeer) {
		f.logger.Warn("dropping route advertisement: per-peer rate limit exceeded",
			"from_peer", fromPeer.ShortString(),
			"origin", originAgent.ShortString(),
			"sequence", sequence)
		return false
	}

	// We are the only authority on routes originating from us. Routes stored
	// under our own origin are never expired by CleanupStaleRoutes, so
	// accepting one from a peer would install an entry we can neither refresh
	// nor age out for the life of the process. NodeInfo already guards this;
	// route advertisements did not.
	if originAgent == f.localID {
		f.logger.Warn("dropping route advertisement claiming our own origin",
			"from_peer", fromPeer.ShortString(),
			"sequence", sequence)
		return false
	}

	key := AdvertisementKey{
		OriginAgent: originAgent,
		Sequence:    sequence,
	}

	// Check if we've already seen this and mark as seen atomically
	f.mu.Lock()
	if existing, ok := f.seenCache[key]; ok {
		// Already seen - update seen time if from a new peer
		if existing.SeenFrom != fromPeer {
			existing.SeenAt = time.Now()
		}
		f.mu.Unlock()
		f.logger.Debug("route advertisement already seen",
			"origin", originAgent.ShortString(),
			"sequence", sequence,
			"from_peer", fromPeer.ShortString(),
			"original_from", existing.SeenFrom.ShortString())
		return false
	}

	// Mark as seen
	f.seenCache[key] = &SeenAdvertisement{
		Key:      key,
		SeenAt:   time.Now(),
		SeenFrom: fromPeer,
	}
	cacheSize := len(f.seenCache)
	f.mu.Unlock()

	f.logger.Debug("new route advertisement received",
		"origin", originAgent.ShortString(),
		"sequence", sequence,
		"from_peer", fromPeer.ShortString(),
		"routes", len(routes),
		"cache_size", cacheSize)

	// Store display name for origin agent.
	// When management key encryption is enabled, suppress storing display names
	// from route advertisements to prevent accumulating plaintext names that
	// could be exposed through other code paths.
	if originDisplayName != "" && f.sealedBox == nil {
		f.routeMgr.SetDisplayName(originAgent, originDisplayName)
	}

	// Check if we're in the seen-by list (loop detection)
	if containsAgent(seenBy, f.localID) {
		return false
	}

	// Decode path from advertisement
	// Note: Paths are sent as plaintext for routing (not encrypted)
	// because transit agents need the path to forward STREAM_OPEN frames.
	// Path hiding happens at the API layer, not on the wire.
	var path []identity.AgentID
	if encPath != nil {
		if encPath.Encrypted {
			// Legacy: try to decrypt if we have the private key
			// (for backwards compatibility with old encrypted paths)
			if f.sealedBox != nil && f.sealedBox.CanDecrypt() {
				decrypted, err := f.sealedBox.Open(encPath.Data)
				if err == nil {
					path, _ = protocol.DecodePath(decrypted)
				}
			}
			// If we can't decrypt, path remains nil (routing will fail)
		} else {
			// Plaintext - decode directly (normal case)
			path, _ = protocol.DecodePath(encPath.Data)
		}
	}

	// Convert protocol routes to routing entries (CIDR, domain, forward, and agent)
	cidrEntries := make([]routing.RouteEntry, 0, len(routes))
	domainEntries := make([]routing.DomainRouteEntry, 0)
	forwardEntries := make([]routing.ForwardRouteEntry, 0)

	for i, r := range routes {
		switch r.AddressFamily {
		case protocol.AddrFamilyDomain:
			// Domain route: PrefixLength 0=exact, 1=wildcard
			pattern := protocol.DecodeDomainPrefix(r.Prefix)
			if pattern != "" {
				domainEntries = append(domainEntries, routing.DomainRouteEntry{
					Pattern:    pattern,
					IsWildcard: r.PrefixLength == 1,
					Metric:     r.Metric,
				})
			}
		case protocol.AddrFamilyForward:
			// Forward route: Prefix contains the routing key and target
			key, target := protocol.DecodeForwardKeyAndTarget(r.Prefix)
			if key != "" {
				forwardEntries = append(forwardEntries, routing.ForwardRouteEntry{
					Key:    key,
					Target: target,
					Metric: r.Metric,
				})
			}
		case protocol.AddrFamilyAgent:
			// Agent presence route: 16-byte agent ID prefix
			agentID := protocol.DecodeAgentPrefix(r.Prefix)
			if agentID != (identity.AgentID{}) {
				f.routeMgr.ProcessAgentRouteAdvertise(fromPeer, originAgent, sequence, agentID, path, encPath, r.Metric+1)
			}
		default:
			// CIDR route (IPv4 or IPv6). comments is nil from older
			// senders; bounds check covers that and any sender bug.
			if ipNet := protocolRouteToIPNet(r); ipNet != nil {
				comment := ""
				if i < len(comments) {
					comment = comments[i]
				}
				cidrEntries = append(cidrEntries, routing.RouteEntry{
					Network: ipNet,
					Metric:  r.Metric,
					Comment: comment,
				})
			}
		}
	}

	// Process CIDR routes in routing manager
	if len(cidrEntries) > 0 {
		f.routeMgr.ProcessRouteAdvertise(fromPeer, originAgent, sequence, cidrEntries, path, encPath)
	}

	// Process domain routes in routing manager
	if len(domainEntries) > 0 {
		f.routeMgr.ProcessDomainRouteAdvertise(fromPeer, originAgent, sequence, domainEntries, path, encPath)
	}

	// Process forward routes in routing manager
	if len(forwardEntries) > 0 {
		// Log forward-routing key collisions only on state change: if this
		// origin is already known for the key, the collision set is unchanged
		// and we silently re-advertise; otherwise this is a new conflicting
		// origin and we log once.
		if fwdTable := f.routeMgr.ForwardTable(); fwdTable != nil {
			for _, entry := range forwardEntries {
				if fwdTable.HasRoute(entry.Key, originAgent) {
					continue
				}
				conflicts := fwdTable.ConflictingOriginsForKey(entry.Key, originAgent)
				if len(conflicts) == 0 {
					continue
				}
				conflictShort := make([]string, 0, len(conflicts))
				for _, id := range conflicts {
					conflictShort = append(conflictShort, id.ShortString())
				}
				f.logger.Warn("forward routing key advertised by multiple origins",
					"key", entry.Key,
					"new_origin", originAgent.ShortString(),
					"existing_origins", conflictShort)
			}
		}
		f.routeMgr.ProcessForwardRouteAdvertise(fromPeer, originAgent, sequence, forwardEntries, path, encPath)
	}

	// Flood to other peers (forward encrypted path as-is).
	newSeenBy := append(seenBy, f.localID)
	f.floodAdvertisementEncrypted(fromPeer, originAgent, originDisplayName, sequence, routes, comments, encPath, newSeenBy)

	return true
}

// HandleRouteWithdraw processes an incoming ROUTE_WITHDRAW frame.
func (f *Flooder) HandleRouteWithdraw(
	fromPeer identity.AgentID,
	originAgent identity.AgentID,
	sequence uint64,
	routes []protocol.Route,
	seenBy []identity.AgentID,
) bool {
	key := AdvertisementKey{
		OriginAgent: originAgent,
		Sequence:    sequence,
	}

	// Check if we've seen this
	f.mu.Lock()
	if _, ok := f.seenCache[key]; ok {
		f.mu.Unlock()
		return false
	}

	f.seenCache[key] = &SeenAdvertisement{
		Key:      key,
		SeenAt:   time.Now(),
		SeenFrom: fromPeer,
	}
	f.mu.Unlock()

	// Check loop detection
	if containsAgent(seenBy, f.localID) {
		return false
	}

	var cidrEntries []routing.RouteEntry
	var fwdEntries []routing.ForwardRouteEntry
	var domEntries []routing.DomainRouteEntry
	for _, r := range routes {
		switch r.AddressFamily {
		case protocol.AddrFamilyIPv4, protocol.AddrFamilyIPv6:
			if ipNet := protocolRouteToIPNet(r); ipNet != nil {
				cidrEntries = append(cidrEntries, routing.RouteEntry{
					Network: ipNet,
					Metric:  r.Metric,
				})
			}
		case protocol.AddrFamilyForward:
			// Match by key+origin only; ignore any encoded target.
			if k := protocol.DecodeForwardKey(r.Prefix); k != "" {
				fwdEntries = append(fwdEntries, routing.ForwardRouteEntry{
					Key: k,
				})
			}
		case protocol.AddrFamilyDomain:
			if p := protocol.DecodeDomainPrefix(r.Prefix); p != "" {
				domEntries = append(domEntries, routing.DomainRouteEntry{
					Pattern:    p,
					IsWildcard: r.PrefixLength == 1,
				})
			}
		}
	}

	if len(cidrEntries) > 0 {
		f.routeMgr.ProcessRouteWithdraw(originAgent, cidrEntries)
	}
	if len(fwdEntries) > 0 {
		f.routeMgr.ProcessForwardRouteWithdraw(originAgent, fwdEntries)
	}
	if len(domEntries) > 0 {
		f.routeMgr.ProcessDomainRouteWithdraw(originAgent, domEntries)
	}

	// Flood withdrawal to other peers
	newSeenBy := append(seenBy, f.localID)
	f.floodWithdrawal(fromPeer, originAgent, sequence, routes, newSeenBy)

	return true
}

// floodAdvertisementEncrypted sends a route advertisement to all peers except the source.
// For plaintext paths, it prepends the local agent ID to the path before forwarding.
// For encrypted paths (legacy), it forwards as-is since we can't modify encrypted data.
// comments must be parallel to routes (or nil/shorter for backwards compatibility).
func (f *Flooder) floodAdvertisementEncrypted(
	fromPeer identity.AgentID,
	originAgent identity.AgentID,
	originDisplayName string,
	sequence uint64,
	routes []protocol.Route,
	comments []string,
	encPath *protocol.EncryptedData,
	seenBy []identity.AgentID,
) {
	// Extend the path if it's plaintext (normal case)
	// For encrypted paths (legacy), forward as-is
	fwdEncPath := encPath
	if encPath != nil && !encPath.Encrypted {
		// Decode existing path, prepend our ID, re-encode
		existingPath, _ := protocol.DecodePath(encPath.Data)
		newPath := make([]identity.AgentID, len(existingPath)+1)
		newPath[0] = f.localID
		copy(newPath[1:], existingPath)
		fwdEncPath = &protocol.EncryptedData{
			Encrypted: false,
			Data:      protocol.EncodePath(newPath),
		}
	}

	// When management key encryption is enabled, omit display names from
	// route advertisements to avoid leaking them in plaintext on the wire.
	fwdDisplayName := originDisplayName
	if f.sealedBox != nil {
		fwdDisplayName = ""
	}

	// Build the advertise payload with extended path
	adv := &protocol.RouteAdvertise{
		OriginAgent:       originAgent,
		OriginDisplayName: fwdDisplayName,
		Sequence:          sequence,
		Routes:            routes,
		EncPath:           fwdEncPath,
		SeenBy:            seenBy,
		Comments:          comments,
	}

	frame := &protocol.Frame{
		Type:     protocol.FrameRouteAdvertise,
		StreamID: protocol.ControlStreamID,
		Payload:  adv.Encode(),
	}

	f.floodFrame(fromPeer, seenBy, frame, "failed to send route advertisement")
}

// floodWithdrawal sends a route withdrawal to all peers except the source.
func (f *Flooder) floodWithdrawal(
	fromPeer identity.AgentID,
	originAgent identity.AgentID,
	sequence uint64,
	routes []protocol.Route,
	seenBy []identity.AgentID,
) {
	withdraw := &protocol.RouteWithdraw{
		OriginAgent: originAgent,
		Sequence:    sequence,
		Routes:      routes,
		SeenBy:      seenBy,
	}

	frame := &protocol.Frame{
		Type:     protocol.FrameRouteWithdraw,
		StreamID: protocol.ControlStreamID,
		Payload:  withdraw.Encode(),
	}

	f.floodFrame(fromPeer, seenBy, frame, "failed to send route withdrawal")
}

// SetLocalDisplayName updates the local display name used in route advertisements.
func (f *Flooder) SetLocalDisplayName(name string) {
	f.displayNameMu.Lock()
	f.localDisplayName = name
	f.displayNameMu.Unlock()
}

// getLocalDisplayName returns the local display name, safe for concurrent access.
func (f *Flooder) getLocalDisplayName() string {
	f.displayNameMu.RLock()
	name := f.localDisplayName
	f.displayNameMu.RUnlock()
	return name
}

// buildLocalRouteAdvertisements creates the route advertisement frames for the
// local agent's routes, including its agent presence route. Each call
// increments the sequence counter to produce a unique advertisement. More than
// protocol.MaxRoutesPerMessage local routes yields several frames, all sharing the one
// sequence, and callers must send every frame.
func (f *Flooder) buildLocalRouteAdvertisements() []*protocol.Frame {
	localRoutes := f.routeMgr.GetLocalRoutes()
	localDomainRoutes := f.routeMgr.GetLocalDomainRoutes()
	localForwardRoutes := f.routeMgr.GetLocalForwardRoutes()

	seq := f.routeMgr.IncrementSequence()

	// Convert to protocol routes (CIDR + domain + forward + agent presence).
	// comments is a parallel slice; only CIDR routes carry comments today,
	// other families and the agent presence record use empty strings.
	routes := make([]protocol.Route, 0, len(localRoutes)+len(localDomainRoutes)+len(localForwardRoutes)+1)
	comments := make([]string, 0, cap(routes))
	hasComment := false

	// Add CIDR routes
	for _, lr := range localRoutes {
		routes = append(routes, ipNetToProtocolRoute(lr.Network, lr.Metric))
		comments = append(comments, lr.Comment)
		if lr.Comment != "" {
			hasComment = true
		}
	}

	// Add domain routes
	for _, dr := range localDomainRoutes {
		prefixLen := uint8(0) // 0 = exact match
		if dr.IsWildcard {
			prefixLen = 1 // 1 = wildcard
		}
		routes = append(routes, protocol.Route{
			AddressFamily: protocol.AddrFamilyDomain,
			PrefixLength:  prefixLen,
			Prefix:        protocol.EncodeDomainPrefix(dr.Pattern),
			Metric:        dr.Metric,
		})
		comments = append(comments, "")
	}

	// Add forward routes
	for _, tr := range localForwardRoutes {
		routes = append(routes, protocol.Route{
			AddressFamily: protocol.AddrFamilyForward,
			PrefixLength:  0, // Not used for forward routes
			Prefix:        protocol.EncodeForwardKeyWithTarget(tr.Key, tr.Target),
			Metric:        tr.Metric,
		})
		comments = append(comments, "")
	}

	// Always add agent presence route (makes this agent reachable by ID)
	routes = append(routes, protocol.Route{
		AddressFamily: protocol.AddrFamilyAgent,
		PrefixLength:  0,
		Prefix:        protocol.EncodeAgentPrefix(f.localID),
		Metric:        0,
	})
	comments = append(comments, "")

	// Elide the wire field entirely when no route carries a comment.
	if !hasComment {
		comments = nil
	}

	// Build path data (always plaintext - needed for multi-hop routing)
	// Note: Path encryption was removed because transit agents need the path
	// to forward STREAM_OPEN frames. Path hiding happens at the API layer.
	path := []identity.AgentID{f.localID}
	pathBytes := protocol.EncodePath(path)
	encPath := &protocol.EncryptedData{
		Encrypted: false,
		Data:      pathBytes,
	}

	// When management key encryption is enabled, omit display names from
	// route advertisements to avoid leaking them in plaintext on the wire.
	displayName := f.getLocalDisplayName()
	if f.sealedBox != nil {
		displayName = ""
	}

	// Build advertisement, splitting when the route count would overflow the
	// single-byte count on the wire.
	//
	// Each chunk carries its OWN sequence. Receivers dedupe on
	// (OriginAgent, Sequence), so chunks sharing one sequence would be
	// discarded as duplicates and only the first would ever propagate.
	chunks := routeChunks(len(routes))
	frames := make([]*protocol.Frame, 0, len(chunks))
	for i, c := range chunks {
		chunkSeq := seq
		if i > 0 {
			chunkSeq = f.routeMgr.IncrementSequence()
		}
		adv := &protocol.RouteAdvertise{
			OriginAgent:       f.localID,
			OriginDisplayName: displayName,
			Sequence:          chunkSeq,
			Routes:            routes[c[0]:c[1]],
			Path:              path,    // Keep for backwards compat
			EncPath:           encPath, // Encrypted path for wire format
			SeenBy:            []identity.AgentID{f.localID},
		}
		// comments is parallel to routes, so it slices with the same bounds.
		if comments != nil {
			adv.Comments = comments[c[0]:c[1]]
		}

		frames = append(frames, &protocol.Frame{
			Type:     protocol.FrameRouteAdvertise,
			StreamID: protocol.ControlStreamID,
			Payload:  adv.Encode(),
		})
	}

	return frames
}

// AnnounceLocalRoutes floods all local routes (CIDR, domain, forward, and
// agent presence) to all peers.
func (f *Flooder) AnnounceLocalRoutes() {
	frames := f.buildLocalRouteAdvertisements()

	// Send to all peers
	for _, peerID := range f.sender.GetPeerIDs() {
		for _, frame := range frames {
			if err := f.sender.SendToPeer(peerID, frame); err != nil {
				f.logger.Debug("failed to announce local routes",
					logging.KeyPeerID, peerID.ShortString(),
					logging.KeyError, err)
				break
			}
		}
	}
}

// WithdrawLocalRoutes floods withdrawal of all local routes.
func (f *Flooder) WithdrawLocalRoutes() {
	localRoutes := f.routeMgr.GetLocalRoutes()
	if len(localRoutes) == 0 {
		return
	}

	seq := f.routeMgr.IncrementSequence()

	routes := make([]protocol.Route, 0, len(localRoutes))
	for _, lr := range localRoutes {
		routes = append(routes, ipNetToProtocolRoute(lr.Network, lr.Metric))
	}

	// Split when the route count would overflow the single-byte wire count.
	// Each chunk needs its own sequence, or the receiver's
	// (OriginAgent, Sequence) dedupe drops every chunk after the first.
	chunks := routeChunks(len(routes))
	frames := make([]*protocol.Frame, 0, len(chunks))
	for i, c := range chunks {
		chunkSeq := seq
		if i > 0 {
			chunkSeq = f.routeMgr.IncrementSequence()
		}
		withdraw := &protocol.RouteWithdraw{
			OriginAgent: f.localID,
			Sequence:    chunkSeq,
			Routes:      routes[c[0]:c[1]],
			SeenBy:      []identity.AgentID{f.localID},
		}
		frames = append(frames, &protocol.Frame{
			Type:     protocol.FrameRouteWithdraw,
			StreamID: protocol.ControlStreamID,
			Payload:  withdraw.Encode(),
		})
	}

	for _, peerID := range f.sender.GetPeerIDs() {
		for _, frame := range frames {
			if err := f.sender.SendToPeer(peerID, frame); err != nil {
				f.logger.Debug("failed to withdraw local routes",
					logging.KeyPeerID, peerID.ShortString(),
					logging.KeyError, err)
				break
			}
		}
	}
}

// withdrawSingle floods a single-route withdrawal with a fresh sequence
// so peers' seen-cache accepts it. seenBy is seeded with self for loop
// suppression; fromPeer is zero so no neighbour is excluded.
func (f *Flooder) withdrawSingle(r protocol.Route) {
	f.floodWithdrawal(
		identity.AgentID{},
		f.localID,
		f.routeMgr.IncrementSequence(),
		[]protocol.Route{r},
		[]identity.AgentID{f.localID},
	)
}

// WithdrawLocalForwardRoute floods withdrawal of a single locally-originated
// port forward route.
func (f *Flooder) WithdrawLocalForwardRoute(key, target string) {
	f.withdrawSingle(protocol.Route{
		AddressFamily: protocol.AddrFamilyForward,
		Prefix:        protocol.EncodeForwardKeyWithTarget(key, target),
	})
}

// WithdrawLocalDomainRoute floods withdrawal of a single locally-originated
// domain route.
func (f *Flooder) WithdrawLocalDomainRoute(pattern string, isWildcard bool) {
	prefixLen := uint8(0)
	if isWildcard {
		prefixLen = 1
	}
	f.withdrawSingle(protocol.Route{
		AddressFamily: protocol.AddrFamilyDomain,
		PrefixLength:  prefixLen,
		Prefix:        protocol.EncodeDomainPrefix(pattern),
	})
}

// WithdrawLocalCIDRRoute floods withdrawal of a single locally-originated
// CIDR route.
func (f *Flooder) WithdrawLocalCIDRRoute(network *net.IPNet, metric uint16) {
	f.withdrawSingle(ipNetToProtocolRoute(network, metric))
}

// routeChunks returns [start, end) index pairs covering n routes in groups of
// at most protocol.MaxRoutesPerMessage. It always returns at least one pair so that a
// message carrying no routes is still sent: an empty advertisement still
// conveys the origin's sequence and presence.
func routeChunks(n int) [][2]int {
	if n <= 0 {
		return [][2]int{{0, 0}}
	}
	chunks := make([][2]int, 0, (n+protocol.MaxRoutesPerMessage-1)/protocol.MaxRoutesPerMessage)
	for start := 0; start < n; start += protocol.MaxRoutesPerMessage {
		end := start + protocol.MaxRoutesPerMessage
		if end > n {
			end = n
		}
		chunks = append(chunks, [2]int{start, end})
	}
	return chunks
}

// SendFullTable sends the full routing table to a newly connected peer.
// Routes are grouped by origin agent and sent with their original path preserved.
// Includes CIDR, domain, forward, and agent presence routes.
func (f *Flooder) SendFullTable(peerID identity.AgentID) {
	fullRoutes := f.routeMgr.GetFullRoutesForAdvertise(peerID)
	agentRoutes := f.routeMgr.AgentTable().GetAllRoutes()
	forwardRoutes := f.routeMgr.ForwardTable().GetAllRoutes()
	domainRoutes := f.routeMgr.DomainTable().GetAllRoutes()

	// Group CIDR routes by origin agent
	byOrigin := make(map[identity.AgentID][]*routing.Route)
	for _, route := range fullRoutes {
		byOrigin[route.OriginAgent] = append(byOrigin[route.OriginAgent], route)
	}

	// Group agent presence routes by origin agent
	agentByOrigin := make(map[identity.AgentID][]*routing.AgentRoute)
	for _, route := range agentRoutes {
		// Don't send routes learned from the peer we're sending to
		if route.NextHop == peerID {
			continue
		}
		agentByOrigin[route.OriginAgent] = append(agentByOrigin[route.OriginAgent], route)
	}

	// Group forward routes by origin agent
	forwardByOrigin := make(map[identity.AgentID][]*routing.ForwardRoute)
	for _, route := range forwardRoutes {
		// Don't send routes learned from the peer we're sending to
		if route.NextHop == peerID {
			continue
		}
		forwardByOrigin[route.OriginAgent] = append(forwardByOrigin[route.OriginAgent], route)
	}

	// Group domain routes by origin agent
	domainByOrigin := make(map[identity.AgentID][]*routing.DomainRoute)
	for _, route := range domainRoutes {
		// Don't send routes learned from the peer we're sending to
		if route.NextHop == peerID {
			continue
		}
		domainByOrigin[route.OriginAgent] = append(domainByOrigin[route.OriginAgent], route)
	}

	// Collect all origin agents
	allOrigins := make(map[identity.AgentID]struct{})
	for id := range byOrigin {
		allOrigins[id] = struct{}{}
	}
	for id := range agentByOrigin {
		allOrigins[id] = struct{}{}
	}
	for id := range forwardByOrigin {
		allOrigins[id] = struct{}{}
	}
	for id := range domainByOrigin {
		allOrigins[id] = struct{}{}
	}

	// Send a separate advertisement for each origin
	for originAgent := range allOrigins {
		cidrRoutes := byOrigin[originAgent]
		agentPresenceRoutes := agentByOrigin[originAgent]
		forwardOriginRoutes := forwardByOrigin[originAgent]
		domainOriginRoutes := domainByOrigin[originAgent]

		// Re-advertising another agent's routes must carry that agent's own
		// sequence. Minting a local one makes this copy look strictly newer
		// than anything the origin itself sends, so receivers reject the
		// origin's genuine advertisements, stop refreshing LastUpdate, and
		// drop the route at RouteTTL. Sequences seed from the wall clock, so
		// a locally minted value can lead the origin's by an unbounded margin
		// and the origin never catches up.
		// Group this origin's routes by the sequence they were learned under.
		//
		// Re-advertising another agent's routes must carry that agent's own
		// sequence. Minting a local one makes the copy look strictly newer
		// than anything the origin itself sends, so receivers reject the
		// origin's genuine advertisements, stop refreshing LastUpdate, and
		// drop the route at RouteTTL. Sequences seed from the wall clock, so
		// a locally minted value can lead the origin's by an unbounded margin
		// and the origin never catches up.
		//
		// Grouping rather than taking one sequence for the whole origin also
		// keeps each advertisement decodable and deliverable: receivers dedupe
		// on (OriginAgent, Sequence), so routes learned under different
		// sequences must go out under those same sequences or all but one
		// group would be discarded as duplicates. Each group is inherently
		// within the wire's 255-route limit, because it arrived that way.
		bySequence := make(map[uint64][]protocol.Route)
		addRoute := func(seq uint64, r protocol.Route) {
			bySequence[seq] = append(bySequence[seq], r)
		}
		for _, r := range cidrRoutes {
			addRoute(r.Sequence, routeToProtocol(r))
		}
		for _, r := range agentPresenceRoutes {
			addRoute(r.Sequence, protocol.Route{
				AddressFamily: protocol.AddrFamilyAgent,
				PrefixLength:  0,
				Prefix:        protocol.EncodeAgentPrefix(r.AgentID),
				Metric:        r.Metric,
			})
		}
		for _, r := range forwardOriginRoutes {
			addRoute(r.Sequence, protocol.Route{
				AddressFamily: protocol.AddrFamilyForward,
				PrefixLength:  0,
				Prefix:        protocol.EncodeForwardKeyWithTarget(r.Key, r.Target),
				Metric:        r.Metric,
			})
		}
		for _, r := range domainOriginRoutes {
			prefixLen := uint8(0)
			if r.IsWildcard {
				prefixLen = 1
			}
			addRoute(r.Sequence, protocol.Route{
				AddressFamily: protocol.AddrFamilyDomain,
				PrefixLength:  prefixLen,
				Prefix:        protocol.EncodeDomainPrefix(r.Pattern),
				Metric:        r.Metric,
			})
		}

		// Use the path from the first available route
		// Prepend ourselves to the path
		var path []identity.AgentID
		switch {
		case len(cidrRoutes) > 0 && len(cidrRoutes[0].Path) > 0:
			path = append([]identity.AgentID{f.localID}, cidrRoutes[0].Path...)
		case len(agentPresenceRoutes) > 0 && len(agentPresenceRoutes[0].Path) > 0:
			path = append([]identity.AgentID{f.localID}, agentPresenceRoutes[0].Path...)
		case len(forwardOriginRoutes) > 0 && len(forwardOriginRoutes[0].Path) > 0:
			path = append([]identity.AgentID{f.localID}, forwardOriginRoutes[0].Path...)
		case len(domainOriginRoutes) > 0 && len(domainOriginRoutes[0].Path) > 0:
			path = append([]identity.AgentID{f.localID}, domainOriginRoutes[0].Path...)
		default:
			path = []identity.AgentID{f.localID}
		}

		// Get display name for origin agent.
		// When management key encryption is enabled, omit display names from
		// route advertisements to avoid leaking them in plaintext on the wire.
		var originDisplayName string
		if f.sealedBox == nil {
			if originAgent == f.localID {
				originDisplayName = f.getLocalDisplayName()
			} else {
				originDisplayName = f.routeMgr.GetDisplayName(originAgent)
			}
		}

		// One advertisement per sequence group. Iterate the sorted sequences
		// so the peer sees them oldest-first and map order does not make the
		// output nondeterministic.
		sequences := make([]uint64, 0, len(bySequence))
		for seq := range bySequence {
			sequences = append(sequences, seq)
		}
		sort.Slice(sequences, func(i, j int) bool { return sequences[i] < sequences[j] })

	sendGroups:
		for _, seq := range sequences {
			groupRoutes := bySequence[seq]

			// A group larger than the wire limit can only happen if an origin
			// packed more than 255 routes under one sequence, which its own
			// encoder would not have produced. Split anyway so the frame stays
			// decodable; the receiver's (origin, sequence) dedupe means only
			// the first split survives, so log the shortfall rather than
			// silently advertising a partial table.
			chunks := routeChunks(len(groupRoutes))
			if len(chunks) > 1 {
				f.logger.Warn("origin has more routes under one sequence than a single advertisement can carry",
					"origin", originAgent.ShortString(),
					"sequence", seq,
					logging.KeyCount, len(groupRoutes),
					"advertised", protocol.MaxRoutesPerMessage)
			}

			for _, c := range chunks {
				adv := &protocol.RouteAdvertise{
					OriginAgent:       originAgent,
					OriginDisplayName: originDisplayName,
					Sequence:          seq,
					Routes:            groupRoutes[c[0]:c[1]],
					Path:              path,
					SeenBy:            []identity.AgentID{f.localID},
				}

				frame := &protocol.Frame{
					Type:     protocol.FrameRouteAdvertise,
					StreamID: protocol.ControlStreamID,
					Payload:  adv.Encode(),
				}

				if err := f.sender.SendToPeer(peerID, frame); err != nil {
					f.logger.Debug("failed to send full routing table",
						logging.KeyPeerID, peerID.ShortString(),
						logging.KeyError, err)
					break sendGroups
				}
			}
		}
	}

	// Always include local agent's own routes (especially agent presence).
	// AnnounceLocalRoutes only runs periodically, so without this, new peers
	// won't learn about our presence until the next periodic announcement.
	for _, localFrame := range f.buildLocalRouteAdvertisements() {
		if err := f.sender.SendToPeer(peerID, localFrame); err != nil {
			f.logger.Debug("failed to send local routes in full table",
				logging.KeyPeerID, peerID.ShortString(),
				logging.KeyError, err)
			break
		}
	}
}

// cleanupLoop periodically cleans up expired seen entries.
func (f *Flooder) cleanupLoop() {
	defer f.wg.Done()
	defer recovery.RecoverWithLog(f.logger, "flood.cleanupLoop")

	ticker := time.NewTicker(f.cfg.SeenCacheTTL / 2)
	defer ticker.Stop()

	for {
		select {
		case <-f.stopCh:
			return
		case <-ticker.C:
			f.cleanup()
		}
	}
}

// cleanup removes expired entries from the seen caches.
func (f *Flooder) cleanup() {
	now := time.Now()
	expiry := f.cfg.SeenCacheTTL

	// Cleanup route advertisement cache
	f.mu.Lock()
	f.cleanupSeenCache(now, expiry)
	f.mu.Unlock()

	// Cleanup node info cache
	f.nodeInfoMu.Lock()
	f.cleanupNodeInfoCache(now, expiry)
	f.nodeInfoMu.Unlock()

	// Cleanup sleep command cache
	f.sleepCmdMu.Lock()
	f.cleanupSleepCmdCache(now, expiry)
	f.sleepCmdMu.Unlock()
}

// cleanupSeenCache removes expired entries from the seen cache.
// Must be called with f.mu held.
func (f *Flooder) cleanupSeenCache(now time.Time, expiry time.Duration) {
	for key, entry := range f.seenCache {
		if now.Sub(entry.SeenAt) > expiry {
			delete(f.seenCache, key)
		}
	}

	// If still too large, remove entries until under limit
	excess := len(f.seenCache) - f.cfg.MaxSeenCacheSize
	if excess <= 0 {
		return
	}
	removed := 0
	for key := range f.seenCache {
		delete(f.seenCache, key)
		removed++
		if removed >= excess {
			break
		}
	}
}

// cleanupNodeInfoCache removes expired entries from the node info cache.
// Must be called with f.nodeInfoMu held.
func (f *Flooder) cleanupNodeInfoCache(now time.Time, expiry time.Duration) {
	for key, entry := range f.nodeInfoSeenCache {
		if now.Sub(entry.SeenAt) > expiry {
			delete(f.nodeInfoSeenCache, key)
		}
	}

	// If still too large, remove entries until under limit
	excess := len(f.nodeInfoSeenCache) - f.cfg.MaxSeenCacheSize
	if excess <= 0 {
		return
	}
	removed := 0
	for key := range f.nodeInfoSeenCache {
		delete(f.nodeInfoSeenCache, key)
		removed++
		if removed >= excess {
			break
		}
	}
}

// cleanupSleepCmdCache removes expired entries from the sleep command cache.
// Must be called with f.sleepCmdMu held.
func (f *Flooder) cleanupSleepCmdCache(now time.Time, expiry time.Duration) {
	for key, entry := range f.sleepCmdSeenCache {
		if now.Sub(entry.SeenAt) > expiry {
			delete(f.sleepCmdSeenCache, key)
		}
	}

	// If still too large, remove oldest entries
	excess := len(f.sleepCmdSeenCache) - f.cfg.MaxSeenCacheSize
	if excess <= 0 {
		return
	}
	removed := 0
	for key := range f.sleepCmdSeenCache {
		delete(f.sleepCmdSeenCache, key)
		removed++
		if removed >= excess {
			break
		}
	}
}

// ClearSeenCacheForOrigin removes all seen cache entries originating from
// a specific agent. Called on peer disconnect so re-advertisements after
// reconnection are not rejected as duplicates.
func (f *Flooder) ClearSeenCacheForOrigin(originAgent identity.AgentID) {
	f.mu.Lock()
	for key := range f.seenCache {
		if key.OriginAgent == originAgent {
			delete(f.seenCache, key)
		}
	}
	f.mu.Unlock()

	f.nodeInfoMu.Lock()
	for key := range f.nodeInfoSeenCache {
		if key.OriginAgent == originAgent {
			delete(f.nodeInfoSeenCache, key)
		}
	}
	f.nodeInfoMu.Unlock()
}

// SeenCacheSize returns the current size of the seen cache.
func (f *Flooder) SeenCacheSize() int {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return len(f.seenCache)
}

// Stop stops the flooder.
func (f *Flooder) Stop() {
	f.stopOnce.Do(func() {
		close(f.stopCh)
	})
	f.wg.Wait()
}

// containsAgent checks if the given agent ID is in the list.
func containsAgent(list []identity.AgentID, id identity.AgentID) bool {
	for _, v := range list {
		if v == id {
			return true
		}
	}
	return false
}

// protocolRouteToIPNet converts a protocol.Route to a net.IPNet.
// Returns nil if the route is not an IP route (e.g., domain route) or has an empty prefix.
func protocolRouteToIPNet(r protocol.Route) *net.IPNet {
	if len(r.Prefix) == 0 {
		return nil
	}
	switch r.AddressFamily {
	case protocol.AddrFamilyIPv4:
		ip := make(net.IP, 4)
		copy(ip, r.Prefix)
		return &net.IPNet{IP: ip, Mask: net.CIDRMask(int(r.PrefixLength), 32)}
	case protocol.AddrFamilyIPv6:
		ip := make(net.IP, 16)
		copy(ip, r.Prefix)
		return &net.IPNet{IP: ip, Mask: net.CIDRMask(int(r.PrefixLength), 128)}
	default:
		return nil
	}
}

// ipNetToProtocolRoute converts a net.IPNet and metric to a protocol.Route.
func ipNetToProtocolRoute(network *net.IPNet, metric uint16) protocol.Route {
	ones, bits := network.Mask.Size()
	family := protocol.AddrFamilyIPv4
	if bits == 128 {
		family = protocol.AddrFamilyIPv6
	}
	return protocol.Route{
		AddressFamily: family,
		PrefixLength:  uint8(ones),
		Prefix:        []byte(network.IP),
		Metric:        metric,
	}
}

// routeToProtocol converts a routing.Route (full route with path) to a protocol.Route.
func routeToProtocol(route *routing.Route) protocol.Route {
	return ipNetToProtocolRoute(route.Network, route.Metric)
}

// floodFrame sends a frame to all peers except the source and those in the seen-by list.
func (f *Flooder) floodFrame(fromPeer identity.AgentID, seenBy []identity.AgentID, frame *protocol.Frame, logMsg string) {
	for _, peerID := range f.sender.GetPeerIDs() {
		if peerID == fromPeer || containsAgent(seenBy, peerID) {
			continue
		}
		if err := f.sender.SendToPeer(peerID, frame); err != nil {
			f.logger.Debug(logMsg,
				logging.KeyPeerID, peerID.ShortString(),
				logging.KeyError, err)
		}
	}
}

// HasSeen checks if an advertisement has been seen.
func (f *Flooder) HasSeen(originAgent identity.AgentID, sequence uint64) bool {
	key := AdvertisementKey{
		OriginAgent: originAgent,
		Sequence:    sequence,
	}

	f.mu.RLock()
	defer f.mu.RUnlock()

	_, ok := f.seenCache[key]
	return ok
}

// ClearSeenCache clears the seen cache (for testing).
func (f *Flooder) ClearSeenCache() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seenCache = make(map[AdvertisementKey]*SeenAdvertisement)
}

// HandleNodeInfoAdvertise processes an incoming NODE_INFO_ADVERTISE frame.
// Returns true if the node info was new and should be processed.
func (f *Flooder) HandleNodeInfoAdvertise(
	fromPeer identity.AgentID,
	originAgent identity.AgentID,
	sequence uint64,
	encInfo *protocol.EncryptedData,
	seenBy []identity.AgentID,
) bool {
	if !f.allowAdvertise(fromPeer) {
		f.logger.Warn("dropping node info advertisement: per-peer rate limit exceeded",
			"from_peer", fromPeer.ShortString(),
			"origin", originAgent.ShortString(),
			"sequence", sequence)
		return false
	}

	key := NodeInfoKey{
		OriginAgent: originAgent,
		Sequence:    sequence,
	}

	// Check if we've already seen this and mark as seen atomically
	f.nodeInfoMu.Lock()
	if existing, ok := f.nodeInfoSeenCache[key]; ok {
		// Already seen - update seen time if from a new peer
		if existing.SeenFrom != fromPeer {
			existing.SeenAt = time.Now()
		}
		f.nodeInfoMu.Unlock()
		return false
	}

	// Mark as seen
	f.nodeInfoSeenCache[key] = &SeenNodeInfo{
		Key:      key,
		SeenAt:   time.Now(),
		SeenFrom: fromPeer,
	}
	f.nodeInfoMu.Unlock()

	// Check if we're in the seen-by list (loop detection)
	if containsAgent(seenBy, f.localID) {
		return false
	}

	// Store the node info in the routing manager (handles decryption if possible)
	if f.routeMgr.SetNodeInfoEncrypted(originAgent, encInfo, sequence) {
		f.logger.Debug("stored node info",
			"origin", originAgent.ShortString(),
			"encrypted", encInfo.Encrypted)
	}

	// Flood to other peers (forward encrypted data as-is)
	newSeenBy := append(seenBy, f.localID)
	f.floodNodeInfoEncrypted(fromPeer, originAgent, sequence, encInfo, newSeenBy)

	return true
}

// floodNodeInfoEncrypted sends encrypted node info advertisement to all peers except the source.
func (f *Flooder) floodNodeInfoEncrypted(
	fromPeer identity.AgentID,
	originAgent identity.AgentID,
	sequence uint64,
	encInfo *protocol.EncryptedData,
	seenBy []identity.AgentID,
) {
	adv := &protocol.NodeInfoAdvertise{
		OriginAgent: originAgent,
		Sequence:    sequence,
		EncInfo:     encInfo,
		SeenBy:      seenBy,
	}

	frame := &protocol.Frame{
		Type:     protocol.FrameNodeInfoAdvertise,
		StreamID: protocol.ControlStreamID,
		Payload:  adv.Encode(),
	}

	f.floodFrame(fromPeer, seenBy, frame, "failed to send node info advertisement")
}

// AnnounceLocalNodeInfo floods local node info to all peers.
func (f *Flooder) AnnounceLocalNodeInfo(info *protocol.NodeInfo) {
	if info == nil {
		return
	}

	// Increment sequence
	f.nodeInfoMu.Lock()
	f.nodeInfoSeq++
	seq := f.nodeInfoSeq
	f.nodeInfoMu.Unlock()

	// Store our own info in the routing manager (plaintext for local access)
	f.routeMgr.SetNodeInfo(f.localID, info, seq)

	// Build encrypted data for flooding
	var encInfo *protocol.EncryptedData
	infoBytes := protocol.EncodeNodeInfo(info)

	if f.sealedBox != nil {
		// Encrypt NodeInfo for flooding
		encrypted, err := f.sealedBox.Seal(infoBytes)
		if err != nil {
			f.logger.Debug("failed to encrypt node info",
				logging.KeyError, err)
			return
		}
		encInfo = &protocol.EncryptedData{
			Encrypted: true,
			Data:      encrypted,
		}
	} else {
		// No encryption - send plaintext
		encInfo = &protocol.EncryptedData{
			Encrypted: false,
			Data:      infoBytes,
		}
	}

	// Build the advertisement
	adv := &protocol.NodeInfoAdvertise{
		OriginAgent: f.localID,
		Sequence:    seq,
		Info:        *info, // Keep for backwards compat in Encode()
		EncInfo:     encInfo,
		SeenBy:      []identity.AgentID{f.localID},
	}

	frame := &protocol.Frame{
		Type:     protocol.FrameNodeInfoAdvertise,
		StreamID: protocol.ControlStreamID,
		Payload:  adv.Encode(),
	}

	// Send to all peers
	for _, peerID := range f.sender.GetPeerIDs() {
		if err := f.sender.SendToPeer(peerID, frame); err != nil {
			f.logger.Debug("failed to announce local node info",
				logging.KeyPeerID, peerID.ShortString(),
				logging.KeyError, err)
		}
	}

	f.logger.Debug("announced local node info",
		"display_name", info.DisplayName,
		"hostname", info.Hostname,
		"sequence", seq,
		"encrypted", encInfo.Encrypted)
}

// SendNodeInfoToNewPeer sends all known node info to a newly connected peer.
func (f *Flooder) SendNodeInfoToNewPeer(peerID identity.AgentID) {
	allEntries := f.routeMgr.GetAllNodeInfoEntries()
	if len(allEntries) == 0 {
		return
	}

	for agentID, entry := range allEntries {
		if entry == nil || entry.EncInfo == nil {
			continue
		}

		adv := &protocol.NodeInfoAdvertise{
			OriginAgent: agentID,
			Sequence:    entry.Sequence,
			EncInfo:     entry.EncInfo, // Forward encrypted data as-is
			SeenBy:      []identity.AgentID{f.localID},
		}

		frame := &protocol.Frame{
			Type:     protocol.FrameNodeInfoAdvertise,
			StreamID: protocol.ControlStreamID,
			Payload:  adv.Encode(),
		}

		if err := f.sender.SendToPeer(peerID, frame); err != nil {
			f.logger.Debug("failed to send node info to new peer",
				logging.KeyPeerID, peerID.ShortString(),
				"origin", agentID.ShortString(),
				logging.KeyError, err)
		}
	}

	f.logger.Debug("sent node info table to new peer",
		logging.KeyPeerID, peerID.ShortString(),
		"count", len(allEntries))
}

// NodeInfoSeenCacheSize returns the current size of the node info seen cache.
func (f *Flooder) NodeInfoSeenCacheSize() int {
	f.nodeInfoMu.RLock()
	defer f.nodeInfoMu.RUnlock()
	return len(f.nodeInfoSeenCache)
}

// markSleepCmdSeen checks if a sleep/wake command has been seen and marks it as seen.
// Returns true if this is a new command.
func (f *Flooder) markSleepCmdSeen(originAgent identity.AgentID, commandID uint64, fromPeer identity.AgentID) bool {
	key := SleepCommandKey{
		OriginAgent: originAgent,
		CommandID:   commandID,
	}

	f.sleepCmdMu.Lock()
	defer f.sleepCmdMu.Unlock()

	if existing, ok := f.sleepCmdSeenCache[key]; ok {
		if existing.SeenFrom != fromPeer {
			existing.SeenAt = time.Now()
		}
		return false
	}

	f.sleepCmdSeenCache[key] = &SeenSleepCommand{
		Key:      key,
		SeenAt:   time.Now(),
		SeenFrom: fromPeer,
	}
	return true
}

// HandleSleepCommand processes an incoming SLEEP_COMMAND frame.
// Returns true if the command was new and should be processed.
func (f *Flooder) HandleSleepCommand(fromPeer identity.AgentID, cmd *protocol.SleepCommand) bool {
	if !f.markSleepCmdSeen(cmd.OriginAgent, cmd.CommandID, fromPeer) {
		return false
	}

	if containsAgent(cmd.SeenBy, f.localID) {
		return false
	}

	// Verify signature if signing key is configured
	if err := f.verifySleepCommand(cmd); err != nil {
		f.logger.Warn("sleep command rejected",
			"origin", cmd.OriginAgent.ShortString(),
			"command_id", cmd.CommandID,
			"from_peer", fromPeer.ShortString(),
			logging.KeyError, err)
		return false
	}

	f.logger.Debug("new sleep command received",
		"origin", cmd.OriginAgent.ShortString(),
		"command_id", cmd.CommandID,
		"from_peer", fromPeer.ShortString(),
		"signed", !cmd.IsZeroSignature())

	newSeenBy := append(cmd.SeenBy, f.localID)
	f.floodSleepCommand(fromPeer, cmd, newSeenBy)

	return true
}

// HandleWakeCommand processes an incoming WAKE_COMMAND frame.
// Returns true if the command was new and should be processed.
func (f *Flooder) HandleWakeCommand(fromPeer identity.AgentID, cmd *protocol.WakeCommand) bool {
	if !f.markSleepCmdSeen(cmd.OriginAgent, cmd.CommandID, fromPeer) {
		return false
	}

	if containsAgent(cmd.SeenBy, f.localID) {
		return false
	}

	// Verify signature if signing key is configured
	if err := f.verifyWakeCommand(cmd); err != nil {
		f.logger.Warn("wake command rejected",
			"origin", cmd.OriginAgent.ShortString(),
			"command_id", cmd.CommandID,
			"from_peer", fromPeer.ShortString(),
			logging.KeyError, err)
		return false
	}

	f.logger.Debug("new wake command received",
		"origin", cmd.OriginAgent.ShortString(),
		"command_id", cmd.CommandID,
		"from_peer", fromPeer.ShortString(),
		"signed", !cmd.IsZeroSignature())

	newSeenBy := append(cmd.SeenBy, f.localID)
	f.floodWakeCommand(fromPeer, cmd, newSeenBy)

	// Store pending wake command for forwarding to new peers
	f.storePendingWake(cmd)

	return true
}

// verifySignedCommand enforces the replay-window and (when a signing key is
// configured) the Ed25519 signature check shared by sleep and wake commands.
//
// The timestamp window runs unconditionally so that unsigned commands captured
// in transit cannot be replayed indefinitely on meshes undergoing a partial
// signing rollout.
func (f *Flooder) verifySignedCommand(timestamp uint64, isZeroSig bool, signable []byte, sig [protocol.SignatureSize]byte) error {
	cmdTime := time.Unix(int64(timestamp), 0)
	timeDiff := time.Since(cmdTime)
	if timeDiff < 0 {
		timeDiff = -timeDiff
	}
	if timeDiff > f.timestampWindow {
		return fmt.Errorf("timestamp outside validity window (%v old, max %v)", timeDiff, f.timestampWindow)
	}

	if f.signingPubKey == nil {
		return nil
	}
	if isZeroSig {
		return fmt.Errorf("signature required but missing")
	}
	if !crypto.Verify(*f.signingPubKey, signable, sig) {
		return fmt.Errorf("signature verification failed")
	}
	return nil
}

func (f *Flooder) verifySleepCommand(cmd *protocol.SleepCommand) error {
	return f.verifySignedCommand(cmd.Timestamp, cmd.IsZeroSignature(), cmd.SignableBytes(), cmd.Signature)
}

func (f *Flooder) verifyWakeCommand(cmd *protocol.WakeCommand) error {
	return f.verifySignedCommand(cmd.Timestamp, cmd.IsZeroSignature(), cmd.SignableBytes(), cmd.Signature)
}

// FloodSleepCommand sends a sleep command to all peers.
// This is used to initiate mesh-wide sleep from this agent.
// The command should already be signed if signing is required.
func (f *Flooder) FloodSleepCommand(cmd *protocol.SleepCommand) error {
	f.markSleepCmdSeen(cmd.OriginAgent, cmd.CommandID, f.localID)

	cmdWithSeen := &protocol.SleepCommand{
		OriginAgent: cmd.OriginAgent,
		CommandID:   cmd.CommandID,
		Timestamp:   cmd.Timestamp,
		Signature:   cmd.Signature, // Preserve signature
		SeenBy:      []identity.AgentID{f.localID},
	}

	frame := &protocol.Frame{
		Type:     protocol.FrameSleepCommand,
		StreamID: protocol.ControlStreamID,
		Payload:  cmdWithSeen.Encode(),
	}

	f.broadcastFrame(frame, "sleep command")
	return nil
}

// FloodWakeCommand sends a wake command to all peers.
// This is used to initiate mesh-wide wake from this agent.
// The command should already be signed if signing is required.
func (f *Flooder) FloodWakeCommand(cmd *protocol.WakeCommand) error {
	f.markSleepCmdSeen(cmd.OriginAgent, cmd.CommandID, f.localID)

	// Store pending wake command for forwarding to new peers
	f.storePendingWake(cmd)

	cmdWithSeen := &protocol.WakeCommand{
		OriginAgent: cmd.OriginAgent,
		CommandID:   cmd.CommandID,
		Timestamp:   cmd.Timestamp,
		Signature:   cmd.Signature, // Preserve signature
		SeenBy:      []identity.AgentID{f.localID},
	}

	frame := &protocol.Frame{
		Type:     protocol.FrameWakeCommand,
		StreamID: protocol.ControlStreamID,
		Payload:  cmdWithSeen.Encode(),
	}

	f.broadcastFrame(frame, "wake command")
	return nil
}

// broadcastFrame sends a frame to all peers and logs the result.
func (f *Flooder) broadcastFrame(frame *protocol.Frame, cmdType string) {
	peerIDs := f.sender.GetPeerIDs()
	for _, peerID := range peerIDs {
		if err := f.sender.SendToPeer(peerID, frame); err != nil {
			f.logger.Debug("failed to send "+cmdType,
				logging.KeyPeerID, peerID.ShortString(),
				logging.KeyError, err)
		}
	}
	f.logger.Info("flooded "+cmdType, "peers", len(peerIDs))
}

// floodSleepCommand forwards a sleep command to all peers except the source.
// Preserves the original signature when forwarding.
func (f *Flooder) floodSleepCommand(
	fromPeer identity.AgentID,
	cmd *protocol.SleepCommand,
	seenBy []identity.AgentID,
) {
	fwdCmd := &protocol.SleepCommand{
		OriginAgent: cmd.OriginAgent,
		CommandID:   cmd.CommandID,
		Timestamp:   cmd.Timestamp,
		Signature:   cmd.Signature, // Preserve signature when forwarding
		SeenBy:      seenBy,
	}

	frame := &protocol.Frame{
		Type:     protocol.FrameSleepCommand,
		StreamID: protocol.ControlStreamID,
		Payload:  fwdCmd.Encode(),
	}

	f.floodFrame(fromPeer, seenBy, frame, "failed to forward sleep command")
}

// floodWakeCommand forwards a wake command to all peers except the source.
// Preserves the original signature when forwarding.
func (f *Flooder) floodWakeCommand(
	fromPeer identity.AgentID,
	cmd *protocol.WakeCommand,
	seenBy []identity.AgentID,
) {
	fwdCmd := &protocol.WakeCommand{
		OriginAgent: cmd.OriginAgent,
		CommandID:   cmd.CommandID,
		Timestamp:   cmd.Timestamp,
		Signature:   cmd.Signature, // Preserve signature when forwarding
		SeenBy:      seenBy,
	}

	frame := &protocol.Frame{
		Type:     protocol.FrameWakeCommand,
		StreamID: protocol.ControlStreamID,
		Payload:  fwdCmd.Encode(),
	}

	f.floodFrame(fromPeer, seenBy, frame, "failed to forward wake command")
}

// SleepCommandSeenCacheSize returns the current size of the sleep command seen cache.
func (f *Flooder) SleepCommandSeenCacheSize() int {
	f.sleepCmdMu.RLock()
	defer f.sleepCmdMu.RUnlock()
	return len(f.sleepCmdSeenCache)
}

// storePendingWake stores a wake command to forward to new peers.
func (f *Flooder) storePendingWake(cmd *protocol.WakeCommand) {
	f.pendingWakeMu.Lock()
	defer f.pendingWakeMu.Unlock()
	f.pendingWakeCmd = cmd
	f.pendingWakeAt = time.Now()
}

// ClearPendingWake clears any stored pending wake command.
func (f *Flooder) ClearPendingWake() {
	f.pendingWakeMu.Lock()
	defer f.pendingWakeMu.Unlock()
	f.pendingWakeCmd = nil
	f.pendingWakeAt = time.Time{}
}

// OnPeerConnected forwards any pending wake command to a newly connected peer.
// This should be called when a peer connection is established.
func (f *Flooder) OnPeerConnected(peerID identity.AgentID) {
	f.pendingWakeMu.RLock()
	cmd := f.pendingWakeCmd
	storedAt := f.pendingWakeAt
	f.pendingWakeMu.RUnlock()

	// No pending wake command
	if cmd == nil {
		return
	}

	// Expire pending wake after SeenCacheTTL (default 5 minutes)
	if time.Since(storedAt) > f.cfg.SeenCacheTTL {
		f.ClearPendingWake()
		return
	}

	// Don't send to the origin agent
	if peerID == cmd.OriginAgent {
		return
	}

	f.logger.Debug("forwarding pending wake to new peer",
		"origin", cmd.OriginAgent.ShortString(),
		"command_id", cmd.CommandID,
		"peer", peerID.ShortString(),
		"signed", !cmd.IsZeroSignature())

	// Create wake command with our local ID in SeenBy, preserving signature
	fwdCmd := &protocol.WakeCommand{
		OriginAgent: cmd.OriginAgent,
		CommandID:   cmd.CommandID,
		Timestamp:   cmd.Timestamp,
		Signature:   cmd.Signature, // Preserve signature when forwarding
		SeenBy:      []identity.AgentID{f.localID},
	}

	frame := &protocol.Frame{
		Type:     protocol.FrameWakeCommand,
		StreamID: protocol.ControlStreamID,
		Payload:  fwdCmd.Encode(),
	}

	if err := f.sender.SendToPeer(peerID, frame); err != nil {
		f.logger.Debug("failed to forward pending wake to new peer",
			"peer", peerID.ShortString(),
			logging.KeyError, err)
	}
}
