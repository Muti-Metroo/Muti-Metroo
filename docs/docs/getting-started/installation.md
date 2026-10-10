---
title: Installation
sidebar_position: 3
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-hammering.png" alt="Mole building" style={{maxWidth: '180px'}} />
</div>

# Installation

Muti Metroo is a single binary with no dependencies. Download it, make it executable, and you are ready to tunnel traffic through any network.

## Download Binary

Download the pre-built binary for your platform - no compilation or package manager required.

**[Download Muti Metroo](/download)**

Pre-built binaries are available for:

- **macOS**: Apple Silicon (arm64) and Intel (amd64) - both installer packages and standalone binaries
- **Linux**: x86_64 (amd64) and ARM64 (aarch64)
- **Windows**: x86_64 (amd64) and ARM64

### Quick Install (Linux/macOS)

```bash
# Download for your platform (example: Linux amd64)
curl -L -o muti-metroo __MUTI_METROO_RELEASE__/muti-metroo-linux-amd64

# Make executable and install
chmod +x muti-metroo
sudo mv muti-metroo /usr/local/bin/

# Verify installation
muti-metroo --version
```

See the [Download page](/download) for platform-specific instructions.

## Docker Deployment

For containerized deployments, see [Docker Deployment](/deployment/docker) for Docker Compose setup and configuration examples.

## System Service Installation

Install Muti Metroo as a system service for automatic startup.

### Linux (systemd)

```bash
# Install as service (requires root) - auto-starts immediately
sudo muti-metroo service install -c /etc/muti-metroo/config.yaml

# Check status
sudo systemctl status muti-metroo

# View logs
sudo journalctl -u muti-metroo -f

# Restart (after config changes)
sudo systemctl restart muti-metroo
```

### Windows

```powershell
# Install as Windows Service (requires Administrator) - auto-starts immediately
muti-metroo.exe service install -c C:\ProgramData\muti-metroo\config.yaml

# Check status
sc query muti-metroo
```

:::tip No Manual Start Needed
The `service install` command automatically enables and starts the service. You don't need to run additional commands or reboot.
:::

### Uninstall Service

```bash
# Linux
sudo muti-metroo service uninstall

# Windows (as Administrator)
muti-metroo.exe service uninstall
```

## Directory Structure

After installation, you will typically have:

```
/etc/muti-metroo/          # Configuration (Linux)
  config.yaml              # Main configuration file
/var/lib/muti-metroo/      # Data directory (Linux)
  agent_id                 # Agent identity file
/var/log/muti-metroo/      # Logs (if using file logging)
```

For development:

```
./config.yaml              # Configuration file
./data/                    # Data directory
  agent_id                 # Agent identity (128-bit hex)
  agent_key                # E2E encryption private key
  agent_key.pub            # E2E encryption public key
./certs/                   # TLS certificates
  ca.crt                   # Certificate Authority
  ca.key                   # CA private key
  <name>.crt               # Agent certificate (named after -n flag)
  <name>.key               # Agent private key
```

## Verify Installation

After installation, verify everything works:

```bash
# Check the binary works
muti-metroo --version

# Initialize agent identity
muti-metroo init -d ./data
# Output: Agent ID: a1b2c3d4...

# Check the generated files
ls ./data/
# Output: agent_id  agent_key  agent_key.pub
```

:::info No Certificate Setup Required
TLS certificates are auto-generated at startup. Manual certificate setup is only needed for [strict TLS verification](/configuration/tls-certificates) or mTLS.
:::

## Next Steps

- [Interactive Setup](/getting-started/interactive-setup) - Guided wizard (recommended)
- [Quick Start](/getting-started/quick-start) - Manual configuration
- [Configuration Reference](/configuration/overview) - All configuration options
