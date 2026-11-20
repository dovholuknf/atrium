// A pasted link's tile ends in a card or an error, never a counter: an open that finds an earlier one still in flight
// waits its turn, fills the dialog with its own link again, and opens.
const { boardScript } = require("./board-source.js");
const page = boardScript();

function liftFn(name) {
  const at = page.search(new RegExp(`(async )?function ${name}\\(`));
  if (at < 0) { console.error(`FAIL: the board has no ${name}`); process.exit(1); }
  let depth = 0, seen = false;
  for (let i = at; i < page.length; i++) {
    if (page[i] === "{") { depth++; seen = true; }
    else if (page[i] === "}") { depth--; if (seen && depth === 0) return page.slice(at, i + 1); }
  }
  console.error(`FAIL: ${name}'s braces do not close`);
  process.exit(1);
}

const run = async () => {
  const log = [];
  let release;
  const stuck = new Promise(r => { release = r; });
  const env = { log, stuck, openings: new Map([["a", {}], ["b", {}]]) };
  const make = new Function("env", `
    const busyKeys = new Set();
    let quickPasteTurn = Promise.resolve();
    let quickPasteBusy = 0;
    let filled = "";
    const linkOpenings = env.openings;
    const launchNow = async o => { env.log.push("launch " + o.id + " " + filled); if (o.id === "a") await env.stuck; };
    const openLaunch = async (a, b, c, d, o) => { filled = o.url; return true; };
    const recogniseLink = async () => {};
    const quickPasteRoom = r => r;
    const linkOpeningStep = (id, line) => env.log.push("step " + id + " " + line);
    const linkOpeningFail = (id, why) => env.log.push("fail " + id + " " + why);
    const linkOpeningEnd = id => linkOpenings.delete(id);
    ${liftFn("oneAtATime")}
    ${liftFn("quickPasteOpen")}
    return quickPasteOpen;
  `)(env);
  const first = make("a", "", "u/a", {});
  const second = make("b", "", "u/b", {});
  await new Promise(r => setImmediate(r));
  if (log.slice().sort().join() !== "launch a ,step b waiting for the link before it") {
    console.error("FAIL: the second open did not wait for the first: " + JSON.stringify(log));
    process.exit(1);
  }
  release();
  await first;
  await second;
  if (log.filter(x => /^launch b/.test(x)).join() !== "launch b u/b" || env.openings.size) {
    console.error("FAIL: the second open was dropped or opened another link: " + JSON.stringify(log));
    process.exit(1);
  }
  console.log("ok: a second open waits its turn and opens its own link");
};
run();
