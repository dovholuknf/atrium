// items: the items view: unified cards, interviews, ready cards.
//
// Units: readyOnce, readyPopout, readyTwoWindows, itemsRealShape, itemsUx, itemsView, itemsMock, itemsEfficacy, itemsPhone, interviewTop, itemMinionChip, itemDocsLanded, itemOpenHiddenCard, kidCounts, unifiedCard, noReadyChildren, readyNoSeed.
// `units` is what the harness runs, in suite order. `sections` is for HEADLESS_ONLY, which names a section and not a unit.
// Alone: `node scripts/board-tests/items.js` runs just these units, the way the full run would.
const fs = require("fs");
const path = require("path");
const { slow, fakeWait, fakeTick, fakePause } = require("../lib/browser.js");
const { mock, WEB_ROOT } = require("../lib/mock.js");
const { fail } = require("../lib/results.js");
const { docImgDir } = require("../lib/helpers.js");

// ── a ready alert rings once per wait, and only after the card went quiet ────
// The alert rang 17 times for one card. `check` diffed on id membership, so a card out of the set for
// one pass came back fresh. Keyed on bare id plus waiting_since now, held for quiet first. The quiet is
// shortened through window.__atriumReadyQuietMs so the test does not sit through five real seconds.
async function readyOnceSection(browser, base) {
  const ctx = await browser.newContext({ viewport: { width: 1400, height: 900 } });
  const page = await ctx.newPage();
  const errors = [];
  page.on("pageerror", e => errors.push(String(e)));
  await page.addInitScript(() => {
    window.__atriumReadyQuietMs = 600;
    Document.prototype.hasFocus = () => true;
  });
  try {
    await page.clock.install();
    await page.goto(base, { waitUntil: "domcontentloaded" });
    await fakeWait(page, () => typeof alerting !== "undefined", null, 15000);
    await fakeTick(page, 1500);
    await fakePause(page);
    await page.evaluate(() => {
      localStorage.removeItem("atrium.toastlog");
      alerting.set({ debounce: 0, muted: true });
      const w = (id, since) => ({ id, task_id: id, status: "needs-input", waiting_since: since });
      const say = t => ({ title: t.id + " ready", body: "done" });
      window.__ro = {
        w,
        check: items => alerting.check("waiting", items, say),
        rings: title => toastLog().filter(e => e.title === title).reduce((n, e) => n + (e.n || 1), 0),
        out: {},
      };
      window.__ro.check([]);
    });
    const step = (fn, arg) => page.evaluate(([src, a]) => { const r = window.__ro; return (0, eval)("(" + src + ")")(r, a); }, [fn.toString(), arg]);
    await step(r => { r.check([r.w("ro-a", "S1")]); r.out.heldAtOnce = r.rings("ro-a ready"); });
    await fakeTick(page, 900);
    await step(r => { r.out.first = r.rings("ro-a ready"); r.check([]); r.check([r.w("ro-a", "S1")]); });
    await fakeTick(page, 900);
    await step(r => { r.out.sameWait = r.rings("ro-a ready"); r.check([]); r.check([r.w("ro-a", "S2")]); });
    await fakeTick(page, 900);
    await step(r => { r.out.newWait = r.rings("ro-a ready"); r.check([]); r.check([r.w("ro-b", "S1")]); });
    await fakeTick(page, 200);
    await step(r => r.check([]));
    await fakeTick(page, 900);
    await step(r => { r.out.ended = r.rings("ro-b ready"); r.check([r.w("ro-c", "S1")]); });
    await fakeTick(page, 350);
    await step(r => alerting.activity("ro-c"));
    await fakeTick(page, 400);
    await step(r => { r.out.busy = r.rings("ro-c ready"); });
    await fakeTick(page, 500);
    await step(r => { r.out.settled = r.rings("ro-c ready"); r.check([]); r.check([r.w("ro-d", "S1"), r.w("ro-e", "S1")]); });
    await fakeTick(page, 900);
    const got = await step(r => { r.out.pile = r.rings("2 agents are ready"); r.out.single = r.rings("ro-d ready") + r.rings("ro-e ready"); return r.out; });
    if (got.heldAtOnce !== 0) fail("readyOnce: a fresh wait rang at once: " + JSON.stringify(got));
    if (got.first !== 1) fail("readyOnce: a quiet wait did not ring exactly once: " + JSON.stringify(got));
    if (got.sameWait !== 1) fail("readyOnce: the same wait rang again after leaving and returning: " + JSON.stringify(got));
    if (got.newWait !== 2) fail("readyOnce: a new waiting_since did not ring: " + JSON.stringify(got));
    if (got.ended !== 0) fail("readyOnce: a wait that ended inside the quiet rang: " + JSON.stringify(got));
    if (got.busy !== 0) fail("readyOnce: a wait with activity inside the quiet rang early: " + JSON.stringify(got));
    if (got.settled !== 1) fail("readyOnce: a wait did not ring after the activity went quiet: " + JSON.stringify(got));
    if (got.pile !== 1 || got.single !== 0) fail("readyOnce: two quiet waits were not one alert: " + JSON.stringify(got));
    if (errors.length) fail("readyOnce: the page threw: " + errors.join(" | "));
  } finally {
    await ctx.close();
  }
}

// ── a window reading a card's terminal says nothing about its ready, and its quiet is pty quiet ──────────
// A focused pop-out (or the board with the terminal attached and showing) gets no toast for its own card's
// ready, only the log line. An unfocused one rings. Output inside the quiet delays it.
async function readyPopoutSection(browser, base) {
  const ctx = await browser.newContext({ viewport: { width: 1400, height: 900 } });
  const page = await ctx.newPage();
  const errors = [];
  page.on("pageerror", e => errors.push(String(e)));
  await page.addInitScript(() => {
    window.__atriumReadyQuietMs = 600;
    window.__focus = true;
    Document.prototype.hasFocus = () => window.__focus;
  });
  try {
    await page.goto(base, { waitUntil: "domcontentloaded" });
    await page.waitForFunction(() => typeof alerting !== "undefined" && typeof soloAlert === "function", null,
      { timeout: slow(15000) });
    await page.waitForTimeout(1500);
    const got = await page.evaluate(async () => {
      localStorage.removeItem("atrium.toastlog");
      alerting.set({ debounce: 0, muted: true });
      const sleep = ms => new Promise(r => setTimeout(r, ms));
      const shown = t => [...document.querySelectorAll("#toasts .toast")].some(e => e.textContent.includes(t));
      const logged = t => toastLog().some(e => e.title === t);
      const out = {};
      // A pop-out: its hash names the card, and it holds that terminal.
      const popout = async (name, focus, activityAt) => {
        window.__focus = focus;
        soloID = name;
        soloTask = { id: name, display_title: name };
        soloKnown = { perm: null, ready: false };
        term = {};
        termTask = { id: name };
        history.replaceState(null, "", "#term=" + name);
        soloAlert("ready", true, { id: name, waiting_since: "W-" + name });
        if (activityAt) { await sleep(activityAt); readyQuietFedAt = 0; feedReadyQuiet(); out[name + "Early"] = shown(name + " is ready"); await sleep(400); out[name + "Mid"] = shown(name + " is ready"); await sleep(500); }
        else await sleep(900);
        out[name] = shown(name + " is ready");
        out[name + "Log"] = logged(name + " is ready");
      };
      await popout("pfocus", true, 0);
      await popout("pblur", false, 0);
      await popout("pbusy", false, 350);
      // The board, with the card's terminal attached and on screen.
      history.replaceState(null, "", "#");
      document.getElementById("terms").hidden = false;
      const w = id => ({ id, task_id: id, status: "needs-input", waiting_since: "S1" });
      const say = t => ({ title: t.id + " ready", body: "done" });
      termTask = { id: "bfocus" };
      window.__focus = true;
      alerting.check("waiting", [], say);
      alerting.check("waiting", [w("bfocus")], say);
      await sleep(900);
      out.board = shown("bfocus ready");
      out.boardLog = logged("bfocus ready");
      termTask = { id: "bother" };
      alerting.check("waiting", [], say);
      alerting.check("waiting", [w("bfocus2")], say);
      await sleep(900);
      out.boardOther = shown("bfocus2 ready");
      return out;
    });
    const j = JSON.stringify(got);
    if (got.pfocus || got.pfocusLog) fail("readyPopout: a focused pop-out toasted its own card or logged it: " + j);
    if (!got.pblur || !got.pblurLog) fail("readyPopout: an unfocused pop-out did not ring for its card: " + j);
    if (got.pbusyEarly || got.pbusyMid) fail("readyPopout: pty output inside the quiet did not delay the ring: " + j);
    if (!got.pbusy) fail("readyPopout: the ring never came after the output went quiet: " + j);
    if (got.board || got.boardLog) fail("readyPopout: the focused board toasted or logged the card it shows: " + j);
    if (!got.boardOther) fail("readyPopout: the board went quiet for a card it is not showing: " + j);
    if (errors.length) fail("readyPopout: the page threw: " + errors.join(" | "));
  } finally {
    await ctx.close();
  }
}

// ── the focused window that shows a card silences its ready alert in EVERY window ──────────────────────
// Two documents of one browser share a BroadcastChannel. The focused one is attached to card X on the
// terminals view, the other is unfocused. X goes ready: neither toasts nor plays, both log. Then focus
// moves away and the next wait rings.
async function readyTwoWindowsSection(browser, base) {
  const ctx = await browser.newContext({ viewport: { width: 1400, height: 900 } });
  const errors = [];
  await ctx.addInitScript(() => {
    window.__atriumReadyQuietMs = 600;
    window.__focus = false;
    Document.prototype.hasFocus = () => window.__focus;
    window.__osc = 0;
    window.AudioContext = class {
      constructor() { this.state = "running"; this.currentTime = 0; this.destination = {}; }
      resume() {}
      createOscillator() {
        window.__osc++;
        return { type: "", frequency: { setValueAtTime() {} }, connect: x => x, start() {}, stop() {} };
      }
      createGain() {
        return { gain: { setValueAtTime() {}, linearRampToValueAtTime() {}, exponentialRampToValueAtTime() {} },
          connect: x => x };
      }
    };
  });
  await ctx.clock.install();
  const open = async () => {
    const p = await ctx.newPage();
    p.on("pageerror", e => errors.push(String(e)));
    await p.goto(base, { waitUntil: "domcontentloaded" });
    await p.waitForFunction(() => typeof alerting !== "undefined" && typeof soloAlert === "function", null,
      { timeout: slow(15000) });
    await fakeTick(p, 1500);
    await p.evaluate(() => {
      alerting.set({ debounce: 0, muted: false });
      document.dispatchEvent(new Event("pointerdown"));
      window.__osc = 0;
    });
    return p;
  };
  const a = await open();
  const b = await open();
  try {
    await fakePause(b);
    await a.evaluate(() => localStorage.removeItem("atrium.toastlog"));
    // A: focused, the board with X attached on the terminals view.
    await a.evaluate(() => {
      window.__focus = true;
      term = {};
      termTask = { id: "tw-x" };
      document.getElementById("terms").hidden = false;
      sayWhetherFocused();
    });
    await fakeWait(b, () => focusedElsewhere.watch === "tw-x", null, 5000, 20);
    const state = p => p.evaluate(t => ({
      toasted: [...document.querySelectorAll("#toasts .toast")].some(e => e.textContent.includes(t)),
      osc: window.__osc
    }), "tw-x is ready");
    const logged = p => p.evaluate(() => toastLog().some(e => e.title === "tw-x is ready"));
    const w = since => ({ id: "tw-x", task_id: "tw-x", status: "needs-input", waiting_since: since });
    const say = t => ({ title: t.id + " is ready", body: "done" });

    // 1. The unfocused board on the kanban hears X go ready.
    await b.evaluate(([w1, sayS]) => {
      alerting.check("waiting", [], t => ({ title: t.id + " is ready", body: "done" }));
      alerting.check("waiting", [{ id: "tw-x", task_id: "tw-x", status: "needs-input", waiting_since: "W1" }],
        t => ({ title: t.id + " is ready", body: "done" }));
    }, [0, 0]);
    await fakeTick(b, 1000);
    const a1 = await state(a), b1 = await state(b);
    if (a1.toasted || a1.osc || b1.toasted || b1.osc) fail("readyTwoWindows: the focused board's card rang in a window: " + JSON.stringify({ a1, b1 }));
    if (await logged(b)) fail("readyTwoWindows: the focused card's ready was logged");

    // 2. A pop-out of X, unfocused, while the board is focused on X.
    await b.evaluate(() => {
      soloID = "tw-x";
      soloTask = { id: "tw-x", display_title: "tw-x" };
      soloKnown = { perm: null, ready: false };
      term = {};
      termTask = { id: "tw-x" };
      history.replaceState(null, "", "#term=tw-x");
      soloAlert("ready", true, { id: "tw-x", waiting_since: "P1" });
    });
    await fakeTick(b, 1000);
    const a2 = await state(a), b2 = await state(b);
    if (a2.toasted || a2.osc || b2.toasted || b2.osc) fail("readyTwoWindows: an unfocused pop-out rang for the card the focused board shows: " + JSON.stringify({ a2, b2 }));

    // 3. Control: focus leaves the board, and the next wait rings.
    await a.evaluate(() => { window.__focus = false; sayWhetherFocused(); });
    await fakeWait(b, () => !focusedElsewhere.win, null, 5000, 20);
    await b.evaluate(() => {
      soloKnown = { perm: null, ready: false };
      soloAlert("ready", true, { id: "tw-x", waiting_since: "P2" });
    });
    await fakeTick(b, 1000);
    const b3 = await state(b);
    if (!b3.toasted && !b3.osc) fail("readyTwoWindows: with focus away the next wait did not ring: " + JSON.stringify(b3));
  } finally {
    await ctx.close();
  }
  if (errors.length) fail("readyTwoWindows: a page threw: " + errors.join(" | "));
}

async function itemMinionChipSection(browser, base) {
  const wasHub = mock.hubMode;
  mock.hubMode = true;
  const ctx = await browser.newContext({ viewport: { width: 1200, height: 900 } });
  const errors = [];
  const imgs = docImgDir("docs", "changes", "img", "u-minion-cards-hidden");
  try {
    fs.mkdirSync(imgs, { recursive: true });
    const p = await ctx.newPage();
    p.on("pageerror", e => errors.push(String(e)));
    await p.route(/\/_hub\/items/, r => r.fulfill({ status: 500, body: "the mock must not reach the hub" }));
    await p.goto(base + "/?itemsmock=1", { waitUntil: "domcontentloaded" });
    await p.waitForFunction(() => { const t = document.querySelector('.tab[data-view="items"]'); return t && !t.hidden; }, null, { timeout: slow(15000) });
    await p.waitForTimeout(1000);
    await p.evaluate(() => switchView("items"));
    await p.waitForFunction(() => document.querySelectorAll("#items-list .it-row").length > 4, null, { timeout: slow(10000) });
    const paint = (minion) => p.evaluate(async minion => {
      const tags = minion ? ["atrium:director", "atrium:minion", "atrium:item:f-ptyhost-reconnect"] : ["atrium:item"];
      const mk = (id, extra) => Object.assign({ id, tags, status: "running", supervised: true, pid: 1, display_title: id, wire_name: id }, extra || {});
      lastTasks = [mk("c-builder-ptyhost"), mk("c-reviewer-1", { status: "done", supervised: false, pid: 0 })];
      itemsShow("f-ptyhost-reconnect");
      await new Promise(r => setTimeout(r, 300));
      for (const it of [itemsDetail, itemsList.find(x => x.id === "f-ptyhost-reconnect")]) {
        const st = it && it.steps && it.steps[2];
        if (st && !st.cards.some(c => c.name === "reviewer-1")) st.cards.push({ name: "reviewer-1", role: "reviewer" });
        if (it && !it.cards.some(c => c.name === "reviewer-1")) it.cards.push({ name: "reviewer-1", id: "c-reviewer-1", room: "sg4" });
      }
      itemsPaint(true);
      return !!document.querySelector(".it-detail");
    }, minion);
    await paint(false);
    await p.waitForSelector(".it-detail .it-card");
    const plain = await p.evaluate(() => document.querySelectorAll(".it-detail .it-card.minion").length);
    await p.locator(".it-detail .it-step >> nth=2").screenshot({ path: path.join(imgs, "item-minion-chip-before.png") });
    await paint(true);
    await p.locator(".it-detail .it-step >> nth=2").screenshot({ path: path.join(imgs, "item-minion-chip-after.png") });
    const got = await p.evaluate(() => {
      const word = (id) => { const d = document.createElement("div"); d.innerHTML = itemChip(id, id); const b = d.firstChild; return { cls: b.className, tag: b.tagName, st: (b.querySelector(".it-mst") || {}).textContent, tip: b.dataset.tip }; };
      const tags = ["atrium:minion"];
      const mk = (id, extra) => Object.assign({ id, tags, status: "running", supervised: true, pid: 1, display_title: id, wire_name: id }, extra || {});
      termTask = null;
      lastTasks = [mk("run"), mk("ask", { status: "needs-input", asks_open: 1 }), mk("perm", { status: "needs-permission" }),
        mk("end", { status: "done", supervised: false, pid: 0 }), mk("worker", { tags: ["atrium:item"] })];
      return { run: word("run"), ask: word("ask"), perm: word("perm"), end: word("end"), worker: word("worker"), gone: word("nobody") };
    });
    if (plain) fail("itemMinionChip: an ordinary card drew a minion chip");
    if (got.run.st !== "running" || !/ minion/.test(" " + got.run.cls) || got.run.cls.includes("hot")) fail("itemMinionChip: running chip is " + JSON.stringify(got.run));
    if (got.ask.st !== "needs you" || !got.ask.cls.includes("hot")) fail("itemMinionChip: a question is not hot: " + JSON.stringify(got.ask));
    if (got.perm.st !== "needs permission" || !got.perm.cls.includes("hot")) fail("itemMinionChip: a permission is not hot: " + JSON.stringify(got.perm));
    if (got.end.st !== "ended" || got.end.cls.includes("hot")) fail("itemMinionChip: ended chip is " + JSON.stringify(got.end));
    if (got.worker.cls !== "it-card" || got.worker.st) fail("itemMinionChip: a worker chip changed: " + JSON.stringify(got.worker));
    if (got.gone.tag !== "SPAN" || !got.gone.cls.includes("gone")) fail("itemMinionChip: a missing card is not a disabled chip: " + JSON.stringify(got.gone));
    if (errors.length) fail("itemMinionChip: page errors: " + errors.join("; "));
    else console.log("itemMinionChip ok");
  } finally { await ctx.close(); mock.hubMode = wasHub; }
}

// The landed check on an item's detail (docs/changes/m-doc-debt-p5.md): "docs: not said" for a landed feature, "close d-7?" once for a
// landed doc worker's item, nothing for an item the hub said nothing of, and the offer stays dismissed.
async function itemDocsLandedSection(browser, base) {
  const wasHub = mock.hubMode;
  mock.hubMode = true;
  const ctx = await browser.newContext({ viewport: { width: 1200, height: 900 } });
  const errors = [];
  const imgs = docImgDir("docs", "changes", "img", "m-doc-debt-p5");
  try {
    fs.mkdirSync(imgs, { recursive: true });
    const p = await ctx.newPage();
    p.on("pageerror", e => errors.push(String(e)));
    await p.route(/\/_hub\/items/, r => r.fulfill({ status: 500, body: "the mock must not reach the hub" }));
    await p.goto(base + "/?itemsmock=1", { waitUntil: "domcontentloaded" });
    await p.waitForFunction(() => { const t = document.querySelector('.tab[data-view="items"]'); return t && !t.hidden; }, null, { timeout: slow(15000) });
    await p.waitForTimeout(1000);
    await p.evaluate(() => { localStorage.removeItem("atrium.items.docsOfferDismissed"); switchView("items"); });
    await p.waitForFunction(() => document.querySelectorAll("#items-list .it-row").length > 4, null, { timeout: slow(10000) });
    // Paint the detail of one item with the hub's `docs` as given, and say what is drawn.
    const paint = (docs) => p.evaluate(async docs => {
      itemsShow("f-ptyhost-reconnect");
      await new Promise(r => setTimeout(r, 300));
      for (const it of [itemsDetail, itemsList.find(x => x.id === "f-ptyhost-reconnect")]) { if (it) { if (docs) it.docs = docs; else delete it.docs; } }
      itemsPaint(true);
      const q = (s) => document.querySelector(".it-detail " + s);
      return { note: (q('[data-docs="not-said"]') || {}).textContent || "", offer: (q('[data-docs="offer"]') || {}).textContent || "", any: document.querySelectorAll(".it-detail .it-docs").length };
    }, docs);
    const none = await paint(null);
    await p.waitForSelector(".it-detail");
    await p.screenshot({ path: path.join(imgs, "item-detail-before.png"), clip: { x: 0, y: 0, width: 1200, height: 420 } });
    const note = await paint({ not_said: true });
    await p.screenshot({ path: path.join(imgs, "item-detail-not-said.png"), clip: { x: 0, y: 0, width: 1200, height: 420 } });
    const offer = await paint({ offer: { id: "d-7", title: "docs behind f-ptyhost-reconnect" } });
    await p.screenshot({ path: path.join(imgs, "item-detail-offer.png"), clip: { x: 0, y: 0, width: 1200, height: 420 } });
    await p.click('.it-detail [data-act="docs-dismiss"]');
    const dismissed = await p.evaluate(() => document.querySelectorAll(".it-detail .it-docs").length);
    const again = await paint({ offer: { id: "d-7", title: "docs behind f-ptyhost-reconnect" } });
    if (none.any) fail("itemDocsLanded: an item the hub said nothing of drew a docs line: " + JSON.stringify(none));
    if (!/^docs: not said/.test(note.note) || note.offer) fail("itemDocsLanded: the note is " + JSON.stringify(note));
    if (!/close d-7\?/.test(offer.offer) || offer.note) fail("itemDocsLanded: the offer is " + JSON.stringify(offer));
    if (dismissed || again.any) fail("itemDocsLanded: a dismissed offer came back: " + dismissed + " " + JSON.stringify(again));
    if (errors.length) fail("itemDocsLanded: page errors: " + errors.join("; "));
    else console.log("itemDocsLanded ok");
  } finally { await ctx.close(); mock.hubMode = wasHub; }
}


async function itemOpenHiddenCardSection(browser, base) {
  const ctx = await browser.newContext({ viewport: { width: 1200, height: 900 } });
  const errors = [];
  try {
    const p = await ctx.newPage();
    p.on("pageerror", e => errors.push(String(e)));
    await p.goto(base + "/", { waitUntil: "domcontentloaded" });
    await p.waitForFunction(() => typeof itemsOpenCard === "function" && typeof renderTermList === "function", null, { timeout: slow(15000) });
    const got = await p.evaluate(async () => {
      const m = { id: "m1", tags: ["atrium:director", "atrium:minion", "atrium:item:i1"], status: "running", supervised: true, pid: 1, display_title: "minion", wire_name: "m1" };
      const s = { id: "s1", tags: [], status: "running", supervised: true, pid: 1, display_title: "mine", wire_name: "s1" };
      lastTasks = [m, s]; cardRows = new Map([m, s].map(t => [t.id, t])); cardsLoaded = true; termTask = null; loadCards = async () => {};
      const before = cardHidden(m);
      openTerm = card => { termTask = card; };
      await itemsOpenCard("m1");
      await renderTermList();
      const ids = Array.from(document.querySelectorAll("#term-list [data-id]")).map(e => e.getAttribute("data-id"));
      const kept = cardHidden(m);
      termTask = null;
      return { before, kept, ids, attached: true };
    });
    if (!got.before) fail("itemOpenHiddenCard: the minion was not hidden to begin with");
    if (got.kept) fail("itemOpenHiddenCard: the attached minion is hidden");
    if (!got.ids.includes("m1")) fail("itemOpenHiddenCard: its row went while attached: " + got.ids);
    if (errors.length) fail("itemOpenHiddenCard: page errors: " + errors.join("; "));
    else console.log("itemOpenHiddenCard ok");
  } finally { await ctx.close(); }
}

async function interviewTopSection(browser, base) {
  const ctx = await browser.newContext({ viewport: { width: 900, height: 600 } });
  const errors = [];
  try {
    const p = await ctx.newPage();
    p.on("pageerror", e => errors.push(String(e)));
    await p.goto(base + "/", { waitUntil: "domcontentloaded" });
    await p.waitForFunction(() => typeof termNest === "function" && typeof subagentRollup === "function" && typeof termKidsToggle === "function", null, { timeout: slow(15000) });
    const got = await p.evaluate(() => {
      const mk = (id, tags, by) => ({ id, tags, spawned_by_id: by || "", status: "running", display_title: id });
      const parent = mk("p1", ["origin:agent"]);
      const worker = mk("w1", ["atrium:subagent"], "p1");
      const iv = mk("i1", ["atrium:subagent", "atrium:interview"], "p1");
      const nest = termNest([parent, worker, iv]);
      const roll = subagentRollup([parent, worker, iv], false);
      termKids = new Map();
      const lone = termKidsToggle({ id: "p1" }, 0, 0, false);
      return { nested: [...nest.nested], rolled: [...roll.rolled], rollable: roll.rollable, lone, rule: typeof nestsUnderLauncher };
    });
    if (got.rule !== "function") fail("interviewTop: nestsUnderLauncher is not defined");
    if (got.nested.includes("i1")) fail("interviewTop: the interview nested under its launcher in the terminals list: " + got.nested);
    if (!got.nested.includes("w1")) fail("interviewTop: an ordinary subagent stopped nesting: " + got.nested);
    if (got.rolled.includes("i1")) fail("interviewTop: the interview was rolled up under its launcher in the stack/board: " + got.rolled);
    if (!got.rolled.includes("w1")) fail("interviewTop: an ordinary subagent stopped rolling up: " + got.rolled);
    // A launcher with children that are filtered out of the list still has a chevron: the total counts them.
    const chev = await p.evaluate(() => {
      const t = { id: "p1" };
      return { filtered: termKidsToggle(t, 2, 2, true), none: termKidsToggle(t, 0, 0, false), pool: typeof termKidTotal };
    });
    if (!chev.filtered.includes("tkidfold")) fail("interviewTop: no chevron on a launcher whose children are hidden");
    if (chev.none !== "") fail("interviewTop: a chevron on a card with no children");
    if (chev.pool !== "function") fail("interviewTop: termKidTotal (children in the whole pool, drawn or not) is not defined");
    if (errors.length) fail("interviewTop: page errors: " + errors.join("; "));
    else console.log("interviewTop ok");
  } finally { await ctx.close(); }
}

async function kidCountsSection(browser, base) {
  const ctx = await browser.newContext({ viewport: { width: 1280, height: 800 } });
  const errors = [];
  try {
    const p = await ctx.newPage();
    p.on("pageerror", e => errors.push(String(e)));
    await p.goto(base + "/", { waitUntil: "domcontentloaded" });
    await p.waitForFunction(() => typeof termChildInfo === "function" && typeof peekBody === "function", null, { timeout: slow(15000) });
    const got = await p.evaluate(() => {
      const kid = (id, extra) => Object.assign({ id, display_title: id, status: "running", supervised: true, pid: 9, tags: ["atrium:subagent"],
        spawned_by_id: "P", worktree: "/w", activity: { what: "tool", tool: "Bash" } }, extra);
      const gone = (id, extra) => kid(id, Object.assign({ supervised: false, pid: 0, resume_id: "r", activity: null }, extra));
      const pool = [
        { id: "P", display_title: "the parent", status: "running", supervised: true, pid: 9, tags: ["origin:agent"], worktree: "/w" },
        kid("runner"),
        kid("asker", { status: "needs-input", activity: null, ask: "which one?", asks_open: 1 }),
        gone("pausedA", { status: "paused" }),
        gone("parkedB", { status: "needs-input", parked_at: "2026-10-13T10:00:00Z" }),
        gone("deadC", { status: "dead" }),
        { id: "lonely", display_title: "lonely", status: "running", supervised: true, pid: 9, tags: [], worktree: "/w" },
      ];
      lastTasks = pool;
      const rows = {};
      for (const chip of [[], ["subagents"]]) {
        localStorage.setItem(termDeviceKey(TERM_SHOW_KEY), JSON.stringify(chip));
        const show = termShow();
        const shown = pool.filter(t => termListed(t, show));
        const nest = termNest(shown);
        termKids = nest.kids; termKidsPool = termNest(pool).kids;
        const host = document.createElement("div");
        host.innerHTML = termRow(pool[0], false, false);
        rows[chip.length ? "open" : "shut"] = {
          n: (host.querySelector(".tkidn") || {}).textContent || null,
          drawn: [...host.querySelectorAll(".tkids .card[data-id]")].map(c => c.dataset.id),
          text: host.textContent,
        };
      }
      localStorage.removeItem(termDeviceKey(TERM_SHOW_KEY));
      const box = document.createElement("div");
      box.innerHTML = peekBody(pool[0], null);
      peekDrawers(box);
      const sec = box.querySelector(".peek-sec.kids");
      const btns = sec ? [...sec.querySelectorAll("button.peek-kid")].map(b => ({ id: b.dataset.id, text: b.textContent })) : [];
      const lone = document.createElement("div");
      lone.innerHTML = peekBody(pool[6], null);
      return { rows, text: (box.querySelector('.peek-drawer[data-key="kids"] .peek-dsum') || {}).textContent || null, btns, loneSection: !!lone.querySelector(".peek-sec.kids"), opens: typeof peekOpenKid };
    });
    if (got.rows.shut.n !== "2") fail("kidCounts: the folded row counts " + got.rows.shut.n + ", want the 2 listed children");
    if (got.rows.open.drawn.length !== 2) fail("kidCounts: the open row draws " + JSON.stringify(got.rows.open.drawn) + ", want 2");
    if (got.rows.open.drawn.length !== Number(got.rows.shut.n)) fail("kidCounts: the folded count and the open rows disagree");
    if (/hidden/i.test(got.rows.shut.text)) fail("kidCounts: the row carries hidden text: " + got.rows.shut.text);
    const t = got.text || "";
    if (t !== "5 · 1 need you · 1 running · 2 paused · 1 dead") fail("kidCounts: the children heading is " + JSON.stringify(t));
    if (got.btns.length !== 2 || got.btns[0].id !== "asker" || got.btns[0].text !== "asker") fail("kidCounts: the need-you names are " + JSON.stringify(got.btns));
    if (got.loneSection) fail("kidCounts: a card with no children has a children section");
    if (got.opens !== "function") fail("kidCounts: peekOpenKid is not defined");
    if (errors.length) fail("kidCounts: page errors: " + errors.join("; "));
    else console.log("kidCounts ok");
  } finally { await ctx.close(); }
}

// THE UNIFIED CARD ON THE BOARD: columns follow cardState (needs you, working, done), one count line, grey only for off,
// the one orange for waiting, a neutral runner mark, chips that wrap, and a capped done column with no pinned group.
async function unifiedCardSection(browser, base) {
  const ctx = await browser.newContext({ viewport: { width: 1400, height: 900 } });
  const p = await ctx.newPage();
  const errors = [];
  p.on("pageerror", e => errors.push(e.message));
  const was = mock.tasksMode;
  try {
    mock.tasksMode = "unified";
    await p.goto(base + "/", { waitUntil: "domcontentloaded" });
    await p.waitForFunction(() => typeof renderBoard === "function" && typeof cardsLoaded !== "undefined" && cardsLoaded,
      null, { timeout: slow(15000) });
    await p.evaluate(() => { setGrouping({ on: false }, "board"); switchView("board"); });
    await p.waitForSelector('#board .col .card', { timeout: slow(10000) });
    const r = await p.evaluate(() => {
      const ids = sel => [...document.querySelectorAll(sel)].map(c => c.dataset.id).sort();
      const px = v => { const e = document.createElement("i"); e.style.color = v; document.body.appendChild(e);
        const c = getComputedStyle(e).color; e.remove(); return c; };
      const card = id => document.querySelector('#board .card[data-id="' + id + '"]');
      const wait = px("var(--wait)"), label = px("var(--label)");
      const stateChip = id => card(id).querySelector(".chip.state");
      const grey = [...document.querySelectorAll("#board .card")].map(c => [c.dataset.id, getComputedStyle(c).filter]);
      const chips = card("uc-quiet").querySelector(".card-line > .chips");
      return {
        cols: [...document.querySelectorAll("#board .col")].map(c => c.dataset.column),
        needs: ids('#board [data-column="needs-you"] .card'), working: ids('#board [data-column="working"] .card'),
        done: ids('#board [data-column="done"] .card'),
        count: (document.getElementById("board-count") || {}).textContent || "",
        pulses: document.querySelectorAll("#board .pulse").length, pins: document.querySelectorAll("#board .pins").length,
        grey, wait, label,
        edge: getComputedStyle(card("uc-agent"), "::before").backgroundColor,
        chipColor: stateChip("uc-agent") ? getComputedStyle(stateChip("uc-agent")).color : "none",
        permColor: stateChip("uc-perm") ? getComputedStyle(stateChip("uc-perm")).color : "none",
        quietChip: stateChip("uc-quiet") ? getComputedStyle(stateChip("uc-quiet")).color : "none",
        quietMark: getComputedStyle(card("uc-quiet").querySelector(".rmark")).color,
        waitMark: getComputedStyle(card("uc-ready").querySelector(".rmark")).color,
        wrap: getComputedStyle(chips).flexWrap, overflowX: getComputedStyle(chips).overflowX,
        workMarks: document.querySelectorAll("#board .rmark.working").length,
        liveDots: [...document.querySelectorAll("#board .chip.live")].filter(e => getComputedStyle(e, "::before").animationName !== "none").length
      };
    });
    const eq = (what, got, want) => { if (JSON.stringify(got) !== JSON.stringify(want)) fail("unifiedCard: " + what + " is " + JSON.stringify(got) + ", want " + JSON.stringify(want)); };
    eq("columns", r.cols, ["needs-you", "working", "done"]);
    eq("needs you", r.needs, ["uc-agent", "uc-perm", "uc-ready"]);
    eq("working", r.working, ["uc-quiet", "uc-work"]);
    eq("done holds every card with no runner, a gone ready card included", r.done, ["uc-dead", "uc-done", "uc-gone", "uc-parked", "uc-paused"]);
    if (!/^5 running: 3 need you, 2 working/.test(r.count) || !/5 done/.test(r.count)) fail("unifiedCard: count line is '" + r.count + "'");
    eq("pulse dots", r.pulses, 0);
    eq("pinned groups", r.pins, 0);
    for (const [id, f] of r.grey) {
      const off = ["uc-gone", "uc-parked", "uc-paused", "uc-done", "uc-dead"].includes(id);
      if (off !== f.includes("grayscale")) fail("unifiedCard: " + id + " filter is '" + f + "': grey is for off cards and nothing else");
    }
    eq("the agent-launched waiting card's edge is the one orange", r.edge, r.wait);
    eq("its state chip is the one orange", r.chipColor, r.wait);
    eq("a permission chip is the one orange", r.permColor, r.wait);
    if (r.quietChip === r.wait) fail("unifiedCard: a live quiet card's chip uses the waiting orange");
    eq("a live claude mark is neutral", r.quietMark, r.label);
    eq("a waiting claude mark takes the orange", r.waitMark, r.wait);
    eq("chips wrap", r.wrap, "wrap");
    if (r.overflowX === "auto" || r.overflowX === "scroll") fail("unifiedCard: the chip row scrolls sideways, so it clips");
    eq("working marks", r.workMarks, 1);
    eq("animated live dots", r.liveDots, 0);

    // 111 finished: capped, with a more row, and not wider than a quiet column.
    mock.tasksMode = "unifiedMany";
    await p.evaluate(async () => { await loadCards(); renderBoard(); });
    await p.waitForSelector('#board [data-column="done"] .card', { timeout: slow(10000) });
    const m = await p.evaluate(() => {
      const col = document.querySelector('#board [data-column="done"]');
      return { n: col.querySelectorAll(".card").length, more: (col.querySelector(".more") || {}).textContent || "",
        head: col.querySelector(".col-head .n").textContent, pins: document.querySelectorAll("#board .pins").length,
        first: col.querySelector(".card").dataset.id, w: col.getBoundingClientRect().width };
    });
    if (m.n > 12 || m.n < 6) fail("unifiedCard: done draws " + m.n + " cards, want 6 to 12");
    if (!new RegExp("^" + (111 - m.n) + " more finished, 17 of them pinned").test(m.more.trim())) fail("unifiedCard: more row says '" + m.more.trim() + "'");
    eq("done header keeps the total", m.head, "111");
    eq("a pinned card is first in done", m.first, "ud0");
    eq("pinned groups with 111 done", m.pins, 0);
    if (m.w > 360) fail("unifiedCard: done is " + m.w + "px wide, it must not grow with its count");
    if (errors.length) fail("unifiedCard: page errors: " + errors.join(" | "));
  } finally { mock.tasksMode = was; await ctx.close(); }
  if (!bad) console.log("unifiedCard ok");
}

// A card that spawned a running card is not waiting on the human: no ready alert until the last child ends.
// Driven through the real pass with /v1/tasks mocked, so the call site's filter is what is under test.
async function noReadyChildrenSection(browser, base) {
  const ctx = await browser.newContext({ viewport: { width: 1400, height: 900 } });
  const page = await ctx.newPage();
  const errors = [];
  page.on("pageerror", e => errors.push(String(e)));
  await page.addInitScript(() => {
    window.__atriumReadyQuietMs = 300;
    Document.prototype.hasFocus = () => true;
  });
  let rows = [];
  await page.route(u => new URL(u).pathname === "/v1/tasks", route =>
    route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ tasks: rows }) }));
  const card = (id, status, extra) => ({ id, wire_name: id, status, supervised: true, tags: [], title: id,
    display_title: id, created_at: "2026-09-01T10:00:00Z", ...extra });
  try {
    await page.clock.install();
    await page.goto(base, { waitUntil: "domcontentloaded" });
    await page.waitForFunction(() => typeof alerting !== "undefined" && typeof refresh === "function", null,
      { timeout: slow(15000) });
    await fakePause(page);
    await fakeTick(page, 1500);
    await page.evaluate(() => { localStorage.removeItem("atrium.toastlog"); alerting.set({ debounce: 0, muted: true }); });
    const step = async list => {
      rows = list;
      await page.evaluate(() => refresh());
      await fakeTick(page, 700);
    };
    const rings = who => page.evaluate(w => toastLog().filter(e => e.title.startsWith(w + " ")).length, who);
    const kid = (id, status) => card(id, status, { tags: ["atrium:subagent"], spawned_by_id: "par" });
    await step([card("par", "running"), kid("k1", "running"), kid("k2", "running"), card("solo", "running")]);
    // The parent ends its turn with two running children, and a card with no children does the same.
    await step([card("par", "needs-input", { waiting_since: "W1" }), kid("k1", "running"), kid("k2", "running"),
      card("solo", "needs-input", { waiting_since: "W1" })]);
    if (await rings("par")) fail("noReadyChildren: a ready alert fired for a card with running children");
    if (await rings("solo") !== 1) fail("noReadyChildren: a card with no children did not ring once");
    // One child ends and one still runs.
    await step([card("par", "needs-input", { waiting_since: "W1" }), kid("k1", "done"), kid("k2", "running"),
      card("solo", "needs-input", { waiting_since: "W1" })]);
    if (await rings("par")) fail("noReadyChildren: a ready alert fired while one child still ran");
    // The last child ends and the parent is still idle.
    await step([card("par", "needs-input", { waiting_since: "W1" }), kid("k1", "done"), kid("k2", "done"),
      card("solo", "needs-input", { waiting_since: "W1" })]);
    if (await rings("par") !== 1) fail("noReadyChildren: no ready alert after the last child ended: " + await rings("par"));
    if (await rings("solo") !== 1) fail("noReadyChildren: the childless card rang again: " + await rings("solo"));
    // The state stays visible on the card: it is still in the waiting set.
    const waiting = await page.evaluate(() => cardsWaiting().map(t => t.id).sort().join());
    if (waiting !== "par,solo") fail("noReadyChildren: the ready state is not kept on the card: " + waiting);
    if (errors.length) fail("noReadyChildren: page errors: " + errors.join(" | "));
  } finally { await ctx.close(); }
  if (!bad) console.log("noReadyChildren ok");
}

// ── a wait found, not happened, never rings ──────────────────────────────────
// 2026-10-13: a minute after a hub deploy the operator was told about a card whose turn ended a week before. Only a
// turn that ends while the board is open is news. A card waiting since days ago is quiet whether it is there at load,
// arrives after a hub start served no cards, or starts counting later (an idle agent card cardsReady now takes in).
// Nobody is looking, so every alert would reach the OS notification, and each one is also in the bell's log.
async function readyNoSeedSection(browser, base) {
  const ctx = await browser.newContext({ viewport: { width: 1400, height: 900 } });
  const page = await ctx.newPage();
  const errors = [];
  page.on("pageerror", e => errors.push(String(e)));
  await page.addInitScript(() => {
    window.__atriumReadyQuietMs = 300;
    Document.prototype.hasFocus = () => false;
    window.__notes = [];
    window.Notification = function (title, o) { this.title = title; this.o = o; window.__notes.push(this); };
    window.Notification.permission = "granted";
    window.Notification.requestPermission = async () => "granted";
    window.Notification.prototype.close = function () {};
  });
  const week = new Date(Date.now() - 7 * 86400000).toISOString();
  const card = (id, status, extra) => ({ id, wire_name: id, status, supervised: true, tags: [], title: id,
    display_title: id, created_at: "2026-09-01T10:00:00Z", ...extra });
  const old = card("rns-old", "needs-input", { waiting_since: week });
  const late = card("rns-late", "needs-input", { waiting_since: week });
  const idle = card("rns-idle", "needs-input", { waiting_since: week, tags: ["origin:agent"] });
  const turn = card("rns-turn", "running");
  let rows = [old, turn];
  await page.route(u => new URL(u).pathname === "/v1/tasks", route =>
    route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ tasks: rows }) }));
  try {
    await page.clock.install();
    await page.goto(base, { waitUntil: "domcontentloaded" });
    await page.waitForFunction(() => typeof alerting !== "undefined" && typeof refresh === "function", null,
      { timeout: slow(15000) });
    await fakePause(page);
    await fakeTick(page, 1500);
    await page.evaluate(() => alerting.set({ debounce: 0, muted: true, desktop: true }));
    const step = async list => {
      rows = list;
      await page.evaluate(() => refresh());
      await fakeTick(page, 800);
    };
    const said = () => page.evaluate(() => ({
      notes: window.__notes.map(n => n.title).filter(t => /^rns-/.test(t)),
      toasts: [...document.querySelectorAll("#toasts .toast")].map(t => t.textContent).filter(t => /rns-/.test(t)),
      log: toastLog().filter(e => /^rns-/.test(e.title)).map(e => e.title + " x" + (e.n || 1))
    }));
    const none = (what, s) => { if (s.notes.length || s.toasts.length || s.log.length) fail("readyNoSeed: " + what + ": " + JSON.stringify(s)); };
    await step([old, turn]);
    none("a card waiting a week at load was announced", await said());
    // a hub start served the board before its room came back, then the room's cards arrive, a week old
    await step([]);
    await step([old, late, turn]);
    none("a week-old wait arriving after the first pass was announced", await said());
    // an idle agent card that only starts counting on a later poll
    await step([old, late, idle, turn]);
    none("an idle agent card counted by a later poll was announced", await said());
    // a turn that ends now is the one event: one line in the log, said once as a notification or a toast
    await step([old, late, idle, card("rns-turn", "needs-input", { waiting_since: new Date().toISOString() })]);
    await fakeTick(page, 600);
    const s = await said();
    if (s.log.join() !== "rns-turn is ready x1" || s.notes.length + s.toasts.length !== 1) {
      fail("readyNoSeed: a turn that ended while the board was open was not exactly one notification: " + JSON.stringify(s));
    }
    if (errors.length) fail("readyNoSeed: page errors: " + errors.join(" | "));
  } finally { await ctx.close(); }
  if (!bad) console.log("readyNoSeed ok");
}

// The hub's repos list (js/hubrepos.js): a mocked /_hub/git/repos, two repos (one empty), branches with and without a known
// card, a released one, both clone URLs, the empty state and a failing fetch.
// THE ITEMS TAB: grouped by state with blocked first and finished folded, the question and answer box on a blocked row,
// cancel behind a confirm, a detail with the history, a count of what waits on the human, and a refresh on the `item`
// event. The hub is a route here, so the POSTs it is sent can be read. See js/items.js.
// The items tab on the shape the live hub answers: an assignment row names its backlog item by `work_id` and carries no title, and
// the titles and steps are the work rows' (`/_hub/work`). Titles, steps, and moving between list, detail and back.
async function itemsRealShapeSection(browser, base) {
  const wasHub = mock.hubMode;
  mock.hubMode = true;
  const ctx = await browser.newContext({ viewport: { width: 1280, height: 700 } });
  const errors = [];
  try {
    const p = await ctx.newPage();
    p.on("pageerror", e => errors.push(e.message));
    const ago = m => new Date(Date.now() - m * 60000).toISOString();
    const names = ["designed", "approved", "building", "tested", "reviewed", "landed", "deployed", "pushed"];
    // Item n is at step n % 6: everything before it done, it active, the rest pending. The last few are finished.
    const rows = [], work = [];
    for (let n = 0; n < 30; n++) {
      const at = n % 6, fin = n >= 26;
      rows.push({ item_id: "5f0c8a1e-0000-4000-8000-0000000000" + String(n).padStart(2, "0"), work_id: "w-" + n, room: "sg4", card: "c-" + n, state: fin ? "finished" : "running",
        revision: 2, generation: 1, owner_kind: "session", owner: "orch@sg4", created_at: ago(900), updated_at: ago(n + 1) });
      work.push({ backlog_id: "w-" + n, title: "Backlog title " + n, state: fin ? "finished" : "running", steps: names.map((s, i) => ({ backlog_id: "w-" + n, step: s,
        state: fin ? (i < 6 ? "done" : "pending") : i < at ? "done" : i === at ? "active" : "pending", cards: "[]", started_at: ago(800) })) });
    }
    await p.route(/\/_hub\/work(\/[^/?]+)?(\?.*)?$/, r => {
      const id = decodeURIComponent(new URL(r.request().url()).pathname.split("/")[3] || "");
      const w = work.find(x => x.backlog_id === id);
      r.fulfill({ status: id && !w ? 404 : 200, contentType: "application/json", body: JSON.stringify(id ? { work: w, steps: w && w.steps } : { work }) });
    });
    await p.route(/\/_hub\/items(\/[^/?]+)?(\?.*)?$/, r => {
      const id = decodeURIComponent(new URL(r.request().url()).pathname.split("/")[3] || "");
      const it = rows.find(i => i.item_id === id);
      r.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(id ? { ...it, item: it, history: [] } : { items: rows }) });
    });
    await p.goto(base + "/", { waitUntil: "domcontentloaded" });
    await p.waitForFunction(() => { const t = document.querySelector('.tab[data-view="items"]'); return t && !t.hidden; }, null, { timeout: slow(15000) });
    await p.click('.tab[data-view="items"]');
    await p.waitForSelector(".it-row", { timeout: slow(10000) });
    // TITLES: the backlog item's title, never the assignment's uuid.
    const seen = await p.evaluate(() => [...document.querySelectorAll(".it-row")].map(r => ({ t: r.querySelector(".it-t").textContent,
      b: r.querySelector(".it-cur b").textContent, id: r.querySelector(".it-id").textContent })));
    if (seen.some(s => !/^Backlog title \d+$/.test(s.t))) fail("itemsRealShape: a row is titled " + JSON.stringify(seen.find(s => !/^Backlog title \d+$/.test(s.t))));
    if (seen.some(s => !/^w-\d+$/.test(s.id))) fail("itemsRealShape: the id shown is " + JSON.stringify(seen.map(s => s.id)));
    // STEPS: the line names the step in words. Item 1 is at approved, item 0 at designed, item 5 at landed.
    const by = {};
    for (const s of seen) by[s.t] = s.b;
    if (!/^step 2 of 8 · /.test(by["Backlog title 1"])) fail("itemsRealShape: item 1 reads " + by["Backlog title 1"] + ", want step 2 of 8");
    if (!/^step 1 of 8 · .*designed/.test(by["Backlog title 0"])) fail("itemsRealShape: item 0 reads " + by["Backlog title 0"] + ", want designed");
    if (!/^step 6 of 8 · .*landed/.test(by["Backlog title 5"])) fail("itemsRealShape: item 5 reads " + by["Backlog title 5"] + ", want landed");
    if (new Set(Object.values(by).map(v => v.replace(/ · .*/, ""))).size < 4) fail("itemsRealShape: the rows read " + JSON.stringify([...new Set(Object.values(by))]));
    // A POLL THAT FOUND NOTHING NEW repaints nothing: the row node is the same one.
    await p.evaluate(() => { document.querySelector(".it-row").dataset.mark = "1"; });
    await p.evaluate(() => loadItems());
    if (!await p.evaluate(() => !!document.querySelector('.it-row[data-mark="1"]'))) fail("itemsRealShape: a poll with no change repainted the list");
    // NAVIGATION: scroll the list, open a row, back by the crumb and by the browser's back button, each ending where it began.
    await p.evaluate(() => { const l = document.getElementById("items-list"); l.scrollTop = 0; document.scrollingElement.scrollTop = 0; });
    const place = () => p.evaluate(() => { let top = 0, el = document.getElementById("items-list");
      for (; el; el = el.parentElement) top = Math.max(top, el.scrollTop); return top; });
    await p.evaluate(() => { const row = [...document.querySelectorAll(".it-row")][14]; row.scrollIntoView({ block: "center" }); });
    const before = await place();
    if (before <= 0) fail("itemsRealShape: the list did not scroll, so the scroll check proves nothing");
    const target = await p.evaluate(() => { const r = [...document.querySelectorAll(".it-row")][14]; r.querySelector(".it-main").click(); return r.dataset.item; });
    await p.waitForSelector(".it-detail", { timeout: slow(5000) });
    if (await place() !== 0) fail("itemsRealShape: the detail opened scrolled to " + await place());
    const heading = await p.textContent(".it-detail h1");
    if (!/^Backlog title \d+$/.test(heading)) fail("itemsRealShape: the detail is titled " + heading);
    await p.click('.it-detail [data-act="back"]');
    await p.waitForSelector(".it-row");
    if (Math.abs(await place() - before) > 2) fail("itemsRealShape: back by the crumb landed at " + await place() + ", was " + before);
    if (!await p.evaluate(t => document.activeElement && document.activeElement.dataset.id === t, target)) fail("itemsRealShape: back did not return the focus to the row opened");
    await p.evaluate(t => itemsShow(t), target);
    await p.waitForSelector(".it-detail");
    await p.goBack();
    await p.waitForSelector(".it-row");
    if (!await p.evaluate(() => isViewing("items"))) fail("itemsRealShape: the back button left the items tab instead of closing the page");
    if (Math.abs(await place() - before) > 2) fail("itemsRealShape: back by the button landed at " + await place() + ", was " + before);
    await p.goForward();
    await p.waitForSelector(".it-detail");
    if (!await p.evaluate(t => itemsOpenId() === t, target)) fail("itemsRealShape: forward did not reopen the item");
  } finally {
    mock.hubMode = wasHub;
    await ctx.close();
  }
  if (errors.length) fail("itemsRealShape: threw " + errors.join(" | "));
  if (!bad) console.log("itemsRealShape ok");
}

async function itemsViewSection(browser, base) {
  const wasHub = mock.hubMode;
  mock.hubMode = true;
  const ctx = await browser.newContext({ viewport: { width: 1280, height: 800 } });
  const errors = [];
  try {
    const p = await ctx.newPage();
    p.on("pageerror", e => errors.push(e.message));
    const ago = m => new Date(Date.now() - m * 60000).toISOString();
    const mk = (id, state, extra) => Object.assign({ item_id: id, room: "sg4", card: "c-" + id, state, revision: 2, generation: 1,
      owner_kind: "human", owner: "", created_by: "orch@sg4", note: "", created_at: ago(300), updated_at: ago(5) }, extra);
    let items = [
      mk("it-run", "running", { owner_kind: "session", owner: "orch@sg4", updated_at: ago(90) }),
      mk("it-blk", "blocked", { note: "which key do I use?", updated_at: ago(12) }),
      mk("it-blk2", "blocked", { owner_kind: "session", owner: "orch@sg4", note: "not for you", updated_at: ago(30) }),
      mk("it-q", "queued", { updated_at: ago(2000) }),
      mk("it-done", "finished", { note: "done: abc123", updated_at: ago(600) }),
      mk("it-can", "finished", { note: "cancelled by owner", updated_at: ago(700) })];
    const posts = [];
    await p.route(/\/_hub\/items(\/[^/?]+)?(\?.*)?$/, async r => {
      const req = r.request(); const u = new URL(req.url());
      const id = decodeURIComponent(u.pathname.split("/")[3] || "");
      const json = o => r.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(o) });
      if (req.method() === "POST") {
        const b = JSON.parse(req.postData());
        posts.push({ id, ...b });
        if (b.do === "cancel") items = items.map(i => i.item_id === id ? { ...i, state: "finished", note: "cancelled by owner" } : i);
        return json({ ok: true });
      }
      if (id) {
        const it = items.find(i => i.item_id === id);
        return it ? json({ ...it, item: it, history: [{ revision: 1, state: "running", at: ago(200) }, { revision: 2, state: it.state, at: it.updated_at }] })
          : r.fulfill({ status: 404, contentType: "application/json", body: '{"error":"no such item"}' });
      }
      let list = items;
      if (u.searchParams.get("owner_kind")) list = list.filter(i => i.owner_kind === u.searchParams.get("owner_kind"));
      if (u.searchParams.get("state")) list = list.filter(i => u.searchParams.get("state").split(",").includes(i.state));
      json({ items: list });
    });
    await p.goto(base + "/", { waitUntil: "domcontentloaded" });
    await p.waitForFunction(() => { const t = document.querySelector('.tab[data-view="items"]'); return t && !t.hidden; }, null, { timeout: slow(15000) });
    await p.waitForTimeout(1000);
    // THE BADGE: only what is blocked on the human, before the tab is ever opened.
    await p.waitForFunction(() => document.getElementById("c-items").textContent === "1", null, { timeout: slow(10000) });
    await p.evaluate(() => {
      lastTasks = [{ id: "c-it-blk", room: "sg4", title: "pick the signing key", display_title: "pick the signing key", wire_name: "sg4-keys", status: "running" }];
      return switchView("items");
    });
    await p.waitForFunction(() => document.querySelectorAll("#items-list .it-row").length === 6, null, { timeout: slow(10000) });
    const got = await p.evaluate(() => ({
      groups: [...document.querySelectorAll("#items-list .it-group")].map(g => g.dataset.state),
      rows: [...document.querySelectorAll("#items-list .it-row")].map(r => r.dataset.item),
      finishedOpen: document.querySelectorAll('.it-group[data-state="finished"] .it-row').length,
      finishedN: document.querySelector('.it-group[data-state="finished"] .it-n').textContent,
      first: document.querySelector(".it-row").textContent,
      ask: [...document.querySelectorAll(".it-q")].map(q => q.textContent),
      boxes: document.querySelectorAll(".it-answer-box").length
    }));
    if (got.groups.join() !== "needs-you,idle,queued,finished") fail("itemsView: group order is " + got.groups);
    // One row per backlog id. A block owned by a session with no card alive is idle, and a card the board does not hold is an ended one.
    if (got.rows.join() !== "it-blk,it-run,it-blk2,it-q,it-done,it-can") fail("itemsView: row order is " + got.rows);
    if (got.finishedOpen !== 2 || got.finishedN !== "2") fail("itemsView: finished is not open with its count: " + JSON.stringify(got));
    const blkText = await p.evaluate(() => document.querySelector('.it-row[data-item="it-blk"]').textContent);
    for (const w of ["pick the signing key", "asked", "sg4-keys", "12m", "it-blk"]) if (!blkText.includes(w)) fail("itemsView: the blocked row lacks " + w + ": " + blkText);
    if (got.ask.join() !== "which key do I use?" || got.boxes !== 0) fail("itemsView: a blocked row lacks its question, or shows the box before answer is clicked: " + JSON.stringify(got));
    if (await p.locator("#items-list .it-row:not(.st-needs-you) [data-act=ask]").count()) fail("itemsView: a row that is not blocked offers an answer");
    if (await p.locator('.it-group[data-state="finished"]').count() !== 1) fail("itemsView: no finished group");
    if (process.env.ITEMS_SHOTS) {
      fs.mkdirSync(process.env.ITEMS_SHOTS, { recursive: true });
      await p.screenshot({ path: path.join(process.env.ITEMS_SHOTS, "items-after.png") });
    }
    // THE CARD LINK opens that card's terminal.
    await p.evaluate(() => { window.__attached = null; window.attachTask = async id => { window.__attached = id; }; });
    await p.click('.it-row[data-item="it-blk"] .it-cards summary');
    await p.click('.it-row[data-item="it-blk"] .it-cards .it-card');
    if ((await p.evaluate(() => window.__attached)) !== "c-it-blk") fail("itemsView: the card link did not attach the card");
    await p.evaluate(() => switchView("items"));
    await p.waitForFunction(() => document.querySelectorAll("#items-list .it-row").length === 6, null, { timeout: slow(10000) });
    // ANSWER: the box opens on the answer button, an empty box sends nothing, a typed one sends unblock as the board, and it survives a repaint while typed.
    await p.click('.it-row[data-item="it-blk"] [data-act=ask]');
    await p.waitForSelector('.it-answer-box[data-id="it-blk"]');
    await p.click('.it-row[data-item="it-blk"] .it-send');
    if (posts.length) fail("itemsView: an empty answer was sent");
    await p.fill('.it-answer-box[data-id="it-blk"]', "use KEY-9");
    await p.evaluate(() => loadItems());
    await p.waitForTimeout(300);
    if ((await p.inputValue('.it-answer-box[data-id="it-blk"]')) !== "use KEY-9") fail("itemsView: a refresh ate the typed answer");
    await p.click('.it-row[data-item="it-blk"] .it-send');
    await p.waitForFunction(() => document.getElementById("items-note").textContent.includes("Sent the answer"), null, { timeout: slow(10000) });
    if (!(posts[0].id === "it-blk" && posts[0].do === "unblock" && posts[0].by === "board" && posts[0].answer === "use KEY-9"))
      fail("itemsView: the answer posted " + JSON.stringify(posts[0]));
    // CANCEL: behind a confirm. Keeping it posts nothing, confirming posts cancel and the row moves to finished.
    await p.click('.it-row[data-item="it-run"] .it-title');
    await p.waitForSelector('.it-detail[data-detail="it-run"] .it-cancel');
    await p.click('.it-cancel');
    await p.waitForSelector("#ask[open]");
    await p.click('#ask button:has-text("keep it")');
    if (posts.length !== 1) fail("itemsView: cancel posted before it was confirmed");
    await p.click('.it-cancel');
    await p.waitForSelector("#ask[open]");
    await p.click('#ask button:has-text("cancel the item")');
    await p.waitForFunction(() => itemsList.find(i => i.id === "it-run").state === "finished", null, { timeout: slow(10000) });
    await p.click('[data-act=back]');
    await p.waitForFunction(() => !document.querySelector('.it-detail') && document.querySelector('.it-row[data-item="it-run"] .it-state').textContent === "cancelled", null, { timeout: slow(10000) });
    if (posts[1].do !== "cancel" || posts[1].id !== "it-run" || posts[1].by !== "board") fail("itemsView: the cancel posted " + JSON.stringify(posts[1]));
    // THE FOLD: finished shows the cancelled ones by their word, and folds shut.
    const fin = await p.evaluate(() => [...document.querySelectorAll('.it-group[data-state="finished"] .it-row')].map(r => r.dataset.item + ":" + r.querySelector(".it-state").textContent).join());
    if (fin !== "it-run:cancelled,it-done:finished,it-can:cancelled") fail("itemsView: the finished group reads " + fin);
    // DETAIL: a click on the row opens it with the note and the timeline, a second click shuts it.
    await p.click('.it-row[data-item="it-done"] .it-title');
    await p.waitForFunction(() => document.querySelectorAll('.it-detail[data-detail="it-done"] .it-history li b').length === 2, null, { timeout: slow(10000) });
    const det = await p.evaluate(() => document.querySelector('.it-detail[data-detail="it-done"]').textContent);
    if (!det.includes("done: abc123") || !det.includes("orch@sg4") || !det.includes("room sg4")) fail("itemsView: the detail reads " + det);
    await p.click('[data-act=back]');
    if (await p.locator(".it-detail").count()) fail("itemsView: back did not return to the list");
    // THE EVENT: a new blocked item shows up and the badge follows, without the tab being touched.
    items = [mk("it-new", "blocked", { note: "again?" }), ...items];
    await p.evaluate(() => onItemEvent());
    await p.waitForFunction(() => !!document.querySelector('.it-row[data-item="it-new"]') && document.getElementById("c-items").textContent === "2", null, { timeout: slow(10000) });
    if (errors.length) fail("itemsView: page errors: " + errors.join(" | "));
  } finally { await ctx.close(); mock.hubMode = wasHub; }
  if (!bad) console.log("itemsView ok");
}

// THE EFFICACY PAGE of the items tab, on the fixtures: a dash is not a 0%, a week with too few landed items says so, "observed only"
// refetches and drops the reported inputs, a reported figure wears its tag, and css/efficacy.css holds no raw colour.
async function itemsEfficacySection(browser, base) {
  const css = fs.readFileSync(path.join(WEB_ROOT, "css", "efficacy.css"), "utf8").replace(/\/\*[\s\S]*?\*\//g, "");
  const raw = (css.match(/#[0-9a-fA-F]{3,8}\b/g) || []).concat((css.match(/\b(?:rgba?|hsla?)\([^)]*\)/g) || []).filter(c => !/var\(/.test(c)));
  if (raw.length) fail("itemsEfficacy: efficacy.css holds raw colour: " + raw.slice(0, 5).join(" "));
  const wasHub = mock.hubMode;
  mock.hubMode = true;
  const ctx = await browser.newContext({ viewport: { width: 1400, height: 900 } });
  const errors = [];
  try {
    const p = await ctx.newPage();
    p.on("pageerror", e => errors.push(e.message));
    await p.route(/\/_hub\/(items|work)/, r => r.fulfill({ status: 500, body: "the mock must not reach the hub" }));
    await p.goto(base + "/?itemsmock=1", { waitUntil: "domcontentloaded" });
    await p.waitForFunction(() => { const t = document.querySelector('.tab[data-view="items"]'); return t && !t.hidden; }, null, { timeout: slow(15000) });
    await p.evaluate(() => switchView("items"));
    await p.waitForFunction(() => document.querySelectorAll("#items-list .it-row").length > 4, null, { timeout: slow(10000) });
    await p.click('#items-list [data-act="efficacy"]');
    await p.waitForSelector("#items-list .ef-rr", { timeout: slow(10000) });
    const on = await p.evaluate(() => ({ rows: document.querySelectorAll(".ef-rr").length, reported: document.querySelectorAll(".ef-rr .it-obs.r").length,
      landed: document.querySelectorAll(".ef-ir").length, tooFew: !!document.querySelector('[data-role="too-few"]') }));
    if (on.rows !== 8 || on.reported < 1 || on.landed !== 5 || on.tooFew) fail("itemsEfficacy: the page is " + JSON.stringify(on));
    await p.click('#items-list [data-act="eff-obs"]');
    await p.waitForFunction(() => document.querySelectorAll(".ef-rr .it-obs.r").length === 0, null, { timeout: slow(10000) });
    const off = await p.evaluate(() => ({ dash: !!document.querySelector('.ef-rr[data-row="bugs"] .ef-rv .ef-dash'), zero: /^0%$/.test(document.querySelector('.ef-rr[data-row="bugs"] .ef-rv').textContent.trim()),
      landed: document.querySelectorAll(".ef-ir").length }));
    if (!off.dash || off.zero || off.landed !== 4) fail("itemsEfficacy: observed only left " + JSON.stringify(off));
    const few = await p.evaluate(async () => {
      effData.periods[effData.periods.length - 1].too_few = true; effData.periods[effData.periods.length - 1].landed = 3;
      itemsPaint(true);
      return (document.querySelector('[data-role="too-few"]') || {}).textContent || "";
    });
    if (!/Too few to trend/.test(few)) fail("itemsEfficacy: a thin week did not say so: " + few);
    if (errors.length) fail("itemsEfficacy: page errors: " + errors.join(" | "));
  } finally { await ctx.close(); mock.hubMode = wasHub; }
  if (!bad) console.log("itemsEfficacy ok");
}

// THE ITEMS TAB, on the fixtures (`?itemsmock=1`): list, detail and blocked draw from js/items-mock.js, an answer posts through the mock,
// a skin change repaints the tab, and css/items.css holds no raw colour. Nothing here touches a hub.
async function itemsMockSection(browser, base) {
  const css = fs.readFileSync(path.join(WEB_ROOT, "css", "items.css"), "utf8").replace(/\/\*[\s\S]*?\*\//g, "");
  const raw = (css.match(/#[0-9a-fA-F]{3,8}\b/g) || []).concat((css.match(/\b(?:rgba?|hsla?)\([^)]*\)/g) || []).filter(c => !/var\(/.test(c)));
  if (raw.length) fail("itemsMock: items.css holds raw colour: " + raw.slice(0, 5).join(" "));
  const wasHub = mock.hubMode;
  mock.hubMode = true;
  const ctx = await browser.newContext({ viewport: { width: 1400, height: 900 } });
  const errors = [];
  try {
    const p = await ctx.newPage();
    p.on("pageerror", e => errors.push(e.message));
    await p.route(/\/_hub\/items/, r => r.fulfill({ status: 500, body: "the mock must not reach the hub" }));
    await p.goto(base + "/?itemsmock=1", { waitUntil: "domcontentloaded" });
    await p.waitForFunction(() => { const t = document.querySelector('.tab[data-view="items"]'); return t && !t.hidden; }, null, { timeout: slow(15000) });
    await p.waitForTimeout(1000);
    await p.evaluate(() => switchView("items"));
    await p.waitForFunction(() => document.querySelectorAll("#items-list .it-row").length > 4, null, { timeout: slow(10000) });
    const list = await p.evaluate(() => ({ stats: document.querySelectorAll(".it-chip").length, segs: document.querySelector(".it-prog").children.length,
      mock: !!document.getElementById("it-mockbar"), blocked: document.querySelectorAll(".it-row.st-needs-you").length }));
    if (list.stats < 4 || list.segs !== 8 || !list.mock || list.blocked < 1) fail("itemsMock: the list is " + JSON.stringify(list));
    // A SKIN CHANGE repaints the tab: the same elements read another colour.
    const paint = () => p.evaluate(() => [".it-chip.hot b", ".it-prog span.d"].map(q => { const e = document.querySelector(q); return e ? getComputedStyle(e).color + "|" + getComputedStyle(e).backgroundColor : "none"; }).join());
    const before = await paint();
    await p.evaluate(() => document.documentElement.setAttribute("data-skin", "paper"));
    if (await paint() === before) fail("itemsMock: a skin change did not repaint the tab: " + before);
    await p.evaluate(() => document.documentElement.removeAttribute("data-skin"));
    // THE DETAIL: a timeline of eight steps and the side column.
    await p.click(".it-row .it-title >> nth=0");
    await p.waitForSelector(".it-detail .it-step");
    const det = await p.evaluate(() => ({ steps: document.querySelectorAll(".it-detail .it-step").length, side: !!document.querySelector(".it-side .it-big"),
      est: /estimated/.test(document.querySelector(".it-side").textContent) }));
    if (det.steps !== 8 || !det.side || !det.est) fail("itemsMock: the detail is " + JSON.stringify(det));
    await p.click("[data-act=back]");
    // BLOCKED: the answer posts through the mock and the row stops being blocked.
    const id = await p.evaluate(() => document.querySelector(".it-row.st-needs-you").dataset.item);
    await p.click('.it-row[data-item="' + id + '"] [data-act=ask]');
    await p.fill('.it-answer-box[data-id="' + id + '"]', "go ahead");
    await p.click('.it-row[data-item="' + id + '"] .it-send');
    await p.waitForFunction(i => { const it = itemsList.find(x => x.id === i); return it && it.state !== "blocked"; }, id, { timeout: slow(10000) });
    // THE QUESTION ON THE ROW: a worker's suggested answers are buttons on the row, the first one orange, and a click types that answer.
    await p.evaluate(() => loadItems());
    const opts = await p.evaluate(() => ({ rows: document.querySelectorAll(".it-row.st-needs-you").length,
      btns: [...document.querySelectorAll(".it-row.st-needs-you .it-opt")].map(b => b.textContent + (b.classList.contains("go") ? "*" : "")) }));
    if (opts.rows !== 1 || opts.btns.length !== 0) fail("itemsMock: after the first answer the rows are " + JSON.stringify(opts));
    // APPROVE and SEND BACK go in the same box for a design waiting on the human, and are not an answer to a worker.
    const dsn = await p.evaluate(() => { const r = document.querySelector(".it-row.st-needs-you"); return { id: r.dataset.item, approve: !!r.querySelector("[data-act=approve]"),
      send: !!r.querySelector('[data-act=ask]'), opts: r.querySelectorAll(".it-opt").length }; });
    if (dsn.id !== "rd-item-lifecycle" || !dsn.approve || !dsn.send || dsn.opts) fail("itemsMock: the design's row is " + JSON.stringify(dsn));
    // A design is sent back with a note, so an empty box is refused with words, and nothing is sent.
    await p.click('.it-row[data-item="rd-item-lifecycle"] [data-act=ask]');
    await p.click('.it-row[data-item="rd-item-lifecycle"] [data-act=sendback]');
    await p.waitForFunction(() => /sent back with a note/.test(document.getElementById("items-note").textContent), null, { timeout: slow(5000) });
    if (await p.evaluate(() => itemsList.find(x => x.id === "rd-item-lifecycle").state) !== "blocked") fail("itemsMock: an empty send back went through");
    await p.fill('.it-answer-box[data-id="rd-item-lifecycle"]', "cover the failure case");
    await p.click('.it-row[data-item="rd-item-lifecycle"] [data-act=sendback]');
    await p.waitForFunction(() => { const it = itemsList.find(x => x.id === "rd-item-lifecycle"); return it && it.state === "running" && it.steps[0].state === "active"; }, null, { timeout: slow(10000) });
    if (!/Sent rd-item-lifecycle back/.test(await p.textContent("#items-note"))) fail("itemsMock: no notice after a send back");
    // THE WORKER'S QUESTION in the detail: its three suggested answers, the first orange, and a click answers.
    await p.reload({ waitUntil: "domcontentloaded" });
    await p.waitForFunction(() => { const t = document.querySelector('.tab[data-view="items"]'); return t && !t.hidden; }, null, { timeout: slow(15000) });
    await p.waitForTimeout(1000);
    await p.evaluate(() => switchView("items"));
    await p.waitForSelector('.it-row[data-item="f-ptyhost-reconnect"] .it-opt');
    const rowOpts = await p.$$eval('.it-row[data-item="f-ptyhost-reconnect"] .it-opt', e => e.map(b => b.textContent + (b.classList.contains("go") ? "*" : "")));
    if (rowOpts.join("|") !== "Fresh host, accept the loss*|Keep a stub pipe|Ask me later") fail("itemsMock: the row's suggested answers are " + rowOpts.join("|"));
    await p.click('.it-row[data-item="f-ptyhost-reconnect"] .it-title');
    await p.waitForSelector(".it-detail .it-qbox .it-opts .it-btn");
    const above = await p.evaluate(() => { const q = document.querySelector(".it-qbox"), t = document.querySelector(".it-tl");
      return { above: !!(q.compareDocumentPosition(t) & Node.DOCUMENT_POSITION_FOLLOWING), n: q.querySelectorAll(".it-opts .it-btn").length, first: q.querySelector(".it-opts .it-btn").classList.contains("go") }; });
    if (!above.above || above.n !== 3 || !above.first) fail("itemsMock: the question in the detail is " + JSON.stringify(above));
    await p.click('.it-qbox [data-act=option][data-i="1"]');
    await p.waitForFunction(() => { const it = itemsList.find(x => x.id === "f-ptyhost-reconnect"); return it && it.state === "running"; }, null, { timeout: slow(10000) });
    if (!/Sent the answer for f-ptyhost-reconnect/.test(await p.textContent("#items-note"))) fail("itemsMock: no notice after a button answer");
    // APPROVE from the detail.
    await p.click("[data-act=back]");
    await p.click('.it-row[data-item="rd-item-lifecycle"] .it-title');
    await p.waitForSelector(".it-detail .it-qbox [data-act=approve]");
    await p.click(".it-qbox [data-act=approve]");
    await p.waitForFunction(() => { const it = itemsList.find(x => x.id === "rd-item-lifecycle"); return it && it.state === "running" && it.steps[1].state === "done" && it.steps[2].state === "active"; }, null, { timeout: slow(10000) });
    if (!/Approved rd-item-lifecycle at e3a91c0/.test(await p.textContent("#items-note"))) fail("itemsMock: no notice after approving");
    if (errors.length) fail("itemsMock: page errors: " + errors.join(" | "));
  } finally { await ctx.close(); mock.hubMode = wasHub; }
  if (!bad) console.log("itemsMock ok");
}

// THE ITEMS ON THE PHONE (/m, `?itemsmock=1`): the door shows, the list is one column with the same rows, a blocked row carries its question and
// its suggested answers as full width buttons, an item's page has the question above the timeline, and an answer and an approval go through.
// m/items.css holds no raw colour. Nothing here touches a hub.
async function itemsPhoneSection(browser, base) {
  const css = fs.readFileSync(path.join(WEB_ROOT, "m", "items.css"), "utf8").replace(/\/\*[\s\S]*?\*\//g, "");
  const raw = (css.match(/#[0-9a-fA-F]{3,8}\b/g) || []).concat((css.match(/\b(?:rgba?|hsla?)\([^)]*\)/g) || []).filter(c => !/var\(/.test(c)));
  if (raw.length) fail("itemsPhone: m/items.css holds raw colour: " + raw.slice(0, 5).join(" "));
  const ctx = await browser.newContext({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true });
  const errors = [];
  try {
    const p = await ctx.newPage();
    p.on("pageerror", e => errors.push(e.message));
    await p.route(/\/_hub\/items|\/_hub\/work/, r => r.fulfill({ status: 500, body: "the mock must not reach the hub" }));
    await p.goto(base + "/m/?itemsmock=1", { waitUntil: "domcontentloaded" });
    await p.waitForFunction(() => { const b = document.getElementById("m-items-btn"); return b && !b.hidden; }, null, { timeout: slow(15000) });
    if (!/items\s*2/.test(await p.textContent("#m-items-btn"))) fail("itemsPhone: the door does not count the two blocked items: " + await p.textContent("#m-items-btn"));
    await p.click("#m-items-btn");
    await p.waitForSelector("#m-items .im-row", { timeout: slow(10000) });
    const list = await p.evaluate(() => {
      const rows = [...document.querySelectorAll("#m-items .im-row")];
      const lefts = new Set(rows.map(r => Math.round(r.getBoundingClientRect().left)));
      return { rows: rows.length, oneColumn: lefts.size === 1, wide: rows.every(r => r.getBoundingClientRect().width > 300), segs: rows[0].querySelector(".im-prog").children.length,
        stats: document.querySelectorAll("#m-items .im-stat").length, blocked: document.querySelectorAll("#m-items .im-row.st-blocked").length,
        asks: [...document.querySelectorAll("#m-items .im-ask .im-q")].map(e => e.textContent.slice(0, 20)),
        overflow: document.getElementById("m-items").scrollWidth > innerWidth + 1,
        firstBlocked: document.querySelector("#m-items .im-row.st-blocked").dataset.item,
        btns: [...document.querySelectorAll('#m-items .im-row[data-item="f-ptyhost-reconnect"] .im-opts .im-btn')].map(b => b.textContent + (b.classList.contains("go") ? "*" : "") + ":" + (b.getBoundingClientRect().width > 300)),
        nums: document.querySelector("#m-items .im-nums").textContent };
    });
    if (list.rows < 6 || !list.oneColumn || !list.wide || list.segs !== 8 || list.stats !== 4 || list.blocked !== 2 || list.asks.length !== 2 || list.overflow)
      fail("itemsPhone: the list is " + JSON.stringify(list));
    // The oldest question is first, and its suggested answers are full width buttons, the first orange, then a way to type another.
    if (list.firstBlocked !== "f-ptyhost-reconnect" || list.btns.join("|") !== "Fresh host, accept the loss*:true|Keep a stub pipe:true|Ask me later:true|other:true")
      fail("itemsPhone: the blocked row is " + JSON.stringify(list));
    if (!/tokens/.test(list.nums) || !/\$/.test(list.nums)) fail("itemsPhone: the numbers line is " + list.nums);
    // THE PAGE: the question above the timeline, eight steps, the same buttons.
    await p.click('#m-items .im-row[data-item="f-ptyhost-reconnect"] .im-main');
    await p.waitForSelector("#m-items .im-page .im-step");
    const page = await p.evaluate(() => { const q = document.querySelector("#m-items .im-qbox"), t = document.querySelector("#m-items .im-tl");
      return { above: !!(q.compareDocumentPosition(t) & Node.DOCUMENT_POSITION_FOLLOWING), steps: t.children.length, opts: q.querySelectorAll(".im-opts .im-btn").length,
        title: document.getElementById("m-items").querySelector(".im-title").textContent, box: !!q.querySelector(".im-box") }; });
    if (!page.above || page.steps !== 8 || page.opts !== 3 || !page.box || page.title !== "f-ptyhost-reconnect") fail("itemsPhone: the page is " + JSON.stringify(page));
    // A BUTTON ANSWER is sent, with a notice that does not claim the item is running yet.
    await p.click("#m-items .im-qbox .im-opts .im-btn >> nth=1");
    await p.waitForFunction(() => /Sent the answer for f-ptyhost-reconnect/.test(document.querySelector("#m-items .im-note").textContent), null, { timeout: slow(10000) });
    await p.waitForFunction(() => !document.querySelector("#m-items .im-qbox"), null, { timeout: slow(10000) });
    // Back is history: the list again, with one blocked row left.
    await p.click("#m-items .im-back");
    await p.waitForSelector("#m-items .im-list");
    // The design: approve and send back, approve is orange, send back opens a note box and refuses an empty note.
    const d = await p.evaluate(() => { const r = document.querySelector('#m-items .im-row[data-item="rd-item-lifecycle"]');
      return [...r.querySelectorAll(".im-opts .im-btn")].map(b => b.textContent + (b.classList.contains("go") ? "*" : "")).join("|"); });
    if (d !== "approve*|send back") fail("itemsPhone: the design's buttons are " + d);
    await p.click('#m-items .im-row[data-item="rd-item-lifecycle"] .im-opts .im-btn >> nth=1');
    await p.click('#m-items .im-row[data-item="rd-item-lifecycle"] .im-ans .im-btn');
    await p.waitForFunction(() => /sent back with a note/.test(document.querySelector("#m-items .im-note").textContent), null, { timeout: slow(5000) });
    await p.click('#m-items .im-row[data-item="rd-item-lifecycle"] .im-opts .im-btn >> nth=0');
    await p.waitForFunction(() => { const r = document.querySelector('#m-items .im-row[data-item="rd-item-lifecycle"]'); return r && !r.classList.contains("st-blocked"); }, null, { timeout: slow(10000) });
    if (!/Approved rd-item-lifecycle at e3a91c0/.test(await p.textContent("#m-items .im-note"))) fail("itemsPhone: no notice after approving: " + await p.textContent("#m-items .im-note"));
    if (errors.length) fail("itemsPhone: page errors: " + errors.join(" | "));
  } finally { await ctx.close(); }
  if (!bad) console.log("itemsPhone ok");
}

// THE ITEMS TAB UX PASS (docs/ui/items-tab-ux-pass.md), on rows shaped like the live hub (docs/ui/img/items-tab-ux-pass/fixture.js): one row per backlog id,
// no "(no reason given)", the header equal to the rows that need you, named steps and a legend, idle and offline cards, no empty spend, and the
// layout at 2338 and 1280 wide. The current segment must read apart from a done one in every skin.
async function itemsUxSection(browser, base) {
  const fx = require("../../docs/ui/img/items-tab-ux-pass/fixture.js");
  const { SKINS } = require("../lib/mock.js");
  const wasHub = mock.hubMode;
  mock.hubMode = true;
  const errors = [];
  const open = async w => {
    const ctx = await browser.newContext({ viewport: { width: w, height: 1400 } });
    const p = await ctx.newPage();
    p.on("pageerror", e => errors.push(e.message));
    await p.route(/\/_hub\/work(\/[^/?]+)?(\?.*)?$/, r => r.fulfill({ contentType: "application/json", body: JSON.stringify({ work: fx.work }) }));
    await p.route(/\/_hub\/items(\/[^/?]+)?(\?.*)?$/, r => r.fulfill({ contentType: "application/json", body: JSON.stringify({ items: fx.rows }) }));
    await p.route(/\/v1\/tasks(\?.*)?$/, r => r.fulfill({ contentType: "application/json", body: JSON.stringify(fx.tasks) }));
    await p.goto(base + "/", { waitUntil: "domcontentloaded" });
    await p.waitForFunction(() => { const t = document.querySelector('.tab[data-view="items"]'); return t && !t.hidden; }, null, { timeout: slow(15000) });
    await p.click('.tab[data-view="items"]');
    await p.waitForSelector(".it-row", { timeout: slow(10000) });
    await p.evaluate(t => { lastTasks = t; itemsHtml = ""; itemsPaint(); }, fx.tasks);
    return { ctx, p };
  };
  try {
    const { ctx, p } = await open(2338);
    const got = await p.evaluate(() => {
      const rows = [...document.querySelectorAll(".it-row")];
      const grp = g => [...document.querySelectorAll('.it-group[data-state="' + g + '"] .it-row')].map(r => r.dataset.item);
      const item = b => itemsList.find(x => x.backlog === b);
      const handoff = item("f-room-handoff");
      return { n: rows.length, ids: new Set(rows.map(r => r.querySelector(".it-id").textContent)).size, text: document.getElementById("items-list").textContent,
        needsRows: grp("needs-you").length, chip: document.querySelector('[data-stat="needs"] b').textContent,
        stepLine: rows.map(r => r.querySelector(".it-cur b").textContent), legend: document.querySelector(".it-lgd").textContent,
        g: Object.fromEntries(["bug-lean-no-status-line", "rd-test-layout", "u-items-tab", "r-usage-export", "f-room-handoff", "f-hub-forge-push", "f-session-ask", "d-readme-links"]
          .map(b => [b, item(b).fold.group])),
        offlineNote: document.querySelector('.it-row[data-item="' + handoff.id + '"] .it-note').textContent,
        retired: document.querySelectorAll(".it-tok, .it-usd, .it-age, .it-stat, .it-sort").length,
        cols: getComputedStyle(document.querySelector(".it-cols")).gridTemplateColumns.split(" ").length, scrollX: document.documentElement.scrollWidth > innerWidth,
        wrap: document.querySelector(".it-wrap").getBoundingClientRect().width };
    });
    const backlogs = new Set(fx.rows.map(r => r.work_id)).size;
    if (got.n !== backlogs || got.ids !== backlogs) fail("itemsUx: " + fx.rows.length + " assignments for " + backlogs + " backlog ids drew " + got.n + " rows");
    const ph = got.text.match(/no reason given|no usage yet|none yet/);
    if (ph) fail("itemsUx: the list still says a placeholder: " + ph[0]);
    if (got.needsRows !== +got.chip || got.needsRows !== 2) fail("itemsUx: the header says " + got.chip + " and " + got.needsRows + " rows need you, want 2 and 2");
    if (got.g["bug-lean-no-status-line"] !== "working" || got.g["rd-test-layout"] !== "idle") fail("itemsUx: groups " + JSON.stringify(got.g));
    if (got.g["u-items-tab"] !== "working" || got.g["r-usage-export"] !== "working" || got.g["d-readme-links"] !== "finished") fail("itemsUx: a landed item is " + JSON.stringify(got.g));
    if (got.g["f-hub-forge-push"] !== "queued" || got.g["f-room-handoff"] !== "working" || got.g["f-session-ask"] !== "working") fail("itemsUx: groups " + JSON.stringify(got.g));
    if (!/room m1mini offline/.test(got.offlineNote)) fail("itemsUx: an offline card reads " + got.offlineNote);
    if (!got.stepLine.some(l => /^step 3 of 8 · building/.test(l)) || !got.stepLine.some(l => /idle at /.test(l)) || !got.stepLine.some(l => /waiting for your approval/.test(l))) fail("itemsUx: step lines " + got.stepLine.join(" | "));
    if (!/stale step/.test(got.legend) || !/idle: nothing running/.test(got.legend) || !/current step/.test(got.legend)) fail("itemsUx: the legend reads " + got.legend);
    if (got.retired) fail("itemsUx: " + got.retired + " retired list elements are still drawn");
    if (got.cols !== 2 || got.scrollX || got.wrap > 1840) fail("itemsUx: at 2338 the columns are " + got.cols + ", scrolls sideways " + got.scrollX + ", width " + got.wrap);
    // An idle row offers cancel and a disabled relaunch, and an offline card has no relaunch.
    const acts = await p.evaluate(() => ({
      idle: [...document.querySelectorAll('.it-group[data-state="idle"] .it-row')].map(r => [...r.querySelectorAll(".it-btn")].map(b => b.textContent + (b.disabled ? "!" : "")).join()),
      off: [...document.querySelectorAll(".it-row")].filter(r => r.querySelector(".it-note") && /offline/.test(r.querySelector(".it-note").textContent)).map(r => r.querySelectorAll(".it-btn").length) }));
    if (acts.idle.some(a => a !== "cancel item,relaunch!") || acts.off.some(n => n)) fail("itemsUx: idle actions " + JSON.stringify(acts));
    // THE CURRENT SEGMENT reads apart from a done one in every skin, and is underlined.
    for (const skin of SKINS) {
      await p.evaluate(s => document.documentElement.setAttribute("data-skin", s), skin);
      const c = await p.evaluate(() => {
        const g = e => getComputedStyle(e), cur = document.querySelector(".it-prog span.c:not(.w):not(.s):not(.i)"), d = document.querySelector(".it-prog span.d");
        return { cur: g(cur).backgroundColor, done: g(d).backgroundColor, shadow: g(cur).boxShadow };
      });
      if (c.cur === c.done || !c.shadow || c.shadow === "none") fail("itemsUx: in " + skin + " the current segment is " + JSON.stringify(c));
    }
    await p.evaluate(() => document.documentElement.removeAttribute("data-skin"));
    // A poll that changed nothing repaints nothing.
    await p.evaluate(() => { document.querySelector(".it-row").dataset.mark = "1"; return loadItems(); });
    if (!await p.evaluate(() => !!document.querySelector('.it-row[data-mark="1"]'))) fail("itemsUx: a poll with no change repainted the list");
    await ctx.close();
    const n = await open(1280);
    const narrow = await n.p.evaluate(() => ({ cols: getComputedStyle(document.querySelector(".it-cols")).gridTemplateColumns, scrollX: document.documentElement.scrollWidth > innerWidth,
      order: [...document.querySelectorAll(".it-group")].map(g => g.dataset.state).join() }));
    if (narrow.cols.trim().split(" ").length > 1) fail("itemsUx: at 1280 there are columns: " + narrow.cols);
    if (narrow.scrollX) fail("itemsUx: at 1280 the page scrolls sideways");
    if (narrow.order !== "needs-you,idle,working,queued,finished") fail("itemsUx: the group order is " + narrow.order);
    await n.ctx.close();
    if (errors.length) fail("itemsUx: page errors: " + errors.join(" | "));
  } finally { mock.hubMode = wasHub; }
  if (!bad) console.log("itemsUx ok");
}

const units = [
  { name: "readyOnce", run: ({ browser, base }) => readyOnceSection(browser, base) },
  { name: "readyPopout", run: ({ browser, base }) => readyPopoutSection(browser, base) },
  { name: "readyTwoWindows", run: ({ browser, base }) => readyTwoWindowsSection(browser, base) },
  { name: "itemsRealShape", run: ({ browser, base }) => itemsRealShapeSection(browser, base) },
  { name: "itemsView", run: ({ browser, base }) => itemsViewSection(browser, base) },
  { name: "itemsMock", run: ({ browser, base }) => itemsMockSection(browser, base) },
  { name: "itemsEfficacy", run: ({ browser, base }) => itemsEfficacySection(browser, base) },
  { name: "itemsUx", run: ({ browser, base }) => itemsUxSection(browser, base) },
  { name: "itemsPhone", run: ({ browser, base }) => itemsPhoneSection(browser, base) },
  { name: "interviewTop", run: ({ browser, base }) => interviewTopSection(browser, base) },
  { name: "itemMinionChip", run: ({ browser, base }) => itemMinionChipSection(browser, base) },
  { name: "itemDocsLanded", run: ({ browser, base }) => itemDocsLandedSection(browser, base) },
  { name: "itemOpenHiddenCard", run: ({ browser, base }) => itemOpenHiddenCardSection(browser, base) },
  { name: "kidCounts", run: ({ browser, base }) => kidCountsSection(browser, base) },
  { name: "unifiedCard", run: ({ browser, base }) => unifiedCardSection(browser, base) },
  { name: "noReadyChildren", run: ({ browser, base }) => noReadyChildrenSection(browser, base) },
  { name: "readyNoSeed", run: ({ browser, base }) => readyNoSeedSection(browser, base) },
];
const sections = { readyOnce: readyOnceSection, readyPopout: readyPopoutSection, readyTwoWindows: readyTwoWindowsSection, interviewTop: interviewTopSection, itemMinionChip: itemMinionChipSection, itemDocsLanded: itemDocsLandedSection, itemOpenHiddenCard: itemOpenHiddenCardSection, noReadyChildren: noReadyChildrenSection, readyNoSeed: readyNoSeedSection };

module.exports = { units, sections };
if (require.main === module) require("../lib/alone.js")(units);
