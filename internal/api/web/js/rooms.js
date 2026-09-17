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
  // THE DENOMINATOR IS WHAT IS MISSING, and this is the one place whose job is
  // to say so. Every count on this board is live only, on purpose: a badge
  // saying three agents want permission on a laptop that is shut is a number
  // you cannot act on and will look at anyway. `1/2 rooms` is where the other
  // half is reported, and nowhere else.
  //
  // A room that has NEVER connected is not counted either. It cannot have
  // cards, so nothing is missing because of it: it is inventory, and the rooms
  // tab is where inventory lives.
  const everConnected = hubInventory.filter(r => r.attached || r.first_seen).length;
  const known = Math.max(live, everConnected, room ? 1 : 0);
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


// hubAttachedRows is the attached rooms, as rows for the rooms pane.
//
// Drawn beside the machines that check in rather than in a pane of their own.
// They are two ways of being the same thing, and splitting them is what made a
// hub with two rooms show an empty room list. See `renderRooms` in runners.js.
async function hubAttachedRows() {
  await loadHubRooms();
  // A PLAIN DAEMON IS A ROOM TOO, and drawing it as one is not decoration.
  //
  // A room's settings live behind the cog on its row. On a board with no hub
  // there are no rows, so without this the editor command, the picker roots,
  // the shell and the rest would have nowhere to be opened from: they would be
  // in the page and unreachable. The daemon IS the hub with its own room, which
  // is decision 3, and this is what that looks like on a machine running one
  // atrium by itself.
  if (!hubIsHub) return thisMachineRow();
  const rooms = await withOwnRoom(hubInventory);

  if (!rooms.length) {
    return `<div class="panel"><div class="empty">
      This atrium is a hub. It serves the board and holds no work, so until a room
      is added there is nothing to show and no agents to run.
      <a href="#" onclick="openRoomJoin();return false;">Add a room</a>.
    </div></div>`;
  }

  // LIVE, THEN OFFLINE, THEN NEVER CONNECTED, and the order is the design.
  // What is running is what the board is for and is never pushed down the page
  // by what is not. A room that has never connected cannot have cards, so this
  // tab is the only place it appears at all: it is inventory, not work.
  const live = rooms.filter(r => r.attached);
  const off = rooms.filter(r => !r.attached && r.first_seen);
  const never = rooms.filter(r => !r.attached && !r.first_seen);

  const group = (title, hint, list) => !list.length ? "" :
    `<div class="col-head roomgroup"><span>${esc(title)}</span>
       <span class="grow"></span><span class="by">${esc(hint)}</span></div>` +
    list.map(roomRow).join("");

  return group("here now", live.length === 1 ? "1 room" : live.length + " rooms", live) +
    group("not answering", "what they last said is remembered, and cannot be acted on", off) +
    group("never connected", "added here, and has not dialled in yet", never);
}

// withOwnRoom makes sure the hub's own machine has a row, even when it is not
// running agents.
//
// THE SWITCH LIVES BEHIND THAT ROW'S COG, so the row has to exist before the
// switch is ever thrown. A hub's own room is written down when it is turned on,
// which means that without this the one control that turns it on would only
// appear once it already was.
//
// Drawn as never-connected, which is what it is: the hub knows the machine is
// there and has nothing running on it.
async function withOwnRoom(rooms) {
  let own;
  try { own = await plainFetch("/_hub/room").then(x => x.json()); } catch (e) { return rooms; }
  if (!own || !own.available) return rooms;
  const name = String(own.name || "");
  if (!name || rooms.some(r => String(r.name).toLowerCase() === name.toLowerCase())) {
    return rooms;
  }
  // PUT BACK INTO THE LIST EVERYTHING ELSE READS, not only into what is drawn.
  // The cog looks its room up by name in `hubInventory`, so a row that exists
  // only in the markup is a cog that opens on nothing.
  hubInventory = rooms.concat([{
    name: name, transport: "local", state: "active",
    attached: false, cards: 0, waiting: false,
  }]);
  return hubInventory;
}

// thisMachineRow is the one row a board with no hub draws.
//
// No name, because there is nothing to tell it apart from: one atrium, one
// machine, and a name would be a label for a set of one. No transport badge and
// no state chip either, for the same reason. What it carries is the cog, which
// is the whole point of drawing it.
function thisMachineRow() {
  return `<div class="panel roomrow">
    <div class="col-head" style="margin:0 0 8px">
      <span>this machine</span>
      <span class="grow"></span>
      <button class="ghost roomcog" data-room="" title="settings for this machine"
        >&#9881;</button>
    </div>
    <div class="hintline">The editor command, where pasted files land, what the picker may
      open, the worktree command, the shell and the scrollback are all behind that cog.
      They are facts about this machine, and a board serving several would ask them of
      each one separately.</div>
  </div>`;
}

// roomRow is one room, however it is doing.
//
// NO BUTTON TO FOCUS A ROOM. The counter in the header is the selector, and a
// second control doing the same thing in a different place is a second thing to
// find, to keep in step and to be surprised by. This list says what is there.
// The header says which one you are in.
function roomRow(r) {
  const here = roomNow() === r.name;
  // WHAT THE MACHINE CALLS ITSELF, BESIDE WHAT IT IS CALLED, never instead of.
  // The hub's name is the name and routes. The machine's own is observed, and
  // what a machine reports never overwrites what a human typed.
  const self = r.self_name && r.self_name.toLowerCase() !== String(r.name).toLowerCase()
    ? `<span class="by">calls itself ${esc(r.self_name)}</span>` : "";
  return `<div class="panel roomrow">
    <div class="col-head" style="margin:0 0 8px">
      <span>${esc(r.name)}</span>
      ${transportBadge(r.transport)}
      ${roomState(r)}
      ${r.marked || r.state === "marked-for-deletion"
        ? `<span class="chip no">marked for deletion</span>` : ""}
      ${here ? `<span class="chip">the board is scoped to this one</span>` : ""}
      <span class="grow"></span>
      ${self}
      <button class="ghost roomcog" data-room="${esc(r.name)}"
        title="settings for this room">&#9881;</button>
    </div>
    <div class="hintline">${roomLine(r)}</div>
  </div>`;
}

// transportBadge is how a room reaches this hub.
//
// A BADGE AND NOTHING MORE. Worth seeing at a glance, never worth a column:
// transport is ancillary noise and must not become a concept in the UI.
function transportBadge(t) {
  const marks = {
    direct: ["mTLS", "a direct connection, over mutual TLS"],
    ziti: ["ziti", "over an OpenZiti service"],
    zrok: ["zrok", "over a private zrok share"],
    "zrok-public": ["zrok", "over a public zrok share"],
    local: ["here", "this hub's own machine, over a pipe inside one process"],
  };
  const m = marks[t] || [t || "?", "how this room reaches the hub"];
  return `<span class="chip" title="${esc(m[1])}">${esc(m[0])}</span>`;
}

// roomState is the one chip that says how the room is doing.
function roomState(r) {
  if (r.attached) return `<span class="chip live idle">attached ${esc(shortTime(r.since))}</span>`;
  // THE HUB'S OWN ROOM IS OFF, NOT MISSING. Nothing has failed to connect: this
  // is the machine the board is served from, and running agents on it is a
  // switch somebody has not thrown.
  if (r.transport === "local") return `<span class="chip">not running agents</span>`;
  if (!r.first_seen) {
    return r.waiting ? `<span class="chip">waiting to join</span>`
      : `<span class="chip">no join string outstanding</span>`;
  }
  return `<span class="chip no">last heard from ${esc(shortTime(r.last_seen))}</span>`;
}

// roomLine is the sentence under a room, and it is different for each state
// because the useful thing to say about each one is different.
function roomLine(r) {
  if (r.attached) {
    return `Its agents, its terminals and its database are on that machine and reachable
      through this board. Restarting this hub does not touch them.`;
  }
  if (r.transport === "local") {
    return `This is the machine the hub itself is on. Its cog turns agents on here, which
      is off by default: a hub that holds a database is a hub whose restart is no longer
      free, and that freedom is the whole reason the hub and the rooms are separate.`;
  }
  if (!r.first_seen) {
    return r.waiting
      ? `A join string was minted for this name and has not been used. Paste it into
         <code>atrium2 join</code> on the machine that will be this room.`
      : `Nothing has ever connected as this room. <code>atrium2 hub room token
         ${esc(r.name)}</code> on the hub prints a fresh join string.`;
  }
  // AN OFFLINE ROOM CANNOT BE ACTED ON, and saying so here is cheaper than
  // letting somebody find out by clicking. The cache is what was last heard,
  // shown as such, and the only authoritative answer comes from the room.
  const cards = r.cards
    ? `${r.cards} card${r.cards === 1 ? "" : "s"} were on it when it was last heard from, and
       what is shown of them is remembered rather than current. `
    : "";
  return cards + `Nothing on this room can be opened or changed until it is back.`;
}

// loadInventory asks what rooms EXIST, which is a different question from what
// is attached and has its own endpoint for exactly that reason.
//
// `/_hub/rooms` is the live list, and the picker, the grouping, the counters
// and the question about where a write lands all mean that one. A laptop
// somebody shut last week belongs on this tab and nowhere else.
//
// A hub with no record of its rooms answers `durable: false` and hands back
// what is attached. Drawn the same way, because a hub that can only see what is
// connected is telling the truth about what it knows.
let hubInventory = [];
async function loadInventory() {
  try {
    const got = await plainFetch("/_hub/inventory");
    if (!got.ok) throw new Error("no inventory");
    const out = await got.json();
    hubInventory = (out.rooms || []).slice().sort((a, b) =>
      String(a.name).localeCompare(String(b.name)));
  } catch (e) {
    hubInventory = hubRooms.map(r => Object.assign({ attached: true }, r));
  }
  return hubInventory;
}

// The cog is wired by DELEGATION, not by an inline handler, and that is a rule
// rather than a preference.
//
// A room name is durable data somebody typed, and building `onclick="openRoomCog(
// '...')"` out of it means hand-escaping a string for two nested contexts at
// once: an HTML attribute and the JavaScript inside it. Entity escaping does not
// do that job, because character references are decoded BEFORE the handler is
// compiled, so a name carrying a quote either breaks the button or runs as code.
// The board may be served over an overlay, which makes that worth ruling out by
// construction instead of by trusting the name check on the way in.
//
// The name rides in a `data-` attribute, where it is only ever text, and the
// listener reads it back as a string. There is no second context to escape for.
document.addEventListener("click", e => {
  const cog = e.target.closest && e.target.closest(".roomcog");
  if (!cog) return;
  e.preventDefault();
  openRoomCog(cog.getAttribute("data-room") || "");
});

// openRoomCog is the settings for one room.
//
// THE COG IS PER ROOM BECAUSE THE SETTINGS ARE. A room is the unit, not a
// machine: one machine can hold more than one room, and a hub can be a room
// itself. Everything that is a fact about one room belongs behind here.
//
// What it holds today is what the hub knows. The editor command, where pasted
// files land, the picker roots, the worktree command, the shell and the
// scrollback are still in `settings -> this machine`, and moving them here is
// the next piece of work.
// roomCfgFor is the room whose settings are open, empty for none.
//
// IT IS ALSO HOW THE FIELDS KNOW WHERE TO WRITE. The savers were written
// against one machine and call `/v1/settings` with no idea a hub exists. They
// land in the right place because this sets `writeRoom`, which the fetch
// wrapper puts on every write. Clearing it on close is not tidiness: a stale
// one would send the next save somewhere nobody was looking.
//
// The empty string is a real value here and means "no hub", where every request
// already goes to the only atrium there is.
let roomCfgFor = "";
let roomCfgOpen = false;

async function openRoomCog(name) {
  const dlg = document.getElementById("roomcfg");
  if (!dlg) return;
  const r = hubInventory.find(x => String(x.name) === String(name));
  if (name && !r) { tellUser("rooms", "that room is not on this hub any more"); return; }

  roomCfgFor = name || "";
  roomCfgOpen = true;
  writeRoom = roomCfgFor;

  document.getElementById("rc-heading").textContent = name ? "room " + name : "this machine";
  paintRoomFacts(r);

  // THE HUB'S OWN ROOM IS THE ONLY ONE WITH A SWITCH, because it is the only
  // one this process can start or stop. Every other room is a machine
  // somewhere else that was started by somebody there.
  const own = document.getElementById("rc-own-field");
  let ownState = null;
  if (own) {
    try { ownState = await plainFetch("/_hub/room").then(x => x.json()); } catch (e) { }
    const mine = ownState && ownState.available &&
      (!name || String(ownState.name || "") === String(name));
    own.hidden = !mine;
    if (mine) {
      const b = document.getElementById("rc-own");
      b.textContent = ownState.on ? "stop running agents here" : "run agents here too";
      b.className = ownState.on ? "no" : "go";
    }
  }

  // AN OFFLINE ROOM ANSWERS NOTHING. The only authoritative answer about a room
  // comes from that room, so there is no version of this pane that reads from
  // the cache: the cache holds what the board drew, not what a machine is set
  // up to do. The fields are hidden rather than shown empty, because an empty
  // box that saves nowhere is worse than no box.
  const live = !name || (r && r.attached);
  showRoomFields(live, r && r.transport === "local" ? "own" : "offline");
  if (live) await loadRoomCfg();

  // The fields in here save themselves on change, and the wiring is done once
  // per element. Called here rather than at startup because this dialog can be
  // opened before the settings one ever is.
  if (typeof wireSelfSaving === "function") wireSelfSaving();
  if (!dlg.open) dlg.showModal();
}

// paintRoomFacts is what the hub knows without asking the room.
function paintRoomFacts(r) {
  const table = document.getElementById("rc-facts");
  const state = document.getElementById("rc-state");
  const field = document.getElementById("rc-facts-field");
  if (!table || !field) return;
  if (!r) {
    // A board with no hub. There is one atrium and it is this one, so there is
    // nothing to say about which machine this is.
    field.hidden = true;
    return;
  }
  field.hidden = false;
  const when = t => t ? new Date(t).toLocaleString() : "never";
  const facts = [
    ["called", esc(r.name) +
      ` <span class="by">by this hub, and this is the name that routes</span>`],
    ["calls itself", esc(r.self_name || "nothing has connected yet")],
    ["reaches the hub", esc(r.transport || "")],
    ["running", esc(r.version || "not known yet")],
    ["first connected", esc(when(r.first_seen))],
    ["last heard from", esc(when(r.last_seen))],
  ];
  setHTML(table, facts
    .map(f => `<tr><td class="by">${f[0]}</td><td>${f[1]}</td></tr>`).join(""));

  const marked = r.state === "marked-for-deletion";
  state.innerHTML = (marked
    ? `<b>Marked for deletion.</b> It starts no new cards, everything already running
       carries on, and taking the mark off puts it back into ordinary service. `
    : ``) +
    `Adding a room, replacing its join string and removing one are done at a terminal on
     the hub, under "atrium2 hub room". The board does not mint credentials: atrium has
     no login, and this page may be reachable from elsewhere.`;

  const mark = document.getElementById("rc-mark");
  if (mark) {
    // NOT FOR THE HUB'S OWN ROOM. Marking is "start no new cards here, I am
    // going to remove this", and removing the machine the board is served from
    // is not a thing this button could do. The switch above is what turns that
    // room off, and it is the honest control for it.
    mark.hidden = r.transport === "local";
    mark.textContent = marked ? "take the mark off" : "mark for deletion";
    mark.className = marked ? "go" : "no";
  }
}

// showRoomFields hides the settings themselves for a room that cannot answer.
//
// `why` is which kind of not-answering this is, because the two read completely
// differently to whoever opened the pane. A room that is offline is a machine
// somebody has to go and look at. The hub's own room being off is a switch two
// inches above, and calling that "not answering" would be alarming about a
// thing the reader just chose.
function showRoomFields(on, why) {
  const body = document.querySelector("#roomcfg .dlg-body");
  if (!body) return;
  const keep = ["rc-facts-field", "rc-own-field", "rc-offline-field"];
  Array.from(body.children).forEach(el => {
    if (keep.indexOf(el.id) >= 0) return;
    el.hidden = !on;
  });
  const off = document.getElementById("rc-offline-field");
  if (!off) return;
  off.hidden = on;
  if (on) return;
  off.innerHTML = why === "own"
    ? `<span class="hintline">Nothing runs here yet, so there is nothing to set up.
       Turn agents on above and this machine's own settings appear.</span>`
    : `<span class="hintline"><b>This room is not answering.</b> The only authoritative
       answer about a room comes from that room, so nothing here can be read or changed
       until it is back. What is shown above is what it last said.</span>`;
}

// loadRoomCfg fills the fields from the room they belong to.
//
// An explicit header rather than the wrapper's, because the wrapper only scopes
// READS when the whole board is scoped. This pane is the case where one room is
// being asked about while the board is looking at all of them.
async function loadRoomCfg() {
  let s;
  try {
    const h = roomCfgFor ? { "X-Atrium-Room": roomCfgFor } : {};
    s = await plainFetch("/v1/settings", { headers: h }).then(x => x.json());
  } catch (e) {
    toast("could not read that room's settings", e.message);
    return;
  }
  fillMachineFields(s);
}

// closeRoomCfg puts the write scope back.
function closeRoomCfg() {
  const dlg = document.getElementById("roomcfg");
  if (dlg && dlg.open) dlg.close();
  else forgetRoomCfg();
}

// forgetRoomCfg is the part that must happen however this dialog went away.
//
// ESCAPE CLOSES A MODAL DIALOG AND CALLS NOTHING. Hanging the scope reset off
// the close button alone meant one press of escape left `writeRoom` pointing at
// the room whose pane had been open, and the next setting saved from anywhere
// on the board would go there instead. Silent, and only visible later as one
// machine having the other's editor command. Found by review.
function forgetRoomCfg() {
  roomCfgOpen = false;
  roomCfgFor = "";
  writeRoom = "";
  // And the settings dialog's own remembered answer, which was a choice made
  // when per-machine settings lived there. Leaving it set would have the same
  // effect one dialog over.
  if (typeof settingsRoom !== "undefined") settingsRoom = "";
  renderRooms();
}

// Every way out of the dialog, including the ones nothing on this page calls:
// escape, the backdrop, and a form inside it.
//
// Wired now when the markup is already here, and on load when it is not, since
// which of those is true depends on where in the page this script is included
// and that is not a thing this file should have an opinion about.
(function wireRoomCfgClose() {
  const dlg = document.getElementById("roomcfg");
  if (dlg) { dlg.addEventListener("close", forgetRoomCfg); return; }
  document.addEventListener("DOMContentLoaded", () => {
    const late = document.getElementById("roomcfg");
    if (late) late.addEventListener("close", forgetRoomCfg);
  });
})();

// toggleOwnRoom throws the hub's own-room switch from inside the cog.
//
// NO CONFIRMATION. It is one click to undo, the button says what it does, and
// that agents on a machine stop when that machine's atrium stops is not news.
async function toggleOwnRoom() {
  const b = document.getElementById("rc-own");
  const on = b && b.className === "no";
  try {
    await plainFetch("/_hub/room", {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ on: !on })
    });
  } catch (e) { tellUser("atrium", "could not change that: " + e.message); return; }
  hubRead = 0;
  await loadHubRooms();
  renderRooms();
  await openRoomCog(roomCfgFor);
}

// markFromCog is the mark button inside the room's own pane.
async function markFromCog() {
  const r = hubInventory.find(x => String(x.name) === String(roomCfgFor));
  if (!r) return;
  await markRoom(r.name, r.state !== "marked-for-deletion");
  await openRoomCog(roomCfgFor);
}

// markRoom is the one change the board can make to a room.
async function markRoom(name, marked) {
  try {
    const got = await plainFetch("/_hub/inventory/mark", {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ name: name, marked: !!marked })
    });
    if (!got.ok) {
      const why = await got.json().catch(() => ({}));
      throw new Error(why.error || "the hub refused that");
    }
  } catch (e) { tellUser("rooms", e.message); return; }
  hubRead = 0;
  await loadHubRooms();
  renderRooms();
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
  // THE DURABLE LIST COMES WITH IT, because the header's counter needs both
  // halves: how many rooms are answering, and how many exist to answer. Asked
  // here rather than only when the rooms tab is open, or the counter would
  // report nothing missing until somebody went looking for it, which is the
  // opposite of what a counter is for.
  await loadInventory();
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
