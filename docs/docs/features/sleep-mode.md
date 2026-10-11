---
title: Sleep Mode
description: Put a whole mesh to sleep so agents close their connections and only reconnect briefly to check for a wake command, then wake it again.
sidebar_position: 10
---

# Sleep Mode

Sleep mode lets an operator idle a mesh without stopping its agents. A sleep command spreads from agent to agent; each agent closes its peer connections, stops its SOCKS5 proxy and transport listeners, and then reconnects only briefly at intervals to check for a wake command. A wake command spreads the same way and returns every agent to normal operation.

Use it for deployments that should stay in place but quiet until needed, or to reduce network activity and resource use during inactive periods.

## Agent states

| State | Meaning |
| ----- | ------- |
| `AWAKE` | Normal operation |
| `SLEEPING` | Peer connections closed, SOCKS5 proxy and transport listeners stopped |
| `POLLING` | Briefly reconnected to peers to receive queued updates and wake commands |

## Enable sleep mode

Sleep mode is off by default. Enable it on every agent that should take part:

```yaml
sleep:
  enabled: true
  poll_interval: 5m
  poll_interval_jitter: 0.3
  poll_duration: 30s
```

A sleeping agent polls every `poll_interval`, varied by up to `poll_interval_jitter` (30% here) so that many agents do not reconnect at the same moment, and stays connected for `poll_duration`. Instead of random jitter, agents can poll in deterministic windows derived from their agent ID. All options, including state persistence and deterministic windows, are in [Sleep Configuration](/configuration/sleep).

## Put the mesh to sleep

Run `sleep` against any awake agent:

```bash
muti-metroo sleep
```

The command asks for confirmation; pass `--yes` in scripts. The agent forwards the sleep command to its peers, which forward it to theirs, and every agent that receives it goes to sleep. Agents that are disconnected at that moment do not receive it and stay awake.

## Wake the mesh

```bash
muti-metroo wake
```

The agent you run it against wakes immediately, reconnects to its peers, and sends the wake command on. Sleeping agents receive it at their next poll, so the whole mesh can take up to one `poll_interval` (plus jitter) to wake.

## Check the state

```bash
muti-metroo sleep-status
```

```text
Sleep Mode Status
=================
State:          SLEEPING
Enabled:        true
Sleep Started:  2026-10-11T10:30:00Z
Last Poll:      2026-10-11T10:35:12Z
Next Poll:      2026-10-11T10:40:41Z
Queued Peers:   2
```

`Queued Peers` counts sleeping peers for which this agent holds updates until their next poll. Use `--json` for machine-readable output, and `-a` to query another agent.

## Start agents asleep

With `auto_sleep_on_start: true`, an agent goes to sleep as soon as it starts and stays asleep until it receives a wake command:

```yaml
sleep:
  enabled: true
  auto_sleep_on_start: true
```

When `persist_state` is enabled (the default), an agent that was sleeping when it stopped resumes sleeping when it restarts.

## Restrict who can sleep and wake the mesh

Sleep and wake commands carry a timestamp, and every agent ignores commands more than five minutes old or in the future, so agent clocks must be roughly synchronized.

Every agent that has `management.signing_public_key` configured also requires the command to be signed with the matching private key. An agent without a signing public key accepts unsigned sleep and wake commands from any peer. To make sure only operators can sleep the mesh:

- Configure `management.signing_public_key` on every agent.
- Keep `management.signing_private_key` only on operator agents; commands issued through those agents are signed automatically.

The agent that receives the `sleep` or `wake` request through its HTTP API acts on it whether or not it can sign it, so also protect each agent's HTTP API with `http.token_hash` or a local-only bind address. See [Authentication](/security/authentication#sleep-and-wake).

## Scheduled sleep

Use cron or a systemd timer to sleep and wake on a schedule. The HTTP API does not prompt for confirmation:

```bash
# Sleep at 18:00, wake at 08:00
0 18 * * * curl -s -X POST http://localhost:8080/sleep
0 8 * * * curl -s -X POST http://localhost:8080/wake
```

Add `-H "Authorization: Bearer <token>"` when the API requires a token. See [Sleep API](/api/sleep).

## Troubleshooting

### sleep mode not enabled

The agent you ran the command against does not have `sleep.enabled: true`.

### Some agents stay awake

They were not connected when the sleep command spread, or they do not have sleep mode enabled. Run `muti-metroo sleep` again once they are connected.

### Wake does not reach some agents

Sleeping agents only see the wake command during a poll. Wait for one full `poll_interval`, then check `muti-metroo sleep-status` on an agent near them. Agents that have no awake or polling peer within reach cannot receive it.

### Commands are rejected

Agent logs show "sleep command rejected" or "wake command rejected" when a command fails verification:

- The command was unsigned or signed with a different key than the agent's `management.signing_public_key`. Issue it through an agent that holds the matching private key.
- The agent's clock differs from the issuing agent's clock by more than five minutes.

### Agent does not stay asleep after restart

Check that `sleep.persist_state` is `true` and that `agent.data_dir` is writable; the state is stored in `sleep_state.json` there.

## See Also

- [Sleep Configuration](/configuration/sleep): poll timing, persistence, and deterministic windows
- [sleep CLI](/cli/sleep): `sleep`, `wake`, and `sleep-status`
- [Sleep API](/api/sleep): HTTP endpoints
- [signing-key CLI](/cli/signing-key): generate the key pair that authenticates commands
- [Authentication](/security/authentication): what the signing key protects
