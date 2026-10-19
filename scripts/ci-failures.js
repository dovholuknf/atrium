'use strict';
const fs = require('fs');
const path = require('path');

const MODULE = 'github.com/dovholuknf/atrium/';

function parseTsv(p) {
  return fs.readFileSync(p, 'utf8').split(/\r?\n/).filter(Boolean).map((l) => l.split('\t'));
}

function jobFailures(dir) {
  const m = /^ci-(\w+)-latest-(.+)-attempt-\d+$/.exec(path.basename(dir));
  if (!m) return [];
  const os = m[1];
  const job = m[2];
  const out = [];
  const ft = path.join(dir, 'failing.tsv');
  const rows = fs.existsSync(ft) ? parseTsv(ft) : [];
  for (const r of rows) {
    const [grp, a, b = ''] = r;
    if (grp === 'go-test' || grp === 'go-race') {
      const mm = /^(\S+)\s+(Test\S*)$/.exec(a);
      const pkg = (mm ? mm[1] : a.split(/\s+/)[0]).replace(MODULE, '');
      out.push({ os, group: pkg, unit: mm ? mm[2] : '(package)', error: b.trim() });
    } else if (a === '(check)') {
      const cm = /^([^:]+):\s*(.*)$/.exec(b);
      out.push({ os, group: grp, unit: `check: ${cm ? cm[1] : b}`, error: (cm ? cm[2] : b).trim() });
    } else out.push({ os, group: grp, unit: a, error: b.replace(/^\S+:\s*/, '').trim() });
  }
  const st = path.join(dir, 'steps.tsv');
  if (!rows.length && fs.existsSync(st)) {
    for (const r of parseTsv(st)) if (r[2] === 'FAILED') out.push({ os, group: 'step', unit: `step: ${r[0]}`, error: `${job} step ${r[0]} FAILED` });
  }
  return out;
}

function retryPassed(dir) {
  const log = path.join(dir, 'board.log');
  if (!fs.existsSync(log)) return new Set();
  const lines = fs.readFileSync(log, 'utf8').split(/\r?\n/);
  const out = new Set();
  const at = lines.findIndex((l) => l.startsWith('FAILED IN THE SHARDS, PASSED ALONE'));
  if (at < 0) return out;
  for (const l of lines.slice(at + 1)) {
    const m = /^ {2}(\S.*)$/.exec(l);
    if (m) out.add(m[1].trim());
    else if (!/^ {4}/.test(l)) break;
  }
  return out;
}

module.exports = { parseTsv, jobFailures, retryPassed };
