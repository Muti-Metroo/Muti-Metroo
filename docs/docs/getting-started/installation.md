---
title: Installation
description: Install the Muti Metroo binary on Linux, macOS, or Windows, build it from source, and run it as a system service.
sidebar_position: 3
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-hammering.png" alt="Mole building" style={{maxWidth: '180px'}} />
</div>

# Installation

Muti Metroo is a single binary with no runtime dependencies. It runs as an unprivileged user; root or Administrator is only needed to install it as a system service or, on Linux and macOS, to listen on ports below 1024.

## Supported platforms

Each release publishes these binaries:

| Platform | Architecture | Asset |
| --- | --- | --- |
| Linux | x86_64 | `muti-metroo-linux-amd64` |
| Linux | ARM64 | `muti-metroo-linux-arm64` |
| macOS | Apple Silicon | `muti-metroo-darwin-arm64` |
| macOS | Intel | `muti-metroo-darwin-amd64` |
| Windows | x86_64 | `muti-metroo-windows-amd64.exe`, `muti-metroo-windows-amd64.dll` |
| Windows | ARM64 | `muti-metroo-windows-arm64.exe`, `muti-metroo-windows-arm64.dll` |

The Windows DLL runs the agent through `rundll32.exe` without a console window. See [DLL Mode](/deployment/dll-mode).

## Install a release binary

The [Download](/download) page has download buttons and per-platform instructions. On Linux and macOS, the steps are:

```bash
curl -L -O https://github.com/Muti-Metroo/Muti-Metroo/releases/latest/download/muti-metroo-linux-amd64
chmod +x muti-metroo-linux-amd64
sudo mv muti-metroo-linux-amd64 /usr/local/bin/muti-metroo
muti-metroo --version
```

Replace `linux-amd64` with the asset for your platform from the table above. To check the download against the published SHA-256 checksums before installing it, see [Verify your download](/download#verify-your-download).

## Build from source

Building requires Go 1.26 or later.

```bash
git clone https://github.com/Muti-Metroo/Muti-Metroo.git
cd Muti-Metroo
make build
./build/muti-metroo --version
```

## Run with Docker

The repository includes a `Dockerfile` and a Compose file. See [Docker Deployment](/deployment/docker).

## Install as a system service

`muti-metroo service install` registers the agent with the platform's service manager, then enables and starts it. It needs a config file:

```bash
sudo muti-metroo service install -c /etc/muti-metroo/config.yaml
```

| Platform | Service manager | Privileges |
| --- | --- | --- |
| Linux | systemd | root |
| Linux (`--user`) | systemd user unit, or cron `@reboot` when systemd is unavailable | none |
| macOS | launchd | root |
| Windows | Windows Service | Administrator |
| Windows (`--user`) | Registry Run key with the DLL | none |

By default, `service install` copies the binary to a standard system location such as `/usr/local/bin`; pass `--deploy=false` to use the binary where it is. Remove the service with `muti-metroo service uninstall`. The [System Service](/deployment/system-service) guide covers each platform, and the [`service` command reference](/cli/service) lists every flag.

## Agent identity and data directory

Each agent has a 128-bit agent ID, stored in its data directory (`agent.data_dir`, default `./data`). The agent creates the ID on first start if none exists. To create it in advance, for example to put the ID in another agent's peer config before this agent runs, use `init`:

```bash
muti-metroo init -d ./data
```

```text
Agent initialized in ./data
Agent ID: 250bcc6f4401c0cfe55de1c8b643c9db
```

Running `init` again on an initialized directory prints the existing ID and changes nothing.

`init` creates `agent_id` in the data directory. On first start, the agent also creates `agent_key` and `agent_key.pub`, the X25519 key pair used for end-to-end encryption. Keep the data directory between restarts; deleting it gives the agent a new identity.

TLS certificates for peer connections are generated in memory at startup when none are configured, so a first agent needs no certificate setup. See [TLS Certificates](/configuration/tls-certificates) to use your own CA.

## See Also

- [Download](/download): download buttons, checksum verification, and the PDF user manual
- [Quick Start](/getting-started/quick-start): write a minimal config and run a first agent
- [Interactive Setup](/getting-started/interactive-setup): generate a config with the setup wizard
- [System Service](/deployment/system-service): service installation details for each platform
- [`init` command](/cli/init): reference for agent initialization
