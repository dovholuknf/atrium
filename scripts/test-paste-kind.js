// The kind of a dropped or pasted batch picks the preamble, run rather than read.
//
// pasteKindOf and preambleForFiles are lifted out of js/notes-files.js by name and run in node.
const fs = require("fs");
const path = require("path");

const src = fs.readFileSync(path.join(__dirname, "..", "internal", "api", "web", "js", "notes-files.js"), "utf8");
const from = src.indexOf("function preambleOf(");
const start = src.indexOf("const PASTE_WORDING");
const end = src.indexOf("// pasteSettings fetches");
if (from < 0 || start < 0 || end < 0) { console.error("FAIL: the paste preamble code moved"); process.exit(1); }
const body = src.slice(from, src.indexOf("\n}\n", from) + 3) + src.slice(start, end);
const { pasteKindOf, preambleForFiles, PASTE_WORDING } = new Function(body + "; return { pasteKindOf, preambleForFiles, PASTE_WORDING };")();

let bad = 0;
function is(got, want, what) {
  if (got !== want) { console.error(`FAIL: ${what}: expected ${JSON.stringify(want)}, got ${JSON.stringify(got)}`); bad++; }
}
const f = (name, type) => ({ name, type: type === undefined ? "" : type });

is(pasteKindOf([f("a.md", "text/markdown")]), "text", "markdown by extension");
is(pasteKindOf([f("A.PNG", "image/png")]), "image", "upper case extension");
is(pasteKindOf([f("spec.pdf")]), "pdf", "pdf by extension, empty MIME");
is(pasteKindOf([f("x.tgz")]), "archive", "archive by extension");
is(pasteKindOf([f("pasted.png", "image/png")]), "image", "clipboard image");
is(pasteKindOf([f("pasted.svg+xml", "image/svg+xml")]), "image", "odd subtype falls to MIME");
is(pasteKindOf([f("notes", "text/plain")]), "text", "no extension, text MIME");
is(pasteKindOf([f("doc", "application/pdf")]), "pdf", "no extension, pdf MIME");
is(pasteKindOf([f("thing.xyz", "")]), "other", "unknown extension, empty MIME");
is(pasteKindOf([f("a.png", "image/png"), f("b.jpg", "image/jpeg")]), "image", "same kind batch");
is(pasteKindOf([f("a.png", "image/png"), f("b.md", "text/markdown")]), "mixed", "mixed batch");
is(pasteKindOf([]), "other", "empty batch");

const batch = [f("a.md", "text/markdown")];
is(preambleForFiles({}, batch), PASTE_WORDING.text, "unset gives the kind wording");
is(preambleForFiles({ paste_preamble: "look: " }, batch), "look: ", "custom string wins");
is(preambleForFiles({ paste_preamble: "none" }, batch), "", "none gives nothing");

if (bad) process.exit(1);
console.log("paste kind: extension, MIME, empty MIME and mixed batches pick the wording, and a custom preamble still wins.");
