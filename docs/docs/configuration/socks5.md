---
title: SOCKS Proxy
description: Configuration reference for the socks5 section, covering the SOCKS4, SOCKS4a, and SOCKS5 listener, username and password authentication, connection limits, and the WebSocket listener.
sidebar_position: 7
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-inspecting.png" alt="Mole configuring SOCKS5" style={{maxWidth: '180px'}} />
</div>

# SOCKS Proxy

The `socks5` section turns the agent into an ingress: applications connect to the proxy, and the agent carries their traffic through the mesh to an exit. One listener serves SOCKS4, SOCKS4a, and SOCKS5; the version is detected from the first byte of each connection.

## Configuration

```yaml
socks5:
  enabled: true
  address: "127.0.0.1:1080"
  max_connections: 1000
  auth:
    enabled: false
    users:
      - username: "user1"
        password_hash: "$2a$12$GiitCO3IBCFV2/QmQ4BzNOQGykcLM1DtPyvfMB7kF5qZLDGk6BtnS"
  websocket:
    enabled: false
    address: "0.0.0.0:8443"
    path: "/socks5"
    plaintext: false
```

## Keys

| Key                         | Type   | Default            | Description                                                                                                       |
| --------------------------- | ------ | ------------------ | ----------------------------------------------------------------------------------------------------------------- |
| `enabled`                   | bool   | `false`            | Start the SOCKS listener.                                                                                         |
| `address`                   | string | `"127.0.0.1:1080"` | Bind address. Required when `enabled` is true.                                                                    |
| `max_connections`           | int    | `1000`             | Maximum concurrent client connections. New connections beyond the limit are closed. `0` means no limit.          |
| `auth.enabled`              | bool   | `false`            | Require SOCKS5 username and password authentication.                                                              |
| `auth.users`                | list   | `[]`               | Accepted credentials.                                                                                             |
| `auth.users[].username`     | string | `""`               | User name.                                                                                                        |
| `auth.users[].password_hash`| string | `""`               | bcrypt hash of the password. Redacted by `config show`.                                                           |
| `auth.users[].password`     | string | `""`               | Plaintext password. Deprecated; use `password_hash`. Redacted by `config show`.                                  |
| `websocket.enabled`         | bool   | `false`            | Also accept SOCKS5 over WebSocket. Requires `socks5.enabled: true`.                                              |
| `websocket.address`         | string | `""`               | Bind address for the WebSocket listener. Required when `websocket.enabled` is true.                              |
| `websocket.path`            | string | `"/socks5"`        | Path that accepts the WebSocket upgrade. Must start with `/`.                                                     |
| `websocket.plaintext`       | bool   | `false`            | Serve plain HTTP instead of TLS, for use behind a reverse proxy that terminates TLS.                              |

## Bind address

The default binds to loopback, so only local applications can use the proxy. To accept clients from the network, bind to an interface address or `0.0.0.0`, and turn on authentication:

```yaml
socks5:
  enabled: true
  address: "0.0.0.0:1080"
  auth:
    enabled: true
    users:
      - username: "team"
        password_hash: "$2a$12$GiitCO3IBCFV2/QmQ4BzNOQGykcLM1DtPyvfMB7kF5qZLDGk6BtnS"
```

:::warning
A proxy on a non-loopback address without authentication lets anyone who can reach the port use the mesh.
:::

For SOCKS5 UDP ASSOCIATE, the agent opens the UDP relay socket on the same IP as `address`, so a loopback proxy also keeps the UDP relay on loopback.

## Authentication

With `auth.enabled: true`, clients must authenticate with SOCKS5 username and password. SOCKS4 and SOCKS4a have no password mechanism, so those clients are rejected.

Generate a hash with [`muti-metroo hash`](/cli/hash):

```bash
muti-metroo hash
```

Any bcrypt hash works, including one from `htpasswd -bnBC 12 "" yourpassword | tr -d ':\n'`.

Use the same form for every user. When at least one user has a `password_hash`, users that only have a plaintext `password` are not accepted.

With `auth.enabled: true` and no users, every client is rejected.

## WebSocket listener

```yaml
socks5:
  enabled: true
  address: "127.0.0.1:1080"
  websocket:
    enabled: true
    address: "0.0.0.0:8443"
    path: "/socks5"
```

The WebSocket listener carries the SOCKS5 protocol inside a WebSocket connection, for networks that only allow HTTPS. It runs next to the TCP listener and only starts when `socks5.enabled` is true.

- Without `plaintext`, it serves TLS with the agent certificate from [`tls`](/configuration/tls-certificates), or a generated self-signed certificate.
- With `plaintext: true`, it serves plain HTTP; bind it to loopback and put a TLS-terminating reverse proxy in front.
- When `auth.enabled` is true, the WebSocket upgrade also requires HTTP Basic authentication with the same credentials, before the SOCKS5 handshake.
- Requests to `/` return a plain splash page; only `path` accepts upgrades.

[Mutiauk](/mutiauk) can connect to this listener with its WebSocket transport.

## Hostname resolution

When a client sends a host name (SOCKS5 domain address or SOCKS4a), the ingress agent resolves it in this order:

1. A matching [domain route](/configuration/exit#domain-routes) sends the name to the exit agent, which resolves it.
2. A name in the local [mesh host registry](/configuration/hosts) resolves to its registered IP.
3. Otherwise the system resolver of the ingress host resolves it, and the result is routed by CIDR.

## Examples

### Local only

```yaml
socks5:
  enabled: true
  address: "127.0.0.1:1080"
```

### Shared proxy with authentication

```yaml
socks5:
  enabled: true
  address: "0.0.0.0:1080"
  max_connections: 5000
  auth:
    enabled: true
    users:
      - username: "${SOCKS_USER}"
        password_hash: "${SOCKS_PASS_HASH}"
```

## See Also

- [SOCKS Proxy usage](/features/socks5-proxy) for client examples (curl, SSH, browsers, proxychains)
- [UDP](/configuration/udp) for the exit-side UDP relay settings used by UDP ASSOCIATE
- [Exit](/configuration/exit) for the routes that decide where proxied traffic leaves the mesh
- [hash CLI](/cli/hash) to generate password hashes
- [Authentication](/security/authentication) for how SOCKS, HTTP API, and management authentication fit together
