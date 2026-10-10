---
title: Agent Endpoints
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-wiring.png" alt="Mole managing agents" style={{maxWidth: '180px'}} />
</div>

# Agent Endpoints

Query any agent in your mesh from any other agent. Get status, routes, and peer lists without direct access to the remote machine.

**Quick examples:**

```bash
# List all known agents
curl http://localhost:8080/agents

# Get specific agent's status
curl http://localhost:8080/agents/abc123def456/

# See what routes an agent advertises
curl http://localhost:8080/agents/abc123def456/routes
```

## GET /agents

List all known agents in the mesh.

**Response:**

```json
[
  {
    "id": "abc123def456789012345678901234ab",
    "short": "abc123de",
    "display_name": "Agent 1",
    "local": true
  },
  {
    "id": "def456789012345678901234567890cd",
    "short": "def45678",
    "local": false
  }
]
```

## GET /agents/\{agent-id\}

Get status from specific agent.

**Response:**

```json
{
  "agent_id": "abc123def456789012345678901234ab",
  "running": true,
  "peer_count": 3,
  "stream_count": 42,
  "route_count": 5,
  "socks5_running": true,
  "exit_running": false,
  "can_sign_management": false,
  "accepts_signed_management": true
}
```

`can_sign_management` reports whether the remote agent holds the management signing private key (can initiate privileged operations). `accepts_signed_management` reports whether it holds the public key (accepts signed requests from others). Use these to decide which mesh agents can be offered as targets for management commands, file transfer, and dynamic config actions. See [Authentication](/security/authentication).

## GET /agents/\{agent-id\}/routes

Get route table from specific agent.

**Response:**

```json
[
  {
    "network": "10.0.0.0/8",
    "next_hop": "def456...",
    "origin": "abc123...",
    "metric": 1,
    "hop_count": 2
  }
]
```

## GET /agents/\{agent-id\}/peers

Get peer list from specific agent.

**Response:**

```json
["abc123def456789012345678901234ab", "def456789012345678901234567890cd"]
```

## GET /agents/\{agent-id\}/interfaces

List the IP addresses assigned to a remote agent's up interfaces. Same response
shape as the local [GET /api/interfaces](/api/dashboard#get-apiinterfaces); used
to pick a source IP when binding a route on a remote exit agent. Read-only.

## GET /agents/\{agent-id\}/shell

WebSocket endpoint for management command sessions.

See [Management Commands WebSocket API](/api/shell).

## GET /agents/\{agent-id\}/icmp

WebSocket endpoint for ICMP ping sessions.

See [ICMP Ping WebSocket API](/api/icmp).

## POST /agents/\{agent-id\}/file/upload

Upload file to remote agent.

See [File Transfer Endpoints](/api/file-transfer).

## POST /agents/\{agent-id\}/file/download

Download file from remote agent.

See [File Transfer Endpoints](/api/file-transfer).

## POST /agents/\{agent-id\}/file/browse

Browse filesystem on remote agent.

See [File Transfer Endpoints](/api/file-transfer).

## POST /agents/\{agent-id\}/routes/manage

Manage dynamic routes on remote agent.

See [Route Management](/api/route-management).

## POST /agents/\{agent-id\}/forward/manage

Manage dynamic forward listeners on remote agent.

See [Forward Management](/api/forward-management).

## POST /agents/\{agent-id\}/display-name/manage

Manage display name on remote agent.

See [Display Name Management](/api/display-name-management).
