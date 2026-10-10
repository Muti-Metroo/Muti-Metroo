# Management Commands

Run management commands on remote agents with support for both streaming and interactive modes:

- **Streaming mode**: Standard command execution with Unix-style stdin piping (default)
- **Interactive mode**: Full terminal support for vim, bash, htop

## Configuration

```yaml
shell:
  enabled: false # Disabled by default (security)
  whitelist: [] # Commands allowed (empty = none)
  timeout: 0s # Command timeout (0 = no timeout)
  max_sessions: 0 # Concurrent sessions (0 = unlimited)
```

**Warning**: Remote command execution is a powerful feature. Enable only when needed and configure security controls carefully.

## Security Model

### Command Whitelist

Only whitelisted commands can run:

| Configuration               | Behavior                             |
| --------------------------- | ------------------------------------ |
| `[]` (empty)                | No commands allowed (default)        |
| `["*"]`                     | All commands allowed (testing only!) |
| `["bash", "vim", "whoami"]` | Only specified commands              |

```yaml
shell:
  whitelist:
    - bash
    - sh
    - whoami
    - hostname
    - cat
    - ls
```

### Management-Key Authorization

Every management command request must carry a valid Ed25519 signature from the mesh management signing key (configured under the `management` block). There is no per-feature password.

- Target agent must have `management.signing_public_key` set to accept management command requests.
- Gateway agent (the one the CLI connects to via `-a`) must have `management.signing_private_key` set to initiate them.

Generate the pair with `muti-metroo signing-key generate` and distribute the public half to every agent in the mesh. See the Security chapter for the full authorization model.

## Modes

### Streaming Mode (Default)

Standard execution without PTY allocation:

- Separate stdout and stderr streams
- Commands run until exit and return an exit code
- No terminal control characters
- Local stdin is streamed to the remote command, and stdin EOF is propagated so EOF-sensitive commands (`cat`, `wc`, `sort`, `sha256sum`, `tar xzf -`, ...) terminate naturally

```bash
# Simple commands
muti-metroo shell abc123 whoami
muti-metroo shell abc123 ls -la /tmp

# Long-running streaming commands
muti-metroo shell abc123 journalctl -u muti-metroo -f
muti-metroo shell abc123 tail -f /var/log/syslog

# Pipe local data through the mesh to a remote command
cat build.log | muti-metroo shell abc123 -- grep ERROR
seq 1 1000   | muti-metroo shell abc123 -- wc -l
head -c 1M /dev/urandom | muti-metroo shell abc123 sha256sum
tar cz ./src | muti-metroo shell abc123 -- tar xzf - -C /tmp/deploy
```

**Tip:** insert `--` before the remote command to stop cobra from interpreting short flags as `muti-metroo shell` flags. Without it, `muti-metroo shell abc123 wc -l` complains that `-l` is unknown.

### Interactive Mode (--tty)

Allocates a PTY (pseudo-terminal) on the remote agent:

- Full terminal emulation
- Supports terminal resize
- Works with interactive programs (vim, less, htop)
- Single combined stdout/stderr stream
- EOF delivered via PTY master close / Ctrl-D

```bash
# Interactive bash
muti-metroo shell --tty abc123 bash

# Edit files with vim
muti-metroo shell --tty abc123 vim /etc/config.yaml

# System monitoring
muti-metroo shell --tty abc123 htop
```

## CLI Usage

```bash
muti-metroo shell [flags] <agent-id> [command] [args...]
```

### Flags

| Flag        | Short | Default          | Description                                                                                                   |
| ----------- | ----- | ---------------- | ------------------------------------------------------------------------------------------------------------- |
| `--agent`   | `-a`  | `localhost:8080` | Agent HTTP API address (host:port or http(s):// URL)                                                          |
| `--timeout` | `-w`  | `0`              | Session timeout (seconds). `-t` is a deprecated alias that warns and will mean `--target` in a future release |
| `--tty`     |       |                  | Interactive mode with PTY                                                                                     |

### Examples

```bash
# Simple command (streaming mode)
muti-metroo shell abc123 whoami

# Follow logs (streaming mode)
muti-metroo shell abc123 journalctl -f

# Interactive bash
muti-metroo shell --tty abc123 bash

# Via different agent's HTTP API
muti-metroo shell -a 192.168.1.10:8080 --tty abc123 top

# With timeout (auto-terminate after 300 seconds)
muti-metroo shell -t 300 abc123 bash

# Pipe a local file through the mesh to remote sha256sum
cat /etc/passwd | muti-metroo shell abc123 sha256sum
```

## Platform Support

| Platform | Interactive (PTY) | Streaming |
| -------- | :---------------: | :-------: |
| Linux    |        Yes        |    Yes    |
| macOS    |        Yes        |    Yes    |
| Windows  |   Yes (ConPTY)    |    Yes    |

Windows uses ConPTY (Windows Pseudo Console) for interactive sessions, available on Windows 10 version 1809 and later.

## Shell Detection

Each agent automatically detects installed shells at startup. When `shell.enabled` is `true`, the agent advertises both the detected shells and a `shell_enabled` flag to the mesh. You can see which shells are available on any agent through the `/api/nodes` endpoint. When shell is disabled, neither the shell list nor the enabled flag appears in the API response.

Probed shells by platform:

- **Linux/macOS**: bash, sh, zsh, fish, ash, dash, ksh
- **Windows**: powershell.exe, pwsh.exe, cmd.exe

Shell detection is separate from the command whitelist. A detected shell only reports what is installed -- the whitelist controls what is allowed to execute.

### Windows PowerShell

```bash
# Interactive PowerShell on Windows target
muti-metroo shell --tty abc123 powershell

# Or cmd.exe
muti-metroo shell --tty abc123 cmd
```

## Example Configuration

### Minimal (Testing)

```yaml
shell:
  enabled: true
  whitelist:
    - "*" # Allow all commands (TESTING ONLY)
management:
  signing_public_key: "..." # Required to accept inbound management command requests
```

### Production

```yaml
shell:
  enabled: true
  whitelist:
    - bash
    - sh
    - whoami
    - hostname
    - cat
    - ls
    - ps
    - netstat
  timeout: 3600s # 1 hour max session
  max_sessions: 5 # Limit concurrent sessions
management:
  signing_public_key: "..." # Required to accept inbound management command requests
```

## Troubleshooting

### Command Rejected

```
Error: command not in whitelist
```

Add the command to the whitelist:

```yaml
shell:
  whitelist:
    - your-command
```

### Authentication Failed

```
Error: management signature required
Error: management signing public key not configured on this agent
```

The gateway cannot sign, or the target has no public key. Ensure both sides of `management.signing_{private,public}_key` are configured. Generate with `muti-metroo signing-key generate`.

### PTY Not Working

Ensure you're using the `--tty` flag for interactive programs:

```bash
# Wrong (no terminal)
muti-metroo shell abc123 vim

# Correct
muti-metroo shell --tty abc123 vim
```

### Piped Command Hangs

If a remote command that consumes stdin hangs despite your pipe closing, make sure both the gateway and the target agent are on Muti Metroo v4.0.0 or later -- stdin EOF propagation was added in that release. Older servers silently drop the stdin-close message.
