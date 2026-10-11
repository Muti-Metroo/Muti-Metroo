---
title: Embedded Configuration
description: Build a single Muti Metroo executable that carries its own configuration and identity, so nothing else has to be copied to the target host.
sidebar_position: 5
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-hammering.png" alt="Mole embedding configuration" style={{maxWidth: '180px'}} />
</div>

# Embedded Configuration

A Muti Metroo executable can carry its YAML configuration appended to the end of the file. When the binary starts, it detects the embedded config and uses it. With the agent identity embedded as well, the binary is the only file the target host needs.

Embedding works for the executables of every platform. It is not supported for the [Windows DLL](/deployment/dll-mode).

## How an embedded binary behaves

- Running the binary with no arguments starts the agent, as if `run` had been given. Set `default_action: help` at the top level of the embedded config to print help instead.
- `run` uses the embedded config and ignores `-c`.
- Every other command works normally, for example `./my-agent status` or `./my-agent --version`.
- `muti-metroo config show <binary>` and `muti-metroo config validate <binary>` read the embedded config.

On startup, the agent reports where the config came from:

```text
Loaded configuration: embedded in binary
Starting Muti Metroo agent...
Display Name: branch-office
Agent ID: 114251b9ef730e0ab0295460c7c576f7
```

The embedded config is obfuscated, not encrypted. Anyone with the binary can recover it, including secrets such as `agent.private_key` and `management.signing_private_key`. Protect the binary like the config file it contains.

## Embedded identity

An agent's identity is its agent ID and its X25519 private key, normally stored in `data_dir`. Both can live in the config instead:

```yaml
agent:
  id: "ed65187723415eea4de1ea73e3a540fc"
  private_key: "7878954c7da02d0edcda90f57cb9eb1fa8de675ef8375fd90831c2ae693b555b"
```

The values above are an example. The setup wizard generates both when it embeds a config. When you prepare a config for `embed-config` yourself, copy the values from a data directory that an agent has already started with: `agent_id`, and the key in `agent_key` (`muti-metroo init` creates only `agent_id`; the key is generated on first start). Alternatively, keep `data_dir` set so the agent creates and keeps its own identity.

When `agent.id` and `agent.private_key` are both set, the agent does not need a data directory. To run without one, set it to an empty string:

```yaml
agent:
  id: "ed65187723415eea4de1ea73e3a540fc"
  private_key: "7878954c7da02d0edcda90f57cb9eb1fa8de675ef8375fd90831c2ae693b555b"
  data_dir: ""
```

A config that leaves `data_dir` out still uses the default `./data`, relative to the working directory, and the agent creates it there. Without a data directory, runtime changes made with the dynamic management commands (`route add`, `peer add`, and similar) are not persisted across restarts.

## Create an embedded binary

### With `embed-config`

[`muti-metroo embed-config`](/cli/embed-config) embeds a prepared YAML file without prompts, for scripts and CI pipelines:

```bash
# Write a new binary, leaving the source untouched
muti-metroo embed-config agent.yaml -b ./muti-metroo -o ./branch-office

# Embed into the running muti-metroo binary in place
muti-metroo embed-config agent.yaml
```

| Flag | Default | Description |
|------|---------|-------------|
| `-b`, `--binary` | The running executable | Source binary. |
| `-o`, `--output` | The source path (in place) | Destination binary. |

The config file must have a `.yaml` or `.yml` extension and must pass validation; otherwise nothing is written. A config already embedded in the source is replaced, not stacked. The result is written to a temporary file and renamed into place, and keeps the source file's permissions. `.dll` targets are refused.

To build binaries for another platform, download that platform's release binary and pass it with `-b`:

```bash
muti-metroo embed-config agent.yaml -b ./muti-metroo-windows-amd64.exe -o ./branch-office.exe
```

### With the setup wizard

Run [`muti-metroo setup`](/cli/setup). After the configuration steps, the wizard asks how to deliver the config:

```text
Delivery method:
  Save to config file (traditional)
> Embed in binary (single-file deployment)
```

Choosing **Embed in binary** prompts for:

- **Service name**: used for the default output file name and, if you install a service, for the service and installed binary name.
- **Output binary path**: defaults to the system temporary directory, for example `/tmp/<service-name>`.

The wizard embeds the generated identity in the config, leaves `data_dir` out of it (so the default `./data` applies, as described in [Embedded identity](#embedded-identity)), copies the running binary to the output path with the config appended, and saves a copy of the config to the config path from the first step for reference. It then offers to install the embedded binary as a system service.

## Run an embedded binary

```bash
./branch-office
```

On Windows:

```powershell
.\branch-office.exe
```

## Change an embedded config

### With `embed-config`

Run `embed-config` again with the edited YAML file and the deployed binary as the source:

```bash
sudo systemctl stop branch-office
sudo muti-metroo embed-config agent.yaml -b /usr/local/bin/branch-office
sudo systemctl start branch-office
```

The new file replaces the old embedded config completely. If the old config embedded the identity, the new one must contain the same `agent.id` and `agent.private_key`, or the agent comes up with a different identity. `config show` redacts secrets, so keep the source YAML file rather than relying on reading it back from the binary.

### With the setup wizard

```bash
muti-metroo setup -c /usr/local/bin/branch-office
```

When `-c` points to a file that does not end in `.yaml` or `.yml`, the wizard treats it as a binary. It loads the embedded config as defaults, skips the config path and data directory questions, keeps the existing identity, and writes the result back into the same binary. No separate config file is written.

## Install as a service

When the setup wizard creates an embedded binary, it offers to install it as a service. The installed service runs the binary without `-c`:

| Platform | Binary location | Service definition |
|----------|-----------------|--------------------|
| Linux | `/usr/local/bin/<name>` | `/etc/systemd/system/<name>.service` with `ExecStart=/usr/local/bin/<name> run` |
| macOS | `/usr/local/bin/<name>` | `/Library/LaunchDaemons/com.<name>.plist`, logging to `/var/log/<name>.log` and `/var/log/<name>.err.log` |
| Windows | `C:\Program Files\<name>\<name>.exe` | Windows Service `<name>` |

The service is enabled and started as part of the installation. Manage it as described in [System Service](/deployment/system-service), using `<name>` in place of `muti-metroo`.

## See Also

- [embed-config CLI](/cli/embed-config) for the full `embed-config` reference
- [setup CLI](/cli/setup) for the interactive wizard, including the embed option
- [config CLI](/cli/config) for `config show` and `config validate` on embedded binaries
- [System Service](/deployment/system-service) for managing the installed service
- [Agent](/configuration/agent) for `agent.id`, `agent.private_key`, and `data_dir`
- [Security Best Practices](/security/best-practices) for protecting keys and secrets
