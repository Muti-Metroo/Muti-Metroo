# Hosts Management API

HTTP endpoint for managing the agent's mesh hostname registry at runtime.

The mesh-host registry maps symbolic names like `target-11.mesh` to in-mesh IPs. It is local to each agent and surfaced to Mutiauk's DNS resolver via the `/api/dashboard` poll. Static seed entries live in the agent's `mesh_hosts:` config block; runtime entries added through this endpoint are journalled to `agent.data_dir` and survive restarts.

See [Hosts Configuration](/configuration/hosts) for the YAML schema and [host CLI](/cli/host) for the command-line wrapper.

## Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/hosts/manage` | POST | Manage mesh-host entries on the local agent |
| `/hosts/suffixes/manage` | POST | Manage the active TLD suffix set on the local agent |

There is **no `/agents/{id}/hosts/manage` remote variant**: the registry is intentionally local-only in v1. Run the request directly against each agent that should serve the names.

Both endpoints require `http.dashboard: true` in configuration.

---

## POST /hosts/manage

Add, remove, or list mesh-host entries on the local agent.

### Request

Add an IPv4-only entry with a comment:

```bash
curl -X POST http://localhost:8080/hosts/manage \
  -H "Content-Type: application/json" \
  -d '{"action": "add", "name": "target-11.mesh", "ipv4": "10.8.0.11", "comment": "edge worker"}'
```

Add a dual-stack entry:

```bash
curl -X POST http://localhost:8080/hosts/manage \
  -H "Content-Type: application/json" \
  -d '{"action": "add", "name": "gateway.mesh", "ipv4": "10.0.0.1", "ipv6": "fd00::1"}'
```

Remove a runtime entry:

```bash
curl -X POST http://localhost:8080/hosts/manage \
  -H "Content-Type: application/json" \
  -d '{"action": "remove", "name": "target-11.mesh"}'
```

List all entries (config + dynamic, merged):

```bash
curl -X POST http://localhost:8080/hosts/manage \
  -H "Content-Type: application/json" \
  -d '{"action": "list"}'
```

### Request Body

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `action` | string | Yes | `add`, `remove`, or `list` |
| `name` | string | For add/remove | Hostname; must equal one of the active suffixes (e.g. `mesh`) or end with one of them under a label prefix (e.g. `target-11.mesh`) |
| `ipv4` | string | For add | IPv4 address; at least one of `ipv4` or `ipv6` is required |
| `ipv6` | string | For add | IPv6 address; at least one of `ipv4` or `ipv6` is required |
| `comment` | string | No | Free-text comment (add only) |

### Response

Every action returns the merged `{suffixes, hosts[]}` payload, so callers do not need a follow-up `list` request.

**Success (200)**:

```json
{
  "suffixes": ["mesh"],
  "hosts": [
    {"name": "gateway.mesh", "ipv4": "10.0.0.1", "ipv6": "fd00::1"},
    {"name": "target-11.mesh", "ipv4": "10.8.0.11", "comment": "edge worker"}
  ]
}
```

The `suffixes` field reports the active TLD set so dashboard clients can validate names client-side.

**Bad Request (400)**:

```json
{ "error": "invalid host entry: name \"target-11.lan\" must equal or end with one of .mesh" }
```

```json
{ "error": "invalid host entry: at least one of ipv4 or ipv6 is required" }
```

```json
{ "error": "invalid host entry: ipv4 \"not-an-ip\" is not a valid IPv4 address" }
```

**Forbidden (403)**:

```json
{ "error": "config-seeded host, cannot remove dynamically: gateway.mesh" }
```

Returned when removal targets an entry that came from the config file. Edit `mesh_hosts:` and restart instead.

**Not Found (404)**:

```json
{ "error": "host not found: target-99.mesh" }
```

**Conflict (409)**:

```json
{ "error": "host already exists: target-11.mesh" }
```

Returned when `add` collides with either a config seed or an existing dynamic entry.

**Service Unavailable (503)**:

```
mesh host management not configured
```

Returned when the agent is built or wired without a mesh-host provider.

### Behavior

When an entry is added:
1. The name is lowercased and validated (must equal one of the active suffixes or end with one under a label prefix, RFC 1035 labels, 253-char cap).
2. IPs are parsed and canonicalized; at least one family is required.
3. If management-key encryption is configured, the request is signed with the management signing key (same gate as `muti-metroo route add` / `muti-metroo peer add`).
4. The entry is appended to the dynamic-config journal under `agent.data_dir/dynamic-config.log`. Without a writable `data_dir`, the entry remains ephemeral and is lost on restart.

When an entry is removed:
1. Only dynamic entries can be removed; config-seeded entries return 403.
2. The removal is journalled so the entry stays gone after restart.

When listing:
- Both config and dynamic entries are returned, sorted by name.
- The two sets are not flagged separately in the output; the distinction matters only when removing.

---

## POST /hosts/suffixes/manage

Add, remove, or list TLD suffixes the agent accepts when validating mesh hostnames. The active set is the union of `mesh_hosts_suffixes:` from config and any runtime add/remove journalled via this endpoint -- including removals of suffixes that were originally seeded by config.

### Request

Add a new TLD:

```bash
curl -X POST http://localhost:8080/hosts/suffixes/manage \
  -H "Content-Type: application/json" \
  -d '{"action": "add", "suffix": "alt"}'
```

Remove a TLD:

```bash
curl -X POST http://localhost:8080/hosts/suffixes/manage \
  -H "Content-Type: application/json" \
  -d '{"action": "remove", "suffix": "alt"}'
```

List the active TLD set:

```bash
curl -X POST http://localhost:8080/hosts/suffixes/manage \
  -H "Content-Type: application/json" \
  -d '{"action": "list"}'
```

### Request Body

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `action` | string | Yes | `add`, `remove`, or `list` |
| `suffix` | string | For add/remove | Single RFC 1035 label, no leading dot (e.g. `alt`, not `.alt`) |

### Response

Every action returns the post-mutation active set:

```json
{ "suffixes": ["alt", "mesh"] }
```

### Status Codes

| Status | Meaning |
|--------|---------|
| 200 | Success |
| 400 | Invalid suffix label (empty, leading dot, bad characters) |
| 404 | Remove targeted a suffix that is not currently active |
| 409 | Add: suffix already active. Remove: suffix still has registered hosts (the error body lists them) |

A removal that conflicts with registered hosts looks like:

```json
{ "error": "suffix has registered hosts: mesh used by gateway.mesh, target-11.mesh" }
```

Remove the dependent hosts first, then retry.

---

## Dashboard Exposure

The merged registry and the active suffix set are also available through `GET /api/dashboard` under the top-level `mesh_hosts` and `mesh_hosts_suffixes` fields. Mutiauk's autoroutes poller consumes these to populate its in-process DNS server, and the agent itself consults the registry for SOCKS5 dial and `POST /route/trace` hostname resolution -- so registered names work even on agents whose host has no Mutiauk forwarding the suffix. See [Dashboard API Endpoints](/api/dashboard) for the full payload shape.

---

## Authorization

When the agent has `http.token_hash` configured, all `/hosts/manage` and `/hosts/suffixes/manage` requests must include `Authorization: Bearer <token>` (or `?token=<token>`); the splash page and `/health` endpoints are the only exemptions.

When the mesh has management-key encryption enabled, mutations (`add`, `remove`) additionally require the management signing key on the agent receiving the request, following the same rules as route and peer management. `list` is a read-only operation and is not gated by management-key signing. Agents without the signing private key reject mutations with HTTP 403.
