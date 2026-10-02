# Review: measure-term-switch kept rows and --pool 7486e0e5

Range `a51c45fe..7486e0e5`, one commit, in `scripts/measure-term-switch.js` only. It is a measuring script, standalone,
and not run by the board or the suite.

Verdict: **OK.** Two Lows can follow.

- **Kept rows.**
  - A switch with no ws whose marks hold `keep-show` is now a kept row. It is timed to `keep-end` and to two
    animation frames, and the ws columns show `-`.
  - The wait loop leaves as soon as `keep-end` is marked, and an older board still prints "no ws".
  - The medians for first, repeat and rapid leave the kept rows out, and kept rows get their own median.
- **--pool.**
  - It raises the kept setting through the board's own `setTermKeep`, in a fresh Playwright context, so nobody's
    real setting changes.
  - It attaches K cards of one room, then times card K+1.
  - It refuses with too few cards, and on a board that predates keep-alive.
  - It prints the ws the tab holds open, which is the number to read beside the verdict.
- `sw` is a function declaration, so it is hoisted, and the pool block can call it before line 131. It uses only
  `page` and `hover`, so nothing it needs is still in its temporal dead zone (declared but not yet initialised) at
  that point.
- `node --check` passes.

## Lows

- **L1: the pool takes any card that is not done or backlog.**
  - A joined, parked or unsupervised card opens no ws, or opens the join dialog. So the K attaches can hold fewer than
    K sockets, and the run says "no slowdown" for the wrong reason.
  - For `--pool`, filter to `supervised && !parked_at`. The "ws this tab holds open" line will show it either way.
- **L2: the verdict compares K+1 with attach 1.**
  - Attach 1 is the coldest: it is the board's first, and it misses the hub's card cache.
  - Compare against the median of attaches 2 to 4, which are still inside the warm pool, so a cold first attach
    cannot hide a slowdown.

Atrium-Verdict: room-ok a51c45fe..7486e0e5
Atrium-Verdict: hub-ok a51c45fe..7486e0e5
