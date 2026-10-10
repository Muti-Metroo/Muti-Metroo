---
title: ICMP Ping WebSocket API
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-drilling.png" alt="Mole pinging" style={{maxWidth: '180px'}} />
</div>

# ICMP Ping WebSocket API

Send ICMP echo requests through agents to test connectivity and measure latency. The CLI handles this automatically - this reference is for building custom integrations.

**Using the CLI (recommended):**
```bash
muti-metroo ping abc123 8.8.8.8
muti-metroo ping -c 10 abc123 192.168.1.1
```

**WebSocket endpoint for custom clients:**
```
ws://localhost:8080/agents/{agent-id}/icmp
```

## WebSocket Endpoint

```
GET /agents/{agent-id}/icmp
```

### Subprotocol

The WebSocket uses the `muti-metroo-icmp` subprotocol.

## Message Protocol

All messages use JSON text format.

### Message Types

| Type | Direction | Description |
|------|-----------|-------------|
| `init` | Client -> Server | Initialize session with destination IP |
| `init_ack` | Server -> Client | Acknowledge session start |
| `echo` | Client -> Server | Send echo request |
| `reply` | Server -> Client | Echo reply received |
| `error` | Server -> Client | Error response |

## Session Flow

### 1. Connect

```
WebSocket: ws://localhost:8080/agents/abc123/icmp
Subprotocol: muti-metroo-icmp
```

### 2. Initialize Session

Send init message with destination IP:

```json
{
  "type": "init",
  "dest_ip": "8.8.8.8"
}
```

| Field | Type | Description |
|-------|------|-------------|
| `type` | string | Must be "init" |
| `dest_ip` | string | Destination IP address (IPv4) |

### 3. Receive Acknowledgment

**Success:**
```json
{
  "type": "init_ack",
  "success": true
}
```

**Failure:**
```json
{
  "type": "init_ack",
  "success": false,
  "error": "ICMP session failed: icmp not enabled"
}
```

### 4. Send Echo Requests

```json
{
  "type": "echo",
  "sequence": 1,
  "payload": "optional payload data"
}
```

| Field | Type | Description |
|-------|------|-------------|
| `type` | string | Must be "echo" |
| `sequence` | number | Sequence number for tracking |
| `payload` | string | Optional payload data |

### 5. Receive Replies

**Success:**
```json
{
  "type": "reply",
  "sequence": 1
}
```

**Timeout or error:**
```json
{
  "type": "error",
  "sequence": 1,
  "error": "timeout"
}
```

## Error Handling

Common init errors:

| Error | Cause |
|-------|-------|
| `invalid destination IP` | dest_ip is not a valid IP address |
| `ICMP session failed: icmp not enabled` | Target agent has ICMP disabled |
| `failed to open ICMP session` | Network or routing error |

## Custom Client Example

```javascript
const ws = new WebSocket(
  'ws://localhost:8080/agents/abc123/icmp',
  'muti-metroo-icmp'
);

ws.onopen = () => {
  // Initialize session
  ws.send(JSON.stringify({
    type: 'init',
    dest_ip: '8.8.8.8'
  }));
};

ws.onmessage = (event) => {
  const msg = JSON.parse(event.data);

  if (msg.type === 'init_ack' && msg.success) {
    // Send echo request
    ws.send(JSON.stringify({
      type: 'echo',
      sequence: 1,
      payload: ''
    }));
  } else if (msg.type === 'reply') {
    console.log(`Reply seq=${msg.sequence}`);
  } else if (msg.type === 'error') {
    console.log(`Error seq=${msg.sequence}: ${msg.error}`);
  }
};
```

## Requirements

The target agent must have ICMP enabled:

```yaml
icmp:
  enabled: true
```

## See Also

- [CLI - Ping](/cli/ping) - CLI reference
- [ICMP Configuration](/configuration/icmp) - Configure ICMP on agents
- [ICMP Relay Feature](/features/icmp-relay) - Feature overview
