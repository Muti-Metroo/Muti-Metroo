# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

**Note:** The current year is 2026. Keep documentation and references up to date accordingly.

## Project Overview

Muti Metroo is a userspace mesh networking agent written in Go that builds VPN-like overlay networks across heterogeneous transport layers. It enables multi-hop routing with SOCKS5 ingress and CIDR-based exit routing, operating entirely in unprivileged userspace - no root privileges, no kernel extensions, no TUN driver.

**Key capabilities:**

- End-to-end encryption: X25519 key exchange + ChaCha20-Poly1305 (transit cannot decrypt)
- Multiple transport layers: QUIC/TLS 1.3, HTTP/2, and WebSocket
- SOCKS proxy ingress (SOCKS4, SOCKS4a, and SOCKS5) with optional authentication
- CIDR-based exit routing (DNS resolved at ingress agent)
- Domain-based exit routing (DNS resolved at exit agent)
- Multi-hop path routing via flood-based route propagation
- Stream multiplexing with half-close support

Muti Metroo is part of a three-component suite: **Muti Metroo** (mesh agent), **Mutiauk** (TUN interface), and **Muti Metroo Manager** (web dashboard). Each is a standalone binary that can be used independently.

## Related Projects

Both are sibling directories. You may explore their codebases when updating docs. Download links in the docs point at each repo's `releases/latest/download/<asset>` URL, so asset names must match the release assets exactly.

- **Mutiauk** (`../Mutiauk/`): Cross-platform TUN interface tool (Linux/macOS/Windows; requires root/CAP_NET_ADMIN on Linux and macOS, Administrator on Windows). Provides transparent Layer 3 traffic interception, forwarding through Muti Metroo's SOCKS5 proxy. Docs: `docs/docs/mutiauk.mdx`. **Build it with `GOTOOLCHAIN=go1.25.5`** (the Makefile targets and the `golang:1.25` Docker image already pin this) -- its gVisor dependency does not compile under Go 1.26.
- **Muti Metroo Manager** (`../Muti-Metroo-Manager/`): Cross-platform single Go binary with embedded React SPA. Browser-based UI that reverse-proxies an agent's HTTP API (topology visualization, real-time stats, route management, management commands, file transfer, mesh testing, sleep/wake). Docs: `docs/docs/muti-metroo-manager.mdx`.

## Documentation

The project documentation is built with Docusaurus and lives in the `docs/` folder.

**Audience distinction:**

- **Docusaurus (`docs/`)**: Public user manual for operators deploying and using Muti Metroo. Focus on installation, configuration, CLI usage, and operational guides. **Do NOT include implementation internals** such as frame protocol details, stream state machines, wire formats, or code architecture.
- **Architecture.md**: Developer documentation for contributors. Contains protocol internals, frame formats, package structure, and implementation details.

**URL Structure:** The public website uses `/` as root, not `/docs`. Source files in `docs/docs/` map to URLs without the `docs` prefix (e.g., `docs/docs/cli/overview.md` -> `/cli/overview`).

**Important:** When adding or modifying features, update docs accordingly:

1. **New features**: Add pages under `docs/docs/` in the appropriate category
2. **CLI changes**: Update `docs/docs/cli/`
3. **API changes**: Update `docs/docs/api/`
4. **Configuration changes**: Update `docs/docs/configuration/`
5. **Protocol/internals changes**: Update `Architecture.md` only (not Docusaurus)

**Editorial rules:** follow `docs/STYLE.md` (MDN editorial model: verify every claim against the source, reference before narrative, one canonical page per concept, ASCII only, every page ends with `## See Also`). Screenshots come only from `docs/scripts/capture-screenshots.sh`, which boots a throwaway Docker fixture mesh plus Muti Metroo Manager and captures every image; never commit a hand-made screenshot.

**Blend new material in; do not emphasize it.** A new feature sits at the same level of importance as its peers, not above them. Find the section(s) where sibling features are already documented and extend those in the same tone, length, and depth. Do not hoist a minor addition into top-level summaries, overview paragraphs, or feature taglines where it does not belong. Before writing, read how a comparable existing feature is documented and match it: same heading depth, same level of detail, same placement relative to the rest of the page.

The site is deployed to GitHub Pages (custom domain `mutimetroo.com`, set by `docs/static/CNAME`) by `.github/workflows/docs.yml` on every push to `master` that touches `docs/`.

To work with documentation locally: `cd docs && npm install && npm start` (dev server at http://localhost:3000). Production build: `npm run build`.

### Docs Toolchain Dependency Policy

Docusaurus produces a fully static site (HTML/CSS/JS) with no user-controlled
input, and the PDF manual is build-time tooling. Dependabot alerts on the
npm lockfiles (`docs/`, `user-manual/`)
are **lower risk than the gomod ones**, but they are not ignored: when alerts
accumulate, update deliberately (`npm audit fix`, direct-dependency upgrades,
targeted `overrides`) and re-run each project's build to confirm nothing
broke. Go module (`gomod`) alerts are always in scope and should be fixed
promptly.

### PDF Manual

The user manual PDF is attached to every release. Build it with `make pdf` in `user-manual/` (Pandoc + XeLaTeX). Update it when adding or modifying user-facing features.

### Making Releases

Releases are fully automated by release-please (`.github/workflows/release-please.yml`). Conventional commits on `master` (`fix:` and `feat:` drive version bumps) make release-please open a release PR; merging it creates the `muti-metroo-vX.Y.Z` tag and GitHub release, then CI builds and uploads the binaries, DLLs, the user manual PDF, and `checksums.txt`. Never tag or create releases by hand.

Build targets: darwin/arm64, darwin/amd64, linux/amd64, linux/arm64, windows/amd64, windows/arm64. Linux binaries are UPX-compressed. **Windows EXE binaries must NOT be UPX-compressed** -- the UPX unpacking stub is commonly flagged by antivirus engines as a false positive. The Windows amd64 DLL is still UPX-compressed since DLLs load differently. The arm64 DLL (cross-compiled with llvm-mingw in the `build-dll-arm64` release job) ships uncompressed because UPX cannot pack ARM64 PE files.

## Build & Development Commands

```bash
# Build / Test / Lint
make build                    # Build binary to ./build/muti-metroo
make test                     # Run all tests with race detection
make test-coverage            # Coverage report to ./coverage/
make test-short               # Short tests only
go test -v ./internal/...                   # Specific package tests
go test -v -run TestName ./internal/peer/   # Single test
make lint                     # gofmt and go vet
make fmt                      # Format code

# Development Setup
make generate-certs           # Self-signed TLS certs for dev
make init-dev                 # Initialize data directory and agent identity

# Run
make run                      # Run agent with ./config.yaml
./build/muti-metroo init -d ./data           # Initialize new agent
./build/muti-metroo run -c ./config.yaml     # Run with config file
./build/muti-metroo run -c ./config.yaml -D  # Windows: detach from SSH session (survives disconnect)

# Docker (preferred for building and testing)
docker compose build                          # Build all images
docker compose up -d agent1 agent2 agent3     # Start 3-agent mesh testbed
docker compose logs -f agent1                 # Follow logs for agent1
docker compose down                           # Stop all containers
docker compose run test                       # Run tests in container
```

**Certificate Management:** `muti-metroo cert {ca|agent|client} --cn "name"` generates the corresponding cert; `muti-metroo cert info <file>` displays info.

**Password Hash Generation** (for SOCKS5 auth and HTTP API bearer tokens): `muti-metroo hash` (interactive), `muti-metroo hash "password"` (arg), `--cost 12` for custom bcrypt cost.

**Interactive Setup Wizard:** `muti-metroo setup [-c ./config.yaml]` launches interactive configuration with cert generation.

**Key Generation:**

- `muti-metroo management-key {generate|public}` - management key for topology encryption
- `muti-metroo signing-key {generate|public}` - Ed25519 signing key for sleep/wake and privileged ops

**Mesh Connectivity Testing:** `muti-metroo mesh-test [--json] [-a host:8080]` tests connectivity to all mesh agents.

**Confirmation prompts**: `sleep`, `wake`, every `... remove` subcommand, `route add 0.0.0.0/0` (typed `add default route`), and `display-name set ""` all prompt for `[y/N]` before acting. Pass `--yes` / `-y` (persistent on `rootCmd`) to skip in scripts. On non-TTY stdin without `--yes`, the command refuses with exit code 6 (matches kubectl/aws). Add `--allow-default-route` to skip the default-route typed confirmation.

**Color**: `--no-color` (persistent) and the `NO_COLOR` env var both disable ANSI escapes. Color is also auto-off when stdout is not a TTY. All emission funnels through `colorize(code, s)` in cmd/muti-metroo/main.go.

**Status vs list naming**: `muti-metroo peer status` / `muti-metroo route status` are the canonical names for live transport state. `muti-metroo peer list` / `muti-metroo route list` are the configuration views (config + dynamic + inbound). The legacy top-level `muti-metroo peers` and `muti-metroo routes` are deprecated cobra aliases that still work but print a one-line warning and don't appear in root help.

**`-t` flag migration (in progress)**: `-t = --target` on the 16 management commands; `-t` for `--timeout` on the 6 action commands (mesh-test, probe, shell, upload, download, ping) is deprecated -- use `-w/--timeout` instead. The old short still works for one release with a stderr deprecation warning. The wiring is in `registerTimeoutFlag` (cmd/muti-metroo/main.go); copy that pattern when adding new action commands.

**TLS cert lifecycle**: `muti-metroo cert agent --days` and `muti-metroo cert client --days` default to 365. `muti-metroo cert info` warns at `<60 days` (heads-up) and `<30 days` (urgent). `muti-metroo run` also scans cert paths at startup and prints the same warning bands to stderr.

**bcrypt**: hash cost defaults to 12 (OWASP 2024). Constant `defaultBcryptCost` in cmd/muti-metroo/main.go.

**Config inspection** (no running agent required):

```bash
muti-metroo config validate ./config.yaml          # exit 0 ok / 2 invalid / 1 unreadable
muti-metroo config show ./config.yaml              # secrets redacted
muti-metroo config show ./config.yaml --diff-from-default
```

**Exit codes** (`docs/docs/cli/overview.md` for the canonical table): 0 ok, 1 generic, 2 usage, 3 agent unreachable, 4 auth failed, 5 operation rejected, 6 confirmation required.

**Dynamic Management CLIs** (all accept `-t <agent-id>` to target a remote agent, and `--json` on `list`):

```bash
# Dynamic CIDR routes
muti-metroo route add 10.0.0.0/8 [--metric 5] [--comment "office LAN"] [--source-ip 192.168.1.50]
muti-metroo route remove 10.0.0.0/8
muti-metroo route comment 10.0.0.0/8 "internal corp"          # set/clear; works on static or dynamic; survives restart; propagates to peers
muti-metroo route source-ip 10.0.0.0/8 192.168.1.50           # set/clear local source IP for outbound dials; works on static or dynamic; survives restart; local-only
muti-metroo route interface 10.0.0.0/8 ens192                 # set/clear egress NIC; portable bind-to-IP when source unset or on-interface; off-interface source uses SO_BINDTODEVICE (Linux + root); works on static or dynamic; survives restart; local-only
muti-metroo route list [--all]                                 # default: dynamic only; --all: config-seeded routes + their comments too
muti-metroo route trace 8.8.8.8                                # explain which route would be picked

# Dynamic forward listeners (ingress side)
muti-metroo forward add web-server :9090 [--max-connections 100]
muti-metroo forward remove web-server
muti-metroo forward list

# Dynamic forward endpoints (exit-side key -> host:port mapping)
muti-metroo forward endpoint add web-server 127.0.0.1:3000
muti-metroo forward endpoint remove web-server
muti-metroo forward endpoint list

# Dynamic peers (tell a running agent to dial a new peer)
muti-metroo peer add host:4433 [--transport h2] [--id abc] [--tcp-keepalive 20s]  # default transport QUIC
muti-metroo peer remove host:4433                              # disconnect + cancel reconnect
muti-metroo peer list                                          # config + dynamic + inbound

# Dynamic transport listeners (open/close a QUIC/H2/WS ingress at runtime)
muti-metroo listener add quic :4434                            # reuses global TLS; --plaintext for ws-behind-proxy
muti-metroo listener add ws 127.0.0.1:3002 --path /ws/ --plaintext [--advertise-address host:port]
muti-metroo listener remove :4434                              # stop accepting; existing conns unaffected
muti-metroo listener list [--json]                             # config + dynamic; survives restart + sleep/wake

# Dynamic display name
muti-metroo display-name set "gateway-us-east"
muti-metroo display-name get

# Mesh-host registry (local-only; no remote `-t` variant)
muti-metroo host add target-11.mesh --ipv4 10.8.0.11 [--ipv6 fd00::11] [--comment "edge"]
muti-metroo host remove target-11.mesh
muti-metroo host list [--json]
muti-metroo host suffix add .mesh                              # manage mesh-host TLD suffixes at runtime
muti-metroo host suffix remove .mesh
muti-metroo host suffix list [--json]
```

## Development Environment Guidelines

**Always use Docker for building and testing** unless explicitly requested otherwise. Docker commands are in Build & Development Commands above. Test endpoints: `curl http://localhost:808{1,2,3}/health`.

**Exception: SSH client testing** - Run SSH client on the host machine, not in Docker containers: `ssh -o ProxyCommand='nc -x localhost:1080 %h %p' user@target-host`

## Integration Tests

Before writing or modifying integration tests, read `internal/integration/README.md` for the conventions (in-process Go tests vs Docker testbed, three-agent minimum for tunnel tests, topology choices, real-network requirement, no-flake policy).

The live coverage matrix is in `internal/integration/coverage.csv` -- it tracks every testable feature with its current coverage status, priority, and the tests that cover it. When adding or extending a test, update the matching row.

## Code Style Guidelines

- **ASCII only**: Do not use emojis or non-ASCII characters in code, comments, documentation, commit messages, or any other text files.

### English-only CLI

The CLI is English-only (the former `internal/i18n` localization was
removed). Keep all user-facing output, comments, and docs in English.

## Architecture

### Agent Roles

An agent can serve multiple roles simultaneously:

- **Ingress**: SOCKS5 listener, DNS resolution, initiates virtual streams, performs route lookup
- **Transit**: Forwards streams between peers, participates in route flooding
- **Exit**: Opens real TCP connections, advertises CIDR and domain routes

### Package Structure (`internal/`)

| Package        | Purpose                                                                                                                                                                       |
| -------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `agent`        | Main orchestrator - initializes components, dispatches frames, manages lifecycle                                                                                              |
| `apiclient`    | HTTP client used by the CLI to call local and remote agent APIs (token auth, signed envelopes)                                                                                |
| `certutil`     | TLS certificate generation and management - CA, server, client, peer certs                                                                                                    |
| `chaos`        | Chaos testing utilities - fault injection, ChaosMonkey for resilience testing                                                                                                 |
| `config`       | YAML config parsing with env var substitution (`${VAR:-default}`)                                                                                                             |
| `crypto`       | End-to-end encryption - X25519 key exchange, ChaCha20-Poly1305, session key derivation                                                                                        |
| `dynconfig`    | Append-only JSONL journal persisting runtime peer/forward/route/display-name mutations                                                                                        |
| `embed`        | Embedded configuration - XOR encoding, binary config extraction and appending                                                                                                 |
| `exit`         | Exit node handler - TCP dial, route-based access control, E2E decryption                                                                                                      |
| `expose`       | Ephemeral forward endpoints over SOCKS5 - operators register ephemeral forward keys over the SOCKS5 `CmdExpose` stream; the registration disappears when the connection drops |
| `filetransfer` | Streaming file/directory transfer with tar, gzip, and permission preservation                                                                                                 |
| `flood`        | Route propagation via flooding with loop prevention and seen-cache                                                                                                            |
| `forward`      | Port forwarding (reverse tunnel) - endpoints expose local services, listeners accept remote                                                                                   |
| `health`       | Health check HTTP server, remote agent status, pprof, dashboard, mesh connectivity testing                                                                                    |
| `hosts`        | Mesh-local hostname registry - config + journalled runtime entries, validation, served via /hosts/manage and /api/dashboard                                                   |
| `icmp`         | ICMP echo (ping) - exit handler, unprivileged sockets, session management with E2E encryption                                                                                 |
| `identity`     | 128-bit AgentID generation, X25519 keypair storage for E2E encryption                                                                                                         |
| `integration`  | Integration tests for multi-agent mesh scenarios                                                                                                                              |
| `loadtest`     | Load testing utilities - stream throughput, route table, connection churn                                                                                                     |
| `logging`      | Structured logging with slog - text/JSON formats, standard attribute keys                                                                                                     |
| `peer`         | Peer connection lifecycle - handshake, keepalive, reconnection with backoff                                                                                                   |
| `probe`        | Connectivity testing for Muti Metroo listeners - transport dial, handshake verification                                                                                             |
| `protocol`     | Binary frame protocol - 14-byte header, encode/decode for all frame types                                                                                                     |
| `recovery`     | Panic recovery utilities for goroutines with logging and callbacks                                                                                                            |
| `routing`      | Route table with CIDR longest-prefix match, domain pattern matching, agent presence table, route manager                                                                      |
| `service`      | Cross-platform service management - systemd (Linux), launchd (macOS), Windows Service                                                                                         |
| `shell`        | Management commands - interactive (PTY) and streaming command execution, whitelist, authentication                                                                            |
| `sleep`        | Mesh hibernation - state machine, jittered polling, command deduplication, persistence, deterministic windows                                                                 |
| `socks5`       | SOCKS proxy server (SOCKS4/4a/5 auto-detected) with no-auth and username/password methods, optional WebSocket transport                                                       |
| `stream`       | Stream state machine (Opening->Open->HalfClosed->Closed), buffered I/O                                                                                                        |
| `sysinfo`      | System information collection for node info advertisements                                                                                                                    |
| `transport`    | Transport abstraction with QUIC, HTTP/2, and WebSocket implementations, TLS fingerprinting                                                                                    |
| `udp`          | UDP relay handler for SOCKS5 UDP ASSOCIATE - association lifecycle, datagram forwarding                                                                                       |
| `wizard`       | Interactive setup wizard with certificate generation                                                                                                                          |

## Configuration

Example config in `configs/example.yaml`. Key sections:

- `agent`: ID, data_dir, display_name, logging
- `tls`: Global TLS settings (CA, cert, key, mTLS)
- `protocol`: Protocol identifiers for customization (ALPN, HTTP header, WS subprotocol; set to empty string to disable)
- `listeners`: Transport listeners (QUIC on :4433). Each listener may carry an optional `advertise: {address, path, transport}` block to publish its public dial target via NodeInfo so other agents can discover and dial it; opt-in, rejects loopback and unspecified hosts.
- `peers`: Outbound peer connections with TLS config
- `socks5`: Ingress proxy settings
- `exit`: Exit node CIDR routes, domain routes, DNS config for domain route resolution
- `routing`: Advertisement intervals, node info interval, TTL, max hops
- `limits`: Stream limits and buffer sizes
- `http`: HTTP API server with granular endpoint control (health, metrics, dashboard, remote APIs, CLI). `token_hash` sets bcrypt-hashed bearer token; `minimal: true` disables all non-health endpoints.
- `shell`: Management commands (disabled by default; `whitelist: ["*"]` to allow all)
- `file_transfer`: File upload/download (disabled by default; `allowed_paths` works like shell whitelist)
- `icmp`: ICMP echo (ping) settings - allowed CIDRs, session limits
- `management`: Two independent key pairs. `public_key` / `private_key` (X25519, hex 32 bytes each) encrypt NodeInfo so only operators with the private key can decrypt the topology (route paths travel in plaintext because transit agents need them; agents without the private key only hide them at the API layer). `signing_public_key` / `signing_private_key` (Ed25519, hex 32 / 64 bytes) authorize every privileged operation -- shell, file transfer, dynamic config mutations, and sleep/wake. Agents with only the signing public key verify and execute signed requests; only operators with the signing private key can initiate them. When `public_key` is set without `private_key`, the agent restricts `/agents/...`, mesh-test fan-out, and route-trace fan-out to local-only.
- `sleep`: Sleep mode settings (enabled, poll_interval, poll_duration, auto_sleep_on_start)
- `udp`: SOCKS5 UDP ASSOCIATE relay settings (enabled by default; max_associations, idle_timeout, max_datagram_size)
- `forward`: Port forwarding - `endpoints` (exit side: key + target) and `listeners` (ingress side: key + address)

## Key Implementation Details

- **Frame size**: Max 16 KB payload for write fairness
- **Buffer size**: 256 KB per stream default
- **Timeouts**: Handshake 10s, Stream open 30s, Idle stream 5m
- **Keepalive**: Every 5m idle, 90s timeout
- **Reconnection**: Exponential backoff 1s->60s with 20% jitter
- **Protocol version**: 0x01 (in PEER_HELLO)
- **Stream ID allocation**: Dialer uses odd IDs, acceptor uses even IDs; StreamID 0 reserved for control

### Configurable Limits

| Parameter           | Config Key                     | Default | Description                                                   |
| ------------------- | ------------------------------ | ------- | ------------------------------------------------------------- |
| Max Hops            | `routing.max_hops`             | 16      | Maximum hops for route advertisements                         |
| Route TTL           | `routing.route_ttl`            | 5m      | Time before routes expire without refresh                     |
| Advertise Interval  | `routing.advertise_interval`   | 2m      | Route advertisement frequency                                 |
| Node Info Interval  | `routing.node_info_interval`   | 2m      | Node info advertisement frequency                             |
| Stream Open Timeout | `limits.stream_open_timeout`   | 30s     | Total round-trip time for STREAM_OPEN                         |
| Buffer Size         | `limits.buffer_size`           | 256 KB  | Deprecated; accepted but ignored                              |
| Max Streams/Peer    | `limits.max_streams_per_peer`  | 1000    | Deprecated; accepted but ignored (no per-peer limit enforced) |
| Max Total Streams   | `limits.max_streams_total`     | 10000   | Total concurrent streams across all peers (incl. pending opens) |
| Max Pending Opens   | `limits.max_pending_opens`     | 100     | Deprecated; pending opens count toward max_streams_total      |
| Idle Threshold      | `connections.idle_threshold`   | 5m      | Peer keepalive interval; SOCKS5 handshake deadline; exit/forward target read idle timeout |
| Keepalive Jitter    | `connections.keepalive_jitter` | 0.2     | Timing jitter for keepalive                                   |
| Connection Timeout  | `connections.timeout`          | 90s     | Disconnect after this keepalive timeout                       |
| TCP Keepalive       | `connections.tcp_keepalive`    | 30s     | SO_KEEPALIVE period for h2/ws (per-peer override; 0 disables) |

### Route Selection

Routes use **longest-prefix-match** with metric tiebreaker: most-specific prefix wins; if tied, lowest metric wins. Flood-based routing supports arbitrary topologies with `SeenBy` lists for loop prevention. Trigger immediate advertisement: `curl -X POST http://localhost:8080/routes/advertise`.

## Design Decisions

### No Prometheus Metrics

Muti Metroo intentionally does not include Prometheus metrics functionality. Agents may be deployed ad hoc and are often short-lived; operational simplicity is prioritized over observability.

**Do not add metrics functionality to this codebase.**
