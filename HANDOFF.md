# @fabric handoff

You are @fabric, the Director of Fabric: hub, rooms, cross-room, overlays and provisioning. Read BRIEF.md first
(the role and rules), then PLAN.md (item 1), then this file.

- Your handle is `fabric-director-of-rooms-hub-cross-room`, on room claude-sg4.
- You report to atrium-87300 (alias orchestrator), with atrium_report once per batch. Use atrium_report rather
  than atrium_say for status, because the ledger only records reports.

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
  `docs/changes/<item>.md`. Test-plan letters: FA (provision), FB (room-git), FC (toolchain).
- Past about 150k context, write HANDOFF.md and ask atrium-87300 for a new context.
- The Bash hook refuses `;` chains, `>`, `2>&1`, `find` and `git -C`. Do ssh work through PowerShell.
- **No git command may reach a remote** (hook). `git push`, `fetch` and therefore `room-git.ps1 push-base` and
  `fetch` are clint's to run. `room-git.ps1 worktree` and `init`'s remote git init work, since they are plain ssh.
- For multi-line remote scripts on Windows, use `powershell -EncodedCommand`.
- The local `scp` on PATH is a broken Cygwin build. Use `C:\Windows\System32\OpenSSH\scp.exe`.

## Branch state

- claude/fabric head is **35fa4a1** (merge of fb01), on top of 7e4f85d (the last handoff). Not merged with
  claude/main since the 6601547 rebase.
- **UNCOMMITTED in this worktree: two smoke fixes in `scripts/provision-room.ps1`** (`Invoke-Smoke`, the `$body`
  hash). Commit them only after a passing -SmokeOnly run:
  1. The smoke model is `claude-sonnet-5-5` at low effort, not Haiku. Claude Code's auto mode does not run on
     Haiku, so a Haiku card fell back to asking for atrium_say, and nobody answered. fb03 on Sonnet ran in auto
     mode on sg3.
  2. `--allowedTools=<list>` is one argument with `=`. As two arguments the variadic flag also SWALLOWED THE
     PROMPT: the card came up at an empty input line (seen live on the sg3 smoke card's scrollback).
- A background -SmokeOnly run against sg3 was started BEFORE fix 2, so it will fail with exit 8. Ignore it.
- Next: rerun `pwsh -NoProfile -File scripts/provision-room.ps1 sg3 -SmokeOnly -SmokeCwd C:/Users/claude/smoke-fresh-1`
  and read the card live with `Invoke-WebRequest http://127.0.0.1:7778/v1/tasks/sg3~01a0eb4c-f05a-7a79-8ede-aa7ead4f12c9/scrollback/text`.
  If it passes, commit and add both fixes to `docs/changes/fabric-1-provision.md`.
- Smoke facts found this session:
  - The hub reuses the smoke card by WIRE NAME (`smoke-sg3`, id 01a0eb4c since 03:54), not by folder.
  - `launch_args` DOES reach the card, so args are not dropped on reuse.
  - atrium_report sets `recap` (control_mcp.go reportHandler, then /v1/tasks/{id}/report), which is what smoke
    polls, so the poll is right.
  - Cards on sg3 show no activity on the board (last_activity stuck at launch time). The hooks may not fire
    there, and sg3's `~/.claude/settings.json` has no hooks. Worth a look, not yet diagnosed.

## fb01: DONE, merged (35fa4a1). Clean-up NOT done

- 872dc96 on claude/fb01-provision. Card 01a0eb29 is still up at 150k context and was told to stop.
- To do: atrium_exit it, then `git worktree remove D:/worktrees/claude/atrium/fb01-provision` and
  `git branch -d claude/fb01-provision`.
- Unproven from fb01: systemd PATH (no Linux box), the Sch /End of a live task, -Autostart start on sg3.

## fb03: RUNNING ON sg3, waiting for claudevm runs

- Card sg3~01a0eb52, alias fb03@sg3, Sonnet 5.5 medium.
- Worktree on sg3: `C:/Users/claude/git/github/dovholuknf/atrium-worktrees/fb03-toolchain`, branch
  claude/fb03-toolchain off hub-main 18b9015 (an older claude/main). Head **f6bf357**.
- `_ref/` there holds reference copies and is excluded in the clone's info/exclude. fb03 rewrote its worktree's
  `.git` file from a /cygdrive path to `C:/...`.
- A copy of its script for running from here is in the scratchpad
  `...\scratchpad\fb03\scripts\room-toolchain.ps1` (with go.mod beside it). If that is gone, scp it again.
- **Proven from here:** `-Check` against m1mini and sg3 both give done ok, exit 0, as fb03 predicted. The results
  have been sent to it.
- **Still to run** (claudevm is Windows): `room-toolchain.ps1 claudevm -Check`, then a full run twice (expect all ok
  the second time), then `-TestBadHash` (expect exit 4). After the install, in a shell on claudevm, dot-source
  `~\.atrium\toolchain\room-env.ps1` and run `git --version; pwsh --version; go version`. Send fb03 any failure
  output. fb03 is holding at f6bf357 with nothing outstanding.
- Design: the PATH record is `~/.atrium/toolchain/path.txt`. Windows gets `room-env.ps1`, which the room start must
  dot-source. The one-line hook for provision's Windows start step, and for the autostart action, is in its
  `docs/changes/fabric-1-toolchain.md`. Add those hooks when merging, not before. There is no -Restart.
- **Its branch comes back only when clint runs `pwsh -File scripts/room-git.ps1 fetch sg3`.** Then review
  `sg3/claude/fb03-toolchain`, merge it into claude/fabric, add the provision hook, exit fb03, and remove the sg3
  worktree.

## Rooms

- **sg3:** restarted once at 23:47:55 with PATH = `atrium-tools\git\cmd;atrium-tools\pwsh;Machine;User`.
  `go test ./internal/daemon` passes there in that PATH form. The claudeconf symlink test still fails there
  (ssh session symlink policy). Live cards on sg3: fb03, and the smoke card whenever a smoke runs.
- **m1mini:** restart SKIPPED tonight on the orchestrator's call, because it would end clint's card
  m1mini~01a0eb2b. Leave that card alone. runtime's sa73 also runs there. The restart is on the morning list for
  clint. `.zprofile` already has the PATH line.

## Waiting on clint (in the morning report via atrium-87300)

1. `room-git.ps1 fetch sg3`, to bring fb03 back.
2. The m1mini room restart, for the .zprofile PATH.
3. Any `push-base`. room-git is operator-only under the hooks.
4. Whether provision should write `permissions.allow` for `mcp__atrium-control__*` into a room's settings.json.
   Only needed if Sonnet plus `--allowedTools=` does not fix the smoke card.

## Then

When fb03 and the smoke fixes are in, merge claude/main into claude/fabric, parse-check
(`pwsh -NoProfile -File scripts/check-powershell.ps1`), and tell @merge it is ready.
