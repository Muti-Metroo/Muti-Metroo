---
title: ICMP Ping WebSocket API
description: WebSocket endpoint and JSON messages for sending ICMP echo requests from a remote agent.
sidebar_position: 12
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-drilling.png" alt="Mole pinging" style={{maxWidth: '180px'}} />
</div>

# ICMP Ping WebSocket API

The ICMP endpoint sends ICMP echo requests from a remote agent to a destination and streams the replies over a WebSocket. The [`muti-metroo ping`](/cli/ping) command is a client of this endpoint; this page is for building other clients.

## `GET /agents/{id}/icmp`

```text
ws://localhost:8080/agents/331974cd4a0be0d7f945ed9d367a0211/icmp
```

| Item | Value |
| ---- | ----- |
| Group | Remote API (`http.remote_api`) |
| Subprotocol | `muti-metroo-icmp` |
| Query parameter `token` | Bearer token, for clients that cannot send an `Authorization` header |
| Target | A remote agent with `icmp.enabled: true` (the default); the local agent's own ID is not reachable |

No management signing key is needed. Session limits and timeouts come from the target's [`icmp` settings](/configuration/icmp).

## Messages

All messages are JSON text messages with a `type` field.

| `type` | Direction | Fields |
| ------ | --------- | ------ |
| `init` | client to server | `dest_ip`: destination IP address. Must be the first message. |
| `init_ack` | server to client | `success`; `error` when `success` is `false` |
| `echo` | client to server | `sequence`: number from 0 to 65535; `payload`: optional string |
| `reply` | server to client | `sequence`; `src_ip`: address that answered, when known |
| `error` | server to client | `sequence`; `error`: reason, for example a timeout |

## Session flow

1. Open the WebSocket and send `init`:

   ```json
   {"type": "init", "dest_ip": "192.0.2.10"}
   ```

2. Read `init_ack`:

   ```json
   {"type": "init_ack", "success": true}
   ```

   On failure the server sends `init_ack` with `success: false` and closes the WebSocket:

   ```json
   {"type": "init_ack", "success": false, "error": "invalid destination IP"}
   ```

3. Send one `echo` per request and read a `reply` or `error` for each:

   ```json
   {"type": "echo", "sequence": 1}
   ```

   ```json
   {"type": "reply", "sequence": 1, "src_ip": "192.0.2.10"}
   ```

Close the WebSocket to end the session. The target also ends idle sessions after `icmp.idle_timeout`.

## Example client

```javascript
const ws = new WebSocket(
  "ws://localhost:8080/agents/331974cd4a0be0d7f945ed9d367a0211/icmp",
  "muti-metroo-icmp"
);

ws.onopen = () => ws.send(JSON.stringify({ type: "init", dest_ip: "192.0.2.10" }));

ws.onmessage = (event) => {
  const msg = JSON.parse(event.data);
  if (msg.type === "init_ack" && msg.success) {
    ws.send(JSON.stringify({ type: "echo", sequence: 1 }));
  } else if (msg.type === "reply") {
    console.log(`reply seq=${msg.sequence} from ${msg.src_ip}`);
    ws.close();
  } else {
    console.log(msg);
    ws.close();
  }
};
```

## Errors

| Situation | Result |
| --------- | ------ |
| First message is not valid JSON or not `init` | WebSocket closed with code 1002 |
| `dest_ip` is not an IP address | `init_ack` with `error: "invalid destination IP"` |
| Target unreachable, ICMP disabled on the target, or session limit reached | `init_ack` with the error |
| Agent has no ICMP support | HTTP `503 ICMP not available` before the upgrade |

## See Also

- [CLI - ping](/cli/ping) - Command-line client
- [ICMP Configuration](/configuration/icmp) - Enable ICMP and set limits
- [ICMP Relay](/features/icmp-relay) - Feature overview
