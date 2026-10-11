// Deterministic screenshot capture for the Muti Metroo Manager dashboard.
//
// Run through capture-screenshots.sh, which starts the four-agent fixture
// mesh (gateway, transit, exit-datacenter, exit-cloud) and Manager, then
// points this script at it with METROO_MANAGER_URL. Writes a fixed set of
// PNGs into ../static/img/screenshots/muti-metroo-manager/. Each view waits
// for a selector that proves the view is rendered; CSS transitions and
// animations are neutered so re-runs produce near-identical output. State a
// shot needs is seeded through the agent or Manager API and removed after
// the shot, so re-runs are idempotent.
//
// Captures (numbered for stable doc references; never renumber):
//
//   01-topology              metro map, no panel, default view
//   02-topology-search       search dropdown open
//   03-agent-info            agent panel (gateway): Info tab
//   04-agent-peers           agent panel (gateway): Connectivity > Peers
//   05-agent-routes          agent panel (gateway): Routes tab
//   06-agent-forwards        agent panel (transit): Forwards tab (seeds a paired forward)
//   07-routes-table          top-level Routes tab
//   08-port-forwarding       top-level Port Forwarding tab (seeds a paired forward)
//   09-footer-mesh-health    footer "Mesh: N/M reachable" indicator, clipped
//   10-agent-shell           agent panel (exit-datacenter): Shell tab, bash session
//   11-agent-files           agent panel (exit-datacenter): Files tab in /srv/files
//   12-domains-tab           DNS tab > Hosts (seeds two mesh hosts)
//   13-routes-test-routing   Routes tab > Test routing (seeded mesh-host trace)
//   14-downloads-tab         top-level Downloads tab (bundle staged by the rig)
//   15-mesh-test-modal       Mesh test results dialog
//   16-agent-ping            agent panel: Ping tab with an active session
//   17-dns-tlds              DNS tab > TLDs sub-tab
//   18-config-wizard-entry   Mutiauk config wizard, first step (TUN interface)
//   19-agent-listeners       agent panel (gateway): Connectivity > Listeners
//   20-route-edit-dialog     Edit route dialog for a route on exit-datacenter
//   21-topology-networks     metro map with network boxes (seeds agent metadata)
//   22-actions-menu          actions menu open on the topology view
//   23-settings-dialog       Manager settings dialog
//   24-agent-wizard-review   agent config wizard, Review & download step
//   25-topology-dark         metro map in the dark theme (captured last)
//   26-sleep-confirm         Suspend mesh confirmation in the actions menu
//
// Every shot is required: the script exits non-zero if any is skipped.
//
// Usage:
//   docs/scripts/capture-screenshots.sh            (normal path)
//   METROO_MANAGER_URL=http://127.0.0.1:27130 node capture-muti-metroo-manager.js
//     (against a rig left up with KEEP_RUNNING=1)

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
  /* Map links animate a dash offset; with the animation removed the dots
     always sit at the same phase. */
  .metro-svg .connection { animation: none !important; }
`;

async function main() {
  await fs.mkdir(OUTPUT_DIR, { recursive: true });

  const browser = await chromium.launch({
    headless: !process.env.HEADED,
    // Software rendering with a fixed color profile and grayscale text
    // anti-aliasing, so edges rasterize the same way on every run.
    args: [
      "--disable-gpu",
      "--force-color-profile=srgb",
      "--disable-lcd-text",
      "--font-render-hinting=none",
      // One raster thread: tiles rasterize in a fixed order, so
      // anti-aliased edges come out identical.
      "--num-raster-threads=1",
      // Re-rasterize whole tiles instead of only their invalidated part.
      // Partial raster leaves anti-aliased edges (rounded corners, dialog
      // borders) in one of two states depending on what was redrawn last.
      "--disable-partial-raster",
      "--disable-zero-copy",
    ],
  });
  const context = await browser.newContext({
    viewport: VIEWPORT,
    ignoreHTTPSErrors: true,
    deviceScaleFactor: 1,
    reducedMotion: "reduce",
    timezoneId: "UTC",
    locale: "en-US",
    // Manager follows the OS color scheme when no theme is stored; pin it
    // so every shot except 25 is in the light theme.
    colorScheme: "light",
  });
  // Freeze the page clock, so "Last update" and "tested" times in the
  // footer, and any other rendered time, are the same on every run. Timers
  // keep running; only the reported wall time is fixed.
  await context.clock.setFixedTime(new Date("2026-01-15T10:30:00Z"));
  context.setDefaultTimeout(STEP_TIMEOUT);

  // Normalize the two responses that carry run-to-run noise:
  //  - topology: the agent lists agents in no fixed order, and lists
  //    either end of a link first, which reorders search results and shifts
  //    where the map draws its dots. Sort agents by ID, orient every link
  //    from the lower to the higher short ID, and sort the links.
  //  - mesh test: per-agent latencies are live measurements. Replace them
  //    with the fixture's nominal values (set by the netem delays in
  //    capture-screenshots.sh) and sort results by agent ID.
  const NOMINAL_LATENCY_MS = {
    transit: 12,
    "exit-datacenter": 30,
    "exit-cloud": 77,
  };
  // Fixed stand-ins for live values in agent API responses, so uptimes and
  // timestamps render the same on every run.
  const FIXED_TIME = "2026-01-15T10:30:00Z";
  function normalizeLive(value) {
    if (Array.isArray(value)) return value.forEach(normalizeLive);
    if (!value || typeof value !== "object") return;
    for (const [key, v] of Object.entries(value)) {
      if (key === "uptime_hours" && typeof v === "number") value[key] = 26.5;
      else if (
        ["started_at", "last_update", "last_poll_time", "test_time"].includes(key) &&
        typeof v === "string" &&
        !v.startsWith("0001-")
      )
        value[key] = FIXED_TIME;
      else normalizeLive(v);
    }
  }
  function normalizeTopology(body) {
    if (Array.isArray(body.agents)) {
      body.agents.sort((a, b) => a.id.localeCompare(b.id));
    }
    if (Array.isArray(body.connections)) {
      body.connections = body.connections
        .map((c) =>
          c.from_agent > c.to_agent
            ? { ...c, from_agent: c.to_agent, to_agent: c.from_agent }
            : c,
        )
        .sort((a, b) =>
          `${a.from_agent}|${a.to_agent}`.localeCompare(`${b.from_agent}|${b.to_agent}`),
        );
    }
  }
  function normalizeMeshTest(body) {
    if (!Array.isArray(body.results)) return;
    for (const r of body.results) {
      if (r.reachable && r.display_name in NOMINAL_LATENCY_MS) {
        r.response_time_ms = NOMINAL_LATENCY_MS[r.display_name];
      }
    }
    body.results.sort((a, b) => a.agent_id.localeCompare(b.agent_id));
    body.duration_ms = Math.max(0, ...body.results.map((r) => r.response_time_ms || 0));
  }
  await context.route("**/api/proxy/**", async (route) => {
    const request = route.request();
    if (request.method() !== "GET" && !request.url().endsWith("/api/mesh-test")) {
      return route.continue();
    }
    const response = await route.fetch();
    const type = response.headers()["content-type"] || "";
    if (!type.includes("application/json")) return route.fulfill({ response });
    let body;
    try {
      body = await response.json();
    } catch {
      return route.fulfill({ response });
    }
    const url = new URL(request.url());
    if (url.pathname.endsWith("/api/topology")) normalizeTopology(body);
    if (url.pathname.endsWith("/api/mesh-test")) normalizeMeshTest(body);
    normalizeLive(body);
    await route.fulfill({ response, json: body });
  });
  context.setDefaultNavigationTimeout(NAV_TIMEOUT);

  // Clear UI state that Muti Metroo Manager persists in localStorage so
  // every navigation starts from the same centered map, default panel
  // width, and OS-derived (light) theme. Keys from the Manager frontend:
  // MetroMap.tsx, usePanelWidth.ts, and theme.ts. Tabs live in the URL.
  // Install the freeze stylesheet before the page's first paint, so no
  // view is restyled after it has rendered.
  await context.addInitScript((css) => {
    const install = () => {
      const style = document.createElement("style");
      style.textContent = css;
      document.documentElement.appendChild(style);
    };
    if (document.documentElement) install();
    else document.addEventListener("DOMContentLoaded", install, { once: true });
  }, FREEZE_CSS);
  await context.addInitScript(() => {
    try {
      localStorage.removeItem("muti-metroo-manager.map-view");
      localStorage.removeItem("muti-metroo-manager.agent-panel-width");
      localStorage.removeItem("mutiMetrooTheme");
    } catch {
      /* ignore */
    }
  });

  let page;
  // Each shot gets a fresh page, so no layout or compositor state carries
  // over from the previous shot.
  async function freshPage() {
    if (page) await page.close();
    page = await context.newPage();
    instrumentScreenshot(page);
  }

  // Wait for the rendering to settle before writing a shot. Chromium
  // re-rasterizes anti-aliased edges (map stations, rounded table frames)
  // a second or two after the page loads or the layout changes, and the
  // timing varies, so a fixed delay cannot guarantee the same pixels. Take
  // screenshots until three in a row are byte-identical, then write that
  // one.
  function instrumentScreenshot(p) {
    const rawScreenshot = p.screenshot.bind(p);
    p.screenshot = async (options = {}) => {
      const { path: outPath, ...rest } = options;
      await p.evaluate(() => document.fonts.ready);
      let previous = await rawScreenshot(rest);
      let stable = 1;
      for (let attempt = 0; attempt < 20 && stable < 3; attempt++) {
        await p.waitForTimeout(1200);
        const current = await rawScreenshot(rest);
        stable = current.equals(previous) ? stable + 1 : 1;
        previous = current;
      }
      if (stable < 3) throw new Error("rendering did not settle");
      if (outPath) await fs.writeFile(outPath, previous);
      return previous;
    };
  }
  await freshPage();

  // Pick agent IDs deterministically (lexicographically lowest id) so
  // the ?agent=<id> deep-link in the panel shots is stable across runs.
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
    shellId,
    byName,
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
  if (shellId) console.log(`Using agent short_id=${shellId} for shell/files.`);

  // Fixture agents by display name, for shots that need a specific one.
  function agentNamed(name) {
    const a = byName.get(name);
    if (!a) throw new Error(`fixture agent "${name}" not in topology`);
    return a;
  }

  const captured = [];
  const skipped = [];

  // ONLY=01-topology,07-routes-table captures just those shots.
  const only = process.env.ONLY ? new Set(process.env.ONLY.split(",")) : null;

  async function capture(name, fn) {
    if (only && !only.has(name)) return;
    const filename = `${name}.png`;
    const out = path.join(OUTPUT_DIR, filename);
    try {
      await freshPage();
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
    await page.waitForSelector(".metro-svg .station", {
      timeout: STEP_TIMEOUT,
    });
    // The footer's mesh summary arrives with the first mesh-test result
    // after load; wait for it so every shot carries the same footer.
    await page
      .locator("footer", { hasText: /Mesh: \d+\/\d+ reachable/ })
      .first()
      .waitFor({ timeout: STEP_TIMEOUT * 2 })
      .catch(() => {});
  }

  // The major tab strip is the first tablist inside <main>; the agent
  // panel and in-tab sub-strips render additional tablists elsewhere.
  function mainTab(label) {
    return page
      .locator("main > div [role=tablist]")
      .first()
      .locator("button[role=tab]", { hasText: new RegExp(`^${label}$`) })
      .first();
  }

  // Tabs live in the URL. Navigating there directly, instead of clicking
  // the tab, renders the view in one pass: a client-side tab switch leaves
  // rounded table frames rasterized slightly differently from run to run.
  const TAB_PATHS = {
    Routes: "/routes/active",
    "Test routing": "/routes/test",
    "Port Forwarding": "/forwards",
    DNS: "/dns/hosts",
    TLDs: "/dns/tlds",
    Downloads: "/downloads",
  };
  async function switchMainTab(label) {
    const tabPath = TAB_PATHS[label];
    if (!tabPath) {
      await mainTab(label).click();
      return;
    }
    await page.goto(`${BASE_URL}${tabPath}`, { waitUntil: "domcontentloaded" });
    const parent = { "Test routing": "Routes", TLDs: "DNS" }[label] || label;
    await mainTab(parent).waitFor({ timeout: STEP_TIMEOUT });
    await page
      .locator("footer", { hasText: /Mesh: \d+\/\d+ reachable/ })
      .first()
      .waitFor({ timeout: STEP_TIMEOUT * 2 })
      .catch(() => {});
  }

  async function openAgentPanel(id = mainId, panel) {
    const url = `${BASE_URL}/topology?agent=${id}${panel ? `&panel=${panel}` : ""}`;
    await page.goto(url, { waitUntil: "domcontentloaded" });
    await page.waitForSelector(".metro-svg .station", {
      timeout: STEP_TIMEOUT,
    });
    await page.waitForSelector(".agent-panel", { timeout: STEP_TIMEOUT });
  }

  async function switchAgentTab(label) {
    await page
      .locator(".agent-panel [role=tablist]")
      .first()
      .locator("button[role=tab]", { hasText: new RegExp(`^${label}$`) })
      .first()
      .click();
  }

  // Manager runs its first mesh test shortly after start, and every shot
  // carries the footer's "Mesh: N/M reachable" line. Wait until that line
  // shows the whole mesh reachable, re-running the test from the actions
  // menu if the first pass caught a link mid-handshake.
  async function ensureMeshHealthy() {
    const healthy = /Mesh: (\d+)\/\1 reachable/;
    for (let attempt = 0; attempt < 5; attempt++) {
      await openTopology();
      const ok = await page
        .locator("footer", { hasText: healthy })
        .first()
        .waitFor({ timeout: STEP_TIMEOUT * 2 })
        .then(() => true)
        .catch(() => false);
      if (ok) return;
      await page.locator('button[aria-label="Actions"]').first().click();
      await page
        .locator('[role="menuitem"]', { hasText: /Mesh connectivity test/ })
        .first()
        .click();
      await page
        .locator('[role="dialog"]', { hasText: "Mesh test results" })
        .first()
        .waitFor({ timeout: STEP_TIMEOUT * 3 });
      await page.waitForTimeout(2500);
    }
    throw new Error("mesh never reported all agents reachable");
  }
  await ensureMeshHealthy();

  // Manager defaults the displayed agent API and SOCKS addresses to the
  // browser's own host:port, which here is the rig's loopback port. Set
  // the overrides an operator would configure for a public deployment so
  // the Info tab (03) and the Settings dialog (23) show realistic values;
  // the previous settings are restored at the end of the run.
  const savedSettings = await managerJson(context, "GET", "/api/settings");
  await managerJson(context, "PUT", "/api/settings", {
    api_address: "http://gateway.example.net:8080",
    socks_address: "gateway.example.net:1080",
    sounds_enabled: savedSettings.sounds_enabled,
  });

  // 01 - Topology map, no panel, default view.
  await capture("01-topology", async (out) => {
    await openTopology();
    await page.screenshot({ path: out });
  });

  // 02 - Topology with the in-corner search dropdown visible.
  await capture("02-topology-search", async (out) => {
    await openTopology();
    const input = page.locator('input[placeholder^="Search agents"]').first();
    // The search field starts collapsed (w-0); the icon button expands it.
    await page
      .locator('button:has(span[class*="tabler--search"])')
      .first()
      .click();
    await input.click();
    await input.fill("a");
    await page.waitForSelector('ul[role="listbox"]', {
      timeout: STEP_TIMEOUT,
    });
    // Park the pointer away from the clear button so no hover state shows.
    await page.mouse.move(800, 860);
    await page.screenshot({ path: out });
  });

  // 03 - Agent panel, Info tab. Shows notes textarea and icon picker.
  await capture("03-agent-info", async (out) => {
    await openTopology();
    await openAgentPanel();
    await switchAgentTab("Info");
    await page.waitForSelector(
      '.agent-panel textarea[placeholder^="Free-form notes"]',
      { timeout: STEP_TIMEOUT },
    );
    await page.screenshot({ path: out });
  });

  // 04 - Connectivity tab, Peers sub-tab (the default view of the tab).
  await capture("04-agent-peers", async (out) => {
    await openTopology();
    await openAgentPanel();
    await switchAgentTab("Connectivity");
    await page.waitForSelector(".agent-panel .routes-tab", {
      timeout: STEP_TIMEOUT,
    });
    await page
      .waitForSelector(".agent-panel .ui-loading", { state: "detached" })
      .catch(() => {});
    await page.screenshot({ path: out });
  });

  // 05 - Routes tab on the agent panel of exit-cloud, whose two config
  // routes (cloud VPC and a default route) fill the list. Waits for a
  // route row, so the "Loading routes..." placeholder is gone.
  await capture("05-agent-routes", async (out) => {
    await openTopology();
    await openAgentPanel(agentNamed("exit-cloud").shortId);
    await switchAgentTab("Routes");
    await page
      .locator(".agent-panel .routes-list-item", { hasText: "172.31.0.0/16" })
      .first()
      .waitFor({ timeout: STEP_TIMEOUT });
    await page.screenshot({ path: out });
  });

  // 06 - Forwards tab with listener/endpoint subtab bar visible. Seed a
  // paired listener+endpoint so the table is non-empty, regardless of
  // whether the operator has any persistent forwards configured. Opens the
  // panel on the secondary agent (where the LISTENER landed) so the
  // default Listeners sub-tab shows a populated row.
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
      // Listeners sub-tab shows the seeded entry. Fall back to mainId
      // when there is no secondary agent.
      const panelId = secondaryId || mainId;
      await openAgentPanel(panelId);
      await switchAgentTab("Forwards");
      await page.waitForSelector(
        '.agent-panel [role=tablist] button[role=tab]:has-text("Listeners")',
        { timeout: STEP_TIMEOUT },
      );
      await page
        .waitForSelector(".agent-panel .ui-loading", { state: "detached" })
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
    await page.waitForSelector(".ui-table-frame, .ui-empty", {
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
      await page.waitForSelector(".ui-table-frame, .ui-empty", {
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

  // 10 - Management commands on the first agent that offers shells
  // (exit-datacenter in the fixture). Starts bash and runs a command so the
  // shot shows output rather than a bare prompt.
  await capture("10-agent-shell", async (out) => {
    const targetId = shellId;
    if (!targetId) throw new Error("no agent with shell enabled in topology");
    await openTopology();
    await openAgentPanel(targetId);
    const shellTab = page
      .locator(".agent-panel [role=tablist]")
      .first()
      .locator("button[role=tab]", { hasText: /^Shell$/ })
      .first();
    if ((await shellTab.getAttribute("disabled")) !== null) {
      throw new Error("shell tab is disabled (no management signing key?)");
    }
    await shellTab.click();
    const picker = page.locator(
      ".agent-panel .ui-section button:has(span[class*='tabler--terminal'])",
    );
    await picker.first().waitFor({ timeout: STEP_TIMEOUT });
    const bash = picker.filter({ hasText: /^bash$|^\/bin\/bash$/ });
    if ((await bash.count()) > 0) await bash.first().click();
    else await picker.first().click();
    await page.waitForSelector(".agent-panel .shell-terminal", {
      timeout: STEP_TIMEOUT,
    });
    await page.waitForSelector(".agent-panel .text-success", {
      timeout: STEP_TIMEOUT,
    });
    // Give the remote PTY a moment to render its prompt, then run the
    // command.
    await page.waitForTimeout(1500);
    await page.locator(".agent-panel .shell-terminal").click();
    await page.keyboard.type("uname -sm && ls -l /srv/files/reports\n");
    await page.waitForTimeout(1500);
    await page.screenshot({ path: out });
  });

  // 11 - File browser on the same agent, showing a directory listing
  // rather than the roots selector. Same capability requirements as the
  // shell.
  await capture("11-agent-files", async (out) => {
    const targetId = shellId;
    if (!targetId) throw new Error("no agent with file transfer enabled in topology");
    await openTopology();
    await openAgentPanel(targetId);
    const filesTab = page
      .locator(".agent-panel [role=tablist]")
      .first()
      .locator("button[role=tab]", { hasText: /^Files$/ })
      .first();
    if ((await filesTab.getAttribute("disabled")) !== null) {
      throw new Error("files tab is disabled (no management signing key?)");
    }
    await filesTab.click();
    // With several allowed roots (or a Windows drive list) the tab opens
    // on a "Browsable paths" picker; with a single root (the fixture's
    // /srv/files) it opens that directory directly. Pick the first root
    // only when the picker is what is on screen.
    const picker = page
      .locator(".agent-panel .ui-section-header", { hasText: /^Browsable paths$/ })
      .first();
    if (await picker.waitFor({ timeout: 2000 }).then(() => true).catch(() => false)) {
      await page.locator(".agent-panel .routes-list-item").first().click();
    }
    // Wait for the directory listing to land and the loading placeholder
    // to clear before snapping.
    await page.waitForSelector(
      ".agent-panel .ui-table-frame, .agent-panel .ui-empty, .agent-panel .ui-error",
      { timeout: STEP_TIMEOUT },
    );
    await page
      .waitForSelector(".agent-panel .ui-loading", { state: "detached" })
      .catch(() => {});
    await page.screenshot({ path: out });
  });

  // 12 - Top-level DNS tab > Hosts. Seed two entries via /hosts/manage so
  // the table has content; cleanup runs in the finally block to keep
  // re-runs idempotent.
  await capture("12-domains-tab", async (out) => {
    await seedDomain(context, "db.mesh", {
      ipv4: "10.20.0.15",
      ipv6: "fd00::15",
      comment: "datacenter database",
    });
    await seedDomain(context, "build.mesh", {
      ipv4: "172.31.4.20",
      comment: "cloud build runner",
    });
    try {
      await openTopology();
      await switchMainTab("DNS");
      // Wait for the seeded row: proves the table finished loading and
      // is not the empty or "Loading..." state.
      await page
        .locator("td", { hasText: "db.mesh" })
        .first()
        .waitFor({ timeout: STEP_TIMEOUT });
      await page.screenshot({ path: out });
    } finally {
      await unseedDomain(context, "db.mesh");
      await unseedDomain(context, "build.mesh");
    }
  });

  // 13 - Routes major tab -> Test routing sub-tab with a populated trace.
  // Reuses a seeded mesh-host so the captured trace shows the
  // resolution=mesh path that's the headline of the new feature.
  await capture("13-routes-test-routing", async (out) => {
    await seedDomain(context, "db.mesh", { ipv4: "10.20.0.15" });
    try {
      await openTopology();
      await switchMainTab("Test routing");
      const input = page
        .locator('input[placeholder^="IP address or hostname"]')
        .first();
      await input.fill("db.mesh");
      await page
        .locator("button", { hasText: /^Trace$/ })
        .first()
        .click();
      await page
        .locator(".ui-section", { hasText: "Resolution:" })
        .first()
        .waitFor({ timeout: STEP_TIMEOUT });
      await page.screenshot({ path: out });
    } finally {
      await unseedDomain(context, "db.mesh");
    }
  });

  // 09 - Footer mesh-health indicator. Clipped to the footer bbox so the
  // screenshot focuses on the "Mesh: N/M reachable" summary.
  await capture("09-footer-mesh-health", async (out) => {
    await openTopology();
    const footer = page.locator("footer").first();
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
    const tab = mainTab("Downloads");
    if ((await tab.count()) === 0) {
      throw new Error(
        "Downloads tab not present (no downloads bundle deployed)",
      );
    }
    await switchMainTab("Downloads");
    // The downloads listing renders one section per binary group with a
    // plain table each; wait until at least one artifact row is visible.
    await page.waitForSelector("main section table tbody tr", {
      timeout: STEP_TIMEOUT,
    });
    await page.screenshot({ path: out });
  });

  // 15 - Mesh Connectivity Test results dialog. Triggered from the actions
  // menu. Waits for the results dialog so the capture shows real latency
  // rows, not the running spinner.
  await capture("15-mesh-test-modal", async (out) => {
    await openTopology();
    await page.locator('button[aria-label="Actions"]').first().click();
    await page.waitForSelector('[role="menu"]', { timeout: STEP_TIMEOUT });
    await page
      .locator('[role="menuitem"]', {
        hasText: /Mesh connectivity test|Testing\.\.\./,
      })
      .first()
      .click();
    await page
      .locator('[role="dialog"]', { hasText: "Mesh test results" })
      .first()
      .waitFor({ timeout: STEP_TIMEOUT * 3 });
    // Give the in-flight test a moment to land before snapping; mesh-test
    // probes are quick (~1s/pair) but bursty.
    await page.waitForTimeout(2500);
    await page.screenshot({ path: out });
    // Close the dialog so subsequent shots are not contaminated.
    await page
      .locator('[role="dialog"] button[aria-label="Close"]')
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
    // exit-cloud inside the fixture network: the RTT is set by the fixed
    // netem delays, not by an internet path, so it is the same every run.
    const destIp = "10.99.0.40";
    let success = false;
    let lastError = null;
    for (const candidate of candidates) {
      try {
        await openTopology();
        await openAgentPanel(candidate);
        const pingTab = page
          .locator(".agent-panel [role=tablist]")
          .first()
          .locator("button[role=tab]", { hasText: /^Ping$/ })
          .first();
        if ((await pingTab.getAttribute("disabled")) !== null) {
          lastError = `ping tab disabled on ${candidate}`;
          continue;
        }
        await pingTab.click();
        const input = page
          .locator('.agent-panel input[placeholder^="IP address"]')
          .first();
        await input.waitFor({ timeout: STEP_TIMEOUT });
        await input.fill(destIp);
        await page
          .locator(".agent-panel button", { hasText: /^Start$/ })
          .first()
          .click();
        // Let the default 10-echo run finish, so the shot always shows ten
        // replies and the Stopped state. If the agent's ICMP socket fails
        // to open no replies land; try the next candidate.
        const ok = await page
          .waitForFunction(
            () => {
              const rows = document.querySelectorAll(
                ".agent-panel .ui-table-frame tbody tr",
              );
              let replies = 0;
              for (const row of rows) if (/reply/.test(row.textContent)) replies += 1;
              const panel = document.querySelector(".agent-panel");
              return replies >= 10 && /Stopped/.test(panel.textContent);
            },
            undefined,
            { timeout: STEP_TIMEOUT * 4 },
          )
          .then(() => true)
          .catch(() => false);
        if (ok) {
          // Manager times each echo in the browser with Date.now(), which
          // is frozen for these shots, and the values would be live
          // measurements otherwise. Show the fixture's nominal RTT for this
          // path (transit 6 ms + exit-cloud 70 ms of netem delay) instead.
          await page.evaluate(() => {
            const panel = document.querySelector(".agent-panel");
            const walker = document.createTreeWalker(panel, NodeFilter.SHOW_TEXT);
            for (let n = walker.nextNode(); n; n = walker.nextNode()) {
              if (/^\s*\d+(\.\d+)?ms\s*$/.test(n.textContent)) n.textContent = "76.0ms";
            }
          });
          await page.screenshot({ path: out });
          success = true;
          break;
        }
        // Stop and move on to the next candidate.
        lastError = `no successful echoes on ${candidate}`;
        await page
          .locator(".agent-panel button", { hasText: /^Stop$/ })
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
  // table to populate (or the empty state) so the screenshot is not the
  // "Loading..." state.
  await capture("17-dns-tlds", async (out) => {
    await openTopology();
    await switchMainTab("TLDs");
    // The loading placeholder itself is a .ui-empty cell, so key off the
    // section text: done when "Loading..." is gone (rows or empty state).
    await page.waitForFunction(
      () => {
        const section = document.querySelector("main section");
        if (!section) return false;
        return !section.textContent.includes("Loading");
      },
      undefined,
      { timeout: STEP_TIMEOUT * 2 },
    );
    await page.screenshot({ path: out });
  });

  // 18 - Mutiauk config wizard, first step (TUN interface). Opened from
  // the actions menu's "Generate Mutiauk config..." item; the wizard is an
  // overlay, so the dashboard is dimmed behind it.
  await capture("18-config-wizard-entry", async (out) => {
    await openTopology();
    await page.locator('button[aria-label="Actions"]').first().click();
    await page.waitForSelector('[role="menu"]', { timeout: STEP_TIMEOUT });
    await page
      .locator('[role="menuitem"]', { hasText: /Generate Mutiauk config/ })
      .first()
      .click();
    // First step is "TUN interface" - confirm the heading is present.
    await page
      .locator(".ui-overlay h3", { hasText: /^TUN interface$/ })
      .first()
      .waitFor({ timeout: STEP_TIMEOUT });
    await page.screenshot({ path: out });
    // Close the overlay so subsequent shots are clean.
    await page
      .locator('.ui-overlay button[aria-label="Close"]')
      .first()
      .click()
      .catch(() => page.keyboard.press("Escape").catch(() => {}));
  });

  async function openActionsMenu() {
    await page.locator('button[aria-label="Actions"]').first().click();
    await page.waitForSelector('[role="menu"]', { timeout: STEP_TIMEOUT });
  }

  function menuItem(text) {
    return page.locator('[role="menuitem"]', { hasText: text }).first();
  }

  // 19 - Connectivity tab > Listeners sub-tab on gateway, which has a QUIC
  // and a WebSocket listener, both advertised.
  await capture("19-agent-listeners", async (out) => {
    const gateway = agentNamed("gateway");
    await openTopology();
    await openAgentPanel(gateway.shortId);
    await switchAgentTab("Connectivity");
    await page
      .locator(".agent-panel [role=tablist]")
      .nth(1)
      .locator("button[role=tab]", { hasText: /^Listeners$/ })
      .first()
      .click();
    await page
      .locator(".agent-panel .peers-address", { hasText: ":8443" })
      .first()
      .waitFor({ timeout: STEP_TIMEOUT });
    await page.screenshot({ path: out });
  });

  // 20 - Edit route dialog for the datacenter LAN route on exit-datacenter,
  // with a source IP and egress interface typed in but not saved.
  await capture("20-route-edit-dialog", async (out) => {
    const exitDc = agentNamed("exit-datacenter");
    await openTopology();
    await openAgentPanel(exitDc.shortId);
    await switchAgentTab("Routes");
    const row = page
      .locator(".agent-panel .routes-list-item", { hasText: "10.20.0.0/16" })
      .first();
    await row.waitFor({ timeout: STEP_TIMEOUT });
    await row.locator("button", { hasText: /^Edit$/ }).click();
    const dialog = page.locator('[role="dialog"][aria-label="Edit route"]');
    await dialog.waitFor({ timeout: STEP_TIMEOUT });
    const inputs = dialog.locator("input");
    await inputs.nth(1).fill("10.99.0.30");
    await inputs.nth(2).fill("eth0");
    // Close the suggestion dropdown by clicking a neutral spot.
    await dialog.locator(".font-mono.break-all").first().click();
    await page.screenshot({ path: out });
    await dialog.locator("button", { hasText: /^Cancel$/ }).click();
    await dialog.waitFor({ state: "detached" });
  });

  // 21 - Topology with network boxes. Network membership is Manager's own
  // per-agent metadata; set it for this shot and clear it afterwards so
  // the other topology shots keep their plain layout. The stations are
  // also pinned to the default grid positions (as if an operator had
  // dragged them there): left to itself, the layout pulls transit and
  // exit-datacenter so close together that their names overlap.
  await capture("21-topology-networks", async (out) => {
    const membership = {
      transit: "datacenter",
      "exit-datacenter": "datacenter",
      "exit-cloud": "cloud",
    };
    const grid = {
      gateway: { x: 0, y: 0 },
      transit: { x: 200, y: 0 },
      "exit-datacenter": { x: 400, y: 0 },
      "exit-cloud": { x: 200, y: 200 },
    };
    const savedPositions = await managerJson(context, "GET", "/api/positions");
    try {
      const pins = {};
      for (const [name, pos] of Object.entries(grid)) pins[agentNamed(name).shortId] = pos;
      await managerJson(context, "PUT", "/api/positions", pins);
      for (const [name, networks] of Object.entries(membership)) {
        await putAgentMeta(context, agentNamed(name).fullId, { networks });
      }
      await openTopology();
      await page.waitForSelector('.metro-svg .network-box[data-network-id="cloud"]', {
        timeout: STEP_TIMEOUT,
      });
      await page.screenshot({ path: out });
    } finally {
      for (const name of Object.keys(membership)) {
        const a = byName.get(name);
        if (a) await putAgentMeta(context, a.fullId, { networks: "" });
      }
      await managerJson(context, "PUT", "/api/positions", savedPositions);
    }
  });

  // 22 - Actions (three dots) menu open on the topology view. Waits for
  // the sleep item so the menu shows every entry the fixture enables.
  await capture("22-actions-menu", async (out) => {
    await openTopology();
    await openActionsMenu();
    await menuItem(/^Suspend mesh$/).waitFor({ timeout: STEP_TIMEOUT });
    await page.screenshot({ path: out });
    await page.keyboard.press("Escape");
  });

  // 23 - Manager settings dialog, opened from the actions menu, showing
  // the address overrides set at the start of the run.
  await capture("23-settings-dialog", async (out) => {
    await openTopology();
    await openActionsMenu();
    await menuItem(/^Settings\.\.\.$/).click();
    const dialog = page.locator(
      '[role="dialog"][aria-label="Muti Metroo Manager settings"]',
    );
    await dialog.waitFor({ timeout: STEP_TIMEOUT });
    await page.waitForFunction(
      () => document.querySelector("#settings-socks")?.value === "gateway.example.net:1080",
      null,
      { timeout: STEP_TIMEOUT },
    );
    await page.screenshot({ path: out });
    await page.keyboard.press("Escape");
  });

  // 24 - Agent config wizard walked to its Review & download step for a
  // new branch-office agent: QUIC listener with a public address, transit
  // picked from the advertised dial targets (the wizard lists remote
  // agents only, so gateway is not offered), SOCKS5 on, one exit CIDR.
  await capture("24-agent-wizard-review", async (out) => {
    await openTopology();
    await openActionsMenu();
    await menuItem(/^Generate Muti Metroo agent config/).click();
    const wizard = page.locator(
      '[role="dialog"][aria-label="Generate Muti Metroo agent config"]',
    );
    await wizard.waitFor({ timeout: STEP_TIMEOUT });
    const heading = (text) =>
      wizard.locator(".wizard-pane h3", { hasText: text }).first().waitFor();
    const next = () => wizard.locator("button", { hasText: /^Next$/ }).click();
    const checkbox = (label) =>
      wizard.locator("label", { hasText: label }).locator('input[type="checkbox"]');

    await heading(/^Basics$/);
    await wizard.locator('input[placeholder^="e.g. edge-frankfurt"]').fill("branch-office");
    await next();

    await heading(/^Network$/);
    await wizard
      .locator('input[placeholder="example.com:4433"]')
      .fill("branch.example.net:4433");
    await next();

    await heading(/^TLS$/);
    await next();

    await heading(/^Peers$/);
    const transitCandidate = wizard
      .locator(".wizard-peer-available-row", { hasText: "transit.example.net:4443" })
      .first();
    await transitCandidate.waitFor({ timeout: STEP_TIMEOUT });
    await transitCandidate.locator("button", { hasText: /^Use$/ }).click();
    // "Use" fills in transit's 32-hex agent ID, but the step validates the
    // field as 64 hex characters and refuses to advance (a Manager bug).
    // The ID is optional, so clear it to get past the step.
    await wizard
      .locator('input[placeholder^="expected agent id"]')
      .first()
      .fill("");
    await next();

    await heading(/^SOCKS4\/5 proxy$/);
    await checkbox(/Enable SOCKS4\/5 listener/).check();
    await next();

    await heading(/^Exit routes$/);
    await checkbox(/^Enable exit$/).check();
    await wizard.locator("button", { hasText: /^\+ Add CIDR$/ }).click();
    await wizard.locator('input[placeholder="10.0.0.0/8"]').first().fill("192.168.50.0/24");
    await next();

    await heading(/^Monitoring/);
    await next();
    await heading(/^Remote shell$/);
    await next();
    await heading(/^File transfer$/);
    await next();
    // Management keys are seeded from gateway's public keys.
    await heading(/^Management key$/);
    await next();

    await heading(/^Review & download$/);
    // The YAML box scrolls; show its end, where the listener, peer,
    // SOCKS5, and exit sections built from the inputs above are.
    const yaml = wizard.locator(".wizard-pane pre").first();
    await yaml.waitFor();
    // The fixture generates fresh keys on every run. Replace the public
    // keys in the YAML with fixed placeholder values so the shot is the
    // same each time.
    await yaml.evaluate((el) => {
      const placeholders = [
        "5f1e0c3a9b7d24e8a6c0f3b19d82e7a4c5b6f0e1d2c3b4a59687f0e1d2c3b4a5",
        "a4b3c2d1e0f9687a5b4c3d2e1f0a9b8c7d6e5f4a3b2c1d0e9f8a7b6c5d4e3f2a",
      ];
      const seen = new Map();
      const walker = document.createTreeWalker(el, NodeFilter.SHOW_TEXT);
      for (let node = walker.nextNode(); node; node = walker.nextNode()) {
        node.textContent = node.textContent.replace(/\b[0-9a-f]{64}\b/g, (key) => {
          if (!seen.has(key)) seen.set(key, placeholders[seen.size % placeholders.length]);
          return seen.get(key);
        });
      }
      el.scrollTop = el.scrollHeight;
    });
    await page.screenshot({ path: out });
    await wizard.locator('button[aria-label="Close"]').first().click();
  });

  // 26 - Suspend mesh confirmation. The confirmation replaces the actions
  // menu content; the shot is taken without confirming, then cancelled.
  await capture("26-sleep-confirm", async (out) => {
    await openTopology();
    await openActionsMenu();
    await menuItem(/^Suspend mesh$/).click();
    const confirm = page
      .locator('[role="menu"]', { hasText: "Put the mesh to sleep?" })
      .first();
    await confirm.waitFor({ timeout: STEP_TIMEOUT });
    await page.screenshot({ path: out });
    await confirm.locator("button", { hasText: /^Cancel$/ }).click();
  });

  // 25 - Topology in the dark theme, toggled with the header button.
  // Captured last; the init script clears the stored theme on every
  // navigation anyway, so no later page load would inherit it.
  await capture("25-topology-dark", async (out) => {
    await openTopology();
    await page.locator('button[aria-label="Switch to dark theme"]').click();
    await page.waitForSelector('html[data-theme="dark"]', { timeout: STEP_TIMEOUT });
    await page.screenshot({ path: out });
    await page.locator('button[aria-label="Switch to light theme"]').click();
  });

  await managerJson(context, "PUT", "/api/settings", {
    api_address: savedSettings.api_address,
    socks_address: savedSettings.socks_address,
    sounds_enabled: savedSettings.sounds_enabled,
  });

  console.log("");
  console.log(`Captured ${captured.length} screenshot(s) to ${OUTPUT_DIR}`);
  if (skipped.length) {
    console.log(`Skipped ${skipped.length}:`);
    for (const s of skipped) console.log(`  - ${s.filename}: ${s.reason}`);
  }

  await browser.close();

  // The fixture mesh has everything every shot needs, so a skipped shot
  // is a regression (usually a stale selector), not an optional extra.
  if (skipped.length) process.exit(1);
}

// JSON call against Manager's own API (not proxied to the agent).
async function managerJson(context, method, apiPath, data) {
  const resp = await context.request.fetch(`${BASE_URL}${apiPath}`, {
    method,
    headers: { "Content-Type": "application/json" },
    data,
    timeout: NAV_TIMEOUT,
  });
  if (!resp.ok()) {
    throw new Error(`${method} ${apiPath}: HTTP ${resp.status()} ${await resp.text()}`);
  }
  return resp.json();
}

// Manager's own per-agent metadata (note, icon, network membership). PUT
// replaces the record, so every field is sent; empty fields delete it.
async function putAgentMeta(context, agentFullId, { note = "", icon = "", networks = "" }) {
  const resp = await context.request.put(
    `${BASE_URL}/api/agent-meta/${encodeURIComponent(agentFullId)}`,
    {
      headers: { "Content-Type": "application/json" },
      data: { note, icon, networks },
      timeout: NAV_TIMEOUT,
    },
  );
  if (!resp.ok()) {
    throw new Error(`agent-meta ${agentFullId}: HTTP ${resp.status()} ${await resp.text()}`);
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
    throw new Error(`seed endpoint ${key}: HTTP ${resp.status()} ${await resp.text()}`);
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
    shellId: null,
    byName: new Map(),
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
    // Sorting full IDs makes every pick stable across runs; the fixture
    // IDs are chosen so gateway sorts first and transit second.
    const sorted = [...agents].sort((a, b) => a.id.localeCompare(b.id));
    // Agent with shell/file management enabled, for the shell/files shots.
    const withShells = sorted.find(
      (a) => Array.isArray(a.shells) && a.shells.length > 0,
    );
    // Two distinct agents so paired forward seeds (one listener + one
    // endpoint) land on different agents and produce a populated
    // forward-routes row in the dashboard.
    const main = sorted[0];
    const secondary = sorted.find((a) => a.id !== main.id) || null;
    // Ping candidates: Linux agents first. icmp_enabled=true is set from
    // config, but the runtime ICMP socket also depends on the kernel
    // net.ipv4.ping_group_range sysctl, so the capture step tries each.
    const linuxAgents = sorted.filter((a) => a.os === "linux");
    const ping = linuxAgents[0] || main;
    const byName = new Map(
      sorted.map((a) => [
        a.display_name,
        { shortId: a.short_id, fullId: a.id, isLocal: !!a.is_local },
      ]),
    );
    return {
      mainId: main.short_id,
      mainFullId: main.id,
      mainIsLocal: !!main.is_local,
      secondaryId: secondary ? secondary.short_id : null,
      secondaryFullId: secondary ? secondary.id : null,
      secondaryIsLocal: secondary ? !!secondary.is_local : false,
      pingId: ping ? ping.short_id : null,
      pingCandidates: [...linuxAgents, ...sorted.filter((a) => a.os !== "linux")].map(
        (a) => a.short_id,
      ),
      shellId: withShells ? withShells.short_id : null,
      byName,
    };
  } catch {
    return empty;
  }
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
