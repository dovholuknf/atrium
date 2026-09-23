// ── being a guest ───────────────────────────────────────
//
// A LENT SESSION SERVES THIS SAME FILE, and the page had no idea.
//
// `overlay_guest.go` is an allowlist over one card: the terminal, that card,
// the icon, and health. `/v1/tasks`, `/v1/events`, `/v1/permissions`,
// `/v1/rooms` and every file endpoint answer 403 with a sentence saying so.
// The page caught all of those, carried on with what it had, and drew its own
// chrome with every list empty. Nothing leaked. It looked broken, and the
// operator opening the bare share address asked whether it was a shadow copy
// of his own board.
//
// The refusal IS the signal, so nothing new has to be served to detect it and
// a board that is not lent pays one request at boot for the answer.
//
// The daemon's own sentence is kept rather than replaced. It is the one place
// that knows what was shared, it is already written, and a second wording here
// would be a copy that goes stale.
let guestWord = "";
function isGuest() { return guestWord !== ""; }

// Resolved with the daemon's sentence when this page is a guest, and with the
// empty string otherwise. Started at boot and held, because two things ask:
// the boot block, and every affordance that would post a file.
let guestKnown = Promise.resolve("");

// `fetch` rather than `api`, because the STATUS is the question. `api` turns a
// 403 into an exception that reads the same as a network failure, and a network
// failure must not put a board into guest mode.
async function askIfGuest() {
  try {
    const res = await fetch("/v1/tasks", { cache: "no-store" });
    if (res.status !== 403) return "";
    return (await res.text()).trim() || "this link is one terminal.";
  } catch (e) { return ""; }
}

// ── painting without repainting ─────────────────────────
//
// Replace a chunk of the page, WITHOUT destroying the part of it that did not
// change.
//
// `innerHTML = html` throws away every node under the element, and a browser
// has nowhere to put the scroll position, the text selection, the focus or the
// running CSS transition of an element that no longer exists. So the view
// jumped to the top, a drag-select died mid-drag, and the board flickered.
// The board polls every five seconds AND redraws on every event, which on a
// board with sixteen live agents is constant, so scrolling down a long column
// was something you could not finish doing.
//
// Preserving the scroll offset around the swap was the previous answer here
// and it was a lie: it fights the browser every frame, does nothing for
// selection or focus, and goes wrong the moment the content above the viewport
// changes height. It is gone. Nothing is thrown away now, so there is nothing
// to put back.
//
// What happens instead is in `morphChildren`: the new markup is parsed
// detached and reconciled against what is already on the page.
//
// Returns whether anything in the DOM actually moved. Callers that wire
// listeners onto what they drew use that as their cue.
// Whether a live selection touches this element.
//
// One caller, the card menu, which asks it whether a card is being READ rather
// than pressed. It used to have a second: `setHTML` skipped a repaint that
// would have destroyed a selection, which was the best that could be done
// while a paint was an `innerHTML` assignment. The reconciler below settles it
// properly by keeping the node the selection lives in, so the stall is gone
// and this is only about the menu now.
//
// EITHER end counts. A sweep across a card overshoots its edge constantly, and
// a selection that starts on a card and ends past it is held just as hard as
// one that fits.
//
// Collapsed means a caret and nothing highlighted, which is every click on the
// page, so it is not a selection worth standing a menu down for.
function selectionTouches(el) {
  if (!el) return false;
  const sel = window.getSelection();
  if (!sel || !sel.rangeCount || sel.isCollapsed) return false;
  return el.contains(sel.anchorNode) || el.contains(sel.focusNode);
}

function setHTML(el, html) {
  if (!el) return false;
  // The string this element was last painted from. Compared rather than
  // `el.innerHTML`, which is a fresh serialization and never matches markup
  // written across several lines: the parser normalises the whitespace inside
  // a tag, so the old equality check could not fire for anything on this
  // board bigger than a single line.
  //
  // The child count guards the one way the cache can go stale, which is code
  // elsewhere assigning `innerHTML` on the same element behind this function's
  // back. It catches the realistic version of that, which is a wholesale
  // replacement or a clear.
  if (el.__paintedFrom === html && el.__paintedKids === el.childNodes.length) return false;

  // Parsed into an element of the SAME TAG, not a `<template>`, because the
  // parser's answer depends on where the markup lands: rows outside a table
  // and options outside a select are dropped on the floor.
  const shadow = document.createElement(el.tagName);
  shadow.innerHTML = html;
  const changed = morphChildren(el, shadow);
  el.__paintedFrom = html;
  el.__paintedKids = el.childNodes.length;
  return changed;
}

// Reconcile one element's children against a freshly parsed copy of them.
//
// TWO TIERS, AND THE SECOND IS WHAT MAKES IT HOLD.
//
//   1. THE CONTAINER IS NEVER REBUILT. Children are matched by key, so a row
//      that is still there keeps the exact node it had, wherever it moved to.
//      Scroll, selection and focus survive because nothing that held them was
//      destroyed.
//   2. A MATCHED ROW IS NOT REBUILT EITHER. It is walked, and only the
//      attributes and the text that actually differ are written. The fields
//      that tick constantly, the age and the activity chip and the status
//      class, land in the nodes that are already there.
//
// Tier one on its own swaps one bug for a quieter one. The age changes every
// second, so the card being destroyed is the card being read: the scroll
// position now survives and the selection dies anyway. That is a fix that
// looks complete and is not, which is why both tiers are here.
//
// This is deliberately generic rather than a hand-written updater per view.
// The board, the stack and the terminal switcher all paint through `setHTML`,
// as does every list in every dialog, and a per-view updater would have fixed
// one of them and left the rest to be discovered one at a time.
function morphChildren(parent, source) {
  let changed = false;

  // The keyed children already on the page, so a row that MOVED is found and
  // re-used rather than rebuilt wherever it landed.
  const keyed = new Map();
  for (let n = parent.firstChild; n; n = n.nextSibling) {
    const k = morphKey(n);
    if (k && !keyed.has(k)) keyed.set(k, n);
  }

  // `cursor` is where the next child belongs. Everything before it is done.
  let cursor = parent.firstChild;
  let want = source.firstChild;
  while (want) {
    // Read now: placing `want` moves it out of `source` and loses its sibling.
    const after = want.nextSibling;
    const k = morphKey(want);

    let mine = null;
    if (k) {
      mine = keyed.get(k) || null;
      if (mine) keyed.delete(k);
    } else if (cursor && !morphKey(cursor) &&
        cursor.nodeType === want.nodeType && cursor.nodeName === want.nodeName) {
      // Unkeyed markup matches by position and shape. That is what a heading,
      // a wrapper or a run of text is, and it is why an unkeyed list still
      // benefits from this at all.
      mine = cursor;
    }

    // The key came back on a different KIND of node: a group that used to be
    // drawn as a list, a row that became a heading. That is a replacement and
    // not a patch, and it is handled here rather than in `morphNode` because
    // `morphNode` would have to remove the node the cursor is standing on and
    // the cursor would be left pointing outside the tree.
    if (mine && (mine.nodeType !== want.nodeType || mine.nodeName !== want.nodeName)) {
      if (mine === cursor) cursor = cursor.nextSibling;
      parent.removeChild(mine);
      mine = null;
    }

    if (mine) {
      if (mine !== cursor) { parent.insertBefore(mine, cursor); changed = true; }
      if (morphNode(mine, want)) changed = true;
      cursor = mine.nextSibling;
    } else {
      // New. Adopted from the parsed copy rather than cloned, since the copy
      // is thrown away either way.
      parent.insertBefore(want, cursor);
      changed = true;
    }
    want = after;
  }

  // Whatever the cursor never reached is gone.
  while (cursor) {
    const stale = cursor;
    cursor = cursor.nextSibling;
    parent.removeChild(stale);
    changed = true;
  }
  return changed;
}

// What makes two nodes THE SAME NODE across a repaint.
//
// `data-id` is the card, which is the case that matters and the precondition
// this whole change rests on: every row on the board and in the stack already
// carried one. The rest are the containers a card sits in, and they are keyed
// for the same reason: a column or a group must not be rebuilt just because
// its contents moved.
//
// `data-morph-key` is the escape hatch for markup that has no natural id.
// Nothing without a key goes unreconciled, it just matches by position.
function morphKey(node) {
  if (!node || node.nodeType !== 1) return "";
  const d = node.dataset;
  if (!d) return "";
  if (d.morphKey !== undefined) return "m:" + d.morphKey;
  if (d.id !== undefined) return "i:" + d.id;
  if (d.column !== undefined) return "c:" + d.column;
  if (d.path !== undefined) return "p:" + d.path;
  if (d.status !== undefined) return "s:" + d.status;
  return "";
}

// One node against its new version.
//
// The two are the same KIND of node. `morphChildren` is the only caller and it
// guarantees that, because a node that has to be swapped rather than patched
// has to be swapped where the cursor can see it happen.
function morphNode(mine, want) {
  // Text and comments. Written only when the text differs, which is what keeps
  // a selection alive over a card whose age is ticking two chips away.
  if (mine.nodeType !== 1) {
    if (mine.nodeValue === want.nodeValue) return false;
    mine.nodeValue = want.nodeValue;
    return true;
  }
  let changed = morphAttrs(mine, want);
  // A subtree the board has handed to something else, a terminal or a canvas
  // or anything holding state the markup does not describe, is not the
  // board's to reconcile. Its attributes still sync, its children are left
  // alone.
  if (mine.hasAttribute("data-morph-keep")) return changed;
  if (morphChildren(mine, want)) changed = true;
  return changed;
}

// Attributes, in both directions.
//
// Attributes and not properties, on purpose. Somebody who has typed into an
// input or ticked a checkbox has made that element dirty, and the browser
// stops reflecting the attribute into the value once they have. So writing
// the attribute here updates a field nobody has touched, and leaves a
// half-finished edit exactly where it was. Both of those are what you want.
function morphAttrs(mine, want) {
  let changed = false;
  const wanted = want.attributes;
  for (let i = 0; i < wanted.length; i++) {
    const a = wanted[i];
    if (mine.getAttribute(a.name) === a.value) continue;
    mine.setAttribute(a.name, a.value);
    changed = true;
  }
  const have = mine.attributes;
  // Backwards, since removing shortens the live collection underneath us.
  for (let i = have.length - 1; i >= 0; i--) {
    const name = have[i].name;
    if (want.hasAttribute(name)) continue;
    mine.removeAttribute(name);
    changed = true;
  }
  return changed;
}

// `scrollParent` lived here. It found the ancestor whose scroll offset had to
// be saved and put back around an `innerHTML` swap, and it went with the swap.
// Nothing is destroyed now, so nothing needs finding and nothing needs putting
// back.

function ago(secs) {
  if (secs == null) return "";
  secs = Math.floor(secs);
  if (secs < 60) return secs + "s";
  // Compound d/h/m so "2h" doesn't hide the minutes -- 90061s -> "1d1h1m".
  // Zero units drop out; at least one of h/m is always present for secs >= 60.
  const d = Math.floor(secs / 86400);
  const h = Math.floor((secs % 86400) / 3600);
  const m = Math.floor((secs % 3600) / 60);
  // Past a day the minutes are noise: nothing is decided differently by
  // "1d3h17m" than by "1d3h", and the extra digits make a column of ages
  // harder to compare down.
  // Two digits on everything but the leading unit, so a column of these lines
  // up down the page. `6h9m` and `21h4m` differ by a character in the middle,
  // which is exactly where the eye is trying to compare them: `6h09m` and
  // `21h04m` put the h and the m in the same place on every row.
  //
  // Not on the FIRST unit, which would read as a clock rather than a duration
  // and pad every short age with a zero nobody asked for.
  const pad = n => String(n).padStart(2, "0");
  if (d) return d + "d" + (h ? pad(h) + "h" : "");
  if (h) return h + "h" + pad(m) + "m";
  return m + "m";
}

// Seconds since a timestamp the server sent, for the ages the server did not
// pre-compute into a field.
function sinceSecs(iso) {
  const t = new Date(iso);
  if (isNaN(t)) return null;
  return Math.max(0, (Date.now() - t.getTime()) / 1000);
}

// When a session was first seen, as a date rather than a duration.
//
// "20h4m ago" answers how stale something is. It does not answer which day it
// started, and after a night that is the question: a card carried over from
// yesterday is a different thing from one opened this morning.
function firstSeen(iso) {
  if (!iso) return "";
  const t = new Date(iso);
  if (isNaN(t)) return "";
  const now = new Date();
  const sameDay = t.toDateString() === now.toDateString();
  const pad = n => String(n).padStart(2, "0");
  // Seconds, because this column is what the stack is ORDERED by and the order
  // resolves to the second. Two rows a few seconds apart both reading `10:23`
  // looked like the sort had given up, when the sort was right and the clock
  // was rounding. Dropped for anything older than yesterday, where the second
  // something happened is not a fact anybody wants.
  const clock = `${pad(t.getHours())}:${pad(t.getMinutes())}:${pad(t.getSeconds())}`;
  if (sameDay) return `today ${clock}`;

  const yesterday = new Date(now);
  yesterday.setDate(now.getDate() - 1);
  if (t.toDateString() === yesterday.toDateString()) return `yesterday ${clock}`;

  const short = `${pad(t.getHours())}:${pad(t.getMinutes())}`;

  const MONTHS = ["jan", "feb", "mar", "apr", "may", "jun",
                  "jul", "aug", "sep", "oct", "nov", "dec"];
  const date = `${MONTHS[t.getMonth()]} ${t.getDate()}`;
  // The year only when it is not this one, since it is noise the rest of the
  // time and this board is mostly looking at the last few days.
  return t.getFullYear() === now.getFullYear()
    ? `${date} ${short}`
    : `${date} ${t.getFullYear()}`;
}

const isWaiting = (t) => t.status === "needs-input" || t.status === "needs-permission";

const VIEWS = ["board", "stack", "perms", "runners", "terms", "history", "audit"];

// BACK AND FORWARD, over the board's own moves.
//
// The board is one page that swaps views and attaches terminals, so the
// browser's back button either did nothing or left atrium entirely. Every move
// somebody makes is a place they might want to return to, and the two they
// make constantly are switching view and switching session.
//
// THE ADDRESS BAR IS NOT TOUCHED, and that is deliberate. `#term=<id>` already
// means something here: it is how a popped-out window resolves its card on
// load. Putting the view in the hash would make every reload of a solo window
// a negotiation between two meanings of the same string, which is the reason
// the view was put in `localStorage` rather than the URL in the first place.
// `pushState` carries a state object with the same URL, so the history entries
// exist and the address does not move.
//
// That keeps the two restores separate and each one right for its job.
// `localStorage` answers "where was I yesterday" across a reload and a daemon
// restart. History answers "where was I a moment ago" inside this visit.
let navApplying = false;

function navState() {
  return {
    atrium: true,
    view: VIEWS.find(isViewing) || "board",
    card: termTask ? termTask.id : ""
  };
}

// Pushed only when something actually changed.
//
// Without this every repaint that re-ran `switchView` for the view already
// showing would stack an entry, and back would appear broken by needing to be
// pressed eleven times to move once.
function navPush() {
  if (termOnly()) return;
  const now = navState();
  const was = history.state;
  if (was && was.atrium && was.view === now.view && was.card === now.card) return;
  // The FIRST entry is replaced rather than pushed, or the first press of back
  // leaves the board for whatever was open in this tab before it.
  if (was && was.atrium) history.pushState(now, "");
  else history.replaceState(now, "");
}

// Going back is not a new move, so nothing here pushes. `navApplying` is what
// stops `switchView` and `attachTask` recording the journey they are being
// asked to undo.
addEventListener("popstate", async (e) => {
  const s = e.state;
  // Not ours. A solo window uses `replaceState` for its card and has no
  // entries of its own, and anything else in this tab's history belongs to
  // whatever was here before atrium.
  if (!s || !s.atrium || termOnly()) return;
  navApplying = true;
  try {
    switchView(s.view);
    if (s.view === "terms" && s.card && (!termTask || termTask.id !== s.card)) {
      await attachTask(s.card);
    }
  } finally {
    navApplying = false;
  }
});

function switchView(name) {
  // A popped-out window is one terminal and nothing else, forever. It has no
  // tabs to click, so every way it could become a board is indirect and none
  // of them are obvious from the call site: a tag chip calls `filterByTag`,
  // the in-terminal permission block offers "open in perms", a launch that
  // lands on an unsupervised card falls through to the board. Each one turned
  // the window you alt-tabbed to into a second board, which is the one thing
  // it must never be. Locking it here catches the paths nobody has found yet.
  if (termOnly() && name !== "terms") return;
  document.querySelectorAll(".tab").forEach(t => t.classList.toggle("on", t.dataset.view === name));
  VIEWS.forEach(v => document.getElementById(v).hidden = v !== name);
  // The board's toolbar is a sibling of the board rather than a child, so that
  // redrawing the columns does not take the control you just clicked with it.
  document.getElementById("board-bar").hidden = name !== "board";
  // Painted on the way in rather than once at boot. The group pills are drawn
  // by `paintGroupMode`, which runs whenever the mode changes and therefore may
  // never have run; this needs the same, and the bar is cheap to redraw.
  if (name === "board") paintBoardSort();
  // Fetched when you go there rather than on every poll. It is a question you
  // go looking for, and a query against a table that only grows has no
  // business running every few seconds while you are reading something else.
  if (name === "history") renderHistory(false);
  // A command box built while this view was hidden measured as zero, exactly
  // as a detached one does, so it is sized on the way in rather than only on
  // the way past a poll.
  if (name === "perms") sizeCommands();
  // Anything measured while this view was hidden was measured against zeroes
  // and discarded, so the bridge is placed on the way IN, on a layout that has
  // a size. Cheap enough to sit on every scroll of the list, so it is cheap
  // enough to sit here.
  if (name === "terms") requestAnimationFrame(placeTabBridge);
  // Fetched on the way in, like history: a feed you go looking for, not one
  // worth polling while you are reading something else. It also refreshes on an
  // `audit` delta and on stream reconnect. See js/audit.js.
  if (name === "audit" && typeof loadAudit === "function") loadAudit();
  // Where you were, so a reload puts you back. A restart of the daemon
  // reloads every board it is serving, and landing on the board every time
  // meant two clicks to get back to the terminal you were reading.
  //
  // localStorage rather than the URL, because the URL already means something
  // here: `#term=` is a popped-out window, and a view in the hash would make
  // every reload of a solo window a negotiation between the two.
  rememberWhereYouAre();
  // And a place to come back to. Not while going back, or pressing back would
  // record the arrival and leave forward pointing at where you just were.
  if (!navApplying) navPush();
  refresh();
}

// Where you are, read off the page rather than off the last thing that was
// called.
//
// It matters that this MEASURES rather than remembers. Writing the name into
// storage inside `switchView` looks equivalent and is not: a toast click, an
// alert, a launch that lands on the board and the restore itself all move the
// view, and any one of them writing last leaves storage describing a view you
// were on for a moment on your way somewhere else. The hidden attribute is the
// view you are actually looking at.
function rememberWhereYouAre() {
  if (termOnly()) return;
  try {
    const now = VIEWS.find(isViewing);
    if (now) localStorage.setItem("atrium.view", now);
    // ONLY EVER WRITTEN, NEVER CLEARED HERE.
    //
    // This was the bug that made the whole restore look broken. The daemon
    // answers HTTP before its fixtures start, so the first poll after a
    // restart lists the card as not supervised, `renderTermList` calls
    // `clearTermPane` because there is nothing to attach to, and clearing used
    // to forget which session you were on. By the time the build id changed
    // and the page reloaded, there was nothing left to go back to.
    //
    // Forgetting is a decision, so it is made where the decision is made:
    // attaching something else overwrites this, and closing the terminal
    // yourself clears it. A session that merely went away does neither.
    if (termTask) localStorage.setItem("atrium.term", termTask.id);
  } catch (e) {}
}
document.querySelectorAll(".tab").forEach(t => t.onclick = () => switchView(t.dataset.view));

// What the runner is doing right now, as a chip.
//
// The column says what a card needs from you; this says whether it is moving.
// Cards in running are otherwise indistinguishable whether the session is
// grinding through a build or hung.
//
// Not drawn while the card is waiting: the column and the wait chip carry that.
// Absent when the daemon has heard nothing recently, which is the case for a
// runner with no hooks.
function activityChip(t) {
  const a = t.activity;
  // Nothing a finished card is doing is worth drawing, and drawing it is how a
  // `thinking` badge ends up in the done column. Activity lives in memory and
  // outlasts the status change that filed the card, so this is a second line
  // of defence rather than the fix: whatever put a live session in `done` is
  // the bug, and this stops it looking like a feature.
  if (!a || !a.what || isWaiting(t) || t.status === "shelved" || staleActivity(t)) return "";
  const label = a.what === "tool"
    ? (a.tool ? `running ${esc(a.tool)}` : "running a tool")
    : esc(a.what);
  // "Running Bash for 40 minutes" says something the tool name alone does not.
  const age = a.seconds > 5 ? ` ${ago(a.seconds)}` : "";
  // The count OR the list. The daemon keeps the count at least as large as the
  // list, so these agree, and the badge is drawn off whichever says there is
  // something to show rather than trusting one of them. A named subagent that
  // did not raise the tally is still a subagent, and gating on the number
  // alone is what hid them.
  const subs = Math.max(a.subagents || 0, (a.running || []).length);
  const sub = subs > 0
    ? `<span class="chip sub" title="${esc(subagentTitle(a))}"
         onclick="event.stopPropagation();toggleSubagents(this)"
         >&#8618; ${subs}</span>`
    : "";
  // Compacting is the one state worth explaining, because the right response
  // to it is to do nothing: the session is rewriting what it knows, so a
  // message sent now lands in a conversation that is about to forget it.
  const why = a.what === "compacting"
    ? ` title="this session is rewriting its context and is about to forget most of it. ` +
      `nothing says when a compaction ends, so this clears on the next thing the session does."`
    : "";
  return `<span class="chip live ${esc(a.what)}"${why}>${label}${age}</span>${sub}`;
}

// Stop the live marks while the tab is not on screen.
//
// The marks are composited and cost little, but sixteen of them over a WebGL
// terminal is a count worth not paying for a tab nobody is looking at. One
// class on the body, and the rules that move anything are switched off by it.
function stillWhenHidden() {
  const paint = () => document.body.classList.toggle("tabhidden", document.hidden);
  document.addEventListener("visibilitychange", paint);
  paint();
}

// What the count is made of, for the chip's tooltip. Named where the runner
// says who they are, and honest when it does not: a runner that reports no id
// moves the count and contributes no name, so this can be shorter than the
// number beside it.
function subagentTitle(a) {
  const running = a.running || [];
  if (!running.length) {
    return `${a.subagents} subagent${a.subagents === 1 ? "" : "s"} running. ` +
      "this runner does not say which. click to expand.";
  }
  const named = running.map(s => s.type || "unnamed").join(", ");
  const missing = a.subagents - running.length;
  return missing > 0
    ? `${named}, and ${missing} this runner did not name. click to expand.`
    : `${named}. click to expand.`;
}

// The subagents themselves, under the card that spawned them.
//
// Built on demand rather than always, because most cards have none and a card
// is a row you scan past. Toggled on the chip, since the chip is the thing
// that says there is something to open.
function subagentList(a) {
  const running = a.running || [];
  const rows = running.map(s =>
    `<div class="sub-row">
       <span class="sub-kind">${esc(s.type || "unnamed")}</span>
       <span class="sub-age">${ago(s.seconds)}</span>
     </div>`).join("");
  const missing = a.subagents - running.length;
  // Said rather than hidden. A count that does not match the list is the kind
  // of thing that reads as a bug, and the reason is worth one line.
  const rest = missing > 0
    ? `<div class="sub-row quiet">${missing} more, which this runner did not name</div>`
    : "";
  return `<div class="subagents">${rows}${rest}</div>`;
}

// Opens or closes the list under the card the chip belongs to.
function toggleSubagents(chip) {
  const card = chip.closest(".card, .stackrow");
  if (!card) return;
  const open = card.querySelector(":scope > .subagents");
  if (open) { open.remove(); chip.classList.remove("open"); return; }
  const id = card.dataset.id;
  const t = (lastTasks || []).find(x => x.id === id);
  if (!t || !t.activity) return;
  card.insertAdjacentHTML("beforeend", subagentList(t.activity));
  chip.classList.add("open");
}

// How much of its context window a session has burned, from its statusline.
//
// The single most useful thing on a card when you are deciding which of
// sixteen agents to interrupt. A session at ninety percent is about to
// compact, and compacting is where it forgets what you told it, so the one to
// go and look at is that one and not whichever asked most recently.
//
// Drawn while the card is WAITING as well, unlike the activity chip. That is
// exactly the moment the number decides something: whether to answer this one
// now or let it sit.
//
// Absent for every runner whose statusline does not post, which is all of them
// until one is wired up. See docs/statusline-telemetry.md.
const CTX_WARM = 60;
const CTX_HOT = 85;

function contextChip(t) {
  const c = t.telemetry;
  // Nothing a finished card burned is worth drawing. The daemon forgets a
  // card's telemetry when its session ends, so this is a second line of
  // defence rather than the fix, in the same shape as the activity chip.
  if (!c || over(t) || t.status === "shelved") return "";
  // The limits are drawn whether or not a context figure came with them. A
  // session that has just started has burned nothing and the account it is
  // running on can still be about to stop, which is the more urgent of the
  // two facts.
  const limits = limitChip("5h", c.five_hour) + limitChip("week", c.weekly);
  if (!c.pct) return limits;
  const heat = c.pct >= CTX_HOT ? " hot" : c.pct >= CTX_WARM ? " warm" : "";
  return `<span class="chip ctx${heat}" title="${esc(contextTitle(c))}">ctx ${c.pct}%</span>` + limits;
}

// What is behind the percentage, for the tooltip. Tokens where the statusline
// reported them, because the same percentage is a different amount of room on
// a different window, and the model name is what says which window it is.
function contextTitle(c) {
  const parts = [];
  if (c.used && c.window) {
    parts.push(`${fmtTokens(c.used)} of ${fmtTokens(c.window)} tokens`);
  } else {
    parts.push(`${c.pct}% of the context window`);
  }
  if (c.model) parts.push(c.model);
  for (const [label, lim] of [["5h limit", c.five_hour], ["weekly limit", c.weekly]]) {
    if (!lim) continue;
    parts.push(`${label} ${lim.pct}%${lim.resets_at ? `, resets ${resetsIn(lim.resets_at)}` : ""}`);
  }
  // Aged, because a figure is only ever about when it was taken and a
  // statusline stops posting the moment its terminal stops drawing.
  parts.push(`reported ${c.seconds > 5 ? ago(c.seconds) + " ago" : "just now"}`);
  return parts.join(" · ");
}

// A rolling account limit, drawn only when it is close enough to change what
// you do next.
//
// Below the threshold this is a fact about billing and not about this card,
// and a chip that is always there is one nobody reads. Above it, it is the
// reason the whole board is about to stop.
const LIMIT_SHOW = 80;

function limitChip(label, lim) {
  if (!lim || lim.pct < LIMIT_SHOW) return "";
  const when = lim.resets_at ? `, resets ${resetsIn(lim.resets_at)}` : "";
  return `<span class="chip limit"
    title="this account is at ${lim.pct}% of its ${label} limit${when}"
    >${label} ${lim.pct}%</span>`;
}

// When a limit comes back, as a duration rather than a timestamp. "in 2h14m"
// is a decision, "2026-09-06T18:00:00Z" is arithmetic.
function resetsIn(iso) {
  const at = new Date(iso);
  if (isNaN(at)) return "";
  const secs = Math.floor((at - Date.now()) / 1000);
  return secs <= 0 ? "any moment" : `in ${ago(secs)}`;
}

// Tokens, short. A context window is six digits and a card is a thing you scan.
function fmtTokens(n) {
  return n >= 1000 ? `${Math.round(n / 1000)}k` : String(n);
}

// A card atrium cannot vouch for: nothing heard from the session, and no
// process id to ask the operating system about. It may be working, it may have
// ended without a word, and the difference is not knowable from here.
//
// The label is about what atrium knows, not about what the agent is doing.
// Guessing at the agent's state and being wrong is what makes a board stop
// being believed.
const NO_CONTACT_AFTER_SECONDS = 20 * 60;
const isOutOfContact = (t) =>
  t.status === "running" && !t.pid && (t.idle_seconds || 0) > NO_CONTACT_AFTER_SECONDS;

// The runner is a mark rather than the word. "claude" on every card in a
// column of claude sessions spent the widest chip there saying nothing.
//
// One chip carries the state and how long it has been that way. Three things
// were wrong with two. A ready card read "idle 30m" beside "waiting 31m", the
// same minute off two clocks that start together, with a word the column had
// stopped using. A dead card read "dead" beside "dead 1h". And a card in the
// running column read "idle", the one column where that is a different claim
// from the column it is sitting in. A waiting card times how long it has
// waited. Everything else has only idle to time, and says so in its own
// state's words: a dead card that has been dead an hour is telling you
// something the word idle hides.
//
// NOTE: everything below is inside a template literal, so it must contain no
// backtick. An HTML comment does not protect one: a backtick anywhere in here
// ends the string, and the page then fails to parse with an error pointing at
// whatever word came next rather than at the quote. Prose comments about this
// markup belong out here, where they can use whatever punctuation they like.
function cardHTML(t) {
  const w = isWaiting(t);
  const dark = isOutOfContact(t);
  // Clickable for the actions done most, because the alternative was opening
  // the detail dialog for every shelve and every attach.
  //
  // NOT draggable. A card is text you copy off far more often than a thing you
  // move, and `draggable` is not a hint: it takes the pointerdown, so the
  // selection never starts. `data-rank` stays because the menu's up and down
  // are computed from the ranks on screen.
  return `<div class="card ${w ? "waiting" : ""}${dark ? " nocontact" : ""}${
      t.auto_approve ? " autoon" : ""}"
    onclick="cardMenu(event, '${t.id}')"
    data-id="${t.id}" data-rank="${t.rank}"
    oncontextmenu="cardMenu(event, '${t.id}')">
    <div class="card-line">
      <div class="title">${w ? '<span class="pulse"></span>' : ""}${
        t.pinned ? '<span class="pin on" title="pinned">&#9733;</span>' : ""}${
        runnerMark(t.runner)}${esc(t.display_title)}</div>
      <div class="chips">
      ${modelChip(t)}
      ${tagChips(t)}
      ${noteChip(t)}
      ${originChip(t)}
      ${recapChip(t)}
      ${activityChip(t)}
      ${contextChip(t)}
      ${dark ? `<span class="chip nocontact"
        title="atrium cannot tell whether this is alive. nothing has been heard from the session, and there is no process id to ask the operating system about. it may be working, or it may have ended without saying so. moved to finished after three hours, and brought back the moment it says anything"
        >no contact</span>` : ""}
      ${w
        ? `<span class="chip warn" title="${t.status === "needs-permission"
              ? "how long this agent has been frozen waiting to be answered"
              : "how long since it finished its turn and asked for you"}"
             >${esc(statusLabel(t.status))} ${ago(t.wait_seconds)}</span>`
        : `<span class="chip" title="${t.status === "dead" ? "how long since this stopped"
             : t.status === "done" ? "how long since you marked it done"
             : t.status === "running" ? "how long since it last did anything"
             : "how long since anything was heard from it"}"
             >${esc(statusLabel(t.status))} ${ago(t.idle_seconds)}</span>`}
      ${t.auto_approve ? `<span class="chip auto"
        title="auto mode: requests from this session are approved without asking, and recorded"
        >auto</span>` : ""}
      ${t.report_unverified ? `<span class="chip warn"
        title="${esc("this session reported done at " + (t.report_sha || "") +
          ", and that commit is not in its worktree. it may have landed somewhere else.")}"
        >sha unverified</span>` : ""}
      ${t.escalation && t.escalation.count > 0 ? `<span class="chip warn"
        title="${esc(t.escalation.text)}"
        >stuck ${esc(String(t.escalation.minutes))}m</span>` : ""}
      ${sharedCards.has(t.id) ? `<span class="chip shared"
        title="${esc("this session is published at " + (sharedCards.get(t.id).address || "") +
          ". anyone with that address types into it as you would. right click to stop.")}"
        oncontextmenu="stopSharingChip(event, '${t.id}')"
        >shared</span>` : ""}
      ${t.status === "shelved" && !t.offline ? `<span class="chip attach"
        title="${cannotResume(t)
          ? esc("comes off the shelf, but nothing starts: " + cannotResume(t))
          : "start it again from where the conversation left off"}"
        onclick="event.stopPropagation();unshelveCard('${t.id}')"
        >${cannotResume(t) ? "unshelve" : "resume"}</span>` : ""}
      ${t.status === "backlog" && !t.offline ? `<span class="chip attach"
        title="open the launch dialog with what the source already knew filled in.
               this card is the target, so starting it does not make a second one"
        onclick="event.stopPropagation();startOffered('${t.id}')">start</span>` : ""}
      ${t.offline ? `<span class="chip nocontact"
        title="${esc("room " + (t.room || "") + " is offline. cannot restore terminal")}"
        >&#128683;</span>` : ""}
      ${t.supervised && !t.offline ? `<span class="chip attach"
        onclick="event.stopPropagation();attachTask('${t.id}')">attach</span>
      <span class="chip attach icon"
        title="open this terminal in its own window"
        onclick="event.stopPropagation();popOutTask('${t.id}')">${popIcon()}</span>` : ""}
      </div>
    </div>
    ${askLine(t)}
    ${t.why ? `<div class="why">${esc(t.why)}</div>` : ""}
  </div>`;
}

// A QUESTION THE SESSION RAISED, drawn as a question.
//
// It used to land in `why` and be drawn by the line below it, so a question
// raised five seconds ago and a note typed a week ago were the same italic
// sentence. The reading was that somebody had written a strange note, and the
// question itself went unanswered because nothing said it was one.
//
// They are not the same claim. `why` is WHAT THIS CARD IS FOR: written once,
// read later, still true tomorrow. An ask is WHAT IT NEEDS NOW, and it stops
// being true the moment somebody answers it. So it is its own field on the
// card, drawn in its own voice, with a label in the words a person would use.
//
// A question put to another SESSION says so instead. That card is still
// stopped and still counted, and you are not the one who owes it an answer,
// which is a different thing to be looking at.
// PRESSING IT ANSWERS IT, which is the other half of drawing it at all.
//
// A question you can read and not reply to sends you looking for the card, and
// the reply box is on the card dialog, so the whole path was: read the
// question, find the card, open it, scroll to the box. This is that path in
// one press. Saying anything to a card already clears its ask, so nothing new
// happens here: it is a way to reach something that was already there.
function askLine(t) {
  if (!t.ask) return "";
  const peer = !!t.ask_peer;
  const label = peer ? `asked ${t.ask_peer}` : "this agent has a question";
  // AND HOW MANY MORE THERE ARE.
  //
  // `t.ask` is the OLDEST outstanding question, and the card carries only that
  // one, so a row drawing it alone says "there is a question" about a card
  // that may have six. An ask used to be a single column and a second question
  // destroyed the first. That is fixed; drawing one of several without saying
  // so is the same loss told more quietly.
  //
  // The count rides down with the card, counted for the whole board in one
  // query. Absent below two, because one question is what drawing the ask
  // already means.
  const more = (t.asks_open || 0) - 1;
  const rest = more > 0 ? `<span class="askmore">+${more} more</span>` : "";
  const tip = peer
    ? "it put this to " + t.ask_peer + " rather than to you. it is still stopped, "
      + "so this is where you find out when no answer comes back. press to say "
      + "something to this card anyway"
    : "the session raised this itself. press to answer it: saying anything to "
      + "this card answers it, and the question comes off";
  // `stopPropagation`, because a card and a stack row are both clickable and
  // the ask sits inside them. Without it this fires the row's own handler too,
  // which opens the same dialog and takes the focus straight back off the box.
  return `<div class="ask act" title="${esc(tip)}"
    onclick="event.stopPropagation();answerAsk('${esc(t.id)}')"
    ><span class="asklabel">${esc(label)}</span>${esc(t.ask)}${rest}</div>`;
}

// Opens the card the question is on, with the cursor in the box that answers
// it.
//
// Re-opening is skipped when that card's dialog is already up, since `openTask`
// refetches the card and repaints the timeline, and doing that under somebody
// who just pressed the question is a flash and a scroll for nothing.
// Every other question this card is waiting on.
//
// NOT DRAWN AT ALL FOR ONE, which is the ordinary case: the field above IS
// that question, and a list of one underneath it reads as a second thing.
//
// Fetched when the dialog opens rather than carried on every card in every
// poll. The count comes down with the card and decides whether to ask at all,
// so a board full of cards with one question each makes no extra request.
//
// A failure is silent and draws nothing. This is a detail on a dialog whose
// main answer is already on screen, and a card that cannot list its other
// questions is still worth opening.
async function paintMoreAsks(id, open) {
  const box = document.getElementById("d-ask-more");
  if (!box) return;
  box.hidden = true;
  setHTML(box, "");
  if (!id || open < 2) return;
  let asks;
  try {
    asks = (await api(`/v1/tasks/${encodeURIComponent(id)}/asks`)).asks || [];
  } catch (e) {
    return;
  }
  // The dialog may have moved to another card while this was in flight, and
  // one card's questions drawn on another is worse than none.
  if (!current || current.id !== id) return;
  // The first is the one the field above is already showing.
  const rest = asks.slice(1);
  if (!rest.length) return;
  setHTML(box, `<div class="askrest">
    <span class="asklabel">and ${rest.length} more</span>
    ${rest.map(a => `<div class="askrestone">${esc(a.text)}${
      a.peer ? `<span class="by"> asked ${esc(a.peer)}</span>` : ""}</div>`).join("")}
  </div>`);
  box.hidden = false;
}

// Takes every outstanding question off a card and tells the session nothing.
//
// The count comes back rather than being assumed, because the card may have
// been asked something else between the menu being drawn and the click.
async function dismissAsks(id) {
  try {
    const r = await api(`/v1/tasks/${encodeURIComponent(id)}/asks`, { method: "DELETE" });
    const n = r.dismissed || 0;
    toast(n === 1 ? "question dismissed" : `${n} questions dismissed`,
      "nothing was sent to the session");
  } catch (e) {
    toast("could not dismiss", e.message);
    return;
  }
  refresh();
}

async function answerAsk(id) {
  if (!id) return;
  if (!(detail.open && current && current.id === id)) await openTask(id);
  const box = document.getElementById("d-say");
  if (!box) return;
  box.scrollIntoView({ block: "center" });
  box.focus();
}

// ── grouping cards by project ───────────────────────────
// A card shows its leaf directory, so `dotfiles` and `targetted-releases` sit
// next to each other with nothing saying which repo either belongs to. With
// several worktrees per repo the column is a list of names that mean something
// only if you already know them.
//
// The rule is not atrium's to decide. The operator already has a worktree
// layout with its own conventions, so both the grouping and the ordering are
// JavaScript they can replace. The defaults below work with no configuration.

const GROUPING_KEY = "atrium.grouping";

// Defaults, shown in the settings dialog as the starting point and used when
// nothing has been written.
const DEFAULT_GROUP_BY = `// Return the group name for a card, or "" for no group.
// The whole task is available: worktree, runner, status, title, why, pid.
const path = (task.worktree || "").replace(/\\\\/g, "/");
if (!path) return "";

// .../<forge>/<org>/<repo>/... -> "org/repo"
const forge = path.match(/\\/(?:github|gitlab|bitbucket)[^/]*\\/([^/]+)\\/([^/]+)/i);
if (forge) return forge[1] + "/" + forge[2];

// Otherwise the last two segments, which is usually parent/leaf.
const parts = path.split("/").filter(Boolean);
if (parts.length >= 2) return parts.slice(-2).join("/");
return parts[0] || "";`;

const DEFAULT_GROUP_ORDER = `// Sort two group names. Return <0, 0, or >0.
// Alphabetical, with anything ungrouped last.
if (!a) return 1;
if (!b) return -1;
return a.localeCompare(b);`;

// On by default. A board of leaf directory names says nothing about which
// repo any of them belong to, and the default rule works with no setup.
// `mode` is what a group IS: a project read out of the path, or a tag you
// applied. `by` and `order` stay the escape hatch for anyone who wants
// something neither of those describes.
const GROUPING_DEFAULTS = { on: true, mode: "project", by: "", order: "", hues: {}, groups: [] };

// A card with no tags still has to land somewhere when grouping by tag, and
// "" would sort it in with a group whose name is empty.
const UNTAGGED = "untagged";

function groupingPrefs() {
  try {
    return Object.assign({}, GROUPING_DEFAULTS,
      JSON.parse(localStorage.getItem(GROUPING_KEY) || "{}"));
  } catch (e) { return Object.assign({}, GROUPING_DEFAULTS); }
}

function setGrouping(patch) {
  const next = Object.assign(groupingPrefs(), patch);
  localStorage.setItem(GROUPING_KEY, JSON.stringify(next));
  refresh();
}

// Compiles a function body once per render.
//
// Operator-supplied code running in the operator's own browser is not a
// security question. A broken function taking the board down is, so a compile
// or a call that throws falls back to the default and reports once.
//
// THE RULE THIS RESTS ON, WRITTEN DOWN BECAUSE IT IS NOT OBVIOUS:
//
//   AN EXPRESSION MAY BE STORED WHERE IT WAS TYPED. Anything wider needs a
//   different mechanism, not a bigger text box.
//
// `groupingPrefs` reads `localStorage` and nothing else. That is what makes
// `new Function` safe here, and it is safe by ACCIDENT rather than by design:
// code that runs in this browser was typed into this browser, by whoever was
// sitting at it, and somebody who can write it can already open dev tools.
//
// Three things would break that, and two of them are on the backlog:
//
//   - Storing the expression on the daemon. It becomes something one machine
//     typed and another machine runs, which is the shape of a stored XSS.
//     Wanted, reasonably: grouping is board-wide and today it is lost when you
//     open a different browser. `internal/api/settings.go` refuses the field
//     rather than leaving that to be discovered.
//   - Federation. `docs/federation-design-v2.md` puts many machines behind one
//     board, and a grouping function shipped from a leaf and run in the
//     forum's browser crosses a boundary that does not exist today.
//   - What the function can already reach. These run with full page scope, so
//     `fetch` is in hand, and the board can read the daemon's filesystem. A
//     grouping function is a general-purpose exfiltration primitive the moment
//     somebody other than the operator can supply one.
//
// If board-wide grouping is wanted, the options in order are a restricted
// expression language, a `Worker` with `connect-src 'none'`, or a fixed menu
// for the shared case with free expressions staying local. `docs/backlog.md`
// has the argument.
let groupingFault = "";
function compiled(body, fallback, ...args) {
  const src = (body || "").trim() ? body : fallback;
  try {
    return new Function(...args, src);
  } catch (e) {
    groupingFault = "your grouping code did not compile: " + e.message;
    try { return new Function(...args, fallback); } catch (e2) { return null; }
  }
}

// The project rule as a callable, for the modes that want it as a FALLBACK
// rather than as the whole answer.
//
// Compiled from the same source the settings dialog shows and cached, so there
// is one project rule on the board and not a second copy written in JavaScript
// beside the one written as a string.
let projectRule;
function defaultProjectOf(task) {
  if (projectRule === undefined) projectRule = compiled("", DEFAULT_GROUP_BY, "task");
  if (!projectRule) return "";
  try { return String(projectRule(task) ?? ""); } catch (e) { return ""; }
}

// Builds the grouper and the sorter for one render, each already wrapped so a
// throw on one card cannot stop the board drawing the rest.
// The buckets, newest first. Dormant last, which is where it belongs both in
// order and in how much of your attention it deserves.
const RECENCY_ORDER = ["today", "yesterday", "this week", "this month", "dormant"];

// Which bucket a card falls in, from when it was last active.
//
// Calendar boundaries, not rolling windows. Midnight is what makes something
// "yesterday's work", and a rolling 24 hours would put this morning and last
// night in the same bucket at 2am and different ones at 2pm.
//
// The buckets nest: "this week" means earlier this week and not today or
// yesterday, since those have their own. That is what the operator asked for
// and it is also the only way a card lands in exactly one place.
function recencyBucket(t) {
  const at = new Date(t.last_activity_at);
  if (isNaN(at)) return "dormant";

  const startOfToday = new Date();
  startOfToday.setHours(0, 0, 0, 0);
  if (at >= startOfToday) return "today";

  const startOfYesterday = new Date(startOfToday);
  startOfYesterday.setDate(startOfYesterday.getDate() - 1);
  if (at >= startOfYesterday) return "yesterday";

  // Seven days back from the start of today, which is what "this week" means
  // in conversation. Not the week containing Monday: on a Monday morning that
  // bucket would be empty and everything from the week just worked would read
  // as older than it is.
  const weekAgo = new Date(startOfToday);
  weekAgo.setDate(weekAgo.getDate() - 7);
  if (at >= weekAgo) return "this week";

  const monthAgo = new Date(startOfToday);
  monthAgo.setMonth(monthAgo.getMonth() - 1);
  if (at >= monthAgo) return "this month";

  return "dormant";
}

function grouper() {
  const p = groupingPrefs();
  if (!p.on) return null;
  groupingFault = "";

  // When you last touched it, in buckets rather than as a duration.
  //
  // "2h15m" answers how stale something is and does not answer the question
  // actually being asked, which is whether this is work from today or work you
  // walked away from. Calendar days rather than rolling hours, because that is
  // what a person means: something from 11pm last night is yesterday's at 1am,
  // not "two hours ago".
  //
  // Everything past a month is dormant and drawn greyed. That bucket is the
  // point of the whole grouping: work you started and left, which is invisible
  // in a list ordered by anything else.
  if (p.mode === "recency" && !String(p.by || "").trim()) {
    return {
      // A LIST of one. Every caller iterates this, because grouping by tag
      // puts a card under several names, and a bare string iterates as its
      // characters: "today" became five groups called t, o, d, a and y, each
      // holding every card.
      of: t => [recencyBucket(t)],
      cmp: (a, b) => RECENCY_ORDER.indexOf(a) - RECENCY_ORDER.indexOf(b)
    };
  }

  // BY THE PILE THE LAUNCHER PUT IT IN. `window_name` is a string somebody
  // else decided: `pull-requests`, `tangent`, `discourse`, a repo name,
  // anything. Atrium stores it and groups by it without knowing what any of
  // them mean, which is why this needs no list of valid answers.
  //
  // A card with no window falls back to its PROJECT rather than to one heap
  // called "none". Most cards on any board were never launched with a window,
  // and a mode that put all of them together would be unusable on the day it
  // was turned on.
  if (p.mode === "window" && !String(p.by || "").trim()) {
    const of = t => {
      const w = String(t.window_name || "").trim();
      return [w || defaultProjectOf(t)];
    };
    return { many: false, of, cmp: (a, b) => {
      if (!a) return 1;
      if (!b) return -1;
      return a.localeCompare(b);
    } };
  }

  // Grouping by tag needs no compiled function. A card can carry several, so
  // this is the one grouper that puts a card in more than one place, and the
  // callers have to handle a list rather than a name.
  if (p.mode === "tag" && !String(p.by || "").trim()) {
    return {
      many: true,
      of: t => (t.tags && t.tags.length ? t.tags : [UNTAGGED]),
      // Untagged last, since it is the absence of an answer rather than one.
      cmp: (a, b) => {
        if (a === UNTAGGED) return 1;
        if (b === UNTAGGED) return -1;
        return a.localeCompare(b);
      }
    };
  }

  // The operator's own buckets, kept as an ORDERED list of tag names in
  // localStorage. Membership is a tag on the card, so the state is durable and
  // shared across browsers; the order and the choice of which tags are groups
  // are local to this browser, the way every other view preference is.
  //
  // A card lands in every group it is tagged for, so a card carrying two of
  // them appears in each, the same as `tag` mode. `UNTAGGED` catches cards
  // with none of the named tags: the list is a curated view, so a tag that
  // is not on the list draws no bucket of its own.
  if (p.mode === "custom" && !String(p.by || "").trim()) {
    const groups = Array.isArray(p.groups) ? p.groups : [];
    const set = new Set(groups);
    const at = new Map(groups.map((n, i) => [n, i]));
    return {
      many: true,
      // DRAWN EVEN WHEN EMPTY. A group you just made has no cards yet, and a
      // view that only draws groups with cards in them never showed it, so
      // there was nothing to look at to know the add had worked. Every view
      // that groups seeds its buckets from this list. See `emptyGroupHint`.
      always: groups.slice(),
      // HELD TO RANK, not to the sort pill. A group you made is an order you
      // set, the same as the pinned bucket, so the sort only reorders the
      // groups the board makes for itself. See `byRank`.
      handOrdered: true,
      of: t => {
        const mine = (t.tags || []).filter(x => set.has(x));
        return mine.length ? mine : [UNTAGGED];
      },
      cmp: (a, b) => {
        if (a === UNTAGGED) return 1;
        if (b === UNTAGGED) return -1;
        return (at.get(a) ?? 1e9) - (at.get(b) ?? 1e9);
      }
    };
  }

  const by = compiled(p.by, DEFAULT_GROUP_BY, "task");
  const order = compiled(p.order, DEFAULT_GROUP_ORDER, "a", "b");
  if (!by) return null;

  const safeBy = t => {
    try { return String(by(t) ?? ""); }
    catch (e) {
      groupingFault = groupingFault || "your grouping code threw: " + e.message;
      return "";
    }
  };
  const safeOrder = (a, b) => {
    if (!order) return a.localeCompare(b);
    try { return Number(order(a, b)) || 0; }
    catch (e) {
      groupingFault = groupingFault || "your ordering code threw: " + e.message;
      return a.localeCompare(b);
    }
  };
  // One group per card, wrapped as a list so every caller reads the same
  // shape whichever mode is on.
  return { many: false, of: t => [safeBy(t)], cmp: safeOrder };
}

// A color per group.
//
// Hashed from the name by default, so a project is colored the moment it
// appears and keeps that color with nothing configured. A hash cannot know
// that two projects you care about landed on similar hues, so an override
// wins when there is one.
function groupHue(name) {
  const set = groupingPrefs().hues || {};
  if (set[name] !== undefined) return set[name];
  let h = 0;
  for (let i = 0; i < name.length; i++) h = (h * 31 + name.charCodeAt(i)) % 360;
  return h;
}

// The palette offered when recoloring. Spread around the wheel so two picks
// are always tellable apart, which is the whole job of the color.
const GROUP_HUES = [0, 25, 45, 65, 95, 140, 170, 195, 215, 250, 285, 320];

// Right click a group heading to recolor it, and in `custom` mode to reorder,
// rename, or remove the group from the view.
function groupMenu(e, name) {
  e.preventDefault();
  e.stopPropagation();
  const p = groupingPrefs();
  const set = p.hues || {};
  const custom = p.mode === "custom";
  const groups = Array.isArray(p.groups) ? p.groups : [];
  const at = groups.indexOf(name);
  const inView = custom && at >= 0;
  showMenu(e, [
    { label: name, act: () => {} },
    { sep: true },
    inView && at > 0
      ? { label: "move up", act: () => moveCustomGroup(name, -1) } : null,
    inView && at < groups.length - 1
      ? { label: "move down", act: () => moveCustomGroup(name, 1) } : null,
    inView ? { label: "rename…", act: () => renameCustomGroup(name) } : null,
    inView ? {
      label: "remove from the view",
      note: "does not untag any card",
      help: "Removes this group from the list. The tag stays on every card " +
        "and files them into this bucket again if you add the group back.",
      act: () => removeCustomGroup(name)
    } : null,
    inView ? { sep: true } : null,
    { label: "recolor…", act: () => pickGroupHue(name) },
    set[name] !== undefined
      ? { label: "back to the automatic color", act: () => setGroupHue(name, null) }
      : null
  ]);
}

// Add a new group to the `custom` view.
//
// The name is a TAG under the hood, so `NormalizeTags` will lower case and
// trim it; a bare `askText` accepts what the operator typed and the tag path
// does the canonicalisation. A duplicate is a no-op with a toast, because
// silently ignoring is how "two spellings of the same thing" happens.
async function addCustomGroup() {
  const raw = await askText("name this group",
    "It is a tag under the hood, so a card is filed into it by carrying that " +
    "tag. Anything already carrying the tag lands in it right away.",
    "", "e.g. discourse");
  if (raw === null) return;
  const name = String(raw).trim().toLowerCase();
  if (!name) return;
  const p = groupingPrefs();
  const groups = Array.isArray(p.groups) ? p.groups.slice() : [];
  if (groups.includes(name)) {
    toast("already a group", "the view already has one called " + name);
    return;
  }
  groups.push(name);
  setGrouping({ groups });
}

function removeCustomGroup(name) {
  const p = groupingPrefs();
  const groups = (p.groups || []).filter(x => x !== name);
  setGrouping({ groups });
}

function moveCustomGroup(name, delta) {
  const p = groupingPrefs();
  const groups = (p.groups || []).slice();
  const at = groups.indexOf(name);
  const to = at + delta;
  if (at < 0 || to < 0 || to >= groups.length) return;
  groups.splice(at, 1);
  groups.splice(to, 0, name);
  setGrouping({ groups });
}

async function renameCustomGroup(name) {
  const raw = await askText("rename " + name,
    "The old tag stays on every card. This changes the group's name in the " +
    "view and cards under it will move as their tags update. Retag the cards " +
    "if you want the tag renamed too.",
    name, "new name");
  if (raw === null) return;
  const next = String(raw).trim().toLowerCase();
  if (!next || next === name) return;
  const p = groupingPrefs();
  const groups = (p.groups || []).map(x => x === name ? next : x);
  setGrouping({ groups });
}

function setGroupHue(name, hue) {
  const hues = Object.assign({}, groupingPrefs().hues || {});
  if (hue === null) delete hues[name]; else hues[name] = hue;
  setGrouping({ hues });
}

// Swatches rather than a color input, because the color is a hue on a fixed
// lightness: a full picker would offer combinations the board cannot draw.
function pickGroupHue(name) {
  const body = `<div class="swatches">` + GROUP_HUES.map(h =>
    `<button class="swatch" style="--ghue:${h}" data-hue="${h}"
      title="hue ${h}"></button>`).join("") + `</div>`;
  askUser({
    title: "color for " + name,
    body,
    buttons: [{ label: "cancel", value: null }]
  });
  // Wired after the dialog has drawn its body.
  setTimeout(() => {
    document.querySelectorAll("#ask-body .swatch").forEach(b => b.onclick = () => {
      setGroupHue(name, Number(b.dataset.hue));
      askDlg.close();
    });
  }, 0);
}

// Renders a list of cards, split into project groups when grouping is on.
// A pinned card is in the pinned group, and in no other.
//
// The board never ordered by pinned at all: the stack has done it since
// pinning existed, and the board drew whatever order the API returned, so a
// pinned card could sit at the BOTTOM of its column, which is the one place it
// was promised never to be.
//
// Sorting it to the top of its own project group was the first fix and it was
// not enough. Pinning says "keep this where I can see it", and a card at the
// top of a group four screens down is not where you can see it. So pinned is a
// group of its own, at the top, and a pinned card leaves whatever group it
// would otherwise have been in. Being in two places at once would make the
// counts lie and the card appear twice.
//
// Per column rather than board-wide, because a column is a bucket of your
// attention and a pinned card that is BLOCKED is a different fact from a
// pinned card that is idle.
function pinnedGroupHTML(pins, keyPrefix) {
  if (!pins.length) return "";
  const key = keyPrefix + ": pinned";
  const fold = "proj:pinned";
  const shut = isFolded(fold) ? "" : " open";
  // A fixed hue rather than one derived from a name. Every other group takes
  // its color from what it is called; this one is not a project and borrowing
  // a project's color would say it was.
  return `<details class="cardgroup project pins"${shut} style="--ghue:41"
    data-morph-key="${esc(key)}" data-fold="${esc(fold)}">
    <summary>
      <span class="gname">&#9733; pinned</span>
      <span class="gn">${pins.length}</span>
    </summary>
    ${pins.map(cardHTML).join("")}
  </details>`;
}

function cardsHTML(cards, g, keyPrefix) {
  if (!cards.length) return '<div class="empty">empty</div>';

  // Taken out before anything else looks at them, so they cannot also appear
  // under a project.
  //
  // BY RANK, ALWAYS, and never by the board sort. `columnOrder` has already run
  // over the whole column by the time we get here, so the pins arrive in
  // activity or name order like everything else. But pinning is the one order a
  // person set by hand, so it is held to rank the way manual mode is: rank is
  // what the move up and move down write and the only field that survives a
  // card going quiet. Sorting here rather than trusting the API order restores
  // it after `columnOrder` disturbed it. `cardTieBreak` keeps two equal ranks
  // from swapping between polls.
  const pins = cards.filter(t => t.pinned).sort(byRank);
  const rest = cards.filter(t => !t.pinned);
  const head = pinnedGroupHTML(pins, keyPrefix);

  if (!g) return head + rest.map(cardHTML).join("");

  // A card can belong to several groups when grouping by tag, so it appears
  // under each. Anything else returns one name and this reads the same.
  const byName = new Map();
  for (const t of rest) {
    for (const name of g.of(t)) {
      if (!byName.has(name)) byName.set(name, []);
      byName.get(name).push(t);
    }
  }
  seedGroups(byName, g);
  // One group holding everything is not a grouping, so it is not drawn as one.
  if (byName.size < 2 && byName.has("")) return head + rest.map(cardHTML).join("");

  return head + [...byName.keys()].sort(g.cmp).map(name => {
    const mine = byName.get(name);
    // Cards in none of the operator's groups are the board's own heap, so they
    // keep the sort. Every named group is theirs and keeps its rank.
    if (g.handOrdered && name !== UNTAGGED) mine.sort(byRank);
    if (!name) return mine.map(cardHTML).join("");
    const key = keyPrefix + ":" + name;
    // FOLDED BY NAME, BOARD WIDE, and not by name-within-column. A card that
    // changes status moves to another column, where a group of the same name
    // is a different group and used to be drawn open. So collapsing a project
    // and then watching one of its cards block read as the board deciding to
    // pop the group back open, which is exactly what it looked like.
    const fold = "proj:" + name;
    // UNTAGGED IN `custom` MODE STARTS SHUT, the way the offline group does.
    // The point of a curated view is to see the buckets you built, not a heap
    // of every card that lands in none of them. The fold list records what
    // was CHANGED from the default, so an entry there re-opens it and the
    // shape reads the same as every other fold.
    const shutByDefault = name === UNTAGGED && groupingPrefs().mode === "custom";
    const shut = (isFolded(fold) === shutByDefault) ? " open" : "";
    // Work you started and left. Drawn back rather than hidden: the point of
    // the bucket is that it is findable, and the point of greying it is that
    // it does not compete with what you are doing now.
    const cold = name === "dormant" ? " dormant" : "";
    // Keyed by the group's own name, so reordering the groups moves them
    // rather than rewriting each one with the next one's contents.
    return `<details class="cardgroup project${cold}"${shut}
      style="--ghue:${groupHue(name)}" data-morph-key="${esc(key)}"
      data-fold="${esc(fold)}">
      <summary oncontextmenu="groupMenu(event, '${esc(name).replace(/'/g, "&#39;")}')">
        <span class="gname" title="${esc(name)} &mdash; right click to recolor">${esc(name)}</span>
        <span class="gn">${mine.length}</span>
      </summary>
      ${mine.length ? mine.map(cardHTML).join("") : emptyGroupHint()}
    </details>`;
  }).join("");
}

// The order of a list somebody arranged by hand: the pinned bucket and every
// custom group, in every view and under every sort. Rank is what move up, move
// down and the strip's drag write. `cardTieBreak` keeps two equal ranks from
// swapping between polls.
function byRank(a, b) {
  return (a.rank || 0) - (b.rank || 0) || cardTieBreak(a, b);
}

// Adds the groups a grouper wants drawn whether or not a card is in them.
function seedGroups(byName, g) {
  for (const name of (g && g.always) || []) {
    if (!byName.has(name)) byName.set(name, []);
  }
}

// What an empty group says. There is no dragging onto a group, by design (see
// "moving a card by hand"), so it names the menu entry that files a card.
function emptyGroupHint() {
  return `<div class="empty groupempty">nothing in it yet. file a card here with
    <b>into group</b> on the card's menu</div>`;
}

// Is this group shut?
function isFolded(key) { return foldedColumns().includes(key); }

// Every fold on the board is written down from ONE listener, on the event the
// browser fires when a `<details>` actually changes state.
//
// It used to be an `onclick` on each summary, which recorded the state being
// LEFT rather than the state arrived at, and only for the one way of getting
// there. Anything that opened a group without a click, and every repaint that
// re-asserted the attribute, went unrecorded, so the stored answer and what
// was on screen could disagree and the next paint would pick the stored one.
// The symptom is a group you shut opening on its own.
//
// `toggle` does not bubble, so this captures. One listener rather than one per
// element, because the board's markup is rebuilt by `morphChildren` and a
// listener attached to an element does not survive being replaced.
addEventListener("toggle", e => {
  const el = e.target;
  if (!el || el.nodeType !== 1 || !el.dataset) return;
  const key = el.dataset.fold;
  if (!key) return;
  const list = foldedColumns();
  const i = list.indexOf(key);
  // `el.open` is the state ARRIVED AT here, unlike the click handler this
  // replaced, so there is no off-by-one to reason about.
  //
  // ONE GROUP STORES THE OPPOSITE, and it has to be said here rather than
  // where it is drawn. The list records a group that is NOT AS IT COMES: for
  // every group on the board that means shut, and for the offline group, which
  // comes shut, it means open. Without this line opening that group writes
  // nothing down and the next repaint closes it again, which reads as the
  // board refusing to let you look.
  const inverted = key.startsWith("offline:");
  const shut = inverted ? !!el.open : !el.open;
  // NOTHING TO WRITE IS THE COMMON CASE AND IT HAS TO RETURN HERE. A repaint
  // re-asserts the `open` attribute, setting an attribute fires `toggle`, and
  // a `refresh()` below on a toggle that changed nothing is a repaint loop.
  if (shut === (i >= 0)) return;
  if (shut) list.push(key); else list.splice(i, 1);
  localStorage.setItem("atrium.folded", JSON.stringify(list));
  // A column's WIDTH depends on whether any of its groups are open: a column
  // with everything shut is rows of nothing taking a fifth of the board.
  // Waiting for the next poll means reading a freshly opened group in a narrow
  // column for five seconds.
  refresh();
}, true);

// The last set of cards either view drew, so something built on demand can
// find the card it belongs to without re-fetching. Written by both, because
// only one of them runs at a time and both need the same answer.
let lastTasks = [];

// How many supervised sessions are mid-turn, in the header.
//
// `activity.what` is what a runner is DOING, held in memory by the daemon and
// fed by its hooks. `idle` and empty both mean sitting at a prompt, so anything
// else is a turn in flight. The same test the restart script uses, because it
// is the same question: how much work would stopping right now interrupt.
//
// Painted from whichever list just loaded rather than fetching its own. Every
// view already asks for the tasks, and a header that polled separately would
// be a second answer arriving at a different moment from the cards it counts.
//
// NOT DRAWN AT ZERO. A chip that says nothing is running is a chip you read
// every time to learn nothing, and the quiet state is the common one.
function paintWorking(tasks) {
  const el = document.getElementById("working");
  if (!el) return;
  const n = (tasks || []).filter(t =>
    t.supervised && t.activity && t.activity.what && t.activity.what !== "idle").length;
  el.hidden = n === 0;
  if (!n) return;
  el.textContent = n === 1 ? "1 working" : n + " working";
  el.title = n === 1
    ? "one session is mid-turn. click to go to the terminals"
    : n + " sessions are mid-turn. click to go to the terminals";
}

// WHEN THE TITLE CANNOT TELL TWO CARDS APART, FALL BACK TO THE WIRE NAME.
//
// The board shows `display_title` everywhere, which is the right thing: it is
// the readable name of the work, and a launcher that passed a title said what
// to call it. But several sessions launched into one worktree derive the same
// title from the same repo and branch, and then the board draws a column of
// identical cards with no way to say which is which.
//
// The wire name is unique per session even when the title is not, so the tie is
// broken by appending it. Only for the cards that actually collide, so a board
// where every title is already distinct is left completely alone. Stable across
// polls because it keys off the wire name, not off draw order, so the
// reconciler still keeps the row it did not have to rebuild.
//
// This is a hub-side display fix. It cannot make the derived names themselves
// unique -- that is the launcher's job, and lands room-side -- but it stops the
// operator from staring at duplicates on a board that already knows the
// difference.
function disambiguateTitles(tasks) {
  const counts = {};
  for (const t of tasks) {
    const key = t.display_title || "";
    counts[key] = (counts[key] || 0) + 1;
  }
  for (const t of tasks) {
    const key = t.display_title || "";
    if (counts[key] > 1 && t.wire_name) {
      t.display_title = key + " (" + localWire(t.wire_name) + ")";
    }
  }
}

// localWire strips this atrium's tenant prefix off a wire name, so a card on a
// single-machine board does not repeat the machine's name. The separator is a
// slash, and a local name never contains one (see store/tenant.go), so the tail
// after the last slash is the session's own name.
function localWire(name) {
  const i = name.lastIndexOf("/");
  return i >= 0 ? name.slice(i + 1) : name;
}

async function renderBoard(signal) {
  const { tasks } = await api("/v1/tasks", { signal });
  // SPLIT ONCE, HERE, AND NOT AT EVERY PLACE THAT COUNTS SOMETHING.
  //
  // Everything below this line works on cards from rooms that are answering,
  // which is what makes every number on the board live only WITHOUT anybody
  // remembering to filter: the column counts, the width each column earns, the
  // working indicator, and anything reading `lastTasks`.
  //
  // A cached card last seen as running or blocked is not three things to do. It
  // is work on a machine you cannot reach, and a number that includes it is
  // wrong in the way that costs most, because it is a number you will look at.
  //
  // The offline ones are drawn from `offline`, in their own group, which
  // carries its own count.
  const everything = tasks || [];
  disambiguateTitles(everything);
  const all = everything.filter(t => !t.offline);
  const offline = everything.filter(t => t.offline);
  lastTasks = everything;
  paintWorking(all);
  const g = grouper();

  const html = COLUMNS.filter(col => {
    // The inbox is the one column that should not be there when it is empty.
    //
    // Every other column earns its header while empty: it is somewhere a card
    // can be filed, or "nothing needs permission" is worth reading. Neither is
    // true here. Nothing files into the inbox, and nobody who has not wired up
    // a source has anything to learn from an empty one.
    if (!col.hideWhenEmpty) return true;
    // Work on a machine that is not answering still earns the column its
    // header. The inbox vanishing while it holds a shut laptop's cards would
    // be the board hiding the thing it was asked to remember.
    return all.some(t => col.statuses.includes(t.status)) ||
      offline.some(t => col.statuses.includes(t.status));
  }).map(col => {
    const mine = columnOrder(all.filter(t => col.statuses.includes(t.status)));
    const folded = foldedColumns().includes(col.id) ? " folded" : "";
    // A column with nothing to SHOW gives its width back rather than holding
    // an equal share for nothing. On a normal morning three of the five are
    // empty, and an even split means the two columns you are actually reading
    // get a third of the screen between them.
    //
    // Nothing to show, not nothing to hold: `finished` collapses its done and
    // dead groups by default, so a column with six cards in it can be six rows
    // of nothing while taking a fifth of the board. Folding a group is saying
    // you are not reading it, and the width should follow.
    //
    // The header stays either way. A column that vanished when it emptied
    // would take with it the answer to what is in it, and "nothing needs
    // permission" is worth being able to see.
    const shown = col.statuses.length === 1
      ? mine.length
      : col.statuses.filter(s => !foldedColumns().includes("group:" + s)).length;
    // Approving everything means nothing can arrive here, so an empty
    // permissions column is not "nothing is blocked right now", it is a column
    // that cannot fill. It still shows if something IS in it: a shelved card
    // or a never rule still blocks, and one of those sitting there while the
    // header says everything is approved is the single most confusing thing
    // the board could hide.
    // Approving everything means nothing can arrive here, so the column is not
    // reporting "nothing is blocked", it is a column that cannot fill. Gone
    // entirely rather than narrowed: a strip saying `empty` under a header
    // saying APPROVING EVERYTHING is a place for your eye to go for no reason.
    //
    // Only while it is empty. A shelved card or a never rule still blocks, and
    // one of those sitting here while the header says everything is approved is
    // the single most confusing thing the board could hide, so `mine.length`
    // is part of the test and not an afterthought.
    const moot = col.id === "needs-permission" && globalAuto && mine.length === 0;
    if (moot) return "";
    // Nothing in it, which is not the same question `shown` answers.
    //
    // `shown` counts the groups that are OPEN, so a column holding several
    // statuses read as occupied whenever one of its groups was expanded, even
    // with nothing in either. `finished` kept a full column's width to say
    // DONE 0 and DEAD 0 and then `empty` underneath, three ways of saying the
    // same nothing.
    const bare = mine.length === 0;
    // `moot` is not here: that column has already returned above.
    const vacant = (bare || shown === 0) ? " vacant" : "";

    // A column holding one status is a flat list. One holding several splits
    // into groups, each with its own count and its own clear. Project groups
    // nest inside either.
    // An empty column holding several statuses drops its group headings too.
    // Keyed on `bare` rather than on `vacant`, because a column can be narrow
    // for two other reasons and both of those still have cards to show.
    // What this column holds from rooms that are not answering, drawn under
    // everything live in one group of its own. Worked out here because the
    // split happened at the top: nothing below this line has to know.
    const lost = offline.filter(t => col.statuses.includes(t.status));
    const body = ((bare && !lost.length && col.statuses.length > 1)
      ? `<div class="empty">empty</div>`
      : col.statuses.length === 1
        ? cardsHTML(mine, g, col.id)
        : col.statuses.map(s =>
            groupHTML(s, all.filter(t => t.status === s), g)).join("")) +
      (lost.length ? offlineGroupHTML(lost, g, col.id) : "");

    // Width follows how much is in it.
    //
    // Every column was `flex: 1 1 0`, so one card and nineteen got the same
    // share and `running` sat as a mostly empty half of the board next to a
    // `ready` that had to scroll.
    //
    // The square root, not the count. A column with four times the cards is
    // not worth four times the width: a card is a fixed height, so the extra
    // width buys nothing but a longer title, and a straight ratio would give
    // one busy column the whole board.
    //
    // NOT ROUNDED, and that is the whole of what the square root was for.
    // `Math.round(Math.sqrt(n))` has two values under a cap of two: `sqrt(2)`
    // rounds to 1 and `sqrt(3)` rounds to 2, so a column jumped from narrowest
    // to widest between two cards and three, and three cards and fifty one
    // came out identical. A weight that follows content and then throws away
    // every step except one boundary is a constant with extra arithmetic.
    // `flex-grow` takes fractions, so it does not need to be an integer.
    //
    // The cap is FOUR and it divides the surplus only. Every column starts at
    // a basis of 268 with a `min-width` to match, so no weight can squeeze the
    // quiet column below the width its own cards need: that is what the basis
    // is for, and it is why the old cap of two is not load bearing any more.
    // The cap that remains is about how lopsided the leftover may get, not
    // about protecting anybody.
    //
    // The comment that used to sit here said the basis was ZERO and justified
    // the cap of two with it. That has been false since the basis became 268,
    // and the stylesheet says so at length where `.board .col` is declared.
    const weight = Math.min(4, Math.max(1, Math.sqrt(mine.length)));

    return `<section class="col${folded}${vacant}" data-column="${col.id}"
      ${vacant ? "" : `style="flex-grow:${weight.toFixed(2)}"`}>
      <div class="col-head" onclick="toggleColumn('${col.id}')">
        <span><span class="fold">&#9662;</span>${esc(col.label)}</span>
        <span class="help" tabindex="0" onclick="event.stopPropagation()"
          data-tip="${esc(col.why)}">?</span>
        ${col.statuses.length === 1 && PRUNABLE.includes(col.statuses[0]) && mine.length
          ? `<span class="chip sweep" title="delete every card in this column"
               onclick="event.stopPropagation();pruneColumn('${col.statuses[0]}')">clear</span>` : ""}
        <span class="n">${mine.length}</span>
      </div>
      <div class="cards">${body}</div>
    </section>`;
  }).join("");
  // Nothing to re-wire. Every handler a card has is an attribute in its own
  // markup, so it comes back with the card. The listeners this used to attach
  // on every redraw were the drag ones, and there are none.
  setHTML(document.getElementById("board"), html);

  // Reported after the render, so a broken function shows an error and a board
  // drawn with the defaults rather than a blank page.
  if (groupingFault) {
    toast("grouping code", groupingFault);
    groupingFault = "";
  }
}

// What order the cards come out in, inside whatever they are grouped by.
//
// The board used to draw whatever order the API returned, which is `rank`.
// Rank is the order somebody set by hand with the card menu's up and down, and
// nothing else writes it. That is a real answer for a column you arrange
// yourself and no answer at all for the rest: a card's rank was set while it
// was live and nothing touches it when the work ends, so the finished column
// came out ten days, nine, eight, one, forty seconds, ten days again.
//
// So the order is a choice now, and it is on screen. The same two the terminals
// page offers, because they are the same question asked about the same cards
// and a third vocabulary would be one more thing to hold. `manual` is the third
// only because rank still exists and something has to be able to reach it.
//
// APPLIED INSIDE THE GROUPING, NOT ACROSS IT. `cardsHTML` buckets by project or
// tag while preserving the order it was handed, so sorting here sorts within
// every group without touching which groups there are or what order they come
// in. Group headings stay alphabetical, which is the rule the terminal strip
// already follows and for the same reason: a heading that moved with its
// contents would make the list rearrange itself while you read it.
const BOARD_SORTS = {
  activity: {
    label: "activity",
    title: "most recently active first. for a card that is over, that is when it finished"
  },
  name: {
    label: "name",
    title: "a to z, by what the card is called"
  },
  manual: {
    label: "manual",
    title: "the order you set with a card's move up and move down. that is the only " +
      "thing that writes it, so this is the one sort that holds still"
  }
};

const BOARD_SORT_KEY = "atrium.boardsort";

// Activity by default, which is what the board was reaching for and missing.
function boardSortMode() {
  const v = localStorage.getItem(BOARD_SORT_KEY);
  return BOARD_SORTS[v] ? v : "activity";
}

function setBoardSort(v) {
  if (!BOARD_SORTS[v]) return;
  localStorage.setItem(BOARD_SORT_KEY, v);
  paintBoardSort();
  refresh();
}

function paintBoardSort() {
  const el = document.getElementById("board-sort");
  if (!el) return;
  const mode = boardSortMode();
  setHTML(el, Object.entries(BOARD_SORTS).map(([v, s]) =>
    `<button class="${v === mode ? "on" : ""}" onclick="setBoardSort('${v}')"
       title="${esc(s.title)}">${esc(s.label)}</button>`).join(""));
}

function columnOrder(cards) {
  const mode = boardSortMode();
  // Rank is what the API already ordered by, so manual is the list untouched.
  if (mode === "manual") return cards;
  const by = mode === "name"
    ? (a, b) => (a.display_title || "").localeCompare(b.display_title || "")
    : (a, b) => (a.idle_seconds || 0) - (b.idle_seconds || 0);
  // `cardTieBreak` is the shared one. Two cards at the same age with no
  // tiebreak swap places between polls, which reads as the board twitching.
  return cards.slice().sort((a, b) => by(a, b) || cardTieBreak(a, b));
}

// Statuses the daemon will delete. Anything else has no clear control, because
// pressing one that does nothing is worse than not having it.
const PRUNABLE = ["done", "dead"];

// One status inside a column that holds several. Collapsible, so `dead` can be
// shut away while `done` stays open.
//
// `data-status` is this element's RECONCILER KEY and nothing else reads it. It
// used to be the drop target's answer to "what status is a card dropped here",
// and it outlived that: `morphKey` matches a group across a repaint by it, and
// a group with no key is destroyed and rebuilt every paint, which takes the
// scroll position and any selection inside it along.
// offlineGroupHTML is the one group holding every unreachable room's cards.
//
// LAST IN ITS COLUMN AND SHUT UNLESS SOMEBODY OPENED IT. What is running is
// what the board is for and is never pushed down the page by what is not, and
// these cards cannot be opened or changed until that machine is back: open,
// they are a screen of things that do not respond to being clicked. Shut, they
// are one line saying the work still exists, which is the fact worth carrying.
//
// ONE GROUP, NOT ONE PER ROOM. It is the same kind of thing, which is work you
// cannot touch right now, and four headings for four dead laptops is four times
// the furniture for one fact.
//
// Shut is the opposite default to every other group here, which is why this
// cannot just pass a key to `groupHTML`. The fold list records what was CHANGED
// from its default, so a key absent from it means "as it comes", and as it
// comes for this one is closed.
function offlineGroupHTML(cards, g, keyPrefix) {
  const key = "offline:" + keyPrefix;
  const open = isFolded(key) ? " open" : "";
  // The rooms these came from, said once at the top rather than on every card.
  const rooms = [...new Set(cards.map(t => t.room).filter(Boolean))].sort();
  return `<details class="cardgroup offline" data-fold="${esc(key)}"${open}>
    <summary>
      <span class="gname">not answering</span>
      <span class="gn">${cards.length}</span>
      <span class="chip">${esc(rooms.join(", "))}</span>
    </summary>
    <div class="hintline">What ${rooms.length === 1 ? "that machine" : "those machines"}
      last said ${cards.length === 1 ? "was" : "were"} here. Nothing on
      ${rooms.length === 1 ? "it" : "them"} can be opened or changed until
      ${rooms.length === 1 ? "it is" : "they are"} back.</div>
    ${cards.map(cardHTML).join("")}
  </details>`;
}

// A GROUP THAT DEFAULTS TO SHUT INVERTS THE FOLD LIST, and that is why the
// offline group reads `isFolded` as "open". The list is a record of what was
// toggled away from its default, not a list of closed things, so one entry
// means the same for both kinds of group: somebody changed their mind about
// this one.

function groupHTML(status, cards, g) {
  const shut = isFolded("group:" + status) ? "" : " open";
  return `<details class="cardgroup" data-status="${esc(status)}"${shut}
    data-fold="group:${esc(status)}">
    <summary>
      <span class="gname">${esc(status)}</span>
      <span class="gn">${cards.length}</span>
      ${PRUNABLE.includes(status) && cards.length
        ? `<span class="chip sweep" title="delete every ${esc(status)} card"
             onclick="event.stopPropagation();event.preventDefault();pruneColumn('${status}')"
             >clear</span>` : ""}
    </summary>
    ${cardsHTML(cards, g, status)}
  </details>`;
}

// Groups reuse the folded-columns list, prefixed, so open and shut survives a
// reload the same way a column does. Written down by the `toggle` listener
// above, which every group on the board shares.

// ── moving a card by hand ───────────────────────────────
//
// There is no dragging. It was the kanban idea, it was the only way to change
// a card's status by hand, and it cost more than it earned: `draggable` on a
// card means a pointerdown starts a drag rather than a selection, so a path, a
// branch, a wire name or an error printed on a card could not be selected to
// be copied. Copying one of those off a card is a daily thing. Moving a card
// by hand is not, and a menu entry is the right weight for it.
//
// What replaced it is below, and it is the same two operations the drop did: a
// status change from `moveItem`, and a reorder from `nudgeCard`.

// The columns a card can be filed into, as a flyout.
//
// Built from COLUMNS rather than from a list of statuses, so a column added to
// the board turns up here without anybody remembering to, and `accepts: false`
// keeps one out for the reason it always had.
//
// `shelved` is left out although it accepts a card, because shelving costs
// something: it stops the runner, and the `shelve` entry is where that is
// said. Two ways to shelve is how two descriptions of it drift apart.
//
// Labelled by STATUS rather than by column, because the status is what the
// card becomes and `finished` is a column holding two of them. Filing into it
// means `done`, and an entry reading `finished` would not say which.
function moveItem(id, t) {
  const to = COLUMNS
    .filter(c => c.accepts !== false && c.id !== "shelved")
    .map(c => c.takes || c.statuses[0])
    .filter(s => s !== t.status);
  if (!to.length) return null;
  return {
    label: "move it to…",
    help: "Files this card in another column. The ones not offered are " +
      "reports rather than buckets: a card is in needs permission or in ready " +
      "because an agent said so, and filing one there by hand would claim " +
      "something that never happened.",
    sub: to.map(s => ({
      label: statusLabel(s),
      act: () => patchTask(id, { status: s }).then(refresh)
    }))
  };
}

// Up and down, one place at a time, within the list the card is drawn in.
//
// `rank` is the operator's own order inside a column, and dragging was the
// only thing that ever wrote it. Without these two entries the field would
// still sort every column and nothing could set it, which is a column ordered
// by a number nobody can reach.
//
// Offered as a pair and never hidden AT THE END OF A LIST. Pressing one there
// does nothing, which is what the end of a list is, and hiding the entry would
// move every row under the pointer between one card and the next.
//
// OFFERED WHEREVER THE ORDER IS HELD TO RANK: the pinned bucket and every
// custom group, in any view and under any sort, because the sort pill only
// reorders the groups the board makes for itself. Anywhere else it is hidden
// unless the board sort is manual. There the sort would throw the new rank away
// on the next paint, and a control that visibly does nothing is worse than one
// that does nothing at the end of a list, which at least has an end to blame.
function nudgeItems(id, t) {
  if (!handList(t) && boardSortMode() !== "manual") return null;
  return {
    label: "move it up or down",
    help: "Its place in the pinned bucket or in your group, which is yours " +
      "to set: nothing else writes it, and no sort moves it.",
    sub: [
      { label: "up", act: () => nudgeCard(id, -1) },
      { label: "down", act: () => nudgeCard(id, 1) }
    ]
  };
}

// Moves a card one place, past exactly one neighbour.
//
// The neighbours come off the SCREEN rather than out of a fetch, because what
// is on screen is the list somebody is looking at when they press this: a
// column is split by status, then possibly by project or by pinned, and the
// list that matters is the innermost one the card is drawn in.
//
// The new rank is the midpoint between the card it passes and whatever is on
// the far side of that, which is the arithmetic the drop used and the reason
// the column is ordered by a float: a move renumbers nothing else.
async function nudgeCard(id, delta) {
  const board = document.getElementById("board");
  const card = board && !board.hidden
    ? board.querySelector('.card[data-id="' + id + '"]') : null;
  let ranks;
  if (card) {
    const scope = card.closest(".cardgroup") || card.closest(".col");
    const cards = [...scope.querySelectorAll(".card")];
    ranks = { at: cards.indexOf(card), of: cards.map(el => Number(el.dataset.rank)) };
  } else {
    // THE STACK AND THE STRIP draw no ranks to count, so the list comes from
    // the cards themselves: the pinned bucket, or the first of your groups the
    // card is in. It is the same list those views draw, in the same order.
    const t = lastTasks.find(x => x.id === id);
    const hand = t && handList(t);
    if (!hand) {
      toast("cannot move it", "it is not in the pinned bucket or one of your groups");
      return;
    }
    ranks = { at: hand.indexOf(t), of: hand.map(x => x.rank || 0) };
  }
  const to = ranks.at + delta;
  if (ranks.at < 0 || to < 0 || to >= ranks.of.length) return;

  const past = ranks.of[to];
  // What is on the far side of the card being passed, or nothing when that
  // card is the end of the list. Ranks ascend down the column, so a step off
  // either end is one whole rank past it rather than a midpoint.
  const beyond = to + delta >= 0 && to + delta < ranks.of.length ? ranks.of[to + delta] : null;
  const rank = beyond === null ? past + delta : (past + beyond) / 2;

  await patchTask(id, { rank });
  refresh();
}

// The hand-ordered list a card is in, in rank order, or null when it is in none.
// Pinned wins, because a pinned card is drawn in the pinned bucket and nowhere
// else. A card in several of your groups moves in the first of them, in the
// order the groups are listed. Its rank is one number, so it moves in the others
// too.
function handList(t) {
  if (!t) return null;
  if (t.pinned) return lastTasks.filter(x => x.pinned).sort(byRank);
  const p = groupingPrefs();
  if (!p.on || p.mode !== "custom") return null;
  const tags = new Set(t.tags || []);
  const name = (Array.isArray(p.groups) ? p.groups : []).find(n => tags.has(n));
  if (!name) return null;
  return lastTasks.filter(x => !x.pinned && (x.tags || []).includes(name)).sort(byRank);
}

async function patchTask(id, body) {
  try {
    const res = await api(`/v1/tasks/${id}`, {
      method: "PATCH", headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body)
    });
    // Shelving stops a runner and unshelving starts it again, neither of which
    // is visible from a card moving between columns.
    if (body.status === "shelved") toast("shelved", "its runner was stopped");
    if (res && res.resumed) { toast("resumed", "picked up where it left off"); return res; }
    if (res && res.resume_note) {
      // Nothing started. A card in `running` with no process is the lie this
      // whole board exists to avoid, so it is corrected here rather than left
      // for the operator to notice. Handled in one place, so the menu, the
      // dialog and an agent's own call all end up somewhere true.
      if (body.status === "running") {
        await api(`/v1/tasks/${id}`, {
          method: "PATCH", headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ status: "dead" })
        });
        toast("off the shelf, but not running", res.resume_note);
      } else {
        toast("not resumed", res.resume_note);
      }
    }
    return res;
  } catch (e) { toast("that did not stick", e.message); }
}

