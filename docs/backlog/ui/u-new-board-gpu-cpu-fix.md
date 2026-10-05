# u-new-board-gpu-cpu-fix. The board GPU and CPU fix

Status: BUILT 2026-10-05, on branch claude/u-new-board-gpu-cpu-fix, not merged. Filed by the orchestrator 2026-10-01, from clint, gap G14 of docs-deps. Owned by @ui.

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

## Decision (@ui director, 2026-10-05)

Option 1, the cheapest win. The sound button pulses three times and then holds still until the first click, and the
sticky header is opaque with no backdrop blur and a solid `--bg1-rgb` background that reads the same on every skin.
working.gif stays as it is. Option 2 is not taken: the moving card is worth its cost. This takes the board's idle
GPU from the pulse's cost back to the floor for the least visible change.

## Built

- `css/dialogs.css`: the `nudge` animation runs `3` times, not `infinite`.
- `css/chrome.css` and the website skin in `css/themes.css`: the header has `background: rgb(var(--bg1-rgb))` and no
  `backdrop-filter`.
- `scripts/check-header-still.js`: headless check that the header has no `backdrop-filter` and a solid background on
  the default, daylight and website skins, and that the blocked sound button's nudge runs 3 iterations and ends.
- No `infinite` was added. The loops that remain in `chrome.css` and elsewhere were there before and are not touched.

## Measured again, same method

The method is the earlier one: a preview board with 30 cards, 10 of them live, 10 second windows, 3 runs each, headless
Chrome on the m1mini GPU. "Pulse showing" is the board with no terminal attached and the sound button still blocked.
"Terminal streaming" is the board with a terminal attached to a streaming card. Values are the mean of 3.

| Scenario | | GPU % | fps | renderer CPU % |
| --- | --- | --- | --- | --- |
| Pulse showing, no terminal | before | 10.5 | 60.0 | 2.3 |
| Pulse showing, no terminal | after | 2.8 | 8.3 | 0.9 |
| Terminal streaming | before | 5.4 | 26.8 | 3.4 |
| Terminal streaming | after | 5.3 | 26.8 | 3.9 |

The first row is the fix. The no-terminal GPU swings between runs (8.5 to 13.2 before, the first measurement in the
earlier run read 18.5) but the 60 fps compositor is the constant, and it is gone. The streaming row is xterm and
working.gif and does not move, as the earlier table said it would not.

Screens, before and after, on abyss (dark), daylight (light) and website (dark, the skin with its own blur), are in
`docs/screens/u-new-board-gpu-cpu-fix/`.
