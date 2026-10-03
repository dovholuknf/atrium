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
