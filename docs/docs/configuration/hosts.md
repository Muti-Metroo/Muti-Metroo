---
title: Hosts
sidebar_position: 11
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-wiring.png" alt="Mole configuring mesh hostnames" style={{maxWidth: '180px'}} />
</div>

# Mesh Hosts Configuration

Publish a registry of symbolic hostnames that resolve to in-mesh IPs (for example `target-11.mesh -> 10.8.0.11`). Each agent serves its own registry. Entries are consumed by two paths:

- **The agent itself** — SOCKS5 dial and `POST /route/trace` consult the registry before falling through to system DNS, so SOCKS5 clients can address registered names even when no Mutiauk is running on the agent's host.
- **Mutiauk** — the resolver pulls entries from `/api/dashboard` and serves them as authoritative DNS for the configured suffixes (default `.mesh`), so operators on hosts that run Mutiauk can address peers by name through any local tool.

The registry **does not install routes**. Names only translate to IPs - the underlying IP must already be reachable through Muti Metroo (a static `routes:` entry on Mutiauk, or an autoroute fetched from the agent).

For the runtime CLI and HTTP wrappers, see [host CLI](/cli/host) and [Hosts Management API](/api/hosts-management). The end-to-end consumer-side flow is covered in the [Mutiauk guide](/mutiauk#mesh-hostnames).

## Quick Setup

```yaml
mesh_hosts_suffixes: ["mesh"]   # optional; default is ["mesh"]

mesh_hosts:
  - name: gateway.mesh
    ipv4: 10.0.0.1
    ipv6: fd00::1
  - name: target-11.mesh
    ipv4: 10.8.0.11
    comment: "edge worker"
```

After `muti-metroo run`, the entries are immediately available via `GET /api/dashboard` and `muti-metroo host list`.

## Configuration

```yaml
mesh_hosts_suffixes: ["mesh"]   # one or more suffixes; each may be a single label (mesh) or multi-level (muti-metroo.alt)

mesh_hosts:
  - name: <name>            # required, must equal or end with .<one of mesh_hosts_suffixes>
    ipv4: <ipv4>             # at least one of ipv4 or ipv6 required
    ipv6: <ipv6>
    comment: <text>          # optional
```

### `mesh_hosts_suffixes`

The set of DNS suffixes a host name may end with. Defaults to `["mesh"]` when omitted. A suffix can be a single label (`mesh`) or a multi-level domain (`muti-metroo.alt`, `lab.test.mesh`). Each dot-separated piece must be a valid RFC 1035 label (lowercase letters, digits, internal hyphens); the suffix itself must not have a leading or trailing dot. Duplicates are dropped; entries are lowercased on load.

| Type | Default |
|------|---------|
| list of string | `["mesh"]` |

```yaml
mesh_hosts_suffixes: ["mesh", "lan", "alt"]
```

Mutiauk uses these suffixes to scope its resolver - on Linux with systemd-resolved, only `*.<suffix>` queries are forwarded to the in-mesh resolver, so the operator's normal upstream DNS stays untouched.

The active set can be changed at runtime via `muti-metroo host suffix add|remove|list` or `POST /hosts/suffixes/manage`. Runtime mutations are journalled and survive restart, including removal of suffixes that were originally seeded here. See [Runtime Suffix Management](#runtime-suffix-management) below.

### `mesh_hosts`

The static seed of mesh-local hostname entries. Each entry has the following fields:

| Option | Type | Required | Description |
|--------|------|----------|-------------|
| `name` | string | Yes | Hostname; must end with `.<one of mesh_hosts_suffixes>` (lowercased on load). Each label between dots must be a valid RFC 1035 label (a-z0-9, internal hyphens, 1-63 chars; 253 chars total). |
| `ipv4` | string | One of `ipv4`/`ipv6` | IPv4 address. |
| `ipv6` | string | One of `ipv4`/`ipv6` | IPv6 address. Set both for dual-stack. |
| `comment` | string | No | Free-text comment, surfaced in `host list` output. |

Config entries are validated at startup; an invalid name or IP fails the agent boot with `mesh_hosts[N]: invalid host entry: ...`.

## Static vs Runtime Entries

The store keeps two parallel sets:

- **Config-seeded** (this file): immutable at runtime. They cannot be removed via `muti-metroo host remove` or the API; attempting to remove returns HTTP 403. Edit the config and restart to remove a static entry.
- **Runtime** (added via `muti-metroo host add` or `POST /hosts/manage`): journalled to `agent.data_dir/dynamic-config.log` and replayed on restart, same mechanism used for dynamic routes / peers / forward listeners. See [Dynamic Config Persistence](/configuration/dynamic-config-persistence).

Both sets are merged when a client lists hosts; the two are not flagged separately in the output - the distinction matters only when you try to remove.

## Runtime Suffix Management

The active TLD set can be edited without restarting the agent:

```bash
muti-metroo host suffix add alt        # widen the namespace
muti-metroo host suffix list
muti-metroo host suffix remove alt     # narrow it again
```

Removing a suffix that has any registered host (config-seeded or dynamic) returns HTTP 409 with the dependent hostnames; remove those first. Mutations land in the same journal as host add/remove events, so a runtime-added suffix and any host registered under it both replay cleanly on the next start.

## Examples

### IPv4-only registry

```yaml
mesh_hosts_suffixes: ["mesh"]

mesh_hosts:
  - name: web-1.mesh
    ipv4: 10.20.0.10
  - name: web-2.mesh
    ipv4: 10.20.0.11
  - name: db.mesh
    ipv4: 10.20.0.20
    comment: "primary database"
```

### Dual-stack with sub-labels

```yaml
mesh_hosts_suffixes: ["mesh"]

mesh_hosts:
  - name: gateway.eu.mesh
    ipv4: 10.50.0.1
    ipv6: fd00:eu::1
  - name: gateway.us.mesh
    ipv4: 10.51.0.1
    ipv6: fd00:us::1
```

Sub-labels are allowed as long as each part is a valid RFC 1035 label.

### Custom suffix

```yaml
mesh_hosts_suffixes: ["internal"]

mesh_hosts:
  - name: console.internal
    ipv4: 10.10.0.1
```

Use a custom suffix when `.mesh` collides with another local namespace. Operators consuming the registry must run Mutiauk with `dns.suffix: internal` to match.

### Multiple namespaces

```yaml
mesh_hosts_suffixes: ["mesh", "lan", "alt"]

mesh_hosts:
  - name: gateway.mesh
    ipv4: 10.0.0.1
  - name: printer.lan
    ipv4: 10.20.0.50
  - name: console.alt
    ipv4: 10.10.0.1
```

Each Mutiauk consumer picks one suffix to forward (`dns.suffix: lan`); start additional Mutiauk instances if you need more than one namespace served on the same host.

## Verifying Reachability

Verify both halves end-to-end with `mutiauk route trace <name>` from the consumer host - the trace output shows the resolution source (`(via mesh DNS)`) and the matching route. The IP a name resolves to must be reachable through Mutiauk already, either via a static `routes:` entry or via an autoroute fetched from this agent.

## Related

- [host CLI](/cli/host) - Runtime add / remove / list commands
- [Hosts Management API](/api/hosts-management) - HTTP endpoint reference
- [Mutiauk](/mutiauk#mesh-hostnames) - Consumer-side DNS resolver
- [Dynamic Config Persistence](/configuration/dynamic-config-persistence) - Journal format and guarantees
- [Dashboard API](/api/dashboard) - `mesh_hosts` exposure for Mutiauk's poller
