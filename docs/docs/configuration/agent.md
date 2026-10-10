---
title: Agent
sidebar_position: 2
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-inspecting.png" alt="Mole configuring agent" style={{maxWidth: '180px'}} />
</div>

# Agent Configuration

Give your agent a name and control how much it logs. The identity is auto-generated on first run - you usually don't need to set it manually.

**Most common settings:**
```yaml
agent:
  display_name: "Office Gateway"   # Shows up in dashboard
  data_dir: "./data"               # Where identity is stored
  log_level: "info"                # debug, info, warn, error
```

## Configuration

```yaml
agent:
  # Agent ID
  id: "auto"                    # "auto" or 32-character hex string

  # Human-readable name
  display_name: "My Agent"      # Shown in dashboard and logs

  # Data directory (optional when identity is in config)
  data_dir: "./data"            # For agent_id and keypair files

  # Logging
  log_level: "info"             # debug, info, warn, error
  log_format: "text"            # text, json

  # Startup delay
  startup_delay: 0s             # Delay before network activity (e.g., 90s, 2m)

  # X25519 keypair for E2E encryption (optional - for single-file deployment)
  private_key: ""               # 64-character hex string
  public_key: ""                # Optional, derived from private_key
```

## Agent ID

Every agent has a unique 128-bit identifier.

### Auto-generation

```yaml
agent:
  id: "auto"
  data_dir: "./data"
```

On first run:
1. Check if `./data/agent_id` exists
2. If not, generate new random ID
3. Save to `./data/agent_id`
4. Use for all peer communications

### Manual ID

Specify a specific ID:

```yaml
agent:
  id: "a1b2c3d4e5f6789012345678901234ab"
```

Requirements:
- Exactly 32 hexadecimal characters
- Unique across your mesh

### Viewing Agent ID

```bash
# From file
cat ./data/agent_id

# From running agent
curl http://localhost:8080/healthz | jq '.agent_id'

# From logs
INFO  Agent ID: a1b2c3d4e5f6789012345678901234ab
```

## Display Name

Human-readable name for the agent:

```yaml
agent:
  display_name: "Office Gateway"
```

Used in:
- Web dashboard
- Node info advertisements
- Logs

Supports Unicode:

```yaml
agent:
  display_name: "Moscow Gateway"
```

If not set, the agent ID is used for display.

The display name can also be changed at runtime using `muti-metroo display-name set` or the HTTP API. Dynamic names are ephemeral and revert to the config value on restart. See [Display Name CLI](/cli/display-name) and [Display Name API](/api/display-name-management).

## Data Directory

Where agent persists state:

```yaml
agent:
  data_dir: "./data"
```

Contents:
- `agent_id` - Agent identity file
- `agent_key` - X25519 private key
- `agent_key.pub` - X25519 public key

### When Data Directory is Optional

The data directory becomes **optional** when you specify identity directly in config:

```yaml
agent:
  id: "a1b2c3d4e5f6789012345678901234ab"
  private_key: "48bbea6c0c9be254bde983c92c8a53db759f27e51a6ae77fd9cca81895a5d57c"
  # data_dir not needed - identity is fully in config
```

This enables single-file deployments where the agent can run without any external files.

### Permissions

```bash
# Create with appropriate permissions
mkdir -p ./data
chmod 700 ./data
```

### Shared Storage Warning

Do not share `data_dir` between multiple agents - each agent needs its own identity.

## Identity Keypair

Each agent has an X25519 keypair for end-to-end encryption.

### File-Based Identity (Default)

By default, keys are stored in `data_dir`:

```yaml
agent:
  data_dir: "./data"
```

Keys are auto-generated on first run and stored as:
- `{data_dir}/agent_key` - Private key (permissions 0600)
- `{data_dir}/agent_key.pub` - Public key (permissions 0644)

### Config-Based Identity

For single-file deployments, specify keys directly:

```yaml
agent:
  id: "a1b2c3d4e5f6789012345678901234ab"
  private_key: "48bbea6c0c9be254bde983c92c8a53db759f27e51a6ae77fd9cca81895a5d57c"
```

The `public_key` field is optional - it's automatically derived from `private_key`.

### Generating Keys

To generate a keypair for config:

```bash
# Generate keys in a temporary directory
muti-metroo init -d /tmp/keys

# View the private key
cat /tmp/keys/agent_key

# View the public key
cat /tmp/keys/agent_key.pub

# Clean up
rm -rf /tmp/keys
```

### Single-File Deployment

When using the setup wizard with embedded config, identity is automatically embedded:

```bash
muti-metroo setup
# Choose "Embed in binary" for configuration delivery
```

The wizard automatically:
1. Sets `agent.id` to the generated ID
2. Sets `agent.private_key` to the generated private key
3. Clears `agent.data_dir`

The resulting binary can run without any external files.

## Logging

### Log Level

```yaml
agent:
  log_level: "info"    # debug, info, warn, error
```

| Level | Description |
|-------|-------------|
| `debug` | Verbose debugging (frames, routing) |
| `info` | Normal operation (connections, streams) |
| `warn` | Warnings (reconnections, timeouts) |
| `error` | Errors only |

### Log Format

```yaml
agent:
  log_format: "text"    # text, json
```

**Text format** (human-readable):

```
2025-01-15T10:30:45Z INFO  Starting Muti Metroo agent
2025-01-15T10:30:45Z INFO  Agent ID: a1b2c3d4...
2025-01-15T10:30:45Z INFO  SOCKS5 server started on 127.0.0.1:1080
```

**JSON format** (machine-readable):

```json
{"time":"2025-01-15T10:30:45Z","level":"INFO","msg":"Starting Muti Metroo agent"}
{"time":"2025-01-15T10:30:45Z","level":"INFO","msg":"Agent ID","agent_id":"a1b2c3d4..."}
```

### Log Destination

Logs go to stderr by default. Redirect as needed:

```bash
# To file
muti-metroo run -c config.yaml 2> agent.log

# With rotation (using logrotate)
muti-metroo run -c config.yaml 2>> /var/log/muti-metroo/agent.log
```

### Changing Log Level

To change the log level, update the configuration file and restart the agent:

```yaml
agent:
  log_level: "debug"
```

Then restart the agent to apply changes.

## Startup Delay

Delay all network activity (listeners, peer connections, SOCKS5, etc.) for a specified duration after the process starts. The agent process is alive but idle during the delay.

```yaml
agent:
  startup_delay: 90s
```

Use cases:
- Staggering agent startups across a fleet
- Waiting for dependent services to initialize
- Gradual rollout of mesh connectivity

The delay can also be set via CLI flag, which overrides the config value:

```bash
muti-metroo run -c config.yaml --startup-delay 2m
```

During the delay, the agent can be cleanly shut down with `Ctrl+C` or `SIGTERM`.

## Environment Variables

Use environment variables for deployment flexibility:

```yaml
agent:
  id: "${AGENT_ID:-auto}"
  display_name: "${AGENT_NAME:-}"
  data_dir: "${DATA_DIR:-./data}"
  log_level: "${LOG_LEVEL:-info}"
  log_format: "${LOG_FORMAT:-text}"
```

## Examples

### Development

```yaml
agent:
  id: "auto"
  display_name: "Dev Agent"
  data_dir: "./data"
  log_level: "debug"
  log_format: "text"
```

### Production

```yaml
agent:
  id: "auto"
  display_name: "${HOSTNAME}"
  data_dir: "/var/lib/muti-metroo"
  log_level: "info"
  log_format: "json"
```

### Docker

```yaml
agent:
  id: "auto"
  display_name: "${AGENT_NAME:-container}"
  data_dir: "/app/data"
  log_level: "${LOG_LEVEL:-info}"
  log_format: "json"
```

## Related

- [Getting Started](/getting-started/quick-start) - Initial setup
- [Listeners](/configuration/listeners) - Transport configuration
- [Deployment](/deployment/scenarios) - Production deployment
