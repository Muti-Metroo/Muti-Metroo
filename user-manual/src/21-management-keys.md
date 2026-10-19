# Management Key Encryption

Management key encryption provides cryptographic compartmentalization of mesh topology information. When enabled, sensitive node information (hostnames, OS, IPs, peer lists) is encrypted so only operators with the private key can view topology details.

## Security Model

### Protected Against

- Unauthorized access to agent API - sees encrypted blobs only
- Memory dump of remote agent - no private key present
- Passive network observers - NodeInfo encrypted
- Isolated agent - cannot expose other agents' details

### Not Protected Against

- Connection metadata (timing/volume) remains visible
- Agent ID correlation (IDs remain plaintext for routing)
- Compromise of management machine with private key

## Key Generation

Generate a management keypair:

```bash
muti-metroo management-key generate
```

Output (abridged):

```
Tip: --out <dir> writes keys to disk with safe permissions and avoids
     leaking the private key into terminal scrollback.

=== Management Keypair Generated ===

Public Key (add to ALL agent configs):
  a1b2c3d4e5f6789012345678901234567890123456789012345678901234abcd

Private Key (add ONLY to operator configs - KEEP SECRET!):
  e5f6a7b8c9d012345678901234567890123456789012345678901234567890ef

Config snippet for field agents:
  management:
    public_key: "a1b2c3d4..."

Config snippet for operator nodes:
  management:
    public_key: "a1b2c3d4..."
    private_key: "e5f6a7b8..."
```

For production rollouts use `--out <dir>` so the private key is written
to disk with mode 0600 instead of echoed to terminal scrollback:

```bash
muti-metroo management-key generate --out ./secrets/
# Writes ./secrets/management-public.key (0644)
#    and ./secrets/management-private.key (0600)
```

## Deployment Configuration

### Remote Agents (Encrypt Only)

Remote agents get the **public key only** - they can encrypt but not decrypt:

```yaml
management:
  public_key: "a1b2c3d4e5f6789012345678901234567890123456789012345678901234abcd"
  # NO private_key - remote agents cannot decrypt
```

### Operator Nodes (Can Decrypt)

Operator stations get **both keys** - they can decrypt the topology:

```yaml
management:
  public_key: "a1b2c3d4e5f6789012345678901234567890123456789012345678901234abcd"
  private_key: "e5f6a7b8c9d012345678901234567890123456789012345678901234567890ef"
```

## What Gets Protected

| Data                | Encrypted | Plaintext | Reason                     |
| ------------------- | :-------: | :-------: | -------------------------- |
| Hostname, OS, IPs   |    Yes    |           | System identification      |
| Peer list           |    Yes    |           | Topology exposure          |
| Agent display name  |    Yes    |           | Operational naming         |
| Agent IDs           |           |    Yes    | Required for routing       |
| Route CIDRs/metrics |           |    Yes    | Required for routing       |
| Stream data         |    N/A    |    N/A    | Has its own E2E encryption |

## API Behavior

### Without Private Key (Remote Agent)

When accessing the dashboard API on a remote agent:

```bash
curl http://field-agent:8080/api/dashboard
```

Response:

```json
{
  "agent": { "display_name": "local-only", "is_local": true },
  "peers": [],
  "routes": []
}
```

```bash
curl http://field-agent:8080/api/nodes
```

Response:

```json
{
  "nodes": [{ "is_local": true }]
}
```

### With Private Key (Operator)

When accessing from operator station:

```bash
curl http://operator:8080/api/dashboard
```

Response includes full decrypted topology:

```json
{
  "agent": { "display_name": "Gateway", "hostname": "gw-01", ... },
  "peers": [
    { "agent_id": "...", "display_name": "Field-1", ... }
  ],
  "routes": [...]
}
```

### Restricted Endpoints

When `public_key` is configured but `private_key` is not, the agent forwards encrypted topology blobs through the mesh but refuses to relay queries it cannot decrypt. The following endpoints are restricted to local-only data on such agents and reject remote fan-out (HTTP 403):

- `/api/topology`, `/api/dashboard`, `/api/nodes`: already shown above
- `/agents/{id}` and every `/agents/{id}/...` sub-path (peers, routes, shell, file, probe, etc.)
- `/api/mesh-test`: runs only against local peers
- `/route/trace` and `/agents/{id}/route/trace`: traces only the local routing table

This protects the integrity of the compartmentalization: an operator who cannot decrypt topology cannot use a field agent as a relay to learn about it either. To enable full mesh visibility on a node, give it both keys.

## Deployment Example

### Step 1: Generate Keys

On a secure machine:

```bash
muti-metroo management-key generate > keys.txt
cat keys.txt
```

### Step 2: Deploy Remote Agents

Copy only the public key to remote agent configs:

```yaml
# field-agent-config.yaml
agent:
  display_name: "Remote Agent 1"

management:
  public_key: "a1b2c3d4..."
```

### Step 3: Configure Operator Station

Include both keys on operator machines:

```yaml
# operator-config.yaml
agent:
  display_name: "Operator Console"

management:
  public_key: "a1b2c3d4..."
  private_key: "e5f6a7b8..."
```

### Step 4: Verify

On remote agent (should show limited info):

```bash
curl http://localhost:8080/api/nodes | jq '.nodes | length'
# Output: 1 (only local node)
```

On operator (should show full topology):

```bash
curl http://localhost:8080/api/nodes | jq '.nodes | length'
# Output: 5 (all nodes visible)
```

## Key Management Best Practices

1. **Generate keys offline** on a secure machine
2. **Never store private keys** on remote agents
3. **Limit operator access** to authorized personnel only
4. **Rotate keys periodically** for long-running operations
5. **Destroy keys** after operation concludes

## Using Environment Variables

For additional security, pass keys via environment:

```yaml
management:
  public_key: "${MGMT_PUBKEY}"
  private_key: "${MGMT_PRIVKEY}"
```

```bash
export MGMT_PUBKEY="a1b2c3d4..."
export MGMT_PRIVKEY="e5f6a7b8..."
muti-metroo run -c config.yaml
```

## Command Signing Keys

Separate from topology encryption, signing keys authenticate **every privileged operation** in the mesh using Ed25519 signatures. The same key authorizes:

- **Management commands** (`muti-metroo shell ...`)
- **File transfer** (`muti-metroo upload`, `muti-metroo download`, file browse)
- **Dynamic config mutations**: routes, peers, forward listeners, forward endpoints, transport listeners, hosts, and display name (every `add`, `remove`, and `set_comment` action exposed by `muti-metroo route`, `muti-metroo peer`, `muti-metroo forward`, `muti-metroo listener`, `muti-metroo host`, and `muti-metroo display-name`)
- **Sleep / wake** (`muti-metroo sleep`, `muti-metroo wake`)
- **Agent shutdown** (`muti-metroo stop`)

There is no separate per-feature password for any of these operations. Agents configured with the public key reject unsigned, expired, or incorrectly signed requests; agents without the private key cannot issue them; agents without either cannot use these features at all. (Sleep and wake have a narrow backward-compatibility carve-out; see [Backward Compatibility](#backward-compatibility) below.)

### How a Request Is Signed

1. The CLI sends the request to the gateway agent's HTTP API. The CLI itself holds no key.
2. The gateway agent signs an envelope containing its own agent ID, the target agent ID, a timestamp, the operation type, and a hash of the request body: the shell command and its arguments, the file path and transfer options, or the configuration change.
3. The target agent checks that the envelope names it as the target, that the timestamp is within five minutes of its own clock, and that the signature verifies against its `signing_public_key` over the request it actually received.

A signed envelope is valid for that five-minute window and has no one-time nonce. Because it covers the operation type and the request body, an envelope captured from one request cannot authorize a different one: a shell command cannot be swapped for another, a file delete cannot be pointed at another path, and `add X` cannot be replayed as `remove Y`. Keep agent clocks synchronized; a skew of more than five minutes causes every signed operation to fail.

> **Mixed versions.** Agents before 4.12.0 signed shell and file operations without the request body. A gateway and a target on different sides of that change reject each other's shell and file requests with `invalid_credentials`; upgrade both. Configuration changes are not affected.

> **The gateway signs for its callers.** An operator agent signs every privileged request that reaches its HTTP API. Anyone who can call that API can therefore run commands and transfer files on every agent in the mesh. Bind operator agents' HTTP API to `127.0.0.1` and set `http.token_hash`.

### Purpose

Without signing keys, anyone who can reach an agent's HTTP API can trigger privileged operations. With signing keys configured, only operators with the private key can issue valid commands. The local API on a node without a signing private key also refuses to forward mutations to remote agents (HTTP 403), so an operator cannot accidentally issue an unsigned mutation through their own dashboard.

### Key Generation

```bash
muti-metroo signing-key generate
```

Output (abridged):

```
Tip: --out <dir> writes keys to disk with safe permissions and avoids
     leaking the private key into terminal scrollback.

=== Signing Keypair Generated ===

Public Key (add to ALL agent configs):
  a1b2c3d4e5f6789012345678901234567890123456789012345678901234abcd

Private Key (add ONLY to operator configs - KEEP SECRET!):
  e5f6a7b8c9d012345678901234567890...   (128 hex characters / 64 bytes)

Config snippet for field agents:
  management:
    signing_public_key: "a1b2c3d4..."

Config snippet for operator nodes:
  management:
    signing_public_key: "a1b2c3d4..."
    signing_private_key: "e5f6a7b8..."
```

For production use, write keys directly to disk with `--out <dir>` so the
private key is created with mode 0600 instead of being echoed to
terminal scrollback:

```bash
muti-metroo signing-key generate --out ./secrets/
# Writes ./secrets/signing-public.key (0644)
#    and ./secrets/signing-private.key (0600)
```

### Deployment Configuration

**All Agents** (verify signatures):

```yaml
management:
  signing_public_key: "a1b2c3d4..."
```

**Operators Only** (sign commands):

```yaml
management:
  signing_public_key: "a1b2c3d4..."
  signing_private_key: "e5f6a7b8..."
```

### Key Differences

| Feature          | Management Keys             | Signing Keys                                |
| ---------------- | --------------------------- | ------------------------------------------- |
| Algorithm        | X25519 (encryption)         | Ed25519 (signatures)                        |
| Purpose          | Encrypt topology            | Authenticate commands                       |
| Private key size | 64 hex chars                | 128 hex chars                               |
| Config keys      | `public_key`, `private_key` | `signing_public_key`, `signing_private_key` |

### Combined Usage

You can use both topology encryption and command signing:

```yaml
management:
  # Topology encryption (X25519)
  public_key: "a1b2c3d4..."
  private_key: "e5f6a7b8..." # Only on management nodes

  # Command signing (Ed25519)
  signing_public_key: "1234abcd..."
  signing_private_key: "5678efgh..." # Only on operators
```

### Backward Compatibility

Agents without `signing_public_key` configured fall through to legacy unsigned acceptance for sleep/wake. Every other privileged operation (management commands, file transfer, dynamic mutations) requires the public key on the target and rejects requests without it. For full protection, deploy the public key to every agent in your mesh.

### Capability Reporting

The agent's `/healthz` endpoint reports two boolean flags so dashboards can light up management actions only on agents that support them:

- `can_sign_management`: this agent has a signing private key, so it can issue signed commands.
- `accepts_signed_management`: this agent has a signing public key, so it can verify and execute signed commands.

Muti Metroo Manager reads these flags and hides management actions on agents where they would always 403.
