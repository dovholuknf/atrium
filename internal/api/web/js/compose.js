// ── typing while atrium is typing ───────────────────────
//
// For the whole of a new context the room drops what the board types into the card's terminal, and says
// `in-refused`. Dropping it silently lost keystrokes, so the first refused key opens this box over the terminal,
// seeded with that key. Enter queues the text on the card as an ordinary message, which the room holds for the
// cycle and types, submitted once, when it ends. Escape discards it.
//
// THE WAY OUT IS ALWAYS HERE: "let me type" stops the cycle the way dismissing its chip does (DELETE
// /v1/tasks/{id}/new-context), which also sends what was queued and gives the terminal back at once. It does not
// wait for any step, so a wedged cycle cannot lock the operator out. See internal/daemon/newcontext.go.

// The last printable thing typed and when, so the key the room refused can seed the box.
let composeLast = { d: "", at: 0 };

function composeNoteTyped(d) {
  if (typeof d === "string" && d.length && !/[\x00-\x1f\x7f]/.test(d)) composeLast = { d, at: Date.now() };
}

// What the board has typed into the card's prompt since the last Enter, so a cycle that starts under it can keep the line.
let composeLine = "";
// The board sees only what it sent, so an arrow key or a mouse report leaves this alone and a part-written line is a guess.
function composeLineFeed(d, pasted) {
  if (typeof d !== "string" || !d.length) return;
  if (pasted) { composeLine += d; return; }
  if (d === "\x7f") { composeLine = composeLine.slice(0, -1); return; }
  if (d === "\r" || d === "\x03" || d === "\x15") { composeLine = ""; return; }
  if (!/[\x00-\x1f\x7f]/.test(d)) composeLine += d;
}

// When each card's whole row last arrived by the stream, so a list read that began before it cannot take the hold away.
const composeSeenAt = new Map();
// The open card's cycle, from a list read that began at `startedAt`. A newer stream row wins.
function composeFromList(row, startedAt) {
  if (!row || typeof termTask === "undefined" || !termTask) return;
  if ((composeSeenAt.get(row.id) || 0) > startedAt) return;
  termTask.new_context = row.new_context;
}

function composeOpen() { const b = document.getElementById("t-ctxbox"); return !!b && !b.hidden; }

// The room refused typing for the card on screen. A shell terminal is never held, so this is for a card only.
function composeRefused() {
  const box = document.getElementById("t-ctxbox");
  const ta = document.getElementById("t-ctxbox-in");
  if (!box || !ta || !termTask || termKind === "shell") return false;
  if (composeOpen()) return true;
  composeWire();
  box.dataset.card = termTask.id;
  box.hidden = false;
  ta.value = Date.now() - composeLast.at < 2000 ? composeLast.d : "";
  composeLast = { d: "", at: 0 };
  ta.focus();
  ta.setSelectionRange(ta.value.length, ta.value.length);
  return true;
}

function composeClose() {
  const box = document.getElementById("t-ctxbox");
  const ta = document.getElementById("t-ctxbox-in");
  if (box) { box.hidden = true; delete box.dataset.card; }
  if (ta) ta.value = "";
  if (term) term.focus();
}

// Queues the text on the card. If the cycle has ended by now the room types it at once, which is what was wanted.
async function composeSend() {
  const box = document.getElementById("t-ctxbox");
  const ta = document.getElementById("t-ctxbox-in");
  const id = box && box.dataset.card;
  const text = ta ? ta.value.replace(/\s+$/, "") : "";
  if (!id || !text) { composeClose(); return; }
  try {
    await api("/v1/tasks/" + encodeURIComponent(id) + "/message", {
      method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ text }),
    });
  } catch (e) {
    // Left in the box: losing what was typed is the one outcome this exists to prevent.
    toast("could not queue it", e.message);
    return;
  }
  composeClose();
  toast("queued", "it is typed into the card when the new context is ready");
  if (typeof refresh === "function") refresh();
}

// Stops the cycle, which sends whatever is queued, then types the box's own text straight into the terminal.
async function composeFree() {
  const box = document.getElementById("t-ctxbox");
  const ta = document.getElementById("t-ctxbox-in");
  const id = box && box.dataset.card;
  const text = ta ? ta.value : "";
  if (!id) { composeClose(); return; }
  try {
    await api("/v1/tasks/" + encodeURIComponent(id) + "/new-context", { method: "DELETE" });
  } catch (e) {
    toast("could not stop the new context", e.message);
    return;
  }
  composeClose();
  // Not the box's text through the message queue: the line is free now, so it goes where it was aimed.
  if (text && typeof sendInput === "function") sendInput(text);
  if (typeof refresh === "function") refresh();
}

// The do-not-enter sign over the terminal for as long as the room refuses its keys: from the moment the cycle finds
// the input line clear (`held`). The idle parking's capture and a failed cycle hold nothing, so they draw none.
function composeHeld(n) { return !!n && n.held !== false && n.step !== "failed" && n.step !== "capture"; }

function composeNoEntry(n) {
  const el = document.getElementById("t-nodev");
  if (!el) return;
  const held = composeHeld(n);
  el.hidden = !held;
  if (!held) return;
  const say = document.getElementById("t-nodev-say");
  say.textContent = "step " + n.n + "/" + n.of + ": " + (n.step === "wake" ? "waking it to read the handoff" : n.label);
  say.dataset.tip = say.textContent;
  say.setAttribute("aria-label", say.textContent);
}

// The bar under the terminal while the card is in a cycle: what step it is on and the button that ends it.
function composeBar(task) {
  const bar = document.getElementById("t-ncbar");
  if (!bar) return;
  const n = task && task.new_context;
  composeNoEntry(n);
  if (!n || n.step === "failed") { bar.hidden = true; return; }
  const q = Number(n.queued) || 0;
  document.getElementById("t-ncbar-say").textContent = "new context " + n.n + "/" + n.of + ": " + n.label +
    (q ? " · " + q + " message" + (q === 1 ? "" : "s") + " queued" : "");
  bar.hidden = false;
}

// THE BAR AND THE BOX FOLLOW THE CARD'S OWN TASK EVENT, not the next list paint. The bar was redrawn only by the
// terminals list pass, which a popped-out or card-URL window never runs and which a text selection withholds, so a
// cycle that ran to its end left "1/3: waiting for atrium ready" and "let me type" on screen. Called with every whole
// row the stream or a read brings. A box left open by a cycle that has ended closes, and its text goes to the
// terminal's input line unsubmitted, the same as composeFree after its DELETE.
function composeFollow(row) {
  if (!row || !row.id || typeof termTask === "undefined" || !termTask || !sameCard(row.id, termTask.id)) return;
  const was = composeHeld(termTask.new_context);
  composeSeenAt.set(row.id, Date.now());
  termTask.new_context = row.new_context;
  composeBar(termTask);
  const n = row.new_context;
  if (!was && composeHeld(n) && composeLine && termKind !== "shell") {
    const seed = composeLine;
    composeLine = "";
    composeRefused();
    document.getElementById("t-ctxbox-in").value = seed;
  }
  if ((n && n.step !== "failed") || !composeOpen()) return;
  const ta = document.getElementById("t-ctxbox-in");
  const text = ta ? ta.value : "";
  composeClose();
  if (text && typeof sendInput === "function") sendInput(text);
}

async function composeStop() {
  if (!termTask) return;
  try {
    await api("/v1/tasks/" + encodeURIComponent(termTask.id) + "/new-context", { method: "DELETE" });
  } catch (e) {
    toast("could not stop the new context", e.message);
    return;
  }
  termTask.new_context = null;
  const bar = document.getElementById("t-ncbar");
  if (bar) bar.hidden = true;
  composeNoEntry(null);
  if (term) term.focus();
  if (typeof refresh === "function") refresh();
}

function composeWire() {
  const box = document.getElementById("t-ctxbox");
  const ta = document.getElementById("t-ctxbox-in");
  if (!box || !ta || box.dataset.wired) return;
  box.dataset.wired = "1";
  ta.addEventListener("keydown", e => {
    if (e.key === "Escape") { e.preventDefault(); composeClose(); return; }
    // Enter queues and Shift+Enter is a newline, as in the card's say box. Ctrl+Enter queues too.
    if (e.key === "Enter" && !e.shiftKey && !e.isComposing) { e.preventDefault(); composeSend(); }
  });
}

// Keys are dropped here while the new context holds typing, so a room that has not caught up cannot let them through.
// The first one opens the compose box seeded with it, the same as a key the room refused.
function composeGate(d, pasted) {
  if (typeof termTask === "undefined" || !termTask || termKind === "shell" || !composeHeld(termTask.new_context)) return false;
  if (!pasted) composeNoteTyped(d);
  composeRefused();
  return true;
}
