# Upgrading atrium without stopping your agents (f-011)

Status: built, opt-in. The pty host keeps runners alive while the room restarts, behind the `pty_host` setting
(`internal/ptyhost`, `internal/daemon/hostterm.go`).

Origin: design, for clint's approval. Written by @rnd on 2026-09-29. Not built. The build is split between @terminal,
@runtime and @fabric (section 9). The item is `docs/backlog/fabric/f-011.md`, which holds the six hazards this answers.

## 0. The answer, in one paragraph

f-011 proposed a second room on the same machine and moving each card across at its own idle moment. This design
recommends something else: **take the pseudo terminals out of the daemon.** A small, long-lived process, the pty
host, owns every ConPTY or pty and the runner under it. The daemon becomes a client of that host. Upgrading atrium
restarts the daemon only, in a few seconds, and the new daemon reattaches to the terminals that never closed. No
agent exits, none resumes, no card moves and no second room ever exists, so four of the six hazards do not arise.
This is the tmux server model, and it is the product promise taken literally: the agents are not stopped, not even
one at a time. The per-card idle move from f-011 is kept, but only for the rare upgrade of the host itself, and
there it runs inside one room, where it is `RestartRunner` at an idle moment and nothing more (section 6).

## 1. Why the daemon has to restart its runners today, from the code

- The supervisor spawns every runner under a pty the daemon process owns (`internal/daemon/supervisor.go`, `type
  runner`: `pty pty.Pty`, `cmd *pty.Cmd`). Closing that pty takes the process with it.
  `docs/terminal/supervision-design.md`, "A terminal belongs to the daemon that opened it", says why only the opener
  can attach.
- So a daemon restart ends every supervised runner. `docs/runtime/reload-design.md` section 5 brings back only the
  fixtures, and `reopen.go` brings back what the reopen list names, each as a resume: a new process on the old
  conversation.
- A resume is safe only when the card is idle. That is why a restart waits for every card to be idle at the same
  moment, and on a busy room that moment rarely comes (the f-011 problem statement: twelve fixes waiting on
  2026-09-29).

The coupling is one thing: the process that owns the terminals is also the process that gets upgraded. The
upgrades are frequent (every daemon fix) and the terminal code is the part that rarely changes. Split them along
that line.

## 2. The two candidates, compared

| | A. Second room, move each card when idle (f-011) | B. Pty host, the daemon restarts alone (recommended) |
| --- | --- | --- |
| Do agents stop | Yes, each one, at its own idle moment: exit, then resume | No. The runner process never exits |
| How long an upgrade takes | As long as the slowest card takes to go idle, possibly never | Seconds, gated only on short-lived work (section 5) |
| A card that never goes idle | Stays on the old build, and the old room cannot retire | Not a case |
| Card state | Rows move between two databases, or two daemons share one (a halt risk) | One database, untouched |
| Hooks (hazard 1) | Two location files, two rooms, a cutover of the machine default | Same location file, same ports, nothing to retarget |
| Messages (hazard 2) | Rows must move exactly once | Nothing moves |
| Handles, `name@room`, `room~id` (hazard 3) | The hub must route per card, or `room~id` breaks | Unchanged. Same room, same card ids |
| New-context hold (hazard 4) | A per-card wait | A short gate on the whole restart (section 5) |
| Attached browser (hazard 5) | A new pty on B, an empty replay, a lost input line | Same pty. The socket reconnects and replays |
| New code | Card migration, hub routing, two location files, a move state machine | A pty host, a local protocol, a supervisor split |
| What it cannot do | | Upgrade the host itself without the per-card move (section 6) |

B costs a new process and a versioned local protocol. A costs changes in the hub, the store, messaging and
addressing, and each of those is a place where a card can be lost or duplicated, which is what hazards 1 to 3 are.
B is also the only one where "without stopping your agents" is literally true. A means "each agent stops at a moment
it would not notice", which is weaker, and it leaves a card that stays busy on the old build for good.

**Recommendation: B.** A is written down in section 8 with its hazards answered, as the record, and as what the host
upgrade (section 6) reduces to.

Rejected, a third option: **handing the open terminal to the new daemon** (fd passing with `SCM_RIGHTS` on Unix,
`DuplicateHandle` on Windows). On Unix this works. On Windows a pseudo console is an `HPCON`, an opaque in-process
value wrapping conhost's pipes and signal handle, and there is no documented way to hand one to another process and
still resize or close it there. It would be two designs, one of them on undocumented ground. A host is one design on
both platforms.

## 3. The pty host

```
  browser             atrium daemon (upgraded often)              pty host (upgraded rarely)       runners
  ┌─────────┐  ws    ┌──────────────────────────────┐   local    ┌──────────────────────────┐  pty  ┌────────┐
  │ board   │ <────> │ store, hooks, permission,    │   pipe     │ one ConPTY / pty each    │ <───> │ claude │
  │ xterm   │        │ typing gate, screen model,   │ <────────> │ ring buffer each         │       │ codex  │
  └─────────┘        │ injection, idle clock, hub   │  framed    │ exit status each         │       │ ...    │
                     └──────────────────────────────┘            └──────────────────────────┘       └────────┘
                       restarts in seconds                          keeps running across it
```

### 3.1 What moves into the host, and what stays

The line is "what has to exist while no daemon is running".

Into the host:
- Spawning the runner under a pty, with the argv, env, cwd and size the daemon sends. This is `go-pty`'s code,
  moved rather than rewritten.
- **Draining the output, always.** A pty nobody reads fills its pipe and the runner blocks on a write. That is the
  one thing that must never stop during a daemon gap, and it is why the ring lives here.
- The ring buffer (`ringBuffer`), bounded, sized by the daemon at spawn from the scrollback setting, as today.
- Writing input bytes, resizing, and signalling (terminate, kill).
- The exit status of a runner that ended, kept until a daemon collects it.

Stays in the daemon, which is everything that is policy or interpretation: the screen model, the typing gate
(`typedLine`, `typeMu`, `lastTyped`), peer injection and its locks, the viewports and resize voting, the carryover,
input-lag tracing, the idle clock, and `windDown`'s choice of exit keys. `supervision-design.md`'s "Capture is not
interpretation" is the same line, drawn one process over.

The host holds no store, opens no listener anyone else can reach, runs no hook and knows nothing about cards beyond
an opaque id the daemon gave it. That keeps it small, and small is what keeps it rarely upgraded.

### 3.2 The protocol

A local, owner-only channel: a named pipe on Windows with a DACL granting only the current user's SID, and a unix
socket at mode 0600 in the state dir elsewhere. NOT loopback TCP: anything that can connect can type into every
runner, and loopback TCP has no notion of which local user connected. The name is derived from the state dir, so two
isolated rooms on one machine get two hosts (the state-dir pin in `docs/rnd/room-autostart-design.md` section 2.4 is
the key).

**Host names and generations.** The state-dir-derived name with no suffix is the PRIMARY host, and it is where a
daemon looks first. A host started for a newer protocol (section 6) is a SECONDARY, named with its protocol as a
suffix (`<name>.p2`), so the two never collide. Discovery at daemon start is: connect to the primary, then list the
suffixed names that exist in the state dir and connect to each. A name that exists is not a live host: a crashed
host can leave a stale socket file behind. A name only counts once `hello` answers on it, and a stale unix socket
file that refuses the connection is removed. On Windows the listing is of `\\.\pipe\` rather than the state dir,
and a named pipe vanishes with its last handle, so there is no stale entry to clear. Every pty is owned by exactly
one host, the daemon keeps a `card -> host` map in memory built from each host's `list`, and the board shows each
card's host. When the old primary exits empty, the newest secondary is not renamed. It stays suffixed and is found
by the listing, so there is never a moment where two processes want one name.

Framed messages, one connection per daemon, a control stream and multiplexed pty streams:

- `hello {proto, build}` both ways. A daemon that needs a newer `proto` than the host speaks does not use that host
  for new launches (section 6). The build string is shown on the board.
- `spawn {id, kind, argv, env, cwd, cols, rows, ring}` answers `{pid, run_id}`. `kind` is `runner` (a card's runner)
  or `shell` (a plain shell), the same split the supervisor keeps today as its `runners` and `shells` maps. The host
  stores it and never acts on it. A reattaching daemon rebuilds both maps from it, so a shell is never filed as a
  card's exit, counted by the idle rules, or restarted or parked as a runner. `run_id` is assigned by the host as a
  random 128-bit ULID-style id, the same shape as the store's keys, so it is unique across every host and every host
  lifetime, never a counter that a second or restarted host could repeat. It names this one runner start. A card
  started again gets a new one.
- **`run_id` is the pty's address.** Every verb after `spawn` names the pty by `run_id` alone. `id` is the card id
  and `kind` says which of the card's ptys this is, and both are only attributes that `list` reports back. A card's
  runner and its shell share an `id`, so an `id` never selects a pty, and a verb naming a `run_id` the host does not
  hold is refused. Nothing can type into, resize or kill the wrong pty, or a later start of the right one.
- `list` answers every pty:
  `{run_id, id, kind, pid, cols, rows, started, exited, exit_code, ring_start, out_offset}`.
- `attach {run_id, from}` streams output from an absolute byte offset. A `from` older than the ring's start gets the
  whole ring and a `truncated` flag, which is the replay case. The answer carries the retained bytes WITH their size
  cuts: the same `(offset, cols, rows)` marks `ringBuffer.ReplayCuts` returns today (`supervisor.go`, `sizeCut` in
  `screen.go`), because a screen model rebuilt from bytes alone replays every resize at the wrong width. The host
  therefore owns the cut list along with the ring.
- `write {run_id, bytes}`, `signal {run_id, term|kill}`. Every write to a pty, whatever its source (browser input, a
  peer injection, a restart wake, exit keys), goes through the daemon's existing per-runner input path and its locks
  (`pasteMu`, `typeMu`, `writeOperatorInput`, `injectPeerIf` in `supervisor.go`) before it becomes a frame, exactly
  as it reaches the pty today. The host adds no ordering of its own: it writes the frames for a `run_id` in the order
  the connection carried them, each frame whole and never split or merged with another. It serves one daemon at a
  time, and a new `hello` closes the older connection before the new one's first frame is read, so two daemons can
  never interleave writes during a restart.
- `resize {run_id, cols, rows}` records a cut at the current offset BEFORE applying the pty resize, as the ring does
  today, so the cut and the first byte at the new size can never be out of order.
- `collect {run_id}` acknowledges an exit, and only then does the host forget that pty and its ring. Because the
  address is the start, a late collect can never forget a later start of the same card.

Absolute offsets make a reattach exact: the new daemon replays the ring from its start into a fresh screen model and
then follows live, with no byte seen twice or skipped. The output is always at the width it was written at, because
the cuts say what the width was, which is the problem `supervision-design.md` spends a section on.

**The reattach order, for a runner that exited during the gap.** For every pty in `list`, exited or not, the daemon
first attaches and replays the retained output and its cuts into a screen model. Only then does it file the exit:
the event, the dead-card attribution and the tail it reads for a startup failure, exactly as a live exit is filed
today. It sends `collect` only after that filing is durable in the store. A crash between the two leaves the pty
listed, and the next daemon files it again. So filing an exit MUST be idempotent on `(task_id, run_id)`. The daemon
stores `run_id` with the runner start, and the exit event and the dead-card attribution are keyed on the pair, so a
second filing of the same exit is a no-op and the exit of a later start is never taken for it. The pid and the start
time are not the key, because a pid is reused and two implementers would pick different clocks. All of this is
stage 1: the column, and every exit path keyed on the pair, the live exit and the exit found at reattach alike.
While that filing is in flight, an exited pty is held by the daemon as `exiting`: never runnable, never offered to
attach input, never counted by the idle rules, and never shown as live on the board. Only when the filing commits
does the card's status publish and `collect` run. The start side is the mirror: `spawn` returns and the daemon writes
`(task_id, kind, run_id, host)` durably before it treats the launch as complete, so a pty a crashed daemon had just
started is still claimed by its card when the next daemon lists it.
Collecting first would lose the last screen and could attribute the death wrongly, so it is not allowed.

### 3.3 Its lifetime

- Started by the daemon the first time it needs one, detached with the flags `internal/cli/spawn_windows.go` already
  uses for the same reason: `DETACHED_PROCESS | CREATE_NEW_PROCESS_GROUP | CREATE_BREAKAWAY_FROM_JOB`, with that
  file's retry without breakaway. `Setsid` elsewhere.
- Found again by name. A daemon starting finds a host already there and reattaches. That is the whole upgrade.
- Exits by itself when it holds no pty and no daemon has been connected for ten minutes. It never exits while a
  runner is alive.
- It is the same binary, `atrium ptyhost`, so there is one thing to install. It runs from its own copy,
  `atrium.ptyhost.exe`, copied from the installed binary when a host starts. The rename-aside swap in
  `reload-design.md` section 4 then never touches the image the host runs. Its protocol version is what matters,
  not its build.

### 3.4 What this changes about stop and kill

Daemon resilience guarantee 5 says "a kill is not a stop" because killing the daemon ends every runner. With a host,
killing the daemon ends nothing, which is strictly better. It also means `atrium stop` has to choose:

- `atrium stop` stops the daemon. The runners keep going and the next daemon picks them up. This is what an upgrade
  wants.
- `atrium stop --runners` is today's behaviour: wind every runner down with its exit keys and grace, then stop the
  daemon and the host.

Which is the default is question 1. Logoff and shutdown are unchanged: the operating system ends the host and its
runners together, and the next start resumes them as today.

## 4. The six hazards, answered for B

1. **Hooks.** Nothing is retargeted. The new daemon writes the same location file, binds the same two ports and opens
   the same database. Every runner's `ATRIUM_LOCATION` still names that file (`whereami.go`, `LocationPath`). No
   second room exists, so nothing can capture the hooks. During the gap of a few seconds:
   - Activity posts fail and are dropped. That is fine by design (`docs/runtime/activity-design.md`), and the next
     event re-derives the badge.
   - `SessionStart` and `SessionEnd` in the gap are lost. A runner that ended in the gap is found by `list`
     (`exited`), and the new daemon files it as the reaper would have. A runner cannot start in the gap, because
     only the daemon spawns.
   - **A permission request in flight.** Its long-poll breaks when the old daemon exits, and today that fails open
     to Claude's own prompt in the terminal. MUST: the gate treats ANY answer that is not a decision as retryable:
     a refused or reset connection, a timeout, a non-2xx status (a daemon closing its store, or a new one not yet
     migrated), or a 2xx with no decision in it. It retries with the SAME dedup key (`tool_use_id`) for up to 30
     seconds, then fails open to the runner's own prompt without recording an allow anywhere. The daemon
     upserts the request row by `tool_use_id` BEFORE it waits or answers, so a request that reached a working store
     is durable, the new daemon re-surfaces it and chain step 1 (a replayed decision) answers it once. A request
     whose row could never be written is not on the board, and the design does not claim it is: that case retries
     and then fails open like any other, and the terminal prompt is where it is answered. That keeps
     the fail-open guarantee (a bounded wait, then open) and keeps the question on the board. f-006 moves the gate
     into Go (`atrium hook --event permission`), which is where this retry belongs, so f-011 stage 2 depends on
     f-006.
2. **Held and queued messages.** Nothing moves. `message` rows, restart wakes and owed reports are in the one
   database. What is lost is in memory and already designed to be: a pending injection's on-screen retry schedule
   (`pendinginject.go`: "a restart costs the on-screen retry and nothing that was said").
3. **Handles.** Same room, same card ids, same hub identity. `@alias`, `name@room`, `room~id`, report_to, the ledger's
   launcher links and a parked card's wake all resolve as before. A cross-room say sent during the gap is `held` on
   the sender's room and sent when this one answers, which already exists.
4. **The new-context hold.** A cycle's capture runs on a goroutine in the daemon (`idletick.go` `startIdleHandoff`,
   and `newcontext.go`), so a restart mid-cycle would cut it. The restart gate (section 5) waits for no cycle in
   flight. A cycle takes minutes at most, and nothing moves, so a restart never starts one.
5. **An attached browser.** The pty does not move. The attach websocket closes when the old daemon exits and
   reconnects on its own (it retries for five minutes, `reload-design.md` section 5). The new daemon replays the ring
   into its screen model and the socket replays from there, at the same width. The pop-out window and the phone
   viewer (t-003) are the same socket and do the same. **The human's typed input line survives**, because those
   bytes were written to the runner and sit in its own input box. What the new daemon does not know is what is on
   that line, since `typedLine` was in memory. MUST: after a reattach a card's line is treated as NOT empty until
   the screen model sees it cleared or the operator presses Enter, so no injection types into a half-written line.
6. **What "idle" means, and the rollout.** The per-card idle rule is not needed for a daemon upgrade. The gate is the
   short one in section 5. The restart is started by the existing `restart_atrium` and `provision-room.ps1
   -Restart`, called as today, and watched on the board as today. Rollback is a restart onto `atrium.old.exe`, which
   the swap already keeps: the host does not care which daemon build reconnects, only which protocol it speaks. The
   per-card idle rule comes back only for the host upgrade (section 6), where it is r-007's `idleParkEligible`
   unchanged.

## 5. What the daemon restart still waits for, and what it must re-derive

Not "every card idle". Only work that lives in the daemon's memory and lasts seconds or minutes:

- A new-context cycle or idle handoff capture in flight (`d.nctx` busy).
- An injection mid-typing (a card's `injectMu` held), or a paste in progress (`pasteMu`).
- A launch in progress (the launch lock held), so no runner is spawned by a daemon about to exit.
- A restart wake mid-delivery (`wakes.deliver` held).

Bounded: after two minutes the restart reports which card is holding it and why, and the operator chooses to wait or
go. Going costs what that work costs today when a restart cuts it, and never a runner.

What the new daemon must re-derive, because it was in memory and the runner kept going under a daemon that has never
seen it:

- **Activity counts.** Subagent and background-work counts (`act.onSubagents`, `act.backgroundWork`) start at zero,
  which reads as idle. MUST: a card reattached after a restart is not idle-parkable until its runner has posted a
  fresh `Stop`, so idle parking cannot park a card whose background shell it forgot.
- **The restart wake's start signal.** `restartwake.go` waits for `SessionStart` in this process, and a reattached
  runner never sends one. So the reattach counts as the start (with the same settle delay), or a wake would wait out
  `wakeNoHook`.
- **The idle clock's handoff marks** (`idleParks`), lost as today. It costs one more handoff, as the comment there
  says.
- **Permission waiters.** Rebuilt from the pending rows when the gate re-posts (hazard 1).
- **The r-021 guard.** r-021b accepts a new resume id only from a `SessionStart` whose pid is the card's supervised
  pid. A reattached runner's pid comes from the host's `list`, so the guard keeps working without a new
  `SessionStart`.

## 6. Upgrading the host itself

Rare, because the host is small and its protocol is versioned. A daemon change never needs a new host unless it needs
a new verb. When one does:

1. The new daemon speaks the new `proto` and finds the running host speaking an older one it still supports. It keeps
   the old host for the ptys already there and starts a SECOND host, on the new build, for new launches. The second
   host is a secondary with its protocol as a suffix (section 3.2, "Host names and generations").
2. Each card on the old host moves when it is idle by r-007's rule (`idleParkEligible`, `idletick.go`): no turn, no
   permission, no question, no queued message, no background work or subagents, no live worker of its own, and not
   mid new-context. The move is `RestartRunner` with the new host as the target: exit keys, wait for the slot, then
   resume the same conversation in the same directory on the same card. `RestartRunner` gains a target host option
   for this. Without it, which is every ordinary restart (the board's button, `restart_session`, a new context), a
   card relaunches on the host it is on now, so no restart moves a card except the idle move, which is the only
   caller that passes the option. "The host it is on now" is captured by `RestartRunner` from the old runner BEFORE
   wind-down and carried through to the relaunch, because in the gap between stop and start there is no live
   runner to read it from, and `launchLocked`'s own default (the newest host) must never be what decides it. A brand new
   card goes to the newest host.
3. When the old host holds nothing it exits by itself (section 3.3).

This is f-011's idea, and it is safe here for the reason it was hard there: it happens inside ONE room. Same
database, card, handle and hooks, so hazards 1 to 3 do not arise. Hazard 4 is the idle rule. Hazard 5 is today's
restart of one runner: the socket closes as a restart and the board reattaches (`park.go` `attachParked` does the
same). A card that never goes idle stays on the old host, which keeps working, so nothing is stuck, only not yet
moved. The board shows which host each card is on, with a "move now" per card for an operator who will not wait.

A daemon that needs a verb the old host lacks refuses that one feature on those cards and says why, rather than
refusing to start.

## 7. Platform hazards the build must check

- **Windows job objects.** A room started by Task Scheduler runs in the task's job, and ending the task ends the job.
  The breakaway flag takes the host out, where the job allows breakaway. Where it does not, the host dies with the
  task, which is today's behaviour and no worse. The spike checks which one the real room task gives.
- **systemd.** A user unit with the default `KillMode=control-group` kills every process in its cgroup on stop or
  restart, host included. MUST: the host runs in its own unit (started with `systemd-run --user`), or the room's unit
  uses `KillMode=process`. `docs/release/packaging.md` says a user unit and a logon task are one decision on two
  platforms, so both change together.
- **macOS launchd.** A LaunchAgent's children are killed with it unless they leave its process group, which `Setsid`
  does. Checked in the spike on m1mini.
- **ConPTY owned by a console-less process.** The host is detached, so it has no console. The daemon is in the same
  position today and ConPTY works there, so this is expected to hold. The spike confirms it with claude.

## 8. Candidate A, for the record

If B is refused, A is buildable, and these are the answers it would need. Each is a place B needs nothing.

1. **Hooks.** B starts isolated (`--isolated`, `ATRIUM_SHARED_LOCATION=-`, `internal/cli/roomrun.go`
   `roomLocationEnv`), with its own state dir, ports and database. Supervised runners inherit their room's
   `ATRIUM_LOCATION`, so a card resumed by B reports to B and one still on A reports to A. Only joined and window
   sessions and CLI callers read the machine's default file, and B takes that over only at cutover.
2. **Messages.** A card's rows (task, message, event, work_item, restart_wake, say, session_usage and the rest) are
   copied to B's database and deleted from A's inside the move, with A refusing new messages for that card from the
   moment the move starts. Two daemons on one database is ruled out: two reapers and two sweeps on one store is a halt
   waiting to happen.
3. **Handles.** Either B takes A's room NAME, which puts two processes behind one name at the hub (the hub keys rooms
   by name, and every check-in REPLACES that room's cards: `internal/daemon/rooms.go`, `RoomCheckIn`), so the hub
   would have to route per card. Or B is a new name, which breaks every `room~id` a sender holds. Either is hub work.
4. **New-context.** A card mid-cycle is not moved, by `idleParkEligible` plus a check on `d.nctx`.
5. **The browser.** A new pty on B. The board gets a `moved` close and reattaches to B, the replay starts empty, and
   anything the human had typed and not sent is lost.
6. **Idle and rollout.** r-007's rule. A card that never goes idle keeps A alive indefinitely, and that has no good
   answer.

## 9. Staged plan, and who builds it

- **Stage 0, spike (@terminal, on sg4 and m1mini, never sg4-wsl).** A throwaway `atrium ptyhost` that owns one ConPTY
  running claude, and a test client that attaches, types, exits, restarts and reattaches while claude keeps working.
  It checks typing after reattach, a resize after reattach, and an exit status collected after the client was gone,
  under the real room scheduled task and under a systemd user unit. About a day. The design stands or falls on it.
- **Stage 1, the host and the split (@terminal).** `atrium ptyhost`, the protocol in section 3.2, the supervisor
  talking to it, and reattach with ring replay. No change in behaviour yet: the daemon still stops its runners on
  stop. Behind a setting, off by default. Acceptance: with the setting off, restart, stop, reopen and attach replay
  behave exactly as today and every existing supervisor, attach and restart test passes unchanged. With it on, the
  same tests pass, plus host tests for reattach replay, a resize after reattach, a card with both a runner and a
  shell where each verb reaches only the `run_id` it names, and an exit filed exactly once, on the live path and
  across a daemon crash between filing and `collect`. A runner that dies inside the startup-failure window while no
  daemon is connected records the same tail and reason as the live exit path does.
- **Stage 2, the restart that leaves runners up (@runtime).** The gate and the re-derivation list in section 5, the
  line-unknown rule, `atrium stop` versus `atrium stop --runners`, and the gate's 30-second retry, which needs f-006.
- **Stage 3, rooms (@fabric).** `restart_atrium` and `provision-room.ps1 -Restart` use it. Plus the systemd and Task
  Scheduler changes in section 7, and the board showing the host build.
- **Stage 4, host upgrade (@runtime with @terminal).** Section 6: the second host and the per-card idle move. It is
  only needed the first time the protocol changes, so it can wait for that. Acceptance: restart the daemon while cards
  run on both the primary host and a `.pN` secondary, then check that attach, write, resize, exit filing and
  `collect` reach the right host for each card, that an ordinary restart of an old-host card stays on the old host,
  and that a new launch goes to the newest host.

clint's answers to section 10 become acceptance lines of the stage they touch (stage 2 for the stop default and the
retry, stage 3 for on by default). They do not hold stages 0 and 1.

Stage 2 is where clint gets the feature.

## 10. Open questions for clint

**Decided 2026-09-30 through @orchestrator: every recommendation accepted.** (1) `atrium stop` stops the daemon and
leaves the runners up, and `--runners` stops everything. (2) B over A. (3) The gate waits up to 30 seconds through a
restart, then fails open. (4) The host is opt-in, on one room first, before it becomes a default. These are
acceptance lines of stage 2 (1 and 3) and stage 3 (4). Stages 0 and 1 are handed to @terminal.

**Stage 0 gaps, answered by @rnd, 2026-09-30.** The spike is the appendix below (@terminal, f-011a
c5b87a18). Its verdict holds: B stands. Checks 1 to 5 and 7 passed with a real claude on sg4. These answers are
acceptance lines of stage 1, and stage 1 may be briefed with them.

- **G1, a probe evicts the live daemon. ACCEPT.** Section 3.2's `hello` no longer evicts. A read-only `probe` answers
  the host's build, protocol version and run list and changes nothing. `hello {takeover: true}` is the only frame that
  replaces the connected daemon, and the host logs who took over. A second `hello` without takeover is refused with
  "a daemon is already connected".
- **G2, full duplex on a Windows pipe. ACCEPT go-winio** (`github.com/Microsoft/go-winio`). It is MIT, pure Go,
  maintained by Microsoft and underneath Docker and containerd, and hand-rolled overlapped I/O is where the subtle bugs
  in this design would come from. Conditions: a pinned version, imported only from a `_windows.go` file in the host
  package, and its named-pipe security descriptor set explicitly (G6), never its default.
- **G3, the resize-cut guarantee. ACCEPT the softer wording**: "a cut is never recorded AFTER bytes produced at the new
  size". Today's supervisor has the same race, so this is no regression, and section 3.2 says so.
- **G4, a slow reader. ACCEPT**, with the bound stated in bytes: each client has a bounded queue (4 MB), and on
  overflow the host closes that client and logs it, and the client reattaches from its last offset, which the ring
  replay already serves. A slow client never slows the pty drain or another client. The exit is published only after
  the ring has drained to every connected client, so no client sees an exit before the last bytes.
- **G5, a restart that ends the room's task kills a host that could not break away. ACCEPT, and it is the one that
  matters most.** At start the host records whether it is inside the room task's job object (breakaway refused) or
  outside it. The daemon reads that from `probe` and the board shows it on the room row ("pty host: survives a daemon
  restart" or "pty host: dies with the room's task"). `restart_atrium`, `provision-room.ps1 -Restart` and the room
  restart path REFUSE a daemon-only restart on a room whose host is in-job, with that sentence, rather than killing
  every agent while promising not to. Turning the host on for a room (stage 3, opt-in) refuses the same way. A room
  whose task allows breakaway is the only kind the feature is enabled on.
- **G6, the pipe name. ACCEPT**: the name is a hash of the state directory plus the user's SID, so two atriums for two
  users or two state dirs on one machine never meet, and the name reveals no path. One pipe instance is always left
  waiting, so a reconnect never finds none. The DACL grants the owning user only.
- **G7, closing. ACCEPT**: every handle (pty, pipe, client) closes through one `sync.Once`, and a test closes from two
  goroutines at once under `-race`.
- **Check 6 (a real scheduled task as the room's user) and check 8 (systemd) are not stage 1 blockers.** Stage 1
  ships off by default. Both must pass before the host is turned on for a room of that kind: check 6 on a Windows room
  whose task runs as a user (claudevm has one), check 8 on cdzrok or another Linux machine, and launchd on m1mini.

The questions as they were asked:

1. What `atrium stop` means by default: stop the daemon and leave the runners up (the upgrade case), or today's
   stop-everything with the other behind a flag. The recommendation is the first, with `--runners` for the second.
2. B over A. Is "the agents never stop, and a daemon upgrade takes seconds" the product you meant, with the per-card
   idle move kept only for the rare host upgrade?
3. The gate waiting up to 30 seconds through a restart before failing open. Acceptable, or shorter?
4. The host on by default once stage 2 ships, or opt-in per room for a while?

## Appendix: f-011 stage 0: the pty host spike

Merged from `docs/rnd/f-011-stage0-spike.md` on 2026-10-07.

Run by @terminal's worker `f-011a` on sg4 (Windows 11), 2026-09-29. Tests section 3 of this design.
This is a throwaway. The code is `cmd/ptyhost-spike/`, a standalone command (not a subcommand of `atrium`, and nothing
in the supervisor or the cobra root knows it exists). It builds and vets on `windows` and on `linux`. It is a reference,
not a start on stage 1.

**Verdict: the design stands, with changes.** Every claim about the host that could be run here held. Six changes are
needed and two of them (the gaps in 3.2 and in 7) would have shipped a bug. Two checks could not be run as written
(6 as a real Task Scheduler task, 8 in sg4-wsl) and one is an open gate (launchd). Details follow.

### What was built

- One binary, `ptyhost-spike`, with modes `host`, `start`, `parent`, `client` and (Windows) `jobtest`.
- `host` owns ONE pty via `github.com/aymanbagabas/go-pty` (the supervisor's own package) running one command. A
  goroutine drains the output into a bounded ring ALWAYS, connected or not. The ring keeps the absolute offset of the
  next byte and the list of size cuts `(offset, cols, rows)`.
- Verbs: `hello`, `list`, `attach {from}`, `write`, `resize`, `signal`, `collect`, framed as one JSON object per line
  with bytes in base64. `attach` answers with the retained bytes from `from`, a `truncated` flag and the cuts in force
  from there, then streams `{out: offset, data}` frames. A `resize` records its cut at the current offset BEFORE it
  applies the pty resize. `collect` is refused until the runner has exited, and the host exits after it.
- Windows: a named pipe `\\.\pipe\atrium-spike-<name>`, created with the DACL `D:P(A;;GA;;;<current user SID>)`,
  `FILE_FLAG_FIRST_PIPE_INSTANCE`, `PIPE_REJECT_REMOTE_CLIENTS`, written by hand on `golang.org/x/sys/windows` so the
  spike adds no dependency. Linux: a unix socket, bound under `umask 0177` and chmod 0600.
- `start` launches the host with `internal/cli/spawn_windows.go`'s flags (`DETACHED_PROCESS |
  CREATE_NEW_PROCESS_GROUP | CREATE_BREAKAWAY_FROM_JOB`, retry without breakaway on access denied), `Setsid` on Linux.
- The client is one connection per invocation, and an `attach -script` mode that sends commands on the same connection
  it is streaming from, which is how a "daemon" both watches and types.

Isolation: pipe names `atrium-spike-*`, task names `atrium-spike-f011a-tmp-*`, files under the worker's scratchpad,
`ATRIUM_LOCATION` and `ATRIUM_SHARED_LOCATION` pointed at a file that does not exist (not cleared: cleared, the hooks
fall back to the machine's default location file, which is the live room). The claude that ran had
`ATRIUM_PERM_GATE=off` and `ATRIUM_HUB_URL=http://127.0.0.1:1`. It ran `--model claude-haiku-4-5-20251001` in an empty
scratch directory and was told it was a test. Nothing touched 7777, 7778, the live database, or a real task.

### The checks

#### 1. A client attaches, types into claude, sees the reply. PASS

Host started detached running `claude` at 120x40. `attach from 0` replayed 172 bytes (the trust dialog), a Down and
Enter were written, claude drew its banner. A prompt asking for 80 numbered lines was typed and claude answered with
`Line 1` to `Line 80`, all of it arriving on the client.

One protocol fact this found, not a bug: the text and the Enter must be two `write` frames with a pause between. Sent as
one, claude's input box takes the Enter as part of a paste and does not submit. The design already says frames are
"never split or merged", which is what makes this hold, and the stage 1 supervisor must keep sending them apart as it
does today.

#### 2. Kill the client mid-reply. PASS

Client A was killed after receiving 9234 bytes, with claude mid-reply. `list` one second later: `out_offset` 13165.
Ten seconds later with no client connected: 14814. So claude kept working, the ring kept growing, and the pty never
blocked. (The same holds for the pwsh runner: 2078 to 3013 to 3938 bytes across client kills.)

#### 3. Reattach from the last offset, and from 0. PASS

- Client B `attach from 9234`: header `from 9234`, 5580 bytes replayed, next offset 14814.
- Client C `attach from 0`: 14814 bytes. `A + B` against C: 14814 bytes each, first difference none. The 80 `Line N`
  numbers in `A + B`: 80 found, each once, in order. No byte duplicated or skipped.
- Attaching from 0 with a ring that wrapped (pwsh runner, 4096 byte ring, 5320 bytes written): header
  `from 1224, truncated true, ring_start 1224`, 4096 bytes, and the cuts rebased to the retained window:
  `[{1224, 100x30}, {3638, 80x24}]`. The whole ring, the flag, and the size cuts.

#### 4. Typing and resize after reattach. PASS

Client D attached from the current offset and typed `Now reply with exactly the single word PONG`. claude answered
`● PONG`. Then `resize 90 30`: the host answered `out_offset 22008` and `list` reported 90x30. Attaching from 22008
gave `cuts [{22008, 90x30}]` and the first bytes there are `ESC[?25l ESC[H` then a full repaint at the new width
(`Line 70`, `Line 71`, ...). So the cut precedes the first byte at the new size. See gap G3 for what this does not prove.
The pwsh runner agrees: `[Console]::WindowWidth` answered 80 after `resize 80 24`, and the cuts were
`[{0,100x30}, {3638,80x24}]`.

#### 5. The runner exits with no client connected. PASS

Client E typed `/exit` and disconnected at 23:06:19.9. claude exited at 23:06:25.6 with nobody connected. A new client's
`list` showed `exited true` (in the pwsh run: `exit_code 7`, and 0 for claude). Its `attach from 25085` returned the
2812 bytes that were written after that offset, including claude's last screen, then an `EXIT code=0` frame. `collect`
answered ok, the host exited 200 ms later, and the pipe was gone (`dial: The system cannot find the file specified`).

A note for the wire: my `list` used `omitempty` on `exit_code`, so an exit code 0 was invisible. `exited` and
`exit_code` must always both be sent.

#### 6. Job objects on sg4. PARTIAL: could not be run as a real Task Scheduler task

**A Task Scheduler task cannot run as the `claude` account on sg4.** `scripts/atrium-autostart.ps1` registers
`InteractiveToken`, and that registers here but never fires: `schtasks /Run` returns SUCCESS and the task stays `Ready`
with `Last Result 267011` (`SCHED_S_TASK_HAS_NOT_RUN`), because `quser` shows one session, `clint` on the console, and
none for `claude`. `atrium2-watchdog`, an existing task on this box, has the same never-run result. `S4U` (which needs
no session) is refused at registration: `ERROR: Access is denied`. So the real room here is not started by a task
running as this account, and its job policy cannot be read. I did not touch that task or the room.

Substitute, which answers the design's actual question: the harness `jobtest` creates a job object with
`KILL_ON_JOB_CLOSE`, starts `parent` in it, then closes the job, which is what ending a task does to its members. Three
policies for breakaway:

| Job allows | `CREATE_BREAKAWAY_FROM_JOB` | Host after the job closed | claude-shaped runner |
| --- | --- | --- | --- |
| nothing | refused, `Access is denied`, retried without | DEAD | dead (heartbeat stopped at 175 bytes) |
| `BREAKAWAY_OK` | honoured, host started outside | alive | alive (315 to 420 bytes) |
| `SILENT_BREAKAWAY_OK` | honoured | alive | alive (315 to 385 bytes) |

So `spawn_windows.go`'s flags do what the design says, and the retry path does what the design says ("dies with the
task, no worse"). Also read directly: the shell an agent runs in on sg4 is in a job with `LimitFlags 0x1800`
(`BREAKAWAY_OK` and `SILENT_BREAKAWAY_OK`, no kill on close), so a host started from an agent shell breaks away.

**What is still unknown:** whether Task Scheduler's own job allows breakaway. That needs a machine where a task really
runs, which is any Windows box with an interactive logon for the account. It is an open gate for stage 3, listed under
"Open gates". See gap G5, which says why it matters more than the design thinks.

#### 7. ConPTY owned by a console-less detached process. PASS

Every host in these runs was started `DETACHED_PROCESS`, so it had no console. claude ran correctly under it: banner,
trust dialog, resize repaint, the reply and `/exit` all worked.

#### 8. systemd. NOT RUN (recorded as option a)

Not runnable on sg4 as `claude`. `wsl -l -v` for this account lists one distro, `docker-desktop`, stopped, and I did not
start it. sg4-wsl is `clint`'s distro and a live room, so the design says never. @terminal ruled: record it as not
runnable here, write the steps down, and try a throwaway Linux box only if there was room. There was not, so it was
not tried. Probed only: `ssh cdwsl` is refused (localhost port 22), and `ssh cdzrok` (Ubuntu 22.04 EC2, user `ubuntu`)
answered `systemctl --user is-system-running` with `running` and has `systemd-run`, so it is a ready home for this test.
Nothing was created there.

What is done: the Linux build compiles and vets (`GOOS=linux go vet ./cmd/ptyhost-spike`). The unix socket path
(`pipe_unix.go`, `detach_unix.go`) has been compiled and never executed. What would run it, on any Linux with
`systemd --user` and `linger` on, taking about an hour:

1. `GOOS=linux go build -o build.claude/ptyhost-spike ./cmd/ptyhost-spike`, copy into the distro.
2. A unit `atrium-spike-f011a.service` with `ExecStart=ptyhost-spike parent -name x -- bash`, default `KillMode`. Start
   it, `systemctl --user stop` it, and check whether the host (and its `bash`) survived. Expected: dead, the section 7
   hazard.
3. Repeat with `KillMode=process`, and with the host started by `systemd-run --user --unit atrium-spike-f011a-host
   ptyhost-spike host ...`. Expected: both survive. Recommendation from reading only, not evidence: `systemd-run --user`
   from the daemon, because it needs no change to the room's unit and works for a room whose unit an operator wrote, but
   it makes the host depend on `systemd-run` being on PATH and a user manager running, and `KillMode=process` also leaves
   every other stray process of the room behind on stop. Pick after step 3.
4. Remove both units and `systemctl --user daemon-reload`.

If the distro has no user session: `[boot] systemd=true` in `/etc/wsl.conf`, `wsl --shutdown`, and
`loginctl enable-linger <user>`.

### Findings that are not in the design

Named plainly, by section.

**G1. Section 3.2, "a new `hello` closes the older connection", evicts the live daemon on any probe.** My first host
closed the older connection when ANY connection arrived, and my own test client's `list` killed the attached client.
The design's discovery says a name "only counts once `hello` answers on it", so a second daemon start, `atrium status`,
a stale-socket scan, or the daemon's own reconnect probe would send `hello` and evict the daemon that is running.
Change: two verbs. `probe` (read only, never evicts, answers `proto`, `build`, `owner pid`) for discovery, and
`hello {takeover: true}` as the only thing that closes an older connection, and only after the owner is shown to be
dead or the operator chose to.

**G2. Section 3.2, "one connection, a control stream and multiplexed pty streams", needs overlapped I/O on Windows.** A
named pipe handle opened without `FILE_FLAG_OVERLAPPED` serialises every call on it: a blocked `ReadFile` stops a
`WriteFile`. A daemon that is streaming output and typing on one connection would deadlock or stall. The spike used
overlapped I/O by hand, which works. Stage 1 should use `github.com/Microsoft/go-winio` (not in `go.mod` today) or the
same code, and must say so.

**G3. Section 3.2, "the cut and the first byte at the new size can never be out of order", is stronger than the host can
know.** The cut is recorded at the ring's write position when the `resize` verb runs, but the drain is asynchronous:
bytes the runner wrote at the old size may still be in the pty's pipe and land after the cut, and ConPTY applies the
resize asynchronously too. The evidence in check 4 (the repaint begins right after the cut) shows the common case. The
guarantee is really "the cut is never after bytes written at the new size", and a cut can be a few bytes early. That is
today's behaviour in the supervisor (same race), so it is not a regression, but the design should not promise more.

**G4. Section 3.3 and 3.2, the host never says what happens to a slow reader.** A subscriber that stops reading must
not block the drain. The spike gives each attached client a 4096-frame queue and, when it overflows, closes that client,
which reattaches from its last offset. Say so in the protocol. Related: the exit event must be published after the drain
has finished, or the tail is short. The spike waits for the drain to reach EOF or 1.5 s and files the exit after that.
Whether ConPTY's read side reaches EOF by itself after the child exits was not separately measured, and the 1.5 s cap is
what makes the tail complete either way. Stage 1 should measure it and drop the cap if it is unneeded.

**G5. Section 4.6 and 7, how a restart is done can end the host.** The design says a restart is `restart_atrium` or
`provision-room.ps1 -Restart`. If the room is a scheduled task, ending and re-running the task closes its job, and the
host is in it unless breakaway succeeded. The table under check 6 shows the failure exactly: with breakaway refused, the
host dies with the task and takes every runner, in the very operation the feature exists for. The design's answer, "no
worse than today", is true and beside the point: it is worse than the operator believes. Change: the daemon records
whether the host started outside the job (the spike's `jobReport` shows how: `IsProcessInJob` and
`QueryInformationJobObject` with a null job handle, and the retry path in `startDetached` already knows which happened),
puts that on the board next to the host build, and a restart that would end the job refuses to proceed when the host is
inside it. Otherwise every operator on a task that forbids breakaway is one restart from losing every agent.

**G6. Section 7, Windows pipe names.** The pipe namespace is flat and global, so "derived from the state dir" has to
mean a hash of its full path, and two users on one machine need the user's SID in the name. The pre-created instance
also matters: my Accept creates the next instance only after the previous connection returns, so a client that dials in
that gap gets `ERROR_FILE_NOT_FOUND` or `ERROR_PIPE_BUSY`. The client retries for 5 s and that hid it. The host should
always have one instance waiting.

**G7. Handle hygiene in the host, learned by crashing it.** `pipeConn.Close` was called from two paths and the second
`CloseHandle` hit a value the runtime had already reused: `GetQueuedCompletionStatusEx failed (errno=735)`, `fatal
error: netpoll failed`, the host gone. In a host that owns every runner that is the worst failure there is. It is the
same hazard `internal/daemon/CLAUDE.md` records for `closePTY`. Every handle in the host closes through one function with
a `sync.Once`, and the stage 1 tests should close a connection from two goroutines.

Smaller: the DACL was applied (the log line prints it) but I could not connect from a second account to test the
refusal. A `truncated` replay must also say the offset it starts from, which the spike does (`from`). Not measured:
memory or CPU of the host with many ptys, `write` throughput, and behaviour when the daemon's frame is larger than the
pipe buffer (writes loop, and the spike never sent more than a few KB).

### Open gates

- **Open, half finished: check 8 on cdzrok.** Steps 1 to 4 under check 8 can run there as user `ubuntu` with a uniquely
  named unit in a scratch directory. Not done, so the `KillMode` hazard is still from reading, not evidence.
- **The Task Scheduler path itself stays untested**, because only `clint` has a session on sg4. The
  `KILL_ON_JOB_CLOSE` job in check 6 is a fair substitute for the job semantics, not for the scheduler.
- **macOS launchd, m1mini: OPEN GATE.** Not run, m1mini was offline. Per @rnd this must pass on m1mini before the host is
  turned on for m1mini (it is opt-in per room anyway), and it does not hold stage 1. What to check: a LaunchAgent that
  runs `ptyhost-spike parent`, then `launchctl bootout` it, and see whether the `Setsid` host and its runner survive.
- **Task Scheduler's own job policy** (check 6 above), on a Windows machine with an interactive logon for the account.
  Does not hold stage 1. Holds stage 3.
- **systemd** (check 8 above). Does not hold stage 1. Holds stage 3.

### What the spike created and removed

Created: pipe names `atrium-spike-*` (gone with their hosts), scheduled tasks `atrium-spike-f011a-tmp-break` (twice,
InteractiveToken, then S4U which failed to register), both deleted (`schtasks /Query` finds no `spike`), copies
`spike-task-*.exe` and `spike-jt-*.exe` under the scratchpad, one throwaway claude (ended by `/exit`), several pwsh
runners. Processes after the last run: none (`Get-Process` for `spike-*` and `ptyhost-spike` is empty, and no heartbeat
pwsh is left). No systemd unit was created because none could be. The real room's task, the live daemon, its ports and
its database were not touched.

### Verdict

**The design stands, with changes.** The core claims hold on real hardware with a real claude: a detached, console-less
process can own a ConPTY, keep it drained while nobody is attached, and let a new client attach from an absolute offset
with no byte lost or repeated, type, resize and collect an exit. Required changes before stage 1 is built:

1. A non-evicting `probe` for discovery, and takeover only through an explicit verb (G1).
2. Overlapped I/O for the Windows pipe, stated in 3.2 (G2).
3. Soften the resize-cut guarantee to what a host can know (G3).
4. Specify the slow-reader policy and publish an exit only after the drain (G4).
5. Record whether the host got out of the job, show it, and refuse a restart that would end it (G5).
6. Pipe naming, always one instance waiting, and one idempotent close for every handle (G6, G7).
