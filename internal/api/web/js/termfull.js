// ── attach from a phone, and the full screen terminal ────────────────────────
//
// Both act on the terminal bar and the key bar, so they live together. Nothing here polls: every step is a
// click, a key or a browser event, and the re-fit is the existing ResizeObserver on `#t-screen`.

// THE PICKER. A phone has no drag and drop and rarely a usable image paste, so the terminal had no way to
// upload. The chosen files go through `uploadIntoTerm`, which is the one pipeline for paste, drop and picker.
function pickTermFiles() {
  if (isGuest() || !termTask) return;
  const input = document.getElementById("t-attach-in");
  if (input) input.click();
}

function wireTermAttach() {
  const input = document.getElementById("t-attach-in");
  if (!input || input._wired) return;
  input._wired = true;
  input.addEventListener("change", () => {
    const files = Array.from(input.files || []);
    // Cleared after each pick so choosing the same file twice still fires `change`.
    input.value = "";
    if (files.length && !isGuest()) uploadIntoTerm(files);
  });
  // A tap on the key bar's copy must not move focus (see `wirePhoneKeys`), and the picker needs no focus.
}

// A guest link has no file endpoint, so it is offered no picker at all.
function syncTermAttach() {
  const off = isGuest();
  for (const id of ["t-attach", "t-keys-attach"]) {
    const b = document.getElementById(id);
    if (b) b.hidden = off;
  }
}

// FULL SCREEN. One class on body (`term-full`) hides the board's chrome in CSS; the pane then grows and the
// ResizeObserver on `#t-screen` re-fits xterm. In memory only: not saved anywhere, and detaching or switching
// cards leaves it (`clearTermPane`).
let termFullApi = false;   // the browser's own fullscreen was asked for, so leaving it must leave the mode

function termFullOn() { return document.body.classList.contains("term-full"); }

function setTermFull(on) {
  on = !!on && !!termTask;
  if (on === termFullOn()) return;
  document.body.classList.toggle("term-full", on);
  const b = document.getElementById("t-full");
  if (b) b.setAttribute("aria-pressed", on ? "true" : "false");
  if (on) {
    // The Fullscreen API on a phone. iOS Safari on a phone has none, and a refusal is fine: the CSS mode alone
    // is the full screen there.
    if (termPhone()) {
      const el = document.documentElement;
      const req = el.requestFullscreen || el.webkitRequestFullscreen;
      if (req) {
        try {
          termFullApi = true;
          const r = req.call(el);
          if (r && r.catch) r.catch(() => { termFullApi = false; });
        } catch (e) { termFullApi = false; }
      }
    }
  } else {
    const was = termFullApi;
    termFullApi = false;
    if (was && (document.fullscreenElement || document.webkitFullscreenElement)) {
      const ex = document.exitFullscreen || document.webkitExitFullscreen;
      try { const r = ex && ex.call(document); if (r && r.catch) r.catch(() => {}); } catch (e) {}
    }
  }
}

function toggleTermFull() { setTermFull(!termFullOn()); }

// The two states cannot get out of step: the user swiping out of browser fullscreen leaves the CSS mode.
function onFullscreenChange() {
  if (!termFullOn() || !termFullApi) return;
  if (document.fullscreenElement || document.webkitFullscreenElement) return;
  setTermFull(false);
}
document.addEventListener("fullscreenchange", onFullscreenChange);
document.addEventListener("webkitfullscreenchange", onFullscreenChange);

// ESC. Esc is also the key claude uses to interrupt, so it must never be swallowed while the runner should
// get it. The rule:
//  - Desktop: Esc leaves full screen only when the terminal does NOT have focus (for example straight after
//    clicking the full screen button, which takes focus). With focus in the terminal Esc goes to the runner
//    untouched, and the button leaves.
//  - Phone: the key bar's Esc always reaches the runner, and a SECOND Esc within 500ms leaves full screen
//    instead of being sent. The double press is the price of not stealing a lone Esc. It is a timestamp
//    compare, not a timer.
let termFullEscAt = 0;
document.addEventListener("keydown", e => {
  if (e.key !== "Escape" || !termFullOn()) return;
  // An open dialog owns its Esc: closing it must not also leave full screen.
  if (document.querySelector("dialog[open]")) return;
  const scr = document.getElementById("t-screen");
  if (scr && scr.contains(document.activeElement)) return;
  e.preventDefault();
  setTermFull(false);
}, true);

// Called by `phoneKey` for the key bar's Esc. Returns true when it left full screen (and so sent nothing).
function termFullPhoneEsc() {
  if (!termFullOn()) return false;
  const now = Date.now();
  const dbl = now - termFullEscAt < 500;
  termFullEscAt = dbl ? 0 : now;
  if (dbl) setTermFull(false);
  return dbl;
}

wireTermAttach();
syncTermAttach();

// The key bar's Esc, in the capture phase so a double press can leave full screen before `phoneKey` sends it.
document.addEventListener("click", e => {
  const b = e.target.closest && e.target.closest("#t-keys button[data-key=esc]");
  if (b && termFullPhoneEsc()) { e.stopImmediatePropagation(); e.preventDefault(); }
}, true);

// DETACH LEAVES FULL SCREEN. Wrapped rather than edited in, since clearTermPane is another file's.
(function () {
  const inner = clearTermPane;
  clearTermPane = function (switching) { setTermFull(false); return inner.apply(this, arguments); };
})();

// THE PHONE BOARD HEADER, slim or open (u-021, #hdr-toggle). ONE saved choice per device: "1" open, anything else
// (or nothing saved) slim. A class on body and the chevron's aria state only, so nothing polls.
const PHONE_HDR_KEY = "atrium.phone.headerOpen";
function phoneHeaderOpenApply() {
  let v = null;
  try { v = localStorage.getItem(PHONE_HDR_KEY); } catch (e) {}
  const open = v === "1";
  document.body.classList.toggle("hdr-open", open);
  const h = document.getElementById("hdr-toggle");
  if (h) {
    h.setAttribute("aria-expanded", open ? "true" : "false");
    h.setAttribute("aria-label", open ? "hide the rest of the header" : "show the full header");
    h.textContent = open ? "▴" : "▾";
  }
}
function phoneHeaderOpenSet(open) {
  try { localStorage.setItem(PHONE_HDR_KEY, open ? "1" : "0"); } catch (e) {}
  phoneHeaderOpenApply();
}
// A pop-out window and the board follow each other. A null key is localStorage.clear().
window.addEventListener("storage", e => { if (!e.key || e.key === PHONE_HDR_KEY) phoneHeaderOpenApply(); });
(function () {
  // A tap on a chevron or the handle must not move focus, so it neither pops the keyboard nor blurs the terminal.
  for (const id of ["t-bar-toggle", "hdr-toggle", "t-tray-handle"]) {
    const b = document.getElementById(id);
    if (b) b.addEventListener("mousedown", e => e.preventDefault());
  }
  phoneHeaderOpenApply();
})();

// THE PHONE TERMINAL TRAY (u-023). The terminal bar is fully hidden on a phone, zero height. It slides down OVER the
// terminal, never pushing it, so the grid never changes size when it opens or closes. Opens from the handle at the
// pane's top edge (a tap, or a pull down), closes on a swipe up, a tap outside, or any action in it. Its own saved
// choice per device, in the board and the pop-out alike: "1" open, anything else hidden. Separate from
// atrium.phone.headerOpen, which keeps driving only the board header. A class on body, so nothing polls.
const PHONE_TRAY_KEY = "atrium.phone.trayOpen";
function phoneTrayOpen() { return document.body.classList.contains("tray-open"); }
function phoneTrayPlace() {
  // The floating card list and the fit button hang off the tray's bottom edge.
  const bar = document.querySelector(".term-bar");
  const px = phoneTrayOpen() && bar ? Math.round(bar.getBoundingClientRect().height) : 0;
  document.documentElement.style.setProperty("--trayh", px + "px");
}
function phoneTrayApply() {
  let v = null;
  try { v = localStorage.getItem(PHONE_TRAY_KEY); } catch (e) {}
  const open = v === "1";
  document.body.classList.toggle("tray-open", open);
  const el = document.getElementById("t-tray-handle");
  if (el) {
    el.setAttribute("aria-expanded", open ? "true" : "false");
    el.setAttribute("aria-label", open ? "hide the terminal's tray" : "show the terminal's tray");
  }
  phoneTrayPlace();
  if (open) requestAnimationFrame(phoneTrayPlace);
}
function phoneTraySet(open) {
  try { localStorage.setItem(PHONE_TRAY_KEY, open ? "1" : "0"); } catch (e) {}
  phoneTrayApply();
  if (!open && termListOpen && typeof setTermListOpen === "function") setTermListOpen(false);
}
window.addEventListener("storage", e => { if (!e.key || e.key === PHONE_TRAY_KEY) phoneTrayApply(); });
(function () {
  const phone = () => document.body.classList.contains("term-phone");
  const handle = document.getElementById("t-tray-handle");
  const bar = document.querySelector(".term-bar");
  let y0 = null, x0 = 0;
  // the handle: a tap opens, a pull down opens
  if (handle) {
    handle.addEventListener("click", () => phoneTraySet(true));
    handle.addEventListener("touchstart", e => { y0 = e.touches[0].clientY; }, { passive: true });
    handle.addEventListener("touchmove", e => {
      if (y0 !== null && e.touches[0].clientY - y0 > 20) { y0 = null; phoneTraySet(true); }
    }, { passive: true });
    handle.addEventListener("touchend", () => { y0 = null; }, { passive: true });
  }
  if (bar) {
    // a swipe up in the tray closes it
    bar.addEventListener("touchstart", e => { y0 = e.touches[0].clientY; x0 = e.touches[0].clientX; }, { passive: true });
    bar.addEventListener("touchend", e => {
      if (y0 === null || !phone()) return;
      const t = e.changedTouches[0], dy = t.clientY - y0, dx = t.clientX - x0;
      y0 = null;
      if (dy < -30 && Math.abs(dy) > Math.abs(dx)) phoneTraySet(false);
    }, { passive: true });
    // any action in it closes it, except the two that open more of it: the card picker and the cog
    bar.addEventListener("click", e => {
      if (!phone() || !phoneTrayOpen()) return;
      const b = e.target.closest && e.target.closest("button, a");
      if (!b || b.id === "t-pick" || b.id === "t-cog") return;
      phoneTraySet(false);
    });
  }
  // a tap outside closes it. The tap still goes through to whatever it landed on.
  document.addEventListener("pointerdown", e => {
    if (!phone() || !phoneTrayOpen()) return;
    if (e.target.closest && e.target.closest(".term-bar, #t-tray-handle, #term-list, #t-view, dialog")) return;
    phoneTraySet(false);
  }, true);
  // picking a card closes the list, and that is the end of the picker's job
  if (typeof setTermListOpen === "function") {
    const inner = setTermListOpen;
    setTermListOpen = function (open) {
      const was = termListOpen;
      const r = inner.apply(this, arguments);
      const p = document.getElementById("t-pick");
      if (p) p.setAttribute("aria-expanded", termListOpen ? "true" : "false");
      if (was && !open && phone() && phoneTrayOpen()) phoneTraySet(false);
      return r;
    };
  }
  phoneTrayApply();
})();
