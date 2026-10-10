# Display Name Management API

HTTP endpoints for managing the agent display name at runtime.

## Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/display-name/manage` | POST | Manage display name on local agent |
| `/agents/{agent-id}/display-name/manage` | POST | Manage display name on remote agent |

These endpoints require `http.remote_api: true` in configuration.

---

## POST /display-name/manage

Manage the display name on the local agent.

### Request

Set the display name:

```bash
curl -X POST http://localhost:8080/display-name/manage \
  -H "Content-Type: application/json" \
  -d '{"action": "set", "name": "gateway-us-east"}'
```

Get the current display name:

```bash
curl -X POST http://localhost:8080/display-name/manage \
  -H "Content-Type: application/json" \
  -d '{"action": "get"}'
```

Revert to the config value (set empty name):

```bash
curl -X POST http://localhost:8080/display-name/manage \
  -H "Content-Type: application/json" \
  -d '{"action": "set", "name": ""}'
```

### Request Body

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `action` | string | Yes | Action to perform: `set` or `get` |
| `name` | string | For set | Display name to set (empty string reverts to config value) |

### Response

**Set Success (200)**:

```json
{
  "status": "ok",
  "message": "display name set to \"gateway-us-east\"",
  "name": "gateway-us-east"
}
```

**Get Success (200)**:

```json
{
  "status": "ok",
  "name": "gateway-us-east"
}
```

**Bad Request (400)**:

```json
{
  "error": "unknown action foo (expected set or get)"
}
```

**Forbidden (403)**:

```
display name management restricted: management key decryption unavailable
```

**Service Unavailable (503)**:

```
display name management not configured
```

### Behavior

When a display name is set:
1. The name is stored in memory
2. Route advertisements are triggered immediately
3. Node info is re-advertised to peers
4. The new name appears in dashboard, topology, and agent listings

When a display name is retrieved:
1. Returns the current effective display name
2. Priority: dynamic name > config name > short agent ID

Setting an empty name reverts to the configured `agent.display_name` value.

Dynamic display names:
- Are ephemeral (lost on agent restart)
- Override the config-file display name while set
- Are immediately propagated to all connected peers

---

## POST /agents/\{agent-id\}/display-name/manage

Manage the display name on a remote agent.

### Request

Set display name on remote agent:

```bash
curl -X POST http://localhost:8080/agents/abc123def456/display-name/manage \
  -H "Content-Type: application/json" \
  -d '{"action": "set", "name": "exit-eu-west"}'
```

Get display name from remote agent:

```bash
curl -X POST http://localhost:8080/agents/abc123def456/display-name/manage \
  -H "Content-Type: application/json" \
  -d '{"action": "get"}'
```

### Request Body

Same as `/display-name/manage`.

### Response

Same response formats as `/display-name/manage`.

The request is forwarded to the target agent via the mesh control channel. The response reflects the result from the remote agent.

### Error Responses

Additional error cases for remote display name management:

**Agent Not Found or Unreachable (502)**:

```
failed to send request: <error details>
```

All remote failures (agent not found, timeout, routing errors) return 502 Bad Gateway with a plain text error message.

---

## Error Responses

All endpoints may return:

| Status | Description |
|--------|-------------|
| 400 | Invalid request body or parameters |
| 403 | Management key required but unavailable |
| 404 | Endpoint disabled (remote_api not enabled) |
| 405 | Method not allowed (must be POST) |
| 502 | Remote agent not found, unreachable, or timed out |
| 503 | Display name management not configured |

:::note Management Key Protection
Display name management endpoints follow the same management key restrictions as route management. Agents with only `management.public_key` (field agents) cannot manage display names. Agents with both keys (operator nodes) can manage display names freely. If no management key is configured, display name management is unrestricted.
:::

:::warning Dynamic Display Names are Ephemeral
Dynamic display names set via the API are lost when the agent restarts. For persistent names, set `agent.display_name` in the configuration file.
:::
