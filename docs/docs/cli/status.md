---
title: muti-metroo status
sidebar_label: status
sidebar_position: 12
description: Show whether an agent is running, with its peer, stream, and route counts and the state of its SOCKS5 and exit handlers.
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-reading.png" alt="Mole checking status" style={{maxWidth: '180px'}} />
</div>

# muti-metroo status

Show the current state of a running agent. The command reads the agent's `/healthz` endpoint over the HTTP API, so the agent must have `http.enabled: true`. `/healthz` is exempt from bearer-token authentication, so no `--token` is needed.

Global flags (`--token`, `--yes`, `--no-color`) are described in [CLI overview](overview.md#global-flags).

## Synopsis

```bash
muti-metroo status [flags]
```

## Flags

| Flag | Default | Description |
| --- | --- | --- |
| `-a`, `--agent` | `localhost:8080` | Agent HTTP API address, as `host:port` or an `http(s)://` URL with an optional path prefix. See [agent address](overview.md#agent-address). |
| `--json` | `false` | Print the response as JSON. |

## Output

```text
Agent Status
============
Status:       healthy
Running:      true
Peer Count:   1
Stream Count: 0
Route Count:  0
SOCKS5:       false
Exit Handler: false
```

| Field | JSON key | Description |
| --- | --- | --- |
| Status | `status` | `healthy` when the agent is running, `unavailable` when the HTTP server is up but the agent is not running. |
| Running | `running` | Whether the agent is running. |
| Peer Count | `peer_count` | Number of connected peers. |
| Stream Count | `stream_count` | Number of active streams. |
| Route Count | `route_count` | Number of entries in the route table. |
| SOCKS5 | `socks5_running` | Whether the SOCKS5 listener is running. |
| Exit Handler | `exit_handler_running` | Whether the exit handler is running. |

With `--json`:

```json
{
  "status": "healthy",
  "running": true,
  "peer_count": 1,
  "stream_count": 0,
  "route_count": 0,
  "socks5_running": false,
  "exit_handler_running": false
}
```

## Examples

Check the local agent:

```bash
muti-metroo status
```

Check an agent on another host:

```bash
muti-metroo status -a 192.168.1.10:8080
```

Use the JSON output in a health check script:

```bash
if muti-metroo status --json | jq -e '.status == "healthy"' > /dev/null; then
  echo "agent healthy"
else
  echo "agent unhealthy"
  exit 1
fi
```

## Exit status

The command exits 3 when the agent cannot be reached. It exits 0 whenever the agent answers, including when `status` is `unavailable`, so scripts that need a health verdict should check the `status` field as shown above.

## See Also

- [peer](peer.md): list connected peers with `peer status`
- [route](route.md): inspect the live route table with `route status`
- [sleep-status](sleep.md#sleep-status): check whether the agent is sleeping
- [Health API](../api/health.md): the `/health`, `/healthz`, and `/ready` endpoints
- [HTTP configuration](../configuration/http.md): enable the HTTP API and set its address
