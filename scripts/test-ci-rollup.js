// scripts/ci-rollup.js, RUN against a fixture tree of job artifacts: a new unit, a known one, a retry pass, green and expired runs.
// subject: scripts/ci-rollup.js, scripts/ci-failures.js
const fs = require("fs");
const os = require("os");
const path = require("path");
const { spawnSync } = require("child_process");

const script = path.join(__dirname, "ci-rollup.js");
const tmp = fs.mkdtempSync(path.join(os.tmpdir(), "ci-rollup-"));
let bad = 0;
function fail(msg) {
  console.error("FAIL: " + msg);
  bad++;
}

function job(run, name, failing, boardLog) {
  const d = path.join(tmp, "runs", run, name);
  fs.mkdirSync(d, { recursive: true });
  fs.writeFileSync(path.join(d, "failing.tsv"), failing.map(r => r.join("\t")).join("\n") + (failing.length ? "\n" : ""));
  fs.writeFileSync(path.join(d, "steps.tsv"), "machine\t0\tok\t-\t-\n");
  if (boardLog) fs.writeFileSync(path.join(d, "board.log"), boardLog);
}

const retryLog = "x\nFAILED IN THE SHARDS, PASSED ALONE, AND NOT ON THE FLAKY LIST (1). These fail the run.\n  flakyUnit\n    first try: boom\n\n175 of 176 units passed\n";
const MOD = "github.com/dovholuknf/atrium/internal/ptyhost";

job("300", "ci-ubuntu-latest-board-1-attempt-1", [["board", "brandNew", "it broke: a | b"], ["board", "flakyUnit", "slow"]], retryLog);
job("300", "ci-ubuntu-latest-go-all-attempt-1", [["go-test", MOD + " TestKnown", "x_test.go:1: nope"]]);
job("300", "ci-macos-latest-board-2-attempt-1", []);
job("300", "ci-macos-latest-board-2-attempt-2", [["board", "(check)", "390px stack 8: tap"]]);

job("299", "ci-ubuntu-latest-go-all-attempt-1", [["go-test", MOD + " TestKnown", "x"]]);
job("298", "ci-ubuntu-latest-go-all-attempt-1", [["go-test", MOD + " TestKnown", "x"]]);
job("297", "ci-ubuntu-latest-board-1-attempt-1", []);
fs.mkdirSync(path.join(tmp, "runs", "296"), { recursive: true });

function run(args) {
  const r = spawnSync("node", [script, ...args], { encoding: "utf8" });
  if (r.status !== 0) fail("exit " + r.status + ": " + r.stderr);
  return r.stdout;
}

const runs = path.join(tmp, "runs");
const text = run(["--local", path.join(runs, "300"), "--history", runs, "--run", "300"]);
const row = (needle) => text.split("\n").find(l => l.includes(needle)) || "";

if (!/: 4 failing units/.test(text)) fail("wanted 4 failing units:\n" + text);
if (!/NEW/.test(row("brandNew"))) fail("brandNew should be NEW: " + row("brandNew"));
if (!row("brandNew").includes("a \\| b")) fail("a pipe in an error should be escaped: " + row("brandNew"));
if (!/failed in 2 of the last 4/.test(row("TestKnown"))) fail("TestKnown history: " + row("TestKnown"));
if (!/\(1 unknown\)/.test(row("TestKnown"))) fail("the expired run should count as unknown: " + row("TestKnown"));
if (!/passed on retry, fails the job/.test(row("flakyUnit"))) fail("flakyUnit retry: " + row("flakyUnit"));
if (!/no retry/.test(row("brandNew")) && !/failed on retry/.test(row("brandNew"))) fail("brandNew retry: " + row("brandNew"));
if (!row("390px stack 8").includes("macos | board-2")) fail("attempt 2 should win over attempt 1: " + row("390px"));
if (text.indexOf("brandNew") > text.indexOf("TestKnown")) fail("NEW rows come first");

const green = run(["--local", path.join(runs, "297"), "--history", runs, "--run", "297"]);
if (!/^## ci run 297: green\n\nAll 1 jobs passed\./.test(green)) fail("a green run says so in one line:\n" + green);

const none = run(["--local", path.join(runs, "300"), "--run", "300"]);
if (!/history unavailable/.test(none)) fail("no history should say so:\n" + none);

const bogus = spawnSync("node", [script, "--repo", "a/b", "--run", "1"], { encoding: "utf8", env: { ...process.env, GITHUB_TOKEN: "", GH_TOKEN: "" } });
if (bogus.status !== 0 || !/rollup unavailable/.test(bogus.stdout)) fail("a failure must exit 0 with a line: " + bogus.status + " " + bogus.stdout);

fs.rmSync(tmp, { recursive: true, force: true });
if (bad) process.exit(1);
console.log("ok");
