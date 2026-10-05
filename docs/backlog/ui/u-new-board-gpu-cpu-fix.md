# u-new-board-gpu-cpu-fix. The board GPU and CPU fix

Status: HELD (pause). Filed by the orchestrator 2026-10-01, from clint, gap G14 of docs-deps. Owned by @ui.

## What is missing

The fix. The @ui CPU and GPU evaluation of the board (item H3) only measures and returns a ranked table. No item
holds the change clint will pick from it.

## Why it is needed

The board's load on a laptop or phone is the thing the measurement exists to reduce. The context bar's "no looping
animation" rule also comes from this.

## Depends on it

- `u-new-context-bar-on-rows`

## Done looks like

- The fix clint chooses from the table, built and measured again with the same method.
- The before and after numbers in the item.
- No new looping animation on rows, per the context bar rule.
- This item is a stub until the table exists. Fill in the chosen row when clint picks it.

## Measured 2026-10-01 by @ui (clint: "No ui build yet backlog")



1. The sound button nudge pulse inside the blurred sticky header, until the first click: GPU 18.5 and 60 fps, versus

   6.2 and 8 fps with the pulse off.

2. working.gif: about +4.8 GPU and +8.6 fps. A CSS-only replacement recovers only about 55% and costs more renderer CPU.

3. xterm while streaming: inherent.



Cheapest win: a still sound button (or a few pulse cycles) and an opaque header. Full table, scripts and raw results

are in the @ui atrium_report and the ui-director scratchpad on m1mini.

## Question for clint (@ui director, 2026-10-04)

Skipped tonight: this item builds the row you pick, and no pick is recorded. Which of these?

1. The cheapest win: the sound button stops pulsing (still, or three pulses then still) and the sticky header goes
   opaque (no backdrop blur). Measured GPU 18.5 to about 6, no visible loss. Recommended.
2. Option 1 plus working.gif swapped for a still spinner glyph (about +4.8 GPU back, but the card stops moving).
3. Something else from the table.

Answer here or to @ui and a worker builds it with the same measurement before and after.
