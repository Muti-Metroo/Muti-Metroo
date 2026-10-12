# Installation

This chapter covers all methods for installing Muti Metroo.

## Get the Binary

Pre-built binaries are available for all major platforms:

| Platform | Architecture | Binary Name |
|----------|--------------|-------------|
| Linux | x86_64 | `muti-metroo-linux-amd64` |
| Linux | ARM64 | `muti-metroo-linux-arm64` |
| macOS | Apple Silicon | `muti-metroo-darwin-arm64` |
| macOS | Intel | `muti-metroo-darwin-amd64` |
| Windows | x86_64 | `muti-metroo-windows-amd64.exe` |
| Windows | ARM64 | `muti-metroo-windows-arm64.exe` |
| Windows | x86_64 (DLL) | `muti-metroo-windows-amd64.dll` |
| Windows | ARM64 (DLL) | `muti-metroo-windows-arm64.dll` |

Obtain the file for your platform from the distribution channel your team uses.

### Linux/macOS Installation

Once you have the binary in your working directory (example: Linux amd64):

```bash
chmod +x muti-metroo-linux-amd64
sudo mv muti-metroo-linux-amd64 /usr/local/bin/muti-metroo

# Verify
muti-metroo --version
```

### Windows Installation

1. Place the appropriate `.exe` file in a directory (e.g., `C:\muti-metroo\`)
2. Optionally add that directory to PATH

```powershell
# Verify installation
.\muti-metroo.exe --version
```

### DLL Mode (Windows)

For background execution without a console window, use the DLL variant with `rundll32.exe`:

```powershell
# With an explicit config path
rundll32.exe C:\path\to\muti-metroo.dll,Run C:\path\to\config.yaml

# Or with embedded config (omit the path)
rundll32.exe C:\path\to\muti-metroo.dll,Run
```

The DLL supports both modes: pass a config path on the command line, or omit it to use the configuration embedded in the DLL by the setup wizard. UPX-compressed DLLs still load embedded config correctly.

Both architectures ship a native DLL (`muti-metroo-windows-amd64.dll` and `muti-metroo-windows-arm64.dll`). Use the DLL matching the host architecture and invoke it with the standard `rundll32.exe` in `System32` -- no emulation layer is required on ARM64.

**Process Behavior:**

The DLL runs as a background process (not a Windows service):

- No console window - runs silently in the background
- Appears as `rundll32.exe` in Task Manager
- Does NOT survive reboots - use Registry Run for persistence
- Cannot be managed via Services console (`services.msc`)

To terminate: `taskkill /F /IM rundll32.exe`

**Use .exe for:**

- Normal operation with console output
- Persistent service installation (`muti-metroo service install`)
- Interactive commands (shell, upload, download)

**Use .dll for:**

- Background execution without console window
- Quick deployments without service installation
- Scenarios where hiding the console is important

**Programmatic Installation via Install Export:**

The DLL exports an `Install` function for programmatic service installation without requiring the CLI executable. This is useful for custom deployment tools and automated installers:

```powershell
# Default service name (muti-metroo)
rundll32.exe C:\path\to\muti-metroo.dll,Install C:\path\to\config.yaml

# Custom service name
rundll32.exe C:\path\to\muti-metroo.dll,Install my-tunnel C:\path\to\config.yaml
```

The optional service name argument controls the Registry Run value name (converted to PascalCase, e.g., `my-tunnel` becomes `MyTunnel`). When omitted, it defaults to `muti-metroo`.

This handles the full installation: stops any existing service (for upgrades), creates the Registry Run key, writes service tracking info, and starts the agent immediately. The config file and DLL must both exist at the specified paths before calling `Install`.

**Persistence with Registry Run (via CLI):**

When the CLI executable is available, use it for installation and management:

```powershell
muti-metroo service install --user --dll C:\path\to\muti-metroo.dll -c C:\path\to\config.yaml
```

This creates a Registry Run entry at `HKCU\Software\Microsoft\Windows\CurrentVersion\Run` that starts automatically at user logon.

**Manual Registry Setup:**

```powershell
# Add Registry Run entry (no admin required)
$dll = "C:\Users\$env:USERNAME\muti-metroo\muti-metroo.dll"
$cfg = "C:\Users\$env:USERNAME\muti-metroo\config.yaml"
$cmd = "rundll32.exe `"$dll`",Run `"$cfg`""

Set-ItemProperty -Path "HKCU:\Software\Microsoft\Windows\CurrentVersion\Run" `
    -Name "Muti Metroo" -Value $cmd
```

For system-wide startup (requires admin), use Windows Service instead:

```powershell
muti-metroo.exe service install -c C:\ProgramData\muti-metroo\config.yaml
```

## Docker Deployment

For containerized deployments:

```bash
# Build from source
docker build -t muti-metroo .

# Run with config
docker run -v $(pwd)/config.yaml:/etc/muti-metroo/config.yaml \
  -p 4433:4433/udp \
  -p 1080:1080 \
  -p 8080:8080 \
  muti-metroo run -c /etc/muti-metroo/config.yaml
```

## Directory Structure

After installation, you will typically have:

### Production Layout (Linux)

```
/usr/local/bin/muti-metroo     # Binary
/etc/muti-metroo/              # Configuration
  config.yaml                  # Main configuration file
  certs/                       # TLS certificates
    ca.crt
    agent.crt
    agent.key
/var/lib/muti-metroo/          # Data directory
  agent_id                     # Agent identity
  agent_key                 # E2E encryption keypair
```

### Development Layout

```
./muti-metroo                  # Binary
./config.yaml                  # Configuration file
./data/                        # Data directory
  agent_id                     # Agent identity (128-bit hex)
  agent_key                 # E2E encryption keypair
./certs/                       # TLS certificates
  ca.crt                       # Certificate Authority
  ca.key                       # CA private key
  agent.crt                    # Agent certificate
  agent.key                    # Agent private key
```

### Windows Layout

```
C:\Program Files\muti-metroo\
  muti-metroo.exe              # Binary
C:\ProgramData\muti-metroo\
  config.yaml                  # Configuration
  data\                        # Data directory
    agent_id
  certs\                       # Certificates
    ca.crt
    agent.crt
    agent.key
```

## Verify Installation

After installation, verify everything works:

```bash
# Initialize agent identity
muti-metroo init -d ./data

# Check the generated agent ID
cat ./data/agent_id

# Generate test certificates
muti-metroo cert ca --cn "Test CA" -o ./certs
muti-metroo cert agent --cn "test-agent" \
  --ca ./certs/ca.crt \
  --ca-key ./certs/ca.key \
  -o ./certs

# Verify certificate
muti-metroo cert info ./certs/test-agent.crt
```

## System Requirements

- **OS**: Linux, macOS, or Windows
- **Architecture**: x86_64 or ARM64
- **Memory**: Minimum 64MB, recommended 256MB+
- **Network**: UDP port for QUIC, TCP ports for HTTP/2 and WebSocket
- **Privileges**: No root required (runs in userspace)

## Firewall Configuration

Ensure the following ports are accessible:

| Port | Protocol | Purpose |
|------|----------|---------|
| 4433 | UDP | QUIC transport (default) |
| 8443 | TCP | HTTP/2 transport (optional) |
| 443 | TCP | WebSocket transport (optional) |
| 1080 | TCP | SOCKS5 proxy (ingress) |
| 8080 | TCP | HTTP API and dashboard |

Example firewall rules:

```bash
# Linux (firewalld)
sudo firewall-cmd --permanent --add-port=4433/udp
sudo firewall-cmd --permanent --add-port=1080/tcp
sudo firewall-cmd --permanent --add-port=8080/tcp
sudo firewall-cmd --reload

# Linux (ufw)
sudo ufw allow 4433/udp
sudo ufw allow 1080/tcp
sudo ufw allow 8080/tcp
```
