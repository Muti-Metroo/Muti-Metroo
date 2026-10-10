# Forward Management API

HTTP endpoints for managing dynamic forward listeners (ingress) and endpoints (exit) at runtime.

## Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/forward/manage` | POST | Manage forward listeners on local agent |
| `/agents/{agent-id}/forward/manage` | POST | Manage forward listeners on remote agent |
| `/forward/endpoints/manage` | POST | Manage forward endpoints on local agent |
| `/agents/{agent-id}/forward/endpoints/manage` | POST | Manage forward endpoints on remote agent |

These endpoints require `http.remote_api: true` in configuration.

---

## POST /forward/manage

Manage forward listeners on the local agent.

### Request

Add a listener:

```bash
curl -X POST http://localhost:8080/forward/manage \
  -H "Content-Type: application/json" \
  -d '{"action": "add", "key": "web-server", "address": ":9090"}'
```

Add a listener with connection limit:

```bash
curl -X POST http://localhost:8080/forward/manage \
  -H "Content-Type: application/json" \
  -d '{"action": "add", "key": "web-server", "address": ":9090", "max_connections": 100}'
```

Remove a listener:

```bash
curl -X POST http://localhost:8080/forward/manage \
  -H "Content-Type: application/json" \
  -d '{"action": "remove", "key": "web-server"}'
```

List all listeners:

```bash
curl -X POST http://localhost:8080/forward/manage \
  -H "Content-Type: application/json" \
  -d '{"action": "list"}'
```

### Request Body

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `action` | string | Yes | Action to perform: `add`, `remove`, or `list` |
| `key` | string | For add/remove | Routing key for the forward listener |
| `address` | string | For add | Listen address (e.g., `:9090`, `0.0.0.0:8080`) |
| `max_connections` | integer | No | Maximum concurrent connections (default: 0 = unlimited) |

### Response

**Add Success (200)**:

```json
{
  "status": "ok",
  "message": "forward listener \"web-server\" added on [::]:9090"
}
```

**Remove Success (200)**:

```json
{
  "status": "ok",
  "message": "forward listener \"web-server\" removed"
}
```

**List Success (200)**:

```json
{
  "status": "ok",
  "listeners": [
    {
      "key": "web-server",
      "address": "[::]:9090",
      "max_connections": 0,
      "dynamic": false
    },
    {
      "key": "api-server",
      "address": "[::]:8081",
      "max_connections": 100,
      "dynamic": true
    }
  ]
}
```

**Bad Request (400)**:

```json
{
  "error": "key is required"
}
```

```json
{
  "error": "listener \"web-server\" is a config listener and cannot be removed"
}
```

```json
{
  "error": "listener \"web-server\" not found"
}
```

**Forbidden (403)**:

```
forward management restricted: management key decryption unavailable
```

### Behavior

When a listener is added:
1. The key and address are validated
2. If the key belongs to a config listener, the request is rejected
3. If a dynamic listener with the same key exists, it is stopped and replaced
4. The new listener is started on the specified address
5. Node info is immediately re-advertised to peers

When a listener is removed:
1. The key must belong to a dynamic listener (not from config)
2. The listener is stopped and removed
3. Node info is immediately re-advertised to peers

When listing:
- Both config and dynamic listeners are returned
- The `dynamic` field distinguishes runtime listeners from config-file listeners

Dynamic listeners:
- Are ephemeral (lost on agent restart)
- Can be replaced by adding the same key again
- Config-file listeners are protected from modification

---

## POST /agents/\{agent-id\}/forward/manage

Manage forward listeners on a remote agent.

### Request

```bash
curl -X POST http://localhost:8080/agents/abc123def456/forward/manage \
  -H "Content-Type: application/json" \
  -d '{"action": "add", "key": "web-server", "address": ":9090"}'
```

### Request Body

Same as `/forward/manage`.

### Response

Same response formats as `/forward/manage`.

The request is forwarded to the target agent via the mesh control channel. The response reflects the result from the remote agent.

---

## Error Responses

All endpoints may return:

| Status | Description |
|--------|-------------|
| 400 | Invalid request body or parameters |
| 403 | Management key required but unavailable |
| 404 | Endpoint disabled (remote_api not enabled) or agent not found |
| 405 | Method not allowed (must be POST) |
| 503 | Forward management not configured |

:::note Management Key Protection
Forward listener management endpoints follow the same management key restrictions as route management. Agents with only `management.public_key` (field agents) cannot manage listeners. Agents with both keys (operator nodes) can manage listeners freely.
:::

:::warning Persistence
Dynamic listeners and endpoints survive agent restarts when `agent.data_dir` is configured; without it they are ephemeral. For always-on entries, add them to the `forward.listeners` / `forward.endpoints` sections in the configuration file.
:::

---

## POST /forward/endpoints/manage

Manage forward endpoints (exit-side routing key -> target mappings) on the local agent.

### Request

Add an endpoint:

```bash
curl -X POST http://localhost:8080/forward/endpoints/manage \
  -H "Content-Type: application/json" \
  -d '{"action": "add", "key": "web-server", "target": "127.0.0.1:3000"}'
```

Remove an endpoint:

```bash
curl -X POST http://localhost:8080/forward/endpoints/manage \
  -H "Content-Type: application/json" \
  -d '{"action": "remove", "key": "web-server"}'
```

List all endpoints:

```bash
curl -X POST http://localhost:8080/forward/endpoints/manage \
  -H "Content-Type: application/json" \
  -d '{"action": "list"}'
```

### Request Body

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `action` | string | Yes | Action to perform: `add`, `remove`, or `list` |
| `key` | string | For add/remove | Routing key registered on the agent |
| `target` | string | For add | Destination in `host:port` format |

### Response

**Add Success (200)**:

```json
{
  "status": "ok",
  "message": "forward endpoint \"web-server\" added -> 127.0.0.1:3000"
}
```

**Remove Success (200)**:

```json
{
  "status": "ok",
  "message": "forward endpoint \"web-server\" removed"
}
```

**List Success (200)**:

```json
{
  "status": "ok",
  "endpoints": [
    {
      "key": "web-server",
      "target": "127.0.0.1:3000",
      "dynamic": false
    },
    {
      "key": "api-server",
      "target": "127.0.0.1:4000",
      "dynamic": true
    }
  ]
}
```

**Bad Request (400)**:

```json
{
  "error": "target is required"
}
```

```json
{
  "error": "endpoint \"web-server\" is a config endpoint and cannot be replaced"
}
```

```json
{
  "error": "endpoint \"web-server\" not found"
}
```

### Behavior

When an endpoint is added:
1. `key` and `target` are validated (`host:port` format)
2. If the key belongs to a config endpoint, the request is rejected
3. If a dynamic endpoint with the same key exists, its target is replaced
4. The routing key is registered with the forward handler and a local forward route is added
5. Route advertisement is triggered so peer listeners can reach the new target

When an endpoint is removed:
1. The key must belong to a dynamic endpoint (not from config)
2. The routing key is unregistered from the handler and the local forward route is withdrawn
3. A per-route `RouteWithdraw` is flooded to all connected peers immediately so the withdrawn key is pruned from peer routing tables without waiting for `route_ttl`

When listing, both config and dynamic endpoints are returned; the `dynamic` field distinguishes them.

---

## POST /agents/\{agent-id\}/forward/endpoints/manage

Manage forward endpoints on a remote agent. Request and response shapes are identical to `/forward/endpoints/manage`; the payload is delivered to the target agent via the mesh control channel and the result is returned synchronously.

```bash
curl -X POST http://localhost:8080/agents/abc123def456/forward/endpoints/manage \
  -H "Content-Type: application/json" \
  -d '{"action": "add", "key": "web-server", "target": "127.0.0.1:3000"}'
```
