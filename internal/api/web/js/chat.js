// ── the chat tab ─────────────────────────────────────────────────────────
//
// The phone's conversation view on the desktop board: the bubbles, the working line, the permission asks and the composer
// are the phone page itself (`/m/?chat=1#term=<id>`, in an iframe), not a second implementation. `m/chat-embed.css` hides
// the phone's own navigation under `?chat=1` and centres the column. The phone's behaviour is untouched, and it keeps its
// own event stream, so replies, activity and auto-scroll are live the way they are on a phone.
//
// THE LIST IS THE TERMINALS' LIST. `#term-list` is moved into this view while it is open and put back when it is not, so
// the grouping, the filter, the pins and the folds are the very ones the terminals tab has, with nothing copied. A click on
// a row selects it here rather than attaching it (the capture listener below), and the selection is marked on the row.
// The terminal button attaches the card and lands on the terminals tab, and the terminals tab has a chat button back.

const CHAT_KEY = "atrium.chat";
let chatId = "";
try { chatId = localStorage.getItem(CHAT_KEY) || ""; } catch (e) {}
let chatLent = false;

function chatCard(id) {
  const all = typeof lastTasks !== "undefined" ? lastTasks : [];
  return all.find(t => t.id === id) || null;
}

function chatMark() {
  const list = document.getElementById("term-list");
  if (!list || !chatLent) return;
  for (const row of list.querySelectorAll(".card.tab[data-id]")) {
    const on = !!chatId && row.dataset.id === chatId;
    if (row.classList.contains("on") !== on) row.classList.toggle("on", on);
  }
  requestAnimationFrame(placeTabBridge);
  const t = chatCard(chatId);
  const title = document.getElementById("chat-title");
  if (title) title.textContent = chatId ? (t ? (t.display_title || terminalLabel(t) || chatId) : chatId) : "pick a session";
}

function chatSelect(id) {
  chatId = id || "";
  try { if (chatId) localStorage.setItem(CHAT_KEY, chatId); } catch (e) {}
  const host = document.getElementById("chat-frame-host");
  const empty = document.getElementById("chat-empty");
  document.getElementById("chat-to-term").disabled = !chatId;
  if (empty) empty.hidden = !!chatId;
  // A fresh frame for each card: the phone page opens the card named in its address once, on load.
  const old = host.querySelector("iframe");
  if (old) old.remove();
  if (chatId) {
    const f = document.createElement("iframe");
    f.id = "chat-frame";
    f.setAttribute("aria-label", "conversation");
    f.dataset.card = chatId;
    f.src = "/m/?chat=1#term=" + encodeURIComponent(chatId);
    host.append(f);
  }
  chatMark();
}

// Called by switchView on every switch. Lends the list in, or puts it back where the terminals tab keeps it.
function chatBridges(into) {
  const first = document.getElementById("tab-bridge");
  if (!first) return;
  into.append(first, ...first.parentNode.querySelectorAll(".tabbridge.extra"));
}

function chatView(on) {
  const list = document.getElementById("term-list");
  const layout = document.getElementById("chat-layout");
  if (!list || !layout || on === chatLent) return;
  chatLent = on;
  if (on) {
    // The terminals' grip comes along, so the list is dragged here with the same handle and the same stored width.
    const grip = document.getElementById("term-grip");
    layout.prepend(list, grip);
    chatBridges(layout);
    layout.style.setProperty("--termw", clampTermW(termListW) + "px");
    list.classList.add("in-chat");
    chatSelect(chatId);
    renderTermList();
  } else {
    list.classList.remove("in-chat");
    const grip = document.getElementById("term-grip");
    document.getElementById("term-rail").after(list);
    list.after(grip);
    chatBridges(document.getElementById("term-layout"));
    const f = document.getElementById("chat-frame");
    if (f) f.remove();
    // Rows were marked for the chat; the terminals tab draws its own.
    renderTermList();
  }
}

function chatToTerminal() { if (chatId) attachTask(chatId); }

function chatFromTerm() {
  if (typeof termTask !== "undefined" && termTask) chatId = termTask.id;
  switchView("chat");
}

(function () {
  const list = document.getElementById("term-list");
  if (!list) return;
  list.addEventListener("click", e => {
    if (!chatLent) return;
    const row = e.target.closest(".card.tab[data-id]");
    if (!row || e.target.closest(".pin, button, a, .tkids-toggle")) return;
    e.stopPropagation();
    e.preventDefault();
    chatSelect(row.dataset.id);
  }, true);
  // The list is drawn again on every change: mark the selected row again each time.
  new MutationObserver(() => chatMark()).observe(list, { childList: true, subtree: true });
})();
