// ── paste a link: ctrl-alt-r ────────────────────────────────────────────────
//
// One key from anywhere on the board, a box with the link already in it, and
// Enter starts the card. It is the same road a link pasted on the board takes:
// the recogniser fills the launch dialog, and the dialog's own launch is what
// starts the card, pull request chain and all (`launchNow` in js/fixtures.js).
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

function quickPasteSay(text, warn) {
  const note = document.getElementById("qp-note");
  note.textContent = text;
  note.classList.toggle("warn", !!warn);
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
  quickPasteSay(say || "a pull request, an issue or a support link. it is recognised the same as a paste on the board.",
    !!say);
  dlg.showModal();
  box.focus();
  if (url) return;
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
  }
}

async function quickPasteGo(edit) {
  const url = document.getElementById("qp-url").value.trim();
  if (!QUICKPASTE_URL.test(url)) {
    quickPasteSay("that is not a link. paste one that starts with http:// or https://.", true);
    return;
  }
  document.getElementById("quickpaste").close();
  await openLaunch(null, "", "", null, { url });
  const launch = document.getElementById("launch");
  // No runner enabled: openLaunch said so and went to the runners pane.
  if (!launch.open) return;
  const got = await recogniseLink();
  if (got === "none") {
    launch.close();
    openQuickPaste(url, document.getElementById("l-link-note").textContent);
    return;
  }
  if (edit || !got) return;
  // A missing worktree is a problem the launch itself solves for every kind the open verb takes.
  if ((got.problem || got.fetch_error) && !pastedOpenKind()) return;
  const go = document.getElementById("l-go");
  if (go && go.disabled) return;
  doLaunch();
}

(function () {
  const box = document.getElementById("qp-url");
  if (!box) return;
  box.addEventListener("keydown", e => {
    if (e.key !== "Enter") return;
    e.preventDefault();
    quickPasteGo(e.shiftKey);
  });
})();
