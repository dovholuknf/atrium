// ── settings dialog ─────────────────────────────────────
const settingsDlg = document.getElementById("settings");
document.getElementById("gear").onclick = () => { paintSettings(); settingsDlg.showModal(); };

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
function paintGroupSegs() {
  const p = groupingPrefs();
  const mode = p.on ? (p.mode || "project") : "off";
  const opts = [
    ["project", "by project", "cut the cards into projects, read from the worktree path"],
    ["window", "by pile",
      "cut the cards the way whatever launched them said to: pull requests, tangents, " +
      "support threads. a card that was not told falls back to its project"],
    ["tag", "by tag", "cut the cards into the tags you applied. a card with several appears under each"],
    ["recency", "by when",
      "today, yesterday, this week, this month, and everything older, which is dormant"],
    ["off", "off", "one flat list"]
  ];
  const html = opts.map(([v, label, title]) =>
    `<button class="${v === mode ? "on" : ""}" onclick="setGroupMode('${v}')"
       title="${esc(title)}">${esc(label)}</button>`).join("");
  ["stack-group", "board-group"].forEach(id => {
    const el = document.getElementById(id);
    if (el) setHTML(el, html);
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

function paintGlobalAuto() {
  const b = document.getElementById("gauto");
  if (!b) return;
  b.className = globalAuto ? "gauto on" : "gauto";
  // How long is left goes ON the switch, not behind it. The whole reason a
  // deadline exists is that "approving everything" is easy to leave on, and a
  // reminder you have to hover over is not a reminder.
  b.textContent = globalAuto
    ? (globalAutoLeft ? "approving everything, " + leftLabel(globalAutoLeft) : "approving everything")
    : "asking";
  b.title = globalAuto
    ? (globalAutoLeft
        ? "every session is approved without asking, until this runs out. click to start asking again."
        : "every session is approved without asking, with no deadline. click to start asking again.")
    : "requests are gated. click to approve everything from every session.";
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
  } catch (e) { return; }
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

