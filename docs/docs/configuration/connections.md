---
title: Connections
description: Configuration reference for the connections section, covering peer keepalive, dead-peer detection, dial timeout, TCP keepalive, and reconnection backoff.
sidebar_position: 14
---

# Connections

The `connections` section tunes the transport connections between agents: how often an idle link is probed, when a silent peer is considered dead, and how the agent redials a peer it lost. All settings apply to every peer; [`peers[].tcp_keepalive`](/configuration/peers#keys) is the only per-peer override.

## Configuration

```yaml
connections:
  idle_threshold: 5m      # Keepalive interval; also the target read idle timeout
  timeout: 90s            # Dial timeout and dead-peer grace period
  keepalive_jitter: 0.2   # +/- 20% on each keepalive interval
  tcp_keepalive: 30s      # SO_KEEPALIVE period for h2 and ws connections
  write_timeout: 30s      # Per-frame write deadline on peer connections
  reconnect:
    initial_delay: 1s
    max_delay: 60s
    multiplier: 2.0
    jitter: 0.2
    max_retries: 0        # 0 = retry forever
```

## Keys

| Key                       | Type     | Default | Description                                                                                                                                         |
| ------------------------- | -------- | ------- | --------------------------------------------------------------------------------------------------------------------------------------------------- |
| `idle_threshold`          | duration | `5m`    | Interval between keepalive messages on each peer connection. Also the read idle timeout for connections that exits and forward endpoints open to their targets, and the time a SOCKS client has to complete its handshake. |
| `timeout`                 | duration | `90s`   | Timeout for dialing a peer. A peer that sends nothing for `idle_threshold` plus `timeout`, or leaves a keepalive unanswered for `timeout`, is disconnected. |
| `keepalive_jitter`        | float    | `0.2`   | Random variation applied to each keepalive interval, as a fraction (0.2 = plus or minus 20%). `0` disables jitter.                                  |
| `tcp_keepalive`           | duration | `30s`   | Operating system TCP keepalive period on `h2` and `ws` connections, which keeps NAT mappings alive and detects dead TCP links. `0` disables it. QUIC is not affected. Must not be negative. |
| `write_timeout`           | duration | `30s`   | Deadline applied to each frame write on a peer connection. A peer that stops reading fails the write and the connection is torn down, instead of blocking all later frames behind the stalled one. `0` disables it. Must not be negative. |
| `reconnect.initial_delay` | duration | `1s`    | Delay before the first redial.                                                                                                                      |
| `reconnect.max_delay`     | duration | `60s`   | Upper bound for the redial delay.                                                                                                                   |
| `reconnect.multiplier`    | float    | `2.0`   | Factor applied to the delay after each attempt.                                                                                                     |
| `reconnect.jitter`        | float    | `0.2`   | Random variation applied to each redial delay, as a fraction.                                                                                       |
| `reconnect.max_retries`   | int      | `0`     | Number of redial attempts before the agent stops trying a peer. `0` retries forever.                                                                |

## Keepalive and dead-peer detection

Each peer connection sends a keepalive every `idle_threshold`, varied by `keepalive_jitter` (never less than one second). A peer is declared dead when either:

- nothing has been **received** on the connection for `idle_threshold` plus `timeout` (6.5 minutes with the defaults); outbound traffic, including the keepalive itself, does not reset this timer, or
- a keepalive goes unanswered: the peer must return a `KEEPALIVE_ACK` within `timeout` of each probe.

Lower values detect dead peers sooner and produce more background traffic:

```yaml
connections:
  idle_threshold: 30s
  timeout: 30s
```

`idle_threshold` also limits how long an exit or forward endpoint waits for data from its target: a target connection that receives nothing for this long is closed. Quiet long-lived sessions, such as an idle SSH session through the SOCKS proxy, are affected, so do not set it lower than the longest silence you need to tolerate.

## Reconnection

When a configured or runtime peer disconnects, or cannot be reached at startup, the agent redials it. The first delay is `initial_delay`; after each attempt the delay is multiplied by `multiplier` and capped at `max_delay`. Each delay is varied by `jitter`. With the defaults the delays are about 1s, 2s, 4s, 8s, 16s, 32s, then 60s for every further attempt.

With `max_retries` greater than zero, the agent gives up on a peer after that many redial attempts and does not dial it again until restarted or re-added with [`muti-metroo peer add`](/cli/peer).

## See Also

- [Peers](/configuration/peers) for the per-peer `tcp_keepalive` override
- [Routing](/configuration/routing) for route advertisement and expiry
- [Limits](/configuration/limits) for stream limits and the stream open timeout
- [Sleep](/configuration/sleep) for closing peer connections on purpose
