# Report: u-new-usage-weekly-reset

## What changed
- `internal/api/usageweek.go`: the setting `usage_week_reset` `{day, time, tz}`, one per board, kept in the settings
  store. Validated on POST (400 on a bad day, time or zone), default Sunday 18:00 America/New_York, read in
  `GET /v1/settings`, broadcast on the `settings` event. Wired in `settings.go`. The store/API half is this one file
  and about 20 lines in settings.go.
- `js/usage-charts.js`: a `this week` range from the last reset (the reset is worked out with `Intl` in the
  setting's zone, so clock changes are handled), the reset marked on the burn and cumulative charts of any range that
  crosses it, the pace line naming the next reset, and a toolbar button and form that edit the setting.
- `index.html`, `css/files.css`: the button, the form and the mark styles.
- `scripts/test-board-headless.js`: new section `usageWeekReset`. Three assertions in `usageTab` loosened from an
  exact match to a prefix because the pace line now carries the reset.
- Changelog `changelog/ui/2026-10-04-u-new-usage-weekly-reset.md`. Design note in the item file.

## Design notes (also in the item file)
- No new endpoint. The browser does the zone maths.
- On `this week` the pace line is the week's total at the reset (`1.2M by midnight, 6.7M by the reset Sun 18:00 EDT`).
  On 24h and 7d it says `N more by the reset`, since those charts are not the week's total.
- Where a card has reported the weekly limit, the cumulative chart keeps that report's own reset marks and the
  setting's mark is left off it.
- The control lives in the usage toolbar, not the settings screen. No question for clint that blocks anything.
- Not done: the by-card and group mini charts carry no reset mark.

## Tests
- `go test ./internal/api -run 'UsageWeekReset|UsageCacheReads'`: pass.
- `go test ./internal/api`: one failure, `TestTheWalkerLaunchSetAndClear` (prsdrawer_test.go:239). It fails the same way
  on the base commit 5d68bfa2, so it is not from this change.
- `HEADLESS_ONLY=usageWeekReset,usageTab,usageGroups,usageCacheReads,settingsOnce node scripts/test-board-headless.js`:
  pass. `usageWeekReset` covers the reset maths (EDT, EST after the clock change, a London zone, a bad zone), the
  this-week total leaving last week out, the line starting at zero, the mark on this week, 7d and 24h and not on 1h,
  the pace line, the request's `since`, and the form (save, refused zone, settings event).
- The full headless suite was not run, only those sections. `node_modules` is a symlink to the main checkout's (ignored).

## Screens, `docs/screens/u-new-usage-weekly-reset/`
Real board code, seeded data, now fixed at Mon 2026-10-05 12:00 EDT, 50k an hour before the Sunday reset and 40k after.
- `before-7d.png`: the base board, 7d. The cumulative runs from Mon 28 Sep to 8.2M, mixing both weeks.
- `after-week.png`: `this week`. Starts at zero at the reset, 720k, mark and pace line.
- `after-7d.png`: 7d with the reset marked.
Taken with `USAGE_WEEK_SHOT_ONLY=<png> USAGE_WEEK_RANGE=week|7d HEADLESS_ONLY=usageWeekReset node scripts/test-board-headless.js`.
