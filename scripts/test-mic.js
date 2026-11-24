// The mic in every text box: off until switched on under experimental features, then one per box, one listening at a
// time, and switching it off gives every box back. Runs the real prefs-live.js, emerging.js and mic.js in a real browser.
// Skips when Playwright or its browser is not installed. The phone message box's hold-to-talk button is checked first,
// with the real `wireMic`: it says "release to add it" until the switch is on, and then "release to send".
const fs = require("fs");
const path = require("path");

const composeSrc = fs.readFileSync(path.join(__dirname, "..", "internal", "api", "web", "m", "js", "compose.js"), "utf8");
const at = composeSrc.indexOf("function wireMic(");
let depth = 0, end = at, seen = false;
for (; end < composeSrc.length; end++) {
  if (composeSrc[end] === "{") { depth++; seen = true; } else if (composeSrc[end] === "}") { depth--; if (seen && depth === 0) break; }
}
const wireMic = new Function("el", "window", composeSrc.slice(at, end + 1) + "\nreturn wireMic;");
const heldSays = async sends => {
  const on = {}, says = [], added = [];
  const mic = { dataset: {}, addEventListener: (k, f) => { on[k] = f; }, setAttribute() {}, setPointerCapture() {}, after() {},
    getBoundingClientRect: () => ({ left: 0, top: 0, width: 10, height: 10 }) };
  const voice = { capturable: () => false, blocked: () => "", start: () => Promise.resolve(), active: () => true, stop: async () => "hello", cancel() {} };
  wireMic(() => ({}), { atriumVoice: voice })(mic, { id: "c", say: (k, t) => says.push(t), add: t => added.push(t), live: () => true, sends });
  on.pointerdown({ preventDefault() {}, pointerId: 1 });
  await new Promise(r => setTimeout(r, 0)); // wait: the press resolves on a microtask
  await on.pointerup();
  return { says: says.filter(Boolean), added };
};

let chromium;
try { ({ chromium } = require("@playwright/test")); } catch (e) {
  try { ({ chromium } = require("playwright")); } catch (e2) {
    console.log("playwright is not installed, so the mic check is skipped.");
    process.exit(0);
  }
}
let exe = "";
try { exe = chromium.executablePath(); } catch (e) {}
if (!exe || !fs.existsSync(exe)) { console.log("chromium is not installed, so the mic check is skipped."); process.exit(0); }

const WEB = path.join(__dirname, "..", "internal", "api", "web");
const src = f => fs.readFileSync(path.join(WEB, f), "utf8");
let bad = 0;
const is = (got, want, what) => { if (JSON.stringify(got) !== JSON.stringify(want)) { console.error(`FAIL: ${what}: expected ${JSON.stringify(want)}, got ${JSON.stringify(got)}`); bad++; } };

(async () => {
  const off = await heldSays(() => false), on = await heldSays(() => true), none = await heldSays(undefined);
  is(off.says[0], "listening, release to add it, slide off to cancel", "the phone mic says it adds while the switch is off");
  is(on.says[0], "listening, release to send, slide off to cancel", "and says it sends once the switch is on");
  is(none.says[0], "listening, release to add it, slide off to cancel", "a box that cannot send says it adds");
  is([off.added, on.added], [["hello"], ["hello"]], "release hands the words to the box either way");

  const browser = await chromium.launch({ executablePath: exe });
  const page = await browser.newPage();
  await page.route("**/*", r => r.fulfill({ contentType: "text/html", body: "<!doctype html><html><body></body></html>" }));
  await page.goto("http://mic.test/");
  await page.evaluate(() => {
    window.webkitSpeechRecognition = function () { this.start = () => { this.live = true; }; this.stop = () => { this.live = false; }; };
    document.body.innerHTML = '<div id="a"><textarea class="mc-box"></textarea></div><div id="b"><textarea class="gr-reply"></textarea></div>';
  });
  for (const f of ["js/prefs-live.js", "js/emerging.js", "js/mic.js"]) await page.addScriptTag({ content: src(f) });
  const count = () => page.evaluate(() => document.querySelectorAll("button.mic").length);
  const wraps = () => page.evaluate(() => document.querySelectorAll(".mic-wrap").length);

  is(await page.evaluate(() => document.documentElement.getAttribute("data-off").includes("micin")), true, "off by default");
  is(await count(), 0, "no mic is drawn while the switch is off");

  await page.evaluate(() => flagSet("micin", true));
  is(await count(), 2, "one mic in each box once it is switched on");
  is(await page.evaluate(() => document.querySelectorAll("textarea[data-mic]").length), 2, "both boxes are marked");

  await page.evaluate(() => { const [a, b] = document.querySelectorAll("button.mic"); a.click(); b.click(); });
  is(await page.evaluate(() => [...document.querySelectorAll("button.mic")].map(b => b.dataset.state || "")), ["", "listening"], "only the last one tapped listens");
  is(await page.evaluate(() => document.querySelectorAll("textarea.mic-live").length), 1, "only the listening box is outlined");

  await page.evaluate(() => flagSet("micin", false));
  is(await count(), 0, "switching it off takes every mic away");
  is(await wraps(), 0, "and unwraps the boxes");
  is(await page.evaluate(() => document.querySelectorAll("[data-mic]").length), 0, "and unmarks them");
  is(await page.evaluate(() => document.querySelector("#a").firstElementChild.tagName), "TEXTAREA", "the box is back where it was");

  await page.evaluate(() => flagSet("micin", true));
  is(await count(), 2, "switching it on again draws them again");

  await browser.close();
  if (bad) { console.error(`${bad} check(s) failed`); process.exit(1); }
  console.log("mic checks passed");
})().catch(e => { console.error("FAIL: " + e.message); process.exit(1); });
