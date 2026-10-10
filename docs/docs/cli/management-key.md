---
title: management-key
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-inspecting.png" alt="Mole managing keys" style={{maxWidth: '180px'}} />
</div>

# management-key

Protect your mesh topology from remote agents. Generate encryption keys that let management nodes see which systems are in the mesh while remote agents only see opaque IDs.

**What this protects:** In multi-tenant or sensitive environments, remote agents only see encrypted topology data - no hostnames, no IP addresses, no OS details. They see only random-looking agent IDs.

**Quick setup:**
```bash
# Generate a keypair
muti-metroo management-key generate

# Add public key to ALL agents (they can encrypt but not decrypt)
# Add private key ONLY to management nodes (they can see topology)
```

## Subcommands

### generate

Generate a new management keypair:

```bash
muti-metroo management-key generate
```

**Output:**
```
=== Management Keypair Generated ===

Public Key (add to ALL agent configs):
  a1b2c3d4e5f6789012345678901234567890123456789012345678901234abcd

Private Key (add ONLY to management node configs - KEEP SECRET!):
  e5f6a7b8c9d012345678901234567890123456789012345678901234567890ef

Configuration snippets:

For ALL agents (remote agents and management nodes):
  management:
    public_key: "a1b2c3d4e5f6789012345678901234567890123456789012345678901234abcd"

For MANAGEMENT nodes only (add private key):
  management:
    public_key: "a1b2c3d4e5f6789012345678901234567890123456789012345678901234abcd"
    private_key: "e5f6a7b8c9d012345678901234567890123456789012345678901234567890ef"
```

### public

Derive public key from a private key:

```bash
muti-metroo management-key public --private <private-key-hex>
```

**Flags:**
- `--private`: Private key in hex format (64 characters)

:::tip Interactive Mode
If the `--private` flag is not provided, the CLI prompts interactively for the private key (input is hidden for security).
:::

**Example:**
```bash
muti-metroo management-key public --private e5f6a7b8c9d012345678901234567890123456789012345678901234567890ef
```

**Output:**
```
Public Key:
  a1b2c3d4e5f6789012345678901234567890123456789012345678901234abcd
```

## Usage Guide

### Initial Setup

1. **Generate keypair** on a management machine:
   ```bash
   muti-metroo management-key generate
   ```

2. **Save the private key** securely (password manager, encrypted file)

3. **Distribute public key** to all agents via config

4. **Add private key** only to management nodes that need topology visibility

### Configuration

**Remote agents** (encrypt only - cannot view topology):
```yaml
management:
  public_key: "a1b2c3d4..."
```

**Management nodes** (can encrypt and decrypt):
```yaml
management:
  public_key: "a1b2c3d4..."
  private_key: "e5f6a7b8..."
```

### Security Model

| Role | Has Public Key | Has Private Key | Can View Topology |
|------|---------------|-----------------|-------------------|
| Remote Agent | Yes | No | No |
| Management Node | Yes | Yes | Yes |

### What Gets Protected

- **Encrypted**: NodeInfo (hostname, OS, IP addresses, peer lists)
- **Plaintext**: Agent IDs, route CIDRs (required for routing)

Without the private key, agents see only opaque 128-bit agent IDs instead of meaningful system identification.

## Related

- [Configuration Overview](/configuration/overview)
