# How atrium works

One page on the whole system: what runs, what talks to what, and where each fact lives. Each section links to the
document that covers it in depth. The hub and the rooms have their own reference, `docs/fabric/hub-and-rooms.md`, and
how sessions message each other is `docs/runtime/agent-messaging.md`.

## What atrium is

Atrium is a task board with live coding agents attached. Every agent session is a **card**. The board answers three
questions: what is running, which session needs me most, and what was I doing in that one. It also gates every tool
call an agent makes, so a human (or a standing rule) decides what runs. The agents are whatever runners you use:
claude, codex, gemini, opencode, ollama or a plain shell are rows in a runner table, not assumptions in the code.

## The processes

One binary, `atrium`, and two long-running commands.

```
  browser                  HUB  atrium run                     ROOM  atrium room          (one per machine)
  ┌──────────┐  :7778   ┌──────────────────────────┐  dials out ┌────────────────────────────┐
  │ the board│ ───────► │ serves the board's files │ ◄───────── │ sqlite store               │
  │ xterm.js │          │ passes /v1/* to a room   │ :7779 mTLS │ :7777 agent listener       │
  └──────────┘          │ control MCP at /_hub/mcp │            │ :7781 its own board        │
                        │ rooms, backlog, git      │            │ pty supervisor             │
                        └──────────────────────────┘            └─────────────┬──────────────┘
                                     ▲                                  hooks │ ▲ spawn, type
                                     │ MCP over HTTP                          ▼ │
                                     │                               ┌──────────────────┐
                                     └────────────────────────────── │ agent sessions   │
                                        atrium_peers, atrium_say ... └──────────────────┘
```

**The room** (`atrium room`, `internal/daemon`) owns the database, the pseudo terminals and the agents in them. It
runs for days and is never restarted to change the board. It also serves its own board on `:7781`, so it stays usable
when the hub is down.

**The hub** (`atrium run`, `internal/link`) serves the board's HTML, CSS and JS itself and passes every other request
to a room. It holds no work, so it restarts freely while agents are mid-turn. It keeps a store of its own for what only
it can answer: which rooms exist, the shared backlog and reports, and a git mirror rooms push finished work to. The
split follows lifetimes: board code changes every few minutes, and agents must not die for it.

The room dials the hub, never the other way. A hub restart is the room noticing a closed socket and dialling again.
Joining is one pasted string that pins the hub's certificate authority, and every link after that is mutual TLS. The
name in the room's certificate is the room's identity. `atrium run` on a machine with no room starts one and enrols it
itself, so a single machine needs no join string.

**Two listeners on the room, on purpose.** `:7777` takes agent traffic (hooks, `atrium tell`, `atrium finish`).
`:7781` takes the human API. When storage fails, the room closes `:7777` and leaves it closed. Agents see connection
refused and park on their existing backoff. The board stays up to say what broke.

`atrium daemon` is the older single process: the same room code with the board served in-process on `:7778` and agents
on `:7777`, and no hub. It still runs, and the service installers still default to it.

## Cards

A card outlives the process it describes. It has a `wire_name` (the handle other sessions address it by), a status
column, a worktree, tags, an event log, a queue of messages, and a list of open questions. On a hub with more than one
room, its id carries its room, `room~id`, so the board can address it without knowing what that means.

- **Status is a column, a bucket of human attention.** `running`, `needs-input`, `needs-permission`, `shelved`,
  `done`, `dead`, plus `backlog` for work not started. Stored.
- **Activity is a badge, a fact about the runner right now.** Thinking, running a tool, idle. Never stored, because it
  would be wrong the moment the room restarted. `docs/runtime/activity-design.md`.
- **Observed values never overwrite overrides.** What a hook reports goes in one set of fields. What a human typed
  goes in another and wins.

## How a session reaches the board

Two ways, and they differ in what atrium can do for the session.

**Supervised.** Atrium launches the runner (`atrium launch`, the board's launch dialog, or `atrium_launch`) in a pseudo
terminal it owns. The browser attaches to that terminal over a websocket. Atrium can type into it, read its screen,
restart it, and ask it to leave. With the `pty_host` setting on, the terminals live in a separate pty host process and
survive a room restart. `docs/terminal/supervision-design.md`, `docs/terminal/ptyhost-protocol.md`.

**Joined.** A session started in an ordinary terminal runs `atrium join`. It gets a card and its tool calls are gated,
but atrium has no terminal to write to. Everything atrium says to it has to ride a hook.

## Hooks

Claude Code's hooks are how a session reports in. `atrium hook`, `atrium session` and `atrium turn` are the commands
registered in `settings.json`, and they find the room through the machine's location file. The board writes the
missing ones for you. `docs/runtime/hooks.md`. Other runners report through their own hook files, measured in
`docs/runtime/wiring-a-runner.md`.

| Hook | Goes to | What it does |
| --- | --- | --- |
| `PreToolUse` | `/permission` | The permission gate. Blocks until the chain below answers. |
| `PreToolUse` | `/activity` | Updates the live badge. Fire and forget, one second timeout. |
| `PostToolUse` | activity | Marks the tool call finished. |
| `SessionStart` / `SessionEnd` | `/session` | Opens or closes the card, records the pid and resume id. |
| `Stop` | `/stop` | Moves the card to `needs-input` and delivers any queued message. Optional. |
| `Notification` | activity | Claude Code is blocked on the human. Sorts above a plain turn end. |
| `SubagentStart` / `SubagentStop` | activity | Counts subagents on the card. |

**A hook never fails a session.** When atrium is not listening, the permission hook fails open and every other hook is
ignored. A session must never fail to start, end a turn, or make a tool call because atrium was down.

## The permission chain

Every gated tool call lands in `onPermRequest` in `internal/daemon/daemon.go`. Each step can answer and stop. The order
is the design.

1. **A replayed decision.** The same request already answered gets the same answer.
2. **A queued message.** The call is blocked and the block reason carries the message. This is how a working session
   hears from you. See `docs/runtime/agent-messaging.md`.
3. **A shelved card.** A standing no.
4. **A room deploy hold.** Every held card's next call is refused until the deploy is done.
   `docs/rnd/room-deploy-hold-design.md`.
5. **A standing rule.** Most specific match wins: prefix, glob, or folder.
6. **Auto mode**, for the card or for every card at once. Approve and record. Last, so messages, shelving, holds and
   rules all still win. `docs/runtime/auto-mode.md`.
7. **Ask the human.** The card moves to `needs-permission` and the agent blocks until someone decides.

Every decision is written to the audit log with what made it.

## Sessions talking to sessions

Every card has a durable message queue. A message reaches a session one of two ways:

- **Typed** into its terminal, when atrium owns the terminal and the operator's line is empty and quiet.
- **Carried by a hook**, on the session's next tool call (the permission hook) or at the end of its turn (the Stop
  hook).

Agents use the hub's control MCP: `atrium_peers` to find handles, `atrium_say` to say something, `atrium_report` to
answer the card that launched them. `name@room` reaches a card on another room. The CLI has the same verbs
(`atrium peers`, `atrium tell`, `atrium ask --peer`, `atrium answer`). The mechanics, limits and known gaps are in
`docs/runtime/agent-messaging.md`.

## Agents telling atrium things

A few commands exist because atrium could not infer them:

- `atrium finish [recap]`: the work is over, and here is what was done.
- `atrium ask "question"`: I have stopped and need this. `--continue` says the session is carrying on.
- `atrium ready`: the handoff for a new context is written, so the context can be cleared.
  `docs/runtime/context-cycle-design.md`.

They are commands rather than MCP tools because a command is the one channel every runner has.

## State

One SQLite file per room, pure Go driver, schema kept Postgres portable. `internal/store` is the only code that
touches it. Migrations run in slice order and never re-run, so new ones go at the end and tolerate already being
applied. The hub's store is a second, smaller SQLite file, `internal/hubstore`.

**Stored on the room:** cards and their event log, permissions and decisions, rules, harness rows, fixtures, sources,
actions, messages, questions, recaps, settings, the work ledger.

**Stored on the hub:** rooms and their join secrets, a cache of each room's last report, the backlog, reports, hub
documents, change requests, pull request claims, the audit log, push subscriptions.

**Not stored:** what a runner is doing right now, the on-screen retry schedule for held messages, and the peer rate
limiter. All three are facts about the running process and die with it.

**Storage failure halts.** A room refuses to start on an open or migration failure, and closes the agent listener on a
later one. The hub refuses to start without its store. Running without durable state is worse than not running.

## Stopping and restarting

`atrium stop` winds a room down: event streams released first, supervised runners given ten seconds, every step
logged. Killing the process is not the same thing. Without the pty host, closing a pseudo terminal ends the process
attached to it, so a kill ends every supervised agent at once. A restart from the board or from `restart_atrium` is
scheduled a few seconds out and parks busy agents first. A hub-only deploy goes through the hub restart gate, which
waits until nobody is using a board and lets anyone at one pause it. `docs/runtime/reload-design.md`,
`docs/fabric/hub-restart-gate.md`.

## Reaching the board from elsewhere

The board is loopback only and has no login. Reaching it from another machine is an overlay's job:
`atrium run --board-transport zrok` or `ziti` serves the same board on a zrok share or an OpenZiti service. Atrium holds
the name of a credential, never somebody else's credential. `docs/fabric/overlays.md`.

## What was here before

v1 had two modes. Mode A was a hub and an agent loop over MCP, and Mode B a read-only aggregator (`atrium serve`,
`status`, `watch`). Both are removed. The v2 daemon replaced them, and the daemon was then split into the hub and the
rooms. `docs/story.md` tells it in order, and the designs are in `docs/archive/`.

## Where to go next

| Question | Document |
| --- | --- |
| The hub, rooms, joining and ports | `docs/fabric/hub-and-rooms.md` |
| Messaging between sessions | `docs/runtime/agent-messaging.md` |
| Terminals and attach | `docs/terminal/supervision-design.md` |
| The live badge | `docs/runtime/activity-design.md` |
| Auto mode and its review | `docs/runtime/auto-mode.md` |
| Hooks | `docs/runtime/hooks.md` |
| Wiring a new runner | `docs/runtime/wiring-a-runner.md` |
| Reaching atrium from another machine | `docs/fabric/overlays.md` |
| Installing | `website/docs/install.md`, `docs/release/packaging.md` |
| Why the daemon is shaped this way | `docs/archive/architecture-v2.md` |
