---
title: muti-metroo ping
sidebar_label: ping
sidebar_position: 21
description: Send ICMP echo requests from a remote agent to an IP address and report replies and round-trip times.
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-drilling.png" alt="Mole pinging" style={{maxWidth: '180px'}} />
</div>

# muti-metroo ping

Sends ICMP echo requests from a remote agent to an IP address, through the mesh. The remote agent sends the actual ICMP packets, so the result shows whether that agent can reach the destination, and the reported time includes the mesh path between you and that agent.

The command connects to a gateway agent's HTTP API (`--agent`), which must have the remote API endpoints enabled (`http.remote_api`). The target agent must have ICMP enabled (`icmp.enabled`, on by default). See [ICMP configuration](../configuration/icmp.md).

Global flags (`--token`, `--yes`, `--no-color`) are described in [CLI overview](overview.md#global-flags).

## Synopsis

```bash
muti-metroo ping [flags] <target-agent-id> <destination>
```

## Arguments

| Argument | Description |
| --- | --- |
| `<target-agent-id>` | The agent that sends the ICMP packets. A full 32-character agent ID, or a unique prefix of one. |
| `<destination>` | IPv4 or IPv6 address to ping. Host names are not accepted; resolve them first. |

A prefix is resolved against the gateway agent's list of known agents. The command fails if no agent or more than one agent matches.

## Flags

| Flag | Default | Description |
| --- | --- | --- |
| `-a`, `--agent` | `localhost:8080` | Gateway agent HTTP API address, as `host:port` or a URL. See [Agent address](overview.md#agent-address). |
| `-c`, `--count` | `4` | Number of echo requests to send. `0` sends until interrupted with Ctrl+C. |
| `-i`, `--interval` | `1s` | Wait between requests, as a Go duration (`500ms`, `2s`). |
| `-w`, `--timeout` | `5s` | How long to wait for each reply, as a Go duration. |

`-t` is a deprecated alias for `--timeout`; it still works but prints a warning to stderr. See [Deprecated commands and flags](overview.md#deprecated-commands-and-flags).

## Output

```bash
muti-metroo ping -c 3 abc123de 10.0.0.1
```

```text
PING 10.0.0.1 via abc123def456
Reply from 10.0.0.1: seq=1 time=23.4ms
Reply from 10.0.0.1: seq=2 time=21.8ms
Reply from 10.0.0.1: seq=3 time=22.1ms

--- 10.0.0.1 ping statistics ---
3 packets transmitted, 3 received, 0% packet loss
rtt min/avg/max = 21.8/22.4/23.4 ms
```

The header shows the first 12 characters of the target agent ID. `time` is measured by the CLI, from sending the request to receiving the reply. A request without a reply within `--timeout` prints `seq=N: timeout`, and an error reported by the target agent prints `seq=N: error: <message>`. The statistics are printed when the count is reached or when you press Ctrl+C.

## Examples

```bash
# Four pings from the target agent
muti-metroo ping abc123def456789012345678901234ab 10.0.0.1

# Use a short agent ID prefix
muti-metroo ping abc123de 10.0.0.1

# Through a different gateway agent
muti-metroo ping -a 192.168.1.10:8080 abc123de 10.0.0.1

# Ten pings, 500 ms apart, waiting at most 2 seconds for each reply
muti-metroo ping -c 10 -i 500ms -w 2s abc123de 192.168.1.1

# Ping until Ctrl+C
muti-metroo ping -c 0 abc123de 10.0.0.1

# IPv6 destination
muti-metroo ping abc123de 2001:db8::1
```

## Error messages

| Error | Cause |
| --- | --- |
| `destination must be a valid IP address: <value>` | The destination is a host name or not a valid address. |
| `no agent found matching prefix: <prefix>` | No known agent ID starts with the prefix. |
| `ambiguous prefix '<prefix>' matches N agents: ...` | Use a longer prefix or the full ID. |
| `failed to connect to ICMP endpoint: ...` | The gateway agent is unreachable, rejected the bearer token, or has `http.remote_api` disabled. |
| `ICMP session failed: <reason>` | The target agent refused the session, for example because ICMP is disabled there or the session limit is reached. |

## Exit status

The command exits 0 once it has printed statistics, including when no replies were received; check the packet loss line. It exits 1 when the session cannot be set up (any error in the table above). See [Exit codes](overview.md#exit-codes).

## See Also

- [ICMP configuration](../configuration/icmp.md): enable ICMP on an agent and set session limits
- [ICMP relay](../features/icmp-relay.md): how ICMP echo works through the mesh
- [ICMP HTTP API](../api/icmp.md): the WebSocket endpoint this command uses
- [probe](probe.md): test a transport listener directly
- [shell](shell.md): run diagnostic commands such as `traceroute` on a remote agent
