// ── directory browser ───────────────────────────────────
// Browses the DAEMON's filesystem. The browser's own picker reads whatever
// machine the browser is on, so a board open on a phone would offer the phone's
// folders to start a runner in.
const browseDlg = document.getElementById("browse");
let browseAt = { path: "", parent: "" };
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
  document.getElementById("b-path").textContent = data.path || "this machine";
  document.getElementById("b-up").disabled = !data.path;
  document.getElementById("b-use").disabled = !data.path;

  const entries = data.entries || [];
  const list = document.getElementById("b-list");
  list.innerHTML = entries.length
    ? entries.map(e => `
        <button class="dir ${e.repo ? "repo" : ""}" data-path="${esc(e.path)}"
          title="${esc(e.path)}">
          <span class="ic">${e.repo ? "&#9679;" : "&#9656;"}</span>
          <span class="nm">${esc(e.name)}</span>
        </button>`).join("")
    : `<div class="empty">nothing to open in here</div>`;
  // The path goes in an attribute and comes back through the DOM, never through
  // an inline handler: HTML escaping is not JavaScript escaping, and a
  // directory named `it's mine` breaks out of a quoted string literal.
  list.querySelectorAll(".dir").forEach(b =>
    b.onclick = () => browseTo(b.dataset.path));
}

function browseUp() { browseTo(browseAt.parent); }

function useBrowsed() {
  if (!browseAt.path) return;
  // THE FIELD THAT ASKED. `browseInto` records which one opened the picker,
  // and there are four: launch, source, fixture and harness.
  const into = document.getElementById(browseInto_) || document.getElementById("l-cwd");
  if (into) into.value = browseAt.path;
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
    if (!chField.hidden) {
      setHTML(choices, opts.choices
        .map(c => `<option value="${esc(c.value)}">${esc(c.label)}</option>`).join(""));
      choices.value = opts.value || opts.choices[0].value;
    }

    // A confirmation that can be turned off. Only recorded on the affirmative
    // button: ticking the box and then cancelling means "no, and stop asking",
    // which would silently turn a refusal into a standing yes.
    const remember = document.getElementById("ask-remember");
    const rememberOn = document.getElementById("ask-remember-on");
    remember.hidden = !opts.rememberKey;
    rememberOn.checked = false;

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
        askClose(resolve, answerOf(opts, b.value, input, choices));
      };
      actions.appendChild(el);
    });

    // Escape cancels, enter takes the last button, which is always the
    // affirmative one. Same reflexes as the native dialog it replaces.
    askDlg.onclose = () => resolve(null);
    askDlg.onkeydown = e => {
      if (e.key !== "Enter" || e.shiftKey) return;
      e.preventDefault();
      const last = buttons[buttons.length - 1];
      askClose(resolve, answerOf(opts, last.value, input, choices));
    };

    askDlg.showModal();
    if (opts.input) { input.focus(); input.select(); }
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
  "hooks-nag": "tell me when claude hooks are not wired"
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

