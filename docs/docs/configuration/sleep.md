---
title: Sleep
description: Configuration reference for the sleep section, covering mesh hibernation, poll timing, state persistence, message queueing, and deterministic listening windows.
sidebar_position: 20
---

# Sleep

The `sleep` section controls mesh hibernation. A sleeping agent closes its peer connections and stops its listeners and SOCKS proxy, then reconnects briefly at intervals (polls) to receive queued updates and wake commands. Sleep and wake commands are flooded to every agent in the mesh with [`muti-metroo sleep`](/cli/sleep) and [`muti-metroo wake`](/cli/sleep).

## Configuration

```yaml
sleep:
  enabled: true
  poll_interval: 5m
  poll_interval_jitter: 0.3
  poll_duration: 30s
  persist_state: true
  max_queued_messages: 1000
  auto_sleep_on_start: false
  deterministic_windows:
    enabled: false
    window_length: 30s
    clock_tolerance: 5s
    epoch: ""
```

## Keys

| Key                                     | Type     | Default | Description                                                                                                                      |
| --------------------------------------- | -------- | ------- | -------------------------------------------------------------------------------------------------------------------------------- |
| `enabled`                               | bool     | `false` | Allow this agent to sleep. When false, the agent ignores sleep commands and the sleep API endpoints return errors.             |
| `poll_interval`                         | duration | `5m`    | Base time between polls while sleeping.                                                                                          |
| `poll_interval_jitter`                  | float    | `0.3`   | Random variation applied to each poll interval, as a fraction (0.3 = 70% to 130% of `poll_interval`). `0` disables jitter.     |
| `poll_duration`                         | duration | `30s`   | How long the agent stays connected during each poll.                                                                            |
| `persist_state`                         | bool     | `true`  | Save the sleep state to `data_dir/sleep_state.json` on every transition.                                                        |
| `max_queued_messages`                   | int      | `1000`  | Maximum route and node info updates queued for each sleeping peer; the oldest are dropped first. `0` or less uses `1000`.       |
| `auto_sleep_on_start`                   | bool     | `false` | Enter sleep mode as soon as the agent has started.                                                                               |
| `deterministic_windows.enabled`         | bool     | `false` | Poll at times derived from the agent ID instead of random jitter.                                                               |
| `deterministic_windows.window_length`   | duration | `30s`   | Length of each listening window. A value not shorter than `poll_interval` is replaced with one sixth of `poll_interval`.        |
| `deterministic_windows.clock_tolerance` | duration | `5s`    | Allowance for clock differences between agents.                                                                                  |
| `deterministic_windows.epoch`           | string   | `""`    | Reference time for window calculation in RFC 3339 format, for example `2026-01-01T00:00:00Z`. Empty or unparseable uses the Unix epoch. |

## Poll timing

While sleeping, the agent waits `poll_interval` (varied by `poll_interval_jitter`), reconnects to its peers for `poll_duration`, then disconnects again. A wake command reaches a sleeping agent at its next poll at the latest, so `poll_interval` bounds how long waking takes. `poll_duration` must be long enough to connect and receive queued state.

| Goal                     | `poll_interval` | `poll_duration` |
| ------------------------ | --------------- | --------------- |
| Faster wake              | `1m`            | `15s`           |
| Default                  | `5m`            | `30s`           |
| Less network activity    | `30m`           | `20s`           |

## State file

With `persist_state: true` and `agent.data_dir` set, the agent writes its state to `sleep_state.json` on every transition and reads it back at startup:

```json
{
  "state": 1,
  "sleep_start_time": "2026-01-19T10:30:00Z",
  "last_poll_time": "2026-01-19T10:35:00Z",
  "command_seq": 5
}
```

`state` is `0` (awake), `1` (sleeping), or `2` (polling). The file is not encrypted.

An agent that starts with a saved sleeping state reports `SLEEPING`, but in the current release it does not stop its SOCKS5 proxy and listeners or schedule polls. After restarting an agent that was asleep, run `muti-metroo wake` and then `muti-metroo sleep` against it to put it back to sleep. `auto_sleep_on_start` is not affected.

## Deterministic windows

With `deterministic_windows.enabled: true`, each sleeping agent polls in a window whose position within every `poll_interval` cycle is derived from its agent ID and `epoch`. Other agents can compute when it will be reachable. All agents in a mesh need the same `poll_interval` and `epoch` for the windows to line up.

## Command signing

Sleep and wake commands are verified with `management.signing_public_key` when it is set; without it, unsigned commands are accepted. See [Management](/configuration/management#command-signing-keys).

## Examples

### On-demand agent

```yaml
sleep:
  enabled: true
  auto_sleep_on_start: true
  poll_interval: 15m
  poll_duration: 20s
```

### Predictable polling windows

```yaml
sleep:
  enabled: true
  poll_interval: 10m
  deterministic_windows:
    enabled: true
    window_length: 30s
    epoch: "2026-01-01T00:00:00Z"
```

## See Also

- [Sleep Mode usage](/features/sleep-mode) for the sleep and wake workflow
- [sleep CLI](/cli/sleep) for `sleep`, `wake`, and `sleep-status`
- [Sleep API](/api/sleep) for the HTTP endpoints
- [Management](/configuration/management) for signing sleep and wake commands
