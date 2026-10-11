---
title: Health Endpoints
description: Liveness, readiness, and statistics endpoints for load balancers, orchestrators, and monitoring.
sidebar_position: 2
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-wiring.png" alt="Mole checking health" style={{maxWidth: '180px'}} />
</div>

# Health Endpoints

Three endpoints report whether the agent is running. They are always enabled, including with `http.minimal: true`, and never require the bearer token, so load balancers and container orchestrators can call them without credentials.

| Endpoint | Response | Use for |
| -------- | -------- | ------- |
| `GET /health` | `OK` (plain text) | Liveness: the HTTP server responds |
| `GET /ready` | `READY` or `NOT READY` (plain text) | Readiness: the agent is running |
| `GET /healthz` | JSON statistics | Monitoring scripts and dashboards |

All three accept only `GET`; other methods return `405 Method Not Allowed`.

## `GET /health`

Returns `200 OK` with the body `OK` whenever the HTTP server is up.

```bash
curl http://localhost:8080/health
```

```text
OK
```

## `GET /ready`

Returns `200 OK` with `READY` when the agent is running, and `503 Service Unavailable` with `NOT READY` otherwise.

```bash
curl http://localhost:8080/ready
```

```text
READY
```

## `GET /healthz`

Returns agent statistics as JSON.

```bash
curl http://localhost:8080/healthz
```

**Response (200):**

```json
{
  "accepts_signed_management": true,
  "can_sign_management": true,
  "exit_handler_running": false,
  "peer_count": 1,
  "route_count": 2,
  "running": true,
  "socks5_running": true,
  "status": "healthy",
  "stream_count": 0
}
```

| Field | Type | Description |
| ----- | ---- | ----------- |
| `status` | string | `healthy` |
| `running` | boolean | Always `true` in a 200 response |
| `peer_count` | integer | Connected peers |
| `stream_count` | integer | Open virtual streams |
| `route_count` | integer | Entries in the CIDR route table, local and learned |
| `socks5_running` | boolean | The SOCKS listener is running |
| `exit_handler_running` | boolean | The exit handler is running (exit enabled, or a dynamic route was added) |
| `can_sign_management` | boolean | The agent has `management.signing_private_key` and can initiate privileged operations |
| `accepts_signed_management` | boolean | The agent has `management.signing_public_key` and executes signed privileged operations from other agents |

Clients such as Muti Metroo Manager use the two `*_management` flags to decide whether to offer management actions. See [Authentication](/security/authentication).

**Response (503)** when the agent is not running:

```json
{
  "running": false,
  "status": "unavailable"
}
```

## Container probes

A Kubernetes liveness and readiness probe pair for an agent whose HTTP server listens on port 8080. The kubelet connects to the pod IP, so `http.address` must bind an address the kubelet can reach (for example `0.0.0.0:8080`), not `127.0.0.1`.

```yaml
livenessProbe:
  httpGet:
    path: /health
    port: 8080
readinessProbe:
  httpGet:
    path: /ready
    port: 8080
```

## See Also

- [HTTP API Overview](/api/overview) - Endpoint groups and authentication
- [HTTP Configuration](/configuration/http) - Enable the HTTP server
- [CLI - status](/cli/status) - Check agent status from the command line
- [Docker Deployment](/deployment/docker) - Health checks in containers
