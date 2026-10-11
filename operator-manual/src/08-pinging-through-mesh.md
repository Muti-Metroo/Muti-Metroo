# Use Case: Pinging Through the Mesh

`ping` is the first thing every operator reaches for when they want to know "is the box up?". With Mutiauk installed and a route active, plain `ping` works out of the box -- there is no special command, no `mutiauk ping` subcommand on the workstation. This chapter explains _why_ it works and what to check when it does not.

## The Quick Answer

```bash
ping -c 3 10.20.120.11
PING 10.20.120.11 (10.20.120.11) 56(84) bytes of data.
64 bytes from 10.20.120.11: icmp_seq=1 ttl=63 time=42.1 ms
64 bytes from 10.20.120.11: icmp_seq=2 ttl=63 time=39.8 ms
64 bytes from 10.20.120.11: icmp_seq=3 ttl=63 time=40.2 ms

--- 10.20.120.11 ping statistics ---
3 packets transmitted, 3 received, 0% packet loss, time 2003ms
rtt min/avg/max/mdev = 39.835/40.708/42.123/0.989 ms
```

If the destination is in a CIDR that Mutiauk routes, the `tun0` interface captures the ICMP echo request, the daemon's ICMP forwarder pipes it through SOCKS5 to the shared agent, the agent passes it along the mesh to the exit, the exit sends a real ICMP echo and returns the reply. RTT measurements include every hop -- a useful health signal for the whole path.

## What `ping` Actually Proves

A successful ping tells you:

- The route exists on the workstation and the daemon is forwarding ICMP.
- The shared agent's SOCKS5 listener is reachable.
- The mesh has a path to the exit agent.
- The exit agent has ICMP enabled and can reach the destination.
- The destination is up and responding.

A failed ping does not pin the failure to any one of those, but Chapter 11 shows how to bisect.

## When It Does Not Work

### "Destination Host Unreachable" right away

The workstation has no matching route. `mutiauk route list` should show the CIDR. If not, see Chapter 6 -- the route is missing from the dashboard or autoroutes is broken.

### Silent timeout (no replies)

Order of suspicion:

1. **Is `mutiauk status` healthy?** It runs the same probes as a manual check. If the SOCKS5 connectivity row or the TCP routing test fails, fix that first.
2. **Is the exit agent's ICMP enabled?** Open the agent panel for the exit in Muti Metroo Manager, look at the **Ping** sub-tab. If it loads and you can ping from there but not from the workstation, the workstation -> shared agent -> exit chain is fine and ICMP is enabled; the problem is your workstation's TUN. If the Ping tab fails too, the exit is the issue.
3. **`net.ipv4.ping_group_range`.** Linux exit agents need unprivileged ICMP enabled, which sometimes is not. On the exit:
   ```bash
   sudo sysctl -w net.ipv4.ping_group_range="0 65535"
   ```
   And persist it in `/etc/sysctl.d/`.
4. **Destination just does not answer ICMP.** Plenty of hosts and firewalls drop echo. Try a TCP probe (`nc -zv 10.20.120.11 22`) -- if that works, the host is alive but not pinging.

### `ping` works but raw-socket tools do not

Expected. Tools that build raw packets never traverse the TUN. Use TCP-connect-based tools (`ssh`, `curl`, `nc -zv`) instead. See Chapter 7 for the broader rule.

### `traceroute` shows nothing past the gateway

Also expected. Muti Metroo's ICMP relay forwards echo and echo-reply only. TTL-exceeded messages -- which traceroute relies on -- are not propagated back through the mesh. Use `tcptraceroute` against an open port, or rely on `mutiauk route trace` (Chapter 11) to see the mesh-side path.

## Pinging from the Other Direction

If you want to ping something **from a remote agent's perspective** rather than from your workstation, the right tool is Muti Metroo Manager's **Ping** sub-tab on the agent panel, or the agent CLI:

```bash
# From a workstation running the muti-metroo CLI (or via SSH on the shared host)
muti-metroo ping <agent-id> 10.20.120.11 -c 5
```

Either way, the ICMP echo is launched from the chosen agent, so you get latency from that vantage point. Useful for "is the destination only reachable from one site, or globally?".

## Why You Might See Slightly Different RTTs

The ICMP path goes through the SOCKS5 ingress, the mesh transports (QUIC by default), and the exit agent's unprivileged ICMP socket. Each adds a few milliseconds. If the same destination shows 5 ms latency to a peer agent and 45 ms latency from your workstation, the extra 40 ms is your workstation -> shared host -> peer hop, not "the mesh is slow". Use `muti-metroo mesh-test` from the dashboard to see the cleaner agent-to-agent latencies.
