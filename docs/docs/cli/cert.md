---
title: muti-metroo cert
sidebar_label: cert
sidebar_position: 7
description: Generate a certificate authority, agent and client certificates, and inspect certificate files.
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-reading.png" alt="Mole managing certificates" style={{maxWidth: '180px'}} />
</div>

# muti-metroo cert

Generates the TLS certificates that agents use to authenticate each other, and prints details of existing certificate files. Create a certificate authority (CA) once, then issue an agent certificate for each agent from that CA. These commands work on local files only; no running agent is needed.

Global flags (`--token`, `--yes`, `--no-color`) are described in [CLI overview](overview.md#global-flags).

All generated keys are ECDSA P-256. Certificates are written with mode `0644` and private keys with mode `0600`. The output directory is created if it does not exist. Existing files with the same name are overwritten without a prompt, so take care not to replace a CA you still need.

## Certificate types

| Type | Command | Server authentication | Client authentication | Typical use |
| --- | --- | --- | --- | --- |
| CA | `cert ca` | No | No | Signs agent and client certificates. |
| Agent | `cert agent` | Yes | Yes | Listeners and peer connections. |
| Client | `cert client` | No | Yes | Connections that only dial out with mutual TLS. |

## cert ca

```bash
muti-metroo cert ca [flags]
```

Generates a self-signed CA certificate and private key, written as `ca.crt` and `ca.key` in the output directory.

### Flags

| Flag | Default | Description |
| --- | --- | --- |
| `--cn` | `Muti Metroo CA` | Common name of the CA. |
| `-o`, `--out` | `./certs` | Output directory. |
| `--days` | `365` | Validity period in days. |

### Examples

```bash
muti-metroo cert ca --cn "Mesh CA"
```

```text
Generating CA certificate...
  Common Name: Mesh CA
  Valid for: 365 days

CA certificate generated:
  Certificate: ./certs/ca.crt
  Private key: ./certs/ca.key
  Fingerprint: sha256:fc5556612a9150254151333ddd886d95b271d82590f5e3ac3ac65c7d8c506d7c
  Expires: 2027-10-11T11:53:25Z
```

## cert agent

```bash
muti-metroo cert agent --cn <name> [flags]
```

Generates an agent certificate signed by the CA. The certificate carries both the server authentication and client authentication extended key usages, so the same certificate serves an agent's listeners and its outbound peer connections, including mutual TLS.

The certificate always includes the common name and `localhost` as DNS names, and `127.0.0.1` and `::1` as IP addresses. Use `--dns` and `--ip` to add the names and addresses that peers use to reach the agent. The files are named after the common name: `<cn>.crt` and `<cn>.key`.

### Flags

| Flag | Default | Description |
| --- | --- | --- |
| `--cn` | none (required) | Common name. Also used as the file name. |
| `--dns` | none | Additional DNS names, comma-separated. |
| `--ip` | none | Additional IP addresses, comma-separated. An invalid address fails the command. |
| `-o`, `--out` | `./certs` | Output directory. |
| `--days` | `365` | Validity period in days. |
| `--ca` | `./certs/ca.crt` | Path to the CA certificate. |
| `--ca-key` | `./certs/ca.key` | Path to the CA private key. |

### Examples

```bash
muti-metroo cert agent --cn agent-1 --dns agent1.example.com --ip 192.0.2.10
```

```text
Generating agent certificate...
  Common Name: agent-1
  Valid for: 365 days
  CA: Mesh CA

Agent certificate generated:
  Certificate: ./certs/agent-1.crt
  Private key: ./certs/agent-1.key
  Fingerprint: sha256:540304b162a9fd55ee33e2e747dd76f85666daeaf7439f03fe78b715ce558a01
  Expires: 2027-10-11T11:53:25Z
  DNS Names: agent-1, localhost, agent1.example.com
  IP Addresses: 127.0.0.1, ::1, 192.0.2.10
```

Using a CA stored somewhere other than `./certs`:

```bash
muti-metroo cert agent --cn agent-2 \
  --ca /etc/muti-metroo/ca.crt \
  --ca-key /etc/muti-metroo/ca.key \
  -o /etc/muti-metroo
```

## cert client

```bash
muti-metroo cert client --cn <name> [flags]
```

Generates a certificate signed by the CA that carries only the client authentication extended key usage. Use it for a connection that authenticates to a listener with mutual TLS but never accepts connections itself. It has no DNS or IP subject alternative names. The files are named `<cn>.crt` and `<cn>.key`.

### Flags

| Flag | Default | Description |
| --- | --- | --- |
| `--cn` | none (required) | Common name. Also used as the file name. |
| `-o`, `--out` | `./certs` | Output directory. |
| `--days` | `365` | Validity period in days. |
| `--ca` | `./certs/ca.crt` | Path to the CA certificate. |
| `--ca-key` | `./certs/ca.key` | Path to the CA private key. |

### Examples

```bash
muti-metroo cert client --cn admin
```

```text
Generating client certificate...
  Common Name: admin
  Valid for: 365 days
  CA: Mesh CA

Client certificate generated:
  Certificate: ./certs/admin.crt
  Private key: ./certs/admin.key
  Fingerprint: sha256:e843152360ecc68d059ed41bd0c9df25ddb5928b134c6252effce82e534c6b57
  Expires: 2027-10-11T11:53:25Z
```

## cert info

```bash
muti-metroo cert info <certificate>
```

Prints the subject, issuer, serial number, SHA-256 fingerprint, validity period, expiry status, subject alternative names, and key usages of a PEM certificate file. It takes no command-specific flags.

### Examples

```bash
muti-metroo cert info ./certs/agent-1.crt
```

```text
Certificate: ./certs/agent-1.crt

Subject:      CN=agent-1,O=Muti Metroo
Issuer:       CN=Mesh CA,O=Muti Metroo
Serial:       4840e0f8cabe061a09aa1f6d46d91a
Fingerprint:  sha256:540304b162a9fd55ee33e2e747dd76f85666daeaf7439f03fe78b715ce558a01
Is CA:        false
Not Before:   2026-10-11T11:53:25Z
Not After:    2027-10-11T11:53:25Z
Status:       Valid (364 days left)
DNS Names:    agent-1, localhost, agent1.example.com
IP Addresses: 127.0.0.1, ::1, 192.0.2.10
Key Usage:    DigitalSignature, KeyEncipherment
Ext Key Usage: ServerAuth, ClientAuth
```

The day count is rounded down, so a certificate issued for 365 days shows 364 days left right after it is created.

### Expiry status

The `Status` line uses fixed bands:

| Status line | Condition |
| --- | --- |
| `Valid (N days left)` | 60 or more days remaining. |
| `expiring within 60 days (N days left); plan rotation` | Fewer than 60 days remaining. |
| `EXPIRING SOON (N days left)` | Fewer than 30 days remaining. |
| `EXPIRED` | The `Not After` time has passed. |

[`muti-metroo run`](run.md) checks the certificate files configured in `tls.cert` and in each listener's `tls.cert` at startup with the same bands, and prints a notice (under 60 days) or a warning (under 30 days, or expired) to stderr. Certificates supplied inline with `cert_pem` are not checked.

## Choosing a validity period

The default of 365 days suits deployments that are rotated by hand once a year. Where certificate rotation is automated, a shorter `--days` value such as 90 limits how long a leaked key stays useful.

The validity of an agent or client certificate is not limited by the CA's validity. A certificate whose `Not After` is later than the CA's stops validating when the CA expires, so plan CA rotation before issuing long-lived certificates from it.

## See Also

- [TLS certificates configuration](../configuration/tls-certificates.md): reference the generated files from the agent config
- [TLS and mTLS](../security/tls-mtls.md): how agents verify each other and when to require client certificates
- [muti-metroo setup](setup.md): generate certificates as part of the interactive setup wizard
- [muti-metroo probe](probe.md): test that a listener accepts the certificates you issued
