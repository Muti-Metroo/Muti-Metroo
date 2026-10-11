---
title: File Transfer
description: Upload and download files and directories to and from remote agents, limit transfer speed, resume interrupted downloads, and browse remote directories.
sidebar_position: 8
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-presenting.png" alt="Mole presenting file transfer" style={{maxWidth: '180px'}} />
</div>

# File Transfer

`muti-metroo upload` and `muti-metroo download` copy files and whole directories between your machine and any agent in the mesh. The transfer goes through your local agent (the gateway) and the mesh to the target agent, end-to-end encrypted between the two.

```bash
# Fetch a log file from a remote agent
muti-metroo download 1f743225 /var/log/app/app.log ./app.log

# Copy a script to a remote agent
muti-metroo upload 1f743225 ./deploy.sh /tmp/deploy.sh

# Copy a directory
muti-metroo upload 1f743225 ./tools /opt/tools
```

## Requirements

File transfer is disabled by default. It needs settings on both agents:

| Agent | Setting |
| ----- | ------- |
| Target agent | `file_transfer.enabled: true` |
| Target agent | `file_transfer.allowed_paths` listing the directories that may be read or written |
| Target agent | `management.signing_public_key` |
| Gateway agent (the one `-a` points at, default `localhost:8080`) | `management.signing_private_key` |

The gateway agent signs each request and the target verifies the signature. There is no separate file-transfer password. See [File Transfer Configuration](/configuration/file-transfer) for `allowed_paths` patterns and size limits, and [Authentication](/security/authentication#management-signing-key) for the signing key pair.

```yaml
# Target agent
file_transfer:
  enabled: true
  allowed_paths:
    - /tmp
    - /var/log/app
management:
  signing_public_key: "86d6714fc48cd41afd5051e6ad9523ad889c806001e92bc739913a18c6709a83"
```

## Upload and download

```bash
muti-metroo upload [flags] <target-agent-id> <local-path> <remote-path>
muti-metroo download [flags] <target-agent-id> <remote-path> <local-path>
```

- The remote path must be absolute and inside an `allowed_paths` entry on the target.
- The target agent ID can be a unique prefix of the full ID.
- Directories are detected automatically and sent as a compressed tar archive, then unpacked at the destination.
- File permissions are preserved.
- Files are streamed, so they do not have to fit in memory. The target's `max_file_size` (default 500 MB) caps a single transfer.

Use `-q` to suppress progress output and `-w` to change the transfer timeout (default `5m`). All flags are listed in [File Transfer CLI](/cli/file-transfer).

### Limit transfer speed

`--rate-limit` caps the transfer rate so a large file does not saturate a slow link:

```bash
muti-metroo upload --rate-limit 100KB 1f743225 ./large.iso /tmp/large.iso
muti-metroo download --rate-limit 1MiB 1f743225 /data/backup.tar.gz ./backup.tar.gz
```

Sizes accept decimal units (`100KB`, `1MB`; 1 KB = 1000 bytes), binary units (`100KiB`, `1MiB`; 1 KiB = 1024 bytes), or plain bytes (`1024000`). The sending side enforces the limit: your gateway agent for uploads, the target agent for downloads.

### Resume an interrupted download

```bash
muti-metroo download --resume 1f743225 /data/large.iso ./large.iso
```

While a download runs, data is written to `<local-path>.partial` and progress to `<local-path>.partial.json`. With `--resume`, the download continues from the end of the partial file. If the remote file's size has changed since the first attempt, the download is rejected instead of being resumed. When the download completes, the partial file is renamed to the final name.

Resume does not apply to directories, and uploads are always sent again from the beginning.

## Browse remote directories

The HTTP API can list, inspect, change permissions of, and delete files on a target agent. It uses the same `allowed_paths` and signing key as file transfer. There is no CLI command for it.

```bash
curl -s -X POST http://localhost:8080/agents/1f743225d524f8acd3b42b55639702f0/file/browse \
  -H "Content-Type: application/json" \
  -d '{"action":"list","path":"/var/log/app"}'
```

```json
{"path":"/var/log/app","entries":[{"name":"app.log","size":18874368,"mode":"0644","mod_time":"2026-10-11T11:51:24Z","is_dir":false}],"total":1,"truncated":false}
```

The actions are `roots` (the browsable root paths), `list`, `stat`, `chmod`, and `delete`. See [File Transfer API](/api/file-transfer) for request and response fields, pagination, and recursive deletes.

## Troubleshooting

### path not in allowed list

```text
Error: download failed: remote error: path not in allowed list: /etc/shadow
```

The path is outside every `allowed_paths` entry on the target agent, or `allowed_paths` is empty.

### management signing private key not configured on this agent

The gateway agent has no `management.signing_private_key`. Point `-a` at an operator agent that holds the private key, or add it to the gateway's configuration.

### management signing public key not configured on this agent

The target agent has no `management.signing_public_key`, so it rejects every file transfer. Add the public half of the key pair to its configuration and restart it.

### Signature rejected

The gateway's private key does not match the public key on the target, or the two hosts' clocks differ by more than five minutes. Compare the keys with `muti-metroo signing-key public` and synchronize clocks.

### file too large

```text
Error: upload failed: remote error: file too large: 734003200 bytes (max 524288000)
```

The file exceeds `file_transfer.max_file_size` on the target agent. Raise the limit (`0` means unlimited) or split the file.

### no route to agent

The gateway agent has no path to the target. Check `muti-metroo peer status` and `muti-metroo mesh-test`.

## See Also

- [File Transfer CLI](/cli/file-transfer): every flag of `upload` and `download`
- [File Transfer Configuration](/configuration/file-transfer): `allowed_paths` patterns and size limits
- [File Transfer API](/api/file-transfer): upload, download, and browse endpoints
- [Authentication](/security/authentication): the signing key pair that authorizes transfers
- [Access Control](/security/access-control#file-transfer-paths): how path restrictions are enforced
