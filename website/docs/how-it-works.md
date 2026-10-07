---
title: How it works
description: What runs, what talks to what, and where each fact lives, on one page.
---

# How it works

One page on the whole system. Each section links to the page that covers it in depth.

## Two processes

Atrium is one binary with two long-running commands.

```text
  browser                  HUB  atrium run                     ROOM  atrium room          (one per machine)
  ┌──────────┐  :7778   ┌──────────────────────────┐  dials out ┌────────────────────────────┐
  │ the board│ ───────► │ serves the board         │ ◄───────── │ sqlite store               │
  └──────────┘          │ passes /v1/* to a room   │ :7779 mTLS │ :7777 agent listener       │
                        │ control MCP at /_hub/mcp │            │ :7781 its own board        │
                        │ rooms, backlog, git      │            │ the terminals              │
                        └──────────────────────────┘            └─────────────┬──────────────┘
                                                                  hooks │ ▲ launch, type
                                                                        ▼ │
                                                               ┌──────────────────┐
                                                               │ agent sessions   │
                                                               └──────────────────┘
```

**A room** (`atrium room`) owns the database, the terminals and the agents in them. It runs for days. It also serves
its own board on `:7781`, so it stays usable when the hub is down.

**The hub** (`atrium run`) serves the board and passes every other request to the right room. It holds no work, so
it restarts freely while agents are mid-turn. What it does keep is what only it can answer: which rooms exist, the
shared backlog, and a git store rooms push finished work to.

The split follows lifetimes. Board code changes often, and agents must not die for it. The room dials the hub, never
the reverse, so a hub restart is a room noticing a closed socket and dialling again. On one machine, `atrium run`
starts both. [Rooms and the hub](./rooms.md) covers adding more machines.

## Cards

Every session is a [card](./cards.md), and the card outlives the session. It has a handle other sessions address it
by, a status, a directory, tags, an event log, a message queue and its open questions.

- **Status is a column, a bucket of your attention:** needs permission, ready, running, finished, shelved. Stored.
- **Activity is a badge, a fact about the runner right now:** thinking, running a tool, idle. Never stored, because a
  stored activity is wrong the moment the room restarts.
- **What you typed beats what a hook reported.** They are kept in separate fields, and yours win.

## How a session reaches the board

**Supervised.** Atrium launches the runner in a pseudo terminal it owns, from the board, `atrium launch` or another
agent's `atrium_launch`. The browser attaches over a websocket. Atrium can type into it, read its screen, restart it
and ask it to leave. [Supervised terminals](./terminals.md).

**Joined.** A session you started in your own terminal reports through its hooks, or runs `atrium join`. It gets a
card and its tool calls can be gated, but atrium has no terminal to write to, so everything it says to the session
rides a hook. [Two ways to run it](./modes.md).

## Hooks

A runner's hooks are how a session reports in: `SessionStart` opens the card, `SessionEnd` closes it, `PreToolUse`
moves the badge and asks the gate, `Notification` says the session is blocked on you. The board writes them for
claude and codex with one button. [Hooks](./hooks.md).

**A hook never fails a session.** When atrium is not listening, the permission hook fails open and every other hook
is ignored. A session must never fail to start, end a turn or run a tool because atrium was down.

## The permission chain

Every gated tool call passes one chain. Each step can answer and stop, and the order is the design.

1. **A replayed decision.** The same request already answered gets the same answer.
2. **A queued message.** The call is blocked, and the block reason carries your message. This is how a working
   session hears from you.
3. **A shelved card.** A standing no.
4. **A room deploy hold.** Every held card's next call waits until the deploy is done.
5. **A standing rule.** The most specific match wins: prefix, glob or folder.
6. **Auto mode**, for the card or for every card. Approve and record. Last, so messages, shelving, holds and rules all
   still win.
7. **Ask you.** The card moves to needs permission, and the agent waits until someone decides.

Every decision is recorded with what made it. [Permissions and auto mode](./permissions.md).

## Sessions talking to sessions

A message reaches a session one of two ways: typed into its terminal, when atrium owns it and your line is empty and
quiet, or carried by a hook on its next tool call or turn end. Agents use the hub's
[control MCP server](./control-mcp.md): `atrium_peers` to find handles, `atrium_say` to say something,
`atrium_report` to answer the card that launched them. `name@room` reaches a card on another machine.
[Messages and peers](./messages.md).

## State

Each room keeps one SQLite file: cards and their event logs, permissions and decisions, rules, runners, sources,
actions, messages, questions and settings. The hub keeps a second, smaller one: rooms and their join secrets, a cache
of what each room last reported, the backlog, documents and the audit log.

Nothing about what a runner is doing right now is stored. It is a fact about a running process and dies with it.

**Storage failure halts.** A room refuses to start if its database will not open, and closes the agent port if it
fails later. Agents see connection refused and wait on the backoff they already have. The board stays up to say what
broke. Running without durable state is worse than not running.

## Stopping and restarting

`atrium stop` winds a room down in order and gives runners ten seconds. Killing the process is not the same: closing
a pseudo terminal ends the process in it, so a kill ends every supervised agent at once. With the `pty_host` setting
on, the terminals live in a separate process and survive a room restart. Without it, atrium brings each session back
on a new terminal from its resume id, with its conversation intact.

A hub restart costs nobody a session. When a hub restart is due, a board in use shows a countdown that anyone at it
can pause.
