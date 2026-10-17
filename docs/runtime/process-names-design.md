# Each atrium process named for its role: atrium-hub, atrium-room, atrium-pty

Backlog item `f-process-names`. Status: designed, not built. This replaces the build plan in
`docs/rnd/process-names-design.md` (2026-10-14). That file's spike results (`docs/rnd/process-names-spike.md`) still
stand and are relied on here: a file's own name is what Task Manager, `Get-Process` and the macOS kernel name show, and
a symlink does not get its own name on macOS.

## Decided before this design (the item)

One binary and one build. The three long-running roles run from files beside it named for the role:
`atrium-hub[.exe]` for the hub, `atrium-room[.exe]` for a room, `atrium-pty[.exe]` for the pty host, which replaces
today's `atrium.ptyhost[.exe]`. `atrium` stays the CLI, the hook target and the file every deploy swaps. Phase 1 is the
pty host, phase 2 the hub and the room.

## Decisions

### 0. Copies, not hard links

The 2026-10-14 design chose hard links. A hard link shares its file with `atrium.exe`, and the deploy breaks on that.
Every Windows swap is `Move-Item -Force atrium.exe atrium.prev.exe`, which deletes the previous `atrium.prev.exe`. With
links, that file can be the very image a long-lived pty host runs, or the old hub during a start-before-stop restart.
Windows refuses to delete a running image under any of its names, so the swap fails. A copy never shares a file with
`atrium.exe`. The cost is one binary's size per role on disk, and a copy plus a hash on the first start after a deploy.

One package, `internal/rolebin`, owns it:

- `Installed() string` is the canonical binary: `os.Executable()` resolved, with a role name mapped back to
  `atrium[.exe]` in the same directory when that file exists. It answers "what does the next start run, what do hooks
  run, what does a deploy swap".
- `Running() string` is this process's own image. Only `link.Offered` needs it: the hub offers rooms the bytes it runs.
- `Ensure(role) (path string)` returns the role file, current, or `Installed()` when anything fails. Naming is never
  a reason a start fails, the hook posture applied to starting. A failure is logged once.

### 1. The name in the foreground

- **macOS and Linux: the process execs itself into the copy.** `atrium run` and `atrium room` call `Ensure` first
  and `syscall.Exec` the role file with the SAME argv. An exec keeps the pid, so systemd, launchd, `nohup`, the
  location file and `atrium stop` see one process throughout. The kernel name (`comm`, `ucomm`, Activity Monitor)
  becomes the role. argv is unchanged, so `ps -eo args` still reads `.../atrium room ...` and every script's
  `pgrep -f "$Bin room"` and `(^|/)atrium\s+run` keeps matching. Skipped when already running as the role file, or
  when `ATRIUM_ROLEBIN=off`. An exec that fails carries on as `atrium`.
- **Windows: only the starts atrium or the scripts make use the copy.** Windows has no exec, and a re-exec there is a
  second process with a second pid, which the 10-14 design rejected for good reason (option E). The starts are
  `startRoom` (`room --detach`, the hub's `ensureRoom`, the logon task, which already runs `room --detach`) and
  `spawnRoomRestart`. The scripted ones are `Start-Hub` and `Start-Room` in `scripts/live/live-common.ps1`. They run
  what a hidden `atrium rolebin <hub|room|pty>` prints, which is the `Ensure` path and exits 0 even on its fallback.
  A hub typed by hand into a console stays `atrium.exe`. That console names it already.

### 2. When a copy is refreshed

At every start, by `Ensure`. A copy is current when it has the same size and the same SHA-256 as `Installed()`. The
size test is free and decides most cases. The hash is about 0.2 s on a 70 MB binary, and starts are rare. Size and
mtime alone, today's `copyForHost` rule, cannot tell two builds of one size apart.

A stale copy is replaced by writing `<role>.new` and renaming:

- **Hub and room: rename aside, then in.** The old file is renamed to `<role>.old[.exe]`, and `.new` takes its name.
  Windows allows renaming a running image, so a hub started before the old one stops (`Start-Hub` does this) still
  gets the new build. Leftover `<role>.old*` files are deleted at the next `Ensure`. One that is still running fails
  the delete and is left.
- **The pty host: never moved while one runs.** `ptyhost.Start` is only called after a probe found no host for its
  state dir, as today. It renames `.new` over `atrium-pty` and nothing else. If a running host holds the file on Windows
  (a host for another state dir), the rename fails and the existing copy is used as it is: today's rule, and the
  version check in 4 decides whether that host may serve. On posix the rename replaces the name only, and a running
  host keeps its own image.

### 3. Everything that finds atrium by name or path

| Where | Today | Change |
| --- | --- | --- |
| `internal/claudeconf/whichexe.go` `HookExe` | reads the daemon's `Exe` from the location file | none. The file must hold `atrium`, see the next row |
| `internal/daemon/whereami.go` `daemonBinary` | `os.Executable()` | `rolebin.Installed()`. A room running as `atrium-room` would otherwise write `atrium-room` into every runner's hooks, and a held role image blocks its own refresh. MUST land with phase 2 |
| every other `os.Executable()` (`api/hooks.go`, `cli/upgrade.go`, `link/upgrade.go` swap, `deployready.go`, `roomrestart.go`, `atrium_runroom.go`, `roomhold.go`, `newcontext.go`, `shepherd.go`, `sshconfig.go`, `sshproxy.go`, `ptyhost/start.go`) | own image | `rolebin.Installed()`. `link.Offered` takes `rolebin.Running()`. A Go check fails on any `os.Executable()` outside `internal/rolebin` |
| `cli/control.go` `daemonExe` | sibling `atrium` | none |
| `scripts/live/live-common.ps1` `Find-Atrium` | directory plus subcommand, leaf `atrium*.exe` | none. It already matches `atrium-hub.exe` and the rest |
| `scripts/live/refuse-supervised.ps1` | command line `atrium[^\\/"\s]*(\.exe)?"?\s+(room\|daemon)` | none. It matches `atrium-room.exe` |
| `Find-Hub` in `scripts/provision-room.ps1` and `scripts/room-check.ps1` | Windows: `Name='atrium.exe'` | `Name LIKE 'atrium%.exe'` plus the command line, as `Find-Atrium` does. posix: none, argv is unchanged |
| `scripts/sgg/restart-hub-with-board-share.ps1` | `Name='atrium.exe'` and `ExecutablePath -eq $Bin` | the directory of `$Bin` and ` run `, the `Find-Atrium` rule |
| `atrium-deploy.ps1` swaps, `atrium upgrade` | rename `atrium.exe` aside, rename the new one in | none. They swap `atrium` and the copies follow at the next start |
| `atrium stop`, the reaper | the location file's address, runner pids | none |
| mac launchd plist, systemd unit (`atrium-service.sh`) | `$Bin room ...` | none. The exec in 1 names it |
| `rooms.js` ssh config, `sshcfg` ProxyCommand | `atrium` | none. Written from `Installed()` |

### 4. Room and pty host versions

Unchanged. The room probes before it starts a host, the protocol version (`Proto`) and the build string are checked as
they are now, and a host already running from `atrium.ptyhost.exe` stays in use. Phase 1 changes only the file a NEW
host starts from. `ptyhost.Start` deletes `atrium.ptyhost[.exe]` when nothing holds it, so the old name is gone after
the last old host exits.

### 5. How a deploy proves the new code runs

The swap updates `atrium` alone, and each role's copy is refreshed by its own next start. The proof is the commit
each RUNNING process reports, which the copy cannot fake and a stale copy cannot pass:

- A room: its hello carries its commit, and `Test-RoomBackOnHub` already checks it against the commit just built.
- The hub: it knows its own running commit (`SetRunningCommit`). `Start-Hub`'s check reads it from the hub and
  compares, as rooms are checked. That is new in phase 2.
- The pty host: its probe reports its build, as today.

Then one line per process in the deploy log, for a person to read: role, pid, image name, commit. A wrong image name
with the right commit is a naming bug and never fails a deploy. A wrong commit fails it, as today.

## Build

**Phase 1, the pty host.** `internal/rolebin` with `Installed`, `Running` and `Ensure`. `ptyhost.Start` uses
`Ensure("pty")` in place of `copyForHost`, from `Installed()`, and deletes `atrium.ptyhost[.exe]` when it can. Tests:
a stale copy of the same size is replaced (the hash rule), a held copy is used as it is, a failure returns
`Installed()`, `Installed()` maps every role name back, and the existing `TestStartRunsADetachedHostFromACopy` runs
from `atrium-pty`. Update `internal/ptyhost/AGENTS.md`, `docs/terminal/ptyhost-protocol.md` and the root `AGENTS.md`
"What runs" table.

**Phase 2, the hub and the room.** `daemonBinary` and the `os.Executable()` sweep, with the Go check, first and in
the same branch. Then `startRoom` and `spawnRoomRestart` on `Ensure("room")`, the posix exec in `run` and `room`, the
hidden `atrium rolebin`, `Start-Hub` and `Start-Room` on it, the three script rows in 3, the hub's commit check, and
the deploy log line. Before phase 2 deploys, check the inbound rule that lets sgg reach `:7779` on the hub's machine.
If it is a program rule for `atrium.exe` and not a port rule, `atrium-hub.exe` needs its own rule. The spike could not
read the rules without elevation, so this needs an elevated shell, which is clint's to run.
