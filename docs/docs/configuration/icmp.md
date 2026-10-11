---
title: ICMP
description: Configuration reference for the icmp section, which controls the ICMP echo (ping) relay and its session limits, plus the operating system settings it needs.
sidebar_position: 12
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-presenting.png" alt="Mole configuring ICMP" style={{maxWidth: '180px'}} />
</div>

# ICMP

The `icmp` section controls the ICMP echo relay: the agent that a ping session ends on sends echo requests to the destination IP and returns the replies through the mesh. Sessions are started with [`muti-metroo ping`](/cli/ping), the HTTP API, or Mutiauk. IPv4 and IPv6 destinations are supported; destinations must be IP addresses.

## Configuration

```yaml
icmp:
  enabled: true
  max_sessions: 100
  idle_timeout: 60s
  echo_timeout: 5s
```

## Keys

| Key            | Type     | Default | Description                                                                                                 |
| -------------- | -------- | ------- | ----------------------------------------------------------------------------------------------------------- |
| `enabled`      | bool     | `true`  | Relay echo requests for sessions that end on this agent. When false, sessions are refused with `ICMP echo is disabled`. |
| `max_sessions` | int      | `100`   | Maximum concurrent ICMP sessions. Further sessions are refused with `ICMP session limit reached`. `0` means no limit. |
| `idle_timeout` | duration | `60s`   | A session with no echo traffic for this long is closed.                                                     |
| `echo_timeout` | duration | `5s`    | How long the agent waits for each echo reply. The `--timeout` of `muti-metroo ping` applies separately on the client side. |

`icmp.enabled` is on by default on every agent, independent of `exit.enabled`. On agents that should not send ICMP to other hosts, set `enabled: false`.

## Operating system requirements

The relay uses unprivileged ICMP sockets.

| Platform | Support                                                     |
| -------- | ----------------------------------------------------------- |
| Linux    | Yes, when `net.ipv4.ping_group_range` includes the agent's group |
| macOS    | Yes, no configuration needed                                |
| Windows  | No                                                          |

On most Linux distributions `net.ipv4.ping_group_range` defaults to `1 0`, an empty range, and session setup fails with `failed to create ICMP socket`. Allow all groups:

```bash
sysctl net.ipv4.ping_group_range
sudo sysctl -w net.ipv4.ping_group_range="0 2147483647"
echo "net.ipv4.ping_group_range=0 2147483647" | sudo tee /etc/sysctl.d/99-muti-metroo-ping.conf
```

Running as root does not remove this requirement: the kernel checks only `ping_group_range` for these sockets, so the root group (GID 0) must also be in the range. The setting is read when each session opens a socket, so no agent restart is needed.

## Examples

### Disable the relay

```yaml
icmp:
  enabled: false
```

### More concurrent sessions

```yaml
icmp:
  enabled: true
  max_sessions: 500
  idle_timeout: 30s
```

## See Also

- [ICMP Relay usage](/features/icmp-relay) for how ping sessions travel through the mesh
- [ping CLI](/cli/ping) to send echo requests through an agent
- [UDP](/configuration/udp) for the UDP relay settings
- [Exit](/configuration/exit) for TCP exit routes
