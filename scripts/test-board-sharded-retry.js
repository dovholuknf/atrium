// scripts/test-board-sharded.js, RUN against a stand-in harness: a flaky-listed unit that fails once, an unlisted one, one that always fails.
// subject: scripts/test-board-sharded.js, scripts/ci.sh
const fs = require("fs");
const os = require("os");
const path = require("path");
const { spawnSync } = require("child_process");

const sharded = path.join(__dirname, "test-board-sharded.js");
const tmp = fs.mkdtempSync(path.join(os.tmpdir(), "sharded-retry-"));
let bad = 0;
function fail(msg) {
  console.error("FAIL: " + msg);
  bad++;
}

const fake = path.join(tmp, "fake-harness.js");
fs.writeFileSync(fake, `
const fs = require("fs"), path = require("path");
const units = ["steady", "stalls", "unlisted", "broken", "listedBroken"];
if (process.env.HEADLESS_LIST) { console.log(JSON.stringify({ units, pins: {} })); process.exit(0); }
const once = (process.env.FAKE_ONCE || "").split(",").filter(Boolean), always = (process.env.FAKE_ALWAYS || "").split(",").filter(Boolean);
const out = {}; let bad = 0;
for (const u of (process.env.HEADLESS_UNITS || "").split(",").filter(Boolean)) {
  const marker = path.join(process.env.FAKE_DIR, u + ".seen");
  const seen = fs.existsSync(marker);
  fs.writeFileSync(marker, "");
  const ok = !always.includes(u) && !(once.includes(u) && !seen);
  if (!ok) bad++;
  out[u] = { ms: seen ? 40 : 90, ok, fails: ok ? [] : [u + ": the sheet never settled\\n  waiting for locator #sheet"] };
}
fs.writeFileSync(process.env.HEADLESS_RESULTS, JSON.stringify({ units: out, order: Object.keys(out), bad }));
process.exit(bad ? 1 : 0);
`);

const flakyFile = path.join(tmp, "flaky-suite.json");
fs.writeFileSync(flakyFile, JSON.stringify({ entries: [
  { kind: "board", name: "stalls", reason: "stalls under load on macos" },
  { kind: "board", name: "listedBroken", reason: "always broken here" },
] }));

function run(env, units, extra = []) {
  const dir = fs.mkdtempSync(path.join(tmp, "state-"));
  const r = spawnSync(process.execPath, [sharded, "--local", "--shards", "2", "--units", units, ...extra], {
    encoding: "utf8", env: { ...process.env, BOARD_HEADLESS_FILE: fake, BOARD_FLAKY_FILE: flakyFile, FAKE_DIR: dir, ...env } });
  return { code: r.status, out: r.stdout + r.stderr };
}

const RETRY = "PASSED ON RETRY (slow or stalled, first try failed)";

const listed = run({ FAKE_ONCE: "stalls" }, "steady,stalls");
if (listed.code !== 0) fail("a listed unit that passed on retry must pass the run, exit " + listed.code + ":\n" + listed.out);
if (!/^stalls\s.*PASSED ON RETRY \(slow or stalled, first try failed\)/m.test(listed.out)) fail("the table row for the retried unit:\n" + listed.out);
if (!/^steady\s.*  PASS  /m.test(listed.out)) fail("a clean unit stays PASS:\n" + listed.out);
if (!/^PASSED ON RETRY, NOT A CLEAN PASS: stalls: first try failed after \S+, the retry passed in \S+\./m.test(listed.out)) fail("the line printed when the retry lands:\n" + listed.out);
if (!/^PASSED ON RETRY, NOT A CLEAN PASS \(1\)\./m.test(listed.out)) fail("the block header:\n" + listed.out);
if (!/^  stalls  \[first try 0\.\d+s failed, retry 0\.\d+s passed\]$/m.test(listed.out)) fail("the block names both timings:\n" + listed.out);
if (!/^    first try: stalls: the sheet never settled$/m.test(listed.out)) fail("the block keeps the first-try error:\n" + listed.out);
if (!/1 PASSED ON RETRY \(flaky-listed, not clean, the run passes\)/m.test(listed.out)) fail("the totals line:\n" + listed.out);
if (/^FAILED /m.test(listed.out) || /LOAD/.test(listed.out)) fail("a listed retried pass is not a failure:\n" + listed.out);
if (!/^1 of 2 units passed/m.test(listed.out)) fail("a retried pass does not count as a clean pass:\n" + listed.out);

const unlisted = run({ FAKE_ONCE: "unlisted" }, "steady,unlisted");
if (unlisted.code !== 0) fail("an unlisted unit that passed on retry passes the run, exit " + unlisted.code + ":\n" + unlisted.out);
if (!/^unlisted\s.*LOAD \(passed on retry\)/m.test(unlisted.out)) fail("the unlisted row says LOAD:\n" + unlisted.out);
if (!/^FAILED IN THE SHARDS, PASSED ALONE, AND NOT ON THE FLAKY LIST \(1\)\. NOT CLEAN PASSES, the run passes\./m.test(unlisted.out)) fail("the unlisted block:\n" + unlisted.out);
if (!/^    first try: unlisted: /m.test(unlisted.out)) fail("the block keeps the first-try error:\n" + unlisted.out);
if (!/1 passed only on retry \(LOAD, not clean, the run passes\): unlisted/.test(unlisted.out)) fail("the totals line names the unit:\n" + unlisted.out);
if (/^FAILED \(/m.test(unlisted.out)) fail("a LOAD pass is not a failure:\n" + unlisted.out);
if (unlisted.out.includes(RETRY)) fail("an unlisted unit is never marked as the tolerated kind:\n" + unlisted.out);

const mixed = run({ FAKE_ONCE: "stalls,unlisted", FAKE_ALWAYS: "broken,listedBroken" }, "stalls,unlisted,broken,listedBroken");
if (mixed.code !== 1) fail("a unit that fails both tries fails the run, exit " + mixed.code + ":\n" + mixed.out);
if (!/^FAILED \(2\):\n  broken\n(?:    .*\n)*  listedBroken  \[flaky, failed both tries\]$/m.test(mixed.out)) fail("both double failures are named, listed or not:\n" + mixed.out);
if (!/^PASSED ON RETRY, NOT A CLEAN PASS \(1\)\./m.test(mixed.out)) fail("the retried block is printed beside failures:\n" + mixed.out);
if (!/^FAILED IN THE SHARDS, PASSED ALONE, AND NOT ON THE FLAKY LIST \(1\)\./m.test(mixed.out)) fail("the unlisted block is printed beside failures:\n" + mixed.out);

const none = run({ FAKE_ONCE: "stalls" }, "steady,stalls", ["--no-retry"]);
if (none.code !== 1) fail("with --no-retry the first failure fails the run, exit " + none.code + ":\n" + none.out);
if (none.out.includes(RETRY)) fail("--no-retry never says a unit passed on retry:\n" + none.out);

const ci = fs.readFileSync(path.join(__dirname, "ci.sh"), "utf8");
const report = ci.slice(ci.indexOf("board_report() {"), ci.indexOf("\n}\n", ci.indexOf("board_report() {")) + 3);
const art = path.join(tmp, "art");
fs.mkdirSync(art);
const logFile = path.join(tmp, "board.log");
const both = run({ FAKE_ONCE: "stalls,unlisted" }, "steady,stalls,unlisted");
fs.writeFileSync(logFile, both.out);
const calls = spawnSync("bash", ["-c", [
  "note_counts() { echo \"counts $1 $2\"; }",
  "note_failing() { echo \"failing $*\"; }",
  "note_retried() { echo \"retried $*\"; }",
  "artefacts=" + JSON.stringify(art.split(path.sep).join("/")),
  report,
  "board_report board " + JSON.stringify(logFile.split(path.sep).join("/")),
].join("\n")], { encoding: "utf8" });
if (!/^counts 3 0$/m.test(calls.stdout)) fail("board_report counts neither retried pass as failed:\n" + calls.stdout + calls.stderr);
if (!/^retried board unlisted unlisted: the sheet never settled LOAD, not on the flaky list$/m.test(calls.stdout)) fail("board_report hands the LOAD row its error:\n" + calls.stdout);
if (!/^retried board stalls stalls: the sheet never settled first try 0\.\d+s failed, retry 0\.\d+s passed$/m.test(calls.stdout)) fail("board_report hands the listed retried row its error and timings:\n" + calls.stdout + calls.stderr);
if (/^failing board/m.test(calls.stdout)) fail("a retried pass is not a failing row:\n" + calls.stdout);

const clean = run({}, "steady");
if (clean.code !== 0 || /PASSED ON RETRY|LOAD/.test(clean.out)) fail("a clean run says nothing about retries, exit " + clean.code + ":\n" + clean.out);

fs.rmSync(tmp, { recursive: true, force: true });
if (bad) process.exit(1);
console.log("ok");
