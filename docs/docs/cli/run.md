---
title: muti-metroo run
sidebar_label: run
sidebar_position: 2
description: Start a Muti Metroo agent in the foreground from a config file or an embedded configuration.
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-reading.png" alt="Mole running commands" style={{maxWidth: '180px'}} />
</div>

# muti-metroo run

Start the agent. It loads the configuration, opens its listeners, connects to its peers, and runs in the foreground until it receives `SIGINT` (Ctrl+C), `SIGTERM`, or a [`stop`](stop.md) request. To keep an agent running across reboots, install it as a [system service](service.md).

Global flags (`--token`, `--yes`, `--no-color`) are described in [CLI overview](overview.md#global-flags).

## Synopsis

```bash
muti-metroo run [flags]
```

## Description

On startup the agent:

1. Loads the configuration. Environment variable references such as `${DATA_DIR:-./data}` are expanded first (see [Environment variables](../configuration/environment-variables.md)). An invalid configuration stops startup with an error.
2. Prints a warning to stderr for every configured TLS certificate file that has expired or expires within 60 days (`Notice` under 60 days, `WARNING` under 30 days or expired), and for deprecated config keys that are accepted but ignored.
3. Creates the agent identity in `agent.data_dir` if it does not exist yet: `agent_id`, plus the end-to-end encryption keypair `agent_key` and `agent_key.pub`. An identity set inline in the configuration (`agent.id`, `agent.private_key`) is used instead.
4. Waits for the startup delay, if one is set.
5. Starts the HTTP API, listeners, SOCKS proxy, and peer connections, then reapplies runtime changes made with commands such as [`route add`](route.md#route-add) and [`peer add`](peer.md#peer-add) that were persisted in the data directory.

Log lines go to stderr in the format set by `agent.log_format`; the status lines shown below go to stdout.

On `SIGINT`, `SIGTERM`, or a `stop` request, the agent closes its connections and exits with status 0. If shutdown does not finish within 10 seconds, the process exits with status 1.

### Embedded configuration

When the binary carries an embedded configuration (created with [`embed-config`](embed-config.md) or [`setup`](setup.md)), `run` uses it and ignores `-c`. Running such a binary with no arguments at all is the same as `run`, unless the embedded configuration sets `default_action: help`, in which case it prints help instead.

```bash
./my-agent
```

### Windows services

When the binary is started by the Windows Service Control Manager, `run` runs as a Windows service instead of a console program. Install the service with [`service install`](service.md#service-install) rather than registering it by hand.

## Flags

| Flag | Default | Description |
| --- | --- | --- |
| `-c`, `--config` | `./config.yaml` | Path to the configuration file. Ignored when the binary has an embedded configuration. |
| `--startup-delay` | `0` | Wait this long before any network activity, for example `90s` or `2m`. Overrides `agent.startup_delay` in the configuration. |
| `-D`, `--detach` | `false` | Windows only. Start the agent as a new process outside the SSH session's job object and return immediately, so the agent keeps running after the SSH session ends. On Linux and macOS the command fails; use `nohup` or `setsid` instead. |

## Examples

Run with a config file in the current directory:

```bash
muti-metroo run
```

Run with an explicit config file:

```bash
muti-metroo run -c /etc/muti-metroo/config.yaml
```

```text
Loaded configuration from: /etc/muti-metroo/config.yaml (mtime 2026-10-11 14:54:10 UTC)
Starting Muti Metroo agent...
Display Name: gateway-1
Agent ID: abc123def456789012345678901234ab
time=2026-10-11T14:54:10.179Z level=INFO msg="starting agent" agent_id=abc123de component=agent
time=2026-10-11T14:54:10.179Z level=INFO msg="HTTP server started" address=127.0.0.1:8080
time=2026-10-11T14:54:10.179Z level=INFO msg="listener started" address=0.0.0.0:4433 transport=quic
time=2026-10-11T14:54:10.179Z level=INFO msg="SOCKS5 server started" address=127.0.0.1:1080
time=2026-10-11T14:54:10.179Z level=INFO msg="forward handler started" endpoints=0
time=2026-10-11T14:54:10.179Z level=INFO msg="agent started" agent_id=abc123de peers=0 listeners=1
SOCKS5 server: 127.0.0.1:1080
Status: running (peers: 0, routes: 0)
```

Delay network activity by 90 seconds, for example to let other services come up first:

```bash
muti-metroo run -c config.yaml --startup-delay 90s
```

Set config values through environment variables:

```bash
DATA_DIR=/var/lib/muti-metroo muti-metroo run -c config.yaml
```

with this in `config.yaml`:

```yaml
agent:
  data_dir: "${DATA_DIR:-./data}"
```

Keep the agent running after the SSH session closes (Windows):

```bash
muti-metroo.exe run -c config.yaml --detach
```

## See Also

- [Configuration overview](../configuration/overview.md): every config section the agent reads
- [config validate](config.md#config-validate): check a config file without starting the agent
- [service](service.md): run the agent as a systemd, launchd, or Windows service
- [stop](stop.md): stop a running agent through its HTTP API
- [Embedded configuration](../deployment/embedded-config.md): ship a single binary that starts with no arguments
