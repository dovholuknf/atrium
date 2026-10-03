# Review: u-m-bubbles f912861a

Range `7a9398f8..f912861a`, one commit on claude/u-m-bubbles. It changes `m/js/card.js`, `m/js/compose.js`,
`m/working.css` and the headless units, and adds the item doc `u-new-m-bubbles-copy-reply.md` with its Done
section. On /m:

- bubble text selects, and the thread is not redrawn under a selection;
- every bubble has copy and reply;
- reply is a markdown quote in the composer.

There are no Go changes.

Verdict: **OK** for hub and room. Four Lows can follow.

## How it was checked

- I read the card.js and compose.js diffs, every HTML sink in them, the CSS and the mBubbles unit. Per the standing
  note, I read the board units and did not run them.
- `node --check` passes on card.js, compose.js and test-board-headless.js.
- It merges onto claude/landing `12c369f2` cleanly. test-board-headless.js auto-merges.
- I looked at both screenshots (390, dark and paper): bubbles-dark.png and bubbles-paper.png. The actions sit quietly
  at the right of each header, "copied" reads as a state, and nothing scrolls sideways.

## Points

- **Escaping.**
  - The actions are a fixed string (`ACTS`).
  - `data-k` goes through `U.esc`.
  - The bubble's text is never put back into HTML. It is kept in a `Map` by key, and copy and reply read it from
    there, so the text reaches only `clipboard.writeText`, a textarea's `value` and the composer's `value`.
  - There is no inline handler: one delegated click listener on the thread reads `data-act`.
- **The source, not the markup.** Copy and reply take the text as written (markdown source, or a peer's text with the
  `[atrium] … says:` prefix taken off), not the rendered HTML.
- **The held redraw.**
  - `paintReplies` returns early while a non-empty selection is anchored in the thread, and sets `heldBySel`.
  - `selectionchange` draws it once the selection goes.
  - Closing the card clears the flag and the map, so a hold never carries across a card switch.
  - The unit checks both halves: a selection survives an arriving reply, and the reply lands when the selection is
    cleared.
- **The quote.**
  - CRLF and CR are normalised, and the text is trimmed.
  - Each line gets `> `, including blank ones.
  - It is cut at 300 with an ellipsis.
  - A blank line follows, the caret goes to the end, and an `input` event is dispatched.
  - A draft is kept above the quote. Empty text returns false and leaves the box alone.
- **The hit area.**
  - `min-width: 40px`, and `::after` with `inset: -13px 0` on a 15px button, so it is 41px tall.
  - The unit finds it with `elementFromPoint` 19px above and below the middle.
  - Contrast of 4.5 is asserted on both skins.

## Lows

- **L1: a selection left in place holds the thread indefinitely.** On a phone, a selection often stays after a copy
  from the native menu. Until it is cleared, new replies do not draw and the stick-to-bottom follow stops, and
  nothing says so.
  - Show the existing "new below" cue while `heldBySel` is set, or release the hold after a minute.
  - Or release it when the composer takes focus.
- **L2: the 300 cut can split a surrogate pair.** `t.slice(0, 300)` counts UTF-16 units, so an emoji at the boundary
  leaves a lone half before the ellipsis. Cut with `Array.from(t).slice(0, 300)`.
- **L3: the `execCommand` fallback is untested.** The unit stubs `navigator.clipboard`, and the path used on plain
  http is the fallback. If the range was in the thread and a held redraw fired during the copy, the restored range
  points at nodes that are gone. Add one case with `navigator.clipboard` undefined that checks the copied text and
  that no textarea is left.
- **L4: the hit area overlaps the text.** The 13px above and below the action reaches into the bubble's first line,
  so a long-press to select there lands on the button. If that shows up on a phone, use `inset: -13px 0 -6px` or put
  the actions on their own row.

Atrium-Verdict: hub-ok 7a9398f8..f912861a
Atrium-Verdict: room-ok 7a9398f8..f912861a
Quality: a careful fix. The real cause (an innerHTML redraw under a selection) is found and named, the bubble text
never goes back into markup, and the unit checks the hold and the quote shape, with mutants behind each assert.
