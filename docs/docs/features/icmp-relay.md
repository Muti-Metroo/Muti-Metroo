---
title: ICMP Relay
description: Ping hosts from a chosen remote agent with muti-metroo ping, including the Linux sysctl that unprivileged ICMP needs.
sidebar_position: 3
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-surfacing.png" alt="Mole with ICMP" style={{maxWidth: '180px'}} />
</div>

# ICMP Relay

`muti-metroo ping` sends ICMP echo requests from a remote agent of your choice, so you can check whether a host answers from inside a network that only that agent can reach. Your local agent carries each request through the mesh to the target agent, which sends the real echo request and returns the reply.

```bash
muti-metroo ping 1f743225 10.0.0.1
```

```text
PING 10.0.0.1 via 1f743225d524
Reply from 10.0.0.1: seq=1 time=0.6ms
Reply from 10.0.0.1: seq=2 time=0.6ms
Reply from 10.0.0.1: seq=3 time=0.5ms
Reply from 10.0.0.1: seq=4 time=0.7ms

--- 10.0.0.1 ping statistics ---
4 packets transmitted, 4 received, 0% packet loss
rtt min/avg/max = 0.5/0.6/0.7 ms
```

```mermaid
flowchart LR
    A[muti-metroo ping] -->|HTTP API| B[Local Agent]
    B -->|Encrypted Mesh| C[Transit Agents]
    C -->|Encrypted Mesh| D[Target Agent]
    D -->|ICMP Echo| E[Destination]
```

Echo payloads are end-to-end encrypted between the local agent and the target agent. The destination IP and sequence numbers are visible to transit agents.

[Mutiauk](/mutiauk) also forwards ping traffic from programs on its host through the relay.

## Requirements

- The target agent has ICMP enabled. It is enabled by default; see [ICMP Configuration](/configuration/icmp).
- The target agent runs on Linux or macOS. Windows agents cannot send unprivileged ICMP, so pinging through a Windows agent fails.
- On Linux, the `net.ipv4.ping_group_range` sysctl on the target agent's host includes the agent's group (see below).
- The local agent can route to the target agent.

### Linux sysctl

The agent uses unprivileged ICMP sockets. On Linux the kernel only allows them for groups listed in `net.ipv4.ping_group_range`, and many distributions default to `1 0` (no groups). Running the agent as root does not bypass this check.

```bash
# Show the current range
sysctl net.ipv4.ping_group_range

# Allow all groups now
sudo sysctl -w net.ipv4.ping_group_range="0 2147483647"

# Keep the setting across reboots
echo "net.ipv4.ping_group_range=0 2147483647" | sudo tee /etc/sysctl.d/99-muti-metroo-ping.conf
```

No agent restart is needed; the socket is opened for each ping session. macOS needs no configuration.

## Usage

```bash
# Four echo requests (the default)
muti-metroo ping 1f743225 10.0.0.1

# Ten requests, 500 ms apart
muti-metroo ping -c 10 -i 500ms 1f743225 10.0.0.1

# Until interrupted with Ctrl+C
muti-metroo ping -c 0 1f743225 10.0.0.1

# IPv6 destination
muti-metroo ping 1f743225 2001:db8::10

# Through an agent other than localhost:8080
muti-metroo ping -a 192.168.1.10:8080 1f743225 10.0.0.1
```

The destination must be an IP address; hostnames are rejected. The target agent ID can be a unique prefix. See [ping CLI](/cli/ping) for all flags.

## Troubleshooting

### failed to create ICMP socket

```text
Error: ICMP session failed: failed to create ICMP socket
```

The target agent's host does not allow unprivileged ICMP. On Linux, set `net.ipv4.ping_group_range` as shown above. On Windows, use a Linux or macOS agent instead.

### ICMP echo disabled

```text
Error: ICMP session failed: ICMP echo disabled
```

The target agent has `icmp.enabled: false`.

### no route to agent

```text
Error: ICMP session failed: no route to agent 1f743225
```

The local agent does not know a path to the target agent. Check `muti-metroo peer status` and `muti-metroo mesh-test`.

### Every request times out

The target agent sent the requests but got no reply. The destination may be down, or a firewall between the target agent and the destination drops ICMP.

## See Also

- [ICMP Configuration](/configuration/icmp): enable ICMP and set session limits and timeouts
- [ping CLI](/cli/ping): every flag of `muti-metroo ping`
- [UDP Relay](/features/udp-relay): relay UDP traffic
- [Access Control](/security/access-control): which agents answer ping requests
