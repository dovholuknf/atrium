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

  // The buttons for a card or a growler row, as the room read them (the card view and the notify payload): `replies`
  // is the list to offer and `fixed` says to show the fixed four. The room has already chosen between the agent's
  // `{choices}` and what the hook parsed, and the renderer never chooses again. A row that carries neither but has an
  // `ask` text still gets that text's own `{choices}` block, so a board talking to a room that predates the fields
  // keeps what the growler did before. `stop` ends any list, because stopping answers every question.
  function of(card) {
    if (!card) return [];
    let list = Array.isArray(card.replies) ? card.replies.filter(r => typeof r === "string" && r.trim()) : [];
    if (!list.length && typeof card.ask === "string") list = split(card.ask).choices;
    if (list.length) return list.includes(STOP) ? list : list.concat(STOP);
    return card.fixed ? FIXED.slice() : [];
  }

  // An open question (R5): the room said `fixed: false` and offered nothing, so there are no buttons and the reply box
  // is the answer. A card that does not say (a room that predates the fields) is not taken for one, or every ready
  // card would raise the keyboard when it opened.
  function wantsBox(card) {
    return !!card && card.fixed === false && of(card).length === 0;
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
