# ICMP Relay

ICMP relay enables ping functionality through the mesh network. Test connectivity, measure latency, and diagnose network issues through your encrypted tunnel.

## Configuration

Configure on the **target** agent:

```yaml
icmp:
  enabled: true              # Enabled by default
  max_sessions: 100          # Max concurrent sessions
  idle_timeout: 60s          # Session cleanup timeout
  echo_timeout: 5s           # Per-request timeout
```

### Configuration Options

| Option | Default | Description |
|--------|---------|-------------|
| `enabled` | `true` | Enable ICMP echo forwarding |
| `max_sessions` | `100` | Max concurrent sessions (0 = unlimited) |
| `idle_timeout` | `60s` | Session cleanup after inactivity |
| `echo_timeout` | `5s` | Timeout for individual echo requests |

## Platform Support

ICMP uses unprivileged sockets. Support varies by platform:

| Platform | Supported | Notes |
|----------|-----------|-------|
| **Linux** | Yes | Requires `ping_group_range` sysctl |
| **macOS** | Yes | Works without configuration |
| **Windows** | No | Unprivileged ICMP not supported |

**Important:** ICMP relay does not work on Windows agents. Use Linux or macOS.

### Linux Configuration

On Linux, unprivileged ICMP sockets require kernel configuration:

```bash
# Check current setting (default is usually "1 0" = disabled)
sysctl net.ipv4.ping_group_range

# Enable for all groups (run as root)
sudo sysctl -w net.ipv4.ping_group_range="0 65535"

# Make persistent
echo "net.ipv4.ping_group_range=0 65535" | sudo tee -a /etc/sysctl.conf
```

Without this setting, ICMP will fail with "socket: operation not permitted".

## Usage

### CLI Ping Command

```bash
# Basic ping (requires target agent ID and destination IP)
muti-metroo ping abc123def456 8.8.8.8

# Output:
# PING 8.8.8.8 via agent abc123def456
# 64 bytes from 8.8.8.8: seq=1 time=12.3ms
# 64 bytes from 8.8.8.8: seq=2 time=11.8ms

# Continuous ping (Ctrl+C to stop)
muti-metroo ping abc123def456 -c 0 192.168.1.1

# IPv6 ping
muti-metroo ping abc123def456 2001:4860:4860::8888

# Ping via specific agent's API
muti-metroo ping -a 192.168.1.10:8080 abc123def456 8.8.8.8
```

### Mutiauk (TUN Interface)

When using Mutiauk, standard ping works transparently:

```bash
# Standard ping through TUN interface
ping 10.10.5.100

# Ping with count
ping -c 4 192.168.50.1
```

**Note:** Mutiauk forwards ICMP echo requests via a custom SOCKS5 extension. Only echo (ping) is supported - other ICMP types (port unreachable, TTL exceeded) are not forwarded, so traceroute does not work.

## IPv6 Support

ICMP supports both IPv4 and IPv6 addresses automatically:

| Version | Protocol | Echo Request | Echo Reply |
|---------|----------|--------------|------------|
| IPv4 | ICMPv4 (1) | Type 8 | Type 0 |
| IPv6 | ICMPv6 (58) | Type 128 | Type 129 |

The agent detects the IP version from the destination address.

## Full Example

### Exit Agent Configuration

```yaml
agent:
  display_name: "Exit with ICMP"

listeners:
  - transport: quic
    address: "0.0.0.0:4433"

exit:
  enabled: true
  routes:
    - "0.0.0.0/0"

icmp:
  enabled: true
  max_sessions: 100
  idle_timeout: 60s
```

### Ingress Agent Configuration

```yaml
agent:
  display_name: "Ingress"

peers:
  - id: "exit-agent-id..."
    transport: quic
    address: "exit.example.com:4433"

socks5:
  enabled: true
  address: "127.0.0.1:1080"
```

## Limitations

- **Echo only**: Only ICMP echo (ping) is supported
- **No traceroute**: TTL-exceeded messages not forwarded
- **IP addresses only**: No DNS resolution (use IP addresses)
- **Windows**: Agents on Windows cannot forward ICMP

## Security Considerations

1. **E2E encryption**: All ICMP data encrypted through the mesh
2. **Session limits**: Use `max_sessions` to prevent resource exhaustion
3. **No DNS**: Only IP addresses accepted (no domain names)

## Troubleshooting

### Permission Denied (Linux)

```
Error: socket: operation not permitted
```

Configure the `ping_group_range` sysctl:

```bash
sudo sysctl -w net.ipv4.ping_group_range="0 65535"
```

### ICMP Not Enabled

```
Error: ICMP not enabled
```

1. Verify the target agent has `icmp.enabled: true`
2. Check that a route exists to the target agent
3. Ensure the target agent is connected to the mesh

### Session Timeout

Sessions expire after `idle_timeout` (default 60 seconds) of inactivity. Each ping request resets the timer.
