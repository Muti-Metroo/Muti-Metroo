---
title: muti-metroo mesh-test
sidebar_label: mesh-test
sidebar_position: 20
description: Ask an agent to send a status request to every agent it knows about and report which ones answer.
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-inspecting.png" alt="Mole testing mesh" style={{maxWidth: '180px'}} />
</div>

# muti-metroo mesh-test

Asks a running agent to test connectivity to every agent it knows about. The agent sends a status request through the mesh to each one and reports whether it answered and how long the round trip took.

The command talks to the agent's HTTP API, which must have the dashboard endpoints enabled (`http.dashboard`, on by default). See [HTTP API configuration](../configuration/http.md).

Global flags (`--token`, `--yes`, `--no-color`) are described in [CLI overview](overview.md#global-flags).

## Synopsis

```bash
muti-metroo mesh-test [flags]
```

## Description

The agent at `--agent` builds its list of known agents (itself plus every agent in its agent table), then tests up to ten of them in parallel. Each remote agent has 10 seconds to answer; the local agent is always reported as reachable. The agent runs a fresh test every time the command is used.

When the agent has a management `public_key` but not the `private_key`, it does not reveal the mesh topology and reports only itself. See [Management keys](../configuration/management.md).

## Flags

| Flag | Default | Description |
| --- | --- | --- |
| `-a`, `--agent` | `localhost:8080` | Agent HTTP API address, as `host:port` or a URL. See [Agent address](overview.md#agent-address). |
| `-w`, `--timeout` | `30s` | Overall timeout for the request to the agent, as a Go duration. |
| `--json` | `false` | Print the agent's JSON response. |

`-t` is a deprecated alias for `--timeout`; it still works but prints a warning to stderr. See [Deprecated commands and flags](overview.md#deprecated-commands-and-flags).

## Output

```bash
muti-metroo mesh-test
```

```text
Testing mesh connectivity...

AGENT                STATUS       RESPONSE TIME
-----                ------       -------------
gateway-1            OK           local
exit-1               OK           42ms

Summary: 2/2 agents reachable (tested in 0.1s)
```

The `AGENT` column shows the display name, or the short agent ID when the agent has none. For an unreachable agent, `STATUS` is `FAILED` and the last column carries the error reported by the agent instead of a time.

With `--json`:

```json
{
  "local_agent": "abc123de",
  "test_time": "2026-10-11T14:54:18.695448+03:00",
  "duration_ms": 112,
  "total_count": 2,
  "reachable_count": 2,
  "results": [
    {
      "agent_id": "abc123def456789012345678901234ab",
      "short_id": "abc123de",
      "display_name": "gateway-1",
      "is_local": true,
      "reachable": true,
      "response_time_ms": 0
    },
    {
      "agent_id": "def456789012345678901234567890cd",
      "short_id": "def45678",
      "display_name": "exit-1",
      "is_local": false,
      "reachable": true,
      "response_time_ms": 42
    }
  ]
}
```

An unreachable agent has `"reachable": false`, `"response_time_ms": -1` (when it did not answer at all), and an `error` field.

## Examples

```bash
# Test through the local agent
muti-metroo mesh-test

# Test from a different agent's point of view
muti-metroo mesh-test -a 192.168.1.10:8080

# Allow more time in a large mesh
muti-metroo mesh-test -w 60s

# Alert when any agent is unreachable
result=$(muti-metroo mesh-test --json)
total=$(echo "$result" | jq '.total_count')
reachable=$(echo "$result" | jq '.reachable_count')
if [ "$total" != "$reachable" ]; then
  echo "ALERT: $reachable/$total agents reachable"
fi
```

## Exit status

The command exits 0 when the test completes, even if some agents are unreachable; check the summary or `reachable_count`. It exits 3 when the agent at `--agent` cannot be reached and 1 for any other error, including an HTTP error status such as a rejected bearer token. See [Exit codes](overview.md#exit-codes).

## See Also

- [status](status.md): show the state of a single agent
- [peer](peer.md): list direct peer connections with `peer status`
- [probe](probe.md): test a transport listener that is not yet part of the mesh
- [Dashboard API](../api/dashboard.md): the `/api/mesh-test` endpoint this command calls
