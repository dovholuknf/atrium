// The shared page setup (lib/browser.js): a click on a selector that never appears fails at the 5 s default, prints one
// TIMEOUT-DIAG line, and still throws the original error. A present-but-covered button checks the geometry fields.
const { withClock, withTrace, chromium, DEFAULT_WAIT } = require("./lib/browser.js");

let failed = 0;
const eq = (a, b, what) => { if (a !== b) { failed++; console.error("FAIL " + what + ": got " + JSON.stringify(a) + ", want " + JSON.stringify(b)); } };

(async () => {
  const browser = withTrace(withClock(await chromium.launch()), () => "selfTest");
  const lines = [];
  const log = console.log;
  console.log = s => lines.push(String(s));
  try {
    const page = await browser.newPage();
    await page.setContent('<button id="b" class="x y" style="position:absolute;left:10px;top:10px;width:80px;height:30px">go</button>' +
      '<div id="cover" class="veil" style="position:absolute;left:0;top:0;width:200px;height:200px"></div>');

    const t0 = Date.now();
    let err = null;
    await page.click("#nope .missing").catch(e => { err = e; });
    const ms = Date.now() - t0;
    eq(!!err && err.name === "TimeoutError", true, "the original TimeoutError is thrown");
    eq(!!err && /Timeout 5000ms exceeded/.test(err.message), true, "the default is 5000ms");
    eq(ms >= 4500 && ms < 9000, true, "fails in about 5 s, took " + ms);
    const diag = lines.filter(l => l.startsWith("TIMEOUT-DIAG "));
    eq(diag.length, 1, "one TIMEOUT-DIAG line");
    const j = JSON.parse(diag[0].slice("TIMEOUT-DIAG ".length));
    eq(j.unit, "selfTest", "unit");
    eq(j.selector, "#nope .missing", "selector");
    eq(j.matches, 0, "matches nothing");
    eq(typeof j.inflight, "number", "inflight count");
    eq(/click/.test(j.wait), true, "wait expression");

    lines.length = 0;
    err = null;
    await page.locator("#b").click({ timeout: 1500 }).catch(e => { err = e; });
    const k = JSON.parse((lines.find(l => l.startsWith("TIMEOUT-DIAG ")) || "x {}").slice(13));
    eq(!!err && err.name === "TimeoutError", true, "a covered click throws the original error");
    eq(k.matches, 1, "covered: matches one");
    eq(k.visible, true, "covered: visible");
    eq(!!k.box && k.box.width === 80, true, "covered: bounding box");
    eq(!!k.atCentre && k.atCentre.id + "/" + k.atCentre.tag + "/" + k.atCentre.classes, "cover/div/veil", "covered: element at the centre");

    eq(DEFAULT_WAIT(), 5000 * (Number(process.env.HEADLESS_SLOW) > 0 ? Number(process.env.HEADLESS_SLOW) : 1), "default scales with slow()");
    await page.close();
  } finally { console.log = log; await browser.close(); }
  if (failed) { console.error(failed + " failed"); process.exit(1); }
  console.log("timeout-diag: ok");
})();
