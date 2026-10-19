# Configuration Reference

This chapter provides a complete reference for all Muti Metroo configuration options.

## Configuration File

Muti Metroo uses YAML configuration files. Specify the configuration path with:

```bash
muti-metroo run -c /path/to/config.yaml
```

## Configuration Structure

```yaml
# Default action when binary runs without arguments (embedded configs only)
# default_action: run    # Auto-start agent when embedded

# Agent identity and logging
agent:
  id: "auto" # Agent ID (auto-generate or hex string)
  display_name: "My Agent" # Human-readable name
  data_dir: "./data" # Persistent state directory (optional with identity in config)
  log_level: "info" # debug, info, warn, error
  log_format: "text" # text, json
  startup_delay: 0s # Delay before network activity (e.g., 90s, 2m)
  private_key: "" # X25519 private key (64-char hex, optional)
  public_key: "" # X25519 public key (optional, derived from private_key)

# Global TLS configuration
tls:
  ca: "./certs/ca.crt" # CA certificate (file path or use ca_pem)
  cert: "./certs/agent.crt" # Agent certificate (file path or use cert_pem)
  key: "./certs/agent.key" # Private key (file path or use key_pem)
  strict: false # Verify peer certs against the CA (default: false; E2E layer secures traffic regardless)
  mtls: false # Require client certificates on listeners
  # fingerprint:
  #   preset: ""                # disabled (default), go, chrome, firefox, safari, edge, ios, android, random

# Protocol identifiers (customizable)
protocol:
  alpn: "muti-metroo/1" # ALPN for QUIC/TLS
  http_header: "X-Muti-Metroo-Protocol"
  ws_subprotocol: "muti-metroo/1"

# Transport listeners
listeners:
  - transport: quic # quic, h2, ws
    address: "0.0.0.0:4433"

# Peer connections
peers:
  - id: "abc123..."
    transport: quic
    address: "192.168.1.10:4433"

# SOCKS5 proxy
socks5:
  enabled: true
  address: "127.0.0.1:1080"
  auth:
    enabled: false
  max_connections: 1000

# Exit node
exit:
  enabled: false
  routes: # bare CIDR string or {cidr, metric, comment}
    - "10.0.0.0/8"
  # domain_routes:              # advertise domain patterns (resolved at exit)
  #   - "*.internal.example"
  dns:
    servers: [] # default: [] = use system resolver (incl. .local / mDNS)
    timeout: 5s # only used when servers is non-empty

# Routing settings
routing:
  advertise_interval: 2m
  node_info_interval: 2m # Defaults to advertise_interval if not set
  route_ttl: 5m
  max_hops: 16

# Connection tuning
connections:
  idle_threshold: 5m
  timeout: 90s
  keepalive_jitter: 0.2
  reconnect:
    initial_delay: 1s
    max_delay: 60s
    multiplier: 2.0
    jitter: 0.2
    max_retries: 0

# Resource limits
limits:
  max_streams_per_peer: 1000
  max_streams_total: 10000
  max_pending_opens: 100
  stream_open_timeout: 30s
  buffer_size: 262144

# HTTP API (disabled by default; set enabled: true to expose)
http:
  enabled: true
  address: ":8080"
  read_timeout: 10s
  write_timeout: 10s
  token_hash: "" # bcrypt-hashed bearer token; empty = no auth (Generate: `muti-metroo hash`)
  minimal: false # When true, only /health, /healthz, /ready are served
  pprof: false # /debug/pprof/* (default: true when http.enabled; disable in production)
  dashboard: true # /api/topology, /api/dashboard, /api/nodes, /api/mesh-test, /api/probe, /api/setup-defaults
  remote_api: true # /agents/*, /routes/*, /forward/*, /peers/*, /hosts/*, /sleep, /wake

# Management commands (authorized by management.signing_*_key)
shell:
  enabled: false
  whitelist: []
  timeout: 0s
  max_sessions: 0

# File transfer (authorized by management.signing_*_key)
file_transfer:
  enabled: false
  max_file_size: 524288000
  allowed_paths: []

# Management keys (topology encryption + privileged-operation signing)
management:
  public_key: ""
  private_key: ""
  signing_public_key: ""
  signing_private_key: ""

# UDP relay
udp:
  enabled: true
  max_associations: 1000
  idle_timeout: 5m
  max_datagram_size: 1472

# ICMP echo (ping)
icmp:
  enabled: true
  max_sessions: 100
  idle_timeout: 60s
  echo_timeout: 5s

# Port forwarding (reverse tunnels)
forward:
  endpoints: [] # [{key, target}] - expose local services
  listeners: [] # [{key, address, max_connections}] - accept inbound

# Mesh-local hostname registry (pushed to Mutiauk over the SOCKS5 endpoint)
mesh_hosts_suffixes: ["mesh"] # allowed TLD suffixes; every host must end with one of these
mesh_hosts: [] # [{name, ipv4, ipv6, comment}] config-seeded entries

# Sleep mode (power/resource saving - peers drop, agents poll periodically for queued work)
sleep:
  enabled: false
  poll_interval: 5m
  poll_interval_jitter: 0.3
  poll_duration: 30s
  persist_state: true
  max_queued_messages: 1000
  auto_sleep_on_start: false
```

## Default Action (Embedded Configs Only)

When creating single-file deployments with embedded configuration, the wizard automatically sets `default_action: run`. This allows the binary to start without the `run` argument:

```yaml
default_action: run
```

**Behavior:**

- `./my-agent` - Starts agent automatically
- `./my-agent help` - Shows help
- `./my-agent status` - Shows status

Without `default_action` set (or with file-based configs), running without arguments shows help.

**Valid values:** `run`, `help`

## Agent Section

```yaml
agent:
  id: "auto" # "auto" or 32-char hex string
  display_name: "My Agent" # Shown in dashboard API
  data_dir: "./data" # Where to store state (optional with identity in config)
  log_level: "info" # debug, info, warn, error
  log_format: "text" # text or json
  startup_delay: 0s # Delay before network activity (e.g., 90s, 2m)
  private_key: "" # X25519 private key for E2E encryption (optional)
  public_key: "" # X25519 public key (optional, derived from private_key)
```

### Startup Delay

Delays all network activity (listeners, peer connections, SOCKS5) for a specified duration after the process starts. The agent process is alive but idle during the delay and can be cleanly shut down.

```yaml
agent:
  startup_delay: 90s
```

Can also be set via CLI: `muti-metroo run --startup-delay 2m`

### Identity Keypair

By default, the X25519 keypair for E2E encryption is stored in `data_dir`. For single-file deployments, you can specify the keypair directly in config:

```yaml
agent:
  id: "a1b2c3d4e5f6789012345678901234ab"
  private_key: "48bbea6c0c9be254bde983c92c8a53db759f27e51a6ae77fd9cca81895a5d57c"
  # data_dir not needed when identity is in config
```

When `private_key` is set, `data_dir` becomes optional. The `public_key` field is automatically derived from `private_key` if not specified.

The simplest way to obtain an identity keypair for embedding is to run the
interactive setup wizard (`muti-metroo setup`); it generates both the agent ID and
the X25519 keypair and writes the result into the produced `config.yaml` (or
embedded single-file binary) automatically.

To do it by hand, point the agent at a throwaway data directory once:

```bash
mkdir -p /tmp/keys
muti-metroo init -d /tmp/keys                # writes agent_id only
muti-metroo run -c minimal.yaml &             # config with data_dir: /tmp/keys
sleep 1 && kill %1                     # agent_key is now on disk

cat /tmp/keys/agent_id                 # paste as agent.id
cat /tmp/keys/agent_key                # paste as agent.private_key (hex)
```

`muti-metroo init` deliberately creates only the agent_id file. The X25519
keypair (`agent_key`, `agent_key.pub`) is created lazily on the first
`muti-metroo run` so that the on-disk format and the runtime path stay identical.

## Listeners Section

Configure transport listeners:

```yaml
listeners:
  # QUIC listener
  - transport: quic
    address: "0.0.0.0:4433"

  # HTTP/2 listener
  - transport: h2
    address: "0.0.0.0:8443"
    path: "/mesh"

  # WebSocket listener
  - transport: ws
    address: "0.0.0.0:443"
    path: "/mesh"

  # Plaintext (behind reverse proxy)
  - transport: ws
    address: "127.0.0.1:8080"
    path: "/mesh"
    plaintext: true
```

## Peers Section

Configure outbound peer connections:

```yaml
peers:
  # QUIC peer
  - id: "abc123def456..."
    transport: quic
    address: "192.168.1.10:4433"

  # HTTP/2 peer
  - id: "def456abc123..."
    transport: h2
    address: "relay.example.com:8443"
    path: "/mesh"

  # WebSocket with proxy
  - id: "789xyz..."
    transport: ws
    address: "wss://relay.example.com:443/mesh"
    proxy: "http://proxy.corp.local:8080"
    proxy_auth:
      username: "${PROXY_USER}"
      password: "${PROXY_PASS}"
```

**Connection direction is arbitrary**: An agent with `peers` configured acts as a dialer (client), while the target agent must have `listeners`. However, once connected, **both agents can initiate virtual streams in either direction**. The connection direction does not affect which agent can be ingress, transit, or exit - choose based on network constraints (firewalls, NAT), not functionality. See the Agent Roles chapter for details.

### Dynamic Peer Management

Peers can also be added or removed at runtime without restarting the agent, via CLI (`muti-metroo peer add/remove/list`) or HTTP API (`/peers/manage`). Dynamic peers inherit TLS settings from the agent's global config and auto-reconnect just like configured peers. When `agent.data_dir` is set, dynamic peers are persisted to a journal file and survive restarts; with no `data_dir` (or an unwritable journal), they remain ephemeral. See the "Dynamic Config Persistence" section below.

This is useful when a new agent can only accept connections but an existing agent (behind a firewall or NAT) must initiate the dial - you can command the already-running agent to dial the new peer without editing its `config.yaml`.

```bash
# Tell the local agent to dial a new peer
muti-metroo peer add newagent.example:4433

# Tell a remote agent (reachable via mesh) to dial a new peer
muti-metroo peer add newagent.example:4433 --target <agent-id>
```

## SOCKS5 Section

Configure the SOCKS proxy ingress. This is the endpoint that Mutiauk on an operator workstation, or a SOCKS-aware application, connects to (Chapters 9 and 10). The listener accepts SOCKS4, SOCKS4a, and SOCKS5 connections on the same address, auto-detected from the first byte. The optional `websocket` block adds a SOCKS5-over-WebSocket listener with TLS for remote clients (Chapter 9).

```yaml
socks5:
  enabled: true
  address: "127.0.0.1:1080"
  auth:
    enabled: true
    users:
      - username: "user1"
        password_hash: "$2a$10$..."
  max_connections: 1000
```

Generate password hashes with `muti-metroo hash` (default bcrypt cost 12, OWASP 2024;
pass `--cost N` for a different work factor):

```bash
muti-metroo hash
```

## Exit Section

Configure exit node routing:

```yaml
exit:
  enabled: true
  # routes accepts a bare CIDR string OR a structured mapping with
  # optional metric, comment, and source_ip. comment is operator-visible
  # text (propagated alongside the advertisement). source_ip pins this
  # CIDR's outbound dials at the exit to a specific local IP -- useful
  # on multi-homed hosts (like `curl --interface`); it is local-only
  # and never advertised to peers.
  routes:
    - "10.0.0.0/8"
    - cidr: "192.168.0.0/16"
      comment: "office LAN"
    - cidr: "10.10.0.0/16"
      source_ip: "192.168.1.50" # bind dials for this CIDR to .50
    - cidr: "0.0.0.0/0"
      metric: 5
      comment: "fallback exit"
  domain_routes:
    - "api.internal.corp"
    - "*.example.com"
  dns:
    servers: [] # default: use the system resolver
    # servers:                  # uncomment to pin specific upstreams
    #   - "10.0.0.53:53"        # e.g. an internal/DoT/operator-controlled
    #   - "10.0.0.54:53"        # resolver. Public resolvers like 8.8.8.8
    #                           # or 1.1.1.1 leak DNS to a third party and
    #                           # must NOT be used in compliance-bound or
    #                           # confidentiality-sensitive deployments.
    timeout: 5s # only used when servers is non-empty
```

`dns.servers` defaults to `[]`, which delegates resolution to the agent
host's stub resolver (`/etc/resolv.conf` on Linux, `scutil`/`mDNSResponder`
on macOS, `Dnsapi.dll` on Windows). That path also handles `.local` / mDNS
and any split-horizon DNS the host is already configured for. Override it
only when you need to force specific upstreams.

## HTTP API Section

Configure the HTTP API server:

```yaml
http:
  enabled: true
  address: ":8080"
  read_timeout: 10s
  write_timeout: 10s
  minimal: false # Only health endpoints
  pprof: false # Debug endpoints
  dashboard: true # Dashboard API endpoints
  remote_api: true # Remote agent APIs
```

## Environment Variables

All configuration values support environment variable substitution:

```yaml
agent:
  data_dir: "${DATA_DIR:-./data}"
  log_level: "${LOG_LEVEL:-info}"

socks5:
  auth:
    users:
      - username: "${SOCKS_USER}"
        password_hash: "${SOCKS_PASS_HASH}"
```

Syntax:

- `${VAR}` - Use environment variable (kept as literal if not set)
- `${VAR:-default}` - Use default if variable not set

## Minimal Configuration

The simplest working configuration. TLS certificates are auto-generated if not specified:

```yaml
agent:
  id: "auto"
  data_dir: "./data"

listeners:
  - transport: quic
    address: "0.0.0.0:4433"

socks5:
  enabled: true
  address: "127.0.0.1:1080"

exit:
  enabled: true
  routes:
    - "0.0.0.0/0"

http:
  enabled: true
  address: ":8080"
```

## Dynamic Config Persistence

Runtime changes made through the CLI and HTTP API (`muti-metroo peer add`, `muti-metroo forward add`, `muti-metroo route add`, `muti-metroo display-name set`, and their `remove` counterparts) are persisted to a journal file alongside the agent identity, so they survive restarts.

Persistence is automatic: when `agent.data_dir` is set, every mutation is appended to `<data_dir>/dynamic-config.log` in JSON Lines format. On the next startup the agent replays the journal through the same code paths that originally handled the commands, restoring every dynamic change and preserving the config-vs-dynamic distinction (replayed entries still appear with `dynamic: true` in `list` output).

When `agent.data_dir` is not configured, or the journal file cannot be written (wrong permissions, read-only filesystem, disk full), the runtime mutation still succeeds but stays ephemeral, exactly the pre-3.4 behavior. A warning is logged so operators can diagnose the write failure.

**What gets persisted:**

- Outbound dynamic peers (`muti-metroo peer add/remove`)
- Dynamic forward listeners (`muti-metroo forward add/remove`)
- Dynamic forward endpoints (`muti-metroo forward endpoint add/remove`)
- Dynamic CIDR exit routes (`muti-metroo route add/remove`)
- Dynamic transport listeners (`muti-metroo listener add/remove`)
- Mesh-host entries and TLD suffixes (`muti-metroo host add/remove`, `muti-metroo host suffix add/remove`)
- Route comment / source-ip / interface changes (`muti-metroo route comment|source-ip|interface`)
- Dynamic display name (`muti-metroo display-name set`)

**What does not:**

- Entries defined in `config.yaml` (they are already persistent)
- Inbound peer connections (they are reestablished when the remote dials)
- Connection state, keepalive state, and transient session keys

**Managing the journal:**

```bash
# Inspect current state
cat /var/lib/muti-metroo/dynamic-config.log | jq

# Reset all dynamic state (stop the agent first)
systemctl stop muti-metroo
rm /var/lib/muti-metroo/dynamic-config.log
systemctl start muti-metroo
```

The journal is compacted automatically on every successful replay: long histories of add/remove churn are collapsed into the minimal set of "currently live" records, so the file size stays O(live entries) rather than O(lifetime mutations). No manual maintenance is required.

For production deployments, you should provide your own TLS certificates using the `tls` section.
