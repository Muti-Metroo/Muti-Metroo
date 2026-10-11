// Verify every documentation page against the implementation.
//
// Builds muti-metroo from this checkout, then checks each page under
// docs/docs/:
//
//   commands  every `muti-metroo ...` line in a bash/shell/powershell code
//             block names a real command path, and every long flag it uses
//             exists on that command (or is a global flag)
//   configs   every complete agent config example (a yaml block whose
//             top-level keys are all config sections) passes
//             `muti-metroo config validate`; ${VAR} references are filled
//             with valid dummy values first
//   api       every agent HTTP path in the page (/health, /routes/manage,
//             /agents/{id}/..., ...) is registered by the agent
//   style     frontmatter title and description, ASCII only, no " -- "
//             separator in prose, ends with a See Also section
//
// Writes docs/VERIFICATION.md with one row per page and exits non-zero if
// any check fails.
//
// Usage: node docs/scripts/verify-docs.mjs

import { execFileSync, spawnSync } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

const here = path.dirname(fileURLToPath(import.meta.url));
const repo = path.resolve(here, "..", "..");
const docsDir = path.join(repo, "docs", "docs");
const tmp = fs.mkdtempSync(path.join(os.tmpdir(), "mm-verify-"));
const bin = path.join(tmp, "muti-metroo");

execFileSync("go", ["build", "-o", bin, "./cmd/muti-metroo"], { cwd: repo, stdio: "inherit" });

// --- Command tree from --help -------------------------------------------------

function help(args) {
  return spawnSync(bin, [...args, "--help"], { encoding: "utf8" }).stdout || "";
}
function parseSubcommands(text) {
  const out = [];
  let inSection = false;
  for (const line of text.split("\n")) {
    if (/^[A-Z][A-Za-z ]+:$/.test(line)) {
      // Cobra lists subcommands under "Available Commands" and, on the root
      // command, under the command group titles.
      inSection = /^(Available Commands|Additional Commands|Getting Started|Agent Status|Remote Operations|Administration):$/.test(line);
      continue;
    }
    const m = inSection && line.match(/^  ([a-z][a-z0-9-]*)\s/);
    if (m && m[1] !== "help") out.push(m[1]);
  }
  return out;
}
function parseFlags(text) {
  return new Set([...text.matchAll(/^\s+(?:-\w, )?(--[a-z0-9-]+)/gm)].map((m) => m[1]));
}
const tree = new Map(); // "route add" -> Set(flags incl. global)
function walk(pathParts) {
  if (tree.has(pathParts.join(" ")) || pathParts.length > 4) return;
  const text = help(pathParts);
  tree.set(pathParts.join(" "), parseFlags(text));
  for (const sub of parseSubcommands(text)) walk([...pathParts, sub]);
}
walk([]);
// Hidden deprecated aliases still work and are documented as deprecated.
for (const alias of ["peers", "routes"]) {
  if (!tree.has(alias)) tree.set(alias, parseFlags(help([alias])));
}

function checkCommandLine(line) {
  // Only the muti-metroo invocation itself: stop at redirects, pipes, and
  // command separators.
  line = line.split(/\s(?:>|>>|\||;|&&|\|\|)\s|\$\(/)[0];
  const tokens = line
    .replace(/^\s*(sudo\s+)?(\.\/|\.\\|[\w/\\.:-]*[\\/])?muti-metroo(\.exe)?\s*/, "")
    .split(/\s+/)
    .filter(Boolean);
  const parts = [];
  for (const t of tokens) {
    if (t.startsWith("-") || t.includes("=") || /[^a-z0-9-]/.test(t)) break;
    if (!tree.has([...parts, t].join(" "))) break;
    parts.push(t);
  }
  const flags = tree.get(parts.join(" "));
  const problems = [];
  // Everything after a bare "--" belongs to the remote command (shell).
  const own = tokens.slice(0, tokens.indexOf("--") === -1 ? undefined : tokens.indexOf("--"));
  for (const t of own) {
    const m = t.match(/^(--[a-z0-9-]+)/);
    if (m && !flags.has(m[1])) problems.push(`\`${parts.join(" ") || "(root)"}\` has no ${m[1]}`);
  }
  // `shell <target> <command> [args]`: muti-metroo parses every dash token
  // itself unless a bare "--" comes first, so `shell abc df -h` prints help
  // instead of running `df -h`.
  if (parts[0] === "shell") {
    const positional = [];
    for (let i = parts.length; i < tokens.length; i++) {
      const t = tokens[i];
      if (t === "--") break;
      if (t.startsWith("-")) {
        if (positional.length >= 2) {
          problems.push(`\`shell\` passes ${t} to muti-metroo, not to the remote command (add --)`);
          break;
        }
        // Skip the value of a flag that takes one (-a host, -w 30s, -t id).
        if (/^-[atw]$|^--(agent|timeout|target)$/.test(t)) i++;
        continue;
      }
      positional.push(t);
    }
  }
  const next = own.find((t, i) => i >= parts.length && !t.startsWith("-"));
  const subs = [...tree.keys()].filter((k) => k.startsWith(parts.join(" ") + " ") && k.split(" ").length === parts.length + 1);
  if (subs.length && next && /^[a-z][a-z-]+$/.test(next) && !tree.has([...parts, next].join(" ")) && !help(parts).includes("[flags]\n  muti-metroo " + parts.join(" ") + " [")) {
    // A parent with subcommands followed by an unknown lowercase word is a
    // made-up subcommand (positional arguments on leaf commands are fine).
    if (!/Usage:\n\s+muti-metroo [^\n]*<|\[/.test(help(parts)) || subs.length) {
      problems.push(`\`${[...parts, next].join(" ")}\` is not a command`);
    }
  }
  return problems;
}

// --- HTTP routes from the source ---------------------------------------------

const goSources = execFileSync("git", ["ls-files", "internal/*.go", "cmd/*.go"], { cwd: repo, encoding: "utf8" })
  .split("\n")
  .filter((f) => f && !f.endsWith("_test.go"))
  .map((f) => fs.readFileSync(path.join(repo, f), "utf8"))
  .join("\n");
const registered = new Set([...goSources.matchAll(/HandleFunc\("([^"]+)"/g)].map((m) => m[1].replace(/\/$/, "")));
const remoteSuffixes = new Set([
  ...[...goSources.matchAll(/parts\[1\] == "([a-z/_-]+)"/g)].map((m) => m[1]),
  ...[...goSources.matchAll(/HasPrefix\(parts\[1\], "([a-z/_-]+)"\)/g)].map((m) => m[1]),
  ...[...goSources.matchAll(/case "([a-z]+)":/g)].map((m) => m[1]),
]);
function knownPath(p) {
  const clean = p.replace(/[?#].*$/, "").replace(/\/$/, "");
  if (registered.has(clean)) return true;
  for (const r of registered) if (clean.startsWith(r + "/")) return true; // subtree handlers
  const m = clean.match(/^\/agents\/(?:\{[a-z_]+\}|[0-9a-f]{32}|\$[A-Z_]+|<[^>]+>)(?:\/(.+))?$/);
  if (m) return !m[1] || remoteSuffixes.has(m[1]) || [...remoteSuffixes].some((s) => m[1].startsWith(s + "/"));
  return false;
}
const AGENT_PREFIXES = /^\/(health|healthz|ready|agents|routes|forward|peers|listeners|display-name|hosts|sleep|wake|shutdown|api|debug|file|shell|icmp|probe)\b/;

// --- Config examples ------------------------------------------------------------

const TOP_KEYS = new Set([
  "agent", "tls", "protocol", "listeners", "peers", "socks5", "exit", "routing", "connections",
  "limits", "http", "shell", "file_transfer", "udp", "icmp", "forward", "management", "sleep",
  "mesh_hosts", "mesh_hosts_suffixes", "default_action",
]);
const dummyEnv = {
  ...process.env,
};
function fillEnv(yaml) {
  // Give every ${VAR} a value of the right shape for where it appears.
  return yaml.replace(/\$\{([A-Z0-9_]+)(:-[^}]*)?\}/g, (whole, name, def) => {
    if (def) return def.slice(2);
    if (/SIGNING.*PRIV|SIGN.*PRIV/.test(name)) return "a".repeat(128);
    if (/KEY|PUB|PRIV/.test(name)) return "a".repeat(64);
    if (/PORT/.test(name)) return "4433";
    if (/HASH/.test(name)) return "$2a$12$abcdefghijklmnopqrstuuCq2C8RrlL9b6xQYpBpXc9pX4bXNnLfu";
    return "example";
  });
}
function validateConfig(yaml, n) {
  const file = path.join(tmp, `cfg-${n}.yaml`);
  fs.writeFileSync(file, fillEnv(yaml));
  const r = spawnSync(bin, ["config", "validate", file], { cwd: tmp, encoding: "utf8", env: dummyEnv });
  if (r.status === 0) return null;
  const msg = (r.stdout + r.stderr).split("\n").filter((l) => l.trim().startsWith("-") || /error/i.test(l)).slice(-2).join(" ");
  return msg.trim();
}

// --- Walk pages -------------------------------------------------------------------

function pages(dir) {
  return fs.readdirSync(dir, { withFileTypes: true }).flatMap((e) =>
    e.isDirectory() ? pages(path.join(dir, e.name)) : /\.mdx?$/.test(e.name) ? [path.join(dir, e.name)] : [],
  );
}

const rows = [];
let failures = 0;
let cfgCounter = 0;
for (const file of pages(docsDir).sort()) {
  const rel = path.relative(docsDir, file);
  const text = fs.readFileSync(file, "utf8");
  const problems = [];
  let commands = 0;
  let configs = 0;
  let apiPaths = 0;

  for (const block of text.matchAll(/```(\w*)[^\n]*\n([\s\S]*?)```/g)) {
    const [, lang, body] = block;
    if (/^(bash|sh|shell|powershell|ps1|cmd|console)$/.test(lang)) {
      for (const raw of body.split("\n")) {
        const line = raw.replace(/^\s*(\$|PS [^>]*>|>)\s*/, "");
        if (/^\s*(sudo\s+)?(\.\/|\.\\|[\w/\\.:-]*[\\/])?muti-metroo(\.exe)?(\s|$)/.test(line) && !/^\s*#/.test(line)) {
          commands += 1;
          problems.push(...checkCommandLine(line));
        }
      }
    }
    if (/^ya?ml$/.test(lang)) {
      const keys = [...body.matchAll(/^([a-z_0-9]+):/gm)].map((m) => m[1]);
      if (keys.length && keys.every((k) => TOP_KEYS.has(k))) {
        configs += 1;
        const err = validateConfig(body, cfgCounter++);
        if (err) problems.push(`config example invalid: ${err}`);
      }
    }
  }

  if (/^(api\/|configuration\/http|features\/|cli\/|troubleshooting\/)/.test(rel)) {
    const seen = new Set();
    for (const m of text.matchAll(/(?:https?:\/\/[^\s/`'")]+)?(\/[a-z][a-z0-9_{}<>$\/-]*)(?=[\s`'")?#]|$)/g)) {
      const p = m[1];
      if (!AGENT_PREFIXES.test(p) || seen.has(p)) continue;
      if (/^\/(api|routes|features|configuration|cli|deployment|security|troubleshooting|concepts|getting-started|img)\/[a-z-]+$/.test(p) && !registered.has(p)) {
        // Site-internal doc links such as /api/overview are not agent paths.
        if (!/^\/api\/(dashboard|nodes|topology|mesh-test|interfaces|setup-defaults|forward-routes)/.test(p)) continue;
      }
      seen.add(p);
      apiPaths += 1;
      if (!knownPath(p)) problems.push(`HTTP path ${p} is not registered by the agent`);
    }
  }

  const fm = text.match(/^---\n([\s\S]*?)\n---/);
  if (!fm || !/^title:/m.test(fm[1]) || !/^description:/m.test(fm[1])) problems.push("frontmatter needs title and description");
  if (/[^\x00-\x7F]/.test(text.replace(/OÜ/g, ""))) problems.push("non-ASCII characters");
  let inCode = false;
  for (const line of text.split("\n")) {
    if (line.startsWith("```")) inCode = !inCode;
    else if (!inCode && / -- /.test(line.replace(/`[^`]*`/g, "")) && !/^\s*\|/.test(line)) {
      problems.push("' -- ' separator in prose");
      break;
    }
  }
  if (!/^## See Also\s*$/m.test(text)) problems.push("no See Also section");

  if (problems.length) failures += 1;
  rows.push({ rel, commands, configs, apiPaths, problems });
}

// --- Report ------------------------------------------------------------------------

// The last commit that changed the agent's code, which is what was built.
const commit = execFileSync("git", ["log", "-1", "--format=%h", "--", "cmd", "internal", "go.mod"], {
  cwd: repo,
  encoding: "utf8",
}).trim();

const lines = [
  "# Documentation verification",
  "",
  "Generated by `node docs/scripts/verify-docs.mjs`. Do not edit by hand.",
  "",
  `Checked against the agent built from the code at commit \`${commit}\`: ${tree.size} CLI command paths, ${registered.size} registered HTTP routes.`,
  "",
  "| Page | Commands checked | Config examples validated | HTTP paths checked | Result |",
  "| ---- | ---------------: | ------------------------: | -----------------: | ------ |",
  ...rows.map(
    (r) =>
      `| \`${r.rel}\` | ${r.commands} | ${r.configs} | ${r.apiPaths} | ${r.problems.length ? "FAIL: " + r.problems.join("; ").replace(/\|/g, "\\|") : "pass"} |`,
  ),
  "",
  `${rows.length} pages, ${rows.length - failures} pass, ${failures} fail.`,
  "",
];
fs.writeFileSync(path.join(repo, "docs", "VERIFICATION.md"), lines.join("\n"));
fs.rmSync(tmp, { recursive: true, force: true });
for (const r of rows.filter((x) => x.problems.length)) console.log(`${r.rel}:\n  ${r.problems.join("\n  ")}`);
console.log(`${rows.length} pages, ${rows.length - failures} pass, ${failures} fail. Report: docs/VERIFICATION.md`);
process.exit(failures ? 1 : 0);
