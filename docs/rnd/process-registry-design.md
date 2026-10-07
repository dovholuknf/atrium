# Processes: a long-running background process an agent starts, owned by its card

Status: proposed, not built.

**Origin: design, nothing built.** Written 2026-09-24, revised the same day with clint's answers to the first round
of Open Questions. What is still open is at the end.

## Scope, said first

**This is for long-running background processes only.** `ziti edge quickstart`, a docusaurus dev server, a file
watcher, a throwaway hub for a test. It is what an agent reaches for **instead of a background shell of its own**
(Claude Code's `run_in_background`). It is not how an agent runs a build, a test, a `git` command or anything else that
finishes. Those stay Bash calls, gated by the hook the way they are today.

The test is one sentence, and it is the sentence the agent is taught: **if you would start it in the background and
come back to it later, start it with `atrium_proc_start`. If you wait for it to finish, run it with Bash.**

An agent learns that in two places:

- **The tool description** of `atrium_proc_start` opens with that sentence, names the examples above, and says what
  it is not for ("not for builds, tests or one-shot commands: those finish, run them with Bash").
- **`docs/atrium-for-agents.md`**, the doc agents are pointed at, gets a short section "Background processes" with
  the same rule, the lifetime in one paragraph (below), and how to find and ask the owner of a process.

Nothing in atrium enforces the line. A registered `go test` that finishes in four seconds is harmless: it shows on
the card, exits, and its row says `exited 0`. The line is taught, not policed.

## The problem

Today the orchestrator ran `npx docusaurus start --port 3310` in a background shell of its own. Five things are wrong
with that, and they are the same five every time:

1. **Nothing on the board shows it.** The operator cannot see that port 3310 is taken, or by what.
2. **Its lifetime is invisible.** Whether it dies with the session, or outlives it holding a port, depends on the
   harness, and nobody can see which happened.
3. **Nobody else knows its port.** A second agent that wants the docs site has to be told, or guesses, or starts a
   second copy on the same port and fails.
4. **Nobody else can find its owner.** A second agent that needs the port cannot tell who started the thing holding
   it, so it cannot ask. Once the session is gone, nothing stops it short of `taskkill` by a pid nobody recorded.
5. **Its output is private.** When it fails, the error is in a shell buffer only one session can read.

The ask: an agent registers the desire ("run this, in this directory, it serves port N") and atrium runs it, shows
it, and ends it with the card's runner.

## New thing, or an existing one with a new door

**An existing one with a new door, plus one table.** A registered process is the card shell
(`internal/daemon/shell.go`) with a command instead of an interactive prompt, several per card instead of one, and a
row in the store so the board can say how it last ended.

The three pseudo terminals atrium already owns, and why each is not the answer as it stands:

| | what it is | why it is not this |
| --- | --- | --- |
| runner (`supervisor.runners`) | the process doing a card's work | its exit files the card `dead`, the reaper and the park and shelving all act on it, and there is one per card |
| card shell (`supervisor.shells`) | one interactive shell beside the runner | one per card, no command, closed after 30 minutes with nobody attached |
| fixture (`store.Fixture`) | a runner that comes up with the daemon | it is a runner: it makes a card, has a harness, has a conversation to resume |

The runner is the wrong base. Every caller of `supervisor.get` means "the process doing the work", and
`docs/terminal/supervision-design.md` spends a section on why the shell was kept out of that map. A dev server in it would be
parked, reaped, and would mark its card dead on exit. That is exactly the mistake the shell avoided.

The shell is the right base. It already has everything a process needs from the supervisor: a `runner` struct with a
pty, a ring buffer, a watcher fan-out, attach over `attach.go`, and a hangup-then-kill close in `CloseShell`. What it
lacks is a command, a name, a port, a lifetime tied to the runner rather than to "nobody is looking", and a row. So
the design is a **third map on the supervisor, `procs`, keyed by process id**, reusing the `runner` struct and the
shell's spawn and close paths, with lifecycle rules of its own.

`shell.go` argues "two terminals, not N", because a list needs naming, ordering and a picker. That argument holds for
shells and does not transfer: a process is named by whoever started it, and the list IS the picker. Nobody opens a
process to type at it. They open it to see why it died.

### The workaround that exists today

A harness row whose command is `npx docusaurus start --port 3310`, launched with `atrium_launch`, already gives a
card, a pty, attach, and a stop. It costs a card that lies. The card sits in `running`, goes `dead` on exit and is
archived a minute later, carries no port and no owner, and clutters the columns meant for human attention. It is a
fine stopgap for one server and the wrong shape for the pattern.

## Where it lives: the room

The room owns the pseudo terminals (`docs/archive/hub-room-plan.md`), so the room owns processes. The hub owns none and
restarts all evening. A process started from a hub would die with every CSS change.

- **ROOM-SIDE:** the `procs` map, the store table, the spawn and stop paths, the port checks, the loopback recipes
  and the stable executable path, the HTTP endpoints, and the runner-exit and shutdown stops.
- **HUB-SIDE:** the MCP tools on `atrium-control` (served by the hub, `internal/link/control_mcp.go`), the lifecycle
  kinds the hub whitelists for its audit log, the cross-room fan-out of the list and the port lookup, and the board.

Card-scoped routes (`/v1/tasks/<id>/...`) already route to the room holding the card through `proxy.go`, so the
endpoints below get cross-room routing for free by living under a card.

## Lifetime

One rule: **a process never outlives its card's runner.** A card cannot die and leave its processes behind.

| event | what happens to the card's processes |
| --- | --- |
| a turn ends (`needs-input`, "ready for more input") | nothing. They keep running. A turn ending is the agent waiting, not the agent gone |
| `/clear`, or a context cycle inside the same runner | nothing. The runner process is the same one |
| the runner exits, is stopped, or dies | **stopped**, the full tree sequence below, before the card is filed `dead` |
| the card is shelved | stopped. Shelving stops the runner (`shelve.go`), so this is the row above |
| the card is deleted or pruned | stopped, because the runner is. The card-gone check covers any path that skips it |
| the room shuts down or restarts | stopped, bounded and narrated. See "Shutdown". Nothing starts them again |
| the room is killed | the whole tree dies with it, through the job object (see "Stopping a tree") |
| the hub restarts | nothing. The hub owns no process |

**The runner exit is the trigger, whoever saw it.** Three paths notice a runner is gone: `awaitExit` on a runner
atrium spawned, the reaper on a card whose pid it knows (`reaper.go`), and the orphan pass (`orphans.go`). Each
already moves the card to `dead`. Each calls `stopProcsFor(task)` first, and the processes stopping is logged before
the status changes, so the log reads in the order it happened.

**Cards atrium does not own a runner for.** A window-mode card or a session joined with `/atrium-join` has no runner
in `supervisor.runners`. Its runner, for this rule, is the pid on the card, which the reaper already watches. A start
on such a card is accepted only when the card has a pid, and the reaper stopping it is the trigger. A card with no
pid refuses a start with "this card has no session atrium can watch, so a process would have nothing to end with".
See Q1.

**A room restart stops them and nothing brings them back.** There is no `restart` field and no pin behaviour. It is
the card's job to notice and start them again. The card learns it two ways:

- The rows stay, with `stopped_by` set to `room-restart`. `atrium_procs` shows them as stopped, with why.
- A card whose runner is reopened after the restart, and that had processes running when the room went down, gets a
  line in its restart wake (`[atrium] restart wake: ...`, the existing grey label): "stopped by the restart: docs
  (:3310), test-hub (:7900). start them again if you still need them."

Starting one again is `atrium_proc_start` with the same name. The name is free because the old one is stopped, and
the new start replaces the row's command, args, cwd and port with what it was given.

**No restart on crash.** A process that exits is shown exited, with its code and its last lines. Nothing starts it
again. A supervisor that restarts on exit is a service manager, with backoff, crash-loop detection and a notion of
healthy, and that is not this.

**Idle timeout: none.** The shell closes after 30 minutes unattached because an idle prompt is something the operator
forgot. A dev server with nobody attached is the normal case, and the runner exit bounds its life anyway.

**Why not survive a room restart.** A process in a pty dies with the room, like every runner. ConPTY offers no
reattach (`docs/archive/architecture-v2.md`, "Open risks"). A detached process (no pty, output to a file, a recorded pid)
could survive, and it would have no attach, a stop keyed on a recycled pid, and no job object to make the stop safe.
It would also break the one rule, since the runner does not survive the restart either.

## Who may stop a process

**The owner card's agent, and the human from the board or a plain terminal.** No other agent.

Any other agent can find out **who** started a process and ask. Every place a process is shown names its owner: the
owner card's id and title, and the owner's handle (the name `atrium_say` takes). So the path for a second agent that
needs port 3310 is:

1. `atrium_proc_start` refuses, or `atrium port 3310` / `atrium_procs` answers, with "3310 is held by `docs` on card
   sa51 (handle `atrium-51022`), running 40m".
2. The agent calls `atrium_say` to `atrium-51022`: "I need :3310, can you stop `docs`, or is it the docs server I
   want anyway?"
3. The owner stops it, or says to reuse it.

If the owner is gone, so is the process, by the lifetime rule. That is what makes owner-only safe: the case that
made cross-card stop tempting, a server left holding a port after its session ended, cannot happen.

`atrium_proc_stop` on another card's process is refused as a tool result (not an MCP error) that names the owner and
the handle and says to ask through `atrium_say`. The same text is in the tool's description, so an agent knows before
it tries. Stops are recorded on the row with who asked.

The CLI follows the same line: run from a card's environment (`ATRIUM_TASK_ID` set) it may stop that card's
processes only. Run from a plain terminal it is the operator and may stop any. This is a convention on a loopback
API with no auth, the same position every other control call is in. An agent that strips its environment can
pretend to be the operator, exactly as it can already `curl` the board.

## The surface

### HTTP, on the room

```
POST   /v1/tasks/{id}/procs             start one        { name, command, args?, cwd?, port?, env?, bind? }
                                                          -> { proc }       201, or 409 with the reason
GET    /v1/tasks/{id}/procs             this card's processes
GET    /v1/procs                        every process in this room (the hub fans it out across rooms)
GET    /v1/procs/{proc}                 one, with its row, its owner and live state
POST   /v1/procs/{proc}/stop            { grace? }  -> { proc }  owner card or operator only
GET    /v1/procs/{proc}/logs?lines=N    the tail of its ring buffer, ANSI stripped, bounded
DELETE /v1/procs/{proc}                 stop it if running, and forget the row. Owner card or operator only
GET    /v1/tasks/{id}/attach?kind=proc&proc={proc}
                                        the existing websocket, pointed at a process's pty
GET    /v1/ports/{port}                 is this port usable here, and if not, why and who holds it
```

The caller on stop and delete is the card in `X-Atrium-Agent` when the call comes through the control tools, and the
operator otherwise, the same way every control call already tells an agent from a human.

`{proc}` is atrium's own id, a text ULID like every other key. It is never the operating system's pid, which is
recycled. Start lives under the card because a process always has an owner card.

`port` on a start is an integer or the literal `auto` (stage 3). On the stored row and on every process returned, it
is always a concrete integer, the one asked for or the one auto picked, or null when none was declared.

`bind` is `loopback` (the default) or `any`. See "Firewall prompts".

`name` is required and unique per card: `docs`, `test-hub`, `watch`. A second start with a name that is already
running answers `409` and names the running one. It never starts a twin. That is the idempotency the shell has in
`EnsureShell`, for the same reason: an agent asking twice meant the same thing both times.

`cwd` defaults to the card's worktree and must be inside a root `browseroots.go` allows. `command` is resolved with
`exec.LookPath` before `Dir` is set, and scripts go through `viaShellIfScript`, exactly as `spawnPTYResume` does.
`npx` is a `.cmd` on Windows and fails the same way `claude` would without that.

**`GET /v1/ports/{port}` names the owner.** The answer is one of: free; held by a registered process, with the
process, its owner card, the card's title and the owner's handle; reserved by Windows, with the range; or held by
something outside atrium (stage 3 adds its pid and image name). The hub fans the question out across rooms on the
same machine, because a port is a machine fact, not a room fact.

### MCP, on `atrium-control`

Four tools, named to sort beside the others:

| tool | does |
| --- | --- |
| `atrium_proc_start` | start a long-running background process on the caller's own card. Takes `name`, `command`, `args`, `cwd`, `port`, `bind`. Returns the process, its port, its bind, and where to see it |
| `atrium_procs` | every process in reach: name, owner card, owner handle, command, port, bind, state, uptime, exit code, and why it stopped. Call before starting, to find one already up |
| `atrium_proc_stop` | stop one of your own card's processes, by name or id |
| `atrium_proc_logs` | the last N lines of any process, 200 by default, 2000 at most |

The owner is the calling session's card, read from `X-Atrium-Agent` the way every control tool already reads it. An
agent cannot start a process on another card and cannot stop one.

The descriptions carry the rules, because they are what an agent reads:

- **`atrium_proc_start`:** "For long-running background processes only: a dev server, `ziti edge quickstart`, a
  watcher. Use this instead of running a command in the background yourself. Not for builds, tests or anything you
  wait on: run those with Bash. The process is on the board, binds loopback unless you pass `bind: any`, and is
  stopped when your session ends. A room restart stops it too, and you must start it again. Call `atrium_procs` first:
  the thing you want may already be running."
- **`atrium_procs`:** "Every process shows its owner card and the owner's handle. To get a process stopped that is not
  yours, ask its owner with `atrium_say`. You cannot stop it yourself."
- **`atrium_proc_stop`:** "Stops a process on your own card. For anyone else's, this refuses and names the owner's
  handle: ask them with `atrium_say`."

`atrium_proc_logs` returns output, which `atrium_task` says atrium never does. The difference is deliberate. A
runner's output is a conversation and the way to learn what it thinks is to ask it. A process cannot be asked, and
"why did the dev server exit" is answered only by its last lines. The tail comes from the in-memory ring, is bounded,
and is never written anywhere. Any agent may read any process's tail, because reading does no harm and the reader is
usually the one deciding whether to ask the owner.

### CLI

```
atrium proc start --name docs [--port 3310] [--cwd .] [--bind any] -- npx docusaurus start --port 3310
atrium proc ls [--all]
atrium proc stop <name|id>
atrium proc logs <name|id> [-n 200] [-f]
atrium port 3310
```

The CLI finds the card from `ATRIUM_TASK_ID` the way hooks do. With no card in the environment, `start` refuses:
every process has an owner, and a CLI in a plain terminal has none to give it. `-f` follows over the attach
websocket, read-only.

## What the board shows

**On the card:** a strip of chips under the title, one per process. Each chip is the name, the port, and a dot: green
running, grey stopped, red exited non-zero, and an amber mark when it listens on a non-loopback address. `docs :3310
●`. Clicking a chip opens its terminal in the same pane the card shell uses. The strip is hidden when the card has
none.

**A board-wide list**, reached from the header, because "what is holding 3310" is not a question about any one card.
One row per process across every room:

| column | from |
| --- | --- |
| name | the start request |
| room | the room that owns it |
| owner | the card, linked, and the owner's handle |
| command and cwd | the row, command shown in full on hover, the stable path when one was used |
| port | declared, with discovered beside it when they differ (stage 3) |
| bind | loopback, or the non-loopback addresses it listens on, with the warning |
| state | starting, running, stopping, stopped, exited, lost |
| uptime or exit | "12m", or "exited 1 after 4s" with the first line of its last output, or "stopped by room-restart" |
| actions | open, stop, forget |

The operator can stop anything from here. There is no restart action: a human who wants it back starts it again, or
asks the owner to.

The port renders as a link only when the board is served from the process's own machine. From a hub on another
machine, `localhost:3310` is the wrong machine, so the port is shown as text.

**Attach works.** A process is a pty and the attach path already carries one, so opening a process is attaching to
it, read and write. A dev server takes `r` to reload and `q` to quit, and a test hub may prompt. Everything
`docs/terminal/supervision-design.md` says about sizing and replay applies unchanged.

The attach route grows a parameter (`kind=proc`), which `internal/daemon/CLAUDE.md` calls a new endpoint. It is
refused to a guest by the existing `kind != ""` check in `overlay_guest.go`. That check is load-bearing now and gets
a test that says so.

## Ports

### Declared, and checked before the start

The start request may carry `port`. When it does, the room checks it **before spawning**, and a start that would fail
on the port is refused with the reason:

1. **Held by a registered process.** Looked up in the registry, across rooms on this machine. The refusal names the
   owner and the handle: "3310 is held by `docs` on card sa51 (handle `atrium-51022`, running 40m). ask them with
   atrium_say, or reuse it: it is the same command." When the command and cwd match, the answer says so, because the
   usual case is a second agent wanting the same docs server.
2. **Reserved by the operating system.** On Windows, the TCP excluded port ranges (`netsh int ipv4 show
   excludedportrange protocol=tcp`). On this machine 50260-50359 is reserved, and binding there fails with
   `WSAEACCES`. Docusaurus words the same failure as "something is already running on port". The refusal says
   "reserved by Windows (range 50260-50359)".
3. **Held by something else.** A bind probe on the port. `EADDRINUSE` means taken. Stage 3 names the owning pid with
   `GetExtendedTcpTable` on Windows or `/proc/net/tcp` on Linux.

**The probe is the test and the range list is the explanation.** A bind probe answers checks 2 and 3 together:
`WSAEACCES` is reserved, `WSAEADDRINUSE` is taken. `netsh` is read only to word the refusal and to steer the auto
picker away, and it is read once a minute at most, because Hyper-V and WSL move the ranges at boot.

The probe binds `127.0.0.1` only. A probe on `0.0.0.0` is itself a non-loopback listen by atrium's own executable,
which is one firewall prompt per atrium build path, the thing the next section exists to stop. A process that binds
wide while something else holds the loopback address still loses, and that shows up as the process exiting with the
bind error in its tail. The probe is a check at one instant in any case, so a race can still lose the same way.

### Auto

`port: "auto"` asks the room to pick one. It binds `127.0.0.1:0` until the answer is outside every excluded range and
not in the registry, then passes it to the process as `PORT` in the environment and substitutes `{port}` in `args`.
The orchestrator's preview-port rule (50000 + backlog item) becomes a preference the caller can still pass as a
number.

### Discovered

What a process says it serves and what it listens on can differ. Webpack moves to 3311 when 3310 is taken, and a
test hub picks its own. The room asks the operating system which listening sockets belong to the process tree
(`GetExtendedTcpTable`, filtered to the job's pids), every five seconds for the first minute and then every minute.
Discovered ports count as held for check 1. The same read gives the **bound address**, which is what the firewall
warning needs, so that part lands in stage 2 and the port reconciliation in stage 3.

This reads the operating system, never the output. Parsing "Local: http://localhost:3310" out of a log is the
output-interpretation `docs/archive/architecture-v2.md` rules out.

## Firewall prompts: why clint asked

Windows Defender Firewall prompts for every **new executable path** that listens on a non-loopback interface, and
records the answer as an inbound rule per path. A listen on `127.0.0.1` or `::1` never prompts. The rules already on
this machine (`Get-NetFirewallApplicationFilter`, counted 2026-09-24, two rules per prompt, TCP and UDP) show where
the prompts come from:

| source | rules | why each one is new |
| --- | --- | --- |
| `ziti.exe` in per-worktree `build.claude\` folders | 34 | `ziti edge quickstart` binds every interface by default, and every worktree is a new path |
| a dozen other `build.claude\` binaries (`atrium.exe`, `atrium-preview.exe`, `ziti-red.exe`, `laptopsrv.exe`, ...) | 2 each | the same, one prompt per worktree per binary |
| go test binaries in `C:\Users\claude\AppData\Local\Temp\go-build*\` (`run.test.exe`, `tests.test.exe`) | 10 | a new random directory on every build |

So the prompt is caused by two things together: a listener on a non-loopback address, and a path the firewall has
not seen. Removing either removes the prompt. The registry attacks both for what it runs, in this order.

### 1. Loopback by default

A process started through the registry binds loopback unless the caller passes `bind: any`. Atrium cannot force a
bind address on an arbitrary program, so this works through **recipes**: a short table in the room, keyed on the
resolved executable's base name and leading arguments, that adds the tool's own flag or environment variable when the
caller has not already set it.

| tool | matched on | loopback via |
| --- | --- | --- |
| docusaurus | `docusaurus start` (through `npx`, `npm run`, or direct) | `--host 127.0.0.1` |
| vite | `vite`, `vite dev` | `--host 127.0.0.1` |
| webpack-dev-server | `webpack serve`, `webpack-dev-server` | `--host 127.0.0.1` |
| ziti quickstart | `ziti edge quickstart` | environment for the controller and router bind addresses. **The exact names are to be verified against openziti/ziti `main` in stage 1**: the config templates read bind-address variables, and whether `quickstart` honors them for every listener (controller edge API, control plane, router edge and link) is the question |
| atrium2 hub or room | `atrium2 hub`, `atrium2 room` | explicit `127.0.0.1:` addresses on `--link`, `--addr`, `--http` |

Rules for the table:

- **A caller's own flag wins.** If the args already carry `--host` (or the recipe's variable is in `env`), the recipe
  adds nothing.
- **What was added is on the row.** The row stores the args and env as run, and the tool result says "added `--host
  127.0.0.1` (loopback default; pass bind: any to skip)". An agent is never surprised by a flag it did not write.
- **`npm run` is matched only when the script is resolvable.** The room reads the script from `package.json` in
  `cwd`, matches the recipe against it, and appends the flag after `--`. A script it cannot read gets no recipe.
- **No recipe means no change.** A command the table does not know runs as given. The warning below is what catches
  it.

The table is data in one file, with a test per row that the flag lands where the tool reads it. Adding a tool is one
row.

### 2. A stable path per project for binaries it runs

For a process that must listen wide, or a tool with no loopback switch, the firewall keys on the path, so the fix is
to make the path stable.

When `command` resolves to an executable under a worktree (the per-worktree `build.claude\` case), the room copies it
to **`%LOCALAPPDATA%\atrium\bin\<project>\<name>.exe`** and runs that copy, with `cwd` unchanged. `<project>` is the
repository, named from the git common directory, so every worktree of a repo shares it. One firewall rule for
`...\bin\openziti-ziti\ziti.exe` then covers every quickstart from every worktree.

- **Only worktree binaries are copied.** `node.exe`, `go.exe` and anything else from a stable install already have one
  path. Copying them gains nothing and would break a tool that finds its siblings relative to itself.
- **The copy is replaced only when it differs** (size and hash), and only when no registered process is running it. A
  second card that needs a different build while the first runs gets the original path and a line in the tool result
  that says so: "running from the worktree path, the stable copy is in use by `quickstart` on card sa51". That one
  run may prompt. Sharing one path across two builds by force would be worse.
- **The row records both paths**, so the board and the log say what actually ran.
- **A tool that loads files beside its executable** breaks when moved. The copy is opt-out per start (`stable_path:
  false`) and per recipe row. `ziti.exe` is a single static binary and is safe to move.

### 3. A warning when a process listens wide

From stage 2, the socket read in "Discovered" reports each process's bound addresses. Any listen on an address other
than `127.0.0.1` or `::1` puts an amber mark on the chip and the row, with the addresses: "listening on 0.0.0.0:1280,
0.0.0.0:6262 (reachable from the LAN, may prompt the firewall)". The tool result for `atrium_procs` carries the same
field, so the agent that started it sees it too. The warning does not stop the process. It says what is true.

### Outside the registry

These cause prompts too, and none of them is the registry's to fix. Each is its own item.

**(a) Atrium's own listeners that bind every interface.** An address with no host (`:7777`) is a wildcard bind in Go.

| where | flag default | today |
| --- | --- | --- |
| `internal/cli/cli.go:143` | `--addr ":7777"`, the agent listener | wildcard, served by `daemon.go:869` |
| `internal/cli/cli.go:144` | `--http ":7778"`, the human listener | wildcard, served by `daemon.go:885` |
| `internal/cli/cli.go:231` | `--addr ":7777"` | wildcard |
| `cmd/atrium2/hub.go:375`, `hubrooms.go:178`, `hubrooms.go:219` | `--link ":7801"`, where rooms dial in | wildcard, bound by `internal/link/direct.go:94`, though the advertised address is already derived as `127.0.0.1` |

The hub board (`--addr ":7800"`) is already coerced to loopback by `loopbackBoard`. The recommendation is the same for
the rest: default to `127.0.0.1:<port>`, and keep a wide bind available by saying so explicitly, as the hub link
already requires `--link-advertise` for one. A room on another machine then needs `--link 0.0.0.0:7801` on the hub,
which is the case where a prompt is expected and wanted. The advertise derivation is unchanged by this. This is a
separate change with its own review, because it changes what a remote room can reach by default.

**(b) go test binaries.** Each `go test` links `<pkg>.test.exe` into a fresh `go-build<random>\` directory, so every
run is a new path.

- **`GOTMPDIR` does not help.** It moves where the random directory is made, and the directory is still random.
- **`go test -c -o <stable path>` then running the binary does help**, one path per package. It costs the test cache,
  changes how every agent runs tests, and a stable path per worktree is still a new path per worktree. Not worth it.
- **The fix that holds is the listen address in the test.** A test that listens on `127.0.0.1:0` never prompts,
  whatever its path. Atrium's own tests already do (`net.Listen("tcp", "127.0.0.1:0")` throughout `internal/link` and
  `internal/daemon`). The names in the rules, `run.test.exe` and `tests.test.exe`, match no atrium package, so the
  prompts come from another repo's tests or from a test that starts a server with a wildcard default. The item is:
  find the tests that bind wide, in whichever repo, and make them bind loopback.

**(c) The machine-wide switch.** `Set-NetFirewallProfile -NotifyOnListen False` turns the prompt off for a profile.
A program with no rule is then blocked inbound on non-loopback addresses without a word, so a LAN client that should
reach a dev server fails silently instead of prompting. Loopback is unaffected. That trade is **clint's decision,
not atrium's**, and atrium never runs it.

## Resilience

The daemon guarantees in `CLAUDE.md`, one by one.

**Storage failure halts.** The process row is written before the process is spawned, the same order a launched card
follows. A failed write is a tier-3 failure and halts the room like any other. The halt stops every process along
with the runners, because a registry that cannot record a stop cannot say what is running. And **the start endpoint
checks the halt itself.** The agent listener closes on a halt, but the human listener stays up, and the MCP tools
reach the room through the hub over that human listener. A crash between the row and the spawn leaves a row with no
pid, which the next room start shows as `lost`.

**A kill is not a stop, and a stop must take the tree.** Covered in its own section below, because today no stop path
in atrium does it.

**Shutdown is bounded and narrated.** `stopSupervised` stops runners first, then processes. A runner mid-turn may be
running tests against the test hub, and pulling the hub out first turns a clean exit into a failing test run. Each
runner's processes stop right after that runner, by the lifetime rule, so on shutdown this is the same code path.
Processes stop in parallel, each with its own grace (default 5s, a per-row `stop_grace` capped at 10s so no row can
extend shutdown past the runners' budget), and each step logs: "stopping 3 process(es), up to 5s each", "docs exited
when asked", "test-hub did not exit, ending its tree". Each row gets `stopped_by = room-restart` or `room-stop`, and
the runner's carryover records which processes it had, so the restart wake can name them.

**Nothing here may fail a session.**

- A process exiting never touches its owner card's status. This is the rule the shell exists to keep, and it is why
  processes are not in `runners`. `awaitProcExit` is `awaitShellExit` plus a row update, never `awaitExit`.
- The runner exit waits for its processes to stop before the card is filed `dead`, bounded by the same grace. A
  process that will not die delays the status by at most its grace plus four seconds, then the card moves on and the
  process is logged as "still holding <cwd>".
- A failed start or a refused stop is a tool result with the reason, never an MCP error.
- Nothing about processes rides a hook. `/activity`, `/session`, `/permission` and `/stop` are unchanged.
- The proc tools have their own timeout. The 8s `controlTimeout` fits a status read, not a start that is waiting on a
  human to approve it (see "The permission gate").

### Stopping a tree

Today every stop in atrium ends one process. `windDown` and `CloseShell` write keys, close the pty, then
`cmd.Process.Kill()`, which on Windows is `TerminateProcess` on the direct child only. For `npx docusaurus start` the
child is `cmd.exe` running `npx.cmd`, whose child is `node`, whose children are webpack workers. Closing the pseudo
console sends a close event to every process attached to it, which catches most of that tree. It misses anything
that detached or opened its own console, and any child that ignores the event. The survivor holds port 3310 with no
row pointing at it.

So a process is started inside a **Windows job object** with `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`, and the stop ends
with `TerminateJobObject`, which ends every process in the job whatever it did with its console.

- **A kill of the room ends every process tree too**, because the job handle closes with the room. That is the
  lifetime rule holding even when nothing gets to run a stop.
- **The child must be in the job before it spawns anything.** Assigning after `Start` races a child that spawns in
  its first milliseconds, and `go-pty` does not expose `CREATE_SUSPENDED`. So the pty does not run the command. It
  runs a **gated launcher**, and the launcher runs the command:

  1. The room creates the job and holds its handle.
  2. The room starts the launcher in the pty: atrium's own executable with a hidden subcommand (`atrium2
     proc-exec`), given the resolved command, args and env. The launcher starts nothing. It blocks reading one byte
     from a pipe the room passed it.
  3. The room calls `AssignProcessToJobObject` on the launcher's pid, then writes the byte.
  4. The launcher starts the command as its child, which inherits the job. It ignores ctrl-c itself so the command
     gets it, waits for the command, and exits with its code.

  **The invariant: no process of the tree runs before the job holds the launcher.** The launcher cannot spawn before
  the byte, and the byte is written only after the assignment succeeds. **The fallback is refusal:** if the
  assignment fails, the room kills the launcher (which has started nothing) and the start fails with the reason. A
  process that is not in a job never runs. Assigning after start and sweeping the toolhelp snapshot for stragglers,
  and patching `CREATE_SUSPENDED` into `go-pty`, are both rejected: the first leaves a window the lifetime rule
  cannot cover, and the second is a fork to carry. The launcher is atrium's own executable at its install path and
  never listens, so it adds no firewall prompt.

On Linux and macOS the pty child already leads its own session, so the stop signals the process group: SIGINT,
SIGTERM, then SIGKILL to `-pgid`. A child that calls `setsid` escapes that. A cgroup would catch it, out of scope
until a room runs on Linux for real.

The full stop sequence, per process:

1. Write ctrl-c to the pty. The console delivers it to the whole console-attached tree. Wait up to the grace.
2. Close the pty. Wait two seconds.
3. Terminate the job (or the process group). Wait two seconds for the handles to release, as `CloseShell` does,
   because on Windows a directory with a process in it cannot be removed, and deleting a card is often followed by
   removing its worktree.
4. Log each step that was needed. A process still up after 3 is logged as "still holding <cwd>".

**The runner and the shell get the same job object in a later stage.** They have the same tree problem. That is a
separate change with its own risk (stage 4).

## Security

**Loopback, no auth, unchanged.** Anything that can reach a room's loopback board can already start a runner (`POST
/v1/launch`) and open a card shell and type into it. A process start adds no new class of access on this machine.

**The full board over an overlay** (zrok or OpenZiti, `docs/fabric/overlays.md`) hands over everything, shells included, so
processes are in the same position as shells there. Processes honor an off switch the way shells honor `shell command
= off`: a `processes` setting, `off` meaning the room refuses every start with the reason. The endpoint refuses, not
only the board, for the reason `EnsureShell` gives: an old tab or a script asks anyway.

**A lent session** (`overlay_guest.go`) is an allowlist, and nothing here is added to it. A guest gets 403 from every
`/v1/procs` and `/v1/ports` path and from `/v1/tasks/<id>/procs`, and the attach route refuses `kind=proc` via the
existing `kind != ""` check. A test pins each of those. The guest can still reach processes one way: the guest drives
the agent, and the agent can call `atrium_proc_start`. That is why the gate below matters.

**Binding wide exposes a process to the LAN.** Loopback is the default for that reason as much as for the firewall.
`bind: any` is the caller saying so, it is on the row, and the warning shows what the process really bound.

**Environment.** A process gets the room's scrubbed launch environment plus `ATRIUM_TASK_ID` and `ATRIUM_PROC_ID`,
and **not `ATRIUM_AGENT_NAME`**, for the reason `shellEnv` gives: a claude started from a process would otherwise file
activity as the agent that started it. `env` in the start request is added on top and stored on the row, so the tool
description says not to pass secrets in it. The row is visible on the board.

### The permission gate

**Registering a process goes through the permission chain.** Without that, the proc tool is a way around it.

The PreToolUse hook does not gate `mcp__*` tools (`docs/archive/architecture-v2.md`, "What carries over from v1", item 6),
because gating the agent's own control tools is circular. So a session told "never `npx`" by a standing rule could
run `npx` anyway by asking atrium to.

So the room gates the start itself. It builds the request the hook would have sent, **tool `Bash` and the full
command line as it will run, recipe flags included**, on the owner card, and runs it through `onPermRequest`. The
whole chain applies in order: a replayed decision (the dedup key is a hash of name, command, args and cwd), a queued
message, a shelved card's standing no, a standing rule, auto mode, and finally asking the human, with the card in
`needs-permission`. Using `Bash` rather than a new tool name keeps every existing `Bash` rule applicable. The
request's `details` say it is a background process start, with the port, the bind and the stable path, so the human
deciding sees what they are approving.

- **The tool call blocks until the chain answers**, the way the hook does, so `needs-permission` on the card is true.
- **A human starting from the board is not gated.** The operator is not asked permission for their own click.
- **The CLI names its card**, so an agent running `atrium proc start` through Bash is gated twice: once by the hook on
  the Bash call, once by the room. The recommended rule is a standing allow on `atrium proc`, since the room's check
  is the one that sees the real command. See Q2.
- **Stop is not gated.** It is already limited to the owner and the operator, and a gate on it would be a gate on the
  cleanup.

## Storage

One table, added at the end of the migration slice, written to tolerate already existing:

```sql
CREATE TABLE IF NOT EXISTS proc (
  id           TEXT PRIMARY KEY,
  task_id      TEXT NOT NULL,              -- the owner card. NOT a foreign key with cascade: see below
  name         TEXT NOT NULL,
  command      TEXT NOT NULL,              -- as resolved
  run_path     TEXT NOT NULL DEFAULT '',   -- the stable copy that ran, or '' when the command ran in place
  args         TEXT NOT NULL DEFAULT '[]', -- as run, recipe flags included
  cwd          TEXT NOT NULL,
  env          TEXT NOT NULL DEFAULT '{}', -- as run, recipe variables included
  port         INTEGER,                    -- declared, or the one auto picked
  bind         TEXT NOT NULL DEFAULT 'loopback' CHECK (bind IN ('loopback','any')),
  recipe       TEXT NOT NULL DEFAULT '',   -- which recipe row applied, if any
  stop_grace   INTEGER NOT NULL DEFAULT 5,
  started_by   TEXT NOT NULL DEFAULT '',   -- agent handle, or "you"
  created_at   TEXT NOT NULL,
  started_at   TEXT,
  pid          INTEGER,                    -- a hint for the log, never an identity
  exited_at    TEXT,
  exit_code    INTEGER,
  stopped_by   TEXT NOT NULL DEFAULT '',   -- a handle, "you", "runner-exit", "room-restart", "room-stop"
  last_error   TEXT NOT NULL DEFAULT '',
  UNIQUE (task_id, name)
);
```

**A row is written only after the permission gate approves and the port checks pass**, immediately before the
spawn. A denied or refused start writes no row and lives in the tool result and the daemon log. So an old row reads
one of three ways: `started_at` null is `lost` (written, never spawned), `exit_code` set with `stopped_by` empty is
an exit on its own, and `stopped_by` set is a stop, with who or what asked.

The owner's handle is not stored. It is read from the card when a process is shown, because a card's handle is the
card's fact.

Not a cascading foreign key, because deleting a card must stop its processes before their rows go, and a cascade
deletes the rows first. The card delete path stops, then deletes the rows.

**No `event` rows.** The event kind is a `CHECK` constraint, and widening it rebuilds the largest table in the
database. A process's start and exit go to the row, to the daemon log, and to the hub's audit through `emitLifecycle`
with two new kinds, `process-start` and `process-exit`. The hub whitelists lifecycle kinds, so that is a HUB-SIDE
line.

**What is live is never stored.** Running or not is `procs` in memory. The row says what was asked for and how it
last ended.

## Out of scope

- **Short-lived commands.** Builds, tests, one-shot scripts. Bash runs those.
- **Restart on crash**, and any restart atrium starts on its own.
- **Surviving a room restart**, in place or detached.
- **Outliving the runner.** A process that should run with no session behind it is a fixture's job, or a service's.
- **Health checks.** "Is it serving" beyond "is the port bound" is the process's business.
- **Dependencies between processes.** "Start the hub, then the room" is a script the agent already writes.
- **Scheduling.** A process on a timer is a source (`sources.go`).
- **Resource limits.** A job object could cap memory and CPU. Nothing has asked.
- **Adopting a process started outside atrium.** There is no pty to take over. `atrium port` still names who holds a
  port.
- **Atrium's own listeners, go test binaries, and the firewall notify switch.** Listed under "Outside the registry",
  each its own item.

## Build plan

**Stage 1 waits for the one-atrium consolidation** (sa57, `docs/archive/one-atrium-plan.md`, on its own branch today). Both
change the CLI and the board, and the plan decides which binary and which command tree `atrium proc` lives in. Stage
1 starts after that plan's CLI and board changes land, and is rebased onto them rather than built beside them.
Atrium's own listener defaults (item (a) above) are independent and may land any time.

Each stage lands on its own and is useful on its own.

**Stage 1: start, see, stop.**
- The job object and the gated launcher (`atrium2 proc-exec`), with a test that a child spawned in the command's
  first millisecond is in the job, and a test that a failed assignment starts nothing.
- The ziti quickstart bind variables, verified against openziti/ziti `main` and written into the recipe row. The row
  lands with a test per listener it moves to loopback. A listener it cannot move is named in the row's note and left
  to the wide-bind warning, so the recipe never claims more coverage than it has.
- The `proc` table, the `procs` map, spawn and stop (the full tree sequence), the stop on runner exit from all three
  paths, shutdown ordering and narration, the halt check, the card-gone check.
- Owner-only stop, and the owner card and handle in every answer.
- The HTTP endpoints for start, list, stop, logs, and the `attach?kind=proc` route. `atrium_proc_start`,
  `atrium_procs`, `atrium_proc_stop`, `atrium_proc_logs`, with the descriptions above.
- The permission gate on start, and the `processes = off` setting.
- Loopback recipes and `bind`. The stable path for worktree binaries.
- Port check 1 (registry) and the loopback bind probe (2 and 3 without the pid).
- The restart wake line naming processes the restart stopped.
- Board: the chip strip on the card, with attach.
- Guest allowlist tests.
- The "Background processes" section in `docs/atrium-for-agents.md`.

What stage 1 fixes, against the five problems: all five, with 3 and 4 answered through `atrium_procs`.

**Stage 2: the list and the warning.**
- The board-wide list, fanned out by the hub across rooms.
- The socket read for bound addresses, and the non-loopback warning on chip, row and tool result.
- `forget` from the board. The CLI.

**Stage 3: ports the operating system knows about.**
- Excluded range explanation on refusal, `port: auto`, `GET /v1/ports/{port}` fanned across rooms, `atrium port`.
- Discovered ports reconciled with declared ones, and naming the pid that holds a foreign port.

**Stage 4: the same tree stop for runners and shells.** Separate, because it changes how every agent stops.

## Review

**Round 1**, 2026-09-24, Mercurius session `s_Km0fBMBOcObM`, reviewer codex (gpt-5.5), on the first draft. Verdict
`ready_to_build`, two advisory notes, both folded in: the shape of `port` (A1), and attach missing from the stage 1
checklist (A2).

**Round 2**, 2026-09-24, Mercurius session `s_Klruhz3XfqAr`, reviewer codex (gpt-5.5), on this revision. Verdict
`needs_changes`, one concern and one advisory note, both folded in:

- **C1 (blocker), the job-object spike was still open** while the lifetime rule now leans on it harder. Resolved in
  "Stopping a tree": a gated launcher assigned to the job before it may spawn, refusal as the fallback, and the
  sweep and the `go-pty` patch rejected.
- **A1, the Open Questions read as blocking.** Both have defaults the build follows. Said so under the heading.

**Round 3**, 2026-09-24, Mercurius session `s_xxMe17s5hfKt`, reviewer codex (gpt-5.5), to confirm the C1 fix. Verdict
`ready_to_build`, no concerns and no questions, so the gated launcher holds. Two advisory notes, both folded in:

- **A1, what an old row means.** Rows are written only after the gate and the port checks pass, and the three ways an
  old row reads are spelled out under "Storage".
- **A2, the ziti recipe may cover only some listeners.** It lands with a test per listener it moves, and names the
  rest for the wide-bind warning (stage 1 checklist).

## Open Questions

Both are answered by the orchestrator as defaults, **orchestrator default, clint to confirm**. The build follows them.
Neither blocks stage 1.

1. **Cards atrium does not own a runner for.** Orchestrator default, clint to confirm: a window-mode or
   `/atrium-join` card may start processes while the reaper watches its pid, and they stop when the reaper finds the
   pid gone. The alternative was refusing processes on those cards entirely.
2. **The permission gate: `Bash` rules, or a tool of its own?** Orchestrator default, clint to confirm: gate as `Bash`
   with the full command, so existing `Bash` rules apply. The CLI double gate is acceptable with a standing allow on
   `atrium proc`.
