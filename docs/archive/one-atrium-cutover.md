# The one-atrium cutover on this machine

Stage 4 of `docs/archive/one-atrium-plan.md`, written out for SG4 (clint's desktop, room `claude-sg4`). The hub, the room and
the hooks move from two binaries to one, `C:\Users\claude\.atrium\bin\atrium.exe`, in one window. The window ran on
2026-09-25.

**2026-09-26: the rollback is gone.** Every `atrium2*.exe` on the machine was deleted, including
`.atrium2\bin\atrium2.exe`, and `scripts/live/cutover.ps1` was deleted with it. This file is kept as the record of the
window. Start atrium with `scripts\live\start-atrium.ps1`, deployed as
`C:\Users\claude\.atrium2\scripts\start-atrium.ps1`.

`scripts/live/cutover.ps1` was the window as one script. This file says what it did, why each step is shaped the way
it is, and what to check after.

## Contents

- [What changes](#what-changes)
- [Before the window](#before-the-window)
- [The window](#the-window)
- [Verify, before anybody walks away](#verify-before-anybody-walks-away)
- [Rollback](#rollback)
- [After a day](#after-a-day)
- [The live scripts, line by line](#the-live-scripts-line-by-line)
- [Hazards this is shaped around](#hazards-this-is-shaped-around)

## What changes

| | Before | After |
| --- | --- | --- |
| Hub process | `.atrium2\bin\atrium2.exe hub --addr ... --dir C:\Users\claude\.atrium2\hub` | `.atrium\bin\atrium.exe run --no-room --addr ... --atrium-dir C:\Users\claude\.atrium2\hub` |
| Room process | `.atrium2\bin\atrium2.exe room --dir ... --db ... --http ... --agent ...` | `.atrium\bin\atrium.exe room`, the same flags |
| Hook binary | `.atrium\bin\atrium.exe`, an older build | the same path, the new build. `settings.json` is not touched |
| Room address file | `%LOCALAPPDATA%\atrium2\room\daemon.json`, naming `atrium2.exe` | `%LOCALAPPDATA%\atrium\daemon.json`, naming `atrium.exe` |
| Deploy scripts | `.atrium2\scripts\*.ps1`, matching `Name='atrium2.exe'` | `scripts\live\*.ps1` copied over them, matching directory plus subcommand |
| Hub state, room keys, database | `.atrium2\hub\`, `.atrium2\room\`, `.atrium\atrium.db` | unchanged, named by flags |

What stays: every port (7778 board, 7779 link, 7781 room board, 7777 agent listener), every `/_hub/` path, the
link protocol, `room.json`, the certificates, `hub.db`, `~/.atrium/mcp.json`, the dotfiles permission hook, and
`atrium-control.exe` (Open Question 8 is not part of this). sgg needs nothing: its room reconnects through the
unchanged link.

## Before the window

1. **Stage 3 is on `claude/main` and has gone out once as a shim build.** The orchestrator's `go build -o
   build.claude/ ./...` now writes both `atrium.exe` and `atrium2.exe`. The old `deploy-batch.ps1` deploys
   `build.claude\atrium2.exe`, which is the shim: it answers `atrium2 hub ...` and `atrium2 room ...` exactly as
   before, from the moved code. One batch deploy of it proves the move on the live machine before any path changes.
   Nothing about the scripts changes for that deploy.

   **Do not press the board's "install hooks" between that deploy and the cutover.** The shim room's address file
   names `atrium2.exe`, and the shim answers `hook`, so the lines it would write run, but they name the binary the
   cutover retires. `settings.json` should keep naming `.atrium\bin\atrium.exe`.

2. **Build.** In the orchestrator worktree, `go build -o build.claude/ ./...`. The cutover takes
   `D:\worktrees\claude\atrium\orchestrator\build.claude\atrium.exe`. It refuses a build that does not answer
   `run --no-room --help`, `room --help` and `hook --help`, which is how an old `atrium.exe` is told apart.

3. **Rehearse.** From any shell:

   ```powershell
   pwsh -NoProfile -File D:\worktrees\claude\atrium\orchestrator\scripts\live\cutover.ps1 -WhatIf
   ```

   It changes nothing and prints every step. The rehearsal from this branch printed these, with the two live pids,
   and nothing else. Stop if anything else appears, above all a third process:

   ```text
   ...\build.claude\atrium.exe is dev
   found atrium2 hub pid 4184 and room pid 31616
   hooks running from C:\Users\claude\.atrium\bin\atrium.exe right now: 0 (they keep their image through the rename)
   WHATIF: copy ...\build.claude\atrium.exe -> C:\Users\claude\.atrium\bin\atrium.next.exe (staged, nothing has it open)
   WHATIF: copy C:\Users\claude\.atrium\bin\atrium.exe -> C:\Users\claude\.atrium\bin\atrium.pre-cutover.exe (the rollback hook binary)
   left in place, untouched: C:\Users\claude\.atrium2\bin\atrium2.exe (the rollback atrium2)
   WHATIF: stop atrium2 room pid 31616: POST http://127.0.0.1:7781/v1/shutdown, wait up to 45s, then force
   WHATIF: copy C:\Users\claude\.atrium\atrium.db -> C:\Users\claude\.atrium\atrium.db.pre-cutover
   WHATIF: copy C:\Users\claude\.atrium\atrium.db-wal -> C:\Users\claude\.atrium\atrium.db.pre-cutover-wal
   WHATIF: copy C:\Users\claude\.atrium\atrium.db-shm -> C:\Users\claude\.atrium\atrium.db.pre-cutover-shm
   WHATIF: stop atrium2 hub pid 4184
   WHATIF: copy ...\build.claude\atrium.exe -> C:\Users\claude\.atrium\bin\atrium.next.exe (staged, nothing has it open)
   WHATIF: rename C:\Users\claude\.atrium\bin\atrium.exe -> C:\Users\claude\.atrium\bin\atrium.old-<ts>.exe
   WHATIF: rename C:\Users\claude\.atrium\bin\atrium.next.exe -> C:\Users\claude\.atrium\bin\atrium.exe
   WHATIF: keep C:\Users\claude\.atrium2\scripts as C:\Users\claude\.atrium2\scripts.pre-cutover, and copy ...\scripts\live\*.ps1 into C:\Users\claude\.atrium2\scripts
   WHATIF: keep C:\Users\claude\.atrium2\hub.err as hub.err.<timestamp>
   WHATIF: start hub: C:\Users\claude\.atrium\bin\atrium.exe run --no-room --addr 127.0.0.1:7778 --link 0.0.0.0:7779 --link-advertise 192.168.1.68:7779 --atrium-dir C:\Users\claude\.atrium2\hub
   WHATIF: keep C:\Users\claude\.atrium2\room.err as room.err.<timestamp>
   WHATIF: start room: C:\Users\claude\.atrium\bin\atrium.exe room --dir C:\Users\claude\.atrium2\room --db C:\Users\claude\.atrium\atrium.db --http 127.0.0.1:7781 --agent 127.0.0.1:7777
   cutover window done. now the checks in docs/archive/one-atrium-cutover.md, steps 12 to 15.
   ```

   The pids are the ones running when this was written. A build with a tag says its version instead of `dev`.

   `-WhatIf` on every other script in `scripts\live` should find no process to stop yet: they look for `atrium*.exe`
   in `.atrium\bin` running `run` or `room`, and before the cutover nothing does.

4. **Every peer idle**, per the usual batch-window rule. Stopping the room ends every supervised terminal, the
   orchestrator's included, and the room resumes them on start.

## The window

Start it detached, from a shell atrium does not supervise, or from the orchestrator with `Start-Process` so it
outlives the orchestrator's terminal:

```powershell
Start-Process pwsh -WindowStyle Hidden -ArgumentList '-NoProfile', '-File', `
  'D:\worktrees\claude\atrium\orchestrator\scripts\live\cutover.ps1'
```

Every line goes to `C:\Users\claude\.atrium2\deploy-batch.log`, tagged `CUTOVER`. What it does, in order, numbered
as the plan numbers them:

4. **Stage the build** as `.atrium\bin\atrium.next.exe`. Nothing has that name open, so a plain copy is safe.
5. **Keep the way back.** Copy today's `.atrium\bin\atrium.exe` to `atrium.pre-cutover.exe`.
   `.atrium2\bin\atrium2.exe` is left exactly where it is, as the rollback hub and room.
6. **Stop the room**: `POST http://127.0.0.1:7781/v1/shutdown`, wait up to 45 seconds for the pid to exit, and force
   only after that. The room parks its runners and saves what to reopen.
7. **Copy the database** `atrium.db`, `-wal` and `-shm` to `atrium.db.pre-cutover`, `-wal` and `-shm`. The room is
   down, so the files are quiet. A copy with the room running would not be safe.
8. **Stop the hub.** It holds nothing, so `Stop-Process` is fine.
9. **Swap the hook binary with two renames**: `atrium.exe` to `atrium.old-<ts>.exe`, then `atrium.next.exe` to
   `atrium.exe`, each retried up to 20 times at 400ms. See [the rename](#the-hook-binary-is-renamed-never-copied-over).
   If the second rename fails, the first is undone, because no file at the hook path is an outage for every hook.
   **Then swap the scripts**: `.atrium2\scripts` is copied to `.atrium2\scripts.pre-cutover` and `scripts\live\*.ps1`
   is copied over it. This is earlier than the plan's step 17, on purpose. See
   [the old scripts](#the-old-scripts-cannot-outlive-the-window).
10. **Start the atrium** with the environment of the supervised session that launched this cleared (see
    [ATRIUM_LOCATION](#an-inherited-atrium_location-would-keep-the-old-address-file)):

    ```text
    C:\Users\claude\.atrium\bin\atrium.exe run --no-room --addr 127.0.0.1:7778 --link 0.0.0.0:7779 --link-advertise 192.168.1.68:7779 --atrium-dir C:\Users\claude\.atrium2\hub
    ```

    Through `cmd /c`, appending to `.atrium2\hub.out` and `hub.err`, as `deploy-hub-only.ps1` does today. Waits up to
    25 seconds for `/_hub/health` to answer `ok`.
11. **Start the room**, the same flags it has today:

    ```text
    C:\Users\claude\.atrium\bin\atrium.exe room --dir C:\Users\claude\.atrium2\room --db C:\Users\claude\.atrium\atrium.db --http 127.0.0.1:7781 --agent 127.0.0.1:7777
    ```

    Waits up to 60 seconds for `/_hub/health` to report `rooms >= 1`. The room writes
    `%LOCALAPPDATA%\atrium\daemon.json` with `"exe": "C:/Users/claude/.atrium/bin/atrium.exe"`, and the script reads it
    back and logs a warning if it names anything else. Every session the room reopens inherits
    `ATRIUM_LOCATION=%LOCALAPPDATA%\atrium\daemon.json`.

If 9, 10 or 11 fails, the script runs the rollback itself and exits 1.

## Verify, before anybody walks away

12. `Invoke-RestMethod http://127.0.0.1:7778/_hub/health` shows the new board build. The board loads, and an open tab
    reloads itself on the new board hash.
13. A reopened session's tool call shows its activity badge, which is the `hook --event tool-start` path. A gated
    Bash call appears as a permission and is answered, which is the `/permission` path stage 1 moved.
14. `C:\Users\claude\.atrium\bin\atrium.exe version` prints the new commit. `atrium_status` over `/_hub/mcp` answers.
15. `%LOCALAPPDATA%\atrium\daemon.json` names `atrium.exe`, and the hooks panel on the board shows every hook as
    current. Before the cutover it could not: the room's file named `atrium2.exe`, and the board's install-hooks
    button would have written lines for it.

Also look once at `Get-CimInstance Win32_Process -Filter "Name='atrium2.exe'"`. It must be empty.

## Rollback

Any of 10 to 15 failing. From a shell atrium does not supervise:

```powershell
pwsh -NoProfile -File D:\worktrees\claude\atrium\orchestrator\scripts\live\cutover.ps1 -Rollback
```

The window also copied it to `C:\Users\claude\.atrium2\scripts\cutover.ps1`, which works the same. It does, in
order:

**Void, 2026-09-26.** `cutover.ps1` and `atrium2.exe` are both deleted, so step 16 cannot run. What it did:

16. Stops whatever of the new pair is running (`atrium*.exe` in `.atrium\bin` running `run` or `room`). Renames
    `.atrium\bin\atrium.exe` to `atrium.failed-<ts>.exe` and `atrium.pre-cutover.exe` back to `atrium.exe`. Puts
    `.atrium2\scripts.pre-cutover` back as `.atrium2\scripts`. Removes `%LOCALAPPDATA%\atrium\daemon.json` if the new
    room left it. Then starts today's exact pair, with `ATRIUM_LOCATION=%LOCALAPPDATA%\atrium2\room\daemon.json`:

    ```text
    C:\Users\claude\.atrium2\bin\atrium2.exe hub --addr 127.0.0.1:7778 --link 0.0.0.0:7779 --link-advertise 192.168.1.68:7779 --dir C:\Users\claude\.atrium2\hub
    C:\Users\claude\.atrium2\bin\atrium2.exe room --dir C:\Users\claude\.atrium2\room --db C:\Users\claude\.atrium\atrium.db --http 127.0.0.1:7781 --agent 127.0.0.1:7777
    ```

No stage adds a migration, so `atrium2.exe` opens the database the new binary used. If it does not, stop the room,
move `atrium.db*` aside, and copy `atrium.db.pre-cutover*` back to `atrium.db*` before starting it again.

## After a day

17. Retire the older scripts beside `.atrium2\scripts`, which nothing should run now: `start-atrium2.ps1`,
    `stop-atrium2.ps1`, `restart-room.ps1`, `cutover.ps1` and `deploy-orchestrator.ps1`. The last rewrites
    `~/.atrium/mcp.json` with a `/_hub/mcp` URL, which stays correct. Delete `.atrium2\scripts.pre-cutover`.
    **Still open, 2026-09-26:** the five scripts, `.atrium2\scripts\cutover.ps1` and `scripts.pre-cutover` are still
    on disk. None of them can start anything now that `atrium2.exe` is gone.
18. Open Question 8, when it is decided: re-register the user-scope `atrium-control` at the HTTP URL and delete
    `.atrium\bin\atrium-control.exe`. Not part of this cutover.
19. Delete `%LOCALAPPDATA%\atrium2\room\daemon.json`, `%APPDATA%\atrium2\` (the stale `sg4` test room),
    `.atrium2\bin\atrium2.exe.old-*` and `atrium2.revert-*.exe`, `.atrium\bin\atrium.pre-cutover.exe`, and
    `.atrium\atrium.db.pre-cutover*`. Keep `.atrium2\bin\atrium2.exe` one more week as the rollback binary.
20. Delete `cmd/atrium2`, `internal/cli/atrium2.go` and its test, and the second output in `Makefile`. That is the
    rest of stage 4, a code change of its own. **Done, 2026-09-26:** `make build` writes only `atrium.exe`.
21. Owed outside this repo, for clint: a line in `claude/tuning-changelog.md`, and the orchestrator's memories that
    name `atrium2` (`atrium2-is-the-live-hub`, `throwaway-atrium2-needs-atrium-location`, `atrium-binary-not-on-path`,
    `throwaway-hub-room-recipe`, and the deploy-script paths). In this repo, `docs/orchestrator/cold-start.md` and `docs/orchestrator/wrapup.md`
    name `atrium2.exe` and `.atrium2\scripts` paths.

## The live scripts, line by line

The new scripts are in `scripts/live/`, reviewed there and copied over `C:\Users\claude\.atrium2\scripts\` in the
window. They share `live-common.ps1`, which holds the paths, the two command lines, the process match and the swap,
so the rules below are written once. Every script takes `-WhatIf`, which prints every process it would stop and
every file it would copy or rename and changes nothing. `ATRIUM_NEW_BUILD` names a different build for a rehearsal.

The same three changes in every script:

**The binary.**

```powershell
# before
$bin = 'C:\Users\claude\.atrium2\bin\atrium2.exe'
$new = 'D:\worktrees\claude\atrium\orchestrator\build.claude\atrium2.exe'
# after (live-common.ps1)
$AtriumBin = 'C:\Users\claude\.atrium\bin\atrium.exe'
$AtriumNew = 'D:\worktrees\claude\atrium\orchestrator\build.claude\atrium.exe'
```

**The process match.** Image directory plus subcommand, never the image name:

```powershell
# before
$room = Get-CimInstance Win32_Process -Filter "Name='atrium2.exe'" |
  Where-Object { $_.CommandLine -match ' room ' } | Select-Object -First 1
$hub = Get-CimInstance Win32_Process -Filter "Name='atrium2.exe'" |
  Where-Object { $_.CommandLine -match ' hub ' } | Select-Object -First 1
# after (live-common.ps1, Find-Atrium)
Get-CimInstance Win32_Process | Where-Object {
  $_.ExecutablePath -and
  ((Split-Path $_.ExecutablePath) -eq 'C:\Users\claude\.atrium\bin') -and
  ((Split-Path $_.ExecutablePath -Leaf) -like 'atrium*.exe') -and
  ($_.CommandLine -match " $Sub( |$)")     # $Sub is 'run' for the hub, 'room' for the room
}
```

**The swap.** Stage, then two renames, never a copy onto the live name:

```powershell
# before
try { Copy-Item $new $bin -Force -ErrorAction Stop }
catch { Rename-Item $bin "$bin.old-$(Get-Date -Format yyyyMMddHHmmss)"; Copy-Item $new $bin -Force }
# after (live-common.ps1, Install-Atrium)
Copy-Item $AtriumNew 'C:\Users\claude\.atrium\bin\atrium.next.exe' -Force
Rename-Item 'C:\Users\claude\.atrium\bin\atrium.exe' "atrium.old-$(Get-Date -Format yyyyMMddHHmmss).exe"
Rename-Item 'C:\Users\claude\.atrium\bin\atrium.next.exe' 'atrium.exe'
```

Each rename is retried 20 times at 400ms, and a failed second rename puts the first back.

**The start lines**, per script:

| Script | Before | After |
| --- | --- | --- |
| `deploy-batch.ps1` | `atrium2.exe hub --addr 127.0.0.1:7778 --link 0.0.0.0:7779 --link-advertise 192.168.1.68:7779 --dir $hubDir` | `atrium.exe run --no-room --addr 127.0.0.1:7778 --link 0.0.0.0:7779 --link-advertise 192.168.1.68:7779 --atrium-dir $HubDir` |
| | `atrium2.exe room --dir $roomDir --db $db --http 127.0.0.1:7781 --agent 127.0.0.1:7777` | `atrium.exe room --dir $RoomDir --db $RoomDb --http 127.0.0.1:7781 --agent 127.0.0.1:7777` |
| `deploy-hub-only.ps1` | `cmd /c "atrium2.exe" hub ... --dir C:\Users\claude\.atrium2\hub >> hub.out 2>> hub.err` | `cmd /c "atrium.exe" run --no-room ... --atrium-dir C:\Users\claude\.atrium2\hub >> hub.out 2>> hub.err` |
| `start-atrium-hub.ps1` | the `hub` line above | the `run --no-room` line above |
| `start-atrium-room.ps1` | the `room` line above | the `room` line above, `atrium.exe` |
| `maintenance-window.ps1` | its own copy of the batch deploy, hub first | calls `deploy-batch.ps1`, which stops the room gracefully first |

Two behaviours are new, and both are in `live-common.ps1`:

- `Start-Room` refuses when a room already answers `http://127.0.0.1:7781/v1/health`, and every start clears the
  launching session's `ATRIUM_*` variables first.
- The hub is started through `cmd /c` with `>>` in every script, not only `deploy-hub-only.ps1`, so a deploy never
  overwrites the previous hub's log.

`scripts/hub-restart-gate.ps1` uses `/_hub/restart` and needs nothing. `scripts/sgg/restart-hub-with-board-share.ps1`
is rewritten the same way and refuses to run while an `atrium2.exe` hub is up.

## Hazards this is shaped around

### Every hook is an atrium.exe

After the cutover, `atrium.exe` is the image name of the hub, the room, and every hook process in flight, several a
second. A deploy script that stops `Name='atrium.exe'` stops hooks mid-run. That is why the match is the image
directory plus the subcommand. `atrium-control.exe` sits in the same directory and runs ` control`, which matches
neither.

The match takes any `atrium*.exe` in `.atrium\bin` rather than exactly `atrium.exe`, because a hub-only deploy
renames the file the room is running to `atrium.old-<ts>.exe`. What Windows reports as that process's image path
after the rename is not something to bet a second room on: a room the next deploy failed to find would get a
second room started beside it, on the same database.

### The hook binary is renamed, never copied over

Hooks spawn `.atrium\bin\atrium.exe` several times a second, and Windows refuses to overwrite or delete a file that
is running. It permits a rename within the same directory, and the running image keeps its handle. So the new build
is copied first to `atrium.next.exe`, which nobody has open, and then two renames swap it in. The gap between them
is microseconds. A hook that lands in it fails to start, and a failed atrium hook is a non-blocking error by design.
A copy onto the live name would widen that gap to the length of the copy, and fail outright while any hook ran.

### The old scripts cannot outlive the window

The plan replaced `.atrium2\scripts` a day after the cutover. That leaves a day in which the orchestrator's next
deploy runs the old `deploy-batch.ps1`: it finds no `atrium2.exe` processes, copies the orchestrator's `atrium2.exe`
shim into `.atrium2\bin`, and starts `atrium2 hub` and `atrium2 room` beside the running pair. The hub fails to bind
7778, and the room opens the live database before it fails to bind 7781. So the scripts are swapped in the same
window as the binary, the old ones are kept as `.atrium2\scripts.pre-cutover`, and the rollback puts them back.

### An inherited ATRIUM_LOCATION would keep the old address file

A room writes its address wherever `ATRIUM_LOCATION` names, and keeps an inherited value. The orchestrator is a
supervised session with `ATRIUM_LOCATION=%LOCALAPPDATA%\atrium2\room\daemon.json`, and a script it starts inherits
that. Left alone, the new room would keep writing the old file, every reopened session would be told the old file,
and step 15 would read a file nobody writes. Every start in `scripts\live` clears `ATRIUM_LOCATION`,
`ATRIUM_SHARED_LOCATION`, `ATRIUM_AGENT_NAME`, `ATRIUM_TASK_ID`, `ATRIUM_ROOM`, `ATRIUM_RUNNER` and
`ATRIUM_HOOK_EXE` first. The old scripts did not, which is how today's room and hub carry the orchestrator's agent
name in their environment.

### sgg

sgg runs `C:\Users\localai\.atrium2\bin\atrium2.exe room` with every flag spelled out, and reconnects through the
unchanged link. Its next upgrade, if it takes one, is whatever this hub is running: the shim before the cutover and
the one binary after, written over its own `atrium2.exe`. Both answer `room` with the same flags and the same link
protocol. After it runs the one binary, sgg's room records itself in `%LOCALAPPDATA%\atrium\daemon.json` there,
which is the only room on that machine. `scripts\sgg\stage-sgg.ps1` now stages `build.claude\atrium.exe` under
that same name.
