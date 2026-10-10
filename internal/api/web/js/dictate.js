// ── dictation, a spike ──────────────────────────────────────────────────────
//
// Off unless localStorage "atrium.dictate" is "1". Ctrl+Alt+D toggles listening (the board's own key, works inside a
// terminal). Each FINAL phrase from the browser's Web Speech API is pasted into the focused terminal like a paste
// (`sendPasteText`), with no Enter, so what was said waits on the input line for review. Chrome sends the audio to
// Google and Edge to Microsoft; this keeps nothing. See docs/spikes/speech-to-text.md.
const DICTATE_STORE = "atrium.dictate";
let dictateRec = null, dictateBadge = null;

function dictateOn() {
  try { return localStorage.getItem(DICTATE_STORE) === "1"; } catch (e) { return false; }
}

function dictateSay(text) {
  if (!dictateBadge) {
    dictateBadge = document.createElement("div");
    dictateBadge.style.cssText = "position:fixed;right:12px;bottom:12px;z-index:99999;padding:4px 10px;" +
      "border-radius:12px;background:#b00020;color:#fff;font:12px sans-serif;max-width:40vw;pointer-events:none";
    document.body.appendChild(dictateBadge);
  }
  dictateBadge.textContent = text;
  dictateBadge.hidden = !text;
}

function dictateStop() {
  if (dictateRec) { const r = dictateRec; dictateRec = null; r.stop(); }
  dictateSay("");
}

function dictateStart() {
  const R = window.SpeechRecognition || window.webkitSpeechRecognition;
  if (!R) { dictateSay("no speech recognition in this browser"); return; }
  const r = new R();
  r.continuous = true;
  r.interimResults = true;
  r.lang = "en-US";
  r.onresult = e => {
    let interim = "";
    for (let i = e.resultIndex; i < e.results.length; i++) {
      const t = e.results[i][0].transcript;
      if (e.results[i].isFinal) { if (typeof sendPasteText === "function") sendPasteText(t.trim() + " "); }
      else interim += t;
    }
    dictateSay("● " + interim);
  };
  r.onerror = e => { dictateSay("dictation: " + e.error); dictateRec = null; };
  // Chrome ends a continuous session on its own after a pause; restart while the toggle is on.
  r.onend = () => { if (dictateRec === r) r.start(); };
  dictateRec = r;
  r.start();
  dictateSay("● listening");
}

document.addEventListener("keydown", e => {
  if (!dictateOn() || !e.ctrlKey || !e.altKey || e.code !== "KeyD") return;
  e.preventDefault();
  if (dictateRec) dictateStop(); else dictateStart();
}, true);
