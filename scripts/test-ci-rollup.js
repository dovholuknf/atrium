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

const { build, escData, escProp } = require("./ci-rollup");
if (escData("a%b\r\nc:d,e") !== "a%25b%0D%0Ac:d,e") fail("data escaping: " + escData("a%b\r\nc:d,e"));
if (escProp("a%b\r\nc:d,e") !== "a%25b%0D%0Ac%3Ad%2Ce") fail("property escaping: " + escProp("a%b\r\nc:d,e"));

const mk = (unit, over) => ({ os: "ubuntu", job: "go-all", group: "go-test", unit, error: "boom", retried: false, ...over });
const many = [];
for (let i = 0; i < 14; i++) many.push(mk("u" + String(i).padStart(2, "0"), { retried: i === 3 }));
many.push(mk("fresh%\n,:x", { error: "line1\nline2 100%" }));
const hist = [{ id: "1", known: true, failures: many.slice(0, 14) }];
const cmds = build({ run: "9", current: { jobs: [{}], failures: many }, histRuns: hist }).commands;
const errs = cmds.filter(c => c.startsWith("::error"));
if (cmds.length > 10) fail("at most 10 annotations: " + cmds.length);
if (errs.length !== 8) fail("8 errors when cut: " + errs.length);
if (!errs[0].startsWith("::error title=fresh%25%0A%2C%3Ax::ubuntu go-all: line1 line2 100%25 | retry: no retry | history: NEW")) fail("NEW first and escaped: " + errs[0]);
if (!errs[1].startsWith("::error title=u03::")) fail("passed-on-retry second: " + errs[1]);
if (!errs[2].startsWith("::error title=u00::")) fail("the rest follow: " + errs[2]);
if (cmds[8] !== "::notice title=ci run 9::and 7 more, see the table") fail("more line: " + cmds[8]);
if (cmds[9] !== "::notice title=ci run 9::15 failing units, 1 new, 1 passed on retry") fail("totals: " + cmds[9]);
if (cmds.some(c => /[\r\n]/.test(c))) fail("a command must be one line");

const few = build({ run: "9", current: { jobs: [{}], failures: many.slice(0, 2) }, histRuns: hist }).commands;
if (few.length !== 3 || few.some(c => c.includes("more"))) fail("no more line when nothing was cut: " + few.join("\n"));

if (!/^::notice title=ci run 297::green, all 1 jobs passed\n$/.test(green.split("\n").filter(l => l.startsWith("::")).join("\n") + "\n")) fail("green notice:\n" + green);
if (/::error/.test(green)) fail("green run has no error");

const sumFile = path.join(tmp, "summary.md");
fs.writeFileSync(sumFile, "before\n");
const split = run(["--local", path.join(runs, "300"), "--run", "300", "--summary", sumFile]);
if (/^## /m.test(split) || !/^::error title=brandNew::/m.test(split)) fail("stdout is commands only with --summary:\n" + split);
const written = fs.readFileSync(sumFile, "utf8");
if (!written.startsWith("before\n## ci run 300: 4 failing units")) fail("summary is appended: " + written);

if (!/^::warning title=ci rollup unavailable::/m.test(bogus.stdout) || /::error/.test(bogus.stdout)) fail("failure prints one warning: " + bogus.stdout);
const bogus2 = spawnSync("node", [script, "--repo", "a/b", "--run", "1", "--summary", sumFile], { encoding: "utf8", env: { ...process.env, GITHUB_TOKEN: "", GH_TOKEN: "" } });
if (bogus2.status !== 0 || /^## /m.test(bogus2.stdout) || !fs.readFileSync(sumFile, "utf8").endsWith("no token\n")) fail("failure with --summary: " + bogus2.stdout);

fs.rmSync(tmp, { recursive: true, force: true });
if (bad) process.exit(1);
console.log("ok");
