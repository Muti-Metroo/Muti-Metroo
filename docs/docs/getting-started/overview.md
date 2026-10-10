---
title: Getting Started Overview
sidebar_position: 1
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-presenting.png" alt="Mole presenting" style={{maxWidth: '180px'}} />
</div>

# Getting Started

In the next few minutes, you will have a working tunnel that can reach networks behind firewalls, relay traffic through multiple hops, and route connections to any destination.

## Try It in 2 Minutes

No installation needed - just Docker:

```bash
git clone https://github.com/postalsys/Muti-Metroo.git
cd Muti-Metroo/examples/docker-tryout
docker compose up -d
```

**That's it!** You now have a working 6-agent mesh:

- **Dashboard API:** `curl http://localhost:18080/api/dashboard | jq`
- **Test proxy:** `curl -x socks5h://localhost:11080 https://httpbin.org/ip`

Query the dashboard API to see how agents connect through the mesh, then continue below for production setup.

---

## Production Setup

**What you will build:**
- A SOCKS5 proxy that tunnels traffic through restricted networks
- Multi-hop relay chains that reach destinations you could not access directly
- Exit points that connect to internal resources or the internet

:::tip Transparent Routing with TUN Interface
Want to route all traffic through the mesh without configuring each application for SOCKS5? On Linux, you can use [Mutiauk](/mutiauk) - a companion TUN interface that transparently intercepts Layer 3 traffic and forwards it through Muti Metroo.
:::

### Prerequisites

- **Muti Metroo binary** - [Download for your platform](/download)
- **Basic networking knowledge** (TCP/IP, CIDR notation)

### Choose Your Path

| Path | Best For | Time |
|------|----------|------|
| **[Interactive Setup](/getting-started/interactive-setup)** | New users, guided experience | 5-10 min |
| **[Quick Start](/getting-started/quick-start)** | Manual control, scripting | 10-15 min |
| **[Docker](/deployment/docker)** | Containerized deployments | 5 min |

### Recommended: Interactive Setup

The wizard handles everything - certificates, identity, and configuration:

```bash
muti-metroo setup
```

[Go to Interactive Setup](/getting-started/interactive-setup)

### Alternative: Manual Configuration

For full control or automation:

```bash
muti-metroo init -d ./data
# Create config.yaml (see Quick Start)
muti-metroo run -c ./config.yaml
```

[Go to Quick Start](/getting-started/quick-start)

## Next Steps

After setup, explore:

- **[Your First Mesh](/getting-started/first-mesh)** - Connect multiple agents
- **[Core Concepts](/concepts/architecture)** - Understand the architecture
- **[Configuration Reference](/configuration/overview)** - All available options

## Need Help?

- [Troubleshooting](/troubleshooting/common-issues) - Common issues and solutions
- [FAQ](/troubleshooting/faq) - Frequently asked questions
