# Display Name Commands

Commands for managing the agent display name at runtime.

## display-name set

Set the agent display name dynamically.

```bash
muti-metroo display-name set <name> [flags]
```

### Description

Sets a new display name for the target agent. The name is immediately propagated to all connected peers and appears in the dashboard, topology view, and agent listings.

Dynamic display names persist across restarts when `agent.data_dir` is configured (see [Dynamic Config Persistence](/configuration/dynamic-config-persistence)); when no `data_dir` is set they remain ephemeral. For always-on names, use the `agent.display_name` configuration.

Setting an empty name reverts to the configured display name value.

### Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--agent` | `-a` | `localhost:8080` | Agent API address (host:port or http(s):// URL) |
| `--target` | `-t` | | Target agent ID (omit for local agent) |

### Examples

```bash
# Set display name on local agent
muti-metroo display-name set "gateway-us-east"

# Set display name on remote agent
muti-metroo display-name set "exit-eu-west" -t abc123def456

# Set using short agent ID prefix
muti-metroo display-name set "transit-node" -t abc123

# Via a specific API server
muti-metroo display-name set "backup-exit" -a 192.168.1.10:8080 -t def456

# Revert to config value
muti-metroo display-name set ""
```

### Output

```
Display name set: gateway-us-east
```

---

## display-name get

Get the current agent display name.

```bash
muti-metroo display-name get [flags]
```

### Description

Retrieves the current effective display name from the target agent. The returned name reflects the active display name, whether set dynamically or from configuration.

### Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--agent` | `-a` | `localhost:8080` | Agent API address (host:port or http(s):// URL) |
| `--target` | `-t` | | Target agent ID (omit for local agent) |

### Examples

```bash
# Get display name from local agent
muti-metroo display-name get

# Get display name from remote agent
muti-metroo display-name get -t abc123def456

# Via a specific API server
muti-metroo display-name get -a 192.168.1.10:8080 -t def456
```

### Output

```
gateway-us-east
```

---

## Authorization

### Management Key Restriction

Display name modifications are restricted when the mesh is configured with management key encryption, following the same rules as dynamic route management.

If an agent has `management.public_key` configured but does NOT have the corresponding `management.private_key`, the `display-name set` command is rejected with HTTP 403 Forbidden.

---

## Important Notes

### Persistence

Dynamic display names survive agent restarts when `agent.data_dir` is configured: every `display-name set` is appended to a [dynamic config journal](/configuration/dynamic-config-persistence) under `data_dir/dynamic-config.log` and replayed on the next start so the agent comes back up with the same name.

If no `data_dir` is configured, or the journal file cannot be written (bad permissions, read-only filesystem, disk full), the runtime change still succeeds but remains ephemeral and reverts to `agent.display_name` on the next restart.

For an always-on name, use `agent.display_name` in `config.yaml`:

```yaml
agent:
  display_name: "gateway-us-east"
```

### Name Propagation

After setting a display name, the change is immediately advertised to all connected peers via route and node info advertisements. No manual action is required.

### Short Agent ID Prefixes

The `--target` flag accepts short agent ID prefixes, same as the `route` and `forward` commands.

---

## Workflow Examples

### Rename Agents in a Fleet

Set descriptive names on all agents from a single control point:

```bash
#!/bin/bash
AGENT="http://localhost:8080"

muti-metroo display-name set "ingress-us-east" -a "$AGENT" -t abc123
muti-metroo display-name set "transit-eu-west" -a "$AGENT" -t def456
muti-metroo display-name set "exit-ap-south"   -a "$AGENT" -t ghi789
```

### Verify Display Name

```bash
# Set and verify
muti-metroo display-name set "my-gateway"
muti-metroo display-name get
# Output: my-gateway
```

### Revert to Config Name

```bash
# Revert to config value
muti-metroo display-name set ""
muti-metroo display-name get
# Output: (config display_name or short agent ID)
```
