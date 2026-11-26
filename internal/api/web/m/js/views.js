// The Usage and Queue sheets on the phone: the desktop tabs of the same names, one column. Usage reads the limits the room keeps
// (GET /v1/usage/limits) and what cards report now, Queue is the list the home screen calls "needs you", one row per ask.
(function () {
  "use strict";
  const U = window.mUtil;
  const HOUR = 3600000;
  const WINDOW = { five_hour: 5 * HOUR, weekly: 7 * 24 * HOUR };
  const LABEL = { five_hour: "5 hour", weekly: "week" };
  const VERB = { permission: "APPROVE", questions: "ANSWER", report: "SIGN", held: "HELD", unseen: "READ", ready: "READY" };
  const sheets = {};

  function el(tag, cls, text) {
    const n = document.createElement(tag);
    if (cls) n.className = cls;
    if (text != null) n.textContent = text;
    return n;
  }

  function build(name, title) {
    if (sheets[name]) return sheets[name];
    const sheet = el("section", "imsheet");
    sheet.id = "m-" + name; sheet.hidden = true; sheet.setAttribute("aria-label", name);
    try { const v = parseFloat(localStorage.getItem("atrium.mfs")); if (v) sheet.style.setProperty("--m-fs", v + "px"); } catch (e) { /* the default size */ }
    const top = el("div", "im-top");
    const back = el("button", "im-back"); back.type = "button"; back.setAttribute("aria-label", "back");
    back.innerHTML = '<svg viewBox="0 0 24 24" width="22" height="22" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M15 5l-7 7 7 7"/></svg>';
    top.append(back, el("span", "im-title", title));
    const scroll = el("div", "im-scroll"), body = el("div", "im-body");
    scroll.appendChild(body);
    sheet.append(top, scroll);
    document.body.appendChild(sheet);
    back.addEventListener("click", () => close(name));
    return (sheets[name] = { sheet, body, draw: null });
  }

  function open(name, title, draw) {
    const s = build(name, title);
    s.draw = draw;
    try { history.pushState({ mview: name }, "", location.href); } catch (e) { /* history is a nicety */ }
    s.sheet.hidden = false;
    document.body.classList.add("im-open");
    draw(s.body);
  }
  function hide(name) {
    const s = sheets[name];
    if (!s || s.sheet.hidden) return;
    s.sheet.hidden = true; s.draw = null; s.body.textContent = "";
    document.body.classList.remove("im-open");
  }
  function close(name) {
    const s = sheets[name];
    if (!s || s.sheet.hidden) return;
    if (history.state && history.state.mview === name) history.back(); else hide(name);
  }
  window.addEventListener("popstate", e => {
    for (const n of Object.keys(sheets)) if (!e.state || e.state.mview !== n) hide(n);
  });
  function repaint(name) {
    const s = sheets[name];
    if (s && !s.sheet.hidden && s.draw) s.draw(s.body);
  }

  // ---- usage ------------------------------------------------------------------------------------------------------
  const readings = [];
  let fetched = 0;

  async function loadReadings() {
    if (Date.now() - fetched < 60000) return;
    fetched = Date.now();
    try {
      const r = await window.mNet.api("/v1/usage/limits?since=" + encodeURIComponent(new Date(Date.now() - WINDOW.weekly).toISOString()));
      readings.length = 0;
      for (const x of (r && r.readings) || []) {
        if (x.kind in WINDOW && typeof x.pct === "number") readings.push({ at: U.ts(x.at), kind: x.kind, pct: x.pct, reset: U.ts(x.resets_at) });
      }
    } catch (e) { /* live telemetry only */ }
    repaint("usage");
  }

  function groups(now) {
    const by = new Map();
    const add = (kind, pct, reset, at) => {
      if (reset && reset < now) return;
      const k = kind + "|" + (reset ? Math.round(reset / 60000) : 0);
      let g = by.get(k);
      if (!g) by.set(k, g = { kind, reset, pct: 0, pts: [] });
      g.pct = Math.max(g.pct, pct);
      g.pts.push({ at, pct });
    };
    for (const x of readings) if (now - x.at <= HOUR * 24 * 7) add(x.kind, x.pct, x.reset, x.at);
    for (const t of window.mStore.cards()) {
      const c = t.telemetry;
      if (!c) continue;
      const at = now - (Number(c.seconds) || 0) * 1000;
      for (const [kind, key] of [["five_hour", "five_hour"], ["weekly", "weekly"]]) {
        const l = c[key];
        if (l && typeof l.pct === "number") add(kind, Math.round(l.pct), U.ts(l.resets_at), at);
      }
    }
    return [...by.values()].sort((a, b) => a.kind.localeCompare(b.kind) || (a.reset || 1e18) - (b.reset || 1e18));
  }

  function clock(ms, now) {
    const d = new Date(ms);
    const t = d.toLocaleTimeString([], { hour: "numeric", minute: "2-digit" }).toLowerCase();
    return Math.abs(ms - now) < 20 * HOUR ? t : d.toLocaleDateString([], { weekday: "short" }) + " " + t;
  }
  function span(ms) {
    const m = Math.max(1, Math.round(ms / 60000));
    return m < 60 ? m + "m" : Math.floor(m / 60) + "h " + String(m % 60).padStart(2, "0") + "m";
  }

  // The straight line through the window's readings, to when it would reach 100. Nothing is said below two readings.
  function pace(g, now) {
    const from = g.reset ? g.reset - WINDOW[g.kind] : now - WINDOW[g.kind];
    const pts = g.pts.filter(p => p.at >= from).sort((a, b) => a.at - b.at);
    if (pts.length < 2) return { pts, hit: 0 };
    const a = pts[0], b = pts[pts.length - 1];
    const rate = b.at > a.at ? (b.pct - a.pct) / (b.at - a.at) : 0;
    return { pts, hit: rate > 0 && g.pct < 100 ? now + (100 - g.pct) / rate : 0 };
  }

  function spark(g, now, hit) {
    const NS = "http://www.w3.org/2000/svg";
    const from = g.reset ? g.reset - WINDOW[g.kind] : now - WINDOW[g.kind];
    const x = t => Math.max(0, Math.min(100, (t - from) / WINDOW[g.kind] * 100));
    const y = p => 38 - Math.min(100, p) * 0.36;
    const svg = document.createElementNS(NS, "svg");
    svg.setAttribute("class", "us-spark"); svg.setAttribute("viewBox", "0 0 100 40"); svg.setAttribute("preserveAspectRatio", "none");
    const line = (pts, cls) => {
      const p = document.createElementNS(NS, "path");
      p.setAttribute("d", pts.map((q, i) => (i ? "L" : "M") + x(q.at).toFixed(1) + " " + y(q.pct).toFixed(1)).join(" "));
      if (cls) p.setAttribute("class", cls);
      svg.appendChild(p);
    };
    const pts = pace(g, now).pts;
    if (pts.length > 1) line(pts);
    if (hit) line([{ at: now, pct: g.pct }, { at: Math.min(hit, g.reset || hit), pct: g.reset && hit > g.reset ? g.pct + (100 - g.pct) * (g.reset - now) / (hit - now) : 100 }], "p");
    return svg;
  }

  function drawUsage(body) {
    const now = Date.now();
    body.textContent = "";
    const gs = groups(now);
    const view = el("div", "im-view");
    body.appendChild(view);
    if (!gs.length) { view.appendChild(el("p", "im-quiet", "No card has reported a limit yet.")); return; }
    const seen = {};
    for (const g of gs) {
      const n = seen[g.kind] = (seen[g.kind] || 0) + 1;
      const p = pace(g, now);
      const hot = g.pct >= 90 || (p.hit && (!g.reset || p.hit < g.reset) && p.hit - now <= 2 * HOUR);
      const box = el("div", "us-gauge" + (hot ? " hot" : ""));
      const head = el("div", "us-head");
      head.append(el("span", "im-k", LABEL[g.kind] + (n > 1 ? ", reset " + n : "")), el("b", "", g.pct + "%"));
      const track = el("div", "us-track"); const fill = el("i"); fill.style.width = Math.min(100, g.pct) + "%"; track.appendChild(fill);
      box.append(head, track);
      if (p.pts.length > 1) box.appendChild(spark(g, now, p.hit));
      const row = (k, v, cls) => { const r = el("div", "us-line"); r.append(el("span", "", k), el("span", cls || "", v)); box.appendChild(r); };
      if (g.reset) row("resets", "in " + span(g.reset - now));
      const early = p.hit && (!g.reset || p.hit < g.reset);
      row("at this pace", early ? "100% at " + clock(p.hit, now) + (g.reset ? ", " + span(g.reset - p.hit) + " before reset" : "") : "stays under 100%", early ? "us-flame" : "");
      view.appendChild(box);
    }
  }

  // ---- queue ------------------------------------------------------------------------------------------------------
  function asks() {
    const now = Date.now();
    return window.mHome.needsList(window.mStore.cards(), window.mStore.perms(), now).map(r => {
      const t = r.card, why = r.reasons[0];
      const verb = t.ask_verb ? String(t.ask_verb).toUpperCase() : VERB[why.kind] || "ANSWER";
      return { card: t, verb, why, since: r.since, stuck: !!(t.escalation && t.escalation.since) || verb === "UNBLOCK" };
    });
  }

  function askRow(a) {
    const row = el("div", "im-row" + (a.stuck ? " st-blocked" : ""));
    const main = el("button", "im-main"); main.type = "button";
    const meta = el("span", "im-meta");
    meta.append(el("span", "yq-verb" + (a.stuck ? " stuck" : ""), a.verb), el("span", "im-id", a.since ? U.ago(Date.now() - a.since) : ""));
    const why = el("p", "yq-why"); why.innerHTML = a.why.text;
    main.append(meta, el("span", "im-ttl", U.cardName(a.card)), why);
    main.addEventListener("click", () => { hide("queue"); if (window.mCard) window.mCard.open(a.card.id); });
    row.appendChild(main);
    return row;
  }

  function drawQueue(body) {
    body.textContent = "";
    const view = el("div", "im-view");
    body.appendChild(view);
    const all = asks();
    if (!all.length) { view.appendChild(el("p", "im-quiet", "Nothing is waiting on you.")); return; }
    for (const [name, list, cls] of [["Stuck", all.filter(a => a.stuck), " w"], ["Waiting", all.filter(a => !a.stuck), ""]]) {
      if (!list.length) continue;
      const g = el("div", "im-grp" + cls, name);
      g.appendChild(el("i", "", String(list.length)));
      const l = el("div", "im-list");
      list.forEach(a => l.appendChild(askRow(a)));
      view.append(g, l);
    }
  }

  function badge() {
    const b = document.getElementById("m-queue-btn");
    if (!b) return;
    b.hidden = typeof flagOn === "function" && !flagOn("queue");
    const n = asks().length;
    b.textContent = "queue";
    if (n) b.appendChild(el("em", "", String(n)));
  }

  function init() {
    const q = document.getElementById("m-queue-btn"), u = document.getElementById("m-usage-btn");
    if (q) q.addEventListener("click", () => open("queue", "Queue", drawQueue));
    if (u) u.addEventListener("click", () => { open("usage", "Usage", drawUsage); loadReadings(); });
    window.mStore.on("cards", () => { badge(); repaint("queue"); repaint("usage"); });
    window.mStore.on("perms", () => { badge(); repaint("queue"); });
    badge();
  }
  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", init); else init();
})();
