# How atrium works

One page on the whole system: what runs, what talks to what, and where each fact lives. Each section links to
the design document that covers it in depth. For how sessions message each other, read
`docs/agent-messaging.md`.

## What atrium is

Atrium is a task board with live coding agents attached. Every agent session is a **card**. The board answers
three questions: what is running, which session needs me most, and what was I doing in that one. It also gates
every tool call an agent makes, so a human (or a standing rule) decides what runs.

## The processes

```
  browser                     HUB  (atrium2 hub)                   ROOM  (atrium2 room)
  ┌────────────┐  :7800   ┌──────────────────────┐   dials out   ┌──────────────────────────┐
  │ the board  │ ───────► │ serves board files   │ ◄──────────── │ sqlite store             │
  │ xterm.js   │          │ proxies /v1/* to the │  :7801 mTLS   │ :7778 human API + SSE    │
  └────────────┘          │ right room           │               │ :7777 agent listener     │
                          │ control MCP /_hub/mcp│               │ pty supervisor           │
                          │ holds no card state  │               └───────────┬──────────────┘
                          └──────────────────────┘                     hooks │ ▲ spawn, type
                                     ▲                                       ▼ │
                                     │ MCP over HTTP                  ┌──────────────────┐
                                     └─────────────────────────────── │ claude sessions  │
                                        atrium_peers, atrium_say ...  └──────────────────┘
```

**The room** is the daemon (`internal/daemon`). It owns the database, the pseudo terminals, and the agents in
them. It runs for days and is never restarted to change the board.

**The hub** (`cmd/atrium2`, `internal/link`) serves the board's HTML, CSS, and JS itself and proxies every other
request to a room. It holds no card state, so it restarts freely while board work is in progress. The split
follows lifetimes: board code changes every few minutes, and agents must not die for it. See
`docs/hub-room-plan.md`.

The room dials the hub, never the other way. A hub restart is the room noticing a closed socket and redialling.
Joining is one pasted string, which pins the hub's certificate authority, and every link after that is mutual
TLS. The name in the room's certificate is the room's identity.

A room also serves its own board on loopback, so it stays usable when the hub is down. The older single-process
`atrium daemon` is the same room code with the board served in-process.

**Two listeners on the room, on purpose.** `:7777` takes agent traffic (hooks, `atrium tell`, `atrium finish`).
`:7778` takes the human API. When storage fails, the room closes `:7777` and leaves it closed. Agents see
connection refused and park on their existing backoff. The board on `:7778` stays up to say what broke.

## Cards

A card outlives the process it describes. It has a `wire_name` (the handle other sessions address it by), a
status column, a worktree, tags, an event log, a queue of messages, and a list of open questions.

- **Status is a column, a bucket of human attention.** `running`, `needs-input`, `needs-permission`, `shelved`,
  `done`, `dead`, plus `backlog` for work not started. Stored.
- **Activity is a badge, a fact about the runner right now.** Thinking, running a tool, idle. Never stored,
  because it would be wrong the moment the daemon restarted. `docs/activity-design.md`.
- **Observed values never overwrite overrides.** What a hook reports goes in one set of fields. What a human
  typed goes in another and wins.

## How a session reaches the board

Two ways, and they differ in what atrium can do for the session.

**Supervised.** Atrium launches the runner (`atrium launch`, the board's launch dialog, or `atrium_launch`) in a
pseudo terminal it owns. The browser attaches to that terminal over a websocket. Atrium can type into it, read
its screen, restart it, and ask it to leave. `docs/supervision-design.md`.

**Joined.** A session started in an ordinary terminal runs `atrium join`. It gets a card and its tool calls are
gated, but atrium has no terminal to write to. Everything atrium says to it has to ride a hook.

## Hooks

Claude Code's hooks are how a session reports in. `atrium hook`, `atrium session`, and `atrium turn` are the
commands registered in `settings.json`. The board writes the missing ones for you. `docs/hooks.md`.

| Hook | Goes to | What it does |
| --- | --- | --- |
| `PreToolUse` | `/permission` | The permission gate. Blocks until the chain below answers. |
| `PreToolUse` | `/activity` | Updates the live badge. Fire and forget, one second timeout. |
| `PostToolUse` | activity | Marks the tool call finished. |
| `SessionStart` / `SessionEnd` | `/session` | Opens or closes the card, records the pid and resume id. |
| `Stop` | `/stop` | Moves the card to `needs-input` and delivers any queued message. Optional. |
| `Notification` | activity | Claude Code is blocked on the human. Sorts above a plain turn end. |
| `SubagentStart` / `SubagentStop` | activity | Counts subagents on the card. |

**A hook never fails a session.** When atrium is not listening, the permission hook fails open and every other
hook is ignored. A session must never fail to start, end a turn, or make a tool call because atrium was down.

## The permission chain

Every gated tool call lands in `onPermRequest` in `internal/daemon/daemon.go`. Each step can answer and stop.
The order is the design.

1. **A replayed decision.** The same request already answered gets the same answer.
2. **A queued message.** The call is blocked and the block reason carries the message. This is how a working
   session hears from you. See `docs/agent-messaging.md`.
3. **A shelved card.** A standing no.
4. **A standing rule.** Most specific match wins: prefix, glob, or folder.
5. **Auto mode.** Approve and record. Last, so messages, shelving, and rules all still win.
   `docs/auto-mode.md`.
6. **Ask the human.** The card moves to `needs-permission` and the agent blocks until someone decides.

Every decision is written to the audit log with what made it.

## Sessions talking to sessions

Every card has a durable message queue. A message reaches a session one of two ways:

- **Typed** into its terminal, when atrium owns the terminal and the operator's line is empty and quiet.
- **Carried by a hook**, on the session's next tool call (the permission hook) or at the end of its turn (the
  Stop hook).

Agents use `atrium peers` to find handles, `atrium tell` to say something, `atrium ask --peer` and `atrium
answer` to ask and reply, and the MCP tools `atrium_peers` and `atrium_say` for the same through the hub. The
full mechanics, limits, and known gaps are in `docs/agent-messaging.md`.

## Agents telling atrium things

A few commands exist because atrium could not infer them:

- `atrium finish [recap]`: the work is over, and here is what was done.
- `atrium ask "question"`: I have stopped and need this. `--continue` says the session is carrying on.
- `atrium name`: name this atrium once, so wire names from two machines cannot collide.

They are commands rather than MCP tools because a command is the one channel every runner has.

## State

One SQLite file per room, pure Go driver, schema kept Postgres portable. `internal/store` is the only code that
touches it. Migrations run in slice order and never re-run, so new ones go at the end and tolerate already
being applied. `internal/store/CLAUDE.md`.

**Stored:** cards and their event log, permissions and decisions, rules, harness rows, fixtures, sources,
actions, messages, questions, recaps, settings.

**Not stored:** what a runner is doing right now, the on-screen retry schedule for held messages, and the peer
rate limiter. All three are facts about the running process and die with it.

**Storage failure halts.** Atrium refuses to start on an open or migration failure, and closes the agent
listener on a later one. Running without durable state is worse than not running.

## Stopping

`atrium stop` winds a room down: event streams released first, supervised runners given ten seconds, every step
logged. Killing the process is not the same thing. Closing a pseudo terminal ends the process attached to it,
so a kill ends every supervised agent at once. `restart_atrium` schedules a restart a few seconds out and parks
busy agents first. `docs/reload-design.md`.

## Reaching the board from elsewhere

The board is loopback only and has no login. Reaching it from another machine is an overlay's job: atrium can
serve the board on a zrok share or an OpenZiti service, where the SDK hands back a `net.Listener` and the board
is one handler on it. Atrium holds the name of a credential, never somebody else's credential.
`docs/overlays.md`.

## The older surfaces

Two v1 modes still build and still work. Neither shares state with the daemon.

- **Mode A, hub and agent loop.** `atrium hub` plus an `atrium agent` MCP server inside each session. The agent
  calls `submit` in a loop and long-polls the hub for the next prompt. Amnesiac by design. This `hub` is not the
  `atrium2 hub` above.
- **Mode B, read-only aggregator.** `atrium serve` reads the gwt session ledger and exposes it over MCP.

See the project `CLAUDE.md` for both.

## Where to go next

| Question | Document |
| --- | --- |
| Why v2 is shaped this way | `docs/architecture-v2.md` |
| Hub and room split | `docs/hub-room-plan.md` |
| Messaging between sessions | `docs/agent-messaging.md` |
| Terminals and attach | `docs/supervision-design.md` |
| The live badge | `docs/activity-design.md` |
| Auto mode and its review | `docs/auto-mode.md` |
| Hooks | `docs/hooks.md`, `docs/user-guide.md` |
| Many machines, one board | `docs/federation-design-v2.md` |
| Installing | `docs/packaging.md` |
