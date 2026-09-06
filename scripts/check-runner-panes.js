// Check the runners page partitions, against the real markup, without a browser.
//
// Same hazard as the settings dialog and the same reason it needs a script.
// `splitIntoPanes` cuts at every `.r-section` that is a DIRECT child of
// `#runners`. A heading nested one level deeper is not a pane boundary at
// runtime, and nothing about that is a syntax error: the page parses, the
// script runs, and the page comes up as one giant pane with a nav of one.
//
// The `data-pane` label matters as much. These headings carry a help bubble,
// so without an explicit label the nav button would be the heading plus the
// whole tooltip, which is a paragraph in a button.
const fs = require("fs");

const html = fs.readFileSync(process.argv[2], "utf8");

const start = html.indexOf('<div id="runners"');
if (start < 0) { console.error("no runners page"); process.exit(1); }

// Walk from the opening tag, tracking depth, to find its close.
let depth = 0;
let end = -1;
const tag = /<(\/?)div\b[^]*?>/g;
tag.lastIndex = start;
let m;
while ((m = tag.exec(html))) {
  depth += m[1] ? -1 : 1;
  if (depth === 0) { end = m.index; break; }
}
if (end < 0) { console.error("could not find the end of the runners page"); process.exit(1); }

const inner = html.slice(start, end).replace(/^<div id="runners"[^]*?>/, "");

// Direct children only, counted the same way the browser would resolve
// `:scope > .r-section`.
let d = 0;
const topLevel = [];
const tokens = inner.split(/(<div\b[^]*?>|<\/div>)/);
for (const t of tokens) {
  if (!t) continue;
  if (/^<div\b/.test(t)) {
    if (d === 0 && /class="[^"]*\br-section\b/.test(t)) {
      const label = /data-pane="([^"]*)"/.exec(t);
      topLevel.push(label ? label[1] : null);
    }
    // Self-closing div is not a thing in HTML, so every open has a close.
    d++;
  } else if (/^<\/div>/.test(t)) {
    d--;
  }
}

console.log("panes the runners page would build:");
topLevel.forEach(n => console.log("  - " + (n === null ? "(NO data-pane)" : n)));

let fail = false;
if (topLevel.length < 2) {
  console.error("\nFAIL: fewer than two top-level sections, so the nav would not build.");
  fail = true;
}
const unlabelled = topLevel.filter(n => !n).length;
if (unlabelled) {
  console.error("\nFAIL: " + unlabelled + " section(s) have no data-pane, so the nav button " +
    "would carry the help tooltip as its label.");
  fail = true;
}
const seen = new Set();
for (const n of topLevel) {
  if (n && seen.has(n)) {
    console.error("\nFAIL: two sections both call themselves '" + n + "'. The nav would " +
      "show two buttons that switch to the same pane.");
    fail = true;
  }
  seen.add(n);
}

// Every list the render code fills has to end up inside a pane. One left
// outside would be drawn into an element the split moved away from, so it
// would silently never appear.
const filled = ["launchers", "harness-list", "discovered", "fixture-list", "source-list",
  "action-list", "room-list"];
const missing = filled.filter(id => !inner.includes(`id="${id}"`));
if (missing.length) {
  console.error("\nFAIL: these are filled by renderRunners but are not on the page: " +
    missing.join(", "));
  fail = true;
}

if (fail) process.exit(1);
console.log("\nall " + filled.length + " runner lists are inside the page.");
console.log("the runners partition is sound.");
