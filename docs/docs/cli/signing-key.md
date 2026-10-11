---
title: muti-metroo signing-key
sidebar_label: signing-key
sidebar_position: 10
description: Generate the Ed25519 signing keypair that authorizes privileged operations, and derive its public key.
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-inspecting.png" alt="Mole managing signing keys" style={{maxWidth: '180px'}} />
</div>

# muti-metroo signing-key

Generates and inspects the Ed25519 signing keypair. Privileged operations (remote shell, file transfer, dynamic configuration changes, and mesh-wide sleep and wake) are signed with the private key on the operator's agent and verified with the public key on the agent that carries them out. The agent that the CLI talks to must hold the private key even when the change targets that same agent. These commands themselves need no running agent.

Global flags (`--token`, `--yes`, `--no-color`) are described in [CLI overview](overview.md#global-flags).

The public key is 32 bytes (64 hexadecimal characters) and goes in `management.signing_public_key` on every agent. The private key is 64 bytes (128 hexadecimal characters) and goes in `management.signing_private_key` only on the operator agents that issue privileged commands. This keypair is separate from the X25519 topology encryption keypair; see [muti-metroo management-key](management-key.md).

## signing-key generate

```bash
muti-metroo signing-key generate [flags]
```

Generates a new keypair. Without `--out`, both keys and ready-to-paste configuration snippets are printed to stdout, which leaves the private key in the terminal scrollback.

With `--out <dir>`, the keys are written to `<dir>/signing-public.key` (mode `0644`) and `<dir>/signing-private.key` (mode `0600`), each as a single hex line, and only the public key is printed. The directory is created with mode `0700` if it does not exist. The command refuses to replace existing key files unless `--force` is given.

### Flags

| Flag | Default | Description |
| --- | --- | --- |
| `--out` | none | Directory to write the key files to. The private key is not printed. |
| `--force` | `false` | Overwrite existing key files in the `--out` directory. |

### Examples

```bash
muti-metroo signing-key generate --out ./keys
```

```text
=== Signing Keypair Generated ===
Public Key:   keys/signing-public.key (mode 0644)
Private Key:  keys/signing-private.key (mode 0600)

Public Key (also shown for convenience):
  4efb61edf79ea6a46014e76988c88a27e32ed8eb22cf8d4b76c9fe662986cd68

The private key is not echoed to stdout. Distribute it carefully.
```

Without `--out`, the output has the same layout as [`management-key generate`](management-key.md#management-key-generate), with `signing_public_key` and `signing_private_key` in the configuration snippets.

## signing-key public

```bash
muti-metroo signing-key public [flags]
```

Derives the public key from a private key, for example to recover a lost public key or to check that a public and private key belong together.

Without flags, the command prompts for the private key with input hidden; the prompt requires a terminal. With `--stdin`, it reads the private key from standard input, which accepts the file written by `generate --out`. Surrounding whitespace is ignored. The private key must decode to exactly 64 bytes.

A hidden `--private <hex>` flag also accepts the key on the command line. It prints a warning to stderr, because the value is recorded in shell history and visible in the process list.

### Flags

| Flag | Default | Description |
| --- | --- | --- |
| `--stdin` | `false` | Read the private key from standard input instead of prompting. |

### Examples

```bash
muti-metroo signing-key public --stdin < ./keys/signing-private.key
```

```text
Public Key:
  4efb61edf79ea6a46014e76988c88a27e32ed8eb22cf8d4b76c9fe662986cd68
```

## Configuration

Every agent carries the public key:

```yaml
management:
  signing_public_key: "4efb61edf79ea6a46014e76988c88a27e32ed8eb22cf8d4b76c9fe662986cd68"
```

Operator agents also carry the private key. Keep it out of the configuration file itself by reading it from the environment:

```yaml
management:
  signing_public_key: "4efb61edf79ea6a46014e76988c88a27e32ed8eb22cf8d4b76c9fe662986cd68"
  signing_private_key: "${MUTI_METROO_SIGNING_PRIVATE_KEY}"
```

```bash
export MUTI_METROO_SIGNING_PRIVATE_KEY="$(cat ./keys/signing-private.key)"
```

The configuration validator rejects `signing_private_key` without `signing_public_key`.

## Agents without a signing public key

What an agent does without `signing_public_key` depends on the operation:

| Operation | Agent without `signing_public_key` |
| --- | --- |
| Shell, file transfer, dynamic configuration changes | Rejected. No operator can authorize them. |
| Sleep and wake | Accepted unsigned, subject to the timestamp check below. |

Deploy the public key to every agent so that sleep and wake also require a signature.

Signed requests carry a timestamp. Agents reject requests whose timestamp differs from their own clock by more than 5 minutes, which limits replay of captured commands, so keep agent clocks synchronized.

## Troubleshooting

When privileged operations are rejected:

- Check that the target agent's `signing_public_key` matches the public key derived from the operator's private key with `signing-key public`.
- Check that the agent issuing the command has `signing_private_key` set.
- Check that the clocks of both agents are within 5 minutes of each other.
- Read the target agent's log for the verification error.

## See Also

- [Management configuration](../configuration/management.md): the `management` section and the signing keys
- [Authentication](../security/authentication.md): how signed operations are authorized across the mesh
- [muti-metroo management-key](management-key.md): the separate keypair for topology encryption
- [Sleep mode](../features/sleep-mode.md): mesh-wide sleep and wake, which the signing key protects
- [Environment variables](../configuration/environment-variables.md): substitute secrets into the configuration
