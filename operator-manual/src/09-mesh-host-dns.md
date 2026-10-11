# Use Case: Mesh-Host Names and DNS

In any deployment that lasts more than a day, operators end up giving names to the boxes they care about. `target-11.mesh` is easier to remember than `10.8.0.11`, and the name does not break when the IP changes. Muti Metroo supports this end-to-end: a small registry on the agent, a TLD whitelist, a local resolver in Mutiauk, and host-side wiring so any tool on the workstation can resolve the names.

## What You Get

After this chapter:

- You can register `target-11.mesh -> 10.8.0.11` in Muti Metroo Manager.
- Every Mutiauk instance picks it up within seconds (pushed over the SOCKS5 stream).
- Any tool on the workstation -- not just SOCKS-aware ones -- can resolve it: `ssh op@target-11.mesh`, `curl http://target-11.mesh/`, `ping target-11.mesh`.
- Removing the entry in the dashboard removes it everywhere.

The setup is once-per-deployment for the suffix list, then registry add/remove during day-to-day work.

## Step 1: Pick a Suffix

Default is `.mesh`. If your team prefers something else (`.lab`, `.corp.local`), add it now in Muti Metroo Manager -> **DNS** -> **TLDs**.

A few rules to follow:

- **Multi-label suffixes only.** The agent's hostname validation rejects single-label bare names like `mesh` (with no leading label). Use `target.mesh`, `target.corp.local`, or any other dotted form. Bare-suffix entries like `mesh.alt` (a name that _is_ the multi-label suffix itself) are accepted; a registration whose name is exactly `mesh` is not.
- **Avoid suffixes that resolve on the public internet.** Public TLDs (`example.com`) will produce confusing failures when something falls back to public DNS. `.mesh` is a safe default -- it is not a public TLD.
- **Be consistent.** A team that uses both `.mesh` and `.lab` will eventually wonder why a host is unreachable from one workstation; pick one.

## Step 2: Register a Host

In Muti Metroo Manager, **DNS** -> **Hosts** -> **Add host**:

| Field   | Value                                           |
| ------- | ----------------------------------------------- |
| Name    | `target-11.mesh` (must end in an active suffix) |
| IPv4    | `10.8.0.11`                                     |
| IPv6    | `fd00::11` (optional)                           |
| Comment | `edge box, project Q2-26`                       |

Save. The entry is journalled to the agent's data directory and propagates to every connected agent on the next route advertisement. Mutiauk instances receive the mesh-host registry over the SOCKS5 route-watch stream (pushed, not polled; it arrives within seconds) and load it into their local DNS cache.

![DNS tab listing registered mesh hostnames](./12-domains-tab.png)

## Step 3: Confirm It on the Workstation

```bash
sudo mutiauk dns status
```

The output reports the active backend, where the local resolver is bound, the active TLDs, the number of registered hosts, and live probes:

- **Backend:** `resolvectl`, `nm-dnsmasq`, or `none`. The first two mean the host's resolver is wired to forward `.mesh` (or whatever your suffix is) queries to the local resolver. `none` means it is not.
- **Active TLDs:** the list of suffixes the workstation has picked up from the agent. Expect to see your suffix here within seconds.
- **Probes:** live tests that query a sentinel name (`mutiauk-status-probe.<suffix>`) two ways -- `resolver direct` (the daemon's in-process resolver) and `system stub` (whatever the host normally uses). `[OK] ... -> NXDOMAIN` on both is the healthy state: the resolver is reachable and answers, and the probe name is intentionally not in the registry. `SERVFAIL`, a timeout, or a mismatched verdict between the two probes means the wiring is broken.

To see the actual entries:

```bash
mutiauk host list

NAME              IPV4         IPV6      COMMENT
target-11.mesh    10.8.0.11    fd00::11  edge box, project Q2-26
```

Then resolve it from any tool:

```bash
getent hosts target-11.mesh
host target-11.mesh
ping -c 1 target-11.mesh
ssh op@target-11.mesh
curl http://target-11.mesh/
```

All of these work without proxy configuration, because the host's resolver knows to forward `.mesh` queries to Mutiauk's local resolver, which answers from the cache.

## How the DNS Wiring Works

You do not normally need to know this, but the pieces help when something is off:

- The Muti Metroo agent maintains the registry and the active suffix list, and still exposes both for inspection.
- Mutiauk receives the registry and suffix list over the SOCKS5 route-watch stream (pushed; it does not poll the agent's API).
- Mutiauk runs an in-process DNS resolver bound to the TUN interface IPv4 address on UDP/TCP 53.
- The host integration tells the system resolver to forward queries for the active suffixes to that local resolver:
  - **`resolvectl`** (used when systemd-resolved is present): per-link DNS config; suffix changes propagate live without reload.
  - **`nm-dnsmasq`** (used when NetworkManager is configured with `dns=dnsmasq`): Mutiauk writes `/etc/NetworkManager/dnsmasq.d/mutiauk.conf` and reloads NetworkManager when the suffix set changes.
  - **`none`:** the resolver is reachable directly at the TUN IP, but nothing on the host forwards to it.

`dns_integration: auto` (default) picks the first available backend in that order and falls back to `none` if neither is present. Force one with `dns_integration: resolvectl|nm-dnsmasq|none` in the workstation config if the auto-detection guesses wrong.

On graceful shutdown -- and `sudo mutiauk cleanup` -- the host integration is reverted, so DNS resolution returns to whatever it was before.

### When Auto-Detection Picks `none`

A handful of stripped-down or unusual workstation configurations end up with neither systemd-resolved nor NetworkManager-with-dnsmasq. `dns status` shows `Backend: none`, and `getent hosts target.mesh` returns nothing.

Two ways forward:

1. **Use the resolver directly.** `dig @<tun-ip> target.mesh` always works -- the daemon's resolver is up regardless. Tools that let you specify a resolver (`dig`, some DNS libraries) can use it.
2. **Install one of the supported backends.** On Debian-based systems the cleanest path is `apt install systemd-resolved` and pointing NetworkManager at it. This is advanced configuration -- only do it on workstations where mesh-host names are essential and the default backend chain has not lit up.

For most operators, `auto` finds a backend without intervention.

## Removing a Host

In the dashboard, click the trash icon next to the entry. Confirm. Within seconds each workstation removes the cached entry and the suffix-side resolver returns `NXDOMAIN` for the name.

If you registered the host in the agent's `mesh_hosts:` config block (rather than at runtime), it shows up as protected -- you need to edit the config and restart the agent to remove it. Stick to runtime registration for anything that might change.

## Why Not Just Use /etc/hosts?

`/etc/hosts` works, but only on the box you edit. The mesh-host registry gives you:

- One source of truth (the agent's data directory).
- Propagation to every workstation without per-machine edits.
- Visibility from the dashboard.
- Comments and IPv6 alongside IPv4.
- Mesh-wide propagation: an entry registered at one agent reaches every other agent through the same flood that distributes routes.

When you only need it on one machine for ten minutes, `/etc/hosts` is still fine. When two operators will both need it next week, register it once in the dashboard.

## Common Surprises

- **A suffix you added in the dashboard does not resolve on the workstation.** Updates arrive over the SOCKS5 stream within seconds. Check `mutiauk dns status` -- the **Active TLDs** line should include your new suffix. If it does not, the workstation is not receiving registry data; check `socks5.server` reachability and, if the agent requires it, the SOCKS5 credentials in the workstation config.
- **`getent hosts target-11.mesh` works but `host target-11.mesh` does not.** Some DNS tools talk directly to `/etc/resolv.conf`-listed servers and skip the resolver framework. If `host` returns NXDOMAIN, the tool is asking your upstream DNS, not the system stub. `resolvectl query target-11.mesh` (when `systemd-resolved` is the backend) is the authoritative test.
- **Names work for one operator and not for another.** Compare `mutiauk dns status` between the two boxes. The most common difference is `Backend: none` on a workstation that does not have a supported resolver running. See "When Auto-Detection Picks `none`" above.
