# u-new-usage-weekly-reset. The usage tab knows when the weekly limit resets

Status: filed by the orchestrator 2026-10-04, from clint. Owned by @ui. Not started.

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
