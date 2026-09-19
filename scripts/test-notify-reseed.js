// A single room dropping must not announce the whole board.
//
// When the attached-room count crosses 1<->2 the aggregate view flips every
// card id between `room~id` and bare (see the hub's tagFor/splitTag). The alert
// diff in notify.js used to compare raw ids, so that flip read as every card
// arriving at once: a flood of "X is back" toasts. Two guards fix it and both
// are exercised here against the real code:
//   - `check` diffs on the BARE id (`bareId`), so a card keeps its identity
//     across the flip.
//   - `reseed` takes the next set as the baseline when the room set changes.
//
// The functions are searched for by name rather than sliced at an offset, so a
// paragraph of comment added above one does not break the test that protects it.
const { boardScript } = require("./board-source.js");

let bad = 0;
function fail(msg) {
  console.error("FAIL: " + msg);
  bad++;
}

const page = boardScript();

// `end` is the closing brace WITH its indentation, because these functions are
// nested inside the alerting closure and their brace is not at column 0.
function lift(start, end) {
  const at = page.indexOf(start);
  if (at < 0) {
    console.error(`FAIL: the board no longer has \`${start}\`. The reconnect-flood ` +
      `guard was renamed or moved, and the test cannot say anything about it. ` +
      `Point it at whatever replaced it.`);
    process.exit(1);
  }
  const stop = page.indexOf(end, at + start.length);
  return page.slice(at, stop < 0 ? page.length : stop + end.length);
}

const bareIdSrc = lift("function bareId(", "\n}");
const reseedConst = lift("const RESEED =", ";");
const checkSrc = lift("function check(kind, items, describe) {", "\n  }");
const reseedSrc = lift("function reseed() {", "\n  }");

// The real check and reseed, wired to stubs for everything they lean on that is
// tested elsewhere. `announce` records instead of alerting, so the test can ask
// what would have been said.
const harness = new Function(`
  ${bareIdSrc}
  ${reseedConst}
  const known = {};
  let settlingNow = false;
  const pending = {};
  const prefs = { debounce: 0 };
  const announced = [];
  function poppedOut() { return false; }
  function play() {}
  function notify() {}
  function iconForAlert() { return ""; }
  function soundForAlert() { return ""; }
  function announce(kind, fresh, describe) {
    announced.push({ kind, ids: fresh.map(f => f.id) });
  }
  ${checkSrc}
  ${reseedSrc}
  return { check, reseed, announced };
`)();

const card = (id) => ({ id, task_id: id, agent: "an agent" });
const describe = (t) => ({ title: t.id, body: "" });

// Two rooms attached: ids come tagged.
const tagged = [card("sg4~01a0"), card("sg4~01b1"), card("sgg~02c2")];
// sgg drops (2 -> 1): sg4's cards go BARE, sgg's cards leave.
const bare = [card("01a0"), card("01b1")];

// ── the diff is on the bare id ──────────────────────────────────────────────
harness.announced.length = 0;
harness.check("arrived", tagged, describe);        // first pass: seed, silent
if (harness.announced.length) {
  fail("the first check announced instead of seeding: " +
    JSON.stringify(harness.announced));
}
harness.check("arrived", bare, describe);          // same cards, now bare
if (harness.announced.length) {
  fail("a room dropping re-announced cards whose only change was the `room~` tag: " +
    JSON.stringify(harness.announced) + ". A card must keep its identity across " +
    "the tag flip.");
}

// A genuinely new card while the room set is stable still announces.
harness.check("arrived", bare.concat(card("03d3")), describe);
if (harness.announced.length !== 1 || harness.announced[0].ids.join() !== "03d3") {
  fail("a real new arrival stopped announcing after the bare-id change: " +
    JSON.stringify(harness.announced));
}

// ── the READY/WAITING path is diffed on the bare id too ─────────────────────
// The false alert clint saw was "css-changes is ready" fired when a room
// attached: the ready/waiting kind runs through the same `check`, so a card
// present before and after the room-set change (its id flipping bare<->tagged)
// must NOT re-announce as ready. Same guarantee as arrivals, asserted here for
// the kind that actually rang.
harness.announced.length = 0;
harness.check("waiting", tagged, describe);        // seed: two rooms, tagged
if (harness.announced.length) {
  fail("the first waiting check announced instead of seeding: " +
    JSON.stringify(harness.announced));
}
harness.check("waiting", bare, describe);          // same cards, now bare
if (harness.announced.length) {
  fail("a room dropping re-announced ready cards whose only change was the " +
    "`room~` tag: " + JSON.stringify(harness.announced) + ". A ready card must " +
    "keep its identity across the tag flip, or a room attaching says a " +
    "two-hour-idle card just became ready.");
}
// A card that GENUINELY becomes ready while the room set is stable still rings.
harness.check("waiting", bare.concat(card("06a6")), describe);
if (harness.announced.length !== 1 || harness.announced[0].ids.join() !== "06a6") {
  fail("a real newly-ready card stopped announcing after the bare-id change: " +
    JSON.stringify(harness.announced));
}

// ── reseed silences the churn tick, then diffing resumes ────────────────────
harness.announced.length = 0;
harness.check("waiting", bare, describe);          // seed: one room, bare
harness.reseed();                                  // room set changed
// The re-tagged set arrives; nothing genuinely new, so nothing is said. This
// covers the flip in the other direction (1 -> 2) and any set churn a bare-id
// diff alone would not, since a card leaving and a different one arriving on the
// same tick would otherwise read as one arrival.
harness.check("waiting", tagged.concat(card("sgg~04e4")), describe);
if (harness.announced.length) {
  fail("reseed did not silence the room-set-change tick: " +
    JSON.stringify(harness.announced) + ". A room attaching or detaching must " +
    "re-seed the baseline, not announce the churn.");
}
// And the very next tick diffs normally again: reseed is one-shot.
harness.check("waiting", tagged.concat(card("sgg~04e4"), card("sgg~05f5")), describe);
if (harness.announced.length !== 1 || harness.announced[0].ids.join() !== "sgg~05f5") {
  fail("reseed was not one-shot: the tick after a room change did not resume " +
    "diffing: " + JSON.stringify(harness.announced));
}

if (bad) {
  process.exit(1);
}
console.log("a room dropping does not flood the board with arrivals.");
