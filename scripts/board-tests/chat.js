// chat: the chat tab and the chatter tab.
//
// Units: chatter, chatterTab, chatTab, chatBridge, chatPaste, chatKeep, chatCap, chatFast, chatReview.
// `units` is what the harness runs, in suite order. `sections` is for HEADLESS_ONLY, which names a section and not a unit.
// Alone: `node scripts/board-tests/chat.js` runs just these units, the way the full run would.
const path = require("path");
const fs = require("fs");
const { slow, until, frames, settleStyle } = require("../lib/browser.js");
const { mock, landCard } = require("../lib/mock.js");
const { fail } = require("../lib/results.js");
const { landContext } = require("../lib/helpers.js");

// The details popover's chatter: the says to and from a card, newest first, 20 to a page. A row shows the other party,
// when and the delivery state, links to the other party's card when the board has it, and opens to the full text. A
// "more" pages back by the last id read. The endpoint is answered here, so the test needs no live says.
// CHATTER_SHOTS names a directory for a picture.
async function chatterSection(browser, base) {
  const ctx = await browser.newContext({ viewport: { width: 1400, height: 1000 } });
  const dp = await ctx.newPage();
  const errors = [];
  dp.on("pageerror", e => errors.push(String(e)));
  const asked = [];
  const now = Date.now();
  const all = [];
  for (let i = 0; i < 25; i++) {
    const out = i % 3 !== 0;
    all.push({ id: "say" + String(100 - i), from_task: out ? "me-1" : (i === 3 ? "gone-9" : "other-2"), from: out ? "me" : "other",
      to_task: out ? (i === 1 ? "gone-9" : "other-2") : "me-1", to: out ? (i === 1 ? "ghost" : "other") : "me", to_input: "other",
      sent_at: new Date(now - i * 7 * 60000).toISOString(), state: i === 2 ? "queued" : "delivered", channel: i === 2 ? "" : "terminal",
      preview: "say number " + i + " about the work", chars: i === 4 ? 20000 : 40,
      text: i === 4 ? "x".repeat(16000) : "say number " + i + " about the work\nsecond line of " + i });
  }
  await ctx.route("**/v1/tasks/*/says*", route => {
    const u = new URL(route.request().url());
    asked.push(u.search);
    const before = u.searchParams.get("before");
    const at = before ? all.findIndex(s => s.id === before) + 1 : 0;
    const page = all.slice(at, at + 20);
    route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ says: page, more: at + 20 < all.length }) });
  });
  let releaseUsage;
  const usageGate = new Promise(r => { releaseUsage = r; });
  await ctx.route("**/v1/tasks/*/usage*", async route => { await usageGate; await route.fallback(); });
  // Every refresh replaces lastTasks, so the two cards come in the list the board reads, not as a patch to it.
  await ctx.route("**/v1/tasks", async route => {
    const res = await route.fetch();
    const body = await res.json();
    body.tasks = (body.tasks || []).concat([{ id: "me-1", title: "me", wire_name: "me", status: "running", resume_id: "r-me-1" },
      { id: "other-2", title: "other", wire_name: "other", status: "running" }]);
    await route.fulfill({ response: res, json: body });
  });
  await dp.addInitScript(() => {
    localStorage.setItem("atrium.skipconfirm", JSON.stringify({ "width-floor": true }));
  });
  try {
    await dp.goto(base, { waitUntil: "domcontentloaded" });
    await dp.waitForSelector('.tab[data-view="terms"]', { timeout: slow(15000) });
    await dp.click('.tab[data-view="terms"]');
    await dp.waitForSelector("#t-expando", { state: "attached", timeout: slow(15000) });
    await dp.waitForFunction(() => lastTasks.some(t => t.id === "other-2"), null, { timeout: slow(15000) });
    await dp.evaluate(() => {
      for (let n = document.getElementById("t-expando"); n; n = n.parentElement) n.hidden = false;
      document.getElementById("t-drawer").classList.add("open");
      termDrawerOpen = true;
      peekFill(document.getElementById("t-drawer-body"), "me-1", () => true);
    });
    // The chatter is a drawer, folded until it is opened (the same one the peek has).
    await dp.waitForSelector('#t-drawer-body .peek-drawer[data-key="chatter"]:not([hidden]) > .peek-dh', { timeout: slow(8000) });
    await dp.click('#t-drawer-body .peek-drawer[data-key="chatter"] > .peek-dh');
    releaseUsage();
    await dp.waitForSelector("#t-drawer-body:not(.loading)", { timeout: slow(8000) });
    await dp.waitForSelector("#t-drawer-body .chat-row", { timeout: slow(8000) });
    await dp.evaluate(() => termDrawerFit());
    let rows = await dp.$$eval("#t-drawer-body .chat-row", r => r.length);
    if (rows !== 20) fail("the first page of chatter has " + rows + " rows, not 20.");
    const first = await dp.$eval("#t-drawer-body .chat-row", r => ({ dir: r.querySelector(".chat-dir").textContent,
      who: r.querySelector(".chat-who").textContent, meta: r.querySelector(".chat-meta").textContent, link: !!r.querySelector("button.chat-who") }));
    if (first.dir !== "from" || first.who !== "other" || !first.link || !/ago, delivered by terminal/.test(first.meta)) {
      fail("the newest chatter row reads wrong: " + JSON.stringify(first));
    }
    // A card the board does not know is a name and no link.
    const nth = (i, f) => dp.evaluate(([i, f]) => (new Function("r", "return " + f))(document.querySelectorAll("#t-drawer-body .chat-row")[i]), [i, f]);
    const unknown = await nth(1, "r.querySelector('.chat-who').tagName + ' ' + r.querySelector('.chat-who').textContent");
    if (unknown !== "SPAN ghost") fail("an unknown card's name is drawn as " + unknown + ", not plain text.");
    if (!(await nth(2, "/queued/.test(r.querySelector('.chat-meta').textContent)"))) fail("a queued say does not say queued.");
    // The text is hidden until the row is opened.
    if (!(await dp.$eval("#t-drawer-body .chat-text", e => e.hidden))) fail("a row's full text is showing before it is opened.");
    await dp.click("#t-drawer-body .chat-row .chat-toggle");
    const shown = await dp.$eval("#t-drawer-body .chat-text", e => !e.hidden && e.textContent);
    if (!shown || !/second line of 0/.test(shown)) fail("opening a row did not show its full text: " + shown);
    // The cut text says how much was kept.
    const cut = await nth(4, "r.querySelector('.chat-text').textContent");
    if (!/\[cut: the say was 20000 characters\]/.test(cut)) fail("a cut say does not say it was cut: " + cut.slice(-60));
    // The count is in runes: astral characters are two UTF-16 units each and are not a cut.
    const runes = await dp.evaluate(() => {
      const note = s => peekChatterRow("me-1", Object.assign({ from_task: "me-1", sent_at: new Date().toISOString(), state: "queued" }, s))
        .querySelector(".chat-text").textContent.includes("[cut:");
      return { whole: note({ text: "😀😀😀", chars: 3 }), cut: note({ text: "😀😀", chars: 5 }), preview: note({ preview: "hi", chars: 9 }) };
    });
    if (runes.whole || !runes.cut || runes.preview) fail("the cut note counts the wrong thing: " + JSON.stringify(runes));
    if (process.env.CHATTER_SHOTS) {
      await dp.screenshot({ path: require("path").join(process.env.CHATTER_SHOTS, "say-log-view-after.png") });
      // What it looked like before: the same details with no chatter section.
      await dp.evaluate(() => {
        window.__chat = peekChatter; peekChatter = () => {};
        peekFill(document.getElementById("t-drawer-body"), "me-1", () => true);
      });
      await dp.waitForFunction(() => !document.querySelector("#t-drawer-body .chat-row"), null, { timeout: slow(8000) });
      await settleStyle(dp);
      await dp.screenshot({ path: require("path").join(process.env.CHATTER_SHOTS, "say-log-view-before.png") });
      await dp.evaluate(() => {
        peekChatter = window.__chat;
        peekFill(document.getElementById("t-drawer-body"), "me-1", () => true);
      });
      await dp.waitForSelector("#t-drawer-body .chat-row", { timeout: slow(8000) });
    }
    // More pages back from the last id read, and goes once there is no more.
    await dp.click("#t-drawer-body .chat-more");
    await dp.waitForFunction(() => document.querySelectorAll("#t-drawer-body .chat-row").length === 25, null, { timeout: slow(8000) });
    if (!asked.some(q => q === "?before=say81")) fail("more did not ask for the page before the last row: " + JSON.stringify(asked));
    if (await dp.$("#t-drawer-body .chat-more")) fail("more is still offered after the last page.");
    // A card with no says draws no section.
    await ctx.unroute("**/v1/tasks/*/says*");
    await ctx.route("**/v1/tasks/*/says*", route => route.fulfill({ status: 200, contentType: "application/json", body: '{"says":[],"more":false}' }));
    await dp.evaluate(() => peekFill(document.getElementById("t-drawer-body"), "me-1", () => true));
    await dp.waitForFunction(() => { const e = document.querySelector("#t-drawer-body .peek-sec.chatter"); return e && e.hidden; }, null, { timeout: slow(8000) })
      .catch(() => {});
    if (!(await dp.$eval("#t-drawer-body .peek-sec.chatter", e => e.hidden))) fail("an empty chatter section is showing.");
    if (errors.length) fail("the chatter page threw uncaught errors: " + errors.join(" | "));
  } finally {
    await ctx.close();
  }
}

// The chatter tab: every say from every room, newest first, 20 to a page, read on open and on `more` and never polled.
// A row shows when, room, from, to, state and a preview that opens to the full text, and both names link to their cards
// when the board has them. The filters go to GET /v1/says as agent (or from or to), room and since. The endpoint is
// answered here, merged the way the hub does it. CHATTER_SHOTS names a directory for the pictures.
async function chatterTabSection(browser, base) {
  const ctx = await browser.newContext({ viewport: { width: 1100, height: 900 } });
  const dp = await ctx.newPage();
  const errors = [];
  dp.on("pageerror", e => errors.push(String(e)));
  const asked = [];
  const now = Date.now();
  const all = [];
  for (let i = 0; i < 25; i++) {
    const room = i % 2 ? "beta" : "alpha";
    all.push({ id: room + "~say" + String(100 - i), room, from_task: room + "~me-1", from: i % 4 === 1 ? "other" : "me",
      to_task: i === 2 ? room + "~gone-9" : room + "~other-2", to: i === 2 ? "ghost" : i % 4 === 1 ? "me" : "other", to_input: "other",
      sent_at: new Date(now - i * 11 * 60000).toISOString(), state: i === 3 ? "queued" : "delivered", channel: i === 3 ? "" : "terminal",
      preview: "say number " + i + " about the work", chars: i === 4 ? 20000 : 40,
      text: i === 4 ? "x".repeat(16000) : "say number " + i + " about the work\nsecond line of " + i });
  }
  await ctx.route("**/v1/says*", route => {
    const u = new URL(route.request().url());
    asked.push(u.search);
    const before = u.searchParams.get("before");
    const at = before ? Number(before) : 0;
    const page = all.slice(at, at + 20);
    route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ says: page, more: at + 20 < all.length,
      cursor: at + 20 < all.length ? String(at + 20) : "", missing: before ? [] : ["gamma"] }) });
  });
  await dp.addInitScript(() => {
    localStorage.setItem("atrium.skipconfirm", JSON.stringify({ "width-floor": true }));
  });
  const shot = async name => {
    if (process.env.CHATTER_SHOTS) await dp.screenshot({ path: require("path").join(process.env.CHATTER_SHOTS, name) });
  };
  try {
    await dp.goto(base, { waitUntil: "domcontentloaded" });
    await dp.waitForSelector('.tab[data-view="chatter"]', { state: "attached", timeout: slow(15000) });
    // The tab starts in the ... menu whatever the width, and the arrange dialog can put it in the bar and back.
    const tab = await dp.$eval('.tab[data-view="chatter"]', t => ({ text: t.textContent, menu: t.classList.contains("umenu") }));
    if (tab.text !== "messages between agents" || !tab.menu) fail("the chatter tab is not in the menu by default: " + JSON.stringify(tab));
    await dp.evaluate(() => window.openTabsArrange());
    await dp.waitForSelector("#tabsdlg[open]");
    const sel = '#tabsdlg-list .tabsdlg-row[data-view="chatter"] select';
    if ((await dp.$eval(sel, s => s.value)) !== "menu") fail("the arrange dialog does not show chatter in the menu");
    await dp.selectOption(sel, "bar");
    if (await dp.$eval('.tab[data-view="chatter"]', t => t.classList.contains("umenu"))) fail("chatter stays in the menu after being put in the bar");
    await dp.reload({ waitUntil: "domcontentloaded" });
    await dp.waitForSelector("#stack-list .stackrow", { timeout: slow(15000) });
    await dp.waitForSelector('.tab[data-view="chatter"]:not(.umenu)', { state: "attached", timeout: slow(8000) });
    if (await dp.$eval('.tab[data-view="chatter"]', t => t.classList.contains("umenu"))) fail("chatter is back in the menu after a reload");
    await dp.evaluate(() => window.openTabsArrange());
    await dp.waitForSelector("#tabsdlg[open]");
    await dp.click("#tabsdlg-reset");
    await dp.click("#tabsdlg-close");
    if (!(await dp.$eval('.tab[data-view="chatter"]', t => t.classList.contains("umenu")))) fail("reset did not put chatter back in the menu");
    if (process.env.CHATTER_SHOTS) {
      // Before: the same menu with no chatter in it.
      await dp.evaluate(() => { document.querySelector('.tab[data-view="chatter"]').hidden = true; });
      await dp.click("#tabmore");
      await dp.waitForSelector('#tabmore[aria-expanded="true"]', { timeout: slow(8000) });
      await shot("say-log-all-before.png");
      await dp.keyboard.press("Escape");
      await dp.waitForSelector('#tabmore[aria-expanded="false"]', { timeout: slow(8000) });
      await dp.evaluate(() => { document.querySelector('.tab[data-view="chatter"]').hidden = false; });
      await dp.click("#tabmore");
      await dp.waitForSelector('#tabmore[aria-expanded="true"]', { timeout: slow(8000) });
      await shot("say-log-all-menu.png");
      await dp.keyboard.press("Escape");
    }
    if (asked.length) fail("the board asked for the chatter before the tab was opened: " + JSON.stringify(asked));
    await dp.evaluate(() => {
      lastTasks = lastTasks.concat([{ id: "alpha~me-1", title: "me", wire_name: "me", status: "running" },
        { id: "beta~other-2", title: "other", wire_name: "other", status: "running" }]);
      switchView("chatter");
    });
    await dp.waitForSelector("#chatter-list .chatter-row", { timeout: slow(8000) });
    if (asked.length !== 1 || asked[0] !== "") fail("opening the tab made " + asked.length + " reads: " + JSON.stringify(asked));
    const rows = await dp.$$eval("#chatter-list .chatter-row", r => r.length);
    if (rows !== 20) fail("the first page of chatter has " + rows + " rows, not 20.");
    const first = await dp.$eval("#chatter-list .chatter-row", r => ({ room: r.querySelector(".aud-room").textContent,
      who: [...r.querySelectorAll(".chat-who")].map(e => e.tagName + " " + e.textContent), state: r.querySelector(".chatter-state").textContent }));
    if (first.room !== "alpha" || first.who.join("|") !== "BUTTON me|BUTTON other" || first.state !== "delivered by terminal") {
      fail("the newest row reads wrong: " + JSON.stringify(first));
    }
    const ghost = await dp.$eval("#chatter-list .chatter-row:nth-child(4)", r => r.querySelectorAll(".chat-who")[1].tagName + " " + r.querySelectorAll(".chat-who")[1].textContent);
    if (ghost !== "SPAN ghost") fail("a card the board does not know is drawn as " + ghost + ", not plain text.");
    if (!/gamma/.test(await dp.$eval("#chatter-list .chatter-missing", e => e.textContent))) fail("a room that did not answer is not named.");
    if (!(await dp.$eval("#chatter-list .chat-text", e => e.hidden))) fail("a say's full text is showing before it is opened.");
    await dp.click("#chatter-list .chatter-row .chat-toggle");
    const shown = await dp.$eval("#chatter-list .chat-text", e => !e.hidden && e.textContent);
    if (!shown || !/second line of 0/.test(shown)) fail("opening a row did not show its full text: " + shown);
    const cut = await dp.$eval("#chatter-list .chatter-row:nth-child(6) .chat-text", e => e.textContent);
    if (!/\[cut: the say was 20000 characters\]/.test(cut)) fail("a cut say does not say it was cut: " + cut.slice(-60));
    await shot("say-log-all-after.png");
    // More pages back by the cursor the hub gave, and goes with the last page.
    await dp.click("#chatter-list .chat-more");
    await dp.waitForFunction(() => document.querySelectorAll("#chatter-list .chatter-row").length === 25, null, { timeout: slow(8000) });
    if (asked[1] !== "?before=20") fail("more did not ask with the hub's cursor: " + JSON.stringify(asked));
    if (await dp.$("#chatter-list .chat-more")) fail("more is still offered after the last page.");
    // A filter is a new read from the top: the agent, which side of it, the room and how far back.
    await dp.fill("#chatter-agent", "me");
    await dp.selectOption("#chatter-dir", "from");
    await dp.waitForFunction(() => document.querySelectorAll("#chatter-list .chatter-row").length === 20, null, { timeout: slow(8000) });
    const q = new URLSearchParams(asked[asked.length - 1]);
    if (q.get("from") !== "me" || q.has("agent") || q.has("before")) fail("sent by an agent asked " + asked[asked.length - 1]);
    const saysRead = () => dp.waitForResponse(res => /\/v1\/says/.test(res.url()), { timeout: slow(8000) });
    let read = saysRead();
    await dp.selectOption("#chatter-dir", "");
    await read;
    if (new URLSearchParams(asked[asked.length - 1]).get("agent") !== "me") fail("either side asked " + asked[asked.length - 1]);
    read = saysRead();
    await dp.selectOption("#chatter-since", "86400");
    await read;
    const since = new URLSearchParams(asked[asked.length - 1]).get("since");
    if (!since || Math.abs(Date.parse(since) - (Date.now() - 86400000)) > 60000) fail("the time window asked since " + since);
    // The room filter lists the rooms the hub reports.
    await dp.evaluate(() => { hubRooms = [{ name: "alpha" }, { name: "beta" }]; chatterDrawChoices(); });
    read = saysRead();
    await dp.selectOption("#chatter-room", "beta");
    await read;
    if (new URLSearchParams(asked[asked.length - 1]).get("room") !== "beta") fail("the room filter asked " + asked[asked.length - 1]);
    const before = asked.length;
    await dp.waitForTimeout(1500); // wait: nothing must poll
    if (asked.length !== before) fail("the chatter polled: " + (asked.length - before) + " reads with nothing touched.");
    if (errors.length) fail("the chatter tab threw uncaught errors: " + errors.join(" | "));
  } finally {
    await ctx.close();
  }
}

// ── the chat tab (f-chat-tab) ──────────────────────────────────────────────
// A top-nav tab whose left side is the terminals' session list and whose right side is the phone's conversation view,
// embedded. The tab opens, a row selects a card (and does not attach it), replies are bubbles on the left and the
// operator's own on the right inside a capped column, the working line spins while the card works, a send goes to the
// card, and the terminal button and the terminals tab's chat button switch back and forth. `CHAT_SHOTS=<dir>` writes the
// before (terminals) and after (chat) pictures at 1400x900.
async function chatTabSection(browser, base) {
  const was = mock.tasksMode;
  mock.tasksMode = "land";
  landCard("chat-a", { supervised: true, status: "running", display_title: "chat a", activity: { what: "thinking", seconds: 2 }, created_at: "2026-09-19T12:00:00Z" });
  landCard("chat-b", { supervised: true, status: "running", display_title: "chat b", created_at: "2026-09-19T12:05:00Z" });
  mock.landList = [mock.LAND["chat-a"], mock.LAND["chat-b"]];
  mock.landPerms = [];
  const errors = [];
  const ctx = await landContext(browser);
  await ctx.addInitScript(() => {
    const Real = window.WebSocket;
    window.WebSocket = function (url, protocols) {
      if (!/\/attach(\?|$)/.test(url)) return new Real(url, protocols);
      const s = { url, readyState: 0, binaryType: "arraybuffer", bufferedAmount: 0, onopen: null, onclose: null, onmessage: null, onerror: null,
        send() {}, close() { this.readyState = 3; } };
      setTimeout(() => { s.readyState = 1; if (s.onopen) s.onopen({}); }, 0);
      return s;
    };
    Object.assign(window.WebSocket, { CONNECTING: 0, OPEN: 1, CLOSING: 2, CLOSED: 3 });
  });
  const sent = [];
  await ctx.route("**/v1/tasks/*/replies*", route => route.fulfill({ contentType: "application/json", body: JSON.stringify({
    source: "transcript", more: false, replies: [
      { at: "2026-10-15T10:00:00Z", text: "Here is **the plan**:\n\n- one\n- two", truncated: false },
      { at: "2026-10-15T10:05:00Z", text: "And a second reply.", truncated: false }] }) }));
  await ctx.route("**/v1/tasks/*/message", route => {
    sent.push({ url: route.request().url(), body: route.request().postData() || "" });
    return route.fulfill({ contentType: "application/json", body: JSON.stringify({ delivered: "terminal" }) });
  });
  const tag = "chatTab: ";
  try {
    const p = await ctx.newPage();
    p.on("pageerror", e => errors.push(String(e)));
    await p.goto(base, { waitUntil: "domcontentloaded" });
    await p.waitForSelector("#stack-list .stackrow", { timeout: slow(15000) });
    const shot = async name => {
      if (!process.env.CHAT_SHOTS) return;
      fs.mkdirSync(process.env.CHAT_SHOTS, { recursive: true });
      await p.screenshot({ path: path.join(process.env.CHAT_SHOTS, name) });
    };
    // before: the terminals tab as it is
    await p.click('.tab[data-view="terms"]');
    await p.waitForSelector("#term-list .card.tab[data-id]", { timeout: slow(10000) });
    await settleStyle(p);
    await shot("chat-before-terminals-1400x900.png");
    // the tab opens, with the terminals' own list in it
    await p.click('.tab[data-view="chat"]');
    await p.waitForSelector("#chat:not([hidden]) #chat-layout > #term-list .card.tab[data-id]", { timeout: slow(10000) });
    if (await p.evaluate(() => !document.getElementById("terms").hidden)) fail(tag + "the terminals view is still showing");
    // a row selects, it does not attach
    await p.click('#chat-layout #term-list .card.tab[data-id="chat-a"]');
    await p.waitForSelector('#chat-frame-host iframe[data-card="chat-a"]', { timeout: slow(5000) });
    if (await p.evaluate(() => !!(typeof termTask !== "undefined" && termTask))) fail(tag + "selecting a row attached a terminal");
    const isOn = () => p.evaluate(() => document.querySelector('#term-list .card.tab[data-id="chat-a"]').classList.contains("on"));
    const marked = await until(async () => { if (!await isOn()) return false; await frames(p, 2); return isOn(); }, 5000, "the selected row marked").then(() => true, () => false);
    if (!marked) fail(tag + "the selected row is not marked");
    const fr = await (await p.$('#chat-frame-host iframe[data-card="chat-a"]')).contentFrame();
    if (!fr) { fail(tag + "no phone page frame"); return; }
    await fr.waitForSelector("#m-replies .reply", { timeout: slow(10000) });
    await fr.waitForSelector("#m-compose textarea", { timeout: slow(10000) });
    // the phone's own navigation is gone, the column is capped and centred
    const lay = await fr.evaluate(() => {
      const r = el => el.getBoundingClientRect();
      const first = document.querySelector("#m-replies .reply");
      return { barHidden: getComputedStyle(document.querySelector(".bar")).display === "none", homeHidden: getComputedStyle(document.getElementById("m-home")).display === "none",
        topHidden: getComputedStyle(document.querySelector(".sheet-top")).display === "none", w: window.innerWidth, left: r(first).left, right: r(first).right,
        compose: { left: r(document.getElementById("m-compose")).left, right: r(document.getElementById("m-compose")).right } };
    });
    if (!lay.barHidden || !lay.homeHidden || !lay.topHidden) fail(tag + "the phone's navigation shows: " + JSON.stringify(lay));
    if (lay.right - lay.left > 700) fail(tag + "the column is not capped: " + JSON.stringify(lay));
    if (Math.abs(lay.compose.left - (lay.w - lay.compose.right)) > 2) fail(tag + "the composer is not centred: " + JSON.stringify(lay));
    // the working line spins while the card works
    await fr.waitForFunction(() => { const w = document.getElementById("m-working"); return w && !w.hidden && /thinking/.test(w.textContent); }, null, { timeout: slow(8000) });
    // send goes to the card, and the operator's words come back as a right bubble
    await fr.fill("#m-compose textarea", "hello from the chat tab");
    await fr.evaluate(() => document.querySelector("#m-compose textarea").dispatchEvent(new Event("input", { bubbles: true })));
    await fr.click("#m-compose .mc-send");
    await fr.waitForSelector("#m-replies .reply.mine", { timeout: slow(5000) });
    if (!sent.length || !/chat-a\/message$/.test(sent[sent.length - 1].url) || !/hello from the chat tab/.test(sent[sent.length - 1].body)) fail(tag + "the send did not reach the card: " + JSON.stringify(sent));
    const sides = await fr.evaluate(() => {
      const r = el => el.getBoundingClientRect();
      const theirs = document.querySelector("#m-replies .reply:not(.mine)"), mine = document.querySelector("#m-replies .reply.mine");
      return { theirsLeft: r(theirs).left, theirsRight: r(theirs).right, mineLeft: r(mine).left, mineRight: r(mine).right, w: window.innerWidth,
        md: !!theirs.querySelector(".md strong") };
    });
    if (!sides.md) fail(tag + "a reply is not rendered as markdown");
    if (!(sides.mineRight > sides.theirsRight - 1 && sides.mineLeft > sides.theirsLeft)) fail(tag + "own bubble is not to the right of the reply: " + JSON.stringify(sides));
    await settleStyle(p);
    await shot("chat-after-chat-1400x900.png");
    // wide: the column follows the pane, the list is as wide as the terminals' list
    const paneFits = async w => {
      await until(async () => await p.evaluate(x => innerWidth === x, w) && Math.abs(await p.evaluate(() => document.getElementById("chat-frame-host").getBoundingClientRect().width) - await fr.evaluate(() => innerWidth)) <= 2, 5000, "the chat pane at " + w);
      await settleStyle(p);
    };
    await p.setViewportSize({ width: 2400, height: 1200 });
    await paneFits(2400);
    await shot("chat-" + (process.env.CHAT_SHOT_TAG || "after") + "-chat-2400x1200.png");
    const widths = async () => ({
      pane: await p.evaluate(() => document.getElementById("chat-frame-host").getBoundingClientRect().width),
      list: await p.evaluate(() => document.getElementById("term-list").getBoundingClientRect().width),
      col: await fr.evaluate(() => { const r = el => el.getBoundingClientRect(); const c = document.getElementById("m-compose"); return { w: r(c).width, vw: window.innerWidth }; }),
    });
    const wide = await widths();
    if (wide.col.w < wide.pane * 0.85) fail(tag + "at 2400 the column does not fill the pane: " + JSON.stringify(wide));
    await p.setViewportSize({ width: 1400, height: 900 });
    await paneFits(1400);
    const narrow = await widths();
    if (narrow.col.w < narrow.pane * 0.85 || narrow.col.w > narrow.pane) fail(tag + "at 1400 the column does not track the pane: " + JSON.stringify(narrow));
    // the list is the terminals' width, dragged by the terminals' own grip, and it moves both tabs
    if (await p.evaluate(() => !document.getElementById("chat-layout").contains(document.getElementById("term-grip")))) fail(tag + "the divider is not in the chat view");
    const g = await p.locator("#term-grip").boundingBox();
    const before = narrow.list;
    await p.mouse.move(g.x + g.width / 2, g.y + g.height / 2);
    await p.mouse.down();
    await p.mouse.move(g.x + g.width / 2 + 80, g.y + g.height / 2, { steps: 4 });
    await p.mouse.up();
    const after = (await widths()).list;
    if (Math.abs(after - before - 80) > 2) fail(tag + "dragging the divider moved the list from " + before + " to " + after);
    await p.click("#chat-to-term");
    await p.waitForFunction(() => !document.getElementById("terms").hidden, null, { timeout: slow(10000) });
    const onTerms = await p.evaluate(() => document.getElementById("term-list").getBoundingClientRect().width);
    if (Math.abs(onTerms - after) > 2) fail(tag + "the terminals tab shows the list at " + onTerms + ", the chat tab at " + after);
    await p.click("#t-chat");
    await p.waitForSelector('#chat:not([hidden]) #chat-frame-host iframe[data-card="chat-a"]', { timeout: slow(5000) });
    // a card in the chat list is as wide as on the terminals tab
    const cardW = await p.evaluate(() => document.querySelector('#term-list .card.tab[data-id="chat-a"]').getBoundingClientRect().width);
    if (Math.abs(cardW - onTerms) > 20) fail(tag + "a card is " + cardW + " wide in a list " + onTerms + " wide");
    // terminal switch, and back
    await p.click("#chat-to-term");
    await p.waitForFunction(() => !document.getElementById("terms").hidden && termTask && termTask.id === "chat-a", null, { timeout: slow(10000) });
    if (await p.evaluate(() => !document.getElementById("term-layout").contains(document.getElementById("term-list")))) fail(tag + "the list was not given back to the terminals view");
    if (await p.evaluate(() => !!document.querySelector("#chat-frame-host iframe:not([hidden])"))) fail(tag + "a conversation frame shows outside the chat view");
    await p.click("#t-chat");
    await p.waitForSelector('#chat:not([hidden]) #chat-frame-host iframe[data-card="chat-a"]', { timeout: slow(5000) });
    // another row switches the conversation
    await p.click('#chat-layout #term-list .card.tab[data-id="chat-b"]');
    await p.waitForSelector('#chat-frame-host iframe[data-card="chat-b"]', { timeout: slow(5000) });
    if (await p.evaluate(() => document.querySelectorAll("#chat-frame-host iframe:not([hidden])").length) !== 1) fail(tag + "more than one conversation frame showing");
  } finally {
    await ctx.close();
    mock.tasksMode = was;
  }
  if (errors.length) fail(tag + "the page threw: " + errors.join(" | "));
}

async function chatOpen(browser, base, uploads, ids = ["chat-a", "chat-b"]) {
  ids.forEach((id, i) => landCard(id, { supervised: true, status: "running", display_title: id.replace("-", " "), created_at: new Date(Date.UTC(2026, 8, 19, 12, i)).toISOString() }));
  mock.landList = ids.map(id => mock.LAND[id]);
  mock.landPerms = [];
  const errors = [];
  const sent = [];
  const ctx = await landContext(browser);
  await ctx.addInitScript(() => {
    const Real = window.WebSocket;
    window.WebSocket = function (url, protocols) {
      if (!/\/attach(\?|$)/.test(url)) return new Real(url, protocols);
      const s = { url, readyState: 0, binaryType: "arraybuffer", bufferedAmount: 0, onopen: null, onclose: null, onmessage: null, onerror: null,
        send() {}, close() { this.readyState = 3; } };
      setTimeout(() => { s.readyState = 1; if (s.onopen) s.onopen({}); }, 0);
      return s;
    };
    Object.assign(window.WebSocket, { CONNECTING: 0, OPEN: 1, CLOSING: 2, CLOSED: 3 });
  });
  await ctx.route("**/v1/tasks/*/replies*", route => route.fulfill({ contentType: "application/json", body: JSON.stringify({ source: "transcript", more: false, replies: [] }) }));
  await ctx.route("**/v1/tasks/*/message", route => {
    sent.push(JSON.parse(route.request().postData() || "{}"));
    return route.fulfill({ contentType: "application/json", body: JSON.stringify({ delivered: "terminal" }) });
  });
  await ctx.route("**/v1/tasks/*/files", route => {
    uploads.n = (uploads.n || 0) + 1;
    return route.fulfill({ contentType: "application/json", body: JSON.stringify({ paths: [".atrium/incoming/shot.png"] }) });
  });
  const p = await ctx.newPage();
  p.on("pageerror", e => errors.push(String(e)));
  await p.goto(base, { waitUntil: "domcontentloaded" });
  await p.waitForSelector("#stack-list .stackrow", { timeout: slow(15000) });
  return { ctx, p, errors, sent };
}

async function chatBridgeSection(browser, base) {
  const was = mock.tasksMode;
  mock.tasksMode = "land";
  const tag = "chatBridge: ";
  const { ctx, p, errors } = await chatOpen(browser, base, {});
  try {
    const geometry = async view => p.evaluate(v => {
      const lay = document.getElementById(v === "chat" ? "chat-layout" : "term-layout");
      const pane = document.getElementById(v === "chat" ? "chat-pane" : "term-pane");
      const row = document.querySelector('#term-list .card.tab.on[data-id]');
      const b = [...document.querySelectorAll(".tabbridge")].find(x => !x.hidden && lay.contains(x));
      if (!row || !b) return { row: !!row, bridge: !!b, bridged: lay.classList.contains("bridged") };
      const r = row.getBoundingClientRect(), br = b.getBoundingClientRect(), pr = pane.getBoundingClientRect();
      return {
        bridged: lay.classList.contains("bridged"), rowRight: getComputedStyle(row).borderRightColor,
        rowRadius: getComputedStyle(row).borderTopRightRadius, fromRow: Math.abs(br.left - (r.right - 2)),
        toPane: Math.abs(br.right - (pr.left + (parseFloat(getComputedStyle(pane).borderLeftWidth) || 1))),
        level: Math.abs((br.top + br.bottom) / 2 - (r.top + r.bottom) / 2),
      };
    }, view);
    await p.click('.tab[data-view="terms"]');
    await p.waitForSelector("#term-list .card.tab[data-id]", { timeout: slow(10000) });
    await p.click('#term-list .card.tab[data-id="chat-a"]');
    await p.waitForFunction(() => termTask && termTask.id === "chat-a", null, { timeout: slow(10000) });
    await until(async () => (await geometry("terms")).bridged && (await geometry("terms")).fromRow !== undefined, 5000, "the terminals bridge");
    const want = await geometry("terms");
    await p.click('.tab[data-view="chat"]');
    await p.waitForSelector("#chat:not([hidden]) #chat-layout > #term-list .card.tab[data-id]", { timeout: slow(10000) });
    await p.click('#chat-layout #term-list .card.tab[data-id="chat-a"]');
    await p.waitForSelector('#chat-frame-host iframe[data-card="chat-a"]', { timeout: slow(5000) });
    await until(async () => (await geometry("chat")).fromRow !== undefined, 5000, "the chat bridge").catch(() => {});
    await settleStyle(p);
    const got = await geometry("chat");
    if (process.env.CHAT_SHOTS) {
      fs.mkdirSync(process.env.CHAT_SHOTS, { recursive: true });
      await p.screenshot({ path: path.join(process.env.CHAT_SHOTS, "chat-bridge-" + (process.env.CHAT_SHOT_TAG || "after") + ".png") });
    }
    if (!got.bridged || got.fromRow === undefined) { fail(tag + "the selected row has no bridge: " + JSON.stringify(got)); return; }
    if (got.rowRight !== want.rowRight || got.rowRadius !== want.rowRadius) fail(tag + "the row is not drawn as the terminals' attached row: " + JSON.stringify({ got, want }));
    if (got.fromRow > 1.5 || got.toPane > 1.5 || got.level > 1.5) fail(tag + "the bridge does not join the row to the pane: " + JSON.stringify(got));
    await p.click("#chat-to-term");
    await p.waitForFunction(() => !document.getElementById("terms").hidden && termTask && termTask.id === "chat-a", null, { timeout: slow(10000) });
    const back = await until(async () => (await geometry("terms")).fromRow !== undefined, 5000, "the terminals bridge again").then(() => true, () => false);
    if (!back) fail(tag + "the terminals bridge did not come back: " + JSON.stringify(await geometry("terms")));
  } finally {
    await ctx.close();
    mock.tasksMode = was;
  }
  if (errors.length) fail(tag + "the page threw: " + errors.join(" | "));
}

async function chatPasteSection(browser, base) {
  const was = mock.tasksMode;
  mock.tasksMode = "land";
  const tag = "chatPaste: ";
  const uploads = {};
  const { ctx, p, errors, sent } = await chatOpen(browser, base, uploads);
  try {
    await p.click('.tab[data-view="chat"]');
    await p.waitForSelector("#chat:not([hidden]) #chat-layout > #term-list .card.tab[data-id]", { timeout: slow(10000) });
    await p.click('#chat-layout #term-list .card.tab[data-id="chat-a"]');
    await p.waitForSelector('#chat-frame-host iframe[data-card="chat-a"]', { timeout: slow(5000) });
    const fr = await (await p.$('#chat-frame-host iframe[data-card="chat-a"]')).contentFrame();
    await fr.waitForSelector("#m-compose textarea", { timeout: slow(10000) });
    await fr.evaluate(() => {
      const t = document.querySelector("#m-compose .mc-box");
      const b = Uint8Array.from(atob("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="), c => c.charCodeAt(0));
      const dt = new DataTransfer();
      dt.items.add(new File([b], "pasted.png", { type: "image/png" }));
      t.dispatchEvent(new ClipboardEvent("paste", { clipboardData: dt, bubbles: true, cancelable: true }));
    });
    await fr.waitForSelector('#m-compose .mc-file[data-state="ok"] img.mc-thumb', { timeout: slow(5000) });
    const box = await fr.$eval("#m-compose .mc-box", t => t.value);
    if (/incoming|\.png/.test(box)) fail(tag + "the input shows the path: " + JSON.stringify(box));
    if (await fr.$eval("#m-compose .mc-send", b => b.disabled)) fail(tag + "send is disabled with an attachment");
    if (process.env.CHAT_SHOTS) {
      fs.mkdirSync(process.env.CHAT_SHOTS, { recursive: true });
      await p.screenshot({ path: path.join(process.env.CHAT_SHOTS, "chat-paste-" + (process.env.CHAT_SHOT_TAG || "after") + ".png") });
    }
    await fr.click("#m-compose .mc-send");
    await until(async () => sent.length > 0, 5000, "the send");
    if (!/\.atrium\/incoming\/shot\.png/.test(sent[0].text || "")) fail(tag + "the path did not reach the agent: " + JSON.stringify(sent));
    if (await fr.$$eval("#m-compose .mc-file", x => x.length)) fail(tag + "the chip outlived the send");

    await fr.evaluate(() => {
      const b = Uint8Array.from(atob("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="), c => c.charCodeAt(0));
      const dt = new DataTransfer();
      dt.items.add(new File([b], "again.png", { type: "image/png" }));
      document.querySelector("#m-compose .mc-box").dispatchEvent(new ClipboardEvent("paste", { clipboardData: dt, bubbles: true, cancelable: true }));
    });
    await fr.waitForSelector('#m-compose .mc-file[data-state="ok"]', { timeout: slow(5000) });
    await fr.click("#m-compose .mc-file .mc-fx");
    if (await fr.$$eval("#m-compose .mc-file", x => x.length)) fail(tag + "the chip did not go");
    if (!await fr.$eval("#m-compose .mc-send", b => b.disabled)) fail(tag + "send stayed enabled after the attachment was removed");
  } finally {
    await ctx.close();
    mock.tasksMode = was;
  }
  if (errors.length) fail(tag + "the page threw: " + errors.join(" | "));
}

const chatRow = id => '#chat-layout #term-list .card.tab[data-id="' + id + '"]';
const chatFrameSel = id => '#chat-frame-host iframe[data-card="' + id + '"]';

async function chatPick(p, id) {
  await p.click(chatRow(id));
  await p.waitForSelector(chatFrameSel(id) + ":not([hidden])", { timeout: slow(5000) });
}

async function chatKeepSection(browser, base) {
  const was = mock.tasksMode;
  mock.tasksMode = "land";
  const tag = "chatKeep: ";
  const { ctx, p, errors } = await chatOpen(browser, base, {});
  const loads = {};
  p.on("request", r => {
    const m = /\/m\/\?chat=1$/.exec(r.url());
    if (m && r.resourceType() === "document" && r.frame() !== p.mainFrame()) loads.n = (loads.n || 0) + 1;
  });
  try {
    await p.click('.tab[data-view="chat"]');
    await p.waitForSelector("#chat:not([hidden]) #chat-layout > #term-list .card.tab[data-id]", { timeout: slow(10000) });
    await chatPick(p, "chat-a");
    await p.evaluate(() => { window.__chatA = document.querySelector('#chat-frame-host iframe[data-card="chat-a"]'); });
    const same = id => p.evaluate(i => window.__chatA === document.querySelector('#chat-frame-host iframe[data-card="' + i + '"]'), id);
    await p.click('.tab[data-view="terms"]');
    await p.waitForFunction(() => !document.getElementById("terms").hidden, null, { timeout: slow(5000) });
    await p.click('.tab[data-view="chat"]');
    await p.waitForSelector(chatFrameSel("chat-a") + ":not([hidden])", { timeout: slow(5000) });
    if (!await same("chat-a")) fail(tag + "Chat, Terminals, Chat built a new frame for chat-a");
    await chatPick(p, "chat-b");
    if (!await p.evaluate(() => document.querySelector('#chat-frame-host iframe[data-card="chat-a"]').hidden)) fail(tag + "chat-a's frame shows beside chat-b's");
    await chatPick(p, "chat-a");
    if (!await same("chat-a")) fail(tag + "chat-a, chat-b, chat-a built a new frame for chat-a");
    const shown = await p.evaluate(() => [...document.querySelectorAll("#chat-frame-host iframe")].filter(f => !f.hidden).map(f => f.dataset.card));
    if (shown.join() !== "chat-a") fail(tag + "the shown frames are " + JSON.stringify(shown));
    await until(() => loads.n > 2, 500, "a third load").then(() => {}, () => {});
    if (loads.n !== 2) fail(tag + "the phone page was loaded " + loads.n + " times for two cards, not 2");
    const title = await p.evaluate(() => document.getElementById("chat-title").textContent);
    if (title !== "chat a") fail(tag + "the chat title is " + JSON.stringify(title) + " for chat-a");
    const on = await p.evaluate(() => document.querySelector('#term-list .card.tab.on[data-id]').dataset.id);
    if (on !== "chat-a") fail(tag + "the marked row is " + on);
  } finally {
    await ctx.close();
    mock.tasksMode = was;
  }
  if (errors.length) fail(tag + "the page threw: " + errors.join(" | "));
}

async function chatCapSection(browser, base) {
  const was = mock.tasksMode;
  mock.tasksMode = "land";
  const tag = "chatCap: ";
  const ids = ["chat-a", "chat-b", "chat-c", "chat-d", "chat-e"];
  const { ctx, p, errors } = await chatOpen(browser, base, {}, ids);
  const held = () => p.evaluate(() => [...document.querySelectorAll("#chat-frame-host iframe")].map(f => f.dataset.card).sort().join());
  try {
    await p.click('.tab[data-view="chat"]');
    await p.waitForSelector("#chat:not([hidden]) #chat-layout > #term-list .card.tab[data-id]", { timeout: slow(10000) });
    for (const id of ["chat-a", "chat-b", "chat-c", "chat-d"]) await chatPick(p, id);
    if (await held() !== "chat-a,chat-b,chat-c,chat-d") fail(tag + "four cards hold " + await held());
    await chatPick(p, "chat-a");
    await chatPick(p, "chat-e");
    if (await held() !== "chat-a,chat-c,chat-d,chat-e") fail(tag + "the fifth card did not drop the least recently used, held: " + await held());
  } finally {
    await ctx.close();
    mock.tasksMode = was;
  }
  if (errors.length) fail(tag + "the page threw: " + errors.join(" | "));
}

async function chatFastSection(browser, base) {
  const was = mock.tasksMode;
  mock.tasksMode = "land";
  const tag = "chatFast: ";
  const { ctx, p, errors } = await chatOpen(browser, base, {});
  const hold = { ms: 0 };
  await ctx.route("**/v1/tasks", async route => {
    if (hold.ms && route.request().frame().parentFrame()) await new Promise(r => setTimeout(r, hold.ms)); // wait: the held answer is the latency under test
    await route.fallback();
  });
  await ctx.route("**/v1/tasks/chat-a", route => route.fulfill({ contentType: "application/json", body: JSON.stringify(mock.LAND["chat-a"]) }));
  await ctx.route("**/v1/tasks/*/replies*", route => route.fulfill({ contentType: "application/json", body: JSON.stringify({
    source: "transcript", more: false, replies: [{ at: "2026-10-15T10:00:00Z", text: "first reply", truncated: false }] }) }));
  try {
    await p.click('.tab[data-view="chat"]');
    await p.waitForSelector("#chat:not([hidden]) #chat-layout > #term-list .card.tab[data-id]", { timeout: slow(10000) });
    hold.ms = slow(8000);
    await p.click(chatRow("chat-a"));
    await p.waitForSelector(chatFrameSel("chat-a"), { timeout: slow(5000) });
    const fr = await (await p.$(chatFrameSel("chat-a"))).contentFrame();
    await fr.waitForSelector("#m-replies .reply", { timeout: slow(4000) }).catch(() => fail(tag + "the replies were not drawn while the full list was still pending"));
  } finally {
    await ctx.close();
    mock.tasksMode = was;
  }
  if (errors.length) fail(tag + "the page threw: " + errors.join(" | "));
}

// CHAT_MEASURE=<cards> adds a unit that times click to replies drawn on a mocked board of that many cards, and prints
// one JSON line per kind of switch. CHAT_MEASURE_LIST_MS holds back the phone page's list read by that long, which is
// what 6 MB costs on a real link. It asserts nothing.
async function chatMeasureSection(browser, base) {
  const was = mock.tasksMode;
  mock.tasksMode = "land";
  const n = Number(process.env.CHAT_MEASURE) || 800;
  const ids = Array.from({ length: n }, (_, i) => "land-" + i);
  const { ctx, p } = await chatOpen(browser, base, {}, ids);
  await ctx.route("**/v1/tasks/*/replies*", route => route.fulfill({ contentType: "application/json", body: JSON.stringify({
    source: "transcript", more: false, replies: [{ at: "2026-10-15T10:00:00Z", text: "a reply", truncated: false }] }) }));
  const lagMs = Number(process.env.CHAT_MEASURE_LIST_MS) || 0;
  if (lagMs) {
    await ctx.route("**/v1/tasks", async route => {
      if (route.request().frame().parentFrame()) await new Promise(r => setTimeout(r, lagMs)); // wait: the injected list latency being measured
      await route.fallback();
    });
  }
  const drawn = id => p.waitForFunction(i => {
    const f = document.querySelector("#chat-frame-host iframe:not([hidden])");
    const d = f && f.dataset.card === i && f.contentDocument;
    const r = d && d.querySelector("#m-replies .reply");
    return !!(r && r.offsetParent);
  }, id, { timeout: 30000, polling: "raf" });
  const time = async fn => { const t = Date.now(); await fn(); return Date.now() - t; };
  const row = id => p.evaluate(i => { document.querySelector('#chat-layout #term-list .card.tab[data-id="' + i + '"]').click(); }, id);
  const out = { cards: n, first: [], backToCard: [], backToTab: [] };
  try {
    await p.click('.tab[data-view="chat"]');
    await p.waitForSelector("#chat:not([hidden]) #chat-layout > #term-list .card.tab[data-id]", { timeout: slow(10000) });
    for (let i = 0; i < 3; i++) out.first.push(await time(async () => { await row(ids[i]); await drawn(ids[i]); }));
    for (let i = 0; i < 2; i++) {
      out.backToCard.push(await time(async () => { await row(ids[i]); await drawn(ids[i]); }));
      await row(ids[2]);
      await drawn(ids[2]);
    }
    for (let i = 0; i < 3; i++) {
      await p.click('.tab[data-view="terms"]');
      await p.waitForFunction(() => !document.getElementById("terms").hidden);
      out.backToTab.push(await time(async () => { await p.click('.tab[data-view="chat"]'); await drawn(ids[2]); }));
    }
    console.log("CHAT-MEASURE " + JSON.stringify(out));
  } finally {
    await ctx.close();
    mock.tasksMode = was;
  }
}

async function chatReviewSection(browser, base) {
  const was = mock.tasksMode;
  mock.tasksMode = "land";
  const tag = "chatReview: ";
  landCard("rv-a", { supervised: true, status: "running", display_title: "rv a", tags: ["atrium:review"], created_at: "2026-09-19T12:00:00Z" });
  landCard("rv-b", { supervised: true, status: "running", display_title: "rv b", created_at: "2026-09-19T12:05:00Z" });
  mock.landList = [mock.LAND["rv-a"], mock.LAND["rv-b"]];
  mock.landPerms = [];
  const errors = [];
  const asked = [];
  const ctx = await landContext(browser);
  await ctx.route("**/v1/tasks/*/replies*", route => route.fulfill({ contentType: "application/json", body: JSON.stringify({ source: "transcript", more: false, replies: [] }) }));
  const finding = (num, group, state, cat) => ({ num, cat, kind: "code", origin: "mine", site: "lib/c.c:" + num, title: "finding " + num, note: "note " + num,
    footer: "CHANGE " + cat + " : one\nSUMMARY     : two", diff: "@@ -1,2 +1,2 @@\n ctx\n-old " + num + "\n+new " + num + "\n", base_diff: "", lines_vs_base: num, group, state, reason: "" });
  await ctx.route("**/v1/tasks/*/atrium-review", route => {
    const id = /tasks\/([^/]+)\//.exec(route.request().url())[1];
    asked.push(id);
    if (id !== "rv-a") return route.fulfill({ status: 404, contentType: "application/json", body: '{"error":"no review"}' });
    return route.fulfill({ contentType: "application/json", body: JSON.stringify({ base: "main", merge_base: "abc", files: [{ path: "lib/c.c", added: 1, removed: 1 }],
      findings: [finding(1, "batch", "applied", "DRY"), finding(2, "", "waiting", "SCOPE")] }) });
  });
  try {
    const p = await ctx.newPage();
    p.on("pageerror", e => errors.push(String(e)));
    await p.goto(base, { waitUntil: "domcontentloaded" });
    await p.waitForSelector("#stack-list .stackrow", { timeout: slow(15000) });
    await p.click('.tab[data-view="chat"]');
    await p.waitForSelector("#chat:not([hidden]) #chat-layout > #term-list .card.tab[data-id]", { timeout: slow(10000) });
    const shot = async name => {
      if (!process.env.REVIEW_SHOTS) return;
      fs.mkdirSync(process.env.REVIEW_SHOTS, { recursive: true });
      await settleStyle(p);
      await p.screenshot({ path: path.join(process.env.REVIEW_SHOTS, name) });
    };
    await chatPick(p, "rv-b");
    await p.waitForTimeout(400); // wait: lets the picked card paint before the screenshot only
    await shot("before-chat.png");
    if (!await p.$eval("#chat-review", b => b.hidden)) fail(tag + "the review button shows for a card with no review tag");
    await chatPick(p, "rv-a");
    await p.waitForFunction(() => !document.getElementById("chat-review").hidden, null, { timeout: slow(8000) });
    await shot("after-chat-with-review-button.png");
    await p.click("#chat-review");
    await p.waitForSelector("#review-rail .rv-f", { timeout: slow(5000) });
    const heads = await p.$$eval("#review-rail .rv-h", h => h.map(x => x.textContent));
    if (heads.join("|") !== "Applied for veto (1)|One at a time (1)") fail(tag + "the rail groups read " + JSON.stringify(heads));
    const rows = await p.$$eval("#review-rail .rv-f", r => r.map(x => x.querySelector(".rv-cat").textContent + "/" + x.querySelector(".rv-st").textContent));
    if (rows.join("|") !== "DRY/applied|SCOPE/waiting") fail(tag + "the rail rows read " + JSON.stringify(rows));
    if (await p.$$eval("#review-main button, #chat-review-pane button", b => b.length)) fail(tag + "the review view has buttons");
    await p.click('#review-rail .rv-f[data-num="2"]');
    const diff = await p.$eval(".rv-diff", d => ({ a: d.querySelectorAll(".a").length, d: d.querySelectorAll(".d").length, h: d.querySelectorAll(".h").length }));
    if (diff.a !== 1 || diff.d !== 1 || diff.h !== 1) fail(tag + "the diff is drawn as " + JSON.stringify(diff));
    await shot("after-review.png");
    const foot = await p.$eval(".rv-foot", f => f.textContent);
    if (foot !== "CHANGE SCOPE : one\nSUMMARY     : two") fail(tag + "the footer is not verbatim: " + JSON.stringify(foot));
    if (!/note 2/.test(await p.$eval(".rv-note", n => n.textContent))) fail(tag + "the note line is missing");
    if (await p.$eval("#chat-frame-host", h => !h.hidden)) fail(tag + "the conversation shows under the review");
    await p.click("#chat-review");
    if (await p.$eval("#chat-review-pane", h => !h.hidden)) fail(tag + "the review pane stays open after the button is pressed again");
    await shot("after-chat-review-closed.png");
    const n = asked.length;
    await until(async () => asked.length > n, 6000, "a poll");
  } finally {
    await ctx.close();
    mock.tasksMode = was;
  }
  if (errors.length) fail(tag + "the page threw: " + errors.join(" | "));
}

const units = [
  { name: "chatReview", run: ({ browser, base }) => chatReviewSection(browser, base) },
  ...(process.env.CHAT_MEASURE ? [{ name: "chatMeasure", run: ({ browser, base }) => chatMeasureSection(browser, base) }] : []),
  { name: "chatKeep", run: ({ browser, base }) => chatKeepSection(browser, base) },
  { name: "chatCap", run: ({ browser, base }) => chatCapSection(browser, base) },
  { name: "chatFast", run: ({ browser, base }) => chatFastSection(browser, base) },
  { name: "chatBridge", run: ({ browser, base }) => chatBridgeSection(browser, base) },
  { name: "chatPaste", run: ({ browser, base }) => chatPasteSection(browser, base) },
  { name: "chatter", run: ({ browser, base }) => chatterSection(browser, base) },
  { name: "chatterTab", run: ({ browser, base }) => chatterTabSection(browser, base) },
  { name: "chatTab", run: ({ browser, base }) => chatTabSection(browser, base) },
];
const sections = { chatReview: chatReviewSection, ...(process.env.CHAT_MEASURE ? { chatMeasure: chatMeasureSection } : {}), chatKeep: chatKeepSection, chatCap: chatCapSection, chatFast: chatFastSection, chatBridge: chatBridgeSection, chatPaste: chatPasteSection, chatter: chatterSection, chatterTab: chatterTabSection, chatTab: chatTabSection };

module.exports = { units, sections };
if (require.main === module) require("../lib/alone.js")(units);
