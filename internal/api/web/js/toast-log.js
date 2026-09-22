// ── what the board has told you, kept ───────────────────────────────────────
//
// A toast is gone in a few seconds, which is right for a toast and wrong as
// the only copy. Everything worth interrupting you for is also worth finding
// afterwards: a share address, a path that failed to save, which card just
// asked for something. Missing one meant it never happened.
//
// A WRAPPER RATHER THAN AN EDIT TO `toasts.js`. That file carries two stray
// NUL bytes, which is why git diffs it as binary and why a patch cut without
// `--text` silently drops it (B2-51). Wrapping the function from here keeps
// this change out of a file that is awkward to review, and the seam is one
// line rather than a change scattered through the toast's own logic.
//
// LOAD ORDER MATTERS AND IS THE ONE FRAGILE PART. `toasts.js` defines `toast`
// as a function declaration, which is hoisted, so this file has to be loaded
// AFTER it to wrap the real one rather than a hoisted stub.

// How many to keep. Enough to cover a session's worth of missed alerts and
// small enough that the list is readable and localStorage stays trivial.
const TOASTLOG_MAX = 200;
const TOASTLOG_KEY = "atrium.toastlog";

// IN THIS BROWSER, not on the daemon. What you were told is a fact about this
// screen: two people on one board saw different things, and a card's own event
// log already carries anything that happened to the WORK. Putting this on the
// daemon would be inventing a second, worse event log that records who was
// looking.
function toastLog() {
  try { return JSON.parse(localStorage.getItem(TOASTLOG_KEY) || "[]"); }
  catch (e) { return []; }
}

function saveToastLog(list) {
  try { localStorage.setItem(TOASTLOG_KEY, JSON.stringify(list.slice(-TOASTLOG_MAX))); }
  catch (e) {}
}

// Records ONE line into the durable log, bumping a repeat rather than adding a
// row, then repaints the badge. This is the whole history-append, kept apart
// from showing anything so EVERY path can reach it: a toast records through the
// wrapper below, and a desktop notification, which the operating system draws
// and which is never a toast, records through `logNotification`.
//
// A REPEAT BUMPS THE LAST ENTRY rather than adding one, matching what the toast
// itself does: four identical messages are one thing that happened four times,
// and a list that says it four times is a list nobody scrolls.
function recordToLog(title, body, goTo, key, taskFor) {
  const list = toastLog();
  const last = list[list.length - 1];
  const sig = title + " " + (body || "");
  if (last && last.sig === sig) {
    last.n = (last.n || 1) + 1;
    last.at = Date.now();
  } else {
    // `key` rides along with the rest. It is the pending item a permission
    // toast points at, and without it a permission opened from here lands on
    // the right tab and leaves you to find the request.
    list.push({ sig, title, body: body || "", goTo: goTo || "", taskFor: taskFor || "",
      key: key || "", at: Date.now(), n: 1 });
  }
  saveToastLog(list);
  paintToastLogBadge();
}

// A DESKTOP notification lands in the log the same as a toast would have. It is
// shown by the operating system rather than as a toast, so the wrapper below
// never sees it, and before this the OS alert left no trace: the chime and the
// popup fired while the board was unfocused and nothing could be found
// afterward. `notify` calls this on that one path. The other paths all reach a
// toast, here or in a sibling window on the same origin sharing this
// localStorage, and are recorded by the wrapper.
function logNotification(title, body, goTo, key, taskFor) {
  recordToLog(title, body, goTo, key, taskFor);
}

// The real one, held before it is replaced.
const rawToast = toast;

// eslint-disable-next-line no-func-assign
toast = function (title, body, goTo, key, taskFor) {
  // Recorded BEFORE it is shown, so a toast that throws on its way to the
  // screen is still in the list. The list is the durable half and the toast is
  // the interruption.
  recordToLog(title, body, goTo, key, taskFor);
  return rawToast(title, body, goTo, key, taskFor);
};

// HOW MANY HAVE ARRIVED SINCE YOU LAST LOOKED, which is the number that says
// whether opening this is worth it. Stored rather than counted from the list,
// because the list is trimmed and a count that shrinks when old entries fall
// off would be a lie about what you missed.
function toastLogSeen() {
  return Number(localStorage.getItem(TOASTLOG_KEY + ".seen") || 0);
}

function paintToastLogBadge() {
  const list = toastLog();
  const seen = toastLogSeen();
  const unseen = list.filter(t => t.at > seen).length;
  const btn = document.getElementById("toastlog-open");
  if (!btn) return;
  btn.classList.toggle("has", unseen > 0);
  const n = btn.querySelector(".count");
  if (n) n.textContent = unseen > 99 ? "99+" : (unseen || "");
}

function openToastLog() {
  const list = toastLog().slice().reverse();
  const host = document.getElementById("toastlog-list");
  if (!host) return;
  const seen = toastLogSeen();
  setHTML(host, list.length
    ? list.map(t => {
        // What the copy button hands to the clipboard: the title, then the body
        // on a second line when there is one. The same shape that arrived as a
        // toast, so pasting into a bug report or a chat message reads as the
        // alert did rather than as two disconnected strings.
        const clip = t.title + (t.body ? "\n" + t.body : "");
        const arg = JSON.stringify(clip).replace(/'/g, "&#39;");
        return `<div class="tlrow${t.at > seen ? " fresh" : ""}"${
          t.taskFor ? ` data-task="${esc(t.taskFor)}"` : ""}${
          t.goTo ? ` data-goto="${esc(t.goTo)}"` : ""}${
          t.key ? ` data-key="${esc(t.key)}"` : ""}>
          <div class="tlwhen">${esc(shortWhen(t.at))}</div>
          <div class="tlwhat">
            <b>${esc(t.title)}${t.n > 1 ? ` <span class="tltimes">×${t.n}</span>` : ""}</b>
            ${t.body ? `<span>${esc(t.body)}</span>` : ""}
          </div>
          <button class="tlcopy" title="copy this entry"
            onclick='event.stopPropagation();copyText(this, ${arg})'>copy</button>
        </div>`;
      }).join("")
    : `<div class="empty">nothing yet. anything the board tells you turns up here.</div>`);

  // A ROW GOES WHERE THE TOAST WOULD HAVE GONE, and that is the whole
  // requirement: this list exists because a toast went past before it could be
  // clicked, so a row has to do what clicking it would have done.
  //
  // Written out rather than shared with `toasts.js`, which is the one thing
  // worth being uncomfortable about here. It is three lines and a `return`,
  // and the alternative is exporting the body of a click handler out of a file
  // that git treats as binary. If a fourth step is ever added to the toast's
  // click, it has to be added here too.
  //
  // Through `data-` attributes rather than inline handlers. A card id is safe
  // and a permission key is not necessarily, and HTML escaping is not
  // JavaScript escaping.
  host.querySelectorAll(".tlrow").forEach(r => {
    const task = r.dataset.task, go = r.dataset.goto, key = r.dataset.key;
    if (!task && !go && !key) return;
    r.classList.add("clickable");
    r.onclick = async () => {
      // Closes the tray along with anything else open, and refuses when a form
      // has unsaved edits. Refused means the row stays clickable rather than
      // the tray shutting on work somebody is in the middle of.
      if (!await closeOpenDialogs()) return;
      // Atrium owning the runner means the terminal is where the work is.
      // Landing on the perms tab would mean answering and then going to find
      // it.
      if (task && await attachIfSupervised(task)) return;
      if (go) switchView(go);
      // The actual request, not just the right tab. The list may still be
      // rendering, so this waits a frame before hunting for the card.
      if (key) setTimeout(() => focusPerm(key), 60);
    };
  });

  // Marked read on OPEN rather than on close, so the highlight says what was
  // new when you got here and does not clear under you while you read it.
  try { localStorage.setItem(TOASTLOG_KEY + ".seen", String(Date.now())); } catch (e) {}
  // Already open means this is a repaint after clearing, and `showModal` on an
  // open dialog throws.
  const dlg = document.getElementById("toastlog");
  if (!dlg.open) dlg.showModal();
  paintToastLogBadge();
}

function clearToastLog() {
  saveToastLog([]);
  openToastLog();
}

// A time somebody can read at a glance. Today is a clock, older is a date:
// "11:54" answers "was that just now" and a date answers "was that today".
function shortWhen(ms) {
  const d = new Date(ms);
  const now = new Date();
  const sameDay = d.toDateString() === now.toDateString();
  const hh = String(d.getHours()).padStart(2, "0");
  const mm = String(d.getMinutes()).padStart(2, "0");
  if (sameDay) return `${hh}:${mm}`;
  return `${d.getMonth() + 1}/${d.getDate()} ${hh}:${mm}`;
}

addEventListener("DOMContentLoaded", paintToastLogBadge);
