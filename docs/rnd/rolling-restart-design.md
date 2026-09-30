# Upgrading atrium without stopping your agents (f-011)

Status: design, for clint's approval. Written by @rnd on 2026-09-29. Not built. The build is split between @terminal,
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
suffixed names that exist in the state dir and connect to each. Every pty is owned by exactly one host, the daemon
keeps a `card -> host` map in memory built from each host's `list`, and the board shows each card's host. When the
old primary exits empty, the newest secondary is not renamed. It stays suffixed and is found by the listing, so
there is never a moment where two processes want one name.

Framed messages, one connection per daemon, a control stream and multiplexed pty streams:

- `hello {proto, build}` both ways. A daemon that needs a newer `proto` than the host speaks does not use that host
  for new launches (section 6). The build string is shown on the board.
- `spawn {id, argv, env, cwd, cols, rows, ring}` answers `{pid, run_id}`. `run_id` is assigned by the host, unique
  for the host's lifetime and never reused, and it names this one runner start. A card started again gets a new one.
- `list` answers every pty:
  `{id, run_id, pid, cols, rows, started, exited, exit_code, ring_start, out_offset}`.
- `attach {id, run_id, from}` streams output from an absolute byte offset. A `from` older than the ring's start gets the
  whole ring and a `truncated` flag, which is the replay case. The answer carries the retained bytes WITH their size
  cuts: the same `(offset, cols, rows)` marks `ringBuffer.ReplayCuts` returns today (`supervisor.go`, `sizeCut` in
  `screen.go`), because a screen model rebuilt from bytes alone replays every resize at the wrong width. The host
  therefore owns the cut list along with the ring.
- `write {id, bytes}`, `signal {id, term|kill}`.
- `resize {id, cols, rows}` records a cut at the current offset BEFORE applying the pty resize, as the ring does
  today, so the cut and the first byte at the new size can never be out of order.
- `collect {id, run_id}` acknowledges an exit, and only then does the host forget that pty and its ring. A `run_id`
  that does not match the pty's is refused, so a late collect can never forget a later start.

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
time are not the key, because a pid is reused and two implementers would pick different clocks. Stage 1 adds the
column and stage 2 checks every exit path uses it. Collecting first would lose the last screen and could attribute the death wrongly, so it is not allowed.

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
     seconds, then fails open to the runner's own prompt without recording an allow anywhere. The request row is durable, so the new
     daemon re-surfaces it and chain step 1 (a replayed decision) answers it once. That keeps the fail-open guarantee
     (a bounded wait, then open) and keeps the question on the board. f-006 moves the gate into Go (`atrium hook
     --event permission`), which is where this retry belongs, so f-011 stage 2 depends on f-006.
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
   resume the same conversation in the same directory on the same card.
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
  same tests pass, plus host tests for reattach replay, a resize after reattach, and an exit filed once across a
  daemon crash between filing and `collect`.
- **Stage 2, the restart that leaves runners up (@runtime).** The gate and the re-derivation list in section 5, the
  line-unknown rule, `atrium stop` versus `atrium stop --runners`, and the gate's 30-second retry, which needs f-006.
- **Stage 3, rooms (@fabric).** `restart_atrium` and `provision-room.ps1 -Restart` use it. Plus the systemd and Task
  Scheduler changes in section 7, and the board showing the host build.
- **Stage 4, host upgrade (@runtime with @terminal).** Section 6: the second host and the per-card idle move. It is
  only needed the first time the protocol changes, so it can wait for that.

Stage 2 is where clint gets the feature.

## 10. Open questions for clint

1. What `atrium stop` means by default: stop the daemon and leave the runners up (the upgrade case), or today's
   stop-everything with the other behind a flag. The recommendation is the first, with `--runners` for the second.
2. B over A. Is "the agents never stop, and a daemon upgrade takes seconds" the product you meant, with the per-card
   idle move kept only for the rare host upgrade?
3. The gate waiting up to 30 seconds through a restart before failing open. Acceptable, or shorter?
4. The host on by default once stage 2 ships, or opt-in per room for a while?
