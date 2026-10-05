# u-new-no-question-reminders report

## What changed
The hub side was already on claude/main (commit afa1d88a): `growlLadderDefault` in `internal/link/growl.go` (permission,
halt and deploy-hold remind, question and blocked ring once), the `growl_ladder` setting, `GET/PUT /_hub/growl-ladder`,
and ended cards ending their growlers. Its tests are in `internal/link/growl_ladder_test.go` and cover the four cases in
the brief (question raises once, permission keeps its ladder, the setting turns question reminders back on, an ended
card's growlers go). No Go change was needed, so I made none.

What was missing was the board control, which this branch adds:
- `internal/api/web/index.html`: row `#s-gl-row` in the notifications pane, two boxes (permission, question), hidden
  until the hub answers.
- `internal/api/web/js/hubnotify.js`: `loadGrowlLadder` and `saveGrowlLadder` (PUT on change, repaints from the answer).
- `internal/api/web/js/settings.js`: loads the row when the gear opens.
- `scripts/check-growl-ladder.js`: headless check, real board code and mocked endpoints.
- `changelog/ui/2026-10-04-u-new-no-question-reminders.md`.

## Design notes
The row is hub-wide, not per browser, because the hub runs the ladder. PUT is local-operator only on the hub, so a
remote board gets the hub's refusal sentence in the row. Only permission and question are exposed. Blocked, halt and
deploy-hold stay on their defaults.

## Tests
- `go test ./internal/link/` ok (156s).
- `NODE_PATH=<atrium>/node_modules node scripts/check-growl-ladder.js docs/screens/u-new-no-question-reminders`
  prints "growl ladder row ok": opens permission on and question off, ticking question PUTs both, reopen reads it back,
  unticking permission PUTs, a board with no hub draws no row.
- `node scripts/check-settings-panes.js internal/api/web/index.html`: sound, 21 controls.

## PNGs, `docs/screens/u-new-no-question-reminders/`
- `before.png`: the notifications pane's growler row from the tree before this change (no reminder row below it).
- `after.png`: the new row, defaults.
- `after-question-on.png`: after ticking question and saving.
