---
title: Peers
description: Configuration reference for outbound peer connections, including expected agent IDs, address formats per transport, per-peer TLS overrides, and TCP keepalive.
sidebar_position: 4
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-inspecting.png" alt="Mole configuring peers" style={{maxWidth: '180px'}} />
</div>

# Peers

The `peers` list tells the agent which other agents to dial. The other side needs a matching [listener](/configuration/listeners). Dial direction only matters for reachability: once connected, either agent can open streams through the other, so put agents behind NAT or firewalls on the dialing side.

## Configuration

```yaml
peers:
  - id: "abc123def456789012345678901234ab" # Expected agent ID, or "auto"
    transport: quic                         # quic, h2, or ws
    address: "192.0.2.10:4433"

  - id: "auto"
    transport: h2
    address: "https://relay.example.com:443/mesh" # URL form carries the path
    tcp_keepalive: 20s                     # h2/ws only; overrides connections.tcp_keepalive
    tls:                                   # Optional per-peer overrides
      ca: "./certs/relay-ca.crt"
      strict: true
```

## Keys

Each list entry accepts these keys:

| Key                  | Type     | Default                        | Description                                                                                                                                                       |
| -------------------- | -------- | ------------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `id`                 | string   | required                       | Expected agent ID of the peer (32 hexadecimal characters). The connection is rejected if the remote agent reports a different ID. `auto` accepts any agent ID.  |
| `transport`          | string   | required                       | `quic`, `h2`, or `ws`. Must match the remote listener.                                                                                                            |
| `address`            | string   | required                       | Where to dial. See [Address formats](#address-formats).                                                                                                           |
| `tcp_keepalive`      | duration | `connections.tcp_keepalive`    | TCP keepalive period for `h2` and `ws` peers. `0` disables it. Ignored for `quic`. Must not be negative.                                                         |
| `tls.ca`             | string   | global `tls.ca`                | CA file used to verify this peer's certificate.                                                                                                                   |
| `tls.ca_pem`         | string   | `""`                           | Inline CA PEM; takes precedence over `tls.ca`.                                                                                                                    |
| `tls.cert`           | string   | global `tls.cert`              | Client certificate file presented to this peer.                                                                                                                   |
| `tls.key`            | string   | global `tls.key`               | Client private key file. Redacted by `config show`.                                                                                                               |
| `tls.cert_pem`       | string   | `""`                           | Inline client certificate PEM; takes precedence over `tls.cert`.                                                                                                  |
| `tls.key_pem`        | string   | `""`                           | Inline client key PEM; takes precedence over `tls.key`. Redacted by `config show`.                                                                                |
| `tls.strict`         | bool     | global `tls.strict`            | Verify this peer's certificate against the effective CA. Requires a CA (per-peer or global).                                                                     |
| `path`               | string   | `""`                           | Accepted, but not applied when dialing in the current release. Put the path in `address` instead.                                                                 |
| `proxy`              | string   | `""`                           | Accepted, but not applied when dialing in the current release.                                                                                                    |
| `proxy_auth.username`| string   | `""`                           | Accepted, but not applied when dialing in the current release.                                                                                                    |
| `proxy_auth.password`| string   | `""`                           | Accepted, but not applied when dialing in the current release. Redacted by `config show`.                                                                         |

`tls.mtls` is a listener setting and has no effect on a peer. Client certificate and key must be configured together.

## Address formats

| Transport | Accepted `address` forms                                  | Request path                       |
| --------- | --------------------------------------------------------- | ---------------------------------- |
| `quic`    | `host:port`, `[ipv6]:port`                                | not used                           |
| `h2`      | `host:port`, or `https://host:port/path`                  | `/mesh` for the bare form          |
| `ws`      | `host:port`, or `wss://host:port/path` / `ws://host:port/path` | `/mesh` for the bare form     |

When the remote listener uses a path other than `/mesh`, use the URL form:

```yaml
peers:
  - id: "auto"
    transport: ws
    address: "wss://relay.example.com:443/agents/mesh"
```

Use `ws://` only to reach a plaintext WebSocket listener.

## Peer ID

`id` is required. With a full agent ID, the agent checks the ID the remote side reports during the handshake and drops the connection on a mismatch. `auto` skips that check, which is useful when the remote ID is not known in advance.

An `id` that is neither `auto` nor 32 hexadecimal characters passes validation, but the agent logs `failed to parse peer ID` and does not dial that peer.

To find the ID of the remote agent, run `muti-metroo status` on it or read its `data_dir/agent_id`.

## TLS

By default the agent does not verify the peer's certificate. Stream payloads are still end-to-end encrypted, but without verification an attacker who can intercept the link can sit in the middle of it; see [End-to-End Encryption](/security/e2e-encryption#limits-of-the-protection). The global [`tls`](/configuration/tls-certificates) settings apply to every peer, and the `tls` block on a peer overrides them for that peer only:

- The effective CA (per-peer, else global) verifies the peer when `strict` is true.
- The effective certificate and key (per-peer, else global) are presented as the client certificate, which a listener with mTLS requires.

```yaml
tls:
  ca: "./certs/ca.crt"
  cert: "./certs/agent.crt"
  key: "./certs/agent.key"
  strict: true

peers:
  - id: "abc123def456789012345678901234ab"
    transport: quic
    address: "192.0.2.10:4433"      # Verified against the global CA

  - id: "def456abc123789012345678901234cd"
    transport: ws
    address: "wss://relay.example.com:443/mesh"
    tls:
      strict: false                 # Not verified
```

## Reconnection and keepalive

A peer that disconnects, or cannot be reached at startup, is redialed with exponential backoff. The dial timeout, backoff, retry limit, and keepalive timing are global settings in [`connections`](/configuration/connections). `tcp_keepalive` is the only per-peer timing override.

## Runtime management

Peers can be added and removed on a running agent with [`muti-metroo peer`](/cli/peer) or the `/peers/manage` HTTP endpoint. Runtime peers use the global TLS settings, reconnect like configured peers, and persist across restarts when `agent.data_dir` is set. Peers from this file cannot be removed or replaced at runtime.

## Examples

### Spoke dialing a hub

```yaml
peers:
  - id: "abc123def456789012345678901234ab"
    transport: quic
    address: "hub.example.com:4433"
```

### Peer from environment variables

```yaml
peers:
  - id: "${PEER_ID:-auto}"
    transport: "${PEER_TRANSPORT:-quic}"
    address: "${PEER_ADDR}"
```

## See Also

- [Listeners](/configuration/listeners) for the accepting side of a connection
- [Connections](/configuration/connections) for dial timeout, keepalive, and reconnection backoff
- [TLS](/configuration/tls-certificates) for global certificate and verification settings
- [peer CLI](/cli/peer) to add and remove peers at runtime
- [Architecture](/concepts/architecture) for why dial direction does not affect stream direction
