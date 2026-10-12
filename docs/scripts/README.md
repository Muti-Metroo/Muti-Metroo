# docs/scripts

Reproducible screenshot capture for the documentation site.

**`capture-screenshots.sh` is the only supported way to create or refresh a
documentation screenshot.** Hand-made snapshots drift from the product and
cannot be regenerated. If a page needs a new screenshot, add a step to
`capture-muti-metroo-manager.js` and re-run the whole batch.

## Run

```bash
docs/scripts/capture-screenshots.sh
```

Requirements: Go, Docker, Node.js 20+, and an authenticated `gh` CLI
(used to fetch the Manager release binary). Playwright and its
Chromium are installed on the first run.

The script is self-contained:

1. Builds a Linux `muti-metroo` from this repository's committed `HEAD`
   (uncommitted changes are ignored), stamped with the latest release
   version, and downloads the latest Muti Metroo Manager release binary for
   the host, cached in `docs/scripts/.cache/` by tag.
2. Starts a four-agent fixture mesh in Docker containers on a private
   network (`10.99.0.0/24`), with fixed agent IDs, display names, hostnames,
   and addresses:

   ```text
   gateway --QUIC-- transit --HTTP/2-- exit-datacenter
      \
       --WebSocket-- exit-cloud
   ```

   `gateway` runs SOCKS5 and the HTTP API that Manager proxies, and holds the
   management and signing private keys (generated fresh on every run). `gateway` and `transit` advertise their listeners as public dial
   targets. `exit-datacenter` allows management commands and file transfer
   under `/srv/files`. Sleep mode is enabled on every agent. Each container
   adds a fixed egress delay with `tc netem`, so links show realistic RTTs.
3. Starts Manager on the host, waits until it reports the whole mesh
   reachable, and runs `capture-muti-metroo-manager.js`.
4. Removes the containers, the network, and all temporary files, also when a
   step fails.

Output goes to `docs/static/img/screenshots/muti-metroo-manager/`. Filenames
are stable, so page references keep working across runs. Every shot is
required; the run fails if one is skipped.

| File                        | Shows                                                       |
| --------------------------- | ----------------------------------------------------------- |
| `01-topology`               | Topology tab, default view                                  |
| `02-topology-search`        | Topology search with results                                |
| `03-agent-info`             | Agent panel Info tab (gateway)                              |
| `04-agent-peers`            | Agent panel Connectivity > Peers (gateway)                  |
| `05-agent-routes`           | Agent panel Routes tab (exit-cloud)                         |
| `06-agent-forwards`         | Agent panel Forwards tab (transit, seeded forward)          |
| `07-routes-table`           | Routes tab > Active routes                                  |
| `08-port-forwarding`        | Port Forwarding tab (seeded forward)                        |
| `09-footer-mesh-health`     | Footer mesh-health summary, cropped                         |
| `10-agent-shell`            | Agent panel Shell tab, bash on exit-datacenter              |
| `11-agent-files`            | Agent panel Files tab in `/srv/files` (exit-datacenter)     |
| `12-domains-tab`            | DNS tab > Hosts (seeded mesh hosts)                         |
| `13-routes-test-routing`    | Routes tab > Test routing for a seeded mesh host            |
| `15-mesh-test-modal`        | Mesh test results dialog                                    |
| `16-agent-ping`             | Agent panel Ping tab after a 10-echo session to exit-cloud  |
| `17-dns-tlds`               | DNS tab > TLDs                                              |
| `18-config-wizard-entry`    | Mutiauk config wizard, first step                           |
| `19-agent-listeners`        | Agent panel Connectivity > Listeners (gateway)              |
| `20-route-edit-dialog`      | Edit route dialog on exit-datacenter, not saved             |
| `21-topology-networks`      | Topology with network boxes (seeded metadata and positions) |
| `22-actions-menu`           | Actions menu open                                           |
| `23-settings-dialog`        | Settings dialog                                             |
| `24-agent-wizard-review`    | Agent config wizard, Review & download step                 |
| `25-topology-dark`          | Topology tab in the dark theme                              |
| `26-sleep-confirm`          | Suspend mesh confirmation, not confirmed                    |

| Variable         | Default                   | Effect                                         |
| ---------------- | ------------------------- | ---------------------------------------------- |
| `MANAGER_BIN`    | latest release            | Manager binary to use instead of the release   |
| `PORT_BASE`      | `27100`                   | Host ports used: `PORT_BASE+20` (agent API), `PORT_BASE+30` (Manager) |
| `KEEP_RUNNING=1` | unset                     | Leave the mesh and Manager running afterwards  |
| `HEADED=1`       | unset                     | Show the browser while capturing               |

## Determinism

Every run writes byte-identical files, with no tolerance applied. What makes that hold:

- **Fixed fixture.** Agent IDs, names, hostnames, IP addresses, link delays,
  and file timestamps never change. Key pairs are
  generated fresh on every run and never stored in the repository.
- **Fixed browser.** Viewport 1600x900, UTC timezone, en-US locale, light
  color scheme, reduced motion, software rendering with a fixed color
  profile and a single raster thread. A stylesheet installed before first
  paint stops animations, transitions, and the text caret.
- **Frozen clock.** The page clock is fixed at 2026-01-15 10:30 UTC.
- **Normalized live data.** Agent API responses pass through the capture
  script before Manager sees them: agents and links are sorted, uptimes and
  timestamps are set to fixed values, and mesh-test latencies are replaced
  with the fixture's nominal values (transit 12 ms, exit-datacenter 30 ms,
  exit-cloud 77 ms). The ping shot shows the nominal 76 ms for its path,
  and the agent config wizard shot shows fixed placeholder public keys.
- **Seeded state.** Forwards, mesh hosts, network membership, and station
  positions are created through the agent or Manager API before a shot and
  removed after it.
- **Settled rendering.** Every shot opens in a fresh page, tabs are opened
  by URL, and a shot is taken only after three consecutive screenshots 1.2 s
  apart are identical.
- **Fixed inputs.** The agent is built from committed `HEAD` and the
  Manager is the latest release binary, so work in progress in either
  repository never changes a shot.
- **Full-tile rasterization.** Chromium runs with partial raster disabled,
  so anti-aliased edges (rounded corners, dialog borders) are drawn the same
  way on every run.

To capture only some shots, set `ONLY` to a comma-separated list of names,
for example `ONLY=07-routes-table,08-port-forwarding`. It needs a running
fixture: start one with `KEEP_RUNNING=1 docs/scripts/capture-screenshots.sh`,
then run `METROO_MANAGER_URL=http://127.0.0.1:27130 ONLY=... node
docs/scripts/capture-muti-metroo-manager.js`.

## Adding a screenshot

1. Add a `capture("NN-name", ...)` step to `capture-muti-metroo-manager.js`.
   Use the next free number; never renumber existing shots.
2. If the shot needs fixture state that the mesh does not have, add it to the
   agent configs in `capture-screenshots.sh` or seed it in the step and
   remove it afterwards.
3. Run `capture-screenshots.sh` and reference the image from the page with
   descriptive alt text.
