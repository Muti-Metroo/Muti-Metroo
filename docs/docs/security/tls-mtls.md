---
title: TLS and mTLS
description: How peer connections use TLS 1.3, what the default unverified mode does and does not protect against, and when to enable strict verification and mutual TLS.
sidebar_position: 3
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-inspecting.png" alt="Mole securing connections" style={{maxWidth: '180px'}} />
</div>

# TLS and mTLS

Every peer connection (QUIC, HTTP/2, or WebSocket) runs over TLS 1.3; older TLS versions are refused. The one exception is a WebSocket listener with `plaintext: true`, intended to sit behind a reverse proxy that terminates TLS.

How much TLS protects depends on whether certificates are verified. Muti Metroo has three modes:

| Mode | Settings | Who can sit on a peer link |
| ---- | -------- | -------------------------- |
| Default | No `tls` section, or `strict: false` | Anyone who can reach the listener, and anyone who can intercept the connection |
| Strict | `strict: true` and a CA | Listeners must present a certificate from your CA |
| Strict with mutual TLS | `strict: true`, `mtls: true`, and a CA | Both sides must present certificates from your CA |

Certificate keys and paths are described in [TLS Certificates Configuration](/configuration/tls-certificates).

## Default mode

Without TLS configuration, each agent generates a self-signed ECDSA P-256 certificate in memory at startup, and dialing agents accept any certificate. Agents can connect with no certificate setup.

In this mode:

- Traffic on each link is encrypted, so a passive observer on the network sees neither payloads nor destinations.
- A dialing agent cannot tell its real peer from an impostor. An attacker who can intercept the connection (for example by DNS spoofing or on a shared network) can terminate TLS with its own certificate and relay traffic as if it were a transit agent.
- Because [end-to-end session keys](/security/e2e-encryption#limits-of-the-protection) are not authenticated either, such an attacker can also read and change the session payloads that pass through it.
- Any host that can reach a listener can join the mesh as a peer.

Use the default mode for development and for meshes whose links run over networks you control. Use strict mode with mutual TLS when links cross networks you do not control.

## Strict verification

With `strict: true`, a dialing agent checks the listener's certificate against the configured CA and checks that it covers the address being dialed. A connection to a listener with a self-signed or foreign certificate fails.

```yaml
tls:
  ca: "./certs/ca.crt"
  cert: "./certs/agent-1.crt"
  key: "./certs/agent-1.key"
  strict: true
```

Strict mode can be set per peer with `peers[].tls.strict`, and a peer can use its own CA with `peers[].tls.ca`. This lets an agent verify internal peers while accepting, for example, a peer reached through a public reverse proxy whose certificate comes from a public CA.

## Mutual TLS

With `mtls: true`, listeners require the connecting agent to present a client certificate signed by the global `tls.ca`. Connections without one are refused during the TLS handshake, before any Muti Metroo traffic is exchanged. Dialing agents present their `tls.cert` as the client certificate.

```yaml
tls:
  ca: "./certs/ca.crt"
  cert: "./certs/agent-1.crt"
  key: "./certs/agent-1.key"
  strict: true
  mtls: true
```

`mtls` works independently of `strict` and can be turned off for a single listener with `listeners[].tls.mtls: false`. Combine both settings so that each side verifies the other.

## What certificates prove

A valid certificate proves that its holder was issued a certificate by your CA. It does not prove which agent the holder is:

- Certificates are not tied to agent IDs. Any agent with a valid certificate can claim any agent ID.
- The `id` in a peer entry is checked against the ID the remote agent reports, which detects misconfiguration but is not an authentication mechanism.
- There is no certificate revocation (CRL or OCSP) and no certificate pinning. To remove an agent's access, issue new certificates from a new CA to the remaining agents, or restrict the agent at the network level.

Treat every holder of a certificate from your CA as a trusted member of the mesh.

## Agent IDs are self-asserted

The agent ID exchanged in the peer handshake is self-asserted at every TLS mode, including mutual TLS. The persistent X25519 identity key pair each agent generates (see [End-to-End Encryption](/security/e2e-encryption)) is used for ECDH key exchange and is distributed through node information, but it never authenticates the peer link: the handshake carries no signature or challenge that would prove possession of the identity key.

Two consequences:

- On an unauthenticated link (the default), any reachable endpoint that completes the handshake can join the mesh under any agent ID, and can then receive and inject route advertisements.
- Even with mutual TLS, proof stops at "holds a certificate from your CA": the agent ID it claims is not bound to the certificate.

If a peer's identity matters (rather than just its membership), pin the expected ID with `peers[].id` in addition to certificate verification. The pin is checked on every (re)connection, so an impostor that joins with a valid certificate but the wrong ID is refused.

## Generate certificates

Muti Metroo only accepts ECDSA certificates and keys for its own certificate and CA. RSA and Ed25519 are rejected. The built-in commands generate ECDSA P-256:

```bash
# Certificate authority
muti-metroo cert ca --cn "Example Mesh CA" -o ./certs

# Agent certificate, valid for the addresses peers will dial
muti-metroo cert agent --cn agent-1 \
  --dns agent-1.example.com --ip 203.0.113.10 \
  --ca ./certs/ca.crt --ca-key ./certs/ca.key -o ./certs
```

Agent certificates always include the common name, `localhost`, `127.0.0.1`, and `::1`; add every hostname and IP address that peers use to reach the agent. Certificates are valid for 365 days unless `--days` says otherwise. See [cert CLI](/cli/cert).

## Troubleshooting

### x509: certificate signed by unknown authority

The dialing agent has `strict: true` and the listener's certificate was not issued by the configured CA. Check the CA path, and verify the certificate with `openssl verify -CAfile ca.crt agent-1.crt`.

### x509: certificate is valid for ..., not ...

The listener's certificate does not include the address the peer dials. Issue a new certificate with the right `--dns` or `--ip` values.

### x509: certificate has expired

Check the date with `muti-metroo cert info ./certs/agent-1.crt` and issue a new certificate. `muti-metroo run` also warns at startup when a configured certificate expires within 60 days.

### RSA certificates are not supported

```text
RSA certificates are not supported; use EC (ECDSA) certificates
```

Issue an ECDSA certificate with `muti-metroo cert` or OpenSSL.

### tls: certificate required

The listener has `mtls: true` and the dialing agent presented no client certificate. Configure `tls.cert` and `tls.key` on the dialing agent.

## See Also

- [TLS Certificates Configuration](/configuration/tls-certificates): every TLS key, per-peer and per-listener overrides
- [cert CLI](/cli/cert): create and inspect certificates
- [End-to-End Encryption](/security/e2e-encryption): the encryption layer inside the TLS connection
- [Best Practices](/security/best-practices): certificate rotation and CA protection
- [Transports](/concepts/transports): QUIC, HTTP/2, and WebSocket
