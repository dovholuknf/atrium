// A flapping room must not turn the board into a fetch storm.
//
// A room that attaches and detaches every few seconds makes the hub emit a
// `rooms` event on every flip, and the board answers each one with a refresh
// that fans out seven-plus fetches. With nothing bounding that, a flap storm
// stacked passes until the tab had emptied Chrome's per-tab socket pool and
// every request after that failed with ERR_INSUFFICIENT_RESOURCES.
//
// Three guards fix it and all three are exercised here against the real code:
//   - DEBOUNCE: a burst of triggers collapses into ONE refresh (`refreshSoon`),
//     with a cap so a room that never stops flapping still repaints eventually.
//   - SINGLE-FLIGHT: a trigger while a pass is running queues EXACTLY ONE more
//     pass, it does not start a second overlapping fan-out (`runRefresh`).
//   - SUPERSEDE + BACK OFF: the next pass aborts the last one's fetches, and a
//     run of failed fetches delays the queued pass rather than re-firing it.
//
// The functions are lifted by name from the concatenated board so a paragraph
// of comment added above one does not break the test that protects it.
const { boardScript } = require("./board-source.js");

let bad = 0;
function fail(msg) {
  console.error("FAIL: " + msg);
  bad++;
}

const page = boardScript();

// The debounce and single-flight machinery is a block of consts and two
// functions. Lift from the first const up to the `refresh` it drives.
function region(start, stopBefore) {
  const at = page.indexOf(start);
  if (at < 0) {
    console.error(`FAIL: the board no longer has \`${start}\`. The refresh-storm ` +
      `guard was renamed or moved, and the test cannot say anything about it. ` +
      `Point it at whatever replaced it.`);
    process.exit(1);
  }
  const stop = page.indexOf(stopBefore, at + start.length);
  if (stop < 0) {
    console.error(`FAIL: could not find \`${stopBefore}\` after \`${start}\`.`);
    process.exit(1);
  }
  return page.slice(at, stop);
}

const src = region("const REFRESH_DEBOUNCE", "async function refresh(signal)");

// A virtual clock, so a three-second flap storm runs instantly and the test can
// say exactly when each timer fires.
const harness = new Function("hooks", `
  let now = 0;
  let nextId = 1;
  const timers = [];
  function setTimeout(fn, ms) {
    const id = nextId++;
    timers.push({ id, at: now + (ms || 0), fn });
    return id;
  }
  function clearTimeout(id) {
    const i = timers.findIndex(t => t.id === id);
    if (i >= 0) timers.splice(i, 1);
  }
  const Date = { now: () => now };
  function advance(ms) {
    const end = now + ms;
    for (;;) {
      let due = null;
      for (const t of timers) if (t.at <= end && (!due || t.at < due.at)) due = t;
      if (!due) break;
      timers.splice(timers.indexOf(due), 1);
      now = due.at;
      due.fn();
    }
    now = end;
  }

  // Stand-ins the lifted code leans on.
  let apiFailStreak = 0;
  let abortCount = 0;
  function AbortController() {
    this.signal = { aborted: false };
    this.abort = () => { abortCount++; this.signal.aborted = true; };
  }

  // The refresh under test is stubbed: it counts its starts and, unless the
  // test asks it to settle at once, returns a promise the test resolves by hand
  // so a pass can be held "in flight".
  let starts = 0;
  let pending = null;
  function refresh(signal) {
    starts++;
    if (hooks.autoSettle) return Promise.resolve();
    return new Promise(res => { pending = res; });
  }
  function settle() { const r = pending; pending = null; if (r) r(); }

  ${src}

  return {
    refreshSoon, runRefresh, advance, settle,
    starts: () => starts,
    abortCount: () => abortCount,
    setFail: n => { apiFailStreak = n; },
    setAutoSettle: v => { hooks.autoSettle = v; }
  };
`)({ autoSettle: false });

// Let the microtask queue drain so a settled refresh's `.finally` runs before
// the next assertion.
async function drain() {
  for (let i = 0; i < 8; i++) await Promise.resolve();
}

async function main() {
  // ── a burst collapses into one refresh ────────────────────────────────────
  // Ten flaps in three seconds, one every ~250ms, all inside the debounce.
  for (let i = 0; i < 10; i++) {
    harness.refreshSoon();
    harness.advance(250);
  }
  harness.advance(300);       // let the window close
  await drain();
  if (harness.starts() !== 1) {
    fail("a burst of " + 10 + " flaps started " + harness.starts() +
      " refreshes, not 1. The trailing debounce is not coalescing.");
  }

  // ── single-flight: triggers during a pass queue exactly one more ──────────
  // The first pass is still in flight (never settled). Fire twenty more
  // triggers; not one may start a second overlapping pass.
  for (let i = 0; i < 20; i++) {
    harness.refreshSoon();
    harness.advance(400);
  }
  if (harness.starts() !== 1) {
    fail("triggers during an in-flight pass started " + (harness.starts() - 1) +
      " overlapping refreshes. Single-flight is broken.");
  }

  // Settle the pass. Exactly ONE queued pass may run, and it must have aborted
  // the pass it supersedes.
  harness.settle();
  await drain();
  harness.advance(10);        // the requeue is a zero-delay timer
  await drain();
  if (harness.starts() !== 2) {
    fail("after the in-flight pass settled, " + (harness.starts() - 1) +
      " passes ran, not 1. The dirty flag must queue exactly one.");
  }
  if (harness.abortCount() < 1) {
    fail("the superseding pass did not abort the previous pass's fetches.");
  }

  // Settle the second pass with nothing dirty: the loop stops.
  harness.settle();
  await drain();
  harness.advance(100);
  await drain();
  if (harness.starts() !== 2) {
    fail("a pass ran with nothing dirty: the refresh loop does not stop.");
  }

  // ── back off while the wire is down ───────────────────────────────────────
  // A pass is in flight, a trigger marks it dirty, and every fetch has been
  // failing. The requeued pass must WAIT, not fire at once.
  harness.setFail(3);         // three failures in a row -> ~1500ms wait
  harness.refreshSoon();
  harness.advance(300);
  await drain();              // starts pass 3
  if (harness.starts() !== 3) fail("expected a third pass to start.");
  harness.refreshSoon();      // mark dirty mid-pass
  harness.advance(400);
  harness.settle();           // pass 3 done; requeue should be delayed
  await drain();
  harness.advance(100);       // well under the backoff
  await drain();
  if (harness.starts() !== 3) {
    fail("a failing tab re-fired the storm immediately instead of backing off.");
  }
  harness.advance(2000);      // past the backoff
  await drain();
  if (harness.starts() !== 4) {
    fail("the backed-off pass never ran after its delay elapsed.");
  }
  harness.settle();
  await drain();

  // ── the debounce still fires under a never-ending flap ────────────────────
  // Events every 250ms forever would reset a plain trailing debounce and the
  // board would never repaint. The max-wait cap must force a refresh anyway.
  harness.setAutoSettle(true);
  const before = harness.starts();
  for (let i = 0; i < 12; i++) {   // 12 * 250ms = 3s of sub-debounce flapping
    harness.refreshSoon();
    harness.advance(250);
    await drain();
  }
  harness.advance(400);
  await drain();
  if (harness.starts() <= before) {
    fail("a room flapping faster than the debounce never repainted the board. " +
      "The max-wait cap is missing.");
  }

  if (bad) process.exit(1);
  console.log("a flapping room does not storm the board with fetches.");
}

main();
