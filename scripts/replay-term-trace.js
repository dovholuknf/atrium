// Replays a terminal trace saved from the board's cog ("save terminal trace")
// through the board's own xterm.js in headless Chromium, and prints the screen.
//
//   node scripts/replay-term-trace.js <trace.json> [--upto <ms>] [--frames]
//
// The trace starts mid-session, since it is a rolling window, so the first
// screen can be partial. What matters is the redraw that went wrong: step
// `--upto` across it and watch which frame put text on the wrong row.
// `--frames` lists every frame with its time, direction and a readable escape.
//
// The grid starts at the size the trace recorded first and follows every
// `xterm-resize` in it, which is what the board's terminal did.
const fs = require("fs");
const path = require("path");

function loadPlaywright() {
  for (const p of ["playwright", "@playwright/test"]) {
    try { return require(p); } catch (e) {}
  }
  console.error("playwright is not installed. run `npm install` in the repo root.");
  process.exit(2);
}

function visible(buf) {
  return buf.toString("latin1").replace(/[\x00-\x1f\x7f]/g, c =>
    c === "\x1b" ? "\\e" : c === "\r" ? "\\r" : c === "\n" ? "\\n\n      " :
      "\\x" + c.charCodeAt(0).toString(16).padStart(2, "0"));
}

(async () => {
  const args = process.argv.slice(2);
  const file = args.find(a => !a.startsWith("--") && !/^\d+$/.test(a));
  if (!file) {
    console.error("usage: node scripts/replay-term-trace.js <trace.json> [--upto <ms>] [--frames]");
    process.exit(2);
  }
  const ui = args.indexOf("--upto");
  const upto = ui >= 0 ? +args[ui + 1] : Infinity;
  const trace = JSON.parse(fs.readFileSync(file, "utf8"));
  const frames = trace.frames.filter(f => f.t <= upto);

  if (args.includes("--frames")) {
    for (const f of frames) {
      const body = f.b64 != null ? visible(Buffer.from(f.b64, "base64")) : f.s;
      console.log(`${f.t.toFixed(1).padStart(9)} ${f.d.padEnd(3)} ${body}`);
    }
    return;
  }

  let cols = trace.cols, rows = trace.rows;
  const first = frames.find(f => f.d === "ctl" && /resize|open-term/.test(f.s));
  if (first) ({ cols, rows } = JSON.parse(first.s));
  // A text frame that is the daemon's `caps` message is consumed by the board,
  // not written. See `takeTermCaps`.
  const isCaps = f => f.s != null && /^\{"t":"caps"/.test(f.s);
  const steps = frames
    .filter(f => (f.d === "out" && !isCaps(f)) || (f.d === "ctl" && /xterm-resize|sock-open-reset/.test(f.s)))
    .map(f => f.d === "out"
      ? { out: f.b64 != null ? Array.from(Buffer.from(f.b64, "base64")) : f.s }
      : { ctl: JSON.parse(f.s) });

  const xterm = fs.readFileSync(path.join(__dirname, "..", "internal", "api", "web", "vendor", "xterm.js"), "utf8");
  const { chromium } = loadPlaywright();
  const browser = await chromium.launch();
  const page = await browser.newPage();
  await page.setContent("<div id=t></div>");
  await page.addScriptTag({ content: xterm });
  const res = await page.evaluate(async ({ cols, rows, steps }) => {
    const term = new Terminal({ cols, rows, scrollback: 5000, allowProposedApi: true });
    term.open(document.getElementById("t"));
    for (const s of steps) {
      if (s.ctl) {
        if (s.ctl.ev === "xterm-resize") term.resize(s.ctl.cols, s.ctl.rows);
        else term.reset();
        continue;
      }
      const data = typeof s.out === "string" ? s.out : new Uint8Array(s.out);
      await new Promise(r => term.write(data, r));
    }
    const b = term.buffer.active;
    const lines = [];
    for (let y = 0; y < term.rows; y++) lines.push(b.getLine(b.viewportY + y).translateToString(true));
    return { lines, x: b.cursorX, y: b.cursorY, cols: term.cols, rows: term.rows };
  }, { cols, rows, steps });
  await browser.close();

  res.lines.forEach((l, i) => console.log(String(i).padStart(3) + (i === res.y ? ">" : "|") + l));
  console.log(`replayed ${steps.length} steps at ${res.cols}x${res.rows}, cursor ${res.x},${res.y}`);
  if (upto === Infinity && trace.screen && trace.cursor) {
    const same = trace.screen.length === res.lines.length &&
      trace.screen.every((l, i) => l === res.lines[i]);
    console.log(same ? "matches the screen at save time"
      : `differs from the screen at save time (saved cursor ${trace.cursor.x},${trace.cursor.y})`);
  }
})();
