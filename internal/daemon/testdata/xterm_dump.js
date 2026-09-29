// Feed bytes to the VENDORED xterm.js and print what it holds, for
// screen_diff_test.go. No DOM: open() is never called, so this runs in plain
// node. Reads one JSON document on stdin:
//
//   {"cols":80,"rows":24,"cuts":[{"at":120,"cols":60,"rows":0}],"data":"<base64>"}
//
// and prints {"lines":[{"t":"text","s":"0011"}],"baseY":n,"curY":n,"curX":n}.
// `t` is the row with trailing blanks trimmed. `s` is a mask, one character per
// character of `t`: 1 where the cell carries any colour or attribute, 0 where
// it is default. Wide characters take one position, the way translateToString
// gives them.
const path = require("path");
const { Terminal } = require(path.join(__dirname, "..", "..", "api", "web", "vendor", "xterm.js"));

let raw = "";
process.stdin.on("data", (d) => (raw += d));
process.stdin.on("end", async () => {
  const job = JSON.parse(raw);
  const data = Buffer.from(job.data, "base64");
  const t = new Terminal({ cols: job.cols, rows: job.rows, scrollback: 100000, allowProposedApi: true });
  const write = (b) => new Promise((res) => t.write(b, res));
  let at = 0;
  for (const c of job.cuts || []) {
    if (c.at > at) await write(data.subarray(at, c.at));
    at = Math.max(at, c.at);
    t.resize(c.cols || t.cols, c.rows || t.rows);
  }
  if (at < data.length) await write(data.subarray(at));

  const b = t.buffer.active;
  const cell = b.getNullCell();
  const lines = [];
  for (let i = 0; i < b.length; i++) {
    const l = b.getLine(i);
    // xterm trims only never-written cells, so a written space survives. screen.go
    // trims both kinds, and neither is content, so trim them here too.
    const text = l.translateToString(true).replace(/ +$/, "");
    let mask = "";
    let pos = 0;
    for (let x = 0; x < l.length && pos < [...text].length; x++) {
      const c = l.getCell(x, cell);
      if (c.getWidth() === 0) continue;
      const styled =
        c.getFgColorMode() !== 0 || c.getBgColorMode() !== 0 ||
        c.isBold() || c.isDim() || c.isItalic() || c.isUnderline() ||
        c.isBlink() || c.isInverse() || c.isInvisible() || c.isStrikethrough();
      mask += styled ? "1" : "0";
      pos++;
    }
    lines.push({ t: text, s: mask });
  }
  process.stdout.write(JSON.stringify({ lines, baseY: b.baseY, curY: b.cursorY, curX: b.cursorX }));
});
