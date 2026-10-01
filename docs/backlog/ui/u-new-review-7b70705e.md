# Review of u-m-typefix 7b70705e at 24b43091 (@ui: touch state follows touch events only)

Reviewed by @review, 2026-10-01, from the board half of merge 7b70705e (`git show 7b70705e`, the combined diff) and
the tip 24b43091. Read only, and fast, as board landings are. Everything after 7b70705e is merges of claude/main
that touch no file under `internal/api/web`. 3d9b5f67 itself was reviewed OK at 7550c02a.

## The landed onScroll, byHand and byHandEnd (the queue's check)

At 24b43091, `internal/api/web/m/js/card.js`:

- 3d9b5f67's reader gate is there. `mine` uses `progTop` (line 518: within 1px of the last top the page set, after
  the page's own move). Only `moved < -1 && reader` lets go of the end (line 529).
- Load older's `nearTop()` is there, at `scrollTop < 40` (line 523).
- Pull's gates are there and unchanged: `pullBlocked()` (`olderBusy() || sending()`) on touchstart, touchmove and
  touchend, plus the sheet, menu and typing checks on touchstart.

## The fix

`touching` is now set by `touchstart`, and by a `pointerdown` that is not a touch (line 502). `byHandEnd` ignores a
touch pointer's events, and ignores a `touchcancel` that leaves fingers down. `pointercancel` is no longer listened
for (line 949). That is the right root cause: once the browser takes a touch drag over for scrolling, it cancels the
touch's pointer while the finger is still down, and the old code then let `touching` fall to false mid-drag.

The test changes (0.5px drag steps in mFollow, a wheel event before `scrollTop = 0` in mPull so that the move reads
as the reader's) are test-side only.

## Findings

### Low

1. **A pen that scrolls can leave `touching` stuck at true.** Pen and mouse still set `touching` on `pointerdown`,
   but `pointercancel` is no longer a listener. A pen dragged on a touch screen (a Surface pen, an Apple Pencil) is
   handed to scrolling the same way a finger is. It gets `pointercancel` and no `pointerup`. `touching` then stays
   true, and `gap <= AT_END && !touching` never sticks to the end again until some later `pointerup` or `touchend`
   on the thread. The fix is to listen for `pointercancel` again. `byHandEnd` already returns early for
   `pointerType === "touch"`, so only pen and mouse cancels get through, and those do end the gesture.

### Nit

2. `byHandEnd` keeps `touching` for a `touchcancel` that leaves fingers down, but not for a `touchend` that does:
   lift one finger of two and `touching` drops while the other is still on the glass. Use the same `e.touches.length`
   check for both.

Quality: after the Sonnet switch. The flake was traced to its root cause, the browser cancelling a touch's pointer
mid-scroll, and not papered over with a wait. The miss is the pen, which goes down the same cancel path. No drop.

HUB DEPLOY OK 3d9b5f67~1..24b43091. Low 1 can follow as a one-line change.

## Re-read: m1mini/claude/u-m-typefix2 10aae191 (off e5a75826)

One commit, card.js and the headless suite only.

- Low 1 is closed. `pointercancel` is a `byHandEnd` listener again. A touch pointer's cancel still returns early on
  `pointerType === "touch"`, so the touch events keep owning finger gestures, and only pen and mouse cancels clear
  `touching`. Those are the two kinds `byHand` sets it for on `pointerdown`, so set and clear now match.
- Nit 2 is closed. `touchend` and `touchcancel` both keep `touching` while `e.touches.length` is non-zero.
- The two new mFollow cases (two-finger touchend keeps the thread held, pen pointerdown then pointercancel releases
  it and sticks to the bottom) test exactly these paths. Read, not run: board tests are @ui's.

No new findings.

Quality: after the Sonnet switch. Both items fixed as specified, each with a test that fails on the old code. No drop.

HUB DEPLOY OK and ROOM DEPLOY OK 10aae191~1..10aae191.
