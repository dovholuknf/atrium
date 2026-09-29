# @fabric handoff

You are @fabric, the Director of Fabric: hub, rooms, cross-room, overlays and provisioning. Read BRIEF.md first
(the role and rules), then PLAN.md (item 1), then this file.

- Your handle is `fabric-director-of-rooms-hub-cross-room`.
- You report to atrium-87300 (alias orchestrator), with atrium_report once per batch. Use atrium_report rather
  than atrium_say for status, because the ledger only records reports.

## Standing rules added since BRIEF.md

- **Ask atrium-87300 before each worker launch**, in one line: the item, why, and which room. It grants slots, 10
  across all directors. Worktrees are made with `pwsh -File scripts/new-worktree.ps1 -Name <x> -Base claude/fabric`,
  which also links the 8 CLAUDE.md files.
- **One card per worker.** To move a worker up to Opus, exit its session, then atrium_launch on the SAME worktree
  with `model: claude-opus-5-5` and the same "fbNN: ..." title. It reuses the card.
- **No stop, uninstall, -Remove, service or autostart test against ANY real room.** The real rooms are sg3,
  m1mini, claude-sg4 and sg4-wsl. Those tests run on claudevm only (`ssh claudevm`, Windows, not signed in to
  claude).
- **No remote room restart while any card on it has a live session**, clint's included (check atrium_peers with
  rooms=true). Tell atrium-87300 first, every time. Never touch this machine's room or hub. No deploys, no push to
  origin.
- **A throwaway card is thrown away when its room restarts.**
- **@merge (card 01a0eb20) is the only writer of claude/main.** Merge workers into claude/fabric, merge claude/main
  in, resolve conflicts, then tell @merge the branch is ready.
- **Nobody edits CHANGELOG.md or docs/test-plan.md** until item 77b lands. Each item writes
  `docs/changes/<item>.md` with "Changelog" and "Test plan" sections. Test-plan letters are FA (provision), FB
  (room-git) and FC (toolchain).
- **Past about 150k context**, write HANDOFF.md and ask atrium-87300 for a new context.
- When a design needs review, run a Mercurius round before building.
- Unsigned commits made on the remote rooms are fine: clint re-signs everything before the push.
- The Bash hook refuses `;` chains, `>` and `2>&1`. Do ssh work through PowerShell.
- For multi-line remote scripts on Windows, use `powershell -EncodedCommand`. `-Command -` on stdin breaks
  multi-line blocks.

## Item 1: setting up a room takes one command

Goal (clint): one command takes a bare ssh machine to a room that runs claude workers and whose work merges here.
Tonight's goal (orchestrator): m1mini and sg3 can each build atrium and run go test from a push-seeded clone, so
workers can be sent there. Tell atrium-87300 the moment one is fully ready.

### Branch state

- claude/fabric head is **5145bad**. It is rebased onto the re-signed claude/main 6601547 and has not been merged
  with claude/main since.
- Commits:
  - 08294e1 and 4e14465: PLAN.md
  - 903e9e6: backlog-2 item 63 marked DONE (stages 0cbbaa2, 8deea51 and 479c9d7; follow-up noted)
  - 5145bad: merge of fb02
- PLAN.md is committed. This HANDOFF.md is committed with this handoff.

### fb02: DONE, merged (5145bad)

- `scripts/room-git.ps1` has four subcommands: `init <room> -Target <ssh>`, `push-base <room> [-From claude/main]`,
  `fetch <room>` and `worktree <room> <name>`.
- Proven on m1mini and sg3.
- The provision hook is `[string] $Repo = 'atrium'` plus one line before the final `if ($bad -gt 0)`.
- Docs are in `docs/changes/fabric-1-room-git.md`, `docs/packaging.md` and `docs/remote-launch.md` section 6.
- Git remotes added to this repo's shared config:
  - `m1mini` = `ssh://m1mini/Users/claude/git/github/dovholuknf/atrium`
  - `sg3` = `sg3:C:/Users/claude/git/github/dovholuknf/atrium`
- sg3 has fb02's wrapper `C:\Users\claude\.room-git\git.cmd` (it puts Cygwin git on PATH), which
  uploadpack and receivepack use. Do not remove it.
- Remote worktrees go to `<clone>-worktrees/<name>`.
- Still open: a cmd.exe default ssh shell is untested, there has been no Linux run, and the manifest lookup of the
  ssh target is missing.
- The worktree and branch are removed.

### fb01: RUNNING on Opus

- Card 01a0eb29, worktree `D:/worktrees/claude/atrium/fb01-provision`, branch `claude/fb01-provision`.
- The first session's work is in f293ece, 9dbe1bf and 0c79f2e:
  - binary builds from the checkout by default (proven on claudevm)
  - `auth` step using `claude auth status` JSON `loggedIn` (proven)
  - schtasks instead of CIM (proven on claudevm, and the helpers on sg3)
  - systemd PATH (written, NOT proven: there is no Linux box but WSL, and WSL is ours)
  - `smoke` step (written, NEVER run)
  - docs in `docs/changes/fabric-1-provision.md`
  - the Stop-AtriumGracefully guard (stop only when the named task is Running)
- The second session was told to:
  - review the diff
  - rebase onto claude/fabric, resolving fb02's hook
  - run git init before smoke, with the smoke cwd defaulting to the clone
  - add `-SmokeOnly`
  - prove smoke on sg3 without stopping the room: same binary, or `-Binary`
  - delete HANDOFF.md from its branch at the end
- **It is told to HOLD everything against sg3 until I say go**, because of the sg3 restart below.
- When it reports: review, merge into claude/fabric, exit it, and remove the worktree and branch.

### sg3 (Windows, room `sg3`, no admin)

- **INCIDENT at 23:31.** fb01's service-uninstall test stopped the real sg3 room, and clint's throwaway agent
  sg3~01a0eb2c was thrown away when I restarted the room at 23:32 (`~\.atrium\bin\atrium.exe room --detach` over
  ssh, PATH Machine;User). The orchestrator put it in the morning report.
- Installed:
  - go 1.26.2 at `C:\Users\claude\go-sdk\go\bin` (by clint's agent), on the user Path
  - node v24.21.0 at `C:\Users\claude\node-sdk\node-v24.21.0-win-x64` (I added it to the user Path)
  - PortableGit 2.56.0 and pwsh 7.6.6 (by me, sha256 checked) under
    `C:\Users\claude\.local\share\atrium-tools\{git,pwsh}`, NOT on any Path yet
- Clone: `C:\Users\claude\git\github\dovholuknf\atrium` on hub-main, with a repo-local git identity of dovholuknf
  and `46322585+dovholuknf@users.noreply.github.com`.
- `go build ./...` passes.
- First `go test ./...`: 14 failures across 4 packages, all traced to the machine:
  - 7 Cull tests (Cygwin git)
  - 3 CaptureEnv tests (need pwsh 7)
  - claudeconf TestInstallWritesThroughASymlink ("untrusted mount point", Windows symlink policy in ssh sessions)
  - the known-flaky link test
- With `atrium-tools\git\cmd` and `atrium-tools\pwsh` FIRST on PATH, internal/daemon passes completely. Only the
  symlink test still fails.
- **The catch:** Windows puts the machine Path (which has c:\work\tools\cygwin\bin) before the user Path, so user
  entries cannot win. The room has to be STARTED with the tools prepended.
- **APPROVED, NOT YET DONE** (see "Waiting on" 1): restart the sg3 room once, with
  PATH = `atrium-tools\git\cmd;atrium-tools\pwsh;Machine;User`. Before doing it:
  - check atrium_peers rooms=true for live sg3 cards
  - start it through `powershell -EncodedCommand`, setting `$env:Path` and then `& $HOME\.atrium\bin\atrium.exe
    room --detach`
  - confirm on `http://127.0.0.1:7778/_hub/rooms`
  - then tell fb01 "go", and tell the orchestrator sg3 is ready, with the test result
- Durable fix, for a later toolchain worker: on Windows, provision's Invoke-Remote PATH and atrium-autostart put
  user tools first.

### m1mini (mac arm64, room `m1mini`, no sudo)

- clint's agent m1mini~01a0eb2b installed go 1.26.2 at `~/.local/opt/go` and node v24.21.0 at
  `~/.local/opt/node`, plus Playwright chromium.
- It put PATH in `~/.zshrc`. I added the same line to `~/.zprofile`, marked `# added by atrium room-toolchain`,
  since the room starts with `zsh -lc`. `zsh -lc 'go version'` gives 1.26.2.
- go build passes. go test fails 2 tests in internal/daemon deterministically: TestASayToAGoneSessionIsUndeliverable
  and TestAHeldMessageOnAnEndedSessionIsDropped. These are likely product bugs.
- That agent is investigating them for the orchestrator (`~/atrium-m1mini-findings.md`).
- **Hands off m1mini**: do not exit that agent and do not restart the room until it has reported to atrium-87300.
  After that, the room restart (to pick up the .zprofile PATH) needs no live cards and a heads-up to atrium-87300.
- Its clone has a repo-local git identity set.

### fb03: ON HOLD, not launched

- Worktree `D:/worktrees/claude/atrium/fb03-toolchain` (branch claude/fb03-toolchain, still at 4e14465, unused).
- Its brief is drafted in the scratchpad (`...\scratchpad\fb03.md`), which may be gone.
- When the room agents' steps are known, write `scripts/room-toolchain.ps1` from them:
  - go and node tarballs or zips under the home dir
  - PortableGit and pwsh on Windows
  - on Windows, the room is started with the tools prepended
  - on mac and linux, a marked profile line
  - a restart of the remote room under the rules
- It needs the orchestrator's OK before launch. Rebase its branch onto claude/fabric first, or recreate it.

### Queue after item 1

backlog-2 item 63 is DONE (nothing to do). Nothing else is queued for fabric yet.

## How to check

- `git log --oneline claude/main..claude/fabric`
- Parse-check PowerShell with `pwsh -NoProfile -File scripts/check-powershell.ps1`
- Hub rooms: `Invoke-RestMethod http://127.0.0.1:7778/_hub/rooms`
- Remote test in sg3's clone: see the encoded-command pattern above, with ATRIUM_LOCATION and ATRIUM_DEBUG_INPUTLAG
  cleared.

## Waiting on

1. **APPROVED** by atrium-87300, just before the context clear: restart the sg3 room once, detached, with
   atrium-tools prepended, since it has no live cards. It has NOT been done yet: do it first, after checking
   atrium_peers. Report when it is back up and `go test ./internal/daemon` passes there, run in the ROOM's PATH
   form. The durable prepend goes into room-toolchain.ps1.
2. fb01's report.
3. The m1mini agent's report to atrium-87300, then the m1mini room restart.
4. When everything is merged, merge claude/main into claude/fabric and tell @merge it is ready.
