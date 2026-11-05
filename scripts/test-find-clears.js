// Closing the find bar must drop every match highlight and the selection, on both close paths.
const fs = require("fs");
const src = fs.readFileSync(__dirname + "/../internal/api/web/js/terminal-links.js", "utf8");
const m = src.match(/function clearFind\(\) \{[\s\S]*?\n\}\n/);
const closeSrc = src.match(/function closeFind\(\) \{[\s\S]*?\n\}\n/);
if (!m || !closeSrc) { console.error("FAIL: clearFind or closeFind not found"); process.exit(1); }
const calls = [];
const bar = { hidden: false };
const run = new Function("document", "placeToasts", "focusTerm", "calls", "bar",
  `let termSearch = { clearDecorations(...a) { calls.push("clearDecorations" + a.length); }, clearActiveDecoration() { calls.push("active"); } };
   let term = { clearSelection() { calls.push("selection"); } };
   ${m[0]}${closeSrc[0]}
   return { closeFind, clearFind };`);
const f = run({ getElementById: () => bar }, () => {}, () => calls.push("focus"), calls, bar);
let bad = 0;
function is(got, want, what) {
  if (got !== want) { console.error(`FAIL: ${what}: expected ${want}, got ${got}`); bad++; }
}
f.closeFind();
is(calls.join(), "clearDecorations0,active,selection,focus", "closeFind");
is(bar.hidden, true, "bar hidden");
calls.length = 0;
f.clearFind();
is(calls.join(), "clearDecorations0,active,selection", "clearFind");
if (bad) process.exit(1);
console.log("ok");
