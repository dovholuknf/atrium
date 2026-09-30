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
    els.term.href = "/#term=" + encodeURIComponent(t.id);
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
    els.scroll.scrollTop = 0;
    els.head.dataset.sig = els.notices.dataset.sig = els.extras.dataset.sig = els.replies.dataset.sig = "";
    paint(true);
    mountFor(id);
    // A frame later, so the slide has a start to move from.
    requestAnimationFrame(() => requestAnimationFrame(() => els.sheet.classList.add("on")));
    document.body.classList.add("sheet-open");
    if (!fromHistory) {
      try { history.pushState({ mcard: id }, ""); } catch (e) {}
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
    if (history.state && history.state.mcard) history.back(); // popstate does the closing
    else closeNow();
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
      back: q("m-card-back"), term: q("m-card-term"),
    };
    els.back.addEventListener("click", close);
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
  }

  window.mCard = { init, open, close, isOpen: () => !!openId, current: () => openId };
})();
