---
title: Listeners
sidebar_position: 3
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-inspecting.png" alt="Mole configuring listeners" style={{maxWidth: '180px'}} />
</div>

# Listener Configuration

Let other agents connect to you. Choose QUIC for best performance, HTTP/2 or WebSocket to bypass firewalls that block UDP.

**Quick setup:**
```yaml
listeners:
  - transport: quic           # Best performance (UDP)
    address: "0.0.0.0:4433"

  - transport: h2             # Firewall-friendly (TCP/HTTPS)
    address: "0.0.0.0:443"
    path: "/mesh"
```

## Configuration

Listeners use the global TLS configuration by default:

```yaml
tls:
  ca: "./certs/ca.crt"
  cert: "./certs/agent.crt"
  key: "./certs/agent.key"
  mtls: true

listeners:
  - transport: quic             # quic, h2, ws
    address: "0.0.0.0:4433"     # Bind address
```

## Transport Types

### QUIC Listener

Best performance, UDP-based:

```yaml
tls:
  ca: "./certs/ca.crt"
  cert: "./certs/agent.crt"
  key: "./certs/agent.key"
  mtls: true

listeners:
  - transport: quic
    address: "0.0.0.0:4433"
```

### HTTP/2 Listener

TCP-based, firewall-friendly:

```yaml
listeners:
  - transport: h2
    address: "0.0.0.0:8443"
    path: "/mesh"              # Required for h2 transport
```

### WebSocket Listener

Maximum compatibility:

```yaml
listeners:
  - transport: ws
    address: "0.0.0.0:443"
    path: "/mesh"              # Required for WebSocket
```

### Plain WebSocket (Reverse Proxy)

For deployments behind a reverse proxy that handles TLS termination:

```yaml
listeners:
  - transport: ws
    address: "127.0.0.1:8080"  # Bind to localhost only
    path: "/mesh"
    plaintext: true            # No TLS - proxy handles it
```

The `plaintext` option:
- Accepts unencrypted WebSocket connections (`ws://`)
- Only available for `ws` transport
- Does not require TLS certificates
- Should only be used behind trusted reverse proxies

**Security considerations:**
- Always bind to `127.0.0.1` to prevent direct external access
- Block the plaintext port from external access via firewall
- mTLS client authentication is not available in this mode
- Peer authentication and end-to-end encryption still work

See [Reverse Proxy Deployment](/deployment/reverse-proxy) for Nginx, Caddy, and Apache configuration examples.

## Multiple Listeners

An agent can listen on multiple transports simultaneously:

```yaml
tls:
  ca: "./certs/ca.crt"
  cert: "./certs/agent.crt"
  key: "./certs/agent.key"
  mtls: true

listeners:
  # Primary: QUIC for direct connections
  - transport: quic
    address: "0.0.0.0:4433"

  # Fallback: HTTP/2 for firewall traversal
  - transport: h2
    address: "0.0.0.0:8443"
    path: "/mesh"

  # Alternative: WebSocket for maximum compatibility
  - transport: ws
    address: "0.0.0.0:443"
    path: "/mesh"
```

## TLS Configuration

### Using Global Settings

By default, listeners use the global TLS configuration:

```yaml
tls:
  ca: "./certs/ca.crt"
  cert: "./certs/agent.crt"
  key: "./certs/agent.key"
  mtls: true

listeners:
  - transport: quic
    address: "0.0.0.0:4433"
    # Uses global cert, key, and mtls setting
```

### Per-Listener Overrides

Override specific settings per listener:

```yaml
tls:
  ca: "./certs/ca.crt"
  cert: "./certs/agent.crt"
  key: "./certs/agent.key"
  mtls: true

listeners:
  # Uses global settings
  - transport: quic
    address: "0.0.0.0:4433"

  # Override: different certificate
  - transport: h2
    address: "0.0.0.0:8443"
    tls:
      cert: "./certs/public.crt"
      key: "./certs/public.key"

  # Override: disable mTLS
  - transport: ws
    address: "0.0.0.0:443"
    tls:
      mtls: false
```

### Mutual TLS (mTLS)

mTLS is controlled by the global `mtls` setting or per-listener override:

```yaml
tls:
  ca: "./certs/ca.crt"
  cert: "./certs/agent.crt"
  key: "./certs/agent.key"
  mtls: true                    # Global: require client certs

listeners:
  - transport: quic
    address: "0.0.0.0:4433"
    # mTLS enabled (from global)

  - transport: h2
    address: "0.0.0.0:8443"
    tls:
      mtls: false               # Override: no client certs required
```

With mTLS:
- Only peers with valid certificates can connect
- Provides mutual authentication
- Recommended for production

See [TLS Configuration](/configuration/tls-certificates) for details.

## Bind Address

### All Interfaces

Accept connections from anywhere:

```yaml
listeners:
  - transport: quic
    address: "0.0.0.0:4433"    # All IPv4 interfaces
```

### Specific Interface

Bind to specific IP:

```yaml
listeners:
  - transport: quic
    address: "192.168.1.10:4433"   # Specific interface
```

### IPv6

```yaml
listeners:
  - transport: quic
    address: "[::]:4433"           # All IPv6 interfaces
```

### Localhost Only

For testing or local-only access:

```yaml
listeners:
  - transport: quic
    address: "127.0.0.1:4433"      # Localhost only
```

## Port Selection

| Transport | Default Port | Alternative Ports |
|-----------|-------------|-------------------|
| QUIC | 4433 | Any UDP port |
| HTTP/2 | 8443, 443 | Any TCP port |
| WebSocket | 443, 80 | Any TCP port |

### Firewall Considerations

```yaml
# For restrictive firewalls, use HTTPS port
listeners:
  - transport: h2
    address: "0.0.0.0:443"

# Or WebSocket on standard HTTPS
listeners:
  - transport: ws
    address: "0.0.0.0:443"
    path: "/mesh"
```

## URL Path

HTTP/2 and WebSocket support URL paths:

```yaml
listeners:
  - transport: h2
    address: "0.0.0.0:443"
    path: "/mesh/v1"
```

Peers must use matching path:

```yaml
peers:
  - transport: h2
    address: "server.example.com:443"
    path: "/mesh/v1"
```

## Advertising a Listener

Add an optional `advertise:` block to publish a listener's public dial target across the mesh. Other agents can then see and reach this listener through their topology view, including agents that have never directly peered with you.

```yaml
listeners:
  - transport: h2
    address: "0.0.0.0:3000"      # Bind locally on all interfaces
    path: "/mesh"
    advertise:
      address: "kreata.ee:3000"  # Public host:port to dial
      # path defaults to listener.path; transport defaults to listener.transport
```

Without `advertise`, the listener is not propagated. This is opt-in by design: bind-only or NAT-shielded listeners must not be auto-advertised.

`advertise.address` must validate as `host:port` and may not be a loopback or unspecified address (`0.0.0.0`, `127.0.0.1`, `::`, `::1`). `advertise.path` and `advertise.transport` are only needed when a reverse proxy maps the public path or transport differently from the local listener.

## Examples

### Development

```yaml
tls:
  ca: "./certs/ca.crt"
  cert: "./certs/agent.crt"
  key: "./certs/agent.key"
  mtls: false                  # Relax for development

listeners:
  - transport: quic
    address: "127.0.0.1:4433"
```

### Production (Multi-Transport)

```yaml
tls:
  ca: "/etc/muti-metroo/certs/ca.crt"
  cert: "/etc/muti-metroo/certs/agent.crt"
  key: "/etc/muti-metroo/certs/agent.key"
  mtls: true

listeners:
  # QUIC for performance
  - transport: quic
    address: "0.0.0.0:4433"

  # HTTP/2 for TCP fallback
  - transport: h2
    address: "0.0.0.0:443"
    path: "/mesh"
```

### Containers

```yaml
tls:
  ca_pem: "${TLS_CA}"
  cert_pem: "${TLS_CERT}"
  key_pem: "${TLS_KEY}"
  mtls: true

listeners:
  - transport: quic
    address: "0.0.0.0:4433"
```

## Troubleshooting

### Port Already in Use

```bash
# Find what's using the port
lsof -i :4433
netstat -tlnp | grep 4433

# Kill the process or choose different port
```

### Permission Denied

Ports below 1024 require root on Linux:

```bash
# Option 1: Use port > 1024
address: "0.0.0.0:4433"

# Option 2: Run as root (not recommended)
sudo muti-metroo run -c config.yaml

# Option 3: Use capabilities
sudo setcap 'cap_net_bind_service=+ep' muti-metroo
```

### Certificate Errors

```bash
# Verify certificate
muti-metroo cert info ./certs/agent.crt

# Check key matches certificate
openssl x509 -noout -pubkey -in agent.crt | openssl md5
openssl ec -in agent.key -pubout 2>/dev/null | openssl md5
# (should match)
```

## Runtime management

Listeners can also be opened and closed on a running agent without editing this file, using `muti-metroo listener add/remove/list` (or the `/listeners/manage` HTTP endpoint). Dynamic listeners reuse the global TLS configuration and are persisted across restarts when `agent.data_dir` is set. See [Listener Commands](/cli/listener).

## Related

- [Listener Commands](/cli/listener) - Manage listeners at runtime
- [Peers](/configuration/peers) - Outbound connections
- [TLS Certificates](/configuration/tls-certificates) - Certificate management
- [Transports](/concepts/transports) - Transport comparison
