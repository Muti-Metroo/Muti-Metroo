// Package config provides configuration parsing and validation for Muti Metroo.
package config

import (
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/postalsys/muti-metroo/internal/embed"
	"github.com/postalsys/muti-metroo/internal/hosts"
	"github.com/postalsys/muti-metroo/internal/protocol"
	"github.com/postalsys/muti-metroo/internal/sysinfo"
	"gopkg.in/yaml.v3"
)

// getPEM returns inline PEM content if set, otherwise reads from file path.
// Returns nil if neither is configured.
func getPEM(inline, filePath string) ([]byte, error) {
	if inline != "" {
		return []byte(inline), nil
	}
	if filePath != "" {
		return os.ReadFile(filePath)
	}
	return nil, nil
}

// isOneOf returns true if value matches any of the allowed values.
func isOneOf(value string, allowed ...string) bool {
	for _, a := range allowed {
		if value == a {
			return true
		}
	}
	return false
}

// parseHexKey decodes a hex-encoded key and validates its length.
func parseHexKey(hexStr, keyName string, expectedSize int) ([KeySize]byte, error) {
	var key [KeySize]byte
	if hexStr == "" {
		return key, fmt.Errorf("%s not configured", keyName)
	}

	decoded, err := hex.DecodeString(hexStr)
	if err != nil {
		return key, fmt.Errorf("invalid %s hex: %w", keyName, err)
	}

	if len(decoded) != expectedSize {
		return key, fmt.Errorf("%s must be %d bytes, got %d", keyName, expectedSize, len(decoded))
	}

	copy(key[:], decoded)
	return key, nil
}

// Config represents the complete agent configuration.
type Config struct {
	// DefaultAction controls what a binary with embedded config does when
	// invoked with no arguments. The implicit default is "run" -- the agent
	// auto-starts so a single-file deployment "just works". Set to "help" to
	// opt out and show usage instead. Has no effect when no config is embedded.
	DefaultAction string             `yaml:"default_action,omitempty"`
	Agent         AgentConfig        `yaml:"agent"`
	Protocol      ProtocolConfig     `yaml:"protocol,omitempty"`
	TLS           GlobalTLSConfig    `yaml:"tls,omitempty"`
	Management    ManagementConfig   `yaml:"management,omitempty"`
	Listeners     []ListenerConfig   `yaml:"listeners,omitempty"`
	Peers         []PeerConfig       `yaml:"peers,omitempty"`
	SOCKS5        SOCKS5Config       `yaml:"socks5,omitempty"`
	Exit          ExitConfig         `yaml:"exit,omitempty"`
	Routing       RoutingConfig      `yaml:"routing,omitempty"`
	Connections   ConnectionsConfig  `yaml:"connections,omitempty"`
	Limits        LimitsConfig       `yaml:"limits,omitempty"`
	HTTP          HTTPConfig         `yaml:"http,omitempty"`
	FileTransfer  FileTransferConfig `yaml:"file_transfer,omitempty"`
	Shell         ShellConfig        `yaml:"shell,omitempty"`
	UDP           UDPConfig          `yaml:"udp,omitempty"`
	ICMP          ICMPConfig         `yaml:"icmp,omitempty"`
	Forward       ForwardConfig      `yaml:"forward,omitempty"`
	Sleep         SleepConfig        `yaml:"sleep,omitempty"`

	// MeshHostsSuffixes is the set of DNS suffixes any mesh host name may
	// end with (without leading dot). Defaults to ["mesh"] when empty.
	// Operators can also add or remove suffixes at runtime via the
	// /hosts/suffixes/manage API; runtime mutations are journalled so they
	// survive restarts and may even drop a suffix that was originally
	// seeded here.
	MeshHostsSuffixes []string `yaml:"mesh_hosts_suffixes,omitempty"`
	// LegacyMeshHostsSuffix catches configs that still use the old
	// singular `mesh_hosts_suffix:` field. yaml.Unmarshal silently drops
	// unknown keys, so without this we would lose the value on rename.
	// Validation rejects any config that sets it, telling the operator
	// to migrate to mesh_hosts_suffixes.
	LegacyMeshHostsSuffix string `yaml:"mesh_hosts_suffix,omitempty"`
	// MeshHosts is the static seed of mesh-local hostname -> IP entries
	// served to Mutiauk clients via /api/dashboard. Runtime entries
	// added via /hosts/manage live alongside these.
	MeshHosts []hosts.Host `yaml:"mesh_hosts,omitempty"`
}

// ProtocolConfig defines protocol identifiers used for transport negotiation.
// These can be customized to match local network policy or middlebox expectations.
type ProtocolConfig struct {
	// ALPN is the Application-Layer Protocol Negotiation identifier.
	// Used for QUIC and TLS connections. Default: "muti-metroo/1"
	// Set to empty string "" to use no custom ALPN (uses transport defaults like "h2").
	ALPN string `yaml:"alpn,omitempty"`

	// HTTPHeader is the custom header name for HTTP/2 transport protocol identification.
	// Default: "X-Muti-Metroo-Protocol". Set to empty string "" to disable custom header.
	HTTPHeader string `yaml:"http_header,omitempty"`

	// WSSubprotocol is the WebSocket subprotocol identifier.
	// Default: "muti-metroo/1". Set to empty string "" to disable subprotocol negotiation.
	WSSubprotocol string `yaml:"ws_subprotocol,omitempty"`
}

// GlobalTLSConfig defines global TLS settings shared across all connections.
// The CA is used for both verifying peer certificates and client certificate
// verification when mTLS is enabled on listeners.
//
// By default, TLS certificate verification is disabled (Strict: false) because
// Muti Metroo uses an additional E2E encryption layer (X25519 + ChaCha20-Poly1305)
// that provides confidentiality and integrity regardless of transport security.
// Self-signed certificates are auto-generated if none are configured.
type GlobalTLSConfig struct {
	// CA certificate for verifying peer certificates and client certs (mTLS)
	CA    string `yaml:"ca,omitempty"`     // CA certificate file path
	CAPEM string `yaml:"ca_pem,omitempty"` // CA certificate PEM content (takes precedence)

	// Agent's identity certificate used for listeners and peer connections
	// If not configured, a self-signed certificate is auto-generated at startup
	Cert    string `yaml:"cert,omitempty"`     // Certificate file path
	Key     string `yaml:"key,omitempty"`      // Private key file path
	CertPEM string `yaml:"cert_pem,omitempty"` // Certificate PEM content (takes precedence)
	KeyPEM  string `yaml:"key_pem,omitempty"`  // Private key PEM content (takes precedence)

	// MTLS enables mutual TLS on listeners (require client certificates)
	// Requires CA to be configured
	MTLS bool `yaml:"mtls,omitempty"`

	// Strict enables TLS certificate verification (default: false)
	// When false (default), peer certificates are not validated, which is safe
	// because the E2E layer provides security. When true, peer certificates
	// must be signed by the configured CA.
	Strict bool `yaml:"strict,omitempty"`

	// Fingerprint configures TLS ClientHello profiles (client-side only).
	// This uses uTLS to present a browser-compatible ClientHello on outbound
	// dials, improving compatibility with strict proxies and servers. Default
	// is disabled -- operators must opt in. Note: some WAFs treat a
	// browser-like ClientHello + non-browser HTTP as a bot signal and may
	// reject dials, so this knob can break peer connectivity in front of
	// profile-strict middleboxes.
	Fingerprint FingerprintConfig `yaml:"fingerprint,omitempty"`
}

// FingerprintConfig configures TLS ClientHello profiles for client connections.
// This uses uTLS to present browser-like TLS ClientHello messages.
type FingerprintConfig struct {
	// Preset selects a predefined ClientHello profile.
	// Valid values:
	//   - "chrome": Chrome-like ClientHello
	//   - "firefox": Firefox-like ClientHello
	//   - "safari": Safari-like ClientHello
	//   - "edge": Microsoft Edge-like ClientHello
	//   - "ios": iOS Safari-like ClientHello
	//   - "android": Android Chrome-like ClientHello
	//   - "random": Randomized ClientHello per connection
	//   - "go": Go standard library ClientHello (default behavior)
	//   - "disabled" or "" (default): Use Go standard library TLS
	Preset string `yaml:"preset,omitempty"`
}

// GetCAPEM returns the CA certificate PEM content, reading from file if necessary.
func (g *GlobalTLSConfig) GetCAPEM() ([]byte, error) {
	return getPEM(g.CAPEM, g.CA)
}

// GetCertPEM returns the certificate PEM content, reading from file if necessary.
func (g *GlobalTLSConfig) GetCertPEM() ([]byte, error) {
	return getPEM(g.CertPEM, g.Cert)
}

// GetKeyPEM returns the private key PEM content, reading from file if necessary.
func (g *GlobalTLSConfig) GetKeyPEM() ([]byte, error) {
	return getPEM(g.KeyPEM, g.Key)
}

// HasCA returns true if CA certificate is configured (either file or PEM).
func (g *GlobalTLSConfig) HasCA() bool {
	return g.CA != "" || g.CAPEM != ""
}

// HasCert returns true if certificate is configured (either file or PEM).
func (g *GlobalTLSConfig) HasCert() bool {
	return g.Cert != "" || g.CertPEM != ""
}

// HasKey returns true if private key is configured (either file or PEM).
func (g *GlobalTLSConfig) HasKey() bool {
	return g.Key != "" || g.KeyPEM != ""
}

// ManagementConfig configures management key encryption for mesh metadata.
// When enabled, NodeInfo and route paths are encrypted so only operators
// with the private key can view topology details. This protects against
// blue team discovery of mesh topology from compromised agents.
type ManagementConfig struct {
	// PublicKey is the management public key (hex-encoded, 64 characters).
	// When set, NodeInfo and route paths are encrypted before flooding.
	// All agents in the mesh should have the same public key.
	PublicKey string `yaml:"public_key,omitempty"`

	// PrivateKey is the management private key (hex-encoded, 64 characters).
	// Only set on operator/management nodes that need to view topology.
	// NEVER distribute to field agents.
	PrivateKey string `yaml:"private_key,omitempty"`

	// SigningPublicKey is the Ed25519 public key for verifying signed commands
	// (hex-encoded, 64 characters = 32 bytes).
	// When set, sleep/wake commands must be signed with the corresponding private key.
	// Add to ALL agents to require command authentication.
	SigningPublicKey string `yaml:"signing_public_key,omitempty"`

	// SigningPrivateKey is the Ed25519 private key for signing commands
	// (hex-encoded, 128 characters = 64 bytes).
	// Only set on operator nodes that need to issue sleep/wake commands.
	// NEVER distribute to field agents.
	SigningPrivateKey string `yaml:"signing_private_key,omitempty"`
}

// KeySize is the size of X25519 keys in bytes.
const KeySize = 32

// Ed25519 key sizes
const (
	// SigningPublicKeySize is the size of Ed25519 public keys in bytes.
	SigningPublicKeySize = 32

	// SigningPrivateKeySize is the size of Ed25519 private keys in bytes.
	SigningPrivateKeySize = 64
)

// HasManagementKey returns true if management encryption is configured.
func (c *Config) HasManagementKey() bool {
	return c.Management.PublicKey != ""
}

// GetManagementPublicKey returns the parsed management public key.
// Returns an error if the key is not configured or invalid.
func (c *Config) GetManagementPublicKey() ([KeySize]byte, error) {
	return parseHexKey(c.Management.PublicKey, "management public key", KeySize)
}

// GetManagementPrivateKey returns the parsed management private key.
// Returns an error if the key is not configured or invalid.
func (c *Config) GetManagementPrivateKey() ([KeySize]byte, error) {
	return parseHexKey(c.Management.PrivateKey, "management private key", KeySize)
}

// CanDecryptManagement returns true if management private key is configured.
func (c *Config) CanDecryptManagement() bool {
	return c.Management.PrivateKey != ""
}

// HasSigningKey returns true if command signing verification is configured.
// When true, sleep/wake commands must be signed with the corresponding private key.
func (c *Config) HasSigningKey() bool {
	return c.Management.SigningPublicKey != ""
}

// CanSign returns true if command signing is configured (has private key).
// Only operators with the signing private key can issue signed commands.
func (c *Config) CanSign() bool {
	return c.Management.SigningPrivateKey != ""
}

// GetSigningPublicKey returns the parsed Ed25519 signing public key.
// Returns an error if the key is not configured or invalid.
func (c *Config) GetSigningPublicKey() ([SigningPublicKeySize]byte, error) {
	var key [SigningPublicKeySize]byte
	if c.Management.SigningPublicKey == "" {
		return key, fmt.Errorf("signing public key not configured")
	}

	decoded, err := hex.DecodeString(c.Management.SigningPublicKey)
	if err != nil {
		return key, fmt.Errorf("invalid signing public key hex: %w", err)
	}

	if len(decoded) != SigningPublicKeySize {
		return key, fmt.Errorf("signing public key must be %d bytes, got %d", SigningPublicKeySize, len(decoded))
	}

	copy(key[:], decoded)
	return key, nil
}

// GetSigningPrivateKey returns the parsed Ed25519 signing private key.
// Returns an error if the key is not configured or invalid.
func (c *Config) GetSigningPrivateKey() ([SigningPrivateKeySize]byte, error) {
	var key [SigningPrivateKeySize]byte
	if c.Management.SigningPrivateKey == "" {
		return key, fmt.Errorf("signing private key not configured")
	}

	decoded, err := hex.DecodeString(c.Management.SigningPrivateKey)
	if err != nil {
		return key, fmt.Errorf("invalid signing private key hex: %w", err)
	}

	if len(decoded) != SigningPrivateKeySize {
		return key, fmt.Errorf("signing private key must be %d bytes, got %d", SigningPrivateKeySize, len(decoded))
	}

	copy(key[:], decoded)
	return key, nil
}

// AgentConfig contains agent identity settings.
type AgentConfig struct {
	ID          string `yaml:"id,omitempty"`           // "auto" or hex string
	DisplayName string `yaml:"display_name,omitempty"` // Human-readable name (Unicode allowed)
	DataDir     string `yaml:"data_dir,omitempty"`     // Directory for persistent state (optional with identity in config)
	LogLevel    string `yaml:"log_level,omitempty"`    // debug, info, warn, error
	LogFormat   string `yaml:"log_format,omitempty"`   // text, json

	// StartupDelay delays all network activity (listeners, peers, SOCKS5, etc.)
	// for the specified duration after the process starts. Useful for staggering
	// agent startups or waiting for dependencies. Default: 0 (no delay).
	StartupDelay time.Duration `yaml:"startup_delay,omitempty"`

	// X25519 keypair for E2E encryption (optional - enables single-file deployment)
	// When specified, takes precedence over data_dir files, making data_dir optional.
	// Generate with: muti-metroo init, then copy values from agent_key file.
	PrivateKey string `yaml:"private_key,omitempty"` // 64-char hex string (32 bytes)
	PublicKey  string `yaml:"public_key,omitempty"`  // Optional - derived from private_key if not specified
}

// HasIdentityKeypair returns true if the identity private key is configured in config.
func (a *AgentConfig) HasIdentityKeypair() bool {
	return a.PrivateKey != ""
}

// GetIdentityPrivateKey returns the parsed identity private key.
// Returns an error if the key is not configured or invalid.
func (a *AgentConfig) GetIdentityPrivateKey() ([KeySize]byte, error) {
	return parseHexKey(a.PrivateKey, "agent private key", KeySize)
}

// GetIdentityPublicKey returns the parsed identity public key if configured.
// Returns the key, a boolean indicating if it was configured, and any error.
func (a *AgentConfig) GetIdentityPublicKey() ([KeySize]byte, bool, error) {
	if a.PublicKey == "" {
		return [KeySize]byte{}, false, nil
	}
	key, err := parseHexKey(a.PublicKey, "agent public key", KeySize)
	return key, true, err
}

// ListenerConfig defines a transport listener.
type ListenerConfig struct {
	Transport string           `yaml:"transport"`           // quic, h2, ws (required)
	Address   string           `yaml:"address"`             // listen address (required)
	Path      string           `yaml:"path,omitempty"`      // HTTP path for h2/ws
	PlainText bool             `yaml:"plaintext,omitempty"` // Allow plain WebSocket without TLS (for reverse proxy)
	TLS       TLSConfig        `yaml:"tls,omitempty"`
	Advertise *AdvertiseConfig `yaml:"advertise,omitempty"` // Optional: public dial target advertised to mesh
}

// AdvertiseConfig declares how a listener should be advertised to the mesh
// so new agents can dial it directly. Optional; absent block = not advertised.
type AdvertiseConfig struct {
	Address   string `yaml:"address"`             // host:port (required when block is present)
	Path      string `yaml:"path,omitempty"`      // h2/ws only; defaults to listener.Path
	Transport string `yaml:"transport,omitempty"` // defaults to listener.Transport
}

// PeerConfig defines a peer connection.
type PeerConfig struct {
	ID        string    `yaml:"id,omitempty"`         // Expected peer AgentID
	Transport string    `yaml:"transport"`            // quic, h2, ws (required)
	Address   string    `yaml:"address"`              // peer address (required)
	Path      string    `yaml:"path,omitempty"`       // HTTP path for h2/ws
	Proxy     string    `yaml:"proxy,omitempty"`      // HTTP proxy for ws
	ProxyAuth ProxyAuth `yaml:"proxy_auth,omitempty"` // Proxy authentication
	TLS       TLSConfig `yaml:"tls,omitempty"`

	// TCPKeepAlive overrides connections.tcp_keepalive for this peer's
	// TCP-based transports (h2, ws). Pointer distinguishes "not set" (inherit
	// global) from 0 (disabled). Ignored for QUIC, which has its own keepalive.
	TCPKeepAlive *time.Duration `yaml:"tcp_keepalive,omitempty"`
}

// TLSConfig defines per-connection TLS settings that can override global settings.
// For each certificate/key, you can specify either a file path or inline PEM content.
// If both are provided, inline PEM takes precedence.
type TLSConfig struct {
	// Override global cert/key (optional - uses global if not set)
	Cert    string `yaml:"cert,omitempty"`     // Certificate file path
	Key     string `yaml:"key,omitempty"`      // Private key file path
	CertPEM string `yaml:"cert_pem,omitempty"` // Certificate PEM content
	KeyPEM  string `yaml:"key_pem,omitempty"`  // Private key PEM content

	// Override global CA (optional - peer connections only)
	CA    string `yaml:"ca,omitempty"`     // CA certificate file path
	CAPEM string `yaml:"ca_pem,omitempty"` // CA certificate PEM content

	// mTLS override (optional - listener only, uses global if nil)
	// Use pointer to distinguish "not set" from "false"
	MTLS *bool `yaml:"mtls,omitempty"`

	// Strict override (optional - peer connections only, uses global if nil)
	// When true, peer certificates must be validated against CA
	Strict *bool `yaml:"strict,omitempty"`
}

// GetCertPEM returns the certificate PEM content, reading from file if necessary.
func (t *TLSConfig) GetCertPEM() ([]byte, error) {
	return getPEM(t.CertPEM, t.Cert)
}

// GetKeyPEM returns the private key PEM content, reading from file if necessary.
func (t *TLSConfig) GetKeyPEM() ([]byte, error) {
	return getPEM(t.KeyPEM, t.Key)
}

// GetCAPEM returns the CA certificate PEM content, reading from file if necessary.
func (t *TLSConfig) GetCAPEM() ([]byte, error) {
	return getPEM(t.CAPEM, t.CA)
}

// HasCert returns true if certificate is configured (either file or PEM).
func (t *TLSConfig) HasCert() bool {
	return t.Cert != "" || t.CertPEM != ""
}

// HasKey returns true if private key is configured (either file or PEM).
func (t *TLSConfig) HasKey() bool {
	return t.Key != "" || t.KeyPEM != ""
}

// HasCA returns true if CA certificate is configured (either file or PEM).
func (t *TLSConfig) HasCA() bool {
	return t.CA != "" || t.CAPEM != ""
}

// GetEffectiveCertPEM returns the effective certificate PEM, preferring per-connection
// override over global config.
func (c *Config) GetEffectiveCertPEM(override *TLSConfig) ([]byte, error) {
	// Check per-connection override first
	if override != nil && override.HasCert() {
		return override.GetCertPEM()
	}
	// Fall back to global
	return c.TLS.GetCertPEM()
}

// GetEffectiveKeyPEM returns the effective private key PEM, preferring per-connection
// override over global config.
func (c *Config) GetEffectiveKeyPEM(override *TLSConfig) ([]byte, error) {
	// Check per-connection override first
	if override != nil && override.HasKey() {
		return override.GetKeyPEM()
	}
	// Fall back to global
	return c.TLS.GetKeyPEM()
}

// GetEffectiveCAPEM returns the effective CA certificate PEM, preferring per-connection
// override over global config.
func (c *Config) GetEffectiveCAPEM(override *TLSConfig) ([]byte, error) {
	// Check per-connection override first
	if override != nil && override.HasCA() {
		return override.GetCAPEM()
	}
	// Fall back to global
	return c.TLS.GetCAPEM()
}

// GetEffectiveStrict returns the effective strict TLS verification setting,
// preferring per-connection override over global config.
// Default is false (no certificate verification) because the E2E layer provides security.
func (c *Config) GetEffectiveStrict(override *TLSConfig) bool {
	// Check per-connection override first
	if override != nil && override.Strict != nil {
		return *override.Strict
	}
	// Fall back to global
	return c.TLS.Strict
}

// GetEffectiveTCPKeepAlive returns the SO_KEEPALIVE period for a peer's
// TCP-based transports: the per-peer override when set, otherwise the global
// connections.tcp_keepalive. A return value of 0 means keepalive is disabled.
func (c *Config) GetEffectiveTCPKeepAlive(peer *PeerConfig) time.Duration {
	if peer != nil && peer.TCPKeepAlive != nil {
		return *peer.TCPKeepAlive
	}
	return c.Connections.TCPKeepAlive
}

// ProxyAuth defines proxy authentication.
type ProxyAuth struct {
	Username string `yaml:"username,omitempty"`
	Password string `yaml:"password,omitempty"`
}

// SOCKS5Config defines SOCKS5 server settings.
type SOCKS5Config struct {
	Enabled        bool                  `yaml:"enabled,omitempty"`
	Address        string                `yaml:"address,omitempty"`
	Auth           SOCKS5AuthConfig      `yaml:"auth,omitempty"`
	MaxConnections int                   `yaml:"max_connections,omitempty"`
	WebSocket      WebSocketSOCKS5Config `yaml:"websocket,omitempty"`
}

// WebSocketSOCKS5Config defines WebSocket SOCKS5 listener settings.
// This allows SOCKS5 protocol to be tunneled over WebSocket transport,
// which can pass through firewalls that block raw TCP/SOCKS5 traffic.
type WebSocketSOCKS5Config struct {
	// Enabled controls whether the WebSocket SOCKS5 listener is active.
	Enabled bool `yaml:"enabled,omitempty"`

	// Address is the listen address (e.g., "0.0.0.0:8443" or "127.0.0.1:8081").
	Address string `yaml:"address,omitempty"`

	// Path is the WebSocket upgrade path (default: "/socks5").
	Path string `yaml:"path,omitempty"`

	// PlainText allows running without TLS (for reverse proxy mode).
	// When true, the listener serves plain HTTP/WebSocket.
	// Use this when running behind nginx/Caddy that handles TLS termination.
	PlainText bool `yaml:"plaintext,omitempty"`
}

// SOCKS5AuthConfig defines SOCKS5 authentication settings.
type SOCKS5AuthConfig struct {
	Enabled bool               `yaml:"enabled,omitempty"`
	Users   []SOCKS5UserConfig `yaml:"users,omitempty"`
}

// SOCKS5UserConfig defines a SOCKS5 user.
type SOCKS5UserConfig struct {
	Username string `yaml:"username,omitempty"`
	// Password is the plaintext password (deprecated, use PasswordHash).
	Password string `yaml:"password,omitempty"`
	// PasswordHash is the bcrypt hash of the password (recommended).
	// Generate with: htpasswd -bnBC 10 "" <password> | tr -d ':\n'
	PasswordHash string `yaml:"password_hash,omitempty"`
}

// ExitConfig defines exit node settings.
type ExitConfig struct {
	Enabled      bool          `yaml:"enabled,omitempty"`
	Routes       []RouteConfig `yaml:"routes,omitempty"`        // CIDR routes to advertise
	DomainRoutes []string      `yaml:"domain_routes,omitempty"` // Domain patterns to advertise (exact or *.wildcard)
	DNS          DNSConfig     `yaml:"dns,omitempty"`
}

// RouteConfig is a single CIDR exit route. The YAML form accepts either a
// bare CIDR string ("10.0.0.0/8") or a structured mapping ({cidr, metric,
// comment, source_ip, interface}); UnmarshalYAML normalizes both into the
// same struct.
//
// SourceIP, when set, binds the exit's outbound dial to this local address
// (equivalent to `curl --interface`). Interface, when set, pins the egress NIC:
// portably by binding an address on the interface
// when SourceIP is unset or already on it (unprivileged, all platforms), or via
// SO_BINDTODEVICE for an off-interface SourceIP -- the source-IP-plus-interface
// loopback pattern, which is Linux + root only. Both fields are local-only and
// never propagated to other agents.
type RouteConfig struct {
	CIDR      string `yaml:"cidr" json:"cidr"`
	Metric    uint16 `yaml:"metric,omitempty" json:"metric,omitempty"`
	Comment   string `yaml:"comment,omitempty" json:"comment,omitempty"`
	SourceIP  string `yaml:"source_ip,omitempty" json:"source_ip,omitempty"`
	Interface string `yaml:"interface,omitempty" json:"interface,omitempty"`
}

// UnmarshalYAML accepts a scalar (legacy bare CIDR string) or a mapping
// (structured form with optional metric/comment).
func (r *RouteConfig) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.ScalarNode:
		r.CIDR = value.Value
		return nil
	case yaml.MappingNode:
		// Decode into an alias type to avoid recursing into UnmarshalYAML.
		type rawRouteConfig RouteConfig
		var raw rawRouteConfig
		if err := value.Decode(&raw); err != nil {
			return err
		}
		*r = RouteConfig(raw)
		return nil
	default:
		return fmt.Errorf("exit.routes entry must be a CIDR string or {cidr, metric, comment} mapping (line %d)", value.Line)
	}
}

// MarshalYAML emits a bare CIDR string when no metric, comment, source IP, or
// interface is set, so existing configs round-trip in their original compact
// form. When any of those fields is set, the structured mapping form is emitted.
func (r RouteConfig) MarshalYAML() (any, error) {
	if r.Metric == 0 && r.Comment == "" && r.SourceIP == "" && r.Interface == "" {
		return r.CIDR, nil
	}
	type rawRouteConfig RouteConfig
	return rawRouteConfig(r), nil
}

// DNSConfig defines DNS settings for exit nodes.
type DNSConfig struct {
	Servers []string      `yaml:"servers,omitempty"`
	Timeout time.Duration `yaml:"timeout,omitempty"`
}

// RoutingConfig defines routing parameters.
type RoutingConfig struct {
	AdvertiseInterval time.Duration `yaml:"advertise_interval,omitempty"`
	NodeInfoInterval  time.Duration `yaml:"node_info_interval,omitempty"` // Defaults to AdvertiseInterval if not set
	RouteTTL          time.Duration `yaml:"route_ttl,omitempty"`
	MaxHops           int           `yaml:"max_hops,omitempty"`
}

// ConnectionsConfig defines connection tuning parameters.
type ConnectionsConfig struct {
	IdleThreshold   time.Duration   `yaml:"idle_threshold,omitempty"`
	Timeout         time.Duration   `yaml:"timeout,omitempty"`
	KeepaliveJitter float64         `yaml:"keepalive_jitter,omitempty"` // Jitter fraction for keepalive timing (0.0-1.0)
	TCPKeepAlive    time.Duration   `yaml:"tcp_keepalive,omitempty"`    // SO_KEEPALIVE period for TCP transports (h2, ws); 0 disables
	Reconnect       ReconnectConfig `yaml:"reconnect,omitempty"`
}

// ReconnectConfig defines reconnection behavior.
type ReconnectConfig struct {
	InitialDelay time.Duration `yaml:"initial_delay,omitempty"`
	MaxDelay     time.Duration `yaml:"max_delay,omitempty"`
	Multiplier   float64       `yaml:"multiplier,omitempty"`
	Jitter       float64       `yaml:"jitter,omitempty"`
	MaxRetries   int           `yaml:"max_retries,omitempty"` // 0 = infinite
}

// LimitsConfig defines resource limits.
type LimitsConfig struct {
	MaxStreamsPerPeer int           `yaml:"max_streams_per_peer,omitempty"`
	MaxStreamsTotal   int           `yaml:"max_streams_total,omitempty"`
	MaxPendingOpens   int           `yaml:"max_pending_opens,omitempty"`
	StreamOpenTimeout time.Duration `yaml:"stream_open_timeout,omitempty"`
	BufferSize        int           `yaml:"buffer_size,omitempty"`
}

// HTTPConfig defines HTTP API server settings.
type HTTPConfig struct {
	Enabled      bool          `yaml:"enabled,omitempty"`
	Address      string        `yaml:"address,omitempty"`
	ReadTimeout  time.Duration `yaml:"read_timeout,omitempty"`
	WriteTimeout time.Duration `yaml:"write_timeout,omitempty"`

	// TokenHash is a bcrypt hash of the API bearer token.
	// When set, all non-health endpoints require a valid Authorization: Bearer <token> header
	// or ?token=<token> query parameter. Health endpoints (/health, /healthz, /ready) are exempt.
	TokenHash string `yaml:"token_hash,omitempty"`

	// Minimal mode - only enable /health, /healthz, /ready endpoints.
	// When true, overrides all other endpoint flags to false.
	Minimal bool `yaml:"minimal,omitempty"`

	// Endpoint group controls. All default to true when http.enabled=true.
	// Use pointer types to distinguish between "not set" (nil = use default) and "explicitly false".
	// Disabled endpoints return 404 and log access attempts.
	Pprof     *bool `yaml:"pprof,omitempty"`      // /debug/pprof/* - Go profiling endpoints
	Dashboard *bool `yaml:"dashboard,omitempty"`  // /api/* - Dashboard API endpoints
	RemoteAPI *bool `yaml:"remote_api,omitempty"` // /agents/* - Distributed mesh APIs
}

// PprofEnabled returns whether the /debug/pprof/* endpoints are enabled.
func (h HTTPConfig) PprofEnabled() bool {
	if h.Minimal {
		return false
	}
	return h.Pprof == nil || *h.Pprof
}

// DashboardEnabled returns whether the /api/* dashboard endpoints are enabled.
func (h HTTPConfig) DashboardEnabled() bool {
	if h.Minimal {
		return false
	}
	return h.Dashboard == nil || *h.Dashboard
}

// RemoteAPIEnabled returns whether the /agents/* endpoints are enabled.
func (h HTTPConfig) RemoteAPIEnabled() bool {
	if h.Minimal {
		return false
	}
	return h.RemoteAPI == nil || *h.RemoteAPI
}

// AuthEnabled returns whether bearer token authentication is enabled for the HTTP API.
func (h HTTPConfig) AuthEnabled() bool {
	return h.TokenHash != ""
}

// FileTransferConfig defines file transfer settings.
type FileTransferConfig struct {
	// Enabled controls whether file transfer is available on this agent.
	Enabled bool `yaml:"enabled,omitempty"`

	// MaxFileSize is the maximum allowed file size in bytes.
	// Default is 500MB (500 * 1024 * 1024).
	MaxFileSize int64 `yaml:"max_file_size,omitempty"`

	// AllowedPaths controls which paths are allowed for file operations.
	// This works consistently with RPC whitelist:
	//   - Empty list []: No paths are allowed (feature is effectively disabled)
	//   - ["*"]: All absolute paths are allowed
	//   - Specific paths: Only listed paths/patterns are allowed
	//
	// Supports glob patterns:
	//   - "/tmp": Exact prefix - allows /tmp and any path under /tmp
	//   - "/tmp/*": Single-level glob - allows /tmp/file.txt, /tmp/subdir
	//   - "/tmp/**": Recursive glob - allows any path under /tmp
	//   - "/home/*/uploads": Pattern matching - allows /home/alice/uploads
	//
	// Example: ["/tmp", "/home/*/uploads"]
	AllowedPaths []string `yaml:"allowed_paths,omitempty"`

	// PasswordHash is DEPRECATED and IGNORED. File transfer (like shell and
	// dynamic config mutations) is authorized by the Ed25519 management
	// signing key, not a per-feature password. Kept for backward-compatible
	// parsing only; `muti-metroo run` warns when it is set.
	PasswordHash string `yaml:"password_hash,omitempty"`
}

// ShellConfig defines management command settings.
type ShellConfig struct {
	// Enabled controls whether management commands are available on this agent.
	Enabled bool `yaml:"enabled,omitempty"`

	// Whitelist contains allowed commands. Empty list = no commands allowed.
	// Use ["*"] to allow all commands (for testing only!).
	// Commands should be base names only (e.g., "whoami", "ls", "bash").
	Whitelist []string `yaml:"whitelist,omitempty"`

	// PasswordHash is DEPRECATED and IGNORED. Management command access (like
	// file transfer and dynamic config mutations) is authorized by the Ed25519
	// management signing key, not a per-feature password. Kept for
	// backward-compatible parsing only; `muti-metroo run` warns when it is set.
	PasswordHash string `yaml:"password_hash,omitempty"`

	// Timeout is the optional command timeout (0 = no timeout).
	Timeout time.Duration `yaml:"timeout,omitempty"`

	// MaxSessions limits concurrent shell sessions (0 = unlimited).
	MaxSessions int `yaml:"max_sessions,omitempty"`
}

// UDPConfig configures UDP relay support for exit nodes.
// UDP relay enables SOCKS5 UDP ASSOCIATE for tunneling UDP traffic through the mesh.
type UDPConfig struct {
	// Enabled controls whether UDP relay is available on this exit node.
	Enabled bool `yaml:"enabled,omitempty"`

	// MaxAssociations limits concurrent UDP associations (0 = unlimited).
	MaxAssociations int `yaml:"max_associations,omitempty"`

	// IdleTimeout is the timeout for inactive UDP associations.
	IdleTimeout time.Duration `yaml:"idle_timeout,omitempty"`

	// MaxDatagramSize is the maximum UDP payload size in bytes.
	// Default is 1472 (Ethernet MTU minus IP and UDP headers).
	MaxDatagramSize int `yaml:"max_datagram_size,omitempty"`
}

// ICMPConfig configures ICMP echo (ping) support for exit nodes.
// When enabled, agents can send ICMP echo requests to allowed destinations.
type ICMPConfig struct {
	// Enabled controls whether ICMP echo is available on this exit node.
	Enabled bool `yaml:"enabled,omitempty"`

	// MaxSessions limits concurrent ICMP sessions (0 = unlimited).
	MaxSessions int `yaml:"max_sessions,omitempty"`

	// IdleTimeout is the timeout for inactive ICMP sessions.
	IdleTimeout time.Duration `yaml:"idle_timeout,omitempty"`

	// EchoTimeout is the timeout for each individual ICMP echo request.
	EchoTimeout time.Duration `yaml:"echo_timeout,omitempty"`
}

// ForwardConfig configures TCP port forwarding.
// This enables ngrok/localtunnel-style reverse port forwarding where local services
// can be exposed through the mesh network using named routing keys.
type ForwardConfig struct {
	// Endpoints define port forward exit points - where forwarded connections terminate.
	// Each endpoint maps a routing key to a fixed target host:port.
	// The agent advertises these routing keys through the mesh.
	Endpoints []ForwardEndpoint `yaml:"endpoints,omitempty"`

	// Listeners define port forward ingress points - where external connections enter.
	// Each listener binds to a local address and forwards connections to the
	// agent with the matching routing key.
	Listeners []ForwardListener `yaml:"listeners,omitempty"`
}

// ForwardEndpoint defines a port forward exit point configuration.
// Traffic arriving for this routing key will be forwarded to the target.
type ForwardEndpoint struct {
	// Key is the routing key that identifies this port forward endpoint.
	// Must be unique within the mesh. Example: "my-web-server"
	Key string `yaml:"key,omitempty"`

	// Target is the fixed destination host:port for forwarded connections.
	// Example: "localhost:3000" or "192.168.1.10:8080"
	Target string `yaml:"target,omitempty"`
}

// ForwardListener defines a port forward ingress point configuration.
// Connections to this listener are forwarded through the mesh to the
// agent with a matching routing key endpoint.
type ForwardListener struct {
	// Key is the routing key to look up in the mesh.
	// Must match a ForwardEndpoint.Key on some agent.
	Key string `yaml:"key,omitempty"`

	// Address is the local address to listen on.
	// Example: ":8080" or "127.0.0.1:8080"
	Address string `yaml:"address,omitempty"`

	// MaxConnections limits concurrent connections (0 = unlimited).
	MaxConnections int `yaml:"max_connections,omitempty"`
}

// SleepConfig configures sleep mode for mesh hibernation.
// When enabled, agents can enter a low-profile sleep state where all peer
// connections are closed and the agent periodically polls for queued messages.
type SleepConfig struct {
	// Enabled controls whether sleep mode is available on this agent.
	Enabled bool `yaml:"enabled,omitempty"`

	// PollInterval is the base interval between polls while sleeping.
	// Actual interval varies by +/- PollIntervalJitter.
	// Default: 5 minutes.
	PollInterval time.Duration `yaml:"poll_interval,omitempty"`

	// PollIntervalJitter is the jitter fraction applied to PollInterval.
	// For example, 0.3 means +/- 30% variation.
	// Default: 0.3 (30%).
	PollIntervalJitter float64 `yaml:"poll_interval_jitter,omitempty"`

	// PollDuration is how long to stay connected during each poll.
	// Should be long enough to receive queued state and check for wake commands.
	// Default: 30 seconds.
	PollDuration time.Duration `yaml:"poll_duration,omitempty"`

	// PersistState enables saving sleep state to disk.
	// When true, sleep state survives agent restarts.
	// Default: true.
	PersistState bool `yaml:"persist_state,omitempty"`

	// MaxQueuedMessages limits the number of messages queued per sleeping peer.
	// Older messages are evicted when this limit is exceeded.
	// Default: 1000.
	MaxQueuedMessages int `yaml:"max_queued_messages,omitempty"`

	// AutoSleepOnStart starts the agent in sleep mode.
	// Useful for on-demand deployments that should remain idle until explicitly woken.
	// Default: false.
	AutoSleepOnStart bool `yaml:"auto_sleep_on_start,omitempty"`

	// DeterministicWindows configures deterministic listening windows.
	// When enabled, agents listen at predictable times derived from their AgentID,
	// allowing peers to calculate when a sleeping agent will be available.
	DeterministicWindows DeterministicWindowConfig `yaml:"deterministic_windows,omitempty"`
}

// DeterministicWindowConfig configures deterministic listening windows for sleep mode.
// When enabled, sleeping agents listen at predictable times derived from their AgentID,
// allowing peers to time their reconnection attempts efficiently.
type DeterministicWindowConfig struct {
	// Enabled controls whether deterministic windows are used.
	// When false (default), legacy random jitter is used for poll timing.
	// Default: false.
	Enabled bool `yaml:"enabled,omitempty"`

	// WindowLength is the duration of each listening window.
	// Maximum 30 seconds to limit exposure.
	// Default: 30 seconds.
	WindowLength time.Duration `yaml:"window_length,omitempty"`

	// ClockTolerance accounts for clock drift between agents.
	// Connection attempts start ClockTolerance before the predicted window.
	// Default: 5 seconds.
	ClockTolerance time.Duration `yaml:"clock_tolerance,omitempty"`

	// Epoch is the reference point for window calculations (RFC3339 format).
	// All agents should use the same epoch for interoperability.
	// Default: Unix epoch (1970-01-01T00:00:00Z).
	Epoch string `yaml:"epoch,omitempty"`
}

// Default returns a Config with default values.
func Default() *Config {
	return &Config{
		Agent: AgentConfig{
			ID:        "auto",
			DataDir:   "./data",
			LogLevel:  "info",
			LogFormat: "text",
		},
		Protocol: ProtocolConfig{
			ALPN:          "muti-metroo/1",
			HTTPHeader:    "X-Muti-Metroo-Protocol",
			WSSubprotocol: "muti-metroo/1",
		},
		Listeners: []ListenerConfig{},
		Peers:     []PeerConfig{},
		SOCKS5: SOCKS5Config{
			Enabled:        false,
			Address:        "127.0.0.1:1080",
			MaxConnections: 1000,
		},
		Exit: ExitConfig{
			Enabled: false,
			Routes:  []RouteConfig{},
			DNS: DNSConfig{
				Servers: []string{}, // Empty = use system resolver (supports .local domains)
				Timeout: 5 * time.Second,
			},
		},
		Routing: RoutingConfig{
			AdvertiseInterval: 2 * time.Minute,
			RouteTTL:          5 * time.Minute,
			MaxHops:           16,
		},
		Connections: ConnectionsConfig{
			IdleThreshold:   5 * time.Minute, // Long-running connections like SSH should stay alive
			Timeout:         90 * time.Second,
			KeepaliveJitter: 0.2,              // 20% jitter makes timing patterns less distinguishable
			TCPKeepAlive:    30 * time.Second, // SO_KEEPALIVE on TCP transports keeps NAT mappings warm
			Reconnect: ReconnectConfig{
				InitialDelay: 1 * time.Second,
				MaxDelay:     60 * time.Second,
				Multiplier:   2.0,
				Jitter:       0.2,
				MaxRetries:   0,
			},
		},
		Limits: LimitsConfig{
			MaxStreamsPerPeer: 1000,
			MaxStreamsTotal:   10000,
			MaxPendingOpens:   100,
			StreamOpenTimeout: 30 * time.Second,
			BufferSize:        262144, // 256 KB
		},
		HTTP: HTTPConfig{
			Enabled:      false,
			Address:      ":8080",
			ReadTimeout:  10 * time.Second,
			WriteTimeout: 10 * time.Second,
		},
		FileTransfer: FileTransferConfig{
			Enabled:      false,
			MaxFileSize:  500 * 1024 * 1024, // 500 MB
			AllowedPaths: []string{},        // Empty = no paths allowed (must configure explicitly)
		},
		Shell: ShellConfig{
			Enabled:     false,      // Disabled by default for security
			Whitelist:   []string{}, // Empty = no commands allowed
			MaxSessions: 0,          // 0 = unlimited (trusted network)
		},
		UDP: UDPConfig{
			Enabled:         true,
			MaxAssociations: 1000,            // Default limit
			IdleTimeout:     5 * time.Minute, // Same as connection idle threshold
			MaxDatagramSize: 1472,            // MTU - IP/UDP headers
		},
		ICMP: ICMPConfig{
			Enabled:     true,
			MaxSessions: 100,              // Default limit
			IdleTimeout: 60 * time.Second, // Session idle timeout
			EchoTimeout: 5 * time.Second,  // Per-echo timeout
		},
		Forward: ForwardConfig{
			Endpoints: []ForwardEndpoint{},
			Listeners: []ForwardListener{},
		},
		Sleep: SleepConfig{
			Enabled:            false,
			PollInterval:       5 * time.Minute,
			PollIntervalJitter: 0.3,
			PollDuration:       30 * time.Second,
			PersistState:       true,
			MaxQueuedMessages:  1000,
			AutoSleepOnStart:   false,
			DeterministicWindows: DeterministicWindowConfig{
				Enabled:        false,
				WindowLength:   30 * time.Second,
				ClockTolerance: 5 * time.Second,
				Epoch:          "", // Empty = Unix epoch
			},
		},
	}
}

// Load reads and parses a configuration file.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	return Parse(data)
}

// LoadOrEmbedded loads configuration from embedded binary data if present,
// otherwise falls back to loading from the specified file path.
// Returns the config, a boolean indicating if it was embedded, and any error.
// When embedded config is present, the path argument is ignored.
func LoadOrEmbedded(path string) (*Config, bool, error) {
	// Check for embedded config first
	if embed.HasEmbeddedConfigSelf() {
		data, err := embed.ReadFromSelf()
		if err != nil {
			return nil, false, fmt.Errorf("failed to read embedded config: %w", err)
		}
		cfg, err := Parse(data)
		if err != nil {
			return nil, false, fmt.Errorf("failed to parse embedded config: %w", err)
		}
		return cfg, true, nil
	}

	// Fall back to file
	cfg, err := Load(path)
	if err != nil {
		return nil, false, err
	}
	return cfg, false, nil
}

// LoadOrEmbeddedFrom loads configuration from embedded binary data in a specific binary,
// otherwise falls back to loading from the specified config file path.
// This is useful for DLLs where os.Executable() returns the host process path.
// If binaryPath is empty, falls back to LoadOrEmbedded behavior.
func LoadOrEmbeddedFrom(binaryPath, configPath string) (*Config, bool, error) {
	// If no binary path specified, use standard behavior
	if binaryPath == "" {
		return LoadOrEmbedded(configPath)
	}

	// Check for embedded config in the specified binary
	hasEmbedded, err := embed.HasEmbeddedConfig(binaryPath)
	if err == nil && hasEmbedded {
		data, err := embed.ReadEmbeddedConfig(binaryPath)
		if err != nil {
			return nil, false, fmt.Errorf("failed to read embedded config from %s: %w", binaryPath, err)
		}
		cfg, err := Parse(data)
		if err != nil {
			return nil, false, fmt.Errorf("failed to parse embedded config: %w", err)
		}
		return cfg, true, nil
	}

	// Fall back to config file
	if configPath == "" {
		return nil, false, fmt.Errorf("no embedded config found and no config path specified")
	}
	cfg, err := Load(configPath)
	if err != nil {
		return nil, false, err
	}
	return cfg, false, nil
}

// Parse parses configuration from YAML bytes.
func Parse(data []byte) (*Config, error) {
	// Expand environment variables
	expanded := expandEnvVars(string(data))

	// Start with defaults
	cfg := Default()

	// Parse YAML
	if err := yaml.Unmarshal([]byte(expanded), cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	// Validate
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return cfg, nil
}

// ParseRaw parses configuration from YAML bytes into an empty Config WITHOUT
// applying defaults and WITHOUT validating. Every field that the operator did
// not set is left at its zero value, which lets callers tell "explicitly set"
// apart from "defaulted". The setup wizard uses this to preserve only the
// sections an operator actually configured when re-running on an existing
// config, instead of re-emitting every default value. Environment variables
// are still expanded so the result matches what Parse would have seen.
func ParseRaw(data []byte) (*Config, error) {
	expanded := expandEnvVars(string(data))

	cfg := &Config{}
	if err := yaml.Unmarshal([]byte(expanded), cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	return cfg, nil
}

// envVarRegex matches ${VAR} or $VAR patterns
var envVarRegex = regexp.MustCompile(`\$\{([^}]+)\}|\$([A-Za-z_][A-Za-z0-9_]*)`)

// expandEnvVars replaces environment variable references with their values.
func expandEnvVars(s string) string {
	return envVarRegex.ReplaceAllStringFunc(s, func(match string) string {
		// Extract variable name
		var name string
		if strings.HasPrefix(match, "${") {
			name = match[2 : len(match)-1]
		} else {
			name = match[1:]
		}

		// Handle default values: ${VAR:-default}
		if idx := strings.Index(name, ":-"); idx != -1 {
			varName := name[:idx]
			defaultVal := name[idx+2:]
			if val, ok := os.LookupEnv(varName); ok {
				return val
			}
			return defaultVal
		}

		// Simple lookup
		if val, ok := os.LookupEnv(name); ok {
			return val
		}
		return match // Keep original if not found
	})
}

// Validate checks the configuration for errors.
func (c *Config) Validate() error {
	var errs []string

	// Validate default_action (only for embedded config usage)
	if c.DefaultAction != "" && !isOneOf(c.DefaultAction, "run", "help") {
		errs = append(errs, fmt.Sprintf("invalid default_action: %s (must be 'run' or 'help')", c.DefaultAction))
	}

	// Validate agent config
	// data_dir is required unless identity keypair is fully specified in config
	if c.Agent.DataDir == "" && !c.Agent.HasIdentityKeypair() {
		errs = append(errs, "agent.data_dir is required when agent.private_key is not configured")
	}
	// The display name rides in PEER_HELLO behind a 1-byte length prefix. A
	// longer value is clamped by the encoder rather than corrupting the frame,
	// but failing here tells the operator their name will be cut instead of
	// silently shortening it on the wire.
	if len(c.Agent.DisplayName) > protocol.MaxStringLen {
		errs = append(errs, fmt.Sprintf("agent.display_name is %d bytes, maximum is %d",
			len(c.Agent.DisplayName), protocol.MaxStringLen))
	}

	// data_dir is also required if agent.id is "auto" (can't auto-generate without persistence)
	if c.Agent.DataDir == "" && (c.Agent.ID == "" || c.Agent.ID == "auto") {
		errs = append(errs, "agent.data_dir is required when agent.id is 'auto' (cannot auto-generate without persistence)")
	}
	if !isValidLogLevel(c.Agent.LogLevel) {
		errs = append(errs, fmt.Sprintf("invalid log_level: %s (must be debug, info, warn, or error)", c.Agent.LogLevel))
	}
	if !isValidLogFormat(c.Agent.LogFormat) {
		errs = append(errs, fmt.Sprintf("invalid log_format: %s (must be text or json)", c.Agent.LogFormat))
	}
	if c.Agent.StartupDelay < 0 {
		errs = append(errs, "agent.startup_delay must not be negative")
	}
	if c.Connections.TCPKeepAlive < 0 {
		errs = append(errs, "connections.tcp_keepalive must not be negative")
	}

	// Validate identity keypair configuration
	if err := c.validateIdentityKeypair(); err != nil {
		errs = append(errs, err.Error())
	}

	// Validate global TLS config
	if err := c.validateGlobalTLS(); err != nil {
		errs = append(errs, err.Error())
	}

	// Validate listeners
	for i, l := range c.Listeners {
		if err := c.validateListener(l, i); err != nil {
			errs = append(errs, fmt.Sprintf("listeners[%d]: %v", i, err))
		}
	}

	// Validate peers
	for i, p := range c.Peers {
		if err := c.validatePeer(p, i); err != nil {
			errs = append(errs, fmt.Sprintf("peers[%d]: %v", i, err))
		}
	}

	// Validate SOCKS5
	if c.SOCKS5.Enabled && c.SOCKS5.Address == "" {
		errs = append(errs, "socks5.address is required when enabled")
	}

	// Validate SOCKS5 WebSocket
	if c.SOCKS5.WebSocket.Enabled {
		if c.SOCKS5.WebSocket.Address == "" {
			errs = append(errs, "socks5.websocket.address is required when enabled")
		}
		if c.SOCKS5.WebSocket.Path != "" && !strings.HasPrefix(c.SOCKS5.WebSocket.Path, "/") {
			errs = append(errs, "socks5.websocket.path must start with '/'")
		}
	}

	// Validate exit routes (CIDR + optional source_ip)
	for i, route := range c.Exit.Routes {
		if route.CIDR == "" {
			errs = append(errs, fmt.Sprintf("exit.routes[%d]: cidr is required", i))
			continue
		}
		_, ipNet, err := net.ParseCIDR(route.CIDR)
		if err != nil {
			errs = append(errs, fmt.Sprintf("exit.routes[%d]: invalid CIDR: %s", i, route.CIDR))
			continue
		}
		if route.SourceIP != "" {
			src := net.ParseIP(route.SourceIP)
			if src == nil {
				errs = append(errs, fmt.Sprintf("exit.routes[%d]: invalid source_ip: %s", i, route.SourceIP))
				continue
			}
			if CIDRIsV4(ipNet) != IPIsV4(src) {
				errs = append(errs, fmt.Sprintf("exit.routes[%d]: source_ip %s address family does not match CIDR %s", i, route.SourceIP, route.CIDR))
			}
		}
		if route.Interface != "" {
			if err := RequireInterfaceBindingPrivilege(route.SourceIP, route.Interface); err != nil {
				errs = append(errs, fmt.Sprintf("exit.routes[%d]: interface %q rejected: %v", i, route.Interface, err))
			}
		}
	}

	// Validate domain routes
	for i, pattern := range c.Exit.DomainRoutes {
		if err := isValidDomainPattern(pattern); err != nil {
			errs = append(errs, fmt.Sprintf("exit.domain_routes[%d]: %v", i, err))
		}
	}

	// Validate routing
	if c.Routing.MaxHops < 1 || c.Routing.MaxHops > 255 {
		errs = append(errs, "routing.max_hops must be between 1 and 255")
	}

	// Validate limits
	if c.Limits.MaxStreamsPerPeer < 1 {
		errs = append(errs, "limits.max_streams_per_peer must be positive")
	}
	if c.Limits.MaxStreamsTotal < c.Limits.MaxStreamsPerPeer {
		errs = append(errs, "limits.max_streams_total must be >= max_streams_per_peer")
	}
	if c.Limits.BufferSize < 1024 {
		errs = append(errs, "limits.buffer_size must be at least 1024")
	}

	// Validate management key configuration
	if err := c.validateManagementKeys(); err != nil {
		errs = append(errs, err.Error())
	}

	// Validate port forward configuration
	if err := c.validateForward(); err != nil {
		errs = append(errs, err.Error())
	}

	// Reject the deprecated singular `mesh_hosts_suffix:` field with a
	// migration hint instead of silently ignoring it.
	if strings.TrimSpace(c.LegacyMeshHostsSuffix) != "" {
		errs = append(errs, fmt.Sprintf("mesh_hosts_suffix is no longer supported; rename it to mesh_hosts_suffixes (a list, e.g. [%q])", c.LegacyMeshHostsSuffix))
	}

	// Validate mesh hosts (suffixes + seed entries). Construct a Store --
	// it applies the same rules used at runtime, so config and API
	// stay in lockstep.
	if _, err := hosts.New(c.MeshHostsSuffixes, c.MeshHosts); err != nil {
		errs = append(errs, err.Error())
	}

	if len(errs) > 0 {
		return fmt.Errorf("validation errors:\n  - %s", strings.Join(errs, "\n  - "))
	}

	return nil
}

// validateGlobalTLS validates the global TLS configuration.
func (c *Config) validateGlobalTLS() error {
	// Check for mTLS without CA
	if c.TLS.MTLS && !c.TLS.HasCA() {
		return fmt.Errorf("tls.ca is required when tls.mtls is enabled")
	}

	// Check for strict mode without CA (warning: peer verification won't work)
	if c.TLS.Strict && !c.TLS.HasCA() {
		return fmt.Errorf("tls.ca is required when tls.strict is enabled (for peer certificate verification)")
	}

	// Check for partial cert/key configuration
	if c.TLS.HasCert() != c.TLS.HasKey() {
		return fmt.Errorf("tls.cert and tls.key must both be specified or both be empty")
	}

	// Validate fingerprint preset
	if err := c.TLS.Fingerprint.Validate(); err != nil {
		return fmt.Errorf("tls.fingerprint: %w", err)
	}

	return nil
}

// validFingerprintPresets lists all valid fingerprint preset values.
var validFingerprintPresets = []string{
	"", "disabled", "go", "chrome", "firefox", "safari", "edge", "ios", "android", "random",
}

// Validate checks if the fingerprint configuration is valid.
func (f *FingerprintConfig) Validate() error {
	if f.Preset == "" {
		return nil
	}
	if !isOneOf(f.Preset, validFingerprintPresets...) {
		return fmt.Errorf("invalid preset %q (valid values: chrome, firefox, safari, edge, ios, android, random, go, disabled)", f.Preset)
	}
	return nil
}

// IsEnabled returns true if TLS fingerprinting is enabled.
func (f *FingerprintConfig) IsEnabled() bool {
	return f.Preset != "" && f.Preset != "disabled" && f.Preset != "go"
}

// validateManagementKeys validates the management key configuration.
func (c *Config) validateManagementKeys() error {
	// Validate encryption keys (X25519)
	if c.Management.PublicKey == "" {
		// But warn if private key is set without public key
		if c.Management.PrivateKey != "" {
			return fmt.Errorf("management.private_key requires management.public_key to be set")
		}
	} else {
		// Validate public key format
		if _, err := c.GetManagementPublicKey(); err != nil {
			return fmt.Errorf("management.public_key: %w", err)
		}

		// Validate private key format if set
		if c.Management.PrivateKey != "" {
			if _, err := c.GetManagementPrivateKey(); err != nil {
				return fmt.Errorf("management.private_key: %w", err)
			}
		}
	}

	// Validate signing keys (Ed25519)
	if c.Management.SigningPublicKey == "" {
		// Warn if signing private key is set without public key
		if c.Management.SigningPrivateKey != "" {
			return fmt.Errorf("management.signing_private_key requires management.signing_public_key to be set")
		}
	} else {
		// Validate signing public key format
		if _, err := c.GetSigningPublicKey(); err != nil {
			return fmt.Errorf("management.signing_public_key: %w", err)
		}

		// Validate signing private key format if set
		if c.Management.SigningPrivateKey != "" {
			if _, err := c.GetSigningPrivateKey(); err != nil {
				return fmt.Errorf("management.signing_private_key: %w", err)
			}
		}
	}

	return nil
}

// validateIdentityKeypair validates the agent identity keypair configuration.
func (c *Config) validateIdentityKeypair() error {
	// If no private key, check that public key is also not set
	if c.Agent.PrivateKey == "" {
		if c.Agent.PublicKey != "" {
			return fmt.Errorf("agent.public_key requires agent.private_key to be set")
		}
		return nil
	}

	// Validate private key format
	if _, err := c.Agent.GetIdentityPrivateKey(); err != nil {
		return fmt.Errorf("agent.private_key: %w", err)
	}

	// Validate public key format if set
	// Note: The derivation check (public matches private) is done in identity.KeypairFromConfig()
	if c.Agent.PublicKey != "" {
		if _, _, err := c.Agent.GetIdentityPublicKey(); err != nil {
			return fmt.Errorf("agent.public_key: %w", err)
		}
	}

	return nil
}

// validateForward validates the port forward configuration.
func (c *Config) validateForward() error {
	var errs []string

	// Validate forward endpoints
	seenKeys := make(map[string]bool)
	for i, ep := range c.Forward.Endpoints {
		if ep.Key == "" {
			errs = append(errs, fmt.Sprintf("forward.endpoints[%d]: key is required", i))
			continue
		}
		if seenKeys[ep.Key] {
			errs = append(errs, fmt.Sprintf("forward.endpoints[%d]: duplicate key %q", i, ep.Key))
		}
		seenKeys[ep.Key] = true

		if ep.Target == "" {
			errs = append(errs, fmt.Sprintf("forward.endpoints[%d]: target is required", i))
		} else if err := isValidHostPort(ep.Target); err != nil {
			errs = append(errs, fmt.Sprintf("forward.endpoints[%d]: invalid target: %v", i, err))
		}
	}

	// Validate forward listeners
	for i, lis := range c.Forward.Listeners {
		if lis.Key == "" {
			errs = append(errs, fmt.Sprintf("forward.listeners[%d]: key is required", i))
		}
		if lis.Address == "" {
			errs = append(errs, fmt.Sprintf("forward.listeners[%d]: address is required", i))
		}
		if lis.MaxConnections < 0 {
			errs = append(errs, fmt.Sprintf("forward.listeners[%d]: max_connections cannot be negative", i))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("forward validation errors:\n    - %s", strings.Join(errs, "\n    - "))
	}

	return nil
}

// isValidHostPort validates a host:port string.
func isValidHostPort(hostPort string) error {
	host, port, err := net.SplitHostPort(hostPort)
	if err != nil {
		return fmt.Errorf("invalid host:port format: %w", err)
	}
	if host == "" {
		return fmt.Errorf("host cannot be empty")
	}
	if port == "" {
		return fmt.Errorf("port cannot be empty")
	}
	return nil
}

// isAdvertisableHost rejects bind-only / loopback hosts that are common
// mistakes for advertise.address. Hostnames pass through unchanged.
func isAdvertisableHost(host string) error {
	ip := net.ParseIP(host)
	if ip == nil {
		return nil
	}
	if ip.IsUnspecified() {
		return fmt.Errorf("host %q is unspecified (0.0.0.0 / ::); advertise.address must be a publicly reachable host", host)
	}
	if ip.IsLoopback() {
		return fmt.Errorf("host %q is loopback; advertise.address must be a publicly reachable host", host)
	}
	return nil
}

func isValidLogLevel(level string) bool {
	return isOneOf(level, "debug", "info", "warn", "error")
}

func isValidLogFormat(format string) bool {
	return isOneOf(format, "text", "json")
}

func isValidTransport(transport string) bool {
	return isOneOf(transport, "quic", "h2", "ws")
}

// ValidateListenerConfig validates a single listener against the global TLS
// settings. It is exported so the agent can validate listeners added at runtime
// (e.g. via the dynamic listener-management API) using the same rules applied to
// config-file listeners at startup.
func (c *Config) ValidateListenerConfig(l ListenerConfig) error {
	return c.validateListener(l, 0)
}

// validateListener validates a listener configuration, considering global TLS settings.
func (c *Config) validateListener(l ListenerConfig, index int) error {
	if !isValidTransport(l.Transport) {
		return fmt.Errorf("invalid transport: %s (must be quic, h2, or ws)", l.Transport)
	}
	if l.Address == "" {
		return fmt.Errorf("address is required")
	}
	if (l.Transport == "h2" || l.Transport == "ws") && l.Path == "" {
		return fmt.Errorf("path is required for %s transport", l.Transport)
	}
	if err := validateAdvertise(l.Advertise); err != nil {
		return err
	}
	// PlainText mode is only supported for WebSocket (for reverse proxy scenarios)
	if l.PlainText {
		if l.Transport != "ws" {
			return fmt.Errorf("plaintext mode is only supported for ws transport (for reverse proxy scenarios)")
		}
		// Skip TLS requirement for plaintext WebSocket
		return nil
	}

	// Note: cert/key is no longer required - if not configured, a self-signed
	// certificate will be auto-generated at startup. Only validate if partially configured.
	hasCert := l.TLS.HasCert() || c.TLS.HasCert()
	hasKey := l.TLS.HasKey() || c.TLS.HasKey()
	if hasCert != hasKey {
		return fmt.Errorf("tls certificate and key must both be specified or both be empty (auto-generated if empty)")
	}

	// Determine effective mTLS setting
	enableMTLS := c.TLS.MTLS
	if l.TLS.MTLS != nil {
		enableMTLS = *l.TLS.MTLS
	}

	// If mTLS is enabled for this listener, ensure CA is configured
	if enableMTLS && !c.TLS.HasCA() {
		return fmt.Errorf("global tls.ca is required when mTLS is enabled")
	}

	return nil
}

// validateAdvertise validates the optional listener advertise block.
func validateAdvertise(a *AdvertiseConfig) error {
	if a == nil {
		return nil
	}
	if a.Address == "" {
		return fmt.Errorf("advertise.address is required when advertise block is present")
	}
	if err := isValidHostPort(a.Address); err != nil {
		return fmt.Errorf("advertise.address: %w", err)
	}
	host, _, _ := net.SplitHostPort(a.Address)
	if err := isAdvertisableHost(host); err != nil {
		return fmt.Errorf("advertise.address: %w", err)
	}
	if a.Transport != "" && !isValidTransport(a.Transport) {
		return fmt.Errorf("invalid advertise.transport: %s (must be quic, h2, or ws)", a.Transport)
	}
	return nil
}

// validatePeer validates a peer configuration, considering global TLS settings.
func (c *Config) validatePeer(p PeerConfig, index int) error {
	if p.ID == "" {
		return fmt.Errorf("id is required")
	}
	if !isValidTransport(p.Transport) {
		return fmt.Errorf("invalid transport: %s (must be quic, h2, or ws)", p.Transport)
	}
	if p.Address == "" {
		return fmt.Errorf("address is required")
	}

	// Check for partial cert/key override
	if p.TLS.HasCert() != p.TLS.HasKey() {
		return fmt.Errorf("tls cert and key must both be specified or both be empty")
	}

	// Check for strict mode without CA
	effectiveStrict := c.GetEffectiveStrict(&p.TLS)
	hasCA := p.TLS.HasCA() || c.TLS.HasCA()
	if effectiveStrict && !hasCA {
		return fmt.Errorf("tls.ca is required when strict mode is enabled (for peer certificate verification)")
	}

	if p.TCPKeepAlive != nil && *p.TCPKeepAlive < 0 {
		return fmt.Errorf("tcp_keepalive must not be negative")
	}

	return nil
}

func isValidCIDR(cidr string) bool {
	_, _, err := net.ParseCIDR(cidr)
	return err == nil
}

// CIDRIsV4 reports whether an *net.IPNet is IPv4.
func CIDRIsV4(n *net.IPNet) bool {
	return n.IP.To4() != nil && len(n.Mask) == net.IPv4len
}

// IPIsV4 reports whether an IP is IPv4. net.ParseIP returns 16-byte IPs even
// for v4 literals, so a plain `len(ip) == net.IPv4len` check is not reliable.
func IPIsV4(ip net.IP) bool {
	return ip.To4() != nil
}

// RequireInterfaceBindingPrivilege validates an (sourceIP, iface) route binding
// against the current runtime, returning an error only for the case that
// genuinely needs the privileged SO_BINDTODEVICE path. It returns nil when:
//   - iface is empty (no interface binding requested),
//   - the runtime can use SO_BINDTODEVICE (Linux + root, which honors any
//     source including an off-interface one),
//   - sourceIP is empty (the portable path picks an address off the interface),
//   - sourceIP is already assigned to iface, or
//   - iface cannot currently be resolved (deferred to dial-time enforcement).
//
// It returns an error only when running unprivileged with an explicit source IP
// that is definitively not on the selected interface -- the loopback-source-
// over-NIC case that only Linux + root can satisfy. This is a best-effort early
// check; the exit dial path (computeBindingPlan) is the authoritative gate.
func RequireInterfaceBindingPrivilege(sourceIP, iface string) error {
	if iface == "" {
		return nil
	}
	if validateInterfacePrivilege() == nil {
		return nil
	}
	if sourceIP == "" {
		return nil
	}
	src := net.ParseIP(sourceIP)
	if src == nil {
		// An invalid source IP is reported by ParseCIDRWithSource; don't
		// double-report it here.
		return nil
	}
	has, resolved := sysinfo.InterfaceHasAddress(iface, src)
	if has || !resolved {
		return nil
	}
	return fmt.Errorf("source_ip %s is not assigned to interface %q; an off-interface source requires Linux + root: %w", sourceIP, iface, validateInterfacePrivilege())
}

// ParseCIDRWithSource parses a CIDR string and, if sourceIP is non-empty, the
// source IP literal too, validating that they share an address family. Returns
// the parsed network, the parsed source (nil when sourceIP is empty), or an
// error tagged for invalid CIDR / invalid source / family mismatch.
// Centralizes the validation used by exit route add and set_source_ip across
// the CLI, dashboard API, and config validation.
func ParseCIDRWithSource(network, sourceIP string) (*net.IPNet, net.IP, error) {
	_, ipNet, err := net.ParseCIDR(network)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid CIDR %q: %w", network, err)
	}
	if sourceIP == "" {
		return ipNet, nil, nil
	}
	src := net.ParseIP(sourceIP)
	if src == nil {
		return nil, nil, fmt.Errorf("invalid source_ip %q", sourceIP)
	}
	if CIDRIsV4(ipNet) != IPIsV4(src) {
		return nil, nil, fmt.Errorf("source_ip %s address family does not match CIDR %s", sourceIP, network)
	}
	return ipNet, src, nil
}

// isValidDomainPattern validates a domain pattern (exact or *.wildcard).
func isValidDomainPattern(pattern string) error {
	if pattern == "" {
		return fmt.Errorf("empty domain pattern")
	}

	pattern = strings.TrimSpace(pattern)

	// Check for wildcard pattern
	var baseDomain string
	if strings.HasPrefix(pattern, "*.") {
		baseDomain = pattern[2:]
	} else {
		baseDomain = pattern
	}

	if baseDomain == "" {
		return fmt.Errorf("empty domain after wildcard")
	}

	// Basic domain validation
	if strings.HasPrefix(baseDomain, ".") || strings.HasSuffix(baseDomain, ".") {
		return fmt.Errorf("domain cannot start or end with a dot")
	}

	if strings.Contains(baseDomain, "..") {
		return fmt.Errorf("domain cannot contain consecutive dots")
	}

	// Check for valid characters
	for _, r := range baseDomain {
		if !isValidDomainChar(r) {
			return fmt.Errorf("invalid character in domain: %c", r)
		}
	}

	// Must have at least one dot (TLD)
	if !strings.Contains(baseDomain, ".") {
		return fmt.Errorf("domain must have at least one dot (e.g., example.com)")
	}

	return nil
}

// isValidDomainChar checks if a character is valid in a domain name.
func isValidDomainChar(r rune) bool {
	return (r >= 'a' && r <= 'z') ||
		(r >= 'A' && r <= 'Z') ||
		(r >= '0' && r <= '9') ||
		r == '-' || r == '.'
}

// String returns a string representation of the config (for debugging).
// WARNING: This method redacts sensitive values. Use StringUnsafe() for full output.
func (c *Config) String() string {
	redacted := c.Redacted()
	data, _ := yaml.Marshal(redacted)
	return string(data)
}

// StringUnsafe returns a string representation including sensitive values.
// Use with caution - do not log the output.
func (c *Config) StringUnsafe() string {
	data, _ := yaml.Marshal(c)
	return string(data)
}

// redactedValue is the placeholder for sensitive values.
const redactedValue = "[REDACTED]"

// redact replaces non-empty strings with redactedValue.
func redact(s *string) {
	if *s != "" {
		*s = redactedValue
	}
}

// Redacted returns a copy of the config with sensitive values redacted.
// This is safe to log or display to users.
func (c *Config) Redacted() *Config {
	// Create a deep copy by marshaling and unmarshaling
	data, err := yaml.Marshal(c)
	if err != nil {
		return c
	}

	redacted := &Config{}
	if err := yaml.Unmarshal(data, redacted); err != nil {
		return c
	}

	// Redact global TLS key
	redact(&redacted.TLS.Key)
	redact(&redacted.TLS.KeyPEM)

	// Redact sensitive fields in peers
	for i := range redacted.Peers {
		redact(&redacted.Peers[i].ProxyAuth.Password)
		redact(&redacted.Peers[i].TLS.Key)
		redact(&redacted.Peers[i].TLS.KeyPEM)
	}

	// Redact sensitive fields in listeners
	for i := range redacted.Listeners {
		redact(&redacted.Listeners[i].TLS.Key)
		redact(&redacted.Listeners[i].TLS.KeyPEM)
	}

	// Redact SOCKS5 user passwords and password hashes
	for i := range redacted.SOCKS5.Auth.Users {
		redact(&redacted.SOCKS5.Auth.Users[i].Password)
		redact(&redacted.SOCKS5.Auth.Users[i].PasswordHash)
	}

	// Redact other sensitive fields
	redact(&redacted.Agent.PrivateKey)
	redact(&redacted.FileTransfer.PasswordHash)
	redact(&redacted.Shell.PasswordHash)
	redact(&redacted.Management.PrivateKey)
	redact(&redacted.Management.SigningPrivateKey)
	redact(&redacted.HTTP.TokenHash)

	return redacted
}

// HasSensitiveData returns true if the config contains any sensitive data.
func (c *Config) HasSensitiveData() bool {
	// Check agent identity private key
	if c.Agent.PrivateKey != "" {
		return true
	}

	// Check peer proxy passwords
	for _, p := range c.Peers {
		if p.ProxyAuth.Password != "" {
			return true
		}
	}

	// Check SOCKS5 passwords and password hashes
	for _, u := range c.SOCKS5.Auth.Users {
		if u.Password != "" || u.PasswordHash != "" {
			return true
		}
	}

	// Check FileTransfer password hash
	if c.FileTransfer.PasswordHash != "" {
		return true
	}

	// Check Shell password hash
	if c.Shell.PasswordHash != "" {
		return true
	}

	// Check management private key
	if c.Management.PrivateKey != "" {
		return true
	}

	// Check signing private key
	if c.Management.SigningPrivateKey != "" {
		return true
	}

	return false
}
