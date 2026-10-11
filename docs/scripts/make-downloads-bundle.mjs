// Build a Muti Metroo Manager downloads bundle from downloaded release assets.
//
// Produces the same layout as bundle-downloads.sh in the Manager repository
// (a flat zip with a manifest.json), from GitHub release assets instead of
// local dist/ directories, so the screenshot fixture can show a real
// Downloads tab.
//
// Usage: node make-downloads-bundle.mjs <asset-dir> <mm-version> <mutiauk-version> <out.zip>

import { createHash } from "node:crypto";
import { execFileSync } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

const [assetDir, mmVersion, maVersion, outZip] = process.argv.slice(2);
if (!outZip) {
  console.error(
    "usage: make-downloads-bundle.mjs <asset-dir> <mm-version> <mutiauk-version> <out.zip>",
  );
  process.exit(2);
}

// name | category | os | arch | type | download_name
const ENTRIES = [
  ["muti-metroo-darwin-arm64", "agent", "macOS", "arm64", "executable", "muti-metroo"],
  ["muti-metroo-darwin-amd64", "agent", "macOS", "amd64", "executable", "muti-metroo"],
  ["muti-metroo-linux-amd64", "agent", "Linux", "amd64", "executable", "muti-metroo"],
  ["muti-metroo-linux-arm64", "agent", "Linux", "arm64", "executable", "muti-metroo"],
  ["muti-metroo-windows-amd64.exe", "agent", "Windows", "amd64", "executable", "muti-metroo.exe"],
  ["muti-metroo-windows-arm64.exe", "agent", "Windows", "arm64", "executable", "muti-metroo.exe"],
  ["muti-metroo-windows-amd64.dll", "agent-dll", "Windows", "amd64", "dll", "muti-metroo.dll"],
  ["muti-metroo-windows-arm64.dll", "agent-dll", "Windows", "arm64", "dll", "muti-metroo.dll"],
  ["mutiauk-linux-amd64", "entry", "Linux", "amd64", "executable", "mutiauk"],
  ["mutiauk-linux-arm64", "entry", "Linux", "arm64", "executable", "mutiauk"],
  ["muti-metroo-user-manual.pdf", "manual", "", "", "", "user-manual.pdf"],
];

const mm = `v${mmVersion.replace(/^v/, "")}`;
const ma = `v${maVersion.replace(/^v/, "")}`;
const stage = fs.mkdtempSync(path.join(os.tmpdir(), "mm-bundle-"));
const files = [];

for (const [name, category, osName, arch, type, downloadName] of ENTRIES) {
  const src = path.join(assetDir, name);
  if (!fs.existsSync(src)) {
    console.warn(`  skipping ${name}: not in release`);
    continue;
  }
  const data = fs.readFileSync(src);
  fs.copyFileSync(src, path.join(stage, name));
  files.push({
    name,
    category,
    os: osName,
    arch,
    type,
    download_name: downloadName,
    size: data.length,
    sha256: createHash("sha256").update(data).digest("hex"),
    version: category === "entry" ? ma : mm,
  });
}

// A fixed timestamp keeps the manifest, and anything the UI derives from
// it, identical between runs of the same release pair.
fs.writeFileSync(
  path.join(stage, "manifest.json"),
  JSON.stringify(
    {
      muti_metroo_version: mm,
      entry_version: ma,
      generated_at: "2026-01-01T00:00:00Z",
      files,
    },
    null,
    2,
  ),
);

fs.rmSync(outZip, { force: true });
execFileSync("zip", ["-q", "-X", "-r", path.resolve(outZip), "."], { cwd: stage });
fs.rmSync(stage, { recursive: true, force: true });
console.log(`  wrote ${outZip} (${files.length} files)`);
