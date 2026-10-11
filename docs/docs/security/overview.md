---
title: Security Overview
description: The security controls in Muti Metroo, what each protects against, what it does not, and a checklist for hardening a deployment.
sidebar_position: 1
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-inspecting.png" alt="Mole inspecting security" style={{maxWidth: '180px'}} />
</div>

# Security Overview

Muti Metroo layers several independent controls. Some are always on; most must be configured. This page summarizes them and links to the page that explains each one.

## Controls

| Layer | Mechanism | Default |
| ----- | --------- | ------- |
| [End-to-end encryption](/security/e2e-encryption) | Per-session X25519 key exchange, ChaCha20-Poly1305 | Always on |
| [Peer link encryption](/security/tls-mtls) | TLS 1.3 | Always on, certificates not verified |
| [Peer authentication](/security/tls-mtls#mutual-tls) | Certificates from your CA, mutual TLS | Off |
| [SOCKS authentication](/security/authentication#socks-authentication) | Username and bcrypt-hashed password | Off |
| [HTTP API authentication](/security/authentication#http-api-token) | Bearer token checked against a bcrypt hash | Off |
| [Privileged operations](/security/authentication#management-signing-key) | Ed25519 signature with the mesh signing key | Disabled until keys are configured |
| [Sleep and wake](/security/authentication#sleep-and-wake) | Ed25519 signature with the mesh signing key | Unsigned commands accepted until keys are configured |
| [Exit routes](/security/access-control#exit-routes) | Exit agents only connect to TCP destinations in their routes | Exit role off |
| [Command whitelist](/security/access-control#management-command-whitelist) | Only listed commands run | Management commands off |
| [File paths](/security/access-control#file-transfer-paths) | Only listed paths are accessible | File transfer off |
| [Topology encryption](/configuration/management) | Node information sealed to a management key | Off |

## What is protected

| Threat | Protection | Condition |
| ------ | ---------- | --------- |
| Someone records traffic between agents | TLS on every link; end-to-end encryption of payloads | Always |
| A transit agent's operator inspects relayed traffic | End-to-end encryption; transit agents never hold session keys | The transit agent relays as designed |
| Someone modifies or replays encrypted frames | Authenticated encryption with per-frame counters | Always |
| An unknown host connects as a peer | Listeners require a certificate from your CA | `mtls: true` |
| An agent dials an impostor | Listener certificate checked against your CA | `strict: true` |
| An unauthorized client uses the proxy | SOCKS username and password | `socks5.auth.enabled: true`, or a local-only bind address |
| Someone calls the HTTP API | Bearer token | `http.token_hash` set, or a local-only bind address |
| Someone runs commands or transfers files on an agent | Signature from the mesh signing key, whitelist, allowed paths | Feature enabled with keys configured |
| Someone puts the mesh to sleep | Signature from the mesh signing key | `signing_public_key` on every agent, and every agent's HTTP API protected |

## What is not protected

| Exposure | Notes |
| -------- | ----- |
| Routing metadata | Transit agents see destination addresses and ports, hostnames for domain routes, the remaining path, the session type, and traffic timing and volume. |
| A malicious transit agent | Session key exchange is not authenticated. An agent that rewrites session setup can read and alter the sessions it relays. |
| Active interception of unverified links | With the default TLS settings, an attacker who intercepts a peer connection can act as a malicious transit agent. |
| Agent identity | Agent IDs in the peer handshake are self-asserted at every TLS mode, and the identity key pair never authenticates the link. On an unauthenticated link, any reachable endpoint can join the mesh as any agent ID and inject route advertisements. Pin `peers[].id` where identity matters. See [TLS and mTLS](/security/tls-mtls#agent-ids-are-self-asserted). |
| Compromised ingress or exit agents | They are session endpoints and see plaintext. |
| Operator agents' HTTP API | An operator agent signs every privileged request that reaches its API. Access to that API grants control of the mesh. |
| File browse on operator agents | An agent that holds the signing private key accepts unsigned file browse requests (including `chmod` and `delete`) from other agents. See [Authentication](/security/authentication#how-a-request-is-signed). |
| Unrouted SOCKS destinations | The ingress agent dials them directly from its own host. |
| UDP and ICMP destinations | Exit routes do not restrict them. Disable `udp` and `icmp` where they are not needed. |
| Certificate revocation | There is no CRL or OCSP support; replacing the CA is the only way to revoke a certificate. |
| Audit trail | Agents do not keep an audit log of management commands, file transfers, or configuration changes. |

## Hardening checklist

### Every deployment

- [ ] SOCKS listener bound to `127.0.0.1`, or authentication enabled
- [ ] HTTP API bound to `127.0.0.1`, or `http.token_hash` set
- [ ] Exit routes limited to the networks that are needed
- [ ] Management commands and file transfer left disabled unless needed

### Links across networks you do not control

- [ ] Certificates issued from your own CA to every agent
- [ ] `tls.strict: true` and `tls.mtls: true`
- [ ] CA private key kept off the agents

### Management features in use

- [ ] Signing key pair generated; public key on every agent, private key only on operator agents
- [ ] Operator agents' HTTP API local-only and token-protected
- [ ] File transfer disabled on operator agents
- [ ] Narrow `shell.whitelist` without interpreters or shells
- [ ] `file_transfer.allowed_paths` limited to specific directories
- [ ] `udp.enabled: false` and `icmp.enabled: false` where they are not needed
- [ ] Agent clocks synchronized

[Best Practices](/security/best-practices) covers certificate rotation, key handling, system hardening, and incident response.

## Reporting security issues

Report vulnerabilities privately through [GitHub security advisories](https://github.com/postalsys/Muti-Metroo/security/advisories/new) rather than in a public issue. Include the affected version and reproduction steps.

## See Also

- [End-to-End Encryption](/security/e2e-encryption): what payload encryption protects and its limits
- [TLS and mTLS](/security/tls-mtls): verify peers and restrict who can join
- [Authentication](/security/authentication): SOCKS, HTTP API, and signing key
- [Access Control](/security/access-control): what authenticated users can reach
- [Best Practices](/security/best-practices): production hardening
