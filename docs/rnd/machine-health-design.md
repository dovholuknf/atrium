# Machine health in the gear, and "profile this machine" (u-new-machine-health-gear)

Status: proposed, not built.

Origin: designed by @rnd 2026-09-30, for @runtime (room half) and @ui (gear). Nothing here is built. Backlog item
`docs/backlog/ui/u-new-machine-health-gear.md`. Builds on `docs/rnd/defender-advice-design.md`, whose advisory
shape this reuses.

## The answer

Two parts, and the second changes shape from the item.

1. **The machine section** lists each room's advisories in the shape the Defender design already puts in the
   `room-stats` snapshot (`id`, `since`, `seen`, `command`, `doc`, `dismissed`). A new advisory is a detector in the
   room and nothing on the board. The second one is a disk nearly full, because the disk section is already sampled.
2. **"Profile this machine" gives the agent no shell.** The item asks for a read-only agent that samples processes.
   Atrium cannot make a shell read-only. A standing rule matches a command by prefix (`matchPattern` is
   `strings.HasPrefix`), so an allow rule for `Get-Process` also approves `Get-Process | Stop-Process`, and board-wide
   auto mode approves whatever no rule answers. So the ROOM does the sampling, which it already knows how to do,
   and writes the samples to a file. The agent reads that file and writes a report. Its shell and every write
   outside its own directory are refused by standing rules, which beat auto mode (`docs/runtime/auto-mode.md`, "What
   it does NOT override").

This is also cheaper: minutes of sampling cost the agent nothing, and it spends tokens only on reading one file and
writing one.

## What is there today

- `internal/roomstats` samples the room every 10 s and pushes `room-stats`. The Defender design adds `watched`
  (other processes' CPU, from the system process list) and `advisories`.
- The hub board draws a tile per room from those snapshots (`rooms.js`, `rooms-dash.js`), so the gear on a hub
  board can list every room's advisories with no hub code.
- Standing rules (`internal/store/rules.go`) carry a `scope`, the worktree the request came from. A path rule
  (`KindPath`, `matchPath`) allows a path inside a directory and refuses anything that reaches outside it. A broad
  rule with pattern `*` needs `AddBroadRule`. The most specific match wins.
- The daemon knows each supervised runner's pid (the supervisor and the reaper use it).

## 1. The machine section

In the gear, one block per room, drawn from that room's latest snapshot:

- Each advisory not dismissed: `seen`, the numbers, "show fix" opening the copyable `command`, the `doc` reference,
  "dismiss for this room", and "profile this" (section 3, narrowed to the advisory).
- Dismissed advisories, greyed, each with "undo". Nothing is hidden from someone who looks.
- "Profile this machine", for the room as a whole.
- A room with no advisories says `nothing noticed` and still has the profile button.

The rooms dashboard tile keeps one line, `2 notices, see the gear`, once this section exists. The Defender design's
tile block (its stage D3) is replaced by that line when this is built.

### The second advisory: disk nearly full

`disk` already reports free and total bytes of the worktree volume. Raise `disk-low` when free space is under 10%
or under 10 GB, whichever is larger, and clear it above 15% and 15 GB. `seen` names the volume and the numbers. The
command lists the ten largest card worktrees of cards that are done or archived, as `Remove-Item` lines commented
out, since deleting is a person's choice. No new sampling.

The others the item names (SwiftShader in headless runs, an indexer on the worktree root, a reserved port range)
each become one backlog item for @runtime in this shape when someone wants them. None is designed here.

## 2. A process table the room can produce

`roomstats` gains `Processes()`: every process's pid, parent pid, image name, CPU percent over the last interval,
and working set. On Windows it is the same `NtQuerySystemInformation(SystemProcessInformation)` read as the
Defender design's `watched`, which already carries the parent pid (`InheritedFromUniqueProcessId`). On Linux it is
`/proc/<pid>/stat` and `statm`. On macOS it is absent, and the profile button says `not available on macOS yet`.

Each process is attributed to a card where its parent chain, walked at most 16 levels, reaches a supervised
runner's pid. The row then carries the card's id and handle. Anything else is `unattributed`, which on Windows
is where Defender, the indexer and the build servers show up.

Not an endpoint in this design. It exists to feed section 3, and a live process table on the board is a separate
question nobody has asked.

## 3. Profiling: the room samples, the agent reads

### The run

`POST /v1/room/profile` on the board listener, with an optional `{"advisory": "<id>"}`. One run per room at a
time, and refused when the room is at its worker cap, saying so.

1. The room makes its profiler directory, `<data dir>/profiler`, reused every run, and a subdirectory per run
   named by its start time.
2. For `minutes` (default 5), every 10 s, it appends one line to `samples.jsonl` in the run directory: the time, the
   machine section, `watched`, the advisories, and the top 25 processes by CPU plus every watched one, attributed.
   It also writes `cards.json` once: each card on the room with its handle, status and what it is doing.
3. It launches the profiler card: the room's default Claude harness, model Sonnet, `cwd` the run directory, tags
   `atrium:profiler` and `origin:atrium`, and a fixed brief (below). It counts toward the room's worker cap like any
   worker.
4. When the card's turn ends and `REPORT.md` exists in the run directory, the room marks the card done and lets it
   exit. The operator reads the report through the card's files, which `internal/safepath` already confines to its
   directory.

The card appears on the board from step 3, with a chip saying `sampling, 3 of 5 minutes` during step 2, so the
button is not silent for five minutes. So the card is created at step 1 with no runner, and the launch in step 3
starts its runner.

### What keeps it read-only

At the first run, the room writes these standing rules, scoped to `<data dir>/profiler`, once:

| tool | rule | decision |
| --- | --- | --- |
| `Bash`, `PowerShell` | `*` (broad) | block: "the profiler has no shell. Everything it needs is in samples.jsonl" |
| `Write`, `Edit`, `NotebookEdit` | path rule `<data dir>/profiler` | approve |
| `Write`, `Edit`, `NotebookEdit` | `*` (broad) | block: "the profiler writes only its report" |
| `WebFetch`, `mcp__*` | `*` (broad) | block |

The path rule is more specific than the broad block (`specificity` counts the directory's characters, the broad
rule none), so writes inside the directory pass and writes outside are refused. Rules sit ahead of both auto modes in the permission chain, so auto mode cannot approve around them. The
Read tool is not gated, and reading is what the agent is for. It runs as the agents' user, not elevated, so it
could not change a machine setting even if a write got through.

Reusing one directory means the rules are written once and the rule list does not grow per run.

### The brief

Fixed text in code, one paragraph, narrowed by one sentence when an advisory is given:

> You are profiling the machine this atrium room runs on. Read samples.jsonl and cards.json in this directory. Do
> not run commands: you have no shell, and everything you need is in those files. Find what used the most CPU,
> memory and disk, and attribute each to a card or to the system. For each problem, say what was seen with numbers,
> the likely cause, and a fix a person would run, as a command with this machine's paths. Never claim a fix was
> applied. Write REPORT.md in this directory, under 150 lines, and end your turn.

With an advisory: `Start with the advisory "<seen>", and say whether the samples confirm it.`

## Stages

| stage | what | owner | size | acceptance test |
| --- | --- | --- | --- | --- |
| M1 | `Processes()` with parent pids and card attribution, Windows and Linux | @runtime | medium | a fake process list with a runner pid three levels up attributes the grandchild to the card, a process with no runner ancestor is unattributed, a parent loop stops at 16. On Windows, reading its own pid without elevation returns its parent |
| M2 | `disk-low` advisory | @runtime | small | fake disk numbers at 8% raise it, 12% keeps it, 16% clears it. The command lists only done or archived cards' worktrees, commented out |
| M3 | `POST /v1/room/profile`: samples, rules, launch, done on report | @runtime | medium | a fake clock runs a 1 minute profile: `samples.jsonl` has 6 lines, the card launches after it with the brief and `cwd` in the run directory, and the four rules exist once after two runs. A Bash request from that directory is blocked with board-wide auto ON. A Write inside is approved, a Write outside is blocked. A second run while one is going is refused |
| M4 | the gear's machine section and the buttons | @ui | medium | headless: mocked snapshots for two rooms, one with a Defender advisory and one dismissed, draw both blocks, "undo" and "dismiss" call the D2 endpoints, "profile this" posts the advisory id, and the tile shows the one-line count |

M1 and M3 wait on the Defender design's D1, which adds the Windows process-list read they reuse. M4 waits on D2's
advisory shape only, and can be built against mocked snapshots. M3's rules touch the permission chain's inputs, not
its order, and @review should read M3 before it ships.

## Questions for later

None for clint. Decided here: no shell for the profiler, the room samples for 5 minutes, one run per room, Sonnet,
disk-low as the second advisory at 10% or 10 GB.
