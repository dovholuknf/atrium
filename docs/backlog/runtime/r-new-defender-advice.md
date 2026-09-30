# r-new-defender-advice: notice Defender eating a Windows room, and say the fix

Status: parked (clint, 2026-09-30). Runtime detects, @ui shows it. Design review by @rnd before a build.

## Why

On sg4 on 2026-09-30, `MsMpEng.exe` used 103% CPU while directors built and tested, more than any agent. The fix
is a set of Defender exclusions, now in `docs/user-guide.md` Pattern 13. clint asked whether atrium can see this
and recommend the fix to other people. It can see the symptom, and it can write the command with the right paths.

## What atrium can and cannot see

- CAN: `MsMpEng.exe` CPU, the same way the room already samples process CPU for the rooms dashboard (r-010).
- CAN: the runner user's own paths. The daemon runs as the agents' user, so `go env GOCACHE GOMODCACHE`, the
  worktree roots and the build directory are that user's. This avoids the trap clint hit, where an admin shell
  opened as himself expanded `$env:LOCALAPPDATA` to his own profile instead of the agents'.
- CANNOT: whether exclusions are set. `Get-MpPreference` answers "Must be an administrator to view exclusions"
  without elevation. So the signal is the symptom, never the configuration.
- MUST NOT: apply it. It needs elevation and changes a security setting. Atrium writes the command, a human runs it.

## Wanted

- Room side: on Windows, sample `MsMpEng.exe` CPU. When it stays above a threshold (say 50% of one core for 2
  minutes) while any card on the room has a tool running, raise an advisory on the room. Clear it when the CPU
  stays under for 10 minutes. Nothing stored beyond the room's existing stats.
- The advisory carries the ready-made command with this room's actual paths, one line per path, and a link to
  Pattern 13.
- Board: a dismissible notice on the rooms dashboard and the room's settings, not a growler. "Dismiss for this
  room" is remembered, since a machine whose admin has decided not to exclude should not be nagged.
- Hub: the same advisory for every attached Windows room, so one board says it for all of them.

## Open for the design

- The threshold, and whether to also count `SmartScreen` and `SearchIndexer`, which show the same pattern less often.
- Whether the advisory should include the process exclusions (`go.exe`, `chrome-headless-shell.exe`), which are
  broader than path exclusions.
