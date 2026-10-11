# SOCKS5 Proxy

Muti Metroo provides SOCKS5 proxy ingress for client connections, supporting both TCP (CONNECT) and UDP (UDP ASSOCIATE) per RFC 1928.

## Configuration

```yaml
socks5:
  enabled: true
  address: "127.0.0.1:1080"
  auth:
    enabled: true
    users:
      - username: "user1"
        password_hash: "$2a$10$..."
  max_connections: 1000
```

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
  address: "127.0.0.1:1080"
  auth:
    enabled: true
    users:
      - username: "operator1"
        password_hash: "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy"
      - username: "operator2"
        password_hash: "$2a$10$..."
```

Generate a password hash (`muti-metroo hash` defaults to bcrypt cost 12 per
OWASP 2024 -- pass `--cost N` to override):

```bash
muti-metroo hash
Enter password:
Confirm password:
$2a$12$...
```

## Usage Examples

### cURL

```bash
# No auth
curl -x socks5://localhost:1080 https://example.com

# With auth
curl -x socks5://user1:password@localhost:1080 https://example.com

# With hostname resolution at proxy (socks5h)
curl -x socks5h://localhost:1080 https://example.com
```

### SSH

```bash
# Using netcat
ssh -o ProxyCommand='nc -x localhost:1080 %h %p' user@remote-host

# Using ncat
ssh -o ProxyCommand='ncat --proxy localhost:1080 --proxy-type socks5 %h %p' user@remote-host
```

### Firefox

1. Open Preferences -> Network Settings -> Manual proxy configuration
2. SOCKS Host: `localhost`, Port: `1080`
3. Select SOCKS v5
4. Enter username/password if authentication is enabled

### Git

```bash
# Clone via SOCKS5
git -c http.proxy=socks5://localhost:1080 clone https://github.com/repo/name

# Configure globally
git config --global http.proxy socks5://localhost:1080
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

Enable SOCKS5 over WebSocket for environments where raw TCP/SOCKS5 is blocked but HTTPS/WebSocket is permitted:

```yaml
socks5:
  enabled: true
  address: "127.0.0.1:1080" # TCP listener (still works)
  auth:
    enabled: true
    users:
      - username: "user1"
        password_hash: "$2a$10$..."
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

**Authentication:** When `auth.enabled` is true, the WebSocket endpoint requires HTTP Basic Auth using the same credentials. Clients like Mutiauk automatically send credentials for both HTTP Basic Auth and SOCKS5 authentication.

## UDP Support

For UDP traffic tunneling (DNS, NTP, etc.), see [UDP Relay](14-udp-relay.md). UDP relay uses SOCKS5 UDP ASSOCIATE and requires configuration on the exit agent.

## Security Considerations

1. **Bind to localhost** when possible to prevent unauthorized access
2. **Enable authentication** when binding to network interfaces
3. **Use strong passwords** with bcrypt hashing
4. **Monitor connections** via HTTP API health endpoints
5. **Limit max_connections** to prevent DoS

## Alternative: TUN Interface

For transparent proxying without application configuration, see [TUN Interface (Mutiauk)](10-tun-interface.md) (Linux only). The TUN interface captures all IP traffic, enabling any application to use the mesh without SOCKS5 support.
