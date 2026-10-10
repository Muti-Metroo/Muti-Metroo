---
title: TLS Certificates
sidebar_position: 7
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-inspecting.png" alt="Mole checking certificates" style={{maxWidth: '180px'}} />
</div>

# TLS Certificate Configuration

Muti Metroo handles TLS automatically - just run the agent and it works. Certificates are auto-generated and verification is disabled by default.

**Why is this safe?** Muti Metroo uses an end-to-end encryption layer (X25519 + ChaCha20-Poly1305) that protects all traffic regardless of transport security. TLS is defense-in-depth, not the primary security mechanism.

**Minimal config (auto-generated certs):**

```yaml
# TLS section can be omitted entirely - agent auto-generates self-signed certs
listeners:
  - transport: quic
    address: "0.0.0.0:4433"
```

**Strict mode with PKI (for high-trust environments):**

```yaml
tls:
  ca: "./certs/ca.crt" # Shared across all agents
  cert: "./certs/agent.crt" # This agent's identity
  key: "./certs/agent.key"
  strict: true # Enable certificate verification
  mtls: true # Require mutual authentication
```

## Default Behavior

When no TLS configuration is provided:

1. **Auto-generated certificates**: Agent creates a self-signed ECDSA (P-256) certificate on startup
2. **No verification**: Peer certificates are not verified (safe due to E2E encryption)
3. **Ephemeral**: Certificates are regenerated each startup (not persisted)
4. **No mTLS**: Client certificates are not required

This allows agents to connect immediately without certificate setup.

## When to Use Strict Mode

Enable `strict: true` when you need:

- **Defense-in-depth**: Additional security layer beyond E2E encryption
- **Network-level authentication**: Verify peer identity at transport layer
- **Compliance requirements**: Environments requiring PKI-based authentication
- **Zero-trust networks**: Where TLS verification is mandatory

## Global TLS Configuration

TLS settings in the global `tls:` section apply to all connections:

```yaml
tls:
  # Enable strict certificate verification (default: false)
  # When true, peer certificates are verified against the CA
  strict: true

  # CA certificate for verifying peers and clients
  # Required when: strict mode or mTLS enabled
  ca: "./certs/ca.crt"

  # Agent's identity certificate (optional - auto-generated if not set)
  cert: "./certs/agent.crt"
  key: "./certs/agent.key"

  # Enable mutual TLS on listeners (require client certificates)
  # Requires CA to be configured
  mtls: true

  # TLS ClientHello profile (client-side only).
  # Disabled by default. Set preset to opt in.
  fingerprint:
    preset: "chrome" # See below for available presets
```

## TLS ClientHello Profiles

Some networks, proxies, and servers expect specific TLS client behavior. Muti Metroo can use [uTLS](https://github.com/refraction-networking/utls) to present a browser-compatible ClientHello on outbound peer connections, which improves compatibility with strict proxies and servers. The feature is **disabled by default** -- operators opt in per deployment.

:::warning WAF interaction
Some WAFs (notably Apache/ZoneOS in front of certain hosts) treat a browser-like ClientHello combined with non-browser HTTP request shapes as a bot signal and reject the dial. If you enable a preset and peers behind such WAFs stop connecting, disable the preset for those routes.
:::

Enable a preset under `tls.fingerprint`:

```yaml
tls:
  fingerprint:
    preset: "firefox"
```

### Available Presets

| Preset                         | Description                                        |
| ------------------------------ | -------------------------------------------------- |
| `chrome`                       | Chrome-like ClientHello                            |
| `firefox`                      | Firefox-like ClientHello                           |
| `safari`                       | Safari-like ClientHello                            |
| `edge`                         | Microsoft Edge-like ClientHello                    |
| `ios`                          | iOS Safari-like ClientHello                        |
| `android`                      | Android Chrome-like ClientHello                    |
| `random`                       | Randomized ClientHello per connection              |
| `go`                           | Go standard library ClientHello (default behavior) |
| `disabled` (or empty, default) | Go standard library TLS                            |

### Transport Support

| Transport | Profile Support                             |
| --------- | ------------------------------------------- |
| HTTP/2    | Full support                                |
| WebSocket | Full support                                |
| QUIC      | Not supported (uses quic-go's internal TLS) |

### Scope

ClientHello profiles only affect **outbound connections** (peer connections). Listeners always use standard Go TLS because servers don't send ClientHello (only clients do).

### Example Configuration

```yaml
tls:
  ca: "./certs/ca.crt"
  cert: "./certs/agent.crt"
  key: "./certs/agent.key"
  fingerprint:
    preset: "chrome"

protocol:
  alpn: "" # Use transport defaults (e.g., "h2")
```

## Protocol Identifiers

The `protocol` section customizes the identifiers agents use to recognize Muti Metroo connections. You can change or disable them to match local network policy.

```yaml
protocol:
  alpn: "muti-metroo/1" # ALPN for QUIC/TLS
  http_header: "X-Muti-Metroo-Protocol" # HTTP/2 header
  ws_subprotocol: "muti-metroo/1" # WebSocket subprotocol
```

### Options

| Option           | Type   | Default            | Description                                  |
| ---------------- | ------ | ------------------ | -------------------------------------------- |
| `alpn`           | string | `muti-metroo/1`          | ALPN identifier for QUIC and TLS connections |
| `http_header`    | string | `X-Muti-Metroo-Protocol` | Custom header for HTTP/2 transport           |
| `ws_subprotocol` | string | `muti-metroo/1`          | WebSocket subprotocol identifier             |

Set any value to an empty string `""` to disable that identifier:

```yaml
protocol:
  alpn: "" # Use transport defaults (e.g., "h2")
  http_header: "" # No custom HTTP header
  ws_subprotocol: "" # No custom subprotocol
```

## Setup Options

### Option 1: Automatic (Recommended for Development)

Leave TLS unconfigured - agent handles everything:

```yaml
agent:
  id: "auto"
  data_dir: "./data"

listeners:
  - transport: quic
    address: "0.0.0.0:4433"

# No tls: section needed
```

### Option 2: Embedded Certificates (Recommended for Production)

Use the setup wizard to generate and embed certificates in config:

```bash
muti-metroo setup
```

The wizard offers two certificate setup options:

1. **Self-signed certificates (Recommended)** - Auto-generate on startup, no certificates needed in config. Traffic is still encrypted with TLS 1.3.

2. **Strict TLS with CA verification (Advanced)** - Enable CA-based certificate verification. The wizard then offers:
   - Paste CA certificate, agent certificate, and key
   - Generate from CA private key (wizard derives CA cert and generates agent cert)

### Option 3: File-Based Certificates

Reference certificate files:

```yaml
tls:
  ca: "./certs/ca.crt"
  cert: "./certs/agent.crt"
  key: "./certs/agent.key"
  strict: true
```

### Option 4: Inline PEM Certificates

Embed certificates directly in config (useful for secrets management or single-file deployment):

```yaml
tls:
  ca_pem: |
    -----BEGIN CERTIFICATE-----
    MIIBkTCB+wIJAKi...
    -----END CERTIFICATE-----
  cert_pem: |
    -----BEGIN CERTIFICATE-----
    MIIBkTCB+wIJAKi...
    -----END CERTIFICATE-----
  key_pem: |
    -----BEGIN EC PRIVATE KEY-----
    MIIEvQIBADANBg...
    -----END EC PRIVATE KEY-----
  strict: true
```

Inline PEM takes precedence over file paths.

### Option 5: Environment Variables

Use environment variables for secrets:

```yaml
tls:
  ca_pem: "${TLS_CA}"
  cert_pem: "${TLS_CERT}"
  key_pem: "${TLS_KEY}"
  strict: true
```

## Certificate Requirements

**Important**: When providing your own certificates, Muti Metroo only accepts EC (Elliptic Curve) certificates. RSA certificates are not supported for the mesh CA and agent certificates.

The only exception is when connecting through a WebSocket proxy to an external server - in this case, the external server may use any certificate type since mTLS is not available through proxies.

## Generating Certificates

### Using the CLI

```bash
# Generate CA (do once, share across mesh)
muti-metroo cert ca --cn "My Mesh CA" -o ./certs

# Generate agent certificate (signed by the CA)
muti-metroo cert agent --cn "agent-1" \
  --ca ./certs/ca.crt \
  --ca-key ./certs/ca.key \
  --dns "agent1.example.com" \
  --ip "192.168.1.10" \
  -o ./certs

# View certificate info
muti-metroo cert info ./certs/agent-1.crt
```

:::tip Default Paths
If you use `-o ./certs` for the CA, the `--ca` and `--ca-key` flags default to `./certs/ca.crt` and `./certs/ca.key`, so you can omit them.
:::

All certificates generated by the CLI use P-256 ECDSA keys.

### Using OpenSSL

```bash
# Generate EC CA key and certificate
openssl ecparam -name prime256v1 -genkey -noout -out ca.key
openssl req -x509 -new -nodes -key ca.key -sha256 -days 365 \
  -out ca.crt -subj "/CN=My Mesh CA"

# Generate EC agent key and CSR
openssl ecparam -name prime256v1 -genkey -noout -out agent.key
openssl req -new -key agent.key -out agent.csr \
  -subj "/CN=agent-1"

# Sign agent certificate
openssl x509 -req -in agent.csr -CA ca.crt -CAkey ca.key \
  -CAcreateserial -out agent.crt -days 365 -sha256
```

## Mutual TLS (mTLS)

mTLS requires both sides to present certificates:

```yaml
tls:
  ca: "./certs/ca.crt"
  cert: "./certs/agent.crt"
  key: "./certs/agent.key"
  strict: true # Verify peer certificates
  mtls: true # Require client certificates on listeners
```

When `mtls: true`:

- Listeners require connecting peers to present valid client certificates
- The CA is used to verify client certificates
- The agent certificate is automatically used by peers as their client certificate

### Benefits of mTLS

- Mutual authentication (both sides verified)
- Prevents unauthorized connections
- Defense against man-in-the-middle attacks
- Required for zero-trust environments

## Per-Listener Overrides

Individual listeners can override global settings:

```yaml
tls:
  ca: "./certs/ca.crt"
  cert: "./certs/agent.crt"
  key: "./certs/agent.key"
  strict: true
  mtls: true

listeners:
  # Uses global settings
  - transport: quic
    address: "0.0.0.0:4433"

  # Override: disable mTLS for this listener
  - transport: h2
    address: "0.0.0.0:8443"
    tls:
      mtls: false

  # Override: use different certificate
  - transport: ws
    address: "0.0.0.0:443"
    tls:
      cert: "./certs/public-facing.crt"
      key: "./certs/public-facing.key"
      mtls: false

  # No TLS: plaintext for reverse proxy
  - transport: ws
    address: "127.0.0.1:8080"
    plaintext: true
```

## Per-Peer Overrides

Individual peer connections can override global settings:

```yaml
tls:
  ca: "./certs/ca.crt"
  cert: "./certs/agent.crt"
  key: "./certs/agent.key"
  strict: true

peers:
  # Uses global CA and cert
  - id: "abc123..."
    transport: quic
    address: "192.168.1.50:4433"

  # Override: different CA for this peer
  - id: "def456..."
    transport: quic
    address: "external.example.com:4433"
    tls:
      ca: "./certs/external-ca.crt"

  # Override: enable strict verification for specific peer
  - id: "ghi789..."
    transport: quic
    address: "trusted.example.com:4433"
    tls:
      strict: true
```

## Certificate Validity

### Validity Period

Default validity periods:

- CA certificates: **365 days**
- Agent/client certificates: **90 days**

Specify custom validity:

```bash
muti-metroo cert ca --days 730       # 2 years
muti-metroo cert agent --days 180    # 6 months
```

### Subject Alternative Names (SANs)

Always include SANs for agent certificates:

```bash
muti-metroo cert agent --cn "agent-1" \
  --ca ./certs/ca.crt \
  --ca-key ./certs/ca.key \
  --dns "agent1.example.com,agent1.internal" \
  --ip "192.168.1.10,10.0.0.10" \
  -o ./certs
```

## Certificate Rotation

### Planned Rotation

1. Generate new certificates before expiration
2. Deploy new certificates alongside old ones
3. Update configuration to use new certificates
4. Restart agents
5. Remove old certificates

### Emergency Rotation

If CA is compromised:

1. Generate new CA
2. Generate new certificates for all agents
3. Deploy and restart all agents simultaneously
4. Revoke trust in old CA

## Monitoring Expiration

### CLI Check

```bash
muti-metroo cert info ./certs/agent.crt
```

Output includes expiration date.

### OpenSSL Check

```bash
openssl x509 -enddate -noout -in ./certs/agent.crt
```

### Automated Monitoring

Add to your monitoring:

```bash
#!/bin/bash
# Check if cert expires in next 30 days
openssl x509 -checkend 2592000 -noout -in ./certs/agent.crt
if [ $? -eq 1 ]; then
  echo "Certificate expires soon!"
fi
```

## Troubleshooting

### Certificate Not Trusted

```
Error: x509: certificate signed by unknown authority
```

- Verify CA certificate is correct
- Check CA was used to sign agent certificate
- If using default (no verification), this error indicates `strict: true` is set

### Certificate Expired

```
Error: x509: certificate has expired
```

- Generate new certificate
- Check system time is correct

### Name Mismatch

```
Error: x509: certificate is valid for agent1.example.com, not agent2.example.com
```

- Use correct hostname/IP
- Add SANs when generating certificate

### Private Key Mismatch

```
Error: tls: private key does not match public key
```

- Verify key matches certificate:
  ```bash
  openssl x509 -noout -pubkey -in agent.crt | openssl md5
  openssl ec -in agent.key -pubout 2>/dev/null | openssl md5
  # Must match
  ```

### RSA Certificate Rejected

```
Error: certificate must use ECDSA, got RSA
```

- Muti Metroo only accepts EC certificates
- Regenerate certificates using ECDSA (P-256)

## Best Practices

1. **Start simple**: Use auto-generated certs unless you need strict verification
2. **Use EC certificates**: RSA is not supported
3. **Protect CA private key**: Use hardware security module or secure vault
4. **Use short validity**: 90-365 days for agent certs
5. **Use SANs**: Include all hostnames and IPs
6. **Enable strict mode in production**: When defense-in-depth is required
7. **Automate rotation**: Before certificates expire
8. **Monitor expiration**: Alert before expiry

## Examples

### Development (Simplest)

```yaml
agent:
  id: "auto"
  data_dir: "./data"

listeners:
  - transport: quic
    address: "0.0.0.0:4433"

# TLS auto-configured with self-signed cert, no verification
```

### Development with Embedded Cert

```yaml
tls:
  cert_pem: |
    -----BEGIN CERTIFICATE-----
    ...
    -----END CERTIFICATE-----
  key_pem: |
    -----BEGIN EC PRIVATE KEY-----
    ...
    -----END EC PRIVATE KEY-----

listeners:
  - transport: quic
    address: "0.0.0.0:4433"
```

### Production with Strict TLS

```yaml
tls:
  ca: "/etc/muti-metroo/certs/ca.crt"
  cert: "/etc/muti-metroo/certs/agent.crt"
  key: "/etc/muti-metroo/certs/agent.key"
  strict: true
  mtls: true

listeners:
  - transport: quic
    address: "0.0.0.0:4433"

peers:
  - id: "abc123..."
    transport: quic
    address: "peer.example.com:4433"
```

### Container Secrets

When using container orchestration or secret management, inject certificates via environment variables:

```yaml
tls:
  ca_pem: "${CA_CRT}"
  cert_pem: "${TLS_CRT}"
  key_pem: "${TLS_KEY}"
  strict: true
  mtls: true

listeners:
  - transport: quic
    address: "0.0.0.0:4433"
```

## Related

- [CLI: cert](/cli/cert) - Certificate CLI commands
- [Security: TLS/mTLS](/security/tls-mtls) - Security considerations
- [Deployment](/deployment/scenarios) - Production deployment
