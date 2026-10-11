// Package agent implements the main agent orchestration for Muti Metroo.
package agent

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/postalsys/muti-metroo/internal/certutil"
	"github.com/postalsys/muti-metroo/internal/config"
	"github.com/postalsys/muti-metroo/internal/crypto"
	"github.com/postalsys/muti-metroo/internal/dynconfig"
	"github.com/postalsys/muti-metroo/internal/exit"
	"github.com/postalsys/muti-metroo/internal/expose"
	"github.com/postalsys/muti-metroo/internal/filetransfer"
	"github.com/postalsys/muti-metroo/internal/flood"
	"github.com/postalsys/muti-metroo/internal/forward"
	"github.com/postalsys/muti-metroo/internal/health"
	"github.com/postalsys/muti-metroo/internal/hosts"
	"github.com/postalsys/muti-metroo/internal/icmp"
	"github.com/postalsys/muti-metroo/internal/identity"
	"github.com/postalsys/muti-metroo/internal/logging"
	"github.com/postalsys/muti-metroo/internal/peer"
	"github.com/postalsys/muti-metroo/internal/probe"
	"github.com/postalsys/muti-metroo/internal/protocol"
	"github.com/postalsys/muti-metroo/internal/recovery"
	"github.com/postalsys/muti-metroo/internal/routing"
	"github.com/postalsys/muti-metroo/internal/shell"
	"github.com/postalsys/muti-metroo/internal/sleep"
	"github.com/postalsys/muti-metroo/internal/socks5"
	"github.com/postalsys/muti-metroo/internal/stream"
	"github.com/postalsys/muti-metroo/internal/sysinfo"
	"github.com/postalsys/muti-metroo/internal/transport"
	"github.com/postalsys/muti-metroo/internal/udp"
)

// directDialTimeout is the timeout for direct TCP connections (no mesh route).
// Using 10 seconds as it's long enough for real connections but short enough
// for unreachable addresses to fail quickly.
const directDialTimeout = 10 * time.Second

// ErrInterrupted is returned by Start() when the agent is stopped during
// the startup delay (via Stop() or signal).
var ErrInterrupted = errors.New("agent startup interrupted")

// fileTransferStream tracks an active file transfer stream.
type fileTransferStream struct {
	StreamID     uint64
	PeerID       identity.AgentID
	RequestID    uint64
	IsUpload     bool // true for upload (receiving data), false for download (sending data)
	Meta         *filetransfer.TransferMetadata
	MetaReceived bool // true after metadata frame received
	Closed       bool
	// Streaming upload fields (write directly to disk instead of buffering)
	TempFile     *os.File // Temp file for streaming upload data
	BytesWritten int64    // Bytes written to temp file
	// E2E encryption
	sessionKey *crypto.SessionKey // E2E encryption session key
}

// pendingControlRequest tracks an outbound control request awaiting response.
// accumulated holds already-received chunks when the peer is sending a
// multi-frame response; the final delivery stitches them together.
type pendingControlRequest struct {
	RequestID   uint64
	NextHop     identity.AgentID // Peer we sent it to; only they may answer
	ControlType uint8
	ResponseCh  chan *protocol.ControlResponse
	Timeout     time.Time
	accumulated []byte
}

// forwardedControlRequest tracks a request we forwarded so we can route the
// response back. DownstreamID is minted locally and is what forwardedControl is
// keyed by: the upstream peer chose UpstreamID from its own counter, and every
// peer's counter starts at 1, so two peers routing through this agent would
// otherwise collide on the key in ordinary operation and the later request
// would evict the earlier. This mirrors relayEntry, which allocates a
// downstream stream ID for exactly the same reason.
type forwardedControlRequest struct {
	UpstreamID   uint64           // Request ID the upstream peer chose
	DownstreamID uint64           // Request ID we chose, sent downstream
	SourcePeer   identity.AgentID // Peer who sent us the request
	NextHop      identity.AgentID // Peer we forwarded it to; only they may answer
	CreatedAt    time.Time
}

// Agent is the main Muti Metroo agent that orchestrates all components.
type Agent struct {
	cfg     *config.Config
	id      identity.AgentID
	keypair *identity.Keypair // X25519 keypair for E2E encryption
	dataDir string
	logger  *slog.Logger

	// Transport layer - supports QUIC, WebSocket, and HTTP/2.
	transports map[transport.TransportType]transport.Transport

	// listeners holds the durable spec + origin of every transport listener,
	// keyed by listen address. Config listeners are seeded at startup and cannot
	// be removed at runtime; dynamic listeners are managed via ManageListener.
	// liveListeners holds the transient live handles (rebuilt across sleep/wake)
	// guarded by listenersMu. See ManageListener for the dynamic path.
	listeners     *dynRegistry[config.ListenerConfig]
	listenersMu   sync.Mutex
	liveListeners map[string]*listenerHandle

	// Core components
	peerMgr       *peer.Manager
	routeMgr      *routing.Manager
	streamMgr     *stream.Manager
	flooder       *flood.Flooder
	socks5Srv     *socks5.Server
	exitHandler   *exit.Handler
	exitHandlerMu sync.Mutex // Guards on-demand exit handler creation
	healthServer  *health.Server
	// snapshotServer is a listener-less health.Server used only to build
	// dashboard snapshots for the SOCKS5 route-watch path when the HTTP API
	// is disabled. When HTTP is enabled, route-watch reuses healthServer.
	snapshotServer *health.Server
	sleepMgr       *sleep.Manager    // Sleep mode manager (nil if not enabled)
	sealedBox      *crypto.SealedBox // Management key encryption (nil if not configured)

	// File transfer (stream-based)
	fileStreamHandler *filetransfer.StreamHandler
	fileStreamsMu     sync.RWMutex
	fileStreams       map[streamKey]*fileTransferStream // (peer, StreamID) -> active transfer

	// Shell (stream-based)
	shellHandler       *shell.Handler
	shellClientMu      sync.RWMutex
	shellClientStreams map[streamKey]*health.ShellStreamAdapter // (peer, StreamID) -> active client session

	// UDP relay (for exit nodes)
	udpHandler *udp.Handler

	// UDP ingress association tracking (SOCKS5 client -> mesh).
	// Per-agent (not package-global) so multiple agents in the same process
	// do not share state -- this is essential for in-process integration tests
	// and removes a class of cross-talk bugs in any embedded use of the agent
	// package.
	udpIngressMu            sync.RWMutex
	udpIngressByBase        map[uint64]*udpIngressAssociation // SOCKS5 base stream ID -> ingress association
	udpIngressByLocalStream map[streamKey]*udpDestLookup      // (next hop, local stream ID) -> parent + dest
	udpNextBaseID           atomic.Uint64

	// udpRelay tracks UDP associations being relayed through this agent.
	// Per-agent for the same reason as the ingress maps above.
	udpRelay *relayTable

	// ICMP echo (for exit nodes)
	icmpHandler *icmp.Handler

	// icmpRelay tracks ICMP sessions being relayed through this agent.
	// Per-agent (not package-global) for the same reason as the UDP maps:
	// in-process embedded use of the agent package would otherwise share
	// state across instances.
	icmpRelay *relayTable

	// ICMP ingress tracking (SOCKS5 ICMP ECHO -> mesh).
	icmpIngressMu       sync.RWMutex
	icmpIngressByStream map[streamKey]*icmpIngressAssociation

	// icmpIngressByHandle indexes the same associations by the opaque handle
	// CreateICMPSession returns. The wire stream ID cannot serve as that
	// handle: it comes from a per-connection allocator, so two sessions routed
	// via different next hops collide on it. Handles come from
	// icmpNextHandle, which is agent-wide. Both indices are always written and
	// deleted together.
	icmpIngressByHandle map[uint64]*icmpIngressAssociation
	icmpNextHandle      atomic.Uint64

	// ICMP WebSocket session tracking (/agents/{id}/icmp).
	icmpWSSessionMu       sync.RWMutex
	icmpWSSessionByStream map[streamKey]*icmpWebSocketSession

	// icmpWSSessionByHandle mirrors icmpIngressByHandle for WebSocket sessions.
	icmpWSSessionByHandle map[uint64]*icmpWebSocketSession

	// Port forwarding
	forwardHandler          *forward.Handler
	exposeManager           *expose.Manager
	forwardListenersMu      sync.RWMutex
	forwardListeners        map[string]*forward.Listener // key -> listener (all)
	dynamicForwardListeners map[string]struct{}          // keys of dynamic-only
	configForwardListeners  map[string]struct{}          // keys of config-only

	// forwardEndpointsMu guards the config/dynamic endpoint sets. The actual
	// key -> target map lives inside forward.Handler (guarded there). We keep
	// the origin sets here so Manage calls can reject mutations to
	// config-sourced endpoints and so list output can flag each entry.
	forwardEndpointsMu        sync.RWMutex
	configForwardEndpoints    map[string]struct{} // keys declared in config.yaml
	dynamicForwardEndpoints   map[string]struct{} // keys added at runtime
	ephemeralForwardEndpoints map[string]string   // key -> owner session id (expose session)

	// Dynamic peer tracking. Config-sourced addresses are seeded once from
	// cfg.Peers at startup; dynamic addresses are mutated by ManagePeer. Live
	// connection state lives in peer.Manager, so the registry only needs
	// membership (value type struct{}). See also the listeners registry below.
	peers *dynRegistry[struct{}]

	// tcpRelay tracks TCP streams being relayed through this agent.
	tcpRelay *relayTable

	// Control request tracking
	controlMu        sync.RWMutex
	pendingControl   map[uint64]*pendingControlRequest   // Request ID -> pending request (for requests we initiated)
	forwardedControl map[uint64]*forwardedControlRequest // Our downstream request ID -> forwarded request
	nextControlID    uint64

	// Route advertisement trigger channel
	routeAdvertiseCh chan struct{}

	// Node info advertisement trigger channel
	nodeInfoAdvertiseCh chan struct{}

	// Wake signal channel for poll cycle synchronization
	wakeSignalMu sync.Mutex
	wakeSignal   chan struct{}
	pollCancel   context.CancelFunc // Cancel function for active poll context

	// Dynamic display name override (ephemeral, reverts on restart)
	dynamicDisplayName   string
	dynamicDisplayNameMu sync.RWMutex

	// Persistence for dynamic config changes (peers/forwards/routes/display-name).
	// nil when agent has no data_dir configured -- mutations then stay ephemeral
	// exactly as before. During ReplayDynamicConfig we set replayingDynamic so
	// the Manage* methods do not re-write what we just read.
	journal          *dynconfig.Journal
	replayingDynamic atomic.Bool

	// Mesh-host registry served to local Mutiauk clients via
	// /api/dashboard. Independent of routing/domain_routes; not
	// propagated across the mesh.
	meshHosts *hosts.Store

	// Probe implementation. Defaulted to probe.Probe in New(); tests override
	// it to exercise cancellation and other corner cases without dialing.
	probeFunc probe.Func

	// State
	running         atomic.Bool
	stopOnce        sync.Once
	stopCh          chan struct{}
	shutdownReqCh   chan struct{}
	shutdownReqOnce sync.Once
	wg              sync.WaitGroup
}

// New creates a new agent with the given configuration.
func New(cfg *config.Config) (*Agent, error) {
	// Load or create agent identity
	var agentID identity.AgentID
	var err error

	// If config specifies an explicit ID (not "auto"), use it
	if cfg.Agent.ID != "" && cfg.Agent.ID != "auto" {
		agentID, err = identity.ParseAgentID(cfg.Agent.ID)
		if err != nil {
			return nil, fmt.Errorf("parse agent ID from config: %w", err)
		}
		// Only store to data_dir if data_dir is configured
		if cfg.Agent.DataDir != "" {
			if err := agentID.Store(cfg.Agent.DataDir); err != nil {
				return nil, fmt.Errorf("store agent ID: %w", err)
			}
		}
	} else {
		// Auto-generate or load from data directory
		// Note: Config validation ensures data_dir is set when ID is "auto"
		agentID, _, err = identity.LoadOrCreate(cfg.Agent.DataDir)
		if err != nil {
			return nil, fmt.Errorf("load identity: %w", err)
		}
	}

	// Load X25519 keypair for E2E encryption
	// Check config first (enables single-file deployment), then fall back to data_dir
	var keypair *identity.Keypair
	if cfg.Agent.HasIdentityKeypair() {
		// Load from config
		keypair, err = identity.KeypairFromConfig(cfg.Agent.PrivateKey, cfg.Agent.PublicKey)
		if err != nil {
			return nil, fmt.Errorf("load keypair from config: %w", err)
		}
	} else {
		// Fall back to data_dir (existing behavior)
		// Note: Config validation ensures data_dir is set when private_key is not configured
		keypair, _, err = identity.LoadOrCreateKeypair(cfg.Agent.DataDir)
		if err != nil {
			return nil, fmt.Errorf("load keypair: %w", err)
		}
	}

	// Initialize logger
	logger := logging.NewLogger(cfg.Agent.LogLevel, cfg.Agent.LogFormat)

	a := &Agent{
		cfg:                       cfg,
		id:                        agentID,
		keypair:                   keypair,
		dataDir:                   cfg.Agent.DataDir,
		logger:                    logger,
		stopCh:                    make(chan struct{}),
		shutdownReqCh:             make(chan struct{}),
		routeAdvertiseCh:          make(chan struct{}, 1), // Buffered to avoid blocking
		nodeInfoAdvertiseCh:       make(chan struct{}, 1), // Buffered to avoid blocking
		forwardListeners:          make(map[string]*forward.Listener),
		dynamicForwardListeners:   make(map[string]struct{}),
		configForwardListeners:    make(map[string]struct{}),
		configForwardEndpoints:    make(map[string]struct{}),
		dynamicForwardEndpoints:   make(map[string]struct{}),
		ephemeralForwardEndpoints: make(map[string]string),
		peers:                     newDynRegistry[struct{}](),
		listeners:                 newDynRegistry[config.ListenerConfig](),
		liveListeners:             make(map[string]*listenerHandle),
		tcpRelay:                  newRelayTable(),
		udpRelay:                  newRelayTable(),
		icmpRelay:                 newRelayTable(),
		pendingControl:            make(map[uint64]*pendingControlRequest),
		forwardedControl:          make(map[uint64]*forwardedControlRequest),
		fileStreams:               make(map[streamKey]*fileTransferStream),
		shellClientStreams:        make(map[streamKey]*health.ShellStreamAdapter),
		udpIngressByBase:          make(map[uint64]*udpIngressAssociation),
		udpIngressByLocalStream:   make(map[streamKey]*udpDestLookup),
		icmpIngressByStream:       make(map[streamKey]*icmpIngressAssociation),
		icmpIngressByHandle:       make(map[uint64]*icmpIngressAssociation),
		icmpWSSessionByStream:     make(map[streamKey]*icmpWebSocketSession),
		icmpWSSessionByHandle:     make(map[uint64]*icmpWebSocketSession),
		probeFunc:                 probe.Probe,
		journal:                   dynconfig.NewJournal(cfg.Agent.DataDir),
	}

	// Mesh-host registry. Config has already been validated, so this
	// only fails on a programmer error -- propagate it anyway.
	a.meshHosts, err = hosts.New(cfg.MeshHostsSuffixes, cfg.MeshHosts)
	if err != nil {
		return nil, fmt.Errorf("init mesh hosts: %w", err)
	}

	// Initialize components
	if err := a.initComponents(); err != nil {
		return nil, err
	}

	return a, nil
}

// initComponents initializes all agent components.
func (a *Agent) initComponents() error {
	// Initialize all transports
	a.transports = make(map[transport.TransportType]transport.Transport)
	a.transports[transport.TransportQUIC] = transport.NewQUICTransport()
	a.transports[transport.TransportWebSocket] = transport.NewWebSocketTransport()
	a.transports[transport.TransportHTTP2] = transport.NewH2Transport()

	// Initialize routing manager
	a.routeMgr = routing.NewManager(a.id)

	// Initialize stream manager
	streamCfg := stream.ManagerConfig{
		MaxStreamsPerPeer: a.cfg.Limits.MaxStreamsPerPeer,
		MaxStreamsTotal:   a.cfg.Limits.MaxStreamsTotal,
		BufferSize:        a.cfg.Limits.BufferSize,
		IdleTimeout:       a.cfg.Connections.IdleThreshold,
	}
	a.streamMgr = stream.NewManager(streamCfg, a.id)

	// Initialize peer manager with default QUIC transport
	// Other transports are used via ConnectWithTransport()
	peerCfg := peer.DefaultManagerConfig(a.id, a.transports[transport.TransportQUIC])
	peerCfg.DisplayName = a.cfg.Agent.DisplayName
	peerCfg.KeepaliveInterval = a.cfg.Connections.IdleThreshold
	peerCfg.KeepaliveTimeout = a.cfg.Connections.Timeout
	peerCfg.KeepaliveJitter = a.cfg.Connections.KeepaliveJitter
	peerCfg.WriteTimeout = a.cfg.Connections.WriteTimeout
	peerCfg.Logger = a.logger
	peerCfg.ReconnectConfig = peer.ReconnectConfig{
		InitialDelay: a.cfg.Connections.Reconnect.InitialDelay,
		MaxDelay:     a.cfg.Connections.Reconnect.MaxDelay,
		Multiplier:   a.cfg.Connections.Reconnect.Multiplier,
		Jitter:       a.cfg.Connections.Reconnect.Jitter,
		MaxAttempts:  a.cfg.Connections.Reconnect.MaxRetries,
	}
	peerCfg.OnPeerDisconnect = a.handlePeerDisconnect
	peerCfg.OnPeerConnected = a.handlePeerConnected
	a.peerMgr = peer.NewManager(peerCfg)

	// Initialize management key encryption (sealed box) if configured
	if a.cfg.HasManagementKey() {
		pubKey, err := a.cfg.GetManagementPublicKey()
		if err != nil {
			return fmt.Errorf("get management public key: %w", err)
		}
		if a.cfg.CanDecryptManagement() {
			privKey, err := a.cfg.GetManagementPrivateKey()
			if err != nil {
				return fmt.Errorf("get management private key: %w", err)
			}
			a.sealedBox = crypto.NewSealedBoxWithPrivate(pubKey, privKey)
			a.logger.Info("management key encryption enabled (can decrypt)")
		} else {
			a.sealedBox = crypto.NewSealedBox(pubKey)
			a.logger.Info("management key encryption enabled (encrypt only)")
		}
		// Pass sealed box to routing manager for decryption attempts
		a.routeMgr.SetSealedBox(a.sealedBox)
	}

	// Initialize flooder (needs peer manager for sending)
	floodCfg := flood.DefaultFloodConfig()
	floodCfg.LocalDisplayName = a.cfg.Agent.DisplayName
	floodCfg.Logger = a.logger
	floodCfg.SealedBox = a.sealedBox // Pass sealed box for encryption

	// Configure command signing verification if signing public key is set
	if a.cfg.HasSigningKey() {
		signingPubKey, err := a.cfg.GetSigningPublicKey()
		if err != nil {
			return fmt.Errorf("get signing public key: %w", err)
		}
		floodCfg.SigningPublicKey = &signingPubKey
		a.logger.Info("command signing verification enabled")
	}

	a.flooder = flood.NewFlooder(floodCfg, a.id, a.routeMgr, a.peerMgr)

	// Initialize SOCKS5 server if enabled
	if a.cfg.SOCKS5.Enabled {
		auths := a.buildSOCKS5Auth()
		socksCfg := socks5.ServerConfig{
			Address:        a.cfg.SOCKS5.Address,
			MaxConnections: a.cfg.SOCKS5.MaxConnections,
			ConnectTimeout: 30 * time.Second,
			IdleTimeout:    a.cfg.Connections.IdleThreshold,
			Authenticators: auths,
			Dialer:         a, // Agent implements socks5.Dialer
		}
		a.socks5Srv = socks5.NewServer(socksCfg)
	}

	// Initialize exit handler if enabled
	if a.cfg.Exit.Enabled {
		routes := make([]exit.AllowedRoute, 0, len(a.cfg.Exit.Routes))
		for _, r := range a.cfg.Exit.Routes {
			_, ipNet, err := net.ParseCIDR(r.CIDR)
			if err != nil {
				return fmt.Errorf("parse exit route %q: %w", r.CIDR, err)
			}
			ar := exit.AllowedRoute{Network: ipNet, Interface: r.Interface}
			if r.SourceIP != "" {
				ar.Source = net.ParseIP(r.SourceIP)
				if ar.Source == nil {
					return fmt.Errorf("exit route %s: invalid source_ip %q", r.CIDR, r.SourceIP)
				}
			}
			routes = append(routes, ar)
		}

		// Parse domain patterns for exit access control
		var domainPatterns []exit.DomainPattern
		for _, pattern := range a.cfg.Exit.DomainRoutes {
			isWildcard, baseDomain := routing.ParseDomainPattern(pattern)
			domainPatterns = append(domainPatterns, exit.DomainPattern{
				Pattern:    pattern,
				IsWildcard: isWildcard,
				BaseDomain: baseDomain,
			})
		}

		exitCfg := exit.HandlerConfig{
			AllowedRoutes:  routes,
			AllowedDomains: domainPatterns,
			ConnectTimeout: 30 * time.Second,
			IdleTimeout:    a.cfg.Connections.IdleThreshold,
			MaxConnections: a.cfg.Limits.MaxStreamsTotal,
			Logger:         a.logger,
			DNS: exit.DNSConfig{
				Servers: a.cfg.Exit.DNS.Servers,
				Timeout: a.cfg.Exit.DNS.Timeout,
			},
		}
		a.exitHandler = exit.NewHandler(exitCfg, a.id, nil)
	}

	// Add local CIDR routes
	for _, route := range a.cfg.Exit.Routes {
		network := routing.MustParseCIDR(route.CIDR)
		a.routeMgr.AddLocalRoute(network, route.Metric, route.Comment)
	}

	// Add local domain routes
	for _, pattern := range a.cfg.Exit.DomainRoutes {
		a.routeMgr.AddLocalDomainRoute(pattern, 0)
	}

	// Wire up exit handler with Agent as StreamWriter
	if a.exitHandler != nil {
		a.exitHandler.SetWriter(a)
	}

	// Initialize HTTP server if enabled
	if a.cfg.HTTP.Enabled {
		healthCfg := health.ServerConfig{
			Address:         a.cfg.HTTP.Address,
			ReadTimeout:     a.cfg.HTTP.ReadTimeout,
			WriteTimeout:    a.cfg.HTTP.WriteTimeout,
			TokenHash:       a.cfg.HTTP.TokenHash,
			EnablePprof:     a.cfg.HTTP.PprofEnabled(),
			EnableDashboard: a.cfg.HTTP.DashboardEnabled(),
			EnableRemoteAPI: a.cfg.HTTP.RemoteAPIEnabled(),
		}
		provider := &agentStatsProvider{agent: a}
		a.healthServer = health.NewServer(healthCfg, provider)
		a.healthServer.SetRemoteProvider(a)                // Enable remote status via control channel
		a.healthServer.SetRouteAdvertiseTrigger(a)         // Enable route advertisement trigger
		a.healthServer.SetSealedBox(a.sealedBox)           // Enable management key decrypt checks
		a.healthServer.SetShellProvider(a)                 // Enable management commands via HTTP API
		a.healthServer.SetICMPProvider(a)                  // Enable ICMP ping via HTTP API
		a.healthServer.SetSleepProvider(a)                 // Enable sleep mode via HTTP API
		a.healthServer.SetStopProvider(a)                  // Enable /shutdown graceful exit via HTTP API
		a.healthServer.SetRouteManageProvider(a)           // Enable dynamic route management via HTTP API
		a.healthServer.SetRouteTraceProvider(a)            // Enable /route/trace diagnostic via HTTP API
		a.healthServer.SetForwardManageProvider(a)         // Enable dynamic forward listener management via HTTP API
		a.healthServer.SetForwardEndpointManageProvider(a) // Enable dynamic forward endpoint management via HTTP API
		a.healthServer.SetFileBrowseProvider(a)            // Enable file browsing via HTTP API
		a.healthServer.SetDisplayNameManageProvider(a)     // Enable dynamic display name management via HTTP API
		a.healthServer.SetPeerManageProvider(a)            // Enable dynamic peer management via HTTP API
		a.healthServer.SetListenerManageProvider(a)        // Enable dynamic transport listener management via HTTP API
		a.healthServer.SetMeshHostProvider(a)              // Enable mesh-host registry via HTTP API + dashboard
		a.healthServer.SetMgmtAuthProvider(a)              // Provide management-key signing for mutation endpoints
		a.healthServer.SetSetupDefaultsProvider(a)         // Enable /api/setup-defaults for config wizards
	}

	// Initialize file transfer handler (stream-based). Authorization flows
	// through management-key signatures; a missing public key means inbound
	// requests are rejected outright.
	ftStreamCfg := filetransfer.StreamConfig{
		Enabled:      a.cfg.FileTransfer.Enabled,
		MaxFileSize:  a.cfg.FileTransfer.MaxFileSize,
		AllowedPaths: a.cfg.FileTransfer.AllowedPaths,
		Compression:  true, // Default to compression
	}
	a.fileStreamHandler = filetransfer.NewStreamHandler(ftStreamCfg, a.verifyManagementAuthFor(crypto.OpFile))

	// Initialize shell handler. Same model: verifier checks the management
	// signature on every inbound request, using the OpShell tag.
	shellCfg := shell.Config{
		Enabled:     a.cfg.Shell.Enabled,
		Whitelist:   a.cfg.Shell.Whitelist,
		Timeout:     a.cfg.Shell.Timeout,
		MaxSessions: a.cfg.Shell.MaxSessions,
	}
	shellExecutor := shell.NewExecutor(shellCfg)
	a.shellHandler = shell.NewHandler(shellExecutor, a, a.verifyManagementAuthFor(crypto.OpShell), a.logger)

	// Initialize UDP handler for exit nodes
	if a.cfg.UDP.Enabled {
		udpCfg := udp.Config{
			Enabled:         a.cfg.UDP.Enabled,
			MaxAssociations: a.cfg.UDP.MaxAssociations,
			IdleTimeout:     a.cfg.UDP.IdleTimeout,
			MaxDatagramSize: a.cfg.UDP.MaxDatagramSize,
		}
		a.udpHandler = udp.NewHandler(udpCfg, a, a.logger)
	}

	// Initialize ICMP handler for exit nodes
	if a.cfg.ICMP.Enabled {
		icmpCfg := icmp.Config{
			Enabled:     a.cfg.ICMP.Enabled,
			MaxSessions: a.cfg.ICMP.MaxSessions,
			IdleTimeout: a.cfg.ICMP.IdleTimeout,
			EchoTimeout: a.cfg.ICMP.EchoTimeout,
		}
		a.icmpHandler = icmp.NewHandler(icmpCfg, a, a.logger)
	}

	// Set UDP handler on SOCKS5 server (for ingress UDP ASSOCIATE)
	if a.socks5Srv != nil {
		a.socks5Srv.SetUDPHandler(a)
	}

	// Set ICMP handler on SOCKS5 server (for ingress ICMP ECHO)
	if a.socks5Srv != nil && a.icmpHandler != nil {
		a.socks5Srv.SetICMPHandler(a)
	}

	// Set route-watch handler on SOCKS5 server (dashboard snapshot push over
	// CmdRouteWatch / CmdSnapshot), letting Mutiauk clients receive route
	// updates over the same endpoint they use for the data plane.
	if a.socks5Srv != nil {
		a.socks5Srv.SetRouteWatchHandler(a.setupRouteWatch())
	}

	// Wire ephemeral forward endpoints over SOCKS5 CmdExpose, letting Muti Metroo
	// Entry clients register forward keys over the same endpoint they use for
	// the data plane. Authorization is the SOCKS5 handshake.
	a.exposeManager = expose.NewManager(a, expose.ManagerConfig{
		Logger: a.logger,
	})
	if a.socks5Srv != nil {
		a.socks5Srv.SetExposeHandler(a.exposeManager)
	}

	// Initialize forward exit handler unconditionally so runtime-added endpoints
	// have a live handler to register with. When no config endpoints exist the
	// handler starts empty; adds flow through ManageForwardEndpoint.
	endpoints := make([]forward.Endpoint, len(a.cfg.Forward.Endpoints))
	for i, ep := range a.cfg.Forward.Endpoints {
		endpoints[i] = forward.Endpoint{
			Key:    ep.Key,
			Target: ep.Target,
		}
	}
	handlerCfg := forward.HandlerConfig{
		Endpoints:      endpoints,
		ConnectTimeout: 30 * time.Second,
		IdleTimeout:    a.cfg.Connections.IdleThreshold,
		MaxConnections: a.cfg.Limits.MaxStreamsTotal,
		Logger:         a.logger,
	}
	a.forwardHandler = forward.NewHandler(handlerCfg, a.id, a)

	// Register local forward routes and track config-sourced keys so
	// ManageForwardEndpoint can refuse runtime mutations to them.
	for _, ep := range a.cfg.Forward.Endpoints {
		a.routeMgr.AddLocalForwardRoute(ep.Key, ep.Target, 0)
		a.configForwardEndpoints[ep.Key] = struct{}{}
	}

	// Initialize forward listeners
	for _, lisCfg := range a.cfg.Forward.Listeners {
		cfg := forward.ListenerConfig{
			Key:            lisCfg.Key,
			Address:        lisCfg.Address,
			MaxConnections: lisCfg.MaxConnections,
			Logger:         a.logger,
		}
		listener := forward.NewListener(cfg, a)
		a.forwardListeners[lisCfg.Key] = listener
		a.configForwardListeners[lisCfg.Key] = struct{}{}
	}

	return nil
}

// buildSOCKS5Auth builds SOCKS5 authenticators from config.
func (a *Agent) buildSOCKS5Auth() []socks5.Authenticator {
	if !a.cfg.SOCKS5.Auth.Enabled {
		return []socks5.Authenticator{&socks5.NoAuthAuthenticator{}}
	}

	// Separate plaintext and hashed credentials
	users := make(map[string]string)
	hashedUsers := make(map[string]string)

	for _, u := range a.cfg.SOCKS5.Auth.Users {
		if u.PasswordHash != "" {
			// Prefer password hash if available
			hashedUsers[u.Username] = u.PasswordHash
		} else if u.Password != "" {
			// Fall back to plaintext password (deprecated)
			users[u.Username] = u.Password
		}
	}

	return socks5.CreateAuthenticators(socks5.AuthConfig{
		Enabled:     true,
		Required:    true,
		Users:       users,
		HashedUsers: hashedUsers,
	})
}

// buildSOCKS5CredentialStore builds a credential store from SOCKS5 auth config.
// This is used for HTTP Basic Auth on the WebSocket SOCKS5 endpoint.
func (a *Agent) buildSOCKS5CredentialStore() socks5.CredentialStore {
	// Prefer hashed credentials if available
	hashedUsers := make(map[string]string)
	users := make(map[string]string)

	for _, u := range a.cfg.SOCKS5.Auth.Users {
		if u.PasswordHash != "" {
			hashedUsers[u.Username] = u.PasswordHash
		} else if u.Password != "" {
			users[u.Username] = u.Password
		}
	}

	// Return hashed credentials if available, otherwise plaintext
	if len(hashedUsers) > 0 {
		return socks5.HashedCredentials(hashedUsers)
	}
	return socks5.StaticCredentials(users)
}

// Start starts all agent components.
func (a *Agent) Start() error {
	if a.running.Load() {
		return fmt.Errorf("agent already running")
	}

	a.running.Store(true)

	a.logger.Info("starting agent",
		logging.KeyAgentID, a.id.ShortString(),
		logging.KeyComponent, "agent")

	// Set frame callback on peer manager
	a.peerMgr.SetFrameCallback(a.processFrame)

	// Start HTTP server early so health probes succeed during startup delay.
	// The health server only depends on components initialized in New().
	if a.healthServer != nil {
		if err := a.healthServer.Start(); err != nil {
			a.logger.Error("failed to start HTTP server",
				logging.KeyAddress, a.cfg.HTTP.Address,
				logging.KeyError, err)
			a.running.Store(false)
			return fmt.Errorf("start HTTP server: %w", err)
		}
		a.logger.Info("HTTP server started",
			logging.KeyAddress, a.healthServer.Address())

		// Critical-systems guardrail: warn loudly when the management/dashboard
		// HTTP API is bound to a non-loopback address without bearer-token
		// authentication. Without a token hash the auth middleware is not
		// installed at all, so every endpoint is open, mutations included: the
		// management signature only proves this agent signed the request, and
		// this agent signs whatever an unauthenticated caller asks for. The
		// signing key defends against forgery by other agents, not against a
		// caller that can simply reach this port.
		if a.cfg.HTTP.TokenHash == "" && isNonLoopbackBind(a.cfg.HTTP.Address) {
			msg := "HTTP API on " + a.cfg.HTTP.Address + " has no bearer token; every endpoint is unauthenticated on this network, including route/peer/forward mutations, file browse and transfer, management command shells, and shutdown. The management signing key does NOT protect these from an unauthenticated caller: this agent signs their requests with its own key. Set http.token_hash, or bind to loopback."
			a.logger.Warn(msg)
			fmt.Fprintln(os.Stderr, "WARNING: "+msg)
		}
	}

	// Delay startup if configured
	if a.cfg.Agent.StartupDelay > 0 {
		a.logger.Info("delaying startup", "delay", a.cfg.Agent.StartupDelay)
		select {
		case <-time.After(a.cfg.Agent.StartupDelay):
			a.logger.Info("startup delay elapsed, proceeding")
		case <-a.stopCh:
			a.running.Store(false)
			return ErrInterrupted
		}
	}

	// Start listeners. Record each in the registry as config-sourced so dynamic
	// listener management can reject attempts to remove or replace them, and so
	// sleep/wake and advertisement see the full desired set.
	for _, listenerCfg := range a.cfg.Listeners {
		a.listeners.SetConfig(listenerCfg.Address, listenerCfg)
		if err := a.startListener(listenerCfg); err != nil {
			a.logger.Error("failed to start listener",
				logging.KeyAddress, listenerCfg.Address,
				logging.KeyTransport, listenerCfg.Transport,
				logging.KeyError, err)
			a.running.Store(false)
			return fmt.Errorf("start listener %s: %w", listenerCfg.Address, err)
		}
		a.logger.Info("listener started",
			logging.KeyAddress, listenerCfg.Address,
			logging.KeyTransport, listenerCfg.Transport)
	}

	// Record config-file peer addresses so dynamic peer management can reject
	// attempts to remove or replace them.
	for _, peerCfg := range a.cfg.Peers {
		a.peers.SetConfig(peerCfg.Address, struct{}{})
	}

	// Connect to configured peers
	for _, peerCfg := range a.cfg.Peers {
		a.wg.Add(1)
		go a.connectToPeer(peerCfg)
	}

	// Start SOCKS5 server if enabled
	if a.socks5Srv != nil {
		if err := a.socks5Srv.Start(); err != nil {
			a.logger.Error("failed to start SOCKS5 server",
				logging.KeyAddress, a.cfg.SOCKS5.Address,
				logging.KeyError, err)
			a.running.Store(false)
			return fmt.Errorf("start socks5: %w", err)
		}
		a.logger.Info("SOCKS5 server started",
			logging.KeyAddress, a.cfg.SOCKS5.Address)

		// Start WebSocket SOCKS5 listener if enabled
		if a.cfg.SOCKS5.WebSocket.Enabled {
			wsCfg := socks5.WebSocketConfig{
				Address:   a.cfg.SOCKS5.WebSocket.Address,
				Path:      a.cfg.SOCKS5.WebSocket.Path,
				PlainText: a.cfg.SOCKS5.WebSocket.PlainText,
			}

			// Use same credentials as SOCKS5 for HTTP Basic Auth if auth is enabled
			if a.cfg.SOCKS5.Auth.Enabled {
				wsCfg.Credentials = a.buildSOCKS5CredentialStore()
			}

			// Load TLS config unless plaintext mode
			if !wsCfg.PlainText {
				tlsConfig, err := a.loadListenerTLSConfig(nil, false)
				if err != nil {
					a.logger.Error("failed to load TLS config for WebSocket SOCKS5",
						logging.KeyError, err)
					a.running.Store(false)
					return fmt.Errorf("load TLS config for WebSocket SOCKS5: %w", err)
				}
				wsCfg.TLSConfig = tlsConfig
			}

			if err := a.socks5Srv.StartWebSocket(wsCfg); err != nil {
				a.logger.Error("failed to start WebSocket SOCKS5 listener",
					logging.KeyAddress, a.cfg.SOCKS5.WebSocket.Address,
					logging.KeyError, err)
				a.running.Store(false)
				return fmt.Errorf("start WebSocket SOCKS5: %w", err)
			}
			a.logger.Info("WebSocket SOCKS5 listener started",
				logging.KeyAddress, a.cfg.SOCKS5.WebSocket.Address,
				"path", a.cfg.SOCKS5.WebSocket.Path,
				"plaintext", a.cfg.SOCKS5.WebSocket.PlainText,
				"auth", a.cfg.SOCKS5.Auth.Enabled)
		}

		// Start UDP destination association cleanup loop
		a.startUDPDestCleanupLoop()
	}

	// Start exit handler if enabled
	if a.exitHandler != nil {
		a.exitHandler.Start()
		a.logger.Info("exit handler started",
			logging.KeyCount, len(a.cfg.Exit.Routes),
			"domain_routes", len(a.cfg.Exit.DomainRoutes))
	}

	// Start forward handler if enabled
	if a.forwardHandler != nil {
		a.forwardHandler.Start()
		a.logger.Info("forward handler started",
			"endpoints", len(a.cfg.Forward.Endpoints))
	}

	// Start forward listeners
	a.forwardListenersMu.RLock()
	for _, listener := range a.forwardListeners {
		if err := listener.Start(); err != nil {
			a.forwardListenersMu.RUnlock()
			a.logger.Error("failed to start forward listener",
				"key", listener.Key(),
				logging.KeyError, err)
			a.running.Store(false)
			return fmt.Errorf("start forward listener %s: %w", listener.Key(), err)
		}
		a.logger.Info("forward listener started",
			"key", listener.Key(),
			"address", listener.Address().String())
	}
	a.forwardListenersMu.RUnlock()

	// Start route advertisement loop and announce initial routes
	a.wg.Add(1)
	go a.routeAdvertiseLoop()
	a.flooder.AnnounceLocalRoutes() // Initial announcement (always - agent presence route)

	// Start node info advertisement loop and announce initial node info
	// All nodes advertise their info (not just exit nodes)
	a.wg.Add(1)
	go a.nodeInfoAdvertiseLoop()
	// Initial node info announcement (with small delay for peer connections)
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		select {
		case <-time.After(2 * time.Second):
		case <-a.stopCh:
			return
		}
		info := sysinfo.Collect(a.displayNameForAdvertise(), a.getPeerConnectionInfo(), a.keypair.PublicKey, a.getUDPConfig(), a.getForwardConfig(), a.getFileTransferConfig(), a.getShellConfig(), a.getICMPConfig(), a.getAdvertisedListeners())
		a.flooder.AnnounceLocalNodeInfo(info)
		a.logger.Debug("initial node info advertisement sent",
			"display_name", info.DisplayName,
			"hostname", info.Hostname,
			"peers", len(info.Peers))
	}()

	// Initialize sleep manager if enabled
	if a.cfg.Sleep.Enabled {
		a.sleepMgr = sleep.NewManager(
			a.cfg.Sleep,
			a.dataDir,
			a.logger.With(logging.KeyComponent, "sleep"),
		)
		a.sleepMgr.SetLocalID(a.id) // Set local ID for deterministic window calculations
		a.sleepMgr.SetCallbacks(sleep.Callbacks{
			OnSleep: a.enterSleep,
			OnWake:  a.exitSleep,
			OnPoll:  a.doPoll,
		})
		if err := a.sleepMgr.LoadState(); err != nil {
			a.logger.Warn("failed to load sleep state",
				logging.KeyError, err)
		}
		// Start in sleep mode if configured or previously sleeping
		if a.cfg.Sleep.AutoSleepOnStart || a.sleepMgr.GetState() == sleep.StateSleeping {
			a.logger.Info("starting in sleep mode")
			if err := a.sleepMgr.Sleep(); err != nil {
				a.logger.Warn("failed to enter sleep mode on start",
					logging.KeyError, err)
			}
		}
		a.logger.Info("sleep manager initialized",
			"poll_interval", a.cfg.Sleep.PollInterval,
			"poll_jitter", a.cfg.Sleep.PollIntervalJitter)
	}

	a.logger.Info("agent started",
		logging.KeyAgentID, a.id.ShortString(),
		"peers", len(a.cfg.Peers),
		"listeners", len(a.cfg.Listeners))

	return nil
}

// listenerHandle bundles a running transport listener with the config it was
// started from and a per-listener stop channel. Closing stopCh tears down just
// this listener's accept loop (used by ManageListener remove), independent of
// the agent-wide stopCh.
type listenerHandle struct {
	cfg      config.ListenerConfig
	listener transport.Listener
	stopCh   chan struct{}
}

// startListener starts a listener for the given configuration, registers its
// live handle in liveListeners (keyed by configured address), and launches its
// accept loop. It does not touch the listeners registry -- callers decide the
// config-vs-dynamic origin via SetConfig/PutDynamic.
func (a *Agent) startListener(cfg config.ListenerConfig) error {
	// For plaintext WebSocket listeners (reverse proxy mode), skip TLS
	var tlsConfig *tls.Config
	if cfg.PlainText {
		a.logger.Warn("starting plaintext WebSocket listener (no TLS)",
			"address", cfg.Address,
			"path", cfg.Path,
			"warning", "only use behind trusted reverse proxy")
	} else {
		// Determine effective mTLS setting (per-listener override or global)
		enableMTLS := a.cfg.TLS.MTLS
		if cfg.TLS.MTLS != nil {
			enableMTLS = *cfg.TLS.MTLS
		}

		// Load TLS config with mTLS support
		var err error
		tlsConfig, err = a.loadListenerTLSConfig(&cfg.TLS, enableMTLS)
		if err != nil {
			return fmt.Errorf("load TLS config: %w", err)
		}
	}

	// Select the appropriate transport based on config
	transportType := transport.TransportType(cfg.Transport)
	if transportType == "" {
		transportType = transport.TransportQUIC // Default to QUIC
	}

	tr, ok := a.transports[transportType]
	if !ok {
		return fmt.Errorf("unsupported transport type: %s", transportType)
	}

	// Start the listener with protocol identifiers from config
	listener, err := tr.Listen(cfg.Address, transport.ListenOptions{
		TLSConfig:     tlsConfig,
		Path:          cfg.Path, // Used by WebSocket and HTTP/2
		MaxStreams:    a.cfg.Limits.MaxStreamsTotal,
		PlainText:     cfg.PlainText,
		ALPNProtocol:  a.cfg.Protocol.ALPN,
		HTTPHeader:    a.cfg.Protocol.HTTPHeader,
		WSSubprotocol: a.cfg.Protocol.WSSubprotocol,
		TCPKeepAlive:  a.cfg.Connections.TCPKeepAlive,
	})
	if err != nil {
		return err
	}

	h := &listenerHandle{
		cfg:      cfg,
		listener: listener,
		stopCh:   make(chan struct{}),
	}

	a.listenersMu.Lock()
	a.liveListeners[cfg.Address] = h
	a.listenersMu.Unlock()

	// Start accept loop
	a.wg.Add(1)
	go a.acceptLoop(h)

	return nil
}

// stopListener tears down a single live listener: closes its per-listener stop
// channel so the accept loop exits cleanly, closes the underlying transport
// listener, and removes it from liveListeners.
func (a *Agent) stopListener(h *listenerHandle) {
	close(h.stopCh)
	h.listener.Close()
	a.listenersMu.Lock()
	if a.liveListeners[h.cfg.Address] == h {
		delete(a.liveListeners, h.cfg.Address)
	}
	a.listenersMu.Unlock()
}

// stopLiveListener stops and deregisters the live listener bound at addr, if
// one is running. Safe to call for an address with no live listener.
func (a *Agent) stopLiveListener(addr string) {
	a.listenersMu.Lock()
	h := a.liveListeners[addr]
	a.listenersMu.Unlock()
	if h != nil {
		a.stopListener(h)
	}
}

// startListenerWithRetry attempts to start a listener with retries.
// This is useful when waking from sleep mode, as UDP sockets may not be
// immediately released after closing poll listeners.
func (a *Agent) startListenerWithRetry(cfg config.ListenerConfig, maxRetries int, retryDelay time.Duration) error {
	var lastErr error
	for i := 0; i < maxRetries; i++ {
		lastErr = a.startListener(cfg)
		if lastErr == nil {
			return nil
		}
		// Only retry for "address already in use" errors
		if !strings.Contains(lastErr.Error(), "address already in use") {
			return lastErr
		}
		if i < maxRetries-1 {
			a.logger.Debug("listener start retry",
				logging.KeyAddress, cfg.Address,
				"attempt", i+1,
				"max_retries", maxRetries,
				logging.KeyError, lastErr)
			time.Sleep(retryDelay)
		}
	}
	return lastErr
}

// loadListenerTLSConfig loads TLS configuration for a listener.
// Uses per-listener override if available, otherwise falls back to global config.
// If enableMTLS is true, client certificate verification is enabled.
func (a *Agent) loadListenerTLSConfig(override *config.TLSConfig, enableMTLS bool) (*tls.Config, error) {
	var cert tls.Certificate
	var certPEM, keyPEM []byte
	var err error

	// Get effective cert/key (per-listener override or global)
	certPEM, err = a.cfg.GetEffectiveCertPEM(override)
	if err != nil {
		return nil, fmt.Errorf("load certificate: %w", err)
	}
	keyPEM, err = a.cfg.GetEffectiveKeyPEM(override)
	if err != nil {
		return nil, fmt.Errorf("load private key: %w", err)
	}

	if certPEM != nil && keyPEM != nil {
		// Validate EC-only certificates
		if err := certutil.ValidateECKeyPair(certPEM, keyPEM); err != nil {
			return nil, fmt.Errorf("EC validation failed: %w", err)
		}

		cert, err = tls.X509KeyPair(certPEM, keyPEM)
		if err != nil {
			return nil, fmt.Errorf("parse certificate: %w", err)
		}
	} else {
		// Generate self-signed EC cert for development
		certPEM, keyPEM, err = transport.GenerateSelfSignedCert(a.id.ShortString(), 365*24*time.Hour)
		if err != nil {
			return nil, fmt.Errorf("generate self-signed cert: %w", err)
		}
		cert, err = tls.X509KeyPair(certPEM, keyPEM)
		if err != nil {
			return nil, fmt.Errorf("parse generated cert: %w", err)
		}
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{transport.ALPNProtocol},
		MinVersion:   tls.VersionTLS13,
	}

	// Set up mTLS if enabled
	if enableMTLS {
		// Load CA certificate for client verification (always from global config)
		caPEM, err := a.cfg.TLS.GetCAPEM()
		if err != nil {
			return nil, fmt.Errorf("load CA certificate: %w", err)
		}
		if caPEM == nil {
			return nil, fmt.Errorf("tls.ca is required when mTLS is enabled")
		}

		// Validate CA is EC
		if err := certutil.ValidateECCertificate(caPEM); err != nil {
			return nil, fmt.Errorf("CA EC validation failed: %w", err)
		}

		certPool := x509.NewCertPool()
		if !certPool.AppendCertsFromPEM(caPEM) {
			return nil, fmt.Errorf("failed to parse CA certificate")
		}

		tlsConfig.ClientCAs = certPool
		tlsConfig.ClientAuth = tls.RequireAndVerifyClientCert
	}

	return tlsConfig, nil
}

// acceptLoop accepts incoming connections from a listener. It exits when either
// the agent-wide stopCh or the listener's own stopCh closes -- the latter lets a
// single listener be removed at runtime (ManageListener) without spinning on
// Accept errors from its closed socket.
func (a *Agent) acceptLoop(h *listenerHandle) {
	defer a.wg.Done()
	defer recovery.RecoverWithLog(a.logger, "acceptLoop")

	listener := h.listener
	for {
		select {
		case <-a.stopCh:
			return
		case <-h.stopCh:
			return
		default:
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		peerConn, err := listener.Accept(ctx)
		cancel()

		if err != nil {
			select {
			case <-a.stopCh:
				return
			case <-h.stopCh:
				return
			default:
				// Log error and continue
				a.logger.Debug("accept error",
					logging.KeyLocalAddr, listener.Addr(),
					logging.KeyError, err)
				continue
			}
		}

		// Handle the connection in a goroutine
		a.wg.Add(1)
		go a.handleIncomingConnection(peerConn)
	}
}

// handleIncomingConnection processes an incoming peer connection.
func (a *Agent) handleIncomingConnection(peerConn transport.PeerConn) {
	defer a.wg.Done()
	defer recovery.RecoverWithLog(a.logger, "handleIncomingConnection")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	conn, err := a.peerMgr.Accept(ctx, peerConn)
	if err != nil {
		a.logger.Debug("failed to accept peer connection",
			logging.KeyError, err)
		peerConn.Close()
		return
	}

	a.logger.Info("peer connected",
		logging.KeyPeerID, conn.RemoteID.ShortString(),
		logging.KeyRemoteAddr, conn.RemoteAddr())

	// Note: SendFullTable / SendNodeInfoToNewPeer are now triggered from
	// handlePeerConnected (peer manager OnPeerConnected callback), which
	// fires for both this accept path and the ReconnectAll path used after
	// sleep/wake.
}

// connectToPeer initiates a connection to a configured peer.
func (a *Agent) connectToPeer(cfg config.PeerConfig) {
	defer a.wg.Done()
	defer recovery.RecoverWithLog(a.logger, "connectToPeer")

	a.logger.Debug("connecting to peer",
		logging.KeyAddress, cfg.Address,
		logging.KeyTransport, cfg.Transport)

	// Parse expected peer ID (if specified)
	var expectedID identity.AgentID
	if cfg.ID != "" && cfg.ID != "auto" {
		var err error
		expectedID, err = identity.ParseAgentID(cfg.ID)
		if err != nil {
			a.logger.Error("failed to parse peer ID",
				logging.KeyPeerID, cfg.ID,
				logging.KeyError, err)
			return
		}
	}
	// If ID is "auto" or empty, expectedID will be zero and peer manager will accept any ID

	// Determine if this is a WebSocket connection through a proxy
	// In this case, the external server might use RSA, so skip EC validation for CA
	isProxiedWS := cfg.Transport == "ws" && cfg.Proxy != ""

	// Determine ALPN protocol to use
	alpn := a.cfg.Protocol.ALPN
	if alpn == "" {
		alpn = transport.DefaultALPNProtocol
	}

	// Determine effective strict TLS setting (per-peer override or global)
	strictVerify := a.cfg.GetEffectiveStrict(&cfg.TLS)

	// Build DialOptions from peer config with protocol identifiers
	dialOpts := &transport.DialOptions{
		StrictVerify:      strictVerify,
		Timeout:           a.cfg.Connections.Timeout,
		ALPNProtocol:      a.cfg.Protocol.ALPN,
		HTTPHeader:        a.cfg.Protocol.HTTPHeader,
		WSSubprotocol:     a.cfg.Protocol.WSSubprotocol,
		FingerprintPreset: a.cfg.TLS.Fingerprint.Preset,
		TCPKeepAlive:      a.cfg.GetEffectiveTCPKeepAlive(&cfg),
	}

	// Build TLS config for peer connection
	// Default (strictVerify=false) skips verification, which is safe because
	// the E2E encryption layer provides security
	tlsConfig := &tls.Config{
		MinVersion:         tls.VersionTLS13,
		NextProtos:         []string{alpn},
		InsecureSkipVerify: !strictVerify,
	}

	// Load CA certificate for peer verification (per-peer override or global)
	caPEM, err := a.cfg.GetEffectiveCAPEM(&cfg.TLS)
	if err != nil {
		a.logger.Error("failed to load peer CA certificate",
			logging.KeyPeerID, cfg.ID,
			logging.KeyError, err)
		return
	}
	if caPEM != nil {
		// Validate EC-only for CA (skip for proxied WebSocket - external server may use RSA)
		// Also validate when strict mode is enabled (otherwise we're not verifying anyway)
		if !isProxiedWS && strictVerify {
			if err := certutil.ValidateECCertificate(caPEM); err != nil {
				a.logger.Error("CA EC validation failed",
					logging.KeyPeerID, cfg.ID,
					logging.KeyError, err)
				return
			}
		}

		certPool := x509.NewCertPool()
		if !certPool.AppendCertsFromPEM(caPEM) {
			a.logger.Error("failed to parse peer CA certificate",
				logging.KeyPeerID, cfg.ID)
			return
		}
		tlsConfig.RootCAs = certPool
	}

	// Load client certificate for mTLS (per-peer override or global)
	certPEM, err := a.cfg.GetEffectiveCertPEM(&cfg.TLS)
	if err != nil {
		a.logger.Error("failed to load peer client certificate",
			logging.KeyPeerID, cfg.ID,
			logging.KeyError, err)
		return
	}
	keyPEM, err := a.cfg.GetEffectiveKeyPEM(&cfg.TLS)
	if err != nil {
		a.logger.Error("failed to load peer client key",
			logging.KeyPeerID, cfg.ID,
			logging.KeyError, err)
		return
	}

	if certPEM != nil && keyPEM != nil {
		// Validate EC-only for client cert (always required for our certs)
		if err := certutil.ValidateECKeyPair(certPEM, keyPEM); err != nil {
			a.logger.Error("client cert EC validation failed",
				logging.KeyPeerID, cfg.ID,
				logging.KeyError, err)
			return
		}

		cert, err := tls.X509KeyPair(certPEM, keyPEM)
		if err != nil {
			a.logger.Error("failed to parse peer client certificate",
				logging.KeyPeerID, cfg.ID,
				logging.KeyError, err)
			return
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}

	dialOpts.TLSConfig = tlsConfig

	// Select the appropriate transport based on config
	transportType := transport.TransportType(cfg.Transport)
	if transportType == "" {
		transportType = transport.TransportQUIC // Default to QUIC
	}

	// Get transport for this peer (needed for reconnection)
	var peerTransport transport.Transport
	if transportType != transport.TransportQUIC {
		tr, ok := a.transports[transportType]
		if !ok {
			a.logger.Error("unsupported transport type",
				logging.KeyTransport, transportType)
			return
		}
		peerTransport = tr
	}

	// Add peer info to manager (including transport for reconnection)
	a.peerMgr.AddPeer(peer.PeerInfo{
		Address:     cfg.Address,
		ExpectedID:  expectedID,
		Persistent:  true,
		DialOptions: dialOpts,
		Transport:   peerTransport,
	})

	// Attempt connection
	ctx, cancel := context.WithTimeout(context.Background(), a.cfg.Connections.Timeout)
	defer cancel()

	var conn *peer.Connection

	if peerTransport == nil {
		// Use default Connect for QUIC
		conn, err = a.peerMgr.Connect(ctx, cfg.Address)
	} else {
		// Use ConnectWithTransport for other transports
		conn, err = a.peerMgr.ConnectWithTransport(ctx, peerTransport, cfg.Address)
	}

	if err != nil {
		a.logger.Warn("failed to connect to peer",
			logging.KeyAddress, cfg.Address,
			logging.KeyTransport, cfg.Transport,
			logging.KeyError, err)
		// Reconnection will be handled by peer manager
		return
	}

	a.logger.Info("connected to peer",
		logging.KeyPeerID, conn.RemoteID.ShortString(),
		logging.KeyAddress, cfg.Address,
		logging.KeyTransport, cfg.Transport)

	// Note: SendFullTable / SendNodeInfoToNewPeer are now triggered from
	// handlePeerConnected (peer manager OnPeerConnected callback), which
	// fires for both this dial path and the ReconnectAll path used after
	// sleep/wake.
}

// ShutdownCh closes when RequestShutdown is called; the run loop selects
// on it alongside the OS signal channel.
func (a *Agent) ShutdownCh() <-chan struct{} {
	return a.shutdownReqCh
}

// RequestShutdown is idempotent and non-blocking; callers can return so
// the HTTP response is flushed before the run loop tears the server down.
func (a *Agent) RequestShutdown() {
	a.shutdownReqOnce.Do(func() {
		close(a.shutdownReqCh)
	})
}

// Stop gracefully stops the agent.
func (a *Agent) Stop() error {
	var err error
	a.stopOnce.Do(func() {
		a.logger.Info("stopping agent",
			logging.KeyAgentID, a.id.ShortString())

		a.running.Store(false)
		close(a.stopCh)

		// Withdraw routes before shutdown. Unconditional: even an agent with
		// no exit routes or forwards still originates its agent presence
		// record, which WithdrawLocalRoutes now carries, so peers drop it
		// immediately instead of waiting for route TTL expiry.
		a.flooder.WithdrawLocalRoutes()

		// Stop components in reverse order
		if a.healthServer != nil {
			a.healthServer.Stop()
		}

		// Tear down expose sessions; this also unregisters their ephemeral
		// forward keys.
		if a.exposeManager != nil {
			a.exposeManager.Shutdown()
		}

		// Stop forward listeners
		a.forwardListenersMu.RLock()
		for _, listener := range a.forwardListeners {
			listener.Stop()
		}
		a.forwardListenersMu.RUnlock()

		// Stop forward handler
		if a.forwardHandler != nil {
			a.forwardHandler.Stop()
		}

		if a.exitHandler != nil {
			a.exitHandler.Stop()
		}

		if a.socks5Srv != nil {
			a.socks5Srv.Stop()
		}

		if a.flooder != nil {
			a.flooder.Stop()
		}

		if a.peerMgr != nil {
			a.peerMgr.Close()
		}

		// Close listeners
		a.listenersMu.Lock()
		live := a.liveListeners
		a.liveListeners = make(map[string]*listenerHandle)
		a.listenersMu.Unlock()
		for _, h := range live {
			close(h.stopCh)
			h.listener.Close()
		}

		// Close all transports
		for _, tr := range a.transports {
			if tr != nil {
				tr.Close()
			}
		}

		a.wg.Wait()

		a.logger.Info("agent stopped",
			logging.KeyAgentID, a.id.ShortString())
	})

	return err
}

// StopWithContext stops with a timeout.
func (a *Agent) StopWithContext(ctx context.Context) error {
	done := make(chan error, 1)
	go func() {
		done <- a.Stop()
	}()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// IsRunning returns true if the agent is running.
func (a *Agent) IsRunning() bool {
	return a.running.Load()
}

// ID returns the agent's identity.
func (a *Agent) ID() identity.AgentID {
	return a.id
}

// signManagementEnvelope builds a fresh Ed25519-signed envelope for a
// privileged operation. Returns an error when the agent has no signing
// private key configured -- callers treat that as an authorization
// failure rather than silently proceeding. bodyHash is nil for shell and
// file-transfer operations (target policy enforces what they can do)
// and sha256(canonical body) for dynamic-config mutations.
func (a *Agent) signManagementEnvelope(target identity.AgentID, opTag string, bodyHash []byte) (*crypto.AuthEnvelope, error) {
	if !a.cfg.CanSign() {
		return nil, fmt.Errorf("management signing private key not configured (set management.signing_private_key)")
	}
	privKey, err := a.cfg.GetSigningPrivateKey()
	if err != nil {
		return nil, fmt.Errorf("load signing private key: %w", err)
	}
	env := crypto.SignManagement(privKey, a.id, target, opTag, bodyHash)
	crypto.ZeroSigningKey(&privKey)
	return &env, nil
}

// verifyManagementAuthFor returns a verifier bound to the given operation
// tag. The verifier rejects every request when this agent has no signing
// public key configured -- the "locked down" state where privileged
// operations are advertised but no mesh operator can authorize them.
func (a *Agent) verifyManagementAuthFor(opTag string) func(*crypto.AuthEnvelope) error {
	return func(env *crypto.AuthEnvelope) error {
		if !a.cfg.HasSigningKey() {
			return fmt.Errorf("management signing public key not configured on this agent")
		}
		if env == nil {
			return crypto.ErrAuthMissing
		}
		pubKey, err := a.cfg.GetSigningPublicKey()
		if err != nil {
			return fmt.Errorf("load signing public key: %w", err)
		}
		return crypto.VerifyManagement(pubKey, a.id, *env, opTag, nil)
	}
}

// CanSignManagement reports whether this agent has the management signing
// private key configured. Used by HTTP mutation handlers to reject
// requests from un-authorized gateways.
func (a *Agent) CanSignManagement() bool {
	return a.cfg.CanSign()
}

// StampBody signs an envelope bound only to (origin, target, timestamp,
// opTag) and injects it as the "auth" field of the JSON body. Used for
// file-op endpoints (browse) whose target-side verifier matches the
// stream verifier (nil body hash). mutations that need payload binding
// should use StampMutationBody instead.
func (a *Agent) StampBody(target identity.AgentID, opTag string, body []byte) ([]byte, error) {
	obj := map[string]json.RawMessage{}
	if err := json.Unmarshal(body, &obj); err != nil {
		return nil, fmt.Errorf("parse body: %w", err)
	}
	env, err := a.signManagementEnvelope(target, opTag, nil)
	if err != nil {
		return nil, err
	}
	envJSON, err := json.Marshal(env)
	if err != nil {
		return nil, fmt.Errorf("marshal auth envelope: %w", err)
	}
	obj["auth"] = envJSON
	return json.Marshal(obj)
}

// StampMutationBody parses body as a JSON object, computes sha256 over the
// canonical body (with any existing "auth" key stripped), signs an
// envelope for the given target + operation, and returns the re-serialized
// JSON with "auth" set. Used by HTTP handlers that forward dynamic-config
// mutations to remote agents via the control channel.
func (a *Agent) StampMutationBody(target identity.AgentID, opTag string, body []byte) ([]byte, error) {
	obj := map[string]json.RawMessage{}
	if err := json.Unmarshal(body, &obj); err != nil {
		return nil, fmt.Errorf("parse mutation body: %w", err)
	}
	delete(obj, "auth")
	canonical, err := json.Marshal(obj)
	if err != nil {
		return nil, fmt.Errorf("canonicalize mutation body: %w", err)
	}
	hash := crypto.HashBody(canonical)
	if !a.cfg.CanSign() {
		return nil, fmt.Errorf("management signing private key not configured (set management.signing_private_key)")
	}
	privKey, err := a.cfg.GetSigningPrivateKey()
	if err != nil {
		return nil, fmt.Errorf("load signing private key: %w", err)
	}
	env := crypto.SignManagement(privKey, a.id, target, opTag, hash)
	crypto.ZeroSigningKey(&privKey)
	envJSON, err := json.Marshal(env)
	if err != nil {
		return nil, fmt.Errorf("marshal auth envelope: %w", err)
	}
	obj["auth"] = envJSON
	return json.Marshal(obj)
}

// VerifyMutationBody parses body as a JSON object, extracts the "auth"
// field (if present), recomputes the canonical body hash, and verifies
// the signature. Returns nil on success or one of the crypto.ErrAuth*
// sentinels (possibly wrapped) on failure. Used by control-frame handlers
// that receive mutation requests from remote agents.
func (a *Agent) VerifyMutationBody(opTag string, body []byte) error {
	if !a.cfg.HasSigningKey() {
		return fmt.Errorf("management signing public key not configured on this agent")
	}
	obj := map[string]json.RawMessage{}
	if err := json.Unmarshal(body, &obj); err != nil {
		return fmt.Errorf("parse mutation body: %w", err)
	}
	authRaw, ok := obj["auth"]
	if !ok {
		return crypto.ErrAuthMissing
	}
	var env crypto.AuthEnvelope
	if err := json.Unmarshal(authRaw, &env); err != nil {
		return fmt.Errorf("parse auth envelope: %w", err)
	}
	delete(obj, "auth")
	canonical, err := json.Marshal(obj)
	if err != nil {
		return fmt.Errorf("canonicalize mutation body: %w", err)
	}
	pubKey, err := a.cfg.GetSigningPublicKey()
	if err != nil {
		return fmt.Errorf("load signing public key: %w", err)
	}
	return crypto.VerifyManagement(pubKey, a.id, env, opTag, crypto.HashBody(canonical))
}

// DisplayName returns the agent's display name, or falls back to the short ID.
func (a *Agent) DisplayName() string {
	if dn := a.displayNameForAdvertise(); dn != "" {
		return dn
	}
	return a.id.ShortString()
}

// displayNameForAdvertise returns the display name for route/node info advertisements.
// Unlike DisplayName(), this returns empty string when no name is set (rather than
// falling back to the short ID), matching the original sysinfo.Collect behavior.
func (a *Agent) displayNameForAdvertise() string {
	a.dynamicDisplayNameMu.RLock()
	dn := a.dynamicDisplayName
	a.dynamicDisplayNameMu.RUnlock()
	if dn != "" {
		return dn
	}
	return a.cfg.Agent.DisplayName
}

// TriggerRouteAdvertise triggers an immediate route advertisement.
// This is useful when local routes change and you want faster propagation
// than waiting for the next scheduled interval.
func (a *Agent) TriggerRouteAdvertise() {
	select {
	case a.routeAdvertiseCh <- struct{}{}:
		a.logger.Debug("route advertisement triggered")
	default:
		// Already pending, skip
	}
}

// ensureExitHandler creates an exit handler on demand if one does not exist.
// This allows transit-only agents to be promoted to exit agents dynamically.
func (a *Agent) ensureExitHandler() *exit.Handler {
	if a.exitHandler != nil {
		return a.exitHandler
	}

	a.exitHandlerMu.Lock()
	defer a.exitHandlerMu.Unlock()

	// Double-check after acquiring lock
	if a.exitHandler != nil {
		return a.exitHandler
	}

	exitCfg := exit.HandlerConfig{
		AllowedRoutes:  nil,
		ConnectTimeout: 30 * time.Second,
		IdleTimeout:    a.cfg.Connections.IdleThreshold,
		MaxConnections: a.cfg.Limits.MaxStreamsTotal,
		Logger:         a.logger,
		DNS: exit.DNSConfig{
			Servers: a.cfg.Exit.DNS.Servers,
			Timeout: a.cfg.Exit.DNS.Timeout,
		},
	}
	a.exitHandler = exit.NewHandler(exitCfg, a.id, a)
	a.exitHandler.Start()
	a.logger.Info("exit handler created on demand for dynamic routes")

	return a.exitHandler
}

// persistEvent appends a mutation record to the dynamic config journal.
// A failed append is logged at warn and swallowed -- the runtime mutation
// always succeeds regardless of persistence outcome, matching the documented
// "ephemeral fallback" behavior when the journal file cannot be written.
// Calls made during ReplayDynamicConfig are suppressed so we do not re-write
// the same events we just loaded.
func (a *Agent) persistEvent(e dynconfig.Event) {
	if a.journal == nil || a.replayingDynamic.Load() {
		return
	}
	e.Timestamp = time.Now().UnixNano()
	if err := a.journal.Append(e); err != nil {
		a.logger.Warn("persist dynamic config failed; change is ephemeral",
			logging.KeyError, err,
			"op", e.Op,
			"path", a.journal.Path(),
		)
	}
}

// buildDynamicSnapshot returns the minimal set of events representing the
// currently live dynamic state. Used by ReplayDynamicConfig to compact the
// journal after a successful replay so file size stays O(live entries) instead
// of O(lifetime mutations). Ordering mirrors the replay order so a freshly
// compacted file replays identically.
func (a *Agent) buildDynamicSnapshot() []dynconfig.Event {
	var events []dynconfig.Event

	a.dynamicDisplayNameMu.RLock()
	if a.dynamicDisplayName != "" {
		events = append(events, dynconfig.Event{
			Op:   dynconfig.OpDisplayNameSet,
			Name: a.dynamicDisplayName,
		})
	}
	a.dynamicDisplayNameMu.RUnlock()

	for _, r := range a.routeMgr.GetDynamicRoutes() {
		events = append(events, dynconfig.Event{
			Op:      dynconfig.OpRouteAdd,
			Network: r.Network.String(),
			Metric:  r.Metric,
			Comment: r.Comment,
		})
	}

	overrides := a.routeMgr.GetCommentOverrides()
	overrideKeys := make([]string, 0, len(overrides))
	for k := range overrides {
		overrideKeys = append(overrideKeys, k)
	}
	sort.Strings(overrideKeys)
	for _, k := range overrideKeys {
		events = append(events, dynconfig.Event{
			Op:      dynconfig.OpRouteCommentSet,
			Network: k,
			Comment: overrides[k],
		})
	}

	// Source IP and egress interface bindings live on the exit handler's
	// AllowedRoutes (not on routeMgr.LocalRoutes like comments do), so
	// snapshot them via BindingsByCIDR. Without this, a compaction after
	// the first restart would silently drop both fields -- the in-memory
	// state would still have them, but the on-disk journal would not, and
	// the next restart would replay a journal with no bindings. Applies to
	// dynamic and config-seeded routes alike: a runtime override (via
	// `muti-metroo route source-ip` / `muti-metroo route interface`) survives in both
	// cases, mirroring how OpRouteCommentSet handles comment overrides.
	if a.exitHandler != nil {
		bindings := a.exitHandler.BindingsByCIDR()
		bindingKeys := make([]string, 0, len(bindings))
		for k := range bindings {
			bindingKeys = append(bindingKeys, k)
		}
		sort.Strings(bindingKeys)
		for _, k := range bindingKeys {
			b := bindings[k]
			if b.Source != nil {
				events = append(events, dynconfig.Event{
					Op:       dynconfig.OpRouteSourceIPSet,
					Network:  k,
					SourceIP: b.Source.String(),
				})
			}
			if b.Interface != "" {
				events = append(events, dynconfig.Event{
					Op:        dynconfig.OpRouteInterfaceSet,
					Network:   k,
					Interface: b.Interface,
				})
			}
		}
	}

	a.forwardListenersMu.RLock()
	forwardKeys := make([]string, 0, len(a.dynamicForwardListeners))
	for k := range a.dynamicForwardListeners {
		forwardKeys = append(forwardKeys, k)
	}
	sort.Strings(forwardKeys)
	for _, key := range forwardKeys {
		l, ok := a.forwardListeners[key]
		if !ok {
			continue
		}
		addr := ""
		if la := l.Address(); la != nil {
			addr = la.String()
		}
		events = append(events, dynconfig.Event{
			Op:             dynconfig.OpForwardAdd,
			Key:            key,
			Address:        addr,
			MaxConnections: l.MaxConnections(),
		})
	}
	a.forwardListenersMu.RUnlock()

	// Dynamic forward endpoints: acquire forwardEndpointsMu first (matches the
	// order ManageForwardEndpoint uses for add/remove) so a concurrent mutation
	// cannot split the origin set and the handler target map between our two
	// reads.
	a.forwardEndpointsMu.RLock()
	var endpointKeys []string
	targets := a.forwardHandler.Targets()
	for key := range a.dynamicForwardEndpoints {
		if _, ok := targets[key]; ok {
			endpointKeys = append(endpointKeys, key)
		}
	}
	a.forwardEndpointsMu.RUnlock()
	sort.Strings(endpointKeys)
	for _, key := range endpointKeys {
		events = append(events, dynconfig.Event{
			Op:     dynconfig.OpForwardEndpointAdd,
			Key:    key,
			Target: targets[key],
		})
	}

	// Suffix events come before host events so a replayed host_add can rely
	// on a runtime-added suffix being live.
	configSuffixes := make(map[string]struct{})
	for _, s := range a.meshHosts.ConfigSuffixes() {
		configSuffixes[s] = struct{}{}
	}
	activeSuffixes := make(map[string]struct{})
	for _, s := range a.meshHosts.Suffixes() {
		activeSuffixes[s] = struct{}{}
	}
	addedSuffixes := make([]string, 0)
	removedSuffixes := make([]string, 0)
	for s := range activeSuffixes {
		if _, seeded := configSuffixes[s]; !seeded {
			addedSuffixes = append(addedSuffixes, s)
		}
	}
	for s := range configSuffixes {
		if _, live := activeSuffixes[s]; !live {
			removedSuffixes = append(removedSuffixes, s)
		}
	}
	sort.Strings(addedSuffixes)
	sort.Strings(removedSuffixes)
	for _, s := range addedSuffixes {
		events = append(events, dynconfig.Event{Op: dynconfig.OpSuffixAdd, Suffix: s})
	}
	for _, s := range removedSuffixes {
		events = append(events, dynconfig.Event{Op: dynconfig.OpSuffixRemove, Suffix: s})
	}

	for _, h := range a.meshHosts.Dynamic() {
		events = append(events, dynconfig.Event{
			Op:      dynconfig.OpHostAdd,
			Name:    h.Name,
			IPv4:    h.IPv4,
			IPv6:    h.IPv6,
			Comment: h.Comment,
		})
	}

	peerAddrs := a.peers.DynamicKeys()

	// Peer transport/ExpectedID live in peer.Manager's PeerInfo map; read them
	// back so the compacted entry captures the full add-call shape. Peers that
	// have not yet registered (dial goroutine still pending) fall back to QUIC
	// with no expected ID -- the same defaults ManagePeer would apply.
	peerInfos := a.peerMgr.GetPeerInfos()
	for _, addr := range peerAddrs {
		transportName := string(transport.TransportQUIC)
		var expectedID string
		if info, ok := peerInfos[addr]; ok {
			if info.Transport != nil {
				transportName = string(info.Transport.Type())
			}
			if !info.ExpectedID.IsZero() {
				expectedID = info.ExpectedID.String()
			}
		}
		events = append(events, dynconfig.Event{
			Op:         dynconfig.OpPeerAdd,
			Address:    addr,
			Transport:  transportName,
			ExpectedID: expectedID,
		})
	}

	// Dynamic transport listeners. The registry holds the full spec, so the
	// compacted entry captures the original add-call shape verbatim.
	for _, addr := range a.listeners.DynamicKeys() {
		spec, ok := a.listeners.GetDynamic(addr)
		if !ok {
			continue
		}
		events = append(events, listenerAddEvent(spec))
	}

	return events
}

// listenerAddEvent builds the journal event recording a dynamic listener add
// from its spec. Shared by ManageListener and journal compaction so both encode
// listeners identically.
func listenerAddEvent(spec config.ListenerConfig) dynconfig.Event {
	e := dynconfig.Event{
		Op:        dynconfig.OpListenerAdd,
		Transport: spec.Transport,
		Address:   spec.Address,
		Path:      spec.Path,
		PlainText: spec.PlainText,
	}
	if spec.Advertise != nil {
		e.AdvertiseAddress = spec.Advertise.Address
		e.AdvertisePath = spec.Advertise.Path
		e.AdvertiseTransport = spec.Advertise.Transport
	}
	return e
}

// ReplayDynamicConfig loads the dynamic config journal and applies each event
// through the same Manage* methods the CLI and HTTP handlers use, then
// compacts the journal to the minimal live-state snapshot. Must be called
// after Start() has returned successfully -- the Manage* methods assume peer
// manager, route manager, and forward infrastructure are live.
//
// Errors loading the file are logged and swallowed (nil returned) so a
// corrupted journal never prevents the agent from running; a warning tells
// the operator to inspect the file. Per-event failures (e.g. forward listener
// port already in use, peer-id collision with config) are also logged and
// skipped -- the remaining events still replay.
func (a *Agent) ReplayDynamicConfig() error {
	if a.journal == nil {
		return nil
	}

	events, err := a.journal.Load()
	if err != nil {
		a.logger.Warn("load dynamic config journal failed; skipping replay",
			logging.KeyError, err,
			"path", a.journal.Path(),
		)
		return nil
	}
	if len(events) == 0 {
		return nil
	}

	a.replayingDynamic.Store(true)
	defer a.replayingDynamic.Store(false)

	// Bucket by op kind so we can apply in a deterministic order regardless of
	// how churn interleaved them in the journal. Display-name first so the
	// first post-restart node-info advertisement carries the restored name;
	// remaining buckets are independent so order among them is aesthetic.
	var dnEvents, routeEvents, forwardEvents, forwardEndpointEvents, peerEvents, listenerEvents, suffixEvents, hostEvents []dynconfig.Event
	for _, e := range events {
		switch e.Op {
		case dynconfig.OpDisplayNameSet:
			dnEvents = append(dnEvents, e)
		case dynconfig.OpRouteAdd, dynconfig.OpRouteRemove, dynconfig.OpRouteCommentSet, dynconfig.OpRouteSourceIPSet, dynconfig.OpRouteInterfaceSet:
			routeEvents = append(routeEvents, e)
		case dynconfig.OpForwardAdd, dynconfig.OpForwardRemove:
			forwardEvents = append(forwardEvents, e)
		case dynconfig.OpForwardEndpointAdd, dynconfig.OpForwardEndpointRemove:
			forwardEndpointEvents = append(forwardEndpointEvents, e)
		case dynconfig.OpPeerAdd, dynconfig.OpPeerRemove:
			peerEvents = append(peerEvents, e)
		case dynconfig.OpListenerAdd, dynconfig.OpListenerRemove:
			listenerEvents = append(listenerEvents, e)
		case dynconfig.OpSuffixAdd, dynconfig.OpSuffixRemove:
			suffixEvents = append(suffixEvents, e)
		case dynconfig.OpHostAdd, dynconfig.OpHostRemove:
			hostEvents = append(hostEvents, e)
		default:
			a.logger.Warn("dynamic config journal: unknown op, skipping",
				"op", e.Op,
			)
		}
	}

	applied := 0
	skipped := 0

	apply := func(e dynconfig.Event, fn func() error) {
		if err := fn(); err != nil {
			a.logger.Warn("dynamic config replay: event failed",
				logging.KeyError, err,
				"op", e.Op,
				"address", e.Address,
				"network", e.Network,
				"key", e.Key,
			)
			skipped++
			return
		}
		applied++
	}

	for _, e := range dnEvents {
		e := e
		apply(e, func() error {
			_, err := a.ManageDisplayName("set", e.Name)
			return err
		})
	}
	for _, e := range routeEvents {
		e := e
		switch e.Op {
		case dynconfig.OpRouteAdd:
			apply(e, func() error {
				_, err := a.ManageRoute("add", e.Network, e.Metric, e.Comment, e.SourceIP, e.Interface, "")
				return err
			})
		case dynconfig.OpRouteRemove:
			apply(e, func() error {
				_, err := a.ManageRoute("remove", e.Network, 0, "", "", "", "")
				return err
			})
		case dynconfig.OpRouteCommentSet:
			apply(e, func() error {
				_, err := a.ManageRoute("set_comment", e.Network, 0, e.Comment, "", "", "")
				return err
			})
		case dynconfig.OpRouteSourceIPSet:
			apply(e, func() error {
				_, err := a.ManageRoute("set_source_ip", e.Network, 0, "", e.SourceIP, "", "")
				return err
			})
		case dynconfig.OpRouteInterfaceSet:
			apply(e, func() error {
				_, err := a.ManageRoute("set_interface", e.Network, 0, "", "", e.Interface, "")
				return err
			})
		}
	}
	for _, e := range forwardEvents {
		e := e
		switch e.Op {
		case dynconfig.OpForwardAdd:
			apply(e, func() error {
				_, err := a.ManageForwardListener("add", e.Key, e.Address, e.MaxConnections)
				return err
			})
		case dynconfig.OpForwardRemove:
			apply(e, func() error {
				_, err := a.ManageForwardListener("remove", e.Key, "", 0)
				return err
			})
		}
	}
	for _, e := range forwardEndpointEvents {
		e := e
		switch e.Op {
		case dynconfig.OpForwardEndpointAdd:
			apply(e, func() error {
				_, err := a.ManageForwardEndpoint("add", e.Key, e.Target)
				return err
			})
		case dynconfig.OpForwardEndpointRemove:
			apply(e, func() error {
				_, err := a.ManageForwardEndpoint("remove", e.Key, "")
				return err
			})
		}
	}
	for _, e := range peerEvents {
		e := e
		switch e.Op {
		case dynconfig.OpPeerAdd:
			apply(e, func() error {
				_, err := a.ManagePeer("add", e.Address, e.Transport, e.ExpectedID, e.TCPKeepAlive)
				return err
			})
		case dynconfig.OpPeerRemove:
			apply(e, func() error {
				_, err := a.ManagePeer("remove", e.Address, "", "", "")
				return err
			})
		}
	}
	for _, e := range listenerEvents {
		e := e
		switch e.Op {
		case dynconfig.OpListenerAdd:
			apply(e, func() error {
				_, err := a.ManageListener(&health.ListenerManageRequest{
					Action:             "add",
					Transport:          e.Transport,
					Address:            e.Address,
					Path:               e.Path,
					PlainText:          e.PlainText,
					AdvertiseAddress:   e.AdvertiseAddress,
					AdvertisePath:      e.AdvertisePath,
					AdvertiseTransport: e.AdvertiseTransport,
				})
				return err
			})
		case dynconfig.OpListenerRemove:
			apply(e, func() error {
				_, err := a.ManageListener(&health.ListenerManageRequest{Action: "remove", Address: e.Address})
				return err
			})
		}
	}
	// Suffix events apply before host events so a host_add referencing a
	// runtime-added TLD can succeed during replay.
	for _, e := range suffixEvents {
		e := e
		switch e.Op {
		case dynconfig.OpSuffixAdd:
			apply(e, func() error {
				_, err := a.ManageMeshHostSuffix("add", e.Suffix)
				return err
			})
		case dynconfig.OpSuffixRemove:
			apply(e, func() error {
				_, err := a.ManageMeshHostSuffix("remove", e.Suffix)
				return err
			})
		}
	}
	for _, e := range hostEvents {
		e := e
		switch e.Op {
		case dynconfig.OpHostAdd:
			apply(e, func() error {
				_, err := a.ManageMeshHost("add", e.Name, e.IPv4, e.IPv6, e.Comment)
				return err
			})
		case dynconfig.OpHostRemove:
			apply(e, func() error {
				_, err := a.ManageMeshHost("remove", e.Name, "", "", "")
				return err
			})
		}
	}

	a.logger.Info("dynamic config journal replayed",
		"applied", applied,
		"skipped", skipped,
		"path", a.journal.Path(),
	)

	// Skip compaction when the on-disk event count already equals the live
	// snapshot: no churn has accumulated and rewriting would be an identical
	// file plus an extra fsync. Any divergence (add/remove pairs, skipped
	// events, display-name flips) produces len(events) > len(snapshot) and
	// triggers the rewrite.
	snapshot := a.buildDynamicSnapshot()
	if len(events) == len(snapshot) {
		return nil
	}
	if err := a.journal.Compact(snapshot); err != nil {
		a.logger.Warn("compact dynamic config journal failed",
			logging.KeyError, err,
			"path", a.journal.Path(),
		)
	}

	return nil
}

// TraceRoute explains what this agent would do with a destination. Dest is an
// IP address or a hostname; hostnames are resolved using the agent's local
// resolver and also checked against the domain route table. The returned
// result mirrors the selection rules used by the live stream open path.
func (a *Agent) TraceRoute(dest string) (*health.RouteTraceResult, error) {
	if dest == "" {
		return nil, fmt.Errorf("dest is required")
	}

	result := &health.RouteTraceResult{
		Status: "ok",
		Dest:   dest,
	}

	// If the input parses as an IP, trace that single IP. Otherwise treat it
	// as a hostname: look up the domain route table and resolve to IPs.
	if ip := net.ParseIP(dest); ip != nil {
		result.Resolution = health.ResolutionIP
		result.IPs = []*health.RouteTraceIP{a.traceIP(ip)}
		return result, nil
	}

	result.Hostname = dest

	// Domain route lookup (same rules as live: exact > single-level wildcard).
	domTrace := a.routeMgr.DomainTable().Explain(dest)
	if domTrace.Winner != nil {
		w := domTrace.Winner
		result.Domain = &health.RouteTraceDomain{
			Matched:     true,
			Pattern:     w.Pattern,
			IsWildcard:  w.IsWildcard,
			Metric:      w.Metric,
			Origin:      w.OriginAgent.ShortString(),
			OriginName:  a.routeMgr.GetDisplayName(w.OriginAgent),
			NextHop:     w.NextHop.ShortString(),
			NextHopName: a.routeMgr.GetDisplayName(w.NextHop),
			HopCount:    len(w.Path),
		}
	} else {
		result.Domain = &health.RouteTraceDomain{Matched: false}
	}

	// Mesh-host registry: name -> IP from /hosts/manage and the mesh_hosts:
	// config block. Consulted before system DNS so registered names resolve
	// without depending on the agent's host having Mutiauk configured to
	// forward *.<suffix> queries.
	if h, ok := a.meshHosts.Lookup(dest); ok {
		result.Resolution = health.ResolutionMesh
		result.ResolverOK = true
		if ip := net.ParseIP(h.IPv4); ip != nil {
			result.IPs = append(result.IPs, a.traceIP(ip))
		}
		if ip := net.ParseIP(h.IPv6); ip != nil {
			result.IPs = append(result.IPs, a.traceIP(ip))
		}
		return result, nil
	}

	// Resolve hostname via the agent's own resolver, with a short deadline so
	// a bad resolver never hangs the request.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, dest)
	if err != nil {
		result.ResolverOK = false
		result.Error = "resolve " + dest + ": " + err.Error()
		return result, nil
	}
	result.ResolverOK = true
	result.Resolution = health.ResolutionSystem

	for _, ip := range ips {
		result.IPs = append(result.IPs, a.traceIP(ip.IP))
	}

	return result, nil
}

// traceIP produces the CIDR trace for a single IP. Uses the same Table that
// serves the live routeMgr.Lookup path, so the winner here is what the stream
// open code would also pick.
func (a *Agent) traceIP(ip net.IP) *health.RouteTraceIP {
	out := &health.RouteTraceIP{IP: ip.String()}
	tr := a.routeMgr.Table().Explain(ip)
	if len(tr.Candidates) == 0 {
		out.NoMatch = true
		return out
	}
	for _, c := range tr.Candidates {
		last := ""
		if !c.Route.LastUpdate.IsZero() {
			last = c.Route.LastUpdate.UTC().Format(time.RFC3339)
		}
		out.Candidates = append(out.Candidates, &health.RouteTraceCandidate{
			Network:     c.Route.Network.String(),
			PrefixLen:   c.PrefixLen,
			Metric:      c.Route.Metric,
			Origin:      c.Route.OriginAgent.ShortString(),
			OriginName:  a.routeMgr.GetDisplayName(c.Route.OriginAgent),
			NextHop:     c.Route.NextHop.ShortString(),
			NextHopName: a.routeMgr.GetDisplayName(c.Route.NextHop),
			HopCount:    len(c.Route.Path),
			LastUpdate:  last,
			Winner:      c.Winner,
			Reason:      c.Reason,
			Comment:     c.Route.Comment,
		})
	}
	return out
}

// handleRouteTrace processes a ControlTypeRouteTrace control request.
// Read-only; no management-key signature is required.
func (a *Agent) handleRouteTrace(data []byte) ([]byte, bool) {
	var req struct {
		Dest string `json:"dest"`
	}
	if err := json.Unmarshal(data, &req); err != nil {
		resp, _ := json.Marshal(map[string]string{"error": "invalid request: " + err.Error()})
		return resp, false
	}
	result, err := a.TraceRoute(req.Dest)
	if err != nil {
		resp, _ := json.Marshal(map[string]string{"error": err.Error()})
		return resp, false
	}
	resp, _ := json.Marshal(result)
	return resp, true
}

// MeshHosts returns the merged config + dynamic host list. Used by the
// dashboard handler.
func (a *Agent) MeshHosts() []hosts.Host {
	return a.meshHosts.List()
}

// MeshHostsSuffixes returns the currently active TLD set (without leading
// dots). Surfaced over the API so clients can validate names before
// submitting them.
func (a *Agent) MeshHostsSuffixes() []string {
	return a.meshHosts.Suffixes()
}

// ManageMeshHostSuffix handles add/remove/list on the mesh-host TLD set.
// The store returns sentinel errors (hosts.ErrInvalid, ErrExists,
// ErrNotFound, ErrSuffixInUse) which the HTTP handler maps to status
// codes; mutations are journalled so they survive restart.
func (a *Agent) ManageMeshHostSuffix(action, suffix string) (*health.MeshHostSuffixManageResult, error) {
	switch action {
	case "add":
		added, err := a.meshHosts.AddSuffix(suffix)
		if err != nil {
			return nil, err
		}
		a.persistEvent(dynconfig.Event{
			Op:     dynconfig.OpSuffixAdd,
			Suffix: added,
		})
		return a.meshHostSuffixResult(), nil

	case "remove":
		if err := a.meshHosts.RemoveSuffix(suffix); err != nil {
			return nil, err
		}
		a.persistEvent(dynconfig.Event{
			Op:     dynconfig.OpSuffixRemove,
			Suffix: strings.ToLower(strings.TrimSpace(suffix)),
		})
		return a.meshHostSuffixResult(), nil

	case "list":
		return a.meshHostSuffixResult(), nil

	default:
		return nil, fmt.Errorf("unknown action %q (expected add, remove, or list)", action)
	}
}

func (a *Agent) meshHostSuffixResult() *health.MeshHostSuffixManageResult {
	return &health.MeshHostSuffixManageResult{
		Suffixes: a.meshHosts.Suffixes(),
	}
}

// ManageMeshHost handles add/remove/list on the mesh-host registry.
// The store returns sentinel errors (hosts.ErrInvalid, ErrExists,
// ErrNotFound, ErrProtected) which the HTTP handler maps to status
// codes; mutations are journalled so they survive restart.
func (a *Agent) ManageMeshHost(action, name, ipv4, ipv6, comment string) (*health.MeshHostManageResult, error) {
	switch action {
	case "add":
		added, err := a.meshHosts.Add(hosts.Host{
			Name:    name,
			IPv4:    ipv4,
			IPv6:    ipv6,
			Comment: comment,
		})
		if err != nil {
			return nil, err
		}
		a.persistEvent(dynconfig.Event{
			Op:      dynconfig.OpHostAdd,
			Name:    added.Name,
			IPv4:    added.IPv4,
			IPv6:    added.IPv6,
			Comment: added.Comment,
		})
		return a.meshHostResult(), nil

	case "remove":
		removed, err := a.meshHosts.Remove(name)
		if err != nil {
			return nil, err
		}
		a.persistEvent(dynconfig.Event{
			Op:   dynconfig.OpHostRemove,
			Name: removed.Name,
		})
		return a.meshHostResult(), nil

	case "list":
		return a.meshHostResult(), nil

	default:
		return nil, fmt.Errorf("unknown action %q (expected add, remove, or list)", action)
	}
}

func (a *Agent) meshHostResult() *health.MeshHostManageResult {
	return &health.MeshHostManageResult{
		Suffixes: a.meshHosts.Suffixes(),
		Hosts:    a.meshHosts.List(),
	}
}

// ManageRoute implements health.RouteManageProvider. See that interface for
// the contract; this method is the agent-side wiring.
func (a *Agent) ManageRoute(action, network string, metric uint16, comment, sourceIP, iface, scope string) (*health.RouteManageResult, error) {
	switch action {
	case "add":
		ipNet, src, err := config.ParseCIDRWithSource(network, sourceIP)
		if err != nil {
			return nil, err
		}
		if err := config.RequireInterfaceBindingPrivilege(sourceIP, iface); err != nil {
			return nil, fmt.Errorf("interface %q rejected: %w", iface, err)
		}

		if err := a.routeMgr.AddDynamicRoute(ipNet, metric, comment); err != nil {
			return nil, err
		}

		a.ensureExitHandler().AddAllowedRoute(ipNet, src, iface)
		a.TriggerRouteAdvertise()

		a.persistEvent(dynconfig.Event{
			Op:        dynconfig.OpRouteAdd,
			Network:   ipNet.String(),
			Metric:    metric,
			Comment:   comment,
			SourceIP:  sourceIP,
			Interface: iface,
		})

		return &health.RouteManageResult{
			Status:  "ok",
			Message: fmt.Sprintf("route %s added", ipNet.String()),
		}, nil

	case "remove":
		_, ipNet, err := net.ParseCIDR(network)
		if err != nil {
			return nil, fmt.Errorf("invalid CIDR %q: %w", network, err)
		}

		if err := a.routeMgr.RemoveDynamicRoute(ipNet); err != nil {
			return nil, err
		}

		if a.exitHandler != nil {
			a.exitHandler.RemoveAllowedRoute(ipNet)
		}
		a.flooder.WithdrawLocalCIDRRoute(ipNet, metric)
		a.TriggerRouteAdvertise()

		a.persistEvent(dynconfig.Event{
			Op:      dynconfig.OpRouteRemove,
			Network: ipNet.String(),
		})

		return &health.RouteManageResult{
			Status:  "ok",
			Message: fmt.Sprintf("route %s removed", ipNet.String()),
		}, nil

	case "set_comment":
		_, ipNet, err := net.ParseCIDR(network)
		if err != nil {
			return nil, fmt.Errorf("invalid CIDR %q: %w", network, err)
		}

		if err := a.routeMgr.SetRouteComment(ipNet, comment); err != nil {
			return nil, err
		}

		a.persistEvent(dynconfig.Event{
			Op:      dynconfig.OpRouteCommentSet,
			Network: ipNet.String(),
			Comment: comment,
		})

		// Re-advertise so peers learn the new comment without waiting
		// for the periodic refresh. Behaves like any other route refresh:
		// the next advertisement carries an incremented sequence and
		// receivers update the stored Comment alongside it.
		a.TriggerRouteAdvertise()

		return &health.RouteManageResult{
			Status:  "ok",
			Message: fmt.Sprintf("route %s comment updated", ipNet.String()),
		}, nil

	case "set_source_ip":
		ipNet, src, err := config.ParseCIDRWithSource(network, sourceIP)
		if err != nil {
			return nil, err
		}

		if a.exitHandler == nil {
			return nil, fmt.Errorf("exit handler not configured")
		}
		if err := a.exitHandler.SetAllowedRouteSource(ipNet, src); err != nil {
			return nil, err
		}

		a.persistEvent(dynconfig.Event{
			Op:       dynconfig.OpRouteSourceIPSet,
			Network:  ipNet.String(),
			SourceIP: sourceIP,
		})

		return &health.RouteManageResult{
			Status:  "ok",
			Message: fmt.Sprintf("route %s source_ip updated", ipNet.String()),
		}, nil

	case "set_interface":
		_, ipNet, err := net.ParseCIDR(network)
		if err != nil {
			return nil, fmt.Errorf("invalid CIDR %q: %w", network, err)
		}
		if err := config.RequireInterfaceBindingPrivilege(sourceIP, iface); err != nil {
			return nil, fmt.Errorf("interface %q rejected: %w", iface, err)
		}

		if a.exitHandler == nil {
			return nil, fmt.Errorf("exit handler not configured")
		}
		if err := a.exitHandler.SetAllowedRouteInterface(ipNet, iface); err != nil {
			return nil, err
		}

		a.persistEvent(dynconfig.Event{
			Op:        dynconfig.OpRouteInterfaceSet,
			Network:   ipNet.String(),
			Interface: iface,
		})

		return &health.RouteManageResult{
			Status:  "ok",
			Message: fmt.Sprintf("route %s interface updated", ipNet.String()),
		}, nil

	case "list":
		var sources []*routing.LocalRoute
		var dynKeys map[string]struct{}
		all := false
		switch scope {
		case "", health.ScopeDynamic:
			sources = a.routeMgr.GetDynamicRoutes()
		case health.ScopeAll:
			all = true
			sources = a.routeMgr.GetLocalRoutes()
			// Snapshot the dynamic set once so we avoid an RLock + lookup
			// per entry; both methods are cheap on the cold list path but
			// the membership check is shared.
			dyn := a.routeMgr.GetDynamicRoutes()
			dynKeys = make(map[string]struct{}, len(dyn))
			for _, r := range dyn {
				dynKeys[r.Network.String()] = struct{}{}
			}
		default:
			return nil, fmt.Errorf("unknown scope %q (expected \"\", %q, or %q)", scope, health.ScopeDynamic, health.ScopeAll)
		}
		var bindings map[string]exit.RouteBinding
		if a.exitHandler != nil {
			bindings = a.exitHandler.BindingsByCIDR()
		}
		entries := make([]health.RouteManageResultEntry, 0, len(sources))
		for _, r := range sources {
			network := r.Network.String()
			dynamic := true
			if all {
				_, dynamic = dynKeys[network]
			}
			entry := health.RouteManageResultEntry{
				Network: network,
				Metric:  r.Metric,
				Dynamic: dynamic,
				Comment: r.Comment,
			}
			if b, ok := bindings[network]; ok {
				if b.Source != nil {
					entry.SourceIP = b.Source.String()
				}
				entry.Interface = b.Interface
			}
			entries = append(entries, entry)
		}
		if all {
			sort.Slice(entries, func(i, j int) bool {
				return entries[i].Network < entries[j].Network
			})
		}
		return &health.RouteManageResult{
			Status: "ok",
			Routes: entries,
		}, nil

	default:
		return nil, fmt.Errorf("unknown action %q (expected add, remove, list, set_comment, set_source_ip, or set_interface)", action)
	}
}

// handleRouteManage processes a ControlTypeRouteManage control request.
func (a *Agent) handleRouteManage(data []byte) ([]byte, bool) {
	var req struct {
		Action    string `json:"action"`
		Network   string `json:"network"`
		Metric    uint16 `json:"metric"`
		Comment   string `json:"comment,omitempty"`
		SourceIP  string `json:"source_ip,omitempty"`
		Interface string `json:"interface,omitempty"`
		Scope     string `json:"scope,omitempty"`
	}
	if err := json.Unmarshal(data, &req); err != nil {
		resp, _ := json.Marshal(map[string]string{"error": "invalid request: " + err.Error()})
		return resp, false
	}

	if err := a.verifyRouteActionAuth(req.Action, data); err != nil {
		resp, _ := json.Marshal(map[string]string{"error": err.Error()})
		return resp, false
	}

	result, err := a.ManageRoute(req.Action, req.Network, req.Metric, req.Comment, req.SourceIP, req.Interface, req.Scope)
	if err != nil {
		resp, _ := json.Marshal(map[string]string{"error": err.Error()})
		return resp, false
	}

	resp, _ := json.Marshal(result)
	return resp, true
}

// verifyRouteActionAuth maps each route action to its crypto.Op* tag and
// verifies the management signature. Read-only actions (list) skip the
// check; truly unknown actions are rejected by ManageRoute itself.
func (a *Agent) verifyRouteActionAuth(action string, data []byte) error {
	var opTag string
	switch action {
	case "add":
		opTag = crypto.OpRouteAdd
	case "remove":
		opTag = crypto.OpRouteRemove
	case "set_comment":
		opTag = crypto.OpRouteCommentSet
	case "set_source_ip":
		opTag = crypto.OpRouteSourceIPSet
	case "set_interface":
		opTag = crypto.OpRouteInterfaceSet
	default:
		return nil
	}
	return a.VerifyMutationBody(opTag, data)
}

// verifyActionAuth checks the management signature on add/remove mutation
// actions. Unknown actions (including list/get) pass through without
// verification; the underlying handler rejects truly unknown actions.
// Shared by all dynamic-config mutation handlers so the auth step is
// uniform.
func (a *Agent) verifyActionAuth(action, addTag, removeTag string, data []byte) error {
	var opTag string
	switch action {
	case "add":
		opTag = addTag
	case "remove":
		opTag = removeTag
	default:
		return nil
	}
	return a.VerifyMutationBody(opTag, data)
}

// verifySetActionAuth is the display-name-style counterpart: the only
// mutation is "set". Kept separate from verifyActionAuth so callers pick
// the helper whose semantics match their action vocabulary.
func (a *Agent) verifySetActionAuth(action, setTag string, data []byte) error {
	if action != "set" {
		return nil
	}
	return a.VerifyMutationBody(setTag, data)
}

// handleProbeRequest runs a.probeFunc and returns the JSON-encoded
// probe.Response. Probe-level failures (handshake error, refused) carry
// success=false in the body but report success=true at the control layer so
// the originating HTTP handler on a remote agent sees the same shape as a
// local probe. The context is bound to agent shutdown, so an in-flight probe
// cancels promptly instead of running out its own timeout after Stop().
func (a *Agent) handleProbeRequest(data []byte) ([]byte, bool) {
	ctx, cancel := a.ShutdownBoundCtx(context.Background())
	defer cancel()
	return probe.HandleRequest(ctx, data, a.probeFunc)
}

// ShutdownBoundCtx derives a context from parent that is also cancelled when
// the agent starts stopping (stopCh closes). Callers MUST call the returned
// cancel to release the watcher goroutine.
func (a *Agent) ShutdownBoundCtx(parent context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	go func() {
		select {
		case <-a.stopCh:
			cancel()
		case <-ctx.Done():
		}
	}()
	return ctx, cancel
}

// TriggerNodeInfoAdvertise triggers an immediate node info advertisement.
// This is useful when forward listeners change and you want peers to learn quickly.
func (a *Agent) TriggerNodeInfoAdvertise() {
	select {
	case a.nodeInfoAdvertiseCh <- struct{}{}:
		a.logger.Debug("node info advertisement triggered")
	default:
		// Already pending, skip
	}
}

// ManageForwardListener handles dynamic forward listener management (add/remove/list).
func (a *Agent) ManageForwardListener(action, key, address string, maxConnections int) (*health.ForwardManageResult, error) {
	switch action {
	case "add":
		if key == "" {
			return nil, fmt.Errorf("key is required")
		}
		if address == "" {
			return nil, fmt.Errorf("address is required")
		}

		a.forwardListenersMu.Lock()

		// Reject if key exists in config listeners
		if _, isConfig := a.configForwardListeners[key]; isConfig {
			a.forwardListenersMu.Unlock()
			return nil, fmt.Errorf("listener %q is a config listener and cannot be replaced", key)
		}

		// If key exists as dynamic, stop the old listener first (allows replacing)
		if oldListener, exists := a.forwardListeners[key]; exists {
			if _, isDynamic := a.dynamicForwardListeners[key]; isDynamic {
				a.forwardListenersMu.Unlock()
				oldListener.Stop()
				a.forwardListenersMu.Lock()
			}
		}

		cfg := forward.ListenerConfig{
			Key:            key,
			Address:        address,
			MaxConnections: maxConnections,
			Logger:         a.logger,
		}
		listener := forward.NewListener(cfg, a)
		if err := listener.Start(); err != nil {
			a.forwardListenersMu.Unlock()
			return nil, fmt.Errorf("failed to start listener: %w", err)
		}

		a.forwardListeners[key] = listener
		a.dynamicForwardListeners[key] = struct{}{}
		a.forwardListenersMu.Unlock()

		a.TriggerNodeInfoAdvertise()

		// Persist the address as the caller passed it so an explicit ":0"
		// stays ephemeral across restarts; callers who want a stable port
		// pass one explicitly.
		a.persistEvent(dynconfig.Event{
			Op:             dynconfig.OpForwardAdd,
			Key:            key,
			Address:        address,
			MaxConnections: maxConnections,
		})

		return &health.ForwardManageResult{
			Status:  "ok",
			Message: fmt.Sprintf("forward listener %q added on %s", key, listener.Address().String()),
		}, nil

	case "remove":
		if key == "" {
			return nil, fmt.Errorf("key is required")
		}

		a.forwardListenersMu.Lock()

		// Reject if key is a config listener
		if _, isConfig := a.configForwardListeners[key]; isConfig {
			a.forwardListenersMu.Unlock()
			return nil, fmt.Errorf("listener %q is a config listener and cannot be removed", key)
		}

		// Reject if key not in dynamic listeners
		if _, isDynamic := a.dynamicForwardListeners[key]; !isDynamic {
			a.forwardListenersMu.Unlock()
			return nil, fmt.Errorf("listener %q not found", key)
		}

		listener := a.forwardListeners[key]
		delete(a.forwardListeners, key)
		delete(a.dynamicForwardListeners, key)
		a.forwardListenersMu.Unlock()

		if listener != nil {
			listener.Stop()
		}

		a.TriggerNodeInfoAdvertise()

		a.persistEvent(dynconfig.Event{
			Op:  dynconfig.OpForwardRemove,
			Key: key,
		})

		return &health.ForwardManageResult{
			Status:  "ok",
			Message: fmt.Sprintf("forward listener %q removed", key),
		}, nil

	case "list":
		a.forwardListenersMu.RLock()
		entries := make([]health.ForwardManageResultEntry, 0, len(a.forwardListeners))
		for key, listener := range a.forwardListeners {
			_, isDynamic := a.dynamicForwardListeners[key]
			addr := ""
			if lisAddr := listener.Address(); lisAddr != nil {
				addr = lisAddr.String()
			}
			entries = append(entries, health.ForwardManageResultEntry{
				Key:            key,
				Address:        addr,
				MaxConnections: listener.MaxConnections(),
				Dynamic:        isDynamic,
			})
		}
		a.forwardListenersMu.RUnlock()
		return &health.ForwardManageResult{
			Status:    "ok",
			Listeners: entries,
		}, nil

	default:
		return nil, fmt.Errorf("unknown action %q (expected add, remove, or list)", action)
	}
}

// handleForwardManage processes a ControlTypeForwardManage control request.
func (a *Agent) handleForwardManage(data []byte) ([]byte, bool) {
	var req struct {
		Action         string `json:"action"`
		Key            string `json:"key"`
		Address        string `json:"address"`
		MaxConnections int    `json:"max_connections"`
	}
	if err := json.Unmarshal(data, &req); err != nil {
		resp, _ := json.Marshal(map[string]string{"error": "invalid request: " + err.Error()})
		return resp, false
	}

	if err := a.verifyActionAuth(req.Action, crypto.OpForwardAdd, crypto.OpForwardRemove, data); err != nil {
		resp, _ := json.Marshal(map[string]string{"error": err.Error()})
		return resp, false
	}

	result, err := a.ManageForwardListener(req.Action, req.Key, req.Address, req.MaxConnections)
	if err != nil {
		resp, _ := json.Marshal(map[string]string{"error": err.Error()})
		return resp, false
	}

	resp, _ := json.Marshal(result)
	return resp, true
}

// ManageForwardEndpoint handles dynamic forward endpoint management
// (add/remove/list). Implements the health.ForwardEndpointManageProvider
// interface.
//
// Endpoints advertise a local "key -> host:port" mapping into the mesh so
// ingress listeners on peers can reach the target through this agent. Config
// endpoints are protected from runtime mutation; runtime-added endpoints
// persist to the dynconfig journal so they survive agent restarts (when
// data_dir is set).
func (a *Agent) ManageForwardEndpoint(action, key, target string) (*health.ForwardEndpointManageResult, error) {
	switch action {
	case "add":
		if key == "" {
			return nil, fmt.Errorf("key is required")
		}
		if target == "" {
			return nil, fmt.Errorf("target is required")
		}
		if _, _, err := net.SplitHostPort(target); err != nil {
			return nil, fmt.Errorf("invalid target %q: %w", target, err)
		}

		a.forwardEndpointsMu.Lock()
		if _, isConfig := a.configForwardEndpoints[key]; isConfig {
			a.forwardEndpointsMu.Unlock()
			return nil, fmt.Errorf("endpoint %q is a config endpoint and cannot be replaced", key)
		}
		if owner, claimed := a.ephemeralForwardEndpoints[key]; claimed {
			a.forwardEndpointsMu.Unlock()
			return nil, fmt.Errorf("endpoint %q is held by an expose session %s; disconnect it first", key, owner)
		}
		if err := a.forwardHandler.AddTarget(key, target); err != nil {
			a.forwardEndpointsMu.Unlock()
			return nil, err
		}
		a.dynamicForwardEndpoints[key] = struct{}{}
		a.forwardEndpointsMu.Unlock()

		// AddLocalForwardRoute overwrites any existing entry for the key and
		// bumps the sequence, so replacing a dynamic endpoint with a different
		// target works without an explicit remove step.
		a.routeMgr.AddLocalForwardRoute(key, target, 0)
		a.TriggerRouteAdvertise()

		a.persistEvent(dynconfig.Event{
			Op:     dynconfig.OpForwardEndpointAdd,
			Key:    key,
			Target: target,
		})

		return &health.ForwardEndpointManageResult{
			Status:  "ok",
			Message: fmt.Sprintf("forward endpoint %q added -> %s", key, target),
		}, nil

	case "remove":
		if key == "" {
			return nil, fmt.Errorf("key is required")
		}

		a.forwardEndpointsMu.Lock()
		if _, isConfig := a.configForwardEndpoints[key]; isConfig {
			a.forwardEndpointsMu.Unlock()
			return nil, fmt.Errorf("endpoint %q is a config endpoint and cannot be removed", key)
		}
		if _, isDynamic := a.dynamicForwardEndpoints[key]; !isDynamic {
			a.forwardEndpointsMu.Unlock()
			return nil, fmt.Errorf("endpoint %q not found", key)
		}
		delete(a.dynamicForwardEndpoints, key)
		a.forwardHandler.RemoveTarget(key)
		a.forwardEndpointsMu.Unlock()

		target := ""
		if r := a.routeMgr.LookupForward(key); r != nil {
			target = r.Target
		}
		a.routeMgr.RemoveLocalForwardRoute(key)
		a.flooder.WithdrawLocalForwardRoute(key, target)
		a.TriggerRouteAdvertise()

		a.persistEvent(dynconfig.Event{
			Op:  dynconfig.OpForwardEndpointRemove,
			Key: key,
		})

		return &health.ForwardEndpointManageResult{
			Status:  "ok",
			Message: fmt.Sprintf("forward endpoint %q removed", key),
		}, nil

	case "list":
		targets := a.forwardHandler.Targets()
		a.forwardEndpointsMu.RLock()
		entries := make([]health.ForwardEndpointManageResultEntry, 0, len(targets))
		for key, target := range targets {
			_, isDynamic := a.dynamicForwardEndpoints[key]
			entries = append(entries, health.ForwardEndpointManageResultEntry{
				Key:     key,
				Target:  target,
				Dynamic: isDynamic,
			})
		}
		a.forwardEndpointsMu.RUnlock()
		return &health.ForwardEndpointManageResult{
			Status:    "ok",
			Endpoints: entries,
		}, nil

	default:
		return nil, fmt.Errorf("unknown action %q (expected add, remove, or list)", action)
	}
}

// RegisterEphemeralForwardKey installs an in-memory forward endpoint owned by
// an expose (SOCKS5 CmdExpose) session. Returns an error if the key collides
// with a config endpoint, a journaled dynamic endpoint, or another live
// session. Bypasses the dynconfig journal: the registration dies with the
// expose session and must not survive an agent restart.
//
// All state mutations (backend map, ownership map, local route) happen under
// forwardEndpointsMu so a concurrent Unregister cannot leave an orphan route
// pointing at a removed backend.
func (a *Agent) RegisterEphemeralForwardKey(key, owner string, b forward.Backend) error {
	if key == "" {
		return fmt.Errorf("key is required")
	}
	if owner == "" {
		return fmt.Errorf("owner is required")
	}
	if b == nil {
		return fmt.Errorf("backend is required")
	}

	a.forwardEndpointsMu.Lock()
	defer a.forwardEndpointsMu.Unlock()

	if _, isConfig := a.configForwardEndpoints[key]; isConfig {
		return fmt.Errorf("endpoint %q is a config endpoint", key)
	}
	if _, isDynamic := a.dynamicForwardEndpoints[key]; isDynamic {
		return fmt.Errorf("endpoint %q is a dynamic endpoint", key)
	}
	if existing, claimed := a.ephemeralForwardEndpoints[key]; claimed {
		return fmt.Errorf("endpoint %q is already held by another expose session %s", key, existing)
	}
	if err := a.forwardHandler.AddBackend(key, b); err != nil {
		return err
	}
	a.ephemeralForwardEndpoints[key] = owner
	a.routeMgr.AddLocalForwardRoute(key, b.Target(), 0)
	return nil
}

// UnregisterEphemeralForwardKey removes a previously-registered ephemeral
// forward endpoint. The owner check ensures a stale unregister cannot drop
// another session's key. No-op if the key is not held by owner.
func (a *Agent) UnregisterEphemeralForwardKey(key, owner string) {
	a.forwardEndpointsMu.Lock()
	current, claimed := a.ephemeralForwardEndpoints[key]
	if !claimed || current != owner {
		a.forwardEndpointsMu.Unlock()
		return
	}
	target := ""
	if r := a.routeMgr.LookupForward(key); r != nil {
		target = r.Target
	}
	delete(a.ephemeralForwardEndpoints, key)
	a.forwardHandler.RemoveBackend(key)
	a.routeMgr.RemoveLocalForwardRoute(key)
	a.forwardEndpointsMu.Unlock()

	// Flood outside the mutex; sender path may block on peer IO.
	a.flooder.WithdrawLocalForwardRoute(key, target)
}

// handleForwardEndpointManage processes a ControlTypeForwardEndpointManage control request.
func (a *Agent) handleForwardEndpointManage(data []byte) ([]byte, bool) {
	var req struct {
		Action string `json:"action"`
		Key    string `json:"key"`
		Target string `json:"target"`
	}
	if err := json.Unmarshal(data, &req); err != nil {
		resp, _ := json.Marshal(map[string]string{"error": "invalid request: " + err.Error()})
		return resp, false
	}

	if err := a.verifyActionAuth(req.Action, crypto.OpForwardEndpointAdd, crypto.OpForwardEndpointRemove, data); err != nil {
		resp, _ := json.Marshal(map[string]string{"error": err.Error()})
		return resp, false
	}

	result, err := a.ManageForwardEndpoint(req.Action, req.Key, req.Target)
	if err != nil {
		resp, _ := json.Marshal(map[string]string{"error": err.Error()})
		return resp, false
	}

	resp, _ := json.Marshal(result)
	return resp, true
}

// ManagePeer handles dynamic peer connection management (add/remove/list).
// Implements the health.PeerManageProvider interface.
//
// Dynamic peers are ephemeral: they are stored in-memory only and do NOT
// survive an agent restart. TLS settings are inherited from the agent's
// global config. The optional tcpKeepAlive duration string overrides
// connections.tcp_keepalive for this peer. Peers are uniquely keyed by
// dial address.
func (a *Agent) ManagePeer(action, address, transportType, expectedID, tcpKeepAlive string) (*health.PeerManageResult, error) {
	switch action {
	case "add":
		if address == "" {
			return nil, fmt.Errorf("address is required")
		}
		if transportType == "" {
			transportType = string(transport.TransportQUIC)
		}

		// Parse optional per-peer keepalive override (empty = inherit global).
		var keepAlive *time.Duration
		if tcpKeepAlive != "" {
			d, err := time.ParseDuration(tcpKeepAlive)
			if err != nil {
				return nil, fmt.Errorf("invalid tcp_keepalive %q: %w", tcpKeepAlive, err)
			}
			if d < 0 {
				return nil, fmt.Errorf("tcp_keepalive must not be negative")
			}
			keepAlive = &d
		}
		switch transport.TransportType(transportType) {
		case transport.TransportQUIC, transport.TransportHTTP2, transport.TransportWebSocket:
		default:
			return nil, fmt.Errorf("unknown transport %q (expected quic, h2, or ws)", transportType)
		}
		// Validate expected ID up-front so the caller gets immediate feedback.
		// Empty or "auto" means accept any peer ID.
		if expectedID != "" && expectedID != "auto" {
			if _, err := identity.ParseAgentID(expectedID); err != nil {
				return nil, fmt.Errorf("invalid peer id: %w", err)
			}
		}

		if a.peers.IsConfig(address) {
			return nil, fmt.Errorf("peer %q is a config peer and cannot be replaced", address)
		}
		wasDynamic := a.peers.IsDynamic(address)
		a.peers.PutDynamic(address, struct{}{})

		// Replacing an existing dynamic peer: tear down the live connection and
		// cancel any pending reconnect before the new dial below.
		if wasDynamic {
			a.peerMgr.RemovePeerAndDisconnect(address)
		}

		// Dial in a background goroutine so the HTTP/control caller returns
		// promptly. connectToPeer handles PeerInfo registration, TLS setup
		// using the global config (empty TLSConfig = pure inheritance),
		// transport selection, and persistent reconnection on failure.
		a.wg.Add(1)
		go a.connectToPeer(config.PeerConfig{
			Address:      address,
			Transport:    transportType,
			ID:           expectedID,
			TCPKeepAlive: keepAlive,
		})

		a.persistEvent(dynconfig.Event{
			Op:           dynconfig.OpPeerAdd,
			Address:      address,
			Transport:    transportType,
			ExpectedID:   expectedID,
			TCPKeepAlive: tcpKeepAlive,
		})

		return &health.PeerManageResult{
			Status:  "ok",
			Message: fmt.Sprintf("peer %s added (transport=%s)", address, transportType),
		}, nil

	case "remove":
		if address == "" {
			return nil, fmt.Errorf("address is required")
		}

		if a.peers.IsConfig(address) {
			return nil, fmt.Errorf("peer %q is a config peer and cannot be removed", address)
		}
		if !a.peers.IsDynamic(address) {
			return nil, fmt.Errorf("dynamic peer %q not found", address)
		}
		a.peers.DeleteDynamic(address)

		remoteID, closed := a.peerMgr.RemovePeerAndDisconnect(address)
		msg := fmt.Sprintf("peer %s removed", address)
		if closed {
			msg = fmt.Sprintf("peer %s removed (disconnected %s)", address, remoteID.ShortString())
		}

		a.persistEvent(dynconfig.Event{
			Op:      dynconfig.OpPeerRemove,
			Address: address,
		})

		return &health.PeerManageResult{
			Status:  "ok",
			Message: msg,
		}, nil

	case "list":
		dynamicSet := a.peers.DynamicSnapshot()

		infos := a.peerMgr.GetPeerInfos()
		connectedBy := a.peerMgr.ConnectedByAddr()
		entries := make([]health.PeerManageResultEntry, 0, len(infos))
		outboundIDs := make(map[identity.AgentID]struct{}, len(connectedBy))
		for addr, info := range infos {
			// PeerInfo.Transport is nil for the default (QUIC); only set for h2/ws.
			transportName := string(transport.TransportQUIC)
			if info.Transport != nil {
				transportName = string(info.Transport.Type())
			}
			var remoteIDStr string
			var connected bool
			if id, ok := connectedBy[addr]; ok {
				connected = true
				remoteIDStr = id.String()
				outboundIDs[id] = struct{}{}
			}
			var expectedIDStr string
			if !info.ExpectedID.IsZero() {
				expectedIDStr = info.ExpectedID.String()
			}
			_, isDyn := dynamicSet[addr]
			entries = append(entries, health.PeerManageResultEntry{
				Address:    addr,
				Transport:  transportName,
				ExpectedID: expectedIDStr,
				RemoteID:   remoteIDStr,
				Connected:  connected,
				Persistent: info.Persistent,
				Dynamic:    isDyn,
			})
		}
		// Inbound entries leave Persistent/Dynamic/ExpectedID zero-valued -- those
		// fields are config-driven concepts that don't apply to accepted peers.
		// Skip any RemoteID already accounted for outbound, in case the peer
		// briefly appears under both directions during a handshake-collision swap
		// between ConnectedByAddr and AcceptedConnections lock windows.
		for _, in := range a.peerMgr.AcceptedConnections() {
			if _, dup := outboundIDs[in.RemoteID]; dup {
				continue
			}
			entries = append(entries, health.PeerManageResultEntry{
				Address:   in.RemoteAddr,
				Transport: string(in.Transport),
				RemoteID:  in.RemoteID.String(),
				Connected: true,
				Direction: health.PeerDirectionInbound,
			})
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Address < entries[j].Address })

		return &health.PeerManageResult{
			Status: "ok",
			Peers:  entries,
		}, nil

	default:
		return nil, fmt.Errorf("unknown action %q (expected add, remove, or list)", action)
	}
}

// handleStopRequest processes a ControlTypeStop control request. The JSON
// response is returned before the shutdown actually runs -- RequestShutdown
// only closes the channel the run loop is watching -- so the originating
// operator sees confirmation.
func (a *Agent) handleStopRequest(data []byte) ([]byte, bool) {
	if err := a.VerifyMutationBody(crypto.OpStop, data); err != nil {
		resp, _ := json.Marshal(map[string]string{"error": err.Error()})
		return resp, false
	}
	a.RequestShutdown()
	resp, _ := json.Marshal(map[string]string{
		"status":  "shutting-down",
		"message": "agent will exit shortly",
	})
	return resp, true
}

// handlePeerManage processes a ControlTypePeerManage control request.
func (a *Agent) handlePeerManage(data []byte) ([]byte, bool) {
	var req struct {
		Action       string `json:"action"`
		Address      string `json:"address"`
		Transport    string `json:"transport"`
		ID           string `json:"id"`
		TCPKeepAlive string `json:"tcp_keepalive"`
	}
	if err := json.Unmarshal(data, &req); err != nil {
		resp, _ := json.Marshal(map[string]string{"error": "invalid request: " + err.Error()})
		return resp, false
	}

	if err := a.verifyActionAuth(req.Action, crypto.OpPeerAdd, crypto.OpPeerRemove, data); err != nil {
		resp, _ := json.Marshal(map[string]string{"error": err.Error()})
		return resp, false
	}

	result, err := a.ManagePeer(req.Action, req.Address, req.Transport, req.ID, req.TCPKeepAlive)
	if err != nil {
		resp, _ := json.Marshal(map[string]string{"error": err.Error()})
		return resp, false
	}

	resp, _ := json.Marshal(result)
	return resp, true
}

// buildDynamicListenerSpec turns a listener-management request into the
// config.ListenerConfig used to start a listener. Dynamic listeners carry no
// per-listener TLS material: a zero TLS block makes startListener fall back to
// the agent's global cert/key (or an auto-generated self-signed cert) and the
// global mTLS setting.
func (a *Agent) buildDynamicListenerSpec(req *health.ListenerManageRequest) (config.ListenerConfig, error) {
	if req.Address == "" {
		return config.ListenerConfig{}, fmt.Errorf("address is required")
	}
	transportType := req.Transport
	if transportType == "" {
		transportType = string(transport.TransportQUIC)
	}
	spec := config.ListenerConfig{
		Transport: transportType,
		Address:   req.Address,
		Path:      req.Path,
		PlainText: req.PlainText,
	}
	if req.AdvertiseAddress != "" {
		spec.Advertise = &config.AdvertiseConfig{
			Address:   req.AdvertiseAddress,
			Path:      req.AdvertisePath,
			Transport: req.AdvertiseTransport,
		}
	}
	return spec, nil
}

// ManageListener handles dynamic transport listener management (add/remove/list).
// Implements the health.ListenerManageProvider interface.
//
// Dynamic listeners are uniquely keyed by listen address. They reuse the
// agent's global TLS config and mTLS setting (no per-listener cert overrides at
// runtime; --plaintext is the only TLS-affecting knob, for ws reverse-proxy
// setups). They are persisted to the journal so they survive restart, and are
// restored across sleep/wake from the listeners registry.
func (a *Agent) ManageListener(req *health.ListenerManageRequest) (*health.ListenerManageResult, error) {
	switch req.Action {
	case "add":
		spec, err := a.buildDynamicListenerSpec(req)
		if err != nil {
			return nil, err
		}
		if err := a.cfg.ValidateListenerConfig(spec); err != nil {
			return nil, err
		}
		if a.listeners.IsConfig(spec.Address) {
			return nil, fmt.Errorf("listener %q is a config listener and cannot be replaced", spec.Address)
		}

		// Replacing an existing dynamic listener: deregister and stop the old
		// socket first so the rebind below does not collide. Removing the
		// registry entry up-front keeps the address clean if the rebind fails.
		if a.listeners.IsDynamic(spec.Address) {
			a.listeners.DeleteDynamic(spec.Address)
			a.stopLiveListener(spec.Address)
		}

		if err := a.startListener(spec); err != nil {
			return nil, fmt.Errorf("start listener: %w", err)
		}
		a.listeners.PutDynamic(spec.Address, spec)
		a.TriggerNodeInfoAdvertise()
		a.persistEvent(listenerAddEvent(spec))

		return &health.ListenerManageResult{
			Status:  "ok",
			Message: fmt.Sprintf("listener %s added (transport=%s)", spec.Address, spec.Transport),
		}, nil

	case "remove":
		if req.Address == "" {
			return nil, fmt.Errorf("address is required")
		}
		if a.listeners.IsConfig(req.Address) {
			return nil, fmt.Errorf("listener %q is a config listener and cannot be removed", req.Address)
		}
		if !a.listeners.IsDynamic(req.Address) {
			return nil, fmt.Errorf("dynamic listener %q not found", req.Address)
		}
		a.listeners.DeleteDynamic(req.Address)
		a.stopLiveListener(req.Address)
		a.TriggerNodeInfoAdvertise()
		a.persistEvent(dynconfig.Event{
			Op:      dynconfig.OpListenerRemove,
			Address: req.Address,
		})

		return &health.ListenerManageResult{
			Status:  "ok",
			Message: fmt.Sprintf("listener %s removed", req.Address),
		}, nil

	case "list":
		all := a.listeners.All()
		entries := make([]health.ListenerManageResultEntry, 0, len(all))
		for addr, spec := range all {
			entry := health.ListenerManageResultEntry{
				Transport: spec.Transport,
				Address:   addr,
				Path:      spec.Path,
				PlainText: spec.PlainText,
				Dynamic:   a.listeners.IsDynamic(addr),
			}
			if spec.Advertise != nil {
				entry.AdvertiseAddress = spec.Advertise.Address
			}
			entries = append(entries, entry)
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Address < entries[j].Address })

		return &health.ListenerManageResult{
			Status:    "ok",
			Listeners: entries,
		}, nil

	default:
		return nil, fmt.Errorf("unknown action %q (expected add, remove, or list)", req.Action)
	}
}

// handleListenerManage processes a ControlTypeListenerManage control request.
func (a *Agent) handleListenerManage(data []byte) ([]byte, bool) {
	var req health.ListenerManageRequest
	if err := json.Unmarshal(data, &req); err != nil {
		resp, _ := json.Marshal(map[string]string{"error": "invalid request: " + err.Error()})
		return resp, false
	}

	if err := a.verifyActionAuth(req.Action, crypto.OpListenerAdd, crypto.OpListenerRemove, data); err != nil {
		resp, _ := json.Marshal(map[string]string{"error": err.Error()})
		return resp, false
	}

	result, err := a.ManageListener(&req)
	if err != nil {
		resp, _ := json.Marshal(map[string]string{"error": err.Error()})
		return resp, false
	}

	resp, _ := json.Marshal(result)
	return resp, true
}

// ManageDisplayName handles dynamic display name management (set/get).
// Implements the health.DisplayNameManageProvider interface.
func (a *Agent) ManageDisplayName(action, name string) (*health.DisplayNameManageResult, error) {
	switch action {
	case "set":
		// Same limit the config path enforces: the name rides in PEER_HELLO
		// and NODE_INFO behind a 1-byte length prefix, and the encoder would
		// otherwise clamp it silently.
		if len(name) > protocol.MaxStringLen {
			return nil, fmt.Errorf("display name is %d bytes, maximum is %d", len(name), protocol.MaxStringLen)
		}

		a.dynamicDisplayNameMu.Lock()
		a.dynamicDisplayName = name
		a.dynamicDisplayNameMu.Unlock()

		advertiseName := a.displayNameForAdvertise()
		displayName := a.DisplayName()

		a.flooder.SetLocalDisplayName(advertiseName)
		a.routeMgr.SetDisplayName(a.id, displayName)

		a.TriggerRouteAdvertise()
		a.TriggerNodeInfoAdvertise()

		a.persistEvent(dynconfig.Event{
			Op:   dynconfig.OpDisplayNameSet,
			Name: name,
		})

		return &health.DisplayNameManageResult{
			Status:  "ok",
			Message: fmt.Sprintf("display name set to %q", displayName),
			Name:    displayName,
		}, nil

	case "get":
		return &health.DisplayNameManageResult{
			Status: "ok",
			Name:   a.DisplayName(),
		}, nil

	default:
		return nil, fmt.Errorf("unknown action %q (expected set or get)", action)
	}
}

// handleDisplayNameManage processes a ControlTypeDisplayNameManage control request.
func (a *Agent) handleDisplayNameManage(data []byte) ([]byte, bool) {
	var req struct {
		Action string `json:"action"`
		Name   string `json:"name"`
	}
	if err := json.Unmarshal(data, &req); err != nil {
		resp, _ := json.Marshal(map[string]string{"error": "invalid request: " + err.Error()})
		return resp, false
	}

	if err := a.verifySetActionAuth(req.Action, crypto.OpDisplayNameSet, data); err != nil {
		resp, _ := json.Marshal(map[string]string{"error": err.Error()})
		return resp, false
	}

	result, err := a.ManageDisplayName(req.Action, req.Name)
	if err != nil {
		resp, _ := json.Marshal(map[string]string{"error": err.Error()})
		return resp, false
	}

	resp, _ := json.Marshal(result)
	return resp, true
}

// BrowseFiles handles file browsing requests (list, stat, roots). Used on
// the local path when a browse request targets this agent directly; the
// HTTP gateway doesn't attach a signature, so stamp one here so the
// stream handler's verifier sees an authorized request when we have the
// private key. Remote-dispatched browses come in through handleFileBrowse
// with the envelope already signed by the originating agent.
//
// Implements the health.FileBrowseProvider interface.
func (a *Agent) BrowseFiles(req *filetransfer.BrowseRequest) *filetransfer.BrowseResponse {
	if a.fileStreamHandler == nil {
		return &filetransfer.BrowseResponse{Error: "file transfer is disabled"}
	}
	if req.Auth == nil {
		env, err := a.signManagementEnvelope(a.id, crypto.OpFile, nil)
		if err != nil {
			return &filetransfer.BrowseResponse{
				Error:     err.Error(),
				ErrorCode: filetransfer.ErrCodeAuthRequired,
			}
		}
		req.Auth = env
	}
	return a.fileStreamHandler.Browse(req)
}

// handleFileBrowse handles a file browse control request from the control channel.
func (a *Agent) handleFileBrowse(data []byte) ([]byte, bool) {
	var req filetransfer.BrowseRequest
	if err := json.Unmarshal(data, &req); err != nil {
		resp, _ := json.Marshal(map[string]string{"error": "invalid request: " + err.Error()})
		return resp, false
	}

	result := a.BrowseFiles(&req)
	resp, _ := json.Marshal(result)
	return resp, result.Error == ""
}

// routeAdvertiseLoop periodically announces local routes to peers.
// Also responds to manual triggers for immediate advertisement.
// Cleans up stale routes that haven't been refreshed within route_ttl.
func (a *Agent) routeAdvertiseLoop() {
	defer a.wg.Done()
	defer recovery.RecoverWithLog(a.logger, "routeAdvertiseLoop")

	interval := a.cfg.Routing.AdvertiseInterval
	if interval <= 0 {
		interval = 2 * time.Minute // Default if not configured
	}

	// Route TTL for cleanup (use config value or default to 5x advertise interval)
	routeTTL := a.cfg.Routing.RouteTTL
	if routeTTL <= 0 {
		routeTTL = interval * 5
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	a.logger.Debug("route advertise loop started",
		"interval", interval,
		"route_ttl", routeTTL)

	for {
		select {
		case <-a.stopCh:
			return
		case <-ticker.C:
			// Clean up stale routes before advertising
			if removed := a.routeMgr.CleanupStaleRoutes(routeTTL); removed > 0 {
				a.logger.Debug("cleaned up stale routes",
					"removed", removed,
					"ttl", routeTTL)
			}

			if removed := a.routeMgr.CleanupStaleDomainRoutes(routeTTL); removed > 0 {
				a.logger.Debug("cleaned up stale domain routes",
					"removed", removed,
					"ttl", routeTTL)
			}

			if removed := a.routeMgr.CleanupStaleForwardRoutes(routeTTL); removed > 0 {
				a.logger.Debug("cleaned up stale forward routes",
					"removed", removed,
					"ttl", routeTTL)
			}

			if removed := a.routeMgr.CleanupStaleAgentRoutes(routeTTL); removed > 0 {
				a.logger.Debug("cleaned up stale agent routes",
					"removed", removed,
					"ttl", routeTTL)
			}

			// Clean up stale forwarded control requests to prevent memory leaks
			// when remote agents crash before responding.
			a.controlMu.Lock()
			staleCount := 0
			for id, req := range a.forwardedControl {
				if time.Since(req.CreatedAt) > 60*time.Second {
					delete(a.forwardedControl, id)
					staleCount++
				}
			}

			// Clean up stale pending control requests (locally initiated)
			// to prevent unbounded map growth if responses never arrive.
			staleLocalCount := 0
			for id, req := range a.pendingControl {
				if time.Now().After(req.Timeout) {
					delete(a.pendingControl, id)
					staleLocalCount++
				}
			}
			a.controlMu.Unlock()
			if staleCount > 0 {
				a.logger.Debug("cleaned up stale forwarded control requests",
					"removed", staleCount)
			}
			if staleLocalCount > 0 {
				a.logger.Debug("cleaned up stale pending control requests",
					"removed", staleLocalCount)
			}

			a.flooder.AnnounceLocalRoutes()
			a.logger.Debug("periodic route advertisement sent")
		case <-a.routeAdvertiseCh:
			a.flooder.AnnounceLocalRoutes()
			a.logger.Debug("triggered route advertisement sent")
		}
	}
}

// nodeInfoAdvertiseLoop periodically announces local node info to peers.
// All nodes advertise their info, not just exit nodes.
// Also cleans up stale node info from agents that haven't advertised recently.
func (a *Agent) nodeInfoAdvertiseLoop() {
	defer a.wg.Done()
	defer recovery.RecoverWithLog(a.logger, "nodeInfoAdvertiseLoop")

	// Use NodeInfoInterval if set, otherwise fall back to AdvertiseInterval
	interval := a.cfg.Routing.NodeInfoInterval
	if interval <= 0 {
		interval = a.cfg.Routing.AdvertiseInterval
	}
	if interval <= 0 {
		interval = 2 * time.Minute // Default if not configured
	}

	// Node info TTL is 5x the advertise interval (gives agents time to miss a few advertisements)
	nodeInfoTTL := interval * 5

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	a.logger.Debug("node info advertise loop started",
		"interval", interval,
		"node_info_ttl", nodeInfoTTL)

	for {
		select {
		case <-a.stopCh:
			return
		case <-ticker.C:
			// Clean up stale node info before advertising
			if removed := a.routeMgr.CleanupStaleNodeInfo(nodeInfoTTL); removed > 0 {
				a.logger.Debug("cleaned up stale node info entries",
					"removed", removed,
					"ttl", nodeInfoTTL)
			}

			// Collect and announce local node info with current peer connections
			info := sysinfo.Collect(a.displayNameForAdvertise(), a.getPeerConnectionInfo(), a.keypair.PublicKey, a.getUDPConfig(), a.getForwardConfig(), a.getFileTransferConfig(), a.getShellConfig(), a.getICMPConfig(), a.getAdvertisedListeners())
			a.flooder.AnnounceLocalNodeInfo(info)
			a.logger.Debug("periodic node info advertisement sent",
				"display_name", info.DisplayName,
				"hostname", info.Hostname,
				"peers", len(info.Peers))
		case <-a.nodeInfoAdvertiseCh:
			// Triggered re-advertisement (e.g., after dynamic forward listener change)
			info := sysinfo.Collect(a.displayNameForAdvertise(), a.getPeerConnectionInfo(), a.keypair.PublicKey, a.getUDPConfig(), a.getForwardConfig(), a.getFileTransferConfig(), a.getShellConfig(), a.getICMPConfig(), a.getAdvertisedListeners())
			a.flooder.AnnounceLocalNodeInfo(info)
			a.logger.Debug("triggered node info advertisement sent",
				"display_name", info.DisplayName,
				"hostname", info.Hostname,
				"peers", len(info.Peers))
		}
	}
}

// processFrame dispatches incoming frames to the appropriate handler.
func (a *Agent) processFrame(peerID identity.AgentID, frame *protocol.Frame) {
	switch frame.Type {
	case protocol.FrameStreamOpen:
		a.handleStreamOpen(peerID, frame)
	case protocol.FrameStreamOpenAck:
		a.handleStreamOpenAck(peerID, frame)
	case protocol.FrameStreamOpenErr:
		a.handleStreamOpenErr(peerID, frame)
	case protocol.FrameStreamData:
		a.handleStreamData(peerID, frame)
	case protocol.FrameStreamClose:
		a.handleStreamClose(peerID, frame)
	case protocol.FrameStreamReset:
		a.handleStreamReset(peerID, frame)
	case protocol.FrameRouteAdvertise:
		a.handleRouteAdvertise(peerID, frame)
	case protocol.FrameRouteWithdraw:
		a.handleRouteWithdraw(peerID, frame)
	case protocol.FrameNodeInfoAdvertise:
		a.handleNodeInfoAdvertise(peerID, frame)
	case protocol.FrameKeepalive:
		a.handleKeepalive(peerID, frame)
	case protocol.FrameKeepaliveAck:
		a.handleKeepaliveAck(peerID, frame)
	case protocol.FrameControlRequest:
		a.handleControlRequest(peerID, frame)
	case protocol.FrameControlResponse:
		a.handleControlResponse(peerID, frame)
	// UDP frames
	case protocol.FrameUDPOpen:
		a.handleUDPOpen(peerID, frame)
	case protocol.FrameUDPOpenAck:
		a.handleUDPOpenAck(peerID, frame)
	case protocol.FrameUDPOpenErr:
		a.handleUDPOpenErr(peerID, frame)
	case protocol.FrameUDPDatagram:
		a.handleUDPDatagram(peerID, frame)
	case protocol.FrameUDPClose:
		a.handleUDPClose(peerID, frame)
	// ICMP frames
	case protocol.FrameICMPOpen:
		a.handleICMPOpen(peerID, frame)
	case protocol.FrameICMPOpenAck:
		a.handleICMPOpenAck(peerID, frame)
	case protocol.FrameICMPOpenErr:
		a.handleICMPOpenErr(peerID, frame)
	case protocol.FrameICMPEcho:
		a.handleICMPEcho(peerID, frame)
	case protocol.FrameICMPClose:
		a.handleICMPClose(peerID, frame)
	// Sleep/Wake frames
	case protocol.FrameSleepCommand:
		a.handleSleepCommand(peerID, frame)
	case protocol.FrameWakeCommand:
		a.handleWakeCommand(peerID, frame)
	case protocol.FrameQueuedState:
		a.handleQueuedState(peerID, frame)
	}
}

// handleStreamOpen processes a STREAM_OPEN request.
func (a *Agent) handleStreamOpen(peerID identity.AgentID, frame *protocol.Frame) {
	open, err := protocol.DecodeStreamOpen(frame.Payload)
	if err != nil {
		a.logger.Debug("failed to decode stream open",
			logging.KeyPeerID, peerID.ShortString(),
			logging.KeyError, err)
		return
	}

	// Check if we are the exit node (path is empty or we're the target)
	if len(open.RemainingPath) == 0 || (len(open.RemainingPath) == 1 && open.RemainingPath[0] == a.id) {
		// Check if this is a file transfer or shell stream
		if open.AddressType == protocol.AddrTypeDomain {
			destAddr := addressToString(open.AddressType, open.Address)
			if destAddr == protocol.FileTransferUpload {
				a.handleFileUploadStreamOpen(peerID, frame.StreamID, open.RequestID, open.EphemeralPubKey)
				return
			}
			if destAddr == protocol.FileTransferDownload {
				a.handleFileDownloadStreamOpen(peerID, frame.StreamID, open.RequestID, open.EphemeralPubKey)
				return
			}
			// Shell streams
			if destAddr == protocol.ShellStream {
				a.handleShellStreamOpen(peerID, frame.StreamID, open.RequestID, false, open.EphemeralPubKey)
				return
			}
			if destAddr == protocol.ShellInteractive {
				a.handleShellStreamOpen(peerID, frame.StreamID, open.RequestID, true, open.EphemeralPubKey)
				return
			}
			// Forward streams (port forwarding)
			if strings.HasPrefix(destAddr, protocol.ForwardStreamPrefix) {
				key := strings.TrimPrefix(destAddr, protocol.ForwardStreamPrefix)
				if a.forwardHandler != nil {
					ctx := context.Background()
					a.forwardHandler.HandleStreamOpen(ctx, frame.StreamID, open.RequestID, peerID, key, open.EphemeralPubKey)
				} else {
					// No forward handler - send error
					errPayload := &protocol.StreamOpenErr{
						RequestID: open.RequestID,
						ErrorCode: protocol.ErrForwardNotFound,
						Message:   "forward key not configured",
					}
					errFrame := &protocol.Frame{
						Type:     protocol.FrameStreamOpenErr,
						StreamID: frame.StreamID,
						Payload:  errPayload.Encode(),
					}
					a.peerMgr.SendToPeer(peerID, errFrame)
				}
				return
			}
		}

		// We are the exit node for TCP traffic
		if a.exitHandler != nil {
			ctx := context.Background()
			// Convert address bytes to string based on address type
			destAddr := addressToString(open.AddressType, open.Address)
			a.exitHandler.HandleStreamOpen(ctx, frame.StreamID, open.RequestID, peerID, destAddr, open.Port, open.EphemeralPubKey)
		}
		return
	}

	// Forward to next hop
	nextHop := open.RemainingPath[0]

	// Get connection to next hop
	conn := a.peerMgr.GetPeer(nextHop)
	if conn == nil {
		// No route to next hop, send error back
		errPayload := &protocol.StreamOpenErr{
			RequestID: open.RequestID,
			ErrorCode: protocol.ErrHostUnreachable,
			Message:   "no route to next hop",
		}
		errFrame := &protocol.Frame{
			Type:     protocol.FrameStreamOpenErr,
			StreamID: frame.StreamID,
			Payload:  errPayload.Encode(),
		}
		a.peerMgr.SendToPeer(peerID, errFrame)
		return
	}

	// Generate new downstream stream ID
	downstreamID := conn.NextStreamID()

	// Create relay entry
	relay := &relayEntry{
		UpstreamPeer:   peerID,
		UpstreamID:     frame.StreamID,
		DownstreamPeer: nextHop,
		DownstreamID:   downstreamID,
	}
	a.tcpRelay.Insert(relay)

	// Update remaining path (remove the next hop)
	newPath := open.RemainingPath[1:]

	// Build forwarded STREAM_OPEN (preserve ephemeral key for E2E encryption)
	fwdOpen := &protocol.StreamOpen{
		RequestID:       open.RequestID,
		AddressType:     open.AddressType,
		Address:         open.Address,
		Port:            open.Port,
		RemainingPath:   newPath,
		EphemeralPubKey: open.EphemeralPubKey,
	}

	fwdFrame := &protocol.Frame{
		Type:     protocol.FrameStreamOpen,
		StreamID: downstreamID,
		Payload:  fwdOpen.Encode(),
	}

	if err := a.peerMgr.SendToPeer(nextHop, fwdFrame); err != nil {
		// Clean up relay entry on failure
		a.tcpRelay.Delete(relay)

		// Send error back
		errPayload := &protocol.StreamOpenErr{
			RequestID: open.RequestID,
			ErrorCode: protocol.ErrConnectionRefused,
			Message:   err.Error(),
		}
		errFrame := &protocol.Frame{
			Type:     protocol.FrameStreamOpenErr,
			StreamID: frame.StreamID,
			Payload:  errPayload.Encode(),
		}
		a.peerMgr.SendToPeer(peerID, errFrame)
	}
}

// addressToString converts address bytes to a string representation.
func addressToString(addrType uint8, addr []byte) string {
	switch addrType {
	case protocol.AddrTypeIPv4:
		if len(addr) >= 4 {
			return net.IP(addr[:4]).String()
		}
	case protocol.AddrTypeIPv6:
		if len(addr) >= 16 {
			return net.IP(addr[:16]).String()
		}
	case protocol.AddrTypeDomain:
		// Domain addresses have length prefix byte that needs to be skipped
		if len(addr) > 0 {
			return string(addr[1:])
		}
	}
	return ""
}

// handleStreamOpenAck processes a STREAM_OPEN_ACK.
func (a *Agent) handleStreamOpenAck(peerID identity.AgentID, frame *protocol.Frame) {
	// Check if this is a relay stream response - ACK comes from downstream.
	// Relay entries are immutable once inserted, so reading fields after the
	// LookupDownstream RLock returns is safe even though the entry could be
	// removed concurrently.
	if relay := a.tcpRelay.LookupDownstream(peerID, frame.StreamID); relay != nil {
		// Forward ACK to upstream with upstream stream ID
		fwdFrame := &protocol.Frame{
			Type:     protocol.FrameStreamOpenAck,
			StreamID: relay.UpstreamID,
			Payload:  frame.Payload,
		}
		a.peerMgr.SendToPeer(relay.UpstreamPeer, fwdFrame)
		return
	}

	ack, err := protocol.DecodeStreamOpenAck(frame.Payload)
	if err != nil {
		return
	}

	// Convert bound address bytes to net.IP
	var boundIP net.IP
	if len(ack.BoundAddr) > 0 {
		boundIP = net.IP(ack.BoundAddr)
	}

	a.streamMgr.HandleStreamOpenAck(peerID, ack.RequestID, boundIP, ack.BoundPort, ack.EphemeralPubKey)
}

// handleStreamOpenErr processes a STREAM_OPEN_ERR.
func (a *Agent) handleStreamOpenErr(peerID identity.AgentID, frame *protocol.Frame) {
	// Check if this is a relay stream response - ERR comes from downstream
	if relay := a.tcpRelay.PopDownstream(peerID, frame.StreamID); relay != nil {
		// Forward error to upstream with upstream stream ID
		fwdFrame := &protocol.Frame{
			Type:     protocol.FrameStreamOpenErr,
			StreamID: relay.UpstreamID,
			Payload:  frame.Payload,
		}
		a.peerMgr.SendToPeer(relay.UpstreamPeer, fwdFrame)
		return
	}

	errPayload, err := protocol.DecodeStreamOpenErr(frame.Payload)
	if err != nil {
		a.logger.Debug("failed to decode stream open error",
			logging.KeyPeerID, peerID.ShortString(),
			logging.KeyError, err)
		return
	}

	a.logger.Debug("stream open failed",
		logging.KeyStreamID, frame.StreamID,
		logging.KeyRequestID, errPayload.RequestID,
		"error_code", errPayload.ErrorCode,
		"message", errPayload.Message)

	a.streamMgr.HandleStreamOpenErr(peerID, errPayload.RequestID, errPayload.ErrorCode, errPayload.Message)
}

// failTCPRelay handles a failed relay forward: the relay entry is removed and
// both ends receive a STREAM_RESET so neither endpoint keeps writing into a
// hole. Without this the surviving frames would still decrypt (the E2E
// high-water mark tolerates gaps) and the proxied byte stream would lose a
// chunk with nothing downstream able to detect it.
func (a *Agent) failTCPRelay(relay *relayEntry, cause error) {
	a.tcpRelay.Delete(relay)
	a.logger.Debug("relay forward failed, resetting stream both ways",
		"upstream_peer", relay.UpstreamPeer.ShortString(),
		logging.KeyStreamID, relay.UpstreamID,
		"downstream_peer", relay.DownstreamPeer.ShortString(),
		"downstream_stream_id", relay.DownstreamID,
		logging.KeyError, cause)
	payload := (&protocol.StreamReset{ErrorCode: protocol.ErrWriteFailed}).Encode()
	a.peerMgr.SendToPeer(relay.UpstreamPeer, &protocol.Frame{
		Type:     protocol.FrameStreamReset,
		StreamID: relay.UpstreamID,
		Payload:  payload,
	})
	a.peerMgr.SendToPeer(relay.DownstreamPeer, &protocol.Frame{
		Type:     protocol.FrameStreamReset,
		StreamID: relay.DownstreamID,
		Payload:  payload,
	})
}

// handleStreamData processes stream data.
func (a *Agent) handleStreamData(peerID identity.AgentID, frame *protocol.Frame) {
	// Check if this is a relay stream - could be from upstream or downstream.
	// The lookup is keyed by (peer, stream ID), which both disambiguates
	// direction and keeps two peers using the same numeric ID apart. Relay
	// entries are immutable once inserted, so reading entry fields after the
	// LookupBoth RLock returns is safe.
	upRelay, downRelay := a.tcpRelay.LookupBoth(peerID, frame.StreamID)

	// Data from upstream: the key matched on the upstream side.
	if upRelay != nil {
		// Data from upstream, forward to downstream
		fwdFrame := &protocol.Frame{
			Type:     protocol.FrameStreamData,
			StreamID: upRelay.DownstreamID,
			Flags:    frame.Flags,
			Payload:  frame.Payload,
		}
		if err := a.peerMgr.SendToPeer(upRelay.DownstreamPeer, fwdFrame); err != nil {
			a.failTCPRelay(upRelay, err)
		}
		return
	}

	// Data from downstream: the key matched on the downstream side.
	if downRelay != nil {
		// Data from downstream, forward to upstream
		fwdFrame := &protocol.Frame{
			Type:     protocol.FrameStreamData,
			StreamID: downRelay.UpstreamID,
			Flags:    frame.Flags,
			Payload:  frame.Payload,
		}
		if err := a.peerMgr.SendToPeer(downRelay.UpstreamPeer, fwdFrame); err != nil {
			a.failTCPRelay(downRelay, err)
		}
		return
	}

	// Check if this is an exit handler stream
	if a.exitHandler != nil && a.exitHandler.GetConnection(peerID, frame.StreamID) != nil {
		a.exitHandler.HandleStreamData(peerID, frame.StreamID, frame.Payload, frame.Flags)
		return
	}

	// Check if this is a forward handler stream
	if a.forwardHandler != nil {
		if err := a.forwardHandler.HandleStreamData(peerID, frame.StreamID, frame.Payload, frame.Flags); err == nil {
			return
		}
	}

	// Check if this is a file transfer stream
	if a.getFileTransferStream(peerID, frame.StreamID) != nil {
		a.handleFileTransferStreamData(peerID, frame.StreamID, frame.Payload, frame.Flags)
		return
	}

	// Check if this is a shell stream (where we are the target/server)
	if a.shellHandler != nil {
		a.shellHandler.HandleStreamData(peerID, frame.StreamID, frame.Payload, frame.Flags)
		// Note: shellHandler tracks its own streams and will ignore unknown stream IDs
	}

	// Check if this is a shell client stream (where we initiated to a management command endpoint)
	if a.handleShellClientData(peerID, frame.StreamID, frame.Payload, frame.Flags) {
		return
	}

	if err := a.streamMgr.HandleStreamData(peerID, frame.StreamID, frame.Flags, frame.Payload); err != nil {
		if !errors.Is(err, stream.ErrReadBufferFull) {
			return
		}
		// The local consumer of this stream is not reading and its buffer
		// is full. PushData drops the frame rather than blocking this
		// connection's frame drainer, so the byte stream has a hole in it
		// now: reset the stream to fail it cleanly instead of letting the
		// consumer hang on corrupt data. Other streams are unaffected.
		a.logger.Debug("stream read buffer full, resetting stream",
			logging.KeyPeerID, peerID.ShortString(),
			logging.KeyStreamID, frame.StreamID)
		a.streamMgr.RemoveStream(peerID, frame.StreamID)
		a.peerMgr.SendToPeer(peerID, &protocol.Frame{
			Type:     protocol.FrameStreamReset,
			StreamID: frame.StreamID,
			Payload:  (&protocol.StreamReset{ErrorCode: protocol.ErrResourceLimit}).Encode(),
		})
	}
}

// handleStreamClose processes a stream close.
func (a *Agent) handleStreamClose(peerID identity.AgentID, frame *protocol.Frame) {
	a.logger.Debug("handleStreamClose received",
		logging.KeyPeerID, peerID.ShortString(),
		logging.KeyStreamID, frame.StreamID)

	// Check if this is a relay stream - PopMatchingPeer atomically looks up
	// (peer, stream ID), derives the direction, and removes the entry under
	// one Lock.
	if entry, fromUpstream := a.tcpRelay.PopMatchingPeer(peerID, frame.StreamID); entry != nil {
		dstPeer, dstID := entry.UpstreamPeer, entry.UpstreamID
		if fromUpstream {
			dstPeer, dstID = entry.DownstreamPeer, entry.DownstreamID
		}
		fwdFrame := &protocol.Frame{
			Type:     protocol.FrameStreamClose,
			StreamID: dstID,
			// Preserve flags: a spec-conforming sender may combine
			// STREAM_CLOSE with FIN_WRITE or FIN_READ to half-close, and
			// rebuilding the frame without them turns every half-close into a
			// full close by the time it reaches the far end.
			Flags: frame.Flags,
		}
		// The entry is already popped and the originator has closed its side;
		// if the forward fails the far end is unreachable and will time out on
		// its own. Log so the dropped close is at least visible.
		if err := a.peerMgr.SendToPeer(dstPeer, fwdFrame); err != nil {
			a.logger.Debug("failed to forward stream close",
				logging.KeyPeerID, dstPeer.ShortString(),
				logging.KeyStreamID, dstID,
				logging.KeyError, err)
		}
		return
	}

	// Check if this is an exit handler stream
	if a.exitHandler != nil && a.exitHandler.GetConnection(peerID, frame.StreamID) != nil {
		a.exitHandler.HandleStreamClose(peerID, frame.StreamID)
		return
	}

	// Check if this is a forward handler stream
	if a.forwardHandler != nil {
		a.forwardHandler.HandleStreamClose(peerID, frame.StreamID)
	}

	// Check if this is a file transfer stream
	if a.getFileTransferStream(peerID, frame.StreamID) != nil {
		a.cleanupFileTransferStream(peerID, frame.StreamID)
		return
	}

	// Check if this is a shell stream (where we are the target/server)
	if a.shellHandler != nil {
		a.shellHandler.HandleStreamClose(peerID, frame.StreamID)
	}

	// Check if this is a shell client stream (where we initiated to a management command endpoint)
	if a.handleShellClientClose(peerID, frame.StreamID) {
		// Also clean up the stream manager entry (shell client streams are tracked in both places)
		a.streamMgr.HandleStreamClose(peerID, frame.StreamID)
		return
	}

	a.streamMgr.HandleStreamClose(peerID, frame.StreamID)
}

// handleStreamReset processes a stream reset.
func (a *Agent) handleStreamReset(peerID identity.AgentID, frame *protocol.Frame) {
	reset, err := protocol.DecodeStreamReset(frame.Payload)
	if err != nil {
		return
	}

	// Check if this is a relay stream - PopMatchingPeer atomically looks up
	// (peer, stream ID), derives the direction, and removes the entry under
	// one Lock.
	if entry, fromUpstream := a.tcpRelay.PopMatchingPeer(peerID, frame.StreamID); entry != nil {
		dstPeer, dstID := entry.UpstreamPeer, entry.UpstreamID
		if fromUpstream {
			dstPeer, dstID = entry.DownstreamPeer, entry.DownstreamID
		}
		fwdFrame := &protocol.Frame{
			Type:     protocol.FrameStreamReset,
			StreamID: dstID,
			Payload:  frame.Payload,
		}
		// Same shape as the close forward above: the entry is already popped
		// and the originator has torn down its side, so a failed forward only
		// needs to be visible in the logs.
		if err := a.peerMgr.SendToPeer(dstPeer, fwdFrame); err != nil {
			a.logger.Debug("failed to forward stream reset",
				logging.KeyPeerID, dstPeer.ShortString(),
				logging.KeyStreamID, dstID,
				logging.KeyError, err)
		}
		return
	}

	// Check if this is an exit handler stream
	if a.exitHandler != nil && a.exitHandler.GetConnection(peerID, frame.StreamID) != nil {
		a.exitHandler.HandleStreamReset(peerID, frame.StreamID, reset.ErrorCode)
		return
	}

	// Check if this is a forward handler stream
	if a.forwardHandler != nil {
		a.forwardHandler.HandleStreamReset(peerID, frame.StreamID, reset.ErrorCode)
	}

	// Check if this is a file transfer stream
	if a.getFileTransferStream(peerID, frame.StreamID) != nil {
		a.cleanupFileTransferStream(peerID, frame.StreamID)
		return
	}

	a.streamMgr.HandleStreamReset(peerID, frame.StreamID, reset.ErrorCode)
}

// handleRouteAdvertise processes a route advertisement.
func (a *Agent) handleRouteAdvertise(peerID identity.AgentID, frame *protocol.Frame) {
	adv, err := protocol.DecodeRouteAdvertise(frame.Payload)
	if err != nil {
		a.logger.Debug("failed to decode route advertise",
			logging.KeyPeerID, peerID.ShortString(),
			logging.KeyError, err)
		return
	}

	// Log with available info (path may be encrypted)
	encrypted := adv.EncPath != nil && adv.EncPath.Encrypted
	a.logger.Debug("received route advertisement",
		logging.KeyPeerID, peerID.ShortString(),
		"origin", adv.OriginAgent.ShortString(),
		logging.KeyCount, len(adv.Routes),
		"encrypted", encrypted)

	a.flooder.HandleRouteAdvertise(peerID, adv.OriginAgent, adv.OriginDisplayName, adv.Sequence, adv.Routes, adv.Comments, adv.EncPath, adv.SeenBy)
}

// handleRouteWithdraw processes a route withdrawal.
func (a *Agent) handleRouteWithdraw(peerID identity.AgentID, frame *protocol.Frame) {
	withdraw, err := protocol.DecodeRouteWithdraw(frame.Payload)
	if err != nil {
		return
	}

	a.flooder.HandleRouteWithdraw(peerID, withdraw.OriginAgent, withdraw.Sequence, withdraw.Routes, withdraw.SeenBy)
}

// handleNodeInfoAdvertise processes a node info advertisement.
func (a *Agent) handleNodeInfoAdvertise(peerID identity.AgentID, frame *protocol.Frame) {
	adv, err := protocol.DecodeNodeInfoAdvertise(frame.Payload)
	if err != nil {
		a.logger.Debug("failed to decode node info advertise",
			logging.KeyPeerID, peerID.ShortString(),
			logging.KeyError, err)
		return
	}

	// Log with available info (may be encrypted)
	encrypted := adv.EncInfo != nil && adv.EncInfo.Encrypted
	a.logger.Debug("received node info advertisement",
		logging.KeyPeerID, peerID.ShortString(),
		"origin", adv.OriginAgent.ShortString(),
		"encrypted", encrypted)

	a.flooder.HandleNodeInfoAdvertise(peerID, adv.OriginAgent, adv.Sequence, adv.EncInfo, adv.SeenBy)
}

// handleKeepalive processes a keepalive.
// Note: This is kept for completeness but is currently not called.
// The peer.Manager handles keepalive frames internally and sends acks
// before frames reach the agent's callback. See manager.go readLoop.
func (a *Agent) handleKeepalive(peerID identity.AgentID, frame *protocol.Frame) {
	ka, err := protocol.DecodeKeepalive(frame.Payload)
	if err != nil {
		return
	}

	// Send keepalive ack - uses the same Keepalive type with the original timestamp
	ackPayload := (&protocol.Keepalive{
		Timestamp: ka.Timestamp,
	}).Encode()

	ackFrame := &protocol.Frame{
		Type:     protocol.FrameKeepaliveAck,
		StreamID: protocol.ControlStreamID,
		Payload:  ackPayload,
	}

	a.peerMgr.SendToPeer(peerID, ackFrame)
}

// handleKeepaliveAck processes a keepalive ack.
// Note: This is kept for completeness but is currently not called.
// The peer.Manager handles keepalive acks internally and updates RTT
// before frames reach the agent's callback. See manager.go readLoop.
func (a *Agent) handleKeepaliveAck(peerID identity.AgentID, frame *protocol.Frame) {
	// RTT tracking is handled by peer.Manager.readLoop which calls
	// conn.UpdateRTT(ka.Timestamp) for FrameKeepaliveAck frames.
}

// handleControlRequest processes a CONTROL_REQUEST from a peer.
func (a *Agent) handleControlRequest(peerID identity.AgentID, frame *protocol.Frame) {
	req, err := protocol.DecodeControlRequest(frame.Payload)
	if err != nil {
		a.logger.Debug("failed to decode control request",
			logging.KeyPeerID, peerID.ShortString(),
			logging.KeyError, err)
		return
	}

	a.logger.Debug("received control request",
		"from", peerID.ShortString(),
		"request_id", req.RequestID,
		"type", req.ControlType,
		"target", req.TargetAgent.ShortString(),
		"path_len", len(req.Path))

	// Check if this request is for us or needs forwarding
	var zeroID identity.AgentID
	if req.TargetAgent != zeroID && req.TargetAgent != a.id {
		// Need to forward to target agent
		var nextHop identity.AgentID
		var remainingPath []identity.AgentID

		if len(req.Path) > 0 {
			// Use the path provided in the request
			nextHop = req.Path[0]
			remainingPath = req.Path[1:]
		} else {
			// Path is empty, look up route to target
			conn := a.peerMgr.GetPeer(req.TargetAgent)
			if conn != nil {
				// Target is a direct peer
				nextHop = req.TargetAgent
			} else if agentRoute := a.routeMgr.LookupAgent(req.TargetAgent); agentRoute != nil {
				// Primary: agent presence table
				nextHop = agentRoute.NextHop
				rPath := agentRoute.Path
				for i, id := range rPath {
					if id == nextHop && i+1 < len(rPath) {
						remainingPath = rPath[i+1:]
						break
					}
				}
			} else {
				// Fallback: CIDR route table (backward compat with old agents)
				routes := a.routeMgr.Table().GetRoutesFromAgent(req.TargetAgent)
				if len(routes) == 0 {
					a.sendControlResponse(peerID, req.RequestID, req.ControlType, false, []byte("no route to target"))
					return
				}
				nextHop = routes[0].NextHop
				rPath := routes[0].Path
				for i, id := range rPath {
					if id == nextHop && i+1 < len(rPath) {
						remainingPath = rPath[i+1:]
						break
					}
				}
			}
		}

		conn := a.peerMgr.GetPeer(nextHop)
		if conn == nil {
			a.sendControlResponse(peerID, req.RequestID, req.ControlType, false, []byte("next hop not connected"))
			return
		}

		// Track this forwarded request so we can route the response back,
		// under an ID of our own so two upstream peers cannot collide.
		a.controlMu.Lock()
		a.nextControlID++
		downstreamID := a.nextControlID
		a.forwardedControl[downstreamID] = &forwardedControlRequest{
			UpstreamID:   req.RequestID,
			DownstreamID: downstreamID,
			SourcePeer:   peerID,
			NextHop:      nextHop,
			CreatedAt:    time.Now(),
		}
		a.controlMu.Unlock()

		// Forward the request
		a.logger.Debug("forwarding control request",
			"to", nextHop.ShortString(),
			"target", req.TargetAgent.ShortString(),
			"remaining_path", len(remainingPath),
			"source_peer", peerID.ShortString())

		fwdReq := &protocol.ControlRequest{
			RequestID:   downstreamID,
			ControlType: req.ControlType,
			TargetAgent: req.TargetAgent,
			Path:        remainingPath,
			Data:        req.Data, // Preserve data payload for RPC and other control types
		}

		fwdFrame := &protocol.Frame{
			Type:     protocol.FrameControlRequest,
			StreamID: protocol.ControlStreamID,
			Payload:  fwdReq.Encode(),
		}
		if err := a.peerMgr.SendToPeer(nextHop, fwdFrame); err != nil {
			a.logger.Debug("failed to forward control request",
				logging.KeyPeerID, nextHop.ShortString(),
				logging.KeyError, err)
			a.controlMu.Lock()
			delete(a.forwardedControl, downstreamID)
			a.controlMu.Unlock()
			a.sendControlResponse(peerID, req.RequestID, req.ControlType, false, []byte("failed to forward: "+err.Error()))
		}
		return
	}

	// Handle locally
	var data []byte
	var success bool

	switch req.ControlType {
	case protocol.ControlTypeStatus:
		data, success = a.getLocalStatus()
	case protocol.ControlTypePeers:
		data, success = a.getLocalPeers()
	case protocol.ControlTypeRoutes:
		data, success = a.getLocalRoutes()
	case protocol.ControlTypeInterfaces:
		data, success = a.getLocalInterfaces()
	case protocol.ControlTypeRouteManage:
		data, success = a.handleRouteManage(req.Data)
	case protocol.ControlTypeRouteTrace:
		data, success = a.handleRouteTrace(req.Data)
	case protocol.ControlTypeForwardManage:
		data, success = a.handleForwardManage(req.Data)
	case protocol.ControlTypeForwardEndpointManage:
		data, success = a.handleForwardEndpointManage(req.Data)
	case protocol.ControlTypeFileBrowse:
		data, success = a.handleFileBrowse(req.Data)
	case protocol.ControlTypeDisplayNameManage:
		data, success = a.handleDisplayNameManage(req.Data)
	case protocol.ControlTypePeerManage:
		data, success = a.handlePeerManage(req.Data)
	case protocol.ControlTypeListenerManage:
		data, success = a.handleListenerManage(req.Data)
	case protocol.ControlTypeStop:
		data, success = a.handleStopRequest(req.Data)
	case protocol.ControlTypeProbe:
		// Probes do network I/O (dial + PEER_HELLO) and can take seconds.
		// Run off the peer's frame loop so other control requests from the
		// same peer are not blocked while the probe is in flight.
		reqID := req.RequestID
		reqData := req.Data
		ctrlType := req.ControlType
		a.wg.Add(1)
		go func() {
			defer a.wg.Done()
			defer recovery.RecoverWithLog(a.logger, "handleProbeRequest")
			data, success := a.handleProbeRequest(reqData)
			a.sendControlResponse(peerID, reqID, ctrlType, success, data)
		}()
		return
	default:
		data = []byte("unknown control type")
		success = false
	}

	a.sendControlResponse(peerID, req.RequestID, req.ControlType, success, data)
}

// handleControlResponse processes a CONTROL_RESPONSE from a peer. Responses
// may arrive as multiple chunks with the same RequestID -- HasMore=true means
// more are coming. Initiators accumulate before delivering to the waiter;
// forwarders relay each chunk and keep the forwarding entry alive until the
// final one arrives.
func (a *Agent) handleControlResponse(peerID identity.AgentID, frame *protocol.Frame) {
	resp, err := protocol.DecodeControlResponse(frame.Payload)
	if err != nil {
		a.logger.Debug("failed to decode control response",
			logging.KeyPeerID, peerID.ShortString(),
			logging.KeyError, err)
		return
	}

	a.logger.Debug("received control response",
		"from", peerID.ShortString(),
		"request_id", resp.RequestID,
		"success", resp.Success,
		"has_more", resp.HasMore,
		"data_len", len(resp.Data))

	// Only the peer a request was sent to may answer it. Request IDs come from
	// a sequential counter, so they are as guessable as stream IDs: without
	// this check any peer could satisfy or fail a request this agent addressed
	// to a different one. A mismatched answer is discarded without consuming
	// the entry, so it cannot deny the real answer either.
	var wrongPeer bool
	a.controlMu.Lock()
	pending := a.pendingControl[resp.RequestID]
	if pending != nil && pending.NextHop != peerID {
		pending, wrongPeer = nil, true
	}
	if pending != nil {
		pending.accumulated = append(pending.accumulated, resp.Data...)
		if !resp.HasMore {
			delete(a.pendingControl, resp.RequestID)
		}
	}

	forwarded := a.forwardedControl[resp.RequestID]
	if forwarded != nil && forwarded.NextHop != peerID {
		forwarded, wrongPeer = nil, true
	}
	if forwarded != nil && !resp.HasMore {
		delete(a.forwardedControl, resp.RequestID)
	}
	a.controlMu.Unlock()

	if wrongPeer {
		a.logger.Debug("dropping control response from a peer the request was not sent to",
			logging.KeyPeerID, peerID.ShortString(),
			"request_id", resp.RequestID)
		return
	}

	if pending != nil && pending.ResponseCh != nil && !resp.HasMore {
		final := &protocol.ControlResponse{
			RequestID:   resp.RequestID,
			ControlType: resp.ControlType,
			Success:     resp.Success,
			Data:        pending.accumulated,
		}
		select {
		case pending.ResponseCh <- final:
		default:
			// Channel full or closed, ignore
		}
		return
	}

	if forwarded != nil {
		// Relay this chunk to the source peer. Forwarders do not
		// reassemble -- the initiator at the far end does. Restore the
		// request ID the upstream peer chose; the one on the wire is ours.
		resp.RequestID = forwarded.UpstreamID
		responseFrame := &protocol.Frame{
			Type:     protocol.FrameControlResponse,
			StreamID: protocol.ControlStreamID,
			Payload:  resp.Encode(),
		}
		if err := a.peerMgr.SendToPeer(forwarded.SourcePeer, responseFrame); err != nil {
			a.logger.Debug("failed to forward control response",
				logging.KeyPeerID, forwarded.SourcePeer.ShortString(),
				logging.KeyError, err)
		}
	}
}

// sendControlResponse sends a control response to a peer, splitting into
// multiple CONTROL_RESPONSE frames with HasMore set when the payload exceeds
// a single frame. Failures (success=false) are always small and sent in one
// frame -- chunking assumes HasMore implies Success, which legacy decoders
// rely on to interpret the flags byte as a plain non-zero "ok".
func (a *Agent) sendControlResponse(peerID identity.AgentID, requestID uint64, controlType uint8, success bool, data []byte) {
	a.logger.Debug("sending control response",
		"to", peerID.ShortString(),
		"request_id", requestID,
		"success", success,
		"data_len", len(data))

	chunk := protocol.ControlResponseMaxChunkSize
	if !success || len(data) <= chunk {
		a.sendControlResponseChunk(peerID, requestID, controlType, success, false, data)
		return
	}

	for offset := 0; offset < len(data); {
		end := offset + chunk
		if end > len(data) {
			end = len(data)
		}
		a.sendControlResponseChunk(peerID, requestID, controlType, success, end < len(data), data[offset:end])
		offset = end
	}
}

func (a *Agent) sendControlResponseChunk(peerID identity.AgentID, requestID uint64, controlType uint8, success, hasMore bool, data []byte) {
	resp := &protocol.ControlResponse{
		RequestID:   requestID,
		ControlType: controlType,
		Success:     success,
		HasMore:     hasMore,
		Data:        data,
	}
	frame := &protocol.Frame{
		Type:     protocol.FrameControlResponse,
		StreamID: protocol.ControlStreamID,
		Payload:  resp.Encode(),
	}
	if err := a.peerMgr.SendToPeer(peerID, frame); err != nil {
		a.logger.Debug("failed to send control response",
			logging.KeyPeerID, peerID.ShortString(),
			logging.KeyError, err)
	}
}

// findControlPath finds the next hop and remaining path to reach a target agent for control requests.
// Returns the next hop ID and the path to include in the control request.
func (a *Agent) findControlPath(targetID identity.AgentID) (identity.AgentID, []identity.AgentID, error) {
	// Check if target is a direct peer
	if conn := a.peerMgr.GetPeer(targetID); conn != nil {
		return targetID, nil, nil
	}

	// Primary: look up in agent presence table
	if agentRoute := a.routeMgr.LookupAgent(targetID); agentRoute != nil {
		nextHop := agentRoute.NextHop
		var path []identity.AgentID
		for i, id := range agentRoute.Path {
			if id == nextHop && i+1 < len(agentRoute.Path) {
				path = make([]identity.AgentID, len(agentRoute.Path)-i-1)
				copy(path, agentRoute.Path[i+1:])
				break
			}
		}
		return nextHop, path, nil
	}

	// Fallback: find route via CIDR routing table (backward compat with old agents)
	routes := a.routeMgr.Table().GetRoutesFromAgent(targetID)
	if len(routes) == 0 {
		return identity.AgentID{}, nil, fmt.Errorf("no route to agent %s", targetID.ShortString())
	}

	route := routes[0]
	nextHop := route.NextHop

	var path []identity.AgentID
	for i, id := range route.Path {
		if id == nextHop && i+1 < len(route.Path) {
			path = make([]identity.AgentID, len(route.Path)-i-1)
			copy(path, route.Path[i+1:])
			break
		}
	}

	return nextHop, path, nil
}

// SendControlRequest sends a control request to a target agent and waits for response.
func (a *Agent) SendControlRequest(ctx context.Context, targetID identity.AgentID, controlType uint8) (*protocol.ControlResponse, error) {
	return a.SendControlRequestWithData(ctx, targetID, controlType, nil)
}

// SendControlRequestWithData sends a control request with data payload to a target agent.
func (a *Agent) SendControlRequestWithData(ctx context.Context, targetID identity.AgentID, controlType uint8, data []byte) (*protocol.ControlResponse, error) {
	nextHop, path, err := a.findControlPath(targetID)
	if err != nil {
		return nil, err
	}

	a.logger.Debug("sending control request",
		"target", targetID.ShortString(),
		"next_hop", nextHop.ShortString(),
		"path_len", len(path),
		"control_type", controlType,
		"data_len", len(data))

	// Generate request ID and create pending request
	a.controlMu.Lock()
	a.nextControlID++
	requestID := a.nextControlID
	pending := &pendingControlRequest{
		RequestID:   requestID,
		NextHop:     nextHop,
		ControlType: controlType,
		ResponseCh:  make(chan *protocol.ControlResponse, 1),
		Timeout:     time.Now().Add(30 * time.Second),
	}
	a.pendingControl[requestID] = pending
	a.controlMu.Unlock()

	// Build and send request
	req := &protocol.ControlRequest{
		RequestID:   requestID,
		ControlType: controlType,
		TargetAgent: targetID,
		Path:        path,
		Data:        data,
	}

	frame := &protocol.Frame{
		Type:     protocol.FrameControlRequest,
		StreamID: protocol.ControlStreamID,
		Payload:  req.Encode(),
	}

	if err := a.peerMgr.SendToPeer(nextHop, frame); err != nil {
		a.controlMu.Lock()
		delete(a.pendingControl, requestID)
		a.controlMu.Unlock()
		return nil, fmt.Errorf("send control request: %w", err)
	}

	// Wait for response
	select {
	case resp := <-pending.ResponseCh:
		return resp, nil
	case <-ctx.Done():
		a.controlMu.Lock()
		delete(a.pendingControl, requestID)
		a.controlMu.Unlock()
		return nil, ctx.Err()
	}
}

// getLocalStatus returns the agent's status as JSON.
func (a *Agent) getLocalStatus() ([]byte, bool) {
	stats := a.Stats()
	data, err := json.Marshal(map[string]interface{}{
		"agent_id":                  a.id.String(),
		"running":                   a.running.Load(),
		"peer_count":                stats.PeerCount,
		"stream_count":              stats.StreamCount,
		"route_count":               stats.RouteCount,
		"socks5_running":            stats.SOCKS5Running,
		"exit_running":              stats.ExitHandlerRun,
		"can_sign_management":       a.cfg.CanSign(),
		"accepts_signed_management": a.cfg.HasSigningKey(),
	})
	if err != nil {
		return []byte(err.Error()), false
	}
	return data, true
}

// getLocalPeers returns the list of connected peers.
func (a *Agent) getLocalPeers() ([]byte, bool) {
	peerIDs := a.peerMgr.GetPeerIDs()
	peers := make([]string, len(peerIDs))
	for i, id := range peerIDs {
		peers[i] = id.String()
	}
	data, err := json.Marshal(peers)
	if err != nil {
		return []byte(err.Error()), false
	}
	return data, true
}

// getLocalRoutes returns the routing table.
func (a *Agent) getLocalRoutes() ([]byte, bool) {
	routes := a.routeMgr.Table().GetAllRoutes()
	type routeInfo struct {
		Network  string `json:"network"`
		NextHop  string `json:"next_hop"`
		Origin   string `json:"origin"`
		Metric   int    `json:"metric"`
		HopCount int    `json:"hop_count"`
	}
	info := make([]routeInfo, len(routes))
	for i, r := range routes {
		info[i] = routeInfo{
			Network:  r.Network.String(),
			NextHop:  r.NextHop.ShortString(),
			Origin:   r.OriginAgent.ShortString(),
			Metric:   int(r.Metric),
			HopCount: len(r.Path),
		}
	}
	data, err := json.Marshal(info)
	if err != nil {
		return []byte(err.Error()), false
	}
	return data, true
}

// getLocalInterfaces returns the agent's assigned interface addresses. The
// response shape matches the local GET /api/interfaces endpoint so the same
// client code can consume either path.
func (a *Agent) getLocalInterfaces() ([]byte, bool) {
	resp := struct {
		Addresses []sysinfo.InterfaceAddr `json:"addresses"`
	}{Addresses: sysinfo.GetInterfaceAddresses()}
	data, err := json.Marshal(resp)
	if err != nil {
		return []byte(err.Error()), false
	}
	return data, true
}

// handlePeerConnected is called when a peer connection is established.
// It forwards any pending wake commands to the new peer and sends the full
// routing table + node info so the peer learns about this side immediately.
//
// Sending here (rather than only from connectToPeer / handleIncomingConnection)
// matters after sleep/wake: the peerMgr.ReconnectAll path used by exitSleep
// goes directly through peer.Manager.connectWithTransport, which never goes
// through the agent's per-config connectToPeer wrapper. Without this call,
// reconnected peers would only learn each other's routes via the next
// periodic advertise (default 2 minutes), which is too slow for tests and
// causes a real-world recovery delay after wake.
func (a *Agent) handlePeerConnected(conn *peer.Connection) {
	peerID := conn.RemoteID

	a.logger.Debug("peer connected",
		logging.KeyPeerID, peerID.ShortString())

	// Forward any pending wake command to the new peer
	if a.flooder != nil {
		a.flooder.OnPeerConnected(peerID)

		// Send the full routing table and node info so the peer can learn
		// what we know. Idempotent on the receiving side (seen-cache dedupes
		// duplicates), so it is safe even if connectToPeer / handleIncoming
		// also call SendFullTable for the same connection.
		a.flooder.SendFullTable(peerID)
		a.flooder.SendNodeInfoToNewPeer(peerID)
	}
}

// handlePeerDisconnect is called when a peer connection is closed.
// It cleans up any relay streams and routes involving the disconnected peer.
func (a *Agent) handlePeerDisconnect(conn *peer.Connection, err error) {
	peerID := conn.RemoteID

	a.logger.Info("peer disconnected",
		logging.KeyPeerID, peerID.ShortString(),
		logging.KeyError, err)

	// Clean up relay streams involving this peer
	a.cleanupRelaysForPeer(peerID)

	// Close this agent's own streams opened through the dead peer, so
	// ingress consumers (SOCKS5 clients, file transfers) get an error
	// instead of hanging on a next hop that no longer exists.
	if n := a.streamMgr.CloseStreamsForPeer(peerID); n > 0 {
		a.logger.Debug("closed streams for disconnected peer",
			logging.KeyPeerID, peerID.ShortString(), logging.KeyCount, n)
	}

	// Clean up routes learned from this peer
	removedCIDR := a.routeMgr.HandlePeerDisconnect(peerID)
	removedDomain := a.routeMgr.HandlePeerDisconnectDomain(peerID)
	removedForward := a.routeMgr.HandlePeerDisconnectForward(peerID)
	removedAgent := a.routeMgr.HandlePeerDisconnectAgent(peerID)

	// Clear seen cache entries for this peer so re-advertisements
	// after reconnection are not rejected as duplicates.
	if a.flooder != nil {
		a.flooder.ClearSeenCacheForOrigin(peerID)
		a.flooder.RemoveAdvertiseLimiter(peerID)

		// Flood withdrawals for the routes that died with this peer so
		// remote agents drop them now instead of blackholing traffic until
		// route TTL expiry. Routes still reachable via another next hop are
		// left alone.
		if total := len(removedCIDR) + len(removedDomain) + len(removedForward) + len(removedAgent); total > 0 {
			a.logger.Debug("flooding withdrawals for routes lost with peer",
				logging.KeyPeerID, peerID.ShortString(),
				logging.KeyCount, total)
			a.flooder.FloodWithdrawnRoutes(removedCIDR, removedDomain, removedForward, removedAgent)
		}
	}

	// Tear down any shell client adapters routed through the lost peer.
	a.closeShellClientStreamsForPeer(peerID)

	// Close exit and forward connections opened by this peer. Their replies
	// have nowhere to go now, and leaving them to age out holds real sockets
	// open while the peer's stream IDs restart at 1 on reconnect.
	if a.exitHandler != nil {
		if n := a.exitHandler.CloseConnectionsForPeer(peerID); n > 0 {
			a.logger.Debug("closed exit connections for disconnected peer",
				logging.KeyPeerID, peerID.ShortString(), logging.KeyCount, n)
		}
	}
	if a.forwardHandler != nil {
		if n := a.forwardHandler.CloseConnectionsForPeer(peerID); n > 0 {
			a.logger.Debug("closed forward connections for disconnected peer",
				logging.KeyPeerID, peerID.ShortString(), logging.KeyCount, n)
		}
	}
	if a.shellHandler != nil {
		if n := a.shellHandler.CloseStreamsForPeer(peerID); n > 0 {
			a.logger.Debug("closed shell streams for disconnected peer",
				logging.KeyPeerID, peerID.ShortString(), logging.KeyCount, n)
		}
	}
	if a.udpHandler != nil {
		if n := a.udpHandler.CloseAssociationsForPeer(peerID); n > 0 {
			a.logger.Debug("closed udp associations for disconnected peer",
				logging.KeyPeerID, peerID.ShortString(), logging.KeyCount, n)
		}
	}
	if a.icmpHandler != nil {
		if n := a.icmpHandler.CloseSessionsForPeer(peerID); n > 0 {
			a.logger.Debug("closed icmp sessions for disconnected peer",
				logging.KeyPeerID, peerID.ShortString(), logging.KeyCount, n)
		}
	}
}

// closeShellClientStreamsForPeer tears down shell client adapters whose
// next-hop was the disconnected peer. CloseFromRemote skips the STREAM_CLOSE
// frame that Close would otherwise emit -- the peer is gone anyway.
//
// The peer is part of the map key, so selecting the affected adapters needs
// only the key: no adapter.NextHop() call, and therefore no risk of inverting
// the adapter.mu -> shellClientMu lock order that the normal
// adapter.Close -> cleanupShellClientStream path takes. CloseFromRemote is
// still called after the lock is released, because it takes adapter.mu.
// Filtering on the key is also more complete than NextHop(), which reads the
// zero ID until SetNextHop runs after the handshake.
func (a *Agent) closeShellClientStreamsForPeer(peerID identity.AgentID) {
	a.shellClientMu.Lock()
	var closing []*health.ShellStreamAdapter
	for key, adapter := range a.shellClientStreams {
		if key.peer != peerID {
			continue
		}
		delete(a.shellClientStreams, key)
		closing = append(closing, adapter)
	}
	a.shellClientMu.Unlock()

	for _, adapter := range closing {
		adapter.CloseFromRemote()
	}
}

// cleanupRelaysForPeer removes all relay entries involving the specified peer.
// TCP, UDP, and ICMP share the relay table type; leaving UDP/ICMP entries
// behind would leak them until a UDP_CLOSE/ICMP_CLOSE that can no longer
// arrive, and the peer's stream IDs restart at 1 on reconnect, colliding with
// the stale entries.
func (a *Agent) cleanupRelaysForPeer(peerID identity.AgentID) {
	if cleaned := a.tcpRelay.DeleteByPeer(peerID); cleaned > 0 {
		a.logger.Debug("cleaned up relay streams",
			logging.KeyPeerID, peerID.ShortString(),
			logging.KeyCount, cleaned)
	}
	if cleaned := a.udpRelay.DeleteByPeer(peerID); cleaned > 0 {
		a.logger.Debug("cleaned up udp relay entries",
			logging.KeyPeerID, peerID.ShortString(),
			logging.KeyCount, cleaned)
	}
	if cleaned := a.icmpRelay.DeleteByPeer(peerID); cleaned > 0 {
		a.logger.Debug("cleaned up icmp relay entries",
			logging.KeyPeerID, peerID.ShortString(),
			logging.KeyCount, cleaned)
	}
}

// resolveHost translates host (a hostname, never an IP literal) to one or
// more IP addresses. The mesh-host registry is consulted first so that
// names registered via /hosts/manage resolve without depending on the
// agent's host having Mutiauk forward the suffix to a local DNS
// server. On a registry miss, falls back to the system resolver.
func (a *Agent) resolveHost(ctx context.Context, host string) ([]net.IP, error) {
	if h, ok := a.meshHosts.Lookup(host); ok {
		var ips []net.IP
		if ip := net.ParseIP(h.IPv4); ip != nil {
			ips = append(ips, ip)
		}
		if ip := net.ParseIP(h.IPv6); ip != nil {
			ips = append(ips, ip)
		}
		if len(ips) > 0 {
			return ips, nil
		}
	}
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	ips := make([]net.IP, len(addrs))
	for i, addr := range addrs {
		ips[i] = addr.IP
	}
	return ips, nil
}

// Dial implements socks5.Dialer for SOCKS5 connections.
// This routes connections through the mesh network.
func (a *Agent) Dial(network, address string) (net.Conn, error) {
	return a.DialContext(context.Background(), network, address)
}

// directDial opens a local outbound TCP connection to address, applying the
// exit handler's per-route source_ip / interface binding for destIP when this
// agent has an exit handler. This is the path taken when ingress and exit are
// the same agent (route originates locally) or when no mesh route applies, so
// local exits honor the same binding as streams arriving over a peer link.
// Pure-ingress agents (no exit handler) fall back to a plain timed dialer.
func (a *Agent) directDial(ctx context.Context, network, address string, destIP net.IP) (net.Conn, error) {
	if eh := a.exitHandler; eh != nil {
		dialer, src, iface, err := eh.DialerFor(destIP, directDialTimeout)
		if err != nil {
			return nil, err
		}
		if src != "" || iface != "" {
			a.logger.Debug("local exit dial binding applied",
				"addr", address,
				"source_ip", src,
				"interface", iface,
			)
		}
		return dialer.DialContext(ctx, network, address)
	}
	dialer := &net.Dialer{Timeout: directDialTimeout}
	return dialer.DialContext(ctx, network, address)
}

// DialContext implements socks5.Dialer for SOCKS5 connections with context support.
// This allows cancellation when the client disconnects during dial.
func (a *Agent) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	// Parse the address
	host, portStr, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("invalid address: %w", err)
	}

	port, err := net.LookupPort(network, portStr)
	if err != nil {
		return nil, fmt.Errorf("invalid port: %w", err)
	}

	// Check if host is already an IP address
	destIP := net.ParseIP(host)

	// If host is a domain, check domain routes BEFORE DNS resolution
	if destIP == nil {
		domainRoute := a.routeMgr.LookupDomain(host)
		if domainRoute != nil {
			// If domain route points to us (local exit), resolve and dial directly
			if domainRoute.OriginAgent == a.id {
				ips, err := a.resolveHost(ctx, host)
				if err != nil {
					return nil, fmt.Errorf("resolve %s: %w", host, err)
				}
				if len(ips) == 0 {
					return nil, fmt.Errorf("no IP addresses for %s", host)
				}
				return a.directDial(ctx, network, net.JoinHostPort(ips[0].String(), portStr), ips[0])
			}

			// Route via domain route - exit node will resolve DNS
			return a.dialViaDomainRouteWithContext(ctx, network, host, port, domainRoute)
		}

		// No domain route - resolve at ingress (mesh-host registry, then DNS).
		ips, err := a.resolveHost(ctx, host)
		if err != nil {
			return nil, fmt.Errorf("resolve %s: %w", host, err)
		}
		if len(ips) == 0 {
			return nil, fmt.Errorf("no IP addresses for %s", host)
		}
		destIP = ips[0]
	}

	// Look up CIDR route in routing table
	route := a.routeMgr.Lookup(destIP)

	// If no route, or route is to ourselves (local exit), do direct dial
	if route == nil || route.OriginAgent == a.id {
		return a.directDial(ctx, network, address, destIP)
	}

	// Route through mesh - get next hop connection
	conn := a.peerMgr.GetPeer(route.NextHop)
	if conn == nil {
		// Next hop not connected, fall back to direct
		return a.directDial(ctx, network, address, destIP)
	}

	// Build the path for STREAM_OPEN
	var remainingPath []identity.AgentID
	if len(route.Path) > 1 {
		remainingPath = make([]identity.AgentID, len(route.Path)-1)
		copy(remainingPath, route.Path[1:])
	}

	// Generate stream ID
	streamID := conn.NextStreamID()

	// Build address bytes
	var addrType uint8
	var addrBytes []byte
	if ip4 := destIP.To4(); ip4 != nil {
		addrType = protocol.AddrTypeIPv4
		addrBytes = ip4
	} else {
		addrType = protocol.AddrTypeIPv6
		addrBytes = destIP
	}

	// Generate ephemeral keypair for E2E encryption key exchange
	ephPriv, ephPub, err := crypto.GenerateEphemeralKeypair()
	if err != nil {
		return nil, fmt.Errorf("generate ephemeral key: %w", err)
	}

	// Create the stream in stream manager
	pending := a.streamMgr.OpenStream(streamID, route.NextHop, host, uint16(port), 30*time.Second)

	// Store ephemeral keys in pending request for later key derivation
	a.streamMgr.SetPendingEphemeralKeys(pending.RequestID, ephPriv, ephPub)

	// Build and send STREAM_OPEN with ephemeral public key
	openPayload := &protocol.StreamOpen{
		RequestID:       pending.RequestID,
		AddressType:     addrType,
		Address:         addrBytes,
		Port:            uint16(port),
		RemainingPath:   remainingPath,
		EphemeralPubKey: ephPub,
	}

	frame := &protocol.Frame{
		Type:     protocol.FrameStreamOpen,
		StreamID: streamID,
		Payload:  openPayload.Encode(),
	}

	if err := a.peerMgr.SendToPeer(route.NextHop, frame); err != nil {
		a.streamMgr.CancelPendingRequest(pending.RequestID)
		return nil, fmt.Errorf("send stream open: %w", err)
	}

	// Wait for response with context support
	var result *stream.StreamOpenResult
	select {
	case result = <-pending.ResultCh:
		// Got result
	case <-ctx.Done():
		// Context cancelled - clean up and return
		a.streamMgr.CancelPendingRequest(pending.RequestID)
		crypto.ZeroKey(&ephPriv)
		return nil, ctx.Err()
	}

	if result.Error != nil {
		crypto.ZeroKey(&ephPriv)
		return nil, result.Error
	}

	// Derive session key from ECDH with exit node's ephemeral public key
	sharedSecret, err := crypto.ComputeECDH(ephPriv, result.RemoteEphemeral)
	if err != nil {
		crypto.ZeroKey(&ephPriv)
		return nil, fmt.Errorf("compute ECDH: %w", err)
	}

	crypto.ZeroKey(&ephPriv)

	// Derive session key - we are the initiator
	sessionKey := crypto.DeriveSessionKey(sharedSecret, pending.RequestID, ephPub, result.RemoteEphemeral, true)
	crypto.ZeroKey(&sharedSecret)

	// Store session key in stream
	result.Stream.SetSessionKey(sessionKey)

	// Return a MeshConn wrapper
	return &meshConn{
		agent:    a,
		stream:   result.Stream,
		peerID:   route.NextHop,
		streamID: streamID,
		localAddr: &net.TCPAddr{
			IP:   result.BoundIP,
			Port: int(result.BoundPort),
		},
		remoteAddr: &net.TCPAddr{
			IP:   destIP,
			Port: port,
		},
	}, nil
}

// dialViaDomainRouteWithContext routes a connection through a domain route with context support.
func (a *Agent) dialViaDomainRouteWithContext(ctx context.Context, network, host string, port int, route *routing.DomainRoute) (net.Conn, error) {
	// Get next hop connection
	conn := a.peerMgr.GetPeer(route.NextHop)
	if conn == nil {
		return nil, fmt.Errorf("next hop %s not connected", route.NextHop.ShortString())
	}

	// Build the path for STREAM_OPEN
	var remainingPath []identity.AgentID
	if len(route.Path) > 1 {
		remainingPath = make([]identity.AgentID, len(route.Path)-1)
		copy(remainingPath, route.Path[1:])
	}

	// Generate stream ID
	streamID := conn.NextStreamID()

	// Build domain address bytes (length-prefixed string)
	addrBytes := make([]byte, 1+len(host))
	addrBytes[0] = byte(len(host))
	copy(addrBytes[1:], host)

	// Generate ephemeral keypair for E2E encryption key exchange
	ephPriv, ephPub, err := crypto.GenerateEphemeralKeypair()
	if err != nil {
		return nil, fmt.Errorf("generate ephemeral key: %w", err)
	}

	// Create the stream in stream manager
	pending := a.streamMgr.OpenStream(streamID, route.NextHop, host, uint16(port), 30*time.Second)

	// Store ephemeral keys in pending request for later key derivation
	a.streamMgr.SetPendingEphemeralKeys(pending.RequestID, ephPriv, ephPub)

	// Build and send STREAM_OPEN with domain address
	openPayload := &protocol.StreamOpen{
		RequestID:       pending.RequestID,
		AddressType:     protocol.AddrTypeDomain,
		Address:         addrBytes,
		Port:            uint16(port),
		RemainingPath:   remainingPath,
		EphemeralPubKey: ephPub,
	}

	frame := &protocol.Frame{
		Type:     protocol.FrameStreamOpen,
		StreamID: streamID,
		Payload:  openPayload.Encode(),
	}

	if err := a.peerMgr.SendToPeer(route.NextHop, frame); err != nil {
		a.streamMgr.CancelPendingRequest(pending.RequestID)
		return nil, fmt.Errorf("send stream open: %w", err)
	}

	// Wait for response with context support
	var result *stream.StreamOpenResult
	select {
	case result = <-pending.ResultCh:
		// Got result
	case <-ctx.Done():
		// Context cancelled - clean up and return
		a.streamMgr.CancelPendingRequest(pending.RequestID)
		crypto.ZeroKey(&ephPriv)
		return nil, ctx.Err()
	}

	if result.Error != nil {
		crypto.ZeroKey(&ephPriv)
		return nil, result.Error
	}

	// Derive session key from ECDH with exit node's ephemeral public key
	sharedSecret, err := crypto.ComputeECDH(ephPriv, result.RemoteEphemeral)
	if err != nil {
		crypto.ZeroKey(&ephPriv)
		return nil, fmt.Errorf("compute ECDH: %w", err)
	}

	crypto.ZeroKey(&ephPriv)

	// Derive session key - we are the initiator
	sessionKey := crypto.DeriveSessionKey(sharedSecret, pending.RequestID, ephPub, result.RemoteEphemeral, true)
	crypto.ZeroKey(&sharedSecret)

	// Store session key in stream
	result.Stream.SetSessionKey(sessionKey)

	// Return a MeshConn wrapper
	return &meshConn{
		agent:    a,
		stream:   result.Stream,
		peerID:   route.NextHop,
		streamID: streamID,
		localAddr: &net.TCPAddr{
			IP:   result.BoundIP,
			Port: int(result.BoundPort),
		},
		remoteAddr: &domainAddr{
			domain: host,
			port:   port,
		},
	}, nil
}

// DialForward implements forward.ForwardDialer for port forward connections.
// This routes connections through the mesh network to a forward endpoint.
func (a *Agent) DialForward(ctx context.Context, key string) (net.Conn, error) {
	// Look up forward route
	route := a.routeMgr.LookupForward(key)
	if route == nil {
		return nil, fmt.Errorf("no route for forward: %s", key)
	}

	// Self-loop: this agent both advertises and exits the route. Splice the
	// listener directly to the local backend in-process; no STREAM_OPEN, no
	// ECDH, no per-frame encrypt/decrypt.
	if route.NextHop == a.id {
		return a.dialForwardLocal(ctx, key)
	}

	// Get next hop connection
	conn := a.peerMgr.GetPeer(route.NextHop)
	if conn == nil {
		return nil, fmt.Errorf("next hop %s not connected", route.NextHop.ShortString())
	}

	// Build the path for STREAM_OPEN
	var remainingPath []identity.AgentID
	if len(route.Path) > 1 {
		remainingPath = make([]identity.AgentID, len(route.Path)-1)
		copy(remainingPath, route.Path[1:])
	}

	// Generate stream ID
	streamID := conn.NextStreamID()

	// Build forward address (domain type with "forward:" prefix)
	forwardAddr := protocol.ForwardStreamPrefix + key
	addrBytes := make([]byte, 1+len(forwardAddr))
	addrBytes[0] = byte(len(forwardAddr))
	copy(addrBytes[1:], forwardAddr)

	// Generate ephemeral keypair for E2E encryption key exchange
	ephPriv, ephPub, err := crypto.GenerateEphemeralKeypair()
	if err != nil {
		return nil, fmt.Errorf("generate ephemeral key: %w", err)
	}

	// Create the stream in stream manager (port 0 for forward connections)
	pending := a.streamMgr.OpenStream(streamID, route.NextHop, forwardAddr, 0, 30*time.Second)

	// Store ephemeral keys in pending request for later key derivation
	a.streamMgr.SetPendingEphemeralKeys(pending.RequestID, ephPriv, ephPub)

	// Build and send STREAM_OPEN with forward address
	openPayload := &protocol.StreamOpen{
		RequestID:       pending.RequestID,
		AddressType:     protocol.AddrTypeDomain,
		Address:         addrBytes,
		Port:            0, // Not used for forwards
		RemainingPath:   remainingPath,
		EphemeralPubKey: ephPub,
	}

	frame := &protocol.Frame{
		Type:     protocol.FrameStreamOpen,
		StreamID: streamID,
		Payload:  openPayload.Encode(),
	}

	if err := a.peerMgr.SendToPeer(route.NextHop, frame); err != nil {
		a.streamMgr.CancelPendingRequest(pending.RequestID)
		return nil, fmt.Errorf("send stream open: %w", err)
	}

	// Wait for response with context support
	var result *stream.StreamOpenResult
	select {
	case result = <-pending.ResultCh:
		// Got result
	case <-ctx.Done():
		// Context cancelled - clean up and return
		a.streamMgr.CancelPendingRequest(pending.RequestID)
		crypto.ZeroKey(&ephPriv)
		return nil, ctx.Err()
	}

	if result.Error != nil {
		crypto.ZeroKey(&ephPriv)
		return nil, result.Error
	}

	// Derive session key from ECDH with exit node's ephemeral public key
	sharedSecret, err := crypto.ComputeECDH(ephPriv, result.RemoteEphemeral)
	if err != nil {
		crypto.ZeroKey(&ephPriv)
		return nil, fmt.Errorf("compute ECDH: %w", err)
	}

	crypto.ZeroKey(&ephPriv)

	// Derive session key - we are the initiator
	sessionKey := crypto.DeriveSessionKey(sharedSecret, pending.RequestID, ephPub, result.RemoteEphemeral, true)
	crypto.ZeroKey(&sharedSecret)

	// Store session key in stream
	result.Stream.SetSessionKey(sessionKey)

	// Return a MeshConn wrapper
	return &meshConn{
		agent:    a,
		stream:   result.Stream,
		peerID:   route.NextHop,
		streamID: streamID,
		localAddr: &net.TCPAddr{
			IP:   result.BoundIP,
			Port: int(result.BoundPort),
		},
		remoteAddr: &forwardAddr_{
			key: key,
		},
	}, nil
}

// dialForwardLocal handles the self-loop case. Bypasses the mesh wire
// protocol entirely -- no STREAM_OPEN, no ECDH -- by splicing the listener
// directly to a local backend RWC.
func (a *Agent) dialForwardLocal(ctx context.Context, key string) (net.Conn, error) {
	backend, ok := a.forwardHandler.GetBackend(key)
	if !ok {
		return nil, fmt.Errorf("forward key %q has no local backend", key)
	}
	// streamID is unused on the self-loop: tcp/ws backends ignore it or
	// allocate their own internal id.
	rwc, err := backend.Open(ctx, 0, key, "local")
	if err != nil {
		return nil, err
	}

	// io.Pipe pair (rather than net.Pipe) so CloseWrite on one direction
	// propagates EOF to the backend without tearing down the read side.
	// forward.Listener.relay relies on halfCloser semantics on the target.
	listenerToBackendR, listenerToBackendW := io.Pipe()
	backendToListenerR, backendToListenerW := io.Pipe()

	go func() {
		defer rwc.Close()
		_, _ = io.Copy(rwc, listenerToBackendR)
	}()
	go func() {
		_, _ = io.Copy(backendToListenerW, rwc)
		_ = backendToListenerW.Close()
	}()

	return &localForwardConn{
		recv: backendToListenerR,
		send: listenerToBackendW,
	}, nil
}

// localForwardConn is a half-close-aware net.Conn over a pair of io.Pipes,
// used only by the self-loop forward path.
type localForwardConn struct {
	recv *io.PipeReader
	send *io.PipeWriter
}

func (c *localForwardConn) Read(p []byte) (int, error)  { return c.recv.Read(p) }
func (c *localForwardConn) Write(p []byte) (int, error) { return c.send.Write(p) }

// io.PipeReader/Writer Close is documented as idempotent and never errors.
func (c *localForwardConn) Close() error {
	_ = c.recv.Close()
	_ = c.send.Close()
	return nil
}

func (c *localForwardConn) CloseWrite() error { return c.send.Close() }

// LocalAddr/RemoteAddr/SetDeadline exist only to satisfy net.Conn; the
// listener never reads them.
func (c *localForwardConn) LocalAddr() net.Addr                { return &net.TCPAddr{} }
func (c *localForwardConn) RemoteAddr() net.Addr               { return &net.TCPAddr{} }
func (c *localForwardConn) SetDeadline(_ time.Time) error      { return nil }
func (c *localForwardConn) SetReadDeadline(_ time.Time) error  { return nil }
func (c *localForwardConn) SetWriteDeadline(_ time.Time) error { return nil }

// forwardAddr_ implements net.Addr for port forward connections.
type forwardAddr_ struct {
	key string
}

func (t *forwardAddr_) Network() string { return "tcp" }
func (t *forwardAddr_) String() string  { return "forward:" + t.key }

// domainAddr implements net.Addr for domain-based connections.
type domainAddr struct {
	domain string
	port   int
}

func (d *domainAddr) Network() string { return "tcp" }
func (d *domainAddr) String() string  { return fmt.Sprintf("%s:%d", d.domain, d.port) }

// meshConn implements net.Conn for mesh-routed connections.
type meshConn struct {
	agent      *Agent
	stream     *stream.Stream
	peerID     identity.AgentID
	streamID   uint64
	localAddr  net.Addr
	remoteAddr net.Addr
	readBuf    []byte
	readOffset int

	// Deadlines for read/write operations
	mu            sync.Mutex
	readDeadline  time.Time
	writeDeadline time.Time
}

// Read reads data from the mesh connection.
func (c *meshConn) Read(b []byte) (int, error) {
	// If we have buffered data, return from that
	if c.readOffset < len(c.readBuf) {
		n := copy(b, c.readBuf[c.readOffset:])
		c.readOffset += n
		if c.readOffset >= len(c.readBuf) {
			c.readBuf = nil
			c.readOffset = 0
		}
		return n, nil
	}

	// Create context with deadline if set
	c.mu.Lock()
	deadline := c.readDeadline
	c.mu.Unlock()

	var ctx context.Context
	var cancel context.CancelFunc
	if !deadline.IsZero() {
		ctx, cancel = context.WithDeadline(context.Background(), deadline)
		defer cancel()
	} else {
		ctx = context.Background()
	}

	// Read new data from stream
	data, err := c.stream.Read(ctx)
	if err != nil {
		return 0, err
	}

	// Decrypt the received data
	sessionKey := c.stream.GetSessionKey()
	if sessionKey == nil {
		return 0, fmt.Errorf("no session key for stream %d", c.streamID)
	}

	plaintext, err := sessionKey.DecryptStrict(data)
	if err != nil {
		return 0, fmt.Errorf("decrypt: %w", err)
	}

	n := copy(b, plaintext)
	if n < len(plaintext) {
		c.readBuf = plaintext
		c.readOffset = n
	}
	return n, nil
}

// Write writes data to the mesh connection.
// Chunks data larger than MaxPayloadSize into multiple frames.
func (c *meshConn) Write(b []byte) (int, error) {
	if !c.stream.CanWrite() {
		return 0, fmt.Errorf("stream closed for writing")
	}

	if len(b) == 0 {
		return 0, nil
	}

	// Get session key for encryption
	sessionKey := c.stream.GetSessionKey()
	if sessionKey == nil {
		return 0, fmt.Errorf("no session key for stream %d", c.streamID)
	}

	// Account for encryption overhead when chunking
	maxPlaintext := protocol.MaxPayloadSize - crypto.EncryptionOverhead

	// Chunk data into max plaintext size pieces, encrypt each, and send
	for offset := 0; offset < len(b); {
		end := offset + maxPlaintext
		if end > len(b) {
			end = len(b)
		}

		chunk := b[offset:end]

		// Encrypt the chunk
		ciphertext, err := sessionKey.Encrypt(chunk)
		if err != nil {
			return offset, fmt.Errorf("encrypt: %w", err)
		}

		frame := &protocol.Frame{
			Type:     protocol.FrameStreamData,
			StreamID: c.streamID,
			Payload:  ciphertext,
		}

		if err := c.agent.peerMgr.SendToPeer(c.peerID, frame); err != nil {
			// Return bytes written so far
			return offset, err
		}

		offset = end
	}

	return len(b), nil
}

// Close closes the mesh connection.
func (c *meshConn) Close() error {
	// Send close frame
	frame := &protocol.Frame{
		Type:     protocol.FrameStreamClose,
		StreamID: c.streamID,
	}
	c.agent.peerMgr.SendToPeer(c.peerID, frame)

	// Remove from stream manager (also closes the stream)
	c.agent.streamMgr.RemoveStream(c.peerID, c.streamID)

	return nil
}

// CloseWrite signals that we're done writing (half-close).
// Sends FIN_WRITE flag through the mesh to signal remote side.
func (c *meshConn) CloseWrite() error {
	// Get session key for encryption
	sessionKey := c.stream.GetSessionKey()
	if sessionKey == nil {
		return fmt.Errorf("no session key for stream %d", c.streamID)
	}

	// Send empty encrypted data with FIN_WRITE flag
	emptyEncrypted, err := sessionKey.Encrypt([]byte{})
	if err != nil {
		return fmt.Errorf("encrypt fin: %w", err)
	}

	frame := &protocol.Frame{
		Type:     protocol.FrameStreamData,
		Flags:    protocol.FlagFinWrite,
		StreamID: c.streamID,
		Payload:  emptyEncrypted,
	}

	if err := c.agent.peerMgr.SendToPeer(c.peerID, frame); err != nil {
		return err
	}

	// Update local stream state
	c.stream.CloseWrite()
	return nil
}

// LocalAddr returns the local address.
func (c *meshConn) LocalAddr() net.Addr {
	return c.localAddr
}

// RemoteAddr returns the remote address.
func (c *meshConn) RemoteAddr() net.Addr {
	return c.remoteAddr
}

// SetDeadline sets read and write deadlines.
func (c *meshConn) SetDeadline(t time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.readDeadline = t
	c.writeDeadline = t
	return nil
}

// SetReadDeadline sets the read deadline.
func (c *meshConn) SetReadDeadline(t time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.readDeadline = t
	return nil
}

// SetWriteDeadline sets the write deadline.
func (c *meshConn) SetWriteDeadline(t time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.writeDeadline = t
	return nil
}

// Stats returns agent statistics.
func (a *Agent) Stats() AgentStats {
	return AgentStats{
		PeerCount:      a.peerMgr.PeerCount(),
		StreamCount:    a.streamMgr.StreamCount(),
		RouteCount:     a.routeMgr.TotalRoutes(),
		SOCKS5Running:  a.socks5Srv != nil && a.socks5Srv.IsRunning(),
		ExitHandlerRun: a.exitHandler != nil && a.exitHandler.IsRunning(),
	}
}

// HealthStats returns health statistics for the health.StatsProvider interface.
func (a *Agent) HealthStats() health.Stats {
	return health.Stats{
		PeerCount:               a.peerMgr.PeerCount(),
		StreamCount:             a.streamMgr.StreamCount(),
		RouteCount:              a.routeMgr.TotalRoutes(),
		SOCKS5Running:           a.socks5Srv != nil && a.socks5Srv.IsRunning(),
		ExitHandlerRun:          a.exitHandler != nil && a.exitHandler.IsRunning(),
		CanSignManagement:       a.cfg.CanSign(),
		AcceptsSignedManagement: a.cfg.HasSigningKey(),
	}
}

// agentStatsProvider adapts Agent to health.StatsProvider interface.
type agentStatsProvider struct {
	agent *Agent
}

// IsRunning implements health.StatsProvider.
func (p *agentStatsProvider) IsRunning() bool {
	return p.agent.IsRunning()
}

// Stats implements health.StatsProvider.
func (p *agentStatsProvider) Stats() health.Stats {
	return p.agent.HealthStats()
}

// AgentStats contains agent statistics.
type AgentStats struct {
	PeerCount      int
	StreamCount    int
	RouteCount     int
	SOCKS5Running  bool
	ExitHandlerRun bool
}

// GetRoutes returns all routes for debugging.
func (a *Agent) GetRoutes() []*routing.Route {
	return a.routeMgr.Table().GetAllRoutes()
}

// LookupForwardRoute returns the best forward route for a key, or nil if none.
func (a *Agent) LookupForwardRoute(key string) *routing.ForwardRoute {
	return a.routeMgr.LookupForward(key)
}

// ForwardListenerAddress returns the bound address of the forward listener for the
// given key, or nil if no such listener exists.
func (a *Agent) ForwardListenerAddress(key string) net.Addr {
	a.forwardListenersMu.RLock()
	defer a.forwardListenersMu.RUnlock()
	if listener, ok := a.forwardListeners[key]; ok {
		return listener.Address()
	}
	return nil
}

// GetPeerIDs returns all connected peer IDs for debugging.
func (a *Agent) GetPeerIDs() []identity.AgentID {
	return a.peerMgr.GetPeerIDs()
}

// GetKnownAgentIDs returns all known agent IDs (peers and route origins).
// This implements health.RemoteMetricsProvider.
func (a *Agent) GetKnownAgentIDs() []identity.AgentID {
	seen := make(map[identity.AgentID]struct{})
	var result []identity.AgentID

	// Add connected peers
	for _, id := range a.peerMgr.GetPeerIDs() {
		if _, ok := seen[id]; !ok {
			seen[id] = struct{}{}
			result = append(result, id)
		}
	}

	// Add route origins from CIDR table
	routes := a.routeMgr.Table().GetAllRoutes()
	for _, r := range routes {
		if _, ok := seen[r.OriginAgent]; !ok {
			seen[r.OriginAgent] = struct{}{}
			result = append(result, r.OriginAgent)
		}
	}

	// Add agents from presence table
	for _, id := range a.routeMgr.AgentTable().GetAllAgentIDs() {
		if _, ok := seen[id]; !ok {
			seen[id] = struct{}{}
			result = append(result, id)
		}
	}

	return result
}

// GetPeerDetails returns detailed information about connected peers.
// This implements health.RemoteMetricsProvider.
func (a *Agent) GetPeerDetails() []health.PeerDetails {
	peers := a.peerMgr.GetAllPeers()
	details := make([]health.PeerDetails, len(peers))
	for i, p := range peers {
		// Prefer the routing manager's display name: it refreshes on every
		// NodeInfo advertisement, so dynamic renames propagate without a
		// peer reconnect. RemoteDisplayName is only set at handshake time.
		displayName := a.routeMgr.GetDisplayName(p.RemoteID)
		if displayName == "" {
			displayName = p.RemoteDisplayName
		}
		if displayName == "" {
			displayName = p.RemoteID.ShortString()
		}
		details[i] = health.PeerDetails{
			ID:          p.RemoteID,
			DisplayName: displayName,
			State:       p.State().String(),
			RTT:         p.RTT(),
			IsDialer:    p.IsDialer(),
			Transport:   string(p.TransportType()),
		}
	}
	return details
}

// GetRouteDetails returns detailed route information for the dashboard.
// This implements health.RemoteMetricsProvider.
func (a *Agent) GetRouteDetails() []health.RouteDetails {
	routes := a.routeMgr.Table().GetAllRoutes()
	// Source IPs + interfaces are local-only -- look them up once for routes
	// this agent originates. nil-safe: agents without an exit handler get an
	// empty map.
	var bindings map[string]exit.RouteBinding
	if a.exitHandler != nil {
		bindings = a.exitHandler.BindingsByCIDR()
	}
	details := make([]health.RouteDetails, len(routes))
	for i, r := range routes {
		// Copy the path slice to avoid sharing underlying array
		pathCopy := make([]identity.AgentID, len(r.Path))
		copy(pathCopy, r.Path)
		var sourceIP, iface string
		if r.OriginAgent == a.id {
			if b, ok := bindings[r.Network.String()]; ok {
				if b.Source != nil {
					sourceIP = b.Source.String()
				}
				iface = b.Interface
			}
		}
		details[i] = health.RouteDetails{
			Network:   r.Network.String(),
			NextHop:   r.NextHop,
			Origin:    r.OriginAgent,
			Metric:    int(r.Metric),
			HopCount:  len(r.Path),
			Path:      pathCopy,
			Comment:   r.Comment,
			SourceIP:  sourceIP,
			Interface: iface,
		}
	}
	return details
}

// GetDomainRouteDetails returns detailed domain route information for the dashboard.
func (a *Agent) GetDomainRouteDetails() []health.DomainRouteDetails {
	routes := a.routeMgr.DomainTable().GetAllRoutes()
	details := make([]health.DomainRouteDetails, 0, len(routes))
	for _, r := range routes {
		// Copy the path slice to avoid sharing underlying array
		pathCopy := make([]identity.AgentID, len(r.Path))
		copy(pathCopy, r.Path)
		details = append(details, health.DomainRouteDetails{
			Pattern:    r.Pattern,
			IsWildcard: r.IsWildcard,
			NextHop:    r.NextHop,
			Origin:     r.OriginAgent,
			Metric:     int(r.Metric),
			HopCount:   len(r.Path),
			Path:       pathCopy,
		})
	}
	return details
}

// GetAllDisplayNames returns display names for all known agents.
// This implements health.RemoteMetricsProvider.
func (a *Agent) GetAllDisplayNames() map[identity.AgentID]string {
	return a.routeMgr.GetAllDisplayNames()
}

// GetAllNodeInfo returns node info for all known agents.
func (a *Agent) GetAllNodeInfo() map[identity.AgentID]*protocol.NodeInfo {
	return a.routeMgr.GetAllNodeInfo()
}

// GetLocalNodeInfo returns local node info.
func (a *Agent) GetLocalNodeInfo() *protocol.NodeInfo {
	return sysinfo.Collect(a.displayNameForAdvertise(), a.getPeerConnectionInfo(), a.keypair.PublicKey, a.getUDPConfig(), a.getForwardConfig(), a.getFileTransferConfig(), a.getShellConfig(), a.getICMPConfig(), a.getAdvertisedListeners())
}

// getUDPConfig returns the UDP configuration for node info advertisements.
func (a *Agent) getUDPConfig() *sysinfo.UDPConfig {
	return &sysinfo.UDPConfig{
		Enabled: a.cfg.UDP.Enabled,
	}
}

// getAdvertisedListeners builds the list of listener dial targets to advertise
// via NodeInfo. Listeners without an `advertise:` block are omitted. Defaults:
// advertise.transport -> listener.transport; advertise.path -> listener.path
// (only meaningful for h2/ws).
func (a *Agent) getAdvertisedListeners() []protocol.AdvertisedListener {
	// Cover the full desired set (config + dynamic), keyed by listen address, so
	// dynamic listeners added with --advertise-address are announced too. Iterate
	// in sorted address order for stable NodeInfo output.
	all := a.listeners.All()
	addrs := make([]string, 0, len(all))
	for addr := range all {
		addrs = append(addrs, addr)
	}
	sort.Strings(addrs)

	var out []protocol.AdvertisedListener
	for _, addr := range addrs {
		l := all[addr]
		if l.Advertise == nil || l.Advertise.Address == "" {
			continue
		}
		transport := l.Advertise.Transport
		if transport == "" {
			transport = l.Transport
		}
		path := l.Advertise.Path
		if path == "" {
			path = l.Path
		}
		out = append(out, protocol.AdvertisedListener{
			Transport: transport,
			Address:   l.Advertise.Address,
			Path:      path,
		})
	}
	return out
}

// getForwardConfig returns the forward listener configuration for node info advertisements.
func (a *Agent) getForwardConfig() *sysinfo.ForwardConfig {
	a.forwardListenersMu.RLock()
	var listeners []protocol.ForwardListenerInfo
	for _, listener := range a.forwardListeners {
		if addr := listener.Address(); addr != nil {
			listeners = append(listeners, protocol.ForwardListenerInfo{
				Key:     listener.Key(),
				Address: addr.String(),
			})
		}
	}
	a.forwardListenersMu.RUnlock()
	if len(listeners) == 0 {
		return nil
	}
	return &sysinfo.ForwardConfig{Listeners: listeners}
}

// getFileTransferConfig returns the file transfer configuration for node info advertisements.
func (a *Agent) getFileTransferConfig() *sysinfo.FileTransferConfig {
	return &sysinfo.FileTransferConfig{
		Enabled: a.cfg.FileTransfer.Enabled,
	}
}

// getShellConfig returns the shell configuration for node info advertisements.
func (a *Agent) getShellConfig() *sysinfo.ShellConfig {
	return &sysinfo.ShellConfig{
		Enabled: a.cfg.Shell.Enabled,
	}
}

// getICMPConfig returns the ICMP configuration for node info advertisements.
func (a *Agent) getICMPConfig() *sysinfo.ICMPConfig {
	return &sysinfo.ICMPConfig{
		Enabled: a.cfg.ICMP.Enabled,
	}
}

// GetSOCKS5Info returns SOCKS5 configuration info for the dashboard.
func (a *Agent) GetSOCKS5Info() health.SOCKS5Info {
	return health.SOCKS5Info{
		Enabled: a.cfg.SOCKS5.Enabled,
		Address: a.cfg.SOCKS5.Address,
	}
}

// GetUDPInfo returns UDP relay configuration info for the dashboard.
func (a *Agent) GetUDPInfo() health.UDPInfo {
	return health.UDPInfo{
		Enabled: a.cfg.UDP.Enabled,
	}
}

// GetPortForwardInfo returns port forward configuration info for the dashboard.
func (a *Agent) GetPortForwardInfo() health.PortForwardInfo {
	info := health.PortForwardInfo{}

	// Get listener keys and addresses
	a.forwardListenersMu.RLock()
	for _, listener := range a.forwardListeners {
		info.ListenerKeys = append(info.ListenerKeys, listener.Key())
		if addr := listener.Address(); addr != nil {
			info.ListenerAddresses = append(info.ListenerAddresses, addr.String())
		}
	}
	a.forwardListenersMu.RUnlock()

	// Get endpoint keys from forward handler
	if a.forwardHandler != nil {
		info.EndpointKeys = a.forwardHandler.GetKeys()
	}

	return info
}

// GetPortForwardRouteDetails returns detailed port forward route information for the dashboard.
func (a *Agent) GetPortForwardRouteDetails() []health.PortForwardRouteDetails {
	localID := a.ID()
	var details []health.PortForwardRouteDetails

	// Build a map of local listener addresses by key
	listenerAddrs := make(map[string]string)
	a.forwardListenersMu.RLock()
	for _, listener := range a.forwardListeners {
		if addr := listener.Address(); addr != nil {
			listenerAddrs[listener.Key()] = addr.String()
		}
	}
	a.forwardListenersMu.RUnlock()

	// Add local forward endpoints (this agent is the exit)
	if a.forwardHandler != nil {
		for _, key := range a.forwardHandler.GetKeys() {
			target, _ := a.forwardHandler.GetTarget(key)
			details = append(details, health.PortForwardRouteDetails{
				Key:             key,
				ListenerAddress: listenerAddrs[key], // Include listener if we have one
				Target:          target,
				NextHop:         localID,
				Origin:          localID,
				Metric:          0,
				HopCount:        0,
				Path:            []identity.AgentID{},
				IsLocal:         true,
			})
		}
	}

	// Add remote forward routes from route table
	for _, route := range a.routeMgr.ForwardTable().GetAllRoutes() {
		// Skip local routes (already handled above)
		if route.OriginAgent == localID {
			continue
		}
		details = append(details, health.PortForwardRouteDetails{
			Key:             route.Key,
			ListenerAddress: listenerAddrs[route.Key], // Include listener if we have one
			Target:          route.Target,             // Target is now propagated in advertisements
			NextHop:         route.NextHop,
			Origin:          route.OriginAgent,
			Metric:          int(route.Metric),
			HopCount:        len(route.Path),
			Path:            route.Path,
			IsLocal:         false,
		})
	}

	return details
}

// getPeerConnectionInfo returns peer connection info for NodeInfo advertisement.
func (a *Agent) getPeerConnectionInfo() []protocol.PeerConnectionInfo {
	peers := a.peerMgr.GetAllPeers()
	info := make([]protocol.PeerConnectionInfo, 0, len(peers))
	for _, p := range peers {
		var peerInfo protocol.PeerConnectionInfo
		copy(peerInfo.PeerID[:], p.RemoteID[:])
		peerInfo.Transport = string(p.TransportType())
		peerInfo.RTTMs = p.RTT().Milliseconds()
		peerInfo.IsDialer = p.IsDialer()
		info = append(info, peerInfo)
	}
	return info
}

// SOCKS5Address returns the SOCKS5 server address, or nil if not running.
func (a *Agent) SOCKS5Address() net.Addr {
	if a.socks5Srv == nil {
		return nil
	}
	return a.socks5Srv.Address()
}

// HealthServerAddress returns the HTTP health server address, or nil if not running.
func (a *Agent) HealthServerAddress() net.Addr {
	if a.healthServer == nil {
		return nil
	}
	return a.healthServer.Address()
}

// WriteStreamData implements exit.StreamWriter.
// Chunks data larger than MaxPayloadSize into multiple frames.
func (a *Agent) WriteStreamData(peerID identity.AgentID, streamID uint64, data []byte, flags uint8) error {
	// Handle empty data with flags (e.g., FIN_WRITE)
	if len(data) == 0 {
		frame := &protocol.Frame{
			Type:     protocol.FrameStreamData,
			StreamID: streamID,
			Flags:    flags,
			Payload:  nil,
		}
		return a.peerMgr.SendToPeer(peerID, frame)
	}

	// Chunk data into MaxPayloadSize pieces
	for offset := 0; offset < len(data); {
		end := offset + protocol.MaxPayloadSize
		if end > len(data) {
			end = len(data)
		}

		chunk := data[offset:end]
		isLast := end >= len(data)

		// Only set flags on the last chunk
		var chunkFlags uint8
		if isLast {
			chunkFlags = flags
		}

		frame := &protocol.Frame{
			Type:     protocol.FrameStreamData,
			StreamID: streamID,
			Flags:    chunkFlags,
			Payload:  chunk,
		}

		if err := a.peerMgr.SendToPeer(peerID, frame); err != nil {
			return err
		}

		offset = end
	}

	return nil
}

// WriteStreamOpenAck implements exit.StreamWriter.
func (a *Agent) WriteStreamOpenAck(peerID identity.AgentID, streamID uint64, requestID uint64, boundIP net.IP, boundPort uint16, ephemeralPubKey [crypto.KeySize]byte) error {
	var addrType uint8
	var addrBytes []byte
	if ip4 := boundIP.To4(); ip4 != nil {
		addrType = protocol.AddrTypeIPv4
		addrBytes = ip4
	} else if len(boundIP) == 16 {
		addrType = protocol.AddrTypeIPv6
		addrBytes = boundIP
	} else {
		addrType = protocol.AddrTypeIPv4
		addrBytes = []byte{0, 0, 0, 0}
	}

	ack := &protocol.StreamOpenAck{
		RequestID:       requestID,
		BoundAddrType:   addrType,
		BoundAddr:       addrBytes,
		BoundPort:       boundPort,
		EphemeralPubKey: ephemeralPubKey,
	}
	frame := &protocol.Frame{
		Type:     protocol.FrameStreamOpenAck,
		StreamID: streamID,
		Payload:  ack.Encode(),
	}
	return a.peerMgr.SendToPeer(peerID, frame)
}

// WriteStreamOpenErr implements exit.StreamWriter.
func (a *Agent) WriteStreamOpenErr(peerID identity.AgentID, streamID uint64, requestID uint64, errorCode uint16, message string) error {
	errPayload := &protocol.StreamOpenErr{
		RequestID: requestID,
		ErrorCode: errorCode,
		Message:   message,
	}
	frame := &protocol.Frame{
		Type:     protocol.FrameStreamOpenErr,
		StreamID: streamID,
		Payload:  errPayload.Encode(),
	}
	return a.peerMgr.SendToPeer(peerID, frame)
}

// WriteStreamClose implements exit.StreamWriter.
func (a *Agent) WriteStreamClose(peerID identity.AgentID, streamID uint64) error {
	frame := &protocol.Frame{
		Type:     protocol.FrameStreamClose,
		StreamID: streamID,
	}
	return a.peerMgr.SendToPeer(peerID, frame)
}

// WriteStreamReset implements exit.StreamWriter and forward.StreamWriter.
func (a *Agent) WriteStreamReset(peerID identity.AgentID, streamID uint64, errorCode uint16) error {
	frame := &protocol.Frame{
		Type:     protocol.FrameStreamReset,
		StreamID: streamID,
		Payload:  (&protocol.StreamReset{ErrorCode: errorCode}).Encode(),
	}
	return a.peerMgr.SendToPeer(peerID, frame)
}

// deriveResponderSessionKey performs E2E key exchange for a responder (receiving a stream open).
// Returns the session key and our ephemeral public key, or an error.
func deriveResponderSessionKey(requestID uint64, remoteEphemeralPub [crypto.KeySize]byte) (*crypto.SessionKey, [crypto.KeySize]byte, error) {
	// Check for zero key (encryption required)
	var zeroKey [crypto.KeySize]byte
	if remoteEphemeralPub == zeroKey {
		return nil, zeroKey, fmt.Errorf("encryption required")
	}

	// Generate ephemeral keypair for E2E encryption
	ephPriv, ephPub, err := crypto.GenerateEphemeralKeypair()
	if err != nil {
		return nil, zeroKey, fmt.Errorf("key generation failed: %w", err)
	}

	// Compute shared secret from ECDH
	sharedSecret, err := crypto.ComputeECDH(ephPriv, remoteEphemeralPub)
	if err != nil {
		crypto.ZeroKey(&ephPriv)
		return nil, zeroKey, fmt.Errorf("key exchange failed: %w", err)
	}

	// Zero out ephemeral private key
	crypto.ZeroKey(&ephPriv)

	// Derive session key - we are the responder
	sessionKey := crypto.DeriveSessionKey(sharedSecret, requestID, remoteEphemeralPub, ephPub, false)
	crypto.ZeroKey(&sharedSecret)

	return sessionKey, ephPub, nil
}

// handleFileTransferStreamOpen is the common handler for file upload/download stream opens.
func (a *Agent) handleFileTransferStreamOpen(peerID identity.AgentID, streamID uint64, requestID uint64, remoteEphemeralPub [crypto.KeySize]byte, isUpload bool) {
	opName := "download"
	if isUpload {
		opName = "upload"
	}

	a.logger.Debug("file "+opName+" stream open",
		logging.KeyPeerID, peerID.ShortString(),
		logging.KeyStreamID, streamID,
		logging.KeyRequestID, requestID)

	// Check if file transfer is enabled
	if a.fileStreamHandler == nil {
		a.WriteStreamOpenErr(peerID, streamID, requestID, protocol.ErrFileTransferDenied, "file transfer disabled")
		return
	}

	// Perform E2E key exchange
	sessionKey, ephPub, err := deriveResponderSessionKey(requestID, remoteEphemeralPub)
	if err != nil {
		a.logger.Warn("rejecting file "+opName+" stream",
			logging.KeyPeerID, peerID.ShortString(),
			logging.KeyStreamID, streamID,
			logging.KeyError, err)
		a.WriteStreamOpenErr(peerID, streamID, requestID, protocol.ErrGeneralFailure, err.Error())
		return
	}

	// Create file transfer stream entry with session key
	fts := &fileTransferStream{
		StreamID:     streamID,
		PeerID:       peerID,
		RequestID:    requestID,
		IsUpload:     isUpload,
		MetaReceived: false,
		sessionKey:   sessionKey,
	}

	a.fileStreamsMu.Lock()
	a.fileStreams[streamKey{peer: peerID, streamID: streamID}] = fts
	a.fileStreamsMu.Unlock()

	// Send ACK with our ephemeral public key for E2E encryption
	a.WriteStreamOpenAck(peerID, streamID, requestID, nil, 0, ephPub)
}

// handleFileUploadStreamOpen handles a file upload stream open request.
func (a *Agent) handleFileUploadStreamOpen(peerID identity.AgentID, streamID uint64, requestID uint64, remoteEphemeralPub [crypto.KeySize]byte) {
	a.handleFileTransferStreamOpen(peerID, streamID, requestID, remoteEphemeralPub, true)
}

// handleFileDownloadStreamOpen handles a file download stream open request.
func (a *Agent) handleFileDownloadStreamOpen(peerID identity.AgentID, streamID uint64, requestID uint64, remoteEphemeralPub [crypto.KeySize]byte) {
	a.handleFileTransferStreamOpen(peerID, streamID, requestID, remoteEphemeralPub, false)
}

// handleShellStreamOpen handles a shell stream open request.
func (a *Agent) handleShellStreamOpen(peerID identity.AgentID, streamID uint64, requestID uint64, interactive bool, remoteEphemeralPub [crypto.KeySize]byte) {
	modeStr := "normal"
	if interactive {
		modeStr = "interactive"
	}
	a.logger.Debug("shell stream open",
		logging.KeyPeerID, peerID.ShortString(),
		logging.KeyStreamID, streamID,
		logging.KeyRequestID, requestID,
		"mode", modeStr)

	// Delegate to shell handler - performs E2E key exchange
	errCode, localEphemeralPub := a.shellHandler.HandleStreamOpen(peerID, streamID, requestID, interactive, remoteEphemeralPub)
	if errCode != 0 {
		a.WriteStreamOpenErr(peerID, streamID, requestID, errCode, protocol.ErrorCodeName(errCode))
		return
	}

	// Send ACK with our ephemeral public key for E2E encryption
	a.WriteStreamOpenAck(peerID, streamID, requestID, nil, 0, localEphemeralPub)
}

// handleFileTransferStreamData processes data for a file transfer stream.
func (a *Agent) handleFileTransferStreamData(peerID identity.AgentID, streamID uint64, data []byte, flags uint8) {
	// Hold lock while checking stream state to avoid race with cleanup
	a.fileStreamsMu.RLock()
	fts := a.fileStreams[streamKey{peer: peerID, streamID: streamID}]
	if fts == nil || fts.Closed {
		a.fileStreamsMu.RUnlock()
		return
	}
	// Get session key while still holding lock
	sessionKey := fts.sessionKey
	a.fileStreamsMu.RUnlock()

	// Decrypt data using E2E session key
	if sessionKey == nil {
		a.logger.Error("no session key for file transfer stream",
			logging.KeyStreamID, streamID)
		a.closeFileTransferStream(peerID, streamID, protocol.ErrGeneralFailure, "no session key", "")
		return
	}

	plaintext, err := sessionKey.DecryptStrict(data)
	if err != nil {
		a.logger.Error("failed to decrypt file transfer data",
			logging.KeyStreamID, streamID,
			logging.KeyError, err)
		a.closeFileTransferStream(peerID, streamID, protocol.ErrGeneralFailure, "decryption failed", "")
		return
	}

	// First data frame contains metadata
	if !fts.MetaReceived {
		meta, err := filetransfer.ParseMetadata(plaintext)
		if err != nil {
			a.logger.Error("invalid file transfer metadata",
				logging.KeyStreamID, streamID,
				logging.KeyError, err)
			a.closeFileTransferStream(peerID, streamID, protocol.ErrConnectionRefused, "invalid metadata", "")
			return
		}
		fts.Meta = meta
		fts.MetaReceived = true

		if fts.IsUpload {
			// Validate upload metadata
			if err := a.fileStreamHandler.ValidateUploadMetadata(meta); err != nil {
				a.logger.Error("file upload validation failed",
					logging.KeyStreamID, streamID,
					logging.KeyError, err)
				a.closeFileTransferStream(peerID, streamID, protocol.ErrNotAllowed, err.Error(), filetransfer.AuthErrorCode(err))
				return
			}

			// Create temp file for streaming upload (write directly to disk)
			tmpFile, err := os.CreateTemp("", "upload-stream-*")
			if err != nil {
				a.logger.Error("failed to create temp file for upload",
					logging.KeyStreamID, streamID,
					logging.KeyError, err)
				a.closeFileTransferStream(peerID, streamID, protocol.ErrWriteFailed, "failed to create temp file", "")
				return
			}
			fts.TempFile = tmpFile

			a.logger.Info("file upload started",
				"path", meta.Path,
				"size", meta.Size,
				"is_directory", meta.IsDirectory,
				"temp_file", tmpFile.Name())
		} else {
			// Validate download metadata and start sending file
			if err := a.fileStreamHandler.ValidateDownloadMetadata(meta); err != nil {
				a.logger.Error("file download validation failed",
					logging.KeyStreamID, streamID,
					logging.KeyError, err)
				a.closeFileTransferStream(peerID, streamID, protocol.ErrNotAllowed, err.Error(), filetransfer.AuthErrorCode(err))
				return
			}
			// Start goroutine to send file data
			go a.sendFileDownload(fts)
		}
		return
	}

	// Subsequent data frames contain file content (only for uploads)
	if fts.IsUpload && fts.TempFile != nil {
		// Write decrypted data directly to disk (streaming, no memory buffering)
		n, err := fts.TempFile.Write(plaintext)
		if err != nil {
			a.logger.Error("failed to write upload data to temp file",
				logging.KeyStreamID, streamID,
				logging.KeyError, err)
			a.closeFileTransferStream(peerID, streamID, protocol.ErrWriteFailed, "write failed", "")
			return
		}
		fts.BytesWritten += int64(n)

		// Check for FIN flag (end of upload)
		if flags&protocol.FlagFinWrite != 0 {
			go a.completeFileUpload(fts)
		}
	}
}

// completeFileUpload processes the uploaded data from temp file to final destination.
func (a *Agent) completeFileUpload(fts *fileTransferStream) {
	defer a.cleanupFileTransferStream(fts.PeerID, fts.StreamID)

	if fts.Meta == nil {
		a.logger.Error("file upload missing metadata", logging.KeyStreamID, fts.StreamID)
		return
	}

	if fts.TempFile == nil {
		a.logger.Error("file upload missing temp file", logging.KeyStreamID, fts.StreamID)
		return
	}

	// Close and reopen temp file for reading
	tmpPath := fts.TempFile.Name()
	fts.TempFile.Close()
	defer os.Remove(tmpPath) // Clean up temp file when done

	tmpFile, err := os.Open(tmpPath)
	if err != nil {
		a.logger.Error("failed to reopen temp file",
			logging.KeyStreamID, fts.StreamID,
			logging.KeyError, err)
		a.WriteStreamOpenErr(fts.PeerID, fts.StreamID, fts.RequestID, protocol.ErrWriteFailed, err.Error())
		return
	}
	defer tmpFile.Close()

	// Write file to final destination. The context cancels on agent shutdown
	// so a wedged upload does not pin completeFileUpload past Stop().
	ctx, cancel := a.ShutdownBoundCtx(context.Background())
	defer cancel()
	written, err := a.fileStreamHandler.WriteUploadedFile(
		ctx,
		fts.Meta.Path,
		tmpFile,
		fts.Meta.Mode,
		fts.Meta.IsDirectory,
		fts.Meta.Compress,
	)

	if err != nil {
		a.logger.Error("file upload write failed",
			logging.KeyStreamID, fts.StreamID,
			logging.KeyError, err)
		a.WriteStreamOpenErr(fts.PeerID, fts.StreamID, fts.RequestID, protocol.ErrWriteFailed, err.Error())
		return
	}

	a.logger.Info("file upload completed",
		"path", fts.Meta.Path,
		"bytes_received", fts.BytesWritten,
		"bytes_written", written)

	// Send close to signal completion
	a.WriteStreamClose(fts.PeerID, fts.StreamID)
}

// sendFileDownload sends file data to the requester.
func (a *Agent) sendFileDownload(fts *fileTransferStream) {
	defer a.cleanupFileTransferStream(fts.PeerID, fts.StreamID)

	if fts.Meta == nil {
		a.logger.Error("file download missing metadata", logging.KeyStreamID, fts.StreamID)
		return
	}

	// Check session key for E2E encryption
	if fts.sessionKey == nil {
		a.logger.Error("no session key for file download stream",
			logging.KeyStreamID, fts.StreamID)
		return
	}

	// Shutdown-bound context so the background tar/gzip goroutines and the
	// optional rate limiter all unblock when the agent is asked to stop --
	// without it, Stop() would wait for a wedged consumer indefinitely.
	ctx, cancel := a.ShutdownBoundCtx(context.Background())
	defer cancel()

	var reader io.Reader
	var size int64
	var mode uint32
	var isDir bool
	var err error
	var originalSize int64

	// Check if this is a resume request
	if fts.Meta.Offset > 0 {
		// Validate that file hasn't changed
		info, statErr := os.Stat(fts.Meta.Path)
		if statErr != nil {
			a.logger.Error("file download stat failed",
				logging.KeyStreamID, fts.StreamID,
				logging.KeyError, statErr)
			a.WriteStreamOpenErr(fts.PeerID, fts.StreamID, fts.RequestID, protocol.ErrFileNotFound, statErr.Error())
			return
		}

		originalSize = info.Size()

		// If original size was provided and doesn't match, reject resume
		if fts.Meta.OriginalSize > 0 && info.Size() != fts.Meta.OriginalSize {
			a.logger.Warn("file changed since partial download, rejecting resume",
				logging.KeyStreamID, fts.StreamID,
				"expected_size", fts.Meta.OriginalSize,
				"actual_size", info.Size())
			a.WriteStreamOpenErr(fts.PeerID, fts.StreamID, fts.RequestID, protocol.ErrResumeFailed, "file size changed")
			return
		}

		// Use offset-aware reader
		reader, size, mode, isDir, err = a.fileStreamHandler.ReadFileForDownloadAtOffset(
			ctx, fts.Meta.Path, fts.Meta.Offset, fts.Meta.Compress)
		if err != nil {
			a.logger.Error("file download read at offset failed",
				logging.KeyStreamID, fts.StreamID,
				"offset", fts.Meta.Offset,
				logging.KeyError, err)
			a.WriteStreamOpenErr(fts.PeerID, fts.StreamID, fts.RequestID, protocol.ErrGeneralFailure, err.Error())
			return
		}
	} else {
		// Normal download from beginning
		reader, size, mode, isDir, err = a.fileStreamHandler.ReadFileForDownload(ctx, fts.Meta.Path, fts.Meta.Compress)
		if err != nil {
			a.logger.Error("file download read failed",
				logging.KeyStreamID, fts.StreamID,
				logging.KeyError, err)
			a.WriteStreamOpenErr(fts.PeerID, fts.StreamID, fts.RequestID, protocol.ErrFileNotFound, err.Error())
			return
		}

		// Get original size for non-resume downloads
		if !isDir {
			if info, err := os.Stat(fts.Meta.Path); err == nil {
				originalSize = info.Size()
			}
		}
	}

	// Apply rate limiting if requested
	if fts.Meta.RateLimit > 0 {
		reader = filetransfer.NewRateLimitedReader(ctx, reader, fts.Meta.RateLimit)
	}

	// Send response metadata as first data frame
	respMeta := &filetransfer.TransferMetadata{
		Path:         fts.Meta.Path,
		Mode:         mode,
		Size:         size,
		IsDirectory:  isDir,
		Compress:     fts.Meta.Compress,
		OriginalSize: originalSize, // Include original size for resume tracking
	}
	metaData, err := filetransfer.EncodeMetadata(respMeta)
	if err != nil {
		a.logger.Error("failed to encode response metadata",
			logging.KeyStreamID, fts.StreamID,
			logging.KeyError, err)
		return
	}
	// Encrypt metadata before sending
	encryptedMeta, err := fts.sessionKey.Encrypt(metaData)
	if err != nil {
		a.logger.Error("failed to encrypt response metadata",
			logging.KeyStreamID, fts.StreamID,
			logging.KeyError, err)
		return
	}
	a.WriteStreamData(fts.PeerID, fts.StreamID, encryptedMeta, 0)

	a.logger.Info("file download started",
		"path", fts.Meta.Path,
		"size", size,
		"offset", fts.Meta.Offset,
		"rate_limit", fts.Meta.RateLimit,
		"is_directory", isDir)

	// Stream file data in chunks
	// Leave room for encryption overhead (nonce + auth tag) plus protocol overhead
	buf := make([]byte, protocol.MaxPayloadSize-100-crypto.NonceSize-crypto.TagSize)
	for {
		n, readErr := reader.Read(buf)
		if n > 0 {
			// Encrypt file data before sending
			encryptedData, encErr := fts.sessionKey.Encrypt(buf[:n])
			if encErr != nil {
				a.logger.Error("failed to encrypt file data",
					logging.KeyStreamID, fts.StreamID,
					logging.KeyError, encErr)
				return
			}

			flags := uint8(0)
			if readErr == io.EOF {
				flags = protocol.FlagFinWrite
			}
			if err := a.WriteStreamData(fts.PeerID, fts.StreamID, encryptedData, flags); err != nil {
				a.logger.Error("failed to send file data",
					logging.KeyStreamID, fts.StreamID,
					logging.KeyError, err)
				return
			}
		}
		if readErr != nil {
			if readErr != io.EOF {
				a.logger.Error("file read error",
					logging.KeyStreamID, fts.StreamID,
					logging.KeyError, readErr)
			}
			break
		}
	}

	// Close any readers that implement io.Closer
	if closer, ok := reader.(io.Closer); ok {
		closer.Close()
	}

	a.logger.Info("file download completed", "path", fts.Meta.Path)

	// Send close to signal completion
	a.WriteStreamClose(fts.PeerID, fts.StreamID)
}

// closeFileTransferStream sends an error and cleans up. errorCode, when
// non-empty, is copied to TransferMetadata.ErrorCode so clients can
// programmatically distinguish error categories (e.g. auth_required vs
// invalid_credentials) without string-matching.
func (a *Agent) closeFileTransferStream(peerID identity.AgentID, streamID uint64, errCode uint16, message string, errorCode string) {
	// Use write lock and mark as closed atomically to avoid race
	a.fileStreamsMu.Lock()
	fts := a.fileStreams[streamKey{peer: peerID, streamID: streamID}]
	if fts == nil {
		a.fileStreamsMu.Unlock()
		return
	}
	// Mark as closed immediately to prevent concurrent handling
	fts.Closed = true
	// Copy fields we need after releasing lock
	sessionKey := fts.sessionKey
	requestID := fts.RequestID
	a.fileStreamsMu.Unlock()

	// If stream has a session key, it's already open and we need to send
	// an encrypted error response instead of STREAM_OPEN_ERR
	if sessionKey != nil {
		// Send error as encrypted metadata response
		errMeta := &filetransfer.TransferMetadata{
			Error:     message,
			ErrorCode: errorCode,
		}
		metaData, err := filetransfer.EncodeMetadata(errMeta)
		if err == nil {
			encryptedMeta, encErr := sessionKey.Encrypt(metaData)
			if encErr == nil {
				a.WriteStreamData(peerID, streamID, encryptedMeta, protocol.FlagFinWrite)
			}
		}
		a.WriteStreamClose(peerID, streamID)
	} else {
		// Stream not yet open, use STREAM_OPEN_ERR
		a.WriteStreamOpenErr(peerID, streamID, requestID, errCode, message)
	}
	a.cleanupFileTransferStream(peerID, streamID)
}

// cleanupFileTransferStream removes a file transfer stream from tracking.
func (a *Agent) cleanupFileTransferStream(peerID identity.AgentID, streamID uint64) {
	key := streamKey{peer: peerID, streamID: streamID}
	a.fileStreamsMu.Lock()
	fts, ok := a.fileStreams[key]
	if ok {
		fts.Closed = true
		delete(a.fileStreams, key)
	}
	a.fileStreamsMu.Unlock()

	// Clean up temp file if it exists (outside lock to avoid holding it during I/O)
	if ok && fts.TempFile != nil {
		tmpPath := fts.TempFile.Name()
		fts.TempFile.Close()
		os.Remove(tmpPath)
	}
}

// getFileTransferStream returns a file transfer stream by ID.
func (a *Agent) getFileTransferStream(peerID identity.AgentID, streamID uint64) *fileTransferStream {
	a.fileStreamsMu.RLock()
	defer a.fileStreamsMu.RUnlock()
	return a.fileStreams[streamKey{peer: peerID, streamID: streamID}]
}

// ============================================================================
// File Transfer Client Methods (initiating transfers to remote agents)
// ============================================================================

// UploadFile uploads a local file or directory to a remote agent via stream-based transfer.
// The transfer uses the mesh network to reach the target agent.
func (a *Agent) UploadFile(ctx context.Context, targetID identity.AgentID, localPath, remotePath string, opts health.TransferOptions, progress health.FileTransferProgress) error {
	// Check if file transfer is enabled locally (for validation config)
	if a.fileStreamHandler == nil {
		return fmt.Errorf("file transfer is disabled")
	}

	// Get file info
	info, err := os.Stat(localPath)
	if err != nil {
		return fmt.Errorf("cannot access local path: %w", err)
	}

	// Find path to target agent
	nextHop, remainingPath, conn, err := a.findPathToAgent(targetID)
	if err != nil {
		return fmt.Errorf("no route to agent %s: %w", targetID.ShortString(), err)
	}

	// Allocate stream ID
	streamID := conn.NextStreamID()

	// Generate ephemeral keypair for E2E encryption key exchange
	ephPriv, ephPub, err := crypto.GenerateEphemeralKeypair()
	if err != nil {
		return fmt.Errorf("generate ephemeral key: %w", err)
	}

	// Create pending stream (5 min timeout for large files)
	pending := a.streamMgr.OpenStream(streamID, nextHop, protocol.FileTransferUpload, 0, 5*time.Minute)

	// Store ephemeral keys in pending request for later key derivation
	a.streamMgr.SetPendingEphemeralKeys(pending.RequestID, ephPriv, ephPub)

	// Build and send STREAM_OPEN with ephemeral public key
	// Domain addresses need length prefix byte
	domainAddr := protocol.FileTransferUpload
	domainBytes := append([]byte{byte(len(domainAddr))}, []byte(domainAddr)...)
	openPayload := &protocol.StreamOpen{
		RequestID:       pending.RequestID,
		AddressType:     protocol.AddrTypeDomain,
		Address:         domainBytes,
		Port:            0,
		RemainingPath:   remainingPath,
		EphemeralPubKey: ephPub,
	}

	frame := &protocol.Frame{
		Type:     protocol.FrameStreamOpen,
		StreamID: streamID,
		Payload:  openPayload.Encode(),
	}

	if err := a.peerMgr.SendToPeer(nextHop, frame); err != nil {
		crypto.ZeroKey(&ephPriv)
		return fmt.Errorf("send stream open: %w", err)
	}

	// Wait for STREAM_OPEN_ACK
	var result *stream.StreamOpenResult
	select {
	case result = <-pending.ResultCh:
		if result.Error != nil {
			crypto.ZeroKey(&ephPriv)
			return fmt.Errorf("stream open failed: %w", result.Error)
		}
	case <-ctx.Done():
		crypto.ZeroKey(&ephPriv)
		return ctx.Err()
	}

	// Derive session key from ECDH with remote agent's ephemeral public key
	sharedSecret, err := crypto.ComputeECDH(ephPriv, result.RemoteEphemeral)
	if err != nil {
		crypto.ZeroKey(&ephPriv)
		return fmt.Errorf("compute ECDH: %w", err)
	}

	// Zero out ephemeral private key after computing shared secret
	crypto.ZeroKey(&ephPriv)

	// Derive session key - we are the initiator
	sessionKey := crypto.DeriveSessionKey(sharedSecret, pending.RequestID, ephPub, result.RemoteEphemeral, true)
	crypto.ZeroKey(&sharedSecret)

	// Build metadata
	var fileSize int64 = -1
	isDirectory := info.IsDir()
	var fileMode uint32 = uint32(info.Mode().Perm())
	if !isDirectory {
		fileSize = info.Size()
	}

	authEnv, err := a.signManagementEnvelope(targetID, crypto.OpFile, nil)
	if err != nil {
		a.WriteStreamClose(nextHop, streamID)
		return err
	}
	meta := &filetransfer.TransferMetadata{
		Path:        remotePath,
		Mode:        fileMode,
		Size:        fileSize,
		IsDirectory: isDirectory,
		Auth:        authEnv,
		Compress:    true,
		RateLimit:   opts.RateLimit,
	}

	// Encode and encrypt metadata
	metaData, err := filetransfer.EncodeMetadata(meta)
	if err != nil {
		a.WriteStreamClose(nextHop, streamID)
		return fmt.Errorf("encode metadata: %w", err)
	}

	encryptedMeta, err := sessionKey.Encrypt(metaData)
	if err != nil {
		a.WriteStreamClose(nextHop, streamID)
		return fmt.Errorf("encrypt metadata: %w", err)
	}

	if err := a.WriteStreamData(nextHop, streamID, encryptedMeta, 0); err != nil {
		return fmt.Errorf("send metadata: %w", err)
	}

	// Capture the stream reference now (before streaming) so we can read the
	// server's response after the stream content. After STREAM_CLOSE arrives
	// the manager removes the stream from its map, so a later GetStream call
	// would return nil; the *Stream object remains valid via this captured
	// reference and Read() correctly drains buffered data even on a closed
	// stream.
	s := a.streamMgr.GetStream(nextHop, streamID)

	// Brief check for immediate server rejection (e.g., file too large, path
	// not allowed). This is an optimization to bail before streaming a large
	// file -- it is best-effort and does not need to catch every rejection
	// (the post-stream drain at the end of this function is the
	// authoritative check).
	if s != nil {
		responseData, readErr := s.ReadWithTimeout(200 * time.Millisecond)
		if readErr == nil && len(responseData) > 0 {
			// Got response data - check if it's an error
			decryptedResponse, decErr := sessionKey.DecryptStrict(responseData)
			if decErr == nil {
				responseMeta, parseErr := filetransfer.ParseMetadata(decryptedResponse)
				if parseErr == nil && responseMeta.Error != "" {
					a.WriteStreamClose(nextHop, streamID)
					return &filetransfer.RemoteError{Code: responseMeta.ErrorCode, Message: responseMeta.Error}
				}
			}
		}
		// Timeout or no data is normal - server hasn't rejected yet
	}

	// Stream file/directory content with E2E encryption
	var written int64
	if isDirectory {
		// Tar and stream directory
		pr, pw := io.Pipe()

		// Start tar in goroutine
		go func() {
			err := filetransfer.TarDirectory(ctx, localPath, pw)
			if err != nil {
				pw.CloseWithError(err)
			} else {
				pw.Close()
			}
		}()

		// Apply rate limiting if requested
		var reader io.Reader = pr
		if opts.RateLimit > 0 {
			reader = filetransfer.NewRateLimitedReader(ctx, pr, opts.RateLimit)
		}

		// Stream tar data in chunks with encryption
		written, err = a.streamFileContent(ctx, nextHop, streamID, reader, -1, progress, sessionKey)
		if err != nil {
			pr.Close()
			return fmt.Errorf("stream directory: %w", err)
		}
	} else {
		// Open file and optionally compress
		f, err := os.Open(localPath)
		if err != nil {
			a.WriteStreamClose(nextHop, streamID)
			return fmt.Errorf("open file: %w", err)
		}
		defer f.Close()

		// Compress with gzip if requested
		if meta.Compress {
			pr, pw := io.Pipe()
			go func() {
				gzw := gzip.NewWriter(pw)
				_, copyErr := io.Copy(gzw, f)
				gzw.Close()
				if copyErr != nil {
					pw.CloseWithError(copyErr)
				} else {
					pw.Close()
				}
			}()

			// Apply rate limiting if requested
			var reader io.Reader = pr
			if opts.RateLimit > 0 {
				reader = filetransfer.NewRateLimitedReader(ctx, pr, opts.RateLimit)
			}

			written, err = a.streamFileContent(ctx, nextHop, streamID, reader, -1, progress, sessionKey)
			if err != nil {
				pr.Close()
				return fmt.Errorf("stream file: %w", err)
			}
		} else {
			// Apply rate limiting if requested
			var reader io.Reader = f
			if opts.RateLimit > 0 {
				reader = filetransfer.NewRateLimitedReader(ctx, f, opts.RateLimit)
			}

			written, err = a.streamFileContent(ctx, nextHop, streamID, reader, fileSize, progress, sessionKey)
			if err != nil {
				return fmt.Errorf("stream file: %w", err)
			}
		}
	}

	a.logger.Info("file upload completed",
		"target", targetID.ShortString(),
		"local_path", localPath,
		"remote_path", remotePath,
		"bytes_sent", written)

	// Wait for stream to close (server sends STREAM_CLOSE on completion or
	// rejection). After close, drain any buffered response: the server may
	// have sent encrypted error metadata before STREAM_CLOSE if validation
	// or write failed. Without this drain we'd return success even when the
	// server rejected the upload (a flaky test source for restricted-path
	// and bad-password tests). We use the captured `s` reference because
	// the manager removes the stream from its map on STREAM_CLOSE, so
	// A GetStream call here would return nil.
	if s != nil {
		select {
		case <-s.Done():
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(30 * time.Second):
			return fmt.Errorf("timeout waiting for upload acknowledgement")
		}

		// Drain any buffered response (error metadata sent before STREAM_CLOSE).
		// Read with a short timeout per chunk; on EOF / no-data we are done.
		for {
			respData, readErr := s.ReadWithTimeout(50 * time.Millisecond)
			if readErr != nil || len(respData) == 0 {
				break
			}
			decrypted, decErr := sessionKey.DecryptStrict(respData)
			if decErr != nil {
				continue
			}
			respMeta, parseErr := filetransfer.ParseMetadata(decrypted)
			if parseErr == nil && respMeta.Error != "" {
				return &filetransfer.RemoteError{Code: respMeta.ErrorCode, Message: respMeta.Error}
			}
		}
	}

	return nil
}

// DownloadFile downloads a file or directory from a remote agent via stream-based transfer.
func (a *Agent) DownloadFile(ctx context.Context, targetID identity.AgentID, remotePath, localPath string, opts health.TransferOptions, progress health.FileTransferProgress) error {
	// Check if file transfer is enabled locally
	if a.fileStreamHandler == nil {
		return fmt.Errorf("file transfer is disabled")
	}

	// Find path to target agent
	nextHop, remainingPath, conn, err := a.findPathToAgent(targetID)
	if err != nil {
		return fmt.Errorf("no route to agent %s: %w", targetID.ShortString(), err)
	}

	// Allocate stream ID
	streamID := conn.NextStreamID()

	// Generate ephemeral keypair for E2E encryption key exchange
	ephPriv, ephPub, err := crypto.GenerateEphemeralKeypair()
	if err != nil {
		return fmt.Errorf("generate ephemeral key: %w", err)
	}

	// Create pending stream (5 min timeout for large files)
	pending := a.streamMgr.OpenStream(streamID, nextHop, protocol.FileTransferDownload, 0, 5*time.Minute)

	// Store ephemeral keys in pending request for later key derivation
	a.streamMgr.SetPendingEphemeralKeys(pending.RequestID, ephPriv, ephPub)

	// Build and send STREAM_OPEN with ephemeral public key
	// Domain addresses need length prefix byte
	downloadDomainAddr := protocol.FileTransferDownload
	downloadDomainBytes := append([]byte{byte(len(downloadDomainAddr))}, []byte(downloadDomainAddr)...)
	openPayload := &protocol.StreamOpen{
		RequestID:       pending.RequestID,
		AddressType:     protocol.AddrTypeDomain,
		Address:         downloadDomainBytes,
		Port:            0,
		RemainingPath:   remainingPath,
		EphemeralPubKey: ephPub,
	}

	frame := &protocol.Frame{
		Type:     protocol.FrameStreamOpen,
		StreamID: streamID,
		Payload:  openPayload.Encode(),
	}

	if err := a.peerMgr.SendToPeer(nextHop, frame); err != nil {
		crypto.ZeroKey(&ephPriv)
		return fmt.Errorf("send stream open: %w", err)
	}

	// Wait for STREAM_OPEN_ACK
	var openResult *stream.StreamOpenResult
	select {
	case result := <-pending.ResultCh:
		if result.Error != nil {
			crypto.ZeroKey(&ephPriv)
			return fmt.Errorf("stream open failed: %w", result.Error)
		}
		openResult = result
	case <-ctx.Done():
		crypto.ZeroKey(&ephPriv)
		return ctx.Err()
	}

	// Derive session key from ECDH with remote agent's ephemeral public key
	sharedSecret, err := crypto.ComputeECDH(ephPriv, openResult.RemoteEphemeral)
	if err != nil {
		crypto.ZeroKey(&ephPriv)
		return fmt.Errorf("compute ECDH: %w", err)
	}

	// Zero out ephemeral private key after computing shared secret
	crypto.ZeroKey(&ephPriv)

	// Derive session key - we are the initiator
	sessionKey := crypto.DeriveSessionKey(sharedSecret, pending.RequestID, ephPub, openResult.RemoteEphemeral, true)
	crypto.ZeroKey(&sharedSecret)

	// Send encrypted request metadata (path to download)
	authEnv, err := a.signManagementEnvelope(targetID, crypto.OpFile, nil)
	if err != nil {
		a.WriteStreamClose(nextHop, streamID)
		return err
	}
	meta := &filetransfer.TransferMetadata{
		Path:         remotePath,
		Auth:         authEnv,
		Compress:     true,
		RateLimit:    opts.RateLimit,
		Offset:       opts.Offset,
		OriginalSize: opts.OriginalSize,
	}

	metaData, err := filetransfer.EncodeMetadata(meta)
	if err != nil {
		a.WriteStreamClose(nextHop, streamID)
		return fmt.Errorf("encode metadata: %w", err)
	}

	encryptedMeta, err := sessionKey.Encrypt(metaData)
	if err != nil {
		a.WriteStreamClose(nextHop, streamID)
		return fmt.Errorf("encrypt metadata: %w", err)
	}

	if err := a.WriteStreamData(nextHop, streamID, encryptedMeta, 0); err != nil {
		return fmt.Errorf("send metadata: %w", err)
	}

	// Wait for response metadata (first data frame) and decrypt
	s := openResult.Stream
	responseData, err := s.ReadWithTimeout(30 * time.Second)
	if err != nil {
		return fmt.Errorf("read response metadata: %w", err)
	}

	decryptedResponse, err := sessionKey.DecryptStrict(responseData)
	if err != nil {
		return fmt.Errorf("decrypt response metadata: %w", err)
	}

	responseMeta, err := filetransfer.ParseMetadata(decryptedResponse)
	if err != nil {
		return fmt.Errorf("parse response metadata: %w", err)
	}

	a.logger.Info("file download started",
		"target", targetID.ShortString(),
		"remote_path", remotePath,
		"local_path", localPath,
		"size", responseMeta.Size,
		"is_directory", responseMeta.IsDirectory)

	// Receive file data with E2E decryption and write to local path
	var written int64
	if responseMeta.IsDirectory {
		// Receive tar stream and extract
		written, err = a.receiveAndExtractDirectory(ctx, s, localPath, responseMeta.Size, progress, sessionKey)
		if err != nil {
			return fmt.Errorf("receive directory: %w", err)
		}
	} else {
		// Receive file data
		written, err = a.receiveAndWriteFile(ctx, s, localPath, responseMeta.Mode, responseMeta.Size, responseMeta.Compress, progress, sessionKey)
		if err != nil {
			return fmt.Errorf("receive file: %w", err)
		}
	}

	a.logger.Info("file download completed",
		"target", targetID.ShortString(),
		"remote_path", remotePath,
		"local_path", localPath,
		"bytes_received", written)

	// Send STREAM_CLOSE to acknowledge completion
	a.WriteStreamClose(nextHop, streamID)

	return nil
}

// DownloadFileStream opens a streaming download from a remote agent.
// Returns a reader that streams file data directly without writing to disk.
// The caller must call Close() on the result when done.
func (a *Agent) DownloadFileStream(ctx context.Context, targetID identity.AgentID, remotePath string, opts health.TransferOptions) (*health.DownloadStreamResult, error) {
	// Check if file transfer is enabled locally
	if a.fileStreamHandler == nil {
		return nil, fmt.Errorf("file transfer is disabled")
	}

	// Find path to target agent
	nextHop, remainingPath, conn, err := a.findPathToAgent(targetID)
	if err != nil {
		return nil, fmt.Errorf("no route to agent %s: %w", targetID.ShortString(), err)
	}

	// Allocate stream ID
	streamID := conn.NextStreamID()

	// Generate ephemeral keypair for E2E encryption key exchange
	ephPriv, ephPub, err := crypto.GenerateEphemeralKeypair()
	if err != nil {
		return nil, fmt.Errorf("generate ephemeral key: %w", err)
	}

	// Create pending stream (5 min timeout for large files)
	pending := a.streamMgr.OpenStream(streamID, nextHop, protocol.FileTransferDownload, 0, 5*time.Minute)

	// Store ephemeral keys in pending request for later key derivation
	a.streamMgr.SetPendingEphemeralKeys(pending.RequestID, ephPriv, ephPub)

	// Build and send STREAM_OPEN with ephemeral public key
	downloadDomainAddr := protocol.FileTransferDownload
	downloadDomainBytes := append([]byte{byte(len(downloadDomainAddr))}, []byte(downloadDomainAddr)...)
	openPayload := &protocol.StreamOpen{
		RequestID:       pending.RequestID,
		AddressType:     protocol.AddrTypeDomain,
		Address:         downloadDomainBytes,
		Port:            0,
		RemainingPath:   remainingPath,
		EphemeralPubKey: ephPub,
	}

	frame := &protocol.Frame{
		Type:     protocol.FrameStreamOpen,
		StreamID: streamID,
		Payload:  openPayload.Encode(),
	}

	if err := a.peerMgr.SendToPeer(nextHop, frame); err != nil {
		crypto.ZeroKey(&ephPriv)
		return nil, fmt.Errorf("send stream open: %w", err)
	}

	// Wait for STREAM_OPEN_ACK
	var openResult *stream.StreamOpenResult
	select {
	case result := <-pending.ResultCh:
		if result.Error != nil {
			crypto.ZeroKey(&ephPriv)
			return nil, fmt.Errorf("stream open failed: %w", result.Error)
		}
		openResult = result
	case <-ctx.Done():
		crypto.ZeroKey(&ephPriv)
		return nil, ctx.Err()
	}

	// Derive session key from ECDH with remote agent's ephemeral public key
	sharedSecret, err := crypto.ComputeECDH(ephPriv, openResult.RemoteEphemeral)
	if err != nil {
		crypto.ZeroKey(&ephPriv)
		return nil, fmt.Errorf("compute ECDH: %w", err)
	}

	// Zero out ephemeral private key after computing shared secret
	crypto.ZeroKey(&ephPriv)

	// Derive session key - we are the initiator
	sessionKey := crypto.DeriveSessionKey(sharedSecret, pending.RequestID, ephPub, openResult.RemoteEphemeral, true)
	crypto.ZeroKey(&sharedSecret)

	// Send encrypted request metadata (path to download)
	authEnv, err := a.signManagementEnvelope(targetID, crypto.OpFile, nil)
	if err != nil {
		a.WriteStreamClose(nextHop, streamID)
		return nil, err
	}
	meta := &filetransfer.TransferMetadata{
		Path:         remotePath,
		Auth:         authEnv,
		Compress:     true,
		RateLimit:    opts.RateLimit,
		Offset:       opts.Offset,
		OriginalSize: opts.OriginalSize,
	}

	metaData, err := filetransfer.EncodeMetadata(meta)
	if err != nil {
		a.WriteStreamClose(nextHop, streamID)
		return nil, fmt.Errorf("encode metadata: %w", err)
	}

	encryptedMeta, err := sessionKey.Encrypt(metaData)
	if err != nil {
		a.WriteStreamClose(nextHop, streamID)
		return nil, fmt.Errorf("encrypt metadata: %w", err)
	}

	if err := a.WriteStreamData(nextHop, streamID, encryptedMeta, 0); err != nil {
		return nil, fmt.Errorf("send metadata: %w", err)
	}

	// Wait for response metadata (first data frame) and decrypt
	s := openResult.Stream
	responseData, err := s.ReadWithTimeout(30 * time.Second)
	if err != nil {
		a.WriteStreamClose(nextHop, streamID)
		return nil, fmt.Errorf("read response metadata: %w", err)
	}

	decryptedResponse, err := sessionKey.DecryptStrict(responseData)
	if err != nil {
		a.WriteStreamClose(nextHop, streamID)
		return nil, fmt.Errorf("decrypt response metadata: %w", err)
	}

	responseMeta, err := filetransfer.ParseMetadata(decryptedResponse)
	if err != nil {
		a.WriteStreamClose(nextHop, streamID)
		return nil, fmt.Errorf("parse response metadata: %w", err)
	}

	// Check for error response from server
	if responseMeta.Error != "" {
		a.WriteStreamClose(nextHop, streamID)
		return nil, &filetransfer.RemoteError{Code: responseMeta.ErrorCode, Message: responseMeta.Error}
	}

	a.logger.Info("file download stream started",
		"target", targetID.ShortString(),
		"remote_path", remotePath,
		"size", responseMeta.Size,
		"original_size", responseMeta.OriginalSize,
		"is_directory", responseMeta.IsDirectory,
		"compressed", responseMeta.Compress)

	// Create a stream reader that reads from the stream with E2E decryption.
	// Directories ship to the HTTP handler as raw gz(tar(...)) so the
	// Content-Type: application/gzip header matches the body bytes; only
	// regular file downloads have their gzip wrapper peeled off here.
	reader := &streamReader{
		stream:     s,
		ctx:        ctx,
		compressed: responseMeta.Compress && !responseMeta.IsDirectory,
		sessionKey: sessionKey,
	}

	// Cleanup function to close the stream when done
	cleanup := func() {
		a.WriteStreamClose(nextHop, streamID)
	}

	return &health.DownloadStreamResult{
		Reader:       reader,
		Size:         responseMeta.Size,
		OriginalSize: responseMeta.OriginalSize,
		Mode:         responseMeta.Mode,
		IsDirectory:  responseMeta.IsDirectory,
		Compressed:   responseMeta.Compress,
		Close:        cleanup,
	}, nil
}

// streamReader wraps a stream for io.Reader interface with E2E decryption.
type streamReader struct {
	stream     *stream.Stream
	ctx        context.Context
	compressed bool
	gzReader   io.ReadCloser
	buffer     []byte
	bufOffset  int
	eof        bool
	sessionKey *crypto.SessionKey
}

func (r *streamReader) Read(p []byte) (int, error) {
	// If we have a gzip reader, read from it
	if r.gzReader != nil {
		return r.gzReader.Read(p)
	}

	// If we have buffered data, return it first
	if r.bufOffset < len(r.buffer) {
		n := copy(p, r.buffer[r.bufOffset:])
		r.bufOffset += n
		return n, nil
	}

	if r.eof {
		return 0, io.EOF
	}

	// Read from stream
	data, err := r.stream.ReadWithTimeout(30 * time.Second)
	if err != nil {
		if err == io.EOF {
			r.eof = true
		}
		return 0, err
	}

	// Decrypt data using E2E session key
	if r.sessionKey != nil {
		plaintext, decErr := r.sessionKey.DecryptStrict(data)
		if decErr != nil {
			return 0, fmt.Errorf("decrypt stream data: %w", decErr)
		}
		data = plaintext
	}

	// If compressed and this is the first read, set up gzip reader.
	if r.compressed && r.gzReader == nil {
		// We need to create a gzip reader, but it needs an io.Reader
		// Create a pipe to feed data to the gzip reader
		pr, pw := io.Pipe()

		// Start goroutine to feed data to the pipe
		go func() {
			// Write the first chunk (already decrypted)
			pw.Write(data)

			// Continue reading from stream and writing to pipe
			for {
				chunk, err := r.stream.ReadWithTimeout(30 * time.Second)
				if err != nil {
					if err == io.EOF {
						pw.Close()
					} else {
						pw.CloseWithError(err)
					}
					return
				}
				// Decrypt chunk using E2E session key
				if r.sessionKey != nil {
					plaintext, decErr := r.sessionKey.DecryptStrict(chunk)
					if decErr != nil {
						pw.CloseWithError(fmt.Errorf("decrypt stream chunk: %w", decErr))
						return
					}
					chunk = plaintext
				}
				if _, err := pw.Write(chunk); err != nil {
					pw.CloseWithError(err)
					return
				}
			}
		}()

		gzr, err := gzip.NewReader(pr)
		if err != nil {
			pr.Close()
			return 0, fmt.Errorf("create gzip reader: %w", err)
		}
		r.gzReader = gzr
		return r.gzReader.Read(p)
	}

	// Not compressed, return data directly
	r.buffer = data
	r.bufOffset = 0
	n := copy(p, r.buffer)
	r.bufOffset = n
	return n, nil
}

func (r *streamReader) Close() error {
	if r.gzReader != nil {
		return r.gzReader.Close()
	}
	return nil
}

// findPathToAgent finds the next hop and remaining path to reach a specific agent by ID.
// Returns: nextHop ID, remaining path (for STREAM_OPEN), peer connection, error.
func (a *Agent) findPathToAgent(targetID identity.AgentID) (identity.AgentID, []identity.AgentID, *peer.Connection, error) {
	// Check if target is a direct peer
	conn := a.peerMgr.GetPeer(targetID)
	if conn != nil {
		// Direct connection, no path needed
		return targetID, nil, conn, nil
	}

	// Primary: look up in agent presence table
	if agentRoute := a.routeMgr.LookupAgent(targetID); agentRoute != nil {
		conn = a.peerMgr.GetPeer(agentRoute.NextHop)
		if conn != nil {
			var remainingPath []identity.AgentID
			if len(agentRoute.Path) > 1 {
				remainingPath = make([]identity.AgentID, len(agentRoute.Path)-1)
				copy(remainingPath, agentRoute.Path[1:])
			}
			return agentRoute.NextHop, remainingPath, conn, nil
		}
	}

	// Fallback: scan CIDR routes for OriginAgent match (backward compat with old agents)
	allRoutes := a.routeMgr.GetFullRoutesForAdvertise(identity.AgentID{})
	var bestRoute *routing.Route
	for _, route := range allRoutes {
		if route.OriginAgent == targetID {
			if bestRoute == nil || route.Metric < bestRoute.Metric {
				bestRoute = route
			}
		}
	}

	if bestRoute == nil {
		return identity.AgentID{}, nil, nil, fmt.Errorf("agent %s not found in routing table", targetID.ShortString())
	}

	conn = a.peerMgr.GetPeer(bestRoute.NextHop)
	if conn == nil {
		return identity.AgentID{}, nil, nil, fmt.Errorf("next hop %s not connected", bestRoute.NextHop.ShortString())
	}

	var remainingPath []identity.AgentID
	if len(bestRoute.Path) > 1 {
		remainingPath = make([]identity.AgentID, len(bestRoute.Path)-1)
		copy(remainingPath, bestRoute.Path[1:])
	}

	return bestRoute.NextHop, remainingPath, conn, nil
}

// streamFileContent streams data from a reader to the peer in chunks with E2E encryption.
func (a *Agent) streamFileContent(ctx context.Context, peerID identity.AgentID, streamID uint64, r io.Reader, totalSize int64, progress health.FileTransferProgress, sessionKey *crypto.SessionKey) (int64, error) {
	// Leave room for frame overhead and encryption overhead (nonce + auth tag)
	buf := make([]byte, protocol.MaxPayloadSize-100-crypto.EncryptionOverhead)
	var totalWritten int64

	for {
		select {
		case <-ctx.Done():
			return totalWritten, ctx.Err()
		default:
		}

		n, readErr := r.Read(buf)
		if n > 0 {
			// Encrypt data before sending
			encryptedData, encErr := sessionKey.Encrypt(buf[:n])
			if encErr != nil {
				return totalWritten, fmt.Errorf("encrypt data: %w", encErr)
			}

			flags := uint8(0)
			if readErr == io.EOF {
				flags = protocol.FlagFinWrite
			}

			if err := a.WriteStreamData(peerID, streamID, encryptedData, flags); err != nil {
				return totalWritten, fmt.Errorf("write data: %w", err)
			}
			totalWritten += int64(n)

			if progress != nil {
				progress(totalWritten, totalSize)
			}
		}

		if readErr != nil {
			if readErr == io.EOF {
				// Ensure FIN_WRITE is sent even if last read was empty
				if n == 0 {
					// Send empty encrypted payload with FIN flag
					emptyEncrypted, encErr := sessionKey.Encrypt(nil)
					if encErr != nil {
						return totalWritten, fmt.Errorf("encrypt fin: %w", encErr)
					}
					if err := a.WriteStreamData(peerID, streamID, emptyEncrypted, protocol.FlagFinWrite); err != nil {
						return totalWritten, fmt.Errorf("write fin: %w", err)
					}
				}
				break
			}
			return totalWritten, fmt.Errorf("read error: %w", readErr)
		}
	}

	return totalWritten, nil
}

// receiveEncryptedStreamData receives and decrypts data from a stream into a buffer.
// Returns the total bytes received (decrypted).
func (a *Agent) receiveEncryptedStreamData(ctx context.Context, s *stream.Stream, sessionKey *crypto.SessionKey, totalSize int64, progress health.FileTransferProgress) (*bytes.Buffer, int64, error) {
	var dataBuf bytes.Buffer
	var totalReceived int64

	for {
		select {
		case <-ctx.Done():
			return &dataBuf, totalReceived, ctx.Err()
		default:
		}

		data, err := s.ReadWithTimeout(30 * time.Second)
		if err != nil {
			if err == io.EOF {
				break
			}
			// Try to drain remaining data if stream is closed
			if s.IsClosed() {
				for {
					select {
					case remaining := <-s.ReadBuffer():
						plaintext, decErr := sessionKey.DecryptStrict(remaining)
						if decErr != nil {
							return &dataBuf, totalReceived, fmt.Errorf("decrypt remaining data: %w", decErr)
						}
						dataBuf.Write(plaintext)
						totalReceived += int64(len(plaintext))
					default:
						return &dataBuf, totalReceived, nil
					}
				}
			}
			return &dataBuf, totalReceived, fmt.Errorf("read stream: %w", err)
		}

		if len(data) == 0 {
			continue
		}

		plaintext, decErr := sessionKey.DecryptStrict(data)
		if decErr != nil {
			return &dataBuf, totalReceived, fmt.Errorf("decrypt data: %w", decErr)
		}

		dataBuf.Write(plaintext)
		totalReceived += int64(len(plaintext))

		if progress != nil {
			progress(totalReceived, totalSize)
		}
	}

	return &dataBuf, totalReceived, nil
}

// receiveAndWriteFile receives file data from a stream, decrypts with E2E session key, and writes to disk.
func (a *Agent) receiveAndWriteFile(ctx context.Context, s *stream.Stream, localPath string, mode uint32, totalSize int64, compressed bool, progress health.FileTransferProgress, sessionKey *crypto.SessionKey) (int64, error) {
	// Create parent directories
	dir := filepath.Dir(localPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return 0, fmt.Errorf("create directory: %w", err)
	}

	// Create the file
	f, err := os.OpenFile(localPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(mode))
	if err != nil {
		return 0, fmt.Errorf("create file: %w", err)
	}
	defer f.Close()

	// Receive encrypted data
	dataBuf, _, err := a.receiveEncryptedStreamData(ctx, s, sessionKey, totalSize, progress)
	if err != nil {
		return 0, err
	}

	// Decompress if needed and write to file
	var reader io.Reader = dataBuf
	if compressed {
		gzr, err := gzip.NewReader(dataBuf)
		if err != nil {
			return 0, fmt.Errorf("create gzip reader: %w", err)
		}
		defer gzr.Close()
		reader = gzr
	}

	written, err := io.Copy(f, reader)
	if err != nil {
		return 0, fmt.Errorf("write file: %w", err)
	}

	return written, nil
}

// receiveAndExtractDirectory receives tar stream data, decrypts with E2E session key, and extracts to a directory.
func (a *Agent) receiveAndExtractDirectory(ctx context.Context, s *stream.Stream, localPath string, totalSize int64, progress health.FileTransferProgress, sessionKey *crypto.SessionKey) (int64, error) {
	// Receive encrypted data
	dataBuf, _, err := a.receiveEncryptedStreamData(ctx, s, sessionKey, totalSize, progress)
	if err != nil {
		return 0, err
	}

	// Extract tar.gz to directory
	if err := filetransfer.UntarDirectory(ctx, dataBuf, localPath); err != nil {
		return 0, fmt.Errorf("extract directory: %w", err)
	}

	// Calculate extracted size
	size, _ := filetransfer.CalculateDirectorySize(localPath)
	return size, nil
}

// OpenShellStream opens a shell stream to a remote agent.
// Implements health.ShellProvider interface.
func (a *Agent) OpenShellStream(ctx context.Context, targetID identity.AgentID, meta *shell.ShellMeta, interactive bool) (*health.ShellSession, error) {
	// Sign the metadata before we send anything on the wire. This is the
	// authorization step: refusing here when the agent cannot sign keeps
	// unauthenticated requests from ever reaching the remote.
	env, err := a.signManagementEnvelope(targetID, crypto.OpShell, nil)
	if err != nil {
		return nil, err
	}
	meta.Auth = env

	// Find route to target using existing helper
	nextHop, remainingPath, conn, err := a.findPathToAgent(targetID)
	if err != nil {
		return nil, fmt.Errorf("no route to agent %s: %w", targetID.ShortString(), err)
	}

	// Determine shell address based on mode
	destAddr := protocol.ShellStream
	if interactive {
		destAddr = protocol.ShellInteractive
	}

	// Allocate stream ID from connection
	streamID := conn.NextStreamID()

	// Generate ephemeral keypair for E2E encryption key exchange
	ephPriv, ephPub, err := crypto.GenerateEphemeralKeypair()
	if err != nil {
		return nil, fmt.Errorf("generate ephemeral key: %w", err)
	}

	// Create pending stream request
	pending := a.streamMgr.OpenStream(streamID, nextHop, destAddr, 0, a.cfg.Limits.StreamOpenTimeout)

	// Store ephemeral keys in pending request for later key derivation
	a.streamMgr.SetPendingEphemeralKeys(pending.RequestID, ephPriv, ephPub)

	// Create adapter for this client session
	adapter := health.NewShellStreamAdapter(streamID, targetID, func() {
		a.cleanupShellClientStream(nextHop, streamID)
	})

	// Register the client stream
	a.shellClientMu.Lock()
	a.shellClientStreams[streamKey{peer: nextHop, streamID: streamID}] = adapter
	a.shellClientMu.Unlock()

	// Build domain address with length prefix (same as file transfer)
	domainBytes := append([]byte{byte(len(destAddr))}, []byte(destAddr)...)

	// Build and send STREAM_OPEN with ephemeral public key for E2E encryption
	openPayload := &protocol.StreamOpen{
		RequestID:       pending.RequestID,
		AddressType:     protocol.AddrTypeDomain,
		Address:         domainBytes,
		Port:            0,
		RemainingPath:   remainingPath,
		EphemeralPubKey: ephPub,
	}

	frame := &protocol.Frame{
		Type:     protocol.FrameStreamOpen,
		StreamID: streamID,
		Payload:  openPayload.Encode(),
	}

	if err := a.peerMgr.SendToPeer(nextHop, frame); err != nil {
		crypto.ZeroKey(&ephPriv)
		a.cleanupShellClientStream(nextHop, streamID)
		return nil, fmt.Errorf("send stream open: %w", err)
	}

	// Wait for STREAM_OPEN_ACK
	var result *stream.StreamOpenResult
	select {
	case <-ctx.Done():
		crypto.ZeroKey(&ephPriv)
		a.cleanupShellClientStream(nextHop, streamID)
		return nil, ctx.Err()
	case result = <-pending.ResultCh:
		if result.Error != nil {
			crypto.ZeroKey(&ephPriv)
			a.cleanupShellClientStream(nextHop, streamID)
			return nil, result.Error
		}
		// Stream opened successfully
	}

	// Derive session key from ECDH with remote agent's ephemeral public key
	sharedSecret, err := crypto.ComputeECDH(ephPriv, result.RemoteEphemeral)
	if err != nil {
		crypto.ZeroKey(&ephPriv)
		a.cleanupShellClientStream(nextHop, streamID)
		return nil, fmt.Errorf("compute ECDH: %w", err)
	}

	// Zero out ephemeral private key after computing shared secret
	crypto.ZeroKey(&ephPriv)

	// Derive session key - we are the initiator
	// Use requestID (not streamID) because streamID changes at each relay hop
	sessionKey := crypto.DeriveSessionKey(sharedSecret, pending.RequestID, ephPub, result.RemoteEphemeral, true)
	crypto.ZeroKey(&sharedSecret)

	// Store session key in adapter for E2E encryption
	adapter.SetSessionKey(sessionKey)

	// Encode metadata with shell message format (MsgMeta prefix)
	metaBytes, err := shell.EncodeMeta(meta)
	if err != nil {
		a.cleanupShellClientStream(nextHop, streamID)
		return nil, fmt.Errorf("encode shell metadata: %w", err)
	}

	// Encrypt metadata before sending
	encryptedMeta, err := sessionKey.Encrypt(metaBytes)
	if err != nil {
		a.cleanupShellClientStream(nextHop, streamID)
		return nil, fmt.Errorf("encrypt metadata: %w", err)
	}

	// Send encrypted metadata as first data frame
	dataFrame := &protocol.Frame{
		Type:     protocol.FrameStreamData,
		StreamID: streamID,
		Payload:  encryptedMeta,
	}
	if err := a.peerMgr.SendToPeer(nextHop, dataFrame); err != nil {
		a.cleanupShellClientStream(nextHop, streamID)
		return nil, fmt.Errorf("send metadata: %w", err)
	}

	// Store next hop for sending data
	adapter.SetNextHop(nextHop, a.peerMgr)

	// Start goroutine to forward data from adapter.Send to peer
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		a.forwardShellClientData(streamID, nextHop, adapter)
	}()

	a.logger.Debug("shell client stream opened",
		logging.KeyStreamID, streamID,
		"target", targetID.ShortString(),
		"interactive", interactive)

	return adapter.ToSession(), nil
}

// forwardShellClientData forwards data from the shell client adapter to the peer.
func (a *Agent) forwardShellClientData(streamID uint64, nextHop identity.AgentID, adapter *health.ShellStreamAdapter) {
	sessionKey := adapter.GetSessionKey()
	if sessionKey == nil {
		a.logger.Error("no session key for shell client stream",
			logging.KeyStreamID, streamID)
		adapter.Close()
		return
	}

	for {
		data, ok := adapter.PopSend()
		if !ok {
			return // Adapter closed
		}

		// Encrypt data before sending
		encryptedData, err := sessionKey.Encrypt(data)
		if err != nil {
			a.logger.Error("failed to encrypt shell client data",
				logging.KeyStreamID, streamID,
				logging.KeyError, err)
			adapter.Close()
			return
		}

		frame := &protocol.Frame{
			Type:     protocol.FrameStreamData,
			StreamID: streamID,
			Payload:  encryptedData,
		}
		if err := a.peerMgr.SendToPeer(nextHop, frame); err != nil {
			a.logger.Debug("shell client send error",
				logging.KeyStreamID, streamID,
				logging.KeyError, err)
			adapter.Close()
			return
		}
	}
}

// cleanupShellClientStream tears down a shell client stream.
//
// The matching entry in streamMgr is removed here as well, not just on the
// STREAM_CLOSE frame that may or may not arrive from the remote: if we only
// delete the adapter, any in-flight STREAM_DATA frames for this stream ID
// fall through in handleStreamData to streamMgr.HandleStreamData and pile up
// in the stream's readBuffer (capacity 64). Once the buffer is full, PushData
// drops further frames and the agent resets the stream. Closing the stream
// here makes subsequent HandleStreamData return "unknown stream" (logged and
// dropped) and lets readers observe the teardown immediately.
func (a *Agent) cleanupShellClientStream(peerID identity.AgentID, streamID uint64) {
	a.shellClientMu.Lock()
	delete(a.shellClientStreams, streamKey{peer: peerID, streamID: streamID})
	a.shellClientMu.Unlock()
	a.streamMgr.HandleStreamClose(peerID, streamID)
}

// handleShellClientData handles incoming data for a shell client stream.
func (a *Agent) handleShellClientData(peerID identity.AgentID, streamID uint64, data []byte, flags uint8) bool {
	// Hold lock while getting adapter and session key to avoid race with cleanup
	a.shellClientMu.RLock()
	adapter := a.shellClientStreams[streamKey{peer: peerID, streamID: streamID}]
	if adapter == nil {
		a.shellClientMu.RUnlock()
		a.logger.Debug("handleShellClientData: no adapter", logging.KeyStreamID, streamID)
		return false
	}
	// Get session key while still holding lock
	sessionKey := adapter.GetSessionKey()
	a.shellClientMu.RUnlock()

	if sessionKey == nil {
		a.logger.Error("handleShellClientData: no session key",
			logging.KeyStreamID, streamID)
		adapter.Close()
		return false
	}

	plaintext, err := sessionKey.DecryptStrict(data)
	if err != nil {
		a.logger.Error("handleShellClientData: decryption failed",
			logging.KeyStreamID, streamID,
			logging.KeyError, err)
		adapter.Close()
		return false
	}

	a.logger.Debug("handleShellClientData: pushing data", logging.KeyStreamID, streamID, "bytes", len(plaintext))
	// Push decrypted data to adapter for reading by HTTP handler
	adapter.PushReceive(plaintext)

	// Check for exit code in flags or parse from data
	// Exit code is sent as last frame with FIN flag
	if flags&protocol.FlagFinWrite != 0 {
		adapter.Close()
	}

	return true
}

// handleShellClientClose handles close of a shell client stream.
func (a *Agent) handleShellClientClose(peerID identity.AgentID, streamID uint64) bool {
	// Use write lock and remove from map atomically to avoid race with other handlers
	a.shellClientMu.Lock()
	key := streamKey{peer: peerID, streamID: streamID}
	adapter, exists := a.shellClientStreams[key]
	if exists {
		delete(a.shellClientStreams, key)
	}
	a.shellClientMu.Unlock()

	if adapter == nil {
		a.logger.Debug("handleShellClientClose: no adapter found", logging.KeyStreamID, streamID)
		return false
	}

	a.logger.Debug("handleShellClientClose: closing adapter", logging.KeyStreamID, streamID)
	adapter.CloseFromRemote()
	return true
}

// handleSleepCommand processes an incoming sleep command.
func (a *Agent) handleSleepCommand(peerID identity.AgentID, frame *protocol.Frame) {
	cmd, err := protocol.DecodeSleepCommand(frame.Payload)
	if err != nil {
		a.logger.Debug("failed to decode sleep command",
			logging.KeyPeerID, peerID.ShortString(),
			logging.KeyError, err)
		return
	}

	// Process through flooder for deduplication and forwarding
	if !a.flooder.HandleSleepCommand(peerID, cmd) {
		return
	}

	// Brief delay to allow flood to propagate before disconnecting
	time.Sleep(100 * time.Millisecond)

	// New command - enter sleep mode
	if a.sleepMgr != nil {
		a.logger.Info("entering sleep mode from mesh command",
			"origin", cmd.OriginAgent.ShortString())
		if err := a.sleepMgr.Sleep(); err != nil {
			a.logger.Error("failed to enter sleep mode",
				logging.KeyError, err)
		}
	}
}

// handleWakeCommand processes an incoming wake command.
func (a *Agent) handleWakeCommand(peerID identity.AgentID, frame *protocol.Frame) {
	cmd, err := protocol.DecodeWakeCommand(frame.Payload)
	if err != nil {
		a.logger.Debug("failed to decode wake command",
			logging.KeyPeerID, peerID.ShortString(),
			logging.KeyError, err)
		return
	}

	// Process through flooder for deduplication and forwarding
	if !a.flooder.HandleWakeCommand(peerID, cmd) {
		return
	}

	// New command - wake up
	if a.sleepMgr != nil {
		a.logger.Info("waking from mesh command",
			"origin", cmd.OriginAgent.ShortString())

		// Always call Wake() first to set state to AWAKE
		// This ensures the poll fallback check works even if signal is lost
		if err := a.sleepMgr.Wake(); err != nil {
			// ErrNotSleeping is expected if already awake
			if err != sleep.ErrNotSleeping {
				a.logger.Error("failed to wake from sleep mode",
					logging.KeyError, err)
			}
		}

		// If we're in a poll cycle, also signal it to stop waiting early
		a.wakeSignalMu.Lock()
		inPoll := a.wakeSignal != nil
		a.wakeSignalMu.Unlock()

		if inPoll {
			a.signalWake()
		}
	}
}

// signalWake signals the wake channel if a poll cycle is active.
// Also cancels the poll context to ensure doPoll exits promptly.
func (a *Agent) signalWake() {
	a.wakeSignalMu.Lock()
	defer a.wakeSignalMu.Unlock()

	// Cancel the poll context to force doPoll to exit the select
	// This is critical: Wake() was already called before this,
	// so state is AWAKE. When doPoll exits and checks state, it will
	// see AWAKE and skip the DisconnectAll() call.
	if a.pollCancel != nil {
		a.pollCancel()
	}

	// Also send on wake signal in case doPoll is waiting on it
	if a.wakeSignal != nil {
		select {
		case a.wakeSignal <- struct{}{}:
		default:
			// Channel not ready or already signaled
		}
	}
}

// handleQueuedState processes queued state received from a peer.
func (a *Agent) handleQueuedState(peerID identity.AgentID, frame *protocol.Frame) {
	state, err := protocol.DecodeQueuedState(frame.Payload)
	if err != nil {
		a.logger.Debug("failed to decode queued state",
			logging.KeyPeerID, peerID.ShortString(),
			logging.KeyError, err)
		return
	}

	a.logger.Debug("received queued state",
		logging.KeyPeerID, peerID.ShortString(),
		"routes", len(state.Routes),
		"withdraws", len(state.Withdraws),
		"node_infos", len(state.NodeInfos))

	// Process queued routes
	for _, route := range state.Routes {
		a.flooder.HandleRouteAdvertise(peerID, route.OriginAgent, route.OriginDisplayName, route.Sequence, route.Routes, route.Comments, route.EncPath, route.SeenBy)
	}

	// Process queued withdraws
	for _, withdraw := range state.Withdraws {
		a.flooder.HandleRouteWithdraw(peerID, withdraw.OriginAgent, withdraw.Sequence, withdraw.Routes, withdraw.SeenBy)
	}

	// Process queued node infos
	for _, nodeInfo := range state.NodeInfos {
		a.flooder.HandleNodeInfoAdvertise(peerID, nodeInfo.OriginAgent, nodeInfo.Sequence, nodeInfo.EncInfo, nodeInfo.SeenBy)
	}

	// Check for sleep/wake commands in queued state
	if state.SleepCmd != nil && a.sleepMgr != nil {
		a.logger.Info("entering sleep mode from queued command")
		if err := a.sleepMgr.Sleep(); err != nil {
			a.logger.Error("failed to enter sleep mode from queued command",
				logging.KeyError, err)
		}
	}
	if state.WakeCmd != nil && a.sleepMgr != nil {
		a.logger.Info("waking from queued command")
		if err := a.sleepMgr.Wake(); err != nil {
			a.logger.Error("failed to wake from queued command",
				logging.KeyError, err)
		}
	}
}

// enterSleep is called when transitioning to sleep mode.
func (a *Agent) enterSleep() error {
	a.logger.Info("entering sleep mode - disconnecting all peers")

	// Stop SOCKS5 server temporarily
	if a.socks5Srv != nil {
		a.socks5Srv.Stop()
	}

	// Close all peer connections
	if err := a.peerMgr.DisconnectAll(); err != nil {
		a.logger.Warn("error disconnecting peers",
			logging.KeyError, err)
	}

	// Stop accepting new connections on listeners. The registry (desired set of
	// config + dynamic listeners) is left intact so exitSleep can restore them.
	a.listenersMu.Lock()
	live := a.liveListeners
	a.liveListeners = make(map[string]*listenerHandle)
	a.listenersMu.Unlock()
	for _, h := range live {
		close(h.stopCh)
		h.listener.Close()
	}

	return nil
}

// exitSleep is called when transitioning from sleep to awake.
func (a *Agent) exitSleep() error {
	a.logger.Info("exiting sleep mode - reconnecting")

	// Restart listeners only if none exist (poll listeners may have been promoted)
	a.listenersMu.Lock()
	liveCount := len(a.liveListeners)
	a.listenersMu.Unlock()
	if liveCount == 0 {
		// No existing listeners, start new ones with retry logic. Restart the
		// full desired set (config + dynamic) so dynamic listeners survive a
		// sleep/wake cycle. UDP sockets may not be immediately released after
		// poll listeners close.
		for _, listenerCfg := range a.listeners.All() {
			if err := a.startListenerWithRetry(listenerCfg, 5, 200*time.Millisecond); err != nil {
				a.logger.Error("failed to restart listener",
					logging.KeyAddress, listenerCfg.Address,
					logging.KeyError, err)
			}
		}
	} else {
		a.logger.Debug("listeners already exist (promoted from poll), skipping restart",
			"count", liveCount)
	}

	// Reconnect to all configured peers
	ctx := context.Background()
	if err := a.peerMgr.ReconnectAll(ctx); err != nil {
		a.logger.Warn("error reconnecting peers",
			logging.KeyError, err)
	}

	// Start aggressive reconnection in background to hit sleeping peers' poll windows
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		a.aggressiveReconnect()
	}()

	// Restart SOCKS5 server
	if a.socks5Srv != nil {
		if err := a.socks5Srv.Start(); err != nil {
			a.logger.Error("failed to restart SOCKS5 server",
				logging.KeyError, err)
		}
	}

	// Re-announce routes
	if a.cfg.Exit.Enabled || len(a.cfg.Forward.Endpoints) > 0 {
		a.flooder.AnnounceLocalRoutes()
	}

	return nil
}

// aggressiveReconnect repeatedly attempts to reconnect to all peers
// for multiple poll cycles to ensure we catch sleeping peers' poll windows.
func (a *Agent) aggressiveReconnect() {
	pollInterval := a.cfg.Sleep.PollInterval
	pollDuration := a.cfg.Sleep.PollDuration

	// Try to reconnect frequently for 3 poll cycles
	totalDuration := 3*pollInterval + pollDuration
	retryInterval := pollDuration / 2 // Try twice per poll window
	if retryInterval < 500*time.Millisecond {
		retryInterval = 500 * time.Millisecond
	}

	// Create a parent context that gets canceled when the agent stops
	// This ensures in-flight reconnection operations are interrupted
	parentCtx, parentCancel := context.WithCancel(context.Background())
	defer parentCancel()

	// Monitor stopCh and cancel the parent context
	go func() {
		select {
		case <-a.stopCh:
			parentCancel()
		case <-parentCtx.Done():
		}
	}()

	ticker := time.NewTicker(retryInterval)
	defer ticker.Stop()
	deadline := time.Now().Add(totalDuration)

	a.logger.Debug("starting aggressive reconnection",
		"total_duration", totalDuration,
		"retry_interval", retryInterval)

	for time.Now().Before(deadline) {
		select {
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(parentCtx, retryInterval)
			if err := a.peerMgr.ReconnectAll(ctx); err != nil {
				// Don't log context canceled errors during shutdown
				if !errors.Is(err, context.Canceled) {
					a.logger.Debug("aggressive reconnect attempt",
						logging.KeyError, err)
				}
			}
			cancel()
		case <-a.stopCh:
			return
		}
	}

	a.logger.Debug("aggressive reconnection complete")
}

// doPoll is called during a poll cycle while sleeping.
func (a *Agent) doPoll() error {
	a.logger.Debug("starting poll cycle")

	// Create a context for this poll cycle
	pollCtx, pollCancel := context.WithTimeout(context.Background(), a.cfg.Sleep.PollDuration)
	defer pollCancel()

	// Create wake signal channel for this poll cycle and store cancel func
	a.wakeSignalMu.Lock()
	a.wakeSignal = make(chan struct{})
	a.pollCancel = pollCancel // Store so wake handler can cancel poll early
	a.wakeSignalMu.Unlock()
	defer func() {
		a.wakeSignalMu.Lock()
		a.wakeSignal = nil
		a.pollCancel = nil
		a.wakeSignalMu.Unlock()
	}()

	// Start temporary listeners for the poll duration via the regular
	// startListener path (per-listener accept loop + liveListeners registration)
	// so connections persist if we wake up. Cover the full desired set (config +
	// dynamic). Track the addresses we started so we can tear them down again if
	// still sleeping when the poll ends.
	var pollAddrs []string
	for _, listenerCfg := range a.listeners.All() {
		if err := a.startListener(listenerCfg); err != nil {
			a.logger.Warn("failed to create poll listener",
				logging.KeyAddress, listenerCfg.Address,
				logging.KeyError, err)
			continue
		}
		pollAddrs = append(pollAddrs, listenerCfg.Address)
	}

	// Temporarily reconnect to peers
	if err := a.peerMgr.ReconnectAll(pollCtx); err != nil {
		a.logger.Warn("error reconnecting for poll",
			logging.KeyError, err)
	}

	// Wait for poll duration OR wake signal
	select {
	case <-pollCtx.Done():
		// Poll timeout - check state below
	case <-a.wakeSignal:
		// Explicitly woken during poll - listeners are already registered
		// Just call Wake() to trigger exitSleep() (which won't restart listeners)
		a.logger.Debug("poll cycle received wake signal - keeping listeners")
		if a.sleepMgr != nil {
			if err := a.sleepMgr.Wake(); err != nil {
				// ErrNotSleeping is expected if state already changed
				if err != sleep.ErrNotSleeping {
					a.logger.Debug("wake during poll", logging.KeyError, err)
				}
			}
		}
		return nil
	case <-a.stopCh:
		// Clean up listeners before returning
		a.closePollListeners(pollAddrs)
		return nil
	}

	// Check if we were woken during the poll - if so, don't disconnect
	// This is a fallback in case state changed but signal wasn't received
	if a.sleepMgr != nil && a.sleepMgr.GetState() == sleep.StateAwake {
		a.logger.Debug("poll cycle ended but agent is awake - keeping listeners")
		// Listeners are already registered, nothing more to do
		return nil
	}

	// Disconnect again (still sleeping)
	if err := a.peerMgr.DisconnectAll(); err != nil {
		a.logger.Warn("error disconnecting after poll",
			logging.KeyError, err)
	}

	// Close poll listeners and remove from agent listeners
	a.closePollListeners(pollAddrs)

	a.logger.Debug("poll cycle complete")
	return nil
}

// closePollListeners stops the listeners started for a poll cycle (by address)
// when the poll ends and the agent is still sleeping.
func (a *Agent) closePollListeners(pollAddrs []string) {
	for _, addr := range pollAddrs {
		a.stopLiveListener(addr)
	}
}

// TriggerSleep initiates sleep mode for this agent and floods the command to the mesh.
func (a *Agent) TriggerSleep() error {
	if a.sleepMgr == nil {
		return fmt.Errorf("sleep mode not enabled")
	}

	// Create sleep command
	cmd := &protocol.SleepCommand{
		OriginAgent: a.id,
		CommandID:   uint64(time.Now().UnixNano()),
		Timestamp:   uint64(time.Now().Unix()),
		SeenBy:      []identity.AgentID{a.id},
	}

	// Sign command if we have a signing key
	if a.cfg.CanSign() {
		privKey, err := a.cfg.GetSigningPrivateKey()
		if err != nil {
			return fmt.Errorf("get signing key: %w", err)
		}
		cmd.Signature = crypto.Sign(privKey, cmd.SignableBytes())
		crypto.ZeroSigningKey(&privKey)
		a.logger.Debug("signed sleep command",
			"command_id", cmd.CommandID)
	}

	// Flood command to mesh first (while we have connections)
	if err := a.flooder.FloodSleepCommand(cmd); err != nil {
		a.logger.Warn("failed to flood sleep command",
			logging.KeyError, err)
	}

	// Brief delay to allow flood to propagate before disconnecting
	// This ensures peers receive the sleep command before we close connections
	time.Sleep(100 * time.Millisecond)

	// Enter sleep mode locally
	return a.sleepMgr.Sleep()
}

// TriggerWake initiates wake mode for this agent and floods the command to the mesh.
func (a *Agent) TriggerWake() error {
	if a.sleepMgr == nil {
		return fmt.Errorf("sleep mode not enabled")
	}

	// Wake locally first (to establish connections for flooding)
	if err := a.sleepMgr.Wake(); err != nil {
		return err
	}

	// Create wake command once (same command ID for deduplication)
	cmd := &protocol.WakeCommand{
		OriginAgent: a.id,
		CommandID:   uint64(time.Now().UnixNano()),
		Timestamp:   uint64(time.Now().Unix()),
		SeenBy:      []identity.AgentID{a.id},
	}

	// Sign command if we have a signing key
	if a.cfg.CanSign() {
		privKey, err := a.cfg.GetSigningPrivateKey()
		if err != nil {
			return fmt.Errorf("get signing key: %w", err)
		}
		cmd.Signature = crypto.Sign(privKey, cmd.SignableBytes())
		crypto.ZeroSigningKey(&privKey)
		a.logger.Debug("signed wake command",
			"command_id", cmd.CommandID)
	}

	// Flood multiple times during the wait period to catch peers at different poll windows.
	// Total wait time should span at least 2 poll cycles to ensure all peers get the message.
	totalWait := 2*a.cfg.Sleep.PollInterval + a.cfg.Sleep.PollDuration
	if totalWait < 5*time.Second {
		totalWait = 5 * time.Second
	}

	// Flood at intervals during the wait period
	floodInterval := a.cfg.Sleep.PollDuration
	if floodInterval < 500*time.Millisecond {
		floodInterval = 500 * time.Millisecond
	}

	ticker := time.NewTicker(floodInterval)
	defer ticker.Stop()
	deadline := time.Now().Add(totalWait)

	// Initial flood
	a.flooder.FloodWakeCommand(cmd)

	for time.Now().Before(deadline) {
		select {
		case <-ticker.C:
			a.flooder.FloodWakeCommand(cmd)
		case <-a.stopCh:
			return nil
		}
	}

	// Final flood
	return a.flooder.FloodWakeCommand(cmd)
}

// GetSleepState returns the current sleep state.
func (a *Agent) GetSleepState() sleep.State {
	if a.sleepMgr == nil {
		return sleep.StateAwake
	}
	return a.sleepMgr.GetState()
}

// GetSleepStatus returns detailed sleep status information.
// Implements health.SleepProvider interface.
func (a *Agent) GetSleepStatus() health.SleepStatusInfo {
	pollSec := a.cfg.Sleep.PollInterval.Seconds()
	jitter := a.cfg.Sleep.PollIntervalJitter
	if a.sleepMgr == nil {
		return health.SleepStatusInfo{
			State:              "AWAKE",
			Enabled:            false,
			PollIntervalSec:    pollSec,
			PollIntervalJitter: jitter,
		}
	}
	status := a.sleepMgr.GetStatus()
	return health.SleepStatusInfo{
		State:              status.State.String(),
		Enabled:            status.Enabled,
		SleepStartTime:     status.SleepStartTime,
		LastPollTime:       status.LastPollTime,
		NextPollTime:       status.NextPollTime,
		QueuedPeers:        status.QueuedPeers,
		PollIntervalSec:    pollSec,
		PollIntervalJitter: jitter,
	}
}

// IsSleepEnabled returns true if sleep mode is enabled.
// Implements health.SleepProvider interface.
func (a *Agent) IsSleepEnabled() bool {
	return a.cfg.Sleep.Enabled
}

// isNonLoopbackBind reports whether addr binds to a network-reachable
// interface (rather than 127.0.0.1 / ::1 / a loopback hostname). The empty
// host (e.g. ":8080" => bind all interfaces) and 0.0.0.0 / :: count as
// non-loopback. Used to decide whether to print the unauth'd-HTTP warning.
func isNonLoopbackBind(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		// Fall back to substring check; if we can't even parse it, err on
		// the side of warning.
		return !strings.Contains(addr, "127.0.0.1") && !strings.Contains(addr, "::1")
	}
	if host == "" {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsLoopback() {
			return false
		}
		// 0.0.0.0 / :: -- IsUnspecified() is true; clearly non-loopback.
		return true
	}
	// Hostname: treat anything literally "localhost" as loopback.
	return !strings.EqualFold(host, "localhost")
}
