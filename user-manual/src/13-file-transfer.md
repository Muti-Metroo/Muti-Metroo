# File Transfer

Upload and download files and directories to/from remote agents using streaming transfers.

## Configuration

```yaml
file_transfer:
  enabled: true
  max_file_size: 524288000    # 500 MB, 0 = unlimited
  allowed_paths:
    - /tmp
    - /home/user/uploads
```

File transfer is authorized by the mesh management Ed25519 signing key (see the Management Keys chapter); there is no per-feature password.

## CLI Usage

### Upload File

```bash
muti-metroo upload <agent-id> <local-path> <remote-path>

# Example
muti-metroo upload abc123 ./data.bin /tmp/data.bin
```

### Upload Directory

Directories are automatically tar/gzipped:

```bash
muti-metroo upload abc123 ./mydir /tmp/mydir
```

### Download File

```bash
muti-metroo download <agent-id> <remote-path> <local-path>

# Example
muti-metroo download abc123 /etc/config.yaml ./config.yaml
```

### Download Directory

```bash
muti-metroo download abc123 /var/log/app ./app-logs
```

### Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--agent` | `-a` | `localhost:8080` | Agent HTTP API address (host:port or http(s):// URL) |
| `--token` | | | Bearer token for the agent HTTP API (or set `MUTI_METROO_TOKEN`) |
| `--timeout` | `-w` | `5m` | Transfer timeout. `-t` is a deprecated alias that warns and will mean `--target` in a future release |
| `--rate-limit` | | | Limit transfer speed |
| `--resume` | | | Resume interrupted transfer |
| `--quiet` | `-q` | | Suppress progress output |

### Authorization

File transfer requires the local agent reached via `-a` to hold `management.signing_private_key`, and the target agent to hold `management.signing_public_key`. The CLI signs each request automatically; there is no `--password` / `-p` flag.

## Rate Limiting

Limit transfer speed to avoid saturating network links:

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

## Resume Support

Continue interrupted transfers:

```bash
# Resume interrupted download
muti-metroo download --resume abc123 /data/large.iso ./large.iso

# Resume interrupted upload
muti-metroo upload --resume abc123 ./huge.iso /tmp/huge.iso

# Combine with rate limiting
muti-metroo download --rate-limit 500KB --resume abc123 /data/huge.iso ./huge.iso
```

### How Resume Works

1. Partial data is written to `<filename>.partial`
2. Progress is tracked in `<filename>.partial.json`
3. On resume, transfer continues from the last byte
4. On completion, `.partial` is renamed to final filename

**Note**: Resume is not supported for directory transfers.

## Access Control

### allowed_paths Configuration

| Configuration | Behavior |
|--------------|----------|
| `[]` (empty) | No paths allowed - feature disabled |
| `["*"]` | All absolute paths allowed |
| `["/tmp", "/data"]` | Only specified prefixes allowed |

### Glob Pattern Support

```yaml
file_transfer:
  allowed_paths:
    # Prefix match
    - /tmp

    # Recursive glob
    - /data/**

    # Wildcard in path
    - /home/*/uploads

    # Extension matching
    - /var/log/*.log
```

### Pattern Examples

| Pattern | Matches | Does Not Match |
|---------|---------|----------------|
| `/tmp` | `/tmp`, `/tmp/file.txt` | `/tmpevil` |
| `/home/*/uploads` | `/home/alice/uploads` | `/home/uploads` |
| `/var/log/*.log` | `/var/log/syslog.log` | `/var/log/app/error.log` |

## File Browsing

Browse the filesystem on remote agents via the HTTP API. Shares `allowed_paths` and the same management-signature authorization as file transfer.

### API: POST /agents/{agent-id}/file/browse

Five actions are available: `list`, `stat`, `roots`, `chmod`, and `delete`.

**List directory contents:**

```bash
curl -X POST http://localhost:8080/agents/abc123/file/browse \
  -H "Content-Type: application/json" \
  -d '{"action":"list","path":"/tmp","limit":100}'
```

Response:

```json
{
  "path": "/tmp",
  "entries": [
    { "name": "subdir", "size": 4096, "mode": "0755",
      "mod_time": "2026-02-17T08:00:00Z", "is_dir": true },
    { "name": "file.txt", "size": 1024, "mode": "0644",
      "mod_time": "2026-02-18T10:30:00Z", "is_dir": false }
  ],
  "total": 2,
  "truncated": false
}
```

**Stat a single path:**

```bash
curl -X POST http://localhost:8080/agents/abc123/file/browse \
  -H "Content-Type: application/json" \
  -d '{"action":"stat","path":"/tmp/file.txt"}'
```

**Discover browsable roots:**

```bash
curl -X POST http://localhost:8080/agents/abc123/file/browse \
  -H "Content-Type: application/json" \
  -d '{"action":"roots"}'
```

Response:

```json
{ "roots": ["/tmp", "/data"] }
```

When `allowed_paths: ["*"]`, the response includes a `wildcard` flag:

```json
{ "roots": ["/"], "wildcard": true }
```

**Change file permissions:**

```bash
curl -X POST http://localhost:8080/agents/abc123/file/browse \
  -H "Content-Type: application/json" \
  -d '{"action":"chmod","path":"/tmp/script.sh","mode":"0755"}'
```

**Delete a file or directory:**

```bash
# Delete a file
curl -X POST http://localhost:8080/agents/abc123/file/browse \
  -H "Content-Type: application/json" \
  -d '{"action":"delete","path":"/tmp/old-config.yaml"}'

# Delete a non-empty directory (requires recursive flag)
curl -X POST http://localhost:8080/agents/abc123/file/browse \
  -H "Content-Type: application/json" \
  -d '{"action":"delete","path":"/tmp/old-logs","recursive":true}'
```

Files, symlinks, and empty directories are deleted directly. Non-empty directories require `"recursive": true`. Without it, the request is rejected.

The `list` action supports pagination via `offset` and `limit` (default 100, max 200). Entries are sorted with directories first, then files, alphabetically by name. Symlinks include `is_symlink` and `link_target` fields.

## Implementation Details

- **Streaming**: Files transferred in 16KB chunks
- **No size limits**: Stream directly without memory buffering
- **Directories**: Automatically tar/gzip with permission preservation
- **Permissions**: File mode preserved (Unix)

## Example Configuration

### Minimal (Testing)

```yaml
file_transfer:
  enabled: true
  allowed_paths:
    - "*"           # Allow all paths (TESTING ONLY)
```

### Production

```yaml
file_transfer:
  enabled: true
  max_file_size: 104857600    # 100 MB limit
  allowed_paths:
    - /tmp
    - /var/www/uploads
    - /home/*/data

management:
  signing_public_key: "..."   # Operators sign requests; targets verify with this public half
```

## Troubleshooting

### Path Not Allowed

```
Error: path not in allowed paths
```

Add the path prefix to `allowed_paths`:

```yaml
file_transfer:
  allowed_paths:
    - /your/path
```

### File Too Large

```
Error: file exceeds maximum size
```

Increase `max_file_size` or set to 0 for unlimited:

```yaml
file_transfer:
  max_file_size: 0
```

### Transfer Stalls

For large transfers over slow links, use rate limiting to prevent timeouts:

```bash
muti-metroo download --rate-limit 500KB --timeout 3600 abc123 /data/large.file ./large.file
```
