---
title: Route Management Endpoints
description: Add, remove, list, and annotate CIDR exit routes at runtime, bind them to a source IP or interface, and trigger route advertisement.
sidebar_position: 4
---

# Route Management Endpoints

These endpoints change the CIDR exit routes an agent advertises without editing its configuration or restarting it, and push route updates to peers immediately. They belong to the remote API group (`http.remote_api`, enabled by default) and accept only `POST`.

| Endpoint | Description |
| -------- | ----------- |
| `POST /routes/manage` | Manage routes on the local agent |
| `POST /agents/{id}/routes/manage` | Manage routes on a remote agent |
| `POST /routes/advertise` | Advertise the local routing table to peers now |

Domain routes (`exit.domain_routes`) cannot be changed at runtime.

## `POST /routes/manage`

The request body is a JSON object whose `action` selects the operation.

| Field | Type | Used by | Description |
| ----- | ---- | ------- | ----------- |
| `action` | string | all | `add`, `remove`, `list`, `set_comment`, `set_source_ip`, or `set_interface` |
| `network` | string | all except `list` | CIDR prefix, for example `10.0.0.0/8`. A bare address such as `10.0.0.1` is rejected. |
| `metric` | integer | `add` | Route metric, 0 to 65535. Default `0`; lower is preferred. |
| `comment` | string | `add`, `set_comment` | Operator note shown in route lists, the dashboard, and route trace on every agent that learns the route. An empty string clears it. |
| `source_ip` | string | `add`, `set_source_ip` | Local address the exit binds outbound connections for this prefix to. Must be the same address family as `network`. An empty string clears it. |
| `interface` | string | `add`, `set_interface` | Network interface to send outbound connections for this prefix through. An empty string clears it. |
| `scope` | string | `list` | `dynamic` (default) returns runtime-added routes; `all` also returns routes from `exit.routes` |

Every action except `list` is a mutation: the agent serving the request must have `management.signing_private_key`, otherwise it returns `403`. Mutations are written to the [dynamic configuration journal](/configuration/dynamic-config-persistence) when `agent.data_dir` is set and are replayed on restart.

`source_ip` and `interface` apply only on the agent that originates the route and are never advertised to peers. When `source_ip` is set and is assigned to `interface`, binding works on every platform. An `interface` with a `source_ip` that is not assigned to it requires Linux and root; the request is rejected otherwise. See [Exit Configuration](/configuration/exit) for the equivalent config keys.

### `add`

Adds a dynamic route and advertises it immediately. Adding a prefix that already exists as a dynamic route replaces it; a prefix from `exit.routes` cannot be added. An agent without an exit role becomes an exit for the new route.

```bash
curl -X POST http://localhost:8080/routes/manage \
  -H "Content-Type: application/json" \
  -d '{"action": "add", "network": "10.0.0.0/8", "metric": 5, "comment": "corp"}'
```

```json
{
  "status": "ok",
  "message": "route 10.0.0.0/8 added"
}
```

### `remove`

Removes a dynamic route and withdraws it from peers immediately, without waiting for `routing.route_ttl` to expire. Routes from `exit.routes` cannot be removed.

```bash
curl -X POST http://localhost:8080/routes/manage \
  -H "Content-Type: application/json" \
  -d '{"action": "remove", "network": "10.0.0.0/8"}'
```

```json
{
  "status": "ok",
  "message": "route 10.0.0.0/8 removed"
}
```

### `set_comment`

Sets or clears the comment on a dynamic route or a route from `exit.routes`. The override survives restart, and the route is re-advertised so peers see the new comment.

```bash
curl -X POST http://localhost:8080/routes/manage \
  -H "Content-Type: application/json" \
  -d '{"action": "set_comment", "network": "10.0.0.0/8", "comment": "corp WAN"}'
```

```json
{
  "status": "ok",
  "message": "route 10.0.0.0/8 comment updated"
}
```

### `set_source_ip`

Sets or clears the local source address for a dynamic route or a route from `exit.routes`. Requires the agent to be an exit.

```bash
curl -X POST http://localhost:8080/routes/manage \
  -H "Content-Type: application/json" \
  -d '{"action": "set_source_ip", "network": "10.0.0.0/8", "source_ip": "192.168.1.50"}'
```

```json
{
  "status": "ok",
  "message": "route 10.0.0.0/8 source_ip updated"
}
```

Use [`GET /api/interfaces`](/api/dashboard#get-apiinterfaces) or [`GET /agents/{id}/interfaces`](/api/agents#get-agentsidinterfaces) to list the addresses available on the exit.

### `set_interface`

Sets or clears the egress interface for a dynamic route or a route from `exit.routes`. Requires the agent to be an exit.

```bash
curl -X POST http://localhost:8080/routes/manage \
  -H "Content-Type: application/json" \
  -d '{"action": "set_interface", "network": "10.0.0.0/8", "interface": "eth1"}'
```

```json
{
  "status": "ok",
  "message": "route 10.0.0.0/8 interface updated"
}
```

### `list`

Lists local routes. With the default scope only dynamic routes are returned; when there are none, the `routes` field is omitted.

```bash
curl -X POST http://localhost:8080/routes/manage \
  -H "Content-Type: application/json" \
  -d '{"action": "list", "scope": "all"}'
```

```json
{
  "status": "ok",
  "routes": [
    {
      "network": "10.0.0.0/8",
      "metric": 5,
      "dynamic": true,
      "comment": "corp WAN",
      "source_ip": "192.168.1.50",
      "interface": "eth1"
    },
    {
      "network": "10.20.0.0/16",
      "metric": 0,
      "dynamic": false,
      "comment": "lab network"
    },
    {
      "network": "192.0.2.0/24",
      "metric": 0,
      "dynamic": false
    }
  ]
}
```

| Field | Description |
| ----- | ----------- |
| `network` | CIDR prefix |
| `metric` | Route metric |
| `dynamic` | `true` for routes added at runtime, `false` for routes from `exit.routes` |
| `comment` | Comment, omitted when empty |
| `source_ip` | Bound source address, omitted when unset |
| `interface` | Bound egress interface, omitted when unset |

### Errors

Errors returned by the operation use status `400` and a JSON body:

| Body | Cause |
| ---- | ----- |
| `{"error":"invalid CIDR \"10.0.0.1\": invalid CIDR address: 10.0.0.1"}` | `network` missing or not a CIDR prefix |
| `{"error":"route 10.20.0.0/16 exists as a config route"}` | `add` of a prefix from `exit.routes` |
| `{"error":"route 10.20.0.0/16 is a config route and cannot be removed dynamically"}` | `remove` of a prefix from `exit.routes` |
| `{"error":"route 172.16.0.0/12 not found"}` | `remove` of an unknown prefix |
| `{"error":"exit handler not configured"}` | `set_source_ip` or `set_interface` on an agent that is not an exit |
| `{"error":"unknown scope \"zzz\" (expected \"\", \"dynamic\", or \"all\")"}` | Invalid `scope` |
| `{"error":"unknown action \"bogus\" (expected add, remove, list, set_comment, set_source_ip, or set_interface)"}` | Invalid `action` |
| `{"error":"invalid request: ..."}` | Body is not valid JSON |

Other statuses return a plain-text body:

| Status | Body | Cause |
| ------ | ---- | ----- |
| 403 | `management signing private key not configured on this agent` | Mutation without a signing private key |
| 403 | `route management restricted: management key decryption unavailable` | `management.public_key` is set without `management.private_key` |
| 404 | HTML page | `http.remote_api` is disabled |
| 405 | `method not allowed` | Method other than `POST` |

## `POST /agents/{id}/routes/manage`

Runs the same operations on a remote agent. The request body and responses are identical to [`POST /routes/manage`](#post-routesmanage). `{id}` is the full 32-character agent ID.

```bash
curl -X POST http://localhost:8080/agents/331974cd4a0be0d7f945ed9d367a0211/routes/manage \
  -H "Content-Type: application/json" \
  -d '{"action": "add", "network": "172.16.0.0/12", "metric": 1}'
```

```json
{
  "status": "ok",
  "message": "route 172.16.0.0/12 added"
}
```

The local agent signs mutations with its signing private key and forwards the request over the mesh; the target verifies the signature with its `management.signing_public_key`. `list` is forwarded unsigned.

Additional errors:

| Status | Body | Cause |
| ------ | ---- | ----- |
| 400 | `{"error":"management signing public key not configured on this agent"}` | The target has no signing public key |
| 400 | `{"error":"..."}` | The target rejected the signature or the operation |
| 400 | `invalid agent ID format` | `{id}` is not a full agent ID |
| 502 | `failed to send request: <reason>` | The target is unknown, unreachable, or did not answer within 30 seconds |

## `POST /routes/advertise`

Sends the local routing table to all peers now instead of at the next `routing.advertise_interval` (default 2 minutes). The request body is ignored. Route mutations already trigger an advertisement, so this is mainly useful after network changes or for troubleshooting propagation.

```bash
curl -X POST http://localhost:8080/routes/advertise
```

```json
{
  "message": "route advertisement triggered",
  "status": "triggered"
}
```

## Example: copy dynamic routes to another agent

```bash
#!/bin/bash
API="http://localhost:8080"
TARGET="331974cd4a0be0d7f945ed9d367a0211"

curl -s -X POST "$API/routes/manage" -d '{"action": "list"}' \
  | jq -c '.routes // [] | .[] | {action: "add", network, metric, comment}' \
  | while read -r BODY; do
      curl -s -X POST "$API/agents/$TARGET/routes/manage" -d "$BODY"
      echo
    done
```

## See Also

- [CLI - route](/cli/route) - The `route` commands that call these endpoints
- [Route Trace Endpoints](/api/route-trace) - Check which route a destination would use
- [Exit Configuration](/configuration/exit) - Static routes, `source_ip`, and `interface`
- [Dynamic Configuration Persistence](/configuration/dynamic-config-persistence) - How runtime changes survive restart
- [Routing Configuration](/configuration/routing) - Advertisement interval and route TTL
