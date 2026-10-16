// Items on the phone (/m): a sheet with two screens, the list and one item's page, the same rows as the board's Items tab (js/items.js) in one
// column. docs/rnd/item-lifecycle-design.md sections 7.1 to 7.3 and docs/changes/item-lifecycle-l7.md.
//
// THE DATA IS THE BOARD'S: js/items-data.js (`itemsData`) is loaded here too, so one file knows the hub's routes and the fixtures
// (`?itemsmock=1`) work on the phone as on the board. This file only draws. EVERYTHING FROM THE HUB IS TEXT: titles, questions and
// notes are set with textContent, never as markup.
//
// A DOOR ONLY WHERE THERE IS A HUB THAT ANSWERS. The "items" button in the header stays hidden until the list answers once, and carries
// the count of items blocked on the operator. History is the sheet's back: opening it pushes the list, an item pushes a second entry.
// Live by the hub's `item` event, which the store passes to `onEvent`, and read again when the sheet opens. No timer.
//
// A BLOCKED ITEM asks on its row and above its timeline, and answers the same way the board does. The worker's own suggested answers
// are buttons, the first in orange. A button answer is typed to the worker as `atrium` and the item leaves `blocked` when the worker's
// turn starts, so the notice says what happened and does not claim the item is running. A design waiting on approval has `approve` and
// `send back` instead, which are the step engine's routes.
(function () {
  "use strict";

  if (typeof itemsData === "undefined") return;

  const GROUPS = [["blocked", "blocked"], ["running", "running"], ["queued", "queued"], ["finished", "finished"]];
  const FINISHED_SHOWN = 5;
  const SEG = { done: "d", active: "c", blocked: "c", stale: "s", skipped: "k", pending: "" };
  const NODE = { done: "d", active: "c", blocked: "b", stale: "w", skipped: "k", pending: "f" };

  let els = null;
  let avail = null;
  const st = { list: [], stats: null, loaded: false, screen: "list", sel: "", detail: null, note: "", asking: {}, draft: {}, showAll: false, busy: false, err: "" };

  const el = (tag, cls, text) => { const e = document.createElement(tag); if (cls) e.className = cls; if (text != null) e.textContent = text; return e; };
  const btn = (label, fn, cls) => { const b = el("button", "im-btn" + (cls ? " " + cls : ""), label); b.type = "button"; if (fn) b.addEventListener("click", fn); return b; };
  const ms = iso => { const t = iso ? Date.parse(iso) : NaN; return isNaN(t) ? null : t; };
  const dur = (from, to) => {
    const a = ms(from), b = to ? ms(to) : Date.now();
    if (a == null || b == null) return "";
    const m = Math.max(0, Math.round((b - a) / 60000));
    if (m < 1) return "just now";
    if (m < 60) return m + "m";
    if (m < 1440) return Math.floor(m / 60) + "h " + String(m % 60).padStart(2, "0") + "m";
    return Math.floor(m / 1440) + "d " + Math.floor((m % 1440) / 60) + "h";
  };
  const clock = iso => { const t = ms(iso); if (t == null) return ""; const d = new Date(t); return String(d.getHours()).padStart(2, "0") + ":" + String(d.getMinutes()).padStart(2, "0"); };
  const tokN = t => (t ? t.in + t.cw + t.cr + t.out : 0);
  const tok = n => (n < 1000 ? String(n) : n < 1e6 ? Math.round(n / 1000) + "k" : (n / 1e6).toFixed(1) + "M");
  const usd = v => (v == null ? "?" : "$" + v.toFixed(2));
  const cancelled = it => it.state === "finished" && /cancelled/i.test(it.note || "");
  const stateWord = it => (cancelled(it) ? "cancelled" : it.state);
  const blockedOnMe = it => it.state === "blocked" && it.owner_kind === "human";
  const byId = id => (st.detail && st.detail.id === id ? st.detail : st.list.find(x => x.id === id) || null);
  const title = it => it.title || it.card || it.id;
  const stack = t => {
    const s = el("div", "im-stack"), n = tokN(t);
    if (n) for (const k of ["in", "cw", "cr", "out"]) { const i = el("i", "t-" + k); i.style.width = (t[k] * 100 / n) + "%"; s.appendChild(i); }
    return s;
  };

  // ---- reading ----------------------------------------------------------------------------------------------------
  async function load() {
    try {
      const a = await itemsData.list();
      st.list = a.items; st.stats = a.stats; st.loaded = true; st.err = "";
    } catch (e) {
      st.loaded = false;
      st.err = "The hub is not answering, so there is nothing to show until it is back.";
    }
    badge();
    return st.loaded;
  }
  async function loadDetail(id) {
    try {
      const d = await itemsData.detail(id);
      if (st.sel === id) st.detail = d;
    } catch (e) { /* the page keeps what the list had */ }
  }
  function badge() {
    const b = document.getElementById("m-items-btn");
    if (!b) return;
    const n = st.list.filter(blockedOnMe).length;
    b.textContent = "items";
    if (n) { const em = el("em", "", String(n)); em.setAttribute("aria-label", n + " blocked on you"); b.appendChild(em); }
  }

  // ---- the sheet --------------------------------------------------------------------------------------------------
  function build() {
    if (els) return els;
    const sheet = el("section", "imsheet");
    sheet.id = "m-items"; sheet.hidden = true; sheet.setAttribute("aria-label", "items");
    try { const v = parseFloat(localStorage.getItem("atrium.mfs")); if (v) sheet.style.setProperty("--m-fs", v + "px"); } catch (e) { /* the default size */ }
    const top = el("div", "im-top");
    const back = el("button", "im-back"); back.type = "button"; back.setAttribute("aria-label", "back");
    back.innerHTML = '<svg viewBox="0 0 24 24" width="22" height="22" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M15 5l-7 7 7 7"/></svg>';
    const ttl = el("span", "im-title");
    top.append(back, ttl);
    const note = el("p", "im-note"); note.setAttribute("role", "status");
    const scroll = el("div", "im-scroll"), body = el("div", "im-body");
    scroll.appendChild(body);
    sheet.append(top, note, scroll);
    document.body.appendChild(sheet);
    back.addEventListener("click", close);
    els = { sheet, ttl, note, scroll, body };
    return els;
  }

  function go(s, replace) {
    build();
    try { (replace ? history.replaceState : history.pushState).call(history, { mit: s }, "", location.href); } catch (e) { /* history is a nicety */ }
    render(s);
  }
  function render(s) {
    build();
    st.screen = s.v; st.sel = s.id || "";
    if (s.v === "list") st.detail = null;
    els.sheet.hidden = false;
    document.body.classList.add("im-open");
    draw(true);
  }
  function hide() { if (!els) return; els.sheet.hidden = true; els.body.textContent = ""; document.body.classList.remove("im-open"); st.note = ""; st.detail = null; }
  function close() {
    if (!els || els.sheet.hidden) return;
    if (history.state && history.state.mit) history.back(); else hide();
  }

  async function openList() {
    await load();
    go({ v: "list" });
  }
  async function openItem(id) {
    st.detail = null; st.note = "";
    go({ v: "item", id });
    await loadDetail(id);
    if (st.sel === id && st.screen === "item") draw(false);
  }
  async function refresh() {
    await load();
    if (st.screen === "item" && st.sel) await loadDetail(st.sel);
    draw(false);
  }

  function say(text) { st.note = text || ""; if (els) els.note.textContent = st.note; }

  // ---- acts -------------------------------------------------------------------------------------------------------
  // What is typed in a box survives the repaint a live event causes.
  function keepDrafts() { if (els) for (const i of els.body.querySelectorAll(".im-box")) st.draft[i.dataset.id] = i.value; }

  async function act(id, body, done) {
    if (st.busy) return false;
    st.busy = true; say("");
    try {
      const it = byId(id);
      await itemsData.act(id, body, it && it.backlog);
      delete st.draft[id]; delete st.asking[id];
      say(done);
      return true;
    } catch (e) {
      say("Could not " + body.do.replace("_", " ") + " " + id + ": " + e.message);
      return false;
    } finally {
      st.busy = false;
      refresh();
    }
  }
  const answer = (id, text) => {
    const a = (text || "").trim();
    if (!a) { say("Type the answer first."); return; }
    return act(id, { do: "unblock", answer: a }, "Sent the answer for " + id + ". It shows as running once the worker starts its turn.");
  };
  const approve = id => {
    const it = byId(id), sha = it && it.ask && it.ask.sha ? it.ask.sha : "";
    return act(id, sha ? { do: "approve", sha } : { do: "approve" }, "Approved " + ((it && it.backlog) || id) + (sha ? " at " + sha.slice(0, 7) : "") + ". The build step is next.");
  };
  const sendBack = (id, text) => {
    const n = (text || "").trim(), it = byId(id);
    if (!n) { say("A design is sent back with a note: type what the designer should change."); return; }
    return act(id, { do: "send_back", note: n }, "Sent " + ((it && it.backlog) || id) + " back. The note is typed to the designer, and the item shows as running once its turn starts.");
  };
  function cancelItem(id) {
    if (!window.confirm("Cancel " + id + "? The item is finished as cancelled and its worker is asked to leave. This cannot be undone.")) return;
    act(id, { do: "cancel" }, "Cancelled " + id + ".");
  }

  // ---- drawing ----------------------------------------------------------------------------------------------------
  function draw(top) {
    if (!els || els.sheet.hidden) return;
    keepDrafts();
    const view = el("div", "im-view");
    let ttl = "Items";
    if (st.screen === "item") {
      const it = byId(st.sel);
      ttl = it ? it.backlog || it.id : "Item";
      view.appendChild(it ? drawPage(it) : el("p", "im-quiet", "This item is not on the hub any more."));
    } else view.appendChild(drawList());
    els.ttl.textContent = ttl;
    els.note.textContent = st.note;
    const keep = els.scroll.scrollTop;
    els.body.replaceChildren(view);
    els.scroll.scrollTop = top ? 0 : keep;
  }

  function progress(it) {
    const p = el("div", "im-prog" + (it.state === "blocked" ? " b" : ""));
    for (const s of it.steps) { const i = el("span", SEG[s.state] || ""); i.title = s.name; p.appendChild(i); }
    return p;
  }

  function currentLine(it) {
    const i = typeof itemCurrent === "function" ? itemCurrent(it) : 0, s = it.steps[i] || { name: "" }, of = it.steps.length;
    const l = el("div", "im-cur");
    if (it.state === "finished") {
      const n = it.steps.filter(x => x.state === "done" || x.state === "skipped").length;
      l.append(el("b", "", cancelled(it) ? "cancelled" : "finished"), el("em", "", n + " of " + of + " steps"));
      return l;
    }
    const step = "step " + (i + 1) + " of " + of;
    if (it.state === "blocked") {
      l.append(el("b", "w", (blockedOnMe(it) ? "waiting on you at " : "blocked at ") + s.name), el("em", "", step + " · " + dur(it.ask && it.ask.since ? it.ask.since : it.updated_at)));
    } else if (it.state === "queued") l.append(el("b", "t", s.name), el("em", "", step + " · queued " + dur(it.created_at)));
    else l.append(el("b", "t", s.name), el("em", "", step + " · " + (s.note || dur(s.from))));
    return l;
  }

  function numbersLine(it) {
    const n = tokN(it.tokens);
    const age = it.state === "finished" ? "done " + dur(it.updated_at) + " ago" : dur(it.created_at);
    return el("div", "im-nums", (n ? tok(n) + " tokens" : "no usage yet") + " · " + (it.usd == null && !n ? "–" : usd(it.usd)) + " · " + age);
  }

  // A card's chip. A minion says what it is doing, in orange when it needs you, and a card the board no longer holds is struck through.
  function chip(c) {
    const all = window.mStore && window.mStore.cards ? window.mStore.cards() : [];
    const t = all.length ? all.find(x => x.id === c.id) || null : undefined;
    const name = c.name || c.id;
    if (t === null) { const g = el("span", "im-card gone", name); g.title = "this card has ended and was removed"; return g; }
    if (!t || typeof isMinion !== "function" || !isMinion(t)) return el("span", "im-card", name);
    const need = cardNeedsYou(t), cs = cardState(t);
    const word = need ? (t.status === "needs-permission" ? "needs permission" : "needs you") : cs === "off" ? "ended" : cs === "unreachable" ? "unreachable" : "running";
    const e = el("span", "im-card minion" + (need ? " hot" : ""), name + " ");
    e.appendChild(el("i", "im-mst", word));
    return e;
  }

  function chips(it) {
    const m = el("div", "im-meta");
    m.appendChild(el("span", "im-id", it.backlog));
    if (it.priority) m.appendChild(el("span", "im-tag" + (it.priority === "high" ? " hi" : ""), it.priority));
    if (it.dept) m.appendChild(el("span", "im-tag", it.dept));
    for (const c of it.cards || []) m.appendChild(chip(c));
    return m;
  }

  // The question, as the board draws it on a row: who is asking is implied, the words, then the answers as full width buttons.
  function questionRow(it) {
    const ask = it.ask || { text: it.note || "(no reason given)", options: [] };
    const box = el("div", "im-ask");
    box.append(el("b", "", "asks you"), el("span", "im-q", ask.brief || ask.text));
    box.appendChild(answerControls(it, false));
    return box;
  }

  // The buttons and the box for one blocked item. `page` is the detail's, where the box is always open.
  function answerControls(it, page) {
    const id = it.id, ask = it.ask || { options: [] };
    const wrap = el("div", "im-ctl");
    const approval = itemAwaitsApproval(it);
    const bar = el("div", "im-opts");
    if (approval) {
      bar.appendChild(btn("approve", () => approve(id), "go"));
      if (!page) bar.appendChild(btn("send back", () => { keepDrafts(); st.asking[id] = !st.asking[id]; draw(false); }));
    } else {
      (ask.options || []).slice(0, 3).forEach((o, n) => bar.appendChild(btn(o, () => answer(id, o), n === 0 ? "go" : "")));
      if (!page) bar.appendChild(btn((ask.options || []).length ? "other" : "answer", () => { keepDrafts(); st.asking[id] = !st.asking[id]; draw(false); }, (ask.options || []).length ? "" : "go"));
    }
    if (bar.childNodes.length) wrap.appendChild(bar);
    if (page || st.asking[id]) {
      const row = el("div", "im-ans");
      const box = el("input", "im-box");
      box.type = "text"; box.dataset.id = id; box.value = st.draft[id] || "";
      box.placeholder = approval ? "or send back with a note" : page ? "or type an answer to the worker" : "answer";
      box.setAttribute("aria-label", (approval ? "note for " : "answer for ") + id);
      box.setAttribute("enterkeyhint", "send");
      const send = () => (approval ? sendBack(id, box.value) : answer(id, box.value));
      box.addEventListener("keydown", e => { if (e.key === "Enter") { e.preventDefault(); send(); } });
      row.append(box, btn(approval ? "send back" : "send", send, approval ? "" : "go"));
      wrap.appendChild(row);
    }
    return wrap;
  }

  function drawRow(it) {
    const r = el("div", "im-row st-" + stateWord(it));
    r.dataset.item = it.id;
    const main = el("button", "im-main"); main.type = "button";
    main.append(el("span", "im-ttl", title(it)), chips(it));
    main.addEventListener("click", () => openItem(it.id));
    r.append(main, progress(it), currentLine(it), numbersLine(it));
    if (it.state === "blocked") r.appendChild(questionRow(it));
    return r;
  }

  function drawStats(s) {
    const g = el("div", "im-stats");
    const cell = (k, v, sub, hot) => { const c = el("div", "im-stat" + (hot ? " hot" : "")); c.append(el("span", "im-k", k), el("b", "", v), el("small", "", sub)); return c; };
    g.append(cell("needs you", String(s.needs_you), "blocked on " + (s.who || "you"), s.needs_you > 0), cell("in flight", String(s.in_flight), (s.working || 0) + " building or landing"),
      cell("spend today", s.spend_today == null ? "–" : "$" + s.spend_today.toFixed(1), s.spend_today == null ? "no usage yet" : "incl. $" + (s.spend_coord || 0).toFixed(1) + " coordination"),
      cell("landed this week", s.landed_week == null ? "–" : String(s.landed_week), s.landed_week == null ? "none yet" : "median " + (s.landed_median || "–") + ", " + usd(s.landed_cost)));
    return g;
  }

  function drawList() {
    const root = el("div", "im-list");
    if (!st.loaded) {
      root.append(el("p", "im-quiet im-warn", st.err || "The items are not answering right now."), btn("Try again", () => refresh()));
      return root;
    }
    if (!st.list.length) { root.appendChild(el("p", "im-quiet", "No items.")); return root; }
    if (st.stats) root.appendChild(drawStats(st.stats));
    for (const [state, label] of GROUPS) {
      let rows = st.list.filter(it => it.state === state);
      if (!rows.length) continue;
      // The oldest question first, as on the board.
      if (state === "blocked") rows = rows.slice().sort((a, b) => (ms(a.ask && a.ask.since) || 0) - (ms(b.ask && b.ask.since) || 0));
      const shown = state === "finished" && !st.showAll ? rows.slice(0, FINISHED_SHOWN) : rows;
      const h = el("div", "im-grp" + (state === "blocked" ? " w" : ""));
      h.append(el("span", "", label), el("i", "", String(rows.length)));
      root.appendChild(h);
      for (const it of shown) root.appendChild(drawRow(it));
      if (rows.length > shown.length) root.appendChild(btn("show " + (rows.length - shown.length) + " more", () => { st.showAll = true; draw(false); }, "quiet"));
    }
    return root;
  }

  function drawStep(it, s, i) {
    const c = el("div", "im-step " + (NODE[s.state] || "f"));
    c.dataset.step = s.name;
    c.appendChild(el("div", "im-node", s.state === "done" ? "✓" : s.state === "skipped" ? "–" : String(i + 1)));
    const body = el("div", "im-sc");
    const head = el("div", "im-sn");
    head.appendChild(el("span", "", s.name));
    let range = s.when || "";
    if (!range && s.state !== "pending" && s.from) range = clock(s.from) + " → " + (s.to ? clock(s.to) : "now") + " · " + dur(s.from, s.to);
    if (range) head.appendChild(el("small", "", range));
    body.appendChild(head);
    const who = el("div", "im-who");
    for (const w of s.cards || []) who.append(el("span", "im-card", w.name), el("span", "", w.role || ""));
    if (s.sha) who.appendChild(el("span", "im-sha", s.sha));
    if (s.strength) who.appendChild(el("span", "im-obs" + (s.strength === "reported" ? " r" : ""), s.strength));
    const fact = s.state === "skipped" ? "skipped: " + (s.reason || "not needed") : s.fact || s.note || "";
    if (fact) who.appendChild(el("span", "im-fact", fact));
    if (who.childNodes.length) body.appendChild(who);
    const n = tokN(s.tokens);
    if (s.state !== "pending" && (s.usd != null || n)) {
      const m = el("div", "im-money");
      m.append(el("b", "", s.usd == null ? "?" : usd(s.usd)), el("span", "", n ? tok(n) + " tokens" : "no model turns"));
      body.append(m);
      if (n) body.appendChild(stack(s.tokens));
    }
    c.appendChild(body);
    return c;
  }

  function drawPage(it) {
    const d = st.detail && st.detail.id === it.id ? st.detail : it;
    const page = el("div", "im-page");
    page.dataset.detail = d.id;
    const head = el("div", "im-head");
    head.appendChild(el("span", "im-pill" + (d.state === "blocked" ? " b" : ""), blockedOnMe(d) ? "blocked on you" : stateWord(d)));
    head.appendChild(el("h2", "", title(d)));
    const meta = chips(d);
    meta.appendChild(el("span", "", "owner " + (d.owner_kind === "role" ? d.owner + " role" : d.owner || "you")));
    if (d.room) meta.appendChild(el("span", "", "room " + d.room));
    head.appendChild(meta);
    if (d.body) head.appendChild(el("p", "im-body-ex", d.body));
    if (d.note && d.state === "finished") head.appendChild(el("p", "im-body-ex", d.note));
    page.appendChild(head);
    if (d.state === "blocked") {
      const ask = d.ask || { text: d.note || "(no reason given)" };
      const q = el("div", "im-qbox");
      q.append(el("h3", "", "asks you" + (ask.who ? " · " + ask.who : "") + (ask.since ? " · blocked " + dur(ask.since) : "")), el("p", "im-q", ask.text), answerControls(d, true));
      page.appendChild(q);
    }
    const tl = el("div", "im-tl");
    d.steps.forEach((s, i) => tl.appendChild(drawStep(d, s, i)));
    page.appendChild(tl);
    page.appendChild(drawTotals(d));
    if (d.state !== "finished") page.appendChild(btn("cancel item", () => cancelItem(d.id), "danger"));
    return page;
  }

  function drawTotals(d) {
    const n = tokN(d.tokens);
    const box = el("div", "im-tot");
    const big = el("div", "im-big");
    big.append(el("b", "", d.usd == null && !n ? "–" : usd(d.usd)), el("small", "", (d.state === "finished" ? "total" : "so far") + " · " + (n ? tok(n) + " tokens" : "no usage yet") + " · estimated"));
    box.append(el("h3", "", "Item total"), big, stack(d.tokens));
    if (d.coord_usd != null && d.usd != null) {
      const k = el("div", "im-kv");
      k.append(el("span", "", "with coordination"), el("b", "", usd(d.usd + d.coord_usd)));
      box.appendChild(k);
    }
    return box;
  }

  // ---- the door ---------------------------------------------------------------------------------------------------
  async function init() {
    const door = document.getElementById("m-items-btn");
    window.addEventListener("popstate", e => {
      if (!els || els.sheet.hidden) { if (e.state && e.state.mit && avail) render(e.state.mit); return; }
      if (e.state && e.state.mit) render(e.state.mit); else hide();
    });
    if (door) door.addEventListener("click", openList);
    avail = await load();
    if (door) door.hidden = !avail;
  }
  // The hub says `item` on /v1/events; the store passes it here. Read again once the list has been read at all.
  let evT = 0;
  function onEvent() {
    if (!st.loaded) return;
    clearTimeout(evT);
    evT = setTimeout(refresh, 300);
  }
  window.mItems = { init, openList, openItem, onEvent, available: () => avail === true, state: st };
  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", init); else init();
})();
