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
- **Nobody edits CHANGELOG.md or docs/test-plan.md** until item 77b lands. Each item writes
  `docs/changes/<item>.md`. Test-plan letters: FA (provision), FB (room-git), FC (toolchain), FD (item 59), FE
  (item 49), FF (item 52). The next free one is FG.
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

- claude/fabric head: this handoff commit, on top of b502849 (item 16 round 1 fixes). Not merged with claude/main
  since the 6601547 rebase.
- **When claude/main is merged in:** backlog-2 item 52 conflicts, because @ui's item 52 design went to claude/main
  at bb65176 (claude/ui baee190). Take OUR section wholesale, or replace @ui's text with a pointer to
  `docs/pin-order-rooms-design.md`. @ui agreed and will not touch 52 again.

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

## In flight

1. **Item 16, Mercurius round 2** of `s_xT8IKRB82yUj` was started 05:17Z on b502849. Collect it
   (`mercurius_collect_round`, round 2), fold in, record notes, close the session. Then add a Review section to
   `docs/review-memory-design.md` and a status line under `## 16.` in backlog-2 (line 474), update the index table row
   for 16, commit, atrium_report. The design's artifacts, if a round 3 is needed: the design, the notes, and in
   `D:/git/github/dovholuknf/dotfiles/claude/` `skills/review-panel/SKILL.md`, `agents/codebase-steward.md`,
   `agents/go-security-reviewer.md`, plus `D:/git/github/dovholuknf/atrium/docs/far-backlog.md`.
2. **fb04 (item 52), card 01a0eb8d, DONE at 930fe2e** on `claude/fb04-pin-order`, worktree
   `D:/worktrees/claude/atrium/fb04-pin-order`. Diff reviewed and good. The targeted tests passed when re-run by
   @fabric in that worktree. Commits: 63ed4e8 hub fan-out (FF4, FF5), **8b7d11c store predicate (the @runtime
   commit)**, 920c3c5 `docs/changes/fabric-52-pin-order.md`, 930fe2e an api test that now pins first. NEXT: ask
   @runtime (handle from atrium_peers, or ask atrium-87300) to read 8b7d11c. Once they are OK, merge
   claude/fb04-pin-order into claude/fabric, exit fb04, remove its worktree and branch, and atrium_report. fb04 noted
   `/v1/tasks/prune` has the same one-room shape: unfixed, worth a backlog line.
3. **fb05 (item 16 reading), card 01a0eb90:** merged and asked to exit. Remove
   `D:/worktrees/claude/atrium/fb05-review-memory` and branch `claude/fb05-review-memory` once its session is gone.

## fb03: ACCEPTED, waiting on clint's fetch

- Card sg3~01a0eb52 is `done`. Keep it and the sg3 worktree
  `C:/Users/claude/git/github/dovholuknf/atrium-worktrees/fb03-toolchain` until the branch is here.
- Head **9bce8ad** on claude/fb03-toolchain in the sg3 clone, proven on claudevm.
- Cosmetic nit, not sent: the `path` line prints `~/.atrium/toolchain/path.txt` even under `-StateDir`.
- **When clint runs `pwsh -File scripts/room-git.ps1 fetch sg3`:** review `sg3/claude/fb03-toolchain`, merge it into
  claude/fabric, add the provision Windows start hook from its `docs/changes/fabric-1-toolchain.md` (dot-source
  room-env.ps1 before `& $Bin room --detach`, the `$ds` line in section 8), exit fb03, remove the sg3 worktree.
- An empty folder `D:/worktrees/claude/atrium/fb01-provision` may still be there. Delete it when it lets go.

## Waiting on clint (in the morning report via atrium-87300)

1. `room-git.ps1 fetch sg3`, to bring fb03 back.
2. The m1mini room restart, for the .zprofile PATH.
3. Any `push-base`.
4. Open questions: item 59 (4, `docs/multi-room-design.md`), item 49 (3), item 46 (4) and item 75 (4) in backlog-2,
   item 16 (6, `docs/review-memory-design.md`).

## Then

When fb03 is in and fb04 is merged, merge claude/main into claude/fabric (mind item 52, above), parse-check
(`pwsh -NoProfile -File scripts/check-powershell.ps1`), run `go test ./...`, and tell @merge it is ready.
