---
title: shell
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-drilling.png" alt="Mole running management commands" style={{maxWidth: '180px'}} />
</div>

# muti-metroo shell

Execute management commands on remote agents through the mesh. Run one-off commands or open a full interactive terminal session.

**Quick examples:**

```bash
# Run a command
muti-metroo shell abc123 whoami

# Monitor system resources interactively
muti-metroo shell --tty abc123 htop

# Follow logs in real-time
muti-metroo shell abc123 journalctl -f
```

## Usage

```bash
muti-metroo shell [flags] <target-agent-id> [command] [args...]
```

## Flags

- `-a, --agent <addr>`: Agent HTTP API address — `host:port` or `http(s)://host[:port][/prefix]` (default: `localhost:8080`)
- `-t, --timeout <duration>`: Session timeout as duration string, e.g., `30s`, `5m` (default: 0 = no timeout)
- `--tty`: Interactive mode with PTY (for vim, htop, top, etc.)

:::info Authorization
Management commands are gated by the mesh management signing key. The gateway agent (reached via `-a`) must have `management.signing_private_key` set and the target must have `management.signing_public_key`. There is no per-command password. See [Authentication](/security/authentication).
:::

:::tip

- **Default command**: If no command is specified, defaults to `bash`
- **Agent ID prefix**: You can use a short agent ID prefix (e.g., `abc123`) instead of the full 32-character ID. The prefix is automatically resolved.
  :::

## Modes

### Normal Mode (Default)

Runs commands without PTY allocation. Commands execute and return an exit code. Stdout and stderr stream back as separate streams; local stdin is piped to the remote command and EOF propagates, so Unix-style pipelines work end-to-end:

```bash
# Simple commands
muti-metroo shell abc123 whoami
muti-metroo shell abc123 hostname
muti-metroo shell abc123 ls -la /tmp

# Follow logs
muti-metroo shell abc123 journalctl -u muti-metroo -f

# Tail a file
muti-metroo shell abc123 tail -f /var/log/syslog

# Pipe local data into a remote command (stdin EOF is propagated, so
# EOF-sensitive commands like wc and sha256sum terminate naturally)
cat build.log | muti-metroo shell abc123 -- grep ERROR
seq 1 1000 | muti-metroo shell abc123 -- wc -l
head -c 1M /dev/urandom | muti-metroo shell abc123 sha256sum
```

:::tip Flags that collide with commands
Insert `--` before the remote command to stop cobra from interpreting short flags as muti-metroo flags. Without it, `muti-metroo shell abc123 wc -l` would complain about "unknown shorthand flag: 'l'".
:::

### Interactive Mode (--tty)

Use `--tty` for programs that require a terminal (vim, htop, top):

```bash
# Run htop for resource monitoring
muti-metroo shell --tty abc123 htop

# Run top for process monitoring
muti-metroo shell --tty abc123 top

# Run vim to edit configuration
muti-metroo shell --tty abc123 vim /etc/muti-metroo/config.yaml
```

#### Windows Examples

```bash
# List running processes
muti-metroo shell abc123 tasklist

# Get system information
muti-metroo shell abc123 systeminfo

# View network connections
muti-metroo shell abc123 netstat -an
```

## Examples

```bash
# Simple command (normal mode)
muti-metroo shell abc123 whoami

# Follow logs (normal mode)
muti-metroo shell abc123 journalctl -u nginx -f

# Monitor resources interactively (requires --tty)
muti-metroo shell --tty abc123 htop

# Via different agent
muti-metroo shell -a 192.168.1.10:8080 --tty abc123 top

# With session timeout (1 hour)
muti-metroo shell -t 3600 --tty abc123 htop

# Piped pipeline: compute a checksum of a remote file on a remote agent
muti-metroo shell abc123 cat /var/log/syslog | sha256sum

# Piped pipeline the other direction: send local data to a remote command
tar cz ./src | muti-metroo shell abc123 -- tar xzf - -C /tmp/deploy
```

## Terminal Features

In interactive mode:

- Window resize is automatically forwarded (SIGWINCH)
- Ctrl+C sends SIGINT to remote process
- Full terminal emulation (colors, cursor movement)

## Exit Codes

The command exits with:

- The remote command's exit code on success
- 1 on connection or protocol errors

## See Also

- [Management Commands](/features/shell) - Detailed management commands documentation
- [Configuration](/configuration/overview) - Management command configuration
