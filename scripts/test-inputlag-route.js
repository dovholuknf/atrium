// An input lag line says which room the card is in and how that room reaches the hub, because a wire number means
// nothing without it: 1ms over loopback, 85ms over an overlay.
const { boardScript } = require("./board-source.js");

let bad = 0;
function fail(msg) { console.error("FAIL: " + msg); bad++; }

const page = boardScript();

function lift(start, end) {
  const at = page.indexOf(start);
  if (at < 0) {
    console.error(`FAIL: the board no longer has \`${start}\`. Point the test at whatever replaced it.`);
    process.exit(1);
  }
  const stop = page.indexOf(end, at + start.length);
  return page.slice(at, stop < 0 ? page.length : stop + end.length);
}

const { lagRoute, lagWireText } = new Function(
  lift("function lagRoute(", "\n}") + "\n" + lift("function lagWireText(", "\n}") +
  "\nreturn { lagRoute, lagWireText };")();

const rooms = [
  { name: "claude-sg4", transport: "ziti", attached: true, via: "ziti" },
  { name: "sg3", transport: "ziti", attached: true, via: "direct", fallback: true },
  { name: "away", transport: "direct", attached: false },
];
const eq = (got, want, what) => { if (got !== want) fail(`${what}: got ${JSON.stringify(got)}, want ${JSON.stringify(want)}`); };

eq(lagRoute(rooms, { room: "claude-sg4" }), "claude-sg4 via ziti", "an attached room names the transport it is on");
eq(lagRoute(rooms, { room: "sg3" }), "sg3 via direct", "the live transport wins over the one it joined with");
eq(lagRoute(rooms, { room: "away" }), "away via direct", "a room not attached falls back to its joined transport");
eq(lagRoute(rooms, { room: "ghost" }), "ghost", "a room the list lacks is named alone");
eq(lagRoute(rooms, { room: "" }), "", "a one-room board has no hop to name");
eq(lagRoute(rooms, null), "", "no card, no route");
eq(lagRoute(undefined, { room: "sg3" }), "sg3", "no room list yet");

eq(lagWireText({ sent: 100, echo: 187, route: "claude-sg4 via ziti" }), "wire 87.0ms (claude-sg4 via ziti)", "wire text");
eq(lagWireText({ sent: 100, echo: 101, route: "" }), "wire 1.0ms", "wire text on a one-room board");

if (!/last \$\{s\.length\} keys\$\{lagRoutes \?/.test(page)) fail("the p50/p95 summary line does not carry the route");

if (bad) process.exit(1);
console.log("an input lag line names the room and its transport.");
