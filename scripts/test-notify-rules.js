// The turn-end notification rules, RUN against stubs for the board globals.
//
// (1) A card's turn that is fully over, with the agent idle, is a ready alert for a card the operator is not on. That
//     includes an agent-launched card that ended its turn with no question (issue-20674@claude-sg4, 2026-10-13: the tray
//     stayed empty because `waitingRow` drops those). A card with a child still running is not over.
// (2) A card the operator is typing in or watching never alerts, even in the instant its pane is rebuilt under them.
const fs = require("fs");
const path = require("path");

let bad = 0;
function fail(msg) {
  console.error("FAIL: " + msg);
  bad++;
}

const js = f => fs.readFileSync(path.join(__dirname, "..", "internal", "api", "web", "js", f), "utf8");
// Only the functions under test are cut out of cardrules.js and cards.js: the rest of those files wants a page.
function fn(src, name) {
  const at = src.indexOf("function " + name + "(");
  if (at < 0) throw new Error("no function " + name);
  let depth = 0;
  for (let i = src.indexOf("{", at); i < src.length; i++) {
    if (src[i] === "{") depth++;
    if (src[i] === "}" && --depth === 0) return src.slice(at, i + 1);
  }
  throw new Error("unterminated " + name);
}
const rules = js("cardrules.js"), cards = js("cards.js");
const make = new Function(`
  const bareId = id => String(id).replace(/^[^~]*~/, "");
  const sameCard = (a, b) => !!a && !!b && bareId(a) === bareId(b);
  let term = null, termTask = null;
  const NO_CONTACT_AFTER_SECONDS = 20 * 60, LIVE_STATUSES = ["running", "needs-input", "needs-permission"];
  const cardIdleAge = () => 0;
  ${fn(rules, "runnerGone")}
  ${fn(rules, "cardWaiting")}
  ${fn(rules, "cardLive")}
  ${fn(rules, "cardState")}
  const isWaiting = cardWaiting, staleActivity = t => !t || t.status === "dead" || (!t.supervised && ["done", "dead", "shelved"].includes(t.status));
  ${fn(rules, "workingNow")}
  ${fn(cards, "waitingRow")}
  ${fn(cards, "hasRunningChild")}
  ${fn(cards, "cardsReady")}
  ${fn(cards, "cardsWaiting")}
  let cardList = () => [];
  ${js("engaged.js")}
  return {
    cardState, waitingRow, hasRunningChild, cardsReady, cardsWaiting, noteEngaged, engagedWith,
    setCards: l => { cardList = () => l; },
    set: o => { if ("term" in o) term = o.term; if ("termTask" in o) termTask = o.termTask; },
  };
`);

const b = make();
const row = o => Object.assign({ id: "sg4~c", status: "needs-input", wire_name: "w", tags: [] }, o);

// (1) the worker that finished
{
  const worker = row({ id: "sg4~issue-20674", tags: ["origin:agent", "atrium:subagent"], waiting_since: "T1" });
  const mine = row({ id: "sg4~mine", waiting_since: "T0" });
  b.setCards([worker, mine]);
  if (!b.waitingRow(worker)) fail("a running agent-launched card waiting on the operator is not waiting: grey means not running, nothing else");
  const gone = row({ id: "sg4~gone", tags: ["origin:agent"], supervised: false });
  if (b.waitingRow(gone)) fail("a card whose runner is gone (supervised false, no pid) counts as waiting");
  if (!b.waitingRow(row({ supervised: false, pid: 42 }))) fail("a joined terminal with a pid is not waiting");
  const ids = b.cardsReady().map(t => t.id);
  if (!ids.includes("sg4~issue-20674")) fail("an agent-launched card that ended its turn is not in the ready set: " + ids);
  if (ids.join() !== "sg4~mine,sg4~issue-20674") fail("the ready set is not oldest wait first: " + ids);
  if (b.cardsWaiting().length !== 2) fail("cardsWaiting must count the agent-launched card waiting on the operator");
  for (const gone of [{ status: "running" }, { archived_at: "x" }, { offline: true }, { wire_name: "" }]) {
    if (b.cardsReady([row(gone)]).length) fail("a card that is not at rest is ready: " + JSON.stringify(gone));
  }
  // a parent whose child still runs is not done: the alert filter reads hasRunningChild over the whole list
  const child = row({ id: "sg4~kid", status: "running", spawned_by_id: "sg4~mine" });
  if (!b.hasRunningChild(mine, [mine, child])) fail("a card with a running child is not held back");
  if (b.hasRunningChild(mine, [mine, row({ id: "sg4~kid", status: "needs-input", spawned_by_id: "sg4~mine" })])) {
    fail("a card whose child has ended is still held back");
  }
}

// (1b) cardState: one word per card, grey is "off" and nothing else (docs/rnd/unified-card.md)
{
  const st = o => b.cardState(row(o));
  const want = (what, got, exp) => { if (got !== exp) fail("cardState: " + what + " is " + got + ", want " + exp); };
  want("a ready card with a runner", st({ supervised: true }), "waiting");
  want("a ready agent-launched card", st({ supervised: true, tags: ["origin:agent"] }), "waiting");
  want("a permission card", st({ status: "needs-permission", supervised: true }), "waiting");
  want("a ready card whose runner is gone", st({ supervised: false }), "off");
  want("a mid-turn card", st({ status: "running", supervised: true, activity: { what: "thinking" } }), "working");
  want("a quiet running card", st({ status: "running", supervised: true }), "live");
  want("a starting card", st({ status: "running", supervised: false, starting: true }), "live");
  want("a parked card", st({ supervised: false, parked_at: "x" }), "off");
  for (const s of ["shelved", "done", "dead", "backlog"]) want("a " + s + " card", st({ status: s }), "off");
  want("an offline card", st({ supervised: true, offline: true }), "unreachable");
}

// (2) the card you are typing in
{
  const T = 1000000;
  if (b.engagedWith("sg4~c", T)) fail("a card never typed in reads engaged");
  b.noteEngaged("sg4~c", T);
  b.set({ term: null, termTask: null });
  if (!b.engagedWith("room~c", T + 5000)) fail("a card typed in 5s ago, with its pane being rebuilt, does not read engaged");
  if (b.engagedWith("sg4~c", T + 21000)) fail("the engagement did not expire");
  if (b.engagedWith("sg4~other", T + 1000)) fail("another card reads engaged");
  b.set({ term: {}, termTask: { id: "sg4~other" } });
  if (b.engagedWith("sg4~c", T + 1000)) fail("a card reads engaged while another card's terminal is attached");
  b.set({ term: {}, termTask: { id: "sg4~c" } });
  if (!b.engagedWith("sg4~c", T + 1000)) fail("its own terminal attached does not read engaged");
}

// The wiring, since these two are only worth anything if the page calls them.
{
  const notify = js("notify.js"), spine = js("settings-spine.js"), links = js("terminal-links.js"), growl = js("growl.js");
  if (!/alerting\.check\("waiting", ready\.filter/.test(spine)) fail("the ready alert does not read cardsReady");
  if (!/engagedWith\(id\)/.test(notify) || !/noteEngaged\(id\)/.test(notify)) fail("readySilenced does not use the engaged card");
  if (!/noteEngaged\(termTask && termTask\.id\)/.test(links)) fail("typing in a terminal does not note the card");
  if (!/if \(seed\) next\.forEach\(g => \{ if \(g\.state === "open" && growlOnIt\(g\)\)/.test(growl)) {
    fail("the first set a page hears does not hide a question for the card you are on");
  }
  if (!/src="\/js\/engaged\.js"/.test(fs.readFileSync(path.join(__dirname, "..", "internal", "api", "web", "index.html"), "utf8"))) {
    fail("engaged.js is not loaded by the page");
  }
}

if (bad) process.exit(1);
console.log("notify rules ok");
