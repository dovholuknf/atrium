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
// Switched on from settings ("show the typing gate readout") or in the console:
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

async function pollTyping() {
  clearTimeout(typingTimer);
  typingTimer = 0;
  if (!typingOn) return;
  const t = termTask;
  if (t && !document.hidden) {
    const kind = termKind === "shell" ? "?kind=shell" : "";
    let s = null, err = "";
    try {
      s = await api("/v1/tasks/" + encodeURIComponent(t.id) + "/typing" + kind);
    } catch (e) {
      err = (e && e.message) || String(e);
    }
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

function paintTyping(s, err) {
  const el = document.getElementById("t-typing");
  if (!el) return;
  el.hidden = !typingOn;
  if (!typingOn) return;
  el.classList.remove("open", "shut");
  if (err) {
    el.textContent = "typing gate: " + err;
    return;
  }
  if (!s) {
    el.textContent = "typing gate: no terminal attached";
    return;
  }
  el.classList.add(s.open ? "open" : "shut");
  el.textContent =
    "gate " + (s.open ? "open" : "closed") + ": " + (s.reason || "") +
    " · line “" + typingLineText(s.line) + "”" +
    " · " + (s.count || 0) + " char" + (s.count === 1 ? "" : "s") +
    (s.in_paste ? " · inside a paste" : "") +
    " · last key " + typingAgo(s.since_ms);
}

if (typingOn) pollTyping();
