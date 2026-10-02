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
2026-10-02, live hub http://127.0.0.1:7781 on m1mini, headless Chromium on the same box (so no network in it),
`node scripts/measure-term-switch.js` (`IDS=a,b` to pick cards). Times in ms from `attachTask(id)`; "drain" is when xterm
has parsed everything written (a `term.write("", cb)` after the last frame). Renderer in this run: the default (not WebGL).

| card (m1mini) | click to ws ctor | ctor to open | first byte | last frame | replay bytes | xterm drained | buffer lines |
|---|---|---|---|---|---|---|---|
| fabric | 22 | 6 | 28 | 141 | 884 KB | 416 | 8076 |
| runtime | 22 | 11 | 33 | 84 | 551 KB | 392 | 3914 |
| ui-director | 25 | 11 | 37 | 86 | 449 KB | 330 | 4339 |
| rnd | 22 | 6 | 28 | 75 | 0.3 KB | 230 | 34 |

(first column pair is the same cold-ish case each time: the first switch of a page load was 76/16/91/204 for fabric.)

Rapid sequences, 12 live cards, 3 rounds, 0 ms and 200 ms apart (32 and 36 sockets): ws ctor to open stayed 5-15 ms on
every socket, no jump, no drift. The ws URL is `/v1/tasks/<bare uuid>/attach?link=<nonce>`: a uuid, no name, and no
`room~id` tag (this hub has no tagged rooms: `/v1/rooms` is empty).

Not measured: **a claude-sg4 (remote) card.** `/v1/tasks` on this hub lists only m1mini cards, so there is no remote
card to click; the pool draw-down and card-lookup costs @fabric lists only exist for a remote room. Needs the sg4 room
attached, or @fabric's opt-in hub timing log.

What the local numbers say: the socket is not the cost (ctor to open under 15 ms, replay arrives in 50-140 ms). Of the
330-420 ms a switch takes on a big scrollback, 20 ms is the fetch before the socket, and the remaining ~250-300 ms is
xterm parsing the replay (about 2 MB/s into a fresh `Terminal` per switch, which `openTerm` builds every time). Time to
the first paint of a partial screen is earlier (first rAF at ~20 ms is the empty pane). So the lever is not the
transport: it is keeping the parsed terminal (hidden) so a switch back replays nothing, or sending the tail first.
