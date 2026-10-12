# The next room and the last room: deploy beside the old build, never over it

Status: design, 2026-10-12. Nothing here is built. Written for `f-next-room-last-room` (high). It replaces the
2026-10-10 draft of this file, which left six questions open for clint, assumed `pty_host` already kept runners
across a deploy, and had the hub route between two generations. Every question is decided below with its reason. The
build is eight phases, filed as `f-blue-green-1` to `f-blue-green-8`, each one a worker can follow without a judgment
call.

## The problem

clint, 2026-10-08: "we still deploy rooms so often we really need to have a 'the last room' and 'the next room' idea so
that we can just START THE NEXT ROOM up and then just start using that new room and eventually decom the 'old room' on
those machines... and as always if NOTHING is running on a room - it can ALWAYS be redeployed immediately".

Today a deploy stops the one room on a machine and starts the new build in its place. On 2026-10-12 at 15:20 a deploy
interrupted a build worker mid-phase (it resumed) and a launch made during the restart came up with no prompt typed
(`r-launch-prompt-lost-in-deploy`). Both come from the same fact: for some seconds or minutes there is no room, and
every runner the room owned ends with it.

What is wanted, per machine: a LAST generation (the old build) and a NEXT one (the new build). A deploy starts NEXT
beside LAST and never stops LAST. New launches, resumes and new cards go to NEXT once it is up. `room: m1mini` means
m1mini's current generation. LAST keeps its running cards until each ends, parks or moves, and exits when nothing runs
on it. The board shows one room, with LAST's cards marked "old build".

## What exists

- **One room per machine** (`internal/cli/roomrun.go`, `runRoom`). It binds the fixed ports in
  `internal/cli/atrium_defaults.go` (agents 7777, its own board 7781), opens one sqlite store (`defaultRoomDB`), and
  sets `ATRIUM_LOCATION` in its own environment to the machine's one address file (`roomLocationEnv`), which every
  runner inherits. `writeLocation` (`internal/daemon/whereami.go`) refuses to take that file while another live pid
  holds it (`takingOver`). `--isolated` puts a second room's file beside its `--dir` instead. A daemon given
  `Options.LocationFile` already passes that file to its runners (`tellWhereIAm`).
- **The store** (`internal/store/store.go` header). Open runs every migration and a failure refuses to start (tier
  one). The first error that is not contention halts the store and closes the agent listener (tier three). Migrations
  are forward only.
- **The hub** keys attached rooms by name (`internal/link/hub.go`, `h.rooms[keyOf(name)]`). A second attach under the
  same name with the same certificate key replaces the first and closes it, killing its in-flight requests
  (`old.close("replaced by a newer connection from the same room")`). A different key is refused. About forty hub
  callers iterate `h.Rooms()` and dial a room by name, and every one of them assumes one link per name.
- **The room's announcement** (`internal/link/announce.go`) reads the room's own `GET /v1/tasks` and the hub caches it
  by room name. The control MCP finds a caller's card by listing the room's `/v1/tasks` through the hub
  (`callerCard`, `internal/link/docs_mcp.go`).
- **`pty_host`** is built and off on every live room (`internal/ptyhost`, `internal/daemon/hostterm.go`). The host
  keeps each pty and drains it while no daemon is connected, and a starting daemon reattaches (`reattachRuns`). The
  host's `probe` already reports `in_job` (design gap G5). **But a graceful stop still ends every runner, host or
  not**: `shutdown` in `internal/daemon/daemon.go` calls `stopSupervised`, which winds down every runner with its exit
  keys, and nothing there skips a host-held one. Only a daemon that is killed or crashes leaves its host runners up.
  Stage 2 of `docs/rnd/rolling-restart-design.md` (the stop that leaves runners up, the short gate, the
  re-derivation list, the 30 second permission retry) and stage 3 (the G5 refusal, the service changes, turning it on)
  are not built. I found no retry across a restart in `internal/cli/hook_permission.go`.
- **The deploy hold** (`docs/rnd/room-deploy-hold-design.md`, built) makes every agent end its turn before a deploy and
  wakes them after. It exists because a deploy ends every runner.
- **Room handoff** (`docs/rnd/room-handoff-design.md`) moves a card between rooms. M1, the room side, is built
  (`internal/daemon/move.go`, `moveroutes.go`): freeze, capture, park, `moved_to`. M2 and M3 are not.
- **The deploy** (`scripts/live/atrium-deploy.ps1`) cross-builds, copies, runs `atrium stop`, waits for port 7781 to be
  free, swaps the binary keeping `atrium.prev`, starts the room, and checks it is back on the hub and unheld.

## Does `pty_host` everywhere give most of this sooner

**Yes, once its stage 2 and 3 are built, and that is phase 1.** Turning the setting on today gives nothing for a
deploy, because the deploy's `atrium stop` winds down the host's runners too. With stage 2 and 3 built and the setting
on, a deploy is a daemon restart of a few seconds that ends no runner, resumes nothing, needs no deploy hold for
running cards, and keeps a half-typed input line. That is the larger half of the pain in the item (the interrupted
build worker) and it is a week of work on a design that is already decided.

What blue-green adds beyond it:

| | `pty_host` on, stage 2 and 3 built | Blue-green on top |
| --- | --- | --- |
| Runners survive a deploy | yes | yes |
| A gap with no room | a few seconds: hooks lost, a permission long-poll retried, a launch in flight cut | none: LAST answers until NEXT has taken over |
| A launch made during the deploy | can land in the gap (`r-launch-prompt-lost-in-deploy`) | lands on LAST or NEXT, both are up |
| A bad build | is already the room. Rollback is another restart onto `atrium.prev` | is checked before it takes anything. Before the handover, abort is free |
| A migration that fails | tier one, the room is down until fixed | NEXT refuses to start, LAST keeps serving |
| In-memory state (idle marks, activity counts, wake signals) | re-derived after reattach, some of it guessed | never lost: LAST keeps it for its own cards |
| Upgrading the pty host itself | the per-card idle move (rolling-restart section 6) | a new slot has its own host |
| Cost | the stage 2 and 3 work | two slots, a store handover, a room-side forwarder, a small hub change |

So the two compose. Phase 1 is `pty_host` finished and on, and it is useful alone. Blue-green is phases 3 to 8. It
also leans on phase 1 once: the bootstrap deploy, the first one onto a slotted build, is an ordinary restart, and
phase 1 makes that lossless.

## Decisions

Each is decided here, because clint is away and the brief says to decide.

1. **Phase 1 is `pty_host` finished and turned on everywhere.** Reason above: most of the pain, soonest, on a design
   clint already approved (rolling-restart section 10).

2. **Two fixed slots per machine, A and B, and a deploy always takes the free one.** Slot A is today's layout exactly,
   so a machine that has never heard of slots is already in slot A with no migration. Slot B is the same layout under
   `<dir of the room db>/slot-b/` with both room ports shifted by +100: 7877 for agents and 7881 for its board, and on
   sg4-control 7887 and 7891. Fixed ports beat port 0 because the escape-hatch board and every script that names a
   port keep meaning something, and +100 was checked against sg4-control's 7787 and 7791 so it never lands on 7777.
   The hub's ports do not move, since the hub is not a room.

3. **One store per generation.** Two processes on one sqlite file means each meets the other's locks and checkpoints,
   two reapers and two idle clocks act on one set of rows, and the first error that is not contention halts a
   generation whose agents are mid-turn. Worse, the moment NEXT opens a shared file its migrations change the schema
   under LAST. One store per generation means NEXT migrates a file LAST never opens, and a migration that fails is
   tier one on NEXT only. Rolling-restart section 8 refused the shared store for the same reasons. Slot A's store is
   today's file. Slot B's is `slot-b/atrium.db`. The pty host's socket is derived from the store's directory
   (`hostStateDir`), so each slot gets its own host with no new code.

4. **Cards move between the stores by one primitive, cede, and never while they run.** LAST cedes a set of card ids to
   NEXT: it marks them ceded, takes a `VACUUM INTO` snapshot, and NEXT opens a copy of that snapshot with its own
   `store.Open` (so every NEXT migration runs on the copy), then copies every row belonging to those ids into its own
   store in one transaction, idempotent by id. Card ids, aliases and wire names do not change, so `room~id`, `name@room`,
   `report_to` and every handle a sender holds stay valid and no `moved_to` chain is needed. The handover cedes every
   card that is not live. A cede loop on LAST cedes each card the moment it stops being live. When LAST holds nothing,
   it exits. So a parked card on LAST is on NEXT before anyone resumes it, and LAST never keeps a card that does not
   need it.

5. **NEXT's store starts as a migrated snapshot of LAST's, not empty.** At the handover NEXT's store is the snapshot,
   migrated, with the rows of LAST's live cards deleted and those cards' ids, aliases and wire names written to a new
   table, `gen_held`. NEXT starts with the board's whole history and every setting, and it never allocates a name a
   live card on LAST holds. Settings are NEXT's from then on.

6. **"Live" has one definition**, used by the handover, the cede loop and retirement. A card is live on a generation
   when its status is `running`, `needs-input` or `needs-permission`, or that generation's supervisor holds a runner or
   a shell for it, or a new-context cycle is in flight for it (`d.nctx`), or a `restart_wake` row is owed to it, or a
   `card_resources` row of kind `proc`, `port` or `overlay` names it. Everything else (done, dead, shelved, backlog,
   parked) is not live and is ceded.

7. **A runner's hooks follow the generation that launched it, for life.** Each generation writes its own address file
   (`slot-a/daemon.json`, `slot-b/daemon.json`, both beside the slot's store) and passes it as `ATRIUM_LOCATION` to every
   runner it launches, through the existing `Options.LocationFile` and `tellWhereIAm`. A runner's hook reads that file
   on every fire, so it reaches LAST for as long as LAST lives and NEXT can never capture it.

8. **The machine's address file becomes a pointer the current generation owns.** It is what sessions atrium did not
   start, the CLI, `atrium join` and the control MCP read. NEXT takes it at the handover by an explicit act, never by
   `takingOver`'s accident rule, and records `slot` and `last` (LAST's agent and board addresses and pid) in it.
   Runners that predate slots carry the machine's file and their hooks therefore arrive at NEXT after the handover.
   Decision 9 forwards them, so the bootstrap needs no special case and the forwarder is permanent rather than a
   bridge to remove later.

9. **The hub routes by machine, and the current generation fronts the other one.** This is the decision the 2026-10-10
   draft got the other way round. The hub keeps one link per room name, exactly as today, and that link is NEXT's
   after the handover. NEXT answers for LAST's cards over loopback:
   - A request on NEXT's board or agent listener that names a card id in `gen_held` (in the path, in a launch body's
     `task_id`, or in a hook body's `task_id`) is reverse-proxied to LAST's matching listener, websockets included.
   - `GET /v1/tasks` on NEXT appends LAST's live cards, read from LAST over loopback, each with `"generation": "last"`
     and `"build": "<LAST's commit, 8 hex>"`.
   - NEXT relays LAST's event stream into its own, for LAST's cards only, with the same two fields.

   Because every hub caller (the board's lists and stream, the announcement cache, `callerCard`, `placeCard`,
   `placeByName`, the launch cap) reads the room through those same routes, none of the forty `h.Rooms()` callers
   changes. Two links per name would have touched every one of them, and each is a place a card could be listed twice
   or not at all.

10. **The hub changes in one place: the second link of a room drains the first instead of killing it.** The hello
    gains `gen` (`a` or `b`). In `control`, a new link with the same key and a different non-empty `gen` takes the
    name, and the old link is moved to `h.draining[name]` and left open until it hangs up or two minutes pass, so a
    request in flight on it finishes. Same key and same `gen` is a reconnect and closes the old one as today. A
    different key is refused as today. `Attached` gains `LastCommit`, so the room row can show both builds. A
    retiring LAST never redials the hub.

11. **The handover order is fixed, and nothing is lost in it.** Before it, LAST is the room and NEXT has proved itself
    on a throwaway copy (decision 12). Then: LAST marks every non-live card ceded and answers a write to a ceded card
    with 503 and `Retry-After: 2` ("this card is moving to the new build"), LAST snapshots, NEXT seeds its store from
    the snapshot, LAST starts forwarding ceded cards and room-level writes to NEXT instead of 503, NEXT takes the
    machine pointer, NEXT dials the hub. The 503 window lasts as long as a snapshot and a migrated open, seconds, and
    touches only cards that are not running, so it costs a retry and never a write. A running card is never paused.

12. **NEXT proves itself before it takes anything, and abort is free until the seed.** NEXT's checks: open and migrate
    a trial snapshot (deleted after), bind both slot listeners, write its slot address file, and answer its own canary (a
    `GET /v1/health` on both listeners, and a hook post to its agent listener naming a card id in `gen_held`, which
    must come back with LAST's answer, so the forward path is proved too). Any failure ends NEXT with the reason and LAST has not changed.
    A failure after LAST has marked cards ceded and before NEXT dialed the hub makes LAST clear the marks and stay the
    room.

13. **Rollback is roll forward.** After the handover the bad build is the room. Ceding cards back to LAST would need
    LAST to open a snapshot at NEXT's newer schema, which an older build cannot do. So there is no swap. The checks in
    decision 12 are the guard, and a bad NEXT is fixed by deploying a good build once LAST has retired, exactly as
    today with `atrium.prev`. A NEXT that crashes is restarted in its own slot by the room's usual start, and its
    runners survive if `pty_host` is on (phase 1).

14. **A third deploy while LAST still lives is refused, naming LAST's live cards.** Never end a card automatically,
    since a card that runs for days is doing what it was asked. The board gives each old-build card the existing end
    action. With nothing live on LAST, LAST is already gone, which is the item's "if NOTHING is running it can ALWAYS be
    redeployed immediately": the handover cedes every card and LAST exits in the same breath.

15. **Room identity and the certificate are the machine's, shared by both slots.** Both generations read the one
    `room/` directory (key, hub-signed certificate, `room.json`), so both dial as the same room with the same key, which
    is what the hub's same-key check already accepts. A re-join or a new certificate written while two generations run
    applies to each on its next dial. `ATRIUM_ROOM` stays the room name in every runner, and a runner never needs to
    know it is on an old build.

16. **NEXT's launch cap counts LAST's live cards.** The cap is per room and both generations share one machine, so a cap
    that counted only NEXT would let a machine hold ten agents during a drain. The hub reads the room's `/v1/tasks`,
    which now includes LAST's cards (decision 9), so the hub cap needs no change. Phase 5 checks it with a test.

17. **LAST retires when it holds nothing.** Every card ceded, its `relay_outbox` empty (it owes nothing to other
    rooms), no `card_resources` process row of its own. It removes its slot address file, closes its store and exits.
    NEXT sees the `last` pid gone and clears `last` from the pointer and the forwarder's table. LAST's pty host exits on
    its own idle rule.

## The picture

```
  machine m1mini                                                     hub
  +------------------------------------------------------------+     +-------------------------+
  | slot A  LAST  build 41f0  ports 7777 / 7781                |     | rooms["m1mini"]         |
  |   atrium.db   slot-a/daemon.json   pty host A               |     |   -> link gen b (NEXT)  |
  |   runners c1 c2 (ATRIUM_LOCATION = slot-a file)            |     | draining["m1mini"]      |
  |   cede loop: c3 ended -> ceded to B                        |     |   -> link gen a, closes |
  |        ^ loopback: cards in gen_held, hooks of c1 c2        |     |      when it hangs up   |
  |        |          from pre-slot runners                     |     +-----------^-------------+
  | slot B  NEXT  build 52c1  ports 7877 / 7881                |                 |
  |   slot-b/atrium.db (seeded from A)  slot-b/daemon.json     | ----------------+
  |   pty host B   runners c4 (new)                            |   the room's one link
  |   GET /v1/tasks = B's cards + A's live cards, "old build"  |
  |                                                            |
  | daemon.json (machine pointer): slot b, last = A            |
  +------------------------------------------------------------+
```

A deploy, start to finish:

```
  atrium room next
    |
    v
  NEXT starts in the free slot -> trial snapshot migrated, listeners bound, canary answered
    |                              (any failure: NEXT exits, nothing changed)
    v
  handover: LAST cedes every non-live card -> snapshot -> NEXT seeds -> LAST forwards to NEXT
    |        -> NEXT takes the pointer -> NEXT dials the hub, which drains LAST's link
    v
  LAST drains: its runners work on, each card that stops being live is ceded to NEXT
    |
    v
  LAST holds nothing -> exits                 (nothing live at the start: this happens at once)
```

## The wire

### Slot layout

| | Slot A | Slot B |
| --- | --- | --- |
| store | `defaultRoomDB()`, today's file | `<dir of defaultRoomDB()>/slot-b/atrium.db` |
| address file | `<dir of the store>/slot-a/daemon.json` | `<dir of defaultRoomDB()>/slot-b/daemon.json` |
| agent, board | the flags as given (7777, 7781 by default) | each flag's port + 100 |
| pty host | derived from the store's dir, as today | derived from `slot-b/`, so a second host |
| room dir | `roomDir()`, shared | `roomDir()`, shared |

`--slot` is `a` or `b`. Absent means `a` with today's behaviour exactly, including writing the machine file directly,
so a room started by any existing script is unchanged until phase 8 moves the scripts.

### The machine pointer

`daemon.Location` gains three fields, all omitted when empty so an older reader is unaffected:

```json
{"agent": "127.0.0.1:7877", "board": "127.0.0.1:7881", "pid": 4120, "slot": "b",
 "last": {"agent": "127.0.0.1:7777", "board": "127.0.0.1:7781", "pid": 3301, "slot": "a"}}
```

### Store additions (room)

Migration `gen_handover`, appended at the end of the room store's migrations:

```sql
CREATE TABLE IF NOT EXISTS gen_held (
    task_id TEXT PRIMARY KEY,
    alias   TEXT NOT NULL DEFAULT '',
    wire    TEXT NOT NULL DEFAULT '',
    since   TEXT NOT NULL
)
CREATE TABLE IF NOT EXISTS gen_ceded (
    task_id TEXT PRIMARY KEY,
    at      TEXT NOT NULL,
    acked   INTEGER NOT NULL DEFAULT 0
)
```

`gen_held` is written only on NEXT and lists LAST's live cards. `gen_ceded` is written only on LAST and lists cards it
has handed over (`acked` 0 while the 503 window is open, 1 once NEXT has them).

### Generation routes (room, loopback only, on the agent listener)

All refuse a caller that fails the check `shutdownAllowed` makes (`internal/daemon/shutdown.go`), so only a process of
the same user on the same machine can drive a handover.

| Route | Who answers | What it does |
| --- | --- | --- |
| `POST /v1/gen/snapshot {path, cede: [ids]}` | LAST | marks `cede` ids in `gen_ceded` with `acked 0`, then `VACUUM INTO path`. Answers `{live: [{task_id, alias, wire}], schema}` |
| `POST /v1/gen/adopt {path, ids}` | NEXT | opens a copy of `path` with `store.Open`, copies every card row for `ids` in one transaction, deletes those ids from `gen_held`. Answers `{adopted: n}` |
| `POST /v1/gen/acked {ids}` | LAST | sets `acked 1` and starts forwarding those ids to NEXT |
| `POST /v1/gen/release {ids}` | LAST | the abort: deletes those `gen_ceded` rows |
| `POST /v1/gen/retiring {next: {agent, board}}` | LAST | from now on room-level writes go to NEXT, and the link does not redial |
| `GET /v1/gen/held` | LAST | its live cards, for a refused third deploy |

### Hub (link protocol)

`hello` gains `Gen string \`json:"gen,omitempty"\``. `attached` gains `gen string`. `Hub` gains
`draining map[string]*attached`. `Attached` gains `LastCommit string \`json:"last_commit,omitempty"\``. Nothing else
in the link package changes.

## Build plan

Eight phases. Each lands alone with its tests passing, and nothing earlier breaks if a later one never lands. Phase 1
and 2 are the `pty_host` track. Phases 3 to 8 are blue-green. Phases 3, 4 and 6 touch different packages and may run in
parallel. Phase 5 needs 3 and 4. Phase 7 needs 5 and 6. Phase 8 needs 7 and 2.

Every new Go file follows the comment style of the file it is modelled on. No new dependency.

**How it is tested without the live hub.** Every phase's tests run in one process with temp directories and port 0:
the daemon tests already start two daemons on one machine (`internal/daemon/hostterm_integration_test.go` starts `d1`
then `d2` on one host), and the link tests already run a hub and rooms in process
(`internal/link/link_integration_test.go`). A blue-green test is two daemons, one per slot, in temp state dirs, with
`Options.LocationFile` set per slot, and an in-process `link.Hub` they dial. Runners are the fake runners the
supervisor tests already use. Nothing touches 7777, 7778, a live store or a live hub. Phase 2 and phase 8 are the only
ones that touch real machines, and they say which.

### Phase 1: `pty_host` stage 2, the stop that leaves runners up (`f-blue-green-1`, runtime, `internal/daemon`, `internal/cli`)

This is rolling-restart sections 3.4, 4 and 5 as decided there. Read that design first.

1. `internal/daemon/daemon.go` `stopSupervised`: skip every runner where `hostOf(r) != nil` unless the daemon was
   asked to stop runners. Add `Daemon.stopRunners atomic.Bool`, set by the stop path when asked. The skipped runners
   are not wound down, not written to the carryover and not written to the reopen list (`saveCarryover` and
   `saveReopen` get only the in-process ones). `stopShells` skips host-held shells the same way.
2. `atrium stop --runners` (the cobra command that sends the stop, found with `grep -n '"stop"' internal/cli`) sets a
   field in the shutdown request body, `{"runners": true}`, and the shutdown handler in `internal/daemon/shutdown.go`
   sets `stopRunners` from it. Plain `atrium stop` leaves host runners up (rolling-restart decision 1).
3. The short gate before a stop that leaves runners up (rolling-restart section 5): wait while `d.nctx` is busy, any
   card's `injectMu` or `pasteMu` is held, the launch lock is held, or a restart wake is mid-delivery. Poll each every
   250 ms, give up after 2 minutes and log which card held it and why, then stop anyway.
4. The re-derivation MUSTs of rolling-restart section 5, in `adoptLive` in `hostterm.go`: a reattached card is not
   idle-parkable until its runner has posted a fresh `Stop` (a `reattached` flag on the runner, cleared by the `Stop`
   hook), the reattach counts as `SessionStart` for a restart wake (call the same function the `SessionStart` hook
   calls for wakes, after the settle delay), and the card's typed line is treated as not empty until the screen model
   sees it cleared or the operator presses Enter.
5. `internal/cli/hook_permission.go`: any answer that is not a decision (refused, reset, timeout, non-2xx, a 2xx with
   no decision) is retried with the same `tool_use_id` every second for up to 30 seconds, then fails open as today
   without recording an allow.
6. G5 (rolling-restart appendix): the daemon reads `InJob` from the host's `probe` at connect and keeps it. A stop that
   would leave runners up on a daemon whose host is in a job refuses with "the pty host would die with this room's
   task, so this restart would end every agent. stop with --runners, or fix the task's job policy" unless
   `--runners` is given. `GET /v1/preflight` adds `pty_host: {on, in_job, build}`.
7. Tests, in `internal/daemon` beside `hostterm_integration_test.go`, each with `pty_host` on and a host from the
   test's own binary as those tests start it:
   - `TestStopLeavesHostRunnersUp`: start a card, stop the daemon, the runner's pid is alive, a second daemon
     reattaches it and the card is still `running`, never resumed, with no reopen row.
   - `TestStopRunnersEndsHostRunners`: the same with `runners: true`, the runner ends and is filed once.
   - `TestReattachedCardIsNotIdleParkableUntilStop`, `TestReattachCountsAsStartForAWake`,
     `TestReattachedLineIsNotEmpty`.
   - `TestStopRefusedWhenHostInJob`: a fake probe answering `in_job` refuses, `runners: true` proceeds.
   - `TestStopWaitsForANewContextCycle`: a busy `d.nctx` holds the stop until it ends.
   - In `internal/cli`: `TestPermissionHookRetriesThroughARestart`: a server that resets twice then decides is
     answered by its decision, and one that never answers fails open after the bound (the bound is a variable the test
     sets to 2 seconds).

   Run: `go test ./internal/daemon -run 'TestStop|TestReattach' -count=1` and
   `go test ./internal/cli -run TestPermissionHook -count=1`

### Phase 2: `pty_host` on, everywhere (`f-blue-green-2`, fabric with the operator, real machines)

1. Close the open gates in rolling-restart's appendix, in this order, each written into that appendix as passed or
   failed with the evidence: systemd on cdzrok (check 8, steps 1 to 4), launchd on m1mini (`launchctl bootout` of a
   LaunchAgent running the host), and the Task Scheduler job on a Windows room whose task runs as a user (check 6).
2. For each live room: if its gate passed, set `pty_host` on (`POST /v1/settings {"pty_host": true}` through the hub
   with `X-Atrium-Room`), then run `atrium-deploy.ps1 -Rooms <room>` while one test card is running. Pass when the test
   card is still running afterwards with the same pid and was not resumed, and `/v1/preflight` says `in_job: false`.
   If a gate failed, leave that room off and write why in the appendix.
3. `atrium-deploy.ps1` and `deploy-batch.ps1`: skip the deploy hold for a room whose preflight says `pty_host.on` and
   not `in_job`, since its runners survive. Keep it for the rest.

   Done when sg4-control, claude-sg4, sg3 and m1mini each pass step 2, or the appendix names the gate that stops one.

### Phase 3: slots (`f-blue-green-3`, runtime, `internal/cli`, `internal/daemon`)

1. `internal/cli/roomrun.go`: a `--slot` flag on `atrium room`, values `a` or `b`, default empty. Pass it to `runRoom`
   and to `roomLaunch` so a hub-asked restart keeps the slot.
2. New `internal/cli/slot.go` with `func slotLayout(slot, db, human, agent string) (dbOut, humanOut, agentOut,
   locFile string, err error)`: empty slot returns its inputs and an empty `locFile`. `a` returns the inputs and
   `<dir of db>/slot-a/daemon.json`. `b` returns `<dir of db>/slot-b/atrium.db`, both addresses with the port + 100
   (parse with `net.SplitHostPort`, an unparseable address is an error), and `<dir of db>/slot-b/daemon.json`. Any
   other value is an error naming `a` and `b`. Create the slot directory with mode 0700.
3. `runRoom`: with a slot, do not set `ATRIUM_LOCATION` from `roomLocationEnv`. Set `daemon.Options.LocationFile` to
   the slot's file instead, so `writeLocation` writes it and `tellWhereIAm` hands it to every runner. Add
   `Options.Slot string` and put it in `Location` as `slot`.
4. `internal/daemon/whereami.go`: add `Location.Slot` and `Location.Last *Location`, both `omitempty`. Add
   `func (d *Daemon) TakeMachinePointer(last *Location) error` that writes the machine file
   (`DefaultLocationPath`) with this daemon's location plus `last`, through `putLocation`, with no `takingOver`
   check. Add `func (d *Daemon) ClearLast() error` that rewrites it with `last` removed when this daemon owns it.
   `removeLocation` also removes the slot file.
5. `link.Room` gains `Gen string`, sent in the hello as `gen` (the field itself lands in phase 6, so here add it to
   `hello` in `internal/link/protocol.go` with `omitempty` and set it from the slot). An older hub ignores it.
6. `atrium status` (and `atrium room status` if it exists) prints the machine pointer's `slot` and, when `last` is
   set, the last generation's addresses and pid.
7. Tests:
   - `internal/cli`: `TestSlotLayout` (empty, `a`, `b`, a bad slot, an unparseable address).
   - `internal/daemon`: `TestSlotRunnerGetsSlotLocation`: a daemon with a slot launches a fake runner whose
     environment has `ATRIUM_LOCATION` equal to the slot file. `TestTakeMachinePointerWritesLast`: two daemons, the
     second takes the pointer with the first as `last`, the file names both, `ClearLast` removes `last`.
     `TestRunnerOfSlotAKeepsReportingToSlotA`: daemon A launches a fake runner, daemon B takes the pointer, the
     runner's hook (posted to the address in its own `ATRIUM_LOCATION`) arrives at A.

   Run: `go test ./internal/cli -run TestSlot -count=1` and
   `go test ./internal/daemon -run 'TestSlot|TestTakeMachinePointer|TestRunnerOfSlotA' -count=1`

### Phase 4: cede, in the store (`f-blue-green-4`, runtime, `internal/store`)

1. The migration `gen_handover` above, appended at the end of the store's migrations.
2. New `internal/store/gen.go`:
   - `var CardTables []CardTable`, where `CardTable{Name, Column string}` names a table and the column that holds its
     card id. `var CardExempt []string` names the tables that are machine-wide and copied only at the seed:
     `schema_migration`, `setting`, `harness`, `provider`, `provider_repo`, `recogniser`, `perm_rule`, `term_theme`,
     `pr_recipe`, `source`, `gen_held`, `gen_ceded`. Fill `CardTables` by running the guard test below, which prints
     every table it finds unlisted, and put each printed table in `CardTables` with the column the test names. A table
     with no card column that is not in `CardExempt` also goes in `CardExempt`, with a one-line comment why.
   - `func (s *Store) Snapshot(path string) error`: `VACUUM INTO ?` with a path that must not exist.
   - `func (s *Store) LiveCards() ([]HeldCard, error)`: the store half of decision 6, the cards whose status is
     `running`, `needs-input` or `needs-permission`, or that a `restart_wake` row or a `card_resources` row of kind
     `proc`, `port` or `overlay` names. `HeldCard{TaskID, Alias, Wire string}`. The daemon adds the in-memory half.
   - `func (s *Store) Cede(ids []string) error`, `func (s *Store) Acked(ids []string) error`,
     `func (s *Store) Release(ids []string) error`, `func (s *Store) Ceded(id string) (acked, ok bool)`,
     `func (s *Store) CededAll() ([]string, error)`, on `gen_ceded`.
   - `func Adopt(dst *Store, snapshot string, ids []string) (int, error)`: copy `snapshot` to a temp file beside it,
     `Open` the copy (every migration runs on it, and a failure returns that error with nothing written to `dst`),
     close it, then on `dst` in one transaction `ATTACH` the copy, and for each `CardTables` entry
     `INSERT OR REPLACE INTO main.<t> SELECT * FROM src.<t> WHERE <col> IN (<ids>)` with the column list read from
     `PRAGMA table_info` of `main.<t>` on both sides of the select, then `DELETE FROM gen_held WHERE task_id IN (...)`,
     commit, `DETACH`, remove the copy. Running it twice with the same ids gives the same rows.
   - `func Seed(dstPath, snapshot string, live []HeldCard) error`: copy the snapshot to `dstPath`, `Open` it, and in
     one transaction delete every `CardTables` row for the `live` ids and insert one `gen_held` row each. A `dstPath`
     that exists is an error, so a seed never overwrites a store.
   - `func (s *Store) Held() ([]HeldCard, error)` and `func (s *Store) IsHeld(id string) bool` on `gen_held`.
3. The alias and wire-name allocators (`internal/store/alias.go` and wherever a wire name is made unique) also treat a
   name in `gen_held` as taken.
4. Tests, new `internal/store/gen_integration_test.go`:
   - `TestCardTablesCoverEverySchemaTable` (the guard): read every table from `sqlite_master` of a freshly opened
     store, and for each that has a column named `task_id`, `card_id` or `card` and is in neither `CardTables` nor
     `CardExempt`, fail with `table <t> has card column <c>: add it to CardTables or CardExempt`. Also fail for a table
     with none of those columns that is in neither list.
   - `TestSeedDropsLiveAndHoldsTheirNames`: a store with done, parked and running cards, snapshot, seed. The done and
     parked cards are present with all their events and messages, the running one is absent and is in `gen_held`.
   - `TestAdoptIsIdempotent`, `TestAdoptMigratesTheSnapshot` (a snapshot from a store with the last migration removed
     from the list is adopted at the full schema), `TestAdoptFailingMigrationWritesNothing` (a migration made to fail
     leaves `dst` and the snapshot byte for byte as they were).
   - `TestAliasAvoidsHeldNames`.
   - `TestCedeAckRelease`.

   Run: `go test ./internal/store -run 'TestCardTables|TestSeed|TestAdopt|TestAlias|TestCede' -count=1`

### Phase 5: the front, NEXT answers for LAST (`f-blue-green-5`, runtime, `internal/daemon`, `internal/api`)

1. New `internal/daemon/gen.go` holding the generation state: `type genPeer struct{ agent, board string; pid int;
   commit string }`, `d.last atomic.Pointer[genPeer]` (set on NEXT), `d.next atomic.Pointer[genPeer]` (set on LAST
   once retiring), and `d.retiring atomic.Bool`.
2. New `internal/daemon/genforward.go`, a middleware wrapped around both the agent mux and `d.BoardHandler()` where
   `agentSrv` and `humanSrv` are built (`daemon.go`, near line 1168):
   - On NEXT (`d.last` set): find the card a request names. Path `/v1/tasks/{id}/...` (use the same segment rule as
     `cardIDIn` in `internal/link`), a launch body's `task_id`, or for a `POST` on the agent listener a JSON body's
     `task_id` (read at most 1 MiB and put it back, as `launchCardIn` in `internal/link/cardroute.go` does). If
     `d.st.IsHeld(id)`, reverse-proxy the request with `httputil.ReverseProxy` to LAST's same listener (agent to
     agent, board to board), which carries a websocket upgrade unchanged. A launch forwarded this way that LAST answers
     with 409 and `{"ceded": true}` is run again locally, once.
   - On LAST (`d.next` set): a request naming a card in `gen_ceded` with `acked 1`, and every `POST /v1/launch`
     without a `task_id`, goes to NEXT the same way. A request naming a card in `gen_ceded` with `acked 0` answers 503
     with `Retry-After: 2` and `{"error": "this card is moving to the new build, try again in a moment"}`. Settings
     writes answer 409 "this is the old build, change settings on the current one".
   - Everything else is untouched.
3. `GET /v1/tasks` on NEXT: after the local list, fetch LAST's `GET /v1/tasks` over loopback (2 second timeout, cached
   for 2 seconds), keep only ids in `gen_held`, set `generation: "last"` and `build` on each, append. A failed fetch
   appends nothing and logs once a minute. Add `Generation string \`json:"generation,omitempty"\`` and
   `Build string \`json:"build,omitempty"\`` to the task JSON in `internal/api`.
4. Events: on NEXT, a goroutine holds `GET /v1/events` on LAST's board over loopback, reconnecting every 2 seconds
   while `d.last` is set, and republishes each event whose card id is in `gen_held` through `d.ap.Broadcast` with
   the two fields added to the task payload.
5. Says: the local say path (`sayGate` and `forwardMoved` in `move.go` show the shape) delivers a say to a card in
   `gen_held` (on NEXT) or in `gen_ceded` with `acked 1` (on LAST) by posting it to the other generation's agent
   listener, the way `forwardMoved` posts across rooms.
6. Live, the daemon half: `func (d *Daemon) liveCards() ([]store.HeldCard, error)` is `d.st.LiveCards()` plus every
   card `d.sup.all()` holds a runner or shell for, plus every card `d.nctx` is busy on.
7. The cede loop on LAST, `func (d *Daemon) cedeLoop(ctx)`, every 30 seconds while `d.retiring`: the held cards that
   `liveCards` no longer lists are ceded with `POST /v1/gen/snapshot` on itself, `POST /v1/gen/adopt` on NEXT and
   `Acked`. A failure releases them and retries on the next tick.
8. The routes in "Generation routes" above, registered on the agent listener in a new `internal/daemon/genroutes.go`.
9. Tests, new `internal/daemon/gen_integration_test.go`, two daemons A and B in temp dirs with slot location files,
   B seeded from A, `d.last` set on B and `d.next` on A:
   - `TestFrontForwardsAHeldCardsRequest`: `GET /v1/tasks/<A's live id>` on B answers A's card.
   - `TestFrontForwardsAHook`: a hook body with A's live id posted to B's agent listener arrives at A.
   - `TestFrontForwardsTheTerminal`: a websocket attach to A's card through B replays A's ring.
   - `TestFrontListsBothWithGeneration`: B's `/v1/tasks` has B's cards and A's live ones with `generation: "last"`.
   - `TestFrontRelaysEvents`: a status change on A's card appears on B's event stream.
   - `TestLastForwardsNewLaunchesToNext`, `TestLastAnswers503WhileCeding`, `TestLastRefusesSettings`.
   - `TestResumeOfACededCardRunsOnNext`: A's card stops being live, a launch naming it through B ends with the card
     running on B under the same id and alias.
   - `TestSayCrossesGenerations` both ways.
   - `TestLaunchCapCountsBothGenerations`: a hub in process with a cap of 2, one live card on A and one on B, a third
     launch through the hub is refused.

   Run: `go test ./internal/daemon -run 'TestFront|TestLast|TestResumeOfACeded|TestSayCrosses|TestLaunchCapCounts'
   -count=1`

### Phase 6: the hub drains the old link (`f-blue-green-6`, fabric, `internal/link`, board)

1. `internal/link/protocol.go`: `hello.Gen` (if phase 3 has not added it). `internal/link/hub.go`: `attached.gen`,
   set from `hi.Gen` in `control`. `Hub.draining map[string]*attached`, made in `NewHub`.
2. In `control`, where `taken` is handled today: if `taken && old.key == a.key && old.gen != "" && a.gen != "" &&
   old.gen != a.gen`, do not call `old.close`. Instead put `old` in `h.draining[keyOf(name)]` (closing any link already
   there), log `room %q: generation %s drains, %s is current`, and start a goroutine that waits on `old.done` or 2
   minutes, then calls `old.close("its generation was replaced")` and deletes it from `draining` if it is still the
   one there. Otherwise behave exactly as today. `forget` is unchanged: it only removes `h.rooms` entries that are
   still the link it was given.
3. `Rooms()`: set `LastCommit` on a room's entry from `h.draining[key].commit` when one is there.
4. `link.Room` (the room side): a `Retiring func() bool`. When it answers true, the dial loop does not redial after the
   link closes. The daemon sets it to `d.retiring.Load`.
5. The board: where a card's room tag is drawn (find it with `grep -rn "room" internal/api/web/m/js/card.js` and the
   same in the desktop board's card renderer), draw a chip `old build <build>` when the task has
   `generation === "last"`, with the title "this card runs on the previous build of this room until it ends". The
   room row draws `LastCommit` beside the commit when present. `scripts/test-board-headless.js`: one mock card with
   `generation: "last"`, and a check that the chip is drawn.
6. Tests, in `internal/link` beside `link_integration_test.go`:
   - `TestSecondGenerationDrainsTheFirst`: a room attaches with gen `a`, a request is held open on it, the same key
     attaches with gen `b`, a new request goes to `b`, the held one finishes on `a`, and `a` closes after it hangs up.
   - `TestSameGenerationStillReplaces` and `TestNoGenerationStillReplaces` (today's behaviour).
   - `TestDifferentKeyStillRefusedWithAGen`.
   - `TestRoomsReportsLastCommit`.
   - `TestRetiringRoomDoesNotRedial`.

   Run: `go test ./internal/link -run 'TestSecondGeneration|TestSameGeneration|TestNoGeneration|TestDifferentKey|
   TestRoomsReportsLastCommit|TestRetiringRoom' -count=1` and, where node is installed,
   `node scripts/test-board-headless.js`

### Phase 7: `atrium room next`, the lifecycle (`f-blue-green-7`, fabric with runtime, `internal/cli`, `internal/daemon`)

1. New `internal/cli/roomnext.go`, `atrium room next [--binary <path>] [--dry-run]`:
   - Read the machine pointer. Current slot is its `slot`, or `a` when absent. Next is the other.
   - If the next slot's address file names a live pid, refuse: ask that generation `GET /v1/gen/held` and print
     "the last generation still runs <alias or id>, ... end them or wait, then deploy again". Exit 3.
   - Start `<binary> room --slot <next> --next-of <current agent address> --detach` with the same `--dir`, `--db` and
     address flags the current one was started with (read from the pointer's `room_dir`, `db`, `agent_listen` and
     `board_listen`). Wait up to 3 minutes for the next slot's file to name a live pid that answers
     `GET /v1/health`, and print each step NEXT logs.
2. `runRoom` with `--next-of`: before `daemon.New`, run the checks of decision 12 against LAST (trial snapshot through
   `POST /v1/gen/snapshot` with an empty `cede`, `store.Adopt` dry run of the trial by opening a copy, then delete it).
   Then the handover of decision 11, in order: `POST /v1/gen/snapshot` with `cede` set to every non-live card (LAST
   computes the list as `all cards minus liveCards`, ignore what NEXT sends and use LAST's own list, sent back as
   `ceded`), `store.Seed` into the slot's store path, `daemon.New` on it, bind, the canary, `POST /v1/gen/acked`,
   `POST /v1/gen/retiring`, `TakeMachinePointer(last)`, then start the link with `Gen` set. Any failure before
   `retiring` calls `POST /v1/gen/release` for the ceded ids, deletes the seeded store, and exits non-zero with the
   reason.
3. LAST, on `retiring`: `d.retiring` true, `d.next` set, the cede loop started, and a retirement check every 30 seconds:
   no card in `liveCards` that is not ceded, `relay_outbox` empty, no own `card_resources` process row. Then remove the
   slot file, close the store, exit 0. NEXT, seeing the `last` pid gone (checked on the same 30 second tick), calls
   `ClearLast` and sets `d.last` to nil, and from then on `gen_held` is empty.
4. The hub's `restart_atrium` path (`OnRestart` in `roomrun.go`) on a room started with `--slot` runs the same
   sequence as `atrium room next` with the running binary, instead of a stop and start.
5. Tests, new `internal/cli/roomnext_integration_test.go`, with the test binary re-exec'd as the room the way
   `roomrestart_integration_test.go` does, an in-process hub, temp dirs, port 0 made into fixed test ports:
   - `TestNextWithNothingRunningRetiresAtOnce`: LAST holds only done cards, `room next` ends with NEXT current on the
     hub, LAST exited, every card on NEXT with the same ids.
   - `TestNextLeavesRunningCardsOnLast`: three running fake runners on LAST stay on LAST with the same pids, a new
     launch through the hub lands on NEXT, one LAST card ends and is on NEXT within one cede tick, and when the last
     one ends LAST exits.
   - `TestNextWithAFailingMigrationChangesNothing`: a NEXT build with a migration made to fail exits with the reason,
     LAST is still the room, no card is ceded.
   - `TestThirdDeployIsRefusedWithNames`.
   - `TestBadCanaryReleasesTheCededCards`.

   Run: `go test ./internal/cli -run 'TestNext|TestThirdDeploy|TestBadCanary' -count=1`

### Phase 8: the scripts (`f-blue-green-8`, fabric, `scripts/live`, real machines)

1. `scripts/live/atrium-deploy.ps1`: for a remote room, the swap no longer stops the room. It copies the binary to
   `atrium.next[.exe]` and runs `atrium room next --binary <that path>`, and on exit 0 renames it over `atrium`
   keeping `atrium.prev`. The wait for port 7781 to be free goes. Exit 3 is reported as
   `failed - the last generation still runs <names>`. The hub and sg4-control follow the same change in
   `deploy-batch.ps1` and the sg4-control block.
2. The deploy hold is skipped for a room on slots, since nothing is stopped.
3. `scripts/live/README.md`: the deploy section rewritten for slots, with the two port pairs.
4. The bootstrap, once per room: the first deploy onto a build with phases 3 to 7 is an ordinary restart with
   `--slot a` added to the room's start line (`provision-room.ps1` and the remote swap lines). With phase 2 on that
   room it loses nothing. Every later deploy is `room next`.

   Done when, on m1mini and then sg3: a deploy while three cards run needs no wait and leaves all three running on the
   old build with "old build" on the board, a card started during the deploy lands on the new build with its prompt
   typed, a deploy while nothing runs takes effect at once, and the old build exits when its last card ends.

## How this fits room handoff

Room handoff moves a running card across machines, which needs a freeze, a capture, a branch and transcript carry, a
successor with a new id and a `moved_to` chain. This design never moves a running card: a card on LAST ends or parks
where it is, and only then is it ceded, under the same id. So the heavy half of handoff is not on this path. What they
share:

- The cede's table list (`CardTables`, phase 4) is the inventory of a card's rows that `f-card-move-inventory` needs.
  Built once here, with its guard test.
- A same-machine `atrium move` between two rooms (the hand-run case of r-new-move-card) can later be a cede, since the
  disk and paths are the same. Not built here.
- `followMoved` stays handoff's. Nothing here writes `moved_to`.

## Out of scope

- A swap back to LAST after the handover (decision 13).
- LAST taking the room back when NEXT crashes. NEXT is restarted in its own slot by the room's usual start.
- Moving a running card to NEXT. A card that never ends keeps LAST alive, and the board shows which.
- More than two generations.
- The hub's own blue-green. The hub holds no work and restarts in seconds.
- The cross-machine half of `f-card-move-inventory`, which stays with room handoff.

## Risks

- **Cede is the new thing that can lose a card.** It is a database copy with a migration in the middle. One
  transaction, idempotent by id, the source row kept until NEXT acknowledges, and a guard test on the table list. A
  card is in two stores for a moment and never in neither.
- **The front is in NEXT's process.** If NEXT is down, LAST's cards are unreachable from the hub until NEXT is back,
  though LAST's runners and LAST's own board on its slot port keep working.
- **Two generations use twice the memory and idle CPU** for the length of a drain.
- **A schema rename breaks `INSERT ... SELECT *` with matched columns.** Adopt reads the column list from `main`, and a
  rename migrates the snapshot copy too, so both sides match. A migration that drops a card table must also drop it
  from `CardTables`, and the guard test catches the reverse.
- **Disk.** A snapshot per cede batch, deleted after adopt. On a large store the 30 second cede tick may be too often,
  and the tick is a constant to raise if a snapshot takes more than a few seconds.
