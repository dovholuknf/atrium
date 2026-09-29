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

- claude/fabric head: this handoff commit. 5c3c5f6 merged claude/main bb65176 and @merge was told it is ready. Since
  then, docs only: item 92 and the item 16 stage 1 list (5d22694). Tell @merge again when there is more.

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
  deferred (stage 2 waits on clint). Review section in the design, backlog-2 status and index row.
- **Item 52:** @runtime passed 8b7d11c. fb04 merged, exited, worktree and branch removed. fb05's too. `prune` noted in
  backlog-2 as the same one-room shape, unfixed.
- **claude/main (bb65176) merged in** at 5c3c5f6, and @merge told it is ready. Item 77b has landed
  (`docs/changes/README.md`): our three change files are rewritten to its `@LETTER@` format and pass
  `fold-changes.ps1 -DryRun`. Fixed test-plan letters (FA, FB...) are gone, so do not hand out letters any more.
- `go test ./...` passes except `internal/daemon` `TestRealSessionsKeepTheirText`, which replays live scrollback and
  fails the same on claude/main alone. @terminal told.
- A dead session's cwd gets `.claude/agent-log.txt` written on exit, which is what kept the fb01 and fb04 folders.

- **Item 92** numbered: `/v1/tasks/prune` reaches one room. Fold it into the next worker that touches
  `internal/link`, never a worker of its own.
- **Item 16 stage 1 is HELD, fb06 was NOT launched.** It changes clint's dotfiles skill and personas, and @dotfiles is
  his card. `docs/review-memory-design.md` "Stage 1, file by file" lists the eight edits and his one-line yes. If he
  says yes: fb06 (Sonnet, claude-sg4) writes them UNCOMMITTED in dotfiles, per the standing rule. Ask atrium-87300
  where it works first. The replay (about 17M tokens) is a separate yes. Exit fb06 by card id once done.

## In flight

Nothing on this card. fb03 below is the only open worker. **This context stays idle until clint answers or @merge
needs you.**

## fb03: ACCEPTED, waiting on clint's fetch

- Card sg3~01a0eb52 is `done`. Keep it and the sg3 worktree
  `C:/Users/claude/git/github/dovholuknf/atrium-worktrees/fb03-toolchain` until the branch is here.
- Head **9bce8ad** on claude/fb03-toolchain in the sg3 clone, proven on claudevm.
- Cosmetic nit, not sent: the `path` line prints `~/.atrium/toolchain/path.txt` even under `-StateDir`.
- **When clint runs `pwsh -File scripts/room-git.ps1 fetch sg3`:** review `sg3/claude/fb03-toolchain`, merge it into
  claude/fabric, add the provision Windows start hook from its `docs/changes/fabric-1-toolchain.md` (dot-source
  room-env.ps1 before `& $Bin room --detach`, the `$ds` line in section 8), exit fb03, remove the sg3 worktree.

## Waiting on clint (in the morning report via atrium-87300)

Each is listed once. The recommendation, where there is one, is in the doc named.

**Things only clint can run**

1. `pwsh -File scripts/room-git.ps1 fetch sg3`, to bring fb03 back.
2. The m1mini room restart, for the .zprofile PATH.
3. Any `room-git.ps1 push-base`.

**Yes or no**

4. Item 16 stage 1: "build it" (eight edits in his dotfiles, uncommitted). `docs/review-memory-design.md`.
5. Item 16: "and run the replay" of PR #4480, about 17M tokens. Only after 4.

**Item 16, `docs/review-memory-design.md` "Open questions". 6, 7 and 11 gate stage 2**

6. Layout: `personas/<id>/repos/<host>/<org>/<repo>.md` in dotagents, or beside `mercurius.yaml`?
7. Who edits a reviewer file: the conductor from `repo_notes`, or each persona itself?
8. Is a resident card per repo still wanted once the files exist, or only if stage 2 falls short?
9. Panel size: is "about 150 changed lines, or a backport" the line for steward plus one?
10. Should Mercurius ever read a repo's reviewer file? Recommended no.
11. Does anything in the parked `bc58c32:docs/personas-design.md` still stand?

**Item 59, `docs/multi-room-design.md` "Open questions for clint"**

12. Is a sibling room worth building for the test room alone?
13. Draining in place: worth a flag, or is today's 90 second park enough?
14. May the permission hook for a supervised session wait for its room instead of failing open at once?
15. Who owns C: the supervisor is Terminal's, the adoption and the hook are Fabric's and Runtime's.

**Item 49, `docs/everywhere-card-design.md` "Open questions for clint"**

16. Is `atrium:everywhere` the tag name?
17. Should a foreign card's permission be answerable from a scoped view? Recommended later.
18. Tag the orchestrator by hand, or have `atrium_launch` tag it? Recommended by hand.

**Item 46, backlog-2 `## 46.` "Open questions for clint"**

19. Should `-Autostart` become the default, and on Windows is it a logon task or the detached room?
20. Which Linux machine may the Linux autostart test run on? sg4-wsl is ruled out.
21. Should provision write `permissions.allow` for `mcp__atrium-control__*`? Likely no.
22. When does stage 2 start, and does it wait on item 75's account question?

**Item 75, backlog-2 `## 75.` "Open questions for clint"**

23. What is the shared folder, and what goes in it?
24. Does `localai` replace `claude` on sg3 (moving a live room), or only apply to new machines?
25. May provisioning create the account (needs admin), or does the operator make `localai` first?
26. Should the room run so CIM works for its workers, or does a worker that needs CIM go without?

## Then

When fb03 is in, merge claude/main into claude/fabric again, parse-check
(`pwsh -NoProfile -File scripts/check-powershell.ps1`), run `go test ./...`, and tell @merge it is ready.
