---
title: ICMP Relay
sidebar_position: 7
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-surfacing.png" alt="Mole with ICMP" style={{maxWidth: '180px'}} />
</div>

# ICMP Relay

Ping remote hosts through your mesh network. Test connectivity, measure latency, and diagnose network issues - all through your encrypted tunnel.

```bash
# Ping through the mesh (requires target agent ID and destination)
muti-metroo ping <target-agent-id> 8.8.8.8

# Continuous ping
muti-metroo ping <target-agent-id> -c 0 192.168.1.1

# IPv6 ping
muti-metroo ping <target-agent-id> 2001:4860:4860::8888
```

## How It Works

ICMP echo requests flow through your mesh just like TCP and UDP - encrypted end-to-end from ingress to exit.

```mermaid
flowchart LR
    A[CLI Client] -->|ICMP_OPEN| B[Ingress Agent]
    B -->|Encrypted Mesh| C[Transit Agents]
    C -->|Encrypted Mesh| D[Target Agent]
    D -->|ICMP Echo| E[Destination]
```

All ICMP payloads are encrypted end-to-end between ingress and exit using ChaCha20-Poly1305. Transit nodes cannot decrypt the traffic.

## Requirements

ICMP relay requires:

1. **Agent** with ICMP enabled (enabled by default)
2. A route from ingress to the target agent
3. **Supported platform** on the target agent (see Platform Support below)

## Platform Support

ICMP uses unprivileged sockets and platform support varies:

| Platform | Supported | Requirements |
|----------|-----------|--------------|
| **Linux** | Yes | `ping_group_range` sysctl must be configured |
| **macOS** | Yes | Works out of the box (no configuration needed) |
| **Windows** | No | Not supported - Windows lacks unprivileged ICMP socket support |

:::warning Windows Agents
ICMP relay does not work when the target agent runs on Windows. Use Linux or macOS agents for ICMP functionality.
:::

:::tip Configuration
See [ICMP Configuration](/configuration/icmp) for all options including session limits and timeouts.
:::

## Linux System Requirements

On Linux, the unprivileged ICMP ping sockets Muti Metroo uses require kernel configuration:

```bash
# Check current setting
sysctl net.ipv4.ping_group_range

# Enable for all groups (run as root)
sudo sysctl -w net.ipv4.ping_group_range="0 2147483647"

# Make persistent
echo "net.ipv4.ping_group_range=0 2147483647" | sudo tee /etc/sysctl.d/99-muti-metroo-ping.conf
```

Without this setting, ICMP fails with "failed to create ICMP socket".

:::info No Root Required -- but root does not bypass the sysctl either
Muti Metroo uses unprivileged ICMP ping sockets, so no root privileges are needed once the sysctl is configured. Conversely, running the agent as root does **not** let you skip it: the kernel gates ping sockets solely on `ping_group_range` and ignores `CAP_NET_RAW`, so an empty range fails even for root. The agent's GID must fall inside the range.
:::

## Usage

### Basic Ping

```bash
# Ping a host through a specific exit agent
muti-metroo ping abc123def456 8.8.8.8

# Output:
# PING 8.8.8.8 via agent abc123def456
# 64 bytes from 8.8.8.8: seq=1 time=12.3ms
# 64 bytes from 8.8.8.8: seq=2 time=11.8ms
```

### Continuous Ping

```bash
# Ping indefinitely (Ctrl+C to stop)
muti-metroo ping abc123def456 -c 0 192.168.1.1
```

### IPv6 Ping

```bash
# Ping IPv6 addresses
muti-metroo ping abc123def456 2001:4860:4860::8888
```

### Via Specific Agent

```bash
# Ping through a specific agent's API
muti-metroo ping -a 192.168.1.10:8080 abc123def456 8.8.8.8
```

## IPv4 and IPv6 Support

ICMP supports both IP versions automatically:

| Version | Protocol | Echo Request | Echo Reply |
|---------|----------|--------------|------------|
| IPv4 | ICMPv4 (1) | Type 8 | Type 0 |
| IPv6 | ICMPv6 (58) | Type 128 | Type 129 |

The agent detects the IP version from the destination address and uses the appropriate socket type.

## End-to-End Encryption

ICMP traffic is encrypted between ingress and exit:

1. Ingress generates ephemeral X25519 keypair
2. Exit generates ephemeral X25519 keypair
3. Both derive shared secret via ECDH
4. Each echo request/reply encrypted with ChaCha20-Poly1305

Transit nodes cannot decrypt ICMP payloads.

## Troubleshooting

### Failed to create ICMP socket (Linux)

```
Error: ICMP open failed: failed to create ICMP socket
```

Configure the `ping_group_range` sysctl on the **exit** agent's host (this is required even when the agent runs as root):

```bash
sudo sysctl -w net.ipv4.ping_group_range="0 2147483647"
echo "net.ipv4.ping_group_range=0 2147483647" | sudo tee /etc/sysctl.d/99-muti-metroo-ping.conf
```

No agent restart is needed -- the socket is created per ping session.

### ICMP Disabled

```
Error: ICMP not enabled
```

- Verify the target agent has `icmp.enabled: true`
- Check that a route exists to the target agent
- Ensure the target agent is connected to the mesh

### Session Timeout

Sessions expire after `idle_timeout` (default 60 seconds) of inactivity. Each ping request resets the timer.

## Security Considerations

1. **E2E encryption**: All ICMP data is encrypted through the mesh
2. **Session limits**: Use `max_sessions` to prevent resource exhaustion
3. **No DNS resolution**: Only IP addresses are accepted (no domain names)

## Related

- [Configuration - ICMP](/configuration/icmp) - Full configuration reference
- [CLI - ping](/cli/ping) - CLI command reference
- [Features - UDP Relay](/features/udp-relay) - UDP tunneling
- [Configuration - Exit](/configuration/exit) - Exit routing setup
