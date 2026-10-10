---
title: UDP
sidebar_position: 8
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-inspecting.png" alt="Mole configuring UDP" style={{maxWidth: '180px'}} />
</div>

# UDP Configuration

Tunnel UDP traffic through the mesh. Enable this on exit nodes to support applications like DNS queries, VoIP, and games that need UDP.

**Quick setup:**
```yaml
udp:
  enabled: true
  max_associations: 1000
  idle_timeout: 5m
```

## Configuration

```yaml
udp:
  enabled: true
  max_associations: 1000
  idle_timeout: 5m
  max_datagram_size: 1472
```

## Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `enabled` | bool | true | Enable UDP relay |
| `max_associations` | int | 1000 | Maximum concurrent UDP associations |
| `idle_timeout` | duration | 5m | Association timeout after inactivity |
| `max_datagram_size` | int | 1472 | Maximum UDP payload size in bytes |

## Association Limits

Control resource usage with association limits:

```yaml
udp:
  max_associations: 1000    # Per exit node
  idle_timeout: 5m          # Close inactive associations
```

### max_associations

Maximum number of concurrent UDP associations. When the limit is reached, new UDP ASSOCIATE requests are rejected with an error.

Set to `0` for unlimited associations (not recommended for production).

### idle_timeout

Time after which inactive associations are closed. The timer resets on each datagram sent or received.

## Datagram Size

```yaml
udp:
  max_datagram_size: 1472
```

Maximum UDP payload size in bytes. The default (1472) is calculated as:

```
MTU (1500) - IP header (20) - UDP header (8) = 1472 bytes
```

Datagrams exceeding this size are rejected.

## Examples

### Basic UDP Relay

Enable UDP relay with default settings:

```yaml
udp:
  enabled: true
```

### Custom Timeouts

Adjust timeouts for your environment:

```yaml
udp:
  enabled: true
  idle_timeout: 2m
```

### High-Capacity Exit

For exit nodes handling many clients:

```yaml
udp:
  enabled: true
  max_associations: 10000
  idle_timeout: 10m
  max_datagram_size: 1472
```

### Disabled

To disable UDP relay:

```yaml
udp:
  enabled: false
```

## Complete Exit Configuration

UDP relay works alongside TCP exit routing:

```yaml
exit:
  enabled: true
  routes:
    - "0.0.0.0/0"
  dns:
    servers:
      - "8.8.8.8:53"
    timeout: 5s

udp:
  enabled: true
  max_associations: 1000
  idle_timeout: 5m
```

## Troubleshooting

### UDP ASSOCIATE Rejected

Check that:

1. `udp.enabled` is `true`
2. Exit node is connected to mesh
3. Route exists from ingress to exit

### Too Many Associations

If new associations are rejected:

```
Error: UDP association limit reached
```

Increase `max_associations` or reduce `idle_timeout` to free resources faster.

### Datagram Rejected

If datagrams are rejected for size:

```
Error: datagram too large
```

The payload exceeds `max_datagram_size`. Either:
- Reduce payload size in the application
- Increase `max_datagram_size` (not recommended above MTU)

## Security

1. **Limit associations**: Set reasonable `max_associations`
2. **Short timeouts**: Use shorter `idle_timeout` for high-traffic nodes
3. **Monitor usage**: Track UDP relay metrics for abuse

## Related

- [Features - UDP Relay](/features/udp-relay) - Feature overview
- [Configuration - Exit](/configuration/exit) - Exit node configuration
- [Configuration - SOCKS5](/configuration/socks5) - SOCKS5 ingress setup
