# Cross-room `atrium_say` (backlog-2 item 58)

Status: DRAFT, not yet reviewed. Written by sa58 before a context handoff. The next session finishes it, then runs the
mercurius design review.

## What is true today

- The hub's control MCP (`internal/link/control_mcp.go`, mounted at `/_hub/mcp`, loopback only) resolves every
  handle inside the caller's own room (`X-Atrium-Room`) and posts `/v1/tasks/<id>/message` with `from` set to the
  caller's `X-Atrium-Agent`. So a card on the hub's machine cannot name a card on another room.
- The hub's `/v1/tasks/room~id/message` does reach another room, but a caller without `from` is typed as the
  operator. With `from` set the room already frames it as a peer (`peerBanner`, `QueueFromPeer`).
- A room on another machine cannot reach `/_hub/mcp` (loopback only, and that stays). Checked on m1mini by
  atrium-87300: its cards run NO atrium-control at all. `claude mcp list` shows only claude.ai connectors.
- The stdio `atrium control` (`internal/cli/control.go`, `control_peers.go`) talks to the LOCAL daemon through
  `ATRIUM_LOCATION`. A room sets that to its private file and launched cards inherit it, so a stdio child on a room
  finds that room. Its `atrium_say` sends NO `from` today, so its messages are typed as the operator. It has no
  `atrium_report`.
- Every link connection is dialled by the room. The hub only answers. Kinds: control, data, enrol, upgrade,
  announce (`internal/link/protocol.go`, `hub.go take`). An old hub refuses an unknown kind with a sentence.
- Launcher notices (`notifyLauncher` in `internal/daemon/a2a.go`, context size in `contextsize.go`, reports in
  `finish.go` through `store.NoticeSpec{ToID}` inside `RecordReport`) only find a launcher on the same room
  (`launcherOf`). `spawned_by` carries a bare handle.

## Addressing grammar

Parsed in one place per side (a shared helper in each of `internal/link` and `internal/daemon`, pinned together by
a test, since neither imports the other).

- `name` or `@name`: the sender's own room. Unchanged behaviour. Handle first, then alias.
- `name@room` or `@name@room`: card `name` (handle, then alias) on room `room`. Split on the LAST `@`. Room names
  are matched case-insensitively, as the hub already does (`equalFold`, `keyOf`). Aliases cannot hold `@`
  (`aliasShape`), and handles do not in practice.
- `room~id` (the aggregate board's tagged id) and `id@room`: a card id on that room.
- A room part naming the sender's own room is the same as no room part.
- The sender is always written `handle@room` on a cross-room message, so the recipient replies to exactly what it
  was shown. The hub appends the room from the link certificate, never from the body, so a room cannot speak for
  another room.

## The route

Two entry points, one delivery.

1. HUB-SIDE, a card on the hub's machine using `/_hub/mcp`: `sayHandler` sees a room part that is not the caller's.
   It forwards the say to the SENDER'S room (`POST /v1/say` on that room, below), so the sender's room records it
   exactly as it records a room-originated one. A caller with no room header delivers straight to the target room
   with `from` = its handle. A caller with no `X-Atrium-Agent` is refused for cross-room: an unnamed sender would be
   typed as the operator, which rule 3 forbids.
2. ROOM-SIDE, a card on any room using the stdio `atrium control` or `atrium tell`: the room's new `POST /v1/say`
   (board API) and the agent listener's `/tell` accept the grammar. Local targets take today's path. A cross-room
   target is relayed.
3. LINK, new connection kind `relay`, room to hub, the same shape as `announce`: hello, welcome, one JSON request,
   one JSON answer. Ops: `say` and `peers`. The hub requires the room to be attached (`h.Has(name)`).
4. HUB-SIDE delivery: the hub resolves `name` in the target room through its own loopback board with
   `X-Atrium-Room: <target>` (the same `controlMCP.resolvePeer` and `ask`), then posts
   `/v1/tasks/<id>/message` with `from` = `handle@sourceRoom`. The target room delivers it as any peer message:
   QUEUED, typed only through the gate (empty line, turn rule), carried by the hooks otherwise. Never as the operator.
   The hub holds nothing: it answers the relay with the target room's answer.

Why the sender's room and not the hub records the say: `peerSaid` (the work ledger and `MarkReported` when the
target is the sender's launcher) is room-side and keyed on the local sender card. Routing every cross-room say
through the sender's room keeps that single source of truth.

## Offline target, and what the sender is told

- The relay tries synchronously first. Success returns the target room's answer (`terminal`, `queued`,
  `undeliverable`, `when`, warning) with `to` = `handle@room`.
- A definitive refusal (no such session on that room, it ended, too long, rate limited) is returned as an error and
  nothing is kept. The not-found error lists the live handles on that room.
- Unreachable (hub not attached, hub older than `relay`, target room not attached, link error, 502/503): the message
  goes into a ROOM-SIDE outbox on the sender's room and the answer is `delivered: "held"` with a note naming the
  room and saying it is sent when the hub and that room answer. Nothing is queued on the hub.
- The outbox is drained on the reaper tick, right after an insert, and when the link reattaches. Kept 24 hours, then
  dropped with an event on the sender's card and a log line. Automatic notices go into the same outbox, so a report
  to a launcher on an offline room is not lost.
- Migration: one table, `relay_outbox` (id, from_task, from_wire, to_room, to_name, text, when, source, created_at,
  attempts, last_error, sent_at), appended at the END of the slice in `internal/store/schema.go`,
  `CREATE TABLE IF NOT EXISTS`.

## Notices to a launcher on another room (rule 5)

- `spawned_by` may be `handle@room`. The hub's `atrium_launch` gains an optional `room`: launching into another
  room records `spawned_by` = `me@myroom`. A same-room launch is unchanged.
- `launcherOf` stays local. A new `remoteLauncher(worker)` returns `(name, room)` when `spawned_by` names another
  room. `notifyLauncher` (silent stop, long tool, context size) and `finish` (report) enqueue into the outbox for a
  remote launcher. `RecordReport` writes the outbox row in the same transaction it writes the local notice today.
- `peerSaid` marks a worker reported when it says to `spawned_by` across rooms, compared after normalising both.

## Peers (rule 4)

- Hub-side `atrium_peers` gains `rooms: true`, which lists live cards on every attached room from the aggregate
  `/v1/tasks` (each row carries `room`). Rows from other rooms carry `room` and handle `name@room`. Without it the
  answer says how many live sessions are on other rooms.
- Room-side (stdio `atrium_peers`) gets the same through the relay op `peers`.

## Version skew

- Old hub, new room: the `relay` hello is refused with "a connection is control, data, enrol, upgrade or announce".
  The room says "the hub is older than cross-room say" and does NOT hold the message (it would never drain).
- New hub, old room as sender: the old room has no `/v1/say`. The hub's forward gets a 404 and falls back to direct
  delivery with `from` = `handle@room`, saying the sender's room is older so the ledger did not record it.
- New hub, old room as target: `/v1/tasks/<id>/message` with `from` has existed since the peer bus. Works.
- The link `Version` does not change: a new kind is additive.

## A room without atrium-control

m1mini today: no atrium-control, so its sessions cannot call `atrium_say` or `atrium_report` at all. They can still
be reached (a relayed message is queued and carried by the hooks), and `atrium tell` works if the binary is on PATH.
The fix is provisioning: `scripts/provision-room.ps1` (item 46) registers the stdio server at user scope,
`claude mcp add --scope user atrium-control -- <atrium> control`, idempotently, and `-Remove` takes it off. The stdio
server gains `from` on `atrium_say` and an `atrium_report`, so a room's cards can answer and report.

## Which side each part lives on

| Part | Side |
|---|---|
| Grammar parse, forward from `/_hub/mcp` say, relay server, target resolution, peers across rooms, launch `room` | HUB-SIDE |
| `/v1/say`, `/tell` grammar, outbox and drain, remote launcher notices, report outbox, `peerSaid` across rooms | ROOM-SIDE |
| stdio `atrium control` `from`, `atrium_report`, cross-room peers | ROOM-SIDE (runs on the room's machine) |
| `relay` connection kind, `Room.Relay` client | link, both |
| provisioning registers atrium-control | script |
