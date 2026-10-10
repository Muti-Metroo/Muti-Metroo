# Probe API

HTTP endpoints for testing connectivity to Muti Metroo listeners. The probe dials a
listener, performs the `PEER_HELLO` handshake, and reports diagnostics
(success, RTT, remote agent ID, classified error) without establishing a
real peer connection. This is the same operation exposed by the
[`muti-metroo probe`](/cli/probe) CLI command, but available over HTTP so
dashboards and other tools can run tests programmatically.

## Endpoints

| Endpoint                   | Method | Description                                          |
| -------------------------- | ------ | ---------------------------------------------------- |
| `/api/probe`               | POST   | Probe from the local agent (the one serving the API) |
| `/agents/{agent-id}/probe` | POST   | Probe from a remote agent in the mesh                |

`/api/probe` requires `http.dashboard: true` in configuration.
`/agents/{agent-id}/probe` requires `http.remote_api: true` in configuration.

A probe that ran to completion always returns HTTP 200, even when the
handshake failed. The boolean `success` field in the response body indicates
whether the listener was reachable. HTTP 4xx is reserved for invalid
requests (bad JSON, unknown transport, malformed PEM, etc.).

:::warning Treat as outbound network access
An authenticated caller of `/api/probe` can dial any `host:port` reachable
from the agent, including internal addresses the caller could not reach
directly. The probe only sends a `PEER_HELLO` handshake, but the TCP/TLS
dial itself is a stronger primitive than anything else exposed by the
dashboard API. Protect it with `http.token_hash` and gate the endpoint
groups with `http.dashboard` / `http.remote_api`. The remote variant is
additionally blocked when management-key decryption is unavailable.
:::

---

## POST /api/probe

Probe a Muti Metroo listener from the local agent.

### Request

```bash
curl -X POST http://localhost:8080/api/probe \
  -H "Content-Type: application/json" \
  -d '{"transport": "quic", "address": "agent2.example.com:4433"}'
```

Probe over HTTP/2 with strict TLS verification using a custom CA bundle:

```bash
curl -X POST http://localhost:8080/api/probe \
  -H "Content-Type: application/json" \
  -d @- <<'JSON'
{
  "transport": "h2",
  "address": "agent2.example.com:443",
  "path": "/mesh",
  "strict_verify": true,
  "ca_cert_pem": "-----BEGIN CERTIFICATE-----\n...\n-----END CERTIFICATE-----\n"
}
JSON
```

Probe a plaintext WebSocket listener (e.g. behind a reverse proxy):

```bash
curl -X POST http://localhost:8080/api/probe \
  -H "Content-Type: application/json" \
  -d '{"transport": "ws", "address": "127.0.0.1:8080", "path": "/mesh", "plaintext": true}'
```

### Request Body

| Field             | Type    | Required | Description                                                                   |
| ----------------- | ------- | -------- | ----------------------------------------------------------------------------- |
| `address`         | string  | Yes      | Target `host:port` to probe                                                   |
| `transport`       | string  | No       | `quic` (default), `h2`, or `ws`                                               |
| `path`            | string  | No       | HTTP path for `h2`/`ws` transports (default `/mesh`)                          |
| `timeout_ms`      | integer | No       | Probe timeout in milliseconds (default `10000`, max `60000`)                  |
| `strict_verify`   | boolean | No       | If `true`, verify the server's TLS certificate (default `false`)              |
| `plaintext`       | boolean | No       | Use plaintext (no TLS); valid only with `transport: ws`                       |
| `ca_cert_pem`     | string  | No       | Inline CA certificate PEM for verification                                    |
| `client_cert_pem` | string  | No       | Inline client certificate PEM for mTLS (must be paired with `client_key_pem`) |
| `client_key_pem`  | string  | No       | Inline client key PEM for mTLS (must be paired with `client_cert_pem`)        |
| `alpn_protocol`   | string  | No       | Override the ALPN identifier                                                  |
| `http_header`     | string  | No       | Override the protocol HTTP header                                             |
| `ws_subprotocol`  | string  | No       | Override the WebSocket subprotocol                                            |

:::note Inline PEM, not file paths
Unlike the `muti-metroo probe` CLI, the HTTP API never accepts filesystem paths for
TLS material. Provide PEM blocks inline in the request body. This avoids
turning the API into a remote file-read primitive.
:::

### Response

**Probe Succeeded (200)**:

```json
{
  "success": true,
  "transport": "quic",
  "address": "agent2.example.com:4433",
  "remote_id": "abc123def456789012345678",
  "remote_display_name": "agent-eu-west",
  "rtt_ms": 23
}
```

**Probe Ran but Listener Unreachable (200)**:

```json
{
  "success": false,
  "transport": "quic",
  "address": "agent2.example.com:9999",
  "error": "Connection refused - listener not running or port blocked"
}
```

```json
{
  "success": false,
  "transport": "h2",
  "address": "agent2.example.com:443",
  "error": "TLS error - certificate signed by unknown authority (disable verification or provide a trusted CA)"
}
```

**Bad Request (400)**:

```json
{
  "error": "address is required"
}
```

```json
{
  "error": "invalid transport \"foo\" (expected quic, h2, or ws)"
}
```

```json
{
  "error": "plaintext mode is only supported for the ws transport"
}
```

```json
{
  "error": "ca_cert_pem must be a CERTIFICATE PEM block"
}
```

```json
{
  "error": "client_cert_pem and client_key_pem must be provided together"
}
```

```json
{
  "error": "timeout_ms must be between 0 and 60000"
}
```

**Method Not Allowed (405)**:

```
method not allowed
```

**Not Found (404)**:

Returned when `http.dashboard: false` in the agent's configuration.

---

## POST /agents/\{agent-id\}/probe

Probe a Muti Metroo listener from the perspective of a remote mesh agent. The
request is forwarded to the target agent over the mesh control channel,
which executes the probe locally and returns the result. Useful for
verifying connectivity from any vantage point in the mesh from a single
dashboard.

### Request

```bash
curl -X POST http://localhost:8080/agents/abc123def456/probe \
  -H "Content-Type: application/json" \
  -d '{"transport": "quic", "address": "10.0.0.4:4433"}'
```

### Request Body

Same as `/api/probe`.

### Response

Same response format as `/api/probe`. The body reflects the probe result as
seen from the target agent.

### Additional Error Responses

**Forbidden (403)**:

```
probe restricted: management key decryption unavailable
```

**Bad Gateway (502)**:

```
failed to send request: <error details>
```

Returned when the control request to the remote agent fails (agent unknown,
unreachable, or timed out).

---

## Error Responses

| Status | Description                                                                                                   |
| ------ | ------------------------------------------------------------------------------------------------------------- |
| 200    | Probe ran (inspect `success` field for the result)                                                            |
| 400    | Invalid request body, validation error, or malformed PEM                                                      |
| 401    | Bearer token authentication required (when `http.token_hash` is set)                                          |
| 403    | Management key decryption unavailable (remote variant only)                                                   |
| 404    | Endpoint disabled (`http.dashboard: false` for `/api/probe`, `http.remote_api: false` for the remote variant) |
| 405    | Method not allowed (must be POST)                                                                             |
| 502    | Remote agent not found, unreachable, or timed out (remote variant only)                                       |

:::tip success vs HTTP status
The HTTP status reflects whether the API call itself succeeded. The `success`
field in the JSON body reflects whether the probe reached the listener and
completed the handshake. A handshake failure is an HTTP 200 with
`"success": false`, mirroring the `/api/mesh-test` convention.
:::

---

## Examples

### Verify a Set of Listeners

```bash
#!/bin/bash
AGENT="http://localhost:8080"

LISTENERS=(
  "quic agent1.example.com:4433"
  "quic agent2.example.com:4433"
  "h2   agent3.example.com:443"
)

for ENTRY in "${LISTENERS[@]}"; do
  read -r TRANSPORT ADDR <<<"$ENTRY"
  RESULT=$(curl -s -X POST "$AGENT/api/probe" \
    -H "Content-Type: application/json" \
    -d "{\"transport\":\"$TRANSPORT\",\"address\":\"$ADDR\",\"timeout_ms\":3000}")
  OK=$(echo "$RESULT" | jq -r '.success')
  if [ "$OK" = "true" ]; then
    echo "OK   $TRANSPORT $ADDR rtt=$(echo "$RESULT" | jq -r '.rtt_ms')ms"
  else
    echo "FAIL $TRANSPORT $ADDR error=$(echo "$RESULT" | jq -r '.error')"
  fi
done
```

### Probe a Listener From Every Agent

```bash
#!/bin/bash
ENTRY_AGENT="http://localhost:8080"
TARGET="agent2.example.com:4433"

# Get all known agents
AGENTS=$(curl -s "$ENTRY_AGENT/agents" | jq -r '.[].id')

for AGENT_ID in $AGENTS; do
  RESULT=$(curl -s -X POST "$ENTRY_AGENT/agents/$AGENT_ID/probe" \
    -H "Content-Type: application/json" \
    -d "{\"transport\":\"quic\",\"address\":\"$TARGET\",\"timeout_ms\":3000}")
  OK=$(echo "$RESULT" | jq -r '.success')
  echo "$AGENT_ID -> $TARGET: $OK ($(echo "$RESULT" | jq -r '.rtt_ms // .error'))"
done
```
