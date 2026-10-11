// Package health provides health check HTTP endpoints for Muti Metroo.
package health

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/postalsys/muti-metroo/internal/crypto"
	"github.com/postalsys/muti-metroo/internal/filetransfer"
	"github.com/postalsys/muti-metroo/internal/hosts"
	"github.com/postalsys/muti-metroo/internal/identity"
	"github.com/postalsys/muti-metroo/internal/probe"
	"github.com/postalsys/muti-metroo/internal/protocol"
	"golang.org/x/crypto/bcrypt"
)

// splashPageTemplate is the HTML for the root splash page.
const splashPageTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Muti Metroo</title>
    <style>
        * { margin: 0; padding: 0; box-sizing: border-box; }
        body {
            background: radial-gradient(ellipse at 50% 60%, #d5cbbe 0%, #c4b8a6 25%, #b0a48f 50%, #9a8e7a 75%, #857968 100%);
            min-height: 100vh;
            display: flex;
            align-items: center;
            justify-content: center;
        }
        img { width: 160px; height: 160px; }
    </style>
</head>
<body>
    <img src="/logo.png" alt="Muti Metroo">
</body>
</html>
`

// StatsProvider provides agent statistics.
type StatsProvider interface {
	// IsRunning returns true if the agent is running.
	IsRunning() bool

	// Stats returns agent statistics.
	Stats() Stats
}

// FileTransferProgress is a callback for reporting file transfer progress.
type FileTransferProgress func(bytesTransferred, totalBytes int64)

// SOCKS5Info contains SOCKS5 proxy configuration for display.
type SOCKS5Info struct {
	Enabled bool
	Address string
}

// UDPInfo contains UDP relay configuration for display.
type UDPInfo struct {
	Enabled bool
}

// PortForwardInfo contains port forward configuration for display.
type PortForwardInfo struct {
	// ListenerKeys contains routing keys for port forward listeners (ingress).
	ListenerKeys []string
	// ListenerAddresses contains listen addresses for each listener.
	ListenerAddresses []string
	// EndpointKeys contains routing keys for port forward endpoints (exit).
	EndpointKeys []string
}

// RemoteStatusProvider provides the ability to fetch status from remote agents.
type RemoteStatusProvider interface {
	// ID returns the local agent's ID.
	ID() identity.AgentID

	// DisplayName returns the local agent's display name, or falls back to short ID.
	DisplayName() string

	// SendControlRequest sends a control request to a remote agent.
	SendControlRequest(ctx context.Context, targetID identity.AgentID, controlType uint8) (*protocol.ControlResponse, error)

	// SendControlRequestWithData sends a control request with data payload to a remote agent.
	SendControlRequestWithData(ctx context.Context, targetID identity.AgentID, controlType uint8, data []byte) (*protocol.ControlResponse, error)

	// GetPeerIDs returns a list of all connected peer IDs.
	GetPeerIDs() []identity.AgentID

	// GetKnownAgentIDs returns a list of all known agent IDs (including remote via routes).
	GetKnownAgentIDs() []identity.AgentID

	// GetPeerDetails returns detailed information about connected peers for the dashboard.
	GetPeerDetails() []PeerDetails

	// GetRouteDetails returns detailed route information for the dashboard.
	GetRouteDetails() []RouteDetails

	// GetDomainRouteDetails returns detailed domain route information for the dashboard.
	GetDomainRouteDetails() []DomainRouteDetails

	// GetAllDisplayNames returns display names for all known agents (from route advertisements).
	GetAllDisplayNames() map[identity.AgentID]string

	// GetAllNodeInfo returns node info for all known agents.
	GetAllNodeInfo() map[identity.AgentID]*protocol.NodeInfo

	// GetLocalNodeInfo returns local node info.
	GetLocalNodeInfo() *protocol.NodeInfo

	// GetSOCKS5Info returns SOCKS5 configuration for the local agent.
	GetSOCKS5Info() SOCKS5Info

	// GetUDPInfo returns UDP relay configuration for the local agent.
	GetUDPInfo() UDPInfo

	// GetPortForwardInfo returns port forward configuration for the local agent.
	GetPortForwardInfo() PortForwardInfo

	// GetPortForwardRouteDetails returns detailed port forward route information for the dashboard.
	GetPortForwardRouteDetails() []PortForwardRouteDetails

	// UploadFile uploads a file or directory to a remote agent via stream-based transfer.
	// localPath is the local file/directory path, remotePath is the destination on the remote agent.
	UploadFile(ctx context.Context, targetID identity.AgentID, localPath, remotePath string, opts TransferOptions, progress FileTransferProgress) error

	// DownloadFile downloads a file or directory from a remote agent via stream-based transfer.
	// remotePath is the path on the remote agent, localPath is the local destination.
	DownloadFile(ctx context.Context, targetID identity.AgentID, remotePath, localPath string, opts TransferOptions, progress FileTransferProgress) error

	// DownloadFileStream opens a streaming download from a remote agent.
	// Returns a reader that streams file data directly without writing to disk.
	// The caller must call Close() on the result when done.
	DownloadFileStream(ctx context.Context, targetID identity.AgentID, remotePath string, opts TransferOptions) (*DownloadStreamResult, error)

	// ShutdownBoundCtx derives a context from parent that also cancels when
	// the agent is asked to stop. Used by long-running handlers (file
	// transfers, in particular) to abort cleanly during shutdown instead of
	// keeping the HTTP request goroutine pinned past Stop().
	ShutdownBoundCtx(parent context.Context) (context.Context, context.CancelFunc)
}

// DownloadStreamResult contains the result of a streaming download request.
type DownloadStreamResult struct {
	Reader       io.ReadCloser
	Size         int64  // Size of the (possibly compressed) data stream
	OriginalSize int64  // Original uncompressed file size
	Mode         uint32 // File permission mode
	IsDirectory  bool   // True if downloading a directory (tar.gz)
	Compressed   bool   // True if data is gzip compressed
	Close        func() // Cleanup function to call when done
}

// TransferOptions contains options for file upload/download operations.
// Authentication is not represented here -- the implementing agent stamps
// the outgoing transfer metadata with a management-key signature.
type TransferOptions struct {
	RateLimit    int64 // Max bytes per second (0 = unlimited)
	Offset       int64 // Resume from this byte offset (for downloads)
	OriginalSize int64 // Expected file size for resume validation
}

// PeerDetails contains detailed information about a connected peer.
type PeerDetails struct {
	ID          identity.AgentID
	DisplayName string
	State       string
	RTT         time.Duration
	IsDialer    bool
	Transport   string // Transport type: "quic", "h2", "ws"
}

// RouteDetails contains detailed route information.
type RouteDetails struct {
	Network   string
	NextHop   identity.AgentID
	Origin    identity.AgentID
	Metric    int
	HopCount  int
	Path      []identity.AgentID // Full path from local to origin
	Comment   string             // Operator comment for routes the local agent originates; empty for routes learned from peers
	SourceIP  string             // Local source IP bound on the exit for this CIDR; only set when the local agent originates the route (remote source bindings are never advertised)
	Interface string             // Egress network interface (SO_BINDTODEVICE) for this CIDR; only set when the local agent originates the route (remote interface bindings are never advertised). Linux + root only.
}

// DomainRouteDetails contains detailed domain route information for the dashboard.
type DomainRouteDetails struct {
	Pattern    string
	IsWildcard bool
	NextHop    identity.AgentID
	Origin     identity.AgentID
	Metric     int
	HopCount   int
	Path       []identity.AgentID // Full path from local to origin
}

// PortForwardRouteDetails contains detailed port forward route information for the dashboard.
type PortForwardRouteDetails struct {
	Key             string
	ListenerAddress string // Listener address if local agent has a listener for this key
	Target          string // Target address (propagated in route advertisements)
	NextHop         identity.AgentID
	Origin          identity.AgentID
	Metric          int
	HopCount        int
	Path            []identity.AgentID // Full path from local to origin
	IsLocal         bool               // True if this is a local port forward endpoint
}

// RouteAdvertiseTrigger provides the ability to trigger immediate route advertisement.
type RouteAdvertiseTrigger interface {
	// TriggerRouteAdvertise triggers an immediate route advertisement.
	TriggerRouteAdvertise()
}

// SleepStatusInfo contains sleep status information.
type SleepStatusInfo struct {
	State          string    `json:"state"`
	Enabled        bool      `json:"enabled"`
	SleepStartTime time.Time `json:"sleep_start_time,omitempty"`
	LastPollTime   time.Time `json:"last_poll_time,omitempty"`
	NextPollTime   time.Time `json:"next_poll_time,omitempty"`
	QueuedPeers    int       `json:"queued_peers"`

	// Cadence info -- the configured poll_interval and jitter for this
	// agent. Surfaced in the API so the CLI can report a realistic
	// "reattachment expected within ~Xm" hint after `muti-metroo wake`.
	PollIntervalSec    float64 `json:"poll_interval_sec,omitempty"`
	PollIntervalJitter float64 `json:"poll_interval_jitter,omitempty"`
}

// SleepProvider provides sleep mode functionality.
type SleepProvider interface {
	// TriggerSleep puts the mesh into sleep mode.
	TriggerSleep() error
	// TriggerWake wakes the mesh from sleep mode.
	TriggerWake() error
	// GetSleepStatus returns the current sleep status.
	GetSleepStatus() SleepStatusInfo
	// IsSleepEnabled returns true if sleep mode is enabled.
	IsSleepEnabled() bool
}

// StopProvider asks the agent to begin a graceful shutdown asynchronously.
// The call must not block -- the HTTP handler invokes it after the response
// is written so the listener stays up long enough to flush the body.
type StopProvider interface {
	RequestShutdown()
}

// RouteManageResult contains the response for a route management operation.
type RouteManageResult struct {
	Status  string                   `json:"status"`
	Message string                   `json:"message,omitempty"`
	Routes  []RouteManageResultEntry `json:"routes,omitempty"`
}

// Scope values accepted by the list action on /routes/manage.
const (
	ScopeDynamic = "dynamic" // dynamic routes only (default)
	ScopeAll     = "all"     // config-seeded + dynamic routes
)

// RouteManageResultEntry describes a single local route in list output.
// Dynamic is true for runtime-added routes, false for config-seeded ones.
// SourceIP and Interface, when non-empty, are the local IP and egress NIC
// this exit binds outbound dials to for the route (local-only; never
// propagated to peers). Interface is Linux + root only.
type RouteManageResultEntry struct {
	Network   string `json:"network"`
	Metric    uint16 `json:"metric"`
	Dynamic   bool   `json:"dynamic"`
	Comment   string `json:"comment,omitempty"`
	SourceIP  string `json:"source_ip,omitempty"`
	Interface string `json:"interface,omitempty"`
}

// RouteManageProvider provides dynamic route management.
type RouteManageProvider interface {
	// ManageRoute handles add/remove/list/set_comment/set_source_ip/
	// set_interface operations on CIDR routes. set_comment edits the
	// operator-visible comment; comment is local metadata and is not
	// advertised to peers. sourceIP (on add or set_source_ip) binds
	// outbound dials matching this CIDR to the given local IP at the exit;
	// iface (on add or set_interface) pins the egress NIC via
	// SO_BINDTODEVICE (Linux + root). Both are local-only. scope controls
	// list output: "" or "dynamic" returns dynamic routes only (default;
	// preserves the `muti-metroo route list` contract); "all" returns
	// config+dynamic.
	ManageRoute(action, network string, metric uint16, comment, sourceIP, iface, scope string) (*RouteManageResult, error)
}

// MeshHostManageResult is the response body for /hosts/manage. The full
// merged list of config + dynamic hosts is returned for every action so
// the caller does not need a follow-up list request. Suffixes lets
// clients validate names locally before submitting.
type MeshHostManageResult struct {
	Suffixes []string     `json:"suffixes"`
	Hosts    []hosts.Host `json:"hosts"`
}

// MeshHostSuffixManageResult is the response body for
// /hosts/suffixes/manage. Returns the post-mutation active suffix set
// for every action (add, remove, list) so callers do not need a
// follow-up list request.
type MeshHostSuffixManageResult struct {
	Suffixes []string `json:"suffixes"`
}

// MeshHostProvider exposes the mesh-host registry to the HTTP layer:
// management mutations and dashboard reads.
type MeshHostProvider interface {
	// ManageMeshHost handles add/remove/list. On error, returns one of
	// hosts.ErrInvalid / ErrExists / ErrNotFound / ErrProtected wrapped
	// with details, which the handler maps to HTTP status codes.
	ManageMeshHost(action, name, ipv4, ipv6, comment string) (*MeshHostManageResult, error)
	// ManageMeshHostSuffix handles add/remove/list on the active TLD set.
	// On error, returns hosts.ErrInvalid / ErrExists / ErrNotFound /
	// ErrSuffixInUse, which the handler maps to HTTP status codes.
	ManageMeshHostSuffix(action, suffix string) (*MeshHostSuffixManageResult, error)
	// MeshHosts returns the merged list of config + dynamic hosts for
	// /api/dashboard exposure.
	MeshHosts() []hosts.Host
	// MeshHostsSuffixes returns the currently active TLD set (without
	// leading dots) so dashboard consumers can validate names
	// client-side.
	MeshHostsSuffixes() []string
}

// ForwardManageResult contains the response for a forward listener management operation.
type ForwardManageResult struct {
	Status    string                     `json:"status"`
	Message   string                     `json:"message,omitempty"`
	Listeners []ForwardManageResultEntry `json:"listeners,omitempty"`
}

// ForwardManageResultEntry describes a single forward listener in list output.
type ForwardManageResultEntry struct {
	Key            string `json:"key"`
	Address        string `json:"address"`
	MaxConnections int    `json:"max_connections"`
	Dynamic        bool   `json:"dynamic"`
}

// ForwardManageProvider provides dynamic forward listener management.
type ForwardManageProvider interface {
	// ManageForwardListener handles add/remove/list operations on dynamic forward listeners.
	ManageForwardListener(action, key, address string, maxConnections int) (*ForwardManageResult, error)
}

// ForwardEndpointManageResult contains the response for a forward endpoint management operation.
type ForwardEndpointManageResult struct {
	Status    string                             `json:"status"`
	Message   string                             `json:"message,omitempty"`
	Endpoints []ForwardEndpointManageResultEntry `json:"endpoints,omitempty"`
}

// ForwardEndpointManageResultEntry describes a single forward endpoint in list output.
type ForwardEndpointManageResultEntry struct {
	Key     string `json:"key"`
	Target  string `json:"target"`
	Dynamic bool   `json:"dynamic"`
}

// ForwardEndpointManageProvider provides dynamic forward endpoint management.
type ForwardEndpointManageProvider interface {
	// ManageForwardEndpoint handles add/remove/list operations on dynamic forward endpoints.
	ManageForwardEndpoint(action, key, target string) (*ForwardEndpointManageResult, error)
}

// PeerManageResult contains the response for a peer management operation.
type PeerManageResult struct {
	Status  string                  `json:"status"`
	Message string                  `json:"message,omitempty"`
	Peers   []PeerManageResultEntry `json:"peers,omitempty"`
}

// PeerDirectionInbound marks a PeerManageResultEntry as an accepted (inbound)
// peer connection. Outbound entries omit the field.
const PeerDirectionInbound = "inbound"

// PeerManageResultEntry describes a single peer in list output.
type PeerManageResultEntry struct {
	Address    string `json:"address"`
	Transport  string `json:"transport"`
	ExpectedID string `json:"expected_id,omitempty"`
	RemoteID   string `json:"remote_id,omitempty"`
	Connected  bool   `json:"connected"`
	Persistent bool   `json:"persistent"`
	Dynamic    bool   `json:"dynamic"`
	// Direction is PeerDirectionInbound for accepted peer connections. Outbound
	// entries omit this field for backwards compatibility with existing clients.
	Direction string `json:"direction,omitempty"`
}

// PeerManageProvider provides dynamic peer connection management.
type PeerManageProvider interface {
	// ManagePeer handles add/remove/list operations on dynamic peer connections.
	// tcpKeepAlive is an optional SO_KEEPALIVE duration string for "add" (empty
	// inherits the global default); ignored for other actions.
	ManagePeer(action, address, transport, expectedID, tcpKeepAlive string) (*PeerManageResult, error)
}

// ListenerManageRequest is the request body for transport listener management.
type ListenerManageRequest struct {
	Action             string `json:"action"`
	Transport          string `json:"transport,omitempty"`
	Address            string `json:"address,omitempty"`
	Path               string `json:"path,omitempty"`
	PlainText          bool   `json:"plaintext,omitempty"`
	AdvertiseAddress   string `json:"advertise_address,omitempty"`
	AdvertisePath      string `json:"advertise_path,omitempty"`
	AdvertiseTransport string `json:"advertise_transport,omitempty"`
}

// ListenerManageResult contains the response for a transport listener management operation.
type ListenerManageResult struct {
	Status    string                      `json:"status"`
	Message   string                      `json:"message,omitempty"`
	Listeners []ListenerManageResultEntry `json:"listeners,omitempty"`
}

// ListenerManageResultEntry describes a single transport listener in list output.
type ListenerManageResultEntry struct {
	Transport        string `json:"transport"`
	Address          string `json:"address"`
	Path             string `json:"path,omitempty"`
	PlainText        bool   `json:"plaintext"`
	AdvertiseAddress string `json:"advertise_address,omitempty"`
	Dynamic          bool   `json:"dynamic"`
}

// ListenerManageProvider provides dynamic transport listener management.
type ListenerManageProvider interface {
	// ManageListener handles add/remove/list operations on dynamic transport listeners.
	ManageListener(req *ListenerManageRequest) (*ListenerManageResult, error)
}

// FileBrowseProvider provides file browsing (directory listing, stat, roots).
type FileBrowseProvider interface {
	BrowseFiles(req *filetransfer.BrowseRequest) *filetransfer.BrowseResponse
}

// DisplayNameManageResult contains the response for a display name management operation.
type DisplayNameManageResult struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
	Name    string `json:"name,omitempty"`
}

// DisplayNameManageProvider provides dynamic display name management.
type DisplayNameManageProvider interface {
	// ManageDisplayName handles set/get operations on the agent display name.
	ManageDisplayName(action, name string) (*DisplayNameManageResult, error)
}

// Stats contains agent health statistics.
type Stats struct {
	PeerCount      int  `json:"peer_count"`
	StreamCount    int  `json:"stream_count"`
	RouteCount     int  `json:"route_count"`
	SOCKS5Running  bool `json:"socks5_running"`
	ExitHandlerRun bool `json:"exit_handler_running"`

	// CanSignManagement is true when this agent holds the management
	// signing private key and can therefore initiate privileged
	// operations (shell, file transfer, dynamic config mutations,
	// sleep/wake). Web clients use this to enable/disable management UI.
	CanSignManagement bool `json:"can_sign_management"`
	// AcceptsSignedManagement is true when this agent holds the
	// management signing public key and will verify and execute signed
	// privileged operations from other agents. When false, inbound
	// shell / file-transfer / mutation requests are rejected outright.
	AcceptsSignedManagement bool `json:"accepts_signed_management"`
}

// TopologyAgentInfo contains information about an agent for the topology API.
type TopologyAgentInfo struct {
	ID                  string   `json:"id"`
	ShortID             string   `json:"short_id"`
	DisplayName         string   `json:"display_name"`
	IsLocal             bool     `json:"is_local"`
	IsConnected         bool     `json:"is_connected"`
	Hostname            string   `json:"hostname,omitempty"`
	OS                  string   `json:"os,omitempty"`
	Arch                string   `json:"arch,omitempty"`
	Version             string   `json:"version,omitempty"`
	PID                 int      `json:"pid,omitempty"`
	UptimeHours         float64  `json:"uptime_hours,omitempty"`
	IPAddresses         []string `json:"ip_addresses,omitempty"`
	Roles               []string `json:"roles,omitempty"`                 // Agent roles: "ingress", "exit", "transit", "forward_ingress", "forward_exit"
	SOCKS5Addr          string   `json:"socks5_addr,omitempty"`           // SOCKS5 listen address (for ingress)
	ExitRoutes          []string `json:"exit_routes,omitempty"`           // CIDR routes (for exit)
	DomainRoutes        []string `json:"domain_routes,omitempty"`         // Domain patterns (for exit)
	UDPEnabled          bool     `json:"udp_enabled,omitempty"`           // UDP relay enabled (for exit)
	ForwardListeners    []string `json:"forward_listeners,omitempty"`     // Port forward listener keys (for forward_ingress)
	ForwardEndpoints    []string `json:"forward_endpoints,omitempty"`     // Port forward endpoint keys (for forward_exit)
	Shells              []string `json:"shells,omitempty"`                // Available shells (e.g., ["bash", "sh"])
	ShellEnabled        bool     `json:"shell_enabled,omitempty"`         // Shell access enabled
	FileTransferEnabled bool     `json:"file_transfer_enabled,omitempty"` // File transfer enabled
	IcmpEnabled         bool     `json:"icmp_enabled,omitempty"`          // ICMP echo (ping) enabled

	AdvertisedListeners []protocol.AdvertisedListener `json:"advertised_listeners,omitempty"` // Public dial targets advertised by this agent
}

// TopologyConnection represents a connection between two agents.
type TopologyConnection struct {
	FromAgent    string `json:"from_agent"`
	ToAgent      string `json:"to_agent"`
	IsDirect     bool   `json:"is_direct"`
	RTTMs        int64  `json:"rtt_ms,omitempty"`
	Unresponsive bool   `json:"unresponsive,omitempty"` // RTT > 60s indicates connection is stuck
	Transport    string `json:"transport,omitempty"`    // Transport type for direct connections: "quic", "h2", "ws"
	Dialer       string `json:"dialer,omitempty"`       // ShortID of the dialing agent; empty if unknown
}

// TopologyResponse is the response for the /api/topology endpoint.
type TopologyResponse struct {
	LocalAgent  TopologyAgentInfo    `json:"local_agent"`
	Agents      []TopologyAgentInfo  `json:"agents"`
	Connections []TopologyConnection `json:"connections"`
}

// DashboardPeerInfo contains information about a connected peer.
type DashboardPeerInfo struct {
	ID           string `json:"id"`
	ShortID      string `json:"short_id"`
	DisplayName  string `json:"display_name"`
	State        string `json:"state"`
	RTTMs        int64  `json:"rtt_ms"`
	Unresponsive bool   `json:"unresponsive,omitempty"` // RTT > 60s indicates connection is stuck
	IsDialer     bool   `json:"is_dialer"`
}

// DashboardRouteInfo contains information about a route.
type DashboardRouteInfo struct {
	Network     string   `json:"network"`
	RouteType   string   `json:"route_type"` // "cidr" or "domain"
	Origin      string   `json:"origin"`     // Display name of origin
	OriginID    string   `json:"origin_id"`  // Short ID of origin
	HopCount    int      `json:"hop_count"`
	PathDisplay []string `json:"path_display"`        // Display names: [local, peer1, ..., origin]
	PathIDs     []string `json:"path_ids"`            // Short IDs for path highlighting
	TCP         bool     `json:"tcp"`                 // TCP support (always true)
	UDP         bool     `json:"udp"`                 // UDP support (exit has UDP enabled)
	Comment     string   `json:"comment,omitempty"`   // Operator comment, only set for routes the local agent originates
	SourceIP    string   `json:"source_ip,omitempty"` // Local source IP the exit binds to, only set for routes the local agent originates (paired semantics with Comment)
	Interface   string   `json:"interface,omitempty"` // Egress NIC the exit binds via SO_BINDTODEVICE, only set for routes the local agent originates (Linux + root)
}

// DashboardDomainRouteInfo contains information about a domain route.
type DashboardDomainRouteInfo struct {
	Pattern     string   `json:"pattern"` // Domain pattern (e.g., "*.example.com")
	IsWildcard  bool     `json:"is_wildcard"`
	Origin      string   `json:"origin"`    // Display name of origin
	OriginID    string   `json:"origin_id"` // Short ID of origin
	HopCount    int      `json:"hop_count"`
	PathDisplay []string `json:"path_display"` // Display names: [local, peer1, ..., origin]
	PathIDs     []string `json:"path_ids"`     // Short IDs for path highlighting
	TCP         bool     `json:"tcp"`          // TCP support (always true)
	UDP         bool     `json:"udp"`          // UDP support (exit has UDP enabled)
}

// DashboardPortForwardRouteInfo contains information about a port forward route.
// Shows ingress-exit pairs for port forwarding routes.
type DashboardPortForwardRouteInfo struct {
	Key             string   `json:"key"`                        // Port forward routing key
	IngressAgent    string   `json:"ingress_agent"`              // Display name of ingress agent
	IngressAgentID  string   `json:"ingress_agent_id"`           // Short ID of ingress agent
	ListenerAddress string   `json:"listener_address,omitempty"` // Ingress listen address
	ExitAgent       string   `json:"exit_agent"`                 // Display name of exit agent
	ExitAgentID     string   `json:"exit_agent_id"`              // Short ID of exit agent
	Target          string   `json:"target,omitempty"`           // Exit target address
	HopCount        int      `json:"hop_count"`                  // Number of hops from ingress to exit
	PathDisplay     []string `json:"path_display"`               // Path from ingress to exit: [ingress, ..., exit]
	PathIDs         []string `json:"path_ids"`                   // Short IDs for path highlighting
}

// DashboardResponse is the response for the /api/dashboard endpoint.
type DashboardResponse struct {
	Agent             TopologyAgentInfo               `json:"agent"`
	Stats             Stats                           `json:"stats"`
	Peers             []DashboardPeerInfo             `json:"peers"`
	Routes            []DashboardRouteInfo            `json:"routes"`
	DomainRoutes      []DashboardDomainRouteInfo      `json:"domain_routes,omitempty"`
	ForwardRoutes     []DashboardPortForwardRouteInfo `json:"forward_routes,omitempty"`
	MeshHosts         []hosts.Host                    `json:"mesh_hosts,omitempty"`
	MeshHostsSuffixes []string                        `json:"mesh_hosts_suffixes,omitempty"`
}

// ServerConfig contains health server configuration.
type ServerConfig struct {
	// Address to listen on (e.g., ":8080")
	Address string

	// ReadTimeout for HTTP reads
	ReadTimeout time.Duration

	// WriteTimeout for HTTP writes
	WriteTimeout time.Duration

	// TokenHash is the bcrypt hash of the API bearer token.
	// When non-empty, non-exempt endpoints require authentication.
	TokenHash string

	// Endpoint group toggles. Disabled endpoints return 404 with logging.
	// /health, /healthz, /ready are always enabled.

	// EnablePprof enables the /debug/pprof/* endpoints
	EnablePprof bool

	// EnableDashboard enables the /api/* dashboard endpoints
	EnableDashboard bool

	// EnableRemoteAPI enables the /agents/*, /routes/advertise endpoints
	EnableRemoteAPI bool
}

// DefaultServerConfig returns sensible defaults.
func DefaultServerConfig() ServerConfig {
	return ServerConfig{
		Address:         ":8080",
		ReadTimeout:     10 * time.Second,
		WriteTimeout:    10 * time.Second,
		EnablePprof:     true,
		EnableDashboard: true,
		EnableRemoteAPI: true,
	}
}

// Server is an HTTP server for health check endpoints.
type Server struct {
	cfg                           ServerConfig
	provider                      StatsProvider
	remoteProvider                RemoteStatusProvider
	routeTrigger                  RouteAdvertiseTrigger
	shellProvider                 ShellProvider                 // For shell WebSocket sessions
	icmpProvider                  ICMPProvider                  // For ICMP WebSocket sessions
	sleepProvider                 SleepProvider                 // For sleep mode endpoints
	stopProvider                  StopProvider                  // For /shutdown graceful exit
	routeManageProvider           RouteManageProvider           // For dynamic route management
	routeTraceProvider            RouteTraceProvider            // For /route/trace diagnostic
	forwardManageProvider         ForwardManageProvider         // For dynamic forward listener management
	forwardEndpointManageProvider ForwardEndpointManageProvider // For dynamic forward endpoint management
	peerManageProvider            PeerManageProvider            // For dynamic peer connection management
	listenerManageProvider        ListenerManageProvider        // For dynamic transport listener management
	fileBrowseProvider            FileBrowseProvider            // For file browsing (list, stat, roots)
	displayNameManageProvider     DisplayNameManageProvider     // For dynamic display name management
	meshHostProvider              MeshHostProvider              // For mesh-host registry (manage + dashboard)
	mgmtAuthProvider              MgmtAuthProvider              // Management-key signature helpers for mutation endpoints
	setupDefaultsProvider         SetupDefaultsProvider         // For /api/setup-defaults
	sealedBox                     *crypto.SealedBox             // For checking decrypt capability
	meshTestState                 *MeshTestState                // For mesh test caching
	probeFunc                     ProbeFunc                     // Injectable for tests; defaults to probe.Probe
	server                        *http.Server
	listener                      net.Listener
	running                       atomic.Bool

	// Bearer token authentication cache
	tokenCacheMu    sync.RWMutex
	cachedTokenSHA  [32]byte // SHA-256 of last validated token
	tokenCacheValid bool
}

// disabledHandler returns a handler that returns 404 for disabled endpoints.
func disabledHandler(_ string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		io.WriteString(w, notFoundPage)
	}
}

// authExemptPaths are paths that do not require authentication.
// Health/readiness probes and the splash page are always accessible.
var authExemptPaths = map[string]bool{
	"/health":       true,
	"/healthz":      true,
	"/ready":        true,
	"/":             true,
	"/logo.png":     true,
	"/notfound.png": true,
}

// requireAuth returns middleware that enforces bearer token authentication.
// Exempt paths (health probes, splash) bypass authentication.
func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if authExemptPaths[r.URL.Path] {
			next.ServeHTTP(w, r)
			return
		}

		token := extractBearerToken(r)
		if token == "" || !s.validateToken(token) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="muti-metroo"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// extractBearerToken extracts the bearer token from the Authorization header,
// falling back to the ?token= query parameter for WebSocket clients.
func extractBearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return auth[7:]
	}
	return r.URL.Query().Get("token")
}

// validateToken checks whether the provided token matches the configured hash.
// Fast path: compare SHA-256 of token against cached value.
// Slow path: bcrypt verify, then update cache on success.
func (s *Server) validateToken(token string) bool {
	tokenSHA := sha256.Sum256([]byte(token))

	// Fast path: check cache
	s.tokenCacheMu.RLock()
	if s.tokenCacheValid && subtle.ConstantTimeCompare(tokenSHA[:], s.cachedTokenSHA[:]) == 1 {
		s.tokenCacheMu.RUnlock()
		return true
	}
	s.tokenCacheMu.RUnlock()

	// Slow path: bcrypt verify
	if bcrypt.CompareHashAndPassword([]byte(s.cfg.TokenHash), []byte(token)) != nil {
		return false
	}

	// Update cache on success
	s.tokenCacheMu.Lock()
	s.cachedTokenSHA = tokenSHA
	s.tokenCacheValid = true
	s.tokenCacheMu.Unlock()

	return true
}

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// fileTransferAuthStatus returns the HTTP status for a filetransfer auth
// wire code, or 0 if the code is not an auth failure.
func fileTransferAuthStatus(code string) int {
	switch code {
	case filetransfer.ErrCodeAuthRequired:
		return http.StatusUnauthorized
	case filetransfer.ErrCodeInvalidCredentials:
		return http.StatusForbidden
	default:
		return 0
	}
}

// writeFileTransferError writes a structured JSON response for auth failures
// returned by UploadFile/DownloadFileStream. Returns true when it handled
// the error (caller should return).
func writeFileTransferError(w http.ResponseWriter, err error) bool {
	var remoteErr *filetransfer.RemoteError
	if !errors.As(err, &remoteErr) {
		return false
	}
	status := fileTransferAuthStatus(remoteErr.Code)
	if status == 0 {
		return false
	}
	writeJSON(w, status, map[string]interface{}{
		"success": false,
		"code":    remoteErr.Code,
		"error":   remoteErr.Message,
	})
	return true
}

// browseStatusForCode maps a BrowseResponse.ErrorCode to an HTTP status,
// falling back to 400 for non-auth failures.
func browseStatusForCode(code string) int {
	if s := fileTransferAuthStatus(code); s != 0 {
		return s
	}
	return http.StatusBadRequest
}

// requireGET returns true if the request method is GET, otherwise sends a 405 error.
func requireGET(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return false
	}
	return true
}

// requirePOST returns true if the request method is POST, otherwise sends a 405 error.
func requirePOST(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return false
	}
	return true
}

// calculateUptimeHours calculates uptime in hours from a Unix start time.
func calculateUptimeHours(startTime int64) float64 {
	if startTime <= 0 {
		return 0
	}
	return float64(time.Now().Unix()-startTime) / 3600.0
}

// populateNodeInfo fills in TopologyAgentInfo fields from NodeInfo.
//
// Shell, file transfer, and ICMP are reported as disabled when populating
// the local agent's row: their /agents/{id}/... endpoints route through the
// mesh, and the local agent has no mesh route to itself, so the client
// holding this connection cannot use them against the local entry. Remote
// agents in the same response are unaffected.
func populateNodeInfo(agent *TopologyAgentInfo, nodeInfo *protocol.NodeInfo) {
	if nodeInfo == nil {
		return
	}
	agent.Hostname = nodeInfo.Hostname
	agent.OS = nodeInfo.OS
	agent.Arch = nodeInfo.Arch
	agent.Version = nodeInfo.Version
	agent.PID = nodeInfo.PID
	agent.IPAddresses = nodeInfo.IPAddresses
	agent.UptimeHours = calculateUptimeHours(nodeInfo.StartTime)
	if nodeInfo.DisplayName != "" {
		agent.DisplayName = nodeInfo.DisplayName
	}
	if nodeInfo.UDPEnabled {
		agent.UDPEnabled = true
	}
	if len(nodeInfo.Shells) > 0 {
		agent.Shells = nodeInfo.Shells
	}
	if !agent.IsLocal {
		if nodeInfo.ShellEnabled {
			agent.ShellEnabled = true
		}
		if nodeInfo.FileTransferEnabled {
			agent.FileTransferEnabled = true
		}
		if nodeInfo.IcmpEnabled {
			agent.IcmpEnabled = true
		}
	}
	if len(nodeInfo.AdvertisedListeners) > 0 {
		agent.AdvertisedListeners = nodeInfo.AdvertisedListeners
	}
	if len(nodeInfo.ForwardListeners) > 0 {
		keys := make([]string, 0, len(nodeInfo.ForwardListeners))
		for _, fl := range nodeInfo.ForwardListeners {
			keys = append(keys, fl.Key)
		}
		agent.ForwardListeners = keys
	}
}

// buildLocalAgentInfo constructs TopologyAgentInfo for the local agent.
func (s *Server) buildLocalAgentInfo(localID identity.AgentID, displayName string, stats Stats, socks5Info SOCKS5Info, udpInfo UDPInfo, forwardInfo PortForwardInfo) TopologyAgentInfo {
	agent := TopologyAgentInfo{
		ID:          localID.String(),
		ShortID:     localID.ShortString(),
		DisplayName: displayName,
		IsLocal:     true,
		IsConnected: true,
	}
	populateNodeInfo(&agent, s.remoteProvider.GetLocalNodeInfo())

	hasForwardListeners := len(forwardInfo.ListenerKeys) > 0
	hasForwardEndpoints := len(forwardInfo.EndpointKeys) > 0
	agent.Roles = s.buildAgentRoles(true, stats.SOCKS5Running, stats.ExitHandlerRun, hasForwardListeners, hasForwardEndpoints)

	if socks5Info.Enabled {
		agent.SOCKS5Addr = socks5Info.Address
	}
	if udpInfo.Enabled {
		agent.UDPEnabled = true
	}
	if hasForwardListeners {
		agent.ForwardListeners = forwardInfo.ListenerKeys
	}
	if hasForwardEndpoints {
		agent.ForwardEndpoints = forwardInfo.EndpointKeys
	}
	return agent
}

// NewServer creates a new health check server.
func NewServer(cfg ServerConfig, provider StatsProvider) *Server {
	s := &Server{
		cfg:           cfg,
		provider:      provider,
		meshTestState: NewMeshTestState(),
		probeFunc:     probe.Probe,
	}

	mux := http.NewServeMux()

	// Health endpoints - always enabled (minimal footprint)
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/healthz", s.handleHealthz)
	mux.HandleFunc("/ready", s.handleReady)

	// Remote API endpoints: /agents, /agents/*, /routes/advertise, /sleep/*
	if cfg.EnableRemoteAPI {
		mux.HandleFunc("/agents", s.handleListAgents)
		mux.HandleFunc("/agents/", s.handleAgentInfo)
		mux.HandleFunc("/routes/advertise", s.handleTriggerAdvertise)
		mux.HandleFunc("/routes/manage", s.handleRouteManage)
		mux.HandleFunc("/route/trace", s.handleRouteTrace)
		mux.HandleFunc("/forward/manage", s.handleForwardManage)
		mux.HandleFunc("/forward/endpoints/manage", s.handleForwardEndpointManage)
		mux.HandleFunc("/peers/manage", s.handlePeerManage)
		mux.HandleFunc("/listeners/manage", s.handleListenerManage)
		mux.HandleFunc("/display-name/manage", s.handleDisplayNameManage)
		mux.HandleFunc("/hosts/manage", s.handleMeshHostManage)
		mux.HandleFunc("/hosts/suffixes/manage", s.handleMeshHostSuffixManage)
		// Sleep mode endpoints
		mux.HandleFunc("/sleep", s.handleSleep)
		mux.HandleFunc("/sleep/status", s.handleSleepStatus)
		mux.HandleFunc("/wake", s.handleWake)
		mux.HandleFunc("/shutdown", s.handleShutdown)
	} else {
		mux.HandleFunc("/agents", disabledHandler("agents"))
		mux.HandleFunc("/agents/", disabledHandler("agents"))
		mux.HandleFunc("/routes/advertise", disabledHandler("routes_advertise"))
		mux.HandleFunc("/routes/manage", disabledHandler("routes_manage"))
		mux.HandleFunc("/route/trace", disabledHandler("route_trace"))
		mux.HandleFunc("/forward/manage", disabledHandler("forward_manage"))
		mux.HandleFunc("/forward/endpoints/manage", disabledHandler("forward_endpoints_manage"))
		mux.HandleFunc("/peers/manage", disabledHandler("peers_manage"))
		mux.HandleFunc("/listeners/manage", disabledHandler("listeners_manage"))
		mux.HandleFunc("/display-name/manage", disabledHandler("display_name_manage"))
		mux.HandleFunc("/hosts/manage", disabledHandler("hosts_manage"))
		mux.HandleFunc("/hosts/suffixes/manage", disabledHandler("hosts_suffixes_manage"))
		mux.HandleFunc("/sleep", disabledHandler("sleep"))
		mux.HandleFunc("/sleep/status", disabledHandler("sleep_status"))
		mux.HandleFunc("/wake", disabledHandler("wake"))
		mux.HandleFunc("/shutdown", disabledHandler("shutdown"))
	}

	// Dashboard API endpoints
	if cfg.EnableDashboard {
		mux.HandleFunc("/api/topology", s.handleTopology)
		mux.HandleFunc("/api/dashboard", s.handleDashboard)
		mux.HandleFunc("/api/nodes", s.handleNodes)
		mux.HandleFunc("/api/mesh-test", s.handleMeshTest)
		mux.HandleFunc("/api/probe", s.handleProbe)
		mux.HandleFunc("/api/setup-defaults", s.handleSetupDefaults)
		mux.HandleFunc("/api/interfaces", s.handleInterfaces)
	} else {
		mux.HandleFunc("/api/", disabledHandler("dashboard_api"))
	}

	// pprof debug endpoints
	if cfg.EnablePprof {
		mux.HandleFunc("/debug/pprof/", pprof.Index)
		mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
		mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
		mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
		mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	} else {
		mux.HandleFunc("/debug/", disabledHandler("pprof"))
	}

	// Logo image for splash page
	mux.HandleFunc("/logo.png", handleLogo)
	mux.HandleFunc("/notfound.png", handleNotFoundImage)

	// Root splash page
	mux.HandleFunc("/", s.handleSplash)

	// Wrap with auth middleware if token_hash is configured
	var handler http.Handler = mux
	if cfg.TokenHash != "" {
		handler = s.requireAuth(mux)
	}

	s.server = &http.Server{
		Addr:         cfg.Address,
		Handler:      handler,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
	}

	return s
}

// SetRemoteProvider sets the remote status provider.
// This is called after the agent is initialized.
func (s *Server) SetRemoteProvider(provider RemoteStatusProvider) {
	s.remoteProvider = provider
}

// SetRouteAdvertiseTrigger sets the route advertisement trigger.
// This is called after the agent is initialized.
func (s *Server) SetRouteAdvertiseTrigger(trigger RouteAdvertiseTrigger) {
	s.routeTrigger = trigger
}

// SetSealedBox sets the sealed box for checking decrypt capability.
// This is called after the agent is initialized.
func (s *Server) SetSealedBox(sealedBox *crypto.SealedBox) {
	s.sealedBox = sealedBox
}

// SetSleepProvider sets the sleep mode provider.
// This is called after the agent is initialized.
func (s *Server) SetSleepProvider(provider SleepProvider) {
	s.sleepProvider = provider
}

// SetStopProvider wires the agent's shutdown trigger for /shutdown.
func (s *Server) SetStopProvider(provider StopProvider) {
	s.stopProvider = provider
}

// SetRouteManageProvider sets the route management provider.
// This is called after the agent is initialized.
func (s *Server) SetRouteManageProvider(provider RouteManageProvider) {
	s.routeManageProvider = provider
}

// SetMeshHostProvider sets the mesh-host registry provider.
// This is called after the agent is initialized.
func (s *Server) SetMeshHostProvider(provider MeshHostProvider) {
	s.meshHostProvider = provider
}

// SetForwardManageProvider sets the forward listener management provider.
// This is called after the agent is initialized.
func (s *Server) SetForwardManageProvider(provider ForwardManageProvider) {
	s.forwardManageProvider = provider
}

// SetForwardEndpointManageProvider sets the forward endpoint management provider.
// This is called after the agent is initialized.
func (s *Server) SetForwardEndpointManageProvider(provider ForwardEndpointManageProvider) {
	s.forwardEndpointManageProvider = provider
}

// SetPeerManageProvider sets the dynamic peer management provider.
// This is called after the agent is initialized.
func (s *Server) SetPeerManageProvider(provider PeerManageProvider) {
	s.peerManageProvider = provider
}

// SetListenerManageProvider sets the dynamic transport listener management provider.
// This is called after the agent is initialized.
func (s *Server) SetListenerManageProvider(provider ListenerManageProvider) {
	s.listenerManageProvider = provider
}

// SetFileBrowseProvider sets the file browse provider.
// This is called after the agent is initialized.
func (s *Server) SetFileBrowseProvider(provider FileBrowseProvider) {
	s.fileBrowseProvider = provider
}

// SetDisplayNameManageProvider sets the display name management provider.
// This is called after the agent is initialized.
func (s *Server) SetDisplayNameManageProvider(provider DisplayNameManageProvider) {
	s.displayNameManageProvider = provider
}

// MgmtAuthProvider supplies management-key signing/verification for
// HTTP endpoints that forward privileged requests to remote agents. The
// HTTP layer uses it to (a) reject requests when the local agent cannot
// sign, and (b) stamp signed auth envelopes onto bodies before
// forwarding them.
type MgmtAuthProvider interface {
	CanSignManagement() bool
	// StampMutationBody is used for dynamic-config mutations; the
	// signature binds the canonical body hash so a signed "add route X"
	// cannot be replayed as "remove route Y".
	StampMutationBody(target identity.AgentID, opTag string, body []byte) ([]byte, error)
	// StampBody is used for file browse and other file operations whose
	// target-side verifier expects an envelope without a body hash
	// (target policy -- allowed_paths, max_file_size -- constrains what
	// the operation can do).
	StampBody(target identity.AgentID, opTag string, body []byte) ([]byte, error)
}

// SetMgmtAuthProvider sets the management-auth provider used by mutation
// endpoints.
func (s *Server) SetMgmtAuthProvider(provider MgmtAuthProvider) {
	s.mgmtAuthProvider = provider
}

// requireMutationAuthorization rejects the request with 403 when the local
// agent lacks a signing private key. Returns true when the caller may
// proceed.
func (s *Server) requireMutationAuthorization(w http.ResponseWriter) bool {
	if s.mgmtAuthProvider == nil || !s.mgmtAuthProvider.CanSignManagement() {
		http.Error(w, "management signing private key not configured on this agent", http.StatusForbidden)
		return false
	}
	return true
}

// isMutationAction reports whether the action is a state-changing
// mutation that should require a management signature. List/get actions
// are read-only and stay gated only by the HTTP bearer token.
func isMutationAction(action string) bool {
	switch action {
	case "add", "remove", "set", "set_comment", "set_source_ip", "set_interface":
		return true
	default:
		return false
	}
}

// CanDecryptManagement returns true if management key decryption is available.
func (s *Server) CanDecryptManagement() bool {
	return s.sealedBox != nil && s.sealedBox.CanDecrypt()
}

// shouldRestrictTopology returns true if topology info should be restricted to local-only.
// This happens when management key encryption is enabled but we don't have the private key.
func (s *Server) shouldRestrictTopology() bool {
	// If no sealed box configured, no restriction (management key not enabled)
	if s.sealedBox == nil {
		return false
	}
	// If sealed box configured but can't decrypt, restrict to local only
	return !s.sealedBox.CanDecrypt()
}

// Start starts the health check server.
func (s *Server) Start() error {
	ln, err := net.Listen("tcp", s.cfg.Address)
	if err != nil {
		return err
	}
	s.listener = ln
	s.running.Store(true)

	go s.server.Serve(ln)

	return nil
}

// Stop stops the health check server.
func (s *Server) Stop() error {
	if !s.running.Swap(false) {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	return s.server.Shutdown(ctx)
}

// Address returns the server's listen address.
func (s *Server) Address() net.Addr {
	if s.listener != nil {
		return s.listener.Addr()
	}
	return nil
}

// IsRunning returns true if the server is running.
func (s *Server) IsRunning() bool {
	return s.running.Load()
}

// handleHealth handles the basic health check endpoint.
// Returns 200 if the server is responding.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if !requireGET(w, r) {
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK\n"))
}

// handleHealthz handles the detailed health check endpoint.
// Returns 200 with JSON stats if healthy, 503 if not running.
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if !requireGET(w, r) {
		return
	}

	if s.provider == nil || !s.provider.IsRunning() {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{
			"status":  "unavailable",
			"running": false,
		})
		return
	}

	stats := s.provider.Stats()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":                    "healthy",
		"running":                   true,
		"peer_count":                stats.PeerCount,
		"stream_count":              stats.StreamCount,
		"route_count":               stats.RouteCount,
		"socks5_running":            stats.SOCKS5Running,
		"exit_handler_running":      stats.ExitHandlerRun,
		"can_sign_management":       stats.CanSignManagement,
		"accepts_signed_management": stats.AcceptsSignedManagement,
	})
}

// handleReady handles the readiness probe endpoint.
// Returns 200 if the agent is ready to serve traffic.
func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	if !requireGET(w, r) {
		return
	}

	w.Header().Set("Content-Type", "text/plain")
	if s.provider == nil || !s.provider.IsRunning() {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte("NOT READY\n"))
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("READY\n"))
}

// handleLogo serves the embedded logo image.
func handleLogo(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Write(logoPNG)
}

// handleNotFoundImage serves the embedded 404 image.
func handleNotFoundImage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Write(notFoundPNG)
}

// notFoundPage is the custom 404 page with the same foggy background as the splash page.
const notFoundPage = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>404</title>
    <style>
        * { margin: 0; padding: 0; box-sizing: border-box; }
        body {
            background: radial-gradient(ellipse at 50% 60%, #d5cbbe 0%, #c4b8a6 25%, #b0a48f 50%, #9a8e7a 75%, #857968 100%);
            min-height: 100vh;
            display: flex;
            flex-direction: column;
            align-items: center;
            justify-content: center;
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
            color: #3a3228;
        }
        img { width: 160px; height: 160px; margin-bottom: 2rem; }
        h1 { font-size: 1.1rem; font-weight: 400; opacity: 0.7; font-family: monospace; }
    </style>
</head>
<body>
    <img src="/notfound.png" alt="404">
    <h1>&#1054;&#1096;&#1080;&#1073;&#1082;&#1072; 404. &#1057;&#1090;&#1088;&#1072;&#1085;&#1080;&#1094;&#1072; &#1085;&#1077; &#1085;&#1072;&#1081;&#1076;&#1077;&#1085;&#1072;</h1>
</body>
</html>
`

// handleSplash handles the root "/" splash page.
func (s *Server) handleSplash(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		io.WriteString(w, notFoundPage)
		return
	}
	if !requireGET(w, r) {
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	io.WriteString(w, splashPageTemplate)
}

// Handler returns the HTTP handler for embedding in other servers.
func (s *Server) Handler() http.Handler {
	return s.server.Handler
}

// handleListAgents lists all known agents in the mesh.
func (s *Server) handleListAgents(w http.ResponseWriter, r *http.Request) {
	if !requireGET(w, r) {
		return
	}
	if s.remoteProvider == nil {
		http.Error(w, "remote provider not configured", http.StatusServiceUnavailable)
		return
	}

	localID := s.remoteProvider.ID()

	// When management key encryption is enabled but we lack the private key,
	// restrict to local-only to avoid leaking mesh topology.
	if s.shouldRestrictTopology() {
		writeJSON(w, http.StatusOK, []map[string]interface{}{
			{
				"id":           localID.String(),
				"short":        localID.ShortString(),
				"display_name": s.remoteProvider.DisplayName(),
				"local":        true,
			},
		})
		return
	}

	agents := s.remoteProvider.GetKnownAgentIDs()

	result := []map[string]interface{}{
		{
			"id":           localID.String(),
			"short":        localID.ShortString(),
			"display_name": s.remoteProvider.DisplayName(),
			"local":        true,
		},
	}

	for _, id := range agents {
		if id != localID {
			result = append(result, map[string]interface{}{
				"id":    id.String(),
				"short": id.ShortString(),
				"local": false,
			})
		}
	}

	writeJSON(w, http.StatusOK, result)
}

// handleAgentInfo handles fetching status from a specific agent.
// URL format: /agents/{agent-id} or /agents/{agent-id}/routes or /agents/{agent-id}/peers
func (s *Server) handleAgentInfo(w http.ResponseWriter, r *http.Request) {
	if s.remoteProvider == nil {
		http.Error(w, "remote provider not configured", http.StatusServiceUnavailable)
		return
	}

	// Parse path: /agents/{agent-id}[/routes|/peers|/shell|/file/*]
	path := strings.TrimPrefix(r.URL.Path, "/agents/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) == 0 || parts[0] == "" {
		http.Error(w, "agent ID required", http.StatusBadRequest)
		return
	}

	targetID, err := identity.ParseAgentID(parts[0])
	if err != nil {
		http.Error(w, "invalid agent ID format", http.StatusBadRequest)
		return
	}

	// Dispatch to sub-handlers based on path suffix
	if len(parts) > 1 {
		switch {
		case strings.HasPrefix(parts[1], "file/upload"):
			s.handleFileUpload(w, r, targetID)
			return
		case strings.HasPrefix(parts[1], "file/download"):
			s.handleFileDownload(w, r, targetID)
			return
		case parts[1] == "shell":
			s.handleShellWebSocket(w, r, targetID)
			return
		case parts[1] == "icmp":
			s.handleICMPWebSocket(w, r, targetID)
			return
		case parts[1] == "routes/manage":
			s.handleRemoteRouteManage(w, r, targetID)
			return
		case parts[1] == "route/trace":
			s.handleRemoteRouteTrace(w, r, targetID)
			return
		case parts[1] == "forward/manage":
			s.handleRemoteForwardManage(w, r, targetID)
			return
		case parts[1] == "forward/endpoints/manage":
			s.handleRemoteForwardEndpointManage(w, r, targetID)
			return
		case parts[1] == "peers/manage":
			s.handleRemotePeerManage(w, r, targetID)
			return
		case parts[1] == "listeners/manage":
			s.handleRemoteListenerManage(w, r, targetID)
			return
		case parts[1] == "display-name/manage":
			s.handleRemoteDisplayNameManage(w, r, targetID)
			return
		case parts[1] == "file/browse":
			s.handleFileBrowse(w, r, targetID)
			return
		case parts[1] == "probe":
			s.handleRemoteProbe(w, r, targetID)
			return
		case parts[1] == "shutdown":
			s.handleRemoteShutdown(w, r, targetID)
			return
		}
	}

	// For status/routes/peers requests, only allow GET
	if !requireGET(w, r) {
		return
	}

	// When management key encryption is enabled but we lack the private key,
	// block remote agent queries to avoid leaking mesh topology.
	if s.shouldRestrictTopology() {
		http.Error(w, "topology restricted: management key decryption unavailable", http.StatusForbidden)
		return
	}

	// Determine control type from path suffix
	controlType := protocol.ControlTypeStatus
	if len(parts) > 1 {
		switch parts[1] {
		case "routes":
			controlType = protocol.ControlTypeRoutes
		case "peers":
			controlType = protocol.ControlTypePeers
		case "interfaces":
			controlType = protocol.ControlTypeInterfaces
		}
	}

	// Fetch from remote agent
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	resp, err := s.remoteProvider.SendControlRequest(ctx, targetID, controlType)
	if err != nil {
		http.Error(w, "failed to fetch: "+err.Error(), http.StatusBadGateway)
		return
	}
	if !resp.Success {
		http.Error(w, "remote agent error: "+string(resp.Data), http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(resp.Data)
}

// handleFileUpload handles file upload requests for files and directories.
// POST /agents/{agent-id}/file/upload
// Content-Type: multipart/form-data
// Form fields:
//   - file: the file to upload (can be a tar archive for directories)
//   - path: remote destination path (required)
//   - directory: "true" if uploading a directory tar (optional)
//
// Response: JSON with success, error, bytes_written
func (s *Server) handleFileUpload(w http.ResponseWriter, r *http.Request, targetID identity.AgentID) {
	if !requirePOST(w, r) {
		return
	}

	// Parse multipart form (max 32MB in memory, rest goes to temp files)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		http.Error(w, "failed to parse multipart form: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Get remote path
	remotePath := r.FormValue("path")
	if remotePath == "" {
		http.Error(w, "missing required field: path", http.StatusBadRequest)
		return
	}

	isDirectory := r.FormValue("directory") == "true"

	// Parse rate limit
	var rateLimit int64
	if rl := r.FormValue("rate_limit"); rl != "" {
		fmt.Sscanf(rl, "%d", &rateLimit)
	}

	// Parse resume options
	resumeUpload := r.FormValue("resume") == "true"
	var originalSize int64
	if os := r.FormValue("original_size"); os != "" {
		fmt.Sscanf(os, "%d", &originalSize)
	}
	_ = resumeUpload // TODO: implement upload resume on server side

	// Get uploaded file
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "failed to get uploaded file: "+err.Error(), http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Create temp file to store upload
	tmpFile, err := os.CreateTemp("", "upload-*")
	if err != nil {
		http.Error(w, "failed to create temp file: "+err.Error(), http.StatusInternalServerError)
		return
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath) // Clean up temp file

	// Copy uploaded file to temp. A wedged upload (client stalls mid-body)
	// would otherwise pin the HTTP request goroutine past agent shutdown --
	// bind the read to the agent stop signal so it bails on Stop().
	uploadCtx, uploadCancel := s.remoteProvider.ShutdownBoundCtx(r.Context())
	defer uploadCancel()
	bytesReceived, err := io.Copy(tmpFile, &filetransfer.CtxReader{Ctx: uploadCtx, R: file})
	tmpFile.Close()
	if err != nil {
		http.Error(w, "failed to save uploaded file: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// For directories, extract the tar first
	localPath := tmpPath
	if isDirectory {
		// Create temp directory for extraction
		tmpDir, err := os.MkdirTemp("", "upload-dir-*")
		if err != nil {
			http.Error(w, "failed to create temp directory: "+err.Error(), http.StatusInternalServerError)
			return
		}
		defer os.RemoveAll(tmpDir)

		// Open the tar file
		tarFile, err := os.Open(tmpPath)
		if err != nil {
			http.Error(w, "failed to open tar file: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// Try to extract (handles gzip internally)
		if err := extractTarWithFallback(tarFile, tmpDir); err != nil {
			tarFile.Close()
			http.Error(w, "failed to extract tar: "+err.Error(), http.StatusBadRequest)
			return
		}
		tarFile.Close()
		localPath = tmpDir
	}

	// Build transfer options
	opts := TransferOptions{
		RateLimit:    rateLimit,
		OriginalSize: originalSize,
	}

	// Perform stream-based upload. Bind ctx to agent stop so a wedged mesh
	// stream cannot keep the handler goroutine running past Stop().
	boundCtx, boundCancel := s.remoteProvider.ShutdownBoundCtx(r.Context())
	defer boundCancel()
	ctx, cancel := context.WithTimeout(boundCtx, 30*time.Minute) // 30 minute timeout for large files
	defer cancel()

	// Extend write deadline for long file transfers (default WriteTimeout is 10s)
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Now().Add(30 * time.Minute))

	err = s.remoteProvider.UploadFile(ctx, targetID, localPath, remotePath, opts, nil)
	if err != nil {
		if writeFileTransferError(w, err) {
			return
		}
		writeJSON(w, http.StatusBadGateway, map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":       true,
		"bytes_written": bytesReceived,
		"filename":      header.Filename,
		"remote_path":   remotePath,
	})
}

// handleFileDownload handles file download requests for files and directories.
// POST /agents/{agent-id}/file/download
// Request body: JSON with path and optional rate-limit/offset fields
// Response: Binary file data with Content-Disposition header, or tar.gz for directories
func (s *Server) handleFileDownload(w http.ResponseWriter, r *http.Request, targetID identity.AgentID) {
	if !requirePOST(w, r) {
		return
	}

	// Read request body
	body, err := io.ReadAll(io.LimitReader(r.Body, 1*1024*1024)) // 1MB limit for request
	if err != nil {
		http.Error(w, "failed to read request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Parse request
	var req struct {
		Path         string `json:"path"`
		RateLimit    int64  `json:"rate_limit,omitempty"`
		Offset       int64  `json:"offset,omitempty"`
		OriginalSize int64  `json:"original_size,omitempty"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	if req.Path == "" {
		http.Error(w, "missing required field: path", http.StatusBadRequest)
		return
	}

	// Use the basename of the remote path as local filename
	localName := filepath.Base(req.Path)
	if localName == "" || localName == "." || localName == "/" {
		localName = "download"
	}

	// Build transfer options
	opts := TransferOptions{
		RateLimit:    req.RateLimit,
		Offset:       req.Offset,
		OriginalSize: req.OriginalSize,
	}

	// Perform streaming download directly (no temp file). Bind ctx to agent
	// stop so the io.Copy below unblocks on Stop().
	boundCtx, boundCancel := s.remoteProvider.ShutdownBoundCtx(r.Context())
	defer boundCancel()
	ctx, cancel := context.WithTimeout(boundCtx, 30*time.Minute) // 30 minute timeout
	defer cancel()

	// Extend write deadline for long file transfers (default WriteTimeout is 10s)
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Now().Add(30 * time.Minute))

	result, err := s.remoteProvider.DownloadFileStream(ctx, targetID, req.Path, opts)
	if err != nil {
		if writeFileTransferError(w, err) {
			return
		}
		writeJSON(w, http.StatusBadGateway, map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		})
		return
	}
	defer result.Reader.Close()
	if result.Close != nil {
		defer result.Close()
	}

	// Set headers based on result type
	if result.IsDirectory {
		// For directories, stream compressed tar data directly
		w.Header().Set("Content-Type", "application/gzip")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", localName+".tar.gz"))
	} else {
		// For files, stream directly
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", localName))
		w.Header().Set("X-File-Mode", fmt.Sprintf("%04o", result.Mode))
		w.Header().Set("X-Original-Size", fmt.Sprintf("%d", result.OriginalSize))
		// Note: We don't set Content-Length because the stream is decompressed
		// and we don't know the final size until we've read it all
	}

	// Stream data directly to response. Wrap with the shutdown-bound ctx so
	// the copy aborts on agent stop instead of pinning this goroutine.
	_, err = io.Copy(w, &filetransfer.CtxReader{Ctx: ctx, R: result.Reader})
	if err != nil {
		// Can't return error via HTTP at this point since we already started streaming
		// The connection will just be broken
		return
	}
}

// extractTarWithFallback tries to extract a tar archive, handling both plain tar and gzip.
func extractTarWithFallback(r io.Reader, destDir string) error {
	// Try reading first few bytes to detect gzip
	buf := make([]byte, 2)
	n, err := r.Read(buf)
	if err != nil && err != io.EOF {
		return err
	}

	// Create a reader that includes the bytes we already read
	var reader io.Reader
	if n > 0 {
		reader = io.MultiReader(bytes.NewReader(buf[:n]), r)
	} else {
		reader = r
	}

	// Check for gzip magic number
	if n >= 2 && buf[0] == 0x1f && buf[1] == 0x8b {
		gzr, err := gzip.NewReader(reader)
		if err != nil {
			return fmt.Errorf("failed to create gzip reader: %w", err)
		}
		defer gzr.Close()
		reader = gzr
	}

	// Create tar reader
	tr := tar.NewReader(reader)

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read tar header: %w", err)
		}

		targetPath := filepath.Join(destDir, header.Name)

		// Security check
		if !strings.HasPrefix(filepath.Clean(targetPath), filepath.Clean(destDir)) {
			return fmt.Errorf("tar entry attempts path traversal: %s", header.Name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(targetPath, os.FileMode(header.Mode)); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
				return err
			}
			f, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return err
			}
			f.Close()
		case tar.TypeSymlink:
			if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
				return err
			}
			os.Remove(targetPath)
			if err := os.Symlink(header.Linkname, targetPath); err != nil {
				return err
			}
		}
	}

	return nil
}

// handleTriggerAdvertise handles POST /routes/advertise to trigger immediate route advertisement.
func (s *Server) handleTriggerAdvertise(w http.ResponseWriter, r *http.Request) {
	if !requirePOST(w, r) {
		return
	}
	if s.routeTrigger == nil {
		http.Error(w, "route trigger not configured", http.StatusServiceUnavailable)
		return
	}

	s.routeTrigger.TriggerRouteAdvertise()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":  "triggered",
		"message": "route advertisement triggered",
	})
}

// handleRouteManage handles POST /routes/manage to add/remove/list dynamic routes.
func (s *Server) handleRouteManage(w http.ResponseWriter, r *http.Request) {
	if !requirePOST(w, r) {
		return
	}
	if s.routeManageProvider == nil {
		http.Error(w, "route management not configured", http.StatusServiceUnavailable)
		return
	}
	if s.shouldRestrictTopology() {
		http.Error(w, "route management restricted: management key decryption unavailable", http.StatusForbidden)
		return
	}

	var req struct {
		Action    string `json:"action"`
		Network   string `json:"network"`
		Metric    uint16 `json:"metric"`
		Comment   string `json:"comment,omitempty"`
		SourceIP  string `json:"source_ip,omitempty"`
		Interface string `json:"interface,omitempty"`
		Scope     string `json:"scope,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request: " + err.Error()})
		return
	}

	if isMutationAction(req.Action) && !s.requireMutationAuthorization(w) {
		return
	}

	result, err := s.routeManageProvider.ManageRoute(req.Action, req.Network, req.Metric, req.Comment, req.SourceIP, req.Interface, req.Scope)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, result)
}

// handleMeshHostManage handles POST /hosts/manage to add/remove/list
// mesh-host entries. The store is local-only, so unlike /routes/manage
// this handler does not gate on shouldRestrictTopology(). Sentinel
// errors from the store are mapped to 400/404/409/403; anything else
// becomes 400.
func (s *Server) handleMeshHostManage(w http.ResponseWriter, r *http.Request) {
	if !requirePOST(w, r) {
		return
	}
	if s.meshHostProvider == nil {
		http.Error(w, "mesh host management not configured", http.StatusServiceUnavailable)
		return
	}

	var req struct {
		Action  string `json:"action"`
		Name    string `json:"name"`
		IPv4    string `json:"ipv4"`
		IPv6    string `json:"ipv6"`
		Comment string `json:"comment"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request: " + err.Error()})
		return
	}

	if isMutationAction(req.Action) && !s.requireMutationAuthorization(w) {
		return
	}

	result, err := s.meshHostProvider.ManageMeshHost(req.Action, req.Name, req.IPv4, req.IPv6, req.Comment)
	if err != nil {
		status := http.StatusBadRequest
		switch {
		case errors.Is(err, hosts.ErrExists):
			status = http.StatusConflict
		case errors.Is(err, hosts.ErrNotFound):
			status = http.StatusNotFound
		case errors.Is(err, hosts.ErrProtected):
			status = http.StatusForbidden
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, result)
}

// handleMeshHostSuffixManage handles POST /hosts/suffixes/manage to
// add, remove, or list TLD suffixes for the local agent's mesh-host
// registry. Mirrors handleMeshHostManage; sentinel errors map to
// 400/404/409.
func (s *Server) handleMeshHostSuffixManage(w http.ResponseWriter, r *http.Request) {
	if !requirePOST(w, r) {
		return
	}
	if s.meshHostProvider == nil {
		http.Error(w, "mesh host management not configured", http.StatusServiceUnavailable)
		return
	}

	var req struct {
		Action string `json:"action"`
		Suffix string `json:"suffix"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request: " + err.Error()})
		return
	}

	if isMutationAction(req.Action) && !s.requireMutationAuthorization(w) {
		return
	}

	result, err := s.meshHostProvider.ManageMeshHostSuffix(req.Action, req.Suffix)
	if err != nil {
		status := http.StatusBadRequest
		switch {
		case errors.Is(err, hosts.ErrExists), errors.Is(err, hosts.ErrSuffixInUse):
			status = http.StatusConflict
		case errors.Is(err, hosts.ErrNotFound):
			status = http.StatusNotFound
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, result)
}

// forwardRemoteControl forwards a control request body to a remote agent and relays the response.
// Used by both route and forward management remote handlers.
func (s *Server) forwardRemoteControl(w http.ResponseWriter, r *http.Request, targetID identity.AgentID, controlType uint8, featureName string) {
	s.forwardRemoteControlWithStatus(w, r, targetID, controlType, featureName, nil, nil, signWithBodyHash)
}

// signStrategy selects how a forwarded request is stamped. Mutations
// bind the canonical body hash (so swapping fields invalidates the
// signature); file operations sign only the (origin, target, timestamp,
// op) tuple to match the verifier used by shell and file-transfer
// streams.
type signStrategy int

const (
	signWithBodyHash signStrategy = iota
	signWithoutBodyHash
)

// forwardRemoteControlMutation is forwardRemoteControl for mutation
// endpoints: it requires the local agent to hold the management signing
// private key and stamps an auth envelope onto the body before
// forwarding. opTagFor returns the crypto.Op* tag for a given action
// string (e.g. "add", "remove") or "" if the action is not a mutation.
func (s *Server) forwardRemoteControlMutation(w http.ResponseWriter, r *http.Request, targetID identity.AgentID, controlType uint8, featureName string, opTagFor func(action string) string) {
	s.forwardRemoteControlWithStatus(w, r, targetID, controlType, featureName, nil, opTagFor, signWithBodyHash)
}

// forwardRemoteControlWithStatus is forwardRemoteControl with an optional
// errStatusFn that inspects a failure response body and returns the HTTP
// status to use (e.g. 401 for auth failures). nil = default 400.
// opTagFor, when non-nil, makes this a signing forwarder: the request's
// "action" is looked up, and if it resolves to a non-empty op tag the
// local agent must sign the body before forwarding. strategy decides
// whether the signature binds the canonical body hash.
func (s *Server) forwardRemoteControlWithStatus(w http.ResponseWriter, r *http.Request, targetID identity.AgentID, controlType uint8, featureName string, errStatusFn func([]byte) int, opTagFor func(action string) string, strategy signStrategy) {
	if !requirePOST(w, r) {
		return
	}
	if s.remoteProvider == nil {
		http.Error(w, "remote provider not configured", http.StatusServiceUnavailable)
		return
	}
	if s.shouldRestrictTopology() {
		http.Error(w, featureName+" restricted: management key decryption unavailable", http.StatusForbidden)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read request body", http.StatusBadRequest)
		return
	}

	if opTagFor != nil {
		var probe struct {
			Action string `json:"action"`
		}
		_ = json.Unmarshal(body, &probe)
		if opTag := opTagFor(probe.Action); opTag != "" {
			if s.mgmtAuthProvider == nil || !s.mgmtAuthProvider.CanSignManagement() {
				http.Error(w, "management signing private key not configured on this agent", http.StatusForbidden)
				return
			}
			var signed []byte
			switch strategy {
			case signWithoutBodyHash:
				signed, err = s.mgmtAuthProvider.StampBody(targetID, opTag, body)
			default:
				signed, err = s.mgmtAuthProvider.StampMutationBody(targetID, opTag, body)
			}
			if err != nil {
				http.Error(w, "failed to sign request: "+err.Error(), http.StatusInternalServerError)
				return
			}
			body = signed
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	resp, err := s.remoteProvider.SendControlRequestWithData(ctx, targetID, controlType, body)
	if err != nil {
		http.Error(w, "failed to send request: "+err.Error(), http.StatusBadGateway)
		return
	}

	status := http.StatusOK
	if !resp.Success {
		status = http.StatusBadRequest
		if errStatusFn != nil {
			if custom := errStatusFn(resp.Data); custom != 0 {
				status = custom
			}
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(resp.Data)
}

// mutationOpTags maps each mutation feature to the per-action crypto.Op*
// tag. Used to pick the right tag for a forwarded mutation based on the
// request's "action" field; empty string means the action is not a
// mutation (e.g. list/get) and must not be signed.
var mutationOpTags = map[string]struct{ add, remove, set, setComment, setSourceIP, setInterface string }{
	"route":            {crypto.OpRouteAdd, crypto.OpRouteRemove, "", crypto.OpRouteCommentSet, crypto.OpRouteSourceIPSet, crypto.OpRouteInterfaceSet},
	"forward":          {crypto.OpForwardAdd, crypto.OpForwardRemove, "", "", "", ""},
	"forward_endpoint": {crypto.OpForwardEndpointAdd, crypto.OpForwardEndpointRemove, "", "", "", ""},
	"peer":             {crypto.OpPeerAdd, crypto.OpPeerRemove, "", "", "", ""},
	"listener":         {crypto.OpListenerAdd, crypto.OpListenerRemove, "", "", "", ""},
	"display_name":     {"", "", crypto.OpDisplayNameSet, "", "", ""},
}

func opTagFor(feature, action string) string {
	entry, ok := mutationOpTags[feature]
	if !ok {
		return ""
	}
	switch action {
	case "add":
		return entry.add
	case "remove":
		return entry.remove
	case "set":
		return entry.set
	case "set_comment":
		return entry.setComment
	case "set_source_ip":
		return entry.setSourceIP
	case "set_interface":
		return entry.setInterface
	}
	return ""
}

// featureOpTagFunc binds opTagFor to a given feature so forwarders can
// pass a single-arg closure to forwardRemoteControlMutation.
func featureOpTagFunc(feature string) func(string) string {
	return func(action string) string { return opTagFor(feature, action) }
}

// handleRemoteRouteManage forwards route management requests to a remote agent.
// Mutation actions are signed with the local management private key before
// being forwarded; list actions pass through unsigned.
func (s *Server) handleRemoteRouteManage(w http.ResponseWriter, r *http.Request, targetID identity.AgentID) {
	s.forwardRemoteControlMutation(w, r, targetID, protocol.ControlTypeRouteManage, "route management", featureOpTagFunc("route"))
}

// handleForwardManage handles POST /forward/manage to add/remove/list dynamic forward listeners.
func (s *Server) handleForwardManage(w http.ResponseWriter, r *http.Request) {
	if !requirePOST(w, r) {
		return
	}
	if s.forwardManageProvider == nil {
		http.Error(w, "forward management not configured", http.StatusServiceUnavailable)
		return
	}
	if s.shouldRestrictTopology() {
		http.Error(w, "forward management restricted: management key decryption unavailable", http.StatusForbidden)
		return
	}

	var req struct {
		Action         string `json:"action"`
		Key            string `json:"key"`
		Address        string `json:"address"`
		MaxConnections int    `json:"max_connections"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request: " + err.Error()})
		return
	}

	if isMutationAction(req.Action) && !s.requireMutationAuthorization(w) {
		return
	}

	result, err := s.forwardManageProvider.ManageForwardListener(req.Action, req.Key, req.Address, req.MaxConnections)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, result)
}

// handleRemoteForwardManage forwards forward listener management requests to a remote agent.
func (s *Server) handleRemoteForwardManage(w http.ResponseWriter, r *http.Request, targetID identity.AgentID) {
	s.forwardRemoteControlMutation(w, r, targetID, protocol.ControlTypeForwardManage, "forward management", featureOpTagFunc("forward"))
}

// handleForwardEndpointManage handles POST /forward/endpoints/manage to add/remove/list dynamic forward endpoints.
func (s *Server) handleForwardEndpointManage(w http.ResponseWriter, r *http.Request) {
	if !requirePOST(w, r) {
		return
	}
	if s.forwardEndpointManageProvider == nil {
		http.Error(w, "forward endpoint management not configured", http.StatusServiceUnavailable)
		return
	}
	if s.shouldRestrictTopology() {
		http.Error(w, "forward endpoint management restricted: management key decryption unavailable", http.StatusForbidden)
		return
	}

	var req struct {
		Action string `json:"action"`
		Key    string `json:"key"`
		Target string `json:"target"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request: " + err.Error()})
		return
	}

	if isMutationAction(req.Action) && !s.requireMutationAuthorization(w) {
		return
	}

	result, err := s.forwardEndpointManageProvider.ManageForwardEndpoint(req.Action, req.Key, req.Target)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, result)
}

// handleRemoteForwardEndpointManage forwards forward endpoint management requests to a remote agent.
func (s *Server) handleRemoteForwardEndpointManage(w http.ResponseWriter, r *http.Request, targetID identity.AgentID) {
	s.forwardRemoteControlMutation(w, r, targetID, protocol.ControlTypeForwardEndpointManage, "forward endpoint management", featureOpTagFunc("forward_endpoint"))
}

// handlePeerManage handles POST /peers/manage to add/remove/list dynamic peer connections.
func (s *Server) handlePeerManage(w http.ResponseWriter, r *http.Request) {
	if !requirePOST(w, r) {
		return
	}
	if s.peerManageProvider == nil {
		http.Error(w, "peer management not configured", http.StatusServiceUnavailable)
		return
	}
	if s.shouldRestrictTopology() {
		http.Error(w, "peer management restricted: management key decryption unavailable", http.StatusForbidden)
		return
	}

	var req struct {
		Action       string `json:"action"`
		Address      string `json:"address"`
		Transport    string `json:"transport"`
		ID           string `json:"id"`
		TCPKeepAlive string `json:"tcp_keepalive"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request: " + err.Error()})
		return
	}

	if isMutationAction(req.Action) && !s.requireMutationAuthorization(w) {
		return
	}

	result, err := s.peerManageProvider.ManagePeer(req.Action, req.Address, req.Transport, req.ID, req.TCPKeepAlive)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, result)
}

// handleRemotePeerManage forwards peer management requests to a remote agent.
func (s *Server) handleRemotePeerManage(w http.ResponseWriter, r *http.Request, targetID identity.AgentID) {
	s.forwardRemoteControlMutation(w, r, targetID, protocol.ControlTypePeerManage, "peer management", featureOpTagFunc("peer"))
}

// handleListenerManage handles POST /listeners/manage to add/remove/list dynamic transport listeners.
func (s *Server) handleListenerManage(w http.ResponseWriter, r *http.Request) {
	if !requirePOST(w, r) {
		return
	}
	if s.listenerManageProvider == nil {
		http.Error(w, "listener management not configured", http.StatusServiceUnavailable)
		return
	}
	if s.shouldRestrictTopology() {
		http.Error(w, "listener management restricted: management key decryption unavailable", http.StatusForbidden)
		return
	}

	var req ListenerManageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request: " + err.Error()})
		return
	}

	if isMutationAction(req.Action) && !s.requireMutationAuthorization(w) {
		return
	}

	result, err := s.listenerManageProvider.ManageListener(&req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, result)
}

// handleRemoteListenerManage forwards listener management requests to a remote agent.
func (s *Server) handleRemoteListenerManage(w http.ResponseWriter, r *http.Request, targetID identity.AgentID) {
	s.forwardRemoteControlMutation(w, r, targetID, protocol.ControlTypeListenerManage, "listener management", featureOpTagFunc("listener"))
}

// handleDisplayNameManage handles POST /display-name/manage to set/get the agent display name.
func (s *Server) handleDisplayNameManage(w http.ResponseWriter, r *http.Request) {
	if !requirePOST(w, r) {
		return
	}
	if s.displayNameManageProvider == nil {
		http.Error(w, "display name management not configured", http.StatusServiceUnavailable)
		return
	}
	if s.shouldRestrictTopology() {
		http.Error(w, "display name management restricted: management key decryption unavailable", http.StatusForbidden)
		return
	}

	var req struct {
		Action string `json:"action"`
		Name   string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request: " + err.Error()})
		return
	}

	if isMutationAction(req.Action) && !s.requireMutationAuthorization(w) {
		return
	}

	result, err := s.displayNameManageProvider.ManageDisplayName(req.Action, req.Name)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, result)
}

// handleRemoteDisplayNameManage forwards display name management requests to a remote agent.
func (s *Server) handleRemoteDisplayNameManage(w http.ResponseWriter, r *http.Request, targetID identity.AgentID) {
	s.forwardRemoteControlMutation(w, r, targetID, protocol.ControlTypeDisplayNameManage, "display name management", featureOpTagFunc("display_name"))
}

// handleFileBrowse handles POST /agents/{agent-id}/file/browse for directory listing, stat, and roots.
func (s *Server) handleFileBrowse(w http.ResponseWriter, r *http.Request, targetID identity.AgentID) {
	if !requirePOST(w, r) {
		return
	}

	// For remote agents, forward via control channel. The status decoder
	// maps BrowseResponse.ErrorCode back onto 401/403 for auth failures so
	// the frontend surfaces the right status. File browse is privileged:
	// every request must be signed with the management key, but the
	// signature does NOT bind the body hash (matches the file-transfer
	// stream verifier used by upload/download).
	if targetID != s.remoteProvider.ID() {
		s.forwardRemoteControlWithStatus(w, r, targetID, protocol.ControlTypeFileBrowse, "file browse", func(data []byte) int {
			var decoded filetransfer.BrowseResponse
			if err := json.Unmarshal(data, &decoded); err != nil || decoded.ErrorCode == "" {
				return 0
			}
			return fileTransferAuthStatus(decoded.ErrorCode)
		}, func(_ string) string { return crypto.OpFile }, signWithoutBodyHash)
		return
	}

	if s.fileBrowseProvider == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "file browsing not configured"})
		return
	}

	var req filetransfer.BrowseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request: " + err.Error()})
		return
	}

	resp := s.fileBrowseProvider.BrowseFiles(&req)
	if resp.Error != "" {
		writeJSON(w, browseStatusForCode(resp.ErrorCode), resp)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleSleep handles POST /sleep to trigger mesh-wide sleep mode.
func (s *Server) handleSleep(w http.ResponseWriter, r *http.Request) {
	if !requirePOST(w, r) {
		return
	}
	if s.sleepProvider == nil || !s.sleepProvider.IsSleepEnabled() {
		http.Error(w, "sleep mode not enabled", http.StatusServiceUnavailable)
		return
	}

	if err := s.sleepProvider.TriggerSleep(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"status": "error",
			"error":  err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":  "triggered",
		"message": "sleep command triggered",
	})
}

// handleWake handles POST /wake to trigger mesh-wide wake.
func (s *Server) handleWake(w http.ResponseWriter, r *http.Request) {
	if !requirePOST(w, r) {
		return
	}
	if s.sleepProvider == nil || !s.sleepProvider.IsSleepEnabled() {
		http.Error(w, "sleep mode not enabled", http.StatusServiceUnavailable)
		return
	}

	if err := s.sleepProvider.TriggerWake(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"status": "error",
			"error":  err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":  "triggered",
		"message": "wake command triggered",
	})
}

// handleShutdown handles POST /shutdown -- gated by the same mutation
// authorization as peer/route management.
func (s *Server) handleShutdown(w http.ResponseWriter, r *http.Request) {
	if !requirePOST(w, r) {
		return
	}
	if s.stopProvider == nil {
		http.Error(w, "shutdown not configured", http.StatusServiceUnavailable)
		return
	}
	if !s.requireMutationAuthorization(w) {
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":  "shutting-down",
		"message": "agent will exit shortly",
	})

	// Async: calling inline would deadlock since healthServer.Shutdown
	// (invoked downstream by Agent.Stop) waits for this handler to return.
	go s.stopProvider.RequestShutdown()
}

func (s *Server) handleRemoteShutdown(w http.ResponseWriter, r *http.Request, targetID identity.AgentID) {
	s.forwardRemoteControlMutation(w, r, targetID, protocol.ControlTypeStop, "shutdown", stopOpTagFunc)
}

// stopOpTagFunc ignores the action field -- /shutdown has no action vocabulary.
func stopOpTagFunc(_ string) string {
	return crypto.OpStop
}

// handleSleepStatus handles GET /sleep/status to get current sleep status.
func (s *Server) handleSleepStatus(w http.ResponseWriter, r *http.Request) {
	if !requireGET(w, r) {
		return
	}
	if s.sleepProvider == nil {
		// Return default awake status if not configured
		writeJSON(w, http.StatusOK, SleepStatusInfo{
			State:   "AWAKE",
			Enabled: false,
		})
		return
	}

	writeJSON(w, http.StatusOK, s.sleepProvider.GetSleepStatus())
}

// handleTopology handles GET /api/topology for the mesh topology visualization.
func (s *Server) handleTopology(w http.ResponseWriter, r *http.Request) {
	if !requireGET(w, r) {
		return
	}
	if s.remoteProvider == nil {
		http.Error(w, "provider not configured", http.StatusServiceUnavailable)
		return
	}

	localID := s.remoteProvider.ID()
	localName := s.remoteProvider.DisplayName()

	// Get local stats for role determination
	var localStats Stats
	if s.provider != nil {
		localStats = s.provider.Stats()
	}

	socks5Info := s.remoteProvider.GetSOCKS5Info()
	udpInfo := s.remoteProvider.GetUDPInfo()
	forwardInfo := s.remoteProvider.GetPortForwardInfo()

	// Build base local agent info
	localAgent := s.buildLocalAgentInfo(localID, localName, localStats, socks5Info, udpInfo, forwardInfo)

	// If management key encryption is enabled but we can't decrypt,
	// only return local agent info (no peers, routes, or other agents)
	if s.shouldRestrictTopology() {
		writeJSON(w, http.StatusOK, TopologyResponse{
			LocalAgent:  localAgent,
			Agents:      []TopologyAgentInfo{localAgent},
			Connections: []TopologyConnection{},
		})
		return
	}

	// Get all known display names from route advertisements
	displayNames := s.remoteProvider.GetAllDisplayNames()
	allNodeInfo := s.remoteProvider.GetAllNodeInfo()

	// Get route details for determining exit roles
	routeDetails := s.remoteProvider.GetRouteDetails()
	domainRouteDetails := s.remoteProvider.GetDomainRouteDetails()
	forwardRouteDetails := s.remoteProvider.GetPortForwardRouteDetails()

	// Build maps of exit routes, domain routes, and forward endpoints per agent (by origin)
	exitRoutesPerAgent := make(map[string][]string)
	domainRoutesPerAgent := make(map[string][]string)
	forwardEndpointsPerAgent := make(map[string][]string)
	for _, route := range routeDetails {
		originID := route.Origin.String()
		exitRoutesPerAgent[originID] = append(exitRoutesPerAgent[originID], route.Network)
	}
	for _, route := range domainRouteDetails {
		originID := route.Origin.String()
		domainRoutesPerAgent[originID] = append(domainRoutesPerAgent[originID], route.Pattern)
	}
	for _, route := range forwardRouteDetails {
		originID := route.Origin.String()
		forwardEndpointsPerAgent[originID] = append(forwardEndpointsPerAgent[originID], route.Key)
	}

	// Add exit routes to local agent
	if routes, ok := exitRoutesPerAgent[localID.String()]; ok {
		localAgent.ExitRoutes = routes
	}
	if domains, ok := domainRoutesPerAgent[localID.String()]; ok {
		localAgent.DomainRoutes = domains
	}

	// Track all agents and connections
	agentMap := make(map[string]TopologyAgentInfo)
	agentMap[localID.String()] = localAgent

	// Build set of direct peers
	peerIDs := s.remoteProvider.GetPeerIDs()
	peerSet := make(map[identity.AgentID]bool)
	for _, id := range peerIDs {
		peerSet[id] = true
	}

	// Track unique connections (from -> to)
	connectionSet := make(map[string]TopologyConnection)

	// Add direct peer connections
	peerDetails := s.remoteProvider.GetPeerDetails()
	for _, peer := range peerDetails {
		// Add peer to agent map
		displayName := peer.DisplayName
		if displayName == "" {
			displayName = peer.ID.ShortString()
		}
		if _, exists := agentMap[peer.ID.String()]; !exists {
			agentMap[peer.ID.String()] = TopologyAgentInfo{
				ID:          peer.ID.String(),
				ShortID:     peer.ID.ShortString(),
				DisplayName: displayName,
				IsLocal:     false,
				IsConnected: true,
			}
		}
		// Add direct connection
		connKey := localID.ShortString() + "->" + peer.ID.ShortString()
		dialer := peer.ID.ShortString()
		if peer.IsDialer {
			dialer = localID.ShortString()
		}
		connectionSet[connKey] = TopologyConnection{
			FromAgent:    localID.ShortString(),
			ToAgent:      peer.ID.ShortString(),
			IsDirect:     true,
			RTTMs:        peer.RTT.Milliseconds(),
			Unresponsive: peer.RTT.Seconds() > 60,
			Transport:    peer.Transport,
			Dialer:       dialer,
		}
	}

	// Extract agents and connections from route paths
	for _, route := range routeDetails {
		// Add all agents in the path
		for _, agentID := range route.Path {
			if _, exists := agentMap[agentID.String()]; !exists {
				// Look up display name from route advertisements
				displayName := displayNames[agentID]
				if displayName == "" {
					displayName = agentID.ShortString()
				}
				agentMap[agentID.String()] = TopologyAgentInfo{
					ID:          agentID.String(),
					ShortID:     agentID.ShortString(),
					DisplayName: displayName,
					IsLocal:     false,
					IsConnected: peerSet[agentID],
				}
			}
		}

		// Build connections between consecutive agents in path
		// Path is ordered from local toward origin
		for i := 0; i < len(route.Path)-1; i++ {
			fromID := route.Path[i]
			toID := route.Path[i+1]
			connKey := fromID.ShortString() + "->" + toID.ShortString()
			if _, exists := connectionSet[connKey]; !exists {
				conn := TopologyConnection{
					FromAgent: fromID.ShortString(),
					ToAgent:   toID.ShortString(),
					IsDirect:  false,
				}
				// Try to find transport/RTT/dialer from the "from" agent's peer info
				if fromNodeInfo, ok := allNodeInfo[fromID]; ok {
					for _, peer := range fromNodeInfo.Peers {
						var peerAgentID identity.AgentID
						copy(peerAgentID[:], peer.PeerID[:])
						if peerAgentID == toID {
							conn.Transport = peer.Transport
							conn.RTTMs = peer.RTTMs
							conn.Unresponsive = peer.RTTMs > 60000 // 60 seconds in ms
							conn.Dialer = toID.ShortString()
							if peer.IsDialer {
								conn.Dialer = fromID.ShortString()
							}
							break
						}
					}
				}
				connectionSet[connKey] = conn
			}
		}
	}

	// Add all agents from NODE_INFO advertisements to the topology
	// This ensures we see all known agents, not just those with routes
	for agentID, nodeInfo := range allNodeInfo {
		if _, exists := agentMap[agentID.String()]; !exists {
			displayName := displayNames[agentID]
			if displayName == "" {
				displayName = agentID.ShortString()
			}
			agentMap[agentID.String()] = TopologyAgentInfo{
				ID:          agentID.String(),
				ShortID:     agentID.ShortString(),
				DisplayName: displayName,
				IsLocal:     false,
				IsConnected: peerSet[agentID],
			}
		}

		// Add connections from this agent's peer list
		if nodeInfo != nil {
			for _, peer := range nodeInfo.Peers {
				var peerAgentID identity.AgentID
				copy(peerAgentID[:], peer.PeerID[:])

				// Add the peer agent if not already in the map
				if _, exists := agentMap[peerAgentID.String()]; !exists {
					peerDisplayName := displayNames[peerAgentID]
					if peerDisplayName == "" {
						peerDisplayName = peerAgentID.ShortString()
					}
					agentMap[peerAgentID.String()] = TopologyAgentInfo{
						ID:          peerAgentID.String(),
						ShortID:     peerAgentID.ShortString(),
						DisplayName: peerDisplayName,
						IsLocal:     false,
						IsConnected: peerSet[peerAgentID],
					}
				}

				// Add the connection if not already tracked
				connKey := agentID.ShortString() + "->" + peerAgentID.ShortString()
				reverseKey := peerAgentID.ShortString() + "->" + agentID.ShortString()
				if _, exists := connectionSet[connKey]; !exists {
					if _, reverseExists := connectionSet[reverseKey]; !reverseExists {
						dialer := peerAgentID.ShortString()
						if peer.IsDialer {
							dialer = agentID.ShortString()
						}
						connectionSet[connKey] = TopologyConnection{
							FromAgent:    agentID.ShortString(),
							ToAgent:      peerAgentID.ShortString(),
							IsDirect:     false,
							Transport:    peer.Transport,
							RTTMs:        peer.RTTMs,
							Unresponsive: peer.RTTMs > 60000,
							Dialer:       dialer,
						}
					}
				}
			}
		}
	}

	// Populate node info for all agents in the map
	for agentID, nodeInfo := range allNodeInfo {
		if existing, ok := agentMap[agentID.String()]; ok {
			populateNodeInfo(&existing, nodeInfo)
			agentMap[agentID.String()] = existing
		}
	}

	// Populate exit routes, domain routes, and forward endpoints for all agents, and determine roles
	for agentID, existing := range agentMap {
		if existing.IsLocal {
			continue // Already handled above
		}

		// Check if agent has exit routes (makes it an exit node)
		hasExitRoutes := false
		if routes, ok := exitRoutesPerAgent[agentID]; ok {
			existing.ExitRoutes = routes
			hasExitRoutes = true
		}
		if domains, ok := domainRoutesPerAgent[agentID]; ok {
			existing.DomainRoutes = domains
			hasExitRoutes = true
		}

		// Check if agent has forward endpoints (makes it a forward_exit node)
		hasForwardEndpoints := false
		if endpoints, ok := forwardEndpointsPerAgent[agentID]; ok {
			existing.ForwardEndpoints = endpoints
			hasForwardEndpoints = true
		}

		// Build roles for remote agents.
		// SOCKS5 advertisement is still a real protocol gap.
		hasForwardListeners := len(existing.ForwardListeners) > 0
		existing.Roles = s.buildAgentRoles(false, false, hasExitRoutes, hasForwardListeners, hasForwardEndpoints)

		agentMap[agentID] = existing
	}

	// Convert maps to slices
	agents := make([]TopologyAgentInfo, 0, len(agentMap))
	for _, agent := range agentMap {
		agents = append(agents, agent)
	}

	connections := make([]TopologyConnection, 0, len(connectionSet))
	for _, conn := range connectionSet {
		connections = append(connections, conn)
	}

	writeJSON(w, http.StatusOK, TopologyResponse{
		LocalAgent:  localAgent,
		Agents:      agents,
		Connections: connections,
	})
}

// buildAgentRoles constructs the roles array based on agent capabilities.
// All agents can act as transit, but we only include it if they're not ingress or exit.
func (s *Server) buildAgentRoles(isLocal bool, hasSOCKS5 bool, hasExitRoutes bool, hasForwardListeners bool, hasForwardEndpoints bool) []string {
	var roles []string
	if hasSOCKS5 {
		roles = append(roles, "ingress")
	}
	if hasExitRoutes {
		roles = append(roles, "exit")
	}
	if hasForwardListeners {
		roles = append(roles, "forward_ingress")
	}
	if hasForwardEndpoints {
		roles = append(roles, "forward_exit")
	}
	// If no specific roles, mark as transit
	if len(roles) == 0 {
		roles = append(roles, "transit")
	}
	return roles
}

// handleDashboard handles GET /api/dashboard for the dashboard overview.
func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	if !requireGET(w, r) {
		return
	}
	resp, err := s.BuildDashboardSnapshot()
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// BuildDashboardSnapshotJSON returns the dashboard snapshot marshaled to JSON.
// It is used by the SOCKS5 route-watch path (CmdRouteWatch / CmdSnapshot) so
// pushed snapshots carry the same bytes a client would get from
// GET /api/dashboard.
func (s *Server) BuildDashboardSnapshotJSON() ([]byte, error) {
	resp, err := s.BuildDashboardSnapshot()
	if err != nil {
		return nil, err
	}
	return json.Marshal(resp)
}

// BuildDashboardSnapshot assembles the dashboard state served by
// GET /api/dashboard. It is the single source of truth for both the HTTP
// handler and the SOCKS5 route-watch push path (CmdRouteWatch / CmdSnapshot),
// so the two can never drift. It returns an error when the required
// providers are not configured.
func (s *Server) BuildDashboardSnapshot() (DashboardResponse, error) {
	if s.remoteProvider == nil || s.provider == nil {
		return DashboardResponse{}, fmt.Errorf("provider not configured")
	}

	localID := s.remoteProvider.ID()
	localName := s.remoteProvider.DisplayName()
	stats := s.provider.Stats()

	localAgentInfo := TopologyAgentInfo{
		ID:          localID.String(),
		ShortID:     localID.ShortString(),
		DisplayName: localName,
		IsLocal:     true,
		IsConnected: true,
	}

	// If management key encryption is enabled but we can't decrypt,
	// only return local agent info and stats (no peers or routes)
	if s.shouldRestrictTopology() {
		return DashboardResponse{
			Agent:  localAgentInfo,
			Stats:  stats,
			Peers:  []DashboardPeerInfo{},
			Routes: []DashboardRouteInfo{},
		}, nil
	}

	// Build peer info
	peers := make([]DashboardPeerInfo, 0, len(s.remoteProvider.GetPeerDetails()))
	for _, peer := range s.remoteProvider.GetPeerDetails() {
		peers = append(peers, DashboardPeerInfo{
			ID:           peer.ID.String(),
			ShortID:      peer.ID.ShortString(),
			DisplayName:  peer.DisplayName,
			State:        peer.State,
			RTTMs:        peer.RTT.Milliseconds(),
			Unresponsive: peer.RTT.Seconds() > 60,
			IsDialer:     peer.IsDialer,
		})
	}

	// Get display names for building path display
	displayNames := s.remoteProvider.GetAllDisplayNames()
	allNodeInfo := s.remoteProvider.GetAllNodeInfo()

	// Helper to get display name or fall back to short ID
	getDisplayName := func(id identity.AgentID) string {
		if id == localID {
			return localName
		}
		if name, ok := displayNames[id]; ok && name != "" {
			return name
		}
		return id.ShortString()
	}

	// Helper to get UDP info for an agent
	getUDPEnabled := func(id identity.AgentID) bool {
		info, ok := allNodeInfo[id]
		return ok && info.UDPEnabled
	}

	// Helper to build path display and IDs from agent path
	buildPath := func(path []identity.AgentID) (pathDisplay, pathIDs []string) {
		pathDisplay = []string{localName}
		pathIDs = []string{localID.ShortString()}
		for _, agentID := range path {
			pathDisplay = append(pathDisplay, getDisplayName(agentID))
			pathIDs = append(pathIDs, agentID.ShortString())
		}
		return pathDisplay, pathIDs
	}

	// Build route info (CIDR and domain routes)
	routeDetails := s.remoteProvider.GetRouteDetails()
	domainRouteDetails := s.remoteProvider.GetDomainRouteDetails()
	routes := make([]DashboardRouteInfo, 0, len(routeDetails)+len(domainRouteDetails))

	for _, route := range routeDetails {
		pathDisplay, pathIDs := buildPath(route.Path)
		routes = append(routes, DashboardRouteInfo{
			Network:     route.Network,
			RouteType:   "cidr",
			Origin:      getDisplayName(route.Origin),
			OriginID:    route.Origin.ShortString(),
			HopCount:    route.HopCount,
			PathDisplay: pathDisplay,
			PathIDs:     pathIDs,
			TCP:         true,
			UDP:         getUDPEnabled(route.Origin),
			Comment:     route.Comment,
			SourceIP:    route.SourceIP,
			Interface:   route.Interface,
		})
	}

	for _, route := range domainRouteDetails {
		pathDisplay, pathIDs := buildPath(route.Path)
		routes = append(routes, DashboardRouteInfo{
			Network:     route.Pattern,
			RouteType:   "domain",
			Origin:      getDisplayName(route.Origin),
			OriginID:    route.Origin.ShortString(),
			HopCount:    route.HopCount,
			PathDisplay: pathDisplay,
			PathIDs:     pathIDs,
			TCP:         true,
			UDP:         getUDPEnabled(route.Origin),
		})
	}

	// Sort routes: CIDR first (by network), then domains (by pattern)
	sort.Slice(routes, func(i, j int) bool {
		if routes[i].RouteType != routes[j].RouteType {
			return routes[i].RouteType == "cidr"
		}
		if routes[i].Network != routes[j].Network {
			return routes[i].Network < routes[j].Network
		}
		return routes[i].OriginID < routes[j].OriginID
	})

	// Build legacy domain routes for backward compatibility
	domainRoutes := make([]DashboardDomainRouteInfo, 0, len(domainRouteDetails))
	for _, route := range domainRouteDetails {
		pathDisplay, pathIDs := buildPath(route.Path)
		domainRoutes = append(domainRoutes, DashboardDomainRouteInfo{
			Pattern:     route.Pattern,
			IsWildcard:  route.IsWildcard,
			Origin:      getDisplayName(route.Origin),
			OriginID:    route.Origin.ShortString(),
			HopCount:    route.HopCount,
			PathDisplay: pathDisplay,
			PathIDs:     pathIDs,
			TCP:         true,
			UDP:         getUDPEnabled(route.Origin),
		})
	}

	// Sort domain routes deterministically by pattern, then by origin
	sort.Slice(domainRoutes, func(i, j int) bool {
		if domainRoutes[i].Pattern != domainRoutes[j].Pattern {
			return domainRoutes[i].Pattern < domainRoutes[j].Pattern
		}
		return domainRoutes[i].OriginID < domainRoutes[j].OriginID
	})

	// Build port forward routes as ingress-exit pairs
	forwardRouteDetails := s.remoteProvider.GetPortForwardRouteDetails()

	// Build listener map from node info: key -> list of (agentID, address)
	type listenerInfo struct {
		agentID identity.AgentID
		address string
	}
	listenersByKey := make(map[string][]listenerInfo)
	for agentID, nodeInfo := range allNodeInfo {
		// AnnounceLocalNodeInfo stores the local agent's info in the
		// routing manager, so allNodeInfo already contains localID.
		// Skip it here and rely on the explicit GetLocalNodeInfo() block
		// below to add local listeners exactly once.
		if agentID == localID {
			continue
		}
		if nodeInfo == nil {
			continue
		}
		for _, fl := range nodeInfo.ForwardListeners {
			listenersByKey[fl.Key] = append(listenersByKey[fl.Key], listenerInfo{
				agentID: agentID,
				address: fl.Address,
			})
		}
	}
	// Also check local node info
	localNodeInfo := s.remoteProvider.GetLocalNodeInfo()
	if localNodeInfo != nil {
		for _, fl := range localNodeInfo.ForwardListeners {
			listenersByKey[fl.Key] = append(listenersByKey[fl.Key], listenerInfo{
				agentID: localID,
				address: fl.Address,
			})
		}
	}

	// Build adjacency graph for hop calculation from node info peer connections
	adj := make(map[identity.AgentID]map[identity.AgentID]bool)
	addEdge := func(a, b identity.AgentID) {
		if adj[a] == nil {
			adj[a] = make(map[identity.AgentID]bool)
		}
		if adj[b] == nil {
			adj[b] = make(map[identity.AgentID]bool)
		}
		adj[a][b] = true
		adj[b][a] = true
	}
	for agentID, nodeInfo := range allNodeInfo {
		// Same reasoning as the listener loop above: the explicit
		// localNodeInfo block is authoritative for local edges.
		if agentID == localID {
			continue
		}
		if nodeInfo == nil {
			continue
		}
		for _, peer := range nodeInfo.Peers {
			addEdge(agentID, identity.AgentID(peer.PeerID))
		}
	}
	if localNodeInfo != nil {
		for _, peer := range localNodeInfo.Peers {
			addEdge(localID, identity.AgentID(peer.PeerID))
		}
	}

	// BFS to compute hops between two agents
	computeHops := func(from, to identity.AgentID) int {
		if from == to {
			return 0
		}
		visited := make(map[identity.AgentID]bool)
		queue := []struct {
			id   identity.AgentID
			hops int
		}{{from, 0}}
		visited[from] = true
		for len(queue) > 0 {
			curr := queue[0]
			queue = queue[1:]
			for neighbor := range adj[curr.id] {
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

	// Group exit routes by key
	exitsByKey := make(map[string][]PortForwardRouteDetails)
	for _, route := range forwardRouteDetails {
		exitsByKey[route.Key] = append(exitsByKey[route.Key], route)
	}

	// Build cross-product of listeners x exits for each key
	var forwardRoutes []DashboardPortForwardRouteInfo
	for key, exits := range exitsByKey {
		listeners := listenersByKey[key]
		for _, exit := range exits {
			for _, listener := range listeners {
				hops := computeHops(listener.agentID, exit.Origin)
				if hops < 0 {
					hops = 0 // Fallback if no path found
				}
				forwardRoutes = append(forwardRoutes, DashboardPortForwardRouteInfo{
					Key:             key,
					IngressAgent:    getDisplayName(listener.agentID),
					IngressAgentID:  listener.agentID.ShortString(),
					ListenerAddress: listener.address,
					ExitAgent:       getDisplayName(exit.Origin),
					ExitAgentID:     exit.Origin.ShortString(),
					Target:          exit.Target,
					HopCount:        hops,
					PathDisplay:     []string{getDisplayName(listener.agentID), getDisplayName(exit.Origin)},
					PathIDs:         []string{listener.agentID.ShortString(), exit.Origin.ShortString()},
				})
			}
		}
	}

	// Sort port forward routes by key, then by ingress, then by exit
	sort.Slice(forwardRoutes, func(i, j int) bool {
		if forwardRoutes[i].Key != forwardRoutes[j].Key {
			return forwardRoutes[i].Key < forwardRoutes[j].Key
		}
		if forwardRoutes[i].IngressAgentID != forwardRoutes[j].IngressAgentID {
			return forwardRoutes[i].IngressAgentID < forwardRoutes[j].IngressAgentID
		}
		return forwardRoutes[i].ExitAgentID < forwardRoutes[j].ExitAgentID
	})

	var meshHosts []hosts.Host
	var meshHostsSuffixes []string
	if s.meshHostProvider != nil {
		meshHosts = s.meshHostProvider.MeshHosts()
		meshHostsSuffixes = s.meshHostProvider.MeshHostsSuffixes()
	}

	return DashboardResponse{
		Agent:             localAgentInfo,
		Stats:             stats,
		Peers:             peers,
		Routes:            routes,
		DomainRoutes:      domainRoutes,
		ForwardRoutes:     forwardRoutes,
		MeshHosts:         meshHosts,
		MeshHostsSuffixes: meshHostsSuffixes,
	}, nil
}

// NodesResponse is the response for the /api/nodes endpoint.
type NodesResponse struct {
	Nodes []TopologyAgentInfo `json:"nodes"`
}

// handleNodes handles GET /api/nodes for detailed node info listing.
func (s *Server) handleNodes(w http.ResponseWriter, r *http.Request) {
	if !requireGET(w, r) {
		return
	}
	if s.remoteProvider == nil {
		http.Error(w, "provider not configured", http.StatusServiceUnavailable)
		return
	}

	localID := s.remoteProvider.ID()
	localName := s.remoteProvider.DisplayName()

	// Build local node info
	localNode := TopologyAgentInfo{
		ID:          localID.String(),
		ShortID:     localID.ShortString(),
		DisplayName: localName,
		IsLocal:     true,
		IsConnected: true,
	}
	populateNodeInfo(&localNode, s.remoteProvider.GetLocalNodeInfo())

	// If management key encryption is enabled but we can't decrypt,
	// only return local node info
	if s.shouldRestrictTopology() {
		writeJSON(w, http.StatusOK, NodesResponse{Nodes: []TopologyAgentInfo{localNode}})
		return
	}

	// Get all known node info
	allNodeInfo := s.remoteProvider.GetAllNodeInfo()

	// Build set of direct peers
	peerIDs := s.remoteProvider.GetPeerIDs()
	peerSet := make(map[identity.AgentID]bool)
	for _, id := range peerIDs {
		peerSet[id] = true
	}

	// Build list of all nodes, starting with local
	nodes := []TopologyAgentInfo{localNode}

	// Add all known remote nodes
	for agentID, nodeInfo := range allNodeInfo {
		if agentID == localID {
			continue
		}

		displayName := nodeInfo.DisplayName
		if displayName == "" {
			displayName = agentID.ShortString()
		}

		node := TopologyAgentInfo{
			ID:          agentID.String(),
			ShortID:     agentID.ShortString(),
			DisplayName: displayName,
			IsLocal:     false,
			IsConnected: peerSet[agentID],
		}
		populateNodeInfo(&node, nodeInfo)
		nodes = append(nodes, node)
	}

	writeJSON(w, http.StatusOK, NodesResponse{Nodes: nodes})
}
