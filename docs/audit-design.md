# An audit log for the hub and the rooms

A place on the board to SEE important operational events, instead of digging them out of logs and the per-card
event stream. A room attaching or detaching, the hub starting, a launch refused by the cap, the board being put on
a share, a room winding down. Today each of those is scattered: some are a log line on the hub, some are a delta on
one card's stream, some are nowhere at all. This is one cross-cutting feed, read-only, newest first, filterable.

This is the design. The first slice it describes is HUB-ONLY and deployable without touching a room. What only a
room can emit is named at the end and deferred, so a room restart is a later and separate step.

## What an important event is

An OPERATIONAL event: something that changed the shape of the system, not something one card did on its way through
its own work. The test is whether somebody asking "what happened to atrium" would want the line.

In, because they are operational:

- the hub started (which is also how a restart reads, because a restart is a start)
- a room attached, and a room detached, with why it went
- a launch refused by the concurrency cap
- the board put on a share or overlay, or that share changing
- a room announcing it is winding down (`going-down`), which the hub already relays
- the room lifecycle the hub already records: a room added, removed, marked for deletion or unmarked, a join string
  minted or spent, a room's cache replaced on reconnect and what was discarded

Out, because they are per-card history and already have a home:

- a card being created, renamed, answered, shelved or finished
- a permission decided on one card
- an activity tick, one per tool call, which is the noisiest thing on the stream

The per-card event table (`internal/store` `AppendEvent`/`Events`, the `event` table, the `/v1/events` stream) is
that home and stays it. The audit log is a DIFFERENT question with a DIFFERENT lifetime: a card's history dies with
the card, an operational event outlives every room it names.

## Where it lives

In `internal/hubstore`, because the hub is the one place that sees all of it. The hub sees its own lifecycle, it
sees every room attach and detach because it holds the socket, it decides the launch cap, and it relays what the
rooms say. A room sees only itself. Putting the collector anywhere else would mean gathering back to the hub what
the hub already had.

The table already exists. `room_audit` (schema.go, migration `0001_rooms`) and `Store.Log`/`Store.Audit` were built
to keep what happened to a room after the room is gone, with no foreign key on purpose so forcing a room out does
not delete the record of having done it. That is the same durability an operational log needs, and the same shape:
an id, a time, an optional room, a kind, a detail. So the audit log is that table, widened in use rather than
replaced. Rows with no room are hub-level events. Rows with a room are about that room. Nothing migrates.

`Store.Log(r *Room, kind, detail)` is the one writer. It is already BEST EFFORT and says so: every caller is in the
middle of doing the thing being recorded, and failing an attach because its audit line would not write is the tail
wagging the dog. A store that truly cannot write has already halted through `guard`, which is the loud half. The new
call sites inherit that posture unchanged.

## Bounded and retained

The table must not grow forever. A busy fleet attaches, detaches and launches all day, and an unbounded log is a
disk leak with a nice name.

Retention is a ROLLING CAP BY ROW COUNT, trimmed on write. After an insert, `Log` deletes everything older than the
newest `auditCap` rows, using the `room_audit_at` index that already exists. The cap is generous (thousands of
rows) because a row is small and the value of the log is history, but it is a cap. This mirrors the event-sink's
bounding posture: keep a rolling window, drop the oldest, never stall the writer. The trim is inside the same
`guard` as the insert, so it retries contention and never halts the hub on its own.

The read side is bounded too. `Audit` already caps a request at 1000 rows and defaults to 200. The board asks for a
page, not the whole table.

## What the board shows

A new top-level tab, `audit`, beside the others. Read-only. It draws the log newest first, one row per event: the
time, the room (or nothing, for a hub-level line), the kind as a small label, and the detail. Two filters, a room
and a kind, because the two questions people ask are "what happened to THAT machine" and "show me every refused
launch".

The tab is HUB-ONLY in the UI as well. A plain daemon answers `/_hub/*` with a 404, so the tab reveals itself only
when the hub probe (`js/rooms.js`, `loadHubRooms`) has found a hub, the same way the room counter does. On a plain
daemon it is simply not there.

## How the board gets it

`GET /_hub/audit`, under the reserved `/_hub/` prefix so it can never collide with a board route a room grows later.
It takes `limit`, `room` and `kind` as query parameters and answers newest-first JSON. It is served by the hub
proxy (`serveHubAPI` in `internal/link/proxy.go`), which reads the log through a small `AuditLog` interface the
daemon wires to the store, so `internal/link` never learns the hub has a database, exactly as `Inventory` and the
control server already avoid it.

Live updates follow the board's own doctrine: DELTAS, NEVER STATE (see `internal/link/events.go`). When an event is
recorded the hub emits an `audit` delta on the existing hub event stream, carrying only that something happened. The
pane does a full `GET /_hub/audit` when it opens and again whenever the stream reconnects, and on an `audit` delta
it re-fetches the page. A missed delta costs one re-fetch, which is the recovery the whole stream already rests on,
and it means the audit pane needs no replay buffer and no second long-lived connection. A board with the pane
closed pays nothing.

Fail-open all the way. A hub with no audit wiring answers `/_hub/audit` empty rather than erroring, an unresolved
room name records a hub-level line rather than dropping the event, and the delta emit is best effort like every
other write on that stream.

## Hub-observable now versus room-side later

The first slice records everything the hub can see WITHOUT a room emitting anything new:

- `hub-started` at daemon startup
- `room-attached` when a room adopts, and `room-detached` when the hub lets it go, carrying the reason (hung up, no
  heartbeat, its record was removed, its listener stopped)
- `launch-refused` when the concurrency cap turns a launch away
- `board-share-opened` when the board is put on a zrok share at startup
- `room-going-down` relayed from a room that announces it is winding down
- the existing room lifecycle already written by `Store.Log`

Room-side only, DEFERRED to a planned room restart, because only the room can emit them and it does not yet:

- a session starting, finishing or exiting on a room, with the reason. The hub infers a card appearing or leaving,
  which is not the same as the room saying a session ended and why.
- a permission requested or decided as an OPERATIONAL line with the actor and the outcome. The hub relays the raw
  `permission` event, but turning it into an audit line means reading a payload whose shape is the room's, and
  guessing it here is how the two halves drift. This is a hub-only follow-up once the shape is pinned, and does not
  need a room restart, but it is out of the first slice.
- a room's own health degrading or its store halting, which the room knows and the hub only sees as silence.
- a file transfer, and a share or overlay opened ON a room rather than on the hub's board.
- a clean `hub-stopping` line. A restart's stop runs detached (see docs/reload-design.md), so a reliable
  end-of-life line needs a shutdown hook that survives the wind-down. Start is recorded, which already makes a
  restart visible as the gap plus the next `hub-started`.
