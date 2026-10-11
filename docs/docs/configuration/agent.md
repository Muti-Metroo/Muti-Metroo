---
title: Agent
description: Configuration reference for the agent section, covering agent ID, display name, data directory, identity keypair, logging, and startup delay.
sidebar_position: 2
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-inspecting.png" alt="Mole configuring agent" style={{maxWidth: '180px'}} />
</div>

# Agent

The `agent` section sets the agent's identity, where it keeps persistent state, and how it logs. With the defaults, the agent generates its ID and keypair on first start and stores them in `./data`.

## Configuration

```yaml
agent:
  id: "auto"                # "auto" or a 32-character hex agent ID
  display_name: "Office Gateway" # Shown in the dashboard and node info
  data_dir: "./data"        # Identity files, dynamic config journal, sleep state
  log_level: "info"         # debug, info, warn, error
  log_format: "text"        # text, json
  startup_delay: 0s         # Wait before starting listeners, peers, SOCKS5
  # private_key: "48bbea6c0c9be254bde983c92c8a53db759f27e51a6ae77fd9cca81895a5d57c"
  # public_key: ""          # Optional; must match private_key when set
```

## Keys

| Key             | Type     | Default    | Description                                                                                                                                                                                  |
| --------------- | -------- | ---------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `id`            | string   | `"auto"`   | Agent ID. `auto` (or empty) loads the ID from `data_dir/agent_id`, generating it on first start. Any other value must be 32 hexadecimal characters and is used as-is.                       |
| `display_name`  | string   | `""`       | Human-readable name advertised to the mesh. Unicode is allowed; maximum 255 bytes. When empty, the short (8-character) agent ID is shown.                                                    |
| `data_dir`      | string   | `"./data"` | Directory for persistent state. Required unless both `id` and `private_key` are set in the configuration.                                                                                   |
| `log_level`     | string   | `"info"`   | `debug`, `info`, `warn`, or `error`.                                                                                                                                                         |
| `log_format`    | string   | `"text"`   | `text` or `json`.                                                                                                                                                                            |
| `startup_delay` | duration | `0s`       | Time to wait after the process starts before starting listeners, peer connections, SOCKS5, and the other network components. Must not be negative.                                         |
| `private_key`   | string   | `""`       | X25519 identity private key as 64 hexadecimal characters. When set, the keypair is taken from the configuration instead of `data_dir`.                                                      |
| `public_key`    | string   | `""`       | X25519 identity public key as 64 hexadecimal characters. Optional: it is derived from `private_key`. If set, it must match the derived key or the agent refuses to start. Requires `private_key`. |

## Agent ID

Every agent has a 128-bit ID, written as 32 hexadecimal characters. With `id: "auto"` the agent reads `data_dir/agent_id`, or generates a random ID and writes it there on first start.

An explicit ID is used as given and, when `data_dir` is set, also written to `data_dir/agent_id`:

```yaml
agent:
  id: "a1b2c3d4e5f6789012345678901234ab"
```

To read the ID of an agent:

```bash
cat ./data/agent_id
muti-metroo status
```

`muti-metroo run` also prints `Agent ID: <id>` when it starts.

## Display name

```yaml
agent:
  display_name: "Office Gateway"
```

The display name appears in the dashboard, in `muti-metroo` output, and in node info advertisements. A longer value than 255 bytes fails validation.

The name can be changed at runtime with [`muti-metroo display-name set`](/cli/display-name). A runtime name overrides this key and, when `data_dir` is set, persists across restarts. Setting an empty runtime name reverts to the configured value.

## Data directory

```yaml
agent:
  data_dir: "/var/lib/muti-metroo"
```

The agent creates the directory with mode `0700` if it does not exist. It holds:

| File                 | Contents                                                                                     |
| -------------------- | -------------------------------------------------------------------------------------------- |
| `agent_id`           | Agent ID (mode `0600`)                                                                       |
| `agent_key`          | X25519 identity private key (mode `0600`); not used when `private_key` is set               |
| `agent_key.pub`      | X25519 identity public key (mode `0644`); not used when `private_key` is set                |
| `dynamic-config.log` | Journal of runtime changes, see [Dynamic Config Persistence](/configuration/dynamic-config-persistence) |
| `sleep_state.json`   | Sleep mode state, see [Sleep](/configuration/sleep)                                          |

Each agent needs its own data directory. Two agents sharing one directory share an identity.

When `data_dir` is empty, runtime changes and sleep state are not persisted.

## Identity keypair

Each agent has an X25519 keypair used for end-to-end encryption. By default the keypair is generated on first start and stored in `data_dir`.

For a single-file deployment, put the ID and private key in the configuration and leave `data_dir` empty:

```yaml
agent:
  id: "a1b2c3d4e5f6789012345678901234ab"
  private_key: "48bbea6c0c9be254bde983c92c8a53db759f27e51a6ae77fd9cca81895a5d57c"
  data_dir: ""
```

Both `id` and `private_key` are needed: with `id: "auto"`, `data_dir` is still required to store the generated ID.

The [setup wizard](/getting-started/interactive-setup) fills in `id` and `private_key` automatically when it embeds the configuration in a binary. To produce the values by hand, start an agent once against a scratch data directory, stop it with `Ctrl+C` after it prints its agent ID, and copy the files:

```bash
printf 'agent:\n  data_dir: "/tmp/keys"\n' > /tmp/keys.yaml
muti-metroo run -c /tmp/keys.yaml
cat /tmp/keys/agent_id /tmp/keys/agent_key
rm -rf /tmp/keys /tmp/keys.yaml
```

`muti-metroo init` creates only `agent_id`; the keypair files are written the first time the agent runs.

## Logging

```yaml
agent:
  log_level: "info"
  log_format: "json"
```

Logs are written to stderr. The `text` format:

```text
time=2026-10-11T14:50:05.746+03:00 level=INFO msg="starting agent" agent_id=5725c9df component=agent
time=2026-10-11T14:50:05.746+03:00 level=INFO msg="SOCKS5 server started" address=127.0.0.1:1080
```

The `json` format, one object per line:

```text
{"time":"2026-10-11T14:50:09.787881+03:00","level":"INFO","msg":"starting agent","agent_id":"5725c9df","component":"agent"}
{"time":"2026-10-11T14:50:09.788015+03:00","level":"INFO","msg":"SOCKS5 server started","address":"127.0.0.1:1080"}
```

## Startup delay

```yaml
agent:
  startup_delay: 90s
```

The agent waits for the delay before it starts listeners, dials peers, and starts SOCKS5, forward listeners, and the exit handler. The HTTP API starts before the delay, so health probes answer while the agent waits. `Ctrl+C` or `SIGTERM` during the delay stops the agent cleanly.

The `--startup-delay` flag of [`muti-metroo run`](/cli/run) overrides this key.

## Examples

### Production host

```yaml
agent:
  display_name: "${AGENT_NAME:-gateway}"
  data_dir: "/var/lib/muti-metroo"
  log_format: "json"
```

### Container

```yaml
agent:
  display_name: "${AGENT_NAME:-container}"
  data_dir: "/app/data"
  log_level: "${LOG_LEVEL:-info}"
  log_format: "json"
```

## See Also

- [Dynamic Config Persistence](/configuration/dynamic-config-persistence) for what the agent writes to `data_dir` at runtime
- [init CLI](/cli/init) to create a data directory and identity ahead of time
- [display-name CLI](/cli/display-name) to change the display name at runtime
- [Embedded Configuration](/deployment/embedded-config) for single-file deployments with the identity in the configuration
- [E2E Encryption](/security/e2e-encryption) for how the identity keypair is used
