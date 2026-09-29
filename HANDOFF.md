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
- **No stop, uninstall, -Remove, service or autostart test against ANY real room** (sg3, m1mini, claude-sg4,
  sg4-wsl). Those run on claudevm only (`ssh claudevm`, Windows, not signed in to claude).
- **No remote room restart while any card on it has a live session**, clint's included (atrium_peers rooms=true).
  Tell atrium-87300 first, every time. Never touch this machine's room or hub. No deploys, no push to origin.
- **@merge (card 01a0eb20) is the only writer of claude/main.** Merge workers into claude/fabric, merge claude/main
  in, then tell @merge the branch is ready.
- **Nobody edits CHANGELOG.md or docs/test-plan.md** until item 77b lands. Each item writes
  `docs/changes/<item>.md`. Test-plan letters: FA (provision), FB (room-git), FC (toolchain), FD (sibling rooms,
  proposed in item 59).
- Past about 150k context, write HANDOFF.md and ask atrium-87300 for a new context.
- The Bash hook refuses `;` chains, `>`, `2>&1`, `find` and `git -C`/`--git-dir`. Do ssh work through PowerShell.
  PowerShell refuses `git branch --contains <non-claude ref>`, so use `git merge-base --is-ancestor`.
- **No git command may reach a remote** (hook). `git push`, `fetch` and therefore `room-git.ps1 push-base` and
  `fetch` are clint's to run.
- For multi-line remote scripts on Windows, use `powershell -EncodedCommand`. The ssh default shell on sg3 and
  claudevm is Windows PowerShell 5.1, so `&&` does not work there: use `;`.
- The local `scp` on PATH is a broken Cygwin build. Use `C:\Windows\System32\OpenSSH\scp.exe`.
- `powershell.exe` is not on this machine's PATH. Test PS 5.1 behaviour over `ssh claudevm`.

## Branch state

- claude/fabric head: the handoff commit on top of **8c700d8** (item 59 design) and **e6f8b97** (smoke fixes
  recorded). Not merged with claude/main since the 6601547 rebase.

## Done this context

- **Smoke fixes proven.** `provision-room.ps1 sg3 -SmokeOnly` passed, exit 0, the worker reported in 9s. Recorded in
  `docs/changes/fabric-1-provision.md` with FA9 (e6f8b97).
- **fb01 cleaned up.** Card already gone, worktree unregistered, branch deleted. An empty folder
  `D:/worktrees/claude/atrium/fb01-provision` is left because something holds it open. Delete it when it lets go.

## fb03: ACCEPTED, waiting on clint's fetch

- Card sg3~01a0eb52 shows status `done` (its session ended after its report). Keep the card and the sg3 worktree
  `C:/Users/claude/git/github/dovholuknf/atrium-worktrees/fb03-toolchain` until the branch is here.
- Head **9bce8ad** on claude/fb03-toolchain in the sg3 clone. Proven on claudevm over ssh: -Check, full install,
  rerun all ok, room-env hand check with `-ExecutionPolicy Bypass`, -TestBadHash exits 4 (the fix: the payload
  prints `rc=N` and the caller reads it, since the exit code was lost on the ssh route). A fresh one-tool install is
  still exit 0. The Unix `rc=` path is only covered by fb03's older fake-tarball test.
- Cosmetic nit, not sent: the `path` line prints `~/.atrium/toolchain/path.txt` even under `-StateDir`.
- A copy of 9bce8ad's script is in this context's scratchpad `...\91ee3484-...\scratchpad\fb03b\`.
- **When clint runs `pwsh -File scripts/room-git.ps1 fetch sg3`:** review `sg3/claude/fb03-toolchain`, merge it into
  claude/fabric, add the provision Windows start hook from its `docs/changes/fabric-1-toolchain.md` (dot-source
  room-env.ps1 before `& $Bin room --detach`, the `$ds` line in section 8), exit fb03, remove the sg3 worktree.
  The autostart hook belongs on top of fb01's XML registration.

## Item 59: design written, Mercurius round 1 done and NOT collected

- `docs/multi-room-design.md`, committed at 8c700d8. Backlog-2 item 59 has a status line.
- Recommends: A sibling rooms (`-Instance` in provision), "draining in place" (a room flag in its `setting` table,
  announced to the hub, plus a restart ask that waits `until_idle`), NOT drain-to-sibling (resident sessions never
  drain, no card move, the binary swap assumes one instance). The goal goes to a new item C: a holder process per
  runner that owns the pty and outlives the room. It argues the "ConPTY has no reattach" objection does not apply
  because the holder creates the console and never hands it over, and it flags that the permission hook's fail-open
  becomes a real hole once runners outlive the room. Spike plan written, claudevm only, NOT run.
- **Mercurius session `s_AUQNaSEQ3M5Y`, round 1 finished, not collected.** It reviewed the 8c700d8 text. After it
  started, one sentence was fixed and committed in the handoff commit: the doc wrongly said the hub persists only
  certificates. It has `internal/hubstore` (tables room, room_secret, room_card, room_audit, hub_setting). If a
  finding is about that sentence, it is already fixed. Collect with `mercurius_collect_round`, fold the findings
  into the doc, record notes, run round 2 until ready_to_build, add a "Review" section like
  `docs/cross-room-say-design.md` has, commit, then atrium_report to atrium-87300.
- The round's artifacts were: the design, supervision-design.md, reload-design.md, hooks.md, card-room-routing.md,
  roomrestart.go, roomrun.go, whereami.go, spawn_windows.go, hub.go. Names must be plain file names (Mercurius
  refuses parentheses).

## Item 49 (next): the orchestrator can appear on every room, design only

Backlog-2 line 1159. Research so far, nothing written:

- Addressing: item 58 already gives `name@room`, the `relay` link kind, the room-side outbox, and hub-side
  `resolvePeer` (`docs/cross-room-say-design.md`). Item 49's addressing half is a bare name that falls through to
  an "everywhere" card when the caller's own room has no match. Local wins. Two everywhere cards with one alias are
  refused, naming both.
- The mark: a card tag (`task.tags`, migration 0019, `SetTags` in `internal/store/tasks.go`), e.g.
  `atrium:everywhere`, so there is no migration. The hub learns the set from `announce` (`internal/link/announce.go`
  into `hubstore.room_card`), not by fanning out every scoped list.
- Seeing: a scoped view is a byte pipe today (`docs/hub-room-requirements.md`). Showing another room's card there
  means the hub appends it to the scoped `/v1/tasks`, tagged `room~id`, and forwards that card's events into the
  scoped stream (`serveEvents` in `internal/link/events.go`, subscribers filtered by room in `p.feeds`). That
  knowingly breaks the dumb-pipe rule for one endpoint. The board drawing a foreign card with a room chip in a
  scoped view is UI's area: hand that part to @ui.
- Rejected already: a mirror card in each room's store (a second source of truth, which federation-design-v2
  rules out).
- Then: Mercurius review, commit on claude/fabric, atrium_report.

## Waiting on clint (in the morning report via atrium-87300)

1. `room-git.ps1 fetch sg3`, to bring fb03 back.
2. The m1mini room restart, for the .zprofile PATH.
3. Any `push-base`. room-git is operator-only under the hooks.
4. Whether provision should write `permissions.allow` for `mcp__atrium-control__*`. Likely moot now: the smoke card
   on Sonnet with `--allowedTools=` passes.

## Then

When fb03 is in, merge claude/main into claude/fabric, parse-check
(`pwsh -NoProfile -File scripts/check-powershell.ps1`), and tell @merge it is ready.
