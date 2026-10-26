// ── how much room the session list gets ─────────────────────────────────────
//
// Three modes and a width, because the list is competing with the one thing
// on this page anybody came for. A terminal is measured in columns: every
// pixel the list takes is characters the session does not have, and eighty
// columns is not a preference.
//
// `full` is the list as it was. `mini` keeps the mark, eight characters and
// the left edge, which between them still say which session and whether it
// wants you. `off` gives the terminal the whole pane and leaves a strip to
// hover, and in that mode the list floats OVER the terminal rather than
// pushing it, so passing the pointer near the left edge never re-fits xterm.
//
// All of it is localStorage. It is a fact about this screen, and the same
// board on a laptop wants a different answer.
const TERMLIST_MIN = 150, TERMLIST_MAX = 520;
let termListMode = localStorage.getItem("atrium.termlist.mode") || "full";
let termListW = Number(localStorage.getItem("atrium.termlist.w")) || 260;
if (!["full", "mini", "off"].includes(termListMode)) termListMode = "full";

// A PHONE IS A DIFFERENT SCREEN FROM A DESKTOP, and a few of the terminal's
// settings are facts about the screen rather than about the work. A font that
// is comfortable on a 32in monitor is gigantic on a phone, and a split dragged
// on one screen is wrong on the other. Those keys are namespaced by device
// class so the two screens keep their own answers and neither writes over the
// other. The break is the same 900px the phone stylesheet uses.
function termNarrow() {
  return !!(window.matchMedia && window.matchMedia("(max-width: 900px)").matches);
}
function termDeviceKey(base) { return termNarrow() ? base + ".mobile" : base; }

// ── warming a card on hover ──────────────────────────────────────────────────
//
// A SWITCH STARTS WITH A FETCH of the card (`attachTask`), and for a card in a
// remote room the hub answers it with a lookup that crosses the link the first
// time and is cached for two minutes after. Resting the pointer on a row for a
// moment fetches it ahead of the click, so the click finds the answer ready and
// the hub's cache warm.
//
// ONLY THE FETCH. Opening the socket on hover would take one of the hub's few
// warm connections to that room for as long as the socket lived, and hovering
// down a list would drain them. So the socket still opens on the click.
//
// SHORT DWELL, CANCELLED ON LEAVE: sweeping the pointer down the list warms
// nothing. At most `PREWARM_MAX` fetches are in flight. What came back is
// kept for `PREWARM_FRESH_MS` and handed to `attachTask` once, so the card it
// opens on is no older than a click would have fetched. A mouse only: touch has
// no hover, and a phone gets nothing from this.
const PREWARM_DWELL_MS = 100;
const PREWARM_MAX = 2;
const PREWARM_FRESH_MS = 3000;
const prewarmed = new Map();
let prewarmTimer = 0, prewarmFlying = 0;

function prewarmCard(id) {
  if (!id || prewarmFlying >= PREWARM_MAX) return;
  const had = prewarmed.get(id);
  if (had && Date.now() - had.at < PREWARM_FRESH_MS) return;
  prewarmFlying++;
  const p = api(`/v1/tasks/${id}`);
  prewarmed.set(id, { at: Date.now(), p });
  const done = () => { prewarmFlying--; };
  // Only its own entry: a click can take this one and a second rest start another for the same id before this
  // one fails, and that newer one must survive.
  p.then(done, () => { done(); if ((prewarmed.get(id) || {}).p === p) prewarmed.delete(id); });
}

// The card a click is about to fetch, from the prewarm if it is fresh. Taken
// once: a second click fetches again.
function takePrewarmed(id) {
  const had = prewarmed.get(id);
  prewarmed.delete(id);
  return had && Date.now() - had.at < PREWARM_FRESH_MS ? had.p : null;
}

function prewarmRowOf(e) {
  if (e.pointerType !== "mouse" || !e.target || !e.target.closest) return null;
  const row = e.target.closest("#term-list .card.tab[data-id]");
  return row && !row.classList.contains("cold") ? row : null;
}
document.addEventListener("pointerover", e => {
  const row = prewarmRowOf(e);
  if (!row) return;
  clearTimeout(prewarmTimer);
  const id = row.dataset.id;
  if (typeof termTask !== "undefined" && termTask && termTask.id === id) return;
  prewarmTimer = setTimeout(() => prewarmCard(id), PREWARM_DWELL_MS);
});
document.addEventListener("pointerout", e => {
  const row = prewarmRowOf(e);
  if (row && !row.contains(e.relatedTarget)) clearTimeout(prewarmTimer);
});

// The ended group's open rooms (see termEndedHTML). Defined up here because prefLive below reads it at load.
const TERM_ENDED_KEY = "atrium.termended";
const CLOSED_TAG = "atrium:closed";
const termClosedGone = t => !t.supervised && (t.tags || []).includes(CLOSED_TAG);

// ── what the list shows: the drawer's chips ──────────────────────────────────
//
// A DOER IS AN AGENT-LAUNCHED SESSION, tagged `origin:agent` by the hub (DOER_TAG and isDoer live in js/cardrules.js),
// and a SUBAGENT is one tagged `atrium:subagent`. Those are what the list nests under their parent (see `termNest`).
//
// THE HIDE TOGGLES ARE GONE. `hide inactive` (agents, subagents) and `show stopped` read three different signals and
// said "hide" about each, and nobody could tell what a lit one was taking out. They are replaced by the three chips of
// the filter drawer (js/filter-drawer.js, docs/backlog/ui/u-011-interview.md):
//   - `running` and `ready` sessions are ALWAYS listed. They get no chip.
//   - `paused N`: cards with no agent running that CAN be resumed: stopped from the menu (`stop`) or parked after sitting
//     idle. Listed cold. Off by default, and a hidden one is hidden at every nesting level.
//   - `dead N`: pinned rows whose runner has gone, and the `ended` group of finished cards. Off by default.
//   - `subagents`: off, every parent row starts FOLDED with a count on its chevron and a child that needs you still
//     shows through the fold. On, parents start expanded. Off by default.
// The attached session is never taken out by a chip, so turning one off cannot pull the pane out from under you.
//
// STORED per device (see `termDeviceKey`) as the list of chips that are ON, so no stored value is the default.
const TERM_SHOW_KEY = "atrium.terms.show";
//   - `minions`: off, the throwaway sessions an item launched are left out of every list (`cardHidden`). On, they are
//     drawn like any session, with a `minion` chip. It is the debug toggle, and the board and stack read THIS key too,
//     so there is one switch and not three (`minionShown`, js/cardrules.js).
const TERM_CHIPS = ["paused", "dead", "subagents", "minions"];
// The keys the old controls kept, cleared on load (both device variants) so a stale `hide inactive` does not outlive
// the control that wrote it. `atrium.hidedoers` is older still.
function clearLegacyHideKey() {
  try {
    ["atrium.hidedoers", "atrium.hideagents", "atrium.hidesubagents", "atrium.termtray", "atrium.showstopped"]
      .forEach(k => { localStorage.removeItem(k); localStorage.removeItem(k + ".mobile"); });
  } catch (e) {}
}
clearLegacyHideKey();

function termShow() {
  try {
    const v = JSON.parse(localStorage.getItem(termDeviceKey(TERM_SHOW_KEY)) || "null");
    // `shelved` was this chip's old name, and a device may still hold it.
    if (Array.isArray(v)) return new Set(v.map(x => x === "shelved" ? "paused" : x).filter(x => TERM_CHIPS.includes(x)));
  } catch (e) {}
  return new Set();
}
function termSubsOn() { return termShow().has("subagents"); }
// The one switch for hidden minions, read by `cardHidden` through the getter cardrules.js keeps. Read at call time, so
// a click in this window or a change in another one is the next render's answer.
setMinionShown(() => termShow().has("minions"));
// The board and stack draw from the same key, so a change repaints the one on screen. The lists in the terminal views
// are repainted by `renderTermList` already.
function repaintMinionViews() {
  const tab = document.querySelector(".tab.on");
  const view = tab && tab.dataset.view;
  if (typeof repaintLists === "function" && view !== "terms" && view !== "chat") repaintLists();
}

function toggleTermShow(id) {
  if (!TERM_CHIPS.includes(id)) return;
  const on = termShow();
  if (on.has(id)) on.delete(id); else on.add(id);
  try {
    if (on.size) localStorage.setItem(termDeviceKey(TERM_SHOW_KEY), JSON.stringify([...on]));
    else localStorage.removeItem(termDeviceKey(TERM_SHOW_KEY));
    // A parent's fold means the opposite on each side of the subagents chip (see `termRow`), so the entries written
    // under the other meaning are dropped and every parent starts the way the chip says.
    if (id === "subagents") {
      localStorage.setItem("atrium.folded", JSON.stringify(foldedColumns().filter(k => !k.startsWith("kids:"))));
    }
  } catch (e) {}
  paintTermShow();
  renderTermList();
  if (id === "minions") repaintMinionViews();
}

// The chips, painted on change and once at load. The counts are written into them by `renderTermList`, so the button
// under the pointer is not replaced every poll.
function paintTermShow() {
  const el = document.getElementById("term-show");
  if (!el) return;
  const on = termShow();
  el.innerHTML =
    fdChip("paused", on.has("paused"), "toggleTermShow('paused')",
      "cards with no agent running that you can resume: ones you paused, and idle ones that were parked", "paused") +
    fdChip("dead", on.has("dead"), "toggleTermShow('dead')",
      "pinned rows whose session has exited, and the ended group. a click starts one again", "dead") +
    fdChip("subagents", on.has("subagents"), "toggleTermShow('subagents')",
      "off: a parent's subagents are folded under it, with a count. a count and a ! chip say what needs you. on: parents start open", "subagents") +
    fdChip("minions", on.has("minions"), "toggleTermShow('minions')",
      "off: the throwaway sessions an item launched are left out of every list, here, on the board and in the stack. one that needs you still shows. on: they are listed", "minions") +
    emptyGroupsChip("terms");
}
prefLive([TERM_SHOW_KEY + "*"], () => { paintTermShow(); renderTermList(); repaintMinionViews(); });

// WHICH KIND OF ROW A CARD IS, to the chips:
//   `gone`    a closed card with no session. Nothing to resume, listed only while it is the attached one.
//   `live`    a runner is up, or it was joined from the operator's own terminal. Always listed.
//   `paused`  stopped, or idle-parked: no process, and it CAN be resumed (a dead one cannot). Behind the paused chip.
//             Parking frees the process and the chip is what lists it.
//   `dead`    pinned, with its runner gone. Behind the dead chip.
//   ``        not a row of the list.
function termRowKind(t) {
  if (termClosedGone(t)) return "gone";
  if (t.status === "paused") return "paused";
  if (t.supervised || termJoined(t)) return "live";
  if (t.parked_at) return "paused";
  if (t.pinned) return "dead";
  return "";
}


// IS THIS SESSION WORKING RIGHT NOW is `workingNow`, in js/cardrules.js.

// IS THIS ROW COLD: no live connection. `supervised` is set while atrium holds a
// runner for the card and drops when that runner exits or disconnects. Idle and
// waiting sessions stay `supervised`, so they are not cold: a connected agent
// sitting at a prompt is not dead.
//
// ONE PREDICATE FOR THE GREY AND THE HIDE. `termRow` greys a row on this, and the
// agents toggle hides on this, so a row drawn inactive is exactly a row the
// toggle takes out. Two copies of the test let them drift: the toggle hid on
// "not supervised" while exempting every pinned row, and since only a pinned row
// can be cold in the strip, it hid nothing the operator could see was grey.
//
// COLD MEANS GONE, NOT "ATRIUM DOES NOT OWN THE TERMINAL". A session joined from
// the operator's own terminal (`atrium join`) is live and asking for input with
// `supervised` false, so it counts as live by its status and stays out of the hide.
const TERM_LIVE_STATUS = ["running", "needs-input", "needs-permission"];
function termCold(t) {
  if (t && t.supervised) return false;
  // A parked card keeps its status but its process is gone.
  if (t && t.parked_at) return true;
  return !(t && TERM_LIVE_STATUS.includes(t.status)) || runnerGone(t);
}
// Live but not attachable: joined from a terminal atrium does not hold.
function termJoined(t) { return !!t && !t.supervised && !termCold(t); }

// A click on a joined row asks what you want, because attaching is the one thing it cannot do. Only what the room
// already does is live: its card and a message to it. Ending it and taking it over both need the terminal atrium
// does not hold (exit types into it, kill stops a runner atrium started), so they are drawn grey, with the reason.
async function joinedClick(id) {
  let t;
  try { t = await api(`/v1/tasks/${id}`); }
  catch (e) { toast("could not open it", e.message); return; }
  const pick = await askUser({
    title: t.display_title,
    body: "This session runs in a terminal atrium does not hold, so it cannot be attached.<br><br>" +
      "<b>end it</b> and <b>take it over</b> are off: ending needs that terminal, and resuming here while it is " +
      "still running there would run the conversation twice.",
    buttons: [
      { label: "cancel", value: null },
      { label: "end it", value: "end", disabled: "atrium holds no terminal to ask it to exit" },
      { label: "take it over", value: "take", disabled: "it cannot be ended from here, so it cannot be resumed here yet" },
      { label: "details", value: "details" },
      { label: "message it", value: "say", style: "go" }
    ]
  });
  if (pick === "details") { openTask(id); return; }
  if (pick !== "say") return;
  const text = await askUser({ title: "message " + t.display_title, input: true,
    body: "It is queued and reaches the session on its next tool call. " +
      "A session sitting idle makes none, so it waits until someone types into that other terminal.",
    buttons: [{ label: "cancel", value: null }, { label: "send", value: true, style: "go" }] });
  if (!text || !String(text).trim()) return;
  try {
    await api(`/v1/tasks/${id}/message`, { method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ text: String(text).trim(), when: "done" }) });
  } catch (e) { toast("not sent", e.message); return; }
  toast("sent", t.display_title);
}

// Whether a chip being off is what would take this row out, and only the attached one is spared it: the pane is never
// pulled out from under the operator. Such a row is drawn as it is, with a chip that says why it is still here.
function termKept(t, show) {
  if (!termTask || t.id !== termTask.id) return false;
  const k = termRowKind(t);
  return k === "gone" || (k === "paused" && !show.has("paused")) || (k === "dead" && !show.has("dead"));
}

// Whether the list holds this card at all, given the chips. Live rows always, the rest by their chip.
function termListed(t, show) {
  if (cardHidden(t)) return false;
  const k = termRowKind(t);
  return k === "live" || (k !== "" && (termKept(t, show) || (k === "paused" && show.has("paused")) ||
    (k === "dead" && show.has("dead"))));
}

// ── the sort, and the gear's copy of it ──────────────────────────────────────
//
// THE SORT AND GROUP LIVE IN TWO PLACES, ONE STATE. The filter drawer's rows (`#term-sort`, `#term-drawer-group`) and
// the gear's `terminal list` section (index.html) draw the same markup (`termSortHTML`, `paintGroupSegs`) bound to the
// same handlers, so a change made in either place is what the other shows. The settings keys are `atrium.termSort` and
// the grouping prefs.

// The sort as a pair rather than a chip that names its own state.
function setTermSort(byActivity) {
  if (sortByActivity !== !!byActivity || termSortStarted) setTermSortMode(byActivity ? "activity" : "name");
}

function termSortHTML() {
  return `<button class="${sortByActivity || termSortStarted ? "" : "on"}" onclick="setTermSort(false)"
        data-tip="alphabetical by name">name</button>
      <button class="${sortByActivity ? "on" : ""}" onclick="setTermSort(true)"
        data-tip="working sessions first, then anything waiting on you, then newest activity"
        >activity</button>
      <button class="${termSortStarted ? "on" : ""}" onclick="setTermSortMode('started')"
        data-tip="newest card first, by when it was started">started</button>`;
}

function paintTermGear() {
  const sort = document.getElementById("gear-term-sort");
  if (sort) setHTML(sort, termSortHTML());
  const drawer = document.getElementById("term-sort");
  if (drawer) setHTML(drawer, termSortHTML());
  if (typeof paintGroupSegs === "function") paintGroupSegs();
}
paintTermShow();
paintTermGear();

// ── the phone dropdown ───────────────────────────────────────────────────────
//
// On a phone the list is a switcher competing with the one thing you came for,
// and its header alone (a sort chip, a row of grouping buttons, a pinned
// heading) pushed the sessions off the bottom before a single card was drawn.
// So with a terminal attached the list COLLAPSES to one row naming the attached
// session, and a tap opens the full list back up. The sort and grouping
// controls are in the gear (see `paintTermGear`).
//
// Only on a phone, and only with a terminal attached. A desktop has the width
// for the list beside the terminal, and a phone with nothing attached is
// already showing the list as its whole view, so there is nothing to collapse.
// The trigger is hidden by CSS in both of those.
//
// `open` is not persisted: attaching collapses it (see `openTerm`) and it opens
// on a tap, so a stored value would only ever fight one of those.
let termListOpen = false;
// The `filters` button this replaced kept its own key. 
try { localStorage.removeItem("atrium.termfilters"); } catch (e) {}

// The class the phone stylesheet reads, set from state rather than toggled in
// place so a poll's re-render keeps whatever the taps left. Called from
// `applyTermList`, which runs on every render.
function applyTermDrop(lay) {
  lay.classList.toggle("tl-open", termListOpen);
}

function setTermListOpen(open) {
  termListOpen = !!open;
  const lay = document.getElementById("term-layout");
  if (lay) lay.classList.toggle("tl-open", termListOpen);
  // Opening or closing trades height with the terminal below, and xterm only
  // knows its size because something measured it. A frame later, after the
  // layout it is measuring.
  requestAnimationFrame(onTermResize);
}
function toggleTermListOpen() { setTermListOpen(!termListOpen); }

// u-023: the card picker of the phone tray (#t-pick in the terminal bar). Same list as the "atrium" row.
function termPickTap() {
  // A card's own window has no list to open, so the picker there is the switcher.
  if (typeof termOnly === "function" && termOnly() && typeof openSwitcher === "function") { openSwitcher(); return; }
  toggleTermListOpen();
  const b = document.getElementById("t-pick");
  if (b) b.setAttribute("aria-expanded", termListOpen ? "true" : "false");
}

// The collapsed trigger: the attached session's name and a caret that opens the
// list. Hidden by CSS everywhere except a phone with a terminal attached, so it
// is always rendered and never in the way.
function termDropHTML() {
  const t = termTask;
  const label = t
    ? (String(t.display_title || "").trim() || terminalLabel(t) || "this session")
    : "choose a session";
  return `<div class="termdrop">
      <button class="termdrop-cur" onclick="toggleTermListOpen()"
        data-tip="switch session"><span class="tname">${esc(label)}</span
        ><span class="caret">&#9662;</span></button>
    </div>`;
}

function applyTermList() {
  const lay = document.getElementById("term-layout");
  if (!lay) return;
  applyTermDrop(lay);
  // `mini` and `off` are the desktop's answer to a list competing with the
  // terminal beside it: shrink it, or hide it behind a rail. A phone does not
  // lay them side by side, so neither mode means anything there, and the grip
  // and rail that reach them are hidden at this width. The stored mode is kept
  // (the same browser on a wide screen still honours it) but ignored on a
  // phone, where the list is the whole terminals view and must stay full.
  const narrow = termNarrow();
  lay.classList.toggle("tl-mini", !narrow && termListMode === "mini");
  lay.classList.toggle("tl-off", !narrow && termListMode === "off");
  lay.style.setProperty("--termw", clampTermW(termListW) + "px");
  const cl = document.getElementById("chat-layout");
  if (cl) cl.style.setProperty("--termw", clampTermW(termListW) + "px");
}

// Puts the bridge across the gutter, level with the attached card.
//
// Measured rather than calculated. The gap is a CSS variable, the handle is
// nine pixels, the list is a width somebody dragged and the card is wherever
// the scroll left it: four numbers that all change independently, and the only
// one that is ever right is the one the browser just laid out.
//
// Cheap enough to call on every scroll: two `getBoundingClientRect` calls and
// four style writes, no layout of its own.
//
// ONE BRIDGE PER COPY OF THE ATTACHED ROW. A pinned row filed into a group is
// drawn twice, in the bucket and in the group, and both are the row you are
// on. With one bridge, the first copy got it and the second was a tab that
// stopped short of the pane. `#tab-bridge` is the first, and the rest are
// copies of it made here and kept beside it.
function bridgeSides() {
  const chat = typeof chatLent !== "undefined" && chatLent;
  return chat
    ? { chat, lay: document.getElementById("chat-layout"), pane: document.getElementById("chat-pane"), live: !!chatId }
    : { chat, lay: document.getElementById("term-layout"), pane: document.getElementById("term-pane"), live: !!term };
}

function placeTabBridge() {
  const first = document.getElementById("tab-bridge");
  if (!first) return;
  const side = bridgeSides();
  // A shut group holding the attached row stands in for it: its heading is the tab.
  // So does a collapsed parent row holding it as a hidden child.
  const cards = [...document.querySelectorAll("#term-list .card.on, #term-list .tgroup.carries, #term-list .card.carries")];
  const bridges = [first, ...first.parentNode.querySelectorAll(".tabbridge.extra")];
  while (bridges.length < cards.length) {
    const b = first.cloneNode(false);
    b.removeAttribute("id");
    b.classList.add("extra");
    bridges[bridges.length - 1].after(b);
    bridges.push(b);
  }
  // The pane wears the attached row's frame, so the row, the bridge and the
  // pane are one outline of one width. Both copies are framed alike, so the
  // first speaks for them. Set before the bridges, which reach across it.
  const pane = side.pane;
  const fw = cards.length && side.live ? getComputedStyle(cards[0]).getPropertyValue("--framew").trim() : "";
  if (pane) {
    if (fw) {
      pane.style.setProperty("--framew", fw);
      pane.style.setProperty("--framec", getComputedStyle(cards[0]).borderTopColor);
    } else {
      pane.style.removeProperty("--framew");
      pane.style.removeProperty("--framec");
    }
  }
  bridges.forEach((b, i) => {
    if (i < cards.length) placeOneBridge(b, cards[i], side);
    else b.hidden = true;
  });
}

const FUNNEL_FLARE = 7;

function placeOneBridge(bridge, card, side) {
  side = side || bridgeSides();
  const lay = side.lay;
  if (lay) lay.classList.add("bridged");
  // Nothing attached, or the list is not beside the terminal to begin with.
  // In `off` the list floats OVER the pane, so there is no gutter to cross.
  if (!lay || !card || (termListMode === "off" && !side.chat) || !side.live) { bridge.hidden = true; return; }

  const l = lay.getBoundingClientRect();
  const c = card.getBoundingClientRect();
  const list = document.getElementById("term-list");
  // Clipped by the box the rows scroll in, not the whole list, so a card
  // scrolled up to the tray's edge loses its bridge there.
  const lr = (list.querySelector(".termscroll") || list).getBoundingClientRect();

  // NOTHING TO MEASURE IS NOT THE SAME AS NOTHING TO DRAW.
  //
  // Every rectangle on a `display: none` subtree is zero, and read as
  // geometry that says the card is scrolled out of sight and the gutter has
  // no width, so this hid the bridge for a reason that had nothing to do with
  // the bridge. A window resized while you are on another view is enough to
  // reach it, and coming back does not undo it.
  //
  // So a measurement taken on a hidden layout is DISCARDED rather than acted
  // on. The state is left exactly as it was, and the next placement on a
  // visible layout is the one that decides.
  if (!l.width && !l.height) return;

  // Scrolled out of sight behind its own header, or past the bottom. Drawing
  // it anyway would leave a bar of colour floating against the terminal with
  // nothing on the other end of it.
  if (c.bottom <= lr.top + 2 || c.top >= lr.bottom - 2) { bridge.hidden = true; return; }

  const pane = side.pane;
  const p = pane ? pane.getBoundingClientRect() : { left: lr.right };
  // From just inside the card's right edge to the inside of the pane's left
  // border, so it overlaps both and no seam shows at either end.
  const pw = pane ? parseFloat(getComputedStyle(pane).borderLeftWidth) || 1 : 1;
  const from = c.right - 2, to = p.left + pw;
  if (to <= from) { bridge.hidden = true; return; }

  bridge.hidden = false;
  bridge.style.left = (from - l.left) + "px";
  bridge.style.width = (to - from) + "px";
  // A shut group's bridge is a funnel: the heading's height at the heading, FLARE taller each side where it meets the pane.
  const funnel = card.classList.contains("tgroup") || card.classList.contains("carries");
  const up = funnel ? Math.max(0, Math.min(FUNNEL_FLARE, c.top - lr.top)) : 0;
  const down = funnel ? Math.max(0, Math.min(FUNNEL_FLARE, lr.bottom - c.bottom)) : 0;
  const top = Math.max(c.top - up, lr.top), bottom = Math.min(c.bottom + down, lr.bottom);
  bridge.style.top = (top - l.top) + "px";
  bridge.style.height = (bottom - top) + "px";
  bridge.classList.toggle("funnel", funnel);
  if (funnel) {
    const w = to - from, h = bottom - top, bw = parseFloat(getComputedStyle(card).getPropertyValue("--framew")) || 1;
    const x = w * 0.4, y0 = Math.max(0, up), y1 = h - Math.max(0, down), k = bw / 2;
    bridge.innerHTML = `<svg width="${w}" height="${h}" viewBox="0 0 ${w} ${h}" style="overflow:visible"><polygon style="fill:var(--tabbg)" points="0,${y0} ${x},${y0} ${w},0 ${w},${h} ${x},${y1} 0,${y1}"/><path style="fill:none;stroke:var(--tabc);stroke-width:var(--tabw)" d="M0,${y0 + k} L${x},${y0 + k} L${w},${k} M0,${y1 - k} L${x},${y1 - k} L${w},${h - k}"/></svg>`;
  } else if (bridge.firstChild) bridge.textContent = "";
  // The colours are the card's, whatever theme it is wearing.
  const cs = getComputedStyle(card);
  // A skin's card is a gradient, which has no background colour to copy. Its row says what to use.
  const flat = /rgba\(.*,\s*0\)$|transparent/.test(cs.backgroundColor);
  bridge.style.setProperty("--tabbg", flat ? cs.getPropertyValue("--tabbg").trim() || cs.backgroundColor : cs.backgroundColor);
  bridge.style.setProperty("--tabc", cs.borderTopColor);
  // A worn row's frame is its border plus an inset line, and it says how
  // thick in `--framew`. The bridge's edge is that thick too, or the frame
  // steps down where it crosses.
  bridge.style.setProperty("--tabw", cs.getPropertyValue("--framew").trim() || cs.borderTopWidth);
}

function clampTermW(px) {
  return Math.max(TERMLIST_MIN, Math.min(TERMLIST_MAX, Math.round(px || 0)));
}

// TWO BUTTONS, NOT ONE CYCLE.
//
// It was one button that went full to mini to off and round again, which is
// three states behind a control that only ever appeared to make things
// smaller. Halfway along, in `mini`, there was no way to go back: the button
// only shrank, and the way out was to shrink it AGAIN to `off` and find the
// rail. Nobody works that out. They collapse it once, lose the list, and stop
// using the mode.
//
// A pair says what it does, both directions are always reachable, and the end
// of the range is a disabled button rather than a wrap-around that undoes what
// you just did.
const TERM_LIST_MODES = ["off", "mini", "full"];

function setTermListMode(mode) {
  if (!TERM_LIST_MODES.includes(mode) || mode === termListMode) return;
  termListMode = mode;
  try { localStorage.setItem("atrium.termlist.mode", mode); } catch (e) {}
  applyTermList();
  renderTermList();
  // The pane just changed width, and xterm only knows its size because
  // something measured it. Deferred a frame so the measurement happens after
  // the layout it is measuring.
  requestAnimationFrame(onTermResize);
}

// Another window of this browser changed how the list looks: its mode and width, the folded groups, the ended group.
// Read again and drawn the way the control in that window drew it. The chips have their own (see `paintTermShow`).
prefLive(["atrium.termlist.*", "atrium.termfolded", TERM_ENDED_KEY + "*"], () => {
  const mode = localStorage.getItem("atrium.termlist.mode") || "full";
  termListMode = TERM_LIST_MODES.includes(mode) ? mode : "full";
  termListW = Number(localStorage.getItem("atrium.termlist.w")) || 260;
  applyTermList();
  paintTermGear();
  renderTermList();
  requestAnimationFrame(onTermResize);
});

function stepTermList(by) {
  const at = TERM_LIST_MODES.indexOf(termListMode);
  const to = TERM_LIST_MODES[at + by];
  if (to) setTermListMode(to);
}

// Called by the rail, which is the way back from `off`.
function showTermList() { setTermListMode("mini"); }

// Only the directions that go anywhere.
//
// A disabled button at each end says "you cannot", which is a sentence nobody
// needed: at full width there is obviously no wider, and the greyed control
// is just something else in a header that has three things in it. What is
// missing IS the answer.
// Three diagonal ticks hugging the lower left corner, the usual resize grip. The widen button mirrors it.
const TL_GRIP = `<svg viewBox="0 0 12 12" width="18" height="18" aria-hidden="true"><path
  d="M1 4L8 11M1 7.5L4.5 11M1 11L1.01 11" fill="none" stroke="currentColor" stroke-width="1.6"
  stroke-linecap="round"/></svg>`;
function termListButtons() {
  const at = TERM_LIST_MODES.indexOf(termListMode);
  let out = "";
  if (at > 0) {
    out += `<button class="termsort tlcycle" onclick="stepTermList(-1)"
      data-tip="${termListMode === "full"
        ? "shrink it to just the names"
        : "hide it, and hover the edge to peek"}" aria-label="narrow the list">${TL_GRIP}</button>`;
  }
  if (at < TERM_LIST_MODES.length - 1) {
    out += `<button class="termsort tlcycle tlwider" onclick="stepTermList(1)"
      data-tip="${termListMode === "off"
        ? "put the list back"
        : "show the whole name"}" aria-label="widen the list">${TL_GRIP}</button>`;
  }
  return out;
}

// ── dragging the width ──────────────────────────────────────────────────────
//
// Pointer events with capture rather than mousemove on the document. Capture
// means the drag keeps following the pointer over the terminal, over an
// iframe, and past the edge of the window, which a listener on the element
// alone does not.
let gripFrom = 0, gripWas = 0;
function startGrip(e) {
  // The grip drags the list's WIDTH beside the terminal. This is a desktop
  // control: a phone stacks the terminal on its own and never ties its size to
  // the list, so the grip is hidden there and this never runs. See the phone
  // dropdown, where the list floats OVER the terminal instead of resizing it.
  if (termListMode === "off") return;
  e.preventDefault();
  // THE ELEMENT IS HELD IN A LOCAL, not read off the event inside the
  // closures. `currentTarget` is only set while an event is being dispatched
  // and is null after that, so `done` threw on its first line: nothing was
  // removed, `gripping` was never taken off the body, and the pointermove
  // handler stayed bound to the grip for the life of the page. Every later
  // pass of the pointer near the handle then resized from a `gripFrom` set
  // during a drag that ended long ago, which is why the list grew a little
  // wider every time you moved across the cards.
  const el = e.currentTarget;
  gripFrom = e.clientX;
  gripWas = clampTermW(termListW);
  el.setPointerCapture(e.pointerId);
  el.classList.add("dragging");
  document.body.classList.add("gripping");
  const move = ev => setTermW(gripWas + (ev.clientX - gripFrom));
  const done = () => {
    el.classList.remove("dragging");
    document.body.classList.remove("gripping");
    el.removeEventListener("pointermove", move);
    el.removeEventListener("pointerup", done);
    el.removeEventListener("pointercancel", done);
    saveTermW();
  };
  el.addEventListener("pointermove", move);
  el.addEventListener("pointerup", done);
  el.addEventListener("pointercancel", done);
}

function gripKey(e) {
  const step = e.shiftKey ? 40 : 10;
  if (e.key === "ArrowLeft") { e.preventDefault(); setTermW(termListW - step); saveTermW(); }
  else if (e.key === "ArrowRight") { e.preventDefault(); setTermW(termListW + step); saveTermW(); }
  else return;
}

// The width is set on the element as it moves and written down only when the
// drag ends. A localStorage write per pointermove is a write per frame.
function setTermW(px) {
  termListW = clampTermW(px);
  const lay = document.getElementById("term-layout");
  if (lay) lay.style.setProperty("--termw", termListW + "px");
  // The chat tab borrows the list and the grip, so it carries the same width.
  const cl = document.getElementById("chat-layout");
  if (cl) cl.style.setProperty("--termw", termListW + "px");
  // Live, so the terminal reflows under the handle rather than jumping when
  // it is let go. xterm's fit is cheap next to the repaint it triggers, and
  // this only runs while a pointer is down.
  onTermResize();
}

function saveTermW() {
  try { localStorage.setItem("atrium.termlist.w", String(termListW)); } catch (e) {}
}

// The name at eight characters, for the collapsed list.
//
// Built from the repo and the branch rather than by cutting the full address,
// which starts with the host and the org: every entry would be truncated in
// the part they all share. `github/dovholuknf/atrium:main` becomes `atrium:m`,
// which is not the whole story and is not meant to be. The full name is the
// title attribute, and the mode exists for the case where you already know
// what is in your own list.
// THE BRANCH IS NOT THE NAME. It was in here as a fallback, and it won every
// time, because a card carries a branch far more often than it carries a repo:
// every session was labelled `main`, which is the one word that cannot tell two
// of them apart. What identifies a session in a list of four is the DIRECTORY.
//
// So: the repo if the card knows one, the last segment of the working
// directory if not, and only then whatever the label ends with. The branch is
// in the tooltip, where a tie-break belongs.
function shortLabel(task) {
  if (!task) return "";
  const leaf = s => {
    const segs = String(s || "").replace(/\\/g, "/").split("/").filter(Boolean);
    return segs.length ? segs[segs.length - 1] : "";
  };
  // `atrium:main` is one segment of a label, and the half before the colon is
  // the half worth keeping.
  const base = String(task.repo || "").trim()
    || leaf(task.worktree)
    || leaf(terminalLabel(task) || task.display_title);
  return base.split(":")[0].slice(0, 10);
}

// The list of things worth attaching to, so switching is a click.
//
// RIGHT CLICK A SESSION IN THE LIST (or Shift+right click in its terminal). docs/interviews/INTERVIEW-card-menu-2026-10-09.md
// settled this menu with clint, top to bottom:
//
//   restart, resume (a stopped card), new context, add to group, remove from group,
//   ---
//   settings
//   ---
//   stop, terminate and clean up
//
// Every item keeps its (?) bubble and none carries a grey sub-line. Nothing asks "are you sure?" but terminate and
// clean up, and only when the folder holds work that exists nowhere else (see `cleanUpCard` in js/sharing.js).
// What is NOT here and where it went: rename and alias are a click on the terminal's title (js/alias.js), the context
// limit is in settings (the cog), and "what did it do?", "open a shell here", attach, "which repo" and "new agent
// here" are gone. The board's menu is a different interview and is untouched.
async function termMenu(e, id) {
  e.preventDefault();
  e.stopPropagation();
  const t = await api(`/v1/tasks/${id}`);
  // The runners, for the resume check: whether a card's runner has resume arguments.
  await loadHarnesses();
  showMenu(e, termMenuItems(id, t));
}

// The items, apart from the drawing so a test and the phone sheet read the same list.
function termMenuItems(id, t) {
  // A finished card has nothing to restart or stop, and offering them produced a dialog that failed.
  const over = ["done", "dead", "paused"].includes(t.status);
  const stopped = t.status === "paused";
  return [
    // The one used nearly every time. Only where atrium owns the terminal, since it types the exit keys, or on a
    // parked card, which restarts as a resume.
    !over && cardState(t) !== "off" && (t.supervised || (t.parked_at && t.resume_id)) ? {
      label: "restart",
      help: "Types the runner's exit keys (claude: ctrl-d twice), waits for it to leave, then starts it again " +
        "on this card, resuming the same conversation with the same model, effort, arguments and settings. " +
        "A runner that will not leave is left running and the card says so. For a fresh conversation use " +
        "new context.",
      act: () => restartCard(id)
    } : null,
    // A stopped card. `stop` pauses it, and paused is the status with no runner whose request block is still up, so
    // resuming it lifts the block and starts the runner. Any other card with no runner and a conversation resumes by
    // the same two entries the board has.
    stopped ? {
      label: "resume",
      help: "Starts this card's runner again and picks the conversation back up where it stopped.",
      limited: !!cannotResume(t),
      tip: cannotResume(t) || "",
      act: () => cannotResume(t) ? toast("cannot resume", cannotResume(t)) : resumePausedCard(id)
    } : t.parked_at ? {
      label: "resume",
      help: "This card was parked after sitting idle, so nothing is running. Starting it again resumes the same " +
        "conversation, and its prompt cache is probably cold, so the first turn runs on a full context.",
      act: () => resumeParked(id)
    } : t.worktree && canResume(t) && !cannotResume(t) ? {
      label: "resume",
      help: "Starts a runner in this card's directory and picks the conversation back up where it stopped. It " +
        "opens where this card was last open.",
      sub: resumeEntries(id, t)
    } : null,
    // Absent while a cycle runs, since a second one would type over the first. A failed cycle may be run again.
    t.supervised && !over && !(t.new_context && t.new_context.step !== "failed") ? {
      label: "new context",
      help: "Asks the session to commit or stash and write everything relevant to its own HANDOFF.<name>.md, " +
        "waits for that turn to end, clears the context, and then tells it to read the file and continue. " +
        "Ctrl+Alt+N in its terminal does the same.",
      act: () => newContext(id)
    } : null,
    typeof groupItem === "function" ? groupItem(id, t, true) : null,
    typeof outOfGroupItem === "function" ? outOfGroupItem(id, t, true) : null,
    { sep: true },
    // Everything about what this card is, the context limit included (settings-spine.js).
    { label: "settings", act: () => openTask(id) },
    { sep: true },
    // What the board calls paused. It stops the runner and the card keeps its place, hidden from this list unless
    // the paused filter is on. Resume starts it again.
    t.supervised && !over ? {
      label: "stop",
      help: "Stops the agent so it can be resumed later. The card stays on the board, and is left out of this " +
        "list unless the paused filter is on.",
      act: () => oneAtATime("pause:" + id, () => patchTask(id, { status: "paused" }).then(refresh))
    } : null,
    // A COLD ROW IS ONLY HERE BECAUSE IT IS PINNED, so dismissing it is unpinning it. Without this a dead terminal's
    // menu holds nothing that removes it from the list, since stop needs a runner.
    !t.supervised && t.pinned ? {
      label: "dismiss",
      help: "Takes this exited terminal out of the list by unpinning it. The card and its history stay on the board.",
      act: () => togglePin(id, false)
    } : null,
    {
      label: "terminate and clean up", danger: true,
      help: "Stops the agent and removes the folder and branch it was working in, here and on the hub, when atrium " +
        "created them. A folder it did not create stays, and it says so first. It asks about changes that exist " +
        "nowhere else.",
      act: () => cleanUpCard(id, t),
      sub: [{
        label: "and don't clean up",
        help: "Stops the session and ends the card. The folder, the branch and any hub branch are left untouched.",
        act: () => endCardKeeping(id, t)
      }]
    }
  ].filter(Boolean);
}

// Attach to a card and land on its shell, from anywhere that offers it.
//
// Two steps rather than one, and in this order: attach first so the pane is on
// the right card, then switch. Asking for the shell before attaching would
// leave the toggle describing a card the pane is not showing.
// Ends a card's shell from outside it.
//
// `exit` in the shell does the same thing and is what you reach for when you
// are looking at it. This is for a shell open on a card you are not: it holds a
// process and its working directory, and the idle sweep is half an hour away.
//
// If the pane is showing that shell, it falls back to the runner on its own:
// the socket closes and `connectTerm` handles it, the same as `exit`.
async function closeShellFor(id) {
  try {
    await api(`/v1/tasks/${id}/shell`, { method: "DELETE" });
  } catch (e) {
    toast("could not close it", e.message);
    return;
  }
  toast("shell closed", "the agent's terminal is untouched");
  refresh();
}

async function openShellFor(id) {
  await attachTask(id);
  // `attachTask` raises a popped-out window instead of attaching here, and in
  // that case this pane is showing something else entirely. Switching would
  // move the wrong terminal.
  if (!termTask || termTask.id !== id) return;
  await showTerminal("shell");
}

// The name is an override, so it survives the runner reconnecting and
// reporting its directory again.
async function renameTask(id, currentName) {
  const name = await askText("rename this session",
    "Shown on the card and in the terminal list. Kept when the runner " +
    "reconnects and reports its own name again.", currentName || "");
  if (name === null) return;
  await patchTask(id, { overrides: { title: name.trim() } });
  refresh();
}

// THE CARD LAYER OF THE CONTEXT LIMIT, an override on the card (`context_limit_k`, in thousands of tokens). Empty
// clears it, and the hub's limit for its harness applies again. The daemon checks the range.
async function setTaskContextLimit(id, t) {
  const own = t.overrides && t.overrides.context_limit_k || "";
  const v = await askText("context limit for this card",
    `In thousands of tokens, from 10 to ${(t.context_size && t.context_size.ceiling_k) || 2000}. Now ${limitFrom(t)}. Past it atrium cycles the card's context. ` +
    "This is the card's own setting and wins over the hub's limit for its harness (settings). Leave it empty " +
    "to go back to that.", own);
  if (v === null) return;
  await patchTask(id, { overrides: { context_limit_k: v.trim() } });
  refresh();
}

// WHICH REPOSITORY THIS IS, when the answer read off the path is wrong.
//
// The daemon resolves the repo in three tiers: this override, then whatever the
// launcher recorded, then a guess read out of the worktree. The guess handles
// the ordinary case and cannot handle a checkout kept somewhere that does not
// look like `<forge>/<org>/<repo>`, so this is the way out of that.
//
// It decides WHERE THE ROW SITS, not what it is called. The strip's headings
// are built by anchoring on the repo inside the worktree path, so a card with
// the wrong one lands under the wrong heading, or under three headings named
// after directories below the checkout. Renaming is the other entry and answers
// a different question.
//
// Empty clears it, which is what `SetOverrides` does with an empty value, and
// then the guess applies again. That is the undo.
async function setTaskRepo(id, current) {
  const repo = await askText("which repository is this",
    "Decides where this session sits in the terminal list, which groups by " +
    "forge, org and repo read out of the worktree path. Atrium guesses this " +
    "when nothing told it. Leave it empty to go back to the guess.",
    current || "");
  if (repo === null) return;
  await patchTask(id, { overrides: { repo: repo.trim() } });
  refresh();
}

// WHERE A SESSION LIVES, as the path its heading is built from.
//
// `terminalLabel` already composes `github/dovholuknf/atrium:atrium-backlog`
// out of the worktree. Everything before the colon is the place and the branch
// is the leaf, so the tree is a split rather than anything new to derive.
//
// A card with no path at all still has to land somewhere, and the empty array
// is that somewhere: it sorts before every heading and draws as loose rows at
// the top of the strip. A session started in a directory atrium cannot ask git
// about is not an error and should not be filed under a heading invented for
// it.
// Where a row nests, and what it is called once it is there.
//
// THE TWO HALVES COME FROM THE TASK, NOT FROM SPLITTING THE LABEL. This split
// on the last colon of the joined label, which is right until a field holds a
// colon of its own. One did: a card whose branch was recorded as
// `main:desktop-edge-win` produced a label with two colons, and the split
// handed back `desktop-edge-win:main` as a directory. The card grew a heading
// of its own next to the repo it belongs under.
function termPathOf(t) {
  const p = terminalParts(t);
  const label = terminalLabel(t) || t.display_title || "";
  return { segs: p.where.split("/").filter(Boolean), leaf: p.leaf || label };
}

// THE NAME SOMEBODY GAVE THIS CARD, WHEN THE PATH DOES NOT ALREADY SAY IT.
//
// `terminalLabel` builds a label out of the worktree, the repo and the branch,
// and never looks at what the card is called. That is right for placing a row
// in the tree and wrong for naming it, because every session ever started in
// one checkout derives the SAME string. Five cards on `D:/git/.../atrium` all
// read `github/dovholuknf/atrium:main`, and the one that was waiting on the
// operator, titled `spike: mcp-gateway in, beside, or rooms`, was drawn as an
// anonymous `main` at the bottom of the strip.
//
// `display_title` differs from `title` when an override was written, which is
// `atrium join --title` and `atrium launch --title` as well as a rename.
//
// SO THE OVERRIDE DOES NOT REPLACE THE LABEL, IT IS ADDED TO IT. Most cards
// here carry one and most of those are the repo name again: `zrok-research`
// against `github/openziti/zrok:zrok-research`. Swapping the label for it would
// shorten ten rows to something the heading above them already said, and the
// pinned group has no headings at all, so those rows would lose the org and the
// repo entirely.
//
// Contained rather than equal, because the useful cases are exactly the ones
// that say something new. `ziti-openwrt` is already in its own path and adds
// nothing. `spike: mcp-gateway in, beside, or rooms` is not and is the whole
// reason this exists.
function termExtraName(t) {
  const shown = String((t && t.display_title) || "").trim();
  const base = String((t && t.title) || "").trim();
  if (!shown || shown === base) return "";
  const path = terminalLabel(t) || "";
  return path.toLowerCase().includes(shown.toLowerCase()) ? "" : shown;
}

// What a row reads as, for deciding whether two of them read the same.
function termRowName(t) {
  const extra = termExtraName(t);
  const path = terminalLabel(t) || (t && t.display_title) || "";
  return extra ? path + " " + extra : path;
}

// TWO ROWS THAT READ THE SAME ARE TWO ROWS YOU CANNOT CHOOSE BETWEEN.
//
// Naming by hand fixes the cards somebody has named and nothing else. Three
// sessions in one checkout that nobody renamed still derive one string, and
// the strip offers three identical rows, one of which is live, one of which
// has exited and one of which belongs to another runner entirely.
//
// So a row whose name is not its own says what makes it different. The runner
// when that tells them apart, which is the common case and the readable one,
// and the tail of the card id when it does not, which is ugly and is still
// better than two rows that cannot be told apart at all.
//
// Held per render rather than computed per row: the question is about the
// whole drawn list and a row cannot answer it alone. Keyed by card id so the
// answer survives the tree walk, which visits rows in an order this does not
// know about.
let termSuffix = new Map();

function termNoteDuplicates(tasks) {
  const by = new Map();
  for (const t of tasks) {
    const k = termRowName(t);
    if (!k) continue;
    if (!by.has(k)) by.set(k, []);
    by.get(k).push(t);
  }
  const out = new Map();
  for (const group of by.values()) {
    if (group.length < 2) continue;
    // Only when the runner is a different answer for every one of them. Two
    // claude sessions both labelled `(claude)` is the same problem with more
    // characters in it.
    const runners = new Set(group.map(t => String(t.runner || "").trim()));
    const byRunner = runners.size === group.length && !runners.has("");
    for (const t of group) {
      out.set(t.id, byRunner
        ? ` (${String(t.runner).trim()})`
        : ` (${String(t.id).slice(-4)})`);
    }
  }
  termSuffix = out;
}

// The strip as a tree of headings.
//
// ELEVEN ENTRIES REPEATED `github/` ELEVEN TIMES, and the characters that told
// them apart were the last few, so the list was widest where it said least.
// Entries were already being cut at the FRONT to keep the leaf, which is the
// symptom of a shared prefix drawn once per row. A heading holds it once.
//
// A NAMED LEVEL HOLDING NO ROW AND ONE LEVEL merges with it, names joined by `/`, down the chain, so `claude/atrium/rnd`
// is one heading. A level with two or more children, or a row of its own, keeps its heading. The merged name is the
// full path, so a fold is keyed on it and a chain that later splits starts open on the split.
function termTree(list) {
  const root = { name: "", rows: [], kids: new Map() };
  for (const t of list) {
    const { segs } = termPathOf(t);
    let node = root;
    for (const seg of segs) {
      if (!node.kids.has(seg)) node.kids.set(seg, { name: seg, rows: [], kids: new Map() });
      node = node.kids.get(seg);
    }
    node.rows.push(t);
  }
  const condense = node => {
    for (const [key, kid] of node.kids) {
      let k = kid;
      while (!k.rows.length && k.kids.size === 1) {
        const [only] = k.kids.values();
        k = { name: k.name + "/" + only.name, rows: only.rows, kids: only.kids };
      }
      condense(k);
      node.kids.set(key, k);
    }
  };
  condense(root);
  return root;
}

// The runner's mark, moving while the runner is.
//
// The strip is where you look to decide which session to go to, and every row
// in it looked the same whether the agent was grinding through a build or sat
// at a prompt. The board answers that with a chip that spells the state out in
// words, which is right in a column and does not fit in a strip a few hundred
// pixels wide. So the mark that is already there carries it.
//
// CSS RATHER THAN THE ANIMATED IMAGE THIS WAS ASKED FOR, and the reasons are
// worth stating because a gif is the obvious answer:
//
//   - There is no build step here and nothing is served that is not in the
//     repository, so a gif is a binary to vendor, embed and cache-bust.
//   - It could not take the session's colour. The mark wears `--tabc`, which
//     is that terminal's own theme, and a gif is the pixels it was saved as.
//   - `prefers-reduced-motion` cannot stop a gif. It stops this, by name, in
//     the block that already exists for the board's live chips.
//   - The board already animates exactly this state three ways, keyed on the
//     same field. A fourth spelling of "working" would be a second answer.
//
// The state is `t.activity.what`, which is what the board's `activityChip`
// reads, and the guards are the same: nothing is drawn as working for a card
// that is waiting on you, finished, or paused. Activity is held in memory and
// outlives the status change that filed the card, so a card in `done` can
// still be carrying a stale `thinking`.
function termRunnerMark(t) {
  const mark = runnerMark(t.runner);
  const a = t.activity;
  // The row carries the one tooltip, so the mark says nothing of its own.
  const bare = mark.replace(/ data-tip="[^"]*"/, "");
  if (!workingNow(t)) return bare;
  // Inserted into the class list the shared builder produced, rather than the
  // builder growing a parameter. `runnerMark` is the board's and is called
  // from three places that do not want this.
  return bare.replace('class="rmark"', `class="rmark working ${esc(a.what)}"`);
}

// One entry in the switcher.
//
// A BOARD CARD, the same shape and the same class, because the switcher had
// grown its own card with its own borders and hover and the two drifted until
// one session looked like two different things depending on the view.
//
// `deep` is whether a heading above already says where this is. Under one the
// row draws its LEAF alone, which is the whole point of the headings: the
// shared prefix moves up and stops being drawn once per row.
//
// The class and the custom properties a row is drawn with: `--tabc` and `--tabbg` for the bridge, and the worn palette.
// A shut group's heading wears the same when it stands in for the attached row.
function termRowWear(t) {
  const given = themeSource(t);
  const th = given || themeFor(t);
  const on = termTask && t.id === termTask.id;
  const wear = given ? termWear(t, on, termCold(t)) : on ? { cls: " plain", style: "" } : NO_WEAR;
  return { cls: wear.cls, vars: (given ? `--tabc:${th.cursor || th.foreground};` : "") + `--tabbg:${th.background}` +
    (wear.style ? ";" + wear.style : "") };
}

function termRow(t, deep, kid) {
  // THE ENTRY WEARS ITS OWN TERMINAL'S COLOURS, so the list reads as a row of
  // tabs onto the panes behind it rather than as a list of names. Amber for
  // waiting said something the board already says better and cost the one
  // signal that is actually per session.
  // A card nobody coloured has no theme of its own here: it wears the skin's card whether it is attached or not, and its
  // terminal's neutral palette only reaches the bridge, through `--tabbg`.
  const on = termTask && t.id === termTask.id;
  // A COLLAPSED PARENT HOLDING THE ATTACHED CARD CARRIES ITS BRIDGE, as a shut group does (`termGroupHead`). The attached
  // card is one of its hidden children, at any depth, so the row wears that card's colours and stands in for it.
  const carries = !on && termKidTotal(t) > 0 && termKidsShut(t) && termHoldsOn(t);
  const wear = termRowWear(carries ? termTask : t);
  if (on) themeTrace("row", t);
  const style = wear.vars;
  const full = terminalLabel(t) || t.display_title;
  const leaf = termPathOf(t).leaf;
  const tail = termSuffix.get(t.id) || "";
  // THE NAME LEADS, THE ADDRESS FOLLOWS. Every session in one checkout derives
  // the same address, so a row led by the address buried the one thing that tells
  // them apart: the name somebody gave it. So `display_title` is the primary label
  // and the address is a dim second line under it. A card with no name of its own
  // has only the address to show, and then that is the single line it draws.
  const path = (deep ? leaf : full) || "";
  const lead = cardLead(t, "terms");
  const named = lead.main;
  const primary = (named || path) + tail;
  const secondary = named ? path : "";
  // The hover keeps the whole address and the name whatever the row had room to draw.
  const hover = named ? (lead.other ? named + " · " + lead.other : named) + " · " + full + tail : full + tail;
  // KEPT AGAINST THE FILTER. The open pane is never hidden (see `termKept`), so with the chip for its kind off, a cold
  // row still listed is this one. Grey means the filter would take the row out, and this one is not going anywhere,
  // so it is NOT drawn grey. It wears a chip that says it is kept and why.
  const keptOpen = on && termCold(t) && termKept(t, termShow());
  const keptWord = attachIsInFlight(t.id) ? "opening" : "attached";
  const keptChip = keptOpen
    ? `<span class="chip accent kept" aria-label="${keptWord}. the filter would hide it" data-tip="${
      keptWord === "opening" ? "being started again here, so the filter keeps it listed"
        : "open in the pane, so the filter keeps it listed"}">${keptWord}</span>` : "";
  const allKids = termKids.get(t.id) || [];
  const shut = termKidTotal(t) > 0 && termKidsShut(t);
  // A fold hides every child, one that needs you included: the warn chip on the parent is the signal (`termKidsToggle`).
  const shown = shut ? [] : allKids;
  const kids = shown.map(k => termRow(k, false, true)).join("");
  return `
    <div class="card tab ${on ? "on" : ""}${carries ? "carries" : ""}${kid ? " kid" : ""}${
           // COLD, NOT GONE. Only ever a pinned row, since an unpinned one
           // without a runner is not drawn at all. Greyed rather than removed
           // so the bucket keeps the shape you gave it, and it still answers
           // a click: there is nothing to attach to, so it offers to start
           // the session again where it was.
           termCold(t) && !keptOpen ? " cold" : keptOpen ? " kept" : ""}${wear.cls}${newCardClass(t)}"
         data-id="${t.id}"${kid ? "" : ` draggable="true"`}
         data-tip="${esc(hover + (termCold(t) ? (t.parked_at ? "\nparked after sitting idle. resume it from the right-click menu"
           : "\nthis one has exited. resume it from the right-click menu") +
           (keptOpen ? "\nthe filter would hide it, but it is open in the pane, so it stays until you switch away" : "")
           : t.starting && !t.supervised ? "\natrium is starting this one. its runner is not up yet"
           : termJoined(t) ? "\njoined from your own terminal. atrium cannot attach to it. click for what you can do" : ""))}"
         style="${style}"
         onclick="${termJoined(t) ? `joinedClick('${t.id}')` : `attachTask('${t.id}')`}"
         oncontextmenu="termMenu(event, '${t.id}')">
      ${ctxLine(t)}
      <div class="card-line">
        <div class="title">
          <span class="pin ${t.pinned ? "on" : ""}"
            aria-label="${t.pinned ? "always here. click to unpin" : "keep this here"}"
            data-tip="${t.pinned ? "always here. click to unpin" : "keep this here"}"
            onclick="event.stopPropagation();togglePin('${t.id}', ${!t.pinned})"
            >${t.pinned ? "&#9733;" : "&#9734;"}</span>${termRunnerMark(t)}${typeof kaTitleMark === "function" ? kaTitleMark(t) : ""}<span
            class="tstack"><span
            class="tname${secondary ? "" : " aspath"}"
            >${esc(primary)}</span>${
              // Where it lives, under the name and dimmed. Only when the name is
              // its own: an unnamed card already shows the address as its name,
              // so a second copy of it under itself says nothing.
              secondary
                ? `<span class="tpath">${esc(secondary)}</span>`
                : ""
            }</span>
        </div>
        ${termRowChips(t, keptChip)}${termKidsToggle(t, termKidTotal(t), termKidTotal(t) - shown.length, shut)}
      </div>
      ${typeof seenQuestionLines === "function" ? seenQuestionLines(t) : ""}
    </div>${kids ?`<div class="tkids">${kids}</div>` : ""}`;
}

// The spawned cards drawn under each parent row, by parent id. Rebuilt on every render by `termNest`.
let termKids = new Map();
// The same, over every card the list could hold, drawn or not: a chip, the search or a cold row can leave a parent's
// children out of `termKids`, and the parent still has them. The popup's children section reads these (`termChildInfo`).
let termKidsPool = new Map();
// THE COUNT ON A ROW IS WHAT EXPANDING IT SHOWS. A collapsed parent said 3 while its open row listed 1, because the other
// two were hidden by a chip. So this counts the children that are drawn, and nothing about the hidden ones goes on the
// row: the popup says how many there are and what hides them.
function termKidTotal(t) { return (termKids.get(t.id) || []).length; }

// A parent's fold lives in the same list as the group folds, keyed by the bare id so a room-set change keeps it.
// Like the groups it records what is NOT as it comes, and what it comes as is the subagents chip's say: off, a parent
// comes folded and an entry means expanded, on, it comes expanded and an entry means folded. A chip change clears them
// (see `toggleTermShow`).
function termKidsKey(t) { return "kids:" + bareId(t.id); }
function termKidsShut(t) { return termSubsOn() ? isFolded(termKidsKey(t)) : !isFolded(termKidsKey(t)); }

// IS THE HUMAN OWED SOMETHING BY THIS CARD. The one predicate behind a folded parent's `N need you` chip, its hover line and
// the popup's children summary and list. A worker that ended its turn for the agent that launched it is needs-input too,
// but it is waiting on that agent, so `isWaiting` alone overcounts. Owed to the human: a permission prompt, an open question
// the launcher has not taken (`seen.seen_via` `launcher` means a report already reached it), or a blocked or stuck notice.
function termOwesHuman(t) {
  if (!t || !isWaiting(t)) return false;
  if (t.status === "needs-permission" || isBlocker(t) || isStuck(t)) return true;
  return cardHasOpenQuestion(t) && !(t.seen && t.seen.seen_via === "launcher");
}

// How many cards under this one, at any depth, satisfy `pred`.
function termKidsCount(id, pred, kids) {
  const map = kids || termKids;
  return (map.get(id) || []).reduce((n, k) => n + (pred(k) ? 1 : 0) + termKidsCount(k.id, pred, map), 0);
}

// WHAT THE ROW'S BADGES MEAN, for the popup's children section. `pool` is every card, `t` the parent. The visible tree is
// the one the list draws: the cards the chips list, nested by `termNest`, so its counts are the row's. Everything else under
// the parent is hidden, and says by what: `paused` (no agent, can be resumed: the paused chip) or `dead` (no agent, cannot
// be revived: it is listed only in the ended group). `waiting` is every card under this one at any depth that needs you,
// the row's `!` count, so each can be named and opened.
function termChildInfo(t, pool) {
  const cards = (pool || []).filter(c => !c.offline);
  const show = termShow();
  const direct = termNest(cards).kids.get(t.id) || [];
  if (!direct.length) return null;
  const visible = termNest(cards.filter(c => c.id === t.id || termListed(c, show))).kids;
  const shown = visible.get(t.id) || [];
  const shownIds = new Set(shown.map(c => c.id));
  const hidden = direct.filter(c => !shownIds.has(c.id));
  const waiting = collectKids(t.id, termOwesHuman, visible);
  const paused = hidden.filter(cardPaused).length;
  return { total: direct.length, shown: shown.length, hidden: hidden.length, paused, dead: hidden.length - paused,
    running: termKidsCount(t.id, workingNow, visible), waiting,
    // What the popup lists, one per line: the cards that need you at any depth first, then the shown direct children.
    list: waiting.concat(shown.filter(c => !waiting.includes(c))) };
}
// The cards under `id`, at any depth, that satisfy `pred`.
function collectKids(id, pred, kids) {
  return (kids.get(id) || []).flatMap(k => (pred(k) ? [k] : []).concat(collectKids(k.id, pred, kids)));
}

// The chevron on a parent row. A real button so it takes focus and Enter/Space. Folded, it is a count of what is
// under the row (the direct cards), then how many cards at any depth are running and how many need the human, drawn
// as chips like the row's other badges. Nothing under a folded row is drawn, a waiting child included: the chip is how it is found.
function termKidsToggle(t, total, hidden, shut) {
  if (!total) return "";
  const run = shut ? termKidsCount(t.id, workingNow) : 0;
  const wait = shut ? termKidsCount(t.id, termOwesHuman) : 0;
  const base = shut ? "show the cards this one launched" : "hide the cards this one launched";
  const say = [run && run + " running", wait && wait + " need" + (wait === 1 ? "s" : "") + " you"].filter(Boolean);
  const tip = shut ? `${total} under this one${say.length ? ": " + say.join(", ") : ""}. click to show them` : base;
  return `<button type="button" class="tkidfold${shut ? " shut" : ""}${run ? " running" : ""}" aria-expanded="${shut ? "false" : "true"}"
    aria-label="${tip}" data-tip="${tip}"
    onclick="event.stopPropagation();toggleKids('${t.id}')"
    ><span class="tcaret">${shut ? "&#9656;" : "&#9662;"}</span>${shut ? `<span class="chip tkidn">${total}</span>${
      run ? `<span class="chip tkidrun"><i class="tkidspin"></i>${run}</span>` : ""}${
      wait ? `<span class="chip warn tkidwait">${wait}&#8239;!</span>` : ""}` : ""}</button>`;
}

function toggleKids(id) {
  const key = termKidsKey({ id });
  const list = foldedColumns();
  const i = list.indexOf(key);
  if (i >= 0) list.splice(i, 1); else list.push(key);
  localStorage.setItem("atrium.folded", JSON.stringify(list));
  repaintLists();
}

// A second window follows, as the grouping does. The browser fires this in every OTHER window.
addEventListener("storage", e => {
  if (!e.key || e.key === "atrium.folded") repaintLists();
});

// SPAWNED CARDS FILE UNDER THEIR PARENT, whatever the sort or the grouping. A card tagged atrium:subagent with a
// spawned_by_id whose parent is in the list is drawn inside the parent's row, so it cannot sit beside it under the
// same name. Done on the sorted list before grouping, so every group mode sees only the top rows and the kids keep
// the sort's order. A pinned card stays a row of its own because pinning is a deliberate placement. A child whose
// parent is not in the list, or whose lineage loops, stays a normal row.
function termNest(list) {
  const byBare = new Map(list.map(t => [bareId(t.id), t]));
  const parentOf = t => {
    if (!nestsUnderLauncher(t) || t.pinned) return null;
    return launcherOf(t, list, byBare);
  };
  const kids = new Map();
  const nested = new Set();
  for (const t of list) {
    const p = parentOf(t);
    if (!p) continue;
    const seen = new Set([t]);
    let up = p;
    while (up && !seen.has(up)) { seen.add(up); up = parentOf(up); }
    if (up) continue;
    if (!kids.has(p.id)) kids.set(p.id, []);
    kids.get(p.id).push(t);
    nested.add(t.id);
  }
  return { kids, nested };
}

// The chips on the right of a terminal row: a held peer message, and the
// popped-out marker. Drawn in one .chips box so a row can carry both.
function termRowChips(t, lead) {
  const held = termHeldChip(t);
  const room = termRoomChip(t);
  const popped = poppedOut(t.id)
    // Says where it is rather than letting you click and wonder why nothing
    // happened. Clicking raises that window.
    ? `<span class="chip accent"
         aria-label="this session is showing in a window of its own. click to raise it" data-tip="this session is showing in a window of its own. click to raise it"
         >&#8599;</span>`
    : "";
  // Unread and unanswered, which is where the operator is looking while workers
  // run. See js/seen.js.
  const stuck = typeof stuckMark === "function" ? stuckMark(t) : "";
  const ncx = typeof newContextChip === "function" ? newContextChip(t) : "";
  const cache = typeof keepaliveChip === "function" ? keepaliveChip(t) : "";
  // A card atrium is launching has no runner yet, which is not the same as one joined from another terminal.
  const joined = t.starting && !t.supervised ? `<span class="chip">starting</span>`
    : termJoined(t) ? `<span class="chip">joined</span>` : "";
  // The past-the-limit mark, which the strip did not draw. A card cycling its context shows that instead.
  const lim = t.new_context ? "" : ctxWarnMark(t);
  const minion = isMinion(t) ? `<span class="chip" data-tip="a throwaway session an item launched. it is listed because it needs you">minion</span>` : "";
  const landed = t.landed_kept
    ? `<span class="chip warn" aria-label="${esc(t.landed_kept)}" data-tip="${esc(t.landed_kept)}">landed</span>` : "";
  const inner = (lead || "") + minion + stuck + newCardChip(t) + lim + ncx + seenChips(t) + cache + held + room + popped + joined + landed;
  return inner ? `<div class="chips">${inner}</div>` : "";
}

// WHICH ROOM THIS CARD RUNS IN, when that is a question worth asking. With one
// room attached every id is bare, `roomOf` answers "", and this draws nothing: a
// single room needs no label because every card is in it. The tag appears only
// when a second room joins, so the badge appears with it, on every card at once,
// which is exactly when clint could not tell an sg4 card from an sgg one. The
// room name is the tag itself (e.g. claude-sg4 / claude-sgg). Mirrored in the
// terminal pane header by `openTerm`, so a card names its room in the list and
// when it is open.
function termRoomChip(t) {
  const room = roomOf(t && t.id);
  if (!room) return "";
  return `<span class="chip room" style="--rhue:${roomHue(room)}"
    data-tip="this card runs in room ${esc(room)}"
    >${esc(room)}</span>`;
}

// A PULSING BANG WHEN A PEER MESSAGE IS HELD AGAINST ITS SENDER, and a quiet
// envelope when it only waits as it was sent to.
//
// The room-side gate holds a peer message when the operator's line is dirty or
// he has just been typing, and retries it on a widening backoff. Until it lands
// the card carries this so a glance across the strip finds the one holding
// something for him, and the title says what clears it. Distinct from the
// working spinner on the runner mark: that is the session moving, this is a
// message stuck behind his own line.
//
// THE `!` IS FOR SOMETHING IN THE WAY. A message sent `when: "done"`, or to a
// runner that does not take input mid-turn, waits for the turn to end because
// that is what was asked, and it wore the warn-coloured `!` for as long as a
// worker's turn ran. clint: "this 'alerta' is super dumb". The room decides
// which it is (`held_quiet`, see noteHeld in pendinginject.go) and this only
// draws it: a quiet hold is `✉` or `✉ 2` in the neutral chip, with no pulse.
// It turns into the `!` if the turn runs past the room's patience, or if an
// immediate message is queued behind it.
//
// COUPLED TO THE ROOM SIDE. `activity.held_peer` is the live signal a daemon
// with the injector raises. Against a daemon without it the field is never set
// and this draws nothing. A room older than `held_quiet` gets the `!` for every
// hold, as it always did.
//
// THE TIP NAMES WHAT IS HOLDING IT, because there are three things and each is
// cleared by something different: the line (`held_for` "line"), the turn
// ("turn"), or a dialog. A turn wait names whose rule it is, the sender's
// (`held_turn` "asked") or the runner's ("runner"), and only that one. Blaming
// the line for a turn wait told the operator to clear a line that was already
// empty. A room older than `held_for` gets the line wording it always had. The
// chip counts, `✉︎ 2`, like `? N`. The `!` is kept for what needs you.
//
// ONE SHAPE FOR EVERY REASON: how many, how long, what blocks them, and what
// to do. The sender is not in it: the count and the reason are what decide
// whether to act, and the timeline says who sent what.
function termHeldChip(t) {
  const a = t && t.activity;
  if (!a || !a.held_peer) return "";
  const secs = Number(a.held_seconds) || 0;
  const count = Number(a.held_count) || 1;
  if (a.held_quiet) {
    return `<span class="chip queued" onclick="event.stopPropagation()"
      data-tip="${esc(termQueuedTip(count, secs, a.held_turn, a.held_escalate_min))}">${termHeldLabel(count)}</span>`;
  }
  return `<span class="chip held" onclick="event.stopPropagation()"
    data-tip="${esc(termHeldTip(count, secs, a.held_for, a.held_turn))}">${termHeldLabel(count)}</span>`;
}

// The drawn envelope of the mail chip (peek-answers.js MAIL_SVG), so it centres against the count the same way.
function termHeldLabel(count) {
  return MAIL_SVG + (count > 1 ? `<span class="mail-n">${count}</span>` : "");
}

// The quiet mark's tooltip: how many, how long, and the one rule that holds
// them.
function termQueuedTip(count, secs, turn, escalateMin) {
  const one = count === 1;
  const why = turn === "runner"
    ? "because this runner does not take input mid-turn"
    : "sent to arrive when the session's turn ends";
  return `${count} ${one ? "message" : "messages"} queued for this agent for ${termHeldAge(secs)}, ${why}. ` +
    `${one ? "It goes" : "They go"} in when the turn ends, or after ${Number(escalateMin) || 15} minutes if the turn runs ` +
    `that long`;
}

// The held chip's tooltip, singular or plural throughout.
function termHeldTip(count, secs, why, turn) {
  const one = count === 1;
  const these = one ? "this message" : "these messages";
  const theTurn = {
    asked: `the session's turn, which ${one ? "it" : "the oldest"} was sent to wait for`,
    runner: "the session's turn, because this runner does not take input mid-turn",
  }[turn] || "the session's turn, which has to end first";
  const blocked = {
    turn: [theTurn, "They go in when the turn ends", "It goes in when the turn ends"],
    dialog: ["a dialog open in this terminal, which typing would answer",
      "Answer the dialog to dequeue " + these],
    line: ["input in this terminal", "Submit your text to dequeue " + these],
  }[why];
  const waited = `${count} ${one ? "message has" : "messages have"} been waiting to be delivered to this agent ` +
    `for ${termHeldAge(secs)}`;
  // "new-context" is a card cycling its context, and "" or anything unknown is a hold the daemon cannot name,
  // so neither is the operator's line.
  if (why === "new-context") {
    return `${waited} and ${one ? "is" : "are"} held while a new-context cycle is in progress. ` +
      `${one ? "It goes" : "They go"} in after its wake prompt`;
  }
  if (!blocked) return `${waited} and ${one ? "is" : "are"} about to be retried`;
  const todo = one && blocked[2] ? blocked[2] : blocked[1];
  return `${waited} and ${one ? "is" : "are"} blocked by ${blocked[0]}. ${todo}`;
}

// The held age in full, hours, minutes and seconds, with the leading zero units
// left off: 16s, 2m 5s, 1h 0m 3s.
function termHeldAge(secs) {
  secs = Math.max(0, Math.floor(secs));
  const h = Math.floor(secs / 3600), m = Math.floor(secs % 3600 / 60), s = secs % 60;
  if (h) return `${h}h ${m}m ${s}s`;
  if (m) return `${m}m ${s}s`;
  return `${s}s`;
}

// The whole strip, everything under where it lives.
//
// PINNING STOPS BEING A DIVIDER HERE, which is the decision this had to
// settle. Grouping and pinning both wanted to be the top level split and they
// cannot both be, and the obvious answer is the wrong one.
//
// The obvious answer is what the stack does: pinned as one group above all the
// others, since pinning outranks the sort there. Run against the real board it
// falls apart. Eight of ten terminals here are pinned, so the pinned block is
// almost the whole list drawn flat, and the repeated `github/` prefixes that
// this exists to remove are all still in it. A split that puts most rows on
// one side is not a split.
//
// So pinning keeps its meaning and loses its heading. It still sorts first,
// now WITHIN each group, and the star says which rows it applies to. That star
// was invisible until two days ago, which is probably why the divider was
// carrying the whole signal.
//
// A HEADING IS NOT A CONTROL HERE. It labels and nothing else: no click
// target, no accordion. `docs/orchestrator/dispatch-queue.md` at 3be95cab group F has an open complaint
// that a group heading on the BOARD toggles when you click the white space
// around it, and three levels of heading in a narrow strip would multiply
// whatever is decided there. A label cannot have that problem.
function termGroupsHTML(list) {
  const folded = termFolded();
  // THE BOARD'S GROUPING, ON THE STRIP. One setting, two surfaces: `grouper`
  // and `groupingPrefs` are the board's and are read here rather than copied,
  // so changing how the board groups changes how this groups and there is no
  // second answer to keep in step.
  //
  // PROJECT IS THE EXCEPTION AND STAYS THE TREE. The board draws a project as
  // one flat heading, `org/repo`, which is right for a column and wrong for a
  // strip a few hundred pixels wide: eleven entries repeating `github/` is the
  // complaint `termTree` exists to answer, and it answers it by lifting the
  // shared prefix into nested headings. Same grouping, drawn for the space it
  // is in. Every other mode has no path to nest, so it draws flat.
  const p = typeof groupingPrefs === "function" ? groupingPrefs("terms") : null;
  const g = typeof grouper === "function" ? grouper("terms") : null;
  // OFF MEANS OFF, and it did not.
  //
  // `grouper()` answers null for two different reasons: grouping is switched
  // OFF, or the mode is one this strip draws its own way. Both arrived here as
  // "no grouper", and the fallback is the path tree, so pressing `off` swapped
  // one set of headings for another set of headings. The one thing it could not
  // do was stop grouping.
  //
  // `p.on` is the question that was being skipped. Off is a flat list in the
  // order it was handed over, which is what sorting by activity means when
  // nothing is allowed to reorder it into buckets.
  if (p && !p.on) return list.map(t => termRow(t, false)).join("");
  if (!g || !p || (p.mode === "project" && !String(p.by || "").trim())) {
    return termNodeHTML(termTree(list), 0, "", folded);
  }
  return termFlatGroupsHTML(list, g, folded);
}

// One heading per group, in the grouper's own order.
//
// A card can land in SEVERAL groups, which is what `many` means and what
// grouping by tag does. That is why the rows are collected into a map rather
// than the list being partitioned: partitioning has to decide where a card
// with three tags goes, and the answer is all three.
//
// The heading keys are prefixed, so folding a group called `dotfiles` here
// does not also fold a path segment called `dotfiles` in the tree. They are
// different groupings of the same sessions and a fold is an opinion about one
// of them.
function termFlatGroupsHTML(list, g, folded) {
  const by = new Map();
  for (const t of list) {
    let names = [];
    try { names = g.of(t) || []; } catch (e) { names = []; }
    if (!names.length) names = [""];
    for (const n of names) {
      const key = String(n ?? "");
      if (!by.has(key)) by.set(key, []);
      by.get(key).push(t);
    }
  }
  if (typeof seedGroups === "function") seedGroups(by, g);
  const names = [...by.keys()].sort((a, b) => {
    try { return g.cmp(a, b); } catch (e) { return a.localeCompare(b); }
  });
  return names.map(name => {
    // A group with no name is the absence of an answer rather than one, and
    // the tree calls that `uncategorized`. Same word, so the two modes do not
    // disagree about what nothing is called.
    const shown = name || "uncategorized";
    const at = "g:" + shown;
    // An empty group starts shut, and `open:` + the heading records somebody opening it.
    const nobody = by.get(name).length === 0;
    const off = nobody ? !folded.has("open:" + at) : folded.has(at);
    // A group you made is a drop target, see `wireTermDrag`. Its rows keep the strip's sort.
    const mine = g.handOrdered && name && name !== UNTAGGED;
    // THE GROUP'S COLOUR, from the same `groupHue` the stack and the board
    // read, so a recolour made on any of the three lands on all three. The
    // strip drew every heading in the label grey, which is what "the group
    // colours don't work" was about when it was said from here.
    const hue = name && typeof groupHue === "function" ? groupHue(name) : null;
    const style = hue === null ? "" : ` style="--ghue:${hue}"`;
    // And its heading takes the board's group menu on a right click, which is
    // where it is moved, renamed, removed and recoloured. Without it a group
    // made with `+ new group` here could never be taken away from here.
    // `untagged` is the absence of a group and gets no menu.
    const menu = name && name !== UNTAGGED ? name : "";
    const head = termHeading(shown, at, by.get(name).length, off, menu, style, mine, by.get(name), nobody);
    if (off) return head;
    const rows = by.get(name);
    // `untagged` in custom mode is where a row is dragged to take it OUT of
    // the group it came from. See `wireUngroupDrop`.
    const out = g.handOrdered && name === UNTAGGED ? ` data-ungroup="1"` : "";
    return head + `<div class="tnest${hue === null ? "" : " hued"}"${style}${out}${
      mine ? ` data-group="${esc(name)}"` : ""}>${rows.length
      ? rows.map(t => termRow(t, false)).join("") : emptyGroupHint(shown)}</div>`;
  }).join("");
}

// IS THIS CARD FILED INTO ONE OF YOUR GROUPS. A pinned card is drawn in the
// pinned bucket, and in custom mode also under every named group it carries.
//
// Leaving it out of the groups made `into group` on a pinned card do nothing
// you could see: the tag landed, the group still said 0 and asked you to file
// something. Filing is as deliberate as pinning, so both are honoured. A
// pinned card in none of your groups stays out of `untagged`, which is the
// board's heap and not somewhere you put it.
function termFiled(t) {
  const g = typeof grouper === "function" ? grouper("terms") : null;
  if (!g || !g.handOrdered) return false;
  try { return g.of(t).some(n => n && n !== UNTAGGED); } catch (e) { return false; }
}

// A drag moves rows by hand while the pointer travels, so the DOM can disagree
// with the markup the strip was last painted from. `setHTML` skips a paint
// whose markup did not change, and filing a pinned card or abandoning a drag
// can leave it unchanged. Forgetting the last paint makes the next one
// reconcile the rows that were moved.
function repaintTermList() {
  const host = document.getElementById("term-list");
  if (host) host.__paintedFrom = null;
  renderTermList();
}

// WHICH GROUPS ARE FOLDED, kept in this browser and keyed by the path itself.
//
// Not on the card and not on the daemon: this is a property of how somebody is
// reading the list right now, and two windows on one board are allowed to
// disagree about what is folded.
//
// Keyed by path rather than by position, so folding `github/openziti` and then
// starting a session in a new org leaves the fold where it was instead of
// sliding onto whatever moved into that slot.
const TERM_FOLDED = "atrium.termfolded";

function termFolded() {
  try { return new Set(JSON.parse(localStorage.getItem(TERM_FOLDED) || "[]")); }
  catch (e) { return new Set(); }
}

function toggleTermGroup(path, empty) {
  const folded = termFolded();
  if (empty) path = "open:" + path;
  if (folded.has(path)) folded.delete(path);
  else folded.add(path);
  try { localStorage.setItem(TERM_FOLDED, JSON.stringify([...folded])); } catch (e) {}
  renderTermList();
}

// A group heading, which IS a control now.
//
// THE ROW IS NOT THE TARGET, and that is deliberate. `docs/orchestrator/dispatch-queue.md` at 3be95cab
// group F carries a complaint about the board's group headings: the whole
// width toggles the accordion and nothing says so, so clicking what looks like
// empty space next to a name collapses the thing you were reading. The button
// here is the caret and the name and stops there, and the rule that fills the
// rest of the width is decoration outside it.
//
// The count is on the heading because a folded group has to say what is inside
// it or folding loses information rather than hiding it.
//
// SHOWN OUT OF HOW MANY. When the search is taking rows out of a group the
// count reads `7/15`, so a short group says it is short because of the search
// and not because the sessions are gone. A group with nothing hidden reads its
// plain count. The totals are the same grouping run over the unsearched list,
// set by `renderTermList` into `termTotals` and keyed by the heading's path.
//
// `group` names a group, and gives the heading the board's group menu (see
// `groupMenu`). Empty for every other heading. `style` carries its hue.
//
// `movable` is a group you made, in `custom` mode, and only that heading drags:
// it is the one mode whose order is yours to write (`p.groups`). Every other
// mode sorts its groups by a rule, so a drag there would be undone by the next
// paint, and the heading says where reordering lives instead of offering it.
function termHeading(name, path, count, folded, group, style, movable, rows, empty) {
  const total = termTotals.get(path);
  const shown = total > count ? `${count}/${total}` : `${count}`;
  const tip = total > count ? ` (${total - count} hidden by the search)` : "";
  // The apostrophe, on top of the HTML escaping. A group name is free text now
  // that these headings can come from tags and from operator-written code, and
  // HTML escaping is not JavaScript escaping: `it's mine` closes the quoted
  // argument and the rest of the attribute parses as nonsense. Same fix, same
  // reason, as `tagChips` in `stack.js`.
  const arg = esc(path).replace(/'/g, "&#39;");
  const menu = group && typeof groupMenu === "function"
    ? ` oncontextmenu="groupMenu(event, '${esc(group).replace(/'/g, "&#39;")}')"` : "";
  const how = movable ? ". drag to move it, right click to rename, remove or recolor it"
    : group ? ". right click to recolor it. groups reorder by hand in the custom grouping" : "";
  // A SHUT GROUP HOLDING THE ATTACHED SESSION IS WHERE ITS BRIDGE GOES. See `placeTabBridge`.
  const carries = folded && rows && rows.some(termHoldsOn);
  // It wears the attached row's colours, so the heading, the bridge and the pane frame are one colour.
  const wear = carries ? termRowWear(termTask) : null;
  if (wear) style = ` style="${(style || "").replace(/^ style="|"$/g, "")};${wear.vars}"`;
  return `<button class="tgroup${style ? " hued" : ""}${movable ? " movable" : ""}${carries ? " carries" + wear.cls : ""}"${style || ""}${
      movable ? ` draggable="true" data-ghead="${esc(group)}"` : ""}
      onclick="toggleTermGroup('${arg}'${empty ? ", true" : ""})"${menu}
      data-tip="${folded ? "show" : "hide"} ${esc(name)}${tip}${how}"
      >${movable ? `<span class="tgrip" aria-hidden="true">&#10303;</span>` : ""}<span
        class="tcaret">${folded ? "&#9656;" : "&#9662;"}</span
      ><span class="tgname">${esc(name)}</span
      ><span class="tgcount">${shown}</span></button>`;
}

// Is the attached session this row, or one drawn inside it.
function termHoldsOn(t) {
  return !!termTask && (t.id === termTask.id || (termKids.get(t.id) || []).some(termHoldsOn));
}

// How many rows each heading would hold with nothing hidden, keyed by the same
// path `termHeading` is given. Rebuilt on every render. See `termCountTotals`.
let termTotals = new Map();

// termCountTotals runs the strip's grouping over the UNFILTERED list and counts
// each heading, in the same shape the drawing code keys its headings: `g:name`
// for a flat grouping, the slash path for the tree, and `uncategorized`.
function termCountTotals(list) {
  const out = new Map();
  const p = typeof groupingPrefs === "function" ? groupingPrefs("terms") : null;
  const g = typeof grouper === "function" ? grouper("terms") : null;
  if (p && !p.on) return out;
  if (!g || !p || (p.mode === "project" && !String(p.by || "").trim())) {
    const walk = (node, path) => {
      const at = node.name ? (path ? path + "/" + node.name : node.name) : "";
      if (at) out.set(at, termCount(node));
      for (const kid of node.kids.values()) walk(kid, at);
    };
    const root = termTree(list);
    walk(root, "");
    out.set("uncategorized", root.rows.length);
    return out;
  }
  for (const t of list) {
    let names = [];
    try { names = g.of(t) || []; } catch (e) { names = []; }
    if (!names.length) names = [""];
    for (const n of names) {
      const at = "g:" + (String(n ?? "") || "uncategorized");
      out.set(at, (out.get(at) || 0) + 1);
    }
  }
  return out;
}

// Rows at this level first, then the levels under it.
//
// The root has no name and draws no heading, which is what puts a card with no
// path at the top as loose rows rather than under a heading invented for it.
//
// A LEVEL HOLDING ONE ROW IS NOT A LEVEL. It draws as `project:branch` on the
// row instead of a heading with a single thing under it.
//
// This is the difference between the design working and not. The first version
// drew eleven headings for ten rows on the real board, which is the failure the
// whole item warns about: three levels of accordion in a strip a few hundred
// pixels wide spending more height on headings than on sessions. Folding the
// single-row levels took it to five headings for the same ten rows, and the
// ones that remain are the ones that group something.
// NESTED CONTAINERS, NOT A FLAT LIST WITH AN INDENT CLASS.
//
// The first version drew headings with a `data-depth` and left the rows at the
// left edge, so a session three levels down sat in line with one at the top and
// the tree was in the markup and nowhere on the screen. Indenting rows by depth
// would have fixed the alignment and nothing else.
//
// A container per level gets both for free: the indent is the container's, and
// a border down its left edge draws the vertical guide that says which rows
// belong to which heading, the way `tree` does with line drawing characters.
// The lines are continuous because the box is, rather than being reassembled
// per row out of glyphs that have to agree about depth.
//
// It also makes folding a container with nothing in it rather than a filter
// over a flat list.
function termNodeHTML(node, depth, path, folded) {
  const at = node.name ? (path ? path + "/" + node.name : node.name) : "";
  const isFolded = at !== "" && folded.has(at);
  let out = node.name ? termHeading(node.name, at, termCount(node), isFolded, "", "", false, isFolded && termNodeRows(node)) : "";
  if (isFolded) return out;

  // AT THE ROOT, LOOSE ROWS GET A HEADING OF THEIR OWN.
  //
  // A session in a directory atrium cannot read a forge and an org out of sits
  // under no heading, and at the top of the list that read as a session that
  // had escaped the grouping rather than one the grouping has nothing to say
  // about. `uncategorized` says which it is.
  //
  // Named and moved to the BOTTOM, since a catch-all above everything is the
  // first thing read and the least interesting. And only when there is
  // something to contrast it with: with no groups at all it is the whole list,
  // and a heading over everything names nothing, which is the same rule the
  // pinned divider followed.
  const loose = node.name ? "" : (() => {
    if (!node.rows.length) return "";
    if (!node.kids.size) return node.rows.map(t => termRow(t, false)).join("");
    const isOff = folded.has("uncategorized");
    const head = termHeading("uncategorized", "uncategorized", node.rows.length, isOff, "", "", false, node.rows);
    if (isOff) return head;
    return head + `<div class="tnest">${node.rows.map(t => termRow(t, true)).join("")}</div>`;
  })();

  // EVERY LEVEL IS A HEADING, including one holding a single row.
  //
  // This folded a level with one row into that row, so a repo with one branch
  // came out as `ziti-openwrt:firmware-upgrade-recovery-docs` on a row while
  // `desktop-edge-win` beside it, which happened to have two, was a heading.
  // Two repos, two shapes, decided by something that is not about them.
  //
  // It was done to keep the height down and that is the wrong thing to spend
  // consistency on. The same instinct folded orgs a moment earlier and made
  // the same mess: the shape of the list should say host, then org, then repo,
  // every time, so somebody can read it without working out which rule applied
  // to which row.
  let inner = node.name ? node.rows.map(t => termRow(t, true)).join("") : "";
  // HEADINGS ARE ALWAYS IN NAME ORDER, whatever the rows under them are sorted
  // by.
  //
  // The children arrive in a Map, so iterating it draws them in the order the
  // rows happened to build them in, which is the row sort. Under `by activity`
  // that order changes every poll, so `ziti` and `ziti-sdk-csharp` swapped
  // places while nothing about either had changed. A heading names a place,
  // and a place does not become more urgent, so it has no business moving.
  // Rows inside a heading still follow whichever sort is on: that is the thing
  // the sort was asked about.
  for (const kid of [...node.kids.values()].sort((a, b) => a.name.localeCompare(b.name))) {
    inner += termNodeHTML(kid, depth + 1, at, folded);
  }
  // The root has no heading, so it has nothing to indent under and no guide to
  // draw. Wrapping it would push the whole strip right for no reason.
  return node.name ? out + `<div class="tnest">${inner}</div>` : out + inner + loose;
}

// Every row under a heading, at any depth.
function termNodeRows(node) {
  return [...node.rows, ...[...node.kids.values()].flatMap(termNodeRows)];
}

// How many sessions are under a heading, at any depth. A folded group has to
// say what is inside it, or folding loses information rather than hiding it.
function termCount(node) {
  let n = node.rows.length;
  for (const kid of node.kids.values()) n += termCount(kid);
  return n;
}

// `termPinBreak` stood here. It drew `pinned` and `the rest` across the strip,
// and `termGroupsHTML` replaced it: once the list has headings for where a
// session lives, a second kind of heading for whether it is pinned is two
// systems competing for the same top level. Pinned is a group now. The stack
// still has its own `.pinbreak`, which is the same idea in a list that has no
// other headings in it.

// Sort the strip in place before grouping or rendering. Keeping this separate
// lets test-sort-order.js check stability across different input orders.
function termOrder(tasks) {
  if (termSortStarted) {
    // NEWEST CARD FIRST, whatever the card is doing. The card id breaks a tie, so two polls with the same data
    // give the same order. Grouping keeps this order inside each heading.
    tasks.sort((a, b) => (b.created_at || "").localeCompare(a.created_at || "") ||
      (a.id || "").localeCompare(b.id || ""));
  } else if (sortByActivity) {
    // WORKING NOW SITS ON TOP. "Sorted by activity" is about which sessions are
    // doing something, so the live signal leads: a card running a tool or
    // thinking (`workingNow`, the same badge the mark animates) outranks one
    // that is idle or waiting. This used to sort on `idle_seconds` alone, which
    // is a stored gap since the last event and not the same thing: a session
    // grinding through a long build logs nothing for a stretch and so read as
    // idle and sank, which is exactly backwards. Under it, anything waiting on
    // you is lifted next so a "wants you" card is not buried under idle rows,
    // and then most-recent activity breaks the rest. The shared tiebreak keeps
    // equal values from reshuffling between two polls that said the same thing.
    tasks.sort((a, b) => {
      const act = (workingNow(b) ? 1 : 0) - (workingNow(a) ? 1 : 0);
      if (act) return act;
      const w = (isWaiting(b) ? 1 : 0) - (isWaiting(a) ? 1 : 0);
      if (w) return w;
      return cardActivityCmp(a, b) || cardTieBreak(a, b);
    });
  } else {
    // Sort by the name the row leads with, which is `display_title` and then
    // the address when the card has no name of its own: the same key `termRow`
    // draws first and the one the board and the stack sort on. This sorted on
    // the address alone, so a list of named cards came out in the order of the
    // dim second line under each name. Grouping preserves this order within
    // headings.
    const named = t => String(t.display_title || "").trim() || terminalLabel(t) || "";
    tasks.sort((a, b) => named(a).localeCompare(named(b)) || cardTieBreak(a, b));
  }
  // Lift pinned rows with a stable second pass to preserve the order within
  // pinned and unpinned groups.
  //
  // WITHIN the pinned set the order is the one somebody dragged, and neither
  // sort above gets a say. That is the difference between a bucket and a
  // filter: activity and name both move on their own, so a set arranged by
  // hand and then sorted by either is a set that rearranges itself while you
  // are not looking. `rank` is what every view now uses to order the pinned
  // bucket (see the pinned-order-fix on the board, and `SetPinOrder` in the
  // store: the drop writes both fields and every view reads rank). Two pins
  // still at their creation rank stay tied and fall through to the sort
  // underneath, so an untouched board looks exactly as it did.
  tasks.sort((a, b) => (b.pinned ? 1 : 0) - (a.pinned ? 1 : 0) ||
    (a.pinned && b.pinned ? (a.rank || 0) - (b.rank || 0) : 0));
  return tasks;
}

// The line under the title bar, in words: `5 shown · activity · by project`. The chips say what is in the five.
function termSummary(show, n) {
  const what = n + " shown";
  const g = typeof groupingPrefs === "function" ? groupingPrefs("terms") : { on: false };
  return [what, termSortStarted ? "started" : sortByActivity ? "activity" : "name", groupWords(g)].join(" \u00b7 ");
}

async function renderTermList() {
  // Cheap and idempotent, and it means the mode survives a reload without
  // finding a second place to call it from.
  applyTermList();
  let all = [];
  const readFrom = Date.now();
  try { all = await boardCards(); } catch (e) { return; }
  // The header's count, from the list that just loaded. The terminals view is
  // the one you are most likely to be on while something is working, and it is
  // the view that does not call `renderBoard`.
  if (typeof paintWorking === "function") paintWorking(all);
  // THE NEW-CONTEXT BAR FOLLOWS THE CARD. It was painted only when the pane opened, so a cycle that moved on, or
  // ended, left the bar saying "1/3: waiting for atrium ready" until the next switch.
  const open = termTask && all.find(t => t.id === termTask.id);
  if (open) {
    if (typeof composeFromList === "function") composeFromList(open, readFrom); else termTask.new_context = open.new_context;
    if (typeof composeBar === "function") composeBar(termTask);
  }
  // AND THE BOARD'S COPY OF THE LIST. `renderBoard` and `renderStack` both set
  // this and only one of the three runs per poll, so on the terminals view it
  // was whatever the last visit to another view left behind. The alert for a
  // card arriving reads it, and a stale list means a card that turned up while
  // you were watching a terminal is announced whenever you next look at the
  // board, or not at all.
  lastTasks = all; tasksLoaded = true;
  if (typeof pullsTasksMoved === "function") pullsTasksMoved();
  if (typeof trayQuestionsMoved === "function") trayQuestionsMoved();
  if (typeof cacheRefresh === "function") cacheRefresh(all);
  newCardNote(all);
  // The bar's alias, when it changed under the open terminal. See js/alias.js.
  if (typeof followTermAlias === "function") followTermAlias(all);
  // And the "clear the line" notice, from the same list. See js/heldline.js.
  if (typeof followHeldLine === "function") followHeldLine(all);

  // Terminals, AND the pinned bucket whether or not it is running.
  //
  // These are two different lists sharing one strip, and the rule that tells
  // them apart is pinning. An unpinned row is a live terminal and nothing
  // else: it appears when a runner starts and goes when it exits, because
  // there is nothing left to switch to. A PINNED row is somewhere you keep a
  // session, and it stays through the exit, drawn cold.
  //
  // This reverses a decision recorded here, that a row which cannot be
  // switched to is a row that does nothing. The reasoning held while pinning
  // only meant "sort me first". It stops holding once pinning means "this is
  // mine and I put it here": a bucket you arranged that empties itself when
  // you quit a session is not a bucket, and putting the row back by hand is
  // the work that pinning it was supposed to save. A cold row still does
  // something, it just is not attaching: it holds its place, and clicking it
  // starts the session again in the same directory, onto the same card.
  // A CARD FROM A ROOM THAT IS NOT ANSWERING IS NOT A TERMINAL ROW.
  //
  // It cannot be attached to, and a pinned one would otherwise sit here as a
  // cold row whose whole purpose is to start the session again on a machine
  // that is not there. `supervised` never survives the cache, so the live rows
  // were already safe; this is about the pinned ones.
  // A live joined card (see `termJoined`) is listed too, unattachable. A parked
  // or exited one is cold, so only a pin keeps it here.
  // WHICH ROWS THE LIST CAN HOLD AT ALL is `termRowKind`: live ones always, a stopped card or a pinned row whose runner
  // is gone only while its chip is on, a closed card (`atrium:closed`, nothing left to resume) only while it is the
  // attached one. The attached one stays whatever is off, so turning a chip off never pulls the pane out from under you.
  const show = termShow();
  const pool = all.filter(t => !t.offline);
  const tasks = pool.filter(t => termListed(t, show));
  // The badge counts what is actually attachable, since it is a count of live terminals rather than of rows. Over the
  // whole pool, whatever the chips say: the nav badge is a fact about the board, not about how this screen reads it.
  badge("c-term", pool.filter(t => termRowKind(t) === "live" && t.supervised).length);

  // The attached session is gone, so the pane showing it is stale.
  //
  // Driven off the poll rather than off the socket closing. A socket close is
  // one event that can be missed: a tab in the background, a daemon that went
  // away, a close the browser never delivered. This asks the daemon what is
  // actually running, which is the same question the list is already asking.
  // clearTermPane rather than closeTerm, since closeTerm refreshes and this is
  // running inside a refresh.
  //
  // ATTACHABLE, not merely present. A pinned row outlives its runner now, so
  // testing that the id is still in the list would leave the pane holding a
  // dead terminal for as long as the row stayed pinned, which is forever.
  //
  // NOT WHILE AN ATTACH IS IN FLIGHT FOR IT. The cached list drops `supervised`
  // before the single-card poll does, so a card being attached right now reads
  // as stale here for a beat. Tearing it down on that lag is what let a failed
  // attach spin the board: teardown -> reattach -> openTerm -> refresh -> back
  // here. The socket's own retry (or the ended state) settles the card. A render
  // must not. See `attachInFlight`.
  //
  // A ROOM-SET CHANGE FLIPS THE ID; IT DOES NOT REMOVE THE CARD. The aggregate
  // view tags a card `room~id` while more than one room is attached and serves
  // it bare with one (see `bareId` and the hub's splitTag). A room joining or
  // leaving flips every id in `/v1/tasks` at once, but the attached `termTask`
  // still holds the id from before the flip. Comparing raw ids read the attached
  // card as gone and tore the pane down, the single-card endpoint still resolved
  // the stale tagged id so the watchdog re-attached, and the next poll tore it
  // down again: the infinite teardown/reattach loop a room-set change spun,
  // which kept going after the second room had already left because the browser
  // held the tagged id it remembered while there were two. So the attached card
  // is matched by its BARE id here, and when the tag flipped the remembered id
  // is re-resolved to the form the hub serves now rather than looped on.
  reconcileAttached(tasks);

  // THE SEARCH narrows what is drawn, EXCEPT THE ATTACHED ROW, which stays whatever is typed (`fdAttached`), as it does
  // whatever chip is off. The chips decided `tasks`; this decides `shown`.
  const q = fdSearchText("terms");
  const shown = q ? tasks.filter(t => fdAttached(t) || cardMatchesText(t, q)) : tasks;
  termOrder(shown);
  // Spawned cards move under their parent row, so the lists below hold top rows only. See `termNest`.
  const nest = termNest(shown);
  termKids = nest.kids;
  termKidsPool = termNest(all.filter(t => !t.offline)).kids;
  const top = shown.filter(t => !nest.nested.has(t.id));
  // What each heading would hold with nothing searched away, so it can say `7/15`.
  const nestAll = q ? termNest(tasks) : nest;
  termTotals = termCountTotals(tasks.filter(t => !nestAll.nested.has(t.id) && (!t.pinned || termFiled(t))));
  // Before anything is drawn, and over the rows that will BE drawn: a card
  // filtered out above cannot be confused with one on screen.
  termNoteDuplicates(shown);
  const pinnedTasks = top.filter(t => t.pinned);
  pinnedNow = pinnedTasks.map(t => t.id);

  const host = document.getElementById("term-list");
  // The phone's collapsed switcher, hidden by CSS on a desktop and while nothing
  // is attached. It names the attached session and opens the list over the
  // terminal. See `termDropHTML`.
  // THE ROWS IN A BOX OF THEIR OWN. `.termscroll` is the box that scrolls. The filter line is not in here: it is the
  // drawer under the title bar (js/filter-drawer.js), over the list rather than in it.
  //
  // THE TRIGGER STAYS IN FLOW; THE BODY CAN FLOAT. On a phone the trigger is the
  // one row you always see and `.termbody` is what the caret opens OVER the
  // terminal, so opening the switcher never resizes the terminal under it. On a
  // desktop `.termbody` is `display: contents` and the tray and the rows sit in
  // the list directly. The board card, its shape and class, is unchanged:
  // this list is a switcher, and what it drops (the status chip, the duration)
  // it drops because the board says those better.
  // THE ENDED GROUP IS THE DEAD CHIP'S TOO: with it off the group is not drawn, and its count is the chip's.
  const endedAll = pool.filter(termEnded);
  const ended = show.has("dead") ? (q ? endedAll.filter(t => cardMatchesText(t, q)) : endedAll) : [];
  const deadCold = pool.filter(t => termRowKind(t) === "dead");
  const nestPool = termNest(tasks);
  const counts = {
    paused: pool.filter(t => termRowKind(t) === "paused").length,
    dead: deadCold.length + endedAll.length,
    subagents: nestPool.nested.size,
    minions: pool.filter(isMinion).length
  };
  pillCounts("term-show", counts);
  const listed = shown.length + ended.length;
  fdStatus("terms", {
    summary: termSummary(show, shown.length + ended.length),
    match: q && listed !== tasks.length + (show.has("dead") ? endedAll.length : 0)
      ? `${listed} of ${tasks.length + (show.has("dead") ? endedAll.length : 0)}` : ""
  });
  // Pinned rows the dead chip keeps out of the bucket, so an empty bucket can say why.
  const deadPinned = show.has("dead") ? 0 : deadCold.length;
  setHTML(host, tasks.length || ended.length || endedAll.length || deadCold.length
    ? termDropHTML() + `<div class="termbody">` +
      `<div class="termscroll">` +
      termBucketHTML(pinnedTasks, tasks.filter(t => t.pinned).length,
        termFolded().has(PINNED_FOLD), deadPinned) +
      termGroupsHTML(top.filter(t => !t.pinned || termFiled(t))) +
      termEndedHTML(ended) +
      `</div><div class="tlcorner">${termListButtons()}</div></div>`
    : `<div class="panel"><div class="empty">
         no terminals. start one from the board, or attach to a running session.
       </div></div>`);
  // A shut heading stands in for the attached row only while no copy of that row is drawn.
  if (host.querySelector(".card.on")) host.querySelectorAll(".tgroup.carries").forEach(h => h.classList.remove("carries"));

  // After the cards exist, since it is placed against one of them. A frame
  // later, so the measurement happens on the layout that was just written
  // rather than on the one being replaced.
  requestAnimationFrame(placeTabBridge);
  // Scrolling the rows moves the attached card under a bridge that is not
  // moving with it, and so does the tray rolling up or down. A capturing
  // listener on the host hears the scroll of `.termscroll` inside it, and
  // `transitionend` bubbles up from the tray. Guarded so a refresh does not
  // stack listeners the way the terminal's paste handler did.
  if (!host.dataset.bridged) {
    host.dataset.bridged = "1";
    host.addEventListener("scroll", placeTabBridge, { passive: true, capture: true });
    host.addEventListener("transitionend", placeTabBridge);
  }
  wireTermDrag(host);
  // After the host is replaced, since `setHTML` above threw away the div these
  // buttons live in. Guarded because the strip draws an empty state with no
  // header at all, and the board's painter writes into whichever of its three
  // hosts it finds.
  if (typeof paintGroupSegs === "function") paintGroupSegs();
  // The gear's terminal list section follows the list.
  paintTermGear();
  // A link still being opened sits at the top of the list too (js/link-opening.js).
  if (typeof linkOpeningPaint === "function") linkOpeningPaint();
}

// ── the ended cards ──────────────────────────────────────────────────────────
//
// A CARD WHOSE SESSION ENDED STAYS FINDABLE. The strip lists live terminals and pinned ones, so a worker that
// reported done and left (told to stay open, or not) fell out of it and was found only when a say to it came back
// undeliverable. Per room there is a collapsed `ended` group under the rows: its count in the heading, closed by
// default, and each row says what it was, when it ended, how, and offers the one thing worth doing: resume.
//
// ENDED IS `done` OR `dead` WITH NO RUNNER, and nothing the strip already shows: a pinned card stays in its bucket,
// a stopped one has the paused filter, a closed one has no folder to resume into. It ignores the two hide toggles,
// since those are about the live rows and a worker's husk is exactly what the group is for.
//
// HOW IT ENDED comes from what the card already holds: the commit a done report named, the `needs:` line a blocked
// report leaves in the recap, any other recap, and otherwise the status (dead is a crash, done with no report an exit).
//
// OPEN IS PER DEVICE like the strip's other view settings, kept as the list of rooms whose group is open.
const TERM_ENDED_MAX = 30;
const termEndedMore = new Set();

function termEnded(t) {
  return !!t && !t.offline && !t.supervised && !t.pinned && !t.parked_at && !termClosedGone(t) &&
    (t.status === "done" || t.status === "dead");
}

function termEndedOpen() {
  try { return new Set(JSON.parse(localStorage.getItem(termDeviceKey(TERM_ENDED_KEY)) || "[]")); }
  catch (e) { return new Set(); }
}

function toggleTermEnded(room) {
  const open = termEndedOpen();
  if (open.has(room)) open.delete(room); else open.add(room);
  try { localStorage.setItem(termDeviceKey(TERM_ENDED_KEY), JSON.stringify([...open])); } catch (e) {}
  renderTermList();
}

function termEndedAt(t) { return Date.parse(t.last_activity_at || t.reported_at || "") || 0; }

// How it ended, as the one line a row shows.
function termEndedHow(t) {
  const recap = String(t.recap || "");
  const needs = /(?:^|\n)needs: (.*)/.exec(recap);
  if (t.report_sha) return "done " + String(t.report_sha).slice(0, 8);
  if (needs) return "blocked: " + needs[1].trim();
  const first = recap.split("\n").map(s => s.trim()).find(Boolean);
  if (first) return "report: " + first;
  return t.status === "dead" ? "crashed" : "exited";
}

function termEndedRow(t) {
  const at = termEndedAt(t);
  const when = at ? ago((Date.now() - at) / 1000) + " ago" : "";
  const how = termEndedHow(t);
  const id = String(t.id).replace(/\\/g, "\\\\").replace(/'/g, "\\'");
  return `<div class="endedrow" data-ended-id="${esc(t.id)}">
      <div class="endedmain">
        <span class="endedtitle" data-tip="${esc(t.display_title || t.title || t.id)}">${esc(t.display_title || t.title || t.id)}</span>
        <span class="endedhow" data-tip="${esc(how)}">${esc(how)}</span>
      </div>
      <span class="endedwhen">${esc(when)}</span>
    </div>`;
}

function termEndedHTML(ended) {
  if (!ended.length) return "";
  const rooms = new Map();
  for (const t of ended) {
    const r = t.room || "";
    if (!rooms.has(r)) rooms.set(r, []);
    rooms.get(r).push(t);
  }
  const open = termEndedOpen();
  return [...rooms.entries()].map(([room, list]) => {
    list.sort((a, b) => termEndedAt(b) - termEndedAt(a));
    const isOpen = open.has(room);
    const all = termEndedMore.has(room);
    const rows = isOpen ? list.slice(0, all ? list.length : TERM_ENDED_MAX).map(termEndedRow).join("") : "";
    const more = isOpen && !all && list.length > TERM_ENDED_MAX
      ? `<button class="endedmore" onclick="termEndedMore.add('${esc(room)}');renderTermList()">show older (${list.length - TERM_ENDED_MAX})</button>`
      : "";
    const name = rooms.size > 1 || room ? "ended · " + (room || "this room") : "ended";
    return `<div class="termended${isOpen ? " open" : ""}" data-ended-room="${esc(room)}">
      <button class="tgroup endedhead" onclick="toggleTermEnded('${esc(room.replace(/'/g, "\\'"))}')"
        data-tip="${isOpen ? "hide" : "show"} the cards whose session ended"
        ><span class="tcaret">${isOpen ? "&#9662;" : "&#9656;"}</span
        ><span class="tgname">${esc(name)}</span
        ><span class="tgcount">${list.length}</span></button>
      ${rows}${more}
    </div>`;
  }).join("");
}

// The fold key for the pinned bucket.
//
// Held in the same store as the tree's folds, which is keyed by path. `*` is
// not a legal path segment anywhere the tree builds keys from, so this cannot
// collide with a group called `pinned`.
const PINNED_FOLD = "*pinned";

// The pinned ids in the order the strip last drew them.
//
// Needed because a FOLDED bucket draws no rows, so a drop onto it cannot read
// the order off the DOM the way an open one does. Without this the drop would
// send a one-id list and renumber that card to the front of a bucket it was
// meant to land at the back of.
let pinnedNow = [];

// The pinned bucket: a heading, and the rows in the order somebody put them.
//
// FLAT, AND NOT THROUGH THE TREE, which is the one thing that makes it a
// bucket. The tree exists to lift a shared `github/org/` prefix off a column
// of rows that all repeat it, and it is right for the rows underneath. It is
// wrong here: an order arranged by hand and then sorted into folders is not
// the order any more, and the whole promise of the bucket is that a row stays
// where it was put.
//
// So this is the opposite trade from the rows below, taken knowingly. These
// rows draw their FULL label, prefix and all, because there is no heading
// above them to carry it. That is more text per row and it is the price of the
// row not moving.
//
// The heading is drawn even when the bucket is empty, because an empty bucket
// still has to be a place you can drag the first row into. It says so.
//
// IT FOLDS, like every other heading in this strip. It was written as a label
// on the reasoning that the bucket is where you put things so you can see
// them, so a control that hides it only exists to undo the feature. That was
// wrong in the case that actually happens: eight pinned rows is most of the
// strip, and folding them is how you get at the ones underneath without
// unpinning anything. The count on the heading is what makes a folded group
// honest, and it is already there.
//
// A FOLDED BUCKET STILL TAKES A DROP. It is a fold, not a lid: a row dragged
// onto it goes on the end, which is the same thing dropping past the last row
// does when it is open.
//
// SHOWN OUT OF HOW MANY, like every group heading (see `termHeading`). The search
// reaches into the bucket, so `total` is every pinned row listed and the
// count reads `5/15` while any are hidden. A bucket whose rows are all hidden
// says so rather than inviting a first drag.
function termBucketHTML(pinned, total, folded, deadHidden) {
  const rows = folded ? "" : pinned.map(t => termRow(t, false)).join("");
  const hidden = total - pinned.length;
  const empty = folded ? "" : hidden > 0
    ? `<div class="empty bucketdrop">${hidden} hidden by the search</div>`
    : deadHidden > 0
    ? `<div class="empty bucketdrop">${deadHidden} dead, behind the dead filter. drag a terminal here to keep it</div>`
    : `<div class="empty bucketdrop">drag a terminal here to keep it</div>`;
  const count = hidden > 0 ? `${pinned.length}/${total}` : `${pinned.length}`;
  const tip = hidden > 0 ? ` (${hidden} hidden by the search)` : "";
  return `<div class="termbucket${folded ? " folded" : ""}" data-bucket="1">
      <button class="tgroup pinnedhead${folded && pinned.some(termHoldsOn) ? " carries" : ""}" onclick="toggleTermGroup('${PINNED_FOLD}')"
        data-tip="${folded ? "show" : "hide"} the pinned terminals${tip}"
        ><span class="tcaret">${folded ? "&#9656;" : "&#9662;"}</span
        ><span class="tgname">pinned</span
        ><span class="tgcount">${count}</span></button>
      ${rows || empty}
    </div>`;
}

// Where a drag started, held because `dataTransfer` cannot be read during
// `dragover` and the drop indicator has to be drawn while the pointer moves.
let termDragID = "";
// And the row itself. A card in two of your groups is drawn twice with one id,
// so the id alone cannot say which of the two is being moved.
let termDragEl = null;

// Dragging a row into the bucket, or around inside it.
//
// TWO INTENTIONS, ONE GESTURE, told apart by where the row came from. A row
// dragged in from below is being pinned, and where it lands is also where it
// goes in the order. A row dragged within the bucket is only being moved. Both
// end in the same write, so they are one handler.
//
// The DOM is reordered on the drop and the order read back off it, rather than
// computed from the model and re-rendered. The list is rebuilt from the daemon
// on the next poll anyway, so computing it twice is two chances to disagree,
// and reading the rows is what the operator can actually see.
//
// Wired after every render, guarded on the host, because `renderTermList`
// replaces the whole strip and an unguarded listener per render is the leak
// the terminal's paste handler already taught this file about.
function wireTermDrag(host) {
  host.querySelectorAll(".card.tab:not(.kid)").forEach(el => {
    el.ondragstart = e => {
      termDragID = el.dataset.id;
      termDragEl = el;
      e.dataTransfer.effectAllowed = "move";
      // Firefox refuses to start a drag with nothing set.
      e.dataTransfer.setData("text/plain", termDragID);
      e.dataTransfer.setData("application/x-atrium-card", termDragID);
    };
    el.ondragend = () => {
      const aborted = !!termDragID;
      termDragID = "";
      termDragEl = null;
      host.querySelectorAll(".dropover").forEach(x => x.classList.remove("dropover"));
      // AN ABANDONED DRAG PUTS THE STRIP BACK. `dragover` moves the row as the
      // pointer travels, so letting go outside the bucket leaves the list
      // showing an order that was never written. The poll would correct it
      // within five seconds, which is five seconds of the strip claiming
      // something untrue about where things are.
      //
      // Told apart from a completed drag by the id: `ondrop` runs first and
      // clears it, so anything still set here never reached a drop.
      if (aborted) repaintTermList();
    };
  });

  host.querySelectorAll(".tnest[data-group]").forEach(wireGroupDrop);
  host.querySelectorAll(".tgroup[data-ghead]").forEach(el => wireHeadDrag(host, el));
  host.querySelectorAll(".tnest[data-ungroup]").forEach(wireUngroupDrop);

  const bucket = host.querySelector(".termbucket");
  if (!bucket) return;

  bucket.ondragover = e => {
    if (!termDragID) return;
    // Without this the browser refuses the drop and the gesture ends in the
    // page's own "no" cursor.
    e.preventDefault();
    e.dataTransfer.dropEffect = "move";
    bucket.classList.add("dropover");
    const over = e.target.closest(".card.tab:not(.kid)");
    const moving = host.querySelector(`.card.tab[data-id="${termDragID}"]`);
    if (!moving || !over || over === moving) return;
    // Above or below the row under the pointer, decided by which half of it
    // the pointer is in. Snapping to one side would make the last position in
    // the bucket unreachable.
    const box = over.getBoundingClientRect();
    const after = e.clientY > (box.top + box.height / 2);
    bucket.insertBefore(moving, after ? over.nextSibling : over);
  };
  bucket.ondragleave = e => {
    if (!bucket.contains(e.relatedTarget)) bucket.classList.remove("dropover");
  };
  bucket.ondrop = async e => {
    e.preventDefault();
    bucket.classList.remove("dropover");
    const id = termDragID;
    termDragID = "";
    if (!id) return;
    const moving = host.querySelector(`.card.tab[data-id="${id}"]`);
    // Dropped on the bucket but never dragged over a row in it, which is what
    // dropping onto the heading or onto an empty bucket looks like. It goes on
    // the end.
    if (moving && !bucket.contains(moving)) bucket.appendChild(moving);
    // Off the DOM when the bucket is open, because the rows are what the
    // operator can see and computing the same answer twice is two chances to
    // disagree. A FOLDED bucket has no rows to read, so the last drawn order
    // stands and the dropped card goes on the end.
    const ids = bucket.classList.contains("folded")
      ? pinnedNow.filter(x => x !== id).concat(id)
      : [...bucket.querySelectorAll(".card.tab:not(.kid)")].map(x => x.dataset.id);
    // Bare, because the ids go in the BODY and the hub only untags the path.
    // A tagged id matches no row in the room's store and the order is lost.
    const bare = ids.map(bareId);
    // Painted from the held map at once, the two writes behind it. A refusal puts the pin back.
    const wasPinned = !!(cardRows.get(id) || {}).pinned;
    setPinLocal(id, true);
    host.__paintedFrom = null;
    try {
      // Pinned FIRST, and only then ordered. A card that was dragged in from
      // below is not in the bucket as far as the daemon is concerned, so an
      // order written before the pin would be an order over a set it is not
      // in yet, and the next poll would drop it back out.
      await api(`/v1/tasks/${id}`, {
        method: "PATCH", headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ pinned: true })
      });
      await api("/v1/tasks/pin-order", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ ids: bare })
      });
    } catch (err) {
      toast("that order did not stick", err.message);
      setPinLocal(id, wasPinned);
    }
    host.__paintedFrom = null;
    refresh();
  };
}

// One of your groups as a drop target, under any sort.
//
// A row dragged in from outside is FILED: it gets the group's tag, the same as
// `into group` on its menu, and the strip's sort places it. It keeps any group
// it was already in, because a group is a tag and a card can carry several.
function wireGroupDrop(nest) {
  const name = nest.dataset.group;
  nest.ondragover = e => {
    if (termHeadDrag) return headDragOver(e, name);
    if (!termDragEl) return;
    e.preventDefault();
    e.dataTransfer.dropEffect = "move";
    nest.classList.add("dropover");
  };
  nest.ondragleave = e => {
    if (!nest.contains(e.relatedTarget)) nest.classList.remove("dropover");
  };
  nest.ondrop = async e => {
    if (termHeadDrag) return headDrop(e);
    e.preventDefault();
    // The pinned bucket has a drop of its own and this one has already done it.
    e.stopPropagation();
    nest.classList.remove("dropover");
    const el = termDragEl;
    termDragEl = null;
    termDragID = "";
    if (!el) return;
    const id = el.dataset.id;
    const t = lastTasks.find(x => x.id === id);
    const tags = (t && t.tags) || [];
    if (!tags.includes(name)) await patchTask(id, { tags: tags.concat(name) });
    repaintTermList();
  };
}

// `untagged` as a drop target, in custom mode. A row dragged here from one of
// your groups leaves THAT group: its tag comes off, the same as `out of` on the
// card's menu. Only the group it was dragged from, because a card in two groups
// dragged out of one of them has said nothing about the other. A row that was
// not in a group has nothing to leave, and the drop puts the strip back.
function wireUngroupDrop(nest) {
  nest.ondragover = e => {
    if (!termDragEl || !termDragEl.closest(".tnest[data-group]")) return;
    e.preventDefault();
    e.dataTransfer.dropEffect = "move";
    nest.classList.add("dropover");
  };
  nest.ondragleave = e => {
    if (!nest.contains(e.relatedTarget)) nest.classList.remove("dropover");
  };
  nest.ondrop = async e => {
    e.preventDefault();
    e.stopPropagation();
    nest.classList.remove("dropover");
    const el = termDragEl;
    termDragEl = null;
    termDragID = "";
    const from = el && el.closest(".tnest[data-group]");
    if (from) {
      const t = lastTasks.find(x => x.id === el.dataset.id);
      const tags = (t && t.tags) || [];
      const name = from.dataset.group;
      if (tags.includes(name)) await patchTask(el.dataset.id, { tags: tags.filter(x => x !== name) });
    }
    repaintTermList();
  };
}

// The name of the group whose HEADING is being dragged, or "".
//
// Its own variable, apart from `termDragID` and `termDragEl`, and that is what
// keeps the two drags from being mistaken for each other. Every row target asks
// for a row and every heading target asks for a heading, so a heading dropped
// on the pinned bucket is refused and a row dropped on a heading is not a
// reorder. Pinned stays on top because nothing here can reach the bucket.
let termHeadDrag = "";

// A heading of yours, dragged to reorder the groups. The whole group moves: the
// heading and the nest under it travel together while the pointer does, and
// the rows inside keep their order. The drop writes `p.groups`, the same list
// `move up` and `move down` write, so the stack and the board follow.
function wireHeadDrag(host, el) {
  el.ondragstart = e => {
    termHeadDrag = el.dataset.ghead;
    e.dataTransfer.effectAllowed = "move";
    e.dataTransfer.setData("text/plain", termHeadDrag);
    e.dataTransfer.setData("application/x-atrium-card", termHeadDrag);
    el.classList.add("dragging");
  };
  el.ondragend = () => {
    const aborted = !!termHeadDrag;
    termHeadDrag = "";
    el.classList.remove("dragging");
    host.querySelectorAll(".dropover").forEach(x => x.classList.remove("dropover"));
    // Let go anywhere but a heading or a group, and the strip goes back to the
    // order that was written, for the reason an abandoned row drag does.
    if (aborted) repaintTermList();
  };
  el.ondragover = e => { if (termHeadDrag) headDragOver(e, el.dataset.ghead); };
  el.ondrop = e => { if (termHeadDrag) headDrop(e); };
}

// A group's heading and, when it is open, the nest drawn under it.
function headBlock(name) {
  const host = document.getElementById("term-list");
  const q = CSS.escape(name);
  return {
    head: host.querySelector(`.tgroup[data-ghead="${q}"]`),
    nest: host.querySelector(`.tnest[data-group="${q}"]`)
  };
}

// Over another group: the dragged one goes above it or below it, by which half
// of that group's block the pointer is in, the same rule the rows use.
function headDragOver(e, over) {
  e.preventDefault();
  e.dataTransfer.dropEffect = "move";
  if (!over || over === termHeadDrag) return;
  const from = headBlock(termHeadDrag), to = headBlock(over);
  if (!from.head || !to.head) return;
  const top = to.head.getBoundingClientRect().top;
  const bottom = (to.nest || to.head).getBoundingClientRect().bottom;
  const after = e.clientY > (top + bottom) / 2;
  const parent = to.head.parentNode;
  const anchor = after ? (to.nest || to.head).nextSibling : to.head;
  if (anchor === from.head) return;
  parent.insertBefore(from.head, anchor);
  if (from.nest) parent.insertBefore(from.nest, from.head.nextSibling);
}

// The order is read off the headings the strip now shows, rather than worked
// out from where the pointer was, because what the operator can see is the
// answer. A group in the list that is not drawn keeps its place at the end.
function headDrop(e) {
  e.preventDefault();
  e.stopPropagation();
  const name = termHeadDrag;
  termHeadDrag = "";
  if (!name) return;
  const host = document.getElementById("term-list");
  const drawn = [...host.querySelectorAll(".tgroup[data-ghead]")].map(b => b.dataset.ghead);
  const had = Array.isArray(groupingPrefs().groups) ? groupingPrefs().groups : [];
  const groups = drawn.filter(n => had.includes(n)).concat(had.filter(n => !drawn.includes(n)));
  host.__paintedFrom = null;
  if (groups.join("\n") === had.join("\n")) { renderTermList(); return; }
  setGrouping({ groups });
}

// How long to keep trying to attach to a runner that is not there YET.
//
// Launching into its own window opens the window as soon as `/v1/launch`
// answers, and the runner is registered with the supervisor a moment later. The
// attach in between is refused, the socket closes without ever opening, and the
// close handler below tore the pane down: a window whose whole purpose is one
// terminal, showing "nothing attached".
//
// A socket that NEVER OPENED and one that closed after being open are different
// events and take different paths.
//
// The second case then splits again, and this is the part that matters when
// atrium restarts itself. A socket that was open and closed means either the
// RUNNER went away or the DAEMON did, and those want opposite behavior: a
// runner that exited should tear the pane down, and a daemon that is coming
// back should be waited for. Nothing in the close event says which.
//
// So the daemon is asked. `/v1/health` answering means the daemon is up and
// this was the runner; a failed request means the daemon is the thing missing.
// One extra request, only on a disconnect, and it is the difference between a
// terminal that reconnects itself after `restart_atrium` and one that clears
// itself two seconds before the daemon comes back.
const attachRetryFor = 5 * 60 * 1000;
const attachRetryEvery = 700;
// The retry delay GROWS and is capped, rather than hammering at a flat rate. A
// card whose attach keeps closing before it opens is either mid-restart or gone
// for good, and neither wants a fixed 700ms forever: the first recovers in a
// beat or two, the second should back off toward the cap so a dead card costs a
// handful of attempts a minute, not one every 700ms. Reset when the socket
// opens so the next outage starts from the short delay again.
const attachRetryMax = 8000;
let attachTries = 0;
function attachRetryDelay() {
  const d = Math.min(attachRetryMax, attachRetryEvery * Math.pow(2, attachTries));
  attachTries++;
  return d;
}
// How long a refused attach stays silent before it says it is waiting.
//
// `/v1/launch` returns before the supervisor has registered the runner, and
// that gap is well under a second. Anything past this is a wait worth naming.
const attachRaceFor = 4000;
let attachSince = 0;
// Said once per outage rather than once per attempt. At 700ms a five minute
// wait is four hundred lines of the same sentence.
let attachSaidGone = false;

// THE CARD AN ATTACH IS IN FLIGHT FOR, and the guard that stops a render from
// spinning the whole board.
//
// The failure this closes seized the screen. An attach whose socket closes
// before it opens, together with a task LIST that lags the live card (the
// cached list drops `supervised` while the single-card poll still has it, see
// `renderTermList`), used to loop: the render found the pane stale, tore it
// down, the watchdog re-attached, and re-attaching ran openTerm -> switchView
// -> refresh -> renderTermList, which tore it down again. Hundreds of times a
// second, a new WebGL context each pass, until the tab ran out of them.
//
// So the card openTerm has committed to is recorded from the moment it commits
// until the socket opens or the attempt is abandoned. While it is in flight a
// render must not tear the pane down and the watchdog must not start a second
// attach for it: either one re-enters the loop above. Set in `openTerm`,
// cleared when the socket opens and on any teardown.
// COMPARED BY BARE ID, so a room-set change that flips the attached card
// between `room~id` and bare does not strand the in-flight mark on the old
// spelling. `onopen` clears with the id the socket was opened under, and a
// render may retag `termTask` to the new form before the socket lands (see
// `retagTermId`), so raw equality would leave the mark stuck and the pane
// unclearable. The bare id is the same across the flip. See `bareId`.
let attachInFlight = "";
function markAttachInFlight(card) { attachInFlight = card || ""; }
function clearAttachInFlight(card) {
  if (!card || bareId(attachInFlight) === bareId(card)) attachInFlight = "";
}
function attachIsInFlight(card) {
  return !!card && !!attachInFlight && bareId(attachInFlight) === bareId(card);
}

// Re-point the attached card at the id the hub serves NOW, after a room-set
// change flipped it between `room~id` and bare. The two ids are the same card
// (same bare id) so the socket stays open and nothing reattaches: only the id
// the board remembers it by changes, so the strip's `on` row, the pane check
// and the reload slot all speak the current form again instead of a stale tag
// the single-card endpoint happens to still resolve. The in-flight and reattach
// marks are left alone: they compare by bare id and so already match either
// spelling. Called from `renderTermList` when the fresh list disagrees with
// `termTask.id` on the tag but not on the card.
// The attached pane, reconciled against the freshly polled task list.
//
// Two decisions, in this order, and the order matters: re-resolve first, tear
// down only if there is genuinely nothing left. A room-set change flips every
// id between `room~id` and bare (see `retagTermId`), so the attached card is
// found by its BARE id; when the tag flipped, the remembered id is re-pointed
// at the live form rather than read as gone. Only when no supervised row shares
// the attached card's bare id is the pane stale, and even then not while an
// attach for it is in flight (the cached list lags the single-card poll). Split
// out of `renderTermList` so the room-flip loop it closes can be tested against
// the real code. Returns whether it tore the pane down.
function reconcileAttached(tasks) {
  keepReconcile(tasks);
  fileViewPrune(tasks);
  if (!termTask) return false;
  const live = tasks.find(t => t.supervised && bareId(t.id) === bareId(termTask.id));
  if (termDeadFor) {
    if (live) openTerm(live);
    return false;
  }
  if (live && live.id !== termTask.id) {
    retagTermId(live.id);
    // The room set just changed under an attached pane, which is what flipped the
    // id. That same change moves the header's height (the room picker and chips
    // come and go), the header ResizeObserver re-fits the terminal, and a re-fit
    // that resizes the grid leaves the cursor misplaced: a viewer that is not the
    // binding one gets no SIGWINCH and so no repaint, and a claude-code TUI does
    // repaint but does not fully re-park its own input-line cursor. Neither is
    // caught by the retag above, which keeps the socket open and replays nothing.
    // So re-attach once, which is the only thing that restores the cursor the way
    // an initial attach does: `openTerm`/`onopen` resets the terminal and the
    // daemon replays through `screen.textWithCursor`. See `resyncCursorAfterFlip`.
    resyncCursorAfterFlip();
  }
  if (!attachIsInFlight(termTask.id) && !live) { clearTermPane(); return true; }
  return false;
}

// A ROOM FLIP RE-ATTACHES THE PANE ONCE, so the cursor is restored the way it is
// on a first attach. This is the fix for the garble that came back whenever a
// room attached or detached.
//
// It is deliberately the re-attach the flicker-flap work took OUT of the render
// loop, put back as a single gated event rather than a per-poll one. `retagTermId`
// keeps the socket open on a flip, which is right for everything except the
// cursor: no new socket means no replay, and the live re-fit that the flip
// triggers moves xterm's grid with nothing to re-sync the cursor against. A
// re-attach runs the daemon's replay, and that replay ends with the cursor move
// `textWithCursor` owes an attaching viewer.
//
// GATED TO ONE PENDING RECONNECT, and never in a solo window: a solo window
// carries its own reconnect path keyed off its hash, not this slot. The next
// poll sees the id already re-resolved, so this fires once per flip and cannot
// loop the way the raw-id teardown did.
let cursorResyncPending = false;
function resyncCursorAfterFlip() {
  if (termOnly() || !term || !termTask) return;
  if (cursorResyncPending) return;
  cursorResyncPending = true;
  // A frame later, so the flip's re-render and its re-fit have settled before the
  // reconnect measures and replays into the pane.
  requestAnimationFrame(() => {
    cursorResyncPending = false;
    if (term && termTask) connectTerm(termTask.id);
  });
}

// A TAB RETURN CAN REFLOW THE GRID THE SAME WAY A ROOM FLIP DOES, and it leaves
// the cursor misplaced for the same reason. clint hit this switching browser
// tabs away from an attached sg4 pane and back: the typed input garbled again,
// a different trigger from the room flip already fixed above.
//
// The mechanism is the one `cursor_refit_test.go` pins. A re-fit that changes
// cols/rows reflows xterm's buffer and moves the cursor against it, and a viewer
// that is not the binding one gets no SIGWINCH, so the runner never repaints and
// the old cursor sits against a reflowed grid. While the tab is hidden the box
// can change under it (the window resized behind it, a scrollbar came or went)
// and the layout only flushes on return, so the fit that reflows runs as the tab
// becomes visible rather than while it was away.
//
// So remember the grid on the way out, and on the way back settle the box
// through `onTermResize` (which skips a no-op fit via `paneBoxUnchanged`, so a
// return that changed nothing does nothing) and re-attach ONCE, the way a flip
// does, only when the grid actually moved. The grid-change gate is what keeps an
// ordinary glance-away-and-back from re-attaching the pane for no reason: no
// reflow, no garble, no resync. It shares `resyncCursorAfterFlip`'s one-shot
// gate, so a flip and a tab return that land together still re-attach once, and
// it is skipped in a solo window for the same reason the flip is: solo owns its
// own reconnect. HUB-ONLY: the reflow and its cure are both board-side, and the
// daemon replay it leans on is the same one an initial attach already runs.
let preHideGrid = "";
function onTabVisibility() {
  if (document.visibilityState !== "visible") {
    preHideGrid = term ? term.cols + "x" + term.rows : "";
    return;
  }
  const was = preHideGrid;
  preHideGrid = "";
  if (!was || termOnly() || !term || !termTask) return;
  // A frame later, so the return's own re-render and the ResizeObserver fit have
  // a chance to run first, then force the fit ourselves in case the observer has
  // not fired yet. `onTermResize` is idempotent and box-gated, so calling it
  // here costs nothing when the box did not move.
  requestAnimationFrame(() => {
    if (termOnly() || !term || !termTask) return;
    onTermResize();
    if (term.cols + "x" + term.rows !== was) resyncCursorAfterFlip();
  });
}
document.addEventListener("visibilitychange", onTabVisibility);

function retagTermId(id) {
  if (!termTask || !id || id === termTask.id) return;
  const was = termTask.id;
  rlog("room set changed; re-resolving", was, "to", id);
  termTask.id = id;
  // `termKindFor` keys the runner/shell choice to the attached card; move it so
  // the next open of this card does not read as a different one and reset it.
  if (typeof termKindFor !== "undefined" && termKindFor === was) termKindFor = id;
  // The reload slot, so a restart comes back to the card under the id this hub
  // now serves rather than waiting out a tag it no longer answers to. Only in
  // the board: a solo window is addressed by its hash, not this slot.
  if (!termOnly()) {
    try {
      if (localStorage.getItem("atrium.term") === was) localStorage.setItem("atrium.term", id);
    } catch (e) {}
  }
}

// A REATTACH AFTER A TEARDOWN IS SCHEDULED AND BACKS OFF, never run straight
// out of the render that noticed the pane was stale.
//
// This is the other half of not spinning. `clearTermPane` used to call
// `waitAndAttach` inline, which polls the card and calls `openTerm` the instant
// it answers, and `openTerm` repaints the board, and the repaint tears the pane
// down again. Routed through here instead, at most one reattach is ever pending,
// it will not fire while an attach for the card is already in flight, and each
// time it has to fire again for the same card without the attach settling it
// waits longer, capped. A card whose runner is genuinely gone settles onto one
// ended state rather than a retry storm.
let reattachTimer = 0, reattachCard = "", reattachTries = 0;
const reattachMin = 500;
const reattachMax = 8000;
function scheduleReattach(card) {
  if (!card) return;
  // openTerm is already on it, or a reattach for it is already queued. Either
  // way, do not stack a second one.
  if (attachIsInFlight(card)) return;
  // Bare id, for the reason `attachIsInFlight` gives: a queued reattach and a
  // teardown that arrives under the other spelling after a room flip are the
  // same card, and stacking a second one is what the guard is here to refuse.
  if (reattachTimer && bareId(reattachCard) === bareId(card)) return;
  if (reattachTimer) clearTimeout(reattachTimer);
  reattachCard = card;
  const wait = Math.min(reattachMax, reattachMin * Math.pow(2, reattachTries));
  reattachTries++;
  reattachTimer = setTimeout(() => {
    reattachTimer = 0;
    reattachCard = "";
    waitAndAttach(card);
  }, wait);
}
// Cleared when an attach settles, so the next genuine outage starts from the
// short delay again rather than from wherever a previous flap left the backoff.
function resetReattach() {
  reattachTries = 0;
  reattachCard = "";
  if (reattachTimer) { clearTimeout(reattachTimer); reattachTimer = 0; }
}

// Draw the terminal on the GPU rather than in the DOM.
//
// xterm has no renderer of its own beyond a fallback, and the fallback is the
// DOM: a `<span>` per styled run per row, rebuilt every frame, plus a
// generated stylesheet of 256 color rules. It is correct and it is enormously
// expensive. One board tab was holding a quarter of a CPU and had accumulated
// two and a half hours of CPU time doing nothing but drawing terminals.
//
// AFTER `term.open`, which is not optional: the addon needs a canvas in the
// document to get a context from, and loading it earlier fails silently and
// leaves you on the DOM renderer wondering why nothing changed.
//
// Three ways this can not happen, all of them ending on the DOM renderer,
// which is slow rather than broken:
//
//   - No WebGL at all: a remote session, a blocklisted driver, a machine with
//     no GPU worth using.
//   - The context is lost later. A driver reset or a laptop switching GPUs
//     does this, and the addon cannot recover, so it is disposed and xterm
//     falls back on its own.
//   - The addon is missing entirely, which is what a build with a stale
//     `vendor/` looks like.
//
// None of them is worth a message to the operator. A terminal that renders is
// the requirement; which path drew it is not.
function useWebgl(t) {
  t._atriumGl = null;
  if (typeof WebglAddon === "undefined") return;
  let addon;
  try {
    addon = new WebglAddon.WebglAddon();
  } catch (e) {
    console.warn("no webgl renderer, falling back to the DOM one:", e);
    return;
  }
  addon.onContextLoss(() => {
    console.warn("webgl context lost, falling back to the DOM renderer");
    addon.dispose();
    if (t._atriumGl === addon) t._atriumGl = null;
  });
  try {
    t.loadAddon(addon);
    // Held so a terminal kept out of sight can give its context back and take another on return.
    t._atriumGl = addon;
  } catch (e) {
    console.warn("webgl renderer would not load:", e);
    addon.dispose();
  }
}

// A HIDDEN TERMINAL DRAWN BY XTERM'S DOM RENDERER MUST NOT REDRAW ON SCROLL.
//
// xterm 5.5 asks its selection to redraw on every line the buffer scrolls, and the render service passes that to
// the renderer without looking at whether it is paused. The WebGL renderer only notes it. The DOM renderer redraws
// every row, and under `display: none` each cell measures zero wide, which its width cache will not keep, so every
// cell of every row forces a style recalc. A kept terminal that gave its WebGL context back, or any terminal in a
// browser that refused WebGL, streaming one line at a time, was tens of thousands of forced recalcs a second in
// animation frames, the "requestAnimationFrame handler took 147ms" that made typing lag.
//
// So while xterm says it is paused, the redraw is only remembered, and xterm's own full redraw when the terminal
// shows again draws the selection with it. Private to the vendored xterm 5.5, so a version without these fields
// gets nothing patched.
function holdHiddenRedraw(t) {
  const rs = t && t._core && t._core._renderService;
  if (!rs || typeof rs.handleSelectionChanged !== "function" || !("_isPaused" in rs) || !rs._selectionState) return;
  const real = rs.handleSelectionChanged;
  rs.handleSelectionChanged = function (start, end, columnSelectMode) {
    if (!this._isPaused) return real.call(this, start, end, columnSelectMode);
    this._selectionState.start = start;
    this._selectionState.end = end;
    this._selectionState.columnSelectMode = columnSelectMode;
    this._needsSelectionRefresh = true;
    this._needsFullRefresh = true;
  };
}

// Gives the GPU context back. The renderer falls to xterm's own, paused while the terminal is hidden, and kept from
// redrawing anyway by `holdHiddenRedraw`.
function dropWebgl(t) {
  const addon = t && t._atriumGl;
  if (!addon) return;
  t._atriumGl = null;
  try { addon.dispose(); } catch (e) {}
}

