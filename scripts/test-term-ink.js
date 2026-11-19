// Every terminal theme, at every nesting depth, keeps its text, band, greys and faint text at 4.5:1 or better, and the
// output rewrite moves the prompt band and faint text.
const vm = require("vm");
const { boardScript } = require("./board-source.js");

let bad = 0;
const fail = m => { console.error("FAIL: " + m); bad++; };
const page = boardScript();

function lift(start, end) {
  const at = page.indexOf(start);
  if (at < 0) { console.error("FAIL: the board no longer has `" + start + "`"); process.exit(1); }
  const stop = page.indexOf(end, at + start.length);
  return page.slice(at, stop < 0 ? page.length : stop + end.length);
}

const src = [
  lift("const TERM_THEMES = {", "\n};"),
  lift("const CARD_TEXT_FLOOR", ";"),
  lift("function legible(", "\n}"), lift("function hexRGB(", "\n}"), lift("function rgbHex(", "\n}"),
  lift("function rgbList(", "\n}"), lift("function mixRGB(", "\n}"), lift("function contrastRGB(", "\n}"),
  lift("const TERM_SHADE", ";"), lift("function termTintOf(", "\n}"),
  lift("function termInk(", "\n}"), lift("function termInkText(", "\n}"), lift("function termInkMap(", "\n}"),
  "this.out = { TERM_THEMES, termInk, termInkText, termInkMap, termTintOf, hexRGB, legible, contrastRGB };"
].join("\n");
const ctx = {};
vm.runInNewContext(src, ctx);
const { TERM_THEMES, termInk, termInkText, termInkMap, termTintOf, hexRGB, legible, contrastRGB } = ctx.out;

const rgb = s => { const [r, g, b] = s.split(";").map(Number); return { r, g, b }; };
const GREYS = { grey153: { r: 153, g: 153, b: 153 }, grey136: { r: 136, g: 136, b: 136 } };
let checked = 0;
for (const [name, theme] of Object.entries(TERM_THEMES)) {
  let th = theme;
  for (let depth = 1; depth <= 3; depth++) {
    if (depth > 1) th = termTintOf(th);
    if (!hexRGB(th.background) || !hexRGB(th.foreground)) continue;
    const bg = hexRGB(th.background), fg = legible(hexRGB(th.foreground), bg);
    const ink = termInk(th), at = name + " depth " + depth;
    const band = rgb(ink.band), faint = rgb(ink.faint);
    const must = (label, v) => { checked++; if (v < 4.5) fail(at + " " + label + " is " + v.toFixed(2)); };
    must("text on bg", contrastRGB(fg, bg));
    must("text on band", contrastRGB(fg, band));
    must("faint on bg", contrastRGB(faint, bg));
    for (const [g, c] of Object.entries(GREYS)) must(g + " under the floor", contrastRGB(legible(c, bg), bg));
  }
}
if (!checked) fail("nothing was checked");

const st = { band: "57;112;70", faint: "170;200;180", on: false };
const out = termInkText("\x1b[48;2;55;55;55m> hi\x1b[49m \x1b[2mdim\x1b[22m \x1b[1mbold\x1b[22m \x1b[48;2;9;9;9mx", st);
if (!out.includes("\x1b[48;2;57;112;70m")) fail("the band was not rewritten: " + JSON.stringify(out));
if (!out.includes("\x1b[38;2;170;200;180mdim\x1b[22;39m")) fail("faint was not rewritten: " + JSON.stringify(out));
if (!out.includes("\x1b[1mbold\x1b[22m")) fail("a 22 that only ends bold changed: " + JSON.stringify(out));
if (!out.includes("\x1b[48;2;9;9;9m")) fail("another background changed: " + JSON.stringify(out));

// A frame boundary at every byte of the stream, as text and as bytes, gives what one frame would.
const stream = "ab\x1b[48;2;55;55;55m> hi\x1b[49m \x1b[2mdim\x1b[22m \x1b[1mbold\x1b[22m\x1b[48;2;9;9;9mx\x1b[0m";
const fresh = () => ({ band: "57;112;70", faint: "170;200;180", on: false, tail: "" });
const whole = termInkText(stream, fresh());
for (let i = 1; i < stream.length; i++) {
  const st = fresh();
  const text = termInkMap(stream.slice(0, i), st) + termInkMap(stream.slice(i), st);
  if (text + st.tail !== whole) fail("text split at " + i + ": " + JSON.stringify(text));
  const sb = fresh();
  const bytes = Buffer.from(Buffer.from(stream, "latin1").subarray(0, i));
  const rest = Buffer.from(Buffer.from(stream, "latin1").subarray(i));
  const got = Buffer.concat([Buffer.from(termInkMap(bytes, sb)), Buffer.from(termInkMap(rest, sb))]).toString("latin1");
  if (got + sb.tail !== whole) fail("bytes split at " + i + ": " + JSON.stringify(got));
}

if (bad) { console.error(bad + " failed"); process.exit(1); }
console.log("term-ink ok, " + checked + " contrast checks");
