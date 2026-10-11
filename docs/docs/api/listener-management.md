---
title: Listener Management Endpoints
description: Open, close, and list QUIC, HTTP/2, and WebSocket transport listeners on a running agent.
sidebar_position: 8
---

# Listener Management Endpoints

Transport listeners are the QUIC, HTTP/2, and WebSocket sockets that accept connections from peers (the `listeners` configuration section). These endpoints open and close listeners on a running agent and list the active ones. They belong to the remote API group (`http.remote_api`, enabled by default) and accept only `POST`.

| Endpoint | Description |
| -------- | ----------- |
| `POST /listeners/manage` | Manage listeners of the local agent |
| `POST /agents/{id}/listeners/manage` | Manage listeners of a remote agent |

`add` and `remove` are mutations: the agent serving the request must have `management.signing_private_key`, otherwise it returns `403`. Mutations are written to the [dynamic configuration journal](/configuration/dynamic-config-persistence) when `agent.data_dir` is set and are replayed on restart and after wake. Listeners from the configuration file cannot be removed or replaced.

## `POST /listeners/manage`

### Request

| Field | Type | Used by | Description |
| ----- | ---- | ------- | ----------- |
| `action` | string | all | `add`, `remove`, or `list` |
| `address` | string | `add`, `remove` | Bind address, for example `0.0.0.0:4434` or `127.0.0.1:3002`. Identifies the listener for `remove`. |
| `transport` | string | `add` | `quic` (default), `h2`, or `ws` |
| `path` | string | `add` | HTTP path; required for `h2` and `ws` |
| `plaintext` | boolean | `add` | Accept unencrypted WebSocket connections, for use behind a TLS-terminating reverse proxy. `ws` only. |
| `advertise_address` | string | `add` | Public `host:port` that other agents should dial. Loopback and unspecified hosts are rejected. |
| `advertise_path` | string | `add` | Advertised path for `h2` and `ws`; defaults to `path` |
| `advertise_transport` | string | `add` | Advertised transport; defaults to `transport` |

Validation follows the same rules as the `listeners` configuration section. New listeners use the agent's global TLS certificate and mTLS setting; per-listener TLS overrides are not available at runtime. When `advertise_address` is set, the dial target is published to the mesh and appears in `advertised_listeners` of [`GET /api/topology`](/api/dashboard#get-apitopology).

### `add`

Adding an address that already has a dynamic listener stops it and starts the new one. Bind failures, such as an address already in use, are returned as errors.

```bash
curl -X POST http://localhost:8080/listeners/manage \
  -H "Content-Type: application/json" \
  -d '{"action": "add", "transport": "quic", "address": "0.0.0.0:4434"}'
```

```json
{
  "status": "ok",
  "message": "listener 0.0.0.0:4434 added (transport=quic)"
}
```

A plaintext WebSocket listener behind a reverse proxy, advertised under its public address:

```bash
curl -X POST http://localhost:8080/listeners/manage \
  -H "Content-Type: application/json" \
  -d '{"action": "add", "transport": "ws", "address": "127.0.0.1:3002", "path": "/ws/", "plaintext": true, "advertise_address": "relay.example.com:443"}'
```

```json
{
  "status": "ok",
  "message": "listener 127.0.0.1:3002 added (transport=ws)"
}
```

### `remove`

Stops accepting new connections on a dynamic listener and closes its socket. Peer connections that are already established stay up.

```bash
curl -X POST http://localhost:8080/listeners/manage \
  -H "Content-Type: application/json" \
  -d '{"action": "remove", "address": "0.0.0.0:4434"}'
```

```json
{
  "status": "ok",
  "message": "listener 0.0.0.0:4434 removed"
}
```

### `list`

Returns configured and dynamic listeners, sorted by address. The `listeners` field is omitted when there are none.

```bash
curl -X POST http://localhost:8080/listeners/manage \
  -H "Content-Type: application/json" \
  -d '{"action": "list"}'
```

```json
{
  "status": "ok",
  "listeners": [
    {
      "transport": "quic",
      "address": "0.0.0.0:4433",
      "plaintext": false,
      "dynamic": false
    },
    {
      "transport": "ws",
      "address": "127.0.0.1:3002",
      "path": "/ws/",
      "plaintext": true,
      "advertise_address": "relay.example.com:443",
      "dynamic": true
    }
  ]
}
```

| Field | Description |
| ----- | ----------- |
| `transport` | `quic`, `h2`, or `ws` |
| `address` | Bind address |
| `path` | HTTP path; omitted for QUIC |
| `plaintext` | The listener accepts unencrypted WebSocket connections |
| `advertise_address` | Advertised dial target; omitted when not advertised |
| `dynamic` | `true` for listeners added at runtime |

### Errors

| Status | Body | Cause |
| ------ | ---- | ----- |
| 400 | `{"error":"address is required"}` | Missing `address` |
| 400 | `{"error":"invalid transport: xyz (must be quic, h2, or ws)"}` | Invalid `transport` |
| 400 | `{"error":"path is required for h2 transport"}`, `{"error":"path is required for ws transport"}` | `h2` or `ws` without `path` |
| 400 | `{"error":"listener \"0.0.0.0:4433\" is a config listener and cannot be replaced"}` | `add` on a configured address |
| 400 | `{"error":"listener \"0.0.0.0:4433\" is a config listener and cannot be removed"}` | `remove` of a configured listener |
| 400 | `{"error":"dynamic listener \"0.0.0.0:4499\" not found"}` | `remove` of an unknown address |
| 400 | `{"error":"start listener: ..."}` | The address could not be bound |
| 400 | `{"error":"advertise.address: host \"127.0.0.1\" is loopback; advertise.address must be a publicly reachable host"}` | `advertise_address` is a loopback or unspecified host |
| 400 | `{"error":"unknown action \"bogus\" (expected add, remove, or list)"}` | Invalid `action` |
| 403 | `management signing private key not configured on this agent` | Mutation without a signing private key |
| 403 | `listener management restricted: management key decryption unavailable` | `management.public_key` is set without `management.private_key` |
| 405 | `method not allowed` | Method other than `POST` |

## `POST /agents/{id}/listeners/manage`

Manages listeners of a remote agent. The request body and responses are the same as for [`POST /listeners/manage`](#post-listenersmanage).

```bash
curl -X POST http://localhost:8080/agents/331974cd4a0be0d7f945ed9d367a0211/listeners/manage \
  -H "Content-Type: application/json" \
  -d '{"action": "add", "transport": "quic", "address": "0.0.0.0:4434"}'
```

The local agent signs `add` and `remove` and forwards the request over the mesh; the target verifies the signature with its `management.signing_public_key`. Additional errors:

| Status | Body | Cause |
| ------ | ---- | ----- |
| 400 | `{"error":"management signing public key not configured on this agent"}` | The target has no signing public key |
| 400 | `invalid agent ID format` | `{id}` is not a full agent ID |
| 502 | `failed to send request: <reason>` | The target is unknown, unreachable, or did not answer within 30 seconds |

## See Also

- [listener CLI](/cli/listener): the `listener` commands that call these endpoints
- [Listeners Configuration](/configuration/listeners): listener options and advertisement
- [Peer Management Endpoints](/api/peer-management): dial peers at runtime
- [Dynamic Configuration Persistence](/configuration/dynamic-config-persistence): how runtime changes survive restart
