// ── paste a link: ctrl-alt-r ────────────────────────────────────────────────
//
// One key from anywhere on the board, a box with the link already in it, and
// Enter starts the card. A link pasted on the board comes up in this same box.
// The recogniser fills the launch dialog, and the dialog's own launch is what
// starts the card, pull request chain and all (`launchNow` in js/fixtures.js).
// On Enter the dialog is filled in out of sight and never shown, so the screen
// stays the board and a line over it names the step that is running.
// Nothing here knows what a link is.
//
// ENTER STARTS AT ONCE, SHIFT-ENTER STOPS FOR EDITING. Enter is "open this", a
// pull request, an issue, a branch or a ticket in its own worktree, which is
// what the key is for nine times in ten. Shift-Enter opens the launch dialog
// filled in and leaves it there, for the prompt, the room, or a ticket's repo
// somebody wants to change first. A link that leaves something to fix (a hole in a
// template, a fetch that failed, a room with no runner) stops at the dialog
// either way, because starting a card that is already wrong is not quicker.
//
// The board's, not the machine's. It works while the page has focus, inside a
// terminal too, and nowhere else.
const QUICKPASTE_KEY = "ctrl+alt+KeyR";

// Capture on `window`, and stopped there, for the switcher's reason: xterm
// listens on its own textarea, so letting the keystroke through would open the
// box and send a control character to the runner. Some layouts send ctrl+alt
// for AltGr, and AltGr+r is a letter somebody meant to type, so that is left
// alone, the same rule as ctrl-alt-n in js/terminal.js.
addEventListener("keydown", e => {
  if (swCapturing || comboOf(e) !== QUICKPASTE_KEY) return;
  if (e.getModifierState && e.getModifierState("AltGraph")) return;
  e.preventDefault();
  e.stopPropagation();
  const dlg = document.getElementById("quickpaste");
  if (dlg && dlg.open) dlg.close();
  else openQuickPaste();
}, true);

const QUICKPASTE_URL = /^https?:\/\/\S+$/;

const QUICKPASTE_HELP = "a pull request, an issue, a branch or a support ticket. it is read as a paste on the board is.";

function quickPasteSay(text, warn) {
  const note = document.getElementById("qp-note");
  note.textContent = text;
  note.classList.toggle("warn", !!warn);
  note.classList.remove("seen");
  note.removeAttribute("data-kind");
}

// WHAT THE BOX READ THE LINK AS, LIVE. The same /v1/recognise a paste on the board asks, a moment after the typing
// stops, and nothing is started or filled in by it. The kind is `pastedOpenKind`'s, read off the captures, so the
// chip says what Enter would open and a link the open verb takes no card for is plainly just a link.
const QUICKPASTE_KINDS = { pr: "PR", issue: "issue", support: "ticket", branch: "branch", "": "link" };
let quickPasteTimer = 0;
let quickPasteSeq = 0;

function quickPasteSeen(got) {
  const note = document.getElementById("qp-note");
  const kind = pastedOpenKind(got);
  const v = got.vars || {};
  const bits = [];
  if (v.org && v.repo) bits.push(v.org + "/" + v.repo + (Number(v.num) > 0 ? "#" + v.num : ""));
  else if (Number(v.num) > 0) bits.push("#" + v.num);
  const detail = got.title && !bits.includes(got.title) ? got.title : bits[0] || "";
  const problem = kind ? "" : got.problem;
  const bad = problem || got.fetch_error;
  note.textContent = "";
  const chip = document.createElement("span");
  chip.className = "qpkind";
  chip.textContent = QUICKPASTE_KINDS[kind];
  const label = document.createElement("span");
  label.className = "qplabel";
  label.textContent = got.label || got.recogniser || "";
  note.append(chip, label);
  if (detail || bad) {
    const rest = document.createElement("span");
    rest.className = "qpdetail";
    rest.textContent = bad ? (got.fetch_error ? "the fetch failed: " + got.fetch_error : problem) : detail;
    note.append(rest);
  }
  note.setAttribute("data-kind", kind || "link");
  note.classList.add("seen");
  note.classList.toggle("warn", !!bad);
}

async function quickPasteRecognise() {
  const url = document.getElementById("qp-url").value.trim();
  const seq = ++quickPasteSeq;
  if (!QUICKPASTE_URL.test(url)) {
    quickPasteSay(QUICKPASTE_HELP);
    return;
  }
  let got;
  try {
    got = await api("/v1/recognise", {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ url })
    });
  } catch (e) {
    if (seq !== quickPasteSeq) return;
    quickPasteSay(/404|no recogniser/i.test(e.message)
      ? "nothing here knows what that is yet. add a row for it under runners, recognisers." : e.message, true);
    return;
  }
  if (seq === quickPasteSeq) {
    quickPasteKnown = { url, got };
    quickPasteSeen(got);
  }
}

// What the box last read, for the link it read it of. Enter uses it rather than asking the same question again.
let quickPasteKnown = null;

// `now` for a link that arrived whole (the clipboard), a short wait for one being typed.
function quickPasteWatch(now) {
  clearTimeout(quickPasteTimer);
  if (now) quickPasteRecognise();
  else quickPasteTimer = setTimeout(quickPasteRecognise, 350);
}

// `url` and `say` reopen the box on a link nothing recognised, with the reason.
async function openQuickPaste(url, say) {
  const dlg = document.getElementById("quickpaste");
  if (!dlg || dlg.open) return;
  // Not stacked on another dialog. The launch dialog half filled, or a card
  // being edited, is somebody in the middle of something.
  if (document.querySelector("dialog[open]")) return;
  const box = document.getElementById("qp-url");
  box.value = url || "";
  clearTimeout(quickPasteTimer);
  quickPasteSeq++;
  quickPasteSay(say || QUICKPASTE_HELP, !!say);
  quickPasteKnown = null;
  dlg.showModal();
  box.focus();
  if (url) {
    // A link that came whole, from a paste on the board: read now, so Enter has the answer already. A link sent back
    // with a reason (`say`) keeps the reason.
    if (!say && QUICKPASTE_URL.test(url)) {
      box.select();
      quickPasteWatch(true);
    }
    return;
  }
  // THE CLIPBOARD, WHEN IT ANSWERS IN TIME. Raced against the same wait as a
  // terminal paste, because an unanswered permission prompt never settles. A
  // link goes in and is selected, so typing over it is one keystroke. Anything
  // else is left out, and so is a box somebody already typed into.
  let text = "";
  try {
    const read = navigator.clipboard && navigator.clipboard.readText ? navigator.clipboard.readText() : null;
    if (read) text = await Promise.race([read, new Promise(r => setTimeout(() => r(""), pasteWaitMs))]);
  } catch (e) {
    text = "";
  }
  text = String(text || "").trim();
  if (dlg.open && !box.value && QUICKPASTE_URL.test(text)) {
    box.value = text;
    box.select();
    quickPasteWatch(true);
  }
}

async function quickPasteGo(edit) {
  const url = document.getElementById("qp-url").value.trim();
  clearTimeout(quickPasteTimer);
  quickPasteSeq++;
  if (!QUICKPASTE_URL.test(url)) {
    quickPasteSay("that is not a link. paste one that starts with http:// or https://.", true);
    return;
  }
  document.getElementById("quickpaste").close();
  // ENTER NEVER SHOWS THE LAUNCH DIALOG. It is filled in out of sight with the defaults it would have had, and the
  // link is opened: clone if needed, worktree, card. Only Shift-Enter shows it, and an error brings it up, filled in,
  // so it can be fixed.
  const known = quickPasteKnown && quickPasteKnown.url === url ? quickPasteKnown.got : null;
  // No runner enabled: openLaunch said so and went to the runners pane.
  if (!await openLaunch(null, "", "", null, { url, quiet: !edit })) return;
  const launch = document.getElementById("launch");
  if (!edit && !known) openProgressSay("recognising the link");
  const got = await recogniseLink(known || undefined);
  if (!edit) openProgressEnd();
  if (got === "none") {
    if (launch.open) launch.close();
    openQuickPaste(url, document.getElementById("l-link-note").textContent);
    return;
  }
  if (edit) return;
  // Not read, or a link with something left to fix: the dialog, with the reason in it.
  // A missing worktree is a problem the launch itself solves for every kind the open verb takes.
  const go = document.getElementById("l-go");
  if (!got || ((got.problem || got.fetch_error) && !pastedOpenKind()) || (go && go.disabled)) {
    if (!launch.open) launch.showModal();
    return;
  }
  await oneAtATime("quickpaste-open", async () => {
    try {
      await launchNow();
    } catch (e) {
      // THE DIALOG COMES UP FILLED IN, with what went wrong beside the link, so it can be fixed and pressed again.
      openProgressEnd();
      if (!launch.open) launch.showModal();
      const note = document.getElementById("l-link-note");
      note.classList.add("warn");
      note.textContent = (e && e.message) || String(e);
    }
  });
}

(function () {
  const box = document.getElementById("qp-url");
  if (!box) return;
  box.addEventListener("input", () => quickPasteWatch(false));
  box.addEventListener("keydown", e => {
    if (e.key !== "Enter") return;
    e.preventDefault();
    quickPasteGo(e.shiftKey);
  });
})();
