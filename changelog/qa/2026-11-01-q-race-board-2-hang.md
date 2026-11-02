# race-board-2 hang: a board unit that never returns now fails after 5 minutes

The ubuntu `race-board-2` job hung at the 30 minute limit (run 41364964371) and for 5 hours (run 41328304018).
A shard of the board suite had no bound per unit, so one unit whose wait never completes held its shard, and the
job, until the runner killed it.

`unit()` in `scripts/test-board-headless.js` now races each unit against `HEADLESS_UNIT_MS` (default 300000, six
times the slowest unit on the weight file). A unit past it fails with "no result after 300s, a wait that never
completes", the shard goes on, and `test-board-sharded.js` retries it alone like any failed unit.
