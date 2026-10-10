# Sleep Mode

Sleep mode saves power and resources by putting the mesh into hibernation - agents close all peer connections and enter an idle state, periodically polling for queued messages.

## Overview

When sleep mode is enabled, agents transition between three states:

- **AWAKE**: Normal operation with all connections active
- **SLEEPING**: Idle state with all peer connections closed
- **POLLING**: Briefly reconnected to receive queued messages

Sleep and wake commands flood through the mesh, allowing you to hibernate or wake the entire network from any connected agent.

## Use Cases

- **On-demand deployments**: Keep agents idle until explicitly needed
- **Resource conservation**: Reduce network and memory footprint during inactive periods
- **Power saving**: Keep agents in a low-power idle state when not in use
- **Scheduled operations**: Sleep during off-hours, wake for maintenance windows

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

### Configuration Options

| Option                 | Default | Description                         |
| ---------------------- | ------- | ----------------------------------- |
| `enabled`              | `false` | Enable sleep mode                   |
| `poll_interval`        | `5m`    | Base interval between poll cycles   |
| `poll_interval_jitter` | `0.3`   | Timing jitter fraction (0.0-1.0)    |
| `poll_duration`        | `30s`   | How long to stay connected per poll |
| `persist_state`        | `true`  | Save sleep state to disk            |
| `max_queued_messages`  | `1000`  | Queue limit per sleeping peer       |
| `auto_sleep_on_start`  | `false` | Start in sleep mode automatically   |

## CLI Commands

### Put Mesh to Sleep

```bash
muti-metroo sleep

# Via a specific agent
muti-metroo sleep -a 192.168.1.10:8080
```

### Wake the Mesh

```bash
muti-metroo wake

# Via a specific agent
muti-metroo wake -a 192.168.1.10:8080
```

Both `muti-metroo sleep` and `muti-metroo wake` prompt for `[y/N]` confirmation before
flooding the command. Pass `--yes` (or `-y`) to skip the prompt in scripts.
On non-TTY stdin without `--yes`, the CLI refuses with exit code 6
(confirmation required) so cron jobs and CI pipelines must opt in:

```bash
muti-metroo sleep --yes
muti-metroo wake --yes
```

### Check Status

```bash
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

1. A SLEEP_COMMAND floods to all connected peers
2. Each agent closes all peer connections
3. SOCKS5 and listener services stop
4. A poll timer is scheduled with jittered timing

### Poll Cycles

While sleeping, agents periodically wake briefly to:

1. Reconnect to configured peers
2. Receive queued route advertisements and node info
3. Check for wake commands
4. Disconnect and return to sleep

The poll interval includes configurable jitter to avoid synchronized polling bursts across agents.

### Waking Up

When wake is triggered:

1. The agent reconnects to all configured peers
2. SOCKS5 and listener services restart
3. A WAKE_COMMAND floods to the mesh
4. Normal operation resumes

## State Persistence

When `persist_state` is enabled, sleep state survives agent restarts:

- An agent sleeping when stopped resumes sleeping when restarted
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

1. Initialize identity, listeners, and the sleep manager
2. Immediately enter sleep mode (before any peer dials complete)
3. Remain idle until explicitly woken

Because step 2 fires before peers connect, an agent with `auto_sleep_on_start: true`
will never establish outbound peer sessions on its own -- it relies on the
deterministic poll windows or an inbound wake command to attach to the mesh.

## HTTP API

Sleep mode exposes HTTP endpoints:

```bash
# Put mesh to sleep
curl -X POST http://localhost:8080/sleep

# Wake the mesh
curl -X POST http://localhost:8080/wake

# Check status
curl http://localhost:8080/sleep/status
```

## Scheduled Sleep

Use cron to schedule sleep/wake cycles. The `curl` form below works directly
against the HTTP API; if you prefer the CLI, remember to pass `--yes` -- a
bare `muti-metroo sleep` in cron has no TTY and will refuse with exit code 6.

```bash
# Sleep at 6 PM
0 18 * * * curl -X POST http://localhost:8080/sleep
0 18 * * * muti-metroo sleep --yes              # CLI equivalent

# Wake at 8 AM
0 8 * * * curl -X POST http://localhost:8080/wake
0 8 * * * muti-metroo wake --yes
```

## Example Configurations

### Standard Sleep Mode

```yaml
sleep:
  enabled: true
  poll_interval: 5m
  poll_interval_jitter: 0.3
  poll_duration: 30s
```

### High-Frequency Polling

For environments where timely responsiveness matters:

```yaml
sleep:
  enabled: true
  poll_interval: 1m
  poll_interval_jitter: 0.2
  poll_duration: 15s
```

### Low-Activity Mode

Minimize network activity:

```yaml
sleep:
  enabled: true
  poll_interval: 30m
  poll_interval_jitter: 0.5
  poll_duration: 20s
  auto_sleep_on_start: true
```

## Command Authentication

By default, sleep/wake commands are not authenticated. In untrusted environments, configure signing keys to prevent unauthorized mesh hibernation.

### Generate Signing Keys

```bash
muti-metroo signing-key generate
```

### Configure Agents

All agents verify signatures:

```yaml
management:
  signing_public_key: "a1b2c3d4..."
```

Operators sign commands:

```yaml
management:
  signing_public_key: "a1b2c3d4..."
  signing_private_key: "e5f6a7b8..."
```

When signing keys are configured:

- Commands are automatically signed when issued
- Agents verify signatures before processing
- Invalid or unsigned commands are rejected

See **Management Key Encryption** chapter for details.

## Limitations

- **Command propagation**: Sleep/wake commands only reach connected agents
- **Isolated agents**: Agents not connected when sleep is triggered remain awake
- **Poll timing**: Sleeping agents may miss real-time events between polls
- **Unsigned commands**: Without signing keys, any party with API access can trigger sleep/wake

## Troubleshooting

### Agent Not Entering Sleep

- Verify `sleep.enabled: true` in configuration
- Check that the HTTP API is accessible
- Review agent logs for sleep-related messages

### Wake Command Not Propagating

- Ensure at least one agent is awake or polling
- Check that agents have matching peer configurations
- Verify network connectivity between agents

### State Not Persisting

- Confirm `persist_state: true` in configuration
- Check that the data directory is writable
- Verify the agent has clean shutdown (state saved on stop)

### Commands Being Rejected

If sleep/wake commands are rejected due to signature verification:

- Verify all agents have the same `signing_public_key`
- Verify the operator node has the matching `signing_private_key`
- Check that agent clocks are synchronized (within 5 minutes)
- Review agent logs for "signature verification failed" messages
