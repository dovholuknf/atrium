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

// The attached room set as last seen, joined into one string to compare by. Null
// until the first read, so the first poll seeds it without counting as a change.
// A change here churns every card's id (see `bareId` in notify.js), so it is
// what tells the alerter to re-seed rather than announce the whole board.
let attachedRoomKey = null;

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

  // TWO CONCERNS, TWO INDICATORS. The room chip counts rooms. Whether THIS board
  // still has its stream to the hub is a different fact, and folding it into the
  // count was misleading: a chip reading `reconnecting` said the rooms were in
  // trouble when the rooms were fine and it was the browser's own link that had
  // dropped. So the count always counts, and `#conn` says `reconnecting` on its
  // own, shown only while the stream is down and hidden while it is up.
  const streamUp = !conn || conn.classList.contains("live");
  if (conn) conn.hidden = streamUp;

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
  //
  // NEITHER IS THE HUB'S OWN MACHINE. Off is a switch somebody threw, not a
  // room that went missing, so it must not sit in the denominator making the
  // counter read as though something needs looking at.
  const everConnected = hubInventory.filter(r =>
    r.transport !== "local" && (r.attached || r.first_seen)).length;
  const known = Math.max(live, everConnected, room ? 1 : 0);
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
  // `down` belonged to the folded-in stream state and coloured the whole chip as
  // though a room were in trouble. The stream now speaks through `#conn`, so the
  // room chip is never `down`: it reports rooms and nothing else.
  el.classList.remove("down");
  el.classList.toggle("cold", here === 0);
  el.classList.toggle("scoped", !!room);
}

// roomPickerRows builds the menu's rows from what is attached and known now.
//
// Factored out of `openRooms` so the same markup serves the open action and the
// live re-render (see `refreshRoomsMenu`): the menu that is already open must
// paint the exact rows it would paint if reopened, or a room that just attached
// would read one way open and another way reopened.
function roomPickerRows() {
  const room = roomNow();
  const rows = [
    `<button class="${room ? "" : "on"}" onclick="pickRoom('')">
       <span class="dot ghost"></span>
       <strong>all rooms</strong></button>`
  ];
  for (const r of hubRooms) {
    const name = esc(r.name);
    rows.push(`<button class="${room === r.name ? "on" : ""}"
      onclick="pickRoom('${name.replace(/'/g, "&#39;")}')">
        <span class="dot live"></span>
        <strong>${name}</strong>${r.host
          ? `<span class="meta">${esc(r.host)}</span>` : ""}</button>`);
  }
  // Rooms that dialled in before and are not attached now. Listed after the live
  // ones and dimmed, because the board handles an offline room by showing what it
  // last said, so switching to one is a real thing to do. A room that has never
  // connected stays out: it has nothing to show and belongs on the rooms tab.
  // The currently scoped room is drawn by the block below, so it is skipped here.
  for (const r of hubInventory) {
    if (r.attached || !r.first_seen || r.transport === "local") continue;
    if (r.name === room) continue;
    if (hubRooms.some(h => h.name === r.name)) continue;
    const name = esc(r.name);
    const q = name.replace(/'/g, "&#39;");
    // A REJECT AFFORDANCE, ON A NOT-ATTACHED ROW ONLY. A stale or duplicate
    // record sits here with nothing to remove it, so this culls it from the
    // hub. It is a plain element rather than a nested button, because a button
    // inside a button is not markup a browser will honour, and it stops the
    // click so choosing to forget a room is never also choosing to look at it.
    rows.push(`<button class="cold"
      onclick="pickRoom('${q}')">
        <span class="dot"></span>
        <strong>${name}</strong>
        <span class="meta">disconnected${r.last_seen
          ? " &middot; " + esc(shortTime(r.last_seen)) : ""}</span>
        <span class="roomx" title="forget ${name}, removing this stale room from the hub"
          onclick="event.stopPropagation();rejectRoom('${q}')">&times;</span></button>`);
  }
  // A room that was chosen and has since gone. Kept in the list so there is
  // something to click your way out of.
  if (room && !hubRooms.some(r => r.name === room)) {
    rows.push(`<button class="on cold">
      <span class="dot"></span>
      <strong>${esc(room)}</strong>
      <span class="meta">not attached</span></button>`);
  }
  if (!hubRooms.length) {
    rows.push(`<div class="none">no room is attached. the hub serves this board
      and holds nothing, so until a room connects there is nothing to show.</div>`);
  }
  return rows.join("");
}

// openRooms is the menu the chip opens.
function openRooms() {
  const menu = document.getElementById("rooms-menu");
  if (!menu) return;
  if (!menu.hidden) { menu.hidden = true; return; }
  menu.innerHTML = roomPickerRows();
  menu.hidden = false;
  // Placed from the chip rather than anchored to it, because the menu lives at
  // the end of the body and not inside the header. Clamped to the left so a
  // long room name cannot push it off the edge of a narrow window.
  const at = document.getElementById("rooms").getBoundingClientRect();
  menu.style.top = (at.bottom + 8) + "px";
  menu.style.left = Math.max(8, Math.min(at.right - menu.offsetWidth,
    window.innerWidth - menu.offsetWidth - 8)) + "px";
}

// refreshRoomsMenu repaints the OPEN picker when the attached-room set changes.
//
// The chip's counter is reactive on the `rooms` event, but the dropdown painted
// once on open and then held a snapshot: a room brought online while the menu
// was open still read as "disconnected" until the user closed and reopened it.
// Called from `loadHubRooms`, so it rides the existing `rooms` event and the
// backstop poll rather than a timer of its own.
//
// Only the rows are rewritten. The menu stays open and keeps its placement: the
// top and left set on open are left untouched so it does not jump or reposition
// under the user while they are reading it.
function refreshRoomsMenu() {
  const menu = document.getElementById("rooms-menu");
  if (!menu || menu.hidden) return;
  menu.innerHTML = roomPickerRows();
}

// roomChip is a card's room, in the aggregate view.
//
// Drawn as a tag because that is what the operator asked for: it sits with the
// tags and it can be filtered on later. Clicking it does the more useful thing
// than filtering, which is to focus the whole board on that room.
//
// USED BY EVERY LIST THAT MERGES ROOMS, which is the board, the stack and the
// history. The history called a name that had never existed, so its rows threw
// on every render and the tab drew nothing at all on a hub: a missing function
// is a silent, total failure of one view.
//
// Never drawn when scoped, because then every card is from the same room and a
// chip saying so on all of them is noise.
function roomChip(t) {
  if (!hubIsHub || roomNow() || !t || !t.room) return "";
  // NOT ON A CARD IN THE OFFLINE GROUP. That group says which rooms it holds,
  // once, in its heading, so a chip on every card repeats it. And this chip's
  // job is to focus the board on that room, which for a machine that is not
  // answering is an invitation to a board that can show nothing.
  if (t.offline) return "";
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
  const rest = hubInventory;

  if (!rest.length) {
    return `<div class="panel"><div class="empty">
      No rooms yet. A hub serves the board; the agents run on rooms, which are machines
      that dial in. <a href="#" onclick="openRoomJoin();return false;">Add one</a> to get started.
    </div></div>`;
  }

  // Live first, then rooms that have gone quiet, then ones that were added but
  // have never dialled in. What is running is what the board is for.
  const live = rest.filter(r => r.attached);
  const off = rest.filter(r => !r.attached && r.first_seen);
  const never = rest.filter(r => !r.attached && !r.first_seen);

  // HEADERS ONLY WHEN THERE IS MORE THAN ONE KIND. When every room is just
  // connected, "connected rooms" over the top of them is a label for the
  // obvious. It earns its place only when it is telling live apart from the rest.
  const kinds = [live, off, never].filter(g => g.length).length;
  const group = (title, list) => !list.length ? "" :
    (kinds > 1
      ? `<div class="roomgroup"><span>${esc(title)}</span></div>` : "") +
    `<div class="panel roomlist">` + list.map(roomRow).join("") + `</div>`;

  return group("connected rooms", live) +
    group("disconnected", off) +
    group("added but never connected", never);
}

// thisMachineRow is the one row a board with no hub draws.
//
// No name, because there is nothing to tell it apart from: one atrium, one
// machine, and a name would be a label for a set of one. No transport badge and
// no state chip either, for the same reason. What it carries is the cog, which
// is the whole point of drawing it.
function thisMachineRow() {
  return `<div class="panel roomlist"><div class="roomrow has-hint">
    <div class="roomrow-top">
      <b class="roomname">this machine</b>
      <span class="grow"></span>
      <button class="ghost roomcog" data-room="" title="settings for this machine"
        >&#9881;</button>
    </div>
    <div class="hintline">Editor, paste, shell and scrollback settings are behind the cog.</div>
  </div></div>`;
}

// roomRow is one room, however it is doing.
//
// NO BUTTON TO FOCUS A ROOM. The counter in the header is the selector, and a
// second control doing the same thing in a different place is a second thing to
// find, to keep in step and to be surprised by. This list says what is there.
// The header says which one you are in.
function roomRow(r) {
  const here = roomNow() === r.name;
  const line = roomLine(r);
  return `<div class="roomrow${line ? " has-hint" : ""}">
    <div class="roomrow-top">
      <b class="roomname">${esc(r.name)}</b>
      ${transportBadge(r.transport)}
      ${roomState(r)}
      ${r.marked || r.state === "marked-for-deletion"
        ? `<span class="chip no">marked for deletion</span>` : ""}
      ${here ? `<span class="chip">showing this one</span>` : ""}
      <span class="grow"></span>
      <button class="ghost roomcog" data-room="${esc(r.name)}"
        title="settings for this room">&#9881;</button>
    </div>
    ${line ? `<div class="hintline">${line}</div>` : ""}
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
  // A LIVE ROOM NEEDS NO LINE. It is attached, it works, and repeating that
  // under every row was the noise. Only say something when there is something
  // to do about it.
  if (r.attached) return "";
  if (!r.first_seen) {
    return r.waiting
      ? `Waiting to connect — run the join string on that machine.`
      : `Never connected. <code>atrium2 hub room token ${esc(r.name)}</code> for a new one.`;
  }
  const cards = r.cards ? `${r.cards} card${r.cards === 1 ? "" : "s"} last seen. ` : "";
  return cards + `Offline until it's back.`;
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
    const out = await cappedFetch(plainFetch, "/_hub/inventory", undefined, async res => {
      if (!res.ok) throw new Error("no inventory");
      return res.json();
    });
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
    ["name", esc(r.name) + ` <span class="by">what the board calls it</span>`],
    ["calls itself", esc(r.self_name || "hasn't connected yet")],
    ["connects over", esc(r.transport || "")],
    ["version", esc(r.version || "unknown")],
    ["first seen", esc(when(r.first_seen))],
    ["last seen", esc(when(r.last_seen))],
  ];
  setHTML(table, facts
    .map(f => `<tr><td class="by">${f[0]}</td><td>${f[1]}</td></tr>`).join(""));

  const marked = r.state === "marked-for-deletion";
  state.innerHTML = (marked
    ? `<b>Marked for deletion.</b> No new work starts here; what's running finishes.
       Take the mark off to use it again. `
    : ``) +
    `Add, re-key or remove a room from a terminal on the hub: <code>atrium2 hub room</code>.
     The board can't do it, since anyone who can open this page could reach it.`;

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
    ? `<span class="hintline">Nothing runs here yet. Turn agents on above and this
       machine's settings show up.</span>`
    : `<span class="hintline"><b>Offline.</b> Its settings live on that machine, so there's
       nothing to change from here until it's back. Above is what it last told us.</span>`;
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

// rejectRoom culls a stale or duplicate room record from the hub's picker.
//
// FORGET, NOT BAN, and the confirm says so: the record goes, the machine is
// untouched, and a room still running `atrium2 join` writes itself back down
// and reappears. The hub refuses an attached room by name, so this is offered
// only on a not-attached row, and any refusal it does answer with is shown as
// the sentence the hub sent rather than a generic apology.
//
// Named apart from runners.js `forgetRoom`, which drops a legacy `/v1/rooms`
// federation record on the room daemon. This is the hub's own known-set, at
// `/_hub/inventory/forget`, and the two must not be confused.
async function rejectRoom(name) {
  if (!await confirmUser("forget " + name + "?",
    "It comes off this hub's picker now. Nothing on that machine changes, and this is not a " +
    "block: if it is still running atrium it writes itself back down and reappears. Use this " +
    "for a stale or duplicate record of a machine that has gone.",
    "forget it")) return;
  try {
    const got = await plainFetch("/_hub/inventory/forget", {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ name: name })
    });
    if (!got.ok) {
      const why = await got.json().catch(() => ({}));
      throw new Error(why.error || "the hub refused that");
    }
  } catch (e) { tellUser("rooms", e.message); return; }
  hubRead = 0;
  await loadHubRooms();
  refreshRoomsMenu();
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
    // The room's name heads its group, and its cog opens that room's settings,
    // the same one the rooms tab opens. It is that machine's set-up, reached
    // from the list of what runs on it.
    return `<div class="col-head roomgroup"><span>${esc(name)}</span>
      <span class="grow"></span>
      <button class="ghost roomcog" data-room="${esc(name)}"
        title="settings for ${esc(name)}">&#9881;</button></div>` + panel(mine);
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
    // Through the shared cap, not a raw plainFetch: a flapping room asks this on
    // every flip, and a popped-out window has no other refresh to bound it.
    got = await cappedFetch(plainFetch, "/_hub/rooms", undefined, async res => {
      if (!res.ok) throw new Error("not a hub");
      return res.json();
    });
  } catch (e) {
    hubIsHub = false;
    paintRooms();
    return false;
  }
  hubIsHub = true;
  hubRooms = (got.rooms || []).slice().sort((a, b) =>
    String(a.name).localeCompare(String(b.name)));
  // A room attaching or detaching flips every card's id between `room~id` and
  // bare when the count crosses 1<->2, which the alerter would otherwise read as
  // the whole board arriving at once. Re-seed its baseline on that tick so it
  // announces nothing. Done here, the moment the set is read, rather than after
  // `loadInventory`, so the reseed lands before the refresh that follows.
  const roomKey = hubRooms.map(r => r.name).join("\n");
  if (attachedRoomKey !== null && roomKey !== attachedRoomKey &&
      typeof alerting !== "undefined" && alerting.reseed) {
    alerting.reseed();
  }
  // A ROOM ATTACHING RE-RESOLVES THE SKIN, because the ALL view borrows its
  // settings from a room and a hub with none yet answers `/v1/settings` with a
  // 409. The load-time read (see bootSkin) then failed and left the board on the
  // default; when the first room attaches the stream is already open, so the
  // reconnect path does not fire, and only this does. Re-read on any change to
  // the attached set, which the api cap bounds and which is rare next to a poll.
  // See applyResolvedSkin.
  if (roomKey !== attachedRoomKey && typeof bootSkin === "function") bootSkin();
  attachedRoomKey = roomKey;
  // THE DURABLE LIST COMES WITH IT, because the header's counter needs both
  // halves: how many rooms are answering, and how many exist to answer. Asked
  // here rather than only when the rooms tab is open, or the counter would
  // report nothing missing until somebody went looking for it, which is the
  // opposite of what a counter is for.
  await loadInventory();
  paintRooms();
  // The audit tab is hub-only too, and this is where "is this a hub" is decided.
  if (typeof paintAuditTab === "function") paintAuditTab();
  // The chip repainted above; the OPEN dropdown has to as well, or a room that
  // just attached keeps reading "disconnected" in a menu the user left open.
  refreshRoomsMenu();
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
