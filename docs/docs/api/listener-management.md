# Listener Management API

HTTP endpoints for managing dynamic transport listeners at runtime.

Transport listeners are the QUIC/HTTP2/WebSocket sockets that accept inbound peer connections (the top-level `listeners:` config block). Dynamic listeners let you open or close one on an already-running agent without editing `config.yaml` and restarting.

## Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/listeners/manage` | POST | Manage transport listeners on local agent |
| `/agents/{agent-id}/listeners/manage` | POST | Manage transport listeners on remote agent |

These endpoints require `http.remote_api: true` in configuration.

---

## POST /listeners/manage

Manage transport listeners on the local agent.

### Request

Add a QUIC listener:

```bash
curl -X POST http://localhost:8080/listeners/manage \
  -H "Content-Type: application/json" \
  -d '{"action": "add", "transport": "quic", "address": ":4434"}'
```

Add a plaintext WebSocket listener with an advertised dial target:

```bash
curl -X POST http://localhost:8080/listeners/manage \
  -H "Content-Type: application/json" \
  -d '{"action": "add", "transport": "ws", "address": "127.0.0.1:3002", "path": "/ws/", "plaintext": true, "advertise_address": "vps.example.com:4433"}'
```

Remove a dynamic listener:

```bash
curl -X POST http://localhost:8080/listeners/manage \
  -H "Content-Type: application/json" \
  -d '{"action": "remove", "address": ":4434"}'
```

List all listeners (dynamic and config):

```bash
curl -X POST http://localhost:8080/listeners/manage \
  -H "Content-Type: application/json" \
  -d '{"action": "list"}'
```

### Request Body

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `action` | string | Yes | Action to perform: `add`, `remove`, or `list` |
| `transport` | string | For add | Transport type: `quic` (default), `h2`, or `ws` |
| `address` | string | For add/remove | Bind address (e.g. `:4433`, `127.0.0.1:3002`) |
| `path` | string | For h2/ws add | HTTP path (e.g. `/ws/`) |
| `plaintext` | bool | No | Start a WebSocket listener without TLS (ws only) |
| `advertise_address` | string | No | Public `host:port` to advertise to the mesh |
| `advertise_path` | string | No | Path to advertise for h2/ws (defaults to `path`) |
| `advertise_transport` | string | No | Transport to advertise (defaults to `transport`) |

### Response

**Add Success (200)**:

```json
{
  "status": "ok",
  "message": "listener :4434 added (transport=quic)"
}
```

**Remove Success (200)**:

```json
{
  "status": "ok",
  "message": "listener :4434 removed"
}
```

**List Success (200)**:

```json
{
  "status": "ok",
  "listeners": [
    {
      "transport": "quic",
      "address": ":4433",
      "plaintext": false,
      "advertise_address": "vps.example.com:4433",
      "dynamic": false
    },
    {
      "transport": "ws",
      "address": "127.0.0.1:3002",
      "path": "/ws/",
      "plaintext": true,
      "dynamic": true
    }
  ]
}
```

**Bad Request (400)**:

```json
{ "error": "address is required" }
```

```json
{ "error": "invalid transport: xyz (must be quic, h2, or ws)" }
```

```json
{ "error": "listener \":4433\" is a config listener and cannot be replaced" }
```

```json
{ "error": "dynamic listener \":4434\" not found" }
```

**Forbidden (403)**:

```
listener management restricted: management key decryption unavailable
```

### Behavior

When a listener is added:
1. The spec is validated (transport, address, path-for-h2/ws, advertise block) using the same rules as config-file listeners.
2. If the address belongs to a config listener, the request is rejected.
3. If a dynamic listener with the same address exists, it is stopped and replaced.
4. The listener binds using the agent's global TLS certificate (or an auto-generated self-signed one) and the global mTLS setting. `plaintext` (ws only) starts the listener without TLS for reverse-proxy deployments. A bind failure (e.g. address already in use) is returned as an error.
5. If `advertise_address` is set, the dial target is published to the mesh via NodeInfo.

When a listener is removed:
1. The address must belong to a dynamic listener (not from config).
2. The listener stops accepting new connections and its socket is closed; existing peer connections are unaffected.

When listing:
- Both config and dynamic listeners are returned; the `dynamic` field distinguishes them.

Dynamic listeners:
- Persist across restarts when `agent.data_dir` is set (replayed from the [dynamic config journal](/configuration/dynamic-config-persistence)) and are restored across sleep/wake; otherwise they are ephemeral.
- Reuse the agent's global TLS configuration and mTLS setting; per-listener certificate overrides are not supported at runtime.

---

## POST /agents/\{agent-id\}/listeners/manage

Manage transport listeners on a remote agent.

### Request

```bash
curl -X POST http://localhost:8080/agents/abc123def456/listeners/manage \
  -H "Content-Type: application/json" \
  -d '{"action": "add", "transport": "quic", "address": ":4433"}'
```

### Request Body

Same as `/listeners/manage`.

### Response

Same response formats as `/listeners/manage`. The request is forwarded to the target agent via the mesh control channel; `add`/`remove` are signed with the local operator's management key, while `list` passes through unsigned.

---

## Error Responses

All endpoints may return:

| Status | Description |
|--------|-------------|
| 400 | Invalid request body or parameters |
| 403 | Management key required but unavailable |
| 404 | Endpoint disabled (`remote_api` not enabled) or agent not found |
| 405 | Method not allowed (must be POST) |
| 503 | Listener management not configured |

:::note Management Key Protection
Listener management follows the same management key restrictions as route, forward, and peer management. Agents with only `management.public_key` (field agents) cannot manage listeners. Agents with both keys (operator nodes) can manage listeners freely.
:::
