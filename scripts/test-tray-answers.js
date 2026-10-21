// Answering a card's open questions from a question row in the tray (the bell), in a real browser against a mock daemon.
//
// The row carries the peek's NEEDS YOU control, so the answer path is the peek's: a draft saved on the card, then one message.
// A question answered elsewhere reads as answered. Skips when Playwright or its browser is not installed.
//   TRAY_SHOTS=dir   writes the after PNGs there, and the before ones when TRAY_BEFORE=1 takes the control away
const http = require("http");
const fs = require("fs");
const path = require("path");
const { wholeBoard } = require("./board-source.js");

let chromium;
try { ({ chromium } = require("@playwright/test")); } catch (e) {
  try { ({ chromium } = require("playwright")); } catch (e2) {
    console.log("playwright is not installed, so the tray answers check is skipped.");
    process.exit(0);
  }
}
let exe = "";
try { exe = chromium.executablePath(); } catch (e) {}
if (!exe || !fs.existsSync(exe)) { console.log("chromium is not installed, so the tray answers check is skipped."); process.exit(0); }

let bad = 0;
const fail = m => { console.error("FAIL: " + m); bad++; };

const HTML = wholeBoard();
const AT = "2026-10-21T09:04:00.000Z";
const mk = (id, title, qs) => ({
  id, title, status: "needs-input", runner: "claude", supervised: true, pinned: true,
  created_at: "2026-10-21T09:00:00Z", last_activity_at: AT,
  seen: { unseen: false, answered: false, questions_at: AT, open_questions: qs },
});
const cards = [
  mk("t~one", "orchestrator", ["Will you land claude/main now?"]),
  mk("t~two", "discourse-4784", ["Install jq with winget now?", "Tear down the d4784 lab on cdaws?"]),
];
const byId = id => cards.find(c => c.id === id);
const patches = [], messages = [];

const sendJSON = (res, v) => { res.writeHead(200, { "Content-Type": "application/json" }); res.end(JSON.stringify(v)); };
const server = http.createServer((req, res) => {
  const url = req.url.split("?")[0];
  if (url === "/" || url === "/index.html") { res.writeHead(200, { "Content-Type": "text/html" }); res.end(HTML); return; }
  if (url.startsWith("/vendor/") || url.startsWith("/m/")) {
    return fs.readFile(path.join(__dirname, "..", "internal", "api", "web", url), (e, b) => {
      if (e) { res.writeHead(404); res.end(""); return; }
      res.writeHead(200, { "Content-Type": url.endsWith(".css") ? "text/css" : "application/javascript" }); res.end(b);
    });
  }
  let raw = "";
  req.on("data", c => { raw += c; });
  req.on("end", () => {
    let body = {};
    try { body = JSON.parse(raw || "{}"); } catch (e) {}
    if (url === "/v1/tasks") return sendJSON(res, { tasks: cards });
    const m = /^\/v1\/tasks\/([^/]+)(\/message)?$/.exec(url);
    if (m && !m[2] && req.method === "PATCH") {
      patches.push(body);
      const c = byId(decodeURIComponent(m[1]));
      if (c && "answer_drafts" in body) { if (body.answer_drafts) c.answer_drafts = body.answer_drafts; else delete c.answer_drafts; }
      return sendJSON(res, c || {});
    }
    if (m && m[2] && req.method === "POST") {
      const id = decodeURIComponent(m[1]);
      messages.push({ id, ...body });
      const c = byId(id);
      if (c) c.seen = { unseen: false, answered: true, questions_at: AT };
      return sendJSON(res, { queued: true });
    }
    if (url.startsWith("/v1/events")) { return; }
    sendJSON(res, {});
  });
});

(async () => {
  await new Promise(r => server.listen(0, "127.0.0.1", r));
  const base = "http://127.0.0.1:" + server.address().port + "/";
  const browser = await chromium.launch({ executablePath: exe });
  const ctx = await browser.newContext({ viewport: { width: 1300, height: 800 } });
  const p = await ctx.newPage();
  p.on("pageerror", e => fail("page error: " + e));
  const shots = process.env.TRAY_SHOTS || "";
  const tag = (process.env.TRAY_BEFORE === "1" ? "before" : "after") + (process.env.TRAY_SKIN ? "-" + process.env.TRAY_SKIN : "");
  const shoot = async name => { if (shots) { await p.waitForTimeout(350); await p.screenshot({ path: `${shots}/tray-${name}-${tag}.png` }); } };
  const refresh = async () => { await p.evaluate(() => runRefresh()); await p.waitForTimeout(250); };
  const row = id => `.tlrow[data-task="${id}"]`;

  try {
    await p.goto(base, { waitUntil: "domcontentloaded" });
    await p.waitForSelector('.card[data-id="t~one"], .stackrow[data-id="t~one"]', { state: "attached", timeout: 15000 });
    if (process.env.TRAY_SKIN) await p.evaluate(k => document.documentElement.setAttribute("data-skin", k), process.env.TRAY_SKIN);
    await refresh();
    await p.evaluate(() => {
      recordToLog("discourse-4784 asked 2 questions", "Install jq with winget now?", "", null, "t~two", "g2");
      recordToLog("orchestrator asked a question", "Will you land claude/main now?", "", null, "t~one", "g1");
    });
    if (process.env.TRAY_BEFORE === "1") await p.evaluate(() => { peekQuestionsHtml = undefined; });
    await p.evaluate(() => openToastLog());
    await p.waitForSelector(".tlrow");
    await shoot("rows");
    if (process.env.TRAY_BEFORE === "1") { await browser.close(); server.close(); process.exit(bad ? 1 : 0); }

    const once = async (id, q) => (await p.innerText(row(id))).split(q).length - 1;
    if (await once("t~one", "Will you land claude/main now?") !== 1) fail("the one-question row should print its question once");
    if (await once("t~two", "Install jq with winget now?") !== 1) fail("the two-question row should print its first question once");
    if (await p.locator(row("t~one") + " .peek-qa .qa-q").count() !== 1) fail("the one-question row should list its question");
    if (await p.locator(row("t~two") + " .peek-qa .qa-q").count() !== 2) fail("the two-question row should list both");

    await p.click(row("t~two") + ' .qa-q[data-qi="0"]');
    await p.waitForSelector(".qa-fly.on textarea");
    if (!await p.evaluate(() => document.getElementById("toastlog").contains(document.querySelector(".qa-fly")))) fail("the flyout must sit inside the modal tray");
    if (!await p.locator("#toastlog[open]").count()) fail("opening a question must not close the tray");
    await p.fill(".qa-fly textarea", "yes");
    await p.click('.qa-fly [data-step="1"]');
    await p.fill(".qa-fly textarea", "no, keep it");
    const geo = await p.evaluate(() => {
      const r = el => el.getBoundingClientRect();
      const tray = r(document.getElementById("toastlog")), fly = r(document.querySelector(".qa-fly"));
      const q = r(document.querySelector('.tlrow[data-task="t~two"] .qa-q[data-qi="' + qaOn.qi + '"]'));
      return { tray: [tray.left, tray.top, tray.bottom], fly: [fly.left, fly.right, fly.top, fly.bottom], q: q.top };
    });
    if (Math.abs(geo.fly[1] - geo.tray[0]) > 1) fail("the drawer is not flush with the tray's left edge: " + JSON.stringify(geo));
    if (geo.fly[2] < geo.tray[1] - 1 || geo.fly[3] > geo.tray[2] + 1) fail("the drawer is outside the tray's height band: " + JSON.stringify(geo));
    if (Math.abs(geo.fly[2] - geo.q) > 1 && geo.fly[3] < geo.tray[2] - 1) fail("the drawer's top is not on the clicked question: " + JSON.stringify(geo));
    await shoot("flyout");
    if (!await p.locator(".qa-fly .qa-b.send:visible").count()) fail("every question answered, the flyout should offer to send");
    await p.click(".qa-fly .qa-b.send");
    await p.waitForTimeout(600);
    if (messages.length !== 1 || messages[0].id !== "t~two") fail("one message to t~two should have gone: " + JSON.stringify(messages));
    else if (messages[0].text !== "Q1 (Install jq with winget): yes\nQ2 (Tear down the d4784): no, keep it") fail("the message was " + JSON.stringify(messages[0].text));
    await refresh();
    if (!/answered/.test(await p.innerText(row("t~two")))) fail("a sent row should read answered");
    if (await p.locator(row("t~two") + " .qa-q").count()) fail("an answered row stops asking");
    await shoot("answered");

    await p.evaluate(() => document.getElementById("toastlog").close());
    if (await p.evaluate(() => !!document.querySelector(".qa-fly.on"))) fail("the drawer stayed open after the tray closed");
    await p.evaluate(() => openToastLog());
    byId("t~one").seen = { unseen: false, answered: true, questions_at: AT };
    await refresh();
    if (!/answered/.test(await p.innerText(row("t~one")))) fail("a question answered elsewhere should read answered in an open tray");
    if (await p.locator(row("t~one") + " .qa-q").count()) fail("a question answered elsewhere stops asking");
    if (messages.length !== 1) fail("nothing else should have been sent");
  } catch (e) {
    fail(String(e && e.stack || e));
  }
  await browser.close();
  server.close();
  if (bad) { console.error(bad + " failure(s)"); process.exit(1); }
  console.log("tray answers: ok");
})();
