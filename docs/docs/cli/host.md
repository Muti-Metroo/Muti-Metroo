# Host Commands

Commands for managing the agent's mesh hostname registry.

The mesh-host registry maps symbolic names like `target-11.mesh` to in-mesh IPs. The map is local to each agent and served to Mutiauk's DNS resolver via the `/api/dashboard` poll, so operators can address peers by name instead of IP. Static seed entries live in the agent's `mesh_hosts:` config block; runtime entries added through the CLI are journalled to `agent.data_dir` (see [Dynamic Config Persistence](/configuration/dynamic-config-persistence)) and replayed on the next start.

The registry is **local-only**: there is no `/agents/{id}/hosts/manage` remote variant. Run the CLI against each agent that should serve names, or bake them into the config.

See [Hosts Configuration](/configuration/hosts) for the YAML schema, [Hosts Management API](/api/hosts-management) for the HTTP shape, and the [Mutiauk guide](/mutiauk#mesh-hostnames) for the consumer-side experience.

## host add

Add a mesh host entry.

```bash
muti-metroo host add <name> [flags]
```

### Description

Registers a runtime hostname with one or both IP families. The name must either equal one of the active suffixes (e.g. `mesh -> 1.2.3.4`) or end with one of them under a label prefix (e.g. `gateway.mesh`); see [host suffix](#host-suffix). Each label between dots must be a valid RFC 1035 label. At least one of `--ipv4` or `--ipv6` is required.

Adding a name that already exists (in either the config seed or the dynamic set) returns an error. Re-running `host add` on an existing dynamic name is rejected; remove and re-add to change the IP.

### Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--agent` | `-a` | `localhost:8080` | Agent API address (host:port or http(s):// URL) |
| `--ipv4` | | | IPv4 address; at least one of `--ipv4` or `--ipv6` is required |
| `--ipv6` | | | IPv6 address; at least one of `--ipv4` or `--ipv6` is required |
| `--comment` | | | Optional free-text comment |

### Examples

```bash
# IPv4-only entry with a comment
muti-metroo host add target-11.mesh --ipv4 10.8.0.11 --comment "edge worker"

# Dual-stack entry
muti-metroo host add gateway.mesh --ipv4 10.0.0.1 --ipv6 fd00::1

# IPv6-only entry on a remote API server
muti-metroo host add v6-only.mesh --ipv6 fd00::42 -a 192.168.1.10:8080
```

### Output

```
Host added: target-11.mesh -> 10.8.0.11
```

For dual-stack entries the IP summary lists both: `gateway.mesh -> 10.0.0.1, fd00::1`.

---

## host remove

Remove a runtime mesh host entry.

```bash
muti-metroo host remove <name> [flags]
```

### Description

Deletes a dynamic entry from the registry. Config-seeded entries (from the agent's `mesh_hosts:` block) are protected: removal returns HTTP 403 with `config-seeded host, cannot remove dynamically`. Edit the config and restart to remove a static entry.

### Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--agent` | `-a` | `localhost:8080` | Agent API address (host:port or http(s):// URL) |

### Examples

```bash
# Remove a runtime entry from the local agent
muti-metroo host remove target-11.mesh

# Remove via a different API server
muti-metroo host remove target-11.mesh -a 192.168.1.10:8080
```

### Output

```
Host removed: target-11.mesh
```

---

## host list

List mesh host entries (config + dynamic, merged).

```bash
muti-metroo host list [flags]
```

### Description

Returns the merged list of config-seeded and runtime entries, sorted by name. Config entries cannot be told apart from dynamic ones in the listing; the distinction matters only when you try to remove (config entries are protected).

### Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--agent` | `-a` | `localhost:8080` | Agent API address (host:port or http(s):// URL) |
| `--json` | | `false` | Output in JSON format |

### Examples

```bash
# Tabular output
muti-metroo host list

# JSON output for scripting
muti-metroo host list --json
```

### Output

Standard output:

```
Mesh Hosts (2)
NAME                             IPV4               IPV6                   COMMENT
gateway.mesh                     10.0.0.1           fd00::1
target-11.mesh                   10.8.0.11                                 edge worker
```

JSON output:

```json
{
  "hosts": [
    {"name": "gateway.mesh", "ipv4": "10.0.0.1", "ipv6": "fd00::1"},
    {"name": "target-11.mesh", "ipv4": "10.8.0.11", "comment": "edge worker"}
  ]
}
```

The HTTP endpoint additionally returns a `suffixes` field for client-side validation; the CLI strips it. Hit [`POST /hosts/manage`](/api/hosts-management) directly when you need the suffix list programmatically.

---

## host suffix

Manage the active TLD suffix set the agent accepts when validating mesh hostnames. The active set starts from `mesh_hosts_suffixes:` in config and can be widened or narrowed at runtime; mutations are journalled and replayed on restart, including removals of suffixes that came from config.

```bash
muti-metroo host suffix add <suffix>
muti-metroo host suffix remove <suffix>
muti-metroo host suffix list [--json]
```

### Description

`add` activates a new suffix. The argument can be a single label (`alt`) or a multi-level domain (`muti-metroo.alt`); each dot-separated piece must be a valid RFC 1035 label, and there must be no leading or trailing dot. Adding a suffix that is already active returns an error.

`remove` deactivates a suffix. Returns an error if the suffix has any registered hosts (config-seeded or dynamic) -- remove the dependent hosts first, then retry. Removes are allowed even on suffixes seeded by config.

`list` prints the post-mutation active set.

### Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--agent` | `-a` | `localhost:8080` | Agent API address (host:port or http(s):// URL) |
| `--json` | | `false` | `list` only: emit JSON |

### Examples

```bash
# Add a new TLD without restarting
muti-metroo host suffix add alt

# Register a host under it
muti-metroo host add gw.alt --ipv4 10.5.0.1

# List the active suffix set
muti-metroo host suffix list

# Remove the TLD (must remove gw.alt first)
muti-metroo host remove gw.alt
muti-metroo host suffix remove alt
```

### Output

```
Suffix added. Active suffixes: alt, mesh
```

```
Active suffixes (2):
  .alt
  .mesh
```

---

## Authorization

The mesh-host registry endpoints honor the standard `http.token_hash` bearer-token gate when configured: pass `--token` or set `MUTI_METROO_TOKEN` so the CLI attaches `Authorization: Bearer ...`. Mutations (`add`, `remove`) require the management signing key when management-key encryption is in effect, following the same rules as `muti-metroo route` and `muti-metroo peer`.

## Important Notes

### Persistence

Runtime entries are appended to the [dynamic config journal](/configuration/dynamic-config-persistence) under `agent.data_dir/dynamic-config.log` and replayed on the next start. Without a writable `data_dir`, runtime entries remain ephemeral and are lost on restart.

### Names Must Resolve to Reachable IPs

The registry only translates names to IPs - it does not install routes. The IP a name resolves to must already be reachable through Muti Metroo (a static `routes:` entry or an autoroute fetched by Mutiauk). Use `mutiauk route trace <name>` from the consumer side to verify both halves.

### Config-Seeded vs Dynamic

Static entries declared in `mesh_hosts:` are immutable at runtime; runtime entries can be freely added and removed. Both are surfaced together in `list`. To change a static entry, edit the config and restart the agent.

### Validation

- Names are lowercased and must either equal one of the active suffixes (default `mesh`) or end with one of them under a label prefix.
- Each label must match RFC 1035 (a-z, 0-9, internal hyphens, 1-63 chars per label, 253 chars total).
- At least one of `--ipv4` or `--ipv6` is required; both can be set for dual-stack.
- Each suffix can be a single label or a multi-level domain (`muti-metroo.alt`); every dot-separated piece must be a valid RFC 1035 label, no leading or trailing dot.
