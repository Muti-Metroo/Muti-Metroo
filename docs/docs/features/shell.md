---
title: Management Commands
description: Run whitelisted commands on remote agents with muti-metroo shell, in streaming mode or with an interactive terminal.
sidebar_position: 9
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-drilling.png" alt="Mole running management commands" style={{maxWidth: '180px'}} />
</div>

# Management Commands

`muti-metroo shell` runs a command on a remote agent's host and connects its input and output to your terminal. Use it to check a host's state, follow logs, or open an interactive program on an agent you can only reach through the mesh. The session is end-to-end encrypted between your gateway agent and the target agent.

```bash
muti-metroo shell 1f743225 uptime
muti-metroo shell 1f743225 -- journalctl -u muti-metroo -f
muti-metroo shell --tty 1f743225 htop
```

## Requirements

Management commands are disabled by default. They need settings on both agents:

| Agent | Setting |
| ----- | ------- |
| Target agent | `shell.enabled: true` |
| Target agent | `shell.whitelist` listing the commands that may run |
| Target agent | `management.signing_public_key` |
| Gateway agent (the one `-a` points at, default `localhost:8080`) | `management.signing_private_key` |

```yaml
# Target agent
shell:
  enabled: true
  whitelist:
    - uptime
    - df
    - journalctl
    - htop
management:
  signing_public_key: "86d6714fc48cd41afd5051e6ad9523ad889c806001e92bc739913a18c6709a83"
```

The gateway agent signs each session request and the target verifies the signature; there is no separate shell password. See [Management Commands Configuration](/configuration/shell) for every key and [Authentication](/security/authentication#management-signing-key) for the signing key pair.

## What the whitelist allows

Unless the whitelist is `["*"]`, the target agent checks every request:

- The command must be listed by its bare name. `uptime` is allowed by `uptime`; `/usr/bin/uptime` is always rejected.
- Arguments must not contain shell metacharacters such as `;`, `|`, `&`, `$`, `*`, or `~`.
- Arguments must not be absolute paths, so `journalctl -u muti-metroo` works but `tail -f /var/log/syslog` does not.

With `["*"]`, any command runs with any arguments. Commands are started directly, not through a shell, so pipes and redirections in the arguments are not interpreted either way; run them locally instead (see [Piping data](#piping-data)). The rules are described in full in [Access Control](/security/access-control#management-command-whitelist).

## Streaming mode

Without `--tty`, the command runs without a terminal. Standard output and standard error arrive separately, so you can redirect them locally, and the session ends when the command exits.

```bash
muti-metroo shell 1f743225 -- df -h
muti-metroo shell 1f743225 -- journalctl -u muti-metroo --since today
```

Put `--` before the remote command when its flags would otherwise be read as `muti-metroo` flags:

```bash
muti-metroo shell 1f743225 -- wc -l
```

### Piping data

In streaming mode, your local standard input is sent to the remote command, and closing it (end of file) closes the remote command's input. Commands that read until end of input finish normally:

```bash
seq 1 1000 | muti-metroo shell 1f743225 -- wc -l
cat ./build.log | muti-metroo shell 1f743225 -- grep -c ERROR
```

## Interactive mode

`--tty` allocates a pseudo-terminal on the target agent. Use it for programs that draw on the screen or read keystrokes, such as `htop`, `top`, `less`, `vim`, or a shell. Window resizes are passed to the remote terminal, and output arrives as one combined stream.

```bash
muti-metroo shell --tty 1f743225 htop
muti-metroo shell --tty 1f743225 bash
```

When no command is given, `bash` is started, so `muti-metroo shell --tty 1f743225` opens an interactive Bash session if `bash` is whitelisted.

Windows agents use ConPTY for interactive mode, which requires Windows 10 version 1809 or later.

## Other options

`-a` selects the gateway agent (default `localhost:8080`), and `-w` ends the session after a set duration such as `30s` or `5m` (default `0`, no limit). The target agent ID can be a unique prefix of the full ID. See [shell CLI](/cli/shell) for every flag and [Management Commands API](/api/shell) for the WebSocket protocol used by the CLI and the web dashboard.

## Available shells

When management commands are enabled, an agent reports the shells installed on its host (for example `bash`, `zsh`, `sh` on Linux and macOS, or `powershell.exe`, `pwsh.exe`, `cmd.exe` on Windows) in its node information, visible through the [Dashboard API](/api/dashboard). This list is informational: a shell can only be started if it is also in the whitelist.

## Troubleshooting

### command is not allowed

```text
Error: remote error: failed to start session: command 'ls' is not allowed
```

The command is not in the target agent's `shell.whitelist`, or it was given with a path. Use the bare command name, or add it to the whitelist.

### argument rejected

```text
Error: remote error: failed to start session: argument 0: absolute paths not allowed
Error: remote error: failed to start session: argument 0 contains dangerous characters
```

An argument is an absolute path or contains a shell metacharacter. Change to a relative argument where the command allows it, or ask the agent's administrator whether `["*"]` is acceptable for that agent.

### Signature or key errors

"management signing private key not configured on this agent" means the gateway agent cannot sign; point `-a` at an operator agent. "management signing public key not configured on this agent" means the target cannot verify; add `management.signing_public_key` to it. A rejected signature means the key pair does not match or the hosts' clocks differ by more than five minutes.

### max sessions reached

The target agent has reached `shell.max_sessions`. Wait for other sessions to end or raise the limit.

### Interactive program misbehaves

Programs such as `vim` or `htop` need `--tty`. Without it, they have no terminal to draw on.

## See Also

- [Management Commands Configuration](/configuration/shell): enable commands, whitelist, and session limit
- [shell CLI](/cli/shell): command reference
- [Management Commands API](/api/shell): WebSocket protocol
- [Authentication](/security/authentication): the signing key pair that authorizes sessions
- [Access Control](/security/access-control): whitelist rules in detail
