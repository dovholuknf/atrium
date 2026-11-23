// The board's `move to room`, RUN against stubs for the board globals: which rooms the row offers, the chip of a card
// that moved, and the step list a move draws as the driver reports it. See internal/api/web/js/roommove.js.
const fs = require("fs");
const path = require("path");

let bad = 0;
function fail(msg) {
  console.error("FAIL: " + msg);
  bad++;
}

const src = fs.readFileSync(path.join(__dirname, "..", "internal", "api", "web", "js", "roommove.js"), "utf8");
const make = new Function("rooms", "inventory", "tasks", `
  const esc = s => (s == null ? "" : String(s)).replace(/[&<>"]/g, c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" })[c]);
  const prMoveRooms = () => rooms;
  const hubInventory = inventory, lastTasks = tasks;
  ${src}
  return { roomMoveMenuItem, roomMoveChip, roomMoveList };
`);

const rooms = [{ name: "sgg" }, { name: "sg4" }, { name: "sgw" }];
const inv = [{ name: "sgg", transport: "ziti" }, { name: "sg4", transport: "ziti" }, { name: "laptop", transport: "ziti" },
  { name: "here", transport: "local" }];
const tasks = [{ room: "sg4" }, { room: "sg4" }, { room: "sgg" }];
const m = make(rooms, inv, tasks);

const it = m.roomMoveMenuItem({ id: "sgg~c1", room: "sgg" });
if (!it || it.label !== "move to room" || !it.help) fail("no move row: " + JSON.stringify(it));
else {
  const picks = it.sub.filter(s => s.label).map(s => s.label + "/" + s.tag);
  if (picks.join() !== "sg4/2 cards,sgw/0 cards") fail("rooms offered " + picks.join());
  const quiet = it.sub.filter(s => s.quiet).map(s => s.quiet);
  if (quiet.join() !== "laptop is offline") fail("offline rooms " + quiet.join());
  if (!it.sub[0].head) fail("no rooms heading");
}
if (m.roomMoveMenuItem({ id: "sgg~c1", room: "sgg", moved_to: "sg4~c2" })) fail("a moved card offers a move");
if (m.roomMoveMenuItem({ id: "c1", room: "" })) fail("a card on no room offers a move");
if (make([{ name: "sgg" }], [], []).roomMoveMenuItem({ id: "sgg~c1", room: "sgg" })) fail("one room offers a move");

if (!/on sg4</.test(m.roomMoveChip({ moved_to: "sg4~c2" }))) fail("chip " + m.roomMoveChip({ moved_to: "sg4~c2" }));
if (m.roomMoveChip({})) fail("a card that did not move has a chip");

const run = { t: { room: "sgg" }, to: "sg4", last: "worktree" };
const marks = html => [...html.matchAll(/mv-mark ?([a-z]*)/g)].map(x => x[1] || "-").join(" ");
if (marks(m.roomMoveList(run, -1)) !== "ok ok ok run - - -") fail("running " + marks(m.roomMoveList(run, -1)));
if (marks(m.roomMoveList(run, 1)) !== "ok ok ok fail - - -") fail("failed " + marks(m.roomMoveList(run, 1)));
if (!/start<span class="val">sg4/.test(m.roomMoveList(run, -1))) fail("the start step does not name the room it is on");

if (bad) {
  console.error(bad + " failure(s)");
  process.exit(1);
}
console.log("room move: ok");
