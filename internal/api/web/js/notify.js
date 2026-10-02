// ── who owns an alert when a terminal is popped out ──────
//
// `inForeground` answers "can you see THIS document". Once a terminal is in
// its own window there are two documents and the answer differs for each, so
// the question becomes which one should speak.
//
// The popped-out window owns its card. It is the window you alt-tab to, it is
// the one whose title bar carries the mark, and it is the only document that
// knows whether you are looking at that session right now. The board keeps
// everything else, including that card's badges and counts: routing a card
// off the board entirely would mean a window buried on another desktop
// silently ate every alert for it, which is worse than the duplicate.
//
// A board cannot find its popped-out windows. `window.open` hands back a
// handle that dies when the board reloads while the window it opened carries
// on, so ownership is ANNOUNCED, not discovered. Each solo window claims its
// card, releases it on close, and re-claims whenever a board asks.
// Claims, with a time on each, because a claim is a heartbeat and not a memory.
//
// It was a Set: a window announced itself on load and removed itself on
// `pagehide`. Anything that skipped that release left the card claimed
// forever, and the board would then refuse to attach and say it had raised a
// window that no longer existed. A crash does it, and so does the browser
// discarding a background tab.
//
// So a solo window re-claims on every poll, and a claim that has not been
// heard in `soloClaimFor` is not believed. Self-healing: the worst case is one
// poll of being wrong, and it corrects without anybody restarting anything.
//
// KEYED BY THE BARE ID, whatever spelling asks. A popped-out window is
// addressed by the id in its `#term=` hash, fixed when it opened, while the
// board re-resolves its attached card between `room~id` and bare every time the
// room set changes (see `retagTermId`). A restart is a room-set change: rooms
// re-attach one at a time. So a window opened as `sgg~X` claims `sgg~X` for
// good while the board comes back holding `X`, and a raw-keyed ledger answered
// "not popped out" to the board and "not mine" to the claim. Both views then
// attached the one terminal and neither let go.
class BareIdMap extends Map {
  get(k) { return super.get(bareId(k)); }
  set(k, v) { return super.set(bareId(k), v); }
  has(k) { return super.has(bareId(k)); }
  delete(k) { return super.delete(bareId(k)); }
}
// Whether two spellings name the same card. See `BareIdMap`.
function sameCard(a, b) { return !!a && !!b && bareId(a) === bareId(b); }
const soloHeld = new BareIdMap();
const soloClaimFor = 15000;
const soloBus = ("BroadcastChannel" in window) ? new BroadcastChannel("atrium-solo") : null;
// Declared up here rather than beside the rest of the solo code, because the
// bus handler below closes over it and a `let` further down the file would be
// in its temporal dead zone if a claim ever arrived first.
let soloID = "", soloTask = null;

// This window has HANDED ITS TERMINAL OVER to another window and must stop
// behaving like a viewer of it.
//
// Set by `solo-yield`, which is how a second window onto one card is made
// recoverable in a click rather than a dead end. Two things read it and both
// have to: `soloRefresh` stops re-claiming, or the window that took the card
// would be refused on its own next roll call, and the watchdog in the same
// function stops re-attaching, or a window told to let go would put the
// terminal straight back on the next poll.
let soloYielded = false;

// ── which atrium window says it, and in what form ────────
//
// ONE ALERT PER EVENT, ACROSS EVERY ATRIUM WINDOW. That is a rule about the
// SET of windows, not about any one of them:
//
//   - a window has focus  -> THAT window toasts, and nothing else happens
//   - no window has focus -> ONE desktop notification, and no toast anywhere
//
// Never both, and never twice.
//
// What was here before could not express it, because each document decided
// alone out of what it could see. The board asked `inForeground`, a popped-out
// window asked `onScreen`, and neither can see the other. So a board sitting
// behind a popped-out window believed itself unattended: it toasted where
// nobody was looking AND rang Windows, while the window being read said
// nothing. The symptom is a chime with no notification behind it.
//
// `document.hasFocus()` is the whole test, and it is only answerable about the
// document asking. So the windows tell each other.
const thisWindowSays = Math.random().toString(36).slice(2, 10) + Date.now().toString(36);

// A FOCUS CLAIM IS A HEARTBEAT, NOT A MEMORY, for the same reason a solo claim
// is one. A window that dies while focused would otherwise suppress every
// desktop notification for as long as any other window stayed open, and
// nothing would ever correct it.
const focusClaimFor = 12000;
const focusBeat = 4000;
let focusedElsewhere = { win: "", at: 0 };
let hadFocus = false;

// This window's own answer is a fact. Every other window's is a report, and
// reports go stale.
// A page that is not visible is not looked at, whatever `hasFocus` says. A phone with the screen off or another app on top can
// keep answering true, and its alerts were then taken for ones the operator was already reading.
function focusIsHere() { return document.hasFocus() && document.visibilityState !== "hidden"; }
function focusIsElsewhere() {
  if (focusIsHere() || !focusedElsewhere.win) return "";
  if (Date.now() - focusedElsewhere.at > focusClaimFor) return "";
  return focusedElsewhere.win;
}

// The bare id of the card this window shows the terminal of, or "". Function
// declarations only: `termWatching` is defined further down and hoists.
function watchedCard() {
  // `termTask` is a `let` in a later script, and this runs once at load before
  // it exists: a `typeof` on it would throw, so the read is guarded instead.
  try { return termTask && termWatching(termTask.id) ? bareId(termTask.id) : ""; } catch (e) { return ""; }
}

// Whether a ready alert for this card is to be said nowhere. A window that is
// focused and showing the card claims it, and that window's claim covers every
// other window: the sound, the toast and the toast forwarded to it.
function readySilenced(id) {
  if (!id) return false;
  if (focusIsHere()) return termWatching(id);
  return !!focusIsElsewhere() && !!focusedElsewhere.watch && focusedElsewhere.watch === bareId(id);
}

// Said on every transition AND on a beat while focused. The transition is what
// makes it prompt and the beat is what makes it survive a window that never
// got to say goodbye.
function sayWhetherFocused() {
  if (!soloBus) return;
  const now = focusIsHere();
  // `watch` is the card this window is reading, so another window's ready alert
  // for it can stay quiet too. See `readySilenced`.
  if (now) soloBus.postMessage({ type: "win-focus", win: thisWindowSays, watch: watchedCard() });
  else if (hadFocus) soloBus.postMessage({ type: "win-blur", win: thisWindowSays });
  hadFocus = now;
}
window.addEventListener("focus", sayWhetherFocused);
window.addEventListener("blur", sayWhetherFocused);
document.addEventListener("visibilitychange", sayWhetherFocused);
// `pagehide` rather than `unload`, which a browser is free to skip for a tab
// it discards. Either way the beat above is the backstop.
window.addEventListener("pagehide", () => {
  if (soloBus && hadFocus) soloBus.postMessage({ type: "win-blur", win: thisWindowSays });
});
setInterval(() => { if (focusIsHere()) sayWhetherFocused(); }, focusBeat);
sayWhetherFocused();

// Whether a desktop notification can actually appear.
//
// Asked separately from sending one because `showNotification` hands back
// `null` on the service worker path whether or not anything was shown, so its
// return value cannot decide whether to fall back to a toast.
function desktopAllowed() {
  return "Notification" in window && Notification.permission === "granted";
}

if (soloBus) {
  soloBus.onmessage = e => {
    const m = e.data || {};
    // Ahead of the solo branch, because it is the one message every window
    // cares about equally. A skin is board-wide, and a popped-out terminal
    // sitting in the old colours beside a board in the new ones is the whole
    // reason this is broadcast rather than left to the next reload.
    if (m.type === "skin") {
      applySkin(m.name);
      rememberSkin(m.name === defaultSkin ? "" : m.name);
      return;
    }
    // A skin being TRIED. Painted and deliberately not remembered: nothing has
    // been chosen yet, and a window that wrote a preview to storage would come
    // back wearing a skin somebody cancelled.
    if (m.type === "skin-preview") {
      applySkin(m.name);
      return;
    }
    // A restart, learned from whichever window heard it first. A popped-out
    // window's own stream may be the one that drops before the message lands,
    // and the board is the window most likely to still be listening.
    if (m.type === "going-down") {
      if (!restartComing()) restartAt = Date.now();
      return;
    }
    // WHO IS IN FRONT. Above the solo branch because every window needs this
    // one equally: the board has to know a popped-out window is being read,
    // and that window has to know the board is.
    if (m.type === "win-focus" && m.win) {
      focusedElsewhere = { win: m.win, at: Date.now(), watch: m.watch || "" };
      return;
    }
    if (m.type === "win-blur" && m.win) {
      // Only the window that made the claim can withdraw it. Otherwise a
      // window blurring as another takes focus would erase the new claim with
      // the old one's goodbye.
      if (focusedElsewhere.win === m.win) focusedElsewhere = { win: "", at: 0 };
      return;
    }
    // SOMETHING FOR THE WINDOW IN FRONT TO SAY, raised somewhere else.
    //
    // The window that NOTICES an alert is rarely the window you are looking
    // at. It hands the toast to the one that is, addressed by name, and says
    // nothing itself.
    if (m.type === "win-toast" && m.win === thisWindowSays) {
      // A ready alert for the card this window is reading is not said here,
      // whoever raised it. Logged.
      if (m.ready && termWatching(m.taskFor)) {
        recordToLog(m.title || "", m.body || "", "stack", "", m.taskFor);
        return;
      }
      toast(m.title || "", m.body || "", m.goTo || "", m.key || null, m.taskFor || null);
      return;
    }
    // A POPPED-OUT WINDOW WAS ASKED TO GO SOMEWHERE ONLY THE BOARD CAN: an
    // alert it showed about a card that is not its own. See `landFromSolo`.
    if (m.type === "land") {
      if (!termOnly()) {
        closeOpenDialogs().then(ok => { if (ok) landOnAlert(m.taskFor || "", m.goTo || "", m.key || ""); });
      }
      return;
    }
    // A solo window alerts for its own card and nothing else, so it has no use
    // for other windows' claims as ALERTS, and answering its own roll call
    // would have it suppress itself.
    //
    // It keeps the ledger anyway, and only for the cards that are not its own.
    // The switcher is why: a popped-out window can now move to another card,
    // and moving onto one that already has a window of its own is exactly the
    // two-views-one-terminal situation `docs/terminal/supervision-design.md` says
    // nothing arbitrates. `soloSwitch` asks `poppedOut` the same question the
    // board asks, and it can only answer it if somebody wrote the claims down.
    if (termOnly()) {
      // `soloYielded` silences the answer as well as the heartbeat. A window
      // that has handed its card over still has `soloID` set, and answering a
      // roll call with it would tell the window that just took the card that
      // the holder is still there, which is the one question that answer is
      // asked to settle.
      if (m.type === "solo-who" && soloID && !soloYielded) {
        soloBus.postMessage({ type: "solo-claim", task: soloID });
      }
      if (m.task && !sameCard(m.task, soloID)) {
        if (m.type === "solo-claim") soloHeld.set(m.task, Date.now());
        if (m.type === "solo-release") soloHeld.delete(m.task);
      }
      // ANOTHER WINDOW IS TAKING THIS CARD, and this one lets go.
      //
      // The other half of the refusal in `bootTerminalOnly`. A window opened on
      // a card somebody else already holds is turned away, and turning it away
      // with nothing beside it strands whoever cannot find the holder: the
      // holding window may be behind fourteen others, on another desktop, or
      // minimised. So the refusal offers to take the card instead, and this is
      // what that costs the holder.
      //
      // The pane is torn down rather than the window closed. A window cannot
      // reliably close itself (see `closeThisWindow`), and a window that
      // announced it was closing and then sat there is worse than one that
      // says plainly what happened to it.
      //
      // `clearTermPane(true)` rather than `closeTerm`: the switching form skips
      // the restore loop, which would otherwise spend ninety seconds trying to
      // reattach the terminal this window was just asked to give up.
      if (m.type === "solo-yield" && sameCard(m.task, soloID)) {
        soloYielded = true;
        soloBus.postMessage({ type: "solo-release", task: soloID });
        clearTermPane(true);
        termWait("another window took this terminal. this one can be closed.");
        return;
      }
      // No `solo-raise` here any more, and it is worth saying why it went.
      // A window cannot raise itself: `window.focus()` in a background tab has
      // no user gesture behind it and every browser refuses it, silently. The
      // board is the one holding the click, so the board raises the window by
      // NAME. See `reopenByName`.
      return;
    }
    if (m.type === "solo-claim" && m.task) {
      soloHeld.set(m.task, Date.now());
      if (typeof growlDraw === "function") growlDraw();
      // AND THE BOARD LETS GO OF IT.
      //
      // Every claim used to be for a card the board had just popped out, and
      // `popOutTask` tears its own pane down on the way, so there was nothing
      // to yield. A window that SWITCHES arrives at a card the board may well
      // be showing, and nobody asked the board first: two views onto one
      // terminal, both taking input, which is the one thing this whole
      // ownership dance exists to prevent.
      //
      // `closeTerm` rather than `clearTermPane`, so the pane says nothing
      // attached instead of keeping the old card's title over an empty screen.
      // The restore it starts gives up immediately, because the first thing
      // `waitLoop` asks is whether the card is popped out, and the claim above
      // is already recorded.
      if (termTask && sameCard(termTask.id, m.task)) {
        rlog("another window claimed", m.task, "- letting go of the pane");
        toast("it moved into its own window", "the board let go of that terminal");
        closeTerm();
      }
    }
    if (m.type === "solo-release" && m.task) {
      soloHeld.delete(m.task);
      // The HANDLE goes too, or the other half of `poppedOut` keeps saying yes.
      // A window that switched to another card is still open and still ours,
      // so the handle outlives the claim, and the board would go on refusing to
      // attach and "raising" a window that is showing something else entirely.
      popOuts.delete(m.task);
      // The board's growler for that card stops saying it is muted, and the board rings it again.
      if (typeof growlDraw === "function") growlDraw();
    }
    // A popped-out window asking to be closed, because it cannot reliably
    // close itself.
    //
    // SAME ASYMMETRY AS RAISING, and it went unnoticed for the same reason:
    // `window.close()` is refused silently in every case where the browser
    // does not agree the script opened that window, and "does not agree" is
    // broader than it sounds. A window opened from a pasted URL was never
    // script-opened. One whose opener has been closed or has reloaded through
    // a build change may no longer count either, and none of that raises an
    // error, so the countdown finishes and the window simply stays.
    //
    // The board holds the handle it got from `window.open`, and closing
    // through that handle is the case every browser does allow. So the window
    // asks and the board acts, exactly as it does for focus.
    if (m.type === "solo-close" && m.task) {
      soloHeld.delete(m.task);
      // By handle, or by NAME when there is no handle. The board reloads
      // itself whenever the daemon serves a new build, and a reload empties
      // `popOuts` while leaving every popped window open. Without the second
      // route the close would fail on exactly the boards that have been up
      // longest, which is most of them.
      let held = popOuts.get(m.task);
      if (!held || held.closed) held = reopenByName(m.task);
      if (held && !held.closed) {
        try { held.close(); } catch (e) {}
      }
      popOuts.delete(m.task);
    }
  };
}

// The bare card id, with any `room~` tag stripped. Mirrors the hub's splitTag
// in internal/link/rooms.go: the aggregate view tags ids as `room~id` only
// while MORE THAN ONE room is attached, so one card is `room~id` with two rooms
// and bare with one. A room dropping (2 -> 1) flips every id at once, and a
// diff on the raw id reads that as every card arriving. Diffing on the bare id
// keeps a card's identity across the flip. Card ids are uuids and carry no `~`
// of their own, so the first `~` is the tag join.
function bareId(id) {
  const s = String(id);
  const i = s.indexOf("~");
  return i > 0 ? s.slice(i + 1) : s;
}

// The room a card lives in, read off the tag the hub writes on its id. The other
// half of `bareId`: the aggregate view addresses a card as `room~id` while MORE
// THAN ONE room is attached and serves it bare with one (see the hub's tagFor /
// splitTag in internal/link/rooms.go), so the tag is present EXACTLY when there
// is more than one room and telling them apart matters. Answers "" for a bare id,
// which is one room attached or scoped mode, and the badge that reads this then
// draws nothing on its own. The tag is the room name, not an id.
function roomOf(id) {
  const s = String(id);
  const i = s.indexOf("~");
  return i > 0 ? s.slice(0, i) : "";
}

// A STABLE colour per room, so sg4 and sgg are told apart at a glance rather than
// read letter by letter. Hashed from the name the same way `groupHue` colours a
// project group, then landed on a curated wheel of hues spread far enough apart
// that two rooms are always tellable, and offset from the group palette so a room
// chip does not echo the group heading beside it. A given room always gets the
// same hue and different rooms get different ones, with nothing configured. The
// chip renders it through `--rhue` and the skin decides the rest, so it stays
// legible on a paper board and a dark one alike (see `.chip.room` in cards.css).
// The palette lives inside the function so the strip's harness can lift it whole.
function roomHue(name) {
  const hues = [205, 150, 285, 25, 175, 320, 95, 250, 45, 190, 130, 350];
  const s = String(name || "");
  let h = 0;
  for (let i = 0; i < s.length; i++) h = (h * 31 + s.charCodeAt(i)) % 2147483647;
  return hues[h % hues.length];
}

// ── the master switch (item 79) ─────────────────────────
//
// "Off" silences permission requests too. Built first as no, since a session
// blocks on one until somebody answers, and clint turned it: "it's
// notifications in general" (2026-09-29). They are still listed in the drawer.
// Flipping it back is this one word.
const NOTIFY_OFF_SILENCES_PERMISSIONS = true;
const NOTIFY_OFF_KEY = "atrium.notify.off";

// Read from storage on every ask rather than held in a variable, so every
// window of this browser, popped-out ones included, follows a change with no
// message between them.
function notifyIsOff() {
  try { return localStorage.getItem(NOTIFY_OFF_KEY) === "1"; } catch (e) { return false; }
}

// A POPPED-OUT WINDOW HAS ITS OWN SWITCHES, keyed by the card it shows. The board
// never reads these, so a toggle in a pop-out cannot change the board. Keyed by
// card rather than window so closing and reopening the pop-out keeps the choice.
// `termOnly` lives in solo.js, which loads after this file, so the hash and the
// path are read here directly.
const NOTIFY_OFF_CARD_KEY = "atrium.notify.off.card:";
const SOUND_CARD_KEY = "atrium.sound.card:";
function inPopout() { return /^#term=/.test(location.hash) || cardUrlIsCard(); }
// The bare id of this pop-out's card, read off the hash each time because the
// switcher rewrites it when the window moves to another card.
function popoutCard() {
  if (!inPopout()) return "";
  if (!/^#term=/.test(location.hash)) return soloID ? bareId(soloID) : "";
  try { return bareId(decodeURIComponent(location.hash.slice("#term=".length))); } catch (e) { return ""; }
}
function popoutIsOff() {
  const id = popoutCard();
  if (!id) return false;
  try { return localStorage.getItem(NOTIFY_OFF_CARD_KEY + id) === "1"; } catch (e) { return false; }
}
function popoutSoundMuted() {
  const id = popoutCard();
  if (!id) return false;
  try { return !!JSON.parse(localStorage.getItem(SOUND_CARD_KEY + id) || "{}").muted; } catch (e) { return false; }
}

// THE ONE ANSWER to "is this window silent", for everything that alerts. The
// board is silent when the board-wide switch is off. A pop-out is silent when
// that OR its own card's switch is off, and says which in the drawer.
function windowIsOff() { return notifyIsOff() || popoutIsOff(); }

function setNotifyOff(off) {
  // In a pop-out the toggle writes the card's key and never the board's.
  const key = inPopout() ? (popoutCard() ? NOTIFY_OFF_CARD_KEY + popoutCard() : "") : NOTIFY_OFF_KEY;
  try {
    if (key) {
      if (off) localStorage.setItem(key, "1");
      else localStorage.removeItem(key);
    }
  } catch (e) {}
  paintNotifyOff();
}

// What the drawer's toggle does: a pop-out cannot flip the board-wide switch, so
// with that one off the toggle is inert and says where to change it.
function notifyToggle() {
  if (inPopout() && notifyIsOff()) return;
  setNotifyOff(!windowIsOff());
}

// Whether this alert is held back. A permission request is held too while the
// constant above says so. `goTo` is how `notify` tells one apart.
function notifyHeld(goTo) {
  if (!windowIsOff()) return false;
  if (goTo === "perms" && !NOTIFY_OFF_SILENCES_PERMISSIONS) return false;
  return true;
}

// The bell, the drawer's toggle and the gear's checkbox, all repainted from the
// one stored answer. The tip and the aria-label are the same text.
function paintNotifyOff() {
  const boardOff = notifyIsOff();
  const off = windowIsOff();
  const popout = inPopout();
  const bell = document.getElementById("toastlog-open");
  if (bell) {
    const g = bell.querySelector(".glyph");
    if (g) g.textContent = off ? "\u{1F515}" : "\u{1F514}";
    const tip = (off ? "notifications are off. click to see what arrived" : "what the board has told you") +
      (typeof phoneBellAutoNote === "function" ? phoneBellAutoNote() : "");
    bell.dataset.tip = tip;
    bell.setAttribute("aria-label", tip);
    bell.classList.toggle("off", off);
    if (typeof phoneBellPaint === "function") phoneBellPaint();
  }
  const t = document.getElementById("toastlog-toggle");
  if (t) {
    t.textContent = off ? "turn on" : "turn off";
    let tip = off
      ? "notifications are off. turn them back on"
      : "hold back toasts, desktop notifications and sound. what arrives is still listed here" +
        (NOTIFY_OFF_SILENCES_PERMISSIONS ? "" : ". permission requests still come through");
    if (popout) {
      t.textContent = boardOff ? "off for the board" : off ? "turn on" : "turn off";
      t.disabled = boardOff;
      tip = boardOff
        ? "off for the whole board, change it on the board"
        : off
          ? "off for this window. turn it back on"
          : "hold back this window's toasts, desktop notifications and sound. the board keeps its own settings";
    }
    t.dataset.tip = tip;
    t.setAttribute("aria-label", tip);
  }
  const scope = document.getElementById("toastlog-scope");
  if (scope) {
    scope.hidden = !popout || !off;
    scope.textContent = boardOff ? "off for the whole board, change it on the board" : "off for this window";
  }
  const c = document.getElementById("s-notifyoff");
  if (c) c.checked = boardOff;
}
// Another window of this browser flipped it. A pop-out hears the board's switch
// and its own card's, and the board hears only its own.
window.addEventListener("storage", e => {
  if (e.key === NOTIFY_OFF_KEY || (e.key && e.key.startsWith(NOTIFY_OFF_CARD_KEY))) paintNotifyOff();
});
addEventListener("DOMContentLoaded", paintNotifyOff);

// Whether THIS window is showing this card's terminal right now: a pop-out for
// it, or the board on the terminals view with it attached. Said to the ready
// alert, which has nothing to tell someone reading the screen it is about.
function termWatching(id) {
  try {
    if (!id || !term || !termTask || !sameCard(termTask.id, id)) return false;
    return termOnly() || isViewing("terms");
  } catch (e) { return false; }
}

// PTY OUTPUT IS ACTIVITY, for the one card this window has attached. A ready
// alert waits for quiet and the window reading a terminal is the one that sees
// it move. At most once a second and it makes no request.
let readyQuietFedAt = 0;
function feedReadyQuiet() {
  const now = Date.now();
  if (now - readyQuietFedAt < 1000 || !termTask) return;
  readyQuietFedAt = now;
  alerting.activity(termTask.id);
}

const alerting = (() => {
  let ctx = null;
  let prefs = loadPrefs();
  // Ids we have already alerted for, so a 5s refresh does not re-ring.
  //
  // A MAP RATHER THAN A VARIABLE PER KIND. There were two, named, and every
  // site that read one did it with a ternary on the kind, so adding a third
  // meant editing four places to get one new alert. `null` until the first
  // pass, which is what makes a reload silent about what was already there.
  const known = {};

  // Set from `/v1/health` while the daemon is bringing sessions back. See
  // internal/daemon/settling.go.
  let settlingNow = false;

  // A one-shot marker: take the next check of this kind as the baseline and
  // announce nothing. Written into `known[kind]` by `reseed` when the attached
  // room set changes. See the header on `reseed`.
  const RESEED = "\0reseed";

  // A pop-out mutes its own card and leaves the board's sound alone.
  const muted = () => inPopout() ? popoutSoundMuted() : !!prefs.muted;

  const btn = document.getElementById("sound");
  const paint = () => {
    btn.classList.toggle("muted", muted());
    btn.innerHTML = muted() ? "&#128263;" : "&#9835;";
    btn.dataset.tip = inPopout()
      ? (muted() ? "muted for this window. click for sound" : "sound on. click to mute this window")
      : (muted() ? "muted. click for sound" : "sound on. click to mute");
    btn.setAttribute("aria-label", btn.dataset.tip);
    // The same switch from the bell's drawer, which is where a phone reaches it with the header folded away.
    const dt = document.getElementById("toastlog-sound");
    if (dt) dt.textContent = muted() ? "sound off" : "sound on";
  };
  paint();
  window.addEventListener("storage", e => {
    if (e.key && e.key.startsWith(SOUND_CARD_KEY)) { paint(); paintAudioState(); }
  });

  const save = () => {
    localStorage.setItem("atrium.sound", JSON.stringify(prefs));
    paint();
  };

  // Browsers refuse to make noise until the page has been interacted with, so
  // the context is built on a gesture and reused after that. WHICH EVENTS COUNT IS A BROWSER RULE: a touch's
  // pointerdown and touchstart do not activate the page, its touchend, pointerup and click do, and iOS only lets
  // a context start from inside one of those. So every one of them tries, and the first that the browser accepts
  // wins. Listening to pointerdown alone left a phone locked until its second touch, or for good.
  const unlock = () => {
    if (!ctx) {
      const AC = window.AudioContext || window.webkitAudioContext;
      if (AC) {
        ctx = new AC();
        if (ctx.addEventListener) ctx.addEventListener("statechange", () => paintAudioState());
      }
    }
    if (ctx && ctx.state === "suspended") {
      const r = ctx.resume();
      if (r && r.then) r.then(paintAudioState, () => {});
    }
    paintAudioState();
  };
  ["pointerdown", "pointerup", "touchend", "click", "keydown"].forEach(t =>
    document.addEventListener(t, unlock, { capture: true, passive: true }));
  // A reload resets the audio context, and a browser will not let it start
  // until the page has been interacted with. Without a visible signal, silence
  // looks like a broken feature rather than a browser rule.
  document.addEventListener("visibilitychange", () => {
    if (document.visibilityState === "visible" && ctx && ctx.state === "suspended") ctx.resume();
  });

  function audioBlocked() {
    return !muted() && (!ctx || ctx.state !== "running");
  }
  function paintAudioState() {
    const el = document.getElementById("sound");
    el.classList.toggle("blocked", audioBlocked());
    if (audioBlocked()) el.dataset.tip = "click anywhere on the page once to let the browser play sound";
    paintSoundHint(audioBlocked());
  }
  // A touch device has no hover and may not show the sound button, so it is told in words while the context is locked.
  function paintSoundHint(blocked) {
    let h = document.getElementById("sound-hint");
    const want = blocked && typeof phoneToasts === "function" && phoneToasts();
    if (!want) { if (h && !h.dataset.press) h.hidden = true; return; }
    if (!h) {
      h = document.createElement("button");
      h.type = "button";
      h.id = "sound-hint";
      h.textContent = "tap to enable sound";
      // A tap on the pill enables sound and does nothing else. It is a real target, so a tap does not fall through to
      // the card row, key bar or composer under it. The capture listeners above have already unlocked by now.
      // The press is marked from the window's capture phase, ahead of the unlock listeners, because unlocking takes
      // the pill down on pointerup and the click would then land on the row that was under it.
      const release = () => { delete h.dataset.press; };
      const press = e => { if (e.target === h) h.dataset.press = "1"; else if (h.dataset.press) { release(); paintSoundHint(audioBlocked()); } };
      ["pointerdown", "touchstart", "mousedown"].forEach(t => window.addEventListener(t, press, true));
      // A press that slides off the pill gets a pointerup but no click, so the mark would stay and keep the pill up.
      // Let go of it when the release is outside the pill, and only then: a release on the pill is followed by its click.
      const lift = e => {
        if (!h.dataset.press) return;
        const pt = e.changedTouches && e.changedTouches[0] || e, b = h.getBoundingClientRect();
        if (pt.clientX < b.left || pt.clientX > b.right || pt.clientY < b.top || pt.clientY > b.bottom) { release(); paintSoundHint(audioBlocked()); }
      };
      ["pointerup", "touchend", "mouseup"].forEach(t => window.addEventListener(t, lift, true));
      h.addEventListener("pointercancel", release);
      h.addEventListener("touchcancel", release);
      h.addEventListener("click", e => { e.preventDefault(); e.stopPropagation(); release(); unlock(); h.hidden = true; });
      document.body.appendChild(h);
    }
    h.hidden = false;
  }
  // First paint, and again shortly after load in case the context settles.
  setTimeout(paintAudioState, 300);

  function note(freq, start, dur, gain, type) {
    const osc = ctx.createOscillator(), amp = ctx.createGain();
    osc.type = type || "sine";
    osc.frequency.setValueAtTime(freq, ctx.currentTime + start);
    const peak = Math.max(0.0001, gain * prefs.volume);
    // Shaped rather than square-edged, because a click on every alert grates.
    amp.gain.setValueAtTime(0, ctx.currentTime + start);
    amp.gain.linearRampToValueAtTime(peak, ctx.currentTime + start + 0.015);
    amp.gain.exponentialRampToValueAtTime(0.0001, ctx.currentTime + start + dur);
    osc.connect(amp).connect(ctx.destination);
    osc.start(ctx.currentTime + start);
    osc.stop(ctx.currentTime + start + dur + 0.02);
  }

  // preview plays a named sound regardless of mute, so the test buttons in
  // settings always do something.
  function preview(name) {
    unlock();
    const s = SOUNDS[name];
    if (!s || !ctx || ctx.state !== "running") return;
    s.notes.forEach(n => note(n[0], n[1], n[2], n[3], n[4]));
  }

  // `sound` is the card's own tone when it has one.
  //
  // The point of giving a session its own tone is knowing which one it was
  // without looking, so the card's choice wins over the board default for
  // both kinds of alert. Losing the ability to tell a permission from a ready
  // by ear is the trade, and it is the right way round: which agent is the
  // fact you cannot get any other way while your back is turned.
  function play(kind, sound) {
    if (muted()) return;
    // A blocker holds an agent at the door like a permission, so it rings the
    // permission tone.
    const perm = kind === "permission" || kind === "blocker";
    if (notifyHeld(perm ? "perms" : "")) return;
    preview(sound || (perm ? prefs.perm : prefs.input));
  }

  // `taskFor` is the card to SUPPRESS for: a popped-out window speaks for its
  // own card, so the board must not. `artFor` is the card the picture belongs
  // to, which is the same thing everywhere except in that window, where they
  // are deliberately different.
  // ONE ALERT, PUT WHERE YOU ARE. See the header above `thisWindowSays`.
  //
  // Every caller hands the whole alert to this and nothing else, because the
  // choice between a toast and a desktop notification is one decision and it
  // cannot be made twice. Callers used to make it themselves and then call
  // `toast` as well, which is how the same event reached you in two forms.
  //
  // `opts.quiet` suppresses ONLY the toast in this window, and only when this
  // window has focus. It is for an alert whose subject is already on screen
  // with its own buttons: a floating copy of the first 120 characters is a
  // panel drawn over the answer. It has no bearing on the other two cases,
  // where by definition you are not looking at anything here.
  //
  // `opts.pending` says the subject is a pending item, a card waiting on you or
  // a request, so the toast and the notification go once it is answered.
  function notify(title, body, goTo, permId, subject, taskFor, mark, artFor, opts) {
    opts = opts || {};
    // A card whose terminal is popped out is spoken for by that window, which
    // is the only document that can be looked at instead of this one. Decides
    // WHO RAISES the alert. Where it lands is the rest of this function.
    if (taskFor && poppedOut(taskFor)) return;

    // A GROWLER ALREADY SITS ON THE BOARD, so it never toasts: what it asks for is the operating system's
    // attention when nobody is looking. The tone is the caller's, through `play`. The toast log line is the
    // growler's own, written once by js/growl.js, so this path records nothing. See the header above.
    if (opts.growl) {
      if (notifyHeld(goTo) || focusIsHere() || focusIsElsewhere()) return;
      if (!prefs.muted && prefs.desktop !== false && desktopAllowed()) {
        showNotification(title, body, goTo, permId, subject, mark, taskFor || artFor || null, opts.growl.key || "", {
          tag: "atrium-growl:" + opts.growl.id, sticky: true, retire: "growl:" + opts.growl.id,
          onShown: opts.growl.onShown
        });
      }
      return;
    }

    // THE MASTER SWITCH (item 79), over item 44's filter and over a card's own
    // tone. HELD BACK MEANS RECORDED: no toast, no desktop notification and no
    // sound, but the drawer and the bell's badge still get the entry, once.
    if (notifyHeld(goTo)) {
      logNotification(title, body, goTo, permId || (opts.pending ? subject : "") || null,
        taskFor || artFor || null);
      return;
    }

    // THE KEY IS WHAT RETIRES IT, so only a pending item gets one. `reapToasts`
    // takes down every keyed toast whose key is not waiting or pending, in the
    // same poll that raised it. Keyed by any subject, "X is on the board", a
    // stuck agent, a share that stopped and a fixture that failed all popped
    // and went in the same breath, and their desktop notifications with them.
    const key = permId || (opts.pending ? subject : "") || null;
    // The card a click lands on, which is not the suppression key: a popped-out
    // window leaves `taskFor` empty so as not to silence itself, and names its
    // card in `artFor`. See `landOnAlert`.
    const landOn = taskFor || artFor || null;

    // 1. YOU ARE LOOKING AT THIS WINDOW. The toast is the whole message, and a
    // second copy from the operating system is noise.
    if (focusIsHere()) {
      if (!opts.quiet) toast(title, body, goTo, key, landOn);
      return;
    }

    // 2. YOU ARE LOOKING AT ANOTHER ATRIUM WINDOW. It toasts, this one says
    // nothing at all. Addressed by name rather than broadcast, or every window
    // open would draw the same toast.
    const elsewhere = focusIsElsewhere();
    if (elsewhere && soloBus) {
      soloBus.postMessage({
        type: "win-toast", win: elsewhere, title, body: body || "",
        goTo: goTo || "", key: key || "", taskFor: landOn || "", ready: !!opts.ready
      });
      return;
    }

    // 3. NOBODY IS LOOKING. Windows says it, and no window toasts.
    //
    // Recorded FIRST, because this is the one path that never reaches `toast`
    // and so the one the toast log never saw. A desktop alert fired while every
    // window was unfocused rang and popped and then left no trace: the whole
    // symptom this fixes. Cases 1, 2 and 4 all land on a `toast`, here or in a
    // sibling window sharing this origin's localStorage, and the wrapper in
    // toast-log.js records those. Only this branch has to say so itself.
    if (!muted() && prefs.desktop !== false && desktopAllowed()) {
      logNotification(title, body, goTo, key, landOn);
      showNotification(title, body, goTo, permId, subject, mark, landOn, key);
      return;
    }

    // Nothing can reach you: notifications are off, denied, or muted. The
    // toast is then a RECORD rather than a message, and it is worth one,
    // because the alternative is an event that happened and left no trace in
    // the log you would go looking through afterwards.
    toast(title, body, goTo, key, landOn);
  }

  btn.onclick = () => {
    if (inPopout()) {
      const id = popoutCard();
      if (id) {
        try { localStorage.setItem(SOUND_CARD_KEY + id, JSON.stringify({ muted: !popoutSoundMuted() })); } catch (e) {}
      }
      paint();
    } else {
      prefs.muted = !prefs.muted;
      save();
    }
    if (!muted()) {
      unlock();
      play("waiting");
    }
  };

  // A request nobody answers freezes an agent indefinitely. One alert when it
  // arrives is not enough, because the alert is easy to miss and the cost of
  // missing it is an agent doing nothing for an hour. So keep asking, at a
  // widening interval so it stays a reminder rather than an assault.
  //
  // Declared BEFORE the return, and this is not a style point. Everything
  // below that return is unreachable: `function nag` is hoisted so it stays
  // callable, but a `const` there never runs, and every call to nag threw
  // "cannot access nagged before initialization" from inside a promise. The
  // throw took the render with it and the panel came up blank. It went unseen
  // because nag only fires when a permission has been waiting a minute, and
  // with auto mode on that is almost never.
  //
  // A parser cannot catch this: it is valid JavaScript that fails when run.
  const nagged = {};

  // State for the once-per-wait rule below, declared up here for the same
  // reason as `nagged`: a `const` under the return never runs.
  const rung = new Map();
  const RUNG_KEEP_MS = 3600000;
  const RUNG_CAP = 1000;
  const held = new Map();
  const lastActive = new Map();
  let liveWaits = new Set();
  // Held alerts, one bucket per kind. Permissions and ready cards are never
  // merged into one alert: they say different things and want different
  // amounts of hurry.
  const pending = {};

  return {
    check, nag, play, preview, save, unlock, notify, settling, reseed, activity, sinceActive, quietMs,
    get: () => prefs,
    set: (patch) => { Object.assign(prefs, patch); save(); }
  };

  // Re-seed the baseline on the next check of every kind, silently.
  //
  // A room attaching or detaching legitimately churns the whole card set, and
  // when the count crosses 1<->2 it also flips every id between `room~id` and
  // bare (see the hub's tagFor/splitTag and `bareId`). That is the same churn a
  // restart causes, so notify should take the next set as its new baseline
  // rather than diff on it. `settlingNow` does not cover this: the hub did not
  // restart and the aggregate stream stayed open, so `es.onopen` never fired.
  // Called from `loadHubRooms` when the attached room set changes.
  //
  // Only kinds already being tracked are marked. A kind not yet seen seeds
  // silently on its own first pass anyway.
  function reseed() {
    Object.keys(known).forEach(kind => { known[kind] = RESEED; });
  }

  // Whether the daemon is still putting back what it had before it restarted.
  //
  // Held here rather than read at each call site, because the thing that
  // learns it is the health poll and the thing that needs it is `check`, and
  // those are two different requests that do not arrive in a fixed order.
  function settling(on) {
    if (on === undefined) return settlingNow;
    settlingNow = !!on;
  }
  // How many steps of the backoff `mins` minutes of waiting have passed.
  function nagSlot(mins) {
    const steps = [1, 2, 5, 10, 30, 60, 120, 240, 480, 1440];
    let n = steps.filter(s => mins >= s).length;
    if (mins > 1440) n += Math.floor((mins - 1440) / 1440);
    return n;
  }
  function nag(perms) {
    const now = Date.now();
    perms.forEach(p => {
      const since = Date.parse((p.requested_at || "").endsWith("Z")
        ? p.requested_at : p.requested_at + "Z");
      if (isNaN(since)) return;
      const waitedMs = now - since;
      if (waitedMs < 60000) return;
      // A growler is on screen for this request and its reminders are the growler's.
      if (typeof growlHas === "function" && growlHas(p.id)) return;

      // THE OPERATOR'S BACKOFF: 1m, 2m, 5m, 10m, 30m, 1h, 2h, 4h, 8h, 24h, then
      // daily. The same schedule the room uses for a silent stop and a stuck
      // tool (EscalationBackoff in internal/daemon/a2a.go), so every kind of
      // stuck rings on one rhythm. Keyed on the request, so a new one starts
      // over.
      const mins = Math.floor(waitedMs / 60000);
      const slot = nagSlot(mins);
      if (nagged[p.id] === slot) return;

      // Asked before the slot is claimed, because an undecidable answer has to
      // leave the slot alone. Claiming it and then bailing out would spend the
      // minute's one alert on a tick that raised nothing.
      const showing = permShowing(p.id);
      if (showing === "unknown") return;
      nagged[p.id] = slot;

      play("permission", soundForAlert(p));
      const body = `${p.tool}: ${(p.command || "").slice(0, 120)}`;
      const who = p.agent || "an agent";
      // `quiet` when the request's own row is on screen, with its command and
      // its four buttons. A floating copy of the first 120 characters is not
      // news there, it is a panel drawn over the answer.
      //
      // On screen, not merely on the perms tab. A queue of six on a phone is
      // taller than the phone, and the one frozen twelve minutes may be well
      // below the fold, where the toast is the only thing that would take you
      // to it. It only silences the toast in THIS window: a nag that reaches
      // Windows, or another window, is not about what is on screen here.
      notify(`${who} is STUCK on a permission, ${mins} minutes`, body, "perms", p.id, p.id,
        p.task_id || p.id, iconForAlert(p), "", { quiet: showing === "on-screen", pending: true });
    });
    // Forget anything answered, so a later request with a fresh id starts over.
    const live = new Set(perms.map(p => p.id));
    Object.keys(nagged).forEach(id => { if (!live.has(id)) delete nagged[id]; });
  }

  // check diffs an incoming set of ids against what we alerted on last time.
  // The first pass only seeds, so opening the page does not fire for a queue
  // that was already there.
  function check(kind, items, describe) {
    // Diffed on the BARE id, so a card keeps its identity whether the aggregate
    // view tags it `room~id` (two rooms) or leaves it bare (one). See `bareId`.
    const ids = new Set(items.map(i => bareId(i.id)));
    trackWaits(kind, items);
    // A ROOM SET CHANGE IS NOT A PILE OF NEW AGENTS. `reseed` marked this kind
    // when a room attached or detached, so take this set as the new baseline
    // and say nothing, the same way settling and a fresh page load do.
    if (known[kind] === RESEED) {
      known[kind] = ids;
      rememberRung(kind, items);
      return;
    }
    // A RESTART IS NOT A PILE OF NEW AGENTS. While the daemon says it is still
    // coming up, a card arriving or going quiet is a session being put back
    // rather than something that just happened, so this re-seeds instead of
    // diffing, which is what a fresh page load does and for the same reason.
    //
    // BOTH `arrived` AND `waiting`. The first pass only covered arrivals, and
    // what came through instead was "X is ready" for a session that had
    // finished its turn BEFORE the restart: the card was marked dead when its
    // pid went, came back on the reopen, and reported the same state it was
    // already in. `justStarted` does not cover it, because the card is old.
    //
    // PERMISSIONS STILL RING. An agent that comes back up already blocked is
    // frozen right now, and that is the one thing during a restart worth being
    // interrupted for.
    if (settlingNow && (kind === "arrived" || kind === "waiting" || kind === "looksidle")) {
      known[kind] = ids;
      rememberRung(kind, items);
      return;
    }
    const prev = known[kind] === undefined ? null : known[kind];
    if (prev === null) {
      known[kind] = ids;
      rememberRung(kind, items);
      // A permission that is already pending when the page loads still needs
      // answering. Staying silent about it meant every reload swallowed the
      // alert for whatever was already blocked, which during a working session
      // is most of them.
      // The same rule as `announce`: a card with its own window is announced
      // by that window. This is the path a RELOAD takes, which is every
      // restart, so getting it wrong here is what you actually hear.
      const first = items.filter(i => !poppedOut(i.task_id || i.id));
      if (kind === "permission" && first.length) {
        play(kind);
        const d = describe(first[0]);
        notify(first.length > 1 ? `${first.length} agents need permission` : d.title,
          d.body, "perms", first.length === 1 ? first[0].id : "",
          first.length === 1 ? first[0].id : "",
          first.length === 1 ? (first[0].task_id || first[0].id) : "",
          first.length === 1 ? iconForAlert(first[0]) : "", "", { pending: true });
      }
      // The same for a blocker: a card still held at a prompt after a reload is
      // still held.
      if (kind === "blocker" && first.length) {
        play(kind);
        const d = describe(first[0]);
        notify(first.length > 1 ? `${first.length} launched agents are BLOCKED` : d.title,
          d.body, "stack", "", first.length === 1 ? first[0].id : "",
          first.length === 1 ? (first[0].task_id || first[0].id) : "",
          first.length === 1 ? iconForAlert(first[0]) : "", "", { pending: true });
      }
      return;
    }
    let fresh = items.filter(i => !prev.has(bareId(i.id)));
    known[kind] = ids;
    // A WAIT THAT HAS RUNG NEVER RINGS AGAIN. Membership flaps (a card out of
    // the set for one pass and back) make a card look fresh with the same wait.
    fresh = fresh.filter(i => !rungHas(kind, i));
    if (!fresh.length) return;
    // A READY ALERT IS HELD UNTIL THE CARD HAS BEEN QUIET. See `holdWaits`.
    if (kind === "waiting") {
      holdWaits(fresh, describe);
      return;
    }
    deliver(kind, fresh, describe);
  }

  // ── a wait rings once, and only after the card went quiet ─────────────
  //
  // The ready alert rang 17 times for one card in four minutes. `check` diffs
  // on id membership, so anything that drops a row out of the waiting set for a
  // single pass (offline, a partial row, needs-input to needs-permission and
  // back, the merged view losing a room for a moment) brought it back as
  // fresh. And a busy agent that ends short turns really is waiting, briefly,
  // many times.
  //
  // KEYED ON THE WAIT, not the card: bare id plus `waiting_since`. The same
  // wait never rings twice however often it leaves and re-enters the set. A new
  // `waiting_since` is a new wait and may ring. `looksidle` is keyed on the
  // flag time already, so it only needs the memory and not the hold.
  function rungKey(kind, i) {
    if (kind === "waiting") return bareId(i.id) + "|" + (i.waiting_since || "");
    if (kind === "looksidle") return bareId(i.id);
    return "";
  }
  function rungHas(kind, i) {
    const k = rungKey(kind, i);
    return !!k && rung.has(k);
  }
  function rememberRung(kind, items) {
    items.forEach(i => { const k = rungKey(kind, i); if (k) rung.set(k, Date.now()); });
  }
  // Every pass: what is waiting now, and which remembered waits are over.
  function trackWaits(kind, items) {
    if (kind !== "waiting" && kind !== "looksidle") return;
    const now = Date.now();
    const live = new Set(items.map(i => rungKey(kind, i)));
    const liveIds = new Set(items.map(i => bareId(i.task_id || i.id)));
    if (kind === "waiting") {
      liveWaits = live;
      held.forEach((h, k) => { if (!live.has(k)) { clearTimeout(h.timer); held.delete(k); } });
    }
    live.forEach(k => { if (rung.has(k)) rung.set(k, now); });
    const mine = k => kind === "waiting" ? k.includes("|") : !k.includes("|");
    rung.forEach((at, k) => {
      if (!mine(k) || live.has(k)) return;
      const id = k.split("|")[0];
      // The card is here under a different wait, or has been gone a while.
      if (liveIds.has(id) || now - at > RUNG_KEEP_MS) rung.delete(k);
    });
    while (rung.size > RUNG_CAP) rung.delete(rung.keys().next().value);
  }

  // SECONDS OF QUIET BEFORE A READY ALERT, overridable so a headless test does
  // not wait for real ones. A plain board never sets it.
  // A function declaration, because a `const` under the return never runs.
  function quietMs() { return (typeof window !== "undefined" && window.__atriumReadyQuietMs) || 5000; }

  // How long a card has been quiet, in ms. THE ONE PLACE to feed the room's real
  // pty-output time once it is published: until then the only signal is an
  // `activity` change heard on the event stream (see `activity` below).
  function quietFor(i) {
    const at = Math.max(lastActive.get(bareId(i.task_id || i.id)) || 0, (held.get(rungKey("waiting", i)) || {}).at || 0);
    return Date.now() - at;
  }

  // The board heard something from this card. Any of it restarts the quiet.
  function activity(id) {
    if (id) lastActive.set(bareId(id), Date.now());
  }

  // Ms since this card was last heard doing anything, for a window that holds
  // its own card and so has no held entry to measure from. Infinity if never.
  function sinceActive(id) {
    const at = lastActive.get(bareId(id));
    return at ? Date.now() - at : Infinity;
  }

  function holdWaits(fresh, describe) {
    fresh.forEach(i => {
      const k = rungKey("waiting", i);
      if (held.has(k)) { held.get(k).item = i; return; }
      held.set(k, { item: i, at: Date.now(), timer: 0 });
    });
    settleWaits(describe);
  }

  // One timer per held wait. A timer that fires announces everything held that
  // is quiet by then, so two quiet waits are one alert, and puts back any that
  // heard activity in the meantime.
  function settleWaits(describe) {
    held.forEach((h, k) => {
      if (h.timer) return;
      h.describe = describe || h.describe;
      const left = Math.max(0, quietMs() - quietFor(h.item));
      h.timer = setTimeout(() => { h.timer = 0; fireWaits(); }, left);
    });
  }
  function fireWaits() {
    const due = [];
    let again = false;
    held.forEach((h, k) => {
      clearTimeout(h.timer);
      h.timer = 0;
      if (!liveWaits.has(k)) { held.delete(k); return; }
      if (quietFor(h.item) >= quietMs()) due.push([k, h]);
      else again = true;
    });
    due.forEach(([k]) => { held.delete(k); rung.set(k, Date.now()); });
    if (again) settleWaits();
    if (due.length) deliver("waiting", due.map(([, h]) => h.item), due[0][1].describe);
  }

  function deliver(kind, fresh, describe) {
    // Held briefly, so several agents finishing together are one alert rather
    // than a burst. Zero means announce now, which is the default: a delay
    // between something needing you and being told is a real cost, and it is
    // only worth paying when the pile-up is worse.
    const holdMs = Math.round((Number(prefs.debounce) || 0) * 1000);
    if (holdMs <= 0) {
      announce(kind, fresh, describe);
      return;
    }
    // A later arrival extends the window rather than starting a second one,
    // which is what makes three finishing over four seconds one alert. Capped
    // at three windows, or a steady trickle would postpone the alert forever
    // and the setting would read as "never tell me".
    const p = pending[kind] || (pending[kind] = { items: [], describe, since: Date.now(), timer: 0 });
    p.items = p.items.concat(fresh);
    p.describe = describe;
    clearTimeout(p.timer);
    const waited = Date.now() - p.since;
    p.timer = setTimeout(() => {
      pending[kind] = null;
      announce(kind, p.items, p.describe);
    }, Math.max(0, Math.min(holdMs, holdMs * 3 - waited)));
  }

  // Says one thing about a set of items that all arrived together.
  function announce(kind, fresh, describe) {
    // A CARD WITH ITS OWN WINDOW IS THAT WINDOW'S TO ANNOUNCE, and that means
    // the sound and the toast as well as the notification.
    //
    // The rule was written down and then applied in exactly one place:
    // `notify` skipped the desktop notification for a popped-out card, and the
    // two lines around it went ahead and rang and toasted anyway. So a session
    // in its own window produced two chimes and two toasts, one from each
    // document, and the suppression that existed made no observable
    // difference.
    //
    // Dropped here rather than in `check`, so those cards are still recorded
    // as seen: they were announced, by the window that owns them.
    fresh = fresh.filter(i => !poppedOut(i.task_id || i.id));
    // A FOCUSED WINDOW SHOWING THE CARD SAYS NOTHING ABOUT IT, in every window:
    // no toast, no sound, and none forwarded. See `readySilenced`. Logged, since
    // the log is the record.
    if (kind === "waiting" || kind === "looksidle") {
      fresh = fresh.filter(i => {
        if (!readySilenced(i.task_id || i.id)) return true;
        const q = describe(i);
        recordToLog(q.title, q.body, "stack", "", i.task_id || i.id);
        return false;
      });
    }
    // A CARD AN AGENT LAUNCHED IS LOGGED AND NOT SAID. Its launcher already
    // hears through `notifyLauncher`, so a toast, a sound or a desktop
    // notification from it is noise. Dropped before `play` and before the count
    // in the title, so a pile of three with one worker is a pile of two. The log
    // line stays, because it is the record that the event happened.
    fresh = fresh.filter(i => {
      if (!quietDoer(kind, i)) return true;
      const q = describe(i);
      recordToLog(q.title, q.body, kind === "permission" ? "perms" : kind === "pulls" ? "pulls" : "stack", "", i.task_id || i.id);
      return false;
    });
    if (!fresh.length) return;
    const d = describe(fresh[0]);
    // One card's own tone when there is exactly one. A pile has no single
    // agent to sound like, so it falls back to the board default.
    play(kind, fresh.length === 1 ? soundForAlert(fresh[0]) : "");
    // Several at once still says what they are. "3 things need you" made an
    // agent blocked mid-tool and one that just finished its turn read the
    // same, and those want different amounts of hurry.
    const title = fresh.length > 1
      ? `${fresh.length} ${{
        permission: "agents need permission",
        arrived: "new agents on the board",
        stuck: "launched agents are stuck",
        blocker: "launched agents are BLOCKED",
        looksidle: "agents look idle (no turn-end received)",
        pulls: "reviews are waiting on you",
      }[kind] || "agents are ready"}`
      : d.title;
    // A pile names who, since the count alone does not, and the names are the
    // reason to look now rather than in a minute.
    const body = fresh.length > 1 ? whoseNames(fresh) : d.body;
    // Buttons only make sense for a single named permission, not a pile.
    const actionable = kind === "permission" && fresh.length === 1 ? fresh[0].id : "";
    // One at a time gets its own notification. A summary of several replaces
    // whatever summary was there, which is what you want from a count.
    const subject = fresh.length === 1 ? fresh[0].id : "";
    // ONE CALL, and it decides the form. The toast that used to follow this
    // line is now case 1 inside it: a board in front of you toasts and rings
    // nothing, a board behind something hands the toast to whichever atrium
    // window you are actually reading, and a board nobody is looking at rings
    // Windows instead. Two calls could not express that, and what they did
    // instead was both at once.
    //
    // Pending only for the two kinds that are answered. An arrival or a stuck
    // step is news, and it stays for its full life.
    notify(title, body, kind === "permission" ? "perms" : kind === "pulls" ? "pulls" : "stack", actionable, subject,
      fresh.length === 1 ? (fresh[0].task_id || fresh[0].id) : "",
      fresh.length === 1 ? iconForAlert(fresh[0]) : "", "",
      { pending: kind === "permission" || kind === "waiting" || kind === "blocker",
        ready: kind === "waiting" || kind === "looksidle" });
  }
})();

// The names behind a count, trimmed so a notification stays one line.
function whoseNames(items) {
  const names = items.map(i => i.agent || i.display_title || "an agent");
  if (names.length <= 3) return names.join(", ");
  return `${names.slice(0, 3).join(", ")} and ${names.length - 3} more`;
}

// Whether an alert about this card is to be logged and not said.
//
// The gear's "don't notify me about cards an agent launched", ticked by default.
// `kind` is the alert's kind, and a permission is never muted: it blocks until a
// human answers, so silence there is a frozen agent. A card with a tone of its
// own is the per-card override. The board has no per-card on/off for alerts, and
// choosing a tone for one card is the one way a card says it wants to be heard.
function quietDoer(kind, item) {
  if (kind === "permission" || kind === "perm") return false;
  if (alerting.get().quietDoers === false) return false;
  return isDoer(item) && !(item.sound && item.sound !== "none");
}

// A card's own tone, for an alert that is about exactly one card.
//
// Both shapes carry it: a task has its own sound, and a permission is given
// the asking card's by the server, which is already joining that row for the
// agent name. Empty means the board default, which is what every card has
// until it is given one.
function soundForAlert(item) { return item.sound || ""; }

// ── desktop notifications ───────────────────────────────
// Windows renders these itself, so the only things we control are the icon,
// the text, whether it persists, and what a click does. An icon has to be a
// raster image, so the mark is drawn to a canvas once and reused.
// Two marks, distinguishable at a glance: a permission blocks an agent and a
// waiting task does not.
const notifIcons = {};
function iconDataURL(kind, mark) {
  kind = kind === "perm" ? "perm" : "atrium";
  // A card's own mark, drawn instead of the A. The kind is still carried, by
  // the field it is drawn on: amber for an agent that is blocked, dark for one
  // that is merely ready. So the notification says both which session and how
  // urgent, where before it said neither.
  //
  // Same trade the tone already makes: which agent is the fact you cannot get
  // any other way while your back is turned, so it wins the picture, and the
  // kind falls back to the color.
  mark = String(mark || "").trim();
  const key = kind + ":" + mark;
  if (notifIcons[key]) return notifIcons[key];
  const c = document.createElement("canvas");
  c.width = c.height = 128;
  const g = c.getContext("2d");
  g.fillStyle = "#0B1B2E";
  g.fillRect(0, 0, 128, 128);
  g.lineCap = "round";
  g.lineJoin = "round";

  if (mark) {
    if (kind === "perm") {
      g.fillStyle = "#FFC64D";
      g.fillRect(0, 0, 128, 128);
    }
    // Drawn, never inserted. Whatever is on the card arrives here as pixels,
    // so an icon is one thing that can never be markup however it was set.
    //
    // Sized down when it is wider than one glyph, since a card set to a short
    // word should shrink rather than run off the edge. Measured rather than
    // guessed from length: an emoji is several code units and one glyph.
    g.fillStyle = kind === "perm" ? "#3A2600" : "#E6EDF6";
    g.textAlign = "center";
    g.textBaseline = "middle";
    let size = 92;
    g.font = `${size}px "Segoe UI Emoji", "Apple Color Emoji", "Noto Color Emoji", ` +
      `"Segoe UI", system-ui, sans-serif`;
    const wide = g.measureText(mark).width;
    if (wide > 104) {
      size = Math.max(28, Math.floor(size * 104 / wide));
      g.font = `${size}px "Segoe UI Emoji", "Apple Color Emoji", "Noto Color Emoji", ` +
        `"Segoe UI", system-ui, sans-serif`;
    }
    // Nudged down: text baselines sit optically high in a square this small.
    g.fillText(mark, 64, 70);
    notifIcons[key] = c.toDataURL("image/png");
    return notifIcons[key];
  }

  if (kind === "perm") {
    // An amber exclamation in a rounded square: something is blocked.
    g.fillStyle = "#FFC64D";
    g.beginPath();
    g.moveTo(64, 20);
    g.lineTo(108, 96);
    g.lineTo(20, 96);
    g.closePath();
    g.fill();
    g.strokeStyle = "#0B1B2E";
    g.lineWidth = 11;
    g.beginPath();
    g.moveTo(64, 50); g.lineTo(64, 74);
    g.stroke();
    g.beginPath();
    g.arc(64, 87, 5.5, 0, Math.PI * 2);
    g.fill();
  } else {
    // The A, from `drawAtriumA` in core.js, which is the same drawing the tab
    // wears. Shared rather than copied so the two can never disagree.
    drawAtriumA(g, 128);
  }
  notifIcons[key] = c.toDataURL("image/png");
  return notifIcons[key];
}

// A card's own mark, for an alert that is about exactly one card.
//
// Both shapes carry it, the same way both carry the tone: a task has its own
// icon and a permission is given the asking card's by the server, which is
// already joining that row for the agent name.
function iconForAlert(item) { return (item && item.icon) || ""; }

// An uploaded picture, rather than a glyph to draw.
//
// `img:` is the marker the card carries. Everything else in that field is a
// character to render, which is what it has always been.
function iconIsImage(mark) { return String(mark || "").startsWith("img:"); }

// The URL a notification should use for a card's uploaded icon.
//
// Served by the daemon rather than inlined as a data URL, so the bytes are not
// copied into every notification and the service worker can fetch it with no
// tab open.
function iconURLFor(taskID) { return `/v1/tasks/${taskID}/icon`; }

// The service worker is what makes buttons on a notification possible, and it
// keeps working with no tab open. Registration is best effort: without it,
// notifications still appear, just without buttons.
let swReg = null;
if ("serviceWorker" in navigator) {
  navigator.serviceWorker.register("/sw.js")
    .then(r => { swReg = r; return navigator.serviceWorker.ready; })
    .then(r => { swReg = r; })
    .catch(e => console.warn("no service worker, notifications lose their buttons:", e));
  // The worker asks the page to switch tabs when a notification is clicked.
  navigator.serviceWorker.addEventListener("message", e => {
    if (e.data && e.data.type === "goTo") {
      closeOpenDialogs().then(ok => {
        if (!ok) return;
        landOnAlert(e.data.taskFor || "", e.data.view || "", e.data.key || "");
        refresh();
      });
    }
  });
}

// `retireBy` is the pending item it goes with, empty for one nothing answers.
// `opts` is a growler's: its own tag, sticky whatever the reason, the subject to retire it by, and a callback
// handed the notification when this page made it directly.
function showNotification(title, body, goTo, permId, subject, mark, taskFor, retireBy, opts) {
  if (!("Notification" in window) || Notification.permission !== "granted") return null;
  opts = opts || {};
  const expiry = Number(alerting.get().expiry) || 0;
  // Sticky means Windows never takes it down by itself. That is right for a
  // blocked agent and wrong forever, so an expiry overrides it.
  const sticky = opts.sticky || (goTo === "perms" && expiry === 0);

  // One tag per subject. A shared tag replaces the notification already on
  // screen, so two agents finishing within a few seconds of each other showed
  // one name and the other went by unseen. The subject is the card or the
  // request, and a summary of several has none, which is correct: those are
  // meant to replace each other.
  const tag = opts.tag || (goTo === "perms" ? "atrium-perm" : "atrium") + (subject ? ":" + subject : "");

  // An uploaded picture is used as it is. A glyph is drawn.
  //
  // `mark` carries `img:<file>` for a card with a picture, and the picture is
  // served by the daemon rather than inlined, so the bytes are not copied into
  // every notification and the service worker can fetch it with no tab open.
  //
  // The kind of alert stops being carried by the field it is drawn on, since
  // atrium did not draw this one. The amber and dark backgrounds only apply to
  // glyphs, and that is the trade for using your own image: it says WHICH
  // agent, and the title says how urgent.
  // The CARD id, not the subject: for a permission the subject is the request,
  // and the picture belongs to the agent that asked.
  const art = iconIsImage(mark) && taskFor
    ? iconURLFor(taskFor)
    : iconDataURL(goTo === "perms" ? "perm" : "atrium", mark);

  // With a worker, the notification gets approve and block buttons and can
  // outlive the page. Without one, fall back to a plain notification.
  if (swReg && swReg.active) {
    swReg.active.postMessage({
      type: "notify", title, body: body || "",
      icon: art,
      tag,
      sticky, expireMs: opts.sticky ? 0 : expiry * 1000, goTo, permId: permId || "",
      // What this is about, so it can be taken down once that is answered.
      // permId only exists for a permission, and a card that has gone ready
      // and then been replied to had nothing to retire it by.
      subject: opts.retire || retireBy || permId || "",
      // Where a click on the body lands. See `landOnAlert`.
      taskFor: taskFor || "", key: retireBy || permId || "",
      // The card's readable path, empty when it has neither alias nor handle. The worker matches a window on it.
      path: taskFor ? cardUrlPath(cardList().find(t => sameCard(t.id, taskFor)) || (typeof soloTask !== "undefined" && soloTask && sameCard(soloTask.id, taskFor) ? soloTask : null)) : "",
      origin: location.origin
    });
    return null;
  }

  const n = new Notification(title, {
    body: body || "",
    icon: art,
    badge: art,
    tag,
    renotify: true,
    // A blocked agent is not something to miss, so that one stays on screen.
    requireInteraction: sticky,
    // We play our own tone, and Windows adding a second one is jarring.
    silent: true
  });
  n.onclick = async () => {
    window.focus();
    // Same reason as the toast: arriving at a tab that is hidden behind a
    // dialog you opened before the notification fired is not arriving.
    if (!await closeOpenDialogs()) return;
    n.close();
    landOnAlert(taskFor || "", goTo || "", retireBy || permId || "");
  };
  if (expiry > 0 && !opts.sticky) setTimeout(() => n.close(), expiry * 1000);
  if (opts.onShown) opts.onShown(n);
  return n;
}

