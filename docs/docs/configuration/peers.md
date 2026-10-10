---
title: Peers
sidebar_position: 4
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-inspecting.png" alt="Mole configuring peers" style={{maxWidth: '180px'}} />
</div>

# Peer Configuration

Connect to other agents in your mesh. One side listens, the other connects - once linked, traffic flows both ways.

**Quick setup:**
```yaml
peers:
  - id: "abc123def456..."       # Target agent's ID
    transport: quic             # quic, h2, or ws
    address: "192.168.1.10:4433"
```

## Configuration

Peers use the global TLS configuration by default:

```yaml
tls:
  ca: "./certs/ca.crt"
  cert: "./certs/agent.crt"
  key: "./certs/agent.key"
  mtls: true

peers:
  - id: "abc123def456789012345678901234ab"   # Expected peer Agent ID
    transport: quic                           # quic, h2, ws
    address: "192.168.1.10:4433"             # Peer address
```

The global `ca` is used to verify the peer's server certificate, and the global `cert`/`key` are used as the client certificate when the peer requires mTLS.

## Peer Options

### Basic Peer

```yaml
peers:
  - id: "abc123def456789012345678901234ab"
    transport: quic
    address: "192.168.1.10:4433"
    # Uses global TLS settings
```

### Full Options

```yaml
peers:
  - id: "abc123def456789012345678901234ab"
    transport: quic
    address: "192.168.1.10:4433"
    tcp_keepalive: 20s                  # Override connections.tcp_keepalive (h2/ws only)
    tls:
      ca: "./certs/other-ca.crt"       # Override global CA (rare)
      strict: true                      # Enable verification for this peer
```

## Peer ID

The `id` field specifies the expected Agent ID of the peer:

```yaml
peers:
  - id: "abc123def456789012345678901234ab"
```

This provides:
- **Authentication**: Verify you are connecting to the right agent
- **Security**: Prevent man-in-the-middle attacks
- **Routing**: Identify peer for route lookup

### Getting Peer ID

From the peer's agent:

```bash
# From file
cat /path/to/peer/data/agent_id

# From API
curl http://peer-host:8080/healthz | jq -r '.agent_id'

# From logs
# Look for: Agent ID: abc123...
```

## Transport Types

### QUIC

```yaml
peers:
  - id: "..."
    transport: quic
    address: "192.168.1.10:4433"
```

### HTTP/2

```yaml
peers:
  - id: "..."
    transport: h2
    address: "192.168.1.10:8443"
    path: "/mesh"                    # Must match listener path
```

### WebSocket

```yaml
peers:
  - id: "..."
    transport: ws
    address: "wss://relay.example.com:443/mesh"
```

### WebSocket Through Proxy

When connecting through a proxy, mTLS is not available and the external server may use RSA certificates:

```yaml
peers:
  - id: "..."
    transport: ws
    address: "wss://relay.example.com:443/mesh"
    proxy: "http://proxy.corp.local:8080"
    proxy_auth:
      username: "${PROXY_USER}"
      password: "${PROXY_PASS}"
```

Note: When using a proxy, the global agent certificate is not used for mTLS since the TLS connection terminates at the proxy or external server.

## TLS Configuration

### Using Global Settings

By default, peers use the global TLS configuration:

```yaml
tls:
  ca: "./certs/ca.crt"
  cert: "./certs/agent.crt"
  key: "./certs/agent.key"
  mtls: true

peers:
  - id: "..."
    transport: quic
    address: "192.168.1.10:4433"
    # Uses global CA to verify server
    # Uses global cert/key as client certificate
```

### Per-Peer Overrides

Override specific settings per peer:

```yaml
tls:
  ca: "./certs/ca.crt"
  cert: "./certs/agent.crt"
  key: "./certs/agent.key"

peers:
  # Uses global settings
  - id: "..."
    transport: quic
    address: "192.168.1.10:4433"

  # Override: different CA with strict verification
  - id: "..."
    transport: quic
    address: "external.example.com:4433"
    tls:
      ca: "./certs/external-ca.crt"
      strict: true
```

### Public Proxy with Different CA

When connecting through a public proxy (like nginx with Let's Encrypt) while using strict TLS for internal peers:

```yaml
tls:
  ca_pem: |
    ... internal CA ...
  cert_pem: |
    ... agent cert ...
  key_pem: |
    ... agent key ...
  strict: true   # Global: verify against internal CA

peers:
  # Internal peer: uses global strict verification
  - id: "abc123..."
    transport: quic
    address: "192.168.1.10:4433"

  # Public proxy: disable strict verification
  - id: "def456..."
    transport: ws
    address: "wss://relay.example.com:443/mesh"
    tls:
      strict: false   # Skip verification for this peer
```

This is safe because E2E encryption protects all traffic regardless of TLS verification.

### Inline Certificates

```yaml
peers:
  - id: "..."
    tls:
      ca_pem: |
        -----BEGIN CERTIFICATE-----
        ...
        -----END CERTIFICATE-----
```

## Reconnection

Reconnection is a **global** setting (not per-peer). Configure it in the `connections` section:

```yaml
connections:
  reconnect:
    initial_delay: 1s         # First retry delay
    max_delay: 60s            # Maximum retry delay
    multiplier: 2.0           # Exponential backoff multiplier
    jitter: 0.2               # 20% random jitter
    max_retries: 0            # 0 = infinite retries
```

### Reconnection Algorithm

```
delay = min(initial_delay * multiplier^attempt, max_delay) * (1 + random(jitter))
```

Example with defaults:
- Attempt 1: ~1s
- Attempt 2: ~2s
- Attempt 3: ~4s
- Attempt 4: ~8s
- ... (caps at 60s)

### Disabling Reconnection

Reconnection is configured globally (not per-peer):

```yaml
connections:
  reconnect:
    max_retries: 1            # Only try once
```

## Keepalive

TCP-based transports (`h2`, `ws`) use the operating system's SO_KEEPALIVE to keep
idle links alive across NAT and to detect dead connections. The period is global
by default and can be overridden per peer:

```yaml
connections:
  tcp_keepalive: 30s          # Global default for h2/ws peers (0 disables)

peers:
  - id: "agent-relay-id..."
    transport: h2
    address: "relay.example.com:3000"
    tcp_keepalive: 20s        # Shorter for this peer (e.g. dialing from behind a strict NAT)
```

This is distinct from the application-level keepalive interval
(`connections.idle_threshold`), which controls the mesh beacon cadence. QUIC peers
manage their own keepalive and ignore `tcp_keepalive`.

## Multiple Peers

Connect to multiple agents:

```yaml
tls:
  ca: "./certs/ca.crt"
  cert: "./certs/agent.crt"
  key: "./certs/agent.key"

peers:
  # Direct QUIC to local agent
  - id: "agent-local-id..."
    transport: quic
    address: "192.168.1.10:4433"

  # HTTP/2 to cloud relay
  - id: "agent-cloud-id..."
    transport: h2
    address: "relay.cloud.com:443"
    path: "/mesh"

  # WebSocket through proxy to remote site
  - id: "agent-remote-id..."
    transport: ws
    address: "wss://remote.site.com:443/mesh"
    proxy: "http://proxy:8080"
```

## Dynamic Peers

Peers can also be added or removed at runtime, without editing `config.yaml` or restarting the agent. See the [`peer` CLI command](/cli/peer) and [`/peers/manage` HTTP endpoint](/api/peer-management).

```bash
# Tell the local agent to dial a new peer
muti-metroo peer add newagent.example:4433

# Tell a remote agent (reachable via mesh) to dial a new peer
muti-metroo peer add newagent.example:4433 --target <agent-id>

# List all peers (config + dynamic)
muti-metroo peer list

# Disconnect and forget a dynamic peer
muti-metroo peer remove newagent.example:4433
```

Dynamic peers:
- Are **ephemeral** - lost when the agent restarts. Move them into `config.yaml` for persistence.
- **Inherit TLS** settings from the agent's global config. Per-peer TLS overrides are only available for config-file peers.
- **Auto-reconnect** on disconnect, just like configured peers.
- Are the solution when a new agent can only accept connections but an existing agent (behind a firewall or NAT) must initiate the dial.

Config-file peers are protected from removal or replacement at runtime.

## Address Formats

### IPv4

```yaml
address: "192.168.1.10:4433"
```

### IPv6

```yaml
address: "[2001:db8::1]:4433"
```

### Hostname

```yaml
address: "agent.example.com:4433"
```

### With Path (HTTP/2, WebSocket)

```yaml
address: "agent.example.com:443"
path: "/mesh"

# Or full URL for WebSocket
address: "wss://agent.example.com:443/mesh"
```

## Environment Variables

```yaml
peers:
  - id: "${PEER_ID}"
    transport: "${PEER_TRANSPORT:-quic}"
    address: "${PEER_ADDR}"
```

## Examples

### Two-Agent Setup

Agent A connects to Agent B:

```yaml
# Agent A config
tls:
  ca: "./certs/ca.crt"
  cert: "./certs/agent.crt"
  key: "./certs/agent.key"

peers:
  - id: "bbbb2222..."    # Agent B's ID
    transport: quic
    address: "192.168.1.20:4433"
```

Agent B (listener only, no peers needed):

```yaml
# Agent B config
tls:
  ca: "./certs/ca.crt"
  cert: "./certs/agent.crt"
  key: "./certs/agent.key"
  mtls: true

listeners:
  - transport: quic
    address: "0.0.0.0:4433"
```

:::note Connection Direction is Arbitrary
In the example above, Agent A connects to Agent B. However, you could reverse this: Agent B could connect to Agent A instead, and the mesh would function identically.

**Key insight**: Transport connection direction does NOT affect virtual stream direction. Once connected, either agent can:
- Initiate virtual streams to the other
- Act as ingress, transit, or exit
- Access routes advertised by the other

Choose connection direction based on **network constraints** (firewalls, NAT), not functionality. Place agents behind NAT/firewalls as dialers (`peers`), and public agents as listeners.

See [Architecture - Connection Model](/concepts/architecture#connection-model) for details.
:::

### Hub and Spoke

Central hub with multiple spokes:

```yaml
# Hub config (no outbound peers, just listeners)
tls:
  ca: "./certs/ca.crt"
  cert: "./certs/agent.crt"
  key: "./certs/agent.key"
  mtls: true

listeners:
  - transport: quic
    address: "0.0.0.0:4433"

# Spoke configs
tls:
  ca: "./certs/ca.crt"
  cert: "./certs/agent.crt"
  key: "./certs/agent.key"

peers:
  - id: "hub-agent-id..."
    transport: quic
    address: "hub.example.com:4433"
```

### Full Mesh

Each agent connects to all others:

```yaml
# Agent A
peers:
  - id: "agent-b-id..."
    address: "192.168.1.20:4433"
  - id: "agent-c-id..."
    address: "192.168.1.30:4433"

# Agent B
peers:
  - id: "agent-a-id..."
    address: "192.168.1.10:4433"
  - id: "agent-c-id..."
    address: "192.168.1.30:4433"

# Agent C
peers:
  - id: "agent-a-id..."
    address: "192.168.1.10:4433"
  - id: "agent-b-id..."
    address: "192.168.1.20:4433"
```

## Troubleshooting

### Connection Failed

```bash
# Check peer is reachable
nc -zv 192.168.1.10 4433

# Check DNS resolution
dig agent.example.com

# Check with debug logging
muti-metroo run -c config.yaml --log-level debug
```

### Certificate Errors

```bash
# Verify CA certificate
openssl x509 -in ./certs/ca.crt -text -noout

# Test TLS connection
openssl s_client -connect 192.168.1.10:4433 -CAfile ./certs/ca.crt
```

### Wrong Peer ID

```
ERROR  Peer ID mismatch: expected abc123..., got def456...
```

Update the `id` field to match the actual peer Agent ID.

## Related

- [Listeners](/configuration/listeners) - Accept incoming connections
- [TLS Certificates](/configuration/tls-certificates) - Certificate setup
- [Transports](/concepts/transports) - Transport details
