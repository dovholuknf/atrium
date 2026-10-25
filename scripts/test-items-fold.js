// The Items tab's row logic, RUN against stubs: foldItems (one row per backlog id), itemNeedsYou, and the steps that decide idle.
// Pure functions in internal/api/web/js/items-data.js, so no browser. See docs/ui/items-tab-ux-pass.md sections 3 and 4.
const fs = require("fs");
const path = require("path");
const vm = require("vm");

let bad = 0;
function fail(msg) { console.error("FAIL: " + msg); bad++; }

const src = fs.readFileSync(path.join(__dirname, "..", "internal", "api", "web", "js", "items-data.js"), "utf8");
const ctx = { location: { search: "" }, document: { readyState: "complete", addEventListener() {}, getElementById() { return null; } }, window: {}, console };
vm.createContext(ctx);
vm.runInContext(src + "\nthis.api = { foldItems, itemNeedsYou, itemFromHub, itemWithWork, itemsStatsFrom };", ctx);
const { foldItems, itemNeedsYou, itemFromHub, itemWithWork, itemsStatsFrom } = ctx.api;

const NAMES = ["designed", "approved", "building", "tested", "reviewed", "landed", "deployed", "pushed"];
const ago = m => new Date(Date.now() - m * 60000).toISOString();
const work = (at, o) => ({ steps: NAMES.map((s, i) => ({ step: s, state: o && o.all ? "done" : i < at ? "done" : i === at ? "active" : "pending",
  party: o && o.approval && s === "approved" ? "human" : "", cards: "[]", started_at: ago(500) })) });
// One hub assignment, as the item the board makes of it.
const asg = (n, backlog, card, state, o) => itemFromHub(Object.assign({ item_id: "a" + n, work_id: backlog, room: "sg4", card, state, owner_kind: "session",
  owner: "orch@sg4", note: "", created_at: ago(600), updated_at: ago(n) }, o));
const row = (a, w) => itemWithWork(a, w);
const tasks = ids => ids.map(id => ({ id, room: "sg4" }));
const one = (list, tk) => foldItems(list, tk)[0];

// ONE ROW PER BACKLOG ID: four assignments fold into one, and the driver is the newest live card.
let r = foldItems([1, 2, 3, 4].map(n => row(asg(n, "b1", "c" + n, n === 3 ? "running" : "blocked"), work(2))), tasks(["c3"]));
if (r.length !== 1) fail("four assignments made " + r.length + " rows");
if (r[0].card !== "c3" || r[0].fold.live !== 1 || r[0].fold.ended !== 3) fail("driver/cards: " + JSON.stringify([r[0].card, r[0].fold.live, r[0].fold.ended]));
if (r[0].fold.group !== "working") fail("a live card on its step is " + r[0].fold.group);
if (foldItems([asg(1, "x", "c1", "running"), asg(2, "y", "c2", "running")], null).length !== 2) fail("two backlog ids did not stay two rows");

// ASK TEXT: a human-owned block with a question needs you, with none it does not, and a session-owned block is not clint's.
const blocked = (note, kind) => row(asg(1, "b2", "c1", "blocked", { owner_kind: kind, owner: kind === "human" ? "clint" : "orch@sg4", note }), work(2));
if (one([blocked("which key?", "human")], tasks(["c1"])).fold.group !== "needs-you") fail("a human question did not need you");
if (!itemNeedsYou(one([blocked("which key?", "human")], tasks(["c1"])))) fail("itemNeedsYou false for a human question");
for (const [note, kind] of [["", "human"], ["   ", "human"], ["a question", "session"], ["", "session"]]) {
  const x = one([blocked(note, kind)], tasks(["c1"]));
  if (itemNeedsYou(x)) fail("itemNeedsYou true for " + JSON.stringify([note, kind]));
  if (x.fold.group !== "working" || !x.fold.waitingOn) fail("a block nobody can answer is " + x.fold.group + " waiting on " + x.fold.waitingOn);
}
if (one([blocked("", "session")], tasks([])).fold.group !== "idle") fail("a block with its card gone is not idle");

// THE APPROVAL GATE needs you with no note at all.
const g3 = itemWithWork(asg(1, "b3", "c1", "running"), { steps: work(1, { approval: true }).steps.map(s => Object.assign({}, s, { state: s.step === "approved" ? "active" : s.state })) });
if (!itemNeedsYou(one([g3], tasks(["c1"])))) fail("a design waiting on approval does not need you");
// The header count is the rows that need you, on a mix of all three.
const mix = foldItems([blocked("q", "human"), Object.assign(blocked("q", "session"), { backlog: "m2", id: "m2" }), Object.assign(g3, { backlog: "m3", id: "m3" })], tasks(["c1"]));
if (itemsStatsFrom(mix).needs_you !== mix.filter(itemNeedsYou).length || mix.filter(itemNeedsYou).length !== 2) fail("the count is " + itemsStatsFrom(mix).needs_you + " for " + mix.filter(itemNeedsYou).length);

// FINISHED: all eight done. A landed item with every card ended is NOT finished, it is at deployed.
if (one([row(asg(1, "b4", "c1", "finished"), work(0, { all: true }))], tasks([])).fold.group !== "finished") fail("all steps done is not finished");
const landed = one([row(asg(1, "b5", "c1", "finished"), work(6))], tasks([]));
if (landed.fold.group !== "working" || !landed.fold.noCard) fail("landed with ended cards is " + landed.fold.group + ", want working with no card needed");
const pushing = one([row(asg(1, "b5", "c1", "finished"), work(7))], tasks([]));
if (pushing.fold.group !== "working") fail("waiting to push is " + pushing.fold.group);
const cancelled = one([asg(1, "b6", "c1", "finished", { note: "cancelled by clint" })], tasks([]));
if (cancelled.fold.group !== "finished" || !cancelled.fold.cancelled) fail("a cancelled item is " + cancelled.fold.group);

// A pushed item is finished though an earlier step was left open, and so is a backlog item marked done or dropped.
const leftOpen = at => ({ steps: work(0, { all: true }).steps.map(s => Object.assign({}, s, { state: s.step === "reviewed" ? "pending" : s.step === "tested" ? "stale" : s.state })), work: at });
if (one([row(asg(1, "b12", "c1", "running"), leftOpen())], tasks([])).fold.group !== "finished") fail("a pushed item with an open earlier step is not finished");
for (const st of ["done", "dropped"]) {
  if (one([row(asg(1, "b13", "c1", "running"), Object.assign(work(2), { work: { status: st } }))], tasks([])).fold.group !== "finished") fail("backlog " + st + " is not finished");
}

// QUEUED: only when none ever started. An ended card means it is not.
if (one([asg(1, "b7", "", "queued")], tasks([])).fold.group !== "queued") fail("a queued item is not queued");
const q2 = one([asg(1, "b7", "", "queued"), asg(2, "b7", "c2", "finished")], tasks([]));
if (q2.fold.group === "queued") fail("an item with an ended card is queued");

// IDLE: a card step with no live card. Approved, deployed and pushed have no card, so they are never idle.
const idle = one([row(asg(1, "b8", "c1", "running"), work(2))], tasks([]));
if (idle.fold.group !== "idle") fail("an unattended building step is " + idle.fold.group);
if (itemNeedsYou(idle)) fail("an idle row needs you");
for (const at of [1, 6, 7]) {
  const x = one([row(asg(1, "b9", "c1", "running"), work(at))], tasks([]));
  if (x.fold.group === "idle") fail("step " + at + " is idle");
}
// A card on an offline room is unknown, not idle.
const off = one([row(asg(1, "b10", "c1", "running"), work(2))], [{ id: "c1", room: "sg4", offline: true }]);
if (off.fold.group !== "working" || off.fold.offline !== 1) fail("an offline card is " + off.fold.group);
// The board has no card list yet (null): a running assignment is not called idle.
if (one([row(asg(1, "b11", "c1", "running"), work(2))], null).fold.group !== "working") fail("an unknown card list made a row idle");

if (!bad) console.log("test-items-fold ok");
process.exit(bad ? 1 : 0);
