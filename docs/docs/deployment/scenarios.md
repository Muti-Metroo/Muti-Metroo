---
title: Deployment Scenarios
description: Example Muti Metroo topologies with complete agent configurations for remote access, site-to-site links, internet egress, and firewall traversal.
sidebar_position: 1
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-climbing.png" alt="Mole deploying" style={{maxWidth: '180px'}} />
</div>

# Deployment Scenarios

Each scenario on this page is a complete topology with the configuration for every agent. Adapt the addresses and routes, generate certificates as described in [TLS Certificates](/configuration/tls-certificates), and start the agents with `muti-metroo run -c <config>` or as a [system service](/deployment/system-service).

| Goal | Scenario |
|------|----------|
| Reach an internal network from anywhere | [Remote access to an office network](#remote-access-to-an-office-network) |
| Connect several sites | [Site-to-site connectivity](#site-to-site-connectivity) |
| Send internet traffic out through a remote host | [Internet egress through a remote exit](#internet-egress-through-a-remote-exit) |
| Connect out of a network that only allows web traffic | [Firewall traversal on port 443](#firewall-traversal-on-port-443) |
| Survive the loss of an agent | [High Availability](/deployment/high-availability) |

The examples use `id: "auto"` for peers so they work before the agent IDs are known. Replace each `auto` with the peer's real agent ID (printed at startup and stored in `agent_id` in its data directory) so that a connection to the wrong agent is rejected.

## Remote access to an office network

A relay with a public address accepts SOCKS clients and peer connections. An agent inside the office dials out to the relay and acts as the exit for the internal network, so the office needs no inbound firewall rules.

```mermaid
flowchart LR
    Laptop[Remote laptop] -->|SOCKS5| Relay[Relay<br/>relay.example.com]
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
  address: "0.0.0.0:1080"
  auth:
    enabled: true
    users:
      - username: "remote-team"
        password_hash: "${SOCKS5_PASSWORD_HASH}"

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

The relay requires client certificates (`mtls: true`), so only agents with a certificate from the mesh CA can peer with it. Generate the password hash with `muti-metroo hash`.

The domain route sends names under `corp.example.com` to the office gateway, which resolves them with the internal DNS server. Use `socks5h://` (or any client mode that passes host names to the proxy) so the name reaches the mesh unresolved:

```bash
curl -x socks5h://remote-team:password@relay.example.com:1080 http://intranet.corp.example.com/
```

For SSH, use a SOCKS client that supports username and password authentication, such as `ncat`:

```bash
ssh -o ProxyCommand='ncat --proxy relay.example.com:1080 --proxy-type socks5 --proxy-auth remote-team:password %h %p' admin@10.0.12.5
```

Exposing SOCKS on a public address relies on SOCKS authentication alone. To avoid that, run an agent on each laptop with SOCKS on `127.0.0.1:1080` and let it peer with the relay, as in [Internet egress through a remote exit](#internet-egress-through-a-remote-exit).

## Site-to-site connectivity

Each site runs a gateway that dials a central relay, exits for its own network, and offers a SOCKS listener to local clients.

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

A client at Site A uses its own gateway, `10.1.0.10:1080`, as the SOCKS proxy. A connection to `10.2.0.25` matches Site B's `10.2.0.0/16` route, travels through the relay, and Site B's gateway opens the TCP connection inside Site B.

```bash
curl -x socks5://10.1.0.10:1080 http://10.2.0.25/
```

The SOCKS listener here has no authentication, so bind it only to an address that untrusted hosts cannot reach, or enable `socks5.auth`.

## Internet egress through a remote exit

An agent on the user's machine provides a local SOCKS proxy and sends all traffic to an exit agent on a remote server.

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

Without `tls` settings, the exit generates a self-signed certificate at startup and the local agent does not verify it; the peer ID check and [end-to-end encryption](/security/e2e-encryption) protect the connection. Add a CA and mTLS as in the first scenario to restrict who can peer with the exit.

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

:::note Networks that allow only an HTTP proxy
If outbound traffic must go through an HTTP proxy, the inside agent cannot reach the cloud agent in the current release: peer entries accept `proxy` and `proxy_auth`, but they are not applied when dialing. See [Peers](/configuration/peers).
:::

## Rollout checklist

1. Generate a CA and one certificate per agent with [`muti-metroo cert`](/cli/cert), and the management keys if you use them.
2. Validate every config with `muti-metroo config validate <file>`.
3. Open the listener ports (UDP for QUIC, TCP for HTTP/2 and WebSocket) on the listening agents' firewalls.
4. Start the agents. Order does not matter: dialing agents retry with exponential backoff until their peers are up.
5. On the ingress agent, check peers and routes with `muti-metroo peer status` and `muti-metroo route status`.
6. Send a test request through the SOCKS listener.
7. Note certificate expiry dates. `muti-metroo run` warns at startup when a configured certificate expires within 60 days.

## See Also

- [High Availability](/deployment/high-availability) - Redundant paths, exits, and ingress agents
- [Configuration Overview](/configuration/overview) - Every config section
- [Exit Routing](/features/exit-routing) - CIDR and domain routes
- [Agent Roles](/concepts/agent-roles) - Ingress, transit, and exit
- [System Service](/deployment/system-service) - Running agents at boot
