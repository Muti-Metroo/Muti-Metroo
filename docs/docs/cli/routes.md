---
title: routes
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-reading.png" alt="Mole viewing routes" style={{maxWidth: '180px'}} />
</div>

# muti-metroo routes

> **Deprecated.** Use [`muti-metroo route status`](route.md) instead. The top-level `routes` command will be removed in a future release. Same data, same flags.

Display the current routing table showing how traffic reaches exit agents.

```bash
# View routes on local agent
muti-metroo routes

# View routes on remote agent
muti-metroo routes -a 192.168.1.10:8080

# JSON output for scripting
muti-metroo routes --json
```

## Usage

```bash
muti-metroo routes [flags]
```

## Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--agent` | `-a` | `localhost:8080` | Agent HTTP API address (host:port or http(s):// URL) |
| `--json` | | `false` | Output in JSON format |

## Example Output

```
Route Table
===========
NETWORK              NEXT HOP        ORIGIN          METRIC   HOPS
-------              --------        ------          ------   ----
10.0.0.0/8           Agent-B         Agent-C         2        2
192.168.1.0/24       Agent-B         Agent-B         1        1
0.0.0.0/0            Agent-C         Agent-C         1        1

Total: 3 route(s)
```

## Output Fields

| Field | Description |
|-------|-------------|
| NETWORK | CIDR that this route handles |
| NEXT HOP | Immediate peer to forward traffic to |
| ORIGIN | Exit agent that advertised this route |
| METRIC | Total hop count to reach exit |
| HOPS | Same as metric |

## JSON Output

```bash
muti-metroo routes --json
```

```json
[
  {
    "network": "10.0.0.0/8",
    "next_hop_id": "abc123...",
    "next_hop_name": "Agent-B",
    "origin_id": "def456...",
    "origin_name": "Agent-C",
    "metric": 2,
    "hop_count": 2,
    "path_display": ["Agent-B", "Agent-C"]
  },
  {
    "network": "192.168.1.0/24",
    "next_hop_id": "abc123...",
    "next_hop_name": "Agent-B",
    "origin_id": "abc123...",
    "origin_name": "Agent-B",
    "metric": 1,
    "hop_count": 1,
    "path_display": ["Agent-B"]
  }
]
```

## Route Selection

When traffic needs to reach a destination, routes are selected using **longest-prefix match**:

1. **Most specific CIDR wins**: `/24` beats `/16` beats `/8` beats `/0`
2. **If tied, lowest metric wins**: Fewer hops is preferred

Example for destination `10.1.2.3`:
- `10.1.2.0/24` (metric 3) beats `10.0.0.0/8` (metric 1) - more specific
- If two `/24` routes exist, the one with lower metric wins

## Understanding the Output

### Direct Routes (Metric 1)

```
192.168.1.0/24       Agent-B         Agent-B         1        1
```

This route goes directly to Agent-B (next hop = origin). Traffic for `192.168.1.x` is forwarded to Agent-B, which handles it as an exit.

### Multi-Hop Routes (Metric > 1)

```
10.0.0.0/8           Agent-B         Agent-C         2        2
```

This route reaches Agent-C (origin) via Agent-B (next hop). Traffic for `10.x.x.x` is forwarded to Agent-B, which forwards it to Agent-C.

## Use Cases

### Verify Route Availability

```bash
# Check if routes exist for your target network
muti-metroo routes | grep "10.0.0.0"
```

### Debug Routing Issues

```bash
# See full route table to diagnose connectivity
muti-metroo routes

# Check route count matches expectations
muti-metroo routes --json | jq 'length'
```

### Monitor Route Changes

```bash
# Watch for route changes (requires watch command)
watch -n 5 'muti-metroo routes'
```

## Troubleshooting

### No Routes in Table

If `muti-metroo routes` shows no routes:
- No exit agents are configured in the mesh
- Routes haven't propagated yet (wait for advertise interval)
- Network connectivity issues between agents

### Missing Expected Route

If a specific route is missing:
- Check the exit agent is running and connected
- Verify the exit agent has the route configured
- Trigger route advertisement: `curl -X POST http://exit-agent:8080/routes/advertise`

## Related

- [status](/cli/status) - Agent status overview
- [peers](/cli/peers) - Connected peers
- [Routing Concepts](/concepts/routing) - How routing works
- [Exit Configuration](/configuration/exit) - Configure exit routes
