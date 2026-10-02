// Times a terminal switch on a running board, end to end. One file, no repo needed: only playwright (and its
// chromium) must resolve from where it runs.
//
//   node measure-term-switch.js [boardURL] [--per-host N] [--rounds N] [--ids a,b,c]
//
// boardURL defaults to http://127.0.0.1:7781 (the m1mini room). On sg4 the hub's board is
// http://127.0.0.1:7778. Cards are grouped by their hostname field (a hub lists every room's cards) and the first
// --per-host (default 2) live cards of each group are used, so one run covers the hub-local room and the remote
// rooms. --ids picks the cards by id instead.
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
//   ms are rounded. One table is printed at the end. "last" and "parsed" include live output, so a card that is working
//   right now (it streams all the time) shows seconds there: read first, c>o and the idle cards for the switch itself.
let pw;
try { pw = require("playwright"); } catch (e) { pw = require("playwright-core"); }

const argv = process.argv.slice(2);
const opt = (name, dflt) => { const i = argv.indexOf(name); return i >= 0 ? argv[i + 1] : dflt; };
const base = argv.find((a, i) => /^https?:/.test(a) && !/^--/.test(argv[i - 1] || "")) || "http://127.0.0.1:7781";
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

  const all = await page.evaluate(() => fetch("/v1/tasks").then(r => r.json()).then(j =>
    j.tasks.filter(t => t.status !== "done" && t.status !== "backlog")
      .map(t => ({ id: t.id, title: t.title, host: t.hostname || "?" }))));
  let cards;
  if (idsArg) cards = idsArg.split(",").map(id => all.find(c => c.id === id) || { id, title: id, host: "?" });
  else {
    const seen = {}; cards = [];
    for (const c of all) { seen[c.host] = (seen[c.host] || 0) + 1; if (seen[c.host] <= perHost) cards.push(c); }
  }
  if (cards.length < 2) { console.error("need at least two live cards, found", cards.length); process.exit(1); }
  const label = c => `${c.host}:${(c.title || c.id).replace(/^.*\//, "").slice(0, 22)}`;

  // One switch. `term` and `attachTask` are the board's own globals.
  async function sw(id) {
    return page.evaluate(async id => {
      const before = window.__ws.length;
      const t0 = performance.now();
      attachTask(id);
      let parsed = 0, hooked = null;
      const hook = setInterval(() => {
        if (typeof term !== "undefined" && term && term !== hooked) {
          hooked = term; term.onWriteParsed(() => { parsed = performance.now(); });
        }
      }, 1);
      for (let i = 0; i < 600; i++) {
        await new Promise(r => setTimeout(r, 10));
        const ws = window.__ws[before];
        if (ws && ws.last && performance.now() - ws.last > 200) break;
        if (i > 300 && !(ws && ws.open)) break;
      }
      clearInterval(hook);
      const ws = window.__ws[before];
      if (!ws) return { err: "no ws created" };
      return { ctor: ws.t0 - t0, co: ws.open ? ws.open - ws.t0 : -1, first: ws.first ? ws.first - t0 : -1,
        last: ws.last ? ws.last - t0 : -1, parsed: parsed ? parsed - t0 : -1, kb: ws.bytes / 1024,
        url: ws.url.replace(/^wss?:\/\/[^/]+/, "").replace(/\?.*/, "") };
    }, id);
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
  console.log(`board ${base}   ${cards.length} cards: ${cards.map(label).join(", ")}`);
  console.log(["kind", "card", "ctor", "c>o", "first", "last", "parsed", "KB"].map((h, i) => pad(h, [7, 30, 6, 6, 6, 6, 7, 6][i])).join(""));
  for (const r of rows) {
    if (r.err) { console.log(pad(r.kind, 7) + pad(r.card, 30) + r.err); continue; }
    console.log(pad(r.kind, 7) + pad(r.card, 30) + [n(r.ctor), n(r.co), n(r.first), n(r.last), n(r.parsed), n(r.kb)]
      .map((v, i) => pad(v, [6, 6, 6, 6, 7, 6][i])).join(""));
  }
  const ok = rows.filter(r => !r.err);
  const med = a => { a = a.filter(v => v >= 0).sort((x, y) => x - y); return a.length ? a[a.length >> 1] : -1; };
  for (const k of ["first", "repeat", "rapid"]) {
    const g = ok.filter(r => r.kind === k);
    console.log(`median ${pad(k, 7)} ctor ${n(med(g.map(r => r.ctor)))}  c>o ${n(med(g.map(r => r.co)))}  parsed ${n(med(g.map(r => r.parsed)))}`);
  }
  const co = ok.filter(r => r.kind === "rapid").map(r => r.co);
  console.log(`rapid c>o in order: ${co.map(n).join(" ")}`);
  console.log(`ws path example: ${(ok[0] || {}).url}`);
})().catch(e => { console.error(e); process.exit(1); });
