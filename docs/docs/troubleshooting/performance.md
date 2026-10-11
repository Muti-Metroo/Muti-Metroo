---
title: Performance Troubleshooting
description: Find the cause of slow connections, low throughput, and high CPU or memory use in a Muti Metroo mesh, and the settings that affect them.
sidebar_position: 3
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-drilling.png" alt="Mole optimizing performance" style={{maxWidth: '180px'}} />
</div>

# Performance Troubleshooting

Traffic through the mesh is limited by the slowest link on its path and by the CPU of the ingress and exit agents, which encrypt and decrypt every stream. This page shows how to locate the bottleneck.

## Measure first

Measure a transfer through the SOCKS listener and compare it with the same transfer made directly from the exit host:

```bash
# Through the mesh, from the ingress side
curl -x socks5h://127.0.0.1:1080 -o /dev/null -w 'time %{time_total}s, %{speed_download} bytes/s\n' \
  https://example.com/large-file

# Directly, on the exit host
curl -o /dev/null -w 'time %{time_total}s, %{speed_download} bytes/s\n' https://example.com/large-file
```

Then measure each hop separately:

```bash
muti-metroo peer status
iperf3 -c relay.example.com
```

`peer status` shows the round-trip time to each directly connected peer once it has been measured. `iperf3` needs an `iperf3 -s` server on the other host and the matching port open.

## Slow connection setup

Every new connection through the mesh needs one round trip along the whole path before data flows, in addition to the TCP handshake at the exit. Interactive tools that open many short connections feel this most.

- **Reduce the number of hops.** Check the path with `muti-metroo route trace <destination>`; the `hops` value is the number of agents between ingress and exit. Peer the ingress closer to the exit, or place relays where they shorten the path.
- **Reduce per-hop latency.** Place relays in regions close to both neighbors.
- **Check DNS.** With `socks5h://`, the ingress resolves host names (or the exit, for domain routes). A slow resolver adds its delay to every new connection.

## Low throughput

- **Find the slowest link.** A stream cannot be faster than the slowest hop on its path. Compare `iperf3` results between neighboring agents.
- **Compare transports.** QUIC runs over UDP and avoids head-of-line blocking between streams sharing a peer connection. HTTP/2 and WebSocket run over a single TCP connection per peer, so packet loss on that link slows every stream on it. Where UDP is allowed and the link is lossy, QUIC usually performs better.
- **Check the CPU on the ingress and exit.** Those agents encrypt and decrypt every byte. A core at 100% there caps throughput. Transit agents do not decrypt stream payloads, but every agent still handles TLS on each of its links.

### QUIC UDP buffer warning

On Linux, the QUIC library may log this at startup:

```text
failed to sufficiently increase receive buffer size (was: 208 kiB, wanted: 7168 kiB, got: 416 kiB). See https://github.com/quic-go/quic-go/wiki/UDP-Buffer-Sizes for details.
```

The kernel limits UDP socket buffers to less than QUIC needs for high-bandwidth links. Raise the limits:

```bash
sudo sysctl -w net.core.rmem_max=7500000
sudo sysctl -w net.core.wmem_max=7500000
```

Add the same settings to `/etc/sysctl.d/` to keep them after a reboot. Set the environment variable `QUIC_GO_DISABLE_RECEIVE_BUFFER_WARNING=true` to silence the warning without changing the limits.

## High CPU use

- **Lower the log level.** `agent.log_level: "debug"` logs per-connection events and costs CPU under load. Use `info` or `warn` in production.
- **Look at the traffic pattern.** CPU use scales with the bytes encrypted and the number of streams opened. Check `stream_count` in `/healthz` over time.
- **Profile the agent** with pprof, as described below.

## High memory use

Memory grows with the number of open streams and peer connections. Check the current count:

```bash
curl -s http://localhost:8080/healthz | jq '.stream_count'
```

- **Limit concurrent streams.** `limits.max_streams_total` (default 10000) caps the streams an agent holds at once. New streams beyond it are refused.
- **Find streams that never close.** A steadily growing `stream_count` with constant traffic usually means clients hold connections open. Check the applications behind the SOCKS listener.
- **Take a heap profile** to see where memory goes, as described below.

In containers, set a memory limit so a runaway agent is restarted instead of exhausting the host.

## Profiling with pprof

The HTTP API serves Go's pprof endpoints under `/debug/pprof/` unless `http.pprof: false` or `http.minimal: true` is set. Expose the HTTP API only to trusted networks while they are enabled.

```bash
# CPU profile over 30 seconds
curl -o cpu.prof "http://localhost:8080/debug/pprof/profile?seconds=30"
go tool pprof -top cpu.prof

# Heap profile
curl -o heap.prof http://localhost:8080/debug/pprof/heap
go tool pprof -top heap.prof

# Stack traces of all goroutines
curl "http://localhost:8080/debug/pprof/goroutine?debug=2"
```

`go tool pprof` requires a Go installation on the machine where you analyze the profile; it does not need to run on the agent's host. Attach profiles to performance bug reports.

## See Also

- [API - Debugging](/api/debugging) - pprof endpoints and how to disable them
- [Configuration - Limits](/configuration/limits) - Stream limits and their defaults
- [Transports](/concepts/transports) - QUIC, HTTP/2, and WebSocket compared
- [Connectivity Troubleshooting](/troubleshooting/connectivity) - Path and route diagnosis
- [High Availability](/deployment/high-availability) - Placing relays and exits
