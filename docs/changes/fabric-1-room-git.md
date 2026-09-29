## Changelog

- **A room's git, by push and fetch.** See `scripts/room-git.ps1` and `docs/backlog-2.md` item 46.

  - `scripts/room-git.ps1` (new): `init`, `push-base`, `fetch` and `worktree` for a room, all run from the hub's
    side. The clone on the remote is made by PUSH over the operator's own ssh, so the remote needs no GitHub
    credential and no route back. `init` adopts a clone that is already there.
  - `init` puts the clone at `~/git/github/<owner>/<repo>` (Windows `$HOME\git\github\...`), sets
    `receive.denyCurrentBranch=updateInstead`, adds a git remote named for the room here, pushes `claude/main` to
    `hub-main` and checks `hub-main` out there. A rerun says `ok`. When git is missing on the remote it names the
    install and exits 3.
  - `worktree` makes `claude/<name>` off `hub-main` at `<clone>-worktrees/<name>` on the remote and prints its
    absolute path, the `cwd` for `atrium_launch room=<room>`. `fetch` brings the room's `claude/*` branches to
    `refs/remotes/<room>/claude/*` for the Release department to merge. Nothing here merges.
  - Windows remotes: the remote is `host:C:/path` (an `ssh://` url sends `/C:/...`, which git on Windows does not
    resolve), and upload-pack and receive-pack go through a small `~\.room-git\git.cmd` that puts git's folder on
    PATH, since a Cygwin git's children exit 127 without it.
  - `provision-room.ps1` calls `room-git.ps1 init` last, and `-Repo none` skips it.

## Test plan

## @LETTER@. A remote room's work comes back by git

Needs a room reachable over ssh (`m1mini`, `sg3`) and a local `claude/main`.

### @LETTER@1. init

Run `pwsh scripts/room-git.ps1 init <room>`, then run it again.

**Expected:** steps `ssh`, `git`, `repo`, `remote`, `push-base`, `checkout` and `done ok`. `git remote get-url <room>`
here shows the remote. The clone's `hub-main` is checked out and equals `claude/main`. The second run says `ok` on
every step.

### @LETTER@2. init adopts

On a room whose clone already exists (a `git init` with a pushed `hub-main`), run `init`.

**Expected:** it does not reinit or overwrite it, sets `updateInstead` if it was missing, and ends `done ok`.

### @LETTER@3. No git

On a remote with git off PATH, run `init`.

**Expected:** it prints the install for that OS (`xcode-select --install`, the distro package,
`winget install --id Git.Git -e`) and exits 3.

### @LETTER@4. worktree

Run `worktree <room> fb-proof` twice.

**Expected:** `cwd ok <absolute path>`, then `ok` with the same path. `atrium_launch room=<room> cwd=<that path>`
starts there.

### @LETTER@5. Work comes back

Commit in that remote worktree over ssh, then `fetch <room>` twice.

**Expected:** `<room>/claude/fb-proof` appears here as `new`, and `git log <room>/claude/fb-proof` shows the commit.
The second fetch says `ok`.

### @LETTER@6. push-base moves the work tree

Make a local change on a throwaway branch and `push-base <room> -From <branch>`. Push back with the default.

**Expected:** the remote `hub-main` and its work tree move each time.

### @LETTER@7. A dirty clone refuses

Edit a tracked file in the clone, then `push-base`. Restore the file, then `push-base` again.

**Expected:** the first fails with exit 5, says the work tree is not clean, and changes nothing. The second works.

### @LETTER@8. Windows

Steps 1, 4 and 5 on a Windows room, whose sshd default shell is PowerShell.

**Expected:** the remote url is `host:C:/...` and `remote.<room>.receivepack` names `~\.room-git\git.cmd`.

### @LETTER@9. Clean-up

Remove the proof branch and worktree on the remote (`git worktree remove`, `git branch -D`), and the
`refs/remotes/<room>/claude/fb-proof` ref here.

**Expected:** nothing of the proof is left on either side.

### @LETTER@10. Provision

Run `provision-room.ps1 <target>`, then again with `-Repo none`.

**Expected:** the first ends with the `room-git init` steps, and the second skips them.
