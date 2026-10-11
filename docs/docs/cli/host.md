---
title: muti-metroo host
sidebar_label: host
sidebar_position: 17
description: Add, remove, and list mesh hostnames and the TLD suffixes they may use on a running agent.
---

# muti-metroo host

Manage the agent's [mesh hostname](../features/mesh-hostnames.md) registry, which maps names such as `target-11.mesh` to IPv4 and IPv6 addresses. Mutiauk reads the registry from the agent and resolves these names locally, so you can address hosts by name instead of IP.

The registry belongs to the agent you call. These commands have no `-t` option: run them against each agent that should serve the names, or define the names in that agent's configuration.

Entries and suffixes from the configuration (`mesh_hosts` and `mesh_hosts_suffixes`, see [hosts configuration](../configuration/hosts.md)) are loaded at startup. Runtime changes made with these commands are recorded in the [dynamic config journal](../configuration/dynamic-config-persistence.md) under `agent.data_dir` and replayed on the next start. Without a `data_dir`, runtime changes last until the agent restarts.

`add` and `remove` (for hosts and suffixes) are privileged operations: the agent must have `management.signing_private_key` configured. `list` needs only the HTTP API, plus a bearer token when `http.token_hash` is set. See [management configuration](../configuration/management.md).

Global flags (`--token`, `--yes`, `--no-color`) are described in [CLI overview](overview.md#global-flags).

## Name rules

- Names are trimmed and lowercased.
- A name must end with `.` followed by one of the active suffixes (`mesh` by default), for example `gateway.mesh`. A name may also equal a multi-label suffix, such as `muti-metroo.alt` when that suffix is active. A name equal to a single-label suffix, such as `mesh`, is rejected.
- Each label before the suffix must be a valid DNS label: letters, digits, and internal hyphens, 1 to 63 characters.
- The whole name may be up to 253 characters.
- At least one of `--ipv4` and `--ipv6` is required.

The registry only maps names to addresses. It does not add routes; the address must already be reachable through the mesh.

## host add

```bash
muti-metroo host add <name> [flags]
```

Add a runtime entry. Adding a name that already exists, from the configuration or at runtime, fails; remove the entry first to change its address.

### Flags

| Flag | Default | Description |
| --- | --- | --- |
| `-a`, `--agent` | `localhost:8080` | Agent HTTP API address. See [agent address](overview.md#agent-address). |
| `--ipv4` | none | IPv4 address for the name. |
| `--ipv6` | none | IPv6 address for the name. |
| `--comment` | none | Free-text comment shown by `host list`. |

### Output

```text
Host added: target-11.mesh -> 10.8.0.11
```

For a dual-stack entry both addresses are listed, for example `Host added: gateway.mesh -> 10.0.0.1, fd00::1`.

### Examples

```bash
muti-metroo host add target-11.mesh --ipv4 10.8.0.11 --comment "edge worker"
```

```bash
muti-metroo host add gateway.mesh --ipv4 10.0.0.1 --ipv6 fd00::1
```

```bash
muti-metroo host add v6-only.mesh --ipv6 fd00::42 -a 192.168.1.10:8080
```

## host remove

```bash
muti-metroo host remove <name> [flags]
```

Remove a runtime entry. Entries from the configuration cannot be removed at runtime; the command fails with `config-seeded host, cannot remove dynamically` and exit status 5.

### Flags

| Flag | Default | Description |
| --- | --- | --- |
| `-a`, `--agent` | `localhost:8080` | Agent HTTP API address. See [agent address](overview.md#agent-address). |

### Confirmation prompt

```text
Remove mesh host "target-11.mesh"?
Mutiauk clients can no longer resolve this name. Continue? [y/N]:
```

Answering anything other than `y` or `yes` prints `Aborted.` and exits 0. `--yes` skips the prompt; without it, a non-terminal stdin makes the command exit 6. See [confirmation prompts](overview.md#confirmation-prompts).

### Output

```text
Host removed: target-11.mesh
```

### Examples

```bash
muti-metroo host remove target-11.mesh
```

```bash
muti-metroo host remove target-11.mesh -a 192.168.1.10:8080 -y
```

## host list

```bash
muti-metroo host list [flags]
```

List all entries, from the configuration and added at runtime, sorted by name. The output does not mark which entries come from the configuration.

### Flags

| Flag | Default | Description |
| --- | --- | --- |
| `-a`, `--agent` | `localhost:8080` | Agent HTTP API address. See [agent address](overview.md#agent-address). |
| `--json` | `false` | Print the list as JSON. |

### Output

```text
Mesh Hosts (3)
NAME                             IPV4               IPV6                   COMMENT
gateway.mesh                     10.0.0.1           fd00::1
target-11.mesh                   10.8.0.11                                 edge worker
v6-only.mesh                                        fd00::42
```

When the registry is empty, the command prints `No mesh hosts configured`.

With `--json`, empty fields are omitted:

```json
{
  "hosts": [
    {
      "name": "gateway.mesh",
      "ipv4": "10.0.0.1",
      "ipv6": "fd00::1"
    },
    {
      "name": "target-11.mesh",
      "ipv4": "10.8.0.11",
      "comment": "edge worker"
    },
    {
      "name": "v6-only.mesh",
      "ipv6": "fd00::42"
    }
  ]
}
```

### Examples

```bash
muti-metroo host list
```

```bash
muti-metroo host list --json
```

## host suffix add

```bash
muti-metroo host suffix add <suffix> [flags]
```

Activate a TLD suffix so that names ending in it are accepted. A suffix is a single label (`alt`) or several labels (`muti-metroo.alt`), with no leading or trailing dot; each label must be a valid DNS label. Adding a suffix that is already active fails.

### Flags

| Flag | Default | Description |
| --- | --- | --- |
| `-a`, `--agent` | `localhost:8080` | Agent HTTP API address. See [agent address](overview.md#agent-address). |

### Output

The command prints the active suffixes after the change:

```text
Suffix added. Active suffixes: alt, mesh
```

### Examples

```bash
muti-metroo host suffix add alt
```

## host suffix remove

```bash
muti-metroo host suffix remove <suffix> [flags]
```

Deactivate a suffix. Suffixes from `mesh_hosts_suffixes` can be removed too, and the removal is journalled like any other change. Removing a suffix that any entry still uses fails with exit status 5, for example `suffix has registered hosts: alt used by gw.alt`; remove those entries first.

### Flags

| Flag | Default | Description |
| --- | --- | --- |
| `-a`, `--agent` | `localhost:8080` | Agent HTTP API address. See [agent address](overview.md#agent-address). |

### Confirmation prompt

The command prompts only when the suffix is the last active one:

```text
"mesh" is the only active suffix.
Removing it invalidates every existing mesh host. Continue? [y/N]:
```

Declining, `--yes`, and non-terminal stdin behave as for `host remove`.

### Output

```text
Suffix removed. Active suffixes: mesh
```

### Examples

```bash
muti-metroo host suffix remove alt
```

## host suffix list

```bash
muti-metroo host suffix list [flags]
```

List the active suffixes.

### Flags

| Flag | Default | Description |
| --- | --- | --- |
| `-a`, `--agent` | `localhost:8080` | Agent HTTP API address. See [agent address](overview.md#agent-address). |
| `--json` | `false` | Print the list as JSON. |

### Output

```text
Active suffixes (2):
  .alt
  .mesh
```

With `--json`:

```json
{
  "suffixes": [
    "alt",
    "mesh"
  ]
}
```

### Examples

Add a suffix, register a host under it, then remove both:

```bash
muti-metroo host suffix add alt
muti-metroo host add gw.alt --ipv4 10.5.0.1
muti-metroo host remove gw.alt -y
muti-metroo host suffix remove alt
```

## See Also

- [Mesh hostnames](../features/mesh-hostnames.md): how names are served to and resolved by Mutiauk
- [Hosts configuration](../configuration/hosts.md): define `mesh_hosts` and `mesh_hosts_suffixes` in YAML
- [Hosts management API](../api/hosts-management.md): the HTTP endpoints behind these commands
- [Mutiauk](../mutiauk.mdx): the TUN client that resolves mesh hostnames
- [Dynamic config persistence](../configuration/dynamic-config-persistence.md): how runtime changes survive restarts
