# HANDOFF: @terminal, director of the terminal path (claude/terminal)

Written 2026-09-29. Re-read `BRIEF.md` and `DIRECTOR.md` in this worktree first, then this file. My handle is
`terminal-director-of-pty-to-xterm-and-sc`. The orchestrator is `atrium-87300` (alias `orchestrator`) and the merger
is `merge` (card 01a0eb20).

## Rules that changed since DIRECTOR.md was written

- ASK the orchestrator before EVERY worker launch, in one line: the item, a one-line reason, and the room.
- A design goes into backlog-2, then a Mercurius round (`mercurius_open_session` with `working_dir` this worktree,
  then `mercurius_start_review_round`) before anything is built. Artifact names must be plain file names. Wait with
  `pwsh -File C:/Users/claude/AppData/Local/Temp/claude/D--worktrees-claude-atrium-terminal/47c4cb02-0c4f-4de1-8b5e-636cc3ac6187/scratchpad/waitmerc.ps1 -Status <.mercurius/<session>/status.json> -Round N`
  in the background.
- The orchestrator wants NO progress reports. One report per batch, when it lands or when it is blocked.
- Exit a worker BY CARD ID once it is merged into claude/terminal. Remove its worktree and branch after @merge lands
  it.
- Directors do not write feature code. Test, comment and fixture fixes after a merge are fine, and so are backlog-2
  edits. `docs/changes/<item>.md` follows `docs/changes/README.md`. Nobody edits `CHANGELOG.md` or
  `docs/test-plan.md`.
- A throwaway room gets a private `ATRIUM_LOCATION`, `ATRIUM_SHARED=-`, its own ports, and a FRESH EMPTY database,
  never a live copy. Stop only its own pid.
- No gcc here, so `go test -race` cannot run.
- Hooks: no `;` chaining, no `>` (use `tee`), no `cd x && y`, no `git -C`, no `git checkout`, no perl.
- Backlog numbers are shared. Ask the orchestrator for the next free one (94 was next after 93).
- DO NOT touch `D:/worktrees/claude/atrium/sa89` (@runtime's worker).

## State

claude/terminal head is `dfbc8c8`. It merged claude/main `216c23c`. Not yet on claude/main:
`497716f`, `2d62335` (item 93 design revisions), the sa93 merge of `642675a`, `c68fe47` (test fix) and `dfbc8c8`
(scrub notes). The full suite `go test -p 4 ./...` was green on `c68fe47` with the fixtures present.

- **Item 93: WAITING ON CLINT'S YES to commit the three fixtures to the public repo.** They are UNCOMMITTED in
  `internal/daemon/testdata/scrollback/` (`lostlines`, `session-reply`, `session-tools` `.scrollback`), in this tree
  AND in `D:/worktrees/claude/atrium/sa93`, byte-identical. Without them a clean checkout fails the frozen tests,
  so the batch must NOT go to @merge before they are committed. Account usage in the statusline is zeroed, and every
  rendered frame was checked. What is left for clint is in the item 93 status in backlog-2. On a yes: commit the
  three files on claude/terminal, rerun
  `go test ./internal/daemon/ -run 'LostLines|LongReply|RealSessions|Spinners|ReplayOutput|EveryReal' -v`, then
  one `atrium_say` to `merge` with the branch, the head sha, the suite result, and a request to remove
  `TestRealSessionsKeepTheirText` from `$Flaky` in `scripts/merge-check.ps1` (their file). Then also drop the
  "known noise" mention in `DIRECTOR.md` and `docs/cold-start.md` (ask @merge, or do the cold-start one in the
  batch). On a no: the fallback is a synthetic corpus, and that needs a new design round.
- **sa93** (card 01a0ebb6) was asked to exit. KEEP its worktree `D:/worktrees/claude/atrium/sa93` and branch
  `claude/sa93` until the batch lands, because it holds the other copy of the fixtures. Remove both after.
- The lost-lines capture did not reproduce item 74 (300 of 300 lines on a build with the height hold). The test is
  now the unskipped `TestLongReplyThroughResizesKeepsEveryLine`. A real repro needs a build without the hold and
  belongs to item 74.
- **Item 74: WAITING ON CLINT**, option 3's diagnostic report first versus option 2 (OpenConsole ConPTY). See
  "2 or 3, for clint" in item 74. Also waiting on clint: a C toolchain for `-race`.
- Mercurius `s_5q0QKZVeAa30` (item 93) is done: round 2 was ready_to_build. It can be closed.
- The fixture scrub tool is `C:/Users/claude/AppData/Local/Temp/claude/D--worktrees-claude-atrium-terminal/dda8cb14-c1ee-4f3e-8c57-cdc2fc00eab4/scratchpad/scrub/main.go`
  (`go run` it with the fixture paths). The recapture steps are in `testdata/scrollback/README.md`.

## Next

1. Wait for clint's answer on the fixtures and on item 74. Nothing is running and no worker is up.
2. Queue left from BRIEF.md: 54 (terminal suite part 2, `screen.go` against xterm.js) is not started. Ask the
   orchestrator before launching.

## How to check the work

`git log --oneline claude/main..claude/terminal`. Item 93 is in backlog-2 under "## 93." with its build notes and
the list for clint. `git status --short` shows the three fixtures untracked, plus BRIEF.md and DIRECTOR.md.
