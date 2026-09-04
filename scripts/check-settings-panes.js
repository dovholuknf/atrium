// Check the settings partition against the real markup, without a browser.
//
// The nav is built at runtime from `h3.s-section` headings that are DIRECT
// children of the dialog body. If that assumption is wrong the dialog opens
// with one giant pane or none, and no parse check would catch it.
//
// So: pull the settings dialog out of index.html and count what the selector
// would match, plus what each pane would contain.
const fs = require("fs");

const html = fs.readFileSync(process.argv[2], "utf8");

const start = html.indexOf('<dialog id="settings"');
if (start < 0) { console.error("no settings dialog"); process.exit(1); }
const bodyStart = html.indexOf('<div class="dlg-body">', start);
if (bodyStart < 0) { console.error("no dialog body"); process.exit(1); }

// Walk from the body open tag, tracking depth, to find its close.
let i = bodyStart;
let depth = 0;
let bodyEnd = -1;
const tag = /<(\/?)div\b[^]*?>/g;
tag.lastIndex = bodyStart;
let m;
while ((m = tag.exec(html))) {
  depth += m[1] ? -1 : 1;
  if (depth === 0) { bodyEnd = m.index; break; }
}
if (bodyEnd < 0) { console.error("could not find the end of the dialog body"); process.exit(1); }

const body = html.slice(bodyStart, bodyEnd);

// Direct children only: a heading nested inside a field would not be a pane
// boundary at runtime either.
const inner = body.replace(/^<div class="dlg-body">/, "");
let d = 0;
const topLevel = [];
const tokens = inner.split(/(<\/?div\b[^]*?>|<h3 class="s-section">[^]*?<\/h3>)/);
for (const t of tokens) {
  if (!t) continue;
  if (t.startsWith("<h3 class=\"s-section\"")) {
    if (d === 0) topLevel.push(t.replace(/<[^]*?>/g, "").trim());
    continue;
  }
  if (/^<div\b/.test(t)) d++;
  else if (/^<\/div>/.test(t)) d--;
}

console.log("panes the nav would build:");
topLevel.forEach(n => console.log("  - " + n));

if (topLevel.length < 2) {
  console.error("\nFAIL: fewer than two top-level sections, so the nav would not build.");
  process.exit(1);
}

// Every id the settings code reaches for has to still be inside the body.
const wanted = ["s-vol", "s-input", "s-perm", "s-expiry", "s-debounce",
  "s-cardsize", "s-density", "s-sweep", "s-prune", "overlays",
  "s-notify-state", "s-group-by", "s-group-order", "s-group-toggle"];
const missing = wanted.filter(id => !body.includes(`id="${id}"`));
if (missing.length) {
  console.error("\nFAIL: these are reached for but not inside the dialog body: " + missing.join(", "));
  process.exit(1);
}
console.log("\nall " + wanted.length + " settings controls are inside the body.");
console.log("the partition is sound.");
