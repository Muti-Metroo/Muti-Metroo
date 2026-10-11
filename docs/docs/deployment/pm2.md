---
title: PM2 Process Manager
description: Supervise Muti Metroo with PM2 for automatic restarts, log files, and startup at boot, including on hosts without root access.
sidebar_position: 3
---

# PM2 Process Manager

[PM2](https://pm2.keymetrics.io/) is a Node.js process manager that can supervise any executable. It restarts the agent when it exits, captures its output to log files, and can restore the process list at boot. PM2 runs as an ordinary user, which makes it an option on hosts where you cannot install a system service, such as shared hosting accounts.

If you have root access and no other use for PM2, a [system service](/deployment/system-service) needs no extra software.

## Quick start

```bash
npm install -g pm2

pm2 start /usr/local/bin/muti-metroo --name muti-metroo --interpreter none \
  --kill-timeout 12000 -- run -c /etc/muti-metroo/config.yaml

pm2 save
```

- `--interpreter none` runs the binary directly instead of through Node.js.
- `--kill-timeout 12000` gives the agent time to shut down. On `pm2 stop` and `pm2 restart`, PM2 sends `SIGINT` and then `SIGKILL` after the timeout (1.6 seconds by default). The agent drains streams for up to 10 seconds after `SIGINT`.
- Arguments after `--` are passed to `muti-metroo`.

## Ecosystem file

An ecosystem file keeps the settings in one place:

```javascript
// /etc/muti-metroo/ecosystem.config.js
module.exports = {
  apps: [{
    name: 'muti-metroo',
    script: '/usr/local/bin/muti-metroo',
    args: 'run -c /etc/muti-metroo/config.yaml',
    cwd: '/etc/muti-metroo',
    interpreter: 'none',

    autorestart: true,
    min_uptime: '10s',
    max_restarts: 10,
    restart_delay: 5000,
    kill_timeout: 12000,

    out_file: '/var/log/muti-metroo/out.log',
    error_file: '/var/log/muti-metroo/error.log',
    log_date_format: 'YYYY-MM-DD HH:mm:ss Z'
  }]
};
```

```bash
sudo mkdir -p /var/log/muti-metroo
sudo chown "$USER" /var/log/muti-metroo
pm2 start /etc/muti-metroo/ecosystem.config.js
pm2 save
```

The agent writes its log lines to stderr, so they appear in `error_file`. Startup messages such as the agent ID go to `out_file`.

`cwd` sets the working directory; relative paths in the config, such as `data_dir: "./data"`, resolve against it.

### Several agents on one host

Give each agent its own config file, data directory, and ports:

```javascript
module.exports = {
  apps: [
    {
      name: 'mm-ingress',
      script: '/usr/local/bin/muti-metroo',
      args: 'run -c /etc/muti-metroo/ingress.yaml',
      interpreter: 'none',
      kill_timeout: 12000
    },
    {
      name: 'mm-exit',
      script: '/usr/local/bin/muti-metroo',
      args: 'run -c /etc/muti-metroo/exit.yaml',
      interpreter: 'none',
      kill_timeout: 12000
    }
  ]
};
```

## Start at boot

```bash
pm2 startup
```

`pm2 startup` prints a command that registers PM2 itself with the init system. Run that command, then save the current process list so PM2 restores it at boot:

```bash
pm2 save
```

On hosts where you cannot run the printed command (it needs root), add a crontab entry instead:

```bash
(crontab -l 2>/dev/null; echo "@reboot $(command -v pm2) resurrect") | crontab -
```

On Windows, `pm2 startup` is not supported. Use [pm2-installer](https://github.com/jessety/pm2-installer) to run PM2 as a Windows Service, or use the agent's own [Windows Service or Registry Run installation](/deployment/system-service#windows-service) instead.

## Day-to-day commands

```bash
pm2 status
pm2 logs muti-metroo --lines 100
pm2 restart muti-metroo
pm2 stop muti-metroo
pm2 delete muti-metroo
```

PM2 does not rotate its log files. Install the `pm2-logrotate` module to do so:

```bash
pm2 install pm2-logrotate
pm2 set pm2-logrotate:max_size 10M
pm2 set pm2-logrotate:retain 7
```

### Upgrade the binary

```bash
pm2 stop muti-metroo
cp ./muti-metroo-linux-amd64 /usr/local/bin/muti-metroo
chmod +x /usr/local/bin/muti-metroo
pm2 start muti-metroo
```

## Running without root

PM2 and the agent both run as an unprivileged user, so the whole setup can live in a home directory. Keep the binary, config, and data directory outside any directory a web server serves; the config file and data directory hold private keys.

```text
~/muti-metroo/
  muti-metroo        binary
  config.yaml        configuration
  data/              agent identity and persisted runtime changes
  ecosystem.config.js
```

```javascript
// ~/muti-metroo/ecosystem.config.js
module.exports = {
  apps: [{
    name: 'muti-metroo',
    script: '/home/alice/muti-metroo/muti-metroo',
    args: 'run -c /home/alice/muti-metroo/config.yaml',
    cwd: '/home/alice/muti-metroo',
    interpreter: 'none',
    autorestart: true,
    restart_delay: 5000,
    kill_timeout: 12000
  }]
};
```

Without root, listeners must use ports above 1023. To accept peers on a web host's existing HTTPS port, run a plaintext WebSocket listener on loopback and forward to it from the web server, as described in [Apache without server access](/deployment/reverse-proxy#apache-without-server-access-htaccess).

## See Also

- [System Service](/deployment/system-service): systemd, launchd, and Windows Service installation
- [Reverse Proxy](/deployment/reverse-proxy): accept peers through Nginx, Caddy, or Apache
- [run CLI](/cli/run): options for `muti-metroo run`
- [Common Issues](/troubleshooting/common-issues): startup errors and their fixes
