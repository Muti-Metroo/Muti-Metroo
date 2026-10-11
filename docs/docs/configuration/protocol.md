---
title: Protocol
description: Configuration reference for the protocol section, which sets the ALPN value, HTTP/2 header, and WebSocket subprotocol that agents use to identify mesh connections.
sidebar_position: 6
---

# Protocol

The `protocol` section sets the identifiers that agents exchange when a transport connection is set up. Change them to match local network policy, for example to avoid a recognizable value on the wire. Agents that connect to each other must use the same values.

## Configuration

```yaml
protocol:
  alpn: "muti-metroo/1"                  # TLS ALPN value (QUIC, HTTP/2, WebSocket over TLS)
  http_header: "X-Muti-Metroo-Protocol"  # HTTP/2 request and response header name
  ws_subprotocol: "muti-metroo/1"        # WebSocket subprotocol
```

## Keys

| Key              | Type   | Default                    | Description                                                                                                                                                    |
| ---------------- | ------ | -------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `alpn`           | string | `"muti-metroo/1"`          | ALPN protocol identifier offered by peers and accepted by QUIC listeners. Also used as the value of the HTTP/2 header.                                         |
| `http_header`    | string | `"X-Muti-Metroo-Protocol"` | Header name the `h2` transport sends with its request and response. An `h2` listener rejects a request whose header carries a value other than its `alpn`.     |
| `ws_subprotocol` | string | `"muti-metroo/1"`          | Subprotocol the `ws` transport requests and accepts during the WebSocket upgrade.                                                                             |

Setting a key to an empty string has the same effect as leaving it at its default; the identifiers are always sent.

## Examples

All agents in the mesh share the custom values:

```yaml
protocol:
  alpn: "app/2"
  http_header: "X-App-Channel"
  ws_subprotocol: "app.v2"
```

## See Also

- [TLS](/configuration/tls-certificates) for certificates and ClientHello profiles used in the same handshake
- [Listeners](/configuration/listeners) and [Peers](/configuration/peers) for the connections these values apply to
- [Transports](/concepts/transports) for how QUIC, HTTP/2, and WebSocket connections are set up
