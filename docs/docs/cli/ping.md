---
title: ping
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-drilling.png" alt="Mole pinging" style={{maxWidth: '180px'}} />
</div>

# muti-metroo ping

Send ICMP echo (ping) requests through a remote agent. Test network connectivity and measure latency to any IP address via the mesh network.

**Quick examples:**
```bash
# Ping through a remote agent
muti-metroo ping abc123def456 8.8.8.8

# Continuous ping
muti-metroo ping -c 0 abc123def456 1.1.1.1

# Custom interval and timeout
muti-metroo ping -c 10 -i 500ms -t 3s abc123def456 8.8.8.8
```

## Synopsis

```bash
muti-metroo ping [flags] <target-agent-id> <destination>
```

## Arguments

- `<target-agent-id>`: The agent that sends the actual ICMP packets
- `<destination>`: IP address to ping (domain names are not supported)

## Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--agent` | `-a` | `localhost:8080` | Gateway agent API address (host:port or http(s):// URL) |
| `--count` | `-c` | `4` | Number of echo requests (0 = infinite) |
| `--interval` | `-i` | `1s` | Interval between requests |
| `--timeout` | `-t` | `5s` | Per-echo timeout |
| `-h, --help` | | | Show help |

## Requirements

The target agent must have ICMP enabled in its configuration (enabled by default):

```yaml
icmp:
  enabled: true
```

See [ICMP Configuration](/configuration/icmp) for details.

## Examples

### Basic Ping

```bash
muti-metroo ping abc123def456 8.8.8.8
```

Output:

```
PING 8.8.8.8 via abc123def456
Reply from 8.8.8.8: seq=1 time=23.4ms
Reply from 8.8.8.8: seq=2 time=21.8ms
Reply from 8.8.8.8: seq=3 time=22.1ms
Reply from 8.8.8.8: seq=4 time=24.0ms

--- 8.8.8.8 ping statistics ---
4 packets transmitted, 4 received, 0% packet loss
rtt min/avg/max = 21.8/22.8/24.0 ms
```

### Continuous Ping

Use `-c 0` for continuous ping until interrupted (Ctrl+C):

```bash
muti-metroo ping -c 0 abc123def456 8.8.8.8
```

### Custom Count and Interval

Send 10 pings with 500ms interval:

```bash
muti-metroo ping -c 10 -i 500ms abc123def456 8.8.8.8
```

### Via Different Gateway Agent

Connect through a specific gateway agent:

```bash
muti-metroo ping -a 192.168.1.10:8080 abc123def456 10.0.0.1
```

### Using Agent ID Prefix

You can use just the first few characters of the agent ID:

```bash
# These are equivalent if "abc" uniquely identifies the agent
muti-metroo ping abc 8.8.8.8
muti-metroo ping abc123def456789012345678 8.8.8.8
```

### Shorter Timeout for Fast Networks

```bash
muti-metroo ping -t 1s abc123def456 192.168.1.1
```

## Output Format

Each reply shows:
- **seq**: Sequence number of the echo request
- **time**: Round-trip time in milliseconds

The statistics summary shows:
- Packets transmitted and received
- Packet loss percentage
- RTT min/avg/max values

## Error Messages

| Error | Cause |
|-------|-------|
| `destination must be a valid IP address` | Use IP address, not domain name |
| `ICMP session failed: icmp not enabled` | Target agent does not have ICMP enabled |
| `timeout` | No reply within timeout period |

## How It Works

1. The CLI connects to the gateway agent's HTTP API via WebSocket
2. An ICMP session is established through the mesh to the target agent
3. The target agent sends real ICMP echo requests using unprivileged sockets
4. Replies are encrypted and relayed back through the mesh
5. All traffic between agents is E2E encrypted (transit nodes cannot see content)

## Use Cases

### Network Connectivity Testing

Verify a target network is reachable through the mesh:

```bash
muti-metroo ping target-agent-id 10.0.0.1
```

### Latency Measurement

Measure latency to a specific destination:

```bash
muti-metroo ping -c 100 target-agent-id 8.8.8.8
```

### Network Troubleshooting

Test connectivity to different points in a network:

```bash
# Test gateway
muti-metroo ping target-agent 192.168.1.1

# Test internal server
muti-metroo ping target-agent 192.168.1.100

# Test external connectivity
muti-metroo ping target-agent 8.8.8.8
```

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success (at least one reply received) |
| 1 | Error or no replies received |

## Limitations

- **IP addresses only**: Domain names must be resolved beforehand
- **Target agent requirement**: The target agent must have ICMP enabled

Both IPv4 and IPv6 destinations are supported; the agent selects ICMPv4 or ICMPv6 automatically based on the destination address.

## Related

- [ICMP Configuration](/configuration/icmp) - Configure ICMP on agents
- [Shell Command](/cli/shell) - Execute commands on remote agents
- [Probe Command](/cli/probe) - Test connectivity to listeners
