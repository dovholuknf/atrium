# @fabric handoff

You are @fabric, the Director of Fabric: hub, rooms, cross-room, overlays and provisioning. Read BRIEF.md first
(the role and rules), then PLAN.md (item 1), then this file.

- Your handle is `fabric-director-of-rooms-hub-cross-room`, on room claude-sg4.
- You report to atrium-87300 (alias orchestrator), with atrium_report once per batch. Use atrium_report rather
  than atrium_say for status, because the ledger only records reports.
- First thing in the new context: one atrium_report to atrium-87300 saying you are up, with the head sha.

## Standing rules added since BRIEF.md

- **Ask atrium-87300 before each worker launch**, in one line: the item, why, and which room.
- **One card per worker.** To move a worker up to Opus, exit it and atrium_launch on the SAME worktree with the
  same title.
- **Make worker worktrees with `pwsh -NoProfile -File scripts/new-worktree.ps1 -Name <name> -Base claude/fabric`**, so
  the CLAUDE.md links are there. Write BRIEF.md into it yourself, then atrium_launch with a short `prompt` and NO
  `brief` param, so your file is not replaced.
- **Do not do heavy source reading on this card.** Ask atrium-87300 for a reading worker instead (fb05 is the model).
- **No stop, uninstall, -Remove, service or autostart test against ANY real room** (sg3, m1mini, claude-sg4,
  sg4-wsl). Those run on claudevm only (`ssh claudevm`, Windows, not signed in to claude).
- **No remote room restart while any card on it has a live session**, clint's included (atrium_peers rooms=true).
  Tell atrium-87300 first, every time. Never touch this machine's room or hub. No deploys, no push to origin.
- **@merge (card 01a0eb20) is the only writer of claude/main.** Merge workers into claude/fabric, merge claude/main
  in, then tell @merge the branch is ready.
- **Nobody edits CHANGELOG.md or docs/test-plan.md.** 77b landed: each item writes `docs/changes/<item>.md` in the
  format of `docs/changes/README.md` (`## @LETTER@.`, no fixed letter), and the merger folds it. fb03's
  `fabric-1-toolchain.md` will need converting the same way when it arrives.
- Past about 150k context, write HANDOFF.md and ask atrium-87300 for a new context.
- The Bash hook refuses `;` chains (even `do :; done`), `>`, `>>`, `2>&1`, `find`, `git -C`/`--git-dir` and
  `git -c`. A `cd` in Bash does NOT persist (the cwd resets), so run tests in another worktree with
  `go -C <dir> test ...`. Wait on things with a PowerShell `while` loop in the background. Use Grep for line-length
  checks (`^[^|].{120,}$`, since table rows run long).
- **No git command may reach a remote** (hook). `push`, `fetch` and therefore `room-git.ps1 push-base` and `fetch`
  are clint's to run.
- For multi-line remote scripts on Windows, use `powershell -EncodedCommand`. The ssh default shell on sg3 and
  claudevm is Windows PowerShell 5.1, so `&&` does not work there: use `;`.
- The local `scp` on PATH is a broken Cygwin build. Use `C:\Windows\System32\OpenSSH\scp.exe`.

## Branch state

- claude/fabric head: the handoff commit on top of 5c3c5f6, which merged claude/main bb65176. @merge told it is ready.

## Done this context

- **Item 49:** Mercurius `s_L4wmA1mqNRCP` round 1 ready_to_build, advisory folded in as FE10 (`1ccecf2`). Waits on
  clint's 3 questions in `docs/everywhere-card-design.md`.
- **Items 46 and 75 reconciled** in backlog-2 with SHAs, what is left, and 4 open questions each (`d55796b`). Index
  table rows for 46, 49, 52, 75 updated.
- **Item 52 designed:** `docs/pin-order-rooms-design.md` (`8e4accb`, `ec4fd66`). Hub-side fan-out of
  `POST /v1/tasks/pin-order` to every attached room, a 3s bound per room, 200 with `unreached`, 502 only when none took
  it. Plus @ui's review finding: `SetPinOrder ... AND pinned = 1`. atrium-87300 chose ours over @ui's board-only fix.
- **Item 16 designed:** `docs/review-memory-design.md`, from fb05's notes `docs/research/item16-notes.md` (merged,
  `e387946`). Read once via a digest (skill AND personas change), panel sized to the change, then per-repo reviewer
  files in dotagents with a resident card as a cache of them. Mercurius `s_xT8IKRB82yUj` round 1 was needs_changes,
  all 3 concerns plus an advisory fixed at b502849 and recorded in the notes.

## Done in the context after that (head 5c3c5f6)

- **Item 16:** Mercurius `s_xT8IKRB82yUj` closed after round 2. C2 (the `repo_notes` parse rule) and A1 fixed, C1
  deferred (stage 2 waits on clint's questions 1, 2 and 6). Review section in the design, backlog-2 status and index row.
- **Item 52:** @runtime passed 8b7d11c. fb04 merged, exited, worktree and branch removed. fb05's too. `prune` noted in
  backlog-2 as the same one-room shape, unfixed.
- **claude/main (bb65176) merged in** at 5c3c5f6, and @merge told it is ready. Item 77b has landed
  (`docs/changes/README.md`): our three change files are rewritten to its `@LETTER@` format and pass
  `fold-changes.ps1 -DryRun`. Fixed test-plan letters (FA, FB...) are gone, so do not hand out letters any more.
- `go test ./...` passes except `internal/daemon` `TestRealSessionsKeepTheirText`, which replays live scrollback and
  fails the same on claude/main alone. @terminal told.
- A dead session's cwd gets `.claude/agent-log.txt` written on exit, which is what kept the fb01 and fb04 folders.

## In flight

Nothing on this card. fb03 below is the only open worker.

## fb03: ACCEPTED, waiting on clint's fetch

- Card sg3~01a0eb52 is `done`. Keep it and the sg3 worktree
  `C:/Users/claude/git/github/dovholuknf/atrium-worktrees/fb03-toolchain` until the branch is here.
- Head **9bce8ad** on claude/fb03-toolchain in the sg3 clone, proven on claudevm.
- Cosmetic nit, not sent: the `path` line prints `~/.atrium/toolchain/path.txt` even under `-StateDir`.
- **When clint runs `pwsh -File scripts/room-git.ps1 fetch sg3`:** review `sg3/claude/fb03-toolchain`, merge it into
  claude/fabric, add the provision Windows start hook from its `docs/changes/fabric-1-toolchain.md` (dot-source
  room-env.ps1 before `& $Bin room --detach`, the `$ds` line in section 8), exit fb03, remove the sg3 worktree.

## Waiting on clint (in the morning report via atrium-87300)

1. `room-git.ps1 fetch sg3`, to bring fb03 back.
2. The m1mini room restart, for the .zprofile PATH.
3. Any `push-base`.
4. Open questions: item 59 (4, `docs/multi-room-design.md`), item 49 (3), item 46 (4) and item 75 (4) in backlog-2,
   item 16 (6, `docs/review-memory-design.md`).

## Then

When fb03 is in, merge claude/main into claude/fabric again, parse-check
(`pwsh -NoProfile -File scripts/check-powershell.ps1`), run `go test ./...`, and tell @merge it is ready.
