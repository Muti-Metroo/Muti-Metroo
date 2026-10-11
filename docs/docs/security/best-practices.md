---
title: Best Practices
description: Production hardening for Muti Metroo, covering the CA and signing keys, certificate rotation, host hardening, secrets, logging, and incident response.
sidebar_position: 6
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-inspecting.png" alt="Mole recommending best practices" style={{maxWidth: '180px'}} />
</div>

# Security Best Practices

This page collects operational practices for production meshes. It assumes the controls in the [Security Overview](/security/overview) are understood. The most important points:

1. **Protect the two mesh-wide secrets**: the CA private key and the signing private key. Either one gives control over the mesh.
2. **Protect operator agents' HTTP API.** An operator agent signs every privileged request that reaches it.
3. **Verify peers** with `tls.strict` and `tls.mtls` when links cross networks you do not control.
4. **Enable only what you use.** Management commands and file transfer are off by default; UDP and ICMP relay are on.

## The CA private key

Anyone with the CA key can issue certificates that every agent accepts, which with mutual TLS is the same as joining the mesh. There is no certificate revocation, so a leaked CA key can only be dealt with by replacing the CA everywhere.

- Generate the CA on a machine that does not run an agent, and keep `ca.key` off agent hosts.
- Store it encrypted: a secrets manager, a hardware token, or an encrypted volume.
- Never commit it to version control or copy it over unencrypted channels.

## The signing private key

`management.signing_private_key` authorizes management commands, file transfer, runtime configuration changes, and remote stop on every agent that holds the matching public key.

- Put the private key only on operator agents, and keep the number of operator agents small.
- Bind operator agents' HTTP API to `127.0.0.1` and set `http.token_hash`.
- Keep file transfer disabled on operator agents; an agent with the private key accepts unsigned file browse requests from other agents (see [Authentication](/security/authentication#how-a-request-is-signed)).
- Keep `management.signing_public_key` on every agent. It is safe to distribute and can live in version control.
- Keep agent clocks synchronized with NTP; signatures are only accepted within five minutes of the verifier's clock.

```bash
muti-metroo signing-key generate
```

The command prints both halves and ready-to-paste configuration snippets. Copy the private key to its destination and clear your terminal scrollback.

### Rotate the signing key

Each agent accepts one public key, so rotation briefly splits the mesh into agents that accept the old key and agents that accept the new one:

1. Generate a new key pair.
2. Replace `signing_public_key` on every agent and restart them.
3. Replace both keys on the operator agents and restart them.
4. Destroy the old private key.

Plan the rotation for a time when no management operations are needed.

## Certificates

`muti-metroo cert agent` issues certificates valid for 365 days by default. Shorter lifetimes limit how long a stolen agent key stays useful, at the cost of more frequent renewal:

```bash
# CA valid for three years
muti-metroo cert ca --cn "Example Mesh CA" --days 1095 -o ./ca

# Agent certificate valid for 90 days
muti-metroo cert agent --cn exit-1 --dns exit-1.example.com \
  --ca ./ca/ca.crt --ca-key ./ca/ca.key --days 90 -o ./certs
```

### Watch expiry

`muti-metroo cert info` shows a certificate's expiry and warns when fewer than 60 days remain; `muti-metroo run` prints the same warning at startup for configured certificates. For automated checks, OpenSSL's `-checkend` exits with a non-zero status when a certificate expires within the given number of seconds:

```bash
#!/bin/sh
# Exit 1 if the agent certificate expires within 30 days
openssl x509 -checkend 2592000 -noout -in /etc/muti-metroo/certs/exit-1.crt \
  || { echo "certificate expires within 30 days"; exit 1; }
```

### Renew a certificate

Renewal happens on the CA machine, so the CA key never reaches the agent:

1. Issue a new certificate with the same `--cn`, `--dns`, and `--ip` values.
2. Copy `exit-1.crt` and `exit-1.key` to the agent.
3. Restart the agent (`systemctl restart muti-metroo` for a service installed with `muti-metroo service install`).

## Host hardening

### Run unprivileged

Agents need no root privileges, including for ICMP. On Linux, root is only needed to listen on ports below 1024 and to bind a route to an interface with a source address that is not assigned to that interface. Create a dedicated user:

```bash
sudo useradd -r -s /usr/sbin/nologin muti-metroo
sudo chown -R muti-metroo:muti-metroo /var/lib/muti-metroo
```

On Linux, `muti-metroo service install` writes a systemd unit with `NoNewPrivileges`, `ProtectSystem=strict`, `ProtectHome=read-only`, and `PrivateTmp`. To run it as the dedicated user, add a drop-in with `sudo systemctl edit muti-metroo`:

```ini
[Service]
User=muti-metroo
Group=muti-metroo
```

See [System Service](/deployment/system-service).

### File permissions

```bash
chmod 700 /var/lib/muti-metroo              # Data directory, including agent_key
chmod 640 /etc/muti-metroo/config.yaml      # Configuration with secrets
chmod 600 /etc/muti-metroo/certs/*.key      # Private keys
```

The data directory holds the agent's key pair, the runtime configuration journal, and the sleep state.

## Secrets in configuration

Read secrets from the environment instead of writing them into configuration files:

```yaml
http:
  token_hash: "${HTTP_TOKEN_HASH}"

socks5:
  auth:
    enabled: true
    users:
      - username: "${SOCKS5_USER}"
        password_hash: "${SOCKS5_PASSWORD_HASH}"

management:
  signing_public_key: "${MESH_SIGNING_PUBLIC_KEY}"
  signing_private_key: "${MESH_SIGNING_PRIVATE_KEY}"
```

Supply them through your secrets manager, systemd credentials, or container secrets. A reference to an unset variable is left in place as literal text, so a missing hash makes every login fail and a missing key fails validation; `${VAR:-default}` supplies a fallback. See [Environment Variables](/configuration/environment-variables).

`muti-metroo config show` prints a configuration with secrets redacted, which is safe to share when asking for help.

## Logging

Use JSON logs to feed a log pipeline:

```yaml
agent:
  log_level: "info"
  log_format: "json"
```

Agents log peer connections and disconnections, certificate errors, file transfer starts and completions (with paths and sizes), and rejected sleep and wake commands. They do not log failed SOCKS logins, management commands, runtime configuration changes, or rejected signatures, so these need monitoring elsewhere, for example on the operator agents' HTTP API through a reverse proxy access log.

## Incident response

### An agent host is compromised

1. Remove the agent's peer entries on its neighbors (from their configuration files, or with `muti-metroo peer remove` for peers added at runtime) and block its address at the network level.
2. If the host held `signing_private_key`, rotate the signing key on every agent.
3. Because certificates cannot be revoked, replace the CA and reissue certificates to the remaining agents if the agent had a CA-issued certificate and mutual TLS is your admission control.
4. Rebuild the host and initialize a new agent identity (`muti-metroo init`) before reconnecting it.

### The CA key is exposed

1. Create a new CA.
2. Issue new certificates to every agent and deploy them with the new `tls.ca`.
3. Restart all agents. Agents with the old CA cannot connect to agents with the new one, so update the whole mesh in one maintenance window.

### The signing private key is exposed

Rotate it as described in [Rotate the signing key](#rotate-the-signing-key). Until every agent has the new public key, anyone with the old private key and access to the mesh can run commands on agents that still trust it.

## See Also

- [Security Overview](/security/overview): controls and hardening checklist
- [TLS and mTLS](/security/tls-mtls): certificate verification and mutual TLS
- [Authentication](/security/authentication): signing key, API token, and SOCKS credentials
- [Access Control](/security/access-control): exit routes, whitelist, and allowed paths
- [System Service](/deployment/system-service): run agents as a service
