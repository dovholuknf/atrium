// The age groups follow activity in every sort mode, and only the order inside a group follows the sort.
const { boardScript } = require("./board-source.js");

const page = boardScript();
function lift(name) {
  const at = page.indexOf(`function ${name}(`);
  if (at < 0) { console.error(`FAIL: the board has no ${name}`); process.exit(1); }
  const end = page.indexOf("\nfunction ", at + 1);
  return page.slice(at, end < 0 ? page.length : end);
}

const ago = ms => new Date(Date.now() - ms).toISOString();
const H = 3600000, DAY = 24 * H;
const card = (id, started) => ({ id, display_title: id, created_at: ago(started), last_activity_at: ago(started) });
const parent = card("orch", 3 * DAY);
const kidA = card("c-two-days", 2 * DAY);
const kidB = card("b-five-hours", 5 * H);
const kidC = card("a-ten-minutes", 10 * 60000);
const kids = [kidA, kidB, kidC];
const solo = card("solo", 30 * 60000);
const all = [parent, solo, ...kids];

const src = `var termKids = new Map([["orch", kids]]);
var termSortStarted = mode === "started", sortByActivity = mode === "activity";
var RECENCY_ORDER = ["1h", "4h", "today", "yesterday", "this week", "this month", "dormant"];
var termAgeOrder = () => true;
var workingNow = () => false, isWaiting = () => false, terminalLabel = t => t.id;
${lift("treeActivityAt")}
${lift("recencyBucket")}
${lift("cardTieBreak")}
${lift("cardIdleSeconds")}
${lift("cardWaitSeconds")}
${lift("cardActivityCmp")}
${lift("termOrder")}
${lift("termGroupOrder")}
return { recencyBucket, termOrder, termGroupOrder };`;

let bad = 0;
function is(got, want, what) {
  if (JSON.stringify(got) !== JSON.stringify(want)) { console.error(`FAIL: ${what}: expected ${JSON.stringify(want)}, got ${JSON.stringify(got)}`); bad++; }
}
const want = {
  started: ["a-ten-minutes", "b-five-hours", "c-two-days"],
  activity: ["a-ten-minutes", "b-five-hours", "c-two-days"],
  name: ["a-ten-minutes", "b-five-hours", "c-two-days"],
};
for (const mode of ["started", "activity", "name"]) {
  const { recencyBucket, termOrder, termGroupOrder } = new Function("mode", "kids", src)(mode, kids);
  const sorted = termOrder([...all].reverse());
  const top = termGroupOrder(sorted, [solo, parent], new Map([["orch", kids]]));
  if (mode === "started") is(top.map(t => t.id), ["solo", "orch"], "started: a parent places by its own start, not its newest child");
  is(sorted.filter(t => kids.includes(t)).map(t => t.id), want[mode], `${mode}: the children's order`);
  const old = { id: "old", created_at: ago(40 * DAY), last_activity_at: ago(60000) };
  is(recencyBucket(old), "1h", `${mode}: a running card started a month ago stays in the 1h group`);
  is(recencyBucket(parent), "1h", `${mode}: a parent files by its newest descendant's activity`);
}
if (bad) process.exit(1);
console.log("test-nested-sort: ok");
