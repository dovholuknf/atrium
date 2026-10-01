#!/usr/bin/env node
// The headless board suite, in parallel: scripts/test-board-headless.js split over several processes of itself.
//
//   node scripts/test-board-sharded.js                    all units, shards chosen from the core count
//   node scripts/test-board-sharded.js --shards 6         six shards
//   node scripts/test-board-sharded.js --units mHome,phonePan    just those units (a pin group comes with its members)
//   node scripts/test-board-sharded.js --save-weights     after the run, write what each unit took to the weight file
//   node scripts/test-board-sharded.js --list             print the shard plan and stop
//   node scripts/test-board-sharded.js --no-retry         do not run a failure a second time (every failure then fails the run)
//   node scripts/test-board-sharded.js --logs dir         keep each shard's output in dir (default: a temp dir, kept on failure)
//   node scripts/test-board-sharded.js --local            run here instead of on sg3 (see below)
//
// WHERE IT RUNS. By default on sg3 (ATRIUM_SUITE_ROOM names another room), through scripts/board-suite-remote.ps1,
// which runs this tree as it is now, committed or not, and streams the report back with the suite's exit code. The
// suite drives up to 15 browsers and lagged sg4, the machine a person works at, where sg3 and m1mini did not, at the
// same wall time. It runs HERE, with a line saying why, when --local or ATRIUM_SUITE_LOCAL=1 is given, when this
// machine is that room, when this repository has no git remote of that name (a worker on m1mini has none), or with
// --save-weights or --logs, which are about this machine's files.

// Each shard is a separate node process with its own mock server on an ephemeral port and its own headless browser,
// started with HEADLESS_UNITS=<its units>. The harness names the units and the groups that must stay together (see
// PIN_GROUPS there). The balancing is greedy, longest first, from scripts/board-suite-weights.json, which a run with
// --save-weights refreshes. A unit with no weight gets the median one.
//
// ONE merged report follows: every unit, slowest first, then the failures. Every failed unit is run once more, alone
// on a quiet machine. A unit named in scripts/board-suite-flaky.json that fails both tries is reported as flaky and
// still fails the run, one that passes the second time is labelled flaky and does not. A unit NOT on the list that
// passes the second time is shown in its own block (it failed under the load of the shards, not on its own, so it
// wants fixing or listing) and still fails the run, so a race is never passed quietly. A unit that fails both tries
// fails the run, as does any unit that did not run, any failure outside every unit, and, with --no-retry, any failure.
// With no flags test-board-headless.js is still the plain serial run.

const { spawn, spawnSync } = require("child_process");
const fs = require("fs");
const os = require("os");
const path = require("path");

const HERE = __dirname;
const HEADLESS = path.join(HERE, "test-board-headless.js");
const WEIGHTS = path.join(HERE, "board-suite-weights.json");
const FLAKY = path.join(HERE, "board-suite-flaky.json");
const DEFAULT_WEIGHT_MS = 8000;

// Three quarters of the cores. Each shard is a node and a Chromium with its renderer, GPU and network processes, and a
// section spends much of its time sleeping on a timer, so this fills a machine without throttling it. More shards
// finish sooner and fail more waits that were set on an idle machine.
function defaultShards() {
  const cores = os.cpus().length;
  return Math.max(2, Math.min(16, Math.floor(cores * 3 / 4)));
}

function parseArgs(argv) {
  const o = { shards: 0, units: null, retry: true, saveWeights: false, list: false, logs: "", local: false };
  for (let i = 0; i < argv.length; i++) {
    const a = argv[i];
    const val = () => { if (i + 1 >= argv.length) { console.error(a + " needs a value"); process.exit(2); } return argv[++i]; };
    if (a === "--shards") o.shards = parseInt(val(), 10);
    else if (a === "--units") o.units = val().split(",").filter(Boolean);
    else if (a === "--no-retry") o.retry = false;
    else if (a === "--save-weights") o.saveWeights = true;
    else if (a === "--list") o.list = true;
    else if (a === "--logs") o.logs = val();
    else if (a === "--local") o.local = true;
    else if (a === "-h" || a === "--help") { console.log(fs.readFileSync(__filename, "utf8").split("\n").slice(1, 20).map(l => l.replace(/^\/\/ ?/, "")).join("\n")); process.exit(0); }
    else { console.error("unknown flag " + a); process.exit(2); }
  }
  if (o.shards && !(o.shards >= 1)) { console.error("--shards is a number of one or more"); process.exit(2); }
  return o;
}

function readJSON(file, fallback) {
  try { return JSON.parse(fs.readFileSync(file, "utf8")); } catch (e) { return fallback; }
}

// The harness's own list of units, in suite order, and its pin groups.
function listUnits() {
  const r = spawnSync(process.execPath, [HEADLESS], { env: Object.assign({}, process.env, { HEADLESS_LIST: "1" }),
    encoding: "utf8", maxBuffer: 1 << 24 });
  const line = String(r.stdout || "").split("\n").reverse().find(l => l.startsWith("{"));
  if (!line) {
    // Playwright or its browser is not installed: the harness says so and exits 0, as check-board.sh treats it.
    console.log(String(r.stdout || "").trim() || String(r.stderr || "").trim() || "the harness listed no units");
    process.exit(r.status === 0 ? 0 : 1);
  }
  return JSON.parse(line);
}

// Units that must run together become one group, in suite order. Everything else is a group of one.
function makeGroups(list, only) {
  const order = list.units, at = new Map(order.map((u, i) => [u, i]));
  const pinOf = new Map();
  for (const [g, p] of Object.entries(list.pins || {})) for (const u of p.units) pinOf.set(u, g);
  const groups = [];
  const byPin = new Map();
  for (const u of order) {
    const g = pinOf.get(u);
    if (g) {
      if (!byPin.has(g)) { const x = { name: g, units: [] }; byPin.set(g, x); groups.push(x); }
      byPin.get(g).units.push(u);
    } else groups.push({ name: u, units: [u] });
  }
  if (!only) return groups;
  for (const u of only) if (!at.has(u)) { console.error("no unit called " + u + ". --list shows them."); process.exit(2); }
  const want = new Set(only);
  return groups.filter(g => g.units.some(u => want.has(u)));
}

function median(xs) {
  if (!xs.length) return DEFAULT_WEIGHT_MS;
  const s = xs.slice().sort((a, b) => a - b);
  return s[Math.floor(s.length / 2)];
}

// Longest group first onto the shard with the least so far.
function plan(groups, weights, n) {
  const known = Object.values(weights).filter(x => x > 0);
  const dflt = median(known);
  const w = u => (weights[u] > 0 ? weights[u] : dflt);
  for (const g of groups) g.weight = g.units.reduce((s, u) => s + w(u), 0);
  const shards = Array.from({ length: Math.min(n, groups.length) }, () => ({ groups: [], weight: 0 }));
  for (const g of groups.slice().sort((a, b) => b.weight - a.weight)) {
    const s = shards.reduce((m, x) => (x.weight < m.weight ? x : m));
    s.groups.push(g);
    s.weight += g.weight;
  }
  return shards;
}

function unitsOf(shard, order) {
  const at = new Map(order.map((u, i) => [u, i]));
  return shard.groups.flatMap(g => g.units).sort((a, b) => at.get(a) - at.get(b));
}

function runShard(label, units, dir) {
  const results = path.join(dir, label + ".json");
  const log = path.join(dir, label + ".log");
  try { fs.unlinkSync(results); } catch (e) {}
  const out = fs.openSync(log, "w");
  const t0 = Date.now();
  return new Promise(resolve => {
    const child = spawn(process.execPath, [HEADLESS], {
      env: Object.assign({}, process.env, { HEADLESS_UNITS: units.join(","), HEADLESS_RESULTS: results }),
      stdio: ["ignore", out, out], windowsHide: true });
    runShard.children.add(child);
    child.on("close", code => {
      runShard.children.delete(child);
      fs.closeSync(out);
      const r = readJSON(results, null);
      resolve({ label, units, code, ms: Date.now() - t0, results: r ? r.units : {}, bad: r ? r.bad : 0, log, wrote: !!r });
    });
    child.on("error", e => { fs.closeSync(out); resolve({ label, units, code: -1, ms: 0, results: {}, log, wrote: false, error: e.message }); });
  });
}
runShard.children = new Set();

// Average and peak CPU over the run, from os.cpus() once a second.
function cpuSampler() {
  const snap = () => os.cpus().reduce((a, c) => { const t = Object.values(c.times).reduce((x, y) => x + y, 0); return { idle: a.idle + c.times.idle, total: a.total + t }; }, { idle: 0, total: 0 });
  let last = snap(); const first = last; let peak = 0;
  const timer = setInterval(() => {
    const now = snap(), d = now.total - last.total;
    if (d > 0) peak = Math.max(peak, 100 * (1 - (now.idle - last.idle) / d));
    last = now;
  }, 1000);
  return { stop() { clearInterval(timer); const now = snap(), d = now.total - first.total; return { avg: d > 0 ? 100 * (1 - (now.idle - first.idle) / d) : 0, peak }; } };
}

const fmt = ms => ms >= 60000 ? Math.floor(ms / 60000) + "m" + String(Math.round((ms % 60000) / 1000)).padStart(2, "0") + "s" : (ms / 1000).toFixed(1) + "s";

// Hand the whole run to another room's machine and return its exit code, or null when this run stays here, saying why.
function dispatchRemote(opt) {
  const room = process.env.ATRIUM_SUITE_ROOM || "sg3";
  const here = why => { console.log("board suite: running here (" + why + ")"); return null; };
  if (opt.local) return here("--local");
  if (process.env.ATRIUM_SUITE_LOCAL === "1") return here("ATRIUM_SUITE_LOCAL=1");
  if (process.env.ATRIUM_SUITE_REMOTE === "1") return here("this is the remote half");
  if (opt.saveWeights || opt.logs) return here("--save-weights and --logs are about this machine's files");
  if (os.hostname().toLowerCase().split(".")[0] === room.toLowerCase()) return here("this machine is " + room);
  const remote = spawnSync("git", ["remote", "get-url", room], { cwd: path.dirname(HERE), encoding: "utf8" });
  if (remote.status !== 0) return here("no git remote named " + room + " in this repository");
  const args = [];
  if (opt.shards) args.push("--shards", String(opt.shards));
  if (opt.units) args.push("--units", opt.units.join(","));
  if (!opt.retry) args.push("--no-retry");
  if (opt.list) args.push("--list");
  console.log("board suite: running on " + room + " (--local runs it here)");
  const r = spawnSync("pwsh", ["-NoProfile", "-File", path.join(HERE, "board-suite-remote.ps1"), "-Room", room,
    "-SuiteArgs", args.join(" ")], { stdio: "inherit" });
  if (r.error) { console.error("board suite: could not start pwsh for the remote run: " + r.error.message + ". --local runs it here"); return 4; }
  return r.status === null ? 1 : r.status;
}

async function main() {
  const opt = parseArgs(process.argv.slice(2));
  const remoteCode = dispatchRemote(opt);
  if (remoteCode !== null) return remoteCode;
  const list = listUnits();
  const groups = makeGroups(list, opt.units);
  const weights = (readJSON(WEIGHTS, {}) || {}).ms || {};
  const flaky = (readJSON(FLAKY, {}) || {}).flaky || {};
  const n = opt.shards || defaultShards();
  const shards = plan(groups, weights, n);
  const wanted = groups.flatMap(g => g.units);

  console.log("board suite: " + wanted.length + " units in " + shards.length + " shards on " + os.cpus().length + " cores" +
    (Object.keys(weights).length ? "" : " (no weight file, so the balance is by count)"));
  if (opt.list) {
    shards.forEach((s, i) => console.log("shard " + (i + 1) + " (~" + fmt(s.weight) + "): " + unitsOf(s, list.units).join(" ")));
    for (const [g, p] of Object.entries(list.pins || {})) console.log("pinned " + g + ": " + p.units.join(" ") + " (" + p.why + ")");
    return 0;
  }

  const dir = opt.logs || fs.mkdtempSync(path.join(os.tmpdir(), "atrium-shards-"));
  fs.mkdirSync(dir, { recursive: true });
  const stop = () => { for (const c of runShard.children) { try { c.kill(); } catch (e) {} } process.exit(130); };
  process.on("SIGINT", stop); process.on("SIGTERM", stop);

  const cpu = cpuSampler();
  const t0 = Date.now();
  // a fail() outside every unit counts in the shard's total and in no unit, and a crash after the results were written
  // leaves a nonzero exit with nothing failed: neither may read as a pass
  const stray = [];
  const strays = r => {
    if (!r.wrote) return;
    const inUnits = Object.values(r.results).reduce((s, x) => s + x.fails.length, 0);
    if (r.bad > inUnits) stray.push(r.label + ": " + (r.bad - inUnits) + " failure(s) outside any unit. see " + r.log);
    else if (r.code !== 0 && !r.bad) stray.push(r.label + ": exit " + r.code + " with nothing failed. see " + r.log);
  };
  const runs = await Promise.all(shards.map((s, i) => runShard("shard-" + (i + 1), unitsOf(s, list.units), dir).then(r => {
    const bad = r.units.filter(u => !(r.results[u] && r.results[u].ok)).length;
    console.log(r.label + " finished in " + fmt(r.ms) + ": " + r.units.length + " units" + (bad ? ", " + bad + " not passing" : ""));
    strays(r);
    return r;
  })));

  // what each unit did, first try
  const got = {};
  for (const r of runs) for (const u of r.units) {
    const x = r.results[u];
    got[u] = x ? { ms: x.ms, ok: x.ok, fails: x.fails, shard: r.label, tries: 1 }
      : { ms: 0, ok: false, fails: ["the unit did not run: " + r.label + " ended (exit " + r.code + ") before it reported. see " + r.log], shard: r.label, tries: 1, notRun: true };
  }

  // a failure runs once more, alone, with its pin group, two at a time, since some sections fail with three browsers busy beside them
  if (opt.retry) {
    const failed = u => got[u] && !got[u].ok;
    const again = groups.filter(g => g.units.some(failed));
    if (again.length) console.log("running " + again.length + " failed unit" + (again.length > 1 ? "s" : "") + " once more, alone: " +
      again.map(g => g.units.filter(failed).join(",")).join(" "));
    const pool = 2;
    const rr = [];
    for (let i = 0; i < again.length; i += pool)
      rr.push(...await Promise.all(again.slice(i, i + pool).map((g, j) => runShard("retry-" + (i + j + 1), g.units, dir))));
    rr.forEach(strays);
    for (const r of rr) for (const u of r.units) {
      if (!failed(u) || got[u].tries === 2) continue;
      const x = r.results[u];
      got[u].tries = 2;
      got[u].retryOk = !!(x && x.ok);
      got[u].retryFails = x ? x.fails : ["the retry did not run (exit " + r.code + "). see " + r.log];
      if (x) got[u].retryMs = x.ms;
    }
  }
  const wall = Date.now() - t0;
  const load = cpu.stop();

  // classify
  const rows = wanted.map(u => {
    const x = got[u];
    let status;
    if (x.ok) status = "PASS";
    else if (x.tries === 2 && x.retryOk) status = flaky[u] ? "FLAKY (passed on retry)" : "LOAD (passed on retry)";
    else if (x.tries === 2) status = flaky[u] ? "FLAKY (failed every try)" : "FAIL";
    else status = x.notRun ? "NOT RUN" : (flaky[u] ? "FLAKY (failed, not retried)" : "FAIL");
    return Object.assign({ unit: u, status }, x);
  });
  const hard = rows.filter(r => r.status === "FAIL" || r.status === "NOT RUN" || r.status === "FLAKY (failed every try)" ||
    r.status === "FLAKY (failed, not retried)" || r.status === "LOAD (passed on retry)");
  const soft = rows.filter(r => r.status.startsWith("FLAKY") && !hard.includes(r));
  const load_ = rows.filter(r => r.status === "LOAD (passed on retry)");
  const failed_ = hard.filter(r => !load_.includes(r));

  console.log("\nunit".padEnd(26) + "time".padStart(9) + "  result");
  for (const r of rows.slice().sort((a, b) => b.ms - a.ms)) console.log(r.unit.padEnd(25) + fmt(r.ms).padStart(9) + "  " + r.status + "  " + r.shard + (r.retryMs ? "  (retry " + fmt(r.retryMs) + ")" : ""));

  if (failed_.length) {
    console.log("\nFAILED (" + failed_.length + "):");
    for (const r of failed_) {
      console.log("  " + r.unit + (r.status === "FLAKY (failed every try)" ? "  [flaky, failed both tries]" :
        r.status === "FLAKY (failed, not retried)" ? "  [flaky, not retried]" : ""));
      for (const m of r.fails.slice(0, 6)) console.log("    " + m.split("\n")[0].slice(0, 300));
      if (r.fails.length > 6) console.log("    ... and " + (r.fails.length - 6) + " more");
    }
  }
  if (soft.length) {
    console.log("\nFLAKY, reported and not failing the run (" + soft.length + "):");
    for (const r of soft) {
      console.log("  " + r.unit + ": " + r.status + ". " + flaky[r.unit].reason.split("\n")[0].slice(0, 160));
      for (const m of r.fails.slice(0, 3)) console.log("    first try: " + m.split("\n")[0].slice(0, 300));
    }
  }

  if (load_.length) {
    console.log("\nFAILED IN THE SHARDS, PASSED ALONE, AND NOT ON THE FLAKY LIST (" + load_.length + "). These fail the run. Fix the wait, or list it with its reason:");
    for (const r of load_) {
      console.log("  " + r.unit);
      for (const m of r.fails.slice(0, 3)) console.log("    first try: " + m.split("\n")[0].slice(0, 300));
    }
  }

  if (stray.length) {
    console.log("\nFAILED OUTSIDE ANY UNIT (" + stray.length + "):");
    for (const s of stray) console.log("  " + s);
  }

  const sum = rows.reduce((s, r) => s + r.ms, 0);
  console.log("\n" + rows.filter(r => r.status === "PASS").length + " of " + rows.length + " units passed" +
    (soft.length ? ", " + soft.length + " flaky" : "") + (load_.length ? ", " + load_.length + " passed only on retry (failing the run)" : "") +
    (failed_.length ? ", " + failed_.length + " failed" : "") + (stray.length ? ", " + stray.length + " outside any unit" : "") + ".");
  console.log("wall time " + fmt(wall) + " on " + shards.length + " shards (the units add up to " + fmt(sum) + " run one after another)." +
    " cpu " + load.avg.toFixed(0) + "% average, " + load.peak.toFixed(0) + "% peak.");
  const keep = hard.length || stray.length || opt.logs;
  if (keep) console.log("shard output: " + dir);

  if (opt.saveWeights) {
    const old = (readJSON(WEIGHTS, {}) || {}).ms || {};
    const ms = Object.assign({}, old);
    for (const r of rows) if (!r.notRun) ms[r.unit] = r.ms;
    const sorted = {}; for (const k of Object.keys(ms).sort()) sorted[k] = ms[k];
    fs.writeFileSync(WEIGHTS, JSON.stringify({
      note: "Milliseconds each unit took in a sharded run. test-board-sharded.js balances shards from it. Refresh with --save-weights.",
      ms: sorted }, null, 1) + "\n");
    console.log("weights written to " + path.relative(process.cwd(), WEIGHTS));
  }
  if (!keep) { try { fs.rmSync(dir, { recursive: true, force: true }); } catch (e) {} }
  return hard.length || stray.length ? 1 : 0;
}

main().then(code => process.exit(code), e => { console.error(e); process.exit(1); });
