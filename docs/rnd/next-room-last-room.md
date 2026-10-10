# The next room and the last room: deploy beside the old build, never over it

Status: design, for clint's approval. Written 2026-10-10 by a worker on `claude/next-room-design`. Nothing is built.
The item is backlog `f-next-room-last-room` (HIGH, clint 2026-10-08).

## 0. The answer

Give every machine two **slots**, A and B, and let a room run in either. A deploy starts the new build in the free slot
as NEXT, beside LAST, and never touches LAST. Each slot has its own ports, its own address file, its own database and
its own pty host, and the two share only the room's key and its name. When NEXT has attached and passed its own checks
the hub **flips**: every new launch, resume and new card goes to NEXT, and `room: m1mini` means whichever generation is
current. LAST keeps every runner it has. Nothing is moved, resumed or typed into. As each one ends or parks, LAST's
work is done, and when it holds nothing live it hands its remaining cards to NEXT and exits.

Three decisions carry the design:

1. **A runner's hooks follow the generation that launched it, for life.** Each runner is launched with
   `ATRIUM_LOCATION` naming its own slot's address file, not the machine's. The machine's file becomes a pointer that
   the current generation owns (section 2.2).
2. **One store per generation.** Two processes on one database is a halt waiting to happen and two builds on one
   schema is worse (section 2.3). The cost is a way to carry cards from one store to the next. Because the two stores
   are on one machine and LAST never moves a running card, that carry is an **adoption by the same id**, which is far
   cheaper than room handoff's successor card (section 4).
3. **Turn `pty_host` on first.** It gives most of the pain relief in a fraction of the work, and blue-green is then
   the second layer for what it cannot do: rollback, a gap-free flip and a migration that can fail without taking the
   room down (section 3).

## 1. Today, in one paragraph

A machine runs one room, `atrium room` (`internal/cli/roomrun.go`, `runRoom`), and a deploy replaces it. The room binds
the fixed ports from `internal/cli/atrium_defaults.go` (agents 7777, its own board 7781) and writes its address to the
machine's one location file (`daemon.DefaultLocationPath`, `writeLocation` in `internal/daemon/whereami.go`). It also
sets `ATRIUM_LOCATION` in its own environment, which every runner inherits, so a runner's hooks always find "the file",
and `writeLocation` refuses to take that file while another live pid owns it (`takingOver`). The hub keys rooms by
name, and a second attach under a name from the same key evicts the first (`internal/link/hub.go`,
`h.rooms[keyOf(name)]` and `old.close("replaced by a newer connection ...")`), and `RoomCheckIn`
(`internal/daemon/rooms.go`) replaces that room's whole card list on every heartbeat. The room owns one sqlite store whose open runs every migration and refuses to
start on failure, and whose first non-contention error halts the process (`internal/store/store.go` header, tiers one
and three). A deploy (`scripts/live/deploy-ready.ps1`, `Start-Room` in `live-common.ps1`) stops that room and starts the
new one, and it waits for the board to be idle first because every supervised runner dies with the room unless
`pty_host` is on (`internal/daemon/hostterm.go`, `docs/rnd/rolling-restart-design.md`). `pty_host` is built and off on
every live room. Moving a card between rooms is design only (`docs/rnd/room-handoff-design.md`, M1 partly built).

## 2. The recommended design

### 2.1 Slots and generations

```
  machine m1mini                                        hub
  +-----------------------------------------------+     +----------------------+
  | slot A  (LAST)  build 41  ports 7777 / 7781   |     | room "m1mini"        |
  |   db room.db   addr slot-a/daemon.json        | <-> |   gen A  old build   |
  |   pty host a   runners: c1 c2 c3 (running)    |     |   gen B  current     |
  |                                               |     |                      |
  | slot B  (NEXT)  build 42  ports 7877 / 7881   | <-> | new work  -> gen B   |
  |   db room-b.db addr slot-b/daemon.json        |     | card ~id  -> its gen |
  |   pty host b   runners: c4 (new)              |     +----------------------+
  |                                               |
  | daemon.json (the machine pointer) -> slot B   |
  +-----------------------------------------------+
```

- A **slot** is a fixed layout: ports, a directory holding the database, the address file, the logs and the pty host's
  socket. Slot A is today's layout exactly, so a machine that has never heard of slots is already in slot A with no
  migration. Slot B is the same layout in `<state>/room-b/` with both ports shifted by +100 (7877 and 7881). A fixed
  shift is chosen over a random port because the escape-hatch board at `:7781` and every script that matches on a port
  keep meaning something. The shift is +100 rather than -10 so a second room on the machine that already uses 7787 and
  7791 (sg4-control) lands on 7887 and 7891 and not on 7777.
- A **generation** is the daemon that holds a slot. The hello it sends the hub gains `gen` (the slot and the build
  commit) and `state`: `warming`, `current` or `retiring`. A deploy always takes the free slot. If both are held, the
  deploy refuses and names the cards that keep LAST alive (open question 3).
- The room's key, certificate and name are shared. Both generations dial as the same room with the same key, which is
  what the hub's same-key check already accepts, and the second one no longer evicts the first.

### 2.2 How a runner's hook finds the generation that owns it

Today every runner inherits one `ATRIUM_LOCATION`, and that is why a second room on a machine used to steal hooks
(`LocationPath` says so at length). The fix is to stop sharing it:

- **Each generation writes its own address file** (`slot-a/daemon.json`, `slot-b/daemon.json`) and sets
  `ATRIUM_LOCATION` to it in the environment of every runner it launches. `tellWhereIAm` in `whereami.go` already does
  exactly this when a daemon has its own `LocationFile`, so the supervised-runner path exists. It becomes the rule
  rather than the exception. A runner's hook reads that file on every fire, so it reaches LAST for as long as LAST
  lives and can never be captured by NEXT. A runner that resumes later on the same slot reads the same file.
- **The machine's file is a pointer, owned by the current generation.** It is what a session atrium did not start, the
  CLI, `atrium join` and the control MCP read (`ReadLocation`). `takingOver` refuses to take it while another live pid
  is recorded, which is right for an accident and wrong for a flip. The flip is an explicit act: NEXT writes the
  pointer with a `take_default` option, atomically, and only after the hub has flipped. The pointer also records the
  other generation's address as `last`, so a tool can find both.
- **The same-box shared copy** (`ATRIUM_SHARED_LOCATION`) is not written by a room, isolated or not (`roomLocationEnv`),
  so nothing changes there.
- **Runners that are already running when the first slotted deploy happens** carry the machine's file, not a slot
  file, and the environment of a live process cannot be changed. When NEXT takes the pointer their hooks would arrive at
  a room that does not know their card. NEXT therefore forwards, for one generation only: a hook or permission post on
  its agent listener that names an `ATRIUM_TASK_ID` NEXT's store does not hold goes to the `last` address in the
  pointer, long-poll included. It is a few dozen lines, it only ever fires for pre-slot runners, and it is removed once
  every live runner carries a slot file (phase 1 builds it, a later release deletes it).
- **Permission requests in flight at the flip** are not a gap, which is something the pty-host restart cannot say.
  LAST keeps answering the requests it has, from its own store, on its own listener.
- `ATRIUM_ROOM` stays the machine's room name in every runner. The control MCP scopes by room name and the hub then
  decides the generation, so a runner needs no idea that it is on an old build.

### 2.3 One store per generation

**Recommended: one store per generation.** The two rules that decide it, both from `internal/store/store.go`:

- **The halt rule.** The first error that is not contention halts the store and closes the agent listener so every
  session parks. That posture assumes one writer. LAST and NEXT sharing a file means each process meets the other's
  locks, the other's checkpoints and, worse, the other's schema, and any resulting error halts one of them while
  agents are mid-turn. It also puts two reapers, two sweeps and two idle clocks on one set of rows, which
  `rolling-restart-design.md` section 8 already ruled out for the same reason.
- **Migrations are forward only and run at open.** If NEXT's build carries migration 0N+1, then the moment NEXT opens
  the shared file LAST's queries meet a schema it was not built for. A per-generation store means NEXT migrates a file
  LAST has never seen and LAST's file is never opened by a newer build while LAST is alive. A migration that fails is
  tier one: NEXT refuses to start, LAST is untouched and still serving, and the deploy says so. Today the same failure
  leaves the room down.

What NEXT's store starts with, and how cards move between the stores, is one primitive, **adopt**:

1. LAST takes a consistent snapshot of its own file (`VACUUM INTO`, a read, which never blocks its writers for long).
2. NEXT copies the snapshot, opens the copy with its own `store.Open` (so every migration runs on the copy and a failure
   is tier one on a throwaway file), and for a named set of card ids copies every row that belongs to each card into
   its own store in one transaction, idempotent by card id.
3. LAST marks those ids `ceded` after NEXT acknowledges and stops reporting them. A ceded id is never reported by two
   generations, so the hub's merge never sees one card twice.

Which table rows belong to a card is a list kept in one place with a guard test that fails when a new table with a card
key is added and is in neither the adopt list nor an explicit exempt list. This is the same list
`f-card-move-inventory` needs, so it is built once (section 4).

Adopt runs twice in a deploy:

- **At seed.** Every card that is not live (done, backlog, review, the settings and tables that are not card rows) is
  adopted, so NEXT opens with the board's whole history and LAST keeps only the cards it is actually running. "Live"
  means a running or needs-input status, a runner process, a new-context cycle in flight or a restart wake owed.
- **At retirement.** When LAST holds nothing live, it takes a final snapshot after its writers are quiet, NEXT adopts
  the remainder (the cards that ended on LAST since the seed, and the parked ones), and LAST exits. This is also how a
  **parked card on LAST "moves to NEXT on resume"**, with one refinement: it moves at retirement, or sooner when it is
  resumed, so a card that stays parked for a week does not keep LAST alive. Resume on LAST of a parked card runs a
  one-card adopt first and then the ordinary resume on NEXT.

Settings are copied at seed and are NEXT's from then on. A setting changed on the board after the flip goes to NEXT,
the current generation, and LAST does not see it. That is the correct behaviour for a generation that is on its way
out and the board says so on LAST's rows.

### 2.4 The hub and the board

- `h.rooms` keeps its key (the room name) but each entry holds up to two attached links by `gen`. The eviction rule
  becomes: the same key and the same `gen` replaces the older link (a reconnect), the same key and a different `gen`
  coexists. A third gen is refused with the room's current two named.
- **Routing.** A request that names a card id (`room~id`, `internal/link/proxy.go`) goes to the generation whose last
  report holds that card. A request that names only the room goes to the current generation. A launch, a resume of a
  card whose rows are in NEXT, a new card and a cross-room `atrium_say` to a bare `name@m1mini` all follow that rule.
  Two generations never hold the same id (section 2.3), so there is no tie. A say to a card on LAST is delivered by
  LAST. This is what `room~id` stability needs and it needs no `moved_to` chain, because ids do not change.
- **Nothing routes to NEXT until it is `current`.** NEXT's hello is `warming`, and the hub flips it only when NEXT
  says it passed its own checks (store open and migrated, both listeners bound, its address file written, a
  self-posted canary hook answered). Until then new work still goes to LAST, so a NEXT that never comes up costs
  nothing. LAST is told `retiring` at the flip and refuses launches itself with "this generation is retiring, use
  `<NEXT>`" as a second line of defence, reusing the `startsNothing` idea.
- **The board draws one room.** The room's row is the union of both generations' cards. LAST's cards carry an
  "old build" mark with the build commit and the age of the generation. The room row shows both builds while both
  live. Each generation's heartbeat still replaces only its own cards, which is why a union is safe: `RoomCheckIn`
  already keys the replace on one report, and the hub now keys the cache on `(room, gen)`.
- **The room-wide caps** (`launch cap of 5`, handoff check 9) count both generations on the machine. A cap that
  counted only NEXT would let a machine hold ten agents during a deploy.

### 2.5 A deploy, start to finish

```
  deploy start      NEXT starts in the free slot, db seeded by adopt, warming
       |
       v
  NEXT attaches --> checks pass --> hub flips: NEXT current, LAST retiring, pointer moves
       |                                       |
       |                                       v
       |                            LAST drains: runs its cards, refuses launches
       |                                       |
       |                    each card on LAST ends or parks (or an operator ends it)
       |                                       |
       v                                       v
  NEXT is the room            LAST holds nothing live: final snapshot, NEXT adopts the rest, LAST exits
```

- **Nothing running on the room is an immediate redeploy.** With no live card, the seed adopts everything, the flip
  happens, LAST has nothing to drain and retires in the same breath. That is the sentence in the item: if nothing is
  running it can always be redeployed immediately. There is no idle wait and `deploy-ready`'s whole gate goes away
  for rooms on slots.
- **LAST's retirement waits for more than cards.** Inventory kinds that live in LAST's process tree (`proc`,
  `overlay`, `port` held by a process LAST started, `docs/rnd/card-lifecycle-design.md` section 6) die when LAST exits
  unless they are also pty hosted. They count as live for LAST until freed. Until card inventory is built this is
  empty, and the retirement check reads the inventory table when it exists.
- **Rollback is the same operation reversed.** NEXT is bad. Before the flip, kill it and nothing has changed. After the
  flip, deploy the good build into the free slot as the new NEXT. The bad generation becomes LAST and drains. No
  special path exists, and the one thing that cannot be undone, a migration, only ever ran on NEXT's own file.
- **The pty host.** Each slot has its own pty host (its socket is derived from the state dir, which differs per slot,
  `rolling-restart-design.md` section 3.2). It is optional here. LAST does not need a host to keep its runners, since
  LAST does not stop. The host is for the day a generation crashes, and for the bootstrap deploy that goes from the
  unslotted room to slot A (section 6, phase 1).

### 2.6 What stays the same on purpose

Card ids, handles, `name@room` addresses, the hub's view of the room's identity and its key, `report_to` links, the
git sync (one clone per machine is shared by both generations, both being the same machine's room) and the board's
terminal attach (an attach names a card and the hub sends it to that card's generation). Nothing that a sender, a
script or a card holds becomes stale across a deploy, which is the property the rolling-restart design called out and
room handoff cannot have across machines.

## 3. Does `pty_host` everywhere give most of this sooner

**Yes, most of the pain, and it should go first.** The pain in the item is "we deploy so often and every deploy waits
for the room to be idle or kills agents". With `pty_host` on:

- a daemon restart does not end any runner, so no deploy waits for idle, none resumes anything and none loses a
  half-written input line (`rolling-restart-design.md` section 4),
- the restart is a few seconds of gap rather than a gate on every card,
- it is built, tested and gated behind a setting, so it can be on this week.

What stands between it and "on everywhere" is not design. It is the gates the spike left open
(`rolling-restart-design.md` Appendix, "Open gates"): the Windows task's job object must allow breakaway (G5), systemd
`KillMode`, launchd on m1mini, and a daemon that refuses a restart that would end an in-job host.

**What blue-green adds beyond it:**

- **Hooks and the permission long-poll.** A pty host restart has a gap of a few seconds, bridged by a 30 second retry
  that needs f-006. A flip has none, because LAST keeps answering what it was asked.
- **A bad build.** After a host restart the new daemon already is the room and rollback is another restart. NEXT is
  checked before it takes anything and rollback is a flip.
- **A failed migration.** On one store it is tier one and the room is down until fixed. On NEXT's own file LAST keeps
  serving. A forward-only schema change cannot be rolled back at all in the first case, and in the second LAST is
  kept until the new build is trusted.
- **Upgrading the host itself.** The first needs the rare per-card idle move (section 6 of that design). A new slot
  builds its own host and needs nothing.
- **In-memory state** (idle marks, activity counts, wake start signals). The first must re-derive it at reattach
  (section 5 of that design). Under slots LAST holds it and it is never lost.
- **Runners survive.** Both, though the first needs a host that got out of the job object and slots need no host.
- **Cards that never idle.** Not a problem for the first. Under slots they keep LAST alive, the one cost (question 3).
- **Cost.** A host, a protocol and an OS-specific survival story, against two slots, a hub that routes by generation
  and adopt.

So the two are not rivals. `pty_host` is the cheap, near-term answer to "deploys kill agents" and it should be done
first. Blue-green is the answer to "deploys are risky and visible", and it removes the whole class of hazards that come
from a restart having a gap. They compose: the pty host also saves a generation that crashes, and slot A's bootstrap
deploy (the one that introduces slots) is exactly a `pty_host` restart.

## 4. How this fits room handoff and `f-card-move-inventory`

**Room handoff solves a harder problem than this one.** It moves a running card across machines, which needs a freeze,
a handoff capture, a branch and transcript carry, a successor with a new id and a forwarding chain
(`room-handoff-design.md` sections 3 and 5). This design never moves a running card. A running card on LAST ends or
parks where it is. So the heavy half of handoff is not on this design's path.

The parts that are shared:

- **The idle-park rule** (`idleParkEligible`, r-007) decides when a LAST card may be parked and is also the freeze's
  precondition in handoff. One predicate, two callers.
- **`resolvePeer` preferring the card a `moved_to` names** is handoff's. Here ids never change, so it is not needed and
  nothing in this design adds a `moved_to` to a LAST card.
- **The same-machine case** that handoff section 7 reduces to "section 3 without steps 5 and 6" is what a deploy would
  be if a deploy moved cards. This design takes the cheaper road for the same case: adopt keeps the id because the
  source is parked or ended and the destination is the same machine with the same disk, so there is nothing to chain.
  Handoff's `moved_to` stays for the cross-machine case and for a card moved while it is running.
- **Move may later reuse adopt.** If handoff's M1 wanted a cheap same-machine card move between two rooms (the
  hand-run `move.sh` case of r-new-move-card), adopt is the row-copy half of it.

`f-card-move-inventory` is item 12 in the card lifecycle plan and is "last". Its same-machine half is nearly free
under this design. A card's inventory is `card_resources` rows (`card-lifecycle-design.md` section 6) naming
worktrees, branches, dirs, ports, procs and overlays. On one machine the paths are valid for both generations, so
adopting the card adopts its inventory rows and nothing is rebuilt. What is not free is the process-shaped rows
(`proc`, `overlay`, `port`): they belong to LAST's process tree and are stopped or re-offered, exactly as that doc
says for any move. They also hold LAST open (section 2.5). So the build order is:

1. the adopt list in section 2.3 is made table-driven and guarded, so `card_resources` joins it the day it exists,
2. inventory's process kinds are given an owner generation, and the retirement check reads it,
3. the cross-machine half of `f-card-move-inventory` stays with room handoff, where it was.

## 5. Alternatives rejected

- **One store shared by both generations.** The smallest change on paper (no adopt) and the one the item might first
  suggest. Rejected for the halt and migration rules above. A discipline of "migrations must be additive while two
  generations are live" cannot be enforced, two reapers and two idle clocks on one set of rows still double-act, and the
  first error that is not contention halts a generation that is mid-turn. It is also the design
  `rolling-restart-design.md` section 8 already refused for the same reason.
- **A shared store with a lock so only one generation writes.** The idle one would have to proxy every write to the
  other, which is a second room link inside the machine. Strictly more code than adopt for a worse failure mode.
- **Move every running card across at its idle moment** (f-011's candidate A). Rejected there and still rejected. It
  needs the whole of room handoff on the critical path of every deploy and still leaves a card that never idles on
  the old build with no good answer.
- **Dynamic ports.** Bind port 0 and publish it in the address file. It works for hooks and loses the stable
  `:7781` escape hatch and every script that names a port. Two fixed slots cost one constant.
- **Hub-side per-card routing keyed on a new card id** (the successor model for a deploy). It would make `room~id`
  change across every deploy and put a forwarding chain on the hot path for no gain over keeping ids.
- **A forwarding hook shim for every runner** (the hook asks a router which generation owns its card). It adds a hop
  to every hook, several a second per runner, forever. The slot file gives the same answer with no hop. The shim is
  kept only as the one-generation bridge for runners that predate slots.
- **Waiting for the pty host and skipping this.** `pty_host` does not give a gap-free flip, a safe migration or a
  rollback, and those are what make a deploy cheap to do often. It is the first step and not the whole design.

## 6. Phases

Each phase is one worker's job, lands on its own, and is useful alone.

**Phase 0. `pty_host` on, everywhere (@fabric with @terminal, about 2 days).** Close the open gates in
`rolling-restart-design.md` (Task Scheduler job policy on sg4, systemd on cdzrok, launchd on m1mini), make the
in-job refusal (G5) real in `restart_atrium` and `provision-room.ps1 -Restart`, and turn the setting on per room.
*Done when* a deploy of sg4, sg3 and m1mini each ends with every running card still running and no card resumed, and
the board shows "pty host: survives a daemon restart" on each.

**Phase 1. Slots on one room (@runtime, about 3 days).** `--slot a|b`, the slot layout (ports, directory, database,
address file, host socket), each generation writing its own address file and launching its runners with that as
`ATRIUM_LOCATION`, the machine pointer with `take_default` and `last`, and the one-generation hook forwarder for runners
that carry the machine's file. The hub is untouched: slot B runs `--isolated`-style with a different room name in
tests. *Done when* two rooms on one machine run in slot A and slot B, a runner launched by A keeps reporting to A after
B takes the pointer, a pre-slot runner's hook and a permission long-poll in flight are answered by A through B's
forwarder, and the machine pointer and `atrium status` name both.

**Phase 2. The hub knows generations (@fabric, about 3 days). After phase 1.** `gen` and `state` in the hello, two
attached links per room, the eviction rule, the `(room, gen)` report cache, routing by owning generation for a card id
and by current generation for a room name, the refusal of a third generation, the cap counted across both, and the
board drawing the union with "old build" on LAST. *Done when* two generations of one test room are attached to one
hub, a launch to the room name lands on the current one, a say to a card id lands on its owner, a heartbeat from one
never removes the other's cards, and the board shows both builds on one row.

**Phase 3. Adopt (@runtime, about 3 days). After phase 1, parallel with phase 2.** The snapshot, the migrated copy
opened with `store.Open`, the table-driven per-card row list with its guard test, idempotent adoption by card id in
one transaction, `ceded` marking and reporting. *Done when* a snapshot of a store full of done, parked and running
cards seeds a fresh one at a newer schema with every non-live card present, the running ones absent and marked
LAST's, a second adopt of the same ids changes nothing, a migration made to fail refuses cleanly and leaves the source
file byte for byte as it was, and the guard test fails when a card-keyed table is added without a list entry.

**Phase 4. The lifecycle (@fabric with @runtime, about 3 days). After phases 2 and 3.** `atrium room next` (and the
hub tool it sits behind): start NEXT in the free slot, seed, warm, canary, ask for the flip, tell LAST `retiring`,
refuse launches on LAST, watch LAST for "nothing live" including inventory process kinds, final snapshot and adopt,
exit. The one-card adopt on resume of a parked card on LAST. The explicit abort before the flip. *Done when* a real
deploy on a test machine with three running cards flips with none touched, a new card launched during the drain
lands on NEXT, a card that ends on LAST moves to NEXT's board history at retirement, a parked card resumes on NEXT with
its same id and alias, a failing NEXT before the flip changes nothing, and a flipped-then-bad NEXT is replaced by a
good one that takes the other slot and drains the bad one.

**Phase 5. The scripts and the board (@fabric with @ui, about 2 days). After phase 4.** `deploy-ready.ps1`,
`deploy-batch.ps1` and `restart_atrium` call phase 4 for a room on slots and drop the idle gate, the room row shows
both builds and the age of the old one with a button to end a card that is holding LAST, and
`scripts/live/README.md` is rewritten. *Done when* a deploy while every card is busy needs no wait, a deploy while
nothing is running takes effect at once, and an operator can see from the board which cards keep the old build alive.

**Phase 6. Inventory and handoff reuse (@runtime, later).** `card_resources` joins the adopt list, process kinds carry
an owner generation, and handoff's same-machine card move is rebuilt on adopt if it is still wanted. *Done when* a
card with a worktree, a registered port and a process survives a deploy with its inventory intact and a finish on
NEXT frees what LAST made.

The order that gets clint something soonest is phase 0, then 1 and 3 together, then 2, 4 and 5. Phase 4 is where the
feature arrives, and phase 5 is where it feels like one.

## 7. Risks worth saying out loud

- **Adopt is the new thing that can lose a card.** It is a database copy with a migration in the middle, so it is
  guarded the way the rest of the store is: one transaction, idempotent by id, the source never deleted until the
  destination acknowledges, and a guard test on the table list. A card is in two files for a moment and never in
  neither.
- **Two generations on one machine use twice the memory and twice the idle CPU** for as long as LAST lives. The cap
  of five launches is counted across both, but the housekeeping of two rooms is real.
- **Settings drift.** A setting changed during the drain applies to NEXT only. That is the intended behaviour and it
  must be visible, so the board marks LAST's settings pane as "old build, read only".
- **A long-lived LAST** is the cost of never stopping it. The design chooses to show it, not to kill it (question 3).
- **Disk.** Two databases and their snapshots live for the length of a drain. A snapshot is deleted on a successful
  adopt and kept on a failed one.

## 8. Open questions for clint

Each with the answer this design recommends.

1. **Ports for slot B.** Fixed (+100 from the default, so 7877 and 7881) or a free port chosen at start? Recommend
   **fixed**, so the escape-hatch board and every script that names a port keep working, and the hub never needs to be
   told a port.
2. **The bootstrap.** The first slotted deploy meets runners that predate slots and carry the machine's file. Build the
   one-generation hook forwarder (phase 1), or make the first slotted deploy an ordinary restart that ends or parks
   them? Recommend **build the forwarder**, since it is small and it is what makes the first deploy as painless as every
   later one. Phase 0 makes this a restart that loses nothing either way.
3. **A third deploy while LAST still has a card that never ends.** Refuse and name the cards, wait, or end the stuck
   cards on request? Recommend **refuse and name them**, with an "end this card" button on the board's old-build mark.
   Never end a card automatically, since a card that runs for days is doing what it was asked.
4. **Who flips.** NEXT flips on its own after its checks pass, or a human or the deploy gate confirms? Recommend **on
   its own**, because the checks are mechanical and the flip is reversible by deploying a good build into the other
   slot. A deploy that wants a human can use `deploy-ready.ps1`'s existing hold before it starts NEXT.
5. **Do phase 0 first or in parallel?** Recommend **first, and short**. It is the cheapest big win and it removes the
   hazard that makes the bootstrap deploy lossy. Phases 1 and 3 can start the same day.
6. **History of cards that ended on LAST.** Adopt them into NEXT at retirement so the board has one history (this
   design), or leave LAST's database as a read-only archive the board reads when asked? Recommend **adopt**, because an
   archive file per deploy is a growing pile that nothing reads, and adopt is needed for parked cards anyway.
