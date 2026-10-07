# How atrium got here

Status: maintained. Add a section when the shape of atrium changes, and archive the design it replaces.

Atrium has been rebuilt three times in four months, each time because the last shape broke on something real. This
is the order it happened in, what broke at each step, and the archived doc that holds the detail. The published
version, told for users, is `website/docs/story.md`. Read this one if you are about to change how atrium is shaped
and want to know which ideas were already tried.

## June: a chat window with a broker behind it

The first atrium (2026-06-04) was a localhost broker for one person and many claude sessions. It had two modes.

- **Mode A** was `atrium hub`, a Bubble Tea TUI on an HTTP server, and `atrium agent`, a small MCP server each claude
  session loaded. Its one tool, `submit`, posted to the hub and long-polled for a reply. You typed, the agent acted,
  it submitted again. `@name` routed a prompt to one agent.
- **Mode B** was `atrium serve`, `status` and `watch`: a read-only view over the gwt session ledger, exposed as MCP
  tools.

The hub was amnesiac by rule. Restart it and everything was gone, and that was the point: nothing to corrupt, nothing
to migrate. Four rules from this version still hold. The model never sees a disconnect, never sees an empty prompt,
idles for free, and the permission hook fails open.

**What broke.** It answered "let me talk to many sessions from one terminal". The question that cost time every day
was "what do I have running, which one needs me, and what was I doing in it?" An amnesiac hub cannot answer that.
`docs/archive/state-of-the-art.md` describes v1 as it stood.

## 09-01: the daemon, a board with live agents

`atrium daemon` (`fdc5c8e6`) reversed v1's central rule. Atrium became a task board with live agents attached: a
SQLite database and an append-only event log per card, a web board on :7778, agents on :7777, a permission gate with
standing rules, and sessions that register through `SessionStart` and `SessionEnd` hooks. Within days it owned the
runners' terminals too, launching them under pseudo terminals and attaching the browser over a websocket.

Durable state brought the halt with it: if storage fails, the agent port closes and stays closed, and the board stays
up to say why.

**What broke.** Three things, and all three pointed the same way.

- The board was served by the process that owned the terminals. Installing a new board meant restarting the daemon,
  and on Windows closing a pty kills its runner, so every board change killed every supervised agent. The preview
  board, a second daemon on a copy of the cards, only softened it.
- It was one machine. A second machine meant a second board in another tab.
- An agent under its own operating system account fell off the desktop, and so off the board.

`docs/archive/architecture-v2.md` is the design. Its staged migration planned a TUI rewrite against the new API. That
stage was abandoned and the TUI was deleted instead.

## 09-02 to 09-08: federation, then the forum

The first answer to many machines was federation (`docs/archive/federation-design.md`, 09-02). It ruled out a central
atrium that mirrors other atriums, because a second durable copy of a card is wrong for the same reason a stored
activity badge is wrong, and it recommended federating in the client: one browser page opening several daemons.

A day later the second design reversed the recommendation and kept the ban (`docs/archive/federation-design-v2.md`,
09-03). Federating in the client still needed every machine reachable. A forum that holds nothing, which the machines
dial instead of being dialled, solves reachability too, and the daemon's board was already one `http.Handler` that
could be served on a dialled-out connection. `docs/archive/forum-implementation.md` is how it was to be built.

**What broke.** The forum's own rules. On 09-08 clint asked for rooms to be transparent: an agent on another
machine is just an agent on the board, attached the same way, with the room an attribute you notice only when you
care. The forum had said terminals stay where they are, attach redirects to the room's own board, and nothing is
proxied. Transparency meant the hub relays a terminal, so those rules went, and the room keeps its own board only
for when the hub is gone. `docs/archive/transparent-rooms.md` holds that reasoning.

## 09-17 to 09-21: the hub and the rooms, as atrium2

The forum was built as a second binary, `atrium2`, with `atrium2 hub` on :7800 and `atrium2 room` on :7801. The hub
serves the board and holds no work. The room owns the database, the ptys and the agents, and dials the hub over
mutual TLS after a one-time join string that pins the hub's certificate authority. Restarting the hub costs nobody a
session. The split followed lifetimes, and it answered all three of the daemon's problems at once: a second machine is
another room, and an agent account is a room of its own.

clint looked at the first build on 09-17 and it was not what he wanted. `docs/archive/hub-room-requirements.md` is his
list: every screen is driven from the hub, the room's own board is a fallback nobody uses, the room counter scopes
the whole board to one room, no selection means all rooms, and the transport is a badge at most.
`docs/archive/hub-room-plan.md` is how it was built.

**What broke.** "Holds nothing" did not survive the first week. Rooms, names, join secrets and deletion state have to
be written somewhere, so the hub got its own SQLite store (decision 11, 09-17, in `docs/fabric/hub-decisions.md`).
And two binaries on one machine meant two state folders, a hook binary that was not the hub binary, an old copy kept
only so the control MCP would not lock the hook binary, and every doc explaining which was which.

## 09-24 to 09-26: one binary

clint decided three things on 09-24 (`docs/archive/one-atrium-plan.md`): Mode A goes, Mode B goes, and there is one
binary. `cmd/atrium2` moved into `internal/cli` as `atrium run`, `atrium room` and `atrium rooms` (09-25), the live
machine was cut over (`docs/archive/one-atrium-cutover.md`), and the `atrium2` shim went on 09-26. Nothing had called
Mode B since 09-02, and the board showed everything it did.

The ports settled where they are now: the hub's board on 127.0.0.1:7778 and its room link on 7779, a room's agents on
127.0.0.1:7777 and its own board on 7781. `atrium daemon`, the single process, still runs, for a machine that needs no
hub.

**What broke.** Mostly the docs, which went on describing `atrium daemon`, `atrium2` and :7800 for two weeks. This
overhaul is the fix.

## 09-27 onward: the hub takes on work, on purpose

Since the cutover the hub has been asked to hold more, one decision at a time, and each time the reason was written
down.

- **Integration branches** (decision 19, 09-29). An agent may not touch a remote, so moving `claude/main` to a new
  room took a person or an agent spending tokens on git plumbing. The hub now holds a bare repository per project
  and moves code between rooms by fetch (`internal/gitsync`, `docs/fabric/git-sync-design.md`), and later serves it as
  a forge (`docs/fabric/hub-forge-design.md`).
- **The backlog, reports and documents** live on the hub, so every room reaches them
  (`docs/rnd/reports-channel-design.md`, `docs/rnd/hub-documents-design.md`).
- **Cross-room messages.** A peer on another room is `name@room` (`docs/fabric/cross-room-say-design.md`).

The room got two things it lacked:

- **A pty host.** "ConPTY has no reattach" was written into four docs as fact, until the stage 0 spike showed
  otherwise. With the `pty_host` setting on, runners outlive a room restart (`docs/rnd/rolling-restart-design.md`).
- **A context cycle.** Long-lived agents ran at three or four times their context threshold. The room now captures a
  handoff, clears and wakes the session itself (`docs/runtime/context-cycle-design.md`). Three earlier designs for
  it are in `docs/archive/`.

## Where it stands

One `atrium` binary. `atrium run` is the hub, `atrium room` is a room on each machine, and rooms dial the hub.
`docs/how-atrium-works.md` is the whole system on one page, and `docs/fabric/hub-and-rooms.md` the hub and rooms in
detail.

The pattern across all of it is the same. A shape was built, used every day, and broke on something that cost
somebody time. The fix went in, and the reasoning went into a doc that is now in `docs/archive/`.
