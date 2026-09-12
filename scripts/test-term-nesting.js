// What the terminal strip's grouping DRAWS, run rather than read.
//
// The tree was already checked, and checking the tree is what let a mis-nested
// strip through. `termTree` can be perfectly right while the markup that comes
// out of `termNodeHTML` puts a row under a heading it does not belong to, and
// nothing about the tree says so: the failure is entirely in how the tree is
// flattened into headings and containers.
//
// So this asserts the RENDERED nesting. It runs the real functions over a list
// of sessions, parses the html they produce, and for every row it works out
// which headings the row is actually inside by walking the `.tnest` containers
// around it. The invariant is one sentence:
//
//   A ROW IS INSIDE THE HEADINGS THAT SPELL ITS OWN PATH, AND NO OTHERS.
//
// The case that fails whenever the flattening is wrong is a group with several
// members followed by a single member sibling at a shallower level, which on
// the real board is `dovholuknf/atrium` with six sessions in it followed by
// `openziti-test-kitchen` with one. A renderer that stops drawing a heading
// because the group holds one row, without also CLOSING the containers that
// row is not in, leaves the single member inside the previous group.
//
// There is no browser here and this repo has no npm dependencies, so the html
// is parsed by the small parser below. It understands tags, double quoted
// attributes and text, which is all this markup is.
const { boardScript } = require("./board-source.js");

let bad = 0;
function fail(msg) {
  console.error("FAIL: " + msg);
  bad++;
}
function is(got, want, what) {
  if (got !== want) fail(`${what}: expected ${JSON.stringify(want)}, got ${JSON.stringify(got)}`);
}

// ── the real functions, lifted out of the board ─────────
//
// SEARCHED FOR BY NAME, never sliced at a fixed offset. A checker that reads a
// window of characters from the top of a file fails the day somebody adds a
// paragraph of comment, which has nothing to do with what it protects. Each
// function is taken whole, from its own `function` keyword to the next one at
// the left margin.
const page = boardScript();

function lift(name) {
  const at = page.indexOf(`function ${name}(`);
  if (at < 0) {
    console.error(`FAIL: the board has no ${name}. The strip's grouping was renamed or ` +
      `removed, and this test cannot say anything about what it draws. Point it at ` +
      `whatever replaced it.`);
    process.exit(1);
  }
  const end = page.indexOf("\nfunction ", at + 1);
  return page.slice(at, end < 0 ? page.length : end);
}

// The localStorage key the folds are kept under, lifted for the same reason as
// the functions: `termFolded` reads it, and a copy here would go stale.
const foldKeyAt = page.indexOf("const TERM_FOLDED =");
if (foldKeyAt < 0) {
  console.error("FAIL: TERM_FOLDED is gone, so which groups are folded is kept somewhere " +
    "else now. This test folds a group through localStorage and needs to know where.");
  process.exit(1);
}
const foldKey = page.slice(foldKeyAt, page.indexOf("\n", foldKeyAt));

const names = ["shortLabel", "termPathOf", "termTree", "termRow", "termHeading", "termFolded",
  "termNodeHTML", "termCount", "termGroupsHTML"];
// Whatever sits between two functions comes along with the one above it, so
// the key may already be in there. Declaring it twice is a syntax error, which
// is a confusing way to be told the file was reordered.
const lifted = names.map(lift).join("\n");
const src = (lifted.includes("const TERM_FOLDED =") ? "" : foldKey + "\n") + lifted;

// The globals the strip reaches for. Everything here is either a browser thing
// or a function from another part of the board, and none of it is what is
// being tested: what a row's colours are does not change which heading it is
// under.
const store = new Map();
const localStorage = {
  getItem: (k) => (store.has(k) ? store.get(k) : null),
  setItem: (k, v) => store.set(k, v),
};
const esc = (s) => String(s)
  .replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;");
// The label is `terminalLabel`'s job and it has its own tests. Here it is the
// INPUT: a fixture says what a session's label is and this hands it over.
const terminalLabel = (t) => t.label || "";
const built = new Function("localStorage", "esc", "terminalLabel", "themeFor", "runnerMark",
  "poppedOut", "termTask",
  src + "\nreturn { termGroupsHTML, termPathOf };")(
  localStorage, esc, terminalLabel,
  () => ({ cursor: "#fff", background: "#000" }), () => "", () => false, null);
const { termGroupsHTML, termPathOf } = built;

// ── enough html parsing to see the nesting ──────────────

// Tags, double quoted attributes and text. The strip's markup writes tags
// across several lines and closes them with the `>` on a line of its own, so
// whitespace inside a tag is allowed anywhere a space is.
function parse(html) {
  const root = { tag: "#root", attrs: {}, kids: [], text: "" };
  const stack = [root];
  const token = /<\/([a-zA-Z0-9]+)\s*>|<([a-zA-Z0-9]+)((?:\s+[a-zA-Z0-9-]+="[^"]*")*)\s*>/g;
  let at = 0, m;
  const text = (s) => { if (s.trim()) stack[stack.length - 1].text += s; };
  while ((m = token.exec(html))) {
    text(html.slice(at, m.index));
    at = token.lastIndex;
    if (m[1]) {
      if (stack.length < 2) throw new Error("unbalanced close </" + m[1] + ">");
      stack.pop();
      continue;
    }
    const el = { tag: m[2], attrs: {}, kids: [], text: "" };
    const attr = /([a-zA-Z0-9-]+)="([^"]*)"/g;
    let a;
    while ((a = attr.exec(m[3]))) el.attrs[a[1]] = a[2];
    stack[stack.length - 1].kids.push(el);
    stack.push(el);
  }
  text(html.slice(at));
  if (stack.length !== 1) throw new Error("unclosed tag in the strip's markup");
  return root;
}

const cls = (el) => (el.attrs.class || "").split(/\s+/);
const has = (el, name) => cls(el).includes(name);
const deepText = (el, wanted) => {
  if (has(el, wanted)) return el.text;
  for (const k of el.kids) {
    const found = deepText(k, wanted);
    if (found !== null) return found;
  }
  return null;
};

// Every row that got drawn, with the headings it is actually INSIDE.
//
// A `.tnest` is the container a heading opens, so the heading it belongs to is
// the button immediately before it. Walking the containers is the only honest
// way to ask this question: it is what the eye does when it reads the strip,
// and it is what an indent and a guide line down the left say on screen.
function drawn(html) {
  const rows = [], heads = [];
  const walk = (el, under) => {
    for (let i = 0; i < el.kids.length; i++) {
      const kid = el.kids[i];
      if (kid.tag === "button" && has(kid, "tgroup")) {
        heads.push({
          name: deepText(kid, "tgname"),
          count: Number(deepText(kid, "tgcount")),
          path: (kid.attrs.onclick || "").replace(/^toggleTermGroup\('|'\)$/g, ""),
          under: under.slice(),
        });
        continue;
      }
      if (kid.tag === "div" && has(kid, "tnest")) {
        // The heading this container belongs to, which is the button before it.
        const prev = el.kids[i - 1];
        const owner = prev && prev.tag === "button" ? deepText(prev, "tgname") : "(no heading)";
        walk(kid, under.concat([owner]));
        continue;
      }
      if (kid.tag === "div" && has(kid, "tab")) {
        rows.push({
          id: kid.attrs["data-id"],
          under: under.slice(),
          shown: deepText(kid, "tname"),
          full: (kid.attrs && deepText(kid, "tname") !== null)
            ? findTitle(kid) : "",
        });
        continue;
      }
      walk(kid, under);
    }
  };
  walk(parse(html), []);
  return { rows, heads };
}

// The `title` on the name, which is the whole label. A row drawn under the
// headings that spell its path shows its LEAF, and the title is where the rest
// of it still is.
function findTitle(el) {
  if (has(el, "tname")) return el.attrs.title || "";
  for (const k of el.kids) {
    const found = findTitle(k);
    if (found !== null) return found;
  }
  return null;
}

// ── the fixtures ────────────────────────────────────────

// Labels as `terminalLabel` writes them: where a session lives, then a colon,
// then the branch. These are the real board's, which is where the bug was
// seen.
const LIVE = [
  "github/dovholuknf/atrium:b2-01-favicon",
  "github/dovholuknf/atrium:b2-14-short-write",
  "github/dovholuknf/atrium:atrium-backlog",
  "github/dovholuknf/dotfiles:main",
  "github/openziti/desktop-edge-win:sec-report-sep",
  "github/openziti/desktop-edge-win:advisory-GHSA-7gg5",
  "github/openziti/ziti-sdk-csharp:no-preview-build",
  "github/openziti-test-kitchen/docpreview:aug-revisions",
  "github/netfoundry/docusaurus-shared:add-cni-draft",
];
const sessions = (labels) => labels.map((label, i) => ({ id: "t" + i, label, supervised: true }));

// What the invariant says the strip must look like, worked out from the labels
// alone rather than written down twice: a row is under the headings that spell
// its own path, and it shows its leaf.
function expected(list) {
  const want = new Map();
  for (const t of list) {
    const { segs, leaf } = termPathOf(t);
    want.set(t.id, { under: segs.length ? segs : ["uncategorized"], leaf });
  }
  return want;
}

function checkNesting(list, what, opts = {}) {
  const { rows, heads } = drawn(termGroupsHTML(list));
  const want = expected(list);
  const hidden = new Set(opts.hidden || []);
  const shownIds = new Set(rows.map(r => r.id));

  for (const t of list) {
    const w = want.get(t.id);
    const row = rows.find(r => r.id === t.id);
    if (hidden.has(t.id)) {
      if (row) fail(`${what}: ${t.label} is inside a folded group and was drawn anyway.`);
      continue;
    }
    if (!row) {
      fail(`${what}: ${t.label} was not drawn at all.`);
      continue;
    }
    is(row.under.join("/"), w.under.join("/"),
      `${what}: which headings ${t.label} is inside`);
    is(row.shown, w.leaf, `${what}: what ${t.label} shows on the row`);
  }
  is(rows.length, list.length - hidden.size, `${what}: how many rows were drawn`);

  // A heading says where it is twice, once by where it sits and once in the
  // path it folds. Those two disagreeing is the same bug seen from the other
  // side: the container is open somewhere its name does not belong.
  for (const h of heads) {
    if (h.path === "uncategorized") continue;
    is(h.path, h.under.concat([h.name]).join("/"),
      `${what}: the fold path on the ${h.name} heading`);
    const inside = list.filter(t => {
      const segs = termPathOf(t).segs;
      return segs.slice(0, h.under.length + 1).join("/") === h.path;
    }).length;
    is(h.count, inside, `${what}: the count on the ${h.name} heading`);
  }
  return { rows, heads, shownIds };
}

// 1. THE CASE THE WHOLE THING IS FOR. A group with several members, then a
//    single member sibling one level shallower. `dovholuknf/atrium` holds
//    three sessions and `openziti-test-kitchen` holds one, and the one landed
//    inside `atrium` for as long as the bug existed, wearing the rest of its
//    own path on the row because the renderer knew it had segments it had not
//    turned into headings.
{
  const list = sessions(LIVE);
  const { rows, heads } = checkNesting(list, "the real board's shape");
  const kitchen = rows.find(r => r.full.startsWith("github/openziti-test-kitchen"));
  is(kitchen ? kitchen.under.join("/") : "",
    "github/openziti-test-kitchen/docpreview",
    "the single member org after a multi member one");
  is(kitchen ? kitchen.shown : "", "aug-revisions",
    "a row under its own headings shows the leaf and nothing else");
  // And the headings themselves are the tree, not a run: one `github`, one
  // `openziti`, and an org for every org.
  is(heads.filter(h => h.name === "github").length, 1, "how many github headings");
  is(heads.filter(h => h.under.join("/") === "github").map(h => h.name).sort().join(","),
    "dovholuknf,netfoundry,openziti,openziti-test-kitchen", "the orgs under github");
}

// 2. ORDER INDEPENDENT. The strip is drawn from a list sorted by pinned first
//    and then by name or by activity, and none of those is the grouping key. A
//    grouper that walks the list comparing each row with the one before it
//    nests correctly only while the list happens to arrive grouped, and starts
//    mis-nesting the moment somebody clicks `sorted by activity`.
{
  const forwards = sessions(LIVE);
  const back = sessions(LIVE.slice().reverse());
  const shuffled = sessions([LIVE[4], LIVE[0], LIVE[8], LIVE[2], LIVE[6],
    LIVE[1], LIVE[7], LIVE[3], LIVE[5]]);
  const key = (list) => drawn(termGroupsHTML(list)).rows
    .map(r => r.under.join("/") + "|" + r.full).sort().join("\n");
  checkNesting(back, "the list arriving backwards");
  checkNesting(shuffled, "the list arriving in activity order");
  is(key(back), key(forwards), "the same sessions in another order nest the same way");
  is(key(shuffled), key(forwards), "and in any other order");
}

// 3. FOLDING HIDES ROWS. IT DOES NOT MOVE THEM. What is left has to be nested
//    exactly as it was, which is the half of folding that fails quietly: a
//    container closed one level too early spills everything after it into the
//    heading above.
{
  store.set("atrium.termfolded", JSON.stringify(["github/dovholuknf"]));
  const list = sessions(LIVE);
  const hidden = list.filter(t => t.label.startsWith("github/dovholuknf")).map(t => t.id);
  const { heads } = checkNesting(list, "with an org folded", { hidden });
  const folded = heads.find(h => h.name === "dovholuknf");
  is(folded ? folded.count : -1, 4, "a folded heading still says what is inside it");
  store.delete("atrium.termfolded");
}

// 4. A SESSION WITH NO PATH IS NOT SOMEBODY ELSE'S. A directory atrium cannot
//    read a forge and an org out of has no headings to sit under, and the
//    place it must not end up is inside whichever group happens to be open
//    when it is drawn.
{
  const list = sessions(LIVE.concat(["worktrees/random-help"]));
  const { rows } = checkNesting(list, "a session with no path");
  const loose = rows.find(r => r.full === "worktrees/random-help");
  is(loose ? loose.under.join("/") : "", "uncategorized",
    "a pathless session is under the catch-all and not under the last group");
}

// 5. NOTHING BUT PATHLESS SESSIONS. No headings at all then, and the rows show
//    their whole label, since there is nothing above them saying where they
//    are.
{
  const list = sessions(["scratch", "somewhere-else"]);
  const { rows, heads } = drawn(termGroupsHTML(list));
  is(heads.length, 0, "no headings when there is nothing to group");
  is(rows.every(r => r.under.length === 0), true, "the rows are at the top level");
  is(rows.map(r => r.shown).join(","), "scratch,somewhere-else",
    "and each says its whole label");
}

// 6. ONE MEMBER ALL THE WAY DOWN. Every level here holds exactly one thing, so
//    a renderer that skips a heading for a group of one draws no headings at
//    all and puts the row at the top of the strip, which is the same bug with
//    nothing else in the list to hide it.
{
  const list = sessions(["github/openziti/ziti-openwrt:firmware-upgrade-recovery-docs"]);
  const { rows, heads } = checkNesting(list, "a single session three levels down");
  is(heads.map(h => h.name).join("/"), "github/openziti/ziti-openwrt",
    "every level of a chain of one gets its heading");
  is(rows[0].under.join("/"), "github/openziti/ziti-openwrt",
    "and the row is inside all three of them");
}

if (bad) {
  console.error(`${bad} assertion(s) failed. The strip draws rows under headings they are ` +
    `not in, which is what the indent and the guide line down the left say they are.`);
  process.exit(1);
}
console.log("the terminal strip nests what it draws.");
