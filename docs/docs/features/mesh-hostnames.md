---
title: Mesh Hostnames
description: Give in-mesh IP addresses short names such as gateway.mesh that SOCKS clients and Mutiauk users can connect to.
sidebar_position: 5
---

# Mesh Hostnames

An agent can keep a small registry of names that map to IP addresses reachable through the mesh, for example `gateway.mesh` to `10.0.0.1`. Operators then connect to the name instead of remembering the address. The registry is local to each agent: it is not advertised to other agents.

Two consumers read the registry:

- **The agent's own SOCKS listener.** When a SOCKS client sends a hostname, the ingress agent looks it up in the registry before asking the system resolver.
- **Mutiauk.** Mutiauk fetches the registry from the agent and answers DNS queries for the registered suffixes, so any local program can use the names.

A name only translates to an IP. The IP still needs a route: an exit agent must advertise a CIDR that covers it (see [Exit Routing](/features/exit-routing)).

## Register names in the configuration

Declare names under `mesh_hosts`. Every name must end with one of the suffixes in `mesh_hosts_suffixes` (default `mesh`).

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

Configured entries cannot be removed at runtime; change the file and restart the agent. Validation rules and all keys are on the [Hosts configuration](/configuration/hosts) page.

## Add names at runtime

Use `muti-metroo host` against the agent whose registry you want to change:

```bash
muti-metroo host add db.mesh --ipv4 10.8.0.20 --comment "primary database"
muti-metroo host list
```

```text
Mesh Hosts (1)
NAME                             IPV4               IPV6                   COMMENT
db.mesh                          10.8.0.20                                 primary database
```

Remove an entry with `muti-metroo host remove db.mesh`. To use names outside the `mesh` suffix, add the suffix first with `muti-metroo host suffix add <suffix>`.

Runtime entries are written to the [dynamic config journal](/configuration/dynamic-config-persistence) and restored when the agent restarts, provided `agent.data_dir` is writable. The `host` commands act on the agent named by `-a` only; they have no `-t` option for remote agents. Like other runtime changes, they require that agent to hold `management.signing_private_key` (see [Runtime Configuration](/features/runtime-configuration#before-you-start)). See [host CLI](/cli/host) for every flag.

## Connect by name through SOCKS

Use a proxy mode that sends the hostname to the agent (`socks5h://` in curl, SOCKS4a, or "Proxy DNS" in browsers). With plain `socks5://`, the client resolves the name itself and the registry is never consulted.

```bash
curl -x socks5h://localhost:1080 http://db.mesh:8080/health
ssh -o ProxyCommand='nc -x localhost:1080 %h %p' admin@gateway.mesh
```

To confirm how the agent resolves and routes a name, trace it:

```bash
muti-metroo route trace db.mesh
```

```text
Trace for db.mesh
  Domain route: no match
  Resolved: 10.8.0.20

  10.8.0.20:
    -> 10.8.0.0/16          metric=1 hops=1 next_hop=exit-b (1f743225) origin=exit-b (1f743225)
```

:::note[Domain routes take precedence]
If an exit advertises a [domain route](/features/exit-routing#route-types) that matches the name (for example `*.mesh`), the ingress sends the name to that exit for resolution and the local registry is not used.
:::

## Use names with Mutiauk

Mutiauk reads the registry from the agent it is configured to use and answers DNS queries for the registered suffixes, so tools that do not support SOCKS can also use the names. Setup on the Mutiauk side is described in [Mutiauk: Mesh Hostnames](/mutiauk#mesh-hostnames).

## See Also

- [Hosts Configuration](/configuration/hosts): `mesh_hosts` and `mesh_hosts_suffixes` keys and validation rules
- [host CLI](/cli/host): `host add`, `remove`, `list`, and `suffix`
- [Hosts Management API](/api/hosts-management): HTTP endpoints behind the CLI
- [SOCKS Proxy](/features/socks5-proxy): how the ingress resolves and routes hostnames
- [Mutiauk](/mutiauk#mesh-hostnames): resolve mesh names for any local program
