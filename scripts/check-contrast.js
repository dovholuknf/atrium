// Every skin's colour pairs, checked for contrast against the surface they
// actually sit on.
//
// WHY THIS IS A SCRIPT AND NOT A LOOK. Twenty skins is twenty chances for a
// value tuned against navy to be invisible, and nobody is going to open twenty
// skins and hover every element by hand. The defect this catches is the one
// that shipped: a hover that raised a name's LIGHTNESS to a fixed 85%, which
// reads as more prominent on a dark board and nearly invisible on a light one.
//
// THE RULE IT ENFORCES, which is the thing worth keeping: anything that means
// "more prominent" has to move relative to the skin's OWN text colour rather
// than toward white. A fixed direction is wrong on half the set. So a hover is
// not checked for a ratio alone, it is checked against the colour it replaces:
// a hover that is less legible than the resting state fails, whichever way the
// skin is pointed.
//
// THE HAZARD, and the reason most of this file is compositing rather than
// arithmetic: hovers and lifts are `rgba` over a backdrop, and a check that
// compares a raw `--lift` against a text colour passes everything and proves
// nothing. `rgba(255,255,255,.05)` has a contrast ratio against any dark text
// that looks wonderful and describes no pixel on the screen. Every pair below
// therefore names the whole STACK it sits on, bottom layer first, and each
// rgba layer is composited onto what is under it before a ratio is taken.
//
// WHAT IT CATCHES: "invisible" and "far too light". WHAT IT DOES NOT: "ugly",
// and a colour that is legible and wrong, such as a hover that reads as a
// warning. Those still need an eye.
//
//   node scripts/check-contrast.js internal/api/web/index.html
//   node scripts/check-contrast.js internal/api/web/index.html --report
//
// `--report` prints every ratio in every skin instead of only the failures,
// which is how you tell a floor that is too low from a colour that is fine.
"use strict";
const fs = require("fs");

// ── the stylesheet ─────────────────────────────────────────────────────────
// Comments go first, and they are replaced by their own newlines rather than
// deleted: the block scanner below works on line shape, and swallowing a
// twelve line comment would move every declaration after it into the wrong
// skin. Prose also mentions variables by name, and a comment reading
// `--card-tint` is not a declaration of one.
function stripComments(css) {
  return css.replace(/\/\*[\s\S]*?\*\//g, (m) => m.replace(/[^\n]/g, " "));
}

// `:root { ... }` and every `:root[data-skin="x"] { ... }`, flattened to a map
// of variable to unresolved value text. Same shape the skins check parses, and
// the same assumption: one block per skin, closed by a `}` at two spaces.
function parseSkins(css) {
  const lines = stripComments(css).split("\n");
  const blocks = new Map();
  let current = null;
  for (const line of lines) {
    if (/^\s{0,2}:root\s*\{/.test(line)) { current = ":root"; blocks.set(current, {}); continue; }
    const skin = line.match(/^\s{0,2}:root\[data-skin="([^"]+)"\]\s*\{/);
    if (skin) { current = skin[1]; blocks.set(current, {}); continue; }
    if (current && /^\s{0,2}\}/.test(line)) { current = null; continue; }
    if (!current) continue;
    for (const decl of line.split(";")) {
      const m = decl.match(/^\s*(--[a-z0-9-]+)\s*:\s*(.+?)\s*$/);
      if (m) blocks.get(current)[m[1]] = m[2];
    }
  }
  const root = blocks.get(":root");
  if (!root) throw new Error("no :root block found. the parser is broken, not the stylesheet.");
  const skins = new Map();
  // The default board wears `:root` and no `data-skin`, so it is a skin here
  // even though it has no block of its own. Leaving it out would exempt the
  // palette every other skin is derived from.
  skins.set("(default)", { ...root });
  for (const [name, vars] of blocks) {
    if (name === ":root") continue;
    skins.set(name, { ...root, ...vars });
  }
  return skins;
}

// READ THE COLOUR OUT OF THE RULE, not out of this file.
//
// A pair that restates the stylesheet's colour is a pair that keeps checking
// the OLD colour after somebody edits the stylesheet. That is not a small
// gap: the two defects this file has found were both a literal colour in a
// rule, and if the table holds its own copy then putting either of them back
// is invisible here. Proved by breaking the rules on purpose and watching a
// table of copies stay green.
//
// So the hue-derived colours are named by SELECTOR and read from the file. The
// palette variables are still written as `var(--x)`, which is safe from the
// same drift for a different reason: the variables are what the skins check
// already holds to a fixed set, and a rule that stops using one is a rule that
// this table cannot follow anyway. That limit is real. It is why a pair reads
// from the rule wherever the rule holds a colour of its own.
function parseRules(css) {
  const text = stripComments(css);
  const rules = [];
  let at = 0;
  const scan = (from, to) => {
    let selStart = from;
    for (let i = from; i < to; i++) {
      if (text[i] === "}") { selStart = i + 1; continue; }
      if (text[i] !== "{") continue;
      const selector = text.slice(selStart, i).trim();
      // Find this block's close, allowing for @media wrapping other rules.
      let depth = 1, j = i + 1;
      while (j < to && depth > 0) {
        if (text[j] === "{") depth++;
        else if (text[j] === "}") depth--;
        j++;
      }
      if (selector.startsWith("@")) scan(i + 1, j - 1);
      else rules.push({ selector, body: text.slice(i + 1, j - 1) });
      selStart = j;
      i = j - 1;
    }
  };
  scan(at, text.length);
  return rules;
}

// The last declaration of a property on a rule whose selector list contains
// the one asked for. Last wins, which is the cascade for equal specificity and
// is as much of CSS as this is willing to model.
//
// A miss is an ERROR and not a skipped check. A selector that no longer
// matches anything means the rule was renamed and this table is describing a
// board that does not exist.
function ruleValue(rules, selector, prop) {
  const want = selector.replace(/\s+/g, " ").trim();
  let found = null;
  for (const rule of rules) {
    const list = rule.selector.split(",").map((s) => s.replace(/\s+/g, " ").trim());
    if (!list.includes(want)) continue;
    for (const decl of rule.body.split(";")) {
      const m = decl.match(/^\s*([a-z-]+)\s*:\s*(.+?)\s*$/is);
      if (m && m[1] === prop) found = m[2].replace(/\s+/g, " ").trim();
    }
  }
  if (found === null) {
    throw new Error(`no \`${prop}\` on \`${selector}\` in the stylesheet. ` +
      "the rule was renamed or removed, so this pair is checking a colour that " +
      "is no longer on the board. point it at the rule that carries the colour now.");
  }
  return found;
}

// ── colour ─────────────────────────────────────────────────────────────────
// Split on a separator only at bracket depth zero, so the commas inside
// `rgba(var(--x-rgb),.4)` do not tear an argument list apart.
function splitTop(s, seps) {
  const out = [];
  let depth = 0, at = 0;
  for (let i = 0; i < s.length; i++) {
    const c = s[i];
    if (c === "(") depth++;
    else if (c === ")") depth--;
    else if (depth === 0 && seps.includes(c)) { out.push(s.slice(at, i)); at = i + 1; }
  }
  out.push(s.slice(at));
  return out.map((x) => x.trim()).filter((x) => x.length > 0);
}

// `var(--x)` substituted as TEXT, not as a colour. The triples are the reason:
// `rgba(var(--warn-rgb),.13)` is three channels and an alpha only after the
// variable is spliced in, and a resolver that parsed `var(--warn-rgb)` as a
// colour would have to invent a fourth value to do it.
function expandVars(expr, vars, seen) {
  let out = expr, guard = 0;
  while (/var\(\s*--/.test(out)) {
    if (guard++ > 40) throw new Error(`variable expansion does not terminate: ${expr}`);
    out = out.replace(/var\(\s*(--[a-z0-9-]+)\s*(?:,\s*([^()]*))?\)/g, (m, name, fallback) => {
      if (seen.has(name)) throw new Error(`${name} is defined in terms of itself`);
      const v = vars[name];
      if (v === undefined) {
        if (fallback !== undefined) return fallback;
        throw new Error(`${name} is used but never defined`);
      }
      return v;
    });
  }
  return out;
}

// Only what the stylesheet actually writes: a sum or difference of numbers.
// `calc(var(--group-fill) + .06)` is the whole of it, and it matters because
// the alpha it produces changes the surface a heading is read against.
function num(text) {
  const t = text.trim();
  const calc = t.match(/^calc\((.+)\)$/);
  if (calc) {
    const parts = calc[1].match(/^\s*(-?[\d.]+%?)\s*([+\-*/])\s*(-?[\d.]+%?)\s*$/);
    if (!parts) throw new Error(`calc too clever for this checker: ${t}`);
    const a = num(parts[1]), b = num(parts[3]);
    return parts[2] === "+" ? a + b : parts[2] === "-" ? a - b
      : parts[2] === "*" ? a * b : a / b;
  }
  if (t.endsWith("%")) return parseFloat(t) / 100;
  const n = parseFloat(t);
  if (Number.isNaN(n)) throw new Error(`not a number: ${t}`);
  return n;
}

function hslToRgb(h, s, l) {
  h = ((h % 360) + 360) % 360;
  const c = (1 - Math.abs(2 * l - 1)) * s;
  const x = c * (1 - Math.abs(((h / 60) % 2) - 1));
  const m = l - c / 2;
  const seg = [[c, x, 0], [x, c, 0], [0, c, x], [0, x, c], [x, 0, c], [c, 0, x]][Math.floor(h / 60) % 6];
  return { r: (seg[0] + m) * 255, g: (seg[1] + m) * 255, b: (seg[2] + m) * 255 };
}

// A colour expression to straight (not premultiplied) rgba, channels 0-255 and
// alpha 0-1. Alpha is kept rather than flattened here, because flattening is
// what the stack does and doing it early is the hazard this file exists for.
function parseColor(expr, vars, seen = new Set()) {
  const s = expandVars(String(expr).trim(), vars, seen);
  const hex = s.match(/^#([0-9a-f]{3,8})$/i);
  if (hex) {
    const h = hex[1];
    const wide = h.length <= 4 ? h.split("").map((c) => c + c).join("") : h;
    if (wide.length !== 6 && wide.length !== 8) throw new Error(`not a colour: ${s}`);
    const ch = (i) => parseInt(wide.slice(i * 2, i * 2 + 2), 16);
    return { r: ch(0), g: ch(1), b: ch(2), a: wide.length === 8 ? ch(3) / 255 : 1 };
  }
  const fn = s.match(/^([a-z-]+)\((.*)\)$/is);
  if (!fn) {
    if (s === "transparent") return { r: 0, g: 0, b: 0, a: 0 };
    if (s === "white") return { r: 255, g: 255, b: 255, a: 1 };
    if (s === "black") return { r: 0, g: 0, b: 0, a: 1 };
    throw new Error(`not a colour this checker understands: ${s}`);
  }
  const name = fn[1].toLowerCase(), args = fn[2];
  if (name === "rgb" || name === "rgba") {
    const p = splitTop(args, ",/").map(num);
    if (p.length < 3) throw new Error(`rgb() wants three channels: ${s}`);
    return { r: p[0], g: p[1], b: p[2], a: p.length > 3 ? p[3] : 1 };
  }
  if (name === "hsl" || name === "hsla") {
    const p = splitTop(args, ", /").map(num);
    if (p.length < 3) throw new Error(`hsl() wants three parts: ${s}`);
    const rgb = hslToRgb(p[0], p[1], p[2]);
    return { ...rgb, a: p.length > 3 ? p[3] : 1 };
  }
  // `color-mix(in srgb, A 45%, B)` is how the fixed hover blends toward the
  // skin's own head colour, so it has to be understood here or the one rule
  // this file is about cannot be expressed in the stylesheet.
  if (name === "color-mix") {
    const parts = splitTop(args, ",");
    if (parts.length !== 3 || !/^in\s+srgb$/i.test(parts[0])) {
      throw new Error(`only \`color-mix(in srgb, A, B)\` is understood: ${s}`);
    }
    const side = (text) => {
      const m = text.match(/^(.*?)\s+(-?[\d.]+%)$/);
      return m ? { c: parseColor(m[1], vars, seen), w: num(m[2]) } : { c: parseColor(text, vars, seen), w: null };
    };
    const a = side(parts[1]), b = side(parts[2]);
    let wa = a.w, wb = b.w;
    if (wa === null && wb === null) { wa = 0.5; wb = 0.5; }
    else if (wa === null) wa = 1 - wb;
    else if (wb === null) wb = 1 - wa;
    const total = wa + wb;
    wa /= total; wb /= total;
    return {
      r: a.c.r * wa + b.c.r * wb,
      g: a.c.g * wa + b.c.g * wb,
      b: a.c.b * wa + b.c.b * wb,
      a: a.c.a * wa + b.c.a * wb,
    };
  }
  throw new Error(`colour function this checker does not know: ${name}()`);
}

// THE WHOLE POINT. Bottom layer first, each one painted onto what is already
// there. The bottom layer has to be opaque, because an rgba over nothing is
// exactly the mistake being guarded against: it would pick up whatever the
// checker happened to default to and prove a ratio no pixel has.
function flatten(layers, vars, label) {
  let out = null;
  for (const expr of layers) {
    const c = parseColor(expr, vars);
    if (out === null) {
      if (c.a < 0.999) {
        throw new Error(`${label}: the bottom layer \`${expr}\` is translucent. ` +
          `name the surface under it, or the ratio is against nothing.`);
      }
      out = { r: c.r, g: c.g, b: c.b };
      continue;
    }
    out = {
      r: c.r * c.a + out.r * (1 - c.a),
      g: c.g * c.a + out.g * (1 - c.a),
      b: c.b * c.a + out.b * (1 - c.a),
    };
  }
  if (out === null) throw new Error(`${label}: no layers`);
  return out;
}

// Text is composited too, onto the same stack. Most of the foregrounds are
// opaque, but rgba text does show up in the stylesheet and an unflattened one
// would score better than it looks.
function overlay(fgExpr, layers, vars, label) {
  return flatten([...layers, fgExpr], vars, label);
}

function luminance({ r, g, b }) {
  const ch = (v) => {
    const s = v / 255;
    return s <= 0.03928 ? s / 12.92 : Math.pow((s + 0.055) / 1.055, 2.4);
  };
  return 0.2126 * ch(r) + 0.7152 * ch(g) + 0.0722 * ch(b);
}

function ratio(a, b) {
  const la = luminance(a), lb = luminance(b);
  return (Math.max(la, lb) + 0.05) / (Math.min(la, lb) + 0.05);
}

// ── the surfaces ───────────────────────────────────────────────────────────
// Named once, because the same stack carries a dozen pairs and a stack written
// out per pair is a stack that drifts. An array inside a stack is a set of
// ALTERNATIVES: the page is a gradient through three colours and a card is a
// gradient through two, so the pair is scored at every stop and judged on the
// worst one. A ratio that only holds at the light end of a gradient is a ratio
// that fails halfway down the column.
const PAGE = [["var(--bg-0)", "var(--bg-1)", "var(--bg-2)"]];
const COL = [...PAGE, ["rgba(var(--shell0-rgb),.7)", "rgba(var(--shell1-rgb),.7)"]];
const CARD = [["var(--card-0)", "var(--card-1)"]];
const SINK = ["var(--bg-2)"];
const MENU = ["var(--shell-0)"];
// A project group is a wash of the group's own hue over the column, and the
// cards inside it carry a weaker wash of the same hue over their own gradient.
const GROUP = [...COL, "hsl(var(--ghue) 60% 45% / var(--group-fill))"];
const PINS = [...COL, "hsl(var(--ghue) 70% 50% / calc(var(--group-fill) + .06))"];
const PROJECT_CARD = [...CARD, "hsl(var(--ghue) 60% 45% / var(--card-tint))"];

// Every group hue the board can hash a project name onto. Twelve stops rather
// than 360, because the failure being looked for is a whole skin being wrong
// and not one degree of hue.
const HUES = [0, 30, 60, 90, 120, 150, 180, 210, 240, 270, 300, 330];

// THE PINNED GROUP IS NOT HASHED. `pinnedGroupHTML` writes `--ghue:41` on it
// and always has: it is not a project, so borrowing a project's colour would
// say that it was.
//
// Sweeping the hues over this block therefore invents a group that cannot
// exist, and scores a defect nobody can see. Which happened: the pinned
// heading was first reported at 1.96, a number taken at hue 240. At the hue
// the board actually draws it is 2.73, on one skin rather than four. A pair
// that sweeps a value the markup fixes is a pair that cries wolf, and a check
// that cries wolf gets switched off.
const PIN_HUE = [41];

// ── the floors ─────────────────────────────────────────────────────────────
// WHAT THESE ARE NOT: a WCAG AA certificate. AA is 4.5 for body text and 3.0
// for large text, and this palette runs its recessive tiers under both on
// purpose. `--dim` and `--dimmest` are timestamps and counts, chosen to sit
// back so the names in front of them read first, and a floor of 4.5 on those
// would fail all twenty-one skins and be turned off within a week.
//
// So each floor is set BELOW what the palette manages today and above what is
// unreadable, and the worst value the current palette produces is written next
// to it. That number is the useful one: it is what a regression has to fall
// through, and it is how you tell "this needs tightening" from "this is red".
// Tighten a floor by fixing the colours first and then raising it here.
const READ = 4.0;    // text read at length. Worst today: 4.70, a column heading.
const LABEL = 2.8;   // a coloured label, chip or heading. Worst today: 2.98, warn on linen.
const QUIET = 1.8;   // deliberately recessive. Worst today: 1.92, an idle chip on clay.
const VISIBLE = 1.02; // a surface tellable from the one under it. Worst today: 1.04.

// ── the pairs ──────────────────────────────────────────────────────────────
// Each entry names a pair that has to stay legible, the stack it sits on, and
// the floor it has to clear.
//
//   text:    `fg` against `on`.
//   surface: `on` plus `hover` against `on`, which is whether a hover is
//            visible at all rather than whether anything on it is readable.
//   hover:   `fg` against `on`, AND `fg` no worse than `rest` on the same
//            stack. This is the rule the fixed nit taught, in the only form a
//            script can hold it: "more prominent" cannot be a direction in
//            absolute lightness, so what is asserted is that the hover does
//            not LOSE contrast against the surface it is on.
const PAIRS = [
  // The page, and the shell.
  { what: "body text on the page", kind: "text", fg: "var(--body)", on: PAGE, floor: READ },
  { what: "body text on a column", kind: "text", fg: "var(--body)", on: COL, floor: READ },
  { what: "a column heading", kind: "text", fg: "var(--label)", on: COL, floor: READ },
  { what: "a status group heading", kind: "text", fg: "var(--dim)", on: COL, floor: LABEL },
  { what: "a status group's count", kind: "text", fg: "var(--dimmest)", on: COL, floor: QUIET },

  // A card.
  { what: "a card title", kind: "text", fg: "var(--head)", on: CARD, floor: READ },
  { what: "body text on a card", kind: "text", fg: "var(--body)", on: CARD, floor: READ },
  { what: "a card's quiet facts", kind: "text", fg: "var(--dim)", on: CARD, floor: LABEL },
  { what: "a card's quietest facts", kind: "text", fg: "var(--dimmest)", on: CARD, floor: QUIET },
  { what: "a card in a project group", kind: "text", fg: "var(--head)", on: PROJECT_CARD, floor: READ, hues: HUES },
  { what: "body text on a card in a project group", kind: "text", fg: "var(--body)",
    on: PROJECT_CARD, floor: READ, hues: HUES },

  // Chips. Each one is its own tint over the card it sits on, which is the
  // hazard in miniature: `rgba(var(--warn-rgb),.13)` against `var(--warn)` is
  // a ratio of nearly nothing and tells you the chip is unreadable, which it
  // is not.
  { what: "a neutral chip's text", kind: "text", fg: "var(--label)", on: [...CARD, "var(--chip)"], floor: LABEL },
  { what: "an accent chip's text", kind: "text", fg: "var(--chip-accent-text)",
    on: [...CARD, "var(--chip-accent)"], floor: READ },
  { what: "a warn chip's text", kind: "text", fg: "var(--warn)", on: [...CARD, "var(--warn-bg)"], floor: LABEL },
  { what: "a shell runner chip's text", kind: "text", fg: "var(--warn)",
    on: [...CARD, "rgba(var(--warn-rgb),.10)"], floor: LABEL },
  { what: "a live chip's text", kind: "text", fg: "var(--teal)",
    on: [...CARD, "rgba(var(--teal-rgb),.10)"], floor: LABEL },
  { what: "a codex runner chip's text", kind: "text", fg: "var(--path)",
    on: [...CARD, "rgba(var(--path-rgb),.14)"], floor: LABEL },
  { what: "a subscribed chip's text", kind: "text", fg: "var(--path)",
    on: [...CARD, "rgba(var(--path-rgb),.12)"], floor: LABEL },
  { what: "an idle chip's text", kind: "text", fg: "var(--dimmest)", on: [...CARD, "var(--hairline)"], floor: QUIET },

  // Warnings and refusals, on the panels they get drawn on.
  { what: "warn text on a warn panel", kind: "text", fg: "var(--warn)", on: [...CARD, "var(--warn-bg)"], floor: LABEL },
  { what: "warn text on a soft warn panel", kind: "text", fg: "var(--warn)",
    on: [...COL, "var(--warn-bg-soft)"], floor: LABEL },
  { what: "the text of a refusal", kind: "text", fg: "var(--danger-text)",
    on: [...CARD, "rgba(var(--danger-rgb),.24)"], floor: READ },
  { what: "an accent on a card", kind: "text", fg: "var(--teal)", on: CARD, floor: LABEL },
  { what: "a path on a card", kind: "text", fg: "var(--path)", on: CARD, floor: LABEL },
  { what: "a wait timer on a row", kind: "text", fg: "var(--warn)", on: CARD, floor: LABEL },

  // Buttons.
  { what: "a button's label", kind: "text", fg: "var(--body)", on: [...CARD, "var(--chip)"], floor: READ },
  { what: "a go button's label", kind: "text", fg: "var(--chip-accent-text)",
    on: [...CARD, "var(--chip-accent)"], floor: READ },
  { what: "an icon button", kind: "text", fg: "var(--teal)", on: COL, floor: LABEL },
  { what: "a menu entry", kind: "text", fg: "var(--label)", on: MENU, floor: READ },
  { what: "a menu entry's note", kind: "text", fg: "var(--dimmest)", on: MENU, floor: QUIET },
  { what: "a dangerous menu entry", kind: "text", fg: "var(--warn)", on: MENU, floor: LABEL },

  // Lists in a recess.
  { what: "a file's name", kind: "text", fg: "var(--body)", on: SINK, floor: READ },
  { what: "a file's size and age", kind: "text", fg: "var(--dimmest)", on: SINK, floor: QUIET },
  { what: "a directory's name", kind: "text", fg: "var(--teal)", on: SINK, floor: LABEL },

  // Hovers, as surfaces. Every one of these is `rgba` over something, and the
  // question is whether the pointer being over the row is visible at all.
  { what: "a hovered file row", kind: "surface", on: SINK, hover: ["var(--lift)"], floor: VISIBLE },
  { what: "a hovered stack row", kind: "surface", on: CARD, hover: ["var(--lift)"], floor: VISIBLE },
  { what: "a hovered decision row", kind: "surface", on: CARD, hover: ["rgba(var(--chip-rgb),.4)"], floor: VISIBLE },
  { what: "a hovered log line", kind: "surface", on: CARD, hover: ["rgba(var(--chip-rgb),.5)"], floor: VISIBLE },
  { what: "a hovered menu entry", kind: "surface", on: MENU, hover: ["var(--lift)"], floor: VISIBLE },
  { what: "a hovered column heading", kind: "surface", on: COL, hover: ["rgba(var(--chip-rgb),.35)"], floor: VISIBLE },
  { what: "a hovered button", kind: "surface",
    on: [...CARD, "var(--chip)"], hover: ["var(--btn-hover)"], floor: VISIBLE },
  { what: "a hairline between rows", kind: "surface", on: CARD, hover: ["var(--hairline)"], floor: VISIBLE },

  // Text on a hovered surface. A hover that is visible and that swallows the
  // text on it is still a bug.
  { what: "body text on a hovered row", kind: "text", fg: "var(--body)",
    on: [...CARD, "rgba(var(--chip-rgb),.4)"], floor: READ },
  { what: "quiet text on a hovered row", kind: "text", fg: "var(--dimmest)",
    on: [...CARD, "rgba(var(--chip-rgb),.4)"], floor: QUIET },
  { what: "a file's name on a hovered row", kind: "text", fg: "var(--body)",
    on: [...SINK, "var(--lift)"], floor: READ },
  { what: "a hovered menu entry's text", kind: "text", fg: "var(--head)", on: [...MENU, "var(--lift)"], floor: READ },

  // Hovers, as text. THE RULE. A hover means "more prominent", so it may not
  // read as less. `rest` is what the colour is before the pointer arrives, and
  // a hover that scores below it fails whichever direction the skin points.
  {
    what: "a hovered status group heading",
    kind: "hover", on: COL, rest: "var(--dim)", fg: "var(--head)", floor: READ,
  },
  {
    what: "a hovered column heading's text",
    kind: "hover", on: [...COL, "rgba(var(--chip-rgb),.35)"], rest: "var(--label)", fg: "var(--head)", floor: READ,
  },
  {
    what: "a hovered button's label",
    kind: "hover", on: [...CARD, "var(--btn-hover)"], rest: "var(--body)", fg: "var(--head)", floor: READ,
  },
  {
    what: "a hovered menu entry's label",
    kind: "hover", on: [...MENU, "var(--lift)"], rest: "var(--label)", fg: "var(--head)", floor: READ,
  },
  {
    what: "a hovered tab",
    kind: "hover", on: PAGE, rest: "var(--label)", fg: "var(--head)", floor: READ,
  },
  // THE NIT THIS FILE WAS WRITTEN FOR, and every colour in it read from the
  // rule rather than copied here. The resting name is the group's own hue, and
  // hovering blends it toward `--head`, which is lighter on a dark skin and
  // darker on a light one. Written as a fixed lightness it passed on the ten
  // dark skins and was nearly invisible on `paper`.
  {
    what: "a hovered project group name",
    kind: "hover", on: GROUP, hues: HUES,
    rest: [".cardgroup.project > summary .gname", "color"],
    from: [".cardgroup.project > summary:hover .gname", "color"],
    floor: LABEL,
  },
  {
    what: "a hovered stack group name",
    kind: "hover", on: [...COL, "hsl(var(--ghue) 60% 45% / var(--group-fill))"], hues: HUES,
    rest: [".stackgroup > summary .gname", "color"],
    from: [".stackgroup > summary:hover .gname", "color"],
    floor: LABEL,
  },
  // Warn, blended toward the skin's own head colour. Written as the raw
  // `var(--warn)` this scored 2.73 on `linen`, under the floor, and 2.95 to
  // 3.08 on the other three light skins, which is over it and not by much:
  // amber on the loudest wash on the board. It is now about 4.9 on `linen`.
  {
    what: "a pinned group's name", kind: "text", hues: PIN_HUE,
    from: [".cardgroup.project.pins > summary .gname", "color"], on: PINS, floor: LABEL,
  },

  // A KNOWN DEFECT, PINNED. See `pinned` below: this pair is not held to its
  // floor, it is held to the number it measures today.
  //
  // Nit 1's fix moved the group name's HOVER off a fixed lightness and left
  // the resting colour on one, a line above it. On the four light skins the
  // resting name is the same pastel as the wash behind it and the ratio is
  // 1.00 at some hues: a heading that is not there until you point at it.
  //
  // It is not fixed here because fixing it is a choice rather than a value. A
  // name has to stay identifiably its project's hue, and the colours that
  // clear the floor on `linen` are close enough to `--head` that the hover
  // has nowhere left to go. That wants an eye. It is recorded as an open nit,
  // and pinned so that it cannot quietly get worse, and so that fixing it
  // fails this check and makes somebody delete the pin.
  {
    what: "a project group name at rest",
    kind: "text", on: GROUP, hues: HUES,
    from: [".cardgroup.project > summary .gname", "color"],
    floor: LABEL, nit: "docs/css-nits.md, open nit 4",
    // Exactly 1.00 on all four, at whichever hue crosses the wash in
    // luminance. Not "low": gone.
    pinned: { paper: 1.00, daylight: 1.00, linen: 1.00, frost: 1.00 },
  },
  {
    what: "a stack group name at rest",
    kind: "text", on: [...COL, "hsl(var(--ghue) 60% 45% / var(--group-fill))"], hues: HUES,
    from: [".stackgroup > summary .gname", "color"],
    floor: LABEL, nit: "docs/css-nits.md, open nit 4",
    // Exactly 1.00 on all four, at whichever hue crosses the wash in
    // luminance. Not "low": gone.
    pinned: { paper: 1.00, daylight: 1.00, linen: 1.00, frost: 1.00 },
  },
];

// ── scoring ────────────────────────────────────────────────────────────────
// A stack may hold alternatives, so a pair is scored at every combination and
// judged on the worst. Small: two or three stops on one gradient.
function stacks(layers) {
  let out = [[]];
  for (const layer of layers) {
    const options = Array.isArray(layer) ? layer : [layer];
    const next = [];
    for (const base of out) for (const opt of options) next.push([...base, opt]);
    out = next;
  }
  return out;
}

// `from` and a selector-shaped `rest` are resolved against the stylesheet once
// and written into `fg`, so everything below works on colour expressions and
// does not care where they came from.
function bindRules(pairs, rules) {
  return pairs.map((pair) => {
    const bound = { ...pair };
    if (pair.from) bound.fg = ruleValue(rules, pair.from[0], pair.from[1]);
    if (Array.isArray(pair.rest)) bound.rest = ruleValue(rules, pair.rest[0], pair.rest[1]);
    if (bound.kind !== "surface" && !bound.fg) {
      throw new Error(`the pair "${pair.what}" names no colour. give it \`fg\` or \`from\`.`);
    }
    return bound;
  });
}

function score(pair, vars, label) {
  let worst = null;
  const hues = pair.hues || [null];
  for (const hue of hues) {
    const v = hue === null ? vars : { ...vars, "--ghue": String(hue) };
    for (const stack of stacks(pair.on)) {
      const bg = flatten(stack, v, label);
      let value, rest = null;
      if (pair.kind === "surface") {
        value = ratio(flatten([...stack, ...pair.hover], v, label), bg);
      } else {
        value = ratio(overlay(pair.fg, stack, v, label), bg);
        if (pair.kind === "hover") rest = ratio(overlay(pair.rest, stack, v, label), bg);
      }
      const where = hue === null ? "" : ` at hue ${hue}`;
      if (worst === null || value < worst.value) worst = { value, rest, where };
      // A hover that loses contrast is its own failure and can happen at a
      // ratio that is not the worst one, so it is kept separately.
      if (rest !== null && value < rest && (worst.lost === undefined || rest - value > worst.lostBy)) {
        worst.lost = where; worst.lostBy = rest - value; worst.lostValue = value; worst.lostRest = rest;
      }
    }
  }
  return worst;
}

// ── judging ────────────────────────────────────────────────────────────────
// A PINNED PAIR is a defect somebody has looked at, written down, and not
// fixed yet. It is held to the ratio it measures today rather than to its
// floor, within a tolerance that is smaller than any change worth making.
//
// This is not a suppression list, and the difference matters: a suppression
// goes green whatever the colour does afterwards, and a pin fails in BOTH
// directions. Make the defect worse and it fails. Fix the defect and it also
// fails, and tells you to delete the pin, so the nit list cannot end up
// describing a board that no longer has the nit.
const PIN_TOLERANCE = 0.05;

// One pair in one skin, to a list of complaints. Empty means it passed.
function judge(pair, skin, vars) {
  const label = `${skin}: ${pair.what}`;
  let s;
  try {
    s = score(pair, vars, label);
  } catch (e) {
    return { label, ratio: null, bad: [e.message] };
  }
  const bad = [];
  // A pin is per skin, because these defects are: the same declaration is
  // fine on the ten dark skins and gone on the four light ones. A skin the pin
  // does not name is held to the floor like anything else.
  const pinned = pair.pinned === undefined ? undefined : pair.pinned[skin];
  if (pinned !== undefined) {
    if (s.value >= pair.floor) {
      bad.push(`this is a KNOWN defect pinned at ${pinned.toFixed(2)} and it now scores ` +
        `${s.value.toFixed(2)}, over its floor of ${pair.floor.toFixed(2)}. if you fixed it, ` +
        `delete the pin here and the entry in ${pair.nit}.`);
    } else if (Math.abs(s.value - pinned) > PIN_TOLERANCE) {
      bad.push(`this is a KNOWN defect pinned at ${pinned.toFixed(2)} and it has MOVED to ` +
        `${s.value.toFixed(2)}${s.where}. something changed a colour it depends on. ` +
        `see ${pair.nit}.`);
    }
    return { label, ratio: s.value, rest: s.rest, bad, what: pair.what, skin, known: bad.length === 0 };
  }
  if (s.value < pair.floor) {
    bad.push(`${s.value.toFixed(2)} against a floor of ${pair.floor.toFixed(2)}${s.where}`);
  }
  if (s.lost !== undefined) {
    bad.push("the hover reads as LESS prominent than the resting colour " +
      `(${s.lostValue.toFixed(2)} hovered, ${s.lostRest.toFixed(2)} at rest${s.lost}). ` +
      "move it relative to --head, not toward white.");
  }
  return { label, ratio: s.value, rest: s.rest, bad, what: pair.what, skin };
}

function judgeAll(pairs, skins) {
  const out = [];
  for (const [skin, vars] of skins) for (const pair of pairs) out.push(judge(pair, skin, vars));
  return out;
}

// ── the self test ──────────────────────────────────────────────────────────
// A CHECK THAT CANNOT FAIL IS WORSE THAN NO CHECK, because it is also a claim
// that the colours were looked at. This one passed on its first run against a
// palette that had a real defect in it, which is exactly what a broken checker
// looks like.
//
// So every case below is a colour broken ON PURPOSE, in memory, and the check
// has to catch it and name the right pair. Two of them are the actual bugs:
// the hover that shipped, and the pinned heading this file found.
//
// It runs on every invocation rather than behind a flag. A self test you have
// to remember to run is a self test that is passing in someone's memory.
const BROKEN = [
  {
    // THE BUG THAT SHIPPED. The group name's hover raised lightness to a fixed
    // 85%: more prominent on a dark board, nearly invisible on a light one.
    // Caught by comparing the hover to the colour it replaces, not by a floor,
    // because on `paper` it clears any floor low enough to be useful.
    story: "the hover that raises lightness to a fixed 85%",
    pair: "a hovered project group name",
    fg: "hsl(var(--ghue) 70% 85%)",
    skins: ["paper", "linen", "daylight", "frost"],
  },
  {
    // THE SAME RULE, at a ratio no floor will catch. This hover is perfectly
    // legible: about 4.1 against the block on the dark skins, well over the
    // floor. It is still wrong, because the resting colour scores about 6.8
    // and the pointer therefore makes the name QUIETER. Only the comparison
    // against the resting colour finds this, which is why the comparison is
    // there, and this case is what proves it is still wired up.
    story: "a hover that is legible and still less prominent than the rest",
    pair: "a hovered project group name",
    fg: "hsl(var(--ghue) 45% 55%)",
    skins: ["plum", "ember", "noir", "vapor", "abyss"],
  },
  {
    // THE HAZARD, in the one form that would prove a checker is not
    // compositing. A white lift over a near-white card is nothing, and a
    // checker that scored `rgba(255,255,255,.05)` as a colour in its own right
    // would call it a bright hover and pass.
    story: "a light skin left holding the dark skins' white lift",
    vars: { "--lift": "rgba(255,255,255,.05)", "--lift-strong": "rgba(255,255,255,.09)" },
    skins: ["paper", "daylight", "linen", "frost"],
    expect: "a hovered stack row",
  },
  {
    story: "a hover tinted the same colour as the surface under it",
    vars: { "--chip-rgb": "18,41,63", "--card-0": "#12293F", "--card-1": "#12293F" },
    skins: ["(default)"],
    expect: "a hovered decision row",
  },
  {
    story: "the quietest text taken one shade too far",
    vars: { "--dimmest": "#1E2228" },
    skins: ["graphite"],
    expect: "a card's quietest facts",
  },
  {
    story: "a warn colour tuned against navy and worn by a light skin",
    vars: { "--warn-rgb": "250,240,200" },
    skins: ["paper"],
    expect: "a warn chip's text",
  },
  {
    // THE DEFECT THIS FILE FOUND, kept as a case so the fix cannot be undone
    // quietly. `linen` alone, because `linen` alone was under the floor at the
    // hue the pinned group is actually drawn at. The other three light skins
    // sat just over it and are not in a position to prove anything.
    story: "the pinned group's name at the raw warn colour",
    pair: "a pinned group's name",
    fg: "var(--warn)",
    skins: ["linen"],
  },
];

function selftest(skins, bound) {
  let broken = 0;
  for (const c of BROKEN) {
    const want = c.expect || c.pair;
    const pairs = c.pair
      ? bound.filter((p) => p.what === c.pair).map((p) => ({ ...p, fg: c.fg }))
      : bound;
    if (c.pair && pairs.length !== 1) {
      console.error(`SELFTEST BROKEN: no pair named "${c.pair}". the case is stale.`);
      broken++;
      continue;
    }
    for (const skin of c.skins) {
      if (!skins.has(skin)) {
        console.error(`SELFTEST BROKEN: there is no skin called "${skin}".`);
        broken++;
        continue;
      }
      const vars = { ...skins.get(skin), ...(c.vars || {}) };
      const caught = judgeAll(pairs, new Map([[skin, vars]]))
        .filter((r) => r.bad.length > 0)
        .map((r) => r.what);
      if (!caught.includes(want)) {
        console.error(`SELFTEST FAILED on ${skin}: ${c.story}`);
        console.error(`  expected "${want}" to fail, and it did not.`);
        console.error(caught.length > 0
          ? `  what failed instead: ${caught.join(", ")}`
          : "  nothing failed at all, so this check is measuring the wrong thing.");
        broken++;
      }
    }
  }
  return broken;
}

// ── run ────────────────────────────────────────────────────────────────────
const page = process.argv[2];
const report = process.argv.includes("--report");
if (!page) {
  console.error("usage: node scripts/check-contrast.js <index.html> [--report]");
  process.exit(2);
}

const css = fs.readFileSync(page, "utf8");
const skins = parseSkins(css);
if (skins.size < 2) {
  console.error(`found ${skins.size} skins in ${page}. expected the whole set: the parser is broken.`);
  process.exit(1);
}

let bound;
try {
  bound = bindRules(PAIRS, parseRules(css));
} catch (e) {
  console.error(`the pair table no longer matches the stylesheet: ${e.message}`);
  process.exit(1);
}

const selfBroken = selftest(skins, bound);
if (selfBroken > 0) {
  console.error(`\nthe contrast check cannot detect ${selfBroken} defect(s) it is supposed to. ` +
    "fix the check before trusting the run below.");
  process.exit(1);
}

const results = judgeAll(bound, skins);
let failures = 0;
for (const r of results) {
  for (const b of r.bad) console.error(`FAIL ${r.label}: ${b}`);
  if (r.bad.length > 0) failures++;
}

if (report) {
  for (const [skin] of skins) {
    console.log(`\n${skin}`);
    for (const r of results.filter((x) => x.skin === skin)) {
      console.log(`  ${(r.ratio === null ? NaN : r.ratio).toFixed(2).padStart(6)}  ${r.what}` +
        (r.rest === null || r.rest === undefined ? "" : ` (at rest ${r.rest.toFixed(2)})`));
    }
  }
}

if (failures > 0) {
  console.error(`\n${failures} contrast failures across ${skins.size} skins.`);
  process.exit(1);
}
// The known defects are named on a green run too. A pin that goes quiet is a
// pin that becomes permanent.
const known = results.filter((r) => r.known);
console.log(`contrast: ${results.length} pairs across ${skins.size} skins, all above their floor, ` +
  `and ${BROKEN.length} deliberately broken colours all caught.`);
if (known.length > 0) {
  const what = [...new Set(known.map((r) => r.what))];
  console.log(`still open, pinned at the ratio it scores today: ${what.join("; ")} ` +
    `(${known.length} skin/pair combinations). see docs/css-nits.md.`);
}
