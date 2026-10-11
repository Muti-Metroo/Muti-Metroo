---
title: muti-metroo shell
sidebar_label: shell
sidebar_position: 22
description: Run a command on a remote agent through the mesh, either streaming its output or in an interactive terminal session.
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-drilling.png" alt="Mole running management commands" style={{maxWidth: '180px'}} />
</div>

# muti-metroo shell

Runs a command on a remote agent through the mesh. By default the command runs without a terminal and its output streams back; with `--tty` it runs in a pseudo-terminal for interactive programs such as `bash`, `vim`, or `htop`.

Requirements:

- The gateway agent (`--agent`) has the HTTP API with remote endpoints enabled (`http.remote_api`) and holds the mesh signing private key (`management.signing_private_key`).
- The target agent has `management.signing_public_key` set, management commands enabled (`shell.enabled: true`), and the command allowed by `shell.whitelist`.

See [Shell configuration](../configuration/shell.md) and [Authentication](../security/authentication.md).

Global flags (`--token`, `--yes`, `--no-color`) are described in [CLI overview](overview.md#global-flags).

## Synopsis

```bash
muti-metroo shell [flags] <target-agent-id> [command] [args...]
```

## Arguments

| Argument | Description |
| --- | --- |
| `<target-agent-id>` | The agent that runs the command. A full 32-character agent ID, or a unique prefix of one, resolved against the gateway agent's list of known agents. |
| `[command] [args...]` | Command and arguments to run. Defaults to `bash` when omitted. |

The CLI parses flags anywhere on the command line, so a remote argument that looks like a flag (`-l`, `-la`) is rejected with `unknown shorthand flag`. Put `--` before the remote command to stop flag parsing:

```bash
muti-metroo shell abc123de -- ls -la /tmp
```

## Flags

| Flag | Default | Description |
| --- | --- | --- |
| `-a`, `--agent` | `localhost:8080` | Gateway agent HTTP API address, as `host:port` or a URL. See [Agent address](overview.md#agent-address). |
| `-w`, `--timeout` | `0` | Session timeout as a Go duration (`30s`, `5m`) or plain seconds. `0` means no timeout. When it expires, the remote command is terminated. |
| `--tty` | `false` | Run the command in a pseudo-terminal (interactive mode). |

`-t` is a deprecated alias for `--timeout`; it still works but prints a warning to stderr. See [Deprecated commands and flags](overview.md#deprecated-commands-and-flags).

## Modes

### Streaming mode (default)

No terminal is allocated. The remote command's stdout and stderr are written to the local stdout and stderr separately. Local stdin is forwarded to the remote command, and end-of-file is forwarded too, so the command works in pipelines. Pressing Ctrl+C ends the session.

```bash
# One-off commands
muti-metroo shell abc123de whoami
muti-metroo shell abc123de -- ls -la /tmp

# Continuous output
muti-metroo shell abc123de tail -f /var/log/syslog
muti-metroo shell abc123de journalctl -u muti-metroo -f

# Send local data to a remote command
seq 1 1000 | muti-metroo shell abc123de -- wc -l
tar cz ./src | muti-metroo shell abc123de -- tar xzf - -C /tmp/deploy

# Process remote output locally
muti-metroo shell abc123de cat /var/log/syslog | grep ERROR
```

```text
$ seq 1 1000 | muti-metroo shell abc123de -- wc -l
    1000
```

### Interactive mode (`--tty`)

A pseudo-terminal is allocated on the target agent with the size and `TERM` of your local terminal (`xterm-256color` when `TERM` is unset). The local terminal is switched to raw mode, so keys such as Ctrl+C go to the remote program, and window size changes are forwarded. Before the session starts the CLI prints `Connected to <name> (<short-id>)` to stderr, and `Connection to <name> closed.` when it ends.

```bash
muti-metroo shell --tty abc123de bash
muti-metroo shell --tty abc123de htop
muti-metroo shell --tty abc123de vim /etc/muti-metroo/config.yaml
```

On Windows targets, use the native commands that the whitelist allows:

```bash
muti-metroo shell abc123de tasklist
muti-metroo shell abc123de -- netstat -an
muti-metroo shell --tty abc123de powershell
```

## Examples

```bash
# Through a different gateway agent
muti-metroo shell -a 192.168.1.10:8080 abc123de uptime

# Interactive session that ends after one hour
muti-metroo shell -w 1h --tty abc123de bash

# Use the remote exit status in a script
if muti-metroo shell abc123de -- test -f /etc/app/ready; then echo "ready"; fi
```

## Exit status

When the remote command runs, `shell` exits with the remote command's exit status, so scripts can test it directly. A session terminated by `--timeout` exits 255.

When the session cannot start, the command prints an error and exits 1. Typical messages are `remote error: ...` (for example, the command is not whitelisted, management commands are disabled, or there is no route to the agent), `failed to connect: ...` (the gateway is unreachable or rejected the bearer token), and `timeout waiting for shell session to start`. A flag parsing error exits 2.

Because the remote exit status is passed through, a remote command that exits with 2 to 6 is indistinguishable from the CLI's own [exit codes](overview.md#exit-codes).

## See Also

- [Management commands](../features/shell.md): how remote command execution works and when to use it
- [Shell configuration](../configuration/shell.md): enable management commands and set the whitelist
- [Authentication](../security/authentication.md): the signing keys that authorize remote commands
- [Shell HTTP API](../api/shell.md): the WebSocket endpoint this command uses
- [upload and download](file-transfer.md): copy files to and from a remote agent
