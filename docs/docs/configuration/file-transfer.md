---
title: File Transfer
sidebar_position: 13
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-presenting.png" alt="Mole configuring file transfer" style={{maxWidth: '180px'}} />
</div>

# File Transfer Configuration

Upload and download files to remote agents through the mesh. Supports individual files and directories with automatic compression.

:::warning Security Feature
File transfer is disabled by default. Enable only on agents that need file operations, and always pair it with path restrictions. Authorization requires the mesh management signing key (see [Authentication](/security/authentication)); there is no per-feature password.
:::

**Minimal secure setup:**

```yaml
file_transfer:
  enabled: true
  allowed_paths:
    - /tmp
management:
  signing_public_key: "..." # Generate the pair with: muti-metroo signing-key generate
```

## Configuration

```yaml
file_transfer:
  enabled: false # Disabled by default
  max_file_size: 524288000 # 500 MB (0 = unlimited)
  allowed_paths: [] # Paths allowed for transfer (empty = none)
```

## Options

| Option          | Type | Default     | Description                         |
| --------------- | ---- | ----------- | ----------------------------------- |
| `enabled`       | bool | `false`     | Enable file transfer                |
| `max_file_size` | int  | `524288000` | Maximum file size in bytes (500 MB) |
| `allowed_paths` | list | `[]`        | Allowed path patterns               |

## Management-key Authorization

Every upload, download, and browse request must carry a valid Ed25519 signature from the mesh management signing key configured in the `management` block. Agents without `management.signing_public_key` reject file-transfer requests; agents without `management.signing_private_key` cannot initiate them.

## Path Restrictions

The `allowed_paths` list controls which paths can be accessed:

### No Paths (Default)

```yaml
file_transfer:
  allowed_paths: [] # No paths allowed
```

### Specific Directories

```yaml
file_transfer:
  allowed_paths:
    - /tmp # /tmp and everything under it
    - /var/log # /var/log and everything under it
    - /home/deploy # Specific user directory
```

### Glob Patterns

```yaml
file_transfer:
  allowed_paths:
    - /data/** # Anything under /data
    - /home/*/uploads # uploads dir for any user
    - /var/log/*.log # Only .log files in /var/log
```

### All Paths (Testing Only)

```yaml
file_transfer:
  allowed_paths:
    - "*" # Allow everything - DANGEROUS
```

:::danger Never Use in Production
The `["*"]` wildcard allows access to any path. Only use for testing in isolated environments.
:::

### Pattern Syntax

| Pattern           | Matches                                        | Does NOT Match  |
| ----------------- | ---------------------------------------------- | --------------- |
| `/tmp`            | `/tmp`, `/tmp/file.txt`, `/tmp/dir/file`       | `/tmpdir`       |
| `/tmp/*`          | `/tmp/file.txt`                                | `/tmp/dir/file` |
| `/tmp/**`         | `/tmp/file.txt`, `/tmp/dir/file`, `/tmp/a/b/c` | -               |
| `/home/*/uploads` | `/home/alice/uploads`, `/home/bob/uploads`     | `/home/uploads` |
| `*.log`           | `app.log`, `/var/log/sys.log`                  | `app.txt`       |

A leading `~`, `~/`, or `~\` (Windows) in a request path is expanded to the agent user's home directory **before** the allowlist check, so an `allowed_paths` entry like `/home/agent/uploads/**` matches a client request for `~/uploads/file.txt`. Write the patterns against the absolute home path, not the tilde shortcut.

## File Size Limits

Control maximum transfer size:

```yaml
file_transfer:
  max_file_size: 104857600 # 100 MB
```

| Value        | Size             |
| ------------ | ---------------- |
| `0`          | Unlimited        |
| `10485760`   | 10 MB            |
| `104857600`  | 100 MB           |
| `524288000`  | 500 MB (default) |
| `1073741824` | 1 GB             |

:::tip Directory Transfers
For directory transfers, the limit applies to the compressed tar archive, not individual files.
:::

## Transfer Features

### Streaming

Files are streamed in chunks (16 KB) - no memory limits regardless of file size.

### Compression

Directories are automatically compressed with gzip during transfer.

### Permissions

File permissions (mode) are preserved during transfer.

### Resume

The CLI supports resuming interrupted transfers with `--resume`.

## Security Best Practices

1. **Restrict paths**: Only allow directories actually needed
2. **Set size limits**: Prevent disk exhaustion
3. **Protect the signing key**: Hold `management.signing_private_key` only on operator nodes; treat it as the most sensitive credential in the mesh.
4. **Monitor usage**: Check logs for file operations

### Recommended Configurations by Use Case

**Deployment staging:**

```yaml
file_transfer:
  allowed_paths:
    - /opt/deploy/staging
  max_file_size: 104857600 # 100 MB
```

**Log collection:**

```yaml
file_transfer:
  allowed_paths:
    - /var/log/*.log
    - /var/log/**/*.log
  max_file_size: 52428800 # 50 MB
```

**General file sharing:**

```yaml
file_transfer:
  allowed_paths:
    - /tmp
    - /home/shared
  max_file_size: 524288000 # 500 MB
```

## Examples

### Minimal Access

```yaml
file_transfer:
  enabled: true
  max_file_size: 10485760 # 10 MB
  allowed_paths:
    - /tmp
```

### Deployment Agent

```yaml
file_transfer:
  enabled: true
  max_file_size: 524288000 # 500 MB
  allowed_paths:
    - /opt/app
    - /etc/app
```

### Log Collection Agent

```yaml
file_transfer:
  enabled: true
  max_file_size: 104857600 # 100 MB
  allowed_paths:
    - /var/log
```

In every example, `management.signing_public_key` must also be set on the agent for file transfer to work. See the [Management](/configuration/management) section.

## Environment Variables

```yaml
file_transfer:
  enabled: ${FILE_TRANSFER_ENABLED:-false}
  max_file_size: ${FILE_TRANSFER_MAX_SIZE:-524288000}
```

## Related

- [File Transfer Usage](/features/file-transfer) - How to use file transfer
- [Management Commands](/configuration/shell) - Related remote administration feature
- [Security Overview](/security/overview) - Security considerations
