#!/usr/bin/env node
// subject: scripts/test-touched.sh, scripts/test-touched.ps1, scripts/test-touched-plan.js, scripts/test-buckets.js, scripts/test-buckets.json, scripts/check-test-buckets.js
//
// The diff-to-tests mapping of scripts/test-touched.sh, on fake changed-file lists. No browser, no go, no git: it builds
// the planner's index from this checkout once and asks it about diffs that never happened. Under a second.
//
//   node scripts/test-touched-selftest.js
//
// The `subject:` header above is how test-touched.sh knows to run this when any of those files change.

const fs = require("fs");
const path = require("path");
const { plan, buildIndex } = require("./test-touched-plan.js");

const ROOT = path.join(__dirname, "..");
const ix = buildIndex();
let bad = 0, ran = 0;
const check = (name, ok, got) => { ran++; if (!ok) { bad++; console.error("FAIL " + name + (got !== undefined ? ": " + JSON.stringify(got) : "")); } };
const P = (files, opts) => plan(files, Object.assign({ index: ix }, opts || {}));
const units = p => [...p.units.keys()];
const nodes = p => [...p.node.keys()];

// the index itself: the harness parsed into units, with weights
check("the harness has its units", Object.keys(ix.h.units).length > 100, Object.keys(ix.h.units).length);
check("bootClean is a unit", "bootClean" in ix.h.units);
check("the core pin group is read", ix.h.pins.some(g => g.includes("core") && g.includes("core2")), ix.h.pins);

// a Go file: its package, no tags, nothing else
let p = P(["internal/store/tasks.go"]);
check("go: the package", [...p.go.keys()].join() === "./internal/store", [...p.go.keys()]);
check("go: no tags", p.tags.length === 0, p.tags);
check("go: no units, no node", !p.units.size && !p.node.size, [units(p), nodes(p)]);

// an integration file adds the tag, by name and by build line
p = P(["internal/cli/hook_nopoll_integration_test.go", "internal/store/tasks.go"]);
check("integration: tag", p.tags.map(t => t.tag).join() === "integration", p.tags);
check("integration: both packages", [...p.go.keys()].sort().join() === "./internal/cli,./internal/store", [...p.go.keys()]);
const tagged = fs.readdirSync(path.join(ROOT, "internal/store")).find(f => f.endsWith("_test.go") && !f.includes("integration") &&
  /^\/\/go:build[^\n]*integration/m.test(fs.readFileSync(path.join(ROOT, "internal/store", f), "utf8").split("\n").slice(0, 5).join("\n")));
if (tagged) check("integration: by build line", P(["internal/store/" + tagged]).tags.length === 1);

// go.mod builds everything, a standalone script Go file runs with its test
check("go.mod: build ./...", !!P(["go.mod"]).gobuild);
p = P(["scripts/ci-report_test.go"]);
check("scripts go: file pair", p.gofiles.length === 1 && p.gofiles[0].files.join() === "scripts/ci-report.go,scripts/ci-report_test.go", p.gofiles);
check("scripts go: no package", !p.go.size, [...p.go.keys()]);

// a web file: bootClean, its own units, its node test, a syntax check
p = P(["internal/api/web/js/seen.js"]);
check("seen.js: bootClean", units(p)[0] === "bootClean", units(p));
check("seen.js: test-seen.js", nodes(p).includes("scripts/test-seen.js"), nodes(p));
check("seen.js: node --check", p.jscheck.includes("internal/api/web/js/seen.js"), p.jscheck);
check("seen.js: no go", !p.go.size);

p = P(["internal/api/web/js/pulls.js"]);
check("pulls.js: a pulls unit", units(p).some(u => /^pulls/.test(u)), units(p));
check("pulls.js: not the whole suite", p.units.size < 15, p.units.size);

p = P(["internal/api/web/js/toasts.js"]);
check("toasts.js: toast units", units(p).filter(u => /^toast/.test(u)).length >= 2, units(p));

p = P(["internal/api/web/js/cardrules.js"]);
check("cardrules.js: the node scripts that read or lift it",
  nodes(p).includes("scripts/test-notify-rules.js") && nodes(p).includes("scripts/test-sort-order.js"), nodes(p));

p = P(["internal/api/web/js/termcursor.js"]);
check("termcursor.js: test-term-cursor.js", nodes(p).includes("scripts/test-term-cursor.js"), nodes(p));

// a web file that maps to nothing: bootClean only, and said
p = P(["internal/api/web/working.gif"]);
check("gif: bootClean only", units(p).join() === "bootClean", units(p));
check("gif: warned", p.warnings.some(w => /working\.gif maps to no headless unit/.test(w)), p.warnings);

// notes under web are not code
p = P(["internal/api/web/AGENTS.md"]);
check("web notes: nothing", !p.units.size && p.skipped.length === 1, [units(p), p.skipped]);

// the budget is honoured and what it left out is said
p = P(["internal/api/web/index.html", "internal/api/web/css/growl.css"], { budget: 30000 });
check("budget: something left out", p.dropped.length > 0, p.dropped);
check("budget: spent within it, bootClean aside", p.unitMs - (ix.weights.bootClean || 0) <= 30000, p.unitMs);

// the harness: a section's lines run its unit, shared lines run bootClean and warn
const hl = fs.readFileSync(path.join(ROOT, "scripts/test-board-headless.js"), "utf8").split("\n");
const tw = hl.findIndex(l => /^async function termWearSection\(/.test(l)) + 1;
p = P(["scripts/test-board-headless.js"], { harnessLines: [[tw + 2, tw + 3]] });
check("harness section: termWear", units(p).includes("termWear") && !p.warnings.length, [units(p), p.warnings]);
p = P(["scripts/lib/mock.js", "scripts/lib/browser.js"]);
check("harness library: bootClean and a warning each", units(p).join() === "bootClean" && p.warnings.length === 2, [units(p), p.warnings]);
p = P(["scripts/test-board-headless.js"], { harnessLines: [[1, 1]] });
check("harness top: bootClean and a warning", units(p).join() === "bootClean" && p.warnings.length === 1, [units(p), p.warnings]);
p = P(["scripts/test-board-headless.js"]);
check("harness with no lines: bootClean and a warning", units(p).join() === "bootClean" && p.warnings.length === 1, [units(p), p.warnings]);
const inl = ix.h.units.gauto;
p = P(["scripts/test-board-headless.js"], { harnessLines: [[inl.start + 1, inl.start + 1]] });
check("harness inline block: gauto", units(p).includes("gauto"), units(p));
// a helper only a few units call runs those units
const few = Object.entries(ix.h.helpers).map(([fn, r]) => [fn, r, Object.keys(ix.unitTok).filter(u => ix.unitTok[u].has(fn))])
  .find(([, , us]) => us.length >= 1 && us.length <= 3);
if (few) {
  p = P(["scripts/test-board-headless.js"], { harnessLines: [[few[1].start, few[1].start]] });
  check("harness helper " + few[0] + ": its callers", few[2].every(u => units(p).includes(u)), [few[2], units(p)]);
}

// node scripts: the script itself, board-source.js consumers, and this file through its subject header
check("node script: itself", nodes(P(["scripts/test-sort-order.js"])).includes("scripts/test-sort-order.js"));
p = P(["scripts/board-source.js"]);
check("board-source: its readers and bootClean", nodes(p).includes("scripts/test-morph.js") && units(p).join() === "bootClean", [nodes(p), units(p)]);
for (const f of ["scripts/test-touched.sh", "scripts/test-touched-plan.js", "scripts/test-touched.ps1", "scripts/test-touched-selftest.js"]) {
  check(f + ": the selftest", nodes(P([f])).includes("scripts/test-touched-selftest.js"), nodes(P([f])));
}
check("the planner is not a test", !nodes(P(["internal/api/web/js/seen.js"])).includes("scripts/test-touched-plan.js"));
p = P(["scripts/check-board.sh"]);
check("a shell script: bash -n", p.shcheck.includes("scripts/check-board.sh"), p.shcheck);

// docs: nothing at all
p = P(["docs/agents.md", "AGENTS.md"]);
check("docs: nothing", !p.go.size && !p.units.size && !p.node.size && p.skipped.length === 2, p);

// DECLARED FIRST: one fake diff per bucket, so the map cannot rot. A file the bucket owns names the bucket
const buckets = require("./test-buckets.js");
const bk = buckets.load();
const ownedBy = (b, f) => { const o = buckets.ownersOfFile(bk, f); return o.length === 1 && o[0].bucket === b; };
const walkFiles = d => fs.readdirSync(path.join(ROOT, d), { withFileTypes: true }).flatMap(e =>
  e.isDirectory() ? (e.name === "vendor" || e.name === "icons" ? [] : walkFiles(d + "/" + e.name)) : [d + "/" + e.name]);
const allFiles = [...walkFiles("internal/api/web").filter(f => /\.(js|css|html)$/.test(f)),
  ...["internal/api", "internal/link", "internal/link/wire", "internal/daemon", "internal/store", "internal/hubstore", "internal/gitsync", "internal/claudeconf", "internal/ptyhost", "internal/cli", "internal/worktrees"]
    .flatMap(d => fs.readdirSync(path.join(ROOT, d)).filter(f => f.endsWith(".go") && !f.endsWith("_test.go")).map(f => d + "/" + f))];
for (const b of bk.buckets) {
  const f = allFiles.find(x => ownedBy(b, x));
  if (!f) { check("bucket " + b.name + ": owns a file", b.rest || (b.kind === "board" && !(b.sources || []).length), b.name); continue; }
  p = P([f]);
  check("bucket " + b.name + ": " + f + " selects it", p.buckets.has(b.name), [...p.buckets.keys()]);
  if (b.kind === "board") {
    const own = Object.keys(ix.h.units).filter(u => { const o = buckets.owners(bk, "units", u); return o.length === 1 && o[0].bucket === b; });
    check("bucket " + b.name + ": only its own units, and bootClean", units(p).every(u => u === "bootClean" || own.includes(u)), units(p));
    check("bucket " + b.name + ": bootClean", units(p)[0] === "bootClean", units(p));
  } else {
    check("bucket " + b.name + ": its package", p.go.has("./" + path.posix.dirname(f)), [...p.go.keys()]);
  }
}
// every unit name is in a bucket, so a unit run is always attributable
check("every unit is in one bucket", Object.keys(ix.h.units).every(u => buckets.owners(bk, "units", u).length === 1),
  Object.keys(ix.h.units).filter(u => buckets.owners(bk, "units", u).length !== 1));
// a shared file selects boot and the buckets that need it
p = P(["internal/api/web/js/cards.js"]);
check("shared cards.js: boot and what needs it", p.buckets.has("boot") && p.buckets.has("terminals"), [...p.buckets.keys()]);
p = P(["internal/api/web/js/core.js"]);
check("shared core.js: boot", p.buckets.has("boot") && p.buckets.has("cards"), [...p.buckets.keys()]);
// a bucket's node tests run with it
p = P(["internal/api/web/js/termcursor.js"]);
check("bucket tests: terminals runs test-term-trace.js", nodes(p).includes("scripts/test-term-trace.js"), nodes(p));
// a web file in no bucket falls back to the inference and says to add it
p = P(["internal/api/web/js/zzz-not-in-a-bucket.js"]);
check("no bucket: inferred and said", p.warnings.some(w => /in no bucket/.test(w)) && !p.buckets.size, [p.warnings, [...p.buckets.keys()]]);
// an api file and a link file name their bucket on the package
p = P(["internal/api/prsmove.go"]);
check("api/prs by file", p.buckets.has("api/prs") && p.go.has("./internal/api"), [...p.buckets.keys()]);
p = P(["internal/link/items_mcp_integration_test.go"]);
check("mcp/items by its test file", p.buckets.has("mcp/items"), [...p.buckets.keys()]);
// the lint: a new file or unit outside a bucket fails, the tree as it is passes
const { spawnSync } = require("child_process");
const lint = a => spawnSync(process.execPath, [path.join(__dirname, "check-test-buckets.js")].concat(a), { encoding: "utf8" });
check("lint: the tree is clean", lint([]).status === 0, lint([]).stderr);
let r = lint(["--probe-file", "internal/api/web/js/zzz-new.js"]);
check("lint: a new web file outside a bucket fails", r.status === 1 && /zzz-new\.js is in no bucket/.test(r.stderr), r.stderr);
r = lint(["--probe-file", "internal/link/zzz_new.go"]);
check("lint: a new link file outside a bucket fails", r.status === 1 && /zzz_new\.go is in no bucket/.test(r.stderr), r.stderr);
r = lint(["--probe-file", "internal/api/zzz_new.go"]);
check("lint: a new api file outside a bucket fails", r.status === 1 && /zzz_new\.go is in no bucket/.test(r.stderr), r.stderr);
r = lint(["--probe-unit", "zzzNewUnit"]);
check("lint: a new unit outside a bucket fails", r.status === 1 && /zzzNewUnit is in no bucket/.test(r.stderr), r.stderr);
r = lint(["--probe-unit", "termZzz"]);
check("lint: a unit matching one glob passes", r.status === 0, r.stderr);
r = lint(["--probe-go-test", "internal/api/zzz_new_integration_test.go"]);
check("lint: a new api test file in no bucket grows rest past its ceiling and fails", r.status === 1 && /api\/rest holds 30 test files, over its ceiling of 29/.test(r.stderr), r.stderr);
r = lint(["--probe-go-test", "internal/link/zzz_new_integration_test.go"]);
check("lint: a new link test file in no bucket fails", r.status === 1 && /hub\/rest holds 30 test files, over its ceiling/.test(r.stderr), r.stderr);
r = lint(["--probe-go-test", "internal/api/prs_zzz_integration_test.go"]);
check("lint: a new test file a bucket glob owns passes and leaves rest alone", r.status === 0, r.stderr);
r = spawnSync(process.execPath, [path.join(__dirname, "test-buckets.js"), "--go-run", "hub/backlog"], { encoding: "utf8" });
check("go bucket: --go-run prints the package and a -run regex of its tests", r.status === 0 && /^\.\/internal\/link\t\^\(Test\w+(\|Test\w+)*\)\$$/.test(r.stdout.trim()), r.stdout + r.stderr);

if (bad) { console.error("test-touched-selftest: " + bad + " of " + ran + " failed"); process.exit(1); }
console.log("test-touched-selftest ok, " + ran + " checks");
