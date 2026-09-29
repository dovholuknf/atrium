# HANDOFF: @ui, director of the board, branch claude/ui

Read BRIEF.md and DIRECTOR.md first. They still hold. Written 2026-09-29 around 03:45 UTC, updated at the cycle
with the headless result. The orchestrator already has the three open questions below, and wants every turn to end
with `atrium_report`.

## Rules added since BRIEF.md (from the orchestrator, atrium-87300)

- Ask the orchestrator with one `atrium_say` line (item, why, room) before each worker launch. It grants slots.
- A design gets a Mercurius review round (`mercurius_open_session` with working_dir this worktree, then
  `mercurius_start_review_round`) before it is built. Put the outcome in the batch report.
- End every turn with `atrium_report` to atrium-87300.
- Past about 150k context, write HANDOFF.md and cycle.
- Hooks refuse `;`, `cd x && y`, `git -C`, `>` and python in Bash. Use a .ps1 in the scratchpad run with `& path` to
  work in another worktree (`Set-Location` inside it).

## State

claude/ui head: the sa44 merge, then claude/main, then the item 80 design commits (`git log --oneline -8`).

Done and merged into claude/ui:
- 78, sa78 `3fe3838`, plus my commit fixing two tip strings. Worker exited.
- 44, sa44 `543239d`. Worker exited.
- Designs filed in docs/backlog-2.md: 43 (1 Mercurius round, C1 folded in), 79 (not reviewed yet, queued), 80
  (3 rounds, all findings folded in).

In flight:
- **sa43** (item 43) reported DONE at `2e4443e` on claude/sa43. I reviewed the diff and it is good. NOT merged yet. Its
  diff against claude/ui also shows item 31's code, which is from claude/main (52ca01a) and expected.
- **sa80** (item 80, usage charts, clint wants it tonight) is RUNNING in D:/worktrees/claude/atrium/sa80, card
  01a0eb39-9ae9-7def-a47a-d2d40ade9d82. It reports to this card. It goes in the NEXT batch. At the cycle it was at
  152k context, and I told it to commit, write its own HANDOFF.md and report progress asking for a new context.
  When that report comes, start the new-context sequence on its card (or ask the orchestrator to), so it boots from
  its HANDOFF.md.

Batch suite on claude/ui before the sa43 merge: `go test -p 4 ./...` failed only TestRealSessionsKeepTheirText
(known noise). check-board.sh passed. The headless full run was still going at the cycle (log:
C:/Users/claude/AppData/Local/Temp/claude/D--worktrees-claude-atrium-ui/c7720565-5f3e-4a42-9025-68bf67ea86a6/scratchpad/headless.log).
By then it had 13 FAILs, ALL in the atrium-down and restart-cover section: "a reload with atrium down did not load
a page: net::ERR_EMPTY_RESPONSE", down.html clock empty, restart cover not drawn, and "the atrium-down pages threw:
TypeError: Cannot read properties of null (reading 'open')". None of 78, 44 or 43 touches down.html or the restart
cover, so check that section on claude/main before blaming this branch: in a scratch worktree of claude/main, or by
`git stash`-free means such as running `HEADLESS_ONLY=<that section>` on claude/ui and again on a claude/main
worktree. Item 28 in backlog-2 says the full headless run fails often on claude/main. If it fails the same on
claude/main, hand to @merge saying so. If only on claude/ui, find which merge broke it before handing off.

## Next steps, in order

1. `git merge --no-edit claude/sa43` into claude/ui (commit merges with `git commit --no-edit --cleanup=strip` if it
   stops on a conflict).
2. Clear ATRIUM_LOCATION and ATRIUM_DEBUG_INPUTLAG, then run `go test ./internal/daemon ./internal/store
   ./internal/link`, `bash scripts/check-board.sh`, and the full headless run
   (`NODE_PATH=D:/worktrees/claude/atrium/attach-loop/node_modules node scripts/test-board-headless.js`). Known
   noise: TestRealSessionsKeepTheirText, the link restart-gate timing tests under load, and toastLives timing.
3. One `atrium_say` to @merge (alias `merge`, card 01a0eb20): branch claude/ui, head sha, suite result, items 78,
   44, 43.
4. Exit sa43 (`atrium_exit` card 01a0eb3a-f39c-7890-953b-ef531059e4b5). After @merge confirms the merge, remove the
   sa78, sa44 and sa43 worktrees and branches (`git worktree remove`, `git branch -d`).
5. When sa80 reports: review, merge, suite, hand to @merge the same way.
6. Then ask the orchestrator for a slot for 79. After that, specs only for 50 and 71 (BRIEF.md item 4).
7. I told @runtime about 43 touching the seen state. Their sa41 will rebase onto sa43 if 43 lands first. Nothing
   owed.

## Open questions for clint (already relayed by the orchestrator)

1. Item 44: the board has no per-card notification on/off, so a card given its own tone counts as the override and
   is not muted. OK?
2. Item 79: should "off" also silence permission requests? The recommendation is no.
3. Item 80: is burn at turn end real-time enough, or should the room tail transcripts mid-turn?
