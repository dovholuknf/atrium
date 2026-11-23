// A card's move to another room: the `move to room` row, its dialog and the toast after. The hub drives the move
// (internal/link/moveask.go), the card's room asks it (internal/api/cardmove.go), and this only starts it and follows it.

const roomMoveSteps = [
  ["frozen", "freeze", "from"], ["checked", "check", "from"], ["worktree", "carry the branch", "to"],
  ["parked", "park", "from"], ["launched", "start", "to"], ["moved", "hand over", "to"], ["done", "done", "to"]
];
const roomMoveHelp = {
  row: "Freezes this card, carries its conversation, brief and branch to the room you pick, and resumes it there " +
    "with the same model and settings. This card stays here as a pointer to the new one. Only an idle card with a " +
    "clean worktree moves.",
  dlg: "The hub drives it: freeze, check, carry the branch and park here, then start and hand over on the other " +
    "room. Every step before the hand over is undone if a later one fails.",
  wip: to => "Commits what is not committed as \"WIP: move to " + to + "\" on the card's branch, so it travels with " +
    "the branch. The repository's hooks do not run for that commit.",
  failed: "Every step done here was undone. The other room kept nothing."
};
let roomMoveRun = null;

const roomMoveHelpHTML = t => `<span class="help" tabindex="0" data-tip="${esc(t)}">?</span>`;
const roomMoveMark = k => `<span class="mv-mark ${k}">${
  k === "ok" ? "&#10003;" : k === "bad" ? "!" : k === "fail" ? "&times;" : ""}</span>`;

// The row, offered on a hub with another room, for a card that has not moved already.
function roomMoveMenuItem(t) {
  if (!t || !t.room || t.moved_to || typeof prMoveRooms !== "function" || prMoveRooms().length < 2) return null;
  const on = new Set(prMoveRooms().map(r => r.name));
  const count = name => (typeof lastTasks === "undefined" ? [] : lastTasks).filter(c => c.room === name).length;
  const sub = [{ head: "rooms" }].concat(prMoveRooms().filter(r => r.name !== t.room).map(r => ({
    label: r.name, tag: count(r.name) + " cards", act: () => roomMoveOpen(t, r.name)
  })));
  const off = (typeof hubInventory === "undefined" ? [] : hubInventory || [])
    .filter(r => r && r.transport !== "local" && !on.has(r.name)).map(r => ({ quiet: r.name + " is offline" }));
  return { label: "move to room", help: roomMoveHelp.row, sub: sub.concat(off) };
}

// The "on X" chip of a card that has moved.
function roomMoveChip(t) {
  if (!t || !t.moved_to) return "";
  const room = String(t.moved_to).split("~")[0];
  return `<span class="chip mv-to" data-tip="moved to ${esc(t.moved_to)}">on ${esc(room)}</span>`;
}

function roomMoveDialog() {
  let d = document.getElementById("roommove");
  if (d) return d;
  d = document.createElement("dialog");
  d.id = "roommove";
  d.className = "mv";
  d.addEventListener("cancel", e => { if (roomMoveRun && roomMoveRun.state === "running") e.preventDefault(); });
  document.body.appendChild(d);
  return d;
}

function roomMovePaint(t, to, body, foot) {
  const d = roomMoveDialog();
  setHTML(d, `<div class="dlg-head"><div class="grow"><div class="dlg-eyebrow">move to room</div>
      <h2>${esc(cardLead(t, "board").main)}${roomMoveHelpHTML(roomMoveHelp.dlg)}</h2></div></div>
    <div class="dlg-body">
      <div class="mv-route"><span>${esc(t.room)}</span><span class="arrow">&rarr;</span><b>${esc(to)}</b></div>
      ${body}
    </div>
    <div class="dlg-foot">${foot}</div>`);
  if (!d.open) d.showModal();
  return d;
}

// The confirm: what is known before anything moves. The driver checks again after the freeze, and its word wins.
async function roomMoveOpen(t, to) {
  const idle = t.status !== "running";
  let dirty = null, branch = "";
  try {
    const pv = await api("/v1/tasks/" + encodeURIComponent(t.id) + "/close");
    const a = ((pv && pv.asks) || []).find(x => x.path && !x.hub);
    dirty = a ? a.dirty || 0 : 0;
    branch = a ? a.branch || "" : "";
  } catch (e) {}
  const tree = dirty === null ? `<span class="val">not read</span>`
    : dirty ? `<span class="val warn">${dirty} file${dirty === 1 ? "" : "s"} not committed</span>`
      : `<span class="val">clean${branch ? ", " + esc(branch) : ""}</span>`;
  const d = roomMovePaint(t, to, `<ul class="mv-checks">
      <li>${roomMoveMark(idle ? "ok" : "bad")}idle<span class="val${idle ? "" : " warn"}">${
        idle ? "last turn " + esc(ago(cardIdleAge(t))) + " ago" : "mid-turn"}</span></li>
      <li>${roomMoveMark(dirty ? "bad" : dirty === null ? "" : "ok")}worktree${tree}</li>
      <li>${roomMoveMark("ok")}${esc(to)}<span class="val">online</span></li>
    </ul>
    ${dirty ? `<label class="mv-opt"><input type="checkbox" id="mv-wip">commit them as WIP and move${
      roomMoveHelpHTML(roomMoveHelp.wip(to))}</label>` : ""}`,
    `<button type="button" id="mv-cancel">cancel</button><button type="button" class="go" id="mv-go"${
      dirty ? " disabled" : ""}>move</button>`);
  const wip = d.querySelector("#mv-wip"), go = d.querySelector("#mv-go");
  if (wip) wip.onchange = () => { go.disabled = !wip.checked; };
  d.querySelector("#mv-cancel").onclick = () => d.close();
  go.onclick = () => roomMoveStart(t, to, !!(wip && wip.checked));
}

async function roomMoveStart(t, to, wip) {
  let st;
  try {
    st = await api("/v1/tasks/" + encodeURIComponent(t.id) + "/move", {
      method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ to, wip })
    });
  } catch (e) {
    roomMoveEnd(t, to, { state: "refused", error: e.message });
    return;
  }
  const run = roomMoveRun = { t, to, wip, id: st.move_id, state: "running", last: "", shown: true };
  roomMoveProgress(run, st);
  while (run === roomMoveRun && run.state === "running") {
    await new Promise(r => setTimeout(r, 1000));
    try {
      st = await api("/v1/tasks/" + encodeURIComponent(t.id) + "/move?move=" + encodeURIComponent(run.id));
    } catch (e) { continue; }
    roomMoveProgress(run, st);
  }
}

function roomMoveProgress(run, st) {
  if (st.step && st.step !== "undone") run.last = st.step;
  run.state = st.state || "running";
  run.newID = st.new_id || run.newID;
  if (run.state !== "running") { roomMoveEnd(run.t, run.to, st, run); return; }
  if (!run.shown) return;
  const undoable = !run.stopping && roomMoveSteps.findIndex(s => s[0] === run.last) < roomMoveSteps.findIndex(s => s[0] === "parked");
  const d = roomMovePaint(run.t, run.to, roomMoveList(run, -1), `<button type="button" class="left" id="mv-stop"${
    undoable ? "" : " disabled"}>${run.stopping ? "stopping…" : "stop and undo"}</button><button type="button" id="mv-hide">hide</button>`);
  d.querySelector("#mv-hide").onclick = () => { run.shown = false; d.close(); };
  d.querySelector("#mv-stop").onclick = () => roomMoveStop(run);
}

// Asks the hub to stop the move before the park. The driver undoes what it did, and the poll hears the end.
async function roomMoveStop(run) {
  run.stopping = true;
  roomMoveProgress(run, { state: run.state, step: run.last, new_id: run.newID });
  try {
    await api("/v1/tasks/" + encodeURIComponent(run.t.id) + "/move", {
      method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ undo: run.id })
    });
  } catch (e) {
    run.stopping = false;
    toast("could not stop the move", e.message);
  }
}

// The steps, marked done up to the last the driver saved, then the one under way or, with failAt, the one that failed.
function roomMoveList(run, failAt) {
  const at = roomMoveSteps.findIndex(s => s[0] === run.last);
  return `<ul class="mv-checks">${roomMoveSteps.map(([, label, where], i) => {
    let k = "", cls = "dim";
    if (i <= at) { k = "ok"; cls = ""; }
    else if (i === at + 1) { k = failAt >= 0 ? "fail" : "run"; cls = "now"; }
    return `<li class="${cls}">${roomMoveMark(k)}${esc(label)}<span class="val">${
      esc(where === "from" ? run.t.room : run.to)}</span></li>`;
  }).join("")}</ul>`;
}

function roomMoveEnd(t, to, st, run) {
  if (run && roomMoveRun === run) roomMoveRun = null;
  if (st.state === "done") {
    const d = document.getElementById("roommove");
    if (d && d.open) d.close();
    roomMoveToast(t, to, run && run.newID);
    if (typeof refresh === "function") refresh();
    return;
  }
  const lines = String(st.error || "").split("\n").map(s => s.trim()).filter(Boolean);
  const why = `<div class="mv-why">${lines.map(esc).join("<br>")}</div>`;
  const d = st.state === "refused" || !run
    ? roomMovePaint(t, to, `<p class="mv-say warn">Not moved. Nothing changed.</p>${why}`, roomMoveAgain())
    : roomMovePaint(t, to, `<p class="mv-say warn">Not moved. The card is back here and resumed.${
      roomMoveHelpHTML(roomMoveHelp.failed)}</p>${roomMoveList(run, 1)}${why}`, roomMoveAgain());
  d.querySelector("#mv-close").onclick = () => d.close();
  d.querySelector("#mv-again").onclick = () => roomMoveOpen(t, to);
}

const roomMoveAgain = () =>
  `<button type="button" id="mv-close">close</button><button type="button" class="go" id="mv-again">try again</button>`;

function roomMoveToast(t, to, newID) {
  document.querySelectorAll(".mv-toast").forEach(n => n.remove());
  const n = document.createElement("div");
  n.className = "mv-toast";
  setHTML(n, `<b>${esc(cardLead(t, "board").main)}</b> moved to ${esc(to)}. It resumed there on the same conversation.
    <div class="row"><button type="button" class="mv-stay">stay here</button>${
      newID ? `<button type="button" class="go mv-open">open it there</button>` : ""}</div>`);
  document.body.appendChild(n);
  n.querySelector(".mv-stay").onclick = () => n.remove();
  const open = n.querySelector(".mv-open");
  if (open) open.onclick = () => { n.remove(); landOnAlert(to + "~" + newID, "", ""); };
}
