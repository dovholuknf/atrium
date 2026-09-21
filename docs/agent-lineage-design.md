# Agent lineage: who spawned whom, and showing it

Design for recording that one session started another, and drawing that relation on the board. When the
orchestrator launches a session through `atrium_launch`, the board should be able to say YOU started this one, and
group or filter by it.

Written before the code, so the decisions can be argued with rather than reverse engineered.

## Why this exists

A session can now start another session. `atrium_launch` on the hub's control MCP spawns a real session on its own
card, and the orchestrator pattern leans on it: one supervising session fans work out to several launched ones.
Nothing records that it did. The launched card looks exactly like a card a human started from the board dialog or
one that joined on its own, so the board cannot answer the first question anybody asks of a fan-out: which of these
did that session start.

The relation is small and the value is not. A board that knows the parent can chip the child with it, group a
wave of launched work under the session that launched it, and answer "show me what the orchestrator spawned"
without the operator holding the set in their head.

## The seam that already exists

The hard part is already built, and this design rests on it rather than adding to it.

Every call to the control MCP carries `X-Atrium-Agent`, the calling session's own handle, read per request by
`agentOf` in `internal/link/control_mcp.go`. So at the moment `atrium_launch` runs, the hub already knows WHO is
launching. The launch handler has that value in hand and today throws it away.

`launchHandler` forwards to the hub's own board over loopback as `POST /v1/launch`, carrying a body the room turns
into a `daemon.LaunchRequest` and hands to `Daemon.Launch` (`internal/daemon/launch.go`). That is the path a parent
reference has to travel: header at the hub, field on the request, column on the card the room stores. Every step
of it already carries other launch-time facts the same way, `why`, `tags`, `theme`, `brief`, so lineage is one
more field beside them and not a new mechanism.

The launch returns a new card id. Cards are the durable unit, per `docs/architecture-v2.md`: a card outlives its
process, so lineage belongs on the card and never on a pid. The parent is named by its HANDLE, which is
`wire_name`, the same qualified name `atrium_peers` lists and `atrium_say` resolves through `GetByWireName`. That
is deliberate: lineage reuses the identity the peer bus already trades in rather than inventing a second one.

## The data model

Two columns on `task`, added by an append-only migration that tolerates re-running, the same shape every column
since `0005` has taken:

```sql
-- 00NN_agent_lineage
ALTER TABLE task ADD COLUMN spawned_by    TEXT NOT NULL DEFAULT '';
ALTER TABLE task ADD COLUMN spawned_by_id TEXT NOT NULL DEFAULT '';
```

- `spawned_by` is the parent's handle, the qualified `wire_name` the peer bus uses. It is what a chip prints and
  what a filter matches on.
- `spawned_by_id` is the parent's card id, stored so a click on the chip can route to the parent card. Room-tagged
  as `room~id` when the parent lives on another room, which is the form the aggregate already builds and untags
  (see `tagFor` in `internal/link/fanout.go`), so a click routes back with the board none the wiser that rooms
  exist. Empty when the id is not known at launch, in which case the handle alone still names the parent.

### Lineage is neither observed nor an override

Atrium already sorts a card's fields into two buckets. OBSERVED fields are what a runner reports and are refreshed
on every reconnect by `refreshObserved`. OVERRIDES are operator opinions that survive a reconnect untouched.
Lineage is a third kind and must not be filed as either.

It is a launch-time FACT, set once, by the daemon, at the moment the card is created or claimed, and never written
again. It is not observed, because the child runner cannot see who launched it and a reconnect must not clear it.
It is not an override, because no operator typed it and `SetOverrides` with an empty value would delete it. So it
is written by its own setter, once, in `Launch`, right where `SetPlace` and `SetOrigin` already run, and read-only
thereafter.

### Direct parent only

Store the direct parent and nothing more. A grandparent chain is derived by walking `spawned_by` from card to card,
which the board can do over the list it already holds in memory. Storing a materialised ancestor path would be a
second copy of a fact the single column already carries, and it would go stale the moment a card in the middle is
pruned.

## How the parent reference reaches the store

One field added at each hop it already passes through:

1. `launchHandler` reads `parent := agentOf(req)` (already available) and puts it on the loopback body as
   `spawned_by`, beside `why` and `tags`.
2. `daemon.LaunchRequest` gains a `SpawnedBy string` field, populated by the `/v1/launch` API handler from the
   body, exactly as `Why` and `Tags` are.
3. `Launch` resolves the parent's card id from the handle with `GetByWireName` (best effort, empty on miss) and,
   after the child card exists, writes both values with a new `store.SetLineage(id, handle, parentID)` setter,
   beside the `SetPlace`/`SetOrigin`/`SetModel` block near the end of `launchLocked`.

The resolve is best effort on purpose. A handle that does not resolve to a live card, because the parent's room is
offline or its card was already swept, still names the parent and is still worth recording. The id is the
convenience that makes the chip clickable, not the truth of the relation.

## Cross-room reality

The orchestrator lives on one room and a launched card may land on another. Two facts about atrium's naming make
this work without new plumbing.

A handle is hub-wide. `wire_name` is qualified with the room on the way into the store by `Qualify`, so
`sg4/atrium-87300` names exactly one session across the whole hub and cannot collide with another room's card. A
parent reference stored as that qualified handle stays resolvable from any room's board and through the aggregate.

A card id is room-scoped, so `spawned_by_id` carries the room the way every other cross-room card reference does,
as `room~id`. When the parent and child are on the same room the tag is unnecessary and the plain id is enough. The
design should store the tagged form whenever the parent room is known and different, so the board never has to work
out which room an id belongs to.

When the parent's room is OFFLINE, mirror how `remembered` in `fanout.go` already handles offline work. The child's
`spawned_by` handle still prints, and the board resolves it to a live card only when it can. An unresolvable parent
is drawn as a plain chip with the handle text and no link, the same posture a remembered card takes: the fact is
shown, the dead click is not offered.

One caveat worth stating rather than discovering. The `X-Atrium-Agent` header carries the name the launching
session calls ITSELF, which is its local `ATRIUM_AGENT_NAME` and not yet room-qualified. The store qualifies a name
on the way in, so lineage must qualify the parent handle the same way before storing it, or a chip stored from one
room will not match the qualified handle the peer bus lists. Qualifying it at the `SetLineage` boundary, the way
`Register` qualifies at its one boundary, keeps this in a single place.

## Human versus agent spawns

A human pressing start in the board dialog is also a spawn, and losing that case would make lineage a partial
record. Three origins have to stay distinguishable:

- **Agent launch.** `X-Atrium-Agent` is present. `spawned_by` is that handle.
- **Human launch.** An interactive launch from the board dialog, which `LaunchRequest.Interactive` already marks,
  with no agent header. `spawned_by` is the reserved token `@human`. `@` cannot appear in a `wire_name`, because
  `NormalizeTenant` strips it, so the token can never collide with a real handle.
- **Self-join.** A session that registered itself through a hook and was never launched. It has no lineage at all,
  and `spawned_by` stays empty, which is the same "empty means nobody set this" every other column here uses.

Recommending `@human` over an empty value for the dialog case is what keeps human-initiated launches from being
indistinguishable from self-joins. The board can then say "you started this" for the operator's own launches too,
which is a smaller version of the same feature and comes for free.

## The UI

The smallest thing that answers the ask is a PARENT CHIP on the child card: a short line reading `spawned by
atrium-87300`, drawn in the same quiet style the board uses for a card's other observed facts, and clickable to the
parent card when `spawned_by_id` resolves to one on the board. `@human` draws as `you` rather than a link. An
unresolvable handle draws as plain text. This is board JS and CSS in `internal/api/web/`, embedded and hub-only
when built, and it reads a field the payload now carries rather than asking for anything new.

Two richer options, both worth noting and neither needed first:

- **A filter.** "Show what `atrium-87300` spawned" is a match on `spawned_by`, and it can reuse the room-tag filter
  path the aggregate view already drives rather than a parallel one. This is the direct answer to the operator's
  words about SEEING what a session spawned.
- **Indented grouping.** Draw launched children under their parent as an indented cluster. This is the most
  expressive and the most layout work, and it collides with the board's existing window grouping, so it is a later
  step rather than the first.

Start with the chip and the filter. The chip answers "who started this one" on any card, the filter answers "what
did this one start", and between them they cover the ask without the board learning a new grouping model.

## Lifecycle and staleness

A parent handle names a session, and a session ends. Lineage has to outlive it, and the store already has the
pattern: `fixture.task_id` and `card_share.task_id` are soft references with NO foreign key, kept precisely so a
swept card does not delete the row that points at it. `spawned_by_id` is the same kind of reference and takes the
same rule. It is a recorded string, not a constraint, so:

- A parent card that is ARCHIVED by the sweep (`internal/daemon/sweep.go`, dead cards after a minute) leaves the
  child's lineage intact. The chip still prints the handle. The click resolves to the archived card if the board
  still holds it, and degrades to plain text once it does not.
- A parent card that is PRUNED, deleted outright with its history, also leaves the child's lineage intact, for the
  same reason. The relation is a fact about how the child started, and that the parent's record was later discarded
  does not unmake it.
- A parent whose ROOM is offline resolves to nothing right now and draws as a plain chip, recovering a live link
  when the room reconnects.

A dangling parent is therefore never an error and never a blank. It is the handle text, which is the one durable
part of the relation, shown without a link it cannot honour.

## Out of scope

Named so the next reader does not have to guess where the line is.

- **Not a permission or ownership model.** A parent has no rights over a child it does not already have as a peer.
  Lineage records who started whom and grants nothing.
- **Not a control channel.** A parent does not command a child beyond the peer bus every session already shares.
  `atrium_say` is the whole of the channel, and lineage adds no second one.
- **Not claude subagent trees.** A subagent is an in-process turn with no card and no life of its own. Lineage is
  between real sessions, each with a card, and the two mechanisms do not meet.
- **Not retroactive.** Cards that exist before this ships have no parent to recover, so they carry empty lineage
  and are drawn without a chip. There is nothing to backfill.

## Open questions for review

1. **Qualified or local handle in the header.** `X-Atrium-Agent` carries the local name and the store works in
   qualified names. Qualifying at the `SetLineage` boundary is the recommendation above, but it assumes the child's
   room is the right tenant to qualify the PARENT with, which is only true when parent and child share a room. A
   cross-room launch needs the parent's own room to qualify correctly, and that room is knowable at the hub. Worth
   settling whether the hub should qualify the handle before it travels, rather than the room qualifying on
   arrival.

2. **Whether to store the parent's room explicitly.** `spawned_by_id` carries it inside the `room~id` tag, but a
   handle alone does not. A separate `spawned_by_room` column would make an offline-parent chip able to say which
   machine it is waiting on, at the cost of a third column for something the tag mostly already holds.

3. **Depth of walk for grouping.** The direct-parent-only model derives chains by walking, which is cheap over the
   in-memory list but unbounded in principle. A confused fan-out could in theory launch a chain long enough to
   matter. Probably not worth a guard, but worth deciding rather than assuming.

4. **Whether a filter belongs to lineage or to the existing tag path.** A launched wave could instead be tagged
   with the parent handle at launch, which would make "what did this spawn" a plain tag filter and need no new
   filter code at all. That trades a dedicated relation for reuse of tags, and it is worth weighing before building
   a lineage-specific filter.
</content>
</invoke>
