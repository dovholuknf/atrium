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
    // WHAT THE NEXT PAGE PUTS OVER ITSELF WHILE IT COMES UP. Read by an inline
    // script in the head, before the first paint, because the reload shows the
    // stack and then jumps to the view you were on and the frame in between is
    // somebody else's screen. See the head of index.html.
    sessionStorage.setItem("atrium.switching", name || "all rooms");
  } catch (e) {}
  try {
    if (name) localStorage.setItem(ROOM_KEY, name);
    else localStorage.removeItem(ROOM_KEY);
    // AND THE TERMINAL YOU WERE READING IS FORGOTTEN, because it was on the
    // machine you just stopped looking at.
    //
    // The board remembers one card so a restart puts you back on it. That slot
    // has no idea which room it belongs to, so switching rooms left it holding
    // another machine's card: the terminals list came up empty, correctly,
    // while the board announced it was waiting for a session to come back that
    // is not on this machine and was never coming.
    localStorage.removeItem("atrium.term");
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
  const conn = document.getElementById("conn");
  if (!el) return;
  if (!hubIsHub) {
    el.hidden = true;
    if (conn) conn.hidden = false;
    return;
  }
  el.hidden = false;

  // ONE INDICATOR, NOT TWO. A red `reconnecting` beside a green `2/2 rooms` is
  // a board contradicting itself: the rooms are attached to the hub, the
  // stream to the browser is not, and nobody reading a header is holding those
  // two facts apart. So the chip carries both and the other one goes away.
  const streamUp = !conn || conn.classList.contains("live");
  if (conn) conn.hidden = true;

  const room = roomNow();
  const live = hubRooms.length;
  const known = Math.max(live, room ? 1 : 0);
  const here = room ? (hubRooms.some(r => r.name === room) ? 1 : 0) : live;

  const label = document.getElementById("rooms-t");
  if (!streamUp) {
    label.textContent = "reconnecting";
    el.title = "this board lost its connection to the hub. the rooms are unaffected: " +
      "their agents keep running and the board catches up when it reconnects.";
  } else if (room) {
    label.textContent = room;
    el.title = here
      ? `scoped to ${room}. this is that machine's atrium.`
      : `scoped to ${room}, which is not attached right now.`;
  } else {
    label.textContent = `${here}/${known} room${known === 1 ? "" : "s"}`;
    el.title = "you are on an atrium hub, looking at every room at once. " +
      "click to focus on one.";
  }
  el.classList.toggle("down", !streamUp);
  el.classList.toggle("cold", streamUp && here === 0);
  el.classList.toggle("scoped", streamUp && !!room);
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
  // Placed from the chip rather than anchored to it, because the menu lives at
  // the end of the body and not inside the header. Clamped to the left so a
  // long room name cannot push it off the edge of a narrow window.
  const at = document.getElementById("rooms").getBoundingClientRect();
  menu.style.top = (at.bottom + 8) + "px";
  menu.style.left = Math.max(8, Math.min(at.right - menu.offsetWidth,
    window.innerWidth - menu.offsetWidth - 8)) + "px";
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

// ── the rooms pane ──────────────────────────────────────────────────────────

// ownRoomPanel is the switch for running agents on the hub's own machine.
//
// OFF BY DEFAULT. A hub holding a database is a hub whose restart is no longer
// free, and that freedom is the reason the two halves are separate at all.
async function ownRoomPanel() {
  let own;
  try { own = await plainFetch("/_hub/room").then(r => r.json()); } catch (e) { return ""; }
  if (!own || !own.available) return "";
  return `<div class="panel roomrow">
    <div class="col-head" style="margin:0 0 8px">
      <span>this machine</span>
      ${own.on ? `<span class="chip live idle">running as ${esc(own.name)}</span>`
        : `<span class="chip">not running agents</span>`}
      <span class="grow"></span>
      <button class="${own.on ? "no" : "go"}" onclick="setOwnRoom(${own.on ? "false" : "true"})"
        >${own.on ? "stop running agents here" : "run agents here too"}</button>
    </div>
    <div class="hintline">Restarting the hub also stops any agents running on it.
      Agents on the other rooms keep going.</div>
  </div>`;
}

// setOwnRoom throws that switch and redraws.
//
// NO CONFIRMATION. It is one click to undo, the button says what it does, and
// that agents on a machine stop when that machine's atrium stops is not news to
// anybody. A dialog here would be ceremony around a toggle.
async function setOwnRoom(on) {
  try {
    await plainFetch("/_hub/room", {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ on: !!on })
    });
  } catch (e) { tellUser("atrium", "could not change that: " + e.message); }
  hubRead = 0;
  await loadHubRooms();
  renderRooms();
}

// hubAttachedRows is the attached rooms, as rows for the rooms pane.
//
// Drawn beside the machines that check in rather than in a pane of their own.
// They are two ways of being the same thing, and splitting them is what made a
// hub with two rooms show an empty room list. See `renderRooms` in runners.js.
async function hubAttachedRows() {
  await loadHubRooms();
  if (!hubIsHub) return "";
  const own = await ownRoomPanel();
  const room = roomNow();
  if (!hubRooms.length) {
    return own + `<div class="panel"><div class="empty">
      This atrium is a hub. It serves this board and holds nothing, so until a room
      attaches there is nothing to show and no agents to run.
      <a href="#" onclick="openRoomJoin();return false;">Add a room</a>.
    </div></div>`;
  }
  // NO BUTTON TO FOCUS A ROOM. The counter in the header is the selector, and
  // a second control doing the same thing in a different place is a second
  // thing to find, to keep in step and to be surprised by. This list says what
  // is there; the header says which one you are in.
  return own + hubRooms.map(r => {
    const here = room === r.name;
    return `<div class="panel roomrow">
      <div class="col-head" style="margin:0 0 8px">
        <span>${esc(r.name)}</span>
        <span class="chip live idle">attached ${esc(shortTime(r.since))}</span>
        ${here ? `<span class="chip">the board is scoped to this one</span>` : ""}
        <span class="grow"></span>
        <span class="by">${esc(r.host || "")}${r.version ? " &middot; " + esc(r.version) : ""}</span>
      </div>
      <div class="hintline">Its agents, its terminals and its database are on that machine and
        reachable through this board. Restarting this hub does not touch them.</div>
    </div>`;
  }).join("");
}

// ── which room a change lands in ────────────────────────────────────────────
//
// Reading is merged: the hub asks every room for its runners, its fixtures,
// its sources, its recognisers and its actions, and hands back one list with
// the room on each row. Writing cannot be. "Add this runner" across four
// machines is a question, not a guess, and the hub refuses it outright.
//
// So a change carries the room it is for. Editing an existing row uses that
// row's room, which is never ambiguous. Adding something new, with more than
// one room and none chosen, asks once.

// writeRoom is the room the open editor is for, empty for none.
let writeRoom = "";

// roomGroups draws a list of configuration rows, grouped by the machine they
// are on.
//
// A HEADING AND A PANEL PER ROOM, not a pill on every row. Twelve runners from
// three machines in one box is a list you have to read sideways to use: the
// thing that tells you which machine you are looking at is a small chip near
// the right edge, repeated twelve times, and the boundary between one machine
// and the next is not drawn at all. Grouped, the boundary IS the drawing, the
// name is said once, and the rows go back to being about the runner.
//
// One plain panel when there is nothing to group by: no hub, or a board scoped
// to one room, or rows that carry no room at all.
function roomGroups(rows, row) {
  rows = rows || [];
  const panel = list => `<div class="panel">` + list.map(row).join("") + `</div>`;
  if (!hubIsHub || roomNow() || !rows.some(r => r && r.room)) return panel(rows);

  // The header's order, so every pane on the page lists machines the same way
  // round. Anything from a room that has since detached goes last rather than
  // disappearing: it is still on that machine.
  const known = hubRooms.map(r => r.name);
  const extra = rows.map(r => (r && r.room) || "")
    .filter(n => n && known.indexOf(n) < 0);
  const names = known.concat([...new Set(extra)]);

  return names.map(name => {
    const mine = rows.filter(r => r && (r.room || "") === name);
    if (!mine.length) return "";
    return `<div class="col-head roomgroup"><span>${esc(name)}</span></div>` + panel(mine);
  }).join("");
}

// rowOf finds a configuration row by id AND room.
//
// THE ROOM IS PART OF THE KEY once lists are merged. Two machines can both
// have a runner called `claude`, an action called `review` or a fixture called
// `notes`, and looking one up by id alone returns whichever room answered
// first. Editing sparta's runner and saving it to athens is the kind of wrong
// nobody notices until the wrong machine starts behaving oddly.
function rowOf(list, id, room) {
  return (list || []).find(x => x.id === id && (!room || (x.room || "") === room));
}

// chooseWriteRoom settles which room an editor is about to change.
//
// Answers false when the question was asked and dismissed, which the caller
// treats as "do not open the dialog": an editor with no room to save into
// would fail on the save, after the typing.
async function chooseWriteRoom(row, what) {
  writeRoom = "";
  // Not a hub, or already scoped to one room: the request goes where every
  // other request on this page goes and nothing extra is needed.
  if (!hubIsHub || roomNow()) return true;
  if (row && row.room) { writeRoom = row.room; return true; }
  if (hubRooms.length === 1) { writeRoom = hubRooms[0].name; return true; }
  if (!hubRooms.length) {
    tellUser("no room", "No room is attached, so there is nowhere to put this. " +
      "A hub serves the board and holds nothing.");
    return false;
  }
  const pick = await askUser({
    title: "which machine?",
    body: `A ${esc(what)} belongs to one machine. You are looking at all of them, ` +
      `so say which one this is for.`,
    buttons: hubRooms.map(r => ({ label: r.name, value: r.name, style: "go" }))
      .concat([{ label: "cancel", value: null }])
  });
  if (!pick) return false;
  writeRoom = pick;
  return true;
}

// ── scoping every request this page makes ───────────────────────────────────

const plainFetch = window.fetch.bind(window);
window.fetch = function (input, init) {
  // The room being looked at, or, for a CHANGE made while looking at all of
  // them, the room the open editor is for. See `chooseWriteRoom`.
  //
  // Writes only, and that is what makes a stale `writeRoom` harmless: an
  // editor sets it and nothing clears it, so if it reached reads too, closing
  // a dialog would silently leave the whole board scoped to one machine.
  const how = String((init && init.method) || (input && input.method) || "GET").toUpperCase();
  const room = roomNow() || (how === "GET" || how === "HEAD" ? "" : writeRoom);
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

// ── the cover, while a room is being changed ────────────────────────────────

// nameTheSwitch fills in which room is being moved to.
//
// The cover is already up by the time this runs: the head script put it there
// before the first paint, and this only writes the name into it. Naming it
// from here rather than from the head keeps the part that has to happen before
// anything is drawn down to one line.
function nameTheSwitch() {
  let to = "";
  try { to = sessionStorage.getItem("atrium.switching") || ""; } catch (e) {}
  if (!to) return;
  const what = document.querySelector("#switching .s-what");
  if (what) what.innerHTML = `switching to <b>${esc(to)}</b>`;
  // A COVER THAT NEVER LIFTS IS WORSE THAN THE FLASH IT HID. Boot has paths
  // that never reach the line which takes it away: a guest page, a terminal
  // only window, anything that throws on the way. Eight seconds is far longer
  // than a board takes to come up and far shorter than somebody will sit
  // looking at a spinner before reaching for F5.
  setTimeout(doneSwitching, 8000);
}

// doneSwitching takes the cover away, once the board is on the right view.
//
// The flag is cleared FIRST. A reload during the switch would otherwise come
// up covered again, and a cover with nothing behind it to wait for is a board
// that looks hung.
function doneSwitching() {
  let was = "";
  try {
    was = sessionStorage.getItem("atrium.switching") || "";
    sessionStorage.removeItem("atrium.switching");
  } catch (e) {}
  document.documentElement.classList.remove("switching");
  if (was) landOnATerminal();
}

// landOnATerminal picks up this room's session, after a switch.
//
// THE REMEMBERED CARD WAS ON THE MACHINE YOU JUST LEFT, so `pickRoom` forgets
// it: waiting for a session that is not here was the bug before this. But
// forgetting it alone means arriving at the terminals view with a list of one
// and an empty pane beside it, which is the board making you click the only
// thing there.
//
// So the equivalent card on THIS machine is opened instead. Most recently
// active, because that is the one "where was I" means on a machine you have
// not been looking at.
//
// Only when there is nothing else going on: still on the terminals view, still
// nothing attached, and something to attach to. Any of those failing means
// somebody has already decided, and a board that overrides that is a board
// arguing with you.
async function landOnATerminal() {
  if (typeof isViewing === "function" && !isViewing("terms")) return;
  if (typeof termTask !== "undefined" && termTask) return;
  let tasks = [];
  try { tasks = (await api("/v1/tasks")).tasks || []; } catch (e) { return; }
  const live = tasks
    .filter(t => t.supervised && !t.archived_at)
    .sort((a, b) => String(b.last_activity_at || "").localeCompare(String(a.last_activity_at || "")));
  if (!live.length) return;
  if (typeof termTask !== "undefined" && termTask) return;
  attachTask(live[0].id);
}

// ── finding out whether this is a hub at all ────────────────────────────────

// loadHubRooms asks what is attached. Answers false when this is a plain daemon,
// which is how everything above turns itself off.
let hubRead = 0;
async function loadHubRooms() {
  // Throttled, because the rooms pane, the header chip and the poll all ask
  // within the same frame after a refresh and would otherwise put three
  // identical requests on the wire and disagree about the answer.
  if (Date.now() - hubRead < 2000) return hubIsHub;
  hubRead = Date.now();
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
  nameTheSwitch();
  if (!await loadHubRooms()) return;
  // A BACKSTOP POLL, and it is not the primary signal. The merged stream says
  // `rooms` the moment membership changes, but a board scoped to one room is
  // not on that stream, so this is what keeps its counter honest.
  setInterval(loadHubRooms, 10000);
  document.addEventListener("click", e => {
    const menu = document.getElementById("rooms-menu");
    if (!menu || menu.hidden) return;
    if (e.target.closest("#rooms") || e.target.closest("#rooms-menu")) return;
    menu.hidden = true;
  });
}
