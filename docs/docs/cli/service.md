---
title: service
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-reading.png" alt="Mole managing services" style={{maxWidth: '180px'}} />
</div>

# muti-metroo service

Install the agent as a system service so it starts automatically on boot and restarts if it crashes. Works on Linux (systemd, systemd --user, or cron), macOS (launchd), and Windows.

**Quick install:**
```bash
# Linux/macOS (requires root)
sudo muti-metroo service install -c /etc/muti-metroo/config.yaml

# Linux without root (auto-picks systemd --user when available, else cron+nohup)
muti-metroo service install --user -c ~/muti-metroo/config.yaml

# Windows (as Administrator)
muti-metroo service install -c C:\muti-metroo\config.yaml

# Windows without admin (uses Registry Run key + DLL)
muti-metroo service install --user --dll C:\path\to\muti-metroo.dll -c C:\muti-metroo\config.yaml
```

## Subcommands

### service install

Install as system service.

```bash
muti-metroo service install -c <config-file> [-n <service-name>] [--user] [--dll <dll-path>]
```

**Flags:**
- `-c, --config <file>`: Configuration file path (required)
- `-n, --name <name>`: Service name (default: muti-metroo)
- `--deploy`: Copy binary to system location (default: true). Installs to `/usr/local/bin` on Linux/macOS or `C:\Program Files\<name>` on Windows. Use `--deploy=false` to use the binary from its current location.
- `--user`: Install as user service (Linux: systemd --user or cron+nohup, auto-detected; Windows: Registry Run)
- `--user-method <auto|systemd|cron>`: Force a specific Linux user-service backend (default: `auto`, prefers `systemd` when available)
- `--dll <path>`: Path to muti-metroo.dll (Windows `--user` mode only)

**Linux (systemd)** - requires root:
- Creates `/etc/systemd/system/muti-metroo.service`
- Reloads systemd daemon
- Enables automatic startup

**Linux (systemd --user)** - with `--user` flag, no root required (preferred when available):
- Creates `~/.config/systemd/user/muti-metroo.service`
- Reloads the user systemd manager and enables the unit (`systemctl --user enable --now`)
- Best-effort runs `loginctl enable-linger <user>` so the unit survives logout (prints a warning if linger could not be enabled)
- Logs to journald (view with `journalctl --user -u muti-metroo -f`)

**Linux (cron+nohup)** - with `--user` flag, used when systemd --user is not available:
- Creates `~/.muti-metroo/muti-metroo.sh` wrapper script
- Adds `@reboot` cron entry
- Logs to `~/.muti-metroo/muti-metroo.log`

**macOS (launchd)** - requires root:
- Creates `/Library/LaunchDaemons/com.muti-metroo.plist`
- Loads service with `launchctl`

**Windows Service** - requires Administrator:
- Registers Windows Service
- Sets to automatic startup

**Windows Registry Run** - with `--user --dll` flags, no admin required:
- Adds entry to `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`
- Runs at user logon via `rundll32.exe`
- No console window (background execution)
- The `-n` flag sets the Registry value name, converted to PascalCase (e.g., "My Tunnel" becomes "MyTunnel")

:::tip Auto-Start
All installation methods start the service immediately after installation. You don't need to manually start it or reboot.
:::

### service uninstall

Uninstall system service.

```bash
muti-metroo service uninstall [-n <service-name>] [-f]
```

**Flags:**
- `-n, --name <name>`: Service name (default: muti-metroo)
- `-f, --force`: Skip confirmation prompt

Removes service registration. On Linux and Windows, automatically detects whether system service or user service was used.

### service status

Check service status.

```bash
muti-metroo service status [-n <service-name>] [--json]
```

**Flags:**
- `-n, --name <name>`: Service name (default: muti-metroo)
- `--json`: Emit the status as structured JSON instead of the human-readable summary.

Shows current service state (running, stopped, etc.).

**`--json` output** is a single object intended for fleet tooling. The shape is cross-platform-native: it carries the per-OS status string Muti Metroo already computes rather than backend-specific booleans. Field names are part of the CLI's JSON contract and are not localized. `status`, `type`, `config_path`, `log_path`, and `dll_path` are omitted when empty.

```json
{
  "installed": true,
  "scope": "user",
  "platform": "linux",
  "service_name": "muti-metroo",
  "status": "running",
  "type": "cron+nohup",
  "config_path": "/home/user/.muti-metroo/config.yaml",
  "log_path": "/home/user/.muti-metroo/muti-metroo.log"
}
```

| Field | Description |
|-------|-------------|
| `installed` | Whether a service is installed under this name |
| `scope` | `system`, `user`, or `none` |
| `platform` | Operating system the agent is running on |
| `service_name` | Service name queried |
| `status` | Per-OS status string (e.g., `running`, `stopped`) |
| `type` | User-service backend, e.g. `cron+nohup` (Linux) or `Registry Run key` (Windows) |
| `config_path` | Recorded config path, if any |
| `log_path` | Recorded log path, if any |
| `dll_path` | Recorded DLL path (Windows Registry Run installs) |

:::note Windows detach hint
When `muti-metroo run -D` cannot break away from the OpenSSH Job Object, the detach failure suggests installing as a service instead. The suggestion is admin-aware: Administrators are pointed at the system install (`muti-metroo service install`), while non-Administrators are pointed at the per-user (Registry Run key) install (`muti-metroo service install --user --dll <path> -c <config>`).
:::

## Linux: Systemd vs Systemd --user vs Cron+Nohup

| Feature | Systemd (root) | Systemd --user | Cron+Nohup |
|---------|---------------|----------------|------------|
| Requires root | Yes | No | No |
| Auto-restart on crash | Yes | Yes | No |
| Log management | journald | journald (per-user) | File-based |
| Resource limits | cgroups | cgroups (per-user) | None |
| Start on boot | Yes | At user login (or always with linger) | Yes |
| Survives logout | n/a | Only with `loginctl enable-linger` | Yes |

`muti-metroo service install --user` auto-picks `systemd --user` when the user has a working systemd session, otherwise falls back to cron+nohup. Use `--user-method=systemd` or `--user-method=cron` to force one.

**Use systemd (root) when:**
- You have root access
- The agent should run before any user logs in

**Use systemd --user when:**
- You don't have root, but the host has systemd and a user session bus
- You want auto-restart and journald logs without privileged install

**Use cron+nohup when:**
- The host has no systemd at all, or no user session bus is available

## Windows: Windows Service vs Registry Run

| Feature | Windows Service | Registry Run |
|---------|----------------|--------------|
| Requires admin | Yes | No |
| Auto-restart on crash | Yes | No |
| Start on boot | Yes (before login) | Yes (at login) |
| Console window | No | No |
| Runs as | SYSTEM/service account | Current user |
| Process name | `muti-metroo.exe` | `rundll32.exe` |
| Requires DLL | No | Yes |

**Use Windows Service when:**
- You have Administrator access
- You need automatic restart on crash
- Service must start before user login

**Use Registry Run when:**
- You don't have Administrator access
- User-level background execution is sufficient
- You want minimal installation footprint

## Linux Management (Systemd)

After installation with root (service auto-starts):

```bash
# Check status
sudo systemctl status muti-metroo

# View logs
sudo journalctl -u muti-metroo -f

# Restart (after config changes)
sudo systemctl restart muti-metroo

# Stop
sudo systemctl stop muti-metroo
```

:::note ICMP (ping) needs a host sysctl, even as root
The generated unit does not restrict ICMP (no `RestrictAddressFamilies`, `SystemCallFilter`, `PrivateNetwork`, or capability drops), and a system install runs as **root**. But ICMP relay still needs `net.ipv4.ping_group_range` widened on the host: Muti Metroo uses unprivileged ICMP *ping sockets*, which the kernel gates **solely** on that sysctl and which **do not honor `CAP_NET_RAW`** -- so running as root does **not** bypass it. The default `1 0` (disabled) makes `muti-metroo ping` fail with "failed to create ICMP socket".

```bash
sudo sysctl -w net.ipv4.ping_group_range="0 2147483647"
echo "net.ipv4.ping_group_range=0 2147483647" | sudo tee /etc/sysctl.d/99-muti-metroo-ping.conf
```

No agent restart is required -- the socket is created per ping session. If you harden the unit with `User=` (a dedicated unprivileged account), that user's GID must fall inside the range. See [ICMP Configuration](/configuration/icmp). Operators who want this applied automatically can add `ExecStartPre=+/sbin/sysctl -w net.ipv4.ping_group_range="0 2147483647"` to the unit (the `+` runs it as root); Muti Metroo does not do this by default to avoid mutating a global kernel setting.
:::

## Linux Management (Systemd --user)

After installation with `--user` on a host where systemd --user is available:

```bash
# Check status
muti-metroo service status                  # cross-platform wrapper
systemctl --user status muti-metroo         # native

# View logs
journalctl --user -u muti-metroo -f

# Restart (after config changes)
systemctl --user restart muti-metroo

# Stop
systemctl --user stop muti-metroo

# Keep the agent running after logout (one-time, requires sudo on most distros)
sudo loginctl enable-linger $USER

# Uninstall
muti-metroo service uninstall
```

## Linux Management (Cron+Nohup)

After installation with `--user` when cron+nohup was selected (no systemd --user available):

```bash
# Check status
muti-metroo service status

# View logs
tail -f ~/.muti-metroo/muti-metroo.log

# Manually start (if stopped)
~/.muti-metroo/muti-metroo.sh

# Uninstall
muti-metroo service uninstall
```

## macOS Management

After installation:

```bash
# Check status
sudo launchctl list | grep muti-metroo

# Stop service
sudo launchctl stop com.muti-metroo

# Start service
sudo launchctl start com.muti-metroo

# View logs (in the config file's directory)
tail -f /etc/muti-metroo/muti-metroo.log
```

## Windows Management (Windows Service)

After installation as Administrator:

```powershell
# Start service
sc start muti-metroo

# Check status
sc query muti-metroo

# Stop service
sc stop muti-metroo
```

## Windows Management (Registry Run)

After installation with `--user --dll`:

```powershell
# Check status (shows DLL and config paths)
muti-metroo service status

# Uninstall
muti-metroo service uninstall

# View registry entry (default name)
reg query "HKCU\Software\Microsoft\Windows\CurrentVersion\Run" /v Muti Metroo

# View registry entry (custom name, e.g., -n "My Tunnel" becomes "MyTunnel")
reg query "HKCU\Software\Microsoft\Windows\CurrentVersion\Run" /v MyTunnel

# Stop manually (if needed)
taskkill /F /IM rundll32.exe
```

:::tip Registry Value Names
The `-n` flag sets the service name, which is converted to PascalCase for the registry value:
- `muti-metroo` (default) becomes `Muti Metroo`
- `my-tunnel` becomes `MyTunnel`
- `My Tunnel` becomes `MyTunnel`
:::

:::note
The service starts automatically after installation and at each user logon. To restart the service, uninstall and reinstall it, or log out and log back in.
:::

## Examples

```bash
# Linux install (systemd, requires root) - auto-starts
sudo muti-metroo service install -c /etc/muti-metroo/config.yaml

# Linux install (cron+nohup, no root required) - auto-starts
muti-metroo service install --user -c ~/muti-metroo/config.yaml

# macOS install - auto-starts
sudo muti-metroo service install -c /etc/muti-metroo/config.yaml

# Windows install (as Administrator) - auto-starts
muti-metroo service install -c C:\Program Files\muti-metroo\config.yaml

# Windows install (no admin required, uses DLL) - auto-starts
muti-metroo service install --user --dll C:\path\to\muti-metroo.dll -c C:\path\to\config.yaml

# Windows install with custom name (no admin required)
muti-metroo service install --user -n "My Tunnel" --dll C:\path\to\muti-metroo.dll -c C:\path\to\config.yaml

# Check status (all platforms)
muti-metroo service status

# Uninstall (auto-detects installation type)
sudo muti-metroo service uninstall  # Linux systemd / macOS
muti-metroo service uninstall        # Linux user service / Windows user service
```
