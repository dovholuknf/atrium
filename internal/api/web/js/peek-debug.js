// The debug section of the terminal's details drawer: the live typing gate for the attached card, and the two
// per-terminal debug switches (the gate line and the input lag log). Only the drawer on the shortcut strip carries
// it. The card hover and the card menu open the same details without it. See js/peek.js.
//
// The gate runs in the daemon whether or not anyone looks. This asks `GET /v1/tasks/{id}/typing` while the drawer
// is open and stops the moment it closes, so a closed drawer costs nothing. It polls because the daemon pushes no
// gate state and the keystroke path must do no extra work for it. See js/typing.js.

const DBG_POLL_MS = 500;
// The quiet the gate waits for after the last key. Mirrors `peerGateIdle` in internal/daemon/supervisor.go.
const DBG_QUIET_MS = 2000;

let dbgTimer = 0, dbgSeq = 0;

function dbgRows(s, held) {
  const rows = [
    ["chars on the line", String(s.count || 0) + (s.in_paste ? " (inside a paste)" : "")],
    ["last key", typingAgo(s.since_ms)],
    ["held for this card", held ? held.n + " from @" + held.from : "none"]
  ];
  // The same sentence the gate line says, so the two cannot drift. Only when something is held and blocked.
  const g = typingGateText(s, held);
  if (g.blocking) rows.push(["blocked", g.text]);
  if (s.unsure) rows.splice(1, 0, ["not sure", s.unsure]);
  return rows;
}

// "closed, opens in 2s" while the gate only waits out its quiet time, so a closed gate after typing does not look
// stuck. Any other closed reason is said as it is.
function dbgGateText(s) {
  if (s.open) return "gate open: " + (s.reason || "");
  const waiting = !s.count && !s.unsure && s.since_ms >= 0 && s.since_ms < DBG_QUIET_MS;
  if (waiting) return "gate closed, opens in " + Math.max(1, Math.ceil((DBG_QUIET_MS - s.since_ms) / 1000)) + "s";
  return "gate closed: " + (s.reason || "");
}

function paintDebug(s, err) {
  const gate = document.getElementById("t-dbg-gate");
  const rows = document.getElementById("t-dbg-rows");
  if (!gate || !rows) return;
  gate.classList.remove("open", "shut");
  if (err || !s) {
    gate.textContent = err ? "typing gate: " + err : "typing gate: no terminal attached";
    rows.innerHTML = "";
    return;
  }
  gate.classList.add(s.open ? "open" : "shut");
  gate.textContent = dbgGateText(s);
  const held = termTask ? typingHeld(termTask.id) : null;
  rows.innerHTML = dbgRows(s, held).map(r =>
    `<div class="peek-debug-row"><span>${esc(r[0])}</span><b>${esc(r[1])}</b></div>`).join("");
}

async function dbgPoll(seq) {
  clearTimeout(dbgTimer);
  dbgTimer = 0;
  const t = termTask;
  if (seq !== dbgSeq || !t) return;
  // A hidden tab asks nothing, as the gate line does. It picks up again when the timer next fires visible.
  if (!document.hidden) {
    const { s, err } = await typingRead(t);
    if (seq !== dbgSeq || termTask !== t) return;
    paintDebug(s, err);
  }
  dbgTimer = setTimeout(() => dbgPoll(seq), DBG_POLL_MS);
}

// The machine's answer for the input lag box, once per open, as opening the settings does: the box reflects what
// the hub and the room are doing and the pinned note shows when ATRIUM_DEBUG_INPUTLAG decides.
async function dbgSyncLag(seq) {
  let s = null;
  try { s = await api("/v1/settings"); } catch (e) { return; }
  if (seq === dbgSeq) syncInputLag(s);
}

// The drawer opened or closed, or followed the terminal to another card.
function dbgDrawer(open) {
  const seq = ++dbgSeq;
  clearTimeout(dbgTimer);
  dbgTimer = 0;
  const box = document.getElementById("t-drawer-debug");
  if (box) box.hidden = !open;
  if (!open) return;
  const lag = document.getElementById("s-inputlag");
  if (lag) lag.checked = lagOn;
  const typ = document.getElementById("s-typing");
  if (typ) typ.checked = typingOn;
  dbgSyncLag(seq);
  dbgPoll(seq);
}

// A click anywhere outside the drawer closes it, as the hover and menu popovers do, and Escape too. The caret and the
// strip toggle it themselves. A click in the terminal closes it and still focuses the terminal, since nothing here
// cancels the event. Escape is taken only when it is aimed at the terminal or the drawer, and is swallowed so it
// does not also reach the program in the terminal.
document.addEventListener("pointerdown", e => {
  if (typeof termDrawerOpen === "undefined" || !termDrawerOpen) return;
  const t = e.target;
  if (t && t.closest && t.closest("#t-drawer, .term-help")) return;
  toggleTermDrawer(false);
}, true);

document.addEventListener("keydown", e => {
  if (e.key !== "Escape" || typeof termDrawerOpen === "undefined" || !termDrawerOpen) return;
  const t = e.target;
  if (!(t && t.closest && t.closest("#t-screen, #t-drawer"))) return;
  e.stopPropagation();
  e.preventDefault();
  toggleTermDrawer(false);
}, true);
