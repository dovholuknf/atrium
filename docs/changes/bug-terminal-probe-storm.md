# bug-terminal-probe-storm

Hovering the terminal sent about a hundred `files/probe` requests. xterm asks `provideFileLinks` per row on every hover,
and a miss was cached for only `probeMissTtl` = 3000 ms, so moving across the terminal re-probed every row every 3 s.
Most words are misses.

The rule now: a row probes at most once per change to its own text, however much the pointer moves and whatever happens
on other rows. The 3 s TTL is gone. A miss is keyed by token and the text of its row (`probeMissKey` in
`js/terminal-links.js`), a hit by token for the card's life. A spinner redrawing one row touches no other row's key, so
it causes no probes. A path hovered before the agent wrote it becomes a link once its row's text changes, because the
row is then a new key. A row left exactly as it was stays plain, which is the cost of asking nothing. The cache starts
over past 20000 entries.

## Before and after

Unit `termProbeStorm`: 40 rows of mostly-miss text, the pointer swept over the rows repeatedly for 10 s after a first
pass, with a spinner redrawing one other row once a second.

| | First pass | Sweeping 10 s, spinner running |
| --- | --- | --- |
| Before (3 s miss TTL) | 33 | 99 |
| After (row text) | 32 | 0 |

Rewriting one swept row then costs exactly one probe, and a further sweep asks nothing.

## Test plan

## @LETTER@. Terminal probe storm

### @LETTER@1. An unchanged terminal is not probed

1. Run `HEADLESS_UNITS=termProbeStorm,termLinkLate node scripts/test-board-headless.js`.
2. Both units print ok. `PROBE_COUNTS=1` prints the counts above.
3. By hand: open a card's terminal with devtools on the network tab, fill it with text, sweep the pointer over it. The
   first pass probes, later sweeps send no `files/probe` requests, even while a spinner redraws.
