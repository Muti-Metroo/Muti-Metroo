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

The signature covers the file path and transfer options, so a captured request cannot be reused for another path or operation. Agents before 4.12.0 signed file operations without the request body: a gateway and a target on different sides of that change reject each other's file requests with `invalid_credentials`, so upgrade both (Chapter 21).

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

Paths must be absolute and must not contain `..`. A leading `~` is expanded to the home directory of the user the target agent runs as.

### Symbolic Links

Every path must stay inside `allowed_paths` after symbolic links are resolved. A link inside an allowed directory that points outside it cannot be used to read, write, list, or change anything outside, whether the link is the last path component or a parent directory; such requests fail with `symlink target not allowed`. `delete` and `rename` act on the link itself, so a link that points outside can still be deleted or renamed.

## HTTP API

The CLI and the Muti Metroo Manager file browser are clients of three HTTP endpoints on the local agent. All three accept only `POST`:

| Endpoint | Description |
|----------|-------------|
| `POST /agents/{agent-id}/file/upload` | Upload a file or directory |
| `POST /agents/{agent-id}/file/download` | Download a file or directory |
| `POST /agents/{agent-id}/file/browse` | List directories, inspect, chmod, delete, create directories, and rename |

`{agent-id}` can be any agent in the mesh, including the serving agent's own ID. A request for the own ID is signed with the agent's own key and checked against its own `file_transfer` settings, like a request from the mesh.

### Upload

`file/upload` takes `multipart/form-data`:

| Form field | Required | Description |
|------------|----------|-------------|
| `file` | Yes | File content. It may be empty. |
| `path` | Yes | Destination path on the target. Missing parent directories are created. |
| `directory` | No | `true` when `file` is a tar archive (optionally gzip-compressed) to unpack into `path` |
| `mode` | No | Octal permission bits up to `0777` for a newly created file, for example `"0644"`. Default `0600`. |
| `rate_limit` | No | Maximum transfer rate in bytes per second; `0` or omitted means unlimited |

```bash
curl -X POST http://localhost:8080/agents/abc123/file/upload \
  -F "file=@./app.conf" \
  -F "path=/srv/app/app.conf" \
  -F "mode=0644"
```

A new file gets mode `0600` (or `mode`). A file that already exists is overwritten in place and keeps its mode. Files inside an uploaded directory archive keep their modes.

### Download

`file/download` takes a JSON body:

| Field | Required | Description |
|-------|----------|-------------|
| `path` | Yes | Path on the target |
| `rate_limit` | No | Maximum transfer rate in bytes per second |
| `offset` | No | Start a file download at this byte offset, for example to resume |
| `length` | No | Return at most this many bytes from `offset`; `0` or omitted means to the end of the file. Files only. |
| `original_size` | No | Expected total size when resuming; the download fails if the file has changed size |

A file is returned as raw content with the headers `X-File-Mode` (permission bits in octal) and `X-Original-Size` (file size in bytes). `X-Original-Size` is always the full file size, also for a download with `offset` or `length`. A directory is returned as a gzip-compressed tar archive; ranges are supported for files only. Upload and download time out after 30 minutes.

Read bytes 1024 to 2047 of a file:

```bash
curl -X POST http://localhost:8080/agents/abc123/file/download \
  -H "Content-Type: application/json" \
  -d '{"path":"/var/log/app.log","offset":1024,"length":1024}' \
  -o part.bin
```

### Browse

`file/browse` performs one file operation per request. It shares `allowed_paths` and the same management-signature authorization as file transfer. The request body is JSON:

| Field | Used by | Description |
|-------|---------|-------------|
| `action` | all | `list` (default), `stat`, `roots`, `chmod`, `delete`, `mkdir`, or `rename` |
| `path` | all except `roots` | Path on the target; for `rename`, the source |
| `new_path` | `rename` | Destination path |
| `offset` | `list` | Number of entries to skip; default `0` |
| `limit` | `list` | Maximum entries to return; default `100`, maximum `5000` |
| `mode` | `chmod` | Octal permission bits up to `0777`, for example `"0755"` |
| `recursive` | `delete` | Required to delete a directory that is not empty |

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

Entries are sorted with directories first, then files, alphabetically by name. `total` counts the entries before `offset` and `limit` are applied, and `truncated` is `true` when more entries follow. Symbolic links include `is_symlink` and `link_target`; their `size` and `is_dir` describe the link target. For a broken link, or a link that points outside `allowed_paths`, they describe the link itself.

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
{ "total": 0, "truncated": false, "roots": ["/tmp", "/data"], "home": "/home/muti" }
```

Glob patterns such as `/data/**` are reduced to their base directory. `home` is the home directory of the user the target agent runs as. When `allowed_paths: ["*"]`, `roots` is `["/"]` and the response includes `"wildcard": true`.

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

Files, symlinks, and empty directories are deleted directly. Non-empty directories require `"recursive": true`; without it, the request is rejected with `not_empty`.

**Create a directory:**

```bash
curl -X POST http://localhost:8080/agents/abc123/file/browse \
  -H "Content-Type: application/json" \
  -d '{"action":"mkdir","path":"/tmp/reports"}'
```

`mkdir` creates one directory with mode `0700`. The parent must exist (`not_found` otherwise), and the path must not (`already_exists`).

**Rename or move:**

```bash
curl -X POST http://localhost:8080/agents/abc123/file/browse \
  -H "Content-Type: application/json" \
  -d '{"action":"rename","path":"/tmp/draft.txt","new_path":"/tmp/reports/final.txt"}'
```

`rename` moves a file, directory, or symbolic link to `new_path` within the allowed paths. The destination must not exist (`already_exists`); delete it first to replace it.

`stat`, `chmod`, `delete`, `mkdir`, and `rename` return a single `entry` in the same shape as a list entry: the entry after the change, or for `delete`, as it was before deletion.

### Browse Errors

Errors reported by the target keep the response shape and add `error`, plus `error_code` where the cause has a stable code:

```json
{
  "total": 0,
  "truncated": false,
  "error": "path not in allowed list: /etc",
  "error_code": "not_allowed"
}
```

| `error_code` | Cause |
|--------------|-------|
| `not_found` | The path does not exist (for `mkdir`, its parent; for `rename`, the source or the destination's parent) |
| `already_exists` | The `mkdir` path or `rename` destination exists |
| `not_allowed` | The path is outside `allowed_paths`, directly or through a symbolic link |
| `not_empty` | `delete` of a non-empty directory without `recursive` |

Signature failures return `auth_required` (HTTP 401, the target has no signing public key or the signature is missing) or `invalid_credentials` (HTTP 403, the target rejected the signature).

## Implementation Details

- **Streaming**: Files transferred in 16KB chunks
- **No size limits**: Stream directly without memory buffering
- **Directories**: Automatically tar/gzip with permission preservation
- **Permissions**: Downloads and files inside uploaded directories keep their mode (Unix). A single uploaded file is created with mode `0600` unless the upload sets `mode`; an overwritten file keeps its mode.

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

### Signature Rejected

```
invalid_credentials
```

The gateway's signing private key does not match the public key on the target, the two hosts' clocks differ by more than five minutes, or one of the two agents runs a version before 4.12.0. Compare the keys with `muti-metroo signing-key public`, synchronize clocks, and upgrade both agents.

### Transfer Stalls

For large transfers over slow links, use rate limiting to prevent timeouts:

```bash
muti-metroo download --rate-limit 500KB --timeout 3600 abc123 /data/large.file ./large.file
```
