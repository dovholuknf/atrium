# Review of 2ddb2a4f (@ui u-m-pull: pull to refresh on /m)

Reviewed by @review, 2026-10-01, from `git diff 2ddb2a4f~1 2ddb2a4f -- internal/api/web`, read only. Hub side. Board
and headless checks are @ui's, and I ran none.

## What holds

- **The home list gets the browser's own pull back.** `overscroll-behavior-y` is `auto` on `body.m` and `none` only
  while a sheet is open, where the document does not scroll.
- **The card's pull is narrow.** It needs one finger that went down at `scrollTop` 0, and it is refused while older
  entries are loading, while the recap or the picker is open, or while typing. It reloads only past 80 px with the
  thread still at the top. It never sets `scrollTop`, so it cannot meet the follow rules. `preventDefault` runs only
  while dragging down from the top, and the indicator has no pointer events.
- **Drafts survive the reload.** The composer keeps a draft per card in localStorage, cleared only by an accepted
  send.

## Findings

### Low

1. **A pull during a send in flight can lose the message.** The composer frees the box and clears the draft as soon
   as a send starts (compose.js, "a send frees the box at once"). If the page reloads before the POST answers, a
   failed send has nowhere to put its text back, and whether a successful one landed is not shown. The `flight` map
   in card.js already knows about pending sends. Refusing the pull while any entry for this card is `pending`, and
   while an upload chip is in flight, would close it.

Quality: after the Sonnet switch. The gates are thought through, and the fix to the home list's native pull came
from finding the cause (`overscroll-behavior-y: none`), not from adding a second mechanism. The in-flight send is
the one state not gated.

HUB DEPLOY OK 2ddb2a4f

## Re-read of dff5146f (@ui, low 1)

`git diff 2ddb2a4f dff5146f -- internal/api/web`, read only. `mCompose.busy()` reads the mounted composer's own
`sending` and `uploading` counters (compose.js increments and decrements them around the POST and the upload). The
pull is refused at touchstart, at touchmove and at release when that is true, or when this card has a `pending`
entry in `flight`, so no spinner starts and no reload follows. Low 1 is closed. No findings.

Quality: after the Sonnet switch. It uses the state the composer already kept, and the test asserts the thread is
at the top first. No drop seen.

HUB DEPLOY OK dff5146f
