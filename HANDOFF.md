# HANDOFF: @terminal, director of the terminal path (claude/terminal)

Written 2026-09-29 about 04:10 at the 150k context line. Re-read `BRIEF.md` and `DIRECTOR.md` in this worktree
first, then this file. My handle is `terminal-director-of-pty-to-xterm-and-sc`. The orchestrator is
`atrium-87300` (alias `orchestrator`) and the merger is `merge` (card 01a0eb20).

## Rules that changed since DIRECTOR.md was written

- ASK the orchestrator before EVERY worker launch, in one line: the item, a one-line reason, and the room. Launch
  only after it says yes. The cap is set by the orchestrator (up to 10 across all directors).
- A design goes into the backlog-2 section, then gets a Mercurius review round (`mercurius_open_session` with
  `working_dir` set to this worktree, then `mercurius_start_review_round`) before anything is built. Put the
  outcome in the batch report. The `mercurius` CLI is not on PATH, so wait on
  `.mercurius/<session>/status.json` with `scratchpad/waitmerc.ps1` (edit the session id in it), run in the
  background.
- Exit finished workers as soon as they are merged into claude/terminal (slots are scarce). Remove their worktree
  and branch once @merge has taken them into claude/main.
- `docs/changes/<item>.md` must follow `docs/changes/README.md`: each entry starts `- **Title.**`, and the test
  plan uses `## @LETTER@. Title` and `### @LETTER@1.`. @merge had to reshape 53 and 61.
- There is no gcc on this machine, so `go test -race` cannot run. Do not believe a worker that says it did.
- Past about 150k context: write HANDOFF.md and ask the orchestrator for a new context.

## Done and landed

Batch 1 is on claude/main at `e45fc2c`, test-plan sections CP, CQ and CR:
- item 61 (sa61, `88fc53d`): the hub's echo clock ignores ping, pong and close frames.
- item 53 (sa53, `54016ed`, plus my gofmt `61c6c36`): resizeMu serialises setViewport and dropViewport. It
  reproduced 10 of 10 without the fix.
- item 54 (sa54, `1938e74`): a screen.go against vendored xterm.js differential in node, plus size tests.
- items 81 and 82 were filed in backlog-2 (`f1d871b`).
- The batch suite passed except TestRealSessionsKeepTheirText (known noise). The worktrees and branches for
  sa53, sa54 and sa61 are removed.

claude/terminal is at `e45fc2c`. claude/main has since moved to `2ca5636` (it contains claude/terminal). Merge
claude/main in before the next handover.

## Running workers

| Worker | Card | Branch, worktree | Item and state |
| --- | --- | --- | --- |
| sa74 | 01a0eac2 | claude/lost-lines, D:/worktrees/claude/atrium/lost-lines | 74: BUILDING option 1, the height hold |
| sa81 | 01a0eb55 | claude/sa81, D:/worktrees/claude/atrium/sa81 | 81: DECSTBM scroll regions in screen.go, building |
| sa82 | 01a0eb57 | claude/sa82, D:/worktrees/claude/atrium/sa82 | 82: wide cells. Design phase ONLY |

### Item 74 (sa74)

The cause is proven: the inbox ConPTY (conhost 10.0.26100) turns a fast pty HEIGHT flip into one bare `\e[H`
full-screen repaint that overwrites rows in place (live ring byte 2460305, 10 rows). OpenConsole 1.24 loses 0.
Claude Code is not implicated. The transient is the shortest viewer reattaching while another is attached: the
old socket drops before the new one sends its size, so the rows grow and then shrink back. The design is in
backlog-2 item 74 on claude/lost-lines (`f14b9e1` plus `9022a87`, the re-tell rule).

Mercurius session `s_X7i342IjsKnJ` (closed). Round 1 had C1 and C2 (state rule, revalidation), both fixed.
Round 2 had C1 (re-tell viewers on cancel), fixed. The A1 advisory (the OpenConsole setting key and log line) is
deferred.

I APPROVED option 1: the height is held 0.5s, the width applies at once, the timer takes resizeMu and revalidates
(gen, r.done, viewer count, agreed == pending != applied), and the viewers are re-told the size on cancel without a
resize. The tests plus a harness repro must show a 50-40-50 flip loses no rows. When sa74 reports done: review the
diff against the design, merge it into claude/terminal, and exit it.

Option 2 is the OPEN QUESTION, carried to clint by the orchestrator: ship OpenConsole ConPTY (conpty.dll plus
OpenConsole.exe, MIT, about 1.2MB on x64) behind a setting with inbox as the fallback. It needs item 81 first and
a full terminal test-plan rerun. Option 3 (a replay-only repair) only if 2 is refused. Neither is built.

### screen.go merge order: sa81 FIRST, then sa82

sa82's design is committed at `874d51d` (item 82 "Design" in backlog-2). It follows xterm 5.5.0 UnicodeV6 widths
through a generated table with no new dependency, and adds a continuation-cell sentinel. Mercurius session
`s_v2plA2V8cqDd` (closed): ready_to_build with no concerns. A1 (a wide character in the last column) is deferred to
the differential.

UPDATE 04:11: sa81 is DONE (`f645161`), reviewed, merged into claude/terminal, targeted tests pass here, and it
has exited (remove its worktree and branch after @merge). sa82 has its GO: it merges claude/terminal into
claude/sa82 and builds phase 2. Now running: sa74 and sa82 only.

Both workers know the order. sa81 keeps its function shapes stable once it reports. sa82 writes its design (a
"Design" subsection of item 82 in backlog-2) and atrium_says me the sha. Then I:
1. run a Mercurius round on sa82's design (a new session, `working_dir` this worktree), and triage it to sa82.
2. When sa81 reports done: review it, merge it into claude/terminal, and exit it.
3. Tell sa82 to go: merge claude/terminal into claude/sa82, then build phase 2.

## Next

`git rm` this HANDOFF.md in a commit before handing the branch to @merge, so it never lands on claude/main.

When sa74, sa81 and sa82 are merged: merge claude/main in, run `go test -p 4 ./...` once
(`scratchpad/suite.ps1`), hand the branch to @merge with one atrium_say (branch, head sha, suite result), then
send ONE atrium_report batch report to the orchestrator: the items and shas, the suite result, the Mercurius
outcomes, and the open questions (OpenConsole, and a C toolchain for -race). The terminal queue from BRIEF.md is
otherwise empty.

Scratchpad: `C:/Users/claude/AppData/Local/Temp/claude/D--worktrees-claude-atrium-terminal/075e4391-05a2-4d3f-a248-c6f697238fe1/scratchpad`
(suite.ps1, waitmerc.ps1, cleanup.ps1).
