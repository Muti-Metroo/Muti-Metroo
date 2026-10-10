---
title: Debugging (pprof)
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-drilling.png" alt="Mole debugging" style={{maxWidth: '180px'}} />
</div>

# Debugging Endpoints

Find memory leaks, CPU hotspots, and goroutine issues. These Go pprof endpoints help diagnose performance problems in running agents.

**Quick diagnostics:**
```bash
# CPU profile (30 seconds)
curl http://localhost:8080/debug/pprof/profile?seconds=30 > cpu.prof
go tool pprof cpu.prof

# Memory usage
curl http://localhost:8080/debug/pprof/heap > heap.prof
go tool pprof heap.prof

# Goroutine count
curl http://localhost:8080/debug/pprof/goroutine?debug=1 | head -1
```

## Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/debug/pprof/` | GET | pprof index page |
| `/debug/pprof/cmdline` | GET | Command line arguments |
| `/debug/pprof/profile` | GET | CPU profile |
| `/debug/pprof/symbol` | GET | Symbol lookup |
| `/debug/pprof/trace` | GET | Execution trace |
| `/debug/pprof/heap` | GET | Heap memory profile |
| `/debug/pprof/goroutine` | GET | Goroutine stack traces |
| `/debug/pprof/block` | GET | Block profile |
| `/debug/pprof/mutex` | GET | Mutex contention profile |
| `/debug/pprof/threadcreate` | GET | Thread creation profile |

## CPU Profiling

Capture a 30-second CPU profile:

```bash
# Capture profile
curl http://localhost:8080/debug/pprof/profile?seconds=30 > cpu.prof

# Analyze with go tool
go tool pprof cpu.prof

# Interactive mode commands:
# top         - show top functions
# list func   - show annotated source for func
# web         - open graph in browser
```

### Web Interface

```bash
# Start web server for interactive analysis
go tool pprof -http=:8081 cpu.prof
```

## Memory Profiling

Capture heap profile:

```bash
# Current heap state
curl http://localhost:8080/debug/pprof/heap > heap.prof

# Analyze
go tool pprof heap.prof

# Show allocations
(pprof) top
(pprof) list functionName
```

### Memory Types

```bash
# In-use memory (default)
curl http://localhost:8080/debug/pprof/heap > heap.prof

# All allocations since start
curl 'http://localhost:8080/debug/pprof/heap?debug=1' > heap.txt
```

## Goroutine Analysis

Dump all goroutine stacks:

```bash
# Simple dump
curl http://localhost:8080/debug/pprof/goroutine?debug=2

# Machine-readable profile
curl http://localhost:8080/debug/pprof/goroutine > goroutine.prof
go tool pprof goroutine.prof
```

### Detecting Goroutine Leaks

```bash
# Baseline
curl http://localhost:8080/debug/pprof/goroutine?debug=1 | head -1
# goroutine profile: total 42

# After load test
curl http://localhost:8080/debug/pprof/goroutine?debug=1 | head -1
# goroutine profile: total 45 (should not grow unbounded)
```

## Block Profiling

Find where goroutines block on synchronization:

```bash
curl http://localhost:8080/debug/pprof/block > block.prof
go tool pprof block.prof

# Show blocking operations
(pprof) top
```

**Note**: Block profiling has runtime overhead. Use only during debugging.

## Mutex Contention

Find mutex contention hotspots:

```bash
curl http://localhost:8080/debug/pprof/mutex > mutex.prof
go tool pprof mutex.prof

(pprof) top
```

## Execution Tracing

Capture detailed execution trace:

```bash
# Capture 5-second trace
curl http://localhost:8080/debug/pprof/trace?seconds=5 > trace.out

# Analyze with trace tool
go tool trace trace.out
```

The trace viewer shows:
- Goroutine scheduling
- System calls
- GC events
- Network I/O

## Continuous Profiling

### Using curl in a Loop

```bash
#!/bin/bash
while true; do
    DATE=$(date +%Y%m%d_%H%M%S)
    curl -s http://localhost:8080/debug/pprof/heap > heap_$DATE.prof
    sleep 60
done
```

### Using pprof Directly

```bash
# Continuously profile, compare to base
go tool pprof -base heap_base.prof heap_current.prof
```

## Remote Profiling

Profile a remote agent:

```bash
# Direct connection
go tool pprof http://agent-host:8080/debug/pprof/profile?seconds=30

# Via SSH tunnel
ssh -L 8080:localhost:8080 user@agent-host
go tool pprof http://localhost:8080/debug/pprof/heap
```

## Common Debugging Scenarios

### High Memory Usage

```bash
# Capture heap profile
curl http://localhost:8080/debug/pprof/heap > heap.prof

# Find largest allocations
go tool pprof -top heap.prof

# Find allocation sites
go tool pprof heap.prof
(pprof) top -cum
```

### High CPU Usage

```bash
# Profile for 30 seconds during high load
curl http://localhost:8080/debug/pprof/profile?seconds=30 > cpu.prof

# Find hot functions
go tool pprof cpu.prof
(pprof) top
(pprof) list hotFunction
```

### Goroutine Leak

```bash
# Check goroutine count over time
watch -n 5 'curl -s http://localhost:8080/debug/pprof/goroutine?debug=1 | head -1'

# If growing, dump full trace
curl http://localhost:8080/debug/pprof/goroutine?debug=2 > goroutines.txt
```

### Deadlock Detection

```bash
# Dump all goroutines
curl http://localhost:8080/debug/pprof/goroutine?debug=2 | grep -A 100 "sync.Mutex"

# Look for goroutines blocked on mutexes
```

## Security Considerations

**Warning**: pprof endpoints expose sensitive information about the running process including:
- Memory contents
- Stack traces
- Function names
- File paths

**Recommendations:**
1. Bind HTTP server to localhost only in production
2. Use firewall rules to restrict access
3. Disable in highly sensitive environments
4. Use authentication if exposing externally

```yaml
# Restrict to localhost
http:
  address: "127.0.0.1:8080"
```

## Related

- [Troubleshooting - Performance](/troubleshooting/performance) - Performance issues
- [API - Health](/api/health) - Health check endpoints
