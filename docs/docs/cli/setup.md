---
title: muti-metroo setup
sidebar_label: setup
sidebar_position: 4
description: Create or edit an agent configuration with an interactive wizard, optionally embedding it in a binary and installing a service.
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-idea.png" alt="Mole setup wizard" style={{maxWidth: '180px'}} />
</div>

# muti-metroo setup

Build an agent configuration through interactive prompts. The wizard writes a config file or embeds the configuration in a binary, creates the agent identity, and on Linux, macOS, and Windows offers to install the agent as a system service. It needs an interactive terminal. For scripted builds, write the YAML yourself and use [`embed-config`](embed-config.md).

Global flags (`--token`, `--yes`, `--no-color`) are described in [CLI overview](overview.md#global-flags).

## Synopsis

```bash
muti-metroo setup [flags]
```

## Description

The wizard asks for the following, in order. Press Enter to accept the default shown in brackets.

1. **Basic setup**: config file path, data directory, and display name. If a valid config file exists at the given path, its values become the defaults for every later question.
2. **Agent role**: ingress, transit, and exit; several can be selected.
3. **Inbound listener** (optional): transport, listen address, and, for HTTP/2 and WebSocket, path and plaintext mode for use behind a reverse proxy.
4. **TLS**: self-signed certificates generated at startup, or strict verification against a CA.
5. **Peer connections**, each with a connectivity test.
6. **SOCKS5 proxy**, for the ingress role.
7. **Exit routes**, for the exit role.
8. **Monitoring and logging**: log level, HTTP API, and an optional API bearer token.
9. **Management commands** (shell), skipped for transit-only agents.
10. **File transfer**, skipped for transit-only agents.
11. **Management and signing keys**.

The wizard then asks how to deliver the configuration:

- **Save to config file**: writes the YAML file and creates `agent_id`, `agent_key`, and `agent_key.pub` in the data directory.
- **Embed in binary**: writes a copy of the running binary with the configuration and identity embedded, so no config file is needed, and also saves the YAML file as a backup. The embedded configuration leaves `agent.data_dir` at its default, so the agent still keeps runtime state in `./data` relative to its working directory. The new binary starts the agent when run with no arguments.

With `-c <binary>`, the wizard skips the config path and data directory questions and always embeds into that binary. An identity already embedded in the binary is kept.

Finally, on Linux, macOS, and Windows, the wizard offers to install or update the agent as a service. See [service](service.md) for what installation does on each platform.

## Flags

| Flag | Default | Description |
| --- | --- | --- |
| `-c`, `--config` | none | Path to a binary that has, or should receive, an embedded configuration. The wizard loads the binary's current embedded configuration as defaults and embeds the result back into that binary. DLL files are rejected. |

## Examples

Create a new configuration:

```bash
muti-metroo setup
```

Edit the configuration embedded in an existing binary:

```bash
muti-metroo setup -c /usr/local/bin/my-agent
```

## See Also

- [Interactive setup](../getting-started/interactive-setup.md): walkthrough of each wizard step
- [embed-config](embed-config.md): embed a prepared YAML file without prompts
- [Embedded configuration](../deployment/embedded-config.md): how single-binary deployments work
- [service](service.md): install, inspect, and remove the system service
- [Configuration overview](../configuration/overview.md): every key the wizard can set
