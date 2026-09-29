# HANDOFF: @runtime director, claude/runtime, 2026-09-29 ~00:55 (confirmed current at the capture)

Read BRIEF.md and DIRECTOR.md first. They are the job and the rules. This is the state. `git rm` this file before
the next @merge request, because it must not reach claude/main.

## Rules from the orchestrator (atrium-87300), still in force

- ASK before every worker launch, one line to atrium-87300 (item, why, room). sa32 and sa83 were pre-approved.
  Any worker after them needs a yes first.
- A design gets a Mercurius round first, on a STANDALONE design file.
- To refresh a worker's context: `curl.exe -s -X POST http://127.0.0.1:7778/v1/tasks/<id>/new-context`. If the worker
  already wrote HANDOFF.md, touch it RIGHT AFTER the POST, in the same command, or the capture check fails (it did
  once tonight): `curl.exe ... ; Start-Sleep -Milliseconds 500; (Get-Item <wt>\HANDOFF.md).LastWriteTime = Get-Date`.
- A worker that reports `done` has its card moved to `done`. Item 83 (now merged here) lets atrium_say reach it again
  once the room runs it. Until then, the workaround is a PATCH to needs-input:
  `curl -X PATCH -H "Content-Type: application/json" -d "{\"status\":\"needs-input\"}" http://127.0.0.1:7778/v1/tasks/<id>`
- Change files go in `docs/changes/<item>.md`. Check with `pwsh scripts/fold-changes.ps1 -DryRun`.
- Batch check: `pwsh scripts/merge-check.ps1 -Base claude/main -Board` with
  NODE_PATH=D:\worktrees\claude\atrium\merge\node_modules. Known noise: TestRealSessionsKeepTheirText (live scrollback).
- Send @merge a batch BY SHA, and tell it before adding commits after a sha it was given.
- Hooks: no `;` chaining, no `>`, no `cd x && y`, no `git -C` in Bash. `cd` alone first, then plain git. In
  PowerShell, `Remove-Item` next to `git branch` trips the branch hook, so run them separately.

## Landed

- **21 and 41**: on claude/main at db2b33a (@merge took claude/runtime 403ee9c, folded as CY and CZ, migration 0068
  included). claude/main is merged back into claude/runtime (2731f78).
- sa21: card gone, branch `claude/sa21` deleted, `sa21-frames` deleted. **Still owed: `git worktree remove --force
  D:/worktrees/claude/atrium/sa21` failed with Permission denied.** Something still holds it. Retry, and if it still
  fails, `git worktree prune` after the holder is gone.
- sa41 (card 01a0eb3d): asked to exit at ~00:52. **Still owed:** `git worktree remove --force
  D:/worktrees/claude/atrium/sa41` and `git branch -d claude/sa41`.

## Next batch on claude/runtime (NOT sent to @merge yet), head 2731f78

- **83 (sa83, card 01a0eb70-ef9d-7b7d-b1fa-fd8b10c029e8, D:\worktrees\claude\atrium\sa83, claude/sa83)**: reported done
  at 8bbdb92, reviewed by me, merged at 41c708c. `sessionGone` is a daemon method now: a done/dead card is gone only
  if its pid is dead AND the supervisor has no live runner with an unended session (newest launched vs exited event).
  `resolvePeer` (atrium tell and ask routing) and pendinginject use it too. The card stays in `done` on delivery. The
  targeted daemon tests pass on claude/runtime. sa83 is at its prompt: exit it and remove its worktree/branch after
  @merge lands the batch.
- **Specs 38 and 39, and item 87**: committed at 31bbfd0 (no longer uncommitted). `docs/restart-idle-spec.md` and
  `docs/keepalive-marked-spec.md`, Open Questions for clint, from session_usage on a copy of the live db. The 38
  finding is Open Question 1: a resume by itself never misses the cache, so parking idle cards saves processes,
  memory and restart time, and NO tokens.
- **87 filed in docs/backlog-2.md and DIAGNOSED, no worker needed.** The two full-write refreshes ($1.02 on 01a0e960,
  $0.76 on 01a0e8f0) were both `atrium:lean` cards, whose fork cannot rebuild their prompt. Already fixed by item 70,
  fa2b2cc (01:19Z), which skips lean cards. On the db copy no lean-tagged card was refreshed after that. The other four
  `miss` rows were effectively warm ($0.04 to $0.09 each). Close once a room running fa2b2cc or later shows no
  full-write refresh for a day. Next free item number: 88.
- Before sending: sa32 if it is done by then (or send without it), `git rm HANDOFF.md`, `git merge --no-edit
  claude/main`, fold dry run, merge-check, one atrium_say to `merge` with the sha.

## Workers running

- **sa32 (item 32), card 01a0eb71-6e8c-7791-9f78-e7b86dc7c573**, local, D:\worktrees\claude\atrium\sa32,
  claude/sa32. A say's lifecycle on record, a stale handle answers with candidates, and a reply owed. It hit 151k,
  handed off at 5fc3176, and I cycled it with new-context (clear and wake done at ~00:49). The state at handoff: the design
  `docs/say-lifecycle-design.md` passed Mercurius round 1 with nothing blocking. The store side is built with
  **migration 0069_say**, and store tests pass. The daemon did not compile yet (peers.go:332 needs the new reply
  argument), and the daemon, API and MCP wiring is unfinished. Its HANDOFF.md has the steps. Review when it reports:
  0069 last in the slice and tolerant of a rerun, no collision (`git for-each-ref refs/heads/claude` + grep 0069),
  verify on a COPY of the live db, that it does not touch `owed_at` (item 41), and no link/hub code.
- **sa73 (item 73)** and **sa84 (item 84)**, on m1mini: no report yet. Review the diff in the report (or read-only
  over ssh: `ssh m1mini "git -C ~/git/wt/sa73 diff hub-main..claude/sa73"`). Land NOTHING here: the branches come back
  through clint's room-git fetch. No scp, no bundle. Nothing to tell @fabric.

## Queue after these

Queue after these: nothing else from BRIEF.md. 32 is in flight, 83 is done, and 38 and 39 are specced. Then write the batch report to
the orchestrator (one atrium_report). Items: 21, 41 landed (db2b33a). 83 and the specs and 87 in the next batch.
Open questions for clint: the four in restart-idle-spec.md and the three in keepalive-marked-spec.md.

## Notes for the batch report

- Mercurius: 31 got ready_to_build on round 2 (round 1 over the whole backlog was off target). 21 got ready_to_build
  on round 1, and record_round_notes failed with "session not found". sa41's round 1 said needs_changes (C1, fixed,
  no second round). sa32's round 1 found nothing blocking.
- Item 66: the new-context check is on mtime only, and it failed once tonight because the touch landed after the check.
- sa21's board test (looksIdleSection) was never called in the full run. Fixed at 403ee9c. @ui's usageChartsSection
  has the same problem on claude/main, which I told @merge about.
