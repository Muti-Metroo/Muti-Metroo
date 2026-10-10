# Stop API

HTTP endpoints for gracefully stopping an agent.

## Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/shutdown` | POST | Stop the local agent |
| `/agents/{id}/shutdown` | POST | Stop a remote agent over the mesh |

Both endpoints require `http.remote_api: true` in the agent's configuration. They are intended for operator tooling (CLI, Muti Metroo Manager) -- the response confirms the request was accepted; the actual shutdown runs after the response flushes.

---

## POST /shutdown

Stop the local agent.

### Request

```bash
curl -X POST -H "Authorization: Bearer <token>" \
     -H "Content-Type: application/json" \
     -d '{}' \
     http://localhost:8080/shutdown
```

The request body must be a JSON object. An empty `{}` is accepted; no fields are required.

### Response

**Success (200)**:

```json
{
  "status": "shutting-down",
  "message": "agent will exit shortly"
}
```

**Forbidden (403)** -- agent is missing `management.signing_private_key`:

```
management signing private key not configured on this agent
```

**Service Unavailable (503)** -- agent did not register a stop provider:

```
shutdown not configured
```

### Behavior

1. The handler verifies the agent has a signing private key (matches the gate used by other dynamic-config mutations).
2. HTTP 200 is written immediately so the client sees confirmation.
3. The agent's run loop is notified asynchronously via an internal channel; it then runs the same graceful shutdown path as a SIGTERM (drain in-flight streams up to 10 seconds, close peer connections, exit).

---

## POST /agents/&#123;id&#125;/shutdown

Stop a remote agent.

### Request

```bash
curl -X POST -H "Authorization: Bearer <token>" \
     -H "Content-Type: application/json" \
     -d '{}' \
     http://localhost:8080/agents/kreata/shutdown
```

The path segment accepts either a full agent ID or a unique short prefix (resolved against the topology by the entry-point agent).

### Behavior

1. The entry-point agent signs the body with `OpStop` using its `management.signing_private_key`.
2. The signed payload is forwarded as a `CONTROL_REQUEST` of type `ControlTypeStop` (0x10) across the mesh.
3. The target agent verifies the signature with its `management.signing_public_key`.
4. On valid signature, the target agent enters the same shutdown path as the local endpoint.

### Response

The response from the target is returned verbatim. Success returns the same 200 envelope as the local endpoint. On verification failure the target returns the `crypto.ErrAuth*` reason and the gateway surfaces it as HTTP 400.

---

## Service manager restart

A successful stop exits the process. Agents installed as a system service may be restarted automatically by the service manager (notably macOS launchd `KeepAlive` restarts immediately on clean exit). See the [stop CLI page](/cli/stop) for the per-platform matrix. For permanent removal, run `muti-metroo service uninstall` on the host before stopping.

---

## Authorization summary

| Path | Local signing key required | Target signing key required |
|------|----------------------------|-----------------------------|
| `POST /shutdown` | Yes (`signing_private_key` on the agent) | n/a -- target is the same agent |
| `POST /agents/{id}/shutdown` | Yes on the entry-point agent | Yes (`signing_public_key` on the target) |

Bearer-token auth applies to both endpoints if `http.token_hash` is set.
