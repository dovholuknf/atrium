# r-new-restart-to-update: notice a session running an old runner, and restart it with one click

Status: parked (clint, 2026-09-30). @runtime the detection, @ui the badge and buttons.

## Why

Claude Code updates itself in the background and then shows `✔ Update installed · Restart to update` in every
session started before the update. Each one keeps running the old version until somebody restarts it. clint asked
for atrium to recognise this and restart those sessions when asked, by clicking a button.

## What is already there

- **restart this session** in the terminal menu (`internal/api/web/js/terminal.js`, `restartTerm`): exits and
  resumes the same conversation on the same card. Its help text already names this nag.
- `internal/daemon/runnerupdate.go` reads a harness's INSTALLED version from the package's own metadata, with no
  process started, and knows the published version.

## Wanted

- **Record the runner version at spawn.** When a supervised runner starts, read the installed version the same
  way `runnerupdate.go` does and keep it with the running process, in memory like the activity (a restart ends
  the process it describes).
- **Stale means installed is newer than spawned.** Compare on the reaper's tick or on a harness update event. No
  screen scraping as the main signal. The footer text is a fallback only, for a runner without a `package`: if
  the screen model already shows the line, count it.
- **A badge on the card:** "update installed, restart to apply" with the two versions, and a **restart** button
  that calls the existing restart.
- **Restart only between turns.** The button waits for the same gap the new-context cycle waits for (not
  running, no dialog, empty line), and uses r-new-new-context-mid-turn's "end your turn" nudge if the card never
  stops. A restart mid-turn loses the turn.
- **Restart every stale idle session**, one button on the board (the gear, or the header count "3 sessions on an
  old claude"). It restarts each one in turn, only when idle, and skips any with a human typing.
- The board-wide button never touches a card with a dirty input line or an open dialog, and says which it skipped.

## How to check

A fake harness whose package metadata changes version after spawn: the card turns stale, the badge shows both
versions, the button restarts only at a gap, and the resumed session shows the new version and no badge.
