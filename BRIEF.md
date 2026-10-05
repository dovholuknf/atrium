# Worker brief: u-new-no-question-reminders

Launched by @ui director (director-ui on claude-sg4) for the orchestrator, night of 2026-10-04. You implement one
backlog item. Your worktree is on branch `claude/u-new-no-question-reminders`, cut from claude/main c721241b.

## Read first
- `docs/backlog/ui/u-new-no-question-reminders.md`: the item. Its status says HELD (pause), but the orchestrator put
  it on tonight's list, so build it.
- `docs/backlog/ui/u-new-growler-off-bell-instead.md`, section "For @fabric: stopping the hub's reminder ladder".
  Option 1 there (questions and blocked skip the ladder, permissions keep it) is the fix. Build that, plus the
  setting per reason (questions off by default, permissions on) and a card that ends or exits takes its growlers
  with it.
- `internal/link/growl.go` (`growlBackoff`, the reminder loop) and `growl_test.go`. This is Go on the hub side.
  Keep the change small and contained.
- `internal/api/web/AGENTS.md` for the board side of the setting.

## Rules
- Go tests: a question raises once and is never reminded, a permission keeps its ladder, the setting turns question
  reminders back on, an exited card's growlers are gone.
- The setting needs a control on the board (settings dialog). Before and after PNGs of that row (headless board,
  real code) under `docs/screens/u-new-no-question-reminders/`, and a headless check that it saves.
- Changelog: `changelog/ui/2026-10-04-u-new-no-question-reminders.md`.
- Commit subjects: one line, no body, no trailers. Commit on this branch only. Never on main or claude/main.
- Go builds go to `build.claude/`. Clear `ATRIUM_LOCATION` and `ATRIUM_DEBUG_INPUTLAG` before `go test`.
- Do not merge, deploy, or restart atrium, the hub or a room. Do not edit CLAUDE.md files.
- Prose: no em-dashes, no semicolons, wrap at 120.

## Finish
Write REPORT.md in the worktree root (what changed, design notes, test results with the command, PNG paths), commit
it, then send ONE atrium_report: `done <sha>, REPORT.md` or `incomplete <sha>, state in REPORT.md`. Nothing else: no
start, progress or status messages.

If you have `atrium_git_url`: to read code that is not in your cwd, call it, then fetch it from the URL it gives. Never ask for a paste.
