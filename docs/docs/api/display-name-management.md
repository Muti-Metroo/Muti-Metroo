---
title: Display Name Management Endpoints
description: Read or change the display name of a local or remote agent at runtime.
sidebar_position: 9
---

# Display Name Management Endpoints

These endpoints read and change an agent's display name, the label shown in topology views, route lists, and CLI output. They belong to the remote API group (`http.remote_api`, enabled by default) and accept only `POST`.

| Endpoint | Description |
| -------- | ----------- |
| `POST /display-name/manage` | Display name of the local agent |
| `POST /agents/{id}/display-name/manage` | Display name of a remote agent |

## `POST /display-name/manage`

### Request

| Field | Type | Used by | Description |
| ----- | ---- | ------- | ----------- |
| `action` | string | all | `get` or `set` |
| `name` | string | `set` | New display name, up to 255 bytes. An empty string reverts to `agent.display_name` from the configuration. |

`set` is a mutation: the agent serving the request must have `management.signing_private_key`, otherwise it returns `403`. The new name is advertised to peers immediately and is written to the [dynamic configuration journal](/configuration/dynamic-config-persistence) when `agent.data_dir` is set, so it survives restart.

### `set`

```bash
curl -X POST http://localhost:8080/display-name/manage \
  -H "Content-Type: application/json" \
  -d '{"action": "set", "name": "gateway-us-east"}'
```

```json
{
  "status": "ok",
  "message": "display name set to \"gateway-us-east\"",
  "name": "gateway-us-east"
}
```

`name` in the response is the effective display name. After a `set` with an empty name it is the configured name, or the short agent ID when none is configured.

### `get`

```bash
curl -X POST http://localhost:8080/display-name/manage \
  -H "Content-Type: application/json" \
  -d '{"action": "get"}'
```

```json
{
  "status": "ok",
  "name": "gateway-us-east"
}
```

The effective name is the runtime name if one is set, otherwise `agent.display_name`, otherwise the short agent ID.

### Errors

| Status | Body | Cause |
| ------ | ---- | ----- |
| 400 | `{"error":"display name is 300 bytes, maximum is 255"}` | Name too long |
| 400 | `{"error":"unknown action \"foo\" (expected set or get)"}` | Invalid `action` |
| 400 | `{"error":"invalid request: ..."}` | Body is not valid JSON |
| 403 | `management signing private key not configured on this agent` | `set` without a signing private key |
| 403 | `display name management restricted: management key decryption unavailable` | `management.public_key` is set without `management.private_key` |
| 405 | `method not allowed` | Method other than `POST` |

## `POST /agents/{id}/display-name/manage`

Reads or changes the display name of a remote agent. The request body and responses are the same as for [`POST /display-name/manage`](#post-display-namemanage).

```bash
curl -X POST http://localhost:8080/agents/331974cd4a0be0d7f945ed9d367a0211/display-name/manage \
  -H "Content-Type: application/json" \
  -d '{"action": "set", "name": "exit-eu"}'
```

```json
{
  "status": "ok",
  "message": "display name set to \"exit-eu\"",
  "name": "exit-eu"
}
```

The local agent signs `set` and forwards the request over the mesh; the target verifies the signature with its `management.signing_public_key`. Additional errors:

| Status | Body | Cause |
| ------ | ---- | ----- |
| 400 | `{"error":"management signing public key not configured on this agent"}` | The target has no signing public key |
| 400 | `invalid agent ID format` | `{id}` is not a full agent ID |
| 502 | `failed to send request: <reason>` | The target is unknown, unreachable, or did not answer within 30 seconds |

## See Also

- [display-name CLI](/cli/display-name): the `display-name` commands that call these endpoints
- [Agent Configuration](/configuration/agent): the configured `display_name`
- [Dynamic Configuration Persistence](/configuration/dynamic-config-persistence): how runtime changes survive restart
