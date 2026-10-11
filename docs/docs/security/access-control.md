---
title: Access Control
description: What exit routes, the SOCKS listener, the management command whitelist, file transfer paths, and network bindings restrict, and what they leave open.
sidebar_position: 5
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-inspecting.png" alt="Mole controlling access" style={{maxWidth: '180px'}} />
</div>

# Access Control

[Authentication](/security/authentication) decides who may use an agent. Access control decides what they can reach once they do. Each control below applies to one kind of traffic; none of them covers the others.

| Control | Restricts | Configured in |
| ------- | --------- | ------------- |
| Exit routes | TCP destinations an exit agent connects to | [Exit Configuration](/configuration/exit) |
| UDP and ICMP settings | Whether an agent relays UDP or answers ping requests | [UDP](/configuration/udp), [ICMP](/configuration/icmp) |
| SOCKS listener address and authentication | Who can send traffic into the mesh | [SOCKS5 Configuration](/configuration/socks5) |
| Management command whitelist | Commands that may run on an agent | [Management Commands Configuration](/configuration/shell) |
| File transfer paths | Files that may be read, written, or deleted | [File Transfer Configuration](/configuration/file-transfer) |
| Forward endpoints | Services published under a routing key | [Forward Configuration](/configuration/forward) |
| Listener addresses, mutual TLS, firewalls | Who can connect to an agent at all | [TLS and mTLS](/security/tls-mtls) |

## Exit routes

An exit agent opens a TCP connection only when the destination is covered by one of its own routes, configured or added at runtime:

- An IP address must fall inside one of the exit's CIDR routes.
- A hostname that matches one of the exit's domain routes is allowed, whatever IP it resolves to.
- A hostname that matches no domain route is allowed only if its resolved IP falls inside a CIDR route.

Anything else is refused with "destination not allowed". This holds even if another agent sends the exit a request for a destination it never advertised.

| Routes | Effect |
| ------ | ------ |
| `10.0.0.0/8` | Only the internal network; no internet access |
| `192.168.1.20/32` | A single host |
| `*.internal.example.com` | Hosts under one domain, wherever their names resolve |
| `0.0.0.0/0` | Everything the exit host can reach |

Exit routes do not restrict:

- **UDP relay.** An agent with `udp.enabled: true` (the default) sends UDP datagrams to any destination it is asked to, without checking its routes.
- **ICMP echo.** An agent with `icmp.enabled: true` (the default) pings any IP address it is asked to, and `muti-metroo ping` lets any caller of an agent's HTTP API choose the agent that sends the pings.
- **Forward endpoints**, which always connect to their own configured target.

Set `udp.enabled: false` and `icmp.enabled: false` on agents that should not relay UDP or ICMP.

## SOCKS listener

The SOCKS listener is the entry point for most traffic, and it allows more than exit routes suggest:

- A destination that no route covers is dialed directly from the ingress agent's host. A SOCKS client can therefore reach anything that host can reach.
- Mutiauk's `expose` feature registers port forward endpoints through the SOCKS listener. Any SOCKS client can publish services into the mesh under unused routing keys.
- UDP ASSOCIATE starts a UDP relay for the client.

The only controls are the listener's bind address and SOCKS authentication. Bind to `127.0.0.1` unless other hosts need the proxy, and enable authentication when they do:

```yaml
socks5:
  enabled: true
  address: "127.0.0.1:1080"
  auth:
    enabled: true
    users:
      - username: "operator"
        password_hash: "$2a$12$dTB6hp0rb6lWsON4uHeTHOSHvO6mzANiuI4ZOv9.MfAhpRiKUvkie"
```

The UDP relay socket binds to the same IP address as the TCP listener.

## Management command whitelist

`shell.whitelist` lists the commands that a signed request may run:

| Whitelist | Behavior |
| --------- | -------- |
| `[]` (default) | No commands |
| `["uptime", "df"]` | Only these commands, with argument checks |
| `["*"]` | Any command with any arguments |

For any whitelist other than `["*"]`:

- Commands must be given by their bare name and match an entry exactly, including case. A path such as `/usr/bin/uptime` is always rejected.
- Arguments containing shell metacharacters (`` ; & | $ ` ( ) { } [ ] < > \ ! * ? ~ ``) are rejected.
- Arguments that are absolute paths are rejected.
- Commands are started directly, not through a shell.

These checks look at characters, not at what a program does with its arguments. Whitelisting a program that runs other programs or reads arbitrary files gives the same access as that program:

- Interpreters and shells (`bash`, `sh`, `python3`, `perl`, `powershell.exe`): `bash -c "..."` runs any command line without metacharacters.
- Programs that execute other commands (`env`, `xargs`, `find`, `nice`, `timeout`, `sudo`).
- Programs that read or write files given as relative paths (`cat`, `cp`, `tar`, `dd`).

The client also chooses the environment variables and working directory of the command; they are not restricted.

Safer choices for monitoring are programs that only report state, such as `uptime`, `hostname`, `whoami`, `df`, `free`, and `ps`.

## File transfer paths

`file_transfer.allowed_paths` lists the locations that upload, download, and browse (including `chmod` and `delete`) may touch:

| Entry | Allows |
| ----- | ------ |
| `[]` (default) | Nothing |
| `/var/log/app` | That directory and everything below it |
| `/srv/data/**` | Everything below `/srv/data` |
| `/home/*/uploads` | `uploads` in every home directory, and everything below it |
| `*` | Every absolute path |

Requested paths must be absolute, and any path containing `..` is rejected. When the final path component is a symbolic link, its target must also be allowed. Symbolic links in intermediate directories are followed without that check, so do not allow directories in which untrusted users can create symbolic links. Pattern syntax is described in [File Transfer Configuration](/configuration/file-transfer).

`file_transfer.max_file_size` (default 500 MB) limits the size of each transfer.

## Network controls

Bind each service to the narrowest address that works:

| Address | Reachable from |
| ------- | -------------- |
| `127.0.0.1:1080` | The local host only |
| `192.168.1.10:1080` | Hosts that can reach that interface |
| `0.0.0.0:1080` | Every interface |

With [mutual TLS](/security/tls-mtls#mutual-tls), peer listeners refuse connections from hosts without a certificate from your CA. Host firewalls add another layer:

```bash
# Accept peer connections (QUIC on UDP 4433) only from the agents' network
iptables -A INPUT -p udp --dport 4433 -s 10.0.0.0/24 -j ACCEPT
iptables -A INPUT -p udp --dport 4433 -j DROP

# Keep the HTTP API local
iptables -A INPUT -p tcp --dport 8080 ! -s 127.0.0.1 -j DROP
```

## Example: a locked-down exit

An exit that serves one internal network, accepts only signed management requests from operators, and allows a few monitoring commands and one transfer directory:

```yaml
tls:
  ca: "./certs/ca.crt"
  cert: "./certs/exit-1.crt"
  key: "./certs/exit-1.key"
  strict: true
  mtls: true

listeners:
  - transport: quic
    address: "0.0.0.0:4433"

http:
  address: "127.0.0.1:8080"
  token_hash: "${HTTP_TOKEN_HASH}"

exit:
  enabled: true
  routes:
    - "10.0.0.0/8"

udp:
  enabled: false

icmp:
  enabled: false

shell:
  enabled: true
  whitelist:
    - uptime
    - df
    - free

file_transfer:
  enabled: true
  allowed_paths:
    - /srv/transfers
  max_file_size: 10485760

management:
  signing_public_key: "${MESH_SIGNING_PUBLIC_KEY}"
```

## Troubleshooting

### destination not allowed

The exit agent received a TCP request for a destination outside its routes. Check the routes with `muti-metroo route list --all -t <exit-agent-id>` and the ingress agent's decision with `muti-metroo route trace <destination>`.

### command is not allowed, or argument rejected

The command is missing from `shell.whitelist`, was given with a path, or has an argument with a metacharacter or an absolute path. See [Management Commands](/features/shell#troubleshooting).

### path not in allowed list

The path is outside every `file_transfer.allowed_paths` entry. See [File Transfer](/features/file-transfer#troubleshooting).

## See Also

- [Authentication](/security/authentication): who can use each entry point
- [Exit Routing](/features/exit-routing): configure and inspect exit routes
- [Management Commands Configuration](/configuration/shell): whitelist and session limits
- [File Transfer Configuration](/configuration/file-transfer): path patterns and size limits
- [Best Practices](/security/best-practices): production hardening
