# A card on every room (backlog-2 item 49)

Status: built. A card tagged `atrium:everywhere` is shown on and reachable from every room (`internal/daemon/peers.go`,
`internal/cli/control_peers.go`).

Origin: designed, not built. Written by @fabric.

clint wants to put the orchestrator on every room: seen in each room's view, and addressable from each. A card runs
in one place and that does not change. What changes is what the OTHER rooms' views show, and how a bare name
resolves from them.

## What is true today

- A card belongs to one room's store. The hub's aggregate view (no room picked) already shows every card on every
  room, tagged `room~id` with `room` on the row (`internal/link/fanout.go`). A scoped view is a byte pipe to one
  room (`docs/fabric/hub-room-requirements.md`, "scoped: a dumb pipe").
- Item 58 gives `name@room`, the `relay` link kind, the room-side outbox, and hub-side resolution on the target
  room (`docs/fabric/cross-room-say-design.md`). So the orchestrator IS reachable from sg3 today, as
  `atrium-87300@claude-sg4` or `orchestrator@claude-sg4`. A bare `orchestrator` from sg3 is a 404, because a bare
  name means the caller's own room.
- Item 63 made the card beat the header (`internal/link/cardroute.go`): a request whose path names `room~id` goes
  to that room whatever `X-Atrium-Room` says. So a scoped board that holds a tagged id can already open, attach to,
  message and read that card. It just never holds one.
- Every room tells the hub what it holds (`internal/link/announce.go`, `announce` kind), within two seconds of a
  change, into `hubstore.room_card`. The payload is the stored row from `GET /v1/state`, `tags` and `alias`
  included. So the hub already knows every card's tags on every room, attached or not, with no new wire field.

## The mark

A tag, `atrium:everywhere`, set the way any tag is set: the board's tag editor, `atrium launch --tags`, or
`atrium_launch` with `tags`. No migration, no column, nothing new in the room.

Why a tag and not a hub-side list: whether a card is on every room is a fact about the card, it is set where the
card is edited, and it survives the hub being replaced. A hub-side list of card ids would be a second source of
truth about a card that federation-design-v2 rules out, and it would go on naming a card after its room deleted it.

Rejected: a mirror card in each room's store. Two rows for one card is the shape `docs/rnd/federation-design-v2.md`
exists to rule out, and each copy would drift the moment the real one moved column.

## The hub's index

The hub keeps an in-memory index: for each room, the cards in its last announcement that carry the tag and whose
status is not `done` or `dead`. Built from `room_card` when the hub starts, and replaced for one room on every
announcement from it (the `Cached` callback that already writes the cache). Nothing new on disk.

The status rule is the one alias resolution already uses: an ended card keeps its tag as a record and no longer
answers to it. A resident card passing through `dead` on a restart drops out for the seconds that takes, which is
the truth.

The index is what both halves below read. It is never the source of a card's CONTENT: a live room is asked, and
the cache is read only for a room that is not attached, the rule `remembered` in `fanout.go` already follows.

## Addressing

A bare name, from a caller on room R:

1. **R's own cards first**, handle, then alias, then id, exactly as today. Local wins, always. A card on sg3 aliased
   `orchestrator` shadows the everywhere one for sg3's callers, which is what someone who made that alias meant.
2. **Then the everywhere index**, excluding R: a card whose handle is the name, or whose alias is the name (case and
   a leading `@` ignored, as aliases are matched now). Not by card id: another room's card is `room~id`, which
   already routes.
3. **Exactly one match** is used as `handle@room`, and the answer says so: `to` is `atrium-87300@claude-sg4`,
   `card` is `claude-sg4~<id>`. The sender learns the real address from its first message.
4. **Two or more** (two rooms each tagged a card aliased `orchestrator`) is a 409 naming every match as
   `handle@room (@alias)`. Nothing is sent. The hub cannot refuse the tag, since tags are the room's, so the refusal
   is at the point of use, where it can name both.
5. **None** is today's 404 with the local peer list, plus the everywhere cards as `handle@room`.

### Which tools fall through

| Tool | Falls through | Why |
|---|---|---|
| `atrium_say`, `atrium tell`, `POST /v1/say` | yes | the point of the item |
| `atrium_task` | yes | a read, and reading the orchestrator from a worker's room is the second thing asked for |
| `atrium_peers` | lists them | every room's answer carries the everywhere cards, below |
| `atrium_exit`, `atrium_cull`, `atrium_alias` | NO | a bare name that ends or renames a card on another machine is too far to reach by accident. `name@room` still works, as today |
| launcher notices, reports | unchanged | `spawned_by` already names `handle@room` for a cross-room launch |

### Where it runs

**Hub-side entry** (a card on the hub's machine, `/_hub/mcp`): `controlMCP.resolvePeer` misses in the caller's
room, the handler reads the index, and a single match continues through the existing `sayAcross` (forwarded to
the sender's room as `POST /v1/say` with `to` = `handle@room`, so the sender's room records it as it records any
cross-room say). `atrium_task` continues through `cardAcross`.

**Room-side entry** (`POST /v1/say`, `/tell`, the stdio `atrium control`): `localTarget` misses, the room asks the
hub one new relay op, `find` `{to: name}`, and the hub answers from the index: `{ok, to: handle@room, card:
room~id}`, the 409, or the 404 with the everywhere list. On a match the room continues through its existing
`sayAcross` with the explicit room, unchanged, so holding, `unconfirmed` and the outbox all behave exactly as they
do for a typed `name@room`.

Two round trips rather than one combined op, on purpose: `find` is a read with no delivery semantics, and every
delivery outcome stays in the one path item 58 already reviewed.

**The owning room offline.** The index still holds its cards (from the cache), so `find` answers the address and
the say that follows is `held`, since the room is one the hub knows. A worker telling the orchestrator while
claude-sg4 is shut gets its message delivered when claude-sg4 comes back, which is the answer it would get had it
typed the full address.

**The hub offline, or older.** The room cannot look. The answer is today's 404 with a note: "the hub is not
answering, so no card on another room was looked for" or "the hub is older than cards on every room". Nothing is
held. A bare name the room cannot resolve might be a typo, and item 58 already decided a typo is said at once
rather than held for a day. An old hub answers `find` with "this hub does not know the relay op", which the room
already recognises.

### Peers

`atrium_peers` without `rooms` gains the everywhere cards of other rooms, each `{handle: name@room, card: room~id,
room, everywhere: true}`, after the local rows. Hub-side from the index. Room-side through the existing relay op
`peers` with a new `everywhere: true` field, which an old hub ignores (the answer is then the full `rooms` list,
which the room filters on the `everywhere` flag each row lacks, so it adds nothing). A hub that is not answering
leaves the local list as it is, with a `warning`.

## Seeing

A scoped view shows the everywhere cards of other rooms beside its own, marked with their room.

**The list is a hub endpoint, not a change to the pipe.** `GET /_hub/everywhere?room=<scope>` answers `{"tasks":
[...]}`: every indexed card not on `<scope>`, fetched live from its room (`GET /v1/tasks/<id>`, bounded, in
parallel, one per card and usually one in all), tagged `room~id` with `room` and `everywhere: true` on the row. A
card whose room is not attached comes from the cache with `offline: true`, drawn and refused the way
`remembered` rows already are. The scoped `/v1/tasks` stays a byte pipe, untouched.

**The stream is where the pipe grows, and only when asked.** `/v1/events/room/<name>?everywhere=1` carries that
room's events exactly as today, plus, for the cards in the index on other rooms, their `task`, `task-removed`,
`activity` and `keepalive` events, tagged with `tagEvent`. Tagged ids cannot collide with the room's own bare ids.
Without the parameter the stream is byte for byte what it is now, so an old board sees nothing new. When the index
changes, the hub sends an `everywhere` event on these streams and the board fetches the list again, the same way a
`rooms` event already tells it membership changed.

In `internal/link/events.go` that is: a sub records whether it asked for everywhere, `wanted` counts the rooms
holding an indexed card as wanted while such a sub exists, and `emit` hands an event from another room to that sub
when its kind is one of the four and its id field names an indexed card. The cost is one upstream stream per room
holding an indexed card, which is usually one room, and none while no scoped board is open.

**Not forwarded: permissions.** A permission answer is the one write that does not route by card. Its path is
`/v1/permissions/<id>/decide`, which carries a permission id, so the hub would send it with the scoped view's
header to the wrong room. So the scoped view shows the foreign card's `needs-permission` column and answers
nothing for it. The card links to its own room's view. Widening this means teaching `placeCard` a tagged permission
id, which is a small change and a separate one.

**Not counted.** The room's badges and totals are that room's cards. A foreign card is shown, not counted, since a
number in sg3's header that sg3 cannot act on is a number that makes you look.

**The board's part is @ui's.** Fetch `/_hub/everywhere` beside `/v1/tasks` in a scoped view and again on an
`everywhere` event or a reconnect, open the stream with `?everywhere=1`, and draw those rows with a room chip,
which a scoped view otherwise never draws. Routing needs no board change, because every per-card url built from a
tagged id already goes to the right room (item 63). Where the chip goes and whether the rows get their own group
are @ui's to decide.

## What each side sees

A worker on sg3, before:

```
atrium_say to=orchestrator
error: no session called "orchestrator". these would have worked: sa1, sa2
```

After:

```
{"delivered":"queued","to":"atrium-87300@claude-sg4","card":"claude-sg4~01K...","when":"immediate",
 "note":"queued, not typed yet. ..."}
```

Two rooms each with an everywhere card aliased `orchestrator`:

```
error: "orchestrator" names a card on more than one room: atrium-87300@claude-sg4 (@orchestrator),
atrium-5120@sg3 (@orchestrator). say which, as name@room
```

## Version skew

- New room, old hub: `find` is refused as an unknown op. The room answers the local 404 with "the hub is older
  than cards on every room". Nothing held.
- Old room, new hub: nothing from that room ever asks `find`. The hub-side entry still falls through for cards on
  the hub's machine. A scoped view of the old room still gets the list and the stream additions, since both are
  the hub's.
- Old board, new hub: it never asks `/_hub/everywhere` or `?everywhere=1`, and sees exactly what it sees now.
- New board, old hub: `/_hub/everywhere` is a 404, which the board reads as "none".
- The link `Version` does not change. `find` and the `everywhere` peers field are additive.

## Which side each part lives on

| Part | Side |
|---|---|
| the index, the `find` op, the hub-side fall-through in say and task, peers rows, `/_hub/everywhere`, the stream additions and the `everywhere` event | HUB-SIDE (`internal/link`) |
| `localTarget` miss asks `find`, then the existing `sayAcross`, the 404 note, peers filter | ROOM-SIDE (`internal/daemon/relay.go`, `peers.go`) |
| stdio `atrium control`: nothing new, it already posts `/v1/say` and reads `/v1/peers` | ROOM-SIDE |
| the scoped board drawing foreign rows with a room chip | UI (@ui) |
| setting the tag | nothing new |

No migration on either side.

## Test plan, when built (letter FE)

- FE1: tag the orchestrator `atrium:everywhere`. From a card on sg3, `atrium_say to=orchestrator` delivers, and the
  answer's `to` is `atrium-87300@claude-sg4`.
- FE2: a local card on sg3 aliased `orchestrator` wins over the everywhere card for sg3's callers only.
- FE3: two everywhere cards with one alias on two rooms: the say is refused naming both, nothing is delivered.
- FE4: claude-sg4 not attached: the say from sg3 is `held` and arrives when claude-sg4 reattaches.
- FE5: the hub stopped: the say from sg3 is a 404 with the "hub is not answering" note, nothing held.
- FE6: `atrium_exit orchestrator` from sg3 is refused as a local 404. `atrium_exit orchestrator@claude-sg4` works.
- FE7: the sg3 scoped view shows the orchestrator with a room chip, its column moves live, its terminal opens, and
  a permission on it shows the column but no answer buttons.
- FE8: removing the tag drops it from the sg3 view within a few seconds without a reload.
- FE9: an old board against the new hub: the scoped stream is unchanged byte for byte.
- FE10: a new room against an old hub: `atrium_peers` without `rooms` lists the local rows only. The old hub answers
  the `peers` op with every room's cards, none carries `everywhere`, and the room appends none of them. Pin this as
  a unit test too, since today's `rooms:true` answer already returns cross-room rows and forgetting the filter would
  list them all.

## Review

Mercurius session `s_L4wmA1mqNRCP`, round 1, 2026-09-29: `ready_to_build`, no concerns, no questions. One advisory:
the old-hub peers path depends on a filter over a field today's rows lack, so an implementation could list every
remote peer by forgetting it. Folded in as FE10, with a unit test named there.

## Open questions for clint

1. Is `atrium:everywhere` the name? It shows as a tag chip unless @ui hides it the way it hides `atrium:lean`.
2. Should a foreign card's permission be answerable from a scoped view? Recommended later, as a separate change.
3. Should the orchestrator be tagged by hand, or should `atrium_launch` tag a card that launches workers on
   another room? Recommended by hand: it is one card, and a rule that tags cards automatically is a way to end up
   with two.

## Out of scope

- The card running anywhere but its own room. Nothing moves.
- Exiting or renaming a card on another room by a bare name.
- A scoped view showing ANY card from another room. Only the tagged ones, which is what makes it a mark rather than
  a merged board with extra steps.
