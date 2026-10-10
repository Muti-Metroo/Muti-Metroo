---
title: init
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-reading.png" alt="Mole initializing" style={{maxWidth: '180px'}} />
</div>

# muti-metroo init

Create a unique identity for an agent. This generates the AgentID that other agents use to identify and route traffic to this agent.

**Note:** You usually don't need to run this manually - `muti-metroo run` creates the identity automatically if it doesn't exist. Use this command when you need to generate the identity ahead of time (e.g., to configure peers before the agent runs).

## Usage

```bash
muti-metroo init -d <data-dir>
```

## Flags

- `-d, --data-dir <dir>`: Data directory path (default: `./data`)

## Examples

```bash
# Initialize in current directory
muti-metroo init -d ./data

# Initialize in /var/lib
muti-metroo init -d /var/lib/muti-metroo
```

## What It Does

1. Creates data directory if it doesn't exist
2. Generates a unique 128-bit AgentID
3. Generates an X25519 keypair for end-to-end encryption
4. Saves identity files to data directory:
   - `agent_id` - The 128-bit AgentID
   - `agent_key` - X25519 private key (for E2E encryption)
   - `agent_key.pub` - X25519 public key (advertised to peers)
5. Outputs the AgentID to stdout

## Output

When creating a new identity:
```
Agent initialized in ./data
Agent ID: abc123def456789012345678901234ab
```

When the identity already exists:
```
Agent already initialized in ./data
Agent ID: abc123def456789012345678901234ab
```
