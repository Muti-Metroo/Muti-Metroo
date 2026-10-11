---
title: HTTP API
description: Configuration reference for the http section, covering the API server address and timeouts, bearer token authentication, minimal mode, and the endpoint group switches.
sidebar_position: 16
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-presenting.png" alt="Mole configuring HTTP API" style={{maxWidth: '180px'}} />
</div>

# HTTP API

The `http` section starts the agent's HTTP server. It serves health checks, the dashboard API used by Muti Metroo Manager, the management API that the `muti-metroo` CLI talks to, and optional Go profiling endpoints.

## Configuration

```yaml
http:
  enabled: true
  address: "127.0.0.1:8080"
  read_timeout: 10s
  write_timeout: 10s
  token_hash: ""      # bcrypt hash of the bearer token; empty = no authentication
  minimal: false      # true = health endpoints only
  pprof: true         # /debug/pprof/*
  dashboard: true     # /api/*
  remote_api: true    # /agents/*, management and mutation endpoints
```

## Keys

| Key             | Type     | Default   | Description                                                                                                         |
| --------------- | -------- | --------- | ------------------------------------------------------------------------------------------------------------------- |
| `enabled`       | bool     | `false`   | Start the HTTP server.                                                                                              |
| `address`       | string   | `":8080"` | Bind address. The default listens on all interfaces.                                                                |
| `read_timeout`  | duration | `10s`     | Maximum time to read a request.                                                                                     |
| `write_timeout` | duration | `10s`     | Maximum time to write a response.                                                                                   |
| `token_hash`    | string   | `""`      | bcrypt hash of the API bearer token. When set, every endpoint except the [exempt ones](#authentication) needs the token. Redacted by `config show`. |
| `minimal`       | bool     | `false`   | Serve only the health endpoints. Overrides `pprof`, `dashboard`, and `remote_api`.                                  |
| `pprof`         | bool     | `true`    | Serve Go profiling endpoints.                                                                                       |
| `dashboard`     | bool     | `true`    | Serve the dashboard API.                                                                                            |
| `remote_api`    | bool     | `true`    | Serve the management API.                                                                                           |

## Endpoint groups

| Switch       | Paths                                                                                                                                                       |
| ------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------- |
| always on    | `/health`, `/healthz`, `/ready`, and the `/` splash page                                                                                                    |
| `dashboard`  | `/api/*` (topology, dashboard, nodes, mesh test, probe, interfaces, setup defaults)                                                                         |
| `remote_api` | `/agents`, `/agents/*`, `/routes/advertise`, `/routes/manage`, `/route/trace`, `/peers/manage`, `/listeners/manage`, `/forward/manage`, `/forward/endpoints/manage`, `/display-name/manage`, `/hosts/manage`, `/hosts/suffixes/manage`, `/sleep`, `/sleep/status`, `/wake`, `/shutdown` |
| `pprof`      | `/debug/pprof/*`                                                                                                                                            |

A disabled group returns HTTP 404. Most `muti-metroo` CLI commands that talk to an agent, and Muti Metroo Manager, need `remote_api`, and Manager also needs `dashboard`. Endpoint details are in the [HTTP API reference](/api/overview).

## Authentication

With `token_hash` set, requests must carry the token as `Authorization: Bearer <token>`, or as a `?token=<token>` query parameter for WebSocket clients. A missing or wrong token gets HTTP 401. These paths never need the token: `/health`, `/healthz`, `/ready`, `/`, `/logo.png`, and `/notfound.png`.

Generate the hash with [`muti-metroo hash`](/cli/hash) and put it in the configuration:

```bash
muti-metroo hash
```

```yaml
http:
  enabled: true
  address: ":8080"
  token_hash: "$2a$12$GiitCO3IBCFV2/QmQ4BzNOQGykcLM1DtPyvfMB7kF5qZLDGk6BtnS"
```

The CLI sends the token from `--token` or the `MUTI_METROO_TOKEN` environment variable:

```bash
export MUTI_METROO_TOKEN=my-secret-token
muti-metroo status
curl -H "Authorization: Bearer my-secret-token" http://localhost:8080/agents
```

Without `token_hash`, every enabled endpoint is open to anyone who can reach `address`, including the endpoints that change configuration, transfer files, open management command sessions, and stop the agent. The management signing key does not protect against such callers, because the agent signs requests made through its own API. When `address` is not a loopback address and `token_hash` is empty, the agent prints a warning at startup.

## Minimal mode

```yaml
http:
  enabled: true
  address: "127.0.0.1:8080"
  minimal: true
```

Only `/health`, `/healthz`, and `/ready` are served, which is enough for load balancer and container health probes.

## Examples

### Local access only

```yaml
http:
  enabled: true
  address: "127.0.0.1:8080"
  pprof: false
```

### Remote access with a token

```yaml
http:
  enabled: true
  address: ":8080"
  token_hash: "${HTTP_TOKEN_HASH}"
  pprof: false
```

## See Also

- [HTTP API reference](/api/overview) for every endpoint, request, and response
- [Authentication](/security/authentication) for how the bearer token relates to management signing keys
- [hash CLI](/cli/hash) to generate `token_hash`
- [Health API](/api/health) for the probe endpoints that stay open in minimal mode
- [Management](/configuration/management) for the signing keys that authorize management operations
