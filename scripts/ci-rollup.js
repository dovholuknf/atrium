#!/usr/bin/env node
'use strict';
// A run-level rollup of every failing unit across the matrix, for the run's summary page.
//   ci-rollup.js --local RUN_DIR [--history DIR] [--run ID] [--out FILE]
//   ci-rollup.js --repo OWNER/NAME --run ID [--token T] [--branch main] [--out FILE]
// RUN_DIR and each DIR/<run-id> hold one folder per job, named ci-<os>-latest-<group>-attempt-<n>, as ci.sh left them.
// A history run with no job folders is unknown, never green. Best effort: any failure prints a line and exits 0.
const fs = require('fs');
const os = require('os');
const path = require('path');
const { execFileSync } = require('child_process');
const { jobFailures, retryPassed } = require('./ci-failures');

const MAX_ROWS = 30;
const WINDOW = 10;
const JOB_DIR = /^ci-(\w+)-latest-(.+)-attempt-(\d+)$/;

const unitKey = (f) => `${f.group}|${f.unit}`;
const idCmp = (a, b) => (BigInt(a) < BigInt(b) ? -1 : BigInt(a) > BigInt(b) ? 1 : 0);

function jobDirs(dir) {
  if (!fs.existsSync(dir)) return [];
  const best = new Map();
  for (const d of fs.readdirSync(dir)) {
    const m = JOB_DIR.exec(d);
    if (!m || !fs.statSync(path.join(dir, d)).isDirectory()) continue;
    const k = `${m[1]}/${m[2]}`;
    if (!best.has(k) || +m[3] > best.get(k).attempt) best.set(k, { os: m[1], job: m[2], attempt: +m[3], dir: path.join(dir, d) });
  }
  return [...best.values()];
}

function readRun(dir) {
  const jobs = jobDirs(dir);
  const failures = [];
  for (const j of jobs) {
    const retried = retryPassed(j.dir);
    for (const f of jobFailures(j.dir)) failures.push({ ...f, job: j.job, retried: retried.has(f.unit) });
  }
  return { jobs, failures };
}

function ago(rows, history) {
  const known = history.filter((h) => h.known);
  const n = history.length;
  return rows.map((r) => {
    const failed = known.filter((h) => h.keys.has(unitKey(r))).length;
    let text;
    if (!known.length) text = 'history unavailable';
    else if (!failed) text = 'NEW';
    else text = `failed in ${failed} of the last ${n}`;
    if (known.length && known.length < n) text += ` (${n - known.length} unknown)`;
    return { ...r, failed, history: text, isNew: known.length > 0 && !failed };
  });
}

function retryText(r) {
  if (r.retried) return 'passed on retry, fails the job';
  if (r.group === 'board' && !r.unit.startsWith('check:') && !r.unit.startsWith('step:')) return 'failed on retry';
  return 'no retry';
}

function render({ run, jobs, rows, missing, history, note }) {
  const out = [];
  const label = run ? `ci run ${run}` : 'ci run';
  if (!rows.length && !missing.length) {
    out.push(`## ${label}: green`, '', `All ${jobs.length} jobs passed.`, '');
    return out.join('\n');
  }
  out.push(`## ${label}: ${rows.length} failing unit${rows.length === 1 ? '' : 's'}`, '');
  const known = history.filter((h) => h.known).length;
  out.push(`History is the last ${history.length} completed runs of ci on main, ${known} with artifacts to read.`, '');
  if (rows.length) {
    out.push('| os | job | unit | first error | retry | history |', '|---|---|---|---|---|---|');
    const cell = (s) => String(s).replace(/\|/g, '\\|').replace(/\s+/g, ' ').slice(0, 160);
    for (const r of rows.slice(0, MAX_ROWS)) {
      out.push(`| ${r.os} | ${r.job} | ${cell(r.unit)} | ${cell(r.error)} | ${retryText(r)} | ${r.history} |`);
    }
    if (rows.length > MAX_ROWS) out.push('', `and ${rows.length - MAX_ROWS} more`);
    out.push('');
  }
  for (const m of missing) out.push(`- ${m}`);
  if (missing.length) out.push('');
  if (note) out.push(note, '');
  return out.join('\n');
}

function build({ run, current, histRuns, jobsWithoutArtifacts = [], note }) {
  const history = histRuns.map((h) => ({ id: h.id, known: h.known, keys: new Set(h.failures.map(unitKey)) }));
  const seen = new Set();
  const rows = [];
  for (const f of current.failures) {
    const k = `${f.os}|${f.job}|${f.unit}`;
    if (seen.has(k)) continue;
    seen.add(k);
    rows.push(f);
  }
  const withHistory = ago(rows, history);
  withHistory.sort((a, b) => (b.isNew - a.isNew) || (b.failed - a.failed) || (a.os + a.job + a.unit < b.os + b.job + b.unit ? -1 : 1));
  const missing = jobsWithoutArtifacts.map((j) => `${j}: failed and left no artifact, so its units are not listed`);
  return render({ run, jobs: current.jobs, rows: withHistory, missing, history, note });
}

function local(opts) {
  const current = readRun(opts.local);
  const histRuns = [];
  if (opts.history && fs.existsSync(opts.history)) {
    const ids = fs.readdirSync(opts.history).filter((d) => /^\d+$/.test(d) && d !== opts.run).sort(idCmp).slice(-WINDOW);
    for (const id of ids) {
      const r = readRun(path.join(opts.history, id));
      histRuns.push({ id, known: r.jobs.length > 0, failures: r.failures });
    }
  }
  return build({ run: opts.run, current, histRuns });
}

async function remote(opts) {
  const token = opts.token || process.env.GITHUB_TOKEN || process.env.GH_TOKEN;
  if (!token) throw new Error('no token');
  const api = async (p) => {
    const res = await fetch(`https://api.github.com/repos/${opts.repo}/${p}`, {
      headers: { Authorization: `Bearer ${token}`, Accept: 'application/vnd.github+json', 'X-GitHub-Api-Version': '2022-11-28' },
    });
    if (!res.ok) throw new Error(`${p}: HTTP ${res.status}`);
    return res;
  };
  const tmp = fs.mkdtempSync(path.join(os.tmpdir(), 'ci-rollup-'));
  const deadline = Date.now() + (opts.budgetMs || 240000);

  const fetchRun = async (id, runDir) => {
    const list = (await (await api(`actions/runs/${id}/artifacts?per_page=100`)).json()).artifacts || [];
    const mine = list.filter((a) => JOB_DIR.test(a.name) && !a.expired);
    let next = 0;
    const worker = async () => {
      while (next < mine.length && Date.now() < deadline) {
        const a = mine[next++];
        try {
          const res = await api(`actions/artifacts/${a.id}/zip`);
          const zip = path.join(tmp, `${a.id}.zip`);
          fs.writeFileSync(zip, Buffer.from(await res.arrayBuffer()));
          const to = path.join(runDir, a.name);
          fs.mkdirSync(to, { recursive: true });
          unzip(zip, to);
        } catch (e) {
          console.error(`ci-rollup: ${a.name} of run ${id}: ${e.message}`);
        }
      }
    };
    await Promise.all([worker(), worker(), worker(), worker()]);
    return mine.length;
  };

  const curDir = path.join(tmp, opts.run);
  fs.mkdirSync(curDir, { recursive: true });
  await fetchRun(opts.run, curDir);
  const current = readRun(curDir);

  let jobsWithoutArtifacts = [];
  try {
    const jobs = (await (await api(`actions/runs/${opts.run}/jobs?per_page=100&filter=latest`)).json()).jobs || [];
    const have = new Set(current.jobs.map((j) => `${j.os}-latest, ${j.job}`));
    jobsWithoutArtifacts = jobs
      .filter((j) => j.conclusion === 'failure' || j.conclusion === 'timed_out')
      .map((j) => ({ name: j.name, key: (/^check \((.+)\)$/.exec(j.name) || [])[1] }))
      .filter((j) => j.key && !have.has(j.key))
      .map((j) => j.name);
  } catch (e) {
    console.error(`ci-rollup: jobs: ${e.message}`);
  }

  const histRuns = [];
  try {
    const wf = (await (await api(`actions/workflows/ci.yml/runs?branch=${opts.branch}&status=completed&per_page=${WINDOW + 5}`)).json()).workflow_runs || [];
    const prior = wf.filter((r) => String(r.id) !== opts.run && r.event === 'push').slice(0, WINDOW);
    for (const r of prior) {
      if (r.conclusion === 'success') { histRuns.push({ id: String(r.id), known: true, failures: [] }); continue; }
      if (Date.now() >= deadline) { histRuns.push({ id: String(r.id), known: false, failures: [] }); continue; }
      const dir = path.join(tmp, String(r.id));
      fs.mkdirSync(dir, { recursive: true });
      try { await fetchRun(r.id, dir); } catch (e) { console.error(`ci-rollup: run ${r.id}: ${e.message}`); }
      const read = readRun(dir);
      histRuns.push({ id: String(r.id), known: read.jobs.length > 0, failures: read.failures });
    }
  } catch (e) {
    console.error(`ci-rollup: history: ${e.message}`);
  }
  fs.rmSync(tmp, { recursive: true, force: true });
  return build({ run: opts.run, current, histRuns, jobsWithoutArtifacts });
}

function unzip(zip, to) {
  try {
    execFileSync('unzip', ['-q', '-o', zip, '-d', to], { stdio: 'ignore' });
  } catch {
    execFileSync('tar', ['-xf', zip, '-C', to], { stdio: 'ignore' });
  }
}

function parse(argv) {
  const o = { branch: 'main' };
  for (let i = 0; i < argv.length; i++) {
    const a = argv[i];
    if (!a.startsWith('--')) throw new Error(`unexpected ${a}`);
    o[a.slice(2)] = argv[++i];
  }
  return o;
}

module.exports = { build, readRun, local };

if (require.main === module) {
  (async () => {
    try {
      const opts = parse(process.argv.slice(2));
      const text = opts.local ? local(opts) : await remote(opts);
      if (opts.out) fs.writeFileSync(opts.out, text);
      else process.stdout.write(text);
    } catch (e) {
      console.error(`ci-rollup: ${e.message}`);
      process.stdout.write(`## ci rollup unavailable\n\n${String(e.message).replace(/\s+/g, ' ')}\n`);
    }
  })();
}
