#!/usr/bin/env node
'use strict';
const fs = require('fs');
const path = require('path');
const { execFileSync } = require('child_process');
const { jobFailures } = require('./ci-failures');

const DATA = process.env.LEDGER_DIR || 'D:/tmp/qa-flake-ledger';
const ARTIFACTS = process.env.CI_ARTIFACTS || 'C:/Users/claude/.atrium2/hub/ci-artifacts/github.com/dovholuknf/atrium';
const HISTORY_CSV = process.env.CI_HISTORY_CSV || 'D:/tmp/qa-ci-history.csv';
const HISTORY_MD = process.env.CI_HISTORY_MD || 'D:/tmp/qa-ci-history.md';
const FIX_REPO = process.env.FIX_REPO || 'D:/worktrees/claude/atrium/qa';
const OSES = ['windows', 'macos', 'ubuntu'];

const file = (n) => path.join(DATA, n);
const readJson = (n, dflt) => (fs.existsSync(file(n)) ? JSON.parse(fs.readFileSync(file(n), 'utf8')) : dflt);
const writeJson = (n, v) => { fs.mkdirSync(DATA, { recursive: true }); fs.writeFileSync(file(n), JSON.stringify(v, null, 1) + '\n'); };
const idNum = (id) => BigInt(id);
const byId = (a, b) => (idNum(a.id) < idNum(b.id) ? -1 : idNum(a.id) > idNum(b.id) ? 1 : 0);
const loadLedger = () => readJson('ledger.json', { runs: [], failures: [], baseline: { runs: [], presence: {} } });
const key = (f) => `${f.group}|${f.unit}`;

function csvLine(line) {
  const out = [];
  let cur = '';
  let q = false;
  for (let i = 0; i < line.length; i++) {
    const c = line[i];
    if (q) {
      if (c === '"' && line[i + 1] === '"') { cur += '"'; i++; } else if (c === '"') q = false; else cur += c;
    } else if (c === '"') q = true;
    else if (c === ',') { out.push(cur); cur = ''; } else cur += c;
  }
  out.push(cur);
  return out;
}

function seed() {
  const md = fs.readFileSync(HISTORY_MD, 'utf8');
  const cols = [...md.matchAll(/\| result(\d+) \| (\d+) \| ([0-9a-f]+)/g)].map((m) => ({ col: +m[1], id: m[2], sha: m[3] }));
  if (cols.length !== 10) throw new Error(`expected 10 history runs in ${HISTORY_MD}, found ${cols.length}`);
  const meta = readJson('runs.json', {});
  const runs = cols.map((c) => ({ id: c.id, sha: c.sha, date: (meta[c.id] || {}).date || null, conclusion: 'failure', source: 'history' }));
  const failures = [];
  const presence = {};
  const lines = fs.readFileSync(HISTORY_CSV, 'utf8').split(/\r?\n/).slice(1).filter(Boolean);
  for (const l of lines) {
    const f = csvLine(l);
    const [unit, group, os] = f;
    const res = f.slice(3, 13);
    const k = `${group}|${unit}`;
    const p = presence[k] || (presence[k] = Array(10).fill('0'));
    res.forEach((r, i) => {
      if (r !== '-') p[i] = '1';
      if (r === 'F' || r === 'F*') failures.push({ run: cols[i].id, os, group, unit, error: '', retried: r === 'F*' });
    });
  }
  for (const k of Object.keys(presence)) presence[k] = presence[k].join('');
  const prev = loadLedger();
  const keep = prev.runs.filter((r) => r.source !== 'history');
  const keepIds = new Set(keep.map((r) => r.id));
  const histIds = new Set(cols.map((c) => c.id));
  const all = runs.concat(keep.filter((r) => !histIds.has(r.id)));
  const ledger = {
    runs: all.sort(byId),
    failures: failures.concat(prev.failures.filter((f) => keepIds.has(f.run) && !histIds.has(f.run))),
    baseline: { runs: cols.map((c) => c.id), presence },
  };
  writeJson('ledger.json', ledger);
  console.log(`seeded ${runs.length} history runs, ${failures.length} failures, ${Object.keys(presence).length} units`);
}

function runSha(runDir) {
  for (const d of fs.readdirSync(runDir)) {
    const s = path.join(runDir, d, 'summary.md');
    if (fs.existsSync(s)) {
      const m = /commit:\s*([0-9a-f]{7,40})/.exec(fs.readFileSync(s, 'utf8'));
      if (m) return m[1].slice(0, 7);
    }
  }
  return null;
}

function ingest(args) {
  let conclusion = null;
  const ids = [];
  for (let i = 0; i < args.length; i++) {
    if (args[i] === '--conclusion') conclusion = args[++i];
    else ids.push(args[i]);
  }
  if (!ids.length) throw new Error('ingest needs at least one run id');
  const ledger = loadLedger();
  const meta = readJson('runs.json', {});
  const have = new Set(ledger.runs.map((r) => r.id));
  for (const id of ids) {
    if (!/^\d+$/.test(id)) throw new Error(`not a run id: ${id}`);
    if (have.has(id)) { console.log(`${id}: already in the ledger`); continue; }
    const dir = path.join(ARTIFACTS, id);
    const hasDir = fs.existsSync(dir);
    if (!hasDir && conclusion !== 'success') {
      console.log(`${id}: no artifact folder. download its failing jobs with atrium_ci first, or pass --conclusion success if the run was green`);
      continue;
    }
    const fails = [];
    const jobs = hasDir ? fs.readdirSync(dir).filter((d) => fs.statSync(path.join(dir, d)).isDirectory()) : [];
    if (hasDir && !jobs.length && conclusion !== 'success') {
      console.log(`${id}: folder is empty, its artifacts were never downloaded`);
      continue;
    }
    for (const d of jobs) fails.push(...jobFailures(path.join(dir, d)));
    const seen = new Set();
    let n = 0;
    for (const f of fails) {
      const k = `${f.os}|${f.group}|${f.unit}`;
      if (seen.has(k)) continue;
      seen.add(k);
      ledger.failures.push({ run: id, os: f.os, group: f.group, unit: f.unit, error: f.error });
      n++;
    }
    const m = meta[id] || {};
    ledger.runs.push({ id, sha: (hasDir && runSha(dir)) || m.sha || null, date: m.date || null, conclusion: conclusion || 'failure', source: 'artifacts' });
    console.log(`${id}: ingested, ${n} failures`);
  }
  ledger.runs.sort(byId);
  writeJson('ledger.json', ledger);
}

function listMissing() {
  const have = new Set(loadLedger().runs.map((r) => r.id));
  const dirs = fs.existsSync(ARTIFACTS) ? fs.readdirSync(ARTIFACTS).filter((d) => /^\d+$/.test(d)) : [];
  const missing = dirs.filter((d) => !have.has(d)).sort((a, b) => (idNum(a) < idNum(b) ? -1 : 1));
  console.log(missing.length ? missing.join('\n') : '(none)');
}

function seedFixes() {
  const ledger = loadLedger();
  const units = [...new Set(ledger.failures.map((f) => f.unit))].filter((u) => /^[A-Za-z][A-Za-z0-9_]{5,}$/.test(u));
  const log = execFileSync('git', ['-C', FIX_REPO, 'log', '--no-merges', '--format=%h\t%s', '-1500', 'claude/main'], { encoding: 'utf8', maxBuffer: 1 << 26 });
  const fixes = readJson('fixes.json', {});
  const subjects = new Set();
  let added = 0;
  for (const l of log.split('\n').filter(Boolean)) {
    const [sha, ...rest] = l.split('\t');
    const subject = rest.join('\t');
    if (subjects.has(subject)) continue;
    subjects.add(subject);
    for (const u of units) {
      if (!new RegExp(`(^|[^A-Za-z0-9_])${u}([^A-Za-z0-9_]|$)`).test(subject)) continue;
      const list = fixes[u] || (fixes[u] = []);
      if (!list.some((e) => e.subject === subject)) { list.push({ sha, subject }); added++; }
    }
  }
  writeJson('fixes.json', fixes);
  console.log(`fixes.json: ${added} entries added for ${Object.keys(fixes).length} units`);
}

function buildStats(ledger) {
  const runs = ledger.runs.slice().sort(byId);
  const baseCols = ledger.baseline.runs;
  const stats = new Map();
  const get = (f) => {
    const k = key(f);
    if (!stats.has(k)) stats.set(k, { group: f.group, unit: f.unit, fails: [] });
    return stats.get(k);
  };
  for (const f of ledger.failures) get(f).fails.push(f);
  for (const s of stats.values()) {
    const k = `${s.group}|${s.unit}`;
    const pres = ledger.baseline.presence[k];
    const failIds = s.fails.map((f) => f.run);
    const firstFail = failIds.slice().sort((a, b) => (idNum(a) < idNum(b) ? -1 : 1))[0];
    let start = firstFail;
    if (pres) {
      const i = pres.indexOf('1');
      if (i >= 0) start = baseCols[i];
    }
    s.start = start;
    s.firstSeen = firstFail;
    s.totalRuns = runs.filter((r) => {
      if (idNum(r.id) < idNum(start)) return false;
      const bi = baseCols.indexOf(r.id);
      return bi < 0 || !pres || pres[bi] === '1' || !pres.includes('1');
    }).length;
    s.totalFailRuns = new Set(failIds).size;
  }
  return { runs, stats };
}

function fixedBy(fixes, unit) {
  const l = fixes[unit];
  return l && l.length ? l.map((e) => `${e.sha} ${e.subject}`).join('; ') : '';
}

function report(args) {
  let want = 10;
  let latest = 3;
  for (let i = 0; i < args.length; i++) {
    if (args[i] === '--window') want = +args[++i];
    else if (args[i] === '--latest') latest = +args[++i];
  }
  if (want !== 10 && want !== 20) throw new Error('--window is 10 or 20');
  const ledger = loadLedger();
  const fixes = readJson('fixes.json', {});
  const { runs, stats } = buildStats(ledger);
  const n = Math.min(want, runs.length);
  const win = runs.slice(-n);
  const winIds = new Set(win.map((r) => r.id));
  const latestRuns = runs.slice(-Math.min(latest, runs.length));
  const latestIds = new Set(latestRuns.map((r) => r.id));
  const firstLatest = idNum(latestRuns[0].id);
  const out = [];
  out.push(`# Flake ledger report`);
  out.push('');
  out.push(`Ledger holds ${runs.length} runs (${runs[0].id} to ${runs[runs.length - 1].id}), ${ledger.failures.length} failure rows, ${stats.size} distinct failing units.`);
  out.push(want > runs.length ? `Asked for the last ${want} runs, the ledger has only ${runs.length}: window is ${n}.` : `Window: the last ${n} runs.`);
  out.push('');
  out.push('Window runs, oldest first:');
  out.push('');
  out.push('| run | sha | conclusion | source |');
  out.push('|---|---|---|---|');
  for (const r of win) out.push(`| ${r.id} | ${r.sha || '?'} | ${r.conclusion} | ${r.source} |`);
  out.push('');
  const rows = [];
  for (const s of stats.values()) {
    const inWin = s.fails.filter((f) => winIds.has(f.run));
    if (!inWin.length) continue;
    const per = OSES.map((os) => `${new Set(inWin.filter((f) => f.os === os).map((f) => f.run)).size}/${n}`);
    const failedLatest = s.fails.some((f) => latestIds.has(f.run));
    rows.push({
      s, inWin, per, failedLatest,
      winRuns: new Set(inWin.map((f) => f.run)).size,
      isNew: failedLatest && idNum(s.firstSeen) >= firstLatest,
    });
  }
  rows.sort((a, b) => b.winRuns - a.winRuns || (a.s.unit < b.s.unit ? -1 : 1));
  out.push(`Failing units in the window: ${rows.length}. Columns: runs failed / runs in window, per OS (windows, macos, ubuntu). "Seen" is runs the ledger holds from the unit's start, a lower bound where no history covers the unit.`);
  out.push('');
  out.push('| unit | group | window | windows | macos | ubuntu | first seen | total fail / seen | latest | fixed by |');
  out.push('|---|---|---|---|---|---|---|---|---|---|');
  for (const r of rows) {
    const osNew = OSES.filter((os) => r.s.fails.some((f) => f.os === os && latestIds.has(f.run)) && !r.s.fails.some((f) => f.os === os && idNum(f.run) < firstLatest));
    const tag = r.failedLatest ? (r.isNew ? 'NEW' : `known${osNew.length ? `, new on ${osNew.join('+')}` : ''}`) : '';
    out.push(`| ${r.s.unit.replace(/\|/g, '\\|')} | ${r.s.group} | ${r.winRuns}/${n} | ${r.per.join(' | ')} | ${r.s.firstSeen} | ${r.s.totalFailRuns}/${r.s.totalRuns} | ${tag} | ${fixedBy(fixes, r.s.unit).replace(/\|/g, '\\|')} |`);
  }
  out.push('');
  out.push(`Latest ${latestRuns.length} runs (${latestRuns.map((r) => r.id).join(', ')}): a unit is NEW when it never failed in a run before the first of these, known otherwise.`);
  out.push('');
  out.push('| run | sha | failure rows | new | known |');
  out.push('|---|---|---|---|---|');
  let tn = 0;
  let tk = 0;
  const seenInBatch = new Set();
  for (const r of latestRuns) {
    const fs_ = ledger.failures.filter((f) => f.run === r.id);
    let nn = 0;
    let kk = 0;
    for (const f of fs_) {
      const st = stats.get(key(f));
      if (idNum(st.firstSeen) >= firstLatest) nn++; else kk++;
    }
    tn += nn;
    tk += kk;
    out.push(`| ${r.id} | ${r.sha || '?'} | ${fs_.length} | ${nn} | ${kk} |`);
    fs_.forEach((f) => seenInBatch.add(key(f)));
  }
  out.push('');
  out.push(`Failure rows in the latest runs: ${tn + tk}, new ${tn}, known ${tk}.`);
  const newU = [];
  const knownU = [];
  for (const k of seenInBatch) (idNum(stats.get(k).firstSeen) >= firstLatest ? newU : knownU).push(stats.get(k).unit);
  out.push(`Distinct units: new ${newU.length} (${newU.join(', ') || '-'}), known ${knownU.length} (${knownU.join(', ') || '-'}).`);
  out.push('');
  const text = out.join('\n') + '\n';
  fs.mkdirSync(DATA, { recursive: true });
  fs.writeFileSync(file('report.md'), text);
  process.stdout.write(text);
}

const [cmd, ...rest] = process.argv.slice(2);
const cmds = { seed, ingest: () => ingest(rest), 'list-missing': listMissing, 'seed-fixes': seedFixes, report: () => report(rest) };
if (!cmds[cmd]) {
  console.error('usage: ci-flake-ledger.js seed | ingest [--conclusion success] <run-id>... | list-missing | seed-fixes | report [--window 10|20] [--latest K]');
  process.exit(2);
}
cmds[cmd]();
