# TLS Certificates

TLS certificates are **optional** in Muti Metroo. By default, agents auto-generate self-signed certificates and don't verify peer certificates. This is safe because the end-to-end encryption layer (X25519 + ChaCha20-Poly1305) protects all traffic regardless of transport security.

For higher-trust environments, enable strict TLS verification with CA-signed certificates.

## Quick Start Options

| Approach | Setup | Security |
|----------|-------|----------|
| **Default (auto-generate)** | None - just run | E2E encryption, no TLS verification |
| **Embedded certs** | Wizard generates and embeds in config | E2E + consistent TLS identity |
| **Strict TLS** | CA + certs + `strict: true` | E2E + TLS verification |
| **Strict + mTLS** | CA + certs + `strict: true` + `mtls: true` | E2E + mutual TLS verification |

## Default Behavior

When no TLS configuration is provided:

- Agent auto-generates ECDSA (P-256) self-signed certificate on startup
- Certificates are regenerated each startup (ephemeral)
- Peer certificates are NOT verified
- Traffic is still encrypted (TLS 1.3 + E2E)

```yaml
# Minimal config - TLS handled automatically
agent:
  id: "auto"
  data_dir: "./data"

listeners:
  - transport: quic
    address: "0.0.0.0:4433"
```

## Certificate Types

| Type | Command | Server Auth | Client Auth | Use Case |
|------|---------|:-----------:|:-----------:|----------|
| CA | `cert ca` | N/A | N/A | Sign other certificates |
| Agent | `cert agent` | Yes | Yes | Listeners and peer connections |
| Client | `cert client` | No | Yes | Client-only connections |

## Generate Certificate Authority

Create a CA to sign all certificates in your mesh:

```bash
muti-metroo cert ca --cn "Mesh CA" -o ./certs
```

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--cn` | "Muti Metroo CA" | Common name for the CA |
| `-o, --out` | ./certs | Output directory |
| `--days` | 365 | Validity period in days |

**Output files:**
- `ca.crt` - CA certificate (share with all agents)
- `ca.key` - CA private key (keep secure!)

**Security**: Keep `ca.key` secure. Anyone with access can create valid certificates for your mesh.

## Generate Agent Certificate

Create a certificate for an agent (supports both server and client authentication):

```bash
muti-metroo cert agent --cn "agent-1" \
  --ca ./certs/ca.crt \
  --ca-key ./certs/ca.key \
  --dns agent1.example.com \
  --ip 192.168.1.10 \
  -o ./certs
```

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--cn` | (required) | Common name for the certificate |
| `--dns` | | Additional DNS names (comma-separated) |
| `--ip` | | Additional IP addresses (comma-separated) |
| `-o, --out` | ./certs | Output directory |
| `--days` | 365 | Validity period (matches the CA; pick a shorter value for high-rotation environments) |
| `--ca` | ./certs/ca.crt | CA certificate path |
| `--ca-key` | ./certs/ca.key | CA private key path |

**Output files:**
- `<name>.crt` - Agent certificate (named after common name)
- `<name>.key` - Agent private key

## Generate Client Certificate

Create a client-only certificate (cannot be used for server authentication):

```bash
muti-metroo cert client --cn "admin" \
  --ca ./certs/ca.crt \
  --ca-key ./certs/ca.key \
  -o ./certs
```

## View Certificate Information

Display detailed information about a certificate:

```bash
muti-metroo cert info ./certs/agent-1.crt
```

**Example output:**

```
Certificate: ./certs/agent-1.crt

Subject:      CN=agent-1,O=Muti Metroo
Issuer:       CN=Mesh CA,O=Muti Metroo
Serial:       1a2b3c4d5e6f...
Fingerprint:  sha256:ab12cd34...
Is CA:        false
Not Before:   2026-01-01T00:00:00Z
Not After:    2027-01-01T00:00:00Z
Status:       Valid (365 days left)
DNS Names:    agent-1, localhost, agent1.example.com
IP Addresses: 127.0.0.1, ::1, 192.168.1.10
Key Usage:    KeyEncipherment, DigitalSignature
Ext Key Usage: ServerAuth, ClientAuth
```

## Configuration

### Global TLS Configuration

Configure TLS settings at the top level to apply to all listeners and peers:

```yaml
tls:
  # Strict TLS verification (default: false)
  # When true, peer certificates are verified against the CA
  strict: true

  # CA certificate (required for strict mode and mTLS)
  ca: "./certs/ca.crt"

  # Agent certificate (optional - auto-generated if not set)
  cert: "./certs/agent.crt"
  key: "./certs/agent.key"

  # Mutual TLS - require client certificates (default: false)
  mtls: true
```

### Strict Mode

Enable `strict: true` to verify peer certificates:

```yaml
tls:
  ca: "./certs/ca.crt"
  cert: "./certs/agent.crt"
  key: "./certs/agent.key"
  strict: true
```

With strict mode:
- Peer certificates are verified against the CA
- Invalid certificates are rejected at TLS layer
- Requires CA to be configured

### Per-Listener Override

Override TLS settings for specific listeners:

```yaml
listeners:
  - transport: quic
    address: "0.0.0.0:4433"
    tls:
      cert: "./certs/listener-specific.crt"
      key: "./certs/listener-specific.key"
      mtls: false  # Disable mTLS for this listener
```

### Per-Peer Override

Override TLS settings for specific peer connections:

```yaml
peers:
  - id: "abc123..."
    transport: quic
    address: "192.168.1.10:4433"
    tls:
      ca: "./certs/other-ca.crt"
      strict: true  # Enable strict mode for this peer only
```

### Mixed Environments: Public Proxy with Internal CA

When using strict TLS internally but connecting through a public proxy (nginx, Cloudflare, etc.) with a public CA (Let's Encrypt), disable strict verification for that specific connection:

```yaml
tls:
  # Global: strict verification with your internal CA
  ca_pem: |
    -----BEGIN CERTIFICATE-----
    ... your internal CA ...
    -----END CERTIFICATE-----
  cert_pem: |
    ... agent cert ...
  key_pem: |
    ... agent key ...
  strict: true

peers:
  # Internal peers: verified against internal CA
  - id: "abc123..."
    transport: quic
    address: "192.168.1.10:4433"

  # Public proxy (nginx with Let's Encrypt): skip TLS verification
  - id: "def456..."
    transport: ws
    address: "wss://relay.example.com:443/mesh"
    tls:
      strict: false   # Override global strict for this peer
```

**Security note:** This is safe because E2E encryption (X25519 + ChaCha20-Poly1305) protects all mesh traffic regardless of TLS verification. The public proxy cannot read or modify the encrypted stream data.

## Mutual TLS (mTLS)

Enable mTLS to require client certificates on listeners:

```yaml
tls:
  ca: "./certs/ca.crt"
  cert: "./certs/agent.crt"
  key: "./certs/agent.key"
  strict: true
  mtls: true
```

With mTLS enabled:
- Listeners require connecting peers to present a valid client certificate
- Peers must have a certificate signed by a trusted CA
- The agent certificate is used as the client certificate

## Inline Certificates

Embed certificates directly in config (wizard can generate these):

```yaml
tls:
  ca_pem: |
    -----BEGIN CERTIFICATE-----
    MIICpDCCAYwCCQC...
    -----END CERTIFICATE-----
  cert_pem: |
    -----BEGIN CERTIFICATE-----
    MIICpDCCAYwCCQC...
    -----END CERTIFICATE-----
  key_pem: |
    -----BEGIN PRIVATE KEY-----
    MIIEvgIBADANBg...
    -----END PRIVATE KEY-----
  strict: true
```

Inline PEM takes precedence over file paths if both are specified.

## Setup Wizard Options

The setup wizard (`muti-metroo setup`) offers these TLS options:

1. **Self-signed certificates (Recommended)** - Auto-generate at startup, no config needed
2. **Strict TLS with CA verification (Advanced)** - CA-based verification with options:
   - Paste CA certificate, agent certificate, and key
   - Generate from CA private key (wizard derives CA cert and generates agent cert)

## Certificate Rotation

To rotate certificates without downtime:

1. Generate new certificates with the same CA
2. Update configuration with new certificate paths
3. Restart the agent

```bash
# Generate new certificate
muti-metroo cert agent --cn "agent-1-new" \
  --ca ./certs/ca.crt \
  --ca-key ./certs/ca.key \
  -o ./certs

# Update config.yaml to use new certificate
# Restart agent
```

## Best Practices

1. **Start with defaults**: E2E encryption provides strong security without PKI setup
2. **Use strict mode for production**: When defense-in-depth is required
3. **Protect CA key**: Store `ca.key` separately from agents
4. **Use short validity**: 90 days for agent certs, rotate regularly
5. **Include SANs**: Add DNS names and IPs that agents will use
6. **Separate CAs**: Use different CAs for different environments
7. **Monitor expiration**: Check certificate validity regularly
