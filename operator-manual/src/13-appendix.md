# Appendix: mutiauk CLI Quick Reference

The complete command surface of `mutiauk`. For any flag listed as "see user manual", consult the corresponding chapter there for the longer description.

## Global Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-c, --config` | `/etc/mutiauk/config.yaml` | Path to the config file |
| `-v, --verbose` | | Enable debug logging |

## Daemon

```bash
sudo mutiauk daemon start                       # Start the daemon (foreground)
sudo mutiauk daemon stop                        # Stop (default --wait=true, --timeout 30s; pass --wait=false for fire-and-forget)
sudo mutiauk daemon stop --timeout 5s --force   # Escalate to SIGKILL after 5s
sudo mutiauk daemon reload                      # SIGHUP - reload config without restart
mutiauk daemon status                           # Lightweight pid/uptime; works as k8s readiness probe
```

In production the daemon is run via systemd: `sudo systemctl {start,stop,restart,reload} mutiauk`.

## Setup

```bash
sudo mutiauk setup                              # Interactive (TTY) wizard

# Non-interactive (cloud-init/Ansible): use `config generate`, not `setup`
sudo mutiauk config generate --from answers.yaml -o /etc/mutiauk/config.yaml --json
sudo MUTI_METROO_SOCKS5_PASSWORD=secret mutiauk config generate --tun-name tun0 \
                      --socks5-server 127.0.0.1:1080 \
                      --socks5-username op \
                      --autoroutes-enabled \
                      -o /etc/mutiauk/config.yaml
```

`mutiauk setup` is interactive and requires a TTY -- it asks the same questions as the Muti Metroo Manager Entry Config wizard and writes the YAML. For unattended provisioning use `mutiauk config generate`, which takes per-field flags or a partial answers YAML (`--from`); the SOCKS5 password always comes from `MUTI_METROO_SOCKS5_PASSWORD` so it never lands in the answers file or process listing. Install the service as a separate `mutiauk service install` step.

## Service

```bash
sudo mutiauk service install -c /etc/mutiauk/config.yaml
sudo mutiauk service uninstall
mutiauk service status
```

Systemd integration, system-level only (writes `/etc/systemd/system/mutiauk.service`). Requires root because the daemon needs `CAP_NET_ADMIN` to create the TUN interface; there is no `--user` mode for `mutiauk service install`. (User-mode install is a Muti Metroo *agent* feature, not a Mutiauk one.)

## Status

```bash
mutiauk status                              # Per-component table + connectivity probes
mutiauk status --json                       # Machine-readable
mutiauk status --skip-tests                 # Skip live probes (faster, less informative)
mutiauk status --exit-code                  # Exit non-zero if anything is unhealthy
mutiauk status -q                           # Quiet (output suppressed; implies --exit-code)
mutiauk status --check socks5 --check tcp   # Repeatable; scopes --exit-code to specific
                                                # components: tun, socks5, tcp, udp, expose, daemon
```

The flag combinations most useful for monitoring:

```bash
mutiauk status --skip-tests --exit-code     # Cheap "is the daemon alive" check
mutiauk status -q --check socks5            # Page on SOCKS5 disconnect specifically
```

## Routes

```bash
mutiauk route list                 # Show active routes (table)
mutiauk route list --json          # JSON output
mutiauk route add <cidr>           # Runtime-only route
mutiauk route add <cidr> --persist                 # Save to config
mutiauk route add <cidr> --comment "label"         # Annotate
mutiauk route remove <cidr>        # Remove from kernel (and config with --persist)
mutiauk route plan                 # Preview pending changes
mutiauk route apply [--dry-run]    # Reconcile config to kernel
mutiauk route check                # Detect overlaps and conflicts
mutiauk route trace <ip|hostname>  # See which route would match, plus mesh path
mutiauk route trace <...> --json   # Machine-readable trace
```

`SOURCE` column in `route list`:

| Source | Meaning |
|--------|---------|
| `config` | From the YAML config file |
| `auto` | From the agent via autoroutes |
| `manual` | Added at runtime with `route add` |

## DNS

```bash
sudo mutiauk dns status            # Backend, suffixes, hosts, probes
sudo mutiauk dns status --json     # Machine-readable
```

Status fields to read:

- **Backend**: `resolvectl`, `nm-dnsmasq`, or `none`. `none` means host wiring is not active.
- **Listen**: where the local resolver is bound (TUN IP, port 53).
- **Active TLDs**: suffixes the workstation knows about, last applied timestamp.
- **Hosts**: count of registry entries.
- **Probes**: live tests. Both `resolver direct` and `system stub` showing `[OK] ... -> NXDOMAIN` for `mutiauk-status-probe.<suffix>` is the healthy state.

## Hosts

```bash
mutiauk host list                  # Local mesh-host cache
mutiauk host list --json
```

Read-only on the workstation. To register or remove entries, use Muti Metroo Manager -> **DNS** -> **Hosts** (Chapter 9).

## Expose

```bash
mutiauk expose status              # Connection state + active forwards
mutiauk expose list [--json]       # Forwards only
mutiauk expose add folder <key> <path> [--comment "..."]
mutiauk expose add tcp    <key> <host:port> [--comment "..."]
mutiauk expose remove <key>
```

These commands talk to the daemon over its Unix socket and do not require root in the typical install where the operator user has read access to the socket. Folder paths must be absolute and exist when the daemon starts. Routing keys must match `[A-Za-z0-9._-]+`. Runtime entries are lost on daemon restart unless they are also defined in the config (Chapter 10).

## Cleanup

```bash
sudo mutiauk cleanup               # Idempotent reset of TUN, socket, PID, DNS
sudo mutiauk cleanup --dry-run     # Preview what would change without touching anything
sudo mutiauk cleanup --purge       # Also remove /var/lib/mutiauk/routes.json
```

Refuses to run while the daemon is active. Stop the daemon first.

## Config

```bash
mutiauk config check               # Validate config; warns on permissive (>0600) modes
mutiauk config check --json        # CI-friendly machine-readable report
mutiauk config check --strict      # Treat warnings as errors (exit non-zero)
```

The setup wizard writes the config with mode `0600` since it carries `socks5.password`. `config check` flags hand-rolled files that drifted from that.

## Version

```bash
mutiauk version
```

Prints the daemon and CLI version (they should match -- the CLI binary is the daemon binary).

## Configuration File

Default location: `/etc/mutiauk/config.yaml`.

Top-level sections (see the user manual chapter on the TUN interface for the complete reference):

- `daemon` -- PID file, socket path
- `tun` -- interface name, MTU, address
- `socks5` -- upstream proxy (TCP or WebSocket), credentials
- `routes` -- static routes
- `autoroutes` -- receives routes/hosts/suffixes over the SOCKS5 endpoint (push); no polling
- `dns_integration` -- `auto`, `resolvectl`, `nm-dnsmasq`, or `none`
- `expose` -- folder and TCP forwards to publish
- `nat` -- internal NAT table tuning
- `logging` -- level, format, output

Environment variable overrides (rarely needed -- the recommended path is to set values in the config file):

| Variable | Overrides |
|----------|-----------|
| `MUTI_METROO_SOCKS5_PASSWORD` | `socks5.password`. Useful with systemd `LoadCredential=` or Kubernetes secrets so the password never lands in `/etc/mutiauk/config.yaml`. |

## File Locations

| Path | Contents |
|------|----------|
| `/usr/local/bin/mutiauk` | The binary |
| `/etc/mutiauk/config.yaml` | Config file (mode 0600) |
| `/var/run/mutiauk.pid` | Daemon PID file |
| `/var/run/mutiauk.sock` | Daemon Unix socket (CLI -> daemon) |
| `/var/lib/mutiauk/routes.json` | Persisted runtime route state |
| `/etc/systemd/system/mutiauk.service` | Systemd unit (after `service install`) |

## Where to Look Next

| Topic | Reference |
|-------|-----------|
| `muti-metroo` (the agent) CLI flags and config keys | User manual chapters 6, 17 |
| `muti-metroo-manager` (the dashboard) flags and config | User manual chapter 18 |
| Protocol internals, frame formats, transports | `Architecture.md` in the source tree |
| New feature added in a recent release | `CHANGELOG.md`, then the relevant user manual chapter |
