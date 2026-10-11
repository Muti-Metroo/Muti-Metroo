---
title: Frequently Asked Questions
description: Short answers to common questions about running, configuring, and troubleshooting Muti Metroo, with links to the full documentation.
sidebar_position: 4
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-reading.png" alt="Mole reading FAQ" style={{maxWidth: '180px'}} />
</div>

# Frequently Asked Questions

Short answers, each with a link to the page that covers the topic in full.

## Running the agent

### Does the agent need root or Administrator rights?

No. The agent runs in userspace with no TUN device or kernel module. Elevation is needed only to bind ports below 1024 on Linux and macOS, and to install a system-wide service. [User services](/deployment/system-service#linux-user-service) and [PM2](/deployment/pm2) work without it.

### Which platforms are supported?

Linux, macOS, and Windows, each on amd64 and arm64. Windows also has a DLL build for each architecture. See [Download](/download).

### Can I run several agents on one machine?

Yes. Give each agent its own config file, its own `agent.data_dir`, and its own listener, SOCKS, and HTTP API ports. Install each as a service with a different `-n <name>`.

### Where are the logs?

The agent writes its log to stderr.

| How it runs | Where the log goes |
|-------------|--------------------|
| Foreground | The terminal |
| systemd service | `journalctl -u muti-metroo` |
| systemd user service | `journalctl --user -u muti-metroo` |
| Linux cron user service | `~/.muti-metroo/muti-metroo.log` |
| macOS launchd | `muti-metroo.err.log` in the config file's directory |
| Docker | `docker logs <container>` |
| PM2 | `pm2 logs muti-metroo` |
| Windows Service, DLL | Not captured |

Set `agent.log_level: "debug"` for more detail and `agent.log_format: "json"` for machine-readable output. See [Agent Configuration](/configuration/agent).

### Do I have to restart after changing the config file?

Yes. The config file is read only at startup. Routes, peers, listeners, forwards, mesh hosts, and the display name can also be changed on a running agent with the dynamic management commands, such as [`route add`](/cli/route) and [`peer add`](/cli/peer). Those changes are saved in the data directory and survive restarts.

### Can I use environment variables in the config?

Yes. `${VAR}` and `${VAR:-default}` are replaced with environment variable values when the config is loaded:

```yaml
agent:
  log_level: "${LOG_LEVEL:-info}"
```

See [Environment Variables](/configuration/environment-variables).

## Networking

### Which transport should I use?

| Situation | Transport |
|-----------|-----------|
| UDP allowed between agents | QUIC |
| Only TCP allowed, or UDP is throttled | HTTP/2 |
| Outbound only through an HTTP proxy, or behind a reverse proxy | WebSocket |

A mesh can mix transports; each peer connection uses its own. See [Transports](/concepts/transports).

### How many hops can a path have?

The current release does not limit path length: `routing.max_hops` is validated (1 to 255) but not applied to route propagation; see [Routing configuration](/configuration/routing). Each hop adds its network latency to connection setup, so keep paths as short as the topology allows.

### Is IPv6 supported?

Yes, for listeners, peers, SOCKS listeners and destinations, exit routes, and DNS servers. Write IPv6 addresses in brackets with a port:

```yaml
listeners:
  - transport: quic
    address: "[::]:4433"

exit:
  enabled: true
  routes:
    - "0.0.0.0/0"
    - "::/0"
```

When the ingress agent resolves a host name, it uses the first address the resolver returns, which may be an IPv6 address. Advertise `::/0` alongside `0.0.0.0/0` on exits that should carry all traffic.

### What happens to traffic that matches no route?

The ingress agent connects to the destination directly from its own host. Use `muti-metroo route trace <destination>` to see whether a destination matches a route. See [Connectivity Troubleshooting](/troubleshooting/connectivity#routing-problems).

## SOCKS

### Which SOCKS features are supported?

SOCKS5, SOCKS4, and SOCKS4a on the same listener, detected automatically. SOCKS5 supports CONNECT and UDP ASSOCIATE, IPv4, IPv6, and domain name destinations, and either no authentication or username and password. BIND is not supported. See [SOCKS5 Proxy](/features/socks5-proxy) and [UDP Relay](/features/udp-relay).

### How do I use the SOCKS listener with SSH?

```bash
ssh -o ProxyCommand='nc -x 127.0.0.1:1080 %h %p' user@10.0.12.5
```

`nc -x` does not support SOCKS authentication. If the listener requires it, use `ncat --proxy 127.0.0.1:1080 --proxy-type socks5 --proxy-auth user:password %h %p` as the proxy command.

### Do host names resolve at the ingress or the exit?

At the ingress, with its system resolver, unless the name matches a domain route (`exit.domain_routes`). Names that match a domain route are passed through the mesh and resolved by the exit, using `exit.dns` if set. The client must send the name to the proxy (`socks5h://` in curl) for either to happen. See [Exit Routing](/features/exit-routing).

## Security

### How is traffic protected?

Each link between agents uses TLS 1.3. In addition, every stream is encrypted end to end between the ingress and the exit with X25519 key exchange and ChaCha20-Poly1305, so transit agents cannot read the payload. See [End-to-End Encryption](/security/e2e-encryption).

### Is mTLS required?

No. Without it, any client that can reach a listener can complete the TLS handshake. With `tls.mtls: true`, a listener accepts only clients with a certificate signed by the mesh CA. See [TLS and mTLS](/security/tls-mtls).

### Can I use Let's Encrypt certificates?

For listeners, yes, if the certificate uses an ECDSA key (Certbot: `--key-type ecdsa`). For mTLS you still need your own CA, because client certificates must be signed by the CA in `tls.ca`. See [TLS Certificates](/configuration/tls-certificates).

### How do I rotate certificates?

Generate new certificates from the same CA, replace the files, and restart the agent. `muti-metroo cert info <file>` shows the expiry date, and `muti-metroo run` warns at startup when a configured certificate expires within 60 days. See [cert CLI](/cli/cert).

## Management

### Can I run commands and transfer files on remote agents?

Yes, when the target enables them and both sides have the management signing keys configured:

```bash
muti-metroo shell bff2a511dad5ea3c3d8025c58f12e5b5 whoami
muti-metroo shell --tty bff2a511dad5ea3c3d8025c58f12e5b5 bash
muti-metroo upload bff2a511dad5ea3c3d8025c58f12e5b5 ./local.txt /tmp/remote.txt
muti-metroo download bff2a511dad5ea3c3d8025c58f12e5b5 /tmp/remote.txt ./local.txt
```

See [Shell](/features/shell) and [File Transfer](/features/file-transfer).

### How do I check that agents are connected?

```bash
muti-metroo peer status
muti-metroo route status
muti-metroo mesh-test
```

See [Connectivity Troubleshooting](/troubleshooting/connectivity).

### Is there an API for monitoring?

Yes. `/health` and `/healthz` report agent health, and the dashboard endpoints (`/api/dashboard`, `/api/topology`, `/api/nodes`) describe the mesh. See [Health API](/api/health) and [Dashboard API](/api/dashboard). For a browser interface, see [Muti Metroo Manager](/muti-metroo-manager).

## See Also

- [Common Issues](/troubleshooting/common-issues): error messages and fixes
- [Connectivity Troubleshooting](/troubleshooting/connectivity): peers and routes step by step
- [CLI Overview](/cli/overview): all commands
- [Configuration Overview](/configuration/overview): all config sections
