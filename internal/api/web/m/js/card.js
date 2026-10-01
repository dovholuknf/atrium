// The card view: a full-height sheet opened by a tap. The browser's back button closes it, through history state, so
// the back gesture feels native.
//
// Top to bottom: the name and the Recap control, anything held or reported for the operator, the last replies as a
// conversation (with the operator's own messages among them), the open questions, the permission rows (`#m-perms`, perms.js), and the composer pinned
// under it all (`#m-compose`, compose.js). The live "working" line (`#m-working`) sits between the thread and the
// composer, where the eye lands. Those two files are another worker's, so every call to them is guarded.
(function () {
  "use strict";

  const U = window.mUtil;
  const MD = window.mMd;
  const REPLIES_N = 10;

  let openId = "";
  let els = null;
  let seq = 0;
  let turnKey = "";
  // The room's `output_at`: the last reply with text, mid-turn too. Absent keeps the last one seen.
  let outAt = "";
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
    return '<h1 class="c-name">' + U.esc(nm.main) + "</h1>" +
      (nm.sub ? '<p class="c-sub">' + U.esc(nm.sub) + "</p>" : "") +
      '<p class="c-state"><span class="pill s-' + U.esc(t.status) + '">' + U.esc(U.statusLabel(t)) + "</span>" +
      (room ? '<span class="pill room">' + U.esc(room) + "</span>" : "") + "</p>" + recapBtnHTML(t);
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

  function mdCtx() {
    const t = openId && window.mStore.card(openId);
    return { id: openId, worktree: t && t.worktree || "" };
  }

  // One header line per message: who, how long ago, and the delivery state when there is one.
  function agoOf(at) {
    const a = U.ago(Date.now() - U.ts(at));
    return a === "now" ? "now" : a;
  }
  function headerHTML(who, at, tag, note) {
    return '<header class="rh"><span class="src">' + U.esc(who) + "</span>" + (note ? '<span class="note">' + U.esc(note) + "</span>" : "") +
      (at ? '<time datetime="' + U.esc(at) + '">' + U.esc(agoOf(at)) + "</time>" : "") + (tag ? '<b class="tag">' + U.esc(tag) + "</b>" : "") + "</header>";
  }
  function whoOf(t) {
    const nm = U.cardName(t);
    const room = window.mNet.roomOf(t.id) || t.room || "";
    return nm.main + (room ? "@" + room : "");
  }
  function replyHTML(r, screen, who) {
    const body = screen ? '<pre class="screen">' + U.esc(r.text) + "</pre>" : '<div class="md">' + MD.render(r.text, mdCtx()) + "</div>";
    return '<article class="reply' + (screen ? " from-screen" : "") + '">' + headerHTML(who, r.at, "", screen ? "from the screen" : "") + body +
      (r.truncated ? '<p class="cut">cut short here. the rest is in the terminal</p>' : "") + "</article>";
  }

  function fallbackHTML(t) {
    const recap = String(t.recap || "").trim();
    return recap ? "" : '<p class="quiet">Nothing to read here yet.</p>';
  }

  function clock(iso) {
    const n = U.ts(iso);
    if (!n) return "";
    return new Date(n).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
  }

  // A recap older than the card's last turn describes a turn that is over.
  function recapStale(t) {
    const r = U.ts(t.recap_at), e = U.ts(turnOf(t));
    return !!(r && e && r < e);
  }

  function recapBtnHTML(t) {
    if (!String(t.recap || "").trim()) return "";
    const at = clock(t.recap_at);
    return '<button type="button" class="recap-btn' + (recapStale(t) ? " stale" : "") + '" id="m-recap-open" aria-haspopup="dialog">Recap' +
      (at ? "<em>" + U.esc(at) + "</em>" : "") + "</button>";
  }

  function recapSheetHTML(t) {
    const recap = String(t.recap || "").trim();
    if (!recap) return "";
    const at = clock(t.recap_at);
    const stale = recapStale(t);
    return '<div class="recap-back" id="m-recap-back"></div><div class="recap-sheet" role="dialog" aria-label="Recap">' +
      '<div class="recap-top"><h2>Recap</h2>' + (at ? '<span class="recap-at">from ' + U.esc(at) + (stale ? ", before the last turn" : "") + "</span>" : "") +
      '<button type="button" class="recap-x" id="m-recap-close" aria-label="close">&times;</button></div>' +
      '<div class="md' + (stale ? " stale" : "") + '">' + MD.render(recap, mdCtx()) + "</div></div>";
  }

  function openRecap() {
    if (!els || !els.recap.innerHTML) return;
    els.recap.hidden = false;
  }
  function closeRecap() { if (!els) return; els.recap.hidden = true; stick = true; settle(); }

  // ── the working line ─────────────────────────────────────────────────────
  // What the card is doing right now, from the activity on its task row. Nothing when it is idle or waiting.
  function workingOf(t) {
    if (!t || t.status !== "running") return "";
    const a = t.activity || {};
    if (a.what === "idle" || a.dialog) return "";
    if (a.what === "tool") return a.tool || "a tool";
    return a.what && a.what !== "thinking" ? String(a.what) : "thinking";
  }

  // The one small row above the composer: how the last message went, and what the card is doing, on one line.
  function sentOf(t) {
    const bare = window.mNet.bareId(t.id);
    const mine = readSent(t.id).concat(flight.get(bare) || []).sort((x, y) => U.ts(x.at) - U.ts(y.at)).pop();
    if (!mine) return "";
    const got = cache.get(openId);
    const reps = got && got.replies || [];
    if (reps.length && U.ts(reps[reps.length - 1].at) > U.ts(mine.at)) return "";
    const st = mine.state || mine.kind;
    return st === "pending" ? "sending" : st === "failed" ? "not sent" : st === "queued" ? "queued" : "sent";
  }

  function paintWorking(t) {
    const w = workingOf(t), sent = sentOf(t);
    const tool = t.activity && t.activity.what === "tool";
    const sig = sent + "|" + (w ? (tool ? "tool:" : "") + w : "");
    if (els.working.dataset.sig === sig) return;
    els.working.dataset.sig = sig;
    els.working.hidden = !w && !sent;
    els.working.innerHTML = (sent ? '<span class="wk-sent">' + U.esc(sent) + "</span>" : "") + (sent && w ? '<span class="wk-dot">&middot;</span>' : "") +
      (w ? '<span class="wk-spin" aria-hidden="true"></span><span class="wk-what">' + (tool ? '<span class="wk-verb">running</span> ' : "") + U.esc(w) + "</span>" : "");
  }

  // ── what the operator sent ───────────────────────────────────────────────
  // The replies endpoint returns the session's own text only, so the operator's messages are kept on this device, per
  // card, from what compose.js sent, and put among the replies by time.
  const SENT = "atrium.msent.";
  const SENT_KEEP = 20;
  function sentKey(id) { return SENT + window.mNet.bareId(id); }
  function readSent(id) {
    try { const a = JSON.parse(localStorage.getItem(sentKey(id)) || "[]"); return Array.isArray(a) ? a : []; } catch (e) { return []; }
  }
  // Each message is cut at about 4 KB so a pasted log cannot fill the origin's quota, and what is kept is pruned on
  // every write: messages past a week go, and only the newest 50 cards keep any.
  const SENT_CUT = 4096;
  const SENT_AGE = 7 * 24 * 3600 * 1000;
  const SENT_CARDS = 50;
  function pruneSent() {
    try {
      const rows = [];
      for (let i = 0; i < localStorage.length; i++) {
        const k = localStorage.key(i);
        if (k && k.indexOf(SENT) === 0) rows.push(k);
      }
      const now = Date.now();
      const live = [];
      rows.forEach(k => {
        let a = [];
        try { a = JSON.parse(localStorage.getItem(k) || "[]"); } catch (e) {}
        a = Array.isArray(a) ? a.filter(m => m && now - U.ts(m.at) < SENT_AGE) : [];
        if (!a.length) { localStorage.removeItem(k); return; }
        live.push({ k, last: U.ts(a[a.length - 1].at) });
        localStorage.setItem(k, JSON.stringify(a));
      });
      live.sort((x, y) => y.last - x.last).slice(SENT_CARDS).forEach(r => localStorage.removeItem(r.k));
    } catch (e) {}
  }
  function noteSent(id, text, kind) {
    if (!id || !text) return;
    const a = readSent(id);
    let t = String(text);
    if (t.length > SENT_CUT) t = t.slice(0, SENT_CUT) + "\n[cut here, the rest was sent but is not kept]";
    a.push({ at: new Date().toISOString(), text: t, kind: kind === "queued" ? "queued" : "sent" });
    try { localStorage.setItem(sentKey(id), JSON.stringify(a.slice(-SENT_KEEP))); } catch (e) {}
    pruneSent();
    if (isOpen(id)) { stick = true; paintReplies(); paintWorkingNow(); }
  }
  function isOpen(id) { return !!openId && window.mNet.bareId(id) === window.mNet.bareId(openId); }
  window.addEventListener("m-sent", e => { const d = e.detail || {}; noteSent(d.id, d.text, d.kind); });

  // A message on its way, in memory only: pending while the room has not answered, and not sent when it failed, with
  // its text back in the composer. An answered one becomes the kept row above, carrying delivered or queued.
  const flight = new Map();
  function paintWorkingNow() { const t = openId && window.mStore.card(openId); if (t && els) paintWorking(t); }
  window.addEventListener("m-send", e => {
    const d = e.detail || {};
    if (!d.id || !d.key) return;
    const bare = window.mNet.bareId(d.id);
    const list = (flight.get(bare) || []).filter(x => x.key !== d.key && !(x.state === "failed" && x.text === d.text));
    if (d.state === "pending" || d.state === "failed") list.push({ key: d.key, text: d.text, state: d.state, at: new Date().toISOString() });
    flight.set(bare, list);
    if (isOpen(d.id)) { if (d.state === "pending") stick = true; paintReplies(); paintWorkingNow(); }
  });

  const OWN_TAG = { pending: "sending", failed: "not sent, back in the box", sent: "delivered", queued: "queued" };
  function ownHTML(m) {
    const st = m.state || m.kind || "";
    return '<article class="reply mine' + (st === "pending" || st === "failed" ? " " + st : "") + '">' + headerHTML("you", m.at, OWN_TAG[st] || "") +
      '<div class="own">' + U.esc(m.text) + "</div></article>";
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

  // The operator's messages from the oldest reply shown on, in time order with the replies.
  function mergeOwn(t, items, since) {
    const own = readSent(t.id).concat(flight.get(window.mNet.bareId(t.id)) || []).filter(m => U.ts(m.at) >= since).map(m => ({ mine: m, at: U.ts(m.at) }));
    return items.concat(own).sort((a, b) => a.at - b.at);
  }

  // What was typed into the session, from the replies endpoint. The operator's own are "you", a peer's are quieter and
  // carry the sender's name, a command is one small line. Absent `prompts` leaves the thread as it was.
  function promptHTML(p) {
    const cut = p.truncated ? '<p class="cut">cut</p>' : "";
    const text = String(p.text || "");
    const body = t => '<div class="own">' + U.esc(t) + "</div>" + cut + "</article>";
    if (p.kind === "command") return '<article class="reply prompt command">' + headerHTML("command", p.at, "") + body(text);
    if (p.kind === "peer") {
      const m = /^\[atrium\] (.+?) says:\s*/.exec(text);
      return '<article class="reply prompt peer">' + headerHTML(m ? m[1] : "a peer", p.at, "") + body(m ? text.slice(m[0].length) : text);
    }
    return '<article class="reply mine prompt">' + headerHTML("you", p.at, "") + body(text);
  }
  const sameText = (a, b) => String(a).trim().slice(0, 200) === String(b).trim().slice(0, 200);

  function repliesHTML(t, got) {
    const who = whoOf(t);
    if (!got) return '<div class="replies loading" aria-busy="true"><div class="sk"></div><div class="sk s2"></div></div>';
    if (got.failed) {
      // The endpoint is not on this room yet, or it failed. The recap and the last report stand in for it.
      const rep = t.reported_at ? '<p class="quiet">Last report ' + U.esc(U.ago(Date.now() - U.ts(t.reported_at))) +
        " ago" + (t.report_sha ? ", commit " + U.esc(String(t.report_sha).slice(0, 7)) : "") + ".</p>" : "";
      return '<div class="replies">' + rep + fallbackHTML(t) + mergeOwn(t, [], 0).map(i => ownHTML(i.mine)).join("") + "</div>";
    }
    const screen = got.source === "screen";
    const list = got.replies || [];
    const prompts = Array.isArray(got.prompts) ? got.prompts.filter(p => p && p.at) : [];
    const since = list.length ? U.ts(list[0].at) : 0;
    const ops = prompts.filter(p => p.kind !== "peer" && p.kind !== "command");
    // A local own row goes once the matching operator prompt has arrived, so nothing shows twice.
    const items = mergeOwn(t, list.map(r => ({ r, at: U.ts(r.at) })), since)
      .filter(i => !i.mine || !ops.some(p => sameText(p.text, i.mine.text) && Math.abs(U.ts(p.at) - U.ts(i.mine.at)) <= 60000))
      .concat(prompts.filter(p => U.ts(p.at) >= since).map(p => ({ p, at: U.ts(p.at) })))
      .sort((a, b) => a.at - b.at);
    if (!items.length) return '<div class="replies">' + fallbackHTML(t) + "</div>";
    return '<div class="replies">' + items.map(i => i.p ? promptHTML(i.p) : i.mine ? ownHTML(i.mine) : replyHTML(i.r, screen, who)).join("") + "</div>";
  }

  // ── painting ─────────────────────────────────────────────────────────────
  function paint(full) {
    if (!els || !openId) return;
    const t = window.mStore.card(openId);
    if (!t) {
      els.head.innerHTML = '<h1 class="c-name">This card is gone</h1><p class="c-sub">It was removed or its room went away.</p>';
      els.notices.innerHTML = els.replies.innerHTML = els.extras.innerHTML = "";
      els.working.hidden = true;
      els.working.dataset.sig = "";
      return;
    }
    const head = headHTML(t);
    if (els.head.dataset.sig !== head) { els.head.innerHTML = head; els.head.dataset.sig = head; }
    const not = noticesHTML(t);
    if (els.notices.dataset.sig !== not) { els.notices.innerHTML = not; els.notices.dataset.sig = not; }
    const ex = questionsHTML(t);
    if (els.extras.dataset.sig !== ex) { els.extras.innerHTML = ex; els.extras.dataset.sig = ex; }
    const rc = recapSheetHTML(t);
    if (els.recap.dataset.sig !== rc) {
      els.recap.innerHTML = rc;
      els.recap.dataset.sig = rc;
      if (!rc) els.recap.hidden = true;
    }
    paintWorking(t);
    settle();
    els.term.href = pathFor(t, "") || "/#term=" + encodeURIComponent(t.id);
    if (full) paintReplies();
  }

  // ── staying at the newest message ────────────────────────────────────────
  // The thread follows its end until the operator scrolls up on purpose, then a jump control brings it back.
  let stick = true;
  const NEAR = 80;
  function toEnd() {
    if (!els) return;
    els.scroll.scrollTop = els.scroll.scrollHeight;
    els.jump.hidden = true;
  }
  function settle() {
    if (!stick || !els) return;
    toEnd();
    requestAnimationFrame(() => { if (stick && els) toEnd(); });
  }
  // Only a scroll the operator made lets go of the end. Layout growth, a restored position or scroll anchoring also
  // fire scroll events, and none of them is a decision to stop following. A touch, wheel, key or drag marks the
  // scrolls that follow it as the operator's, and a finger's momentum is covered by the window after it.
  let userAt = 0;
  // ── pinch is text size ───────────────────────────────────────────────────
  // The browser's own pinch is off (viewport and touch-action). Two fingers on the thread change --m-fs, the message
  // font size, within 11 to 24 px, kept on this device. The text reflows, the header, composer and buttons keep their
  // size, and the message under the fingers stays where it is.
  const FS_KEY = "atrium.mfs", FS_MIN = 11, FS_MAX = 24, FS_DEF = 14;
  function setFs(px) {
    const v = Math.max(FS_MIN, Math.min(FS_MAX, Math.round(px * 10) / 10));
    q("m-card").style.setProperty("--m-fs", v + "px");
    return v;
  }
  function pinchInit() {
    try { const v = parseFloat(localStorage.getItem(FS_KEY)); if (v) setFs(v); } catch (e) {}
    const dist = t => Math.hypot(t[0].clientX - t[1].clientX, t[0].clientY - t[1].clientY);
    let g = null;
    els.scroll.addEventListener("touchstart", e => {
      if (e.touches.length !== 2) { g = null; return; }
      const t = e.touches, my = (t[0].clientY + t[1].clientY) / 2;
      const at = document.elementFromPoint((t[0].clientX + t[1].clientX) / 2, my);
      const anchor = at && at.closest && at.closest(".reply");
      g = { d: dist(t), fs: parseFloat(getComputedStyle(q("m-card")).getPropertyValue("--m-fs")) || FS_DEF, anchor, y: my };
      stick = false;
    }, { passive: true });
    els.scroll.addEventListener("touchmove", e => {
      if (!g || e.touches.length !== 2) return;
      e.preventDefault();
      const was = g.anchor ? g.anchor.getBoundingClientRect().top : 0;
      const v = setFs(g.fs * dist(e.touches) / (g.d || 1));
      if (g.anchor) els.scroll.scrollTop += g.anchor.getBoundingClientRect().top - was;
      g.last = v;
    }, { passive: false });
    const end = () => { if (g && g.last) { try { localStorage.setItem(FS_KEY, String(g.last)); } catch (e) {} } g = null; };
    els.scroll.addEventListener("touchend", end);
    els.scroll.addEventListener("touchcancel", end);
    ["gesturestart", "gesturechange"].forEach(n => els.scroll.addEventListener(n, e => e.preventDefault()));
  }

  function byHand() { userAt = Date.now(); }
  function onScroll() {
    if (!els || !openId) return;
    const gap = els.scroll.scrollHeight - els.scroll.scrollTop - els.scroll.clientHeight;
    if (gap < NEAR) { stick = true; els.jump.hidden = true; return; }
    if (Date.now() - userAt < 1500) { stick = false; els.jump.hidden = false; }
  }

  function paintReplies() {
    const t = window.mStore.card(openId);
    if (!t) return;
    const html = repliesHTML(t, cache.get(openId));
    if (els.replies.dataset.sig === html) { settle(); return; }
    const first = !els.replies.dataset.sig;
    els.replies.innerHTML = html;
    els.replies.dataset.sig = html;
    if (!first) els.replies.classList.add("fresh");
    setTimeout(() => els.replies.classList.remove("fresh"), 400);
    settle();
  }

  // The last replies. Read on open, when the card's turn ends and when its `output_at` moves, never on a timer. The working line follows the task
  // events, through the store.
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
    const t = window.mStore.card(openId);
    paint(false);
    const key = openId + "@" + turnOf(t);
    const out = (t && t.output_at) || "";
    const moved = !!out && out !== outAt;
    if (out) outAt = out;
    if (key !== turnKey || moved) { turnKey = key; loadReplies(); }
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
    outAt = (window.mStore.card(id) || {}).output_at || "";
    els.sheet.hidden = false;
    menuClose();
    els.sheet.classList.toggle("direct", !!(history.state && history.state.direct));
    els.scroll.scrollTop = 0;
    els.head.dataset.sig = els.notices.dataset.sig = els.extras.dataset.sig = els.replies.dataset.sig = els.recap.dataset.sig = els.working.dataset.sig = "";
    els.recap.hidden = true;
    stick = true;
    els.jump.hidden = true;
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
    if (MD.release) MD.release();
    els.sheet.hidden = true;
    els.head.innerHTML = els.notices.innerHTML = els.replies.innerHTML = els.extras.innerHTML = els.recap.innerHTML = "";
    els.working.hidden = true;
    els.recap.hidden = true;
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
    // `/m/#term=<id>` is where the board sends a card it only knows by id.
    const hm = /^#term=(.+)$/.exec(location.hash);
    if (hm && !shapeOf(location.pathname)) {
      const id = dec(hm[1]);
      try { history.replaceState({ mcard: id, direct: true }, "", "/m/"); } catch (e) {}
      whenHeld(id, () => open(id, true));
      return;
    }
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
      back: q("m-card-back"), term: q("m-card-term"), pick: q("m-card-pick"), menu: q("m-card-menu"), working: q("m-working"), recap: q("m-recap"),
      jump: q("m-jump"),
    };
    els.scroll.addEventListener("scroll", onScroll, { passive: true });
    ["wheel", "touchstart", "touchmove", "pointerdown", "keydown"].forEach(n => els.scroll.addEventListener(n, byHand, { passive: true }));
    // The first open lays out late: the Recap button, the permission rows, fonts and any image in a reply move the end
    // after the first scroll. While following, every size change of the thread and the box around it goes to the end again.
    if (window.ResizeObserver) {
      const ro = new ResizeObserver(() => { if (stick && openId) toEnd(); });
      [els.scroll, els.head, els.notices, els.replies, els.extras, els.perms].forEach(e => ro.observe(e));
    }
    pinchInit();
    els.scroll.addEventListener("load", () => { if (stick && openId) toEnd(); }, true);
    els.jump.addEventListener("click", () => { stick = true; toEnd(); });
    if (window.visualViewport) window.visualViewport.addEventListener("resize", () => { if (openId) settle(); });
    els.head.addEventListener("click", e => { if (e.target.closest && e.target.closest("#m-recap-open")) openRecap(); });
    els.recap.addEventListener("click", e => {
      if (e.target.id === "m-recap-back" || (e.target.closest && e.target.closest("#m-recap-close"))) closeRecap();
    });
    document.addEventListener("keydown", e => { if (e.key === "Escape" && els && !els.recap.hidden) closeRecap(); });
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
