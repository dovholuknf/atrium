// ── directory browser ───────────────────────────────────
// Browses the DAEMON's filesystem. The browser's own picker reads whatever
// machine the browser is on, so a board open on a phone would offer the phone's
// folders to start a runner in.
const browseDlg = document.getElementById("browse");
let browseAt = { path: "", parent: "" };
// What the operator PRESSED, which is what `use` hands back. The dialog used
// to hand back the directory being listed however plainly a row looked chosen,
// so choosing `bring-your-theme` and pressing the button gave you its parent.
let browseSel = "";
// This folder's children, held so the filter box can narrow them without
// asking the daemon again.
let browseEntries = [];
// Which field the chosen path goes back into, so one dialog serves both the
// launch form and the runner form.
let browseInto_ = "l-cwd";

// ESCAPE CLOSES THE PICKER.
//
// A modal `<dialog>` does this natively, but only while the focus is inside it,
// and this one opens over a board whose terminal takes focus back on a redraw.
// Listening on the document in the capture phase makes the answer independent
// of where the caret happened to be, and the `open` guard keeps it from
// touching anything when the picker is not up.
//
// `stopPropagation` because the picker STACKS: it opens over the settings,
// fixture, source and harness dialogs, and without it one press would close
// the picker and the form that asked for it.
document.addEventListener("keydown", e => {
  if (e.key !== "Escape" || !browseDlg.open) return;
  e.preventDefault();
  e.stopPropagation();
  browseDlg.close();
}, true);

function openBrowse() { return browseInto("l-cwd"); }

async function browseInto(fieldID) {
  browseInto_ = fieldID;
  // Start from whatever is already typed, so browse refines a path rather than
  // throwing it away.
  const from = (document.getElementById(fieldID).value || "").trim();
  await browseTo(from);
  browseDlg.showModal();
}

async function browseTo(path) {
  let data;
  try {
    data = await api("/v1/browse" + (path ? "?path=" + encodeURIComponent(path) : ""));
  } catch (e) {
    // A path that no longer exists, or one the daemon cannot read. Fall back to
    // the roots rather than leaving an empty dialog.
    if (path) { await browseTo(""); return; }
    tellUser("cannot browse", e.message);
    return;
  }
  browseAt = { path: data.path || "", parent: data.parent || "" };
  browseSel = "";
  browseEntries = data.entries || [];
  document.getElementById("b-path").textContent = data.path || "this machine";
  document.getElementById("b-up").disabled = !data.path;
  document.getElementById("b-filter").value = "";
  browseList();
}

// The rows for what is on screen now, narrowed by whatever is in the filter.
function browseList() {
  const q = (document.getElementById("b-filter").value || "").trim().toLowerCase();
  const rows = q ? browseEntries.filter(e => (e.name || "").toLowerCase().includes(q)) : browseEntries;
  const list = document.getElementById("b-list");
  // The folder name selects; the separate open button navigates into it.
  list.innerHTML = rows.length
    ? rows.map(e => `
        <div class="drow ${e.repo ? "repo" : ""}" data-path="${esc(e.path)}">
          <button class="dir" title="${esc(e.path)}">
            <span class="ic">${e.repo ? "&#9679;" : "&#9656;"}</span>
            <span class="nm">${esc(e.name)}</span>
          </button>
          <button class="into" title="list what is inside">open&#8202;&#8250;</button>
        </div>`).join("")
    : `<div class="empty">${browseEntries.length ? "nothing here matches that" : "nothing to open in here"}</div>`;
  // The path goes in an attribute and comes back through the DOM, never through
  // an inline handler: HTML escaping is not JavaScript escaping, and a
  // directory named `it's mine` breaks out of a quoted string literal.
  list.querySelectorAll(".drow").forEach(row => {
    const path = row.dataset.path;
    const name = row.querySelector(".dir");
    name.onclick = () => browsePick(path);
    // A double press is the reflex a file manager taught everybody, so it
    // descends as well rather than selecting the same row twice.
    name.ondblclick = () => browseTo(path);
    row.querySelector(".into").onclick = () => browseTo(path);
  });
  browseMark();
}

function browsePick(path) { browseSel = path; browseMark(); }

// What is chosen, said on the row and said again on the button that uses it.
function browseMark() {
  document.querySelectorAll("#b-list .drow").forEach(row =>
    row.classList.toggle("on", row.dataset.path === browseSel));
  const target = browseSel || browseAt.path;
  const use = document.getElementById("b-use");
  use.disabled = !target;
  use.textContent = browseSel ? "use " + browseName(browseSel) : "use this folder";
}

function browseName(path) {
  const parts = String(path).split(/[\\/]/).filter(Boolean);
  return parts.length ? parts[parts.length - 1] : path;
}

document.getElementById("b-filter").addEventListener("input", browseList);
document.getElementById("b-filter").addEventListener("keydown", e => {
  if (e.key !== "Enter") return;
  e.preventDefault();
  const typed = e.target.value.trim();
  // A path goes there directly. Anything else is a filter, and enter on a
  // filter that has narrowed the list to one folder opens that folder.
  if (/[\\/]/.test(typed)) { browseTo(typed); return; }
  const rows = document.querySelectorAll("#b-list .drow");
  if (rows.length === 1) browseTo(rows[0].dataset.path);
});

function browseUp() { browseTo(browseAt.parent); }

function useBrowsed() {
  // The row that was pressed, and the folder being listed only when no row was.
  const path = browseSel || browseAt.path;
  if (!path) return;
  // THE FIELD THAT ASKED. `browseInto` records which one opened the picker,
  // and there are four: launch, source, fixture and harness.
  const into = document.getElementById(browseInto_) || document.getElementById("l-cwd");
  if (into) into.value = path;
  browseDlg.close();
}

// Enter starts the runner from any field in the launch form.
document.getElementById("launch").addEventListener("keydown", e => {
  if (e.key !== "Enter" || e.shiftKey) return;
  const t = e.target;
  if (t && t.tagName === "TEXTAREA") return;
  e.preventDefault();
  doLaunch();
});

// ── dialogs ─────────────────────────────────────────────
// The browser's own confirm, alert and prompt are the wrong furniture in an
// app that looks like this, and they block the whole page while they are up.
// One promise-based dialog replaces all three.
const askDlg = document.getElementById("ask");

// Who is waiting on the dialog that is up.
//
// There is ONE dialog element for every question the board asks, so a second
// question arriving while one is on screen is not a second dialog. It is the
// same one, rewritten. Held here so that caller can be answered rather than
// left waiting on a promise nothing will ever settle.
let askPending = null;

function askClose(resolve, value) {
  askDlg.close();
  resolve(value);
}

// What a dialog answers with.
//
// A cancel is always `null`, whatever field is on screen, because the caller
// distinguishes "did not choose" from "chose the empty string" and an input
// cannot express the first. Otherwise the answer is whichever field the dialog
// was built with, and the button's own value when it was built with neither.
function answerOf(opts, btnValue, input, choices) {
  if (btnValue === null) return null;
  if (opts.choices && opts.choices.length) return choices.value;
  if (opts.input) return input.value;
  return btnValue;
}

// opts: { title, body (html), buttons: [{label, value, style}], input, value }
function askUser(opts) {
  return new Promise(resolve => {
    // A question asked while another one is up.
    //
    // `showModal` on a dialog that is already open THROWS `InvalidStateError`,
    // and a caller that does not await this promise never sees the rejection,
    // so the click looks like it did nothing. The header's share pill was
    // exactly that: one stale dialog open somewhere and the pill was dead for
    // the rest of the session.
    //
    // The new question wins, because it is the one the operator just asked for.
    // Whoever was waiting on the old one is answered with a cancel, which is
    // what they get for any other dismissal.
    if (askPending) {
      const prior = askPending;
      askPending = null;
      prior(null);
    }
    askPending = resolve;
    // Settling clears the slot first, so a dialog answered normally does not
    // leave a stale resolver for the next question to cancel. Resolving twice
    // is harmless, and the close event below always arrives after a button.
    const settle = v => {
      if (askPending === resolve) askPending = null;
      resolve(v);
    };
    document.getElementById("ask-title").textContent = opts.title || "";
    document.getElementById("ask-body").innerHTML = opts.body || "";

    const field = document.getElementById("ask-field");
    const input = document.getElementById("ask-input");
    field.hidden = !opts.input;
    if (opts.input) {
      input.value = opts.value || "";
      input.placeholder = opts.placeholder || "";
    }
    // Suggestions, when there is a known set. A datalist rather than a select
    // because these lists are long to scroll and short to type into, and
    // typing something not on the list stays possible.
    const list = document.getElementById("ask-list");
    if (list) {
      setHTML(list, (opts.list || [])
        .filter(Boolean)
        .map(v => `<option value="${esc(v)}"></option>`).join(""));
      input.setAttribute("list", (opts.list && opts.list.length) ? "ask-list" : "");
    }

    // `choices: [{value, label}]` is the closed-set case: a real select, where
    // the answer is one of these and the labels are what makes them readable.
    const chField = document.getElementById("ask-choices-field");
    const choices = document.getElementById("ask-choices");
    chField.hidden = !(opts.choices && opts.choices.length);
    // Use size to show a list box, capped at the requested number of rows.
    // Short lists fit their contents; longer lists scroll.
    let rows = 0;
    if (!chField.hidden) {
      setHTML(choices, opts.choices
        .map(c => `<option value="${esc(c.value)}">${esc(c.label)}</option>`).join(""));
      choices.value = opts.value || opts.choices[0].value;
      rows = opts.size ? Math.min(opts.size, opts.choices.length) : 0;
      if (rows > 1) choices.size = rows; else choices.removeAttribute("size");
      choices.classList.toggle("as-list", rows > 1);
    }

    // A confirmation that can be turned off. Only recorded on the affirmative
    // button: ticking the box and then cancelling means "no, and stop asking",
    // which would silently turn a refusal into a standing yes.
    const remember = document.getElementById("ask-remember");
    const rememberOn = document.getElementById("ask-remember-on");
    remember.hidden = !opts.rememberKey;
    rememberOn.checked = false;
    // A notice is not a question, so it can say "show" rather than "ask".
    const rememberSaid = rememberOn.nextElementSibling;
    if (rememberSaid) rememberSaid.textContent = opts.rememberLabel || "do not ask me again";

    const actions = document.getElementById("ask-actions");
    actions.innerHTML = "";
    const buttons = opts.buttons || [
      { label: "cancel", value: null },
      { label: "ok", value: true, style: "go" }
    ];
    buttons.forEach(b => {
      const el = document.createElement("button");
      el.textContent = b.label;
      if (b.style) el.className = b.style;
      el.onclick = () => {
        if (opts.rememberKey && rememberOn.checked && b.value !== null) {
          skipConfirm(opts.rememberKey, true);
        }
        askClose(settle, answerOf(opts, b.value, input, choices));
      };
      actions.appendChild(el);
    });

    // Escape cancels, enter takes the last button, which is always the
    // affirmative one. Same reflexes as the native dialog it replaces.
    askDlg.onclose = () => settle(null);
    askDlg.onkeydown = e => {
      if (e.key !== "Enter" || e.shiftKey) return;
      e.preventDefault();
      const last = buttons[buttons.length - 1];
      askClose(settle, answerOf(opts, last.value, input, choices));
    };

    // Already open means the element on screen has just been rewritten with
    // this question, so it needs showing again like it needs opening twice.
    if (!askDlg.open) askDlg.showModal();
    if (opts.input) { input.focus(); input.select(); }
    // A list longer than its cap opens scrolled to the top, which can be
    // nowhere near the row that is already chosen. Showing the selection is
    // the whole reason for drawing the rows at once.
    if (rows > 1 && choices.selectedIndex >= 0) {
      choices.options[choices.selectedIndex].scrollIntoView({ block: "nearest" });
    }
  });
}

// Drop-in shapes for the three things the native dialogs did.
// Confirmations the operator has turned off.
//
// Kept by name rather than by wording, so rephrasing a question does not start
// asking it again. Every one can be turned back on from the gear, or none of
// them could be, and a dialog you dismissed once would be gone for good.
const SKIP_KEY = "atrium.skipconfirm";

function skippedConfirms() {
  try { return JSON.parse(localStorage.getItem(SKIP_KEY) || "{}"); }
  catch (e) { return {}; }
}

function skipConfirm(key, on) {
  const all = skippedConfirms();
  if (on) all[key] = true; else delete all[key];
  localStorage.setItem(SKIP_KEY, JSON.stringify(all));
  paintSkipped();
}

// What a turned-off question is called in the gear. A key names the question,
// not its wording, so it needs a phrase here to be recognisable. A key with no
// entry still shows, under its own name, rather than disappearing from the one
// screen that can turn it back on.
const SKIP_LABELS = {
  "exit-terminal": "ask a runner to exit",
  "kill-runner": "terminate a runner",
  "move-while-pending": "move a card an agent is waiting on",
  "hooks-nag": "tell me when claude hooks are not wired",
  "width-floor": "tell me a terminal will not go narrower"
};

// The turned-off list, and the button that empties it.
//
// Whole list at once rather than one at a time: there is rarely more than one,
// and the reason to come here is "something stopped asking me and I want it
// back", which is answered by turning all of them back on.
function paintSkipped() {
  const field = document.getElementById("s-skipped-field");
  const chip = document.getElementById("s-skipped");
  if (!field || !chip) return;
  const keys = Object.keys(skippedConfirms());
  field.hidden = keys.length === 0;
  chip.textContent = keys.map(k => SKIP_LABELS[k] || k).join(", ");
}

function resetConfirms() {
  localStorage.removeItem(SKIP_KEY);
  paintSkipped();
  toast("those questions will be asked again");
}

// `rememberKey` makes the question skippable. Without one it always asks.
//
// The line is what a wrong answer costs. Stopping a runner is skippable
// because you do it all day and the card and its history stay. Forgetting a
// card, clearing a column, deleting a runner and turning a session loose
// unattended have no key: each throws away something there is no way back to,
// so each one asks every time.
const confirmUser = (title, body, okLabel, rememberKey) => {
  if (rememberKey && skippedConfirms()[rememberKey]) return Promise.resolve(true);
  return askUser({
    title, body, rememberKey,
    buttons: [{ label: "cancel", value: null }, { label: okLabel || "ok", value: true, style: "go" }]
  }).then(v => v === true);
};

const tellUser = (title, body) => askUser({
  title, body, buttons: [{ label: "ok", value: true, style: "go" }]
});

const askText = (title, body, value, placeholder) => askUser({
  title, body, input: true, value, placeholder,
  buttons: [{ label: "cancel", value: null }, { label: "ok", value: "", style: "go" }]
});

