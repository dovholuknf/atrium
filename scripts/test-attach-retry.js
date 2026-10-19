// attachRetry (lib/helpers.js) against a stub page: a socket that opens on try 3, and one that never opens.
const { attachRetry } = require("./lib/helpers.js");

let failed = 0;
const eq = (a, b, what) => { if (a !== b) { failed++; console.error("FAIL " + what + ": got " + JSON.stringify(a) + ", want " + JSON.stringify(b)); } };

function stubPage(opensOnTry) {
  let tries = 0;
  const world = { termSock: null, termTask: null, attaches: 0, closes: 0 };
  Object.assign(globalThis, {
    termSock: null, termTask: null,
    window: { __switchMarks: [["click", 0]] },
    document: { querySelectorAll: () => [{ dataset: { id: "c1" } }] },
    attachTask: () => { tries++; world.attaches++; globalThis.termTask = { id: "c1" }; globalThis.termSock = { readyState: tries >= opensOnTry ? 1 : 0, close() { world.closes++; } }; },
    closeTerm: () => { globalThis.termSock = null; globalThis.termTask = null; },
  });
  return { evaluate: async (fn, arg) => fn(arg), world };
}

(async () => {
  const lines = [];
  const log = console.log;
  console.log = s => lines.push(String(s));
  const check = () => termSock && termSock.readyState === 1 && termTask && termTask.id === "c1";
  try {
    const a = stubPage(3);
    await attachRetry(a, { unit: "stubA", id: "c1", check });
    const miss = lines.filter(l => l.startsWith("ATTACHRETRY "));
    eq(a.world.attaches, 3, "attaches on a socket that opens on try 3");
    eq(miss.length, 2, "ATTACHRETRY lines before the recovery");
    const j = JSON.parse(miss[0].slice("ATTACHRETRY ".length));
    eq(j.unit + "/" + j.try + "/" + j.termSock + "/" + j.termTask + "/" + j.rowInDom, "stubA/1/0/c1/true", "first miss fields");
    eq(Array.isArray(j.switchMarks), true, "miss carries switchMarks");
    eq(typeof j.ms, "number", "miss carries elapsed ms");
    eq(lines.filter(l => l.startsWith("ATTACHRETRY-RECOVERED ")).length, 1, "one RECOVERED line");
    eq(JSON.parse(lines[lines.length - 1].replace("ATTACHRETRY-RECOVERED ", "")).try, 3, "recovered on try 3");
    eq(a.world.closes >= 2, true, "each miss closed the socket");

    lines.length = 0;
    const b = stubPage(1);
    await attachRetry(b, { unit: "stubB", id: "c1", check });
    eq(lines.length, 0, "a first-try success logs nothing");

    lines.length = 0;
    const c = stubPage(99);
    let err = null;
    await attachRetry(c, { unit: "stubC", id: "c1", check }).catch(e => { err = e; });
    eq(c.world.attaches, 5, "attaches on a socket that never opens");
    eq(lines.filter(l => l.startsWith("ATTACHRETRY ")).length, 5, "five ATTACHRETRY lines");
    eq(lines.some(l => l.startsWith("ATTACHRETRY-RECOVERED")), false, "no RECOVERED line when it never opens");
    eq(!!err && /stubC: the card terminal never attached in 5 tries/.test(err.message), true, "the error names the unit and tries");
    eq(!!err && [1, 2, 3, 4, 5].every(n => err.message.includes('"try":' + n)), true, "the error carries all five states");
  } finally { console.log = log; }
  if (failed) { console.error(failed + " failed"); process.exit(1); }
  console.log("attach-retry: ok");
})();
