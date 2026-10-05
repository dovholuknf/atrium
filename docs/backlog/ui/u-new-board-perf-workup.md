# u-new-board-perf-workup: a performance workup of the hub board UI

Status: measured 2026-10-05 by a worker. Harness `scripts/perf-board.js`, findings `docs/ui/board-perf-findings.md`,
numbers `docs/ui/board-perf/`. No fix built here: the one clear win is `u-new-board-gpu-cpu-fix` (1a84e706), measured
against main in the findings. Proposals for clint are in the findings, the largest being to stop the waiting `pulse`
dots after a few pulses (stack view idle GPU 14% to under 1% with them off). The long memory run was shortened to 20
minutes with a growth rate per hour (heap flat). Not measured: `/m`, remote (zrok) load, hub side caching (@fabric),
rooms event volume (@runtime). Performance panel traces were not saved, the harness reads animations and frames.
Owner @ui. Next: clint picks from the proposals, @fabric looks at asset caching.

## Scope

The board as served by the hub (stack, board, terminals, history, usage, rooms, repos), desktop first, /m after.

- Time to first paint and time to interactive, cold and with the service worker warm.
- CPU and GPU while idle with many cards (a hundred or more rows, several attached terminals). `u-new-board-gpu-cpu-fix`
  (the sound button nudge pulse in the blurred header, then working.gif) folds in here if it exists as a note; clint picks
  the trade-offs.
- SSE update cost: what a burst of card events costs in script, style and layout, and whether rows repaint whole lists.
- Attach and xterm cost: time from click to first byte painted, WebGL against the DOM renderer, many tabs open.
- Memory over hours: heap and DOM node counts after a long session, listeners, timers, retained terminals and toast logs.

## Method

1. Rerunnable measurements first: Playwright timings in `scripts/` (first paint, interactive, N-card idle CPU sample,
   SSE burst, attach, heap and node counts at t=0, 1 h, 4 h), output to a table that can be diffed run to run.
2. Performance panel traces for the worst offenders, saved with the findings.
3. Findings doc with numbers before any fix. Then fixes by size of win, each measured before and after, each through
   @review.

## Split

- @fabric: hub server side (time to first byte, asset sizes and caching, SSE fan-out).
- @runtime: the rooms (event volume per card, payload size).
- @ui: everything in the browser.

Related: `docs/backlog/ui/u-new-suite-flakes-0930.md` (the headless suite runs without software WebGL: u-suite-webgl).
