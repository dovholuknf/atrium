// Clean copy on a real ctrl-c, in chromium, firefox and webkit, against the real clipboard.
//
// Firefox and Safari write to the clipboard only inside a user gesture. So the board serves a card with a reply in its
// terminal, part of it is selected and ctrl-c is pressed on the keyboard. The clipboard is read back by a real ctrl-v
// into a textarea. Expected: the selection with each line's edge spaces gone, never the render and never nothing.
//
//   node scripts/check-clipboard-browsers.js [chromium firefox webkit]
//
// A browser Playwright has not installed is SKIP, unless CI or FULL_SUITE is set. Exits 1 on any FAIL.
//
// Headless webkit on Windows neither reaches the system clipboard nor reads it, so there it runs only with CLIP_HEADED=1,
// which opens a window and overwrites the clipboard. Playwright's webkit does not refuse a late write as Safari does:
// the headless unit cleanCopyGesture holds that rule.

const { T1, mock, hooks, landCard, server } = require("./lib/mock.js");
const { slow, frames, termWrite } = require("./lib/browser.js");
const { attachRetry, landContext } = require("./lib/helpers.js");
const pw = (() => { try { return require("@playwright/test"); } catch (e) { return require("playwright"); } })();

const MUST_RUN = process.env.FULL_SUITE === "1" || !!process.env.CI;
const shown = "  Here is the draft:\r\n\r\n  ▎ Please review the plan and tell\r\n  ▎ me what is missing from it.\r\n";
const want = "▎ Please review the plan and tell\n▎ me what is missing from it.";

hooks.tasks = () => mock.tasksMode === "land" ? [T1].concat(mock.landList) : [T1];

async function one(name, base) {
  const type = pw[name];
  let exe = "";
  try { exe = type.executablePath(); } catch (e) {}
  if (!exe || !require("fs").existsSync(exe)) return { name, result: MUST_RUN ? "FAIL" : "SKIP", why: "not installed" };
  const headed = name === "webkit" && process.platform === "win32";
  if (headed && process.env.CLIP_HEADED !== "1") return { name, result: "SKIP", why: "headless webkit on Windows has no clipboard, set CLIP_HEADED=1" };
  const browser = await type.launch({ headless: !headed });
  const errors = [];
  try {
    const ctx = await landContext(browser);
    if (name === "chromium") await ctx.grantPermissions(["clipboard-read", "clipboard-write"], { origin: base });
    await ctx.addInitScript(() => {
      const Real = window.WebSocket;
      window.WebSocket = function (url, protocols) {
        if (!/\/attach(\?|$)/.test(url)) return new Real(url, protocols);
        const s = { url, readyState: 0, binaryType: "arraybuffer", bufferedAmount: 0,
          onopen: null, onclose: null, onmessage: null, onerror: null,
          send() {}, close() { this.readyState = 3; } };
        setTimeout(() => { s.readyState = 1; if (s.onopen) s.onopen({}); }, 0);
        return s;
      };
      Object.assign(window.WebSocket, { CONNECTING: 0, OPEN: 1, CLOSING: 2, CLOSED: 3 });
    });
    const p = await ctx.newPage();
    p.on("pageerror", e => errors.push(String(e)));
    await p.goto(base, { waitUntil: "domcontentloaded" });
    await p.waitForSelector("#stack-list .stackrow", { timeout: slow(15000) });
    await attachRetry(p, { unit: "clipboard-" + name, id: "land-live", check: () => termSock && termSock.readyState === 1 && termTask && termTask.id === "land-live" });
    await frames(p, 3);
    await p.evaluate(out => { resetTerm(term); termSock.onmessage({ data: out }); }, shown);
    await termWrite(p, "");
    const sel = await p.evaluate(() => { term.selectLines(2, 3); term.focus(); return termSelectionText(term); });
    if (!/▎/.test(sel)) return { name, result: "FAIL", why: "the selection holds no bar: " + JSON.stringify(sel) };
    await p.keyboard.press("Control+c");
    await new Promise(r => setTimeout(r, 1000)); // wait: no event marks a finished clipboard write
    await p.evaluate(() => {
      const ta = document.createElement("textarea");
      ta.id = "clip-probe";
      ta.style.cssText = "position:fixed;left:0;top:0;width:400px;height:200px;z-index:99999";
      document.body.appendChild(ta);
      ta.focus();
    });
    await p.keyboard.press("Control+v");
    await p.waitForFunction(() => document.getElementById("clip-probe").value !== "", null, { timeout: slow(3000) }).catch(() => {});
    const got = (await p.$eval("#clip-probe", t => t.value)).replace(/\r\n/g, "\n");
    if (errors.length) return { name, result: "FAIL", why: "the page threw: " + errors.join(" | ") };
    if (got === want) return { name, result: "PASS", why: "clean text on the clipboard" };
    const what = got === "" ? "the clipboard is empty" : got === sel.replace(/\r\n/g, "\n") ? "the render, not the clean text" : "something else";
    return { name, result: "FAIL", why: what + ": " + JSON.stringify(got) };
  } finally {
    await browser.close();
  }
}

async function main() {
  const names = process.argv.slice(2).length ? process.argv.slice(2) : ["chromium", "firefox", "webkit"];
  await new Promise(r => server.listen(0, "127.0.0.1", r));
  const base = "http://127.0.0.1:" + server.address().port;
  mock.tasksMode = "land";
  landCard("land-live", { supervised: true, created_at: "2026-09-19T12:00:00Z" });
  mock.landList = [mock.LAND["land-live"]];
  mock.landPerms = [];
  let bad = 0;
  for (const name of names) {
    let r;
    try { r = await one(name, base); } catch (e) { r = { name, result: "FAIL", why: String(e && e.message || e).split("\n")[0] }; }
    if (r.result === "FAIL") bad++;
    console.log(`${r.result} ${r.name}: ${r.why}`);
  }
  server.close();
  process.exit(bad ? 1 : 0);
}

main();
