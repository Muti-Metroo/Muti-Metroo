---
title: Exit
description: Configuration reference for the exit section, covering CIDR routes with metric, comment, source IP, and interface, domain routes, and the exit DNS resolver.
sidebar_position: 8
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-inspecting.png" alt="Mole configuring exit" style={{maxWidth: '180px'}} />
</div>

# Exit

The `exit` section makes the agent an exit: it advertises the networks and domains it can reach, and opens the real TCP connections for streams that arrive for them. The advertised routes also act as the exit's allow list.

## Configuration

```yaml
exit:
  enabled: true
  routes:
    - "10.0.0.0/8"                  # Bare CIDR
    - cidr: "192.168.0.0/16"        # Structured form
      metric: 5
      comment: "office LAN"
      source_ip: "192.168.1.50"
      interface: "ens192"
  domain_routes:
    - "api.internal.example.com"    # Exact name
    - "*.example.com"               # One label below example.com
  dns:
    servers:
      - "10.0.0.1:53"
    timeout: 5s
```

## Keys

| Key                   | Type     | Default | Description                                                                                                                                 |
| --------------------- | -------- | ------- | ------------------------------------------------------------------------------------------------------------------------------------------- |
| `enabled`             | bool     | `false` | Run the exit handler that opens connections for `routes` and `domain_routes`. Set it whenever either list is non-empty.                    |
| `routes`              | list     | `[]`    | CIDR routes. Each entry is a CIDR string or a mapping with the keys below.                                                                 |
| `routes[].cidr`       | string   | required | IPv4 or IPv6 network in CIDR notation.                                                                                                    |
| `routes[].metric`     | int      | `0`     | Base metric (0 to 65535). Each hop adds 1; lower wins among equally specific routes.                                                       |
| `routes[].comment`    | string   | `""`    | Operator note. Advertised with the route and shown in `route list`, `route trace`, and the dashboard. Does not affect route selection.    |
| `routes[].source_ip`  | string   | `""`    | Local address to bind outbound connections to. Must be the same address family as `cidr`. Not advertised.                                  |
| `routes[].interface`  | string   | `""`    | Network interface for outbound connections. Not advertised. See [Egress interface](#egress-interface).                                    |
| `domain_routes`       | list     | `[]`    | Domain patterns this exit resolves and connects to. See [Domain routes](#domain-routes).                                                   |
| `dns.servers`         | list     | `[]`    | DNS servers (`host:port`) the exit uses for domain routes. Empty uses the system resolver.                                                 |
| `dns.timeout`         | duration | `5s`    | Timeout for one name lookup.                                                                                                               |

Outbound connections from the exit use a fixed 30-second connect timeout, close after [`connections.idle_threshold`](/configuration/connections) without traffic, and count toward [`limits.max_streams_total`](/configuration/limits).

## Routes

```yaml
exit:
  enabled: true
  routes:
    - "10.0.0.0/8"
    - "0.0.0.0/0"
    - "::/0"
```

Each route is advertised to the mesh. Ingress agents pick the route with the longest matching prefix, and among equally specific routes the lowest metric, which is the configured `metric` plus one per hop. See [Routing](/concepts/routing) for the full selection rules.

The exit only opens TCP connections to destinations inside its own routes. A route list without `0.0.0.0/0` or `::/0` therefore also restricts what clients can reach through this exit. The UDP and ICMP relays are configured separately in [`udp`](/configuration/udp) and [`icmp`](/configuration/icmp).

Both entry forms can be mixed in one list. The bare form is shorthand for a mapping with only `cidr`.

### Route comments

```yaml
exit:
  enabled: true
  routes:
    - cidr: "192.168.0.0/16"
      comment: "office LAN"
```

Comments travel with the route advertisement, so every agent shows the same text for a prefix. Comments longer than 255 bytes are truncated in advertisements. [`muti-metroo route comment`](/cli/route) changes the comment of a configured route at runtime; the change persists across restarts when `agent.data_dir` is set and overrides this file.

### Source IP binding

```yaml
exit:
  enabled: true
  routes:
    - cidr: "10.10.0.0/16"
      source_ip: "192.168.1.50"
    - cidr: "10.20.0.0/16"
      source_ip: "192.168.1.60"
```

On a host with several addresses, `source_ip` sets the local address for outbound connections to that route, like `curl --interface`. The address must be assigned to the exit host when connections are made; otherwise the connection fails. When routes overlap, the most specific matching route's `source_ip` applies. Routes without it use the operating system's normal source selection.

A `source_ip` whose address family differs from `cidr` fails validation.

### Egress interface

```yaml
exit:
  enabled: true
  routes:
    - cidr: "10.10.0.0/16"
      interface: "ens192"
```

`interface` selects the network interface for outbound connections to that route:

- Without `source_ip`, or with a `source_ip` assigned to that interface, the exit binds to an address of the interface. This works on Linux, macOS, and Windows without extra privileges.
- With a `source_ip` that is not assigned to the interface (for example an address on loopback that must leave through `ens192`), the exit binds to the device itself. This requires Linux and root. On other platforms, or on Linux without root, such a route fails validation.

`muti-metroo route source-ip` and `muti-metroo route interface` change both settings at runtime; see [route CLI](/cli/route).

## Domain routes

```yaml
exit:
  enabled: true
  domain_routes:
    - "api.internal.example.com"
    - "*.prod.example.com"
```

A domain route sends matching host names to this exit unresolved; the exit resolves them with its own DNS settings. Use domain routes for names that only resolve inside the exit's network. Host names that match no domain route are resolved by the ingress agent and routed by CIDR.

| Pattern            | Matches                               | Does not match                          |
| ------------------ | ------------------------------------- | --------------------------------------- |
| `api.example.com`  | `api.example.com`                     | `v2.api.example.com`                    |
| `*.example.com`    | `www.example.com`, `api.example.com`  | `example.com`, `a.b.example.com`        |

A wildcard matches exactly one label. To cover a name and its subdomains, list each level. Patterns may contain letters, digits, hyphens, and dots, must contain at least one dot after any `*.` prefix, and cannot start or end with a dot. Matching is case-insensitive. When several exits advertise the same pattern, the lowest metric wins.

## DNS

```yaml
exit:
  dns:
    servers:
      - "10.0.0.1:53"
      - "[fd00::1]:53"
    timeout: 5s
```

`dns` applies to names the exit resolves for domain routes. With an empty `servers` list, the exit uses the host's system resolver, which includes `/etc/hosts` and local names such as `.local`. With servers configured, queries go over UDP to the first server that accepts a connection. Each server needs an explicit port.

When a name has both IPv4 and IPv6 addresses, the exit connects to the IPv4 address. Resolved addresses are cached for 5 minutes.

## Runtime management

CIDR routes can be added and removed on a running agent with [`muti-metroo route`](/cli/route) or the `/routes/manage` HTTP endpoint, and persist across restarts when `agent.data_dir` is set. A route listed in this file cannot be removed at runtime, and adding a runtime route with the same CIDR is rejected. Domain routes are only configured here.

## Examples

### Internet exit

```yaml
exit:
  enabled: true
  routes:
    - "0.0.0.0/0"
    - "::/0"
```

### Internal networks with internal DNS

```yaml
exit:
  enabled: true
  routes:
    - "10.0.0.0/8"
    - "192.168.0.0/16"
  domain_routes:
    - "*.corp.example.com"
  dns:
    servers:
      - "10.0.0.1:53"
```

### Two exits with overlapping routes

```yaml
# Exit A
exit:
  enabled: true
  routes:
    - "10.0.0.0/8"
```

```yaml
# Exit B
exit:
  enabled: true
  routes:
    - "0.0.0.0/0"
```

Traffic to `10.1.2.3` uses exit A (longer prefix); everything else uses exit B.

## See Also

- [Exit Routing usage](/features/exit-routing) for end-to-end examples and troubleshooting
- [Routing concepts](/concepts/routing) for route selection and propagation
- [route CLI](/cli/route) to add, remove, comment, and bind routes at runtime
- [Access Control](/security/access-control) for restricting what an exit can reach
- [Routing](/configuration/routing) for advertisement interval and route expiry
