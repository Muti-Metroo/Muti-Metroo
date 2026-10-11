---
title: Sleep Endpoints
description: Put the mesh to sleep, wake it, and read an agent's sleep state over HTTP.
sidebar_position: 14
---

# Sleep Endpoints

These endpoints control [sleep mode](/features/sleep-mode): `POST /sleep` and `POST /wake` send a command that spreads to every agent in the mesh, and `GET /sleep/status` reports the state of the agent serving the request. They belong to the remote API group (`http.remote_api`, enabled by default).

| Endpoint | Description |
| -------- | ----------- |
| `POST /sleep` | Put this agent and the mesh to sleep |
| `POST /wake` | Wake this agent and the mesh |
| `GET /sleep/status` | Sleep state of this agent |

`POST /sleep` and `POST /wake` need `sleep.enabled: true` on the agent serving the request. When it has `management.signing_private_key`, the command is signed; agents with `management.signing_public_key` ignore unsigned or wrongly signed commands. A successful response means the serving agent changed state and sent the command; it does not confirm that other agents received it. Check each agent with `GET /sleep/status`. See [Sleep Configuration](/configuration/sleep).

## `POST /sleep`

Sends the sleep command to all peers, then closes peer connections, stops the SOCKS and transport listeners, and starts polling at `sleep.poll_interval`. The request body is ignored.

```bash
curl -X POST http://localhost:8080/sleep
```

```json
{
  "message": "sleep command triggered",
  "status": "triggered"
}
```

## `POST /wake`

Wakes this agent immediately (reconnects peers and restarts its services), then re-sends the wake command at intervals so that sleeping agents receive it during one of their polls. The request body is ignored.

The response is sent only after the re-send period ends: twice `sleep.poll_interval` plus `sleep.poll_duration`, and at least 5 seconds. With the default settings that is 10 minutes 30 seconds, which is longer than `http.write_timeout`, so the client may receive no response at all even though the wake succeeded. Confirm the result with `GET /sleep/status`.

```bash
curl -X POST http://localhost:8080/wake
```

```json
{
  "message": "wake command triggered",
  "status": "triggered"
}
```

## `GET /sleep/status`

```bash
curl http://localhost:8080/sleep/status
```

**Response (200)** while sleeping:

```json
{
  "state": "SLEEPING",
  "enabled": true,
  "sleep_start_time": "2026-10-11T14:56:03.519736+03:00",
  "last_poll_time": "0001-01-01T00:00:00Z",
  "next_poll_time": "2026-10-11T15:00:40.59777454+03:00",
  "queued_peers": 0,
  "poll_interval_sec": 300,
  "poll_interval_jitter": 0.3
}
```

**Response (200)** while awake:

```json
{
  "state": "AWAKE",
  "enabled": true,
  "sleep_start_time": "0001-01-01T00:00:00Z",
  "last_poll_time": "0001-01-01T00:00:00Z",
  "next_poll_time": "0001-01-01T00:00:00Z",
  "queued_peers": 0,
  "poll_interval_sec": 300,
  "poll_interval_jitter": 0.3
}
```

| Field | Type | Description |
| ----- | ---- | ----------- |
| `state` | string | `AWAKE`, `SLEEPING`, or `POLLING` |
| `enabled` | boolean | `sleep.enabled` |
| `sleep_start_time` | string | When the agent went to sleep |
| `last_poll_time` | string | When the last poll started |
| `next_poll_time` | string | When the next poll is scheduled |
| `queued_peers` | integer | Sleeping peers for which this agent holds queued state |
| `poll_interval_sec` | number | `sleep.poll_interval` in seconds |
| `poll_interval_jitter` | number | `sleep.poll_interval_jitter` |

Timestamps that do not apply are reported as `0001-01-01T00:00:00Z`.

## Errors

| Status | Body | Cause |
| ------ | ---- | ----- |
| 404 | HTML page | `http.remote_api` is disabled |
| 405 | `method not allowed` | Wrong method (`/sleep` and `/wake` need `POST`, `/sleep/status` needs `GET`) |
| 500 | `{"error":"already sleeping","status":"error"}` | `POST /sleep` while asleep |
| 500 | `{"error":"not sleeping","status":"error"}` | `POST /wake` while awake |
| 503 | `sleep mode not enabled` | `sleep.enabled` is `false` |

## See Also

- [Sleep Mode](/features/sleep-mode) - How sleep and polling work
- [Sleep Configuration](/configuration/sleep) - Poll interval, jitter, and persistence
- [CLI - sleep](/cli/sleep) - The `sleep`, `wake`, and status commands
