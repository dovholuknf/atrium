# u-new-usage-weekly-reset. The usage tab knows when the weekly limit resets

Status: filed by the orchestrator 2026-10-04, from clint. Owned by @ui. Built 2026-10-04 on `claude/u-new-usage-weekly-reset`, not merged.

## What is wrong

The usage tab's 7d view is a rolling seven days. clint's weekly limit does not roll: it resets at a fixed time, which
for him was 6pm ET on Sunday 2026-10-04. The cumulative chart runs from Monday 28 Sep to now and reads 140M, a figure
that mixes the week that just reset with the new one. Screenshot: `.atrium/incoming/20261004-221150-pasted.png`
(local to sg4).

clint: "usage needs to know when the reset happens. it should account for that."

## Done looks like

- A setting for the weekly reset: weekday, time and time zone (default Sunday 18:00 America/New_York). One per
  board, stored as a daemon or hub setting, not per browser.
- A "this week" range beside 1h/6h/24h/7d that starts at the last reset. The cumulative chart starts at zero there.
- The reset is marked on every chart whose range crosses it (a vertical line with the time).
- The pace line ("at this pace: N by midnight") also says what the pace gives by the next reset.
- Before and after PNGs of the real tab, committed under `docs/screens/u-new-usage-weekly-reset/`.

## Design note

- The setting is one JSON value, `usage_week_reset` `{day, time, tz}`, in the existing settings store and
  `GET/POST /v1/settings` (`internal/api/usageweek.go`). The room validates it and the browser works the reset out
  with `Intl` in that zone, so the tab needs no new endpoint and a clock change is handled by the zone's rules.
  Default Sunday 18:00 America/New_York.
- "This week" runs from the last reset to now, with buckets of 5, 15 or 60 minutes by how far into the week it is.
- The pace line on `this week` is the week's own total at the reset. On 24h and 7d the chart is not the week's total,
  so it says `N more by the reset` rather than a total that would mislead.
- Where a card has reported the weekly limit, the cumulative chart keeps drawing the reset from that report and
  the setting's mark is left off it, so the two never sit on top of each other.
- The control is in the usage toolbar, not the settings screen. Moving it there is cheap if clint wants it.
- Not done: the per-card and group mini charts carry no reset mark (a sparkline too small to name it).
