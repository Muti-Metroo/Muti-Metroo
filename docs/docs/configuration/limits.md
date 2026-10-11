---
title: Limits
description: Configuration reference for the limits section, covering the stream limits, stream open timeout, and buffer size keys and which of them the agent enforces.
sidebar_position: 15
---

# Limits

The `limits` section caps the number of concurrent streams an agent carries and sets the stream open timeout. A stream is one tunneled connection, such as a SOCKS connection, a port forward, a file transfer, or a management command session.

## Configuration

```yaml
limits:
  max_streams_total: 10000
  stream_open_timeout: 30s
  # Deprecated keys, accepted but ignored: max_streams_per_peer,
  # max_pending_opens, buffer_size
```

## Keys

| Key                    | Type     | Default  | Description                                                                                                                                                                       |
| ---------------------- | -------- | -------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `max_streams_total`    | int      | `10000`  | Maximum concurrent streams on this agent. Further stream opens are refused. Also caps concurrent exit connections, concurrent forward endpoint connections, and the streams per QUIC listener connection. This is the only enforced stream limit. |
| `max_streams_per_peer` | int      | `1000`   | Deprecated. Accepted for backward compatibility, but ignored: no per-peer limit is enforced.                                                                                      |
| `max_pending_opens`    | int      | `100`    | Deprecated. Accepted for backward compatibility, but ignored: pending opens count toward `max_streams_total`.                                                                     |
| `stream_open_timeout`  | duration | `30s`    | Time allowed for the remote end to accept a management command session opened by this agent. SOCKS and port forward streams use a fixed 30 seconds, and file transfers 5 minutes. |
| `buffer_size`          | int      | `262144` | Deprecated. Accepted for backward compatibility, but ignored: stream buffering is a fixed per-stream frame queue, not a sized byte buffer.                                        |

## Examples

An exit that serves many concurrent clients:

```yaml
limits:
  max_streams_total: 50000
```

## See Also

- [Streams](/concepts/streams) for how streams are opened and carried through the mesh
- [Connections](/configuration/connections) for keepalive and reconnection timing
- [SOCKS Proxy](/configuration/socks5) for the separate `max_connections` limit on SOCKS clients
- [Forward](/configuration/forward) for the per-listener `max_connections` limit
