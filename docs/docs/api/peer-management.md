---
title: Peer Management Endpoints
description: Tell a running agent to dial a new peer, disconnect a dynamic peer, or list its peer connections.
sidebar_position: 7
---

# Peer Management Endpoints

These endpoints add and remove outbound peer connections on a running agent, and list its current connections. A typical use is an agent that can only accept connections: an existing agent that can reach it is told to dial it. The endpoints belong to the remote API group (`http.remote_api`, enabled by default) and accept only `POST`.

| Endpoint | Description |
| -------- | ----------- |
| `POST /peers/manage` | Manage peers of the local agent |
| `POST /agents/{id}/peers/manage` | Manage peers of a remote agent |

`add` and `remove` are mutations: the agent serving the request must have `management.signing_private_key`, otherwise it returns `403`. Mutations are written to the [dynamic configuration journal](/configuration/dynamic-config-persistence) when `agent.data_dir` is set and are replayed on restart. Peers from the `peers` configuration section cannot be removed or replaced.

## `POST /peers/manage`

### Request

| Field | Type | Used by | Description |
| ----- | ---- | ------- | ----------- |
| `action` | string | all | `add`, `remove`, or `list` |
| `address` | string | `add`, `remove` | Dial address. For `quic`, `host:port`. For `h2` and `ws`, `host:port` (path `/mesh`) or a URL with a path such as `https://relay.example.com:443/mesh` or `wss://relay.example.com/mesh`. |
| `transport` | string | `add` | `quic` (default), `h2`, or `ws` |
| `id` | string | `add` | Expected agent ID of the peer. Omit or use `auto` to accept any ID. |
| `tcp_keepalive` | string | `add` | TCP keepalive period for `h2` and `ws`, as a Go duration such as `20s`. `0` disables it. Default: `connections.tcp_keepalive`. |

Dynamic peers use the agent's global TLS settings; per-peer TLS, proxy, and protocol overrides are not available. A dynamic peer reconnects with backoff when the connection drops, like a configured peer.

### `add`

Starts dialing in the background and returns immediately. A failed dial is retried with backoff; check the result with `list`. Adding an address that already exists as a dynamic peer closes the old connection and dials again with the new settings.

```bash
curl -X POST http://localhost:8080/peers/manage \
  -H "Content-Type: application/json" \
  -d '{"action": "add", "address": "203.0.113.10:4433", "id": "331974cd4a0be0d7f945ed9d367a0211"}'
```

```json
{
  "status": "ok",
  "message": "peer 203.0.113.10:4433 added (transport=quic)"
}
```

### `remove`

Closes the connection to a dynamic peer and cancels reconnection. When a connection was open, the message includes the peer's short ID.

```bash
curl -X POST http://localhost:8080/peers/manage \
  -H "Content-Type: application/json" \
  -d '{"action": "remove", "address": "203.0.113.10:4433"}'
```

```json
{
  "status": "ok",
  "message": "peer 203.0.113.10:4433 removed (disconnected 331974cd)"
}
```

### `list`

Returns outbound peers (configured and dynamic) and inbound connections accepted by this agent, sorted by address.

```bash
curl -X POST http://localhost:8080/peers/manage \
  -H "Content-Type: application/json" \
  -d '{"action": "list"}'
```

```json
{
  "status": "ok",
  "peers": [
    {
      "address": "127.0.0.1:52511",
      "transport": "quic",
      "remote_id": "653422d2fc10b554514c5cbe6e6cd6cd",
      "connected": true,
      "persistent": false,
      "dynamic": false,
      "direction": "inbound"
    },
    {
      "address": "198.51.100.20:443",
      "transport": "h2",
      "expected_id": "331974cd4a0be0d7f945ed9d367a0211",
      "remote_id": "331974cd4a0be0d7f945ed9d367a0211",
      "connected": true,
      "persistent": true,
      "dynamic": false
    },
    {
      "address": "203.0.113.10:4433",
      "transport": "quic",
      "connected": false,
      "persistent": true,
      "dynamic": true
    }
  ]
}
```

| Field | Description |
| ----- | ----------- |
| `address` | Dial address for outbound peers; the remote socket address for inbound connections |
| `transport` | `quic`, `h2`, or `ws` |
| `expected_id` | Configured expected agent ID; omitted when any ID is accepted |
| `remote_id` | Agent ID of the connected peer; omitted while disconnected |
| `connected` | A connection is open |
| `persistent` | The agent reconnects when the connection drops; `false` for inbound entries |
| `dynamic` | `true` for peers added at runtime |
| `direction` | `inbound` for accepted connections; omitted for outbound peers |

### Errors

| Status | Body | Cause |
| ------ | ---- | ----- |
| 400 | `{"error":"address is required"}` | Missing `address` |
| 400 | `{"error":"unknown transport \"xyz\" (expected quic, h2, or ws)"}` | Invalid `transport` |
| 400 | `{"error":"invalid peer id: ..."}` | `id` is not a valid agent ID |
| 400 | `{"error":"invalid tcp_keepalive \"abc\": ..."}` | `tcp_keepalive` is not a duration |
| 400 | `{"error":"peer \"198.51.100.20:443\" is a config peer and cannot be replaced"}` | `add` of a configured address |
| 400 | `{"error":"peer \"198.51.100.20:443\" is a config peer and cannot be removed"}` | `remove` of a configured address |
| 400 | `{"error":"dynamic peer \"203.0.113.99:4433\" not found"}` | `remove` of an unknown address |
| 400 | `{"error":"unknown action \"bogus\" (expected add, remove, or list)"}` | Invalid `action` |
| 403 | `management signing private key not configured on this agent` | Mutation without a signing private key |
| 403 | `peer management restricted: management key decryption unavailable` | `management.public_key` is set without `management.private_key` |
| 405 | `method not allowed` | Method other than `POST` |

## `POST /agents/{id}/peers/manage`

Manages peers of a remote agent. The request body and responses are the same as for [`POST /peers/manage`](#post-peersmanage).

```bash
curl -X POST http://localhost:8080/agents/331974cd4a0be0d7f945ed9d367a0211/peers/manage \
  -H "Content-Type: application/json" \
  -d '{"action": "add", "address": "203.0.113.20:4433"}'
```

The local agent signs `add` and `remove` and forwards the request over the mesh; the target verifies the signature with its `management.signing_public_key`. Additional errors:

| Status | Body | Cause |
| ------ | ---- | ----- |
| 400 | `{"error":"management signing public key not configured on this agent"}` | The target has no signing public key |
| 400 | `invalid agent ID format` | `{id}` is not a full agent ID |
| 502 | `failed to send request: <reason>` | The target is unknown, unreachable, or did not answer within 30 seconds |

## See Also

- [peer CLI](/cli/peer): the `peer` commands that call these endpoints
- [Peers Configuration](/configuration/peers): configured peers and their options
- [Listener Management Endpoints](/api/listener-management): open listeners for peers to dial
- [Dynamic Configuration Persistence](/configuration/dynamic-config-persistence): how runtime changes survive restart
