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
  let termFitCols = 0;
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

// ── a window narrower than the pty draws the pty's width and scrolls ────────
//
// The pty follows the widest viewer. This window can show 80 columns, the pty
// is 150, so xterm draws 150, the pane scrolls sideways, and the resize this
// window sends still says 80, or the pty could never narrow again.
const wideSrc = lift("function fitTerm(", "\n}") + "\n" +
  lift("function applyPtyWidth(", "\n}") + "\n" +
  lift("function markWide(", "\n}") + "\n" +
  lift("function sendResize(", "\n}");
const wide = new Function(`
  let termFitCols = 0, termPtyCols = 0, resizeSettleTimer = 0;
  function clearTimeout() {}
  const sent = [];
  function send(m) { sent.push(m); }
  const classes = new Set();
  const xtermEl = { style: { width: "" } };
  const host = {
    classList: { toggle: (c, on) => on ? classes.add(c) : classes.delete(c) },
    querySelector: () => xtermEl,
  };
  const document = { getElementById: () => host };
  let proposed = { cols: 80, rows: 24 };
  const termFit = { proposeDimensions: () => proposed };
  const term = {
    cols: 80, rows: 24,
    resize(c, r) { this.cols = c; this.rows = r; },
    _core: { _renderService: { clear() {}, dimensions: { css: { cell: { width: 8 } } } } },
  };
  ${wideSrc}
  return {
    fitTerm, applyPtyWidth, sendResize, sent, term, xtermEl,
    wide: () => classes.has("wide"),
    setPty: c => { termPtyCols = c; },
    propose: (c, r) => { proposed = { cols: c, rows: r }; },
  };
`)();

wide.fitTerm();
if (wide.term.cols !== 80 || wide.wide()) {
  fail("with no pty width known, the window did not draw its own width. cols " + wide.term.cols + ".");
}
wide.setPty(150);
wide.applyPtyWidth();
if (wide.term.cols !== 150 || !wide.wide() || !wide.xtermEl.style.width) {
  fail("a pty wider than the window did not widen the grid into a sideways scroll. cols " +
    wide.term.cols + ", wide " + wide.wide() + ", width " + JSON.stringify(wide.xtermEl.style.width) + ".");
}
wide.sendResize();
if (wide.sent[0].cols !== 80) {
  fail("a widened window reported the pty's width as its own, so the pty can never narrow. sent " +
    JSON.stringify(wide.sent) + ".");
}
// The window grows past the pty: it draws its own width and stops scrolling.
wide.propose(200, 30);
wide.fitTerm();
if (wide.term.cols !== 200 || wide.wide() || wide.xtermEl.style.width !== "") {
  fail("a window wider than the pty kept the sideways scroll. cols " + wide.term.cols + ".");
}

// ── a claude card drops its scrollback at a width change, a shell never ─────
const sizeSrc = lift("function takeTermSize(", "\n}");
function sizeHarness(reprints) {
  return new Function(`
    let termPtyCols = 0, cleared = 0;
    const termCaps = { reprints_on_resize: ${reprints} };
    const term = { clear() { cleared++; } };
    function applyPtyWidth() {}
    ${sizeSrc}
    return { take: takeTermSize, cleared: () => cleared };
  `)();
}
const claudeCard = sizeHarness(true);
claudeCard.take('{"t":"size","cols":120,"rows":40}');
if (claudeCard.cleared() !== 0) fail("the first size frame on an attach cleared the scrollback.");
claudeCard.take('{"t":"size","cols":120,"rows":30}');
if (claudeCard.cleared() !== 0) fail("a height-only change cleared the scrollback, and nothing reprints it.");
claudeCard.take('{"t":"size","cols":150,"rows":30}');
if (claudeCard.cleared() !== 1) {
  fail("a claude card's width change kept the old transcript, so the reprint piles a second copy on top.");
}
const shellCard = sizeHarness(false);
shellCard.take('{"t":"size","cols":120,"rows":40}');
shellCard.take('{"t":"size","cols":150,"rows":40}');
if (shellCard.cleared() !== 0) fail("a shell's width change cleared its scrollback, which nothing reprints.");

if (bad) process.exit(1);
console.log("a window drag tells the runner its size once, after it settles, " +
  "and a window narrower than the pty scrolls sideways.");
