# Peer Commands

Commands for managing dynamic outbound peer connections.

Dynamic peers let you tell an already-running agent to dial a new peer without editing `config.yaml` and restarting. A common use case: a new agent `F` can accept connections but an existing agent `D` (behind a firewall or NAT) must initiate the dial. Use `muti-metroo peer add` on `D` to create the connection at runtime.

Peers are uniquely keyed by dial address. Dynamic peers persist across agent restarts when `agent.data_dir` is set (see [Dynamic Config Persistence](/configuration/dynamic-config-persistence)); TLS settings are inherited from the agent's global config.

`peer status` and `peer list` answer two different questions:

- `peer status` -> "who am I talking to right now?" (live transport state, role, RTT). Replaces the deprecated top-level `muti-metroo peers`.
- `peer list` -> "what peers does my configuration define?" (config + dynamic + inbound entries; dialer/listener kind).

## peer status

```bash
muti-metroo peer status [flags]
```

Fetch the live peer table from `/api/dashboard` -- equivalent to the deprecated top-level `muti-metroo peers`. Use `--json` for machine-readable output.

## peer add

Add a dynamic outbound peer connection.

```bash
muti-metroo peer add <address> [flags]
```

### Description

Instructs the target agent to dial the given peer address and establish a persistent connection. The agent reuses its global TLS configuration (CA, client cert, mTLS, strict-verify) for the new connection.

Dynamic peers auto-reconnect on disconnect with the usual exponential backoff, same as config-file peers. When `agent.data_dir` is configured they also survive agent restarts via the [dynamic config journal](/configuration/dynamic-config-persistence); if no `data_dir` is set (or the journal is not writable) they remain ephemeral and are lost on restart.

### Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--agent` | `-a` | `localhost:8080` | Agent API address (host:port or http(s):// URL) |
| `--target` | `-t` | | Target agent ID (omit for local agent) |
| `--transport` | | `quic` | Transport type: `quic`, `h2`, or `ws` |
| `--id` | | | Expected peer agent ID (omit or `auto` to accept any) |
| `--tcp-keepalive` | | | SO_KEEPALIVE period for `h2`/`ws` (e.g. `20s`; omit to inherit, `0` to disable) |

### Examples

```bash
# Tell the local agent to dial a new peer via QUIC
muti-metroo peer add newagent.example:4433

# Add over HTTP/2 with a specific expected peer ID
muti-metroo peer add newagent.example:443 --transport h2 --id abc123def456

# Add over WebSocket
muti-metroo peer add newagent.example:443 --transport ws

# Tell remote agent D (reachable via mesh) to dial F
muti-metroo peer add F.example:4433 --target <agent-d-id>

# Via a different API server
muti-metroo peer add newagent.example:4433 -a 192.168.1.10:8080 -t def456
```

### Output

```
Peer added: peer newagent.example:4433 added (transport=quic)
```

### Use Cases

- **NAT/Firewall Traversal**: Asymmetric reachability where one side can only accept connections
- **Mesh Extension**: Add a new agent to an existing mesh without editing deployed configs
- **Rebalancing**: Route around a failing peer by dialing an alternate at runtime

---

## peer remove

Remove a dynamic peer connection.

```bash
muti-metroo peer remove <address> [flags]
```

### Description

Tears down the live connection to the peer at the given address, cancels any pending reconnect, and removes the peer from the manager.

Only dynamic peers can be removed this way. Peers declared in the `peers:` config are protected and cannot be removed without restarting the agent.

### Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--agent` | `-a` | `localhost:8080` | Agent API address (host:port or http(s):// URL) |
| `--target` | `-t` | | Target agent ID (omit for local agent) |

### Examples

```bash
# Remove dynamic peer from local agent
muti-metroo peer remove newagent.example:4433

# Remove from a remote agent
muti-metroo peer remove newagent.example:4433 -t abc123def456
```

### Output

```
Peer removed: peer newagent.example:4433 removed (disconnected abc123de)
```

When no live connection exists (e.g. still in backoff), the short ID portion is omitted.

---

## peer list

List all peer connections.

```bash
muti-metroo peer list [flags]
```

### Description

Displays all peers known to the agent: static (config-file), dynamic (runtime), and inbound (peers that dialed into this agent). Reports transport, connection state, and remote agent ID where available.

Inbound peers are distinguished by a `direction: "inbound"` field in the JSON output. The tabular output does not surface direction separately -- use `--json` when you need to tell inbound and outbound apart programmatically.

### Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--agent` | `-a` | `localhost:8080` | Agent API address (host:port or http(s):// URL) |
| `--target` | `-t` | | Target agent ID (omit for local agent) |
| `--json` | | `false` | Output in JSON format |

### Examples

```bash
# List peers on local agent
muti-metroo peer list

# List peers on remote agent
muti-metroo peer list -t abc123def456

# JSON output
muti-metroo peer list --json
```

### Output

Standard output:

```
Peers (2)
ADDRESS                        TRANS  CONNECTED  TYPE       REMOTE_ID
agent2:4433                    quic   yes        static     abcdef1234567890...
newagent.example:4433          quic   yes        dynamic    9876543210fedcba...
```

JSON output:

```json
{
  "status": "ok",
  "peers": [
    {
      "address": "agent2:4433",
      "transport": "quic",
      "remote_id": "abcdef1234567890abcdef1234567890",
      "connected": true,
      "persistent": true,
      "dynamic": false
    },
    {
      "address": "newagent.example:4433",
      "transport": "quic",
      "remote_id": "9876543210fedcba9876543210fedcba",
      "connected": true,
      "persistent": true,
      "dynamic": true
    },
    {
      "address": "198.51.100.7:54321",
      "transport": "quic",
      "remote_id": "fedcba0987654321fedcba0987654321",
      "connected": true,
      "persistent": false,
      "dynamic": false,
      "direction": "inbound"
    }
  ]
}
```

---

## Authorization

### Management Key Restriction

Dynamic peer modifications are restricted when the mesh is configured with management key encryption, following the same rules as dynamic route and forward management.

If an agent has `management.public_key` configured but does NOT have the corresponding `management.private_key`, peer add/remove/list commands are rejected with HTTP 403 Forbidden.

---

## Important Notes

### Persistence

Dynamic peers survive agent restarts when `agent.data_dir` is configured: every add/remove is appended to a [dynamic config journal](/configuration/dynamic-config-persistence) under `data_dir/dynamic-config.log` and replayed on the next start.

If no `data_dir` is configured, or the journal file cannot be written (bad permissions, read-only filesystem, disk full), the runtime mutation still succeeds but remains ephemeral: the peer is lost on the next restart.

Peers declared in `config.yaml` take precedence and cannot be replaced or removed at runtime:

```yaml
peers:
  - address: "newagent.example:4433"
    transport: "quic"
    id: "auto"   # or a specific agent ID
```

### TLS Settings

Dynamic peers inherit the agent's global TLS configuration (CA, client cert, mTLS, strict-verify). Per-peer TLS overrides are not supported for dynamic peers; use `config.yaml` if you need different TLS material for a specific peer.

### Replacing Dynamic Peers

Re-running `peer add` with the same address stops the old connection, cancels its reconnect backoff, and starts a fresh dial with the new transport/ID settings. Config-file peers cannot be replaced at runtime.

### Short Agent ID Prefixes

The `--target` flag accepts short agent ID prefixes, same as the `route` and `forward` commands.
