// ── what atrium thinks is on the input line ─────────────
//
// A say to a card waits until the operator's input line is empty and quiet, and
// the line is atrium's MODEL of what was typed, built from the keystrokes the
// attach socket carries. When the model drifts from what is really on screen,
// a say sits held behind an empty prompt and nothing says why. This readout is
// how that gets seen: the line atrium thinks is there, its length, the time
// since the last keystroke, and whether the gate is open and why not. It is for
// the operator and an agent debugging the gate together.
//
// OFF UNLESS ASKED FOR, and when off it costs nothing: no poll, no element.
// Switched on from the terminal's details, under "debug" (see js/peek-debug.js), or in the console:
//
//     localStorage.setItem("atrium.debug.typing", "1")
//
// Polled while on, from `GET /v1/tasks/{id}/typing`, rather than pushed on the
// attach socket. The keystroke path is hot and the readout is a debug aid, so
// the daemon does nothing extra per key for it. See internal/daemon/typedline.go.

const TYPING_KEY = "atrium.debug.typing";
const TYPING_POLL_MS = 500;

let typingOn = false;
try { typingOn = localStorage.getItem(TYPING_KEY) === "1"; } catch (e) {}
let typingTimer = 0;

// The checkbox and the console both land here. Remembered per browser.
function toggleTypingReadout(on) {
  typingOn = !!on;
  try {
    if (typingOn) localStorage.setItem(TYPING_KEY, "1");
    else localStorage.removeItem(TYPING_KEY);
  } catch (e) {}
  const box = document.getElementById("s-typing");
  if (box) box.checked = typingOn;
  clearTimeout(typingTimer);
  typingTimer = 0;
  paintTyping(null, "");
  if (typingOn) pollTyping();
}

// One read of the gate for the attached card. The gate line and the details drawer's debug section both ask, so a
// read in flight or answered in the last 300ms is shared and the daemon is asked once.
let typingRead_ = null;
function typingRead(t) {
  const kind = termKind === "shell" ? "?kind=shell" : "";
  const key = t.id + kind;
  if (typingRead_ && typingRead_.key === key && Date.now() - typingRead_.at < 300) return typingRead_.p;
  const p = api("/v1/tasks/" + encodeURIComponent(t.id) + "/typing" + kind)
    .then(s => ({ s, err: "" }), e => ({ s: null, err: (e && e.message) || String(e) }));
  typingRead_ = { key, at: Date.now(), p };
  return p;
}

async function pollTyping() {
  clearTimeout(typingTimer);
  typingTimer = 0;
  if (!typingOn) return;
  const t = termTask;
  if (t && !document.hidden) {
    const { s, err } = await typingRead(t);
    // The pane may have moved to another card while this was in flight.
    if (typingOn && termTask === t) paintTyping(s, err);
  } else {
    paintTyping(null, "");
  }
  if (typingOn && !typingTimer) typingTimer = setTimeout(pollTyping, TYPING_POLL_MS);
}

// The line as one row of text: newlines and tabs drawn as marks, so a
// multi-line prompt still reads on the one line the readout has.
function typingLineText(line) {
  return String(line || "").replace(/\n/g, "⏎").replace(/\t/g, "⇥");
}

function typingAgo(ms) {
  if (ms == null || ms < 0) return "never";
  if (ms < 10000) return (ms / 1000).toFixed(1) + "s ago";
  if (ms < 120000) return Math.round(ms / 1000) + "s ago";
  return Math.round(ms / 60000) + "m ago";
}

// The messages held behind the operator's line, from the card's live activity, and who sent them.
function typingHeld(id) {
  const t = typeof peekCard === "function" ? peekCard(id) : null;
  const a = t && t.activity;
  if (!a || !a.held_peer || a.held_for !== "line") return null;
  return { n: Math.max(1, Number(a.held_count) || 1), from: String(a.held_peer).replace(/^@/, "") };
}

// The line under the terminal speaks only when a message is held behind the gate. It never copies the line
// being typed. A held message on a line that looks empty is the case the readout was built for, and only then
// does it quote what atrium thinks is there, cut short.
function typingBlockText(s, held) {
  let why;
  if (s.count > 0) why = s.count + " char" + (s.count === 1 ? "" : "s") + " on your line";
  else {
    const line = typingLineText(s.line).trim();
    why = line ? "line looks empty, atrium thinks “" + (line.length > 40 ? line.slice(0, 40) + "…" : line) + "”"
      : (s.reason || "line looks empty");
  }
  return held.n + (held.n === 1 ? " message" : " messages") + " from @" + held.from +
    (held.n === 1 ? " waits: " : " wait: ") + why;
}

function paintTyping(s, err) {
  const el = document.getElementById("t-typing");
  if (!el) return;
  el.classList.remove("open", "shut");
  const held = s && !err && termTask ? typingHeld(termTask.id) : null;
  if (!typingOn || (!err && !(held && !s.open))) { el.hidden = true; return; }
  el.hidden = false;
  if (err) {
    el.textContent = "typing gate: " + err;
    return;
  }
  el.classList.add("shut");
  el.textContent = typingBlockText(s, held);
}

// Another window of this browser switched it. The storage event reaches every other open window at once.
addEventListener("storage", e => {
  if (e.key !== null && e.key !== TYPING_KEY) return;
  let on = false;
  try { on = localStorage.getItem(TYPING_KEY) === "1"; } catch (err) {}
  if (on !== typingOn) toggleTypingReadout(on);
});

if (typingOn) pollTyping();
