# Processes: a dev server an agent starts, owned by atrium

**Status: design, nothing built.** Written 2026-09-24 for the idea below. Decisions that are clint's are the
numbered Open Questions at the end.

## The problem

Agents start long-running processes all the time: a docs dev server, a throwaway hub for a test, a file watcher,
`go test -run X -count=1000` left grinding. Today the orchestrator ran `npx docusaurus start --port 3310` in a
background shell of its own. Five things are wrong with that, and they are the same five every time:

1. **Nothing on the board shows it.** The operator cannot see that port 3310 is taken, or by what.
2. **It dies with the session that started it.** A `/clear`, a context cycle, or the session ending takes it down.
3. **Nobody else knows its port.** A second agent that wants the docs site has to be told, or guesses, or starts a
   second copy on the same port and fails.
4. **Nobody else can stop it.** Only the session holding the background shell can, and once that session is gone,
   nothing can short of `taskkill` by pid, which nobody recorded.
5. **Its output is private.** When it fails, the error is in a shell buffer only one session can read.

The ask: an agent registers the desire ("run this, in this directory, it serves port N") and atrium runs it and
shows it.

## New thing, or an existing one with a new door

**An existing one with a new door, plus one table.** A registered process is the card shell
(`internal/daemon/shell.go`) with a command instead of an interactive prompt, several per card instead of one, and
a row in the store so a restart can bring it back.

The three pseudo terminals atrium already owns, and why each is not the answer as it stands:

| | what it is | why it is not this |
| --- | --- | --- |
| runner (`supervisor.runners`) | the process doing a card's work | its exit files the card `dead`, the reaper and the park and shelving all act on it, and there is one per card |
| card shell (`supervisor.shells`) | one interactive shell beside the runner | one per card, no command, closed after 30 minutes with nobody attached, never survives anything |
| fixture (`store.Fixture`) | a runner that comes up with the daemon | it is a runner: it makes a card, has a harness, has a conversation to resume |

The runner is the wrong base. Every caller of `supervisor.get` means "the process doing the work", and
`docs/supervision-design.md` spends a section on why the shell was kept out of that map. A dev server in it would
be parked, reaped, and would mark its card dead on exit. That is exactly the mistake the shell avoided.

The shell is the right base. It already has everything a process needs from the supervisor: a `runner` struct with
a pty, a ring buffer, a watcher fan-out, attach over `attach.go`, and a hangup-then-kill close in `CloseShell`. What
it lacks is a command, a name, a port, a lifetime longer than "until nobody is looking", and a row. So the design is
a **third map on the supervisor, `procs`, keyed by process id**, reusing the `runner` struct and the shell's
spawn and close paths, with lifecycle rules of its own.

`shell.go` argues "two terminals, not N", because a list needs naming, ordering and a picker. That argument holds
for shells and does not transfer: a process is named by whoever started it, and the list IS the picker. Nobody
opens a process to type at it. They open it to see why it died.

### The workaround that exists today

Check this first: a harness row whose command is `npx docusaurus start --port 3310`, launched with
`atrium_launch`, already gives a card, a pty, attach, and a stop. It costs a card that lies. The card sits in
`running`, goes `dead` on exit and is archived a minute later, carries no port, and clutters the columns meant for
human attention. It is a fine stopgap for one server and the wrong shape for the pattern. The rest of this doc is
the right shape.

## Where it lives: the room

The room owns the pseudo terminals (`docs/hub-room-plan.md`), so the room owns processes. The hub owns none and
restarts all evening. This is the split's whole point, and a process started from a hub would die with every CSS
change.

- **ROOM-SIDE:** the `procs` map, the store table, the spawn and stop paths, the port checks, the HTTP endpoints,
  shutdown and reopen.
- **HUB-SIDE:** the MCP tools on `atrium-control` (served by the hub, `internal/link/control_mcp.go`), the
  lifecycle kinds the hub whitelists for its audit log, and the board.

Card-scoped routes (`/v1/tasks/<id>/...`) already route to the room holding the card through `proxy.go`, so the
endpoints below get cross-room routing for free by living under a card.

## The surface

### HTTP, on the room

```
POST   /v1/tasks/{id}/procs             start one        { name, command, args?, cwd?, port?, env?, restart? }
                                                          -> { proc }       201, or 409 with the reason
GET    /v1/tasks/{id}/procs             this card's processes
GET    /v1/procs                        every process in this room (the hub fans it out across rooms)
GET    /v1/procs/{proc}                 one, with its row and live state
POST   /v1/procs/{proc}/stop            { grace? }  -> { proc }  asked, then insisted, then the tree
POST   /v1/procs/{proc}/restart         stop then start with the same row
GET    /v1/procs/{proc}/logs?lines=N    the tail of its ring buffer, ANSI stripped, bounded
DELETE /v1/procs/{proc}                 stop it if running, and forget the row
GET    /v1/tasks/{id}/attach?kind=proc&proc={proc}
                                        the existing websocket, pointed at a process's pty
GET    /v1/ports/{port}                 is this port usable here, and if not why (see "Ports")
```

`{proc}` is atrium's own id, a text ULID like every other key. It is never the operating system's pid, which is
recycled. Start lives under the card because a process always has an owner card. Everything after start is keyed
by the process alone, because the owner card may be archived while the process runs (see "Lifetime").

`port` on a start is an integer or the literal `auto` (stage 3). On the stored row and on every process returned,
it is always a concrete integer, the one asked for or the one auto picked, or null when none was declared. The
union exists only on the way in, so HTTP, MCP, CLI and the board all read one shape.

`name` is required and unique per card: `docs`, `test-hub`, `watch`. It is what the board labels the row with and
what an agent says when it asks "is the docs server up". A second start with a name that is already running
answers `409` and names the running one. It never starts a twin. That is the idempotency the shell has in
`EnsureShell`, for the same reason: an agent asking twice meant the same thing both times.

`cwd` defaults to the card's worktree and must be inside a root `browseroots.go` allows. `command` is resolved with
`exec.LookPath` before `Dir` is set, and scripts go through `viaShellIfScript`, exactly as `spawnPTYResume` does.
`npx` is a `.cmd` on Windows and fails the same way `claude` would without that.

### MCP, on `atrium-control`

Four tools, named to sort beside the others:

| tool | does |
| --- | --- |
| `atrium_proc_start` | start one on the caller's own card. Takes `name`, `command`, `args`, `cwd`, `port`, `restart`. Returns the process, its port, and where to see it |
| `atrium_procs` | every process in reach: name, owner card, command, port, state, uptime, exit code. Call before starting, to find one already up |
| `atrium_proc_stop` | stop one by name (on your card) or by id (anywhere) |
| `atrium_proc_logs` | the last N lines, 200 by default, 2000 at most |

The owner is the calling session's card, read from `X-Atrium-Agent` the way every control tool already reads it.
**An agent cannot start a process on another card.** It can stop one on another card, because the reason to stop
somebody else's process is that it is holding a port or wedged, and asking the owner is exactly the thing that
fails when the owner is gone.

`atrium_proc_logs` returns output, which `atrium_task` says atrium never does. The difference is deliberate. A
runner's output is a conversation and the way to learn what it thinks is to ask it. A process cannot be asked, and
"why did the dev server exit" is answered only by its last lines. The tail comes from the in-memory ring, is
bounded, and is never written anywhere.

The tool description tells the agent what it most needs: the process outlives you, it is on the board, stop it
when you are done with it, and call `atrium_procs` first.

### CLI

```
atrium proc start --name docs [--port 3310] [--cwd .] [--restart room-start] -- npx docusaurus start --port 3310
atrium proc ls [--all]
atrium proc stop <name|id>
atrium proc logs <name|id> [-n 200] [-f]
atrium port 3310
```

The CLI finds the card from `ATRIUM_TASK_ID` the way hooks do. Run with no card in the environment, `start`
refuses: every process has an owner, and a CLI in a plain terminal has none to give it. `-f` follows over the
attach websocket, read-only.

## What the board shows

**On the card:** a strip of chips under the title, one per process. Each chip is the name, the port, and a dot:
green running, grey stopped, red exited non-zero. `docs :3310 ●`. Clicking a chip opens its terminal in the same
pane the card shell uses. The strip is hidden when the card has none, so no card grows chrome it does not use.

**A board-wide list**, reached from the header, because "what is holding 3310" is not a question about any one
card. One row per process across every room:

| column | from |
| --- | --- |
| name | the start request |
| room | the room that owns it |
| owner card | linked, and marked when the card is archived |
| command and cwd | the row, command shown in full on hover |
| port | declared, with discovered beside it when they differ (stage 3) |
| state | starting, running, stopping, stopped, exited, lost |
| uptime or exit | "12m", or "exited 1 after 4s" with the first line of its last output |
| restart | never or room-start |
| actions | open, restart, stop, forget |

The port renders as a link only when the board is served from the process's own machine. From a hub on another
machine, `localhost:3310` is the wrong machine, so the port is shown as text.

**Attach works.** A process is a pty and the attach path already carries one, so opening a process is attaching to
it, read and write. Write matters more than it sounds: a dev server takes `r` to reload and `q` to quit, and a test
hub may prompt. Everything `docs/supervision-design.md` says about sizing and replay applies unchanged.

The attach route grows a parameter (`kind=proc`), which `internal/daemon/CLAUDE.md` calls a new endpoint. It is
refused to a guest by the existing `kind != ""` check in `overlay_guest.go`. That check is load-bearing now and
gets a test that says so.

## Lifetime

The rules follow one idea: **a process belongs to a card, not to a session.** A session ending is not a card
ending, so it is not a process ending.

| event | what happens to the card's processes |
| --- | --- |
| the owner session exits, `/clear`s, or its runner dies | nothing. The card goes `dead` and the processes run on |
| the dead-card sweep (`sweep.go`, one minute) | **the card is not archived while it has a running process.** It stays on the board, `dead`, with its strip showing what is still up |
| the operator marks the card `done` | asked: "2 processes are still running on this card. stop them?" Default stop. See Q3 |
| the card is shelved | nothing. Shelving parks the runner so it stops spending tokens, and a dev server spends none |
| the card is deleted or pruned | stopped, after the operator is told which ones. Bulk prune stops them without asking, and the prune count says so |
| the room shuts down (`atrium stop`, a restart) | stopped, bounded and narrated. See "Shutdown" |
| the room comes back | processes whose `restart` is `room-start` start again. Everything else is shown `stopped`, with a start button |
| the room is killed | the whole tree dies with it (see "Stopping a tree"). The rows say `lost` on the next start |
| the hub restarts | nothing. The hub owns no process |

**Why hold the card off the sweep rather than let the process outlive its card.** The alternative is a process
with no owner on the board, which is the problem this doc exists to solve, moved one step later. A dead card with
"docs :3310 ●" on it says exactly what is true: the session is over and something it started is not. The operator
stops it or keeps it. `pruneTimer`-style deletion that a human did not ask for still stops processes, because a
timer deleting a card and leaving its server running is the orphan again.

**The existing check does most of this.** `sweepIdleShell` already asks "is my card still there" on its own tick
rather than threading ids out of three bulk-delete paths. Processes do the same, which covers the prune timer,
the archive sweep and `POST /v1/tasks/prune` with one call site that cannot be forgotten.

**Pin.** A pinned card is a fixture in all but name, "keep this in front of me". A process started on a pinned card
defaults to `restart: room-start`, and one on an unpinned card defaults to `never`. Either is overridable per
process. See Q4.

**Idle timeout: none.** The shell closes after 30 minutes unattached because an idle prompt is something the
operator forgot. A dev server with nobody attached is the normal case. What stands in for the timeout is the
board showing every process, all the time.

### Surviving a room restart, and what it costs

A process in a pty dies with the room, like every runner. ConPTY offers no reattach (`docs/architecture-v2.md`,
"Open risks"), so a pty process cannot survive a room restart at all. The choice is only whether it comes back.

- **`restart: room-start`** re-runs the row. The command, args, cwd, env and port are on the row, so nothing else is
  needed. This is `reopen.go` for processes, and it is ordered after runners reopen, one at a time with the same
  400ms gap.
- **What it costs:** anything the process held in memory. A docs server loses nothing that matters. A test hub loses
  every room that joined it and every token it issued. That is why the default for an unpinned card is `never`, and
  why the choice is on the row where the agent that knows makes it.
- **The port may be taken** by the time it restarts. The start checks run again, and a failure lands on the row as
  the reason, exactly as a fixture that could not start records why (`NoteFixtureRun`).

**Detached processes that survive the room** are possible: spawn outside a pty, output to a file, record the pid.
That is window mode's trade, and it breaks three things this design wants. There is no attach. Stopping needs a
pid atrium recorded before the room died, and pids are recycled. And the job object that makes a kill safe (below)
cannot hold a process that must outlive it. Out of scope, recorded in Q6.

## Ports

### Declared, and checked before the start

The start request may carry `port`. When it does, the room checks it **before spawning**, and a start that would
fail on the port is refused with the reason, so the agent learns it has to pick another port without waiting for a
dev server to crash:

1. **Held by a registered process.** Looked up in the registry, across the room. The refusal names the owner: "3310
   is held by `docs` on card sa51 (running 40m). stop it, or reuse it: it is the same command." When the command
   and cwd match, the answer says so, because the usual case is a second agent wanting the same docs server.
2. **Reserved by the operating system.** On Windows, the TCP excluded port ranges (`netsh int ipv4 show
   excludedportrange protocol=tcp`). On this machine 50260-50359 is reserved, and binding there fails with
   `WSAEACCES`, which Go prints as "An attempt was made to access a socket in a way forbidden by its access
   permissions". Docusaurus words the same failure as "something is already running on port", which is how it
   cost an afternoon. The refusal says "reserved by Windows (range 50260-50359)", which is the sentence nobody gets
   today.
3. **Held by something else.** A bind probe on the port. `EADDRINUSE` means taken. Stage 3 names the owning pid
   with `GetExtendedTcpTable` on Windows or `/proc/net/tcp` on Linux.

**The probe is the test and the range list is the explanation.** A bind probe answers checks 2 and 3 together:
`WSAEACCES` is reserved, `WSAEADDRINUSE` is taken. It needs no parsing and cannot disagree with what the process
is about to see. `netsh` is read only to word the refusal and to steer the auto picker away, and it is read once a
minute at most. Hyper-V and WSL move the ranges at boot, so it cannot be read once.

The probe binds the address the process will use. That is `127.0.0.1` and `0.0.0.0` both, because a dev server
binding one while something holds the other is the second most common collision. A probe is a check at one instant
and the process binds a moment later, so it can still lose a race. That shows up as the process exiting, with the
bind error in the tail its exit reports.

**`GET /v1/ports/{port}` and `atrium port N` ask the same question without starting anything.** That is how an
agent is told a port is taken before it tries, whether or not it uses atrium to start the thing.

### Auto

`port: "auto"` asks the room to pick one. It binds `:0` until the answer is outside every excluded range and not
in the registry, then passes it to the process as `PORT` in the environment and substitutes `{port}` in `args`. The
answer is on the row and in the tool result. This removes the guessing, and the orchestrator's preview-port rule
(50000 + backlog item) becomes a preference the caller can still pass as a number.

### Discovered (stage 3)

What a process says it serves and what it listens on can differ. Webpack moves to 3311 when 3310 is taken, and a
test hub picks its own. From stage 3, the room asks the operating system which listening sockets belong to the
process tree (same `GetExtendedTcpTable` call, filtered to the tree's pids), every five seconds for the first
minute and then every minute. Discovered ports are shown beside declared ones and count as held for check 1. A
declared port nothing ever listened on is shown as a warning, not an error, because some processes serve nothing.

This reads the operating system, never the output. Parsing "Local: http://localhost:3310" out of a log is the
output-interpretation `docs/architecture-v2.md` rules out, and a guess would disagree with the socket table.

## Resilience

The daemon guarantees in `CLAUDE.md`, one by one.

**Storage failure halts.** The process row is written before the process is spawned, the same order a launched
card follows. A failed write is a tier-3 failure and halts the room like any other. The halt stops every process
along with the runners, because a registry that cannot record a stop cannot say what is running. And **the start
endpoint checks the halt itself.** The agent listener closes on a halt, but the human listener stays up, and the MCP
tools reach the room through the hub over that human listener. Without the check, a halted room would start
processes it cannot record. A crash between the row and the spawn leaves a row with no pid, which the next start
shows as `lost`.

**A kill is not a stop, and a stop must take the tree.** Covered in its own section below, because today no stop
path in atrium does it.

**Shutdown is bounded and narrated.** `stopSupervised` stops runners first, then processes. That order is
deliberate: a runner mid-turn may be running tests against the test hub, and pulling the hub out first turns a
clean exit into a failing test run the agent then tries to debug. Processes stop in parallel, each with its own
grace (default 5s, a per-row `stop_grace` capped at 10s so no row can extend shutdown past the runners' budget),
and each step logs: "stopping 3 process(es), up to 5s each", "docs exited when asked", "test-hub did not exit,
ending its tree". Carryover saves process scrollback beside runner scrollback, bounded by the same budget.

**Nothing here may fail a session.** Four places it could, and what stops each:

- A process exiting never touches its owner card's status. This is the rule the shell exists to keep, and it is why
  processes are not in `runners`. `awaitProcExit` is `awaitShellExit` plus a row update, never `awaitExit`.
- A failed start is a tool result with the reason, never an MCP error the session has to recover from.
- Nothing about processes rides a hook. `/activity`, `/session`, `/permission` and `/stop` are unchanged.
- The proc tools have their own timeout. The 8s `controlTimeout` fits a status read, not a start that is waiting on
  a human to approve it (see "The permission gate").

**A source is a suggestion** has a cousin here: a process is never parsed for status, its output is bounded while
read (the ring), and a process crash-looping under `restart: room-start` does not loop. Restart happens once per
room start, never on exit. "Restart on crash" is out of scope (see Q7).

### Stopping a tree

Today every stop in atrium ends one process. `windDown` and `CloseShell` write keys, close the pty, then
`cmd.Process.Kill()`, which on Windows is `TerminateProcess` on the direct child only. For claude that is enough.
For `npx docusaurus start` it is not: the child is `cmd.exe` running `npx.cmd`, whose child is `node`, whose
children are webpack workers. Closing the pseudo console sends a close event to every process attached to it,
which catches most of that tree. It misses anything that detached or opened its own console, and any child that
ignores the event. The survivor holds port 3310 with no row pointing at it, and the next start fails on a port
atrium just said was free.

So a process is started inside a **Windows job object** with `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`, and the stop
ends with `TerminateJobObject`, which ends every process in the job whatever it did with its console. Two
consequences:

- **A kill of the room ends every process tree too**, because the job handle closes with the room. That is the
  guarantee we want. "A kill is not a stop" stays true in the sense `CLAUDE.md` means: a kill still takes everything
  down at once and is still the wrong way to stop atrium. What changes is that it leaves no orphan holding a port.
- **The child must be in the job before it spawns anything.** Assigning after `Start` races a child that spawns in
  its first milliseconds. `go-pty` does not expose `CREATE_SUSPENDED`. The spike in stage 1 decides between
  patching that in, starting through a tiny launcher that assigns itself first, and assigning after start and
  sweeping the toolhelp snapshot for stragglers (`ancestry_windows.go` already walks it). The spike's result is
  recorded here before stage 1 merges.

On Linux and macOS the pty child already leads its own session, so the stop signals the process group: SIGINT,
SIGTERM, then SIGKILL to `-pgid`. A child that calls `setsid` escapes that. On Linux a cgroup would catch it, and
that is out of scope until a room runs on Linux for real.

The full stop sequence, per process:

1. Write ctrl-c to the pty. The console delivers it to the whole console-attached tree. Wait up to the grace.
2. Close the pty. Wait two seconds.
3. Terminate the job (or the process group). Wait two seconds for the handles to release, as `CloseShell` does,
   because on Windows a directory with a process in it cannot be removed, and deleting a card is often followed by
   removing its worktree.
4. Log each step that was needed. A process still up after 3 is logged as "still holding <cwd>", as the shell does.

**The runner and the shell get the same job object in a later stage.** They have the same tree problem, and a
claude runner's Bash tool children are the same shape. That is a separate change with its own risk, and is not
bundled here (see "Build plan", stage 4).

## Security

**Loopback, no auth, unchanged.** Anything that can reach a room's loopback board can already start a runner
(`POST /v1/launch`) and open a card shell and type into it. A process start adds no new class of access on this
machine. It is the same command execution, reached more directly.

**The full board over an overlay** (zrok or OpenZiti, `docs/overlays.md`) hands over everything, shells included,
so processes are in the same position as shells there. A new door is still a door, so processes honor an off switch
the way shells honor `shell command = off`: a `processes` setting, `off` meaning the room refuses every start with
the reason. The start endpoint is refused outright if the setting is `off`, not only hidden on the board, for the
reason `EnsureShell` gives: an old tab or a script asks anyway.

**A lent session** (`overlay_guest.go`) is an allowlist, and nothing here is added to it. A guest gets 403 from
every `/v1/procs` and `/v1/ports` path and from `/v1/tasks/<id>/procs`, and the attach route refuses `kind=proc` via
the existing `kind != ""` check. A test pins each of those. The guest can still reach processes one way, and it is
the right way: the guest drives the agent, and the agent can call `atrium_proc_start`. That is why the gate below
matters.

**A process that binds `0.0.0.0` exposes itself to the LAN.** Atrium does not decide what a dev server binds, and
does not pretend to. The board shows the bound address from stage 3 so the exposure is visible.

**Environment.** A process gets the room's scrubbed launch environment plus `ATRIUM_TASK_ID` and `ATRIUM_PROC_ID`,
and **not `ATRIUM_AGENT_NAME`**, for the reason `shellEnv` gives: a claude started from a process would otherwise
file activity as the agent that started it. `env` in the start request is added on top and stored on the row, so the
tool description says not to pass secrets in it. The row is visible on the board.

### The permission gate

**Registering a process goes through the permission chain.** Without that, the proc tool is a way around it.

The PreToolUse hook does not gate `mcp__*` tools (`docs/architecture-v2.md`, "What carries over from v1", item 6),
because gating the agent's own control tools is circular. So a session told "never `npx`" by a standing rule could
run `npx` anyway by asking atrium to. For `atrium_launch` that gap is harmless, since what it starts is a claude
with its own gate. For a process it is the whole command.

So the room gates the start itself. It builds the request the hook would have sent, **tool `Bash` and the full
command line**, on the owner card, and runs it through `onPermRequest`. The whole chain applies in order: a replayed
decision (the dedup key is a hash of name, command, args and cwd), a queued message, a shelved card's standing no,
a standing rule, auto mode, and finally asking the human, with the card in `needs-permission`. Using `Bash` rather
than a new tool name is deliberate. The 134 imported rules and every rule written since are `Bash` rules, and a new
tool name would make every process start a fresh question and every existing `never` rule silently inapplicable.
The request's `details` say it is a process start, with the port and the restart policy, so the human deciding sees
what they are approving.

- **The tool call blocks until the chain answers**, the way the hook does. The card column says `needs-permission`,
  and that is only true if the agent really is waiting. An asynchronous start that returned "pending" would leave a
  card claiming to wait while its agent carried on.
- **A human starting from the board is not gated.** The operator is not asked permission for their own click, which
  is the same position as the card shell.
- **The CLI names its card**, so an agent running `atrium proc start` through Bash is gated twice: once by the hook
  on the Bash call, once by the room. The recommended rule is a standing allow on `atrium proc`, since the room's
  check is the one that sees the real command. See Q5.
- **Stop is not gated.** Stopping a process is never how damage is done, and a gate on it would be a gate on the
  cleanup this exists to make easy. Stops are recorded on the row with who asked.

## Storage

One table, added at the end of the migration slice, written to tolerate already existing:

```sql
CREATE TABLE IF NOT EXISTS proc (
  id          TEXT PRIMARY KEY,
  task_id     TEXT NOT NULL,              -- the owner card. NOT a foreign key with cascade: see below
  name        TEXT NOT NULL,
  command     TEXT NOT NULL,
  args        TEXT NOT NULL DEFAULT '[]',
  cwd         TEXT NOT NULL,
  env         TEXT NOT NULL DEFAULT '{}',
  port        INTEGER,                    -- declared, or the one auto picked
  restart     TEXT NOT NULL DEFAULT 'never' CHECK (restart IN ('never','room-start')),
  stop_grace  INTEGER NOT NULL DEFAULT 5,
  started_by  TEXT NOT NULL DEFAULT '',   -- agent handle, or "you"
  created_at  TEXT NOT NULL,
  started_at  TEXT,
  pid         INTEGER,                    -- a hint for the log, never an identity
  exited_at   TEXT,
  exit_code   INTEGER,
  stopped_by  TEXT NOT NULL DEFAULT '',
  last_error  TEXT NOT NULL DEFAULT '',
  UNIQUE (task_id, name)
);
```

Not a cascading foreign key, because deleting a card must stop its processes before their rows go, and a cascade
deletes the rows first, leaving nothing to say what to stop. The card delete path stops, then deletes the rows.

**No `event` rows.** The event kind is a `CHECK` constraint, and widening it rebuilds the largest table in the
database, which `shell.go` already declined for the same kind of fact. A process's start and exit go to the row, to
the daemon log, and to the hub's audit through `emitLifecycle` with two new kinds, `process-start` and
`process-exit`. The hub whitelists lifecycle kinds, so that is a HUB-SIDE line.

**What is live is never stored.** Running or not is `procs` in memory, as a runner's activity is. The row says what
was asked for and how it last ended. A row that said `running` would be a lie the moment the room restarted.

## Out of scope

- **Restart on crash.** A supervisor that restarts on exit is a service manager, with backoff, crash-loop detection
  and a notion of healthy. That is a real feature and a different one (Q7).
- **Surviving a room restart in place.** ConPTY cannot reattach, and detached processes lose attach and safe stop
  (Q6).
- **Health checks.** "Is it serving" beyond "is the port bound" is the process's business.
- **Dependencies between processes.** "Start the hub, then the room" is a script the agent already writes.
- **Scheduling.** A process on a timer is a source (`sources.go`), which already exists.
- **Resource limits.** A job object could cap memory and CPU. Nothing has asked.
- **Starting a process on a machine with no room.** No room, no pty.
- **Adopting a process started outside atrium.** There is no pty to take over, and atrium cannot stop what it did not
  start safely. `atrium port` still names who holds a port, which is most of what adopting would buy.

## Build plan

Each stage lands on its own and is useful on its own.

**Stage 1: start, see, stop. The smallest useful thing.**
- The job-object spike, answered in this doc first.
- The `proc` table, the `procs` map, spawn and stop (the full tree sequence), shutdown ordering and narration, the
  halt check, the card-gone check.
- The HTTP endpoints for start, list, stop, logs, and the `attach?kind=proc` route. `atrium_proc_start`, `atrium_procs`, `atrium_proc_stop`,
  `atrium_proc_logs`.
- The permission gate on start, and the `processes = off` setting.
- Port check 1 (registry) and the bind probe (2 and 3 without the pid).
- Board: the chip strip on the card, with attach. No board-wide list yet.
- Guest allowlist tests.
- `restart` accepted and stored, always `never` in behavior. A room restart shows the row `stopped`.

What stage 1 fixes, against the five problems: 1, 2, 4 and 5 fully, and 3 through `atrium_procs`.

**Stage 2: the list, and coming back.**
- The board-wide list, fanned out by the hub across rooms.
- `restart: room-start` honored on room start, after runners reopen. Pinned-card default.
- The dead-card sweep holding a card with a running process. Done and delete asking first.
- `restart` and `forget` from the board. The CLI.

**Stage 3: ports the operating system knows about.**
- Excluded range explanation on refusal, `port: auto`, `GET /v1/ports/{port}`, `atrium port`.
- Discovered ports and bound address from the socket table, and naming the pid that holds a foreign port.

**Stage 4: the same tree stop for runners and shells.** Separate, because it changes how every agent stops.

## Review

Reviewed by Mercurius on 2026-09-24, session `s_Km0fBMBOcObM`, one round, reviewer codex (gpt-5.5). Verdict
`ready_to_build`, with no concerns and no questions. Two advisory notes, both folded in:

- **A1, the shape of `port`.** Input is an integer or `auto`, and stored and returned it is always an integer or
  null. Added under "HTTP, on the room".
- **A2, attach missing from the stage 1 checklist.** Stage 1 promised attach from the card chip, and its endpoint
  bullet did not list the route. Added.

Nothing was rejected. A clean verdict on a design review checks that the doc is buildable as written. It does not
check that the decisions are right, and those stay the Open Questions below.

## Open Questions

1. **Ship this, or live on the harness-row workaround?** A harness whose command is the dev server, launched with
   `atrium_launch`, works today with no code, at the cost of a card that lies about its state and no port. Default:
   build stage 1.
2. **Does a dead card with a running process stay on the board?** Default: yes, the sweep skips it until the
   processes stop. The alternative is stopping processes when the sweep archives the card, one minute after the
   session ends, which kills the docs server the orchestrator started the moment the orchestrator cycles.
3. **Marking a card `done` with processes running: ask, stop, or leave?** Default: ask, with stop preselected.
4. **What does a pin do?** Default: a pinned card's processes default to `restart: room-start`. The alternative is
   that pin means nothing here and `restart` is always explicit.
5. **The permission gate: `Bash` rules, or a tool of its own?** Default: gate as `Bash` with the full command, so
   existing rules apply. A new tool name (`Process`) is cleaner in the audit log and makes every existing rule
   miss. Also: is the CLI double gate acceptable with a standing allow on `atrium proc`?
6. **Should a process ever survive a room restart in place** (detached, no pty, no attach)? Default: no.
7. **Restart on crash, ever?** Default: out of scope, and restart happens once per room start only.
8. **Cross-card stop by agents.** Default: any agent may stop any process, recorded with who asked. The alternative
   is owner-card-only, with the human able to stop anything.
