// Deterministic screenshot capture for the Muti Metroo Manager dashboard.
//
// Points Playwright at a running Muti Metroo Manager instance (default
// http://muti-metroo.test) and writes a fixed set of PNGs into
// ../static/img/screenshots/muti-metroo-manager/. Each view waits for a
// selector that proves the view is mounted; CSS transitions and
// animations are neutered so re-runs produce near-identical output.
//
// Captures (numbered for stable doc references):
//
//   Always required (CORE):
//     01-topology              metro map, no panel, default view
//     02-topology-search       search dropdown open
//     03-agent-info            agent panel: Info tab
//     04-agent-peers           agent panel: Peers tab
//     05-agent-routes          agent panel: Routes tab
//     06-agent-forwards        agent panel: Forwards tab (seeds a paired forward)
//     07-routes-table          top-level Routes tab
//     08-port-forwarding       top-level Port Forwarding tab (seeds a paired forward)
//     09-footer-mesh-health    footer "Mesh: N/M reachable" indicator
//     12-domains-tab           DNS tab > Hosts (seeds two mesh hosts)
//     13-routes-test-routing   Routes tab > Test routing (seeded mesh-host trace)
//     15-mesh-test-modal       Mesh Connectivity Test modal
//     16-agent-ping            agent panel: Ping tab with active session
//     17-dns-tlds              DNS tab > TLDs sub-tab
//
//   Skipped if upstream feature is off:
//     10-agent-shell-powershell  Windows agent + management signing key
//     11-agent-files             Windows agent + management signing key
//     14-downloads-tab           muti-metroo-downloads-bundle.zip deployed
//     18-config-wizard-entry     Downloads tab present (uses Create config button)
//
// Usage:
//   npm install
//   npx playwright install chromium
//   npm run screenshots
//   METROO_MANAGER_URL=http://localhost:3000 npm run screenshots

import { chromium } from "playwright";
import path from "node:path";
import fs from "node:fs/promises";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const BASE_URL = (process.env.METROO_MANAGER_URL || "http://muti-metroo.test").replace(
  /\/$/,
  "",
);
const OUTPUT_DIR = path.resolve(
  __dirname,
  "..",
  "static",
  "img",
  "screenshots",
  "muti-metroo-manager",
);
const VIEWPORT = { width: 1600, height: 900 };
const NAV_TIMEOUT = 20000;
const STEP_TIMEOUT = 8000;

// CSS injected on every page to kill animations/transitions and the
// caret so textareas and polling spinners don't produce pixel noise.
const FREEZE_CSS = `
  *, *::before, *::after {
    animation-duration: 0s !important;
    animation-delay: 0s !important;
    transition-duration: 0s !important;
    transition-delay: 0s !important;
    caret-color: transparent !important;
  }
`;

async function main() {
  await fs.mkdir(OUTPUT_DIR, { recursive: true });

  const browser = await chromium.launch({ headless: true });
  const context = await browser.newContext({
    viewport: VIEWPORT,
    ignoreHTTPSErrors: true,
    deviceScaleFactor: 1,
    reducedMotion: "reduce",
    timezoneId: "UTC",
    locale: "en-US",
  });
  context.setDefaultTimeout(STEP_TIMEOUT);
  context.setDefaultNavigationTimeout(NAV_TIMEOUT);

  // Clear UI state that Muti Metroo Manager persists across reloads so every
  // run starts from the same topology-tab, centered-map state.
  await context.addInitScript(() => {
    try {
      localStorage.removeItem("muti-metroo-manager:main-tab");
      localStorage.removeItem("muti-metroo-manager:metro-view");
      localStorage.removeItem("muti-metroo-manager:metro-positions");
      localStorage.removeItem("muti-metroo-manager:routes-sub-tab");
    } catch {
      /* ignore */
    }
  });

  const page = await context.newPage();

  // Pick agent IDs deterministically (lexicographically lowest id) so
  // the #/agents/<id> deep-link in the panel shots is stable across runs.
  // mainId hosts the seeded forward endpoint; secondaryId hosts the
  // matching listener so the forwards tables show a paired row.
  const {
    mainId,
    mainFullId,
    mainIsLocal,
    secondaryId,
    secondaryFullId,
    secondaryIsLocal,
    pingId,
    pingCandidates,
    windowsId,
  } = await pickAgentShortIds(context);
  if (!mainId) {
    console.error(
      `No agents visible at ${BASE_URL}/api/proxy/api/topology.\n` +
        "Point METROO_MANAGER_URL at a Muti Metroo Manager instance whose upstream agent\n" +
        "is connected to a mesh with at least one agent.",
    );
    await browser.close();
    process.exit(1);
  }
  // For management mutations the local agent must use the unscoped
  // /forward/manage path; the remote-agent variant rejects routing to
  // self. mainPathId / secondaryPathId resolve to null for local
  // agents and the full ID for remote agents.
  const mainPathId = mainIsLocal ? null : mainFullId;
  const secondaryPathId = secondaryIsLocal ? null : secondaryFullId;
  console.log(
    `Using agent short_id=${mainId}${mainIsLocal ? " (local)" : ""} for deep-link shots.`,
  );
  if (secondaryId) {
    console.log(
      `Using secondary agent short_id=${secondaryId}${secondaryIsLocal ? " (local)" : ""} for forward listener.`,
    );
  }
  if (pingId && pingId !== mainId)
    console.log(`Using agent short_id=${pingId} for ping panel.`);
  if (windowsId)
    console.log(`Using windows agent short_id=${windowsId} for shell/files.`);

  const captured = [];
  const skipped = [];

  async function capture(name, fn) {
    const filename = `${name}.png`;
    const out = path.join(OUTPUT_DIR, filename);
    try {
      await fn(out);
      captured.push(filename);
      console.log(`  captured ${filename}`);
    } catch (err) {
      skipped.push({ filename, reason: err.message });
      console.warn(`  skipped ${filename}: ${err.message}`);
    }
  }

  async function openTopology() {
    await page.goto(`${BASE_URL}/`, { waitUntil: "domcontentloaded" });
    await page.addStyleTag({ content: FREEZE_CSS });
    await page.waitForSelector(".metro-svg .station", {
      timeout: STEP_TIMEOUT,
    });
  }

  async function switchMainTab(label) {
    await page
      .locator(".tabs-bar .tab-button", { hasText: label })
      .first()
      .click();
  }

  async function openAgentPanel(id = mainId) {
    // Muti Metroo Manager v0.2.25+ uses path + ?agent=<id> instead of the legacy
    // #/agents/<id> hash. Navigate directly so the router parses the URL
    // on initial load and opens the panel.
    await page.goto(`${BASE_URL}/topology?agent=${id}`, {
      waitUntil: "domcontentloaded",
    });
    await page.addStyleTag({ content: FREEZE_CSS });
    await page.waitForSelector(".metro-svg .station", {
      timeout: STEP_TIMEOUT,
    });
    await page.waitForSelector(".agent-panel .agent-panel-title", {
      timeout: STEP_TIMEOUT,
    });
  }

  async function switchAgentTab(label) {
    await page
      .locator(".agent-panel .agent-panel-tab", {
        hasText: new RegExp(`^${label}$`),
      })
      .first()
      .click();
  }

  // 01 - Topology map, no panel, default view.
  await capture("01-topology", async (out) => {
    await openTopology();
    await page.screenshot({ path: out });
  });

  // 02 - Topology with the in-corner search dropdown visible.
  await capture("02-topology-search", async (out) => {
    await openTopology();
    const search = page.locator(".agent-search-input").first();
    await search.click();
    await search.fill("a");
    await page.waitForSelector(".agent-search-results", {
      timeout: STEP_TIMEOUT,
    });
    await page.screenshot({ path: out });
  });

  // 03 - Agent panel, Info tab. Shows notes textarea and icon picker.
  await capture("03-agent-info", async (out) => {
    await openTopology();
    await openAgentPanel();
    await switchAgentTab("Info");
    await page.waitForSelector(".agent-panel .info-section-header.notes", {
      timeout: STEP_TIMEOUT,
    });
    await page.screenshot({ path: out });
  });

  // 04 - Peers tab.
  await capture("04-agent-peers", async (out) => {
    await openTopology();
    await openAgentPanel();
    await switchAgentTab("Peers");
    await page.waitForSelector(
      ".agent-panel .peers-remote-id, .agent-panel .tab-empty",
      {
        timeout: STEP_TIMEOUT,
      },
    );
    await page.screenshot({ path: out });
  });

  // 05 - Routes tab on the agent panel. Wait for the async fetch to land
  // so the "Loading routes..." placeholder is gone.
  await capture("05-agent-routes", async (out) => {
    await openTopology();
    await openAgentPanel();
    await switchAgentTab("Routes");
    await page.waitForSelector(
      ".agent-panel .routes-list, .agent-panel .tab-empty",
      {
        timeout: STEP_TIMEOUT,
      },
    );
    await page
      .waitForSelector(".agent-panel .tab-loading", { state: "detached" })
      .catch(() => {});
    await page.screenshot({ path: out });
  });

  // 06 - Forwards tab with listener/endpoint subtab bar visible. Same
  // loading-placeholder handling as the routes tab. Seed a paired
  // listener+endpoint so the table is non-empty, regardless of whether
  // the operator has any persistent forwards configured. Opens the
  // panel on the secondary agent (where the LISTENER landed) so the
  // default LISTENERS sub-tab shows a populated row.
  await capture("06-agent-forwards", async (out) => {
    const seeded = secondaryFullId !== null;
    if (seeded) {
      await seedForwardEndpoint(context, mainPathId, {
        key: "tools-share",
        target: "127.0.0.1:8000",
      });
      await seedForwardListener(context, secondaryPathId, {
        key: "tools-share",
        address: ":8090",
        max_connections: 50,
      });
    }
    try {
      // Give the listener side a moment to advertise so the panel sees a
      // populated forward route on the next render.
      if (seeded) await page.waitForTimeout(1500);
      await openTopology();
      // Open the panel on the listener-side agent so the default
      // LISTENERS sub-tab shows the seeded entry. Fall back to mainId
      // when there is no secondary agent.
      const panelId = secondaryId || mainId;
      await openAgentPanel(panelId);
      await switchAgentTab("Forwards");
      await page.waitForSelector(".agent-panel .forwards-subtabs", {
        timeout: STEP_TIMEOUT,
      });
      await page.waitForSelector(
        ".agent-panel .routes-list, .agent-panel .tab-empty",
        {
          timeout: STEP_TIMEOUT,
        },
      );
      await page
        .waitForSelector(".agent-panel .tab-loading", { state: "detached" })
        .catch(() => {});
      await page.screenshot({ path: out });
    } finally {
      if (seeded) {
        await unseedForwardListener(context, secondaryPathId, "tools-share");
        await unseedForwardEndpoint(context, mainPathId, "tools-share");
      }
    }
  });

  // 07 - Routes top-level tab.
  await capture("07-routes-table", async (out) => {
    await openTopology();
    await switchMainTab("Routes");
    await page.waitForSelector(".table-container, .no-data", {
      timeout: STEP_TIMEOUT,
    });
    await page.screenshot({ path: out });
  });

  // 08 - Port Forwarding top-level tab. Seeds the same paired listener+
  // endpoint as 06 so the global table shows at least one populated row.
  await capture("08-port-forwarding", async (out) => {
    const seeded = secondaryFullId !== null;
    if (seeded) {
      await seedForwardEndpoint(context, mainPathId, {
        key: "tools-share",
        target: "127.0.0.1:8000",
      });
      await seedForwardListener(context, secondaryPathId, {
        key: "tools-share",
        address: ":8090",
        max_connections: 50,
      });
    }
    try {
      if (seeded) await page.waitForTimeout(1500);
      await openTopology();
      await switchMainTab("Port Forwarding");
      await page.waitForSelector(".table-container, .no-data", {
        timeout: STEP_TIMEOUT,
      });
      await page.screenshot({ path: out });
    } finally {
      if (seeded) {
        await unseedForwardListener(context, secondaryPathId, "tools-share");
        await unseedForwardEndpoint(context, mainPathId, "tools-share");
      }
    }
  });

  // 10 - Management commands on the Windows workstation with PowerShell picked.
  // Requires the upstream agent to have management.signing_private_key
  // configured and the target agent to be reachable with shell enabled.
  await capture("10-agent-shell-powershell", async (out) => {
    if (!windowsId) throw new Error("no windows agent in topology");
    await openTopology();
    await openAgentPanel(windowsId);
    const shellTab = page
      .locator(".agent-panel .agent-panel-tab", { hasText: /^Shell$/ })
      .first();
    if ((await shellTab.getAttribute("disabled")) !== null) {
      throw new Error("shell tab is disabled (no management signing key?)");
    }
    await shellTab.click();
    await page
      .locator(".agent-panel .shell-picker-btn", {
        hasText: /^powershell\.exe$/,
      })
      .first()
      .click();
    await page.waitForSelector(".agent-panel .shell-status-connected", {
      timeout: STEP_TIMEOUT,
    });
    // Give the remote PTY a moment to render its banner/prompt.
    await page.waitForTimeout(2500);
    await page.screenshot({ path: out });
  });

  // 11 - File browser on the Windows workstation, navigated into C:\ so
  // the directory listing is shown rather than the roots selector. Same
  // capability requirements as the shell.
  await capture("11-agent-files", async (out) => {
    if (!windowsId) throw new Error("no windows agent in topology");
    await openTopology();
    await openAgentPanel(windowsId);
    const filesTab = page
      .locator(".agent-panel .agent-panel-tab", { hasText: /^Files$/ })
      .first();
    if ((await filesTab.getAttribute("disabled")) !== null) {
      throw new Error("files tab is disabled (no management signing key?)");
    }
    await filesTab.click();
    // Wait for the roots list, then click the first root (C:\).
    await page.waitForSelector(".agent-panel .files-root-item", {
      timeout: STEP_TIMEOUT,
    });
    await page.locator(".agent-panel .files-root-item").first().click();
    // Wait for the directory listing to land and the loading placeholder
    // to clear before snapping.
    await page.waitForSelector(
      ".agent-panel .files-list, .agent-panel .files-empty, .agent-panel .files-tab .tab-error",
      { timeout: STEP_TIMEOUT },
    );
    await page
      .waitForSelector(".agent-panel .files-loading", { state: "detached" })
      .catch(() => {});
    await page.screenshot({ path: out });
  });

  // 12 - Top-level Domains tab. Seed two entries via /hosts/manage so the
  // table has content; cleanup runs in the finally block to keep re-runs
  // idempotent.
  await capture("12-domains-tab", async (out) => {
    await seedDomain(context, "gateway.mesh", {
      ipv4: "10.0.0.1",
      ipv6: "fd00::1",
      comment: "edge gateway",
    });
    await seedDomain(context, "target-11.mesh", {
      ipv4: "10.8.0.11",
      comment: "edge worker",
    });
    try {
      await openTopology();
      await switchMainTab("DNS");
      // Wait for the loaded table: a route-domain cell only renders when
      // hosts.length > 0, so this skips the "Loading..." state.
      await page.waitForSelector(".dns-tab tbody td.route-domain", {
        timeout: STEP_TIMEOUT,
      });
      await page.screenshot({ path: out });
    } finally {
      await unseedDomain(context, "gateway.mesh");
      await unseedDomain(context, "target-11.mesh");
    }
  });

  // 13 - Routes major tab -> Test routing sub-tab with a populated trace.
  // Reuses a seeded mesh-host so the captured trace shows the
  // resolution=mesh path that's the headline of the new feature.
  await capture("13-routes-test-routing", async (out) => {
    await seedDomain(context, "gateway.mesh", { ipv4: "10.0.0.1" });
    try {
      await openTopology();
      await switchMainTab("Routes");
      await page
        .locator(".routes-sub-tab", { hasText: /^Test routing$/ })
        .first()
        .click();
      const input = page.locator(".route-trace-input").first();
      await input.fill("gateway.mesh");
      await page
        .locator(".route-trace-form .panel-btn", { hasText: /^Trace$/ })
        .first()
        .click();
      await page.waitForSelector(".route-trace-section .trace-views", {
        timeout: STEP_TIMEOUT,
      });
      await page.screenshot({ path: out });
    } finally {
      await unseedDomain(context, "gateway.mesh");
    }
  });

  // 09 - Footer mesh-health indicator. Clipped to the footer bbox so the
  // screenshot focuses on the "Mesh: N/M reachable" summary.
  await capture("09-footer-mesh-health", async (out) => {
    await openTopology();
    const footer = page.locator("footer, .footer-status").first();
    await footer.waitFor({ timeout: STEP_TIMEOUT });
    const box = await footer.boundingBox();
    if (!box) throw new Error("footer not measurable");
    await page.screenshot({
      path: out,
      clip: {
        x: 0,
        y: Math.max(0, Math.floor(box.y) - 4),
        width: VIEWPORT.width,
        height: Math.ceil(box.height) + 8,
      },
    });
  });

  // 14 - Top-level Downloads tab. Only present when muti-metroo-downloads-bundle.zip
  // is deployed alongside muti-metroo-manager; the tab is filtered out otherwise. Skip
  // gracefully if the tab is missing so the script still passes on hosts
  // without a bundle.
  await capture("14-downloads-tab", async (out) => {
    await openTopology();
    const tab = page
      .locator(".tabs-bar .tab-button", { hasText: /^Downloads$/ })
      .first();
    if ((await tab.count()) === 0) {
      throw new Error(
        "Downloads tab not present (no downloads bundle deployed)",
      );
    }
    await tab.click();
    await page.waitForSelector(".downloads-tab", { timeout: STEP_TIMEOUT });
    // The downloads listing renders one section per binary group; wait
    // until at least one row is visible so the snap is non-empty.
    await page.waitForSelector(
      ".downloads-tab .downloads-row-name, .downloads-tab .no-data",
      {
        timeout: STEP_TIMEOUT,
      },
    );
    await page.screenshot({ path: out });
  });

  // 15 - Mesh Connectivity Test modal. Triggered from the kebab menu. Waits
  // for the per-pair table to populate so the captured modal shows real
  // latency rows, not the running spinner.
  await capture("15-mesh-test-modal", async (out) => {
    await openTopology();
    await page.locator(".actions-menu-trigger").first().click();
    await page.waitForSelector(".actions-menu-dropdown", {
      timeout: STEP_TIMEOUT,
    });
    await page
      .locator(".actions-menu-item", {
        hasText: /Mesh Connectivity Test|Testing\.\.\./,
      })
      .first()
      .click();
    await page.waitForSelector(".test-modal", { timeout: STEP_TIMEOUT });
    // The modal renders an N x N grid of agent-pair cells once the
    // backend POST /api/mesh-test returns. Wait for a populated cell.
    await page.waitForSelector(".test-modal-list", { timeout: STEP_TIMEOUT });
    // Give the in-flight test a moment to land before snapping; mesh-test
    // probes are quick (~1s/pair) but bursty.
    await page.waitForTimeout(2500);
    await page.screenshot({ path: out });
    // Close the modal so subsequent shots are not contaminated.
    await page
      .locator(".test-modal-close")
      .first()
      .click()
      .catch(() => {});
  });

  // 16 - Agent panel Ping sub-tab with an active session. Iterates
  // through every Linux agent, trying each until one produces real
  // result rows. icmp_enabled=true at config level does not guarantee
  // the runtime ICMP socket opens (depends on the kernel
  // net.ipv4.ping_group_range sysctl), so we have to probe.
  await capture("16-agent-ping", async (out) => {
    const candidates =
      pingCandidates && pingCandidates.length > 0
        ? pingCandidates
        : [pingId, mainId].filter(Boolean);
    if (candidates.length === 0)
      throw new Error("no agent available for ping panel");
    const destIp = "8.8.8.8";
    let success = false;
    let lastError = null;
    for (const candidate of candidates) {
      try {
        await openTopology();
        await openAgentPanel(candidate);
        const pingTab = page
          .locator(".agent-panel .agent-panel-tab", { hasText: /^Ping$/ })
          .first();
        if ((await pingTab.getAttribute("disabled")) !== null) {
          lastError = `ping tab disabled on ${candidate}`;
          continue;
        }
        await pingTab.click();
        await page.waitForSelector(".ping-tab .ping-input", {
          timeout: STEP_TIMEOUT,
        });
        await page.locator(".ping-tab .ping-input").first().fill(destIp);
        await page
          .locator(".ping-tab .panel-btn", { hasText: /^Start$/ })
          .first()
          .click();
        // Wait for at least 3 successful (rtt-bearing) rows. If the
        // agent's ICMP socket fails to open, no rows will land --
        // detect that and try the next candidate.
        const ok = await page
          .waitForFunction(
            () => {
              return (
                document.querySelectorAll(".ping-tab .ping-row.ping-row-ok")
                  .length >= 3
              );
            },
            { timeout: STEP_TIMEOUT * 3 },
          )
          .then(() => true)
          .catch(() => false);
        if (ok) {
          await page.screenshot({ path: out });
          await page
            .locator(".ping-tab .ping-stop-btn")
            .first()
            .click()
            .catch(() => {});
          success = true;
          break;
        }
        // Stop and move on to the next candidate.
        lastError = `no successful echoes on ${candidate}`;
        await page
          .locator(".ping-tab .ping-stop-btn")
          .first()
          .click()
          .catch(() => {});
      } catch (err) {
        lastError = `candidate ${candidate}: ${err.message}`;
      }
    }
    if (!success) {
      throw new Error(`no Linux agent had a working ICMP socket: ${lastError}`);
    }
  });

  // 17 - DNS tab > TLDs sub-tab. Companion to 12 (Hosts). Wait for the
  // table to populate (a route-domain cell appears once the suffixes
  // list has loaded) so the screenshot is not the "Loading..." state.
  await capture("17-dns-tlds", async (out) => {
    await openTopology();
    await switchMainTab("DNS");
    await page
      .locator(".dns-tab .routes-sub-tab", { hasText: /^TLDs$/ })
      .first()
      .click();
    // Wait until the loading placeholder is gone -- either a real row
    // (.route-domain) renders, or the empty state ("no data") replaces
    // it. Both are documentation-worthy; "Loading..." is not.
    await page.waitForFunction(
      () => {
        const table = document.querySelector(
          ".dns-tab table, .dns-tab .table-container",
        );
        if (!table) return false;
        const text = table.textContent || "";
        // Filled list shows ".mesh" style rows; empty state shows e.g.
        // "No suffixes configured". Either is fine; reject only the
        // transitional "Loading..." text.
        return !text.includes("Loading");
      },
      { timeout: STEP_TIMEOUT * 2 },
    );
    await page.screenshot({ path: out });
  });

  // 18 - Entry config wizard, first step (TUN interface). Triggered from
  // the kebab menu's "Generate Mutiauk Config..." item. The wizard
  // is a modal so the rest of the dashboard is dimmed in the background,
  // which is fine for a feature-introduction screenshot.
  await capture("18-config-wizard-entry", async (out) => {
    await openTopology();
    await page.locator(".actions-menu-trigger").first().click();
    await page.waitForSelector(".actions-menu-dropdown", {
      timeout: STEP_TIMEOUT,
    });
    await page
      .locator(".actions-menu-item", { hasText: /Generate Mutiauk Config/ })
      .first()
      .click();
    await page.waitForSelector(".wizard-modal", { timeout: STEP_TIMEOUT });
    // First step is "TUN interface" - confirm the heading is present.
    await page.waitForSelector(".wizard-step-heading", {
      timeout: STEP_TIMEOUT,
    });
    await page.screenshot({ path: out });
    // Close the modal so subsequent shots are clean.
    await page
      .locator(".wizard-close")
      .first()
      .click()
      .catch(() => {});
  });

  console.log("");
  console.log(`Captured ${captured.length} screenshot(s) to ${OUTPUT_DIR}`);
  if (skipped.length) {
    console.log(`Skipped ${skipped.length}:`);
    for (const s of skipped) console.log(`  - ${s.filename}: ${s.reason}`);
  }

  await browser.close();

  // Fail the script only if the core set didn't land. "Core" is every
  // shot that doesn't depend on a specific upstream mesh shape (Windows
  // agent, downloads bundle deployed, etc.).
  const CORE = new Set([
    "01-topology.png",
    "03-agent-info.png",
    "04-agent-peers.png",
    "05-agent-routes.png",
    "06-agent-forwards.png",
    "07-routes-table.png",
    "08-port-forwarding.png",
    "12-domains-tab.png",
    "13-routes-test-routing.png",
    "15-mesh-test-modal.png",
    "16-agent-ping.png",
    "17-dns-tlds.png",
  ]);
  const missingCore = [...CORE].filter((f) => !captured.includes(f));
  if (missingCore.length) {
    console.error(`\nMissing core screenshots: ${missingCore.join(", ")}`);
    process.exit(1);
  }
}

async function seedDomain(context, name, { ipv4, ipv6, comment } = {}) {
  const body = { action: "add", name };
  if (ipv4) body.ipv4 = ipv4;
  if (ipv6) body.ipv6 = ipv6;
  if (comment) body.comment = comment;
  const resp = await context.request.post(
    `${BASE_URL}/api/proxy/hosts/manage`,
    {
      headers: { "Content-Type": "application/json" },
      data: body,
      timeout: NAV_TIMEOUT,
    },
  );
  // 409 = already exists from a prior interrupted run -- treat as success.
  if (!resp.ok() && resp.status() !== 409) {
    throw new Error(`seed ${name}: HTTP ${resp.status()} ${await resp.text()}`);
  }
}

async function unseedDomain(context, name) {
  const resp = await context.request.post(
    `${BASE_URL}/api/proxy/hosts/manage`,
    {
      headers: { "Content-Type": "application/json" },
      data: { action: "remove", name },
      timeout: NAV_TIMEOUT,
    },
  );
  // 404 = entry already gone (e.g. another run cleaned up); 403 = config-seeded
  // entry collided with our test name -- both are fine to ignore.
  if (!resp.ok() && resp.status() !== 404 && resp.status() !== 403) {
    console.warn(`  unseed ${name}: HTTP ${resp.status()}`);
  }
}

// Forward listener (ingress side -- where remote clients connect).
async function seedForwardListener(
  context,
  agentFullId,
  { key, address, max_connections },
) {
  const path = agentFullId
    ? `/api/proxy/agents/${agentFullId}/forward/manage`
    : `/api/proxy/forward/manage`;
  const body = { action: "add", key, address };
  if (typeof max_connections === "number")
    body.max_connections = max_connections;
  const resp = await context.request.post(`${BASE_URL}${path}`, {
    headers: { "Content-Type": "application/json" },
    data: body,
    timeout: NAV_TIMEOUT,
  });
  if (!resp.ok() && resp.status() !== 409) {
    throw new Error(
      `seed listener ${key}: HTTP ${resp.status()} ${await resp.text()}`,
    );
  }
}

async function unseedForwardListener(context, agentFullId, key) {
  const path = agentFullId
    ? `/api/proxy/agents/${agentFullId}/forward/manage`
    : `/api/proxy/forward/manage`;
  const resp = await context.request.post(`${BASE_URL}${path}`, {
    headers: { "Content-Type": "application/json" },
    data: { action: "remove", key },
    timeout: NAV_TIMEOUT,
  });
  if (!resp.ok() && resp.status() !== 404 && resp.status() !== 403) {
    console.warn(`  unseed listener ${key}: HTTP ${resp.status()}`);
  }
}

// Forward endpoint (exit side -- where the actual local service runs).
async function seedForwardEndpoint(context, agentFullId, { key, target }) {
  const path = agentFullId
    ? `/api/proxy/agents/${agentFullId}/forward/endpoints/manage`
    : `/api/proxy/forward/endpoints/manage`;
  const resp = await context.request.post(`${BASE_URL}${path}`, {
    headers: { "Content-Type": "application/json" },
    data: { action: "add", key, target },
    timeout: NAV_TIMEOUT,
  });
  if (!resp.ok() && resp.status() !== 409) {
    throw new Error(
      `seed endpoint ${key}: HTTP ${resp.status()} ${await resp.text()}`,
    );
  }
}

async function unseedForwardEndpoint(context, agentFullId, key) {
  const path = agentFullId
    ? `/api/proxy/agents/${agentFullId}/forward/endpoints/manage`
    : `/api/proxy/forward/endpoints/manage`;
  const resp = await context.request.post(`${BASE_URL}${path}`, {
    headers: { "Content-Type": "application/json" },
    data: { action: "remove", key },
    timeout: NAV_TIMEOUT,
  });
  if (!resp.ok() && resp.status() !== 404 && resp.status() !== 403) {
    console.warn(`  unseed endpoint ${key}: HTTP ${resp.status()}`);
  }
}

async function pickAgentShortIds(context) {
  const empty = {
    mainId: null,
    mainFullId: null,
    mainIsLocal: false,
    secondaryId: null,
    secondaryFullId: null,
    secondaryIsLocal: false,
    pingId: null,
    pingCandidates: [],
    windowsId: null,
  };
  try {
    const resp = await context.request.get(
      `${BASE_URL}/api/proxy/api/topology`,
      {
        timeout: NAV_TIMEOUT,
      },
    );
    if (!resp.ok()) return empty;
    const data = await resp.json();
    const agents = Array.isArray(data?.agents) ? data.agents : [];
    if (agents.length === 0) return empty;
    const sorted = [...agents].sort((a, b) => a.id.localeCompare(b.id));
    // Windows agent by OS, sorted for determinism across re-runs.
    const windows = sorted.find((a) => a.os === "windows");
    // Pick two distinct non-Windows agents so paired forward seeds (one
    // listener + one endpoint) can land on different agents and produce
    // a populated forward-routes row in the dashboard.
    const nonWindows = sorted.filter((a) => a.os !== "windows");
    const main = nonWindows[0] || sorted[0];
    const secondary =
      nonWindows.find((a) => a.id !== main.id) ||
      sorted.find((a) => a.id !== main.id) ||
      null;
    // Ping requires ICMP support; prefer a Linux agent (most reliable).
    // Build a candidate list of all Linux agents so the capture step can
    // try each in turn -- icmp_enabled=true is set from config, but the
    // runtime ICMP socket open also depends on the kernel
    // net.ipv4.ping_group_range sysctl, which not every host has set.
    const linuxAgents = sorted.filter((a) => a.os === "linux");
    const ping = linuxAgents[0] || main;
    return {
      mainId: main.short_id,
      mainFullId: main.id,
      mainIsLocal: !!main.is_local,
      secondaryId: secondary ? secondary.short_id : null,
      secondaryFullId: secondary ? secondary.id : null,
      secondaryIsLocal: secondary ? !!secondary.is_local : false,
      pingId: ping ? ping.short_id : null,
      pingCandidates: linuxAgents.map((a) => a.short_id),
      windowsId: windows ? windows.short_id : null,
    };
  } catch {
    return empty;
  }
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
