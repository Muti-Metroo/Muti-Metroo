---
title: Probe Endpoints
description: Test whether a Muti Metroo listener is reachable and completes the handshake, from the local agent or from any remote agent.
sidebar_position: 17
---

# Probe Endpoints

A probe dials a Muti Metroo listener, performs the peer handshake, and reports whether it succeeded, the round-trip time, and the remote agent's ID, without keeping a peer connection. It is the HTTP form of the [`muti-metroo probe`](/cli/probe) command.

| Endpoint | Group | Description |
| -------- | ----- | ----------- |
| `POST /api/probe` | dashboard (`http.dashboard`) | Probe from the agent serving the request |
| `POST /agents/{id}/probe` | remote API (`http.remote_api`) | Probe from a remote agent |

Probes need no management signing key.

:::warning
A probe makes the agent open a TCP or UDP connection to any `host:port` the caller names, including internal addresses the caller cannot reach directly. Protect the HTTP API with `http.token_hash`, and disable the endpoint groups you do not need.
:::

## `POST /api/probe`

### Request

| Field | Type | Required | Description |
| ----- | ---- | -------- | ----------- |
| `address` | string | Yes | Listener `host:port` |
| `transport` | string | No | `quic` (default), `h2`, or `ws` |
| `path` | string | No | HTTP path for `h2` and `ws`; default `/mesh` |
| `timeout_ms` | integer | No | Probe timeout in milliseconds, up to `60000`; default `10000` |
| `strict_verify` | boolean | No | Verify the listener's TLS certificate; default `false` |
| `plaintext` | boolean | No | Connect without TLS; `ws` only |
| `ca_cert_pem` | string | No | CA certificate PEM for `strict_verify` |
| `client_cert_pem` | string | No | Client certificate PEM for listeners that require mTLS; requires `client_key_pem` |
| `client_key_pem` | string | No | Client private key PEM; requires `client_cert_pem` |
| `alpn_protocol` | string | No | ALPN identifier, when the listener uses a custom `protocol.alpn` |
| `http_header` | string | No | Protocol HTTP header, when the listener uses a custom `protocol.http_header` |
| `ws_subprotocol` | string | No | WebSocket subprotocol, when the listener uses a custom `protocol.ws_subprotocol` |

TLS material is passed inline as PEM text. Unlike the CLI, the API does not accept file paths.

```bash
curl -X POST http://localhost:8080/api/probe \
  -H "Content-Type: application/json" \
  -d '{"transport": "h2", "address": "relay.example.com:443", "path": "/mesh"}'
```

### Response

A probe that ran returns `200`, whether or not the listener answered. Check `success`.

**Listener reachable:**

```json
{
  "success": true,
  "transport": "h2",
  "address": "relay.example.com:443",
  "remote_id": "653422d2fc10b554514c5cbe6e6cd6cd",
  "remote_display_name": "transit",
  "rtt_ms": 1
}
```

**Listener not reachable:**

```json
{
  "success": false,
  "transport": "quic",
  "address": "relay.example.com:4499",
  "error": "Connection timed out - firewall may be blocking"
}
```

| Field | Description |
| ----- | ----------- |
| `success` | The handshake completed |
| `transport`, `address` | What was probed |
| `remote_id`, `remote_display_name` | The listener's agent ID and display name, when the handshake completed |
| `rtt_ms` | Handshake round-trip time in whole milliseconds; omitted when it is `0` or the probe failed |
| `error` | Failure reason with a hint, for example connection refused, timeout, or a TLS verification error |

### Errors

| Status | Body | Cause |
| ------ | ---- | ----- |
| 400 | `{"error":"address is required"}` | Missing `address` |
| 400 | `{"error":"invalid transport \"foo\" (expected quic, h2, or ws)"}` | Invalid `transport` |
| 400 | `{"error":"plaintext mode is only supported for the ws transport"}` | `plaintext` with `quic` or `h2` |
| 400 | `{"error":"timeout_ms must be between 0 and 60000"}` | Timeout out of range |
| 400 | `{"error":"ca_cert_pem must be a CERTIFICATE PEM block"}` | Invalid CA PEM |
| 400 | `{"error":"client_cert_pem and client_key_pem must be provided together"}` | Only one of the client PEMs |
| 400 | `{"error":"request body: ..."}` | Body larger than 1 MB |
| 404 | HTML page | `http.dashboard` is disabled |
| 405 | `method not allowed` | Method other than `POST` |

## `POST /agents/{id}/probe`

Runs the probe on a remote agent, which is useful for checking that a listener is reachable from a particular place in the mesh. The request and response are the same as for [`POST /api/probe`](#post-apiprobe).

```bash
curl -X POST http://localhost:8080/agents/331974cd4a0be0d7f945ed9d367a0211/probe \
  -H "Content-Type: application/json" \
  -d '{"transport": "quic", "address": "10.20.0.4:4433"}'
```

Additional errors:

| Status | Body | Cause |
| ------ | ---- | ----- |
| 400 | `invalid agent ID format` | `{id}` is not a full agent ID |
| 403 | `probe restricted: management key decryption unavailable` | `management.public_key` is set without `management.private_key` |
| 404 | HTML page | `http.remote_api` is disabled |
| 502 | `failed to send request: <reason>` | The target is unknown, unreachable, or did not answer within 30 seconds |

## Example: probe one listener from every agent

```bash
#!/bin/bash
API="http://localhost:8080"
LISTENER="relay.example.com:4433"

for ID in $(curl -s "$API/agents" | jq -r '.[] | select(.local | not) | .id'); do
  RESULT=$(curl -s -X POST "$API/agents/$ID/probe" \
    -d "{\"address\": \"$LISTENER\", \"timeout_ms\": 3000}")
  echo "$ID: $(echo "$RESULT" | jq -r 'if .success then "ok \(.rtt_ms // 0) ms" else .error end')"
done
```

## See Also

- [probe CLI](/cli/probe): probe from the command line
- [Dashboard Endpoints](/api/dashboard#get-post-apimesh-test): reachability test of every agent
- [Listeners Configuration](/configuration/listeners): listener transports and paths
- [Protocol Configuration](/configuration/protocol): custom ALPN, header, and subprotocol values
