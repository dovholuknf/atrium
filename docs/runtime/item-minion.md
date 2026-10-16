# Item minion: one item, from launch to landed

A per-item minion is one lean sonnet session that takes ONE backlog item from launch to landed on `claude/main`, then
exits. This is the recipe the resident minion has run by hand for every item. Each command is exact. Replace the
placeholders: `<id>` the backlog id (`bug-foo`), `<room>` the room that runs the worker (`sg3`, `sgg`, `m1mini`,
`claude-sg4`), `<sha>` the sha of `claude/main` at launch, `<repo>` the room's clone, `<wt>` the worktree.

Rules that hold for every step:

- Never edit the main checkout `D:\git\github\dovholuknf\atrium` except `git merge --ff-only`.
- Never commit on `claude/main`. Nothing lands before the launcher says "accepted".
- Commits are by Clint: `Clint Dovholuk <clint.dovholuk@netfoundry.io>`, one short subject line, no body, no trailers.
- Go builds go to `build.claude/` (`go build -o build.claude/...`). No restart of the live hub or a room, no deploy.

## Room paths

| room | clone `<repo>` | worktrees |
| --- | --- | --- |
| claude-sg4 (this machine) | `D:\git\github\dovholuknf\atrium` | `D:\worktrees\github\dovholuknf\atrium\<name>` |
| sg3 | `C:/Users/claude/git/github/dovholuknf/atrium` | `C:/Users/claude/git/github/dovholuknf/atrium-worktrees/<name>` |
| sgg | `C:/Users/localai/git/github/dovholuknf/atrium` | `C:/Users/localai/git/github/dovholuknf/atrium-worktrees/<name>` |
| m1mini | `/Users/claude/git/github/dovholuknf/atrium` | `/Users/claude/git/github/dovholuknf/atrium-worktrees/<name>` |

Staging worktree for landing (on claude-sg4): `D:\worktrees\minion-ssh-default`. Its parking branch is
`claude/land-ci-green`: switch back to it after every landing.

## 0. Check the item first

1. Read it: `atrium_backlog` `{action: "get", id: "<id>"}`.
2. Check it against main so no worker builds what is already there. On claude-sg4:
   `git log --oneline --grep="<keyword>" -i -8 claude/main` and `git grep -n "<symbol>" claude/main -- internal`.
   An item marked open can already be built (r-hub-recognisers and f-room-ssh-wizard phase 1 both were). If it is,
   mark it done and stop.
3. Read `claude/main`'s sha: `git log --oneline -1 claude/main` in `D:\git\github\dovholuknf\atrium`. This is `<sha>`.

## 1. Make the worktree on the room

1. Bring the room's clone up to date: `atrium_git_sync` `{room: "<room>"}`. A `behind` answer with the right `sha`
   is fine (a worktree holds the branch). A failed `git worktree add` after this silently continues in the WRONG
   directory, so always read the HEAD line in step 3.
2. Write the script, in the shell that will run it (the Bash and PowerShell `/tmp` differ), then pipe it to the room.
   For a Windows room, `/tmp/w.ps1`:

   ```powershell
   Set-Location <repo>
   git worktree add -b claude/<id> ../atrium-worktrees/<id> <sha>
   Set-Location ../atrium-worktrees/<id>
   git config extensions.worktreeConfig true
   git config --worktree remote.hub.url http://127.0.0.1:7777/git/hub/github/dovholuknf/atrium.git
   git config --worktree user.name "Clint Dovholuk"
   git config --worktree user.email clint.dovholuk@netfoundry.io
   git log --oneline -1
   Get-Location
   ```

   On claude-sg4 the same lines run locally against `D:\git\github\dovholuknf\atrium` with the worktree at
   `D:\worktrees\github\dovholuknf\atrium\<id>`. On m1mini use a bash script and `ssh m1mini 'bash -s' < file.sh`.
3. Pipe it: sg3 `Get-Content /tmp/w.ps1 | ssh sg3 powershell -NoProfile -Command -`; sgg
   `Get-Content /tmp/w.ps1 | ssh localai@sgg powershell -NoProfile -Command -` (if `ssh sgg` answers "room ... is
   offline", use the LAN name `sgg.parkplace-via-dhcp`).
4. Confirm: the output ends with the worktree path, and the `git log --oneline -1` line shows `<sha>`. If not, stop:
   the clone was not synced.

## 2. Launch the worker

`atrium_launch_worker` with:

| arg | value |
| --- | --- |
| `room` | `<room>` (leave out for claude-sg4) |
| `cwd` | the worktree path from step 1, `C:/...` form for a Windows room |
| `model` | `sonnet` |
| `title` | the item id |
| `why` | one line: the backlog id and what it fixes |
| `tags` | `["atrium:item:<id>"]` |
| `prompt` | `Read BRIEF.md and do the task.` |
| `brief` | the item body plus the rules below |

Do NOT pass `item: true` (the tool rejects it for a worker that must `atrium_say`/`atrium_done` to its launcher).
`lean` defaults to true: the worker gets only its brief, the repo, atrium's hooks and the control MCP.
Use `atrium_launch_worker`, not `atrium_launch`: atrium exits the runner once its branch merges, and the card and
worktree stay for the cleanup in step 6.

The brief is the item body, then this block, with the item's specifics added (board PNG paths, the phase to build):

```
RULES: commit as Clint Dovholuk (configured), one short subject line per commit, no body, no trailers. Do not edit
CLAUDE.md. Go builds go to build.claude/ (go build -o build.claude/...). gofmt -l internal/ cmd/ must be empty; go run
scripts/check-test-globals.go exit 0 (tests may not assign package vars). Unit tests stay under the 10ms budget (disk,
socket or store tests go in *_integration_test.go with //go:build integration). Add a changelog entry and
docs/changes/<id>.md. Board work: before/after PNGs under docs/changes/img/<id>/ with a README line, headless units
plus bootClean, no one-off shooter scripts committed. Run scripts/test-touched.sh in ONE foreground call, timeout up
to 600000ms, never background/sleep/poll (a 20ms unit-budget miss on a disk test you did not touch under load is not
yours: rerun once and say so). Known failing, not yours: TestReposIsOpenLikeGrowlsAndExactlyShaped,
TestRealSessionsKeepTheirText, api taskkill tests, TestPublishTaskCoalescesABurstToTheLatestState. Never restart the
live hub or rooms, never deploy, do not merge or push. Finish with atrium_done giving the commit sha.
```

Time limits: a launch after 21:30 EDT is not made; at 22:00 EDT every worker commits WIP and reports.

## 3. Collect the branch

When the worker's `atrium_done` arrives (it carries the sha):

1. `atrium_git_collect` `{room: "<room>"}`. The branch is now the ref `refs/remotes/<room>/claude/<id>` in
   `D:\git\github\dovholuknf\atrium`. It is only a ref: a collected branch is NOT accepted.
2. Read what it holds, in PowerShell:

   ```powershell
   Set-Location D:\worktrees\minion-ssh-default
   git log --oneline claude/main..refs/remotes/<room>/claude/<id>
   git diff --stat claude/main...refs/remotes/<room>/claude/<id>
   ```

   Check it against the item, not the worker's say-so: the worker has said "done" on work that was already on main.
   For board work open the after PNGs and read them.

## 4. Rebase onto main and verify (the staging worktree)

All in `D:\worktrees\minion-ssh-default`:

1. `git switch -C claude/land-<id> claude/main`
2. `git cherry-pick claude/main..refs/remotes/<room>/claude/<id>` (cherry-pick, so the land branch is main plus the
   worker's commits; if main has moved since the worker's base, this is the rebase).
3. Strip BOMs from every new or changed `.md` (the first byte must not be 239): for each, `tail -c +4 <file>` to a temp
   file and copy it back, then `git commit -a --amend --no-edit`. A BOM-prefixed changelog entry breaks the build's
   changelog parse.
4. Format and globals, from bash with `export PATH=/v/work/tools/go/current/bin:$PATH`:
   `gofmt -l internal/ cmd/` must print nothing; `go run scripts/check-test-globals.go` must exit 0.
5. Tests, in ONE foreground call with a 600000 ms timeout:
   `export PATH=/v/work/tools/go/current/bin:$PATH NODE_PATH=/d/git/github/dovholuknf/atrium/node_modules; timeout 590 bash scripts/test-touched.sh | tail`.
   It prints `everything it ran passed` or names the failing step. Known failures are listed in the brief block above.
   A miss of the 20ms unit-budget gate on a disk test the branch did not touch is load, not the branch: rerun once and
   report it (item `t-unit-budget-load-headroom`).
6. Report to the launcher with `atrium_say` kind `needs`: the land branch and sha, what was checked, the test result, any
   change you made. Then WAIT. Land nothing before "accepted". If the launcher asks for a cut, amend it out of the commit
   and rerun the gofmt and globals checks.

## 5. Land on claude/main

After "accepted", in PowerShell:

1. Rebase if main moved: in `D:\worktrees\minion-ssh-default`, `git switch claude/land-<id>`, `git rebase claude/main`.
2. Fast-forward main from the main checkout. This is the only thing allowed there:

   ```powershell
   Set-Location D:\git\github\dovholuknf\atrium
   git merge --ff-only claude/land-<id>
   git log --oneline -1 claude/main
   ```

3. Park the staging worktree and drop the land branch:

   ```powershell
   Set-Location D:\worktrees\minion-ssh-default
   git switch claude/land-ci-green
   git branch -D claude/land-<id>
   ```

   Never `git reset --hard` on `claude/land-ci-green`.
4. A landing that is not a plain cherry-pick of Clint's commits (a doc written by the minion itself) is committed with
   `git commit --author "Clint Dovholuk <clint.dovholuk@netfoundry.io>"` on `claude/land-<id>` before step 2.

## 6. Close and clean up

1. Backlog: `atrium_backlog` `{action: "status", id: "<id>", status: "done"}`. If the item says its done bar is a live
   check (for example `/_hub/work` after a deploy), leave it open until the check passes.
   An item built in phases stays open and the report says which phases remain.
2. Close the worker's card: `atrium_close` `{card: "<title>@<room>", confirm: true}` (call it once without `confirm` to
   read what it would remove; answer every `asks` entry). It stops the session and archives the review.
3. Remove the worktree and branch on the room, with a script piped as in step 1:

   ```powershell
   Set-Location <repo>
   git worktree remove --force ../atrium-worktrees/<id>
   git branch -D claude/<id>
   ```

   On claude-sg4: `git worktree remove --force D:/worktrees/github/dovholuknf/atrium/<id>` and
   `git branch -D claude/<id>` in `D:\git\github\dovholuknf\atrium`.
4. Add one dated line to `D:\git\github\dovholuknf\atrium\.uncommitted\notes\MINION-LOG.md`
   (`Add-Content`): `2026-10-15 22:41 landed <id> <sha>`.
5. Tell the launcher in one line: `atrium_say` kind `fyi`, `done: <id> landed <sha>, backlog done, worker closed`.
6. The minion then exits itself with `atrium_exit` and no card. Not `atrium_done`, which reports and leaves the
   session running, and not a wait for the launcher: a minion is tagged `atrium:director`, and no agent can exit a
   director but the director itself.

## Things that went wrong, so you check for them

- A worker reports "already built" or "done" with no new commit: verify against main (`git grep`), do not land a
  no-op or a rename, close the card, fix the backlog status.
- The main history was rewritten between a design and a build (SHAs in a design note may not exist on main): branch from
  the current `claude/main`, never from an old sha in a note.
- Hook-interrupted tool calls (a message arrives instead of the result): read the message, retry the call.
- The Bash tool refuses `cd X && cmd`, `git -C`, `sleep`, and a `>` redirect in a command that touches branches:
  use PowerShell `Set-Location <dir>; ...` or a script piped to `bash -s`.
- A worker that ends a turn with no report: read its worktree (`git status`, `git log`) before answering, then tell it
  exactly what to commit.
