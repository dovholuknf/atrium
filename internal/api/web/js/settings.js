// ── settings dialog ─────────────────────────────────────
const settingsDlg = document.getElementById("settings");
document.getElementById("gear").onclick = () => {
  // A fresh visit asks again which machine it is editing, if it has to ask.
  settingsRoom = "";
  paintSettings();
  // Every field that saves itself, wired once. Here rather than at load
  // because the panes are cut up at runtime and this is the moment they are
  // all certain to exist. See `wireSelfSaving`.
  wireSelfSaving();
  settingsDlg.showModal();
};

function soundOptions(selected) {
  return Object.entries(SOUNDS)
    .map(([k, s]) => `<option value="${k}"${k === selected ? " selected" : ""}>${s.label}</option>`)
    .join("");
}

// One scale for the whole page. Every size in the stylesheet is a --fs-* value
// multiplied by this, so nothing is left behind at a fixed pixel size.
const UI_SCALE_KEY = "atrium.uiscale";

function uiScale() {
  const v = Number(localStorage.getItem(UI_SCALE_KEY));
  return v > 0 ? v : 1.1;
}

function applyUiScale() {
  document.documentElement.style.setProperty("--uiscale", String(uiScale()));
}
applyUiScale();

// How much room around things, separately from how big they are.
//
// Held in the browser rather than on the daemon, like the text scale and for
// the same reason: it is about the screen being looked at, and two people on
// two screens want different answers to it.
const DENSITY_KEY = "atrium.density";

function density() {
  const v = Number(localStorage.getItem(DENSITY_KEY));
  return v > 0 ? v : 1;
}

function applyDensity() {
  document.documentElement.style.setProperty("--density", String(density()));
}
applyDensity();

document.getElementById("s-sweep").addEventListener("change", e => {
  saveHousekeeping("sweep_dead_after", e.target.value);
});
// The one setting in the dialog that destroys something, so turning it on is
// confirmed and turning it off never is. Not skippable: the tick box that
// stops asking belongs on repeatable questions, and this one is answered about
// once.
document.getElementById("s-prune").addEventListener("change", async e => {
  const value = e.target.value;
  if (value !== "off") {
    const label = e.target.options[e.target.selectedIndex].textContent;
    if (!await confirmUser("delete finished cards " + label + "?",
      "Every done or dead card older than that is deleted, along with its whole audit log: what " +
      "the session ran, what it was allowed to do, and what answered each request. There is no " +
      "other copy.<br><br>Shelved cards and the inbox are never touched.",
      "delete them")) {
      loadHousekeeping();
      return;
    }
  }
  saveHousekeeping("prune_after", value);
});
document.getElementById("s-cardsize").addEventListener("change", e => {
  localStorage.setItem(UI_SCALE_KEY, e.target.value);
  applyUiScale();
});
document.getElementById("s-density").addEventListener("change", e => {
  localStorage.setItem(DENSITY_KEY, e.target.value);
  applyDensity();
});

// The grouping controls. Blank code means the defaults, so the boxes are
// seeded with the defaults as a starting point rather than left empty.
function paintGrouping() {
  const g = groupingPrefs();
  // The button says what grouping IS, and pressing it flips that. A button
  // reading "on" is ambiguous about whether that is the state or the action.
  const btn = document.getElementById("s-group-toggle");
  btn.textContent = g.on ? "enabled" : "disabled";
  btn.title = g.on ? "click to disable grouping" : "click to enable grouping";
  btn.className = g.on ? "go" : "";
  document.getElementById("s-group-code").hidden = !g.on;
  document.getElementById("s-order-code").hidden = !g.on;
  document.getElementById("s-group-by").value = g.by || DEFAULT_GROUP_BY;
  document.getElementById("s-group-order").value = g.order || DEFAULT_GROUP_ORDER;
}

function toggleGrouping() {
  const g = groupingPrefs();
  setGrouping({ on: !g.on });
  paintGrouping();
  paintGroupSegs();
}

// The same on/off, on the board and on the stack, where you are actually
// looking when you decide you want it.
//
// One setting behind both, so turning grouping off on the stack turns it off
// on the board too. Grouping is a way of reading the same cards, not a
// property of one screen.
// ── settings that save themselves ───────────────────────────────────────────
//
// EIGHT SAVE BUTTONS ON ONE PANE was the state of `this machine`, one beside
// every box, each a different width because the box in front of it was. The
// pane read as a form to be filled in and submitted, which it is not: these
// are independent settings and each one is done the moment you have typed it.
//
// A `change` event, which fires on blur and only when the value actually
// changed. Tabbing through the pane saves nothing. Typing and leaving saves
// once. The existing save functions are unchanged and still say what happened,
// so a failure is still a toast rather than a silent loss.
//
// `data-saves` names the function on the element. Wiring it from the markup
// keeps the list of what-saves-what next to the fields rather than in a table
// here that has to be kept in step with them.
// settingsRoom is which machine a pane is editing, asked once per opening.
//
// MOSTLY UNUSED NOW, and kept for the one case left. The per-machine settings
// moved behind each room's own cog, where the room is not a question: you
// opened that room's pane, so it is that room, and `openRoomCog` sets the write
// scope before a field is ever touched. This remains for a self-saving field
// that is still in the settings dialog and belongs to a machine.
let settingsRoom = "";

function wireSelfSaving() {
  // BOTH DIALOGS. The machine settings live behind a room's cog and the board's
  // own live in settings, and both hold fields that save themselves. Wiring
  // only the first is how those nine boxes silently stopped saving when they
  // moved: nothing threw, nothing logged, and every edit was simply dropped.
  document.querySelectorAll("#settings [data-saves], #roomcfg [data-saves]").forEach(el => {
    if (el.dataset.wired) return;
    el.dataset.wired = "1";
    el.addEventListener("change", async () => {
      const fn = window[el.dataset.saves];
      if (typeof fn !== "function") return;
      // WHICH MACHINE, when the board is looking at all of them. A hub with two
      // rooms refuses a write that names none.
      //
      // Skipped entirely for a field in a room's own pane: the room is already
      // decided and already set, and asking there would be asking somebody to
      // name the room whose name is in the title of the dialog they are in.
      const inRoomPane = !!el.closest("#roomcfg");
      if (!inRoomPane && typeof hubIsHub !== "undefined" && hubIsHub &&
          typeof roomNow === "function" && !roomNow()) {
        if (!settingsRoom) {
          if (!await chooseWriteRoom(null, "setting")) return;
          settingsRoom = writeRoom;
        } else {
          writeRoom = settingsRoom;
        }
      }
      try {
        await fn();
        flashSaved(el);
      } catch (e) {}
    });
  });
}

// A GREEN EDGE FOR A MOMENT. The save functions toast, which says what
// happened but not WHERE, and on a pane of nine boxes "saved" alone leaves you
// checking you edited the one you meant.
function flashSaved(el) {
  el.classList.add("justsaved");
  setTimeout(() => el.classList.remove("justsaved"), 1400);
}

function paintGroupSegs() {
  const p = groupingPrefs();
  const mode = p.on ? (p.mode || "project") : "off";
  const opts = [
    ["project", "by project", "cut the cards into projects, read from the worktree path"],
    ["window", "by pile",
      "cut the cards the way whatever launched them said to: pull requests, tangents, " +
      "support threads. a card that was not told falls back to its project"],
    ["tag", "by tag", "cut the cards into the tags you applied. a card with several appears under each"],
    ["custom", "by group",
      "your own named buckets. the + button adds one, and a card's menu files " +
      "it into one. every group is a tag under the hood, so the choices stick " +
      "across browsers"],
    // `by age`, not `by when`. There is a SORT control beside this one, and
    // `by when` reads as an answer to that: it sounds like an ordering, so
    // pressing it and getting five headings looks like sorting that did not
    // work. It cuts the cards into age buckets, which is what age means.
    ["recency", "by age",
      "today, yesterday, this week, this month, and everything older, which is dormant"],
    ["off", "off", "one flat list, in the order the sort above put them"]
  ];
  const html = opts.map(([v, label, title]) =>
    `<button class="${v === mode ? "on" : ""}" onclick="setGroupMode('${v}')"
       title="${esc(title)}">${esc(label)}</button>`).join("");
  // A `+ new group` button rides beside the picker when `custom` is on.
  // Drawn in the same host as the picker so it sits alongside the mode
  // buttons and disappears the moment another mode is chosen: an add
  // button for a mode that is off does nothing anybody expects.
  const plus = mode === "custom"
    ? `<button class="groupplus" onclick="addCustomGroup()"
         title="add a group. it is a tag under the hood: files a card into this bucket by tagging it">+ new group</button>`
    : "";
  // The strip is the third. It is rebuilt wholesale on every render, so it
  // calls this afterwards rather than relying on having been painted once.
  ["stack-group", "board-group", "term-group"].forEach(id => {
    const el = document.getElementById(id);
    if (el) setHTML(el, html + plus);
  });
}

// Global auto mode: every session, including ones that do not exist yet.
//
// Held by the daemon rather than the browser, so it is one answer for the
// machine and it survives a restart. A restart is not consent to start asking
// again, and it is not consent to keep approving either: it is whatever it
// was when you left.
let globalAuto = false;
// Seconds left on a deadline, sent by the daemon rather than worked out from a
// timestamp here. A clock a few minutes out between the browser and the
// machine is ordinary, and it would show as a switch that expired in the
// future.
let globalAutoLeft = 0;
// Whether a settings read has ever landed in this page, and whether the last
// one failed. A hub that was just restarted answers `/v1/settings` with a 409
// until a room attaches, and a room scope whose room is not back yet fails the
// same way. The button used to be painted only by a read that landed, so a
// failed one left an empty pill in the header. It now says it does not know,
// or that what it shows is the last answer, and the poll re-reads until one
// lands. See `refresh` and `loadHubRooms`.
let globalAutoRead = false;
let globalAutoStale = false;
function globalAutoNeedsRead() { return globalAutoStale; }

function paintGlobalAuto() {
  const b = document.getElementById("gauto");
  if (!b) return;
  if (globalAutoStale && !globalAutoRead) {
    b.className = "gauto unknown";
    b.textContent = "auto: unknown";
    b.title = "the daemon has not answered whether requests are gated. retrying.";
    return;
  }
  b.className = (globalAuto ? "gauto on" : "gauto") + (globalAutoStale ? " stale" : "");
  // How long is left goes ON the switch, not behind it. The whole reason a
  // deadline exists is that "approving everything" is easy to leave on, and a
  // reminder you have to hover over is not a reminder.
  b.textContent = globalAuto
    ? (globalAutoLeft ? "approving everything, " + leftLabel(globalAutoLeft) : "approving everything")
    : "asking";
  b.title = (globalAutoStale ? "the last answer, the daemon is not answering right now. retrying. " : "") +
    (globalAuto
      ? (globalAutoLeft
          ? "every session is approved without asking, until this runs out. click to start asking again."
          : "every session is approved without asking, with no deadline. click to start asking again.")
      : "requests are gated. click to approve everything from every session.");
}

// Time left, rounded the way a person reads it. Rounded UP for minutes, so
// something with fifty seconds on it does not read as being over.
function leftLabel(secs) {
  secs = Number(secs) || 0;
  if (secs <= 0) return "";
  if (secs < 3600) return Math.ceil(secs / 60) + "m left";
  const h = Math.floor(secs / 3600);
  const m = Math.round((secs % 3600) / 60);
  return m ? h + "h " + m + "m left" : h + "h left";
}

async function loadGlobalAuto() {
  try {
    const s = await api("/v1/settings");
    globalAuto = !!s.global_auto;
    globalAutoLeft = s.global_auto_seconds || 0;
    // Re-wear the scope's skin off this same read. Called on every stream
    // reopen (see connect), which is when a hub that was down or still
    // attaching rooms first answers settings, so a skin the load-time read
    // could not fetch heals here rather than staying dark until a reload. Uses
    // this fetch rather than a second one. See `applyResolvedSkin`.
    globalAutoRead = true;
    globalAutoStale = false;
    if (typeof applyResolvedSkin === "function") applyResolvedSkin(s);
  } catch (e) {
    globalAutoStale = true;
  }
  paintGlobalAuto();
}

// Turning it ON asks; turning it off never does. The confirmation is not
// skippable, since this is the widest thing on the board.
//
// It asks HOW LONG rather than whether, because the honest answer is nearly
// always "while I do this one thing" and the only reminder it used to have was
// a badge. "Until I turn it off" is still there and still means it: a switch
// that could only be temporary would just be a shorter lie.
//
// Enter takes the last button, so an hour is what a reflex gets.
async function toggleGlobalAuto() {
  const on = !globalAuto;
  let minutes = 0;
  if (on) {
    const choice = await askUser({
      title: "approve everything, from every session?",
      body: "Every request from every session on this machine is approved without asking, " +
        "including sessions that have not started yet." +
        "<br><br>Everything is still recorded, and <b>never</b> rules and shelved cards still " +
        "block. Read what any session did with <b>what did it do?</b>" +
        "<br><br>How long for?",
      buttons: [
        { label: "cancel", value: null },
        { label: "until I turn it off", value: 0 },
        { label: "for four hours", value: 240 },
        { label: "for an hour", value: 60, style: "go" }
      ]
    });
    if (choice === null || choice === undefined) return;
    minutes = Number(choice) || 0;
  }
  let s;
  try {
    s = await api("/v1/settings", {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ global_auto: on, global_auto_minutes: minutes })
    });
    globalAuto = !!s.global_auto;
    globalAutoLeft = s.global_auto_seconds || 0;
    globalAutoRead = true;
    globalAutoStale = false;
  } catch (e) {
    toast("that did not stick", e.message);
    return;
  }
  paintGlobalAuto();
  // Immediately, not on the next poll. The drain has already answered
  // everything that was queued, and refresh is what retires the toasts and OS
  // notifications those requests raised. Five seconds of growlers shouting
  // about requests that have just been approved is the switch looking broken
  // at exactly the moment it worked.
  refresh();
  // The drained count is said out loud. Those requests were sitting in front
  // of you a second ago and they have just been answered on your behalf, which
  // you should hear about from the thing that did it.
  const drained = on && s.drained
    ? ` ${s.drained} request${s.drained === 1 ? "" : "s"} that ${
        s.drained === 1 ? "was" : "were"} already waiting ${
        s.drained === 1 ? "was" : "were"} approved.`
    : "";
  toast(on ? "approving everything" : "asking again",
    on ? "nothing on this machine will stop to ask you." + drained
       : "requests are gated again");
}

