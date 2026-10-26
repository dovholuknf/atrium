# Does the 1h cache pin pay for launched workers

Item f-worker-cache-ttl-measure. Status: measured, one change filed.

## Answer

In aggregate yes, by 2.2 times, but the gain sits in a few long workers. Of 446 launched workers, 30 had five turns or
more. Those 30 paid $23.90 of premium and avoided $124.09 of rewrites. The other 416 paid $33.54 and avoided $2.14.
Taken together the pin cost $57.44 and avoided $126.22 on this machine, so the default should stay on for workers that
live long and the short ones are where the money leaks.

## Method

Read-only. The room database was copied with `sqlite3 .backup` and every query ran on the copy. The table is
`session_usage`, one row per turn, from 2026-09-29 to 2026-10-26: 2,180 rows over 476 cards. A worker is a card whose
`launcher` is set, which is 446 cards and 2,058 turns. Subagent rows are left out of the pause count because they write
the 5 minute cache.

For each worker the gap is the start of a turn minus the end of the previous turn of the same card. A gap of 5 to 60
minutes is a pause the 1h pin saved: on the 5 minute cache the next turn would have rewritten the previous turn's
context. Prices are from `docs/runtime/claude-cost-model.md` section 2.

- Premium paid: `cache_write_1h` tokens times the 1h write price minus the 5m one, $3.00 per million on Opus 5.5 and
  $1.50 on Sonnet 5.5.
- Rewrite avoided: the context before the pause times a 5m write minus a cache read, $4.80 per million on Opus and
  $2.40 on Sonnet.

## Findings

Gaps between turns of one worker, all 2,058 turns:

| Gap before the turn | Turns | Average context before | Context carried, millions of tokens |
| --- | --- | --- | --- |
| first turn | 446 | | |
| under 5 minutes | 1,386 | 105k | 145.74 |
| 5 to 60 minutes | 203 | 167k | 33.93 |
| over 60 minutes | 23 | 86k | 1.98 |

The 5 to 60 minute gaps average 22.4 minutes and top out at 57. Most come from `say` turns (144 of 203), a peer or
launcher messaging a parked worker.

| Model | Premium paid | Rewrite avoided |
| --- | --- | --- |
| Opus 5.5 | $21.13 | $89.60 |
| Sonnet 5.5 | $36.31 | $36.62 |

- **Opus workers pay back 4.2 times.** Their pauses carry more context and each token costs more to rewrite.
- **Sonnet workers break even.** $36.31 against $36.62.
- **The 30 workers of five turns or more carry the result.** 15 of them won individually. Of the 416 shorter workers,
  7 won. 420 of 446 workers never paused 5 to 60 minutes between turns, and 35 ran a single turn.
- **Over 60 minutes the pin does nothing.** A gap past the hour is a full rewrite either way, and the 1h price made it
  $3.24 (Opus) and $1.35 (Sonnet) dearer.

## Limits

- A pause inside a turn, such as a long test run between two tool calls, is invisible to a per-turn table, so pauses
  are undercounted and the avoided figure is a floor.
- The previous turn's context stands for what a rewrite would write, which ignores compaction in between.
- `cost` is 0 on most rows, so every dollar here is computed from tokens and the price table, not read from the ledger.
- Keep-alive refreshes are 10 rows and are not netted against the avoided cost.
- Only workers that wrote to the 1h cache are counted, which is the pinned population.

## Change filed

Pin for workers that will live, not for every worker. See the backlog item filed with this note.
