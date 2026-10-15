# Item lifecycle, stage L3

Backlog item f-item-lifecycle-l3. Stage L3 of `docs/rnd/item-lifecycle-design.md`: the steps git and the builds can
see, `landed`, `deployed` and `pushed`.

## What changed

- **Landed** is ancestry, not patch-equivalence: the item's build tip is an ancestor of `claude/main` on the hub's
  mirror. Its sha is the first commit on `claude/main`'s first-parent line that holds the tip, which is the tip itself
  for a fast-forward and the merge commit otherwise. `gitsync.Store.LandFacts` reads it.
- **Pushed** asks the forge once (`ls-remote` for `main`) and checks the landed sha is an ancestor of that tip. A forge
  that does not answer, or a tip the mirror has not fetched, falls back to the mirror's own `main`, and the step's
  reason says so.
- **Deployed** compares the landed sha with the build each process reports. The room already sends its build commit in
  its hello when it dials, so no room change was needed. The scope is `deployready.Needs` and `RoomSide`: hub-only
  code needs the hub, any other shipped code needs the hub and every attached room. A room whose build the mirror does
  not hold, or that reports none, counts as behind and is named in the step's reason. A `doc` item, or one whose files
  are all docs, tests and images, is `skipped` with the reason. Files that cannot be found are not a skip: the item is
  taken to need the hub and every room, as the deploy queue takes an unreported room.
- **Where it shows.** `GET /_hub/work/<id>` carries `steps`, derived fresh and stored in `work_step`. They are also
  derived after each mirror pass that moved, each collect and each room attach (`gitsync.Hub.Observed`), and every 30
  seconds.
- **Naming the branch.** `work.branch` has no writer until the build step lands, so `POST /_hub/work/<id>` takes
  `{"branch": "...", "kind": "code|doc|mixed"}`, from the hub's own machine only. An item with no branch has no git
  steps.
- **Migrations** `0023_work_step` and `0024_work_paths` (the files an item changed, remembered because once the branch
  is on origin's main its own range is empty). `work_step` is the design's table and is `IF NOT EXISTS`, so the step
  engine's migration may share it. Expect to renumber on landing.
- The derivation is `DeriveGit` in `internal/link/lifecycle_git.go`, pure, so the step engine can call it.

## Test plan

## @LETTER@. Git and build steps

### @LETTER@1. Landed

1. On a test hub (other ports, temp data dir), name a branch for an item with `POST /_hub/work/<id>`, and have it on
   the hub's mirror but not on `claude/main`.
2. Fast-forward `claude/main` onto it. Wait for the next mirror pass.

**Expected:** `GET /_hub/work/<id>` shows `landed` active before and `done` after, at the branch tip.

### @LETTER@2. A room on an older build

1. With a room attached that runs a build older than the landed sha, read the item.

**Expected:** `deployed` is `active` and its reason names the room and its build. Restart that room on the new build
and `deployed` becomes `done`.

### @LETTER@3. A doc-only item

1. Land an item whose files are only `*.md` and `changelog/`.

**Expected:** `deployed` is `skipped` with the reason, and `pushed` follows origin's main.
