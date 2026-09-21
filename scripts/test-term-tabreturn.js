// A browser tab return must restore the cursor when it reflowed the grid, the
// same way a room-set change does, and must NOT re-attach the pane when it did
// not.
//
// THE BUG. clint switches browser tabs away from an attached sg4 pane and back,
// types, and the input garbles. It is the same class the room flip already
// fixed (bae77e1): a re-fit that changes cols/rows reflows xterm's buffer and
// moves the cursor against it, a non-binding viewer gets no SIGWINCH so the
// runner never repaints, and the old cursor sits against a reflowed grid. The
// trigger is different: not a room-set change but a tab-return re-fit, which
// runs as the tab becomes visible because a box change that happened while it
// was hidden only flushes to layout on return.
//
// THE FIX, exercised here against the real code. `onTabVisibility` remembers the
// grid on the way to hidden, and on the way back settles the box through
// `onTermResize` (which skips a no-op fit via `paneBoxUnchanged`) and re-attaches
// ONCE, through `resyncCursorAfterFlip`, only when the grid actually moved. The
// function is searched for by name, so a paragraph of comment above it does not
// break this test.
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
    console.error(`FAIL: the board no longer has \`${start}\`. The tab-return ` +
      `cursor restore was renamed or moved, and this test cannot say anything ` +
      `about it. Point it at whatever replaced it.`);
    process.exit(1);
  }
  const stop = page.indexOf(end, at + start.length);
  return page.slice(at, stop < 0 ? page.length : stop + end.length);
}

const handlerSrc = lift("function onTabVisibility(", "\n}");

// The real handler, wired to stubs for everything it leans on. `doc.visibilityState`
// is the tab state; `term` and `termTask` are mutable the way the board globals
// are; `onTermResize` applies whatever the next fit would produce (`fitResult`),
// which is how a return that reflowed the grid is modelled; `resyncCursorAfterFlip`
// is counted, since the whole point is that it fires on a reflowing return and
// nowhere else. `requestAnimationFrame` runs synchronously so the assertions can
// read the result on the same tick.
const harness = new Function(`
  const doc = { visibilityState: "visible" };
  const document = doc;
  let term = null;
  let termTask = null;
  let solo = false;
  let fitResult = null;
  let resized = 0;
  let resynced = 0;
  function termOnly() { return solo; }
  function onTermResize() {
    resized++;
    // A real fit sizes the grid; the return that reflowed it is exactly the case
    // this test is about, so the stub applies the pre-set post-fit dimensions.
    if (term && fitResult) { term.cols = fitResult.cols; term.rows = fitResult.rows; }
  }
  function resyncCursorAfterFlip() { resynced++; }
  function requestAnimationFrame(fn) { fn(); }
  let preHideGrid = "";
  ${handlerSrc}
  return {
    fire: onTabVisibility,
    setVisible: v => { doc.visibilityState = v ? "visible" : "hidden"; },
    setTerm: t => { term = t; },
    getTerm: () => term,
    setTask: t => { termTask = t; },
    setSolo: v => { solo = v; },
    fitTo: (cols, rows) => { fitResult = { cols, rows }; },
    resized: () => resized,
    resynced: () => resynced,
    preHide: () => preHideGrid,
  };
`)();

const mkTerm = (cols, rows) => ({ cols, rows });

// ── a return that reflowed the grid re-attaches once ─────────────────────────
harness.setTerm(mkTerm(80, 24));
harness.setTask({ id: "claude-sg4~01a0" });
// Away: the grid is remembered.
harness.setVisible(false);
harness.fire();
if (harness.preHide() !== "80x24") {
  fail("going hidden did not remember the grid, so the return has nothing to " +
    "compare against. preHideGrid is " + JSON.stringify(harness.preHide()) + ".");
}
if (harness.resynced() !== 0) {
  fail("going hidden re-attached the pane. The resync is for the RETURN, not " +
    "for leaving. resync count is " + harness.resynced() + ".");
}
// Back, and the window resized behind the tab, so the fit reflows to a new grid.
harness.fitTo(100, 30);
harness.setVisible(true);
harness.fire();
if (harness.resized() !== 1) {
  fail("the return did not settle the box through onTermResize once. resize " +
    "count is " + harness.resized() + ".");
}
if (harness.resynced() !== 1) {
  fail("a tab return that reflowed the grid did not re-attach the pane, so the " +
    "input garble that returns on a tab switch is not fixed. resync count is " +
    harness.resynced() + ".");
}
if (harness.preHide() !== "") {
  fail("the remembered grid was not cleared on return, so a later visible with " +
    "no hidden in between could re-fire off a stale grid.");
}

// ── a return that changed nothing does NOT re-attach ─────────────────────────
harness.setTerm(mkTerm(80, 24));
harness.setVisible(false);
harness.fire();
// Back, box unchanged: the fit lands on the same grid.
harness.fitTo(80, 24);
harness.setVisible(true);
harness.fire();
if (harness.resynced() !== 1) {
  fail("a tab return that did not reflow the grid re-attached the pane anyway, " +
    "so an ordinary glance away and back resets the terminal for no reason. " +
    "resync count is " + harness.resynced() + " (expected the earlier 1, no more).");
}

// ── a bare visible with no hidden before it does nothing ─────────────────────
// A fresh open, or a second visible event, must not re-fit or re-attach off a
// grid it never recorded.
const resizedBefore = harness.resized();
harness.setVisible(true);
harness.fire();
if (harness.resynced() !== 1 || harness.resized() !== resizedBefore) {
  fail("a visible event with no prior hidden re-fit or re-attached the pane. " +
    "resync=" + harness.resynced() + " resize=" + harness.resized() + ".");
}

// ── a solo window owns its own reconnect, so it is skipped ────────────────────
harness.setSolo(true);
harness.setTerm(mkTerm(80, 24));
harness.setVisible(false);
harness.fire();
harness.fitTo(120, 40);
harness.setVisible(true);
harness.fire();
if (harness.resynced() !== 1) {
  fail("a solo window re-attached on a tab return. Solo carries its own " +
    "reconnect keyed off its hash and must be skipped, the way the flip skips " +
    "it. resync count is " + harness.resynced() + ".");
}
harness.setSolo(false);

// ── no attached terminal: nothing to restore ─────────────────────────────────
harness.setTerm(null);
harness.setVisible(false);
harness.fire();
if (harness.preHide() !== "") {
  fail("going hidden with no terminal recorded a grid, so a later return would " +
    "compare against a phantom. preHideGrid is " + JSON.stringify(harness.preHide()) + ".");
}
harness.setVisible(true);
harness.fire();
if (harness.resynced() !== 1) {
  fail("a tab return with no attached terminal re-attached something. resync " +
    "count is " + harness.resynced() + ".");
}

if (bad) {
  process.exit(1);
}
console.log("a tab return restores the cursor only when it reflowed the grid.");
