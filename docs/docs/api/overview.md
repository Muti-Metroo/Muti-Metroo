---
title: HTTP API Overview
description: Endpoint index, endpoint groups, authentication, and error conventions for the Muti Metroo agent HTTP API.
sidebar_position: 1
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-wiring.png" alt="Mole connecting APIs" style={{maxWidth: '180px'}} />
</div>

# HTTP API Overview

Every agent can run an HTTP server that exposes health probes, mesh status, runtime configuration changes, and remote operations on other agents. The `muti-metroo` CLI management commands and Muti Metroo Manager are clients of this API.

The server is off by default. Enable it in the [`http` configuration section](/configuration/http):

```yaml
http:
  enabled: true
  address: "127.0.0.1:8080"
```

All examples in this section use `http://localhost:8080` as the base URL.

## Endpoint index

| Method | Path | Group | Purpose |
| ------ | ---- | ----- | ------- |
| GET | [`/health`](/api/health#get-health) | always on | Liveness probe (plain text) |
| GET | [`/healthz`](/api/health#get-healthz) | always on | Health with statistics (JSON) |
| GET | [`/ready`](/api/health#get-ready) | always on | Readiness probe (plain text) |
| GET | `/` | always on | Splash page (HTML logo, no data) |
| GET | [`/agents`](/api/agents#get-agents) | remote API | List known agents |
| GET | [`/agents/{id}`](/api/agents#get-agentsid) | remote API | Status of a remote agent |
| GET | [`/agents/{id}/routes`](/api/agents#get-agentsidroutes) | remote API | CIDR route table of a remote agent |
| GET | [`/agents/{id}/peers`](/api/agents#get-agentsidpeers) | remote API | Connected peers of a remote agent |
| GET | [`/agents/{id}/interfaces`](/api/agents#get-agentsidinterfaces) | remote API | Interface addresses of a remote agent |
| POST | [`/routes/advertise`](/api/route-management#post-routesadvertise) | remote API | Advertise routes immediately |
| POST | [`/routes/manage`](/api/route-management#post-routesmanage) | remote API | Add, remove, list, and edit dynamic routes |
| POST | [`/agents/{id}/routes/manage`](/api/route-management#post-agentsidroutesmanage) | remote API | Same, on a remote agent |
| POST | [`/route/trace`](/api/route-trace#post-routetrace) | remote API | Explain route selection for a destination |
| POST | [`/agents/{id}/route/trace`](/api/route-trace#post-agentsidroutetrace) | remote API | Same, on a remote agent |
| POST | [`/forward/manage`](/api/forward-management#post-forwardmanage) | remote API | Forward listeners (ingress side) |
| POST | [`/agents/{id}/forward/manage`](/api/forward-management#post-agentsidforwardmanage) | remote API | Same, on a remote agent |
| POST | [`/forward/endpoints/manage`](/api/forward-management#post-forwardendpointsmanage) | remote API | Forward endpoints (exit side) |
| POST | [`/agents/{id}/forward/endpoints/manage`](/api/forward-management#post-agentsidforwardendpointsmanage) | remote API | Same, on a remote agent |
| POST | [`/peers/manage`](/api/peer-management#post-peersmanage) | remote API | Dynamic outbound peers |
| POST | [`/agents/{id}/peers/manage`](/api/peer-management#post-agentsidpeersmanage) | remote API | Same, on a remote agent |
| POST | [`/listeners/manage`](/api/listener-management#post-listenersmanage) | remote API | Dynamic transport listeners |
| POST | [`/agents/{id}/listeners/manage`](/api/listener-management#post-agentsidlistenersmanage) | remote API | Same, on a remote agent |
| POST | [`/display-name/manage`](/api/display-name-management#post-display-namemanage) | remote API | Get or set the display name |
| POST | [`/agents/{id}/display-name/manage`](/api/display-name-management#post-agentsiddisplay-namemanage) | remote API | Same, on a remote agent |
| POST | [`/hosts/manage`](/api/hosts-management#post-hostsmanage) | remote API | Mesh hostname registry (local agent only) |
| POST | [`/hosts/suffixes/manage`](/api/hosts-management#post-hostssuffixesmanage) | remote API | Mesh hostname suffixes (local agent only) |
| GET | [`/agents/{id}/shell`](/api/shell) | remote API | Management command session (WebSocket) |
| GET | [`/agents/{id}/icmp`](/api/icmp) | remote API | ICMP echo session (WebSocket) |
| POST | [`/agents/{id}/file/upload`](/api/file-transfer#post-agentsidfileupload) | remote API | Upload a file or directory |
| POST | [`/agents/{id}/file/download`](/api/file-transfer#post-agentsidfiledownload) | remote API | Download a file or directory |
| POST | [`/agents/{id}/file/browse`](/api/file-transfer#post-agentsidfilebrowse) | remote API | List, stat, chmod, and delete files |
| POST | [`/sleep`](/api/sleep#post-sleep) | remote API | Put the mesh to sleep |
| POST | [`/wake`](/api/sleep#post-wake) | remote API | Wake the mesh |
| GET | [`/sleep/status`](/api/sleep#get-sleepstatus) | remote API | Sleep state of this agent |
| POST | [`/shutdown`](/api/stop#post-shutdown) | remote API | Stop this agent |
| POST | [`/agents/{id}/shutdown`](/api/stop#post-agentsidshutdown) | remote API | Stop a remote agent |
| POST | [`/agents/{id}/probe`](/api/probe#post-agentsidprobe) | remote API | Probe a listener from a remote agent |
| GET | [`/api/dashboard`](/api/dashboard#get-apidashboard) | dashboard | Agent, peers, routes, forwards, mesh hosts |
| GET | [`/api/topology`](/api/dashboard#get-apitopology) | dashboard | Agents and links for topology views |
| GET | [`/api/nodes`](/api/dashboard#get-apinodes) | dashboard | System details of every known agent |
| GET, POST | [`/api/mesh-test`](/api/dashboard#get-post-apimesh-test) | dashboard | Reachability test of every known agent |
| GET | [`/api/interfaces`](/api/dashboard#get-apiinterfaces) | dashboard | Interface addresses of this agent |
| GET | [`/api/setup-defaults`](/api/dashboard#get-apisetup-defaults) | dashboard | Non-secret defaults for configuration wizards |
| POST | [`/api/probe`](/api/probe#post-apiprobe) | dashboard | Probe a listener from this agent |
| GET | [`/debug/pprof/`](/api/debugging) | pprof | Go runtime profiling |

`{id}` is always the full 32-character hex agent ID, for example `331974cd4a0be0d7f945ed9d367a0211`. The 8-character short form is rejected with `400 invalid agent ID format`. Get full IDs from [`GET /agents`](/api/agents#get-agents).

The `/agents/{id}/...` endpoints reach the target agent over the mesh. They do not work against the agent serving the request, because an agent has no mesh route to itself (the one exception is `file/browse`, which is answered locally). Use the local endpoint instead.

## Endpoint groups

Endpoints are enabled in groups. A disabled group answers every request with `404 Not Found` and an HTML error page.

| Group | Config key | Default | Endpoints |
| ----- | ---------- | ------- | --------- |
| always on | none | enabled | `/health`, `/healthz`, `/ready`, `/`, `/logo.png`, `/notfound.png` |
| remote API | `http.remote_api` | `true` | `/agents`, `/agents/...`, `/routes/...`, `/route/trace`, `/forward/...`, `/peers/manage`, `/listeners/manage`, `/display-name/manage`, `/hosts/...`, `/sleep`, `/sleep/status`, `/wake`, `/shutdown` |
| dashboard | `http.dashboard` | `true` | `/api/...` |
| pprof | `http.pprof` | `true` | `/debug/pprof/...` |

`http.minimal: true` disables the remote API, dashboard, and pprof groups regardless of their own keys, leaving only the always-on endpoints.

## Authentication

Three independent checks can apply to a request.

### Bearer token

When `http.token_hash` is set, every endpoint except `/health`, `/healthz`, `/ready`, `/`, `/logo.png`, and `/notfound.png` requires the plaintext token:

```bash
curl -H "Authorization: Bearer my-secret-token" http://localhost:8080/agents
```

Clients that cannot set headers, such as browser WebSocket clients, can pass the token as a query parameter instead:

```text
ws://localhost:8080/agents/331974cd4a0be0d7f945ed9d367a0211/shell?mode=stream&token=my-secret-token
```

A missing or wrong token returns:

```text
HTTP/1.1 401 Unauthorized
Www-Authenticate: Bearer realm="muti-metroo"

unauthorized
```

Generate the hash with [`muti-metroo hash`](/cli/hash). The CLI sends the token from `--token` or the `MUTI_METROO_TOKEN` environment variable. When `token_hash` is empty (the default), no token is required.

:::warning
Without `http.token_hash`, anyone who can reach the HTTP port can use every enabled endpoint, including mutations, file transfer, management commands, and shutdown. The management signing key does not protect against this, because the agent signs requests on the caller's behalf. The agent prints a warning at startup when the HTTP server binds to a non-loopback address without a token. Bind to `127.0.0.1` or set a token.
:::

### Management signing key

Privileged operations are signed with the mesh management signing key (Ed25519). The agent serving the HTTP request signs them on the caller's behalf, so it needs `management.signing_private_key`; the agent that executes them needs `management.signing_public_key` to verify the signature.

| Operation | Requires |
| --------- | -------- |
| `add`, `remove`, `set`, `set_comment`, `set_source_ip`, `set_interface` actions on any `.../manage` endpoint | Signing private key on the serving agent; signing public key on the target for `/agents/{id}/...` |
| `POST /shutdown`, `POST /agents/{id}/shutdown` | Same as above |
| Management commands, file upload, download, and browse | Same as above |
| `list` and `get` actions, status, trace, probe, dashboard reads | Nothing beyond the bearer token |

When the serving agent has no signing private key, signed operations fail with `403` and the plain-text body `management signing private key not configured on this agent`. When the target agent rejects a signature, the target's error is returned with status `400` (management endpoints) or `401`/`403` (file operations). Sleep and wake are signed when the key is available but do not require it; see [Sleep Endpoints](/api/sleep). See [Management Keys](/configuration/management) for key generation and distribution.

### Topology restriction

When `management.public_key` is set but `management.private_key` is not, the agent cannot decrypt mesh topology and restricts what it reveals:

- `GET /agents`, `/api/topology`, `/api/nodes`, `/api/dashboard`, and `/api/mesh-test` report only the local agent.
- `GET /agents/{id}`, every `/agents/{id}/.../manage` endpoint, `/agents/{id}/route/trace`, `/agents/{id}/probe`, `/agents/{id}/file/browse`, and the local `/routes/manage`, `/route/trace`, `/forward/manage`, `/forward/endpoints/manage`, `/peers/manage`, `/listeners/manage`, and `/display-name/manage` return `403` with a body such as `route management restricted: management key decryption unavailable`.

## Responses and errors

Successful responses are JSON (`Content-Type: application/json`) except for the plain-text probes `/health` and `/ready`, file downloads (binary), the WebSocket endpoints, and pprof.

Errors come in two shapes:

- **JSON** `{"error": "..."}` for validation and operation errors returned by the `.../manage`, trace, and probe endpoints.
- **Plain text** for errors detected before the request body is processed: wrong method, missing provider, missing signing key, topology restriction, and mesh delivery failures.

| Status | Meaning |
| ------ | ------- |
| 200 | Success. Diagnostic endpoints (probe, mesh test, trace) also use 200 when the diagnosed target failed; check the body. |
| 400 | Invalid JSON, invalid parameters, or an operation the agent rejected (including errors returned by a remote target). |
| 401 | Missing or wrong bearer token. File operations also use 401 when the target reports a missing signature. |
| 403 | No signing private key on the serving agent, topology restriction, a protected config entry (`/hosts/manage`), or a rejected file-operation signature. |
| 404 | Endpoint group disabled, or unknown path (HTML error page). `/hosts/manage` also uses 404 for unknown entries. |
| 405 | Wrong HTTP method. Body: `method not allowed`. |
| 409 | Conflict in `/hosts/manage` or `/hosts/suffixes/manage`. |
| 500 | Internal failure, for example `already sleeping`. |
| 502 | The request could not be delivered to a remote agent, or a remote file transfer failed. |
| 503 | The feature is not available on this agent, for example `sleep mode not enabled`. |

## See Also

- [HTTP Configuration](/configuration/http): enable the server and choose endpoint groups
- [Management Keys](/configuration/management): signing and topology encryption keys
- [Authentication](/security/authentication): authorization model for privileged operations
- [CLI Overview](/cli/overview): CLI commands that call this API
- [Muti Metroo Manager](/muti-metroo-manager): web dashboard built on this API
