# Review: live stop and start scripts, 487294d4 (m1mini, 2026-10-02): OK

sg4 branch claude/live-stop-start, one commit, sent as a pasted format-patch. A pause exception clint approved (he
ran these for the 10-01 reboot). It was rebuilt on scratch/live off claude/main 5f2bcd58 as 5ab7d5b6. Every file matches
the blob in the patch's index lines (README e5aabc13, live-common 4cc84dad, start-atrium-control cfa4d8b8,
start-atrium a0f4dbed, stop-atrium 5c90c436), so 5ab7d5b6 is 487294d4's content byte for byte, re-committed with the same
author date and message. The base blobs (live-common 537d0f14, start-atrium 9307721d) are claude/main's. Unsigned.

## Correctness: holds

- **Find-Control** matches the control room by its own binary directory and the ` room` subcommand, never by image
  name. So the hooks running `.atrium\bin\atrium.exe` and the claude-sg4 room are never touched, and Find-Atrium never
  matches ctl-bin. `room join <x>` matches too.
- **Stop-Control** is Stop-Room's shape: POST `/v1/shutdown` on 7791, wait 45 s, then force. stop-atrium.ps1 stops
  control, then the room, then the hub, so the rooms can still report while they wind down. It refuses inside an
  atrium card (`ATRIUM_TASK_ID`) unless `-Force`, and reports anything left.
- **start-atrium-control.ps1** does nothing when the room is already running. It copies the binary once, clears the
  supervised session's env (`Clear-SessionEnv`, the `ATRIUM_*` it names, and `CLAUDE_CODE_*` and `CLAUDECODE*`), and starts
  `--isolated` on its own dir, db and ports. The join string is masked in the logged command line. It waits up to
  30 s for health and shows room.err on a failure. start-atrium.ps1 calls it last, with `-NoControl` to skip it.
- **-WhatIf** is threaded through `Invoke-Step` (live-common.ps1:50-57). Every helper the new code calls exists.

## Lows

- **A refused shutdown is still forced.** With a share running, `/v1/shutdown` refuses ("a share is running, so
  loopback no longer means this machine", internal/daemon/shutdown.go:57-66). Stop-Control (and Stop-Room before it)
  logs the refusal, waits 45 s and force-stops, which is the kill the README's first rule forbids. Before a reboot
  that is no worse than the reboot. But the script says it winds down. Fix both together: on an HTTP refusal, say
  "refused: <reason>, stop the share first" and do not force unless `-Force`.
- **The README's last line names `docs/packaging.md`**, which does not exist. It is `docs/release/packaging.md`.
- **sg4-control's binary is copied once and never updated.** Nothing in the deploy scripts refreshes
  `.atrium\ctl-bin`, so sg4-control runs its first build until someone copies a new one by hand. Say how it gets a
  new build (README, "What runs on sg4"), or add it to `deploy-hub-only.ps1`'s swap.
- **The join string stays in the room's command line** for that run: `room join <join>` is visible to any local
  user in the process list (the 2026-09-30 audit's L2). It is single-use and spent at enrolment, so this is Low. The
  README could say to restart without `-Join` once joined.

## Public repo

Nothing new is sensitive. The paths (`C:\Users\claude\…`, `D:\git\…`), ports, room names and the LAN link
address (192.168.1.68, in live-common.ps1 before this commit) were already in the repo. The README adds the
sg4-control layout and a scheduled task's name. There are no credentials and no tokens, and the join string is a
placeholder.

Verdict: OK 5f2bcd58..5ab7d5b6 for 487294d4, hub-ok and room-ok (ops tooling).

Quality: careful ops scripts that keep the house rules: match by directory, stop by request, swap by rename. The
lows are inherited from Stop-Room or are documentation.
