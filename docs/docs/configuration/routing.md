---
title: Routing
sidebar_position: 14
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-plumbing.png" alt="Mole configuring routing" style={{maxWidth: '180px'}} />
</div>

# Routing Configuration

Control how routes propagate through your mesh. These settings affect route advertisement timing, expiration, and path depth limits.

**Default settings work for most deployments:**

```yaml
routing:
  advertise_interval: 2m
  route_ttl: 5m
  max_hops: 16
```

## Configuration

```yaml
routing:
  advertise_interval: 2m # How often to re-advertise routes
  node_info_interval: 2m # How often to advertise node info
  route_ttl: 5m # How long routes are valid
  max_hops: 16 # Maximum path length
```

## Options

| Option               | Type     | Default | Description                       |
| -------------------- | -------- | ------- | --------------------------------- |
| `advertise_interval` | duration | `2m`    | Route advertisement frequency     |
| `node_info_interval` | duration | `2m`    | Node info advertisement frequency |
| `route_ttl`          | duration | `5m`    | Time until routes expire          |
| `max_hops`           | int      | `16`    | Maximum route path length         |

## Route Advertisement

Routes are periodically re-advertised to keep them alive:

```yaml
routing:
  advertise_interval: 2m # Re-advertise every 2 minutes
```

### Trigger Immediate Advertisement

After configuration changes, trigger immediate advertisement:

```bash
curl -X POST http://localhost:8080/routes/advertise
```

### Trade-offs

| Interval | Bandwidth | Responsiveness  |
| -------- | --------- | --------------- |
| `30s`    | Higher    | Fast failover   |
| `2m`     | Moderate  | Good balance    |
| `5m`     | Lower     | Slower failover |

## Route TTL

Routes expire if not refreshed:

```yaml
routing:
  route_ttl: 5m # Routes expire after 5 minutes
```

### Relationship with Advertisement

TTL should be **at least 2x** the advertisement interval:

| Advertisement | Recommended TTL | Why                           |
| ------------- | --------------- | ----------------------------- |
| `30s`         | `90s`           | Allow 2 missed advertisements |
| `2m`          | `5m`            | Default - good balance        |
| `5m`          | `12m`           | Slower networks               |

If TTL < 2x advertisement interval, routes may flap during normal operation.

## Node Info Advertisement

Node info (display name, roles, system info) is advertised separately:

```yaml
routing:
  node_info_interval: 2m # Update node info every 2 minutes
```

This controls how often the dashboard sees updated information about agents.

## Maximum Hops

Limit route propagation depth:

```yaml
routing:
  max_hops: 16 # Routes stop propagating after 16 hops
```

### What max_hops Affects

- **Route advertisements**: Routes with metric >= max_hops are not forwarded
- **NOT stream paths**: Stream open timeout (30s default) limits actual path length

### Recommended Values

| Topology                  | Suggested max_hops |
| ------------------------- | ------------------ |
| Simple chain (2-3 agents) | `4`                |
| Small mesh (5-10 agents)  | `8`                |
| Large mesh (10+ agents)   | `16`               |
| Complex topology          | `16` (default)     |

Setting max_hops too low may prevent routes from reaching all agents. Setting it too high has minimal cost.

## Connection Tuning

Related settings in the `connections` section affect peer behavior:

```yaml
connections:
  idle_threshold: 5m # Keepalive after idle time
  timeout: 90s # Peer dead after no response
  keepalive_jitter: 0.2 # Timing jitter (avoids synchronized keepalives)

  reconnect:
    initial_delay: 1s # First reconnect attempt
    max_delay: 60s # Maximum backoff
    multiplier: 2.0 # Backoff multiplier
    jitter: 0.2 # Reconnect timing jitter
    max_retries: 0 # 0 = infinite retries
```

### Keepalive Settings

| Option             | Type     | Default | Description               |
| ------------------ | -------- | ------- | ------------------------- |
| `idle_threshold`   | duration | `5m`    | Send keepalive after idle |
| `timeout`          | duration | `90s`   | Declare peer dead         |
| `keepalive_jitter` | float    | `0.2`   | Timing randomization      |

### Reconnection Settings

| Option          | Type     | Default | Description                     |
| --------------- | -------- | ------- | ------------------------------- |
| `initial_delay` | duration | `1s`    | First retry delay               |
| `max_delay`     | duration | `60s`   | Maximum retry delay             |
| `multiplier`    | float    | `2.0`   | Exponential backoff factor      |
| `jitter`        | float    | `0.2`   | Retry timing randomization      |
| `max_retries`   | int      | `0`     | Maximum attempts (0 = infinite) |

## Resource Limits

The `limits` section controls stream and buffer resources:

```yaml
limits:
  max_streams_per_peer: 1000 # Concurrent streams per peer
  max_streams_total: 10000 # Total concurrent streams
  max_pending_opens: 100 # Pending stream open requests
  stream_open_timeout: 30s # Stream open round-trip timeout
  buffer_size: 262144 # Per-stream buffer (bytes)
```

### Options

| Option                 | Type     | Default  | Description                                    |
| ---------------------- | -------- | -------- | ---------------------------------------------- |
| `max_streams_per_peer` | int      | `1000`   | Maximum concurrent streams per peer connection |
| `max_streams_total`    | int      | `10000`  | Maximum concurrent streams across all peers    |
| `max_pending_opens`    | int      | `100`    | Maximum pending stream open requests           |
| `stream_open_timeout`  | duration | `30s`    | Total round-trip time allowed for stream open  |
| `buffer_size`          | int      | `262144` | Per-stream buffer size in bytes (256 KB)       |

### When to Adjust

- **High-traffic proxy**: Increase `max_streams_per_peer` and `max_streams_total`
- **Memory-constrained**: Reduce `buffer_size` (trades throughput for memory)
- **High-latency paths**: Increase `stream_open_timeout`

## Tuning Guide

### Fast Failover

For environments requiring quick recovery:

```yaml
routing:
  advertise_interval: 30s
  route_ttl: 90s

connections:
  idle_threshold: 15s
  timeout: 45s
```

### Bandwidth Conscious

For low-bandwidth or metered connections:

```yaml
routing:
  advertise_interval: 5m
  route_ttl: 12m
  node_info_interval: 5m

connections:
  idle_threshold: 2m
  timeout: 5m
```

### High Latency Networks

For satellite or high-latency links:

```yaml
routing:
  advertise_interval: 3m
  route_ttl: 8m

connections:
  idle_threshold: 1m
  timeout: 3m
```

## Examples

### Default (Most Deployments)

```yaml
routing:
  advertise_interval: 2m
  node_info_interval: 2m
  route_ttl: 5m
  max_hops: 16
```

### Fast Convergence

```yaml
routing:
  advertise_interval: 30s
  node_info_interval: 30s
  route_ttl: 90s
  max_hops: 16
```

### Large Mesh

```yaml
routing:
  advertise_interval: 3m
  node_info_interval: 3m
  route_ttl: 8m
  max_hops: 32
```

## Environment Variables

```yaml
routing:
  advertise_interval: "${ROUTE_ADVERTISE_INTERVAL:-2m}"
  route_ttl: "${ROUTE_TTL:-5m}"
  max_hops: ${ROUTE_MAX_HOPS:-16}
```

## Related

- [Routing Concepts](/concepts/routing) - How routing works
- [Exit Configuration](/configuration/exit) - Configure exit routes
- [Troubleshooting](/troubleshooting/connectivity) - Route issues
