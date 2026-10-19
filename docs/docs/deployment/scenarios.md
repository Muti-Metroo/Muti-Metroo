---
title: Deployment Scenarios
description: Example Muti Metroo topologies with complete agent configurations for remote access, site-to-site links, internet egress, and firewall traversal.
sidebar_position: 1
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-climbing.png" alt="Mole deploying" style={{maxWidth: '180px'}} />
</div>

# Deployment Scenarios

Each scenario on this page is a complete topology with the configuration for every agent. Agents run on the mesh nodes, and operators reach the mesh from their workstations with [Mutiauk](/mutiauk), which connects to an ingress agent's SOCKS listener; SOCKS-aware applications can use the same listener directly. Adapt the addresses and routes, generate certificates as described in [TLS Certificates](/configuration/tls-certificates), and start the agents with `muti-metroo run -c <config>` or as a [system service](/deployment/system-service).

| Goal | Scenario |
|------|----------|
| Reach an internal network from anywhere | [Remote access to an office network](#remote-access-to-an-office-network) |
| Connect several sites | [Site-to-site connectivity](#site-to-site-connectivity) |
| Send internet traffic out through a remote host | [Internet egress through a remote exit](#internet-egress-through-a-remote-exit) |
| Connect out of a network that only allows web traffic | [Firewall traversal on port 443](#firewall-traversal-on-port-443) |
| Survive the loss of an agent | [High Availability](/deployment/high-availability) |

The examples use `id: "auto"` for peers so they work before the agent IDs are known. Replace each `auto` with the peer's real agent ID (printed at startup and stored in `agent_id` in its data directory) so that a connection to the wrong agent is rejected.

## Remote access to an office network

A relay with a public address accepts peer connections and operator workstations. An agent inside the office dials out to the relay and acts as the exit for the internal network, so the office needs no inbound firewall rules. Operators run Mutiauk on their laptops and connect to the relay's SOCKS5 listener over TLS.

```mermaid
flowchart LR
    Laptop[Operator laptop<br/>Mutiauk] -->|SOCKS5 over WSS| Relay[Relay<br/>relay.example.com]
    Office[Office gateway] -->|QUIC, outbound| Relay
    Office --> Internal[10.0.0.0/8<br/>*.corp.example.com]
```

**Relay** (public host):

```yaml
agent:
  display_name: "Relay"
  data_dir: "/var/lib/muti-metroo"

tls:
  ca: "/etc/muti-metroo/certs/ca.crt"
  cert: "/etc/muti-metroo/certs/relay.crt"
  key: "/etc/muti-metroo/certs/relay.key"
  mtls: true

listeners:
  - transport: quic
    address: "0.0.0.0:4433"

socks5:
  enabled: true
  address: "127.0.0.1:1080"
  auth:
    enabled: true
    users:
      - username: "remote-team"
        password_hash: "${SOCKS5_PASSWORD_HASH}"
  websocket:
    enabled: true
    address: "0.0.0.0:8443"
    path: "/socks5"

http:
  enabled: true
  address: "127.0.0.1:8080"
```

**Office gateway** (inside the office network):

```yaml
agent:
  display_name: "Office Gateway"
  data_dir: "/var/lib/muti-metroo"

tls:
  ca: "/etc/muti-metroo/certs/ca.crt"
  cert: "/etc/muti-metroo/certs/office.crt"
  key: "/etc/muti-metroo/certs/office.key"

peers:
  - id: "auto"
    transport: quic
    address: "relay.example.com:4433"

exit:
  enabled: true
  routes:
    - "10.0.0.0/8"
  domain_routes:
    - "*.corp.example.com"
  dns:
    servers:
      - "10.0.0.1:53"

http:
  enabled: true
  address: "127.0.0.1:8080"
```

The relay requires client certificates (`mtls: true`) from peering agents, so only agents with a certificate from the mesh CA can peer with it. The SOCKS5 WebSocket listener does not ask for client certificates; it serves the relay's certificate and requires the SOCKS5 password, which also protects the HTTP upgrade. Generate the password hash with `muti-metroo hash`.

**Operator laptop** (`/etc/mutiauk/config.yaml`):

```yaml
tun:
  name: tun0
  address: 10.200.200.1/24

socks5:
  server: wss://relay.example.com:8443/socks5
  username: remote-team

autoroutes:
  enabled: true
```

Set the password with the `MUTIAUK_SOCKS5_PASSWORD` environment variable, or in `socks5.password`. Mutiauk checks the relay's certificate against the laptop's system trust store, so either add the mesh CA (`ca.crt`) to the laptop's trust store, or issue the relay a certificate for `relay.example.com` from a public CA. With autoroutes, Mutiauk installs the office's `10.0.0.0/8` route, and tools on the laptop reach the office directly:

```bash
sudo mutiauk daemon start
ssh admin@10.0.12.5
```

The domain route for `*.corp.example.com` applies only to clients that send host names to the SOCKS listener; Mutiauk sends IP addresses that the laptop resolved itself. For the laptop to resolve internal names, point its resolver for `corp.example.com` at the office DNS server `10.0.0.1` (reachable through the `10.0.0.0/8` route), or register the names as [mesh hostnames](/features/mesh-hostnames), which Mutiauk resolves.

SOCKS-aware applications cannot use the WebSocket listener. To use them from outside, bind `socks5.address` to a reachable address as well; plain SOCKS5 is not encrypted, so the password and the traffic up to the relay travel in clear text. With such a listener, a client that sends host names uses the domain route, and the office gateway resolves the name with the internal DNS server:

```bash
curl -x socks5h://remote-team:password@relay.example.com:1080 http://intranet.corp.example.com/
```

## Site-to-site connectivity

Each site runs a gateway that dials a central relay, exits for its own network, and offers a SOCKS listener to Mutiauk workstations and SOCKS clients at the site.

```mermaid
flowchart TB
    A["Site A gateway<br/>10.1.0.0/16"] <-->|QUIC| R[Relay]
    B["Site B gateway<br/>10.2.0.0/16"] <-->|QUIC| R
    C["Site C gateway<br/>10.3.0.0/16"] <-->|QUIC| R
```

**Relay:**

```yaml
agent:
  display_name: "Relay"
  data_dir: "/var/lib/muti-metroo"

tls:
  ca: "/etc/muti-metroo/certs/ca.crt"
  cert: "/etc/muti-metroo/certs/relay.crt"
  key: "/etc/muti-metroo/certs/relay.key"
  mtls: true

listeners:
  - transport: quic
    address: "0.0.0.0:4433"
```

**Site A gateway** (repeat for each site with its own CIDR, DNS server, and SOCKS address):

```yaml
agent:
  display_name: "Site A Gateway"
  data_dir: "/var/lib/muti-metroo"

tls:
  ca: "/etc/muti-metroo/certs/ca.crt"
  cert: "/etc/muti-metroo/certs/site-a.crt"
  key: "/etc/muti-metroo/certs/site-a.key"

peers:
  - id: "auto"
    transport: quic
    address: "relay.example.com:4433"

socks5:
  enabled: true
  address: "10.1.0.10:1080"

exit:
  enabled: true
  routes:
    - "10.1.0.0/16"
```

A workstation at Site A runs Mutiauk with `socks5.server: 10.1.0.10:1080` and autoroutes enabled. It installs the other sites' routes (`10.2.0.0/16`, `10.3.0.0/16`). Its own site's `10.1.0.0/16` route covers the gateway's address, so Mutiauk refuses that one and local traffic stays on the LAN. A connection to `10.2.0.25` matches Site B's route, travels through the relay, and Site B's gateway opens the TCP connection inside Site B. A SOCKS client does the same without Mutiauk:

```bash
curl -x socks5://10.1.0.10:1080 http://10.2.0.25/
```

The SOCKS listener here has no authentication, so bind it only to an address that untrusted hosts cannot reach, or enable `socks5.auth`.

## Internet egress through a remote exit

An agent on the user's machine provides a local SOCKS proxy and sends all traffic to an exit agent on a remote server. Here the user's machine is itself a mesh node, and applications use the local SOCKS proxy directly. Mutiauk's autoroutes never installs default routes, so this scenario sends only the applications configured for the proxy through the exit.

```mermaid
flowchart LR
    Browser[Browser or app] -->|SOCKS5 127.0.0.1:1080| Local[Local agent]
    Local -->|QUIC| Exit[Exit agent<br/>exit.example.com]
    Exit --> Internet
```

**Exit agent** (remote server):

```yaml
agent:
  display_name: "Exit"
  data_dir: "/var/lib/muti-metroo"

listeners:
  - transport: quic
    address: "0.0.0.0:4433"

exit:
  enabled: true
  routes:
    - "0.0.0.0/0"
    - "::/0"
```

**Local agent** (user's machine):

```yaml
agent:
  display_name: "Laptop"
  data_dir: "./data"

peers:
  - id: "auto"
    transport: quic
    address: "exit.example.com:4433"

socks5:
  enabled: true
  address: "127.0.0.1:1080"
```

Without `tls` settings, the exit generates a self-signed certificate at startup and the local agent does not verify it. Stream payloads are still [end-to-end encrypted](/security/e2e-encryption), but an attacker who can intercept the link can sit in the middle of it. Add a CA with strict verification and mTLS, as in the first scenario, to protect the link and restrict who can peer with the exit.

Point the browser or application at `127.0.0.1:1080`, or test with:

```bash
curl -x socks5h://127.0.0.1:1080 https://example.com/
```

With `socks5h`, the local agent resolves host names with its own system resolver before choosing a route. To resolve names at the exit instead, add the domains to the exit's `domain_routes`.

## Firewall traversal on port 443

The corporate firewall blocks UDP and every outbound TCP port except 443. The inside agent connects to a WebSocket listener on port 443 of a cloud agent, so the link looks like ordinary HTTPS.

```mermaid
flowchart LR
    subgraph Corp[Corporate network]
        Agent[Inside agent] --> FW[Firewall]
    end
    FW -->|WebSocket over HTTPS, port 443| Cloud[Cloud agent<br/>cloud.example.com]
```

**Cloud agent:**

```yaml
agent:
  display_name: "Cloud Agent"
  data_dir: "/var/lib/muti-metroo"

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
```

Listener certificates must use ECDSA keys; with Certbot, request one with `--key-type ecdsa`. Binding port 443 requires root or the `cap_net_bind_service` capability. To share port 443 with a web server instead, see [Reverse Proxy Deployment](/deployment/reverse-proxy).

**Inside agent:**

```yaml
agent:
  display_name: "Inside Agent"
  data_dir: "./data"

peers:
  - id: "auto"
    transport: ws
    address: "wss://cloud.example.com:443/mesh"

socks5:
  enabled: true
  address: "127.0.0.1:1080"
```

HTTP/2 (`transport: h2`, address `cloud.example.com:443`) works the same way. Firewalls that inspect TLS replace the server certificate, which works with the default (non-strict) certificate handling.

:::note[Networks that allow only an HTTP proxy]
If outbound traffic must go through an HTTP proxy, the inside agent cannot reach the cloud agent in the current release: peer entries accept `proxy` and `proxy_auth`, but they are not applied when dialing. See [Peers](/configuration/peers).
:::

## Rollout checklist

1. Generate a CA and one certificate per agent with [`muti-metroo cert`](/cli/cert), and the management keys if you use them.
2. Validate every config with `muti-metroo config validate <file>`.
3. Open the listener ports (UDP for QUIC, TCP for HTTP/2 and WebSocket) on the listening agents' firewalls.
4. Start the agents. Order does not matter: dialing agents retry with exponential backoff until their peers are up.
5. On the ingress agent, check peers and routes with `muti-metroo peer status` and `muti-metroo route status`.
6. Send a test request through the SOCKS listener, then connect an operator workstation with Mutiauk and check `mutiauk route list` and `mutiauk status`.
7. Note certificate expiry dates. `muti-metroo run` warns at startup when a configured certificate expires within 60 days.

## See Also

- [High Availability](/deployment/high-availability): redundant paths, exits, and ingress agents
- [Configuration Overview](/configuration/overview): every config section
- [Exit Routing](/features/exit-routing): CIDR and domain routes
- [Agent Roles](/concepts/agent-roles): ingress, transit, and exit
- [Mutiauk](/mutiauk): the operator workstation side of each scenario
- [System Service](/deployment/system-service): running agents at boot
