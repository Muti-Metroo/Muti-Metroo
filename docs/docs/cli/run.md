---
title: run
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-reading.png" alt="Mole running commands" style={{maxWidth: '180px'}} />
</div>

# muti-metroo run

Start the agent. It reads your config file, connects to peers, and begins accepting connections.

```bash
muti-metroo run -c config.yaml
```

The agent runs in the foreground and logs to stdout. Use `Ctrl+C` to stop. For persistent background operation, see [System Service](/deployment/system-service). On Windows, `--detach` keeps it running after an SSH session ends without requiring service installation.

## Usage

```bash
muti-metroo run -c <config-file>
```

## Flags

- `-c, --config <file>`: Path to configuration file (default: `./config.yaml`)
- `--startup-delay <duration>`: Delay before starting network activity (e.g., `90s`, `2m`). Overrides config value.
- `-D, --detach`: Re-spawn the agent outside the SSH session's Job Object and exit immediately (Windows only). The agent continues running after the SSH session ends.

:::info Embedded Configuration
When using a binary with embedded configuration:
- Embedded configuration takes precedence over the `-c` flag
- If `default_action: run` is set in embedded config, the agent auto-starts without arguments
- Run the embedded binary without `run` command: `./my-agent` instead of `./my-agent run`
:::

## Examples

```bash
# Run with config file
muti-metroo run -c ./config.yaml

# Run with different config
muti-metroo run -c /etc/muti-metroo/config.yaml

# Run with a startup delay (wait 90 seconds before network activity)
muti-metroo run -c config.yaml --startup-delay 90s

# Start detached from an SSH session (Windows only)
muti-metroo run -c config.yaml --detach
```

## Environment Variables

Config file supports environment variable substitution:

```yaml
agent:
  data_dir: "${DATA_DIR:-./data}"
  log_level: "${LOG_LEVEL:-info}"
```

Set before running:

```bash
export DATA_DIR=/var/lib/muti-metroo
export LOG_LEVEL=debug
muti-metroo run -c config.yaml
```
