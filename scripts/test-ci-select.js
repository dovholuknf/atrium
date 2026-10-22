// scripts/ci-select.sh, RUN for every os and suite, and checked against ci.sh's groups and ci.yml's use of its outputs.
// subject: scripts/ci-select.sh, .github/workflows/ci.yml
const fs = require("fs");
const path = require("path");
const { spawnSync } = require("child_process");

const dir = __dirname;
let bad = 0;
const fail = (msg) => { console.error("FAIL: " + msg); bad++; };

function select(args) {
  const r = spawnSync("bash", [path.join(dir, "ci-select.sh"), ...args], { encoding: "utf8" });
  const out = {};
  for (const l of r.stdout.split("\n").filter(Boolean)) out[l.slice(0, l.indexOf("="))] = l.slice(l.indexOf("=") + 1);
  return { code: r.status, out };
}

if (spawnSync("bash", ["-c", "true"]).status !== 0) {
  console.log("skip: no bash on PATH");
  process.exit(0);
}

const groups = (o, kind) => ["ubuntu", "macos", "windows"].flatMap((p) => JSON.parse(o[kind + "_" + p]).map((g) => p + "/" + g));
const all = select([]);
if (all.code !== 0 || all.out.skipped !== "0") fail("no flags did not select every job: " + JSON.stringify(all));
const full = [...groups(all.out, "flaky"), ...groups(all.out, "check")];
if (full.length !== 17) fail("all selected " + full.length + " groups, wanted 17");

const list = spawnSync("bash", [path.join(dir, "ci.sh"), "--list-groups"], { encoding: "utf8" }).stdout;
const known = new Set(list.split("\n").map((l) => l.split(":")[0]));
for (const g of full) if (!known.has(g.split("/")[1])) fail(g + " is not a ci.sh group");

for (const os of ["all", "ubuntu", "macos", "windows"]) {
  for (const suite of ["all", "go", "ui"]) {
    const r = select(["--os", os, "--suite", suite]);
    const got = [...groups(r.out, "flaky"), ...groups(r.out, "check")];
    const want = full.filter((g) => {
      const [p, name] = g.split("/");
      const side = /^(flaky-board|board)/.test(name) ? "ui" : "go";
      return (os === "all" || os === p) && (suite === "all" || suite === side);
    });
    if (JSON.stringify(got) !== JSON.stringify(want)) fail(os + " " + suite + " selected " + got + ", wanted " + want);
    const empty = Object.entries(r.out).filter(([k, v]) => k !== "skipped" && v === "[]").length;
    if (r.out.skipped !== String(empty)) fail(os + " " + suite + " skipped " + r.out.skipped + ", counted " + empty);
    if (os !== "all" && suite !== "all" && !got.length) fail(os + " " + suite + " selected nothing");
  }
}

const blank = select(["--os", "", "--suite", ""]);
if (blank.code !== 0 || blank.out.skipped !== "0") fail("empty inputs (a push) did not mean all");
for (const badArgs of [["--os", "plan9"], ["--suite", "x"], ["--nope"]]) {
  if (select(badArgs).code !== 2) fail(badArgs.join(" ") + " did not exit 2");
}

const yml = fs.readFileSync(path.join(dir, "..", ".github", "workflows", "ci.yml"), "utf8");
for (const k of ["flaky", "check"]) for (const p of ["ubuntu", "macos", "windows"]) {
  if (!yml.includes("needs.select.outputs." + k + "_" + p)) fail("ci.yml never reads " + k + "_" + p);
}

if (bad) { console.error(bad + " failed"); process.exit(1); }
console.log("ci-select: ok");
