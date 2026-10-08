// ── typing while atrium is typing ───────────────────────
//
// While a new context is typing into a card's terminal itself, the room drops what the board types and says
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

function composeOpen() { const b = document.getElementById("t-compose"); return !!b && !b.hidden; }

// The room refused typing for the card on screen. A shell terminal is never held, so this is for a card only.
function composeRefused() {
  const box = document.getElementById("t-compose");
  const ta = document.getElementById("t-compose-in");
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
  const box = document.getElementById("t-compose");
  const ta = document.getElementById("t-compose-in");
  if (box) { box.hidden = true; delete box.dataset.card; }
  if (ta) ta.value = "";
  if (term) term.focus();
}

// Queues the text on the card. If the cycle has ended by now the room types it at once, which is what was wanted.
async function composeSend() {
  const box = document.getElementById("t-compose");
  const ta = document.getElementById("t-compose-in");
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
  const box = document.getElementById("t-compose");
  const ta = document.getElementById("t-compose-in");
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

// The bar under the terminal while the card is in a cycle: what step it is on and the button that ends it.
function composeBar(task) {
  const bar = document.getElementById("t-ncbar");
  if (!bar) return;
  const n = task && task.new_context;
  if (!n || n.step === "failed") { bar.hidden = true; return; }
  const q = Number(n.queued) || 0;
  document.getElementById("t-ncbar-say").textContent = "new context " + n.n + "/" + n.of + ": " + n.label +
    (q ? " · " + q + " message" + (q === 1 ? "" : "s") + " queued" : "");
  bar.hidden = false;
}

async function composeStop() {
  if (!termTask) return;
  try {
    await api("/v1/tasks/" + encodeURIComponent(termTask.id) + "/new-context", { method: "DELETE" });
  } catch (e) {
    toast("could not stop the new context", e.message);
    return;
  }
  const bar = document.getElementById("t-ncbar");
  if (bar) bar.hidden = true;
  if (term) term.focus();
  if (typeof refresh === "function") refresh();
}

function composeWire() {
  const box = document.getElementById("t-compose");
  const ta = document.getElementById("t-compose-in");
  if (!box || !ta || box.dataset.wired) return;
  box.dataset.wired = "1";
  ta.addEventListener("keydown", e => {
    if (e.key === "Escape") { e.preventDefault(); composeClose(); return; }
    // Enter queues and Shift+Enter is a newline, as in the card's say box. Ctrl+Enter queues too.
    if (e.key === "Enter" && !e.shiftKey && !e.isComposing) { e.preventDefault(); composeSend(); }
  });
}
