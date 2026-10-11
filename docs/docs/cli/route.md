---
title: muti-metroo route
sidebar_label: route
sidebar_position: 14
description: Show the live route table, manage dynamic CIDR exit routes, and trace which route an agent picks for a destination.
---

# muti-metroo route

Inspect and change the CIDR exit routes of a running agent. Dynamic routes let you turn a transit agent into an exit, or adjust routing, without editing the configuration file. All subcommands talk to a running agent's HTTP API.

Global flags (`--token`, `--yes`, `--no-color`) are described in [CLI overview](overview.md#global-flags).

`route status` and `route list` answer different questions:

- `route status` shows the route table the agent uses right now: every network it has learned, with origin and hop count.
- `route list` shows the exit routes the agent itself originates: dynamic routes by default, plus routes from `exit.routes` with `--all`.

## Prerequisites

- Every subcommand except `status` needs `http.remote_api` on the agent at `-a`; `status` reads `/api/dashboard` and needs `http.dashboard`. Both are on by default (see [HTTP configuration](../configuration/http.md)).
- `add`, `remove`, `comment`, `source-ip`, and `interface` are mutations. The agent that serves the API (the one `-a` points at) must have `management.signing_private_key` configured, otherwise the request is refused. When `-t` targets a remote agent, the API agent signs the request and the target verifies it with its `management.signing_public_key`. See [Management keys](../configuration/management.md).
- If the API agent has `management.public_key` configured without `management.private_key`, every subcommand except `status` is refused.

`-t` accepts a full 32-character agent ID or a unique prefix. A prefix that matches more than one known agent is rejected; use a longer prefix.

## route status

```bash
muti-metroo route status [flags]
```

Prints the route table of the agent at `-a`. This command only queries the agent it talks to; it has no `-t` flag.

### Flags

| Flag | Default | Description |
| --- | --- | --- |
| `-a`, `--agent` | `localhost:8080` | Agent HTTP API address. See [Agent address](overview.md#agent-address). |
| `--json` | `false` | Print the route array as JSON. |

### Output

The table has the columns `NETWORK`, `NEXT HOP`, `ORIGIN`, `METRIC`, `HOPS`, and `COMMENT`, followed by a `Total: N route(s)` line. `ORIGIN` identifies the exit agent that advertises the network, `HOPS` is the number of hops to it, and `COMMENT` is the operator comment attached to the route. With no routes, the table is replaced by `No routes in table.`

### Examples

```bash
# Route table of the local agent
muti-metroo route status

# Route table of another agent's API
muti-metroo route status -a 192.168.1.10:8080

# Check whether any route covers 10.0.0.0/8
muti-metroo route status --json | jq '.[] | select(.network == "10.0.0.0/8")'
```

To see which of several overlapping routes a destination would use, run [`route trace`](#route-trace).

### Deprecated alias

`muti-metroo routes` is a deprecated alias for `route status` with the same flags and output. It prints this line to stderr before the table:

```text
Command "routes" is deprecated, use 'muti-metroo route status' (same data; cleaner naming alongside 'muti-metroo route list')
```

The alias does not appear in `muti-metroo help`. See [Deprecated commands and flags](overview.md#deprecated-commands-and-flags).

## route add

```bash
muti-metroo route add <cidr> [flags]
```

Adds a dynamic exit route for `<cidr>` and advertises it to the mesh immediately. If the agent was not an exit before, it starts handling exit traffic for the route.

Adding a default route (`0.0.0.0/0` or `::/0`) requires a typed confirmation, because the agent becomes the exit for every destination without a more specific route:

```text
0.0.0.0/0 is a DEFAULT ROUTE. Adding it advertises this agent as the exit
for any destination not covered by a more-specific route. Type 'add default route' to confirm:
```

Anything other than the exact phrase prints `Aborted. Re-run with --allow-default-route to skip this prompt.` and exits with code 0. `--allow-default-route` or `--yes` skips the prompt; without either, a non-terminal stdin exits with code 6 (see [Confirmation prompts](overview.md#confirmation-prompts)).

### Flags

| Flag | Default | Description |
| --- | --- | --- |
| `-a`, `--agent` | `localhost:8080` | Agent HTTP API address. See [Agent address](overview.md#agent-address). |
| `-t`, `--target` | none | Agent ID (full or unique prefix) of the remote agent that should originate the route. Omit to act on the agent at `-a`. |
| `-m`, `--metric` | `0` | Route metric, 0 to 65535. Lower wins when two routes have the same prefix length. |
| `--comment` | none | Operator comment. Advertised with the route, so other agents show the same text. |
| `--source-ip` | none | Local IP the exit binds outbound connections to for this route, like `curl --interface`. Must be in the same address family as `<cidr>`. Not advertised. |
| `--interface` | none | Network interface the exit binds outbound connections to for this route. Not advertised. See [`route interface`](#route-interface). |
| `--allow-default-route` | `false` | Skip the typed confirmation for `0.0.0.0/0` and `::/0`. |

An invalid CIDR or a `--source-ip` from the wrong address family is rejected before contacting the agent, with exit code 2.

### Output

```text
Route added: route 172.16.0.0/12 added
```

### Examples

```bash
# Add a route on the local agent
muti-metroo route add 10.0.0.0/24

# Add a route with a metric and a comment
muti-metroo route add 192.168.0.0/16 -m 5 --comment "office LAN"

# Bind outbound connections for this route to a local address
muti-metroo route add 10.10.0.0/16 --source-ip 192.168.1.50

# Add a route on a remote agent
muti-metroo route add 10.0.0.0/24 -t abc123de

# Make a remote agent the default exit, without prompting
muti-metroo route add 0.0.0.0/0 -t abc123de --allow-default-route
```

## route remove

```bash
muti-metroo route remove <cidr> [flags]
```

Removes a dynamic route and immediately tells connected peers to withdraw it, so they do not wait for the route TTL to expire. Routes from `exit.routes` cannot be removed at runtime; the agent rejects the request with exit code 5.

Before acting, the command asks for confirmation. When the route is found, the prompt shows its metric, kind, and comment:

```text
Remove route 172.16.0.0/12 (metric 5, dynamic, "lab")? [y/N]:
```

Any answer other than `y` or `yes` prints `Aborted.` and exits with code 0. `--yes` skips the prompt; without it, a non-terminal stdin exits with code 6.

### Flags

| Flag | Default | Description |
| --- | --- | --- |
| `-a`, `--agent` | `localhost:8080` | Agent HTTP API address. See [Agent address](overview.md#agent-address). |
| `-t`, `--target` | none | Agent ID (full or unique prefix) of the remote agent that originates the route. Omit to act on the agent at `-a`. |

### Output

```text
Route removed: route 172.16.0.0/12 removed
```

### Examples

```bash
# Remove a route from the local agent
muti-metroo route remove 10.0.0.0/24

# Remove a route from a remote agent without prompting
muti-metroo route remove 10.0.0.0/24 -t abc123de --yes
```

## route comment

```bash
muti-metroo route comment <cidr> <text> [flags]
```

Sets the operator comment on a route the agent originates, dynamic or from `exit.routes`. Pass `""` as `<text>` to clear it. The comment is re-advertised immediately, and peers show it in `route status`, `route trace`, and the dashboard. A comment set this way overrides the one in the configuration file and is kept across restarts when `agent.data_dir` is set.

### Flags

| Flag | Default | Description |
| --- | --- | --- |
| `-a`, `--agent` | `localhost:8080` | Agent HTTP API address. See [Agent address](overview.md#agent-address). |
| `-t`, `--target` | none | Agent ID (full or unique prefix) of the remote agent that originates the route. Omit to act on the agent at `-a`. |

### Output

```text
Route comment updated: route 10.0.0.0/8 comment updated
```

### Examples

```bash
# Set a comment, overriding the value from the configuration file
muti-metroo route comment 10.0.0.0/8 "internal corp network"

# Clear the comment
muti-metroo route comment 10.0.0.0/8 ""

# Set a comment on a remote agent
muti-metroo route comment 10.0.0.0/8 "internal corp network" -t abc123de
```

## route source-ip

```bash
muti-metroo route source-ip <cidr> <ip> [flags]
```

Sets the local source address the exit binds outbound connections to for a route, like `curl --interface`. Works on dynamic routes and routes from `exit.routes`; pass `""` as `<ip>` to clear the binding. The address must be in the same address family as `<cidr>` (checked before contacting the agent, exit code 2). It must also be assigned on the exit host; this is not checked when the binding is set, so a missing address shows up as failed connections for that route. The binding is local to the exit and never advertised. It overrides the configuration file value and is kept across restarts when `agent.data_dir` is set.

The target agent must already be an exit (have `exit.enabled: true` or at least one dynamic route); otherwise the request is rejected.

### Flags

| Flag | Default | Description |
| --- | --- | --- |
| `-a`, `--agent` | `localhost:8080` | Agent HTTP API address. See [Agent address](overview.md#agent-address). |
| `-t`, `--target` | none | Agent ID (full or unique prefix) of the remote agent that originates the route. Omit to act on the agent at `-a`. |

### Output

```text
Route source IP updated: route 192.168.0.0/16 source_ip updated
```

### Examples

```bash
# Bind outbound connections for 10.10.0.0/16 to a local address
muti-metroo route source-ip 10.10.0.0/16 192.168.1.50

# Clear the binding and use the operating system's default source address
muti-metroo route source-ip 10.10.0.0/16 ""
```

## route interface

```bash
muti-metroo route interface <cidr> <name> [flags]
```

Sets the network interface the exit binds outbound connections to for a route. Works on dynamic routes and routes from `exit.routes`; pass `""` as `<name>` to clear the binding. The binding is local to the exit, never advertised, overrides the configuration file value, and is kept across restarts when `agent.data_dir` is set.

How the binding is applied depends on the route's source IP:

- With no source IP, or a source IP assigned to that interface, the exit binds to an address on the interface. This works on Linux, macOS, and Windows without elevated privileges.
- With a source IP that is not assigned to the interface (for example a loopback-bound address that must leave through a physical NIC), the exit uses `SO_BINDTODEVICE`, which requires Linux and root. On other platforms, or on Linux without root, connections for that route fail.

The interface name is not checked when the binding is set. As with `source-ip`, the target agent must already be an exit.

### Flags

| Flag | Default | Description |
| --- | --- | --- |
| `-a`, `--agent` | `localhost:8080` | Agent HTTP API address. See [Agent address](overview.md#agent-address). |
| `-t`, `--target` | none | Agent ID (full or unique prefix) of the remote agent that originates the route. Omit to act on the agent at `-a`. |

### Output

```text
Route interface updated: route 192.168.0.0/16 interface updated
```

### Examples

```bash
# Send outbound connections for 10.10.0.0/16 out of ens192
muti-metroo route interface 10.10.0.0/16 ens192

# Clear the binding
muti-metroo route interface 10.10.0.0/16 ""

# Set source IP and interface together when adding a route
muti-metroo route add 10.10.0.0/16 --source-ip 192.168.1.50 --interface ens192
```

## route list

```bash
muti-metroo route list [flags]
```

Lists the exit routes the agent originates. By default only dynamic routes are shown. With `--all`, routes from `exit.routes` are included and a `KIND` column (`config` or `dynamic`) is added; entries are sorted by network.

`SOURCE` and `INTERFACE` columns appear only when at least one listed route has a source IP or interface binding.

### Flags

| Flag | Default | Description |
| --- | --- | --- |
| `-a`, `--agent` | `localhost:8080` | Agent HTTP API address. See [Agent address](overview.md#agent-address). |
| `-t`, `--target` | none | Agent ID (full or unique prefix) of a remote agent to query. Omit to query the agent at `-a`. |
| `--all` | `false` | Include routes from `exit.routes`. |
| `--json` | `false` | Print the full API response as JSON. |

### Output

Default:

```text
Dynamic Routes (1)
NETWORK                  METRIC  COMMENT
172.16.0.0/12            5       lab
```

With `--all`:

```text
Local Routes (3)
NETWORK                  METRIC  KIND     COMMENT
10.0.0.0/8               0       config   internal corp
172.20.0.0/16            0       dynamic  remote lab
192.168.0.0/16           0       config
```

With bindings set:

```text
Local Routes (3)
NETWORK                  METRIC  KIND     SOURCE             INTERFACE    COMMENT
10.0.0.0/8               0       config
172.20.0.0/16            0       dynamic                                  remote lab
192.168.0.0/16           0       config   192.168.1.50       ens192
```

With no routes, the command prints `No dynamic routes configured` (or `No routes configured` with `--all`).

`--all --json` output:

```json
{
  "status": "ok",
  "routes": [
    {
      "network": "10.0.0.0/8",
      "metric": 0,
      "dynamic": false,
      "comment": "internal corp"
    },
    {
      "network": "192.168.0.0/16",
      "metric": 0,
      "dynamic": false,
      "source_ip": "192.168.1.50",
      "interface": "ens192"
    }
  ]
}
```

### Examples

```bash
# Dynamic routes of the local agent
muti-metroo route list

# All routes the local agent originates, including configured ones
muti-metroo route list --all

# All routes of a remote agent, as JSON
muti-metroo route list -t abc123de --all --json
```

## route trace

```bash
muti-metroo route trace <ip|hostname> [flags]
```

Shows which route the agent would choose for a destination, using the same rules as a real connection: longest prefix wins, then lowest metric. When two exits advertise the same network with the same metric, `route trace` names the exit with the lower agent ID, but a real connection may use either one (see [Route selection](/concepts/routing#route-selection)). Every matching CIDR route is listed. The winner is marked with `->`; each other candidate carries the reason it lost (`shorter prefix`, `higher metric`, or `lost origin tiebreaker`). Route comments are appended after `#`.

A hostname is resolved on the selected agent, in this order: domain routes are checked and reported, then the [mesh-host registry](host.md), then the system resolver. CIDR candidates are listed for every resolved address. The JSON output reports how the destination was resolved in the `resolution` field: `ip` for a literal address, `mesh` for a mesh-host entry, or `system` for the system resolver.

`route trace` is read-only and does not need a signing key.

### Flags

| Flag | Default | Description |
| --- | --- | --- |
| `-a`, `--agent` | `localhost:8080` | Agent HTTP API address. See [Agent address](overview.md#agent-address). |
| `-t`, `--target` | none | Agent ID (full or unique prefix) of a remote agent to trace from. Omit to trace on the agent at `-a`. |
| `--json` | `false` | Print the API response as JSON (single line). |

### Output

For an IP address:

```text
Trace for 10.1.2.3
    -> 10.1.0.0/16          metric=4 hops=1 next_hop=exit-office (b0bca945) origin=exit-office (b0bca945)  # branch
       10.0.0.0/8           metric=1 hops=1 next_hop=exit-office (b0bca945) origin=exit-office (b0bca945)  [shorter prefix]
```

For a hostname:

```text
Trace for example.com
  Domain route: no match
  Resolved: 203.0.113.10

  203.0.113.10:
    no matching CIDR route
```

### Examples

```bash
# Trace an address on the local agent
muti-metroo route trace 10.1.2.3

# Trace a hostname from a remote agent
muti-metroo route trace example.com -t abc123de

# Pretty-print the JSON result
muti-metroo route trace 10.1.2.3 --json | jq .
```

## Persistence

When `agent.data_dir` is set, every route mutation (`add`, `remove`, `comment`, `source-ip`, `interface`) is written to the dynamic configuration journal and replayed when the agent starts. Without a `data_dir`, or when the journal cannot be written, the change still takes effect but is lost on restart. For routes that must always exist, use `exit.routes` (see [Exit configuration](../configuration/exit.md)) or see [Dynamic configuration persistence](../configuration/dynamic-config-persistence.md).

## Exit status

Exit codes follow the table in [CLI overview](overview.md#exit-codes). For `add` and `remove`, a request the agent rejects (unknown route, config route, invalid binding) exits with 5. An invalid CIDR passed to `add` or `remove`, or a source IP from the wrong address family, exits with 2. Declining a confirmation prompt exits with 0.

## See Also

- [Exit configuration](../configuration/exit.md): define permanent exit routes, comments, and bindings in YAML
- [Routing concepts](../concepts/routing.md): how routes propagate and how the winner is chosen
- [Route management API](../api/route-management.md): the HTTP endpoint behind the route subcommands
- [Route trace API](../api/route-trace.md): the HTTP endpoint behind `route trace`
- [muti-metroo host](host.md): register mesh hostnames that `route trace` resolves
