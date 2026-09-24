// The starter gate script in docs/hooks.md, extracted and driven against a mock of atrium's agent port, so the
// script the docs hand people is one that has been run. Needs pwsh. Usage: node scripts/test-gate-hook.js docs/hooks.md
const fs = require('fs');
const http = require('http');
const {spawn} = require('child_process');
const os = require('os');
const path = require('path');

const ps1 = path.join(fs.mkdtempSync(path.join(os.tmpdir(), 'atrium-gate-')), 'atrium-gate.ps1');

const md = fs.readFileSync(process.argv[2], 'utf8');
const script = md.split('```powershell\n# atrium-gate.ps1')[1].split('```')[0];
fs.writeFileSync(ps1, '# atrium-gate.ps1' + script);

let reply = {};
let gate = true;
const srv = http.createServer((req, res) => {
  let body = '';
  req.on('data', (d) => (body += d));
  req.on('end', () => {
    res.setHeader('Content-Type', 'application/json');
    if (req.url.startsWith('/gate')) return res.end(JSON.stringify({gate}));
    if (req.url === '/permission') {
      const got = JSON.parse(body);
      if (got.command !== 'ls /tmp' || got.agent !== 'demo' || got.tool !== 'Bash') {
        return res.end(JSON.stringify({decision: 'block', reason: 'bad request ' + body}));
      }
      return res.end(JSON.stringify(reply));
    }
    res.statusCode = 404;
    res.end();
  });
});

const input = JSON.stringify({cwd: '/work/demo', tool_name: 'Bash', tool_input: {command: 'ls /tmp'}, tool_use_id: 't1'});

function run(hub, env = {}) {
  return new Promise((resolve) => {
    const p = spawn('pwsh', ['-NoProfile', '-File', ps1], {
      env: {...process.env, ATRIUM_HUB_URL: hub, ATRIUM_AGENT_NAME: 'demo', ATRIUM_PERM_GATE: '', ...env},
    });
    let out = '';
    p.stdout.on('data', (d) => (out += d));
    p.on('close', (code) => {
      out = out.trim();
      resolve({code, out: out ? JSON.parse(out).hookSpecificOutput : null});
    });
    p.stdin.end(input);
  });
}
let failed = 0;
function check(name, got, want) {
  const ok = JSON.stringify(got) === JSON.stringify(want);
  if (!ok) failed++;
  console.log(`${ok ? 'ok  ' : 'FAIL'} ${name}${ok ? '' : ' got ' + JSON.stringify(got)}`);
}

srv.listen(0, '127.0.0.1', async () => {
  const hub = `http://127.0.0.1:${srv.address().port}`;
  reply = {decision: 'approve', reason: ''};
  let r = await run(hub);
  check('approve -> allow', [r.code, r.out.permissionDecision], [0, 'allow']);

  reply = {decision: 'block', reason: 'use the other directory'};
  r = await run(hub);
  check('block -> deny with reason', [r.code, r.out.permissionDecision, r.out.permissionDecisionReason],
    [0, 'deny', 'use the other directory']);

  reply = {decision: 'approve', reason: '', command: 'ls /tmp/safe'};
  r = await run(hub);
  check('edited approve -> deny naming the edit', [r.code, r.out.permissionDecision,
    r.out.permissionDecisionReason.endsWith('ls /tmp/safe')], [0, 'deny', true]);

  gate = false;
  r = await run(hub);
  check('not gated -> no decision', [r.code, r.out], [0, null]);
  gate = true;

  r = await run(hub, {ATRIUM_PERM_GATE: 'off'});
  check('ATRIUM_PERM_GATE=off -> no decision', [r.code, r.out], [0, null]);

  gate = false;
  reply = {decision: 'approve', reason: ''};
  r = await run(hub, {ATRIUM_PERM_GATE: 'on'});
  check('ATRIUM_PERM_GATE=on skips /gate', [r.code, r.out.permissionDecision], [0, 'allow']);

  srv.close(async () => {
    r = await run(hub);
    check('atrium unreachable -> exit 0, no decision', [r.code, r.out], [0, null]);
    process.exit(failed ? 1 : 0);
  });
});
