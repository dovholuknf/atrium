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

let bad = 0;
function is(got, want, what) {
  if (got !== want) { console.error(`FAIL: ${what}: expected ${want}, got ${got}`); bad++; }
}

const orch = { id: "sg4~7", wire_name: "ogorch" };
const other = { id: "m1~7", wire_name: "ogorch" };
const kid = { id: "sg4~9", spawned_by: "ogorch" };
const cards = [other, orch, kid];
is(launcherOf(kid, cards), orch, "a bare spawned_by names a launcher on the child's own room");
is(launcherOf({ id: "sg4~9", spawned_by: "ogorch@m1" }, cards), other, "a qualified spawned_by still names its own room");
is(launcherOf({ id: "sg4~9", spawned_by: "nobody" }, cards), null, "a bare name nobody holds nests nowhere");

if (bad) process.exit(1);
console.log("ok: launcherOf same room");
