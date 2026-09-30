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

// THE PHONE HEADER, hidden on its own. Saved per device: "1" hidden, "0" shown, nothing saved means the
// landscape auto-collapse in phone.css decides. A saved choice wins over that, through the two body classes.
const PHONE_HDR_KEY = "atrium.phone.headerHidden";
function phoneHeaderApply() {
  let v = null;
  try { v = localStorage.getItem(PHONE_HDR_KEY); } catch (e) {}
  document.body.classList.toggle("hdr-hidden", v === "1");
  document.body.classList.toggle("hdr-shown", v === "0");
}
function phoneHeaderSet(hide) {
  try { localStorage.setItem(PHONE_HDR_KEY, hide ? "1" : "0"); } catch (e) {}
  phoneHeaderApply();
}
phoneHeaderApply();
