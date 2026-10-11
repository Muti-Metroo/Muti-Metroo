---
title: Agent Endpoints
description: List the agents in the mesh and read status, routes, peers, and interface addresses of any remote agent.
sidebar_position: 3
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-wiring.png" alt="Mole managing agents" style={{maxWidth: '180px'}} />
</div>

# Agent Endpoints

These endpoints list the agents the local agent knows about and read state from any remote agent over the mesh, without direct network access to it. They belong to the remote API group (`http.remote_api`, enabled by default) and accept only `GET`.

`{id}` is the full 32-character agent ID. The `/agents/{id}` endpoints do not work for the local agent's own ID; use [`/healthz`](/api/health#get-healthz) and the [dashboard endpoints](/api/dashboard) for local state.

Other `/agents/{id}/...` endpoints are documented with their feature:

| Endpoint | Page |
| -------- | ---- |
| `POST /agents/{id}/routes/manage` | [Route Management](/api/route-management#post-agentsidroutesmanage) |
| `POST /agents/{id}/route/trace` | [Route Trace](/api/route-trace#post-agentsidroutetrace) |
| `POST /agents/{id}/forward/manage`, `POST /agents/{id}/forward/endpoints/manage` | [Forward Management](/api/forward-management) |
| `POST /agents/{id}/peers/manage` | [Peer Management](/api/peer-management#post-agentsidpeersmanage) |
| `POST /agents/{id}/listeners/manage` | [Listener Management](/api/listener-management#post-agentsidlistenersmanage) |
| `POST /agents/{id}/display-name/manage` | [Display Name Management](/api/display-name-management#post-agentsiddisplay-namemanage) |
| `GET /agents/{id}/shell` | [Management Commands WebSocket](/api/shell) |
| `GET /agents/{id}/icmp` | [ICMP Ping WebSocket](/api/icmp) |
| `POST /agents/{id}/file/upload`, `file/download`, `file/browse` | [File Transfer](/api/file-transfer) |
| `POST /agents/{id}/probe` | [Probe](/api/probe#post-agentsidprobe) |
| `POST /agents/{id}/shutdown` | [Shutdown](/api/stop#post-agentsidshutdown) |

## `GET /agents`

Lists the local agent and every agent known through route and node advertisements.

```bash
curl http://localhost:8080/agents
```

**Response (200):**

```json
[
  {
    "display_name": "gateway",
    "id": "93cdf211bb05aa48900a61ab6699fbdd",
    "local": true,
    "short": "93cdf211"
  },
  {
    "id": "653422d2fc10b554514c5cbe6e6cd6cd",
    "local": false,
    "short": "653422d2"
  },
  {
    "id": "331974cd4a0be0d7f945ed9d367a0211",
    "local": false,
    "short": "331974cd"
  }
]
```

| Field | Description |
| ----- | ----------- |
| `id` | Full agent ID |
| `short` | First 8 characters of the ID |
| `display_name` | Display name; present only on the local entry |
| `local` | `true` for the agent serving the request |

Use [`GET /api/nodes`](/api/dashboard#get-apinodes) when you need display names and system details for every agent. When the agent has `management.public_key` without `management.private_key`, the list contains only the local agent.

## `GET /agents/{id}`

Fetches the status of a remote agent.

```bash
curl http://localhost:8080/agents/331974cd4a0be0d7f945ed9d367a0211
```

**Response (200):**

```json
{
  "accepts_signed_management": true,
  "agent_id": "331974cd4a0be0d7f945ed9d367a0211",
  "can_sign_management": false,
  "exit_running": true,
  "peer_count": 1,
  "route_count": 2,
  "running": true,
  "socks5_running": false,
  "stream_count": 0
}
```

The fields match [`GET /healthz`](/api/health#get-healthz), with `agent_id` added and the exit flag named `exit_running`.

## `GET /agents/{id}/routes`

Returns the remote agent's CIDR route table. Domain routes are not included.

```bash
curl http://localhost:8080/agents/331974cd4a0be0d7f945ed9d367a0211/routes
```

**Response (200):**

```json
[
  {
    "network": "10.20.0.0/16",
    "next_hop": "331974cd",
    "origin": "331974cd",
    "metric": 0,
    "hop_count": 0
  },
  {
    "network": "192.0.2.0/24",
    "next_hop": "331974cd",
    "origin": "331974cd",
    "metric": 0,
    "hop_count": 0
  }
]
```

| Field | Description |
| ----- | ----------- |
| `network` | CIDR prefix |
| `next_hop` | Short ID of the next hop, as seen from the remote agent |
| `origin` | Short ID of the agent that advertises the route |
| `metric` | Route metric |
| `hop_count` | Hops from the remote agent to the origin; `0` for the remote agent's own routes |

## `GET /agents/{id}/peers`

Returns the full IDs of the remote agent's connected peers.

```bash
curl http://localhost:8080/agents/653422d2fc10b554514c5cbe6e6cd6cd/peers
```

**Response (200):**

```json
["93cdf211bb05aa48900a61ab6699fbdd", "331974cd4a0be0d7f945ed9d367a0211"]
```

## `GET /agents/{id}/interfaces`

Lists every IP address on the remote agent's up interfaces. The response has the same shape as [`GET /api/interfaces`](/api/dashboard#get-apiinterfaces). Use it to choose a `source_ip` when binding a route on a remote exit agent.

```bash
curl http://localhost:8080/agents/331974cd4a0be0d7f945ed9d367a0211/interfaces
```

**Response (200):**

```json
{
  "addresses": [
    {
      "interface": "lo0",
      "ip": "127.0.0.1",
      "family": "ipv4",
      "loopback": true,
      "link_local": false,
      "global": false
    },
    {
      "interface": "en0",
      "ip": "192.168.1.10",
      "family": "ipv4",
      "loopback": false,
      "link_local": false,
      "global": true
    }
  ]
}
```

## Errors

| Status | Body | Cause |
| ------ | ---- | ----- |
| 400 | `agent ID required` | Path is `/agents/` with no ID |
| 400 | `invalid agent ID format` | ID is not a full 32-character hex ID |
| 403 | `topology restricted: management key decryption unavailable` | `management.public_key` is set without `management.private_key` |
| 404 | HTML page | `http.remote_api` is disabled |
| 405 | `method not allowed` | Method other than `GET` |
| 502 | `failed to fetch: <reason>` | The agent is unknown, unreachable, or did not answer within 30 seconds; for example `failed to fetch: no route to agent 93cdf211` |

## See Also

- [HTTP API Overview](/api/overview) - Endpoint index and authentication
- [Dashboard Endpoints](/api/dashboard) - Topology and node details for the whole mesh
- [CLI - status](/cli/status) - Read agent status from the command line
- [Management Keys](/configuration/management) - Topology encryption and its restrictions
