# Use Case: Routing Internal CIDRs

This is the most common Mutiauk workflow: "I need 10.20.0.0/16 to be reachable from my Linux workstation, transparently, without per-tool configuration."

The pattern is always the same:

1. The mesh has an agent that already exits for the CIDR (someone has set up the remote side).
2. You add the CIDR as a route in Muti Metroo Manager.
3. Within seconds, every Mutiauk instance picks it up via autoroutes (pushed over the SOCKS5 stream).
4. Your workstation just works -- `ssh`, `curl`, anything TCP / UDP / ICMP.

## Add a Route from Muti Metroo Manager

Open the **Routes** tab. On the **Active routes** sub-tab, click **Add route**:

| Field        | Value                                                                                                 |
| ------------ | ----------------------------------------------------------------------------------------------------- |
| Network      | `10.20.0.0/16`                                                                                        |
| Origin agent | The agent that should advertise this route (usually the one you have set up as exit for that network) |
| Comment      | `data-centre-A` (free text, propagates over the wire)                                                 |
| Metric       | leave default                                                                                         |

Save. The dashboard sends the request to the shared host's agent, which signs it with its configured signing private key and propagates the change. The route appears in the active table immediately. It is also persisted to the agent's data directory, so it survives a restart of the origin agent.

![Active routes table after adding a CIDR](./07-routes-table.png)

## Watch It Flow to Your Workstation

On the workstation:

```bash
# Routes arrive within seconds, pushed over the SOCKS5 stream.
mutiauk route list

DESTINATION       SOURCE   INTERFACE   COMMENT
10.20.0.0/16      auto     tun0        data-centre-A
```

`SOURCE: auto` means it came from the agent via autoroutes (as opposed to `config` from the YAML, or `manual` from `mutiauk route add`). If the route has not appeared yet, give it a few seconds, then check again. Chapter 11 has the debugging flow if it never shows up.

A faster confirmation -- straight from the daemon, no waiting:

```bash
sudo journalctl -u mutiauk -n 50 --no-pager | grep -i route
```

You should see a "applied 1 new route" line.

## Test It

Once the route is on the workstation, every tool sees it transparently. Pick any IP in the CIDR that has a service on it:

```bash
# TCP
curl --max-time 5 http://10.20.120.11/
ssh op@10.20.120.11

# ICMP
ping -c 3 10.20.120.11
```

No proxy environment variables, no per-tool configuration. The TUN interface captures everything bound for the CIDR and tunnels it through the shared agent.

## Removing a Route

When the project ends, remove the route from Muti Metroo Manager. Open **Routes**, click the trash icon, confirm. Within seconds the workstation drops the route and traffic to that CIDR goes back through the regular interface.

If you want to remove it locally just on your machine without affecting anyone else (for example, you are about to scan that CIDR directly from the same network), `mutiauk route remove 10.20.0.0/16` removes it from the kernel. The next autoroutes push will re-add it within seconds unless the dashboard route also goes away. `--persist` only deletes the matching entry from `/etc/mutiauk/config.yaml` -- there is no "deny" or "exclude" mechanism, so for an autoroutes-sourced route (`SOURCE: auto` in `route list`) the only way to stop it locally is to remove it from the dashboard or pause autoroutes. `--persist` is the right choice when the entry was originally `SOURCE: config`.

## Persistent Local Routes

Sometimes you want a route on the workstation that the shared agent does **not** advertise -- a CIDR only this operator should reach, or a temporary override. Two ways:

```bash
# Add a kernel-only route (lost on daemon restart)
sudo mutiauk route add 192.168.99.0/24 --comment "lab subnet"

# Add and write to /etc/mutiauk/config.yaml under routes:
sudo mutiauk route add 192.168.99.0/24 --persist --comment "lab subnet"
```

Persisted routes show up as `SOURCE: config` in `route list`. They are independent of the dashboard -- changing the agent's routes will not affect them.

## Reaching Specific Hosts in Overlapping CIDRs

A common situation in larger deployments: two different sites both use `192.168.0.0/24` (or `10.0.0.0/8`, or some other RFC 1918 favourite). They are isolated -- they cannot collide on the wire -- but they cannot both be advertised to your workstation as the same `/24`, because the workstation cannot route the same CIDR two ways at once.

The pragmatic workaround: do not route the whole CIDR. Route the specific hosts you actually care about, as `/32` entries.

Pick the IPs you need from one of the overlapping sites and add them in Muti Metroo Manager -> **Routes** -> **Add route**:

| Network            | Origin agent  | Comment            |
| ------------------ | ------------- | ------------------ |
| `192.168.50.10/32` | `exit-site-A` | `dc-1 jump host`   |
| `192.168.50.21/32` | `exit-site-A` | `dc-1 file server` |
| `192.168.50.7/32`  | `exit-site-B` | `dc-2 admin host`  |

Each `/32` is a single host route. Different hosts that happen to share the same `/24` can be reached via different exit agents, because longest-prefix-match picks the most specific route. The other addresses in the overlapping range stay on the workstation's local interface (or on whatever else owns the route).

`mutiauk route trace 192.168.50.10` will confirm which exit will handle that specific host. Worth running for each `/32` after you add it -- it is the cheapest way to spot a typo.

When the project is over, remove the `/32` entries individually, or use a single comment label on every route you added (`project-Q2-26`) so you can find and delete them quickly.

## Picking Which Local IP the Exit Dials From

On a multi-homed exit (two NICs, a VLAN trunk, alias addresses), the OS picks the source IP for outbound dials using its system routing table. Sometimes that is not what you want -- the upstream firewall keys on the source, or one of the local IPs has a quota, or you want auditable separation.

Per-route source binding pins the exit's outbound dial to a specific local IP -- the same effect as `curl --interface 192.168.1.50 https://target`. Add it on the route itself:

```bash
# On the exit agent (or via -t from anywhere with the management key)
muti-metroo route add 10.10.0.0/16 --source-ip 192.168.1.50
muti-metroo route add 10.20.0.0/16 --source-ip 192.168.1.60
```

Or in YAML:

```yaml
exit:
  routes:
    - cidr: "10.10.0.0/16"
      source_ip: "192.168.1.50"
```

The source IP must already be bound on the exit host (an alias on an existing interface is fine) and must be the same address family as the CIDR. It is **local-only**: the binding is never advertised to other agents, because it is only meaningful on the host that owns the address. If the IP is unbound when the dial runs, the OS rejects with `EADDRNOTAVAIL` and the SOCKS client sees a normal connection-refused error -- not a silent fallback to the default source.

You can also pin the egress network interface for a route's outbound dials. Set it on the route, either at add time or on a running agent:

```bash
muti-metroo route add 10.10.0.0/16 --interface ens192
muti-metroo route interface 10.10.0.0/16 ens192          # set/clear on an existing route
```

There are two tiers. When `source_ip` is unset, or is already an address on that interface, the bind is portable and unprivileged -- it works on Linux, macOS, and Windows. When `source_ip` is _not_ on the interface (for example a loopback-bound floating address you want forced out a specific NIC), the kernel needs `SO_BINDTODEVICE`, which is **Linux + root only**; on macOS, Windows, or Linux as a non-root user that combination is rejected with a clear error. Like `source_ip`, the interface binding is **local-only** and never advertised to other agents.

## When You Want Just One Tool, Not the Whole Machine

If transparent routing is more than you want -- for example, you want one browser tab to use the mesh while everything else stays direct -- skip the autoroute flow entirely and use the agent's SOCKS5 directly. That is the next chapter.

## Common Surprises

- **Routes do not flow if `autoroutes.enabled: false`.** The wizard turns it on by default, but if you wrote the config by hand, double-check.
- **Routes only flow if the SOCKS5 endpoint is reachable.** Autoroutes rides the same SOCKS5 connection Mutiauk uses for traffic -- there is no separate token. If routes never arrive, check `socks5.server` reachability and, if the agent requires it, the SOCKS5 username/password in `/etc/mutiauk/config.yaml`. `journalctl -u mutiauk` will show the connection error.
- **Routes that overlap with the workstation's local subnets are not applied.** Mutiauk filters out loopback, link-local, and routes that would conflict with the existing default route. This is by design -- you cannot accidentally cut yourself off from `eth0`.
- **The CIDR must actually be exported by some agent.** Adding `10.20.0.0/16` in the dashboard does nothing on its own; some agent has to be configured (statically or via the dashboard) as the exit for that CIDR. If `mutiauk route trace 10.20.120.11` reports "not in mesh", that is the missing piece.
