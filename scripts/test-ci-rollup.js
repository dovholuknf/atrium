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

function job(run, name, failing, retried, boardLog) {
  const d = path.join(tmp, "runs", run, name);
  fs.mkdirSync(d, { recursive: true });
  fs.writeFileSync(path.join(d, "failing.tsv"), failing.map(r => r.join("\t")).join("\n") + (failing.length ? "\n" : ""));
  fs.writeFileSync(path.join(d, "steps.tsv"), "machine\t0\tok\t-\t-\n");
  if (boardLog) fs.writeFileSync(path.join(d, "board.log"), boardLog);
  if (retried) fs.writeFileSync(path.join(d, "retried.tsv"), retried.map(r => r.join("\t")).join("\n") + "\n");
}

const retriedRows = [["board", "listedSlow", "boom | slow", "first try 12.3s failed, retry 4.1s passed"]];
const retryLog = "x\nFAILED IN THE SHARDS, PASSED ALONE, AND NOT ON THE FLAKY LIST (1). These fail the run.\n  flakyUnit\n    first try: boom\n\n175 of 176 units passed\n";
const MOD = "github.com/dovholuknf/atrium/internal/ptyhost";

job("300", "ci-ubuntu-latest-board-1-attempt-1", [["board", "brandNew", "it broke: a | b"], ["board", "flakyUnit", "slow"]], retriedRows, retryLog);
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

if (!/: 4 failing units/.test(text)) fail("wanted 4 failing units, a listed retried pass is not one:\n" + text);
if (!/NEW/.test(row("brandNew"))) fail("brandNew should be NEW: " + row("brandNew"));
if (!row("brandNew").includes("a \\| b")) fail("a pipe in an error should be escaped: " + row("brandNew"));
if (!/failed in 2 of the last 4/.test(row("TestKnown"))) fail("TestKnown history: " + row("TestKnown"));
if (!/\(1 unknown\)/.test(row("TestKnown"))) fail("the expired run should count as unknown: " + row("TestKnown"));
if (!/### 1 unit PASSED ON RETRY \(not clean passes\)/.test(text)) fail("the retried section:\n" + text);
if (!/passed on retry, not on the flaky list, fails the job/.test(row("flakyUnit"))) fail("an unlisted retried pass still fails the job: " + row("flakyUnit"));
if (!/\| listedSlow \| first try 12\.3s failed, retry 4\.1s passed \| boom \\\| slow \| NEW/.test(row("listedSlow"))) fail("listedSlow row: " + row("listedSlow"));
if (/fails the job/.test(row("listedSlow"))) fail("a listed retried pass does not fail the job:\n" + row("listedSlow"));
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

const mk = (unit, over) => ({ os: "ubuntu", job: "go-all", group: "go-test", unit, error: "boom", ...over });
const many = [];
for (let i = 0; i < 14; i++) many.push(mk("u" + String(i).padStart(2, "0")));
many.push(mk("fresh%\n,:x", { error: "line1\nline2 100%" }));
const hist = [{ id: "1", known: true, failures: many.slice(0, 14) }];
const slow = mk("u03", { group: "board", error: "stalled", times: "first try 9.0s failed, retry 2.0s passed" });
many[3].retried = true;
const cmds = build({ run: "9", current: { jobs: [{}], failures: many, retried: [slow] }, histRuns: hist }).commands;
const errs = cmds.filter(c => c.startsWith("::error"));
if (cmds.length > 10) fail("at most 10 annotations: " + cmds.length);
if (errs.length !== 8) fail("8 errors when cut: " + errs.length);
if (!errs[0].startsWith("::error title=fresh%25%0A%2C%3Ax::ubuntu go-all: line1 line2 100%25 | retry: no retry | history: NEW")) fail("NEW first and escaped: " + errs[0]);
if (!errs[1].startsWith("::error title=u03::")) fail("unlisted passed-on-retry second: " + errs[1]);
if (!errs[2].startsWith("::error title=u00::")) fail("the rest follow: " + errs[2]);
if (cmds[8] !== "::notice title=ci run 9::and 8 more, see the table") fail("more line: " + cmds[8]);
if (cmds[9] !== "::notice title=ci run 9::15 failing units, 1 new, 1 unlisted passed on retry, 1 passed on retry (not clean)") fail("totals: " + cmds[9]);
if (cmds.some(c => /[\r\n]/.test(c))) fail("a command must be one line");

const few = build({ run: "9", current: { jobs: [{}], failures: many.slice(0, 2) }, histRuns: hist }).commands;
if (few.length !== 3 || few.some(c => c.includes("more"))) fail("no more line when nothing was cut: " + few.join("\n"));

const greenRetry = build({ run: "9", current: { jobs: [{}, {}], failures: [], retried: [slow] }, histRuns: hist });
if (greenRetry.commands[0] !== "::notice title=ci run 9::green, all 2 jobs passed, 1 unit PASSED ON RETRY (not clean)") fail("green with a retried pass: " + greenRetry.commands[0]);
if (greenRetry.commands[1] !== "::warning title=PASSED ON RETRY%3A u03::ubuntu go-all: not a clean pass, the job passed. first try 9.0s failed, retry 2.0s passed | first try: stalled | history: NEW") fail("the warning: " + greenRetry.commands[1]);
if (!/^## ci run 9: green, but 1 unit PASSED ON RETRY \(not clean passes\)/.test(greenRetry.summary)) fail("green summary says so:\n" + greenRetry.summary);
if (/::error/.test(greenRetry.commands.join("\n"))) fail("a retried pass is a warning, not an error");

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

const rep = (unit, pass) => ["flaky-go", unit, `[pass ${pass}/5] x_test.go:1: nope`];
job("500", "ci-ubuntu-latest-flaky-go-attempt-1", [rep("TestWobble", 2), rep("TestWobble", 4), rep("TestSteady", 4), ["flaky-go", "TestPlain", "no tag"]]);
const repeated = run(["--local", path.join(runs, "500"), "--run", "500"]);
if (!/: 3 failing units/.test(repeated)) fail("a test failing in 2 passes is one unit:\n" + repeated);
if (!/\| TestWobble \(failed 2 of 5 passes\) \| x_test\.go:1: nope |/.test(repeated)) fail("pass count and tag stripped from the error:\n" + repeated);
if (!/\| TestSteady \(failed 1 of 5 passes\) \|/.test(repeated)) fail("one failed pass:\n" + repeated);
if (!/\| TestPlain \| no tag \|/.test(repeated)) fail("an untagged row is unchanged:\n" + repeated);
const { jobFailures } = require("./ci-failures");
const rows500 = jobFailures(path.join(runs, "500", "ci-ubuntu-latest-flaky-go-attempt-1"));
if (rows500.length !== 4 || rows500[0].pass !== 2 || rows500[0].of !== 5) fail("rows keep their pass: " + JSON.stringify(rows500));
const fdoc = require("./ci-rollup").failingDoc({ run: "500", apiJobs: [{ id: 1, name: "check (ubuntu-latest, flaky-go)", conclusion: "failure" }], current: { failures: rows500.map((f) => ({ ...f, job: "flaky-go" })), retried: [] } });
if (fdoc.jobs[0].tests.join() !== "TestPlain,TestSteady,TestWobble") fail("failing-tests lists each test once: " + fdoc.jobs[0].tests);
if (fdoc.jobs[0].passes.TestWobble.failed !== 2 || fdoc.jobs[0].passes.TestWobble.of !== 5) fail("failing-tests carries the pass count: " + JSON.stringify(fdoc.jobs[0].passes));

fs.rmSync(tmp, { recursive: true, force: true });
if (bad) process.exit(1);
console.log("ok");
