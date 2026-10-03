# /m chat bubbles: selectable text, copy button, reply

From clint, 2026-10-02, by the orchestrator. Queued ahead of the mobile CSS pass (u-new-mobile-css-pass-1002.md).
Lands through @review.

## Asks

1. **Select and copy.** Text in a bubble on the /m card view must be selectable and copyable. Today it is not,
   probably a user-select rule or a tap/long-press handler eating the selection. Long-press selects natively (no
   preventDefault on it, no callout suppression), and each bubble gets a small copy button.
2. **Reply.** A "reply" action on a bubble puts the bubble's text into the composer as a markdown quote: "> " on
   each line, cut to about 300 characters with an ellipsis, then a blank line, with the caret after it. The user edits
   and sends. No conversation id is involved: the runner sees only text, so the quote is the whole mechanism.

## Checks

- Headless on the phone layout: text is selectable (computed user-select, a programmatic selection survives a tap),
  copy button writes the bubble text, reply fills the composer (quote prefix on every line, cut at about 300 with an
  ellipsis, blank line, caret at the end, an existing draft is kept), no scroll or follow-mode regression.
- Dark and paper, 390px.

## Done

- **Why it would not hold a selection.** No `user-select` or callout rule was in the way on /m. The thread was redrawn
  (`innerHTML`) whenever the replies changed and nobody's finger was down, which replaces every node and drops a selection
  made after the finger lifted. `paintReplies` now holds the redraw while a selection sits inside the thread and draws it
  when the selection goes (`selectionchange`). Bubble text also says `user-select: text` outright.
- **Copy and reply** are two small actions at the end of each bubble's header line (replies, own messages, peer and command
  prompts). 15px high on screen, a 40px hit area from `::after`, 40px wide, not part of a selection. Copy writes the bubble's
  source text (the markdown as written) through the clipboard API, with a textarea and `execCommand` fallback, and says
  "copied" for 1.4s.
- **Reply** is `mCompose.quote(text)`: `> ` on every line, cut at 300 characters with an ellipsis, a blank line, caret at
  the end. An existing draft stays above the quote (draft, blank line, quote), so the caret is under what is quoted. Empty
  text does nothing.
- Test: `HEADLESS_ONLY=mBubbles` (390px, dark and paper). Mutant-checked: the redraw guard, the `> ` prefix, the 300 cut, the
  ellipsis, draft order, caret place, the copied text, user-select and the hit area each turn it red.
