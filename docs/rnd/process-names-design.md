# Telling the hub, the room and the pty host apart in Task Manager, ps and Activity Monitor

Status: superseded 2026-10-17 by `docs/runtime/process-names-design.md`, which uses copies instead of hard links and
dash names. The findings below still stand. Original status: design, 2026-10-14. Nothing here is built. Written by @rnd for backlog `r-ptyhost-in-arch-and-process-names`,
part B only. Part A (the docs that never mention the pty host) is another worker's and is not touched here. For
clint's review before anything is built.

## The problem

clint, 2026-10-14 16:23, with Task Manager open: "hard to know what is what. is there some way to make them show up
differently?" He does NOT want another executable. The backlog item names the candidates: hard links of the one binary
(`atrium-hub.exe`, `atrium-room.exe`, `atrium-ptyhost.exe`) that the deploy recreates on every swap, argv[0] or a
process title on posix, and a version-resource description. Today's workaround from the item: Task Manager, Details,
right-click the header, Select columns, Command line.

The goal is one rule: a person looking at the process list can name each long-running atrium process by its image
name alone, on Windows, macOS and Linux, with the one binary still the only thing shipped.

## What exists today

- **One binary, three long-running roles.** `atrium run` is the hub, `atrium room` is a room, `atrium ptyhost` is the
  host that keeps terminals alive across a room restart (`AGENTS.md`, "What runs", and `internal/cli/ptyhost.go`).
  Every hook line in a runner's `settings.json` runs the same `atrium.exe` several times a second, so the process list
  also fills with short-lived `atrium.exe` rows (`scripts/live/live-common.ps1`, header).
- **The pty host is ALREADY a copy under its own name.** `ptyhost.Start` runs the host from `atrium.ptyhost[.exe]`
  beside the installed binary (`internal/ptyhost/start.go`, `copyForHost`). The reason is not the process list. It is
  the swap: the copy keeps the rename-aside of a new `atrium.exe` from touching the image a long-lived host holds open.
  A copy that is already there and current (same size, not older) is left alone, and a copy that cannot be replaced
  because a running host holds it is used as it is. `C:\Users\claude\.atrium\bin\atrium.ptyhost.exe` exists on this
  machine. So with `pty_host` on, the host already shows as `atrium.ptyhost.exe` in Task Manager. The identical rows
  clint saw are the hub (`atrium run --no-room`) and the room (`atrium room`), and the hook processes.
- **The room is spawned by atrium itself, the hub is not.** `startRoom` runs `os.Executable()` detached
  (`internal/cli/atrium_runroom.go:180`), and `spawnRoomRestart` does the same for a hub-triggered restart
  (`internal/cli/roomrestart.go:169`). Both go through `internal/detach`. The hub is started by scripts:
  `scripts/live/live-common.ps1` builds `$HubArgs` and its start function runs `"$AtriumBin"` with them (line 490,
  reached from `start-atrium-hub.ps1`), and the autostart paths (`scripts/atrium-autostart.ps1`,
  `scripts/atrium-service.ps1`, `scripts/atrium-service.sh`, whose systemd `ExecStart` takes the binary it found) run
  `atrium room` directly.
- **The scripts already tolerate other image names.** `Find-Atrium` matches by image DIRECTORY and subcommand, and the
  image leaf with `-like 'atrium*.exe'`, so `atrium.ptyhost.exe` and any `atrium.room.exe` match
  (`scripts/live/live-common.ps1:152`). Rule 1 in that file's header says a process is never matched by image name
  alone, because that would stop every hook in flight.
- **The deploy swaps by rename.** Local: stage `atrium.next.exe`, rename `atrium.exe` aside, rename the new one in
  (`live-common.ps1`, the two-rename swap). Remote Windows rooms: `Move-Item atrium.exe atrium.prev.exe`, then
  `Move-Item atrium.new.exe atrium.exe`, then `atrium room --detach` (`scripts/live/atrium-deploy.ps1`, `$winSwap`,
  `$sg3Detach`). m1mini: `mv -f atrium atrium.prev && mv -f atrium.new atrium` (`$macDetach`). In every case the room
  is stopped BEFORE the swap and started AFTER it, and `room --detach` goes through `startRoom`.
- **The location file records the daemon's own path.** `daemonBinary()` is `os.Executable()` with symlinks resolved
  (`internal/daemon/whereami.go:365`), and `claudeconf.HookExe` reads it back out of the location file to decide
  which binary goes into every runner's hook config (`internal/claudeconf/whichexe.go`, whose header says not to use
  `os.Executable()` anywhere else).
- **There is no version resource, no process title and no argv[0] handling anywhere** (a search of `internal`, `cmd`
  and the `Makefile` finds none).

## What each OS shows (these are OS facts, not from the repo, and the spike below checks them)

- **Windows Task Manager, Details tab.** The Name column is the image file name. Nothing else about the process is shown
  by default. Command line is a hidden column.
- **Windows Task Manager, Processes tab.** For a process with a version resource the Name column shows the resource's
  FileDescription, and without one it shows the file name. This matters for candidate C below.
- **Linux `ps` and `top`.** `ps -o comm` and `top` show `/proc/<pid>/comm`, which the kernel sets from the basename of
  the path passed to `execve`, 15 characters at most. `ps aux` and `ps -ef` show the full command line, whose first word
  is argv[0].
- **macOS Activity Monitor.** Shows the process name, which comes from the executable's file name. Whether a hard link
  and a symlink each give their own link name there needs one run on m1mini.

## Options

### A. Role-named hard links of the one binary, made by atrium itself when it spawns a role

The binary keeps a file named for each role beside itself, `atrium.room.exe` and `atrium.hub.exe` (the pattern the
existing `atrium.ptyhost.exe` already follows), and runs the role from it. It is a hard link where the filesystem
allows one (NTFS, APFS, ext4 all do, on the same volume) and a copy where it does not. It is made at SPAWN time, not by
the deploy.

- **Why not the deploy.** The backlog item proposes the deploy recreating links on every swap. That puts the rule in
  five places that each swap differently (the local two-rename swap, `$winSwap`, `$sg3Detach`, the mac swap, and
  `atrium upgrade`), and a missed one leaves a link pointing at the OLD inode. A hard link is a second name for one
  file. When `atrium.exe` is replaced by a rename, the old file lives on under the link's name and the link runs the
  previous build. Recreating it at spawn time, where atrium compares file identity with `os.SameFile` against the
  binary it is about to run, makes a stale link impossible whichever script did the swap.
- **Cost.** A hard link takes no disk. A copy takes one binary's size. Both are ignored on failure: the spawn falls
  back to the plain `os.Executable()`, so naming is never a reason a room fails to start (the hook posture, `AGENTS.md`
  guarantee 2, applied to starting).
- **The room.** `startRoom` and `spawnRoomRestart` call the helper and run the returned path. That covers
  `room --detach`, `ensureRoom` (the hub starting this machine's room) and a hub-triggered restart, which is every
  self-spawn.
- **The hub and the autostarts.** Nothing atrium runs spawns the hub, and the autostart tasks run `atrium room` as a
  scheduled task, a systemd unit or a launch agent. A process cannot rename its own image, so these call a hidden
  `atrium rolebin <hub|room>` that makes the link and PRINTS its path, and the scripts run what it printed. It is a
  small edit to `live-common.ps1`, `atrium-autostart.ps1`, `atrium-service.ps1` and `atrium-service.sh`.
- **The pty host stays as it is.** It is already a role-named copy, kept as a COPY on purpose (its whole point is to
  outlive the rename of `atrium.exe`, and `copyForHost` already handles a running host holding its copy). The helper
  can later replace `copyForHost`, and renaming it to match is not worth a compatibility break.
- **Hook processes stay `atrium.exe`**, which is correct: the short-lived rows are the hook, and the standing rows are
  named for their roles.

Trade-offs: a first spawn after an upgrade does a file operation (a link is microseconds, a copy is a few hundred
milliseconds, once per upgrade). Three more image names exist on disk. See Risks for the three things this can break.

### B. A copy per role, without links

The same as A with a copy always and never a link. It is what the pty host does today, so it is proven here. It costs
one binary of disk per role (three times the size on a machine that runs all three) and a copy of a running image on
every upgrade, and it gains nothing over a link, since both are an independent file once the swap happens. Kept as
A's fallback only.

### C. A version resource (FileDescription) in the Windows binary

Linking `goversioninfo` output (a `.syso`) gives every build one `FileDescription`. It is static per file, so all three
roles read the same, which is the problem restated. It also makes things WORSE in one place: the Processes tab would
show the description in place of the file name, so a role-named file would be hidden there behind "Atrium" three times.
Doing it per role means patching the resource into each copy after copying, which is real code and breaks if the
binary is ever signed. Rejected. A description is a reasonable later addition for the "Publisher and product" look of
the Details dialog, and has no part in this fix.

### D. argv[0] and a process title on posix

Setting `exec.Cmd.Args[0]` to `atrium-room` makes `ps aux` show it, and costs nothing. It does not change `comm`, so
`top`, `ps -o comm` and Activity Monitor still show `atrium`, and Windows ignores it (Task Manager shows the image).
`prctl(PR_SET_NAME)` through `golang.org/x/sys/unix`, which the repo already imports, sets `comm` on Linux only, from
the main thread, and macOS has no equivalent that is not a hack on argv memory. A partial answer on one OS. Kept as an
optional extra for Linux (section 4 of the build plan), never as the fix.

### E. Re-exec under the role name

`atrium room` finds it is not running as `atrium.room.exe`, makes it and re-executes. On Windows a re-exec is a new
process with a new pid and the first one has to wait or exit, so `startRoom`'s pid, a scheduled task's idea of its
process, `atrium stop` and `Find-Atrium` all see the wrong one for a moment. Rejected: it is complexity in the one place
(process identity) that the live scripts are most careful about.

## Recommendation

**A.** Role-named hard links of the one binary, made at spawn time by atrium (a copy when a link is refused), run by
`startRoom`, `spawnRoomRestart` and a hidden `atrium rolebin` that the hub's and the autostarts' start scripts call.
Names follow the pty host's, `atrium.hub.exe`, `atrium.room.exe` and the existing `atrium.ptyhost.exe`, so the scripts'
`atrium*.exe` match already covers them. No second executable is shipped, built or versioned: it is the same bytes under
three names.

Task Manager then reads `atrium.exe` (a hook, briefly), `atrium.hub.exe`, `atrium.room.exe`, `atrium.ptyhost.exe`.

## Build plan

Each step lands alone and nothing breaks if a later one never does.

1. **The helper, `internal/rolebin`.** One function, `Ensure(exe, role string) (path string, err error)`.
   - `role` is `hub`, `room` or `ptyhost`, and the file is `<dir of exe>/atrium.<role><ext of exe>`.
   - If the file exists and `os.SameFile` says it is `exe`, return it. If it exists and is not, rename it to
     `<name>.old` (a running image can be renamed on Windows, and a held `.old` that cannot be removed is left), then
     link. `os.Link`, and on error copy through a temp file and a rename as `copyForHost` does.
   - Any error returns `exe` itself with the error. It never fails the caller.
   - Tests in `internal/rolebin/rolebin_integration_test.go`: makes a link, is idempotent, replaces a stale link after
     `exe` is replaced by a rename (the swap), falls back to a copy when a link is refused (a test seam on the link
     function), returns `exe` when the directory is read only, and leaves a held target alone.
2. **The room's self-spawns.** `startRoom` (`internal/cli/atrium_runroom.go:180`) and `spawnRoomRestart`
   (`internal/cli/roomrestart.go:169`) run `rolebin.Ensure(self, "room")`. A test pins that the spawn path is the
   returned one and that an error still spawns `self`.
3. **Keep the location file and the hook binary canonical.** `daemonBinary()` (`internal/daemon/whereami.go:365`)
   maps `atrium.<role><ext>` back to `atrium<ext>` before it records the path, because `claudeconf.HookExe` writes
   that path into runners' hook config, and a hook that ran `atrium.room.exe` would hold the room's image open and
   block every swap. Test: a daemon running as `atrium.room.exe` records `atrium.exe`. This step MUST land with step 2.
4. **The hub and the autostarts.** Add the hidden `atrium rolebin <hub|room>` (prints the path, exits 0 even when it
   fell back, so a script always has something to run). Switch `$HubArgs`' launcher in `live-common.ps1` and the room
   start in `atrium-autostart.ps1`, `atrium-service.ps1` and `atrium-service.sh` to run what it printed. Scripts keep
   matching with `Find-Atrium`'s existing `atrium*.exe`.
5. **Old links.** `atrium upgrade`'s and the deploy's `atrium.old-*.exe` cleanup learns `atrium.<role>.old` so they do not
   pile up. Nothing else in the deploy changes.
6. **Linux extra, optional.** `prctl(PR_SET_NAME, "atrium-room")` in the room and hub entry points, behind a
   `//go:build linux` file. Skip it if the spike shows `comm` already follows the link name.
7. **The docs.** The spawn names go into the "What runs" table that part A edits (one column, "shows up as"), and one
   paragraph in `docs/fabric/hub-and-rooms.md`. A changelog and a `docs/changes/` note, as every change ships.

The spike before step 1, on three machines, ten minutes: make a hard link of `atrium` in a scratch folder, run
`<link> version`-like long command (`atrium room` on spare ports, never a real room's), and read the process list.
Windows: Details and Processes tabs on sg4 or claudevm. macOS: Activity Monitor and `ps -o comm` on m1mini, with a hard
link and a symlink. Linux: `ps -o comm` in sg4-wsl.

## Risks

- **The Windows firewall prompts per program path.** The hub listens on `0.0.0.0:7779` (the room link). A first run of
  `atrium.hub.exe` is a new program to the firewall, and an existing allow rule for `atrium.exe` does not cover it. A
  rule written for the path has to be added once per machine, or the link silently does not accept. This is the biggest
  real risk. The spike must run the hub from a link on sg4 and watch for it. The room's listeners are loopback and are
  not affected.
- **The hook binary.** Step 3 above. If a runner's hook config ever names `atrium.room.exe`, every swap fails on a held
  image. The test and the canonicalisation exist for this alone.
- **A stale link.** Prevented by comparing identity at spawn time. It is why the deploy is not the place to make them.
- **A script that matches `atrium.exe` by name.** `live-common.ps1` header rule 1 says none does, and `Find-Atrium` and
  `Find-Control` match by directory and subcommand. `Stop-Room` and `Stop-Hub` therefore still find the new names. Check
  the rest of `scripts/` with a search for `atrium.exe` before step 4 lands.
- **Security tools that allow by path or hash.** A hard link has the same hash and a different path. A rule by path
  needs the new names.
- **The pty host copy is not a link.** Unchanged, and deliberate.

## Open questions for clint

1. The Windows firewall: are you happy to add one inbound allow rule for `atrium.hub.exe` on 7779 per machine that runs
   a hub (today only sg4)? If not, the hub could stay `atrium.exe` and only the room and host get names.
2. Names: `atrium.room.exe` (matches the existing `atrium.ptyhost.exe` and `atrium*.exe` matches) or
   `atrium-room.exe` (reads better and sorts apart from `atrium.exe`)? This design uses the dot form.
3. May the macOS spike run on m1mini (a spare-port room from a link, no real room touched)?
4. Is a description or icon in the Windows binary wanted for its own sake, knowing it does not help here?
5. With `pty_host` on, `atrium.ptyhost.exe` already exists. Were the three identical rows you saw a room with
   `pty_host` off? If it was on, something other than the host is the third row, and a hook is the likely one.

## Decisions (orchestrator, 2026-10-14)

- Option A as written. All three roles get names, in the dot form: `atrium.hub.exe`, `atrium.room.exe`, `atrium.ptyhost.exe`.
- The firewall risk stands. The live hub runs `--link 0.0.0.0:7779 --transport direct,ziti`, and sgg attaches over direct
  TCP, so a new program path can meet an inbound firewall prompt. The spike runs the hub from a link on sg4 and checks
  whether the firewall prompts. Open question 1 stays open until it has.
- The m1mini spike is allowed, on spare ports only. No real room is touched.
- No version resource (open question 4 is no).
- The spike answers open question 5: whether the three identical rows were a room with `pty_host` off.
- Nothing is built yet.
