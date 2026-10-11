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

Requirements: Go, Docker, Node.js 20+, `zip`, and an authenticated `gh` CLI
(used to fetch release assets for the Downloads tab). Playwright and its
Chromium are installed on the first run.

The script is self-contained:

1. Builds a Linux `muti-metroo` from this checkout, stamped with the latest
   release version, and `muti-metroo-manager` from a sibling checkout
   (`MANAGER_REPO`, default `../Muti-Metroo-Manager`).
2. Starts a four-agent fixture mesh in Docker containers on a private
   network (`10.99.0.0/24`), with fixed agent IDs, display names, hostnames,
   and addresses:

   ```text
   gateway --QUIC-- transit --HTTP/2-- exit-datacenter
      \
       --WebSocket-- exit-cloud
   ```

   `gateway` runs SOCKS5 and the HTTP API that Manager proxies, and holds the
   management and signing private keys (a fixed pair used only by this
   fixture). `gateway` and `transit` advertise their listeners as public dial
   targets. `exit-datacenter` allows management commands and file transfer
   under `/srv/files`. Sleep mode is enabled on every agent. Each container
   adds a fixed egress delay with `tc netem`, so links show realistic RTTs.
3. Builds a Manager downloads bundle from the latest GitHub release assets,
   cached in `docs/scripts/.cache/` by release tag.
4. Starts Manager on the host, waits until it reports the whole mesh
   reachable, and runs `capture-muti-metroo-manager.js`.
5. Removes the containers, the network, and all temporary files, also when a
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
| `14-downloads-tab`          | Downloads tab                                               |
| `15-mesh-test-modal`        | Mesh test results dialog                                    |
| `16-agent-ping`             | Agent panel Ping tab with a running session                 |
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
| `MANAGER_REPO`   | `../Muti-Metroo-Manager`  | Manager checkout to build                      |
| `PORT_BASE`      | `27100`                   | Host ports used: `PORT_BASE+20` (agent API), `PORT_BASE+30` (Manager) |
| `KEEP_RUNNING=1` | unset                     | Leave the mesh and Manager running afterwards  |
| `HEADED=1`       | unset                     | Show the browser while capturing               |

## Determinism

- Fixed viewport (1600x900), UTC timezone, en-US locale, reduced motion, and
  an injected stylesheet that stops CSS animations and the text caret.
- Fixed agent IDs, names, hostnames, IP addresses, link delays, and a fixed
  bundle timestamp. The capture script picks agents by sorting full IDs, so
  the same agent appears in the same shot every run.
- State a shot needs is seeded through the agent API (forwards, mesh hosts)
  or Manager's own API (network membership, station positions) before the
  shot and removed after it. The Manager's displayed agent API and SOCKS5
  addresses are set to `gateway.example.net` for the whole run and restored
  at the end.
- Every page load clears the Manager's localStorage (map view, panel width,
  theme), and the browser reports a light color scheme, so all shots except
  `25-topology-dark` use the light theme.
- Each view waits for a selector that proves it is rendered, not for a fixed
  delay.

Live values (clock times, uptime, measured RTTs, ping results, file
timestamps, and the date in the generated agent config) differ between runs.
Everything else is the same.

## Adding a screenshot

1. Add a `capture("NN-name", ...)` step to `capture-muti-metroo-manager.js`.
   Use the next free number; never renumber existing shots.
2. If the shot needs fixture state that the mesh does not have, add it to the
   agent configs in `capture-screenshots.sh` or seed it in the step and
   remove it afterwards.
3. Run `capture-screenshots.sh` and reference the image from the page with
   descriptive alt text.
