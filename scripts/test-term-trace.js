// The terminal trace ring: bounded by bytes, both directions recorded, and a
// saved file that round-trips the exact output bytes.
//
// The ring is the evidence for a garble nobody can reproduce on demand, so the
// two ways it can fail silently are the ones asserted: it grows without bound
// on a busy terminal, or it saves something other than the bytes xterm was
// handed. Runs the real js/termtrace.js against stubs for the board globals.
const fs = require("fs");
const path = require("path");

let bad = 0;
function fail(msg) {
  console.error("FAIL: " + msg);
  bad++;
}

const src = fs.readFileSync(path.join(__dirname, "..", "internal", "api", "web", "js", "termtrace.js"), "utf8");
const run = new Function("performance", "btoa", "navigator", `
  let term = null, termTask = { id: "01abc-task", title: "t" }, termKind = "runner";
  ${src}
  return {
    traceTerm, traceOut, traceIn, traceCtl, traceSnapshot,
    size: () => traceSize, log: () => traceLog, max: TRACE_BYTES,
    setTerm: t => { term = t; },
  };
`);
let now = 0;
const api = run({ now: () => now++ }, s => Buffer.from(s, "latin1").toString("base64"),
  { userAgent: "test" });

let resized = null;
const fakeTerm = {
  cols: 80, rows: 24,
  onResize: fn => { resized = fn; },
  buffer: { active: { cursorX: 2, cursorY: 5, baseY: 0, viewportY: 0,
    getLine: y => ({ translateToString: () => "row" + y }) } },
};
api.setTerm(fakeTerm);
api.traceTerm(fakeTerm);

const chunk = new Uint8Array([0x1b, 0x5b, 0x41, 0x00, 0xff, 0x68, 0x69]).buffer;
api.traceOut(chunk);
api.traceIn('{"t":"in","d":"\\u001b[3;5~"}');
resized({ cols: 100, rows: 30 });

const snap = api.traceSnapshot();
const kinds = snap.frames.map(f => f.d).join(",");
if (kinds !== "ctl,out,in,ctl") fail(`expected ctl,out,in,ctl frames, got ${kinds}`);
const out = snap.frames.find(f => f.d === "out");
if (!out || !Buffer.from(out.b64, "base64").equals(Buffer.from(new Uint8Array(chunk)))) {
  fail("the saved output frame is not the exact bytes the terminal was handed");
}
if (!/"xterm-resize".*"cols":100/.test(snap.frames[3].s)) fail("a local xterm resize was not recorded");
if (snap.cursor.y !== 5 || snap.screen.length !== 24) fail("the snapshot lost the screen or cursor");

// A busy terminal: far more than the window, in frames of several sizes.
for (let i = 0; i < 2000; i++) api.traceOut(new Uint8Array(100 + (i % 7) * 50).buffer);
if (api.size() > api.max) fail(`the ring holds ${api.size()} bytes, over its ${api.max} cap`);
if (api.size() < api.max - 500) fail(`the ring dropped more than it had to: ${api.size()} of ${api.max}`);
const sum = api.log().reduce((n, e) => n + (e.data.byteLength != null ? e.data.byteLength : e.data.length), 0);
if (sum !== api.size()) fail(`the ring's byte count drifted: counts ${api.size()}, holds ${sum}`);

// A new terminal starts a new window.
api.traceTerm(fakeTerm);
if (api.log().length !== 1) fail("a new terminal kept the previous terminal's trace");

if (bad) process.exit(1);
console.log("the terminal trace ring is bounded and saves exact bytes.");
