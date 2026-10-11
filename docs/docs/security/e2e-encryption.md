---
title: End-to-End Encryption
description: How Muti Metroo encrypts traffic between the ingress and exit agents, what transit agents can still see, and the limits of that protection.
sidebar_position: 2
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-inspecting.png" alt="Mole with encryption" style={{maxWidth: '180px'}} />
</div>

# End-to-End Encryption

Traffic that crosses the mesh is encrypted twice: once on every peer link by TLS, and once end to end between the two agents that terminate the session (for a SOCKS connection, the ingress and the exit). Transit agents in between forward the end-to-end ciphertext without holding the keys for it.

End-to-end encryption is always on. There is nothing to configure.

## What is encrypted

Every session that carries user data gets its own end-to-end keys:

| Session type | Endpoints |
| ------------ | --------- |
| SOCKS TCP connections | Ingress agent and exit agent |
| Port forwards | Listener agent and endpoint agent |
| UDP relay datagrams | Ingress agent and exit agent |
| ICMP echo (ping) | Ingress agent and target agent |
| Management commands (shell) | Gateway agent and target agent |
| File transfer and file browsing | Gateway agent and target agent |

## How it works

1. When a session opens, the initiating agent generates a fresh X25519 key pair and sends its public key in the open request.
2. The responding agent generates its own fresh X25519 key pair and returns its public key in the reply.
3. Both sides compute the X25519 shared secret and derive a session key with HKDF-SHA256.
4. Every data frame is sealed with ChaCha20-Poly1305 using a nonce built from a direction marker and a counter. A frame that fails authentication, arrives out of order, or repeats an earlier counter is rejected.
5. Transit agents forward the sealed frames unchanged.

Because the key pairs are generated per session and discarded when the session ends, recorded ciphertext cannot be decrypted later by stealing an agent's configuration or data directory (forward secrecy).

Each sealed frame is 28 bytes larger than the plaintext (a 12-byte nonce and a 16-byte authentication tag).

:::note Agent key pair
Each agent also has a persistent X25519 key pair (`agent_key` in the data directory, or `agent.private_key` in the configuration; see [Agent Configuration](/configuration/agent)). Session keys are not derived from it, so losing or rotating it does not affect past or future session encryption.
:::

## What transit agents can see

End-to-end encryption hides payloads, not routing information. A transit agent, or anyone who can read a peer link, can observe:

| Information | Visible to transit |
| ----------- | ------------------ |
| Application payload (HTTP bodies, SSH sessions, file contents, command output) | No |
| Destination IP address and port | Yes |
| Destination hostname, when a [domain route](/features/exit-routing#route-types) is used | Yes |
| The remaining path of agent IDs toward the exit | Yes |
| Session type (TCP, UDP, ICMP, shell, file transfer, port forward key) | Yes |
| Per-datagram UDP destinations, and ICMP sequence numbers | Yes |
| Timing and volume of traffic | Yes |

Peer-link TLS hides all of this from observers on the network between two agents, but not from the agents themselves.

## Limits of the protection

The session key exchange is not authenticated: the public keys in the open request and reply are not signed or tied to an agent identity. The protection therefore depends on the position of the attacker:

| Attacker | Can read payloads? |
| -------- | ------------------ |
| Passive observer on a peer link | No. TLS and end-to-end encryption both apply. |
| Transit agent that relays traffic as designed, or an attacker who later obtains its logs or memory dumps | No. It never holds session keys. |
| Transit agent under an attacker's control that rewrites session setup messages | Yes. It can substitute its own keys toward both ends and decrypt and re-encrypt the session. |
| Active attacker on a peer link where certificates are not verified (`tls.strict: false`, the default) | Yes. It can terminate TLS with its own certificate and then act like a malicious transit. |
| Compromised ingress or exit agent | Yes. It is a session endpoint. |

To narrow these risks:

- Enable certificate verification (`tls.strict: true`) and mutual TLS (`tls.mtls: true`) so that only holders of certificates from your CA can sit on a peer link. See [TLS and mTLS](/security/tls-mtls).
- Only connect peers and transit agents that you operate or trust.
- Use an application-layer protocol with its own authentication (HTTPS with certificate checks, SSH with host keys) for traffic that must stay confidential even if a transit agent is compromised.

## End-to-end versus management encryption

Muti Metroo has a second, optional encryption system that protects topology information rather than traffic:

| | End-to-end encryption | Management encryption |
| - | --------------------- | --------------------- |
| Protects | Session payloads | Node information advertisements (hostnames, OS, IPs, display names) |
| Keys | Generated per session | One X25519 key pair shared by the mesh, configured by the operator |
| Enabled | Always | When `management.public_key` is set |

Route advertisements themselves (prefixes, domain patterns, paths, forward keys) are not encrypted by management encryption. See [Management Key Configuration](/configuration/management).

## Troubleshooting

### Sessions fail after an upgrade

Session encryption is negotiated per session by both endpoints. If sessions fail with decryption or "encryption required" errors after upgrading only some agents, upgrade every agent on the path to the same release.

### Agent identity files are missing

The persistent agent key pair and agent ID are created by `muti-metroo init`. If `agent_key` was deleted, the agent creates a new key pair on the next start. See [init](/cli/init) and [Agent Configuration](/configuration/agent).

## See Also

- [TLS and mTLS](/security/tls-mtls): protect peer links and restrict who can join the mesh
- [Security Overview](/security/overview): the full set of security controls
- [Management Key Configuration](/configuration/management): encrypt topology information
- [Streams](/concepts/streams): how sessions are carried across the mesh
- [Best Practices](/security/best-practices): production hardening
