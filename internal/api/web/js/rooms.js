// Which room you are looking at.
//
// ── what this file is for ────────────────────────────────────────────────────
//
// A hub serves this board and holds nothing. The agents, the terminals and the
// database live in ROOMS, which are atriums on other machines dialling in. One
// room or twenty, the board is the same board.
//
// So the header grows one control: a counter that is also a selector. Leave it
// alone and you are looking at every room at once, which is the hub's whole
// point. Pick one and the entire board becomes that room's atrium, with no room
// column, no room field and nothing asking again.
//
// ── how the scoping actually happens ─────────────────────────────────────────
//
// ONE HEADER, ADDED IN ONE PLACE. `X-Atrium-Room` on every request this page
// makes. The hub reads it and stops merging: it becomes a byte pipe to that one
// room and the answers arrive exactly as that room wrote them.
//
// Added by wrapping `fetch` rather than by editing every call site, because
// there are over a hundred of those and the board was written before rooms
// existed. A wrapper is also the only way to catch the ones that do not go
// through `api()`: the uploads, the downloads and the icon posts.
//
// A websocket cannot carry a header, so the terminal attach gets the same
// answer as a query parameter, added by wrapping `WebSocket` for the same
// reason.
//
// ── and when there is no hub ─────────────────────────────────────────────────
//
// This file does nothing at all. `/_hub/health` is the only endpoint a hub has
// and a plain daemon answers it with a 404, so the probe below is the whole of
// the feature detection: no chip, no header, no wrapper doing anything. The
// same board serves both and cannot be built twice.

// ROOM_KEY is where the choice lives. Per browser rather than per machine: two
// people looking at one hub are usually looking at different rooms, and a
// choice stored on the hub would have them fighting over it.
const ROOM_KEY = "atrium.room";

// hubRooms is what is attached right now, and hubIsHub says whether to believe
// it. Both are read by the header chip and by the launch dialog.
let hubRooms = [];
let hubIsHub = false;

// roomNow is the chosen room, or empty for all of them.
function roomNow() {
  try { return localStorage.getItem(ROOM_KEY) || ""; } catch (e) { return ""; }
}

// pickRoom changes what the whole board is looking at.
//
// A RELOAD, AND THAT IS THE DESIGN RATHER THAN A SHORTCUT. Scoping changes the
// answer to every request this page has already made: the cards, the queue, the
// settings, the terminals, the rules. Repainting all of it in place would mean
// a re-fetch and a redraw of every pane, including the ones that are not open,
// and any pane that missed the memo would be showing another room's data next
// to this one's. A reload is one line and cannot be half done.
function pickRoom(name) {
  try {
    if (name) localStorage.setItem(ROOM_KEY, name);
    else localStorage.removeItem(ROOM_KEY);
  } catch (e) {}
  location.reload();
}

// ── the header chip ─────────────────────────────────────────────────────────

// paintRooms draws the counter, which is the one thing on this board that says
// what you are looking at.
//
// `2/2 rooms` when looking at everything, the room's own name when scoped. The
// operator asked for this because the only sign a room was attached used to be
// the absence of the reconnecting dialog, which reads as broken rather than as
// working.
function paintRooms() {
  const el = document.getElementById("rooms");
  if (!el) return;
  if (!hubIsHub) { el.hidden = true; return; }
  el.hidden = false;

  const room = roomNow();
  const live = hubRooms.length;
  const known = Math.max(live, room ? 1 : 0);
  const here = room ? (hubRooms.some(r => r.name === room) ? 1 : 0) : live;

  const label = document.getElementById("rooms-t");
  if (room) {
    label.textContent = room;
    el.title = here
      ? `scoped to ${room}. this is that machine's atrium.`
      : `scoped to ${room}, which is not attached right now.`;
  } else {
    label.textContent = `${here}/${known} room${known === 1 ? "" : "s"}`;
    el.title = "you are on an atrium hub, looking at every room at once. " +
      "click to focus on one.";
  }
  el.classList.toggle("cold", here === 0);
  el.classList.toggle("scoped", !!room);
}

// openRooms is the menu the chip opens.
function openRooms() {
  const menu = document.getElementById("rooms-menu");
  if (!menu) return;
  if (!menu.hidden) { menu.hidden = true; return; }
  const room = roomNow();
  const rows = [
    `<button class="${room ? "" : "on"}" onclick="pickRoom('')">
       <strong>all rooms</strong>
       <span>every room at once, which is what a hub is for</span></button>`
  ];
  for (const r of hubRooms) {
    const name = esc(r.name);
    rows.push(`<button class="${room === r.name ? "on" : ""}"
      onclick="pickRoom('${name.replace(/'/g, "&#39;")}')">
        <strong>${name}</strong>
        <span>${esc(r.host || "")}</span></button>`);
  }
  // A room that was chosen and has since gone. Kept in the list so there is
  // something to click your way out of.
  if (room && !hubRooms.some(r => r.name === room)) {
    rows.push(`<button class="on cold"><strong>${esc(room)}</strong>
      <span>not attached right now</span></button>`);
  }
  if (!hubRooms.length) {
    rows.push(`<div class="none">no room is attached. the hub serves this board
      and holds nothing, so until a room connects there is nothing to show.</div>`);
  }
  menu.innerHTML = rows.join("");
  menu.hidden = false;
}

// roomChip is a card's room, in the aggregate view.
//
// Drawn as a tag because that is what the operator asked for: it sits with the
// tags and it can be filtered on later. Clicking it does the more useful thing
// than filtering, which is to focus the whole board on that room.
//
// Never drawn when scoped, because then every card is from the same room and a
// chip saying so on all of them is noise.
function roomChip(t) {
  if (!hubIsHub || roomNow() || !t || !t.room) return "";
  const name = esc(t.room);
  return `<span class="chip room" title="on ${name}. click to focus on it."
    onclick="event.stopPropagation();pickRoom('${name.replace(/'/g, "&#39;")}')"
    >${name}</span>`;
}

// ── scoping every request this page makes ───────────────────────────────────

const plainFetch = window.fetch.bind(window);
window.fetch = function (input, init) {
  const room = roomNow();
  if (!room) return plainFetch(input, init);
  // A Request object carries its own headers, so it is rebuilt rather than
  // having an init merged onto it, which fetch ignores for most fields.
  if (typeof Request !== "undefined" && input instanceof Request) {
    // AN EXPLICIT ROOM WINS. The launch dialog names the room a card is to
    // start in, which may not be the one being looked at.
    if (input.headers.get("X-Atrium-Room")) return plainFetch(input, init);
    const next = new Request(input, init);
    next.headers.set("X-Atrium-Room", room);
    return plainFetch(next);
  }
  const opts = Object.assign({}, init);
  const headers = new Headers(opts.headers || {});
  if (!headers.get("X-Atrium-Room")) headers.set("X-Atrium-Room", room);
  opts.headers = headers;
  return plainFetch(input, opts);
};

const PlainSocket = window.WebSocket;
window.WebSocket = function (url, protocols) {
  const room = roomNow();
  let u = String(url);
  if (room && u.indexOf("atrium_room=") < 0) {
    u += (u.indexOf("?") < 0 ? "?" : "&") + "atrium_room=" + encodeURIComponent(room);
  }
  return protocols === undefined ? new PlainSocket(u) : new PlainSocket(u, protocols);
};
window.WebSocket.prototype = PlainSocket.prototype;
["CONNECTING", "OPEN", "CLOSING", "CLOSED"].forEach((k, i) => {
  window.WebSocket[k] = i;
});

// eventsURL is which stream this board wants.
//
// A PATH RATHER THAN THE HEADER ABOVE, because `EventSource` sets no headers.
// That is the whole reason the hub has three spellings of this endpoint.
function eventsURL() {
  if (!hubIsHub) return "/v1/events";
  const room = roomNow();
  return room ? "/v1/events/room/" + encodeURIComponent(room) : "/v1/events/hub";
}

// ── finding out whether this is a hub at all ────────────────────────────────

// loadRooms asks what is attached. Answers false when this is a plain daemon,
// which is how everything above turns itself off.
async function loadRooms() {
  let got;
  try {
    got = await plainFetch("/_hub/rooms");
    if (!got.ok) throw new Error("not a hub");
    got = await got.json();
  } catch (e) {
    hubIsHub = false;
    paintRooms();
    return false;
  }
  hubIsHub = true;
  hubRooms = (got.rooms || []).slice().sort((a, b) =>
    String(a.name).localeCompare(String(b.name)));
  paintRooms();
  return true;
}

// startRooms wires the chip up. Called once, before the event stream opens,
// because `eventsURL` cannot answer until the probe has.
async function startRooms() {
  if (!await loadRooms()) return;
  // A BACKSTOP POLL, and it is not the primary signal. The merged stream says
  // `rooms` the moment membership changes, but a board scoped to one room is
  // not on that stream, so this is what keeps its counter honest.
  setInterval(loadRooms, 10000);
  document.addEventListener("click", e => {
    const menu = document.getElementById("rooms-menu");
    if (!menu || menu.hidden) return;
    if (e.target.closest("#rooms") || e.target.closest("#rooms-menu")) return;
    menu.hidden = true;
  });
}
