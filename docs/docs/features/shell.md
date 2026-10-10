---
title: Management Commands
sidebar_position: 5
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-drilling.png" alt="Mole running management commands" style={{maxWidth: '180px'}} />
</div>

# Management Commands

Run management commands on any agent in your mesh. Check system status, monitor resources, or edit configuration files - all through your encrypted tunnel.

```bash
# Run a quick command
muti-metroo shell abc123 whoami

# Monitor system resources interactively
muti-metroo shell --tty abc123 htop

# Edit a configuration file with vim
muti-metroo shell --tty abc123 vim /etc/muti-metroo/config.yaml
```

Two modes are available:

- **Normal mode**: Run commands and see output (default)
- **Interactive TTY**: Full terminal for vim, htop, top, and other interactive programs

:::tip Configuration
See [Management Commands Configuration](/configuration/shell) for all options including command whitelist and session limits. Authorization is handled mesh-wide by the management signing key -- see [Authentication](/security/authentication).
:::

## Modes

### Normal Mode (Default)

Standard execution without PTY allocation:

- Separate stdout and stderr streams
- Commands run until exit and return an exit code
- No terminal control characters
- Supports Unix-style stdin piping: bytes fed to the CLI's stdin are streamed to the remote command, and stdin EOF is propagated so commands that read a full input stream (cat, wc, sort, sha256sum, ...) terminate naturally

```bash
# Simple commands
muti-metroo shell abc123 whoami
muti-metroo shell abc123 ls -la /tmp

# Long-running streaming commands
muti-metroo shell abc123 journalctl -u muti-metroo -f
muti-metroo shell abc123 tail -f /var/log/syslog

# Pipe data through the mesh -- stdin EOF signals the remote to finish
cat largefile.txt | muti-metroo shell abc123 sha256sum
seq 1 1000 | muti-metroo shell abc123 -- wc -l
tar cz ./dir | muti-metroo shell abc123 -- tar xzf - -C /tmp/dest
```

:::tip Flags that collide with commands
Use `--` to stop `muti-metroo shell` from interpreting the remote command's flags. For example `muti-metroo shell abc123 -- wc -l`; without `--`, cobra would reject `-l` as an unknown muti-metroo flag.
:::

### Interactive Mode (--tty)

Allocates a PTY (pseudo-terminal) on the remote agent:

- Full terminal emulation
- Supports terminal resize (SIGWINCH)
- Works with interactive programs (vim, less, htop)
- Single combined stdout/stderr stream

```bash
muti-metroo shell --tty abc123 htop
muti-metroo shell --tty abc123 vim /etc/muti-metroo/config.yaml
muti-metroo shell --tty abc123 top
```

## CLI Usage

```bash
muti-metroo shell [flags] <agent-id> [command] [args...]

# Simple command (normal mode, default)
muti-metroo shell abc123 whoami

# Follow logs (normal mode)
muti-metroo shell abc123 journalctl -f

# Monitor resources interactively (requires --tty)
muti-metroo shell --tty abc123 htop

# Interactive vim (requires --tty)
muti-metroo shell --tty abc123 vim /etc/muti-metroo/config.yaml

# Via different agent
muti-metroo shell -a 192.168.1.10:8080 --tty abc123 top

# Pipe local data to a remote command
cat ./build.log | muti-metroo shell abc123 -- grep ERROR
```

### Flags

- `-a, --agent`: Agent HTTP API address — `host:port` or `http(s)://host[:port][/prefix]` (default: `localhost:8080`)
- `-t, --timeout`: Session timeout as duration string, e.g., `30s`, `5m` (default: 0 = no timeout)
- `--tty`: Interactive mode with PTY (for vim, htop, top, etc.)

## WebSocket API

Management command sessions use WebSocket for bidirectional communication.

**Endpoint**: `GET /agents/{agent-id}/shell?mode=tty|stream`

See [API - Management Commands](/api/shell) for protocol details.

## Platform Support

| Platform | Interactive (PTY) | Normal |
| -------- | ----------------- | ------ |
| Linux    | Yes               | Yes    |
| macOS    | Yes               | Yes    |
| Windows  | Yes (ConPTY)      | Yes    |

:::info Windows PTY
Windows agents use ConPTY (Windows Pseudo Console) for interactive sessions. ConPTY is available on Windows 10 version 1809 and later.
:::

### Available Shells

Each agent automatically detects installed shells at startup. When `shell.enabled` is `true`, the agent advertises both the detected shells and a `shell_enabled` flag to the mesh via node info. You can see which shells are available on any agent through the [Dashboard API](/api/dashboard).

When management commands are disabled, neither the shell list nor the `shell_enabled` flag appears in the topology -- making it clear that management commands are not available on that agent.

**Probed shells by platform:**

| Platform    | Shells probed (in preference order) |
| ----------- | ----------------------------------- |
| Linux/macOS | bash, sh, zsh, fish, ash, dash, ksh |
| Windows     | powershell.exe, pwsh.exe, cmd.exe   |

Shell detection is separate from the command whitelist:

- **Detection** reports which shells are installed on the system
- **Whitelist** controls which commands are allowed to execute

A shell appearing in the detected list does not mean it can be used -- it must also be in the agent's `whitelist` configuration.

### Windows Examples

Run system management commands on Windows agents:

```bash
# List running processes
muti-metroo shell abc123 tasklist

# Get system information
muti-metroo shell abc123 systeminfo

# View network connections
muti-metroo shell abc123 netstat -an
```

## Common Workflows

### System Health Check

```bash
# Quick system status
muti-metroo shell abc123 uptime
muti-metroo shell abc123 df -h
muti-metroo shell abc123 free -m

# Check running services
muti-metroo shell abc123 systemctl status muti-metroo
```

### Log Monitoring

```bash
# Follow service logs
muti-metroo shell abc123 journalctl -u muti-metroo -f

# Search recent logs
muti-metroo shell abc123 journalctl -u muti-metroo --since "1 hour ago"
```

### Configuration Management

```bash
# View config (streaming mode)
muti-metroo shell abc123 cat /etc/muti-metroo/config.yaml

# Edit config (interactive mode)
muti-metroo shell --tty abc123 vim /etc/muti-metroo/config.yaml

# Restart service after changes
muti-metroo shell abc123 systemctl restart muti-metroo
```

## Troubleshooting

### Command Not Allowed

```
Error: command not in whitelist: vim
```

**Cause:** The command is not in the agent's `whitelist` configuration.

**Solutions:**

- Use only whitelisted commands
- Contact the agent administrator to add the command
- Check available commands in the agent's config

### Authentication Failed

```
Error: management signature required
Error: management signing public key not configured on this agent
```

**Cause:** The gateway agent cannot sign management operations, or the target has no configured public key to verify against.

**Solution:** Ensure `management.signing_private_key` is set on the gateway you're connecting through (`-a`) and `management.signing_public_key` is set on the target agent. Generate the pair with `muti-metroo signing-key generate`. See [Authentication](/security/authentication).

### Session Limit Reached

```
Error: maximum sessions exceeded
```

**Cause:** The agent has reached its `max_sessions` limit.

**Solutions:**

- Wait for existing sessions to complete
- Close unused sessions
- Contact the agent administrator to increase the limit

### Interactive Program Not Working

```
# vim appears broken, no colors in htop
```

**Cause:** Running an interactive program without `--tty` flag.

**Solution:** Use `--tty` for interactive programs:

```bash
muti-metroo shell --tty abc123 htop
muti-metroo shell --tty abc123 vim /etc/config.yaml
```

### Agent Not Reachable

```
Error: no route to agent abc123
```

**Cause:** The target agent is not connected to the mesh.

**Solutions:**

```bash
# Check if agent is known
curl http://localhost:8080/agents

# Verify connectivity
curl http://localhost:8080/healthz | jq '.peer_count'
```

## Related

- [CLI - shell](/cli/shell) - CLI reference
- [API - Management Commands](/api/shell) - WebSocket API reference
- [Configuration - Management Commands](/configuration/shell) - Configuration reference
