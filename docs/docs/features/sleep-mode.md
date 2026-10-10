# Sleep Mode

Sleep mode enables mesh hibernation - agents can close all peer connections and enter an idle state, periodically polling for queued messages. This is useful for on-demand deployments that should remain idle until needed.

## Overview

When sleep mode is enabled, agents can transition between three states:

- **AWAKE**: Normal operation with all connections active
- **SLEEPING**: Idle state with all peer connections closed
- **POLLING**: Briefly reconnected to receive queued messages

Sleep and wake commands flood through the mesh, allowing you to hibernate or wake the entire network from any connected agent.

## Use Cases

- **On-demand deployments**: Keep agents idle until explicitly needed
- **Resource conservation**: Reduce network and memory footprint during inactive periods
- **Traffic pattern reduction**: Minimize network connections when not in use
- **Scheduled operations**: Sleep during off-hours, wake for maintenance windows

## Basic Usage

### Putting the Mesh to Sleep

```bash
# Put the mesh to sleep via local agent
muti-metroo sleep

# Via a specific agent
muti-metroo sleep -a 192.168.1.10:8080
```

### Waking the Mesh

```bash
# Wake the mesh via local agent
muti-metroo wake

# Via a specific agent
muti-metroo wake -a 192.168.1.10:8080
```

### Checking Sleep Status

```bash
# Check status
muti-metroo sleep-status

# JSON output
muti-metroo sleep-status --json
```

Example output:

```
Sleep Mode Status
=================
State:          SLEEPING
Enabled:        true
Sleep Started:  2026-01-19T10:30:00Z
Last Poll:      2026-01-19T10:35:00Z
Next Poll:      2026-01-19T10:40:00Z
Queued Peers:   2
```

## How It Works

### Entering Sleep Mode

When sleep is triggered:

1. A SLEEP_COMMAND is flooded to all connected peers
2. Each agent closes all peer connections
3. SOCKS5 and listener services are stopped
4. A poll timer is scheduled with jittered timing

### Poll Cycles

While sleeping, agents periodically wake briefly to:

1. Reconnect to configured peers
2. Receive queued route advertisements and node info
3. Check for wake commands
4. Disconnect and return to sleep

The poll interval includes configurable jitter to prevent predictable connection patterns.

### Waking Up

When wake is triggered:

1. The agent reconnects to all configured peers
2. SOCKS5 and listener services restart
3. A WAKE_COMMAND is flooded to the mesh
4. Normal operation resumes

## Configuration

Enable sleep mode in your agent configuration:

```yaml
sleep:
  enabled: true
  poll_interval: 5m # How often to poll while sleeping
  poll_interval_jitter: 0.3 # +/- 30% timing variation
  poll_duration: 30s # How long to stay connected per poll
  persist_state: true # Survive agent restarts
  max_queued_messages: 1000 # Queue limit per sleeping peer
  auto_sleep_on_start: false # Start in sleep mode
```

See [Sleep Configuration](/configuration/sleep) for details on each option.

## State Persistence

When `persist_state` is enabled, sleep state survives agent restarts. This means:

- An agent sleeping when stopped will resume sleeping when restarted
- Poll timing and command sequence are preserved
- No manual intervention needed after system reboots

## Auto-Sleep on Start

For on-demand deployments, enable `auto_sleep_on_start`:

```yaml
sleep:
  enabled: true
  auto_sleep_on_start: true
```

The agent will:

1. Start and initialize normally
2. Wait 5 seconds for connections to establish
3. Automatically enter sleep mode
4. Remain idle until explicitly woken

## Idle Behavior

Sleep mode keeps idle agents quiet and resource-friendly:

- **Jittered polling**: Connection timing varies by +/- configured percentage, so many sleeping agents do not reconnect in lockstep
- **No persistent connections**: All connections closed between polls
- **Minimal poll duration**: Brief reconnection windows

## Command Authentication

By default, any party that can reach an agent's HTTP API can trigger sleep/wake commands. For untrusted environments, configure signing keys to authenticate commands.

### Generate Signing Keys

```bash
muti-metroo signing-key generate
```

### Configure Agents

All agents need the public key to verify signatures:

```yaml
management:
  signing_public_key: "a1b2c3d4..."
```

Operator nodes also need the private key to sign commands:

```yaml
management:
  signing_public_key: "a1b2c3d4..."
  signing_private_key: "e5f6a7b8..."
```

When signing keys are configured:

- Commands are automatically signed when issued via CLI or HTTP API
- Agents verify signatures before processing commands
- Unsigned or invalid commands are rejected

See [signing-key CLI](/cli/signing-key) for details.

## Limitations

- **Command propagation**: Sleep/wake commands only reach connected agents
- **Isolated agents**: Agents not connected when sleep is triggered remain awake
- **Poll timing**: Sleeping agents may miss real-time events between polls
- **Unsigned commands**: Without signing keys, any party with API access can trigger sleep/wake

## HTTP API

Sleep mode also exposes HTTP endpoints:

- `POST /sleep` - Trigger mesh-wide sleep
- `POST /wake` - Trigger mesh-wide wake
- `GET /sleep/status` - Get current sleep status

See [Sleep API](/api/sleep) for details.

## Example: Scheduled Sleep

Use cron or systemd timers to schedule sleep/wake cycles:

```bash
# Sleep at 6 PM
0 18 * * * curl -X POST http://localhost:8080/sleep

# Wake at 8 AM
0 8 * * * curl -X POST http://localhost:8080/wake
```

## Troubleshooting

### Agent not entering sleep

- Verify `sleep.enabled: true` in configuration
- Check that the HTTP API is accessible
- Review agent logs for sleep-related messages

### Wake command not propagating

- Ensure at least one agent is awake or polling
- Check that agents have matching peer configurations
- Verify network connectivity between agents

### State not persisting

- Confirm `persist_state: true` in configuration
- Check that the data directory is writable
- Verify the agent has clean shutdown (state saved on stop)

### Sleep/wake commands rejected

If commands are being rejected due to signature verification:

- Verify all agents have the same `signing_public_key`
- Verify the operator node has the matching `signing_private_key`
- Check that agent clocks are synchronized (within 5 minutes)
- Review agent logs for "signature verification failed" messages
