---
title: System Service
sidebar_position: 4
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-climbing.png" alt="Mole installing service" style={{maxWidth: '180px'}} />
</div>

# System Service Installation

Install once, run forever. The agent starts automatically when the system boots and restarts if it crashes - no manual intervention needed.

**What you get:**
- Automatic start on boot - agent runs without logging in
- Crash recovery - system restarts the agent if it dies
- Managed lifecycle - start, stop, and check status with standard commands
- Security hardening - runs as a dedicated user with limited privileges

## Linux (systemd)

### Installation

```bash
# Install as service (requires root) - auto-starts immediately
sudo muti-metroo service install -c /etc/muti-metroo/config.yaml
```

This copies the binary to `/usr/local/bin/muti-metroo`, creates a systemd unit file at `/etc/systemd/system/muti-metroo.service`, enables boot startup, and starts the service immediately.

:::info Binary Deployment
By default, `service install` copies the binary to a standard system location (`/usr/local/bin` on Linux/macOS, `C:\Program Files\<name>` on Windows). The service definition references the installed copy. Use `--deploy=false` to skip this and reference the binary at its current path.
:::

### Service Management

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

:::tip No Manual Start Needed
The `service install` command automatically enables and starts the service. You don't need to run `systemctl enable` or `systemctl start` manually.
:::

### Uninstall

```bash
# Stop and disable
sudo systemctl stop muti-metroo
sudo systemctl disable muti-metroo

# Uninstall service
sudo muti-metroo service uninstall

# Clean up files (optional)
sudo rm -rf /etc/muti-metroo
sudo rm -rf /var/lib/muti-metroo
```

### Systemd Unit File

The installer creates:

```ini
# /etc/systemd/system/muti-metroo.service
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

:::note
The `ReadWritePaths` directive includes both the config directory and the data directory specified in your config file. This allows the service to write to the data directory while maintaining strict filesystem protection.
:::

### Custom Installation

For manual setup:

```bash
# Create user
sudo useradd -r -s /sbin/nologin muti-metroo

# Create directories
sudo mkdir -p /etc/muti-metroo
sudo mkdir -p /var/lib/muti-metroo
sudo chown muti-metroo:muti-metroo /var/lib/muti-metroo

# Copy binary
sudo cp muti-metroo /usr/local/bin/

# Copy config
sudo cp ./config.yaml /etc/muti-metroo/
sudo chown root:muti-metroo /etc/muti-metroo/config.yaml
sudo chmod 640 /etc/muti-metroo/config.yaml

# Copy certificates
sudo cp -r ./certs /etc/muti-metroo/
sudo chown -R root:muti-metroo /etc/muti-metroo/certs
sudo chmod 750 /etc/muti-metroo/certs
sudo chmod 640 /etc/muti-metroo/certs/*

# Create unit file
sudo nano /etc/systemd/system/muti-metroo.service
# (paste unit file content)

# Reload and start
sudo systemctl daemon-reload
sudo systemctl enable --now muti-metroo
```

### Configuration for Service

```yaml
# /etc/muti-metroo/config.yaml
agent:
  id: "auto"
  display_name: "${HOSTNAME}"
  data_dir: "/var/lib/muti-metroo"
  log_level: "info"
  log_format: "json"

listeners:
  - transport: quic
    address: "0.0.0.0:4433"
    tls:
      cert: "/etc/muti-metroo/certs/agent.crt"
      key: "/etc/muti-metroo/certs/agent.key"

socks5:
  enabled: true
  address: "127.0.0.1:1080"

http:
  enabled: true
  address: "127.0.0.1:8080"
```

## Windows Service

### Installation

Run as Administrator:

```powershell
# Install service
muti-metroo.exe service install -c C:\ProgramData\muti-metroo\config.yaml
```

:::tip Non-Admin Alternative
Windows Service installation requires Administrator privileges. If you don't have admin access, use [DLL Mode](/deployment/dll-mode) with the Registry Run key instead - it provides similar background execution and starts automatically at user login without requiring elevation.
:::

### Service Management

```powershell
# Start
sc start muti-metroo

# Stop
sc stop muti-metroo

# Status
sc query muti-metroo

# Configure automatic start
sc config muti-metroo start= auto
```

Or use Services GUI:
1. Open `services.msc`
2. Find "Muti Metroo"
3. Right-click for Start/Stop/Properties

### Uninstall

Run as Administrator:

```powershell
# Stop service
sc stop muti-metroo

# Uninstall
muti-metroo.exe service uninstall
```

### Windows Paths

```
C:\ProgramData\muti-metroo\
  config.yaml           # Configuration
  data\                 # Agent data
    agent_id            # Agent identity
  certs\                # Certificates
    agent.crt
    agent.key
    ca.crt
C:\Program Files\muti-metroo\
  muti-metroo.exe       # Binary
```

### Configuration for Windows

```yaml
# C:\ProgramData\muti-metroo\config.yaml
agent:
  id: "auto"
  data_dir: "C:\\ProgramData\\muti-metroo\\data"
  log_level: "info"
  log_format: "json"

listeners:
  - transport: quic
    address: "0.0.0.0:4433"
    tls:
      cert: "C:\\ProgramData\\muti-metroo\\certs\\agent.crt"
      key: "C:\\ProgramData\\muti-metroo\\certs\\agent.key"

socks5:
  enabled: true
  address: "127.0.0.1:1080"

http:
  enabled: true
  address: "127.0.0.1:8080"
```

## Windows User Service (Registry Run)

For users without Administrator access, install as a user service using the Registry Run key and the DLL.

### Requirements

- `muti-metroo.exe` - CLI for installation and management
- `muti-metroo.dll` - DLL for background execution via rundll32
- `config.yaml` - Configuration file

### Installation

```powershell
# Install as user service (no admin required)
muti-metroo service install --user --dll C:\path\to\muti-metroo.dll -c C:\path\to\config.yaml

# Install with custom service name
muti-metroo service install --user -n "My Tunnel" --dll C:\path\to\muti-metroo.dll -c C:\path\to\config.yaml
```

**Flags:**
- `--user`: Install as user service (required for Registry Run mode)
- `--dll <path>`: Path to muti-metroo.dll (required)
- `-c, --config <path>`: Path to config file (required)
- `-n, --name <name>`: Custom service name (default: muti-metroo). The name is converted to PascalCase for the Registry value (e.g., "My Tunnel" becomes "MyTunnel").

This creates a Registry Run entry at `HKCU\Software\Microsoft\Windows\CurrentVersion\Run` that:
- Starts immediately after installation (no reboot required)
- Runs automatically at each user logon
- Uses `rundll32.exe` to execute the DLL
- Runs with current user privileges
- No console window (background execution)

### Service Management

```powershell
# Check status (shows DLL and config paths)
muti-metroo service status

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

### Uninstall

```powershell
# Uninstall (no admin required)
muti-metroo service uninstall
```

## Windows: Quick Background Start (--detach)

If you only need the agent to keep running after closing a terminal or disconnecting an SSH session — without installing a service — use the `--detach` flag:

```cmd
muti-metroo.exe run --detach -c config.yaml
```

Muti Metroo re-spawns itself outside the session's Job Object, prints the PID, and exits immediately. No admin rights, no DLL, no Task Scheduler required. This works even on machines where those tools are restricted.

```cmd
# Verify it's running
tasklist | findstr muti-metroo

# Stop it
taskkill /im muti-metroo.exe /f
```

The trade-off: the agent does not survive a reboot. For persistence across reboots, use Windows Service or Registry Run.

### Comparison: Windows Service vs Registry Run vs --detach

| Feature | Windows Service | Registry Run | --detach |
|---------|----------------|--------------|----------|
| Requires admin | Yes | No | No |
| Requires DLL | No | Yes | No |
| Requires Task Scheduler | No | Yes (for start) | No |
| Auto-restart on crash | Yes | No | No |
| Survives reboot | Yes | Yes (at logon) | No |
| Start timing | At boot (before login) | At user logon | On demand |
| Console window | No | No | No |
| Runs as | SYSTEM/service account | Current user | Current user |
| Process name | `muti-metroo.exe` | `rundll32.exe` | `muti-metroo.exe` |

**Choose Windows Service when:**
- You have Administrator access
- Agent must start before any user logs in
- You need automatic crash recovery

**Choose Registry Run when:**
- You don't have Administrator access
- Persistence across reboots is required

**Choose --detach when:**
- You don't have Administrator access
- Task Scheduler or DLL are unavailable (e.g., locked-down machine)
- Reboots are rare and you can restart manually when needed

## macOS (launchd)

### Installation

```bash
# Install as service (requires root)
sudo muti-metroo service install -c /etc/muti-metroo/config.yaml
```

This creates a launchd plist at `/Library/LaunchDaemons/com.muti-metroo.plist`.

### Service Management

```bash
# Check status
muti-metroo service status

# Stop service
sudo launchctl stop com.muti-metroo

# Start service
sudo launchctl start com.muti-metroo

# View logs (in the config file's directory)
tail -f /etc/muti-metroo/muti-metroo.log
tail -f /etc/muti-metroo/muti-metroo.err.log
```

### Uninstall

```bash
# Uninstall service
sudo muti-metroo service uninstall

# Clean up files (optional)
sudo rm -rf /etc/muti-metroo
sudo rm -rf /var/lib/muti-metroo
```

### Launchd Plist

The installer creates:

```xml
<!-- /Library/LaunchDaemons/com.muti-metroo.plist -->
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

:::note
Log files are created in the config file's directory (WorkingDirectory). The `KeepAlive` with `SuccessfulExit=false` ensures the service restarts only if it exits with an error, not on clean shutdown.
:::

### Custom Installation

For manual setup:

```bash
# Copy binary
sudo cp muti-metroo /usr/local/bin/

# Create directories
sudo mkdir -p /etc/muti-metroo
sudo mkdir -p /var/lib/muti-metroo

# Copy config and certs
sudo cp ./config.yaml /etc/muti-metroo/
sudo cp -r ./certs /etc/muti-metroo/

# Create plist manually (use template above)
sudo nano /Library/LaunchDaemons/com.muti-metroo.plist

# Load and start
sudo launchctl load -w /Library/LaunchDaemons/com.muti-metroo.plist
```

### Configuration for macOS

```yaml
# /etc/muti-metroo/config.yaml
agent:
  id: "auto"
  display_name: "${HOSTNAME}"
  data_dir: "/var/lib/muti-metroo"
  log_level: "info"
  log_format: "json"

listeners:
  - transport: quic
    address: "0.0.0.0:4433"
    tls:
      cert: "/etc/muti-metroo/certs/agent.crt"
      key: "/etc/muti-metroo/certs/agent.key"

socks5:
  enabled: true
  address: "127.0.0.1:1080"

http:
  enabled: true
  address: "127.0.0.1:8080"
```

## Security Considerations

### File Permissions

```bash
# Linux
sudo chmod 700 /var/lib/muti-metroo
sudo chmod 640 /etc/muti-metroo/config.yaml
sudo chmod 600 /etc/muti-metroo/certs/*.key
sudo chmod 644 /etc/muti-metroo/certs/*.crt
```

### Network Ports

If using ports below 1024, either:

1. Run as root (not recommended)
2. Use capabilities:
   ```bash
   sudo setcap 'cap_net_bind_service=+ep' /usr/local/bin/muti-metroo
   ```
3. Use ports above 1024

### Firewall Rules

```bash
# Linux (firewalld)
sudo firewall-cmd --permanent --add-port=4433/udp
sudo firewall-cmd --permanent --add-port=1080/tcp
sudo firewall-cmd --permanent --add-port=8080/tcp
sudo firewall-cmd --reload

# Linux (ufw)
sudo ufw allow 4433/udp
sudo ufw allow 1080/tcp
sudo ufw allow 8080/tcp
```

## Troubleshooting

### Service Won't Start

```bash
# Check systemd logs
sudo journalctl -u muti-metroo -n 50

# Check service status
sudo systemctl status muti-metroo

# Test config manually
sudo -u muti-metroo /usr/local/bin/muti-metroo run -c /etc/muti-metroo/config.yaml
```

### Permission Denied

```bash
# Check file ownership
ls -la /etc/muti-metroo/
ls -la /var/lib/muti-metroo/

# Fix permissions
sudo chown -R muti-metroo:muti-metroo /var/lib/muti-metroo
```

### Port Already in Use

```bash
# Find what's using the port
sudo lsof -i :4433
sudo netstat -tlnp | grep 4433
```

## See Also

- [CLI - Service](/cli/service) - Service management commands
- [CLI - Run](/cli/run) - Run agent manually

## Next Steps

- [High Availability](/deployment/high-availability) - Redundancy setup
- [Troubleshooting](/troubleshooting/common-issues) - Common issues
