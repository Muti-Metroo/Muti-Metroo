# Route Trace API

HTTP endpoint that explains which route an agent would pick for a given destination, using the same selection rules as the live stream open path.

## Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/route/trace` | POST | Trace on local agent |
| `/agents/{agent-id}/route/trace` | POST | Trace on remote agent |

Both endpoints require `http.remote_api: true` in configuration.

Route trace is a read-only diagnostic. It does not require a management key signature.

---

## POST /route/trace

### Request

```bash
curl -X POST http://localhost:8080/route/trace \
  -H "Content-Type: application/json" \
  -d '{"dest": "8.8.8.8"}'
```

For a hostname, the agent resolves the name in this order — (1) literal-IP shortcut, (2) domain route table, (3) mesh-host registry, (4) system DNS — and returns one trace per resolved IP, plus a domain-route lookup. The mesh-host step lets registered names under any active `mesh_hosts_suffixes` entry trace correctly even when the agent's host has no Mutiauk forwarding the suffix:

```bash
curl -X POST http://localhost:8080/route/trace \
  -H "Content-Type: application/json" \
  -d '{"dest": "example.com"}'
```

### Request Body

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `dest` | string | Yes | IP address or hostname |

### Response (200)

All CIDR routes that match the destination are returned. Exactly one candidate per trace has `winner: true`; the rest carry a `reason` explaining why they lost (`shorter prefix`, `higher metric`, or `lost origin tiebreaker`). When the input was not an IP literal, a `resolution` field reports which source produced the IPs: `"ip"` (input was a literal address), `"mesh"` (mesh-host registry), or `"system"` (system DNS).

```json
{
  "status": "ok",
  "dest": "8.8.8.8",
  "resolution": "ip",
  "resolver_ok": false,
  "ips": [
    {
      "ip": "8.8.8.8",
      "candidates": [
        {
          "network": "8.8.8.0/24",
          "prefix_len": 24,
          "metric": 2,
          "origin": "ab12cd",
          "origin_name": "exit-us",
          "next_hop": "ab12cd",
          "next_hop_name": "exit-us",
          "hop_count": 1,
          "last_update": "2026-04-22T11:30:45Z",
          "winner": true
        },
        {
          "network": "0.0.0.0/0",
          "prefix_len": 0,
          "metric": 1,
          "origin": "ab12cd",
          "origin_name": "exit-us",
          "next_hop": "ab12cd",
          "next_hop_name": "exit-us",
          "hop_count": 1,
          "winner": false,
          "reason": "shorter prefix"
        }
      ]
    }
  ]
}
```

For a hostname, the shape adds `hostname`, `resolver_ok`, `resolution`, and `domain`. A registered `*.mesh` name resolves through the local registry without hitting system DNS and reports `"resolution": "mesh"`; an unregistered name falls through to the resolver and reports `"resolution": "system"`:

```json
{
  "status": "ok",
  "dest": "example.com",
  "hostname": "example.com",
  "resolution": "system",
  "resolver_ok": true,
  "domain": {
    "matched": true,
    "pattern": "*.example.com",
    "is_wildcard": true,
    "metric": 0,
    "origin": "cd34ef",
    "origin_name": "dns-exit",
    "next_hop": "cd34ef",
    "hop_count": 1
  },
  "ips": [
    {"ip": "93.184.215.14", "candidates": [/* ... */]}
  ]
}
```

When no CIDR route matches a given IP, the per-IP entry carries `no_match: true` and no candidates.

### Error Responses

| Status | Description |
|--------|-------------|
| 400 | Invalid request body or missing `dest` |
| 403 | Management key decryption unavailable |
| 405 | Method not allowed (must be POST) |
| 503 | Route trace not configured |

Resolver errors are surfaced in-band (HTTP 200 with `resolver_ok: false` and a human-readable `error` field) rather than as HTTP errors, since a resolution failure is a legitimate trace outcome.

---

## POST /agents/\{agent-id\}/route/trace

Run the trace on a remote agent via the mesh control channel.

### Request

```bash
curl -X POST http://localhost:8080/agents/abc123def456/route/trace \
  -H "Content-Type: application/json" \
  -d '{"dest": "10.1.2.3"}'
```

### Response

Same shape as `/route/trace`. The trace uses the remote agent's routing tables and resolver, so the result reflects what that agent would do -- not what the local agent would do.

---

## Related

- [`muti-metroo route trace`](/cli/route#route-trace) -- CLI wrapper for this endpoint.
- [Route Management API](/api/route-management) -- add, remove, and list dynamic routes.
