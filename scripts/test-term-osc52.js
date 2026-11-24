// OSC 52 is how opencode copies its own drag-select. osc52Text turns the sequence into the text to copy, and a query
// or anything unreadable into nothing, so the handler never writes a clipboard with garbage.
const { boardScript } = require("./board-source.js");

const page = boardScript();
const at = page.indexOf("function osc52Text(");
if (at < 0) {
  console.error("FAIL: the board no longer has `function osc52Text(`. Point this test at what replaced it.");
  process.exit(1);
}
const osc52Text = new Function(page.slice(at, page.indexOf("\n}", at) + 2) + "\nreturn osc52Text;")();

let bad = 0;
const b64 = s => Buffer.from(s, "utf8").toString("base64");
for (const [data, want] of [
  ["c;" + b64("hello → world\nline 2"), "hello → world\nline 2"],
  [";" + b64("no selection named"), "no selection named"],
  ["c;?", ""],
  ["c;", ""],
  ["c;%%%not base64", ""],
]) {
  const got = osc52Text(data);
  if (got !== want) {
    console.error(`FAIL: osc52Text(${JSON.stringify(data)}) = ${JSON.stringify(got)}, want ${JSON.stringify(want)}`);
    bad++;
  }
}
if (bad) process.exit(1);
console.log("ok: osc52Text");
