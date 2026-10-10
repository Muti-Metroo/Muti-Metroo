---
title: End-to-End Encryption
sidebar_position: 2
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-inspecting.png" alt="Mole with encryption" style={{maxWidth: '180px'}} />
</div>

# End-to-End Encryption

Even if a transit node is compromised or the network is monitored, your traffic stays private. Only the ingress and exit can read your data - everything in between sees encrypted bytes.

**This protects you when:**

- Traffic passes through untrusted networks
- A transit agent you don't control is in the path
- Someone captures packets between agents

## What Gets Encrypted

| Data                                    | Encrypted? | Why                          |
| --------------------------------------- | ---------- | ---------------------------- |
| Your application data (HTTP, SSH, etc.) | Yes        | Protected from transit nodes |
| Destination address/port                | No         | Needed for routing           |

## Security Properties

| What You Get        | What It Means                                               |
| ------------------- | ----------------------------------------------------------- |
| **Confidentiality** | Transit nodes can't read your traffic                       |
| **Integrity**       | If someone modifies data, it's detected and rejected        |
| **Forward Secrecy** | Even if keys are later compromised, past traffic stays safe |

## No Configuration Required

End-to-end encryption is enabled automatically. There is no configuration to set up - it just works.

### Key Generation

Each agent has an X25519 keypair for E2E encryption. Keys can be stored in files or in the config.

#### File-Based Keys (Default)

By default, keys are generated on first start and stored in `data_dir`:

| File                       | Purpose                           | Permissions           |
| -------------------------- | --------------------------------- | --------------------- |
| `{data_dir}/agent_key`     | Private key (never shared)        | 0600 (owner only)     |
| `{data_dir}/agent_key.pub` | Public key (distributed to peers) | 0644 (world readable) |

The keypair is persistent - once generated, it's reused on subsequent starts.

#### Config-Based Keys

For single-file deployments, keys can be specified directly in config:

```yaml
agent:
  id: "a1b2c3d4e5f6789012345678901234ab"
  private_key: "48bbea6c0c9be254bde983c92c8a53db759f27e51a6ae77fd9cca81895a5d57c"
  # public_key is optional - derived automatically from private_key
```

When `private_key` is set in config:

- Keys are loaded from config instead of data_dir files
- The `data_dir` field becomes optional
- Enables true single-file deployment (no external files needed)

Config-based keys take precedence over file-based keys.

The public key is automatically distributed to other agents via NodeInfo advertisements, so peers can encrypt data destined for this agent.

### Stream Encryption Flow

When a stream is opened:

1. Ingress and exit agents exchange ephemeral public keys
2. Both derive a shared secret using X25519 ECDH
3. All stream data is encrypted with ChaCha20-Poly1305
4. Transit agents forward encrypted data unchanged

## Performance Impact

| Metric    | Impact                          |
| --------- | ------------------------------- |
| CPU       | Minimal (~5-10% for encryption) |
| Latency   | Negligible                      |
| Bandwidth | +28 bytes per frame             |

ChaCha20-Poly1305 is highly optimized and runs at several GB/s on modern CPUs.

## E2E vs Management Encryption

Muti Metroo uses two separate encryption systems:

| Feature              | E2E Encryption               | Management Encryption                     |
| -------------------- | ---------------------------- | ----------------------------------------- |
| **Purpose**          | Protect stream payload data  | Protect mesh topology metadata            |
| **What's encrypted** | Application data in streams  | NodeInfo (hostnames, IPs, OS)             |
| **Key type**         | Per-agent persistent keypair | Shared across all agents                  |
| **Automatic**        | Yes, always on               | No, requires configuration                |
| **Algorithm**        | X25519 + ChaCha20-Poly1305   | X25519 + ChaCha20-Poly1305 (sealed boxes) |

Both systems use the same cryptographic primitives but serve different purposes. E2E encryption protects your traffic; management encryption protects your infrastructure topology.

See [management-key command](/cli/management-key) for management encryption details.

## What E2E Protects (and Doesn't)

### You're Protected Against

- Someone monitoring traffic at a transit node
- A compromised transit agent trying to read your data
- Replay attacks (sending captured traffic again)
- Traffic modification (changing data in transit)

### You're NOT Protected Against

- Compromised ingress or exit agent - they see your data (secure your endpoints!)
- Connection metadata - timing and volume patterns remain visible
- Destination visibility - routing requires knowing where traffic goes

## Troubleshooting

### Decryption Failures

If streams fail with decryption errors:

1. **Version mismatch**: Ensure all agents are running the same version
2. **Corrupted frames**: Check network reliability
3. **Clock skew**: Verify system time is synchronized

### Key Issues

For file-based keys:

```bash
# Verify keypair exists
ls -la {data_dir}/agent_key*

# Regenerate if corrupted (will change agent identity)
rm {data_dir}/agent_key*
muti-metroo init -d {data_dir}
```

For config-based keys:

```bash
# Generate new keys
muti-metroo init -d /tmp/newkeys

# Copy the values to your config
cat /tmp/newkeys/agent_key       # -> agent.private_key
cat /tmp/newkeys/agent_id        # -> agent.id
```

## Next Steps

- [TLS/mTLS Configuration](/security/tls-mtls) - Transport-layer security between peers
- [Authentication](/security/authentication) - SOCKS5 and shell authentication
