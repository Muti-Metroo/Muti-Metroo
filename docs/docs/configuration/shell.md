---
title: Shell
description: Configuration reference for the shell section, which enables remote management commands and controls the command whitelist, argument checks, timeout, and session limit.
sidebar_position: 18
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-presenting.png" alt="Mole configuring management commands" style={{maxWidth: '180px'}} />
</div>

# Shell

The `shell` section lets operators run management commands on this agent through the mesh, either streaming (for `whoami` or `journalctl -f`) or interactive with a terminal (for `htop` or `vim`). It is disabled by default. Every request must also be signed with the mesh management signing key, see [Management](/configuration/management#command-signing-keys).

## Configuration

```yaml
shell:
  enabled: true
  whitelist:
    - whoami
    - hostname
    - journalctl
  timeout: 5m
  max_sessions: 10

management:
  signing_public_key: "91ec6f66440771f9a68f2d65adb835faccdff429e97f9e4be1ce5ac10eec158f"
```

## Keys

| Key             | Type     | Default | Description                                                                                                    |
| --------------- | -------- | ------- | -------------------------------------------------------------------------------------------------------------- |
| `enabled`       | bool     | `false` | Accept management command sessions.                                                                            |
| `whitelist`     | list     | `[]`    | Command names that may be run. Empty allows nothing; `["*"]` allows any command with any arguments.           |
| `timeout`       | duration | `0s`    | Run time limit for streaming commands whose client does not request its own timeout; a timeout sent by the client takes its place. Interactive sessions are not limited. `0` means no limit. |
| `max_sessions`  | int      | `0`     | Maximum concurrent sessions. Further sessions are refused. `0` means no limit.                                 |
| `password_hash` | string   | `""`    | Deprecated and ignored. Authorization uses the management signing key. `muti-metroo run` prints a warning when it is set. |

The agent also needs `management.signing_public_key`; without it, every command request is refused.

## Whitelist

```yaml
shell:
  enabled: true
  whitelist:
    - whoami
    - uptime
    - journalctl
```

A command runs only when its name exactly matches a whitelist entry (case-sensitive). Names with a path, such as `/usr/bin/whoami`, are always refused unless the whitelist is `["*"]`; the command is looked up on the agent's `PATH`.

Unless the whitelist is `["*"]`, arguments are checked as well. An argument is refused when it:

- contains any of the characters `` ; & | $ ` ( ) { } [ ] < > \ ! * ? ~ ``, or
- is an absolute path, such as `/etc/hosts`.

So with `journalctl` whitelisted, `journalctl -u muti-metroo -f` runs, but `cat /etc/hosts` is refused even with `cat` whitelisted. Whitelisting a shell such as `bash` gives the caller everything the shell can do.

:::danger
`whitelist: ["*"]` allows any command with any arguments, as the user the agent runs as. Use it only on test systems.
:::

## Platform support

| Platform | Streaming | Interactive         |
| -------- | --------- | ------------------- |
| Linux    | Yes       | Yes                 |
| macOS    | Yes       | Yes                 |
| Windows  | Yes       | Yes (ConPTY)        |

## Examples

### Read-only diagnostics

```yaml
shell:
  enabled: true
  whitelist:
    - whoami
    - hostname
    - uptime
    - df
  max_sessions: 5
  timeout: 1m
```

### Test system with no restrictions

```yaml
shell:
  enabled: true
  whitelist:
    - "*"
```

## See Also

- [Management Commands usage](/features/shell) for running streaming and interactive commands
- [shell CLI](/cli/shell) for the client command and its flags
- [Management](/configuration/management) for the signing keys every request needs
- [File Transfer](/configuration/file-transfer) for the other remote administration feature
- [Security best practices](/security/best-practices) for hardening agents that accept commands
