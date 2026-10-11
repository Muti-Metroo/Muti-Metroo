---
title: Your First Mesh
description: Connect an ingress agent to an exit agent so SOCKS5 traffic leaves the mesh from a remote host, then add a transit agent between them.
sidebar_position: 5
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-wiring.png" alt="Mole wiring connections" style={{maxWidth: '180px'}} />
</div>

# Your First Mesh

This guide connects two agents so that traffic entering one leaves the mesh from the other:

- **Agent A** (ingress) runs where your applications are. It accepts SOCKS5 connections.
- **Agent B** (exit) runs on the remote network. It opens connections to destinations.

A later section adds a third agent as a transit relay between them.

<div className="tunnel-diagram">

```mermaid
flowchart LR
    C["Client<br/>(curl, SSH, browser)"]
    A["Agent A<br/>(Ingress)"]
    B["Agent B<br/>(Exit)"]
    I((Destinations))

    C -->|"SOCKS5<br/>localhost:1080"| A
    A -->|"QUIC<br/>192.168.1.20:4433"| B
    B -->|TCP| I
```

</div>

## Prerequisites

- `muti-metroo` [installed](/getting-started/installation) on both hosts.
- Agent A can reach Agent B on UDP port 4433. Only the dialing side needs to reach the listening side; Agent B never connects to Agent A.

You can also run both agents on one machine. Give each agent its own data directory, and use different ports for the listener and HTTP API.

The examples use `192.168.1.20` as Agent B's address. Replace it with the real address of your exit host.

## Step 1: Initialize Agent B

On Agent B's host:

```bash
muti-metroo init -d ./data-b
```

```text
Agent initialized in ./data-b
Agent ID: a139b7e2819a0ce019552100203b1656
```

Note the agent ID. Agent A uses it to make sure it is talking to the right peer.

## Step 2: Configure and start Agent B (exit)

Create `config-b.yaml`:

```yaml
agent:
  display_name: "Agent B (Exit)"
  data_dir: "./data-b"

# Accept peer connections from other agents
listeners:
  - transport: quic
    address: "0.0.0.0:4433"

# Open connections to any IPv4 destination
exit:
  enabled: true
  routes:
    - "0.0.0.0/0"

http:
  enabled: true
  address: "127.0.0.1:8081"
```

Start it:

```bash
muti-metroo run -c ./config-b.yaml
```

## Step 3: Configure and start Agent A (ingress)

On Agent A's host, create `config-a.yaml`. Put Agent B's ID in `peers[].id`:

```yaml
agent:
  display_name: "Agent A (Ingress)"
  data_dir: "./data-a"

# Dial Agent B
peers:
  - id: "a139b7e2819a0ce019552100203b1656"
    transport: quic
    address: "192.168.1.20:4433"

# Accept SOCKS5 connections from local applications
socks5:
  enabled: true
  address: "127.0.0.1:1080"

http:
  enabled: true
  address: "127.0.0.1:8080"
```

Agent A has no `listeners` section because it only dials out. Add one if other agents need to connect to it.

Start it:

```bash
muti-metroo run -c ./config-a.yaml
```

Agent A creates its identity on first start, and logs the connection:

```text
level=INFO msg="connected to peer" peer_id=a139b7e2 address=192.168.1.20:4433 transport=quic
```

Agent B logs the accepted connection:

```text
level=INFO msg="peer connected" peer_id=bc0e9d4a remote_addr=192.168.1.10:57050
```

## Step 4: Check the connection

On Agent A's host, list the connected peers:

```bash
muti-metroo peer status
```

```text
Connected Peers
===============
ID           NAME                 STATE          ROLE       RTT
--           ----                 -----          ----       ---
a139b7e2     Agent B (Exit)       CONNECTED      dialer     -

Total: 1 peer(s)
```

Agent A has learned Agent B's exit route. The dashboard API shows the route and the path to its origin:

```bash
curl -s http://localhost:8080/api/dashboard | jq '.routes'
```

```json
[
  {
    "network": "0.0.0.0/0",
    "route_type": "cidr",
    "origin": "Agent B (Exit)",
    "origin_id": "a139b7e2",
    "hop_count": 1,
    "path_display": [
      "Agent A (Ingress)",
      "Agent B (Exit)"
    ],
    "path_ids": [
      "bc0e9d4a",
      "a139b7e2"
    ],
    "tcp": true,
    "udp": true
  }
]
```

## Step 5: Send traffic through the mesh

On Agent A's host:

```bash
curl -x socks5h://localhost:1080 https://example.com
```

The page comes back through the mesh. The connection to `example.com` is opened by Agent B, so the destination sees Agent B's address. Other SOCKS5 clients work the same way:

```bash
ssh -o ProxyCommand='nc -x localhost:1080 %h %p' user@server.example.com
```

To use the mesh from a browser, set its SOCKS5 proxy to `localhost:1080` and enable DNS through the proxy.

## How the traffic flows

When you run `curl -x socks5h://localhost:1080 https://example.com`:

1. curl connects to Agent A's SOCKS5 proxy and asks for `example.com:443`.
2. Agent A resolves `example.com`, takes the first address the resolver returns, and looks it up in its route table. For an IPv4 address the only match is `0.0.0.0/0`, advertised by Agent B.
3. Agent A opens an end-to-end encrypted stream to Agent B.
4. Agent B opens a TCP connection to the destination and relays data in both directions.

Because Agent A resolves the name for CIDR routes, it must be able to resolve the hostnames you use. To resolve names at the exit instead, use [domain routes](/features/exit-routing).

If the first address is IPv6 and no route covers it, Agent A opens the connection from its own host instead of through the mesh. `muti-metroo route trace example.com` shows the resolved addresses and the route for each. When Agent B has IPv6 connectivity, add `"::/0"` to its `exit.routes`.

## Add a transit agent

A transit agent relays traffic between other agents. It needs no SOCKS5 or exit configuration. This section puts Agent C between A and B.

```mermaid
flowchart LR
    A[Agent A<br/>Ingress] --> C["Agent C<br/>(Transit)"] --> B[Agent B<br/>Exit]
```

On Agent C's host (`192.168.1.30` in this example), initialize it and note its ID:

```bash
muti-metroo init -d ./data-c
```

```text
Agent initialized in ./data-c
Agent ID: c90ff182452eb355e857ae7ab93ab563
```

Create `config-c.yaml`. Agent C listens for Agent A and dials Agent B:

```yaml
agent:
  display_name: "Agent C (Transit)"
  data_dir: "./data-c"

listeners:
  - transport: quic
    address: "0.0.0.0:4433"

peers:
  - id: "a139b7e2819a0ce019552100203b1656"
    transport: quic
    address: "192.168.1.20:4433"

http:
  enabled: true
  address: "127.0.0.1:8082"
```

Change the `peers` section of `config-a.yaml` to dial Agent C instead of Agent B:

```yaml
peers:
  - id: "c90ff182452eb355e857ae7ab93ab563"
    transport: quic
    address: "192.168.1.30:4433"
```

Start Agent C, then restart Agent A. Agent B's route now reaches Agent A through Agent C, and `hop_count` becomes `2`:

```bash
curl -s http://localhost:8080/api/dashboard | jq '.routes[0].path_display'
```

```json
[
  "Agent A (Ingress)",
  "Agent C (Transit)",
  "Agent B (Exit)"
]
```

Traffic is still encrypted end to end between A and B. Agent C forwards it without being able to read it.

## Troubleshooting

### Agent A never connects

- Check that Agent B's log shows `listener started` with the expected address.
- QUIC uses UDP. Check that firewalls between the hosts allow UDP to port 4433, or switch both sides to `h2` or `ws`, which use TCP. See [Transports](/concepts/transports).
- If `peers[].id` does not match the ID Agent B presents, the handshake fails. Use `id: "auto"` to accept any ID while testing.

### Traffic does not go through Agent B

- If `muti-metroo peer status` shows no peers, fix the connection first.
- If the dashboard shows no route that covers the destination, check that Agent B has `exit.enabled: true` and a matching route. `muti-metroo route trace <ip>` explains which route the agent would pick.

See [Connectivity](/troubleshooting/connectivity) for more.

## See Also

- [Architecture](/concepts/architecture): agent roles, routing, and encryption in depth
- [Peers](/configuration/peers): peer connection options, including TLS and HTTP proxies
- [Listeners](/configuration/listeners): QUIC, HTTP/2, and WebSocket listener options
- [Exit Routing](/features/exit-routing): CIDR and domain routes
- [Deployment Scenarios](/deployment/scenarios): topologies for real deployments
- [Best Practices](/security/best-practices): hardening a mesh for production
