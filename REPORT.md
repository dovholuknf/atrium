# r-provision-sgg report

Adding a Windows room is now one `provision-room.ps1` command from a bare machine. The smoke passes on sgg and a rerun
reports every step ok or already right, apart from warns that need a human (listed at the end).

## What was wrong and what changed

1. **No git on a bare Windows box.** `room-git.ps1 init` now installs MinGit when the Windows remote has no git. It
   fetches the latest git-for-windows release (or `-GitVersion 2.56.0`, also a provision parameter), reads the SHA256 from
   the release notes (else the asset digest), downloads the zip on the hub side, checks it, scp's it, checks it again on the
   remote, unzips to `~\.local\git` and prepends `~\.local\git\cmd` to the user Path. A mismatch or a release with no hash
   is a `git fail` and exit 3. macOS and Linux still print the install command. `-Check` installs nothing. New file
   `scripts/room-mingit.ps1`. room-git also got an `-Scp` parameter that resolves the scp beside ssh, since Git's scp
   dials `/usr/bin/ssh`, which does not exist. Proven live on sgg with a probe install to `~\.local\git-probe` (download,
   hash, scp, unzip, Path), then removed. The real install branch did not run on sgg because git is already there.
2. **Start with nobody logged in.** The Windows start script moved to `scripts/room-start.ps1`. When the task cannot bring
   the room up (267011 or any other result), it runs `room --detach` and prints `start warn <reason>`, plus `started=1` so the
   attach wait wants a fresh connection. Only a failed detach is `start fail`. Decision: the fallback is for any task
   failure, and only the wording differs for 267011 and 0x41303. Live on sgg: `start warn the logon task cannot run now,
   it is Interactive and nobody is logged in ... started with room --detach instead`, and the run continued.
3. **The smoke failure.** Cause: the card's own screen (hub `/v1/tasks/<id>/scrollback/text`) said claude had no
   atrium_say or atrium_report tools. Claude's state on sgg was fine (onboarding done, trust accepted for the clone). The
   `mcp` step, which writes `~\.atrium\mcp.json` and names it in the claude runner row, runs after attach, and provision had
   stopped at `start`, so it never ran. Fixed by item 2, nothing more was needed. The smoke now also reads the card's
   screen when it does not report, prints the last lines as `card: ...`, and names this fix (or the first-run-screen fix
   with `ssh -t <target> claude`). `-SmokeOnly` was rerun first and failed as before, then the full run passed it in 6s,
   three times.
4. **"origin is not guarded".** room-git's clone has no origin and no mark, so the room's git sync treated it as the
   operator's. room-git now sets `atrium.clone=made` on a clone it makes, or on an existing one with no origin (so sgg's was
   marked on the rerun). `internal/gitsync/clonerem.go` reads that mark and says nothing for a marked clone with no origin
   (guards origin if one exists). Needs the Go change deployed on the room to take effect, which I did not do beyond the
   dev build provision put on sgg.
5. **room-gate** failed on sgg with Git's scp (`/usr/bin/ssh`), an unrelated bug the full run exposed. Provision now passes
   `-Scp $Scp` to it.

## Files

`scripts/provision-room.ps1`, `scripts/room-git.ps1`, new `scripts/room-start.ps1`, `scripts/room-mingit.ps1`,
`scripts/test-room-start.ps1`, `internal/gitsync/clonerem.go` and its test, `docs/release/packaging.md`, changelog
`changelog/fabric/2026-10-04-r-provision-sgg.md`. No bash script provisions rooms, so none changed.

## Tests

`pwsh -NoProfile -File scripts/test-room-start.ps1`: 58 checks pass. It runs the start script locally with a fake schtasks,
health endpoint and atrium (up already, task works, 267011 plus detach, 0x41303, other result, both fail), the MinGit
release choice against fake GitHub answers (tag, arch, hash from notes, digest fallback, refusals), and parses the install
script. `test-room-folders.ps1` still passes (70). Go: new `TestEnsureRemotesLeavesARoomGitCloneAloneAndSilent` passes.
`go test ./internal/gitsync` has one failure not mine: `TestPushToHubRefusesWhenAGlobalPushInstead...` (Windows path
mangling in the test's own url rewrite).

## Final rerun on sgg (`provision-room.ps1 localai@sgg -Name sgg -User localai`)

```
provision ssh ok localai@sgg
provision os ok windows amd64 SGG
provision account ok localai, the account this room runs as
provision account-rights ok SGG\localai: not elevated, not in an admin group, not the operator's account
provision hub ok direct, rooms dial 192.168.1.68:7779, 5 attached now
provision state ok provisioned before as sgg
provision fetch warn dovholuknf/atrium has no release on GitHub, so this builds atrium from the checkout the script is in
provision build ok windows/amd64 55ceba60f3ca
provision binary ok same build already there
provision join ok already joined as sgg over direct
provision autostart ok logon task atrium, RunLevel Limited
provision start ok
provision attached ok sgg on the hub since 18:35:29, host sgg, build dev
provision runner:claude ok 2.1.289 (Claude Code) at C:\Users\localai\.local\bin\claude.exe
provision mcp ok atrium-control is in C:/Users/localai/.atrium/mcp.json and the claude runner row names it
provision statusline warn no bash found on sgg to run the status line. install git-bash or cygwin, then rerun
room-git git ok git version 2.56.0.windows.1 on localai@sgg
room-git done ok
room-gate register ok the gate is already in PreToolUse (first)
room-gate done ok
provision folders skip a rerun keeps the list as it is ...
provision auth ok signed in with claude.ai, clint.dovholuk@netfoundry.io, NetFoundry
provision smoke:claude ok a claude worker on sgg reported 221577b5 in 6s, and it exited, and said it to ...
provision smoke-outside skip this atrium has no room folders verb yet, so sgg has no list to launch outside of
provision requirements warn room-check exited 4: ...
provision done ok
```

The first full run (the one that did the work) showed `start warn` (267011 fallback), `mcp done`, and `room-gate copy fail`
before the `-Scp` fix.

## Warns left, none a hand step provision could do

- `fetch warn`: no GitHub release, so it builds from the checkout and put a dev build on sgg (this run replaced sgg's
  atrium once, then `binary ok`).
- `statusline warn`: no bash on sgg. MinGit has none. Needs git-bash or the busybox MinGit variant, a decision for the
  operator, not done.
- `requirements warn`: go and node are not on the room's PATH (`room-toolchain.ps1 -Fix`), by design a warn.
- `defender`: an administrator must paste one line (seen in the first run).
- `folders skip` and `smoke-outside skip`: the dev build has no `room folders` verb.

## The old atrium2 install

`C:\Users\localai\.atrium2` was left alone. Decision: provision should warn about it, not detect-and-refuse, but I did not
build it. It is a hand-made second install that may hold a room on other ports, so a `state warn` naming any
`~\.atrium*` folder besides `.atrium` would be the right small follow-up. The "one room per machine" check only looks at
`~\.atrium`.

## Decisions made without asking

Fallback to detach for any task failure, MinGit not removed by `-Remove`, MinGit hash source order (notes, then digest),
the origin guard skipped for a marked clone with no origin, and `-Scp` passed to room-gate.
