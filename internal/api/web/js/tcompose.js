// The board's phone terminal, typed into a real textarea instead of xterm's hidden one (u-025).
//
// WHY. xterm reads a hidden textarea, and an Android soft keyboard composes swipe words and dictation in place
// (compositionstart, update, end) and replaces them as it corrects. xterm turns each of those into keystrokes,
// so a swiped word arrives twice or garbled and one Backspace sends one DEL for a whole word. A real textarea
// lets the OS own swipe, whole-word delete, autocorrect and dictation. The text leaves only on the send button,
// as ONE bracketed paste then Enter, exactly what `sendPasteText` does for a paste and what the key bar's
// enter does after it.
//
// THE COMPOSER is web/m/js/compose.js, the /m page's, mounted here with `opts.send`. It is loaded from
// index.html and lives outside js/ and css/ on purpose, see `TestBoardLoadsEveryFile`.
//
// HOW THE SOFT KEYBOARD AND A HARDWARE KEYBOARD ARE TOLD APART. xterm's textarea stays where it is and stays
// focusable, so a hardware key still reaches xterm through keydown on the terminal with no compose focus. What
// changes is `inputmode="none"` on it (`phoneInputMode`, js/terminal-links.js): the browser will not raise the
// soft keyboard for a field that says so, and a tap on the terminal focuses xterm's textarea without one. Only
// the compose box raises it. Nothing is intercepted, so nothing has to guess which keyboard is which.
//
// Phone only (`termPhone`). A desktop is unchanged: the element stays hidden and nothing is mounted.
let tcomposeFor = "";

// Uploaded paths go into the box at its caret and the box takes focus, so the keyboard comes up and the person
// can write around them. Nothing is written to the pty until send. False when there is no composer (a desktop),
// and the caller pastes into the terminal as before.
function tcomposeInsert(text) {
  if (!tcomposeFor || !window.mCompose) return false;
  return window.mCompose.insert(text);
}

// A batch of files for the composer: one upload, one chip each, paths at the caret. False when there is no
// composer, and the caller uploads into the terminal as before.
function tcomposeAttach(files) {
  if (!tcomposeFor || !window.mCompose) return false;
  window.mCompose.attach(files);
  return true;
}

function tcomposeSync() {
  const host = document.getElementById("t-compose");
  if (!host || !window.mCompose) return;
  const want = termPhone() && termTask ? termTask.id : "";
  if (!want) {
    if (tcomposeFor) { window.mCompose.unmount(); tcomposeFor = ""; }
    host.hidden = true;
    return;
  }
  host.hidden = false;
  if (tcomposeFor === want) return;
  tcomposeFor = want;
  window.mCompose.mount(host, want, {
    compact: true, maxLines: 4, follow: false, noteMs: 2500,
    canUpload: () => !isGuest(),
    upload: (files) => {
      const form = new FormData();
      for (const f of files) form.append("file", f, f.name);
      return api(`/v1/tasks/${want}/files`, { method: "POST", body: form });
    },
    canPaste: () => !!((term && term.modes && term.modes.bracketedPasteMode) || termCaps.bracketed_paste),
    send: async (text) => {
      if (!termSock || termSock.readyState !== WebSocket.OPEN) throw new Error("the terminal is not connected");
      sendPasteText(text);
      sendInput("\r");
      return { kind: "sent", text: "sent" };
    }
  });
}
