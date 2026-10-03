# Review: u-mobile-pass 147e7c70, with the ui queue docs to f0e3ee33

`147e7c70` on claude/u-mobile-pass sits on claude/ui-director `f0e3ee33`, which is landing `3350384e` plus docs-only
queue commits. Range `3350384e..147e7c70`. It also carries @fabric's flake merge 6405a0d9, which is already landed.

Verdict: **OK** for hub and room, and doc-ok for the queue docs. There are two Lows.

## Points

- **The mobile pass.** Under 900px the repos ledger strip snaps, with `scroll-snap-type: x proximity` and the tiles
  `start`, and fades its right edge with a mask, so a repo is not cut mid-word. In after-repos-ledger-390-dark.png
  the second tile fades out as "more". A test asserts it, with a mutant.
- **Keep-alive is off on the phone.** That is checked here, since the worker could not find it. The gate is in
  `terminal.js`, not `keepalive.js`:
  - `keepOn()` (line 645) is false under `termNarrow()` or `termPhone()`;
  - a `matchMedia("(max-width: 900px)")` listener (line 618) runs `keepEnforce()`, so narrowing lets go of what is
    kept at once.
- **The seven Lows from f912861a and 9f54d709:**
  - **Bubbles L1.** The hold ends when the composer takes focus, through `selectionchange`, or after `HOLD_MAX` (60 s)
    in `releaseHold`. Closing the card clears the timer.
  - **Bubbles L2.** The quote cuts by `Array.from` code points.
  - **Bubbles L3.** The `execCommand` fallback has a test.
  - **Bubbles L4.** The hit area is `inset: -21px 0 -4px`, still 40px tall, grown upward.
  - **Chart L1.** `.ucstat` is in the contrast loop.
  - **Chart L2.** The reset label has 8px of padding, with an assert.
  - **Chart L3.** Buckets before `lim.start` are not drawn, and `maxTok` skips them too.
- I read the units and did not run them, per the standing note. The queue docs have no quotes or paths.

## Lows

- **L1: the one-minute cap clears a selection the reader may still be using.** `releaseHold` calls
  `removeAllRanges()` while someone may be mid-selection, for example a long, slow drag across several bubbles. Only
  clear it when the selection has not changed for the minute: reset the timer in `selectionchange` while the hold is
  on.
- **L2: the hit area now reaches 21px up.** On a bubble whose header follows the previous bubble closely, that can
  cover the previous bubble's last line, so a long-press there taps copy. Check the gap between bubbles at 360. If it
  is under 21px, split the growth, for example `-16px 0 -9px`.

Atrium-Verdict: room-ok 3350384e..147e7c70
Atrium-Verdict: hub-ok 3350384e..147e7c70
Atrium-Verdict: doc-ok 3350384e..f0e3ee33
Quality: a tidy pass. It fixes the one broken layout, closes all seven Lows, and finds the phone's keep-alive gate.
m1mini commits are unsigned.
