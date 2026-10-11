---
title: muti-metroo embed-config
sidebar_label: embed-config
sidebar_position: 5
description: Embed a YAML configuration into a Muti Metroo binary without prompts, for CI pipelines and scripted builds.
---

# muti-metroo embed-config

Embed a YAML configuration file into a Muti Metroo binary. The resulting binary loads that configuration at startup without a config file on disk, and runs the agent when started with no arguments. Unlike [`setup`](setup.md), this command asks no questions, so it fits CI pipelines and scripted deployments.

Global flags (`--token`, `--yes`, `--no-color`) are described in [CLI overview](overview.md#global-flags).

## Synopsis

```bash
muti-metroo embed-config <config.yaml> [flags]
```

## Description

The configuration is parsed and validated with the same checks the agent applies at startup before anything is written; an invalid file leaves the binary untouched. If the source binary already has an embedded configuration, it is replaced. The output binary keeps the source binary's file permissions.

The YAML is embedded as written, after validation. Environment variable references such as `${VAR}` are stored unexpanded and are expanded each time the binary starts. The configuration does not include an agent identity unless the YAML sets `agent.id` and `agent.private_key`; without them, the agent creates its identity in `agent.data_dir` on first start. `data_dir` defaults to `./data`, which is relative to the working directory the binary starts in, so set an absolute path for a binary that runs from different locations.

DLL files are rejected. To run the DLL build, pass a config file path to `rundll32.exe` instead (see [DLL mode](../deployment/dll-mode.md)).

Embedding does not sign or encrypt the binary. The configuration is obfuscated, not protected: anyone with the binary can read it. Use [`config show`](config.md#config-show) to inspect what a binary contains.

## Arguments

| Argument | Description |
| --- | --- |
| `<config.yaml>` | The configuration to embed. The file name must end in `.yaml` or `.yml`. |

## Flags

| Flag | Default | Description |
| --- | --- | --- |
| `-b`, `--binary` | the running executable | Source binary to embed the configuration into. |
| `-o`, `--output` | same as the source binary | Path of the binary to write. Without it, the source binary is replaced in place. |

## Examples

Write a new binary with an embedded configuration, leaving the source binary unchanged:

```bash
muti-metroo embed-config agent.yaml -o ./my-agent
```

```text
Embedded config into ./my-agent
```

Embed into a binary built for another platform:

```bash
muti-metroo embed-config agent.yaml -b ./muti-metroo-linux-amd64 -o ./edge-agent
```

Check the result and start it:

```bash
muti-metroo config validate ./my-agent
./my-agent
```

## Exit status

0 on success. 1 if the file name does not end in `.yaml` or `.yml`, the configuration fails validation, or the binary cannot be read or written. 2 for a missing argument or unknown flag.

## See Also

- [Embedded configuration](../deployment/embedded-config.md): how single-binary deployments work and when to use them
- [setup](setup.md): create or edit an embedded configuration interactively
- [config](config.md): validate a binary's embedded configuration or print it with secrets redacted
- [run](run.md): how a binary with an embedded configuration starts
