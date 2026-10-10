---
title: peers
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-reading.png" alt="Mole listing peers" style={{maxWidth: '180px'}} />
</div>

# muti-metroo peers

> **Deprecated.** Use [`muti-metroo peer status`](peer.md) instead. The top-level `peers` command will be removed in a future release. Same data, same flags.

List all agents currently connected to this agent.

```bash
# List peers on local agent
muti-metroo peers

# List peers on remote agent
muti-metroo peers -a 192.168.1.10:8080

# JSON output for scripting
muti-metroo peers --json
```

## Usage

```bash
muti-metroo peers [flags]
```

## Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--agent` | `-a` | `localhost:8080` | Agent HTTP API address (host:port or http(s):// URL) |
| `--json` | | `false` | Output in JSON format |

## Example Output

```
Connected Peers
===============
ID           NAME                 STATE          ROLE       RTT
--           ----                 -----          ----       ---
abc123def456 Agent-B              connected      dialer     23ms
789xyz012345 Agent-C              connected      listener   15ms

Total: 2 peer(s)
```

## Output Fields

| Field | Description |
|-------|-------------|
| ID | Short agent ID (first 12 characters) |
| NAME | Agent display name |
| STATE | Connection state (`connected` or `UNRESPONSIVE` in red) |
| ROLE | `dialer` (this agent initiated) or `listener` (peer initiated) |
| RTT | Round-trip time in milliseconds (`-` if not measured) |

## JSON Output

```bash
muti-metroo peers --json
```

```json
[
  {
    "id": "abc123def456789012345678901234ab",
    "short_id": "abc123def456",
    "display_name": "Agent-B",
    "state": "connected",
    "rtt_ms": 23,
    "unresponsive": false,
    "is_dialer": true
  },
  {
    "id": "789xyz012345678901234567890123cd",
    "short_id": "789xyz012345",
    "display_name": "Agent-C",
    "state": "connected",
    "rtt_ms": 15,
    "unresponsive": false,
    "is_dialer": false
  }
]
```

## Understanding Roles

- **dialer**: This agent initiated the connection to the peer
- **listener**: The peer initiated the connection to this agent

Both roles are functionally equivalent once connected - traffic flows bidirectionally regardless of who initiated.

## Unresponsive Peers

Peers marked as `UNRESPONSIVE` (shown in red) have not responded to keepalive messages within the timeout period. This may indicate:

- Network connectivity issues
- The peer agent is overloaded
- The peer agent has crashed

Unresponsive peers will be automatically disconnected after the connection timeout.

## Use Cases

### Check Mesh Connectivity

```bash
# Verify all expected peers are connected
muti-metroo peers
```

### Scripted Peer Count Check

```bash
# Alert if peer count drops below threshold
PEER_COUNT=$(muti-metroo peers --json | jq 'length')
if [ "$PEER_COUNT" -lt 2 ]; then
  echo "Warning: Only $PEER_COUNT peers connected"
fi
```

## Related

- [status](/cli/status) - Agent status overview
- [routes](/cli/routes) - List route table
- [Dashboard API](/api/dashboard) - Topology and mesh status
