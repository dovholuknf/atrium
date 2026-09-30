# Defender advice (r-new-defender-advice)

Status: designed by @rnd 2026-09-30, for @runtime (detector) and @ui (notice). Nothing here is built. Backlog item
`docs/backlog/runtime/r-new-defender-advice.md`.

## The answer

Build it, with one correction to the item. The room CANNOT read `MsMpEng.exe`'s CPU the way it reads its own.
Proven on sg4 as `claude`, not elevated: `Get-Process MsMpEng` returns the process with an empty `CPU` and
`TotalProcessorTime`, because opening a SYSTEM service's process for its times is refused to a non-admin. What does
work is the system's process list, `NtQuerySystemInformation(SystemProcessInformation)`, which carries every
process's kernel and user time without opening any of them. It is what Task Manager and the `\Process(*)\% Processor
Time` counters read. As `claude` the counter answered `msmpeng 105.89` and `searchindexer 13.88` at 18:50 while
directors were building. `golang.org/x/sys/windows` (v0.47, already required) has `NtQuerySystemInformation` and
`SYSTEM_PROCESS_INFORMATION`, so there is no PDH, no WMI and no cgo.

The rest of the item stands: detect the symptom, write the command with the room user's own paths, never apply it,
dismiss per room. Three decisions it left open:

- **Only `MsMpEng.exe` raises this advisory.** `SearchIndexer` has a different fix (indexing options, not Defender
  exclusions), so it is a separate advisory under u-new-machine-health-gear. SmartScreen is triggered by starting
  downloaded programs, not by builds, and is left out.
- **Process exclusions are in the command, commented out**, under a line saying they are broader than path
  exclusions. The path exclusions are the recommendation.
- **The threshold** is a 2 minute average over 50% of one core while any card on the room is mid-turn, cleared by a
  10 minute average under 25%. Averages, not every sample, because Defender's scanning comes in bursts.

## What is there today

- `internal/roomstats` samples the room every 10 s and pushes a `room-stats` event: tokens, the room process (by
  `GetProcessTimes` on its own handle), the machine, disk and runners. `GET /v1/room/stats` answers the last
  snapshot. A section that fails keeps its last values and gains `stale_since`.
- The hub's rooms dashboard (`internal/api/web/js/rooms-dash.js`) draws one tile per room from those snapshots,
  keyed by the snapshot's `room`. So anything the room puts in its snapshot reaches every board that shows the room,
  which is the item's "Hub: the same advisory for every attached Windows room" with no hub code.
- `docs/user-guide.md` Pattern 13 has the fix and the trap: `$env:LOCALAPPDATA` in an admin shell is the admin's
  profile, not the agents'.
- The daemon runs as the agents' user, and runner env is uniform, so `go env` run by the daemon answers for the
  agents.
- Daemon-wide settings keyed by name live in `internal/store/settings.go`.

## 1. Sampling other processes (roomstats)

A new source, Windows only: `Watch func() (map[string]time.Duration, error)`, the cumulative CPU time of each
watched image name, summed over its instances. Implemented once in `plat_windows.go` with
`NtQuerySystemInformation`, growing the buffer on `STATUS_INFO_LENGTH_MISMATCH`, and matching `ImageName`
case-insensitively. Other platforms leave the source nil and the section absent.

The watched names are a fixed list in code: `MsMpEng.exe` now, `SearchIndexer.exe` when its advisory is built.
Nothing configurable, since each name only means something with an advisory written for it.

The sampler turns two readings into a percent of one core, exactly as `sampleProcess` does for the room, and
publishes `watched: {"MsMpEng.exe": 105.9}`. A process that vanishes between samples is dropped, not reported as
zero. A failed read marks the section stale like any other.

Cost: one system call and a buffer of a few hundred KB every 10 s. Task Manager does the same every second.

## 2. The detector (daemon)

In memory, like activity, and evaluated on each roomstats tick.

- **Raise** when the 2 minute average of `MsMpEng.exe` is over 50 and at least one card on this room was mid-turn
  (`act.midTurn`) for some part of that window. The mid-turn condition keeps a scheduled full scan at 3 a.m. from
  raising it: that is Defender doing its job, and exclusions would not help.
- **Clear** when the 10 minute average is under 25. The advisory goes away by itself once someone applies the fix,
  which is the only confirmation atrium can get, since exclusions cannot be read back without elevation.
- **Nothing stored.** A restart starts the window over. The advisory describes the machine now.

The snapshot gains `advisories: [...]`, each one:

```json
{
  "id": "defender",
  "since": "2026-09-30T18:50:00Z",
  "seen": "MsMpEng.exe averaged 103% of one core for 2 minutes while agents were building",
  "command": "...",
  "doc": "docs/user-guide.md, Pattern 13",
  "dismissed": false
}
```

`seen` is written by the room, so the board shows the room's own numbers and has no threshold logic.

## 3. The command, with this room's paths

Built when the advisory is raised, and cached until it clears.

- `go env GOCACHE GOMODCACHE`, run once by the daemon with a 5 s limit, using the `go` the runners would find. When
  `go` is not found, those two lines are left out and the command says so in a comment.
- The worktree root of every provider with worktrees (`Providers()`, the same list `worktreeVolume` reads).
- `build.claude` under each distinct card worktree that has one and is not under a worktree root already listed.
  That is the main checkout's build directory on sg4. The checkout itself is not listed: it is not only build
  output.
- Each path single-quoted with `'` doubled, one per line with a backtick continuation, so it pastes into an elevated
  PowerShell as is.

```powershell
# Run in an ELEVATED PowerShell. These are the paths of claude, the user atrium's agents run as.
# Do not replace them with $env: variables: in your admin shell those expand to your own profile.
Add-MpPreference -ExclusionPath `
  'D:\worktrees', `
  'D:\git\github\dovholuknf\atrium\build.claude', `
  'C:\Users\claude\AppData\Local\go-build', `
  'C:\Users\claude\go\pkg\mod'
# Broader: these exclude every file these programs open, wherever it is.
# Add-MpPreference -ExclusionProcess 'go.exe', 'chrome-headless-shell.exe'
```

`%TEMP%`, where `go test` builds its binaries, is NOT offered. Downloads land there too. Pointing `GOTMPDIR` at an
excluded directory is the fix for that, and it belongs in room provisioning (f-new-defender-at-provision), not in a
command a person pastes.

## 4. Dismissal

- `POST /v1/room/advice/{id}/dismiss` on the board listener, and `DELETE` to undo. Stored as the setting
  `advice.dismissed.<id>` with the time. That is the one thing this design writes down, and it is a person's
  decision, not a sample.
- A dismissed advisory is still detected and still in the snapshot, with `"dismissed": true`. The board draws no
  notice for it, and u-new-machine-health-gear lists it greyed with an undo. So a machine whose admin chose not to
  exclude is never nagged, and nothing is hidden from someone who goes looking.
- Not on the agent listener, and refused on a lent session's guest listener like the rest of `/v1/room/*`.

## 5. The board (@ui)

- The room's tile on the rooms dashboard carries a notice line for each advisory not dismissed: `seen`, a "show
  fix" toggle that opens the command in a copyable block, the doc reference as text, and "dismiss for this room".
  No growler and no sound: this is not an interruption.
- The room's settings show the same block.
- The notice is drawn from the snapshot, so a hub board shows every attached Windows room's advisories.
- When u-new-machine-health-gear is built, the gear's machine section takes over the full listing, and the tile keeps
  only a one-line notice pointing to it.

## Stages

| stage | what | owner | size | acceptance test |
| --- | --- | --- | --- | --- |
| D1 | `watched` section: other processes' CPU from the system process list | @runtime | small | a fake `Watch` source gives two readings 10 s apart, 10 s of CPU apart, and the snapshot reads 100. A vanished process is dropped. On Windows, a test reads `System` or its own image by name and gets a non-negative number without elevation |
| D2 | detector, command and dismissal | @runtime | medium | a fake clock and fake watched numbers: 2 minutes over 50 with a card mid-turn raises the advisory, the same with no card mid-turn does not, 10 minutes under 25 clears it. The command has one line per path, doubles a `'`, and leaves out the `go env` lines when `go` is missing. Dismiss sets `dismissed` and survives a restart, undo clears it |
| D3 | notice on the rooms dashboard tile and the room's settings | @ui | small | headless: a mocked snapshot with one advisory draws the notice and the copyable command, dismiss calls the endpoint and removes it, a dismissed advisory draws nothing |

D1 and D2 are one branch for @runtime if it prefers. D3 waits on D2's shape only, and can build against a mocked
snapshot.

## Questions for later

None for clint. Decided here: MsMpEng only, process exclusions commented out, 50% for 2 minutes with a card mid-turn,
25% for 10 minutes to clear, `%TEMP%` left to provisioning.
