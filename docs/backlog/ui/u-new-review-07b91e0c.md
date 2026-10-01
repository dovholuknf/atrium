# Review of 07b91e0c (@ui u-m-follow: the /m thread never pulls a reader back down)

Reviewed by @review, 2026-10-01, from `git diff 07b91e0c~1 07b91e0c -- internal/api/web`, read only. Hub side.
Board and headless checks are @ui's, and I ran none.

## What holds

- **The rule is enforced in one place.** Every scroll the page makes goes through `setTop`: `toEnd`, the typing pin,
  the pinch anchor, the redraw anchor, and opening a card. `setTop` marks a short window, so `onScroll` does not
  take the page's own move for the reader's. `toEnd` refuses unless following and idle, or forced. Only the jump
  arrow and the reader's own send force it.
- **Letting go is immediate. Taking hold again is deliberate.** `wheel`, `touchstart`, `touchmove`, `pointerdown`
  and `keydown` on the thread's scroller stop following before any scroll event. A scroll up past 24 px stops it
  too. Following resumes only within 2 px of the bottom with no finger down, or by the jump arrow. `byHand` is
  bound to `#m-card-scroll` only, and the composer is its sibling, so typing a message does not count as a touch.
- **Nothing moves under a finger.** `active()` covers a touch that is down, 300 ms after it lifts, and any reader
  scroll in the last 120 ms, which keeps iOS momentum covered. While active, `settle` and `paintReplies` wait and
  try again, and they never scroll. A redraw while reading finds the first visible bubble by `data-k` and moves the
  scroll by exactly what that bubble shifted. `overflow-anchor` is on only while not following, so the browser holds
  a reader still when an image above loads.
- **The recap gives back what it took.** It restores the reader's following state on close, rather than forcing
  the end. That is the one test expectation that changed, and it changed for the right reason.
- `data-k` is a djb2 hash of the time and text, put through `U.esc`, and it carries no new data.

## Findings

### Nit

1. **A tap at the bottom while output streams stops following.** Pressing a code-copy button or a link in the last
   bubble sends `pointerdown`, so `byHand` lets go. If a reply grows during the tap, the delayed `onScroll` then sees
   a gap of more than 2 px, and the reader is left above the new end with the arrow showing. That fails safe (no
   yank), but a reader who was at the end and only tapped could stay following. `byHand` could note `gapOf() <=
   AT_END` at the press, and `byHandEnd` could resume when the thread did not move during the touch.

Quality: after the Sonnet switch. The commit lists every scroll site, the test reproduces clint's case (a held,
dragged touch with output every 2 s) and fails on main, and the rule is enforced at one choke point rather than at
each site. Strong work. No drop seen.

HUB DEPLOY OK 07b91e0c
