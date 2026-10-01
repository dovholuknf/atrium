# Review of 1094ba62 (@ui u-m-homefix: /m home order and the subagent filter)

Reviewed by @review, 2026-10-01, from `git diff 1094ba62~1 1094ba62 -- internal/api/web`, read only. Hub side. Board
and headless checks are @ui's, and I ran none.

## What holds

- **Each list has its own stored order.** `orderNeeds` defaults to oldest, and `order` (the all list) defaults to
  newest. `orderKey()` picks the one for the list on screen, in both the control's paint and `setOpt`. An old stored
  value without `orderNeeds` loads the default. The needs list is already oldest-wait-first, so "newest" is a reverse
  of a copy, and the list itself is not mutated.
- **Sorting by `last_activity_at`.** @ui's diagnosis is correct. `idle_seconds` is a count taken when a row was
  read, so two rows read at different times cannot be compared by it, and `last_activity_at` is a fixed fact.
  `idle_seconds` is kept only as the fallback.
- **Hide subagents** now also keeps cards tagged `atrium:director`, `atrium:orchestrator`, `orchestrators` or
  `atrium:hold-notices`. That narrows the filter, takes nothing away, and is local to `home.js`.
- No new data reaches the page. The group headings still go through `U.esc`.

## Findings

### Low

1. **The desktop board still sorts by the flawed number.** `cardActivityCmp` in `cardrules.js` reads `idle_seconds`
   first (`cardIdleSeconds`), and that is the board's "last active" sort. So the board orders rows read at different
   times wrongly, exactly as /m did. /m now forks the shared rule instead of fixing it. Preferring `last_activity_at`
   in `cardIdleSeconds` would fix both pages and keep one rule.

### Nit

2. For a card with neither field, `activityMs` returns `Date.now()`, which changes between comparisons in one sort.
   That is harmless in practice, but `-Infinity` (or the `Infinity` the board uses) would make the comparator stable.

Quality: after the Sonnet switch. The root cause was found on real data and tested against an independent sort over
183 live cards, which is strong work. The miss is not carrying the finding back to the shared rule it came from.

HUB DEPLOY OK 1094ba62

## Re-read of 03cca53f (@ui, low 1 and nit 2, a change to the shared sort)

`git diff 1094ba62 03cca53f -- internal/api/web`, read only.

- `cardIdleSeconds(t, now)` prefers `last_activity_at`, falls back to `idle_seconds`, and gives `Infinity` when both
  are missing, with no `Date.now()`. The clamp to zero is gone, so a stamp slightly ahead of the clock still orders
  ahead.
- `cardActivityCmp` takes one `now` per comparison and returns 0 when the two values are equal. Two cards with
  neither field (`Infinity === Infinity`) therefore tie instead of giving `NaN`.
- The /m fork is removed. `home.js`, the /m card switcher (`card.js:524`) and the stack's activity sort
  (`stack.js:11`) all call `cardActivityCmp`.
- Nit 2 is closed.

### Low (low 1, only partly closed)

3. **The desktop board's main sorts still compare `idle_seconds` directly.** They do not go through
   `cardActivityCmp`:
   - `board.js:1909`, `columnOrder`, the "last active" sort of the board's own columns.
   - `terminal-list.js:1545`, the terminal list's last tier.
   - `switcher.js:260`, the card switcher.
   - `stack.js:38`, the status sort's tie break.

   Each one keeps the flaw @ui found. A row an event just replaced is compared, by a count, against rows read a
   minute earlier. Replacing each `(a.idle_seconds || 0) - (b.idle_seconds || 0)` with `cardActivityCmp(a, b)` would
   finish the job.

Quality: after the Sonnet switch. The shared function is right, and the subtle details are handled: one `now` per
comparison, the `Infinity` tie, and the dropped clamp with the live data that justified dropping it. The miss is
scope again. The change was framed as fixing the shared sort, but four desktop sorts never used the shared function.

HUB DEPLOY OK 03cca53f (low 3 to follow; it only matters where rows were read at different times)

## Re-read of 6f701eba (@ui, low 3: every read goes through the shared rules)

`git diff 03cca53f 6f701eba -- internal/api/web`, read only.

- **Sorts.** `columnOrder`, `termOrder`, the switcher's rows and the stack's status tie break all call
  `cardActivityCmp`. The waited sort uses `cardWaitSeconds` with one `now` per comparison.
- **Waiting time** gets the same treatment as idle: `cardWaitSeconds` prefers `waiting_since` and falls back to
  `wait_seconds`, which the daemon makes the same stale way (api.go). It feeds the waited sort, `bigNumber` and the
  state chip.
- **Shown ages** go through `cardIdleAge` and `cardSecs`: never negative, never infinite, and 0 with no stamp. The
  board's chips, `isOutOfContact` (unchanged for a card with no stamp, which reads 0 as before), the peek's foot,
  the task header and the stack's big number all use them.
- `stateChip` takes one `now` for both of its numbers, so "the big number already says this" is compared at a
  single instant, and `Infinity === Infinity` holds for a card with no stamp.
- **Left alone, correctly.** `board.js:547` reads `activity.idle_seconds`, the pty-quiet figure from
  `daemon/looksidle.go`, which is a different fact.

Low 3 is closed. No findings.

Quality: after the Sonnet switch. The sweep was taken to its end (sorts, keys, chips and shown ages), and the one
read left was judged a different fact, which it is. This answers the scope note on the last two reviews. No drop
seen.

HUB DEPLOY OK 6f701eba
