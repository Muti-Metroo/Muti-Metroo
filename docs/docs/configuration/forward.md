---
title: Forward
description: Configuration reference for the forward section, covering port forward endpoints that expose a service under a routing key and listeners that accept connections for a key.
sidebar_position: 10
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-inspecting.png" alt="Mole configuring forward" style={{maxWidth: '180px'}} />
</div>

# Forward

The `forward` section publishes TCP services through the mesh under a name, the routing key. An agent with an **endpoint** advertises the key and connects to a fixed target. Any agent with a **listener** for the same key accepts local TCP connections and carries them to that endpoint.

## Configuration

```yaml
forward:
  endpoints:                   # Agent next to the service
    - key: "web-server"
      target: "127.0.0.1:3000"

  listeners:                   # Agents where clients connect
    - key: "web-server"
      address: "127.0.0.1:8080"
      max_connections: 100
```

An agent can have endpoints, listeners, or both.

## Keys

| Key                           | Type   | Default  | Description                                                                                                      |
| ----------------------------- | ------ | -------- | ---------------------------------------------------------------------------------------------------------------- |
| `endpoints`                   | list   | `[]`     | Services this agent exposes.                                                                                     |
| `endpoints[].key`             | string | required | Routing key advertised to the mesh. Case-sensitive. Must be unique among this agent's endpoints.               |
| `endpoints[].target`          | string | required | `host:port` the agent connects to for each forwarded connection.                                                |
| `listeners`                   | list   | `[]`     | Local ports that forward to a key.                                                                               |
| `listeners[].key`             | string | required | Routing key to look up in the mesh.                                                                              |
| `listeners[].address`         | string | required | Local bind address, `host:port` or `:port`.                                                                      |
| `listeners[].max_connections` | int    | `0`      | Maximum concurrent connections on this listener. `0` means no limit. Must not be negative.                      |

## Endpoints

```yaml
forward:
  endpoints:
    - key: "internal-api"
      target: "192.168.1.10:8080"
```

The endpoint's key is advertised with the agent's routes, on the [`routing.advertise_interval`](/configuration/routing) schedule. It does not require `exit.enabled`. Connections to the target close after [`connections.idle_threshold`](/configuration/connections) without traffic, and count toward [`limits.max_streams_total`](/configuration/limits).

If several agents advertise the same key, listeners use the nearest one.

## Listeners

```yaml
forward:
  listeners:
    - key: "internal-api"
      address: "127.0.0.1:3000"
```

Each accepted connection is routed to the closest agent that advertises the key. Bind to `127.0.0.1` when only local clients should use the listener, and to `0.0.0.0` or an interface address to accept clients from the network.

## Runtime management

Endpoints and listeners can be added and removed on a running agent:

| What      | CLI                                            | HTTP endpoint                    |
| --------- | ---------------------------------------------- | -------------------------------- |
| Endpoints | `muti-metroo forward endpoint add/remove/list` | `POST /forward/endpoints/manage` |
| Listeners | `muti-metroo forward add/remove/list`          | `POST /forward/manage`           |

Runtime entries persist across restarts when `agent.data_dir` is set. Entries from this file cannot be changed at runtime. Like other runtime changes, adding and removing entries needs the management signing keys described on [Management](/configuration/management#command-signing-keys). See [forward CLI](/cli/forward).

## Examples

### Expose a web application to branch offices

```yaml
# Headquarters agent, next to the application
forward:
  endpoints:
    - key: "intranet"
      target: "127.0.0.1:8000"
```

```yaml
# Branch office agent
forward:
  listeners:
    - key: "intranet"
      address: "0.0.0.0:8000"
      max_connections: 200
```

### Several services from one agent

```yaml
forward:
  endpoints:
    - key: "http-tools"
      target: "127.0.0.1:80"
    - key: "ssh-jump"
      target: "127.0.0.1:22"
```

## See Also

- [Port Forwarding usage](/features/port-forwarding) for the service discovery pattern and troubleshooting
- [forward CLI](/cli/forward) to manage endpoints and listeners at runtime
- [Forward Management API](/api/forward-management) for the HTTP endpoints
- [Dynamic Config Persistence](/configuration/dynamic-config-persistence) for how runtime entries survive restarts
