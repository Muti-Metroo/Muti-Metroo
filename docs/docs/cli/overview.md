---
title: CLI Overview
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-reading.png" alt="Mole reading CLI docs" style={{maxWidth: '180px'}} />
</div>

# CLI Reference

Everything you need to set up agents, manage certificates, transfer files, and run commands on remote systems - all from the command line.

**Quick reference:**

| I want to...                         | Command                               |
| ------------------------------------ | ------------------------------------- |
| Set up a new agent interactively     | `muti-metroo setup`                         |
| Start an agent                       | `muti-metroo run -c config.yaml`            |
| Create TLS certificates              | `muti-metroo cert ca` / `muti-metroo cert agent`  |
| Generate a password hash             | `muti-metroo hash`                          |
| Run a command on a remote agent      | `muti-metroo shell <agent-id> <command>`    |
| Transfer files                       | `muti-metroo upload` / `muti-metroo download`     |
| Ping a host through the mesh         | `muti-metroo ping <agent-id> <destination>` |
| Test if a listener is reachable      | `muti-metroo probe <address>`               |
| Test connectivity to all mesh agents | `muti-metroo mesh-test`                     |
| Install as a system service          | `muti-metroo service install`               |

## HTTP API

All CLI commands that query agent state use the HTTP API to communicate with agents.

| Aspect                | Details                       |
| --------------------- | ----------------------------- |
| **Local queries**     | `status`, `peers`, `routes`   |
| **Remote operations** | `shell`, `upload`, `download` |
| **Default address**   | `localhost:8080`              |
| **Configuration**     | `http.address` in config      |

The `-a` / `--agent` flag accepts either a bare `host:port` (HTTP) or a full URL. Using `https://` is required when the agent is fronted by a TLS reverse proxy, and the shell and ping WebSocket transports are upgraded to `wss` to match. A path prefix is respected, so a proxy that mounts the agent under a subpath is also supported.

```bash
# Query local agent (default: localhost:8080)
muti-metroo status
muti-metroo peers
muti-metroo routes

# Query different agent
muti-metroo status -a 192.168.1.10:8080
muti-metroo peers -a 192.168.1.10:8080

# Agent behind an HTTPS reverse proxy
muti-metroo status -a https://muti-metroo.example.com
muti-metroo shell  -a https://muti-metroo.example.com/muti-metroo <target-agent-id> whoami

# Execute command on remote agent
muti-metroo shell <target-agent-id> whoami
muti-metroo shell --tty <target-agent-id> bash

# Transfer files
muti-metroo upload <target-agent-id> ./file.txt /tmp/file.txt
muti-metroo download <target-agent-id> /tmp/file.txt ./file.txt
```

## Global Flags

Available for all commands:

- `-h, --help`: Show help for command
- `--version`: Show version information
- `--token`: API bearer token for authentication (or set `MUTI_METROO_TOKEN` env var)
- `-y, --yes`: Skip interactive confirmation prompts on destructive operations (sleep, wake, route remove, peer remove, forward remove, host remove, default-route add, etc.). Required when stdin is not a TTY (cron, CI) - the command will otherwise refuse with exit code 6.
- `--no-color`: Disable ANSI colors in output. Also honored: the `NO_COLOR` env var (per [no-color.org](https://no-color.org/)). Color is auto-disabled when stdout is not a terminal, so log capture is already clean by default.

JSON output (`--json`), HTTP API responses, log lines, and wire-protocol identifiers are always English so scripts and log scrapers stay stable.

## Flag Changes

The `-t` short flag historically meant timeout on action commands (mesh-test, probe, shell, upload, download, ping) and target on management commands (route/peer/forward/host add/remove, etc.). To resolve the conflict:

- **`-t` continues to mean `--target`** on all 16 management commands (no change).
- **`-t` is deprecated for `--timeout`** on the 6 action commands. Use **`-w/--timeout`** instead. The old `-t` form still works for one release but prints a deprecation warning to stderr; in a future major release, `-t` on those commands will mean `--target` to match the rest of the CLI.

Migrate scripts: `muti-metroo upload <id> file.txt /tmp -t 5m` → `muti-metroo upload <id> file.txt /tmp -w 5m`.

## Confirmation Prompts

For critical-systems safety, mesh-wide and irreversible operations require an explicit confirmation:

| Command                                                 | Prompt                                                                                                    |
| ------------------------------------------------------- | --------------------------------------------------------------------------------------------------------- |
| `sleep` / `wake`                                        | Mesh-wide flood; reachable-agent count is shown before y/N                                                |
| `route remove <cidr>`                                   | Shows next-hop / metric / origin before y/N                                                               |
| `route add 0.0.0.0/0` (or `::/0`)                       | Typed confirmation: requires the literal string `add default route`. Bypass with `--allow-default-route`. |
| `peer remove <addr>`                                    | Shows peer state and how many routes flow through it                                                      |
| `forward remove` / `forward endpoint remove`            | Shows the entry being removed                                                                             |
| `listener remove <addr>`                                | Confirms before tearing down the transport listener                                                       |
| `host remove` / `host suffix remove` (last suffix only) | Confirms before invalidating mesh hostnames                                                               |
| `display-name set ""`                                   | Confirms revert to config value                                                                           |
| `stop`                                                  | Confirms before stopping the agent (a service manager may restart it)                                     |

In every case `--yes` / `-y` skips the prompt. On non-TTY stdin (cron, CI) the prompt refuses with exit code 6 unless `--yes` is passed.

## Exit Codes

| Code | Meaning                                                                       |
| ---- | ----------------------------------------------------------------------------- |
| 0    | Success                                                                       |
| 1    | Generic error                                                                 |
| 2    | Usage error (bad flags or arguments)                                          |
| 3    | Agent unreachable (network or HTTP failure)                                   |
| 4    | Authentication failed (bearer token missing or rejected)                      |
| 5    | Operation rejected by the agent (validation failure, signature missing, etc.) |
| 6    | Confirmation required (non-TTY stdin without `--yes`)                         |

## Commands

| Command           | Description                                                                                 |
| ----------------- | ------------------------------------------------------------------------------------------- |
| `run`             | Run agent with configuration file                                                           |
| `init`            | Initialize agent identity                                                                   |
| `setup`           | Interactive setup wizard                                                                    |
| `embed-config`    | Embed a YAML config into a Muti Metroo binary non-interactively (CI / scripted)                   |
| `cert`            | Certificate management (CA, agent, client)                                                  |
| `hash`            | Generate bcrypt password hash                                                               |
| `status`          | Show agent status via HTTP API                                                              |
| `peer status`     | Show currently-connected peers (live transport state). Replaces `peers` (deprecated).       |
| `route status`    | Show the live route table (operational view). Replaces `routes` (deprecated).               |
| `route`           | Dynamic route management (add, remove, list, trace)                                         |
| `forward`         | Dynamic forward listener management (add, remove, list)                                     |
| `peer`            | Dynamic peer connection management (add, remove, list)                                      |
| `listener`        | Dynamic transport listener management (add, remove, list)                                   |
| `ping`            | Send ICMP echo requests through the mesh                                                    |
| `probe`           | Test connectivity to a listener (standalone)                                                |
| `probe listen`    | Start a test listener for connectivity probing                                              |
| `mesh-test`       | Test connectivity to all mesh agents                                                        |
| `shell`           | Run management commands (interactive or streaming)                                          |
| `upload`          | Upload file to remote agent                                                                 |
| `download`        | Download file from remote agent                                                             |
| `sleep`           | Trigger mesh-wide sleep                                                                     |
| `wake`            | Trigger mesh-wide wake                                                                      |
| `sleep-status`    | Check sleep mode status                                                                     |
| `stop`            | Gracefully stop an agent (local or remote via `-t`)                                         |
| `service`         | Service management (install, uninstall, status)                                             |
| `management-key`  | Generate and manage mesh topology encryption keys                                           |
| `signing-key`     | Generate and manage Ed25519 signing keys for sleep/wake authentication                      |
| `display-name`    | Set or get agent display name dynamically                                                   |
| `config validate` | Parse and validate a config file (or binary with embedded config) without starting an agent |
| `config show`     | Print the effective configuration with secrets redacted; supports `--diff-from-default`     |

## Quick Examples

```bash
# Start agent
muti-metroo run -c config.yaml

# Interactive setup
muti-metroo setup

# Generate CA
muti-metroo cert ca --cn "My CA"

# Generate password hash for config
muti-metroo hash --cost 12

# Check agent status
muti-metroo status

# Check agent on different port
muti-metroo status -a localhost:9090

# Test connectivity to a listener (no running agent needed)
muti-metroo probe server.example.com:4433
muti-metroo probe --transport h2 server.example.com:443

# Start a test listener (ephemeral certs, no config needed)
muti-metroo probe listen -T quic -a 0.0.0.0:4433

# List connected peers
muti-metroo peers

# List route table
muti-metroo routes

# Test connectivity to all mesh agents
muti-metroo mesh-test
muti-metroo mesh-test --json

# Ping a host through the mesh (via exit agent)
muti-metroo ping agent123 10.0.0.1
muti-metroo ping -c 5 agent123 192.168.1.1

# Execute remote command
muti-metroo shell agent123 whoami
muti-metroo shell --tty agent123 bash

# Upload file
muti-metroo upload agent123 local.txt /tmp/remote.txt

# Set display name
muti-metroo display-name set "My Gateway"
muti-metroo display-name get
```
