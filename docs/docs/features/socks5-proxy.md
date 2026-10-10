---
title: SOCKS Proxy
sidebar_position: 1
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-presenting.png" alt="Mole presenting SOCKS proxy" style={{maxWidth: '180px'}} />
</div>

# SOCKS Proxy

Route any TCP application through your mesh - curl, SSH, browsers, database clients, or any tool that supports SOCKS proxying. The proxy accepts SOCKS4, SOCKS4a, and SOCKS5 — auto-detected per connection. Point your application at the proxy and traffic flows through the mesh to its destination.

```bash
# These just work once your mesh is running
curl -x socks5://localhost:1080 https://internal.example.com
ssh -o ProxyCommand='nc -x localhost:1080 %h %p' user@remote-host
```

:::tip Configuration
See [SOCKS5 Configuration](/configuration/socks5) for all options including authentication, bind address, and connection limits.
:::

## Usage Examples

### cURL

```bash
# Basic usage
curl -x socks5://localhost:1080 https://example.com

# With authentication
curl -x socks5://user1:password@localhost:1080 https://example.com

# With hostname resolution at exit agent (socks5h)
curl -x socks5h://localhost:1080 https://internal.corp.local
```

:::tip socks5 vs socks5h
Use `socks5h://` (note the `h`) when the destination hostname should be resolved by the exit agent rather than locally. This is essential for accessing internal DNS names that aren't resolvable from your machine.
:::

### SSH

```bash
# Using netcat (nc)
ssh -o ProxyCommand='nc -x localhost:1080 %h %p' user@remote-host

# Using ncat
ssh -o ProxyCommand='ncat --proxy localhost:1080 --proxy-type socks5 %h %p' user@remote-host

# Add to ~/.ssh/config for permanent use
# Host internal-*
#     ProxyCommand nc -x localhost:1080 %h %p
```

### Firefox

1. Preferences -> Network Settings -> Manual proxy configuration
2. SOCKS Host: `localhost`, Port: `1080`
3. Select SOCKS v5
4. Check "Proxy DNS when using SOCKS v5" for internal DNS resolution
5. Enter username/password if authentication is enabled

### Git

```bash
# Clone via SOCKS5
git -c http.proxy=socks5://localhost:1080 clone https://github.com/repo/name

# Configure globally
git config --global http.proxy socks5://localhost:1080

# Remove global config
git config --global --unset http.proxy
```

### Python

```python
import socks
import socket

# Configure default socket to use SOCKS5
socks.set_default_proxy(socks.SOCKS5, "localhost", 1080)
socket.socket = socks.socksocket

# Now all socket connections go through the proxy
import urllib.request
urllib.request.urlopen("https://example.com")
```

Requires [PySocks](https://pypi.org/project/PySocks/): `pip install pysocks`

### proxychains

Configure `/etc/proxychains.conf` or `~/.proxychains/proxychains.conf`:

```ini
[ProxyList]
socks5 127.0.0.1 1080
```

Run any application through the proxy:

```bash
proxychains4 curl https://example.com
proxychains4 ssh admin@192.168.1.20
```

### wget

```bash
# Via environment variable
ALL_PROXY=socks5://localhost:1080 wget https://example.com/file.tar.gz

# Or in ~/.wgetrc
# use_proxy = on
# http_proxy = socks5://localhost:1080
# https_proxy = socks5://localhost:1080
```

## Verifying the Proxy

### Check Proxy is Running

```bash
# Health check
curl http://localhost:8080/healthz | jq '{socks5_running: .socks5_running}'

# Test connection
nc -zv localhost 1080
```

### Test Connectivity

```bash
# Check your exit IP
curl -x socks5://localhost:1080 https://httpbin.org/ip

# Test internal connectivity
curl -x socks5h://localhost:1080 http://internal-server/health
```

### View Active Connections

```bash
# Via health endpoint
curl http://localhost:8080/healthz | jq '.stream_count'
```

## Transparent Proxying

For applications that don't support SOCKS5, use [Mutiauk](/mutiauk) (Linux only) to create a TUN interface that transparently routes all traffic through the proxy. No per-app configuration needed.

## UDP Support

SOCKS5 UDP ASSOCIATE is supported for tunneling UDP traffic (DNS, NTP) through the mesh.

See [UDP Relay](/features/udp-relay) for details and examples.

## Troubleshooting

### Connection Refused

```
curl: (7) Failed to connect to localhost port 1080: Connection refused
```

**Causes:**

- SOCKS5 server not enabled in config
- Agent not running
- Wrong port number

**Solutions:**

```bash
# Check agent is running
curl http://localhost:8080/health

# Verify SOCKS5 is enabled
curl http://localhost:8080/healthz | jq '.socks5_running'
```

### Authentication Failed

```
curl: (7) User was rejected by the SOCKS5 server (1 1)
```

**Causes:**

- Wrong username or password
- Authentication required but not provided

**Solutions:**

- Verify credentials: `curl -x socks5://user:pass@localhost:1080 ...`
- Check if auth is enabled in config

### No Route to Host

```
curl: (7) Can't complete SOCKS5 connection to example.com:443
```

**Causes:**

- No exit agent with matching route
- Exit agent not connected to mesh
- Destination unreachable from exit

**Solutions:**

```bash
# Check routes exist
curl http://localhost:8080/healthz | jq '.route_count'

# Verify peer connections
curl http://localhost:8080/healthz | jq '.peer_count'
```

### DNS Resolution Failed

```
curl: (6) Could not resolve host: internal.corp.local
```

**Cause:** Using `socks5://` instead of `socks5h://`

**Solution:** Use `socks5h://` to resolve DNS at the exit agent:

```bash
curl -x socks5h://localhost:1080 https://internal.corp.local
```

## Related

- [Configuration - SOCKS5](/configuration/socks5) - Full configuration reference
- [UDP Relay](/features/udp-relay) - UDP tunneling through SOCKS5
- [Mutiauk](/mutiauk) - Transparent proxying via TUN interface
- [Troubleshooting](/troubleshooting/common-issues) - More troubleshooting tips
