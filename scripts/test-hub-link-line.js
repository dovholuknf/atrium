// A room that cannot dial its hub says so on its own Rooms pane, with the address it tried.
// The hub never hears of a dial that did not arrive, so this line is the only place that is said.
const { boardScript } = require("./board-source.js");

let bad = 0;
function fail(msg) { console.error("FAIL: " + msg); bad++; }

const page = boardScript();

function lift(start, end) {
  const at = page.indexOf(start);
  if (at < 0) {
    console.error(`FAIL: the board no longer has \`${start}\`. The line this test protects was renamed or ` +
      `moved. Point the test at whatever replaced it.`);
    process.exit(1);
  }
  const stop = page.indexOf(end, at + start.length);
  return page.slice(at, stop < 0 ? page.length : stop + end.length);
}

const esc = s => String(s).replace(/[&<>"]/g, c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c]));
const { hubLinkLine, thisMachineRow } = new Function("esc",
  lift("function hubLinkLine(", "\n}") + "\n" + lift("function thisMachineRow(", "\n}") +
  "\nreturn { hubLinkLine, thisMachineRow };")(esc);

const down = { up: false, hub: "192.168.1.68:7779", error: "direct 192.168.1.68:7779: no answer within 5s" };
const line = hubLinkLine(down);
if (!line.includes("192.168.1.68:7779")) fail("the line does not name the address it tried: " + line);
if (!line.includes("no answer within 5s")) fail("the line does not say the dial timed out: " + line);
if (!line.includes("atrium room hub")) fail("the line does not say how to change the address: " + line);
if (!thisMachineRow(down).includes("hublink-down")) fail("the room's own pane does not carry the line");

const lost = { up: false, room: "sgg", hub: "sg4:7779", error: "every rung failed",
  unreachable: ["direct sg4:7779", "direct 192.168.1.68:7779", "ziti atrium-hub"] };
const all = hubLinkLine(lost);
if (!all.includes("cannot reach the hub at any known address")) fail("a room no rung reaches does not say so: " + all);
if (!all.includes("sgg")) fail("the line does not name the room: " + all);
for (const a of lost.unreachable) if (!all.includes(a)) fail(`the line does not name ${a}: ` + all);
if (!all.includes("atrium room hub")) fail("the line does not give the one-line fix: " + all);

if (hubLinkLine({ up: true, hub: "10.0.0.1:7779" }) !== "") fail("an attached room drew a line");
if (hubLinkLine(null) !== "") fail("a plain daemon with no hub drew a line");
if (thisMachineRow(null).includes("hublink-down")) fail("a plain daemon's pane carries the line");
if (!hubLinkLine({ up: false, hub: "<b>x</b>:1", error: "<i>" }).includes("&lt;b&gt;")) fail("the address is not escaped");

if (bad) process.exit(1);
console.log("a room that cannot reach its hub says so, with the address it tried.");
