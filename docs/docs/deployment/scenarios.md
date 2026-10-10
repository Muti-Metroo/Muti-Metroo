---
title: Deployment Scenarios
sidebar_position: 1
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-climbing.png" alt="Mole deploying" style={{maxWidth: '180px'}} />
</div>

# Deployment Scenarios

Pick the pattern that matches your goal. Each scenario shows you exactly how to configure agents to solve a specific access problem.

**Quick Guide:**

| I want to... | Use |
|--------------|-----|
| Access internal resources from anywhere | [Corporate Network Access](#scenario-1-corporate-network-access) |
| Connect multiple office networks | [Multi-Site Connectivity](#scenario-2-multi-site-connectivity) |
| Route all my traffic through a secure gateway | [Secure Internet Gateway](#scenario-3-secure-internet-gateway) |
| Get through corporate firewalls that block everything | [Firewall Traversal](#scenario-4-firewall-traversal) |
| Stay connected even when servers fail | [High Availability](#scenario-5-high-availability) |

## Scenario 1: Corporate Network Access

Provide remote access to internal resources without VPN infrastructure.

### Architecture

```mermaid
flowchart LR
    subgraph Internet
        Cloud[Cloud Relay]
    end

    subgraph Office[Office Network]
        Gateway[Office Gateway]
        Internal[Internal Network]
    end

    Laptop[Remote Laptop] -->|SOCKS5| Cloud
    Cloud -->|QUIC| Gateway
    Gateway --> Internal
```

### Configuration

**Cloud Relay (Transit):**

```yaml
agent:
  display_name: "Cloud Relay"

tls:
  ca: "/etc/muti-metroo/certs/ca.crt"
  cert: "/etc/muti-metroo/certs/agent.crt"
  key: "/etc/muti-metroo/certs/agent.key"
  mtls: true

listeners:
  - transport: quic
    address: "0.0.0.0:4433"
    # Uses global TLS settings

socks5:
  enabled: true
  address: "0.0.0.0:1080"
  auth:
    enabled: true
    users:
      - username: "remote-team"
        password_hash: "${SOCKS5_PASSWORD_HASH}"

http:
  enabled: true
  address: ":8080"
```

**Office Gateway (Exit):**

```yaml
agent:
  display_name: "Office Gateway"

tls:
  ca: "/etc/muti-metroo/certs/ca.crt"
  cert: "/etc/muti-metroo/certs/agent.crt"
  key: "/etc/muti-metroo/certs/agent.key"

peers:
  - id: "${CLOUD_RELAY_ID}"
    transport: quic
    address: "cloud-relay.example.com:4433"
    # Uses global CA and cert/key

exit:
  enabled: true
  routes:
    - "10.0.0.0/8"           # Internal network
  dns:
    servers:
      - "10.0.0.1:53"        # Internal DNS

http:
  enabled: true
  address: ":8080"
```

### Usage

```bash
# Remote user connects via SOCKS5
curl -x socks5://remote-team:password@cloud-relay.example.com:1080 \
  http://internal-app.company.local

# SSH to internal server
ssh -o ProxyCommand='nc -x cloud-relay.example.com:1080 %h %p' user@server.internal
```

---

## Scenario 2: Multi-Site Connectivity

Connect multiple office locations through a cloud relay.

### Architecture

```mermaid
flowchart TB
    subgraph SiteA[Site A - 10.1.0.0/16]
        A[Site A Agent]
    end

    subgraph SiteB[Site B - 10.2.0.0/16]
        B[Site B Agent]
    end

    subgraph SiteC[Site C - 10.3.0.0/16]
        C[Site C Agent]
    end

    Cloud[Cloud Relay]

    A <-->|QUIC| Cloud
    B <-->|QUIC| Cloud
    C <-->|QUIC| Cloud
```

### Configuration

**Cloud Relay:**

```yaml
agent:
  display_name: "Cloud Relay"

tls:
  ca: "/etc/muti-metroo/certs/ca.crt"
  cert: "/etc/muti-metroo/certs/agent.crt"
  key: "/etc/muti-metroo/certs/agent.key"
  mtls: true

listeners:
  - transport: quic
    address: "0.0.0.0:4433"
    # Uses global TLS settings
```

**Site A Gateway:**

```yaml
agent:
  display_name: "Site A Gateway"

tls:
  ca: "/etc/muti-metroo/certs/ca.crt"
  cert: "/etc/muti-metroo/certs/agent.crt"
  key: "/etc/muti-metroo/certs/agent.key"

peers:
  - id: "${CLOUD_RELAY_ID}"
    transport: quic
    address: "cloud-relay.example.com:4433"
    # Uses global TLS settings

socks5:
  enabled: true
  address: "10.1.0.10:1080"

exit:
  enabled: true
  routes:
    - "10.1.0.0/16"
  dns:
    servers:
      - "10.1.0.1:53"
```

**Site B Gateway:**

```yaml
agent:
  display_name: "Site B Gateway"

tls:
  ca: "/etc/muti-metroo/certs/ca.crt"
  cert: "/etc/muti-metroo/certs/agent.crt"
  key: "/etc/muti-metroo/certs/agent.key"

peers:
  - id: "${CLOUD_RELAY_ID}"
    transport: quic
    address: "cloud-relay.example.com:4433"
    # Uses global TLS settings

socks5:
  enabled: true
  address: "10.2.0.10:1080"

exit:
  enabled: true
  routes:
    - "10.2.0.0/16"
  dns:
    servers:
      - "10.2.0.1:53"
```

### Routing

From Site A, traffic to Site B:
1. User configures `10.2.0.10:1080` as SOCKS5 proxy
2. Traffic goes to Site A Gateway
3. Route lookup: `10.2.x.x` matches Site B's `10.2.0.0/16`
4. Traffic routes through Cloud Relay to Site B Gateway
5. Site B Gateway opens connection to destination

---

## Scenario 3: Secure Internet Gateway

Route all internet traffic through a secure exit point.

### Architecture

```mermaid
flowchart LR
    User[User Device] -->|SOCKS5<br/>localhost:1080| Gateway[Gateway Agent]
    Gateway -->|Internet| Web[Internet]
```

### Configuration

**All-in-One Agent:**

```yaml
agent:
  display_name: "Secure Gateway"

listeners:
  - transport: quic
    address: "0.0.0.0:4433"
    tls:
      cert: "./certs/agent.crt"
      key: "./certs/agent.key"

socks5:
  enabled: true
  address: "127.0.0.1:1080"
  auth:
    enabled: true
    users:
      - username: "user"
        password_hash: "${SOCKS5_PASSWORD_HASH}"

exit:
  enabled: true
  routes:
    - "0.0.0.0/0"
  dns:
    servers:
      - "1.1.1.1:53"
      - "8.8.8.8:53"

http:
  enabled: true
  address: ":8080"
```

### Usage

```bash
# Configure browser or system to use localhost:1080 as SOCKS5 proxy
# All traffic will exit through the gateway
```

---

## Scenario 4: Firewall Traversal

Connect agents through restrictive corporate firewalls.

### Architecture

```mermaid
flowchart LR
    subgraph Corp[Corporate Network]
        Agent[Corporate Agent]
        Proxy[HTTP Proxy]
    end

    subgraph Cloud[Cloud]
        CloudAgent[Cloud Agent]
    end

    Agent -->|Via Proxy| Proxy
    Proxy -->|WebSocket<br/>HTTPS:443| CloudAgent
```

### Configuration

**Corporate Agent:**

```yaml
agent:
  display_name: "Corporate Agent"

peers:
  - id: "${CLOUD_AGENT_ID}"
    transport: ws
    address: "wss://cloud.example.com:443/mesh"
    proxy: "http://proxy.corp.local:8080"
    proxy_auth:
      username: "${PROXY_USER}"
      password: "${PROXY_PASS}"
    tls:
      ca: "./certs/ca.crt"

socks5:
  enabled: true
  address: "127.0.0.1:1080"
```

**Cloud Agent:**

```yaml
agent:
  display_name: "Cloud Agent"

listeners:
  - transport: ws
    address: "0.0.0.0:443"
    path: "/mesh"
    tls:
      cert: "/etc/letsencrypt/live/cloud.example.com/fullchain.pem"
      key: "/etc/letsencrypt/live/cloud.example.com/privkey.pem"

exit:
  enabled: true
  routes:
    - "0.0.0.0/0"
  dns:
    servers:
      - "8.8.8.8:53"
```

---

## Scenario 5: High Availability

Multiple paths for fault tolerance.

### Architecture

```mermaid
flowchart LR
    Ingress[Ingress Agent] <-->|Primary Path| Primary[Primary Relay]
    Ingress <-->|Backup Path| Backup[Backup Relay]
    Primary --> Exit[Exit Agent]
    Backup --> Exit
```

### Configuration

**Ingress Agent:**

```yaml
agent:
  display_name: "Ingress"

peers:
  # Primary path
  - id: "${PRIMARY_RELAY_ID}"
    transport: quic
    address: "primary-relay.example.com:4433"
    tls:
      ca: "./certs/ca.crt"

  # Backup path
  - id: "${BACKUP_RELAY_ID}"
    transport: quic
    address: "backup-relay.example.com:4433"
    tls:
      ca: "./certs/ca.crt"

socks5:
  enabled: true
  address: "127.0.0.1:1080"
```

Both relays connect to the same exit agent. Routes are learned from both, with the primary having lower metric (fewer hops).

### Failover Behavior

1. Primary relay goes offline
2. Route via primary expires (TTL)
3. Traffic switches to backup route
4. Primary recovers - traffic returns (lower metric)

---

## Deployment Checklist

### Pre-Deployment

- [ ] Generate CA certificate
- [ ] Generate agent certificates for all nodes
- [ ] Plan network topology and routes
- [ ] Configure firewall rules

### Deployment

- [ ] Deploy exit agents first (routes must be available)
- [ ] Deploy transit/relay agents
- [ ] Deploy ingress agents last
- [ ] Verify connectivity between all agents
- [ ] Test SOCKS5 proxy end-to-end

### Post-Deployment

- [ ] Monitor logs
- [ ] Set up alerting for certificate expiration
- [ ] Document the topology
- [ ] Plan certificate rotation

## Next Steps

- [Docker Deployment](/deployment/docker) - Containerized deployment
- [System Service](/deployment/system-service) - Install as service
- [PM2 Process Manager](/deployment/pm2) - Node.js-based process management
- [High Availability](/deployment/high-availability) - Redundancy patterns
