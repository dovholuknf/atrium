#!/usr/bin/env node
'use strict';
// Once per finished main CI run: record the run's failed jobs, then make scripts/flaky-suite.json the tests that failed in
// the 15 newest runs of the store, nothing else, and log every move in scripts/flaky-history.md.
//   node scripts/flaky-sync.js <run id> [--dry-run]
// The run's failures come from its failing-tests artifact (ci-rollup.js writes it), else from the job list with each failed
// job's artifact folder under CI_ARTIFACTS, else from the job log. Rows of a run id up to `since` in the JSON are ignored. scripts/flaky-jobs.jsonl holds one line per failed job
// (a run with none gets one line of its own, so green runs age the window). The JSON is edited by text, never rewritten.
const fs = require('fs');
const os = require('os');
const path = require('path');
const { execFileSync } = require('child_process');
const { parseJobName } = require('./ci-jobname');

const REPO = 'dovholuknf/atrium';
const WINDOW = 15;
const NO_TEST = '(step failed, no test named in the log)';
const GREEN = '(no failed job)';
const ROOT = path.join(__dirname, '..');
const byName = (a, b) => a.localeCompare(b);
const kindOf = (name) => (/^Test\w*$/.test(name) ? 'go' : 'board');
const isCheck = (name) => name.startsWith('check: ');

function readStore(file) {
  if (!fs.existsSync(file)) return [];
  return fs.readFileSync(file, 'utf8').split(/\r?\n/).filter(Boolean).map((l) => JSON.parse(l));
}

function windowRuns(rows, n = WINDOW) {
  const ids = [...new Set(rows.map((r) => String(r.run)))];
  ids.sort((a, b) => (BigInt(a) < BigInt(b) ? 1 : BigInt(a) > BigInt(b) ? -1 : 0));
  return ids.slice(0, n);
}

function windowTests(rows) {
  const keep = new Set(windowRuns(rows));
  const seen = new Map();
  const ordered = rows.filter((r) => keep.has(String(r.run))).sort((a, b) => (BigInt(a.run) < BigInt(b.run) ? 1 : -1));
  for (const r of ordered) {
    for (const t of [...r.tests, ...(r.retried || [])]) if (t !== NO_TEST && !seen.has(t)) seen.set(t, r);
  }
  return seen;
}

function runsSince(rows, test) {
  const ids = windowRuns(rows, Infinity);
  const at = ids.findIndex((id) => rows.some((r) => String(r.run) === id && [...r.tests, ...(r.retried || [])].includes(test)));
  return at < 0 ? `>${ids.length}` : String(at);
}

function plan(rows, entries) {
  const inWindow = windowTests(rows);
  const listed = new Set(entries.map((e) => e.name));
  const add = [];
  const checks = [];
  for (const [name, row] of inWindow) {
    if (listed.has(name)) continue;
    if (isCheck(name)) { if (!entries.some((e) => e.kind === 'script' && `check: ${e.name}` === name)) checks.push(name); continue; }
    add.push({ name, kind: kindOf(name), row });
  }
  add.sort((a, b) => byName(a.name, b.name));
  const remove = entries.filter((e) => !inWindow.has(e.name) && !(e.kind === 'script' && inWindow.has(`check: ${e.name}`)));
  const noTest = rows.filter((r) => windowRuns(rows).includes(String(r.run)) && r.tests.includes(NO_TEST));
  return { add, remove, checks: checks.sort(byName), unchanged: entries.length - remove.length, noTest };
}

const objLine = (o) => '{' + Object.entries(o).map(([k, v]) => `${JSON.stringify(k)}: ${JSON.stringify(v)}`).join(', ') + '}';

function entryAt(line) {
  try { return JSON.parse(line.trim().replace(/,$/, '')); } catch { return null; }
}

function editSuite(text, adds, removes) {
  const eol = text.includes('\r\n') ? '\r\n' : '\n';
  let lines = text.split(/\r?\n/);
  const gone = new Set(removes.map((e) => e.kind + '\0' + (e.package || '') + '\0' + e.name));
  lines = lines.filter((l) => {
    const e = entryAt(l);
    return !(e && e.kind && gone.has(e.kind + '\0' + (e.package || '') + '\0' + e.name));
  });
  const close = lines.findIndex((l) => /^\s*\]\s*$/.test(l));
  if (close < 0) throw new Error('flaky-suite.json has no closing ] for entries');
  const at = adds.map((a) => '  ' + objLine(a.entry));
  lines.splice(close, 0, ...at);
  const first = lines.findIndex((l) => /"entries":\s*\[/.test(l));
  const last = lines.findIndex((l, i) => i > first && /^\s*\]\s*$/.test(l));
  const idx = [];
  for (let i = first + 1; i < last; i++) if (entryAt(lines[i])) idx.push(i);
  idx.forEach((i, n) => { lines[i] = lines[i].replace(/,\s*$/, '') + (n < idx.length - 1 ? ',' : ''); });
  return lines.join(eol);
}

const HISTORY_HEAD = [
  '# Flaky suite history',
  '',
  'Every move of `scripts/flaky-suite.json`, appended by `node scripts/flaky-sync.js`. SEED is an entry already listed when this log began.',
  '',
  '| date | run | test | action | listed before | runs since last failure |',
  '|---|---|---|---|---|---|',
  '',
].join('\n');

function historyRow(r) {
  return `| ${r.date} | ${r.run} | ${r.test} | ${r.action} | ${r.before} | ${r.since} |`;
}

function ensureHistory(file) {
  if (!fs.existsSync(file)) fs.writeFileSync(file, HISTORY_HEAD);
}

function listedBefore(historyText, test, gitSeen) {
  return historyText.includes(`| ${test} |`) || gitSeen(test) ? 'yes' : 'no';
}

function logFailures(text) {
  const tests = new Set();
  const errors = {};
  const lines = text.split(/\r?\n/).map((raw) => raw.replace(/^\d{4}-\d\d-\d\dT[\d:.]+Z /, ''));
  lines.forEach((l, i) => {
    let m = /^--- FAIL: (Test[^\s/]*)/.exec(l);
    if (m) {
      tests.add(m[1]);
      const next = lines.slice(i + 1, i + 6).find((x) => /_test\.go:\d+:/.test(x));
      if (next && !errors[m[1]]) errors[m[1]] = next.trim().slice(0, 200);
    }
    m = /^(check-[\w-]+)(?:\.js|\.go|\.sh)?:.*(problem|fail|FAIL|error)/.exec(l);
    if (m) tests.add('check: ' + m[1]);
    if (/a native title tooltip/.test(l)) tests.add('check: titles');
  });
  return { tests: [...tests], errors };
}

function defaultDeps() {
  const gh = (args) => execFileSync('gh', args, { encoding: 'utf8', maxBuffer: 512 * 1024 * 1024 });
  const artifacts = process.env.CI_ARTIFACTS || 'C:/Users/claude/.atrium2/hub/ci-artifacts/github.com/dovholuknf/atrium';
  return {
    gh,
    artifacts,
    download(run) {
      const tmp = fs.mkdtempSync(path.join(os.tmpdir(), 'flaky-sync-'));
      try {
        gh(['run', 'download', String(run), '-R', REPO, '-n', 'failing-tests', '-D', tmp]);
        return JSON.parse(fs.readFileSync(path.join(tmp, 'failing-tests.json'), 'utf8'));
      } catch { return null; } finally { fs.rmSync(tmp, { recursive: true, force: true }); }
    },
    findPkg(name) {
      try {
        const out = execFileSync('git', ['grep', '-l', `func ${name}(`, '--', '*_test.go'], { cwd: ROOT, encoding: 'utf8' });
        const f = out.split(/\r?\n/).filter(Boolean)[0];
        return f ? path.posix.dirname(f) : null;
      } catch { return null; }
    },
    gitSeen(name) {
      try { return execFileSync('git', ['log', '-S"name": "' + name + '"', '--oneline', '--', 'scripts/flaky-suite.json'], { cwd: ROOT, encoding: 'utf8' }).trim() !== ''; } catch { return false; }
    },
    now: () => new Date().toISOString().slice(0, 10),
    log: (s) => console.log(s),
  };
}

function fallbackTests(deps, job) {
  const m = parseJobName(job.name);
  const attempt = job.run_attempt || 1;
  const run = job.run;
  if (m && deps.artifacts) {
    const dir = path.join(deps.artifacts, String(run), `ci-${m.os}-latest-${m.group}-attempt-${attempt}`);
    if (fs.existsSync(dir)) {
      const { jobFailures, retriedRows } = require('./ci-failures');
      const fails = jobFailures(dir).filter((f) => !f.unit.startsWith('step: ') && !f.unit.startsWith('('));
      const errors = {};
      for (const f of fails) if (f.error && !errors[f.unit]) errors[f.unit] = f.error.slice(0, 200);
      let tests = [...new Set(fails.map((f) => f.unit))];
      if (!tests.length) for (const f of fs.readdirSync(dir)) if (/\.log$/.test(f)) tests.push(...logFailures(fs.readFileSync(path.join(dir, f), 'utf8')).tests);
      tests = [...new Set(tests)];
      return { tests, errors, retried: [...new Set(retriedRows(dir).map((f) => f.unit))] };
    }
  }
  try {
    const l = logFailures(deps.gh(['api', `repos/${REPO}/actions/jobs/${job.id}/logs`]));
    return { tests: l.tests, errors: l.errors, retried: [] };
  } catch { return { tests: [], errors: {}, retried: [] }; }
}

function collect(runId, deps) {
  let doc = deps.download(runId);
  const stub = doc && !(doc.jobs || []).length ? doc : null;
  if (stub || !doc) {
    const text = deps.gh(['api', '--paginate', `repos/${REPO}/actions/runs/${runId}/jobs?per_page=100&filter=latest`, '--jq', '.jobs[] | {id,name,conclusion,started_at,head_sha,run_attempt}']);
    const jobs = text.split(/\r?\n/).filter(Boolean).map((l) => JSON.parse(l));
    doc = {
      run: String(runId),
      date: jobs.map((j) => j.started_at).filter(Boolean).sort()[0] || (stub && stub.date) || '',
      sha: String((jobs[0] || {}).head_sha || '').slice(0, 9) || (stub && stub.sha) || '',
      jobs: jobs.filter((j) => j.conclusion === 'failure' || j.conclusion === 'timed_out').map((j) => ({ id: j.id, name: j.name, run_attempt: j.run_attempt, tests: [], retried: [], errors: {} })),
    };
  }
  const out = [];
  for (const j of doc.jobs) {
    let { tests = [], retried = [], errors = {} } = j;
    if (!tests.length) {
      const f = deps.fallback ? deps.fallback({ ...j, run: runId }) : fallbackTests(deps, { ...j, run: runId });
      tests = f.tests;
      errors = { ...errors, ...f.errors };
      retried = [...new Set([...retried, ...(f.retried || [])])];
    }
    out.push({
      run: String(runId), date: doc.date, sha: doc.sha, job: j.id, name: j.name,
      tests: tests.length ? [...tests].sort(byName) : [NO_TEST], retried: [...retried].sort(byName),
      ...(Object.keys(errors).length ? { errors } : {}),
    });
  }
  if (!out.length) out.push({ run: String(runId), date: doc.date, sha: doc.sha, job: 0, name: GREEN, tests: [], retried: [] });
  return out;
}

function sync(runId, opts = {}) {
  const deps = { ...defaultDeps(), ...(opts.deps || {}) };
  const storeFile = opts.store || path.join(__dirname, 'flaky-jobs.jsonl');
  const suiteFile = opts.suite || path.join(__dirname, 'flaky-suite.json');
  const historyFile = opts.history || path.join(__dirname, 'flaky-history.md');
  const dry = !!opts.dryRun;
  let rows = readStore(storeFile);
  const had = rows.some((r) => String(r.run) === String(runId));
  let fresh = [];
  if (!had) {
    fresh = collect(runId, deps);
    rows = rows.concat(fresh);
    if (!dry) fs.appendFileSync(storeFile, fresh.map((r) => JSON.stringify(r)).join('\n') + '\n');
  }
  const suiteText = fs.readFileSync(suiteFile, 'utf8');
  const parsed = JSON.parse(suiteText);
  const entries = parsed.entries;
  if (parsed.since) rows = rows.filter((r) => BigInt(r.run) > BigInt(parsed.since));
  const p = plan(rows, entries);
  const historyText = fs.existsSync(historyFile) ? fs.readFileSync(historyFile, 'utf8') : HISTORY_HEAD;
  const date = (rows.find((r) => String(r.run) === String(runId)) || {}).date || '';
  const day = (date || deps.now()).slice(0, 10);
  const skipped = [];
  const adds = [];
  for (const a of p.add) {
    const pkg = a.kind === 'go' ? deps.findPkg(a.name) : null;
    if (a.kind === 'go' && !pkg) { skipped.push(`${a.name} (no package found)`); continue; }
    const err = a.row.errors && a.row.errors[a.name];
    const reason = `failed in run ${a.row.run} job ${a.row.name}${err ? ': ' + err : ''}`;
    const entry = a.kind === 'go' ? { kind: 'go', package: pkg, name: a.name, reason } : { kind: 'board', name: a.name, reason };
    adds.push({ ...a, entry });
  }
  const moves = [
    ...adds.map((a) => ({ test: a.name, action: 'ADD' })),
    ...p.remove.map((e) => ({ test: e.name, action: 'REMOVE' })),
  ].map((m) => ({ date: day, run: runId, ...m, before: listedBefore(historyText, m.test, deps.gitSeen), since: runsSince(rows, m.test) }));
  if (!dry && (adds.length || p.remove.length)) {
    fs.writeFileSync(suiteFile, editSuite(suiteText, adds, p.remove));
    ensureHistory(historyFile);
    fs.appendFileSync(historyFile, moves.map(historyRow).join('\n') + '\n');
  }
  const noTestJobs = fresh.filter((r) => r.tests.includes(NO_TEST));
  const lines = [
    `flaky-sync run ${runId}${dry ? ' (dry run, nothing written)' : ''}: ${had ? 'already in the store' : `${fresh.filter((r) => r.job).length} failed job(s) recorded`}`,
    `window: ${windowRuns(rows).length} runs, newest ${windowRuns(rows)[0]}, oldest ${windowRuns(rows).slice(-1)[0]}`,
    `added ${adds.length}${adds.length ? ': ' + adds.map((a) => a.name).join(', ') : ''}`,
    `removed ${p.remove.length}${p.remove.length ? ': ' + p.remove.map((e) => e.name).join(', ') : ''}`,
    `unchanged ${p.unchanged}`,
    `check scripts seen, never added: ${p.checks.length ? p.checks.join(', ') : 'none'}`,
    `jobs that named no test in this run: ${noTestJobs.length ? noTestJobs.map((r) => r.name).join('; ') : 'none'}`,
  ];
  if (skipped.length) lines.push(`skipped: ${skipped.join(', ')}`);
  deps.log(lines.join('\n'));
  return { added: adds, removed: p.remove, unchanged: p.unchanged, checks: p.checks, skipped, moves, fresh };
}

module.exports = { sync, plan, windowRuns, windowTests, runsSince, editSuite, collect, logFailures, readStore, NO_TEST, GREEN, HISTORY_HEAD, historyRow, objLine };

if (require.main === module) {
  const args = process.argv.slice(2);
  const run = args.find((a) => /^\d+$/.test(a));
  if (!run) { console.error('usage: node scripts/flaky-sync.js <run id> [--dry-run]'); process.exit(2); }
  try { sync(run, { dryRun: args.includes('--dry-run') }); } catch (e) { console.error('flaky-sync: ' + e.message); process.exit(1); }
}
