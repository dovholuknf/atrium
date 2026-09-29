# HANDOFF: sa21, looks-idle badge (backlog-2 item 21)

Branch `claude/sa21`. Spec, findings and design are in `docs/backlog-2.md` item 21. Read that first.

## "First" investigation

- The existing check (`stuckNow`, a2a.go) covers only agent-launched cards, only a silent stop in `needs-input`, and
  only a tool running 20+ minutes. A board-started `running`/`thinking` card is invisible to it by construction.
- Why the Stop was lost is NOT provable: the room log (`C:\Users\claude\.atrium2\room.err.20260926-083926`) has
  nothing between 13:00 and 17:00 on 2026-09-25, because successful hooks are never logged. Leading hypothesis: a late
  `tool-start` post raced past `turn-end` (`handleActivity` runs `go d.onActivity`), and `turnResumed` put the card
  back to `running`. Untested. A sequence stamp on `/activity` posts would settle it if the new firing log shows it.

## Design and review

As written in backlog item 21. Mercurius session `s_jGsDlA3ObWch` round 1: `ready_to_build`, no concerns, two
advisories (fixtures for both frames, classifier reason in the log), both taken. `record_round_notes` failed with
"session not found" (the server lost the session), so no notes file was written.

## Built (all committed)

- `internal/daemon/idleframe.go`: `classifyFrame(tail) (idle, reason)`.
- `internal/daemon/looksidle.go`: `watchLooksIdle` (claude only, running, mid-turn, no subagents/dialog, pty silent
  25s via `ATRIUM_LOOKS_IDLE`), flag, `looksIdleGone`, logging, counter `looksIdleFired`.
- `supervisor.go`: `runner.lastOut` stamp and `runner.wake` hook in `deliverOutput` and `noteOperatorTyped`.
- `activity.go`: `Activity.LooksIdle/IdleSeconds/IdleAt`, `looksIdle` map, cleared in `set` and `forget`; `get` split
  into `get` + `getLocked`. `onActivity` and `turnEndedBecause` call `looksIdleGone` first so clearing is logged.
- `reaper.go`: one call to `watchLooksIdle` after `watchWorkers`. a2a.go is untouched (sa31 works there).
- Board: `looksIdleChip` in board.js, `workingNow` in terminal-list.js, `looksidle` alert in settings-spine.js and
  notify.js, headless section `looksIdle` in scripts/test-board-headless.js.
- `docs/changes/21.md` (changelog + test plan), backlog item 21 updated.

## Tested

`go test ./internal/daemon -run 'LooksIdle|ClassifyFrame|ARunningCardWithASilent'` passes, and
`-run 'Activity|Turn|Subagent|Held|Reap'` passes. `go vet` clean. `go build -o build.claude/atrium.exe ./cmd/atrium`
ok (`make` is not installed here). `scripts/check-board.sh` parses fine.

## Left, in order

1. Run the headless `looksIdle` section: `HEADLESS_ONLY=looksIdle scripts/check-board.sh`. Playwright is NOT
   installed on this machine, so it never ran. Expect to fix selectors if it fails.
2. Check the frame signature against a real Claude Code screen. The fixtures are hand-written (`workingFrame`,
   `idleFrame` in looksidle_test.go). Strings are constants in idleframe.go: `╭`, `╰`, `to interrupt`.
3. Rule 77c says merge `claude/main` into the branch before reporting done. The brief said no merges, so it was not
   done. The director decides.
4. Optional: the alert rides its own kind `looksidle`, not the gear's waiting setting. Decide whether it needs a gear
   switch.
5. Report `done` with the final sha and the review outcome above.

## Traps

- The Bash hook refuses `;` chaining, `>` redirection and python. One command per call.
- The Mercurius artifact name must be a safe name (no spaces or parentheses), and the artifact should be an extract
  (`item21.md` in the scratchpad), not the whole backlog.
- `activityTracker.get` holds `a.mu` and now delegates to `getLocked`: do not take the lock inside `withLooksIdle`.
- The `wake` callback runs on the pty reader and keystroke paths: keep it to an atomic store plus `go`.
- `wake` stays armed after a hook clears the flag. That is harmless (`looksIdleGone` is a no-op) and deliberate.
