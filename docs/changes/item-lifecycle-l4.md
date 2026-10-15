# Item lifecycle, stage L4

Backlog item f-item-lifecycle-l4. Stage L4 of `docs/rnd/item-lifecycle-design.md`: `tested` and `reviewed`, with the
change record's C1 and C2 absorbed.

## What changed

- **`atrium_record`** (a worker tool, `POST /_hub/work/record`) records a test run: `cmd`, `result` pass or fail,
  optional `duration_s`, and a `log` or a `log_file`. The hub finds the item the card is attached to and asks the
  card's room (`POST /v1/tasks/{id}/stamp`) to read the card's own worktree. The room answers HEAD, the branch and a
  dirty flag (atrium's own BRIEF.md, REPORT.md and LEARNINGS.md do not count). The fact is written `reported`, at that
  sha. A card names no sha: the decoder refuses `sha` and `at_sha` by name, and `kind` can only be `test`.
- **The log** is kept in the room's state dir, `items/<id>/tests/`, folders 0700 and files 0600, outside every repo.
  A `log_file` resolves through `internal/safepath` against the card's directory. The tail (512 KB) is kept.
- **Branch rule.** A record from a worktree on another branch is refused, naming both. An item with no branch yet
  adopts the first non-main branch no other item holds. A detached HEAD or a main branch is refused.
- **Tested** shows the newest test at the branch tip (a test atrium saw beats a reported one), else the newest test
  there is, as `stale` with "N commits ago" (review-only commits are not counted) or "not on the branch any more". A
  run with uncommitted changes says "plus uncommitted changes". A failed run is `active`.
- **Reviewed** is read from verdict trailers on the item's own commits through `deployready` (`Checker.Coverage`,
  the same matcher a deploy uses, so a hold wins the same way), or from a review fact. Partial coverage is `stale`,
  "covers 1 of 2 commits". Trailers are never stored, so a rebase cannot leave a copy that still says ok.
- **A card cannot write a review.** `POST /_hub/work/<id>/review` is loopback-operator only, refuses a request carrying
  `X-Atrium-Agent`, takes no sha, and pins the verdict to the branch tip the hub reads itself. `atrium_review` does
  not exist yet, so this route is its intake.
- **Ready.** `Lifecycle.Ready` is tested and reviewed at the tip, with the test SEEN by atrium on a clean tree. A
  reported test never makes an item ready, and `not_ready` says why. The step rows carry an `observed` or `reported`
  tag (migration 0027, `work_step.tag`, plus an index on `work_fact`).
- A landed item keeps the tested and reviewed rows it had, since its own range is empty once it is on `claude/main`.
- `gitsync.Store.Checker` is how the hub wires deployready in: deployready's tests import gitsync, so gitsync cannot
  import it.

## Things to know

- Cards on the hub's machine can reach the loopback routes, the same posture as approve.
- The migration is 0027, after L5's 0026_price_work_usage.
- No board strip yet: the items board is L6/L7.

## Test plan

Use a test hub and room on other ports with a temp data dir, never the live ones.

## @LETTER@. Record and readiness

### @LETTER@1. A test then a commit shows "1 commit ago"

1. Launch a builder with `--item <id>`, commit, and call `atrium_record` with `cmd` and `result: pass`.
2. Commit once more and push the branch. `GET /_hub/work/<id>`.

**Expected:** `tested` is `stale` and says "1 commit ago".

### @LETTER@2. Uncommitted changes are said

1. Edit a tracked file, do not commit, and call `atrium_record`.

**Expected:** the answer has `dirty: true` and `tested` says "plus uncommitted changes".

### @LETTER@3. Refusals

1. Call `atrium_record` from a worktree on another branch.
2. POST `/_hub/work/record` with a `sha` field, and with `kind: review`.
3. POST `/_hub/work/<id>/review` with `X-Atrium-Agent` set.

**Expected:** 409 naming both branches, 400, 403, 403. No fact is written.

### @LETTER@4. A reported test is not ready

1. Record a passing test at the tip and add a verdict trailer covering the branch.

**Expected:** `ready` is false and `not_ready` says the test is reported, not seen.
