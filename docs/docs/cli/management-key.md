---
title: muti-metroo management-key
sidebar_label: management-key
sidebar_position: 9
description: Generate the X25519 management keypair that encrypts mesh topology data, and derive its public key.
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-inspecting.png" alt="Mole managing keys" style={{maxWidth: '180px'}} />
</div>

# muti-metroo management-key

Generates and inspects the X25519 management keypair. When agents have the management public key in `management.public_key`, they encrypt node information (hostnames, IP addresses, OS details) before flooding it through the mesh, and withhold route paths from their HTTP API. Route paths still travel unencrypted, because transit agents need them to forward streams. Only agents that also have `management.private_key` can decrypt and display the topology. No running agent is needed.

Global flags (`--token`, `--yes`, `--no-color`) are described in [CLI overview](overview.md#global-flags).

Both keys are 32 bytes, written as 64 hexadecimal characters. Put the public key on every agent and the private key only on the operator agents that need to see the topology. The management keypair is separate from the Ed25519 signing keypair that authorizes privileged operations; see [muti-metroo signing-key](signing-key.md).

## management-key generate

```bash
muti-metroo management-key generate [flags]
```

Generates a new keypair. Without `--out`, both keys and ready-to-paste configuration snippets are printed to stdout, which leaves the private key in the terminal scrollback.

With `--out <dir>`, the keys are written to `<dir>/management-public.key` (mode `0644`) and `<dir>/management-private.key` (mode `0600`), each as a single hex line, and only the public key is printed. The directory is created with mode `0700` if it does not exist. The command refuses to replace existing key files unless `--force` is given.

### Flags

| Flag | Default | Description |
| --- | --- | --- |
| `--out` | none | Directory to write the key files to. The private key is not printed. |
| `--force` | `false` | Overwrite existing key files in the `--out` directory. |

### Examples

```bash
muti-metroo management-key generate
```

```text
Tip: --out <dir> writes keys to disk with safe permissions and avoids
     leaking the private key into terminal scrollback.

=== Management Keypair Generated ===

Public Key (add to ALL agent configs):
  9e5dcaa28cbc418e8e063ddd4c2920b8791ddf3111129d50bde2725be7c8fc17

Private Key (add ONLY to operator configs - KEEP SECRET!):
  88f462338e9fb8970e782d8cd23d935a3f0769a1c48e777f7d68bd427f28e36a

Config snippet for field agents:
  management:
    public_key: "9e5dcaa28cbc418e8e063ddd4c2920b8791ddf3111129d50bde2725be7c8fc17"

Config snippet for operator nodes:
  management:
    public_key: "9e5dcaa28cbc418e8e063ddd4c2920b8791ddf3111129d50bde2725be7c8fc17"
    private_key: "88f462338e9fb8970e782d8cd23d935a3f0769a1c48e777f7d68bd427f28e36a"
```

Writing the keys to files:

```bash
muti-metroo management-key generate --out ./keys
```

```text
=== Management Keypair Generated ===
Public Key:   keys/management-public.key (mode 0644)
Private Key:  keys/management-private.key (mode 0600)

Public Key (also shown for convenience):
  d6f5966b3291a457850a943988dbf3930697221b0e1bb1394261a94e3ff3de26

The private key is not echoed to stdout. Distribute it carefully.
```

## management-key public

```bash
muti-metroo management-key public [flags]
```

Derives the public key from a private key, for example to recover a lost public key or to check that a public and private key belong together.

Without flags, the command prompts for the private key with input hidden; the prompt requires a terminal. With `--stdin`, it reads the private key from standard input, which accepts the file written by `generate --out`. Surrounding whitespace is ignored. The private key must decode to exactly 32 bytes.

A hidden `--private <hex>` flag also accepts the key on the command line. It prints a warning to stderr, because the value is recorded in shell history and visible in the process list.

### Flags

| Flag | Default | Description |
| --- | --- | --- |
| `--stdin` | `false` | Read the private key from standard input instead of prompting. |

### Examples

```bash
muti-metroo management-key public --stdin < ./keys/management-private.key
```

```text
Public Key:
  d6f5966b3291a457850a943988dbf3930697221b0e1bb1394261a94e3ff3de26
```

## Configuration

Paste the keys into the `management` section, as in the snippets that `generate` prints: field agents carry only `public_key`, operator agents carry both `public_key` and `private_key`. The configuration validator rejects `private_key` without `public_key`. Agent IDs and advertised route prefixes stay in plaintext because routing needs them. An agent that has the public key but not the private key also limits its remote agent queries, mesh tests, and route traces to itself. The full behavior is described on the [management configuration](../configuration/management.md) page.

## See Also

- [Management configuration](../configuration/management.md): the `management` section and what each key enables
- [muti-metroo signing-key](signing-key.md): the separate keypair that authorizes privileged operations
- [Security overview](../security/overview.md): how topology encryption fits into the security model
- [muti-metroo config](config.md): validate a configuration after adding the keys
