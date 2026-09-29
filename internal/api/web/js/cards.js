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
function loadCards(signal) {
  if (cardsLoading) return cardsLoading;
  cardsLoading = (async () => {
    const { tasks } = await api("/v1/tasks", { signal });
    const next = new Map();
    (tasks || []).forEach(t => { if (t && t.id) next.set(t.id, t); });
    cardRows = next;
    cardsLoaded = true;
    cardsReadAt = Date.now();
  })().finally(() => { cardsLoading = null; });
  return cardsLoading;
}

function cardList() {
  return Array.from(cardRows.values(), t => Object.assign({}, t));
}

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
  cardRows.set(row.id, row);
}

function dropCard(id) {
  return cardRows.delete(id);
}

// WHAT IS WAITING ON YOU, worked out from the cards rather than asked for.
//
// The same filter as `st.Waiting()` in internal/store/tasks.go, which is what
// `/v1/waiting` answered from: a waiting column, a wire name to send to (an
// adopted session with none can be watched and not answered), not archived,
// oldest wait first. A card from a room that is not answering is left out, as
// the hub's fan-out of `/v1/waiting` left it out.
function waitingRow(t) {
  return (t.status === "needs-input" || t.status === "needs-permission") &&
    !!t.wire_name && !t.archived_at && !t.offline;
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
