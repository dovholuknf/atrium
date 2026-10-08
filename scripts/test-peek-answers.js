// Answering a card's open questions from its peek, in a real browser against a mock daemon.
//
// Answer two of three, blur (close the peek), see the mail chip on the card and the drafts on the card itself.
// Come back, send, and check the one message that went. Then a new turn with new questions: the old drafts
// survive and are sent marked as an earlier turn. Skips when Playwright or its browser is not installed.
//   PEEK_SHOTS=dir   writes before/after PNGs there
const http = require("http");
const fs = require("fs");
const path = require("path");
const { wholeBoard } = require("./board-source.js");

let chromium;
try { ({ chromium } = require("@playwright/test")); } catch (e) {
  try { ({ chromium } = require("playwright")); } catch (e2) {
    console.log("playwright is not installed, so the peek answers check is skipped.");
    process.exit(0);
  }
}
let exe = "";
try { exe = chromium.executablePath(); } catch (e) {}
if (!exe || !fs.existsSync(exe)) { console.log("chromium is not installed, so the peek answers check is skipped."); process.exit(0); }

let bad = 0;
const fail = m => { console.error("FAIL: " + m); bad++; };

const HTML = wholeBoard();
const QAT1 = "2026-10-07T10:12:00.000Z";
const QAT2 = "2026-10-07T11:40:00.000Z";
const card = {
  id: "t~q1", title: "ask me things", status: "needs-input", runner: "claude", supervised: true, pinned: true,
  created_at: "2026-10-07T09:00:00Z", last_activity_at: "2026-10-07T10:12:00Z",
  seen: { unseen: false, answered: false, questions_at: QAT1,
    open_questions: ["Where does the autocompact limit come from?", "Keep the old flag?", "Ship it today?"] },
};
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
    if (url === "/v1/tasks") return sendJSON(res, { tasks: [card] });
    if (/^\/v1\/tasks\/[^/]+$/.test(url) && req.method === "PATCH") {
      patches.push(body);
      if ("answer_drafts" in body) { if (body.answer_drafts) card.answer_drafts = body.answer_drafts; else delete card.answer_drafts; }
      return sendJSON(res, card);
    }
    if (url.endsWith("/message") && req.method === "POST") {
      messages.push(body);
      card.seen = { unseen: false, answered: true, questions_at: card.seen.questions_at };
      return sendJSON(res, { queued: true });
    }
    if (url === "/v1/events" || url.startsWith("/v1/events")) { return; } // held open, never answers
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
  const shots = process.env.PEEK_SHOTS || "";
  const shoot = async name => { if (shots) { await p.waitForTimeout(350); await p.screenshot({ path: `${shots}/${name}.png` }); } };
  const refresh = async () => { await p.evaluate(() => runRefresh()); await p.waitForTimeout(250); };
  const open = () => p.evaluate(() => { const c = document.querySelector('.card[data-id="t~q1"], .stackrow[data-id="t~q1"]'); openPeek("t~q1", c, "menu"); });

  try {
    await p.goto(base, { waitUntil: "domcontentloaded" });
    await p.waitForSelector('.card[data-id="t~q1"], .stackrow[data-id="t~q1"]', { state: "attached", timeout: 15000 });
    await refresh();
    await shoot("peek-answers-before");

    await open();
    await p.waitForSelector(".peek-qa .qa-q");
    if (await p.locator(".peek-qa .qa-q").count() !== 3) fail("the peek should list three questions");
    if (await p.locator("[data-qa-send]").count()) fail("nothing is answered, so nothing should offer to send");

    // Answer two of three.
    await p.click('.peek-qa .qa-q[data-qi="0"]');
    await p.waitForSelector(".qa-fly.on textarea");
    await p.fill(".qa-fly textarea", "use the hub value");
    await p.click('.qa-fly [data-step="1"]');
    await p.fill(".qa-fly textarea", "no, drop it");
    await shoot("peek-answers-flyout");
    if (await p.locator(".qa-fly .qa-b.send:visible").count()) fail("one question is unanswered, the flyout must not offer to send");

    // Blur: click away. The peek closes, the drafts do not.
    await p.mouse.click(5, 790);
    await p.waitForTimeout(500);
    if (await p.locator(".peek.on").count()) fail("a click away should close the peek");
    if (!card.answer_drafts) fail("the drafts were not saved on the card");
    else {
      const g = JSON.parse(card.answer_drafts)[0];
      if (g.a[0] !== "use the hub value" || g.a[1] !== "no, drop it" || g.a[2]) fail("the saved drafts are wrong: " + card.answer_drafts);
    }
    await refresh();
    const chip = await p.locator('.chip.mail').count();
    if (!chip) fail("a card holding drafts should wear the mail chip");
    await shoot("peek-answers-chip");

    // Come back: it offers to send what there is, and the flyout still has the text.
    await open();
    await p.waitForSelector(".peek-qa [data-qa-send]");
    const label = (await p.locator(".peek-qa [data-qa-send]").innerText()).replace(/\s+/g, " ");
    if (!/send answers to agent.*2 of 3/.test(label)) fail("coming back should offer 'send answers to agent', got: " + label);
    await p.click('.peek-qa .qa-q[data-qi="2"]');
    await p.fill(".qa-fly textarea", "yes");
    if (!await p.locator(".qa-fly .qa-b.send:visible").count()) fail("every question answered, the flyout should offer to send now");
    await shoot("peek-answers-complete");
    await p.click(".qa-fly .qa-b.send");
    await p.waitForTimeout(500);
    if (messages.length !== 1) fail("one message should have gone, got " + messages.length);
    else {
      const want = "Q1 (Where does the autocompact): use the hub value\nQ2 (Keep the old flag): no, drop it\nQ3 (Ship it today): yes";
      if (messages[0].text !== want) fail("the message was:\n" + messages[0].text + "\nwant:\n" + want);
    }
    if (card.answer_drafts) fail("sent drafts should be cleared from the card");

    // A new turn with new questions while a draft is waiting: it survives and is sent marked as earlier.
    card.answer_drafts = JSON.stringify([{ at: QAT1, qs: card.seen.open_questions || ["Where does the autocompact limit come from?", "Keep the old flag?", "Ship it today?"], a: ["", "keep it", ""] }]);
    card.seen = { unseen: false, answered: false, questions_at: QAT2, open_questions: ["Which branch?"] };
    await refresh();
    await open();
    await p.waitForSelector(".peek-qa .qa-old");
    await p.click('.peek-qa .qa-q[data-gi="1"]');
    await p.fill(".qa-fly textarea", "main");
    await p.click(".qa-fly .qa-b.send");
    await p.waitForTimeout(500);
    const m2 = messages[1] && messages[1].text;
    const clock = new Date(QAT1).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", hour12: false });
    if (m2 !== `re ${clock} Q2 (Keep the old flag): keep it\nQ1 (Which branch): main`) fail("the second message was: " + JSON.stringify(m2));
    await shoot("peek-answers-after");

    // CTRL+ENTER: on the last question with another unanswered it goes to that one and stays open. With everything
    // answered it sends, from any question, and the hint says which it will do.
    card.seen = { unseen: false, answered: false, questions_at: QAT2, open_questions: ["One?", "Two?", "Three?"] };
    delete card.answer_drafts;
    await refresh();
    await open();
    await p.waitForSelector('.peek-qa .qa-q[data-qi="2"]');
    await p.click('.peek-qa .qa-q[data-qi="2"]');
    await p.fill(".qa-fly textarea", "c");
    await p.keyboard.press("Control+Enter");
    await p.waitForTimeout(200);
    if (!await p.locator(".qa-fly.on").count()) fail("ctrl+enter on the last question must not close the flyout");
    if (!/Two/.test(await p.innerText(".qa-fly .qa-fq")) && !/One/.test(await p.innerText(".qa-fly .qa-fq"))) fail("ctrl+enter on the last should jump to an unanswered question");
    if (!/for the next one/.test(await p.innerText(".qa-fly .qa-hint"))) fail("the hint should say next while some are unanswered");
    const before = messages.length;
    await p.fill(".qa-fly textarea", "a");
    await p.keyboard.press("Control+Enter");
    await p.waitForTimeout(200);
    await p.fill(".qa-fly textarea", "b");
    if (!/to send to the agent/.test(await p.innerText(".qa-fly .qa-hint"))) fail("the hint should say send once all are answered");
    await p.click('.qa-fly .qa-dot[data-dot="0"]');
    await p.keyboard.press("Control+Enter");
    await p.waitForTimeout(500);
    if (messages.length !== before + 1) fail("ctrl+enter with everything answered should send, from any question");
    if (await p.locator(".qa-fly.on").count()) fail("the flyout should close after the send");

    // THE TERMINALS TAB, where the work is done: its row wears the chip, and the chip opens the same peek and flyout.
    card.seen = { unseen: false, answered: false, questions_at: QAT2, open_questions: ["Which branch?"] };
    card.answer_drafts = JSON.stringify([{ at: QAT2, qs: ["Which branch?"], a: ["main"] }]);
    await p.click('.tab[data-view="terms"]');
    await refresh();
    const row = '#term-list .card.tab[data-id="t~q1"]';
    await p.waitForSelector(row, { state: "attached", timeout: 10000 });
    if (!await p.locator(row + " .chip.mail").count()) fail("the Terminals tab row should wear the mail chip");
    await shoot("peek-answers-terminals");
    await p.evaluate(sel => document.querySelector(sel + " .chip.mail").click(), row);
    await p.waitForSelector(".peek.on .peek-qa .qa-q");
    await p.click(".peek-qa .qa-q");
    await p.waitForSelector(".qa-fly.on textarea");
    if (await p.inputValue(".qa-fly textarea") !== "main") fail("the flyout opened from the Terminals tab lost the draft");
  } catch (e) {
    fail(String(e && e.stack || e));
  }
  await browser.close();
  server.close();
  if (bad) { console.error(bad + " failure(s)"); process.exit(1); }
  console.log("peek answers: ok");
})();
