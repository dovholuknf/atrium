# Install, update and run atrium as services, for a human on a fresh machine

Status: proposed, not built. A design for review.

Origin: backlog item `o-install-update-lifecycle` (ops, high), filed 2026-10-23 by @orchestrator for clint. It folds in
`r-rooms-as-services`, `f-install-wires-atrium-control` and `bug-install-live-scripts-skips-room-ssh-json`, and it
builds on `docs/rnd/room-autostart-design.md` (shelved 2026-09-29), `docs/release/packaging.md`,
`docs/rnd/rolling-restart-design.md` (the pty host) and `docs/rnd/room-deploy-hold-design.md`.

## 0. The answer, in one paragraph

Today a working atrium on a machine is the product of about 2000 lines of private PowerShell under
`C:\Users\claude\.atrium2\scripts`, three service scripts in the repo that nobody runs any more, and
`scripts/provision-room.ps1`. None of it is reachable by a person who downloaded the binary. This design moves the
lifecycle into the binary as four verbs: `atrium install`, `atrium update` (with an automatic rollback), `atrium
service` (start, stop, restart, status, logs, per role) and `atrium uninstall`. The hub and the room are registered
separately, each as the user who owns it, with the platform's own scheduler, so both survive a log-off and a reboot.
A room joins a hub with the join string it already uses, passed to `atrium install --room --join`. The deploy
scripts' hard-won rules (rename aside, gate on serving and on attached, revert on not serving, never a kill, one room
per machine) become the code of `atrium update`, and the private scripts shrink to nothing.

## 1. What exists today

### 1.1 In the binary

| Command | What it does now | Gap for a human |
| --- | --- | --- |
| `atrium run` | the hub, and starts this machine's room detached when none answers (`ensureRoom`) | the room it starts dies with the session that ran `atrium run` |
| `atrium room` | the room, from what `room join` saved under `<state dir>/room` | needs someone or something to start it |
| `atrium room --detach` | starts the room outside the calling shell | survives the ssh session, not a log-off or reboot |
| `atrium room join <string>` | makes a key, gets a certificate, saves `room.json`, runs (`--no-run` to stop there) | nothing |
| `atrium rooms add <name>` | on the hub machine, records the room and prints a join string, good once for an hour | nothing |
| `atrium rooms token <name>` | mints a fresh join string, retiring the old one | nothing |
| `atrium stop` | asks a room to wind down (`--now`, `--runners`), never a kill | does not know about any supervisor |
| `atrium version` | version, commit, tree state, board hash, platform, stamped by the linker | nothing |
| `atrium hook install` | writes the runner's hook config against an installed binary | a person must know to run it |
| `atrium room setup --spec` | applies a room.yaml (work root, caches, agent pack) | nothing |
| `--accept-upgrades` and the hub's builds directory | the hub offers a binary per platform, the room fetches, checks the sha256, renames it in and stops | "a room started by hand stays down", and there is no rollback |
| hub-asked room restart (`roomrestart.go`) | the room parks its agents, spawns a detached restarter, winds down | fine for a restart, not an update |
| `atrium backups` | the hub snapshots its own store every ten minutes, `backups restore` puts one back | the room's store has no such snapshot |

### 1.2 In the repo, not in the binary

| File | What it does | State |
| --- | --- | --- |
| `scripts/atrium-service.ps1`, `scripts/atrium-autostart.ps1` | Windows logon task (`InteractiveToken`, `LeastPrivilege`) with install, start, stop, restart, status | proven on claudevm, stops at log-off |
| `scripts/atrium-service.sh`, `packaging/atrium.service`, `packaging/atrium.plist` | systemd user unit (linger optional), macOS LaunchAgent | proven on WSL and m1mini, the LaunchAgent cannot load over headless ssh |
| `packaging/` and `scripts/package-*` | deb, rpm, pkg, MSI, scoop manifest, with a postinstall that enables the unit | built, none published |
| `scripts/provision-room.ps1` | a remote machine becomes a room over ssh: binary, toolchain, git, account checks, work root, join, autostart, folders, mcp, auth, smoke | in use, 1400 lines, Windows hub only |
| `website/docs/install.md` | build from source, copy the binary, run the service script | accurate, and long for what it achieves |

### 1.3 The private scripts, which are the real lifecycle

| Script | What it does that nothing else does |
| --- | --- |
| `build-deploy.ps1` | builds only from a clean `claude/main`, stamps commit and tree |
| `live-common.ps1` | `Save-Revert` (snapshot named by commit and board hash), `Install-Atrium` (stage to `atrium.next.exe`, rename the live one aside, rename the new one in, put the old back on failure, keep three), `Start-Jobless` (start outside any Windows job object, by WMI or breakaway), `Wait-RoomAttached` (this room, by name, on a connection made after the swap, still attached ten seconds later), `Test-RoomServes` (the room answers `/v1/settings` from its store, twice) |
| `deploy-batch.ps1` | the full local sequence: preflight (would the stop refuse), new-context wait, deploy hold, snapshot, stop room, stop hub, swap, start hub, start room, gate, revert when not serving, exit 2 when serving but not attached |
| `atrium-deploy.ps1` | cross-builds remote rooms, copies by scp, stops, swaps keeping `atrium.prev`, starts detached, asks the hub whether the room came back |
| `start-atrium-*.ps1`, `stop-atrium.ps1` | start the hub and room with their exact arguments after a reboot, stop them before one |
| `refuse-supervised.ps1` | refuses to stop a room from a shell that room supervises |
| `install-live-scripts.ps1` | copies the repo's `scripts/live` over the live ones |

The hub's arguments (`--no-room`, `--link 0.0.0.0:7779`, `--link-advertise`, the ziti identity, `--board`,
`--board-views`) and the room's (`--dir`, `--db`, `--http`, `--agent`) exist only as variables in
`live-common.ps1`. A reboot of sg4 brings nothing back until someone runs `start-atrium.ps1`.

`bug-install-live-scripts-skips-room-ssh-json` is already fixed in the live copy: `install-live-scripts.ps1` now copies
every file but `.md`, and its header names `room-ssh.json`. The test the item asks for is still owed. Section 9 makes
the script itself go away.

### 1.4 What goes wrong for a person today

1. There is no install. The binary lands wherever it was unpacked, and hooks, tasks and swaps name that path.
2. Claude sessions on a fresh install get no atrium-control (`f-install-wires-atrium-control`). Only provision step 11
   writes `~/.atrium/mcp.json` and adds `--mcp-config` to the claude row.
3. Nothing starts the hub at boot on any platform. The room's registration exists but stops at log-off on Windows and
   macOS, and the sg3 room died that way on 10/21.
4. A second room was started by hand on sgg while one was already up (10/21). Only the store lock stopped it.
5. Update and rollback exist only in private scripts, with paths for one machine written into them.
6. `packaging.md` names self-update against a package manager as unsolved.

## 2. Principles

1. **The binary owns its lifecycle.** This reverses `packaging.md`'s "registration belongs in packaging, not in the
   binary", and it is the first open decision (section 11). The reason: three service scripts in two languages
   drifted from each other and from the private scripts, and a person on a fresh machine has the binary, not the
   repo. Packages keep their scriptlets, and those scriptlets call `atrium service install`, so there is one
   implementation.
2. **As a user, never as SYSTEM or root.** The constraint in `packaging.md` stands: runners need the account's PATH,
   home, runner sign-in and ssh agent. Every registration runs as the account that owns the role.
3. **Stop is `atrium stop`, never a kill.** Every stop below winds down first and forces only after the wait.
4. **One room per machine, enforced at install and at start**, not only by the store lock.
5. **Every step is idempotent and says what it did,** in the provision script's form,
   `install <step> <ok|done|skip|warn|fail> <detail>`. `--plan` prints the steps and changes nothing.
6. **A package manager owns its file.** A packaged atrium never renames itself. It prints the manager's command.
7. **Elevation is one printed command, run by a human,** never attempted, and never `LocalAccountTokenFilterPolicy`.

## 3. Where things live

One layout on every platform, under the state directory the join used (`~/.atrium` unless `WORKTREE_ROOT` says
otherwise, `internal/cardroot`). The binary moves into it so that the swap is always a rename within one directory.

| What | Path | Note |
| --- | --- | --- |
| the binary | `~/.atrium/bin/atrium` (`atrium.exe`) | the one path hooks, mcp.json and registrations name |
| the pty host copy | `~/.atrium/bin/atrium-pty` | made by the room today, unchanged |
| earlier builds | `~/.atrium/bin/versions/atrium-<version>-<commit7>` | the last three, for rollback |
| a staged build | `~/.atrium/bin/atrium.next` | only during an update |
| on PATH | Unix: a symlink `~/.local/bin/atrium` to the binary. Windows: `~\.atrium\bin` on the user Path | no admin |
| the install record | `~/.atrium/install.json` | section 3.1 |
| the hub | `~/.atrium/hub/` (keys, store, snapshots) | `hubDir()` today |
| the room | `~/.atrium/room/` (key, certificate, `room.json`, `supervisor.json`), `~/.atrium/atrium.db` | `roomDir()`, `defaultRoomDB()` today |
| logs | `~/.atrium/logs/hub.log`, `room.log`, rotated at 10 MB, five kept | Linux also journald |
| runner wiring | `~/.atrium/mcp.json`, `~/.claude/settings.json` hooks | f-install-wires |

Today's live layout on sg4 (`~/.atrium/bin/atrium.exe` with state in `~/.atrium2`) is a pinned state dir and fits
this as is. A Unix install that already has `~/.local/bin/atrium` as a real file has it moved into `~/.atrium/bin` and
replaced by the symlink.

### 3.1 The install record

`~/.atrium/install.json` is what every later verb reads, so no verb guesses from the environment it was started in.

```json
{
  "installed_by": "atrium",
  "binary": "C:/Users/localai/.atrium/bin/atrium.exe",
  "state_dir": "C:/Users/localai/.atrium",
  "roles": {
    "hub":  {"service": "task-s4u", "name": "atrium-hub", "args": ["run", "--no-room", "--addr", "127.0.0.1:7778"]},
    "room": {"service": "task-s4u", "name": "atrium-room",
             "args": ["room", "--db", "C:/Users/localai/.atrium/atrium.db"]}
  },
  "current": {"version": "v0.6.0", "commit": "6c5383d", "sha256": "..."},
  "history": [{"at": "2026-10-23T14:02:11Z", "from": "v0.5.2", "to": "v0.6.0", "outcome": "ok"}]
}
```

`installed_by` is `atrium`, `scoop`, `deb`, `rpm`, `brew`, `msi` or `pkg`. Packages write it from their scriptlet,
and the linker variable `packaging.md` asked for (`cli.InstalledBy`) is the fallback when the record is missing.

## 4. The commands a human types

Every case starts with getting one binary onto the machine, then one `atrium install`.

### 4.1 Get the binary

| OS | No admin | With a package manager |
| --- | --- | --- |
| Windows | download `atrium_<v>_windows_amd64.zip` from the release, unzip, run `.\atrium.exe install` | `scoop install atrium`, or the MSI |
| macOS | download the tarball, `./atrium install` | `brew install dovholuknf/tap/atrium`, or the `.pkg` |
| Linux | download the tarball, `./atrium install` | the deb or rpm, which runs `atrium service install` for the installing user |

From source it is `go build -o build.claude/ ./cmd/atrium` and then `build.claude/atrium install`, which copies
itself out of the build folder. That copy is the point: nothing ever names `build.claude/` again.

### 4.2 One machine, hub and room together

```
atrium install
```

That is the whole of it. It installs the binary, registers `atrium-hub` and `atrium-room` for this account, starts
both, wires claude to atrium-control, checks claude is signed in, and ends with:

```
install done ok
  board      http://127.0.0.1:7778
  hub        running, starts at boot (task atrium-hub)
  room       sg4, attached, starts at boot (task atrium-room)
  claude     signed in, atrium-control wired
  next       atrium service status   |   atrium update --check
```

### 4.3 The recommended split: hub as you, room as `localai`

`website/docs/accounts.md` recommends that agents never run as the operator. On one machine:

```
# as you
atrium install --hub
atrium rooms add lab1                 # prints: atrium room join <string>

# as localai (runas, su - localai, or ssh localai@localhost)
atrium install --room --join <string>
```

### 4.4 A room on another machine

On the hub's machine, once:

```
atrium install --hub --link 0.0.0.0:7779 --link-advertise sg4.lan:7779
atrium rooms add m1mini
```

On the room's machine, as the account that will run the room:

```
atrium install --room --join <string>
```

Over ssh from the hub, `scripts/provision-room.ps1 localai@m1mini` keeps doing this with its extra steps, and its
install, join and autostart steps become one remote `atrium install --room --join`. Section 8 has the join.

### 4.5 Day to day

```
atrium service status              # both roles: registered, enabled, running, pid, build, attached
atrium service restart room        # atrium stop, then the supervisor starts it
atrium service stop hub            # sticky: stays down across reboots until `service start`
atrium service logs room -f
atrium update --check              # is there a newer release, and what it would change
atrium update                      # update both roles here, with rollback
atrium update --rollback           # back to the previous build
atrium uninstall                   # registrations and the binary. --purge also takes ~/.atrium
```

## 5. What `atrium install` does

Flags: `--hub`, `--room`, neither meaning both. `--join <string>` for a room. Hub flags pass through (`--link`,
`--link-advertise`, `--transport`, `--atrium-identity`, and any other `atrium run` flag), and are recorded in the
install record as the hub's arguments. `--no-service` installs and wires but registers nothing. `--plan` changes
nothing. `--boot=false` registers the at-logon form instead of the at-boot form (section 7).

| Step | What it does | Refuses or warns when |
| --- | --- | --- |
| `preflight` | OS, arch, account. Is this account an administrator or root (the `room-account.ps1` probe) | admin: `warn`, `--require-dedicated` makes it `fail` |
| `existing` | finds every atrium on the machine: registrations of any user it can read, running `atrium run` and `atrium room` processes, `room.json` | a room of another account or another state dir: `fail`, names it and the `uninstall` that clears it. One room per machine |
| `packaged` | reads `installed_by` | a packaged binary: skips `binary` and `path` |
| `binary` | copies itself to `~/.atrium/bin`, stage then rename. Already there and the same sha256: `ok` | |
| `path` | the symlink, or the user Path entry. Says a new shell is needed | |
| `state` | pins the state dir, writes `install.json` | a `WORKTREE_ROOT` in the environment that differs from an existing record: `fail`, never silently a second state dir |
| `hub` | `--hub`: makes the hub's keys and store the way `atrium run` does. With `--link` wide, checks the advertise address resolves | Windows with a wide link: `warn` with the one elevated `New-NetFirewallRule` line |
| `join` | `--join`: `atrium room join --no-run`. Both roles on one machine and no `--join`: mints the room's own join the way `ensureRoom` does | an expired or used string: `fail` naming `atrium rooms token` |
| `hooks` | `atrium hook install` for each runner found on PATH | |
| `mcp` | f-install-wires step 1: writes `~/.atrium/mcp.json` with the `mcp-relay` entry, never over an atrium-control entry the operator made, and sets `--mcp-config` on the claude row | |
| `service` | registers each role (section 7) with `--started-by <kind>:<nonce>` and the pinned environment | the at-boot form needs elevation: `warn`, prints the command, registers the at-logon form meanwhile |
| `start` | starts each role through its supervisor, never directly | |
| `verify` | hub `/_hub/health`, room serves `/v1/settings` twice ten seconds apart, room attached on the hub on a connection made after the start, `POST /v1/preflight` for runner sign-in from inside the supervised room | claude not signed in: `warn` naming `claude auth login` |

A rerun changes only what is wrong, as provision does. A rerun with different hub flags rewrites the registration and
restarts the hub, and says so.

## 6. `atrium update` and its rollback

### 6.1 Where a new build comes from

| Source | Spelling | Check |
| --- | --- | --- |
| a GitHub release | `atrium update` (latest), `atrium update --to v0.6.1` | the release's `checksums.txt`, and its signature once releases are signed |
| a file | `atrium update --from ./atrium.exe` | it runs `version` and reports a commit and a clean tree, which is `Test-NewBuildStamped` |
| the hub | a room with `update_from_hub` on takes the hub's offer (today's `--accept-upgrades`) | the sha256 in the offer, as today |

All three end in the same engine, so a room updated by its hub gets the same gate and rollback as a person typing
`atrium update`. That closes the gap in `upgrade.go`, where a hand-started room stays down and a bad build stays in.

### 6.2 The sequence

The engine is the NEW binary, run from `~/.atrium/bin/atrium.next` as a detached process outside any job object,
cgroup or launchd job of the room (`Start-Jobless` in Go, which `internal/detach` already half is). So the process
doing the swap is never the one being swapped and never a child of what it stops. A room's hub-offered update
spawns it the way `roomrestart.go` spawns its restarter, and the room then winds down.

1. **Fetch and check.** Into `atrium.next`, beside the live binary. Verify the hash. Run `atrium.next version`: it
   must exit 0, name the right platform, and report a commit.
2. **Refuse what cannot work.** A packaged install: print the manager's command (`scoop update atrium`, `brew upgrade
   atrium`, `sudo apt install ./atrium_<v>.deb`) and stop. Same build as running: `ok`, nothing to do.
3. **Snapshot.** Copy the live binary to `versions/atrium-<version>-<commit7>` (`Save-Revert`), keep three. Snapshot
   the room's store with `VACUUM INTO ~/.atrium/backups/atrium-<stamp>.db`, and take a hub snapshot through the
   existing backup path.
4. **Preflight the room.** `/v1/preflight`: would the stop refuse because the pty host dies with it? Then stop here,
   unless `--kill-runners`. Wait for new contexts under way. If runners will not survive the restart, set the deploy
   hold and wait for quiet (`room-deploy-hold-design.md`), up to five minutes.
5. **Stop.** The room first: `atrium stop`, wait for its ports and its store, then the supervisor's stop so it does
   not restart the old build. Then the hub, which holds nothing. With pty host on, the runners stay up.
6. **Swap.** Rename the live binary aside, rename `atrium.next` in, put the old one back if the second rename fails
   (`Install-Atrium`, which is also `installUpgrade`).
7. **Start** the hub, then the room, each through its supervisor.
8. **Gate.** The hub reports the new build at `/_hub/health`. The room serves `/v1/settings` now and ten seconds
   later. The room is attached on the hub on a connection made after the swap, still attached ten seconds later.
9. **Decide.**
   - All pass: record `ok` in the history, lift the hold, done.
   - The hub is not healthy, or the room is not serving: **roll back** (6.3), exit 1.
   - The room serves but is not attached: **fail without rolling back**, exit 2, as `deploy-batch.ps1` does. The room
     works for its agents, and a rollback would restart every card again for what may be the hub refusing it.

The ordering and the waits are `deploy-batch.ps1`'s, which were each learned from an outage. Section 9 maps them.

### 6.3 Rollback

Automatic, inside the same run, when step 9 says so: stop as in step 5, rename the snapshot back in, start, gate
again, record `rolled-back` with the reason and the last 20 log lines. If the rollback also fails the gate, it stops
there and says both, and leaves the old build in place. It never loops.

By hand: `atrium update --rollback` runs the same sequence with the newest snapshot as the source, and `--to
<version>` picks another from `versions/`.

**The store is not rolled back by default.** Migrations are recorded by name and an older binary skips names it does
not know (`store.migrate`), so an older build runs on a newer schema as long as the migrations it skipped only added
things. Restoring the snapshot would throw away every card change since the update. So the default keeps the store,
and `--restore-store` puts the step 3 snapshot back. Whether the binary should know which migrations are not
additive and refuse a rollback across them is open decision 6.

### 6.4 Remote rooms

From the hub, `atrium update --rooms all` (or a list) does what `atrium-deploy.ps1` does without ssh: the hub
offers the build for each room's platform from its builds directory, each room with `update_from_hub` on runs 6.2
locally, and the hub reports one line per room as the deploy does today (`sgg: success`, `sgg: failed - not serving,
rolled back`). A room with it off is listed as `skipped, update_from_hub is off` and is updated over ssh or by
hand. The hub never pushes a binary, as `internal/link/upgrade.go` requires.

## 7. Services, per operating system

### 7.1 The mechanism

Each role is its own registration so either can stop, restart or update alone. Names: `atrium-hub` and `atrium-room`
(task names, `atrium-hub.service` and `atrium-room.service`, launchd labels `io.github.dovholuknf.atrium.hub` and
`.room`). Today's single `atrium` registration is migrated by `atrium install` and removed.

| | Linux | Windows | macOS |
| --- | --- | --- | --- |
| at boot (the default) | systemd user unit, `loginctl enable-linger` | scheduled task, S4U principal, boot trigger plus a 5 minute repeat, `IgnoreNew` | LaunchDaemon in `/Library/LaunchDaemons` with `UserName` set to the account |
| at logon (`--boot=false`, and the fallback) | user unit without linger | today's logon task (`InteractiveToken`) | LaunchAgent |
| runs as | the account | the account | the account |
| elevation | none on most distros, `sudo loginctl enable-linger <user>` elsewhere | possibly none, settled by the S4U spike | `sudo` once to write and load the plist |
| survives log-off | yes | yes | yes |
| survives reboot | yes | yes | yes, except an unplanned reboot with FileVault on |
| restart on crash | `Restart=on-failure` | the repeat trigger, and three restarts a minute apart | `KeepAlive` `SuccessfulExit false` |
| logs | journald and `~/.atrium/logs` | `~/.atrium/logs` | `~/.atrium/logs` |

This is `room-autostart-design.md` section 2, unshelved and applied to both roles. Its S4U spike on claudevm
(section 6 there) is still the gate for the Windows at-boot form. Until it passes, Windows registers the logon task
and `service status` says `survives: logon only`.

The hub holds no terminal, so it can take the at-boot form on Windows whatever the spike finds about ConPTY.

### 7.2 What every registration pins

- The exact command line from the install record, with `--started-by <kind>:<nonce>` (`room-autostart-design.md`
  section 4), and the nonce in `<state dir>/room/supervisor.json`.
- `HOME` (`USERPROFILE`), `WORKTREE_ROOT` set or cleared to match the state dir, `--db` explicitly, and the PATH
  captured once from the account's login shell at install (bounded, no terminal). `install` recaptures it, and
  `service status` warns when the login shell's PATH has drifted from it.
- `ATRIUM_HOSTS` and the other settings the live scripts carry in the environment become hub settings first, so a
  registration carries no secret and no host list.

### 7.3 The pty host under a supervisor

The room starts the pty host from `atrium-pty`, and the host must outlive a room restart. Each supervisor would
otherwise take it down with the room:

- systemd: `KillMode=process` on `atrium-room.service`, so a stop ends the room and not the host. The host is then
  outside the unit's lifetime, which is the intent.
- Windows: the task runs its action in a job object. The room starts the host with `CREATE_BREAKAWAY_FROM_JOB`, and
  the registration allows breakaway. `Test-RunnersSurvive` in the deploy scripts is the check, and it moves into
  `service status` as `runners survive a restart: yes|no (why)`.
- launchd: `AbandonProcessGroup` true on the room's plist.

### 7.4 Stop, start, and one room per machine

`service stop` is sticky (disable, then `atrium stop`), `service start` enables and starts, `service restart` never
changes whether it is enabled. A bare `atrium stop` on a supervised room says what the supervisor will do next. This
is `room-autostart-design.md` section 3 unchanged.

A hand start of `atrium room` when `install.json` records a registered room, and no `--started-by` matching it, is
refused: `this machine's room is registered as task atrium-room. use: atrium service start room`. `--manual` gets
past it for debugging. `atrium run` stops starting a room of its own when a room registration exists. Together with
the `existing` step at install, the second room on sgg on 10/21 cannot happen without someone typing `--manual`.

## 8. How a room joins a hub

The join string stays as it is. What changes is that joining and starting become one step that leaves a supervised
room behind.

1. **The hub is reachable.** Direct: the hub listens wide (`--link 0.0.0.0:7779`) and advertises an address the room
   can dial (`--link-advertise`), and an inbound firewall rule allows 7779. On Windows that rule is the one elevated
   line `atrium install --hub` prints. Ziti or zrok: no inbound port, the hub's identity or share is set at install.
2. **The hub names the room.** On the hub's machine, as the hub's account: `atrium rooms add <name> [--transport
   direct|ziti|zrok] [--spec room.yaml]`. The string is good once, for an hour. `atrium rooms token <name>` mints
   another.
3. **The room machine joins and registers.** As the room's account: `atrium install --room --join <string>`. That is
   `room join --no-run`, the private key made here and never sent, then section 5's remaining steps.
4. **Both sides agree.** `atrium rooms ls` on the hub shows it connected, and `atrium service status` on the room
   shows `attached to <hub> as <name>`.

Rejoining (a new hub, a lost certificate) is `atrium rooms token <name>` on the hub, then
`atrium install --room --join <new string>` on the room, which rejoins and restarts through the supervisor. Leaving
is `atrium uninstall --room` there and `atrium rooms rm <name>` on the hub.

`provision-room.ps1` keeps everything that is not this: account and rights checks, the toolchain, git, the work root
and agent pack, Defender, folders, auth and the smoke. Its binary, join, autostart, restart and mcp steps become
`atrium install` and `atrium service` run over ssh, so the remote and the local path are the same code.

## 9. The private scripts, retired

| Today | Becomes |
| --- | --- |
| `build-deploy.ps1` | stays on the build machine (it is about claude/main, not about installing), feeding `atrium update --from` |
| `live-common.ps1` `$HubArgs`, `$RoomArgs` | the install record's `args`, written once by `atrium install --hub ...` |
| `Save-Revert`, `Install-Atrium`, `Test-NewBuildStamped` | `atrium update` steps 1, 3 and 6 |
| `Stop-Room`, `Stop-Hub`, `Start-Jobless` | `atrium service stop` and the detached engine of 6.2 |
| `Wait-RoomAttached`, `Test-RoomServes`, revert in `deploy-batch.ps1` | `atrium update` steps 8 and 9 |
| `deploy-batch.ps1`, `deploy-hub-only.ps1`, `deploy-ready.ps1` | `atrium update`, `atrium update --hub` |
| `atrium-deploy.ps1` remote rooms | `atrium update --rooms`, through the hub, ssh only for a room with `update_from_hub` off |
| `start-atrium*.ps1`, `stop-atrium.ps1` | `atrium service start` and `stop`, and nothing at all after a reboot |
| `refuse-supervised.ps1` | a check inside `atrium service stop room` and `atrium update`, which already know the room's pid |
| sg4-control and its `-IKnowItKillsMyShell` | a third role, `atrium install --room --name sg4-control --http 127.0.0.1:7791 ...`, registered like any room. Whether a machine may hold two rooms is open decision 5 |
| `install-live-scripts.ps1` | deleted with the scripts it copies |
| `scripts/atrium-service.*`, `atrium-autostart.ps1` | thin wrappers that call `atrium service`, kept one release for anyone who scripted them, then deleted |

## 10. Staged plan and follow-up items

1. **Spikes on claudevm only** (from `room-autostart-design.md` section 6): the S4U task hosts ConPTY and a claude
   session through one tool call, with and without elevation to register. The Windows at-boot form waits on it.
2. **`atrium service`** in Go for the at-logon forms that exist today, both roles, with `status` and `logs`. The repo
   scripts become wrappers. @fabric.
3. **`atrium install`** with the record, `binary`, `path`, `state`, `hooks`, `mcp` (closes the install half of
   `f-install-wires-atrium-control`), `service`, `start`, `verify`. @runtime and @fabric.
4. **The one-room guard**: `--started-by`, the hand-start refusal, `atrium run` deferring to a registration.
   @runtime.
5. **`atrium update`** for the local machine, with the detached engine, the gate and the rollback, ported from
   `deploy-batch.ps1` and proven against it on claudevm before sg4 uses it. @runtime.
6. **At-boot forms**: Linux linger by default, macOS LaunchDaemon, Windows S4U if the spike passed. @fabric.
7. **`atrium update --rooms`** through the hub, replacing `atrium-deploy.ps1`. @runtime.
8. **provision-room.ps1** calls `atrium install` remotely. @fabric.
9. **Docs**: `website/docs/install.md` rewritten to section 4, `packaging.md` sections 1 and 2 of "Three things"
   updated to the decisions, `website/docs/control-mcp.md` fixed (f-install-wires step 4). @merge.
10. **Retire the private scripts** on sg4, one at a time, each after its replacement has done one real deploy.
11. Owed regardless: the `install-live-scripts.ps1` test from the bug item, until step 10 deletes the script.

Nothing touches sg3, sgg, m1mini or sg4 until clint says so, room by room.

## 11. Open decisions for the operator

1. **The binary registers its own services, reversing `packaging.md`.** Recommend yes. The scripts drifted, a person
   has the binary and not the repo, and packages can call the same verb from their scriptlets.
2. **At boot or at logon by default.** Recommend at boot for both roles everywhere, with `--boot=false` for a desktop
   user who wants atrium only while logged in. Windows uses the logon task until the S4U spike passes.
3. **If Windows S4U fails the spike: auto-logon with a lock task, or Windows rooms stay logon-only.** Recommend
   logon-only for the room and at-boot for the hub (which needs no ConPTY), and revisit auto-logon only for sgg if
   its outages continue. Auto-logon leaves a stored password and a briefly unlocked desktop.
4. **Binary location.** Recommend `~/.atrium/bin` on every OS with a `~/.local/bin` symlink on Unix, so the swap is
   always a same-directory rename and Windows matches today. The alternative, `%LOCALAPPDATA%\Programs\atrium`, is
   more Windows-native and splits the binary from the state.
5. **Two rooms on one machine** (claude-sg4 and sg4-control). Recommend allowing it only by name with
   `--allow-second-room`, each with its own state dir, ports and registration, and keeping the default refusal.
6. **Store rollback.** Recommend keeping the newer store on rollback by default, and adding a `breaking: true` mark
   to migrations so `--rollback` across one refuses unless `--restore-store`. The mark is a schema.go convention and
   needs no edit to a shipped migration.
7. **Updates from the hub.** Recommend replacing `--accept-upgrades` with a room setting `update_from_hub`, off by
   default, set at join with `atrium install --room --join ... --update-from-hub`. The hub still only offers.
8. **Release source and signing.** Recommend GitHub Releases with `checksums.txt` now, and a signature on
   `checksums.txt` (minisign or cosign, a key clint holds) before `atrium update` fetches from the internet by
   default. Until then `atrium update` without `--from` asks for `--yes`.
9. **Package-managed installs.** Recommend refusing the self-swap and printing the manager's command, per
   `packaging.md`, with the gate still run after the manager's update by `atrium service restart --verify`.
10. **macOS FileVault on a room.** Recommend leaving FileVault on, routing planned reboots through `atrium service
    reboot` (`fdesetup authrestart`), and accepting that an unplanned reboot waits for a human. This is
    `room-autostart-design.md` question 3, still unanswered.
11. **Where the hub may listen wide.** Recommend that `atrium install --hub` binds the link on loopback unless
    `--link` is given, and refuses a wide `--link` without `--link-advertise`, so a fresh hub is never reachable by
    accident.
