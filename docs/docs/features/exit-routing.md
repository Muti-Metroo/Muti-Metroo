---
title: Exit Routing
description: Make networks and domains reachable through exit agents, check which exit handles a destination, and control how exits connect outward.
sidebar_position: 4
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-presenting.png" alt="Mole presenting exit routing" style={{maxWidth: '180px'}} />
</div>

# Exit Routing

An exit agent opens the real TCP connections for traffic that enters the mesh elsewhere. It advertises the destinations it serves as routes, and every other agent learns those routes and sends matching traffic toward it.

Typical setups:

- An exit inside an office network advertises `10.0.0.0/8` so remote operators reach internal hosts.
- An exit with access to internal DNS advertises `*.internal.example.com` so names resolve inside that network.
- An exit with internet access advertises `0.0.0.0/0` as a general-purpose gateway.

## Route types

| Type | Example | Matches | DNS resolved by |
| ---- | ------- | ------- | --------------- |
| CIDR route | `10.0.0.0/8` | Destination IP addresses | Ingress agent |
| Domain route | `*.internal.example.com` | Destination hostnames sent by the client | Exit agent |

Domain routes only apply when the client sends a hostname, for example curl with `socks5h://`. [Mutiauk](/mutiauk) works at the IP layer and only uses CIDR routes.

## Configure an exit

Enable the exit role and list its routes in the agent configuration:

```yaml
exit:
  enabled: true
  routes:
    - "10.0.0.0/8"
    - "192.168.0.0/16"
  domain_routes:
    - "*.internal.example.com"
  dns:
    servers:
      - "10.0.0.53:53"
```

The exit advertises its routes when it starts, and again every `routing.advertise_interval` (default 2m). Every key, including per-route metric, comment, source IP, and egress interface, is described in [Exit Configuration](/configuration/exit).

## Add routes at runtime

CIDR routes can be added to a running agent without editing its configuration. This also turns a transit-only agent into an exit for that range:

```bash
# On the local agent
muti-metroo route add 172.16.0.0/12 --comment "lab network"

# On a remote agent, through the local agent
muti-metroo route add 172.16.0.0/12 --target 1f743225d524f8acd3b42b55639702f0

# List routes the agent originates, including config-seeded ones
muti-metroo route list --all

# Withdraw the route
muti-metroo route remove 172.16.0.0/12
```

New and removed routes are advertised immediately. Runtime routes are stored in the [dynamic config journal](/configuration/dynamic-config-persistence) and restored on restart when `agent.data_dir` is writable. Adding `0.0.0.0/0` or `::/0` asks for a typed confirmation unless `--allow-default-route` is given. Domain routes can only be set in the configuration file.

See [route CLI](/cli/route) for all subcommands and [Runtime Configuration](/features/runtime-configuration) for the authorization these changes need.

## How a destination is matched

For a hostname from a SOCKS client, the ingress agent checks domain routes first:

- An exact pattern such as `api.example.com` matches only that name.
- A wildcard such as `*.example.com` matches exactly one extra label: `foo.example.com`, but not `a.b.example.com` or `example.com`.
- Matching is case-insensitive.

If a domain route matches, the hostname travels to the exit and the exit resolves it, with its `exit.dns` servers or the system resolver. Otherwise the ingress resolves the name itself (mesh hostname registry first, then system DNS) and continues with the IP.

For an IP address, the CIDR route with the longest matching prefix wins. When two routes have the same prefix, the one with the lower metric (fewer hops) wins. For example, `10.1.2.0/24` beats `10.0.0.0/8` for `10.1.2.3`.

If no route matches, the ingress agent dials the destination itself. See [SOCKS Proxy](/features/socks5-proxy#how-destinations-are-routed).

### Check a destination

`route trace` shows which route an agent would use, without sending traffic:

```bash
muti-metroo route trace 10.1.2.3
```

```text
Trace for 10.1.2.3
    -> 10.1.2.0/24          metric=1 hops=1 next_hop=exit-b (1f743225) origin=exit-b (1f743225)
       10.0.0.0/8           metric=2 hops=2 next_hop=transit (9a0b1c2d) origin=office (5e6f7a8b)  [shorter prefix]
```

The arrow marks the winning route. A hostname argument also shows the domain-route decision and how the name was resolved. Use `muti-metroo route status` to print the whole route table.

## Choose the outbound address of an exit

On an exit host with several addresses or network interfaces, each CIDR route can pin the local source IP or the interface used for connections to that range:

```bash
muti-metroo route source-ip 10.10.0.0/16 192.168.1.50
muti-metroo route interface 10.10.0.0/16 ens192
```

Pass an empty string to clear a binding. The same settings exist as `source_ip` and `interface` on a route in the configuration file. They stay on the exit and are not advertised. Binding to an interface with a source IP that is not assigned to that interface requires Linux and root. Details are in [Exit Configuration](/configuration/exit#source-ip-binding).

## Destinations the exit accepts

An exit only opens TCP connections to destinations covered by its own routes, from the configuration or added at runtime:

- A connection to an IP outside every CIDR route is refused with "destination not allowed".
- A hostname that matches one of the exit's domain routes is accepted, and its resolved IP is not checked against the CIDR routes.
- A hostname that matches no domain route is accepted only if its resolved IP falls inside a CIDR route.

This check covers TCP streams only. UDP datagrams and ICMP echo requests are not checked against exit routes; see [Access Control](/security/access-control#exit-routes).

## Troubleshooting

### Traffic does not reach the exit

1. Run `muti-metroo route trace <destination>` on the ingress agent.
2. If no route is marked, check that the exit is connected (`muti-metroo peer status`) and advertises the route (`muti-metroo route list --all -t <exit-agent-id>`).
3. To push routes immediately instead of waiting for the next advertisement, run `curl -X POST http://localhost:8080/routes/advertise` on the exit.

### Domain route does not match

- The client resolved the name itself. Use `socks5h://` instead of `socks5://`.
- The wildcard is one level deep. `*.example.com` does not match `a.b.example.com`; add `*.b.example.com`.
- Confirm the decision with `muti-metroo route trace <hostname>`.

### Connection times out or is refused

The route is correct but the exit host cannot reach the destination. Check the exit's network, firewall, and, for domain routes, its DNS servers. If source IP or interface binding is set for the route, confirm the address is still assigned on the exit host.

## See Also

- [Exit Configuration](/configuration/exit): routes, domain routes, DNS, source IP, and interface keys
- [route CLI](/cli/route): add, remove, comment, trace, and inspect routes
- [Routing](/concepts/routing): how routes propagate and expire
- [SOCKS Proxy](/features/socks5-proxy): send client traffic to exits
- [Access Control](/security/access-control): what exit routes do and do not restrict
