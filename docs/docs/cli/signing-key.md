---
title: signing-key
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-inspecting.png" alt="Mole managing signing keys" style={{maxWidth: '180px'}} />
</div>

# signing-key

Authenticate sleep/wake commands with Ed25519 signatures. Generate signing keypairs that let authorized operators control mesh hibernation while preventing unauthorized parties from putting your mesh to sleep.

**What this protects:** Without signing keys, anyone who can reach an agent's HTTP API or connect to the mesh can trigger sleep/wake commands. With signing keys, only operators with the private key can issue valid commands.

**Quick setup:**
```bash
# Generate a keypair
muti-metroo signing-key generate

# Add public key to ALL agents (they verify signatures)
# Add private key ONLY to operator nodes (they sign commands)
```

## Subcommands

### generate

Generate a new Ed25519 signing keypair:

```bash
muti-metroo signing-key generate
```

**Output:**
```
=== Signing Keypair Generated ===

Public Key (add to ALL agent configs):
  a1b2c3d4e5f6789012345678901234567890123456789012345678901234abcd

Private Key (add ONLY to operator configs - KEEP SECRET!):
  e5f6a7b8c9d012345678901234567890123456789012345678901234567890efe5f6a7b8c9d012345678901234567890123456789012345678901234567890ef

Configuration snippets:

For ALL agents (verify signatures):
  management:
    signing_public_key: "a1b2c3d4e5f6789012345678901234567890123456789012345678901234abcd"

For OPERATOR nodes only (sign commands):
  management:
    signing_public_key: "a1b2c3d4e5f6789012345678901234567890123456789012345678901234abcd"
    signing_private_key: "e5f6a7b8c9d012345678901234567890123456789012345678901234567890efe5f6a7b8c9d012345678901234567890123456789012345678901234567890ef"
```

### public

Derive public key from a private key:

```bash
# Interactive (input hidden)
muti-metroo signing-key public

# With --private flag
muti-metroo signing-key public --private "e5f6a7b8c9d0..."
```

:::note
Piping via stdin (e.g., `echo "..." | muti-metroo signing-key public`) does not work -- the interactive prompt requires a terminal. Use the `--private` flag instead.
:::

**Output:**
```
Public Key:
  a1b2c3d4e5f6789012345678901234567890123456789012345678901234abcd
```

## Usage Guide

### Initial Setup

1. **Generate keypair** on a secure machine:
   ```bash
   muti-metroo signing-key generate
   ```

2. **Save the private key** securely (password manager, encrypted file)

3. **Distribute public key** to all agents via config

4. **Add private key** only to operator nodes that need to issue sleep/wake commands

### Configuration

**All agents** (verify signatures - cannot issue commands):
```yaml
management:
  signing_public_key: "a1b2c3d4..."
```

**Operator nodes** (can sign and issue commands):
```yaml
management:
  signing_public_key: "a1b2c3d4..."
  signing_private_key: "e5f6a7b8..."
```

### Security Model

| Role | Has Public Key | Has Private Key | Can Issue Commands |
|------|---------------|-----------------|-------------------|
| Remote Agent | Yes | No | No |
| Operator Node | Yes | Yes | Yes |
| Agent without keys | No | No | Accepts all commands |

:::warning Backward Compatibility
Agents without a `signing_public_key` configured will accept all sleep/wake commands, signed or unsigned. For full protection, deploy the public key to ALL agents in your mesh.
:::

### Key Differences from Management Keys

| Feature | Management Keys (X25519) | Signing Keys (Ed25519) |
|---------|-------------------------|----------------------|
| Purpose | Encrypt topology data | Sign sleep/wake commands |
| Algorithm | X25519 (encryption) | Ed25519 (signatures) |
| Public key size | 32 bytes (64 hex chars) | 32 bytes (64 hex chars) |
| Private key size | 32 bytes (64 hex chars) | 64 bytes (128 hex chars) |
| Config key | `public_key` / `private_key` | `signing_public_key` / `signing_private_key` |

## How Signing Works

When an operator triggers sleep or wake:

1. The command includes a timestamp (nanosecond precision)
2. The operator's agent signs the command with the private key
3. The signed command floods to all connected agents
4. Each agent verifies the signature using the public key
5. Commands with invalid signatures are rejected

### Replay Protection

Commands include a Unix timestamp. Agents reject commands with timestamps more than 5 minutes from the current time, preventing replay attacks.

## Troubleshooting

### Command Rejected

If sleep/wake commands are being rejected:

- Verify all agents have the **same** `signing_public_key`
- Verify the operator node has the matching `signing_private_key`
- Check agent clocks are synchronized (within 5 minutes)
- Review agent logs for signature verification errors

### Mixed Deployment

If some agents accept commands and others don't:

- Agents without `signing_public_key` accept all commands
- Deploy public key to all agents for consistent behavior

## Related

- [Sleep Mode](/features/sleep-mode) - Sleep mode overview
- [Sleep Configuration](/configuration/sleep) - Configuration options
- [Management Key](/cli/management-key) - Topology encryption keys
- [Best Practices](/security/best-practices) - Security recommendations
