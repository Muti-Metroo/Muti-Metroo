# Peer Management API

HTTP endpoints for managing dynamic outbound peer connections at runtime.

Dynamic peers let you instruct an already-running agent to dial a new peer without editing `config.yaml` and restarting. A typical use case: a new agent that can only accept connections but an existing agent (behind a firewall or NAT) must initiate the dial.

## Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/peers/manage` | POST | Manage peer connections on local agent |
| `/agents/{agent-id}/peers/manage` | POST | Manage peer connections on remote agent |

These endpoints require `http.remote_api: true` in configuration.

---

## POST /peers/manage

Manage outbound peer connections on the local agent.

### Request

Add a peer (QUIC, any peer ID accepted):

```bash
curl -X POST http://localhost:8080/peers/manage \
  -H "Content-Type: application/json" \
  -d '{"action": "add", "address": "newagent.example:4433"}'
```

Add a peer with a specific transport and expected ID:

```bash
curl -X POST http://localhost:8080/peers/manage \
  -H "Content-Type: application/json" \
  -d '{"action": "add", "address": "newagent.example:443", "transport": "h2", "id": "abc123def456..."}'
```

Remove a dynamic peer:

```bash
curl -X POST http://localhost:8080/peers/manage \
  -H "Content-Type: application/json" \
  -d '{"action": "remove", "address": "newagent.example:4433"}'
```

List all peers (dynamic and config):

```bash
curl -X POST http://localhost:8080/peers/manage \
  -H "Content-Type: application/json" \
  -d '{"action": "list"}'
```

### Request Body

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `action` | string | Yes | Action to perform: `add`, `remove`, or `list` |
| `address` | string | For add/remove | Peer dial address (e.g. `host:4433`) |
| `transport` | string | No | Transport type: `quic` (default), `h2`, or `ws` |
| `id` | string | No | Expected peer agent ID; omit or `auto` to accept any |

### Response

**Add Success (200)**:

```json
{
  "status": "ok",
  "message": "peer newagent.example:4433 added (transport=quic)"
}
```

**Remove Success (200)**:

```json
{
  "status": "ok",
  "message": "peer newagent.example:4433 removed (disconnected abc123de)"
}
```

When no live connection exists (e.g. still in reconnect backoff), the short ID suffix is omitted.

**List Success (200)**:

```json
{
  "status": "ok",
  "peers": [
    {
      "address": "agent2:4433",
      "transport": "quic",
      "remote_id": "abcdef1234567890abcdef1234567890",
      "connected": true,
      "persistent": true,
      "dynamic": false
    },
    {
      "address": "newagent.example:4433",
      "transport": "quic",
      "remote_id": "9876543210fedcba9876543210fedcba",
      "connected": true,
      "persistent": true,
      "dynamic": true
    },
    {
      "address": "198.51.100.7:54321",
      "transport": "quic",
      "remote_id": "fedcba0987654321fedcba0987654321",
      "connected": true,
      "persistent": false,
      "dynamic": false,
      "direction": "inbound"
    }
  ]
}
```

**Bad Request (400)**:

```json
{ "error": "address is required" }
```

```json
{ "error": "unknown transport \"xyz\" (expected quic, h2, or ws)" }
```

```json
{ "error": "peer \"agent2:4433\" is a config peer and cannot be replaced" }
```

```json
{ "error": "dynamic peer \"newagent.example:4433\" not found" }
```

**Forbidden (403)**:

```
peer management restricted: management key decryption unavailable
```

### Behavior

When a peer is added:
1. Address is required; transport defaults to `quic`; expected ID is parsed if set and not `auto`.
2. If the address belongs to a config peer, the request is rejected.
3. If a dynamic peer with the same address exists, its live connection is closed and its pending reconnect is cancelled, then the peer is replaced.
4. A background dial is started using the agent's global TLS configuration. If the initial dial fails, the peer manager will retry with exponential backoff.

When a peer is removed:
1. The address must belong to a dynamic peer (not from config).
2. Any live connection is closed immediately.
3. Pending reconnect attempts are cancelled.

When listing:
- Outbound peers (config and dynamic) are returned, plus any inbound peers that have dialed into this agent.
- The `dynamic` field distinguishes runtime peers from config-file peers.
- The `direction` field is `"inbound"` for accepted connections and omitted for outbound. For inbound entries, `address` is the remote socket (ephemeral), and `persistent`/`dynamic`/`expected_id` are not applicable and left at their zero values.
- `connected` indicates whether a live connection currently exists; `remote_id` is populated only when connected.

Dynamic peers:
- Are ephemeral (lost on agent restart).
- Inherit TLS settings (CA, client cert, mTLS, strict-verify) from the agent's global config; per-peer TLS overrides are not supported.
- Auto-reconnect on disconnect (persistent=true), just like config peers.

---

## POST /agents/\{agent-id\}/peers/manage

Manage peer connections on a remote agent.

### Request

```bash
curl -X POST http://localhost:8080/agents/abc123def456/peers/manage \
  -H "Content-Type: application/json" \
  -d '{"action": "add", "address": "F.example:4433"}'
```

### Request Body

Same as `/peers/manage`.

### Response

Same response formats as `/peers/manage`.

The request is forwarded to the target agent via the mesh control channel. The response reflects the result from the remote agent.

---

## Error Responses

All endpoints may return:

| Status | Description |
|--------|-------------|
| 400 | Invalid request body or parameters |
| 403 | Management key required but unavailable |
| 404 | Endpoint disabled (`remote_api` not enabled) or agent not found |
| 405 | Method not allowed (must be POST) |
| 503 | Peer management not configured |

:::note Management Key Protection
Peer management endpoints follow the same management key restrictions as route and forward management. Agents with only `management.public_key` (field agents) cannot manage peers. Agents with both keys (operator nodes) can manage peers freely.
:::

:::warning Dynamic Peers are Ephemeral
Dynamic peers added via the API are lost when the agent restarts. For persistent peers, add them to the `peers:` section in the configuration file.
:::
