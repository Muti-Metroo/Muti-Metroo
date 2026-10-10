---
title: Embedded Configuration
sidebar_position: 2
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-hammering.png" alt="Mole embedding configuration" style={{maxWidth: '180px'}} />
</div>

# Embedded Configuration

Drop a single executable. No config files, no setup - just run the binary and the agent starts with all settings baked in.

**What you get:**
- One file to transfer and deploy
- Nothing to configure on the target system
- Custom binary name for each deployment
- Can still edit config later without rebuilding

:::note
Embedded configuration is supported for both standalone executables and the Windows DLL variant. The DLL can run with embedded config via `rundll32.exe muti-metroo.dll,Run` (no config path argument). See [DLL Mode](/deployment/dll-mode) for details.
:::

## How It Works

The configuration is appended to the binary itself:
- YAML config is XOR-obfuscated and added to the end of the executable
- The agent detects and loads embedded config automatically on startup
- No `-c` flag or `run` command needed - just `./my-agent`
- External config files are ignored when embedded config is present

## True Single-File Deployment

With embedded identity, the agent requires **zero external files**:

- **Agent ID** - Stored in config as `agent.id`
- **X25519 keypair** - Stored in config as `agent.private_key`
- **No data directory** - Not needed when identity is in config

The setup wizard offers this option automatically when embedding config.

## Creating an Embedded Binary

### Using the Setup Wizard

The easiest way to create an embedded binary:

```bash
muti-metroo setup
```

When prompted for "Configuration Delivery" (step 12 of 13), choose **"Embed in binary"**:

```
--- Configuration Delivery ----------------------------------------------------
Choose how to deploy the configuration.
Embedding creates a single-file binary with config baked in.

Delivery method:
  Save to config file (traditional)
> Embed in binary (single-file deployment)
```

You'll then specify:
- **Service name**: Custom name for the service (e.g., `my-agent`)
- **Output path**: Where to save the embedded binary

### Automatic Identity Embedding

When you choose to embed config, the wizard **automatically** embeds the identity:
- `agent.id` is set to the generated agent ID
- `agent.private_key` is set to the generated X25519 private key
- `agent.data_dir` is cleared (not needed)

The resulting binary can run with no external files - true single-file deployment.

### Binary Output

The wizard creates a new binary with the embedded config:

```bash
# Original binary size
$ ls -la muti-metroo
-rwxr-xr-x  1 user  staff  20753408 muti-metroo

# Embedded binary (config adds ~1-2KB)
$ ls -la my-agent
-rwxr-xr-x  1 user  staff  20755200 my-agent
```

### Non-interactive embedding

For CI pipelines and scripted deployments where the config is already prepared, `muti-metroo embed-config` embeds a YAML config without the interactive wizard:

```bash
# Embed config.yaml into the running muti-metroo binary, in place
muti-metroo embed-config config.yaml

# Embed into a specific source binary and write the result elsewhere
muti-metroo embed-config config.yaml -b ./muti-metroo -o ./my-agent
```

Flags:
- `-b, --binary`: source binary to embed into (default: the running executable).
- `-o, --output`: destination path (default: overwrite the source in place).

Behavior:
- **Validation**: the config is parsed and validated before anything is written; an invalid config aborts with no change to the binary.
- **Idempotent**: any config already embedded in the source binary is stripped first, so repeated runs replace rather than stack configs.
- **Atomic write**: the result is written to a temporary file in the destination directory and renamed into place, so a partially written binary is never left behind. Source permissions are preserved.
- **DLL rejection**: `.dll` targets are refused; use a config file with `rundll32.exe muti-metroo.dll,Run config.yaml` instead (see [DLL Mode](/deployment/dll-mode)).

As with the wizard, a binary with an embedded config auto-runs on launch unless the embedded config sets `default_action: help`.

## Running an Embedded Binary

Simply run the binary - no `run` command needed:

```bash
./my-agent
```

The wizard automatically sets `default_action: run` in embedded configs, so the agent starts without any arguments.

Output shows it's using embedded config:

```
Using embedded configuration
Starting Muti Metroo agent...
Display Name: my-agent
Agent ID: abc123def456...
```

Other commands still work normally:

```bash
./my-agent help     # Show help
./my-agent status   # Check status
./my-agent --version
```

## Editing Embedded Configuration

To modify the configuration in an existing embedded binary:

```bash
# Use a regular muti-metroo binary to edit the embedded one
muti-metroo setup -c /path/to/embedded-binary
```

The wizard will:
1. Detect and extract the embedded configuration
2. Use existing values as defaults
3. Skip questions about config file path and data directory (not needed for embedded binaries)
4. Preserve the existing agent identity (Agent ID and X25519 keypair)
5. Guide you through changes
6. Write the updated configuration directly back to the binary

No backup config file or data folder is created when editing an existing embedded binary - the config goes directly into the target binary.

### Example: Updating a Deployed Agent

```bash
# Stop the service
sudo systemctl stop my-agent

# Edit the embedded config
muti-metroo setup -c /usr/local/bin/my-agent

# Restart the service
sudo systemctl start my-agent
```

## Service Installation with Embedded Config

When installing as a service, embedded binaries are handled specially:

1. The binary is copied to a standard location:
   - Linux/macOS: `/usr/local/bin/<service-name>`
   - Windows: `C:\Program Files\<service-name>\<service-name>.exe`

2. The service definition runs without `-c` flag:
   ```bash
   # Traditional service
   ExecStart=/usr/local/bin/muti-metroo run -c /etc/muti-metroo/config.yaml

   # Embedded config service (no config flag needed)
   ExecStart=/usr/local/bin/my-agent run
   ```

### Linux (systemd)

```bash
# Wizard creates:
# - /usr/local/bin/my-agent (embedded binary)
# - /etc/systemd/system/my-agent.service

sudo systemctl enable my-agent
sudo systemctl start my-agent
```

### macOS (launchd)

```bash
# Wizard creates:
# - /usr/local/bin/my-agent (embedded binary)
# - /Library/LaunchDaemons/com.my-agent.plist

sudo launchctl load /Library/LaunchDaemons/com.my-agent.plist
```

### Windows

```powershell
# Wizard creates:
# - C:\Program Files\my-agent\my-agent.exe (embedded binary)
# - Windows Service registration

sc start my-agent
```

## Binary Format

The embedded binary format is:

```
[original executable]
[XOR-obfuscated YAML config]
[8-byte config length (little-endian)]
[8-byte magic: "MUTI_METROOCFG"]
```

The XOR obfuscation prevents casual inspection of the config but is **not cryptographic security**. Do not rely on it for protecting secrets - use the management key feature for sensitive data.

## Use Cases

### IoT and Edge Devices

Single-file deployment is ideal for resource-constrained devices:
- Deploy a single executable to edge nodes
- No config files to manage on limited storage
- Pre-configured for immediate operation
- Easy to update config without redeploying binary

### Air-Gapped Environments

For environments without network access during deployment:
- Pre-configure the binary with all settings
- Transfer single file to target
- Run immediately without additional setup

### Simplified Distribution

For distributing pre-configured agents:
- Build once with embedded config
- Distribute single file to remote sites
- No separate config file management

## Backup Configuration

When creating a **new** embedded binary via `muti-metroo setup`, the wizard also saves a backup config file:

```
Backup config saved to: ./config.yaml
```

Keep this backup for:
- Documentation of the configuration
- Recovery if the binary is lost
- Reference when editing embedded config

**Note**: When editing an existing embedded binary via `muti-metroo setup -c /path/to/binary`, no backup file is created - changes go directly into the binary.

## Security Considerations

### Config Obfuscation

The XOR obfuscation:
- Prevents casual `strings` inspection
- Is NOT encryption - can be reversed
- Should not be relied upon for secrets

### Secrets Management

For sensitive data:
- Use the management key feature for topology encryption
- Store passwords as bcrypt hashes (generated by `muti-metroo hash`)
- Consider environment variable substitution for runtime secrets

### Binary Integrity

After embedding:
- The binary checksum will change
- Re-sign if code signing is required
- Update any integrity verification systems
