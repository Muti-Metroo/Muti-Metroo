---
title: HTTP API Overview
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-wiring.png" alt="Mole connecting APIs" style={{maxWidth: '180px'}} />
</div>

# HTTP API Reference

Query agent status, trigger actions, and build integrations. Every agent exposes an HTTP API for monitoring and management.

**Quick reference:**

| I want to...                                     | Endpoint                                                                |
| ------------------------------------------------ | ----------------------------------------------------------------------- |
| Check if an agent is running                     | [GET /healthz](/api/health)                                             |
| See all agents in the mesh                       | [GET /agents](/api/agents)                                              |
| Push route updates immediately                   | [POST /routes/advertise](/api/routes)                                   |
| Add, remove, or list dynamic routes              | [POST /routes/manage](/api/route-management)                            |
| Manage routes on a remote agent                  | [POST /agents/\{id\}/routes/manage](/api/route-management)              |
| Explain which route an agent would pick          | [POST /route/trace](/api/route-trace)                                   |
| Trace route selection on a remote agent          | [POST /agents/\{id\}/route/trace](/api/route-trace)                     |
| Set or get agent display name                    | [POST /display-name/manage](/api/display-name-management)               |
| Manage display name on remote agent              | [POST /agents/\{id\}/display-name/manage](/api/display-name-management) |
| Add, remove, or list dynamic peer connections    | [POST /peers/manage](/api/peer-management)                              |
| Manage peers on a remote agent                   | [POST /agents/\{id\}/peers/manage](/api/peer-management)                |
| Add, remove, or list dynamic transport listeners | [POST /listeners/manage](/api/listener-management)                      |
| Manage listeners on a remote agent               | [POST /agents/\{id\}/listeners/manage](/api/listener-management)        |
| Run commands on remote agents                    | [WebSocket /agents/\{id\}/shell](/api/shell)                            |
| Transfer files to/from agents                    | [POST /agents/\{id\}/file/*](/api/file-transfer)                        |
| Gracefully stop an agent                         | [POST /shutdown](/api/stop)                                             |
| Stop a remote agent                              | [POST /agents/\{id\}/shutdown](/api/stop)                               |
| Test connectivity to all mesh agents             | [POST /api/mesh-test](/api/dashboard#getpost-apimesh-test)              |
| Probe a Muti Metroo listener (dial + handshake)        | [POST /api/probe](/api/probe)                                           |
| Probe a listener from a remote agent             | [POST /agents/\{id\}/probe](/api/probe)                                 |
| Get topology for visualization                   | [GET /api/topology](/api/dashboard)                                     |

## Base URL

```
http://localhost:8080
```

Configure via:

```yaml
http:
  enabled: true
  address: ":8080"
```

## Endpoint Categories

| Category                            | Purpose                                                        |
| ----------------------------------- | -------------------------------------------------------------- |
| [Health](/api/health)               | Health checks and readiness probes                             |
| [Agents](/api/agents)               | Remote agent status and management                             |
| [Routes](/api/routes)               | Route management and triggers                                  |
| [Management Commands](/api/shell)   | Run management commands (interactive and streaming)            |
| [File Transfer](/api/file-transfer) | File upload/download                                           |
| [Dashboard](/api/dashboard)         | Topology data, dashboard overview, and mesh connectivity test  |
| [Probe](/api/probe)                 | Connectivity probe (dial + handshake) for individual listeners |

## Authentication

### Bearer Token (API-wide)

When `http.token_hash` is configured, all non-health endpoints require a bearer token:

```
Authorization: Bearer <token>
```

**Exempt endpoints** (always accessible without a token):

- `/health`, `/healthz`, `/ready` -- health probes
- `/`, `/logo.png`, `/notfound.png` -- splash page assets

**Query parameter fallback** for WebSocket clients that cannot set headers:

```
ws://localhost:8080/agents/{id}/shell?token=<token>
```

**CLI usage:**

```bash
# Flag
muti-metroo status --token my-secret-token

# Environment variable
export MUTI_METROO_TOKEN=my-secret-token
muti-metroo status
```

**Generate a token hash:**

```bash
muti-metroo hash
# Paste the output into config:
# http:
#   token_hash: "$2a$10$..."
```

When `token_hash` is empty (default), no API-wide authentication is enforced.

### Management-key Authorization

Privileged operations -- management commands, file transfer, every dynamic mutation (routes, peers, forward listeners and endpoints, hosts, display name), and sleep/wake -- additionally require an Ed25519 signature from the mesh **management signing key** on top of the bearer token. See [Management Key Configuration](/configuration/management) for the full scope and the deployment model. There is no separate per-feature password for these operations.

## Response Formats

- **JSON**: Most endpoints return JSON
- **Plain text**: Health checks return plain text
- **Binary**: File downloads return binary data

## Error Responses

```json
{
  "error": "error message"
}
```

Common HTTP status codes:

- `200 OK`: Success
- `400 Bad Request`: Invalid request
- `401 Unauthorized`: Authentication failed
- `404 Not Found`: Resource not found
- `500 Internal Server Error`: Server error
