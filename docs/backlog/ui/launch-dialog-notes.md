# Launch dialog notes (u-005, u-006)

## 1. The slow ROOM and RUNNER dropdowns: measurement, before any change

Script: `scripts/measure-launch-dropdowns.js` (headless Chromium, mocked hub, 120 cards across 5 rooms, 15 runner rows, the hub
streaming a change every 250ms). It records the Event Timing input delay of a press on the select (press to the page's handler
running), how late a 10ms timer runs on the idle thread, long tasks, and a CPU profile. A native select's list is not in the
page, so the time the page takes to answer the press is the part that can be measured, and it is what a busy thread adds to the
1 to 2 seconds. Run it as
`NODE_PATH=<@playwright/test> TERMS=n LINES=n VIEW=stack CHROME_ARGS="..." node scripts/measure-launch-dropdowns.js 120`.

| board | timer lateness p50 | + new agent to dialog showing p50 | press on ROOM, input delay p50 | on RUNNER p50 |
|---|---|---|---|---|
| 120 cards, no terminal open | 1 ms | 50 ms | 2 ms | 1 ms |
| terminals view, 1 terminal open, 3 lines per 40ms | 61 ms | 415 ms | 38 ms | 0 to 34 ms |
| terminals view, 1 terminal, 40 lines per 40ms | 61 ms | 403 ms | 37 ms | 33 ms |
| terminals view, 6 terminals kept, 40 lines per 40ms | 63 ms | 393 ms | 36 ms | 32 ms |
| 6 terminals kept streaming, board view showing | 1 ms | 58 ms | 2 ms | 1 ms |

Findings:

- The data is not slow and a full board's cards and rooms are not what holds the thread. With no terminal in view the thread is
  idle (timers 1 ms late) and the press is answered in 2 ms.
- What holds the thread is a visible terminal drawn by WebGL. One terminal at a trickle is enough: every task is held about 50 ms,
  so timers run 60 ms late whatever the output rate, and the dialog takes 400 ms to show. The CPU profile has 18 of 22 seconds in
  `(program)`, native work outside any script, with xterm's JS in the low hundreds of ms.
- Same board, same output, the renderer changed: `--disable-webgl` (xterm's DOM renderer) gives timers 1 ms late, dialog 61 ms,
  ROOM 7 ms. `--use-angle=d3d11` (a real GPU path) gives 1 ms, 52 ms and 2 ms. Default headless Chromium draws WebGL in software
  (SwiftShader), and that is the stall. So the headless number is the software path's, and on a machine with a working GPU the
  cost this shows is not there.
- Not reproduced: clint's 1 to 2 seconds on a real GPU. The shape matches (it appears when terminals are drawing, goes away when
  the board is showing instead), but a real-GPU machine with several WebGL contexts open (xterm keeps `KEEP_GL` contexts) may stall
  for a different reason that headless cannot show. The fix below does not depend on which.

Cause: a visible WebGL terminal keeps the page's frames expensive while a native select is opened over a modal dialog, so the
press is answered late and the list drops late. The terminals behind the dialog are covered by it and need no drawing at all.

Fix: while the launch dialog is open, drawing of the visible terminal is handed back to xterm's DOM renderer (the covered pane,
output still written), and the GPU renderer is taken again when the dialog closes.
