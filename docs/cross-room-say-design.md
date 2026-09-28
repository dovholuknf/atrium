# Cross-room `atrium_say` (backlog-2 item 58)

Status: reviewed (mercurius `s_H1ILoNvxloBH`, ready_to_build) and built. Written by sa58.

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
- A request the hub proxies to a room that is not attached answers 503 (`ErrNoRoom` in `proxy.go oops`). A room
  that is attached and slow answers 503 too, and a broken link answers 502.

## Addressing grammar

Parsed by one helper per side (`internal/link/address.go` and `internal/daemon/address.go`), pinned together by a
table test in each package over the same cases, since neither package imports the other.

- `name` or `@name`: the sender's own room. Unchanged behaviour. Handle first, then alias, then card id.
- `name@room` or `@name@room`: card `name` (handle, then alias, then id) on room `room`. Split on the LAST `@`, so
  a handle that holds an `@` still parses as long as the room part is given. Room names match case-insensitively,
  as the hub already does (`equalFold`, `keyOf`). Aliases cannot hold `@` (`aliasShape`).
- `room~id`, the aggregate board's tagged id, is the same as `id@room`.
- A room part naming the sender's own room is the same as no room part.
- An empty name or an empty room part (`sa1@`, `@m1mini`) is refused with the grammar in the sentence.
- The sender is always written `handle@room` on a cross-room message, so the recipient replies to exactly what it
  was shown. The hub appends the room from the link certificate, never from the body, so a room cannot speak for
  another room.

## The route

```
card on m1mini                    m1mini room               hub                    claude-sg4 room
atrium_say to=atrium-87300@claude-sg4
  stdio atrium control ---------> POST /v1/say
                                  local ledger (peerSaid)
                                  Room.Relay  ------ relay kind -----> serveRelay
                                                                       resolve on claude-sg4
                                                                       POST /v1/tasks/<id>/message
                                                                       from=sa1@m1mini ----> QUEUED or typed
                                                                                             through the gate
                                  <------------------ answer ---------
  <------------------------------ answer
```

1. ROOM-SIDE entry. The room's new `POST /v1/say` on the board API takes `{from, to, text, when}`. The agent
   listener's `/tell` (`atrium tell`) takes the same grammar. A local target takes today's path, the same code
   `handleMessage` runs. A cross-room target is relayed.
2. HUB-SIDE entry, a card on the hub's machine using `/_hub/mcp`. `sayHandler` sees a room part that is not the
   caller's and forwards the say to the SENDER'S room as `POST /v1/say`, so the sender's room records it exactly as
   it records a say from its own stdio server. The route then continues as above. A caller with no
   `X-Atrium-Agent` is refused for cross-room: an unnamed sender would be typed as the operator, which rule 3
   forbids. A caller with no room header keeps today's behaviour exactly (the aggregate list, where `room~id`
   already reaches any room), because it has no room to be the sender's.
3. LINK. A new connection kind, `relay`, dialled by the room, the same shape as `announce`: hello, welcome, one
   JSON request, one JSON answer, closed. Ops `say` and `peers`. The hub takes it from an attached room only.
4. HUB-SIDE delivery. `serveRelay` resolves `name` in the target room through its own loopback board with
   `X-Atrium-Room: <target>` (the existing `controlMCP.resolvePeer` and `ask`), then posts
   `/v1/tasks/<id>/message` with `from` = `handle@sourceRoom`. The target room delivers it as any peer message:
   QUEUED, typed only through the gate (empty line, turn rule), carried by the hooks otherwise. Never as the
   operator. The hub holds nothing: it answers the relay with the target room's answer and forgets it.

Why the sender's room and not the hub records the say: `peerSaid` (the work ledger, and `MarkReported` when the
target is the sender's launcher) is room-side and keyed on the local sender card. Routing every cross-room say
through the sender's room keeps one record, in the one place that owns it.

What the target room does with `from = sa1@m1mini`: `peerLimit` counts it under that name, the banner reads
`[atrium] sa1@m1mini says:`, and `peerSaid` finds no local card by that name and records nothing, which is right
because the sender's room already did.

## What each side sees

Before, a card on m1mini:

```
atrium_say to=atrium-87300@claude-sg4
error: no session called "atrium-87300@claude-sg4". these would have worked: sa1, sa2
```

After, the sender:

```
{"delivered":"queued","to":"atrium-87300@claude-sg4","card":"claude-sg4~01K...","when":"immediate",
 "note":"queued, not typed yet. ..."}
```

After, the recipient on claude-sg4, typed through the gate or carried by the hook:

```
[atrium] sa1@m1mini says: the build is green on macOS. sha 1a2b3c.
```

It answers with `atrium_say to=sa1@m1mini`, the handle it was shown.

With m1mini's hub link down, the sender sees:

```
{"delivered":"held","to":"atrium-87300@claude-sg4","note":"the hub or room claude-sg4 is not answering. held on
 this room and sent when it answers, for up to 24 hours. nothing is queued on the hub."}
```

## Offline target, and what the sender is told

- The relay tries synchronously first. Success returns the target room's answer (`terminal`, `queued`,
  `undeliverable`, `queued-unconfirmed`, `when`, warning) with `to` = `handle@room` and `card` = `room~id`.
- A definitive refusal from the target (no such session, it ended, too long, rate limited, bad `when`) is returned
  to the sender as an error with the target's sentence, and nothing is kept. The not-found error lists the live
  handles on that room, as a local one does.
- Unreachable goes into a ROOM-SIDE outbox on the sender's room, and the answer is `delivered: "held"` with the
  note above. Unreachable means the failure is KNOWN to come before the message reached the target: no link from
  this room right now, the relay dial or hello failing, the request not written, the hub saying the target room is
  not attached, or the resolve step (a read) on the target answering 502 or 503.
- Ambiguous is anything after the message may have reached the target: the message post answering 502, 503 or 504,
  or the relay answer not arriving after the request was written. The sender is told `delivered: "unconfirmed"`
  with a note saying it may or may not have arrived, and nothing is held, so a retry cannot deliver it twice. On a
  drain, an ambiguous say is dropped with an event on the sender's card. An ambiguous automatic notice is kept and
  tried again, because a launcher told twice is better than a launcher never told.
- A room this hub has never heard of (not attached and not in its inventory) is a definitive 404 that names the
  rooms it knows, not a held message, so a typo is said at once rather than held for a day.
- The outbox is drained on the reaper tick, right after an insert, and when the link reattaches. One drain at a
  time. Rows go oldest first. A row that meets a definitive refusal on drain is dropped with an event on the sender's
  card and a log line, since the sender's turn that asked for it is long gone.
- Kept 24 hours from when it was held, then dropped with the same event and log line.
- Automatic notices to a launcher on another room go into the same outbox, so a report to a launcher on an offline
  room is not lost.
- Migration: one table, appended at the END of the slice in `internal/store/schema.go`, `CREATE TABLE IF NOT
  EXISTS`:

  ```sql
  relay_outbox (id TEXT PRIMARY KEY, from_task TEXT NOT NULL DEFAULT '', from_wire TEXT NOT NULL,
    to_room TEXT NOT NULL, to_name TEXT NOT NULL, to_card TEXT NOT NULL DEFAULT '', text TEXT NOT NULL,
    when_word TEXT NOT NULL DEFAULT '', source TEXT NOT NULL, created_at TEXT NOT NULL,
    attempts INTEGER NOT NULL DEFAULT 0, last_error TEXT NOT NULL DEFAULT '')
  ```

  A sent row is deleted, so the table only ever holds what is owed. `to_card` is the target's bare card id on its
  room when it is known, which is always the case for a launcher notice (below), and the drain addresses the card
  by that id rather than by the handle, so a handle reused later cannot take a notice meant for another card.

## Notices to a launcher on another room (rule 5)

- `spawned_by` may be `handle@room`. The hub's `atrium_launch` gains an optional `room`: launching into another
  room records `spawned_by` = `me@myroom` on the new card, and `spawned_by_id` = `myroom~<my card id>`, resolved
  by the hub on the caller's room. The room stores a tagged `spawned_by_id` as given (a bare one is still resolved
  locally, as today). Nothing local can match a tagged id, so `launcherOf` and the ledger's arbiter lookup find
  nothing and stay local, as they should. A same-room launch is unchanged.
- `launcherOf` stays local. A new `remoteLauncher(worker)` returns `(name, room)` when `spawned_by` names another
  room and no local card is the launcher.
- `notifyLauncher` (silent stop, long tool, context size) keeps its `RecordNotice` dedupe and then, for a remote
  launcher, holds the notice in the outbox and kicks a drain. It returns true, so the caller does not retry.
- `finish` (a report) writes the outbox row inside `RecordReport`'s transaction, through a new `ReportWrite.Relay`
  beside `Notice`, so a crash cannot record the report and lose the notice. `launcher_told` is true.
- The worker's own say to its launcher across rooms marks it reported: `sayAcross` compares the resolved
  `handle@room` (and the address as typed) with `spawned_by`, case-insensitive on the room.
- The ledger's `ended` notice (`queueNotice`) still needs a local arbiter card and is not carried across rooms by
  this item. The silent-stop and report notices cover the case clint named. Nor is a remote launcher's verdict on
  the worker's work item: the ledger's arbiter is a local card, and a tagged id matches none.
- The work ledger's `say` entry needs both cards local, so a cross-room say is not logged on the worker's item. The
  sender's room still marks the worker reported.
- The hub also sees it. The hub proxies the launcher's card list with `context_size` today. A notice reaches the
  launcher's card as an ordinary queued peer message, which is on that card's timeline and so on the hub's board.

## Peers (rule 4)

- Hub-side `atrium_peers` gains `rooms: true`, which lists live cards on every attached room from the aggregate
  `/v1/tasks` (each row carries `room`, and a remembered row of a room that is not answering carries `offline` and
  is left out). A row from another room carries `room`, handle `name@room` and card `room~id`. Rows of the caller's
  own room keep bare handles. Without `rooms` the answer is unchanged.
- Room-side (stdio `atrium_peers`, `rooms: true`) asks the room's new `GET /v1/peers/rooms`, which asks the hub
  through the relay op `peers`. The hub answers from the same aggregate list, minus the asking room.

## Version skew

- Old hub, new room: the `relay` hello is refused with "a connection is control, data, enrol, upgrade or announce".
  The room says "the hub is older than cross-room say" and does NOT hold the message, since nothing would ever
  drain it. A held row that meets an old hub on drain (the hub was downgraded) stays and expires.
- New hub, old room as sender (hub-side entry): the forward to `/v1/say` gets a 404. The hub falls back to
  delivering directly with `from` = `handle@room`, and the answer's note says the sender's room is older, so its
  work ledger did not record the message.
- New hub, old room as target: `/v1/tasks/<id>/message` with `from` has existed since the peer bus. Works.
- New stdio `atrium control` against an old room: `/v1/say` is a 404, and a bare name falls back to today's path
  (now with `from`). A cross-room name is refused with "this room is older than cross-room say".
- The link `Version` does not change. A new kind is additive.

## A room without atrium-control

m1mini today: no atrium-control, so its sessions cannot call `atrium_say` or `atrium_report` at all. They can still
be reached (a relayed message is queued and carried by the hooks), and `atrium tell` works if the binary is on PATH.
The fix is provisioning: `scripts/provision-room.ps1` (item 46) registers the stdio server at user scope with
`claude mcp add --scope user atrium-control -- <atrium> control`, idempotently, and `-Remove` takes it off. The
stdio server gains `from` on `atrium_say` and an `atrium_report`, so a room's cards can answer and report.

## Which side each part lives on

| Part | Side |
|---|---|
| Grammar parse for `/_hub/mcp`, forward of a cross-room say to the sender's room, `serveRelay` (say, peers), `atrium_peers rooms`, `atrium_launch room` | HUB-SIDE |
| Grammar parse, `POST /v1/say`, `/tell` grammar, `GET /v1/peers/rooms`, outbox and drain, remote launcher notices, report into the outbox, cross-room `peerSaid` | ROOM-SIDE |
| stdio `atrium control`: `from`, `atrium_report`, grammar, `rooms` | ROOM-SIDE (runs on the room's machine) |
| `relay` connection kind, `Room.Relay` client, `Room.OnAttach` | link, both |
| provisioning registers atrium-control | script |

## Review

Mercurius design review, session `s_H1ILoNvxloBH`, round 1, verdict needs_changes. Both findings folded in.

- C1, an ambiguous 502 or 503 could hold a message the target already queued, and a drain would send it twice.
  Folded in: only failures known to come before delivery are held. The rest answer `unconfirmed` and hold
  nothing, except an automatic notice, where a duplicate beats a loss. See "Offline target".
- C2, a held launcher notice keyed on a handle could reach a later card that reused the handle. Folded in: the
  cross-room launch records the launcher's tagged card id, and the outbox addresses notices by that id.
- A1 (advisory), telling an old hub from the refusal sentence is brittle. Kept, narrowed: the room matches only
  the unknown-kind refusal, which an old hub has sent unchanged since the kind switch existed, and a test pins it.
  There is no other signal an old hub sends.

Round 2, verdict ready_to_build. One advisory, folded in: a dropped held message (expired, refused, or an ambiguous
say) is a `notified` event on the sender's card with `kind: "relay-dropped"`, `to`, `why` and the text, and a log
line `gave up on <sender>'s message to <name@room>: <why>`.
