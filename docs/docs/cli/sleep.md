---
title: muti-metroo sleep, wake, and sleep-status
sidebar_label: sleep / wake
sidebar_position: 24
description: Put the mesh into sleep mode, wake it, and check an agent's sleep state.
---

# muti-metroo sleep, wake, and sleep-status

`sleep` and `wake` change the sleep state of the whole mesh, starting from the agent you call. `sleep-status` reports the sleep state of a single agent. All three commands talk to an agent's HTTP API, and `sleep` and `wake` require `sleep.enabled: true` in that agent's configuration. See [sleep mode](../features/sleep-mode.md) for how sleeping agents behave and [sleep configuration](../configuration/sleep.md) for the settings.

Agents that have `management.signing_public_key` configured ignore sleep and wake commands that are not signed by the matching private key. The agent you call signs the command when it has `management.signing_private_key`. See [management configuration](../configuration/management.md).

Global flags (`--token`, `--yes`, `--no-color`) are described in [CLI overview](overview.md#global-flags).

## sleep

```bash
muti-metroo sleep [flags]
```

Put the mesh to sleep. The agent at `--agent` floods a sleep command to every agent it can reach, then closes its peer connections and stops its SOCKS5 server and transport listeners. While asleep, agents reconnect periodically to poll for queued messages.

### Flags

| Flag | Default | Description |
| --- | --- | --- |
| `-a`, `--agent` | `localhost:8080` | Agent HTTP API address. See [agent address](overview.md#agent-address). |

### Confirmation prompt

The prompt names the originating agent, the number of agents it can currently reach (including itself), and the worst-case time a later `wake` needs to reach sleeping agents, computed from the agent's `poll_interval` and jitter:

```text
Will flood SLEEP from gateway-a (70a9eb1d) to ~2 reachable mesh agents.
This closes peer connections mesh-wide; a subsequent wake takes up to ~6m30s. Continue? [y/N]:
```

Answering anything other than `y` or `yes` prints `Aborted.` and exits 0. `--yes` skips the prompt; without it, a non-terminal stdin makes the command exit 6. See [confirmation prompts](overview.md#confirmation-prompts).

### Output

```text
Sleep mode triggered: sleep command triggered
```

### Examples

```bash
muti-metroo sleep
```

```bash
muti-metroo sleep -a 192.168.1.10:8080 -y
```

## wake

```bash
muti-metroo wake [flags]
```

Wake the mesh. The agent at `--agent` leaves sleep mode, restarts its transport listeners and SOCKS5 server, reconnects to its configured peers, and floods a wake command repeatedly so that sleeping agents receive it during their next poll window.

### Flags

| Flag | Default | Description |
| --- | --- | --- |
| `-a`, `--agent` | `localhost:8080` | Agent HTTP API address. See [agent address](overview.md#agent-address). |

### Confirmation prompt

```text
Will flood WAKE from gateway-a (70a9eb1d) to all sleeping mesh agents.
Reattachment expected within ~6m30s (poll_interval=5m, +30% jitter)
Continue? [y/N]:
```

Declining, `--yes`, and non-terminal stdin behave as for `sleep`.

### Output

```text
Wake mode triggered: wake command triggered
Reattachment expected within ~6m30s (poll_interval=5m, +30% jitter)
Use 'muti-metroo sleep-status' on each agent to verify reattachment.
```

If the agent is not sleeping, the command fails with `Error: operation rejected: wake failed: not sleeping` and exits 5.

The agent answers a wake request only after it has finished re-sending the wake command: twice `sleep.poll_interval` plus `sleep.poll_duration`, 10 minutes 30 seconds with the defaults. The CLI waits 30 seconds, so with the defaults `wake` reports a timeout and exits 3 even though the agent has woken. Confirm with `muti-metroo sleep-status`.

### Examples

```bash
muti-metroo wake
```

```bash
muti-metroo wake -a 192.168.1.10:8080 -y
```

## sleep-status

```bash
muti-metroo sleep-status [flags]
```

Show the sleep state of the agent at `--agent`. This command does not prompt and does not change anything. It works on agents with sleep mode disabled, which report `AWAKE` and `Enabled: false`.

### Flags

| Flag | Default | Description |
| --- | --- | --- |
| `-a`, `--agent` | `localhost:8080` | Agent HTTP API address. See [agent address](overview.md#agent-address). |
| `--json` | `false` | Print the status as JSON. |

### Output

```text
Sleep Mode Status
=================
State:          SLEEPING
Enabled:        true
Sleep Started:  2026-10-11T14:55:04+03:00
Next Poll:      2026-10-11T15:00:50+03:00
Queued Peers:   0
```

`Sleep Started`, `Last Poll`, and `Next Poll` are printed only when they have a value.

| Field | JSON key | Description |
| --- | --- | --- |
| State | `state` | `AWAKE`, `SLEEPING`, or `POLLING`. |
| Enabled | `enabled` | Whether `sleep.enabled` is true on this agent. |
| Sleep Started | `sleep_start_time` | When the agent entered sleep mode. |
| Last Poll | `last_poll_time` | When the last poll cycle ran. |
| Next Poll | `next_poll_time` | When the next poll cycle is scheduled. |
| Queued Peers | `queued_peers` | Number of peers with messages queued for them. |

With `--json`, all fields are always present. Times that have no value are printed as `0001-01-01T00:00:00Z`:

```json
{
  "state": "AWAKE",
  "enabled": true,
  "sleep_start_time": "0001-01-01T00:00:00Z",
  "last_poll_time": "0001-01-01T00:00:00Z",
  "next_poll_time": "0001-01-01T00:00:00Z",
  "queued_peers": 0
}
```

### Examples

```bash
muti-metroo sleep-status
```

```bash
muti-metroo sleep-status -a 192.168.1.10:8080 --json | jq -r .state
```

## Scheduling sleep with cron

`sleep` and `wake` prompt by default, so scheduled runs need `--yes`:

```text
0 22 * * * /usr/local/bin/muti-metroo sleep -a localhost:8080 --yes
0 6 * * * /usr/local/bin/muti-metroo wake -a localhost:8080 --yes
```

## See Also

- [Sleep mode](../features/sleep-mode.md): how sleeping agents poll, queue messages, and wake
- [Sleep configuration](../configuration/sleep.md): `poll_interval`, jitter, and other settings
- [Sleep API](../api/sleep.md): the `/sleep`, `/wake`, and `/sleep/status` endpoints
- [signing-key](signing-key.md): generate the keypair used to sign sleep and wake commands
- [stop](stop.md): shut down a single agent instead
