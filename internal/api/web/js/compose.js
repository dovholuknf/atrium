// ── the new context panel ───────────────────────────────
//
// For the whole of a new context the room drops what the board types into the card's terminal. One panel over the
// terminal shows the step and takes a message: Enter queues it on the card, which types it, submitted once, when the
// cycle ends. Pasting an image into it uploads the file and puts its path in the text.
//
// THE WAY OUT IS ALWAYS HERE: the x, Escape and "let me type" stop the cycle (DELETE /v1/tasks/{id}/new-context),
// which sends what was queued and gives the terminal back at once, whatever step it is on. A key typed into the
// terminal does the same and goes through. See internal/daemon/newcontext.go.

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
  if (composeOpen()) {
    if (!ta.value && Date.now() - composeLast.at < 2000) ta.value = composeLast.d;
    return true;
  }
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
  if (!id || !termTask || termTask.id !== id) { composeClose(); return; }
  if (!(await composeStop())) return;
  // Not the box's text through the message queue: the line is free now, so it goes where it was aimed.
  if (text && typeof sendInput === "function") sendInput(text);
}

// Whether the room refuses the card's keys: from the moment the cycle finds the input line clear (`held`). The idle parking's capture and a failed cycle hold nothing, so they draw none.
function composeHeld(n) { return !!n && n.held !== false && n.step !== "failed" && n.step !== "capture"; }

// The card whose panel was dismissed, so the next row of the same cycle does not open it again.
let composeDismissedFor = "";

// The panel opens with the hold and shows the step. It does not take focus, so the terminal stays typeable.
function composeNoEntry(n) {
  const el = document.getElementById("t-ctxbox");
  if (!el) return;
  const held = composeHeld(n);
  if (!held) {
    composeDismissedFor = "";
    const ta = document.getElementById("t-ctxbox-in");
    if (!el.hidden && ta && !ta.value) el.hidden = true;
    return;
  }
  if (!termTask || composeDismissedFor === termTask.id) return;
  composeWire();
  el.dataset.card = termTask.id;
  el.hidden = false;
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
    // Off the terminal's line too, or it is left there and typed a second time when the box is sent or freed.
    if (typeof send === "function") send({ t: "clear-line", n: [...seed].length });
  }
  if ((n && n.step !== "failed") || !composeOpen()) return;
  const ta = document.getElementById("t-ctxbox-in");
  const text = ta ? ta.value : "";
  composeClose();
  if (text && typeof sendInput === "function") sendInput(text);
}

async function composeStop() {
  if (!termTask) return false;
  try {
    await api("/v1/tasks/" + encodeURIComponent(termTask.id) + "/new-context", { method: "DELETE" });
  } catch (e) {
    toast("could not stop the new context", e.message);
    return false;
  }
  termTask.new_context = null;
  const bar = document.getElementById("t-ncbar");
  if (bar) bar.hidden = true;
  composeNoEntry(null);
  composeClose();
  if (typeof refresh === "function") refresh();
  return true;
}

// The x and Escape: the panel goes away and the hold with it, at any step. What was typed in it is dropped.
async function composeDismiss() {
  if (termTask) composeDismissedFor = termTask.id;
  const box = document.getElementById("t-ctxbox");
  if (box) box.hidden = true;
  if (!(await composeStop())) composeDismissedFor = "";
  composeClose();
}

// A key or paste typed into the terminal while it is held: the hold is released and the bytes go through.
let composeTyped = null;
async function composeRelease(d, pasted) {
  if (composeTyped) { composeTyped.d += d; composeTyped.pasted = composeTyped.pasted || !!pasted; return; }
  composeTyped = { d, pasted: !!pasted };
  composeDismissedFor = termTask.id;
  const ok = await composeStop();
  const typed = composeTyped;
  composeTyped = null;
  if (ok) { sendInput(typed.d, false, typed.pasted); return; }
  composeDismissedFor = "";
  composeNoteTyped(typed.d);
  composeRefused();
}

// Pasting a file into the panel goes up the way a paste into the terminal does, and its path lands in the text.
async function composePasteFiles(files) {
  const id = termTask && termTask.id;
  const ta = document.getElementById("t-ctxbox-in");
  if (!id || !ta) return;
  const form = new FormData();
  for (const f of files) form.append("file", f, f.name);
  let res;
  try {
    res = await api("/v1/tasks/" + encodeURIComponent(id) + "/files", { method: "POST", body: form });
  } catch (e) {
    toast("that did not go up", e.message);
    return;
  }
  const paths = (res.paths || []).join(" ");
  if (!paths) return;
  const pre = preambleOf(await pasteSettings());
  const at = ta.selectionStart == null ? ta.value.length : ta.selectionStart;
  const ins = pre + paths + " ";
  ta.value = ta.value.slice(0, at) + ins + ta.value.slice(ta.selectionEnd);
  ta.setSelectionRange(at + ins.length, at + ins.length);
}

function composeWire() {
  const box = document.getElementById("t-ctxbox");
  const ta = document.getElementById("t-ctxbox-in");
  if (!box || !ta || box.dataset.wired) return;
  box.dataset.wired = "1";
  ta.addEventListener("paste", e => {
    const dt = e.clipboardData;
    const files = [];
    for (const item of (dt && dt.items) || []) {
      const f = item.kind === "file" ? item.getAsFile() : null;
      if (f && f.size > 0) files.push(f);
    }
    if (!files.length || isGuest()) return;
    e.preventDefault();
    composePasteFiles(files);
  });
  box.addEventListener("keydown", e => {
    if (e.key === "Escape") { e.preventDefault(); composeDismiss(); }
  });
  ta.addEventListener("keydown", e => {
    // Enter queues and Shift+Enter is a newline, as in the card's say box. Ctrl+Enter queues too.
    if (e.key === "Enter" && !e.shiftKey && !e.isComposing) { e.preventDefault(); composeSend(); }
  });
}

// Keys are dropped here while the new context holds typing, so a room that has not caught up cannot let them through.
// The first one opens the compose box seeded with it, the same as a key the room refused.
function composeGate(d, pasted) {
  if (typeof termTask === "undefined" || !termTask || termKind === "shell" || !composeHeld(termTask.new_context)) return false;
  composeRelease(d, pasted);
  return true;
}
