---
title: muti-metroo upload and download
sidebar_label: upload / download
sidebar_position: 23
description: Copy files and directories to and from a remote agent through the mesh with the upload and download commands.
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-reading.png" alt="Mole transferring files" style={{maxWidth: '180px'}} />
</div>

# muti-metroo upload and download

`upload` copies a local file or directory to a remote agent, and `download` copies a file or directory from a remote agent to the local machine. Data streams through the mesh without being held in memory. Directories are detected automatically and sent as a gzip-compressed tar archive, which is unpacked on the receiving side.

Requirements:

- The gateway agent (`--agent`) has the HTTP API with remote endpoints enabled (`http.remote_api`) and holds the mesh signing private key (`management.signing_private_key`).
- The target agent has `management.signing_public_key` set, file transfer enabled (`file_transfer.enabled: true`), and the remote path inside `file_transfer.allowed_paths`. Single files larger than `file_transfer.max_file_size` (500 MB by default) are rejected.

See [File transfer configuration](../configuration/file-transfer.md) and [Authentication](../security/authentication.md).

Global flags (`--token`, `--yes`, `--no-color`) are described in [CLI overview](overview.md#global-flags).

Both commands take the target agent as a full 32-character agent ID or a unique prefix of one, resolved against the gateway agent's list of known agents. The remote path must be absolute: it starts with `/`, or with a drive letter such as `C:\` or `C:/` for Windows targets.

## upload

```bash
muti-metroo upload [flags] <target-agent-id> <local-path> <remote-path>
```

Uploads `<local-path>` to `<remote-path>` on the target agent. `<remote-path>` is the full destination path, not a directory to copy into. For a directory, the contents of `<local-path>` are unpacked into `<remote-path>`, and file modes inside the directory are preserved.

### Flags

| Flag | Default | Description |
| --- | --- | --- |
| `-a`, `--agent` | `localhost:8080` | Gateway agent HTTP API address, as `host:port` or a URL. See [Agent address](overview.md#agent-address). |
| `-w`, `--timeout` | `5m` | Maximum time for the whole transfer, as a Go duration (`30s`, `1h`) or plain seconds. Set it higher for large files on slow links. |
| `--rate-limit` | none | Maximum transfer speed, such as `100KB`, `1MB`, or `10MiB`. `KB` and `MB` are powers of 1000; `KiB` and `MiB` are powers of 1024. |
| `--resume` | `false` | Accepted for symmetry with `download`, but an upload always starts from the beginning. Ignored for directories, with a warning. |
| `-q`, `--quiet` | `false` | Do not print progress or the summary line. |

`-t` is a deprecated alias for `--timeout`; it still works but prints a warning to stderr. See [Deprecated commands and flags](overview.md#deprecated-commands-and-flags).

### Output

```bash
muti-metroo upload abc123de ./data.bin /tmp/data.bin
```

```text
Uploading data.bin (3.0 MB) to abc123def456:/tmp/data.bin
Uploaded 3.0 MB to /tmp/data.bin in 250ms (12 MB/s)
```

While a file is transferring, a progress bar with percentage, speed, and estimated time is shown on one line and cleared at the end. Directory uploads show no progress bar, and the reported size is the size of the compressed archive.

### Examples

```bash
# Upload a file
muti-metroo upload abc123de ./config.yaml /etc/app/config.yaml

# Upload a directory
muti-metroo upload abc123de ./my-folder /tmp/my-folder

# Upload to a Windows target
muti-metroo upload abc123de ./tool.exe 'C:\Temp\tool.exe'

# Through a different gateway agent
muti-metroo upload -a 192.168.1.10:8080 abc123de ./data.txt /tmp/data.txt

# Limit to 100 KB/s and allow up to an hour
muti-metroo upload --rate-limit 100KB -w 1h abc123de ./large.iso /tmp/large.iso
```

## download

```bash
muti-metroo download [flags] <target-agent-id> <remote-path> <local-path>
```

Downloads `<remote-path>` from the target agent to `<local-path>`. `<local-path>` is the full destination path; missing parent directories are created and an existing file is overwritten. A single file keeps the file mode it has on the remote agent. A remote directory is unpacked into `<local-path>`, which is created if needed.

A file is written to `<local-path>.partial` while it downloads, with progress tracked in `<local-path>.partial.json`, and renamed to `<local-path>` when complete. If the download is interrupted, these files stay in place and `--resume` continues from where it stopped.

### Flags

| Flag | Default | Description |
| --- | --- | --- |
| `-a`, `--agent` | `localhost:8080` | Gateway agent HTTP API address, as `host:port` or a URL. See [Agent address](overview.md#agent-address). |
| `-w`, `--timeout` | `5m` | Maximum time for the whole transfer, as a Go duration (`30s`, `1h`) or plain seconds. Set it higher for large files on slow links. |
| `--rate-limit` | none | Maximum transfer speed, such as `100KB`, `1MB`, or `10MiB`. `KB` and `MB` are powers of 1000; `KiB` and `MiB` are powers of 1024. |
| `--resume` | `false` | Continue an interrupted file download from the existing `.partial` file. Not supported for directories. |
| `-q`, `--quiet` | `false` | Do not print progress. |

`-t` is a deprecated alias for `--timeout`; it still works but prints a warning to stderr. See [Deprecated commands and flags](overview.md#deprecated-commands-and-flags).

### Output

```bash
muti-metroo download abc123de /var/log/app.log ./app.log
```

```text
Downloading abc123def456:/var/log/app.log to /home/user/app.log
Downloading...
Downloaded 3.0 MB to /home/user/app.log in 0.4s (7.5 MB/s)
```

A directory download prints `Downloading... OK` followed by `Extracted directory to <path> in <seconds>s`. A resumed download prints `Resuming from offset <bytes> (of <total>)` and reports how much was added.

### Examples

```bash
# Download a file
muti-metroo download abc123de /etc/app/config.yaml ./config.yaml

# Download a directory
muti-metroo download abc123de /var/log/app ./app-logs

# Limit to 1 MB/s
muti-metroo download --rate-limit 1MB abc123de /data/backup.tar.gz ./backup.tar.gz

# Continue an interrupted download, allowing up to two hours
muti-metroo download --resume -w 2h abc123de /data/large.iso ./large.iso
```

## Exit status

Both commands exit 0 when the transfer completes and 1 on any failure, including `remote path must be absolute`, a path outside `allowed_paths`, a file over the size limit, an unreachable gateway, or a transfer that exceeds `--timeout`. A missing or extra argument exits 2. See [Exit codes](overview.md#exit-codes).

## See Also

- [File transfer](../features/file-transfer.md): how transfers work and how to restrict them
- [File transfer configuration](../configuration/file-transfer.md): `enabled`, `allowed_paths`, and `max_file_size`
- [File transfer HTTP API](../api/file-transfer.md): the endpoints these commands use, including directory browsing
- [Authentication](../security/authentication.md): the signing keys that authorize transfers
- [shell](shell.md): run commands on a remote agent
