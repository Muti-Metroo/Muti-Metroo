---
title: Environment Variables
description: How environment variable references in the configuration file are expanded, including default values, unset variables, and passing multi-line PEM content.
sidebar_position: 22
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-inspecting.png" alt="Mole reading environment" style={{maxWidth: '180px'}} />
</div>

# Environment Variables

Any value in the configuration file can come from an environment variable. This keeps secrets and per-host values out of the file.

```yaml
agent:
  log_level: "${LOG_LEVEL:-info}"

http:
  token_hash: "${HTTP_TOKEN_HASH}"
```

## Syntax

| Form             | Result                                                                    |
| ---------------- | ------------------------------------------------------------------------- |
| `${VAR}`         | Value of `VAR`. If `VAR` is not set, the text `${VAR}` stays in place.    |
| `${VAR:-default}`| Value of `VAR`, or `default` if `VAR` is not set. A set but empty `VAR` gives an empty value. |
| `$VAR`           | Same as `${VAR}`. The name must start with a letter or underscore.        |

Expansion is plain text substitution over the whole file before it is parsed as YAML. It also applies inside comments and quoted strings, and there is no escape syntax for a literal `$` followed by a name. A substituted value is parsed as YAML, so quote references that may expand to something YAML would interpret differently:

```yaml
peers:
  - id: "${PEER_ID:-auto}"
    transport: "${PEER_TRANSPORT:-quic}"
    address: "${PEER_ADDR}"
```

Values that are not strings work without quotes:

```yaml
http:
  enabled: ${HTTP_ENABLED:-true}
shell:
  timeout: ${SHELL_TIMEOUT:-5m}
```

## Unset variables

A reference to an unset variable without a default is not an error. The literal `${VAR}` text is used as the value, and the problem shows up later as a validation error or odd behavior, for example a SOCKS password hash that never matches. Use `muti-metroo config show` to see the expanded result:

```bash
muti-metroo config show ./config.yaml
```

Give non-secret values a default. Leave secrets without a default, so a missing secret is visible instead of silently falling back to a known value.

## Multiline values

YAML folds line breaks inside a double-quoted string into spaces, so a PEM certificate exported with real line breaks does not survive `cert_pem: "${TLS_CERT}"`. Export the content with `\n` escape sequences instead; YAML turns them back into line breaks:

```bash
export TLS_CERT="$(awk '{printf "%s\\n", $0}' ./certs/agent.crt)"
export TLS_KEY="$(awk '{printf "%s\\n", $0}' ./certs/agent.key)"
```

```yaml
tls:
  cert_pem: "${TLS_CERT}"
  key_pem: "${TLS_KEY}"
```

In an env file read by Docker or systemd, write the value on one line with `\n` between the PEM lines.

## Supplying variables

The agent reads its own process environment. Set variables in the shell, a service manager, or a container runtime:

```bash
LOG_LEVEL=debug muti-metroo run -c ./config.yaml
```

```ini
# systemd unit
[Service]
EnvironmentFile=/etc/muti-metroo/environment
```

For containers, see [Docker Deployment](/deployment/docker).

The CLI uses its own variables, such as `MUTI_METROO_TOKEN` for the HTTP API token; those are documented on [CLI overview](/cli/overview).

## See Also

- [Configuration Overview](/configuration/overview) for the file layout and validation
- [TLS](/configuration/tls-certificates) for inline PEM keys
- [Docker Deployment](/deployment/docker) for passing variables to containers
- [System Service](/deployment/system-service) for service manager setup
