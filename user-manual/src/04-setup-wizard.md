# Setup Wizard

The interactive setup wizard is the easiest way to configure Muti Metroo. It guides you through all configuration steps with sensible defaults.

## Running the Wizard

```bash
muti-metroo setup
```

## Wizard Steps

The wizard walks you through 11 numbered steps, followed by configuration delivery and optional service installation. Some steps are conditional based on your role selections.

### Step 1: Basic Setup

- **Config file path**: Where to save the configuration (default: `./config.yaml`)
- **Data directory**: Where agent state is stored (default: `./data`)
- **Display name**: Human-readable name shown in the dashboard API (optional)

### Step 2: Agent Role

Choose one or more roles for your agent:

| Role        | Description                               |
| ----------- | ----------------------------------------- |
| **Ingress** | SOCKS5 listener that Mutiauk and SOCKS clients connect to |
| **Transit** | Relay traffic between peers               |
| **Exit**    | Open connections to external destinations |

You can select multiple roles to create a combined agent.

### Step 3: Inbound Listener (Optional)

- **Accept inbound peer connections?**: Answer No for SOCKS5-only clients, laptops behind NAT, or any agent that only dials out to other peers. Declining skips the transport and address prompts below and emits `listeners: []`. Outbound peer dials are still configured in the next step; per-peer transport then defaults to QUIC.

When accepting inbound connections:

- **Transport type**: QUIC (recommended), HTTP/2, or WebSocket
- **Listen address**: IP and port to listen on (e.g., `0.0.0.0:4433`)
- **HTTP path**: Path for HTTP/2 and WebSocket transports (default: `/mesh`)
- **Reverse proxy**: For HTTP/2 and WebSocket, option to accept plain connections when TLS is terminated by a reverse proxy (e.g., Nginx, Caddy, Cloudflare)

### Step 4: TLS Configuration (Optional)

All traffic is end-to-end encrypted (X25519 + ChaCha20-Poly1305). Transport TLS adds certificate-based peer verification on top of E2E encryption.

- **Self-signed certificates (default)**: Auto-generate at startup - no configuration needed
- **Strict TLS with CA verification (advanced)**: For PKI-based certificate verification (includes mTLS option)

### Step 5: Peer Connections

Optionally connect to existing mesh agents:

- Enter peer address (host:port)
- Pin to specific agent ID (confirmation prompt; defaults to `auto` if declined)
- Transport defaults to listener's transport with option to change
- Enable strict TLS verification (requires CA-signed certs; disabled by default)
- HTTP path prompt for HTTP/2 and WebSocket peers
- Connectivity test with options: "Continue anyway", "Retry the connection test", "Re-enter peer configuration", "Skip this peer"

### Step 6: SOCKS5 Configuration (Ingress Role)

If ingress role is selected:

- **Listen address**: Where to accept SOCKS5 connections
- **Authentication**: Enable/disable, add username and password

The wizard does not prompt for the SOCKS5 WebSocket listener. To let Mutiauk on a remote workstation connect over TLS, add the `socks5.websocket` block to the generated config (Chapter 9).

### Step 7: Exit Configuration (Exit Role)

If exit role is selected:

- **CIDR routes**: Networks to advertise (e.g., `0.0.0.0/0`, `::/0`). If no routes entered, defaults to all traffic (`0.0.0.0/0` and `::/0`)
- **Domain routes**: Domain patterns for domain-based routing (optional)

### Step 8: Monitoring & Logging

- **Log level**: Debug, Info (default), Warning, or Error
- **HTTP management API**: Enable health checks, dashboard, and CLI remote commands
- **HTTP API authentication**: Optional bearer token to protect non-health endpoints

### Step 9: Management Commands

Skipped for transit-only agents.

- **Enable management commands**: Allow remote command execution (disabled by default)
- **Command whitelist**: "Allow all commands" (default), "Custom whitelist", or "No commands (lockdown)"

Management commands are authorized by the mesh management signing key generated in the **Management Key** step; there is no per-feature password.

### Step 10: File Transfer

Skipped for transit-only agents.

- **Enable file transfer**: Allow file upload/download (disabled by default)
- **Max file size**: Limit in MB (default: 500 MB, 0 = unlimited)
- **Path restrictions**: Allow all paths or restrict to specific directories

File transfer reuses the same management signing key as management commands; no separate password is asked for.

### Step 11: Management & Signing Keys

This step configures two independent keypairs that control mesh management:

- **Management key (X25519)**: encrypts topology data so only operators can view the network structure.
- **Signing key (Ed25519)**: authorizes management commands, file transfer, sleep/wake, and dynamic config changes. An agent without the signing public key cannot accept these privileged operations.

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

When you choose to embed config, the wizard **automatically** embeds the identity:

- `agent.id` is set to the generated agent ID
- `agent.private_key` is set to the generated X25519 private key
- `agent.data_dir` is cleared (not needed)

The resulting binary can run without any external files -- true single-file deployment.

When editing a target binary (`setup -c /path/to/binary`), the delivery step is skipped -- config is always embedded back into the target binary.

### Service Installation (Optional)

If a service with the same name is already installed, the wizard offers to update the binary and restart the service.

For fresh installations:

- **Linux (root)**: systemd service
- **Linux (non-root)**: User service via `systemd --user` (preferred when a user systemd manager is available) or cron `@reboot` + nohup as a fallback. The wizard auto-detects; you can also force a method with `--user-method=systemd|cron` when invoking `muti-metroo service install` directly.
- **macOS**: launchd service (requires root)
- **Windows (Administrator)**: Windows Service
- **Windows (non-admin)**: User service via Registry Run key

For Windows without Administrator access, use the CLI after the wizard:

```powershell
muti-metroo service install --user --dll path\to\muti-metroo.dll -c path\to\config.yaml
```

This uses the Registry Run key for automatic startup at user logon.

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
Display Name (press Enter to use Agent ID) []: Gateway Agent

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

Add peer connections? [y/N]: n

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
Route (or empty to finish) []: 0.0.0.0/0
Route (or empty to finish) []: ::/0
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
Management commands allow executing commands remotely on this agent.

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
  - Signing key (Ed25519): authorizes management commands, file transfer, sleep/wake, and
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

  Display Name:    Gateway Agent
  Agent ID:        abc123def456789012345678901234567
  E2E Public Key:  0123456789abcdef0123456789abcdef...
  Config file:     ./config.yaml
  Data dir:        ./data

  Listener:        quic://0.0.0.0:4433
  SOCKS5:          127.0.0.1:1080
  Exit routes:     [0.0.0.0/0 ::/0]
  HTTP API:        http://:8080

  To start the agent:
    muti-metroo run -c ./config.yaml
```

## After the Wizard

Once the wizard completes:

1. **Configuration saved**: Your `config.yaml` is ready
2. **Agent initialized**: Identity stored in data directory
3. **TLS certificates**: Auto-generated at startup (or embedded in config if using strict mode)

### Start the Agent Manually

If you did not start during the wizard:

```bash
muti-metroo run -c ./config.yaml
```

### Verify Operation

```bash
# Check health
curl http://localhost:8080/health

# Get detailed status (JSON)
curl http://localhost:8080/healthz | jq

# Test SOCKS5 (if exit is enabled)
curl -x socks5h://localhost:1080 https://example.com
```

## Re-running the Wizard

You can run the wizard again to modify configuration:

```bash
muti-metroo setup

# Existing config detected
Use existing configuration as base? [y]: y
```

The wizard will load existing values as defaults.

## Embedded Configuration

For single-file deployments, the wizard can embed configuration directly into the binary.

### Creating an Embedded Binary

During setup, choose "Embed in binary" when prompted:

```
Configuration Delivery
----------------------
> 1. Save to config file (traditional)
  2. Embed in binary (single-file deployment)

Delivery method [1]: 2
Service name [muti-metroo]: my-agent
Output binary path: ./my-agent
```

### Running Embedded Binary

```bash
# No config file or run command needed
./my-agent

# Output shows embedded config detection
Using embedded configuration
Starting Muti Metroo agent...
Display Name: my-agent
```

### Editing Embedded Configuration

To modify config in an existing embedded binary:

```bash
# Use regular binary to edit embedded one
muti-metroo setup -c /path/to/embedded-binary
```

The wizard extracts existing config as defaults, guides you through changes, and writes updated config back to the binary.

### Updating Deployed Agents

```bash
# Stop service
sudo systemctl stop my-agent

# Edit embedded config
muti-metroo setup -c /usr/local/bin/my-agent

# Restart service
sudo systemctl start my-agent
```

### Binary Format

```
[executable][XOR'd config][8-byte length][8-byte magic]
```

The XOR obfuscation prevents casual inspection but is not cryptographic security.

## Service Installation via Wizard

When running the wizard with elevated privileges, you can install as a system service:

### Linux (as root)

```bash
sudo muti-metroo setup
# At the end, choose to install as service
```

### Windows (as Administrator)

```powershell
# Run as Administrator
.\muti-metroo.exe setup
```

The wizard will offer to:

1. Create configuration in the appropriate system location
2. Register the system service
3. Enable automatic startup
4. Start the service
