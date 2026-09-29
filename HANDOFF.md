# sa74 handoff: backlog-2 item 74, option 1 (the height hold) built, not yet merged with claude/main

Read BRIEF.md, then the item 74 section of `docs/backlog-2.md`. It holds the diagnosis, the height-hold state rule,
and the recommendation.

Branch `claude/lost-lines`, head **ab53c5c**. Everything is committed except BRIEF.md and this file.

## Who you report to, and the rules in force

- Report to `@terminal` (handle `terminal-director-of-pty-to-xterm-and-sc`). atrium-87300 cycles this card.
- Do NOT edit `CHANGELOG.md` or `docs/test-plan.md`. The change note is `docs/changes/74.md`, and it passes
  `pwsh scripts/fold-changes.ps1 -DryRun -Item 74` (it folds as CS).
- Targeted tests only (`-run`). Clear ATRIUM_LOCATION and ATRIUM_DEBUG_INPUTLAG first. One-line commits, no trailer.
  No merge to claude/main, no deploy, never the live room.
- Option 2 (OpenConsole ConPTY) and the replay-only repair stay UNBUILT. Option 2 is clint's call. Advisory A1 (the
  setting key and log text for OpenConsole) is deferred until clint picks option 2.

## State of the work

The design went through Mercurius round 1 (C1 pending state, C2 revalidate, A1 lookup order, `f14b9e1`) and round
2 (re-tell on cancel or supersede, `9022a87`). @terminal approved option 1 and asked for it to be built.

Built in `ab53c5c`:
- `internal/daemon/supervisor.go`. `setViewport` and `dropViewport` now call `applyViewport`, which applies the width
  at once at the APPLIED rows, then `holdHeight`. `holdHeight` covers three cases: the height came back (cancel and
  re-tell), the same value is still pending (keep counting), or a new value (restart the timer, and re-tell if it
  superseded one). `applyHeld(gen)` runs when the timer fires. It takes resizeMu, returns on a stale gen, clears
  the pending height, and returns after `r.done`. Then it re-reads the views, applies only if they still agree on
  that height, and re-tells otherwise. Runner fields: `pendingRows`, `pendingGen`, `pendingTimer`, and `hold` (0
  means the const `heightHold` of 500 ms, a field so tests can shorten it).
- `internal/daemon/height_hold_test.go` has 10 tests, one per rule. They fire the timer by hand, except one
  real-timer test.
- Existing tests adjusted: `resize_order_test.go` (1 ms hold, waits for the settle). `attach_width_test.go` and
  `width_floor_test.go` now assert width only, because the height waits out the hold and those attaches last under
  0.5 s.
- Harness knob `ATRIUM_CONPTY_VIA=runner` (`conpty_scroll_repro_test.go`, plus `harnessViaPTY` in
  `conpty_harness_repro_test.go`) sends each flip through a runner's viewports.
- Docs: item 74 status and table row, `docs/changes/74.md` rewritten (changelog plus test plan steps 1-5),
  and a paragraph in `docs/terminal-resize-decoupling-design.md`.

Tested, and all passed:
- `go vet ./internal/daemon/`
- `go test ./internal/daemon/ -count=1 -run 'Held|Hold|Height|Viewport|Viewer|Resize|Width|Attach|Replay|Size|PTY|Floor|Shell|Carry|Wake'`
- `go test -race ./internal/daemon/ -count=1 -run 'Held|Hold|Height|Concurrent|Viewer|Wake'`
- The harness, inbox ConPTY, 50-40-50 flips (`build.claude/lost-lines/run.ps1`, which now writes to
  `build.claude/lost-lines/caps/` and reads `VIA`):

  | Run | Resizes landed | Lines lost (fixed grid / grid follows) |
  | --- | --- | --- |
  | stream 300, direct | 8 | 10 / 40 |
  | stream 300, VIA=runner | 0 | 0 / 0 |
  | loop 30, direct | 22 | 56 / 72 |
  | loop 30, VIA=runner | 0 | 0 / 0 |
  | loop 30, FLIP_HOLD=800, direct | 4 | - / 2 |
  | loop 30, FLIP_HOLD=800, VIA=runner | 1 | - / 2 |

  Run it as `$env:RESIZE="flip"; $env:FLIP_ROWS="40"; $env:LOOP="30"; $env:VIA="runner"; run.ps1 <name> "" "" 300`.

## What is left

1. Merge `claude/main` into `claude/lost-lines` and resolve any conflicts. Item 53's resizeMu is already on this
   branch through the earlier merge. Watch `supervisor.go` around `setViewport`, `dropViewport` and the runner fields.
2. Rerun vet and the targeted tests above on the merged result.
3. `atrium_report` status `done` to @terminal with the merge sha. The summary covers the harness table above and the
   one behaviour change worth noting: the first attach at a new height now takes 0.5 s to reach the pty, and a
   viewer that is really shorter runs under a taller pty for that half second.
