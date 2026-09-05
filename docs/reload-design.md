# Reloading atrium from inside atrium

How a new build of the daemon gets installed by an agent that atrium is currently running, without a human at a
shell and without the agent killing itself mid sentence.

This is the loop that makes atrium self hosting. An agent working on atrium edits atrium, builds it, installs it,
and comes back in the same conversation on the new binary. Every piece below exists because a simpler version of it
was tried and broke in a specific way.

## The chicken and egg

Three constraints collide:

1. **The daemon owns the terminal.** A supervised runner sits on a pseudo terminal the daemon created. Closing that
   terminal takes the runner with it, so a supervised session cannot stop the daemon and live to start it again.
2. **A running binary cannot be overwritten.** On Windows the file is held open for as long as anything is executing
   it. `go build -o` straight over a running `atrium.exe` fails.
3. **A tool call that kills its caller never returns.** The model is left with a tool use that has no result, which
   is a state nothing has tested and no amount of prompting makes legible.

So the restart needs a third party that outlives both the daemon and the session, the binary swap needs a moment
when nothing is running the file, and the tool has to answer before anything happens.

## The pieces

```
  claude session                atrium control (mcp)         detached restarter        atrium daemon
  ┌────────────────┐            ┌───────────────────┐        ┌──────────────────┐      ┌──────────────┐
  │ make build     │            │ restart_atrium    │        │ sleep 4s         │      │ :7778 board  │
  │ copy .next.exe │ ──tool──>  │ capture db path   │ spawn> │ atrium stop      │ ──>  │ winds down   │
  │ "say nothing"  │ <─"sched"─ │ spawn detached    │        │ wait for port    │      │ (10s grace)  │
  └────────────────┘            └───────────────────┘        │ swapStaged()     │      └──────────────┘
         ^                                                   │ start daemon     │ ──>  ┌──────────────┐
         └──────────────── resumed as a fixture ─────────────┴──────────────────┘      │ new binary   │
                                                                                       └──────────────┘
```

- **`atrium control`** (`internal/cli/control.go`) is a stdio MCP server registered at USER scope. It is a
  subprocess of the claude session, not of the daemon, which is the point: the daemon is the thing going away.
- **`atrium control --restart-now`** is the same binary re-invoked detached. Not a second executable, so there is
  one thing to install and it can never be a version behind.
- **`atrium.next.exe`** is the staged build, sitting beside the installed binary and waiting for the one instant it
  can be moved into place.

## The sequence

### 1. Build and stage

```bash
make build                                            # writes build.claude/atrium.exe
cp build.claude/atrium.exe ~/.atrium/bin/atrium.next.exe
```

Staged rather than installed. `~/.atrium/bin/atrium.exe` is running right now and cannot be written. `atrium.next`
is an ordinary file nobody has open.

**Never register the MCP server against `build.claude/atrium.exe`.** An MCP server is held open by the session that
spawned it, so pointing at the build output means every subsequent `make build` fails on a locked file. Register
the installed copy.

### 2. Ask for the restart

`restart_atrium` does four things and returns:

- Reads the database path from the address file **while the daemon is still up**. `atrium stop` deletes that file
  on its way out, so reading it later finds nothing and the new daemon would open whatever database the detached
  process happened to inherit.
- Spawns `atrium control --restart-now --db <path> --delay 4s`, detached.
- Answers `scheduled: true`.
- Tells the caller, in the tool result, to say nothing further this turn.

The four second delay is sized for the tool result to reach the model, the turn to end, and the session end hook to
fire. Anything shorter races the transcript write and the conversation loses its last exchange.

`run_in_background` is not a substitute for detaching. A background bash task is a child of the session and dies
with it, which leaves the daemon stopped and nothing to start it again. See `detach_windows.go` and
`detach_other.go`: `CREATE_NEW_PROCESS_GROUP | DETACHED_PROCESS` and `Setsid`.

### 3. The detached half

`runRestart` waits out the delay, then:

- `atrium stop` against the recorded board address. A daemon already gone is not a failure. The goal is to end with
  one running, not to have stopped one.
- **Polls `/v1/health` until it stops answering**, up to twenty seconds. `atrium stop` returns as soon as the
  request is accepted, and the wind down after that gives supervised runners ten seconds. Starting during that
  window produces a second daemon that cannot bind and exits, which looks exactly like the restart doing nothing.
- `swapStaged()`. This is the only moment the binary can be replaced.
- Starts `atrium daemon --db <path>`, detached.

### 4. The swap

The outgoing binary is **moved aside, not deleted**, and on Windows that distinction is the whole trick. Windows
refuses to delete an open file and an executable is open while anything runs it. It permits a rename within the
same directory: the running image keeps its handle, the name is freed, and the new file takes it.

```
atrium.exe       ->  atrium.old.exe     (rename, the running image does not care)
atrium.next.exe  ->  atrium.exe         (rename, so there is no half written binary)
```

A failure at either step puts the old name back. A staged binary that does not land means the daemon returns on the
old one, which is a disappointment. No binary at that name is an outage.

`atrium.old.exe` is removed on the next restart, once nothing is running it.

### 5. Coming back

- **Fixtures restart themselves.** A supervised runner that is a fixture comes up with the daemon and resumes its
  conversation from `resume_mode`. Anything else does not come back at all, which the tool description says.
- **Browsers reconnect on their own.** The attach websocket retries for five minutes against a daemon that is
  simply absent. The board and any popped out terminal reattach when it answers again.

## Why the board reloads itself

A restart replaces what is served and touches nothing already running. A window open since before a fix is still
executing the JavaScript it downloaded. On the board that is one reload away and obvious. On a **popped out
terminal** it is neither: those stay open for days, and the symptom is a fix that works in every window opened
afterwards and not in the one you are looking at. That cost an hour of "the reconnect is broken" for code that
reconnected correctly everywhere else.

So `/v1/health` carries a build id: the first eight bytes of the sha256 of the embedded `index.html`. The page
remembers the first one it saw, and a different answer means the thing serving the page is not the thing that wrote
it, so it calls `location.reload()`.

Hashing the board rather than stamping a version means this fires when the page and the daemon genuinely differ,
and never on an ordinary restart of the same build. The reconnect path checks it too, so a daemon that came back on
a new build is noticed the moment it answers rather than on the next five second poll.

Reloading a popped out terminal costs the scrollback and nothing else. The card id is in the URL, the runner was
never touched, and it reattaches on load.

`Cache-Control: no-store` on the board solves a different problem: a browser reusing a stale copy on a fresh load.
Vendored libraries keep caching, because they only change when they are re-vendored.

## Checking it worked

`atrium_status` from the same MCP server, after the fact. By definition whoever asked for the restart is gone, so
the restarter's own logging goes nowhere useful and is not the record. `atrium_status` answers without a daemon
too, which is the case worth being able to see: `running: false` plus where the last one was, rather than a
failure.

## What this does not do

- **No version check.** The staged binary is installed because it is there, not because it is newer. Staging is the
  deliberate act.
- **No rollback.** `atrium.old.exe` exists until the next restart, and putting it back is a manual rename.
- **No restart on a schedule or a file watch.** A restart takes down every supervised session, so it is always
  something somebody asked for.
