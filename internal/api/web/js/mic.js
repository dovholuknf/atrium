// The one microphone. Every box that sends text to an agent gets the same button, drawn here and nowhere else, and
// dictation lands in the box at the caret as typed text, so the draft, the send button and the typing gate all see it.
// The board and the phone page both load this file, and `css/mic.css` is its only style.
//
// A box is named in BOXES and nothing else. A box drawn later is caught by the observer, so no render site changes.
// A browser with no speech recognition gets no button. Chrome sends the audio to Google and Edge to Microsoft, and
// nothing is kept here. See docs/spikes/speech-to-text.md.
//
// ONE LISTENS AT A TIME. Starting one stops the one before, and the box that is listening is outlined. With
// `data-mic-show="one"` on the page only the mic of the box last focused or used is drawn, and the terminal strip's
// mic (a button marked `data-mic-term`, which pastes into the terminal) when no box qualifies.
(function () {
  "use strict";
  const BOXES = ".qa-box, .it-answer-box, .gr-reply, .gm-reply, .gm-why, .mp-reason-box, .mc-box, #t-paste-in, #t-ctxbox-in";
  const ICON = '<svg viewBox="0 0 24 24" aria-hidden="true"><rect x="9" y="3" width="6" height="11" rx="3"/>' +
    '<path d="M5 11a7 7 0 0 0 14 0M12 18v3"/><path class="mic-slash" d="M4 4l16 16"/></svg>';
  const R = window.SpeechRecognition || window.webkitSpeechRecognition;
  if (!R) return;

  const enabled = () => typeof flagOn !== "function" || flagOn("micin");
  let live = null; // { rec, btn }
  let last = null; // the box last focused or used

  function put(box, text) {
    const at = box.selectionEnd == null ? box.value.length : box.selectionEnd;
    const gap = at && !/\s$/.test(box.value.slice(0, at)) ? " " : "";
    box.setRangeText(gap + text, box.selectionStart == null ? at : box.selectionStart, at, "end");
    box.dispatchEvent(new Event("input", { bubbles: true }));
  }

  function stop(state) {
    if (!live) return;
    const l = live;
    live = null;
    l.rec.stop();
    l.btn.dataset.state = state || "";
    l.btn.dataset.tip = state === "denied" ? "the microphone is blocked, allow it for this site in the browser settings" : "dictate";
    if (l.btn._box) l.btn._box.classList.remove("mic-live");
  }

  function start(btn, sink) {
    stop();
    const rec = new R();
    rec.continuous = true;
    rec.interimResults = false;
    rec.lang = document.documentElement.lang || "en-US";
    rec.onresult = e => {
      for (let i = e.resultIndex; i < e.results.length; i++) if (e.results[i].isFinal) sink(e.results[i][0].transcript.trim());
    };
    rec.onerror = e => { if (live && live.rec === rec) stop(e.error === "not-allowed" || e.error === "service-not-allowed" ? "denied" : ""); };
    // Chrome ends a continuous session on its own after a pause, so it restarts while this one is the live one.
    rec.onend = () => { if (live && live.rec === rec && btn.isConnected) rec.start(); else if (live && live.rec === rec) stop(); };
    live = { rec, btn };
    btn.dataset.state = "listening";
    btn.dataset.tip = "listening, tap to stop";
    if (btn._box) btn._box.classList.add("mic-live");
    rec.start();
  }

  // The mic that stands for the page right now: the last box's, else the terminal strip's.
  function here() {
    const own = last && last.offsetParent && last._mic && last._mic.isConnected ? last._mic : null;
    const mine = own || document.querySelector("button.mic[data-mic-term]");
    document.querySelectorAll("button.mic.mic-here").forEach(b => { if (b !== mine) b.classList.remove("mic-here"); });
    if (mine) mine.classList.add("mic-here");
  }

  // A box with an action row under it gives the mic a place in that row, just left of send. Any other box wears it inside.
  function slot(box) {
    if (box.classList.contains("gm-why")) return box.nextElementSibling; // the placeholder is long, so the mic sits beside it
    const host = box.closest(".pastebox, .qa-fly"), row = host && host.querySelector(".pastebox-row, .qa-ff");
    return row ? Array.from(row.querySelectorAll("button")).find(b => b.hasAttribute("data-qa-send") || /^(send|queue)$/.test(b.textContent.trim())) : null;
  }

  function add(box) {
    if (!enabled() || box.dataset.mic || !box.setRangeText) return;
    box.dataset.mic = "1";
    const btn = document.createElement("button");
    btn.type = "button";
    btn.className = "mic";
    btn.dataset.tip = "dictate";
    btn.setAttribute("aria-label", "dictate");
    btn.innerHTML = ICON;
    btn._box = box;
    box._mic = btn;
    if (document.activeElement === box) last = box;
    // Keeps the caret and the keyboard where they are, so the words land where the person was typing.
    btn.addEventListener("pointerdown", e => e.preventDefault());
    btn.addEventListener("click", () => {
      last = box;
      here();
      if (live && live.btn === btn) stop(); else start(btn, t => put(box, t));
    });
    const at = slot(box);
    if (at) { at.before(btn); return; }
    // The button sits INSIDE the box, so the box goes into a wrapper. A box that had the focus keeps it and its caret,
    // because moving a node drops focus.
    const had = document.activeElement === box, from = box.selectionStart, to = box.selectionEnd;
    const wrap = document.createElement("span");
    wrap.className = "mic-wrap";
    box.replaceWith(wrap);
    wrap.append(box, btn);
    if (had) { box.focus(); box.setSelectionRange(from, to); }
  }

  function wireTerm(btn) {
    if (!enabled() || btn.dataset.micWired) return;
    btn.dataset.micWired = "1";
    btn.innerHTML = ICON;
    btn.addEventListener("pointerdown", e => e.preventDefault());
    btn.addEventListener("click", () => {
      if (live && live.btn === btn) stop();
      else start(btn, t => { if (typeof sendPasteText === "function") sendPasteText(t + " "); });
    });
  }

  function scan(root) {
    if (root.matches && root.matches(BOXES)) add(root);
    if (root.matches && root.matches("button.mic[data-mic-term]")) wireTerm(root);
    if (root.querySelectorAll) {
      root.querySelectorAll(BOXES).forEach(add);
      root.querySelectorAll("button.mic[data-mic-term]").forEach(wireTerm);
    }
  }

  // Off until it is switched on under experimental features. Switching it off takes every mic away and gives the boxes back.
  function unwire() {
    stop();
    document.querySelectorAll("button.mic[data-mic-term]").forEach(b => { delete b.dataset.micWired; b.innerHTML = ""; });
    document.querySelectorAll("[data-mic]").forEach(box => {
      if (box._mic && box._mic.parentNode) box._mic.remove();
      delete box._mic;
      delete box.dataset.mic;
      box.classList.remove("mic-live");
      const wrap = box.parentNode;
      if (wrap && wrap.classList && wrap.classList.contains("mic-wrap")) wrap.replaceWith(box);
    });
  }
  window.addEventListener("emerging", () => { if (enabled()) { scan(document); here(); } else unwire(); });

  document.addEventListener("focusin", e => { if (e.target.dataset && e.target.dataset.mic) { last = e.target; here(); } });
  new MutationObserver(ms => {
    ms.forEach(m => m.addedNodes.forEach(n => { if (n.nodeType === 1) scan(n); }));
    here();
  }).observe(document.documentElement, { childList: true, subtree: true, attributes: true, attributeFilter: ["hidden"] });
  scan(document);
  here();
})();
