---
title: Dynamic Config Persistence
description: How changes made to a running agent through the CLI or HTTP API are journaled in the data directory and restored after a restart.
sidebar_position: 21
---

# Dynamic Config Persistence

Changes made to a running agent through the CLI or HTTP API are written to a journal in `agent.data_dir`. When the agent starts, it replays the journal, so runtime changes survive restarts. No configuration key controls this: setting `agent.data_dir` turns it on, and leaving `data_dir` empty keeps runtime changes in memory only.

## What is persisted

| CLI command                                         | HTTP endpoint                   |
| --------------------------------------------------- | ------------------------------- |
| `muti-metroo peer add`, `peer remove`               | `/peers/manage`                 |
| `muti-metroo listener add`, `listener remove`       | `/listeners/manage`             |
| `muti-metroo forward add`, `forward remove`         | `/forward/manage`               |
| `muti-metroo forward endpoint add`, `endpoint remove` | `/forward/endpoints/manage`   |
| `muti-metroo route add`, `route remove`             | `/routes/manage`                |
| `muti-metroo route comment`, `route source-ip`, `route interface` | `/routes/manage`  |
| `muti-metroo host add`, `host remove`               | `/hosts/manage`                 |
| `muti-metroo host suffix add`, `host suffix remove` | `/hosts/suffixes/manage`        |
| `muti-metroo display-name set`                      | `/display-name/manage`          |

Entries from the configuration file are not journaled. Route comment, source IP, and interface changes are journaled for configured routes too, and override the file on restart.

## Journal file

| Property    | Value                                         |
| ----------- | --------------------------------------------- |
| Location    | `<agent.data_dir>/dynamic-config.log`         |
| Format      | JSON Lines, one event per line                |
| Permissions | `0600`                                        |
| Writes      | Appended and synced to disk after each change |

```text
{"op":"display_name_set","ts":1712345678901234567,"name":"gateway-us-east"}
{"op":"route_add","ts":1712345678912345678,"network":"10.0.0.0/8","metric":3,"comment":"office LAN"}
{"op":"forward_add","ts":1712345678923456789,"key":"web","address":":9090","max_connections":100}
{"op":"peer_add","ts":1712345678934567890,"address":"peer.example.com:4433","transport":"quic"}
{"op":"host_add","ts":1712345678967890123,"name":"target-11.mesh","ipv4":"10.8.0.11"}
```

The journal contains no keys or passwords. The agent's identity files are separate files in the same directory.

## Replay at startup

After the agent has started, it reads the journal and applies each event through the same code path as the original CLI or API request, in this order: display name, routes, forward listeners, forward endpoints, peers, transport listeners, mesh host suffixes, mesh hosts.

- An event that fails, for example a forward listener whose port is now taken, is logged as a warning and skipped; the rest are applied.
- An event that conflicts with the configuration file, such as a peer address or route CIDR that the file now also lists, fails and is skipped.
- If any line of the journal cannot be parsed, the whole journal is skipped and a warning names the line number.

After replay, the agent rewrites the journal to the minimum set of events that reproduces the current state, so it does not grow with churn. Replayed entries are still listed as dynamic by `peer list`, `listener list`, `forward list`, and `route list`.

## When the journal cannot be written

If the journal cannot be written, for example because `data_dir` is read-only or the disk is full, the change is still applied in memory and the agent logs a warning:

```text
time=2026-10-11T14:50:05.746+03:00 level=WARN msg="persist dynamic config failed; change is ephemeral" error="open /data/dynamic-config.log: permission denied" op=peer_add path=/data/dynamic-config.log
```

The change is lost at the next restart.

## Inspecting and resetting

```bash
jq . /var/lib/muti-metroo/dynamic-config.log
```

To drop all runtime changes, stop the agent, delete the file, and start the agent again:

```bash
systemctl stop muti-metroo
rm /var/lib/muti-metroo/dynamic-config.log
systemctl start muti-metroo
```

The file can be edited by hand while the agent is stopped.

## See Also

- [Agent](/configuration/agent#data-directory) for the data directory and the other files in it
- [peer CLI](/cli/peer), [route CLI](/cli/route), and [forward CLI](/cli/forward) for the commands that create journal entries
- [Mesh Hosts](/configuration/hosts) for runtime host and suffix changes
- [Management](/configuration/management#command-signing-keys) for the signing keys runtime changes require
