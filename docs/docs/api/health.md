---
title: Health Endpoints
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-wiring.png" alt="Mole checking health" style={{maxWidth: '180px'}} />
</div>

# Health Endpoints

Check if an agent is running and ready to handle traffic. Use these for load balancer health checks, container orchestration probes, or monitoring scripts.

**Quick check:**

```bash
curl http://localhost:8080/healthz
# Returns: peer count, stream count, route count, and status
```

## GET /health

Basic health check.

**Response:**

```
OK
```

**Status:** 200 OK

## GET /healthz

Detailed health check with statistics.

**Response (200 OK):**

```json
{
  "status": "healthy",
  "running": true,
  "peer_count": 3,
  "stream_count": 42,
  "route_count": 5,
  "socks5_running": true,
  "exit_handler_running": false,
  "can_sign_management": true,
  "accepts_signed_management": true
}
```

`can_sign_management` is `true` when this agent holds the management signing private key and can therefore initiate management command sessions, file transfers, dynamic config mutations, and sleep/wake. `accepts_signed_management` is `true` when this agent holds the matching public key and will verify signed requests from other agents. Web dashboards and clients use these flags to enable or disable management UI without probing each feature. See [Authentication](/security/authentication) for the full authorization model.

**Response (503 Service Unavailable):**

```json
{
  "status": "unavailable",
  "running": false
}
```

Returns 503 Service Unavailable if agent is not running.

## GET /ready

Readiness probe for container orchestration.

**Response:**

```
READY
```

Returns 200 if agent is ready to accept traffic.
Returns 503 with "NOT READY" if agent is not running.

## Examples

```bash
# Basic health check
curl http://localhost:8080/health

# Detailed health
curl http://localhost:8080/healthz

# Readiness
curl http://localhost:8080/ready
```

## See Also

- [CLI - Status](/cli/status) - Check agent status from CLI
- [HTTP Configuration](/configuration/http) - Configure HTTP endpoints
- [Deployment Scenarios](/deployment/scenarios) - Using health checks in production
