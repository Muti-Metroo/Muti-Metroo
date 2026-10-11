---
title: muti-metroo display-name
sidebar_label: display-name
sidebar_position: 18
description: Set or read an agent's display name at runtime, locally or on a remote agent.
---

# muti-metroo display-name

Set or read the display name of a running agent. The display name labels the agent in topology views, the dashboard, and agent listings. A name set with this command overrides `agent.display_name` from the configuration until it is cleared.

The commands talk to the agent's HTTP API. Pass `-t` to act on another agent in the mesh, reached through the agent at `--agent`. An agent that has `management.public_key` configured without `management.private_key` refuses both `set` and `get`.

Global flags (`--token`, `--yes`, `--no-color`) are described in [CLI overview](overview.md#global-flags).

## display-name set

```bash
muti-metroo display-name set <name> [flags]
```

Set the display name. The new name is advertised to peers right away. The name may be up to 255 bytes long.

An empty name (`""`) clears the runtime name and reverts to `agent.display_name`. If that is also empty, the agent is shown by its short ID.

When `agent.data_dir` is set, the name is recorded in the [dynamic config journal](../configuration/dynamic-config-persistence.md) and restored on the next start. Without a `data_dir`, or if the journal cannot be written, the change lasts until the agent restarts.

Setting a display name is a privileged operation: the agent at `--agent` must have `management.signing_private_key` configured. For a remote target, the entry agent signs the request and the target verifies it with its `management.signing_public_key`. See [management configuration](../configuration/management.md).

### Flags

| Flag | Default | Description |
| --- | --- | --- |
| `-a`, `--agent` | `localhost:8080` | Agent HTTP API address used as the entry point. See [agent address](overview.md#agent-address). |
| `-t`, `--target` | none | ID of the agent to rename: a full 32-character ID or a unique prefix of one. Omit for the agent at `--agent`. |

### Confirmation prompt

Only the empty name prompts:

```text
An empty name reverts the agent's display name to the config-file value.
Continue? [y/N]:
```

Answering anything other than `y` or `yes` prints `Aborted.` and exits 0. `--yes` skips the prompt; without it, a non-terminal stdin makes the command exit 6. See [confirmation prompts](overview.md#confirmation-prompts).

### Output

```text
Display name set: gateway-us-east
```

When reverting:

```text
Display name reverted to config value: gateway-a
```

### Examples

Rename the local agent:

```bash
muti-metroo display-name set "gateway-us-east"
```

Rename a remote agent by ID prefix:

```bash
muti-metroo display-name set "exit-eu-west" -t abc123de
```

Revert to the configured name without prompting:

```bash
muti-metroo display-name set "" -y
```

## display-name get

```bash
muti-metroo display-name get [flags]
```

Print the agent's current display name: the runtime name if one is set, otherwise `agent.display_name`, otherwise the short agent ID.

### Flags

| Flag | Default | Description |
| --- | --- | --- |
| `-a`, `--agent` | `localhost:8080` | Agent HTTP API address used as the entry point. See [agent address](overview.md#agent-address). |
| `-t`, `--target` | none | ID of the agent to query: a full 32-character ID or a unique prefix of one. Omit for the agent at `--agent`. |

### Output

```text
gateway-us-east
```

### Examples

```bash
muti-metroo display-name get
```

```bash
muti-metroo display-name get -a 192.168.1.10:8080 -t def456789012345678901234567890cd
```

## See Also

- [Display name API](../api/display-name-management.md): the HTTP endpoint behind these commands
- [Agent configuration](../configuration/agent.md): set a permanent name with `agent.display_name`
- [Dynamic config persistence](../configuration/dynamic-config-persistence.md): how runtime changes survive restarts
- [Runtime configuration](../features/runtime-configuration.md): other settings you can change on a running agent
