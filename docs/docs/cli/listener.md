# Listener Commands

Commands for managing dynamic transport listeners.

Transport listeners are the QUIC/HTTP2/WebSocket sockets that accept inbound peer connections (the top-level `listeners:` config block). Dynamic listeners let you open or close one on an already-running agent without editing `config.yaml` and restarting. A common use case: bring up a second ingress on a different port or transport to migrate peers, or expose a plaintext WebSocket endpoint behind a reverse proxy on demand.

Listeners are uniquely keyed by listen address. Dynamic listeners persist across agent restarts when `agent.data_dir` is set (see [Dynamic Config Persistence](/configuration/dynamic-config-persistence)) and are restored across sleep/wake. They reuse the agent's global TLS certificate (or an auto-generated self-signed one) and the global mTLS setting.

## listener add

Add a dynamic transport listener.

```bash
muti-metroo listener add <transport> <address> [flags]
```

### Description

Opens a new listener on the target agent. `<transport>` is one of `quic`, `h2`, or `ws`; `<address>` is the bind address (`:4433`, `0.0.0.0:4433`, `127.0.0.1:3002`). HTTP/2 and WebSocket listeners require `--path`.

The listener reuses the agent's global TLS configuration. `--plaintext` starts a WebSocket listener without TLS, for use behind a trusted reverse proxy. Supply `--advertise-address` to publish a public dial target via NodeInfo so other agents can discover and dial the listener.

When `agent.data_dir` is configured the listener survives agent restarts via the [dynamic config journal](/configuration/dynamic-config-persistence); if no `data_dir` is set (or the journal is not writable) it remains ephemeral and is lost on restart.

### Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--agent` | `-a` | `localhost:8080` | Agent API address (host:port or http(s):// URL) |
| `--target` | `-t` | | Target agent ID (omit for local agent) |
| `--path` | | | HTTP path for h2/ws listeners (e.g. `/ws/`) |
| `--plaintext` | | `false` | Start a WebSocket listener without TLS (ws only) |
| `--advertise-address` | | | Public `host:port` to advertise to the mesh |
| `--advertise-path` | | | Path to advertise for h2/ws (defaults to `--path`) |
| `--advertise-transport` | | | Transport to advertise (defaults to the listener transport) |

### Examples

```bash
# Open a new QUIC listener on the local agent
muti-metroo listener add quic :4434

# Open a plaintext WebSocket listener behind a reverse proxy
muti-metroo listener add ws 127.0.0.1:3002 --path /ws/ --plaintext

# Open a listener and advertise its public dial target to the mesh
muti-metroo listener add quic :4433 --advertise-address vps.example.com:4433

# Tell remote agent D (reachable via mesh) to open a listener
muti-metroo listener add quic :4433 --target <agent-d-id>
```

### Output

```
Listener added: listener :4434 added (transport=quic)
```

---

## listener remove

Remove a dynamic transport listener.

```bash
muti-metroo listener remove <address> [flags]
```

### Description

Stops accepting new inbound connections on the listener at the given address and tears the socket down. Existing peer connections established through it are not affected.

Only dynamic listeners can be removed this way. Listeners declared in the `listeners:` config are protected and cannot be removed without restarting the agent.

This command prompts for confirmation; pass `--yes` / `-y` to skip in scripts.

### Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--agent` | `-a` | `localhost:8080` | Agent API address (host:port or http(s):// URL) |
| `--target` | `-t` | | Target agent ID (omit for local agent) |

### Examples

```bash
# Remove a dynamic listener from the local agent
muti-metroo listener remove :4434

# Remove from a remote agent
muti-metroo listener remove :4434 -t abc123def456
```

### Output

```
Listener removed: listener :4434 removed
```

---

## listener list

List all transport listeners.

```bash
muti-metroo listener list [flags]
```

### Description

Displays every transport listener known to the agent: static (config-file) and dynamic (runtime). Reports transport, address, path, TLS mode, and whether a dial target is advertised.

### Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--agent` | `-a` | `localhost:8080` | Agent API address (host:port or http(s):// URL) |
| `--target` | `-t` | | Target agent ID (omit for local agent) |
| `--json` | | `false` | Output in JSON format |

### Examples

```bash
# List listeners on local agent
muti-metroo listener list

# List listeners on remote agent
muti-metroo listener list -t abc123def456

# JSON output
muti-metroo listener list --json
```

### Output

Standard output:

```
Listeners (2)
ADDRESS                  TRANS  PATH         TLS        TYPE     ADVERTISE
:4433                    quic   -            tls        static   vps.example.com:4433
127.0.0.1:3002           ws     /ws/         plaintext  dynamic  -
```

JSON output:

```json
{
  "status": "ok",
  "listeners": [
    {
      "transport": "quic",
      "address": ":4433",
      "plaintext": false,
      "advertise_address": "vps.example.com:4433",
      "dynamic": false
    },
    {
      "transport": "ws",
      "address": "127.0.0.1:3002",
      "path": "/ws/",
      "plaintext": true,
      "dynamic": true
    }
  ]
}
```

---

## Authorization

Dynamic listener modifications follow the same rules as dynamic route, forward, and peer management. If an agent has `management.public_key` configured but does NOT have the corresponding `management.private_key`, listener add/remove/list commands are rejected with HTTP 403 Forbidden. Add/remove on a remote agent (via `--target`) additionally require a valid management signature; `list` is read-only.

---

## Important Notes

### Persistence

Dynamic listeners survive agent restarts when `agent.data_dir` is configured: every add/remove is appended to a [dynamic config journal](/configuration/dynamic-config-persistence) under `data_dir/dynamic-config.log` and replayed on the next start. They are also restored when the agent wakes from sleep mode.

If no `data_dir` is configured, or the journal file cannot be written, the runtime mutation still succeeds but remains ephemeral: the listener is lost on the next restart.

Listeners declared in `config.yaml` take precedence and cannot be replaced or removed at runtime.

### TLS Settings

Dynamic listeners inherit the agent's global TLS certificate, key, and mTLS setting. Per-listener certificate overrides are not supported at runtime; use `config.yaml` if you need different TLS material for a specific listener. `--plaintext` (ws only) starts the listener without TLS for reverse-proxy deployments.

### Replacing Dynamic Listeners

Re-running `listener add` with the same address stops the old listener and starts a fresh one with the new settings. Config-file listeners cannot be replaced at runtime.
