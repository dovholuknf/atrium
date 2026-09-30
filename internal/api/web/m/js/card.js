// The card view: a full-height sheet opened by a tap. The browser's back button closes it, through history state, so
// the back gesture feels native.
//
// Top to bottom: the name and what it is doing, anything held or reported for the operator, the last replies as a
// conversation, the recap and the open questions, the permission rows (`#m-perms`, perms.js), and the composer pinned
// under it all (`#m-compose`, compose.js). Those two files are another worker's, so every call to them is guarded.
(function () {
  "use strict";

  const U = window.mUtil;
  const MD = window.mMd;
  const REPLIES_N = 3;

  let openId = "";
  let els = null;
  let seq = 0;
  let turnKey = "";
  let offs = [];
  // Replies already read, so a card opened again paints at once and is refreshed behind it.
  const cache = new Map();
  let mounted = "";

  function q(id) { return document.getElementById(id); }

  function turnOf(t) { return (t && t.seen && t.seen.turn_ended_at) || ""; }

  // ── the parts ────────────────────────────────────────────────────────────
  function headHTML(t) {
    const nm = U.cardName(t);
    const room = window.mNet.manyRooms() ? window.mNet.roomOf(t.id) || t.room || "" : "";
    const doing = U.activityText(t);
    return '<h1 class="c-name">' + U.esc(nm.main) + "</h1>" +
      (nm.sub ? '<p class="c-sub">' + U.esc(nm.sub) + "</p>" : "") +
      '<p class="c-state"><span class="pill s-' + U.esc(t.status) + '">' + U.esc(U.statusLabel(t)) + "</span>" +
      (room ? '<span class="pill room">' + U.esc(room) + "</span>" : "") +
      (doing ? '<span class="doing">' + U.esc(doing) + "</span>" : "") + "</p>";
  }

  function noticesHTML(t) {
    const out = [];
    const rs = window.mHome.reasons(t, window.mStore.perms());
    const rep = rs.find(r => r.kind === "report");
    if (rep) {
      out.push('<div class="notice report"><b>Report waiting</b><span>' + U.esc(t.report_sha ? "commit " + String(t.report_sha).slice(0, 7) + ", " : "") +
        U.esc(U.ago(Date.now() - rep.since)) + " ago</span></div>");
    }
    const held = rs.find(r => r.kind === "held");
    if (held) out.push('<div class="notice held"><b>' + U.esc(held.text) + "</b><span>it goes in when the line is clear</span></div>");
    return out.join("");
  }

  function replyHTML(r, screen) {
    const when = r.at ? U.ago(Date.now() - U.ts(r.at)) : "";
    const body = screen ? '<pre class="screen">' + U.esc(r.text) + "</pre>" : '<div class="md">' + MD.render(r.text) + "</div>";
    return '<article class="reply' + (screen ? " from-screen" : "") + '">' +
      (screen ? '<span class="src">from the screen</span>' : "") + body +
      (r.truncated ? '<p class="cut">cut short here. the rest is in the terminal</p>' : "") +
      (when ? '<time datetime="' + U.esc(r.at) + '">' + U.esc(when === "now" ? "just now" : when + " ago") + "</time>" : "") +
      "</article>";
  }

  function fallbackHTML(t) {
    const recap = String(t.recap || "").trim();
    return recap ? "" : '<p class="quiet">Nothing to read here yet.</p>';
  }

  function recapHTML(t) {
    const recap = String(t.recap || "").trim();
    let h = "";
    if (recap) {
      h += '<section class="block"><h2>Recap</h2><div class="md">' + MD.render(recap) + "</div></section>";
    }
    return h;
  }

  function questionsHTML(t) {
    const s = t.seen || {};
    if (s.answered !== false) return "";
    const qs = s.open_questions || [];
    if (!qs.length && !s.questions_unparsed) return "";
    return '<section class="block questions"><h2>Open questions</h2>' + (qs.length
      ? "<ol>" + qs.map(x => "<li>" + MD.inline(x) + "</li>").join("") + "</ol>"
      : '<p class="quiet">Its last turn asked questions atrium could not read. Look at the terminal.</p>') + "</section>";
  }

  function repliesHTML(t, got) {
    if (!got) return '<div class="replies loading" aria-busy="true"><div class="sk"></div><div class="sk s2"></div></div>';
    if (got.failed) {
      // The endpoint is not on this room yet, or it failed. The recap and the last report stand in for it.
      const rep = t.reported_at ? '<p class="quiet">Last report ' + U.esc(U.ago(Date.now() - U.ts(t.reported_at))) +
        " ago" + (t.report_sha ? ", commit " + U.esc(String(t.report_sha).slice(0, 7)) : "") + ".</p>" : "";
      return '<div class="replies">' + rep + fallbackHTML(t) + "</div>";
    }
    const screen = got.source === "screen";
    const list = got.replies || [];
    if (!list.length) return '<div class="replies">' + fallbackHTML(t) + "</div>";
    return '<div class="replies">' + list.map(r => replyHTML(r, screen)).join("") + "</div>";
  }

  // ── painting ─────────────────────────────────────────────────────────────
  function paint(full) {
    if (!els || !openId) return;
    const t = window.mStore.card(openId);
    if (!t) {
      els.head.innerHTML = '<h1 class="c-name">This card is gone</h1><p class="c-sub">It was removed or its room went away.</p>';
      els.notices.innerHTML = els.replies.innerHTML = els.extras.innerHTML = "";
      return;
    }
    const head = headHTML(t);
    if (els.head.dataset.sig !== head) { els.head.innerHTML = head; els.head.dataset.sig = head; }
    const not = noticesHTML(t);
    if (els.notices.dataset.sig !== not) { els.notices.innerHTML = not; els.notices.dataset.sig = not; }
    const ex = recapHTML(t) + questionsHTML(t);
    if (els.extras.dataset.sig !== ex) { els.extras.innerHTML = ex; els.extras.dataset.sig = ex; }
    els.term.href = pathFor(t, "") || "/#term=" + encodeURIComponent(t.id);
    if (full) paintReplies();
  }

  function paintReplies() {
    const t = window.mStore.card(openId);
    if (!t) return;
    const html = repliesHTML(t, cache.get(openId));
    if (els.replies.dataset.sig === html) return;
    const first = !els.replies.dataset.sig;
    els.replies.innerHTML = html;
    els.replies.dataset.sig = html;
    if (!first) els.replies.classList.add("fresh");
    setTimeout(() => els.replies.classList.remove("fresh"), 400);
  }

  // The last replies. Read on open and when the card's turn ends, never on a timer.
  async function loadReplies() {
    const id = openId;
    if (!id) return;
    const mine = ++seq;
    let got;
    try {
      const r = await window.mNet.api("/v1/tasks/" + encodeURIComponent(id) + "/replies?n=" + REPLIES_N);
      got = r && Array.isArray(r.replies) ? r : { failed: true };
    } catch (e) {
      got = { failed: true };
    }
    if (mine !== seq || id !== openId) return;
    cache.set(id, got);
    paintReplies();
  }

  function onCards() {
    if (!openId) return;
    paint(false);
    const key = openId + "@" + turnOf(window.mStore.card(openId));
    if (key !== turnKey) { turnKey = key; loadReplies(); }
  }

  function mountFor(id) {
    if (window.mPerms && window.mPerms.mount) { try { window.mPerms.mount(els.perms, id); } catch (e) { console.error(e); } }
    if (window.mCompose && window.mCompose.mount) { try { window.mCompose.mount(els.compose, id); } catch (e) { console.error(e); } }
    mounted = id;
  }
  function unmountAll() {
    if (!mounted) return;
    if (window.mPerms && window.mPerms.unmount) { try { window.mPerms.unmount(); } catch (e) { console.error(e); } }
    if (window.mCompose && window.mCompose.unmount) { try { window.mCompose.unmount(); } catch (e) { console.error(e); } }
    mounted = "";
  }

  // ── open and close ───────────────────────────────────────────────────────
  function reduced() { return window.matchMedia("(prefers-reduced-motion: reduce)").matches; }

  function open(id, fromHistory) {
    if (!els || !id) return;
    if (openId) return;
    openId = id;
    turnKey = id + "@" + turnOf(window.mStore.card(id));
    els.sheet.hidden = false;
    menuClose();
    els.sheet.classList.toggle("direct", !!(history.state && history.state.direct));
    els.scroll.scrollTop = 0;
    els.head.dataset.sig = els.notices.dataset.sig = els.extras.dataset.sig = els.replies.dataset.sig = "";
    paint(true);
    mountFor(id);
    // A frame later, so the slide has a start to move from.
    requestAnimationFrame(() => requestAnimationFrame(() => els.sheet.classList.add("on")));
    document.body.classList.add("sheet-open");
    if (!fromHistory) {
      // The card's readable path goes in the bar, so a bookmark or a shared link opens it. A card with no name keeps the address.
      const at = pathFor(window.mStore.card(id), "/m");
      try { history.pushState({ mcard: id }, "", at || location.href); } catch (e) {}
    }
    offs = [window.mStore.on("cards", onCards), window.mStore.on("perms", () => paint(false))];
    loadReplies();
  }

  function finishClose() {
    els.sheet.hidden = true;
    els.head.innerHTML = els.notices.innerHTML = els.replies.innerHTML = els.extras.innerHTML = "";
    openId = "";
  }

  // The back button and the on-screen chevron both end up here through popstate.
  function closeNow() {
    if (!openId) return;
    offs.forEach(f => f());
    offs = [];
    unmountAll();
    seq++;
    document.body.classList.remove("sheet-open");
    els.sheet.classList.remove("on");
    const id = openId;
    if (reduced()) { finishClose(); return; }
    const done = () => { if (openId === id && !els.sheet.classList.contains("on")) finishClose(); };
    els.sheet.addEventListener("transitionend", done, { once: true });
    setTimeout(done, 400);
  }

  function close() {
    if (!openId) return;
    // Opened straight from its address, so there is no page of ours behind it to go back to.
    if (history.state && history.state.direct) {
      try { history.replaceState(null, "", "/m/"); } catch (e) {}
      closeNow();
    } else if (history.state && history.state.mcard) history.back(); // popstate does the closing
    else closeNow();
  }

  // ── the card picker: every other card, newest first, one tap to go there ──
  function menuClose() {
    els.menu.hidden = true;
    els.pick.setAttribute("aria-expanded", "false");
  }

  function menuToggle() {
    if (!els.menu.hidden) { menuClose(); return; }
    const cards = window.mStore.cards().filter(t => !t.archived_at && t.id !== openId);
    cards.sort((a, b) => cardActivityCmp(a, b) || cardTieBreak(a, b));
    els.menu.innerHTML = cards.length ? cards.map(t => {
      const nm = U.cardName(t);
      return '<button type="button" class="pm-row" data-id="' + U.esc(t.id) + '"><b>' + U.esc(nm.main) + "</b>" +
        '<span>' + U.esc(U.statusLabel(t)) + "</span></button>";
    }).join("") : '<p class="quiet">no other cards</p>';
    els.menu.hidden = false;
    els.pick.setAttribute("aria-expanded", "true");
  }

  // Leaves this card without the slide and opens another, which writes its own history entry.
  function goTo(id) {
    menuClose();
    offs.forEach(f => f());
    offs = [];
    unmountAll();
    seq++;
    document.body.classList.remove("sheet-open");
    els.sheet.classList.remove("on");
    finishClose();
    // The entry this card was opened on now shows the other one, so the way back still leaves for the list.
    const direct = !!(history.state && history.state.direct);
    open(id, true);
    const at = pathFor(window.mStore.card(id), "/m");
    try { history.replaceState({ mcard: id, direct }, "", at || location.href); } catch (e) {}
  }

  // ── the card's address ───────────────────────────────────────────────────
  // `/m/alias/<alias>` and `/m/room/<room>/<name>` open a card, the phone's side of docs/rnd/card-urls-design.md. Names
  // are compared the way the resolver does, without a leading `@` and in lower case. The lookup is one read per open.
  const enc = encodeURIComponent;
  const dec = s => { try { return decodeURIComponent(s); } catch (e) { return s; } };
  const nameOf = s => String(s || "").replace(/^@/, "").toLowerCase();

  function shapeOf(path) {
    const p = String(path).replace(/\/$/, "").split("/");
    if (p[0] !== "" || p[1] !== "m") return null;
    if (p[2] === "alias" && p.length === 4 && p[3]) return { kind: "alias", alias: nameOf(dec(p[3])) };
    if (p[2] === "room" && p.length === 5 && p[3] && p[4]) return { kind: "card", room: dec(p[3]), name: dec(p[4]).replace(/^@/, "") };
    return null;
  }

  // The path a card is linked by under `base` (`/m` for this page, empty for the board), or "" when it has no alias
  // and no handle. `/alias/` unless another live card holds the alias, then the room's qualified form.
  function pathFor(t, base) {
    if (!t) return "";
    const alias = nameOf(t.alias);
    const wire = String(t.wire_name || "");
    const handle = wire.slice(wire.lastIndexOf("/") + 1);
    const room = t.room || window.mNet.roomOf(t.id) || "";
    const clash = alias && window.mStore.cards().some(o => window.mNet.bareId(o.id) !== window.mNet.bareId(t.id) &&
      nameOf(o.alias) === alias && o.status !== "dead" && o.status !== "done");
    if (alias && !clash) return base + "/alias/" + enc(alias);
    const name = handle || alias;
    if (room && name) return base + "/room/" + enc(room) + "/" + enc(name);
    return alias ? base + "/alias/" + enc(alias) : "";
  }

  function keyOf(s) { return "atrium.cardurl." + (s.kind === "alias" ? "alias:" + s.alias : s.room + "/" + s.name); }
  function lastOf(s) { try { return localStorage.getItem(keyOf(s)) || ""; } catch (e) { return ""; } }
  function remember(s, id) { try { localStorage.setItem(keyOf(s), window.mNet.bareId(id)); } catch (e) {} }

  // What the address says when it opens nothing: a sentence and the links that would work.
  function say(title, rows, s) {
    let box = q("m-cardurl");
    if (!box) { box = document.createElement("div"); box.id = "m-cardurl"; document.body.appendChild(box); }
    box.hidden = false;
    box.innerHTML = "<h1>" + U.esc(title) + '</h1><p><a class="home" href="/m/">all cards</a></p><div class="cu-list">' +
      rows.map(r => r.href ? '<a class="cu-row" href="' + U.esc(r.href) + '"' + (r.id ? ' data-id="' + U.esc(r.id) + '"' : "") + ">" + U.esc(r.text) + "</a>"
        : '<span class="cu-row">' + U.esc(r.text) + "</span>").join("") + "</div>";
    if (s) box.onclick = e => { const a = e.target.closest && e.target.closest("a[data-id]"); if (a) remember(s, a.dataset.id); };
  }

  // A card is opened once the store holds it, which is after its first read. The store is asked again on every change
  // until then, and a store that has read and still lacks the card opens it to say so.
  function whenHeld(id, fn) {
    if (window.mStore.card(id) || window.mNet.loaded()) { fn(); return; }
    const off = window.mStore.on("cards", () => {
      if (window.mStore.card(id) || window.mNet.loaded()) { off(); fn(); }
    });
  }

  function openNamed(t, s) {
    remember(s, t.id);
    whenHeld(t.id, () => {
      try { history.replaceState({ mcard: t.id, direct: true }, ""); } catch (e) {}
      open(t.id, true);
    });
  }

  async function openFromPath() {
    const s = shapeOf(location.pathname);
    if (!s) return;
    const who = s.kind === "alias" ? s.alias : s.name;
    let t = null;
    try {
      t = await window.mNet.api("/v1/tasks/" + (s.kind === "alias" ? enc(s.alias) : enc(s.name) + "@" + enc(s.room)));
    } catch (e) {
      const b = e.body || {};
      const base = s.kind === "alias" ? "" : " on room " + s.room;
      if (e.status === 409) { await clash(s, b); return; }
      if (e.status !== 404) { say("could not look that up", [{ text: e.message }]); return; }
      if (s.kind === "card" && b.would_work === undefined && /no room called/.test(e.message)) {
        say("room " + s.room + " is not attached to this hub", window.mNet.rooms().map(r => ({ text: r.name, href: "/m/room/" + enc(r.name) })));
        return;
      }
      const one = window.mNet.rooms().length === 1 ? window.mNet.rooms()[0].name : "";
      say("no card called " + who + base, (b.would_work || []).map(w => {
        const m = /^(\S+?)(?:@(\S+))?(?: \(@(\S+)\))?$/.exec(w);
        if (!m) return { text: w };
        const room = m[2] || one;
        return { text: w, href: m[3] ? "/m/alias/" + enc(m[3]) : room ? "/m/room/" + enc(room) + "/" + enc(m[1]) : "" };
      }), null);
      return;
    }
    openNamed(t, s);
  }

  // A name two rooms hold. The card opened last time wins when it is one of them. Otherwise a list, and picking one
  // remembers it for this path.
  async function clash(s, body) {
    const cands = (body.candidates || []).map(c => { const m = /^(.*)@(\S+) \((.+)\)$/.exec(String(c)); return m ? { name: m[1], room: m[2], id: m[3] } : null; }).filter(Boolean);
    const last = lastOf(s);
    const mine = last && cands.find(c => window.mNet.bareId(c.id) === last);
    if (mine) {
      try { openNamed(await window.mNet.api("/v1/tasks/" + enc(mine.id)), s); return; } catch (e) {}
    }
    const rows = [];
    for (const c of cands) {
      let t = null;
      try { t = await window.mNet.api("/v1/tasks/" + enc(c.id)); } catch (e) {}
      rows.push({ text: c.room + "  " + c.name + (t ? "  " + (t.status === "done" ? "done" : (t.activity && t.activity.what) || t.status || "") : ""),
        href: "/m/room/" + enc(c.room) + "/" + enc(c.name), id: c.id, done: !!t && t.status === "done", at: t && t.created_at || "" });
    }
    rows.sort((a, b) => (a.done - b.done) || String(b.at).localeCompare(String(a.at)));
    say("@" + (s.alias || s.name) + " is on more than one room", rows, s);
  }

  // ── the keyboard ─────────────────────────────────────────────────────────
  // The layout follows the visual viewport, so the composer stays above the keyboard where the browser does not
  // resize the layout for it (iOS).
  function followViewport() {
    const vv = window.visualViewport;
    if (!vv) return;
    const set = () => {
      const r = document.documentElement.style;
      r.setProperty("--vvh", vv.height + "px");
      r.setProperty("--vvtop", vv.offsetTop + "px");
    };
    vv.addEventListener("resize", set);
    vv.addEventListener("scroll", set);
    set();
  }

  function init() {
    els = {
      sheet: q("m-card"), scroll: q("m-card-scroll"), head: q("m-card-head"), notices: q("m-card-notices"),
      replies: q("m-replies"), extras: q("m-card-extras"), perms: q("m-perms"), compose: q("m-compose"),
      back: q("m-card-back"), term: q("m-card-term"), pick: q("m-card-pick"), menu: q("m-card-menu"),
    };
    els.back.addEventListener("click", close);
    els.pick.addEventListener("click", menuToggle);
    els.menu.addEventListener("click", e => {
      const b = e.target.closest(".pm-row");
      if (b) goTo(b.dataset.id);
    });
    window.addEventListener("popstate", e => {
      const id = e.state && e.state.mcard;
      if (id) open(id, true);
      else closeNow();
    });
    // A sheet reopened by reload or a forward step has no card behind it.
    if (history.state && history.state.mcard) {
      try { history.replaceState(null, ""); } catch (e) {}
    }
    followViewport();
    openFromPath();
  }

  window.mCard = { init, open, close, isOpen: () => !!openId, current: () => openId };
})();
