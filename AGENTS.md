# AGENTS.md

Guidance for AI agents (and humans) working in this repository. `CLAUDE.md`
carries the same content for Claude Code; this file is the canonical
agent-focused guide. If they disagree, this file wins — fix the other one.

## Project Overview

Muti Metroo is a userspace mesh networking agent written in Go. It builds
VPN-like overlay networks across heterogeneous transports (QUIC/TLS 1.3,
HTTP/2, WebSocket), with SOCKS4/4a/5 ingress, CIDR and domain-based exit
routing, multi-hop flood routing, and X25519 + ChaCha20-Poly1305 end-to-end
encryption. It runs entirely in unprivileged userspace — no root, no kernel
extensions, no TUN driver.

Three-component suite (sibling checkouts):

- **Muti Metroo** (this repo) — the mesh agent. Module
  `github.com/Muti-Metroo/Muti-Metroo`, binary `muti-metroo`.
- **Mutiauk** (`../Mutiauk/`) — TUN interface client forwarding traffic
  through an agent's SOCKS5 proxy. Module `github.com/Muti-Metroo/Mutiauk`.
  Build it with `GOTOOLCHAIN=go1.25.5` — its gVisor dependency does not
  compile under Go 1.26.
- **Muti Metroo Manager** (`../Muti-Metroo-Manager/`) — web dashboard
  (single Go binary + embedded React SPA) that reverse-proxies an agent's
  HTTP API. Module `github.com/Muti-Metroo/Muti-Metroo-Manager`.

## Repo Layout

- `cmd/muti-metroo/` — CLI entry point (cobra), all subcommands
- `cmd/muti-dll/` — Windows DLL build (`-buildmode=c-shared`)
- `internal/` — all packages; see the package table in CLAUDE.md /
  Architecture.md. Hot paths: `transport`, `stream`, `peer`, `routing`,
  `flood`, `agent`, `socks5`, `exit`, `forward`
- `configs/example.yaml` — fully commented reference config
- `docs/` — Docusaurus site (public operator docs)
- `user-manual/` — user manual PDF source (Pandoc/XeLaTeX)
- `tests/connectivity-tunnel/` — Docker testbed: Mutiauk TUN client ->
  ingress -> transit -> exit -> target (TCP+UDP)
- `testbed/` — larger Docker fixtures
- `docker-compose.yml` (root) — 5-agent mixed-transport chain for local dev
- `Architecture.md` — protocol internals (developer docs; keep wire-format
  details out of `docs/`)
- `.github/workflows/` — `release-please.yml` (releases and assets) and `docs.yml` (docs site to GitHub Pages)

## Build & Test

Go **1.26** (go.mod `go 1.26.0`; Dockerfiles and CI pin `golang:1.26`).
Local toolchain is usually newer; `GOTOOLCHAIN=auto` handles it.

```bash
go build ./...              # full build
go test -short ./...        # fast suite; the default bar for changes
go test ./...               # full suite incl. internal/integration (~3-4 min)
go test -race ./internal/transport/ ./internal/stream/ ./internal/routing/ \
  ./internal/peer/ ./internal/agent/ ./internal/socks5/ ./internal/forward/ \
  ./internal/flood/         # race detector on the hot packages

make build                  # binary to ./build/muti-metroo
make test                   # full suite with -race
make test-short             # -short
make lint                   # gofmt -s -l . && go vet ./...
```

Docs sites:

```bash
cd docs && npm ci && npm run build     # Docusaurus (Node 20+)
cd user-manual && make html            # make pdf needs XeLaTeX
```

Docker testbeds:

```bash
docker compose build && docker compose up -d        # root 5-agent chain
cd tests/connectivity-tunnel
docker compose build && docker compose up -d
docker compose exec client /scripts/start-client.sh # starts ingress + Mutiauk
docker compose exec client /scripts/test-routing.sh # TCP/UDP assertions
```

Integration-test conventions: `internal/integration/README.md`; live
coverage matrix in `internal/integration/coverage.csv` — update it when
adding tests.

## Release Process

- Conventional commits on master; **release-please** (see
  `.github/workflows/release-please.yml`) proposes release PRs.
  `fix:`/`feat:` drive version bumps; `docs:`/`test:`/`chore:` do not.
- `.github/.release-please-manifest.json` pins the released version.
- Merging the release PR creates the tag and GitHub release; CI then builds
  and uploads binaries, DLLs, the user manual PDF, and checksums. Never tag or
  create releases by hand.
- Windows EXEs are never UPX-compressed (AV false positives); the DLL is.

## House Rules

- **Branding**: the product is "Muti Metroo" (binary/module `muti-metroo`),
  companions "Mutiauk" and "Muti Metroo Manager". The mascot is the **mole**
  (`docs/static/img/mole-*.png`). Never reintroduce "Meshy" or hedgehog
  references; `git grep -in 'meshy\|hedgehog'` must stay empty. ALPN/WS
  protocol string is `muti-metroo/1`; env prefix `MUTI_METROO_`.
- **English-only**: code, comments, docs, commit messages. ASCII only; no
  emojis. (The i18n system was removed — the CLI is English-only; do not
  re-add localization.)
- **Wire/JSON/API strings are a public contract** — do not rename log
  fields, JSON keys, or protocol identifiers for style reasons.
- **No Prometheus metrics** — intentional; do not add.
- Prefer Docker for building/testing when the host toolchain might diverge.
- Docs updates ride along with features (docs/, user-manual/src/,
  Architecture.md for internals). Blend new material in at the same level
  as sibling features; do not hoist it into summaries/taglines.
