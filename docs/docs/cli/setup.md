---
title: setup
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-idea.png" alt="Mole setup wizard" style={{maxWidth: '180px'}} />
</div>

# muti-metroo setup

Create a complete agent configuration through guided prompts. The wizard generates everything you need: config file, TLS certificates, and optionally installs as a system service.

**Best for:**

- First-time setup
- Creating configs without memorizing YAML syntax
- Single-file deployments with embedded config

```bash
muti-metroo setup
```

## Usage

```bash
# New configuration
muti-metroo setup

# Edit embedded configuration in existing binary
muti-metroo setup -c /path/to/embedded-binary
```

## Flags

| Flag       | Short | Description                                        |
| ---------- | ----- | -------------------------------------------------- |
| `--config` | `-c`  | Path to config file or binary with embedded config |

:::warning Windows DLL Files
DLL files cannot be used with `setup -c` for config embedding (UPX compression is incompatible). For DLL-based deployments, use a separate config file and specify its path when running via `rundll32.exe`.
:::

## What It Does

Guides you through 11 numbered steps:

1. Basic configuration (config file, data directory, display name)
2. Agent role selection (ingress, transit, exit - multi-select, transit is default)
3. Network configuration (transport, listen address, reverse proxy option for HTTP/2 and WebSocket)
4. TLS certificate setup (self-signed or strict CA verification)
5. Peer connections (with connectivity testing)
6. SOCKS5 configuration (if ingress role selected)
7. Exit routes (if exit role selected)
8. Monitoring & logging (log level, HTTP management API, API authentication)
9. Management commands (skipped for transit-only, password optional)
10. File transfer (skipped for transit-only, password optional)
11. Management key encryption (topology privacy)

Then:

- Configuration delivery (save to file or embed in binary)
- Service installation (if root/admin, with update support for existing services)

## Output

Generates complete `config.yaml` and optionally:

- TLS certificates
- Agent identity
- Embedded binary (single-file deployment)
- Systemd service file (Linux)
- Windows Service registration (Windows)

## Embedded Configuration

The wizard can embed configuration directly into the binary for single-file deployments:

```bash
# During setup, choose "Embed in binary" when prompted
muti-metroo setup

# Edit existing embedded config
muti-metroo setup -c /usr/local/bin/my-agent
```

See [Embedded Configuration](/deployment/embedded-config) for details.

See [Interactive Setup](/getting-started/interactive-setup) for detailed guide.
