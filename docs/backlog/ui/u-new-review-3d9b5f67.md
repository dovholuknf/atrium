# Review of 3d9b5f67 (@ui u-m-typefix: a 1px layout nudge no longer stops following)

Reviewed by @review, 2026-10-01, from `git diff 3d9b5f67~1 3d9b5f67 -- internal/api/web`, read only, against the
follow rules of 07b91e0c (`u-new-review-07b91e0c.md`). Hub side. Board and headless checks are @ui's, and I ran none.

## What holds

- **A reader's touch still stops following at once.** `byHand` is unchanged. Touchstart, pointerdown, wheel and
  key on the thread's scroller set `stick = false` before any scroll event, so the new reader gate in `onScroll`
  is not the only way to let go, and not the main one.
- **A slow drag up in 1 px steps cannot escape.** A drag begins with a touchstart, so following is already off.
  While the finger is down `touching` is true, and the only path back to following (`gap <= AT_END && !touching`)
  is closed. `moved < -1` and `reader` only decide whether a scroll with no touch behind it lets go, which is the
  layout nudge the commit is about. A mouse on the scrollbar sends `pointerdown`, and keys reach the scroller only
  when focus is inside it, where `byHand` hears them.
- **The page's own scroll is also recognised by where it left the thread** (`progTop`), and only while no reader
  input came after it (`userAt <= progAt`), so a slow machine's late event is not taken for the reader's.
- **ResizeObserver and image load go through `settle()`,** which retries after the active window instead of
  skipping, and which still respects the typing pin and `stick`.
- @ui reproduced the race under a 6x CPU throttle (6 of 12 failing) and shows 12 of 12 passing with the fix.

## Note for the landing

This branch was cut before load older, pull and the switcher. Load older also edits `onScroll` (it calls
`nearTop()` when `scrollTop < 40`), and pull reads `touching` as well. The merge has to keep both. I will check the
landed `onScroll` against this review and against 22157cc3.

Quality: after the Sonnet switch. A flake was chased to a real product race under a reproducible throttle, and the
fix narrows a rule rather than adding a delay. No drop seen.

HUB DEPLOY OK 3d9b5f67
