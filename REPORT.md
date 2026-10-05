# f-pr-review-move report

## Built
- **Store** (`internal/store/prsmove.go`, no migration): `PRTransfer` (the row's fields as they travel), `PRByKey` (the
  live row, newest not archived, any state, case-folded), `PRTransferOf`, `PRRunDirTaken`, `ImportPR` (fresh id, claim
  `claimed`, no walker, an in-flight row arrives `queued` with run fields cleared), `ArchivePR`.
- **Room routes** (`internal/api/prsmove.go`, human listener only):
  - `GET /v1/prs/export?key=host/org/repo/number` answers a tar.gz: `row.json` first, then `files/<rel>`. The row id
    is in the `X-Atrium-PR-ID` header. Files come from `run_dir` through `safepath`, links are skipped, `src/` is left
    out (the fetch step rebuilds it from the hub). Over 64 MiB answers 413 before a byte is sent.
  - `POST /v1/prs/import` refuses a name outside `files/`, `..`, absolute, backslash, drive, a link or special entry, a
    repeated name, more than 20000 entries, or more than the cap (sent or unpacked). A refusal removes the folder and
    makes no row. A key with a live row here answers that row (200, `created:false`) and writes nothing. The folder is
    `RunFolderOn`'s, else `-moved-N` after it when an archived row holds the name (a review coming back to a room it
    left) or the folder has content. A `queued` arrival is started through the runner.
  - `POST /v1/prs/{id}/archive` aborts a queued, fetching or running row (marks `aborted`, then `Abort`), sets
    `archived_at`, keeps the folder.
- **Hub** (`internal/link/prmove.go`): `MovePRClaimWith(ctx, key, to, walker)`. Both rooms online or it refuses with
  409 and a sentence ("the review is on sg3, which is offline. bring it back or move it later"). Export is piped into
  import (no disk, no whole copy in memory). 404 on export means no review, so only the claim moves. A failed or
  refused import answers 502 with the room's sentence and leaves the claim and old row. Then the claim moves, then the
  old room archives. `MovePRClaim(key, to)` keeps its signature and calls it. `POST /_hub/pr-claims/move` takes an
  optional `walker` and answers `review` (`moved` or `none`) and `pr_id`.
- Tests: `internal/api/prsmove_test.go` (round trip with findings, `walk.txt` including `accepted`, row fields, live key
  answers and writes nothing, containment and link refusals, caps both ways, running row resumed, archive, return to
  a left room), `internal/link/prmove_test.go` (two fake rooms: copy then move then archive, no review, either room
  offline, failed import, same room). `TestManualMoveUpdatesTheClaim` was changed: the owner offline is now refused,
  and the move goes through once it is back.
- Changelog `changelog/fabric/2026-10-04-f-pr-review-move.md`, a line in Built of `docs/rnd/scm-forge-design.md`.

## Decisions
- `src/` is not archived: a checkout can be far over the cap and the fetch step recreates it.
- A failed archive on the old room after the claim moved is logged and audited (`pr-review-archive-failed`) and does
  not undo the move. The old row is then a stale live copy until archived by hand.
- "Live" for the export and the dedupe is any state that is not archived, so a `ready` or `failed` row counts.

## Not done
- **Walker (item 5), the handoff.** Nothing in this tree calls `MovePRClaim` besides the operator's route. I found no
  room handoff that moves a PR card and then calls it, so there is no caller that knows the new card id. The hub side is
  ready: pass `walker` to `POST /_hub/pr-claims/move` or to `MovePRClaimWith`, and it posts `walker set` on the new row.
  Whoever moves the card has to pass the id.
- A claim whose room is gone for good can no longer be moved (the brief requires both rooms online). There is no
  claim-only escape. Say if one is wanted.
- Test run: `internal/store` passes. `internal/api` and `internal/link` fail only the known ones (`TestPRWorktree*` on the
  signing key, the `NUL` link tests). The new tests all pass.
