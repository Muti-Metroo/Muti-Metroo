---
title: Your First Mesh
sidebar_position: 5
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-wiring.png" alt="Mole wiring connections" style={{maxWidth: '180px'}} />
</div>

# Your First Mesh Network

This guide shows you how to tunnel traffic through a remote network. You will connect two agents so that traffic entering one exits from the other - reaching destinations that would otherwise be inaccessible.

**What you will build:**
- An ingress agent that accepts your SOCKS5 connections
- An exit agent on a remote network that opens connections to destinations
- A secure tunnel between them that traverses any network path

## The Setup

Here is what you are building:

<div className="tunnel-diagram">

```mermaid
flowchart LR
    C["Client<br/>(Browser)"]
    A["Agent A<br/>(Ingress)"]
    B["Agent B<br/>(Exit)"]
    I((Internet))

    C -->|"SOCKS5<br/>localhost:1080"| A
    A -->|"QUIC/TLS<br/>192.168.1.20:4433"| B
    B -->|TCP| I
```

</div>

- **Agent A** (your machine): Where you connect with curl, SSH, or browser
- **Agent B** (remote network): Opens connections to destinations you want to reach

## Prerequisites

- Muti Metroo [installed](/getting-started/installation)
- Two machines or terminals (can use same machine with different ports)
- Network connectivity between agents

:::info Automatic TLS
No certificate setup required. Agents auto-generate TLS certificates and E2E encryption secures all traffic. For strict verification, see [TLS/mTLS](/security/tls-mtls).
:::

## Step 1: Initialize Agent Identities

### Agent A

```bash
muti-metroo init -d ./data-a
# Note the Agent ID: aaaa1111....
```

### Agent B

```bash
muti-metroo init -d ./data-b
# Note the Agent ID: bbbb2222....
```

## Step 2: Configure Agent B (Exit Node)

Create `config-b.yaml`:

```yaml
agent:
  id: "auto"
  display_name: "Agent B (Exit)"
  data_dir: "./data-b"
  log_level: "info"

# Listen for peer connections
listeners:
  - transport: quic
    address: "0.0.0.0:4433"

# Exit node - open connections to internet
exit:
  enabled: true
  routes:
    - "0.0.0.0/0"              # Default route - all traffic

# HTTP API
http:
  enabled: true
  address: ":8081"             # Different port if on same machine
```

Start Agent B:

```bash
muti-metroo run -c ./config-b.yaml
```

Note the Agent ID from the output (e.g., `bbbb2222333344445555666677778888`).

## Step 3: Configure Agent A (Ingress Node)

Create `config-a.yaml` with Agent B's ID:

```yaml
agent:
  id: "auto"
  display_name: "Agent A (Ingress)"
  data_dir: "./data-a"
  log_level: "info"

# Listen for peer connections (optional, for other agents)
listeners:
  - transport: quic
    address: "0.0.0.0:4434"    # Different port if on same machine

# Connect to Agent B
peers:
  - id: "bbbb2222333344445555666677778888"  # Agent B's ID
    transport: quic
    address: "192.168.1.20:4433"            # Agent B's address

# SOCKS5 proxy for client connections
socks5:
  enabled: true
  address: "127.0.0.1:1080"

# HTTP API
http:
  enabled: true
  address: ":8080"
```

Start Agent A:

```bash
muti-metroo run -c ./config-a.yaml
```

## Step 4: Verify Connection

### Check Agent A's Peers

```bash
curl http://localhost:8080/healthz
```

Expected output:

```json
{
  "status": "healthy",
  "running": true,
  "peer_count": 1,
  "stream_count": 0,
  "route_count": 1,
  "socks5_running": true,
  "exit_handler_running": false
}
```

### Check Logs

Agent A should show:

```
INFO  Connected to peer peer_id=bbbb2222... addr=192.168.1.20:4433
INFO  Route added cidr=0.0.0.0/0 next_hop=bbbb2222... metric=1
```

Agent B should show:

```
INFO  Accepted peer connection peer_id=aaaa1111... addr=192.168.1.10:xxxxx
```

## Step 5: Test the Mesh

Now test that traffic flows through the mesh:

```bash
# From Agent A's machine
curl -x socks5://localhost:1080 https://example.com
```

You should see the webpage content. In the logs:

**Agent A:**
```
INFO  SOCKS5 connect request dest=example.com:443
INFO  Stream opened stream_id=1 dest=example.com:443
```

**Agent B:**
```
INFO  Stream open request stream_id=1 dest=example.com:443
INFO  Exit connection established dest=93.184.216.34:443
```

## Step 6: Test More Applications

### SSH Through the Mesh

```bash
ssh -o ProxyCommand='nc -x localhost:1080 %h %p' user@remote-server
```

### Browser Proxy

Configure your browser to use `localhost:1080` as SOCKS5 proxy.

### curl with Verbose Output

```bash
curl -v -x socks5://localhost:1080 https://httpbin.org/ip
```

## How It Works

When you run `curl -x socks5://localhost:1080 https://example.com`:

1. Your request goes to Agent A's SOCKS5 proxy
2. Agent A finds that Agent B advertises a route to the destination
3. Agent A tunnels the connection through to Agent B
4. Agent B opens the real TCP connection to example.com
5. Data flows back through the tunnel to your curl command

The key insight: Agent B can reach destinations that Agent A cannot. Your traffic exits from Agent B's network.

## Querying the Dashboard API

Check the mesh topology via the dashboard API:

```bash
# Agent A topology
curl http://localhost:8080/api/topology | jq

# Agent B dashboard overview
curl http://localhost:8081/api/dashboard | jq
```

The topology endpoint returns all agents and their connections.

## Adding a Third Agent (Transit)

To add a transit node between A and B:

```mermaid
flowchart LR
    A[Agent A] <--> C["Agent C<br/>(Transit)"] <--> B[Agent B]
```

Agent C configuration:

```yaml
agent:
  id: "auto"
  display_name: "Agent C (Transit)"
  data_dir: "./data-c"
  log_level: "info"

listeners:
  - transport: quic
    address: "0.0.0.0:4435"

# Connect to Agent B
peers:
  - id: "bbbb2222..."
    transport: quic
    address: "192.168.1.20:4433"

# No socks5 or exit - pure transit
http:
  enabled: true
  address: ":8082"
```

Update Agent A to connect to Agent C instead of Agent B:

```yaml
peers:
  - id: "cccc3333..."     # Agent C's ID
    transport: quic
    address: "192.168.1.30:4435"
```

Routes will propagate: Agent A learns about Agent B's 0.0.0.0/0 route through Agent C.

## Troubleshooting

### No Route to Host

```
Error: no route to 93.184.216.34
```

- Check Agent B is running and has exit enabled
- Verify connection between agents
- Check routes: `curl http://localhost:8080/healthz`

### Connection Refused

```
Error: connection refused
```

- Verify Agent B is listening on correct address
- Check firewall rules
- Verify TLS certificates

### Certificate Error (strict TLS only)

If you enabled [strict TLS verification](/security/tls-mtls):

```
Error: certificate verify failed
```

- Ensure both agents use certs signed by same CA
- Check certificate expiration
- Verify peer ID matches certificate

See [Troubleshooting Guide](/troubleshooting/common-issues) for more help.

## Next Steps

- [Core Concepts](/concepts/architecture) - Understand the architecture
- [Configuration Reference](/configuration/overview) - All configuration options
- [Deployment Scenarios](/deployment/scenarios) - Real-world deployment patterns
- [Security Best Practices](/security/best-practices) - Production hardening
