// Check that stack and terminal strip order is independent of input order,
// including ties on every sort field. Also exercise cards with missing fields
// so a comparator cannot break the whole repaint.
const { boardScript } = require("./board-source.js");

let bad = 0;
function fail(msg) {
  console.error("FAIL: " + msg);
  bad++;
}

// The real code, searched for by name rather than sliced at an offset, for the
// same reason as the other checkers here: a paragraph of comment added above it
// must not break the test that protects it.
const page = boardScript();

function lift(start, end) {
  const at = page.indexOf(start);
  if (at < 0) {
    console.error(`FAIL: the board no longer has \`${start}\`. The sorting this test ` +
      `protects was renamed or moved, and the test cannot say anything about it. ` +
      `Point it at whatever replaced it.`);
    process.exit(1);
  }
  const stop = page.indexOf(end, at + start.length);
  return page.slice(at, stop < 0 ? page.length : stop + end.length);
}

const tieBreak = lift("function cardTieBreak(", "\n}");

// Tie every sort field and vary creation time against input order, so the
// expected result requires the tiebreak.
const tied = (over) => over.map(id => ({
  id,
  created_at: { a: "2026-09-11T10:00:01Z", b: "2026-09-11T10:00:02Z",
    d: "2026-09-11T10:00:02Z", c: "2026-09-11T10:00:03Z" }[id],
  display_title: "same", label: "same", worktree: "/w", repo: "w", runner: "claude",
  status: "running", idle_seconds: 30, wait_seconds: 5, tags: ["x"], supervised: true
}));

// Oldest first, and the id decides between the two raised in the same second.
// Spelled out rather than left to "the two runs match", since two runs that are
// both wrong the same way would match.
const TIED_ORDER = "abdc";
const arrived = ["c", "a", "d", "b"];

// Stub askRank: permission state is tested elsewhere; this tests sorting ties.
const askRank = (t) => (t.asking ? 0 : 2);
const { STACK_SORTS, cardTieBreak } = new Function("askRank",
  tieBreak + "\n" + lift("const STACK_SORTS = {", "\n};") +
  "\nreturn { STACK_SORTS, cardTieBreak };")(askRank);

// The board sorts through one call, and this is it. Kept in one place so the
// test says what the board does rather than what each axis does on its own.
const stackOrder = (mode, list) =>
  [...list].sort((a, b) => STACK_SORTS[mode].cmp(a, b) || cardTieBreak(a, b))
    .map(t => t.id).join("");

for (const mode of Object.keys(STACK_SORTS)) {
  const forwards = stackOrder(mode, tied(arrived));
  const backwards = stackOrder(mode, tied([...arrived].reverse()));
  if (forwards !== backwards) {
    fail(`the stack sorted by ${mode} depends on the order the cards arrived in: ` +
      `${forwards} one way, ${backwards} the other. Cards that tie on ${mode} move ` +
      `around on a repaint that changed nothing.`);
  }
  if (forwards !== TIED_ORDER) {
    fail(`the stack sorted by ${mode} broke the tie as ${forwards}, expected ` +
      `${TIED_ORDER}: oldest first, then by id.`);
  }
  try {
    stackOrder(mode, [...tied(arrived), { id: "", created_at: "" }]);
  } catch (e) {
    fail(`the stack sorted by ${mode} throws on a card with no fields: ${e.message}. ` +
      `One such card takes the whole repaint with it.`);
  }
}

// Pass sort mode in a mutable object so tests can toggle it. Stub waiting
// state and labels, whose own behavior is tested elsewhere.
const strip = new Function("isWaiting", "terminalLabel", "mode",
  tieBreak + "\n" + lift("function termOrder(", "\n}") +
  "\nreturn (tasks) => { sortByActivity = mode.on; return termOrder(tasks); };")(
  (t) => t.status === "needs-input", (t) => t.label || "", (globalThis.__mode = { on: true }));

// `sortByActivity` is declared in terminal-list.js and lifted out of its
// declaration, so the function above needs it to exist. Declared here as the
// same kind of binding the board gives it.
const mode = globalThis.__mode;

const stripOrder = (list) => strip(tied(list).slice()).map(t => t.id).join("");

for (const on of [true, false]) {
  mode.on = on;
  const what = on ? "by activity" : "by name";
  const forwards = stripOrder(arrived);
  const backwards = stripOrder([...arrived].reverse());
  if (forwards !== backwards) {
    fail(`the strip sorted ${what} depends on the order the sessions arrived in: ` +
      `${forwards} one way, ${backwards} the other. The strip reshuffles between two ` +
      `polls that said the same thing, under a cursor already on its way to a tab.`);
  }
  if (forwards !== TIED_ORDER) {
    fail(`the strip sorted ${what} came out ${forwards}, expected ${TIED_ORDER}: ` +
      `oldest first, then by id.`);
  }
  try {
    strip([...tied(arrived), { id: "", created_at: "" }]);
  } catch (e) {
    fail(`the strip sorted ${what} throws on a session with no fields: ${e.message}. ` +
      `One such row takes the whole strip with it.`);
  }
}

// The name sort has to actually sort, which the tie fixtures above cannot show:
// they all carry the same label on purpose. A session named `zed` handed over
// first belongs last.
mode.on = false;
const named = [
  { id: "1", created_at: "2026-09-11T10:00:01Z", label: "zed" },
  { id: "2", created_at: "2026-09-11T10:00:02Z", label: "alpha" },
  { id: "3", created_at: "2026-09-11T10:00:03Z", label: "mid" }
];
const byName = strip(named.slice()).map(t => t.label).join(",");
if (byName !== "alpha,mid,zed") {
  fail(`the strip sorted by name came out ${byName}, expected alpha,mid,zed. The ` +
    `button says the list is sorted by name and it is in whatever order the poll ` +
    `delivered.`);
}

// Pinned still wins, in either mode, and everything decided before it survives.
mode.on = true;
const pinned = tied(arrived);
pinned.find(t => t.id === "c").pinned = true;
const withPin = strip(pinned.slice()).map(t => t.id).join("");
if (withPin !== "cabd") {
  fail(`pinning moved more than the pinned row: ${withPin}, expected cabd. The pin ` +
    `pass is meant to lift one row and leave the order underneath it alone.`);
}

if (bad) {
  process.exit(1);
}
console.log("the stack and the strip sort the same way twice.");
