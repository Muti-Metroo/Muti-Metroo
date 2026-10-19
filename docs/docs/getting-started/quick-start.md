---
title: Quick Start
description: Write a minimal configuration and run a single Muti Metroo agent that serves as a SOCKS5 proxy and exit.
sidebar_position: 4
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-escalator.png" alt="Mole on the move" style={{maxWidth: '180px'}} />
</div>

# Quick Start

This guide runs one agent from a hand-written config file. The agent accepts SOCKS5 connections on `localhost:1080`, opens connections to any destination itself, and serves its HTTP API on `localhost:8080`. A single agent is not yet a mesh; [Your First Mesh](/getting-started/first-mesh) adds a second agent so traffic leaves from another host.

For a guided alternative that writes the config for you, use the [Interactive Setup](/getting-started/interactive-setup) wizard.

## Prerequisites

The `muti-metroo` binary must be installed. See [Installation](/getting-started/installation).

## Step 1: Initialize the agent identity

```bash
muti-metroo init -d ./data
```

```text
Agent initialized in ./data
Agent ID: 250bcc6f4401c0cfe55de1c8b643c9db
```

This step is optional, because the agent creates its identity on first start. Running it first lets you see the agent ID before the agent runs, which other agents need when they [pin a peer ID](/configuration/peers).

## Step 2: Create the configuration file

Create `config.yaml`:

```yaml
agent:
  data_dir: "./data"

listeners:
  - transport: quic
    address: ":4433"

socks5:
  enabled: true
  address: "127.0.0.1:1080"

exit:
  enabled: true
  routes:
    - "0.0.0.0/0"

http:
  enabled: true
  address: "127.0.0.1:8080"
```

With this config, the agent:

- Accepts peer connections from other agents on UDP port 4433 (QUIC). A standalone agent does not need this, but [Your First Mesh](/getting-started/first-mesh) uses it.
- Accepts SOCKS5 connections on `127.0.0.1:1080`.
- Acts as an exit for all IPv4 destinations (`0.0.0.0/0`).
- Serves the HTTP API on `127.0.0.1:8080`.

Every other setting uses its default. The TLS certificate for the QUIC listener is generated in memory at startup. See the [Configuration Overview](/configuration/overview) for all sections.

## Step 3: Run the agent

```bash
muti-metroo run -c ./config.yaml
```

The agent runs in the foreground. The first lines of its output show the agent ID and the services it started:

```text
Loaded configuration from: /home/user/config.yaml (mtime 2026-10-11 14:49:15 EEST)
Starting Muti Metroo agent...
Agent ID: 250bcc6f4401c0cfe55de1c8b643c9db
time=2026-10-11T14:49:15.431+03:00 level=INFO msg="starting agent" agent_id=250bcc6f component=agent
time=2026-10-11T14:49:15.431+03:00 level=INFO msg="HTTP server started" address=127.0.0.1:8080
time=2026-10-11T14:49:15.434+03:00 level=INFO msg="listener started" address=:4433 transport=quic
time=2026-10-11T14:49:15.434+03:00 level=INFO msg="SOCKS5 server started" address=127.0.0.1:1080
time=2026-10-11T14:49:15.434+03:00 level=INFO msg="exit handler started" count=1 domain_routes=0
time=2026-10-11T14:49:15.434+03:00 level=INFO msg="forward handler started" endpoints=0
time=2026-10-11T14:49:15.434+03:00 level=INFO msg="agent started" agent_id=250bcc6f peers=0 listeners=1
```

Stop the agent with Ctrl+C.

## Step 4: Check the agent

In another terminal:

```bash
curl http://localhost:8080/healthz
```

```json
{"accepts_signed_management":false,"can_sign_management":false,"exit_handler_running":true,"peer_count":0,"route_count":1,"running":true,"socks5_running":true,"status":"healthy","stream_count":0}
```

`muti-metroo status` shows the same information in a readable form:

```bash
muti-metroo status
```

```text
Agent Status
============
Status:       healthy
Running:      true
Peer Count:   0
Stream Count: 0
Route Count:  1
SOCKS5:       true
Exit Handler: true
```

## Step 5: Send traffic through the agent

Test the agent with a SOCKS client:

```bash
curl -x socks5h://localhost:1080 https://example.com
```

With `socks5h://`, curl sends the hostname to the proxy and the agent resolves it. Any SOCKS5-capable client works the same way, for example SSH through `nc`:

```bash
ssh -o ProxyCommand='nc -x localhost:1080 %h %p' user@server.example.com
```

## From a test agent to a deployment

This single agent is both ingress and exit, which is convenient for a test. In a deployment, agents run on the mesh nodes, and the operator's workstation reaches the mesh through [Mutiauk](/mutiauk): a TUN interface that receives the mesh's routes from an ingress agent and forwards connections to its SOCKS5 listener, so applications need no proxy settings. [Your First Mesh](/getting-started/first-mesh) builds a two-agent mesh and connects a workstation to it with Mutiauk.

Do not point Mutiauk at an exit agent on the same host for networks that the agent itself dials: the agent's own connections to those networks would follow the TUN routes back into Mutiauk.

## Troubleshooting

### The agent does not start

- Another process may already use one of the ports. On Linux and macOS, `lsof -i :4433 -i :1080 -i :8080` lists them.
- Check the config without starting the agent: `muti-metroo config validate ./config.yaml`.
- For more detail, set `agent.log_level: "debug"` and start the agent again.

### SOCKS5 connection refused

- Check that `socks5.enabled` is `true` and the agent log shows `SOCKS5 server started`.
- `127.0.0.1` accepts only local clients. To accept clients from other hosts, bind another address and enable [SOCKS5 authentication](/configuration/socks5).

See [Common Issues](/troubleshooting/common-issues) for more.

## See Also

- [Your First Mesh](/getting-started/first-mesh): connect an ingress agent to a remote exit agent
- [Configuration Overview](/configuration/overview): every configuration section and its defaults
- [Mutiauk](/mutiauk): the TUN interface for the operator's workstation
- [SOCKS5 Proxy](/features/socks5-proxy): authentication, UDP, and client examples
- [Exit Routing](/features/exit-routing): CIDR and domain routes, and where DNS is resolved
- [`run` command](/cli/run): flags for running the agent
