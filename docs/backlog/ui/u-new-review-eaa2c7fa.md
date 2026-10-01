# Review of burn-chart eaa2c7fa: the cumulative chart as percent of the limit

Reviewer: @review, 2026-10-01. Range 0c06e8bb..eaa2c7fa on sg3/claude/burn-chart, one commit, unsigned (sg3 has no
key), noted. Read usage-charts.js and usage-limits.js, the burnChart case, and the three screenshots. Board checks are
@ui's: I did not run the suite.

**Verdict: HOLD 0c06e8bb..eaa2c7fa** on the medium below.

## MEDIUM: the 1h range and a card filter calibrate the limit on part of the burn, so "hits limit" comes early

`ucLimitFor` sets tokens per point as `tok / pct`, where `tok` is what `series` holds inside the limit's window
`[reset - window, now]` and `pct` is the account's reading. That is right only when `series` holds the whole window
for the whole account. Two cases where it does not:

- **The 1h range.** `series` covers the last hour, and the 5h window reaches up to 5 hours back. Example: 3 hours into
  the window at 60%, burning evenly. `tok` is one hour of burn (20 points' worth), so `perPct` is a third of the true
  value. `rate` is the same hour, so `pctRate = rate / perPct` is 3 times the true 20 points an hour. The chart says
  "hits 5h limit" in 40 minutes. The true projection reaches 100% at the reset, two hours later.
- **One card filtered.** `series` is one card's burn and `pct` is the account's, so `perPct` shrinks by the same factor
  and the same false "hits limit" appears. The fetch is narrowed to that card (usage-charts.js:134-136), and a group
  filter (`ucGroupParam()`, same line) narrows it the same way.

`ulTokensIn` in usage-limits.js already exists for this reason: "Null when the range does not reach back that far or
one card is filtered, since then it is not the account's burn" (`UC.card || from < UC.since`). Fix: compute `tok` with
`ulTokensIn(start, now)`, or apply the same two guards, and fall back to the token chart when it is null. Add a case:
1h range with a reading at 60% three hours into the window must not show a crossing.

Reasoned from the code with the arithmetic above, not run.

## LOW: usage before the window reads as 0%

On 24h, `Y` clamps every value under `base` to 0%, so the line lies flat at 0% until the window opens (12:30 in
day-hits-limit.jpg) although the right-hand label says 176k for the range and 100% is 86k. The hover says "0% of the 5h
limit" there, which is true of the window and reads as "nothing used". Either start the percent line at the window
start, or keep the token line for the part before it.

## NITS

- With two reset groups of one kind (two accounts), each draws its own reset lines, and both coming resets get a
  "5h reset" label at different places with no account named.
- The y axis labels stop at 100% while `ymax` is at least 120, so the top fifth has no label. Fine, say it is on
  purpose or add 120%.

## Read and fine

- `base = run - pct * perPct` puts the current reading at `run` and 0% at the window start, so the line passes through
  the reading.
- The crossing is `now + (100 - pct) / pctRate`, shown only when it falls before the reset, otherwise the projected
  percent at the reset. `pct >= 100` crosses at now.
- `ucPaintCum` from `ulRefresh` repaints in place and does nothing when the usage tab is not drawn.
- Every interpolated label is a number, a fixed name or a locale date. Nothing from a card reaches the markup.

Verdict: HOLD 0c06e8bb..eaa2c7fa. Re-read 0c06e8bb..tip, hub-ok and room-ok.

Quality: after the Sonnet switch, the chart is careful and the screenshots match the code. The miss is the one the
existing `ulTokensIn` comment already names: the calibration assumes the chart holds the account's whole window.

## Re-read 0c06e8bb..e3d99ebc: OK hub and room

e3d99ebc is a merge of claude/main (9f9a015a) into eaa2c7fa, and the fix is folded into the merge itself. Read with
`git show e3d99ebc -- internal/api/web/js/usage-charts.js` (the combined diff). Unsigned, noted.

- **Medium closed.** `ucLimitFor` returns null when `ulTokensIn(start, now)` does, which is a card filter or a range
  that does not reach the window start (`from < UC.since`, so the 1h range on a 5h window). It also returns null with
  cache reads shown. The chart then falls back to the token line.
- **Correction to the hold.** I named a group filter as a third case. It is not one: `ucGroupParam()` only asks the
  rooms to split each bucket by group, and the totals stay the account's. `ucGroupFilter` narrows the card table, not
  the series. Nothing to fix there.
- **Low closed.** In percent the line starts at the window start, `M X(lim.start) Y(base)`, and skips the points before
  it. The hover before the window says "before the 5h window". The header says "since the 5h window began", and the
  right-hand total is the window's tokens, so the label and the line now agree.
- **Nits closed.** Each coming reset label carries its time, and a label sits at `ymax` when it is above 100%. Two
  accounts' reset labels are still not told apart. Leave it.

NIT (new): a fix folded into a merge does not show in `git show` without the combined diff, or in `git log -p`. Put
it in its own commit next time.

Verdict: OK hub and room 0c06e8bb..e3d99ebc. I read the burnChart case and did not run the suite.
