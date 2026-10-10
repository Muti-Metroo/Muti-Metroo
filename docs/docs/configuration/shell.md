---
title: Management Commands
sidebar_position: 12
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-presenting.png" alt="Mole configuring management commands" style={{maxWidth: '180px'}} />
</div>

# Management Commands Configuration

Execute management commands on remote agents through the mesh. Both interactive mode (PTY for vim, htop) and streaming mode for simple commands and continuous output are supported.

:::warning Security Feature
Management commands are disabled by default. Enable only on agents that need remote administration, and always pair them with a strict command whitelist. Authorization requires the mesh management signing key (see [Authentication](/security/authentication)); there is no per-feature password.
:::

**Minimal secure setup:**

```yaml
shell:
  enabled: true
  whitelist:
    - whoami
    - hostname
management:
  signing_public_key: "..." # Generate the pair with: muti-metroo signing-key generate
```

## Configuration

```yaml
shell:
  enabled: false # Disabled by default
  whitelist: [] # Commands allowed (empty = none)
  timeout: 0s # Command timeout (0 = no timeout)
  max_sessions: 0 # Max concurrent sessions (0 = unlimited)
```

## Options

| Option         | Type     | Default | Description                         |
| -------------- | -------- | ------- | ----------------------------------- |
| `enabled`      | bool     | `false` | Enable management commands          |
| `whitelist`    | list     | `[]`    | Allowed command names               |
| `timeout`      | duration | `0s`    | Maximum command execution time      |
| `max_sessions` | int      | `0`     | Maximum concurrent command sessions |

## Management-key Authorization

Every management command request must carry a valid Ed25519 signature from the mesh management signing key configured in the `management` block. Agents without `management.signing_public_key` reject command requests; agents without `management.signing_private_key` cannot initiate them. The same key also authorizes file transfer, dynamic config mutations, and sleep/wake.

## Command Whitelist

The whitelist controls which commands can be executed:

### No Commands (Default)

```yaml
shell:
  whitelist: [] # No commands allowed
```

### Specific Commands

```yaml
shell:
  whitelist:
    - whoami
    - hostname
    - date
    - uptime
    - journalctl
```

### All Commands (Testing Only)

```yaml
shell:
  whitelist:
    - "*" # Allow everything - DANGEROUS
```

:::danger Never Use in Production
The `["*"]` wildcard allows arbitrary command execution. Only use for testing in isolated environments.
:::

### Whitelist Rules

- Commands must be **base names only** (no paths)
- `bash` allows `bash`, not `/bin/bash`
- Arguments are not restricted - `journalctl -u muti-metroo -f` works if `journalctl` is whitelisted
- Shell built-ins work through the shell (e.g., `bash -c "echo hello"`)

## Session Limits

Control resource usage:

```yaml
shell:
  max_sessions: 10 # Max 10 concurrent command sessions
  timeout: 5m # Commands timeout after 5 minutes
```

| Setting            | Value      | Effect                                   |
| ------------------ | ---------- | ---------------------------------------- |
| `max_sessions: 0`  | Unlimited  | No limit on concurrent sessions          |
| `max_sessions: 10` | Limited    | New sessions rejected when limit reached |
| `timeout: 0s`      | No timeout | Commands run indefinitely                |
| `timeout: 5m`      | 5 minutes  | Commands killed after timeout            |

## Modes

### Streaming Mode (Default)

For simple commands and continuous output:

```bash
muti-metroo shell <agent-id> whoami
muti-metroo shell <agent-id> journalctl -u muti-metroo -f
```

### Interactive Mode (PTY)

For programs requiring a terminal:

```bash
muti-metroo shell --tty <agent-id> htop
muti-metroo shell --tty <agent-id> vim /etc/config.yaml
```

## Platform Support

| Platform | Interactive (PTY) | Streaming |
| -------- | ----------------- | --------- |
| Linux    | Yes               | Yes       |
| macOS    | Yes               | Yes       |
| Windows  | Yes (ConPTY)      | Yes       |

## Security Best Practices

1. **Use specific whitelist**: Only allow commands actually needed
2. **Set session limits**: Prevent resource exhaustion
3. **Use timeouts**: Prevent hung commands
4. **Protect the signing key**: Hold `management.signing_private_key` only on operator nodes; treat it as the most sensitive credential in the mesh.
5. **Audit usage**: Monitor management command usage in logs

### Recommended Whitelists by Use Case

**Monitoring only:**

```yaml
whitelist:
  - whoami
  - hostname
  - uptime
  - date
  - df
  - free
```

**Log access:**

```yaml
whitelist:
  - journalctl
  - tail
  - cat
  - grep
```

**Full administration:**

```yaml
whitelist:
  - bash
  - sh
  - vim
  - nano
  - systemctl
  - journalctl
```

## Examples

### Monitoring Agent

```yaml
shell:
  enabled: true
  whitelist:
    - whoami
    - hostname
    - uptime
  max_sessions: 5
  timeout: 1m
```

### Administration Agent

```yaml
shell:
  enabled: true
  whitelist:
    - bash
    - vim
    - systemctl
    - journalctl
  max_sessions: 3
  timeout: 30m
```

### Development Agent

```yaml
shell:
  enabled: true
  whitelist:
    - "*" # Testing only!
  max_sessions: 0
  timeout: 0s
```

In every example, `management.signing_public_key` must also be set on the agent for management commands to work. See the [Management](/configuration/management) section.

## Environment Variables

```yaml
shell:
  enabled: ${SHELL_ENABLED:-false}
  timeout: "${SHELL_TIMEOUT:-5m}"
```

## Related

- [Management Commands](/features/shell) - How to run management commands
- [Security Overview](/security/overview) - Security considerations
- [File Transfer](/configuration/file-transfer) - Related remote access feature
