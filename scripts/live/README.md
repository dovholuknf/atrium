# scripts/live: running atrium on sg4

These are sg4's own scripts for starting, stopping and deploying atrium. They live in the repo so they are reviewed
and versioned, and they RUN from `C:\Users\claude\.atrium2\scripts\`. After a change here lands on `claude/main`,
copy them over with:

```powershell
pwsh -File D:\git\github\dovholuknf\atrium\scripts\live\install-live-scripts.ps1
```

## What runs on sg4

| Process | Binary | Ports | Directory |
| --- | --- | --- | --- |
| the hub (`run --no-room`) | `C:\Users\claude\.atrium\bin\atrium.exe` | board 7778, link 7779 | `.atrium2\hub` |
| the claude-sg4 room | the same binary | board 7781, agents 7777 | `.atrium2\room` |
| the sg4-control room (`--isolated`) | `C:\Users\claude\.atrium\ctl-bin\atrium.exe` | board 7791, agents 7787 | `.atrium2\ctl` |

The board you open is the hub's, http://127.0.0.1:7778. It shows every room, including the ones on m1mini and sg3,
which run on their own machines and are not started or stopped by anything here.

sg4-control has its own binary so that a claude-sg4 deploy, which matches processes by the `.atrium\bin` directory,
never stops it. That binary, `.atrium\ctl-bin`, is pinned on purpose and updated by hand, so deploys do not refresh it.

## Rebooting Windows

Nothing starts atrium at boot. Run every command below from a plain PowerShell window, not from a terminal inside
atrium: a room stopping takes the scripts running in its terminals with it.

Before the reboot:

```powershell
pwsh -File C:\Users\claude\.atrium2\scripts\stop-atrium.ps1
```

It asks sg4-control, then claude-sg4, to wind down (runners are parked and what to reopen is saved), forces each one
only after 45 seconds, and then stops the hub. Rooms on other machines lose the hub until it is back. Their runners
wait on the backoff they already have and reattach by themselves.

After you log in again:

```powershell
pwsh -File C:\Users\claude\.atrium2\scripts\start-atrium.ps1
```

It starts the hub, then claude-sg4, then sg4-control, and leaves alone any of them that is already running. Then open
http://127.0.0.1:7778 and resume the cards you want back. The orchestrator is on sg4-control.

Both scripts take `-WhatIf`, which says what they would do and does nothing.

## The scripts

| Script | What it does |
| --- | --- |
| `start-atrium.ps1` | Hub, claude-sg4, sg4-control, in that order. `-NoControl` skips sg4-control. |
| `stop-atrium.ps1` | sg4-control, claude-sg4, hub, in that order. `-KeepControl` leaves sg4-control up. |
| `start-atrium-hub.ps1` | The hub alone. |
| `start-atrium-room.ps1` | The claude-sg4 room alone, when the hub is up and the room is not. |
| `start-atrium-control.ps1` | The sg4-control room alone. `-Join '<join string>'` on its first run on a machine. |
| `build-deploy.ps1` | Build from a clean `claude/main` with the commit stamped in. |
| `deploy-hub-only.ps1` | Swap the binary and restart the hub. Rooms are not touched. |
| `deploy-ready.ps1`, `deploy-batch.ps1` | Restart the room on a new build when the board is idle. |
| `maintenance-window.ps1` | Hold the room restart for a window. |
| `install-live-scripts.ps1` | Copy this directory over the live copies. |
| `live-common.ps1` | Shared paths and functions. Dot-sourced by the others, never run alone. |

## Rules every script here keeps

- **A kill is not a stop.** A room owns a pseudo terminal per runner, and killing it ends every runner at once. Stop a
  room by `POST /v1/shutdown`, as `stop-atrium.ps1` does, or with `atrium stop`.
- **Match a process by its directory and subcommand, never by image name.** `atrium.exe` in `.atrium\bin` is also the
  binary every Claude Code hook runs, several times a second. Stopping by name stops the hooks in flight.
- **Swap a binary by two renames, never a copy over the live name.** `Install-Atrium` in `live-common.ps1`.

## Known leftovers

- The scheduled task `atrium2-watchdog` points at `C:\Users\claude\.atrium2\start-atrium2.ps1`, which no longer
  exists. It starts nothing. Boot autostart is shelved, see `docs/release/packaging.md` before adding one.
