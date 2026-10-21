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

  // A node that is re-inserted restarts its CSS animations and loses its hover, so a reorder moves as few as it can.
  // The keyed rows already in the wanted order stay where they are (the longest increasing run of old positions) and
  // only the rest are moved. Without it one row sinking from first to last re-inserted every row it passed.
  const stay = keptInOrder(parent, source, keyed);
  const claimed = new Set();

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
      claimed.add(mine);
      if (mine !== cursor) {
        // Nodes between the cursor and a node that stays are keyed ones moving elsewhere or going stale: stepped over.
        if (stay.has(mine) && onlyKeyedBetween(cursor, mine)) cursor = mine;
        else { parent.insertBefore(mine, cursor); changed = true; }
      }
      if (morphNode(mine, want)) changed = true;
      if (mine === cursor) cursor = mine.nextSibling;
    } else {
      // New. Adopted from the parsed copy rather than cloned, since the copy
      // is thrown away either way.
      claimed.add(want);
      parent.insertBefore(want, cursor);
      changed = true;
    }
    want = after;
  }

  // Whatever was never claimed is gone.
  for (let n = parent.firstChild; n;) {
    const next = n.nextSibling;
    if (!claimed.has(n)) { parent.removeChild(n); changed = true; }
    n = next;
  }
  return changed;
}

// The keyed children already on the page that can stay where they are: of those the new markup still wants, the
// longest run whose old positions already run in the wanted order.
function keptInOrder(parent, source, keyed) {
  const at = new Map();
  let i = 0;
  for (let n = parent.firstChild; n; n = n.nextSibling) at.set(n, i++);
  const seen = new Set();
  const nodes = [];
  for (let w = source.firstChild; w; w = w.nextSibling) {
    const k = morphKey(w);
    const n = k && !seen.has(k) ? keyed.get(k) : null;
    if (!n) continue;
    seen.add(k);
    nodes.push(n);
  }
  const tails = [], prev = new Array(nodes.length).fill(-1);
  for (let j = 0; j < nodes.length; j++) {
    const v = at.get(nodes[j]);
    let lo = 0, hi = tails.length;
    while (lo < hi) { const mid = (lo + hi) >> 1; if (at.get(nodes[tails[mid]]) < v) lo = mid + 1; else hi = mid; }
    if (lo > 0) prev[j] = tails[lo - 1];
    tails[lo] = j;
  }
  const out = new Set();
  for (let j = tails.length ? tails[tails.length - 1] : -1; j >= 0; j = prev[j]) out.add(nodes[j]);
  return out;
}

// Whitespace between rows is unkeyed too and is drawn again whenever it is skipped, which costs nothing to see.
function onlyKeyedBetween(from, to) {
  for (let n = from; n && n !== to; n = n.nextSibling) {
    if (!morphKey(n) && !(n.nodeType === 3 && !n.nodeValue.trim())) return false;
  }
  return !!from;
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

const isWaiting = cardWaiting;

const VIEWS = ["board", "stack", "perms", "runners", "terms", "chat", "history", "usage", "audit", "chatter", "pulls", "hubrepos", "backlog", "items"];

// BACK AND FORWARD, over the board's own moves.
//
// The board is one page that swaps views and attaches terminals, so the
// browser's back button either did nothing or left atrium entirely. Every move
// somebody makes is a place they might want to return to, and the two they
// make constantly are switching view and switching session.
//
// THE ADDRESS BAR CARRIES THE VIEW, AS A ROUTE THAT STARTS WITH `#/`. See js/route.js.
//
// `#term=<id>` already means something here: it is how a popped-out window
// resolves its card on load. A route always starts with `#/` and a pop-out
// address with `#term=`, so the two strings can never be mistaken for each
// other and a reload of a solo window has nothing to negotiate. A solo window
// pushes no route at all.
//
// `pushState` carries a state object and the route as the URL, so back and
// forward step through views, repos sub-views and change requests, and a copied
// address lands on the same place. `localStorage` still answers "where was I
// yesterday" for a bare address. History answers "where was I a moment ago"
// inside this visit, and a route answers "go here".
let navApplying = false;

function navState() {
  const r = routeNow();
  return {
    atrium: true,
    view: r.view,
    sub: r.sub,
    reqTab: r.tab,
    req: r.req,
    card: termTask ? termTask.id : "",
    // The item whose page is open on the items tab, so back closes the page rather than leaving the tab.
    item: typeof itemsOpenId === "function" && isViewing("items") ? itemsOpenId() : ""
  };
}

// Pushed only when something actually changed.
//
// Without this every repaint that re-ran `switchView` for the view already
// showing would stack an entry, and back would appear broken by needing to be
// pressed eleven times to move once.
function navPush() {
  if (termOnly() || navApplying) return;
  const now = navState();
  const was = history.state;
  if (was && was.atrium && routeHash(routeOfState(was)) === routeHash(routeOfState(now)) && was.card === now.card && (was.item || "") === now.item) return;
  // The FIRST entry is replaced rather than pushed, or the first press of back
  // leaves the board for whatever was open in this tab before it.
  if (was && was.atrium) history.pushState(now, "", routeURL(now));
  else history.replaceState(now, "", routeURL(now));
  routeRecord(now);
}

// What the board settled on by itself, such as the first request picked once the list arrived. It rewrites the entry
// rather than adding one, so back does not step through things nobody did.
function navReplace() {
  if (termOnly() || navApplying || !history.state || !history.state.atrium) return;
  const now = navState();
  history.replaceState(now, "", routeURL(now));
  routeRecord(now);
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
  routeRecord(s);
  const settled = routeApply(routeOfState(s));
  navApplying = true;
  try {
    if (s.view === "items" && typeof itemsShow === "function" && (s.item || "") !== itemsOpenId()) itemsShow(s.item || null, true);
    if (s.view === "terms" && s.card && (!termTask || termTask.id !== s.card)) {
      await attachTask(s.card);
    }
  } finally {
    navApplying = false;
  }
  await settled;
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
  if (name !== "terms" && typeof lrLeave === "function") lrLeave("tab");
  document.querySelectorAll(".tab[data-view]").forEach(t => t.classList.toggle("on", t.dataset.view === name));
  VIEWS.forEach(v => document.getElementById(v).hidden = v !== name);
  // The session list is lent to the chat view while it is open. See js/chat.js.
  if (typeof chatView === "function") chatView(name === "chat");
  // The board's toolbar is a sibling of the board rather than a child, so that
  // redrawing the columns does not take the control you just clicked with it.
  document.getElementById("board-bar").hidden = name !== "board";
  document.getElementById("board-count").hidden = name !== "board";
  // The filter drawer starts shut on every tab and every switch. See js/filter-drawer.js.
  fdReset();
  // Painted on the way in rather than once at boot. The group pills are drawn
  // by `paintGroupMode`, which runs whenever the mode changes and therefore may
  // never have run; this needs the same, and the bar is cheap to redraw.
  if (name === "board") { paintBoardShow(); paintBoardSort(); }
  // Fetched when you go there rather than on every poll. It is a question you
  // go looking for, and a query against a table that only grows has no
  // business running every few seconds while you are reading something else.
  if (name === "history") renderHistory(false);
  // Read when you go there, like history. It is kept current by `usage` events
  // while it is open. See js/usage-charts.js.
  if (name === "usage" && typeof loadUsageTab === "function") loadUsageTab();
  // A command box built while this view was hidden measured as zero, exactly
  // as a detached one does, so it is sized on the way in rather than only on
  // the way past a poll.
  if (name === "perms") sizeCommands();
  // Anything measured while this view was hidden was measured against zeroes
  // and discarded, so the bridge is placed on the way IN, on a layout that has
  // a size. Cheap enough to sit on every scroll of the list, so it is cheap
  // enough to sit here.
  if (name === "terms") requestAnimationFrame(placeTabBridge);
  if (name === "terms" && typeof lrReturn === "function") requestAnimationFrame(() => lrReturn("tab"));
  placeToasts();
  // Which card this window is reading just changed if the terminals view came or went.
  sayWhetherFocused();
  // Fetched on the way in, like history: a feed you go looking for, not one
  // worth polling while you are reading something else. It also refreshes on an
  // `audit` delta and on stream reconnect. See js/audit.js.
  if (name === "audit" && typeof loadAudit === "function") loadAudit();
  // Read on the way in and on `more`, never polled and not live. See js/chatter.js.
  if (name === "chatter" && typeof loadChatter === "function") loadChatter(false);
  // Read once. The rows are kept current by `pr` events and by the stream reopening. See js/pulls.js.
  if (name === "pulls" && typeof loadPulls === "function" && !pulls.loaded) loadPulls();
  // Read when you go there, and on its refresh button. No stream event. See js/hubrepos.js.
  if (name === "hubrepos" && typeof loadHubRepos === "function") loadHubRepos();
  // Read on the way in, and again on a `backlog` or `report` event. See js/backlog.js.
  if (name === "backlog" && typeof loadBacklog === "function") loadBacklog();
  // Read on the way in, on an `item` event, and every 5s while it is on screen. See js/items.js.
  if (name === "items" && typeof startItems === "function") startItems();
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
  repaintLists();
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
document.querySelectorAll(".tab[data-view]").forEach(t => t.onclick = () => switchView(t.dataset.view));

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
  if (a && a.looks_idle && !isWaiting(t) && t.status !== "paused" && !staleActivity(t)) return looksIdleChip(a);
  if (!a || !a.what || isWaiting(t) || t.status === "paused" || staleActivity(t)) return "";
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
    ? `<span class="chip sub" data-tip="${esc(subagentTitle(a))}"
         onclick="event.stopPropagation();toggleSubagents(this)"
         >&#8618; ${subs}</span>`
    : "";
  // Compacting is the one state worth explaining, because the right response
  // to it is to do nothing: the session is rewriting what it knows, so a
  // message sent now lands in a conversation that is about to forget it.
  const why = a.what === "compacting"
    ? ` data-tip="this session is rewriting its context and is about to forget most of it. ` +
      `nothing says when a compaction ends, so this clears on the next thing the session does."`
    : "";
  return `<span class="chip live ${esc(a.what)}"${why}>${label}${age}</span>${sub}`;
}

// THE CARD READS RUNNING BUT ITS SCREEN HAS GONE QUIET ON AN IDLE PROMPT: the
// turn-end hook never arrived. Drawn in place of the live chip, so the spinner
// STOPS (a board that keeps animating claims work it cannot see), as a hollow
// ring with a gap in the warn colour. The daemon says it, from the pty, and takes
// it down on any output, keystroke or hook, so there is no board state to clear.
// See internal/daemon/looksidle.go.
function looksIdleChip(a) {
  const tip = `no turn-end from the agent. its screen has been idle for ${ago(a.idle_seconds || 0)}.`;
  return `<span class="chip warn icon looksidle" aria-label="${esc(tip)}" data-tip="${esc(tip)}"
    ><svg viewBox="0 0 16 16" width="12" height="12" aria-hidden="true" fill="none"
    stroke="currentColor" stroke-width="1.6" stroke-linecap="round">
    <path d="M8 2.5a5.5 5.5 0 1 0 5.5 5.5" stroke-dasharray="26 8"/></svg></span>`;
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
// until one is wired up. See docs/runtime/statusline-telemetry.md.
const CTX_WARM = 60;
const CTX_HOT = 85;

function contextChip(t) {
  const c = t.telemetry;
  // Nothing a finished card burned is worth drawing. The daemon forgets a
  // card's telemetry when its session ends, so this is a second line of
  // defence rather than the fix, in the same shape as the activity chip.
  if (!c || over(t) || t.status === "paused") return "";
  // The limits are drawn whether or not a context figure came with them. A
  // session that has just started has burned nothing and the account it is
  // running on can still be about to stop, which is the more urgent of the
  // two facts.
  const limits = limitChip("5h", c.five_hour) + limitChip("week", c.weekly);
  if (!c.pct) return limits;
  const heat = c.pct >= CTX_HOT ? " hot" : c.pct >= CTX_WARM ? " warm" : "";
  return `<span class="chip ctx${heat}" data-tip="${esc(contextTitle(c))}">ctx ${c.pct}%</span>` + limits;
}

// Past its context limit (the daemon's `warn`: the card's own limit, else the hub's for its harness, 200k for
// claude by default) a card gets the small amber mark, and atrium cycles its context. The row's context line
// (ctxLine) turns danger at the land-the-plane line. Never stored. The number is also in the card's details.
function ctxWarnMark(t) {
  const c = t.context_size;
  if (!c || over(t) || t.status === "paused") return "";
  if (!c.warn) return "";
  const tip = `past its context limit, ${limitFrom(t)} tokens. ` +
    (c.cycle ? `atrium is cycling its context. ` : `its context cycle is off. `) +
    `hover the card for its details`;
  return `<span class="chip warn icon ctxwarn" aria-label="${esc(tip)}" data-tip="${esc(tip)}"
    ><svg viewBox="0 0 16 16" width="12" height="12" aria-hidden="true" fill="none" stroke="currentColor"
      stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"
      ><path d="M2.5 4.5h11M2.5 8h11M2.5 11.5h6"/><path d="M12 10.5v3M12 15.2v.1"/></svg></span>`;
}

// THE CONTEXT LINE ALONG A ROW'S BOTTOM EDGE: the context now against the land-the-plane line, the same drawing as
// the details' meter (ctxMeter in js/peek.js), no text. The good colour under 60% of the land line, amber from 60%,
// danger from 85%, with a one-off glow as it crosses (never a loop),
// unless the card is cycling its context, which says that itself. Full at the line plus ten percent. Drawn from
// the row's own context_size, so no read of its own. Same guards as the mark.
const CTX_LINE_WARM = 0.6;
const CTX_LINE_HOT = 0.85;
function ctxLine(t) {
  const c = t.context_size;
  if (!c || over(t) || t.status === "paused" || typeof ctxMeter !== "function") return "";
  const land = landOver(t);
  // Colour by the fraction of the limit: the theme's good colour, yellow from 60%, red from 85%.
  const frac = Number(c.tokens) / (landThePlaneK(t) * 1000);
  const heat = land || frac >= CTX_LINE_HOT ? " hot" : frac >= CTX_LINE_WARM ? " warm" : "";
  const once = land && !t.new_context ? " over" : "";
  return ctxMeter(c.tokens, landThePlaneK(t) * 1000, "ctxline" + heat + once, landTip(t));
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
    data-tip="this account is at ${lim.pct}% of its ${label} limit${when}"
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
const isOutOfContact = (t) =>
  t.status === "running" && !t.pid && cardIdleAge(t) > NO_CONTACT_AFTER_SECONDS;

// The mark of a card: the runner's shape, spinning while the card is working. THE ONE RUNNING MARK.
function cardMark(t, state) {
  const mark = runnerMark(t.runner);
  if ((state || cardState(t)) !== "working") return mark;
  return mark.replace('class="rmark"', `class="rmark working ${esc((t.activity && t.activity.what) || "")}"`);
}

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
function cardHTML(t, wearAs) {
  const w = isWaiting(t);
  const state = cardState(t);
  // `wearAs` is the theme preview's: a palette from `wearOf`, or null for the skin's own look, whatever the setting says.
  const wear = wearAs === undefined ? cardWear(t) : wearAs;
  // Clickable for the actions done most, because the alternative was opening
  // the detail dialog for every pause and every attach.
  //
  // NOT draggable. A card is text you copy off far more often than a thing you
  // move, and `draggable` is not a hint: it takes the pointerdown, so the
  // selection never starts. `data-rank` stays because the menu's up and down
  // are computed from the ranks on screen.
  return `<div class="card s-${state}${w ? " waiting" : ""}${
      t.auto_approve ? " autoon" : ""}${wear ? wear.cls : ""}${newCardClass(t)}"${
      wear ? ` style="${wear.style}"` : ""}
    onclick="cardMenu(event, '${t.id}')"
    data-id="${t.id}" data-rank="${t.rank}"
    oncontextmenu="cardMenu(event, '${t.id}')">
    ${ctxLine(t)}
    <div class="card-line">
      <div class="title">${
        t.pinned ? '<span class="pin on" data-tip="pinned">&#9733;</span>' : ""}${
        cardMark(t, state)}${esc(t.display_title)}</div>
      <div class="chips">
      ${subsBadgeHTML(boardSubBadge.get(t.id))}
      ${openingChip(t)}
      ${newCardChip(t)}
      ${typeof aliasChip === "function" ? aliasChip(t) : ""}
      ${modelChip(t)}
      ${tagChips(t)}
      ${noteChip(t)}
      ${originChip(t)}
      ${recapChip(t)}
      ${activityChip(t)}
      ${ctxWarnMark(t)}
      ${contextChip(t)}
      ${seenChips(t)}
      ${w
        ? `<span class="chip state owed" data-tip="${t.status === "needs-permission"
              ? "how long this agent has been frozen waiting to be answered"
              : "how long since it finished its turn and asked for you"}"
             >${esc(statusLabel(t.status))} ${ago(cardSecs(cardWaitSeconds(t)))}</span>`
        : `<span class="chip state" data-tip="${t.status === "dead" ? "how long since this stopped"
             : t.status === "done" ? "how long since you marked it done"
             : t.status === "running" ? "how long since it last did anything"
             : "how long since anything was heard from it"}"
             >${esc(cardPaused(t) ? "paused" : statusLabel(runnerGone(t) ? "stopped" : t.status))} ${ago(cardIdleAge(t))}</span>`}
      ${t.auto_approve ? `<span class="chip auto"
        data-tip="auto mode: requests from this session are approved without asking, and recorded"
        >auto</span>` : ""}
      ${t.report_unverified ? `<span class="chip warn"
        data-tip="${esc("this session reported done at " + (t.report_sha || "") +
          ", and that commit is not in its worktree. it may have landed somewhere else.")}"
        >sha unverified</span>` : ""}
      ${t.restart_wake ? `<span class="chip"
        data-tip="${esc("typed in once after the next restart brings this runner back: " + t.restart_wake.text)}"
        >wake queued</span>` : ""}
      ${newContextChip(t)}${parkedChip(t)}
      ${typeof keepaliveChip === "function" ? keepaliveChip(t) : ""}
      ${stuckMark(t)}
      ${sharedCards.has(t.id) ? `<span class="chip shared"
        data-tip="${esc("this session is published at " + (sharedCards.get(t.id).address || "") +
          ". anyone with that address types into it as you would. right click to stop.")}"
        oncontextmenu="stopSharingChip(event, '${t.id}')"
        >shared</span>` : ""}
      ${t.status === "backlog" && !t.offline ? `<span class="chip attach"
        data-tip="open the launch dialog with what the source already knew filled in.
               this card is the target, so starting it does not make a second one"
        onclick="event.stopPropagation();startOffered('${t.id}')">start</span>` : ""}
      ${t.offline ? `<span class="chip nocontact"
        data-tip="${esc("room " + (t.room || "") + " is offline. cannot restore terminal")}"
        >&#128683;</span>` : ""}
      ${t.supervised && !t.offline ? `<span class="chip attach hoveronly"
        onclick="event.stopPropagation();attachTask('${t.id}')">attach</span>
      <span class="chip attach icon hoveronly"
        aria-label="open this terminal in its own window" data-tip="open this terminal in its own window"
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
  return `<div class="ask act" data-tip="${esc(tip)}"
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
// ── new context ─────────────────────────────────────────
// The context cycle: the limit prompt and its ack, then clear and wake, run by the
// room. The card's `new_context` says which step it is on, or why it stopped. See
// internal/daemon/newcontext.go and docs/runtime/context-cycle-design.md.

// One chip while a step runs, gone when the wake prompt lands. A step that
// stopped leaves a red one with the reason, and clicking it takes it off.
function newContextChip(t) {
  const n = t.new_context;
  if (!n) return "";
  if (n.step === "failed") {
    return `<span class="chip warn cycle" data-tip="${esc("new context stopped, and nothing further was typed. " +
      (n.reason || "") + ". click to dismiss this, or run new context again")}"
      onclick="event.stopPropagation();dismissNewContext('${t.id}')"
      >new context failed</span>`;
  }
  const word = NEW_CONTEXT_WORDS[n.step] || n.step;
  // What the operator queued while atrium was typing waits on the card until the cycle ends. The way out ("let me
  // type") is on the terminal's own bar and in the compose box. See js/compose.js.
  const q = Number(n.queued) || 0;
  const queued = q ? `<span class="chip" data-tip="typed into the card when the new context ends"
    >${q} message${q === 1 ? "" : "s"} queued</span>` : "";
  return `<span class="chip cycle" data-tip="${esc("context cycle, step " + n.n + " of " + n.of + ": " + n.label +
    (n.reason ? " (" + n.reason + ")" : ""))}"
    >context ${n.n}/${n.of}: ${esc(word)}</span>${queued}`;
}
const NEW_CONTEXT_WORDS = { limit: "waiting for ack", clear: "clearing", wake: "waking", capture: "handoff" };

// A parked card: idle, no process, waking on the first key or a say with wake.
function parkedChip(t) {
  if (!t.parked_at) return "";
  return `<span class="chip" data-tip="${esc("parked: idle with no process since " +
    new Date(t.parked_at).toLocaleTimeString() + ". press a key in its terminal, or resume it from the right-click menu")}"
    >parked</span>`;
}

async function resumeParked(id) {
  try {
    await api(`/v1/tasks/${encodeURIComponent(id)}/resume`, { method: "POST" });
    toast("resuming", "it is starting again on the same conversation");
  } catch (e) {
    toast("could not resume", e.message);
    return;
  }
  refresh();
}

async function dismissNewContext(id) {
  try {
    await api(`/v1/tasks/${encodeURIComponent(id)}/new-context`, { method: "DELETE" });
  } catch (e) {
    toast("could not dismiss", e.message);
    return;
  }
  refresh();
}

// The menu entry and Ctrl+Alt+N both land here.
async function newContext(id) {
  try {
    const r = await api(`/v1/tasks/${encodeURIComponent(id)}/new-context`, { method: "POST" });
    const file = (r && r.new_context && r.new_context.file) || "its handoff file";
    toast("new context started",
      `it is asked to write ${file} and run atrium ready, then cleared, then told to read it back`);
  } catch (e) {
    toast("could not start a new context", e.message);
    return;
  }
  refresh();
}

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
// DEFAULT_GROUP_BY and the project rule live in js/cardrules.js, which the phone page loads too.


// DEFAULT_GROUP_ORDER is in js/cardrules.js too.


// On by default. A board of leaf directory names says nothing about which
// repo any of them belong to, and the default rule works with no setup.
// `mode` is what a group IS: a project read out of the path, or a tag you
// applied. `by` and `order` stay the escape hatch for anyone who wants
// something neither of those describes.
const GROUPING_DEFAULTS = { on: true, mode: "project", by: "", order: "", orderBy: "", picked: false, hues: {}, groups: [], made: {} };

// A card with no tags still has to land somewhere when grouping by tag, and
// "" would sort it in with a group whose name is empty.
const UNTAGGED = "untagged";

// EVERY TAB KEEPS ITS OWN VIEW. On or off, the group mode, the group order and the typed code are saved per tab, in
// `atrium.grouping.<tab>`, so choosing `by pile` on the stack leaves the terminals as they were. What stays in the shared
// `atrium.grouping` is what describes the cards and not a view: the operator's named groups and every group's colour.
//
// MIGRATION IS A FALLBACK, NOT A COPY. A tab with nothing of its own yet reads the view fields the shared key held before
// the split, so nobody loses a setting, and the old key is never cleared. The first change on a tab writes its own copy.
const GROUP_VIEWS = ["board", "stack", "terms"];
const GROUP_VIEW_FIELDS = ["on", "mode", "by", "order", "orderBy", "picked"];
const GROUP_VIEW_NAMES = { board: "board", stack: "stack", terms: "terminals" };

function groupViewKey(view) { return GROUPING_KEY + "." + view; }

// The tab you are looking at, for the callers that are not drawing a particular one. The board is the fallback because
// it is the page that is always there.
function groupView() {
  if (typeof isViewing !== "function") return "board";
  return GROUP_VIEWS.find(isViewing) || "board";
}

function readJSONKey(key) {
  try { return JSON.parse(localStorage.getItem(key) || "{}") || {}; } catch (e) { return {}; }
}

function groupingPrefs(view) {
  view = GROUP_VIEWS.includes(view) ? view : groupView();
  const shared = readJSONKey(GROUPING_KEY);
  const own = readJSONKey(groupViewKey(view));
  const mine = {};
  GROUP_VIEW_FIELDS.forEach(f => { if (own[f] !== undefined) mine[f] = own[f]; });
  return Object.assign({}, GROUPING_DEFAULTS, shared, mine);
}

function setGrouping(patch, view) {
  view = GROUP_VIEWS.includes(view) ? view : groupView();
  const sharedPatch = {};
  const viewPatch = {};
  Object.keys(patch).forEach(k => (GROUP_VIEW_FIELDS.includes(k) ? viewPatch : sharedPatch)[k] = patch[k]);
  if (Object.keys(sharedPatch).length) {
    localStorage.setItem(GROUPING_KEY, JSON.stringify(Object.assign(readJSONKey(GROUPING_KEY), sharedPatch)));
  }
  if (Object.keys(viewPatch).length) {
    const now = groupingPrefs(view);
    const next = {};
    GROUP_VIEW_FIELDS.forEach(f => { next[f] = now[f]; });
    localStorage.setItem(groupViewKey(view), JSON.stringify(Object.assign(next, viewPatch)));
  }
  repaintLists();
}

// A SECOND WINDOW FOLLOWS. The grouping, its order and every group's colour are
// kept in this browser, which every window on the board shares, but only the
// window that wrote the change repainted. The others kept the old colour and
// the old order until their next poll, up to `POLL_MS` later, so a recolour
// looked like it had not stuck. The browser tells every OTHER window when a
// key changes, and that is the cue. Each tab's key is its own, so all of them are watched.
window.addEventListener("storage", e => {
  if (e.key === GROUPING_KEY || GROUP_VIEWS.some(v => e.key === groupViewKey(v))) refresh();
});

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
//   - Federation. `docs/archive/federation-design-v2.md` puts many machines behind one
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
function defaultProjectOf(task) { return cardProjectOf(task); }

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

// The group order the operator picked by name. Code when they typed a comparator or picked it, else a named order.
function groupOrderChoice(p) {
  return p.orderBy || (String(p.order || "").trim() ? "code" : "name");
}

// Whether the operator's own group-by expression is in charge. Picking `your own code` says so, and a board that saved
// code before the picker existed (no `picked`, code written) keeps running it.
function usesCode(p) {
  return p.mode === "code" || (!p.picked && !!String(p.by || "").trim());
}

// Wraps a grouper so its groups are ordered by the named order, from the cards on the board now. The hand-ordered and age
// groupers keep their own order, which is the point of them.
function withGroupOrder(g, p) {
  const o = groupOrderChoice(p);
  if (!g || g.handOrdered || o === "name" || p.mode === "recency") return g;
  if (o === "code") {
    if (usesCode(p)) return g;
    const order = compiled(p.order, DEFAULT_GROUP_ORDER, "a", "b");
    if (!order) return g;
    return Object.assign({}, g, { cmp: (a, b) => { try { return Number(order(a, b)) || 0; } catch (e) { return a.localeCompare(b); } } });
  }
  const cache = new Map();
  const cardsOf = n => {
    if (!cache.size) (lastTasks || []).forEach(t => (g.of(t) || []).forEach(k => {
      if (!cache.has(k)) cache.set(k, []);
      cache.get(k).push(t);
    }));
    return cache.get(n) || [];
  };
  return Object.assign({}, g, { cmp: groupOrderCmp(o, cardsOf) });
}

function grouper(view) {
  view = GROUP_VIEWS.includes(view) ? view : groupView();
  const g = withGroupOrder(grouperByMode(view), groupingPrefs(view));
  // Says which tab it was made for, so what draws the groups reads that tab's setting and not whichever is on screen.
  return g ? Object.assign(g, { view }) : g;
}

function grouperByMode(view) {
  const p = groupingPrefs(view);
  if (!p.on) return null;
  groupingFault = "";

  // A named rule from js/cardrules.js. A rule may answer a list (a card under several prefixes).
  if (GROUP_RULES[p.mode] && !usesCode(p)) {
    const rule = GROUP_RULES[p.mode];
    return { many: p.mode === "prefix", of: t => { const r = rule(t); return Array.isArray(r) ? (r.length ? r : [""]) : [r]; },
      cmp: groupOrderCmp("name") };
  }

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
  if (p.mode === "recency" && !usesCode(p)) {
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
  if (p.mode === "window" && !usesCode(p)) {
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
  if (p.mode === "tag" && !usesCode(p)) {
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
  if (p.mode === "custom" && !usesCode(p)) {
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

  const by = compiled(usesCode(p) ? p.by : "", DEFAULT_GROUP_BY, "task");
  // The typed comparator only when it was picked. A named order is applied by withGroupOrder.
  const order = groupOrderChoice(p) === "code" ? compiled(p.order, DEFAULT_GROUP_ORDER, "a", "b") : null;
  if (!by) return null;

  const safeBy = t => {
    try { return String(by(t) ?? ""); }
    catch (e) {
      groupingFault = groupingFault || "your grouping code threw: " + e.message;
      return "";
    }
  };
  const safeOrder = (a, b) => {
    if (!order) return groupOrderCmp("name")(a, b);
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
  const name = groupNameOf(raw);
  if (!name) return;
  const p = groupingPrefs();
  const groups = Array.isArray(p.groups) ? p.groups.slice() : [];
  if (groups.includes(name)) {
    toast("already a group", "the view already has one called " + name);
    return;
  }
  groups.push(name);
  setGrouping({ groups, made: Object.assign({}, p.made, { [name]: Date.now() }) });
}

function removeCustomGroup(name) {
  const p = groupingPrefs();
  const groups = (p.groups || []).filter(x => x !== name);
  const made = Object.assign({}, p.made);
  delete made[name];
  setGrouping({ groups, made });
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
  const made = Object.assign({}, p.made);
  if (name in made) { made[next] = made[name]; delete made[name]; }
  setGrouping({ groups, made });
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
      aria-label="hue ${h}" data-tip="hue ${h}"></button>`).join("") + `</div>`;
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
//
// A PINNED CARD IS NOT A GROUP. It was, and a column then held a "pinned 17" block inside "done 111". A pin is the star on the
// card and a sort key: `pinnedFirst` puts pinned cards first in the column, and the sort is stable, so a pinned card is also
// first inside whatever project group it falls in.
function pinnedFirst(cards) {
  const pins = cards.filter(t => t.pinned).sort(byRank);
  return pins.concat(cards.filter(t => !t.pinned));
}

function cardsHTML(cards, g, keyPrefix) {
  if (!cards.length) return '<div class="empty">empty</div>';

  const rest = cards;
  const head = "";

  if (!g) return head + rest.map(t => cardHTML(t)).join("");

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
  if (byName.size < 2 && byName.has("")) return head + rest.map(t => cardHTML(t)).join("");

  return head + [...byName.keys()].sort(g.cmp).map(name => {
    const mine = byName.get(name);
    // Cards in none of the operator's groups are the board's own heap, so they
    // keep the sort. Every named group is theirs and keeps its rank.
    if (g.handOrdered && name !== UNTAGGED) mine.sort(byRank);
    if (!name) return mine.map(t => cardHTML(t)).join("");
    const key = keyPrefix + ":" + name;
    // FOLDED BY LANE AND NAME. It used to be by name alone, board wide, so shutting `web` in one column shut it
    // in every column, and the operator could not read one lane while folding another. A card that changes
    // status moves to another lane and is drawn there under that lane's own fold, which is the price of folds
    // being per lane. See `laneFoldKey`.
    const fold = laneFoldKey(keyPrefix, name);
    // UNTAGGED IN `custom` MODE STARTS SHUT, the way the offline group does.
    // The point of a curated view is to see the buckets you built, not a heap
    // of every card that lands in none of them. The fold list records what
    // was CHANGED from the default, so an entry there re-opens it and the
    // shape reads the same as every other fold.
    //
    // AN EMPTY GROUP STARTS SHUT TOO, by the same mechanism: nothing to look at until it gains a card, and an
    // explicit open is the entry that records it.
    const shutByDefault = mine.length === 0
      || (name === UNTAGGED && groupingPrefs(g.view).mode === "custom");
    const shut = (isFolded(fold) === shutByDefault) ? " open" : "";
    // Work you started and left. Drawn back rather than hidden: the point of
    // the bucket is that it is findable, and the point of greying it is that
    // it does not compete with what you are doing now.
    const cold = name === "dormant" ? " dormant" : "";
    // Keyed by the group's own name, so reordering the groups moves them
    // rather than rewriting each one with the next one's contents.
    return `<details class="cardgroup project${cold}"${shut}
      style="--ghue:${groupHue(name)}" data-morph-key="${esc(key)}"
      data-fold="${esc(fold)}"${shutByDefault ? ` data-fold-shut="1"` : ""}>
      <summary oncontextmenu="groupMenu(event, '${esc(name).replace(/'/g, "&#39;")}')">
        <span class="gname" data-tip="${esc(name)} &mdash; right click to recolor">${esc(name)}</span>
        <span class="gn">${mine.length}</span>
      </summary>
      ${mine.length ? mine.map(t => cardHTML(t)).join("") : emptyGroupHint(name)}
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

// What an empty group says: the tag it is made from, which is its name. An empty group also starts SHUT (see
// the callers), so this is read once somebody opens it.
function emptyGroupHint(tag) {
  return `<div class="empty groupempty">Nothing has the '${esc(tag)}' tag</div>`;
}

// The fold key of one group in one lane. `keyPrefix` is the column id, or the status inside a column that holds
// several, so two lanes never share a key. The name goes after a `|`, which `migrateLaneFolds` also uses to tell
// these from the board-wide keys older versions wrote ("proj:<name>").
function laneFoldKey(keyPrefix, name) { return "proj:" + keyPrefix + "|" + name; }
function pinFoldKey(keyPrefix) { return "pin:" + keyPrefix; }

// Every group the board has drawn, shut or open, as the elements carrying a fold key.
function boardGroupEls() {
  return [...document.querySelectorAll("#board details[data-fold]")];
}

// COLLAPSE ALL / EXPAND ALL for the board tab. Writes the fold list in one go and repaints once, rather than
// setting `open` on each element and leaving a `toggle` event per group to do the same thing one repaint at a
// time. The list records what differs from how a group comes, so for the groups that come shut the entry means
// open, exactly as the toggle listener reads it.
function foldAllGroups(shut) {
  const list = foldedColumns();
  for (const el of boardGroupEls()) {
    const key = el.dataset.fold;
    const inverted = key.startsWith("offline:") || el.dataset.foldShut === "1";
    const want = inverted ? !shut : shut;
    const i = list.indexOf(key);
    if (want && i < 0) list.push(key);
    if (!want && i >= 0) list.splice(i, 1);
  }
  localStorage.setItem("atrium.folded", JSON.stringify(list));
  repaintLists();
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
  //
  // `untagged` in custom mode comes shut too, and the render says so with
  // `data-fold-shut` because the key alone cannot. Read off the key, opening it
  // wrote nothing, the next repaint shut it, shutting wrote the entry that the
  // repaint draws as open, and the two went round with a refresh on every turn:
  // a group expanding and collapsing on its own, in every window on the board.
  const inverted = key.startsWith("offline:") || el.dataset.foldShut === "1";
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
  repaintLists();
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
  // The slim phone header shows the count beside the status dot (css/phone.css).
  const cn = document.getElementById("conn-n");
  if (cn) { cn.hidden = n === 0; cn.textContent = String(n); }
  if (!n) return;
  el.textContent = n === 1 ? "1 working" : n + " working";
  el.dataset.n = String(n);
  el.dataset.tip = n === 1
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
  const tasks = await boardCards(signal);
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
  // A resting minion is left out of every list drawn below and every count taken from them (`cardHidden`). The working
  // indicator, `lastTasks` and the new card note still see it: hiding is a view filter, never a loss.
  const shown = shownCards(everything);
  const live = shown.filter(t => !t.offline);
  lastTasks = everything;
  if (typeof pullsTasksMoved === "function") pullsTasksMoved();
  // THE DRAWER'S FILTERS: the search text, and the subagents that roll up into their parent's card (`N subagents`).
  // The rollup is worked out over every live card before the search, so a parent's badge does not move with what was
  // typed. The columns are filtered below, where they are drawn.
  const q = fdSearchText("board");
  const roll = subagentRollup(live, boardSubsOn());
  boardSubBadge = roll.badge;
  const keepCard = t => !roll.rolled.has(t.id) && (fdAttached(t) || cardMatchesText(t, q));
  const all = live.filter(keepCard);
  const offline = shown.filter(t => t.offline && cardMatchesText(t, q));
  // A column holding the attached card is drawn whatever its chip says (`fdAttached`).
  const showCols = boardShow();
  const heldCol = BOARD_COLUMNS.find(c => live.some(t => fdAttached(t) && boardColumnOf(t) === c.id));
  const drawCols = new Set(showCols);
  if (heldCol) drawCols.add(heldCol.id);
  paintBoardCounts(live.filter(t => !roll.rolled.has(t.id) && cardMatchesText(t, q)), roll.rollable);
  paintBoardCount(shown.filter(t => !roll.rolled.has(t.id)));
  fdStatus("board", {
    summary: boardSummary(showCols),
    match: all.length === live.length - roll.rolled.size && offline.length === shown.filter(t => t.offline).length
      ? "" : `${all.length} of ${live.length - roll.rolled.size}`
  });
  if (typeof ulOnCards === "function") ulOnCards();
  newCardNote(everything);
  paintWorking(everything.filter(t => !t.offline));
  const g = grouper("board");

  const html = BOARD_COLUMNS.filter(col => {
    // A column whose chip is off is not drawn, and the others take its width.
    if (!drawCols.has(col.id)) return false;
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
    return all.some(t => boardColumnOf(t) === col.id) ||
      offline.some(t => boardColumnOf(t) === col.id);
  }).map(col => {
    const inCol = columnOrder(all.filter(t => boardColumnOf(t) === col.id));
    const mine = col.id === "needs-you" ? needsYouOrder(inCol) : pinnedFirst(inCol);
    // DONE IS THE RECORD, NOT WORK: only the recent few are drawn, the rest are one click away in the stack.
    const drawn = col.id === "done" ? doneCap(mine) : mine;
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
    const shown = mine.length;
    // Approving everything means nothing can arrive here, so an empty
    // permissions column is not "nothing is blocked right now", it is a column
    // that cannot fill. It still shows if something IS in it: a paused card
    // or a never rule still blocks, and one of those sitting there while the
    // header says everything is approved is the single most confusing thing
    // the board could hide.
    // Approving everything means nothing can arrive here, so the column is not
    // reporting "nothing is blocked", it is a column that cannot fill. Gone
    // entirely rather than narrowed: a strip saying `empty` under a header
    // saying APPROVING EVERYTHING is a place for your eye to go for no reason.
    //
    // Only while it is empty. A paused card or a never rule still blocks, and
    // one of those sitting here while the header says everything is approved is
    // the single most confusing thing the board could hide, so `mine.length`
    // is part of the test and not an afterthought.
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
    const lost = offline.filter(t => boardColumnOf(t) === col.id);
    const body = (bare && !lost.length ? `<div class="empty">empty</div>` : cardsHTML(drawn, g, col.id) + doneMoreHTML(mine, drawn)) +
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
    // Done is a fixed width, so only the columns that need you or are working share the surplus.
    const weight = col.id === "done" ? 0 : Math.min(4, Math.max(1, Math.sqrt(mine.length)));

    return `<section class="col${folded}${vacant}${col.id === "done" ? " fixed" : ""}" data-column="${col.id}"
      ${vacant || col.id === "done" ? "" : `style="flex-grow:${weight.toFixed(2)}"`}>
      <div class="col-head" onclick="toggleColumn('${col.id}')">
        <span><span class="fold">&#9662;</span>${esc(col.label)}</span>
        <span class="help" tabindex="0" onclick="event.stopPropagation()"
          data-tip="${esc(col.why)}">?</span>
        ${col.id === "done" && mine.some(t => PRUNABLE.includes(t.status))
          ? `<span class="chip sweep" data-tip="delete every finished and dead card. paused and parked cards stay"
               onclick="event.stopPropagation();pruneFinished()">clear</span>` : ""}
        <span class="n">${mine.length}</span>
      </div>
      <div class="cards">${body}</div>
    </section>`;
  }).join("");
  // Nothing to re-wire. Every handler a card has is an attribute in its own
  // markup, so it comes back with the card. The listeners this used to attach
  // on every redraw were the drag ones, and there are none.
  setHTML(document.getElementById("board"), html);
  // A link still being opened is drawn over what was just written (js/link-opening.js).
  if (typeof linkOpeningPaint === "function") linkOpeningPaint();

  // Reported after the render, so a broken function shows an error and a board
  // drawn with the defaults rather than a blank page.
  if (groupingFault) {
    toast("grouping code", groupingFault);
    groupingFault = "";
  }
}

// ── the drawer on the board ─────────────────────────────────────────────────
//
// SHOW is one chip per column, all on by default. Turning one off removes the column and the rest take its width.
// Turning off the last one brings every column back, so it is never an empty board with no explanation. Stored as the
// ids that are on, and cleared when that is all of them, so a stored value always means it differs from the default.
const BOARD_SHOW_KEY = "atrium.board.show";
const BOARD_SUBS_KEY = "atrium.board.subagents";
let boardSubBadge = new Map();

function boardShow() {
  try {
    const v = JSON.parse(localStorage.getItem(BOARD_SHOW_KEY) || "null");
    if (Array.isArray(v)) {
      const on = new Set(v.filter(id => BOARD_COLUMNS.some(c => c.id === id)));
      if (on.size) return on;
    }
  } catch (e) {}
  return new Set(BOARD_COLUMNS.map(c => c.id));
}
function toggleBoardShow(id) {
  const on = boardShow();
  if (on.has(id)) on.delete(id); else on.add(id);
  try {
    if (!on.size || on.size === BOARD_COLUMNS.length) localStorage.removeItem(BOARD_SHOW_KEY);
    else localStorage.setItem(BOARD_SHOW_KEY, JSON.stringify([...on]));
  } catch (e) {}
  paintBoardShow();
  repaintLists();
}
function boardSubsOn() {
  try { return localStorage.getItem(BOARD_SUBS_KEY) === "on"; } catch (e) { return false; }
}
function toggleBoardSubs() {
  try {
    if (boardSubsOn()) localStorage.removeItem(BOARD_SUBS_KEY); else localStorage.setItem(BOARD_SUBS_KEY, "on");
  } catch (e) {}
  paintBoardShow();
  repaintLists();
}
prefLive([BOARD_SHOW_KEY, BOARD_SUBS_KEY], () => { paintBoardShow(); repaintLists(); });

function paintBoardShow() {
  const el = document.getElementById("board-seg");
  if (!el) return;
  const on = boardShow();
  setHTML(el, BOARD_COLUMNS.map(c => fdChip(c.label, on.has(c.id), `toggleBoardShow('${esc(c.id)}')`,
    c.label + ": click to show or hide this column", c.id)).join("") +
    fdChip("subagents", boardSubsOn(), "toggleBoardSubs()",
      "a subagent is not a card of its own: its parent's card says how many it has. one that needs you still shows", "subagents"));
  boardPillCounts();
}
// The chips' counts, written onto the pills by `paintBoardCounts` as each render learns them and kept here so a repaint
// of the pills does not blank them until the next poll.
let boardCounts = {};
function paintBoardCounts(cards, rollable) {
  boardCounts = { subagents: rollable };
  BOARD_COLUMNS.forEach(c => { boardCounts[c.id] = cards.filter(t => boardColumnOf(t) === c.id).length; });
  boardPillCounts();
}
// THE COUNT LINE above the board: `6 running: 3 need you, 3 working` and `111 done` at the right. Both come from `boardColumnOf`, so
// they cannot disagree with the columns. "Running" is a runner being alive, which is what the two live columns hold.
function paintBoardCount(cards) {
  const el = document.getElementById("board-count");
  if (!el) return;
  const n = id => cards.filter(t => boardColumnOf(t) === id).length;
  const need = n("needs-you"), work = n("working");
  setHTML(el, `<span class="running">${need + work} running: ${need} need you, ${work} working</span>` +
    `<span class="done">${n("done")} done</span>`);
}

// Who comes first in needs you: a permission (that agent is frozen), then an open question, then a turn that ended. Longest
// wait first inside each. Not the board's sort: this column is ordered by what is owed.
function needsYouOrder(cards) {
  const rank = t => t.status === "needs-permission" ? 0 : cardHasOpenQuestion(t) ? 1 : 2;
  const now = Date.now();
  return cards.slice().sort((a, b) => rank(a) - rank(b) || cardWaitSeconds(b, now) - cardWaitSeconds(a, now) || cardTieBreak(a, b));
}

// DONE DRAWS THE RECENT FEW: the 6 most recent, or every one finished in the last 24 hours, at most 12. Pinned cards are
// first in the list, so they come first under the same cap.
const DONE_MIN = 6, DONE_MAX = 12;
function doneCap(cards) {
  const day = cards.filter(t => cardIdleAge(t) < 86400).length;
  return cards.slice(0, Math.min(DONE_MAX, Math.max(DONE_MIN, day)));
}
function doneMoreHTML(all, drawn) {
  const more = all.length - drawn.length;
  if (more <= 0) return "";
  const pins = all.filter(t => t.pinned).length;
  return `<div class="more" onclick="openDoneInStack()" data-tip="open the stack with only finished cards shown">${more} more finished${
    pins ? `, ${pins} of them pinned` : ""} \u00b7 open in the stack</div>`;
}
function openDoneInStack() {
  setStackShow(new Set(["finished", "paused"]));
  switchView("stack");
}
async function pruneFinished() {
  const tasks = await boardCards();
  const n = (tasks || []).filter(t => PRUNABLE.includes(t.status)).length;
  if (!n) return;
  const ok = await confirmUser(`clear ${n} finished card${n === 1 ? "" : "s"}?`,
    "Their history goes with them. Anything still running, parked or paused is untouched.", "clear");
  if (!ok) return;
  const res = await api("/v1/tasks/prune", { method: "POST", body: JSON.stringify({ statuses: PRUNABLE }) });
  toast("cleared", `${res.removed} card${res.removed === 1 ? "" : "s"} gone`);
  refresh();
}

function boardPillCounts() { pillCounts("board-seg", boardCounts); }

function boardSummary(show) {
  const g = groupingPrefs("board");
  const parts = [groupWords(g), BOARD_SORTS[boardSortMode()].label];
  if (show.size < BOARD_COLUMNS.length) parts.unshift(show.size + " of " + BOARD_COLUMNS.length + " columns");
  if (boardSubsOn()) parts.push("subagents");
  return parts.join(" \u00b7 ");
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

prefLive(BOARD_SORT_KEY, () => { paintBoardSort(); repaintLists(); });

function setBoardSort(v) {
  if (!BOARD_SORTS[v]) return;
  localStorage.setItem(BOARD_SORT_KEY, v);
  paintBoardSort();
  repaintLists();
}

function paintBoardSort() {
  const el = document.getElementById("board-sort");
  if (!el) return;
  const mode = boardSortMode();
  setHTML(el, Object.entries(BOARD_SORTS).map(([v, s]) =>
    `<button class="${v === mode ? "on" : ""}" data-sort="${v}" onclick="setBoardSort('${v}')"
       data-tip="${esc(s.title)}">${esc(s.label)}</button>`).join(""));
}

function columnOrder(cards) {
  const mode = boardSortMode();
  // Rank is what the API already ordered by, so manual is the list untouched.
  if (mode === "manual") return cards;
  const by = mode === "name"
    ? (a, b) => (a.display_title || "").localeCompare(b.display_title || "")
    : cardActivityCmp;
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
    ${cards.map(t => cardHTML(t)).join("")}
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
        ? `<span class="chip sweep" data-tip="delete every ${esc(status)} card"
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
// `paused` is left out although it accepts a card, because pausing costs
// something: it stops the runner, and the `pause` entry is where that is
// said. Two ways to pause is how two descriptions of it drift apart.
//
// Labelled by STATUS rather than by column, because the status is what the
// card becomes and `finished` is a column holding two of them. Filing into it
// means `done`, and an entry reading `finished` would not say which.
function moveItem(id, t) {
  const to = COLUMNS
    .filter(c => c.accepts !== false && c.id !== "paused")
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
    // Pausing stops a runner and resuming starts it again, neither of which
    // is visible from a card moving between columns.
    if (body.status === "paused") toast("paused", "its runner was stopped");
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
        toast("back, but not running", res.resume_note);
      } else {
        toast("not resumed", res.resume_note);
      }
    }
    return res;
  } catch (e) { toast("that did not stick", e.message); }
}

