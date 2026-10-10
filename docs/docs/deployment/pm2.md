---
title: PM2 Process Manager
sidebar_position: 5
---

# PM2 Process Manager

Run Muti Metroo with [PM2](https://pm2.keymetrics.io/), a production process manager that provides automatic restarts, log management, and cluster mode support.

**When to use PM2:**
- You already use PM2 for other applications
- You want unified process management across Node.js and non-Node.js apps
- You need built-in log rotation and monitoring
- Cross-platform support (Linux, macOS, Windows)

## Quick Start

```bash
# Install PM2 globally
npm install -g pm2

# Start Muti Metroo with PM2
pm2 start muti-metroo -- run -c /etc/muti-metroo/config.yaml

# Save process list for automatic restart on reboot
pm2 save

# Setup PM2 to start on boot
pm2 startup
```

## Ecosystem File

For production deployments, use an ecosystem file for reproducible configuration.

### Basic Configuration

```javascript
// ecosystem.config.js
module.exports = {
  apps: [{
    name: 'muti-metroo',
    script: '/usr/local/bin/muti-metroo',
    args: 'run -c /etc/muti-metroo/config.yaml',
    cwd: '/etc/muti-metroo',

    // Restart policy
    autorestart: true,
    max_restarts: 10,
    min_uptime: '10s',
    restart_delay: 5000,

    // Logging
    error_file: '/var/log/muti-metroo/error.log',
    out_file: '/var/log/muti-metroo/out.log',
    merge_logs: true,
    log_date_format: 'YYYY-MM-DD HH:mm:ss Z',

    // Environment
    env: {
      NODE_ENV: 'production'
    }
  }]
};
```

### Advanced Configuration

```javascript
// ecosystem.config.js
module.exports = {
  apps: [{
    name: 'muti-metroo',
    script: '/usr/local/bin/muti-metroo',
    args: 'run -c /etc/muti-metroo/config.yaml',
    cwd: '/etc/muti-metroo',

    // Process settings
    instances: 1,
    exec_mode: 'fork',
    autorestart: true,
    watch: false,

    // Restart policy
    max_restarts: 10,
    min_uptime: '10s',
    restart_delay: 5000,
    exp_backoff_restart_delay: 100,

    // Resource limits
    max_memory_restart: '500M',

    // Logging
    error_file: '/var/log/muti-metroo/error.log',
    out_file: '/var/log/muti-metroo/out.log',
    merge_logs: true,
    log_date_format: 'YYYY-MM-DD HH:mm:ss Z',

    // Log rotation (requires pm2-logrotate)
    // pm2 install pm2-logrotate
    // pm2 set pm2-logrotate:max_size 10M
    // pm2 set pm2-logrotate:retain 7

    // Graceful shutdown
    kill_timeout: 10000,
    listen_timeout: 5000,
    shutdown_with_message: false,

    // Environment
    env: {
      NODE_ENV: 'production'
    },
    env_development: {
      NODE_ENV: 'development'
    }
  }]
};
```

### Multiple Agents

Run multiple agents on the same host:

```javascript
// ecosystem.config.js
module.exports = {
  apps: [
    {
      name: 'muti-metroo-ingress',
      script: '/usr/local/bin/muti-metroo',
      args: 'run -c /etc/muti-metroo/ingress.yaml',
      cwd: '/etc/muti-metroo',
      autorestart: true,
      error_file: '/var/log/muti-metroo/ingress-error.log',
      out_file: '/var/log/muti-metroo/ingress-out.log',
      merge_logs: true
    },
    {
      name: 'muti-metroo-exit',
      script: '/usr/local/bin/muti-metroo',
      args: 'run -c /etc/muti-metroo/exit.yaml',
      cwd: '/etc/muti-metroo',
      autorestart: true,
      error_file: '/var/log/muti-metroo/exit-error.log',
      out_file: '/var/log/muti-metroo/exit-out.log',
      merge_logs: true
    }
  ]
};
```

## Installation

### Setup Directories

```bash
# Create directories
sudo mkdir -p /etc/muti-metroo
sudo mkdir -p /var/log/muti-metroo

# Copy binary
sudo cp muti-metroo /usr/local/bin/

# Copy configuration
sudo cp config.yaml /etc/muti-metroo/
sudo cp -r certs /etc/muti-metroo/

# Copy ecosystem file
sudo cp ecosystem.config.js /etc/muti-metroo/
```

### Start with PM2

```bash
# Start from ecosystem file
pm2 start /etc/muti-metroo/ecosystem.config.js

# Or start directly
pm2 start muti-metroo --name muti-metroo -- run -c /etc/muti-metroo/config.yaml

# Save process list
pm2 save
```

### Enable Startup

```bash
# Generate startup script (run the command it outputs)
pm2 startup

# Example output:
# sudo env PATH=$PATH:/usr/bin pm2 startup systemd -u youruser --hp /home/youruser

# After running the startup command, save the process list
pm2 save
```

## Management Commands

### Process Control

```bash
# Status
pm2 status
pm2 list

# Detailed info
pm2 show muti-metroo
pm2 describe muti-metroo

# Start/Stop/Restart
pm2 start muti-metroo
pm2 stop muti-metroo
pm2 restart muti-metroo

# Reload (graceful restart)
pm2 reload muti-metroo

# Delete from PM2
pm2 delete muti-metroo
```

### Logs

```bash
# Stream all logs
pm2 logs

# Stream specific app logs
pm2 logs muti-metroo

# Show last 100 lines
pm2 logs muti-metroo --lines 100

# Clear logs
pm2 flush muti-metroo
```

### Monitoring

```bash
# Real-time dashboard
pm2 monit

# Process metrics
pm2 show muti-metroo
```

## Log Rotation

Install the PM2 log rotation module:

```bash
# Install module
pm2 install pm2-logrotate

# Configure rotation
pm2 set pm2-logrotate:max_size 10M
pm2 set pm2-logrotate:retain 7
pm2 set pm2-logrotate:compress true
pm2 set pm2-logrotate:rotateInterval '0 0 * * *'
```

## Windows

PM2 works on Windows but requires additional setup for startup persistence.

### Basic Usage

```powershell
# Install PM2
npm install -g pm2

# Start Muti Metroo
pm2 start C:\ProgramData\muti-metroo\muti-metroo.exe -- run -c C:\ProgramData\muti-metroo\config.yaml

# Save process list
pm2 save
```

### Windows Service (Recommended)

The `pm2 startup` command does not work natively on Windows. For production deployments, use [pm2-installer](https://github.com/jessety/pm2-installer) to run PM2 as a proper Windows Service:

```powershell
# Clone the installer
git clone https://github.com/jessety/pm2-installer
cd pm2-installer

# Run PowerShell installer (as Administrator)
powershell -ExecutionPolicy Bypass -File install.ps1
```

This creates a Windows Service that:
- Starts automatically on boot without requiring user login
- Runs as Local Service user
- Persists across reboots and user logouts

After installation, manage your app normally:

```powershell
pm2 start C:\ProgramData\muti-metroo\muti-metroo.exe -- run -c C:\ProgramData\muti-metroo\config.yaml
pm2 save
```

### Windows Ecosystem File

```javascript
// ecosystem.config.js
module.exports = {
  apps: [{
    name: 'muti-metroo',
    script: 'C:\\ProgramData\\muti-metroo\\muti-metroo.exe',
    args: 'run -c C:\\ProgramData\\muti-metroo\\config.yaml',
    cwd: 'C:\\ProgramData\\muti-metroo',
    autorestart: true,
    error_file: 'C:\\ProgramData\\muti-metroo\\logs\\error.log',
    out_file: 'C:\\ProgramData\\muti-metroo\\logs\\out.log',
    merge_logs: true
  }]
};
```

## Comparison with System Services

| Feature | PM2 | systemd | Windows Service |
|---------|-----|---------|-----------------|
| Cross-platform | Yes | Linux only | Windows only |
| Log rotation | Via pm2-logrotate module | journald | Event Log |
| Monitoring UI | pm2 monit | journalctl | Event Viewer |
| Cluster mode | Yes | Manual | Manual |
| Memory limits | Yes | Yes | Limited |
| Root required | No | Yes (for install) | Yes (for install) |
| Node.js required | Yes | No | No |

**Choose PM2 when:**
- Managing multiple applications with a single tool
- Need cross-platform consistency
- Want built-in monitoring dashboard
- Already using PM2 for Node.js apps

**Choose system services when:**
- Minimal dependencies preferred
- Native OS integration important
- Security hardening required (systemd sandboxing)

## Troubleshooting

### Process Not Starting

```bash
# Check PM2 logs
pm2 logs muti-metroo --lines 50

# Check if binary is executable
ls -la /usr/local/bin/muti-metroo

# Test manually
/usr/local/bin/muti-metroo run -c /etc/muti-metroo/config.yaml
```

### Startup Script Not Working

```bash
# Regenerate startup script
pm2 unstartup
pm2 startup

# Verify saved processes
pm2 save
cat ~/.pm2/dump.pm2
```

### High Memory Usage

```bash
# Check memory
pm2 show muti-metroo | grep memory

# Set memory limit in ecosystem file
# max_memory_restart: '500M'

# Or restart with limit
pm2 start muti-metroo --max-memory-restart 500M
```

## Next Steps

- [System Service](/deployment/system-service) - Native OS service installation
- [Docker](/deployment/docker) - Container deployment
- [High Availability](/deployment/high-availability) - Redundancy setup
