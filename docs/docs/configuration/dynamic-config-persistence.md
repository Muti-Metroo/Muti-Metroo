---
title: Dynamic Config Persistence
sidebar_position: 20
---

# Dynamic Config Persistence

Runtime mutations made through the CLI and HTTP API -- `muti-metroo peer add`, `muti-metroo listener add`, `muti-metroo forward add`, `muti-metroo forward endpoint add`, `muti-metroo route add`, `muti-metroo route comment`, `muti-metroo host add`, `muti-metroo display-name set`, and their `remove` counterparts -- are persisted to a journal file in the agent's data directory. When the agent restarts, the journal is replayed through the same code paths that originally handled the commands, restoring every dynamic change.

Persistence is automatic and opt-in by way of `agent.data_dir`: set one, and persistence is enabled. Leave it unset, and every runtime change stays ephemeral (the pre-3.4 behavior).

## What gets persisted

| Command                                  | Kind of change                                                |
|------------------------------------------|---------------------------------------------------------------|
| `muti-metroo peer add/remove`                  | Outbound peer dial and teardown                               |
| `muti-metroo listener add/remove`              | Transport listener (QUIC/H2/WS) on ingress side               |
| `muti-metroo forward add/remove`               | Port-forward listener on ingress side                         |
| `muti-metroo forward endpoint add/remove`      | Port-forward endpoint on exit side                            |
| `muti-metroo route add/remove`                 | CIDR exit route on exit side                                  |
| `muti-metroo route comment`                    | Operator comment on a route (dynamic or config-seeded override) |
| `muti-metroo host add/remove`                  | Mesh-host hostname registration                               |
| `muti-metroo display-name set`                 | Agent display name override                                   |

The equivalent HTTP endpoints (`/peers/manage`, `/listeners/manage`, `/forward/manage`, `/forward/endpoints/manage`, `/routes/manage`, `/hosts/manage`, `/display-name/manage`) persist the same way -- the CLI is just a wrapper around them.

Changes driven by the main `config.yaml` (config peers, config listeners, config forward listeners, config routes) are NOT written to the journal: they are already durable in the config file.

## Journal file

- **Location:** `<agent.data_dir>/dynamic-config.log`
- **Format:** JSON Lines -- one event object per line, newline terminated
- **Permissions:** `0600` (agent user only)
- **Write mode:** append-only, fsynced after each event

Each event captures the verb plus the arguments originally passed to `Manage*`:

```jsonl
{"op":"display_name_set","ts":1712345678901234567,"name":"gateway-us-east"}
{"op":"route_add","ts":1712345678912345678,"network":"10.0.0.0/8","metric":3,"comment":"office LAN"}
{"op":"route_comment_set","ts":1712345678956789012,"network":"10.0.0.0/8","comment":"internal corp"}
{"op":"forward_add","ts":1712345678923456789,"key":"web","address":":9090","max_connections":100}
{"op":"forward_endpoint_add","ts":1712345678945678901,"key":"web","target":"127.0.0.1:3000"}
{"op":"peer_add","ts":1712345678934567890,"address":"peer.example:4433","transport":"quic"}
{"op":"listener_add","ts":1712345678939876543,"transport":"quic","address":":4434","advertise_address":"vps.example.com:4434"}
{"op":"host_add","ts":1712345678967890123,"name":"target-11.mesh","ipv4":"10.8.0.11"}
```

Route comments survive journal compaction for both dynamic routes and config-seeded routes whose YAML comment was overridden via `muti-metroo route comment`.

The file is safe to `cat` while the agent is running. You can also edit it by hand when the agent is stopped; malformed lines cause the replay to abort (with a warning) on the offending line number so mistakes are easy to locate.

## Replay on startup

After `agent.Start()` completes successfully, the agent loads the journal and replays each event through the same `Manage*` method that originally produced it. The replay:

1. Applies `display_name_set` first, so the first post-restart advertisement carries the restored name.
2. Applies routes, forward listeners, peers, and transport listeners in that order (the sets are independent).
3. Logs a warning for any event that fails (for example a forward listener whose bound port is no longer available) and continues with the next event.
4. Compacts the journal on success: the file is rewritten to the minimal set of "add"/"set" events that represent the current live state, collapsing long histories of add/remove churn into O(live entries).

Replayed entries are still tagged `Dynamic: true` in `muti-metroo peer list`, `muti-metroo listener list`, `muti-metroo forward list`, and `muti-metroo route list`, so the config-vs-dynamic distinction is preserved across restarts.

## Fallback to ephemeral

Persistence is best-effort. If the journal file cannot be written -- wrong permissions on `data_dir`, a read-only filesystem, a full disk -- the runtime mutation still succeeds and the agent logs a warning that the change is ephemeral:

```
WARN  persist dynamic config failed; change is ephemeral  op=peer_add path=/data/dynamic-config.log error="open ...: permission denied"
```

The mutation is applied in memory as usual; it just does not survive the next restart. Operators who deliberately want ephemeral behavior can unset `agent.data_dir`, which disables persistence entirely.

## Inspecting and clearing the journal

```bash
# Inspect the current state
cat /var/lib/muti-metroo/dynamic-config.log | jq

# Count pending events
wc -l /var/lib/muti-metroo/dynamic-config.log

# Reset all dynamic state (stop the agent first)
rm /var/lib/muti-metroo/dynamic-config.log
systemctl start muti-metroo
```

There is no dedicated CLI for inspecting or pruning the journal; the file format is stable and deliberately trivial to work with using standard shell tools.

## Interaction with config entries

- **Config peers/forwards/listeners take precedence.** A journal entry that conflicts with an entry in `config.yaml` (same peer address, same forward-listener key) is rejected on replay with a warning, and dropped during the next compaction.
- **Config routes are not deduplicated against journal routes.** If `exit.routes` in `config.yaml` covers the same CIDR as a dynamic route, both are announced -- the route-selection algorithm already handles longest-prefix + metric tiebreaking.
- **Unsetting a dynamic display name** (via `muti-metroo display-name set ""`) reverts to `agent.display_name` in the config and is recorded in the journal.

## Security notes

The journal file is written with mode `0600` and contains only data the operator has already sent over the network via the HTTP API. It does not contain TLS keys, passwords, or identity material -- the agent's identity stays in `agent_id` / `agent_key` alongside it.

When using management key encryption, dynamic mutations are already restricted to agents with the private key; the journal file itself is not additionally encrypted.
