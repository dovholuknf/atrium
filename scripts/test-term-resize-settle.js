// A window drag must send the runner ONE resize, at the size it settled on.
//
// THE BUG. Claude Code redraws its whole conversation on every resize and never
// clears the scrollback first. A drag fires a fit per step, and each one sent a
// resize, so one drag left one full transcript copy in the scrollback per step.
// The operator's terminal held one message 87 times after a single drag.
//
// THE FIX, exercised here against the real code. `sendResizeSettled` waits for
// the size to hold still before `sendResize` sends the frame, and the attach's
// own immediate `sendResize` cancels anything pending.
const { boardScript } = require("./board-source.js");

let bad = 0;
function fail(msg) {
  console.error("FAIL: " + msg);
  bad++;
}

const page = boardScript();

function lift(start, end) {
  const at = page.indexOf(start);
  if (at < 0) {
    console.error(`FAIL: the board no longer has \`${start}\`. The resize ` +
      `settle was renamed or moved, and this test cannot say anything about it. ` +
      `Point it at whatever replaced it.`);
    process.exit(1);
  }
  const stop = page.indexOf(end, at + start.length);
  return page.slice(at, stop < 0 ? page.length : stop + end.length);
}

const src = lift("function sendResize(", "\n}") + "\n" +
  lift("const resizeSettleMs", "let resizeSettleTimer = 0;") + "\n" +
  lift("function sendResizeSettled(", "\n}");

// A fake clock, so the test does not sleep. `later` queues a callback at a
// time, `advance` runs everything due.
const harness = new Function(`
  let now = 0, seq = 0;
  const timers = new Map();
  function setTimeout(fn, ms) { const id = ++seq; timers.set(id, { at: now + ms, fn }); return id; }
  function clearTimeout(id) { timers.delete(id); }
  function advance(ms) {
    const until = now + ms;
    for (;;) {
      let next = null;
      for (const [id, t] of timers) if (t.at <= until && (!next || t.at < next[1].at)) next = [id, t];
      if (!next) break;
      timers.delete(next[0]);
      now = next[1].at;
      next[1].fn();
    }
    now = until;
  }
  const sent = [];
  function send(m) { sent.push(m); }
  let term = null;
  ${src}
  return {
    sendResize, sendResizeSettled, advance, sent,
    settleMs: resizeSettleMs,
    setTerm: t => { term = t; },
  };
`)();

const t1 = { cols: 170, rows: 40 };
harness.setTerm(t1);

// ── a drag of five steps sends one resize, at the last size ─────────────────
for (const c of [170, 171, 172, 173, 174]) {
  t1.cols = c;
  harness.sendResizeSettled();
  harness.advance(30);
}
if (harness.sent.length !== 0) {
  fail("a resize went out mid-drag, so every drag step is still a reprint. sent " +
    JSON.stringify(harness.sent) + ".");
}
harness.advance(harness.settleMs);
if (harness.sent.length !== 1 || harness.sent[0].cols !== 174) {
  fail("a five-step drag did not send exactly one resize at 174 columns. sent " +
    JSON.stringify(harness.sent) + ".");
}

// ── the attach sends at once and cancels a pending settle ───────────────────
harness.sent.length = 0;
t1.cols = 120;
harness.sendResizeSettled();
harness.sendResize();
harness.advance(harness.settleMs * 2);
if (harness.sent.length !== 1) {
  fail("an immediate resize did not cancel the pending one, so the runner is told " +
    "its size twice and reprints twice. sent " + JSON.stringify(harness.sent) + ".");
}

// ── a terminal switch drops the old terminal's pending resize ───────────────
harness.sent.length = 0;
harness.sendResizeSettled();
harness.setTerm({ cols: 80, rows: 24 });
harness.advance(harness.settleMs * 2);
if (harness.sent.length !== 0) {
  fail("a settle queued for one terminal fired after a switch to another. sent " +
    JSON.stringify(harness.sent) + ".");
}

if (bad) process.exit(1);
console.log("a window drag tells the runner its size once, after it settles.");
