---
title: File Transfer
description: Configuration reference for the file_transfer section, which enables remote upload, download, and browsing and controls the allowed paths and maximum file size.
sidebar_position: 19
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-presenting.png" alt="Mole configuring file transfer" style={{maxWidth: '180px'}} />
</div>

# File Transfer

The `file_transfer` section lets operators upload files and directories to this agent, download from it, and browse its file system through the mesh. It is disabled by default. Every request must also be signed with the mesh management signing key, see [Management](/configuration/management#command-signing-keys).

## Configuration

```yaml
file_transfer:
  enabled: true
  max_file_size: 524288000   # 500 MB
  allowed_paths:
    - /tmp
    - /home/*/uploads

management:
  signing_public_key: "91ec6f66440771f9a68f2d65adb835faccdff429e97f9e4be1ce5ac10eec158f"
```

## Keys

| Key             | Type   | Default     | Description                                                                                                             |
| --------------- | ------ | ----------- | ----------------------------------------------------------------------------------------------------------------------- |
| `enabled`       | bool   | `false`     | Accept file transfer and browse requests.                                                                               |
| `max_file_size` | int    | `524288000` | Maximum size of a single uploaded or downloaded file, in bytes (500 MB). `0` means no limit. Directory transfers are not checked against it. |
| `allowed_paths` | list   | `[]`        | Paths that requests may use. Empty allows nothing; `["*"]` allows any absolute path. See [Allowed paths](#allowed-paths). |
| `password_hash` | string | `""`        | Deprecated and ignored. Authorization uses the management signing key. `muti-metroo run` prints a warning when it is set. |

The agent also needs `management.signing_public_key`; without it, every file transfer request is refused.

## Allowed paths

Request paths must be absolute and must not contain `..`. A leading `~` or `~/` is replaced with the home directory of the user the agent runs as before the check, so write `allowed_paths` entries with the expanded path.

Each `allowed_paths` entry is one of:

| Entry form         | Rule                                                                                        |
| ------------------ | ------------------------------------------------------------------------------------------- |
| `*`                | Any absolute path.                                                                          |
| `/dir/**`          | `/dir` and everything below it.                                                             |
| path without glob  | That path and everything below it. `/tmp` allows `/tmp/a/b` but not `/tmpdir`.              |
| path with glob     | `*`, `?`, and `[...]` match within one path element. The entry allows a path when the path itself or any of its parent directories matches. |

Results for some common entries:

| Entry               | Allowed                                         | Refused                              |
| ------------------- | ----------------------------------------------- | ------------------------------------ |
| `/tmp`              | `/tmp`, `/tmp/file.txt`, `/tmp/dir/file`        | `/tmpdir`                            |
| `/tmp/*`            | `/tmp/file.txt`, `/tmp/dir/file`                | `/tmp`                               |
| `/tmp/**`           | `/tmp`, `/tmp/a/b/c`                            | `/tmpdir`                            |
| `/home/*/uploads`   | `/home/alice/uploads`, `/home/alice/uploads/x`  | `/home/uploads`                      |
| `/var/log/*.log`    | `/var/log/syslog.log`                           | `/var/log/nginx/access.log`          |

Because a parent directory match is enough, `/tmp/*` allows everything below `/tmp`, not only its direct children. Entries must be absolute: a relative pattern such as `*.log` matches nothing.

:::danger
`allowed_paths: ["*"]` gives signed callers read and write access to every file the agent's user can access. Use it only on test systems.
:::

## Transfers

Directories are sent as gzip-compressed tar archives, and file permission bits are preserved. The CLI can resume interrupted downloads with `--resume`; see [file transfer CLI](/cli/file-transfer).

## Examples

### Upload staging directory

```yaml
file_transfer:
  enabled: true
  max_file_size: 104857600   # 100 MB
  allowed_paths:
    - /opt/deploy/staging
```

### Log collection

```yaml
file_transfer:
  enabled: true
  allowed_paths:
    - /var/log
```

## See Also

- [File Transfer usage](/features/file-transfer) for upload, download, and browse workflows
- [file transfer CLI](/cli/file-transfer) for `upload` and `download` flags
- [Management](/configuration/management) for the signing keys every request needs
- [Shell](/configuration/shell) for the other remote administration feature
- [File Transfer API](/api/file-transfer) for the HTTP endpoints
