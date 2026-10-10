# Sleep API

HTTP endpoints for managing mesh sleep mode.

## Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/sleep` | POST | Trigger mesh-wide sleep |
| `/wake` | POST | Trigger mesh-wide wake |
| `/sleep/status` | GET | Get current sleep status |

These endpoints require `http.remote_api: true` in configuration.

---

## POST /sleep

Trigger mesh-wide sleep mode.

### Request

```bash
curl -X POST http://localhost:8080/sleep
```

### Response

**Success (200)**:

```json
{
  "status": "triggered",
  "message": "sleep command triggered"
}
```

**Sleep Not Enabled (503)**:

```
sleep mode not enabled
```

**Error (500)**:

```json
{
  "status": "error",
  "error": "already sleeping"
}
```

### Behavior

When triggered:
1. A SLEEP_COMMAND is flooded to all connected peers
2. All peer connections are closed
3. SOCKS5 and listener services stop
4. The agent enters sleep mode with periodic polling

### Command Signing

If `signing_private_key` is configured, the sleep command is automatically signed with Ed25519. Agents with `signing_public_key` configured will verify the signature before processing.

---

## POST /wake

Trigger mesh-wide wake from sleep mode.

### Request

```bash
curl -X POST http://localhost:8080/wake
```

### Response

**Success (200)**:

```json
{
  "status": "triggered",
  "message": "wake command triggered"
}
```

**Sleep Not Enabled (503)**:

```
sleep mode not enabled
```

**Error (500)**:

```json
{
  "status": "error",
  "error": "not sleeping"
}
```

### Behavior

When triggered:
1. The agent reconnects to all configured peers
2. SOCKS5 and listener services restart
3. A WAKE_COMMAND is flooded to all connected peers
4. Normal operation resumes

### Command Signing

If `signing_private_key` is configured, the wake command is automatically signed with Ed25519. Agents with `signing_public_key` configured will verify the signature before processing.

---

## GET /sleep/status

Get current sleep mode status.

### Request

```bash
curl http://localhost:8080/sleep/status
```

### Response

**Success (200)**:

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

### Response Fields

| Field | Type | Description |
|-------|------|-------------|
| `state` | string | Current state: `AWAKE`, `SLEEPING`, or `POLLING` |
| `enabled` | boolean | Whether sleep mode is enabled |
| `sleep_start_time` | string (ISO 8601) | When sleep mode was entered |
| `last_poll_time` | string (ISO 8601) | When last poll cycle completed |
| `next_poll_time` | string (ISO 8601) | When next poll cycle is scheduled |
| `queued_peers` | integer | Number of sleeping peers with queued state |

### Notes

- If sleep mode is not configured, returns `state: "AWAKE"` and `enabled: false`
- Times are omitted when zero (e.g., no sleep has occurred)

---

## Error Responses

All endpoints may return:

| Status | Description |
|--------|-------------|
| 404 | Endpoint disabled (remote_api not enabled) |
| 405 | Method not allowed |
| 503 | Sleep mode not enabled in configuration |
| 500 | Internal error (see error message) |

:::note Signature Verification
Commands with invalid signatures are rejected by receiving agents (not the API endpoint). The API returns success when the command is sent, but agents with `signing_public_key` configured will drop commands that fail signature verification. Check agent logs for "signature verification failed" messages.
:::

---

## Examples

### Check Status Before Sleep

```bash
# Check current status
STATUS=$(curl -s http://localhost:8080/sleep/status | jq -r '.state')

if [ "$STATUS" = "AWAKE" ]; then
    # Put to sleep
    curl -X POST http://localhost:8080/sleep
fi
```

### Scheduled Sleep with Logging

```bash
#!/bin/bash
LOG_FILE="/var/log/muti-metroo-sleep.log"

echo "$(date): Triggering sleep" >> $LOG_FILE
RESULT=$(curl -s -X POST http://localhost:8080/sleep)
echo "$(date): Result: $RESULT" >> $LOG_FILE
```

### Health Check Integration

```bash
# For monitoring systems - check if agent is in expected state
EXPECTED_STATE="SLEEPING"  # or "AWAKE"

STATE=$(curl -s http://localhost:8080/sleep/status | jq -r '.state')
if [ "$STATE" != "$EXPECTED_STATE" ]; then
    echo "CRITICAL: Agent in unexpected state: $STATE (expected $EXPECTED_STATE)"
    exit 2
fi
echo "OK: Agent state is $STATE"
exit 0
```

### Wake All Agents via Different Entry Points

If you have multiple agents and one is unreachable, try waking via another:

```bash
for AGENT in "192.168.1.10:8080" "192.168.1.11:8080" "192.168.1.12:8080"; do
    if curl -s -X POST "http://$AGENT/wake" | grep -q "triggered"; then
        echo "Wake triggered via $AGENT"
        exit 0
    fi
done
echo "Failed to trigger wake via any agent"
exit 1
```
