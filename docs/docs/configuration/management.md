---
title: Management
description: Configuration reference for the management section, covering the X25519 topology encryption keys and the Ed25519 signing keys that authorize privileged operations.
sidebar_position: 17
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-thinking.png" alt="Mole configuring management keys" style={{maxWidth: '180px'}} />
</div>

# Management

The `management` section holds two independent key pairs:

- **Topology encryption keys** (X25519) encrypt node information, and agents without the private key also withhold route paths from their HTTP API, so only agents with the private key can see the mesh layout. Route paths themselves travel unencrypted, because transit agents need them to forward streams.
- **Signing keys** (Ed25519) authorize privileged operations: management commands, file transfer, runtime configuration changes, and sleep and wake.

Either pair can be used without the other. Distribute the public keys to every agent and keep the private keys on operator agents only.

## Configuration

```yaml
management:
  public_key: "2395e1d946cd67ba009d37ad76df8b3ccde5fbb244a79a0d15ae342bd5dd1539"
  private_key: "782996f369d28e2da0780a6d712a292010562e4d629eb0e35c3b85b9f2c4305f"         # Operator agents only
  signing_public_key: "91ec6f66440771f9a68f2d65adb835faccdff429e97f9e4be1ce5ac10eec158f"
  signing_private_key: "2a848e10f76d1e876127904d60eb7da4a0d5d72710f3a746e778b9d0a06ab92f91ec6f66440771f9a68f2d65adb835faccdff429e97f9e4be1ce5ac10eec158f" # Operator agents only
```

## Keys

| Key                   | Type   | Default | Description                                                                                                       |
| --------------------- | ------ | ------- | ----------------------------------------------------------------------------------------------------------------- |
| `public_key`          | string | `""`    | X25519 topology encryption public key, 64 hexadecimal characters. Enables topology encryption.                   |
| `private_key`         | string | `""`    | X25519 topology encryption private key, 64 hexadecimal characters. Lets this agent decrypt topology. Requires `public_key`. Redacted by `config show`. |
| `signing_public_key`  | string | `""`    | Ed25519 public key, 64 hexadecimal characters. Lets this agent verify and execute signed privileged operations.  |
| `signing_private_key` | string | `""`    | Ed25519 private key, 128 hexadecimal characters. Lets this agent sign privileged operations. Requires `signing_public_key`. Redacted by `config show`. |

Validation checks the hex encoding and length of each key and that a private key is not set without its public key. It does not check that a private key belongs to the configured public key.

Generate the pairs with [`muti-metroo management-key generate`](/cli/management-key) and [`muti-metroo signing-key generate`](/cli/signing-key). Both print ready-to-paste snippets for field and operator agents.

## Topology encryption keys

With `public_key` set, the agent encrypts its node information (display name, system details, peer list, advertised listeners) and the agent path of its route advertisements before flooding them. Destinations (CIDRs, domain patterns, forward keys) stay readable, so routing works on every agent.

| Agent has                      | Effect                                                                                                                                      |
| ------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------- |
| no keys                        | Sends and reads topology in the clear.                                                                                                      |
| `public_key` only              | Encrypts its own topology data and cannot read anyone else's. Its HTTP API limits the `/agents/` endpoints, mesh test, and route trace to the local agent, and refuses requests aimed at other agents with HTTP 403 and a message ending in `restricted: management key decryption unavailable`. |
| `public_key` and `private_key` | Encrypts its own data and decrypts everyone's; shows the full topology.                                                                      |

All agents in a mesh need the same `public_key`. An agent without it sends its topology unencrypted, which other agents can read.

```yaml
# Field agent
management:
  public_key: "2395e1d946cd67ba009d37ad76df8b3ccde5fbb244a79a0d15ae342bd5dd1539"
```

## Command signing keys

Each privileged request carries an Ed25519 signature that binds the requesting agent, the target agent, a timestamp, and the operation type; runtime configuration changes also bind the request body. The target rejects requests that are unsigned, signed with another key, or more than 5 minutes off its clock.

| Operation                                                      | Signed |
| -------------------------------------------------------------- | ------ |
| Management commands ([`shell`](/configuration/shell))          | Always |
| File upload, download, and browse ([`file_transfer`](/configuration/file-transfer)) | Always |
| Runtime changes to routes, peers, listeners, forwards, mesh hosts, and display name | Always |
| Sleep and wake commands                                        | When the receiving agent has `signing_public_key` |

What each agent needs:

- **The agent whose HTTP API receives the request** signs it, so it needs `signing_private_key`. Without it, mutation requests through its API are refused with HTTP 403 (`management signing private key not configured on this agent`), also for changes to the agent itself.
- **The target agent** verifies the signature, so it needs `signing_public_key`. Without it, management commands, file transfer, and runtime configuration changes are refused. Sleep and wake commands are then accepted without a signature.

```yaml
# Field agent: executes signed operations
management:
  signing_public_key: "91ec6f66440771f9a68f2d65adb835faccdff429e97f9e4be1ce5ac10eec158f"
```

```yaml
# Operator agent: issues signed operations
management:
  signing_public_key: "91ec6f66440771f9a68f2d65adb835faccdff429e97f9e4be1ce5ac10eec158f"
  signing_private_key: "2a848e10f76d1e876127904d60eb7da4a0d5d72710f3a746e778b9d0a06ab92f91ec6f66440771f9a68f2d65adb835faccdff429e97f9e4be1ce5ac10eec158f"
```

The signing key authorizes requests between agents. It does not authenticate callers of the local HTTP API; use [`http.token_hash`](/configuration/http#authentication) or a loopback bind address for that.

`/healthz` reports `can_sign_management` (this agent has `signing_private_key`) and `accepts_signed_management` (it has `signing_public_key`), which clients such as Muti Metroo Manager use to show or hide management actions.

## Key rotation

There is no in-band rotation. To replace a key pair, generate a new one, update the configuration of every agent, and restart them. Agents with different keys cannot read each other's topology or accept each other's signed requests in the meantime.

## Examples

### Keys from the environment

```yaml
management:
  public_key: "${MGMT_PUBLIC_KEY}"
  signing_public_key: "${MGMT_SIGNING_PUBLIC_KEY}"
```

## See Also

- [management-key CLI](/cli/management-key) and [signing-key CLI](/cli/signing-key) to generate key pairs
- [Authentication](/security/authentication) for how signing keys, bearer tokens, and SOCKS credentials fit together
- [Shell](/configuration/shell) and [File Transfer](/configuration/file-transfer) for the operations the signing keys authorize
- [Sleep](/configuration/sleep) for signed sleep and wake commands
- [HTTP API](/configuration/http) for protecting the local API with a bearer token
