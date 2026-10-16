#!/usr/bin/env node
// What a diff needs tested, and why. The planner behind scripts/test-touched.sh.
//
//   node scripts/test-touched-plan.js [--harness-lines 120-140,900-900] [--budget ms] < changed-files
//
// Reads changed paths, one per line, relative to the repository root. Prints the plan for a person on stderr and the
// actions on stdout, one per line, for test-touched.sh to run:
//
//   BUCKET <name> <files>  a declared bucket the diff touched (nothing to run, it says why the rest was picked)
//   GO <dir>            go test that package (./internal/store)
//   TAGS integration    an integration file changed, so the Go run takes -tags integration
//   GOFILES <a> <b>     a standalone Go file under scripts/ and its test, run as go test a b
//   GOVET <file>        a standalone Go file under scripts/ with no test of its own
//   GOBUILD ./...       go.mod or go.sum changed
//   UNITS a,b,c         the headless board units, for HEADLESS_UNITS (through test-board-sharded.js --units)
//   NODE <script>       a node test script to run
//   JSCHECK <file>      node --check
//   SHCHECK <file>      bash -n
//
// DECLARED FIRST. scripts/test-buckets.json says which bucket owns a file. A changed board file in a bucket's `sources`
// selects that bucket: its units (the ones the inference below ranks first, then the rest cheapest first) and its node
// `tests`, and the plan names the bucket. A shared file (core.js, boot.js, cards.js, index.html) selects `boot` and the
// buckets whose `needs` name it, and keeps the inference below as well, since what reads it is wider than a list. A
// Go file in a bucket's `sources` names the bucket on its package. Only a file in no bucket goes through the inference,
// and the plan says "add it to test-buckets.json". scripts/check-test-buckets.js fails when a file is in no bucket.
//
// THE BOARD MAP BELOW IS THE FALLBACK, READ OFF THE CODE, NOT KEPT BY HAND. A unit of scripts/test-board-headless.js is the text of its
// `await unit(...)` block plus the section function it calls (and the sections that one calls). A board file is the
// names it declares: its top-level functions and variables, `window.x =`, the element ids and classes it writes, and,
// for a stylesheet or a page, its selectors and ids. A unit is picked for a changed file when it names what the file
// declares, weighted so a name many units use counts for little and a name only this unit uses counts for a lot. A
// unit whose name carries the file's name gets a bonus. The node test scripts are mapped the same way, plus the files
// they read by name, plus an optional `// subject: a, b` header. bootClean runs with any board change. A changed web
// file that maps to no unit gets bootClean only, and a warning that says so.
//
// The units chosen are capped by a time budget, read from scripts/board-suite-weights.json, filled round robin so
// every changed file gets its best units first. What the budget left out is printed, never dropped silently.
//
// scripts/test-touched-selftest.js asserts this mapping on fake diffs.

const fs = require("fs");
const path = require("path");
const buckets = require("./test-buckets.js");

const ROOT = path.join(__dirname, "..");
const WEB = "internal/api/web/";
const HARNESS = "scripts/test-board-headless.js";
const DEFAULT_WEIGHT_MS = 8000;
const DEFAULT_BUDGET_MS = 150000;
// A name used by more than this share of the units says nothing about which one a change touches.
const COMMON_SHARE = 0.25;
// A harness helper used by more units than this is shared plumbing: bootClean stands in for it.
const HELPER_FANOUT = 8;

const read = (rel, root) => { try { return fs.readFileSync(path.join(root || ROOT, rel), "utf8"); } catch (e) { return null; } };
const tokens = s => new Set(s.match(/[A-Za-z_$][\w$-]*[\w$]/g) || []);
// a short plain word (init, life, done) is too common to say which unit a change touches
const isIdent = t => t.length >= 6 || (t.length >= 4 && /[A-Z0-9_$-]/.test(t));

// ── the harness ──────────────────────────────────────────────────────────────────────────────────────────────────
function parseHarness(src) {
  const lines = src.split("\n");
  const sections = {};   // name -> { start, end } 1-based, inclusive
  const helpers = {};    // top-level function or const name -> { start, end }
  for (let i = 0; i < lines.length; i++) {
    const m = /^(?:async\s+)?function\s+(\w+)\s*\(/.exec(lines[i]) || /^(?:const|let)\s+(\w+)\s*=/.exec(lines[i]);
    if (!m) continue;
    let end = i;
    if (/[{(\[]\s*$/.test(lines[i]) || /^(?:async\s+)?function/.test(lines[i])) {
      for (let j = i + 1; j < lines.length; j++) if (/^[})\]]/.test(lines[j])) { end = j; break; }
    }
    const r = { start: i + 1, end: end + 1 };
    if (/Section$/.test(m[1]) && /function/.test(lines[i])) sections[m[1]] = r; else helpers[m[1]] = r;
    i = end;
  }
  const units = {};      // name -> { section, start, end }
  const at = [];
  for (let i = 0; i < lines.length; i++) {
    const m = /^\s*await unit\("(\w+)",\s*(.*)$/.exec(lines[i]);
    if (m) at.push({ name: m[1], line: i, rest: m[2] });
  }
  for (let k = 0; k < at.length; k++) {
    const u = at[k];
    const s = /^(?:async\s*)?\(\)\s*=>\s*(\w+Section)\(/.exec(u.rest);
    if (s) { units[u.name] = { section: s[1], start: u.line + 1, end: u.line + 1 }; continue; }
    // an inline block runs to the next unit, or to the end of main's try
    let end = k + 1 < at.length ? at[k + 1].line - 1 : u.line;
    for (let j = u.line + 1; j < end; j++) if (/^  \} catch/.test(lines[j])) { end = j - 1; break; }
    units[u.name] = { section: null, start: u.line + 1, end: end + 1 };
  }
  const slice = r => lines.slice(r.start - 1, r.end).join("\n");
  // a unit's text: its own block, its section, and every section those call, once each
  const text = {};
  for (const [name, u] of Object.entries(units)) {
    const seen = new Set(), parts = [slice(u)];
    const queue = u.section ? [u.section] : [];
    for (const m of slice(u).matchAll(/\b(\w+Section)\(/g)) queue.push(m[1]);
    while (queue.length) {
      const s = queue.shift();
      if (seen.has(s) || !sections[s]) continue;
      seen.add(s);
      const body = slice(sections[s]);
      parts.push(body);
      for (const m of body.matchAll(/\b(\w+Section)\(/g)) queue.push(m[1]);
    }
    text[name] = { text: parts.join("\n"), sections: seen };
  }
  const pins = [];
  const pm = /const PIN_GROUPS = \{([\s\S]*?)\n\};/.exec(src);
  if (pm) for (const g of pm[1].matchAll(/units:\s*\[([^\]]*)\]/g)) pins.push(g[1].match(/\w+/g) || []);
  return { lines, sections, helpers, units, text, pins };
}

// ── the board's files ────────────────────────────────────────────────────────────────────────────────────────────
function declared(rel, src) {
  const out = new Set();
  const add = t => { if (t && isIdent(t)) out.add(t); };
  const ext = path.extname(rel);
  if (ext === ".js") {
    for (const m of src.matchAll(/^ {0,2}(?:async\s+)?function\s*\*?\s*([A-Za-z_$][\w$]*)\s*\(/gm)) add(m[1]);
    for (const m of src.matchAll(/^ {0,2}(?:const|let|var)\s+([A-Za-z_$][\w$]*)/gm)) add(m[1]);
    for (const m of src.matchAll(/\bwindow\.([A-Za-z_$][\w$]*)\s*=[^=]/g)) add(m[1]);
    for (const m of src.matchAll(/\bid=\\?["']([\w-]+)\\?["']/g)) add(m[1]);
    for (const m of src.matchAll(/getElementById\(["']([\w-]+)["']\)/g)) add(m[1]);
    for (const m of src.matchAll(/\.id\s*=\s*["']([\w-]+)["']/g)) add(m[1]);
    for (const m of src.matchAll(/class(?:Name)?\s*=\s*\\?["']([\w\s-]+)\\?["']/g)) m[1].split(/\s+/).forEach(add);
  } else if (ext === ".css") {
    const bare = src.replace(/\/\*[\s\S]*?\*\//g, "").replace(/\{[^{}]*\}/g, "{}").replace(/\{[^{}]*\}/g, "{}");
    for (const m of bare.matchAll(/[.#]([A-Za-z_][\w-]*)/g)) add(m[1]);
  } else if (ext === ".html") {
    for (const m of src.matchAll(/\bid="([\w-]+)"/g)) add(m[1]);
    for (const m of src.matchAll(/\bclass="([\w\s-]+)"/g)) m[1].split(/\s+/).forEach(add);
    for (const m of src.matchAll(/^\s*(?:async\s+)?function\s+([A-Za-z_$][\w$]*)/gm)) add(m[1]);
  }
  return out;
}

const stemOf = rel => path.basename(rel).replace(/\.[^.]+$/, "").toLowerCase().replace(/[^a-z0-9]/g, "");
// a unit's name carries the file's: pulls.js -> pullsDrawer, toasts.js -> toastStays
function nameMatches(unit, rel) {
  const st = stemOf(rel).replace(/s$/, "");
  return st.length >= 4 && unit.toLowerCase().includes(st);
}

// ── the index, built once per run ────────────────────────────────────────────────────────────────────────────────
function buildIndex(root) {
  root = root || ROOT;
  const hsrc = read(HARNESS, root) || "";
  const h = parseHarness(hsrc);
  const unitTok = {};
  for (const [n, t] of Object.entries(h.text)) unitTok[n] = tokens(t.text);
  const df = new Map();
  for (const set of Object.values(unitTok)) for (const t of set) df.set(t, (df.get(t) || 0) + 1);
  const nUnits = Object.keys(unitTok).length || 1;
  const weights = (JSON.parse(read("scripts/board-suite-weights.json", root) || "{}").ms) || {};
  const nodeScripts = fs.readdirSync(path.join(root, "scripts"))
    .filter(f => /^test-.*\.js$/.test(f) && f !== "test-board-headless.js" && f !== "test-board-sharded.js" && f !== "test-touched-plan.js" && f !== "test-buckets.js")
    .map(f => "scripts/" + f);
  const scriptSrc = {};
  for (const s of nodeScripts) scriptSrc[s] = read(s, root) || "";
  let bk = null;
  try { bk = buckets.load(root); } catch (e) { bk = null; }   // no bucket file (an older tree): the inference does it all
  return { root, h, unitTok, df, nUnits, weights, nodeScripts, scriptSrc, declCache: {}, bk };
}

function declOf(ix, rel) {
  if (!(rel in ix.declCache)) { const src = read(rel, ix.root); ix.declCache[rel] = src == null ? new Set() : declared(rel, src); }
  return ix.declCache[rel];
}

// units that name what a board file declares, best first
function unitsForWebFile(ix, rel) {
  const decl = declOf(ix, rel);
  const scored = [];
  for (const [u, set] of Object.entries(ix.unitTok)) {
    let score = 0;
    const hits = [];
    for (const t of decl) {
      if (!set.has(t)) continue;
      const d = ix.df.get(t);
      if (d / ix.nUnits > COMMON_SHARE) continue;
      score += 1 / d;
      hits.push([t, d]);
    }
    const named = nameMatches(u, rel);
    if (named) score += 2;
    if (score <= 0) continue;
    hits.sort((a, b) => a[1] - b[1]);
    scored.push({ unit: u, score, named, hits: hits.slice(0, 3).map(x => x[0]) });
  }
  scored.sort((a, b) => b.score - a.score || a.unit.localeCompare(b.unit));
  if (!scored.length) return [];
  // keep what scores near the best: a quarter of the top, and never a single shared name
  const floor = Math.max(0.34, scored[0].score / 4);
  return scored.filter(s => s.score >= floor);
}

// node scripts whose subject is this file
function scriptsForFile(ix, rel) {
  const out = [];
  const base = path.basename(rel);
  const decl = rel.startsWith(WEB) ? declOf(ix, rel) : new Set();
  for (const s of ix.nodeScripts) {
    const src = ix.scriptSrc[s];
    if (s === rel) { out.push({ script: s, why: "the script itself changed" }); continue; }
    const subj = /^\/\/ subject:\s*(.+)$/m.exec(src);
    if (subj && subj[1].split(/[,\s]+/).filter(Boolean).some(p => rel === p || rel.startsWith(p.replace(/\/?$/, "/")))) {
      out.push({ script: s, why: "its subject header names " + rel });
      continue;
    }
    if (!rel.startsWith(WEB)) continue;
    // reads the file by name: "js", "seen.js" or "cardrules.js" (a base name shared by board and phone counts for both)
    if (base.length > 3 && (src.includes('"' + base + '"') || src.includes("'" + base + "'"))) {
      out.push({ script: s, why: "it reads " + base });
      continue;
    }
    if (stemOf(s).replace(/^test/, "") === stemOf(rel) && stemOf(rel).length >= 4) {
      out.push({ script: s, why: "its name is the file's" });
      continue;
    }
    // lifts code out of the whole board by name (board-source.js): the names this file declares that it uses
    if (/board-source\.js/.test(src) && path.extname(rel) === ".js") {
      const st = tokens(src);
      const hits = [...decl].filter(t => st.has(t) && /^[A-Za-z_$][\w$]*$/.test(t) && (ix.df.get(t) || 0) / ix.nUnits <= COMMON_SHARE &&
        new RegExp("\\b(?:function\\s+|lift\\(\"|function )" + t.replace(/\$/g, "\\$") + "\\b|\\b" + t.replace(/\$/g, "\\$") + "\\(").test(src));
      if (hits.length) out.push({ script: s, why: "it lifts " + hits.slice(0, 3).join(", ") + " out of the board" });
    }
  }
  return out;
}

// harness lines -> the units that own them, or null for shared lines
function unitsForHarnessLines(ix, ranges) {
  const h = ix.h;
  const hit = new Map();   // unit -> why
  const shared = [];
  const owners = sec => Object.entries(h.text).filter(([, t]) => t.sections.has(sec)).map(([u]) => u);
  for (const [a, b] of ranges) {
    let owned = false;
    for (const [u, r] of Object.entries(h.units)) {
      if (r.section == null && a <= r.end && b >= r.start) { hit.set(u, "its inline block, lines " + a + "-" + b); owned = true; }
      if (r.section != null && a <= r.start && b >= r.start) { hit.set(u, "its unit line " + r.start); owned = true; }
    }
    for (const [sec, r] of Object.entries(h.sections)) {
      if (a <= r.end && b >= r.start) {
        for (const u of owners(sec)) hit.set(u, sec + ", lines " + a + "-" + b);
        owned = true;
      }
    }
    for (const [fn, r] of Object.entries(h.helpers)) {
      if (!(a <= r.end && b >= r.start)) continue;
      const users = Object.entries(ix.unitTok).filter(([, set]) => set.has(fn)).map(([u]) => u);
      if (users.length && users.length <= HELPER_FANOUT) {
        for (const u of users) hit.set(u, "uses the helper " + fn + " (lines " + a + "-" + b + ")");
        owned = true;
      } else if (users.length) {
        shared.push(fn + " (lines " + a + "-" + b + ", used by " + users.length + " units)");
        owned = true;
      }
    }
    if (!owned) shared.push("lines " + a + "-" + b);
  }
  return { hit, shared };
}

function isIntegrationGo(rel, root) {
  if (/_integration_test\.go$/.test(rel)) return true;
  const src = read(rel, root);
  return !!src && /^\/\/go:build[^\n]*\bintegration\b/m.test(src.split("\n").slice(0, 5).join("\n"));
}

// ── declared buckets ─────────────────────────────────────────────────────────────────────────────────────────────
const boardBuckets = ix => ix.bk ? { buckets: ix.bk.buckets.filter(b => b.kind === "board"), shared: ix.bk.shared } : null;
const goBuckets = ix => ix.bk ? { buckets: ix.bk.buckets.filter(b => b.kind === "go"), shared: {} } : null;
const bucketByName = (ix, n) => ix.bk.buckets.find(b => b.name === n);

// the buckets a board file selects: [{bucket, shared}], empty when it is in none
function declaredBoard(ix, rel) {
  const bb = boardBuckets(ix);
  if (!bb) return [];
  if (rel in bb.shared) {
    const out = [{ bucket: bucketByName(ix, "boot"), shared: true }];
    for (const b of bb.buckets) if ((b.needs || []).includes(rel) && !out.some(o => o.bucket === b)) out.push({ bucket: b, shared: true });
    return out;
  }
  return buckets.owners(bb, "sources", rel).map(o => ({ bucket: o.bucket, shared: false }));
}

// a Go source file's bucket, or its test file's through the file it tests
function declaredGo(ix, rel) {
  const gb = goBuckets(ix);
  if (!gb) return null;
  const cand = [rel];
  if (rel.endsWith("_test.go")) cand.push(rel.replace(/(_integration)?_test\.go$/, ".go"));
  for (const c of cand) { const o = buckets.owners(gb, "sources", c); if (o.length === 1) return o[0].bucket; }
  return null;
}

// a selected bucket's units for one changed file: what the inference ranks in the bucket first, then the rest cheapest first
function bucketUnits(ix, b, rel, inferred, why) {
  const bb = boardBuckets(ix);
  const inB = new Set(Object.keys(ix.h.units).filter(u => { const o = buckets.owners(bb, "units", u); return o.length === 1 && o[0].bucket === b; }));
  const w = u => ix.weights[u] > 0 ? ix.weights[u] : DEFAULT_WEIGHT_MS;
  const ranked = inferred.filter(s => inB.has(s.unit));
  const seen = new Set(ranked.map(s => s.unit));
  const rest = [...inB].filter(u => !seen.has(u) && u !== "bootClean").sort((a, c) => w(a) - w(c) || a.localeCompare(c));
  return [
    ...ranked.map(s => ({ unit: s.unit, why: why + (s.hits.length ? ", uses " + s.hits.join(", ") : "") })),
    ...rest.map(u => ({ unit: u, why: why })),
  ];
}

// ── the plan ─────────────────────────────────────────────────────────────────────────────────────────────────────
function plan(files, opts) {
  opts = opts || {};
  const ix = opts.index || buildIndex(opts.root);
  const root = ix.root;
  const budget = opts.budget || DEFAULT_BUDGET_MS;
  const p = { go: new Map(), tags: [], gofiles: [], govet: [], gobuild: null, units: new Map(), node: new Map(),
    jscheck: [], shcheck: [], warnings: [], skipped: [], dropped: [], buckets: new Map() };
  const addBucket = (b, rel) => { if (!p.buckets.has(b.name)) p.buckets.set(b.name, []); if (!p.buckets.get(b.name).includes(rel)) p.buckets.get(b.name).push(rel); };
  const lintNote = "a file the bucket lint covers changed";
  const addNode = (s, why) => { if (!p.node.has(s)) p.node.set(s, why); };
  const wantUnits = [];   // per source: [{unit, why}] best first
  let board = false;

  for (const rel0 of files) {
    const rel = rel0.replace(/\\/g, "/").replace(/^\.\//, "");
    if (!rel || rel.startsWith("build.claude/")) continue;
    let used = false;
    if (rel === "go.mod" || rel === "go.sum") { p.gobuild = rel + " changed, so every package must still build"; continue; }
    if (rel.endsWith(".go")) {
      if (rel.startsWith("scripts/")) {
        const t = rel.replace(/(_test)?\.go$/, "");
        if (fs.existsSync(path.join(root, t + "_test.go")) && fs.existsSync(path.join(root, t + ".go"))) {
          if (!p.gofiles.some(g => g.files[0] === t + ".go")) p.gofiles.push({ files: [t + ".go", t + "_test.go"], why: rel + " changed" });
        } else if (fs.existsSync(path.join(root, rel))) p.govet.push({ file: rel, why: "a standalone Go file with no test" });
        continue;
      }
      const dir = "./" + path.posix.dirname(rel);
      if (!fs.existsSync(path.join(root, path.posix.dirname(rel)))) { p.skipped.push([rel, "its package is gone"]); continue; }
      const gbk = declaredGo(ix, rel);
      if (gbk) addBucket(gbk, rel);
      if (/^internal\/(api|link)\//.test(rel) && !rel.endsWith("_test.go")) addNode("scripts/check-test-buckets.js", lintNote);
      if (!p.go.has(dir)) p.go.set(dir, rel + " changed" + (gbk ? ", bucket " + gbk.name : ", in no bucket"));
      if (isIntegrationGo(rel, root) && !p.tags.length) p.tags.push({ tag: "integration", why: rel + " is an integration test file" });
      continue;
    }
    for (const n of scriptsForFile(ix, rel)) { addNode(n.script, n.why); used = true; }
    if (rel.startsWith(WEB)) {
      const ext = path.extname(rel);
      if (ext === ".md") { p.skipped.push([rel, "notes, not code"]); continue; }
      board = true;
      if (ext === ".js" && fs.existsSync(path.join(root, rel))) p.jscheck.push(rel);
      const code = [".js", ".css", ".html"].includes(ext);
      const picked = code ? unitsForWebFile(ix, rel) : [];
      const inferredWhy = s => path.posix.relative(WEB, rel) + ": " +
        (s.named ? "the unit is named for it" + (s.hits.length ? ", " : "") : "") + (s.hits.length ? "uses " + s.hits.join(", ") : "");
      const decl = code ? declaredBoard(ix, rel) : [];
      if (code) addNode("scripts/check-test-buckets.js", lintNote);
      if (decl.length) {
        const names = decl.map(d => d.bucket.name);
        for (const d of decl) { addBucket(d.bucket, rel); for (const t of d.bucket.tests || []) addNode(t, "bucket " + d.bucket.name + " owns it, and " + path.posix.relative(WEB, rel) + " changed"); }
        const list = [];
        for (const d of decl) list.push(...bucketUnits(ix, d.bucket, rel, picked, "bucket " + d.bucket.name + ": " + path.posix.relative(WEB, rel)));
        // a shared file is read by more than any list says, so what the inference found outside the buckets follows
        if (decl[0].shared) list.push(...picked.filter(s => !list.some(c => c.unit === s.unit)).map(s => ({ unit: s.unit, why: inferredWhy(s) })));
        if (!list.length) p.warnings.push(rel + " is in bucket " + names.join(", ") + ", which has no headless unit, so only bootClean covers it");
        wantUnits.push(list);
        continue;
      }
      if (code && ix.bk) p.warnings.push(rel + " is in no bucket, so the units are inferred. add it to scripts/test-buckets.json");
      if (!picked.length) p.warnings.push(rel + " maps to no headless unit, so only bootClean covers it");
      wantUnits.push(picked.map(s => ({ unit: s.unit, why: inferredWhy(s) })));
      continue;
    }
    if (rel === HARNESS) {
      board = true;
      p.jscheck.push(rel);
      const ranges = opts.harnessLines || [];
      if (!ranges.length) { p.warnings.push(HARNESS + " changed but no line ranges were given, so only bootClean runs"); continue; }
      const { hit, shared } = unitsForHarnessLines(ix, ranges);
      wantUnits.unshift([...hit].map(([u, why]) => ({ unit: u, why: "the harness changed in " + why, own: true })));
      for (const s of shared) p.warnings.push("the harness changed outside every unit (" + s + "), so bootClean stands in for it");
      continue;
    }
    if (rel === buckets.FILE || rel === "scripts/test-buckets.js" || rel === "scripts/check-test-buckets.js") {
      addNode("scripts/check-test-buckets.js", rel + " changed");
      if (rel.endsWith(".js")) p.jscheck.push(rel);
      continue;
    }
    if (rel === "scripts/board-source.js") {
      board = true;
      for (const s of ix.nodeScripts) if (/board-source\.js/.test(ix.scriptSrc[s])) addNode(s, "it reads the board through board-source.js, which changed");
      p.jscheck.push(rel);
      continue;
    }
    if (rel === "scripts/test-board-sharded.js" || rel === "scripts/check-suite-units.js") {
      addNode("scripts/check-suite-units.js", rel + " changed, and this lists the suite through it");
      p.jscheck.push(rel);
      continue;
    }
    if (/^scripts\/[^/]+\.(js|mjs)$/.test(rel) && fs.existsSync(path.join(root, rel))) { p.jscheck.push(rel); used = true; }
    if (/\.sh$/.test(rel) && fs.existsSync(path.join(root, rel))) { p.shcheck.push(rel); used = true; }
    if (!used) p.skipped.push([rel, "nothing here tests it"]);
  }

  // bootClean with any board change, then each source's best units round robin until the budget is spent
  if (board) p.units.set("bootClean", { why: "any board change: the board boots with no page error, desktop and phone", ms: ix.weights.bootClean || DEFAULT_WEIGHT_MS });
  const w = u => ix.weights[u] > 0 ? ix.weights[u] : DEFAULT_WEIGHT_MS;
  const groupOf = u => ix.h.pins.find(g => g.includes(u)) || [u];
  let spent = 0;
  const lists = wantUnits.map(l => l.slice());
  for (let round = 0; lists.some(l => l.length); round++) {
    for (const l of lists) {
      const c = l.shift();
      if (!c || p.units.has(c.unit)) continue;
      const g = groupOf(c.unit).filter(u => !p.units.has(u));
      const cost = g.reduce((s, u) => s + w(u), 0);
      // a unit the harness change is inside always runs: it is the test that changed
      if (!c.own && spent + cost > budget) { p.dropped.push(c.unit); continue; }
      spent += cost;
      for (const u of g) p.units.set(u, { why: u === c.unit ? c.why : "pinned with " + c.unit, ms: w(u) });
    }
  }
  p.unitMs = spent + (p.units.has("bootClean") ? w("bootClean") : 0);
  return p;
}

function describe(p) {
  const out = [];
  const row = (k, a, b) => out.push("  " + k.padEnd(7) + " " + a.padEnd(46) + " " + b);
  if (p.gobuild) row("gobuild", "./...", p.gobuild);
  for (const [d, why] of p.go) row("go", d, why);
  for (const t of p.tags) row("tags", t.tag, t.why);
  for (const g of p.gofiles) row("gofiles", g.files.join(" "), g.why);
  for (const g of p.govet) row("govet", g.file, g.why);
  for (const [n, fs_] of p.buckets) row("bucket", n, fs_.join(", "));
  for (const [u, x] of p.units) row("unit", u + " (" + (x.ms / 1000).toFixed(1) + "s)", x.why);
  if (p.units.size) row("", "", "units total " + (p.unitMs / 1000).toFixed(1) + "s of weight, run in parallel shards");
  if (p.dropped.length) row("budget", p.dropped.length + " more units left out", p.dropped.join(","));
  for (const [s, why] of p.node) row("node", s, why);
  for (const f of p.jscheck) row("jscheck", f, "node --check");
  for (const f of p.shcheck) row("shcheck", f, "bash -n");
  for (const [f, why] of p.skipped) row("skip", f, why);
  for (const wn of p.warnings) out.push("  WARNING " + wn);
  return out.join("\n");
}

function actions(p) {
  const out = [];
  for (const [n, fs_] of p.buckets) out.push("BUCKET " + n + " " + fs_.join(" "));
  if (p.gobuild) out.push("GOBUILD ./...");
  for (const d of p.go.keys()) out.push("GO " + d);
  for (const t of p.tags) out.push("TAGS " + t.tag);
  for (const g of p.gofiles) out.push("GOFILES " + g.files.join(" "));
  for (const g of p.govet) out.push("GOVET " + g.file);
  if (p.units.size) out.push("UNITS " + [...p.units.keys()].join(","));
  for (const s of p.node.keys()) out.push("NODE " + s);
  for (const f of p.jscheck) out.push("JSCHECK " + f);
  for (const f of p.shcheck) out.push("SHCHECK " + f);
  return out;
}

module.exports = { plan, describe, actions, buildIndex, parseHarness, declared };

if (require.main === module) {
  const args = process.argv.slice(2);
  const opts = {};
  for (let i = 0; i < args.length; i++) {
    if (args[i] === "--harness-lines") opts.harnessLines = (args[++i] || "").split(",").filter(Boolean).map(r => r.split("-").map(Number));
    else if (args[i] === "--budget") opts.budget = Number(args[++i]);
    else { console.error("unknown flag " + args[i]); process.exit(2); }
  }
  const files = fs.readFileSync(0, "utf8").split(/\r?\n/).filter(Boolean);
  const p = plan(files, opts);
  console.error(describe(p));
  console.log(actions(p).join("\n"));
}
