// By age, a tree files under the newest activity anywhere in it, at any depth.
const { boardScript } = require("./board-source.js");

const page = boardScript();
function lift(name) {
  const at = page.indexOf(`function ${name}(`);
  if (at < 0) { console.error(`FAIL: the board has no ${name}`); process.exit(1); }
  const end = page.indexOf("\nfunction ", at + 1);
  return page.slice(at, end < 0 ? page.length : end);
}

const ago = ms => new Date(Date.now() - ms).toISOString();
const DAY = 24 * 3600000;
const root = { id: "root", last_activity_at: ago(DAY) };
const mid = { id: "mid", last_activity_at: ago(DAY) };
const leaf = { id: "leaf", last_activity_at: ago(10 * 60000) };
const alone = { id: "alone", last_activity_at: ago(DAY) };

const src = `var termKids = new Map([["root", [mid]], ["mid", [leaf]]]);
${lift("treeActivityAt")}
${lift("recencyBucket")}
return { recencyBucket };`;
const { recencyBucket } = new Function("mid", "leaf", src)(mid, leaf);

let bad = 0;
function is(got, want, what) {
  if (got !== want) { console.error(`FAIL: ${what}: expected ${want}, got ${got}`); bad++; }
}
is(recencyBucket(root), "1h", "root of a three deep tree, only the grandchild recent");
is(recencyBucket(mid), "1h", "middle of the tree");
is(recencyBucket(leaf), "1h", "the grandchild itself");
is(recencyBucket(alone), "yesterday", "a card with no children keeps its own age");
if (bad) process.exit(1);
console.log("test-age-tree: ok");
