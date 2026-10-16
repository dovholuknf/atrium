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

// GO TESTS. Go runs packages and -run patterns, never a file, so a Go bucket is its package plus the names of the tests
// in its files. `tests` are globs of test files. A test file no bucket of its package owns is in that package's `rest`
// bucket (`"rest": true`), which has a `ceiling`: scripts/check-test-buckets.js fails when `rest` grows past it, so a new
// file is put in a bucket and does not pile up there. scripts/ci-report.go reads the same file to name a bucket in a report.
function goOnly(b) { return { buckets: b.buckets.filter(x => x.kind === "go"), shared: {} }; }

function goTestFiles(dir, root) {
  const d = path.join(root || ROOT, dir);
  return fs.existsSync(d) ? fs.readdirSync(d).filter(f => f.endsWith("_test.go")).sort().map(f => dir + "/" + f) : [];
}

// the Go bucket of a test file: [{bucket, via}] from `tests`, or the package's rest bucket when none owns it
function goTestOwners(b, rel) {
  const g = goOnly(b), o = owners(g, "tests", rel).filter(x => !x.bucket.rest);
  if (o.length) return o;
  const dir = path.posix.dirname(rel);
  return g.buckets.filter(x => x.rest && x.package === "./" + dir).map(x => ({ bucket: x, via: "rest" }));
}

// the test files of one Go bucket
function goBucketFiles(b, bk, root) {
  return goTestFiles(bk.package.replace(/^\.\//, ""), root).filter(f => goTestOwners(b, f).some(x => x.bucket === bk));
}

// `-run` for a Go bucket: ^(TestA|TestB)$ over the top level tests declared in its files. null when it owns the package
function goRunRegex(b, bk, root) {
  const names = [];
  for (const f of goBucketFiles(b, bk, root)) {
    const src = fs.readFileSync(path.join(root || ROOT, f), "utf8");
    for (const m of src.matchAll(/^func (Test\w+)\(\w+ \*testing\.T\)/gm)) names.push(m[1]);
  }
  return names.length ? "^(" + [...new Set(names)].sort().join("|") + ")$" : "";
}

if (require.main === module) {
  const b = load(), i = process.argv.indexOf("--go-run");
  if (i > 0) {
    const bk = b.buckets.find(x => x.kind === "go" && x.name === process.argv[i + 1]);
    if (!bk) { console.error("no go bucket " + process.argv[i + 1] + ". they are: " + b.buckets.filter(x => x.kind === "go").map(x => x.name).join(" ")); process.exit(2); }
    console.log(bk.package + "\t" + goRunRegex(b, bk));
  } else console.error("usage: node scripts/test-buckets.js --go-run <bucket>   prints the package and the -run regex");
}

module.exports = { load, owners, ownersOfFile, globRe, isGlob, goTestFiles, goTestOwners, goBucketFiles, goRunRegex, FILE, ROOT };
