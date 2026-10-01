# u-new-burn-chart-axes: the usage burn chart says nothing about time or the limit

clint: "burn chart isn't so useful to know if it's 100% or what date or any of that sorta shit".

## What the chart shows today

The usage tab's "cumulative tokens" line: a grey line from 0 to the right edge, labelled only "0" at the left, "33M" at
the right, and "at this pace: 54M by midnight" under it, with a dotted projection. No time axis, no y ticks, no relation
to any limit.

## What it must answer at a glance

1. **When.** A time axis: hour ticks for a day range, dates for a week range, "now" marked where the solid line ends and
   the projection starts.
2. **How much of the limit.** The y axis in percent of the limit that matters (the 5-hour window and the weekly limit,
   the same figures the status line shows as "5h 40%" and "wk 42%"), with a 100% line drawn. Tokens as the secondary
   label, not the primary.
3. **When the limit is hit.** If the projection crosses 100%, mark the time it crosses ("hits weekly limit Thu 21:40").
   If it does not, say the projected percentage at the window's reset.
4. **Resets.** The 5-hour window and the weekly reset drawn as vertical marks, so a drop in the burn rate after a reset
   reads as one.
5. Hover anywhere on the line: time, cumulative tokens, percent of limit.

## Acceptance

Headless section on mocked usage rows: axis labels present, 100% line drawn, crossing time computed and shown for a
projection that crosses, reset marks at the right times. Screenshot for clint.
