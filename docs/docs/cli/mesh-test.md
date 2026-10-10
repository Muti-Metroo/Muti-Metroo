---
title: mesh-test
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-inspecting.png" alt="Mole testing mesh" style={{maxWidth: '180px'}} />
</div>

# muti-metroo mesh-test

Test connectivity to all known agents in the mesh network. Sends a status request to each agent and reports reachability along with response times.

**Quick test:**
```bash
# Test all mesh agents via local agent
muti-metroo mesh-test

# JSON output for scripting
muti-metroo mesh-test --json
```

## Synopsis

```bash
muti-metroo mesh-test [flags]
```

## What It Tests

1. **Agent discovery** - Queries the local agent for all known agents in the mesh
2. **Status request** - Sends a control-plane status request to each agent through the mesh
3. **Response timing** - Measures round-trip time for each agent's response

The test runs through the local agent's HTTP API, using the mesh network to reach remote agents.

## Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--agent` | `-a` | `localhost:8080` | Agent API address (host:port or http(s):// URL) |
| `--timeout` | `-t` | `30s` | Overall timeout for the test |
| `--json` | | `false` | Output in JSON format |
| `-h, --help` | | | Show help |

## Examples

### Basic Mesh Test

```bash
muti-metroo mesh-test
```

Output:

```
Testing mesh connectivity...

AGENT                STATUS       RESPONSE TIME
-----                ------       -------------
gateway-1            OK           local
exit-us-west         OK           45ms
exit-eu-east         OK           123ms
transit-1            OK           67ms
exit-offline         FAILED       timeout after 10s

Summary: 4/5 agents reachable (tested in 1.2s)
```

### JSON Output

```bash
muti-metroo mesh-test --json
```

Output:

```json
{
  "local_agent": "abc123de",
  "test_time": "2026-01-27T10:30:00Z",
  "duration_ms": 1200,
  "total_count": 5,
  "reachable_count": 4,
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
      "display_name": "exit-us-west",
      "is_local": false,
      "reachable": true,
      "response_time_ms": 45
    }
  ]
}
```

### Test via Different Agent

```bash
muti-metroo mesh-test -a 192.168.1.10:8080
```

### Custom Timeout

```bash
muti-metroo mesh-test -t 60s
```

## HTTP API

The `mesh-test` command calls the `/api/mesh-test` endpoint on the target agent.

| Method | Behavior |
|--------|----------|
| `POST /api/mesh-test` | Force a fresh connectivity test (used by CLI) |
| `GET /api/mesh-test` | Return cached results if available (30s cache TTL) |

```bash
# Via curl (fresh test)
curl -X POST http://localhost:8080/api/mesh-test | jq

# Via curl (cached results)
curl http://localhost:8080/api/mesh-test | jq
```

## Use Cases

### Pre-operation Verification

Before running remote operations (shell, file transfer), verify all agents are reachable:

```bash
muti-metroo mesh-test && muti-metroo shell exit-agent whoami
```

### Monitoring Script

Periodically check mesh health:

```bash
#!/bin/bash
result=$(muti-metroo mesh-test --json)
total=$(echo "$result" | jq '.total_count')
reachable=$(echo "$result" | jq '.reachable_count')
if [ "$total" != "$reachable" ]; then
  echo "ALERT: $reachable/$total agents reachable"
fi
```

### Multi-agent Diagnostics

Compare connectivity from different vantage points:

```bash
for agent in "localhost:8080" "192.168.1.10:8080" "10.0.0.1:8080"; do
  echo "=== From $agent ==="
  muti-metroo mesh-test -a "$agent"
  echo
done
```

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Test completed (check results for individual agent status) |
| 1 | Error (cannot connect to local agent or invalid flags) |

## Related

- [Status](/cli/status) - Show local agent status
- [Peers](/cli/peers) - List connected peers
- [Dashboard API](/api/dashboard) - Dashboard and mesh-test API endpoints
