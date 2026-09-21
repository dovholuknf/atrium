// A room-set change must re-resolve the attached card, not loop on a stale tag.
//
// THE BUG. The aggregate view tags a card `room~id` while more than one room is
// attached and serves it bare with one (the hub's tagFor/splitTag, mirrored by
// `bareId`). When a second room joins or leaves, `/v1/tasks` flips every id at
// once, but the attached `termTask` still holds the id from before the flip.
//
// `renderTermList` used to test the attached card against the list by RAW id, so
// after the flip the attached card read as gone. It tore the pane down; the
// single-card endpoint still resolved the stale tagged id, so the watchdog
// re-attached; the next poll tore it down again. An infinite teardown/reattach
// loop that ate keystrokes and exhausted WebGL contexts, and it kept spinning
// after the second room had already left, because the browser still held the
// tagged id it remembered while there were two.
//
// THE FIX, exercised here against the real code. `reconcileAttached` matches the
// attached card by its BARE id, re-points `termTask.id` (and the reload slot) to
// the form the hub serves now, and tears the pane down ONLY when no supervised
// row shares the bare id. The functions are searched for by name, so a
// paragraph of comment added above one does not break this test.
const { boardScript } = require("./board-source.js");

let bad = 0;
function fail(msg) {
  console.error("FAIL: " + msg);
  bad++;
}

const page = boardScript();

// Lift a top-level function or block by name. `end` is a marker that closes it;
// for a function whose brace sits at column 0 that is "\n}".
function lift(start, end) {
  const at = page.indexOf(start);
  if (at < 0) {
    console.error(`FAIL: the board no longer has \`${start}\`. The room-flip guard ` +
      `was renamed or moved, and this test cannot say anything about it. Point it ` +
      `at whatever replaced it.`);
    process.exit(1);
  }
  const stop = page.indexOf(end, at + start.length);
  return page.slice(at, stop < 0 ? page.length : stop + end.length);
}

const bareIdSrc = lift("function bareId(", "\n}");
const helpersSrc = lift("let attachInFlight = \"\";",
  "bareId(attachInFlight) === bareId(card);\n}");
const reconcileSrc = lift("function reconcileAttached(tasks) {", "\n}");
const retagSrc = lift("function retagTermId(id) {", "\n}");

// The real reconcile/retag/in-flight code, wired to stubs for everything it
// leans on. `store` is a localStorage double; `tornDown` counts pane teardowns;
// `termTask` and `termKindFor` are mutable the way the board's globals are.
const harness = new Function(`
  ${bareIdSrc}
  ${helpersSrc}
  const store = {};
  const localStorage = {
    getItem: k => (k in store ? store[k] : null),
    setItem: (k, v) => { store[k] = String(v); },
    removeItem: k => { delete store[k]; },
  };
  let tornDown = 0;
  let resynced = 0;
  let termTask = null;
  let termKindFor = "";
  function termOnly() { return false; }
  function rlog() {}
  function clearTermPane() { tornDown++; termTask = null; }
  // The real one reconnects the socket a frame later so the daemon replays with
  // the cursor restored. Here we only need to know reconcileAttached ASKS for it
  // on a flip, and never on a steady poll or a genuine teardown.
  function resyncCursorAfterFlip() { resynced++; }
  ${reconcileSrc}
  ${retagSrc}
  return {
    reconcile: reconcileAttached,
    setTask: t => { termTask = t; },
    getTask: () => termTask,
    setKindFor: v => { termKindFor = v; },
    getKindFor: () => termKindFor,
    setInFlight: markAttachInFlight,
    inFlight: attachIsInFlight,
    store,
    torn: () => tornDown,
    resynced: () => resynced,
  };
`)();

const sup = (id) => ({ id, supervised: true });

// ── 2 -> 1: the attached tagged card is re-resolved to bare, not torn down ───
harness.setTask({ id: "claude-sg4~01a0", supervised: true });
harness.store["atrium.term"] = "claude-sg4~01a0";
harness.setKindFor("claude-sg4~01a0");

// The single room now serves the card bare. This is the poll that used to loop.
let tore = harness.reconcile([sup("01a0"), sup("02b1")]);
if (tore) {
  fail("a room-set change tore the attached pane down instead of re-resolving " +
    "its id. This is the teardown that spun the infinite reattach loop.");
}
if (harness.getTask().id !== "01a0") {
  fail("the attached card was not re-resolved to the bare id the hub now serves. " +
    "termTask.id is still " + JSON.stringify(harness.getTask().id) + ".");
}
if (harness.store["atrium.term"] !== "01a0") {
  fail("the reload slot still holds the stale tagged id " +
    JSON.stringify(harness.store["atrium.term"]) + ", so a restart would wait out " +
    "an id the hub no longer answers to.");
}
if (harness.getKindFor() !== "01a0") {
  fail("termKindFor kept the stale tagged id, so the next open of this card reads " +
    "as a different one and resets its runner/shell choice.");
}
// THE FLIP ASKS FOR A CURSOR RESYNC. The re-fit a room-set change triggers is
// what left the cursor misplaced, and re-resolving the id in place replays
// nothing, so the flip has to re-attach once to restore the cursor.
if (harness.resynced() !== 1) {
  fail("a room-set change re-resolved the id but did not schedule a cursor resync, " +
    "so the input garble that returns on a room attach/detach is not fixed. " +
    "resync count is " + harness.resynced() + ".");
}

// And a second, unchanged poll is stable: nothing to re-resolve, nothing torn.
tore = harness.reconcile([sup("01a0"), sup("02b1")]);
if (tore || harness.getTask().id !== "01a0") {
  fail("the reconciled pane did not settle: a steady-state poll tore it down or " +
    "re-tagged it again.");
}
// A STEADY POLL DOES NOT RESYNC. The resync is a one-shot per flip, or a room
// that never changes would re-attach the pane on every poll and reset the
// terminal under whoever is typing.
if (harness.resynced() !== 1) {
  fail("a steady-state poll scheduled another cursor resync, so a quiet board " +
    "would re-attach the pane on every poll. resync count is " + harness.resynced() + ".");
}

// ── 1 -> 2: the bare attached card is re-resolved to tagged, both directions ──
harness.setTask({ id: "01a0", supervised: true });
harness.store["atrium.term"] = "01a0";
tore = harness.reconcile([sup("claude-sg4~01a0"), sup("claude-sgg~02b1")]);
if (tore) {
  fail("a second room joining tore the attached pane down. The flip loops both " +
    "ways, so the bare->tagged direction must re-resolve too.");
}
if (harness.getTask().id !== "claude-sg4~01a0") {
  fail("the attached card was not re-resolved to the tagged id after a room " +
    "joined. termTask.id is " + JSON.stringify(harness.getTask().id) + ".");
}
// The other direction of the flip resyncs too: the re-fit that misplaces the
// cursor happens whether a room joined or left.
if (harness.resynced() !== 2) {
  fail("a room joining re-resolved the id but did not schedule a cursor resync. " +
    "The bare->tagged direction must restore the cursor as well. resync count is " +
    harness.resynced() + ".");
}

// ── a genuinely gone card is still torn down ─────────────────────────────────
harness.setTask({ id: "01a0", supervised: true });
tore = harness.reconcile([sup("09z9")]);
if (!tore) {
  fail("an attached card with no supervised match by bare id was NOT torn down. " +
    "The fix must not blind the poll to a card that really left.");
}
// A CARD THAT LEFT DOES NOT RESYNC. There is nothing to re-attach to, and the
// resync is only for a card that is still here under a re-resolved id.
if (harness.resynced() !== 2) {
  fail("a torn-down card scheduled a cursor resync, which would re-attach a pane " +
    "whose card is gone. resync count is " + harness.resynced() + ".");
}

// ── a torn card while an attach is in flight is left alone ───────────────────
// The cached list drops `supervised` a beat before the single-card poll does.
// Tearing the pane down on that lag is the OTHER way this loop was spun, and the
// in-flight guard must survive the bare-id change.
harness.setTask({ id: "claude-sg4~03c2", supervised: true });
harness.setInFlight("claude-sg4~03c2");
// The list lags: the card is present bare but not yet supervised. Even so, an
// in-flight attach must hold the pane.
tore = harness.reconcile([{ id: "03c2", supervised: false }]);
if (tore) {
  fail("the pane was torn down while an attach was in flight for it, across the " +
    "tag flip. The in-flight guard must compare by bare id.");
}
// The in-flight mark, set under the tagged id, is recognised under the bare id.
if (!harness.inFlight("03c2")) {
  fail("attachIsInFlight did not match the bare id of a mark set under the " +
    "tagged id, so `onopen` clearing under the other spelling would strand it.");
}

if (bad) {
  process.exit(1);
}
console.log("a room-set change re-resolves the attached card instead of looping.");
