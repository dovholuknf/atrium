// The board's headless buckets, one file per page (scripts/test-buckets.json names them and the units each owns). Each file
// exports `units`, [{ name, run(ctx) }] in suite order, with ctx = { browser, base }, and `sections`, the section names
// HEADLESS_ONLY takes. The harness (scripts/test-board-headless.js) still decides the order the units run in and runs each
// through its own unit(), so a bucket file moved here changes nothing about the run.
//
//   node scripts/board-tests/index.js chat pulls     those buckets, and nothing else
//   node scripts/board-tests/chat.js                 one bucket
//
// A unit not listed in a bucket file is still in the harness, waiting to be moved.

const names = ["backlog", "boot", "chat", "deploy", "homes", "hubrepos", "items", "launch", "merge", "pulls", "rooms", "changereq", "route", "settings", "usage"];
const buckets = {};
for (const n of names) buckets[n] = require("./" + n + ".js");

const runs = {};
const sections = {};
for (const [n, b] of Object.entries(buckets)) {
  for (const u of b.units) {
    if (runs[u.name]) throw new Error("unit " + u.name + " is in two bucket files: " + runs[u.name].bucket + " and " + n);
    runs[u.name] = { bucket: n, run: u.run };
  }
  Object.assign(sections, b.sections);
}

// the function the harness hands to unit(): run the named unit of a bucket file
const run = (name, ctx) => {
  if (!runs[name]) throw new Error("unit " + name + " is in no file of scripts/board-tests");
  return runs[name].run(ctx);
};

module.exports = { names, buckets, sections, run };

if (require.main === module) {
  const want = process.argv.slice(2);
  const bad = want.filter(n => !buckets[n]);
  if (bad.length) { console.error("no such bucket: " + bad.join(", ") + ". the buckets are: " + names.join(" ")); process.exit(2); }
  if (!want.length) { console.error("name the buckets to run: " + names.join(" ")); process.exit(2); }
  process.env.HEADLESS_UNITS = want.flatMap(n => buckets[n].units.map(u => u.name)).join(",");
  require("../test-board-headless.js");
}
