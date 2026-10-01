## Test plan

## @LETTER@. The cumulative chart reads against the limit

### @LETTER@1. The axes

On the usage tab at 24h with a card reporting a 5h percentage, the cumulative chart shows hour ticks, a marked "now",
a y axis of 0 to 100% with token counts beside it, and a 100% line. At 7d the ticks are dates and the limit is the weekly one.

### @LETTER@2. The crossing

When the dashed projection reaches 100% before the reset, a red mark and the heading line read "hits 5h limit 16:47".
Otherwise the line reads "at this pace: 67% of the 5h limit at reset". The resets are dashed vertical marks.

### @LETTER@3. Hover

Move over the line: the read-out gives the time, the cumulative tokens and the percent of the limit, and says projected past now.

Pictures: docs/backlog/ui/img/burn-chart/day-hits-limit.jpg, day-under-limit.jpg, week.jpg.
