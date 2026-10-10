---
title: probe
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-inspecting.png" alt="Mole probing" style={{maxWidth: '180px'}} />
</div>

# muti-metroo probe

Check if you can reach a listener before deploying an agent. Useful for testing firewall rules and TLS configuration without running a full agent.

**Quick test:**
```bash
# Test a QUIC listener
muti-metroo probe server.example.com:4433

# Test different transports to find what's not blocked
muti-metroo probe --transport quic server.example.com:4433   # UDP
muti-metroo probe --transport h2 server.example.com:443      # HTTPS
muti-metroo probe --transport ws server.example.com:443      # WebSocket
```

## Synopsis

```bash
muti-metroo probe [flags] <address>
```

## What It Tests

1. **Transport-level connection** - Establishes a TCP/TLS connection using the specified transport (QUIC, HTTP/2, or WebSocket)
2. **Protocol handshake** - Performs a PEER_HELLO exchange to verify it's a real Muti Metroo listener

The probe operates standalone - no running agent needed.

## Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--transport` | `-T` | `quic` | Transport type: `quic`, `h2`, `ws` |
| `--path` | | `/mesh` | HTTP path for h2/ws transports |
| `--timeout` | `-t` | `10s` | Connection timeout |
| `--ca` | | | CA certificate file for TLS verification |
| `--cert` | | | Client certificate file for mTLS |
| `--key` | | | Client key file for mTLS |
| `--insecure` | | `false` | Skip TLS certificate verification |
| `--plaintext` | | `false` | Plaintext mode (no TLS) for WebSocket behind reverse proxy |
| `--json` | | `false` | Output results as JSON |
| `-h, --help` | | | Show help |

## Examples

### Test a QUIC Listener

QUIC is the default transport:

```bash
muti-metroo probe server.example.com:4433
```

Output on success:

```
Probing quic://server.example.com:4433...

[OK] Connection successful!
  Transport:    quic
  Address:      server.example.com:4433
  Remote ID:    abc123def456789012345678
  Display Name: Agent-B
  RTT:          23ms
```

### Test an HTTP/2 Listener

```bash
muti-metroo probe --transport h2 server.example.com:443 --path /mesh
```

### Test a WebSocket Listener

```bash
muti-metroo probe --transport ws server.example.com:443 --path /mesh
```

### Test Plaintext WebSocket (Behind Reverse Proxy)

When the listener is behind a reverse proxy that handles TLS termination:

```bash
muti-metroo probe --transport ws --plaintext localhost:8080 --path /mesh
```

### Test with Self-Signed Certificate

When testing against a listener with a self-signed certificate:

```bash
muti-metroo probe --insecure server.example.com:4433
```

### Test with Specific CA Certificate

When using a private CA:

```bash
muti-metroo probe --ca ./certs/ca.crt server.example.com:4433
```

### Test with mTLS (Mutual TLS)

When the listener requires client authentication:

```bash
muti-metroo probe --ca ./certs/ca.crt --cert ./certs/client.crt --key ./certs/client.key server.example.com:4433
```

### JSON Output for Scripting

```bash
muti-metroo probe --json server.example.com:4433
```

Output:

```json
{
  "success": true,
  "transport": "quic",
  "address": "server.example.com:4433",
  "remote_id": "abc123def456789012345678",
  "remote_display_name": "Agent-B",
  "rtt_ms": 23
}
```

### Failed Connection Example

```bash
muti-metroo probe server.example.com:4433
```

Output on failure:

```
Probing quic://server.example.com:4433...

[FAILED] Connection failed
  Transport:  quic
  Address:    server.example.com:4433
  Error:      Connection timed out - firewall may be blocking
```

## Error Messages

The probe provides helpful error messages for common failure scenarios:

| Error Message | Likely Cause |
|---------------|--------------|
| Could not resolve hostname | DNS lookup failed |
| Connection refused - listener not running or port blocked | No process listening on the port |
| Connection timed out - firewall may be blocking | Firewall dropping packets |
| TLS error - certificate signed by unknown authority | Need `--ca` or `--insecure` |
| TLS error - certificate has expired | Listener certificate expired |
| Connected but handshake failed - not a Muti Metroo listener? | Port is open but not a Muti Metroo listener |
| Connected but received invalid response | Protocol mismatch |

## Use Cases

### Pre-deployment Verification

Before deploying an agent to a remote machine, verify the listener is reachable:

```bash
# On the remote machine (or a machine with similar network access)
muti-metroo probe --insecure relay.example.com:4433
```

### Firewall Troubleshooting

Test different transports to identify what's blocked:

```bash
# Test QUIC (UDP)
muti-metroo probe --transport quic server.example.com:4433

# Test HTTP/2 (TCP, HTTPS)
muti-metroo probe --transport h2 server.example.com:443

# Test WebSocket (TCP, HTTPS)
muti-metroo probe --transport ws server.example.com:443
```

### TLS Configuration Validation

Verify TLS is configured correctly:

```bash
# Should work with proper CA
muti-metroo probe --ca ./certs/ca.crt server.example.com:4433

# Should fail without CA (unless using --insecure)
muti-metroo probe server.example.com:4433
```

## Wizard Integration

The setup wizard automatically tests peer connectivity when configuring peer connections. After entering peer details, the wizard will probe the listener and show results:

```
[INFO] Testing connectivity to peer...
[OK] Connected successfully!
[INFO] Remote agent: Agent-B (abc123def456)
[INFO] Round-trip time: 23ms
```

If the connection fails, you can choose to:

1. Continue anyway (set up the listener later)
2. Retry the connection test
3. Re-enter peer configuration
4. Skip this peer

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Probe successful |
| 1 | Probe failed or error |

---

# muti-metroo probe listen

Start a test listener to validate transport configurations before deploying a full agent. The listener accepts probe connections and responds to handshakes, making it useful for testing TLS certificates, firewall rules, and transport settings.

## Synopsis

```bash
muti-metroo probe listen [flags]
```

## What It Does

1. **Starts a minimal listener** - Binds to the specified address using the chosen transport
2. **Accepts probe connections** - Waits for incoming connections from `muti-metroo probe`
3. **Responds to handshakes** - Performs PEER_HELLO/PEER_HELLO_ACK exchange
4. **Reports connection events** - Displays information about each connection attempt

The listener runs until interrupted (Ctrl+C).

## Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--transport` | `-T` | `quic` | Transport type: `quic`, `h2`, `ws` |
| `--address` | `-a` | `0.0.0.0:4433` | Listen address |
| `--path` | | `/mesh` | HTTP path for h2/ws transports |
| `--cert` | | | TLS certificate file (ephemeral cert if not provided) |
| `--key` | | | TLS private key file (ephemeral key if not provided) |
| `--ca` | | | CA certificate for client verification (mTLS) |
| `--plaintext` | | `false` | Plaintext mode (no TLS) for WebSocket behind reverse proxy |
| `--name` | `-n` | `probe-listener` | Display name for this listener |
| `--json` | | `false` | Output connection events as JSON |
| `--alpn` | | | Custom ALPN protocol |
| `--http-header` | | | Custom HTTP header for h2 transport |
| `--ws-subprotocol` | | | Custom WebSocket subprotocol |
| `-h, --help` | | | Show help |

## Examples

### Basic QUIC Listener

Start a listener with ephemeral self-signed certificates (no cert files needed):

```bash
muti-metroo probe listen -T quic -a 0.0.0.0:4433
```

Output:

```
Probe Listener
==============
Transport:  quic
Address:    0.0.0.0:4433
Name:       probe-listener
TLS:        enabled (ephemeral certificates)

Listening for connections... (Ctrl+C to stop)

[2026-01-21 10:30:45] [OK] 192.168.1.100:54321
  Remote ID:   abc123def456789012345678
  Remote Name: probe
  RTT:         5ms
```

### HTTP/2 Listener

```bash
muti-metroo probe listen -T h2 -a 0.0.0.0:443 --path /mesh
```

### WebSocket Listener

```bash
muti-metroo probe listen -T ws -a 0.0.0.0:443 --path /mesh
```

### Plaintext WebSocket (Behind Reverse Proxy)

When running behind nginx, Caddy, or another reverse proxy that handles TLS:

```bash
muti-metroo probe listen -T ws -a 127.0.0.1:8080 --path /mesh --plaintext
```

### With Static TLS Certificates

```bash
muti-metroo probe listen -T quic -a 0.0.0.0:4433 \
  --cert ./certs/server.crt \
  --key ./certs/server.key
```

### With mTLS (Require Client Certificates)

```bash
muti-metroo probe listen -T quic -a 0.0.0.0:4433 \
  --cert ./certs/server.crt \
  --key ./certs/server.key \
  --ca ./certs/ca.crt
```

### JSON Output for Scripting

```bash
muti-metroo probe listen --json -T quic -a 0.0.0.0:4433
```

Each connection event is output as a JSON line:

```json
{"timestamp":"2026-01-21T10:30:45Z","remote_addr":"192.168.1.100:54321","remote_id":"abc123def456","remote_name":"probe","success":true,"rtt_ms":5}
```

### Custom Display Name

```bash
muti-metroo probe listen --name "test-server-east" -T quic -a 0.0.0.0:4433
```

## Use Cases

### TLS Certificate Validation

Test that your certificates work correctly before deploying:

```bash
# Terminal 1: Start listener with your certificates
muti-metroo probe listen -T quic -a 0.0.0.0:4433 \
  --cert ./certs/server.crt --key ./certs/server.key

# Terminal 2: Test connection with CA verification
muti-metroo probe --ca ./certs/ca.crt localhost:4433
```

### mTLS Configuration Testing

Verify mutual TLS is configured correctly:

```bash
# Terminal 1: Listener requiring client certs
muti-metroo probe listen -T quic -a 0.0.0.0:4433 \
  --cert ./certs/server.crt --key ./certs/server.key \
  --ca ./certs/ca.crt

# Terminal 2: Client with certificate
muti-metroo probe --ca ./certs/ca.crt \
  --cert ./certs/client.crt --key ./certs/client.key \
  localhost:4433
```

### Reverse Proxy Testing

Test WebSocket configuration behind a reverse proxy:

```bash
# Start plaintext listener (proxy handles TLS)
muti-metroo probe listen -T ws -a 127.0.0.1:8080 --path /mesh --plaintext

# Test through the proxy
muti-metroo probe -T ws proxy.example.com:443 --path /mesh
```

### Network Path Verification

Test that a specific port/protocol is reachable:

```bash
# On server: start listener
muti-metroo probe listen -T quic -a 0.0.0.0:4433

# On client: verify connectivity
muti-metroo probe --insecure server.example.com:4433
```

## Related

- [Setup Wizard](/cli/setup) - Interactive agent configuration
- [Transports](/concepts/transports) - Transport protocol details
- [TLS Certificates](/configuration/tls-certificates) - TLS certificate setup
- [Troubleshooting](/troubleshooting/common-issues) - Common issues and solutions
- [Probe HTTP API](/api/probe) - Run the same probe over HTTP from a dashboard
