# More than one room per machine, and a room that drains

Backlog-2 item 59, raised by clint on 2026-09-28 and parked as "seems dumb. deep backlog". This is the design only.
Nothing here was run, and no room was started or stopped to write it.

The item asks for two things, and names the goal they serve:

- more than one room on one machine
- a room marked blocked takes no new work and drains, while a fresh room on the same machine picks up new work

> The goal is to move work between rooms, so that a room restart kills nothing.

The short answer: the first half is cheap, mostly built already, and worth finishing. The second half, as asked, does
not reach the goal, because the sessions that matter most never drain. The goal is reachable another way, by a small
process that holds each runner's terminal and outlives the room. That is a separate item and a larger one, and this
document proposes it with a spike plan.

## What a room restart costs today

A room owns a pseudo terminal per supervised runner, and closing it ends the runner (`docs/supervision-design.md`).
So a room restart ends every runner on that machine. What comes back and what does not:

| comes back | lost |
| --- | --- |
| the conversation, through `--resume` and the card's `resume_id` | the turn in flight, including a long `go test` or a subagent |
| fixtures and the reopen list, restarted by the room | background shells (`run_in_background`) and anything they started |
| scrollback, written out during the wind-down | processes in the process registry (`docs/process-registry-design.md`) |
| a queued wake, typed once the runner is back (`docs/restart-wake.md`) | half a line typed by a human, and the human's attention |
| the unexpected-exit notice (`docs/unexpected-exit-wake.md`) | a warm prompt cache, when the gap outlives its TTL |

A restart asked for by the hub already parks first: `onHubRestart` in `internal/cli/roomrestart.go` tells every busy
supervised session to stop at a safe point, waits up to 90 seconds, and refuses to restart while anything is still
busy unless the ask says `force`. So the cost above is paid by a forced restart, or by a session that parked in the
middle of a long job.

## What already exists for two rooms on one machine

More of it than the item assumes.

- **`--isolated`** (`internal/cli/roomrun.go`, `isolatedFlag`). A second room keeps its address file beside its own
  `--dir` instead of the machine's shared one. Without it the second room overwrites the file every hook reads, and
  the first room's hooks start arriving at the second (`docs/hub-room-requirements.md`, "A second room on ONE
  machine needs --isolated").
- **`ATRIUM_LOCATION` is inherited.** A room sets it before it opens anything and spawns runners with its own
  environment, so a runner's hooks, its `atrium` CLI calls and its stdio control MCP all find the room that started
  it (`internal/daemon/whereami.go`, `LocationPath`).
- **`ATRIUM_ROOM` is set per launch** (`internal/daemon/launch.go`), and a daemon started from inside a session does
  not leak that session's room to its own children. The hub scopes control calls by it.
- **Separate `--dir`, `--db`, `--http` and `--agent`.** Every port and path a room uses is a flag.
- **The hub names rooms by certificate.** `hub room add <name>` mints the name into the join token. A second room is
  a second name.
- **The hub replaces a room with itself.** A new connection presenting the same certificate replaces the old one,
  and a different certificate under an attached name is refused (`internal/link/hub.go`). So two processes cannot
  share one room name and both stay reachable.

What is missing is everything a person would need to run one on purpose: a name for the pair, ports that do not
collide, an autostart per instance, and provisioning that knows about any of it.

## Three designs

### A. Sibling rooms: a second room on a machine, supported

Two rooms, two names, two stores, side by side for as long as both are wanted. `sg3` and `sg3-test`.

It is useful without any drain:

- **A test room beside a live one.** The standing rule that stop, `-Remove`, service and autostart tests run only on
  claudevm exists because a real machine has exactly one room and it is in use. A sibling room can be broken freely.
- **A different identity.** A second OS account, or a second `CLAUDE_CONFIG_DIR` signed in to a different claude
  account, gets its own room and its own board scope.
- **A smaller blast radius.** Clint's own cards on one room and a batch of workers on the other, so restarting the
  workers' room for a deploy does not touch his.

### B. Drain to a sibling: what the item asks for

Mark the old room blocked. It takes no new launches, keeps its running sessions, and exits when the last one ends. A
fresh room on the same machine, usually on a newer binary, takes the new work.

### C. A holder that outlives the room

Each runner's pseudo terminal is created and held by a small separate process, not by the room. The room connects to
the holder over a local socket. When the room exits, the holders and their runners keep going. When a room comes back
it finds the holders and reattaches. A restart costs the board a few seconds of silence and nothing else.

## Why B does not reach the goal

1. **Resident sessions never drain.** The orchestrator, every director, and clint's own cards stay up for days on
   purpose. A room waiting for them to end waits for ever, so a drain either never finishes or ends by killing them,
   which is the restart it was meant to avoid. The sessions a drain does save are the short-lived workers, and those
   are the cheapest to lose: they park, resume and carry on.
2. **A card cannot move between rooms.** Each room has its own store, and a card id is `room~id` outside a room
   (`docs/card-room-routing.md`). A card that should go on in the fresh room after its runner ends on the old one
   needs a move: its row, its events, its resume id, its seen state, its wake, a tombstone that redirects the old
   id. That is a new feature in Runtime's store, and `export.go` does not help, because it exports configuration
   and never cards.
3. **The binary swap assumes one instance.** `docs/reload-design.md` moves `atrium.exe` aside to `atrium.old.exe`
   and deletes the old one on the next restart, once nothing runs it. A draining room runs it for as long as it
   drains, and a second deploy during that drain finds `atrium.old.exe` in use. The fix is a versioned binary per
   instance, and then hooks and the control MCP, which name one installed path, run the NEW binary against the OLD
   room. That protocol skew is new and nothing tests for it.
4. **It doubles the room-shaped state on the machine.** Two sets of ports, two stores, two sets of fixtures that
   both want to start, two copies of every source polling on a timer.

The one part of B that is cheap and worth having is the flag itself: a room that takes no new work. It is below as
"Draining in place".

## The recommendation

1. **Build A**, small, in Fabric: named sibling rooms, provisioned by one flag.
2. **Build "draining in place"**, small: a room can refuse new work, and a restart can wait for it to empty. It
   reuses the park that exists and adds no second instance.
3. **Do not build B.**
4. **Open C as its own item** for the goal, with the spike below. It touches `internal/daemon/supervisor.go`, which
   is the Terminal department's, and the permission hook, which is everybody's, so it needs atrium-87300 to decide
   who owns it.

## A in detail: sibling rooms

**Naming.** A sibling is `<machine room>-<instance>`, minted by `hub room add sg3-test` like any other room. The hub
already treats it as a separate room. Nothing on the hub changes.

**Paths.** Everything for an instance lives under its own directory, so removing one is removing a folder:

| what | first room (today) | instance `test` |
| --- | --- | --- |
| certificate, `room.json` | `~/.atrium/room` | `~/.atrium/rooms/test` |
| database | `~/.atrium/atrium.db` | `~/.atrium/rooms/test/atrium.db` |
| address file | the machine's shared one | `~/.atrium/rooms/test/daemon.json`, through `--isolated` |
| autostart | logon task `atrium`, `atrium.service`, LaunchAgent `io.github.dovholuknf.atrium` | the same, suffixed `-test` or `.test` |
| ports | 7781 (room board), 7777 (agents) | a block of its own |

The first column is what `provision-room.ps1` writes today, and it does not change. The second is the proposal.

**Ports.** Each instance takes a block of ten, above the first room's block, recorded in the provision manifest so a
rerun reuses them. The first room keeps its ports. `provision-room.ps1` names 7781 and 7778 in about ten places (the
health probe, every `atrium stop --url`, the port-free wait), so `-Instance` means routing all of them through one
variable first, which is a refactor worth doing on its own. Choosing by probing for a free port was rejected: a port
that is free now and taken after a reboot moves the room, and every hook and bookmark with it.

**The first room is the machine's room.** Only it writes the shared address file, so a claude session somebody opens
by hand on that machine reports to the first room. A sibling sees only the sessions it launched. That is what
`--isolated` does today, and it is the right default: an unmanaged session has to land somewhere, and one place is
easier to reason about than a guess.

**What they share, and why that is fine.** `~/.claude` is shared: `settings.json` names `atrium hook`, which reads
`ATRIUM_LOCATION`, so each runner reports to its own room. Conversation files are keyed by working directory, so two
rooms never write one file unless they run cards in the same folder, which is the same hazard as two cards in one
folder on one room. Folder trust is per folder and item 67 writes it from the binary.

**Provisioning.** `provision-room.ps1 <target> -Instance test` does what it does today with the table above, plus
`--isolated` in the start command and the instance name in the autostart name. `-Remove -Instance test` removes only
that instance. `-SmokeOnly -Instance test` smokes it. Without `-Instance` nothing changes.

**Test plan letter:** FD, when it is built.

## Draining in place

A room can be marked **not accepting**. Nothing else about it changes.

- **Where the flag lives.** In the room, as a row in its `setting` table, which is key-value, so there is no
  migration. The room announces it to the hub with the rest of what it announces (`internal/link/announce.go`),
  and the hub's `room_card` cache in `internal/hubstore` keeps the last answer for a room that is offline. Not a
  column on the hub's `room` table: whether a machine takes work is a fact about the machine
  (`docs/hub-room-requirements.md`: "Anything about A MACHINE belongs to a room"), and a flag the hub held would
  go on saying so about a room that was reinstalled under the same name.
- **Who sets it.** The board's room page, `atrium room accept off|on` on the machine, and a hub endpoint for scripts.
- **What it refuses.** A new launch placed on it: from the board's launch dialog, from `atrium_launch` with that
  room, and from `atrium launch`. The refusal names a room that is accepting, when there is one. The launch dialog
  greys the room out with the reason. With one room attached and it not accepting, a launch is refused rather than
  placed there anyway.
- **What it allows.** Everything on a card already there: typing, messages, permissions, a runner resumed onto an
  existing card, a fixture starting. Refusing a resume is B's card move again, by the back door.
- **The restart that waits for it.** A hub restart ask gains `until_idle`: park, then keep waiting until no card on the
  room is busy, with no 90 second cap, and restart the moment it is empty. The board shows the room as `draining`
  with the busy cards named. `force` still interrupts. The flag turns itself off when the restarted room comes up,
  unless it was set with `--keep`.
- **What counts as busy.** A card whose runner is mid-turn, read off the activity badge
  (`docs/activity-design.md`). Nothing else. A fixture is judged the same way: a shell fixture has no turns and
  never holds a drain open, and a claude fixture holds it only while it is mid-turn, like any card. A fixture
  starting during a drain does not add to the wait unless it starts a turn, and it is parked and restarted with the
  room as it is today.

It is still a restart. Resident sessions are parked and resumed. What it buys is that no worker is cut off mid-turn,
without anybody having to watch for the moment they all stop.

## C in detail: a holder that outlives the room

### The objection it has to answer

`docs/architecture-v2.md` ("Open risks") and `docs/process-registry-design.md` both say ConPTY offers no reattach, so
a runner cannot outlive the room on Windows. `docs/charon.md` section 3 adopts Charon's holder for POSIX only, for the
same reason.

That is true of handing a pseudo console from one process to another, and C never does that. The holder CREATES the
pseudo console and keeps its handle for the runner's whole life. The room never owned it, so there is nothing to hand
over. The claim to prove is narrower and checkable: a pseudo console created by a detached process outside the room's
job object keeps its client alive when the room exits. `startDetached` in `internal/cli/spawn_windows.go` already
starts a process that way, for the room's own restart.

### The shape

```
  room (restarts)                   holder, one per runner (stays)          runner
  ┌───────────────────────┐  pipe   ┌───────────────────────────────┐  pty  ┌────────┐
  │ supervisor            │ <─────> │ owns the pseudo terminal      │ <───> │ claude │
  │  screen, typed line,  │         │ ring spool while nobody reads │       └────────┘
  │  attach fan out       │         │ exit code, kept until read    │
  └───────────────────────┘         └───────────────────────────────┘
```

- **One holder per runner**, started by the room with `startDetached`. A holder that dies takes one runner, not all.
- **The socket.** A named pipe on Windows with a DACL for the current user, a `0600` Unix socket elsewhere, named by
  card id under the room's directory. One room connection at a time, and a new one replaces the old, which is what
  makes a crash and a restart racing each other safe (Charon, `holder.py:59-60`).
- **Five messages:** output, input, resize, exit, and a hello carrying the holder's protocol version and the runner's
  pid.
- **A spool while nobody reads.** Output is appended to a capped ring. On reattach the holder replays it before any
  live output, which rebuilds the room's screen model and scrollback in order.
- **Adoption.** A starting room lists the sockets in its directory, connects, and matches each hello to a card by id.
  A socket that does not answer within a bounded wait is left alone and reported, never deleted: a busy holder is
  not a dead one.
- **The binary.** A holder runs for as long as its runner, which is days, so it must not run the room's own binary or
  it holds that file open through every swap. Each protocol version gets its own copy, `atrium-hold-v1.exe`, written
  once and never replaced. A room speaks every holder version it knows about.

### The hazard it creates

`docs/hooks.md` rule 2: when atrium is unreachable, the permission hook approves. That is safe today only because a
supervised runner cannot run while its room is down. With C it can. Every tool call during a room restart would be
approved without asking, which is auto mode switched on by accident, and unrecorded.

So C has to change the permission hook for a supervised session. A session with `ATRIUM_TASK_ID` set, whose room
does not answer, waits and retries on the backoff Mode A already has, up to a bound, and then fails open as now. An
unsupervised session keeps failing open at once. This is a change to a CRITICAL guarantee in `CLAUDE.md`, and it needs
clint's yes before anything is built.

### The spike, when it is approved

On claudevm only, never a live room:

1. A Go program that creates a pseudo console with `startDetached`'s flags, runs `cmd /c ping -t 127.0.0.1` in it, and
   serves its output on a named pipe. Kill the parent that started it. The ping keeps running, and a new client on the
   pipe reads it. This is the one fact the design rests on.
2. The same with `claude` in the pseudo console, driven through a resize and a reattach.
3. The same on m1mini, only once clint says m1mini may be used, and on Linux when there is a box.

If step 1 fails, C is POSIX only, as `docs/charon.md` already says, and the Windows answer stays park and resume.

## Open questions for clint

1. Is a sibling room (A) worth building for the test room alone? It retires the claudevm-only rule for anything a
   sibling can stand in for.
2. Draining in place: is waiting for workers to finish before a restart worth a flag, or is today's 90 second park
   enough?
3. C: may the permission hook for a supervised session wait for its room, instead of failing open at once?
4. C: who owns it? The holder is the Terminal department's supervisor. The adoption and the hook are Fabric's and
   Runtime's.

## Out of scope

- Moving a card from one room to another.
- Two processes serving one room name. The hub refuses it and should go on refusing.
- Choosing a room for a launch automatically by load. The launch dialog asks when there is more than one room, and
  that stays.

## Review

Mercurius design review, session `s_AUQNaSEQ3M5Y`, round 1, verdict ready_to_build, no concerns or questions.

- A1 (advisory), a fixture starting during a drain could keep an `until_idle` restart waiting. Folded in: busy
  means mid-turn on the activity badge and nothing else, so a shell fixture never holds a drain open and a claude
  fixture holds it only while it is mid-turn. See "What counts as busy" under "Draining in place".
