---
title: Best Practices
sidebar_position: 6
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-inspecting.png" alt="Mole recommending best practices" style={{maxWidth: '180px'}} />
</div>

# Security Best Practices

Production deployments need additional hardening beyond the secure defaults. This guide covers certificate management, system hardening, and incident response.

**The three most important things:**

1. **Protect your CA private key** - if stolen, unauthorized parties can create valid certificates
2. **Enable mTLS** - only agents with your certificates can connect
3. **Disable unused features** - shell and file transfer are disabled by default for a reason

## Certificate Management

### Protect CA Private Key

If someone gets your CA key, they can create certificates that your mesh will trust.

**Secure storage options:**

- Hardware Security Module (HSM)
- Cloud KMS (AWS KMS, GCP KMS, Azure Key Vault)
- HashiCorp Vault
- Encrypted disk with strong passphrase

**Never:**

- Store in version control
- Leave on shared systems
- Transmit over unencrypted channels

### Certificate Validity

Use short validity periods to limit exposure if keys are compromised:

| Certificate        | Recommended Validity |
| ------------------ | -------------------- |
| CA certificate     | 1-3 years            |
| Agent certificates | 90 days              |

```bash
# Generate CA with 1-year validity
muti-metroo cert ca --days 365

# Generate agent cert with 90-day validity
muti-metroo cert agent --days 90 --ca ./ca.crt --ca-key ./ca.key
```

### Automate Certificate Rotation

Example rotation script:

```bash
#!/bin/bash
# rotate-cert.sh - run via cron
CERT_DIR="/etc/muti-metroo/certs"
CA_DIR="/etc/muti-metroo/ca"

# Calculate days until expiration
DAYS_LEFT=$(( ($(date -d "$(openssl x509 -enddate -noout -in $CERT_DIR/agent.crt | cut -d= -f2)" +%s) - $(date +%s)) / 86400 ))

if [ $DAYS_LEFT -lt 30 ]; then
    muti-metroo cert agent --cn "$(hostname)" \
      --ca "$CA_DIR/ca.crt" \
      --ca-key "$CA_DIR/ca.key" \
      -o "$CERT_DIR"
    systemctl restart muti-metroo
fi
```

### Monitor Certificate Expiration

```bash
#!/bin/bash
# check-cert-expiry.sh - run daily via monitoring
CERT="/etc/muti-metroo/certs/agent.crt"
DAYS_LEFT=$(( ($(date -d "$(openssl x509 -enddate -noout -in $CERT | cut -d= -f2)" +%s) - $(date +%s)) / 86400 ))

if [ $DAYS_LEFT -lt 14 ]; then
    echo "WARNING: Certificate expires in $DAYS_LEFT days"
    exit 1
fi
```

## Signing Key Management

If you use sleep mode in untrusted environments, protect your signing keys with the same care as CA private keys.

### Key Distribution

```
Operator Station      Remote Agents
(has private key)     (public key only)
      |                    |
      | Signed commands    |
      |------------------->|
      |                    | (can verify)
```

- **Public key**: Distribute to ALL agents
- **Private key**: Keep ONLY on operator machines

### Generate Keys Securely

```bash
# Generate on secure machine
muti-metroo signing-key generate > signing-keys.txt

# Extract and store private key securely
# Delete signing-keys.txt after distribution
```

### Key Rotation

To rotate signing keys:

1. Generate new keypair
2. Update public key on all agents (deploy via config management)
3. Update private key on operator nodes
4. Restart agents to pick up new keys
5. Securely delete old private key

### What to Protect

| Key                 | Protection Level | Storage Recommendations                 |
| ------------------- | ---------------- | --------------------------------------- |
| Signing private key | High             | Encrypted vault, password manager       |
| Signing public key  | Low              | Can be in config files, version control |

### Run as Non-Root

Muti Metroo doesn't require root privileges. Create a dedicated user:

```bash
# Create dedicated user
useradd -r -s /sbin/nologin muti-metroo

# Set ownership
chown -R muti-metroo:muti-metroo /var/lib/muti-metroo
```

### Limit Capabilities (systemd)

```ini
[Service]
User=muti-metroo
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
ReadWritePaths=/var/lib/muti-metroo
```

### File Permissions

```bash
chmod 700 /var/lib/muti-metroo          # Data dir - owner only
chmod 640 /etc/muti-metroo/config.yaml  # Config - owner + group
chmod 600 /etc/muti-metroo/certs/*.key  # Private keys - owner only
chmod 644 /etc/muti-metroo/certs/*.crt  # Certificates - world readable
```

## Secure Secrets

### Use Environment Variables

Never hardcode secrets in configuration files:

```yaml
http:
  token_hash: "${HTTP_TOKEN_HASH}"

socks5:
  auth:
    users:
      - username: "${SOCKS5_USER}"
        password_hash: "${SOCKS5_PASSWORD_HASH}"

management:
  signing_public_key: "${MESH_SIGNING_PUBLIC_KEY}"
  signing_private_key: "${MESH_SIGNING_PRIVATE_KEY}" # Operator nodes only
```

### Secret Storage Options

- HashiCorp Vault
- AWS Secrets Manager
- Docker Secrets
- systemd credentials

**Never:**

- Commit secrets to git
- Log secrets
- Store in plain text files

## Logging and Monitoring

### Structured Logging

Enable JSON logging for aggregation:

```yaml
agent:
  log_level: "info"
  log_format: "json"
```

### Key Events to Monitor

- Authentication failures
- Connection rejections
- Certificate errors
- Route changes

### Log Retention

Keep logs for compliance and forensics:

```bash
# Archive logs periodically
journalctl -u muti-metroo --since "30 days ago" > /archive/muti-metroo-$(date +%Y%m%d).log
```

## Incident Response

### Prepare for Compromise

1. **Have backups** of configuration and certificates
2. **Document** certificate rotation procedure
3. **Know how to** revoke certificates quickly
4. **Have contacts** for security team

### If CA is Compromised

1. Generate new CA immediately
2. Generate new certificates for all agents
3. Deploy new certificates to all agents
4. Update all configurations
5. Restart all agents
6. Investigate how compromise occurred

### If Agent is Compromised

1. Disconnect agent from mesh
2. Revoke agent's certificate
3. Investigate compromised host
4. Re-image or clean host
5. Generate new identity and certificates
6. Reconnect to mesh

## Security Checklists

### Pre-Deployment

- [ ] CA key stored securely (HSM, Vault, or encrypted)
- [ ] mTLS enabled for peer connections
- [ ] SOCKS5 authentication enabled (if network-accessible)
- [ ] Management commands disabled or whitelist-restricted
- [ ] File transfer disabled or path-restricted
- [ ] Exit routes minimized to required networks
- [ ] Services bound to appropriate interfaces
- [ ] Signing keys configured (if using sleep mode in untrusted environments)

### Post-Deployment

- [ ] Firewall rules configured
- [ ] Monitoring and alerting set up
- [ ] Log aggregation configured
- [ ] Certificate expiration monitoring active
- [ ] Running as non-root user
- [ ] Incident response plan documented

### Regular Maintenance

- [ ] Rotate certificates before expiration
- [ ] Review access controls quarterly
- [ ] Update to latest version
- [ ] Review security logs
- [ ] Test failover procedures

## Quick Reference

| Security Control      | Configuration                                               |
| --------------------- | ----------------------------------------------------------- |
| Enable mTLS           | [TLS Configuration](/configuration/tls-certificates)        |
| Restrict routes       | [Exit Configuration](/configuration/exit)                   |
| Limit shell commands  | [Shell Configuration](/configuration/shell)                 |
| Restrict file paths   | [File Transfer Configuration](/configuration/file-transfer) |
| SOCKS5 authentication | [SOCKS5 Configuration](/configuration/socks5)               |

## Next Steps

- [TLS/mTLS](/security/tls-mtls) - Certificate security concepts
- [Authentication](/security/authentication) - Password security
- [Access Control](/security/access-control) - Restrict what users can do
