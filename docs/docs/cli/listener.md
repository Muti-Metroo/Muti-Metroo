---
title: muti-metroo listener
sidebar_label: listener
sidebar_position: 15
description: Open, close, and list QUIC, HTTP/2, and WebSocket transport listeners on a running agent.
---

# muti-metroo listener

Open or close a transport listener (the QUIC, HTTP/2, or WebSocket socket that accepts inbound peer connections) on a running agent without editing its configuration file. Use it to add a second ingress on another port or transport, or to expose a plaintext WebSocket endpoint behind a reverse proxy. All subcommands talk to a running agent's HTTP API.

Global flags (`--token`, `--yes`, `--no-color`) are described in [CLI overview](overview.md#global-flags).

## Prerequisites

- The agent at `-a` must have the HTTP API enabled with `http.remote_api` left at its default of `true` (see [HTTP configuration](../configuration/http.md)).
- `listener add` and `listener remove` are mutations. The agent that serves the API (the one `-a` points at) must have `management.signing_private_key` configured, otherwise the request is refused. When `-t` targets a remote agent, the API agent signs the request and the target verifies it with its `management.signing_public_key`. See [Management keys](../configuration/management.md).
- If the API agent has `management.public_key` configured without `management.private_key`, all listener subcommands are refused.

## listener add

```bash
muti-metroo listener add <transport> <address> [flags]
```

Starts a listener on the target agent. `<transport>` is `quic`, `h2`, or `ws`. `<address>` is the bind address, such as `:4434`, `0.0.0.0:4434`, or `127.0.0.1:3002`. HTTP/2 and WebSocket listeners require `--path`.

The listener uses the agent's global TLS certificate and key (or the auto-generated self-signed certificate) and the global mTLS setting. Per-listener TLS overrides are only available in the `listeners:` config section.

Listeners are keyed by address. Running `listener add` again with the address of a dynamic listener replaces it: the old listener stops and a new one starts with the new settings. Addresses that belong to a configured listener cannot be replaced.

To let other agents discover the listener, pass `--advertise-address`. The agent then publishes the dial target to the mesh. Loopback and unspecified hosts are rejected.

### Flags

| Flag | Default | Description |
| --- | --- | --- |
| `-a`, `--agent` | `localhost:8080` | Agent HTTP API address. See [Agent address](overview.md#agent-address). |
| `-t`, `--target` | none | Agent ID (full or unique prefix) of the remote agent that should open the listener. Omit to act on the agent at `-a`. |
| `--path` | none | HTTP path for `h2` and `ws` listeners, such as `/ws/`. Required for those transports. |
| `--plaintext` | `false` | Start a `ws` listener without TLS, for use behind a trusted reverse proxy that terminates TLS. Rejected for `quic` and `h2`. |
| `--advertise-address` | none | Public `host:port` to advertise to the mesh so other agents can dial this listener. |
| `--advertise-path` | value of `--path` | Path to advertise for `h2` and `ws`. |
| `--advertise-transport` | listener transport | Transport to advertise. |

### Output

```text
Listener added: listener 127.0.0.1:18434 added (transport=quic)
```

### Examples

```bash
# Open a QUIC listener on the local agent
muti-metroo listener add quic :4434

# Open an HTTP/2 listener
muti-metroo listener add h2 0.0.0.0:8443 --path /mesh

# Open a plaintext WebSocket listener behind a reverse proxy
muti-metroo listener add ws 127.0.0.1:3002 --path /ws/ --plaintext

# Open a listener and advertise its public dial target
muti-metroo listener add quic :4433 --advertise-address vps.example.com:4433

# Open a listener on a remote agent
muti-metroo listener add quic :4433 -t abc123de
```

## listener remove

```bash
muti-metroo listener remove <address> [flags]
```

Stops a dynamic listener. It stops accepting new connections; peer connections already established through it stay up. `<address>` must match the address used with `listener add`. Listeners from the `listeners:` config section cannot be removed; the agent rejects the request with exit code 5.

Before acting, the command asks for confirmation:

```text
Remove listener :4434?
This stops accepting new inbound connections on it. Continue? [y/N]:
```

Any answer other than `y` or `yes` prints `Aborted.` and exits with code 0. `--yes` skips the prompt; without it, a non-terminal stdin exits with code 6 (see [Confirmation prompts](overview.md#confirmation-prompts)).

### Flags

| Flag | Default | Description |
| --- | --- | --- |
| `-a`, `--agent` | `localhost:8080` | Agent HTTP API address. See [Agent address](overview.md#agent-address). |
| `-t`, `--target` | none | Agent ID (full or unique prefix) of the remote agent that owns the listener. Omit to act on the agent at `-a`. |

### Output

```text
Listener removed: listener 127.0.0.1:18434 removed
```

### Examples

```bash
# Remove a dynamic listener from the local agent
muti-metroo listener remove :4434

# Remove a listener from a remote agent without prompting
muti-metroo listener remove :4434 -t abc123de --yes
```

## listener list

```bash
muti-metroo listener list [flags]
```

Lists all transport listeners of the agent, configured and dynamic.

### Flags

| Flag | Default | Description |
| --- | --- | --- |
| `-a`, `--agent` | `localhost:8080` | Agent HTTP API address. See [Agent address](overview.md#agent-address). |
| `-t`, `--target` | none | Agent ID (full or unique prefix) of a remote agent to query. Omit to query the agent at `-a`. |
| `--json` | `false` | Print the full API response as JSON. |

### Output

```text
Listeners (4)
ADDRESS                  TRANS  PATH         TLS        TYPE     ADVERTISE
127.0.0.1:18433          quic   -            tls        static   -
127.0.0.1:18434          quic   -            tls        dynamic  -
127.0.0.1:18435          ws     /ws/         plaintext  dynamic  -
127.0.0.1:18436          h2     /mesh        tls        dynamic  vps.example.com:443
```

`TYPE` is `static` for a listener from the `listeners:` config section and `dynamic` for one added at runtime. `ADVERTISE` shows the advertised address, or `-` when none is set.

With `--json`:

```json
{
  "status": "ok",
  "listeners": [
    {
      "transport": "quic",
      "address": "127.0.0.1:18433",
      "plaintext": false,
      "dynamic": false
    },
    {
      "transport": "quic",
      "address": "127.0.0.1:18434",
      "plaintext": false,
      "dynamic": true
    },
    {
      "transport": "ws",
      "address": "127.0.0.1:18435",
      "path": "/ws/",
      "plaintext": true,
      "dynamic": true
    },
    {
      "transport": "h2",
      "address": "127.0.0.1:18436",
      "path": "/mesh",
      "plaintext": false,
      "advertise_address": "vps.example.com:443",
      "dynamic": true
    }
  ]
}
```

### Examples

```bash
# Listeners of the local agent
muti-metroo listener list

# Listeners of a remote agent, as JSON
muti-metroo listener list -t abc123de --json
```

## Persistence

When `agent.data_dir` is set, every `listener add` and `listener remove` is written to the dynamic configuration journal and replayed when the agent starts. Dynamic listeners are also restored when the agent wakes from [sleep mode](sleep.md). Without a `data_dir`, or when the journal cannot be written, the change still takes effect but is lost on restart. See [Dynamic configuration persistence](../configuration/dynamic-config-persistence.md).

## Exit status

Exit codes follow the table in [CLI overview](overview.md#exit-codes). For `add` and `remove`, a request the agent rejects (missing `--path`, `--plaintext` on a non-`ws` transport, loopback advertise address, config listener, unknown address) exits with 5. Declining the confirmation prompt exits with 0.

## See Also

- [Listener configuration](../configuration/listeners.md): define permanent listeners and per-listener TLS in YAML
- [Listener management API](../api/listener-management.md): the HTTP endpoint behind the listener subcommands
- [Transports](../concepts/transports.md): choose between QUIC, HTTP/2, and WebSocket
- [muti-metroo peer](peer.md): dial a listener from another agent at runtime
- [Reverse proxy deployment](../deployment/reverse-proxy.md): run a plaintext WebSocket listener behind a TLS proxy
