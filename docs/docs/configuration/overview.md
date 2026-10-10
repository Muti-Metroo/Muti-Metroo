---
title: Overview
sidebar_position: 1
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-inspecting.png" alt="Mole inspecting configuration" style={{maxWidth: '180px'}} />
</div>

# Configuration Reference

Control every aspect of your agent through a single YAML file. Start with the minimal config and add sections as you need them.

## Quick Reference

| I want to...                  | Section                                                                 |
| ----------------------------- | ----------------------------------------------------------------------- |
| Set agent name and logging    | [Agent](/configuration/agent)                                           |
| Accept incoming connections   | [Listeners](/configuration/listeners)                                   |
| Connect to other agents       | [Peers](/configuration/peers)                                           |
| Create a SOCKS5 proxy         | [SOCKS5](/configuration/socks5)                                         |
| Route traffic to destinations | [Exit](/configuration/exit)                                             |
| Publish mesh-local hostnames  | [Hosts](/configuration/hosts)                                           |
| Set up port forwarding        | [Forward](/configuration/forward)                                       |
| Enable UDP relay              | [UDP](/configuration/udp)                                               |
| Enable ICMP ping              | [ICMP](/configuration/icmp)                                             |
| Configure sleep mode          | [Sleep](/configuration/sleep)                                           |
| Enable management commands    | [Management Commands](/configuration/shell)                             |
| Enable file transfer          | [File Transfer](/configuration/file-transfer)                           |
| Configure HTTP API            | [HTTP](/configuration/http)                                             |
| Tune route propagation        | [Routing](/configuration/routing)                                       |
| Persist runtime CLI changes   | [Dynamic Config Persistence](/configuration/dynamic-config-persistence) |
| Encrypt mesh topology         | [Management](/configuration/management)                                 |
| Set up TLS certificates       | [TLS Certificates](/configuration/tls-certificates)                     |
| Use secrets from environment  | [Environment Variables](/configuration/environment-variables)           |

## Configuration File

Muti Metroo uses YAML configuration files. The default location is `./config.yaml`, but you can specify a different path:

```bash
muti-metroo run -c /path/to/config.yaml
```

## Minimal Configuration

The simplest working configuration:

```yaml
agent:
  data_dir: "./data"

listeners:
  - transport: quic
    address: ":4433"

socks5:
  enabled: true
  address: "127.0.0.1:1080"

exit:
  enabled: true
  routes:
    - "0.0.0.0/0"

http:
  enabled: true
  address: ":8080"
```

TLS certificates are auto-generated at startup. See [TLS Certificates](/configuration/tls-certificates) for custom certificate setup.

## Full Example

See [configs/example.yaml](https://github.com/postalsys/Muti-Metroo/blob/master/configs/example.yaml) for a fully commented configuration file with all options.

## Configuration Sections

### Core

| Section     | Purpose                 | Documentation                                       |
| ----------- | ----------------------- | --------------------------------------------------- |
| `agent`     | Agent identity, logging | [Agent](/configuration/agent)                       |
| `tls`       | Global TLS settings     | [TLS Certificates](/configuration/tls-certificates) |
| `listeners` | Accept peer connections | [Listeners](/configuration/listeners)               |
| `peers`     | Connect to other agents | [Peers](/configuration/peers)                       |

### Ingress

| Section             | Purpose                | Documentation                     |
| ------------------- | ---------------------- | --------------------------------- |
| `socks5`            | SOCKS5 proxy           | [SOCKS5](/configuration/socks5)   |
| `forward.listeners` | Port forward listeners | [Forward](/configuration/forward) |

### Exit

| Section             | Purpose                      | Documentation                     |
| ------------------- | ---------------------------- | --------------------------------- |
| `exit`              | CIDR and domain routes       | [Exit](/configuration/exit)       |
| `mesh_hosts`        | Mesh-local hostname registry | [Hosts](/configuration/hosts)     |
| `forward.endpoints` | Port forward endpoints       | [Forward](/configuration/forward) |
| `udp`               | UDP relay                    | [UDP](/configuration/udp)         |
| `icmp`              | ICMP ping                    | [ICMP](/configuration/icmp)       |

### Remote Access

| Section         | Purpose                  | Documentation                                 |
| --------------- | ------------------------ | --------------------------------------------- |
| `shell`         | Remote command execution | [Shell](/configuration/shell)                 |
| `file_transfer` | File upload/download     | [File Transfer](/configuration/file-transfer) |

### HTTP API

| Section | Purpose                 | Documentation               |
| ------- | ----------------------- | --------------------------- |
| `http`  | Health, dashboard, APIs | [HTTP](/configuration/http) |

### Tuning

| Section       | Purpose                 | Documentation                                       |
| ------------- | ----------------------- | --------------------------------------------------- |
| `routing`     | Route advertisement     | [Routing](/configuration/routing)                   |
| `connections` | Keepalive, reconnection | [Routing](/configuration/routing#connection-tuning) |
| `limits`      | Stream limits, buffers  | [Routing](/configuration/routing#resource-limits)   |

### Sleep

| Section | Purpose               | Documentation                 |
| ------- | --------------------- | ----------------------------- |
| `sleep` | Mesh hibernation mode | [Sleep](/configuration/sleep) |

### Runtime State

| Section          | Purpose                                                                         | Documentation                                                           |
| ---------------- | ------------------------------------------------------------------------------- | ----------------------------------------------------------------------- |
| `agent.data_dir` | Persist runtime `peer`/`forward`/`route`/`display-name` changes across restarts | [Dynamic Config Persistence](/configuration/dynamic-config-persistence) |

### Security

| Section      | Purpose              | Documentation                                       |
| ------------ | -------------------- | --------------------------------------------------- |
| `management` | Topology encryption  | [Management](/configuration/management)             |
| `protocol`   | Protocol identifiers | [TLS Certificates](/configuration/tls-certificates) |

## Environment Variables

All configuration values support environment variable substitution:

```yaml
agent:
  data_dir: "${DATA_DIR:-./data}"
  log_level: "${LOG_LEVEL:-info}"

socks5:
  auth:
    users:
      - username: "${SOCKS_USER}"
        password_hash: "${SOCKS_PASS_HASH}"
```

Syntax:

- `${VAR}` - Use environment variable (kept as literal if not set)
- `${VAR:-default}` - Use default if variable not set

See [Environment Variables](/configuration/environment-variables) for more details.

## Validation

Configuration is validated on startup. Common errors:

```
ERROR  Invalid configuration: socks5.address: invalid address format
ERROR  Invalid configuration: listeners[0].tls.cert: file not found
ERROR  Invalid configuration: peers[0].id: invalid agent ID format
```

## Reloading

Configuration cannot be reloaded without restart. To apply changes:

```bash
# Stop agent (Ctrl+C or SIGTERM)
# Edit config.yaml
# Start agent
muti-metroo run -c ./config.yaml
```

## Best Practices

1. **Use environment variables** for secrets (passwords, keys)
2. **Keep configs in version control** (without secrets)
3. **Use display_name** for easier dashboard identification
4. **Start minimal** and add features as needed
5. **Test configuration** before production deployment

## Embedded Configuration

For single-file deployments, configuration can be embedded in the binary:

```yaml
# When running embedded binary without arguments
default_action: run # Auto-start agent
# default_action: help # Show help (default)
```

The setup wizard can create embedded binaries:

```bash
muti-metroo setup
# Choose "Embed in binary" for configuration delivery
```

See [Embedded Configuration](/deployment/embedded-config) for details.

## Next Steps

- [Agent Configuration](/configuration/agent) - Identity and logging
- [Listeners](/configuration/listeners) - Transport setup
- [Peers](/configuration/peers) - Connecting to other agents
- [Getting Started](/getting-started/overview) - First-time setup
