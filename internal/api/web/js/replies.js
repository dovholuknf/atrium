// ── quick replies, one renderer ──────────────────────────────────────────────────────────────────────────────────
//
// What a card or a growler offers as one-press answers, drawn the same on the board's growler, the phone board's
// growler, /m's growler and /m's compose. See docs/rnd/reply-suggestions-design.md, section 4.
//
// A plain script with no dependencies, loaded by index.html and m/index.html before the growlers. It declares one
// global, `replies`, and touches nothing else.
//
//   replies.split(text)             {text, choices}: a `{choices}...{/choices}` block taken out of the text
//   replies.of(card)                the button list for a card or a growler row, `stop` last, or [] for none
//   replies.buttons(list, opts)     one wrapping row of buttons, `rq-row` holding `rq-btn`
//
// A press sends the option's FULL words, never a number and never the cut label, so it stands on its own after the
// agent has compacted. The label is only what fits.
const replies = (function () {
  "use strict";

  const FIXED = ["yes", "go ahead", "no", "stop"];
  const STOP = "stop";
  const NARROW = 28, WIDE = 60;
  const BLOCK = /\{choices\}([\s\S]*?)\{\/choices\}/;

  function split(text) {
    const s = String(text == null ? "" : text);
    const m = BLOCK.exec(s);
    if (!m) return { text: s, choices: [] };
    const choices = m[1].split("\n").map(l => l.trim()).filter(Boolean);
    return { text: (s.slice(0, m.index) + s.slice(m.index + m[0].length)).trim(), choices };
  }

  // The buttons for a card or a growler row. Its `ask` text may carry a `{choices}` block, which is the agent's own
  // options, and those win. Else `fixed` says the board should show the fixed four. `stop` ends any list, because
  // stopping answers every question.
  function of(card) {
    if (!card) return [];
    const list = typeof card.ask === "string" ? split(card.ask).choices : [];
    if (list.length) return list.includes(STOP) ? list : list.concat(STOP);
    return card.fixed ? FIXED.slice() : [];
  }

  // What the card is waiting on is an open question: no buttons, and the reply box is the answer.
  function wantsBox(card) {
    return !!card && of(card).length === 0;
  }

  // The words that fit. Narrow: the first clause, then a word boundary and `...`. Wide: only the cut.
  function label(opt, narrow) {
    let s = String(opt).replace(/\s+/g, " ").trim();
    const max = narrow ? NARROW : WIDE;
    if (narrow) {
      let at = s.length;
      for (const sep of [",", ":", " - ", " so ", " to ", " because "]) {
        const i = s.indexOf(sep);
        if (i > 0 && i < at) at = i;
      }
      s = s.slice(0, at).trim();
    }
    if (s.length <= max) return s;
    const room = max - 3;
    let cut = s.lastIndexOf(" ", room);
    if (cut < Math.floor(room / 2)) cut = room;
    return s.slice(0, cut).replace(/[\s,;:.-]+$/, "") + "...";
  }

  function isNarrow() {
    try { return window.matchMedia("(max-width: 600px)").matches; } catch (e) { return false; }
  }

  // opts.narrow   cut labels for a phone. Defaults to the viewport.
  // opts.cls      more classes for the row, for the surface's own layout.
  // opts.disabled every button disabled, for a question already answered.
  // The full words are on `data-choice` and `title`. The caller listens, this binds nothing.
  function buttons(list, opts) {
    opts = opts || {};
    const narrow = opts.narrow == null ? isNarrow() : !!opts.narrow;
    const row = document.createElement("div");
    row.className = "rq-row" + (opts.cls ? " " + opts.cls : "");
    for (const o of list) {
      const b = document.createElement("button");
      b.type = "button";
      b.className = "rq-btn";
      b.dataset.choice = o;
      b.title = o;
      b.textContent = label(o, narrow);
      if (opts.disabled) b.disabled = true;
      row.appendChild(b);
    }
    return row;
  }

  return { FIXED, split, of, wantsBox, label, buttons };
})();
// compose.js reads it off `window`, and a top level `const` is not a property of it.
window.replies = replies;
