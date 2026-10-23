// scripts/flaky-sync.js, RUN against a fake store, suite, history and gh: the window, idempotence, ADD, REMOVE, the history row.
// subject: scripts/flaky-sync.js, scripts/ci-rollup.js
const fs = require("fs");
const os = require("os");
const path = require("path");
const { sync, runsSince, windowRuns, NO_TEST } = require("./flaky-sync.js");
const { failingDoc } = require("./ci-rollup.js");

let bad = 0;
const fail = (msg) => { console.error("FAIL: " + msg); bad++; };
const eq = (got, want, what) => { if (JSON.stringify(got) !== JSON.stringify(want)) fail(`${what}: got ${JSON.stringify(got)}, want ${JSON.stringify(want)}`); };

const tmp = fs.mkdtempSync(path.join(os.tmpdir(), "flaky-sync-"));
const SUITE = `{
 "note": "n",
 "entries": [
  {"kind": "board", "name": "old", "reason": "r1"},
 {"kind": "go", "package": "internal/x", "name": "TestKept", "reason": "r2"},
  {"kind": "board", "name": "last", "reason": "r3"}
 ]
}
`;
const row = (run, tests, extra = {}) => ({ run: String(run), date: "2026-10-2" + (run % 10) + "T00:00:00Z", sha: "abc", job: run * 10, name: "check (ubuntu-latest, board-1)", tests, retried: [], ...extra });

function world(rows) {
  const dir = fs.mkdtempSync(path.join(tmp, "w-"));
  const p = { store: path.join(dir, "jobs.jsonl"), suite: path.join(dir, "suite.json"), history: path.join(dir, "history.md") };
  fs.writeFileSync(p.store, rows.map((r) => JSON.stringify(r)).join("\n") + "\n");
  fs.writeFileSync(p.suite, SUITE);
  return p;
}
const deps = (over = {}) => ({
  log: () => {}, now: () => "2026-10-30", gitSeen: () => false,
  findPkg: (n) => (n === "TestNew" ? "internal/y" : null),
  download: () => null, gh: () => "", fallback: () => ({ tests: [], errors: {}, retried: [] }), ...over,
});

const fifteen = [];
for (let i = 1; i <= 16; i++) fifteen.push(row(100 + i, []));
eq(windowRuns(fifteen).length, 15, "window holds 15 runs");
eq(windowRuns(fifteen)[0], "116", "newest first");
eq(windowRuns(fifteen).includes("101"), false, "the 16th newest is out");

const aged = [row(101, ["TestKept"]), ...Array.from({ length: 15 }, (_, i) => row(102 + i, i === 1 ? ["last"] : []))];
const pa = world(aged);
let r = sync("117", { ...pa, deps: deps({ download: () => ({ run: "117", date: "2026-10-27T00:00:00Z", sha: "abc", jobs: [] }) }) });
eq(r.removed.map((e) => e.name).sort(), ["TestKept", "old"], "an entry with no failure in the 15 newest runs is REMOVED, a failure in run 101 aged out");
eq(r.unchanged, 1, "one entry unchanged");
let text = fs.readFileSync(pa.suite, "utf8");
eq(text.includes('"name": "old"') || text.includes("TestKept"), false, "removed lines are gone");
eq(JSON.parse(text).entries.map((e) => e.name), ["last"], "the file is still valid JSON with the last entry");
eq(text.split("\n")[0], "{", "the note line is untouched");
eq(/\{"kind": "board", "name": "last", "reason": "r3"\}\n \]/.test(text), true, "the last entry keeps no trailing comma");
let hist = fs.readFileSync(pa.history, "utf8");
eq(/\| 2026-10-27 \| 117 \| old \| REMOVE \| no \| >17 \|/.test(hist), true, "a history row with date, run, test, action, listed before, runs since");
eq(/\| TestKept \| REMOVE \| no \| 16 \|/.test(hist), true, "runs since last failure counts the newer runs");

const p2 = world([row(200, ["old", "TestNew", "newBoard", "check: titles", NO_TEST], { errors: { TestNew: "x_test.go:5: boom" }, retried: ["retried1"] })]);
const dl = { run: "201", date: "2026-10-28T00:00:00Z", sha: "def", jobs: [{ id: 7, name: "check (macos-latest, board-2)", tests: ["TestNew", "zeta", "alpha"], retried: [], errors: { TestNew: "x_test.go:5: boom" } }] };
r = sync("201", { ...p2, deps: deps({ download: () => dl, gitSeen: (n) => n === "TestNew" }) });
eq(r.added.map((a) => a.name), ["alpha", "newBoard", "retried1", "TestNew", "zeta"], "ADD takes every unique failing test of the window, retried included, alphabetized");
eq(r.checks, ["check: titles"], "a check: name is logged and never added");
text = fs.readFileSync(p2.suite, "utf8");
eq(text.includes("check: titles"), false, "no check name in the suite");
const added = JSON.parse(text).entries.find((e) => e.name === "TestNew");
eq(added, { kind: "go", package: "internal/y", name: "TestNew", reason: "failed in run 201 job check (macos-latest, board-2): x_test.go:5: boom" }, "a go entry has its package and a reason with the run, job and first error");
eq(JSON.parse(text).entries.find((e) => e.name === "newBoard").kind, "board", "a non-Test name is a board unit");
eq(JSON.parse(text).entries.find((e) => e.name === "old").reason, "r1", "an entry that stays is not rewritten");
hist = fs.readFileSync(p2.history, "utf8");
eq(/\| TestNew \| ADD \| yes \| 0 \|/.test(hist), true, "ADD row says it was listed before when git saw it");
eq(/\| newBoard \| ADD \| no \| 1 \|/.test(hist), true, "ADD row says no when never listed");

const store1 = fs.readFileSync(p2.store, "utf8");
r = sync("201", { ...p2, deps: deps({ download: () => { throw new Error("must not download twice"); } }) });
eq(fs.readFileSync(p2.store, "utf8"), store1, "a run already in the store is not appended again");
eq(r.added.length + r.removed.length, 0, "a second sync of the same run moves nothing");
eq(fs.readFileSync(p2.history, "utf8"), hist, "and logs nothing");
eq(store1.split("\n").filter(Boolean).length, 2, "the store has the seeded row and the one job of run 201");

const p3 = world([row(300, ["TestKept"])]);
sync("301", { ...p3, deps: deps({ download: () => null, gh: () => JSON.stringify({ id: 9, name: "check (ubuntu-latest, board-1)", conclusion: "failure", started_at: "2026-10-29T00:00:00Z", head_sha: "123456789abc", run_attempt: 1 }) + "\n" + JSON.stringify({ id: 10, name: "rollup", conclusion: "success" }) + "\n" }) });
const rows3 = fs.readFileSync(p3.store, "utf8").split("\n").filter(Boolean).map((l) => JSON.parse(l));
eq(rows3[1].tests, [NO_TEST], "a failed job with no test named is recorded as such");
eq(rows3[1].sha, "123456789", "the job list gives the sha");
eq(rows3.length, 2, "a passing job gets no row");

const p5 = world([row(500, ["TestKept"])]);
sync("501", { ...p5, deps: deps({ download: () => ({ run: "501", date: "d", sha: "s", jobs: [] }), gh: () => JSON.stringify({ id: 7, name: "check (windows-latest, board-1)", conclusion: "failure", started_at: "2026-10-29T00:00:00Z", head_sha: "123456789abc", run_attempt: 1 }), fallback: () => ({ tests: ["mResume"], errors: {}, retried: [] }) }) });
const rows5 = fs.readFileSync(p5.store, "utf8").split(String.fromCharCode(10)).filter(Boolean).map((l) => JSON.parse(l));
eq([rows5.length, rows5[1].tests], [2, ["mResume"]], "an artifact with no jobs does not hide a failed job");

const p4 = world([row(400, ["TestKept"])]);
const before = fs.readFileSync(p4.suite, "utf8");
sync("401", { ...p4, dryRun: true, deps: deps({ download: () => ({ run: "401", date: "d", sha: "s", jobs: [{ id: 1, name: "n", tests: ["TestNew"] }] }) }) });
eq([fs.readFileSync(p4.suite, "utf8"), fs.readFileSync(p4.store, "utf8").split("\n").filter(Boolean).length, fs.existsSync(p4.history)], [before, 1, false], "dry run writes nothing");

eq(runsSince([row(1, ["a"]), row(2, []), row(3, [])], "a"), "2", "runs since counts newer runs");
eq(runsSince([row(1, []), row(2, [])], "a"), ">2", "never failed in the store");

const cur = {
  failures: [
    { os: "ubuntu", job: "board-1", unit: "land", error: "toast missing" },
    { os: "ubuntu", job: "board-1", unit: "step: board", error: "x" },
    { os: "macos", job: "board-1", unit: "other", error: "y" },
  ],
  retried: [{ os: "ubuntu", job: "board-1", unit: "core2" }],
};
const doc = failingDoc({
  run: "5", runInfo: { created_at: "2026-10-30T00:00:00Z", head_sha: "0123456789abcdef" }, current: cur,
  apiJobs: [{ id: 1, name: "check (ubuntu-latest, board-1)", conclusion: "failure" }, { id: 2, name: "check (ubuntu-latest, board-2)", conclusion: "failure" }, { id: 3, name: "rollup", conclusion: "success" }],
});
eq(doc, { run: "5", date: "2026-10-30T00:00:00Z", sha: "012345678", jobs: [
  { id: 1, name: "check (ubuntu-latest, board-1)", tests: ["land"], retried: ["core2"], errors: { land: "toast missing" } },
  { id: 2, name: "check (ubuntu-latest, board-2)", tests: [], retried: [], errors: {} },
  { id: 0, name: "check (macos-latest, board-1)", tests: ["other"], retried: [], errors: { other: "y" } },
] }, "the rollup artifact names each failed job with its tests, retried and first errors");

const EMPTY = ['{', ' "note": "n",', ' "entries": [', ' ]', '}', ''].join("\n");
const pe = world([row(600, ["TestNew", "newBoard"])]);
fs.writeFileSync(pe.suite, EMPTY);
const selectWith = (suite) => require("child_process").spawnSync("bash", [path.join(__dirname, "ci-select.sh")], { encoding: "utf8", env: { ...process.env, FLAKY_SUITE_FILE: suite } }).stdout;
eq(selectWith(pe.suite).includes("flaky_ubuntu=[]"), true, "an empty suite selects no flaky jobs");
r = sync("601", { ...pe, deps: deps({ download: () => ({ run: "601", date: "2026-10-30T00:00:00Z", sha: "abc", jobs: [] }) }) });
eq([r.added.map((a) => a.name), r.removed.length, r.unchanged], [["newBoard", "TestNew"], 0, 0], "with an empty list a failing test is ADDed");
text = fs.readFileSync(pe.suite, "utf8");
eq(JSON.parse(text).entries.map((e) => e.name), ["newBoard", "TestNew"], "the file is valid JSON with the new entries");
eq(selectWith(pe.suite).includes('flaky_ubuntu=["flaky-go"]'), true, "the next select turns the flaky jobs on");
const pz = world([row(700, [])]);
fs.writeFileSync(pz.suite, EMPTY);
sync("701", { ...pz, deps: deps({ download: () => ({ run: "701", date: "2026-10-30T00:00:00Z", sha: "abc", jobs: [] }) }) });
eq(fs.readFileSync(pz.suite, "utf8"), EMPTY, "an empty list with nothing failing is left as it was");

const ps = world([row(800, ["TestNew"]), row(805, ["newBoard"])]);
fs.writeFileSync(ps.suite, ['{', ' "note": "n",', ' "since": 802,', ' "entries": [', ' ]', '}', ''].join("\n"));
sync("806", { ...ps, deps: deps({ download: () => ({ run: "806", date: "2026-10-30T00:00:00Z", sha: "abc", jobs: [] }) }) });
eq(JSON.parse(fs.readFileSync(ps.suite, "utf8")).entries.map((e) => e.name), ["newBoard"], "a failure at or below since stays out, one in a newer run is added");
eq(JSON.parse(fs.readFileSync(ps.suite, "utf8")).since, 802, "since is kept");

fs.rmSync(tmp, { recursive: true, force: true });
if (bad) { console.error(bad + " failure(s)"); process.exit(1); }
console.log("flaky-sync: ok");
