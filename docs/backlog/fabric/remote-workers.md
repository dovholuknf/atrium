# Placing a worker on sg3 or m1mini

For every director. Written by @fabric 2026-09-30, after both rooms were brought onto a claude/main build and f-019
git sync was proven live on each. What follows is the path that was actually run, step by step.

## The rooms

| Room | OS | Cap | Clone | Worktrees |
| --- | --- | --- | --- | --- |
| sg3 | Windows amd64 | 5 | `C:/Users/claude/git/github/dovholuknf/atrium` | `<clone>-worktrees/<id>` |
| m1mini | macOS arm64 | 5 | `/Users/claude/git/github/dovholuknf/atrium` | `<clone>-worktrees/<id>` |

Both have Go 1.26.2, claude 2.1.285 signed in, and atrium-control in the runner's `--mcp-config`. m1mini also has
make and codex. sg3 has no make, so a brief for sg3 says `go build -o build.claude/atrium.exe ./cmd/atrium` rather
than `make build`. Neither room has `gh` or a GitHub credential, and neither can push anywhere. Nothing a worker there
does needs either.

The cap is per room and held by the hub (`GET /_hub/launch-caps`), counted over live `atrium:subagent` cards on that
room. It is shared by every director, so count before you launch. The hub refuses the launch that would go over.

## The five steps

1. **Check the room is ready.** `curl -s http://127.0.0.1:7778/_hub/rooms` lists it with `"git":true`, and
   `curl -s http://127.0.0.1:7778/_hub/git/status` shows `rooms.<room>.state` `ok` with the sha of the hub's mirror.
   When claude/main has just moved and you want it now, `atrium_git_sync room=<room>` does the sync at once. Otherwise
   the hub syncs on its own when claude/main moves and when a room attaches.

2. **Make the worktree.** From any worktree of the atrium checkout on this machine:

   ```
   pwsh -NoProfile -File scripts/room-git.ps1 worktree <room> <id>
   ```

   It takes the item id, not a branch name, and makes branch `claude/<id>` off the room's `hub-main`, which the hub
   keeps equal to claude/main. The last step line is `room-git cwd ok <remote path>`. That path is the launch `cwd`.
   f-019 stage 2, where atrium makes this worktree itself on launch, is not built yet.

3. **Launch.** `atrium_launch` with `room=<room>`, `cwd=<the printed path>`, title `<id>: <what>`, tags
   `["atrium:subagent","dept:<yours>"]`, model and effort as usual, and the brief in `brief`. It is written as
   BRIEF.md on the room, which is the only way to put one there, since the Write tool cannot reach a remote folder.
   Set the alias after launch as you would here. Its reports reach you as `name@<room>`, and you answer that handle.

4. **The worker commits on `claude/<id>` and reports.** It never pushes or fetches. Tell it so in the brief.

5. **Bring the branch home.** The hub collects every room's `claude/*` every five minutes and on attach.
   `atrium_git_collect room=<room>` does it at once. The branch lands in the main checkout
   `D:/git/github/dovholuknf/atrium` as `refs/remotes/<room>/claude/<id>`, which every worktree of that repository
   sees. Merge it from there: `git merge <room>/claude/<id>` on your department branch. Nothing under `refs/heads`
   is ever written here by the hub.

## Finishing a remote worker

- **After you merge it here:** `atrium_cull card=<id>@<room> tip=<sha>`, where the sha is the
  `<room>/claude/<id>` head you merged. The room culls only if its branch is still at that sha, so a worker that
  committed again after the collect is kept. `into` alone does not work for a department branch, since the hub syncs
  only the integration branch to a room, and a cull checking `into=claude/<dept>` answers "there is no branch
  claude/<dept> to check it against". Without `tip`, the default `into=claude/main` works once the branch has landed
  on claude/main and the hub has synced it to the room.
- **Before it lands, or never:** exit the card, then remove the worktree and branch on the room. On m1mini that is
  plain `git worktree remove` and `git branch -D claude/<id>` over ssh. **On sg3, never run a bare `git` over ssh.**
  The ssh session's PATH finds a Cygwin git first, which cannot validate a Git for Windows worktree ("does not point
  back to .git/worktrees") and writes `/cygdrive` gitfiles if it makes one (f-005). Use the room's own git,
  `C:/Users/claude/.local/share/atrium-tools/git/cmd/git.exe`, or load `~/.atrium/toolchain/room-env.ps1` first,
  the way `room-git.ps1` does. The next collect prunes the branch from `refs/remotes/<room>/` here.

## What to expect

- **sg3's hooks are slow.** A tool call can sit about 30 seconds in "running PreToolUse hooks". That is not a
  permission wait: the card stays `running`. A worker there is slower, not stuck.
- **sg3 cannot reach the Linux hosts** (cdzrok, the WSL room). Work that needs them goes elsewhere.
- **hub-main can lag claude/main by one sync.** On sg3 the worktree started at f64c3716 while the hub moved the
  clone's claude/main to 3727f9cb during the run. It does not matter for a worker that merges later. When the base
  has to be the exact tip, run `atrium_git_sync` before step 2.

## Updating a room's binary (fabric does this)

Remote room restarts are @fabric's, and only when the room has been idle for 10 seconds. Idle means no claude process
on the machine, checked by ssh (`pgrep -fl claude` on m1mini, `Get-Process claude` in powershell on sg3), because
card pids on the board can be a day stale. The binary swap force-kills after 20 seconds. Then:

```
pwsh -NoProfile -File scripts/provision-room.ps1 <room> -FromCheckout -Repo none -SmokeCwd <clone path> -SmokeTo <you>@claude-sg4
```

Run it from a worktree at claude/main, since it builds whatever checkout it sits in. Look for `binary done`,
`attached ok` and `provision done ok`. Exit 8 is the smoke card alone. Both rooms gave one on 2026-09-30: sg3's first
smoke card never called a tool, and m1mini's launch got a hub 400 straight after the restart. A `-SmokeOnly` rerun,
or a hand launch, passed on both. A room restart is a reattach, so the hub syncs and collects it at once.

## The proof, 2026-09-30

- Both rooms updated to a build of claude/main b8edf0e2 and listed `git:true`. The first sync made claude/main in
  m1mini's clone, which had none.
- sg3: worker f-019-sg3 committed 04afd6c4 on claude/f-019-sg3. `atrium_git_collect` delivered it as
  `sg3/claude/f-019-sg3` here.
- m1mini: worker f-019-m1mini started on 3727f9cb (claude/main) and committed 6c804d11, which was delivered as
  `m1mini/claude/f-019-m1mini`.
- Both proof branches were then removed on their rooms, and the next collect pruned them here.
