# docs/scripts

Deterministic screenshot capture for the documentation site.

**This script is the only supported way to refresh documentation
screenshots.** Manual snapshots drift; if a new screenshot is needed,
add a step to `capture-muti-metroo-manager.js` and re-run the whole batch.

## capture-muti-metroo-manager.js

Drives a headless Chromium against a running Muti Metroo Manager instance and
writes a fixed set of PNGs into `docs/static/img/screenshots/muti-metroo-manager/`.
Re-run any time the UI changes — filenames are stable, so the
references in `docs/docs/muti-metroo-manager.mdx` keep working.

### Prerequisites

- Node.js 20+
- A reachable Muti Metroo Manager instance. Default is `http://muti-metroo.test`;
  override with `METROO_MANAGER_URL`.
- The upstream Muti Metroo agent that Muti Metroo Manager proxies must have at least
  one connected peer so the topology map has something to render.
- For the richest screenshots, the upstream agent should have
  `management.signing_private_key` set so shell / files / forward
  management UIs are enabled. The script does not fail without it — the
  relevant tabs just show their disabled state.

### Install

```bash
cd docs/scripts
npm install
npx playwright install chromium
```

### Run

```bash
npm run screenshots
# or against a different instance
METROO_MANAGER_URL=http://localhost:3000 npm run screenshots
```

Output:

```
docs/static/img/screenshots/muti-metroo-manager/
  01-topology.png
  02-topology-search.png
  03-agent-info.png
  04-agent-peers.png
  05-agent-routes.png
  06-agent-forwards.png
  07-routes-table.png
  08-port-forwarding.png
  09-footer-mesh-health.png
  10-agent-shell-powershell.png      (Windows agent preferred; falls back to
                                      the first shell on any shell-enabled agent)
  11-agent-files.png                 (same fallback as 10)
  12-domains-tab.png
  13-routes-test-routing.png
```

The script picks an agent to deep-link into by sorting the topology's
`agents[]` by full ID and taking the first — same mesh, same agent,
same screenshots.

### Determinism notes

- Fixed viewport (1600x900), headless, UTC timezone, en-US locale.
- CSS transitions and animations are killed via an injected stylesheet.
- Persisted pan/zoom and station overrides in `localStorage` are
  cleared before the map is shot, so the view is always centered.
- The script uses `waitForSelector` for each view rather than wall-clock
  sleeps, so it waits exactly as long as the SPA needs.
- Shots `12-domains-tab` and `13-routes-test-routing` seed sample
  mesh-host entries via `/hosts/manage` before capturing and remove them
  after, so re-runs are idempotent and don't accumulate state. If a run
  is interrupted between seed and cleanup, the next run's seed treats
  `409 Conflict` as success and proceeds.

Pixel identity across days is not guaranteed — the Muti Metroo agent's
reported uptime, RTT numbers, and poll timing show up in the UI. The
framework is deterministic; the data isn't.

### Out of scope

- Screenshots of tabs that need a signed management key when the
  upstream agent doesn't have one (shell terminal, file browser).
- User manual (PDF) integration of the PNGs.
- Automated docker-compose fixture mesh for CI-style reruns.
