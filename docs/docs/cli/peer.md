---
title: muti-metroo peer
sidebar_label: peer
sidebar_position: 13
description: Show live peer connections and add, remove, or list dynamic outbound peers on a running agent.
---

# muti-metroo peer

Show the peers an agent is connected to, and tell a running agent to dial (or stop dialing) a peer without editing its configuration file. All subcommands talk to a running agent's HTTP API.

Global flags (`--token`, `--yes`, `--no-color`) are described in [CLI overview](overview.md#global-flags).

`peer status` and `peer list` answer different questions:

- `peer status` shows who the agent is talking to right now: live connections with role and round-trip time.
- `peer list` shows the peer set the agent manages: peers from the `peers:` config section, dynamic peers added at runtime, and inbound connections.

## Prerequisites

- `add`, `remove`, and `list` need `http.remote_api` on the agent at `-a`, and `peer status` needs `http.dashboard`. Both are on by default (see [HTTP configuration](../configuration/http.md)).
- `peer add` and `peer remove` are mutations. The agent that serves the API (the one `-a` points at) must have `management.signing_private_key` configured, otherwise the request is refused. When `-t` targets a remote agent, the API agent signs the request and the target verifies it with its `management.signing_public_key`. See [Management keys](../configuration/management.md).
- If the API agent has `management.public_key` configured without `management.private_key`, `add`, `remove`, and `list` are refused.

## peer status

```bash
muti-metroo peer status [flags]
```

Prints the live peer table of the agent at `-a`. This command only queries the agent it talks to; it has no `-t` flag.

### Flags

| Flag | Default | Description |
| --- | --- | --- |
| `-a`, `--agent` | `localhost:8080` | Agent HTTP API address. See [Agent address](overview.md#agent-address). |
| `--json` | `false` | Print the peer array as JSON. |

### Output

```text
Connected Peers
===============
ID           NAME                 STATE          ROLE       RTT
--           ----                 -----          ----       ---
b0bca945     exit-office          CONNECTED      listener   -
3061403a     branch               CONNECTED      dialer     -

Total: 2 peer(s)
```

| Column | Description |
| --- | --- |
| `ID` | Short agent ID of the peer. |
| `NAME` | Display name of the peer. Empty when the peer has not announced one yet. |
| `STATE` | Connection state. Shows `UNRESPONSIVE` (red on a terminal) when the last measured round-trip time exceeds 60 seconds. |
| `ROLE` | `dialer` when this agent opened the connection, `listener` when the peer dialed in. |
| `RTT` | Round-trip time measured by keepalives, or `-` when no measurement exists yet. |

When there are no connections, the table is replaced by `No peers connected.`

With `--json`, the output is an array of peer objects:

```json
[
  {
    "id": "b0bca94598dbfacafd808aed1735e231",
    "short_id": "b0bca945",
    "display_name": "exit-office",
    "state": "CONNECTED",
    "rtt_ms": 0,
    "unresponsive": false,
    "is_dialer": false
  }
]
```

### Examples

```bash
# Live peers of the local agent
muti-metroo peer status

# Live peers of another agent's API
muti-metroo peer status -a 192.168.1.10:8080

# Count connected peers in a script
muti-metroo peer status --json | jq 'length'
```

### Deprecated alias

`muti-metroo peers` is a deprecated alias for `peer status` with the same flags and output. It prints this line to stderr before the table:

```text
Command "peers" is deprecated, use 'muti-metroo peer status' (same data; cleaner naming alongside 'muti-metroo peer list')
```

The alias does not appear in `muti-metroo help`. See [Deprecated commands and flags](overview.md#deprecated-commands-and-flags).

## peer add

```bash
muti-metroo peer add <address> [flags]
```

Tells the agent to dial `<address>` and keep the connection up. A typical use is a network where a new agent can accept connections but cannot dial out: run `peer add` against an agent that can reach it.

The new connection uses the agent's global TLS settings (CA, client certificate, mTLS, strict verification). Per-peer TLS overrides are only available in the `peers:` config section. A dynamic peer reconnects with exponential backoff after a disconnect, like a configured peer.

Peers are keyed by dial address. Running `peer add` again with the same address replaces the dynamic peer: the old connection is closed, its reconnect is cancelled, and a new dial starts with the new settings. Addresses that belong to a configured peer cannot be replaced.

When `--id` is omitted or set to `auto`, the CLI prints a warning to stderr because the agent accepts whatever identity answers at that address:

```text
Warning: --id not specified; this peer connection will accept any agent identity at newagent.example.com:4433.
For peer pinning use --id <agent-id>.
```

### Flags

| Flag | Default | Description |
| --- | --- | --- |
| `-a`, `--agent` | `localhost:8080` | Agent HTTP API address. See [Agent address](overview.md#agent-address). |
| `-t`, `--target` | none | Agent ID (full or unique prefix) of a remote agent that should dial the peer. Omit to act on the agent at `-a`. |
| `--transport` | `quic` | Transport to dial with: `quic`, `h2`, or `ws`. |
| `--id` | none | Expected agent ID of the peer, 32 hex characters. Omit or pass `auto` to accept any identity. |
| `--tcp-keepalive` | none | TCP keepalive period for `h2` and `ws` peers, as a Go duration (`20s`). Omit to inherit `connections.tcp_keepalive`; `0` disables it. The CLI rejects an unparsable value with exit code 2. |

### Output

```text
Peer added: peer 127.0.0.1:18450 added (transport=quic)
```

### Examples

```bash
# Dial a new peer over QUIC and pin its identity
muti-metroo peer add newagent.example.com:4433 --id abc123def456789012345678901234ab

# Dial over HTTP/2
muti-metroo peer add newagent.example.com:443 --transport h2 --id abc123def456789012345678901234ab

# Dial a WebSocket peer by URL
muti-metroo peer add wss://relay.example.com:443/mesh --transport ws

# Tell remote agent def45678 (reachable through the mesh) to dial the new agent
muti-metroo peer add newagent.example.com:4433 -t def45678 --id abc123def456789012345678901234ab
```

## peer remove

```bash
muti-metroo peer remove <address> [flags]
```

Closes the connection to a dynamic peer, cancels its reconnect, and removes it from the agent. `<address>` must match the address used with `peer add`. Peers from the `peers:` config section cannot be removed; the agent rejects the request with exit code 5.

Before acting, the command asks for confirmation:

```text
Remove peer newagent.example.com:4433?
This tears down the live connection and cancels reconnect. Continue? [y/N]:
```

Any answer other than `y` or `yes` prints `Aborted.` and exits with code 0. `--yes` skips the prompt; without it, a non-terminal stdin exits with code 6 (see [Confirmation prompts](overview.md#confirmation-prompts)).

### Flags

| Flag | Default | Description |
| --- | --- | --- |
| `-a`, `--agent` | `localhost:8080` | Agent HTTP API address. See [Agent address](overview.md#agent-address). |
| `-t`, `--target` | none | Agent ID (full or unique prefix) of the remote agent that owns the peer. Omit to act on the agent at `-a`. |

### Output

```text
Peer removed: peer 127.0.0.1:18450 removed (disconnected 3061403a)
```

The `(disconnected ...)` suffix with the peer's short ID appears only when a live connection was closed.

### Examples

```bash
# Remove a dynamic peer from the local agent
muti-metroo peer remove newagent.example.com:4433

# Remove from a remote agent without prompting
muti-metroo peer remove newagent.example.com:4433 -t def45678 --yes
```

## peer list

```bash
muti-metroo peer list [flags]
```

Lists the peers the agent manages: configured peers, dynamic peers, and inbound connections from agents that dialed in.

### Flags

| Flag | Default | Description |
| --- | --- | --- |
| `-a`, `--agent` | `localhost:8080` | Agent HTTP API address. See [Agent address](overview.md#agent-address). |
| `-t`, `--target` | none | Agent ID (full or unique prefix) of a remote agent to query. Omit to query the agent at `-a`. |
| `--json` | `false` | Print the full API response as JSON. |

### Output

```text
Peers (2)
ADDRESS                        TRANS  CONNECTED  TYPE       REMOTE_ID
127.0.0.1:18450                quic   yes        dynamic    3061403a0522cf68cb506d297022822c
127.0.0.1:63705                quic   yes        inbound    b0bca94598dbfacafd808aed1735e231
```

`TYPE` is `static` for a peer from the `peers:` config section, `dynamic` for a peer added at runtime, and `inbound` for a connection another agent opened to this one. For inbound peers, `ADDRESS` is the remote socket address. `REMOTE_ID` is `-` while no connection is established. With no peers, the command prints `No peers configured`.

With `--json`, the response object is printed as is. Inbound entries carry `"direction": "inbound"`:

```json
{
  "status": "ok",
  "peers": [
    {
      "address": "127.0.0.1:18450",
      "transport": "quic",
      "remote_id": "3061403a0522cf68cb506d297022822c",
      "connected": true,
      "persistent": true,
      "dynamic": true
    },
    {
      "address": "127.0.0.1:63705",
      "transport": "quic",
      "remote_id": "b0bca94598dbfacafd808aed1735e231",
      "connected": true,
      "persistent": false,
      "dynamic": false,
      "direction": "inbound"
    }
  ]
}
```

### Examples

```bash
# Peers of the local agent
muti-metroo peer list

# Peers of a remote agent, as JSON
muti-metroo peer list -t def45678 --json
```

## Persistence

When `agent.data_dir` is set, every `peer add` and `peer remove` is written to the dynamic configuration journal and replayed when the agent starts, so dynamic peers survive restarts. Without a `data_dir`, or when the journal cannot be written, the change still takes effect but is lost on restart. See [Dynamic configuration persistence](../configuration/dynamic-config-persistence.md).

## Exit status

Exit codes follow the table in [CLI overview](overview.md#exit-codes). For `add` and `remove`, a request the agent rejects (unknown transport, invalid `--id`, config peer, unknown address) exits with 5, and an invalid `--tcp-keepalive` value exits with 2. Declining the confirmation prompt exits with 0.

## See Also

- [Peer configuration](../configuration/peers.md): define permanent peers and per-peer TLS overrides in YAML
- [Peer management API](../api/peer-management.md): the HTTP endpoint behind `peer add`, `remove`, and `list`
- [Dashboard API](../api/dashboard.md): the endpoint behind `peer status`
- [muti-metroo listener](listener.md): open the inbound side of a peer connection at runtime
- [Dynamic configuration persistence](../configuration/dynamic-config-persistence.md): how runtime changes survive restarts
