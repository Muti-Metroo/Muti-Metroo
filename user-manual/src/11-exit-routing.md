# Exit Routing

Exit nodes advertise routes and open TCP connections to external destinations. Two types of routes are supported:

- **CIDR routes**: Match destinations by IP address (e.g., `10.0.0.0/8`)
- **Domain routes**: Match destinations by domain name (e.g., `*.example.com`)

**Note:** Domain routes only work with **SOCKS5 clients** that send the destination hostname, such as `curl -x socks5h://...` pointed directly at an ingress agent's SOCKS5 listener. Mutiauk, the usual operator entry point, operates at Layer 3 (IP) and only sees IP addresses after DNS resolution, so it can only use **CIDR routes**. Mutiauk's autoroutes feature installs only CIDR routes.

## Configuration

```yaml
exit:
  enabled: true
  # routes accepts either a bare CIDR string or a structured mapping
  # with optional metric and an operator comment (comments are local-only
  # metadata; they are NOT advertised to peers).
  routes:
    - "10.0.0.0/8"
    - cidr: "192.168.0.0/16"
      comment: "office LAN"
    - cidr: "0.0.0.0/0" # Default route
      metric: 5
  domain_routes:
    - "api.internal.corp" # Exact domain match
    - "*.example.com" # Wildcard match
```

`dns.servers` is intentionally omitted from the headline example: the
default `[]` delegates resolution to the agent host's stub resolver, which
already handles `.local` / mDNS, split-horizon DNS, and any operator-configured
upstreams. See [DNS Configuration](#dns-configuration) below before adding an
explicit `dns:` block.

## Route Types

### CIDR Routes

CIDR routes match destinations by IP address. DNS resolution happens at the **ingress** agent.

```yaml
exit:
  routes:
    - "10.0.0.0/8" # Private network
    - "192.168.0.0/16" # Another private range
    - "0.0.0.0/0" # Default route (all traffic)
```

**Flow:**

1. Client connects via SOCKS5 with domain (e.g., `example.com`)
2. Ingress agent resolves domain using system DNS
3. Ingress performs route lookup using the resolved IP
4. Stream opens to exit node with the **IP address**
5. Exit opens TCP connection to the destination IP

#### Egress interface

On a multi-homed exit, a route can pin the egress network interface for its
outbound dials. Set `interface:` to the NIC name:

```yaml
exit:
  routes:
    - cidr: "10.10.0.0/16"
      interface: "ens192" # dial this CIDR out ens192
```

How the binding is applied depends on the source IP and the platform:

- **No `source_ip`, or a `source_ip` already assigned to the interface**: the
  exit binds the dial to an address on that interface
  (`net.Dialer.LocalAddr`). This is portable and unprivileged: it works on
  Linux, macOS, and Windows without root. The source _address_ is set; the
  egress device then follows the host routing table.
- **A `source_ip` that is _not_ assigned to the interface**: e.g. a
  floating/anycast address that lives on loopback while packets must leave via
  `ens192`. This requires
  `SO_BINDTODEVICE` + `IP_FREEBIND` and is therefore **Linux + root only**. On
  macOS/Windows, or on Linux as a non-root user, an off-interface `source_ip`
  paired with `interface:` is rejected with a clear error.

Like `source_ip`, `interface` is **local-only**; it is never advertised to
other agents. The same field is accepted on `POST /routes/manage` and as
`--interface` on `muti-metroo route add`; the dedicated `muti-metroo route interface <cidr>
<name>` action sets or clears it on a config-seeded or dynamic route at runtime
(pass an empty string to clear). Runtime values are journalled to the dynamic
config and restored on restart.

### Domain Routes

Domain routes match destinations by domain name pattern. DNS resolution happens at the **exit** agent.

```yaml
exit:
  domain_routes:
    - "api.internal.corp" # Exact match
    - "*.example.com" # Wildcard match
```

**Flow:**

1. Client connects via SOCKS5 with domain (e.g., `api.internal.corp`)
2. Ingress checks domain routes first
3. If matched, stream opens to exit node with the **domain name**
4. Exit agent resolves domain using its configured DNS servers
5. Exit opens TCP connection to the resolved IP

**When to use domain routes:**

- **Split-horizon DNS**: Internal domains resolve differently inside vs. outside the network
- **Private services**: Route `*.internal.corp` to an exit with access to internal DNS
- **Geo-specific resolution**: Different DNS results based on exit location

## Route Selection

### Domain Route Priority

Domain routes are checked **first** for domain-based requests:

1. **Exact match**: `api.example.com` matches only `api.example.com`
2. **Wildcard match**: `*.example.com` matches `foo.example.com`, `bar.example.com`
3. If no domain route matches, fall back to CIDR routing

Wildcard matching is **single-level only**:

- `*.example.com` matches `foo.example.com`
- `*.example.com` does NOT match `a.b.example.com` or `example.com`

### CIDR Route Selection

Uses longest-prefix match:

1. Filter routes where CIDR contains destination IP
2. Select route with longest prefix (most specific)
3. If tied, select lowest metric (hop count)

Example with multiple exits:

- Exit A: `1.2.3.4/32` (metric 3) - Wins for `1.2.3.4`
- Exit B: `1.2.3.0/24` (metric 2) - Wins for `1.2.3.5`
- Exit C: `0.0.0.0/0` (metric 1) - Wins for everything else

## Route Advertisement

Routes are propagated through the mesh automatically:

- **Periodic**: Every `routing.advertise_interval` (default 2 minutes)
- **On-demand**: Via HTTP API

### Trigger Immediate Advertisement

```bash
curl -X POST http://localhost:8080/routes/advertise
```

This is useful after configuration changes to speed up route propagation.

## DNS Configuration

The exit node's DNS resolver has two modes:

1. **System resolver (default, `dns.servers: []`).** Uses the host's stub
   resolver (`/etc/resolv.conf` on Linux, `scutil` / `mDNSResponder` on
   macOS, `Dnsapi.dll` on Windows). This automatically handles split-horizon
   DNS, `.local` / mDNS, search domains, and any internal resolvers the
   host operator has already configured. This is the recommended setting
   for hospital, spacecraft, and compliance-bound deployments.

2. **Explicit upstream servers.** Set `dns.servers` to a list of
   `host:port` entries to bypass the host resolver and query the listed
   upstreams directly.

> **Warning:** Public resolvers such as `8.8.8.8` (Google) and `1.1.1.1`
> (Cloudflare) leak every resolved name and the exit's source IP to a
> third party. Do **not** paste them into a config that handles
> protected health information, ITAR-controlled data, classified
> traffic, or any deployment where DNS metadata is sensitive. Prefer the
> host's resolver, an operator-controlled recursive resolver, or DNS
> over TLS to your own infrastructure.

Example: pin a private resolver inside the routed network.

```yaml
exit:
  dns:
    servers:
      - "10.0.0.1:53" # Internal DNS server reachable via the exit
    timeout:
      5s # Per-query timeout (default 5s; only used when
      # servers is non-empty)
```

## Access Control

Only destinations matching advertised routes are allowed:

```yaml
exit:
  routes:
    - "10.0.0.0/8" # Only allow 10.x.x.x
```

Connections to other IPs will be rejected with "no route to host".

## Example Configurations

### Internet Gateway

Route all traffic to the internet. The exit uses the host's system resolver
by default. Only add an explicit `dns:` block if you need to force
particular upstreams, and never paste public DNS (`8.8.8.8`, `1.1.1.1`)
into a compliance-bound deployment (see warning above).

```yaml
exit:
  enabled: true
  routes:
    - "0.0.0.0/0"
```

### Internal Network Access

Route only to internal networks:

```yaml
exit:
  enabled: true
  routes:
    - "10.0.0.0/8"
    - "172.16.0.0/12"
    - "192.168.0.0/16"
  dns:
    servers:
      - "10.0.0.1:53"
```

### Split-Horizon DNS

Route internal domains with internal DNS resolution:

```yaml
exit:
  enabled: true
  routes:
    - "10.0.0.0/8"
  domain_routes:
    - "*.internal.corp"
    - "*.corp.local"
  dns:
    servers:
      - "10.0.0.1:53"
```

## Verifying Routes

Check advertised routes on any agent:

```bash
# View live route table (JSON) - /healthz returns counts only
curl http://localhost:8080/api/dashboard | jq '.routes'

# Via CLI (live state)
muti-metroo route status -a localhost:8080

# Configured + dynamic view (does not require routes to be currently active)
muti-metroo route list -a localhost:8080
```

To explain which route the agent would pick for a specific destination, use `muti-metroo route trace`:

```bash
muti-metroo route trace 8.8.8.8
```

The output lists every matching CIDR with the winner and the reason each loser lost (`shorter prefix`, `higher metric`, or `lost origin tiebreaker`). Hostnames are resolved through the selected agent's resolver and also checked against the domain route table.

## Mesh-Local Hostnames

Each Muti Metroo agent can publish a registry of symbolic names that map to in-mesh IPs (for example `target-11.mesh -> 10.8.0.11`). Static seed entries live in the agent's `mesh_hosts:` block; runtime entries added through `muti-metroo host` or the `/hosts/manage` HTTP endpoint are journalled to `agent.data_dir` and replayed on the next start. The merged list is exposed via `/api/dashboard` and pushed to Mutiauk over the SOCKS5 route-watch stream, so Mutiauk's in-process DNS server picks it up whenever Mutiauk's autoroutes feature is enabled (Chapter 10).

The registry only translates names to IPs - it does not install any routes. The IP a name resolves to must already be reachable through Muti Metroo via a static route or autoroute; otherwise the name resolves but traffic falls back to the kernel's default interface.

The agent itself uses the registry too: SOCKS5 dial (TCP and UDP ASSOCIATE) and `POST /route/trace` consult the same map before falling through to system DNS, so registered names like `gateway.mesh` work even when no Mutiauk is running on the agent's host.

### Configuration

```yaml
mesh_hosts_suffixes: ["mesh"] # optional, defaults to ["mesh"]; single label (mesh) or multi-level (muti-metroo.alt)
mesh_hosts:
  - name: gateway.mesh
    ipv4: 10.0.0.1
    ipv6: fd00::1
  - name: target-11.mesh
    ipv4: 10.8.0.11
    comment: "edge worker"
```

Mutiauk receives the active suffix list from the agent over the SOCKS5 route-watch stream whenever it changes, so operators do not configure suffixes on the Mutiauk side; they only need `autoroutes.enabled: true` in the Mutiauk config (autoroutes is off by default). Each `name` must either equal an active suffix (e.g. `mesh -> 1.2.3.4`) or end with one under a label prefix; every dot-separated piece must be a valid RFC 1035 label, and at least one of `ipv4` / `ipv6` is required. The active set can be widened or narrowed at runtime with `muti-metroo host suffix add|remove|list`; mutations are journalled and survive restart.

### CLI

```bash
# Add a runtime entry
muti-metroo host add target-11.mesh --ipv4 10.8.0.11 --comment "edge worker"

# Dual-stack
muti-metroo host add gateway.mesh --ipv4 10.0.0.1 --ipv6 fd00::1

# List (config + dynamic, merged)
muti-metroo host list
muti-metroo host list --json

# Remove a runtime entry (config-seeded entries are protected)
muti-metroo host remove target-11.mesh
```

Config-seeded entries cannot be removed at runtime - the API returns 403. Edit the config and restart the agent instead. Runtime entries persist across restarts when `agent.data_dir` is configured. Verify end-to-end reachability from the consumer side with `mutiauk route trace target-11.mesh`; the trace's `Resolved:` line marks names from the mesh registry as `(via mesh DNS)`.

### HTTP API

`POST /hosts/manage` with a JSON body of `{action, name, ipv4, ipv6, comment}` adds, removes, or lists entries. Every response returns `{suffix, hosts[]}` so callers can validate names client-side. There is no `/agents/{id}/hosts/manage` remote variant - run the request directly against each agent that should serve names. See the HTTP API chapter for full request/response details.

## Dynamic Route Management

Dynamic routes can be added and removed at runtime via CLI or HTTP API without restarting the agent. When `agent.data_dir` is set they are persisted to the dynamic config journal and survive restarts; without a `data_dir` (or when the journal is not writable) they remain ephemeral and are lost on restart. For always-on static routes, use configuration file routes under `exit.routes`. See the "Dynamic Config Persistence" section of the Configuration chapter for details.

### CLI Usage

```bash
# Add a CIDR route with default metric
muti-metroo route add 10.0.0.0/8

# Add a route with custom metric
muti-metroo route add 192.168.0.0/16 --metric 5

# Pin this CIDR's outbound dials to a specific local IP on the exit
# (multi-homed hosts; equivalent to `curl --interface 192.168.1.50`)
muti-metroo route add 10.10.0.0/16 --source-ip 192.168.1.50

# Pin this CIDR's egress network interface
muti-metroo route add 10.10.0.0/16 --interface ens192

# List all routes
muti-metroo route list

# Remove a route
muti-metroo route remove 10.0.0.0/8

# Set or clear the egress interface on an existing route (empty string clears)
muti-metroo route interface 10.10.0.0/16 ens192
muti-metroo route interface 10.10.0.0/16 ""

# Explain which route would be picked for a destination
muti-metroo route trace 8.8.8.8
```

### Remote Agent Management

Add routes to remote agents via the CLI:

```bash
# Add route to specific agent
muti-metroo route add 10.0.0.0/8 --target abc123 -a 192.168.1.10:8080

# List routes on remote agent
muti-metroo route list --target abc123 -a 192.168.1.10:8080
```

### HTTP API

Manage routes directly via the HTTP API:

```bash
# Add a route
curl -X POST http://localhost:8080/routes/manage \
  -H "Content-Type: application/json" \
  -d '{"action":"add","network":"10.0.0.0/8"}'

# Add with custom metric
curl -X POST http://localhost:8080/routes/manage \
  -H "Content-Type: application/json" \
  -d '{"action":"add","network":"192.168.0.0/16","metric":5}'

# List routes
curl -X POST http://localhost:8080/routes/manage \
  -H "Content-Type: application/json" \
  -d '{"action":"list"}'

# Remove a route
curl -X POST http://localhost:8080/routes/manage \
  -H "Content-Type: application/json" \
  -d '{"action":"remove","network":"10.0.0.0/8"}'
```

### Signing Key Authorization

Route mutations (`add`, `remove`, `set_comment`) are authenticated using the mesh management **signing key** (Ed25519, configured under `management.signing_public_key` / `signing_private_key`). The same key authorizes every privileged operation in the mesh: management commands, file transfer, all dynamic mutations, and sleep/wake.

- Agents configured with `signing_public_key` reject unsigned or incorrectly signed mutations (HTTP 403).
- Operator nodes need the matching `signing_private_key` to issue mutations; the local API also gates on this and refuses to forward an unsigned request to a remote agent.
- Each mutation envelope binds the operation tag (e.g. `route.add`) and a body hash, so a captured signature cannot be replayed as a different operation or against a modified payload.

This is distinct from the **management encryption key** (X25519, `management.public_key` / `private_key`), which compartmentalizes topology visibility. The two keys serve different purposes; you can use one, both, or neither. See chapter 21 for full management-key details.

### Transit-to-Exit Promotion

When a dynamic route is added to a transit agent (one that does not have `exit.enabled: true`), the agent is automatically promoted to an exit node for that specific route. The route will be advertised through the mesh, and the agent will handle TCP connections for destinations matching the CIDR.
