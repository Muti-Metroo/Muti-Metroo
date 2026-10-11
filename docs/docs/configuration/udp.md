---
title: UDP
description: Configuration reference for the udp section, which controls the exit-side UDP relay used by SOCKS5 UDP ASSOCIATE.
sidebar_position: 11
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-inspecting.png" alt="Mole configuring UDP" style={{maxWidth: '180px'}} />
</div>

# UDP

The `udp` section controls the UDP relay on the agent that sends datagrams to their destination. A SOCKS5 client opens a relay with UDP ASSOCIATE on the ingress agent; the datagrams travel through the mesh, end-to-end encrypted, and leave from this agent.

## Configuration

```yaml
udp:
  enabled: true
  max_associations: 1000
  idle_timeout: 5m
  max_datagram_size: 1472
```

## Keys

| Key                 | Type     | Default | Description                                                                                                  |
| ------------------- | -------- | ------- | ------------------------------------------------------------------------------------------------------------ |
| `enabled`           | bool     | `true`  | Relay UDP for associations that end on this agent. When false, requests are refused with `UDP relay is disabled`. |
| `max_associations`  | int      | `1000`  | Maximum concurrent associations on this agent. Further requests are refused. `0` means no limit.             |
| `idle_timeout`      | duration | `5m`    | An association with no datagrams in either direction for this long is closed.                                |
| `max_datagram_size` | int      | `1472`  | Maximum UDP payload in bytes. Larger datagrams from the client are dropped. The default is a 1500-byte MTU minus the IPv4 and UDP headers. |

`udp.enabled` is on by default on every agent, independent of `exit.enabled`. On agents that should not send UDP traffic to other hosts, set `enabled: false`.

## Examples

### Disable the relay

```yaml
udp:
  enabled: false
```

### Exit serving many UDP clients

```yaml
udp:
  enabled: true
  max_associations: 10000
  idle_timeout: 2m
```

## See Also

- [UDP Relay usage](/features/udp-relay) for client setup and troubleshooting
- [SOCKS Proxy](/configuration/socks5) for the ingress side of UDP ASSOCIATE
- [Exit](/configuration/exit) for TCP exit routes
- [ICMP](/configuration/icmp) for the ICMP echo relay
