// ── the card map ────────────────────────────────────────
//
// ONE COPY OF THE CARDS, HELD HERE, AND EVERY VIEW PAINTS FROM IT.
//
// Each view used to fetch `/v1/tasks` for itself, on every refresh pass, and a
// refresh pass ran on every clump of task events. On a board of three hundred
// cards that list is over half a megabyte and more than a second on a room, so
// a busy board pulled it every two or three seconds, per tab, and a click that
// only changed how the terminal strip was sorted waited a second on the network
// before it painted.
//
// Now the list is fetched at load, on a resync, and (until every `task` event
// carries a whole row) on a task event at most once every few seconds. What the
// views draw comes from this map, so a click that only changes the drawing
// paints in the frame it was made in.
//
// The rows are kept as the daemon sent them and handed out as shallow copies.
// A view decorates what it is given (`disambiguateTitles` appends to a title
// in place), and a decoration written into the store would be applied again on
// the next paint.
let cardRows = new Map();
let cardsLoaded = false;
// When the list was last read whole, so a task event can tell how long ago
// the last pull was. See `tasksSoon`.
let cardsReadAt = 0;

// One read at a time. A view drawn at boot and the first pass both want the
// list before either has it, and two copies of half a megabyte arriving a
// moment apart is the cost this file exists to remove.
let cardsLoading = null;
// A pin the board has drawn and the room has not confirmed yet: id -> wanted value. A list read that began before the
// PATCH landed carries the old value and would flip the star back off.
const pinHeld = new Map();
function loadCards(signal) {
  if (cardsLoading) return cardsLoading;
  cardsLoading = (async () => {
    const { tasks } = await api("/v1/tasks", { signal });
    const next = new Map();
    (tasks || []).forEach(t => { if (t && t.id) next.set(t.id, t); });
    pinHeld.forEach((on, id) => { if (next.has(id)) next.set(id, Object.assign({}, next.get(id), { pinned: on })); });
    if (cardsLoaded) next.forEach((t, id) => cleanupWhenDone(cardRows.get(id), t));
    cardRows = next;
    cardsLoaded = true;
    cardsReadAt = Date.now();
  })().finally(() => { cardsLoading = null; });
  return cardsLoading;
}

function cardList() {
  return Array.from(cardRows.values(), t => Object.assign({}, t));
}
// What the views DRAW: every card but a hidden minion. `cardList` keeps all of them, for the waiting list and the alerts.
function shownCards(list) { return (list || cardList()).filter(t => !cardHidden(t)); }

// A `task` event that carries the WHOLE `/v1/tasks` row, which the daemon says
// by setting `row: 1` on it. Every decoration on a row is omitempty, so the
// absence of any one of them proves nothing, and a partial row upserted over a
// whole one would take the asks, the seen state and the live activity off a
// card until the next resync. Until the daemon sends the marker, an event is a
// reason to re-read and never a row.
function cardRowComplete(d) {
  return !!(d && d.row === 1 && d.id);
}

function upsertCard(row) {
  cleanupWhenDone(cardRows.get(row.id), row);
  cardRows.set(row.id, row);
}

function dropCard(id) {
  return cardRows.delete(id);
}

// A LAUNCH ANSWER IS THE DAEMON'S WORD ON THE CARD, recorded the moment it lands.
//
// `/v1/launch` answers with the card's row, `supervised` and all, and the board opens the pane on it. The daemon's
// own `task` event for the same card is coalesced (`taskCoalesce` in internal/api/api.go) and arrives up to a fifth
// of a second LATER, and a room older than that only says to read the list again. Until one of those lands, the
// cached copy of the card still says what it said before the click: not supervised. The in-flight mark in
// js/terminal-list.js protects the pane until its socket opens, which on loopback is a few milliseconds, and after
// that a repaint from the cache (`reconcileAttached`) found no supervised row for the attached card and tore the
// pane down: the socket the board had just opened was closed, the terminal went, and the resume spinner had nothing
// left to end on. The boot-time restore, which repaints the remembered view half a second after load, is one such
// repaint. The headless `resumeSpinner` unit caught it there.
//
// So the answer is merged over the cached row, under the id the cache already holds: the merged view keys a card
// as `room~id` and a launch answer comes back bare, so the cached key stays and only the fields move. A card the
// cache has never seen is left for the list to bring, because inserting it under a bare id would draw it twice once
// the tagged copy arrived. Merged rather than replaced because a decoration the answer omits (asks, seen) is
// absent, not cleared, and the next read replaces the whole row anyway.
function cardLaunched(row) {
  if (!row || !row.id) return;
  const key = Array.from(cardRows.keys()).find(k => bareId(k) === bareId(row.id));
  if (!key) return;
  cardRows.set(key, Object.assign({}, cardRows.get(key), row, { id: key }));
}

// WHAT IS WAITING ON YOU, worked out from the cards rather than asked for.
//
// The same filter as `st.Waiting()` in internal/store/tasks.go, which is what
// `/v1/waiting` answered from: a waiting column, a wire name to send to (an
// adopted session with none can be watched and not answered), not archived,
// oldest wait first. A card from a room that is not answering is left out, as
// the hub's fan-out of `/v1/waiting` left it out.
function waitingRow(t) {
  return cardWaiting(t) &&
    !!t.wire_name && !t.archived_at && !t.offline;
}
// WHAT THE READY ALERT WATCHES: every card at rest with a runner there, an agent-launched one that ended its turn included
// (a worker that finished, issue-20674@claude-sg4, 2026-10-13, is news to the board). Whether a card with a running child is
// done is the caller's (`hasRunningChild`).
function cardsReady(list) {
  return (list || cardList()).filter(t => waitingRow(t) && (!isMinion(t) || cardHasOpenQuestion(t)))
    .sort((a, b) => String(a.waiting_since || "").localeCompare(String(b.waiting_since || "")));
}
// A card that launched another still running is not waiting on the human, whatever its own status says. The ready
// alert reads this to hold back until the last child ends. Matched on the bare id, as the board does everywhere.
function hasRunningChild(t, all) {
  const me = bareId(t.task_id || t.id);
  return (all || []).some(c => (c.spawned_by_id || c.spawned_by) && c.status === "running" && !c.archived_at &&
    (c.spawned_by_id && bareId(c.spawned_by_id) === me ||
      (l => !!l && bareId(l.id) === me)(launcherOf(c, all))));
}
function cardsWaiting() {
  return cardList().filter(waitingRow)
    .sort((a, b) => String(a.waiting_since || "").localeCompare(String(b.waiting_since || "")));
}

// ── the permission queue, held the same way ─────────────
//
// Fetched at load, on a `permission` event and on a resync, and read from here
// by everything that draws it: the badge, the perms tab and the banner over an
// attached terminal. The banner used to fetch its own copy a moment after the
// pass fetched the same list.
let permsLocal = [];
let permsLoaded = false;
async function loadPerms(signal) {
  permsLocal = (await api("/v1/permissions", { signal })).permissions || [];
  permsLoaded = true;
  return permsLocal;
}
// Every room's waiting requests, read with the local queue. See `remoteRequests`.
let remoteLocal = [];

// What a view paints from. The map, and on the one occasion it has never been
// read (a view drawn before the first pass lands) the list is read first. A
// read that fails throws, the way the view's own fetch used to.
async function boardCards(signal) {
  if (!cardsLoaded) await loadCards(signal);
  return cardList();
}
