---
title: TLS and mTLS
sidebar_position: 3
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-inspecting.png" alt="Mole securing connections" style={{maxWidth: '180px'}} />
</div>

# TLS and Mutual TLS

## Layered Security Model

Muti Metroo uses a **layered security model**:

1. **End-to-End Encryption (Primary)**: X25519 key exchange + ChaCha20-Poly1305 encrypts all stream data. Transit agents cannot decrypt traffic - only ingress and exit agents can read the payload.

2. **Transport TLS (Defense-in-depth)**: TLS 1.3 encrypts the transport layer. This provides an additional encryption layer but is not the primary security mechanism.

Because of the E2E encryption layer, **TLS certificate verification is optional by default**. Agents auto-generate self-signed certificates and don't verify peer certificates. This allows quick deployment without PKI setup while maintaining strong security guarantees.

:::tip Configuration
See [TLS Certificates Configuration](/configuration/tls-certificates) for all options including certificate paths, strict mode, and mTLS setup.
:::

## TLS Verification Modes

| Mode | Certificate Verification | Use Case |
|------|-------------------------|----------|
| **Default** | None (auto-generated certs) | Quick deployment, development |
| **Strict** | CA-based verification | Production, compliance |
| **Strict + mTLS** | Mutual authentication | Zero-trust, high-security |

## Why Default No-Verification is Safe

The default configuration doesn't verify TLS certificates, yet remains secure:

1. **E2E Encryption**: Stream data is encrypted with ChaCha20-Poly1305 using keys derived from X25519 exchange. Even if a MITM intercepts TLS, they cannot decrypt stream content.

2. **Agent ID Verification**: Peers verify each other's Agent ID during handshake. This is independent of TLS certificates.

3. **Double Encryption**: Traffic is encrypted twice - once at the E2E layer, once at TLS. A MITM would need to break both.

## When to Enable Strict Mode

Enable `strict: true` when you need:

- **Defense-in-depth**: Additional validation beyond E2E encryption
- **Network access control**: Reject connections from agents without valid certificates
- **Compliance requirements**: Environments requiring PKI-based authentication
- **Early MITM detection**: TLS verification can detect interception attempts earlier

## When to Enable mTLS

Enable `mtls: true` (mutual TLS) when you need:

- **Mutual authentication**: Both sides must present valid certificates
- **Network-level access control**: Only agents with valid certs can connect
- **Early rejection**: Unauthorized connections rejected at TLS, before protocol handshake
- **Zero-trust compliance**: Required for many security frameworks

## Certificate Requirements

**Important**: Muti Metroo only accepts EC (Elliptic Curve) certificates. RSA certificates are rejected.

Generate certificates using the built-in CLI:

```bash
# Generate CA
muti-metroo cert ca --cn "My Mesh CA" -o ./certs

# Generate agent certificate
muti-metroo cert agent --cn "agent-1" \
  --ca ./certs/ca.crt \
  --ca-key ./certs/ca.key \
  -o ./certs
```

See [cert command](/cli/cert) for full options.

## Mixed TLS Environments

When connecting through public proxies (like nginx with Let's Encrypt) while using strict TLS internally, you can disable verification for specific peers:

- Internal peers: Verified against your internal CA
- Public proxy peers: Skip verification (E2E encryption still protects traffic)

This is safe because E2E encryption protects all traffic regardless of TLS verification status.

## Security Properties

### What TLS Verification Adds

| Feature | Without Strict | With Strict | With mTLS |
|---------|---------------|-------------|-----------|
| Transport encryption | Yes | Yes | Yes |
| E2E encryption | Yes | Yes | Yes |
| Server identity verification | No | Yes | Yes |
| Client identity verification | No | No | Yes |
| Early MITM detection | No | Yes | Yes |
| Network-level access control | No | Partial | Full |

### What's Always Protected

Regardless of TLS mode, your traffic is protected by:
- E2E encryption (ChaCha20-Poly1305)
- Agent ID verification during handshake
- Integrity checking (AEAD authentication)

## Troubleshooting

### Certificate Not Trusted

```
Error: x509: certificate signed by unknown authority
```

**Cause:** `strict: true` is enabled but the CA certificate is missing or incorrect.

**Solutions:**
- Verify CA certificate path is correct
- Check certificate was signed by the configured CA: `openssl verify -CAfile ca.crt agent.crt`

### Certificate Expired

```
Error: x509: certificate has expired
```

**Solution:** Check expiration with `openssl x509 -enddate -noout -in agent.crt` and regenerate if needed.

### RSA Certificate Rejected

```
Error: certificate must use ECDSA, got RSA
```

**Cause:** Muti Metroo only accepts EC certificates.

**Solution:** Regenerate using the CLI: `muti-metroo cert agent ...`

### Client Certificate Required

```
Error: tls: client didn't provide a certificate
```

**Cause:** The listener has mTLS enabled but the connecting peer doesn't have a certificate configured.

**Solution:** Configure `tls.cert` and `tls.key` on the connecting agent.

## Decision Guide

| Scenario | Recommended Mode |
|----------|-----------------|
| Development/testing | Default (no verification) |
| Internal mesh, trusted network | Default or Strict |
| Production with compliance needs | Strict |
| Zero-trust environment | Strict + mTLS |
| Mixed internal/external peers | Per-peer overrides |

## Next Steps

- [TLS Certificates Configuration](/configuration/tls-certificates) - Full configuration reference
- [Authentication](/security/authentication) - Additional authentication layers
- [Best Practices](/security/best-practices) - Production hardening
