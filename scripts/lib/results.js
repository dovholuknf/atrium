// What a headless run has found so far: how many things failed, which unit is running, and what each unit said. Every
// bucket file under scripts/board-tests/ and the harness itself report through fail(), so the count lives here and not in
// the harness.
//
// `bad` is also a global, read-only, because a section ends with `if (!bad) console.log("name ok")` and a `let` in one
// module cannot be seen from another. A section that declares its own `bad` shadows it, as before.

const results = {
  bad: 0,
  // The unit running now, and what failed in it. See unit() in the harness.
  currentUnit: "",
  unitFails: {},
};

function fail(msg) {
  console.error("FAIL: " + msg);
  results.bad++;
  if (results.currentUnit) (results.unitFails[results.currentUnit] = results.unitFails[results.currentUnit] || []).push(String(msg));
}

Object.defineProperty(globalThis, "bad", { get: () => results.bad, configurable: true });

module.exports = { results, fail };
