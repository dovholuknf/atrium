# Usage tab, cumulative tokens chart: axis, percent, hover readout

From clint, 2026-10-02, by the orchestrator, on the usage tab's "cumulative tokens" chart (a screenshot on sg4, not
readable from m1mini). He likes the graph but wants a percentage on it, a labelled Y axis, and the values shown
wherever the pointer is on the chart.

State: filed, not started. Batched after keep-alive and the context-bar badge (u-new-ctx-bar-land-the-plane.md).
Lands through @review. Visual work: clint's bar is the repos redesign (memory ui-design-bar), so show directions as
screenshot file paths first, at 2000px and phone, paper and dark.

## Asks

1. **Y axis.** A labelled axis with ticks (0, 5M, 10M, 15M, a "tokens counted" label), not only the "0" and "15M" in
   the corners. Pick ticks that suit whatever the window's maximum is.
2. **Percentage.** A second scale or readout as a percent of the plan limit, the same numbers as the status line's
   "5h 12%" and "wk 79%". Show where the line sits against 100% of the window being charted, and what the
   "17M by midnight" pace means as a percent. Find where the status line gets its percent and read it from the same
   place; if the board has no such number, say so and ask @fabric/@runtime rather than inventing a limit.
3. **Hover crosshair.** Wherever the pointer is on the chart: a vertical line plus a readout at the cursor with the
   time, the cumulative tokens, the percent of the limit, and the rate in that hour. The existing "Thu 19:07 · 8k
   tokens" line at the bottom-left follows the cursor as a tooltip instead. Touch-drag does the same on the phone.

## Checks

- Headless section for the readout (hover at x gives the right bucket, edge x clamps, touch-drag), touched sections
  plus bootClean each alone.
- Dark and paper skins, 390px, and a window with no data (readout hidden, no NaN).
