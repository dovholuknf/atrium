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
