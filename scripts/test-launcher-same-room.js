// A worker launched on its launcher's own room records `spawned_by` as a bare name, and nests under that launcher.
const { boardScript } = require("./board-source.js");
const page = boardScript();

function liftFn(name) {
  const at = page.indexOf(`function ${name}(`);
  if (at < 0) { console.error(`FAIL: the board has no ${name}`); process.exit(1); }
  let depth = 0, seen = false;
  for (let i = at; i < page.length; i++) {
    if (page[i] === "{") { depth++; seen = true; }
    else if (page[i] === "}") { depth--; if (seen && depth === 0) return page.slice(at, i + 1); }
  }
  console.error(`FAIL: ${name}'s braces do not close`);
  process.exit(1);
}

const launcherOf = new Function(["bareId", "roomOf", "launcherOf"].map(n => liftFn(n)).join("\n") + "\nreturn launcherOf;")();
const termNest = new Function(page.match(/^const (SUBAGENT|INTERVIEW|RESIDENT|DIRECTOR)_TAG = .*$/gm).join("\n") + "\n" +
  ["bareId", "roomOf", "launcherOf", "isSubagent", "isInterview", "nestsUnderLauncher", "termNest"].map(n => liftFn(n)).join("\n") +
  "\nreturn termNest;")();

let bad = 0;
function is(got, want, what) {
  if (got !== want) { console.error(`FAIL: ${what}: expected ${want}, got ${got}`); bad++; }
}

const orch = { id: "room-e~7", wire_name: "ogorch" };
const other = { id: "m1~7", wire_name: "ogorch" };
const kid = { id: "room-e~9", spawned_by: "ogorch" };
const cards = [other, orch, kid];
is(launcherOf(kid, cards), orch, "a bare spawned_by names a launcher on the child's own room");
is(launcherOf({ id: "room-e~9", spawned_by: "ogorch@m1" }, cards), other, "a qualified spawned_by still names its own room");
is(launcherOf({ id: "room-e~9", spawned_by: "nobody" }, cards), null, "a bare name nobody holds nests nowhere");

const orch2 = { id: "room-e-control~01a0f2da", wire_name: "room-e-control/orchestrator-room-e-control", tags: ["origin:agent"] };
const resident = { id: "room-d~01a1dd05", wire_name: "room-d/launch-runner-picker", spawned_by: "orchestrator-room-e-control@room-e-control",
  spawned_by_id: "room-e-control~01a0f2da", tags: ["atrium:item:u-launch-runner-picker", "atrium:lean", "atrium:resident", "origin:agent"] };
const nest = termNest([orch2, resident]);
is((nest.kids.get(orch2.id) || []).map(x => x.id).join(), resident.id, "a resident launched on another room nests under its launcher");
const director = Object.assign({}, resident, { id: "room-d~d1", tags: ["atrium:director", "atrium:resident", "origin:agent"] });
is(termNest([orch2, director]).nested.has(director.id), false, "a director stays a top row");
const untagged = { id: "room-d~u1", spawned_by: "orchestrator-room-e-control@room-e-control", tags: [] };
is(termNest([orch2, untagged]).nested.has(untagged.id), true, "an untagged card with a launcher, a CLI launch or a re-parent, nests");
is(termNest([orch2, { id: "room-d~h1", spawned_by: "@human", tags: [] }]).nested.size, 0, "a card the human launched stays a top row");

if (bad) process.exit(1);
console.log("ok: launcherOf same room");
