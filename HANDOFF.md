# HANDOFF: @runtime director, claude/runtime, 2026-09-29 ~00:15

Read BRIEF.md and DIRECTOR.md first. They are the job and the rules. This is the state.

## Rules added tonight by the orchestrator (atrium-87300, alias orchestrator)

- ASK before every worker launch, one line to atrium-87300: item, one-line why, which room. Launch only on a yes.
- A design gets a Mercurius round first (mercurius_open_session, mercurius_start_review_round, then
  mercurius_collect_round) on a STANDALONE design file. A round over the whole backlog-2.md goes off target.
- One card per worker. To refresh a worker's context, POST `/v1/tasks/<id>/new-context` on the board
  (127.0.0.1:7778). It captures, clears and wakes. It fails if HANDOFF.md's mtime is older than the capture start,
  so touch the file right after the POST if the worker has already written it.
- A worker that reports `done` gets its card moved to `done`, and atrium_say then refuses it (item 83). To reach it:
  `curl -X PATCH -H "Content-Type: application/json" -d "{\"status\":\"needs-input\"}" http://127.0.0.1:7778/v1/tasks/<id>`
- Change files: `docs/changes/<item>.md`, in the format of docs/changes/README.md. Design docs go under docs/, never
  docs/changes/, or the fold script rejects them. Check with `pwsh scripts/fold-changes.ps1 -DryRun`.
- Batch check: `pwsh scripts/merge-check.ps1 -Base claude/main -Board` (the go suite, the board with headless,
  skins, the build). Playwright comes via NODE_PATH=D:\worktrees\claude\atrium\merge\node_modules.
- Past about 150k context, write HANDOFF.md and ask the orchestrator for a new context.

## Done

- **31** merged to claude/main (52ca01a). sa31's worktree and branch are removed. `docs/background-hold-design.md`.

## In flight

### Batch on claude/runtime, NOT yet sent to @merge: items 21, 83, 84 (filings), plus claude/main merges

- **21 (sa21, card 01a0eb28-41be-7097-a0d1-054d7fffdc2e, D:\worktrees\claude\atrium\sa21, claude/sa21)**, the
  looks-idle badge. Merged into claude/runtime at 8c6d5c8. The classifier renders the ring through screen.go and reads
  the live grid. OPEN, time-boxed to 15 minutes, sent to sa21 about 00:05: a real settled idle frame (tlsuv-fix-ci,
  `D:\worktrees\claude\atrium\sa21-frames\01a0e9aa-settled.tail.bin`) has a custom status line, so there are several
  rows under the bottom rule and the classifier most likely reads it as working. sa21 is to add it as
  `testdata/frame-settled-statusline.bin` with an idle test, let the footer span up to about 4 rows, and update the
  caveat in docs/changes/21.md. When it reports: review, `git merge --no-ff --no-edit claude/sa21`, and move its card
  back to needs-input (PATCH above) first if you need to say anything. If it overran, ship as is with the caveat kept
  in docs/changes/21.md.
- **83** filed at 92ae6f9: atrium_say refuses a done card whose terminal is alive. Not started. Owned by @runtime.
- **84** filed at cb7483f: nosession tests assume PID 1 is dead, which fails on macOS/Linux. Test-only fix in
  `cardFor` (help_test.go:34). Details are on m1mini in `~/atrium-m1mini-findings.md`. Being built by sa84 (below).
- merge-check was running (background) on the tree before sa21's footer fix, and needs a rerun after it anyway.
  Known noise: TestRealSessionsKeepTheirText failed under load AND alone. It reads live room scrollback (one card at
  26% of sampled words), not code from this batch. Say so in the @merge request.
- Then: `git merge --no-edit claude/main`, fold dry run, merge-check, and ONE atrium_say to `merge` with the branch,
  head sha and result. After @merge confirms: merge claude/main back, exit sa21
  (atrium_exit 01a0eb28-41be-7097-a0d1-054d7fffdc2e), then
  `git worktree remove --force D:\worktrees\claude\atrium\sa21` and `git branch -d claude/sa21`. Delete
  D:\worktrees\claude\atrium\sa21-frames.

### Workers running

- **sa41 (item 41), card 01a0eb3d-e8f2-7e2e-bae2-462ab180268c**, local, D:\worktrees\claude\atrium\sa41, claude/sa41.
  A card owes its launcher a report only for the launcher's own prompts. Design first, in
  `docs/owed-report-design.md`, plus a Mercurius round. A migration is allowed. It must not touch the seen state.
  Merge order agreed with @ui: their sa43 (item 43, handleStop and seen.go) lands first, and sa41 merges claude/main
  right before it reports. No report yet.
- **sa73 (item 73), card m1mini~01a0eb3e-6bf2-7c54-89e6-7594c1865956**, on the m1mini room, worktree
  ~/git/wt/sa73 there (branch claude/sa73, off hub-main=52ca01a). The keep-alive fork carries the card's model,
  effort, args and lean. Its brief: export PATH for go/node, ignore the 2 nosession failures (item 84), Mercurius
  may not work there. It hands back `~/sa73.bundle` (`hub-main..claude/sa73`). To land it:
  `scp m1mini:sa73.bundle <scratch>`, `git bundle verify`, `git bundle unbundle`, then
  `git update-ref refs/heads/claude/sa73 <sha>`. The local claude/sa73 branch exists at 52ca01a with no worktree.
  No fetch or pull. The ssh host is `m1mini`. `git -C` works inside the ssh command string. No report yet.
  **PROMISE: tell @fabric when sa73 is done** (atrium_say). The m1mini room restart itself is skipped until clint
  says so, so just tell them.

- **sa84 (item 84), card m1mini~01a0eb58-bcf0-7c38-b3ab-7224249b49fc**, on m1mini, worktree ~/git/wt/sa84
  (claude/sa84 off hub-main=52ca01a), launched with the orchestrator's yes. The orchestrator's rule for it: NO git
  bundle. The branch comes back through clint's room-git fetch in the morning, and the final report carries the full
  `git diff hub-main..claude/sa84`. Review that diff, then it's clint's fetch that lands it. The same rule most
  applies to sa73 too, as the orchestrator confirmed: sa73 may still write ~/sa73.bundle on m1mini, which is
  harmless, but NOBODY scp's or unbundles it here until clint decides. Its branch comes back through clint's room-git
  fetch like the others, so review sa73 from the diff in its report, or over ssh read-only
  (`ssh m1mini "git -C ~/git/wt/sa73 diff hub-main..claude/sa73"`). Ignore the bundle steps in the sa73 entry above.

HANDOFF.md is committed on claude/runtime so it survives the clear. `git rm` it before the @merge request, since it
must not reach claude/main.

## NEWEST, ahead of anything above (00:25)

- **sa21 reported done at df4d0e6.** The settled capture reads idle, the footer allows up to 4 rows under the bottom
  rule, and the caveat is in docs/changes/21.md (the capture is pinned at 206x60, and a wrong width fails safe). NEXT:
  `git merge --no-ff --no-edit claude/sa21` into claude/runtime.
- **sa41 reported done at 916cec6** (branch claude/sa41, merged with claude/main, targeted tests pass, the full suite
  not run). It ADDS MIGRATION `0068_owed_at` (a task.owed_at column, backfilled from prompted_at), stamped in
  `promptOwes` in internal/store/a2a.go. The launch's opening prompt now carries from_peer. The rule: a card owes only
  for a prompt from its launcher, and an operator prompt no longer owes. Mercurius round 1 said needs_changes (C1,
  a reopen or resume blamed the launcher). It was fixed, and no second round was run.
  Review before merging:
  - check that 0068 is LAST in the slice and tolerates a rerun, and look at the INSERT change in tasks.go
  - make sure migration 0068 doesn't collide with a migration number on another branch:
    `git for-each-ref refs/heads/claude` plus `git grep 0068 <branch> -- internal/store/schema.go`
  - verify it against a COPY of the live db (store CLAUDE.md)
  - check that item 31's hold still works
  Then merge it, and this batch becomes 21 + 41 + the 83/84 filings.
- @fabric withdrew the sa73 question: nothing needs to be told to @fabric about m1mini tonight.
- Then: `git rm HANDOFF.md`, `git merge --no-edit claude/main`, fold dry run,
  `pwsh scripts/merge-check.ps1 -Base claude/main -Board`, one atrium_say to `merge`, and one batch report.

## Queue after these (backlog-2 numbers)

32 (a queued say from http-support never produced a backlog entry, give a say a lifecycle on record), then 83,
then specs only for 38 and 39 (Open Questions for clint, do not build).

## Notes for the batch report to the orchestrator

- Mercurius: 31 got ready_to_build on round 2. Round 1, over the whole backlog, was off target. 21 got
  ready_to_build on round 1, but record_round_notes failed with "session not found".
- For item 66: the new-context check is on mtime only, so a worker that already wrote HANDOFF.md fails it twice.
- m1mini works as a room: launching there, a worktree over ssh, and CLAUDE.md copied in by scp (the clone has none).
