#!/usr/bin/env node
// subject: scripts/test-buckets.json, scripts/test-buckets.js, scripts/test-touched-plan.js
//
// The lint behind scripts/test-buckets.json: every board source file, every internal/api and internal/link Go source
// file, and every headless unit name is in exactly one bucket, and nothing in the file points at nothing. A new file
// outside a bucket fails here, which is what keeps the planner from guessing about it.
//
//   node scripts/check-test-buckets.js
//   node scripts/check-test-buckets.js --probe-file internal/api/web/js/new.js --probe-unit newUnit --probe-go-test internal/api/new_test.go
//
// The probes pretend a file, a unit or a Go test file exists, so the self-test can prove a new one outside a bucket fails.
//
// Run by scripts/check-board.sh, and by scripts/test-touched.sh when a file it covers changed. No browser, no go.

const fs = require("fs");
const path = require("path");
const { load, owners, ownersOfFile, globRe, isGlob, goTestFiles, goTestOwners, ROOT } = require("./test-buckets.js");

const b = load();
const errs = [];
const bad = m => errs.push(m);

const walk = (dir, keep) => {
  const out = [];
  const go = d => {
    for (const e of fs.readdirSync(path.join(ROOT, d), { withFileTypes: true })) {
      const rel = d + "/" + e.name;
      if (e.isDirectory()) { if (e.name !== "vendor" && e.name !== "icons") go(rel); } else if (keep(rel)) out.push(rel);
    }
  };
  go(dir);
  return out.sort();
};

// the files the lint covers
const probe = f => { const i = process.argv.indexOf(f); return i > 0 ? process.argv[i + 1] : null; };
const web = walk("internal/api/web", f => /\.(js|css|html)$/.test(f));
const goSrc = ["internal/api", "internal/link"].flatMap(d =>
  fs.readdirSync(path.join(ROOT, d)).filter(f => f.endsWith(".go") && !f.endsWith("_test.go")).map(f => d + "/" + f).sort());
if (probe("--probe-file")) (probe("--probe-file").startsWith("internal/api/web/") ? web : goSrc).push(probe("--probe-file"));
const harness = fs.readFileSync(path.join(ROOT, "scripts/test-board-headless.js"), "utf8");
// a unit is registered in the harness, and the ones that moved are defined in a file of scripts/board-tests/
const BUCKET_DIR = "scripts/board-tests";
const bucketFiles = fs.readdirSync(path.join(ROOT, BUCKET_DIR)).filter(f => f.endsWith(".js") && f !== "index.js").sort();
const fileUnits = {};   // bucket file -> its unit names
for (const f of bucketFiles) fileUnits[BUCKET_DIR + "/" + f] = [...fs.readFileSync(path.join(ROOT, BUCKET_DIR, f), "utf8").matchAll(/^\s*\{ name: "(\w+)", run:/gm)].map(m => m[1]);
const unitNames = [...new Set([...harness.matchAll(/^\s*await unit\("(\w+)"/gm)].map(m => m[1]).concat(Object.values(fileUnits).flat(), probe("--probe-unit") || []))].sort();

const boardOnly = { buckets: b.buckets.filter(x => x.kind === "board"), shared: b.shared };
const goOnly = { buckets: b.buckets.filter(x => x.kind === "go"), shared: {} };

for (const f of web) {
  const o = ownersOfFile(boardOnly, f);
  if (o.length === 0) bad(f + " is in no bucket. add it to `sources` of a board bucket in scripts/test-buckets.json");
  else if (o.length > 1) bad(f + " is in " + o.length + " buckets: " + o.map(x => x.bucket.name + " (" + x.via + ")").join(", "));
}
for (const f of goSrc) {
  const o = owners(goOnly, "sources", f);
  if (o.length === 0) bad(f + " is in no bucket. add it to `sources` of a go bucket in scripts/test-buckets.json");
  else if (o.length > 1) bad(f + " is in " + o.length + " buckets: " + o.map(x => x.bucket.name + " (" + x.via + ")").join(", "));
}
for (const u of unitNames) {
  const o = owners(boardOnly, "units", u);
  if (o.length === 0) bad("headless unit " + u + " is in no bucket. add it to `units` of a board bucket in scripts/test-buckets.json");
  else if (o.length > 1) bad("headless unit " + u + " is in " + o.length + " buckets: " + o.map(x => x.bucket.name + " (" + x.via + ")").join(", "));
}

// a bucket file holds the units its bucket owns and nothing else, and every bucket file is some bucket's `file`
const indexSrc = fs.readFileSync(path.join(ROOT, BUCKET_DIR, "index.js"), "utf8");
for (const bk of boardOnly.buckets.filter(x => x.file)) {
  if (!fileUnits[bk.file]) { bad("bucket " + bk.name + ": file " + bk.file + " does not exist"); continue; }
  if (!indexSrc.includes('"' + bk.name + '"')) bad("bucket " + bk.name + " is not in the names of " + BUCKET_DIR + "/index.js, so the harness never loads it");
  for (const u of fileUnits[bk.file]) {
    const o = owners(boardOnly, "units", u);
    if (o.length === 1 && o[0].bucket !== bk) bad("unit " + u + " is in " + bk.file + " but bucket " + o[0].bucket.name + " owns it");
  }
}
for (const f of Object.keys(fileUnits)) if (!boardOnly.buckets.some(x => x.file === f)) bad(f + " is no bucket's `file` in scripts/test-buckets.json");

// every Go test file is in one bucket of its package, or in its `rest`, and `rest` stays under its ceiling
const goPkgs = [...new Set(goOnly.buckets.map(x => x.package.replace(/^\.\//, "")))].sort();
const testFiles = goPkgs.flatMap(d => goTestFiles(d));
if (probe("--probe-go-test")) testFiles.push(probe("--probe-go-test"));
const restCount = new Map();
for (const f of testFiles) {
  const o = goTestOwners(b, f);
  if (o.length === 0) bad(f + " is in no bucket and its package has no `rest` bucket. add it to `tests` of a go bucket in scripts/test-buckets.json");
  else if (o.length > 1) bad(f + " is in " + o.length + " buckets: " + o.map(x => x.bucket.name + " (" + x.via + ")").join(", "));
  else if (o[0].bucket.rest) restCount.set(o[0].bucket, (restCount.get(o[0].bucket) || 0) + 1);
}
for (const bk of goOnly.buckets.filter(x => x.rest)) {
  const n = restCount.get(bk) || 0;
  if (typeof bk.ceiling !== "number") bad("bucket " + bk.name + " is `rest` and has no numeric `ceiling`");
  else if (n > bk.ceiling) bad("bucket " + bk.name + " holds " + n + " test files, over its ceiling of " + bk.ceiling + ". put the new file in a bucket's `tests` in scripts/test-buckets.json, do not raise the ceiling");
  else if (n < bk.ceiling) bad("bucket " + bk.name + " holds " + n + " test files, under its ceiling of " + bk.ceiling + ". lower `ceiling` to " + n + " so it stays a ratchet");
}

// nothing points at nothing: a pattern that matches no file or unit is a rename the file missed
const extra = new Map();   // the other Go packages a bucket owns whole: their files, read when a pattern asks
const filesIn = d => { if (!extra.has(d)) extra.set(d, fs.existsSync(path.join(ROOT, d)) ? fs.readdirSync(path.join(ROOT, d)).map(f => d + "/" + f) : []); return extra.get(d); };
const all = [...web, ...goSrc];
for (const bk of b.buckets) {
  for (const p of bk.sources || []) {
    const hit = isGlob(p) ? all.concat(filesIn(path.posix.dirname(p))).some(f => globRe(p).test(f)) : all.includes(p) || fs.existsSync(path.join(ROOT, p));
    if (!hit) bad("bucket " + bk.name + ": source " + p + " matches no file");
  }
  for (const p of bk.units || []) {
    const hit = isGlob(p) ? unitNames.some(u => globRe(p).test(u)) : unitNames.includes(p);
    if (!hit) bad("bucket " + bk.name + ": unit " + p + " matches no headless unit");
  }
  if (bk.kind === "go") for (const p of bk.tests || []) {
    if (!(isGlob(p) ? testFiles.some(f => globRe(p).test(f)) : testFiles.includes(p))) bad("bucket " + bk.name + ": test " + p + " matches no test file");
  }
  for (const s of [...(bk.tests || []), ...(bk.checks || [])]) {
    if (bk.kind === "board" && !fs.existsSync(path.join(ROOT, s))) bad("bucket " + bk.name + ": test script " + s + " does not exist");
  }
  if (bk.kind === "go" && bk.package && !fs.existsSync(path.join(ROOT, bk.package))) bad("bucket " + bk.name + ": package " + bk.package + " does not exist");
  for (const n of bk.needs || []) if (!(n in b.shared)) bad("bucket " + bk.name + ": needs " + n + ", which is not in `shared`");
}
for (const f of Object.keys(b.shared)) if (!fs.existsSync(path.join(ROOT, f))) bad("shared file " + f + " does not exist");

// a node script is in one bucket, as a test and as a check
for (const field of ["tests", "checks"]) {
  const seen = new Map();
  for (const bk of b.buckets) for (const s of bk[field] || []) {
    if (seen.has(s)) bad(s + " is a " + field.replace(/s$/, "") + " of both " + seen.get(s) + " and " + bk.name);
    seen.set(s, bk.name);
  }
}
const names = new Set();
for (const bk of b.buckets) { if (names.has(bk.name)) bad("two buckets named " + bk.name); names.add(bk.name); }

if (errs.length) {
  console.error(errs.join("\n"));
  console.error("check-test-buckets: " + errs.length + " problem(s) in scripts/test-buckets.json");
  process.exit(1);
}
console.log("check-test-buckets ok: " + web.length + " board files, " + goSrc.length + " go files, " + unitNames.length + " units, " + testFiles.length + " go test files (" + [...restCount].map(([k, v]) => v + " in " + k.name).join(", ") + "), " + b.buckets.length + " buckets");
