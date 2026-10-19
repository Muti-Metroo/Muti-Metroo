# Agent Roles

Every Muti Metroo agent can serve one or more roles simultaneously. Understanding these roles is essential for designing your mesh topology.

## Role Overview

| Role        | Description                                | Typical Use                |
| ----------- | ------------------------------------------ | -------------------------- |
| **Ingress** | Accepts client connections via SOCKS5      | Entry point that Mutiauk and SOCKS clients connect to |
| **Transit** | Relays traffic between other agents        | Network bridge             |
| **Exit**    | Opens connections to external destinations | Internet/network gateway   |

```mermaid
flowchart LR
    Client[Mutiauk or<br/>SOCKS client] --> Ingress
    Ingress --> Transit
    Transit --> Exit
    Exit --> Target[Target Server]

    style Ingress fill:#5b9bd5
    style Transit fill:#a89f94
    style Exit fill:#6db36d
```

## Ingress Role

An **ingress agent** accepts client connections on its SOCKS5 listener and initiates streams into the mesh. The client is usually Mutiauk on an operator workstation (Chapter 10); SOCKS-aware applications can also connect directly (Chapter 9). The ingress agent is usually a mesh node, such as a relay on a public server, rather than the operator's own machine.

### Responsibilities

- Run SOCKS5 server on configured address
- Authenticate clients (if enabled)
- Perform route lookup for destinations
- Open streams to the appropriate exit node
- Relay data between the SOCKS5 client and the mesh stream
- Push route, mesh-host, and DNS suffix snapshots to Mutiauk clients with autoroutes enabled

### Configuration

```yaml
socks5:
  enabled: true
  address: "127.0.0.1:1080"
  auth:
    enabled: false
  max_connections: 1000
```

### Typical Deployments

- **Public relays and cloud instances**: Entry point that operator workstations running Mutiauk connect to
- **Edge servers**: Remote access gateway for a site
- **Local agent on a workstation**: Single-operator setups and tests with SOCKS clients on `127.0.0.1`

### Example: Ingress-Only Agent

A relay that operator workstations reach over the internet. Authentication is enabled, and the SOCKS5 WebSocket listener serves TLS with the agent's `tls` certificate so Mutiauk can connect with a `wss://` URL (Chapter 9):

```yaml
agent:
  display_name: "Ingress Gateway"

listeners:
  - transport: quic
    address: "0.0.0.0:4433"

peers:
  - id: "transit-agent-id..."
    transport: quic
    address: "relay.example.com:4433"

tls:
  cert: "./certs/relay.crt" # ECDSA certificate the workstations trust
  key: "./certs/relay.key"

socks5:
  enabled: true
  address: "127.0.0.1:1080" # plain TCP listener kept local
  auth:
    enabled: true
    users:
      - username: "operator"
        password_hash: "$2a$12$..."
  websocket:
    enabled: true
    address: "0.0.0.0:8443"
    path: "/socks5"

# No exit configuration - pure ingress
```

## Transit Role

A **transit agent** relays traffic between other agents without initiating or terminating connections.

### Responsibilities

- Receive data from connected peers
- Forward data to next hop based on routing
- Propagate route advertisements through the mesh
- Provide bridging between network segments

### Configuration

Transit role is implicit - any agent that connects to multiple peers becomes a transit node:

```yaml
# No specific configuration needed
# Just connect to multiple peers
peers:
  - id: "peer-a-id..."
    transport: quic
    address: "192.168.1.10:4433"

  - id: "peer-b-id..."
    transport: quic
    address: "192.168.1.20:4433"
```

### Typical Deployments

- **Cloud relay**: Bridge between on-premise networks
- **DMZ server**: Connect internal and external networks
- **Geographic hop**: Reduce latency across regions

### Example: Transit-Only Agent

```yaml
agent:
  display_name: "Cloud Transit"

listeners:
  - transport: quic
    address: "0.0.0.0:4433"

peers:
  - id: "site-a-agent..."
    transport: quic
    address: "site-a.example.com:4433"

  - id: "site-b-agent..."
    transport: quic
    address: "site-b.example.com:4433"

# No socks5 or exit - pure transit
http:
  enabled: true
  address: ":8080"
```

## Exit Role

An **exit agent** opens real TCP connections to external destinations.

### Responsibilities

- Advertise CIDR routes to the mesh
- Accept stream open requests from ingress agents
- Validate destination against allowed routes
- Open TCP connections to destinations
- Handle DNS resolution for domain names

### Configuration

```yaml
exit:
  enabled: true
  routes:
    - "10.0.0.0/8"
    - "192.168.0.0/16"
    - "0.0.0.0/0"
  # dns.servers defaults to [] (use the host's system resolver, including
  # split-horizon DNS and .local / mDNS). Configure explicit upstreams only
  # when you need to override that. Do NOT paste public resolvers
  # (8.8.8.8 / 1.1.1.1) into compliance-bound or confidentiality-sensitive
  # deployments; that leaks every resolved name to a third party.
```

**Note:** All agents are reachable by ID for management operations (management commands, file transfer, ping, status queries) regardless of whether they have exit routes configured. Exit routes only control which network destinations an agent can reach on behalf of ingress clients.

### Typical Deployments

- **Internet gateway**: Default route for all traffic
- **Private network access**: Routes to internal resources
- **Service endpoint**: Access to specific services/CIDRs

### Example: Exit-Only Agent

```yaml
agent:
  display_name: "Exit Gateway"

listeners:
  - transport: quic
    address: "0.0.0.0:4433"

exit:
  enabled: true
  routes:
    - "10.0.0.0/8"
  # Use the host's system resolver by default. The explicit override
  # below is shown only because internal CIDRs sometimes need a private
  # resolver that the host has not been told about:
  dns:
    servers:
      - "10.0.0.1:53" # internal resolver inside 10.0.0.0/8
    timeout: 5s

# No socks5 - pure exit
http:
  enabled: true
  address: ":8080"
```

## Combined Roles

Agents can combine multiple roles. Common patterns:

### Ingress + Transit

Acts as entry point and relay:

```yaml
socks5:
  enabled: true
  address: "127.0.0.1:1080"

peers:
  - id: "remote-exit-id..."
    transport: quic
    address: "remote.example.com:4433"
```

### Transit + Exit

Relays traffic and provides exit for specific routes:

```yaml
peers:
  - id: "ingress-id..."
    transport: quic
    address: "ingress.example.com:4433"

exit:
  enabled: true
  routes:
    - "10.0.0.0/8"
```

### All Roles (Standalone)

Single agent that does everything:

```yaml
agent:
  display_name: "All-in-One"

listeners:
  - transport: quic
    address: "0.0.0.0:4433"

socks5:
  enabled: true
  address: "127.0.0.1:1080"

exit:
  enabled: true
  routes:
    - "0.0.0.0/0"
  # dns.servers defaults to [] -> use the host's system resolver.
  # Override only when you need to (do not paste 8.8.8.8 here for
  # compliance-bound or confidentiality-sensitive deployments).
```

## Role Selection Guide

| Scenario                    | Recommended Roles                  |
| --------------------------- | ---------------------------------- |
| Local proxy for one user    | Ingress + Exit (standalone)        |
| Operator access to internal networks | Ingress (cloud relay, Mutiauk on each workstation) + Exit (each internal network) |
| Multi-site connectivity     | Transit (cloud) + Exit (each site) |
| Internet gateway for office | Ingress (office) + Exit (DMZ)      |

## Roles vs. Connection Direction

A common misconception is that connection direction determines agent roles. This is **not true**.

**Key principle**: Transport connection direction (who dials whom) is independent of agent roles (ingress/transit/exit).

Consider this topology where Agent B dials Agent A:

```mermaid
flowchart LR
    subgraph A["Agent A"]
        A1["SOCKS5 Ingress<br/>Exit: 10.0.0.0/8"]
    end
    subgraph B["Agent B"]
        B1["SOCKS5 Ingress<br/>Exit: 192.168.0.0/16"]
    end
    B1 -->|dial| A1
```

Even though B dialed A, virtual streams can flow **both directions**:

- A client connected to Agent A's SOCKS5 listener can tunnel to `192.168.0.0/16` via Agent B's exit
- A client connected to Agent B's SOCKS5 listener can tunnel to `10.0.0.0/8` via Agent A's exit

**Practical implications:**

| Concern                       | Depends On                                          |
| ----------------------------- | --------------------------------------------------- |
| Which agent can be ingress    | SOCKS5 configuration, not connection direction      |
| Which agent can be exit       | Exit routes configuration, not connection direction |
| Which agent initiates streams | Route table, not connection direction               |
| Connection direction choice   | Network constraints (firewalls, NAT)                |

Think of transport connections as **bidirectional pipes**. Once connected, it doesn't matter which end opened the connection - data and routes flow freely in both directions. Choose connection direction based on network accessibility, not functionality.

## Best Practices

1. **Minimize exit points**: Fewer exits are easier to monitor and secure
2. **Place transit in DMZ**: Transit agents don't need to access sensitive resources
3. **Use specific routes**: Avoid `0.0.0.0/0` on exits unless necessary
4. **Separate concerns**: Don't combine ingress and exit on the same agent in production
5. **Enable HTTP API**: Enable for health checks and monitoring on every agent
