// The phone page's bell and its sound. The same two things the board has: a bell with the count of what arrived since
// it was last opened, and a sound that can be muted, both kept in this browser's localStorage under the board's own keys
// (`atrium.toastlog`, `atrium.sound`) so the two pages agree and a mute on one is a mute on the other.
//
// WHAT RINGS: a permission request that was not there before, and a card that has started waiting for the operator. Not
// the card whose sheet is open on a page that is showing, which the operator is reading. The first read after the page
// loads is the baseline and says nothing. Event driven, no timers.
//
// WHY A SOUND MAY BE SILENT: a mobile browser starts audio only from inside a touch that ends or a click, not from a
// touch that begins. So every one of those tries to start it, and "tap to enable sound" shows while it is still locked.
(function () {
  "use strict";

  const U = window.mUtil;
  const LOG_KEY = "atrium.toastlog";
  const SOUND_KEY = "atrium.sound";
  const LOG_MAX = 200;

  let ctx = null;
  let els = null;
  const seenWait = new Set();
  const seenPerm = new Set();
  let baseline = false;

  const guard = (fn, dflt) => { try { return fn(); } catch (e) { return dflt; } };
  const prefs = () => guard(() => loadPrefs(), { muted: false, volume: 0.35, input: "chime", perm: "alarm" });
  const log = () => guard(() => JSON.parse(localStorage.getItem(LOG_KEY) || "[]"), []);
  const seenAt = () => guard(() => Number(localStorage.getItem(LOG_KEY + ".seen") || 0), 0);

  function setMuted(m) {
    const cur = guard(() => JSON.parse(localStorage.getItem(SOUND_KEY) || "{}"), {});
    cur.muted = !!m;
    guard(() => localStorage.setItem(SOUND_KEY, JSON.stringify(cur)));
  }

  // ── the sound ────────────────────────────────────────────────────────────
  function blocked() { return !prefs().muted && (!ctx || ctx.state !== "running"); }

  function unlock() {
    if (!ctx) {
      const AC = window.AudioContext || window.webkitAudioContext;
      if (AC) {
        ctx = new AC();
        if (ctx.addEventListener) ctx.addEventListener("statechange", paint);
      }
    }
    if (ctx && ctx.state === "suspended") {
      const r = ctx.resume();
      if (r && r.then) r.then(paint, () => {});
    }
    paint();
  }

  function tone(name) {
    const s = SOUNDS[name];
    if (!s || !ctx || ctx.state !== "running") return;
    const vol = prefs().volume;
    s.notes.forEach(([freq, start, dur, gain, type]) => {
      const osc = ctx.createOscillator(), amp = ctx.createGain();
      osc.type = type || "sine";
      osc.frequency.setValueAtTime(freq, ctx.currentTime + start);
      amp.gain.setValueAtTime(0, ctx.currentTime + start);
      amp.gain.linearRampToValueAtTime(Math.max(0.0001, gain * vol), ctx.currentTime + start + 0.015);
      amp.gain.exponentialRampToValueAtTime(0.0001, ctx.currentTime + start + dur);
      osc.connect(amp).connect(ctx.destination);
      osc.start(ctx.currentTime + start);
      osc.stop(ctx.currentTime + start + dur + 0.02);
    });
  }

  function play(kind) {
    const p = prefs();
    if (p.muted) return;
    tone(kind === "permission" ? p.perm : p.input);
  }

  // ── the log ──────────────────────────────────────────────────────────────
  // The board's own entry shape, a repeat bumping the last one.
  function record(title, body, taskFor) {
    const list = log();
    const last = list[list.length - 1];
    const sig = title + " " + (body || "");
    if (last && last.sig === sig) { last.n = (last.n || 1) + 1; last.at = Date.now(); }
    else list.push({ sig, title, body: body || "", goTo: "", taskFor: taskFor || "", key: "", at: Date.now(), n: 1 });
    guard(() => localStorage.setItem(LOG_KEY, JSON.stringify(list.slice(-LOG_MAX))));
    paint();
  }

  function unread() { const s = seenAt(); return log().filter(t => t.at > s).length; }

  function paint() {
    if (!els) return;
    const n = unread(), m = !!prefs().muted;
    els.n.textContent = n > 99 ? "99+" : n || "";
    els.bell.classList.toggle("has", n > 0);
    els.bell.classList.toggle("muted", m);
    els.bell.setAttribute("aria-label", n ? n + " unread notifications" : "notifications");
    els.mute.textContent = m ? "sound off" : "sound on";
    els.mute.setAttribute("aria-pressed", String(m));
    els.hint.hidden = !blocked();
  }

  function ago(ms) { return U.ago(Date.now() - ms); }

  function drawLog() {
    const list = log().slice().reverse(), s = seenAt();
    els.list.innerHTML = list.length ? list.map(t =>
      '<button type="button" class="bl-row' + (t.at > s ? " fresh" : "") + '"' + (t.taskFor ? ' data-id="' + U.esc(t.taskFor) + '"' : "") + ">" +
      "<b>" + U.esc(t.title) + (t.n > 1 ? " <i>x" + t.n + "</i>" : "") + "</b>" +
      (t.body ? "<span>" + U.esc(t.body) + "</span>" : "") + "<time>" + U.esc(ago(t.at)) + "</time></button>").join("")
      : '<p class="quiet">nothing yet</p>';
  }

  function openLog() {
    drawLog();
    els.sheet.hidden = false;
    document.body.classList.add("log-open");
    // Marked read on open, so the rows that were new stay marked while they are read.
    guard(() => localStorage.setItem(LOG_KEY + ".seen", String(Date.now())));
    paint();
  }

  function closeLog() {
    els.sheet.hidden = true;
    document.body.classList.remove("log-open");
  }

  // ── what rings ───────────────────────────────────────────────────────────
  function watching(id) {
    return document.visibilityState === "visible" && window.mCard && window.mCard.current && window.mCard.current() === id;
  }

  function name(t) { return U.cardName(t).main; }

  function onCards() {
    if (!window.mNet.loaded()) return;
    const cards = window.mStore.cards();
    const first = !baseline;
    let rang = false;
    cards.forEach(t => {
      if (t.archived_at || t.status !== "needs-input") return;
      const key = t.id + "@" + (t.waiting_since || "");
      if (seenWait.has(key)) return;
      seenWait.add(key);
      if (first || (isDoer(t))) return;
      if (watching(t.id)) return;
      record(name(t) + " is waiting for you", "", t.id);
      rang = true;
    });
    baseline = true;
    if (rang) play("waiting");
  }

  function onPerms() {
    if (!window.mNet.loaded()) return;
    const perms = window.mStore.perms();
    const first = !baseline;
    let rang = false;
    perms.forEach(p => {
      if (seenPerm.has(p.id)) return;
      seenPerm.add(p.id);
      if (first) return;
      const card = window.mStore.card(p.task_id);
      record("permission needed", (card ? name(card) + " wants to use " : "wants to use ") + (p.tool || "a tool"), p.task_id);
      rang = true;
    });
    if (rang) play("permission");
  }

  function init() {
    els = {
      bell: document.getElementById("m-bell"), n: document.getElementById("m-bell-n"), sheet: document.getElementById("m-log"),
      list: document.getElementById("m-log-list"), mute: document.getElementById("m-log-mute"), close: document.getElementById("m-log-close"),
    };
    els.hint = document.createElement("button");
    els.hint.type = "button";
    els.hint.id = "m-sound-hint";
    els.hint.textContent = "tap to enable sound";
    els.hint.hidden = true;
    document.body.appendChild(els.hint);
    ["pointerdown", "pointerup", "touchend", "click", "keydown"].forEach(t => document.addEventListener(t, unlock, { capture: true, passive: true }));
    document.addEventListener("visibilitychange", () => { if (document.visibilityState === "visible" && ctx && ctx.state === "suspended") ctx.resume(); });
    els.bell.addEventListener("click", openLog);
    els.close.addEventListener("click", closeLog);
    els.mute.addEventListener("click", () => {
      setMuted(!prefs().muted);
      paint();
      if (!prefs().muted) { unlock(); play("waiting"); }
    });
    els.list.addEventListener("click", e => {
      const b = e.target.closest(".bl-row");
      if (!b) return;
      closeLog();
      if (b.dataset.id && window.mCard) window.mCard.open(b.dataset.id);
    });
    // The board and this page share the keys, so a change made in another tab repaints here.
    window.addEventListener("storage", e => { if (!e.key || e.key.indexOf("atrium.toastlog") === 0 || e.key === SOUND_KEY) paint(); });
    window.mStore.on("cards", onCards);
    window.mStore.on("perms", onPerms);
    paint();
  }

  window.mBell = { init, play, record, unread, openLog, blocked };
})();
