// The test buckets: scripts/test-buckets.json read, matched and linted. Used by scripts/test-touched-plan.js (which
// bucket a changed file is in) and scripts/check-test-buckets.js (that every file and unit is in exactly one).
//
// A bucket is a name, the source files it owns (`sources`), and the tests that prove them. Board buckets name headless
// units (`units`) and node scripts (`tests`, run by the planner, and `checks`, listed only). Go buckets name a package
// and the files it owns. See docs/rnd/test-layout-design.md.
//
// MATCHING. A pattern is a name or a glob where `*` stands for any run of characters (not `/` in a path, so
// `js/*.js` is one folder deep, and `**` crosses folders). A name written out in full beats a glob, so `termsDrawerPush`
// can live in `cards` while `term*` is `terminals`. Among globs for one name there must be exactly one.

const fs = require("fs");
const path = require("path");

const ROOT = path.join(__dirname, "..");
const FILE = "scripts/test-buckets.json";

const isGlob = p => p.includes("*");
function globRe(p) {
  const s = p.replace(/[.+?^${}()|[\]\\]/g, "\\$&").replace(/\*\*/g, "\u0000").replace(/\*/g, "[^/]*").replace(/\u0000/g, ".*");
  return new RegExp("^" + s + "$");
}

function load(root) {
  const raw = fs.readFileSync(path.join(root || ROOT, FILE), "utf8");
  const j = JSON.parse(raw);
  const buckets = [];
  for (const [name, b] of Object.entries(j.board || {})) buckets.push(Object.assign({ name, kind: "board", units: [], sources: [], tests: [], checks: [], needs: [] }, b));
  for (const [name, b] of Object.entries(j.go || {})) buckets.push(Object.assign({ name, kind: "go", sources: [], tests: [] }, b));
  return { buckets, shared: j.shared || {}, raw: j };
}

// the buckets that own `name` by `field`: [{bucket, via, exact}], exact ones only when any exist
function owners(b, field, name) {
  const exact = [], globs = [];
  for (const bk of b.buckets) {
    for (const p of bk[field] || []) {
      if (!isGlob(p)) { if (p === name) exact.push({ bucket: bk, via: p }); } else if (globRe(p).test(name)) globs.push({ bucket: bk, via: p });
    }
  }
  return exact.length ? exact : globs;
}

// a source file: the shared list first (its own place), then the buckets
function ownersOfFile(b, rel) {
  if (rel in b.shared) return [{ bucket: null, via: "shared", shared: true }];
  return owners(b, "sources", rel);
}

module.exports = { load, owners, ownersOfFile, globRe, isGlob, FILE, ROOT };
