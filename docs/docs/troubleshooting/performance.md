---
title: Performance Troubleshooting
sidebar_position: 3
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-drilling.png" alt="Mole optimizing performance" style={{maxWidth: '180px'}} />
</div>

# Performance Troubleshooting

Things running slow? Use these diagnostics to find bottlenecks and tune your mesh for better speed.

**Quick diagnostics:**
```bash
# Check stream and connection counts
curl http://localhost:8080/healthz | jq '{streams: .stream_count, peers: .peer_count}'

# CPU profile (30 seconds)
curl http://localhost:8080/debug/pprof/profile?seconds=30 > cpu.prof

# Memory usage
curl http://localhost:8080/debug/pprof/heap > heap.prof
```

## High Latency

### Symptoms

- Slow page loads
- SSH feels sluggish
- Stream open timeouts

### Diagnosis

```bash
# Count hops
curl http://localhost:8080/healthz | jq '.route_count'
# Look at metric values - each increment is one hop

# Check logs for latency issues
journalctl -u muti-metroo | grep -i "latency\|timeout"
```

### Solutions

**1. Reduce hop count**

Fewer hops = lower latency.

```
Before: A → B → C → D → E (4 hops)
After:  A → B → E (2 hops)
```

**2. Use faster transports**

QUIC is generally faster than HTTP/2 or WebSocket.

**3. Optimize network path**

- Use geographically closer relays
- Avoid high-latency links

**4. Tune keepalive**

```yaml
connections:
  idle_threshold: 60s    # Less frequent keepalives
```

## Low Throughput

### Symptoms

- Slow file transfers
- Low throughput
- Buffering on streams

### Diagnosis

```bash
# Check network speed between hops
iperf3 -c peer-address -p 5201

# Check logs for buffer issues
journalctl -u muti-metroo | grep -i "buffer\|throttle"
```

### Solutions

**1. Increase buffer size**

```yaml
limits:
  buffer_size: 524288    # 512 KB (default 256 KB)
```

Larger buffers = better throughput, but more memory.

**2. Check for bottlenecks**

```bash
# Test network speed between hops
iperf3 -c peer-address -p 5201

# Check if CPU-bound
top -p $(pgrep muti-metroo)
```

**3. Use QUIC**

QUIC handles packet loss better than TCP-based transports.

**4. Use larger data transfers**

Small frequent transfers have more overhead. Batch data when possible.

## High Memory Usage

### Symptoms

- Agent using excessive RAM
- OOM kills
- System slowdown

### Diagnosis

```bash
# Check memory usage
ps aux | grep muti-metroo
cat /proc/$(pgrep muti-metroo)/status | grep -i mem

# Check stream count via health endpoint
curl http://localhost:8080/healthz | jq '.stream_count'
```

### Calculation

Memory per stream = buffer_size x number_of_hops

```
1000 streams x 256 KB buffer x 3 hops = 768 MB
```

### Solutions

**1. Reduce buffer size**

```yaml
limits:
  buffer_size: 131072    # 128 KB
```

**2. Limit concurrent streams**

```yaml
limits:
  max_streams_per_peer: 500
  max_streams_total: 2000
```

**3. Reduce hop count**

Fewer hops = less buffering per stream.

**4. Add memory limits (container)**

```yaml
# docker-compose.yml
services:
  agent:
    deploy:
      resources:
        limits:
          memory: 1G
```

## High CPU Usage

### Symptoms

- Agent using high CPU
- Slow response times
- System load high

### Diagnosis

```bash
# Check CPU usage
top -p $(pgrep muti-metroo)

# CPU profiling
curl http://localhost:8080/debug/pprof/profile?seconds=30 > cpu.prof
go tool pprof cpu.prof
```

### Solutions

**1. Reduce logging**

```yaml
agent:
  log_level: "warn"    # Not debug or info
```

**2. Limit stream count**

More streams = more CPU.

```yaml
limits:
  max_streams_total: 5000
```

**3. Use faster hardware**

CPU-bound workloads benefit from faster cores.

## Connection Issues

### Too Many Connections

```bash
# Check connection count
netstat -an | grep 4433 | wc -l
curl http://localhost:8080/healthz | jq '.peer_count'
```

**Solutions:**

```yaml
limits:
  max_streams_per_peer: 500    # Limit per peer
```

### Connection Churn

Frequent connect/disconnect wastes resources.

```bash
# Check logs for reconnection patterns
journalctl -u muti-metroo | grep -i "disconnect\|reconnect"
```

**Solutions:**

1. Increase timeouts
2. Improve network stability
3. Check for misbehaving peers

## pprof Debugging

Muti Metroo exposes pprof for profiling:

```bash
# CPU profile
curl http://localhost:8080/debug/pprof/profile?seconds=30 > cpu.prof
go tool pprof cpu.prof

# Memory profile
curl http://localhost:8080/debug/pprof/heap > heap.prof
go tool pprof heap.prof

# Goroutine dump
curl http://localhost:8080/debug/pprof/goroutine?debug=2

# Block profile (where goroutines block)
curl http://localhost:8080/debug/pprof/block > block.prof
go tool pprof block.prof
```

## Optimization Checklist

### For Latency

- [ ] Minimize hop count
- [ ] Use QUIC transport
- [ ] Geographically optimize relay placement
- [ ] Check network latency between hops

### For Throughput

- [ ] Increase buffer sizes
- [ ] Use QUIC transport
- [ ] Check for network bottlenecks
- [ ] Monitor for packet loss

### For Memory

- [ ] Reduce buffer size if needed
- [ ] Limit stream counts
- [ ] Monitor active streams
- [ ] Set memory limits

### For CPU

- [ ] Reduce logging verbosity
- [ ] Limit stream counts
- [ ] Profile with pprof
- [ ] Check for excessive reconnections

## Performance Tuning Guide

### Low Latency Priority

```yaml
routing:
  max_hops: 4              # Limit hops

connections:
  idle_threshold: 60s      # Less keepalive traffic

limits:
  buffer_size: 131072      # 128 KB - smaller buffers
```

### High Throughput Priority

```yaml
limits:
  buffer_size: 524288      # 512 KB - larger buffers
  max_streams_per_peer: 2000

connections:
  idle_threshold: 30s      # Detect issues quickly
```

### Memory Constrained

```yaml
limits:
  buffer_size: 65536       # 64 KB
  max_streams_total: 1000
  max_streams_per_peer: 100
```

## See Also

- [CLI - Status](/cli/status) - Check agent status
- [API - Debugging (pprof)](/api/debugging) - Profiling endpoints
- [API - Health](/api/health) - Health check endpoints

## Next Steps

- [Common Issues](/troubleshooting/common-issues) - Quick solutions
- [Deployment](/deployment/scenarios) - Optimize deployment
- [FAQ](/troubleshooting/faq) - Common questions
