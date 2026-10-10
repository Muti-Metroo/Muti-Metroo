---
title: Authentication
sidebar_position: 4
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-inspecting.png" alt="Mole checking authentication" style={{maxWidth: '180px'}} />
</div>

# Authentication

Control who can use your mesh. Privileged operations (management commands, file transfer, dynamic config changes, sleep/wake) are authorized by an Ed25519 mesh **management signing key**. SOCKS5 and HTTP API access still use passwords/bearer tokens.

## Where Authentication Applies

| Component                                                                              | Authentication Method            | What It Protects                           |
| -------------------------------------------------------------------------------------- | -------------------------------- | ------------------------------------------ |
| SOCKS5 proxy                                                                           | Username + password              | Who can tunnel traffic                     |
| Management commands                                                                    | Management signing key (Ed25519) | Who can run commands                       |
| File transfer                                                                          | Management signing key (Ed25519) | Who can upload/download/browse files       |
| Dynamic config mutations (route/peer/forward/forward-endpoint/display-name add/remove) | Management signing key (Ed25519) | Who can reshape the mesh at runtime        |
| Sleep/wake                                                                             | Management signing key (Ed25519) | Who can hibernate the mesh                 |
| Peer connections                                                                       | TLS certificates (mTLS)          | Which agents can join the mesh             |
| HTTP API                                                                               | Bearer token (built-in)          | Perimeter for monitoring/management access |

## Management Signing Key

The signing keypair lives in `management.signing_public_key` and `management.signing_private_key`. Operator roles follow directly from which half is present:

| Node holds                         | Can do                                                          |
| ---------------------------------- | --------------------------------------------------------------- |
| Private + public (or just private) | Sign and issue shell/file/mutation requests.                    |
| Public only                        | Verify and execute signed requests. Cannot initiate.            |
| Neither                            | Shell, file transfer, and dynamic mutations are fully disabled. |

There is no per-feature password. Generate the keypair with `muti-metroo signing-key generate` and distribute the public half to every agent in the mesh; the private half only to operator nodes.

### Querying an Agent's Capability

The role an agent plays is exposed on `/healthz` (and on the remote status endpoint `GET /agents/{id}`) so web dashboards and CLI tooling can enable or disable management actions without probing each feature:

```json
{
  "can_sign_management": true,
  "accepts_signed_management": true
}
```

- `can_sign_management` is `true` when the agent holds `management.signing_private_key` and can initiate shell, file-transfer, mutation, and sleep/wake requests.
- `accepts_signed_management` is `true` when the agent holds `management.signing_public_key` and will verify and execute signed requests from other agents.

A gateway agent with `can_sign_management: false` cannot issue any privileged operation; a target agent with `accepts_signed_management: false` rejects all of them. Use both flags to decide which management actions the UI should offer and which mesh agents can be their targets.

## Password Hashing

All passwords are stored as bcrypt hashes - never as plaintext. Generate hashes using the built-in CLI:

```bash
# Interactive (recommended - password hidden)
muti-metroo hash

# With custom cost factor for production
muti-metroo hash --cost 12
```

See [hash command](/cli/hash) for full documentation.

### Cost Factor

The cost factor determines hash computation time:

| Cost | Time (approx) | Recommendation |
| ---- | ------------- | -------------- |
| 10   | ~100ms        | Development    |
| 12   | ~400ms        | Production     |
| 14   | ~1.5s         | High security  |

Higher cost = slower brute force attacks, but also slower authentication.

## SOCKS5 Authentication

Protect your proxy with username/password authentication.

:::tip Configuration
See [SOCKS5 Configuration](/configuration/socks5) for auth setup including multiple users.
:::

### Client Usage

```bash
# curl
curl -x socks5://user1:password@localhost:1080 https://example.com

# ssh via netcat
ssh -o ProxyCommand='nc -x localhost:1080 -P user1 %h %p' user@host

# Firefox: Enter credentials in Network Settings
```

## Shell, File Transfer, Mutations, Sleep/Wake

These four operations are authorized by the management signing key described above; the same flow applies to all of them.

- The **gateway agent** reached via `-a` (or the local agent) must hold `management.signing_private_key`. It signs the request envelope.
- The **target agent** must hold `management.signing_public_key` and have the relevant feature enabled (`shell.enabled`, `file_transfer.enabled`, etc.). It verifies the signature before executing.
- A request that is missing a signature returns `401 auth_required`; a request whose signature does not verify returns `403 invalid_credentials`.

### Client Usage

```bash
# Run a command on a remote agent
muti-metroo shell agent123 whoami

# Open an interactive shell
muti-metroo shell --tty agent123 bash

# Upload / download a file
muti-metroo upload   agent123 ./local.txt /tmp/remote.txt
muti-metroo download agent123 /tmp/remote.txt ./local.txt

# Mutate runtime config on a remote agent
muti-metroo route add 10.0.0.0/8 --target agent123
```

There is no `--password` / `-p` flag on these commands; the CLI signs the request automatically using the local agent's `management.signing_private_key`.

## HTTP API Security

The HTTP API supports built-in bearer token authentication:

```yaml
http:
  address: ":8080"
  token_hash: "$2a$10$..." # bcrypt hash of your bearer token
```

When `token_hash` is set, all non-health endpoints require authentication via:

- `Authorization: Bearer <token>` header, or
- `?token=<token>` query parameter

Health endpoints (`/health`, `/healthz`, `/ready`) are always exempt.

Generate the hash with `muti-metroo hash`.

### Additional Security Measures

For defense in depth, you can also:

**Bind to localhost** (only local access):

```yaml
http:
  address: "127.0.0.1:8080"
```

**Firewall rules** (restrict to management network):

```bash
iptables -A INPUT -p tcp --dport 8080 -s 10.0.0.0/24 -j ACCEPT
iptables -A INPUT -p tcp --dport 8080 -j DROP
```

## Environment Variables

Never hardcode bcrypt hashes or signing keys in config files. Use environment variables:

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

See [Environment Variables](/configuration/environment-variables) for details.

## Defense in Depth

Layer multiple security mechanisms:

1. **Bind to localhost** - Restrict network access
2. **Require authentication** - Verify identity
3. **Use TLS/mTLS** - Encrypt and authenticate transport
4. **Firewall rules** - Network-level restrictions

Example: SOCKS5 proxy bound to localhost AND requiring authentication provides two layers of protection.

## Troubleshooting

### Invalid Password Hash

```
Error: invalid bcrypt hash
```

**Causes:**

- Hash doesn't start with `$2a$` or `$2b$`
- Hash was corrupted or truncated
- Extra whitespace in the hash

**Solution:** Regenerate using `muti-metroo hash`

### Authentication Failed

```
Error: authentication failed
```

**Causes:**

- Wrong password
- Wrong username (case-sensitive)
- Hash generated from different password

**Solution:** Verify credentials and regenerate hash if needed

### User Not Found (SOCKS5)

```
Error: user not found
```

**Causes:**

- Username not in config
- Username typo (case-sensitive)

**Solution:** Check `socks5.auth.users` in configuration

## Next Steps

- [Access Control](/security/access-control) - Route and command restrictions
- [TLS/mTLS](/security/tls-mtls) - Certificate-based authentication
- [Best Practices](/security/best-practices) - Production hardening
