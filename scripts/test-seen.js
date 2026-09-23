// The seen report, RUN against stubs for the board globals.
//
// The rule is that a turn is reported seen only after a visible, focused window
// has shown that card's runner terminal, attached and scrolled to the bottom,
// for the whole dwell. Every way it can go wrong is silent: a report sent from
// a window behind another one marks a turn read that nobody read, which is the
// failure this exists to stop. So each condition is dropped in turn and the
// report must not go. See docs/seen-design.md.
const fs = require("fs");
const path = require("path");

let bad = 0;
function fail(msg) {
  console.error("FAIL: " + msg);
  bad++;
}

const src = fs.readFileSync(path.join(__dirname, "..", "internal", "api", "web", "js", "seen.js"), "utf8");
// setInterval is swallowed: the test drives the tick by hand with its own clock.
const make = new Function("document", "WebSocket", "api", "esc", `
  const setInterval = () => 0;
  let term = null, termTask = null, termSock = null, termKind = "runner";
  let termListOpen = false, lastTasks = [];
  const termNarrow = () => false;
  const refreshSoon = () => {};
  const bareId = id => String(id).replace(/^[^~]*~/, "");
  ${src}
  return {
    seenTick, seenChips, seenBlockedBy, SEEN_DWELL_MS,
    set: o => {
      if ("term" in o) term = o.term;
      if ("termTask" in o) termTask = o.termTask;
      if ("termSock" in o) termSock = o.termSock;
      if ("termKind" in o) termKind = o.termKind;
      if ("termListOpen" in o) termListOpen = o.termListOpen;
      if ("lastTasks" in o) lastTasks = o.lastTasks;
    },
  };
`);

function world() {
  const doc = {
    visibilityState: "visible",
    focused: true,
    hasFocus() { return this.focused; },
    view: { hidden: false },
    getElementById(id) { return id === "terms" ? this.view : null; },
  };
  const posts = [];
  const api = async (p, opts) => { posts.push({ p, body: JSON.parse(opts.body) }); return {}; };
  const esc = s => String(s);
  const b = make(doc, { OPEN: 1 }, api, esc);
  const card = { id: "sg4~c1", seen: { unseen: true, turn_ended_at: "2026-09-23T12:00:00.000Z" } };
  const buffer = { active: { viewportY: 40, baseY: 40 } };
  b.set({
    term: { buffer }, termTask: { id: "sg4~c1" }, termSock: { readyState: 1 },
    lastTasks: [card],
  });
  return { b, doc, posts, card, buffer };
}

// Held for the whole dwell: one report, naming the turn it showed.
{
  const { b, posts } = world();
  for (let t = 0; t <= b.SEEN_DWELL_MS + 500; t += 500) b.seenTick(t);
  if (posts.length !== 1) fail(`a full dwell sent ${posts.length} reports, want 1`);
  else if (posts[0].p !== "/v1/tasks/sg4~c1/seen" || posts[0].body.turn_ended_at !== "2026-09-23T12:00:00.000Z") {
    fail("the report did not name the card and the turn it was showing: " + JSON.stringify(posts[0]));
  }
  // Staying on screen does not report again.
  for (let t = 5000; t <= 12000; t += 500) b.seenTick(t);
  if (posts.length !== 1) fail("a turn left on screen was reported more than once");
}

// Passing through is not reading.
{
  const { b, posts } = world();
  b.seenTick(0);
  b.seenTick(b.SEEN_DWELL_MS - 500);
  if (posts.length) fail("a turn on screen for less than the dwell was reported");
}

// Each condition, dropped halfway, must stop the report and restart the dwell.
const drops = [
  ["a hidden tab", w => { w.doc.visibilityState = "hidden"; }],
  ["an unfocused window", w => { w.doc.focused = false; }],
  ["another view", w => { w.doc.view.hidden = true; }],
  ["a shell instead of the runner", w => { w.b.set({ termKind: "shell" }); }],
  ["a socket that is not open", w => { w.b.set({ termSock: { readyState: 3 } }); }],
  ["scrolled up into the history", w => { w.buffer.active.viewportY = 10; }],
];
for (const [what, drop] of drops) {
  const w = world();
  w.b.seenTick(0);
  w.b.seenTick(1500);
  drop(w);
  if (!w.b.seenBlockedBy()) fail(`${what} is not a reason to hold the report`);
  for (let t = 2000; t <= 8000; t += 500) w.b.seenTick(t);
  if (w.posts.length) fail(`${what} still sent a report`);
}

// A turn that ends mid-dwell starts a new one: what was on screen before was
// the older turn.
{
  const { b, posts, card } = world();
  b.seenTick(0);
  b.seenTick(2500);
  card.seen = { unseen: true, turn_ended_at: "2026-09-23T12:05:00.000Z" };
  b.seenTick(3000);
  b.seenTick(3500);
  if (posts.length) fail("a turn that ended mid-dwell inherited the older turn's dwell");
  b.seenTick(6500);
  if (posts.length !== 1 || posts[0].body.turn_ended_at !== "2026-09-23T12:05:00.000Z") {
    fail("the newer turn was not reported after its own dwell");
  }
}

// A seen turn is not reported at all.
{
  const { b, posts, card } = world();
  card.seen = { unseen: false, turn_ended_at: "2026-09-23T12:00:00.000Z" };
  for (let t = 0; t <= 8000; t += 500) b.seenTick(t);
  if (posts.length) fail("a turn already seen was reported");
}

// The chips.
{
  const { b } = world();
  const none = b.seenChips({ id: "x" });
  if (none !== "") fail("a card with no seen state drew a chip");
  const unread = b.seenChips({ seen: { unseen: true } });
  if (!/chip unseen/.test(unread)) fail("an unseen turn drew no dot");
  const asked = b.seenChips({ seen: { unseen: false, answered: false, open_questions: ["a", "b", "c"] } });
  if (!/\? 3/.test(asked) || /chip unseen/.test(asked)) fail("three open questions did not draw `? 3` alone: " + asked);
  const unreadable = b.seenChips({ seen: { answered: false, questions_unparsed: true } });
  if (!/>\?</.test(unreadable.replace(/\s+/g, ""))) fail("an unreadable block did not draw a bare ?: " + unreadable);
  const answered = b.seenChips({ seen: { unseen: false, answered: true } });
  if (answered !== "") fail("an answered card still drew a chip");
}

if (bad) process.exit(1);
console.log("a turn is reported seen only after a full dwell in front of somebody.");
