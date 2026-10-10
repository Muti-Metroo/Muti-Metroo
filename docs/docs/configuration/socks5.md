---
title: SOCKS Proxy
sidebar_position: 5
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-inspecting.png" alt="Mole configuring SOCKS5" style={{maxWidth: '180px'}} />
</div>

# SOCKS Proxy Configuration

Route your applications through the mesh. Point any SOCKS-compatible app at this proxy and traffic flows through your agents to the exit.

The proxy auto-detects **SOCKS4, SOCKS4a, and SOCKS5** from the first byte of each connection — no separate port or config needed. Older tools that only speak SOCKS4 or SOCKS4a work on the same address as SOCKS5 clients.

**Quick setup:**

```yaml
socks5:
  enabled: true
  address: "127.0.0.1:1080" # localhost only, no auth needed


# Then use it:
# curl -x socks5://localhost:1080 https://example.com
# curl -x socks4a://localhost:1080 https://example.com
```

## Configuration

```yaml
socks5:
  enabled: true
  address: "127.0.0.1:1080"
  auth:
    enabled: false
    users: []
  max_connections: 1000
```

## Options

| Option            | Type   | Default          | Description                    |
| ----------------- | ------ | ---------------- | ------------------------------ |
| `enabled`         | bool   | false            | Enable SOCKS5 server           |
| `address`         | string | "127.0.0.1:1080" | Bind address                   |
| `auth.enabled`    | bool   | false            | Require authentication         |
| `auth.users`      | array  | []               | User credentials               |
| `max_connections` | int    | 1000             | Maximum concurrent connections |

## Basic Configuration

### Local Access Only

```yaml
socks5:
  enabled: true
  address: "127.0.0.1:1080"
```

### Network Access

```yaml
socks5:
  enabled: true
  address: "0.0.0.0:1080" # Accept from any IPv4 interface
```

:::warning
Exposing SOCKS5 on 0.0.0.0 without authentication allows anyone to use your proxy.
:::

### IPv6 Access

```yaml
socks5:
  enabled: true
  address: "[::1]:1080" # IPv6 localhost only
```

Or accept from all IPv6 interfaces:

```yaml
socks5:
  enabled: true
  address: "[::]:1080" # All IPv6 interfaces
```

:::tip
SOCKS5 clients can connect to IPv6 destinations regardless of which address family the server binds to. The destination address family is independent of the listener address.
:::

## Authentication

### No Authentication

```yaml
socks5:
  enabled: true
  address: "127.0.0.1:1080"
  auth:
    enabled: false
```

### Username/Password Authentication

```yaml
socks5:
  enabled: true
  address: "0.0.0.0:1080"
  auth:
    enabled: true
    users:
      - username: "user1"
        password_hash: "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy"
      - username: "user2"
        password_hash: "$2a$10$..."
```

### Generating Password Hash

Use the built-in CLI command (recommended):

```bash
# Interactive (recommended - password hidden)
muti-metroo hash

# Or provide password as argument
muti-metroo hash "yourpassword"

# With custom cost factor
muti-metroo hash --cost 12
```

See [CLI - hash](/cli/hash) for full documentation.

#### Alternative Methods

Using htpasswd:

```bash
htpasswd -bnBC 10 "" yourpassword | tr -d ':\n'
```

Using Python:

```python
import bcrypt
print(bcrypt.hashpw(b"yourpassword", bcrypt.gensalt(10)).decode())
```

### Plaintext Passwords (Development Only)

```yaml
socks5:
  auth:
    users:
      - username: "dev"
        password: "devpass" # NOT for production!
```

:::danger
Never use plaintext passwords in production. Always use bcrypt hashes.
:::

## Connection Limits

```yaml
socks5:
  max_connections: 1000 # Maximum concurrent SOCKS5 connections
```

When limit is reached:

- New connections are rejected
- Existing connections continue working

## WebSocket Transport

Enable SOCKS5 over WebSocket for environments where raw TCP/SOCKS5 is blocked but HTTPS/WebSocket is permitted.

### Configuration

```yaml
socks5:
  enabled: true
  address: "127.0.0.1:1080" # TCP listener (still works)

  websocket:
    enabled: true
    address: "0.0.0.0:8443" # WebSocket listener address
    path: "/socks5" # WebSocket upgrade path
    plaintext: false # TLS mode (true for reverse proxy)
```

### Options

| Option                | Type   | Default   | Description                          |
| --------------------- | ------ | --------- | ------------------------------------ |
| `websocket.enabled`   | bool   | false     | Enable WebSocket listener            |
| `websocket.address`   | string | -         | Listen address (required if enabled) |
| `websocket.path`      | string | "/socks5" | WebSocket upgrade path               |
| `websocket.plaintext` | bool   | false     | Disable TLS (for reverse proxy)      |

### Deployment Modes

**Direct TLS (default):**

```yaml
socks5:
  websocket:
    enabled: true
    address: "0.0.0.0:8443"
    plaintext: false # Uses agent's TLS certificate
```

**Behind Reverse Proxy:**

```yaml
socks5:
  websocket:
    enabled: true
    address: "127.0.0.1:8081" # Internal only
    plaintext: true # Proxy handles TLS termination
```

### Authentication

When SOCKS5 authentication is enabled, the WebSocket endpoint automatically requires HTTP Basic Auth using the same credentials. This provides an additional security layer before the WebSocket upgrade.

```yaml
socks5:
  enabled: true
  address: "127.0.0.1:1080"
  auth:
    enabled: true
    users:
      - username: "user1"
        password_hash: "$2a$10$..."
  websocket:
    enabled: true
    address: "0.0.0.0:8443"
```

Clients must include the `Authorization` header with Basic Auth credentials:

```
Authorization: Basic base64(username:password)
```

:::tip
The WebSocket endpoint uses the same credential store as the SOCKS5 server. Configure users once in `socks5.auth.users` and they work for both TCP and WebSocket connections.
:::

### Client Configuration

Connect using WebSocket-capable SOCKS5 clients:

```yaml
# Mutiauk config with WebSocket transport
socks5:
  server: "wss://proxy.example.com:8443/socks5"
  transport: websocket
  # Same credentials for HTTP Basic Auth and SOCKS5 auth
  username: "user1"
  password: "yourpassword"
```

When SOCKS5 authentication is enabled, Mutiauk automatically:

1. Sends HTTP Basic Auth credentials during the WebSocket handshake
2. Performs SOCKS5 username/password authentication after connection

:::info
The WebSocket endpoint serves a plain splash page at `/`. Only the configured path (`/socks5` by default) accepts WebSocket upgrades.
:::

## UDP Relay Binding

When a client requests UDP ASSOCIATE, the server creates a UDP relay socket for that session. For security, the UDP relay socket binds to the **same IP address** as the SOCKS5 TCP listener.

| SOCKS5 Address      | UDP Relay Binds To      |
| ------------------- | ----------------------- |
| `127.0.0.1:1080`    | `127.0.0.1:<random>`    |
| `0.0.0.0:1080`      | `0.0.0.0:<random>`      |
| `192.168.1.10:1080` | `192.168.1.10:<random>` |

This ensures that if SOCKS5 is configured for localhost-only access, the UDP relay is also restricted to localhost.

:::tip
To restrict UDP relay to localhost only, configure SOCKS5 to bind to `127.0.0.1` instead of `0.0.0.0`.
:::

## Client Configuration

### curl

```bash
# SOCKS5 (recommended)
curl -x socks5://localhost:1080 https://example.com

# SOCKS5h (resolve DNS through proxy)
curl -x socks5h://localhost:1080 https://example.com

# SOCKS4a (domain resolved at proxy)
curl -x socks4a://localhost:1080 https://example.com

# With authentication (SOCKS5 only)
curl -x socks5://user1:password@localhost:1080 https://example.com
```

### SSH

```bash
ssh -o ProxyCommand='nc -x localhost:1080 %h %p' user@remote-host
```

Or in `~/.ssh/config`:

```
Host remote-host
  ProxyCommand nc -x localhost:1080 %h %p
  User myuser
```

### Firefox

1. Settings → Network Settings
2. Manual proxy configuration
3. SOCKS Host: localhost, Port: 1080
4. SOCKS v5
5. If auth enabled, enter credentials when prompted

### Chrome

```bash
# Linux/macOS
google-chrome --proxy-server="socks5://localhost:1080"

# Windows
chrome.exe --proxy-server="socks5://localhost:1080"
```

### Git

```bash
git config --global http.proxy socks5://localhost:1080
```

## Protocol Support

The proxy listener auto-detects the protocol version from the first byte of each connection. No separate ports or configuration are needed.

### SOCKS4 / SOCKS4a

| Feature                | Supported                    |
| ---------------------- | ---------------------------- |
| CONNECT command        | Yes                          |
| BIND command           | No                           |
| IPv4 addresses         | Yes                          |
| Domain names (SOCKS4a) | Yes                          |
| Authentication         | No (userid field is ignored) |

SOCKS4a uses the magic IP `0.0.0.x` to signal that a domain name follows the userid. The domain is resolved at the ingress agent, the same as SOCKS5 domain mode.

:::note
SOCKS4 and SOCKS4a have no authentication mechanism. If your configuration
enables authentication (`auth.enabled: true`), SOCKS4/4a connections are
rejected automatically — only SOCKS5 clients can supply credentials.
:::

### SOCKS5

| Feature           | Supported |
| ----------------- | --------- |
| CONNECT command   | Yes       |
| BIND command      | No        |
| UDP ASSOCIATE     | Yes       |
| No Authentication | Yes       |
| Username/Password | Yes       |
| IPv4 addresses    | Yes       |
| IPv6 addresses    | Yes       |
| Domain names      | Yes       |

See [UDP Relay](/features/udp-relay) for UDP ASSOCIATE configuration and usage.

### Hostname Resolution Order

When a SOCKS5 (or SOCKS4a) client sends a domain name, the ingress agent resolves it locally before route lookup. The resolution order is:

1. **Domain route table** - if a domain pattern matches (`*.example.com`, etc.), the connection is routed by name and the exit agent does the DNS work.
2. **Mesh-host registry** - the local agent's [mesh hostname registry](/configuration/hosts) (`mesh_hosts:` config seed plus runtime entries from `/hosts/manage`). A registered name resolves directly to the configured IP without consulting the system resolver.
3. **System DNS** - the host's normal resolver, with a short deadline.

Step 2 means SOCKS5 clients can use mesh-local names like `gateway.mesh` even when the agent's host has no Mutiauk forwarding the suffix to a local DNS server. The same precedence applies to UDP ASSOCIATE domain destinations.

:::tip ICMP Support
Muti Metroo supports ICMP echo (ping) through a custom SOCKS5 extension. ICMP is handled separately from the standard SOCKS5 proxy and requires configuration on the exit agent. See [ICMP Configuration](/configuration/icmp) and the [ping CLI command](/cli/ping) for details.
:::

## Examples

### Development

```yaml
socks5:
  enabled: true
  address: "127.0.0.1:1080"
```

### Team Shared Proxy

```yaml
socks5:
  enabled: true
  address: "0.0.0.0:1080"
  auth:
    enabled: true
    users:
      - username: "team"
        password_hash: "$2a$10$..."
  max_connections: 5000
```

### High-Security

```yaml
socks5:
  enabled: true
  address: "127.0.0.1:1080" # Localhost only
  auth:
    enabled: true
    users:
      - username: "admin"
        password_hash: "$2a$12$..." # Cost factor 12
  max_connections: 100
```

## Troubleshooting

### Connection Refused

```bash
# Check SOCKS5 is enabled
grep -A5 "socks5:" config.yaml

# Check process is listening
lsof -i :1080
netstat -tlnp | grep 1080
```

### Authentication Failed

```bash
# Verify password hash
# Hash should start with $2a$ or $2b$

# Try with curl
curl -v -x socks5://user:pass@localhost:1080 https://example.com
```

### No Route to Host

- Check exit agent is connected
- Verify routes are advertised
- Check routing table: `curl http://localhost:8080/healthz`

See [Troubleshooting](/troubleshooting/common-issues) for more help.

## Related

- [Features: SOCKS5 Proxy](/features/socks5-proxy) - Detailed usage
- [Exit Configuration](/configuration/exit) - Route configuration
- [Security](/security/authentication) - Authentication best practices
