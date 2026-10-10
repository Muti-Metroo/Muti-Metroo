---
title: File Transfer Endpoints
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-wiring.png" alt="Mole transferring files" style={{maxWidth: '180px'}} />
</div>

# File Transfer Endpoints

Move files to and from any agent in your mesh. Upload configs, download logs, or transfer entire directories.

**Quick examples:**
```bash
# Upload a file
curl -X POST http://localhost:8080/agents/abc123/file/upload \
  -F "file=@./config.yaml" \
  -F "path=/tmp/config.yaml"

# Download a file
curl -X POST http://localhost:8080/agents/abc123/file/download \
  -d '{"path":"/var/log/app.log"}' -o app.log
```

## POST /agents/\{agent-id\}/file/upload

Upload file or directory to remote agent.

**Content-Type:** `multipart/form-data`

**Authorization:** All file/upload, file/download, and file/browse requests require a valid mesh management Ed25519 signature. The local agent serving the request must have `management.signing_private_key` set; the target agent must have `management.signing_public_key` set. Requests without a signature return `401 auth_required`; requests with an invalid signature return `403 invalid_credentials`. See [Authentication](/security/authentication) for details. There is no `password` field on these requests.

**Form Fields:**
- `file`: File to upload (required)
- `path`: Remote destination path (required)
- `directory`: "true" if uploading directory tar (optional)
- `rate_limit`: Max transfer speed in bytes/second (optional)
- `resume`: "true" to resume an interrupted transfer (optional)

**Response:**
```json
{
  "success": true,
  "bytes_written": 1024,
  "filename": "myfile.txt",
  "remote_path": "/tmp/myfile.txt"
}
```

**Example:**
```bash
curl -X POST http://localhost:8080/agents/abc123/file/upload \
  -F "file=@./data.bin" \
  -F "path=/tmp/data.bin"
```

The CLI (`muti-metroo upload`) handles signing automatically. For raw HTTP calls, use the CLI's `--debug` log to inspect the signed envelope format if you need to integrate from another tool.

## POST /agents/\{agent-id\}/file/download

Download file or directory from remote agent.

**Request:**
```json
{
  "path": "/tmp/myfile.txt",
  "rate_limit": 1048576,
  "offset": 0,
  "original_size": 0
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `path` | string | Yes | Remote file path to download |
| `rate_limit` | int64 | No | Max transfer speed in bytes/second (0 = unlimited) |
| `offset` | int64 | No | Resume from byte offset |
| `original_size` | int64 | No | Expected file size for resume validation |

The same management-signature authorization as upload applies.

**Response:** Binary file data

**Headers:**
- `Content-Type`: `application/octet-stream` (file) or `application/gzip` (directory)
- `Content-Disposition`: Filename
- `X-File-Mode`: File permissions (octal, e.g., "0644")
- `X-Original-Size`: Original uncompressed file size in bytes

**Example:**
```bash
curl -X POST http://localhost:8080/agents/abc123/file/download \
  -H "Content-Type: application/json" \
  -d '{"path":"/tmp/data.bin"}' \
  -o data.bin
```

## POST /agents/\{agent-id\}/file/browse

Browse the filesystem on a remote agent. Supports directory listing, file stat, and discovering browsable root paths. Shares the `allowed_paths` configuration with file transfer and uses the same management-signature authorization.

### Action: list

List directory contents with pagination.

**Request:**
```json
{
  "action": "list",
  "path": "/tmp",
  "offset": 0,
  "limit": 100
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `action` | string | No | `"list"` (default), `"stat"`, `"roots"`, `"chmod"`, or `"delete"` |
| `path` | string | Yes | Directory path to list |
| `offset` | int | No | Pagination offset (default 0) |
| `limit` | int | No | Max entries to return (default 100, max 200) |

**Response:**
```json
{
  "path": "/tmp",
  "entries": [
    { "name": "subdir", "size": 4096, "mode": "0755", "mod_time": "2026-02-17T08:00:00Z", "is_dir": true },
    { "name": "file.txt", "size": 1024, "mode": "0644", "mod_time": "2026-02-18T10:30:00Z", "is_dir": false },
    { "name": "link", "size": 12, "mode": "0777", "mod_time": "2026-02-16T12:00:00Z", "is_dir": false, "is_symlink": true, "link_target": "/etc/hosts" }
  ],
  "total": 3,
  "truncated": false
}
```

Entries are sorted with directories first, then files, alphabetically by name within each group. Symlinks include `is_symlink` and `link_target` fields, with size and `is_dir` resolved from the symlink target. For broken symlinks, size and mode reflect the symlink itself rather than the missing target.

### Action: stat

Get info about a single path.

**Request:**
```json
{ "action": "stat", "path": "/tmp/file.txt" }
```

**Response:**
```json
{
  "path": "/tmp/file.txt",
  "entry": { "name": "file.txt", "size": 1024, "mode": "0644", "mod_time": "2026-02-18T10:30:00Z", "is_dir": false }
}
```

### Action: chmod

Change file permissions on a remote agent.

**Request:**
```json
{
  "action": "chmod",
  "path": "/tmp/script.sh",
  "mode": "0755"
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `action` | string | Yes | `"chmod"` |
| `path` | string | Yes | File or directory path |
| `mode` | string | Yes | Octal permission string (e.g. `"0755"`, `"0644"`) |

Mode must be a valid octal value up to `0777`. Returns the updated file entry (same format as `stat`).

**Response:**
```json
{
  "path": "/tmp/script.sh",
  "entry": { "name": "script.sh", "size": 1024, "mode": "0755", "mod_time": "2026-02-18T10:30:00Z", "is_dir": false }
}
```

### Action: delete

Delete a file or directory on a remote agent.

**Request:**
```json
{
  "action": "delete",
  "path": "/tmp/old-config.yaml"
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `action` | string | Yes | `"delete"` |
| `path` | string | Yes | File or directory path to delete |
| `recursive` | bool | No | Required for non-empty directories (default `false`) |

Files, symlinks, and empty directories are deleted directly. Non-empty directories require `recursive: true` -- without it, the request is rejected. This mirrors standard `rm` / `rm -r` behavior.

The response returns the entry info captured before deletion, confirming what was removed.

**Response:**
```json
{
  "path": "/tmp/old-config.yaml",
  "entry": { "name": "old-config.yaml", "size": 1024, "mode": "0644", "mod_time": "2026-02-18T10:30:00Z", "is_dir": false }
}
```

**Delete a non-empty directory:**
```json
{
  "action": "delete",
  "path": "/tmp/old-logs",
  "recursive": true
}
```

### Action: roots

Discover browsable root paths from the `allowed_paths` configuration.

**Request:**
```json
{ "action": "roots" }
```

**Response:**
```json
{ "roots": ["/tmp", "/data"] }
```

When `allowed_paths: ["*"]`, the response is `{ "roots": ["/"], "wildcard": true }`. Glob patterns like `/data/**` are normalized to their base directory `/data`.

**Example:**
```bash
# List directory
curl -X POST http://localhost:8080/agents/abc123/file/browse \
  -H "Content-Type: application/json" \
  -d '{"action":"list","path":"/tmp"}'

# Stat a file
curl -X POST http://localhost:8080/agents/abc123/file/browse \
  -H "Content-Type: application/json" \
  -d '{"action":"stat","path":"/tmp/config.yaml"}'

# Get browsable roots
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

### Errors

| HTTP Status | Body | Cause |
|-------------|------|-------|
| 401 | `{"error_code":"auth_required", ...}` | Request was not signed with a management key |
| 403 | `{"error_code":"invalid_credentials", ...}` | Signature did not verify against the target's `management.signing_public_key` |
| 405 | Method Not Allowed | Request used GET instead of POST |
| 400 | `{"error": "..."}` | Invalid path or unknown action |
| 503 | `{"error": "file browsing not configured"}` | File transfer is disabled on the target agent |

## Implementation Notes

- Files are streamed in 16KB chunks
- No inherent size limits
- Directories are automatically tar/gzip compressed
- File permissions are preserved

### Rate Limiting

When `rate_limit` is set, the sending agent throttles the transfer to the specified bytes per second. The rate limiter uses a token bucket algorithm with a 16KB burst size.

### Resume Transfers

To resume an interrupted transfer:

1. Set `offset` to the number of bytes already received
2. Set `original_size` to the expected total file size

The remote agent validates that the file size matches `original_size`. If the file was modified (size changed), the transfer fails with error code `ErrResumeFailed` (19).

Resume is not supported for directory transfers (tar archives).

## Security

- Requires `file_transfer.enabled: true` on the target agent
- Path restrictions via `file_transfer.allowed_paths`
- Authorization via mesh management Ed25519 signing keys (see [Authentication](/security/authentication))

## See Also

- [File Transfer Feature](/features/file-transfer) - Feature overview
- [CLI - File Transfer](/cli/file-transfer) - CLI reference
- [File Transfer Configuration](/configuration/file-transfer) - Configuration options
