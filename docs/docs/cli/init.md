---
title: muti-metroo init
sidebar_label: init
sidebar_position: 3
description: Create an agent data directory and generate its agent ID ahead of the first run.
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-reading.png" alt="Mole initializing" style={{maxWidth: '180px'}} />
</div>

# muti-metroo init

Create the data directory and generate the agent ID, the 128-bit identifier other agents use to address this agent. Running `init` is optional: [`run`](run.md) creates the identity on first start. Use `init` when you need to know the agent ID before the agent runs, for example to put it in another agent's configuration.

Global flags (`--token`, `--yes`, `--no-color`) are described in [CLI overview](overview.md#global-flags).

## Synopsis

```bash
muti-metroo init [flags]
```

## Flags

| Flag | Default | Description |
| --- | --- | --- |
| `-d`, `--data-dir` | `./data` | Data directory to create. Use the same path as `agent.data_dir` in the configuration. |

## Description

`init` creates the data directory (mode `0700`) if needed, writes a random agent ID to the `agent_id` file in it (mode `0600`), and prints the ID.

If `agent_id` already exists, `init` changes nothing and prints the existing ID. Either way it exits with status 0.

`init` does not create the end-to-end encryption keypair (`agent_key`, `agent_key.pub`); the agent generates it on first start.

## Examples

```bash
muti-metroo init -d /var/lib/muti-metroo
```

```text
Agent initialized in /var/lib/muti-metroo
Agent ID: abc123def456789012345678901234ab
```

Running it again on the same directory:

```text
Agent already initialized in /var/lib/muti-metroo
Agent ID: abc123def456789012345678901234ab
```

## See Also

- [run](run.md): start the agent, which creates any missing identity files
- [Agent configuration](../configuration/agent.md): `agent.data_dir`, `agent.id`, and inline identity keys
- [setup](setup.md): create a configuration and identity interactively
