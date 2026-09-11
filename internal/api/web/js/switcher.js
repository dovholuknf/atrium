// ── the switcher ────────────────────────────────────────────────────────────
//
// The session list answers "what is there". This answers "go there now": one
// key, type a few letters, Enter. It works on the board and inside a
// popped-out window, which is the half that is not obvious and is most of the
// code below.
//
// THE KEY IS THE WHOLE DESIGN RISK, so it is a setting before it is anything
// else.
//
// A browser keeps a handful of accelerators for itself and every browser keeps
// a different handful. Asking to keep one is sometimes granted and sometimes
// refused SILENTLY, which is the worst possible shape: the feature works on
// the machine it was built on and does nothing anywhere else, with no error
// anywhere to say so.
//
// `ctrl-shift-k` is the default because it is free in Chrome, Edge and Brave,
// and because a TERMINAL does not use it either, which is the other half of
// the question and the half `ctrl-k` fails: ctrl-k is readline's kill-line,
// and taking it would quietly break a keystroke every session has. Firefox
// takes ctrl-shift-k for its Web Console and will not give it back, so Firefox
// rebinds, and the settings field says so rather than leaving somebody to
// discover it.
//
// And when the browser takes it anyway, atrium says so. See `swWatchForTheft`.
const SWITCH_KEY_DEFAULT = "ctrl+shift+KeyK";
const SWITCH_KEY_STORE = "atrium.switchkey";
const SWITCH_RECENT_STORE = "atrium.switch.recent";
// How many of the last few to offer before anything is typed. Enough that the
// two or three sessions being worked between are all there, short enough that
// the list is still read at a glance rather than searched.
const SWITCH_RECENT_KEEP = 8;

// Keys no browser hands over, whatever the page asks. Refused at the point of
// binding rather than accepted and then found not to work, which is the exact
// failure this whole setting exists to avoid.
const SWITCH_KEY_TAKEN = [
  "ctrl+KeyT", "ctrl+KeyN", "ctrl+KeyW", "ctrl+KeyQ",
  "ctrl+shift+KeyT", "ctrl+shift+KeyN", "ctrl+shift+KeyW",
  "meta+KeyT", "meta+KeyN", "meta+KeyW", "meta+KeyQ"
];

// In THIS browser, and that is the point. Which key is free is a fact about
// the browser, so a value synced from the desktop would arrive wrong on the
// laptop running something else. Same reasoning as the terminal list's width.
function switchKey() {
  try { return localStorage.getItem(SWITCH_KEY_STORE) || SWITCH_KEY_DEFAULT; }
  catch (e) { return SWITCH_KEY_DEFAULT; }
}

// A keystroke as a comparable string, built from `e.code` and NOT `e.key`.
//
// `code` is the physical key, so a binding stays where it was put on a layout
// that moves the letters, and it does not change under shift: `e.key` for
// ctrl-shift-k is `K`, which would have to be special-cased against the same
// combination without shift.
//
// Empty for a bare modifier, which is what makes the capture field wait rather
// than binding `ctrl` the instant it is held down.
function comboOf(e) {
  const code = e.code || "";
  if (!code || /^(Control|Shift|Alt|Meta|OS)/.test(code)) return "";
  const mods = [];
  if (e.ctrlKey) mods.push("ctrl");
  if (e.altKey) mods.push("alt");
  if (e.metaKey) mods.push("meta");
  if (e.shiftKey) mods.push("shift");
  mods.push(code);
  return mods.join("+");
}

// What that string is called out loud. `KeyK` is a fine thing to store and a
// terrible thing to show somebody.
const COMBO_NAMES = {
  Slash: "/", Backquote: "`", Semicolon: ";", Comma: ",", Period: ".",
  BracketLeft: "[", BracketRight: "]", Quote: "'", Backslash: "\\",
  Space: "space", Enter: "enter", Escape: "esc"
};
function comboName(c) {
  return String(c || "").split("+").map(p =>
    COMBO_NAMES[p] || p.replace(/^Key/, "").replace(/^Digit/, "")
      .replace(/^Numpad/, "num ").replace(/^Arrow/, "").toLowerCase()
  ).join("-");
}

// THE HOTKEY, IN THE CAPTURE PHASE, AND IT STOPS THERE.
//
// Both calls are load-bearing and they answer different things.
// `preventDefault` is aimed at the browser. `stopPropagation` is aimed at
// xterm, which listens on its own textarea, so a listener that let the event
// through would open the switcher AND send a control character to whatever the
// runner was doing. Capture on `window` runs before either of them.
addEventListener("keydown", e => {
  // The settings field is listening for a keystroke to BIND. Opening the
  // switcher on the way past would be the feature fighting its own
  // configuration.
  if (swCapturing) return;
  if (comboOf(e) !== switchKey()) return;
  e.preventDefault();
  e.stopPropagation();
  toggleSwitcher();
}, true);

// What the list is drawn from, and what is drawn right now. Held rather than
// re-derived so the arrow keys and Enter agree with what is on the screen even
// if a poll lands between the two.
let swTasks = [], swRows = [], swQuery = "", swSel = 0;
let swCapturing = false, swSaidTaken = "";
// Whether the list has ever been answered. Only the empty state reads it, and
// only so the first open in a window says "looking" rather than announcing
// there is nothing to go to a few milliseconds before there is.
let swLoaded = false;

function switcherDlg() { return document.getElementById("switcher"); }

function toggleSwitcher() {
  const dlg = switcherDlg();
  if (!dlg) return;
  if (dlg.open) { dlg.close(); return; }
  openSwitcher();
}

async function openSwitcher() {
  const dlg = switcherDlg();
  if (!dlg) return;
  swQuery = "";
  swSel = 0;
  const q = document.getElementById("sw-q");
  q.value = "";
  document.getElementById("sw-key").textContent = comboName(switchKey());
  // Painted from what was last fetched, THEN again when the fresh answer
  // lands. The whole promise here is key, letters, Enter, and a list that
  // appears after a round trip is a list you have already typed into.
  paintSwitcher();
  dlg.showModal();
  q.focus();
  swWatchForTheft();
  let all = null;
  try { all = (await api("/v1/tasks")).tasks || []; } catch (e) {}
  // Terminals only. This list is a place to GO, and a card with nothing
  // running is not one: the board and the stack restart a stopped session,
  // with the whole card in front of you, which is where that belongs.
  if (all) { swTasks = all.filter(t => t.supervised); swLoaded = true; }
  if (dlg.open) paintSwitcher();
}

function closeSwitcher() {
  const dlg = switcherDlg();
  if (dlg && dlg.open) dlg.close();
}

// A KEY THE BROWSER TOOK ANYWAY, DETECTED RATHER THAN GUESSED.
//
// `preventDefault` returning normally proves nothing: a browser that ignores
// it does so without a word. What it cannot hide is where the focus went. Every
// accelerator that costs us this keystroke moves focus out of the document, to
// an address bar, a search field or a devtools panel, so a document that has
// stopped being focused a moment after opening a modal it just focused an
// input inside means the keystroke was delivered twice.
//
// Said once per binding per session, because the answer does not change until
// the binding does, and the fix is one field away.
function swWatchForTheft() {
  const combo = switchKey();
  setTimeout(() => {
    if (document.hasFocus() || swSaidTaken === combo) return;
    swSaidTaken = combo;
    toast("your browser also took " + comboName(combo),
      "the switcher is open and your typing is going somewhere else. " +
      "pick another key in settings, under the board.");
  }, 250);
}

// Ranked, not just filtered.
//
// Every term has to match something or the card is out, which is what makes
// typing more letters narrow rather than widen. Where it matched decides the
// order: the name is what people mean, the tag is what they filed it under,
// and the directory is the long one that matches too easily. A subsequence is
// the last resort, so `atsw` still finds `atrium:switcher` without letting a
// scattering of letters outrank a real word.
function swScore(t, q) {
  const terms = q.toLowerCase().split(/\s+/).filter(Boolean);
  if (!terms.length) return 0;
  const name = String(terminalLabel(t) || t.display_title || "").toLowerCase();
  const dir = String(t.worktree || "").toLowerCase().replace(/\\/g, "/");
  const tags = (t.tags || []).join(" ").toLowerCase();
  let total = 0;
  for (const term of terms) {
    let s = -1;
    const at = name.indexOf(term);
    if (at === 0) s = 100;
    else if (at > 0) s = 70;
    else if (tags.includes(term)) s = 50;
    else if (dir.includes(term)) s = 40;
    else if (swSubseq(name + " " + dir + " " + tags, term)) s = 10;
    if (s < 0) return -1;
    total += s;
  }
  return total;
}

function swSubseq(hay, needle) {
  let i = 0;
  for (const ch of hay) {
    if (ch === needle[i]) i++;
    if (i >= needle.length) return true;
  }
  return i >= needle.length;
}

// Which card this window is already showing, so it can say so rather than
// offering you a trip to where you are.
function swHere() {
  return termOnly() ? soloID : (termTask ? termTask.id : "");
}

function swRecent() {
  try {
    const v = JSON.parse(localStorage.getItem(SWITCH_RECENT_STORE) || "[]");
    return Array.isArray(v) ? v : [];
  } catch (e) { return []; }
}

function swRemember(id) {
  const list = swRecent().filter(x => x !== id);
  list.unshift(id);
  try {
    localStorage.setItem(SWITCH_RECENT_STORE, JSON.stringify(list.slice(0, SWITCH_RECENT_KEEP)));
  } catch (e) {}
}

// THE ORDER WITH NOTHING TYPED IS THE ONE THAT MATTERS, because the common
// case is the key and Enter with nothing typed at all. Most recently switched
// to first, so going back and forth between two sessions is one keystroke and
// one more.
//
// Everything else falls in behind, anything waiting on a human above the rest,
// then by how recently it moved: the same order the session list uses, so the
// two do not disagree about which session is the interesting one.
function switcherRows() {
  const recent = swRecent();
  const here = swHere();
  const scored = [];
  swTasks.forEach(t => {
    const s = swScore(t, swQuery);
    if (s < 0) return;
    const r = recent.indexOf(t.id);
    scored.push({ t, s, r: r < 0 ? recent.length + 1 : r });
  });
  scored.sort((a, b) => {
    if (swQuery && b.s !== a.s) return b.s - a.s;
    if (a.r !== b.r) return a.r - b.r;
    const w = (isWaiting(b.t) ? 1 : 0) - (isWaiting(a.t) ? 1 : 0);
    if (w) return w;
    return (a.t.idle_seconds || 0) - (b.t.idle_seconds || 0);
  });
  // Where you already are, last. It is still listed, because a switcher that
  // hides a session makes you wonder whether it is gone.
  scored.sort((a, b) => (a.t.id === here ? 1 : 0) - (b.t.id === here ? 1 : 0));
  return scored.map(x => x.t);
}

function paintSwitcher() {
  const host = document.getElementById("sw-list");
  if (!host) return;
  swRows = switcherRows();
  if (swSel >= swRows.length) swSel = Math.max(0, swRows.length - 1);
  const here = swHere();
  setHTML(host, swRows.length
    ? swRows.map((t, i) => `
      <div class="swrow ${i === swSel ? "on" : ""}" data-id="${esc(t.id)}">
        ${runnerMark(t.runner)}
        <span class="swname">${esc(terminalLabel(t) || t.display_title)}</span>
        ${t.id === here ? `<span class="chip">here</span>` : ""}
        ${poppedOut(t.id) && t.id !== here
          ? `<span class="chip accent" title="this one is in a window of its own">&#8599;</span>` : ""}
        ${isWaiting(t) ? `<span class="chip warn">wants you</span>` : ""}
        <span class="swdir" title="${esc(t.worktree || "")}">${esc(t.worktree || "")}</span>
      </div>`).join("")
    : `<div class="empty">${swQuery ? "nothing matches that"
        : (swLoaded ? "no sessions to go to" : "looking&hellip;")}</div>`);
  // The id travels in a data attribute and comes back through the DOM, never
  // through an inline handler. The house rule, and it costs nothing here.
  host.querySelectorAll(".swrow").forEach(el => {
    el.onclick = () => switchTo(el.dataset.id);
  });
  const on = host.querySelector(".swrow.on");
  if (on) on.scrollIntoView({ block: "nearest" });
}

function swMove(d) {
  if (!swRows.length) return;
  swSel = (swSel + d + swRows.length) % swRows.length;
  paintSwitcher();
}

// Going there, from either kind of window.
//
// The board attaches in its pane, and `attachTask` already knows the one case
// that is not that: a card in a window of its own is raised instead, because
// two views on one terminal is the thing nothing arbitrates. A popped-out
// window has no pane to attach in, so it MOVES. See `soloSwitch`.
async function switchTo(id) {
  if (!id) return;
  swRemember(id);
  closeSwitcher();
  if (termOnly()) await soloSwitch(id);
  else await attachTask(id);
  // Back to the terminal, or the next thing typed goes into a box that is no
  // longer there. Same reason as `closeFind`.
  if (term) term.focus();
}

// A POPPED-OUT WINDOW CHANGING WHICH CARD IT IS.
//
// Four things move together and leaving any one of them behind is a bug that
// looks like something else entirely:
//
// - **The claim.** The old one is RELEASED and the new one made. Skip the
//   release and the board goes on believing a card is popped out in a window
//   that has moved on, refusing to attach to it and "raising" a window showing
//   something else. Claims are fifteen second heartbeats, so it heals itself,
//   which is worse rather than better: the symptom is a flicker somebody
//   diagnoses as anything but this.
// - **The window's NAME.** It is `atrium-term-<id>`, and the board finds a
//   window it did not open by that name. Leave it and `reopenByName` finds
//   nothing, so the board opens a SECOND window on a card this one is holding.
// - **The address.** `#term=<id>` is how this window resolves its card on
//   load, and it reloads itself whenever the daemon serves a new build.
//   `replaceState` rather than assigning to the hash, so the back button does
//   not walk a window through cards it used to be showing.
// - **What is being alerted about.** The mark in the title bar and the two
//   booleans behind it belong to the old card. `null` again rather than
//   `false`, so arriving at a session that has been waiting for an hour is
//   silent, exactly as opening a window on it is.
async function soloSwitch(id) {
  if (!id || id === soloID) return;
  // The same refusal the board makes, for the same reason, and it can be made
  // here now because a solo window keeps the other windows' claims.
  if (poppedOut(id)) {
    toast("that one has a window already",
      "atrium will not put two views on one terminal. go to that window instead.");
    return;
  }
  let task;
  try { task = await api("/v1/tasks/" + encodeURIComponent(id)); }
  catch (e) { toast("could not go there", e.message); return; }

  const was = soloID;
  soloID = id;
  soloTask = task;
  // A window that gave its card away is a viewer again the moment it is given
  // another one. Leaving this set would keep the heartbeat and the watchdog off
  // for the new card, so the board would never learn this window holds it.
  soloYielded = false;
  soloMark = "";
  soloKnown = { perm: null, ready: null };
  history.replaceState(null, "", location.pathname + "#term=" + encodeURIComponent(id));
  window.name = "atrium-term-" + id;
  if (soloBus) {
    soloBus.postMessage({ type: "solo-release", task: was });
    soloBus.postMessage({ type: "solo-claim", task: id });
  }
  paintSoloTitle();
  openTerm(task);
}

// The field, and the three keys that drive it. Nothing here needs a mouse.
(function wireSwitcher() {
  const q = document.getElementById("sw-q");
  if (!q) return;
  q.addEventListener("input", e => {
    swQuery = e.target.value.trim();
    // Back to the top on every edit. Keeping the selection where it was means
    // the highlighted row jumps to whatever is now in that position, which is
    // a different session and one keystroke from being opened.
    swSel = 0;
    paintSwitcher();
  });
  q.addEventListener("keydown", e => {
    if (e.key === "ArrowDown") { e.preventDefault(); swMove(1); }
    else if (e.key === "ArrowUp") { e.preventDefault(); swMove(-1); }
    else if (e.key === "Enter") {
      e.preventDefault();
      const t = swRows[swSel];
      if (t) switchTo(t.id);
    }
    // Escape is the dialog's own and needs nothing here.
  });
})();

// ── binding it to something else ────────────────────────────────────────────

function paintSwitchKey() {
  const b = document.getElementById("s-switchkey");
  if (!b) return;
  b.textContent = comboName(switchKey());
  b.title = "click, then press the keys you want";
}

// Bound by PRESSING it, not by picking it off a list. The question is whether
// this browser lets that combination through, and the only thing that answers
// it is the combination, in this browser, now.
function captureSwitchKey(btn) {
  if (swCapturing) return;
  swCapturing = true;
  btn.textContent = "press the keys…";
  const stop = () => {
    swCapturing = false;
    removeEventListener("keydown", onKey, true);
    paintSwitchKey();
  };
  const onKey = e => {
    if (e.key === "Escape") { e.preventDefault(); e.stopPropagation(); stop(); return; }
    const combo = comboOf(e);
    // A modifier on its own. Still waiting, rather than binding `ctrl` the
    // moment it goes down.
    if (!combo) return;
    e.preventDefault();
    e.stopPropagation();
    if (!(e.ctrlKey || e.altKey || e.metaKey)) {
      toast("that needs a modifier",
        "a bare key would open the switcher while you were typing into a session.");
      stop();
      return;
    }
    if (SWITCH_KEY_TAKEN.includes(combo)) {
      toast("no browser gives that one up",
        comboName(combo) + " opens a tab or closes a window whatever this page asks. " +
        "the switcher would never see it.");
      stop();
      return;
    }
    try { localStorage.setItem(SWITCH_KEY_STORE, combo); } catch (err) {}
    swSaidTaken = "";
    stop();
    toast("the switcher opens on " + comboName(combo), "in this browser");
  };
  addEventListener("keydown", onKey, true);
}

function resetSwitchKey() {
  try { localStorage.removeItem(SWITCH_KEY_STORE); } catch (e) {}
  swSaidTaken = "";
  paintSwitchKey();
  toast("back to " + comboName(SWITCH_KEY_DEFAULT), "chrome, edge and brave leave it alone");
}

