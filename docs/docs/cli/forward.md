---
title: muti-metroo forward
sidebar_label: forward
sidebar_position: 16
description: Add, remove, and list port forward listeners and endpoints on a running agent.
---

# muti-metroo forward

Manage [port forwarding](../features/port-forwarding.md) on a running agent without editing its configuration. Port forwarding has two halves, joined by a routing key:

- A **listener** (`forward add`, `forward remove`, `forward list`) runs on the ingress side. It accepts TCP connections on a local address and sends them through the mesh to whichever agent advertises the same key.
- An **endpoint** (`forward endpoint add`, `forward endpoint remove`, `forward endpoint list`) runs on the exit side. It advertises the key to the mesh and connects incoming streams to a local `host:port` target.

The commands talk to the agent's HTTP API. Pass `-t` to act on another agent in the mesh, reached through the agent at `--agent`.

Listeners and endpoints defined in the `forward` section of the configuration appear in the lists as `static` and cannot be replaced or removed at runtime. Runtime entries appear as `dynamic`. When `agent.data_dir` is set, each runtime change is recorded in the [dynamic config journal](../configuration/dynamic-config-persistence.md) and replayed on the next start. Without a `data_dir`, or if the journal cannot be written, runtime entries last until the agent restarts.

`add` and `remove` are privileged operations: the agent at `--agent` must have `management.signing_private_key` configured. For a remote target, the entry agent signs the request and the target verifies it with its `management.signing_public_key`. An agent that has `management.public_key` configured without `management.private_key` refuses all `forward` commands, including `list`. See [management configuration](../configuration/management.md).

Global flags (`--token`, `--yes`, `--no-color`) are described in [CLI overview](overview.md#global-flags).

## forward add

```bash
muti-metroo forward add <key> <address> [flags]
```

Start a forward listener on `<address>` (for example `:9090` or `127.0.0.1:9090`) that forwards connections to the endpoint advertising `<key>`. The new listener is advertised to peers right away.

Adding a key that already exists as a dynamic listener stops the old listener and starts a new one with the new address and settings. Adding a key that exists in the configuration fails.

The journal stores the address as given, so an address with port `0` binds a different port after every restart. Use an explicit port when the address must stay the same.

### Flags

| Flag | Default | Description |
| --- | --- | --- |
| `-a`, `--agent` | `localhost:8080` | Agent HTTP API address used as the entry point. See [agent address](overview.md#agent-address). |
| `-t`, `--target` | none | ID of the agent to act on: a full 32-character ID or a unique prefix of one. Omit for the agent at `--agent`. |
| `--max-connections` | `0` | Maximum concurrent connections on this listener. `0` means unlimited. |

### Output

```text
Forward listener added: forward listener "api-server" added on 127.0.0.1:9091
```

### Examples

Listen on port 9090 on all interfaces of the local agent:

```bash
muti-metroo forward add web-server :9090
```

Limit the listener to 100 concurrent connections:

```bash
muti-metroo forward add web-server :9090 --max-connections 100
```

Add the listener on a remote agent:

```bash
muti-metroo forward add web-server 127.0.0.1:9090 -t abc123de
```

## forward remove

```bash
muti-metroo forward remove <key> [flags]
```

Stop and remove a dynamic forward listener. Listeners from the configuration cannot be removed; the command fails with exit status 5.

### Flags

| Flag | Default | Description |
| --- | --- | --- |
| `-a`, `--agent` | `localhost:8080` | Agent HTTP API address used as the entry point. See [agent address](overview.md#agent-address). |
| `-t`, `--target` | none | ID of the agent to act on: a full 32-character ID or a unique prefix of one. Omit for the agent at `--agent`. |

### Confirmation prompt

```text
Remove forward listener "web-server"?
This closes the listening socket and rejects new ingress connections. Continue? [y/N]:
```

Answering anything other than `y` or `yes` prints `Aborted.` and exits 0. `--yes` skips the prompt; without it, a non-terminal stdin makes the command exit 6. See [confirmation prompts](overview.md#confirmation-prompts).

### Output

```text
Forward listener removed: forward listener "web-server" removed
```

### Examples

```bash
muti-metroo forward remove web-server
```

```bash
muti-metroo forward remove web-server -t abc123de -y
```

## forward list

```bash
muti-metroo forward list [flags]
```

List forward listeners, both from the configuration (`static`) and added at runtime (`dynamic`).

### Flags

| Flag | Default | Description |
| --- | --- | --- |
| `-a`, `--agent` | `localhost:8080` | Agent HTTP API address used as the entry point. See [agent address](overview.md#agent-address). |
| `-t`, `--target` | none | ID of the agent to query: a full 32-character ID or a unique prefix of one. Omit for the agent at `--agent`. |
| `--json` | `false` | Print the list as JSON. |

### Output

```text
Forward Listeners (2)
KEY                  ADDRESS                  TYPE     MAX_CONN
web-server           127.0.0.1:9090           static   unlimited
api-server           127.0.0.1:9091           dynamic  100
```

When there are no listeners, the command prints `No forward listeners configured`.

With `--json`:

```json
{
  "status": "ok",
  "listeners": [
    {
      "key": "web-server",
      "address": "127.0.0.1:9090",
      "max_connections": 0,
      "dynamic": false
    },
    {
      "key": "api-server",
      "address": "127.0.0.1:9091",
      "max_connections": 100,
      "dynamic": true
    }
  ]
}
```

### Examples

```bash
muti-metroo forward list
```

```bash
muti-metroo forward list -t abc123de --json
```

## forward endpoint add

```bash
muti-metroo forward endpoint add <key> <target> [flags]
```

Register `<key>` on the agent and connect streams for it to `<target>`, a `host:port` reachable from that agent. The key is advertised to the mesh so that listeners on other agents can reach the target.

Adding a key that already exists as a dynamic endpoint replaces its target. Adding a key that exists in the configuration fails.

### Flags

| Flag | Default | Description |
| --- | --- | --- |
| `-a`, `--agent` | `localhost:8080` | Agent HTTP API address used as the entry point. See [agent address](overview.md#agent-address). |
| `-t`, `--target` | none | ID of the agent to act on: a full 32-character ID or a unique prefix of one. Omit for the agent at `--agent`. |

The `--target` flag selects the agent; the positional `<target>` argument is the service address.

### Output

```text
Forward endpoint added: forward endpoint "db" added -> 127.0.0.1:5432
```

### Examples

Expose a local web server under the key `web-server`:

```bash
muti-metroo forward endpoint add web-server 127.0.0.1:3000
```

Add the endpoint on a remote agent:

```bash
muti-metroo forward endpoint add web-server 127.0.0.1:3000 -t abc123de
```

## forward endpoint remove

```bash
muti-metroo forward endpoint remove <key> [flags]
```

Remove a dynamic forward endpoint. The agent withdraws the key from the mesh right away, so peers drop the route without waiting for `routing.route_ttl` to expire. Endpoints from the configuration cannot be removed; the command fails with exit status 5.

### Flags

| Flag | Default | Description |
| --- | --- | --- |
| `-a`, `--agent` | `localhost:8080` | Agent HTTP API address used as the entry point. See [agent address](overview.md#agent-address). |
| `-t`, `--target` | none | ID of the agent to act on: a full 32-character ID or a unique prefix of one. Omit for the agent at `--agent`. |

### Confirmation prompt

```text
Remove forward endpoint "web-server"?
Ingress agents will no longer be able to route to this exit. Continue? [y/N]:
```

Declining, `--yes`, and non-terminal stdin behave as for `forward remove`.

### Output

```text
Forward endpoint removed: forward endpoint "web-server" removed
```

### Examples

```bash
muti-metroo forward endpoint remove web-server
```

```bash
muti-metroo forward endpoint remove web-server -t abc123de -y
```

## forward endpoint list

```bash
muti-metroo forward endpoint list [flags]
```

List forward endpoints, both from the configuration (`static`) and added at runtime (`dynamic`).

### Flags

| Flag | Default | Description |
| --- | --- | --- |
| `-a`, `--agent` | `localhost:8080` | Agent HTTP API address used as the entry point. See [agent address](overview.md#agent-address). |
| `-t`, `--target` | none | ID of the agent to query: a full 32-character ID or a unique prefix of one. Omit for the agent at `--agent`. |
| `--json` | `false` | Print the list as JSON. |

### Output

```text
Forward Endpoints (2)
KEY                  TARGET                   TYPE
web-server           127.0.0.1:3000           static
db                   127.0.0.1:5432           dynamic
```

When there are no endpoints, the command prints `No forward endpoints configured`.

With `--json`:

```json
{
  "status": "ok",
  "endpoints": [
    {
      "key": "web-server",
      "target": "127.0.0.1:3000",
      "dynamic": false
    },
    {
      "key": "db",
      "target": "127.0.0.1:5432",
      "dynamic": true
    }
  ]
}
```

### Examples

```bash
muti-metroo forward endpoint list
```

```bash
muti-metroo forward endpoint list -t abc123de --json
```

## See Also

- [Port forwarding](../features/port-forwarding.md): how listeners, endpoints, and routing keys work together
- [Forward configuration](../configuration/forward.md): define permanent listeners and endpoints in YAML
- [Forward management API](../api/forward-management.md): the HTTP endpoints behind these commands
- [Dynamic config persistence](../configuration/dynamic-config-persistence.md): how runtime changes survive restarts
- [route](route.md): manage CIDR exit routes at runtime
