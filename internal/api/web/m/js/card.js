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
  const REPLIES_N = 50;
  const OLDER_N = 50;

  let openId = "";
  let els = null;
  let seq = 0;
  let closeTok = 0;
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
  // A stable key for a drawn entry, so older pages can be deduped against what is on screen and the viewport can find
  // the bubble it was anchored to after a prepend or a redraw.
  function entryKey(at, text) {
    const t = String(at || "") + "|" + String(text || "");
    let h = 5381;
    for (let i = 0; i < t.length; i++) h = ((h << 5) + h + t.charCodeAt(i)) | 0;
    return String(at || "") + "~" + (h >>> 0).toString(36) + t.length;
  }
  // The text each drawn bubble was made from, by its key, for copy and reply: the source as written, not the rendered markup.
  const bubbleText = new Map();
  const ACTS = '<span class="acts"><button type="button" class="bact" data-act="copy">copy</button><button type="button" class="bact" data-act="reply">reply</button></span>';
  function headerHTML(who, at, tag, note, text, key) {
    if (key) bubbleText.set(key, String(text || ""));
    return '<header class="rh"><span class="src">' + U.esc(who) + "</span>" + (note ? '<span class="note">' + U.esc(note) + "</span>" : "") +
      (at ? '<time datetime="' + U.esc(at) + '">' + U.esc(agoOf(at)) + "</time>" : "") + (tag ? '<b class="tag">' + U.esc(tag) + "</b>" : "") + (key ? ACTS : "") + "</header>";
  }
  function whoOf(t) {
    const nm = U.cardName(t);
    const room = window.mNet.roomOf(t.id) || t.room || "";
    return nm.main + (room ? "@" + room : "");
  }
  function replyHTML(r, screen, who) {
    const body = screen ? '<pre class="screen">' + U.esc(r.text) + "</pre>" : '<div class="md">' + MD.render(r.text, mdCtx()) + "</div>";
    const k = entryKey(r.at, r.text);
    return '<article class="reply' + (screen ? " from-screen" : "") + '" data-k="' + U.esc(k) + '">' + headerHTML(who, r.at, "", screen ? "from the screen" : "", r.text, k) + body +
      (r.truncated ? '<p class="cut">cut short here. the rest is in the terminal</p>' : "") +
      // How many files this turn's edit calls named, from /replies and with no git number: the diff is read when this is tapped.
      (r.edited > 0 ? '<button type="button" class="chg-chip" data-at="' + U.esc(r.at) + '">' + r.edited + (r.edited === 1 ? " file" : " files") + " edited</button>" : "") + "</article>";
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

  // The recap sheet covers the thread. Closing it gives back what the reader had: following if they were, the place they
  // were reading if not.
  let recapStick = true;
  function openRecap() {
    if (!els || !els.recap.innerHTML) return;
    recapStick = stick;
    els.recap.hidden = false;
  }
  function closeRecap() { if (!els) return; els.recap.hidden = true; stick = recapStick; syncAnchor(); settle(); }

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
    if (isOpen(id)) { stick = true; syncAnchor(); paintReplies(); paintWorkingNow(); toEnd(true); }
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
    if (isOpen(d.id)) { if (d.state === "pending") { stick = true; syncAnchor(); } paintReplies(); paintWorkingNow(); if (d.state === "pending") toEnd(true); }
  });

  const OWN_TAG = { pending: "sending", failed: "not sent, back in the box", sent: "delivered", queued: "queued" };
  function ownHTML(m) {
    const st = m.state || m.kind || "";
    const k = entryKey(m.at, m.text);
    return '<article class="reply mine' + (st === "pending" || st === "failed" ? " " + st : "") + '" data-k="' + U.esc(k) + '">' + headerHTML("you", m.at, OWN_TAG[st] || "", "", m.text, k) +
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
    const k = entryKey(p.at, p.text), dk = ' data-k="' + U.esc(k) + '"';
    const body = t => '<div class="own">' + U.esc(t) + "</div>" + cut + "</article>";
    if (p.kind === "command") return '<article class="reply prompt command"' + dk + '>' + headerHTML("command", p.at, "", "", text, k) + body(text);
    if (p.kind === "peer") {
      const m = /^\[atrium\] (.+?) says:\s*/.exec(text);
      return '<article class="reply prompt peer"' + dk + '>' + headerHTML(m ? m[1] : "a peer", p.at, "", "", m ? text.slice(m[0].length) : text, k) + body(m ? text.slice(m[0].length) : text);
    }
    return '<article class="reply mine prompt"' + dk + '>' + headerHTML("you", p.at, "", "", text, k) + body(text);
  }
  const sameText = (a, b) => String(a).trim().slice(0, 200) === String(b).trim().slice(0, 200);

  // ── older replies ──────────────────────────────────────────────────────
  // Per card: the pages read before the latest window, the room's cursor for the next one, and whether a read is in
  // flight or failed. The cursor is passed back exactly as the room gave it, never worked out from a timestamp.
  const olderOf = new Map();
  function older(id) {
    const b = window.mNet.bareId(id);
    if (!olderOf.has(b)) olderOf.set(b, { replies: [], prompts: [], more: false, next: "", pages: 0, busy: false, failed: false });
    return olderOf.get(b);
  }
  function foldInto(o, win) {
    const seen = new Set();
    const merge = list => list.filter(x => { const k = entryKey(x.at, x.text); if (seen.has(k)) return false; seen.add(k); return true; }).sort((a, b) => U.ts(a.at) - U.ts(b.at));
    o.replies = merge((win.replies || []).concat(o.replies));
    seen.clear();
    o.prompts = merge((Array.isArray(win.prompts) ? win.prompts : []).concat(o.prompts));
  }
  // The latest window with every older page in front of it, in time order, nothing twice.
  function withOlder(id, got) {
    const o = older(id);
    if (!o.replies.length && !o.prompts.length) return got;
    const seen = new Set();
    const uniq = (list) => list.filter(x => { const k = entryKey(x.at, x.text); if (seen.has(k)) return false; seen.add(k); return true; });
    const rs = uniq((got.replies || []).slice().concat(o.replies).sort((a, b) => U.ts(a.at) - U.ts(b.at)));
    seen.clear();
    const ps = uniq((Array.isArray(got.prompts) ? got.prompts : []).slice().concat(o.prompts).sort((a, b) => U.ts(a.at) - U.ts(b.at)));
    return Object.assign({}, got, { replies: rs, prompts: ps });
  }
  function olderRowHTML(id) {
    const o = older(id);
    if (!o.more) return "";
    if (o.busy) return '<button type="button" id="m-older" class="older busy" disabled>loading older&hellip;</button>';
    if (o.failed) return '<button type="button" id="m-older" class="older failed">could not load older. tap to retry</button>';
    return '<button type="button" id="m-older" class="older">load older</button>';
  }

  function repliesHTML(t, got) {
    const who = whoOf(t);
    if (got && !got.failed) got = withOlder(openId, got);
    const row = got && !got.failed ? olderRowHTML(openId) : "";
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
    if (!items.length) return '<div class="replies">' + row + fallbackHTML(t) + "</div>";
    return '<div class="replies">' + row + items.map(i => i.p ? promptHTML(i.p) : i.mine ? ownHTML(i.mine) : replyHTML(i.r, screen, who)).join("") + "</div>";
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
  // THE RULE. The thread follows its end only while the reader is at the very bottom. The moment a finger, wheel, key or
  // pointer touches the thread, or it is more than LEAVE px above the bottom, following STOPS and stays stopped until
  // the reader is back at the very bottom (within AT_END px) themselves or taps the jump arrow. Nothing the page does
  // moves a reader: output arriving, a reply re-read, the activity row, images, the pinch size. And nothing scrolls the
  // thread while a touch is down, for about 300 ms after it lifts (a finger's momentum), or while a scroll is running.
  let stick = true;
  const LEAVE = 24, AT_END = 2, MOMENTUM = 300;
  let touching = false, touchEndAt = 0, lastScrollAt = 0, progUntil = 0, progAt = 0, lastTop = 0, progTop = -1;
  function active() { const n = Date.now(); return touching || n - touchEndAt < MOMENTUM || n - lastScrollAt < 120; }
  // Scroll anchoring is the browser's own way of holding a reader still when something above them grows (an image
  // loading, a bubble reflowing). It is on while not following and off while following, where toEnd does the job.
  function syncAnchor() { if (els) els.scroll.style.overflowAnchor = stick ? "none" : "auto"; }
  function gapOf() { return els.scroll.scrollHeight - els.scroll.scrollTop - els.scroll.clientHeight; }
  // `force` is for what the reader asked for: the jump arrow, their own message going out.
  // Every move the page makes itself goes through here, so its scroll event is not mistaken for the reader's.
  function setTop(v) { progAt = Date.now(); progUntil = progAt + 80; els.scroll.scrollTop = v; lastTop = progTop = els.scroll.scrollTop; }
  function toEnd(force) {
    if (!els) return;
    if (!force && (!stick || active())) return;
    setTop(els.scroll.scrollHeight);
    els.jump.hidden = true;
  }
  // While a message is being typed the thread holds still. The composer growing a line, the keyboard coming or going
  // and every keystroke change the room around the thread, and none of them is a reason to move it. The place it had
  // when typing began (or where the operator last scrolled it to) is put back after each of those, and stick-to-bottom
  // is not run. Sending, the jump control, leaving the box and emptying it end it, and the next input pins again.
  let typing = false, pinTop = 0;
  function pinned() { if (typing && els && !active() && els.scroll.scrollTop !== pinTop) setTop(pinTop); return typing; }
  function typingOn() { if (!typing && els) { typing = true; pinTop = els.scroll.scrollTop; } }
  function typingOff() { typing = false; }
  let settleLater = 0;
  function settle() {
    if (pinned()) return;
    if (!stick || !els) return;
    if (active()) { if (!settleLater) settleLater = setTimeout(() => { settleLater = 0; settle(); }, 130); return; }
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
      if (g.anchor) setTop(els.scroll.scrollTop + g.anchor.getBoundingClientRect().top - was);
      g.last = v;
    }, { passive: false });
    const end = () => { if (g && g.last) { try { localStorage.setItem(FS_KEY, String(g.last)); } catch (e) {} } g = null; };
    els.scroll.addEventListener("touchend", end);
    els.scroll.addEventListener("touchcancel", end);
    ["gesturestart", "gesturechange"].forEach(n => els.scroll.addEventListener(n, e => e.preventDefault()));
  }

  // The same pinch on another scroller that sits inside the card, the file viewer's, with no message to hold in place.
  function bindPinch(el) {
    const dist = t => Math.hypot(t[0].clientX - t[1].clientX, t[0].clientY - t[1].clientY);
    let g = null;
    el.addEventListener("touchstart", e => {
      if (e.touches.length !== 2) { g = null; return; }
      g = { d: dist(e.touches), fs: parseFloat(getComputedStyle(q("m-card")).getPropertyValue("--m-fs")) || FS_DEF };
    }, { passive: true });
    el.addEventListener("touchmove", e => {
      if (!g || e.touches.length !== 2) return;
      e.preventDefault();
      g.last = setFs(g.fs * dist(e.touches) / (g.d || 1));
    }, { passive: false });
    const end = () => { if (g && g.last) { try { localStorage.setItem(FS_KEY, String(g.last)); } catch (e) {} } g = null; };
    el.addEventListener("touchend", end);
    el.addEventListener("touchcancel", end);
    ["gesturestart", "gesturechange"].forEach(n => el.addEventListener(n, e => e.preventDefault()));
  }

  // A touch, pointer, wheel or key in the thread lets go of the end at once, before any scroll event.
  // ── pull down at the very top to reload ──────────────────────────────────
  // The thread keeps the browser's own pull-to-refresh out (overscroll is contained so a drag at its top does not chain to
  // the page), so the card has its own: a single finger that goes down at scrollTop 0 and is dragged down far enough
  // reloads the page. Never from mid-thread, never while older entries are being read, never with a sheet or the picker
  // open, and it never moves the thread, so it cannot meet the follow rules.
  const PULL_AT = 80;
  function olderBusy() { return typeof older === "function" && !!openId && older(openId).busy; }
  // A message going out or a file going up is lost by a reload: the box is already empty and nothing is left to retry.
  function sending() {
    const pending = (flight.get(window.mNet.bareId(openId)) || []).some(x => x.state === "pending");
    return pending || !!(window.mCompose && window.mCompose.busy && window.mCompose.busy());
  }
  function pullBlocked() { return olderBusy() || sending(); }
  function pullInit() {
    const ind = q("m-pull");
    if (!ind) return;
    let g = null;
    const reset = () => { g = null; ind.hidden = true; ind.classList.remove("go"); ind.style.removeProperty("--pull"); ind.style.transform = ""; };
    els.scroll.addEventListener("touchstart", e => {
      g = null;
      if (e.touches.length !== 1 || els.scroll.scrollTop > 0 || pullBlocked() || !els.recap.hidden || !els.menu.hidden || typing) return;
      g = { y: e.touches[0].clientY, d: 0 };
    }, { passive: true });
    els.scroll.addEventListener("touchmove", e => {
      if (!g) return;
      if (e.touches.length !== 1 || els.scroll.scrollTop > 0 || pullBlocked()) { reset(); return; }
      const dy = e.touches[0].clientY - g.y;
      if (dy <= 0) { g.d = 0; ind.hidden = true; return; }
      g.d = dy;
      e.preventDefault();
      ind.hidden = false;
      ind.style.setProperty("--pull", String(Math.min(100, Math.round(dy / PULL_AT * 100))));
      ind.style.transform = "translateY(" + Math.min(dy, PULL_AT + 20) * 0.6 + "px)";
      ind.classList.toggle("go", dy >= PULL_AT);
    }, { passive: false });
    const end = () => {
      const go = g && g.d >= PULL_AT && els.scroll.scrollTop <= 0 && !pullBlocked();
      if (go) { ind.classList.add("go"); setTimeout(() => location.reload(), 120); } else reset();
    };
    els.scroll.addEventListener("touchend", end);
    els.scroll.addEventListener("touchcancel", reset);
  }

  // `touching` follows touch events and a mouse or pen button, never a touch's pointer events: the browser cancels a touch's
  // pointer when it takes the drag over for scrolling (pointercancel), which is exactly when the finger is still down.
  function byHand(e) {
    userAt = Date.now();
    if (!els) return;
    if (e && (e.type === "touchstart" || (e.type === "pointerdown" && e.pointerType !== "touch"))) touching = true;
    stick = false;
    syncAnchor();
    if (gapOf() > AT_END) els.jump.hidden = false;
  }
  function byHandEnd(e) {
    if (e && e.pointerType === "touch") return;
    // A finger lifting while another is still down is not the end of the touch.
    if (e && (e.type === "touchend" || e.type === "touchcancel") && e.touches && e.touches.length) return;
    touching = false; touchEndAt = Date.now(); setTimeout(onScroll, MOMENTUM + 20);
  }
  function onScroll() {
    if (!els || !openId) return;
    // A scroll event the page made itself is not the reader's. It does not make the thread look busy and does not
    // let go of the end. A reader's scroll up does: past LEAVE px from the bottom.
    // On a slow machine the event of our own scroll can arrive after the short window, so it is also known by the place
    // it left the thread at: an event that finds the thread exactly where the page put it is the page's, not the reader's.
    const mine = userAt <= progAt && (Date.now() < progUntil || Math.abs(els.scroll.scrollTop - progTop) < 1);
    const moved = els.scroll.scrollTop - lastTop;
    lastTop = els.scroll.scrollTop;
    if (mine) return;
    lastScrollAt = Date.now();
    if (els.scroll.scrollTop < 40) nearTop();
    if (typing) { if (active() || Date.now() - userAt < 1500) pinTop = els.scroll.scrollTop; else pinned(); }
    const gap = gapOf();
    // Only a reader lets go of the end by scrolling. The browser nudges scrollTop by a pixel or so as layout settles,
    // and when a reply has just landed that nudge sees a big gap: it is not a reader scrolling up.
    const reader = touching || Date.now() - userAt < 1500;
    if (gap > LEAVE && moved < -1 && reader) { stick = false; els.jump.hidden = false; }
    else if (gap > LEAVE && !stick) els.jump.hidden = false;
    else if (gap <= AT_END && !touching) { stick = true; els.jump.hidden = true; }
    syncAnchor();
  }

  // The thread is redrawn only when nobody's finger is on it, and a reader keeps the bubble they were reading where it
  // was: the first bubble in view is found again after the redraw and the scroll is moved by exactly what it shifted.
  let paintLater = 0, heldBySel = false;
  function selecting() {
    const s = window.getSelection && window.getSelection();
    return !!(s && !s.isCollapsed && s.rangeCount && els && els.replies.contains(s.anchorNode) && String(s).length);
  }
  // Copy: the bubble's own text to the clipboard, through the async API where the page may use it, else a hidden textarea
  // and execCommand. The button says "copied" for a moment.
  function copyText(text) {
    const old = () => {
      const ta = document.createElement("textarea");
      ta.value = text; ta.setAttribute("readonly", ""); ta.style.cssText = "position:fixed;top:0;left:0;opacity:0;pointer-events:none";
      const keep = window.getSelection && window.getSelection().rangeCount ? window.getSelection().getRangeAt(0) : null;
      document.body.appendChild(ta); ta.select(); ta.setSelectionRange(0, text.length);
      let ok = false;
      try { ok = document.execCommand("copy"); } catch (e) {}
      ta.remove();
      if (keep) { const s = window.getSelection(); s.removeAllRanges(); s.addRange(keep); }
      return ok;
    };
    if (navigator.clipboard && navigator.clipboard.writeText) return navigator.clipboard.writeText(text).then(() => true, () => old());
    return Promise.resolve(old());
  }
  function bubbleAct(e) {
    const b = e.target.closest && e.target.closest(".bact");
    if (!b) return;
    const art = b.closest("[data-k]");
    const text = art ? bubbleText.get(art.dataset.k) : "";
    if (!text) return;
    if (b.dataset.act === "reply") { if (window.mCompose && window.mCompose.quote) window.mCompose.quote(text); return; }
    copyText(text).then(ok => {
      if (!b.isConnected) return;
      b.textContent = ok ? "copied" : "not copied"; b.classList.add("done");
      setTimeout(() => { b.textContent = "copy"; b.classList.remove("done"); }, 1400);
    });
  }
  function paintReplies() {
    const t = window.mStore.card(openId);
    if (!t) return;
    const html = repliesHTML(t, cache.get(openId));
    if (els.replies.dataset.sig === html) { settle(); return; }
    // Not under a selection: a redraw replaces every node, so the text being selected would be lost. It is drawn when
    // the selection goes (selectionchange).
    if (selecting()) { heldBySel = true; return; }
    if (!stick && active()) {
      if (!paintLater) paintLater = setTimeout(() => { paintLater = 0; if (els && openId) paintReplies(); }, 150);
      return;
    }
    const first = !els.replies.dataset.sig;
    let hold = null;
    if (!stick && !first) {
      const top = els.scroll.getBoundingClientRect().top;
      const el = Array.from(els.replies.querySelectorAll("[data-k]")).find(e => e.getBoundingClientRect().bottom > top + 1);
      if (el) hold = { k: el.dataset.k, y: el.getBoundingClientRect().top };
    }
    els.replies.innerHTML = html;
    els.replies.dataset.sig = html;
    if (hold) {
      const el = Array.from(els.replies.querySelectorAll("[data-k]")).find(e => e.dataset.k === hold.k);
      if (el) { const d = el.getBoundingClientRect().top - hold.y; if (d) setTop(els.scroll.scrollTop + d); }
    }
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
    const o = older(id);
    // The newest window moves on with every refresh, pushing its oldest entries out of reach of the cursor the older
    // pages were read with. Once a card has older pages, the window being replaced is folded into them first.
    const prev = cache.get(id);
    if (prev && !prev.failed && !got.failed && (o.pages || o.replies.length || o.prompts.length)) foldInto(o, prev);
    cache.set(id, got);
    if (!got.failed && !o.pages) { o.more = !!got.more; o.next = got.next_before || ""; o.failed = false; }
    paintReplies();
  }

  // One request per click or scroll to the top, never a loop: a read can cover a lot of transcript. A page with more
  // may be empty, and the cursor still moves on to the next click. Only more=false ends it.
  async function loadOlder() {
    const id = openId;
    if (!id || !els) return;
    const o = older(id);
    if (o.busy || !o.more || !o.next) return;
    o.busy = true; o.failed = false;
    paintReplies();
    const mine = seq;
    let r = null;
    try {
      r = await window.mNet.api("/v1/tasks/" + encodeURIComponent(id) + "/replies?n=" + OLDER_N + "&before=" + encodeURIComponent(o.next));
      if (!r || !Array.isArray(r.replies)) r = null;
    } catch (e) { r = null; }
    o.busy = false;
    if (id !== openId) return;
    if (!r) { o.failed = true; paintReplies(); return; }
    // paintReplies holds the bubble at the top of the view where it is, so the prepend does not move a reader
    o.replies = r.replies.concat(o.replies);
    o.prompts = (Array.isArray(r.prompts) ? r.prompts : []).concat(o.prompts);
    o.more = !!r.more;
    o.next = r.more ? (r.next_before || "") : "";
    if (r.more && !o.next) o.more = false;
    o.pages++;
    paintReplies();
  }
  // Reaching the top asks once, after a short wait, and never while a read is in flight.
  let topTimer = 0;
  function nearTop() {
    if (topTimer || !els || !openId) return;
    const o = older(openId);
    if (!o.more || o.busy || o.failed) return;
    topTimer = setTimeout(() => {
      topTimer = 0;
      if (els && openId && els.scroll.scrollTop < 40) loadOlder();
    }, 350);
  }

  // The documents this card published, as one line. js/docs.js asks the hub at most once in 30 seconds.
  function paintDocs() {
    const n = q("m-card-docs");
    if (n && window.mDocs && openId) window.mDocs.paintCard(n, openId);
  }

  function onCards() {
    if (!openId) return;
    const t = window.mStore.card(openId);
    paint(false);
    const key = openId + "@" + turnOf(t);
    const out = (t && t.output_at) || "";
    const moved = !!out && out !== outAt;
    if (out) outAt = out;
    if (key !== turnKey || moved) { turnKey = key; loadReplies(); paintDocs(); }
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
    closeTok++;
    openId = id;
    turnKey = id + "@" + turnOf(window.mStore.card(id));
    outAt = (window.mStore.card(id) || {}).output_at || "";
    els.sheet.hidden = false;
    menuClose();
    els.sheet.classList.toggle("direct", !!(history.state && history.state.direct));
    setTop(0);
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
    paintDocs();
  }

  function finishClose() {
    if (window.mViewer) window.mViewer.reset();
    if (window.mChanges) window.mChanges.reset();
    if (MD.release) MD.release();
    els.sheet.hidden = true;
    els.head.innerHTML = els.notices.innerHTML = els.replies.innerHTML = els.extras.innerHTML = els.recap.innerHTML = "";
    els.working.hidden = true;
    els.recap.hidden = true;
    if (q("m-card-docs")) q("m-card-docs").hidden = true;
    olderOf.delete(window.mNet.bareId(openId));
    openId = "";
  }

  // The back button and the on-screen chevron both end up here through popstate.
  function closeNow() {
    if (!openId) return;
    offs.forEach(f => f());
    offs = [];
    unmountAll();
    seq++;
    bubbleText.clear(); heldBySel = false;
    document.body.classList.remove("sheet-open");
    els.sheet.classList.remove("on");
    const id = openId, tok = ++closeTok;
    if (reduced()) { finishClose(); return; }
    // A close that was overtaken by a reopen of the same card (a quick tap on a slow machine) must not close the new one.
    const done = () => { if (tok === closeTok && openId === id && !els.sheet.classList.contains("on")) finishClose(); };
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

  // ── the card picker: the other cards, filtered, one tap to go there ──
  // Running and needs-you by default, so a long list of finished cards does not bury the live ones. The chips at the top
  // change that, and the choice is kept on this device. A search box shows when the list is long.
  const PICK_KEY = "atrium.mswitch", PICK_LONG = 12;
  const PICK_CHIPS = [["running", "running"], ["needs", "needs you"], ["ready", "ready"], ["done", "done"], ["all", "all"]];
  function pickSet() {
    try {
      const a = JSON.parse(localStorage.getItem(PICK_KEY) || "null");
      const known = Array.isArray(a) ? a.filter(k => PICK_CHIPS.some(c => c[0] === k)) : [];
      if (known.length) return new Set(known);
    } catch (e) {}
    return new Set(["running", "needs"]);
  }
  function pickSave(set) { try { localStorage.setItem(PICK_KEY, JSON.stringify(Array.from(set))); } catch (e) {} }
  function pickMatches(t, set) {
    if (set.has("all")) return true;
    const rs = window.mHome.reasons(t, window.mStore.perms());
    return (set.has("running") && t.status === "running") || (set.has("needs") && rs.some(r => r.kind !== "ready")) ||
      (set.has("ready") && t.status === "needs-input") || (set.has("done") && (t.status === "done" || t.status === "dead" || t.status === "shelved"));
  }

  function menuClose() {
    els.menu.hidden = true;
    if (els.menuBack) els.menuBack.hidden = true;
    els.pick.setAttribute("aria-expanded", "false");
  }

  function menuPaint() {
    const set = pickSet();
    const chips = els.menu.querySelector(".pm-chips"), search = els.menu.querySelector(".pm-search"), list = els.menu.querySelector(".pm-list");
    chips.innerHTML = PICK_CHIPS.map(c => '<button type="button" class="pm-chip" data-f="' + c[0] + '" aria-pressed="' + (set.has(c[0]) ? "true" : "false") + '">' + U.esc(c[1]) + "</button>").join("");
    let cards = window.mStore.cards().filter(t => !t.archived_at && t.id !== openId && pickMatches(t, set));
    cards.sort((a, b) => cardActivityCmp(a, b) || cardTieBreak(a, b));
    search.hidden = cards.length <= PICK_LONG && !search.value;
    const q = search.value.trim().toLowerCase();
    if (q) cards = cards.filter(t => { const nm = U.cardName(t); return (nm.main + " " + (nm.sub || "") + " " + U.statusLabel(t)).toLowerCase().indexOf(q) >= 0; });
    list.innerHTML = cards.length ? cards.map(t => {
      const nm = U.cardName(t);
      return '<button type="button" class="pm-row" data-id="' + U.esc(t.id) + '"><b>' + U.esc(nm.main) + "</b>" +
        '<span>' + U.esc(U.statusLabel(t)) + "</span></button>";
    }).join("") : '<p class="quiet">no cards here. try another chip</p>';
  }

  function menuToggle() {
    if (!els.menu.hidden) { menuClose(); return; }
    if (!els.menu.querySelector(".pm-list")) {
      els.menu.innerHTML = '<div class="pm-chips" role="group" aria-label="which cards"></div>' +
        '<input type="search" class="pm-search" placeholder="search cards" aria-label="search cards" spellcheck="false" autocomplete="off" hidden>' +
        '<div class="pm-list"></div>';
    }
    els.menu.querySelector(".pm-search").value = "";
    menuPaint();
    if (els.menuBack) els.menuBack.hidden = false;
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
      const ro = new ResizeObserver(() => { if (pinned()) return; if (openId) settle(); });
      [els.scroll, els.head, els.notices, els.replies, els.extras, els.perms].forEach(e => ro.observe(e));
    }
    pinchInit();
    pullInit();
    els.scroll.addEventListener("load", () => { if (openId) settle(); }, true);
    els.jump.addEventListener("click", () => { typingOff(); stick = true; syncAnchor(); toEnd(true); });
    ["touchend", "touchcancel", "pointerup", "pointercancel"].forEach(n => els.scroll.addEventListener(n, byHandEnd, { passive: true }));
    if (window.visualViewport) window.visualViewport.addEventListener("resize", () => { if (openId) settle(); });
    els.sheet.addEventListener("input", e => { if (e.target && e.target.classList && e.target.classList.contains("mc-box")) { if (e.target.value) typingOn(); else { typingOff(); settle(); } } }, true);
    els.sheet.addEventListener("focusout", e => { if (e.target && e.target.classList && e.target.classList.contains("mc-box")) typingOff(); });
    window.addEventListener("m-send", e => { if (e.detail && e.detail.state === "pending") typingOff(); });
    els.head.addEventListener("click", e => { if (e.target.closest && e.target.closest("#m-recap-open")) openRecap(); });
    els.replies.addEventListener("click", e => { if (e.target.closest && e.target.closest("#m-older")) loadOlder(); });
    els.recap.addEventListener("click", e => {
      if (e.target.id === "m-recap-back" || (e.target.closest && e.target.closest("#m-recap-close"))) closeRecap();
    });
    document.addEventListener("keydown", e => { if (e.key === "Escape" && els && !els.recap.hidden) closeRecap(); });
    els.back.addEventListener("click", close);
    els.pick.addEventListener("click", menuToggle);
    // The card's changes, and the chip on a reply that edited files. Both open the changes sheet over the thread.
    q("m-card-changes").addEventListener("click", () => { if (openId && window.mChanges) window.mChanges.openCard(openId); });
    els.replies.addEventListener("click", bubbleAct);
    document.addEventListener("selectionchange", () => { if (heldBySel && els && openId && !selecting()) { heldBySel = false; paintReplies(); } });
    els.replies.addEventListener("click", e => {
      const c = e.target.closest && e.target.closest(".chg-chip");
      if (c && openId && window.mChanges) window.mChanges.openTurn(openId, c.dataset.at);
    });
    els.menuBack = q("m-card-menu-back");
    if (els.menuBack) els.menuBack.addEventListener("click", menuClose);
    els.menu.addEventListener("input", e => { if (e.target.classList && e.target.classList.contains("pm-search")) menuPaint(); });
    els.menu.addEventListener("click", e => {
      const ch = e.target.closest(".pm-chip");
      if (ch) {
        const set = pickSet(), f = ch.dataset.f;
        if (f === "all") { set.clear(); set.add("all"); }
        else { set.delete("all"); if (set.has(f)) set.delete(f); else set.add(f); if (!set.size) { set.add("running"); set.add("needs"); } }
        pickSave(set);
        menuPaint();
        return;
      }
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

  window.mCard = { init, open, close, bindPinch, pathOf: id => pathFor(window.mStore.card(id), "/m"), isOpen: () => !!openId, current: () => openId };
})();
