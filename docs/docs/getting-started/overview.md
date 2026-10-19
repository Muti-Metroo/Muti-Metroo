---
title: Getting Started
description: Run a demo Muti Metroo mesh in Docker, set up agents on your mesh nodes with the setup wizard or a hand-written config, and connect your workstation with Mutiauk.
sidebar_label: Overview
sidebar_position: 1
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-presenting.png" alt="Mole presenting" style={{maxWidth: '180px'}} />
</div>

# Getting Started

A Muti Metroo deployment has two sides. Agents (`muti-metroo`) run on the mesh nodes: relays, gateways, and hosts inside the networks you want to reach. The operator's workstation runs [Mutiauk](/mutiauk), which connects to an ingress agent and sends traffic for the mesh's networks through it, and optionally [Muti Metroo Manager](/muti-metroo-manager) for a web view of the mesh. The [Suite Overview](/suite-overview) describes which component runs where.

This page shows a demo mesh that runs in Docker with no installation, the paths for setting up agents on your own hosts, and how to connect your workstation to them.

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

The second command uses the ingress agent's SOCKS listener directly, which is the quickest way to test a mesh. It travels through the mesh and returns JSON with the source address that httpbin.org saw. Stop the demo with `docker compose down`. The `README.md` in the `examples/docker-tryout` directory describes the topology and more things to try.

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

## Connect your workstation

Once the agents are running, install [Mutiauk](/mutiauk) on your workstation and point it at the SOCKS5 listener of an ingress agent. With autoroutes enabled (the setup wizard's default), Mutiauk receives the mesh's routes and hostnames from the agent and installs them on a TUN interface, so `ssh`, `curl`, browsers, and other tools reach mesh destinations without proxy settings:

```bash
sudo mutiauk setup
sudo mutiauk daemon start
```

Mutiauk runs on Linux, macOS, and Windows and requires root or Administrator. Where that is not available, SOCKS-aware applications can use the agent's [SOCKS listener](/features/socks5-proxy) directly.

## See Also

- [Installation](/getting-started/installation): download the binary and install it as a service
- [Your First Mesh](/getting-started/first-mesh): connect an ingress agent to an exit agent
- [Mutiauk](/mutiauk): install and configure the TUN interface on your workstation
- [Architecture](/concepts/architecture): how agents, routes, and streams fit together
- [Configuration Overview](/configuration/overview): every configuration section
- [Troubleshooting](/troubleshooting/common-issues): fixes for common problems
