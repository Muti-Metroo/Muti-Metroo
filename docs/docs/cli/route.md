# Route Commands

Commands for managing dynamic CIDR exit routes.

`route status` and `route list` answer two different questions:

- `route status` -> "where would my traffic actually go?" (live route table the agent uses right now: next-hop, origin, metric, hop count). Replaces the deprecated top-level `muti-metroo routes`.
- `route list` -> "what routes does my configuration define?" (config-seeded + dynamic; use `--all` to include config-seeded entries).

## route status

```bash
muti-metroo route status [flags]
```

Fetch the live route table from `/api/dashboard` -- equivalent to the deprecated top-level `muti-metroo routes`. Use `--json` for machine-readable output.

## route add

Add a dynamic CIDR exit route.

```bash
muti-metroo route add <cidr> [flags]
```

### Description

Adds a new exit route for the specified CIDR range. The route is added to the target agent's exit route table and advertised to the mesh.

Dynamic routes persist across restarts when `agent.data_dir` is configured (see [Dynamic Config Persistence](/configuration/dynamic-config-persistence)); when no `data_dir` is set they remain ephemeral. For always-on static routes, use the `exit.routes` configuration.

### Flags

| Flag          | Short | Default          | Description                                                                                                                                                                                         |
| ------------- | ----- | ---------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `--agent`     | `-a`  | `localhost:8080` | Agent API address (host:port or http(s):// URL)                                                                                                                                                     |
| `--target`    | `-t`  |                  | Target agent ID (omit for local agent)                                                                                                                                                              |
| `--metric`    | `-m`  | `0`              | Route metric                                                                                                                                                                                        |
| `--comment`   |       |                  | Operator comment. Propagates with the route advertisement so other agents see the same text alongside the CIDR.                                                                                     |
| `--source-ip` |       |                  | Local IP the exit binds outbound dials to for this CIDR (equivalent to `curl --interface`). Local-only; not advertised. Must already be bound on the exit host and match the CIDR's address family. |

### Examples

```bash
# Add route on local agent
muti-metroo route add 10.0.0.0/24

# Add route with metric and comment
muti-metroo route add 10.0.0.0/24 -m 5 --comment "office LAN"

# Pin outbound dials for this CIDR to a specific local IP on the exit
muti-metroo route add 10.10.0.0/16 --source-ip 192.168.1.50

# Add route on remote agent
muti-metroo route add 10.0.0.0/24 -t abc123def456

# Add route using short agent ID prefix
muti-metroo route add 10.0.0.0/24 -t abc123

# Via a specific API server
muti-metroo route add 10.0.0.0/24 -a 192.168.1.10:8080 -t def456
```

### Output

```
Route added: 10.0.0.0/24 (metric 0)
```

### Use Cases

- **Transit to Exit Promotion**: Convert a transit-only agent to an exit agent on the fly
- **Dynamic Route Injection**: Add routes based on runtime conditions or automation
- **Testing**: Temporarily expose routes without modifying configuration files
- **Emergency Routing**: Add fallback routes during network issues

---

## route remove

Remove a dynamic CIDR exit route.

```bash
muti-metroo route remove <cidr> [flags]
```

### Description

Removes a previously added dynamic exit route. The route is removed from the target agent's exit route table and a per-route `RouteWithdraw` is flooded to all connected peers immediately, so they prune the route from their tables without waiting for `route_ttl` to elapse.

Only dynamic routes can be removed via this command. Routes defined in the `exit.routes` configuration are persistent and cannot be removed without restarting the agent.

### Flags

| Flag       | Short | Default          | Description                                     |
| ---------- | ----- | ---------------- | ----------------------------------------------- |
| `--agent`  | `-a`  | `localhost:8080` | Agent API address (host:port or http(s):// URL) |
| `--target` | `-t`  |                  | Target agent ID (omit for local agent)          |

### Examples

```bash
# Remove route from local agent
muti-metroo route remove 10.0.0.0/24

# Remove route from remote agent
muti-metroo route remove 10.0.0.0/24 -t abc123def456

# Remove route using short agent ID prefix
muti-metroo route remove 10.0.0.0/24 -t abc123

# Via a specific API server
muti-metroo route remove 10.0.0.0/24 -a 192.168.1.10:8080 -t def456
```

### Output

```
Route removed: 10.0.0.0/24
```

---

## route comment

Set or clear the operator comment on a CIDR exit route.

```bash
muti-metroo route comment <cidr> <text> [flags]
```

### Description

Updates the operator-visible comment on either a config-seeded or
dynamically-added route. Pass an empty string for `<text>` to clear the
comment. The change is journalled to `data_dir/dynamic-config.log` and
overrides the value from the config file -- the override survives agent
restart.

Comments propagate alongside the route advertisement, so other agents see
the same text when they receive the route. They appear in `muti-metroo route
list`, the Muti Metroo Manager dashboard, and `muti-metroo route trace` regardless of
which agent originally set them.

### Flags

| Flag       | Short | Default          | Description                                     |
| ---------- | ----- | ---------------- | ----------------------------------------------- |
| `--agent`  | `-a`  | `localhost:8080` | Agent API address (host:port or http(s):// URL) |
| `--target` | `-t`  |                  | Target agent ID (omit for local agent)          |

### Examples

```bash
# Set a comment on a config-seeded route (overrides the YAML value)
muti-metroo route comment 10.0.0.0/8 "internal corp network"

# Clear a comment
muti-metroo route comment 10.0.0.0/8 ""

# Update on a remote agent
muti-metroo route comment 10.0.0.0/8 "renamed" -t abc123
```

### Output

```
Route comment updated: route 10.0.0.0/8 comment updated
```

---

## route source-ip

Set or clear the local source IP an exit binds outbound dials to for a CIDR
(equivalent to `curl --interface`).

```bash
muti-metroo route source-ip <cidr> <ip> [flags]
```

### Description

Updates the source IP binding on either a config-seeded or dynamically-added
route. Pass an empty string for `<ip>` to clear the binding. The change is
journalled to `data_dir/dynamic-config.log` and overrides the value from the
config file -- the override survives agent restart.

The source IP must already be bound on the exit host and match the CIDR's
address family. Unlike the comment, the source IP is **local-only** -- it is
never advertised to other agents because it is only meaningful on the host
that owns the address.

### Flags

| Flag       | Short | Default          | Description                                     |
| ---------- | ----- | ---------------- | ----------------------------------------------- |
| `--agent`  | `-a`  | `localhost:8080` | Agent API address (host:port or http(s):// URL) |
| `--target` | `-t`  |                  | Target agent ID (omit for local agent)          |

### Examples

```bash
# Pin outbound dials for 10.10.0.0/16 to a specific local IP
muti-metroo route source-ip 10.10.0.0/16 192.168.1.50

# Change the binding without removing the route
muti-metroo route source-ip 10.10.0.0/16 192.168.1.60

# Clear the binding (fall back to the OS default source)
muti-metroo route source-ip 10.10.0.0/16 ""

# Update on a remote agent
muti-metroo route source-ip 10.10.0.0/16 192.168.1.50 -t abc123
```

### Output

```
Route source IP updated: route 10.10.0.0/16 source_ip updated
```

---

## route interface

Set or clear the egress network interface an exit binds outbound dials to for a
CIDR.

```bash
muti-metroo route interface <cidr> <name> [flags]
```

### Description

Pins the egress NIC for outbound dials matching this CIDR. Pass an empty string
for `<name>` to clear the binding. The change is journalled to
`data_dir/dynamic-config.log` and overrides the config-file value.

When the route has no `source_ip`, or a `source_ip` already assigned to the
interface, the exit binds to an address on that interface
(`net.Dialer.LocalAddr`). This is portable and **unprivileged** -- it works on
Linux, macOS, and Windows without root; the egress device then follows the host
routing table.

When the route's `source_ip` is _not_ assigned to the interface -- a
loopback-bound floating address forced out a physical NIC -- the exit uses the
privileged `SO_BINDTODEVICE` +
`IP_FREEBIND` path, which is **Linux + root only**. On macOS/Windows, or on
Linux as a non-root user, that off-interface combination is rejected at
validation time with a clear error. On Linux + root the interface is honored via
`SO_BINDTODEVICE` regardless of the source.

Like `source_ip`, the interface binding is **local-only** and is never
advertised to other agents.

### Flags

| Flag       | Short | Default          | Description                                     |
| ---------- | ----- | ---------------- | ----------------------------------------------- |
| `--agent`  | `-a`  | `localhost:8080` | Agent API address (host:port or http(s):// URL) |
| `--target` | `-t`  |                  | Target agent ID (omit for local agent)          |

### Examples

```bash
# Pin outbound dials for 10.10.0.0/16 to ens192 (e.g. when 10.10.x.x's source
# IP is a loopback-bound floating address)
muti-metroo route interface 10.10.0.0/16 ens192

# Clear the binding
muti-metroo route interface 10.10.0.0/16 ""

# Combine with --source-ip on add to pin both source IP and egress interface
muti-metroo route add 10.10.0.0/16 --source-ip 192.0.2.10 --interface ens192
```

### Output

```
Route interface updated: route 10.10.0.0/16 interface updated
```

---

## route list

List CIDR exit routes.

```bash
muti-metroo route list [flags]
```

### Description

By default, displays all dynamic (runtime-added) exit routes currently active on the target agent. Routes configured in `exit.routes` are not shown.

With `--all`, displays both config-seeded and dynamic routes, including any operator comments set or overridden via the management API. The `KIND` column distinguishes them.

### Flags

| Flag       | Short | Default          | Description                                          |
| ---------- | ----- | ---------------- | ---------------------------------------------------- |
| `--agent`  | `-a`  | `localhost:8080` | Agent API address (host:port or http(s):// URL)      |
| `--target` | `-t`  |                  | Target agent ID (omit for local agent)               |
| `--json`   |       | `false`          | Output in JSON format                                |
| `--all`    |       | `false`          | Include config-seeded routes (default: dynamic only) |

### Examples

```bash
# List dynamic routes on local agent
muti-metroo route list

# Include config-seeded routes (and their comments) too
muti-metroo route list --all

# List routes on remote agent
muti-metroo route list -t abc123def456

# JSON output
muti-metroo route list --all --json
```

### Output

Default (dynamic only):

```
Dynamic Routes (2)
NETWORK                  METRIC  COMMENT
10.0.0.0/24              0       office LAN
192.168.1.0/24           5
```

With `--all`:

```
Local Routes (3)
NETWORK                  METRIC  KIND     COMMENT
10.0.0.0/24              0       dynamic  office LAN
10.0.0.0/8               0       config   internal corp (operator override)
192.168.1.0/24           5       dynamic
```

JSON output:

```json
{
  "status": "ok",
  "routes": [
    {
      "network": "10.0.0.0/24",
      "metric": 0,
      "dynamic": true,
      "comment": "office LAN"
    },
    {
      "network": "192.168.1.0/24",
      "metric": 5,
      "dynamic": true
    }
  ]
}
```

---

## route trace

Explain which route this agent would pick for a destination.

```bash
muti-metroo route trace <ip|hostname> [flags]
```

### Description

Runs the same route selection logic used by the live stream open path against the supplied destination, and prints every matching CIDR route along with which one would win and why the rest lost.

Accepts either an IP address or a hostname. Hostnames are resolved on the selected agent in this order: literal-IP shortcut, domain route table, mesh-host registry (entries from `mesh_hosts:` and `/hosts/manage` under any active suffix), and finally system DNS. The output reports which source was used. Registered names trace correctly even on agents whose host has no Mutiauk forwarding the suffix.

The winner is chosen by longest prefix, then lowest metric, then lowest origin ID. Losers are annotated with the reason (`shorter prefix`, `higher metric`, or `lost origin tiebreaker`).

### Flags

| Flag       | Short | Default          | Description                                     |
| ---------- | ----- | ---------------- | ----------------------------------------------- |
| `--agent`  | `-a`  | `localhost:8080` | Agent API address (host:port or http(s):// URL) |
| `--target` | `-t`  |                  | Target agent ID (omit for local agent)          |
| `--json`   |       | `false`          | Output in JSON format                           |

### Examples

```bash
# Trace a single IP on the local agent
muti-metroo route trace 8.8.8.8

# Trace a hostname on a remote agent
muti-metroo route trace example.com -t abc123

# JSON output for scripting
muti-metroo route trace 10.1.2.3 --json
```

### Output

```
Trace for 8.8.8.8
    -> 8.8.8.0/24           metric=2 hops=1 next_hop=gateway (ab12cd) origin=gateway (ab12cd)
       0.0.0.0/0            metric=1 hops=1 next_hop=gateway (ab12cd) origin=gateway (ab12cd)  [shorter prefix]
```

For a hostname:

```
Trace for example.com
  Domain route: no match
  Resolved: 93.184.215.14 (via system DNS)

  93.184.215.14:
    -> 93.184.215.0/24      metric=2 hops=1 next_hop=exit (cd34ef) origin=exit (cd34ef)
```

The `Resolved:` line carries one of three suffixes: `(via mesh DNS)` for names found in the agent's mesh-host registry, `(via system DNS)` for names resolved via the host resolver, or `(literal IP)` if the input was already an address. The JSON output reports the same in a `resolution` field with values `"mesh"`, `"system"`, or `"ip"`.

### Authorization

Route trace is a read-only diagnostic. It does not require a management key signature.

When the mesh is configured with management key encryption and the local agent lacks the private key, trace is rejected with HTTP 403 -- the same restriction applied to other topology-exposing endpoints.

---

## Authorization

### Management Key Restriction

Dynamic route modifications are restricted when the mesh is configured with management key encryption.

If an agent has `management.public_key` configured but does NOT have the corresponding `management.private_key`, route add/remove/comment commands are rejected with HTTP 403 Forbidden. This provides compartmentalization -- field agents (with only the public key) cannot modify routes, while operator nodes (with both keys) can.

```yaml
# Field agent config (cannot manage routes)
management:
  public_key: "hex-encoded-public-key"

# Operator node config (can manage routes)
management:
  public_key: "hex-encoded-public-key"
  private_key: "hex-encoded-private-key"
```

If no management key is configured at all, route management is unrestricted.

### Generating Management Keys

```bash
# Generate keypair (outputs both public and private keys in hex)
muti-metroo management-key generate

# Derive public key from an existing private key
muti-metroo management-key public
# (interactive prompt for private key in hex)

# Or provide via flag
muti-metroo management-key public --private <hex-private-key>
```

---

## Important Notes

### Persistence

Dynamic routes survive agent restarts when `agent.data_dir` is configured: every add/remove is appended to a [dynamic config journal](/configuration/dynamic-config-persistence) under `data_dir/dynamic-config.log` and replayed on the next start.

If no `data_dir` is configured, or the journal file cannot be written (bad permissions, read-only filesystem, disk full), the runtime mutation still succeeds but remains ephemeral.

For routes that must always be present, use the `exit.routes` configuration in `config.yaml`:

```yaml
exit:
  routes:
    - 10.0.0.0/24
    - 192.168.1.0/24
```

### Route Propagation

After adding or removing a route, the change is immediately advertised to all connected peers. No manual action is required.

To trigger a route advertisement at any other time (e.g., after configuration changes), use:

```bash
curl -X POST http://localhost:8080/routes/advertise
```

### Short Agent ID Prefixes

The `--target` flag accepts short agent ID prefixes. If multiple agents match the prefix, the command fails. Provide a longer prefix to disambiguate.

```bash
# Agent IDs: abc123def456, abc789ghi012

# Ambiguous
muti-metroo route add 10.0.0.0/24 -t abc
# Error: multiple agents match prefix

# Specific
muti-metroo route add 10.0.0.0/24 -t abc123
# Success
```

---

## Workflow Examples

### Promote Transit Agent to Exit

Convert a transit-only agent to an exit agent at runtime:

```bash
# Add default route
muti-metroo route add 0.0.0.0/0 -t abc123def456

# Verify route
muti-metroo route list -t abc123def456
```

### Automation Script

Add routes based on cloud provider metadata:

```bash
#!/bin/bash
# Detect VPC CIDR and advertise it
VPC_CIDR=$(curl -s http://169.254.169.254/latest/meta-data/network/interfaces/macs/$(curl -s http://169.254.169.254/latest/meta-data/mac)/vpc-ipv4-cidr-block)
muti-metroo route add "$VPC_CIDR"
```

### Testing Exit Routing

Temporarily add a route for testing:

```bash
# Add test route
muti-metroo route add 10.99.0.0/16 -t testnode

# Test connectivity
curl --socks5 localhost:1080 http://10.99.1.1/test

# Remove route
muti-metroo route remove 10.99.0.0/16 -t testnode
```

### Emergency Fallback

Add a backup route during network issues:

```bash
# Primary exit agent down, add fallback
muti-metroo route add 0.0.0.0/0 -t backup-exit -m 100

# When primary recovers, remove fallback
muti-metroo route remove 0.0.0.0/0 -t backup-exit
```
