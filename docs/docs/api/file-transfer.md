---
title: File Transfer Endpoints
description: Upload, download, list, stat, chmod, and delete files on remote agents over the mesh.
sidebar_position: 13
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-wiring.png" alt="Mole transferring files" style={{maxWidth: '180px'}} />
</div>

# File Transfer Endpoints

These endpoints move files to and from a remote agent and manage files on it. The [`muti-metroo upload`, `download`](/cli/file-transfer) commands and Muti Metroo Manager's file browser are clients of these endpoints. They belong to the remote API group (`http.remote_api`, enabled by default) and accept only `POST`.

| Endpoint | Description |
| -------- | ----------- |
| `POST /agents/{id}/file/upload` | Upload a file or directory |
| `POST /agents/{id}/file/download` | Download a file or directory |
| `POST /agents/{id}/file/browse` | List directories, inspect, chmod, and delete files |

## Requirements

- The agent serving the request has `management.signing_private_key`. It signs every operation on the client's behalf; clients do not send credentials beyond the bearer token.
- The target agent has `management.signing_public_key` and `file_transfer.enabled: true`, and every path is inside `file_transfer.allowed_paths`. Uploads are limited by `file_transfer.max_file_size`. See [File Transfer Configuration](/configuration/file-transfer).
- `{id}` is a remote agent. Upload and download do not work with the local agent's own ID; `file/browse` with the local ID is answered by the local agent.

Paths are absolute paths on the target. A leading `~` is expanded to the home directory of the user the target agent runs as.

## `POST /agents/{id}/file/upload`

Uploads a file as `multipart/form-data`.

| Form field | Required | Description |
| ---------- | -------- | ----------- |
| `file` | Yes | File content |
| `path` | Yes | Destination path on the target |
| `directory` | No | `true` when `file` is a tar archive (optionally gzip-compressed) to unpack into `path` as a directory |
| `rate_limit` | No | Maximum transfer rate in bytes per second; `0` or omitted means unlimited |

```bash
curl -X POST http://localhost:8080/agents/331974cd4a0be0d7f945ed9d367a0211/file/upload \
  -F "file=@./app.conf" \
  -F "path=/srv/app/app.conf"
```

**Response (200):**

```json
{
  "bytes_written": 10,
  "filename": "app.conf",
  "remote_path": "/srv/app/app.conf",
  "success": true
}
```

`bytes_written` is the size received by the serving agent. Upload a directory by packing it first:

```bash
tar czf site.tar.gz -C ./site .
curl -X POST http://localhost:8080/agents/331974cd4a0be0d7f945ed9d367a0211/file/upload \
  -F "file=@./site.tar.gz" \
  -F "path=/srv/site" \
  -F "directory=true"
```

## `POST /agents/{id}/file/download`

Downloads a file or directory. The request body is JSON.

| Field | Type | Required | Description |
| ----- | ---- | -------- | ----------- |
| `path` | string | Yes | Path on the target |
| `rate_limit` | integer | No | Maximum transfer rate in bytes per second |
| `offset` | integer | No | Resume a file download from this byte offset |
| `original_size` | integer | No | Expected total size when resuming; the download fails if the file has changed size |

```bash
curl -X POST http://localhost:8080/agents/331974cd4a0be0d7f945ed9d367a0211/file/download \
  -H "Content-Type: application/json" \
  -d '{"path": "/var/log/app.log"}' \
  -o app.log
```

A file is returned as the raw content:

```text
HTTP/1.1 200 OK
Content-Disposition: attachment; filename="app.log"
Content-Type: application/octet-stream
X-File-Mode: 0644
X-Original-Size: 52817
```

A directory is returned as a gzip-compressed tar archive named after the directory:

```text
HTTP/1.1 200 OK
Content-Disposition: attachment; filename="logs.tar.gz"
Content-Type: application/gzip
```

| Header | Description |
| ------ | ----------- |
| `X-File-Mode` | Permission bits of the file on the target, in octal |
| `X-Original-Size` | File size in bytes on the target |

Resuming is supported for files, not directories. Upload and download time out after 30 minutes.

### Upload and download errors

| Status | Body | Cause |
| ------ | ---- | ----- |
| 400 | `missing required field: path` | No `path` |
| 400 | `failed to parse multipart form: ...` or `invalid JSON: ...` | Malformed request |
| 400 | `invalid agent ID format` | `{id}` is not a full agent ID |
| 401 | `{"success": false, "code": "auth_required", "error": "..."}` | The target has no signing public key or the signature is missing |
| 403 | `{"success": false, "code": "invalid_credentials", "error": "..."}` | The target rejected the signature |
| 405 | `method not allowed` | Method other than `POST` |
| 502 | `{"success": false, "error": "remote error: path not in allowed list: /etc/passwd"}` | The target refused the operation |
| 502 | `{"success": false, "error": "no route to agent ..."}` | The target is unknown or unreachable |
| 502 | `{"success": false, "error": "management signing private key not configured (set management.signing_private_key)"}` | The serving agent has no signing private key |

## `POST /agents/{id}/file/browse`

Performs one file operation per request. The request body is JSON with an `action` field.

| Field | Type | Used by | Description |
| ----- | ---- | ------- | ----------- |
| `action` | string | all | `list` (default), `stat`, `roots`, `chmod`, or `delete` |
| `path` | string | all except `roots` | Path on the target |
| `offset` | integer | `list` | Number of entries to skip; default `0` |
| `limit` | integer | `list` | Maximum entries to return; default `100`, maximum `5000` |
| `mode` | string | `chmod` | Octal permission bits up to `0777`, for example `"0755"` |
| `recursive` | boolean | `delete` | Required to delete a directory that is not empty |

Every response contains `total` and `truncated`; the other fields depend on the action.

### `roots`

Returns the directories a client can start browsing from, derived from `file_transfer.allowed_paths`. Glob patterns such as `/data/**` are reduced to their base directory. When `allowed_paths` is `["*"]`, `roots` is `["/"]` and `wildcard` is `true`.

```bash
curl -X POST http://localhost:8080/agents/331974cd4a0be0d7f945ed9d367a0211/file/browse \
  -H "Content-Type: application/json" \
  -d '{"action": "roots"}'
```

```json
{
  "total": 0,
  "truncated": false,
  "roots": ["/srv/files"],
  "home": "/home/muti"
}
```

`home` is the home directory of the user the target agent runs as.

### `list`

Lists a directory, directories first, then files, each group sorted by name.

```bash
curl -X POST http://localhost:8080/agents/331974cd4a0be0d7f945ed9d367a0211/file/browse \
  -H "Content-Type: application/json" \
  -d '{"action": "list", "path": "/srv/files"}'
```

```json
{
  "path": "/srv/files",
  "entries": [
    {
      "name": "sub",
      "size": 96,
      "mode": "0755",
      "mod_time": "2026-10-11T11:51:41Z",
      "is_dir": true
    },
    {
      "name": "hello.txt",
      "size": 6,
      "mode": "0644",
      "mod_time": "2026-10-11T11:51:41Z",
      "is_dir": false
    },
    {
      "name": "latest",
      "size": 6,
      "mode": "0777",
      "mod_time": "2026-10-11T11:52:10Z",
      "is_dir": false,
      "is_symlink": true,
      "link_target": "hello.txt"
    }
  ],
  "total": 3,
  "truncated": false
}
```

| Field | Description |
| ----- | ----------- |
| `total` | Entries in the directory before `offset` and `limit` are applied |
| `truncated` | `true` when more entries follow; request again with a larger `offset` |
| `entries[].name` | File name |
| `entries[].size` | Size in bytes |
| `entries[].mode` | Permission bits in octal |
| `entries[].mod_time` | Modification time (UTC) |
| `entries[].is_dir` | `true` for directories |
| `entries[].is_symlink`, `entries[].link_target` | Present for symbolic links. `size` and `is_dir` describe the link target; for a broken link they describe the link itself. |

### `stat`

Returns a single entry.

```bash
curl -X POST http://localhost:8080/agents/331974cd4a0be0d7f945ed9d367a0211/file/browse \
  -H "Content-Type: application/json" \
  -d '{"action": "stat", "path": "/srv/files/hello.txt"}'
```

```json
{
  "path": "/srv/files/hello.txt",
  "total": 0,
  "truncated": false,
  "entry": {
    "name": "hello.txt",
    "size": 6,
    "mode": "0644",
    "mod_time": "2026-10-11T11:51:41Z",
    "is_dir": false
  }
}
```

### `chmod`

Changes permission bits and returns the updated entry in the same shape as `stat`.

```bash
curl -X POST http://localhost:8080/agents/331974cd4a0be0d7f945ed9d367a0211/file/browse \
  -H "Content-Type: application/json" \
  -d '{"action": "chmod", "path": "/srv/files/hello.txt", "mode": "0600"}'
```

### `delete`

Deletes a file, symbolic link, or empty directory, and returns the entry as it was before deletion in the same shape as `stat`. A directory that is not empty needs `"recursive": true`.

```bash
curl -X POST http://localhost:8080/agents/331974cd4a0be0d7f945ed9d367a0211/file/browse \
  -H "Content-Type: application/json" \
  -d '{"action": "delete", "path": "/srv/files/sub", "recursive": true}'
```

### Browse errors

Errors reported by the target keep the response shape and add `error`, plus `error_code` for signature failures:

```json
{
  "total": 0,
  "truncated": false,
  "error": "path not in allowed list: /etc"
}
```

| Status | Body | Cause |
| ------ | ---- | ----- |
| 400 | `error`: `path not in allowed list: ...`, `path not found: ...`, `not a directory: ...`, `directory is not empty (use recursive to delete)`, `unknown action: ...`, `file transfer is disabled` | The target refused the operation |
| 401 | `error_code`: `auth_required` | The target has no signing public key or the signature is missing |
| 403 | `error_code`: `invalid_credentials` | The target rejected the signature |
| 403 | `management signing private key not configured on this agent` (plain text) | The serving agent has no signing private key |
| 403 | `file browse restricted: management key decryption unavailable` (plain text) | `management.public_key` is set without `management.private_key` |
| 405 | `method not allowed` | Method other than `POST` |
| 502 | `failed to send request: <reason>` (plain text) | The target is unknown or unreachable |

## See Also

- [File Transfer](/features/file-transfer): feature overview
- [upload and download CLI](/cli/file-transfer): command-line clients
- [File Transfer Configuration](/configuration/file-transfer): enable transfers and set allowed paths
- [Authentication](/security/authentication): management signing keys
