#!/usr/bin/env node
// Proves the unit guard: a copy of the harness with one bare section call planted in main() must make
// `test-board-sharded.js --list` exit non-zero, and the real harness must list clean.
const { spawnSync } = require("child_process");
const fs = require("fs");
const path = require("path");
const HERE = __dirname;
const real = path.join(HERE, "test-board-headless.js");
const probe = path.join(HERE, "_unit-guard-probe.js");
const list = env => spawnSync(process.execPath, [path.join(HERE, "test-board-sharded.js"), "--list", "--local"], { env: Object.assign({}, process.env, env), encoding: "utf8" });
try {
  const src = fs.readFileSync(real, "utf8");
  const at = src.indexOf('    await unit("oneTooltip"');
  if (at < 0) { console.error("check-suite-units: no anchor unit to plant after"); process.exit(1); }
  const eol = src.indexOf("\n", at) + 1;
  fs.writeFileSync(probe, src.slice(0, eol) + "    await pasteStartSection(browser, base);\n" + src.slice(eol));
  const r = list({ BOARD_HEADLESS_FILE: probe });
  if (/playwright is not installed|chromium browser is not installed/.test(r.stdout)) { console.log("check-suite-units skipped: no playwright"); process.exit(0); }
  if (r.status === 0) { console.error("check-suite-units: a bare section in main() did not fail --list"); process.exit(1); }
  const ok = list({});
  if (ok.status !== 0) { console.error("check-suite-units: the real harness does not list clean:\n" + ok.stderr); process.exit(1); }
  console.log("check-suite-units ok");
} finally { try { fs.unlinkSync(probe); } catch (e) {} }
