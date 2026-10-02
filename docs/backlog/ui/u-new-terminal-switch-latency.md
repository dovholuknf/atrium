# u-new-terminal-switch-latency: switching terminals is slow

clint (2026-10-02): "is there any way to fix the delay/latency when I change terminals?" First slice of
u-new-board-perf-workup.md, ahead of the rest. @fabric owns the hub proxy half.

## 1. Measure (before any fix)
One switch end to end on the live hub, for a claude-sg4 terminal (remote room, through the hub link proxy) and an
m1mini one: click, ws upgrade, room attach, scrollback bytes replayed, xterm write and fit, first paint. Put the
numbers in the Measurements section below.

## 2. Fix what the numbers point at
Candidates: keep the last N viewed and pinned terminals' sockets open and xterm instances alive (hidden, not
disposed) so a switch back is instant; paint a cached last screen at once then catch up; send the tail of scrollback
first and the rest lazily; prewarm on hover in the terminal list.
Check the hub roomHolding fan-out has not come back to gate the ws upgrade (fixed 3bf0f30).
Land through @review; report before and after numbers to the orchestrator.

## Measurements

### Local, m1mini room daemon (:7781, NOT the hub), 2026-10-02
Headless Chromium on m1mini, `node scripts/measure-term-switch.js` (standalone; takes the board URL). Times in ms from
`attachTask(id)`. Renderer: xterm's WebGL addon on software GL (headless), so paint cost here is NOT what clint's GPU sees.

| card | click to ws ctor | ctor to open | first byte | last replay frame | xterm parsed it | replay |
|---|---|---|---|---|---|---|
| fabric | 18 | 6 | 24 | 169 | 359 | 892 KB, 8154 lines |
| runtime | 19 | 5 | 24 | 70 | 395 | 551 KB |
| ui-director | 21 | 13 | 34 | 90 | 346 | 460 KB |
| rnd | 18 | 6 | 24 | 68 | 227 | 0.3 KB |

Rapid switching (12 cards x3, 0 ms and 200 ms apart): ctor to open 5-15 ms every time, no drift. The ws path is
`/v1/tasks/<bare uuid>/attach?link=<nonce>`: a uuid, no name, no `room~id`.

What it says (an earlier note here blamed xterm parsing; that came from a 150 ms quiet-wait inside the timer and is wrong):
- A bare xterm in the same page parses the 893 KB replay in 28-39 ms.
- A CPU profile of one switch to fabric shows about 40 ms of JavaScript in total. The rest of the ~350 ms is the
  browser's own work (layout, paint, GL), "(program)" in the profile, and it will be smaller on a real GPU.
- So locally the switch is: ~20 ms fetch, ~25 ms to the first byte, the replay in 50-170 ms, then native render.
  Nothing in the page's own JS is slow. What a REMOTE card adds is the link carrying that replay (up to ~0.9 MB) and
  whatever the hub does first.

### Through the hub (sg4), remote cards
Not measured from m1mini: the hub's board is loopback-only on sg4 (http://127.0.0.1:7778 there). Run on sg4:
`node measure-term-switch.js http://127.0.0.1:7778 --per-host 2 --rounds 3`. Results to go here.
