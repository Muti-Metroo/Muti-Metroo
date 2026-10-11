---
title: Route Trace Endpoints
description: Ask an agent which route it would use for an IP address or hostname, and why the other candidates lost.
sidebar_position: 5
---

# Route Trace Endpoints

Route trace explains which route an agent would select for a destination, using the same selection rules as a real connection: longest prefix first, then lowest metric. It is read-only and does not need the management signing key. The endpoints belong to the remote API group (`http.remote_api`, enabled by default) and accept only `POST`.

| Endpoint | Description |
| -------- | ----------- |
| `POST /route/trace` | Trace on the local agent |
| `POST /agents/{id}/route/trace` | Trace on a remote agent |

## `POST /route/trace`

### Request

| Field | Type | Required | Description |
| ----- | ---- | -------- | ----------- |
| `dest` | string | Yes | IP address or hostname |

```bash
curl -X POST http://localhost:8080/route/trace \
  -H "Content-Type: application/json" \
  -d '{"dest": "10.20.1.5"}'
```

For a hostname, the agent looks up the domain route table and then resolves the name to addresses: first from the [mesh hostname registry](/api/hosts-management), then with the system resolver. Each resolved address gets its own CIDR trace.

### Response

**IP address (200):**

```json
{
  "status": "ok",
  "dest": "10.20.1.5",
  "resolution": "ip",
  "resolver_ok": false,
  "ips": [
    {
      "ip": "10.20.1.5",
      "candidates": [
        {
          "network": "10.20.0.0/16",
          "prefix_len": 16,
          "metric": 1,
          "origin": "331974cd",
          "origin_name": "exit-1",
          "next_hop": "653422d2",
          "next_hop_name": "transit",
          "hop_count": 2,
          "last_update": "2026-10-11T11:52:55Z",
          "winner": true,
          "comment": "lab network"
        },
        {
          "network": "10.0.0.0/8",
          "prefix_len": 8,
          "metric": 6,
          "origin": "331974cd",
          "origin_name": "exit-1",
          "next_hop": "653422d2",
          "next_hop_name": "transit",
          "hop_count": 2,
          "last_update": "2026-10-11T11:52:55Z",
          "winner": false,
          "reason": "shorter prefix",
          "comment": "corp WAN"
        }
      ]
    }
  ]
}
```

**Hostname that matches a domain route (200):**

```json
{
  "status": "ok",
  "dest": "www.example.com",
  "hostname": "www.example.com",
  "resolution": "system",
  "resolver_ok": true,
  "ips": [
    {
      "ip": "203.0.113.80",
      "no_match": true
    }
  ],
  "domain": {
    "matched": true,
    "pattern": "*.example.com",
    "is_wildcard": true,
    "metric": 1,
    "origin": "331974cd",
    "origin_name": "exit-1",
    "next_hop": "653422d2",
    "next_hop_name": "transit",
    "hop_count": 2
  }
}
```

When a domain route matches, connections through the SOCKS proxy use it and the exit resolves the name, so the per-address CIDR traces are informational.

**Hostname from the mesh hostname registry (200):**

```json
{
  "status": "ok",
  "dest": "db.mesh",
  "hostname": "db.mesh",
  "resolution": "mesh",
  "resolver_ok": true,
  "ips": [
    {
      "ip": "10.20.0.20",
      "candidates": [
        {
          "network": "10.20.0.0/16",
          "prefix_len": 16,
          "metric": 1,
          "origin": "331974cd",
          "origin_name": "exit-1",
          "next_hop": "653422d2",
          "next_hop_name": "transit",
          "hop_count": 2,
          "last_update": "2026-10-11T11:52:55Z",
          "winner": true,
          "comment": "lab network"
        }
      ]
    }
  ],
  "domain": {
    "matched": false
  }
}
```

**Hostname that does not resolve (200):**

```json
{
  "status": "ok",
  "dest": "nonexistent.invalid",
  "hostname": "nonexistent.invalid",
  "resolver_ok": false,
  "domain": {
    "matched": false
  },
  "error": "resolve nonexistent.invalid: lookup nonexistent.invalid: no such host"
}
```

A resolution failure is a valid trace outcome, so it is reported with status 200.

### Response fields

| Field | Description |
| ----- | ----------- |
| `status` | `ok` |
| `dest` | The requested destination |
| `hostname` | Present when `dest` is not an IP address |
| `resolution` | Where the addresses came from: `ip` (literal address), `mesh` (mesh hostname registry), or `system` (system resolver). Omitted when resolution failed. |
| `resolver_ok` | `true` when the hostname resolved; `false` for literal addresses and failed lookups |
| `ips[].ip` | Address that was traced |
| `ips[].no_match` | `true` when no CIDR route covers the address |
| `ips[].candidates[]` | Every CIDR route that covers the address |
| `domain` | Domain route lookup result; present when `dest` is a hostname |
| `error` | Resolver error, when resolution failed |

Candidate fields:

| Field | Description |
| ----- | ----------- |
| `network`, `prefix_len` | Route prefix and its length |
| `metric` | Metric in this agent's route table; learned routes carry a higher metric than the origin advertises |
| `origin`, `origin_name` | Short ID and display name of the advertising agent |
| `next_hop`, `next_hop_name` | Short ID and display name of the peer the connection would go to |
| `hop_count` | Hops to the origin; `0` for routes this agent originates |
| `last_update` | When the route was last refreshed (UTC) |
| `winner` | `true` for the selected route; exactly one candidate per address is the winner |
| `reason` | Why a candidate lost: `shorter prefix`, `higher metric`, or `lost origin tiebreaker`. For two routes to the same network with equal metrics, the trace picks the lower origin agent ID, but a real connection may use either route |
| `comment` | Route comment, when set |

`domain` fields when `matched` is `true`: `pattern`, `is_wildcard`, `metric`, `origin`, `origin_name`, `next_hop`, `next_hop_name`, and `hop_count`, with the same meanings.

### Errors

| Status | Body | Cause |
| ------ | ---- | ----- |
| 400 | `{"error":"dest is required"}` | Missing or empty `dest` |
| 400 | `{"error":"invalid request: ..."}` | Body is not valid JSON |
| 400 | `{"error":"request body: ..."}` | Body is larger than 16 KB |
| 403 | `route trace restricted: management key decryption unavailable` | `management.public_key` is set without `management.private_key` |
| 404 | HTML page | `http.remote_api` is disabled |
| 405 | `method not allowed` | Method other than `POST` |

## `POST /agents/{id}/route/trace`

Runs the trace on a remote agent, using that agent's routing tables, mesh hostname registry, and resolver. The request and response are the same as for [`POST /route/trace`](#post-routetrace).

```bash
curl -X POST http://localhost:8080/agents/331974cd4a0be0d7f945ed9d367a0211/route/trace \
  -H "Content-Type: application/json" \
  -d '{"dest": "10.20.1.5"}'
```

Additional errors:

| Status | Body | Cause |
| ------ | ---- | ----- |
| 400 | `invalid agent ID format` | `{id}` is not a full agent ID |
| 502 | `failed to send request: <reason>` | The target is unknown, unreachable, or did not answer within 30 seconds |

## See Also

- [CLI - route trace](/cli/route) - Command-line wrapper for these endpoints
- [Route Management Endpoints](/api/route-management) - Add and remove routes
- [Routing](/concepts/routing) - How routes are selected
- [Mesh Hostnames](/features/mesh-hostnames) - Names resolved from the mesh hostname registry
