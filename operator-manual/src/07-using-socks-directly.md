# Use Case: Using the SOCKS Proxy Directly (Bypassing Mutiauk)

Sometimes you do not want every packet on your machine routed through the mesh. You want one application -- a single browser tab, one curl command, one tool -- to traverse the mesh, while the rest of the system keeps working normally.

The right tool for that is the agent's **SOCKS proxy**, not Mutiauk. The Muti Metroo agent speaks SOCKS5 and SOCKS4/4a on the same listener -- the protocol is auto-detected from the first byte the client sends -- so any tool that supports any SOCKS dialect will work. SOCKS is opt-in per application; Mutiauk's TUN is opt-out (you would have to remove the route to bypass it).

SOCKS4 has no authentication mechanism, so it is only available when `socks5.auth.enabled: false` on the agent. If your shared agent requires authentication, your tools must speak SOCKS5 with the username/password method.

## When to Reach for SOCKS5 Instead of TUN

| Scenario                                                                             | Use SOCKS5 directly | Use Mutiauk TUN        |
| ------------------------------------------------------------------------------------ | ------------------- | -------------------------- |
| One browser tab through the mesh                                                     | Yes                 | No                         |
| Whole-machine transparent access to a CIDR                                           | No                  | Yes                        |
| Tool that does not honour the system route table (raw sockets, custom DNS)           | Yes                 | No                         |
| Tool you want to keep simple to debug (`tcpdump` on the SOCKS port shows everything) | Yes                 | No                         |
| `ping` to a routed CIDR                                                              | No                  | Yes (TUN's ICMP forwarder) |
| Tools wrapped in proxychains (`ssh`, `curl`)                                         | Yes                 | No                         |
| Application with native SOCKS5 proxy settings                                        | Yes                 | No                         |

A useful mental model: the TUN is a kernel-level capture, SOCKS5 is an application-level opt-in. They can co-exist on the same workstation -- a tool that knows about SOCKS5 will use it directly, and tools that do not will fall through to the TUN.

## The SOCKS5 Endpoint

Two endpoints are typically available on the shared host:

| Endpoint                             | Use                   | Notes                                                                                                                    |
| ------------------------------------ | --------------------- | ------------------------------------------------------------------------------------------------------------------------ |
| `socks5://shared-host:1080`          | Plain TCP SOCKS5      | Direct connection from the LAN.                                                                                          |
| `wss://muti-metroo.example.com:443/socks5` | SOCKS5 over WebSocket | Goes through the same TLS reverse proxy as the dashboard; useful when you are on a network that blocks non-HTTPS egress. |

If the agent has authentication enabled, you also have a username/password pair (Chapter 3, Step 3, `socks5.auth.users`).

## Examples

### A Single curl

```bash
curl -x socks5h://ops:secret@shared-host:1080 \
     --max-time 10 http://10.20.120.11/

# socks5h means "resolve hostnames at the proxy" -- important for mesh-host
# names (e.g. http://target.mesh/) and any private DNS the exit agent has.
```

### Firefox

`Preferences -> Network Settings -> Manual proxy configuration`:

- SOCKS Host: `shared-host`, Port: `1080`
- SOCKS v5
- Tick "Proxy DNS when using SOCKS v5"
- Username/password if required

Now this Firefox profile uses the mesh; other browsers and applications stay direct. Run a separate Firefox profile (`firefox -P muti-metroo`) so you can keep a normal browser open at the same time.

### SSH

```bash
ssh -o ProxyCommand='ncat --proxy shared-host:1080 --proxy-type socks5 \
                      --proxy-auth ops:secret %h %p' \
    op@10.20.120.11
```

Add this to `~/.ssh/config` if you do it often:

```
Host *.internal
  ProxyCommand ncat --proxy shared-host:1080 --proxy-type socks5 \
               --proxy-auth ops:secret %h %p
```

### proxychains

`/etc/proxychains4.conf`:

```
strict_chain
proxy_dns
[ProxyList]
socks5 shared-host 1080 ops secret
```

```bash
proxychains4 ssh op@10.20.120.11
proxychains4 git clone https://internal.example.com/repo.git
```

### git

```bash
git -c http.proxy=socks5h://shared-host:1080 \
    clone https://internal.example.com/repo.git
```

## SOCKS5 over WebSocket

If your egress only allows HTTPS, the shared-host agent's WebSocket SOCKS5 listener (`socks5.websocket.address`, see Chapter 3) speaks SOCKS5 over WSS instead of raw TCP. Two ways to consume it:

**1. Mutiauk (the standard workstation setup).** Point Mutiauk's
`socks5.server` at the WebSocket URL and switch transport to `websocket`. The
TUN section is still mandatory -- Mutiauk is fundamentally a TUN tool; it
does not expose its own local SOCKS5 listener. The TUN captures packets on
the workstation and Mutiauk forwards each TCP / UDP flow into the
shared-host SOCKS5 server over WSS.

```yaml
# /etc/mutiauk/config.yaml on a tightly firewalled workstation
tun:
  name: muti-metroo0
  address: 10.200.200.1/24
  mtu: 1500

socks5:
  server: wss://muti-metroo.example.com:443/socks5
  username: ops
  password: secret
  transport: websocket
  ws_path: /socks5

routes:
  - destination: 10.20.0.0/16
    comment: "lab subnet"
```

**2. Application-level WSS-SOCKS5 client.** A handful of tools (custom
proxychains forks, bespoke proxies) speak SOCKS5-over-WSS directly without
needing Mutiauk. Most operators do not have such a tool; if you do, point
it at the same `wss://muti-metroo.example.com:443/socks5` URL.

There is no "TUN-less Mutiauk" mode -- `tun.name` is required at config
load and Mutiauk will refuse to start without it.

## Caveats

- **Raw-socket tools bypass SOCKS5.** SYN scanners, `traceroute` (default UDP/ICMP probes), and anything else that builds packets from scratch goes via your default interface. Use TCP-connect-based tools through the proxy.
- **`socks5://` vs `socks5h://`.** The `h` variant resolves hostnames at the proxy. Without it, your local resolver will be asked to resolve names that may only exist on the far side of the mesh, and the lookup will fail.
- **No ICMP through SOCKS5.** Standard SOCKS5 has no ICMP support. Muti Metroo's SOCKS5 has a custom ICMP relay that Mutiauk uses for `ping`, but a direct `curl` or `proxychains` will not give you ping. Use the **agent panel -> Ping** in Muti Metroo Manager instead, or `muti-metroo ping <agent-id> <ip>` from the CLI.
- **UDP works only via UDP ASSOCIATE.** Most SOCKS5 clients only do TCP CONNECT. Tools that explicitly support UDP through SOCKS5 (`proxychains4` with `proxy_dns`, custom code) will work; everything else will not.

## When Both Are Active on the Same Box

Running Mutiauk on the workstation (Chapter 5) does not block you from using SOCKS5 directly. The flow is:

- Application **knows** it is using SOCKS5 -> the operator points it directly at `shared-host:1080` (or the WebSocket URL). Mutiauk is not in the path and the TUN does not see those packets. (Mutiauk does not run a local SOCKS5 listener of its own.)
- Application **does not** know about SOCKS5 -> it sends regular IP packets, and if the destination matches an active route the TUN captures them and Mutiauk tunnels each flow into the shared-host SOCKS5 server.

In practice, run Mutiauk as your default routing layer, and reach for direct SOCKS5 only when you need application-scoped behaviour (one Firefox profile, one git clone, one curl).
