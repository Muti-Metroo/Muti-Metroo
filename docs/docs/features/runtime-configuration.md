---
title: Runtime Configuration
description: Add and remove peers, listeners, routes, forwards, mesh hostnames, and display names on running agents, locally or across the mesh.
sidebar_position: 7
---

# Runtime Configuration

Most of an agent's mesh settings can be changed while it runs, without editing its configuration file or restarting it. An operator can tell an agent to dial a new peer, open another transport listener, advertise a route, publish a forward, or rename itself. The same commands work on the local agent and, through it, on any agent the local agent can route to.

| Change | Command | Remote (`-t`) |
| ------ | ------- | ------------- |
| Dial a new peer, or stop dialing one | `muti-metroo peer add` / `peer remove` | Yes |
| Open or close a transport listener | `muti-metroo listener add` / `listener remove` | Yes |
| Advertise or withdraw a CIDR route, set its comment, source IP, or interface | `muti-metroo route add` / `remove` / `comment` / `source-ip` / `interface` | Yes |
| Publish or remove a forward endpoint | `muti-metroo forward endpoint add` / `remove` | Yes |
| Open or close a forward listener | `muti-metroo forward add` / `remove` | Yes |
| Rename the agent | `muti-metroo display-name set` | Yes |
| Register or remove a mesh hostname | `muti-metroo host add` / `remove` | No |

Every command group has a `list` subcommand (and `peer status` / `route status` for live state) that shows configured and runtime entries side by side. Entries from the configuration file cannot be removed at runtime.

## Before you start

The commands call an agent's HTTP API, by default `localhost:8080`; use `-a` to reach a different one. The agent the CLI talks to must:

- Have the HTTP API enabled. If `http.token_hash` is set, pass the token with `--token` or the `MUTI_METROO_TOKEN` environment variable.
- Hold the mesh signing private key (`management.signing_private_key`). Without it, every change is refused with "management signing private key not configured on this agent".

For a change on a remote agent (`-t <agent-id>`), the local agent signs the request with its private key, and the target agent must hold the matching `management.signing_public_key` to verify it. See [Authentication](/security/authentication#management-signing-key) for how to generate and distribute the key pair.

`-t` accepts a full agent ID or a unique prefix of one.

## Connect a new peer

Tell an agent to dial another agent:

```bash
muti-metroo peer add 203.0.113.10:4433 --id 1f743225d524f8acd3b42b55639702f0
```

```text
Peer added: peer 203.0.113.10:4433 added (transport=quic)
```

The default transport is QUIC; use `--transport h2` or `--transport ws` for the others. `--id` pins the identity the remote agent must report; without it the agent accepts whatever ID the remote side claims and prints a warning. The new peer uses the agent's global TLS settings.

To build a path into a network that only a remote agent can reach, run the same command with `-t` so that the remote agent does the dialing:

```bash
muti-metroo peer add 10.20.0.5:4433 -t 1f743225
```

`muti-metroo peer remove <address>` disconnects a runtime peer and stops reconnecting to it.

## Open a listener

Open an additional transport listener, for example a WebSocket listener behind a reverse proxy:

```bash
muti-metroo listener add ws 127.0.0.1:8443 --path /mesh --plaintext
muti-metroo listener list
```

```text
Listeners (2)
ADDRESS                  TRANS  PATH         TLS        TYPE     ADVERTISE
0.0.0.0:4433             quic   -            tls        static   -
127.0.0.1:8443           ws     /mesh        plaintext  dynamic  -
```

`--advertise-address` publishes a public dial address for the listener to the rest of the mesh. Removing a listener stops new connections; existing peer connections stay up.

## Advertise routes and forwards

Route and forward changes are covered in their own guides:

- [Exit Routing](/features/exit-routing#add-routes-at-runtime): `route add`, `route remove`, `route source-ip`, `route interface`
- [Port Forwarding](/features/port-forwarding#add-forwards-at-runtime): `forward endpoint add`, `forward add`
- [Mesh Hostnames](/features/mesh-hostnames#add-names-at-runtime): `host add`

## Rename an agent

```bash
muti-metroo display-name set "gateway-us-east" -t 1f743225
```

The agent advertises the new name to its peers immediately, and it appears in topology views and route output across the mesh. `muti-metroo display-name set ""` reverts to the configured name.

## Persistence

Runtime changes are appended to a journal in the agent's data directory and replayed on the next start, so they survive restarts and [sleep mode](/features/sleep-mode). If `agent.data_dir` is not set or the journal cannot be written, changes still apply but are lost when the agent stops. To make a change permanent independently of the journal, add it to the configuration file. See [Dynamic Config Persistence](/configuration/dynamic-config-persistence).

## Confirmation prompts

`remove` subcommands, `route add 0.0.0.0/0`, and `display-name set ""` ask for confirmation. Pass `--yes` (`-y`) in scripts; without it, a command run with no terminal attached exits with code 6.

## See Also

- [peer CLI](/cli/peer) and [listener CLI](/cli/listener): every flag for runtime peers and listeners
- [route CLI](/cli/route) and [forward CLI](/cli/forward): runtime routes and forwards
- [Dynamic Config Persistence](/configuration/dynamic-config-persistence): the journal and how it is replayed
- [Authentication](/security/authentication): signing keys and API tokens that gate these changes
- [CLI Overview](/cli/overview): exit codes and global flags
