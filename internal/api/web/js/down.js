// ── atrium is down, and nobody said it would be ──────────
//
// The event stream dropped with no restart announced, and atrium stopped
// answering: it was stopped, or it crashed. Without this the board sat there
// looking live with a small `reconnecting` in the header, and every click went
// nowhere.
//
//   - five seconds after the stream drops, if atrium does not answer, a cover
//     says so and counts how long it has been down
//   - it asks again every two seconds, and the stream retries on its own
//   - once atrium answers, it waits for the board to catch up the way the
//     restart cover does, then comes down
//
// A PLANNED RESTART IS NOT THIS. The restart cover owns everything from the
// countdown to the new hub, so this never shows while the countdown or the
// restart cover is up. See js/hubrestart.js.
//
// A reload or a fresh visit while atrium is down never reaches this file: the
// browser has no page to run it in. The service worker serves `down.html` for
// that, which wears the same card. See sw.js.

// How long the stream may be down before the cover goes up. A stream that
// blips and comes straight back is not atrium being down.
const DOWN_AFTER = 5000;
// How often atrium is asked while it is down.
const DOWN_EVERY = 2000;
// Carried across a reload, for the same reason as the restart cover.
const DOWN_KEY = "atrium.down";
// The palette the offline page wears. See `downKeepSkin`.
const DOWN_SKIN_KEY = "atrium.downskin";

// When the stream dropped, zero while it is up.
let downSince = 0;
let downShown = false;
let downTimer = 0;
let downPoll = 0;
let downClock = 0;
// As `hubSettleFrom`: -1 while the build is read, then the refresh pass to wait for.
let downSettleFrom = 0;

function downPlanned() {
  return !!((typeof hubRestarting !== "undefined" && hubRestarting) ||
    (typeof hubCountdown !== "undefined" && hubCountdown));
}

async function downAnswers() {
  try {
    const r = await fetch("/v1/health", { cache: "no-store" });
    return r.ok;
  } catch (e) {
    return false;
  }
}

// The stream reported an error. It does on every retry, so only the first
// counts.
function onBoardStreamDown() {
  if (downSince) return;
  downSince = Date.now();
  clearTimeout(downTimer);
  downTimer = setTimeout(downCheck, DOWN_AFTER);
}

async function downCheck() {
  downTimer = 0;
  if (!downSince || downShown || downPlanned()) return;
  // Answering with the stream still down is a stream that has not retried
  // yet. Asked again rather than covered.
  if (await downAnswers()) {
    if (downSince) downTimer = setTimeout(downCheck, DOWN_EVERY);
    return;
  }
  if (!downSince || downShown || downPlanned()) return;
  downShow(downSince);
}

function downShow(since) {
  const dlg = document.getElementById("atriumdown");
  if (!dlg) return;
  downShown = true;
  downSince = since;
  downSettleFrom = 0;
  try { sessionStorage.setItem(DOWN_KEY, String(since)); } catch (e) {}
  const clock = document.getElementById("atriumdown-el");
  const paint = () => {
    if (clock) clock.textContent = "down for " + Math.floor((Date.now() - downSince) / 1000) + "s";
  };
  paint();
  clearInterval(downClock);
  downClock = setInterval(paint, 1000);
  clearInterval(downPoll);
  downPoll = setInterval(async () => {
    if (downShown && !downSettleFrom && await downAnswers()) downSettle();
  }, DOWN_EVERY);
  if (!dlg.open) dlg.showModal();
}

// ATRIUM ANSWERS. The same wait as the restart cover's: read the build, so a
// new one reloads under the cover, then one refresh that began after this.
function downSettle() {
  if (!downShown || downSettleFrom) return;
  downSettleFrom = -1;
  fetch("/v1/health", { cache: "no-store" }).then(r => r.ok ? r.json() : Promise.reject(r.status)).then(h => {
    if (!downShown) return;
    if (typeof checkBuild === "function") checkBuild(h.build);
    if (typeof boardReloading !== "undefined" && boardReloading) return;
    downSettleFrom = (typeof refreshSeq === "number" ? refreshSeq : 0) + 1;
    if (typeof refreshSoon === "function") refreshSoon();
    else downClear();
  }).catch(() => { downSettleFrom = 0; });
}

// A refresh pass finished. See `runRefresh`.
function onDownRefreshSettled(seq) {
  if (!downShown || downSettleFrom <= 0 || seq < downSettleFrom) return;
  if (typeof boardReloading !== "undefined" && boardReloading) return;
  downClear();
}

function downClear() {
  downShown = false;
  downSettleFrom = 0;
  clearInterval(downClock);
  clearInterval(downPoll);
  downClock = downPoll = 0;
  try { sessionStorage.removeItem(DOWN_KEY); } catch (e) {}
  const dlg = document.getElementById("atriumdown");
  if (dlg && dlg.open) dlg.close();
}

// The stream is up.
function onBoardStreamUp() {
  downSince = 0;
  clearTimeout(downTimer);
  downTimer = 0;
  if (downShown) downSettle();
  downKeepSkin();
}

// THE OFFLINE PAGE WEARS THE SKIN. It is served by the service worker with
// atrium gone, so it cannot ask for the skin and cannot load the stylesheets.
// The board writes the palette it is wearing down here instead, and the page
// reads it back. Done on every stream open, which is often enough to follow a
// skin change, and when the page goes.
function downKeepSkin() {
  try {
    const cs = getComputedStyle(document.documentElement);
    const names = ["--bg-0", "--bg1-rgb", "--sink-rgb", "--grid", "--card-0", "--card-1", "--head", "--body",
      "--label", "--dim", "--stroke", "--stroke-dim-rgb", "--warn-rgb", "--danger-rgb", "--danger-text",
      "--shadow-rgb", "--teal-rgb", "--blue", "--chip-accent-text"];
    const skin = {};
    names.forEach(n => { const v = cs.getPropertyValue(n).trim(); if (v) skin[n] = v; });
    localStorage.setItem(DOWN_SKIN_KEY, JSON.stringify(skin));
  } catch (e) {}
}
addEventListener("pagehide", downKeepSkin);

(function holdTheDownCover() {
  const dlg = document.getElementById("atriumdown");
  if (!dlg) return;
  dlg.addEventListener("cancel", e => e.preventDefault());
  // Refused while up, as the restart cover's is. See `holdTheCover`.
  const close = dlg.close.bind(dlg);
  dlg.close = v => { if (!downShown) close(v); };
  dlg.addEventListener("close", () => {
    if (downShown) setTimeout(() => { if (downShown && !dlg.open) dlg.showModal(); }, 0);
  });
  // A reload while it was up, from a new build or by hand, comes back with it up
  // until this page has atrium too.
  try {
    const since = Number(sessionStorage.getItem(DOWN_KEY)) || 0;
    if (since && Date.now() - since < 86400000) downShow(since);
    else sessionStorage.removeItem(DOWN_KEY);
  } catch (e) {}
})();
