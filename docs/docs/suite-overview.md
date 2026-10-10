---
sidebar_position: 10
title: Suite Overview
description: How Muti Metroo, Mutiauk, and Muti Metroo Manager work together as a complete networking solution
---

# The Muti Metroo Suite

Muti Metroo is part of a three-component suite designed to cover the full spectrum of mesh networking needs -- from encrypted traffic tunneling, to transparent OS-level routing, to browser-based management. Each component is a standalone binary that can be deployed independently, but together they form a complete networking solution.

## Component Roles

| Component                              | Purpose                                                                       | Platform                    | Requires Root |
| -------------------------------------- | ----------------------------------------------------------------------------- | --------------------------- | ------------- |
| [**Muti Metroo**](/getting-started/overview) | Mesh networking agent -- encrypted tunnels, SOCKS5 proxy, multi-hop routing   | All (Linux, macOS, Windows) | No            |
| [**Mutiauk**](/mutiauk)        | TUN interface -- transparent L3 traffic interception through the SOCKS5 proxy | All (Linux, macOS, Windows) | Yes           |
| [**Muti Metroo Manager**](/muti-metroo-manager)            | Web dashboard -- browser-based monitoring, management, and remote access      | All (Linux, macOS, Windows) | No            |

## Architecture

The following diagram shows how the three components interact:

```mermaid
flowchart TB
    subgraph Client Machine
        App[Applications] -->|IP Traffic| TUN[Mutiauk<br/>TUN Interface]
        TUN -->|SOCKS5| Agent1[Muti Metroo<br/>Ingress Agent]
        Browser[Browser] -->|HTTP| Manager[Muti Metroo Manager<br/>:3000]
        Manager -->|HTTP API| Agent1
        SocksApp[SOCKS-Aware Apps] -->|SOCKS5| Agent1
    end

    subgraph Mesh Network
        Agent1 -->|Encrypted| Agent2[Muti Metroo<br/>Transit Agent]
        Agent2 -->|Encrypted| Agent3[Muti Metroo<br/>Exit Agent]
    end

    Agent3 -->|TCP/UDP| Dest[Destinations]
```

**Traffic paths:**

- **SOCKS-aware applications** connect directly to Muti Metroo's SOCKS5 proxy
- **All other applications** have their traffic transparently intercepted by Mutiauk's TUN interface and forwarded through the SOCKS5 proxy
- **Operators** use Muti Metroo Manager's web UI to monitor the mesh topology, manage routes, run management commands, and transfer files

## When to Use What

| Scenario                                                | Components Needed   | Why                                                              |
| ------------------------------------------------------- | ------------------- | ---------------------------------------------------------------- |
| Route traffic from a specific app (browser, curl)       | Muti Metroo only          | Configure the app to use SOCKS5 proxy directly                   |
| Route all traffic from a machine transparently          | Muti Metroo + Mutiauk | Mutiauk intercepts L3 traffic so no per-app config is needed |
| Monitor and manage the mesh from a browser              | Muti Metroo + Muti Metroo Manager   | Manager provides a visual dashboard and remote access tools      |
| Full deployment with transparent routing and management | All three           | Complete solution for production environments                    |

## Deployment Example

A realistic deployment using all three components:

### Step 1: Deploy Muti Metroo Agents

Set up the mesh by deploying agents on each host. At minimum, you need an ingress agent (where clients connect) and an exit agent (where traffic leaves the mesh).

```bash
# On the ingress host
muti-metroo init -d ./data
muti-metroo run -c ./ingress-config.yaml

# On the exit host
muti-metroo init -d ./data
muti-metroo run -c ./exit-config.yaml
```

The agents connect to each other and advertise routes automatically. See [First Mesh](/getting-started/first-mesh) for a complete walkthrough.

### Step 2: Add Mutiauk for Transparent Routing

On any client that needs transparent access to the mesh, install Mutiauk and point it at the local ingress agent's SOCKS5 proxy:

```bash
sudo mutiauk setup
# Configure:
#   SOCKS5 server: 127.0.0.1:1080
#   Routes: networks reachable through the mesh

sudo mutiauk daemon start
```

Now all applications on this machine can reach destinations through the mesh without any proxy configuration.

### Step 3: Add Muti Metroo Manager for Web Monitoring

On any machine with access to an agent's HTTP API, launch Muti Metroo Manager:

```bash
muti-metroo-manager -agent http://192.168.1.10:8080
# Open http://localhost:3000 in your browser
```

The dashboard shows the mesh topology, agent status, routes, and provides management command and file transfer access to all reachable agents.

## Component Downloads

| Component   | Download                                  |
| ----------- | ----------------------------------------- |
| Muti Metroo       | [Download page](/download)                |
| Mutiauk | [Download section](/mutiauk#download) |
| Muti Metroo Manager   | [Download section](/muti-metroo-manager#download)   |

## Next Steps

- [Getting Started](/getting-started/overview) -- set up your first Muti Metroo mesh
- [Mutiauk](/mutiauk) -- detailed Mutiauk configuration and usage
- [Muti Metroo Manager](/muti-metroo-manager) -- detailed Muti Metroo Manager configuration and features
