# Lows from review: /m bubbles (f912861a) and usage chart (9f54d709)

Both landed OK on 3350384e. Small items, done together with the mobile CSS pass (u-new-mobile-css-pass-1002.md).

## /m bubbles (review u-new-review-f912861a.md)

- L1: a selection left after a native copy holds the thread's redraw indefinitely, with no cue. Show a "new below" cue,
  release after a minute, or release on composer focus (pick one, say which).
- L2: the 300-character cut can split a surrogate pair. Cut with Array.from.
- L3: the execCommand fallback of copy is untested. Add a headless case.
- L4: the 41px hit area of copy/reply reaches into the bubble's first line. Keep it 40px or move it off the text.

## Usage chart (review u-new-review-9f54d709.md)

- L1: the phone strip's .ucstat text is not in the 4.5:1 per-skin contrast loop. Add it.
- L2: "5h reset 17:30" is cut by the dashed end line at 2000px.
- L3: the rate bars before the window start show as stubs before the curve starts. Hide them.

Each fix gets an assert and, for the shape ones, a mutant.

## Done

- Bubbles L1: release on composer focus (the browser moves the selection into the box and selectionchange draws the held
  reply) plus a one-minute cap (`HOLD_MAX`), which clears the selection and draws. No cue was added: the minute is short
  and the cap needs nothing the reader must see. Asserts: both releases, and nothing drawn at 30s. Mutant: no timer fails.
  The focus path is the browser's own selection move, so no separate handler exists to mutate.
- Bubbles L2: the quote cuts with `Array.from`. Assert on an emoji at the boundary; mutants on both cut forms fail.
- Bubbles L3: new case with `navigator.clipboard` undefined: copied text, "copied" feedback, no stray textarea. Mutant
  (textarea left in) fails.
- Bubbles L4: the hit area is 40px grown upward (`inset: -21px 0 -4px`), 4px below the button instead of 13. Assert: 40px
  reachable, at most 5px under the button. Mutants at -13 and -10 fail.
- Chart L1: `.ucstat b`, `b small` and `i` are in the per-skin contrast loop. Mutant (dim colour on `i`) fails on every skin.
- Chart L2: the end label stood 4px from the line in the 2000px headless draw (no overlap there); it now stands 8px clear,
  asserted at 2000px. Mutant at 4px fails.
- Chart L3: rate buckets that end before the limit window starts are not drawn, nor counted in the strip's scale.
  Assert at 2000px; mutant fails.
