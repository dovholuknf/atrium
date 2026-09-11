// ── runners ─────────────────────────────────────────────
let allHarnesses = [];
let launchTarget = null;

// Which pane of the runners page was last open, remembered the same way the
// settings dialog remembers its own. Per browser: it is about this screen.
const RUNNERS_PANE = "atrium.runnersPane";

// Cut into panes ONCE, on the first render, and never again.
//
// `splitIntoPanes` moves elements into new parents, so calling it twice would
// re-parent the panes into a pane. It guards on `dataset.split` for that, and
// this is the call site that would otherwise test the guard on every refresh.
//
// It runs here rather than at boot because `#runners` starts hidden, and the
// nav is measured against a sticky position that a hidden element has no
// answer for.
// Going to the runners page AND landing on the right pane.
//
// Both callers arrive with a reason: one pressed `configure` in the launch
// dialog, the other was told nothing is switched on. Dropping either on
// whichever pane was open last means arriving somewhere that does not answer
// the sentence that sent you.
//
// The split happens in `renderRunners`, which `switchView` calls, so the pane
// is asked for after that rather than before: there are no panes to show yet
// on the first visit.
function goRunners(pane) {
  switchView("runners");
  const host = document.getElementById("runners");
  if (pane && host && host.dataset.split) {
    showPane(host, pane, RUNNERS_PANE, { scroller: document.querySelector("main") });
  }
}

function splitRunners() {
  splitIntoPanes(document.getElementById("runners"), "r-section", RUNNERS_PANE, {
    // The page scrolls in `main`, not in the pane, so switching panes has to
    // scroll the thing that actually moved. Without this, changing pane on a
    // long list leaves you halfway down a short one.
    scroller: document.querySelector("main")
  });
}

// Other machines reporting into this one.
//
// Read rather than remembered, and drawn as a summary rather than as cards. A
// remote card is not something this board can act on: its terminal is on that
// machine and cannot leave it, so the useful thing per row is a link to that
// room's own board rather than a menu of things that would fail.
// The room list, read once per tick rather than once per reader.
//
// Three things want it now: this pane, the permission queue, and the alert loop
// that runs whichever view is open. All three fire on the same five second
// refresh, so without this they ask the daemon the same question three times
// and can disagree about the answer within one frame.
//
// A failure is remembered as an empty list, not left as the last good one. An
// older daemon has no rooms endpoint, and a room that has gone should stop
// being drawn rather than staying on the board because the fetch that would
// have removed it failed.
let roomsRead = { at: 0, rooms: [], answered: false };
async function loadRooms() {
  if (Date.now() - roomsRead.at < 2000) return roomsRead.rooms;
  let rooms = [], answered = true;
  try {
    rooms = (await api("/v1/rooms")).rooms || [];
  } catch (e) {
    answered = false;
  }
  roomsRead = { at: Date.now(), rooms, answered };
  return rooms;
}

// Every request waiting for a human on another machine, in the shape the local
// queue arrives in, so one panel and one alert path can draw both.
async function remoteRequests() {
  const out = [];
  for (const r of await loadRooms()) {
    for (const q of r.requests || []) out.push(remoteView(r, q));
  }
  return out;
}

// One remote request, made to look local.
//
// THE ID IS NAMESPACED BY THE ROOM, and that is not decoration. Every alert,
// every toast, every nag slot and every node in the perms panel is keyed by the
// request id, and each machine numbers its own requests, so an unqualified id
// is a key two rooms could both claim. The unqualified one travels alongside,
// because the decision has to be addressed to that room in its own terms.
//
// The task id is namespaced for a stronger reason: it is what decides whether a
// card has a window of its own open, and a card on another machine never does.
// Left bare, it could match a local card and hand the alert to the wrong
// window.
function remoteView(room, q) {
  return Object.assign({}, q, {
    id: room.name + "/" + q.id,
    perm_id: q.id,
    task_id: q.task_id ? room.name + "/" + q.task_id : "",
    room: room.name,
    room_board: room.board || "",
    room_stale: !!room.stale
  });
}

// A remote card's terminal, as a link to the board that owns it.
//
// THIS IS WHAT ATTACH ACROSS MACHINES IS. A pseudo terminal cannot leave the
// machine that made it, and `docs/federation-design-v2.md` records that this is
// a fact about ConPTY rather than a policy, so the closest thing available is
// sending you to that machine's own board with the right terminal already open.
// The room already reports where its board is, so what was missing was only the
// fragment.
//
// The fragment REPLACES anything already on the address rather than being added
// to it. A URL has one fragment, and appending a second produces one that says
// the wrong thing.
//
// Offered only for the three states where somebody would want to type. A card
// that is finished, dead or offered has nothing behind it to attach to, and a
// link that opens an empty terminal pane reads as a broken link rather than as
// a card with no session.
const TERM_LINK_STATES = ["needs-input", "needs-permission", "running"];
function termLinkTo(board, card) {
  if (!board || !card || !card.id || !card.runner) return "";
  if (!TERM_LINK_STATES.includes(card.status)) return "";
  return String(board).split("#")[0] + "#term=" + encodeURIComponent(card.id);
}

async function renderRooms() {
  const el = document.getElementById("room-list");
  if (!el) return;
  const rooms = await loadRooms();
  if (!roomsRead.answered) {
    // A daemon too old to have the endpoint. Not an error worth drawing:
    // nothing has ever been in this list on that machine.
    setHTML(el, "");
    return;
  }
  if (!rooms.length) {
    setHTML(el, `<div class="panel"><div class="empty">
      No other machines are reporting in. <a href="#" onclick="openRoomJoin();return false;">Add
      a room</a> to see what to run on one.
    </div></div>`);
    return;
  }
  setHTML(el, rooms.map(r => {
    // STALE IS SAID, not hidden. A room that has stopped checking in is the
    // thing worth noticing, and dropping it from the list would make a machine
    // that died look like one that was never there.
    const when = r.stale
      ? `<span class="chip warn">not heard from for ${esc(shortTime(r.last_seen))}</span>`
      : `<span class="chip live idle">checked in ${esc(shortTime(r.last_seen))}</span>`;
    // A DEADLINE, not just a state. "Stale" on its own tells you something is
    // wrong; stale with the time it drops off tells you whether to wait or go
    // and look at that machine. The seconds come from the daemon so the page
    // is not carrying a second copy of the constant.
    const gone = r.stale
      ? `<div class="hintline">Either that machine stopped or it cannot reach this one. What is
           listed below is what it last said. It drops off this list in
           ${esc(ago(r.forget_in_seconds))} unless it checks in again, and nothing here is kept
           when it does.</div>`
      : "";
    const cards = (r.cards || []).length
      ? `<div class="roomcards">` + r.cards.map(c => {
          // ATTACH BY REDIRECT. A pty cannot leave the machine that made it, so
          // the card's title is a link to that machine's own board with this
          // terminal already open, which is the nearest thing to attaching that
          // exists and is one click rather than a board, a tab and a search.
          //
          // Offered only for a card with a runner on it. `#term=` opens the
          // terminal pane, and a card with no session behind it would open an
          // empty one, which reads as the link being broken.
          const to = termLinkTo(r.board, c);
          const name = esc(c.title || c.id);
          return `<div class="roomcard">
             <span class="grow">${to
               ? `<a href="${esc(to)}" target="_blank" rel="noreferrer"
                    title="open this terminal on ${esc(r.name)}'s own board">${name}</a>`
               : name}</span>
             ${c.doing ? `<span class="by">${esc(c.doing)}</span>` : ""}
             <span class="chip ${c.status === "needs-input" || c.status === "needs-permission"
               ? "warn" : ""}">${esc(c.status)}</span>
           </div>`;
        }).join("") + `</div>`
      : `<div class="empty">nothing running there right now</div>`;
    // Requests are DRAWN HERE AS A COUNT AND ANSWERED IN THE PERMS TAB. Two
    // places to press approve would be two places to keep right, and the perms
    // tab is where the scope buttons, the command box and the history already
    // are. What this row owes you is knowing they exist.
    const asking = (r.requests || []).length;
    return `<div class="panel roomrow">
      <div class="col-head" style="margin:0 0 8px">
        <span>${esc(r.name)}</span>
        ${when}
        ${r.waiting ? `<span class="chip warn">${r.waiting} waiting</span>` : ""}
        <span class="grow"></span>
        <span class="by">${esc(r.host || "")}${r.version ? " &middot; " + esc(r.version) : ""}</span>
        <!-- The name comes back through the DOM, not through the handler. A
             room names ITSELF on check-in, so this string arrives from another
             machine, and one containing an escaped quote closes an inline
             handler no matter how the quote was escaped on the way in. -->
        <button class="no" data-room="${esc(r.name)}"
          onclick="forgetRoom(this.dataset.room)">forget</button>
      </div>
      ${gone}
      ${cards}
      ${asking ? `<div class="hintline"><b>${asking}</b> ${asking === 1
          ? "agent there is frozen waiting to be allowed something"
          : "agents there are frozen waiting to be allowed something"}.
        <a href="#" onclick="switchView('perms');return false">answer ${asking === 1
          ? "it" : "them"} here</a>, in the perms tab, without leaving this board.</div>` : ""}
      ${r.board
        // The link is the point of the row. Terminals do not federate, so
        // everything this board cannot do for a remote card is done there.
        ? `<div class="hintline">Its terminals stay on that machine.
             <a href="${esc(r.board)}" target="_blank" rel="noreferrer">open ${esc(r.name)}'s
             own board</a> to attach to one.</div>`
        : `<div class="hintline">That room did not say where its own board is, so there is
             no way to attach to anything on it from here.</div>`}
      ${roomWork(r)}
    </div>`;
  }).join(""));
}

// Whether this room will take work, and what a directory would be checked
// against if it does.
//
// SAID ON THE ROW rather than found out from a refusal. Queueing for a machine
// that has decided not to run anything is the mistake this line exists to stop,
// and the workspace is the other half of it: an item that names a directory is
// only honoured by a room that has one.
function roomWork(r) {
  if (!r.launches) {
    return `<div class="hintline">This room reports its cards and does not take work.
      It was started with <code>--no-launch</code>, so anything queued for it stays queued.</div>`;
  }
  if (r.busy) {
    return `<div class="hintline">This room is starting what it was last handed, so it is
      taking nothing else right now. Anything queued for it goes on its next check-in.</div>`;
  }
  const where = r.workspace
    ? `A queued launch may name a directory inside <code>${esc(r.workspace)}</code>, and nowhere else.`
    : `A queued launch may not name a directory, so each one runs where that machine's own
       runner is configured to. Atrium does not make the directory.`;
  return `<div class="hintline">${where}
    <a href="#" onclick="openDispatch('${esc(r.name)}');return false">send work to
    ${esc(r.name)}</a></div>`;
}

// Work this machine has handed to another one.
//
// Durable, unlike the room list above it, and the difference is whose fact it
// is: a room's cards belong to that room, and this is what this hub asked for.
// So a failed item stays drawn until it is swept, because the reason a launch
// did not happen on a machine nobody is watching is the whole value here.
async function renderDispatch() {
  const el = document.getElementById("dispatch-list");
  if (!el) return;
  let items;
  try {
    items = (await api("/v1/dispatch")).dispatches || [];
  } catch (e) {
    // An older daemon has no queue. Nothing has ever been in it there.
    setHTML(el, "");
    return;
  }
  if (!items.length) {
    setHTML(el, `<div class="panel"><div class="empty">
      Nothing is queued for another machine. Hand an item to a room and it starts there the
      next time that room checks in.
    </div></div>`);
    return;
  }
  setHTML(el, `<div class="panel">` + items.map(d => {
    const cls = d.state === "failed" ? "warn" : d.state === "running" ? "live idle" : "";
    // The reason it did not start, in that machine's own words, is the row.
    // Nothing else here is worth the space if this is filled in.
    const what = d.error
      ? `<div class="hintline">${esc(d.error)}</div>`
      : d.card_url
        ? `<div class="hintline"><a href="${esc(d.card_url)}" target="_blank"
             rel="noreferrer">open it on ${esc(d.room)}</a></div>`
        : d.prompt
          ? `<div class="hintline">${esc(d.prompt.slice(0, 160))}</div>`
          : "";
    return `<div class="roomcard" style="display:block">
      <div style="display:flex;gap:8px;align-items:center">
        <span class="grow">${esc(d.title || d.harness)} &middot; ${esc(d.room)}</span>
        ${d.cwd ? `<span class="by">${esc(d.cwd)}</span>` : ""}
        <span class="chip ${cls}">${esc(d.state)}</span>
        ${d.state === "queued"
          // Only while it is still here. Once a room has taken it there is
          // nothing to withdraw: the hub never dials a room, so it cannot
          // reach in and stop what it started.
          ? `<button onclick="cancelDispatch('${esc(d.id)}')">withdraw</button>`
          : ""}
      </div>
      ${what}
    </div>`;
  }).join("") + `</div>`);
}

// Queue work for a machine. `room` prefills the name when it came from a row.
function openDispatch(room) {
  document.getElementById("dp-room").value = room || "";
  document.getElementById("dp-runner").value = "claude";
  document.getElementById("dp-dir").value = "";
  document.getElementById("dp-title").value = "";
  document.getElementById("dp-prompt").value = "";
  document.getElementById("dispatch").showModal();
  document.getElementById(room ? "dp-prompt" : "dp-room").focus();
}

async function doDispatch() {
  const body = {
    room: document.getElementById("dp-room").value.trim(),
    harness: document.getElementById("dp-runner").value.trim(),
    cwd: document.getElementById("dp-dir").value.trim(),
    title: document.getElementById("dp-title").value.trim(),
    prompt: document.getElementById("dp-prompt").value,
  };
  try {
    await api("/v1/dispatch", {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    });
  } catch (e) {
    tellUser("atrium", e.message);
    return;
  }
  document.getElementById("dispatch").close();
  tellUser("atrium", `queued for ${body.room}. it starts there the next time that machine
    checks in, which is within twenty seconds if it is up.`);
  renderDispatch();
}

async function cancelDispatch(id) {
  try {
    await api("/v1/dispatch/" + encodeURIComponent(id), { method: "DELETE" });
  } catch (e) {
    // The refusal says to stop it on that machine, which is the useful half.
    tellUser("atrium", e.message);
  }
  renderDispatch();
}

// Drops a room from the list now rather than waiting for it to age out.
//
// IT IS NOT A BLOCK LIST and the dialog says so. The hub holds what it has been
// told and nothing else, so a machine that is still running `atrium room` comes
// straight back, and a durable record of a refusal would be exactly the second
// source of truth rooms exist to not have. What this is for is the machine that
// has gone: reimaged, renamed, or a test that left a name behind.
async function forgetRoom(name) {
  if (!await confirmUser("forget " + name + "?",
    "It comes off this board now instead of aging out. Nothing on that machine changes and " +
    "nothing is remembered here: if it is still running atrium room it reappears within a " +
    "heartbeat. Use this for a machine that has actually gone.",
    "forget it")) return;
  try {
    await api("/v1/rooms/" + encodeURIComponent(name), { method: "DELETE" });
  } catch (e) { tellUser("could not forget it", e.message); return; }
  renderRooms();
}

// What to run on the other machine, with the parts this hub knows filled in.
//
// Opening this creates NOTHING. There is no pending room and there cannot be:
// the hub holds no durable record of a room, so a half-added one would be a
// record of an intention, which is the thing this design does not keep. The
// dialog reads what this machine can say about how to be reached and writes a
// command line out of it.
let roomJoinInfo = null;

async function openRoomJoin() {
  const dlg = document.getElementById("roomjoin");
  try {
    roomJoinInfo = await api("/v1/rooms/join");
  } catch (e) { tellUser("could not work out the command", e.message); return; }

  // The ways in, best first. Ziti leads when it is configured because it is
  // the only one where neither machine has to be reachable from the other,
  // which is the case this federation was designed around.
  const ways = [];
  if (roomJoinInfo.service) {
    ways.push({ v: "ziti", label: "over the ziti service " + roomJoinInfo.service });
  }
  if (roomJoinInfo.share) {
    ways.push({ v: "share", label: "at this board's zrok address" });
  }
  ways.push({ v: "hub", label: "at an address on your own network" });
  const sel = document.getElementById("rj-reach");
  sel.innerHTML = ways.map(w =>
    `<option value="${esc(w.v)}">${esc(w.label)}</option>`).join("");
  // A configured service nobody is answering on is a command that fails, so it
  // is offered but not chosen. See the hint under the picker.
  sel.value = (roomJoinInfo.service && roomJoinInfo.service_running) ? "ziti" : ways[0].v;

  document.getElementById("rj-identity").value = "";
  document.getElementById("rj-name").value = "";
  document.getElementById("rj-board").value = "";
  document.getElementById("rj-beat").textContent =
    "It checks in every " + roomJoinInfo.heartbeat_seconds + "s, is called stale after " +
    ago(roomJoinInfo.stale_seconds) + " of silence, and stops being listed after " +
    ago(roomJoinInfo.forget_seconds) + ".";
  roomCommand();
  document.getElementById("rj-copy").onclick = e =>
    copyText(e.target, document.getElementById("rj-cmd").value);
  dlg.showModal();
}

// Builds the command from whatever is in the dialog, on every keystroke.
function roomCommand() {
  const info = roomJoinInfo || {};
  const way = document.getElementById("rj-reach").value;
  const hub = document.getElementById("rj-hub");
  const identity = document.getElementById("rj-identity-field");
  const hubField = document.getElementById("rj-hub-field");

  identity.style.display = way === "ziti" ? "" : "none";
  hubField.style.display = way === "ziti" ? "none" : "";

  // Refilled when the way in changes, and only then: retyping over somebody's
  // correction on every keystroke would make the field impossible to edit.
  if (hub.dataset.way !== way) {
    hub.dataset.way = way;
    hub.value = way === "share" ? (info.share || "") : (info.address || "");
  }
  document.getElementById("rj-hub-hint").textContent = way === "share"
    ? "The zrok share, which is a public link. Anyone who can open it can post to it, and this " +
      "board has no login unless you put one in front of it."
    : "Only works where that machine can already reach this one. This daemon listens on " +
      (info.local || "this machine") + ", so a loopback address here reports nowhere.";
  document.getElementById("rj-reach-hint").textContent = way !== "ziti"
    ? "The room posts to this URL on a timer. It has to be reachable from over there."
    : (info.service_running
      ? "The board is being answered on that service now. Neither machine has to be reachable " +
        "from the other, because both dial the network."
      : "That service is configured but this board is NOT being answered on it. Start the " +
        "OpenZiti overlay in settings first, or the room retries against nothing.");

  const parts = ["atrium", "room"];
  if (way === "ziti") {
    parts.push("--service", quoteArg(info.service || ""));
    parts.push("--identity", quoteArg(document.getElementById("rj-identity").value.trim() ||
      "/path/to/identity.json"));
  } else {
    parts.push("--hub", quoteArg(hub.value.trim() || "http://this-board:7778"));
  }
  const name = document.getElementById("rj-name").value.trim();
  // Left out when empty rather than sent blank. The room defaults to its own
  // hostname, and an empty --name is refused by the hub.
  if (name) parts.push("--name", quoteArg(name));
  const board = document.getElementById("rj-board").value.trim();
  if (board) parts.push("--board", quoteArg(board));
  document.getElementById("rj-cmd").value = parts.join(" ");
}

// Quotes a value only when it needs it, because a command full of quotes reads
// as a template rather than as something to run. Double quotes work in both a
// POSIX shell and PowerShell, which is what the two ends of this can be.
function quoteArg(v) {
  return /[\s"']/.test(v) ? '"' + v.replace(/"/g, '\\"') + '"' : v;
}

// A timestamp as something short enough to sit in a chip.
function shortTime(iso) {
  if (!iso) return "never";
  const t = Date.parse(iso);
  if (isNaN(t)) return iso;
  return ago(Math.max(0, Math.round((Date.now() - t) / 1000)));
}

async function renderRunners() {
  splitRunners();
  // Not awaited. A room list that is slow, or a daemon too old to have the
  // endpoint, must not hold up the rest of this page.
  renderRooms();
  renderDispatch();
  try { allHarnesses = (await api("/v1/harnesses")).harnesses || []; } catch (e) { return; }
  // Before the list is drawn: the claude row carries the count.
  await loadHooks();

  const on = allHarnesses.filter(h => h.enabled);
  document.getElementById("launchers").innerHTML = on.length
    ? `<div class="toolbar">` + on.map(h =>
        `<button class="go big" onclick="openLaunch('${esc(h.id)}')">start a ${esc(h.label)}</button>`
      ).join("") + `</div>`
    : `<div class="panel"><div class="empty">
        nothing is enabled. turn a runner on below, or add one.
      </div></div>`;

  // Whether the command exists is the difference between a runner that works
  // and one that fails on first use looking like atrium is broken. The daemon
  // resolves it the same way launching does, so what this shows is what will
  // actually run.
  document.getElementById("harness-list").innerHTML = `<div class="panel">` + allHarnesses.map(h => `
    <div class="row line">
      <span class="chip ${h.enabled ? "accent" : ""}">${h.enabled ? "on" : "off"}</span>
      <span class="tool">${esc(h.label)}</span>
      <code class="grow ell" title="${esc([h.cmd].concat(h.args || []).join(" "))}">${
        esc([h.cmd].concat(h.args || []).join(" "))}</code>
      ${h.found
        ? `<span class="by found" title="${esc(h.found)}">on PATH</span>`
        : `<span class="by missing" title="${esc(h.cmd)} is not on the daemon's PATH, so starting this would fail">not installed</span>`}
      <span class="by">${esc(h.launch_mode)}</span>
      ${hooksChip(h)}
      <button ${h.found ? "" : "disabled title='its command is not on PATH'"}
        onclick="toggleHarness('${esc(h.id)}')">${h.enabled ? "disable" : "enable"}</button>
      <button onclick="editHarness('${esc(h.id)}')">edit</button>
    </div>`).join("") + `</div>`;

  renderDiscovered();
  renderFixtures();
  renderSources();
  renderRecognisers();
  renderActions();
  // Only paints when the dialog is open. Nothing to draw otherwise.
  if (document.getElementById("hooks").open) renderHooks();
}

// ── everything that has ever run here ───────────────────
//
// Paged from the start rather than after the first machine that has been
// running for a year finds out the hard way. Nothing prunes this unless
// somebody turns pruning on, so it grows forever by design.

let historyShown = 0;
const HISTORY_PAGE = 100;

async function renderHistory(more) {
  const host = document.getElementById("history-list");
  if (!host) return;
  if (!more) historyShown = 0;

  const q = document.getElementById("h-q").value.trim();
  const recap = document.getElementById("h-recap").value;
  let res;
  try {
    res = await api(`/v1/history?limit=${HISTORY_PAGE}&offset=${historyShown}` +
      `&recap=${encodeURIComponent(recap)}&q=${encodeURIComponent(q)}`);
  } catch (e) { return; }

  const rows = res.tasks || [];
  historyShown += rows.length;
  document.getElementById("h-count").textContent = res.total
    ? `${historyShown} of ${res.total}`
    : "nothing yet";
  document.getElementById("h-more").hidden = historyShown >= (res.total || 0);

  const html = rows.map(historyRow).join("");
  if (more) {
    host.querySelector(".panel").insertAdjacentHTML("beforeend", html);
    return;
  }
  setHTML(host, rows.length
    ? `<div class="panel">` + html + `</div>`
    : `<div class="panel"><div class="empty">nothing matches.</div></div>`);
}

function historyRow(t) {
  // Archived is a fact about the card, not a judgement, so it is a quiet mark
  // rather than a status. A card can be archived and done, or archived and
  // dead, and the status column already says which.
  const gone = t.archived_at
    ? `<span class="by" title="off the board since ${esc(t.archived_at)}">archived</span>` : "";
  return `<div class="row line" onclick="openTask('${t.id}')" style="cursor:pointer">
    ${runnerMark(t.runner)}
    <span class="tool">${esc(t.display_title)}</span>
    <span class="grow ell" title="${esc(t.recap || t.why || t.worktree || "")}">${
      esc(t.recap || t.why || t.worktree || "")}</span>
    ${originChip(t)}
    ${t.recap
      ? `<span class="chip recap" title="${esc(t.recap)}">recap</span>`
      : `<span class="by" title="no account of what this session did">&mdash;</span>`}
    <span class="chip" title="${esc(t.status)}">${esc(statusLabel(t.status))}</span>
    ${gone}
    <span class="by" title="${esc(t.created_at)}">${esc(firstSeen(t.created_at))}</span>
  </div>`;
}

function moreHistory() { renderHistory(true); }

// Typing searches, with a pause, because every keystroke is a query against a
// table that only grows.
let historyTimer = null;
["h-q", "h-recap"].forEach(id => {
  const el = document.getElementById(id);
  if (!el) return;
  el.addEventListener("input", () => {
    clearTimeout(historyTimer);
    historyTimer = setTimeout(() => renderHistory(false), 220);
  });
  el.addEventListener("change", () => renderHistory(false));
});

// ── the actions editor ──────────────────────────────────

async function renderActions() {
  const host = document.getElementById("action-list");
  if (!host) return;
  await loadActions();

  if (!allActions.length) {
    setHTML(host, `<div class="panel"><div class="empty">
      no actions. one is a named prompt you can send to any card without typing it again.
    </div></div>`);
    return;
  }

  setHTML(host, `<div class="panel">` + allActions.map(a => `
    <div class="row line">
      <span class="chip ${a.enabled ? "accent" : ""}">${a.enabled ? "on" : "off"}</span>
      <span class="tool">${esc(a.label)}</span>
      <code class="grow ell" title="${esc(a.prompt)}">${esc(a.prompt)}</code>
      ${a.after === "exit" ? `<span class="by" title="asks the runner to quit afterwards"
        >and exit</span>` : ""}
      ${a.tag ? `<span class="chip tag" style="--ghue:${groupHue(a.tag)}">${esc(a.tag)}</span>` : ""}
      ${a.runner ? `<span class="by">${esc(a.runner)}</span>` : ""}
      <button data-edit="${esc(a.id)}">edit</button>
    </div>`).join("") + `</div>`);
  host.querySelectorAll("button[data-edit]").forEach(b => {
    b.onclick = () => editAction(b.dataset.edit);
  });
}

function editAction(id) {
  const a = allActions.find(x => x.id === id) || {
    id: "", label: "", prompt: "", after: "keep", tag: "", runner: "",
    enabled: true, sort: allActions.length * 10
  };
  const dlg = document.getElementById("action");
  dlg.dataset.editing = a.id;
  document.getElementById("a-heading").textContent = a.id ? "action" : "new action";

  document.getElementById("a-label").value = a.label || "";
  document.getElementById("a-prompt").value = a.prompt || "";
  document.getElementById("a-after").value = a.after || "keep";
  document.getElementById("a-tag").value = a.tag || "";
  document.getElementById("a-enabled").checked = !!a.enabled;
  document.getElementById("a-delete").hidden = !a.id;

  // Every runner, not only the enabled ones: an action can outlive a runner
  // being switched off and should still say which one it was for.
  document.getElementById("a-runner").innerHTML =
    `<option value="">any runner</option>` + (allHarnesses || []).map(h =>
      `<option value="${esc(h.id)}"${h.id === a.runner ? " selected" : ""}
        >${esc(h.label)}</option>`).join("");

  dlg.showModal();
}

async function saveAction() {
  const dlg = document.getElementById("action");
  const id = dlg.dataset.editing || "new";
  const body = {
    id: dlg.dataset.editing || "",
    label: document.getElementById("a-label").value.trim(),
    prompt: document.getElementById("a-prompt").value.trim(),
    after: document.getElementById("a-after").value,
    tag: document.getElementById("a-tag").value.trim(),
    runner: document.getElementById("a-runner").value,
    enabled: document.getElementById("a-enabled").checked
  };
  try {
    await api("/v1/actions/" + encodeURIComponent(id), {
      method: "PUT", headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body)
    });
  } catch (e) { tellUser("could not save it", e.message); return; }
  dlg.close();
  renderActions();
}

async function deleteAction() {
  const id = document.getElementById("action").dataset.editing;
  if (!id) return;
  if (!await confirmUser("delete this action?",
    "It stops being offered on any card. Nothing that has already been sent is affected.",
    "delete it")) return;
  try {
    await api("/v1/actions/" + encodeURIComponent(id), { method: "DELETE" });
  } catch (e) { tellUser("could not delete it", e.message); return; }
  document.getElementById("action").close();
  renderActions();
}

// ── sources ─────────────────────────────────────────────
//
// Commands atrium runs on a timer, whose stdout becomes cards in the inbox.
// Rows in a table, like runners and fixtures, because that is what they are.
//
// The row is the whole reporting surface: when it last ran, what it found, and
// what it said when it broke. A source that has failed three times running is
// switched off with the reason still attached, and the row says so rather than
// looking like one that was never turned on.

let allSources = [];

async function renderSources() {
  const host = document.getElementById("source-list");
  if (!host) return;
  try { allSources = (await api("/v1/sources")).sources || []; } catch (e) { return; }

  if (!allSources.length) {
    setHTML(host, `<div class="panel"><div class="empty">
      nothing is looking for work. a source is a command that prints intake items,
      and scripts/sources has working examples.
    </div></div>`);
    return;
  }

  setHTML(host, `<div class="panel">` + allSources.map(s => `
    <div class="row line">
      <span class="chip ${s.enabled ? "accent" : s.last_error ? "warn" : ""}"
        >${s.enabled ? "on" : s.last_error ? "off" : "off"}</span>
      <span class="tool">${esc(s.label || s.id)}</span>
      <code class="grow ell" title="${esc([s.cmd].concat(s.args || []).join(" "))}">${
        esc([s.cmd].concat(s.args || []).join(" "))}</code>
      <span class="by" title="how often it runs">${esc(everyLabel(s.interval_secs))}</span>
      ${sourceStateChip(s)}
      <button onclick="editSource('${esc(s.id)}')">edit</button>
    </div>`).join("") + `</div>`);
}

// How a source is doing, as one chip.
//
// A failing source and a source nobody turned on are both "off", and they are
// not the same thing at all. The reason is what tells them apart, so it is on
// the chip rather than behind the edit dialog.
function sourceStateChip(s) {
  if (s.last_error) {
    const off = !s.enabled
      ? "switched off after " + s.failures + " failures in a row. "
      : "failed " + s.failures + " time(s) in a row. ";
    return `<span class="chip warn" title="${esc(off + s.last_error)}">failing</span>`;
  }
  if (!s.last_run_at) {
    return `<span class="by" title="it has not run yet">never run</span>`;
  }
  return `<span class="by" title="${esc(s.last_run_at)}">found ${s.last_count} last time</span>`;
}

// An interval as something readable. Seconds are what the field takes, because
// that is what the row stores, and nobody reads 900 as a quarter of an hour.
function everyLabel(secs) {
  secs = Number(secs) || 0;
  if (secs % 3600 === 0 && secs >= 3600) return "every " + (secs / 3600) + "h";
  if (secs % 60 === 0 && secs >= 60) return "every " + (secs / 60) + "m";
  return "every " + secs + "s";
}

function editSource(id) {
  const s = allSources.find(x => x.id === id) || {
    id: "", label: "", cmd: "", args: [], cwd: "",
    // Off by default. A source is a command somebody just wrote, and the first
    // thing to do with one is run it by hand and read what it printed.
    interval_secs: 900, enabled: false
  };
  const dlg = document.getElementById("source");
  dlg.dataset.editing = s.id;
  document.getElementById("s-heading").textContent = s.id ? "source" : "new source";

  const idBox = document.getElementById("s-id");
  idBox.value = s.id || "";
  // The id is the key, so changing it would make a second source rather than
  // renaming one. Editable exactly once.
  idBox.disabled = !!s.id;

  document.getElementById("s-label").value = s.label || "";
  document.getElementById("s-cmd").value = s.cmd || "";
  document.getElementById("s-args").value = (s.args || []).join("\n");
  document.getElementById("s-cwd").value = s.cwd || "";
  document.getElementById("s-interval").value = s.interval_secs || 900;
  document.getElementById("s-enabled").checked = !!s.enabled;
  document.getElementById("s-delete").hidden = !s.id;
  document.getElementById("s-run").hidden = !s.id;

  showSourceStatus(s);
  dlg.showModal();
}

function showSourceStatus(s) {
  const field = document.getElementById("s-status-field");
  const box = document.getElementById("s-status");
  if (!s.id || !s.last_run_at) {
    field.hidden = true;
    return;
  }
  field.hidden = false;
  box.textContent = s.last_error
    ? "failed at " + s.last_run_at + " (" + s.failures + " in a row): " + s.last_error
    : "ran at " + s.last_run_at + " and found " + s.last_count + " item(s).";
}

async function saveSource() {
  const id = document.getElementById("s-id").value.trim();
  if (!id) { tellUser("atrium", "a source needs an id."); return; }
  const body = {
    id,
    label: document.getElementById("s-label").value.trim(),
    cmd: document.getElementById("s-cmd").value.trim(),
    // Split on lines and never on spaces. A path with a space in it is one
    // argument, and splitting on whitespace is how that stops being true.
    args: document.getElementById("s-args").value
      .split("\n").map(x => x.trim()).filter(Boolean),
    cwd: document.getElementById("s-cwd").value.trim(),
    interval_secs: Number(document.getElementById("s-interval").value) || 900,
    enabled: document.getElementById("s-enabled").checked
  };
  try {
    await api("/v1/sources/" + encodeURIComponent(id), {
      method: "PUT", headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body)
    });
  } catch (e) { tellUser("could not save it", e.message); return; }
  document.getElementById("source").close();
  renderSources();
}

// Run it now, without waiting for the interval.
//
// The reason this exists: a source is a script somebody just wrote, and the
// question they have is whether it works. Waiting fifteen minutes to find out
// that a path was wrong is how a feature goes unused.
async function runSourceNow() {
  const id = document.getElementById("source").dataset.editing;
  if (!id) return;
  const btn = document.getElementById("s-run");
  btn.disabled = true;
  btn.textContent = "running...";
  let res;
  try {
    res = await api("/v1/sources/" + encodeURIComponent(id) + "/run", { method: "POST" });
  } catch (e) {
    tellUser("could not run it", e.message);
    btn.disabled = false; btn.textContent = "run it now";
    return;
  }
  btn.disabled = false;
  btn.textContent = "run it now";
  await renderSources();
  const fresh = allSources.find(x => x.id === id);
  if (fresh) showSourceStatus(fresh);
  if (res.error) {
    tellUser("the source failed", res.error);
    return;
  }
  tellUser("atrium", res.created
    ? res.created + " new item(s) are in the inbox."
    : "it ran and there was nothing new.");
}

async function deleteSource() {
  const id = document.getElementById("source").dataset.editing;
  if (!id) return;
  // The cards it raised stay. They are work, and deleting the thing that found
  // it does not make it not work.
  if (!await confirmUser("delete this source?",
    "Nothing looks for this kind of work any more. The cards it already raised stay on the " +
    "board: they are work, and deleting the thing that found it does not make it not work.",
    "delete it")) return;
  try {
    await api("/v1/sources/" + encodeURIComponent(id), { method: "DELETE" });
  } catch (e) { tellUser("could not delete it", e.message); return; }
  document.getElementById("source").close();
  renderSources();
}

// ── recognisers ─────────────────────────────────────────
//
// Rows that say what a url means. Rows in a table, like runners, sources and
// fixtures, because that is what they are.
//
// The rule the whole feature rests on: ATRIUM LEARNS NOTHING ABOUT ANY SOURCE
// SYSTEM. A recogniser is a pattern and a set of templates, and whoever wrote
// the row did the understanding. Nothing here special-cases GitHub, and the
// table ships empty.

let allRecognisers = [];

async function renderRecognisers() {
  const host = document.getElementById("recogniser-list");
  if (!host) return;
  try { allRecognisers = (await api("/v1/recognisers")).recognisers || []; } catch (e) { return; }

  if (!allRecognisers.length) {
    setHTML(host, `<div class="panel"><div class="empty">
      nothing recognises a url yet. a recogniser is a pattern and a set of templates,
      and scripts/recognisers has working examples for github and bitbucket.
    </div></div>`);
    return;
  }

  // In the order they are asked, which is the order they are drawn. Somebody
  // debugging "why did the wrong row answer" is looking for exactly this.
  setHTML(host, `<div class="panel">` + allRecognisers.map(r => `
    <div class="row line">
      <span class="chip ${r.enabled ? "accent" : ""}">${r.enabled ? "on" : "off"}</span>
      <span class="by" title="asked in this order, lowest first">${esc(String(r.rank))}</span>
      <span class="tool">${esc(r.label || r.id)}</span>
      <code class="grow ell" title="${esc(r.pattern)}">${esc(r.pattern)}</code>
      ${r.fetch ? `<span class="chip" title="${esc("fetches more facts with: " + r.fetch)}">fetch</span>` : ""}
      ${r.last_error ? `<span class="chip warn" title="${
        esc("the fetch failed " + r.failures + " time(s) in a row: " + r.last_error)
      }">fetch failing</span>` : ""}
      <button class="editrec" data-id="${esc(r.id)}">edit</button>
    </div>`).join("") + `</div>`);

  // THE ID COMES BACK THROUGH THE DOM, not through an inline handler. An id is
  // operator-typed free text, and HTML escaping does nothing about an
  // apostrophe inside a JavaScript string literal: a row called `it's mine`
  // would close the argument and the button would throw instead of opening.
  host.querySelectorAll(".editrec").forEach(b =>
    b.onclick = () => editRecogniser(b.dataset.id));
}

function editRecogniser(id) {
  const r = allRecognisers.find(x => x.id === id) || {
    id: "", label: "", pattern: "", enabled: false,
    // Below whatever is already there, so a new row cannot silently swallow
    // urls a more specific one was answering.
    rank: 100,
    kind: "", title: "", tags: "", cwd: "", prompt: "", branch: "", window: "", theme: "",
    fetch: "", fetch_args: [], fetch_cwd: ""
  };
  const dlg = document.getElementById("recogniser");
  dlg.dataset.editing = r.id;
  document.getElementById("r-heading").textContent = r.id ? "recogniser" : "new recogniser";

  const idBox = document.getElementById("r-id");
  idBox.value = r.id || "";
  // The id is the key, so changing it would make a second row rather than
  // renaming one. Editable exactly once.
  idBox.disabled = !!r.id;

  document.getElementById("r-label").value = r.label || "";
  document.getElementById("r-pattern").value = r.pattern || "";
  document.getElementById("r-rank").value = r.rank == null ? 100 : r.rank;
  document.getElementById("r-cwd").value = r.cwd || "";
  document.getElementById("r-title").value = r.title || "";
  document.getElementById("r-prompt").value = r.prompt || "";
  document.getElementById("r-tags").value = r.tags || "";
  document.getElementById("r-branch").value = r.branch || "";
  document.getElementById("r-window").value = r.window || "";
  document.getElementById("r-theme").value = r.theme || "";
  document.getElementById("r-kind").value = r.kind || "";
  document.getElementById("r-fetch").value = r.fetch || "";
  document.getElementById("r-fetch-args").value = (r.fetch_args || []).join("\n");
  document.getElementById("r-fetch-cwd").value = r.fetch_cwd || "";
  document.getElementById("r-enabled").checked = !!r.enabled;
  document.getElementById("r-delete").hidden = !r.id;
  document.getElementById("r-try").value = "";
  document.getElementById("r-try-out").hidden = true;

  const field = document.getElementById("r-status-field");
  const box = document.getElementById("r-status");
  if (r.id && (r.last_error || r.last_used_at)) {
    field.hidden = false;
    box.textContent = r.last_error
      ? "the fetch failed at " + r.last_used_at + " (" + r.failures + " in a row): " + r.last_error
      : "used at " + r.last_used_at + ", and the fetch worked.";
  } else {
    field.hidden = true;
  }
  dlg.showModal();
}

async function saveRecogniser() {
  const id = document.getElementById("r-id").value.trim();
  if (!id) { tellUser("atrium", "a recogniser needs an id."); return; }
  const body = {
    id,
    label: document.getElementById("r-label").value.trim(),
    pattern: document.getElementById("r-pattern").value.trim(),
    rank: Number(document.getElementById("r-rank").value) || 0,
    cwd: document.getElementById("r-cwd").value.trim(),
    title: document.getElementById("r-title").value.trim(),
    // Not trimmed on the right the way the others are: a first instruction is
    // prose and its own line breaks are the author's.
    prompt: document.getElementById("r-prompt").value,
    tags: document.getElementById("r-tags").value.trim(),
    branch: document.getElementById("r-branch").value.trim(),
    window: document.getElementById("r-window").value.trim(),
    theme: document.getElementById("r-theme").value.trim(),
    kind: document.getElementById("r-kind").value.trim(),
    fetch: document.getElementById("r-fetch").value.trim(),
    // Split on lines and never on spaces, the same rule a source's arguments
    // follow. A path with a space in it is one line and stays one argument.
    fetch_args: document.getElementById("r-fetch-args").value
      .split("\n").map(x => x.trim()).filter(Boolean),
    fetch_cwd: document.getElementById("r-fetch-cwd").value.trim(),
    enabled: document.getElementById("r-enabled").checked
  };
  try {
    await api("/v1/recognisers/" + encodeURIComponent(id), {
      method: "PUT", headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body)
    });
  } catch (e) {
    // A pattern that does not compile comes back here, naming the position in
    // the expression. That is the whole reason it is checked on save rather
    // than at the moment somebody pastes a url.
    tellUser("could not save it", e.message);
    return;
  }
  document.getElementById("recogniser").close();
  renderRecognisers();
}

async function deleteRecogniser() {
  const id = document.getElementById("recogniser").dataset.editing;
  if (!id) return;
  if (!await confirmUser("delete this recogniser?",
    "Urls of this shape stop filling a dialog in. The cards it already filled stay: they are " +
    "work, and deleting the thing that recognised the link does not make it not work.",
    "delete it")) return;
  try {
    await api("/v1/recognisers/" + encodeURIComponent(id), { method: "DELETE" });
  } catch (e) { tellUser("could not delete it", e.message); return; }
  document.getElementById("recogniser").close();
  renderRecognisers();
}

// Paste, look, adjust a template, paste again. That is the loop this feature is
// used in, and a table with no way to try one is a table nobody finishes
// writing.
//
// It asks the WHOLE table as saved, not the row on screen, and names the row
// that answered. "The generic row above swallowed my url" is the mistake this
// makes visible.
async function tryRecogniser() {
  const url = document.getElementById("r-try").value.trim();
  const out = document.getElementById("r-try-out");
  if (!url) return;
  out.hidden = false;
  out.textContent = "asking...";
  let got;
  try {
    got = await api("/v1/recognise", {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ url })
    });
  } catch (e) {
    out.textContent = e.message;
    return;
  }
  const lines = [got.recogniser + " answered, as " + (got.label || got.recogniser)];
  const say = (k, v) => { if (v) lines.push("  " + k + ": " + v); };
  say("directory", got.cwd + (got.cwd_exists ? "" : "   (not here)"));
  say("title", got.title);
  say("tags", (got.tags || []).join(", "));
  say("repo", [got.org, got.repo].filter(Boolean).join("/"));
  say("host", got.host);
  say("branch", got.branch);
  say("window", got.window);
  say("kind", got.kind);
  say("prompt", got.prompt);
  if (got.fetch_error) lines.push("\nthe fetch failed: " + got.fetch_error);
  if (got.problem) lines.push("\n" + got.problem);
  out.textContent = lines.join("\n");
}

