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

### Through the hub (sg4), run on sg4 by the orchestrator, live hub 127.0.0.1:7778
Cards: card-audit (sg4-control), discourse-6101 (claude-sg4), r-opencode-bubbles and r-restart-loopback (m1mini, through
the link). Medians, ms:

| | ctor (click to ws created) | c>o (ws created to open) | parsed |
|---|---|---|---|
| first switch | 99 | 32 | 201 |
| repeat switch | 94 | 52 | 200 |
| rapid | 91 | 33 | 188 |
| card-audit first (436 KB replay) | 325 | 44 | 619 (last frame 620) |

discourse-6101 returned 0 KB every time and no parse: not looked at yet (not an attachable terminal, or no replay).
Reading: the hub proxy and its pool are NOT the gap (c>o 30-55 ms for remote and local alike, rapid did not worsen,
repeat no faster so the card-lookup cache is not it either). The big chunk is `ctor`, 75-325 ms from click to the
socket being created, all board-side and over the hub: the GET /v1/tasks/<id> that `attachTask` awaits before
`openTerm`, which on a hub crosses to the owner room. Locally that fetch is ~3 ms, so locally ctor is ~20 ms.

### Fix 1: hover prewarm of that fetch (`prewarmCard`, `takePrewarmed` in terminal-list.js)
A mouse resting ~100 ms on a terminal row fetches its card (at most 2 in flight, cancelled if the pointer leaves
first, never on touch, NEVER a socket); `attachTask` takes the result if it is under 3 s old, in flight or done, once.
Measured with `scripts/measure-term-prewarm.js` on the live m1mini room, five cards, click to ws created, medians:

| | before | after (hover, then click) |
|---|---|---|
| card fetch as is (~3 ms) | 25 ms | 19 ms |
| card fetch +100 ms (stands in for a hub) | 127 ms | 19 ms |

So on a hub the fetch is taken off the click: ctor falls to the ~19 ms floor (one frame plus `openTerm`'s own work).
What is left before the ws is `openTerm` and a `requestAnimationFrame` so the pane can be fitted before the socket
dials; the size goes in the attach, so that wait is kept.

### Note: a kept terminal counts as watched (review M1)
A kept attach is a watcher on its room, so while the tab is open the auto new-context check for human cards never cycles
a kept card, a pinned card never cycles at all, and a kept shell is never idle-closed. Stated in the gear hint. Setting
"terminals kept alive" to 0 gives the old behaviour.

### Post-deploy run on sg4 (review M2)
Keep eight terminals on ONE remote room (switch through eight of its cards), then time an attach to a ninth card of that
room with `scripts/measure-term-switch.js` (the path/phase table shows whether it waited on the hub for a fresh conn).
Compare with the same attach on a cold page. That decides whether to cap kept sockets per remote room.
