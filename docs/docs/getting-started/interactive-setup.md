---
title: Interactive Setup
sidebar_position: 2
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-idea.png" alt="Mole with bright idea" style={{maxWidth: '180px'}} />
</div>

# Interactive Setup Wizard

Get a working tunnel in under 5 minutes. The wizard asks you a few questions and generates everything you need - certificates, configuration, and agent identity.

By the end, you will have a SOCKS5 proxy ready to tunnel traffic through your mesh.

## Running the Wizard

```bash
muti-metroo setup
```

:::tip Quick Path - 4 Essential Steps
For a basic working setup, most steps can use defaults. Focus on these:

1. **Basic Setup** - accept defaults or set data directory
2. **Agent Role** - select Ingress + Exit for a standalone proxy
3. **Inbound Listener** - accept inbound connections, then accept defaults (QUIC on :4433)
4. **SOCKS5 Proxy** - accept defaults (localhost:1080)

Press Enter to accept defaults on all other steps.
:::

## What the Wizard Configures

The wizard has 11 numbered steps, followed by configuration delivery and optional service installation. Many steps are optional or have sensible defaults.

### 1. Basic Setup

- **Config file path**: Where to save the configuration (default: `./config.yaml`)
- **Data directory**: Where agent state is stored (default: `./data`)
- **Display name**: Human-readable name for the agent (optional)

### 2. Agent Role

Choose what this agent will do:

| Role        | What It Does                                                   |
| ----------- | -------------------------------------------------------------- |
| **Ingress** | Accept your SOCKS5 connections (curl, SSH, browser)            |
| **Transit** | Relay traffic to other agents in the mesh                      |
| **Exit**    | Open connections to destinations (internet, internal networks) |

Most setups need at least one ingress (where you connect) and one exit (where traffic goes). A single agent can do all three.

### 3. Inbound Listener (Optional)

- **Accept inbound peer connections?**: Answer No for SOCKS5-only clients, laptops behind NAT, or any agent that only dials out to other peers. Declining skips the transport and address prompts below and emits `listeners: []`. Outbound peer dials are still configured in the next step; per-peer transport then defaults to QUIC.

When accepting inbound connections:

- **Transport type**: QUIC (recommended), HTTP/2, or WebSocket
- **Listen address**: IP and port to listen on
- **HTTP path**: Path for HTTP/2 and WebSocket transports (default: `/mesh`)
- **Reverse proxy**: For HTTP/2 and WebSocket, option to accept plain connections when TLS is terminated by a reverse proxy (e.g., Nginx, Caddy, Cloudflare)

### 4. TLS Configuration (Optional)

All traffic is end-to-end encrypted (X25519 + ChaCha20-Poly1305). Transport TLS adds certificate-based peer verification on top of E2E encryption.

- **Self-signed certificates (default)**: Auto-generate at startup - no configuration needed
- **Strict TLS with CA verification (advanced)**: For PKI-based certificate verification (includes mTLS option)

### 5. Peer Connections

Optionally connect to existing mesh agents:

- Enter peer address (host:port)
- Pin to specific agent ID (confirmation prompt; defaults to `auto` if declined)
- Transport defaults to listener's transport with option to change
- Enable strict TLS verification (requires CA-signed certs; disabled by default)
- HTTP path prompt for HTTP/2 and WebSocket peers
- Connectivity test with options: "Continue anyway", "Retry the connection test", "Re-enter peer configuration", "Skip this peer"

### 6. SOCKS5 Configuration (Ingress Role)

If ingress role is selected:

- **Listen address**: Where to accept SOCKS5 connections
- **Authentication**: Enable/disable, add username and password

### 7. Exit Configuration (Exit Role)

If exit role is selected:

- **CIDR routes**: Networks to advertise (e.g., `0.0.0.0/0`, `::/0`). If no routes entered, defaults to all traffic (`0.0.0.0/0` and `::/0`)
- **Domain routes**: Domain patterns for domain-based routing (optional)

### 8. Monitoring & Logging

- **Log level**: Debug, Info (default), Warning, or Error
- **HTTP management API**: Enable health checks, dashboard, and CLI remote commands
- **HTTP API authentication**: Optional bearer token to protect non-health endpoints

### 9. Management Commands

Skipped for transit-only agents.

- **Enable management commands**: Allow remote command execution (disabled by default)
- **Command whitelist**: "Allow all commands" (default), "Custom whitelist", or "No commands (lockdown)"

Management commands are authorized by the mesh management signing key (configured in the Management Key step), not a per-feature password.

### 10. File Transfer

Skipped for transit-only agents.

- **Enable file transfer**: Allow file upload/download (disabled by default)
- **Max file size**: Limit in MB (default: 500 MB, 0 = unlimited)
- **Path restrictions**: Allow all paths or restrict to specific directories

File transfer is authorized by the mesh management signing key (configured in the Management Key step), not a per-feature password.

### 11. Management & Signing Keys

This step configures two independent keypairs that control mesh management:

- **Management key (X25519)**: encrypts topology data so only operators can view the network structure.
- **Signing key (Ed25519)**: authorizes shell, file transfer, sleep/wake, and dynamic config changes. An agent without the signing public key cannot accept these privileged operations.

Options:

- **Generate new keypairs**: Create both keypairs at once. You are then asked whether this is an operator/management node. Operator nodes keep the private and public hex of both keys; field agents keep only the two public keys.
- **Enter existing keys** (default): Paste both private keys (operator/management node) or both public keys (field agent).
- **Skip**: No encryption or signing keys.

The public keys go into every agent config; the private keys belong only in operator configs.

### Configuration Delivery

Choose how to deploy the configuration:

| Option              | Description                                 |
| ------------------- | ------------------------------------------- |
| **Save to file**    | Traditional `config.yaml` file (default)    |
| **Embed in binary** | Single-file deployment with config baked in |

For both options, you specify a **service name** used when installing as a system service (default: `muti-metroo`). The service name must contain only alphanumeric characters, hyphens, and underscores.

When embedding:

- **Output path**: Where to save the embedded binary

:::note
When editing a target binary (`setup -c /path/to/binary`), the delivery step is skipped -- config is always embedded back into the target binary.
:::

### Service Installation (Optional)

If a service with the same name is already installed, the wizard offers to update the binary and restart the service.

For fresh installations:

- **Linux (root)**: systemd service
- **Linux (non-root)**: User service via cron `@reboot` + nohup
- **macOS**: launchd service (requires root)
- **Windows (Administrator)**: Windows Service
- **Windows (non-admin)**: User service via Registry Run key

For Windows without Administrator access, use the CLI after the wizard:

```powershell
muti-metroo service install --user --dll path\to\muti-metroo.dll -c path\to\config.yaml
```

This uses the Registry Run key for automatic startup at user logon. See [DLL Mode](/deployment/dll-mode) for details.

## Example Wizard Session

```
$ muti-metroo setup

================================================================================
                        Muti Metroo Setup Wizard
================================================================================
                    Userspace Mesh Networking Agent

Tip: You can re-run this wizard on an existing config to modify settings.

--------------------------------------------------------------------------------
Step 1/11: Basic Setup
--------------------------------------------------------------------------------
Configure the essential paths for your agent.

Config File Path [./config.yaml]:
Data Directory [./data]:
Display Name (press Enter to use Agent ID) []: My Gateway Agent

--------------------------------------------------------------------------------
Step 2/11: Agent Role
--------------------------------------------------------------------------------
Select the roles this agent will perform.
You can select multiple roles.

  1. [ ] Ingress (SOCKS5 proxy entry point)
  2. [x] Transit (relay traffic between peers)
  3. [ ] Exit (connect to external networks)

Select Roles (comma-separated) [2]: 1,3

--------------------------------------------------------------------------------
Step 3/11: Inbound Listener (optional)
--------------------------------------------------------------------------------
Does this agent accept inbound peer connections?
Answer No for SOCKS5-only clients, laptops behind NAT, or any agent
that only dials out to other peers. Outbound peer dials are configured
in the next step regardless.

Accept inbound peer connections? [y/N]: y

Transport Protocol:

> 1. QUIC - Best performance, requires UDP port access
  2. HTTP/2 - Works through firewalls that only allow TCP/443
  3. WebSocket - Works through HTTP proxies and CDNs

Select [1]:
Listen Address [0.0.0.0:4433]:

--------------------------------------------------------------------------------
Step 4/11: TLS Configuration
--------------------------------------------------------------------------------
All traffic is end-to-end encrypted (X25519 + ChaCha20-Poly1305).
Transport TLS adds certificate-based peer verification on top of E2E encryption.

> 1. Self-signed certificates (Recommended)
  2. Strict TLS with CA verification (Advanced)

Certificate Setup [1]:

[OK] TLS certificates will be auto-generated at startup.
    E2E encryption provides security - TLS verification is optional.

--------------------------------------------------------------------------------
Step 5/11: Peer Connections
--------------------------------------------------------------------------------
Configure connections to other mesh agents.

Add peer connections? [y/N]: y

--------------------------------------------------------------------------------
Peer #1
--------------------------------------------------------------------------------

Peer Address (host:port) []: 192.168.1.50:4433
Pin to specific agent ID? [y/N]: n
Use same transport as listener (QUIC)? [Y/n]: y
Enable strict TLS verification? (requires CA-signed certs) [y/N]: n

[INFO] Testing connectivity to peer...
[INFO] Remote agent: Transit Node (abc123def456)
[INFO] Round-trip time: 5ms
[OK] Connected successfully!

Add another peer? [y/N]: n

--------------------------------------------------------------------------------
Step 6/11: SOCKS5 Proxy
--------------------------------------------------------------------------------
Configure the SOCKS5 ingress proxy.

Listen Address [127.0.0.1:1080]:
Enable authentication? [y/N]: n

--------------------------------------------------------------------------------
Step 7/11: Exit Node Configuration
--------------------------------------------------------------------------------
Configure this agent as an exit node.
It will allow traffic to specified networks.

Allowed Routes (CIDR) - one CIDR per line (e.g., 0.0.0.0/0 for all traffic):
Enter routes, one per line. Enter empty line to finish.
Route (or empty to finish) []: 10.0.0.0/8
Route (or empty to finish) []: 192.168.0.0/16
Route (or empty to finish) []:

--------------------------------------------------------------------------------
Domain Routes (Optional)
--------------------------------------------------------------------------------
Route traffic by domain name.
Examples: api.internal.corp, *.example.com

Enter domain patterns, one per line. Enter empty line to finish.
Domain (or empty to finish) []:

--------------------------------------------------------------------------------
Step 8/11: Monitoring & Logging
--------------------------------------------------------------------------------
Configure monitoring, logging, and the HTTP management API.

  1. Debug (verbose)
> 2. Info (recommended)
  3. Warning
  4. Error (quiet)

Log Level [2]:
Enable HTTP management API? (health checks, dashboard, CLI remote commands) [Y/n]: y
Enable HTTP API authentication? [y/N]: n

--------------------------------------------------------------------------------
Step 9/11: Management Commands
--------------------------------------------------------------------------------
Shell allows executing commands remotely on this agent.

Enable management commands? [y/N]: n

--------------------------------------------------------------------------------
Step 10/11: File Transfer
--------------------------------------------------------------------------------
File transfer allows uploading and downloading files to/from this agent.
Files are streamed directly through the mesh network.

Enable file transfer? [y/N]: n

--------------------------------------------------------------------------------
Step 11/11: Management & Signing Keys
--------------------------------------------------------------------------------
Two independent keypairs control mesh management.
  - Management key (X25519): encrypts topology so only operators can view it.
  - Signing key (Ed25519): authorizes shell, file transfer, sleep/wake, and
    dynamic config changes. Agents without the signing public key cannot
    accept these privileged operations.
Recommended for any non-trivial deployment.

  1. Generate new keypairs
> 2. Enter existing keys
  3. Skip

Key Setup [2]: 3

--------------------------------------------------------------------------------
Configuration Delivery
--------------------------------------------------------------------------------
Choose how to deploy the configuration.

> 1. Save to config file (traditional)
  2. Embed in binary (single-file deployment)

Delivery method [1]:

Service name [muti-metroo]:

--------------------------------------------------------------------------------
[OK] Setup Complete!
--------------------------------------------------------------------------------

  Display Name:    My Gateway Agent
  Agent ID:        abc123def456789012345678901234567
  E2E Public Key:  0123456789abcdef0123456789abcdef...
  Config file:     ./config.yaml
  Data dir:        ./data

  Listener:        quic://0.0.0.0:4433
  SOCKS5:          127.0.0.1:1080
  Exit routes:     [10.0.0.0/8 192.168.0.0/16]
  HTTP API:        http://:8080

  To start the agent:
    muti-metroo run -c ./config.yaml
```

## After the Wizard

Once the wizard completes:

1. **Configuration saved**: Your `config.yaml` is ready
2. **Agent initialized**: Identity stored in data directory
3. **TLS certificates**: Auto-generated at startup (or embedded in config if using strict mode)

### Start the Agent

If you did not start it during the wizard:

```bash
muti-metroo run -c ./config.yaml
```

### Verify Operation

```bash
# Check health
curl http://localhost:8080/health

# Query dashboard API
curl http://localhost:8080/api/dashboard | jq

# Test SOCKS5 (if exit is enabled)
curl -x socks5://localhost:1080 https://example.com
```

## Service Installation

When running the wizard with elevated privileges, you can install as a system service:

### Linux (as root)

```bash
sudo muti-metroo setup
# At the end, choose to install as service
```

The wizard will:

1. Create `/etc/muti-metroo/config.yaml`
2. Create systemd unit file
3. Enable and start the service

### Windows (as Administrator)

```powershell
# Run as Administrator
.\muti-metroo.exe setup
```

The wizard will:

1. Create configuration in `C:\ProgramData\muti-metroo\`
2. Register Windows Service
3. Start the service

## Re-running the Wizard

You can run the wizard again to modify configuration:

```bash
muti-metroo setup

# Existing config detected
Use existing configuration as base? [y]: y
```

The wizard will load existing values as defaults.

## Editing Embedded Configuration

To edit the configuration embedded in an existing binary:

```bash
# Edit embedded config in a deployed binary
muti-metroo setup -c /usr/local/bin/my-agent
```

The wizard will:

1. Detect the embedded configuration
2. Load existing values as defaults
3. Guide you through changes
4. Write updated configuration back to the binary

This is useful for updating deployed agents without redeploying:

```bash
# Stop the service
sudo systemctl stop my-agent

# Edit the embedded config
muti-metroo setup -c /usr/local/bin/my-agent

# Restart the service
sudo systemctl start my-agent
```

See [Embedded Configuration](/deployment/embedded-config) for more details.

## Next Steps

- [Your First Mesh](/getting-started/first-mesh) - Connect multiple agents
- [Configuration Reference](/configuration/overview) - All options explained
- [Deployment](/deployment/scenarios) - Production deployment patterns
