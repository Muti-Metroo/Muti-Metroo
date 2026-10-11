---
title: Hosts Management Endpoints
description: Add, remove, and list mesh hostname entries and their allowed suffixes on the local agent.
sidebar_position: 10
---

# Hosts Management Endpoints

The mesh hostname registry maps names such as `db.mesh` to in-mesh addresses. The agent uses it to resolve names for SOCKS connections and route trace, and Mutiauk reads it from [`GET /api/dashboard`](/api/dashboard#get-apidashboard) to answer DNS queries. These endpoints change the registry and the set of allowed suffixes at runtime. They belong to the remote API group (`http.remote_api`, enabled by default) and accept only `POST`.

| Endpoint | Description |
| -------- | ----------- |
| `POST /hosts/manage` | Hostname entries of the local agent |
| `POST /hosts/suffixes/manage` | Allowed suffixes of the local agent |

The registry is local to each agent. There is no `/agents/{id}/hosts/...` variant; send the request to each agent that should serve the names.

`add` and `remove` are mutations: the agent serving the request must have `management.signing_private_key`, otherwise it returns `403`. Mutations are written to the [dynamic configuration journal](/configuration/dynamic-config-persistence) when `agent.data_dir` is set, so they survive restart. `list` needs no signing key. Unlike the other management endpoints, these are not blocked by the `management.public_key` topology restriction.

See [Mesh Hostnames](/features/mesh-hostnames) for how names are resolved and [Hosts Configuration](/configuration/hosts) for the `mesh_hosts` and `mesh_hosts_suffixes` keys.

## `POST /hosts/manage`

### Request

| Field | Type | Used by | Description |
| ----- | ---- | ------- | ----------- |
| `action` | string | all | `add`, `remove`, or `list` |
| `name` | string | `add`, `remove` | Hostname, at most 253 characters. Must end with one of the active suffixes, for example `db.mesh`, and must contain at least two labels. Converted to lowercase. |
| `ipv4` | string | `add` | IPv4 address |
| `ipv6` | string | `add` | IPv6 address |
| `comment` | string | `add` | Free-text note |

`add` requires at least one of `ipv4` and `ipv6`. Entries cannot be edited; remove and add again.

### Response

Every action returns the active suffixes and the full list of entries from the configuration and from runtime changes, sorted by name:

```bash
curl -X POST http://localhost:8080/hosts/manage \
  -H "Content-Type: application/json" \
  -d '{"action": "add", "name": "web.mesh", "ipv4": "10.20.0.30", "ipv6": "fd00::30", "comment": "web"}'
```

```json
{
  "suffixes": ["mesh"],
  "hosts": [
    {
      "name": "db.mesh",
      "ipv4": "10.20.0.20",
      "comment": "database"
    },
    {
      "name": "web.mesh",
      "ipv4": "10.20.0.30",
      "ipv6": "fd00::30",
      "comment": "web"
    }
  ]
}
```

| Field | Description |
| ----- | ----------- |
| `suffixes` | Active suffixes, without leading dots |
| `hosts[].name` | Hostname |
| `hosts[].ipv4`, `hosts[].ipv6` | Addresses; each omitted when not set |
| `hosts[].comment` | Comment; omitted when empty |

The response does not mark which entries come from the configuration. Only entries added at runtime can be removed.

### Errors

| Status | Body | Cause |
| ------ | ---- | ----- |
| 400 | `{"error":"invalid host entry: name \"web.lan\" must equal or end with one of .mesh"}` | Name outside the active suffixes |
| 400 | `{"error":"invalid host entry: at least one of ipv4 or ipv6 is required"}` | No address |
| 400 | `{"error":"invalid host entry: ipv4 \"bad\" is not a valid IPv4 address"}` | Invalid address |
| 400 | `{"error":"invalid host entry: name is required"}` | Missing `name` |
| 400 | `{"error":"unknown action \"bogus\" (expected add, remove, or list)"}` | Invalid `action` |
| 400 | `{"error":"invalid request: ..."}` | Body is not valid JSON |
| 403 | `{"error":"config-seeded host, cannot remove dynamically: db.mesh"}` | `remove` of an entry from `mesh_hosts` |
| 403 | `management signing private key not configured on this agent` | Mutation without a signing private key (plain text) |
| 404 | `{"error":"host not found: nope.mesh"}` | `remove` of an unknown name |
| 405 | `method not allowed` | Method other than `POST` |
| 409 | `{"error":"host already exists: web.mesh"}` | `add` of a name that already exists, from the configuration or added at runtime |

## `POST /hosts/suffixes/manage`

The active suffix set starts as `mesh_hosts_suffixes` from the configuration (default `["mesh"]`) and is then changed by runtime `add` and `remove` actions, including removal of configured suffixes.

### Request

| Field | Type | Used by | Description |
| ----- | ---- | ------- | ----------- |
| `action` | string | all | `add`, `remove`, or `list` |
| `suffix` | string | `add`, `remove` | Suffix without a leading or trailing dot, for example `lab` or `corp.lan` |

### Response

Every action returns the active suffix set after the change:

```bash
curl -X POST http://localhost:8080/hosts/suffixes/manage \
  -H "Content-Type: application/json" \
  -d '{"action": "add", "suffix": "lab"}'
```

```json
{
  "suffixes": ["lab", "mesh"]
}
```

### Errors

| Status | Body | Cause |
| ------ | ---- | ----- |
| 400 | `{"error":"invalid host entry: suffix \".bad\" must not start with a dot"}` | Invalid suffix |
| 400 | `{"error":"invalid request: ..."}` | Body is not valid JSON |
| 403 | `management signing private key not configured on this agent` | Mutation without a signing private key (plain text) |
| 404 | `{"error":"host not found: zzz"}` | `remove` of a suffix that is not active |
| 405 | `method not allowed` | Method other than `POST` |
| 409 | `{"error":"host already exists: lab"}` | `add` of a suffix that is already active |
| 409 | `{"error":"suffix has registered hosts: mesh used by db.mesh, web.mesh"}` | `remove` of a suffix that registered names still use |

Remove the dependent entries before removing their suffix. Entries from `mesh_hosts` can only be removed by editing the configuration.

## See Also

- [CLI - host](/cli/host) - The `host` commands that call these endpoints
- [Mesh Hostnames](/features/mesh-hostnames) - How mesh names are resolved
- [Hosts Configuration](/configuration/hosts) - Static entries and suffixes
- [Dashboard Endpoints](/api/dashboard#get-apidashboard) - The `mesh_hosts` and `mesh_hosts_suffixes` fields
- [Route Trace Endpoints](/api/route-trace) - Trace a registered name
