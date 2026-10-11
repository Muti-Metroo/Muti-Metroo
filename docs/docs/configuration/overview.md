---
title: Configuration Overview
sidebar_label: Overview
description: How the Muti Metroo YAML configuration file is structured, loaded, validated, and where each top-level section is documented.
sidebar_position: 1
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-inspecting.png" alt="Mole inspecting configuration" style={{maxWidth: '180px'}} />
</div>

# Configuration Overview

An agent reads all of its settings from one YAML file. Every section is optional: a key that is not set keeps its built-in default, and validation runs once over the merged result before the agent starts.

## Configuration file

`muti-metroo run` reads `./config.yaml` unless `-c` names another file:

```bash
muti-metroo run -c /etc/muti-metroo/config.yaml
```

A binary with an embedded configuration ignores `-c` and uses the embedded copy. See [Embedded Configuration](/deployment/embedded-config).

The configuration is read once at startup. To apply an edit, restart the agent. Several settings can also be changed on a running agent through the CLI or HTTP API (peers, listeners, routes, forwards, mesh hosts, display name); those changes are stored separately, see [Dynamic Config Persistence](/configuration/dynamic-config-persistence).

## Minimal configuration

```yaml
agent:
  data_dir: "./data"

listeners:
  - transport: quic
    address: "0.0.0.0:4433"

socks5:
  enabled: true
  address: "127.0.0.1:1080"

exit:
  enabled: true
  routes:
    - "0.0.0.0/0"

http:
  enabled: true
  address: "127.0.0.1:8080"
```

TLS certificates are generated at startup when none are configured. See [TLS](/configuration/tls-certificates).

The repository ships a commented example with every section: [configs/example.yaml](https://github.com/postalsys/Muti-Metroo/blob/master/configs/example.yaml).

## Top-level sections

| Key                   | Purpose                                                    | Reference                                                 |
| --------------------- | ---------------------------------------------------------- | --------------------------------------------------------- |
| `default_action`      | What an embedded-config binary does with no arguments      | [below](#default_action)                                  |
| `agent`               | Identity, data directory, logging, startup delay           | [Agent](/configuration/agent)                             |
| `listeners`           | Accept transport connections from other agents             | [Listeners](/configuration/listeners)                     |
| `peers`               | Dial other agents                                          | [Peers](/configuration/peers)                             |
| `tls`                 | Certificates, CA, mTLS, strict verification, ClientHello   | [TLS](/configuration/tls-certificates)                    |
| `protocol`            | ALPN, HTTP header, and WebSocket subprotocol identifiers   | [Protocol](/configuration/protocol)                       |
| `socks5`              | SOCKS4/4a/5 ingress proxy                                  | [SOCKS Proxy](/configuration/socks5)                      |
| `exit`                | CIDR and domain exit routes, exit DNS                      | [Exit](/configuration/exit)                               |
| `mesh_hosts_suffixes` | DNS suffixes allowed for mesh host names                   | [Mesh Hosts](/configuration/hosts)                        |
| `mesh_hosts`          | Mesh-local hostname registry                               | [Mesh Hosts](/configuration/hosts)                        |
| `forward`             | Port forward endpoints and listeners                       | [Forward](/configuration/forward)                         |
| `udp`                 | UDP relay for SOCKS5 UDP ASSOCIATE                         | [UDP](/configuration/udp)                                 |
| `icmp`                | ICMP echo relay                                            | [ICMP](/configuration/icmp)                               |
| `routing`             | Route advertisement and expiry                             | [Routing](/configuration/routing)                         |
| `connections`         | Keepalive, dial timeout, reconnection backoff              | [Connections](/configuration/connections)                 |
| `limits`              | Stream limits and stream open timeout                      | [Limits](/configuration/limits)                           |
| `http`                | HTTP API server, endpoint groups, bearer token             | [HTTP API](/configuration/http)                           |
| `management`          | Topology encryption keys and command signing keys          | [Management](/configuration/management)                   |
| `shell`               | Remote management commands                                 | [Shell](/configuration/shell)                             |
| `file_transfer`       | Remote file upload and download                            | [File Transfer](/configuration/file-transfer)             |
| `sleep`               | Mesh hibernation                                           | [Sleep](/configuration/sleep)                             |

## `default_action`

```yaml
default_action: help # run (implicit) or help
```

| Key              | Type   | Default         | Description                                                                                                                                                  |
| ---------------- | ------ | --------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `default_action` | string | unset (= `run`) | Applies only to a binary with an embedded configuration that is started with no arguments. Unset or `run` starts the agent; `help` prints usage instead. Any other value fails validation. |

It has no effect when the configuration is loaded from a file.

## Environment variables

Every value in the file can reference environment variables with `${VAR}`, `${VAR:-default}`, or `$VAR`:

```yaml
agent:
  data_dir: "${DATA_DIR:-./data}"
  log_level: "${LOG_LEVEL:-info}"
```

An unset variable without a default is left in the file as the literal text `${VAR}`. Syntax rules and pitfalls are on [Environment Variables](/configuration/environment-variables).

## Validation

The agent validates the configuration before it starts and refuses to start if anything is invalid. All errors are reported together. To check a file without starting an agent:

```bash
muti-metroo config validate ./config.yaml
```

```text
Error: operation rejected: config validation failed: validation errors:
  - invalid log_level: verbose (must be debug, info, warn, or error)
  - listeners[0]: path is required for h2 transport
  - peers[0]: id is required
```

To see the effective configuration with defaults filled in and secrets redacted, or only the keys that differ from the defaults:

```bash
muti-metroo config show ./config.yaml
muti-metroo config show ./config.yaml --diff-from-default
```

Validation checks syntax, required keys, value ranges, and key formats. It does not open certificate files or check that addresses are free; those problems surface when `muti-metroo run` starts the corresponding component.

## See Also

- [Agent](/configuration/agent) for identity and the data directory, the first section most configurations set
- [config CLI](/cli/config) for `config validate` and `config show` flags and exit status
- [Environment Variables](/configuration/environment-variables) for substitution syntax
- [Embedded Configuration](/deployment/embedded-config) for single-file binaries that use `default_action`
- [Interactive Setup](/getting-started/interactive-setup) to generate a configuration with the wizard
