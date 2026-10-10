---
title: Dashboard API Endpoints
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-presenting.png" alt="Mole presenting dashboard" style={{maxWidth: '180px'}} />
</div>

# Dashboard API Endpoints

Fetch topology data, dashboard overview, node details, and run mesh connectivity tests via JSON API endpoints.

```bash
# Get dashboard overview
curl http://localhost:8080/api/dashboard | jq

# Get topology data
curl http://localhost:8080/api/topology | jq
```

## GET /api/dashboard

Dashboard overview data.

**Response:**

```json
{
  "agent": {
    "id": "abc123def456789012345678901234ab",
    "short_id": "abc123de",
    "display_name": "My Agent",
    "is_local": true,
    "is_connected": true
  },
  "stats": {
    "peer_count": 3,
    "stream_count": 42,
    "route_count": 5,
    "socks5_running": true,
    "exit_handler_running": false
  },
  "peers": [
    {
      "id": "def456789012345678901234567890cd",
      "short_id": "def45678",
      "display_name": "Peer 1",
      "state": "connected",
      "rtt_ms": 15,
      "is_dialer": true
    }
  ],
  "routes": [
    {
      "network": "10.0.0.0/8",
      "origin": "Exit Node",
      "origin_id": "exit1234",
      "hop_count": 2,
      "path_display": ["My Agent", "Transit", "Exit Node"]
    }
  ],
  "forward_routes": [
    {
      "key": "web-server",
      "ingress_agent": "Ingress Node",
      "ingress_agent_id": "ingr1234",
      "listener_address": ":9080",
      "exit_agent": "Exit Node",
      "exit_agent_id": "exit1234",
      "target": "localhost:3000",
      "hop_count": 2,
      "path_display": ["Ingress Node", "Exit Node"],
      "path_ids": ["ingr1234", "exit1234"]
    }
  ],
  "mesh_hosts_suffixes": ["mesh"],
  "mesh_hosts": [
    { "name": "gateway.mesh", "ipv4": "10.0.0.1", "ipv6": "fd00::1" },
    { "name": "target-11.mesh", "ipv4": "10.8.0.11", "comment": "edge worker" }
  ]
}
```

### Forward Routes Fields

The `forward_routes` array contains ingress-exit pairs for port forwarding:

| Field              | Description                                |
| ------------------ | ------------------------------------------ |
| `key`              | Routing key linking listeners to endpoints |
| `ingress_agent`    | Display name of the ingress agent          |
| `ingress_agent_id` | Short ID of the ingress agent              |
| `listener_address` | Listen address on the ingress agent        |
| `exit_agent`       | Display name of the exit agent             |
| `exit_agent_id`    | Short ID of the exit agent                 |
| `target`           | Target service address on the exit         |
| `hop_count`        | Hops from ingress to exit agent            |
| `path_display`     | Agent names in the path                    |
| `path_ids`         | Short IDs for path highlighting            |

When multiple ingress agents have listeners for the same key, or multiple exit agents have endpoints, all combinations are returned.

### Mesh Hosts Fields

The `mesh_hosts` array and `mesh_hosts_suffixes` list expose the agent's mesh hostname registry (the merged config + dynamic set) and the active TLD set. Mutiauk's autoroutes poller reads these to populate its in-process DNS server, and the agent itself consults the registry for SOCKS5 dial and route-trace hostname resolution. The fields are omitted when no entries / suffixes are configured.

| Field                  | Description                                                                                                                          |
| ---------------------- | ------------------------------------------------------------------------------------------------------------------------------------ |
| `mesh_hosts_suffixes`  | Active TLD suffix set (default `["mesh"]`, no leading dots). Includes runtime add/remove mutations made via `/hosts/suffixes/manage` |
| `mesh_hosts[].name`    | Hostname including suffix, e.g. `target-11.mesh`                                                                                     |
| `mesh_hosts[].ipv4`    | Optional IPv4 address                                                                                                                |
| `mesh_hosts[].ipv6`    | Optional IPv6 address                                                                                                                |
| `mesh_hosts[].comment` | Optional free-text comment                                                                                                           |

See [Hosts Configuration](/configuration/hosts) for the YAML schema and [Hosts Management API](/api/hosts-management) for runtime mutation of both hosts and suffixes.

## GET /api/topology

Mesh topology data for visualization.

**Response:**

```json
{
  "local_agent": {
    "id": "abc123def456789012345678901234ab",
    "short_id": "abc123de",
    "display_name": "My Agent",
    "is_local": true,
    "is_connected": true,
    "hostname": "server1.example.com",
    "os": "linux",
    "arch": "amd64",
    "version": "1.0.7",
    "uptime_hours": 24.5,
    "ip_addresses": ["192.168.1.10", "10.0.0.1"],
    "roles": ["ingress"],
    "socks5_addr": ":1080",
    "shells": ["bash", "sh", "zsh"],
    "shell_enabled": true
  },
  "agents": [
    {
      "id": "abc123def456789012345678901234ab",
      "short_id": "abc123de",
      "display_name": "My Agent",
      "is_local": true,
      "is_connected": true,
      "hostname": "server1.example.com",
      "os": "linux",
      "arch": "amd64",
      "version": "1.0.7",
      "uptime_hours": 24.5,
      "ip_addresses": ["192.168.1.10", "10.0.0.1"],
      "roles": ["ingress"],
      "socks5_addr": ":1080",
      "shells": ["bash", "sh", "zsh"],
      "shell_enabled": true
    },
    {
      "id": "def456789012345678901234567890cd",
      "short_id": "def45678",
      "display_name": "Peer 1",
      "is_local": false,
      "is_connected": true,
      "hostname": "peer1.example.com",
      "os": "linux",
      "arch": "amd64",
      "version": "1.0.7",
      "uptime_hours": 48.0,
      "ip_addresses": ["192.168.1.20"],
      "roles": ["exit"],
      "exit_routes": ["10.0.0.0/8", "0.0.0.0/0"],
      "domain_routes": ["*.internal.example.com"],
      "udp_enabled": true,
      "file_transfer_enabled": true,
      "shells": ["bash", "sh"],
      "shell_enabled": true,
      "advertised_listeners": [
        {
          "transport": "h2",
          "address": "peer1.example.com:3000",
          "path": "/mesh"
        }
      ]
    }
  ],
  "connections": [
    {
      "from_agent": "abc123de",
      "to_agent": "def45678",
      "is_direct": true,
      "rtt_ms": 15,
      "transport": "quic",
      "dialer": "abc123de"
    }
  ]
}
```

**`advertised_listeners`** appears on any agent whose config declares an `advertise:` block on a listener (see [Listeners Configuration](/configuration/listeners)). Empty when no listener is advertised.

**`dialer`** is the short ID of the agent that initiated the connection. Useful for direction arrows in topology renders. Empty when unknown.

## GET/POST /api/mesh-test

Test connectivity to all known agents in the mesh. GET returns cached results (30-second TTL), POST forces a fresh test.

```bash
# Fresh test
curl -X POST http://localhost:8080/api/mesh-test | jq

# Cached results
curl http://localhost:8080/api/mesh-test | jq
```

**Response:**

```json
{
  "local_agent": "abc123de",
  "test_time": "2026-01-27T10:30:00Z",
  "duration_ms": 1200,
  "total_count": 5,
  "reachable_count": 4,
  "results": [
    {
      "agent_id": "abc123def456789012345678901234ab",
      "short_id": "abc123de",
      "display_name": "gateway-1",
      "is_local": true,
      "reachable": true,
      "response_time_ms": 0
    },
    {
      "agent_id": "def456789012345678901234567890cd",
      "short_id": "def45678",
      "display_name": "exit-us-west",
      "is_local": false,
      "reachable": true,
      "response_time_ms": 45
    },
    {
      "agent_id": "fed987654321098765432109876543ef",
      "short_id": "fed98765",
      "display_name": "exit-offline",
      "is_local": false,
      "reachable": false,
      "response_time_ms": -1,
      "error": "context deadline exceeded"
    }
  ]
}
```

### Result Fields

| Field             | Description                            |
| ----------------- | -------------------------------------- |
| `local_agent`     | Short ID of the agent running the test |
| `test_time`       | When the test was performed            |
| `duration_ms`     | Total test duration in milliseconds    |
| `total_count`     | Number of agents tested                |
| `reachable_count` | Number of reachable agents             |
| `results`         | Per-agent test results                 |

### Per-Agent Result Fields

| Field              | Description                         |
| ------------------ | ----------------------------------- |
| `agent_id`         | Full agent ID                       |
| `short_id`         | Short agent ID                      |
| `display_name`     | Agent display name                  |
| `is_local`         | Whether this is the local agent     |
| `reachable`        | Whether the agent responded         |
| `response_time_ms` | Response time (-1 if unreachable)   |
| `error`            | Error message (only if unreachable) |

Also available via CLI: `muti-metroo mesh-test`

For probing a single listener (rather than every mesh agent), see the
[Probe API](/api/probe). The probe endpoint exposes the same operation as
the `muti-metroo probe` CLI over HTTP and is gated by the same `http.dashboard`
toggle.

## GET /api/nodes

Detailed node information for all known agents.

**Response:**

```json
{
  "nodes": [
    {
      "id": "abc123def456789012345678901234ab",
      "short_id": "abc123de",
      "display_name": "Agent 1",
      "is_local": true,
      "is_connected": true,
      "hostname": "agent1.example.com",
      "os": "linux",
      "arch": "amd64",
      "version": "1.0.7",
      "uptime_hours": 24.5,
      "ip_addresses": ["192.168.1.10", "10.0.0.1"],
      "roles": ["ingress", "exit"],
      "socks5_addr": ":1080",
      "exit_routes": ["10.0.0.0/8"],
      "udp_enabled": true,
      "file_transfer_enabled": true,
      "shells": ["bash", "sh", "zsh"],
      "shell_enabled": true
    },
    {
      "id": "def456789012345678901234567890cd",
      "short_id": "def45678",
      "display_name": "Agent 2",
      "is_local": false,
      "is_connected": true,
      "hostname": "agent2.example.com",
      "os": "darwin",
      "arch": "arm64",
      "version": "1.0.7",
      "uptime_hours": 12.0,
      "ip_addresses": ["192.168.1.20"],
      "roles": ["transit"]
    }
  ]
}
```

### Node Fields

| Field                   | Type     | Description                                                                                                                |
| ----------------------- | -------- | -------------------------------------------------------------------------------------------------------------------------- |
| `id`                    | string   | Full 32-character agent ID                                                                                                 |
| `short_id`              | string   | Short 8-character agent ID                                                                                                 |
| `display_name`          | string   | Human-readable agent name                                                                                                  |
| `is_local`              | boolean  | Whether this is the queried agent                                                                                          |
| `is_connected`          | boolean  | Whether the agent is a direct peer                                                                                         |
| `hostname`              | string   | System hostname                                                                                                            |
| `os`                    | string   | Operating system (e.g., `linux`, `darwin`, `windows`)                                                                      |
| `arch`                  | string   | CPU architecture (e.g., `amd64`, `arm64`)                                                                                  |
| `version`               | string   | Agent version                                                                                                              |
| `pid`                   | number   | Operating-system process ID of the agent (best-effort; populated for both local and remote agents, omitted when unknown).  |
| `uptime_hours`          | number   | Agent uptime in hours                                                                                                      |
| `ip_addresses`          | string[] | Non-loopback IPv4 addresses                                                                                                |
| `roles`                 | string[] | Agent roles: `ingress`, `exit`, `transit`, `forward_ingress`, `forward_exit`                                               |
| `socks5_addr`           | string   | SOCKS5 listen address (ingress agents only)                                                                                |
| `exit_routes`           | string[] | Advertised CIDR routes (exit agents only)                                                                                  |
| `domain_routes`         | string[] | Advertised domain patterns (exit agents only)                                                                              |
| `udp_enabled`           | boolean  | Whether UDP relay is enabled                                                                                               |
| `file_transfer_enabled` | boolean  | Whether file transfer is enabled                                                                                           |
| `forward_listeners`     | string[] | Port forward listener keys (forward ingress agents only)                                                                   |
| `forward_endpoints`     | string[] | Port forward endpoint keys (forward exit agents only)                                                                      |
| `shells`                | string[] | Available shells detected on the agent (e.g., `["bash", "sh", "zsh"]`). Only present when management commands are enabled. |
| `shell_enabled`         | boolean  | Whether management commands are enabled on the agent                                                                       |
| `icmp_enabled`          | boolean  | Whether ICMP ping is enabled on the agent                                                                                  |

## GET /api/interfaces

Lists every IP address assigned to the agent's up network interfaces, IPv4 and
IPv6 alike. Unlike the `ip_addresses` field above (which is non-loopback IPv4
only and capped), this is the complete set. It feeds the route source-IP picker:
a route can bind its outbound dial to one of these local addresses (see
[Route Management API](/api/route-management)). Read-only; gated by the same
`http.dashboard` toggle.

The remote variant `GET /agents/{id}/interfaces` returns the addresses of a
remote agent over the mesh control channel - useful when binding a source IP on
an exit agent you are managing remotely.

**Response:**

```json
{
  "addresses": [
    {
      "interface": "eth0",
      "ip": "192.168.1.42",
      "family": "ipv4",
      "loopback": false,
      "link_local": false,
      "global": true
    },
    {
      "interface": "eth0",
      "ip": "fd00::42",
      "family": "ipv6",
      "loopback": false,
      "link_local": false,
      "global": true
    },
    {
      "interface": "lo",
      "ip": "127.0.0.1",
      "family": "ipv4",
      "loopback": true,
      "link_local": false,
      "global": false
    }
  ]
}
```

### Address Fields

| Field        | Type    | Description                               |
| ------------ | ------- | ----------------------------------------- |
| `interface`  | string  | Interface name (e.g. `eth0`)              |
| `ip`         | string  | Address literal                           |
| `family`     | string  | `ipv4` or `ipv6`                          |
| `loopback`   | boolean | Address is a loopback address             |
| `link_local` | boolean | Address is link-local                     |
| `global`     | boolean | Routable: neither loopback nor link-local |

## Examples

```bash
# Get dashboard data
curl http://localhost:8080/api/dashboard

# Get topology
curl http://localhost:8080/api/topology

# Get node details
curl http://localhost:8080/api/nodes
```

See [HTTP Configuration](/configuration/http) for endpoint access options.
