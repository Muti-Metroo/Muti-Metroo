---
title: Debugging Endpoints (pprof)
description: Go runtime profiling endpoints for diagnosing CPU, memory, and goroutine problems in a running agent.
sidebar_position: 18
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-drilling.png" alt="Mole debugging" style={{maxWidth: '180px'}} />
</div>

# Debugging Endpoints (pprof)

The agent exposes the standard Go `net/http/pprof` endpoints for profiling CPU use, memory, and goroutines in a running process. They form the pprof group (`http.pprof`), which is enabled by default whenever the HTTP server is enabled and is disabled by `http.minimal: true`. When `http.token_hash` is set, these endpoints require the bearer token like any other.

:::warning
Profiles reveal stack traces, function names, file paths, command-line arguments, and memory contents of the agent. Set `http.pprof: false` on production agents unless you are actively debugging, and never expose the endpoints without a bearer token.
:::

```yaml
http:
  enabled: true
  address: "127.0.0.1:8080"
  pprof: false
```

## Endpoints

| Endpoint | Description |
| -------- | ----------- |
| `GET /debug/pprof/` | HTML index of the available profiles |
| `GET /debug/pprof/profile?seconds=N` | CPU profile over `N` seconds (default 30) |
| `GET /debug/pprof/trace?seconds=N` | Execution trace over `N` seconds (default 1) |
| `GET /debug/pprof/heap` | Live heap allocations |
| `GET /debug/pprof/allocs` | All allocations since start |
| `GET /debug/pprof/goroutine` | Stacks of all goroutines |
| `GET /debug/pprof/threadcreate` | Stacks that created OS threads |
| `GET /debug/pprof/block` | Blocking on synchronization primitives |
| `GET /debug/pprof/mutex` | Mutex contention |
| `GET /debug/pprof/cmdline` | Command line of the process |
| `GET /debug/pprof/symbol` | Symbol lookup for program counters |

Profiles are returned in the binary pprof format for `go tool pprof`. Add `?debug=1` to `heap`, `allocs`, `goroutine`, `threadcreate`, `block`, or `mutex` for a text summary, or `?debug=2` to `goroutine` for full stack dumps. The block and mutex profiles are empty because the agent does not enable their sampling.

## CPU profile

```bash
curl -o cpu.prof "http://localhost:8080/debug/pprof/profile?seconds=30"
go tool pprof -top cpu.prof
```

Open an interactive view in the browser:

```bash
go tool pprof -http=:8081 cpu.prof
```

`go tool pprof` can also fetch the profile itself:

```bash
go tool pprof "http://localhost:8080/debug/pprof/profile?seconds=30"
```

## Memory

```bash
curl -o heap.prof http://localhost:8080/debug/pprof/heap
go tool pprof -top heap.prof
```

Compare two snapshots to see what grew:

```bash
curl -o heap-before.prof http://localhost:8080/debug/pprof/heap
curl -o heap-after.prof http://localhost:8080/debug/pprof/heap
go tool pprof -top -base heap-before.prof heap-after.prof
```

## Goroutines

The first line of the text summary is the goroutine count. A count that keeps growing under steady load indicates a leak.

```bash
curl -s "http://localhost:8080/debug/pprof/goroutine?debug=1" | head -1
```

```text
goroutine profile: total 87
```

Dump every goroutine stack to a file:

```bash
curl -s -o goroutines.txt "http://localhost:8080/debug/pprof/goroutine?debug=2"
```

## Execution trace

```bash
curl -o trace.out "http://localhost:8080/debug/pprof/trace?seconds=5"
go tool trace trace.out
```

## Profiling a remote host

Keep the HTTP server bound to loopback and forward the port over SSH:

```bash
ssh -L 8080:127.0.0.1:8080 user@agent-host.example.com
```

Then run the commands above against `http://localhost:8080` on your workstation.

## See Also

- [HTTP Configuration](/configuration/http): enable or disable the pprof group
- [Performance Troubleshooting](/troubleshooting/performance): diagnosing slow or resource-hungry agents
- [HTTP API Overview](/api/overview): endpoint groups and authentication
