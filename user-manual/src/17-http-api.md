# HTTP API

The HTTP API provides endpoints for health monitoring, status, and management.

## Configuration

```yaml
http:
  enabled: true # default: false (HTTP API is opt-in)
  address: ":8080"
  read_timeout: 10s
  write_timeout: 10s
  token_hash:
    "" # bcrypt hash of bearer token; empty = no auth.
    # When set, all non-health endpoints require
    # Authorization: Bearer <token> or ?token=<token>.
    # Generate with `muti-metroo hash` (default cost 12).
  minimal: false # Only /health, /healthz, /ready
  pprof: false # /debug/pprof/* (default: true; disable in prod)
  dashboard:
    true # /api/topology, /api/dashboard, /api/nodes,
    # /api/mesh-test, /api/probe, /api/setup-defaults
  remote_api:
    true # /agents/*, /routes/*, /forward/*, /peers/*,
    # /hosts/*, /sleep, /wake
```

## Endpoint Control

| Setting             | Effect                                                                                    | Default |
| ------------------- | ----------------------------------------------------------------------------------------- | ------- |
| `minimal: true`     | Serve only `/health`, `/healthz`, `/ready` (overrides every flag below)                   | false   |
| `pprof: false`      | Disable `/debug/pprof/*`                                                                  | true    |
| `dashboard: false`  | Disable dashboard `/api/*` (topology, dashboard, nodes, mesh-test, probe, setup-defaults) | true    |
| `remote_api: false` | Disable `/agents/*`, dynamic-management endpoints, and sleep/wake                         | true    |

Disabled endpoints return HTTP 404 and log access attempts at debug level. Health endpoints (`/health`, `/healthz`, `/ready`), the splash page (`/`), and `/logo.png` are always public, even with `token_hash` set.

## Health Endpoints

### GET /health

Basic health check, returns "OK":

```bash
curl http://localhost:8080/health
# Output: OK
```

### GET /healthz

Detailed health with JSON stats:

```bash
curl http://localhost:8080/healthz | jq
```

Response:

```json
{
  "status": "healthy",
  "running": true,
  "peer_count": 2,
  "stream_count": 5,
  "route_count": 3,
  "socks5_running": true,
  "exit_handler_running": true,
  "can_sign_management": false,
  "accepts_signed_management": true
}
```

`accepts_signed_management` is true when the agent has
`management.signing_public_key` set (so it can verify privileged
requests). `can_sign_management` is true when the agent also has
`management.signing_private_key` set (so it can initiate them). Web
dashboards use these flags to enable or hide the management UI.

### GET /ready

Readiness probe for orchestration systems:

```bash
curl http://localhost:8080/ready
```

## Dashboard API Endpoints

### GET /api/topology

Mesh topology data:

```bash
curl http://localhost:8080/api/topology | jq
```

### GET /api/dashboard

Dashboard overview with agent info, stats, peers, and routes:

```bash
curl http://localhost:8080/api/dashboard | jq
```

### GET/POST /api/mesh-test

Test connectivity to all known agents in the mesh. GET returns cached results
(30-second TTL), POST forces a fresh test:

```bash
# Fresh test
curl -X POST http://localhost:8080/api/mesh-test | jq

# Cached results
curl http://localhost:8080/api/mesh-test | jq
```

Response:

```json
{
  "local_agent": "abc123de",
  "test_time": "2026-01-27T10:30:00Z",
  "duration_ms": 1200,
  "total_count": 5,
  "reachable_count": 4,
  "results": [
    {
      "agent_id": "abc123def456...",
      "short_id": "abc123de",
      "display_name": "gateway-1",
      "is_local": true,
      "reachable": true,
      "response_time_ms": 0
    }
  ]
}
```

Also available via CLI: `muti-metroo mesh-test`

### POST /api/probe

Probe a single Muti Metroo listener (dial + `PEER_HELLO` handshake) and report the
result. Same operation as the `muti-metroo probe` CLI, exposed over HTTP for
dashboards and other tools. The probe never establishes a real peer
connection.

```bash
curl -X POST http://localhost:8080/api/probe \
  -H "Content-Type: application/json" \
  -d '{"transport":"quic","address":"agent2.example.com:4433"}' | jq
```

Response (success):

```json
{
  "success": true,
  "transport": "quic",
  "address": "agent2.example.com:4433",
  "remote_id": "abc123def456...",
  "remote_display_name": "agent-eu-west",
  "rtt_ms": 23
}
```

Response (handshake failure, still HTTP 200):

```json
{
  "success": false,
  "transport": "quic",
  "address": "agent2.example.com:9999",
  "error": "Connection refused - listener not running or port blocked"
}
```

Optional fields in the request body include `path`, `timeout_ms` (default
10000, max 60000), `strict_verify`, `plaintext` (ws transport only),
`ca_cert_pem`, `client_cert_pem`, `client_key_pem`, and the protocol identifier overrides
`alpn_protocol`, `http_header`, `ws_subprotocol`. TLS material must be
provided inline as PEM; the API never accepts filesystem paths.

To probe a listener from the perspective of another agent in the mesh, use
the remote variant:

```bash
curl -X POST http://localhost:8080/agents/abc123def456/probe \
  -H "Content-Type: application/json" \
  -d '{"transport":"quic","address":"10.0.0.4:4433"}' | jq
```

### GET /api/nodes

Detailed node info for all known agents:

```bash
curl http://localhost:8080/api/nodes | jq
```

## Remote Agent Endpoints

Query other agents through the mesh:

### GET /agents

List all known agents:

```bash
curl http://localhost:8080/agents | jq
```

### GET /agents/{agent-id}

Get status from a specific agent:

```bash
curl http://localhost:8080/agents/abc123def456 | jq
```

### GET /agents/{agent-id}/routes

Get route table from a specific agent:

```bash
curl http://localhost:8080/agents/abc123def456/routes | jq
```

### GET /agents/{agent-id}/peers

Get peer list from a specific agent:

```bash
curl http://localhost:8080/agents/abc123def456/peers | jq
```

### POST /agents/{agent-id}/file/browse

Browse the filesystem on a remote agent (list, stat, roots, chmod, delete):

```bash
curl -X POST http://localhost:8080/agents/abc123/file/browse \
  -H "Content-Type: application/json" \
  -d '{"action":"list","path":"/tmp"}'
```

## Management Endpoints

### POST /routes/advertise

Trigger immediate route advertisement:

```bash
curl -X POST http://localhost:8080/routes/advertise
```

Response:

```json
{
  "status": "triggered",
  "message": "route advertisement triggered"
}
```

### POST /routes/manage

Add, remove, or list dynamic CIDR exit routes:

```bash
# Add a route
curl -X POST http://localhost:8080/routes/manage \
  -H "Content-Type: application/json" \
  -d '{"action":"add","network":"10.0.0.0/8"}'

# Add with custom metric
curl -X POST http://localhost:8080/routes/manage \
  -H "Content-Type: application/json" \
  -d '{"action":"add","network":"192.168.0.0/16","metric":5}'

# List routes
curl -X POST http://localhost:8080/routes/manage \
  -H "Content-Type: application/json" \
  -d '{"action":"list"}'

# Remove a route
curl -X POST http://localhost:8080/routes/manage \
  -H "Content-Type: application/json" \
  -d '{"action":"remove","network":"10.0.0.0/8"}'
```

Response:

```json
{
  "status": "success",
  "message": "route added",
  "routes": [
    {
      "network": "10.0.0.0/8",
      "metric": 1,
      "source": "dynamic"
    }
  ]
}
```

### POST /agents/{agent-id}/routes/manage

Manage routes on a remote agent:

```bash
curl -X POST http://localhost:8080/agents/abc123def456/routes/manage \
  -H "Content-Type: application/json" \
  -d '{"action":"add","network":"10.0.0.0/8"}'
```

### POST /route/trace

Explain which route the agent would pick for a given destination. Runs the same
longest-prefix / metric / origin-ID selection used by the live stream open
path, and returns every matching CIDR route with the winner and the reason
each loser lost. Read-only diagnostic; no management-key signature required.

```bash
curl -X POST http://localhost:8080/route/trace \
  -H "Content-Type: application/json" \
  -d '{"dest":"8.8.8.8"}' | jq
```

Response (abridged):

```json
{
  "status": "ok",
  "dest": "8.8.8.8",
  "ips": [
    {
      "ip": "8.8.8.8",
      "candidates": [
        {
          "network": "8.8.8.0/24",
          "prefix_len": 24,
          "metric": 2,
          "winner": true
        },
        {
          "network": "0.0.0.0/0",
          "prefix_len": 0,
          "metric": 1,
          "winner": false,
          "reason": "shorter prefix"
        }
      ]
    }
  ]
}
```

For a hostname, the agent resolves the name with its own resolver and the
response carries one trace per resolved IP plus a `domain` block for the
domain-route lookup.

To run the trace from a remote agent's point of view (uses that agent's
routing tables and resolver) use the remote variant:

```bash
curl -X POST http://localhost:8080/agents/abc123def456/route/trace \
  -H "Content-Type: application/json" \
  -d '{"dest":"10.1.2.3"}' | jq
```

### POST /forward/manage

Add, remove, or list dynamic forward listeners:

```bash
# Add a forward listener
curl -X POST http://localhost:8080/forward/manage \
  -H "Content-Type: application/json" \
  -d '{"action":"add","key":"web-server","address":":9090"}'

# List forward listeners
curl -X POST http://localhost:8080/forward/manage \
  -H "Content-Type: application/json" \
  -d '{"action":"list"}'

# Remove a forward listener
curl -X POST http://localhost:8080/forward/manage \
  -H "Content-Type: application/json" \
  -d '{"action":"remove","key":"web-server"}'
```

### POST /agents/{agent-id}/forward/manage

Manage forward listeners on a remote agent:

```bash
curl -X POST http://localhost:8080/agents/abc123def456/forward/manage \
  -H "Content-Type: application/json" \
  -d '{"action":"add","key":"web-server","address":":9090"}'
```

### POST /forward/endpoints/manage

Add, remove, or list dynamic forward endpoints (exit-side `key -> host:port`
mappings):

```bash
# Register a routing key on the exit side
curl -X POST http://localhost:8080/forward/endpoints/manage \
  -H "Content-Type: application/json" \
  -d '{"action":"add","key":"web-server","target":"127.0.0.1:3000"}'

# List endpoints
curl -X POST http://localhost:8080/forward/endpoints/manage \
  -H "Content-Type: application/json" \
  -d '{"action":"list"}'

# Remove a dynamic endpoint
curl -X POST http://localhost:8080/forward/endpoints/manage \
  -H "Content-Type: application/json" \
  -d '{"action":"remove","key":"web-server"}'
```

### POST /agents/{agent-id}/forward/endpoints/manage

Manage forward endpoints on a remote agent:

```bash
curl -X POST http://localhost:8080/agents/abc123def456/forward/endpoints/manage \
  -H "Content-Type: application/json" \
  -d '{"action":"add","key":"web-server","target":"127.0.0.1:3000"}'
```

### POST /peers/manage

Add, remove, or list dynamic outbound peer connections. Useful when a new agent
can only accept connections and an existing agent (behind a firewall) must
initiate the dial.

```bash
# Tell the local agent to dial a new peer via QUIC
curl -X POST http://localhost:8080/peers/manage \
  -H "Content-Type: application/json" \
  -d '{"action":"add","address":"newagent.example:4433"}'

# Add with transport + expected peer ID
curl -X POST http://localhost:8080/peers/manage \
  -H "Content-Type: application/json" \
  -d '{"action":"add","address":"newagent.example:443","transport":"h2","id":"abc123def456"}'

# List peers
curl -X POST http://localhost:8080/peers/manage \
  -H "Content-Type: application/json" \
  -d '{"action":"list"}'

# Remove a dynamic peer (tears down live connection, cancels reconnect)
curl -X POST http://localhost:8080/peers/manage \
  -H "Content-Type: application/json" \
  -d '{"action":"remove","address":"newagent.example:4433"}'
```

Dynamic peers inherit TLS settings from the agent's global config and
auto-reconnect on disconnect. When `agent.data_dir` is set they also survive
agent restarts via the dynamic config journal; without a `data_dir` they stay
ephemeral. Config-file peers
appear in `list` output but cannot be removed via this endpoint. The `list`
action also returns inbound peers that have dialed into this agent; these are
marked with `"direction": "inbound"` and cannot be removed via this endpoint
either.

### POST /agents/{agent-id}/peers/manage

Manage peer connections on a remote agent:

```bash
curl -X POST http://localhost:8080/agents/abc123def456/peers/manage \
  -H "Content-Type: application/json" \
  -d '{"action":"add","address":"F.example:4433"}'
```

### POST /listeners/manage

Add, remove, or list dynamic transport listeners (the QUIC/H2/WS sockets that
accept inbound peer connections). Useful for opening a second ingress on a
different port or transport without editing `config.yaml` and restarting.

```bash
# Open a new QUIC listener
curl -X POST http://localhost:8080/listeners/manage \
  -H "Content-Type: application/json" \
  -d '{"action":"add","transport":"quic","address":":4434"}'

# Open a plaintext WebSocket listener (behind a reverse proxy) and advertise it
curl -X POST http://localhost:8080/listeners/manage \
  -H "Content-Type: application/json" \
  -d '{"action":"add","transport":"ws","address":"127.0.0.1:3002","path":"/ws/","plaintext":true,"advertise_address":"vps.example.com:4433"}'

# List listeners
curl -X POST http://localhost:8080/listeners/manage \
  -H "Content-Type: application/json" \
  -d '{"action":"list"}'

# Remove a dynamic listener
curl -X POST http://localhost:8080/listeners/manage \
  -H "Content-Type: application/json" \
  -d '{"action":"remove","address":":4434"}'
```

Dynamic listeners reuse the agent's global TLS certificate and mTLS setting
(`plaintext:true` starts a ws listener without TLS for reverse-proxy use). When
`agent.data_dir` is set they survive agent restarts via the dynamic config
journal and are restored across sleep/wake; without a `data_dir` they stay
ephemeral. Config-file listeners appear in `list` output but cannot be removed
via this endpoint. The CLI equivalent is `muti-metroo listener add/remove/list`.

### POST /agents/{agent-id}/listeners/manage

Manage transport listeners on a remote agent:

```bash
curl -X POST http://localhost:8080/agents/abc123def456/listeners/manage \
  -H "Content-Type: application/json" \
  -d '{"action":"add","transport":"quic","address":":4433"}'
```

### POST /display-name/manage

Set or get the agent's display name dynamically:

```bash
# Set display name
curl -X POST http://localhost:8080/display-name/manage \
  -H "Content-Type: application/json" \
  -d '{"action":"set","name":"gateway-us-east"}'

# Get current display name
curl -X POST http://localhost:8080/display-name/manage \
  -H "Content-Type: application/json" \
  -d '{"action":"get"}'
```

Response:

```json
{
  "status": "ok",
  "message": "display name set to \"gateway-us-east\"",
  "name": "gateway-us-east"
}
```

Setting an empty name reverts to the config value. When `agent.data_dir` is set, the dynamic name is persisted to the dynamic config journal and restored on restart; otherwise it is ephemeral.

### POST /agents/{agent-id}/display-name/manage

Set or get display name on a remote agent:

```bash
curl -X POST http://localhost:8080/agents/abc123def456/display-name/manage \
  -H "Content-Type: application/json" \
  -d '{"action":"set","name":"exit-eu-west"}'
```

## Sleep Mode Endpoints

Control mesh hibernation via HTTP.

### POST /sleep

Put the mesh into sleep mode:

```bash
curl -X POST http://localhost:8080/sleep
```

Response:

```json
{
  "status": "triggered",
  "message": "sleep command triggered"
}
```

### POST /wake

Wake the mesh from sleep mode:

```bash
curl -X POST http://localhost:8080/wake
```

Response:

```json
{
  "status": "triggered",
  "message": "wake command triggered"
}
```

### GET /sleep/status

Get current sleep status:

```bash
curl http://localhost:8080/sleep/status | jq
```

Response:

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

States: `AWAKE`, `SLEEPING`, `POLLING`

## CLI Commands

Use CLI for convenient access:

```bash
# Agent status
muti-metroo status -a localhost:8080

# Live peer connections
muti-metroo peer status -a localhost:8080

# Live route table
muti-metroo route status -a localhost:8080

# Configured + dynamic view (does not require active sessions)
muti-metroo peer list -a localhost:8080
muti-metroo route list -a localhost:8080
```

The legacy top-level aliases `muti-metroo peers` / `muti-metroo routes` still work but
print a deprecation warning -- prefer the `peer`/`route` subcommand groups.

## Security Recommendations

### Production Configuration

```yaml
http:
  enabled: true
  address: "127.0.0.1:8080" # Localhost only
  minimal: false
  pprof: false # Never in production
  dashboard: true
  remote_api: true
```

### Minimal Configuration

For production deployments with reduced exposure:

```yaml
http:
  enabled: true
  address: "127.0.0.1:8080"
  minimal: true # Only health endpoints
```

### Exposed Agent

If HTTP API must be exposed:

1. Use a firewall to limit access
2. Consider reverse proxy with authentication
3. Disable unnecessary endpoints:

```yaml
http:
  enabled: true
  address: "0.0.0.0:8080"
  pprof: false
  dashboard: false
  remote_api: false
```

## Examples

### Quick Health Check Script

```bash
#!/bin/bash
for agent in "localhost:8080" "remote1:8080" "remote2:8082"; do
  echo "=== $agent ==="
  curl -s "http://$agent/healthz" | jq '{running, peers: .peer_count, routes: .route_count}'
done
```

### Monitoring Integration

Prometheus-style health check:

```bash
# Returns 200 if healthy, 503 if not
curl -f http://localhost:8080/ready
```

### Verify Topology

```bash
# Count known nodes
curl -s http://localhost:8080/api/nodes | jq '.nodes | length'

# List connected peers
curl -s http://localhost:8080/api/dashboard | jq '.peers[].display_name'

# Check route paths
curl -s http://localhost:8080/api/dashboard | jq '.routes[] | {network, hops: .hop_count}'
```
