---
title: Listeners
description: Configuration reference for transport listeners that accept QUIC, HTTP/2, and WebSocket connections from other agents, including TLS overrides and advertising.
sidebar_position: 3
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-inspecting.png" alt="Mole configuring listeners" style={{maxWidth: '180px'}} />
</div>

# Listeners

The `listeners` list opens transport endpoints that other agents dial with their [`peers`](/configuration/peers) entries. An agent can run any number of listeners on different transports at once. Once a connection is up, traffic flows in both directions regardless of which side listened.

## Configuration

```yaml
listeners:
  - transport: quic             # quic, h2, or ws
    address: "0.0.0.0:4433"     # Bind address

  - transport: h2
    address: "0.0.0.0:8443"
    path: "/mesh"               # Required for h2 and ws

  - transport: ws
    address: "127.0.0.1:8081"
    path: "/mesh"
    plaintext: true             # ws only: no TLS, for use behind a reverse proxy

  - transport: ws
    address: "0.0.0.0:443"
    path: "/mesh"
    tls:                        # Optional per-listener overrides
      cert: "./certs/public.crt"
      key: "./certs/public.key"
      mtls: false
    advertise:                  # Optional: publish a dial target to the mesh
      address: "relay.example.com:443"
```

## Keys

Each list entry accepts these keys:

| Key                   | Type   | Default            | Description                                                                                                                      |
| --------------------- | ------ | ------------------ | -------------------------------------------------------------------------------------------------------------------------------- |
| `transport`           | string | required           | `quic`, `h2`, or `ws`.                                                                                                           |
| `address`             | string | required           | Bind address as `host:port`, `[ipv6]:port`, or `:port`.                                                                          |
| `path`                | string | `""`               | HTTP path that accepts connections. Required for `h2` and `ws`; not used by `quic`.                                              |
| `plaintext`           | bool   | `false`            | Serve WebSocket without TLS. Only valid with `transport: ws`.                                                                    |
| `tls.cert`            | string | global `tls.cert`  | Certificate file for this listener.                                                                                              |
| `tls.key`             | string | global `tls.key`   | Private key file for this listener. Redacted by `config show`.                                                                  |
| `tls.cert_pem`        | string | `""`               | Inline certificate PEM; takes precedence over `tls.cert`.                                                                        |
| `tls.key_pem`         | string | `""`               | Inline private key PEM; takes precedence over `tls.key`. Redacted by `config show`.                                              |
| `tls.mtls`            | bool   | global `tls.mtls`  | Require client certificates on this listener. Client certificates are always verified against the global `tls.ca`.               |
| `advertise.address`   | string | required in block  | Public `host:port` other agents should dial. Loopback and unspecified hosts (`127.0.0.1`, `::1`, `0.0.0.0`, `::`) are rejected. |
| `advertise.path`      | string | listener `path`    | Path to advertise, when a reverse proxy publishes the listener under a different path.                                          |
| `advertise.transport` | string | listener transport | Transport to advertise (`quic`, `h2`, `ws`), when a proxy changes it.                                                            |

A listener's `tls` block accepts the same keys as a peer's `tls` block, but `ca`, `ca_pem`, and `strict` only apply to peers and have no effect on a listener.

## Transports

| Transport | Protocol      | Notes                                                     |
| --------- | ------------- | --------------------------------------------------------- |
| `quic`    | UDP, TLS 1.3  | Lowest overhead. Needs UDP to be allowed on the path.     |
| `h2`      | TCP, HTTP/2   | Looks like HTTPS. Needs `path`.                           |
| `ws`      | TCP, WebSocket| Passes HTTP proxies and reverse proxies. Needs `path`.    |

See [Transports](/concepts/transports) for a comparison.

## TLS

Without any certificate configured, each listener generates a self-signed ECDSA certificate at startup. To use your own certificate, set it globally in [`tls`](/configuration/tls-certificates) or per listener under `tls`. Certificate and key must be configured together, and certificates must use EC keys.

When mTLS is in effect for a listener (global `tls.mtls: true`, or `tls.mtls: true` on the listener), the global `tls.ca` must be set; validation fails otherwise:

```yaml
tls:
  ca: "./certs/ca.crt"
  cert: "./certs/agent.crt"
  key: "./certs/agent.key"
  mtls: true

listeners:
  - transport: quic
    address: "0.0.0.0:4433"     # Requires client certificates
  - transport: h2
    address: "0.0.0.0:8443"
    path: "/mesh"
    tls:
      mtls: false               # No client certificates on this one
```

## Plaintext WebSocket

```yaml
listeners:
  - transport: ws
    address: "127.0.0.1:8081"
    path: "/mesh"
    plaintext: true
```

A `plaintext` listener serves `ws://` without TLS, for deployments where a reverse proxy terminates TLS and forwards to the agent. mTLS does not apply. Agent traffic stays end-to-end encrypted, but bind the listener to loopback or otherwise keep it unreachable except from the proxy. Proxy configuration examples are in [Reverse Proxy Deployment](/deployment/reverse-proxy).

## Advertising a listener

```yaml
listeners:
  - transport: h2
    address: "0.0.0.0:3000"
    path: "/mesh"
    advertise:
      address: "relay.example.com:443"
      path: "/agents/mesh"
```

With an `advertise` block, the agent includes the listener's public dial target in its node info, so other agents and the dashboard can see how to reach it. Without the block, the listener is not advertised; this keeps NAT-bound or internal listeners private.

## Runtime management

Listeners can be opened and closed on a running agent with [`muti-metroo listener`](/cli/listener) or the `/listeners/manage` HTTP endpoint. Runtime listeners use the global TLS settings and persist across restarts when `agent.data_dir` is set. Listeners from this file cannot be removed at runtime.

## Examples

### QUIC with an HTTP/2 fallback

```yaml
listeners:
  - transport: quic
    address: "0.0.0.0:4433"
  - transport: h2
    address: "0.0.0.0:443"
    path: "/mesh"
```

### Inline certificates from the environment

```yaml
tls:
  ca_pem: "${TLS_CA}"
  cert_pem: "${TLS_CERT}"
  key_pem: "${TLS_KEY}"
  mtls: true

listeners:
  - transport: quic
    address: "0.0.0.0:4433"
```

See [Environment Variables](/configuration/environment-variables#multiline-values) for how to pass PEM content through a variable.

## See Also

- [Peers](/configuration/peers) for the dialing side of a connection
- [TLS](/configuration/tls-certificates) for global certificate, CA, and mTLS settings
- [listener CLI](/cli/listener) to add and remove listeners at runtime
- [Transports](/concepts/transports) to choose between QUIC, HTTP/2, and WebSocket
- [Reverse Proxy Deployment](/deployment/reverse-proxy) for plaintext WebSocket behind a proxy
