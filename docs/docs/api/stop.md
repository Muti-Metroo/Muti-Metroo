---
title: Shutdown Endpoints
description: Stop the local agent or a remote agent gracefully over HTTP.
sidebar_position: 15
---

# Shutdown Endpoints

These endpoints stop an agent gracefully, the same way as a `SIGTERM`: the agent withdraws its routes, closes peer connections, and exits. They belong to the remote API group (`http.remote_api`, enabled by default) and accept only `POST`. The [`muti-metroo stop`](/cli/stop) command is a client of these endpoints.

| Endpoint | Description |
| -------- | ----------- |
| `POST /shutdown` | Stop the agent serving the request |
| `POST /agents/{id}/shutdown` | Stop a remote agent |

Shutdown is a signed operation: the agent serving the request must have `management.signing_private_key`, and for the remote endpoint the target must have `management.signing_public_key`.

An agent installed as a system service may be started again by the service manager after it exits. See [CLI - stop](/cli/stop) for the behavior per platform, and run `muti-metroo service uninstall` first to remove an agent permanently.

## `POST /shutdown`

The response is sent before the shutdown starts. The request body is ignored.

```bash
curl -X POST http://localhost:8080/shutdown
```

**Response (200):**

```json
{
  "message": "agent will exit shortly",
  "status": "shutting-down"
}
```

### Errors

| Status | Body | Cause |
| ------ | ---- | ----- |
| 403 | `management signing private key not configured on this agent` | No signing private key |
| 404 | HTML page | `http.remote_api` is disabled |
| 405 | `method not allowed` | Method other than `POST` |

## `POST /agents/{id}/shutdown`

The request body must be a JSON object; `{}` is sufficient. The local agent signs it and forwards it over the mesh. The target verifies the signature, answers, and then shuts down.

```bash
curl -X POST http://localhost:8080/agents/331974cd4a0be0d7f945ed9d367a0211/shutdown \
  -H "Content-Type: application/json" \
  -d '{}'
```

**Response (200):**

```json
{
  "message": "agent will exit shortly",
  "status": "shutting-down"
}
```

### Errors

| Status | Body | Cause |
| ------ | ---- | ----- |
| 400 | `{"error":"management signing public key not configured on this agent"}` | The target has no signing public key |
| 400 | `{"error":"..."}` | The target rejected the signature |
| 400 | `invalid agent ID format` | `{id}` is not a full agent ID |
| 403 | `management signing private key not configured on this agent` | The serving agent has no signing private key |
| 403 | `shutdown restricted: management key decryption unavailable` | `management.public_key` is set without `management.private_key` |
| 500 | `failed to sign request: parse mutation body: unexpected end of JSON input` | Empty or non-JSON request body |
| 502 | `failed to send request: <reason>` | The target is unknown, unreachable, or did not answer within 30 seconds |

## See Also

- [CLI - stop](/cli/stop) - Stop agents from the command line
- [Service management](/cli/service) - Install and uninstall agents as system services
- [Management Keys](/configuration/management) - Signing keys for privileged operations
