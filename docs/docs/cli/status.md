---
title: status
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-reading.png" alt="Mole checking status" style={{maxWidth: '180px'}} />
</div>

# muti-metroo status

Check if an agent is running and view its current state.

```bash
# Check local agent
muti-metroo status

# Check remote agent
muti-metroo status -a 192.168.1.10:8080

# JSON output for scripting
muti-metroo status --json
```

## Usage

```bash
muti-metroo status [flags]
```

## Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--agent` | `-a` | `localhost:8080` | Agent HTTP API address (host:port or http(s):// URL) |
| `--json` | | `false` | Output in JSON format |

## Example Output

```
Agent Status
============
Status:       OK
Running:      true
Peer Count:   3
Stream Count: 12
Route Count:  5
SOCKS5:       true
Exit Handler: false
```

## Output Fields

| Field | Description |
|-------|-------------|
| Status | Agent health status (OK or error message) |
| Running | Whether the agent is running |
| Peer Count | Number of connected peer agents |
| Stream Count | Number of active streams |
| Route Count | Number of routes in the routing table |
| SOCKS5 | Whether SOCKS5 proxy is enabled |
| Exit Handler | Whether exit routing is enabled |

## JSON Output

```bash
muti-metroo status --json
```

```json
{
  "status": "OK",
  "running": true,
  "peer_count": 3,
  "stream_count": 12,
  "route_count": 5,
  "socks5_running": true,
  "exit_handler_running": false
}
```

## Use Cases

### Health Monitoring

```bash
# Simple health check for monitoring scripts
if muti-metroo status --json | jq -e '.status == "OK"' > /dev/null; then
  echo "Agent healthy"
else
  echo "Agent unhealthy"
  exit 1
fi
```

### Check Remote Agent

```bash
# Verify a remote agent is running before operations
muti-metroo status -a remote-host:8080
```

## Related

- [peers](/cli/peers) - List connected peers
- [routes](/cli/routes) - List route table
- [HTTP API - Health](/api/health) - Programmatic health checks
