---
title: muti-metroo service
sidebar_label: service
sidebar_position: 11
description: Install, remove, and query Muti Metroo as a systemd, launchd, Windows, or per-user service.
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-reading.png" alt="Mole managing services" style={{maxWidth: '180px'}} />
</div>

# muti-metroo service

Registers the agent with the operating system's service manager so it starts at boot or logon, and removes or queries that registration. Every install method starts the agent immediately.

Global flags (`--token`, `--yes`, `--no-color`) are described in [CLI overview](overview.md#global-flags).

| Platform | Without `--user` | With `--user` |
| --- | --- | --- |
| Linux | systemd system unit (requires root) | systemd user unit, or cron `@reboot` with `nohup` when systemd user sessions are not available |
| macOS | launchd daemon (requires root) | Not supported |
| Windows | Windows service (requires Administrator) | `HKCU` Registry Run key that starts the DLL with `rundll32.exe` |

## service install

```bash
muti-metroo service install -c <config> [flags]
```

Installs and starts the service. The configuration path is resolved to an absolute path and must exist. The command fails if a service with the same name is already installed.

For a system service (without `--user`), the running binary is copied to a standard location first, unless `--deploy=false` is given:

| Platform | Binary location with `--deploy` |
| --- | --- |
| Linux, macOS | `/usr/local/bin/<name>` |
| Windows | `%ProgramFiles%\<name>\<name>.exe` |

With `--deploy=false`, the service runs the binary from its current location. User services always run the binary from its current location.

### Flags

| Flag | Default | Description |
| --- | --- | --- |
| `-c`, `--config` | none (required) | Path to the configuration file. |
| `-n`, `--name` | `muti-metroo` | Service name. It names the unit, plist, Windows service, or Registry value, and the deployed binary. |
| `--deploy` | `true` | Copy the binary to the standard location before installing a system service. |
| `--user` | `false` | Install a per-user service that needs no root or Administrator rights. Linux and Windows only. |
| `--user-method` | `auto` | Linux user-service backend: `auto`, `systemd`, or `cron`. `auto` uses systemd when a user session is reachable, otherwise cron. |
| `--dll` | none | Path to `muti-metroo.dll`. Required with `--user` on Windows. |

### Linux system service (systemd)

Run as root. Creates `/etc/systemd/system/<name>.service`, reloads systemd, then enables and starts the unit. The unit runs `<binary> run -c <config>` with:

- `Restart=on-failure` with a 5 second delay.
- `ProtectSystem=strict` and `ProtectHome=read-only`. The agent can write only to the configuration file's directory and the `agent.data_dir` from the configuration (`ReadWritePaths`). File uploads to other paths fail unless you extend `ReadWritePaths` in the unit.
- `NoNewPrivileges=true` and `PrivateTmp=true`.
- Output to the journal under the service name.

ICMP relay through a systemd service still needs the host's `net.ipv4.ping_group_range` sysctl to allow unprivileged ping sockets, even as root; see [ICMP configuration](../configuration/icmp.md).

### Linux user service (systemd user unit)

With `--user`, and `--user-method` set to `systemd` or to `auto` with a reachable user systemd instance. Creates `~/.config/systemd/user/<name>.service`, runs `systemctl --user enable --now`, and then attempts `loginctl enable-linger` for the current user so the service keeps running after logout. If enabling linger is not permitted, the command prints the `sudo loginctl enable-linger <user>` command to run. The unit restarts on failure and logs to the user journal.

### Linux user service (cron)

With `--user`, and `--user-method` set to `cron` or to `auto` without a reachable user systemd instance. Requires `crontab`. Creates the wrapper script `~/.<name>/<name>.sh`, adds an `@reboot` entry to the user's crontab, and runs the script. The agent runs under `nohup` with its PID in `~/.<name>/<name>.pid` and its output appended to `~/.<name>/<name>.log`. The agent is not restarted if it exits.

### macOS (launchd)

Run as root. Creates `/Library/LaunchDaemons/com.<name>.plist` and loads it with `launchctl load -w`. The daemon starts at boot and is restarted when it exits with an error. Output goes to `<name>.log` and `<name>.err.log` in the configuration file's directory, or in `/var/log` for a binary with an embedded configuration.

### Windows service

Run as Administrator. Registers a service with automatic start that runs as LocalSystem, then starts it. No recovery actions are configured, so Windows does not restart the service after a crash unless you add them (for example with `sc failure`).

### Windows user service (Registry Run key)

With `--user` and `--dll`. Adds a value to `HKCU\Software\Microsoft\Windows\CurrentVersion\Run` that runs `rundll32.exe <dll>,Run <config>` at logon, and starts the agent immediately. No console window is shown and the process appears as `rundll32.exe`. The agent is not restarted if it exits.

The Registry value name is the service name converted to PascalCase: hyphens, underscores, and spaces separate words, and the words are joined. The default `muti-metroo` becomes `MutiMetroo`, and `my-tunnel` or `My Tunnel` becomes `MyTunnel`.

### Examples

```bash
# Linux or macOS system service
sudo muti-metroo service install -c /etc/muti-metroo/config.yaml

# Keep the binary where it is
sudo muti-metroo service install -c /etc/muti-metroo/config.yaml --deploy=false

# Linux user service, backend chosen automatically
muti-metroo service install --user -c ~/muti-metroo/config.yaml

# Linux user service, force cron
muti-metroo service install --user --user-method cron -c ~/muti-metroo/config.yaml
```

```powershell
# Windows service (Administrator)
muti-metroo.exe service install -c C:\muti-metroo\config.yaml

# Windows user service with a custom name
muti-metroo.exe service install --user -n my-tunnel --dll C:\muti-metroo\muti-metroo.dll -c C:\muti-metroo\config.yaml
```

## service status

```bash
muti-metroo service status [flags]
```

Prints whether the service is installed and its current state. On Linux and Windows a user service with the given name is reported first; otherwise the system service is reported. The command exits with `0` when nothing is installed.

The state comes from the platform: `systemctl is-active` on Linux (for example `active` or `inactive`), `running` or `stopped` on macOS, and `running`, `stopped`, `starting`, or `stopping` for a Windows service. A Linux systemd user service reports the state with the linger setting, for example `active (systemd-user, linger=yes)`. A cron user service reports `active (pid <pid>, cron+nohup)` or `inactive`. A Windows user service reports `running as '<name>'` or `stopped (installed as '<name>')`.

### Flags

| Flag | Default | Description |
| --- | --- | --- |
| `-n`, `--name` | `muti-metroo` | Service name. |
| `--json` | `false` | Print a JSON object instead of text. |

### Examples

```bash
muti-metroo service status
```

```text
Service 'muti-metroo' is not installed.
```

For an installed system service:

```text
Service: muti-metroo
Status: running
Platform: darwin
```

```bash
muti-metroo service status --json
```

```json
{
  "installed": true,
  "scope": "user",
  "platform": "linux",
  "service_name": "muti-metroo",
  "status": "active (pid 48211, cron+nohup)",
  "type": "cron+nohup",
  "config_path": "/home/operator/muti-metroo/config.yaml",
  "log_path": "/home/operator/.muti-metroo/muti-metroo.log"
}
```

| Field | Description |
| --- | --- |
| `installed` | Whether a service with this name is installed. |
| `scope` | `system`, `user`, or `none`. |
| `platform` | `linux`, `darwin`, or `windows`. |
| `service_name` | The queried service name. |
| `status` | Platform state string, as in the text output. Omitted when not installed. |
| `type` | User service backend. Omitted for system services. |
| `config_path` | Configuration path recorded by a user service. Omitted when unknown. |
| `log_path` | Log file of a cron user service. Omitted otherwise. |
| `dll_path` | DLL path of a Windows user service. Omitted otherwise. |

## service uninstall

```bash
muti-metroo service uninstall [flags]
```

Stops the service and removes its registration. On Linux and Windows, a user service with the given name is removed if one exists, without root or Administrator rights. Otherwise the system service is removed, which requires root or Administrator rights. If no service is installed, the command prints a message and exits with `0`.

Before removing, the command asks `Continue? [y/N]:`. Any answer other than `y`, `Y`, or `yes` prints `Aborted.` and exits with `0`. Pass `-f` to skip the question.

What is removed:

| Backend | Removed |
| --- | --- |
| systemd system unit | Unit stopped, disabled, and deleted; systemd reloaded. |
| systemd user unit | Unit disabled and stopped, deleted; user systemd reloaded. |
| cron user service | Process stopped, crontab entry and wrapper script deleted. The log file is kept. |
| launchd | Daemon unloaded, plist deleted. |
| Windows service | Service stopped and deleted. |
| Registry Run key | Process stopped, Registry value deleted. |

A binary copied by `--deploy` is not deleted.

### Flags

| Flag | Default | Description |
| --- | --- | --- |
| `-n`, `--name` | `muti-metroo` | Service name. |
| `-f`, `--force` | `false` | Remove without asking for confirmation. |

### Examples

```bash
# System service on Linux or macOS
sudo muti-metroo service uninstall

# User service, no prompt
muti-metroo service uninstall -f

# Service installed under a custom name
sudo muti-metroo service uninstall -n edge-agent
```

## Managing an installed service

After installation, the platform's own tools control the service:

```bash
# Linux system service
sudo systemctl restart muti-metroo
sudo journalctl -u muti-metroo -f

# Linux systemd user service
systemctl --user restart muti-metroo
journalctl --user -u muti-metroo -f

# Linux cron user service
tail -f ~/.muti-metroo/muti-metroo.log

# macOS
sudo launchctl list com.muti-metroo
tail -f /etc/muti-metroo/muti-metroo.log
```

```powershell
# Windows service
sc query muti-metroo
sc stop muti-metroo
sc start muti-metroo

# Windows user service
reg query "HKCU\Software\Microsoft\Windows\CurrentVersion\Run" /v MutiMetroo
```

The macOS log path above assumes the configuration file is in `/etc/muti-metroo`. Restart a Windows user service by logging off and on, or by uninstalling and installing it again.

## Choosing a backend

| | systemd system | systemd user | cron user | launchd | Windows service | Registry Run |
| --- | --- | --- | --- | --- | --- | --- |
| Needs root or Administrator | Yes | No | No | Yes | Yes | No |
| Starts | At boot | At boot with linger, otherwise at login | At boot | At boot | At boot | At logon |
| Restarts after a crash | Yes | Yes | No | Yes | No | No |
| Logs | Journal | User journal | `~/.<name>/<name>.log` | Configuration directory | Not captured | Not captured |
| Runs as | root | Current user | Current user | root | LocalSystem | Current user |

## See Also

- [System service deployment](../deployment/system-service.md): end-to-end guide to running agents as services
- [DLL mode](../deployment/dll-mode.md): build and use the Windows DLL for Registry Run installs
- [muti-metroo run](run.md): the command every service runs
- [muti-metroo stop](stop.md): stop an agent through its HTTP API
- [ICMP configuration](../configuration/icmp.md): host settings needed for ping relay
