#!/usr/bin/env node
'use strict';
// A run-level rollup of every failing unit across the matrix, for the run's summary page.
//   ci-rollup.js --local RUN_DIR [--history DIR] [--run ID] [--out FILE] [--summary FILE]
//   ci-rollup.js --repo OWNER/NAME --run ID [--token T] [--branch main] [--out FILE] [--summary FILE] [--failing-json FILE]
// --failing-json writes the run's failed jobs and their failing tests, the artifact scripts/flaky-sync.js reads.
// The table goes to --out (written) or --summary (appended), else stdout. Workflow commands for the run page's
// Annotations block go to stdout.
// RUN_DIR and each DIR/<run-id> hold one folder per job, named ci-<os>-latest-<group>-attempt-<n>, as ci.sh left them.
// A history run with no job folders is unknown, never green. Best effort: any failure prints a line and exits 0.
const fs = require('fs');
const os = require('os');
const path = require('path');
const { execFileSync } = require('child_process');
const { jobFailures, retryPassed, retriedRows } = require('./ci-failures');

const MAX_ROWS = 30;
const MAX_ANNOTATIONS = 10;
const MAX_MESSAGE = 1000;
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
  const retried = [];
  for (const j of jobs) {
    const loaded = retryPassed(j.dir);
    for (const f of jobFailures(j.dir)) failures.push({ ...f, job: j.job, retried: loaded.has(f.unit) });
    for (const f of retriedRows(j.dir)) retried.push({ ...f, job: j.job });
  }
  return { jobs, failures, retried };
}

function ago(rows, history) {
  const known = history.filter((h) => h.known);
  const n = history.length;
  return rows.map((r) => {
    const failed = known.filter((h) => h.keys.has(unitKey(r)) || h.rkeys.has(unitKey(r))).length;
    let text;
    if (!known.length) text = 'history unavailable';
    else if (!failed) text = 'NEW';
    else text = `${r.times ? 'failed or passed on retry' : 'failed'} in ${failed} of the last ${n}`;
    if (known.length && known.length < n) text += ` (${n - known.length} unknown)`;
    return { ...r, failed, history: text, isNew: known.length > 0 && !failed };
  });
}

function retryText(r) {
  if (r.retried) return 'passed on retry, not on the flaky list, fails the job';
  if (r.group === 'board' && !r.unit.startsWith('check:') && !r.unit.startsWith('step:')) return 'failed on retry';
  return 'no retry';
}

const unitText = (r) => (r.passesFailed ? `${r.unit} (failed ${r.passesFailed} of ${r.of} passes)` : r.unit);

const escData = (s) => String(s).replace(/%/g, '%25').replace(/\r/g, '%0D').replace(/\n/g, '%0A');
const escProp = (s) => escData(s).replace(/:/g, '%3A').replace(/,/g, '%2C');
const clip = (s) => { s = String(s).replace(/\s+/g, ' '); return s.length > MAX_MESSAGE ? s.slice(0, MAX_MESSAGE - 3) + '...' : s; };

function annotate({ run, jobs, rows, retried, missing }) {
  const title = escProp(run ? `ci run ${run}` : 'ci run');
  const warn = (r) => `::warning title=${escProp(`PASSED ON RETRY: ${r.unit}`)}::${escData(clip(`${r.os} ${r.job}: not a clean pass, the job passed. ${r.times} | first try: ${r.error} | history: ${r.history}`))}`;
  if (!rows.length && !missing.length) {
    const green = retried.length
      ? `green, all ${jobs.length} jobs passed, ${retried.length} unit${retried.length === 1 ? '' : 's'} PASSED ON RETRY (not clean)`
      : `green, all ${jobs.length} jobs passed`;
    return [`::notice title=${title}::${escData(green)}`, ...retried.slice(0, MAX_ANNOTATIONS - 1).map(warn)];
  }
  const rank = (r) => (r.isNew ? 0 : r.retried ? 1 : 2);
  const ordered = rows.map((r, i) => ({ r, i })).sort((a, b) => rank(a.r) - rank(b.r) || a.i - b.i).map((x) => x.r);
  const lines = [
    ...ordered.map((r) => `::error title=${escProp(r.unit)}::${escData(clip(`${r.os} ${r.job}: ${r.passesFailed ? `failed ${r.passesFailed} of ${r.of} passes: ` : ''}${r.error} | retry: ${retryText(r)} | history: ${r.history}`))}`),
    ...retried.map(warn),
  ];
  const cut = lines.length > MAX_ANNOTATIONS - 1;
  const shown = cut ? lines.slice(0, MAX_ANNOTATIONS - 2) : lines;
  const out = shown.slice();
  if (cut) out.push(`::notice title=${title}::${escData(`and ${lines.length - shown.length} more, see the table`)}`);
  const n = ordered.filter((r) => r.isNew).length;
  const k = ordered.filter((r) => r.retried).length;
  const totals = `${rows.length} failing unit${rows.length === 1 ? '' : 's'}, ${n} new${k ? `, ${k} unlisted passed on retry` : ''}${retried.length ? `, ${retried.length} passed on retry (not clean)` : ''}`;
  out.push(`::notice title=${title}::${escData(missing.length ? `${totals}, ${missing.length} job${missing.length === 1 ? '' : 's'} left no artifact` : totals)}`);
  return out;
}

function retriedTable(retried) {
  const cell = (s) => String(s).replace(/\|/g, '\\|').replace(/\s+/g, ' ').slice(0, 160);
  const out = ['| os | job | unit | timings | first try error | history |', '|---|---|---|---|---|---|'];
  for (const r of retried.slice(0, MAX_ROWS)) out.push(`| ${r.os} | ${r.job} | ${cell(r.unit)} | ${cell(r.times)} | ${cell(r.error)} | ${r.history} |`);
  if (retried.length > MAX_ROWS) out.push('', `and ${retried.length - MAX_ROWS} more`);
  return out;
}

function render({ run, jobs, rows, retried, missing, history, note }) {
  const out = [];
  const label = run ? `ci run ${run}` : 'ci run';
  const retriedHead = `${retried.length} unit${retried.length === 1 ? '' : 's'} PASSED ON RETRY (not clean passes)`;
  if (!rows.length && !missing.length) {
    if (!retried.length) {
      out.push(`## ${label}: green`, '', `All ${jobs.length} jobs passed.`, '');
      return out.join('\n');
    }
    out.push(`## ${label}: green, but ${retriedHead}`, '', `All ${jobs.length} jobs passed. Each unit below failed in its shard and passed alone on the retry, so it is slow or stalls under load.`, '');
    out.push(...retriedTable(retried), '');
    return out.join('\n');
  }
  out.push(`## ${label}: ${rows.length} failing unit${rows.length === 1 ? '' : 's'}`, '');
  const known = history.filter((h) => h.known).length;
  out.push(`History is the last ${history.length} completed runs of ci on main, ${known} with artifacts to read.`, '');
  if (rows.length) {
    out.push('| os | job | unit | first error | retry | history |', '|---|---|---|---|---|---|');
    const cell = (s) => String(s).replace(/\|/g, '\\|').replace(/\s+/g, ' ').slice(0, 160);
    for (const r of rows.slice(0, MAX_ROWS)) {
      out.push(`| ${r.os} | ${r.job} | ${cell(unitText(r))} | ${cell(r.error)} | ${retryText(r)} | ${r.history} |`);
    }
    if (rows.length > MAX_ROWS) out.push('', `and ${rows.length - MAX_ROWS} more`);
    out.push('');
  }
  if (retried.length) out.push(`### ${retriedHead}`, '', ...retriedTable(retried), '');
  for (const m of missing) out.push(`- ${m}`);
  if (missing.length) out.push('');
  if (note) out.push(note, '');
  return out.join('\n');
}

const byName = (a, b) => a.localeCompare(b);
const jobKey = (name) => (/^check \((\w+)-latest, (.+)\)$/.exec(name) || []).slice(1).join('/');

function failingDoc({ run, runInfo = {}, apiJobs = [], current }) {
  const mine = (j) => (f) => `${f.os}/${f.job}` === jobKey(j.name);
  const failed = apiJobs.filter((j) => j.conclusion === 'failure' || j.conclusion === 'timed_out');
  const listed = new Set(failed.map((j) => jobKey(j.name)));
  for (const f of current.failures) {
    const k = `${f.os}/${f.job}`;
    if (listed.has(k) || f.unit.startsWith('step: ') || f.unit.startsWith('(')) continue;
    listed.add(k);
    failed.push({ id: 0, name: `check (${f.os}-latest, ${f.job})` });
  }
  const jobs = failed
    .map((j) => {
      const fails = current.failures.filter(mine(j)).filter((f) => !f.unit.startsWith('step: ') && !f.unit.startsWith('('));
      const errors = {};
      for (const f of fails) if (f.error && !errors[f.unit]) errors[f.unit] = f.error.slice(0, 200);
      const passes = {};
      for (const f of fails) if (f.of) (passes[f.unit] = passes[f.unit] || new Set()).add(f.pass);
      for (const u of Object.keys(passes)) passes[u] = { failed: passes[u].size, of: fails.find((f) => f.unit === u).of };
      const retried = [...new Set(current.retried.filter(mine(j)).map((f) => f.unit))].sort(byName);
      return { id: j.id, name: j.name, tests: [...new Set(fails.map((f) => f.unit))].sort(byName), retried, errors, ...(Object.keys(passes).length ? { passes } : {}) };
    });
  return { run: String(run), date: runInfo.created_at || '', sha: String(runInfo.head_sha || '').slice(0, 9), jobs };
}

function build({ run, current, histRuns, jobsWithoutArtifacts = [], note }) {
  const history = histRuns.map((h) => ({ id: h.id, known: h.known, keys: new Set(h.failures.map(unitKey)), rkeys: new Set((h.retried || []).map(unitKey)) }));
  const passesFailed = new Map();
  for (const f of current.failures) {
    if (!f.of) continue;
    const k = `${f.os}|${f.job}|${f.unit}`;
    if (!passesFailed.has(k)) passesFailed.set(k, new Set());
    passesFailed.get(k).add(f.pass);
  }
  const dedupe = (list) => {
    const seen = new Set();
    return list.filter((f) => {
      const k = `${f.os}|${f.job}|${f.unit}`;
      if (seen.has(k)) return false;
      seen.add(k);
      if (passesFailed.has(k)) f.passesFailed = passesFailed.get(k).size;
      return true;
    });
  };
  const withHistory = ago(dedupe(current.failures), history);
  withHistory.sort((a, b) => (b.isNew - a.isNew) || (b.failed - a.failed) || (a.os + a.job + a.unit < b.os + b.job + b.unit ? -1 : 1));
  const retried = ago(dedupe(current.retried || []), history).sort((a, b) => (b.failed - a.failed) || (a.os + a.job + a.unit < b.os + b.job + b.unit ? -1 : 1));
  const missing = jobsWithoutArtifacts.map((j) => `${j}: failed and left no artifact, so its units are not listed`);
  const ctx = { run, jobs: current.jobs, rows: withHistory, retried, missing, history, note };
  return { summary: render(ctx), commands: annotate(ctx), current };
}

function local(opts) {
  const current = readRun(opts.local);
  const histRuns = [];
  if (opts.history && fs.existsSync(opts.history)) {
    const ids = fs.readdirSync(opts.history).filter((d) => /^\d+$/.test(d) && d !== opts.run).sort(idCmp).slice(-WINDOW);
    for (const id of ids) {
      const r = readRun(path.join(opts.history, id));
      histRuns.push({ id, known: r.jobs.length > 0, failures: r.failures, retried: r.retried });
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
  let apiJobs = [];
  let runInfo = {};
  try {
    runInfo = await (await api(`actions/runs/${opts.run}`)).json();
  } catch (e) {
    console.error(`ci-rollup: run: ${e.message}`);
  }
  try {
    const jobs = (await (await api(`actions/runs/${opts.run}/jobs?per_page=100&filter=latest`)).json()).jobs || [];
    apiJobs = jobs;
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
      histRuns.push({ id: String(r.id), known: read.jobs.length > 0, failures: read.failures, retried: read.retried });
    }
  } catch (e) {
    console.error(`ci-rollup: history: ${e.message}`);
  }
  fs.rmSync(tmp, { recursive: true, force: true });
  const built = build({ run: opts.run, current, histRuns, jobsWithoutArtifacts });
  built.failing = failingDoc({ run: opts.run, runInfo, apiJobs, current });
  return built;
}

function unzip(zip, to) {
  try {
    execFileSync('unzip', ['-q', '-o', zip, '-d', to], { stdio: 'ignore' });
  } catch {
    execFileSync('tar', ['-xf', zip, '-C', to], { stdio: 'ignore' });
  }
}

function emit(opts, text) {
  if (opts.out) fs.writeFileSync(opts.out, text);
  else if (opts.summary) fs.appendFileSync(opts.summary, text);
  else process.stdout.write(text);
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

module.exports = { build, readRun, local, failingDoc, escData, escProp };

if (require.main === module) {
  (async () => {
    try {
      const opts = parse(process.argv.slice(2));
      const { summary, commands, failing } = opts.local ? local(opts) : await remote(opts);
      emit(opts, summary);
      if (opts['failing-json'] && failing) fs.writeFileSync(opts['failing-json'], JSON.stringify(failing, null, 1) + '\n');
      process.stdout.write(commands.join('\n') + '\n');
    } catch (e) {
      console.error(`ci-rollup: ${e.message}`);
      const msg = String(e.message).replace(/\s+/g, ' ');
      const text = `## ci rollup unavailable\n\n${msg}\n`;
      try { emit(parse(process.argv.slice(2)), text); } catch { process.stdout.write(text); }
      process.stdout.write(`::warning title=ci rollup unavailable::${escData(msg)}\n`);
    }
  })();
}
