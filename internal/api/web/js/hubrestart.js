// ── the hub restart gate, the board's half ───────────────
//
// A hub-only deploy asks the hub before it restarts it, and the hub answers once
// nobody is using a board. See docs/hub-restart-gate.md. This file does the
// three things only a board can:
//
//   - says when somebody is using it, so the hub knows what idle means
//   - shows the countdown, and turns a click on it into a pause
//   - blocks the board while the hub is away, and lets go when it is back
//
// EVERY WINDOW DOES ALL THREE, the popped-out ones included. Each holds its own
// event stream and the hub says the same thing down all of them, so a click in
// any window pauses every window.
//
// Only on a hub. A plain daemon has no gate, and reporting input to it would be
// a 404 on every keystroke.

// How often input is reported while somebody keeps going. Well under the hub's
// idle window, so a steady typist never looks idle between reports.
const HUB_INPUT_EVERY = 3000;
// How long the restarting cover waits for the stream to drop before deciding the
// old hub never went.
const HUB_RESTART_GIVEUP = 90000;

let hubInputAt = 0;
let hubCountdown = null;
let hubCountdownTick = 0;
let hubPausedToast = null;
let hubRestarting = 0;

// The gate's own toasts and cover are not input. A click on the countdown is a
// pause, and reporting it as input as well would take the countdown down under
// the pointer before the pause arrived.
function hubInputCounts(e) {
  const t = e.target;
  if (!t || !t.closest) return true;
  return !t.closest(".toast.hubgate, #hubrestart");
}

function hubReportInput(e) {
  if (!hubIsHub || !hubInputCounts(e)) return;
  const now = Date.now();
  if (now - hubInputAt < HUB_INPUT_EVERY) return;
  hubInputAt = now;
  plainFetch("/_hub/restart/input", { method: "POST" }).catch(() => {});
}
// CAPTURE, so a terminal that stops a keystroke on its way to the document still
// counts. Pointer movement is left out: reading is not using.
["keydown", "pointerdown", "wheel", "paste"].forEach(k =>
  document.addEventListener(k, hubReportInput, { capture: true, passive: true }));

// A sticky toast: no timer and no dismiss button, because it is taken down by
// the hub saying so, not by the clock. `sticky` keeps it out of the toast cap.
// `held` is the paused one, which wears a pause mark rather than the ring.
function hubToast(title, body, button, onButton, held) {
  const host = document.getElementById("toasts");
  if (!host) return null;
  if (typeof recordToLog === "function") recordToLog(title, body, "", null, null);
  const el = document.createElement("div");
  el.className = "toast hubgate sticky" + (held ? " held" : "");
  el.innerHTML = `<div class="hg-icon"><i></i></div>
    <div class="body"><b></b><div class="what"></div></div>
    <button class="hubgate-act"></button>`;
  el.querySelector("b").textContent = title;
  el.querySelector(".what").textContent = body;
  el.querySelector(".hubgate-act").textContent = button;
  if (held) el.querySelector(".hg-icon > i").textContent = "❚❚";
  el.addEventListener("click", onButton);
  host.appendChild(el);
  if (typeof raiseToasts === "function") raiseToasts();
  return el;
}

function hubDropCountdown() {
  clearInterval(hubCountdownTick);
  hubCountdownTick = 0;
  if (hubCountdown) hubCountdown.remove();
  hubCountdown = null;
}

function hubDropPaused() {
  if (hubPausedToast) hubPausedToast.remove();
  hubPausedToast = null;
}

function hubPause() {
  // Taken down here as well as on the hub's answer, so a slow answer does not
  // leave a countdown ticking under the click that stopped it.
  hubDropCountdown();
  plainFetch("/_hub/restart/pause", { method: "POST" }).catch(() => {});
}

function hubResume() {
  hubDropPaused();
  plainFetch("/_hub/restart/resume", { method: "POST" }).catch(() => {});
}

function hubShowCountdown(seconds) {
  hubDropCountdown();
  const end = Date.now() + seconds * 1000;
  const left = () => Math.max(0, Math.ceil((end - Date.now()) / 1000));
  const say = () => "atrium restarts in " + left() + "s";
  // A click anywhere on it pauses, and so does the button. Typing or clicking
  // anywhere else takes it down until the board is idle again, which is the
  // "keep working" half.
  hubCountdown = hubToast(say(),
    "an update is ready. keep working and it waits, or pause it until you are done.",
    "pause", hubPause);
  if (!hubCountdown) return;
  const title = hubCountdown.querySelector("b");
  const num = hubCountdown.querySelector(".hg-icon > i");
  const bar = document.createElement("div");
  bar.className = "hg-bar";
  bar.style.animationDuration = seconds + "s";
  hubCountdown.appendChild(bar);
  const paint = () => { title.textContent = say(); num.textContent = left(); };
  paint();
  hubCountdownTick = setInterval(paint, 250);
}

function hubShowPaused() {
  if (hubPausedToast && hubPausedToast.isConnected) return;
  hubDropPaused();
  hubPausedToast = hubToast("restart on hold",
    "atrium keeps running as it is until you press resume.", "resume",
    e => { if (e.target.classList.contains("hubgate-act")) hubResume(); }, true);
}

// BOTH TOASTS STAY UNTIL THE HUB SAYS WHAT COMES NEXT. The countdown is the
// only place to pause, and the paused toast is the only way back out. The cap
// skips them, and anything else that takes one off the stack sees it put back.
// Dropping one clears its variable first, so a drop is never undone here.
// The body rather than the host, because `raiseToasts` moves the host into
// whichever dialog is on top.
if (window.MutationObserver) {
  new MutationObserver(() => {
    const host = document.getElementById("toasts");
    if (!host) return;
    [hubCountdown, hubPausedToast].forEach(el => {
      if (el && !el.isConnected) host.insertBefore(el, host.firstChild);
    });
  }).observe(document.body, { subtree: true, childList: true });
}

// The cover. A modal, so everything under it is inert: nothing typed into a
// terminal while the hub is away can land half way.
// A restart takes a few seconds. Past this the line under the headline stops
// promising that and says it is still waiting, so a slow one does not read as
// a hang with a cheerful sentence on it.
const HUB_RESTART_SLOW = 30000;
let hubRestartClock = 0;

function hubShowRestarting() {
  const dlg = document.getElementById("hubrestart");
  if (!dlg) return;
  hubRestarting = Date.now();
  const line = document.getElementById("hubrestart-t");
  const clock = document.getElementById("hubrestart-el");
  const paint = () => {
    const ms = Date.now() - hubRestarting;
    line.textContent = ms < HUB_RESTART_SLOW
      ? "back in a few seconds. your agents keep running, and this page picks up where you left off."
      : "this is taking longer than usual. your agents keep running while atrium comes back.";
    if (clock) clock.textContent = Math.floor(ms / 1000) + "s";
  };
  paint();
  clearInterval(hubRestartClock);
  hubRestartClock = setInterval(paint, 1000);
  if (!dlg.open) dlg.showModal();
  const at = hubRestarting;
  setTimeout(() => {
    if (hubRestarting !== at) return;
    // Still live on the same stream means the old hub never went.
    const conn = document.getElementById("conn");
    if (conn && conn.classList.contains("live")) {
      hubClearRestarting();
      if (typeof toast === "function") {
        toast("atrium did not restart",
          "the update was called for, but atrium never went down. nothing changed.");
      }
    }
  }, HUB_RESTART_GIVEUP);
}

function hubClearRestarting() {
  hubRestarting = 0;
  clearInterval(hubRestartClock);
  hubRestartClock = 0;
  const dlg = document.getElementById("hubrestart");
  if (dlg && dlg.open) dlg.close();
}

(function holdTheCover() {
  const dlg = document.getElementById("hubrestart");
  if (!dlg) return;
  // Escape does not take it down, and neither does anything that closes every
  // open dialog on its way somewhere: the hub is still away.
  dlg.addEventListener("cancel", e => e.preventDefault());
  dlg.addEventListener("close", () => {
    if (hubRestarting) setTimeout(() => { if (hubRestarting && !dlg.open) dlg.showModal(); }, 0);
  });
})();

// What the hub said, off the event stream.
function onHubRestart(d) {
  switch (d && d.state) {
    case "countdown":
      hubDropPaused();
      hubShowCountdown(Number(d.seconds) || 5);
      break;
    case "cancelled":
      hubDropCountdown();
      break;
    case "paused":
      hubDropCountdown();
      hubShowPaused();
      break;
    case "resumed":
      hubDropPaused();
      break;
    case "restarting":
      hubDropCountdown();
      hubDropPaused();
      hubShowRestarting();
      // The attached terminal's socket is about to close, and this is what
      // makes the pane wait for it to come back rather than tear down.
      if (typeof armRestart === "function") armRestart("installing an update");
      break;
  }
}

// The stream opened. Either this is the first time, or it dropped and came back,
// and a stream only comes back by dropping, so a cover up now is a restart that
// has finished. The pause and a running countdown are re-read, since a window
// that opened after either began never heard the event. A countdown already on
// screen is left counting: the reopen is not news about it.
function onHubStreamOpen() {
  hubClearRestarting();
  if (!hubIsHub) return;
  plainFetch("/_hub/restart").then(r => r.ok ? r.json() : null).then(st => {
    if (!st) return;
    if (st.paused) hubShowPaused();
    else hubDropPaused();
    const left = Number(st.countdown_left) || 0;
    if (!st.paused && left > 0 && !hubCountdown) hubShowCountdown(left);
  }).catch(() => {});
}
