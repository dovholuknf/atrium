// What the board is willing to ask the daemon about, checked against real
// terminal lines.
//
// The board underlines a path in the terminal so it can be clicked, and the
// hard part is NOT UNDERLINING EVERYTHING. The decision itself belongs to
// `files/probe`, which answers whether a candidate is a file in the card, and
// nothing here is allowed to take that decision back by being clever about
// what a filename looks like.
//
// So this checks the two halves that are the board's own:
//
//   The TRIM. A path on a terminal line arrives with punctuation stuck to it:
//   quotes around it, a comma after it, and the `:248:1` a compiler prints.
//   Probing it whole finds nothing, which is the failure that looks exactly
//   like the feature not working.
//
//   The REFUSAL, which has to stay small. Rejecting anything that could be a
//   file is the same bug as deciding by looking, and it fails silently: the
//   path is simply never underlined and there is nothing to see.
//
// The functions are lifted out of the page and run here, because a grep can
// only say the code is present and this has to say it is right.
const fs = require("fs");

const html = fs.readFileSync(process.argv[2], "utf8");
let bad = 0;

function fail(msg) {
  console.error("FAIL: " + msg);
  bad++;
}

// One function, by name, from its `function` line to the first `}` sitting in
// the first column. Brace counting cannot be used: these functions hold
// regular expressions with unbalanced braces inside character classes.
function lift(name) {
  const re = new RegExp("^function " + name + "\\([\\s\\S]*?^\\}$", "m");
  const m = html.match(re);
  if (!m) {
    fail(`there is no ${name} in the page. The board's path links are built out ` +
      `of it, and this check cannot run without it.`);
    return "";
  }
  return m[0] + "\n";
}

const src = ["fileCandidates", "trimCandidate", "worthProbing"].map(lift).join("\n");
if (bad) {
  process.exit(1);
}
const candidates = new Function(src + "return fileCandidates;")();

// The tokens one line offers, in order.
function tokens(line) {
  return candidates(line).map(c => c.token);
}

// Every candidate has to point at itself. An offset that is off by one
// underlines the character beside the path and opens nothing.
function checkOffsets(line) {
  for (const c of candidates(line)) {
    if (line.slice(c.start, c.end) !== c.token) {
      fail(`the candidate ${JSON.stringify(c.token)} points at ` +
        `${JSON.stringify(line.slice(c.start, c.end))} in ${JSON.stringify(line)}. ` +
        `An offset that is off underlines the wrong characters.`);
    }
  }
}

function has(line, want, why) {
  checkOffsets(line);
  if (!tokens(line).includes(want)) {
    fail(`${JSON.stringify(line)} does not offer ${JSON.stringify(want)}. ${why}\n` +
      `  it offered: ${JSON.stringify(tokens(line))}`);
  }
}

function lacks(line, unwanted, why) {
  checkOffsets(line);
  if (tokens(line).includes(unwanted)) {
    fail(`${JSON.stringify(line)} offers ${JSON.stringify(unwanted)}. ${why}`);
  }
}

// ── the trim ────────────────────────────────────────────
//
// Each of these is a line an agent prints constantly, and each one used to
// probe as a string that is not the name of anything.

has("gofmt: internal/api/api.go:248:1: expected declaration",
  "internal/api/api.go",
  "A compiler, a vet run and a test failure all say where they are this way, " +
  "and the line number is not part of the name.");

has(`the file is "internal/api/web/index.html", and it is large.`,
  "internal/api/web/index.html",
  "A quoted path with a comma after it is a path with three characters stuck to it.");

has("see (scripts/ci.sh) for the whole list",
  "scripts/ci.sh",
  "Brackets wrap a path in prose and are not part of it.");

has("wrote build.claude/atrium.exe.",
  "build.claude/atrium.exe",
  "A full stop ends the sentence, not the filename.");

has(`cd d:\\worktrees\\atrium`,
  "d:\\worktrees\\atrium",
  "A Windows path has a colon in it that is not a line number, and dropping it " +
  "leaves a drive letter that resolves to nothing.");

has("Makefile",
  "Makefile",
  "A file with no extension and no separator is still a file. Requiring a dot " +
  "or a slash is deciding by looking, which is what the probe is for.");

// ── the refusal, which stays small ──────────────────────

lacks("run with --check and -v", "--check", "A flag is not a file.");
lacks("run with --check and -v", "-v", "A flag is not a file.");
lacks("see https://zrok.io/docs for more", "https://zrok.io/docs",
  "A URL is not a path in the card, and its host only resembles a filename.");
lacks("built 1.2.3 in 30s", "1.2.3",
  "A version with no letter in it has nothing to open under any reading.");

// ── and everything else is the daemon's to answer ───────
//
// These are the words the endpoint exists to say no to. The board must offer
// them ANYWAY. A local rule that drops them is a rule that is wrong somewhere
// else, and being wrong here means a real file is silently not a link.

for (const [line, want] of [
  ["upgrading to v2.1.263 now", "v2.1.263"],
  ["reserved at zrok.io today", "zrok.io"],
  ["calling foo.bar() again", "foo.bar"],
  ["it took 4.5s to build", "4.5s"],
  [`the model is "Opus 5"`, "Opus"]
]) {
  has(line, want, "It is shaped like a filename, so only the daemon can say it is " +
    "not one. Refusing it here is the same mistake as underlining it.");
}

if (bad) {
  console.error(`\n${bad} path-token rule(s) broken. Each one is either a path that ` +
    `stops being clickable or a page of output with a line under half of it.`);
  process.exit(1);
}
console.log("the terminal's path candidates hold up.");
