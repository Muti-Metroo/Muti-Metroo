# SOCKS5 Proxy

The SOCKS5 listener is the ingress endpoint of the mesh. An agent with `socks5.enabled: true` is an ingress agent: every connection it accepts on this listener is routed through the mesh to the exit agent that advertises the destination. The listener supports TCP (CONNECT) and UDP (UDP ASSOCIATE) per RFC 1928, and also accepts SOCKS4 and SOCKS4a clients (the version is detected per connection).

Two kinds of clients connect to the listener:

- **Mutiauk** (the usual case): runs on the operator workstation, captures IP traffic for mesh routes on a TUN interface, and forwards it to the ingress agent. With autoroutes enabled it also receives routes, mesh hostnames, and DNS suffixes from the agent over the same endpoint. See Chapter 10.
- **SOCKS-aware applications** pointed at the listener directly: `curl`, `proxychains`, browser proxy settings, `ssh` with a `ProxyCommand`. This is the secondary path. It suits quick tests, a single application, machines where root or Administrator is not available, and domain-based exit routes (Mutiauk only sees IP addresses, so domain routes need a SOCKS client that sends the hostname). See "Direct SOCKS Clients" below.

## Where the Ingress Runs

The ingress agent is usually a mesh node, for example a relay on a public server that operator workstations connect to. In that case the listener is reachable over the network, and these settings matter:

- **Enable authentication.** The listener carries traffic into the mesh, and authenticated clients also receive the route and mesh-host snapshots used by Mutiauk autoroutes.
- **Encrypt the client leg.** Plain SOCKS5 over TCP is not encrypted between the client and the ingress agent, including the username and password. Mesh encryption starts at the ingress agent. Prefer the SOCKS5 WebSocket listener with TLS (see "WebSocket Transport"), or reach the ingress over a network you already trust (for example a VPN or an SSH tunnel).

For a single operator, the ingress can also run on the workstation itself, bound to `127.0.0.1`.

## Configuration

```yaml
socks5:
  enabled: true
  address: "127.0.0.1:1080"
  auth:
    enabled: true
    users:
      - username: "user1"
        password_hash: "$2a$12$..."
  max_connections: 1000
```

## Authentication

### No Authentication

Use only for listeners bound to `127.0.0.1` or reachable only from a trusted network:

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
  address: "127.0.0.1:1080"
  auth:
    enabled: true
    users:
      - username: "operator1"
        password_hash: "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy"
      - username: "operator2"
        password_hash: "$2a$12$..."
```

Generate a password hash (`muti-metroo hash` defaults to bcrypt cost 12 per OWASP 2024; pass `--cost N` to override):

```bash
muti-metroo hash
Enter password:
Confirm password:
$2a$12$...
```

Mutiauk sends the matching plain-text password from its `socks5.username` and `socks5.password` settings (or the `MUTIAUK_SOCKS5_PASSWORD` environment variable).

## Bind Address Options

| Address             | Access                   |
| ------------------- | ------------------------ |
| `127.0.0.1:1080`    | Local only (most secure) |
| `0.0.0.0:1080`      | All network interfaces   |
| `192.168.1.10:1080` | Specific interface       |

## Connection Limits

Limit concurrent connections to prevent resource exhaustion:

```yaml
socks5:
  max_connections: 1000 # Maximum concurrent connections
```

## WebSocket Transport

The SOCKS5 WebSocket listener carries the SOCKS5 protocol inside a WebSocket connection. With TLS (the default) it encrypts the leg between the client and the ingress agent, and it passes through networks that allow HTTPS but block raw TCP. This is the recommended way for Mutiauk on a workstation to reach a remote ingress agent.

```yaml
tls:
  cert: "./certs/relay.crt" # served by the WebSocket listener
  key: "./certs/relay.key"

socks5:
  enabled: true
  address: "127.0.0.1:1080" # TCP listener (still works)
  auth:
    enabled: true
    users:
      - username: "user1"
        password_hash: "$2a$12$..."
  websocket:
    enabled: true
    address: "0.0.0.0:8443" # WebSocket listener
    path: "/socks5"
    plaintext: false # TLS (true for reverse proxy)
```

| Option                | Default   | Description                          |
| --------------------- | --------- | ------------------------------------ |
| `websocket.enabled`   | false     | Enable WebSocket listener            |
| `websocket.address`   | -         | Listen address (required if enabled) |
| `websocket.path`      | "/socks5" | WebSocket upgrade path               |
| `websocket.plaintext` | false     | Disable TLS (for reverse proxy)      |

The matching Mutiauk setting is `socks5.server: wss://relay.example.com:8443/socks5`.

**Certificate:** The WebSocket listener uses the agent's global `tls.cert` / `tls.key` (or `cert_pem` / `key_pem`). The certificate must be ECDSA. When no certificate is configured, the agent generates a self-signed certificate at startup. Mutiauk verifies the server certificate against the workstation's system trust store and has no option to skip verification, so a self-signed certificate does not work. Use one of these:

- A certificate from a public CA for the relay's hostname (ECDSA key).
- A certificate from your own CA (for example `muti-metroo cert ca` and `muti-metroo cert agent --dns relay.example.com`) with that CA installed in the workstation's system trust store.
- `plaintext: true` behind a reverse proxy (nginx, Caddy) that terminates TLS with a trusted certificate and forwards the WebSocket upgrade to the agent.

**Authentication:** When `auth.enabled` is true, the WebSocket endpoint requires HTTP Basic Auth using the same credentials, followed by the normal SOCKS5 username/password exchange. Mutiauk sends both from the same `username` and `password`.

## Direct SOCKS Clients

Any SOCKS-aware application can use the listener directly. The examples below assume an agent on the same host at `localhost:1080`; for a remote ingress, use its address instead (and keep in mind that plain SOCKS5 over TCP is not encrypted on the way to the agent).

Use a client mode that sends the hostname to the agent (`socks5h://` in curl) when the destination is covered by a domain-based exit route. With `socks5://`, curl resolves the name locally and sends only the IP address.

### cURL

```bash
# No auth
curl -x socks5h://localhost:1080 https://example.com

# With auth
curl -x socks5h://user1:password@localhost:1080 https://example.com

# Resolve the hostname locally and send only the IP address
curl -x socks5://localhost:1080 https://example.com
```

### SSH

```bash
# Using netcat
ssh -o ProxyCommand='nc -x localhost:1080 %h %p' user@remote-host

# Using ncat
ssh -o ProxyCommand='ncat --proxy localhost:1080 --proxy-type socks5 %h %p' user@remote-host
```

### Firefox

1. Open Settings -> Network Settings -> Manual proxy configuration
2. SOCKS Host: `localhost`, Port: `1080`
3. Select SOCKS v5
4. Enable "Proxy DNS when using SOCKS v5" to send hostnames to the agent
5. Enter username/password if authentication is enabled

### Git

```bash
# Clone via SOCKS5
git -c http.proxy=socks5h://localhost:1080 clone https://github.com/repo/name

# Configure globally
git config --global http.proxy socks5h://localhost:1080
```

### Python

```python
import socks
import socket

socks.set_default_proxy(socks.SOCKS5, "localhost", 1080)
socket.socket = socks.socksocket
```

### proxychains

Configure `/etc/proxychains.conf`:

```
[ProxyList]
socks5 127.0.0.1 1080
```

Use any application through the proxy:

```bash
proxychains4 curl https://example.com
proxychains4 ssh admin@target
```

## UDP Support

For UDP traffic tunneling (DNS, NTP, etc.), see Chapter 14 (UDP Relay). UDP relay uses SOCKS5 UDP ASSOCIATE and requires configuration on the exit agent. Mutiauk uses UDP ASSOCIATE for UDP traffic captured on its TUN interface.

## Security Considerations

1. **Bind to localhost** when the clients run on the same host
2. **Enable authentication** whenever the listener is reachable from a network
3. **Use the WebSocket listener with TLS** for clients on other hosts, with a certificate the clients trust
4. **Use strong passwords** with bcrypt hashing
5. **Monitor connections** via HTTP API health endpoints
6. **Limit max_connections** to prevent DoS
