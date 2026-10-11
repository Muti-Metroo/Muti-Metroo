---
title: Getting Started
description: Run a demo Muti Metroo mesh in Docker, then choose between the setup wizard and a hand-written config for your own agents.
sidebar_label: Overview
sidebar_position: 1
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-presenting.png" alt="Mole presenting" style={{maxWidth: '180px'}} />
</div>

# Getting Started

This page shows two ways in: a demo mesh that runs in Docker with no installation, and the paths for setting up agents on your own hosts.

## Try a demo mesh in Docker

The repository includes a six-agent example mesh. It needs Docker with Compose and builds the agent image from source.

```bash
git clone https://github.com/Muti-Metroo/Muti-Metroo.git
cd Muti-Metroo/examples/docker-tryout
docker compose up -d
```

The ingress agent publishes its SOCKS5 proxy on port `11080` and its HTTP API on port `18080`. Once the containers are up, check the ingress agent and send a request through the mesh:

```bash
curl http://localhost:18080/healthz
curl -x socks5h://localhost:11080 https://httpbin.org/ip
```

The second command travels through the mesh and returns JSON with the source address that httpbin.org saw. Stop the demo with `docker compose down`. The `README.md` in the `examples/docker-tryout` directory describes the topology and more things to try.

## Set up your own agents

You need the `muti-metroo` binary on each host. See [Installation](/getting-started/installation) or the [Download](/download) page. TLS certificates are generated automatically at startup, so no certificate setup is needed to start; see [TLS Certificates](/configuration/tls-certificates) when you want CA-verified peers.

Choose one of these paths:

| Path | Use it when |
| --- | --- |
| [Interactive Setup](/getting-started/interactive-setup) | You want a guided wizard that writes the config, and optionally installs a service. |
| [Quick Start](/getting-started/quick-start) | You want to write a minimal config by hand, or script the setup. |
| [Docker Deployment](/deployment/docker) | You run agents as containers. |

The wizard is a single command:

```bash
muti-metroo setup
```

The manual path initializes an identity, then runs the agent with a config file you write (the [Quick Start](/getting-started/quick-start) has a minimal one):

```bash
muti-metroo init -d ./data
muti-metroo run -c ./config.yaml
```

After one agent works, [Your First Mesh](/getting-started/first-mesh) connects two or three agents so traffic enters on one host and leaves from another.

:::tip[Transparent routing]
Applications that cannot use a SOCKS proxy can reach the mesh through [Mutiauk](/mutiauk), a companion tool that creates a TUN interface and forwards IP traffic into the agent's SOCKS5 proxy. It runs on Linux, macOS, and Windows and requires root or Administrator.
:::

## See Also

- [Installation](/getting-started/installation): download the binary and install it as a service
- [Your First Mesh](/getting-started/first-mesh): connect an ingress agent to an exit agent
- [Architecture](/concepts/architecture): how agents, routes, and streams fit together
- [Configuration Overview](/configuration/overview): every configuration section
- [Troubleshooting](/troubleshooting/common-issues): fixes for common problems
- [FAQ](/troubleshooting/faq): answers to frequently asked questions
