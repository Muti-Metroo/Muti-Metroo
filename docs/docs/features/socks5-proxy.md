---
title: SOCKS Proxy
description: The agent's SOCKS4, SOCKS4a, and SOCKS5 listener, where Mutiauk and SOCKS-aware applications such as curl, SSH, and browsers send traffic into the mesh.
sidebar_position: 1
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-presenting.png" alt="Mole presenting SOCKS proxy" style={{maxWidth: '180px'}} />
</div>

# SOCKS Proxy

The SOCKS listener is where traffic enters the mesh. A client connects to the ingress agent's listener, and the agent carries the connection through the mesh to an exit agent that opens the real TCP connection. The listener accepts SOCKS4, SOCKS4a, and SOCKS5, detected per connection.

Two kinds of client use it:

- **[Mutiauk](/mutiauk)** on the operator's workstation. This is the usual way in: Mutiauk captures traffic on a TUN interface and carries its TCP connections, UDP datagrams, and pings over SOCKS5, so applications on the workstation need no proxy settings. Over the same listener, the agent pushes the mesh's routes and hostnames to Mutiauk.
- **SOCKS-aware applications** connecting directly, such as curl, SSH through `nc`, browsers, and proxychains. Use them to test a mesh, to send a single application through it, on machines where you cannot run Mutiauk as root or Administrator, or for names that only a [domain route](/features/exit-routing#route-types) can reach.

```bash
curl -x socks5h://localhost:1080 https://internal.example.com
ssh -o ProxyCommand='nc -x localhost:1080 %h %p' user@192.168.1.20
```

The examples on this page connect to `localhost:1080`. When the listener runs on another host, use that host's address and the SOCKS credentials.

Enable the listener in the `socks5` section of the agent configuration. Bind address, authentication, connection limits, and the optional WebSocket transport are described in [SOCKS5 Configuration](/configuration/socks5).

## How destinations are routed

When a client connects, the ingress agent picks a path for the destination:

1. If the client sends a hostname that matches a [domain route](/features/exit-routing#route-types), the connection goes to that exit by name and the exit agent resolves it.
2. Otherwise a hostname is resolved at the ingress agent, first against the [mesh hostname registry](/features/mesh-hostnames), then through the system resolver.
3. The IP (for a resolved hostname, the first address returned) is matched against the CIDR route table (longest prefix wins) and the connection goes to the exit that advertised the route.
4. If no route matches, the ingress agent dials the destination directly from its own host, as if it were the exit.

:::warning[Unrouted destinations]
Step 4 means a SOCKS client can reach anything the ingress host itself can reach, not only the networks advertised by exits. Bind the listener to `127.0.0.1` or enable authentication, and use `muti-metroo route trace <destination>` to check whether a destination goes through the mesh.
:::

Mutiauk sends only IP addresses, because the workstation has already resolved the name before the traffic reaches the TUN interface. Domain routes therefore apply only to SOCKS clients that send hostnames. Mesh hostnames still work with Mutiauk: its own [mesh DNS](/mutiauk#mesh-dns) resolver answers them on the workstation.

### socks5 versus socks5h

With `socks5://`, curl resolves the hostname on your machine and sends only an IP to the proxy, so domain routes and mesh hostnames never apply. With `socks5h://`, curl sends the hostname to the agent, which applies the order above. Use `socks5h://` (or SOCKS4a) for internal names that your machine cannot resolve.

```bash
curl -x socks5h://localhost:1080 https://internal.example.com
```

## Client examples

These examples connect SOCKS-aware applications directly to the listener. To route a whole workstation instead, see [Mutiauk](/mutiauk).

### curl

```bash
curl -x socks5h://localhost:1080 https://example.com

# With SOCKS5 authentication
curl -x socks5h://user1:password@localhost:1080 https://example.com

# SOCKS4a (hostname resolved by the agent; no authentication)
curl -x socks4a://localhost:1080 https://example.com
```

### SSH

```bash
# OpenBSD netcat
ssh -o ProxyCommand='nc -x localhost:1080 %h %p' user@192.168.1.20

# Nmap ncat
ssh -o ProxyCommand='ncat --proxy localhost:1080 --proxy-type socks5 %h %p' user@192.168.1.20
```

To use the proxy for a group of hosts, add a block to `~/.ssh/config`:

```text
Host 192.168.1.*
    ProxyCommand nc -x localhost:1080 %h %p
```

### Firefox

1. Open Settings, then Network Settings, then Manual proxy configuration.
2. Set SOCKS Host to `localhost` and Port to `1080`.
3. Select SOCKS v5.
4. Check "Proxy DNS when using SOCKS v5" so hostnames are resolved by the agent.

Firefox prompts for a username and password when the listener requires authentication.

### Git

```bash
# One clone through the proxy
git -c http.proxy=socks5h://localhost:1080 clone https://github.com/Muti-Metroo/Muti-Metroo.git

# All HTTP(S) Git traffic through the proxy
git config --global http.proxy socks5h://localhost:1080

# Remove the global setting
git config --global --unset http.proxy
```

### Python

With [PySocks](https://pypi.org/project/PySocks/) installed (`pip install pysocks`):

```python
import socket
import socks
import urllib.request

socks.set_default_proxy(socks.SOCKS5, "localhost", 1080, rdns=True)
socket.socket = socks.socksocket

print(urllib.request.urlopen("https://example.com").status)
```

### proxychains

Add the agent to `/etc/proxychains.conf` or `~/.proxychains/proxychains.conf`:

```ini
[ProxyList]
socks5 127.0.0.1 1080
```

Then run any dynamically linked program through it:

```bash
proxychains4 curl https://example.com
proxychains4 ssh admin@192.168.1.20
```

## UDP and ICMP

UDP datagrams (DNS, NTP) go through the same listener, with SOCKS5 UDP ASSOCIATE or the UDP STREAM extension that Mutiauk uses; see [UDP Relay](/features/udp-relay). Ping uses a separate path; see [ICMP Relay](/features/icmp-relay).

## Verifying the proxy

Check that the agent is running its SOCKS listener and has peers and routes:

```bash
curl -s http://localhost:8080/healthz
```

```json
{"accepts_signed_management":false,"can_sign_management":false,"exit_handler_running":false,"peer_count":1,"route_count":1,"running":true,"socks5_running":true,"status":"healthy","stream_count":0}
```

Check which route a destination would use:

```bash
muti-metroo route trace 192.168.1.20
```

Then send a request through the proxy:

```bash
curl -x socks5h://localhost:1080 https://example.com
```

## Troubleshooting

### Connection refused

```text
curl: (7) Failed to connect to localhost port 1080 after 0 ms: Couldn't connect to server
```

The agent is not running, the listener is disabled, or it listens on a different address. Check `socks5_running` in `/healthz` and the `socks5.address` setting.

### Authentication rejected

```text
curl: (97) User was rejected by the SOCKS5 server (1 1).
```

The username or password is wrong, or the listener requires authentication and the client sent none. Usernames are case-sensitive.

### Destination unreachable or timed out

```text
curl: (97) Can't complete SOCKS5 connection to 10.1.2.3. (4)
```

The exit agent (or the ingress agent, for an unrouted destination) could not reach the destination. Run `muti-metroo route trace 10.1.2.3` to see which agent handles it, then check that the agent is connected (`peer_count` in `/healthz`) and that its host can reach the destination.

### Internal hostname does not resolve

```text
curl: (6) Could not resolve host: internal.example.com
```

curl tried to resolve the name locally because the proxy URL used `socks5://`. Use `socks5h://`.

## See Also

- [SOCKS5 Configuration](/configuration/socks5): listener address, authentication, limits, and WebSocket transport
- [Exit Routing](/features/exit-routing): make destinations reachable through an exit agent
- [Mesh Hostnames](/features/mesh-hostnames): give in-mesh IPs names that SOCKS clients can use
- [UDP Relay](/features/udp-relay): send UDP traffic through the SOCKS5 listener
- [Mutiauk](/mutiauk): the TUN interface that operators use to reach the mesh through this listener
- [Common Issues](/troubleshooting/common-issues): broader troubleshooting
