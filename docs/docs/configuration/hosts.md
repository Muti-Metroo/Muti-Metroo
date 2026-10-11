---
title: Mesh Hosts
description: Configuration reference for mesh_hosts and mesh_hosts_suffixes, the per-agent registry of mesh-local host names that SOCKS clients and Mutiauk can resolve.
sidebar_position: 9
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-wiring.png" alt="Mole configuring mesh hostnames" style={{maxWidth: '180px'}} />
</div>

# Mesh Hosts

The top-level `mesh_hosts` and `mesh_hosts_suffixes` keys define a registry of host names that map to addresses inside the mesh, for example `target-11.mesh` to `10.8.0.11`. Each agent keeps its own registry; it is not shared with other agents.

The registry is used in two places:

- **SOCKS ingress on this agent.** A registered name sent by a SOCKS client resolves to its configured address without asking the system resolver. Domain routes still take precedence; see [SOCKS hostname resolution](/configuration/socks5#hostname-resolution).
- **Mutiauk.** Mutiauk receives the registry from the agent over the agent's SOCKS5 listener and answers DNS queries for the registered suffixes. See [Mutiauk mesh hostnames](/mutiauk#mesh-hostnames).

The registry only translates names. The address must still be reachable through an exit route.

## Configuration

```yaml
mesh_hosts_suffixes: ["mesh"]

mesh_hosts:
  - name: gateway.mesh
    ipv4: 10.0.0.1
    ipv6: fd00::1
  - name: target-11.mesh
    ipv4: 10.8.0.11
    comment: "edge worker"
```

## Keys

| Key                   | Type   | Default    | Description                                                                                                         |
| --------------------- | ------ | ---------- | ------------------------------------------------------------------------------------------------------------------- |
| `mesh_hosts_suffixes` | list   | `["mesh"]` | DNS suffixes a host name may end with. An empty or omitted list means `["mesh"]`.                                   |
| `mesh_hosts`          | list   | `[]`       | Host entries.                                                                                                       |
| `mesh_hosts[].name`   | string | required   | Host name ending with `.` and one of the suffixes.                                                                  |
| `mesh_hosts[].ipv4`   | string | `""`       | IPv4 address. At least one of `ipv4` and `ipv6` is required.                                                       |
| `mesh_hosts[].ipv6`   | string | `""`       | IPv6 address. Set both for a dual-stack name.                                                                       |
| `mesh_hosts[].comment`| string | `""`       | Free-text note shown by `muti-metroo host list`.                                                                    |

The singular `mesh_hosts_suffix` key from earlier releases is rejected by validation with a message to rename it to `mesh_hosts_suffixes`.

## Suffixes

```yaml
mesh_hosts_suffixes: ["mesh", "lab.example.com"]
```

A suffix is one DNS label (`mesh`) or several (`lab.example.com`). Each label must be a valid DNS label: letters, digits, and inner hyphens. A suffix must not start or end with a dot. Suffixes are lowercased, and duplicates are dropped.

Mutiauk uses the suffixes to decide which queries it answers, so pick suffixes that do not collide with names your normal DNS serves.

## Host names

Names are lowercased when loaded and must:

- end with `.` plus one of the suffixes (a name may also equal a multi-label suffix, but not a single-label one such as `mesh`),
- consist of valid DNS labels,
- be at most 253 characters long,
- be unique within the file.

`ipv4` must be an IPv4 address and `ipv6` an IPv6 address. An invalid entry fails validation:

```text
mesh_hosts[0]: invalid host entry: name "gateway.lan" must equal or end with one of .mesh
```

## Runtime entries

Entries and suffixes can be added and removed on a running agent with [`muti-metroo host`](/cli/host) and [`muti-metroo host suffix`](/cli/host), or the `/hosts/manage` and `/hosts/suffixes/manage` HTTP endpoints. Runtime changes persist across restarts when `agent.data_dir` is set; see [Dynamic Config Persistence](/configuration/dynamic-config-persistence).

- Entries from this file cannot be removed at runtime (HTTP 403).
- A suffix from this file can be removed at runtime, and the removal persists.
- A suffix that still has registered names cannot be removed (HTTP 409); remove the names first.

## Examples

### Dual-stack names under sub-labels

```yaml
mesh_hosts:
  - name: gateway.eu.mesh
    ipv4: 10.50.0.1
    ipv6: fd00:50::1
  - name: gateway.us.mesh
    ipv4: 10.51.0.1
    ipv6: fd00:51::1
```

### Custom suffix

```yaml
mesh_hosts_suffixes: ["internal"]

mesh_hosts:
  - name: console.internal
    ipv4: 10.10.0.1
```

## See Also

- [host CLI](/cli/host) to add, remove, and list names and suffixes at runtime
- [Hosts Management API](/api/hosts-management) for the HTTP endpoints
- [Mutiauk](/mutiauk#mesh-hostnames) for resolving registered names on a client host
- [SOCKS Proxy](/configuration/socks5#hostname-resolution) for where the registry fits in SOCKS name resolution
- [Dashboard API](/api/dashboard) for the `mesh_hosts` data in the dashboard response
