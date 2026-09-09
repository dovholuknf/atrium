# Rooms you cannot see

**Status: a decision being made, not a design. Nothing here is built.** Written 2026-09-08 in the middle of the
conversation that produced it, so that the reasoning survives.

## What the operator asked for

> these rooms and the agents IN those rooms should all be surfaced in atrium just like we are. as a user i
> should not know what room i'm in (atrium main, vs cdaws, vs wherever) to me they are all just agents... now
> of course i WILL care but that's the basic idea.

And, separately, that a room still publishes its own board:

> it needs its own board after, for when atrium disconnects sure

So both, and in that order of importance. Normally a remote agent is just an agent on the board you are already
looking at. When the hub is gone, the room is still reachable on its own address.

## What this contradicts

`docs/federation-design-v2.md` and the rooms code say the opposite, in several places and on purpose:

- **The hub holds nothing durable.** A room's cards belong to that room's database, and a copy on the hub would
  be a second source of truth that is wrong whenever the room is unreachable. `internal/daemon/rooms.go` says
  so where it declines to store them.
- **Terminals stay where they are.** A pty cannot leave the machine that made it, so attaching to a remote card
  was to be a REDIRECT to that room's own board. `docs/dispatch-queue.md` NEXT item 2.
- **Nothing is proxied.** `docs/overlays.md` draws that line hard: both SDKs hand back a `net.Listener` and the
  board is one handler, so no traffic passes through atrium that was not already destined for it.

Transparency breaks the second and third of those, and puts pressure on the first. That does not make it wrong.
It makes it a decision that has to be taken deliberately rather than discovered halfway through an
implementation.

## The shape, if it is taken

The room already dials the hub every twenty seconds and the reply is already a channel outward: that is how
permission decisions and queued launches reach a room today. A terminal is the same direction with a lot more
bytes.

- **A remote card is drawn like a local one.** Same list, same row, same attach. The room is an attribute, the
  way `hostname` already is, discoverable when somebody cares and invisible when they do not.
- **Attach relays.** The board opens its usual websocket to the hub; the hub carries it to the room; the room
  attaches locally and streams back. The hub forwards and stores nothing, which keeps the first rule even
  though it breaks the third.
- **The room's own board stays**, published on its own address, as the answer for when the hub is down. That is
  group P, already decided: discrete shares, no aggregation.

## What has to be answered before anything is written

1. **What else becomes transparent?** A terminal is the loudest one and not the only one. Permission requests
   already forward. What about a card's files, its actions, its messages, its recap, shelving it, killing it?
   Each is a hub endpoint that would have to grow a remote case. Decide the SET, not the first item.
2. **Card identity across two stores.** A remote card's id belongs to that room's database. Two rooms can
   produce the same id shape, and a card can move. Does the hub address a remote card as `room + id`, or does
   something mint a global name? This is the question that decides whether the hub can stay stateless.
3. **What happens when the hub dies mid-attach.** The terminal is a websocket through a machine that just went
   away. The board already knows how to say `atrium is restarting` and wait. Is a relayed terminal the same
   thing, or does it say "this session is on cdaws, open its board" and hand over?
4. **Latency and the twenty second heartbeat.** The check-in cadence is fine for cards and unusable for
   keystrokes. A relayed terminal needs a connection the room holds open, which is a second thing for a room to
   maintain and a second thing to back off on. `docs/architecture-v2.md` already rejected a websocket for the
   agent listener; this is that argument again with a stronger case.
5. **Who may attach.** A guest holding a lent session's link reaches one terminal. An operator on the hub
   reaching every room's terminals is a different amount of trust, and it is trust the ROOM is granting to the
   hub rather than the other way round. A room started with `--no-launch` already says it will not run work;
   there is no equivalent for "you may not drive my terminals".
6. **What a room shows when the hub has never heard of it.** Transparency means the operator stops learning
   which machine a card is on. When something goes wrong they need that back immediately, not after opening a
   dialog.

## The thing to keep hold of

The operator's own framing has both halves and they are not in tension: **transparent when it is working,
separately reachable when it is not.** A design that delivers only the first makes the hub a single point of
failure for four machines. A design that delivers only the second is what exists today and is the thing being
complained about.
