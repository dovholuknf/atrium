// The new agent dialog reads /v1/harnesses on every open, so a room the hub dropped from an earlier read still shows up.
const { boardScript } = require("./board-source.js");
const page = boardScript();

function liftFn(name) {
  const at = page.indexOf(`async function ${name}(`);
  if (at < 0) { console.error(`FAIL: the board has no ${name}`); process.exit(1); }
  let depth = 0, seen = false;
  for (let i = at; i < page.length; i++) {
    if (page[i] === "{") { depth++; seen = true; }
    else if (page[i] === "}") { depth--; if (seen && depth === 0) return page.slice(at, i + 1); }
  }
  console.error(`FAIL: ${name}'s braces do not close`);
  process.exit(1);
}

let bad = 0;
function is(got, want, what) {
  if (got !== want) { console.error(`FAIL: ${what}: expected ${want}, got ${got}`); bad++; }
}

async function main() {
  let reads = 0, rows = [{ id: "claude", room: "room-d", enabled: true }];
  const api = async () => { reads++; return { harnesses: rows }; };
  const state = new Function("api", `let allHarnesses = [], modelsSeen = [], modelPicker = [];\n${liftFn("loadHarnesses")}\n` +
    "return { load: loadHarnesses, rows: () => allHarnesses };")(api);

  await state.load();
  await state.load();
  is(reads, 1, "a plain load reads once and keeps the answer");
  rows = rows.concat([{ id: "codex", room: "claude-room-e", enabled: true }]);
  await state.load(true);
  is(reads, 2, "a fresh load reads again");
  is(state.rows().length, 2, "a fresh load takes the new rows");

  const open = liftFn("openNewAgent");
  is(/loadHarnesses\(true\)/.test(open), true, "opening the dialog asks for a fresh read");
}

main().then(() => {
  if (bad) process.exit(1);
  console.log("ok: launch dialog reads runners fresh");
});
