---
title: Dashboard Endpoints
description: JSON endpoints for dashboards and integrations - agent overview, topology, node details, mesh test, interfaces, and setup defaults.
sidebar_position: 16
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-presenting.png" alt="Mole presenting dashboard" style={{maxWidth: '180px'}} />
</div>

# Dashboard Endpoints

The `/api/` endpoints return JSON for dashboards and other integrations. The agent does not serve a web interface itself; [Muti Metroo Manager](/muti-metroo-manager) is a web dashboard that reads these endpoints, and Mutiauk reads `/api/dashboard` for routes and mesh hostnames.

The endpoints belong to the dashboard group (`http.dashboard`, enabled by default). When the group is disabled, every `/api/` path returns `404`.

| Endpoint | Description |
| -------- | ----------- |
| `GET /api/dashboard` | Local agent, statistics, peers, routes, forwards, and mesh hostnames |
| `GET /api/topology` | Every known agent and the links between them |
| `GET /api/nodes` | System details of every known agent |
| `GET /api/mesh-test`, `POST /api/mesh-test` | Reachability test of every known agent |
| `GET /api/interfaces` | IP addresses of the local agent |
| `GET /api/setup-defaults` | Non-secret defaults for generating a new agent's configuration |
| `POST /api/probe` | Probe a listener; see [Probe Endpoints](/api/probe) |

When the agent has `management.public_key` without `management.private_key`, `/api/dashboard`, `/api/topology`, `/api/nodes`, and `/api/mesh-test` report only the local agent.

## `GET /api/dashboard`

```bash
curl http://localhost:8080/api/dashboard
```

**Response (200):**

```json
{
  "agent": {
    "id": "93cdf211bb05aa48900a61ab6699fbdd",
    "short_id": "93cdf211",
    "display_name": "gateway",
    "is_local": true,
    "is_connected": true
  },
  "stats": {
    "peer_count": 1,
    "stream_count": 0,
    "route_count": 2,
    "socks5_running": true,
    "exit_handler_running": false,
    "can_sign_management": true,
    "accepts_signed_management": true
  },
  "peers": [
    {
      "id": "653422d2fc10b554514c5cbe6e6cd6cd",
      "short_id": "653422d2",
      "display_name": "transit",
      "state": "CONNECTED",
      "rtt_ms": 0,
      "is_dialer": false
    }
  ],
  "routes": [
    {
      "network": "10.20.0.0/16",
      "route_type": "cidr",
      "origin": "exit-1",
      "origin_id": "331974cd",
      "hop_count": 2,
      "path_display": ["gateway", "transit", "exit-1"],
      "path_ids": ["93cdf211", "653422d2", "331974cd"],
      "tcp": true,
      "udp": true,
      "comment": "lab network"
    },
    {
      "network": "*.example.com",
      "route_type": "domain",
      "origin": "exit-1",
      "origin_id": "331974cd",
      "hop_count": 2,
      "path_display": ["gateway", "transit", "exit-1"],
      "path_ids": ["93cdf211", "653422d2", "331974cd"],
      "tcp": true,
      "udp": true
    }
  ],
  "domain_routes": [
    {
      "pattern": "*.example.com",
      "is_wildcard": true,
      "origin": "exit-1",
      "origin_id": "331974cd",
      "hop_count": 2,
      "path_display": ["gateway", "transit", "exit-1"],
      "path_ids": ["93cdf211", "653422d2", "331974cd"],
      "tcp": true,
      "udp": true
    }
  ],
  "forward_routes": [
    {
      "key": "web",
      "ingress_agent": "gateway",
      "ingress_agent_id": "93cdf211",
      "listener_address": "127.0.0.1:9090",
      "exit_agent": "exit-1",
      "exit_agent_id": "331974cd",
      "target": "127.0.0.1:3000",
      "hop_count": 2,
      "path_display": ["gateway", "exit-1"],
      "path_ids": ["93cdf211", "331974cd"]
    }
  ],
  "mesh_hosts": [
    {
      "name": "db.mesh",
      "ipv4": "10.20.0.20",
      "comment": "database"
    }
  ],
  "mesh_hosts_suffixes": ["mesh"]
}
```

| Field | Description |
| ----- | ----------- |
| `agent` | Local agent identity: `id`, `short_id`, `display_name`, `is_local`, `is_connected` |
| `stats` | Same statistics as [`GET /healthz`](/api/health#get-healthz) |
| `peers[]` | Direct peers: `id`, `short_id`, `display_name`, `state`, `rtt_ms`, `is_dialer` (this agent opened the connection), and `unresponsive` (present and `true` when the round-trip time exceeds 60 seconds) |
| `routes[]` | CIDR routes first, then domain routes, each sorted by network |
| `domain_routes[]` | Domain routes only, with `pattern` and `is_wildcard`; omitted when there are none |
| `forward_routes[]` | Port forward listener and endpoint pairs; omitted when there are none |
| `mesh_hosts[]` | Mesh hostname registry entries; omitted when empty. See [Hosts Management](/api/hosts-management). |
| `mesh_hosts_suffixes` | Active hostname suffixes; omitted when empty |

`routes[]` fields:

| Field | Description |
| ----- | ----------- |
| `network` | CIDR prefix or domain pattern |
| `route_type` | `cidr` or `domain` |
| `origin`, `origin_id` | Display name and short ID of the advertising agent |
| `hop_count` | Hops to the origin |
| `path_display`, `path_ids` | Display names and short IDs from this agent to the origin |
| `tcp` | Always `true` |
| `udp` | The origin has UDP relay enabled |
| `comment` | Route comment, when set |
| `source_ip`, `interface` | Outbound binding; present only for routes this agent originates |

`forward_routes[]` lists every combination of an agent with a forward listener and an agent with a forward endpoint for the same `key`:

| Field | Description |
| ----- | ----------- |
| `key` | Routing key |
| `ingress_agent`, `ingress_agent_id` | Agent with the listener |
| `listener_address` | Listen address on the ingress agent |
| `exit_agent`, `exit_agent_id` | Agent with the endpoint |
| `target` | Endpoint target address |
| `hop_count` | Hops between the ingress and exit agents |
| `path_display`, `path_ids` | Ingress and exit agent |

## `GET /api/topology`

Returns every agent the local agent knows about and the links between them.

```bash
curl http://localhost:8080/api/topology
```

**Response (200):**

```json
{
  "local_agent": {
    "id": "93cdf211bb05aa48900a61ab6699fbdd",
    "short_id": "93cdf211",
    "display_name": "gateway",
    "is_local": true,
    "is_connected": true,
    "hostname": "gw-01",
    "os": "linux",
    "arch": "amd64",
    "version": "v4.10.6",
    "pid": 20749,
    "uptime_hours": 26.5,
    "ip_addresses": ["192.168.1.10"],
    "roles": ["ingress", "forward_ingress"],
    "socks5_addr": "127.0.0.1:1080",
    "udp_enabled": true,
    "forward_listeners": ["web"]
  },
  "agents": [
    {
      "id": "93cdf211bb05aa48900a61ab6699fbdd",
      "short_id": "93cdf211",
      "display_name": "gateway",
      "is_local": true,
      "is_connected": true,
      "hostname": "gw-01",
      "os": "linux",
      "arch": "amd64",
      "version": "v4.10.6",
      "pid": 20749,
      "uptime_hours": 26.5,
      "ip_addresses": ["192.168.1.10"],
      "roles": ["ingress", "forward_ingress"],
      "socks5_addr": "127.0.0.1:1080",
      "udp_enabled": true,
      "forward_listeners": ["web"]
    },
    {
      "id": "653422d2fc10b554514c5cbe6e6cd6cd",
      "short_id": "653422d2",
      "display_name": "transit",
      "is_local": false,
      "is_connected": true,
      "hostname": "relay-01",
      "os": "linux",
      "arch": "amd64",
      "version": "v4.10.6",
      "pid": 20751,
      "uptime_hours": 26.5,
      "ip_addresses": ["198.51.100.20"],
      "roles": ["transit"],
      "udp_enabled": true,
      "icmp_enabled": true
    },
    {
      "id": "331974cd4a0be0d7f945ed9d367a0211",
      "short_id": "331974cd",
      "display_name": "exit-1",
      "is_local": false,
      "is_connected": false,
      "hostname": "exit-01",
      "os": "linux",
      "arch": "arm64",
      "version": "v4.10.6",
      "pid": 20753,
      "uptime_hours": 26.5,
      "ip_addresses": ["10.20.0.5"],
      "roles": ["exit", "forward_exit"],
      "exit_routes": ["10.20.0.0/16", "192.0.2.0/24"],
      "domain_routes": ["*.example.com"],
      "udp_enabled": true,
      "forward_endpoints": ["web"],
      "shells": ["bash", "sh", "dash"],
      "shell_enabled": true,
      "file_transfer_enabled": true,
      "icmp_enabled": true
    }
  ],
  "connections": [
    {
      "from_agent": "93cdf211",
      "to_agent": "653422d2",
      "is_direct": true,
      "transport": "quic",
      "dialer": "653422d2"
    },
    {
      "from_agent": "653422d2",
      "to_agent": "331974cd",
      "is_direct": false,
      "rtt_ms": 1,
      "transport": "h2",
      "dialer": "331974cd"
    }
  ]
}
```

`local_agent` and each entry of `agents[]` use the agent fields in the table below. `connections[]` fields:

| Field | Description |
| ----- | ----------- |
| `from_agent`, `to_agent` | Short IDs of the two agents |
| `is_direct` | `true` for links of the local agent; `false` for links reported by other agents |
| `rtt_ms` | Round-trip time in milliseconds; omitted when `0` |
| `unresponsive` | Present and `true` when the round-trip time exceeds 60 seconds |
| `transport` | `quic`, `h2`, or `ws` |
| `dialer` | Short ID of the agent that opened the connection; omitted when unknown |

### Agent fields

Used by `/api/topology` and `/api/nodes`. Fields without a value are omitted.

| Field | Type | Description |
| ----- | ---- | ----------- |
| `id`, `short_id` | string | Full and short agent ID |
| `display_name` | string | Display name, or the short ID when none is set |
| `is_local` | boolean | The agent serving the request |
| `is_connected` | boolean | A direct peer of the local agent (always `true` for the local agent) |
| `hostname`, `os`, `arch`, `version`, `pid` | | System details reported by the agent |
| `uptime_hours` | number | Agent uptime in hours |
| `ip_addresses` | string[] | Up to 10 non-loopback IPv4 addresses |
| `roles` | string[] | `ingress`, `exit`, `forward_ingress`, `forward_exit`, or `transit` when none apply. `/api/topology` only. |
| `socks5_addr` | string | SOCKS listen address; local agent in `/api/topology` only |
| `exit_routes`, `domain_routes` | string[] | Routes the agent advertises. `/api/topology` only. |
| `forward_listeners` | string[] | Forward listener keys |
| `forward_endpoints` | string[] | Forward endpoint keys. `/api/topology` only. |
| `udp_enabled` | boolean | UDP relay enabled |
| `shells` | string[] | Shells available for management commands |
| `shell_enabled`, `file_transfer_enabled`, `icmp_enabled` | boolean | Management commands, file transfer, and ICMP are enabled. Reported for remote agents only, because these features are used through `/agents/{id}/...` and cannot target the local agent. |
| `advertised_listeners` | object[] | Public dial targets the agent advertises, each with `transport`, `address`, and `path`. See [Listeners Configuration](/configuration/listeners). |

## `GET /api/nodes`

Returns the system details of the local agent and of every agent that has sent node information, without roles or routes.

```bash
curl http://localhost:8080/api/nodes
```

**Response (200):**

```json
{
  "nodes": [
    {
      "id": "93cdf211bb05aa48900a61ab6699fbdd",
      "short_id": "93cdf211",
      "display_name": "gateway",
      "is_local": true,
      "is_connected": true,
      "hostname": "gw-01",
      "os": "linux",
      "arch": "amd64",
      "version": "v4.10.6",
      "pid": 20749,
      "uptime_hours": 26.5,
      "ip_addresses": ["192.168.1.10"],
      "udp_enabled": true,
      "forward_listeners": ["web"]
    },
    {
      "id": "331974cd4a0be0d7f945ed9d367a0211",
      "short_id": "331974cd",
      "display_name": "exit-1",
      "is_local": false,
      "is_connected": false,
      "hostname": "exit-01",
      "os": "linux",
      "arch": "arm64",
      "version": "v4.10.6",
      "pid": 20753,
      "uptime_hours": 26.5,
      "ip_addresses": ["10.20.0.5"],
      "udp_enabled": true,
      "shells": ["bash", "sh", "dash"],
      "shell_enabled": true,
      "file_transfer_enabled": true,
      "icmp_enabled": true
    }
  ]
}
```

The local agent is first; the order of the other entries is not fixed. Fields are described under [Agent fields](#agent-fields).

## `GET, POST /api/mesh-test`

Sends a status request to every known agent and reports which ones answered. `POST` always runs a new test. `GET` returns the result of the last test if it is less than 30 seconds old, and runs a new test otherwise. Each agent gets up to 10 seconds to answer. The [`muti-metroo mesh-test`](/cli/mesh-test) command calls this endpoint.

```bash
curl -X POST http://localhost:8080/api/mesh-test
```

**Response (200):**

```json
{
  "local_agent": "93cdf211",
  "test_time": "2026-10-11T14:52:10.393997+03:00",
  "duration_ms": 12,
  "total_count": 3,
  "reachable_count": 2,
  "results": [
    {
      "agent_id": "93cdf211bb05aa48900a61ab6699fbdd",
      "short_id": "93cdf211",
      "display_name": "gateway",
      "is_local": true,
      "reachable": true,
      "response_time_ms": 0
    },
    {
      "agent_id": "653422d2fc10b554514c5cbe6e6cd6cd",
      "short_id": "653422d2",
      "display_name": "transit",
      "is_local": false,
      "reachable": true,
      "response_time_ms": 3
    },
    {
      "agent_id": "331974cd4a0be0d7f945ed9d367a0211",
      "short_id": "331974cd",
      "display_name": "exit-1",
      "is_local": false,
      "reachable": false,
      "response_time_ms": -1,
      "error": "context deadline exceeded"
    }
  ]
}
```

| Field | Description |
| ----- | ----------- |
| `local_agent` | Short ID of the agent that ran the test |
| `test_time` | When the test started |
| `duration_ms` | Test duration in milliseconds |
| `total_count`, `reachable_count` | Agents tested and agents that answered |
| `results[].agent_id`, `short_id`, `display_name`, `is_local` | Agent identity |
| `results[].reachable` | The agent answered |
| `results[].response_time_ms` | Round-trip time in milliseconds; `-1` when the request could not be delivered |
| `results[].error` | Failure reason; present only for unreachable agents |

Methods other than `GET` and `POST` return `405`.

## `GET /api/interfaces`

Lists every IP address on the local agent's up interfaces, IPv4 and IPv6, including loopback and link-local addresses. Use it to choose a `source_ip` for a route (see [Route Management](/api/route-management#set_source_ip)). The remote equivalent is [`GET /agents/{id}/interfaces`](/api/agents#get-agentsidinterfaces).

```bash
curl http://localhost:8080/api/interfaces
```

**Response (200):**

```json
{
  "addresses": [
    {
      "interface": "lo",
      "ip": "127.0.0.1",
      "family": "ipv4",
      "loopback": true,
      "link_local": false,
      "global": false
    },
    {
      "interface": "eth0",
      "ip": "192.168.1.10",
      "family": "ipv4",
      "loopback": false,
      "link_local": false,
      "global": true
    },
    {
      "interface": "eth0",
      "ip": "fe80::c39:f90e:431b:f479",
      "family": "ipv6",
      "loopback": false,
      "link_local": true,
      "global": false
    }
  ]
}
```

| Field | Description |
| ----- | ----------- |
| `interface` | Interface name |
| `ip` | Address |
| `family` | `ipv4` or `ipv6` |
| `loopback` | Loopback address |
| `link_local` | Link-local address |
| `global` | Neither loopback nor link-local |

## `GET /api/setup-defaults`

Returns settings from this agent that a configuration wizard can copy into a new agent's configuration, such as the CA certificate, management public keys, and how to reach this agent as a peer. Private keys and this agent's certificate are never included. The response carries `Cache-Control: no-store`.

```bash
curl http://localhost:8080/api/setup-defaults
```

**Response (200):**

```json
{
  "agent_id": "93cdf211bb05aa48900a61ab6699fbdd",
  "display_name": "gateway",
  "version": "v4.10.6",
  "tls": {
    "ca_pem": "",
    "strict": false,
    "mtls": false,
    "self_signed": true
  },
  "management": {
    "enabled": false,
    "public_key": "",
    "has_private_key": false,
    "signing_public_key": "123101429cb2e1af6caeba09957d986ba1f46cdb0944d68c17d5b20ef28dcf46",
    "has_signing_private_key": true
  },
  "listener": {
    "transport": "quic",
    "address": "0.0.0.0:4433",
    "path": "",
    "plaintext": false
  },
  "suggested_peer": {
    "address": "192.168.1.10:4433",
    "transport": "quic",
    "expected_id": "93cdf211bb05aa48900a61ab6699fbdd"
  },
  "http": {
    "enabled": true
  },
  "shell": {
    "enabled": false
  },
  "file_transfer": {
    "enabled": false
  }
}
```

| Field | Description |
| ----- | ----------- |
| `agent_id`, `display_name`, `version` | This agent |
| `tls.ca_pem` | CA certificate PEM, empty when none is configured |
| `tls.strict`, `tls.mtls` | TLS verification settings |
| `tls.self_signed` | The agent uses an auto-generated certificate |
| `management.public_key`, `management.signing_public_key` | Management public keys |
| `management.enabled` | `management.public_key` is set |
| `management.has_private_key`, `management.has_signing_private_key` | Whether this agent holds the private keys (the keys themselves are not returned) |
| `listener` | The first configured listener |
| `suggested_peer` | Peer entry for dialing this agent. An unspecified listen address such as `0.0.0.0` is replaced with the agent's first routable IPv4 address, or IPv6 when there is none. |
| `http.token_hash` | bcrypt hash of the HTTP bearer token, when set |
| `shell.whitelist`, `file_transfer.allowed_paths` | Present when set |
| `shell.password_hash`, `file_transfer.password_hash` | Deprecated settings that the agent ignores; echoed when present in the configuration |

## Errors

| Status | Body | Cause |
| ------ | ---- | ----- |
| 404 | HTML page | `http.dashboard` is disabled |
| 405 | `method not allowed` | Wrong method |
| 503 | `provider not configured` | The agent has not finished starting |

## See Also

- [Muti Metroo Manager](/muti-metroo-manager) - Web dashboard that uses these endpoints
- [Agent Endpoints](/api/agents) - Status of individual remote agents
- [Probe Endpoints](/api/probe) - Test a single listener
- [CLI - mesh-test](/cli/mesh-test) - Mesh test from the command line
- [HTTP Configuration](/configuration/http) - Enable or disable the dashboard group
