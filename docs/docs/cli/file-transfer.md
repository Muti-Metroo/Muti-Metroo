---
title: File Transfer (upload/download)
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-reading.png" alt="Mole transferring files" style={{maxWidth: '180px'}} />
</div>

# File Transfer Commands

Move files and directories to and from remote agents through the mesh. Works with any file size - data streams directly without loading into memory.

**Quick examples:**
```bash
# Upload a file
muti-metroo upload abc123 ./local-file.txt /tmp/remote-file.txt

# Download a file
muti-metroo download abc123 /etc/config.yaml ./config.yaml

# Upload an entire directory
muti-metroo upload abc123 ./my-folder /tmp/my-folder
```

## muti-metroo upload

Upload file or directory to remote agent.

### Usage

```bash
muti-metroo upload [flags] <target-agent-id> <local-path> <remote-path>
```

### Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--agent` | `-a` | `localhost:8080` | Agent HTTP API address (host:port or http(s):// URL) |
| `--token` | | | Bearer token for the agent HTTP API (or set `MUTI_METROO_TOKEN`) |
| `--timeout` | `-t` | `5m` | Transfer timeout (e.g., 30s, 5m, 1h) |
| `--rate-limit` | | | Max transfer speed (e.g., 100KB, 1MB, 10MiB) |
| `--resume` | | `false` | Resume interrupted transfer if possible |
| `--quiet` | `-q` | `false` | Suppress progress output |

File transfer is authorized by the mesh management Ed25519 signing key (see [Authentication](/security/authentication)). The local agent reached via `-a` must have `management.signing_private_key` set, and the target agent must have `management.signing_public_key` set. There is no per-file password.

### Examples

```bash
# Upload file
muti-metroo upload abc123 ./data.bin /tmp/data.bin

# Upload directory (auto-detected)
muti-metroo upload abc123 ./mydir /tmp/mydir

# Via different agent (HTTP API host:port)
muti-metroo upload -a 192.168.1.10:8080 abc123 data.txt /tmp/data.txt

# Rate-limited upload (100 KB/s)
muti-metroo upload --rate-limit 100KB abc123 ./large.iso /tmp/large.iso

# Resume interrupted upload
muti-metroo upload --resume abc123 ./huge.iso /tmp/huge.iso
```

## muti-metroo download

Download file or directory from remote agent.

### Usage

```bash
muti-metroo download [flags] <target-agent-id> <remote-path> <local-path>
```

### Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--agent` | `-a` | `localhost:8080` | Agent HTTP API address (host:port or http(s):// URL) |
| `--token` | | | Bearer token for the agent HTTP API (or set `MUTI_METROO_TOKEN`) |
| `--timeout` | `-t` | `5m` | Transfer timeout (e.g., 30s, 5m, 1h) |
| `--rate-limit` | | | Max transfer speed (e.g., 100KB, 1MB, 10MiB) |
| `--resume` | | `false` | Resume interrupted transfer if possible |
| `--quiet` | `-q` | `false` | Suppress progress output |

Like `muti-metroo upload`, downloads are authorized by the mesh management signing key. See [Authentication](/security/authentication).

### Examples

```bash
# Download file
muti-metroo download abc123 /etc/config.yaml ./config.yaml

# Download directory
muti-metroo download abc123 /var/log/app ./logs

# Rate-limited download (1 MB/s)
muti-metroo download --rate-limit 1MB abc123 /data/backup.tar.gz ./backup.tar.gz

# Resume interrupted download
muti-metroo download --resume abc123 /data/large.iso ./large.iso

# Combine rate limit and resume
muti-metroo download --rate-limit 500KB --resume abc123 /data/huge.iso ./huge.iso
```

## Implementation Notes

- Directories are automatically tar/gzip compressed
- File permissions are preserved
- Streaming transfer (no size limits)
- Remote paths beginning with `~`, `~/`, or `~\` (Windows) are expanded to the agent user's home directory before path-allowlist checks. The expansion is server-side, so the same shorthand works for upload, download, and the `/agents/{id}/file/browse` API.

:::tip Agent ID Prefix
You can use a short agent ID prefix (e.g., `abc123`) instead of the full 32-character ID. The prefix is automatically resolved to the full agent ID.
:::

## See Also

- [File Transfer Feature](/features/file-transfer) - Detailed documentation
