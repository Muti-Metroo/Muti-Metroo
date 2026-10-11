---
title: muti-metroo probe
sidebar_label: probe
sidebar_position: 19
description: Test whether a Muti Metroo transport listener is reachable and completes a handshake, or start a temporary listener to probe against.
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-inspecting.png" alt="Mole probing" style={{maxWidth: '180px'}} />
</div>

# muti-metroo probe

Connects to a Muti Metroo transport listener (QUIC, HTTP/2, or WebSocket) and performs the peer handshake to confirm that the listener is reachable and is a Muti Metroo agent. `probe listen` starts a temporary listener on the other end, so you can test firewall rules, TLS certificates, and reverse proxy setups before deploying an agent.

Neither command needs a running agent or a configuration file.

Global flags (`--token`, `--yes`, `--no-color`) are described in [CLI overview](overview.md#global-flags).

## probe

```bash
muti-metroo probe <address> [flags]
```

Dials `<address>` (`host:port`) with the selected transport, opens a stream, and exchanges the peer hello messages. On success it prints the remote agent ID, display name, and the round-trip time of the whole probe.

TLS certificate verification is on by default and uses the system trust store. Pass `--ca` to verify against a private CA, or `--insecure` to skip verification (for example, against an agent that uses auto-generated certificates). The probe always offers the default protocol identifiers (ALPN, HTTP header, WebSocket subprotocol), so it cannot complete a handshake with a listener that uses custom values from the `protocol` config section.

### Flags

| Flag | Default | Description |
| --- | --- | --- |
| `-T`, `--transport` | `quic` | Transport type: `quic`, `h2`, or `ws`. |
| `--path` | `/mesh` | HTTP path for the `h2` and `ws` transports. |
| `-w`, `--timeout` | `10s` | Timeout for the whole probe, as a Go duration (`5s`, `1m`). |
| `--ca` | none | CA certificate file used to verify the listener certificate. |
| `--cert` | none | Client certificate file, for listeners that require mTLS. |
| `--key` | none | Client private key file, for listeners that require mTLS. |
| `--insecure` | `false` | Skip TLS certificate verification. |
| `--plaintext` | `false` | Connect without TLS. Only valid with `-T ws`, for a WebSocket listener behind a TLS-terminating reverse proxy. |
| `--json` | `false` | Print the result as a JSON object. |

`-t` is a deprecated alias for `--timeout`; it still works but prints a warning to stderr. See [Deprecated commands and flags](overview.md#deprecated-commands-and-flags).

### Output

A successful probe:

```bash
muti-metroo probe --insecure 192.0.2.10:4433
```

```text
Probing quic://192.0.2.10:4433...

[OK] Connection successful!
  Transport:    quic
  Address:      192.0.2.10:4433
  Remote ID:    abc123def456789012345678901234ab
  Display Name: exit-1
  RTT:          1ms
```

A failed probe:

```text
Probing quic://192.0.2.10:4433...

[FAILED] Connection failed
  Transport:  quic
  Address:    192.0.2.10:4433
  Error:      TLS error - certificate signed by unknown authority (disable verification or provide a trusted CA)
Error: probe failed
```

With `--json`:

```json
{
  "success": true,
  "transport": "quic",
  "address": "192.0.2.10:4433",
  "remote_id": "abc123def456789012345678901234ab",
  "remote_display_name": "exit-1",
  "rtt_ms": 1
}
```

On failure the object has `"success": false` and an `error` field with the same message as the text output. `remote_id`, `remote_display_name`, and `rtt_ms` are omitted when empty.

### Error messages

| Error | Likely cause |
| --- | --- |
| `Could not resolve hostname - DNS lookup failed` | The host name does not resolve. |
| `Connection refused - listener not running or port blocked` | Nothing listens on the TCP port (`h2`, `ws`), or a firewall rejects the connection. |
| `No route to host - network unreachable` | No network path to the host. |
| `Connection timed out - firewall may be blocking` | No answer within `--timeout`. QUIC uses UDP, so a closed or filtered UDP port also shows up as a timeout. |
| `TLS error - certificate signed by unknown authority (...)` | The certificate is not trusted. Pass `--ca` or `--insecure`. |
| `TLS error - certificate has expired` | The listener certificate has expired. |
| `TLS handshake failed - ...` | Any other TLS failure, such as a hostname mismatch or a missing client certificate. |
| `Connected but received invalid response - not a Muti Metroo listener?` | Something answered, but not with a Muti Metroo handshake. |
| `Connected but handshake failed - not a Muti Metroo listener?` | The connection opened, but the handshake stream failed. |

### Examples

```bash
# QUIC listener (default transport)
muti-metroo probe relay.example.com:4433

# HTTP/2 and WebSocket listeners
muti-metroo probe -T h2 relay.example.com:443 --path /mesh
muti-metroo probe -T ws relay.example.com:443 --path /mesh

# Try each transport to find which one the network allows
for t in quic h2 ws; do muti-metroo probe -T "$t" --insecure relay.example.com:443; done

# Plaintext WebSocket listener behind a reverse proxy
muti-metroo probe -T ws --plaintext 127.0.0.1:8888 --path /mesh

# Agent with an auto-generated (self-signed) certificate
muti-metroo probe --insecure relay.example.com:4433

# Private CA, and mTLS with a client certificate
muti-metroo probe --ca ./certs/ca.crt relay.example.com:4433
muti-metroo probe --ca ./certs/ca.crt --cert ./certs/client.crt --key ./certs/client.key relay.example.com:4433

# Machine-readable result
muti-metroo probe --json --insecure relay.example.com:4433
```

### Exit status

| Code | Meaning |
| --- | --- |
| 0 | The probe succeeded. With `--json`, the command also exits 0 when the probe fails; read the `success` field. |
| 1 | The probe failed, `--plaintext` was used without `-T ws`, or `--timeout` is not a valid duration. |
| 2 | Usage error, such as a missing address. |

## probe listen

```bash
muti-metroo probe listen [flags]
```

Starts a minimal listener that answers probe handshakes and prints one entry per incoming connection. It does not join a mesh, route traffic, or accept agent peer sessions beyond the handshake. It runs until interrupted with Ctrl+C (or SIGTERM), then prints a connection summary.

Without `--cert` and `--key` the listener generates an ephemeral self-signed certificate, so clients must probe it with `--insecure`. Each successful handshake reports the remote ID and display name sent by the client; `muti-metroo probe` always identifies itself with the display name `probe` and a fixed placeholder ID.

### Flags

| Flag | Default | Description |
| --- | --- | --- |
| `-T`, `--transport` | `quic` | Transport type: `quic`, `h2`, or `ws`. |
| `-a`, `--address` | `0.0.0.0:4433` | Listen address. |
| `--path` | `/mesh` | HTTP path for the `h2` and `ws` transports. |
| `--cert` | none | TLS certificate file. Must be used together with `--key`. |
| `--key` | none | TLS private key file. Must be used together with `--cert`. |
| `--ca` | none | CA certificate used to verify client certificates. When set, clients must present a certificate (mTLS). |
| `--plaintext` | `false` | Listen without TLS. Only valid with `-T ws`, for use behind a TLS-terminating reverse proxy. |
| `-n`, `--name` | `probe-listener` | Display name returned to clients in the handshake. |
| `--json` | `false` | Print each connection event as a JSON line. |
| `--alpn` | none | Custom ALPN protocol identifier. Empty uses the default. |
| `--http-header` | none | Custom HTTP header identifier for the `h2` transport. Empty uses the default. |
| `--ws-subprotocol` | none | Custom WebSocket subprotocol. Empty uses the default. |

Because `muti-metroo probe` always offers the default protocol identifiers, it cannot connect to a `probe listen` instance started with `--alpn`, `--http-header`, or `--ws-subprotocol`.

### Output

```bash
muti-metroo probe listen -a 0.0.0.0:4433
```

```text
Probe Listener
==============
Transport:  quic
Address:    0.0.0.0:4433
Name:       probe-listener
TLS:        enabled (ephemeral certificates)

Listening for connections... (Ctrl+C to stop)

[2026-10-11 14:55:17] [OK] 198.51.100.7:64808
  Remote ID:   70726f62652d74656d706f7261727921
  Remote Name: probe
  RTT:         0ms


Shutting down...
Total connections: 1 (1 successful, 0 failed)
```

For `h2` and `ws` the header also shows `Path:`, and the remote address is printed as `unknown`. Connections that fail during TLS (for example, a client that rejects the certificate) never reach the listener and are not reported.

With `--json`, the header and summary are not printed, and each event is one line:

```json
{"timestamp":"2026-10-11T14:55:04.174007+03:00","remote_addr":"198.51.100.7:64808","remote_id":"70726f62652d74656d706f7261727921","remote_name":"probe","success":true}
```

Failed events have `"success": false` and an `error` field. `rtt_ms` is omitted when it rounds to zero.

### Examples

```bash
# QUIC with an ephemeral certificate
muti-metroo probe listen -a 0.0.0.0:4433

# HTTP/2 and WebSocket
muti-metroo probe listen -T h2 -a 0.0.0.0:443 --path /mesh
muti-metroo probe listen -T ws -a 0.0.0.0:443 --path /mesh

# Plaintext WebSocket behind a reverse proxy that terminates TLS
muti-metroo probe listen -T ws -a 127.0.0.1:8888 --path /mesh --plaintext

# Static certificate, then require client certificates (mTLS)
muti-metroo probe listen -a 0.0.0.0:4433 --cert ./certs/agent.crt --key ./certs/agent.key
muti-metroo probe listen -a 0.0.0.0:4433 --cert ./certs/agent.crt --key ./certs/agent.key --ca ./certs/ca.crt

# Custom display name and JSON events
muti-metroo probe listen --json --name edge-test -a 0.0.0.0:4433
```

### Testing a certificate setup end to end

```bash
# Terminal 1: listener with the certificate you plan to deploy, requiring client certificates
muti-metroo probe listen -a 0.0.0.0:4433 \
  --cert ./certs/agent.crt --key ./certs/agent.key --ca ./certs/ca.crt

# Terminal 2: probe with full verification and a client certificate
muti-metroo probe --ca ./certs/ca.crt \
  --cert ./certs/client.crt --key ./certs/client.key \
  relay.example.com:4433
```

The probe connects by the name or address you give it, so the certificate must be valid for that name.

### Exit status

| Code | Meaning |
| --- | --- |
| 0 | The listener was stopped with Ctrl+C or SIGTERM. |
| 1 | The listener could not start: address in use, missing certificate file, `--cert` without `--key`, or `--plaintext` without `-T ws`. |

## See Also

- [mesh-test](mesh-test.md): check reachability of agents that are already in the mesh
- [Transports](../concepts/transports.md): differences between QUIC, HTTP/2, and WebSocket
- [TLS certificates](../configuration/tls-certificates.md): create and configure certificates for listeners and peers
- [Probe HTTP API](../api/probe.md): run the same probe from an agent over HTTP
- [Connectivity troubleshooting](../troubleshooting/connectivity.md): diagnose peers that do not connect
