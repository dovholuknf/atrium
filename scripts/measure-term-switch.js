// Times a terminal switch on a running board, end to end. One file, no repo needed: only playwright (and its
// chromium) must resolve from where it runs.
//
//   node measure-term-switch.js [boardURL] [--per-host N] [--rounds N] [--ids a,b,c] [--hover]
//   node measure-term-switch.js [boardURL] --pool [--pool-room HOST] [--pool-k K] [--hover]
//
// boardURL defaults to http://127.0.0.1:7781 (the m1mini room). On sg4 the hub's board is
// http://127.0.0.1:7778. Cards are grouped by their hostname field (a hub lists every room's cards) and the first
// --per-host (default 2) live cards of each group are used, so one run covers the hub-local room and the remote
// rooms. --ids picks the cards by id instead.
//
// --hover switches the way a mouse does: the pointer rests 150 ms on the card's row in the terminals list, then the
// row is clicked (the click goes through the row's own onclick, so what is timed is a real row click, not a call to
// attachTask). The row is found by CARD ID: `#term-list .card.tab[data-id="<id>"]`, the id as the list shows it
// (a hub's `room~uuid` when it has more than one room). A card with no such row (cold, hidden, filtered out) is
// reported as "no row" and skipped. Without --hover, attachTask is called directly: no rest, so no prefetch, the way
// the keyboard and the switcher switch. The table is labelled with the mode.
//
// --pool measures what keeping many terminals on ONE remote room does to the next attach. The hub holds 4 warm idle
// connections per room, and every kept terminal's ws takes one for as long as it is kept, so past 4 the room has to
// dial a fresh one (the hub waits up to 10 s for it). --pool-room HOST picks the room by the hostname its cards carry
// (default: the first host that is not the first host listed, so on a hub the first REMOTE room; with one host, that
// one). --pool-k K (default 8) cards of it are attached one after another, the board's "terminals kept alive" setting
// raised to hold them (the gear's own setting, at most the ceiling of 12), then a (K+1)th card of the room is timed.
// Printed: ctor, c>o (where the hub's dial for a connection sits), first, for the first attach, the Kth and the
// (K+1)th, the number of ws this tab holds open, and a verdict line. A room with fewer than K+1 live cards is said
// so and nothing is timed. The hub's idle pool count is not exposed to the board, so it is not printed.
//
// Per card: FIRST switch to it, a switch to a different card, then REPEAT switch to the first (a hub caches the card
// lookup for two minutes, so the repeat skips it). Then a RAPID run: every picked card, --rounds times (default 3),
// no pause between switches. Timers are ms from the call that starts the switch (attachTask):
//   ctor    the ws is created (the fetch of the card before it)
//   open    ws open, from ctor ("c>o": the gap the hub proxy and its connection pool sit in)
//   first   first byte of the replay, from the click
//   last    last byte of the replay, from the click
//   parsed  xterm finished parsing the last of it, from the click
//   KB      replay size
//   keep    a switch BACK to a kept (hidden, still attached) terminal opens no ws, so ctor, c>o, first, last and parsed
//           show "-" for it, and the row is timed instead by "keep" (ms from the call to the board's keep-end mark,
//           the show itself) and "2fr" (ms to two animation frames after the call: what the eye sees). The path table
//           says "kept" for these. A board that predates keep-alive prints "no ws" for a switch back, as before.
//   ms are rounded. One table is printed at the end. "last" and "parsed" include live output, so a card that is working
//   right now (it streams all the time) shows seconds there: read first, c>o and the idle cards for the switch itself.
let pw;
try { pw = require("playwright"); } catch (e) { pw = require("playwright-core"); }

const argv = process.argv.slice(2);
const opt = (name, dflt) => { const i = argv.indexOf(name); return i >= 0 ? argv[i + 1] : dflt; };
const base = argv.find((a, i) => /^https?:/.test(a) && !/^--/.test(argv[i - 1] || "")) || "http://127.0.0.1:7781";
const hover = argv.includes("--hover");
const pool = argv.includes("--pool"), poolRoom = opt("--pool-room", ""), poolK = +opt("--pool-k", 8);
const perHost = +opt("--per-host", 2), rounds = +opt("--rounds", 3), idsArg = opt("--ids", "");

(async () => {
  const browser = await pw.chromium.launch();
  const page = await (await browser.newContext({ viewport: { width: 1500, height: 900 } })).newPage();
  await page.addInitScript(() => {
    window.__ws = [];
    const W = window.WebSocket;
    window.WebSocket = new Proxy(W, { construct(T, a) {
      const s = new T(...a);
      const r = { url: a[0], t0: performance.now(), open: 0, first: 0, last: 0, bytes: 0 };
      r.sock = s;
      window.__ws.push(r);
      s.addEventListener("open", () => { r.open = performance.now(); });
      s.addEventListener("message", e => {
        const n = performance.now(); if (!r.first) r.first = n; r.last = n;
        r.bytes += e.data.byteLength != null ? e.data.byteLength : e.data.length;
      });
      return s;
    } });
  });
  await page.goto(base + "/", { waitUntil: "load" });
  await page.waitForTimeout(3000);
  if (hover) { await page.click('.tab[data-view="terms"]'); await page.waitForTimeout(1500); }

  const all = await page.evaluate(() => fetch("/v1/tasks").then(r => r.json()).then(j =>
    j.tasks.filter(t => t.status !== "done" && t.status !== "backlog")
      .map(t => ({ id: t.id, title: t.title, host: t.hostname || "?" }))));
  let cards;
  if (idsArg) cards = idsArg.split(",").map(id => all.find(c => c.id === id) || { id, title: id, host: "?" });
  else {
    const seen = {}; cards = [];
    for (const c of all) { seen[c.host] = (seen[c.host] || 0) + 1; if (seen[c.host] <= perHost) cards.push(c); }
  }
  if (pool) {
    const hosts = [...new Set(all.map(c => c.host))];
    const room = poolRoom || hosts[hosts.length > 1 ? 1 : 0];
    const live = all.filter(c => c.host === room);
    const lab = c => `${c.host}:${(c.title || c.id).replace(/^.*\//, "").slice(0, 22)}`;
    console.log(`board ${base}   rooms: ${hosts.join(", ")}   pool room: ${room} (${live.length} live cards)`);
    if (live.length < poolK + 1) {
      console.log(`room ${room} has only ${live.length} live cards; --pool needs ${poolK + 1} (K=${poolK} kept, plus one more to time). Nothing timed.`);
      await browser.close();
      return;
    }
    const keepN = await page.evaluate(k => typeof setTermKeep === "function" ? (setTermKeep("atrium.termKeep", k), termKeepN()) : -1, poolK);
    if (keepN < 0) {
      console.log("this board predates keep-alive (no terminals-kept-alive setting), so nothing would stay attached. Nothing timed.");
      await browser.close();
      return;
    }
    if (keepN < poolK) console.log(`note: the board holds at most ${keepN} kept terminals (ceiling), so fewer than ${poolK} stay attached`);
    const times = [];
    for (let i = 0; i <= poolK; i++) {
      const c = live[i], r = await sw(c.id);
      times.push({ i: i + 1, card: lab(c), ...r });
      await page.waitForTimeout(300);
    }
    const held = await page.evaluate(() => window.__ws.filter(r => r.sock.readyState === 1).length);
    const n = v => v == null || v < 0 ? "-" : String(Math.round(v));
    const pad = (s, w) => String(s).padEnd(w);
    console.log(["#", "card", "ctor", "c>o", "first", "parsed", "KB"].map((h, i) => pad(h, [4, 30, 6, 6, 6, 7, 6][i])).join(""));
    for (const t of times) {
      if (t.err) { console.log(pad(t.i, 4) + pad(t.card, 30) + t.err); continue; }
      console.log(pad(t.i, 4) + pad(t.card, 30) + [n(t.ctor), n(t.co), n(t.first), n(t.parsed), n(t.kb)].map((v, i) => pad(v, [6, 6, 6, 7, 6][i])).join(""));
    }
    const a = times[0], z = times[poolK];
    console.log(`ws this tab holds open: ${held}`);
    if (a && z && !a.err && !z.err) {
      console.log(`attach 1 -> ${poolK + 1}: c>o ${n(a.co)} -> ${n(z.co)} ms, first ${n(a.first)} -> ${n(z.first)} ms (the hub's wait for a connection is in c>o)`);
      console.log(z.co > a.co + 150 || z.first > a.first + 300 ? `SLOWER: attach ${poolK + 1} on ${room} took clearly longer than attach 1 (past the hub's 4 idle conns?)` : `no slowdown seen for attach ${poolK + 1}`);
    }
    await browser.close();
    return;
  }
  if (cards.length < 2) { console.error("need at least two live cards, found", cards.length); process.exit(1); }
  const label = c => `${c.host}:${(c.title || c.id).replace(/^.*\//, "").slice(0, 22)}`;

  // One switch. `term` and `attachTask` are the board's own globals.
  async function sw(id) {
    if (hover) {
      const row = page.locator(`#term-list .card.tab[data-id="${id}"]`);
      if (!(await row.count())) return { err: "no row" };
      await row.hover();
      await page.waitForTimeout(150);
    }
    const res = await page.evaluate(async ({ id, hover }) => {
      const before = window.__ws.length;
      const t0 = performance.now();
      if (hover) document.querySelector(`#term-list .card.tab[data-id="${CSS.escape(id)}"]`).click(); else attachTask(id);
      let parsed = 0, hooked = null;
      const hook = setInterval(() => {
        if (typeof term !== "undefined" && term && term !== hooked) {
          hooked = term; term.onWriteParsed(() => { parsed = performance.now(); });
        }
      }, 1);
      for (let i = 0; i < 600; i++) {
        await new Promise(r => setTimeout(r, 10));
        const ws = window.__ws[before];
        // A kept terminal is shown with no ws at all: done once the board says the show ended.
        if (!ws && (window.__switchMarks || []).some(e => e[0] === "keep-end")) break;
        if (ws && ws.last && performance.now() - ws.last > 200) break;
        if (i > 300 && !(ws && ws.open)) break;
      }
      clearInterval(hook);
      const ws = window.__ws[before];
      if (!ws && (window.__switchMarks || []).some(e => e[0] === "keep-show")) {
        await new Promise(r => requestAnimationFrame(() => requestAnimationFrame(r)));
        const f = performance.now() - t0, m = window.__switchMarks;
        const at = n => { const x = m.find(e => e[0] === n); return x ? x[1] : -1; };
        return { kept: true, ctor: -1, co: -1, first: -1, last: -1, parsed: -1, kb: -1, keep: at("keep-end"), frames: f, marks: m };
      }
      if (!ws) return { err: "no ws (already attached, or the click did nothing)" };
      return { ctor: ws.t0 - t0, co: ws.open ? ws.open - ws.t0 : -1, first: ws.first ? ws.first - t0 : -1,
        last: ws.last ? ws.last - t0 : -1, parsed: parsed ? parsed - t0 : -1, kb: ws.bytes / 1024,
        url: ws.url.replace(/^wss?:\/\/[^/]+/, "").replace(/\?.*/, ""),
        marks: window.__switchMarks || null };
    }, { id, hover });
    if (hover) await page.mouse.move(2, 2);
    return res;
  }
  const rows = [];
  const rec = async (kind, c) => { const r = await sw(c.id); rows.push({ kind, card: label(c), ...r }); await page.waitForTimeout(250); };

  for (let i = 0; i < cards.length; i++) {
    const c = cards[i], other = cards[(i + 1) % cards.length];
    await rec("first", c);
    await rec("away", other);
    await rec("repeat", c);
  }
  // Rapid: no pause at all between switches.
  for (let r = 0; r < rounds; r++) for (const c of cards) {
    const x = await sw(c.id); rows.push({ kind: "rapid", card: label(c), ...x });
  }
  await browser.close();

  const n = v => v == null || v < 0 ? "-" : String(Math.round(v));
  const pad = (s, w) => String(s).padEnd(w);
  console.log(`mode ${hover ? "HOVER 150ms then row click" : "attachTask, no hover"}`);
  console.log(`board ${base}   ${cards.length} cards: ${cards.map(label).join(", ")}`);
  console.log(["kind", "card", "ctor", "c>o", "first", "last", "parsed", "KB", "keep", "2fr"].map((h, i) => pad(h, [7, 30, 6, 6, 6, 6, 7, 6, 6, 5][i])).join(""));
  for (const r of rows) {
    if (r.err) { console.log(pad(r.kind, 7) + pad(r.card, 30) + r.err); continue; }
    console.log(pad(r.kind, 7) + pad(r.card, 30) + [n(r.ctor), n(r.co), n(r.first), n(r.last), n(r.parsed), n(r.kb), n(r.keep), n(r.frames)]
      .map((v, i) => pad(v, [6, 6, 6, 6, 7, 6, 6, 5][i])).join(""));
  }
  // WHERE THE ctor TIME GOES, from the board's own marks (`swMark`, null on a board that predates them): ms from the
  // click to each point, and the PATH the switch took: list (the card was in the board's list), list+prewarm (and a
  // hover's fetch was taken), prewarm / get (the card was not listed, so the read was waited for), kept (a hidden
  // terminal was shown).
  const ms = (m, name) => { const x = (m || []).find(e => e[0] === name || e[0].startsWith(name + ":")); return x ? x[1] : -1; };
  const pathOf = m => { const c = (m || []).find(e => e[0].startsWith("card:")); return (m || []).some(e => e[0] === "keep-show") ? "kept" : c ? c[0].slice(5) : "-"; };
  console.log("");
  console.log(["kind", "card", "path", "openTerm", "xterm", "webgl", "raf", "fit", "ws-new", "kept-end"].map((h, i) => pad(h, [7, 30, 14, 9, 7, 7, 6, 6, 7, 8][i])).join(""));
  for (const r of rows) {
    if (r.err) continue;
    const m = r.marks;
    console.log(pad(r.kind, 7) + pad(r.card, 30) + pad(pathOf(m), 14) +
      ["openTerm", "xterm", "webgl", "raf", "fit", "ws-new", "keep-end"].map((k, i) => pad(n(ms(m, k)), [9, 7, 7, 6, 6, 7, 8][i])).join(""));
  }
  console.log("");
  const ok = rows.filter(r => !r.err);
  const med = a => { a = a.filter(v => v >= 0).sort((x, y) => x - y); return a.length ? a[a.length >> 1] : -1; };
  for (const k of ["first", "repeat", "rapid"]) {
    const g = ok.filter(r => r.kind === k && !r.kept);
    console.log(`median ${pad(k, 7)} ctor ${n(med(g.map(r => r.ctor)))}  c>o ${n(med(g.map(r => r.co)))}  parsed ${n(med(g.map(r => r.parsed)))}`);
  }
  const kg = ok.filter(r => r.kept);
  console.log(kg.length ? `median kept   n ${kg.length}  keep-end ${n(med(kg.map(r => r.keep)))}  two frames ${n(med(kg.map(r => r.frames)))}` : "median kept   none (no switch went to a kept terminal)");
  const co = ok.filter(r => r.kind === "rapid").map(r => r.co);
  console.log(`rapid c>o in order: ${co.map(n).join(" ")}`);
  console.log(`ws path example: ${(ok[0] || {}).url}`);
})().catch(e => { console.error(e); process.exit(1); });
