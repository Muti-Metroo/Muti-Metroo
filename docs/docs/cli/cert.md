---
title: cert
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-reading.png" alt="Mole managing certificates" style={{maxWidth: '180px'}} />
</div>

# muti-metroo cert

Create the certificates that let agents trust each other. Generate a CA once, then create agent certificates signed by that CA.

**Typical workflow:**
```bash
# 1. Create your CA (do this once)
muti-metroo cert ca --cn "My Mesh CA" -o ./certs

# 2. Create a certificate for each agent
muti-metroo cert agent --cn "agent-1" --ca ./certs/ca.crt --ca-key ./certs/ca.key -o ./certs
```

## Subcommands

### cert ca

Generate Certificate Authority.

```bash
muti-metroo cert ca [--cn <name>] [-o <output-dir>] [--days <days>]
```

**Flags:**
| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--cn` | | "Muti Metroo CA" | Common name for the CA |
| `--out` | `-o` | ./certs | Output directory |
| `--days` | | 365 | Validity period in days |

**Output:**
- `ca.crt`: CA certificate
- `ca.key`: CA private key (keep secure!)

### cert agent

Generate agent/peer certificate. The certificate can be used for both server authentication (listeners) and client authentication (peer connections with mTLS).

```bash
muti-metroo cert agent --cn <name> [--dns <hostnames>] [--ip <ips>] [-o <output>] [--days <days>]
```

**Flags:**
| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--cn` | | (required) | Common name for the certificate |
| `--dns` | | | Additional DNS names (comma-separated) |
| `--ip` | | | Additional IP addresses (comma-separated) |
| `--out` | `-o` | ./certs | Output directory |
| `--days` | | 365 | Validity period in days (matches the CA default; pick a shorter value for high-rotation environments) |
| `--ca` | | ./certs/ca.crt | CA certificate path |
| `--ca-key` | | ./certs/ca.key | CA private key path |

**Output:**
- `<name>.crt`: Agent certificate (named after common name)
- `<name>.key`: Agent private key

### cert client

Generate client-only certificate. This certificate can only be used for client authentication (connecting to listeners), not for server authentication.

```bash
muti-metroo cert client --cn <name> [-o <output>] [--days <days>]
```

**Flags:**
| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--cn` | | (required) | Common name for the certificate |
| `--out` | `-o` | ./certs | Output directory |
| `--days` | | 365 | Validity period in days (matches the CA default; pick a shorter value for high-rotation environments) |
| `--ca` | | ./certs/ca.crt | CA certificate path |
| `--ca-key` | | ./certs/ca.key | CA private key path |

**Output:**
- `<name>.crt`: Client certificate (named after common name)
- `<name>.key`: Client private key

### cert info

Display detailed information about a certificate file.

```bash
muti-metroo cert info <cert-file>
```

**Example output:**
```
Certificate: ./certs/agent-1.crt

Subject:      CN=agent-1,O=Muti Metroo
Issuer:       CN=Mesh CA,O=Muti Metroo
Serial:       1a2b3c4d5e6f...
Fingerprint:  sha256:ab12cd34...
Is CA:        false
Not Before:   2025-01-01T00:00:00Z
Not After:    2025-04-01T00:00:00Z
Status:       Valid (89 days left)
DNS Names:    agent-1, localhost, agent1.example.com
IP Addresses: 127.0.0.1, ::1, 192.168.1.10
Key Usage:    KeyEncipherment, DigitalSignature
Ext Key Usage: ServerAuth, ClientAuth
```

`Status` reports three warning bands so you have time to react:

| Status line | Meaning |
|---|---|
| `EXPIRED` | Past `Not After`. The agent will refuse the cert; rotate immediately. |
| `EXPIRING SOON (X days left)` | Less than 30 days remaining. Rotate now. |
| `expiring within 60 days (X days left); plan rotation` | Heads-up window for unattended deployments. |
| `Valid (X days left)` | More than 60 days remaining. |

`muti-metroo run` also prints a startup warning (to stderr) when any configured TLS cert file is in the EXPIRED or `<60 days` bands, so the agent itself nags you on every restart instead of relying on someone running `cert info`.

## Rotation cadence

Pick a `--days` value that matches the rotation capacity you actually have:

- **Long-running, unattended deployments** (hospital edge, spacecraft, factory floor) — keep the 365-day default; combine with a calendar reminder at 60 days out.
- **Frequent-rotation environments** (PKI tooling automated, large fleets) — pass `--days 90` or shorter when generating cert and client certs.
- **Air-gapped / offline replays** — match the certificate window to the maintenance window cadence, never longer.

The CA's `--days` default is also 365. If you bump CA validity, agent certs are still capped to whatever you pass on `muti-metroo cert agent --days`.

## Examples

```bash
# Generate CA
muti-metroo cert ca --cn "Mesh CA" -o ./certs

# Generate agent cert (signed by CA)
muti-metroo cert agent --cn "agent-1" \
  --ca ./certs/ca.crt \
  --ca-key ./certs/ca.key \
  --dns agent1.example.com \
  --ip 192.168.1.10 \
  -o ./certs

# Generate client cert (signed by CA)
muti-metroo cert client --cn "admin" \
  --ca ./certs/ca.crt \
  --ca-key ./certs/ca.key \
  -o ./certs

# View cert info
muti-metroo cert info ./certs/agent-1.crt
```

:::tip Default Paths
The `--ca` and `--ca-key` flags default to `./certs/ca.crt` and `./certs/ca.key`. If your CA files are there, you can omit these flags.
:::

## Certificate Types

| Type | Command | Server Auth | Client Auth | Use Case |
|------|---------|-------------|-------------|----------|
| CA | `cert ca` | N/A | N/A | Sign other certificates |
| Agent | `cert agent` | Yes | Yes | Listeners and peer connections |
| Client | `cert client` | No | Yes | Client-only connections |
