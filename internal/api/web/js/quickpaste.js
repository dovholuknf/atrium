// ── opening a link, from the switcher ───────────────────────────────────────
//
// A link is typed or pasted into the switcher (js/switcher.js) like anything else it is asked, and the top result
// says what Enter would open: "open PR review zrok#12". There is no second box. Ctrl+Alt+R, the link button in the
// header and the "paste a link" button in the switcher all open that same bar, so the key somebody learned still
// works and so does a click. A link pasted on the board comes up in it too.
// The recogniser fills the launch dialog, and the dialog's own launch is what starts the card, pull request chain and
// all (`launchNow` in js/fixtures.js). On Enter the dialog is filled in out of sight and never shown, so the screen
// stays the board and a line over it names the step that is running. The rest of the board stays usable meanwhile.
// Nothing here knows what a link is.
//
// ENTER STARTS AT ONCE, SHIFT-ENTER STOPS FOR EDITING. Enter is "open this", a
// pull request, an issue, a branch or a ticket in its own worktree, which is
// what the key is for nine times in ten. Shift-Enter opens the launch dialog
// filled in and leaves it there, for the prompt, the room, or a ticket's repo
// somebody wants to change first. A link that leaves something to fix (a hole in a
// template, a fetch that failed, a room with no runner) stops at the dialog
// either way, because starting a card that is already wrong is not quicker.
// Somebody who would rather always see the dialog unticks "enter opens at once" in it, and that is kept here
// (`QUICKPASTE_DIRECT_STORE`). Not asking is the default.
//
// The board's, not the machine's. It works while the page has focus, inside a
// terminal too, and nowhere else.
const QUICKPASTE_KEY = "ctrl+alt+KeyR";
const QUICKPASTE_DIRECT_STORE = "atrium.open.direct";

// Capture on `window`, and stopped there, for the switcher's reason: xterm
// listens on its own textarea, so letting the keystroke through would open the
// bar and send a control character to the runner. Some layouts send ctrl+alt
// for AltGr, and AltGr+r is a letter somebody meant to type, so that is left
// alone, the same rule as ctrl-alt-n in js/terminal.js.
addEventListener("keydown", e => {
  if (swCapturing || comboOf(e) !== QUICKPASTE_KEY) return;
  if (e.getModifierState && e.getModifierState("AltGraph")) return;
  e.preventDefault();
  e.stopPropagation();
  const dlg = switcherDlg();
  if (dlg && dlg.open) dlg.close();
  else openQuickPaste();
}, true);

const QUICKPASTE_URL = /^https?:\/\/\S+$/;

const QUICKPASTE_HELP = "paste a link: a pull request, an issue, a branch or a support ticket.";

// What the bar last said about a link in place of reading it: a reason it was sent back, or a read that failed.
// `{ url, text, warn }`, for the link it was said of.
let quickPasteNote = null;

function quickPasteSay(url, text, warn) {
  quickPasteNote = text ? { url, text, warn: !!warn } : null;
  swRepaint();
}

// WHAT THE BAR READ THE LINK AS, LIVE. The same /v1/recognise a paste on the board asks, a moment after the typing
// stops, and nothing is started or filled in by it. The kind is `pastedOpenKind`'s, read off the captures, so the
// chip says what Enter would open and a link the open verb takes no card for is plainly just a link.
const QUICKPASTE_KINDS = { pr: "PR", issue: "issue", support: "ticket", branch: "branch", "": "link" };
let quickPasteTimer = 0;
let quickPasteSeq = 0;

// The top result for a link in the switcher: what Enter would open, or why it would not. Drawn by paintSwitcher.
function quickPasteRow(url) {
  const known = quickPasteKnown && quickPasteKnown.url === url ? quickPasteKnown.got : null;
  const note = quickPasteNote && quickPasteNote.url === url ? quickPasteNote : null;
  if (note) {
    return { kind: "link", seen: false, warn: note.warn,
      html: `<span class="qpkind">open link</span><span class="qpdetail">${esc(note.text)}</span>` };
  }
  if (!known) {
    return { kind: "link", seen: false, warn: false,
      html: `<span class="qpkind">open link</span><span class="qpdetail">${esc(url)}</span>` };
  }
  const kind = pastedOpenKind(known);
  const v = known.vars || {};
  const bits = [];
  if (v.org && v.repo) bits.push(v.org + "/" + v.repo + (Number(v.num) > 0 ? "#" + v.num : ""));
  else if (Number(v.num) > 0) bits.push("#" + v.num);
  const name = known.title || bits[0] || url;
  const problem = kind ? "" : known.problem;
  const bad = problem || known.fetch_error;
  const rest = bad ? (known.fetch_error ? "the fetch failed: " + known.fetch_error : problem)
    : known.label || known.recogniser || "";
  return { kind: kind || "link", seen: true, warn: !!bad,
    html: `<span class="qpkind">open ${esc(QUICKPASTE_KINDS[kind])}</span>` +
      (bad ? "" : `<span class="qplabel">${esc(name)}</span>`) +
      `<span class="qpdetail">${esc(rest)}</span>` };
}

async function quickPasteRecognise(url) {
  const seq = ++quickPasteSeq;
  if (!QUICKPASTE_URL.test(url)) return;
  let got;
  try {
    got = await api("/v1/recognise", {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ url })
    });
  } catch (e) {
    if (seq !== quickPasteSeq) return;
    quickPasteSay(url, /404|no recogniser/i.test(e.message)
      ? "nothing here knows what that is yet. add a row for it under runners, recognisers." : e.message, true);
    return;
  }
  if (seq === quickPasteSeq) {
    quickPasteKnown = { url, got };
    quickPasteNote = null;
    swRepaint();
  }
}

// What the bar last read, for the link it read it of. Enter uses it rather than asking the same question again.
let quickPasteKnown = null;

// `now` for a link that arrived whole (the clipboard), a short wait for one being typed. Called by the switcher on
// every edit, with whatever is in the field.
function quickPasteWatch(url, now) {
  clearTimeout(quickPasteTimer);
  quickPasteSeq++;
  if (!QUICKPASTE_URL.test(url)) return;
  if (quickPasteKnown && quickPasteKnown.url === url) return;
  if (now) quickPasteRecognise(url);
  else quickPasteTimer = setTimeout(() => quickPasteRecognise(url), 350);
}

// The switcher, opened for a link: the same bar the key opens, with the link in it. `url` and `say` reopen it on a link
// nothing recognised, with the reason. With neither, the clipboard's link goes in if it answers in time.
async function openQuickPaste(url, say) {
  // Not stacked on another dialog. The launch dialog half filled, or a card
  // being edited, is somebody in the middle of something.
  if (switcherDlg().open || document.querySelector("dialog[open]")) return;
  quickPasteNote = say && url ? { url, text: say, warn: true } : null;
  await openSwitcher({ link: true, url: url || "" });
  if (url) {
    // A link that came whole, from a paste on the board: read now, so Enter has the answer already. A link sent back
    // with a reason (`say`) keeps the reason.
    if (!say && QUICKPASTE_URL.test(url)) quickPasteWatch(url, true);
    return;
  }
  await quickPasteFromClipboard();
}

// THE CLIPBOARD, WHEN IT ANSWERS IN TIME. Raced against the same wait as a
// terminal paste, because an unanswered permission prompt never settles. A
// link goes in and is selected, so typing over it is one keystroke. Anything
// else is left out, and so is a bar somebody already typed into.
async function quickPasteFromClipboard(force) {
  const dlg = switcherDlg();
  const box = document.getElementById("sw-q");
  let text = "";
  try {
    const read = navigator.clipboard && navigator.clipboard.readText ? navigator.clipboard.readText() : null;
    if (read) text = await Promise.race([read, new Promise(r => setTimeout(() => r(""), pasteWaitMs))]);
  } catch (e) {
    text = "";
  }
  text = String(text || "").trim();
  if (dlg.open && (force || !box.value) && QUICKPASTE_URL.test(text)) {
    swSetQuery(text);
    box.focus();
    box.select();
    quickPasteWatch(text, true);
  } else if (force) {
    box.focus();
    quickPasteNote = null;
    swRepaint();
    toast("no link on the clipboard", "copy a link first, or paste one into the bar");
  }
}

function quickPasteDirect() {
  try { return localStorage.getItem(QUICKPASTE_DIRECT_STORE) !== "0"; } catch (e) { return true; }
}

async function quickPasteGo(url, edit) {
  clearTimeout(quickPasteTimer);
  quickPasteSeq++;
  if (!QUICKPASTE_URL.test(url)) return;
  closeSwitcher();
  // PLAIN ENTER NEVER SHOWS THE LAUNCH DIALOG, unless somebody said they want it to. It is filled in out of sight with
  // the defaults it would have had, and the link is opened: clone if needed, worktree, card. Shift-Enter shows it, and
  // an error brings it up, filled in, so it can be fixed.
  edit = !!edit || !quickPasteDirect();
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
