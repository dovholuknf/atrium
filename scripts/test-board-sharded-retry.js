// scripts/test-board-sharded.js, RUN against a stand-in harness: a unit that fails once and passes on retry, one that always fails.
// subject: scripts/test-board-sharded.js
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
const units = ["steady", "stalls", "broken"];
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

function run(env, extra = []) {
  const dir = fs.mkdtempSync(path.join(tmp, "state-"));
  const r = spawnSync(process.execPath, [sharded, "--local", "--shards", "2", "--units", "steady,stalls,broken", ...extra], {
    encoding: "utf8", env: { ...process.env, BOARD_HEADLESS_FILE: fake, FAKE_DIR: dir, ...env } });
  return { code: r.status, out: r.stdout + r.stderr };
}

const RETRY = "PASSED ON RETRY (slow or stalled, first try failed)";

const once = run({ FAKE_ONCE: "stalls" });
if (once.code !== 0) fail("a unit that passed on retry must pass the run, exit " + once.code + ":\n" + once.out);
if (!/^stalls\s.*PASSED ON RETRY \(slow or stalled, first try failed\)/m.test(once.out)) fail("the table row for the retried unit:\n" + once.out);
if (!/^steady\s.*  PASS  /m.test(once.out)) fail("a clean unit stays PASS:\n" + once.out);
if (!/^PASSED ON RETRY, NOT A CLEAN PASS: stalls: first try failed after \S+, the retry passed in \S+\./m.test(once.out)) fail("the line printed when the retry lands:\n" + once.out);
if (!/^PASSED ON RETRY, NOT A CLEAN PASS \(1\)\./m.test(once.out)) fail("the block header:\n" + once.out);
if (!/^  stalls  \[first try 0\.\d+s failed, retry 0\.\d+s passed\]$/m.test(once.out)) fail("the block names both timings:\n" + once.out);
if (!/^    first try: stalls: the sheet never settled$/m.test(once.out)) fail("the block keeps the first-try error:\n" + once.out);
if (!/1 PASSED ON RETRY \(not clean, the run passes\)/m.test(once.out)) fail("the totals line:\n" + once.out);
if (/^FAILED \(/m.test(once.out) || /LOAD/.test(once.out)) fail("a retried pass is not a failure:\n" + once.out);
if (!/^1 of 3 units passed|^2 of 3 units passed/m.test(once.out)) fail("a retried pass does not count as a clean pass:\n" + once.out);

const both = run({ FAKE_ONCE: "stalls", FAKE_ALWAYS: "broken" });
if (both.code !== 1) fail("a unit that fails both tries still fails the run, exit " + both.code + ":\n" + both.out);
if (!/^FAILED \(1\):\n  broken$/m.test(both.out)) fail("the failed block names only the unit that failed twice:\n" + both.out);
if (!/^PASSED ON RETRY, NOT A CLEAN PASS \(1\)\./m.test(both.out)) fail("the retried block is printed beside a failure:\n" + both.out);

const none = run({ FAKE_ONCE: "stalls" }, ["--no-retry"]);
if (none.code !== 1) fail("with --no-retry the first failure fails the run, exit " + none.code + ":\n" + none.out);
if (none.out.includes(RETRY)) fail("--no-retry never says a unit passed on retry:\n" + none.out);

const ci = fs.readFileSync(path.join(__dirname, "ci.sh"), "utf8");
const report = ci.slice(ci.indexOf("board_report() {"), ci.indexOf("\n}\n", ci.indexOf("board_report() {")) + 3);
const art = path.join(tmp, "art");
fs.mkdirSync(art);
const logFile = path.join(tmp, "board.log");
fs.writeFileSync(logFile, once.out);
const calls = spawnSync("bash", ["-c", [
  "note_counts() { echo \"counts $1 $2\"; }",
  "note_failing() { echo \"failing $*\"; }",
  "note_retried() { echo \"retried $*\"; }",
  "artefacts=" + JSON.stringify(art.split(path.sep).join("/")),
  report,
  "board_report board " + JSON.stringify(logFile.split(path.sep).join("/")),
].join("\n")], { encoding: "utf8" });
if (!/^counts 3 0$/m.test(calls.stdout)) fail("board_report counts a retried pass as a unit and not as a failure:\n" + calls.stdout + calls.stderr);
if (/^failing /m.test(calls.stdout)) fail("a retried pass is not a failing row:\n" + calls.stdout);
if (!/^retried board stalls stalls: the sheet never settled first try 0\.\d+s failed, retry 0\.\d+s passed$/m.test(calls.stdout)) fail("board_report hands the retried row its error and timings:\n" + calls.stdout + calls.stderr);

const clean = run({});
if (clean.code !== 0 || /PASSED ON RETRY/.test(clean.out)) fail("a clean run says nothing about retries, exit " + clean.code + ":\n" + clean.out);

fs.rmSync(tmp, { recursive: true, force: true });
if (bad) process.exit(1);
console.log("ok");
