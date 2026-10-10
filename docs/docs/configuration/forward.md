---
title: Forward
sidebar_position: 8
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-inspecting.png" alt="Mole configuring forward" style={{maxWidth: '180px'}} />
</div>

# Port Forward Configuration

Expose local services through the mesh network. Services register with a routing key via **endpoints**, and any agent can create **listeners** to access them locally - a lightweight service discovery pattern.

See [Port Forwarding Usage](/features/port-forwarding) for the service discovery pattern and use cases.

**Quick setup:**

```yaml
# On agent with the service (endpoint side):
forward:
  endpoints:
    - key: "my-web"
      target: "localhost:80"

# On agents where clients connect (listener side):
forward:
  listeners:
    - key: "my-web"
      address: ":8080"
```

## Configuration

```yaml
forward:
  # Endpoints - where your services run
  endpoints:
    - key: "web-server"           # Routing key (advertised to mesh)
      target: "localhost:3000"     # Local service address
    - key: "internal-api"
      target: "192.168.1.10:8080"

  # Listeners - where remote clients connect
  listeners:
    - key: "web-server"           # Must match an endpoint key
      address: ":8080"            # Bind address for incoming connections
      max_connections: 100        # Optional connection limit
```

## Endpoints

Endpoints define services that can be reached through the mesh. Configure these on agents that have access to the target service.

```yaml
forward:
  endpoints:
    - key: "tools"
      target: "localhost:80"
```

### Options

| Option | Type | Required | Description |
|--------|------|----------|-------------|
| `key` | string | Yes | Unique routing key advertised to the mesh. Other agents use this key to reach this endpoint. |
| `target` | string | Yes | Fixed destination in `host:port` format. Connections are forwarded here. |

### Routing Key Guidelines

- Use descriptive names that reflect the service purpose
- Keys are case-sensitive
- Must be unique within the mesh (duplicate keys cause routing conflicts)
- Examples: `"config-server"`, `"dev-api"`, `"backup-sync"`

## Listeners

Listeners accept incoming connections from remote clients and route them through the mesh to matching endpoints.

```yaml
forward:
  listeners:
    - key: "tools"
      address: "127.0.0.1:8080"
      max_connections: 50
```

### Options

| Option | Type | Required | Default | Description |
|--------|------|----------|---------|-------------|
| `key` | string | Yes | - | Routing key to look up. Must match an endpoint's key somewhere in the mesh. |
| `address` | string | Yes | - | Local bind address in `host:port` or `:port` format. |
| `max_connections` | int | No | 0 (unlimited) | Maximum concurrent connections through this listener. |

### Bind Address Guidelines

- `127.0.0.1:8080` - Localhost only (more secure)
- `0.0.0.0:8080` or `:8080` - All interfaces (required for network access)
- `192.168.1.10:8080` - Specific interface only

## Dynamic Management

Both endpoints and listeners can be added, removed, and listed at runtime without editing `config.yaml`. Runtime changes persist across restarts when `agent.data_dir` is configured; otherwise they remain ephemeral. See [Dynamic Config Persistence](/configuration/dynamic-config-persistence) for the journal format and guarantees.

**Endpoints** (CLI: `muti-metroo forward endpoint add/remove/list`, HTTP: `POST /forward/endpoints/manage`):

```bash
# Register a routing key -> target on the exit side
muti-metroo forward endpoint add web-server 127.0.0.1:3000

# ... or on a remote agent
muti-metroo forward endpoint add web-server 127.0.0.1:3000 -t abc123

muti-metroo forward endpoint list
muti-metroo forward endpoint remove web-server
```

**Listeners** (CLI: `muti-metroo forward add/remove/list`, HTTP: `POST /forward/manage`):

```bash
muti-metroo forward add web-server :9090
muti-metroo forward list
muti-metroo forward remove web-server
```

Config-declared endpoints and listeners are protected from runtime mutation; `list` flags each entry as `static` or `dynamic` so you can tell them apart. When management-key encryption is enabled, agents without the decrypt capability reject mutation requests with HTTP 403.

## Route Advertisement

Endpoint routes propagate through the mesh using flood routing:

- **Automatic**: Routes advertised every `routing.advertise_interval` (default 2 minutes)
- **Manual trigger**: `POST /routes/advertise` on the endpoint agent's HTTP API

```bash
# Trigger immediate route advertisement
curl -X POST http://localhost:8080/routes/advertise
```

## Examples

### Configuration Distribution Server

Serve configuration files from headquarters to remote sites:

```yaml
# Central server - runs HTTP server on port 8000
forward:
  endpoints:
    - key: "config-server"
      target: "localhost:8000"
```

```yaml
# Remote site agents - accept connections on port 8080
forward:
  listeners:
    - key: "config-server"
      address: "127.0.0.1:8080"
      max_connections: 50
```

### Internal API Access

Make internal services accessible from remote offices:

```yaml
# Headquarters - API server on port 3000
forward:
  endpoints:
    - key: "internal-api"
      target: "localhost:3000"
```

```yaml
# Branch office agents - expose API locally
forward:
  listeners:
    - key: "internal-api"
      address: "127.0.0.1:3000"
```

### Multiple Services

Expose several services with different keys:

```yaml
forward:
  endpoints:
    - key: "http-tools"
      target: "localhost:80"
    - key: "smb-share"
      target: "localhost:445"
    - key: "ssh-jump"
      target: "localhost:22"
```

### High Availability (Multiple Listeners)

Deploy the same listener on multiple agents for redundancy:

```yaml
# Agent 1
forward:
  listeners:
    - key: "tools"
      address: ":8080"

# Agent 2 (same key)
forward:
  listeners:
    - key: "tools"
      address: ":8080"
```

Clients can connect to whichever agent is nearest.

## Troubleshooting

| Error | Cause | Solution |
|-------|-------|----------|
| "forward key not found" | No endpoint with matching key | Verify endpoint is configured and agent is connected to mesh |
| Connection timeout | Route not propagated yet | Wait for advertise_interval or trigger `POST /routes/advertise` |
| Connection refused | Target service not running | Verify service is listening on the endpoint's target port |
| Listener bind failed | Port already in use | Choose a different port or stop conflicting service |

### Verify Routes

Check if a forward route exists:

```bash
curl http://localhost:8080/healthz | jq '.forward_routes'
```

## Security Considerations

- **Use unique keys**: Predictable keys could be guessed by unauthorized users
- **Bind to localhost**: Use `127.0.0.1` when external network access isn't needed
- **Set connection limits**: Prevent resource exhaustion
- **Firewall integration**: Allow listener ports through host firewall only where needed

## Related

- [Features - Port Forwarding](/features/port-forwarding) - Feature overview
- [Concepts - Routing](/concepts/routing) - How routes propagate
