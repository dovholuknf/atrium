# Fabric item 1: one command makes a room

Goal: `pwsh scripts/provision-room.ps1 <ssh-target>` takes a bare machine reachable by ssh (Windows, macOS, Linux;
amd64, arm64) to a room that runs claude workers and whose work merges here. No hand steps. The only thing clint
does is a sign-in, and the script names the exact command for it.

## What exists

- `scripts/provision-room.ps1` (sa92, backlog-2 item 46 stage 1, test plan BU) already:
  - probes OS and arch over ssh with BatchMode
  - finds the hub from the running `atrium run`
  - builds (`-FromCheckout`) or fetches the binary (release, which does not exist yet)
  - joins over direct, ziti or zrok, starts `room --detach` or `-Autostart`
  - `-Install claude,codex` from their vendors, and puts `~/.local/bin` on PATH
  - writes `~/.atrium/mcp.json` with atrium-control, and puts `--mcp-config` on the room's claude runner row
    through the hub (`PUT /v1/harnesses/claude` with `X-Atrium-Room`)
  - `-Remove` from a manifest
  - prints `provision <step> <status> <detail>` lines with fixed exit codes
  - on the remote: plain `sh -s` on Unix, Windows PowerShell 5.1 `-EncodedCommand` on Windows. No pwsh needed there.
- Proven: claudevm (Windows), WSL (Linux), m1mini (mac arm64). m1mini is a room now, with a manifest, `~/git/github`,
  and claude and codex installed by the script.
- Not there yet:
  - the claude sign-in check
  - git on the remote, the clone, git remotes here, push and fetch
  - a way to make a remote worktree for a cross-room launch
  - a smoke test
  - PATH for the systemd unit (item 46, still open)
  - the default binary: it is the release, and there is no release, so every real run needs `-FromCheckout`
- `docs/remote-launch.md` section 3: atrium (the binary) does not learn git or create worktrees. All git here lives
  in scripts on this side, run by an operator or a director, so the rule holds.
- `scripts/live/*` is this machine's own hub and room start and deploy. It is not touched.

## What gets built

### fb01: provision-room.ps1 finishes the job (one file)

1. **Binary default.** With no release on GitHub and a checkout around the script, it builds from the checkout
   and says so with a `warn` line. `-FromCheckout` stays as the explicit form.
2. **`auth` step.** Checks the remote claude is signed in, with whatever the CLI offers non-interactively
   (`claude auth status`, or a `-p` probe with a timeout; the worker finds out on m1mini). If it is not signed in,
   the step prints `warn` and the exact command, e.g. ``ssh -t m1mini claude`` then `/login`, or
   `claude setup-token`. It never carries a credential.
3. **Folder trust.** Item 67 fixes this in the binary. The script only checks that the room's build has it, and
   warns on an older one.
4. **systemd PATH.** `packaging/atrium.service` gets a login-shell PATH, so a unit-run room finds `~/.local/bin`
   runners.
5. **`smoke` step, last.** `POST http://<hub>/v1/launch` with `X-Atrium-Room: <room>` and cwd set to the new clone,
   a small model, and a prompt that says back. Then wait, `/v1/.../exit`, and confirm the card left.
   - Say back to what: `-SmokeTo <handle>`, default `$env:ATRIUM_CARD` (the card running the script) when set.
   - With no caller card, it checks the card on the hub reached a turn end after a successful atrium-control call.
   - `-NoSmoke` skips it.
6. Exit codes: add 8 for a smoke failure. The auth warning is not a failure: the room works and only needs clint.

### fb02: scripts/room-git.ps1 (new file, plus a one-line call in provision)

The clone is made by PUSH from here, so the remote needs no GitHub credential and no route back to this machine.

- **`room-git.ps1 init <room> [-Repo atrium] [-Path ~/git/github/<owner>/<repo>]`.**
  - Checks git is on the remote, and names the install when it is not (`xcode-select --install` on mac, the distro
    package on Linux, `winget install Git.Git` on Windows).
  - `git init` there with `receive.denyCurrentBranch=updateInstead`.
  - Here: `git remote add <room> ssh://<host>/<path>`.
  - Pushes the base (below) and checks out `hub-main` on the remote.
  - Idempotent. Provision calls it by default, and `-Repo none` skips it.
- **`room-git.ps1 push-base <room>`.** `git push <room> claude/main:refs/heads/hub-main`. It is what a director
  runs before launching remote work.
- **`room-git.ps1 fetch <room>`.** `git fetch <room> '+refs/heads/claude/*:refs/remotes/<room>/claude/*'`. Release
  then merges `<room>/claude/<branch>` here.
- **`room-git.ps1 worktree <room> <branch>`.** On the remote: `git worktree add -b <branch> <root>/<slug>
  hub-main`. It prints the remote path, which is the cwd for `atrium_launch room=<room>`. A director uses it the way
  it uses `git worktree add` here.
- **Windows remotes.** The OpenSSH default shell may be cmd or powershell, so it sets
  `remote.<room>.receivepack` and `uploadpack` to forms that shell runs. It is proven on claudevm if that machine
  is up.
- **Docs.** `docs/packaging.md` "Provisioning a room over ssh" gains the git flow. `docs/remote-launch.md` section 6
  is marked answered: branches come back by fetch, run from this side.

### Test plan

Section FA: one command, bare machine to smoke pass, rerun is all `ok`, `-Remove`. Section FB: push-base, remote
worktree, remote worker commits, fetch, merge here.

## Proof, in order

1. m1mini (mac arm64): `-Remove`, then the one command from nothing, then a remote worker round trip through FB.
2. Linux: WSL over `ssh localhost`.
3. Windows: sg3 (already a room, not signed in, no autostart), then claudevm.
4. The old x86 Mac mini, when it appears.

Re-provisioning a remote room restarts that room, which is allowed. This machine's room and hub are never touched.

## Workers

fb01 and fb02, Sonnet 5.5 at medium effort, in parallel. They touch different files except provision's one call
line, which fb02 adds last. fb03 is a prover for Linux, Windows and x86 once both are merged. **Held: nothing is
launched until after the room restart (atrium-87300, 2026-09-28).**

## sa75's sg3 findings (Windows, forwarded by atrium-87300), folded in

- sg3 is room `sg3`: binary 03ad918d247b, claude 2.1.284, mcp-config ok, no autostart yet.
- `claude auth status` answers without a prompt (loggedIn=false on sg3). fb01's `auth` step uses it.
- A git clone of the private repo fails silently there. That confirms the clone has to be made by push (fb02).
- Over ssh on Windows, `Get-ScheduledTask` fails with "Cannot connect to CIM server. Access denied".
  - sa75 fixed the state probe in 5ced807 (claude/provision-sg3). fb01 starts from that commit.
  - The autostart install, the binary swap and `-Remove` still call the ScheduledTask cmdlets. fb01 moves them to
    `schtasks.exe`, or tolerates the error and says so.
  - Proven on sg3, not just claudevm.
