---
title: muti-metroo hash
sidebar_label: hash
sidebar_position: 8
description: Generate a bcrypt hash of a password or token for the HTTP API and SOCKS5 authentication settings.
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-inspecting.png" alt="Mole generating hash" style={{maxWidth: '180px'}} />
</div>

# muti-metroo hash

Generates a bcrypt hash of a password or bearer token so the configuration file stores the hash instead of the secret. No running agent is needed.

Global flags (`--token`, `--yes`, `--no-color`) are described in [CLI overview](overview.md#global-flags).

## Synopsis

```bash
muti-metroo hash [password] [flags]
```

## Description

The hash is accepted by these configuration keys:

| Config key | Purpose |
| --- | --- |
| `http.token_hash` | Bearer token for the HTTP API. |
| `socks5.auth.users[].password_hash` | Password of a SOCKS5 proxy user. |

Shell, file transfer, sleep and wake, and dynamic configuration changes are not authorized with passwords. They require the Ed25519 signing key; see [muti-metroo signing-key](signing-key.md).

Without a `password` argument, the command prompts for the password twice with input hidden, and fails if the two entries differ. The prompt reads from the terminal, so it does not accept a password piped on stdin. With a `password` argument, the command prints a warning to stderr that the value is recorded in shell history.

The hash is printed to stdout on its own line. An empty password is rejected.

## Flags

| Flag | Default | Description |
| --- | --- | --- |
| `--cost` | `12` | bcrypt cost factor, from 4 to 31. |

Each increase of the cost by one doubles the time needed to compute and to verify the hash. The agent verifies the hash on every authenticated request, so very high values slow down the HTTP API and SOCKS5 logins.

## Examples

Interactive prompt:

```bash
muti-metroo hash
```

```text
Enter password:
Confirm password:
$2a$12$JXTEjlb300LqN.vto6gLS.eKGsr7CmDKJoGthCYyn8q5LBjuWsxUO
```

Password as an argument (the warning goes to stderr):

```bash
muti-metroo hash "correct horse"
```

```text
WARNING: password supplied on command line; this is recorded in shell history. Re-run without an argument for a hidden prompt.
$2a$12$JXTEjlb300LqN.vto6gLS.eKGsr7CmDKJoGthCYyn8q5LBjuWsxUO
```

Higher cost:

```bash
muti-metroo hash --cost 14
```

Paste the hash into the configuration:

```yaml
http:
  token_hash: "$2a$12$JXTEjlb300LqN.vto6gLS.eKGsr7CmDKJoGthCYyn8q5LBjuWsxUO"

socks5:
  auth:
    enabled: true
    users:
      - username: "admin"
        password_hash: "$2a$12$JXTEjlb300LqN.vto6gLS.eKGsr7CmDKJoGthCYyn8q5LBjuWsxUO"
```

In scripts, pass the secret as an argument from a file or variable and capture stdout. The interactive prompt writes to stdout, so capture only works with the argument form:

```bash
export SOCKS5_PASSWORD_HASH="$(muti-metroo hash "$(cat /run/secrets/socks-password)")"
```

The configuration can then reference `${SOCKS5_PASSWORD_HASH}`; see [Environment variables](../configuration/environment-variables.md).

## Other bcrypt tools

Any standard bcrypt hash works, so other tools can produce it, for example:

```bash
htpasswd -bnBC 12 "" 'correct horse' | tr -d ':\n'
```

## See Also

- [HTTP API configuration](../configuration/http.md): enable bearer token authentication with `token_hash`
- [SOCKS5 configuration](../configuration/socks5.md): configure proxy users with `password_hash`
- [Authentication](../security/authentication.md): how the different credentials fit together
- [muti-metroo signing-key](signing-key.md): keys that authorize management operations
