// A closed PR's row on the pulls tab reads closed, with when, has no actions, and leaves the "open" filter.
// Runs js/pulls.js in a vm with the few board globals it reads. No browser: node scripts/check-pulls-closed.js
const fs = require("fs");
const path = require("path");
const vm = require("vm");

const src = fs.readFileSync(path.join(__dirname, "..", "internal", "api", "web", "js", "pulls.js"), "utf8");
const ctx = vm.createContext({
  esc: s => String(s), diskLabel: () => "", ago: s => Math.round(s / 60) + "m", prMoveOffered: () => true,
  document: { getElementById: () => null, querySelector: () => null, addEventListener: () => {} }, Date, JSON, Math, Set, Array, Number, isNaN, Object, String,
});
vm.runInContext(src + "\nthis.pulls = pulls; this.pullRowHTML = pullRowHTML; this.pullsShown = pullsShown;" +
  "this.pullPill = pullPill;", ctx);

let failed = 0;
const fail = m => { failed++; console.error("FAIL: " + m); };

const ago10 = new Date(Date.now() - 10 * 60 * 1000).toISOString();
const open = { id: "a", org_repo: "o/r", number: 966, state: "failed", run_error: "panel: x", created_at: ago10, walker_task: "c1" };
const closed = Object.assign({}, open, { id: "b", number: 967, archived_at: ago10 });
ctx.pulls.rows = [open, closed];

const html = ctx.pullRowHTML(closed);
if (ctx.pullPill(closed).text !== "closed") fail("pill: " + ctx.pullPill(closed).text);
if (/failed|panel/.test(html)) fail("a closed row still says failed: " + html);
if (!/closed 10m ago/.test(html)) fail("a closed row does not say when: " + html);
for (const act of ["retry", "log", "move", "close", "walk", "abort", "start", "open", "menu"]) {
  if (new RegExp('data-act="' + act + '"').test(html)) fail("a closed row still offers " + act);
}
const openHtml = ctx.pullRowHTML(open);
if (!/failed at panel/.test(openHtml) || !/data-act="retry"/.test(openHtml)) fail("an open failed row changed: " + openHtml);

ctx.pulls.filterState = "open";
const ids = ctx.pullsShown().map(r => r.id).join();
if (ids !== "a") fail("the open filter shows " + ids);
ctx.pulls.filterState = "all";
if (ctx.pullsShown().map(r => r.id).join() !== "a,b") fail("the all filter does not show both");

if (failed) process.exit(1);
console.log("pulls closed: ok");
