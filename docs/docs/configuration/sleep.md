---
title: Sleep
sidebar_position: 14
---

# Sleep Configuration

The `sleep` section configures mesh hibernation mode.

## Basic Configuration

```yaml
sleep:
  enabled: true
  poll_interval: 5m
  poll_interval_jitter: 0.3
  poll_duration: 30s
  persist_state: true
  max_queued_messages: 1000
  auto_sleep_on_start: false
```

## Options

### enabled

Whether sleep mode is available on this agent.

```yaml
sleep:
  enabled: true # Enable sleep mode
```

- **Type**: boolean
- **Default**: `false`

When disabled, sleep/wake commands are ignored and CLI/API endpoints return errors.

### poll_interval

Base interval between poll cycles while sleeping.

```yaml
sleep:
  poll_interval: 5m
```

- **Type**: duration
- **Default**: `5m`

Actual interval varies by +/- `poll_interval_jitter`. Shorter intervals mean more frequent connectivity checks but more network activity.

### poll_interval_jitter

Timing jitter fraction applied to poll interval.

```yaml
sleep:
  poll_interval_jitter: 0.3 # +/- 30%
```

- **Type**: float (0.0 - 1.0)
- **Default**: `0.3`

A value of 0.3 means the actual poll interval will be anywhere from 70% to 130% of `poll_interval`. This helps avoid predictable traffic patterns.

### poll_duration

How long to stay connected during each poll cycle.

```yaml
sleep:
  poll_duration: 30s
```

- **Type**: duration
- **Default**: `30s`

Must be long enough to:

- Establish connections to peers
- Receive queued state updates
- Check for wake commands

Too short may result in missed messages; too long increases exposure.

### persist_state

Save sleep state to disk for restart recovery.

```yaml
sleep:
  persist_state: true
```

- **Type**: boolean
- **Default**: `true`

When enabled, sleep state is saved to `sleep_state.json` in the data directory. The agent resumes its previous sleep state after restart.

### max_queued_messages

Maximum number of messages queued per sleeping peer.

```yaml
sleep:
  max_queued_messages: 1000
```

- **Type**: integer
- **Default**: `1000`

When a peer is sleeping, route advertisements and node info updates are queued. Older messages are evicted when this limit is exceeded. Set higher for meshes with frequent route changes.

### auto_sleep_on_start

Start the agent in sleep mode automatically.

```yaml
sleep:
  auto_sleep_on_start: true
```

- **Type**: boolean
- **Default**: `false`

Useful for on-demand deployments. The agent will:

1. Initialize and connect briefly
2. Wait 5 seconds
3. Enter sleep mode automatically
4. Remain idle until explicitly woken

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

### No Persistence

For ephemeral deployments where state shouldn't survive restarts:

```yaml
sleep:
  enabled: true
  persist_state: false
```

## State File

When `persist_state` is enabled, state is saved to `{data_dir}/sleep_state.json`:

```json
{
  "state": 1,
  "sleep_start_time": "2026-01-19T10:30:00Z",
  "last_poll_time": "2026-01-19T10:35:00Z",
  "command_seq": 5
}
```

State values:

- `0`: AWAKE
- `1`: SLEEPING
- `2`: POLLING

## Security Considerations

- Sleep/wake commands flood to all mesh agents
- Only enable in trusted meshes or with proper access controls
- State file is unencrypted - protect the data directory
- Poll timing determines how quickly sleeping agents notice wake commands

### Command Signing

For untrusted environments, configure signing keys to authenticate sleep/wake commands. This prevents unauthorized parties from hibernating your mesh.

```yaml
management:
  signing_public_key: "a1b2c3d4..." # ALL agents - verify signatures
  signing_private_key: "e5f6a7b8..." # OPERATORS ONLY - sign commands
```

See [Management Configuration](/configuration/management) for details on signing key setup, or [signing-key CLI](/cli/signing-key) for key generation.

## Deterministic Windows

For more predictable reconnection timing, configure deterministic listening windows:

```yaml
sleep:
  enabled: true
  deterministic_windows:
    enabled: true # Use predictable windows derived from AgentID
    window_length: 30s # Duration of each listening window (max 30s)
    clock_tolerance: 5s # Clock drift tolerance
    epoch: "" # Reference point for calculations (RFC3339, default: Unix epoch)
```

When enabled, sleeping agents listen at predictable times derived from their AgentID, allowing peers to calculate when a sleeping agent will be available and time reconnection attempts efficiently.

### Options

| Option            | Type     | Default | Description                                                                   |
| ----------------- | -------- | ------- | ----------------------------------------------------------------------------- |
| `enabled`         | bool     | `false` | Use deterministic windows instead of random jitter                            |
| `window_length`   | duration | `30s`   | Duration of each listening window (max 30s)                                   |
| `clock_tolerance` | duration | `5s`    | Clock drift tolerance between agents                                          |
| `epoch`           | string   | `""`    | Reference point for window calculations (RFC3339 format, default: Unix epoch) |

All agents should use the same `epoch` for interoperability.
