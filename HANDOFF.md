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
- **Do not do heavy source reading on this card.** atrium-87300, 2026-09-29: the item 49 research took this
  context from fresh to 168k in ten minutes. If a design needs more reading, ask atrium-87300 for a worker slot and
  have the worker read and summarise.
- **No stop, uninstall, -Remove, service or autostart test against ANY real room** (sg3, m1mini, claude-sg4,
  sg4-wsl). Those run on claudevm only (`ssh claudevm`, Windows, not signed in to claude).
- **No remote room restart while any card on it has a live session**, clint's included (atrium_peers rooms=true).
  Tell atrium-87300 first, every time. Never touch this machine's room or hub. No deploys, no push to origin.
- **@merge (card 01a0eb20) is the only writer of claude/main.** Merge workers into claude/fabric, merge claude/main
  in, then tell @merge the branch is ready.
- **Nobody edits CHANGELOG.md or docs/test-plan.md** until item 77b lands. Each item writes
  `docs/changes/<item>.md`. Test-plan letters: FA (provision), FB (room-git), FC (toolchain), FD (sibling rooms,
  item 59), FE (everywhere card, item 49).
- Past about 150k context, write HANDOFF.md and ask atrium-87300 for a new context.
- The Bash hook refuses `;` chains, `>`, `>>`, `2>&1`, `find` and `git -C`/`--git-dir`. Use Grep for line-length
  checks (`^.{121,}$`), not awk with `>`. Do ssh work through PowerShell. PowerShell refuses
  `git branch --contains <non-claude ref>`, so use `git merge-base --is-ancestor`.
- **No git command may reach a remote** (hook). `git push`, `fetch` and therefore `room-git.ps1 push-base` and
  `fetch` are clint's to run.
- For multi-line remote scripts on Windows, use `powershell -EncodedCommand`. The ssh default shell on sg3 and
  claudevm is Windows PowerShell 5.1, so `&&` does not work there: use `;`.
- The local `scp` on PATH is a broken Cygwin build. Use `C:\Windows\System32\OpenSSH\scp.exe`.
- `powershell.exe` is not on this machine's PATH. Test PS 5.1 behaviour over `ssh claudevm`.

## Branch state

- claude/fabric head: the handoff commit, on top of the item 49 design commit and **283bbb1** (item 59 review).
  Not merged with claude/main since the 6601547 rebase.

## Done this context

- **Item 59: review done.** Mercurius `s_AUQNaSEQ3M5Y` round 1 was ready_to_build. The one advisory is folded in: while
  a room drains, "busy" means mid-turn on the activity badge. Review section added, notes recorded, backlog-2
  status line updated (283bbb1). Reported to atrium-87300. Building it waits on clint's four open questions in
  `docs/multi-room-design.md`.
- **Item 49: design WRITTEN, not reviewed.** `docs/everywhere-card-design.md`, committed with this handoff. Do not
  re-read the sources: the doc holds the findings. What it decides:
  - Mark: tag `atrium:everywhere`. No migration. The hub already gets every card's tags and alias through
    `announce` (the `/v1/state` payload into `hubstore.room_card`).
  - Hub keeps an in-memory index of tagged, not done/dead cards per room. Built from room_card at start, replaced
    for a room on each announcement.
  - Addressing: local wins. Then the index minus the caller's room, by handle or alias. One match goes out as
    `handle@room`, two or more are a 409 naming both, none is today's 404 plus the everywhere list. Say, tell and
    atrium_task fall through. Peers lists them. exit, cull and alias do NOT.
  - Room-side: `localTarget` miss, then a new relay op `find`, then the existing `sayAcross` with the explicit room,
    so holding and the outbox behave as item 58 has them. Owning room offline means held. Hub down or old means a
    404 with a note, nothing held.
  - Seeing: `GET /_hub/everywhere?room=<scope>` (the scoped `/v1/tasks` pipe stays untouched), plus
    `/v1/events/room/<name>?everywhere=1` adding tagged task, task-removed, activity and keepalive events for
    indexed cards, and an `everywhere` event when the index changes. Permissions are NOT forwarded, because
    `/v1/permissions/<id>/decide` does not route by card. Foreign cards are shown, not counted. The board's part
    (room chip, fetch, stream param) goes to @ui.
  - Test plan FE1 to FE9 are listed in the doc. Three open questions for clint.
- Also fixed one prose line over 120 characters in `docs/multi-room-design.md`.

## Next

1. **Item 49 review.** Open a Mercurius session on `docs/everywhere-card-design.md` with artifacts
   cross-room-say-design.md, hub-room-requirements.md, events.go, announce.go, cardroute.go, relay.go (link),
   control_relay.go, and internal/daemon/relay.go. Use plain file names, since Mercurius refuses parentheses. Fold
   in findings, add a Review section, add a backlog-2 status line under item 49 (line 1159, currently "deep
   backlog, not started"), commit, atrium_report. If a finding needs source reading, ask atrium-87300 for a
   worker slot rather than reading here.

## fb03: ACCEPTED, waiting on clint's fetch

- Card sg3~01a0eb52 shows status `done` (its session ended after its report). Keep the card and the sg3 worktree
  `C:/Users/claude/git/github/dovholuknf/atrium-worktrees/fb03-toolchain` until the branch is here.
- Head **9bce8ad** on claude/fb03-toolchain in the sg3 clone, proven on claudevm over ssh (-Check, full install, rerun,
  room-env hand check, -TestBadHash exits 4 through the `rc=N` line).
- Cosmetic nit, not sent: the `path` line prints `~/.atrium/toolchain/path.txt` even under `-StateDir`.
- **When clint runs `pwsh -File scripts/room-git.ps1 fetch sg3`:** review `sg3/claude/fb03-toolchain`, merge it into
  claude/fabric, add the provision Windows start hook from its `docs/changes/fabric-1-toolchain.md` (dot-source
  room-env.ps1 before `& $Bin room --detach`, the `$ds` line in section 8), exit fb03, remove the sg3 worktree.
  The autostart hook belongs on top of fb01's XML registration.
- An empty folder `D:/worktrees/claude/atrium/fb01-provision` is left because something holds it open. Delete it
  when it lets go.

## Waiting on clint (in the morning report via atrium-87300)

1. `room-git.ps1 fetch sg3`, to bring fb03 back.
2. The m1mini room restart, for the .zprofile PATH.
3. Any `push-base`. room-git is operator-only under the hooks.
4. Whether provision should write `permissions.allow` for `mcp__atrium-control__*`. Likely moot now: the smoke card
   on Sonnet with `--allowedTools=` passes.
5. Item 59's four open questions and item 49's three, both in their docs.

## Then

When fb03 is in, merge claude/main into claude/fabric, parse-check
(`pwsh -NoProfile -File scripts/check-powershell.ps1`), and tell @merge it is ready.
