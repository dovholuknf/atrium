# Token visualizer: the last turn, its cache misses, and what each card is burning

Status: design, nothing built. Item `r-token-visualizer`.

The ask comes from a handover written in clint's dotfiles (`claude/usage/atrium-token-display.md`, 2026-10-20). His
status line shows `prompt <read> <written> <out>` for the turn that just finished in one terminal. He could not tell
what the three numbers meant, which turn they covered, or whether the cache hit. atrium sees every session, so it is
the better place to answer those three questions, for every card at once.

The cost side is `docs/runtime/claude-cost-model.md`. Its point that matters here: a cache miss on a big context is the
largest avoidable cost, and nothing in atrium shows one when it happens.

## 1. What clint wants to read

From the handover, in his terms:

1. What the last turn cost, and why: many calls, a big context, or a cache miss.
2. Whether the cache hit, without doing arithmetic on three numbers.
3. Labels that make each number explain itself, and make clear which turn it covers.
4. Which session is burning the meter.

## 2. What atrium already has

Most of the counting is built. The handover was written without knowing that.

| Piece | Where | What it does |
| --- | --- | --- |
| A row per turn | `daemon/usage.go`, `session_usage` | Read at each Stop from a cursor, merged by message id |
| Turn cause | `usage.go`, `store.Usage*` | `operator`, `say`, `restart-wake`, `resume`, `unknown`, `keepalive`, `subagent` |
| Subagents apart | `usage.go` `readSubagentFiles` | `subagents/` files and inline `isSidechain`, a row of their own |
| Write TTL split | `SessionUsage.CacheWrite5m`, `.CacheWrite1h` | Which cache a write went to |
| Calls per turn | `SessionUsage.Replies` | Distinct message ids in the turn |
| Context per turn | `SessionUsage.Context` | The last reply's input plus writes plus reads |
| Context now | `daemon/contextsize.go` | Read on the reaper's tick, the ctx chip and a row's bottom line |
| Card history | `js/usage.js`, `GET /v1/tasks/{id}/usage` | Totals, by cause, every turn, in the details only |
| Room over time | `js/usage-charts.js`, `GET /v1/usage` | Burn rate, top cards, by kind and by cause, across rooms |
| Live rows | the `usage` SSE event | Each row as it is written, the hub adds the room |
| Turns atrium caused | `daemon/turncost.go`, `js/usage-turns.js` | What each kind of delivery cost in the next turn |
| Keep-alive misses | `internal/daemon/keepalive.go` | A refresh that missed is counted, for refreshes only |

The counting rules of the handover (merge by id taking the max, an incremental cursor, the partial last line, the
subagents apart) are already the rules `usage.go` follows. The turn boundary differs: atrium's turn is what lies
between two Stop hooks, the handover's is what lies between two user entries. Atrium's is the better one for atrium,
because it knows what it typed and why, and it is already the boundary every stored row uses.

## 3. What is missing

1. **Misses are not counted.** A row sums its replies, so a turn of 14 calls with one rewrite of a 300k context looks
   like a turn with 300k of writes and nothing more. Whether a call missed is known per reply inside `replySet.row`
   and dropped there.
2. **No idle gap is kept.** The gap before a turn is its `started` minus the previous row's `ended`. Both are stored,
   but nothing subtracts them, so a miss cannot say "after 1h12m idle".
3. **Nothing is on the card face.** `js/usage.js` says so on purpose: token history is "asked for by opening it". The
   ctx chip is the only token figure a card shows without a click.
4. **No weighted figure.** Raw tokens mislead: reads are 98 percent of the tokens and a tenth of the price. The usage
   tab hides reads by default and counts "in, out and writes", which is closer but still weighs a write and an output
   token the same, and an output token costs four times a 5m write. Money was removed on purpose (backlog runtime 37,
   sa37b: "the cost is stupid"), so a weight has to be something other than dollars.
5. **The turn in flight is invisible.** A row is written at the Stop. A long turn shows nothing until it ends. The
   status line shows the turn so far.
6. **Which turn a number covers is not said.** The details list rows with an end time, which answers it there. On a
   face it would not be obvious.

## 4. What the board shows

### 4.1 Per card

On the face, one chip beside the ctx chip, only for a card whose last turn missed the cache:

```text
[ctx 41%] [miss 1 · 300k]
```

The chip reads "the last turn rewrote the cache once, 300k written". Its tooltip says which turn (its end time and
cause), the idle gap before it, and the turn's calls, read, written and out. No chip when the last turn hit. The chip
clears when the next turn ends without a miss. This answers question 2 without a number to work out, and leaves the
face as quiet as it is today on a card that is fine.

In the card's details, the usage list (`js/usage.js`) gains:

- A `miss` column on each turn row: blank, or the count of calls that missed, in the danger colour.
- An `idle` value in the row's tooltip when the gap before the turn was past 5 minutes, and past an hour on a card
  pinned to the 1 hour cache.
- A `weight` column (section 5) and a sort on it, so the expensive turns come first when asked.
- A header line for the last turn in plain words: `last turn (you, 16:02): 14 calls on 45k, read 652k, wrote 14k,
  out 5k, cache hit`. This is the readable form of the status line's three bare numbers.

### 4.2 Per room

The usage tab gains one section, below tokens by cause, in its existing parts (`uch`, `ucnote`, a `ucmini` per row):

- **Cache misses** in the range: one line per miss, newest first, with the card, the context it rewrote, the idle gap
  and the cause of the turn. A count and the tokens rewritten in the heading. This is the handover's "miss alert"
  drawn where clint already looks at spend, without a toast.

And the by-card tiles take an option to rank by weight instead of counted tokens, which answers "which session is
burning the meter" with the right sense of cost.

### 4.3 Across rooms

Nothing new. The usage tab already reads every attached room through the hub and keys every card by room. The miss
section and the weight follow the same path.

## 5. The weight

A weighted total in input-token equivalents, from the ratios to base input that the cost model uses: read 0.1, 5m
write 1.25, 1h write 2, output 5, uncached input 1. It is shown as `w 108k`, never as dollars. The ratios hold across
Opus 5.5, Sonnet 5 and Haiku 4.5. Sonnet 5.5 and Fable 5.1 read cheaper than 0.1 (cost model, section 3.3), so one
table of ratios is an approximation of a few percent on reads, which are a small share of the weight. The 14-call turn
of the handover comes to about 65k for reads, 18k for writes and 25k for output: reads look dominant in raw tokens and
are not.

The weight is computed on the board from the five stored kinds, so it needs no column and no migration, and changing
the ratios changes every figure at once.

## 6. Where the numbers come from

| Figure | Source today | Change |
| --- | --- | --- |
| read, written, out, calls | `session_usage` | none |
| context | `session_usage.context`, and `context_size` for now | none |
| misses per turn | not kept | count in `replySet.row`, a new column `misses` |
| rewritten tokens | not kept | sum of `cache_write` over the missed calls, a new column `miss_tokens` |
| idle gap | `started` minus the previous row's `ended` | computed on read, no column |
| weight | the five kinds | computed on the board |
| last-turn miss on the face | not on the card payload | the card view carries the last row's two miss figures |

**The miss rule.** The handover gives two. The status line's: a call that writes more than it reads plus its uncached
input. `Get-UsageReport.ps1`'s stricter one: a write after a gap longer than the TTL is a cold wake, and a write of
20k or more with no gap is a context jump. The first is simple and has one known false positive, a big tool result
early in a session when the cached context is still smaller than the result. Atrium has the timestamps for the
stricter rule, but the gap that matters is per call, from the previous call of the same session, and that needs the
previous call's time across turns, which the cursor keeps (`usageCursor.lastAt`). Proposed: count a miss on the
simple rule, and keep the idle gap beside it so the board can say "cold wake" or "jump" without a second rule in the
store. Open question 2.

## 7. The smallest first stage

One change in the room and one on the board, no new view:

1. **Room.** A migration adds `misses` and `miss_tokens` to `session_usage`, default 0. `replySet.row` counts a reply
   as a miss when its cache write exceeds its cache read plus its input, and sums its write into `miss_tokens`.
   `UsageTotals` sums both. The `usage` event carries both. Old rows read 0, which is honest: unknown, shown as blank.
2. **Board.** The details' usage list gets the `miss` column and the last-turn header line of section 4.1, and the
   row tooltip gets the idle gap.

That answers questions 1 to 3 for any card clint opens, with real data on every new turn, and it is the data every
later stage reads. It touches no card face, no hub and no setting.

Later stages, each alone:

2. The miss chip on the card face (section 4.1), from the last row carried on the card view.
3. The weight column in the details and the rank-by-weight option on the by-card tiles.
4. The cache misses section on the usage tab.
5. The turn in flight: a read of the transcript since the last Stop on the reaper's tick, as `contextsize.go` already
   does for the context, shown in the details only. It is the largest of these and the least asked for.

## 8. Not in scope

- Dollars. Removed on purpose, kept reachable (backlog runtime 37). The weight is not a price.
- A toast or a notice on a miss. The usage tab's rule is that it reports and decides nothing (`js/usage-turns.js`).
- The status line itself. It lives in clint's dotfiles and stays there.
- Runners other than Claude. Their transcripts carry no cache split.

## 9. Open questions for clint

1. Is a chip on the card face for a missed last turn wanted, against the rule that token history stays in the
   details? Proposed: yes, only on a miss, so a healthy card shows nothing new.
2. Which miss rule: the status line's "wrote more than it read", or the stricter cold wake and context jump split?
   Proposed: the simple rule stored, the gap shown beside it.
3. Should a miss on a small context be shown at all? A miss on 20k costs little. Proposed: the chip only above a
   floor, 50k by default, and every miss in the details and the tab.
4. The weight as `w 108k` in input-token equivalents, or not at all now that dollars are gone?
5. Is the turn in flight (stage 5) worth a transcript read on every tick, or is the turn that just ended enough?
6. Should a keep-alive refresh that missed join the tab's misses section, or stay on the keep-alive's own surface?
   Proposed: join it, labelled keep-alive, since it is the same cost.
