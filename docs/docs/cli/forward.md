# Forward Commands

Commands for managing dynamic forward listeners and endpoints.

## forward add

Add a dynamic forward listener.

```bash
muti-metroo forward add <key> <address> [flags]
```

### Description

Adds a new forward listener that accepts TCP connections and forwards them through the mesh to port forward endpoints matching the given routing key.

Dynamic listeners persist across restarts when `agent.data_dir` is configured (see [Dynamic Config Persistence](/configuration/dynamic-config-persistence)); when no `data_dir` is set they remain ephemeral. For always-on listeners, use the `forward.listeners` configuration.

### Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--agent` | `-a` | `localhost:8080` | Agent API address (host:port or http(s):// URL) |
| `--target` | `-t` | | Target agent ID (omit for local agent) |
| `--max-connections` | | `0` | Maximum concurrent connections (0 = unlimited) |

### Examples

```bash
# Add listener on local agent
muti-metroo forward add web-server :9090

# Add listener with connection limit
muti-metroo forward add web-server :9090 --max-connections 100

# Add listener on remote agent
muti-metroo forward add web-server :9090 -t abc123def456

# Add listener using short agent ID prefix
muti-metroo forward add web-server :9090 -t abc123

# Via a specific API server
muti-metroo forward add web-server :9090 -a 192.168.1.10:8080 -t def456
```

### Output

```
Forward listener added: forward listener "web-server" added on [::]:9090
```

### Use Cases

- **Ad Hoc Port Forwarding**: Expose services through the mesh without config changes
- **Dynamic Service Discovery**: Add listeners based on runtime conditions
- **Testing**: Temporarily forward traffic without modifying configuration files

---

## forward remove

Remove a dynamic forward listener.

```bash
muti-metroo forward remove <key> [flags]
```

### Description

Removes a previously added dynamic forward listener. The listener is stopped and removed from the agent.

Only dynamic listeners can be removed via this command. Listeners defined in the `forward.listeners` configuration are protected and cannot be removed without restarting the agent.

### Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--agent` | `-a` | `localhost:8080` | Agent API address (host:port or http(s):// URL) |
| `--target` | `-t` | | Target agent ID (omit for local agent) |

### Examples

```bash
# Remove listener from local agent
muti-metroo forward remove web-server

# Remove listener from remote agent
muti-metroo forward remove web-server -t abc123def456

# Via a specific API server
muti-metroo forward remove web-server -a 192.168.1.10:8080 -t def456
```

### Output

```
Forward listener removed: forward listener "web-server" removed
```

---

## forward list

List all forward listeners.

```bash
muti-metroo forward list [flags]
```

### Description

Displays all forward listeners on the target agent, including both static (config-file) and dynamic (runtime) listeners.

### Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--agent` | `-a` | `localhost:8080` | Agent API address (host:port or http(s):// URL) |
| `--target` | `-t` | | Target agent ID (omit for local agent) |
| `--json` | | `false` | Output in JSON format |

### Examples

```bash
# List listeners on local agent
muti-metroo forward list

# List listeners on remote agent
muti-metroo forward list -t abc123def456

# List listeners with JSON output
muti-metroo forward list --json

# Via a specific API server
muti-metroo forward list -a 192.168.1.10:8080 -t def456
```

### Output

Standard output:

```
Forward Listeners (2)
KEY                  ADDRESS                  TYPE     MAX_CONN
web-server           [::]:9090                static   unlimited
api-server           [::]:8081                dynamic  100
```

JSON output:

```json
{
  "status": "ok",
  "listeners": [
    {
      "key": "web-server",
      "address": "[::]:9090",
      "max_connections": 0,
      "dynamic": false
    },
    {
      "key": "api-server",
      "address": "[::]:8081",
      "max_connections": 100,
      "dynamic": true
    }
  ]
}
```

---

## forward endpoint add

Add a dynamic forward endpoint.

```bash
muti-metroo forward endpoint add <key> <target> [flags]
```

### Description

Registers a new routing key on the agent pointing at a local `host:port` target. The endpoint is advertised to the mesh so listeners on peer agents can reach the target through this agent.

Dynamic endpoints persist across restarts when `agent.data_dir` is configured (see [Dynamic Config Persistence](/configuration/dynamic-config-persistence)); when no `data_dir` is set they remain ephemeral. For always-on endpoints, use the `forward.endpoints` configuration.

### Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--agent` | `-a` | `localhost:8080` | Agent API address (host:port or http(s):// URL) |
| `--target` | `-t` | | Target agent ID (omit for local agent) |

### Examples

```bash
# Add endpoint on local agent
muti-metroo forward endpoint add web-server 127.0.0.1:3000

# Add on a remote agent
muti-metroo forward endpoint add web-server 127.0.0.1:3000 -t abc123def456

# Via a specific API server
muti-metroo forward endpoint add web-server 127.0.0.1:3000 -a 192.168.1.10:8080 -t def456
```

### Output

```
Forward endpoint added: forward endpoint "web-server" added -> 127.0.0.1:3000
```

---

## forward endpoint remove

Remove a dynamic forward endpoint.

```bash
muti-metroo forward endpoint remove <key> [flags]
```

### Description

Removes a previously added dynamic forward endpoint. The routing key is unregistered from the handler and a per-route `RouteWithdraw` is flooded to all connected peers immediately, so they prune the key from their routing tables without waiting for `route_ttl` to elapse. Only dynamic endpoints can be removed; endpoints declared in `forward.endpoints` are protected.

### Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--agent` | `-a` | `localhost:8080` | Agent API address (host:port or http(s):// URL) |
| `--target` | `-t` | | Target agent ID (omit for local agent) |

### Examples

```bash
# Remove endpoint from local agent
muti-metroo forward endpoint remove web-server

# Remove endpoint from remote agent
muti-metroo forward endpoint remove web-server -t abc123def456
```

### Output

```
Forward endpoint removed: forward endpoint "web-server" removed
```

---

## forward endpoint list

List all forward endpoints.

```bash
muti-metroo forward endpoint list [flags]
```

### Description

Displays all forward endpoints on the target agent, including both static (config-file) and dynamic (runtime) entries.

### Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--agent` | `-a` | `localhost:8080` | Agent API address (host:port or http(s):// URL) |
| `--target` | `-t` | | Target agent ID (omit for local agent) |
| `--json` | | `false` | Output in JSON format |

### Examples

```bash
# List endpoints on local agent
muti-metroo forward endpoint list

# List endpoints on a remote agent
muti-metroo forward endpoint list -t abc123def456

# JSON output
muti-metroo forward endpoint list --json
```

### Output

Standard output:

```
Forward Endpoints (2)
KEY                  TARGET                   TYPE
web-server           127.0.0.1:3000           static
api-server           127.0.0.1:4000           dynamic
```

JSON output:

```json
{
  "status": "ok",
  "endpoints": [
    {
      "key": "web-server",
      "target": "127.0.0.1:3000",
      "dynamic": false
    },
    {
      "key": "api-server",
      "target": "127.0.0.1:4000",
      "dynamic": true
    }
  ]
}
```

---

## Authorization

### Management Key Restriction

Dynamic forward listener modifications are restricted when the mesh is configured with management key encryption, following the same rules as dynamic route management.

If an agent has `management.public_key` configured but does NOT have the corresponding `management.private_key`, forward add/remove commands are rejected with HTTP 403 Forbidden.

---

## Important Notes

### Persistence

Dynamic listeners survive agent restarts when `agent.data_dir` is configured: every add/remove is appended to a [dynamic config journal](/configuration/dynamic-config-persistence) under `data_dir/dynamic-config.log` and replayed on the next start. The address string is persisted verbatim, so `:0` (ephemeral port) produces a different bound port on each restart; specify an explicit port if you need a stable address across restarts.

If no `data_dir` is configured, or the journal file cannot be written (bad permissions, read-only filesystem, disk full), the runtime mutation still succeeds but remains ephemeral.

For always-on listeners, use the `forward.listeners` configuration in `config.yaml`:

```yaml
forward:
  listeners:
    - key: "web-server"
      address: ":9090"
      max_connections: 100
```

### Node Info Propagation

After adding or removing a listener, the change is immediately advertised to peers via node info. No manual action is required.

### Replacing Dynamic Listeners

Re-adding a dynamic listener with the same key stops the old listener and starts a new one with the updated address and settings. Config-file listeners cannot be replaced.

### Short Agent ID Prefixes

The `--target` flag accepts short agent ID prefixes, same as the `route` command.
