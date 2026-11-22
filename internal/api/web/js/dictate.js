// ── push-to-talk on the desk ────────────────────────────────────────────────
//
// Hold the key (Right Ctrl by default, settings "dictation") or the mic at the bottom of the screen, talk, release.
// The card's room transcribes the clip (m/js/voice.js) and the text is pasted into the attached terminal, then Enter
// is sent. Holding Shift with the key (or the mic) adds the text FOLLOWED BY A NEWLINE inside the same bracketed
// paste instead, which Claude Code keeps in its input, so the person reads it and presses Enter. A runner with no
// bracketed paste gets a space instead, because a bare newline there is Enter. A clip ENDING with the send word
// ("send it", localStorage atrium.dictate.sendword) has it cut off and is sent even with Shift. The room is asked
// first whether it can transcribe, so "not set up" shows on press and not after talking.
//
// RIGHT CTRL waits DICT_ARM_MS before it opens the mic, and any other key in that time or while held cancels, so a
// Ctrl+C typed with the right hand never records. SPACE holds-to-talk only when nothing that takes typing has the
// focus: a held Space in a terminal, a box or on a button is still a space.
const DICT_KEY = "atrium.dictate.key";
const DICT_ARM_MS = 200;
let dictShift = false, dictState = "", dictCard = "", dictArm = 0, dictTick = 0, dictAt = 0, dictDock = null, dictPillTimer = 0;

function dictKey() {
  try { return localStorage.getItem(DICT_KEY) || "right-ctrl"; } catch (e) { return "right-ctrl"; }
}

function setDictKey(v) {
  try { localStorage.setItem(DICT_KEY, v); } catch (e) {}
  if (dictDock) dictMicTip(dictDock.querySelector(".dict-mic"));
}

// The phrase that ends a dictation and presses Enter. Default "send it", "" turns it off.
function dictSendWord() {
  try { const v = localStorage.getItem("atrium.dictate.sendword"); return v === null ? "send it" : v.trim(); }
  catch (e) { return "send it"; }
}

function dictCode() {
  const k = dictKey();
  return k === "space" ? "Space" : k === "right-ctrl" ? "ControlRight" : "";
}

// Whether the focus is somewhere a Space types or presses something.
function dictTakesSpace(el) {
  if (!el || el === document.body || el === document.documentElement) return false;
  return !!el.closest("input, textarea, select, button, a[href], summary, [contenteditable], [role=button], " +
    "[role=textbox], .xterm, [tabindex]:not([tabindex='-1'])");
}

function dictUI() {
  if (dictDock) return dictDock;
  const expando = document.getElementById("t-expando");
  if (!expando) return null;
  dictDock = document.createElement("div");
  dictDock.className = "dict-dock";
  dictDock.innerHTML = '<button type="button" class="dict-mic" aria-label="hold to dictate">' +
    '<svg viewBox="0 0 24 24" width="17" height="17" aria-hidden="true"><path d="M12 3a3 3 0 0 0-3 3v6a3 3 0 0 0 6 0V6a3 3 0 0 0-3-3zM5 11a7 7 0 0 0 14 0M12 18v3" ' +
    'fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"/></svg></button>' +
    '<div class="dict-pill" role="status" aria-live="polite" hidden></div>';
  expando.before(dictDock);
  const drawer = document.getElementById("t-drawer");
  if (drawer) {
    new MutationObserver(dictLift).observe(drawer, { attributes: true, attributeFilter: ["class"] });
    if (window.ResizeObserver) new ResizeObserver(dictLift).observe(drawer);
  }
  const mic = dictDock.querySelector(".dict-mic");
  dictMicTip(mic);
  mic.addEventListener("click", e => e.stopPropagation());
  mic.addEventListener("pointerdown", e => {
    e.preventDefault();
    mic.setPointerCapture(e.pointerId);
    dictBegin();
  });
  mic.addEventListener("pointerup", e => dictFinish(e.shiftKey));
  mic.addEventListener("pointercancel", () => dictCancel());
  mic.addEventListener("pointermove", e => {
    if (dictState !== "listening") return;
    const r = mic.getBoundingClientRect();
    if (Math.hypot(e.clientX - (r.left + r.width / 2), e.clientY - (r.top + r.height / 2)) > 120) dictCancel();
  });
  return dictDock;
}

function dictMicTip(mic) {
  const k = dictKey();
  const key = k === "space" ? "Space (when no box or terminal has the focus)" : k === "right-ctrl" ? "Right Ctrl" : "";
  mic.dataset.tip = "hold to talk, release to send the words" + (key ? ", or hold " + key : "") +
    ". Hold Shift as well to add them to the input line without sending. Transcribed on the card's room, nothing leaves your machines.";
}

// The pill and the details drawer both rise from the strip, so with the drawer open the pill goes above it.
function dictLift() {
  const pill = dictDock && dictDock.querySelector(".dict-pill");
  const dr = document.getElementById("t-drawer");
  if (!pill) return;
  pill.style.bottom = dr && dr.classList.contains("open") ? "calc(100% + 16px + " + dr.offsetHeight + "px)" : "";
}

function dictShow(state, html, ms) {
  const dock = dictUI();
  if (!dock) return;
  dock.dataset.st = state;
  const pill = dock.querySelector(".dict-pill");
  clearTimeout(dictPillTimer);
  pill.innerHTML = html || "";
  pill.hidden = !html;
  dictLift();
  if (html && ms) dictPillTimer = setTimeout(() => { if (dock.dataset.st === state) dictShow("", ""); }, ms);
}

function dictText(state, text, ms) {
  dictShow(state, "<span></span>", ms);
  dictDock.querySelector(".dict-pill span").textContent = text;
}

function dictListening() {
  const s = Math.floor((Date.now() - dictAt) / 1000);
  dictShow("listening", '<span class="dot"></span><span class="lvl"><i></i><i></i><i></i><i></i></span> listening 0:' +
    String(s).padStart(2, "0") + ' <span class="hint">release to send, hold Shift to add lines only, Esc to cancel</span>');
}

function dictBegin() {
  if (dictState) return;
  const why = window.atriumVoice ? window.atriumVoice.blocked() : "The recorder did not load.";
  if (why) {
    dictShow("refused", '<span class="dict-why"></span><button type="button" class="x" aria-label="dismiss">&#10005;</button>');
    const pill = dictDock.querySelector(".dict-pill");
    pill.querySelector(".dict-why").textContent = why;
    pill.querySelector(".x").onclick = () => dictShow("", "");
    return;
  }
  if (!termTask) { dictText("error", "open a terminal to dictate into", 3000); return; }
  dictState = "listening";
  dictCard = termTask.id;
  dictAt = Date.now();
  dictListening();
  dictTick = setInterval(dictListening, 500);
  window.atriumVoice.ready(dictCard).then(off => {
    if (!off || dictState !== "listening") return;
    dictReset();
    window.atriumVoice.cancel();
    dictShow("refused", '<span class="why"></span><button type="button" class="x" aria-label="dismiss">&#10005;</button>');
    const pill = dictDock.querySelector(".dict-pill");
    pill.querySelector(".why").textContent = off;
    pill.querySelector(".x").onclick = () => dictShow("", "");
  });
  window.atriumVoice.start().catch(err => {
    if (dictState !== "listening") return;
    dictReset();
    dictText("error", err.message, 6000);
  });
}

function dictReset() {
  dictState = "";
  clearInterval(dictTick);
}

function dictCancel() {
  if (!dictState) return;
  dictReset();
  window.atriumVoice.cancel();
  dictShow("", "");
}

async function dictFinish(linesOnly) {
  if (dictState !== "listening") return;
  dictReset();
  dictState = "transcribing";
  dictShow("transcribing", '<span class="spin"></span> transcribing');
  const card = dictCard;
  try {
    const text = await window.atriumVoice.stop(card);
    dictState = "";
    if (!text) { dictShow("", ""); return; }
    if (!termTask || termTask.id !== card) {
      try { await navigator.clipboard.writeText(text); } catch (e) {}
      dictText("error", "the terminal changed, so the words are on the clipboard", 5000);
      return;
    }
    const sendWord = dictSendWord();
    const cut = sendWord ? text.replace(new RegExp("[\\s,.;:!?-]*\\b" + sendWord.replace(/[.*+?^${}()|[\]\\]/g, "\\$&") +
      "[\\s.,!?]*$", "i"), "") : text;
    const send = !linesOnly || cut !== text;
    const bracketed = (term && term.modes && term.modes.bracketedPasteMode) || !!termCaps.bracketed_paste;
    if (cut) sendPasteText(cut + (send ? "" : bracketed ? "\n" : " "));
    if (send) setTimeout(() => { if (termTask && termTask.id === card) sendInput("\r"); }, 250);
    dictShow("", "");
  } catch (err) {
    dictState = "";
    dictText("error", "dictation: " + err.message, 6000);
  }
}

document.addEventListener("keydown", e => {
  const code = dictCode();
  if (dictState === "listening" && e.key === "Escape") { e.preventDefault(); e.stopPropagation(); dictCancel(); return; }
  if (code === "ControlRight" && (e.code === "ShiftLeft" || e.code === "ShiftRight")) {
    if (dictArm || dictState === "listening") dictShift = true;
    return;
  }
  if (e.code !== code) {
    if (dictArm) { clearTimeout(dictArm); dictArm = 0; }
    if (dictState === "listening" && code === "ControlRight") dictCancel();
    return;
  }
  if (code === "Space") {
    if (e.ctrlKey || e.altKey || e.metaKey || e.shiftKey || dictTakesSpace(document.activeElement)) return;
    e.preventDefault();
  }
  if (e.repeat || dictArm || dictState) return;
  dictShift = e.shiftKey;
  dictArm = setTimeout(() => { dictArm = 0; dictBegin(); }, DICT_ARM_MS);
}, true);

document.addEventListener("keyup", e => {
  if (e.code !== dictCode()) return;
  if (dictArm) { clearTimeout(dictArm); dictArm = 0; return; }
  if (dictState === "listening") { if (e.code === "Space") e.preventDefault(); dictFinish(dictShift || e.shiftKey); }
}, true);

window.addEventListener("blur", () => {
  if (dictArm) { clearTimeout(dictArm); dictArm = 0; }
  dictCancel();
});

document.addEventListener("DOMContentLoaded", dictUI);
if (document.readyState !== "loading") dictUI();
