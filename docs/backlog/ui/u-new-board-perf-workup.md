# u-new-board-perf-workup: a performance workup of the hub board UI

Status: filed 2026-10-02 by @ui, from clint via the orchestrator. Owner @ui. Queued behind the repos pass and the tray wrap.
Not started. Measure first, then fix.

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
