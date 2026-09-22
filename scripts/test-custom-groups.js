// Custom groups on the board: a group with no cards is still drawn, and a card
// tagged for two groups is drawn under both. Runs the real `cardsHTML` with its
// neighbours stubbed, so it says what the board draws rather than what a copy
// of it would.
const { boardScript } = require("./board-source.js");

let bad = 0;
function fail(msg) {
  console.error("FAIL: " + msg);
  bad++;
}

const page = boardScript();

function lift(start, end) {
  const at = page.indexOf(start);
  if (at < 0) {
    console.error(`FAIL: the board no longer has \`${start}\`. Point this test at whatever replaced it.`);
    process.exit(1);
  }
  const stop = page.indexOf(end, at + start.length);
  return page.slice(at, stop < 0 ? page.length : stop + end.length);
}

const src = [
  lift("function cardsHTML(", "\n}"),
  lift("function seedGroups(", "\n}"),
  lift("function emptyGroupHint(", "\n}"),
  lift("function byRank(", "\n}"),
].join("\n");

const stubs = {
  esc: s => String(s),
  cardHTML: t => `<card ${t.id}>`,
  pinnedGroupHTML: () => "",
  cardTieBreak: (a, b) => String(a.id).localeCompare(String(b.id)),
  groupingPrefs: () => ({ mode: "custom", groups: ["one", "two", "active"] }),
  isFolded: () => false,
  groupHue: () => 0,
  UNTAGGED: "untagged",
};
const names = Object.keys(stubs);
const { cardsHTML } = new Function(...names, src + "\nreturn { cardsHTML };")(...names.map(n => stubs[n]));

const groups = ["one", "two", "active"];
const set = new Set(groups);
const at = new Map(groups.map((n, i) => [n, i]));
const g = {
  many: true,
  handOrdered: true,
  always: groups.slice(),
  of: t => {
    const mine = (t.tags || []).filter(x => set.has(x));
    return mine.length ? mine : ["untagged"];
  },
  cmp: (a, b) => (a === "untagged") - (b === "untagged") || (at.get(a) ?? 1e9) - (at.get(b) ?? 1e9),
};

const html = cardsHTML([
  { id: "both", tags: ["one", "two"] },
  { id: "loose", tags: [] },
], g, "col");

const section = name => {
  const i = html.indexOf(`data-fold="proj:${name}"`);
  if (i < 0) return null;
  const j = html.indexOf("</details>", i);
  return html.slice(i, j);
};

const active = section("active");
if (!active) fail("the empty group `active` was not drawn at all");
else if (!active.includes("nothing in it yet")) fail("the empty group `active` has no hint: " + active);

for (const name of ["one", "two"]) {
  const s = section(name);
  if (!s || !s.includes("<card both>")) fail(`the card tagged one and two is missing from \`${name}\``);
}
const untagged = section("untagged");
if (!untagged || !untagged.includes("<card loose>")) fail("a card in no group is missing from untagged");

const order = groups.concat("untagged").map(n => html.indexOf(`data-fold="proj:${n}"`));
if (order.some((v, i) => i && v < order[i - 1])) fail("the groups are not in the order the list names them: " + order);

// THE SORT DOES NOT REACH A GROUP YOU MADE. The cards arrive in the sort's
// order, which here is the reverse of their ranks, and come out by rank.
const ranked = cardsHTML([
  { id: "r3", tags: ["one"], rank: 3 },
  { id: "r1", tags: ["one"], rank: 1 },
  { id: "r2", tags: ["one"], rank: 2 },
  { id: "u2", tags: [], rank: 1 },
  { id: "u1", tags: [], rank: 2 },
], g, "col");
const pos = id => ranked.indexOf(`<card ${id}>`);
if (!(pos("r1") < pos("r2") && pos("r2") < pos("r3"))) {
  fail("a custom group is not in rank order: " + ["r1", "r2", "r3"].map(pos));
}
// The cards in none of your groups are the board's own heap and keep the sort.
if (!(pos("u2") < pos("u1"))) fail("untagged cards were reordered by rank, not left to the sort");

if (bad) process.exit(1);
console.log("custom groups: an empty one is drawn, a card in two is drawn in both, and rank holds the order.");
