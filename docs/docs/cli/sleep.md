# Sleep Commands

Commands for managing mesh sleep mode.

## sleep

Put the mesh into sleep mode.

```bash
muti-metroo sleep [flags]
```

### Description

Triggers mesh-wide sleep mode. When triggered:
- A sleep command floods to all connected agents
- All peer connections are closed
- SOCKS5 and listener services stop
- Agents periodically poll for queued messages

### Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--agent` | `-a` | `localhost:8080` | Agent API address (host:port or http(s):// URL) |

### Examples

```bash
# Put mesh to sleep via local agent
muti-metroo sleep

# Via a specific agent
muti-metroo sleep -a 192.168.1.10:8080
```

### Output

```
Sleep mode triggered: sleep command triggered
```

### Requirements

- Sleep mode must be enabled in the agent configuration
- Agent must have HTTP API enabled and accessible

---

## wake

Wake the mesh from sleep mode.

```bash
muti-metroo wake [flags]
```

### Description

Triggers mesh-wide wake from sleep mode. When triggered:
- Agent reconnects to all configured peers
- SOCKS5 and listener services restart
- A wake command floods to all connected agents
- Normal operation resumes

### Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--agent` | `-a` | `localhost:8080` | Agent API address (host:port or http(s):// URL) |

### Examples

```bash
# Wake mesh via local agent
muti-metroo wake

# Via a specific agent
muti-metroo wake -a 192.168.1.10:8080
```

### Output

```
Wake mode triggered: wake command triggered
```

---

## sleep-status

Display current sleep mode status.

```bash
muti-metroo sleep-status [flags]
```

### Description

Shows the current sleep state and timing information for an agent.

### Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--agent` | `-a` | `localhost:8080` | Agent API address (host:port or http(s):// URL) |
| `--json` | | `false` | Output in JSON format |

### Examples

```bash
# Check local agent status
muti-metroo sleep-status

# Check remote agent with JSON output
muti-metroo sleep-status -a 192.168.1.10:8080 --json
```

### Output

Standard output:

```
Sleep Mode Status
=================
State:          SLEEPING
Enabled:        true
Sleep Started:  2026-01-19T10:30:00Z
Last Poll:      2026-01-19T10:35:00Z
Next Poll:      2026-01-19T10:40:00Z
Queued Peers:   2
```

JSON output:

```json
{
  "state": "SLEEPING",
  "enabled": true,
  "sleep_start_time": "2026-01-19T10:30:00Z",
  "last_poll_time": "2026-01-19T10:35:00Z",
  "next_poll_time": "2026-01-19T10:40:00Z",
  "queued_peers": 2
}
```

### Status Fields

| Field | Description |
|-------|-------------|
| State | Current state: `AWAKE`, `SLEEPING`, or `POLLING` |
| Enabled | Whether sleep mode is enabled in configuration |
| Sleep Started | When sleep mode was entered |
| Last Poll | When the last poll cycle completed |
| Next Poll | When the next poll cycle is scheduled |
| Queued Peers | Number of sleeping peers with queued messages |

---

## Workflow Examples

### Schedule Sleep During Off-Hours

Use cron to sleep at night and wake in the morning:

```bash
# Add to crontab
# Sleep at 10 PM
0 22 * * * /usr/local/bin/muti-metroo sleep -a localhost:8080

# Wake at 6 AM
0 6 * * * /usr/local/bin/muti-metroo wake -a localhost:8080
```

### Monitoring Script

Check sleep status and alert if unexpectedly awake:

```bash
#!/bin/bash
STATUS=$(muti-metroo sleep-status --json | jq -r '.state')
if [ "$STATUS" = "AWAKE" ]; then
    echo "Warning: Agent is awake when it should be sleeping"
    # Send alert...
fi
```

### Wake for Maintenance

```bash
# Wake the mesh
muti-metroo wake

# Perform maintenance tasks
muti-metroo status
muti-metroo peers

# Return to sleep
muti-metroo sleep
```
