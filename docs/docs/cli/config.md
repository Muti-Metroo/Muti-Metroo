---
title: muti-metroo config
sidebar_label: config
sidebar_position: 6
description: Validate a configuration file or embedded configuration, and print the effective configuration with secrets redacted, without starting an agent.
---

# muti-metroo config

Inspect a configuration without starting an agent. Both subcommands accept either a YAML file or a Muti Metroo binary with an embedded configuration. A path ending in `.yaml` or `.yml` is read as YAML; any other path is first checked for an embedded configuration and then read as YAML.

Global flags (`--token`, `--yes`, `--no-color`) are described in [CLI overview](overview.md#global-flags).

## config validate

```bash
muti-metroo config validate <path> [flags]
```

Parse the configuration and run the same validation the agent runs at startup: schema, key formats, listener and peer settings, CIDR, domain, and hostname values, and management key consistency. Environment variable references are expanded from the current environment first, so validate in the same environment the agent will run in.

### Flags

| Flag | Default | Description |
| --- | --- | --- |
| `--json` | `false` | Print the result as a JSON object instead of text. |

### Output

On success, the text output names the source:

```text
OK: config.yaml (file)
```

```text
OK: /usr/local/bin/my-agent (embedded in binary)
```

On failure, the error lists every problem found:

```text
Error: operation rejected: config validation failed: validation errors:
  - listeners[0]: invalid transport: carrier-pigeon (must be quic, h2, or ws)
```

With `--json`, the object has `path` and `ok`, plus `embedded` on success or `error` on failure:

```json
{
  "embedded": false,
  "ok": true,
  "path": "config.yaml"
}
```

```json
{
  "error": "config validation failed: validation errors:\n  - listeners[0]: invalid transport: carrier-pigeon (must be quic, h2, or ws)",
  "ok": false,
  "path": "config.yaml"
}
```

### Exit status

0 if the configuration is valid, 5 if it fails validation or cannot be read. With `--json`, test the `ok` field rather than the exit status.

### Examples

```bash
muti-metroo config validate ./config.yaml
muti-metroo config validate /usr/local/bin/my-agent
```

Fail a CI job on an invalid configuration:

```bash
muti-metroo config validate --json ./config.yaml | jq -e '.ok'
```

## config show

```bash
muti-metroo config show [<path>] [flags]
```

Print the effective configuration: the file with built-in defaults filled in and environment variable references expanded. `<path>` defaults to `./config.yaml`.

Secrets are replaced with `<redacted>` when set and left empty when not, so the output shows whether a secret is configured without revealing it. Redacted keys:

- `agent.private_key`
- `tls.key`, `tls.key_pem`, and the same keys in per-listener and per-peer `tls` blocks
- `peers[].proxy_auth.password`
- `socks5.auth.users[].password`, `socks5.auth.users[].password_hash`
- `http.token_hash`
- `management.private_key`, `management.signing_private_key`

### Flags

| Flag | Default | Description |
| --- | --- | --- |
| `--diff-from-default` | `false` | Print only the keys whose values differ from the built-in defaults. |
| `--json` | `false` | Print JSON instead of YAML. |

### Examples

Show only what a configuration changes from the defaults:

```bash
muti-metroo config show ./config.yaml --diff-from-default
```

```text
agent:
  display_name: gateway-1
http:
  address: 127.0.0.1:8080
  enabled: true
  token_hash: <redacted>
listeners:
  - address: 0.0.0.0:4433
    transport: quic
socks5:
  address: 127.0.0.1:1080
  enabled: true
```

Compare the effective settings of two deployments:

```bash
diff <(muti-metroo config show site-a.yaml --diff-from-default) \
     <(muti-metroo config show site-b.yaml --diff-from-default)
```

Inspect the configuration embedded in a binary:

```bash
muti-metroo config show /usr/local/bin/my-agent
```

### Exit status

0 on success, 5 if the configuration cannot be read or fails validation.

## See Also

- [Configuration overview](../configuration/overview.md): every config section and its defaults
- [Environment variables](../configuration/environment-variables.md): `${VAR}` and `${VAR:-default}` substitution
- [embed-config](embed-config.md): embed a validated configuration into a binary
- [run](run.md): start an agent with the configuration
