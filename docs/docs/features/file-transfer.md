---
title: File Transfer
sidebar_position: 4
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-presenting.png" alt="Mole presenting file transfer" style={{maxWidth: '180px'}} />
</div>

# File Transfer

Move files to and from any agent in your mesh. Grab a config file from a remote server, deploy scripts to multiple machines, or move large datasets between machines.

```bash
# Download a file from a remote agent
muti-metroo download abc123 /etc/passwd ./passwd.txt

# Upload a script to a remote agent
muti-metroo upload abc123 ./deploy.sh /tmp/deploy.sh

# Transfer entire directories
muti-metroo upload abc123 ./tools /opt/tools
```

:::tip Configuration
See [File Transfer Configuration](/configuration/file-transfer) for all options including path restrictions, size limits, and authentication.
:::

## CLI Usage

### Upload File

```bash
muti-metroo upload <agent-id> <local-path> <remote-path>

# Example
muti-metroo upload abc123 ./data.bin /tmp/data.bin
```

### Upload Directory

```bash
muti-metroo upload abc123 ./mydir /tmp/mydir
```

### Download File

```bash
muti-metroo download <agent-id> <remote-path> <local-path>

# Example
muti-metroo download abc123 /etc/config.yaml ./config.yaml
```

### Authorization

File transfer is gated by the mesh management Ed25519 signing keypair. The local agent reached via `-a` must have `management.signing_private_key` configured; the target agent must have `management.signing_public_key` configured. The `muti-metroo` CLI signs every request automatically -- there is no per-file password. See [Authentication](/security/authentication) for the full model.

## HTTP API

### Upload

`POST /agents/{agent-id}/file/upload`

Multipart form data:

- `file`: File to upload
- `path`: Remote destination path
- `directory`: "true" if directory tar

Requires a valid management signature header (the CLI handles signing).

### Download

`POST /agents/{agent-id}/file/download`

Request:

```json
{
  "path": "/tmp/file.txt"
}
```

Response: Binary file data. Same management-signature requirement as upload.

## File Browsing

Browse the filesystem on remote agents. Shares `allowed_paths` and the same management-signature authorization as file transfer -- no additional configuration needed.

```bash
# List directory contents
curl -X POST http://localhost:8080/agents/abc123/file/browse \
  -H "Content-Type: application/json" \
  -d '{"action":"list","path":"/tmp"}'

# Get info about a specific file
curl -X POST http://localhost:8080/agents/abc123/file/browse \
  -H "Content-Type: application/json" \
  -d '{"action":"stat","path":"/tmp/config.yaml"}'

# Discover browsable root paths
curl -X POST http://localhost:8080/agents/abc123/file/browse \
  -H "Content-Type: application/json" \
  -d '{"action":"roots"}'

# Change file permissions
curl -X POST http://localhost:8080/agents/abc123/file/browse \
  -H "Content-Type: application/json" \
  -d '{"action":"chmod","path":"/tmp/script.sh","mode":"0755"}'

# Delete a file
curl -X POST http://localhost:8080/agents/abc123/file/browse \
  -H "Content-Type: application/json" \
  -d '{"action":"delete","path":"/tmp/old-config.yaml"}'

# Delete a non-empty directory
curl -X POST http://localhost:8080/agents/abc123/file/browse \
  -H "Content-Type: application/json" \
  -d '{"action":"delete","path":"/tmp/old-logs","recursive":true}'
```

The `list` action supports pagination with `offset` and `limit` parameters (default limit: 100, max: 5000). Entries are sorted with directories first, then files, alphabetically by name. The `delete` action removes files and empty directories directly; non-empty directories require `"recursive": true`.

See [API - File Transfer](/api/file-transfer) for full request/response details.

## Implementation Details

- **Streaming**: Files transferred in 16KB chunks
- **Streaming**: Stream directly without full-file memory buffering (configurable `max_file_size`, default 500 MB)
- **Directories**: Automatically tar/gzip with permission preservation
- **Authorization**: Ed25519 mesh management signature (no per-file password)
- **Permissions**: File mode preserved (Unix)

## Rate Limiting

Limit transfer speed to avoid saturating network links.

```bash
# Upload at max 100 KB/s
muti-metroo upload --rate-limit 100KB abc123 ./large.iso /tmp/large.iso

# Download at max 1 MB/s
muti-metroo download --rate-limit 1MB abc123 /data/backup.tar.gz ./backup.tar.gz
```

Supported size formats:

- Decimal: `100KB`, `1MB`, `10GB` (1 KB = 1000 bytes)
- Binary: `100KiB`, `1MiB`, `10GiB` (1 KiB = 1024 bytes)
- Plain bytes: `1024000`

Rate limiting is applied by the **sending agent**:

- Upload: Your local agent limits the upload speed
- Download: The remote agent limits the download speed

## Resume Support

Continue interrupted transfers without starting over.

```bash
# Resume interrupted download
muti-metroo download --resume abc123 /data/large.iso ./large.iso

# Resume interrupted upload
muti-metroo upload --resume abc123 ./huge.iso /tmp/huge.iso

# Combine with rate limiting
muti-metroo download --rate-limit 500KB --resume abc123 /data/huge.iso ./huge.iso
```

### How It Works

1. Partial data is written to `<filename>.partial`
2. Progress is tracked in `<filename>.partial.json`
3. On resume, transfer continues from the last byte written
4. On completion, `.partial` is renamed to the final filename

### Validation

Resume uses **file size comparison** to detect if the source file changed:

- If the original file size matches, transfer resumes from the offset
- If size differs, transfer restarts from the beginning

:::note Directory Transfers
Resume is not supported for directory transfers. If a directory transfer is interrupted, it will restart from the beginning.
:::

## Troubleshooting

### Permission Denied

```
Error: path not allowed: /etc/passwd
```

**Cause:** The path is not in the agent's `allowed_paths` configuration.

**Solutions:**

- Check the agent's `allowed_paths` setting
- Use a path that's explicitly allowed
- Contact the agent administrator to add the path

### Authentication Failed

```
Error: authentication required
```

**Cause:** The request was not signed with a valid management signing key.

**Solutions:**

- The local agent reached via `-a` must have `management.signing_private_key` set in its config -- only operator nodes typically hold the private half.
- The target agent must have `management.signing_public_key` set; without it, every file-transfer request is rejected.
- Generate a fresh keypair with `muti-metroo signing-key generate` and distribute the public half to every agent in the mesh; keep the private half on operator hosts.

```
Error: invalid credentials
```

**Cause:** The signature did not verify against the target's `management.signing_public_key`. Usually the operator and target are using different keys.

### File Too Large

```
Error: file exceeds maximum size limit
```

**Cause:** The file exceeds the agent's `max_file_size` setting.

**Solutions:**

- Check the size limit: `max_file_size: 0` means unlimited
- Split the file into smaller parts
- Contact the agent administrator to increase the limit

### Agent Not Reachable

```
Error: no route to agent abc123
```

**Cause:** The target agent is not connected to the mesh or routes haven't propagated.

**Solutions:**

```bash
# Check if agent is known
curl http://localhost:8080/agents

# Verify connectivity
curl http://localhost:8080/healthz | jq '.peer_count'
```

### Transfer Interrupted

If a transfer is interrupted, use `--resume` to continue:

```bash
# Resume from where it left off
muti-metroo download --resume abc123 /data/large.iso ./large.iso
```

Note: Resume is not supported for directory transfers.

## Related

- [CLI - File Transfer](/cli/file-transfer) - CLI reference
- [API - File Transfer](/api/file-transfer) - HTTP API reference
- [Security - Access Control](/security/access-control) - Path restrictions
- [Troubleshooting - Common Issues](/troubleshooting/common-issues) - File transfer issues
