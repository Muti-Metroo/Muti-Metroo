---
title: Authentication
description: How Muti Metroo authenticates SOCKS clients, HTTP API callers, peers, and privileged operations signed with the mesh signing key.
sidebar_position: 4
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-inspecting.png" alt="Mole checking authentication" style={{maxWidth: '180px'}} />
</div>

# Authentication

Each way into an agent has its own authentication mechanism:

| Entry point | Mechanism | Default |
| ----------- | --------- | ------- |
| SOCKS listener | Username and bcrypt-hashed password | No authentication |
| HTTP API | Bearer token checked against a bcrypt hash | No authentication |
| Peer connections | TLS certificates, optionally mutual TLS | Not verified |
| Privileged operations (management commands, file transfer, runtime configuration changes, remote stop) | Ed25519 signature with the mesh signing key | Disabled until keys are configured |
| Sleep and wake | Ed25519 signature with the mesh signing key | Accepted unsigned until keys are configured |

Peer authentication is covered in [TLS and mTLS](/security/tls-mtls). The rest is described below.

## Management signing key

Operations that act on a remote agent's host or reshape the mesh are authorized by a single Ed25519 key pair shared by the mesh:

- `management.signing_public_key` goes on every agent. An agent with it verifies and executes signed requests.
- `management.signing_private_key` goes only on operator agents. An agent with it signs requests on behalf of whoever calls its HTTP API.

```bash
muti-metroo signing-key generate
```

```yaml
# Every agent
management:
  signing_public_key: "86d6714fc48cd41afd5051e6ad9523ad889c806001e92bc739913a18c6709a83"
```

```yaml
# Operator agents only
management:
  signing_public_key: "86d6714fc48cd41afd5051e6ad9523ad889c806001e92bc739913a18c6709a83"
  signing_private_key: "${MESH_SIGNING_PRIVATE_KEY}" # 128 hex characters from signing-key generate
```

See [signing-key CLI](/cli/signing-key) and [Management Key Configuration](/configuration/management#command-signing-keys).

### What the key authorizes

| Operation | Where it runs | Requirement |
| --------- | ------------- | ----------- |
| Management commands (`muti-metroo shell`) | Target agent | Gateway signs, target verifies |
| File upload, download, and browse (including `chmod` and `delete`) | Target agent | Gateway signs, target verifies |
| Runtime configuration changes with `-t`: peers, listeners, routes (add, remove, comment, source IP, interface), forwards, forward endpoints, display name | Target agent | Gateway signs, target verifies |
| Stop a remote agent | Target agent | Gateway signs, target verifies |
| Runtime configuration changes without `-t`, and mesh hostname changes | Local agent | Local agent holds the private key; nothing is signed |
| Sleep and wake | Every agent the command reaches | Signed when the issuing agent holds the private key; verified by every agent with the public key |

A target agent without `signing_public_key` refuses every operation in the table except sleep and wake. A gateway agent without `signing_private_key` cannot issue any of them.

Reading operations are not signed: route tables, peer lists, `route trace`, `mesh-test`, `probe`, `sleep-status`, and `ping`. They are protected only by the HTTP API token. `ping` sends ICMP from the target agent without a signature, subject only to the target's `icmp` settings.

### How a request is signed

1. The CLI sends the request to the gateway agent's HTTP API. The CLI itself holds no key.
2. The gateway agent signs an envelope containing its own agent ID, the target agent ID, a timestamp, and the operation type. For configuration changes, the envelope also covers a hash of the request body.
3. The target agent checks that the envelope names it as the target, that the timestamp is within five minutes of its own clock, and that the signature verifies against its `signing_public_key`.

A signed envelope is valid for that five-minute window and has no one-time nonce. Keep agent clocks synchronized; a skew of more than five minutes causes every signed operation to fail.

:::warning The gateway signs for its callers
An operator agent signs every privileged request that reaches its HTTP API. Anyone who can call that API can therefore run commands and transfer files on every agent in the mesh. Bind operator agents' HTTP API to `127.0.0.1` and set `http.token_hash`.
:::

### Query an agent's role

`/healthz` (and `GET /agents/{agent-id}` for remote agents) reports both halves:

```json
{"accepts_signed_management":true,"can_sign_management":true,"exit_handler_running":false,"peer_count":1,"route_count":1,"running":true,"socks5_running":true,"status":"healthy","stream_count":0}
```

- `can_sign_management` is `true` when the agent holds `signing_private_key` and can act as a gateway.
- `accepts_signed_management` is `true` when the agent holds `signing_public_key` and can be a target.

### Common errors

| Message | Meaning |
| ------- | ------- |
| `management signing private key not configured on this agent` | The agent the CLI talks to cannot sign. Use an operator agent with `-a`. |
| `management signing public key not configured on this agent` | The target cannot verify signatures and refuses the operation. |
| `management signature verification failed` | The key pair does not match. |
| `management signature outside timestamp window` | The two agents' clocks differ by more than five minutes. |

### Sleep and wake

Sleep and wake commands spread from agent to agent, and each agent checks them on its own:

- Every agent rejects a command whose timestamp is more than five minutes from its own clock.
- An agent with `signing_public_key` also rejects a command that is unsigned or signed with another key.
- An agent without `signing_public_key` accepts unsigned commands from any peer.

The agent whose HTTP API receives the `sleep` or `wake` request acts on it even if it cannot sign it. To restrict sleep and wake to operators, configure `signing_public_key` on every agent and protect every agent's HTTP API. See [Sleep Mode](/features/sleep-mode#restrict-who-can-sleep-and-wake-the-mesh).

## HTTP API token

Set `http.token_hash` to a bcrypt hash of a token you choose:

```bash
muti-metroo hash
```

```yaml
http:
  address: "127.0.0.1:8080"
  token_hash: "$2a$12$dTB6hp0rb6lWsON4uHeTHOSHvO6mzANiuI4ZOv9.MfAhpRiKUvkie"
```

Requests must then present the token as `Authorization: Bearer <token>` or, for clients that cannot set headers (such as browser WebSocket connections), as a `?token=<token>` query parameter. The CLI sends it from `--token` or the `MUTI_METROO_TOKEN` environment variable. A missing or wrong token returns `401 unauthorized`.

`/health`, `/healthz`, `/ready`, and the splash page at `/` (with its images) are exempt. Tokens in query strings can end up in proxy and server logs; prefer the header.

See [HTTP API Configuration](/configuration/http) for minimal mode and per-endpoint switches.

## SOCKS authentication

Enable username and password authentication on the SOCKS listener:

```yaml
socks5:
  enabled: true
  address: "127.0.0.1:1080"
  auth:
    enabled: true
    users:
      - username: "operator"
        password_hash: "$2a$12$dTB6hp0rb6lWsON4uHeTHOSHvO6mzANiuI4ZOv9.MfAhpRiKUvkie"
```

```bash
curl -x socks5h://operator:secret@localhost:1080 https://example.com
ssh -o ProxyCommand='ncat --proxy localhost:1080 --proxy-type socks5 --proxy-auth operator:secret %h %p' user@192.168.1.20
```

- Usernames are case-sensitive.
- SOCKS4 and SOCKS4a clients are refused when authentication is enabled, because those protocols carry no password.
- Failed logins are not logged.
- The same credentials gate UDP relay and Mutiauk's `expose` registrations, which both start on the SOCKS listener.

See [SOCKS5 Configuration](/configuration/socks5#authentication) for all options.

## Password hashes

Token and password hashes are bcrypt. Generate them with `muti-metroo hash`, which prompts for the value without echoing it:

```bash
muti-metroo hash
muti-metroo hash --cost 14
```

The default cost is 12; the allowed range is 4 to 31. Each increment doubles the time to compute a hash, for an attacker and for every login.

Hashes are not validated when the configuration loads. A malformed or truncated hash makes every login fail, so if authentication never succeeds, generate the hash again and check for stray whitespace.

## Keep secrets out of configuration files

Any configuration value, including `http.token_hash`, `socks5.auth.users[].password_hash`, and `management.signing_private_key`, can be read from an environment variable with `${VAR}` syntax. [Best Practices](/security/best-practices#secrets-in-configuration) shows a complete example, and [Environment Variables](/configuration/environment-variables) describes the syntax.

## See Also

- [Access Control](/security/access-control): what authenticated users can reach and run
- [TLS and mTLS](/security/tls-mtls): authenticate peer connections
- [signing-key CLI](/cli/signing-key): generate and inspect the signing key pair
- [HTTP API Configuration](/configuration/http): token and endpoint settings
- [Best Practices](/security/best-practices): production hardening
