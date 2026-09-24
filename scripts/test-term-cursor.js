// The cursor for a runner whose profile holds it: never painted at a cell the
// runner was only drawing, and back at the prompt once output settles.
//
// Runs the real js/termcursor.js against a fake terminal that paints after
// every chunk, which is the worst case the board can hit: the browser paints
// between two websocket messages. The chunks are the shape measured on a live
// codex card through ConPTY, two writes per frame:
//
//   1. ?2026h ?25l <dots drawn across rows 27-29> ?25h ?2026l
//      the cursor is SHOWN where the last dot was drawn
//   2. ?25l <more dots> CUP 28;3 ?25h
//      a couple of milliseconds later, back at the prompt
//
// Claude's frames end `CUP <prompt> ?25h`, and a runner with no settle must be
// written through unchanged.
const fs = require("fs");
const path = require("path");

let bad = 0;
function fail(msg) {
  console.error("FAIL: " + msg);
  bad++;
}

// Fake timers, so the settle is deterministic.
let now = 0;
let timers = [];
function setTimeoutFake(fn, ms) {
  const id = timers.length + 1;
  timers.push({ id, at: now + ms, fn });
  return id;
}
function clearTimeoutFake(id) { timers = timers.filter(t => t.id !== id); }
function advance(ms) {
  now += ms;
  for (;;) {
    const due = timers.filter(t => t.at <= now).sort((a, b) => a.at - b.at)[0];
    if (!due) break;
    timers = timers.filter(t => t !== due);
    due.fn();
  }
}

// A terminal that knows where its cursor is and whether it shows, which is all
// the cursor needs. Every write is parsed in order; `paint` is what the eye
// sees at that instant.
function fakeTerm() {
  const st = { row: 1, col: 1, shown: true, writes: 0 };
  const re = /\x1b\[(\??[0-9;]*)([ A-Za-z@`]+?)|\x1b.|[\x00-\x1f]|[\s\S]/g;
  return {
    st,
    write(data, done) {
      const s = typeof data === "string" ? data : Buffer.from(data).toString("latin1");
      st.writes++;
      let m;
      re.lastIndex = 0;
      while ((m = re.exec(s))) {
        if (m[2]) {
          const p = m[1], f = m[2].trim();
          if (f === "H" && !p.startsWith("?")) {
            const [r, c] = p.split(";");
            st.row = +r || 1; st.col = +c || 1;
          } else if (f === "C") st.col += +p || 1;
          else if (p === "?25") st.shown = f === "h";
          continue;
        }
        const t = m[0];
        if (t === "\r") st.col = 1;
        else if (t === "\n") st.row++;
        else if (t >= " " && t[0] !== "\x1b") st.col++;
      }
      if (done) done();
    },
    paint() { return st.shown ? `${st.row};${st.col}` : "hidden"; },
  };
}

function load(caps, term) {
  const src = fs.readFileSync(path.join(__dirname, "..", "internal", "api", "web", "js", "termcursor.js"), "utf8");
  const mod = new Function("termCaps", "term", "setTimeout", "clearTimeout", `
    ${src}
    return { writeRunnerOutput, cursorReset, lastCursorMode };
  `);
  return mod(caps, term, setTimeoutFake, clearTimeoutFake);
}

const enc = s => new Uint8Array(Buffer.from(s, "latin1"));
const dot = "\xe2\xa0\x81";
function codexFrame(i) {
  // Dots at different columns each frame, the way codex animates them.
  const a = 20 + (i * 7) % 60, b = 30 + (i * 13) % 80;
  return [
    `\x1b[?2026h\x1b[?25l\x1b[m\x1b[38;2;90;90;90m\x1b[48;2;41;41;41m\x1b[27;${a}H${dot}\x1b[23C${dot}` +
      `\x1b[29;8H${dot}\x1b[${b}C${dot}\x1b[1C\x1b[?25h\x1b[0 q\x1b[?2026l`,
    `\x1b[?25l\x1b[m\x1b[26;1H \x1b[48;2;41;41;41m\x1b[29;${a + 3}H${dot}\x1b[28;3H\x1b[?25h`,
  ];
}

// ── codex, settle on ─────────────────────────────────────────────────────────
{
  const term = fakeTerm();
  const api = load({ cursor_settle_ms: 40 }, term);
  api.cursorReset();
  const painted = new Set();
  for (let i = 0; i < 50; i++) {
    for (const chunk of codexFrame(i)) {
      api.writeRunnerOutput(term, enc(chunk));
      painted.add(term.paint());
      advance(2);
    }
    // ~100ms between codex's animation frames, measured.
    advance(98);
    painted.add(term.paint());
  }
  const strays = [...painted].filter(p => p !== "hidden" && p !== "28;3");
  if (strays.length) fail(`codex's cursor was painted away from the prompt at ${strays.join(", ")}`);
  if (!painted.has("28;3")) fail("codex's cursor never came back at the prompt between frames");
}

// ── the same frames with no settle: the defect, so the fixture is proven ─────
{
  const term = fakeTerm();
  const api = load({}, term);
  api.cursorReset();
  const painted = new Set();
  for (let i = 0; i < 10; i++) {
    for (const chunk of codexFrame(i)) {
      api.writeRunnerOutput(term, enc(chunk));
      painted.add(term.paint());
    }
  }
  if (![...painted].some(p => p !== "hidden" && p !== "28;3")) {
    fail("with no settle the fixture never shows a stray cursor, so it does not reproduce codex");
  }
}

// ── a runner that hides its cursor stays hidden after the settle ────────────
{
  const term = fakeTerm();
  const api = load({ cursor_settle_ms: 40 }, term);
  api.cursorReset();
  api.writeRunnerOutput(term, enc("\x1b[?25l\x1b[5;5Hhello"));
  advance(100);
  if (term.paint() !== "hidden") fail(`a runner that hid its cursor shows one at ${term.paint()}`);
  api.writeRunnerOutput(term, enc("\x1b[10;3H\x1b[?25h"));
  advance(100);
  if (term.paint() !== "10;3") fail(`a runner that showed its cursor at 10;3 shows ${term.paint()}`);
}

// ── claude, no settle: written through, one write per chunk ─────────────────
{
  const term = fakeTerm();
  const api = load({ bracketed_paste: true }, term);
  api.cursorReset();
  const chunk = "\x1b[?2026h\x1b[?25l\x1b[23;1H*\x1b[26;3H\x1b[?25h\x1b[?2026l";
  api.writeRunnerOutput(term, enc(chunk));
  if (term.st.writes !== 1) fail(`claude's chunk took ${term.st.writes} writes, wanted it written through once`);
  if (term.paint() !== "26;3") fail(`claude's cursor is at ${term.paint()}, wanted 26;3`);
  if (timers.length) fail("a runner with no settle left a timer running");
}

// ── lastCursorMode reads the last show or hide in a chunk ───────────────────
{
  const api = load({}, fakeTerm());
  const cases = [["\x1b[?25l..\x1b[?25h", true], ["\x1b[?25h..\x1b[?25l", false], ["plain", null], ["\x1b[?2026h", null]];
  for (const [s, want] of cases) {
    const got = api.lastCursorMode(enc(s));
    if (got !== want) fail(`lastCursorMode(${JSON.stringify(s)}) is ${got}, wanted ${want}`);
  }
}

if (bad) process.exit(1);
console.log("term cursor: ok");
