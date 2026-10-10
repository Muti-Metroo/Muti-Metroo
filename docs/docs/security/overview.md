---
title: Security Overview
sidebar_position: 1
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-inspecting.png" alt="Mole inspecting security" style={{maxWidth: '180px'}} />
</div>

# Security Overview

Your traffic stays private even when it passes through transit nodes you don't control. Only the ingress and exit see your data - everything in between sees encrypted bytes they cannot decrypt.

**What this means for you:**

- Compromise a transit node? They see nothing useful
- Network tapped between agents? Traffic is encrypted
- Unauthorized client tries to connect? Rejected by authentication

## Security Model

### End-to-End Encryption

Transit nodes relay your traffic without being able to read it:

- Your data is encrypted before leaving the ingress
- Only the exit node can decrypt it
- Even if a transit is compromised, your data stays private

See [End-to-End Encryption](/security/e2e-encryption) for details.

### Transport Security

All peer connections use TLS 1.3:

- **Encryption**: All traffic encrypted in transit
- **Authentication**: Server certificates validated
- **Mutual TLS**: Optional client certificate authentication
- **Perfect Forward Secrecy**: Session keys derived per connection

### Authentication Layers

| Layer                    | Mechanism                         | Purpose                 |
| ------------------------ | --------------------------------- | ----------------------- |
| E2E                      | X25519 + ChaCha20-Poly1305        | Stream data encryption  |
| TLS                      | Certificates                      | Peer authentication     |
| mTLS                     | Client certs                      | Mutual authentication   |
| HTTP API                 | Bearer token (bcrypt-hashed)      | Local/remote API access |
| SOCKS5                   | Username/password (bcrypt-hashed) | Client authentication   |
| Management commands      | Ed25519 management signature      | Command authorization   |
| File Transfer            | Ed25519 management signature      | Transfer authorization  |
| Dynamic config mutations | Ed25519 management signature      | Mutation authorization  |
| Sleep/Wake               | Ed25519 management signature      | Command authentication  |

### Authorization

- **Route-based ACL**: Exit only allows configured CIDRs
- **Command whitelist**: Only whitelisted management commands allowed
- **File path restrictions**: Only allowed paths accessible

## What You're Protected Against

| If This Happens...                         | You're Protected Because...                      |
| ------------------------------------------ | ------------------------------------------------ |
| Someone captures traffic between agents    | It's encrypted - they see random bytes           |
| Transit node is compromised                | E2E encryption - transit can't decrypt your data |
| Someone replays captured traffic           | Nonce-based encryption detects replays           |
| Someone modifies traffic in transit        | Authenticated encryption detects tampering       |
| Unauthorized peer tries to connect         | mTLS rejects unknown certificates                |
| Unauthorized client tries to use proxy     | SOCKS5 authentication blocks them                |
| Someone tries to run unauthorized commands | Command whitelist blocks unapproved commands     |
| Someone tries to hibernate your mesh       | Signing keys verify sleep/wake commands          |

## What You Need to Protect Yourself

| Risk                                   | Your Responsibility                                                       |
| -------------------------------------- | ------------------------------------------------------------------------- |
| CA private key stolen                  | Store it securely (HSM, vault, encrypted disk)                            |
| Ingress or exit host compromised       | Harden your endpoints - they see your data                                |
| Local network sees connection metadata | Payloads are encrypted; timing and volume remain visible, as with any VPN |
| Insider with valid credentials         | Monitor logs, rotate credentials                                          |

## Security Checklist

### Minimum Security

- [ ] TLS certificates generated and deployed
- [ ] Certificate CA key secured
- [ ] SOCKS5 bound to localhost or authenticated
- [ ] Exit routes restricted to needed CIDRs

### Recommended Security

- [ ] Mutual TLS enabled
- [ ] SOCKS5 authentication enabled
- [ ] Mesh management signing keypair generated and the public half deployed to every agent (private half kept on operator nodes only)
- [ ] Management commands disabled or restricted with a strict whitelist
- [ ] File transfer disabled or restricted
- [ ] Monitoring and alerting configured
- [ ] Regular certificate rotation

### Production Security

- [ ] All of the above
- [ ] CA key in HSM or secure vault
- [ ] Network segmentation
- [ ] Intrusion detection
- [ ] Audit logging
- [ ] Incident response plan

## Quick Hardening

Apply these four changes for immediate security improvement:

1. **Disable unused features** - Management commands and file transfer are disabled by default. Keep them disabled unless needed. See [Management Commands](/configuration/shell) and [File Transfer](/configuration/file-transfer) configuration.

2. **Restrict SOCKS5 access** - Bind to localhost and enable authentication. See [SOCKS5 Configuration](/configuration/socks5).

3. **Restrict exit routes** - Only advertise specific CIDRs, not `0.0.0.0/0`. See [Exit Configuration](/configuration/exit).

4. **Enable mTLS** - Require valid client certificates for peer connections. See [TLS Configuration](/configuration/tls-certificates).

For detailed hardening guidance, see [Best Practices](/security/best-practices).

## Security Topics

| Topic                                      | Description                     |
| ------------------------------------------ | ------------------------------- |
| [E2E Encryption](/security/e2e-encryption) | Stream payload encryption       |
| [TLS/mTLS](/security/tls-mtls)             | Certificate-based security      |
| [Authentication](/security/authentication) | Client and shell authentication |
| [Access Control](/security/access-control) | Route and command restrictions  |
| [Best Practices](/security/best-practices) | Production hardening guide      |

## Reporting Security Issues

If you discover a security vulnerability:

1. Do NOT open a public issue
2. Contact the maintainers privately
3. Provide detailed reproduction steps
4. Allow reasonable time for fix before disclosure

## Next Steps

- [End-to-End Encryption](/security/e2e-encryption)
- [TLS/mTLS Configuration](/security/tls-mtls)
- [Authentication](/security/authentication)
- [Best Practices](/security/best-practices)
