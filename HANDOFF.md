# HANDOFF: @terminal, director of the terminal path (claude/terminal)

Written 2026-09-29 at the 144k context line. Re-read `BRIEF.md` and `DIRECTOR.md` in this worktree first, then
this file. My handle is `terminal-director-of-pty-to-xterm-and-sc`. The orchestrator is `atrium-87300` (alias
`orchestrator`) and the merger is `merge` (card 01a0eb20).

## Rules that changed since DIRECTOR.md was written

- ASK the orchestrator before EVERY worker launch, in one line: the item, a one-line reason, and the room.
- A design goes into backlog-2, then a Mercurius round (`mercurius_open_session` with `working_dir` this worktree,
  then `mercurius_start_review_round`) before anything is built. Artifact names must be plain file names (no
  spaces or colons). The `mercurius` CLI is not on PATH: wait on `.mercurius/<session>/status.json` with a
  background pwsh loop (see the old `waitmerc.ps1` below).
- Exit a worker BY CARD ID once it is merged into claude/terminal. By alias the exit fails on a `done` card, and a
  runner left alive holds its worktree dir (item 89). Remove its worktree and branch after @merge lands it.
- Directors do not write feature code. Test and comment fixes after a merge are fine.
- `docs/changes/<item>.md` follows `docs/changes/README.md`. Nobody edits `CHANGELOG.md` or `docs/test-plan.md`.
- No gcc here, so `go test -race` cannot run. Do not believe a worker that says it did.
- Hooks: no `;` chaining and no `>` in Bash, no `git -C` and no `git checkout` (use `git restore`, `git switch`).
  Put multi-step git across worktrees in a .ps1 under the scratchpad.
- Backlog numbers are shared across directors. Ask the orchestrator for the next free one. 87 and 89 are taken
  (@runtime), and the next free was 89 when last asked, so check again before filing.
- Leave the gofmt issue (three test files from other departments) with @merge.

## Done and landed

- Batch 1 (items 53, 54, 61) on claude/main at `e45fc2c`.
- Batch 2 on claude/main (`cbefcb1` merged claude/terminal, folded in `db2b33a`, sections DA to DD):
  81 (sa81, `f645161`), 74 option 1, the 0.5s height hold (sa74, `387ccd5`), 82 wide cells (sa82, `8e0e68f`), and
  `bed0d36` (cursor_refit_test waits out the hold). Worktrees and branches for sa74, sa81, sa82 are removed.
- Batch 3, items 86 + 88 (sa86, `8fe9f09`, plus my comment fix `79056b4`), handed to @merge at claude/terminal
  `e77918d`. Mercurius `s_qf7b9mnicqAi` (closed): ready_to_build, advisory A1 adopted. LANDED on claude/main at
  `bb65176` (section DF). sa86 is exited and its worktree and branch are removed. No worker is running.

## Open questions, with the orchestrator

- Item 74 option 2: ship OpenConsole ConPTY behind a setting. Parked for clint. Option 3 only if 2 is refused.
- A C toolchain so `-race` can run.

## Next: item 33

Watching a terminal holds every message to it, word deletes miscount, and a gate debug readout. An old sa89
started it on 2026-09-28. First reconcile what already landed on claude/main, with SHAs (`git log claude/main
--grep` for 33 and sa89, and read item 33 in `docs/backlog-2.md`). Then design what is left into item 33,
run a Mercurius round, and ask the orchestrator for a slot.

Scratchpads: old `C:/Users/claude/AppData/Local/Temp/claude/D--worktrees-claude-atrium-terminal/075e4391-05a2-4d3f-a248-c6f697238fe1/scratchpad`
(suite.ps1 runs `go test -p 4 ./...` into suite.log, waitmerc.ps1). This context's
`.../4f9cc759-941c-4fbb-8461-5f818f4af33f/scratchpad` (bench.ps1 compares benchmarks old against new, cleanup.ps1).

`git rm` this HANDOFF.md in a commit before handing the branch to @merge again.
