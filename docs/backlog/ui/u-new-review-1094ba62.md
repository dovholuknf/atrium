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
