---
title: Connectivity Troubleshooting
sidebar_position: 2
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-drilling.png" alt="Mole troubleshooting connectivity" style={{maxWidth: '180px'}} />
</div>

# Connectivity Troubleshooting

Agents not connecting? Routes not showing up? Step through these diagnostics to find the problem.

**Quick checks:**
```bash
# Is the agent healthy?
curl http://localhost:8080/healthz | jq '{peers: .peer_count, routes: .route_count}'

# Can you reach the peer?
nc -zv peer-address 4433

# Are certificates valid?
muti-metroo cert info ./certs/agent.crt
```

## Diagnostic Tools

### CLI Commands

Use the built-in CLI commands for quick diagnostics:

```bash
# Check overall agent status
muti-metroo status

# List connected peers with state and RTT
muti-metroo peer status

# View routing table with hop counts
muti-metroo route status

# Test connectivity to a listener before deployment
muti-metroo probe server.example.com:4433
muti-metroo probe --transport h2 server.example.com:443

# Ping through the mesh to test exit connectivity
muti-metroo ping abc123 8.8.8.8
```

### HTTP API

For scripting and monitoring:

```bash
# Basic health check
curl http://localhost:8080/health

# Detailed status with counts
curl http://localhost:8080/healthz | jq

# Expected output:
{
  "status": "healthy",
  "running": true,
  "peer_count": 2,
  "stream_count": 10,
  "route_count": 5
}

# List all known agents
curl http://localhost:8080/agents | jq

# Trigger route refresh
curl -X POST http://localhost:8080/routes/advertise
```

## Peer Connection Issues

### Can't Connect to Peer

**Step 1: Check network reachability**

```bash
# TCP (for HTTP/2, WebSocket)
nc -zv peer-address 4433
telnet peer-address 4433

# UDP (for QUIC)
nc -zvu peer-address 4433
```

**Step 2: Check DNS resolution**

```bash
dig peer-hostname
nslookup peer-hostname
```

**Step 3: Check firewall**

```bash
# On peer host
sudo iptables -L -n | grep 4433
sudo ufw status

# Try from another host on same network
curl http://peer-address:8080/health
```

**Step 4: Check TLS**

```bash
# Test TLS connection
openssl s_client -connect peer-address:4433 -CAfile ca.crt

# Verify certificate
muti-metroo cert info ./certs/agent.crt
```

### Peer Disconnects Frequently

**Check keepalive settings:**

```yaml
connections:
  idle_threshold: 30s    # Send keepalive after 30s idle
  timeout: 90s           # Disconnect after 90s no response
```

If network is slow, increase timeout:

```yaml
connections:
  idle_threshold: 60s
  timeout: 180s
```

**Check logs for disconnect reasons:**

```bash
journalctl -u muti-metroo | grep -i "disconnect\|timeout"
```

### Slow Reconnection

Tune reconnection backoff:

```yaml
connections:
  reconnect:
    initial_delay: 500ms  # Start faster
    max_delay: 30s        # Cap sooner
    multiplier: 1.5       # Slower backoff
    jitter: 0.3           # More randomization
```

## Transport-Specific Issues

### QUIC Not Working

QUIC uses UDP, which may be blocked or throttled.

**Test UDP connectivity:**

```bash
# From client
echo "test" | nc -u peer-address 4433

# On server, check if receiving
tcpdump -i any udp port 4433
```

**Common issues:**
- Corporate firewalls block UDP
- NAT devices timeout UDP quickly
- Some ISPs throttle UDP

**Solution:** Fall back to HTTP/2 or WebSocket:

```yaml
peers:
  - id: "..."
    transport: h2    # Instead of quic
    address: "peer-address:443"
```

### HTTP/2 Not Working

**Test HTTP/2:**

```bash
curl -v --http2 https://peer-address:8443/mesh
```

**Check TLS:**

```bash
openssl s_client -connect peer-address:8443 -alpn h2
```

### WebSocket Through Proxy

**Test proxy connectivity:**

```bash
# Test CONNECT through proxy
curl -v --proxy http://proxy:8080 https://peer-address:443/

# Check if proxy allows WebSocket upgrade
curl -v --proxy http://proxy:8080 \
  -H "Upgrade: websocket" \
  -H "Connection: Upgrade" \
  https://peer-address:443/mesh
```

**Configure proxy authentication:**

```yaml
peers:
  - transport: ws
    address: "wss://peer-address:443/mesh"
    proxy: "http://proxy:8080"
    proxy_auth:
      username: "${PROXY_USER}"
      password: "${PROXY_PASS}"
```

## Routing Issues

### No Route Found

```
Error: no route to 10.0.0.5
```

**Start with `route trace`** -- it runs the live selection logic against a destination and explains which route (if any) would win:

```bash
muti-metroo route trace 10.0.0.5
muti-metroo route trace 10.0.0.5 --target abc123
```

The output lists every matching CIDR with the reason it won or lost. No matches at all usually means the route has not reached this agent yet or has expired; in that case work through the steps below.

**Step 1: Check if route should exist**

```bash
# On exit agent
grep -A5 "exit:" /etc/muti-metroo/config.yaml
```

**Step 2: Check route propagation**

```bash
# On ingress agent - using CLI
muti-metroo route status

# Or using HTTP API
curl http://localhost:8080/healthz | jq '.route_count'
```

**Step 3: Check peer connectivity**

Routes propagate through peers. If peer is disconnected, routes are lost.

```bash
# Using CLI
muti-metroo peer status

# Or using HTTP API
curl http://localhost:8080/healthz | jq '.peer_count'
```

**Step 4: Trigger route advertisement**

```bash
curl -X POST http://exit-agent:8080/routes/advertise
```

**Step 5: Wait for propagation**

Routes take time to propagate (up to `advertise_interval`).

### Route Expired

Routes expire after `route_ttl` without refresh.

```bash
# Check route TTL
grep route_ttl config.yaml

# If exit disconnected for too long, routes expire
# Reconnect exit and trigger advertisement
```

### Wrong Route Selected

Routes are selected by:
1. Longest prefix match
2. Lowest metric (hop count) if tied

**Debug route selection:**

```bash
# Explain which CIDR would win for a given destination
muti-metroo route trace 1.2.3.4

# Enable debug logging
muti-metroo run --log-level debug

# Look for route lookup logs
grep "route lookup" logs
```

## Stream Issues

### Streams Not Opening

```
Error: stream open timeout
```

**Causes:**
- Network latency too high
- Too many hops
- Exit agent overloaded

**Solutions:**

1. Increase timeout:
   ```yaml
   limits:
     stream_open_timeout: 60s
   ```

2. Check each hop is responsive

3. Reduce hop count if possible

### Streams Dying

**Check logs for stream issues:**

```bash
journalctl -u muti-metroo | grep -i "stream"
```

**Common causes:**
- Idle timeout
- Buffer exhaustion
- Network issues

## Network Diagnostics

### Capture Traffic

```bash
# QUIC (UDP)
tcpdump -i any udp port 4433 -w capture.pcap

# HTTP/2, WebSocket (TCP)
tcpdump -i any tcp port 443 -w capture.pcap
```

### Monitor Connections

```bash
# Watch connection states
watch -n 1 'netstat -an | grep 4433'

# Count connections
netstat -an | grep 4433 | wc -l
```

### Latency Testing

```bash
# Measure round-trip time
ping peer-address

# Measure TCP latency
hping3 -S -p 443 peer-address

# Time a stream open
time curl -x socks5://localhost:1080 https://example.com -o /dev/null
```

## Checklist

- [ ] Network reachable (ping, nc, telnet)
- [ ] Firewall allows traffic
- [ ] DNS resolves correctly
- [ ] TLS certificates valid
- [ ] Peer ID matches
- [ ] Routes advertised
- [ ] Logs show no errors

## See Also

- [CLI - Status](/cli/status) - Check agent status
- [CLI - Peers](/cli/peers) - List connected peers
- [CLI - Routes](/cli/routes) - View routing table
- [CLI - Probe](/cli/probe) - Test listener connectivity
- [CLI - Ping](/cli/ping) - Test network reachability through mesh
- [API - Health](/api/health) - Health check endpoints

## Next Steps

- [Performance Troubleshooting](/troubleshooting/performance)
- [Common Issues](/troubleshooting/common-issues)
