---
title: ICMP
sidebar_position: 10
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-presenting.png" alt="Mole configuring ICMP" style={{maxWidth: '180px'}} />
</div>

# ICMP Configuration

Configure ICMP echo (ping) support on agents. When enabled, agents can forward ICMP echo requests to destinations.

## Overview

ICMP support allows you to ping remote hosts through the mesh network. The feature:

- Uses **unprivileged ICMP sockets** (no root required on most systems)
- Supports **IPv4 and IPv6** addresses
- Supports **E2E encryption** for all traffic through the mesh
- Works with the `muti-metroo ping` CLI command

## Configuration Options

```yaml
icmp:
  enabled: true              # Enable ICMP echo forwarding (default: true)
  max_sessions: 100          # Concurrent session limit (0 = unlimited)
  idle_timeout: 60s          # Session cleanup timeout
  echo_timeout: 5s           # Per-echo request timeout
```

### enabled

Controls whether ICMP echo forwarding is available on this agent.

| Type | Default |
|------|---------|
| bool | `true` |

When disabled, ping requests to this agent will fail with "icmp not enabled".

### max_sessions

Maximum number of concurrent ICMP sessions.

| Type | Default |
|------|---------|
| int | `100` |

Set to `0` for unlimited sessions. Each unique (destination IP, ICMP identifier) pair creates a session.

### idle_timeout

How long inactive sessions are kept before cleanup.

| Type | Default |
|------|---------|
| duration | `60s` |

Sessions are cleaned up after this duration of inactivity. Active sessions are kept alive as long as echo requests are being sent.

### echo_timeout

Timeout for individual ICMP echo requests.

| Type | Default |
|------|---------|
| duration | `5s` |

This is the server-side timeout. The CLI also has its own timeout (`-t` flag) which may be shorter.

## Example Configurations

### Default (Enabled)

ICMP is enabled by default with sensible limits:

```yaml
icmp:
  enabled: true
  max_sessions: 100
```

### High-Volume Testing

For testing environments with many concurrent pings:

```yaml
icmp:
  enabled: true
  max_sessions: 500
  idle_timeout: 30s
```

### Disabled

To disable ICMP echo forwarding:

```yaml
icmp:
  enabled: false
```

## IPv6 Support

ICMP supports both IPv4 and IPv6 addresses. The agent automatically detects the IP version and uses the appropriate socket type:

- **IPv4**: Uses ICMPv4 (protocol 1) with Echo Request/Reply types 8/0
- **IPv6**: Uses ICMPv6 (protocol 58) with Echo Request/Reply types 128/129

```bash
# IPv4 ping (requires target agent ID)
muti-metroo ping abc123def456 8.8.8.8

# IPv6 ping
muti-metroo ping abc123def456 2001:4860:4860::8888
```

For IPv6 to work, ensure the destination is reachable via IPv6 from the agent.

## Platform Support

ICMP uses unprivileged sockets, and support varies by platform:

| Platform | Supported | Notes |
|----------|-----------|-------|
| **Linux** | Yes | Requires `ping_group_range` sysctl (see below) |
| **macOS** | Yes | Works without configuration |
| **Windows** | No | Unprivileged ICMP sockets not supported |

:::warning Windows Not Supported
ICMP relay does not work on Windows. The agent must run on Linux or macOS.
:::

### Linux Configuration

On Linux, the unprivileged ICMP "ping" sockets Muti Metroo uses (`SOCK_DGRAM` / `IPPROTO_ICMP`) require the `ping_group_range` sysctl. This is **disabled by default** on most distributions (`1 0`, an empty range).

```bash
# Check current setting (default is usually "1 0" = disabled)
sysctl net.ipv4.ping_group_range

# Enable for all groups (run as root)
sudo sysctl -w net.ipv4.ping_group_range="0 2147483647"

# Make persistent
echo "net.ipv4.ping_group_range=0 2147483647" | sudo tee /etc/sysctl.d/99-muti-metroo-ping.conf
```

Without this setting, ICMP fails with "failed to create ICMP socket".

:::warning Running as root does not bypass this
The kernel gates ping sockets **solely** on `ping_group_range` and **does not** consult `CAP_NET_RAW` for them (unlike raw sockets). So even an agent running as root -- the default for a `muti-metroo service install` systemd unit -- still fails to open an ICMP socket when the range is empty. The agent's GID (0 for root) must fall inside the configured range. The sysctl is checked when the socket is created (per ping session), so no agent restart is needed after changing it.
:::

### macOS

macOS supports unprivileged ICMP sockets natively. No configuration is required.

## Security Considerations

1. **E2E encryption**: All ICMP data is encrypted through the mesh
2. **Session limits**: Use `max_sessions` to prevent resource exhaustion
3. **No domain resolution**: Only IP addresses are accepted (no DNS)

## Usage

Once configured, use the CLI to ping through the agent:

```bash
# Basic ping (requires target agent ID)
muti-metroo ping abc123def456 8.8.8.8

# Continuous ping
muti-metroo ping abc123def456 -c 0 192.168.1.1

# IPv6 ping
muti-metroo ping abc123def456 2001:4860:4860::8888
```

See [ping CLI command](/cli/ping) for full usage details.

## Related

- [Features - ICMP Relay](/features/icmp-relay) - Feature overview
- [ping CLI Command](/cli/ping) - Send ICMP requests through agents
- [Exit Routing Configuration](/configuration/exit) - Configure exit routing
- [UDP Configuration](/configuration/udp) - UDP relay configuration
