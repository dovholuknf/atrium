// ── the settings spine ──────────────────────────────────
//
// The dialog is written as ONE flow of fields with `h3.s-section` headings,
// and that is how it stays: it reads top to bottom in the file, which is what
// makes it editable. The nav is built from those headings when the dialog
// opens rather than by wrapping every group in markup by hand, so adding a
// section is still one heading and its fields, in order, with nothing else to
// keep in step.
//
// The line this nav has to hold, because the nav is what will be used to place
// the next setting: **everything in here is a setting for the MACHINE.** A
// card's theme, its bell and its notes are settings for one piece of work, and
// they live on the card. Nothing that is per-card belongs in this dialog, and
// nothing board-wide belongs on a card.

// Which pane was last looked at, so opening the gear again lands where you
// were rather than at the top. Per browser, since it is about this screen.
const SETTINGS_PANE = "atrium.settingsPane";

// SPLITTING A LONG SCROLL INTO PANES. Two callers: the settings dialog and the
// runners page.
//
// The property both depend on: the markup stays ONE FLOW of headings and their
// content, top to bottom, which is what makes it editable. The nav is built
// from the headings, so adding a section is a heading and its fields with
// nothing else to keep in step.
//
// `data-pane` on a heading is the nav label when the heading itself is not one.
// The runners page needs it because its headings carry a help bubble, and
// `textContent` would put the whole tooltip in the button.
function splitIntoPanes(host, headingClass, key, opts) {
  opts = opts || {};
  if (!host || host.dataset.split) return;

  const headings = Array.from(host.querySelectorAll(":scope > ." + headingClass));
  if (headings.length < 2) return;

  // Everything from one heading up to the next is that pane.
  const panes = headings.map(h => {
    const pane = document.createElement("div");
    pane.className = "pane";
    pane.dataset.name = (h.dataset.pane || h.textContent || "").trim();
    let node = h;
    const take = [];
    while (node) {
      take.push(node);
      node = node.nextElementSibling;
      if (node && node.classList.contains(headingClass)) break;
    }
    take.forEach(n => pane.appendChild(n));
    return pane;
  });

  const nav = document.createElement("div");
  nav.className = "pane-nav";
  const box = document.createElement("div");
  box.className = "panes";
  panes.forEach(p => box.appendChild(p));

  panes.forEach(p => {
    const b = document.createElement("button");
    b.textContent = p.dataset.name;
    b.onclick = () => showPane(host, p.dataset.name, key, opts);
    nav.appendChild(b);
  });

  const split = document.createElement("div");
  split.className = "pane-split";
  split.appendChild(nav);
  split.appendChild(box);
  host.appendChild(split);
  host.dataset.split = "1";

  let want = "";
  try { want = localStorage.getItem(key) || ""; } catch (e) {}
  showPane(host, want || panes[0].dataset.name, key, opts);
}

function showPane(host, name, key, opts) {
  if (!host) return;
  opts = opts || {};
  const panes = Array.from(host.querySelectorAll(".pane"));
  // A remembered name that no longer exists falls back to the first, so
  // renaming a section does not open an empty screen.
  if (!panes.some(p => p.dataset.name === name)) {
    name = panes.length ? panes[0].dataset.name : "";
  }
  panes.forEach(p => { p.hidden = p.dataset.name !== name; });
  host.querySelectorAll(".pane-nav button").forEach(b => {
    b.classList.toggle("on", b.textContent === name);
  });
  // Scrolled back to the top, since the panes share one scroll box and
  // arriving halfway down a short pane reads as a rendering fault.
  (opts.scroller || host).scrollTop = 0;
  try { localStorage.setItem(key, name); } catch (e) {}
  if (opts.onShow) opts.onShow(name);
}

function settingsBody() { return document.querySelector("#settings .dlg-body"); }
function buildSettingsNav() { splitIntoPanes(settingsBody(), "s-section", SETTINGS_PANE); }
function showSettingsPane(name) { showPane(settingsBody(), name, SETTINGS_PANE); }

function paintSettings() {
  buildSettingsNav();
  // Not awaited. The gear opens on what the browser already knows and the
  // login section fills itself in a moment later, the same way the overlays do.
  loadAuth();
  const p = alerting.get();
  document.getElementById("s-vol").value = Math.round(p.volume * 100);
  document.getElementById("s-input").innerHTML = soundOptions(p.input);
  document.getElementById("s-perm").innerHTML = soundOptions(p.perm);
  document.getElementById("s-expiry").value = String(Number(p.expiry) || 0);
  document.getElementById("s-debounce").value = String(Number(p.debounce) || 0);
  document.getElementById("s-stuck").value = p.stuck || "alert";
  document.getElementById("s-quietdoers").checked = p.quietDoers !== false;
  document.getElementById("s-cardsize").value = String(uiScale());
  document.getElementById("s-density").value = String(density());
  document.getElementById("s-hoverfocus").checked = hoverFocus;
  document.getElementById("s-cardcolors").checked = cardColors;
  for (const k of ["selected", "idle", "exited"])
    document.getElementById("s-termwear-" + k).checked = termWearOn[k];
  document.getElementById("s-inputlag").checked = lagOn;
  document.getElementById("s-typing").checked = typingOn;
  // The two housekeeping timers. Held by the daemon rather than the browser,
  // because they are about the machine's data and not about this screen, so
  // they are read here rather than assumed.
  loadHousekeeping();
  paintGrouping();
  paintSwitchKey();
  paintSkipped();
  // Fetched when the gear opens rather than on every poll: a share's state
  // only changes when something in here changes it, and the event stream
  // carries the ones that happen elsewhere.
  loadOverlays();
  // The redo surface ("expose the board 2"), painted from the same overlay
  // state. Guarded so removing js/expose2.js leaves this a no-op.
  if (typeof loadExpose2 === "function") loadExpose2();
  const state = document.getElementById("s-notify-state");
  const hint = document.getElementById("s-notify-hint");
  const ask = document.getElementById("s-notify-ask");
  const perm = ("Notification" in window) ? Notification.permission : "unsupported";
  // Once granted, an enable button can do nothing, and a page cannot revoke
  // its own permission. What atrium can control is whether it sends any, so
  // that is what the button becomes.
  const toggle = document.getElementById("s-notify-toggle");
  const on = alerting.get().desktop !== false;

  // THE CHIP REPORTS WHAT WILL HAPPEN, NOT WHAT THE BROWSER ALLOWS.
  //
  // Those are two facts and the chip only has room for one. Reading the
  // browser's permission out loud meant it said `granted`, in the positive
  // colour, beside a button offering to `turn on` and a note explaining that
  // nothing was being sent. The status said fine and the two controls beside
  // it said otherwise.
  //
  // Permission is a precondition. Whether atrium sends any is the answer, so
  // the answer is what shows, and the precondition is still readable in the
  // buttons: `enable` appears when it has not been asked for, and the toggle
  // only appears once it has been granted.
  const said = (perm === "granted" && !on) ? "off"
    : perm === "default" ? "not asked yet"
    : perm;
  state.textContent = said;
  state.className = "chip" + (said === "granted" ? " accent" : perm === "denied" ? " warn" : "");
  ask.hidden = perm !== "default";
  toggle.hidden = perm !== "granted";
  toggle.textContent = on ? "turn off" : "turn on";
  toggle.className = on ? "no" : "go";
  // "denied" is sticky: the browser will not ask again, and the enable button
  // can do nothing about it. Say so rather than letting it look broken.
  ask.disabled = perm !== "default";
  hint.innerHTML = {
    granted: (on ? "" : "atrium is not sending any. turn them back on above. ") +
             "a page cannot revoke its own permission, so fully blocking them is a browser setting. " +
             "if nothing appears, Windows is swallowing them: " +
             "check <b>Settings &rsaquo; System &rsaquo; Notifications</b> is on, that your browser is " +
             "listed and enabled there, and that <b>Do not disturb</b> or <b>Focus assist</b> is off. " +
             "note that real alerts only fire while this tab is in the background, though " +
             "<b>send a test</b> works either way.",
    denied: "this browser has notifications blocked for localhost and will not ask again. " +
            "click the icon at the left of the address bar, or open " +
            "<code>chrome://settings/content/notifications</code>, and allow " +
            "<code>localhost:7778</code>. then reload.",
    default: "click enable, then allow it in the browser prompt.",
    unsupported: "this browser has no notification support."
  }[perm] || "";
}

document.getElementById("s-expiry").addEventListener("change", e => {
  alerting.set({ expiry: Number(e.target.value) });
});
document.getElementById("s-debounce").addEventListener("change", e => {
  alerting.set({ debounce: Number(e.target.value) });
});
// Repainted at once, so turning it off takes the marks down without a poll.
document.getElementById("s-stuck").addEventListener("change", e => {
  alerting.set({ stuck: e.target.value });
  if (typeof repaintLists === "function") repaintLists();
});
document.getElementById("s-vol").addEventListener("input", e => {
  alerting.set({ volume: Number(e.target.value) / 100 });
});
document.getElementById("s-vol").addEventListener("change", () => alerting.preview(alerting.get().input));
document.getElementById("s-input").addEventListener("change", e => {
  alerting.set({ input: e.target.value });
  alerting.preview(e.target.value);
});
document.getElementById("s-perm").addEventListener("change", e => {
  alerting.set({ perm: e.target.value });
  alerting.preview(e.target.value);
});

function previewFor(which) {
  const p = alerting.get();
  alerting.preview(which === "perm" ? p.perm : p.input);
}

function toggleDesktop() {
  alerting.set({ desktop: alerting.get().desktop === false });
  paintSettings();
}

function askNotify() {
  if (!("Notification" in window)) { tellUser("atrium", "this browser has no notification support"); return; }
  if (Notification.permission === "denied") { paintSettings(); return; }
  // Some browsers still hand back a callback rather than a promise.
  const done = () => paintSettings();
  const result = Notification.requestPermission(done);
  if (result && typeof result.then === "function") result.then(done);
}

// Fires regardless of tab visibility, so the test proves the browser side
// works even while you are looking at this page.
function testNotify() {
  if (!("Notification" in window)) { tellUser("atrium", "this browser has no notification support"); return; }
  if (Notification.permission !== "granted") {
    tellUser("notifications are " + Notification.permission,
      "see the note under the buttons.");
    return;
  }
  const n = showNotification("atrium", "click this notification to prove it works", "perms");
  alerting.preview(alerting.get().perm);
  // Shown by the service worker, which hands nothing back. The button still
  // has to answer, so the answer is looked for. See `confirmTestShown`.
  if (!n) { confirmTestShown("atrium-perm"); return; }
  // Confirm in the page too, so a click is not a silent no-op.
  n.onclick = () => {
    window.focus();
    n.close();
    document.getElementById("settings").close();
    toast("notification click works", "it focused this window. real ones jump to the waiting item.");
    alerting.preview("chime");
  };
  n.onerror = () => toast("the browser could not show it",
    "Windows is blocking notifications for your browser. check Settings, System, Notifications.");
}

// Whether the test notification actually appeared, MEASURED rather than
// assumed.
//
// With a service worker there is no object to hold and no `onerror` to hang
// anything on: the notification is posted to the worker and the call returns
// nothing whether Windows drew it or swallowed it. So pressing a button
// labelled test said nothing at all, and it said nothing in precisely the case
// the button exists for, which is notifications being suppressed somewhere
// outside the browser. Silence reads as working.
//
// The registration knows. `getNotifications` lists what is on screen for this
// origin, so the test asks a moment later and reports what it found. Same
// shape as `closeThisWindow`: there is no event for refused, so the outcome is
// looked at rather than listened for.
//
// The shortest expiry the settings offer is ten seconds, so a notification
// that was shown is still up when this looks.
const testShownAfter = 600;
async function confirmTestShown(tag) {
  if (!swReg || !swReg.getNotifications) return;
  await new Promise(r => setTimeout(r, testShownAfter));
  let shown = [];
  try { shown = await swReg.getNotifications({ tag }); } catch (e) { return; }
  if (shown.length) {
    toast("it is on screen", "click it to prove the click gets you here too.");
    return;
  }
  toast("nothing appeared", "the browser accepted it and no notification was shown. " +
    "Windows is most likely blocking them for your browser: check Settings, System, Notifications.");
}

// Title carries the count so a background tab still tells you.
function retitle(waiting, perms) {
  const n = (waiting || 0) + (perms || 0);
  document.title = n ? `(${n}) atrium` : "atrium";
}

function badge(id, n) {
  const el = document.getElementById(id);
  el.textContent = n || "";
  el.classList.toggle("zero", !n);
}

// `id` is the key the card is drawn under, which for a request from a room is
// the room's name and the request's own id together. The card carries the two
// halves, because the decision has to be addressed to the machine that holds
// the blocked channel and named in the terms that machine uses.
async function decide(id, decision, forever) {
  const onCard = document.querySelector(`#perms-list .perm[data-id="${id}"]`);
  const room = (onCard && onCard.dataset.room) || "";
  const perm = (onCard && onCard.dataset.perm) || id;
  let reason = "", prefix = "", kind = "command";
  if (decision === "block") {
    // A block includes guidance, so a refusal reads as "no, do it this way
    // instead" rather than "no".
    reason = await askText("why not?",
      "Handed straight back to the agent as the reason it was refused. " +
      "Telling it what to do instead is more useful than a wall.",
      "", "use a temp directory instead");
    if (reason === null) return;
  }
  if (forever) {
    // The scope lives on the card, set by the buttons underneath it. A `.value`
    // read here threw, because a <code> element has no value, and the click
    // silently did nothing.
    const card = onCard;
    const view = document.getElementById("pat-" + id);
    prefix = (card && card.dataset.pattern) || (view && view.textContent) || "";
    prefix = prefix.trim();
    kind = (card && card.dataset.kind) || "command";
    if (!prefix) { tellUser("atrium", "pick a scope for the rule first"); return; }
  }
  // Send the command only when you actually changed it.
  const cmdEl = document.getElementById("cmd-" + id);
  const command = cmdEl && cmdEl.value.trim() !== cmdEl.defaultValue.trim() ? cmdEl.value.trim() : "";

  // Editing the command to something with a wildcard is almost always someone
  // aiming at the rule scope and hitting the wrong field. The command box runs
  // what is in it, so a star there becomes a shell glob and the tool fails on
  // a path that does not exist.
  if (command && /[*?]/.test(command) && !/[*?]/.test(cmdEl.defaultValue)) {
    const asRule = await askUser({
      title: "that wildcard is in the command box",
      body: "The command box is what actually <b>runs</b>, so approving this would execute:" +
        `<code>${esc(command)}</code>` +
        "Wildcards belong in the scope line underneath, which decides what " +
        "<b>always</b> and <b>never</b> cover.",
      buttons: [
        { label: "run it as typed", value: false },
        { label: "use it as the rule scope", value: true, style: "go" }
      ]
    });
    if (asRule === null) return;
    if (asRule) {
      const card = document.querySelector(`#perms-list .perm[data-id="${id}"]`);
      if (card) {
        card.dataset.pattern = command;
        card.dataset.chosen = "1";
        const view = document.getElementById("pat-" + id);
        if (view) view.textContent = command;
      }
      cmdEl.value = cmdEl.defaultValue;
      // Re-read, so the restored command is what gets sent.
      return decide(id, decision, forever);
    }
  }
  // A request from a room goes to the room, and the daemon here only queues it.
  // It cannot answer on that machine's behalf: the channel the agent is blocked
  // on is held in that machine's process and does not move.
  const where = room
    ? `/v1/rooms/${encodeURIComponent(room)}/permissions/${encodeURIComponent(perm)}/decide`
    : `/v1/permissions/${perm}/decide`;
  try {
    await api(where, {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ decision, reason, forever: !!forever, prefix, kind, command })
    });
  } catch (e) {
    // Already answered somewhere else. A toast rather than an alert: there is
    // nothing to acknowledge.
    toast("too late", e.message);
  }
  // Acting on a card updates the view even mid-edit. The pause stops a poll
  // overwriting what you are typing, but this card is finished with, and left
  // on screen the click looks like it did nothing. That turns one approval
  // into four.
  forgetEdits(id);
  // Out of the held queue too, so the repaint below does not draw it again
  // from a list read before it was answered. The `permission` event the
  // answer raises reads the queue again.
  permsLocal = permsLocal.filter(p => p.id !== id);
  remoteLocal = remoteLocal.filter(p => p.id !== id);
  permsSoon();
  applyHeld();
}

// Drops the edit state for a card that has been answered, so it no longer
// counts as an edit in progress and no longer holds the view paused.
function forgetEdits(id) {
  const card = document.querySelector(`#perms-list .perm[data-id="${id}"]`);
  if (!card) return;
  card.querySelectorAll("textarea, input").forEach(f => {
    f.dataset.touched = "";
    if (f.defaultValue !== undefined) f.value = f.defaultValue;
  });
  card.remove();
}

async function openTask(id) {
  current = await api(`/v1/tasks/${id}`);
  document.getElementById("d-title").textContent = current.display_title;
  document.getElementById("d-chips").innerHTML = [
    `<span class="chip accent">${esc(current.runner || "?")}</span>`,
    `<span class="chip" data-tip="${esc(current.status)}">${esc(statusLabel(current.status))}</span>`,
    current.pid ? `<span class="chip">pid ${current.pid}</span>` : "",
    current.worktree ? `<span class="chip">${esc(current.worktree)}</span>` : "",
    `<span class="chip">idle ${ago(current.idle_seconds)}</span>`,
    current.created_at
      ? `<span class="chip" data-tip="${esc(current.created_at)}">first seen ${
          esc(firstSeen(current.created_at))}</span>`
      : ""
  ].join("");
  document.getElementById("d-why").value = current.why || "";
  // The session's own account, when there is one. Editable, because deciding
  // an account was wrong is the operator's call, and read-only text somebody
  // disagrees with is worse than no text.
  paintNote();
  paintCardActions();
  // Offered only where there is a directory to look in. Folded shut each time
  // the dialog opens, since a panel that remembered being open would fetch a
  // listing every time you glanced at a card.
  const filesField = document.getElementById("d-files-field");
  filesField.hidden = !current.worktree;
  document.getElementById("d-files").hidden = true;
  document.getElementById("d-files-toggle").classList.remove("open");
  // Token use on record, folded the same way. See js/usage.js.
  paintUsageField();
  // The question, when there is one, named the way the row names it: a card
  // stopped on another SESSION reads differently from one stopped on you, and
  // that difference is the whole reason `ask_peer` exists.
  const askField = document.getElementById("d-ask-field");
  askField.hidden = !current.ask;
  document.getElementById("d-ask").value = current.ask || "";
  document.getElementById("d-ask-label").textContent = current.ask_peer
    ? "asked " + current.ask_peer
    : "this agent has a question";
  document.getElementById("d-ask-how").textContent = current.ask_peer
    ? "it put this to " + current.ask_peer + " rather than to you. it is still stopped, so " +
      "saying something here still reaches it."
    : "say something below and this comes off. any message answers it.";
  paintMoreAsks(current.id, current.asks_open || 0);

  const recapField = document.getElementById("d-recap-field");
  recapField.hidden = !current.recap;
  document.getElementById("d-recap").value = current.recap || "";
  document.getElementById("d-recap-when").textContent = current.recap_at
    ? "said so " + firstSeen(current.recap_at) + ". edit it if it is wrong, or clear it."
    : "";
  document.getElementById("d-tags").value = (current.tags || []).join(", ");
  paintKnownTags();

  // Terminate is only offered when atrium knows the process. A window-mode
  // launch owns itself, so saying so beats a button that cannot work.
  document.getElementById("d-runner").innerHTML = [
    current.supervised
      ? `<button class="go" onclick="attachCurrent()">attach terminal</button>`
      : "",
    current.pid > 0
      ? `<button class="no" onclick="killTask()">terminate pid ${current.pid}</button>`
      : `<span class="hintline">atrium does not own this process, so it cannot stop it.
         close it in its own terminal.</span>`,
    current.resume_id ? `<button onclick="resumeCurrent()">resume</button>` : "",
    // Auto mode and the review that pays for it, side by side.
    `<button class="${current.auto_approve ? "no" : ""}" onclick="toggleAuto()">${
      current.auto_approve ? "stop auto mode" : "auto mode"}</button>`,
    `<button onclick="openReview()">what did it do?</button>`,
    // Deleting one card. It left the context menu, which is a list of things
    // you do often and this is not one of them, and there is nowhere else to
    // do it: `clear` takes a whole column. It asks first.
    `<button class="no" onclick="forgetCurrent()">forget this card</button>`
  ].join("");
  document.getElementById("d-auto-note").textContent = current.auto_approve
    ? "requests from this session are approved without asking. everything is still recorded, " +
      "and never rules and shelving still block."
    : "";

  // "the board's tone" rather than an empty row, because empty reads as
  // broken where the default is a real choice.
  document.getElementById("d-sound").innerHTML =
    `<option value=""${current.sound ? "" : " selected"}>the board's tone</option>` +
    soundOptions(current.sound);
  const iconEl = document.getElementById("d-icon");
  iconEl.value = current.icon || "";
  paintIconPreview();
  // Drawn as you type, since a glyph that renders as a box here will render as
  // a box on the notification, and that is worth finding out before it does.
  iconEl.oninput = paintIconPreview;
  document.getElementById("d-say").value = "";
  // A message rides a hook, and a session that has ended fires no more of
  // them. Said rather than prevented: a card can be wrong about being dead,
  // and queueing something for a session you are about to resume is fair.
  document.getElementById("d-say-how").textContent =
    ["dead", "done"].includes(current.status)
      ? "this session has ended, so anything queued waits until something resumes it."
      : "";
  paintQueued();

  const ev = await api(`/v1/tasks/${id}/events?limit=400`);
  detailEvents = ev.events || [];
  paintHistoryGaps(ev);
  paintTimeline();
  detail.showModal();
}

// Says something to the open card.
//
// The reply names which of the two routes it took, and that is the whole point
// of reporting it: a message typed into a terminal has already landed, and a
// queued one has not and might not for minutes. One button doing two very
// different things in silence is how you end up sending it four times.
//
// `when` is "done" for send, which waits for the turn to end, or "immediate"
// for the button beside it, typed in as soon as the input line is empty.
//
// Through `busyWhile` on both routes, so Enter pressed again while the first
// is posting cannot send the same text twice. See js/core.js.
function sayToCurrent(when = "done") {
  if (!current) return;
  const box = document.getElementById("d-say");
  const text = box.value.trim();
  if (!text) return;
  const other = document.getElementById(when === "immediate" ? "d-say-send" : "d-say-now");
  if (other && other.dataset.busy) return;
  const btn = document.getElementById(when === "immediate" ? "d-say-now" : "d-say-send");
  return busyWhile(btn, () => sayNow(current, box, text, when), "sending…");
}

async function sayNow(task, box, text, when) {
  const how = document.getElementById("d-say-how");
  how.textContent = "";
  try {
    const res = await api(`/v1/tasks/${task.id}/message`, {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ text, when })
    });
    box.value = "";
    how.textContent = sayOutcome(res);
    await paintQueued();
  } catch (e) {
    how.textContent = `not sent: ${e.message || e}`;
  }
}

// What happened to a message, from the room's answer. A room without the
// `when` field answers as it always did.
function sayOutcome(res) {
  if (res.delivered === "terminal") return "typed into its terminal.";
  if (res.warning) return res.warning;
  if (res.when === "done") return "waiting for its turn to end, then typed in or carried by its Stop hook.";
  return `queued. it is typed in when the input line clears, or arrives on the session's next tool call${turnEndReach()}`;
}

// Whether the end of a turn is a second way in, said only when it is true.
//
// A queued message rides the next tool call, and a session sitting idle makes
// none. The Stop hook is the only thing that reaches one, and it is off by
// default. Claiming a message will arrive "when its turn ends" on a machine
// where that hook is not wired is the box promising something that will not
// happen, and the message sits in the queue looking ignored.
function turnEndReach() {
  const h = (hookReport && hookReport.hooks || []).find(x => x.event === "turn-end");
  if (h && h.installed && !h.stale) return ", or when its turn ends.";
  return ". a session sitting idle makes no tool calls, so it will wait there until " +
    "something happens. wire the Stop hook under rooms > runners to reach an idle session.";
}

// What has been said and has not arrived. Only ever queued messages: one typed
// into a terminal is already gone and has nothing to wait for.
async function paintQueued() {
  const host = document.getElementById("d-say-queue");
  if (!current) return;
  let msgs = [];
  try {
    ({ messages: msgs } = await api(`/v1/tasks/${current.id}/messages`));
  } catch { return; }
  setHTML(host, (msgs || []).map(m =>
    `<div class="q"><span>waiting</span><span class="txt">${esc(m.text)}</span>
     <span data-tip="${esc(m.created_at)}">${esc(ago(sinceSecs(m.created_at)))} ago</span></div>`
  ).join(""));
}

let detailEvents = [];

// The house-keeping kinds. Real, worth keeping, and not what anyone opens a
// card to read.
const NOISE_EVENTS = ["status-changed", "prompted", "submitted"];

function paintTimeline() {
  const seg = document.querySelector("#d-ev-seg .on");
  const all = seg && seg.dataset.v === "all";
  const shown = all
    ? detailEvents
    : detailEvents.filter(e => !NOISE_EVENTS.includes(e.kind));
  const hidden = detailEvents.length - shown.length;
  document.getElementById("d-ev-count").textContent =
    hidden > 0 ? `${hidden} housekeeping events hidden` : "";
  setHTML(document.getElementById("d-events"), timelineHTML(shown));
}

// Says what the history below does not hold, when the room keeps part of it
// out of the db: older events rolled off the hot window, or kinds routed to a
// cold sink only. Silent under the default, where the db has everything.
function paintHistoryGaps(ev) {
  const gaps = [];
  if (ev.rolled_off) gaps.push("older events rolled off");
  const cold = ev.cold_only_kinds || [];
  if (cold.length) gaps.push(`${cold.join(", ")} kept in the event archive only`);
  document.getElementById("d-ev-gaps").textContent =
    gaps.length ? `not shown: ${gaps.join("; ")}` : "";
}

// One line per thing that happened, newest first.
//
// A permission used to take three lines: the request, a status change into
// needs-permission, and the decision, with the command on the first and the
// outcome on the third. Reading what was allowed meant pairing them by eye.
// They are one line here, and the status changes those requests caused are
// dropped, since the request line already says the session was waiting.
function timelineHTML(events) {
  if (!events.length) return '<div class="empty">nothing has happened yet</div>';

  const decisions = {};
  events.forEach(e => {
    if (e.kind === "perm-decided" && e.payload && e.payload.id) {
      decisions[e.payload.id] = e.payload;
    }
  });

  const rows = [];
  events.slice().reverse().forEach(e => {
    const p = e.payload || {};
    // Folded into its request below.
    if (e.kind === "perm-decided" && decisions[p.id]) return;
    // Churn between running and waiting is what a permission IS, so it says
    // nothing the request line does not.
    if (e.kind === "status-changed" && isPermChurn(p)) return;

    if (e.kind === "perm-requested") {
      rows.push(permRow(e, p, decisions[p.id]));
      return;
    }
    const detail = p.text || p.content || p.command
      || (p.from ? `${p.from} to ${p.to}` : "")
      || (p.decision ? `${p.decision}: ${p.reason || ""}` : "");
    rows.push(evRow({
      cls: KEY_EVENTS.includes(e.kind) ? "key" : "",
      verdict: esc(e.kind), at: evTime(e),
      text: esc(String(detail).slice(0, 400))
    }));
  });
  return `<div class="evtable">${rows.join("")}</div>`;
}

// One event, one row, five columns. Anything with nothing to put in a column
// leaves it empty rather than pushing the rest along, so the columns line up
// down the whole list and can be read as one.
function evRow(r) {
  return `<div class="evr ${r.cls || ""}">
    <span class="c-verdict ${r.vcls || ""}">${r.verdict}</span>
    <span class="c-at">${esc(r.at)}</span>
    <span class="c-tool">${r.tool || ""}</span>
    <span class="c-cmd" data-tip="${r.title || ""}">${r.text || ""}</span>
    <span class="c-by">${r.by || ""}</span>
  </div>`;
}

const isPermChurn = p =>
  (p.from === "running" && p.to === "needs-permission") ||
  (p.from === "needs-permission" && p.to === "running");

const evTime = e => (e.at || "").slice(11, 19);

// A request and what answered it, on one line.
//
// The verdict leads, because scanning an audit log is looking for the ones that
// were allowed. What answered it matters as much as the answer: approved by a
// standing rule, by auto mode, or by you are three different things.
function permRow(e, p, d) {
  const raw = String(p.command || p.text || "");
  const row = {
    cls: "key", at: evTime(e),
    tool: esc(p.tool || ""), text: esc(raw.slice(0, 400)), title: esc(raw)
  };
  if (!d) {
    return evRow(Object.assign(row, { verdict: "asked", vcls: "pending" }));
  }
  const ok = d.decision === "approve";
  return evRow(Object.assign(row, {
    verdict: ok ? "approved" : "blocked",
    vcls: ok ? "ok" : "no",
    // A block's reason is the useful half, so it takes the column that would
    // otherwise say what refused it.
    by: esc(!ok && d.reason ? d.reason : decidedHow(d))
  }));
}

// Who or what answered. `by` carries the rule's own pattern when a rule
// matched, so anything that is not one of the known words IS the rule.
function decidedHow(d) {
  switch (d.by) {
    case "auto": return "auto mode";
    case "global-auto": return "global auto";
    // An answer given earlier and handed back to a retry. Said out loud,
    // because a refusal nobody remembers giving is the confusing one.
    case "replay": return "replayed an earlier answer";
    // Nobody answered this: the session that asked it went away first.
    case "orphaned": return "the session went away";
    case "you": case "": case undefined: return "you";
    case "message": return "a message you queued";
    case "shelved": return "the card was shelved";
    default: return `rule ${d.by}`;
  }
}

// Reads its arguments off `current` rather than carrying a worktree path
// through an inline handler, where a directory named `it's mine` would break
// out of the string literal.
function resumeCurrent() {
  if (!current) return;
  openLaunch(current.runner || "claude", current.resume_id, current.worktree || "");
}

// Auto mode: stop asking, keep recording.
//
// Turning it on is confirmed, since it changes what the gate does. Turning it
// off is not: going back to being asked is the safe direction.
async function toggleAuto() {
  if (!current) return;
  const on = !current.auto_approve;
  if (on) {
    const ok = await confirmUser("let this session run unattended?",
      `Requests from <b>${esc(current.display_title)}</b> will be approved without asking you.` +
      `<br><br>Everything is still recorded, and <b>never</b> rules and shelving still block. ` +
      `Read what it did afterwards with <b>what did it do?</b>`,
      "turn auto mode on");
    if (!ok) return;
  }
  const title = current.display_title;
  await patch({ auto_approve: on });
  detail.close();
  toast(on ? "auto mode on" : "auto mode off",
    on ? `${title} will not stop to ask` : `${title} will ask again`);
}

// The counterpart to auto mode: what this session was allowed to do.
//
// The daemon folds repeats, groups by tool, and puts the decisions nobody saw
// first, since a flat list of four hundred approvals will not be read.
async function openReview() {
  if (!current) return;
  const rev = await api(`/v1/tasks/${current.id}/review`);
  const title = current.display_title;
  if (!rev.total) {
    tellUser("nothing to review", `${title} has not been asked about anything yet.`);
    return;
  }

  const head = `<div class="rev-sum">
    <span><b>${rev.total}</b> decisions</span>
    <span class="${rev.unattended ? "hot" : ""}"><b>${rev.unattended}</b> nobody saw</span>
    <span class="${rev.blocked ? "hot" : ""}"><b>${rev.blocked}</b> blocked</span>
  </div>`;

  const body = rev.groups.map(g => `
    <details class="rev-group" ${g.unattended ? "open" : ""}>
      <summary>
        <span class="tool">${esc(g.tool)}</span>
        <span class="n">${g.count}</span>
        ${g.unattended ? `<span class="chip auto">${g.unattended} unseen</span>` : ""}
        ${g.blocked ? `<span class="chip warn">${g.blocked} blocked</span>` : ""}
      </summary>
      <div class="rev-rows">${g.entries.map(e => `
        <div class="rev-row ${e.unattended ? "unseen" : ""}">
          <span class="verdict ${e.decision === "approve" ? "" : "no"}">${
            e.decision === "approve" ? "approved" : "blocked"}</span>
          <span class="rep" ${e.repeats > 1 ? `data-tip="identical calls folded together"` : ""}>${
            e.repeats > 1 ? "&times;" + e.repeats : ""}</span>
          <code data-tip="${esc(e.command)}">${esc(e.command)}</code>
          <span class="by">${e.unattended ? `<span class="auto">auto</span>` : esc(e.by || "you")}</span>
        </div>`).join("")}</div>
    </details>`).join("");

  reviewDlg.querySelector("#rev-title").textContent = `what ${title} did`;
  reviewDlg.querySelector("#rev-body").innerHTML = head + body;
  reviewDlg.showModal();
}

// Patches the card the detail dialog is open on. Goes through patchTask so
// shelving and unshelving report what happened to the runner either way.
async function patch(body) {
  if (!current) return;
  await patchTask(current.id, body);
  refresh();
}

async function move(status) {
  // Moving a card answers anything it is holding. Say so before doing it,
  // because the agent on the other end is frozen until something answers.
  if (current && status !== "needs-permission") {
    let pending = [];
    try {
      pending = ((await api("/v1/permissions")).permissions || [])
        .filter(p => p.task_id === current.id);
    } catch (e) {}
    if (pending.length) {
      const what = pending.length === 1 ? "1 request" : `${pending.length} requests`;
      const extra = status === "shelved"
        ? " While it stays shelved, anything else it asks for is blocked too, " +
          "with the same explanation."
        : "";
      if (!await confirmUser(`this is holding ${what}`,
        `<b>${esc(current.display_title)}</b> has an agent waiting on it. ` +
        `Moving it to <b>${esc(status)}</b> answers ${pending.length === 1 ? "it" : "them"} ` +
        `with block, so the agent is released and told why rather than waiting forever.${extra}`,
        `move it to ${status}`, "move-while-pending")) {
        return;
      }
    }
  }
  await patch({ status });
  detail.close();
}

async function attachTask(id) {
  // Attaching is looking at it, so a new card's mark goes. See js/newcard.js.
  newCardClear(id);
  // Already in a window of its own. Raise that rather than attaching here.
  //
  // Two views onto one terminal both taking input is the situation
  // `docs/terminal/supervision-design.md` says nothing arbitrates, and popping out
  // detaches the board's pane for exactly that reason. Attaching again from
  // the switcher would put it straight back, quietly.
  if (poppedOut(id)) {
    const what = await popOutTask(id);
    // Said from what happened rather than from what was believed. A claim can
    // be a heartbeat stale, so "it is already open" is a guess until the
    // window has actually been found.
    //
    // The raised case says nothing. The window coming to the front is the
    // answer, and a toast in it saying so was one more thing to dismiss. With
    // no channel the board cannot know a raise landed, so it says where the
    // terminal is from here, which is behind you but better than nowhere.
    if (what === "raised") {
      if (!soloBus) toast("it is in its own window", "raised it for you");
    } else if (what === "opened") {
      toast("it was not there any more", "opened it again");
    }
    // `unreachable` and `blocked` have both already said so, and both say
    // something this window is the right place for: they are the two answers
    // where you are not going anywhere.
    return;
  }
  // Recorded from what happened, not from what was asked. The popped-out path
  // above returns before this, and `popOutTask` records its own side, so each
  // destination is written where it is actually reached. See `rememberPlace`:
  // resume reads this instead of asking where to open.
  rememberPlace(id, "here");
  try { openTerm(await api(`/v1/tasks/${id}`)); }
  catch (e) { toast("could not attach", e.message); }
}

// Whether this card is showing in a window of its own.
//
// Two sources, because neither is complete on its own: a handle this page
// opened, and a claim broadcast by a window it did not. The second covers
// every window that outlived a reload of this one.
function poppedOut(id) {
  const held = popOuts.get(id);
  if (held && !held.closed) return true;
  const at = soloHeld.get(id);
  if (!at) return false;
  // Gone quiet. Dropped rather than left, so this answers the same way next
  // time without waiting for anything else to notice.
  if (Date.now() - at > soloClaimFor) { soloHeld.delete(id); return false; }
  return true;
}

function attachCurrent() {
  if (!current) return;
  const task = current;
  detail.close();
  openTerm(task);
}

// Keyed with the card menu's terminate. See oneAtATime in js/core.js.
function killTask() {
  if (!current) return;
  return oneAtATime("kill:" + current.id, () => killTaskNow(current));
}

async function killTaskNow(t) {
  if (!await confirmUser("stop this runner?",
    `<b>${esc(t.display_title)}</b> is killed. The card and its history stay.`,
    "stop it", "kill-runner")) return;
  try { await api(`/v1/tasks/${t.id}/kill`, { method: "POST" }); }
  catch (e) { toast("could not terminate", e.message); return; }
  detail.close();
  refresh();
}

// COMMITTING ON CHANGE IS THE ONLY WAY THIS DIALOG COMMITS, and the button at
// the top says so rather than adding a second one.
//
// `patchOnChange` is what every field here does: `change` fires when focus
// leaves the field, and pressing the button at the top of the dialog moves
// focus, so the write lands on the way out. That is correct and it was
// invisible, which made it indistinguishable from having lost the edit, and
// `close` is the wrong word for a button that commits.
//
// TWO WAYS OUT OF THAT, and only one of them is whole.
//
// Holding the edits and committing on a button was the other, and it was
// refused. This dialog is LIVE: it repaints, it polls the queue, it draws a
// timeline, and the daemon changes the card underneath it while it is open. A
// held draft would have to argue with all of that, and every other field in the
// board (tags, sound, icon, recap, the settings screen) commits on change, so
// this would become the one dialog on the board that behaves differently.
//
// Adding a save button while `change` still patched was refused for a simpler
// reason: two ways to commit, one of them still invisible, which is the defect
// with a button in front of it.
//
// So: commit on change, keep it the only path, and SAY SO. The button is named
// for what it does and the field says the edit is already kept. Nothing here
// can be lost by pressing the wrong thing, because there is no wrong thing to
// press.
const patchOnChange = (id, build) =>
  document.getElementById(id).addEventListener("change", e => patch(build(e.target.value)));

patchOnChange("d-why", v => ({ why: v }));
patchOnChange("d-recap", v => ({ recap: v }));

// Picked and heard in one place. Choosing a bell you cannot hear until the
// next time that agent wants you is choosing blind, and by then it is too late
// to be the one you meant.
document.getElementById("d-sound").addEventListener("change", e => {
  const name = e.target.value;
  patch({ sound: name });
  if (name) alerting.preview(name);
});
function previewCardSound() {
  const name = document.getElementById("d-sound").value;
  alerting.preview(name || alerting.get().input);
}

// Saved on the way out of the field, not on every keystroke. An emoji arrives
// a code unit at a time and half of one is not a mark worth storing.
patchOnChange("d-icon", v => ({ icon: v.trim() }));

// Enter sends, shift-enter is a newline. The chat convention, because this is
// one: short things said to somebody, in a row. Escape would close the dialog
// out from under a half-typed message, so it is caught and only clears the box.
// Ctrl-enter is the immediately button.
document.getElementById("d-say").addEventListener("keydown", e => {
  if (e.key === "Enter" && !e.shiftKey) {
    e.preventDefault();
    sayToCurrent(e.ctrlKey || e.metaKey ? "immediate" : "done");
  } else if (e.key === "Escape" && e.target.value) {
    e.preventDefault();
    e.stopPropagation();
    e.target.value = "";
  }
});

// Saved on change rather than on every keystroke, so a half-typed tag is not
// stored and read straight back under the cursor. The daemon lower cases and
// dedupes, so what comes back may differ from what was typed.
patchOnChange("d-tags", v => ({ tags: v.split(",").map(s => s.trim()).filter(Boolean) }));

// Every tag already in use, so the second card gets the same spelling as the
// first. Free text with no memory is how "discourse" and "discource" both end
// up being groups.
function knownTags() {
  const seen = new Set();
  (allStack || []).forEach(t => (t.tags || []).forEach(x => seen.add(x)));
  return [...seen].sort();
}

function paintKnownTags() {
  const host = document.getElementById("d-tag-known");
  if (!host) return;
  const mine = new Set(current && current.tags || []);
  const rest = knownTags().filter(t => !mine.has(t));
  setHTML(host, rest.map(t =>
    `<button class="chip tag" style="--ghue:${groupHue(t)}"
       onclick="addTag('${esc(t).replace(/'/g, "&#39;")}')">+ ${esc(t)}</button>`).join(""));
}

function addTag(tag) {
  const el = document.getElementById("d-tags");
  const have = el.value.split(",").map(s => s.trim()).filter(Boolean);
  if (have.includes(tag)) return;
  have.push(tag);
  el.value = have.join(", ");
  patch({ tags: have });
  paintKnownTags();
}

// Holding the repaint while you type is TURNED OFF.
//
// It existed because a poll landing mid-edit could wipe a command you were
// changing, and it cost a pill on screen saying the board was out of date. In
// practice the edit survives, and the pill was confusing enough to be worse
// than the problem.
//
// Left in place rather than deleted, because the failure it guarded against is
// real and may come back with a longer poll or a slower machine. Set this to
// true to bring it back.
const HOLD_WHILE_TYPING = false;

let heldUpdate = false;

// Stops the board rewriting itself, on purpose, until you say otherwise.
//
// The five second poll replaces the card markup, because every card carries an
// age and every age has changed. That is right for a board you are watching
// and impossible for a board you are INSPECTING: the node selected in devtools
// is detached a moment later, the styles pane empties, and an edit typed into
// it is gone before it can be read. Working out why anything looks wrong was
// the one thing the board actively prevented.
//
// Shift-P, and it says so in the header, because a board that has silently
// stopped updating is worse than one that never did. The poll keeps running:
// counts, sounds and notifications are unaffected, since those are what tell
// you something arrived. Only the repaint is held, which is the same machinery
// `isEditing` already uses to avoid rewriting a form under your hands.
let paintPaused = false;
// The banner's own words, kept so unpausing puts them back. It is shared with
// the hold-while-typing case, which says something different and is the one
// that appears on its own.
const HELD_TYPING = "&#8635;&nbsp; paused while you type, click to catch up";
function togglePaintPause(on) {
  paintPaused = (on === undefined) ? !paintPaused : !!on;
  const el = document.getElementById("held");
  if (el) el.innerHTML = paintPaused
    ? "&#10073;&#10073;&nbsp; frozen for inspection. shift-P or click to resume"
    : HELD_TYPING;
  showHeld(paintPaused);
  if (!paintPaused) refresh();
}
document.addEventListener("keydown", e => {
  // Shift and P, and not while anything is taking text: a capital P belongs in
  // whatever is being typed.
  if (e.key !== "P" || e.ctrlKey || e.altKey || e.metaKey) return;
  const el = document.activeElement;
  if (el && (el.tagName === "TEXTAREA" || el.tagName === "INPUT" || el.isContentEditable)) return;
  e.preventDefault();
  togglePaintPause();
});

function isEditing() {
  if (paintPaused) return true;
  if (!HOLD_WHILE_TYPING) return false;
  const el = document.activeElement;
  if (el && (el.tagName === "TEXTAREA" || (el.tagName === "INPUT" && el.type !== "range"))
      && !el.closest("dialog")) {
    return true;
  }
  // A pattern or command you changed but have not acted on yet counts as an
  // edit in progress even after the field loses focus.
  return [...document.querySelectorAll("#perms-list .pat, #perms-list .cmd.edit")]
    .some(f => f.dataset.touched === "1" || (f.defaultValue !== undefined && f.value !== f.defaultValue));
}

// IS SOMETHING SELECTED RIGHT NOW.
//
// Every list on this board is drawn by assigning `innerHTML`, which throws
// away the nodes a selection is anchored in, so the selection goes with them.
// Repainting on a timer therefore means dragging across a card title and
// watching the highlight vanish under the cursor before it can be copied. The
// poll caused it and the event stream causes it too, so raising the interval
// only makes it rarer, which is worse: a bug that fires once a minute is one
// nobody can reproduce on purpose.
//
// Held SILENTLY, with no banner, unlike an edit in progress. A half-finished
// permission rule is state worth announcing that the board is sitting on. A
// selection lasts as long as it takes to press ctrl-c, and a banner appearing
// every time somebody drags across a word would be the more irritating of the
// two problems.
//
// Nothing can wedge here: a selection is cleared by the next click anywhere,
// and `keepUpWithSelection` repaints the moment it collapses rather than
// leaving the board a poll behind.
function isSelecting() {
  const sel = typeof getSelection === "function" ? getSelection() : null;
  if (!sel || !sel.rangeCount || sel.isCollapsed) return false;
  return String(sel).length > 0;
}

// Catch up as soon as the selection goes away.
//
// Without this the board stays as stale as whatever withheld the repaint,
// which is up to a full poll after a copy that took a second. `selectionchange`
// fires on the document for every change including the collapse, and the guard
// on `heldUpdate` means it costs nothing on the ordinary case where no repaint
// was ever withheld.
function keepUpWithSelection() {
  document.addEventListener("selectionchange", () => {
    if (heldUpdate && !isSelecting() && !isEditing()) paintSoon();
  });
}

function showHeld(show) {
  document.getElementById("held").classList.toggle("on", !!show);
}

function applyHeld() {
  // The banner is one button for two reasons to be paused, so pressing it has
  // to answer both. Freezing for inspection and then finding the only way out
  // was a keystroke nothing on screen mentioned would be its own small trap.
  if (paintPaused) { togglePaintPause(false); return; }
  heldUpdate = false;
  showHeld(false);
  repaintLists();
}

// `fromStore` is a pass that re-read nothing the lists draw from: a task event
// that carried its whole row, a permission answered, a held repaint let go. The
// board, the stack, the terminals and the perms queue paint from what is held,
// so they repaint. The runners page and the history fetch their own and are not
// drawn from the cards, so a pass like that leaves them alone rather than
// turning every task event into a round of their requests.
function repaintLists(signal, fromStore) {
  const view = document.querySelector(".tab.on").dataset.view;
  if (fromStore && (view === "runners" || view === "history")) return;
  const render = {
    board: renderBoard, stack: renderStack, perms: renderPerms,
    runners: renderRunners, terms: renderTerms,
    // HISTORY IS A VIEW LIKE THE REST and was missing from this table, so any
    // event arriving while it was open threw "render is not a function" and
    // took the repaint with it: the list stopped updating and the only sign was
    // in the console. A tab added without a line here fails exactly this way,
    // which is why the fallback below exists as well.
    history: () => renderHistory(false, true),
    // The audit pane is live by its own `audit` delta (see `onAuditEvent`), so a
    // board event has nothing to repaint there. Without a line it logged "no
    // renderer" on every refresh pass while the pane was open.
    audit: () => Promise.resolve()
  }[view];
  // A VIEW NOBODY WIRED UP MUST NOT STOP THE REPAINT. Every other list on the
  // page is behind this call, and one unknown tab name would silently freeze
  // all of them.
  if (typeof render !== "function") {
    console.error("no renderer for the " + view + " view");
    return;
  }
  // Returned so a refresh pass can await the paint's own fetches, and passed the
  // pass's abort signal so the watchdog can actually cancel a hung list fetch,
  // not just stop waiting on it. The single-flight guard counts a pass done only
  // when everything it started has settled.
  return render(signal).catch(e => console.error(e));
}

// One refresh for a burst of events.
//
// The stream publishes a task event on every change to a card, and a working
// agent changes one constantly: a tool starts, a tool ends, the activity badge
// moves. Bound straight to `refresh`, each of those cost three fetches and a
// rebuild of every card's markup, and with one busy session the board tab sat
// at a fifth of a CPU doing it. A popped-out terminal next to it, drawing on
// the GPU, was at one percent.
//
// Trailing rather than leading: the events arrive in clumps, and the state
// worth drawing is the one at the end of the clump. A quarter second is below
// what anybody notices on a board and far above the gap between two events
// from the same tool call.
// A trailing debounce, so a clump of events draws once at the end of the clump.
// The window is reset on every event, which is what collapses ten flaps in three
// seconds into one refresh instead of ten. A room that never stops flapping would
// reset the window forever and the board would never repaint, so the wait is
// capped: past REFRESH_MAXWAIT since the first held event, the pass runs anyway
// and a fresh window begins.
const REFRESH_DEBOUNCE = 300;
const REFRESH_MAXWAIT = 1500;
let refreshTimer = 0;
let refreshFirst = 0;
function passSoon() {
  const now = Date.now();
  if (!refreshFirst) refreshFirst = now;
  if (refreshTimer) clearTimeout(refreshTimer);
  const wait = Math.min(REFRESH_DEBOUNCE,
    Math.max(0, refreshFirst + REFRESH_MAXWAIT - now));
  refreshTimer = setTimeout(() => {
    refreshTimer = 0;
    refreshFirst = 0;
    runPass();
  }, wait);
}

// WHAT THE NEXT PASS HAS TO RE-READ.
//
// A pass used to fetch everything, and it ran on every clump of events, so a
// board with three hundred cards pulled half a megabyte of cards every two or
// three seconds to learn that one of them had moved. Now each thing the pass
// reads is fetched only when something said it changed, and a pass with nothing
// marked repaints from what is held (see js/cards.js) without a request.
//
// A flag is taken as its fetch starts and put back if the fetch fails or is
// aborted, so an event that lands mid-fetch marks it again and the pass that
// follows reads it again, and a fetch that never finished is retried.
const want = { tasks: true, perms: true, shares: true, health: true };
function wantAll() { want.tasks = want.perms = want.shares = want.health = true; }

// Everything, soon. The stream reopening, a room coming or going, and every
// action on the page that changed something and wants to see the result.
function refreshSoon() { wantAll(); passSoon(); }
// Nothing new to read: repaint from what is held.
function paintSoon() { passSoon(); }
function permsSoon() { want.perms = true; passSoon(); }
function healthSoon() { want.health = true; passSoon(); }

// What `/v1/health` says, applied. Also what the `health` event says (r-017): it is sent on a halt, when the
// settle window opens and once when it closes, so nothing re-reads while settling. It carries no build.
function applyHealth(h) {
  // Before anything else reads it. A daemon that is still putting sessions back says so here, and the arrival
  // alert re-seeds rather than announcing six terminals you restarted yourself.
  alerting.settling(!!h.settling);
  const el = document.getElementById("halted");
  el.style.display = h.halted ? "flex" : "none";
  if (h.halted) {
    document.getElementById("halt-t").innerHTML =
      `agents are parked and will not reconnect until you restart. <code>${esc(h.cause)}</code>`;
  }
}
function onHealthEvent(e) {
  let h = null;
  try { h = JSON.parse(e.data); } catch (err) {}
  if (!h || typeof h !== "object") { healthSoon(); return; }
  applyHealth(h);
}

// A TASK EVENT THAT IS NOT A WHOLE ROW, which is every task event until the
// daemon sends `row: 1`. It says a card changed and not what to, so the list
// is read again, but no more than once every TASKS_EVERY. Trailing, so the
// state read is the one after the burst, and single: one timer, and a pass
// that read the list in the meantime pushes it back rather than being followed
// by a second read. See `cardRowComplete`.
const TASKS_EVERY = 5000;
let tasksTimer = 0;
function tasksSoon() {
  if (tasksTimer) return;
  const fire = () => {
    const wait = cardsReadAt + TASKS_EVERY - Date.now();
    if (wait > 0) { tasksTimer = setTimeout(fire, wait); return; }
    tasksTimer = 0;
    want.tasks = true;
    passSoon();
  };
  tasksTimer = setTimeout(fire, Math.max(0, cardsReadAt + TASKS_EVERY - Date.now()));
}

// `task`: a whole row goes into the map and repaints with no request. Anything
// less is a reason to re-read, throttled. A popped-out window only cares about
// its own card. See `soloTaskEvent`.
function onTaskEvent(e) {
  let d = null;
  try { d = JSON.parse(e.data); } catch (err) {}
  if (typeof dockKick === "function") dockKick(d && d.id);
  if (termOnly()) { soloTaskEvent(d); return; }
  hearActivity(d);
  if (cardRowComplete(d)) { upsertCard(d); paintSoon(); return; }
  tasksSoon();
}

// A card doing something, told to the ready alert so it can wait for quiet.
// Whole rows count only when the live activity changed (its clocks tick and
// would never be quiet), and a row without one counts because it cannot say.
function hearActivity(d) {
  if (!d || !d.id || typeof alerting === "undefined") return;
  if (!cardRowComplete(d)) { alerting.activity(d.id); return; }
  const was = cardRows.get(d.id);
  const shape = a => JSON.stringify(a || null, (k, v) => /seconds$/.test(k) ? undefined : v);
  if (!was || shape(was.activity) !== shape(d.activity)) alerting.activity(d.id);
}

// `task-removed`: with an id, that card goes. Without one it was the sweep,
// which does not say which, so the list is read again.
function onTaskRemovedEvent(e) {
  let d = {};
  try { d = JSON.parse(e.data) || {}; } catch (err) {}
  if (termOnly()) { soloTaskEvent(d.id ? d : null); return; }
  if (d.id) { dropCard(d.id); paintSoon(); return; }
  want.tasks = true;
  passSoon();
}

// THE SAFETY RESYNC, the only poll left on the page. Everything else arrives on
// the event stream. This is for what the stream cannot say: a connection that
// dropped without closing, a counter that only ticks with the clock. Only while
// somebody can see the page: a hidden tab reads everything again the moment it
// is shown instead, which is the first moment it could matter.
// Overridable only so a headless test can prove the hidden-tab rule without
// waiting minutes. A plain board never sets it.
const RESYNC_MS = (typeof window !== "undefined" && window.__atriumResyncMs) || 60000;
function startResync() {
  setInterval(() => {
    if (document.visibilityState !== "visible") return;
    runRefresh();
    // The room chip too, which used to poll on its own every ten seconds. See
    // `startRooms`.
    if (typeof hubIsHub !== "undefined" && hubIsHub) loadHubRooms();
    if (typeof dockResync === "function") dockResync();
  }, RESYNC_MS);
  document.addEventListener("visibilitychange", () => {
    if (document.visibilityState === "visible") refreshSoon();
  });
}

// SINGLE-FLIGHT. One full pass runs at a time. A trigger that lands while a pass
// is in flight marks the board dirty and runs EXACTLY ONE more pass when the
// current one settles, rather than starting a second overlapping fan-out. This
// is what bounds the storm: whatever the event rate, at most one pass worth of
// fetches (and the cap in `api()` bounds those in turn) is ever on the wire.
//
// SUPERSEDE. Each pass runs under an AbortController. Starting the next pass
// aborts the last one's controller, so any fetch still holding a socket from a
// pass that has been overtaken is dropped rather than left to occupy the pool.
//
// BACK OFF. When the wire is down, `api()` counts the failures. A dirty pass
// then waits before it runs, growing the wait with the failure streak, so a tab
// that cannot reach the hub does not turn a flap into hundreds of queued fetches.
//
// WATCHDOG. A pass counts done when its fan-out settles, which is the whole
// point of the await inside `refresh`. But a fetch can hang and never resolve or
// reject: a proxy that holds the socket open, a hub wedged mid-answer, a request
// caught behind an exhausted pool with no timeout of its own. If that happened
// the in-flight flag would never clear and the board would stop refreshing for
// good, blank on whatever it last drew. So the pass also races a deadline: past
// RUN_TIMEOUT it is treated as finished, its fetches aborted so they stop
// holding sockets, and the loop is free to run the next pass. The hung fetch is
// abandoned rather than waited on.
// Thirty seconds, past any answer a healthy hub gives and well short of a person
// giving up on a frozen board. Overridable only so a headless test can prove the
// unwedge without waiting the full timeout; a plain board never sets it.
const RUN_TIMEOUT =
  (typeof window !== "undefined" && window.__atriumRunTimeout) || 30000;
let refreshInFlight = false;
let refreshDirty = false;
let refreshController = null;
// Counts passes as they start, so a waiter can ask for one that began after it
// did. The hub restart cover waits on one. See `onRefreshSettled`.
let refreshSeq = 0;
// A full pass: everything read again. Boot, the safety resync, and the tests.
function runRefresh() { wantAll(); runPass(); }
function runPass() {
  if (refreshInFlight) { refreshDirty = true; return; }
  refreshInFlight = true;
  const seq = ++refreshSeq;
  refreshDirty = false;
  if (refreshController) refreshController.abort();
  refreshController = typeof AbortController !== "undefined"
    ? new AbortController() : null;
  const ctrl = refreshController;
  let watchdog = 0;
  const guard = new Promise(done => {
    watchdog = setTimeout(() => {
      // Abort the stale pass so its sockets are released, and mark the board
      // dirty so the pass that replaces it actually repaints rather than
      // assuming the hung one will.
      if (ctrl) ctrl.abort();
      refreshDirty = true;
      done();
    }, RUN_TIMEOUT);
  });
  Promise.race([
    Promise.resolve(pass(ctrl && ctrl.signal)).catch(() => {}),
    guard
  ]).finally(() => {
    clearTimeout(watchdog);
    refreshInFlight = false;
    if (typeof onRefreshSettled === "function") onRefreshSettled(seq);
    if (typeof onDownRefreshSettled === "function") onDownRefreshSettled(seq);
    if (refreshDirty) {
      refreshDirty = false;
      const streak = typeof apiFailStreak === "number" ? apiFailStreak : 0;
      const backoff = streak > 0 ? Math.min(5000, 500 * streak) : 0;
      setTimeout(runPass, backoff);
    }
  });
}

// Called straight from the page after an action that changed a card, so the
// cards are read again and the result shows now rather than when the event
// that follows it is let through. A control that only changes how the lists
// are drawn calls `repaintLists` instead, which reads nothing.
function refresh() {
  want.tasks = true;
  return pass();
}

async function pass(signal) {
  // A popped-out window polls for ONE card. Falling through here meant it ran
  // the board's whole alerting pass, so a window opened onto one session put
  // up desktop notifications for every other one, from a document with no
  // board to click through to.
  if (termOnly()) return soloRefresh();

  // THE ROLL CALL, ON EVERY BOARD POLL, NOT JUST AT BOOT.
  //
  // `boot.js` asks `solo-who` once at load so a board starting after a window was
  // already popped out learns of it. That was the ONLY time it ever asked, and
  // that was the bug: a solo window re-claims on its own poll, but if that poll
  // stalls past `soloClaimFor` (15s) - which a reconnect/backoff through a hub
  // restart can cause - the board's claim expires and nothing ever refreshes it,
  // so `poppedOut` reads false and the pane takes the terminal back into a second
  // view. Asking again here makes it self-healing: any window still open answers
  // and re-stamps its claim every cycle, so a live card stays claimed. A window
  // that truly went away stops answering, so its claim still expires and its card
  // is still freed - the 15s heartbeat semantics are unchanged, only re-heard in
  // time. Passes are no longer on a ten second clock (see `want`), so the claim
  // between them is kept by the window's own `soloClaimBeat`, every 5s, and this
  // is the extra ask. It is a BroadcastChannel round trip between documents in
  // one browser, well under a frame (see the note in `boot.js`), with no request.
  if (soloBus) soloBus.postMessage({ type: "solo-who" });

  // A deadline that is running is re-read from the daemon rather than counted
  // down here. It costs one small request while a switch is temporary and
  // nothing at all the rest of the time, and it means the label cannot drift
  // away from the thing that actually decides. A read that failed is retried
  // here too, so the header does not sit on "unknown" until a reload. On a full
  // pass only (a resync, the stream reopening): a pass that repaints from a
  // task event is several a second on a busy board. A switch that changes is
  // said on the `settings` event anyway.
  if (want.health && (globalAutoStale || (globalAuto && globalAutoLeft > 0))) loadGlobalAuto();

  // A pass is done only when everything it started has settled. The jobs are
  // collected and awaited at the end so the single-flight guard cannot call a
  // pass finished while its fetches are still holding sockets.
  const jobs = [];

  // Taken now, put back if the read fails. See `want`.
  const take = { tasks: want.tasks, perms: want.perms, shares: want.shares, health: want.health };
  want.tasks = want.perms = want.shares = want.health = false;
  const cards = take.tasks
    ? loadCards(signal).catch(() => { want.tasks = true; })
    : Promise.resolve();
  // The local queue and every room's, read together because they are drawn
  // and counted as one. A failed local read keeps what was held.
  const permsJob = take.perms
    ? Promise.all([
        loadPerms(signal).catch(() => { want.perms = true; }),
        remoteRequests().catch(() => [])
      ]).then(([, remote]) => { remoteLocal = remote; })
    : Promise.resolve();

  // The alerts below read `lastTasks`, which the view sets as it paints, so
  // they wait for the paint. Without it a card that arrived was announced a
  // pass late, whenever the next one ran.
  let painted = Promise.resolve();
  if (isEditing()) {
    // Hold the repaint, but keep the counters, sounds and toasts live: those
    // are what tell you something arrived.
    heldUpdate = true;
    showHeld(true);
  } else if (isSelecting()) {
    // Withheld without saying so. See `isSelecting`.
    heldUpdate = true;
  } else {
    if (heldUpdate) { heldUpdate = false; showHeld(false); }
    painted = Promise.all([cards, permsJob])
      .then(() => repaintLists(signal, !take.tasks && !take.perms));
    jobs.push(painted);
  }

  // What is lent out, so a card can say so and the menu knows without asking.
  // Read at load, on a resync, and after this page starts or stops a share.
  if (take.shares) jobs.push(loadShares());

  // Waiting and permissions are worked out whichever view is open, because the
  // badges, the title, and the alert all have to work while you are looking at
  // something else. Waiting comes from the cards (see `cardsWaiting`), and the
  // permission queue is read only when a `permission` event or a resync said to.
  // THE NAG COVERS EVERY MACHINE, which is the whole point of the room list
  // being on this board at all. An agent frozen on a cloud instance is frozen
  // for the same reason and costs the same hour, and the alerting loop is what
  // makes a request impossible to miss. Left out, a remote request would sit
  // in the perms tab silently and only be found by somebody who went looking.
  //
  // Merged into `perms` rather than alerted on separately, so the badge, the
  // window title and the widening nag all count one queue and none of them can
  // learn about rooms later.
  jobs.push(Promise.all([cards, permsJob, painted]).then(() => {
    const waiting = cardsLoaded ? cardsWaiting() : null;
    const local = permsLoaded ? permsLocal : null;
    const remote = remoteLocal;
    // A local queue never read stays null, so the badge and the title keep
    // saying what they said rather than dropping to zero. Remote requests are
    // added to it only when there was something to add them to.
    const perms = local ? local.concat(remote) : (remote.length ? remote : null);
    if (waiting) {
      // Avoid a stack badge that duplicates the permissions count. Keep the count
      // for titles and alerts, but let the permission handler notify about blocked
      // tools so each event produces one alert. Newly started sessions stay visible
      // without ringing; fixtures-started reports startup failures separately.
      alerting.check("waiting", waiting.filter(t =>
        t.status !== "needs-permission" && !justStarted(t)), t => ({
        title: wasAsked(t)
          ? `${t.display_title} asked you something`
          // Named for what it is. A card stopped on another session still
          // rings, because it has stopped and a peer that never answers is a
          // session nobody is coming back to. Calling it "asked you" would
          // send you looking for a question that is not yours.
          : askedAPeer(t)
          ? `${t.display_title} is waiting on ${t.ask_peer}`
          : `${t.display_title} is ${statusLabel(t.status)}`,
        body: readyBecause(t)
      }));
    }
    // A CARD THAT WAS NOT THERE BEFORE, whoever made it.
    //
    // `atrium launch` from a shell, a script, an intake source or another
    // agent all put a card on the board with nothing on screen saying so, and
    // the board is usually behind something else when they do. The waiting
    // alert above cannot cover this: it filters `justStarted` out on purpose,
    // because a session coming up is not a session wanting you.
    //
    // NO SPECIAL CASE FOR ONES YOU STARTED YOURSELF, and none is needed. A
    // desktop notification is already suppressed while the board is in front,
    // so pressing launch on the board gets a toast and a card appearing while
    // you are in another window gets the notification. The distinction the
    // code would have had to guess at is one the browser already knows.
    //
    // From `lastTasks` rather than its own request: every view paints from the
    // card map (js/cards.js) and leaves its copy there.
    if (lastTasks && lastTasks.length) {
      alerting.check("arrived", lastTasks.filter(t => !over(t)), t => ({
        title: `${t.display_title} is on the board`,
        body: t.why || t.worktree || "a new card"
      }));
      // A RUNNING CARD WHOSE SCREEN SAYS IT FINISHED: the room saw its pty go
      // quiet on an idle prompt and no turn-end arrive. Worded as a guess. Keyed
      // on when it was flagged, so a card that wakes and stalls again rings
      // again. The room takes the flag down itself. See looksidle.go.
      alerting.check("looksidle", lastTasks
        .filter(t => t.activity && t.activity.looks_idle && !over(t) && !isWaiting(t))
        .map(t => Object.assign({}, t, {
          id: `${t.id}#idle#${t.activity.idle_at}`, task_id: t.id
        })), t => ({
        title: `${t.display_title} looks idle`,
        body: "looks idle (no turn-end received)"
      }));
      // AN AGENT-LAUNCHED CARD THAT IS STUCK: it stopped without reporting, or
      // one tool call has run too long. The room works out when, on the
      // operator's backoff (1m, 2m, 5m, 10m, 30m, 1h ... 24h), and steps
      // `escalation.count` each time. Keyed on the count, so each step rings
      // once and a card that moves starts over. See internal/daemon/a2a.go.
      //
      // Only when the gear's setting says to ring. "mark" leaves the card's
      // own mark (see `stuckMark`) as the whole signal.
      if (alerting.get().stuck === "alert") {
        alerting.check("stuck", lastTasks
          .filter(t => isStuck(t))
          .map(t => Object.assign({}, t, {
            id: `${t.id}#${t.escalation.source}#${t.escalation.count}`, task_id: t.id
          })), t => ({
          title: t.escalation.text,
          body: t.spawned_by ? `launched by ${t.spawned_by}` : (t.why || t.worktree || "")
        }));
      }
    }
    if (perms) {
      badge("c-perm", perms.length);
      // Named too. "permission needed" told you something needed answering and
      // not which of eight agents was frozen, which is the one fact that
      // decides whether it can wait.
      alerting.check("permission", perms, p => ({
        title: `${p.agent || "an agent"} needs permission`,
        body: `${p.tool}: ${(p.command || "").slice(0, 120)}`
      }));
      alerting.nag(perms);
    }
    // Retire any toast whose request or task has since been answered.
    if (waiting && perms) {
      reapToasts(new Set([...waiting.map(t => t.id), ...perms.map(p => p.id)]));
    }
    // Cards that are ready, plus requests waiting to be answered. The waiting
    // list holds both kinds, so counting it whole alongside the requests
    // reported a blocked agent twice.
    retitle(waiting && waiting.filter(t => t.status !== "needs-permission").length,
      perms && perms.length);
  }));

  // Health at load, on a resync (which the stream reopening is), and on a
  // `halted` event. A new build only arrives with a restart, which reopens the
  // stream, so nothing here needs a clock.
  if (take.health) jobs.push(api("/v1/health", { signal }).then(h => {
    checkBuild(h.build);
    applyHealth(h);
  }).catch(() => { want.health = true; }));

  // Wait for the whole fan-out. The catches above keep a single failed fetch
  // from rejecting the pass, so this settles once every socket this pass opened
  // has been returned, which is the moment the single-flight guard may run the
  // next one.
  await Promise.allSettled(jobs);
}

function connect() {
  // `/v1/events` on a plain daemon, and one of the hub's two spellings when a
  // hub is serving this. An EventSource sets no headers, so the room this
  // board is scoped to can only be said in the URL. See `js/rooms.js`.
  const es = new EventSource(
    typeof eventsURL === "function" ? eventsURL() : "/v1/events");
  const conn = document.getElementById("conn");
  const label = document.getElementById("conn-t");
  // A reconnect means the daemon went and came back, and everything held in
  // this tab was decided by a daemon that is no longer running. Settings are
  // re-read rather than kept: the header badge claiming to be approving
  // everything while the daemon asks is the worst possible way to be wrong,
  // because the operator stops watching the queue.
  es.onopen = () => {
    conn.classList.add("live");
    conn.classList.remove("down");
    label.textContent = "live";
    // On a hub the room counter carries this, so that two indicators cannot
    // disagree about whether the board is connected. See `paintRooms`.
    if (typeof paintRooms === "function") paintRooms();
    loadGlobalAuto();
    // Assume the daemon is coming up until it says otherwise.
    //
    // The tasks and the health poll are two requests that do not arrive in a
    // fixed order, so the first list of cards after a restart can be read
    // before the flag that explains it. This stream opening is the earliest
    // thing that happens when a daemon comes back, so the assumption is made
    // here and the next health poll either confirms it or clears it a few
    // seconds later.
    alerting.settling(true);
    // AND PULL THE CARDS NOW, not at the next poll. The stream reopening is the
    // first sign the daemon is back, and without this the board sat on whatever
    // it held when the hub went, for up to a poll interval, while the badge
    // already said live. A restart is exactly when the held state is most
    // likely stale, so this is where the wait was most visible. `refreshSoon`
    // is debounced and single-flight, so an event that also fires coalesces
    // with it rather than firing a second fetch.
    refreshSoon();
    // The audit pane heals on reconnect the same way the lists do: a stream that
    // dropped may have missed an `audit` delta, so re-fetch the feed now. Guarded
    // to an open pane by `onAuditEvent`, so a closed one pays nothing. See
    // js/audit.js.
    if (typeof onAuditEvent === "function") onAuditEvent();
    // Rows written while the stream was down were never announced, so an open
    // usage tab reads again.
    if (typeof onUsageStreamOpen === "function") onUsageStreamOpen();
    // A hub restart cover comes down on the stream coming back, and a pause is
    // re-read. See js/hubrestart.js.
    if (typeof onHubStreamOpen === "function") onHubStreamOpen();
    if (typeof onBoardStreamUp === "function") onBoardStreamUp();
  };
  es.onerror = () => {
    conn.classList.remove("live");
    conn.classList.add("down");
    label.textContent = "reconnecting";
    if (typeof paintRooms === "function") paintRooms();
    // The hub restart cover counts this as the old hub going. See
    // js/hubrestart.js.
    if (typeof onHubStreamDrop === "function") onHubStreamDrop();
    // And the down cover, when nobody said atrium would go. See js/down.js.
    if (typeof onBoardStreamDown === "function") onBoardStreamDown();
  };
  // Each event re-reads only what it is about. See `want`.
  es.addEventListener("task", onTaskEvent);
  es.addEventListener("task-removed", onTaskRemovedEvent);
  es.addEventListener("permission", permsSoon);
  es.addEventListener("halted", healthSoon);
  es.addEventListener("health", onHealthEvent);
  // A card that has gone takes its remembered placement with it. See
  // `rememberPlace`.
  //
  // Only the single-card removal carries an id. The sweep broadcasts this with
  // no payload, so placements for swept cards linger: each is one short string
  // under a key nothing will ask for again, which is worth less than a second
  // mechanism to chase them.
  es.addEventListener("task-removed", e => {
    try {
      const d = JSON.parse(e.data) || {};
      if (d.id) forgetPlace(d.id);
    } catch (err) {}
  });
  // settings.json changed under us, which is `atrium hook install` run in a
  // terminal. Without this the count only moves on the next poll, and only
  // while the runners tab happens to be open.
  // A share is one thing for the machine, so two tabs must not disagree about
  // whether it is up.
  // THE DAEMON SAYS IT IS GOING DOWN, before it takes anything down.
  //
  // This is the only reliable way a window learns the difference between a
  // session that ended and a restart. Both are a socket closing, `/v1/health`
  // answers yes during the wind-down because the listener outlives the
  // runners, and waiting-and-hoping is what stranded a popped-out window on a
  // dead terminal until F5.
  //
  // So the daemon announces it and every window arms itself. What arrives
  // after this is the restart. What arrives without it is the session ending.
  // The operational feed got a new line. Only re-fetched while the audit pane
  // is open, so a closed pane pays nothing. The pane also re-fetches on this
  // stream reopening via `refreshSoon`'s siblings and on being switched to. See
  // js/audit.js.
  es.addEventListener("audit", () => {
    if (typeof onAuditEvent === "function") onAuditEvent();
  });
  // The cache keep-alive stopped a card or suspended the room, or refreshed one.
  // The card's chip is redrawn from the next list, and a stop carries a toast.
  // See js/keepalive.js.
  // A usage row was written: one turn's spend. Added to the newest bucket of the
  // usage tab without a refetch, and only ever drawn when that tab is open. No
  // refreshSoon: a row moves no card. See js/usage-charts.js.
  es.addEventListener("usage", e => {
    if (typeof onUsageEvent === "function") onUsageEvent(e);
  });
  // A room's numbers for the rooms dashboard: one snapshot, drawn into its tile
  // in the open room menu. No refresh, since it moves no card. Nothing sends it
  // yet, and a guest stream never will. See js/rooms-dash.js.
  es.addEventListener("room-stats", e => {
    let d;
    try { d = JSON.parse(e.data); } catch (err) { return; }
    if (typeof onRoomStats === "function") onRoomStats(d);
  });
  es.addEventListener("keepalive", e => {
    if (typeof onKeepaliveEvent === "function") onKeepaliveEvent(e);
    tasksSoon();
  });
  es.addEventListener("going-down", e => {
    let why = "";
    try { why = (JSON.parse(e.data) || {}).why || ""; } catch (err) {}
    armRestart(why);
  });
  // THE HUB IS ABOUT TO RESTART, or has been asked to and is waiting. The
  // countdown, the pause and the cover. See js/hubrestart.js.
  es.addEventListener("hub-restart", e => {
    let d = {};
    try { d = JSON.parse(e.data) || {}; } catch (err) { return; }
    if (typeof onHubRestart === "function") onHubRestart(d);
  });
  // An item moved: handed to a room, started there, or refused. The queue is
  // only drawn on the runners pane, and `renderDispatch` is a single fetch, so
  // this redraws rather than trying to patch a row.
  es.addEventListener("dispatch", () => { renderDispatch(); });
  // A ROOM CAME OR WENT. Only the merged stream carries this, because a board
  // scoped to one room is talking to that room and not to the hub. The counter
  // in the header is the thing that has to move, and a poll would make a room
  // attaching take up to ten seconds to show.
  //
  // The event says WHICH rooms, and `loadHubRooms` is asked anyway rather than
  // trusting it: the chip draws a host and an uptime the event does not carry,
  // and one source of truth is worth one request.
  es.addEventListener("rooms", e => {
    let d = null;
    try { d = JSON.parse(e.data); } catch (err) {}
    // RE-SEED THE ALERT BASELINE BEFORE THE REFRESH RUNS, not after. A room
    // attaching or detaching churns the whole card set: the incoming room's
    // idle cards enter the aggregate as new ids, and the count crossing 1<->2
    // flips every id between `room~id` and bare. `loadHubRooms` also reseeds,
    // but only after an awaited `/_hub/rooms` fetch and never at all when the
    // 2s throttle drops the call, so `refreshSoon`'s `check` could win the race
    // and announce the incoming room's two-hour-idle cards as freshly ready.
    // This event only fires on a membership change, so the set is by definition
    // not stable and reseeding is exactly right. Guarded like loadHubRooms, in
    // case notify.js is absent.
    if (typeof alerting !== "undefined" && alerting.reseed) alerting.reseed();
    // Paints from the payload, and fetches only when it has no `attached` (an older hub).
    paintHubRoomsEvent(d);
    // The cards belong to the rooms that are gone or newly here.
    refreshSoon();
  });
  es.addEventListener("overlays", e => {
    let next;
    try { next = JSON.parse(e.data) || []; } catch (err) { return; }
    noticeSharesThatDied(overlays, next);
    overlays = next;
    paintOverlays();
  });
  // How far a share has got. Only ever painted into a dialog this window
  // opened, and only while that dialog is still waiting: the daemon broadcasts
  // to every window, and a share is one machine's business but it is this
  // operator's action.
  // The same narration for the board's own share, painted into the panel that
  // holds the button. Only while this window is the one waiting: `overlayBusy`
  // is set by the click, so a start from another tab does not turn this one's
  // button into a spinner that nothing here will ever clear.
  es.addEventListener("overlay-progress", e => {
    let d;
    try { d = JSON.parse(e.data) || {}; } catch (err) { return; }
    if (!d.kind || !(d.kind in overlayBusy)) return;
    // Both endings are left to the POST. It carries the new overlay state and
    // the error text, and clearing the busy line here would blank the panel a
    // beat before there is anything to put in its place.
    if (d.step === "done" || d.step === "failed") return;
    overlayBusy[d.kind] = d.text || "starting";
    paintOverlays();
  });
  es.addEventListener("share-progress", e => {
    let d;
    try { d = JSON.parse(e.data) || {}; } catch (err) { return; }
    // A share finished coming up, from this tab or another: the list of what
    // is lent is read again. Stopping one has no event yet, so that one is
    // left to the resync unless this tab stopped it.
    if (d.step === "done") { want.shares = true; passSoon(); }
    if (shareEnded || !shareFor || d.task_id !== shareFor) return;
    // `done` is ignored on purpose. The POST carries the object the dialog
    // needs, so finishing here would race it and win with less information.
    if (d.step === "done") return;
    if (d.step === "failed") { sharePaintSteps(shareAt, true); return; }
    const i = SHARE_STEPS.findIndex(s => s[0] === d.step);
    if (i < 0) return;
    shareAt = i;
    sharePaintSteps(i, false);
  });
  es.addEventListener("settings", e => {
    try {
      const s = JSON.parse(e.data);
      globalAuto = !!s.global_auto;
      globalAutoLeft = s.global_auto_seconds || 0;
      globalAutoRead = true;
      globalAutoStale = false;
      if (typeof ucHaveSetting === "function") ucHaveSetting(s);
    } catch (err) { return; }
    paintGlobalAuto();
  });
  // The fixtures that came up with the daemon, said once.
  //
  // Each of them produces a ready card, and a ready card is normally announced,
  // so booting used to be one notification per terminal for an event with no
  // content. Those are suppressed at the source now: a card ready BECAUSE IT
  // JUST STARTED never rings. What is left is the half the board cannot work
  // out for itself, since the absence of a card is not an event, and it is the
  // half worth hearing: the ones that were supposed to open and did not.
  //
  // Silent when everything worked. A summary that fires on success is the same
  // interruption in one message rather than six.
  es.addEventListener("fixtures-started", e => {
    let d;
    try { d = JSON.parse(e.data) || {}; } catch (err) { return; }
    const bad = d.failed || [];
    if (!bad.length) return;
    const ok = Number(d.started) || 0;
    const title = `${bad.length} fixture${bad.length === 1 ? "" : "s"} did not start`;
    // Named when there are few enough to name. A count alone sends you to the
    // page to find out which, and when it is one that is the whole answer.
    const body = (ok ? `${ok} started. ` : "") +
      (bad.length <= 3
        ? bad.map(f => f.label).join(", ")
        : bad.slice(0, 3).map(f => f.label).join(", ") + ` and ${bad.length - 3} more`);
    // Held with the rest while notifications are off (item 79). `play` exempts a
    // permission kind and the toast skips `notify`, so both are gated here, and
    // the `notify` between them records the one drawer entry.
    const held = notifyHeld("runners");
    if (!held) alerting.play("permission");
    alerting.notify(title, body, "runners", "", "fixtures", "", "");
    if (!held) toast(title, body + ". the rooms tab says why", "runners");
  });
  // Atrium is asking a registry whether a newer runner is published, and a
  // launch is waiting on the answer.
  //
  // The only place atrium holds up something you pressed on a request that
  // leaves the machine. Bounded at a few seconds, but a button that does
  // nothing for three of them reads as a hang, so it says so instead.
  es.addEventListener("runner-check", e => {
    let d;
    try { d = JSON.parse(e.data) || {}; } catch (err) { return; }
    const el = document.getElementById("conn-t");
    if (!el) return;
    if (d.checking) {
      el.dataset.was = el.dataset.was || el.textContent;
      el.textContent = `checking ${d.label || d.runner || "runner"}`;
      return;
    }
    el.textContent = el.dataset.was || "live";
    delete el.dataset.was;
  });
  // A theme was brought, edited or deleted. Every window reloads the table and
  // repaints its own terminal, because a palette is shared: two boards open on
  // one machine must not disagree about what `my-dracula` looks like, and a
  // popped-out terminal is a whole second document with its own copy.
  es.addEventListener("themes", async () => {
    await loadThemes();
    if (termTask) previewTheme(termTask.theme || "");
  });
  es.addEventListener("hooks", async () => {
    await loadHooks();
    if (document.getElementById("hooks").open) renderHooks();
    if (document.querySelector(".tab.on").dataset.view === "runners") renderRunners();
  });
}

