---
title: Exit
sidebar_position: 6
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-inspecting.png" alt="Mole configuring exit" style={{maxWidth: '180px'}} />
</div>

# Exit Configuration

Control which networks this agent can reach. Exit nodes open real connections to destinations - define CIDR routes for IP-based access or domain routes for name-based routing.

**Quick setup:**

```yaml
exit:
  enabled: true
  routes:
    - "10.0.0.0/8" # Internal network access
    - "0.0.0.0/0" # Or allow all destinations
  dns:
    servers:
      - "8.8.8.8:53"
```

## Configuration

```yaml
exit:
  enabled: true
  routes:
    - "10.0.0.0/8"
    - "192.168.0.0/16"
    - "0.0.0.0/0"
  domain_routes:
    - "api.internal.corp"
    - "*.example.com"
  dns:
    servers:
      - "8.8.8.8:53"
      - "1.1.1.1:53"
    timeout: 5s
```

## Options

| Option          | Type     | Default | Description                                                                            |
| --------------- | -------- | ------- | -------------------------------------------------------------------------------------- |
| `enabled`       | bool     | false   | Enable exit node                                                                       |
| `routes`        | array    | []      | CIDR routes to advertise (bare string or `{cidr, metric, comment, source_ip}` mapping) |
| `domain_routes` | array    | []      | Domain patterns to advertise                                                           |
| `dns.servers`   | array    | []      | DNS servers for resolution                                                             |
| `dns.timeout`   | duration | 5s      | DNS query timeout                                                                      |

## Routes

Routes define which destinations this exit node can reach:

```yaml
exit:
  routes:
    - "10.0.0.0/8" # Private class A
    - "172.16.0.0/12" # Private class B
    - "192.168.0.0/16" # Private class C
    - "0.0.0.0/0" # Default route (all IPv4 traffic)
```

### Route Types

| Route            | Description                  |
| ---------------- | ---------------------------- |
| `10.0.0.0/8`     | Internal network             |
| `192.168.1.0/24` | Specific subnet              |
| `1.2.3.4/32`     | Single host                  |
| `0.0.0.0/0`      | Default route (all IPv4)     |
| `2001:db8::/32`  | IPv6 documentation prefix    |
| `fd00::/8`       | IPv6 unique local addresses  |
| `::1/128`        | IPv6 single host (localhost) |
| `::/0`           | Default route (all IPv6)     |

### IPv6 Routes

Muti Metroo fully supports IPv6 routes. Use standard CIDR notation:

```yaml
exit:
  routes:
    # IPv4 routes
    - "10.0.0.0/8"
    - "0.0.0.0/0"
    # IPv6 routes
    - "2001:db8::/32" # Specific IPv6 prefix
    - "fd00::/8" # Unique local addresses
    - "::/0" # Default route (all IPv6)
```

For dual-stack environments, include both IPv4 and IPv6 default routes:

```yaml
exit:
  routes:
    - "0.0.0.0/0" # All IPv4 traffic
    - "::/0" # All IPv6 traffic
```

### Route Selection

When multiple exit nodes advertise overlapping routes:

1. **Longest prefix wins**: `/32` beats `/24` beats `/0`
2. **Lower metric wins**: Closer exit preferred

Example:

- Exit A advertises `10.0.0.0/8` (metric 1)
- Exit B advertises `10.1.0.0/16` (metric 2)
- Traffic to `10.1.2.3` goes to Exit B (longer prefix)
- Traffic to `10.2.3.4` goes to Exit A

### Route Comments

Each route can carry an optional operator comment to record what the
subnet is for. Both the legacy bare-string form and the structured
mapping form are accepted in the same list:

```yaml
exit:
  routes:
    - "10.0.0.0/8" # legacy form, no comment
    - cidr: "192.168.0.0/16"
      comment: "office LAN"
    - cidr: "0.0.0.0/0"
      metric: 5
      comment: "default route via this exit"
```

Comments are operator metadata: they show up in `muti-metroo route list`,
the dashboard, and `muti-metroo route trace`, and are **propagated to peers**
alongside the route advertisement so every agent's dashboard sees the
same comment for a given prefix. They do not affect route selection,
metric calculation, or LPM behaviour. Comments are capped at 255 bytes
on the wire.

The same comment field is also accepted on `POST /routes/manage` when
adding a dynamic route, and the dedicated `set_comment` action edits
the comment on either a config-seeded or dynamic route at runtime; see
the [`route comment`](../cli/route.md#route-comment) CLI reference.
`set_comment` triggers an immediate route re-advertisement so peers
pick up the change without waiting for the periodic refresh interval.
Runtime comment changes are journalled to `data_dir/dynamic-config.log`
and survive agent restart, overriding the value from the config file.

### Source IP Binding

On a multi-homed exit, a route can pin its outbound dials to a specific
local IP (equivalent to `curl --interface`). This is useful when
upstream firewalls or quotas key off the source address:

```yaml
exit:
  routes:
    - cidr: "10.10.0.0/16"
      source_ip: "192.168.1.50" # bind dials for this CIDR to .50
    - cidr: "10.20.0.0/16"
      source_ip: "192.168.1.60" # ...and these to .60
```

The address must already be bound on the exit host (an alias on an
existing interface counts) and must match the CIDR's address family
(v4 source for v4 CIDR, v6 for v6). On address-family mismatch the
config fails to load. If the address is not locally bound at dial time
the OS rejects the dial with `EADDRNOTAVAIL` and the client sees a
normal connection-refused error.

When several routes overlap (longest-prefix match), the source from the
most specific matching route is applied. Routes without `source_ip`
keep the default OS source-address selection.

`source_ip` is a **local-only** field: unlike `comment` it is not
propagated to other agents in route advertisements, because it is only
meaningful on the exit host that owns the address. The same field is
accepted on `POST /routes/manage` and as `--source-ip` on
`muti-metroo route add` for dynamic routes; runtime values are journalled to
`data_dir/dynamic-config.log` and restored on restart.

To change the binding on an existing route (config-seeded or dynamic) without
removing and re-adding, use the dedicated `set_source_ip` action: the CLI
wrapper is `muti-metroo route source-ip <cidr> <ip>` (pass an empty string to
clear). Like `set_comment`, the override is journalled and survives restart.

### Egress Interface

A route can also pin the egress network interface for its outbound dials.
Set `interface:` to the NIC name:

```yaml
exit:
  routes:
    - cidr: "10.10.0.0/16"
      interface: "ens192" # dial this CIDR out ens192
```

How the binding is applied depends on the source IP and the platform:

- **No `source_ip`, or a `source_ip` already assigned to the interface** --
  the exit binds the dial to an address on that interface
  (`net.Dialer.LocalAddr`). This is portable and unprivileged: it works on
  Linux, macOS, and Windows without root. The source _address_ is set; the
  egress device then follows the host routing table, which is what you want on
  a single-homed or normally-routed host.
- **A `source_ip` that is _not_ assigned to the interface** -- e.g. a
  floating/anycast address that lives on loopback while packets must leave via
  `ens192`. This requires
  `SO_BINDTODEVICE` + `IP_FREEBIND` and is therefore **Linux + root only**:

  ```yaml
  exit:
    routes:
      - cidr: "10.10.0.0/16"
        source_ip: "192.0.2.10" # loopback-bound floating address
        interface: "ens192" # force egress on ens192
  ```

  On macOS/Windows, or on Linux as a non-root user, an off-interface
  `source_ip` paired with `interface:` is rejected with a clear error. The same
  gate applies to `POST /routes/manage` (`action: "add"` or `"set_interface"`)
  and to `muti-metroo route add --interface` / `muti-metroo route interface <cidr> <name>`.
  On Linux + root the interface is always honored via `SO_BINDTODEVICE`,
  whether or not the source is on it.

Like `source_ip`, `interface` is **local-only** -- never advertised to other
agents.

## Domain Routes

Domain routes allow routing based on domain names instead of IP addresses. When a SOCKS5 client requests a connection to a domain matching a domain route, the domain is passed to the exit node for DNS resolution instead of being resolved at the ingress.

```yaml
exit:
  domain_routes:
    - "api.internal.corp" # Exact match
    - "*.example.com" # Wildcard match
    - "*.prod.service.local" # Wildcard for specific subdomain
```

### Domain Pattern Types

| Pattern             | Matches                              | Does NOT Match                           |
| ------------------- | ------------------------------------ | ---------------------------------------- |
| `api.example.com`   | `api.example.com`                    | `foo.api.example.com`                    |
| `*.example.com`     | `foo.example.com`, `bar.example.com` | `example.com`, `a.b.example.com`         |
| `*.api.example.com` | `foo.api.example.com`                | `api.example.com`, `a.b.api.example.com` |

### Wildcard Matching

Wildcards use **single-level matching** only:

- `*.example.com` matches `foo.example.com` and `bar.example.com`
- `*.example.com` does **NOT** match `a.b.example.com` (multi-level subdomain)
- `*.example.com` does **NOT** match `example.com` (base domain)

### Domain vs CIDR Priority

When both domain and CIDR routes exist:

1. **Domain routes are checked first** for domain-based requests
2. If no domain route matches, DNS resolution happens at the ingress
3. Then **CIDR routes** are used based on the resolved IP

### Multiple Origins

Multiple exit agents can advertise the same domain pattern. The route with the **lowest metric** (fewest hops) is selected.

### Use Cases

Domain routes are ideal for:

- **Split-horizon DNS**: Internal domains resolved by internal DNS servers
- **Private services**: Route `*.internal.corp` to an internal exit
- **Geo-specific resolution**: Different DNS results based on exit location

## DNS Configuration

:::info DNS Resolution Behavior
DNS resolution location depends on the route type:

- **CIDR routes**: Domain names are resolved at the **ingress agent** using the system's DNS resolver. The exit node receives IP addresses.
- **Domain routes**: Domain names are passed to the **exit node** for resolution. By default, the exit node uses the system resolver (which supports local domains like `.local`). You can optionally configure explicit DNS servers.
  :::

### Public DNS

```yaml
exit:
  dns:
    servers:
      - "8.8.8.8:53" # Google DNS (IPv4)
      - "1.1.1.1:53" # Cloudflare DNS (IPv4)
    timeout: 5s
```

### IPv6 DNS Servers

IPv6 DNS servers are supported using bracket notation:

```yaml
exit:
  dns:
    servers:
      - "[2001:4860:4860::8888]:53" # Google DNS (IPv6)
      - "[2606:4700:4700::1111]:53" # Cloudflare DNS (IPv6)
    timeout: 5s
```

For dual-stack DNS resolution, include both IPv4 and IPv6 servers:

```yaml
exit:
  dns:
    servers:
      - "8.8.8.8:53" # Google DNS (IPv4)
      - "[2001:4860:4860::8888]:53" # Google DNS (IPv6)
    timeout: 5s
```

:::note DNS Resolution Preference
When a domain resolves to both A (IPv4) and AAAA (IPv6) records, Muti Metroo prefers IPv4 addresses. If only AAAA records exist, IPv6 addresses are used.
:::

### Private DNS

For internal domains:

```yaml
exit:
  dns:
    servers:
      - "10.0.0.1:53" # Internal DNS server (IPv4)
    timeout: 5s
```

Or with IPv6:

```yaml
exit:
  dns:
    servers:
      - "[fd00::1]:53" # Internal DNS server (IPv6)
    timeout: 5s
```

### DNS-over-TLS (DoT)

Not currently supported. Use standard DNS.

### Default (System Resolver)

The `dns` section is always optional. When omitted, the exit node uses the system resolver, which:

- Resolves local domains (e.g., `printer.local`, `server.internal`)
- Uses `/etc/hosts` entries
- Respects system DNS configuration

```yaml
exit:
  enabled: true
  routes:
    - "10.0.0.0/8"
  domain_routes:
    - "*.internal" # Resolved using system DNS
  # dns section omitted - uses system resolver
```

Configure explicit DNS servers only when you need to override system DNS (e.g., for public DNS or specific resolvers).

## Access Control

Routes also serve as access control:

```yaml
exit:
  routes:
    - "10.0.0.0/8" # Only allow internal network
    # No 0.0.0.0/0 = no internet access
```

Connections to non-matching destinations are rejected.

## Examples

### Internet Gateway (IPv4)

Allow all IPv4 traffic:

```yaml
exit:
  enabled: true
  routes:
    - "0.0.0.0/0" # All IPv4 traffic
  dns:
    servers:
      - "8.8.8.8:53"
      - "1.1.1.1:53"
    timeout: 5s
```

### Internet Gateway (Dual-Stack)

Allow both IPv4 and IPv6 traffic:

```yaml
exit:
  enabled: true
  routes:
    - "0.0.0.0/0" # All IPv4 traffic
    - "::/0" # All IPv6 traffic
  dns:
    servers:
      - "8.8.8.8:53" # Google DNS (IPv4)
      - "[2001:4860:4860::8888]:53" # Google DNS (IPv6)
    timeout: 5s
```

### Private Network Only

Internal resources only:

```yaml
exit:
  enabled: true
  routes:
    - "10.0.0.0/8"
    - "192.168.0.0/16"
  dns:
    servers:
      - "10.0.0.1:53" # Internal DNS
    timeout: 5s
```

### Specific Service Access

Only database and API servers:

```yaml
exit:
  enabled: true
  routes:
    - "10.0.1.10/32" # Database server
    - "10.0.1.20/32" # API server
  dns:
    servers:
      - "10.0.0.1:53"
    timeout: 5s
```

### Split Horizon

Different exits for different networks:

**Exit A (internal network):**

```yaml
exit:
  enabled: true
  routes:
    - "10.0.0.0/8"
  dns:
    servers:
      - "10.0.0.1:53"
```

**Exit B (internet):**

```yaml
exit:
  enabled: true
  routes:
    - "0.0.0.0/0"
  dns:
    servers:
      - "8.8.8.8:53"
```

Traffic is routed:

- `10.x.x.x` → Exit A (longer prefix)
- Everything else → Exit B (default route)

### Domain-Based Routing

Route specific domains to an internal exit node:

```yaml
exit:
  enabled: true
  routes:
    - "10.0.0.0/8"
  domain_routes:
    - "api.internal.corp" # Exact match for API server
    - "*.internal.corp" # All *.internal.corp subdomains
    - "*.prod.mycompany.local" # Production services
  dns:
    servers:
      - "10.0.0.1:53" # Internal DNS server
    timeout: 5s
```

Traffic is routed:

- `api.internal.corp` → This exit (exact domain match)
- `foo.internal.corp` → This exit (wildcard match)
- `bar.prod.mycompany.local` → This exit (wildcard match)
- Domain not matching any pattern → DNS resolved at ingress, then CIDR routing

## Route Advertisement

Routes are advertised to the mesh:

- **Automatically**: Every `routing.advertise_interval` (default 2m)
- **Manually**: Via HTTP API

### Trigger Immediate Advertisement

```bash
curl -X POST http://localhost:8080/routes/advertise
```

Use after:

- Configuration changes
- Network changes
- Agent restart

### Routing Configuration

```yaml
routing:
  advertise_interval: 2m # How often to re-advertise
  route_ttl: 5m # Route expiration time
  max_hops: 16 # Maximum path length
```

## Troubleshooting

### No Route Found

```
Error: no route to 1.2.3.4
```

- Check exit is enabled
- Verify routes include destination
- Check exit agent is connected to mesh

### DNS Resolution Failed

```
Error: DNS lookup failed for example.com
```

DNS resolution location depends on route type:

**For CIDR routes** (domain resolved at ingress):

- Check DNS configuration on the ingress agent's host system
- Verify the ingress host can resolve domains: `dig example.com`
- Check `/etc/resolv.conf` on the ingress host

**For domain routes** (domain resolved at exit):

- Check `exit.dns.servers` configuration on the exit agent
- Verify the exit host can reach the DNS servers
- Test DNS resolution from the exit host: `dig @10.0.0.1 example.com`

### Connection Refused

```
Error: connection refused to 10.0.0.5:22
```

- Verify destination is reachable from exit agent
- Check firewall rules on exit host
- Test directly: `nc -zv 10.0.0.5 22`

### Access Denied

```
Error: destination not in allowed routes
```

- Add appropriate route to `exit.routes`
- Use more permissive CIDR (e.g., `/8` instead of `/24`)

## Security Considerations

1. **Principle of least privilege**: Only advertise necessary routes
2. **Avoid `0.0.0.0/0`** unless you need full internet access
3. **Use internal DNS** for private networks
4. **Consider network segmentation**: Different exits for different trust levels

## Related

- [Features: Exit Routing](/features/exit-routing) - Detailed usage
- [Concepts: Routing](/concepts/routing) - How routing works
- [Security: Access Control](/security/access-control) - Security best practices
