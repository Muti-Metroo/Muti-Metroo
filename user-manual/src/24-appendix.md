# Appendix

## CLI Quick Reference

### Getting Started

| Command                                          | Description                                    |
| ------------------------------------------------ | ---------------------------------------------- |
| `muti-metroo setup`                                    | Interactive setup wizard                       |
| `muti-metroo init -d ./data`                           | Initialize agent identity                      |
| `muti-metroo run -c config.yaml`                       | Run agent with config                          |
| `muti-metroo config validate <file>`                   | Validate a config file (exit 0 ok / 2 invalid) |
| `muti-metroo config show <file> [--diff-from-default]` | Print effective config, secrets redacted       |

### Agent Status

| Command                            | Description                              |
| ---------------------------------- | ---------------------------------------- |
| `muti-metroo status`                     | Show agent status                        |
| `muti-metroo peer status`                | Connected peers (live transport state)   |
| `muti-metroo peer list`                  | Configured + dynamic + inbound peers     |
| `muti-metroo route status`               | Live routing table                       |
| `muti-metroo route list`                 | Configured + dynamic routes              |
| `muti-metroo route trace <ip\|hostname>` | Explain which route the agent would pick |
| `muti-metroo probe <address>`            | Test connectivity to listener            |
| `muti-metroo probe listen`               | Start test listener for probing          |
| `muti-metroo mesh-test`                  | Test connectivity to all mesh agents     |

The legacy aliases `muti-metroo peers` and `muti-metroo routes` still work but print a deprecation warning; prefer the `peer`/`route` subcommand groups above.

### Remote Operations

| Command                                | Description                    |
| -------------------------------------- | ------------------------------ |
| `muti-metroo shell <id> <cmd>`               | Run management command         |
| `muti-metroo shell --tty <id> bash`          | Interactive management session |
| `muti-metroo upload <id> <local> <remote>`   | Upload file                    |
| `muti-metroo download <id> <remote> <local>` | Download file                  |
| `muti-metroo ping <id> <dest>`               | ICMP ping through remote agent |

### Administration

| Command                                | Description                                                                                                                                  |
| -------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------- |
| `muti-metroo service install -c config.yaml` | Install as service                                                                                                                           |
| `muti-metroo service uninstall`              | Uninstall service                                                                                                                            |
| `muti-metroo service status`                 | Check service status                                                                                                                         |
| `muti-metroo stop [-t <id>]`                 | Gracefully stop the local (or remote) agent; signed. A service manager may restart it; run `service uninstall` first for permanent removal |
| `muti-metroo listener add\|remove\|list`     | Manage transport listeners at runtime                                                                                                        |
| `muti-metroo host add\|remove\|list`         | Manage mesh-host registry                                                                                                                    |
| `muti-metroo display-name set\|get`          | Manage the agent's display name                                                                                                              |
| `muti-metroo cert ca --cn "CA"`              | Generate CA certificate                                                                                                                      |
| `muti-metroo cert agent --cn "name"`         | Generate agent certificate                                                                                                                   |
| `muti-metroo cert client --cn "name"`        | Generate client certificate                                                                                                                  |
| `muti-metroo cert info <cert>`               | Display certificate info                                                                                                                     |
| `muti-metroo hash`                           | Generate bcrypt password hash                                                                                                                |
| `muti-metroo management-key generate`        | Generate management keypair                                                                                                                  |
| `muti-metroo signing-key generate`           | Generate Ed25519 signing keypair                                                                                                             |
| `muti-metroo signing-key public`             | Derive signing public key from private                                                                                                       |

### Mutiauk (Operator Workstation)

| Command                                  | Description                                          |
| ---------------------------------------- | ---------------------------------------------------- |
| `sudo mutiauk setup`                     | Interactive setup wizard                             |
| `sudo mutiauk config generate --from <f>` | Write a config non-interactively                    |
| `sudo mutiauk daemon start`              | Start the daemon (foreground)                        |
| `sudo mutiauk status`                    | Component status and connectivity probes             |
| `sudo mutiauk route list`                | Active routes (config, auto, manual)                 |
| `sudo mutiauk route trace <ip\|hostname>` | Show the matching route and mesh path              |
| `sudo mutiauk dns status`                | Mesh DNS backend, active suffixes, probes            |
| `sudo mutiauk host list`                 | Mesh hostnames received from the agent               |
| `sudo mutiauk service install`           | Install as a service                                 |
| `sudo mutiauk cleanup`                   | Remove TUN, PID file, socket; revert host DNS        |

On Windows, run the same commands without `sudo` from an elevated prompt.

## Configuration Cheatsheet

### Minimal Standalone

```yaml
agent:
  id: "auto"
  data_dir: "./data"

tls:
  cert: "./certs/agent.crt"
  key: "./certs/agent.key"

listeners:
  - transport: quic
    address: "0.0.0.0:4433"

socks5:
  enabled: true
  address: "127.0.0.1:1080"

exit:
  enabled: true
  routes:
    - "0.0.0.0/0"

http:
  enabled: true
  address: ":8080"
```

### Ingress Only

```yaml
agent:
  display_name: "Ingress"

listeners:
  - transport: quic
    address: "0.0.0.0:4433"

peers:
  - id: "exit-agent-id..."
    transport: quic
    address: "exit.example.com:4433"

socks5:
  enabled: true
  address: "127.0.0.1:1080"
  auth:
    enabled: true
    users:
      - username: "operator"
        password_hash: "$2a$12$..."
```

### Exit Only

```yaml
agent:
  display_name: "Exit"

listeners:
  - transport: quic
    address: "0.0.0.0:4433"

exit:
  enabled: true
  routes:
    - "0.0.0.0/0"
  # dns.servers defaults to [] (use system resolver). Override only when
  # the host's stub resolver is wrong for your network. Do NOT paste
  # public DNS (8.8.8.8, 1.1.1.1) into compliance-bound deployments.
```

### Minimal Configuration

```yaml
protocol:
  alpn: "h2"
  http_header: ""
  ws_subprotocol: ""

http:
  enabled: true
  address: "127.0.0.1:8080"
  minimal: true

management:
  public_key: "${MGMT_PUBKEY}"
```

## Default Ports

| Port | Protocol | Purpose                                                           |
| ---- | -------- | ----------------------------------------------------------------- |
| 4433 | UDP      | QUIC transport (convention; no code default)                      |
| 8443 | TCP      | HTTP/2 transport (convention; no code default)                    |
| 443  | TCP      | WebSocket transport (convention; pick whatever your edge expects) |
| 1080 | TCP      | SOCKS5 proxy (code default for `socks5.address`)                  |
| 8443 | TCP      | SOCKS5 WebSocket listener (convention in examples; `socks5.websocket.address` has no default) |
| 8080 | TCP      | HTTP API (code default for `http.address`)                        |
| 3000 | TCP      | Muti Metroo Manager web UI (code default for `-addr`)             |
| 48088 | TCP     | Mutiauk local admin interface (default, loopback only)            |

Listener and peer ports are operator-chosen; nothing in the binary forces 4433/8443/443. The values above are widely used in examples and the wizard suggests them, but you can pick anything that suits your network.

## HTTP API Endpoints

Always-on (regardless of endpoint flags):

| Endpoint   | Method | Description                                      |
| ---------- | ------ | ------------------------------------------------ |
| `/health`  | GET    | Basic health check                               |
| `/healthz` | GET    | Detailed health JSON (counts + capability flags) |
| `/ready`   | GET    | Readiness probe                                  |

Dashboard API (gated by `http.dashboard: true`):

| Endpoint              | Method   | Description                                         |
| --------------------- | -------- | --------------------------------------------------- |
| `/api/topology`       | GET      | Topology data                                       |
| `/api/dashboard`      | GET      | Local agent + peers + routes + hosts                |
| `/api/nodes`          | GET      | Per-agent NodeInfo cache                            |
| `/api/mesh-test`      | GET/POST | Mesh connectivity test (GET = cached, POST = fresh) |
| `/api/probe`          | POST     | Probe a listener (local)                            |
| `/api/setup-defaults` | GET      | Suggested defaults for the in-browser setup UI      |

Remote agent / management (gated by `http.remote_api: true`):

| Endpoint                                | Method | Description                                                      |
| --------------------------------------- | ------ | ---------------------------------------------------------------- |
| `/agents`                               | GET    | List all known agents                                            |
| `/agents/{id}`                          | GET    | Remote agent status                                              |
| `/agents/{id}/routes`                   | GET    | Remote agent route table                                         |
| `/agents/{id}/peers`                    | GET    | Remote agent peer list                                           |
| `/agents/{id}/shell`                    | GET    | WebSocket management command session (subprotocol `muti-metroo-shell`) |
| `/agents/{id}/icmp`                     | GET    | WebSocket ICMP ping session (subprotocol `muti-metroo-icmp`)           |
| `/agents/{id}/file/upload`              | POST   | Upload file to remote agent                                      |
| `/agents/{id}/file/download`            | POST   | Download file from remote agent                                  |
| `/agents/{id}/file/browse`              | POST   | Browse filesystem on remote agent                                |
| `/agents/{id}/probe`                    | POST   | Probe a listener from remote agent                               |
| `/agents/{id}/route/trace`              | POST   | Route trace from remote agent                                    |
| `/agents/{id}/routes/manage`            | POST   | Manage routes on remote agent                                    |
| `/agents/{id}/forward/manage`           | POST   | Manage forward listeners on remote agent                         |
| `/agents/{id}/forward/endpoints/manage` | POST   | Manage forward endpoints on remote agent                         |
| `/agents/{id}/peers/manage`             | POST   | Manage peers on remote agent                                     |
| `/agents/{id}/listeners/manage`         | POST   | Manage transport listeners on remote agent                       |
| `/agents/{id}/display-name/manage`      | POST   | Manage display name on remote agent                              |
| `/agents/{id}/shutdown`                 | POST   | Stop a remote agent (signed)                                     |
| `/route/trace`                          | POST   | Local route-selection trace (read-only)                          |
| `/routes/advertise`                     | POST   | Trigger immediate route advertisement                            |
| `/routes/manage`                        | POST   | Add/remove/list dynamic CIDR routes                              |
| `/forward/manage`                       | POST   | Add/remove/list dynamic forward listeners                        |
| `/forward/endpoints/manage`             | POST   | Add/remove/list dynamic forward endpoints                        |
| `/peers/manage`                         | POST   | Add/remove/list dynamic peers                                    |
| `/listeners/manage`                     | POST   | Add/remove/list dynamic transport listeners                      |
| `/display-name/manage`                  | POST   | Set/get the agent's display name                                 |
| `/hosts/manage`                         | POST   | Manage mesh-host registry (local-only)                           |
| `/hosts/suffixes/manage`                | POST   | Manage mesh-host TLD suffixes (local-only)                       |
| `/sleep`                                | POST   | Trigger mesh-wide sleep                                          |
| `/wake`                                 | POST   | Trigger mesh-wide wake                                           |
| `/sleep/status`                         | GET    | Current sleep state                                              |
| `/shutdown`                             | POST   | Gracefully stop the local agent (signed)                         |

Debugging (gated by `http.pprof: true`):

| Endpoint         | Method | Description                 |
| ---------------- | ------ | --------------------------- |
| `/debug/pprof/*` | GET    | Standard Go pprof endpoints |

## Environment Variables

Syntax:

- `${VAR}` - Required variable
- `${VAR:-default}` - With default value

Common usage:

```yaml
agent:
  data_dir: "${DATA_DIR:-./data}"
  log_level: "${LOG_LEVEL:-info}"

socks5:
  auth:
    users:
      - username: "${SOCKS_USER}"
        password_hash: "${SOCKS_PASS_HASH}"

management:
  public_key: "${MGMT_PUBKEY}"
  private_key: "${MGMT_PRIVKEY}"
```

## Transport Comparison

| Transport | Protocol | Performance | Firewall  |
| --------- | -------- | ----------- | --------- |
| QUIC      | UDP      | Best        | Medium    |
| HTTP/2    | TCP      | Good        | Good      |
| WebSocket | TCP      | Fair        | Excellent |

## Role Summary

| Role    | Config Section | Purpose                   |
| ------- | -------------- | ------------------------- |
| Ingress | `socks5:`      | Accept Mutiauk and SOCKS client connections |
| Transit | (implicit)     | Relay between peers       |
| Exit    | `exit:`        | Open external connections |

## Certificate Commands

```bash
# Create CA
muti-metroo cert ca --cn "Mesh CA" -o ./certs

# Create agent cert
muti-metroo cert agent --cn "agent-1" \
  --ca ./certs/ca.crt \
  --ca-key ./certs/ca.key \
  --dns agent1.example.com \
  --ip 192.168.1.10 \
  -o ./certs

# View cert info
muti-metroo cert info ./certs/agent-1.crt
```

## Hash Generation

```bash
# Interactive
muti-metroo hash

# With custom cost
# Default bcrypt cost is 12 (OWASP 2024). Pass --cost N (4-31) to override.
muti-metroo hash --cost 14

# From argument (not recommended; ends up in shell history)
muti-metroo hash "password"
```

## Management Key Generation

```bash
# Generate keypair (echoes both keys to stdout)
muti-metroo management-key generate

# Generate keypair and write to files with safe permissions
muti-metroo management-key generate --out ./secrets/

# Derive public key from private (interactive prompt; preferred)
muti-metroo management-key public

# Or read the private key from a file via stdin
muti-metroo management-key public --stdin < ./secrets/management-private.key
```

The `--private <hex>` flag still exists but is hidden and triggers a stderr
warning: passing a private key on the command line records it in shell
history, so use the prompt or `--stdin` form in scripts.

## Signing Key Generation

```bash
# Generate Ed25519 signing keypair
muti-metroo signing-key generate

# Derive public key from private
muti-metroo signing-key public
```

## Useful Commands

### Health Check

```bash
curl http://localhost:8080/health
curl http://localhost:8080/healthz | jq
```

### Trigger Route Advertisement

```bash
curl -X POST http://localhost:8080/routes/advertise
```

### Test the SOCKS5 Listener Directly

```bash
curl -x socks5://localhost:1080 https://example.com
curl -x socks5://user:pass@localhost:1080 https://example.com
```

### SSH Through Proxy

```bash
ssh -o ProxyCommand='nc -x localhost:1080 %h %p' user@host
```

### Mesh Connectivity Test

Test connectivity to all known agents in the mesh:

```bash
# Test all agents
muti-metroo mesh-test

# JSON output
muti-metroo mesh-test --json

# Via specific agent
muti-metroo mesh-test -a 192.168.1.10:8080

# Via HTTP API
curl -X POST http://localhost:8080/api/mesh-test | jq
```

### Probe Connectivity

Test if a listener is reachable (no running agent needed):

```bash
# Test different transports
muti-metroo probe -T quic target.example.com:4433
muti-metroo probe -T h2 target.example.com:8443
muti-metroo probe -T ws target.example.com:443

# Plaintext WebSocket (behind reverse proxy)
muti-metroo probe -T ws --plaintext localhost:8080
```

### Probe Listener

Start a test listener to validate transport and TLS configuration:

```bash
# Start with ephemeral certificates (no cert files needed)
muti-metroo probe listen -T quic -a 0.0.0.0:4433

# With static certificates
muti-metroo probe listen -T quic -a 0.0.0.0:4433 \
  --cert ./certs/server.crt --key ./certs/server.key

# Plaintext WebSocket (behind reverse proxy)
muti-metroo probe listen -T ws -a 127.0.0.1:8080 --plaintext

# With mTLS (require client certificates)
muti-metroo probe listen -T quic -a 0.0.0.0:4433 \
  --cert ./certs/server.crt --key ./certs/server.key \
  --ca ./certs/ca.crt
```
