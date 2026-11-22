// The open questions of a card, answered one by one: tap a question, type under it, Done (or Enter) moves to the next.
// One "Send answers" posts every answered one as "N. answer" per line. card.js draws `html(t)` into the questions block
// and calls `bind(host, repaint)`, which owns the clicks, so this file never reaches into card.js.
(function () {
  "use strict";

  const U = window.mUtil;
  const MD = window.mMd;
  const SHORT = 34;
  // A question that starts on an auxiliary verb and offers no "or" is a yes or no question.
  const YES_NO = /^(should|is|are|do|does|did|can|could|will|would|shall|may|has|have|was|were)\b(?!.*\bor\b)/i;
  const MIC = '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 3a3 3 0 0 0-3 3v6a3 3 0 0 0 6 0V6a3 3 0 0 0-3-3zM5 11a7 7 0 0 0 14 0M12 18v3" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"/></svg>';

  const yesNo = q => YES_NO.test(String(q).trim());
  function short(a) {
    const l = String(a).split("\n")[0].trim();
    return l.length > SHORT ? l.slice(0, SHORT - 1) + "…" : l;
  }
  function message(answers) {
    return Object.keys(answers).map(Number).sort((a, b) => a - b).map(i => (i + 1) + ". " + answers[i]).join("\n");
  }
  // The next question after `from` with no answer, wrapping, or -1.
  function next(n, answers, from) {
    for (let k = 1; k < n; k++) {
      const i = (from + k) % n;
      if (answers[i] === undefined) return i;
    }
    return -1;
  }

  // One state per card, for the question set it was made for. A new set (a new turn) starts clean.
  const states = new Map();
  function stateOf(id, qs) {
    const key = qs.join("\n");
    let s = states.get(id);
    if (!s || s.key !== key) { s = { key, ans: {}, open: -1, draft: "", sent: false, busy: false, note: "" }; states.set(id, s); }
    return s;
  }

  function html(t) {
    const qs = (t.seen && t.seen.open_questions) || [];
    const s = stateOf(t.id, qs);
    if (s.sent) return "";
    const n = Object.keys(s.ans).length;
    return '<ol class="q-list">' + qs.map((x, i) => {
      const done = s.ans[i] !== undefined, open = s.open === i;
      const hint = next(qs.length, s.ans, i) < 0 ? "done" : "next";
      return '<li class="q-item' + (done ? " done" : "") + (open ? " open" : "") + '" data-i="' + i + '">' +
        '<button type="button" class="q-head" aria-expanded="' + open + '"><span class="q-n">' + (i + 1) + '</span><span class="q-main"><span class="q-text">' + MD.inline(x) + "</span>" +
        (done ? '<span class="q-ans">' + U.esc(short(s.ans[i])) + "</span>" : "") + "</span>" + (done && !open ? '<span class="q-edit">edit</span>' : "") + "</button>" +
        (open ? '<div class="q-form">' + (yesNo(x) ? '<div class="q-chips"><button type="button" class="q-chip' + (s.ans[i] === "yes" ? " on" : "") + '" data-v="yes">Yes</button><button type="button" class="q-chip' + (s.ans[i] === "no" ? " on" : "") + '" data-v="no">No</button></div>' : "") +
          '<div class="q-row"><textarea class="q-box" rows="1" placeholder="Your answer" enterkeyhint="' + hint + '" autocapitalize="sentences" aria-label="answer to question ' + (i + 1) + '">' + U.esc(s.draft) + "</textarea>" +
          (window.atriumVoice ? '<button type="button" class="q-mic" aria-label="dictate">' + MIC + "</button>" : "") + '<button type="button" class="q-done">Done</button></div></div>' : "") + "</li>";
    }).join("") + "</ol>" +
      '<button type="button" class="q-send" id="q-send"' + (n && !s.busy ? "" : " disabled") + ">" + (s.busy ? "Sending…" : "Send answers" + (n ? " (" + n + ")" : "")) + "</button>" +
      '<p class="quiet q-sendnote">' + U.esc(s.note || (n ? n + " of " + qs.length + " answered. Only those are sent." : "Tap a question to answer it.")) + "</p>";
  }

  function grow(t) { t.style.height = "auto"; t.style.height = t.scrollHeight + "px"; }

  // `cur()` is the open card's { id, qs }, `repaint()` redraws the block.
  function bind(host, cur, repaint) {
    const here = () => { const c = cur(); return c ? { c, s: stateOf(c.id, c.qs) } : null; };
    const focusOpen = () => {
      const li = host.querySelector(".q-item.open");
      if (!li) return;
      const t = li.querySelector(".q-box");
      grow(t);
      t.focus({ preventScroll: true });
      t.setSelectionRange(t.value.length, t.value.length);
      setTimeout(() => li.scrollIntoView({ block: "center", behavior: "smooth" }), 60);
    };
    const open = (s, i) => { s.open = i; s.draft = s.ans[i] || ""; repaint(); focusOpen(); };
    // `go` moves on to the next unanswered one, otherwise the question just closes.
    const commit = (h, v, go) => {
      const { c, s } = h, i = s.open;
      v = v.trim();
      if (v) s.ans[i] = v; else delete s.ans[i];
      s.draft = "";
      s.open = go ? next(c.qs.length, s.ans, i) : -1;
      repaint();
      if (s.open >= 0) focusOpen();
      else if (document.activeElement && host.contains(document.activeElement)) document.activeElement.blur();
    };
    async function send(h) {
      const { c, s } = h;
      if (s.busy || !Object.keys(s.ans).length) return;
      s.busy = true; s.note = ""; repaint();
      const r = window.mCompose && window.mCompose.sendText ? await window.mCompose.sendText(c.id, message(s.ans)) : { ok: false, note: "not sent: the composer is not loaded" };
      s.busy = false;
      if (r.ok) s.sent = true; else s.note = r.note;
      repaint();
    }
    host.addEventListener("click", e => {
      const h = here(), li = e.target.closest && e.target.closest(".q-item");
      if (!h) return;
      if (e.target.closest("#q-send")) { send(h); return; }
      if (!li) return;
      const i = +li.dataset.i, box = li.querySelector(".q-box");
      if (e.target.closest(".q-head")) {
        if (h.s.open === i) commit(h, box.value, false);
        else {
          const cur = host.querySelector(".q-item.open .q-box");
          if (cur && cur.value.trim()) h.s.ans[h.s.open] = cur.value.trim();
          open(h.s, i);
        }
        return;
      }
      const chip = e.target.closest(".q-chip");
      if (chip) { commit(h, chip.dataset.v, true); return; }
      if (e.target.closest(".q-done")) { commit(h, box.value, true); return; }
      const mic = e.target.closest(".q-mic");
      if (mic) dictate(h, li, mic);
    });
    host.addEventListener("input", e => {
      if (!e.target.classList.contains("q-box")) return;
      const h = here();
      if (h) h.s.draft = e.target.value;
      grow(e.target);
    });
    // Not while a word is being composed, where Enter commits the word.
    host.addEventListener("keydown", e => {
      if (!e.target.classList.contains("q-box") || e.key !== "Enter" || e.shiftKey || e.isComposing || e.keyCode === 229) return;
      const h = here();
      if (!h) return;
      e.preventDefault();
      commit(h, e.target.value, true);
    });
    // The buttons never take focus, so a tap on one neither opens nor closes the soft keyboard.
    host.addEventListener("pointerdown", e => { if (e.target.closest && e.target.closest(".q-mic, .q-chip, .q-done")) e.preventDefault(); });
  }

  // The composer's two ways to dictate: hold to talk where the page may record, else a tap opens the phone's recorder.
  async function dictate(h, li, mic) {
    const v = window.atriumVoice, box = li.querySelector(".q-box");
    if (!v || mic.classList.contains("hot")) return;
    const put = text => { if (!text) return; box.value += (box.value && !/\s$/.test(box.value) ? " " : "") + text; h.s.draft = box.value; grow(box); box.focus({ preventScroll: true }); };
    if (!v.capturable()) {
      const rec = document.createElement("input");
      rec.type = "file"; rec.accept = "audio/*"; rec.setAttribute("capture", "");
      rec.addEventListener("change", async () => {
        const f = rec.files && rec.files[0];
        if (!f) return;
        mic.classList.add("hot");
        try { put(await v.fromFile(h.c.id, f)); } catch (err) { h.s.note = "dictation: " + err.message; } finally { mic.classList.remove("hot"); }
      });
      rec.click();
      return;
    }
    mic.classList.add("hot");
    try { await v.start(); } catch (err) { mic.classList.remove("hot"); h.s.note = "dictation: " + err.message; return; }
    mic.addEventListener("click", async function stop() {
      mic.removeEventListener("click", stop);
      try { put(await v.stop(h.c.id)); } catch (err) { h.s.note = "dictation: " + err.message; } finally { mic.classList.remove("hot"); }
    }, { once: true, capture: true });
  }

  window.mAnswers = { html, bind, yesNo, short, message, next };
})();
