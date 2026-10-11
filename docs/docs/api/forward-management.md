---
title: Forward Management Endpoints
description: Add, remove, and list port forward listeners and endpoints at runtime on local and remote agents.
sidebar_position: 6
---

# Forward Management Endpoints

These endpoints change [port forwarding](/features/port-forwarding) at runtime. Forward listeners accept TCP connections on the ingress side; forward endpoints map a routing key to a `host:port` target on the exit side. A connection flows when a listener and an endpoint share the same key. The endpoints belong to the remote API group (`http.remote_api`, enabled by default) and accept only `POST`.

| Endpoint | Description |
| -------- | ----------- |
| `POST /forward/manage` | Forward listeners on the local agent |
| `POST /agents/{id}/forward/manage` | Forward listeners on a remote agent |
| `POST /forward/endpoints/manage` | Forward endpoints on the local agent |
| `POST /agents/{id}/forward/endpoints/manage` | Forward endpoints on a remote agent |

`add` and `remove` are mutations: the agent serving the request must have `management.signing_private_key`, otherwise it returns `403`. Mutations are written to the [dynamic configuration journal](/configuration/dynamic-config-persistence) when `agent.data_dir` is set and are replayed on restart. Listeners and endpoints from the `forward` configuration section cannot be removed or replaced.

## `POST /forward/manage`

### Request

| Field | Type | Used by | Description |
| ----- | ---- | ------- | ----------- |
| `action` | string | all | `add`, `remove`, or `list` |
| `key` | string | `add`, `remove` | Routing key |
| `address` | string | `add` | Listen address, for example `127.0.0.1:9090` or `:9090` |
| `max_connections` | integer | `add` | Concurrent connection limit; `0` (default) means unlimited |

Adding a key that already exists as a dynamic listener stops the old listener and starts the new one.

```bash
curl -X POST http://localhost:8080/forward/manage \
  -H "Content-Type: application/json" \
  -d '{"action": "add", "key": "ssh", "address": "127.0.0.1:2222", "max_connections": 10}'
```

```json
{
  "status": "ok",
  "message": "forward listener \"ssh\" added on 127.0.0.1:2222"
}
```

```bash
curl -X POST http://localhost:8080/forward/manage \
  -H "Content-Type: application/json" \
  -d '{"action": "remove", "key": "ssh"}'
```

```json
{
  "status": "ok",
  "message": "forward listener \"ssh\" removed"
}
```

### `list`

Returns listeners from the configuration and those added at runtime. The `listeners` field is omitted when there are none.

```bash
curl -X POST http://localhost:8080/forward/manage \
  -H "Content-Type: application/json" \
  -d '{"action": "list"}'
```

```json
{
  "status": "ok",
  "listeners": [
    {
      "key": "web",
      "address": "127.0.0.1:9090",
      "max_connections": 0,
      "dynamic": false
    },
    {
      "key": "ssh",
      "address": "127.0.0.1:2222",
      "max_connections": 10,
      "dynamic": true
    }
  ]
}
```

| Field | Description |
| ----- | ----------- |
| `key` | Routing key |
| `address` | Bound listen address |
| `max_connections` | Connection limit; `0` is unlimited |
| `dynamic` | `true` for listeners added at runtime |

### Errors

| Status | Body | Cause |
| ------ | ---- | ----- |
| 400 | `{"error":"key is required"}` | Missing `key` |
| 400 | `{"error":"address is required"}` | `add` without `address` |
| 400 | `{"error":"listener \"web\" is a config listener and cannot be removed"}` | `remove` of a configured listener |
| 400 | `{"error":"listener \"web\" is a config listener and cannot be replaced"}` | `add` of a configured key |
| 400 | `{"error":"listener \"zzz\" not found"}` | `remove` of an unknown key |
| 400 | `{"error":"failed to start listener: ..."}` | The address could not be bound |
| 400 | `{"error":"unknown action \"bogus\" (expected add, remove, or list)"}` | Invalid `action` |
| 403 | `management signing private key not configured on this agent` | Mutation without a signing private key |
| 403 | `forward management restricted: management key decryption unavailable` | `management.public_key` is set without `management.private_key` |
| 405 | `method not allowed` | Method other than `POST` |

## `POST /forward/endpoints/manage`

### Request

| Field | Type | Used by | Description |
| ----- | ---- | ------- | ----------- |
| `action` | string | all | `add`, `remove`, or `list` |
| `key` | string | `add`, `remove` | Routing key |
| `target` | string | `add` | Destination in `host:port` form |

Adding a key that already exists as a dynamic endpoint replaces its target. New and removed endpoints are advertised to peers immediately.

```bash
curl -X POST http://localhost:8080/forward/endpoints/manage \
  -H "Content-Type: application/json" \
  -d '{"action": "add", "key": "ssh", "target": "127.0.0.1:22"}'
```

```json
{
  "status": "ok",
  "message": "forward endpoint \"ssh\" added -> 127.0.0.1:22"
}
```

The `>` escape in `message` is the `>` character.

```bash
curl -X POST http://localhost:8080/forward/endpoints/manage \
  -H "Content-Type: application/json" \
  -d '{"action": "remove", "key": "ssh"}'
```

```json
{
  "status": "ok",
  "message": "forward endpoint \"ssh\" removed"
}
```

### `list`

The `endpoints` field is omitted when there are none.

```bash
curl -X POST http://localhost:8080/forward/endpoints/manage \
  -H "Content-Type: application/json" \
  -d '{"action": "list"}'
```

```json
{
  "status": "ok",
  "endpoints": [
    {
      "key": "web",
      "target": "127.0.0.1:3000",
      "dynamic": false
    },
    {
      "key": "ssh",
      "target": "127.0.0.1:22",
      "dynamic": true
    }
  ]
}
```

### Errors

| Status | Body | Cause |
| ------ | ---- | ----- |
| 400 | `{"error":"key is required"}` | Missing `key` |
| 400 | `{"error":"target is required"}` | `add` without `target` |
| 400 | `{"error":"invalid target \"...\": ..."}` | `target` is not `host:port` |
| 400 | `{"error":"endpoint \"web\" is a config endpoint and cannot be replaced"}` | `add` of a configured key |
| 400 | `{"error":"endpoint \"web\" is a config endpoint and cannot be removed"}` | `remove` of a configured key |
| 400 | `{"error":"endpoint \"zzz\" not found"}` | `remove` of an unknown key |
| 400 | `{"error":"endpoint \"ssh\" is held by an expose session ...; disconnect it first"}` | The key is registered by an active SOCKS expose session |
| 403 | `management signing private key not configured on this agent` | Mutation without a signing private key |
| 403 | `forward endpoint management restricted: management key decryption unavailable` | `management.public_key` is set without `management.private_key` |
| 405 | `method not allowed` | Method other than `POST` |

## `POST /agents/{id}/forward/manage`

Manages forward listeners on a remote agent. The request body and responses are the same as for [`POST /forward/manage`](#post-forwardmanage).

```bash
curl -X POST http://localhost:8080/agents/93cdf211bb05aa48900a61ab6699fbdd/forward/manage \
  -H "Content-Type: application/json" \
  -d '{"action": "add", "key": "ssh", "address": "127.0.0.1:2222"}'
```

## `POST /agents/{id}/forward/endpoints/manage`

Manages forward endpoints on a remote agent. The request body and responses are the same as for [`POST /forward/endpoints/manage`](#post-forwardendpointsmanage).

```bash
curl -X POST http://localhost:8080/agents/331974cd4a0be0d7f945ed9d367a0211/forward/endpoints/manage \
  -H "Content-Type: application/json" \
  -d '{"action": "add", "key": "ssh", "target": "127.0.0.1:22"}'
```

For both remote endpoints the local agent signs `add` and `remove` and forwards the request over the mesh; the target verifies the signature with its `management.signing_public_key`. Additional errors:

| Status | Body | Cause |
| ------ | ---- | ----- |
| 400 | `{"error":"management signing public key not configured on this agent"}` | The target has no signing public key |
| 400 | `invalid agent ID format` | `{id}` is not a full agent ID |
| 502 | `failed to send request: <reason>` | The target is unknown, unreachable, or did not answer within 30 seconds |

## See Also

- [CLI - forward](/cli/forward) - The `forward` commands that call these endpoints
- [Port Forwarding](/features/port-forwarding) - How listeners and endpoints work together
- [Forward Configuration](/configuration/forward) - Static listeners and endpoints
- [Dynamic Configuration Persistence](/configuration/dynamic-config-persistence) - How runtime changes survive restart
