---
title: Connectivity Troubleshooting
description: Step-by-step diagnosis for Muti Metroo peers that do not connect, routes that do not appear, and connections that take the wrong path.
sidebar_position: 2
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-drilling.png" alt="Mole troubleshooting connectivity" style={{maxWidth: '180px'}} />
</div>

# Connectivity Troubleshooting

Work through this page from the top: first confirm the agent itself is healthy, then its peer connections, then the routes, then the path a connection takes. For the meaning of a specific error message, see [Common Issues](/troubleshooting/common-issues).

## Check the agent

On the agent you are diagnosing:

```bash
muti-metroo status
curl -s http://localhost:8080/healthz | jq
```

```json
{
  "accepts_signed_management": true,
  "can_sign_management": false,
  "exit_handler_running": false,
  "peer_count": 1,
  "route_count": 2,
  "running": true,
  "socks5_running": true,
  "status": "healthy",
  "stream_count": 0
}
```

`peer_count` is the number of connected peers. `route_count` is the number of routes in this agent's table. Both require the HTTP API (`http.enabled: true`); see [Health API](/api/health).

## Peers do not connect

List the peers and their state:

```bash
muti-metroo peer status
```

```text
Connected Peers
===============
ID           NAME                 STATE          ROLE       RTT
--           ----                 -----          ----       ---
bff2a511     exitB                CONNECTED      dialer     -

Total: 1 peer(s)
```

A configured peer that never appears here is failing to connect. Look for `failed to connect to peer` in the dialing agent's log; the `error` field gives the reason. The table in [Common Issues: Peers do not connect](/troubleshooting/common-issues#peers-do-not-connect) explains the usual values.

Then check, in order:

1. **The listener is running.** On the listening agent, the startup log contains `listener started` with the address and transport, and `muti-metroo listener list` shows it.
2. **The address and transport match.** A peer's `transport` must equal the listener's `transport`. For HTTP/2 and WebSocket, the path must match too: give it in the address (`wss://relay.example.com:443/mesh`). Without a path, the dialer uses `/mesh`.
3. **The network allows it.** QUIC uses UDP; HTTP/2 and WebSocket use TCP. A TCP test such as `nc -zv host 4433` says nothing about a QUIC listener.
4. **The handshake succeeds.** Run [`muti-metroo probe`](/cli/probe) from the dialing host. It dials the listener with the same transport code as the agent and reports the remote agent ID:

   ```bash
   muti-metroo probe relay.example.com:4433
   muti-metroo probe --transport ws relay.example.com:443 --path /mesh
   ```

   If `probe` succeeds but the agent still cannot connect, compare TLS settings: with `mtls: true` on the listener, the dialing agent needs a certificate signed by the listener's CA, and `probe` needs `--ca`, `--cert`, and `--key` to test the same way.

5. **The peer ID is right.** `probe` prints the listener's agent ID. It must equal the `id` in the dialing agent's `peers` entry, or be `"auto"`.

### Through an HTTP proxy

For WebSocket peers that use `proxy`, the proxy must allow `CONNECT` to the peer's host and port. Test the proxy separately:

```bash
curl -v --proxy http://proxy.corp.example.com:8080 https://relay.example.com/
```

A `407` response means `proxy_auth` is missing or wrong.

### Peers that disconnect repeatedly

Each disconnect is logged as `peer disconnected` with an `error` field. A common cause is a NAT device, firewall, or reverse proxy that drops idle connections sooner than the agents send keepalives. Agents send a keepalive after `connections.idle_threshold` (default 5 minutes) without traffic. Lower it below the device's idle timeout:

```yaml
connections:
  idle_threshold: 30s
```

## Routes are missing

On the ingress agent:

```bash
muti-metroo route status
```

```text
Route Table
===========
NETWORK              NEXT HOP        ORIGIN          METRIC   HOPS   COMMENT
-------              --------        ------          ------   ----   -------
10.99.0.0/16                         bff2a511        0        1

Total: 1 route(s)
```

If an expected route is missing:

1. **Check the exit.** On the exit agent, `exit.enabled` must be `true` and the CIDR must be in `exit.routes` or added with `muti-metroo route add`. The exit's startup output lists its exit routes.
2. **Check every hop.** Routes travel only over connected peers. Run `muti-metroo peer status` on each agent along the path.
3. **Check `routing.max_hops`.** Advertisements are not forwarded beyond this many hops (default 16).
4. **Trigger an advertisement.** Exits advertise every `routing.advertise_interval` (default 2 minutes). To advertise immediately, on the exit:

   ```bash
   curl -X POST http://localhost:8080/routes/advertise
   ```

Routes expire if they are not refreshed within `routing.route_ttl` (default 5 minutes). A route that disappears a few minutes after an exit or transit agent goes offline has expired as designed.

## Routing problems

`route trace` explains which route an agent would use for a destination:

```bash
muti-metroo route trace 10.99.1.1
```

```text
Trace for 10.99.1.1
    -> 10.99.0.0/16         metric=1 hops=1 next_hop=exitB (bff2a511) origin=exitB (bff2a511)
```

Add `-t <agent-id>` to run the trace on a remote agent. When several routes match, the most specific prefix wins, then the lowest metric.

```text
Trace for 8.8.8.8
    no matching CIDR route
```

When no route matches, the ingress agent connects to the destination directly from its own host instead of through the mesh. If connections reach a destination from the wrong network, this is the usual cause: add the CIDR to an exit, or add `0.0.0.0/0` and `::/0` to an exit to send everything through the mesh.

Host names are resolved on the ingress agent before the route lookup unless they match a domain route. Trace a host name to see the addresses it resolves to and the route for each. A SOCKS connection uses the first address in the list, which may be IPv6; an exit that advertises only `0.0.0.0/0` does not match it.

```bash
muti-metroo route trace intranet.corp.example.com
```

## Test the full path

From the ingress side:

```bash
curl -v -x socks5h://127.0.0.1:1080 http://10.99.1.1/
```

To test an exit's own network reachability without SOCKS, send ICMP echo through it with [`muti-metroo ping`](/cli/ping) (ICMP must be enabled on the exit):

```bash
muti-metroo ping bff2a511dad5ea3c3d8025c58f12e5b5 10.99.1.1
```

To check that every agent in the mesh is reachable from the agent you are on:

```bash
muti-metroo mesh-test
```

## Logs

Set `agent.log_level: "debug"` and restart the agent for detailed connection and routing logs. Under systemd, follow them with:

```bash
journalctl -u muti-metroo -f
```

## See Also

- [Common Issues](/troubleshooting/common-issues): error messages and fixes
- [peer CLI](/cli/peer): `peer status`, `peer add`, and `peer list`
- [route CLI](/cli/route): `route status` and `route trace`
- [probe CLI](/cli/probe): testing listeners from another host
- [Routing](/concepts/routing): how routes are advertised and selected
- [Connections Configuration](/configuration/connections): keepalive, timeouts, and reconnection
