// A new build reaches the phone page without a hand reload.
//
// The hub's `/v1/health` carries `build`, a hash of the whole `web/` tree. The board remembers the first one it saw
// and reloads when it changes (js/terminal-links.js `checkBuild`). This page read nothing of the kind, and a phone
// page stays alive: an installed app or a tab in the background is never navigated, so it kept the build it opened
// with for days after a deploy. The HTTP cache was not the cause; every `/m/` file is `no-cache` with an ETag.
//
// Same signal, asked at the moments a hub restart shows: the stream coming back (`live`) and the page coming to the
// front. No clock of its own.
//
// A reload throws away what is on screen, so it waits while the person is in the middle of something: a draft in any
// box, a box with focus, text selected, a send or an upload in flight. A quiet "update ready" button shows while it
// waits, and the reload happens once the page is clear. Pressing it reloads at once.
//
// NO LOOP. A tab remembers when it last reloaded for a build, and waits out GUARD_MS before reloading again, so a hub
// that keeps answering with a different id costs one reload every GUARD_MS at most. A page that sees the id it already
// runs drops whatever it was waiting for.
(function () {
  "use strict";

  const KEY = "atrium.m.reloaded";
  const GUARD_MS = 30000;
  const RECHECK_MS = 2000;

  let seen = "";
  let target = "";
  let timer = 0;
  let cue = null;

  function busy() {
    if (window.mCompose && window.mCompose.busy && window.mCompose.busy()) return true;
    // Attached files and review comments are held in memory only, so they count as much as typed text.
    if (window.mCompose && window.mCompose.holding && window.mCompose.holding()) return true;
    const a = document.activeElement;
    if (a && (a.tagName === "TEXTAREA" || (a.tagName === "INPUT" && !/^(button|checkbox|radio|submit)$/.test(a.type)))) return true;
    for (const t of document.querySelectorAll("textarea")) if (t.value && t.value.trim()) return true;
    const s = window.getSelection && window.getSelection();
    return !!(s && !s.isCollapsed && String(s).length);
  }

  function guarded() {
    try {
      const r = JSON.parse(sessionStorage.getItem(KEY) || "null");
      return !!(r && Date.now() - r.at < GUARD_MS);
    } catch (e) { return false; }
  }

  function go() {
    try { sessionStorage.setItem(KEY, JSON.stringify({ build: target, at: Date.now() })); } catch (e) {}
    location.reload();
  }

  function showCue(on) {
    if (on && !cue) {
      cue = document.createElement("button");
      cue.type = "button";
      cue.className = "m-update";
      cue.textContent = "update ready";
      cue.setAttribute("aria-label", "a new version is ready, reload");
      cue.addEventListener("click", go);
      const bar = document.querySelector("header.bar");
      const live = document.getElementById("m-live");
      if (bar) bar.insertBefore(cue, live || null); else document.body.appendChild(cue);
    } else if (!on && cue) {
      cue.remove();
      cue = null;
    }
  }

  function settle() {
    clearTimeout(timer);
    timer = 0;
    if (!target) { showCue(false); return; }
    if (guarded()) { timer = setTimeout(settle, RECHECK_MS); return; }
    if (busy()) { showCue(true); timer = setTimeout(settle, RECHECK_MS); return; }
    go();
  }

  function note(build) {
    if (!build) return;
    if (!seen) { seen = build; return; }
    if (build === seen) { target = ""; settle(); return; }
    if (build === target) return;
    target = build;
    settle();
  }

  async function check() {
    try {
      const res = await fetch("/v1/health", { cache: "no-store" });
      if (!res.ok) return;
      note((await res.json()).build);
    } catch (e) {}
  }

  function init() {
    check();
    if (window.mNet && window.mNet.on) window.mNet.on("live", () => { if (window.mNet.live()) check(); });
    document.addEventListener("visibilitychange", () => { if (document.visibilityState === "visible") check(); });
    document.addEventListener("selectionchange", () => { if (target && !timer) settle(); });
  }

  window.mBuild = { init, check, note };
})();
