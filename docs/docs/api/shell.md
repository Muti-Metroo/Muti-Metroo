---
title: Management Commands WebSocket API
description: WebSocket endpoint and message format for running management commands on remote agents.
sidebar_position: 11
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-drilling.png" alt="Mole running management commands" style={{maxWidth: '180px'}} />
</div>

# Management Commands WebSocket API

The shell endpoint runs a command on a remote agent and streams its input and output over a WebSocket. The [`muti-metroo shell`](/cli/shell) command is a client of this endpoint; this page is for building other clients.

## `GET /agents/{id}/shell`

```text
ws://localhost:8080/agents/331974cd4a0be0d7f945ed9d367a0211/shell?mode=stream
```

| Item | Value |
| ---- | ----- |
| Group | Remote API (`http.remote_api`) |
| Subprotocol | `muti-metroo-shell` |
| Query parameter `mode` | `stream`: run without a terminal, stdout and stderr separate. Any other value, or no value: interactive session with a pseudo-terminal. |
| Query parameter `token` | Bearer token, for clients that cannot send an `Authorization` header |
| Target | A remote agent; the local agent's own ID is not reachable |

### Requirements

- The agent serving the request has `management.signing_private_key`. It signs every session on the client's behalf; any `auth` value the client sends is replaced.
- The target agent has `management.signing_public_key`, `shell.enabled: true`, and the command in `shell.whitelist`. See [Management Commands Configuration](/configuration/shell).

## Message format

Every WebSocket message is binary. The first byte is the message type; the rest is the payload.

| Byte | Name | Direction | Payload |
| ---- | ---- | --------- | ------- |
| `0x01` | META | client to server | JSON session request; must be the first message |
| `0x02` | ACK | server to client | JSON `{"success": true}` or `{"success": false, "error": "..."}` |
| `0x03` | STDIN | client to server | Raw input bytes |
| `0x04` | STDOUT | server to client | Raw output bytes |
| `0x05` | STDERR | server to client | Raw error output bytes (stream mode only) |
| `0x06` | RESIZE | client to server | 4 bytes: rows and columns, each a big-endian uint16 |
| `0x07` | SIGNAL | client to server | 1 byte: signal number, for example `2` for SIGINT |
| `0x08` | EXIT | server to client | 4 bytes: exit code, big-endian int32 |
| `0x09` | ERROR | server to client | JSON `{"message": "...", "code": 21}`; `code` is optional |
| `0x0a` | STDIN_CLOSE | client to server | No payload. Closes the command's standard input so commands that read until end of input (`cat`, `wc`, `sort`) can finish. |

### META payload

```json
{
  "command": "bash",
  "args": ["-l"],
  "env": {"LANG": "C.UTF-8"},
  "work_dir": "/tmp",
  "tty": {"rows": 24, "cols": 80, "term": "xterm-256color"},
  "timeout": 3600
}
```

| Field | Type | Description |
| ----- | ---- | ----------- |
| `command` | string | Command to run; required |
| `args` | string[] | Arguments |
| `env` | object | Extra environment variables |
| `work_dir` | string | Working directory |
| `tty` | object | Terminal size and `TERM` (default `xterm-256color`); set it for interactive sessions |
| `timeout` | integer | Session timeout in seconds; `0` or omitted means no timeout |

## Session flow

1. Open the WebSocket with the `muti-metroo-shell` subprotocol.
2. Send META.
3. Read the first message. ACK with `success: true` means the command started. ERROR or ACK with `success: false` means it did not; the server then closes the WebSocket.
4. Exchange STDIN, STDOUT, STDERR, RESIZE, SIGNAL, and STDIN_CLOSE messages.
5. When the command exits, the server sends EXIT and closes the WebSocket.

## Errors

Errors after the WebSocket is open arrive as ERROR messages:

| `code` | Meaning |
| ------ | ------- |
| `21` | Missing signature, or the target has no `management.signing_public_key` |
| `22` | Signature rejected: wrong key, wrong target, or expired |
| omitted | Any other failure, for example the command is not in the whitelist, the session limit is reached, or the target has management commands disabled |

Errors before the upgrade are plain HTTP responses: `401` for a missing bearer token, `503 shell not available` when the agent has no shell support, and `400` for an invalid agent ID or a failed upgrade.

WebSocket close codes:

| Code | Meaning |
| ---- | ------- |
| 1000 | Normal closure |
| 1002 | Protocol error: the first message was not a valid META |
| 1011 | The session could not be opened or ended unexpectedly |

## See Also

- [Management Commands](/features/shell) - Feature overview
- [CLI - shell](/cli/shell) - Command-line client
- [Management Commands Configuration](/configuration/shell) - Enable commands and set the whitelist
- [Authentication](/security/authentication) - Management signing keys
