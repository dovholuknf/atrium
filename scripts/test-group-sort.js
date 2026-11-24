// A launcher and its launched cards sort as one unit, on the freshest place any of them holds.
const { boardScript } = require("./board-source.js");

const page = boardScript();
const at = page.indexOf("function termGroupOrder(");
if (at < 0) { console.error("FAIL: the board has no termGroupOrder"); process.exit(1); }
const src = page.slice(at, page.indexOf("\n}\n", at) + 2);

const mk = id => ({ id });
const [kid, orch, qa, docprev, pin] = ["kid", "orch", "qa", "docprev", "pin"].map(mk);
pin.pinned = true;
const sorted = [docprev, qa, kid, orch, pin];
const kids = new Map([["orch", [kid]]]);
const top = [docprev, qa, orch];

let bad = 0;
function is(got, want, what) {
  const g = got.map(t => t.id).join(","), w = want.join(",");
  if (g !== w) { console.error(`FAIL: ${what}: expected ${w}, got ${g}`); bad++; }
}
const run = (started, activity, list) => new Function("termSortStarted", "sortByActivity", src + "return termGroupOrder;")(started, activity)(sorted, list, kids);

is(run(true, false, top), ["docprev", "qa", "orch"], "docprev and qa are fresher than the kid, so they stay on top");
sorted.splice(0, sorted.length, kid, docprev, qa, orch, pin);
is(run(true, false, top), ["orch", "docprev", "qa"], "a fresh launched card lifts its whole group");
is(run(false, true, top), ["orch", "docprev", "qa"], "the same by activity");
is(run(false, false, top), ["docprev", "qa", "orch"], "by name the order is left alone");
sorted.splice(0, sorted.length, pin, kid, docprev, qa, orch);
is(run(true, false, [pin, docprev, orch]), ["pin", "orch", "docprev"], "a pinned row keeps its own place");
if (bad) process.exit(1);
console.log("test-group-sort: ok");
