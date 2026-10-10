# u-pulls-panel-stage: the panel is a stage, not the row's state

Every row on the pulls tab said, in red, "failed: panel: no reviewer in the panel applies, or none of their agent files
was found", while the review behind it worked. The panel failure is one stage of the review, not the pull request's
state. See `docs/changes/img/pulls-panel-stage-before.png` and `docs/changes/img/pulls-panel-stage-after.png`.

## What changed

- `internal/daemon/prrunner.go`: a panel with no reviewer to run, or whose reviewers all fail, no longer returns an
  error from its step. It records `outcome` (`skipped` or `failed`), a short `note` and the long reason on the panel
  step in `review.json`, and the review carries on with no panel findings. A review that cannot start (no fetch, no
  prime, no recipe) is still failed as before. A stopped run and the budget stop are unchanged.
- `internal/api/prs.go`: a row carries `stage_note`, read from `review.json`, for every state except `queued` and
  `failed`. No schema change: the note lives in the run folder, so there is no migration.
- `internal/api/web/js/pulls.js`, `css/pulls.css`: the note is one muted line under the row (`.pull-stage`), in the
  colour of the why and second opinion lines. The state words come from the row's own state.

## Decisions made without asking

- A panel that ran and lost every reviewer is treated like a panel that found none. The review goes on and the row says
  `panel failed: every reviewer failed`. Failing the row is what produced the bug.
- A review that goes on with no panel findings still ends `ready`, with whatever the critics and merge produce. The
  muted line is how a person sees the panel did not run.
- The note is read from `review.json` on the way out and not stored on the row, to avoid a migration. A retry runs the
  panel again and replaces the note.
- The long reason (the room's agents folder and the fix) stays on the step in `review.json` and in `run.log`.

## Test plan

## @LETTER@. The panel is a stage on the pulls tab

### @LETTER@1. A room without the reviewer agent files

1. On a room with an empty agents folder, start a review of a pull request from the pulls tab.
2. Wait for the review to end.

**Expected:** the row is `ready to walk`, not failed. Under it is one muted line, `panel: missing agent files` and the
file names. The line is not red.

### @LETTER@2. A review that cannot start

1. Start a review of a pull request that does not exist on the forge.

**Expected:** the row is `failed: fetch: ...` in the row's state words, with retry and log buttons, and no panel line.
