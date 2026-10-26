# What a fresh context rediscovers

Item: f-fresh-rediscovery-measure. The cost model (`docs/runtime/claude-cost-model.md` sections 3.4 and 4) assumes a
fresh context spends 30k tokens rediscovering after a handoff, with 20k base and 5k handoff. It says fresh at expiry
stops winning at about 42k of rediscovery at 150k context, and about 101k at 300k. This note measures it.

## Method

Read-only. The Claude transcripts under `~/.claude/projects/*/*.jsonl` were read in place and nothing was written to
them or to any database. A new-context session is one with a user message starting `[atrium] new context: read`, the
wake atrium types after `/clear`. Nine sessions on this room carry it. Per session, the context of a call is input
plus cache read plus cache creation tokens, one call per assistant message id. "Before first write" is the context at
the first call that uses Edit, Write, `git add` or `git commit`, minus the context of call 1.

## Findings

| Measure | Median | p90 |
| --- | --- | --- |
| Context at call 1 | 7.1k | 20.8k |
| Context at call 5 | 26.6k | 41.6k |
| Context at call 10 | 32.3k | 69.5k |
| Growth before first write | 46.0k | 137.7k |
| Calls before first write | 20 | 49 |

Per session, growth before first write: 4k, 6k, 9k, 38k, 46k, 65k, 71k, 83k, 138k. Handoffs were 1.4k to 6.7k bytes,
under 2k tokens.

- Base is about 7k, not 20k, in six of nine sessions. The three at 15k to 21k are the ones whose call 1 already
  carried a large reply. The model's 20k base is high.
- The 30k rediscovery assumption sits inside the observed spread but the spread is wide. The median is 46k, above the
  42k break-even at 150k. The p90 is far over the 101k break-even at 300k.
- The distribution is bimodal. Three sessions wrote within 3 to 8 calls and read under 10k. Six read 38k to 138k
  first. A mean hides this, so the median and p90 are the right figures.
- Growth before first write includes reads the task needed anyway, so it is an upper bound on pure rediscovery. A
  session on a new task would read the same files warm. No transcript separates the two.

## Limits

Nine sessions, one machine, one operator, all on the same repo. The counts support "30k is plausible, not safe", not
a tighter figure. The window is the first write, so a session that works read-only is counted as never writing.

## Recommendation

Keep fresh-at-expiry offered, not automatic, as the cost model says. Do not make it the default on the 30k figure.
Instrumentation to grow the sample is filed as a runtime item.
