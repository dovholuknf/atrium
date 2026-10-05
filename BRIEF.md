# r-provision-sgg: every hand step from adding sgg goes into provision

You are a lean worker on branch `claude/r-provision-sgg` (claude/main 5fc5b390) on sg4, the hub machine. Work alone.
clint's standing goal: adding a room is ONE command. A step done by hand on one room is not done until provision
does it for every room.

## Rules

- NO messages. Never call atrium_say. Nobody will answer questions: decide, and write the decision in your report.
- When done: write your full report to `REPORT.md` in this worktree and commit it. Then call `atrium_report` ONCE with
  ONE line only: "done <sha>, REPORT.md" or "blocked: <reason>". Then stop.
- Commit on your branch. Short one-line commit messages, NO trailers. Never push, never merge, never touch claude/main.
- NEVER restart, stop or redeploy the hub or any room other than sgg. Never touch ports 7777, 7778, 7779 on this
  machine. sgg's own room you may stop and start (`atrium stop` there, never a kill).
- Prose rules: wrap at 120, no em-dashes, no double-hyphen dashes, no semicolons in prose.
- Changelog: new file `changelog/fabric/2026-10-04-r-provision-sgg.md`. Do not edit docs/backlog.md.
- PowerShell 7 here (`pwsh`). sgg is reached as `ssh sgg` (localai, Windows, Windows PowerShell 5 over ssh: use `;`
  not `&&`). sgg's cold ssh connect takes ~10s, so use ConnectTimeout >= 25.

## What happened today (2026-10-04), by hand, on sgg (bare Windows amd64, no winget, no admin over ssh)

1. `git` was missing. `room-git.ps1` only prints `winget install --id Git.Git -e`, and sgg has no winget. The
   orchestrator installed MinGit by hand: latest `MinGit-<ver>-64-bit.zip` from the git-for-windows GitHub release,
   SHA256 checked against the hash in the release body, unzipped to `%USERPROFILE%\.local\git`, and
   `%USERPROFILE%\.local\git\cmd` prepended to the USER Path. A fresh ssh session then had git 2.56.0.
2. `provision-room.ps1 localai@sgg -Name sgg` got as far as `autostart done logon task atrium, RunLevel Limited`,
   then `start fail the task did not bring the room up, last result 267011. an Interactive task needs the user logged
   in at the machine`, exit 3. The orchestrator ran `~\.atrium\bin\atrium.exe room --detach` over ssh, which started
   the room, survived the ssh closing, and attached (hub health rooms 4 to 5).
3. Because provision stopped at `start`, its `git` step (`room-git.ps1 init`) never ran. The orchestrator ran
   `room-git.ps1 init sgg -Target localai@sgg` by hand: ok, clone at `C:/Users/localai/git/github/dovholuknf/atrium`.
   The hub's git sync then said "origin is not guarded against pushes on this clone, which the operator made", though
   room-git made it. Look at why it is not marked as atrium-made.
4. `provision-room.ps1 localai@sgg -Name sgg -SmokeOnly`: auth ok, then `smoke:claude fail the smoke card
   01a108f8-6608-761f-8cec-9acf875510ab did not report ... in 180s`, exit 8. sgg's room log shows the runner ran about
   four minutes and exited cleanly. Not diagnosed. Suspects: claude's first-run screens (onboarding, theme, folder
   trust) on a machine where claude never ran interactively, or a permission prompt nobody answered. Read the card's
   scrollback through the hub API, and claude's state files on sgg, to find out.
5. sgg also carries an old hand-made atrium2 install under `C:\Users\localai\.atrium2` from 2026-09-21. Leave it
   alone, but say in the report whether provision should detect or warn about it.

## The job

1. `provision-room.ps1` (and `room-git.ps1`): on a Windows remote with no git, install MinGit into the user's home as
   above, verified by SHA256, with no admin. Pin nothing silently: fetch the latest release and its published hash,
   or take `-GitVersion`. macOS and Linux keep printing the install command (they need sudo or xcode-select).
2. When the logon task cannot start the room because nobody is logged in interactively (267011 or the equivalent),
   fall back to `room --detach`, report `start warn` with the reason (the task will start it at the next logon), and
   CONTINUE to the git step and auth and smoke. A start failure must not skip the git step.
3. Fix the smoke failure for a fresh machine, whatever the cause turns out to be, in provision. If it is claude's
   first run, do what claude needs without a human and without touching credentials, or report a `warn` with the one
   command the operator runs. Prove it: rerun `-SmokeOnly` against sgg until it passes, or say exactly why not.
4. Rerun the full `provision-room.ps1 localai@sgg -Name sgg` at the end. A rerun must report every step ok or
   already right, with no hand step left. Paste the step lines in your report.
5. Docs: `docs/packaging.md` and whatever doc describes adding a room (grep for provision-room), plus the script's
   own header: git on a bare Windows box, the interactive-logon fallback, the smoke fix. If a bash script provisions
   rooms too (grep scripts/ for one), give it the same changes.
6. Tests: whatever test harness provision already has (grep for its tests). Add cases for the new branches.
