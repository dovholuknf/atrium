# Review: u-usage-chart 9f54d709

Range `fa5bb21b..9f54d709`, one commit on claude/u-usage-chart. It supersedes 3f3ab768, with the nits amended in.

It changes:

- `js/usage-charts.js`;
- `css/files.css`;
- one tip string in `js/usage.js`;
- the headless units, with the new `burnReadout` section;
- the item doc;
- `changelog/ui/2026-10-02-usage-chart-pace.md`. It is in the commit, as @ui asked.

The usage tab's cumulative chart is now direction C:

- a percent-of-limit axis, read from the status line's 5h reading;
- on the day ranges, the x axis zoomed to the 5h window;
- the even-pace diagonal;
- a rate strip on the same time axis;
- a pointer and touch crosshair, with a dot, a tag and a readout line;
- on the phone, a four-cell headline strip.

With no reading, the chart falls back to plain tokens with round, labelled ticks.

It merges onto landing cleanly. Only the headless file auto-merges.

Verdict: **OK** for hub and room. There are three Lows.

## How it was checked

- I read the diff of usage-charts.js, the CSS, the tip and the `burnReadout` section, including its contrast loop.
  Per the standing note, I read the board units and did not run them.
- `node --check usage-charts.js` passes on the merge.
- I looked at the screenshots real-paper-2000 and real-noir-390.

## Points

- **No limit is invented.**
  - The percent axis exists only when `ucLimitFor` returns something. That happens only for a group whose reset is
    still ahead, whose best reading has a percent above 0, and whose window atrium counted more than 0 tokens in.
    So `perPct` is never 0 or infinite.
  - A stale reading (reset passed), no reading, cache reads, the 1h range or a card filter all fall back to plain
    tokens.
  - The 100% line is the counted tokens divided by the reported percent. The heading and the new `cumPct` tip both
    say that spend atrium did not see is not in it.
- **Division and the numbers.**
  - The fallback ticks come from `ucNiceStep`, which returns 0 for no max or a max that is not finite. With 0 the
    scale is a top of 1 and no ticks are drawn.
  - The rate bars divide by a `maxTok` that is above 0 whenever any bar is drawn.
  - In `ucCumAt`, the ahead-of-pace figure divides by `reset - start`, which is the window's length.
  - Every number shown passes through `ucNum`, `ucPctNum` or `ucAheadText`, which print a dash or nothing when the
    value is not finite. The hover does nothing for a chart with no points.
- **Escaping.**
  - The readout and the tag are set with `textContent`.
  - The strip's HTML is `usageTokens` of a rounded number, `ulClock` wrapped in `esc()`, and the limit's name from
    the fixed `UC_LIMIT_NAME` table.
  - Attributes carry only numbers and the fixed reset kind.
  - The tips go through `esc()`.
  - There is no inline handler. Listeners sit on `document` and use `closest()`.
- **Scrolling.**
  - `.ucstage` is `touch-action: pan-y`, and no handler calls `preventDefault`. So a vertical swipe stays the page's,
    and `pointercancel` hides the crosshair.
  - A mouse's crosshair goes on `pointerout`. A finger's stays until the next touch elsewhere.
  - A test asserts the computed `touchAction`.
- **Cleanup.**
  - Every paint replaces the chart's nodes, which start hidden.
  - `UC.cum` is cleared when there is no series, so no crosshair reads a chart that is gone.
  - The live-limit redraw rebuilds the chart in place.
- **The 4.5:1 assertion is real.**
  - It reads every skin from themes.css and applies each one.
  - Under each skin, it measures the computed `color` of 14 text selectors against their composited backgrounds:
    the layers up the tree, with every gradient stop counted.
  - A missing element fails too.
- **Performance.** A pointer move walks the points and toggles the rate bars, about one per bucket, so at most a few
  hundred on 7d. That is fine without throttling.
- **Design.**
  - The paper 2000 shot reads at a glance: the gradient fill, the even-pace diagonal, the dashed projection to the
    red 100% line, the crossing mark, and the rate strip under one shared crosshair.
  - At 390 on noir, the four-cell headline strip sits above the plot, where a finger does not cover it.
  - It meets the board's bar.

## Lows

- **L1: the phone strip's text is not in the contrast check.** `.ucstat b`, `.ucstat small` and `.ucstat i` are the
  most-read text on the phone, and none of them is among the 14 selectors. Add them, with the strip shown.
- **L2: the reset label collides with the window's end line.** In real-paper-2000, "5h reset 17:30" sits on the
  dashed end line at the right edge, and the line cuts the word. Pad the label away from the line.
- **L3: the rate strip's first bars are before the window.** On the zoomed chart, the bars before `lim.start` are
  drawn as faint stubs, while the curve starts at the window. Skip bars that end before `lim.start`, or shade the
  lead-in, so the strip and the curve start together.

Atrium-Verdict: hub-ok fa5bb21b..9f54d709
Atrium-Verdict: room-ok fa5bb21b..9f54d709
Quality: an honest chart. It draws a limit only from what was reported, says what it counted, degrades to plain
tokens, and asserts its contrast on every skin from computed styles. m1mini commits are unsigned.
