---
title: System Service
description: Install Muti Metroo as a systemd, launchd, or Windows service so the agent starts at boot and is supervised by the operating system.
sidebar_position: 2
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-climbing.png" alt="Mole installing service" style={{maxWidth: '180px'}} />
</div>

# System Service

`muti-metroo service install` registers the agent with the operating system's service manager so it starts at boot and is restarted by the service manager where the platform supports it. The same command also installs user-level services that need no root or Administrator rights.

| Platform | System service (root / Administrator) | User service (no elevation) |
|----------|---------------------------------------|-----------------------------|
| Linux | systemd unit | systemd `--user` unit, or cron `@reboot` + `nohup` |
| macOS | launchd daemon | Not supported |
| Windows | Windows Service | Registry Run key + `rundll32.exe` (requires the DLL) |

For the full flag reference, see [CLI - service](/cli/service).

## Before you install

- Use absolute paths in the config file for `agent.data_dir`, certificates, and keys. The service manager starts the agent with the config file's directory as the working directory, so relative paths resolve against that directory.
- On Linux, create the data directory before installing. The generated unit lists it in `ReadWritePaths`, and systemd refuses to start a unit whose `ReadWritePaths` entry does not exist.
- Validate the config with `muti-metroo config validate <file>` before installing. A service that fails validation exits immediately and is restarted in a loop.

## Linux (systemd)

### Install

```bash
sudo mkdir -p /etc/muti-metroo /var/lib/muti-metroo
sudo cp config.yaml /etc/muti-metroo/
sudo muti-metroo service install -c /etc/muti-metroo/config.yaml
```

The command:

1. Copies the running binary to `/usr/local/bin/muti-metroo` (`/usr/local/bin/<name>` with `-n <name>`). Pass `--deploy=false` to keep the binary at its current path instead.
2. Writes `/etc/systemd/system/muti-metroo.service`.
3. Runs `systemctl daemon-reload`, `systemctl enable`, and `systemctl start`.

No separate `systemctl enable` or `systemctl start` step is needed.

### Generated unit file

For a config at `/etc/muti-metroo/config.yaml` with `data_dir: "/var/lib/muti-metroo"`, the installer writes:

```ini
[Unit]
Description=Userspace mesh networking agent for virtual TCP tunnels
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=/usr/local/bin/muti-metroo run -c /etc/muti-metroo/config.yaml
WorkingDirectory=/etc/muti-metroo
Restart=on-failure
RestartSec=5
TimeoutStopSec=30

# Security hardening
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=read-only
PrivateTmp=true
ReadWritePaths=/etc/muti-metroo /var/lib/muti-metroo

# Logging
StandardOutput=journal
StandardError=journal
SyslogIdentifier=muti-metroo

[Install]
WantedBy=multi-user.target
```

What this means in practice:

- **The agent runs as root.** The unit has no `User=` line. To run as an unprivileged account, add `User=` and `Group=` with `sudo systemctl edit muti-metroo` and give that account ownership of the data directory.
- **Most of the filesystem is read-only.** `ProtectSystem=strict` makes everything except the config directory and the data directory read-only, `ProtectHome=read-only` covers `/home`, and `PrivateTmp=true` gives the agent its own `/tmp`. [File transfer](/features/file-transfer) uploads and [shell](/features/shell) commands that write elsewhere fail with a read-only filesystem error. Add more paths with a `ReadWritePaths=` override in `systemctl edit`.
- **Restart on failure only.** A crash or non-zero exit is restarted after 5 seconds. A clean exit, such as one requested with [`muti-metroo stop`](/cli/stop), is not.

### Manage the service

```bash
sudo systemctl status muti-metroo
sudo journalctl -u muti-metroo -f
sudo systemctl restart muti-metroo
sudo systemctl stop muti-metroo
muti-metroo service status
```

Restart the service after editing the config file. Runtime changes made with the dynamic management commands (`route add`, `peer add`, and similar) are persisted in the data directory and survive a restart.

### Upgrade

Replace the installed binary while the service is stopped:

```bash
sudo systemctl stop muti-metroo
sudo install -m 755 ./muti-metroo-linux-amd64 /usr/local/bin/muti-metroo
sudo systemctl start muti-metroo
```

### Uninstall

```bash
sudo muti-metroo service uninstall
```

The command stops and disables the unit, removes the unit file, and reloads systemd. It prompts for confirmation; pass `-f` to skip the prompt. The binary in `/usr/local/bin`, the config file, and the data directory are left in place.

## Linux user service

Install without root by adding `--user`:

```bash
muti-metroo service install --user -c ~/muti-metroo/config.yaml
```

The installer picks a backend automatically. Force one with `--user-method systemd` or `--user-method cron`.

| Backend | Selected when | What is created | Logs |
|---------|---------------|-----------------|------|
| `systemd` | `systemctl` and `loginctl` are installed and a user systemd instance is reachable | `~/.config/systemd/user/<name>.service`, enabled and started | `journalctl --user -u muti-metroo -f` |
| `cron` | systemd `--user` is not available and `crontab` is installed | A wrapper script `~/.<name>/<name>.sh`, a PID file, and an `@reboot` crontab entry | `~/.<name>/<name>.log` |

User services differ from the system service:

- The binary is not copied. The service runs the binary from the path it was installed from, so do not move or delete it.
- The systemd user unit has no `ProtectSystem` or `ProtectHome` restrictions and is wanted by `default.target`.
- A systemd user unit stops when the user logs out unless lingering is enabled. The installer tries `loginctl enable-linger`; if that is not permitted, it prints the command to run as root:

  ```bash
  sudo loginctl enable-linger "$USER"
  ```

- The cron backend starts the agent once at boot and does not restart it if it exits.

Check and remove a user service with the same commands, without `sudo`:

```bash
muti-metroo service status
muti-metroo service uninstall
```

## macOS (launchd)

### Install

```bash
sudo mkdir -p /etc/muti-metroo /var/lib/muti-metroo
sudo cp config.yaml /etc/muti-metroo/
sudo muti-metroo service install -c /etc/muti-metroo/config.yaml
```

The command copies the binary to `/usr/local/bin/muti-metroo`, writes `/Library/LaunchDaemons/com.muti-metroo.plist`, and loads it with `launchctl load -w`. The daemon starts immediately and at every boot.

### Generated plist

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.muti-metroo</string>

    <key>ProgramArguments</key>
    <array>
        <string>/usr/local/bin/muti-metroo</string>
        <string>run</string>
        <string>-c</string>
        <string>/etc/muti-metroo/config.yaml</string>
    </array>

    <key>WorkingDirectory</key>
    <string>/etc/muti-metroo</string>

    <key>RunAtLoad</key>
    <true/>

    <key>KeepAlive</key>
    <dict>
        <key>SuccessfulExit</key>
        <false/>
    </dict>

    <key>ThrottleInterval</key>
    <integer>5</integer>

    <key>StandardOutPath</key>
    <string>/etc/muti-metroo/muti-metroo.log</string>

    <key>StandardErrorPath</key>
    <string>/etc/muti-metroo/muti-metroo.err.log</string>

    <key>ProcessType</key>
    <string>Background</string>
</dict>
</plist>
```

The daemon runs as root. `KeepAlive` with `SuccessfulExit` set to `false` restarts the agent only after a non-zero exit. Log files are written to the config file's directory.

### Manage the service

```bash
muti-metroo service status
sudo launchctl list com.muti-metroo
sudo launchctl stop com.muti-metroo
sudo launchctl start com.muti-metroo
tail -f /etc/muti-metroo/muti-metroo.err.log
```

The agent writes its log lines to stderr, so most output goes to `muti-metroo.err.log`.

### Uninstall

```bash
sudo muti-metroo service uninstall
```

This unloads the daemon and removes the plist. The binary, config, and data directory are left in place.

## Windows Service

### Install

Run from an elevated (Administrator) prompt:

```powershell
muti-metroo.exe service install -c C:\ProgramData\muti-metroo\config.yaml
```

The command copies the binary to `C:\Program Files\muti-metroo\muti-metroo.exe`, registers a service named `muti-metroo` with the display name "Muti Metroo Mesh Agent", sets it to start automatically at boot, and starts it. The service runs as `LocalSystem`.

Use Windows paths with escaped backslashes or forward slashes in the config file:

```yaml
agent:
  data_dir: "C:/ProgramData/muti-metroo/data"
```

### Manage the service

```powershell
muti-metroo.exe service status
sc.exe query muti-metroo
sc.exe stop muti-metroo
sc.exe start muti-metroo
```

The service is also listed in `services.msc` as "Muti Metroo Mesh Agent".

The installer does not configure recovery actions, so Windows does not restart the agent after a crash. To enable restarts:

```powershell
sc.exe failure muti-metroo reset= 86400 actions= restart/5000/restart/5000/restart/5000
```

The agent's log output is not captured when it runs as a Windows Service. To diagnose startup problems, stop the service and run the same command in a console:

```powershell
& "C:\Program Files\muti-metroo\muti-metroo.exe" run -c C:\ProgramData\muti-metroo\config.yaml
```

### Uninstall

```powershell
muti-metroo.exe service uninstall
```

## Windows user service (Registry Run)

Without Administrator rights, install a per-user service that starts the [DLL](/deployment/dll-mode) through `rundll32.exe` at logon:

```powershell
muti-metroo.exe service install --user --dll C:\Users\alice\muti-metroo\muti-metroo.dll -c C:\Users\alice\muti-metroo\config.yaml
```

`--dll` is required in this mode and must point to the DLL that matches the host architecture. See [CLI - service](/cli/service) for the other flags.

The command:

- Writes a value under `HKCU\Software\Microsoft\Windows\CurrentVersion\Run` that runs `rundll32.exe <dll>,Run <config>` at each logon. The value name is the service name in PascalCase: `muti-metroo` becomes `MutiMetroo`, `my-tunnel` becomes `MyTunnel`.
- Saves the DLL and config paths in `%USERPROFILE%\.<name>\service.info`.
- Starts the agent immediately through a temporary scheduled task. If Task Scheduler is unavailable, the agent starts at the next logon.

The Run key stores the paths unquoted, so keep the DLL and config paths free of spaces.

```powershell
muti-metroo.exe service status
reg query "HKCU\Software\Microsoft\Windows\CurrentVersion\Run" /v MutiMetroo
muti-metroo.exe service uninstall
```

`service uninstall` removes the Run value and stops the running `rundll32.exe` process that loaded the DLL.

## Windows: background start without a service

`run --detach` (short `-D`) re-launches the agent as a detached background process and exits. The agent keeps running after the console closes or an SSH session disconnects, but does not survive a reboot. It needs no Administrator rights, no DLL, and no Task Scheduler.

```powershell
muti-metroo.exe run --detach -c C:\Users\alice\muti-metroo\config.yaml
```

```text
Detached agent started (PID 4312)
```

The detached process runs with the user profile directory as its working directory, so pass an absolute config path and use absolute paths inside the config. `--detach` is supported on Windows only; on Linux and macOS use a service, `nohup`, or `setsid`.

Stop the agent with [`muti-metroo stop`](/cli/stop) or `taskkill /IM muti-metroo.exe /F`.

### Choosing a Windows method

| | Windows Service | Registry Run | `run --detach` |
|---|---|---|---|
| Requires Administrator | Yes | No | No |
| Requires the DLL | No | Yes | No |
| Starts | At boot | At user logon | When run |
| Restarted after a crash | Only with `sc.exe failure` | No | No |
| Runs as | `LocalSystem` | Current user | Current user |
| Process name | `muti-metroo.exe` | `rundll32.exe` | `muti-metroo.exe` |

## Ports and firewalls

Binding a listener to a port below 1024 requires root on Linux and macOS. The system service runs as root, so this works there. For a user service or a manual run on Linux, grant the capability to the binary instead:

```bash
sudo setcap 'cap_net_bind_service=+ep' /usr/local/bin/muti-metroo
```

Open the listener ports in the host firewall. QUIC uses UDP; HTTP/2 and WebSocket use TCP. Keep the SOCKS and HTTP API ports closed to the outside unless clients need them.

```bash
# firewalld
sudo firewall-cmd --permanent --add-port=4433/udp
sudo firewall-cmd --reload

# ufw
sudo ufw allow 4433/udp
```

## Troubleshooting

**The unit fails with `status=226/NAMESPACE`.** A path in `ReadWritePaths` does not exist. Create the data directory, or fix `agent.data_dir`, then run `sudo systemctl restart muti-metroo`.

**The service starts and stops in a loop.** Read the last log lines and run the same command manually to see the error:

```bash
sudo journalctl -u muti-metroo -n 50
sudo /usr/local/bin/muti-metroo run -c /etc/muti-metroo/config.yaml
```

**`service install` fails with "already installed".** Remove the existing service first with `service uninstall`, or install a second instance under a different name with `-n`.

## See Also

- [CLI - service](/cli/service) - Flags for `service install`, `status`, and `uninstall`
- [PM2 Process Manager](/deployment/pm2) - Supervise the agent with PM2 instead of the OS service manager
- [Embedded Configuration](/deployment/embedded-config) - Install a single binary that carries its own config
- [DLL Mode](/deployment/dll-mode) - Run the agent through `rundll32.exe` on Windows
- [Common Issues](/troubleshooting/common-issues) - Startup errors and their fixes
